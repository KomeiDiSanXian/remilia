// provider_anthropic.go — Anthropic Messages API 的 Provider 实现。
//
// 本文件实现 Provider 接口，兼容 Anthropic Messages API：
//   - Claude Sonnet 4 (claude-sonnet-4-20250514)
//   - Claude 3.5 Sonnet (claude-3-5-sonnet-latest)
//   - Claude 3 Opus (claude-3-opus-latest)
//   - Claude 3 Haiku (claude-3-haiku-latest)
//
// 实现差异：
//   - Anthropic 的 system prompt 在顶级字段而非 messages 数组
//   - content 为 content block 数组而非简单字符串
//   - 工具调用通过 tool_use content block 表示
//   - 流式事件类型不同（content_block_start / content_block_delta / content_block_stop）
//   - tool_result 通过 content block 回填（非独立 tool 角色消息）

package protocol

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// anthropicClient 实现 Provider 接口，兼容 Anthropic Messages API。
//
// 支持的模型：
//   - Claude Sonnet 4 (claude-sonnet-4-20250514)
//   - Claude 3.5 Sonnet (claude-3-5-sonnet-latest)
//   - Claude 3 Opus (claude-3-opus-latest)
//   - Claude 3 Haiku (claude-3-haiku-latest)
//
// API 文档：https://docs.anthropic.com/en/api/messages
type anthropicClient struct {
	baseURL    string
	apiKey     string
	model      string
	maxTokens  int
	apiTimeout time.Duration
	maxRetries int
	httpClient *http.Client
}

// NewAnthropicProvider 按线格式参数构造 Anthropic 提供商。
func NewAnthropicProvider(opts ProviderOptions) (Provider, error) {
	baseURL := opts.BaseURL
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	baseURL = strings.TrimRight(baseURL, "/")

	return &anthropicClient{
		baseURL:    baseURL,
		apiKey:     opts.APIKey,
		model:      opts.Model,
		maxTokens:  opts.MaxTokens,
		apiTimeout: opts.APITimeout,
		maxRetries: opts.MaxRetries,
		httpClient: newHTTPClient(),
	}, nil
}

// --- Anthropic API 请求/响应结构 ---

// anthropicMessage 对应 Anthropic Messages API 的 content block 数组格式。
// 与 OpenAI 不同，Anthropic 的 content 是数组而非字符串。
type anthropicReqMessage struct {
	Role    string                     `json:"role"`
	Content []anthropicReqContentBlock `json:"content"`
}

// anthropicImgSrc Anthropic image content block 的 source 字段。
type anthropicImgSrc struct {
	Type      string `json:"type"`       // 固定 "base64"
	MediaType string `json:"media_type"` // "image/jpeg", "image/png", "image/gif", "image/webp"
	Data      string `json:"data"`       // base64 编码的图片数据
}

type anthropicReqContentBlock struct {
	Type   string           `json:"type"`             // "text" / "image" / "tool_use" / "tool_result"
	Text   string           `json:"text,omitempty"`   // type=text
	Source *anthropicImgSrc `json:"source,omitempty"` // type=image
	ID     string           `json:"id,omitempty"`     // tool_result/tool_use
	Name   string           `json:"name,omitempty"`   // tool_use 时填写工具名
	Input  any              `json:"input,omitempty"`  // tool_use 时填写参数
}

type anthropicChatRequest struct {
	Model       string                `json:"model"`
	MaxTokens   int                   `json:"max_tokens"`
	System      string                `json:"system,omitempty"`
	Messages    []anthropicReqMessage `json:"messages"`
	Tools       []AnthropicTool       `json:"tools,omitempty"`
	Temperature float64               `json:"temperature,omitempty"`
	TopP        float64               `json:"top_p,omitempty"`
	Stream      bool                  `json:"stream,omitempty"`
}

type anthropicUsageBody struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	// 前缀缓存用量。Anthropic 的 input_tokens 不含这两项：
	// 本次总输入 = input_tokens + cache_read + cache_creation。
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

// totalInputTokens 返回本次请求的输入 token 总量（含缓存命中与新写入部分）。
// 与 OpenAI 的 prompt_tokens 口径对齐（后者本就包含 cached_tokens），
// 使 CachedTokens/总输入 在两种提供商下都表示真实的缓存命中率。
func (u *anthropicUsageBody) totalInputTokens() int {
	return u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens
}

type anthropicChatResponse struct {
	ID         string                  `json:"id"`
	Type       string                  `json:"type"`
	Role       string                  `json:"role"`
	Content    []AnthropicContentBlock `json:"content"`
	StopReason string                  `json:"stop_reason"`
	Usage      *anthropicUsageBody     `json:"usage,omitempty"`
	Error      *anthropicErrorBody     `json:"error,omitempty"`
}

type anthropicErrorBody struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// --- 消息转换：内部 Message → Anthropic 格式 ---

