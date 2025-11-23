package amazonq

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"amazonq-proxy/internal/config"
	"amazonq-proxy/internal/utils"

	"github.com/google/uuid"
)

// SendChatRequest 发送聊天请求到 Amazon Q API
// 参数 ctx 为上下文
// 参数 accessToken 为 Amazon Q access token
// 参数 rawPayload 为 Claude API 转换后的请求体
// 参数 stream 表示是否流式响应
// 返回事件通道和可能的错误
func SendChatRequest(ctx context.Context, accessToken string, rawPayload map[string]interface{}, stream bool) (chan *EventStreamMessage, error) {
	// 确保 conversationId 已设置
	if convState, ok := rawPayload["conversationState"].(map[string]interface{}); ok {
		if _, exists := convState["conversationId"]; !exists {
			convState["conversationId"] = uuid.New().String()
		}
	}

	// 序列化请求体
	payloadBytes, err := json.Marshal(rawPayload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	// 构建请求
	req, err := http.NewRequestWithContext(ctx, "POST", config.AmazonQAPIURL, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// 设置请求头
	headers := mergeHeaders(config.DefaultHeaders, accessToken)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	// 创建 HTTP 客户端
	client := &http.Client{
		Transport: utils.CreateProxyTransport(),
		Timeout:   5 * time.Minute,
	}

	// 发送请求
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	// 检查响应状态
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("upstream error %d: %s", resp.StatusCode, string(body))
	}

	// 创建事件通道
	eventChan := make(chan *EventStreamMessage, 100)

	// 在后台解析流
	go func() {
		defer resp.Body.Close()
		err := ParseStream(resp.Body, eventChan)
		if err != nil {
			// 记录错误但不中断流
			fmt.Printf("stream parsing error: %v\n", err)
		}
	}()

	return eventChan, nil
}

// mergeHeaders 合并并更新请求头
// 参数 baseHeaders 为基础请求头
// 参数 bearerToken 为认证令牌
// 返回合并后的请求头映射
func mergeHeaders(baseHeaders map[string]string, bearerToken string) map[string]string {
	headers := make(map[string]string)

	// 复制基础头部
	for k, v := range baseHeaders {
		headers[k] = v
	}

	// 移除不应传递的头部
	delete(headers, "content-length")
	delete(headers, "host")
	delete(headers, "connection")
	delete(headers, "transfer-encoding")

	// 设置认证和请求 ID
	headers["Authorization"] = fmt.Sprintf("Bearer %s", bearerToken)
	headers["amz-sdk-invocation-id"] = uuid.New().String()

	return headers
}

// ProcessEventStream 处理事件流并生成 Claude SSE 事件
// 参数 eventChan 为事件消息通道
// 参数 handler 为流处理器
// 返回 SSE 事件字符串通道
func ProcessEventStream(eventChan chan *EventStreamMessage, handler *ClaudeStreamHandler) chan string {
	sseChan := make(chan string, 100)

	go func() {
		defer close(sseChan)

		for message := range eventChan {
			eventInfo := ExtractEventInfo(message)
			if eventInfo != nil && eventInfo.EventType != "" {
				sseEvents := handler.HandleEvent(eventInfo.EventType, eventInfo.Payload)
				for _, event := range sseEvents {
					sseChan <- event
				}
			}
		}

		// 发送最终事件
		finalEvents := handler.Finish()
		for _, event := range finalEvents {
			sseChan <- event
		}
	}()

	return sseChan
}
