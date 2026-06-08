// Package ai provider.go — LLM 提供商抽象接口与消息类型定义。
//
// 本文件定义：
//   - Provider 接口：所有 LLM API 提供商需实现 Chat 和 ChatStream 方法
//   - NewProvider 工厂函数：根据 Config.Provider 选择对应实现
//   - 消息角色（Role）和常用常量
//   - ToolCall / Message / ChatRequest / ChatResponse 等核心数据类型
//   - StreamEvent / StreamEventType：流式事件类型定义
package ai

import (
	"context"
	"fmt"
	"time"
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
//
// LLM 在响应中通过 tool_calls 数组请求调用工具，
// 实现方需根据 ID 和 Name 执行对应工具，并将结果以 [RoleTool] 消息回填。
// ID 用于关联 tool_calls 和 tool 消息的 tool_call_id。
type ToolCall struct {
	// ID 工具调用唯一标识，用于匹配工具结果回填。
	// OpenAI/Anthropic 原生返回；为空的 ID 会在代码中自动生成。
	ID string
	// Name 要调用的工具名称，对应 ToolRegistry 中注册的 Tool.Name。
	Name string
	// Arguments 工具参数，由 LLM 根据工具 Parameters JSON Schema 生成。
	Arguments map[string]any
}

// ContentPartType 多模态内容片段的类型。
type ContentPartType string

const (
	ContentPartText  ContentPartType = "text"
	ContentPartImage ContentPartType = "image"
	ContentPartAudio ContentPartType = "audio"
)

// ContentPart 表示一条消息中的一个多模态内容片段。
//
// 一条 Message 可以包含多个 ContentPart（如文字+图片），按顺序发送给 LLM。
// 无 ContentParts 时回退到 Message.Content（向后兼容）。
//
// Data 和 AudioFormat 不持久化到 session（json:"-"），仅用于请求构建。
type ContentPart struct {
	Type ContentPartType `json:"type"`

	// Type=text 时使用
	Text string `json:"text,omitempty"`

	// Type=image 或 Type=audio 时使用
	SourceURL string `json:"source_url,omitempty"` // 原始下载 URL，仅用于缓存 key

	// 下载后的二进制数据（json:"-" 不持久化到 session）
	Data []byte `json:"-"`
	// MIME 类型，如 "image/jpeg"、"audio/wav"
	MimeType string `json:"mime_type,omitempty"`

	// Type=audio 时使用，如 "wav"、"mp3"（OpenAI input_audio format）
	AudioFormat string `json:"audio_format,omitempty"`
}

// cachedContent 内存中缓存的附件二进制数据。
type cachedContent struct {
	Data        []byte
	MimeType    string
	AudioFormat string
	ExpireAt    time.Time
}

// Message 表示对话中的一条消息，对应 LLM 的 messages 数组中的一项。
//
// 按 Role 不同，字段含义不同：
//   - RoleSystem: Content 为系统提示词，ToolCalls/ToolCallID 为空
//   - RoleUser: Content 为用户消息（ContentParts 优先于 Content）
//   - RoleAssistant: Content 为 AI 回复，ToolCalls 为 AI 请求的工具调用（可选）
//   - RoleTool: Content 为工具执行结果，ToolCallID 对应 Assistant 消息中的 ToolCall.ID
type Message struct {
	Role         Role          `json:"role"`
	Content      string        `json:"content"`
	ContentParts []ContentPart `json:"content_parts,omitempty"`
	ToolCalls    []ToolCall    `json:"tool_calls,omitempty"`
	ToolCallID   string        `json:"tool_call_id,omitempty"`
}

// ChatRequest 发送给 LLM 的聊天请求。
type ChatRequest struct {
	Model       string
	Messages    []Message
	Tools       []Tool
	Temperature float64
	TopP        float64
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
	Content  string    // StreamEventText 时有效
	ToolCall *ToolCall // StreamEventToolCall 时有效
	Err      error     // StreamEventError 时有效
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
