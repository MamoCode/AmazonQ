package amazonq

import (
	"encoding/json"
	"strings"
)

const (
	// ThinkingStartTag thinking 块开始标签
	ThinkingStartTag = "<thinking>"
	// ThinkingEndTag thinking 块结束标签
	ThinkingEndTag = "</thinking>"
)

// ClaudeStreamHandler Claude SSE 流处理器，将 Amazon Q 事件转换为 Claude API 格式
type ClaudeStreamHandler struct {
	Model                  string
	InputTokens            int
	ResponseBuffer         []string
	ContentBlockIndex      int
	ContentBlockStarted    bool
	ContentBlockStartSent  bool
	ContentBlockStopSent   bool
	MessageStartSent       bool
	ConversationID         string
	MessageID              string
	CurrentToolUse         map[string]interface{}
	ToolInputBuffer        []string
	ToolUseID              string
	ToolName               string
	ProcessedToolUseIDs    map[string]bool
	AllToolInputs          []string
	// Thinking 相关状态
	InThinkBlock           bool
	ThinkBuffer            string
	// 用于延迟发送 ping 事件
	PingPending            bool
}

// NewClaudeStreamHandler 创建新的流处理器实例
// 参数 model 为模型名称
// 参数 inputTokens 为输入 token 数量
// 返回初始化的流处理器实例
func NewClaudeStreamHandler(model string, inputTokens int) *ClaudeStreamHandler {
	return &ClaudeStreamHandler{
		Model:                 model,
		InputTokens:           inputTokens,
		ResponseBuffer:        []string{},
		ContentBlockIndex:     -1,
		ProcessedToolUseIDs:   make(map[string]bool),
		AllToolInputs:         []string{},
	}
}

