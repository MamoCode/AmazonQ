package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"amazonq-proxy/internal/amazonq"
	"amazonq-proxy/internal/core"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// SetupRouter 设置路由，配置 CORS 中间件和 API 端点
// 返回配置完成的 Gin 引擎实例
func SetupRouter() *gin.Engine {
	router := gin.Default()

	// CORS 中间件
	router.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "*")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "*")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	})

	// 根路径重定向
	router.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusFound, "https://www.bilibili.com/video/BV1SMH5zfEwe/?spm_id_from=333.337.search-card.all.click&vd_source=1f3b8eb28230105c578a443fa6481550")
	})

	// Claude 兼容的消息端点
	router.POST("/v1/messages", AuthMiddleware(), handleClaudeMessages)

	return router
}

// handleClaudeMessages 处理 Claude 兼容的消息请求
// 支持流式和非流式响应，将 Claude 请求转换为 Amazon Q 格式并返回结果
func handleClaudeMessages(c *gin.Context) {
	var req core.ClaudeRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("Invalid request: %v", err),
		})
		return
	}

	// 1. 转换请求
	aqRequest, err := core.ConvertClaudeToAmazonQRequest(req, "")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("Request conversion failed: %v", err),
		})
		return
	}

	// 2. 获取 access token
	accessToken, exists := c.Get("accessToken")
	if !exists {
		c.JSON(http.StatusBadGateway, gin.H{
			"error": "Access token unavailable",
		})
		return
	}

	// 将 aqRequest 转换为 map[string]interface{}
	var rawPayload map[string]interface{}
	jsonBytes, _ := json.Marshal(aqRequest)
	json.Unmarshal(jsonBytes, &rawPayload)

	// 3. 发送上游请求
	ctx := context.Background()
	eventChan, err := amazonq.SendChatRequest(ctx, accessToken.(string), rawPayload, true)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"error": fmt.Sprintf("Failed to send request: %v", err),
		})
		return
	}

	// 4. 流处理器
	handler := amazonq.NewClaudeStreamHandler(req.Model, 0)
	sseChan := amazonq.ProcessEventStream(eventChan, handler)

	if req.Stream {
		// 流式响应
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")

		c.Stream(func(w io.Writer) bool {
			if event, ok := <-sseChan; ok {
				// 直接写入，因为 FormatSSE 已经包含了完整的 SSE 格式
				w.Write([]byte(event))
				c.Writer.Flush()
				return true
			}
			return false
		})
	} else {
		// 非流式：累积响应
		var finalContent []interface{}
		usage := map[string]int{"input_tokens": 0, "output_tokens": 0}
		var stopReason *string

		for sseEvent := range sseChan {
			// 解析 SSE 事件格式: "event: xxx\ndata: {...}\n\n"
			lines := strings.Split(sseEvent, "\n")
			var dataStr string
			for _, line := range lines {
				if strings.HasPrefix(line, "data: ") {
					dataStr = strings.TrimPrefix(line, "data: ")
					break
				}
			}

			if dataStr == "" || dataStr == "[DONE]" {
				continue
			}

			var data map[string]interface{}
			if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
				continue
			}

			dtype, _ := data["type"].(string)
			if dtype == "content_block_start" {
				idx := int(data["index"].(float64))
				for len(finalContent) <= idx {
					finalContent = append(finalContent, nil)
				}
				finalContent[idx] = data["content_block"]
			} else if dtype == "content_block_delta" {
				idx := int(data["index"].(float64))
				delta := data["delta"].(map[string]interface{})
				if finalContent[idx] != nil {
					block := finalContent[idx].(map[string]interface{})
					if delta["type"] == "text_delta" {
						if text, ok := delta["text"].(string); ok {
							currentText, _ := block["text"].(string)
							block["text"] = currentText + text
						}
					} else if delta["type"] == "input_json_delta" {
						if partialJSON, ok := delta["partial_json"].(string); ok {
							currentJSON, _ := block["partial_json"].(string)
							block["partial_json"] = currentJSON + partialJSON
						}
					}
				}
			} else if dtype == "content_block_stop" {
				idx := int(data["index"].(float64))
				if finalContent[idx] != nil {
					block := finalContent[idx].(map[string]interface{})
					if block["type"] == "tool_use" {
						if partialJSON, ok := block["partial_json"].(string); ok {
							var input map[string]interface{}
							if err := json.Unmarshal([]byte(partialJSON), &input); err == nil {
								block["input"] = input
							}
							delete(block, "partial_json")
						}
					}
				}
			} else if dtype == "message_delta" {
				if usageData, ok := data["usage"].(map[string]interface{}); ok {
					for k, v := range usageData {
						if val, ok := v.(float64); ok {
							usage[k] = int(val)
						}
					}
				}
				if delta, ok := data["delta"].(map[string]interface{}); ok {
					if sr, ok := delta["stop_reason"].(string); ok {
						stopReason = &sr
					}
				}
			}
		}

		// 过滤 nil 元素
		var filteredContent []interface{}
		for _, item := range finalContent {
			if item != nil {
				filteredContent = append(filteredContent, item)
			}
		}

		c.JSON(http.StatusOK, gin.H{
			"id":            fmt.Sprintf("msg_%s", uuid.New().String()),
			"type":          "message",
			"role":          "assistant",
			"model":         req.Model,
			"content":       filteredContent,
			"stop_reason":   stopReason,
			"stop_sequence": nil,
			"usage":         usage,
		})
	}
}
