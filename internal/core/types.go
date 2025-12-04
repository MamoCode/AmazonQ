package core

// ClaudeMessage Claude 消息结构，表示单条对话消息
type ClaudeMessage struct {
	Role    string      `json:"role"`    // 角色：user 或 assistant
	Content interface{} `json:"content"` // 内容：string 或 []ContentBlock
}

// ContentBlock 消息内容块，可以是文本、图片或工具使用
type ContentBlock struct {
	Type      string                 `json:"type"`              // 类型：text, image, tool_use, tool_result
	Text      string                 `json:"text,omitempty"`    // 文本内容
	Source    *ImageSource           `json:"source,omitempty"`  // 图片源
	ID        string                 `json:"id,omitempty"`      // 工具使用 ID
	Name      string                 `json:"name,omitempty"`    // 工具名称
	Input     map[string]interface{} `json:"input,omitempty"`   // 工具输入
	ToolUseID string                 `json:"tool_use_id,omitempty"` // 工具结果对应的工具使用 ID
	Status    string                 `json:"status,omitempty"`  // 工具执行状态
}

// ImageSource 图片源定义
type ImageSource struct {
	Type      string `json:"type"`                  // 类型：base64
	MediaType string `json:"media_type,omitempty"`  // MIME 类型
	Data      string `json:"data,omitempty"`        // Base64 编码的图片数据
}

// ClaudeTool Claude 工具定义
type ClaudeTool struct {
	Name        string                 `json:"name"`                  // 工具名称
	Description string                 `json:"description,omitempty"` // 工具描述
	InputSchema map[string]interface{} `json:"input_schema"`          // JSON Schema 格式的输入定义
}

// ThinkingConfig Thinking 配置结构（符合 Claude API 规范）
type ThinkingConfig struct {
	Type         string `json:"type"`                    // 类型：enabled 或其他
	BudgetTokens int    `json:"budget_tokens,omitempty"` // 思考预算 token 数
}

// ClaudeRequest Claude API 请求结构
type ClaudeRequest struct {
	Model       string          `json:"model"`                 // 模型名称
	Messages    []ClaudeMessage `json:"messages"`              // 消息列表
	MaxTokens   int             `json:"max_tokens"`            // 最大生成 token 数
	Temperature *float64        `json:"temperature,omitempty"` // 温度参数
	Tools       []ClaudeTool    `json:"tools,omitempty"`       // 可用工具列表
	Stream      bool            `json:"stream"`                // 是否流式响应
	System      interface{}     `json:"system,omitempty"`      // 系统提示：string 或 []SystemBlock
	Thinking    *ThinkingConfig `json:"thinking,omitempty"`    // Thinking 配置
}

// SystemBlock 系统提示块
type SystemBlock struct {
	Type string `json:"type"` // 类型：通常为 "text"
	Text string `json:"text"` // 系统提示文本
}

// AmazonQRequest Amazon Q API 请求结构
type AmazonQRequest struct {
	ConversationState ConversationState `json:"conversationState"` // 会话状态
}

// ConversationState 会话状态信息
type ConversationState struct {
	ConversationID  string          `json:"conversationId"`  // 会话 ID
	History         []HistoryEntry  `json:"history"`         // 历史消息
	CurrentMessage  CurrentMessage  `json:"currentMessage"`  // 当前消息
	ChatTriggerType string          `json:"chatTriggerType"` // 触发类型：MANUAL
}

// HistoryEntry 历史记录条目，可以是用户消息或助手响应
type HistoryEntry struct {
	UserInputMessage        *UserInputMessage        `json:"userInputMessage,omitempty"`        // 用户消息
	AssistantResponseMessage *AssistantResponseMessage `json:"assistantResponseMessage,omitempty"` // 助手响应
}

// CurrentMessage 当前消息包装
type CurrentMessage struct {
	UserInputMessage UserInputMessage `json:"userInputMessage"` // 用户输入消息
}

// UserInputMessage 用户输入消息结构
type UserInputMessage struct {
	Content                 string                  `json:"content"`                  // 消息内容
	UserInputMessageContext UserInputMessageContext `json:"userInputMessageContext"` // 消息上下文
	Origin                  string                  `json:"origin"`                  // 来源：CLI
	ModelID                 string                  `json:"modelId,omitempty"`       // 模型 ID
	Images                  []AmazonQImage          `json:"images,omitempty"`        // 图片列表
}

// UserInputMessageContext 用户输入消息上下文
type UserInputMessageContext struct {
	EnvState    EnvState       `json:"envState"`              // 环境状态
	Tools       []AmazonQTool  `json:"tools,omitempty"`       // 可用工具
	ToolResults []ToolResult   `json:"toolResults,omitempty"` // 工具执行结果
}

// EnvState 环境状态信息
type EnvState struct {
	OperatingSystem         string `json:"operatingSystem"`         // 操作系统
	CurrentWorkingDirectory string `json:"currentWorkingDirectory"` // 当前工作目录
}

// AmazonQTool Amazon Q 工具定义
type AmazonQTool struct {
	ToolSpecification ToolSpecification `json:"toolSpecification"` // 工具规范
}

// ToolSpecification 工具规范详情
type ToolSpecification struct {
	Name        string                 `json:"name"`        // 工具名称
	Description string                 `json:"description"` // 工具描述
	InputSchema map[string]interface{} `json:"inputSchema"` // 输入 Schema
}

// ToolResult 工具执行结果
type ToolResult struct {
	ToolUseID string              `json:"toolUseId"`         // 工具使用 ID
	Content   []ToolResultContent `json:"content"`           // 结果内容
	Status    string              `json:"status,omitempty"`  // 执行状态：success/error
}

// ToolResultContent 工具结果内容
type ToolResultContent struct {
	Text string `json:"text"` // 文本内容
}

// AmazonQImage Amazon Q 图片定义
type AmazonQImage struct {
	Format string      `json:"format"` // 图片格式：png, jpeg 等
	Source ImageBytes  `json:"source"` // 图片数据源
}

// ImageBytes 图片字节数据
type ImageBytes struct {
	Bytes string `json:"bytes"` // Base64 编码的图片数据
}

// AssistantResponseMessage 助手响应消息
type AssistantResponseMessage struct {
	MessageID string         `json:"messageId"`          // 消息 ID
	Content   string         `json:"content"`            // 响应内容
	ToolUses  []ToolUse      `json:"toolUses,omitempty"` // 工具使用列表
}

// ToolUse 工具使用记录
type ToolUse struct {
	ToolUseID string                 `json:"toolUseId"` // 工具使用 ID
	Name      string                 `json:"name"`      // 工具名称
	Input     map[string]interface{} `json:"input"`     // 工具输入参数
}