// HandleEvent 处理单个 Amazon Q 事件并生成 Claude SSE 事件
// 参数 eventType 为事件类型
// 参数 payload 为事件数据
// 返回 SSE 事件字符串列表
func (h *ClaudeStreamHandler) HandleEvent(eventType string, payload interface{}) []string {
	var events []string

	payloadMap, ok := payload.(map[string]interface{})
	if !ok {
		return events
	}

	// 1. 消息开始 (initial-response)
	if eventType == "initial-response" {
		if !h.MessageStartSent {
			convID, _ := payloadMap["conversationId"].(string)
			if convID == "" {
				convID = h.ConversationID
			}
			if convID == "" {
				convID = "unknown"
			}
			h.ConversationID = convID
			messageStartEvent, messageID := BuildMessageStart(h.Model, h.InputTokens)
			h.MessageID = messageID
			events = append(events, messageStartEvent)
			h.MessageStartSent = true
			// 延迟发送 ping，等待第一个 content_block_start 之后
			h.PingPending = true
		}
	}

	// 2. 内容块增量 (assistantResponseEvent)
	if eventType == "assistantResponseEvent" {
		content, _ := payloadMap["content"].(string)

		// 关闭任何打开的工具使用块
		if h.CurrentToolUse != nil && !h.ContentBlockStopSent {
			events = append(events, BuildContentBlockStop(h.ContentBlockIndex))
			h.ContentBlockStopSent = true
			h.CurrentToolUse = nil
		}

		// 处理内容并检测 thinking 标签
		if content != "" {
			h.ThinkBuffer += content
			pos := 0

			for pos < len(h.ThinkBuffer) {
				if !h.InThinkBlock {
					// 查找 <thinking> 标签
					thinkStart := strings.Index(h.ThinkBuffer[pos:], ThinkingStartTag)
					if thinkStart != -1 {
						thinkStart += pos
						// 发送 <thinking> 之前的文本
						beforeText := h.ThinkBuffer[pos:thinkStart]
						if beforeText != "" {
							if !h.ContentBlockStartSent {
								h.ContentBlockIndex++
								events = append(events, BuildContentBlockStart(h.ContentBlockIndex, "text"))
								h.ContentBlockStartSent = true
								h.ContentBlockStarted = true
								// 在第一个 content_block_start 之后发送 ping
								if h.PingPending {
									events = append(events, BuildPing())
									h.PingPending = false
								}
							}
							h.ResponseBuffer = append(h.ResponseBuffer, beforeText)
							events = append(events, BuildContentBlockDelta(h.ContentBlockIndex, beforeText))
						}

						// 关闭文本块并开启 thinking 块
						if h.ContentBlockStartSent {
							events = append(events, BuildContentBlockStop(h.ContentBlockIndex))
							h.ContentBlockStopSent = true
							h.ContentBlockStartSent = false
						}

						h.ContentBlockIndex++
						events = append(events, BuildContentBlockStart(h.ContentBlockIndex, "thinking"))
						h.ContentBlockStartSent = true
						h.ContentBlockStarted = true
						h.ContentBlockStopSent = false
						// 在第一个 content_block_start 之后发送 ping
						if h.PingPending {
							events = append(events, BuildPing())
							h.PingPending = false
						}
						h.InThinkBlock = true
						pos = thinkStart + len(ThinkingStartTag)
					} else {
						// 没有找到 <thinking>，发送剩余内容为文本
						remaining := h.ThinkBuffer[pos:]
						if !h.ContentBlockStartSent {
							h.ContentBlockIndex++
							events = append(events, BuildContentBlockStart(h.ContentBlockIndex, "text"))
							h.ContentBlockStartSent = true
							h.ContentBlockStarted = true
							// 在第一个 content_block_start 之后发送 ping
							if h.PingPending {
								events = append(events, BuildPing())
								h.PingPending = false
							}
						}
						h.ResponseBuffer = append(h.ResponseBuffer, remaining)
						events = append(events, BuildContentBlockDelta(h.ContentBlockIndex, remaining))
						h.ThinkBuffer = ""
						break
					}
				} else {
					// 查找 </thinking> 标签
					thinkEnd := strings.Index(h.ThinkBuffer[pos:], ThinkingEndTag)
					if thinkEnd != -1 {
						thinkEnd += pos
						// 发送 thinking 内容
						thinkingText := h.ThinkBuffer[pos:thinkEnd]
						if thinkingText != "" {
							events = append(events, BuildContentBlockDelta(h.ContentBlockIndex, thinkingText))
						}

						// 关闭 thinking 块
						events = append(events, BuildContentBlockStop(h.ContentBlockIndex))
						h.ContentBlockStopSent = true
						h.ContentBlockStartSent = false
						h.InThinkBlock = false
						pos = thinkEnd + len(ThinkingEndTag)
					} else {
						// 没有找到 </thinking>，发送剩余内容为 thinking
						remaining := h.ThinkBuffer[pos:]
						events = append(events, BuildContentBlockDelta(h.ContentBlockIndex, remaining))
						h.ThinkBuffer = ""
						break
					}
				}
			}

			// 保留未处理的内容在缓冲区
			if pos < len(h.ThinkBuffer) {
				h.ThinkBuffer = h.ThinkBuffer[pos:]
			} else {
				h.ThinkBuffer = ""
			}
		}
	}

	// 3. 工具使用 (toolUseEvent)
	if eventType == "toolUseEvent" {
		toolUseID, _ := payloadMap["toolUseId"].(string)
		toolName, _ := payloadMap["name"].(string)
		toolInput := payloadMap["input"]
		isStop, _ := payloadMap["stop"].(bool)

		// 启动新的工具使用
		if toolUseID != "" && toolName != "" && h.CurrentToolUse == nil {
			// 关闭之前的文本块
			if h.ContentBlockStartSent && !h.ContentBlockStopSent {
				events = append(events, BuildContentBlockStop(h.ContentBlockIndex))
				h.ContentBlockStopSent = true
			}

			h.ProcessedToolUseIDs[toolUseID] = true
			h.ContentBlockIndex++

			events = append(events, BuildToolUseStart(h.ContentBlockIndex, toolUseID, toolName))
			// 在第一个 content_block_start 之后发送 ping
			if h.PingPending {
				events = append(events, BuildPing())
				h.PingPending = false
			}

			h.ContentBlockStarted = true
			h.CurrentToolUse = map[string]interface{}{
				"toolUseId": toolUseID,
				"name":      toolName,
			}
			h.ToolUseID = toolUseID
			h.ToolName = toolName
			h.ToolInputBuffer = []string{}
			h.ContentBlockStopSent = false
			h.ContentBlockStartSent = true
		}

		// 累积输入
		if h.CurrentToolUse != nil && toolInput != nil {
			fragment := ""
			switch v := toolInput.(type) {
			case string:
				fragment = v
			default:
				jsonBytes, _ := json.Marshal(toolInput)
				fragment = string(jsonBytes)
			}

			h.ToolInputBuffer = append(h.ToolInputBuffer, fragment)
			events = append(events, BuildToolUseInputDelta(h.ContentBlockIndex, fragment))
		}

		// 停止工具使用
		if isStop && h.CurrentToolUse != nil {
			fullInput := strings.Join(h.ToolInputBuffer, "")
			h.AllToolInputs = append(h.AllToolInputs, fullInput)

			events = append(events, BuildContentBlockStop(h.ContentBlockIndex))
			h.ContentBlockStopSent = true
			h.ContentBlockStarted = false
			h.CurrentToolUse = nil
			h.ToolUseID = ""
			h.ToolName = ""
			h.ToolInputBuffer = []string{}
		}
	}

	// 4. 助手响应结束 (assistantResponseEnd)
	if eventType == "assistantResponseEnd" {
		// 关闭任何打开的块
		if h.ContentBlockStarted && !h.ContentBlockStopSent {
			events = append(events, BuildContentBlockStop(h.ContentBlockIndex))
			h.ContentBlockStopSent = true
		}
	}

	return events
}

// Finish 发送最终事件，关闭所有未关闭的内容块并计算 token 使用量
// 返回最终的 SSE 事件列表
func (h *ClaudeStreamHandler) Finish() []string {
	var events []string

	// 确保最后一个块已关闭
	if h.ContentBlockStarted && !h.ContentBlockStopSent {
		events = append(events, BuildContentBlockStop(h.ContentBlockIndex))
		h.ContentBlockStopSent = true
	}

	// 计算输出 token（近似）
	fullText := strings.Join(h.ResponseBuffer, "")
	fullToolInput := strings.Join(h.AllToolInputs, "")
	// 简单近似：4 个字符为 1 个 token
	outputTokens := (len(fullText) + len(fullToolInput)) / 4
	if outputTokens < 1 {
		outputTokens = 1
	}

	events = append(events, BuildMessageStop(h.InputTokens, outputTokens, nil))

	return events
}
