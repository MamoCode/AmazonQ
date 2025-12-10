package core

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// GetCurrentTimestamp 获取 Amazon Q 格式的当前时间戳
// 返回格式为 "Weekday, ISO8601" 的时间字符串
func GetCurrentTimestamp() string {
	now := time.Now()
	weekday := now.Format("Monday")
	isoTime := now.Format("2006-01-02T15:04:05.000Z07:00")
	return fmt.Sprintf("%s, %s", weekday, isoTime)
}

// IsThinkingModeEnabled 检测是否启用了 thinking 模式
// 参数 thinking 为 thinking 配置对象
// 返回是否启用
func IsThinkingModeEnabled(thinking *ThinkingConfig) bool {
	if thinking == nil {
		return false
	}
	return strings.ToLower(thinking.Type) == "enabled"
}

// GetThinkingBudgetTokens 获取 thinking 预算 token 数
// 参数 thinking 为 thinking 配置对象
// 返回预算 token 数（默认 16000）
func GetThinkingBudgetTokens(thinking *ThinkingConfig) int {
	if thinking == nil || thinking.BudgetTokens <= 0 {
		return 16000
	}
	return thinking.BudgetTokens
}

// BuildThinkingHint 构建 thinking 提示词
// 参数 budgetTokens 为预算 token 数
// 返回格式化的 thinking 提示
func BuildThinkingHint(budgetTokens int) string {
	return fmt.Sprintf("<thinking_mode>interleaved</thinking_mode><max_thinking_length>%s</max_thinking_length>", strconv.Itoa(budgetTokens))
}

// AppendThinkingHint 在文本末尾追加 thinking 提示
// 参数 text 为原始文本
// 参数 hint 为 thinking 提示
// 返回追加后的文本
func AppendThinkingHint(text string, hint string) string {
	normalized := strings.TrimSpace(text)
	if strings.HasSuffix(normalized, hint) {
		return text
	}
	if text == "" {
		return hint
	}
	separator := ""
	if !strings.HasSuffix(text, "\n") && !strings.HasSuffix(text, "\r") {
		separator = "\n"
	}
	return fmt.Sprintf("%s%s%s", text, separator, hint)
}

// ContainsToolContent 检查内容是否包含工具调用或工具结果
// 参数 content 为消息内容
// 返回是否包含工具相关内容
func ContainsToolContent(content interface{}) bool {
	contentList, ok := content.([]interface{})
	if !ok {
		return false
	}
	for _, block := range contentList {
		if blockMap, ok := block.(map[string]interface{}); ok {
			btype, _ := blockMap["type"].(string)
			if btype == "tool_result" || btype == "tool_use" {
				return true
			}
		}
	}
	return false
}


