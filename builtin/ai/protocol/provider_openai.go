// provider_openai.go — OpenAI 兼容 API 的 Provider 实现。
//
// 本文件实现 Provider 接口，兼容 /v1/chat/completions 格式的所有 API：
//   - OpenAI 官方 API（GPT-4o, GPT-4o-mini 等）
//   - DeepSeek Chat API
//   - 月之暗面 Kimi、零一万物 Yi
//   - Groq、Together AI
//   - vLLM、Ollama 等本地部署
//
// 支持流式（SSE）和非流式两种调用方式。
// 流式实现处理 OpenAI 格式的工具调用 delta 分片合并（mergeOrAppendToolCall）。

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

// openaiClient 实现 Provider 接口，兼容 OpenAI Chat Completions API。
//
// 支持的 API 格式：
//   - OpenAI 官方 API（GPT-4o, GPT-4o-mini 等）
//   - DeepSeek Chat API
//   - 月之暗面 Kimi API
//   - 零一万物 Yi API
//   - Groq API
//   - Together AI API
//   - vLLM / Ollama 等本地部署的 OpenAI 兼容接口
//
// 所有兼容 /v1/chat/completions 格式的服务均可使用。
type openaiClient struct {
	baseURL      string
	apiKey       string
	model        string
	maxTokens    int
	apiTimeout   time.Duration
	maxRetries   int
	includeUsage bool
	httpClient   *http.Client
}

// NewOpenAIProvider 按线格式参数构造 OpenAI 兼容提供商。
func NewOpenAIProvider(opts ProviderOptions) (Provider, error) {
	baseURL := opts.BaseURL
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	baseURL = strings.TrimRight(baseURL, "/")

	return &openaiClient{
		baseURL:      baseURL,
		apiKey:       opts.APIKey,
		model:        opts.Model,
		maxTokens:    opts.MaxTokens,
		apiTimeout:   opts.APITimeout,
		maxRetries:   opts.MaxRetries,
		includeUsage: opts.IncludeUsage,
		httpClient:   newHTTPClient(),
	}, nil
}

// --- 请求/响应结构体 ---

// openaiContentPart OpenAI 多模态内容片段（用于 vision / audio）。
type openaiContentPart struct {
	Type     string            `json:"type"`                  // "text" / "image_url" / "input_audio"
	Text     string            `json:"text,omitempty"`        // type=text
	ImageURL *openaiImageURL   `json:"image_url,omitempty"`   // type=image_url
	Audio    *openaiAudioInput `json:"input_audio,omitempty"` // type=input_audio
}

type openaiImageURL struct {
	URL string `json:"url"` // HTTP URL 或 data:{mime};base64,{data}
}

type openaiAudioInput struct {
	Data   string `json:"data"`   // base64 编码的音频数据
	Format string `json:"format"` // "wav" / "mp3" / "pcm"
}

// openaiMessageContent 适配 OpenAI content 字段的双重格式：
//
//   - 纯文字请求 / API 返回： "content": "hello"
//   - 多模态请求：           "content": [{"type":"text","text":"hello"}, ...]
//
// Marshal 时根据 hasParts 自动切换；Unmarshal 时兼容两种格式。
type openaiMessageContent struct {
	text  string
	parts []openaiContentPart
}

func (c *openaiMessageContent) MarshalJSON() ([]byte, error) {
	if c == nil {
		return json.Marshal(nil)
	}
	if len(c.parts) > 0 {
		return json.Marshal(c.parts)
	}
	return json.Marshal(c.text)
}

func (c *openaiMessageContent) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		c.text = s
		c.parts = nil
		return nil
	}
	if err := json.Unmarshal(data, &c.parts); err == nil {
		return nil
	}
	// 容错：部分兼容网关会以单个对象（而非数组）返回多模态 content。
	var one openaiContentPart
	if err := json.Unmarshal(data, &one); err == nil {
		c.parts = []openaiContentPart{one}
		return nil
	}
	return json.Unmarshal(data, &c.parts)
}

