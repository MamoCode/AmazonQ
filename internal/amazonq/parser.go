package amazonq

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

// EventStreamMessage 表示事件流中的单个消息
type EventStreamMessage struct {
	Headers     map[string]string
	Payload     interface{}
	TotalLength uint32
}

// EventInfo 存储解析后的事件信息
type EventInfo struct {
	EventType   string
	ContentType string
	MessageType string
	Payload     interface{}
}

// SSEEvent 表示 Server-Sent Events 事件结构
type SSEEvent struct {
	Event string
	Data  interface{}
}

// ParseHeaders 解析事件流消息的头部数据
// 参数 headersData 为头部二进制数据
// 返回解析后的头部键值对映射
func ParseHeaders(headersData []byte) map[string]string {
	headers := make(map[string]string)
	offset := 0

	for offset < len(headersData) {
		if offset >= len(headersData) {
			break
		}
		nameLength := int(headersData[offset])
		offset++

		if offset+nameLength > len(headersData) {
			break
		}
		name := string(headersData[offset : offset+nameLength])
		offset += nameLength

		if offset >= len(headersData) {
			break
		}
		valueType := headersData[offset]
		offset++

		if offset+2 > len(headersData) {
			break
		}
		valueLength := binary.BigEndian.Uint16(headersData[offset : offset+2])
		offset += 2

		if offset+int(valueLength) > len(headersData) {
			break
		}

		var value string
		if valueType == 7 {
			value = string(headersData[offset : offset+int(valueLength)])
		} else {
			value = string(headersData[offset : offset+int(valueLength)])
		}

		offset += int(valueLength)
		headers[name] = value
	}

	return headers
}

// ParseMessage 解析单个 Event Stream 消息
// 参数 data 为完整的消息二进制数据
// 返回解析后的消息结构和可能的错误
func ParseMessage(data []byte) (*EventStreamMessage, error) {
	if len(data) < 16 {
		return nil, fmt.Errorf("incomplete message: data too short")
	}

	totalLength := binary.BigEndian.Uint32(data[0:4])
	headersLength := binary.BigEndian.Uint32(data[4:8])

	if len(data) < int(totalLength) {
		return nil, fmt.Errorf("incomplete message: expected %d bytes, got %d", totalLength, len(data))
	}

	headersData := data[12 : 12+headersLength]
	headers := ParseHeaders(headersData)

	payloadStart := 12 + headersLength
	payloadEnd := totalLength - 4
	payloadData := data[payloadStart:payloadEnd]

	var payload interface{}
	if len(payloadData) > 0 {
		err := json.Unmarshal(payloadData, &payload)
		if err != nil {
			payload = string(payloadData)
		}
	}

	return &EventStreamMessage{
		Headers:     headers,
		Payload:     payload,
		TotalLength: totalLength,
	}, nil
}

// ParseStream 从字节流中解析事件并发送到通道
// 参数 reader 为字节流读取器
// 参数 eventChan 为事件输出通道
// 返回可能的错误
func ParseStream(reader io.Reader, eventChan chan<- *EventStreamMessage) error {
	defer close(eventChan)

	buffer := make([]byte, 0)
	chunk := make([]byte, 4096)

	for {
		n, err := reader.Read(chunk)
		if n > 0 {
			buffer = append(buffer, chunk[:n]...)

			for len(buffer) >= 12 {
				if len(buffer) < 4 {
					break
				}

				totalLength := binary.BigEndian.Uint32(buffer[0:4])

				if len(buffer) < int(totalLength) {
					break
				}

				messageData := buffer[:totalLength]
				buffer = buffer[totalLength:]

				message, parseErr := ParseMessage(messageData)
				if parseErr != nil {
					continue
				}

				eventChan <- message
			}
		}

		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}

	return nil
}

// ExtractEventInfo 从解析后的消息中提取事件信息
// 参数 message 为解析后的消息结构
// 返回提取的事件信息
func ExtractEventInfo(message *EventStreamMessage) *EventInfo {
	headers := message.Headers

	eventType := headers[":event-type"]
	if eventType == "" {
		eventType = headers["event-type"]
	}

	contentType := headers[":content-type"]
	if contentType == "" {
		contentType = headers["content-type"]
	}

	messageType := headers[":message-type"]
	if messageType == "" {
		messageType = headers["message-type"]
	}

	return &EventInfo{
		EventType:   eventType,
		ContentType: contentType,
		MessageType: messageType,
		Payload:     message.Payload,
	}
}

// FormatSSE 将事件格式化为 Server-Sent Events 格式
// 参数 eventType 为事件类型
// 参数 data 为事件数据
// 返回 SSE 格式字符串
func FormatSSE(eventType string, data interface{}) string {
	jsonData, _ := json.Marshal(data)
	return fmt.Sprintf("event: %s\ndata: %s\n\n", eventType, string(jsonData))
}