func toAnthropicMessages(msgs []Message) []anthropicReqMessage {
	var out []anthropicReqMessage
	var current *anthropicReqMessage

	flush := func() {
		if current != nil && len(current.Content) > 0 {
			out = append(out, *current)
		}
	}

	for _, m := range msgs {
		switch m.Role {
		case RoleSystem:
			continue

		case RoleUser:
			// 连续 user 消息必须合并为一条：Anthropic 要求 roles 严格交替，
			// 拆开会以 400 拒绝（messages: roles must alternate）。
			if current != nil && current.Role == "user" {
				current.Content = append(current.Content, toAnthropicUserBlocks(m)...)
				continue
			}
			flush()
			current = &anthropicReqMessage{
				Role:    "user",
				Content: toAnthropicUserBlocks(m),
			}

		case RoleTool:
			block := anthropicReqContentBlock{
				Type: "tool_result",
				ID:   m.ToolCallID,
				Text: m.Content,
			}
			// 并行工具的多个 tool_result 必须全部放进紧随 assistant(tool_use)
			// 之后的同一条 user 消息里（官方 SDK 同样如此合并）；拆成相邻多条
			// user 消息会被 400 拒绝。
			if current != nil && current.Role == "user" {
				current.Content = append(current.Content, block)
				continue
			}
			flush()
			current = &anthropicReqMessage{
				Role:    "user",
				Content: []anthropicReqContentBlock{block},
			}

		case RoleAssistant:
			flush()
			blocks := []anthropicReqContentBlock{}
			if m.Content != "" {
				blocks = append(blocks, anthropicReqContentBlock{
					Type: "text",
					Text: m.Content,
				})
			}
			for _, tc := range m.ToolCalls {
				blocks = append(blocks, anthropicReqContentBlock{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Name,
					Input: tc.Arguments,
				})
			}
			current = &anthropicReqMessage{
				Role:    "assistant",
				Content: blocks,
			}
		}
	}
	flush()

	return out
}

// toAnthropicUserBlocks 将 Message 转换为 Anthropic user 消息的 content block 列表。
//
// 有 ContentParts 时：
//   - text → {"type":"text","text":"..."}
//   - image → {"type":"image","source":{"type":"base64","media_type":"{mime}","data":"{base64}"}}
//
// 无 ContentParts 时回退到 Content 字符串。
//
// Anthropic 不支持 audio，audio 类型的 ContentPart 被跳过。
func toAnthropicUserBlocks(m Message) []anthropicReqContentBlock {
	if len(m.ContentParts) == 0 {
		if m.Content == "" {
			return nil
		}
		return []anthropicReqContentBlock{{Type: "text", Text: m.Content}}
	}

	blocks := make([]anthropicReqContentBlock, 0, len(m.ContentParts))
	for _, p := range m.ContentParts {
		switch p.Type {
		case ContentPartText:
			blocks = append(blocks, anthropicReqContentBlock{Type: "text", Text: p.Text})
		case ContentPartImage:
			if len(p.Data) == 0 {
				continue
			}
			blocks = append(blocks, anthropicReqContentBlock{
				Type: "image",
				Source: &anthropicImgSrc{
					Type:      "base64",
					MediaType: p.MimeType,
					Data:      base64.StdEncoding.EncodeToString(p.Data),
				},
			})
		case ContentPartAudio:
			// Anthropic 不支持 audio 输入，跳过
			continue
		}
	}
	return blocks
}

// extractAnthropicSystem 从消息列表中提取 system prompt。
// Anthropic API 要求 system prompt 放在顶级字段，不在 messages 数组中。
func extractAnthropicSystem(msgs []Message) string {
	for _, m := range msgs {
		if m.Role == RoleSystem {
			return m.Content
		}
	}
	return ""
}

// attachmentFromAnthropicImage 将 Anthropic 响应中的 image content block
// 转换为附件。Source 缺失或 base64 解码失败时返回 ok=false，调用方静默跳过。
func attachmentFromAnthropicImage(src *anthropicImgSrc) (platform.Attachment, bool) {
	if src == nil || src.Data == "" {
		return platform.Attachment{}, false
	}
	data, err := base64.StdEncoding.DecodeString(src.Data)
	if err != nil || len(data) == 0 {
		return platform.Attachment{}, false
	}
	att := platform.AttachmentFromData(platform.AttachmentKindImage, data)
	att.MimeType = src.MediaType
	return att, true
}

// --- 非流式调用 ---

// defaultAnthropicMaxTokens Anthropic 侧的输出上限兜底。
// Anthropic 的 max_tokens 必填且 >=1：配置与请求都没给时序列化出 0 会被
// API 直接 400 拒绝（错误文案对用户还不可读），这里给协议级默认值。
const defaultAnthropicMaxTokens = 4096

func (c *anthropicClient) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	systemPrompt := extractAnthropicSystem(req.Messages)

	maxTokens := requestMaxTokens(c.maxTokens, req.MaxTokens)
	if maxTokens <= 0 {
		maxTokens = defaultAnthropicMaxTokens
	}
	body := anthropicChatRequest{
		Model:       RequestModel(c.model, req.Model),
		MaxTokens:   maxTokens,
		System:      systemPrompt,
		Messages:    toAnthropicMessages(req.Messages),
		Temperature: req.Temperature,
		TopP:        req.TopP,
	}
	if len(req.Tools) > 0 {
		body.Tools = ToAnthropicTools(req.Tools)
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("ai: anthropic marshal request: %w", err)
	}

	return c.doAnthropicChatRequest(ctx, payload)
}