// String 返回纯文本内容：plain string 格式直接返回 text；
// 多模态数组格式拼接全部 text 片段（兼容图像输出模型的文字+图片混合响应）。
func (c *openaiMessageContent) String() string {
	if c == nil {
		return ""
	}
	if c.text != "" {
		return c.text
	}
	var b strings.Builder
	for _, p := range c.parts {
		if p.Type == "text" && p.Text != "" {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

type openaiChatMessage struct {
	Role       string                `json:"role"`
	Content    *openaiMessageContent `json:"content,omitempty"`
	ToolCalls  []OpenAIToolCall      `json:"tool_calls,omitempty"`
	ToolCallID string                `json:"tool_call_id,omitempty"`
}

type openaiChatRequest struct {
	Model       string              `json:"model"`
	Messages    []openaiChatMessage `json:"messages"`
	Tools       []OpenAITool        `json:"tools,omitempty"`
	Temperature float64             `json:"temperature,omitempty"`
	TopP        float64             `json:"top_p,omitempty"`
	MaxTokens   int                 `json:"max_tokens,omitempty"`
	// ReasoningEffort 思考程度（reasoning_effort）。omitempty 保证未配置时
	// 完全不出现该字段，兼容不认识它的网关。
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
	Stream          bool   `json:"stream,omitempty"`
	StreamOptions   *struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options,omitempty"`
}

type openaiUsageBody struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	// DeepSeek（及部分兼容端点）直接给出缓存命中/未命中的输入 token 数。
	PromptCacheHitTokens  int `json:"prompt_cache_hit_tokens"`
	PromptCacheMissTokens int `json:"prompt_cache_miss_tokens"`
	// OpenAI 把命中量放在 prompt_tokens_details.cached_tokens。
	PromptTokensDetails *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	// 思考型模型（DeepSeek 等）把推理 token 数单独放在
	// completion_tokens_details 里（仍然计入 completion_tokens）。
	CompletionTokensDetails *struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

// reasoningTokens 返回推理 token 数（提供商未单独返回时为 0）。
func (u *openaiUsageBody) reasoningTokens() int {
	if u.CompletionTokensDetails == nil {
		return 0
	}
	return u.CompletionTokensDetails.ReasoningTokens
}

// cachedPromptTokens 返回缓存命中的输入 token 数。
// 两种字段布局按可用性降级：OpenAI 的 prompt_tokens_details.cached_tokens
// 优先，其次 DeepSeek 的 prompt_cache_hit_tokens。
func (u *openaiUsageBody) cachedPromptTokens() int {
	if u.PromptTokensDetails != nil && u.PromptTokensDetails.CachedTokens > 0 {
		return u.PromptTokensDetails.CachedTokens
	}
	return u.PromptCacheHitTokens
}

type openaiChatChoice struct {
	Index        int               `json:"index"`
	Message      openaiChatMessage `json:"message"`
	FinishReason string            `json:"finish_reason"`
	Delta        openaiChatMessage `json:"delta"`
}

type openaiChatResponse struct {
	ID      string             `json:"id"`
	Object  string             `json:"object"`
	Choices []openaiChatChoice `json:"choices"`
	Usage   *openaiUsageBody   `json:"usage,omitempty"`
	Error   *openaiErrorBody   `json:"error,omitempty"`
}

type openaiErrorBody struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}

// --- 消息转换 ---

func toOpenAIMessages(msgs []Message) []openaiChatMessage {
	out := make([]openaiChatMessage, 0, len(msgs))
	for i, m := range msgs {
		ocm := openaiChatMessage{
			Role: string(m.Role),
		}
		if len(m.ContentParts) > 0 {
			ocm.Content = &openaiMessageContent{parts: buildOpenAIContentParts(m.ContentParts)}
		} else if m.Content != "" {
			ocm.Content = &openaiMessageContent{text: m.Content}
		}
		if m.Role == RoleTool {
			if m.ToolCallID == "" {
				continue
			}
			ocm.ToolCallID = m.ToolCallID
		}
		if len(m.ToolCalls) > 0 {
			for _, tc := range m.ToolCalls {
				if tc.Name == "" {
					continue
				}
				tcID := tc.ID
				if tcID == "" {
					tcID = fmt.Sprintf("call_%s_%d", tc.Name, i)
				}
				argsJSON, _ := json.Marshal(tc.Arguments)
				ocm.ToolCalls = append(ocm.ToolCalls, OpenAIToolCall{
					ID:   tcID,
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{
						Name:      tc.Name,
						Arguments: string(argsJSON),
					},
				})
			}
		}
		out = append(out, ocm)
	}
	return out
}

// buildOpenAIContentParts 将 ContentPart 列表转换为 OpenAI 格式的 content 数组。
//
// 输出格式：
//
//	[
//	  {"type":"text","text":"..."},
//	  {"type":"image_url","image_url":{"url":"data:image/jpeg;base64,..."}},
//	  {"type":"input_audio","input_audio":{"data":"base64...","format":"wav"}}
//	]
func buildOpenAIContentParts(parts []ContentPart) []openaiContentPart {
	out := make([]openaiContentPart, 0, len(parts))
	for _, p := range parts {
		switch p.Type {
		case ContentPartText:
			out = append(out, openaiContentPart{Type: "text", Text: p.Text})
		case ContentPartImage:
			data := p.Data
			if len(data) == 0 {
				continue
			}
			uri := fmt.Sprintf("data:%s;base64,%s", p.MimeType, base64.StdEncoding.EncodeToString(data))
			out = append(out, openaiContentPart{
				Type:     "image_url",
				ImageURL: &openaiImageURL{URL: uri},
			})
		case ContentPartAudio:
			data := p.Data
			if len(data) == 0 || p.AudioFormat == "" {
				continue
			}
			out = append(out, openaiContentPart{
				Type: "input_audio",
				Audio: &openaiAudioInput{
					Data:   base64.StdEncoding.EncodeToString(data),
					Format: p.AudioFormat,
				},
			})
		}
	}
	return out
}

// openaiOutputImageAttachments 提取模型输出内容中的图片片段（原生图像输出）。
//
// parts 为 content 数组中的片段（模型可能以多模态格式返回图片）；
// extraImages 为部分兼容网关（如 OpenRouter）在 delta.images 字段携带的
// 图片。data URI 解码为二进制直传，远程 URL 原样透传给平台下载。
func openaiOutputImageAttachments(content *openaiMessageContent, extraImages []openaiContentPart) []platform.Attachment {
	var parts []openaiContentPart
	if content != nil {
		parts = append(parts, content.parts...)
	}
	parts = append(parts, extraImages...)

	var out []platform.Attachment
	for _, part := range parts {
		if part.Type != "image_url" || part.ImageURL == nil {
			continue
		}
		if att, ok := attachmentFromImageURI(part.ImageURL.URL); ok {
			out = append(out, att)
		}
	}
	return out
}

// attachmentFromImageURI 将模型输出的图片 URI（data URI 或远程 URL）转换为附件。
// 无法解析（非 http(s)/data URI、解码失败）时返回 ok=false，调用方静默跳过。
func attachmentFromImageURI(uri string) (platform.Attachment, bool) {
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return platform.Attachment{}, false
	}

	const dataPrefix = "data:"
	const base64Marker = ";base64,"
	if rest, ok := strings.CutPrefix(uri, dataPrefix); ok {
		before, after, ok := strings.Cut(rest, base64Marker)
		if !ok {
			return platform.Attachment{}, false
		}
		mimeType := before
		data, err := base64.StdEncoding.DecodeString(after)
		if err != nil || len(data) == 0 {
			return platform.Attachment{}, false
		}
		att := platform.AttachmentFromData(platform.AttachmentKindImage, data)
		att.MimeType = mimeType
		return att, true
	}

	if !strings.HasPrefix(uri, "http://") && !strings.HasPrefix(uri, "https://") {
		return platform.Attachment{}, false
	}
	return platform.AttachmentFromURL(platform.AttachmentKindImage, uri), true
}

// --- 非流式调用 ---

func (c *openaiClient) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	body := openaiChatRequest{
		Model:           RequestModel(c.model, req.Model),
		Messages:        toOpenAIMessages(req.Messages),
		Temperature:     req.Temperature,
		TopP:            req.TopP,
		MaxTokens:       requestMaxTokens(c.maxTokens, req.MaxTokens),
		ReasoningEffort: req.ReasoningEffort,
	}
	if len(req.Tools) > 0 {
		body.Tools = ToOpenAITools(req.Tools)
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("ai: marshal request: %w", err)
	}

	return c.doChatRequest(ctx, payload)
}

