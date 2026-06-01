package ai

import (
	"context"
	"fmt"
)

// Role 消息角色类型。
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// ToolCall 表示 LLM 发起的一个工具调用请求。
type ToolCall struct {
	// ID 工具调用唯一标识，用于匹配工具结果回填。
	ID string
	// Name 要调用的工具名称。
	Name string
	// Arguments 工具参数，由 LLM 根据工具 schema 生成。
	Arguments map[string]any
}

// Message 表示对话中的一条消息。
type Message struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ChatRequest 发送给 LLM 的聊天请求。
type ChatRequest struct {
	Model       string
	Messages    []Message
	Tools       []Tool
	Temperature float64
	MaxTokens   int
	Stream      bool
}

// ChatResponse 非流式聊天的响应。
type ChatResponse struct {
	Content   string
	ToolCalls []ToolCall
}

// StreamEventType 流式事件的类型。
type StreamEventType int

const (
	// StreamEventText 文本片段。
	StreamEventText StreamEventType = iota
	// StreamEventToolCall 工具调用（流式解析完成后一次性发出）。
	StreamEventToolCall
	// StreamEventDone 流结束。
	StreamEventDone
	// StreamEventError 流式处理出错。
	StreamEventError
)

// StreamEvent 流式事件，由 ChatStream 通过 channel 推送。
type StreamEvent struct {
	Type     StreamEventType
	Content  string  // StreamEventText 时有效
	ToolCall *ToolCall // StreamEventToolCall 时有效
	Err      error   // StreamEventError 时有效
}

// Provider LLM 提供商抽象接口。
// 所有 LLM API 提供商需实现此接口。
type Provider interface {
	// Chat 非流式聊天。
	Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
	// ChatStream 流式聊天，返回一个接收流式事件的 channel。
	ChatStream(ctx context.Context, req *ChatRequest) (<-chan StreamEvent, error)
}

// NewProvider 根据配置创建对应的 LLM 提供商实例。
func NewProvider(cfg *Config) (Provider, error) {
	switch cfg.Provider {
	case "openai", "":
		return NewOpenAIProvider(cfg)
	case "anthropic":
		return NewAnthropicProvider(cfg)
	default:
		return nil, fmt.Errorf("ai: unknown provider %q (supported: openai, anthropic)", cfg.Provider)
	}
}
