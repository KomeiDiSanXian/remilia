// provider.go — LLM 提供商抽象接口与消息类型定义。
//
// 本文件定义：
//   - Provider 接口：所有 LLM API 提供商需实现 Chat 和 ChatStream 方法
//   - 消息角色（Role）和常用常量
//   - ToolCall / Message / ChatRequest / ChatResponse 等核心数据类型
//   - StreamEvent / StreamEventType：流式事件类型定义
//
// 提供商实例的选择与指标包装在装配侧（builtin/ai 的 newProvider）。
package protocol

import (
	"context"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
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
	// Timestamp 消息到达时间（用于历史图片保留时间窗判定；0 值表示未知）。
	Timestamp time.Time `json:"timestamp"`
	// Internal 标记系统注入的内部指令（反思/重规划/校验修正），它们虽以
	// user 角色进入消息序列（各提供商对 user/assistant 交替有硬性约束），
	// 但不是用户真实发言。检索/记忆抽取选取"最后一条用户消息"时须跳过，
	// 否则查询词会变成内部指令。
	Internal bool `json:"internal,omitempty"`
}

// ChatRequest 发送给 LLM 的聊天请求。
type ChatRequest struct {
	Model       string
	Messages    []Message
	Tools       []ToolSpec
	Temperature float64
	TopP        float64
	MaxTokens   int
	// ReasoningEffort 思考程度（OpenAI 兼容的 reasoning_effort：
	// none/minimal/low/medium/high）。空串表示不携带该字段，由端点决定
	// 默认行为——不支持该字段的网关因此不受影响。
	ReasoningEffort string
	Stream          bool
}

// TokenUsage 一次 LLM 调用的 token 用量（由提供商响应解析；缺失时为 nil）。
type TokenUsage struct {
	PromptTokens     int
	CompletionTokens int
	// CachedTokens 命中提示词前缀缓存的输入 token 数（提供商返回时才有值）。
	// OpenAI 兼容端点取 prompt_tokens_details.cached_tokens（DeepSeek 走
	// prompt_cache_hit_tokens），Anthropic 取 cache_read_input_tokens。
	// 用于量化前缀缓存命中率（CachedTokens/PromptTokens），验证提示词
	// 结构改动（稳定前缀长度、工具顺序）是否真的提高了复用。
	CachedTokens int
	// ReasoningTokens 思考型模型的推理 token 数（已计入 CompletionTokens；
	// 提供商未单独返回时为 0）。多数端点把推理过程也算进 max_tokens 预算，
	// 该值用于识别"推理耗尽预算、可见正文为空"的截断。
	ReasoningTokens int
}

// ChatResponse 非流式聊天的响应。
type ChatResponse struct {
	Content   string
	ToolCalls []ToolCall
	// Attachments 模型在响应中直接返回的媒体附件（如原生图像输出模型的
	// base64 图片或图片 URL）。纯文本输出型模型始终为空。
	Attachments []platform.Attachment
	// Usage token 用量（提供商返回时填充，可能为 nil）。
	Usage *TokenUsage
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
	// StreamEventAttachment 附件片段（模型直接输出的图片等媒体）。
	StreamEventAttachment
	// StreamEventReasoning 推理内容片段（思考型模型，如 DeepSeek 的
	// reasoning_content）。编排层只用于计数与日志，不进入回复正文。
	StreamEventReasoning
)

// StreamEvent 流式事件，由 ChatStream 通过 channel 推送。
type StreamEvent struct {
	Type       StreamEventType
	Content    string               // StreamEventText / StreamEventReasoning 时有效
	ToolCall   *ToolCall            // StreamEventToolCall 时有效
	Attachment *platform.Attachment // StreamEventAttachment 时有效
	Err        error                // StreamEventError 时有效
	// Usage token 用量（StreamEventDone 时有效；提供商未返回时为 nil）。
	Usage *TokenUsage
	// FinishReason 提供商给出的结束原因（StreamEventDone 时有效；缺失为空串）。
	// 供编排层区分"正常 stop"与"length/content_filter 导致的空回复"，
	// 空回复不再被静默当成成功。
	FinishReason string
}

// Provider LLM 提供商抽象接口。
// 所有 LLM API 提供商需实现此接口。
type Provider interface {
	// Chat 非流式聊天。
	Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
	// ChatStream 流式聊天，返回一个接收流式事件的 channel。
	ChatStream(ctx context.Context, req *ChatRequest) (<-chan StreamEvent, error)
}

// ProviderOptions 构造具体提供商所需的线格式参数。
//
// 协议层只认这些参数，不认识插件配置（builtin/ai/config）：装配侧做一次字段
// 映射，协议适配器因此可以脱离插件单独构造与测试，签名也直接暴露真实依赖。
type ProviderOptions struct {
	// BaseURL API 地址；空值时由各提供商取自己的默认端点。
	BaseURL string
	// APIKey 鉴权密钥。
	APIKey string
	// Model 默认模型名。
	Model string
	// MaxTokens 请求未指定 max_tokens 时的默认输出上限。
	MaxTokens int
	// APITimeout 单次请求超时；<=0 表示不额外设限。
	APITimeout time.Duration
	// MaxRetries 传输层重试次数。
	MaxRetries int
	// IncludeUsage 是否在 OpenAI 兼容请求里要求返回 usage。
	IncludeUsage bool
}

// RequestModel 优先使用请求级模型名，为空时回退到客户端默认模型。
func RequestModel(defaultModel, reqModel string) string {
	if reqModel != "" {
		return reqModel
	}
	return defaultModel
}

// requestMaxTokens 优先使用请求级 max_tokens，非正值时回退到客户端默认值。
func requestMaxTokens(defaultTokens, reqTokens int) int {
	if reqTokens > 0 {
		return reqTokens
	}
	return defaultTokens
}