func (c *openaiClient) doChatRequest(ctx context.Context, payload []byte) (*ChatResponse, error) {
	resp, err := executeWithRetry(ctx, c.maxRetries, c.apiTimeout, func() (*http.Request, error) {
		req, err := http.NewRequest(http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(payload))
		if err != nil {
			return nil, fmt.Errorf("ai: create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		return req, nil
	}, c.httpClient)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return c.processOpenAIResponse(resp)
}

// processOpenAIResponse 处理 OpenAI 非流式响应，不关闭 resp.Body。
func (c *openaiClient) processOpenAIResponse(resp *http.Response) (*ChatResponse, error) {
	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		return nil, &APIError{StatusCode: resp.StatusCode, Body: string(errBody)}
	}

	var openaiResp openaiChatResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&openaiResp); err != nil {
		return nil, fmt.Errorf("ai: decode response: %w", err)
	}

	if len(openaiResp.Choices) == 0 {
		return nil, fmt.Errorf("ai: empty response")
	}

	choice := openaiResp.Choices[0]
	result := &ChatResponse{
		Content:     choice.Message.Content.String(),
		Attachments: openaiOutputImageAttachments(choice.Message.Content, nil),
	}

	if openaiResp.Usage != nil {
		result.Usage = &TokenUsage{
			PromptTokens:     openaiResp.Usage.PromptTokens,
			CompletionTokens: openaiResp.Usage.CompletionTokens,
			CachedTokens:     openaiResp.Usage.cachedPromptTokens(),
			ReasoningTokens:  openaiResp.Usage.reasoningTokens(),
		}
	}

	if len(choice.Message.ToolCalls) > 0 {
		tcs, err := ParseOpenAIToolCalls(choice.Message.ToolCalls)
		if err != nil {
			return nil, err
		}
		result.ToolCalls = tcs
	}

	return result, nil
}