func (c *anthropicClient) doAnthropicChatRequest(ctx context.Context, payload []byte) (*ChatResponse, error) {
	resp, err := executeWithRetry(ctx, c.maxRetries, c.apiTimeout, func() (*http.Request, error) {
		req, err := http.NewRequest(http.MethodPost, c.baseURL+"/v1/messages", bytes.NewReader(payload))
		if err != nil {
			return nil, fmt.Errorf("ai: anthropic create request: %w", err)
		}
		c.setHeaders(req)
		return req, nil
	}, c.httpClient)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return c.processAnthropicResponse(resp)
}

// processAnthropicResponse 处理 Anthropic 非流式响应，不关闭 resp.Body。
func (c *anthropicClient) processAnthropicResponse(resp *http.Response) (*ChatResponse, error) {
	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		return nil, &APIError{StatusCode: resp.StatusCode, Body: string(errBody)}
	}

	var anthropicResp anthropicChatResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&anthropicResp); err != nil {
		return nil, fmt.Errorf("ai: anthropic decode response: %w", err)
	}

	if anthropicResp.Error != nil {
		return nil, fmt.Errorf("ai: anthropic api error: %s: %s", anthropicResp.Error.Type, anthropicResp.Error.Message)
	}

	result := &ChatResponse{}
	for _, block := range anthropicResp.Content {
		switch block.Type {
		case "text":
			result.Content += block.Text
		case "image":
			// 原生图像输出（如带图像生成能力的模型）：转为附件随回复发送，
			// 避免被静默丢弃。
			if att, ok := attachmentFromAnthropicImage(block.Source); ok {
				result.Attachments = append(result.Attachments, att)
			}
		}
	}

	if anthropicResp.Usage != nil {
		result.Usage = &TokenUsage{
			PromptTokens:     anthropicResp.Usage.totalInputTokens(),
			CompletionTokens: anthropicResp.Usage.OutputTokens,
			CachedTokens:     anthropicResp.Usage.CacheReadInputTokens,
		}
	}

	if anthropicResp.StopReason == "tool_use" {
		result.ToolCalls = ParseAnthropicToolCalls(anthropicResp.Content)
	}

	return result, nil
}

// --- 流式调用 ---

func (c *anthropicClient) ChatStream(ctx context.Context, req *ChatRequest) (<-chan StreamEvent, error) {
	ch := make(chan StreamEvent, 64)

	systemPrompt := extractAnthropicSystem(req.Messages)

	streamMaxTokens := requestMaxTokens(c.maxTokens, req.MaxTokens)
	if streamMaxTokens <= 0 {
		streamMaxTokens = defaultAnthropicMaxTokens
	}
	body := anthropicChatRequest{
		Model:       RequestModel(c.model, req.Model),
		MaxTokens:   streamMaxTokens,
		System:      systemPrompt,
		Messages:    toAnthropicMessages(req.Messages),
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Stream:      true,
	}
	if len(req.Tools) > 0 {
		body.Tools = ToAnthropicTools(req.Tools)
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("ai: anthropic marshal request: %w", err)
	}

	resp, err := c.doStreamRequest(ctx, c.baseURL+"/v1/messages", payload)
	if err != nil {
		return nil, err
	}

	go pumpSSE(ctx, resp, ch, newAnthropicStreamDecoder())

	return ch, nil
}

// doStreamRequest 执行流式 HTTP 请求，并在传输错误或 5xx 响应时按
// maxRetries 配置重试。每次重试都重建请求体（http.Request.Body 是一次性读取的）。
// 4xx 等客户端错误不重试，交由调用方以错误事件处理。
// doStreamRequest 执行流式 HTTP 请求，并在传输错误、429 限流或 5xx 响应时
// 按 maxRetries 配置重试（见 retry.go executeWithRetry）。
// 4xx 等客户端错误不重试，交由调用方以错误事件处理。
func (c *anthropicClient) doStreamRequest(ctx context.Context, url string, payload []byte) (*http.Response, error) {
	return executeWithRetry(ctx, c.maxRetries, c.apiTimeout, func() (*http.Request, error) {
		req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
		if err != nil {
			return nil, fmt.Errorf("ai: anthropic create request: %w", err)
		}
		c.setHeaders(req)
		req.Header.Set("Accept", "text/event-stream")
		return req, nil
	}, c.httpClient)
}

// AnthropicAPIVersion 是 Anthropic Messages API 的版本头取值。
//
// 导出的原因是装配侧的健康探针也要发同一个头：版本号在协议实现与探针两处
// 各写一份时，升级协议很容易只改一处，探针就会以过期版本被端点拒绝。
const AnthropicAPIVersion = "2023-06-01"

// setHeaders 设置 Anthropic API 请求头（含 API 版本）。
func (c *anthropicClient) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", AnthropicAPIVersion)
}