// BuildMessageStart 构建 message_start SSE 事件
// 参数 conversationID 为会话 ID
// 参数 model 为模型名称
// 参数 inputTokens 为输入 token 数量
// 返回 SSE 格式的事件字符串
func BuildMessageStart(conversationID string, model string, inputTokens int) string {
	data := map[string]interface{}{
		"type": "message_start",
		"message": map[string]interface{}{
			"id":            conversationID,
			"type":          "message",
			"role":          "assistant",
			"content":       []interface{}{},
			"model":         model,
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage": map[string]int{
				"input_tokens":  inputTokens,
				"output_tokens": 0,
			},
		},
	}
	return FormatSSE("message_start", data)
}

// BuildContentBlockStart 构建 content_block_start SSE 事件
// 参数 index 为内容块索引
// 参数 blockType 为内容块类型（如 "text", "thinking" 或 "tool_use"）
// 返回 SSE 格式的事件字符串
func BuildContentBlockStart(index int, blockType string) string {
	contentBlock := map[string]interface{}{"type": blockType}
	if blockType == "text" || blockType == "thinking" {
		contentBlock["text"] = ""
	}

	data := map[string]interface{}{
		"type":          "content_block_start",
		"index":         index,
		"content_block": contentBlock,
	}
	return FormatSSE("content_block_start", data)
}

// BuildContentBlockDelta 构建 content_block_delta SSE 事件（文本增量）
// 参数 index 为内容块索引
// 参数 text 为增量文本内容
// 返回 SSE 格式的事件字符串
func BuildContentBlockDelta(index int, text string) string {
	data := map[string]interface{}{
		"type":  "content_block_delta",
		"index": index,
		"delta": map[string]string{
			"type": "text_delta",
			"text": text,
		},
	}
	return FormatSSE("content_block_delta", data)
}

// BuildContentBlockStop 构建 content_block_stop SSE 事件
// 参数 index 为内容块索引
// 返回 SSE 格式的事件字符串
func BuildContentBlockStop(index int) string {
	data := map[string]interface{}{
		"type":  "content_block_stop",
		"index": index,
	}
	return FormatSSE("content_block_stop", data)
}

// BuildPing 构建 ping SSE 事件，用于保持连接活跃
// 返回 SSE 格式的 ping 事件字符串
func BuildPing() string {
	data := map[string]string{"type": "ping"}
	return FormatSSE("ping", data)
}

// BuildMessageStop 构建 message_delta 和 message_stop SSE 事件
// 参数 inputTokens 为输入 token 数量
// 参数 outputTokens 为输出 token 数量
// 参数 stopReason 为停止原因（可为 nil）
// 返回组合的 SSE 格式事件字符串
func BuildMessageStop(inputTokens int, outputTokens int, stopReason *string) string {
	reason := "end_turn"
	if stopReason != nil {
		reason = *stopReason
	}

	deltaData := map[string]interface{}{
		"type": "message_delta",
		"delta": map[string]interface{}{
			"stop_reason":   reason,
			"stop_sequence": nil,
		},
		"usage": map[string]int{
			"output_tokens": outputTokens,
		},
	}
	deltaEvent := FormatSSE("message_delta", deltaData)

	stopData := map[string]string{"type": "message_stop"}
	stopEvent := FormatSSE("message_stop", stopData)

	return deltaEvent + stopEvent
}

// BuildToolUseStart 构建 tool_use 类型的 content_block_start SSE 事件
// 参数 index 为内容块索引
// 参数 toolUseID 为工具使用 ID
// 参数 toolName 为工具名称
// 返回 SSE 格式的事件字符串
func BuildToolUseStart(index int, toolUseID string, toolName string) string {
	data := map[string]interface{}{
		"type":  "content_block_start",
		"index": index,
		"content_block": map[string]interface{}{
			"type":  "tool_use",
			"id":    toolUseID,
			"name":  toolName,
			"input": map[string]interface{}{},
		},
	}
	return FormatSSE("content_block_start", data)
}

// BuildToolUseInputDelta 构建 tool_use 的 input_json_delta SSE 事件
// 参数 index 为内容块索引
// 参数 inputJSONDelta 为 JSON 增量内容
// 返回 SSE 格式的事件字符串
func BuildToolUseInputDelta(index int, inputJSONDelta string) string {
	data := map[string]interface{}{
		"type":  "content_block_delta",
		"index": index,
		"delta": map[string]string{
			"type":         "input_json_delta",
			"partial_json": inputJSONDelta,
		},
	}
	return FormatSSE("content_block_delta", data)
}