// --- 流式调用 ---

func (c *openaiClient) ChatStream(ctx context.Context, req *ChatRequest) (<-chan StreamEvent, error) {
	ch := make(chan StreamEvent, 64)

	body := openaiChatRequest{
		Model:           RequestModel(c.model, req.Model),
		Messages:        toOpenAIMessages(req.Messages),
		Temperature:     req.Temperature,
		TopP:            req.TopP,
		MaxTokens:       requestMaxTokens(c.maxTokens, req.MaxTokens),
		ReasoningEffort: req.ReasoningEffort,
		Stream:          true,
	}
	// 请求流式 usage 统计（OpenAI 需显式开启；不兼容的网关可通过
	// include_usage=false 关闭，避免未知字段被拒）
	if c.includeUsage {
		body.StreamOptions = &struct {
			IncludeUsage bool `json:"include_usage"`
		}{IncludeUsage: true}
	}
	if len(req.Tools) > 0 {
		body.Tools = ToOpenAITools(req.Tools)
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("ai: marshal request: %w", err)
	}

	resp, err := c.doStreamRequest(ctx, c.baseURL+"/chat/completions", payload)
	if err != nil {
		return nil, err
	}

	go pumpSSE(ctx, resp, ch, newOpenAIStreamDecoder())

	return ch, nil
}

// doStreamRequest 执行流式 HTTP 请求，并在传输错误、429 限流或 5xx 响应时
// 按 maxRetries 配置重试（见 retry.go executeWithRetry）。
// 4xx 等客户端错误不重试，交由调用方以错误事件处理。
func (c *openaiClient) doStreamRequest(ctx context.Context, url string, payload []byte) (*http.Response, error) {
	return executeWithRetry(ctx, c.maxRetries, c.apiTimeout, func() (*http.Request, error) {
		req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
		if err != nil {
			return nil, fmt.Errorf("ai: create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		return req, nil
	}, c.httpClient)
}

// mergeOrAppendToolCall 合并流式分片传来的工具调用。
//
// OpenAI 流式 API 会将工具调用参数分多次 delta 传输，通过 index
// 标识属于哪个工具调用。后续分片可能不含 id 和 name，只有 arguments。
//
// 合并策略：
//   - 按 index 匹配同一位置的工具调用
//   - 后续分片会补充 id（如第一片无 id 第二片才有）、name、arguments
//   - arguments 累加拼接（JSON 片段）
func mergeOrAppendToolCall(calls *[]OpenAIToolCall, tc OpenAIToolCall) {
	for i := range *calls {
		if (*calls)[i].Index == tc.Index {
			if tc.ID != "" {
				(*calls)[i].ID = tc.ID
			}
			if tc.Function.Name != "" {
				(*calls)[i].Function.Name = tc.Function.Name
			}
			(*calls)[i].Function.Arguments += tc.Function.Arguments
			return
		}
	}
	*calls = append(*calls, tc)
}