// ExtractTextFromContent 从 Claude 消息内容中提取纯文本
// 参数 content 为 Claude 消息内容（可能是字符串或内容块数组）
// 返回提取的文本内容
func ExtractTextFromContent(content interface{}) string {
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		var parts []string
		for _, block := range v {
			if blockMap, ok := block.(map[string]interface{}); ok {
				if blockMap["type"] == "text" {
					if text, ok := blockMap["text"].(string); ok {
						parts = append(parts, text)
					}
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// ExtractImagesFromContent 从 Claude 内容中提取图片并转换为 Amazon Q 格式
// 参数 content 为 Claude 消息内容
// 返回 Amazon Q 格式的图片列表
func ExtractImagesFromContent(content interface{}) []AmazonQImage {
	var images []AmazonQImage

	contentList, ok := content.([]interface{})
	if !ok {
		return nil
	}

	for _, block := range contentList {
		blockMap, ok := block.(map[string]interface{})
		if !ok {
			continue
		}

		if blockMap["type"] == "image" {
			source, ok := blockMap["source"].(map[string]interface{})
			if !ok {
				continue
			}

			if source["type"] == "base64" {
				mediaType := "image/png"
				if mt, ok := source["media_type"].(string); ok {
					mediaType = mt
				}

				format := "png"
				if strings.Contains(mediaType, "/") {
					parts := strings.Split(mediaType, "/")
					format = parts[len(parts)-1]
				}

				data := ""
				if d, ok := source["data"].(string); ok {
					data = d
				}

				images = append(images, AmazonQImage{
					Format: format,
					Source: ImageBytes{Bytes: data},
				})
			}
		}
	}

	return images
}

// ProcessToolResultBlock 处理单个 tool_result 块，提取内容并添加到结果列表
// 参数 block 为 tool_result 类型的内容块
// 参数 toolResults 为用于存储处理结果的列表指针
func ProcessToolResultBlock(block map[string]interface{}, toolResults *[]ToolResult) {
	toolUseID, _ := block["tool_use_id"].(string)
	rawContent := block["content"]

	var aqContent []ToolResultContent

	switch content := rawContent.(type) {
	case string:
		aqContent = []ToolResultContent{{Text: content}}
	case []interface{}:
		for _, item := range content {
			switch v := item.(type) {
			case map[string]interface{}:
				if v["type"] == "text" {
					if text, ok := v["text"].(string); ok {
						aqContent = append(aqContent, ToolResultContent{Text: text})
					}
				} else if text, ok := v["text"].(string); ok {
					aqContent = append(aqContent, ToolResultContent{Text: text})
				}
			case string:
				aqContent = append(aqContent, ToolResultContent{Text: v})
			}
		}
	}

	// 检查是否有非空内容
	hasContent := false
	for _, c := range aqContent {
		if strings.TrimSpace(c.Text) != "" {
			hasContent = true
			break
		}
	}

	if !hasContent {
		aqContent = []ToolResultContent{{Text: "Tool use was cancelled by the user"}}
	}

	status := "success"
	if s, ok := block["status"].(string); ok {
		status = s
	}

	// 检查是否已存在相同的 toolUseId
	found := false
	for i := range *toolResults {
		if (*toolResults)[i].ToolUseID == toolUseID {
			(*toolResults)[i].Content = append((*toolResults)[i].Content, aqContent...)
			found = true
			break
		}
	}

	if !found {
		*toolResults = append(*toolResults, ToolResult{
			ToolUseID: toolUseID,
			Content:   aqContent,
			Status:    status,
		})
	}
}

// ConvertTool 将 Claude 工具定义转换为 Amazon Q 工具格式
// 参数 tool 为 Claude 工具定义
// 返回 Amazon Q 格式的工具定义
func ConvertTool(tool ClaudeTool) AmazonQTool {
	desc := tool.Description
	if len(desc) > 10240 {
		desc = desc[:10100] + "\n\n...(Full description provided in TOOL DOCUMENTATION section)"
	}

	return AmazonQTool{
		ToolSpecification: ToolSpecification{
			Name:        tool.Name,
			Description: desc,
			InputSchema: map[string]interface{}{
				"json": tool.InputSchema,
			},
		},
	}
}

// MergeUserMessages 合并连续的用户消息，仅保留最后 2 条消息的图片
// 参数 messages 为用户消息列表
// 返回合并后的单条消息
func MergeUserMessages(messages []UserInputMessage) UserInputMessage {
	if len(messages) == 0 {
		return UserInputMessage{}
	}

	var allContents []string
	var baseContext UserInputMessageContext
	var baseOrigin string
	var baseModelID string
	var allImages [][]AmazonQImage

	for i, msg := range messages {
		if i == 0 {
			baseContext = msg.UserInputMessageContext
			baseOrigin = msg.Origin
			baseModelID = msg.ModelID
		}

		if msg.Content != "" {
			allContents = append(allContents, msg.Content)
		}

		if len(msg.Images) > 0 {
			allImages = append(allImages, msg.Images)
		}
	}

	result := UserInputMessage{
		Content:                 strings.Join(allContents, "\n\n"),
		UserInputMessageContext: baseContext,
		Origin:                  baseOrigin,
		ModelID:                 baseModelID,
	}

	// 仅保留最后 2 条消息的图片
	if len(allImages) > 0 {
		var keptImages []AmazonQImage
		startIdx := len(allImages) - 2
		if startIdx < 0 {
			startIdx = 0
		}
		for i := startIdx; i < len(allImages); i++ {
			keptImages = append(keptImages, allImages[i]...)
		}
		if len(keptImages) > 0 {
			result.Images = keptImages
		}
	}

	return result
}

// ProcessHistory 处理历史消息，转换为 Amazon Q 格式（交替的 user/assistant）
// 参数 messages 为 Claude 消息列表
// 参数 thinkingEnabled 为是否启用 thinking 模式
// 参数 thinkingHint 为 thinking 提示词
// 返回 Amazon Q 格式的历史消息列表
func ProcessHistory(messages []ClaudeMessage, thinkingEnabled bool, thinkingHint string) []HistoryEntry {
	var history []HistoryEntry
	seenToolUseIDs := make(map[string]bool)
	var rawHistory []HistoryEntry

	// 第一遍：转换单个消息
	for _, msg := range messages {
		if msg.Role == "user" {
			content := msg.Content
			textContent := ""
			var toolResults []ToolResult
			images := ExtractImagesFromContent(content)
			shouldAppendHint := thinkingEnabled && !ContainsToolContent(content)

			if contentList, ok := content.([]interface{}); ok {
				var textParts []string
				for _, block := range contentList {
					if blockMap, ok := block.(map[string]interface{}); ok {
						btype, _ := blockMap["type"].(string)
						if btype == "text" {
							if text, ok := blockMap["text"].(string); ok {
								textParts = append(textParts, text)
							}
						} else if btype == "tool_result" {
							ProcessToolResultBlock(blockMap, &toolResults)
						}
					}
				}
				textContent = strings.Join(textParts, "\n")
			} else {
				textContent = ExtractTextFromContent(content)
			}

			if shouldAppendHint {
				textContent = AppendThinkingHint(textContent, thinkingHint)
			}

			userCtx := UserInputMessageContext{
				EnvState: EnvState{
					OperatingSystem:         "macos",
					CurrentWorkingDirectory: "/",
				},
			}
			if len(toolResults) > 0 {
				userCtx.ToolResults = toolResults
			}

			uMsg := UserInputMessage{
				Content:                 textContent,
				UserInputMessageContext: userCtx,
				Origin:                  "CLI",
			}
			if len(images) > 0 {
				uMsg.Images = images
			}

			rawHistory = append(rawHistory, HistoryEntry{
				UserInputMessage: &uMsg,
			})

		} else if msg.Role == "assistant" {
			content := msg.Content
			textContent := ExtractTextFromContent(content)

			entry := HistoryEntry{
				AssistantResponseMessage: &AssistantResponseMessage{
					MessageID: uuid.New().String(),
					Content:   textContent,
				},
			}

			if contentList, ok := content.([]interface{}); ok {
				var toolUses []ToolUse
				for _, block := range contentList {
					if blockMap, ok := block.(map[string]interface{}); ok {
						if blockMap["type"] == "tool_use" {
							tid, _ := blockMap["id"].(string)
							if tid != "" && !seenToolUseIDs[tid] {
								seenToolUseIDs[tid] = true
								name, _ := blockMap["name"].(string)
								input := make(map[string]interface{})
								if inp, ok := blockMap["input"].(map[string]interface{}); ok {
									input = inp
								}
								toolUses = append(toolUses, ToolUse{
									ToolUseID: tid,
									Name:      name,
									Input:     input,
								})
							}
						}
					}
				}
				if len(toolUses) > 0 {
					entry.AssistantResponseMessage.ToolUses = toolUses
				}
			}

			rawHistory = append(rawHistory, entry)
		}
	}

	// 第二遍：合并连续的用户消息
	var pendingUserMsgs []UserInputMessage
	for _, item := range rawHistory {
		if item.UserInputMessage != nil {
			pendingUserMsgs = append(pendingUserMsgs, *item.UserInputMessage)
		} else if item.AssistantResponseMessage != nil {
			if len(pendingUserMsgs) > 0 {
				merged := MergeUserMessages(pendingUserMsgs)
				history = append(history, HistoryEntry{UserInputMessage: &merged})
				pendingUserMsgs = nil
			}
			history = append(history, item)
		}
	}

	if len(pendingUserMsgs) > 0 {
		merged := MergeUserMessages(pendingUserMsgs)
		history = append(history, HistoryEntry{UserInputMessage: &merged})
	}

	return history
}

// ConvertClaudeToAmazonQRequest 将 Claude API 请求转换为 Amazon Q API 请求体
// 参数 req 为 Claude API 请求对象
// 参数 conversationID 为会话 ID（可选，为空时自动生成）
// 返回 Amazon Q API 请求体和可能的错误
func ConvertClaudeToAmazonQRequest(req ClaudeRequest, conversationID string) (AmazonQRequest, error) {
	if conversationID == "" {
		conversationID = uuid.New().String()
	}

	// 检测 thinking 模式
	thinkingEnabled := IsThinkingModeEnabled(req.Thinking)
	budgetTokens := GetThinkingBudgetTokens(req.Thinking)
	thinkingHint := BuildThinkingHint(budgetTokens)

	// 1. 工具转换
	var aqTools []AmazonQTool
	var longDescTools []map[string]string

	for _, t := range req.Tools {
		if len(t.Description) > 10240 {
			longDescTools = append(longDescTools, map[string]string{
				"name":             t.Name,
				"full_description": t.Description,
			})
		}
		aqTools = append(aqTools, ConvertTool(t))
	}

	// 2. 当前消息（最后一条用户消息）
	var lastMsg *ClaudeMessage
	if len(req.Messages) > 0 {
		lastMsg = &req.Messages[len(req.Messages)-1]
	}

	promptContent := ""
	var toolResults []ToolResult
	hasToolResult := false
	var images []AmazonQImage

	if lastMsg != nil && lastMsg.Role == "user" {
		content := lastMsg.Content
		images = ExtractImagesFromContent(content)

		if contentList, ok := content.([]interface{}); ok {
			var textParts []string
			for _, block := range contentList {
				if blockMap, ok := block.(map[string]interface{}); ok {
					btype, _ := blockMap["type"].(string)
					if btype == "text" {
						if text, ok := blockMap["text"].(string); ok {
							textParts = append(textParts, text)
						}
					} else if btype == "tool_result" {
						hasToolResult = true
						ProcessToolResultBlock(blockMap, &toolResults)
					}
				}
			}
			promptContent = strings.Join(textParts, "\n")
		} else {
			promptContent = ExtractTextFromContent(content)
		}
	}

	// 注入 thinking 提示（如果启用且不包含工具内容）
	if thinkingEnabled && lastMsg != nil && !ContainsToolContent(lastMsg.Content) {
		promptContent = AppendThinkingHint(promptContent, thinkingHint)
	}

	// 3. 上下文构建
	userCtx := UserInputMessageContext{
		EnvState: EnvState{
			OperatingSystem:         "macos",
			CurrentWorkingDirectory: "/",
		},
	}
	if len(aqTools) > 0 {
		userCtx.Tools = aqTools
	}
	if len(toolResults) > 0 {
		userCtx.ToolResults = toolResults
	}

	// 4. 格式化内容
	formattedContent := ""
	if hasToolResult && promptContent == "" {
		formattedContent = ""
	} else {
		formattedContent = fmt.Sprintf(
			"--- CONTEXT ENTRY BEGIN ---\nCurrent time: %s\n--- CONTEXT ENTRY END ---\n\n--- USER MESSAGE BEGIN ---\n%s\n--- USER MESSAGE END ---",
			GetCurrentTimestamp(),
			promptContent,
		)
	}

	if len(longDescTools) > 0 {
		var docs []string
		for _, info := range longDescTools {
			docs = append(docs, fmt.Sprintf("Tool: %s\nFull Description:\n%s\n", info["name"], info["full_description"]))
		}
		formattedContent = fmt.Sprintf(
			"--- TOOL DOCUMENTATION BEGIN ---\n%s--- TOOL DOCUMENTATION END ---\n\n%s",
			strings.Join(docs, ""),
			formattedContent,
		)
	}

	if req.System != nil && formattedContent != "" {
		sysText := ""
		switch sys := req.System.(type) {
		case string:
			sysText = sys
		case []interface{}:
			var parts []string
			for _, b := range sys {
				if blockMap, ok := b.(map[string]interface{}); ok {
					if blockMap["type"] == "text" {
						if text, ok := blockMap["text"].(string); ok {
							parts = append(parts, text)
						}
					}
				}
			}
			sysText = strings.Join(parts, "\n")
		}

		if sysText != "" {
			formattedContent = fmt.Sprintf(
				"--- SYSTEM PROMPT BEGIN ---\n%s\n--- SYSTEM PROMPT END ---\n\n%s",
				sysText,
				formattedContent,
			)
		}
	}

	// 5. 用户输入消息
	userInputMsg := UserInputMessage{
		Content:                 formattedContent,
		UserInputMessageContext: userCtx,
		Origin:                  "CLI",
		ModelID:                 req.Model,
	}
	if len(images) > 0 {
		userInputMsg.Images = images
	}

	// 6. 历史消息处理
	var historyMsgs []ClaudeMessage
	if len(req.Messages) > 1 {
		historyMsgs = req.Messages[:len(req.Messages)-1]
	}
	aqHistory := ProcessHistory(historyMsgs, thinkingEnabled, thinkingHint)

	// 7. 最终请求体
	return AmazonQRequest{
		ConversationState: ConversationState{
			ConversationID: conversationID,
			History:        aqHistory,
			CurrentMessage: CurrentMessage{
				UserInputMessage: userInputMsg,
			},
			ChatTriggerType: "MANUAL",
		},
	}, nil
}

// ExtractSystemText 从系统提示中提取文本内容
// 参数 system 为系统提示（可能是字符串或内容块数组）
// 返回提取的文本
func ExtractSystemText(system interface{}) string {
	switch v := system.(type) {
	case string:
		return v
	case []interface{}:
		var parts []string
		for _, item := range v {
			if block, ok := item.(map[string]interface{}); ok {
				if block["type"] == "text" {
					if text, ok := block["text"].(string); ok {
						parts = append(parts, text)
					}
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// ToJSON 将数据转换为 JSON 字符串
// 参数 data 为待转换的数据
// 返回 JSON 字符串和可能的错误
func ToJSON(data interface{}) (string, error) {
	bytes, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}
