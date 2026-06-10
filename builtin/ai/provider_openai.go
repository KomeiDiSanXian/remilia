// Package ai provider_openai.go — OpenAI 兼容 API 的 Provider 实现。
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
package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
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
	baseURL    string
	apiKey     string
	model      string
	maxTokens  int
	apiTimeout time.Duration
	maxRetries int
	httpClient *http.Client
}

func NewOpenAIProvider(cfg *Config) (Provider, error) {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	baseURL = strings.TrimRight(baseURL, "/")

	return &openaiClient{
		baseURL:    baseURL,
		apiKey:     cfg.APIKey,
		model:      cfg.Model,
		maxTokens:  cfg.MaxTokens,
		apiTimeout: cfg.APITimeout,
		maxRetries: cfg.MaxRetries,
		httpClient: &http.Client{
			Transport: &http.Transport{
				DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
				TLSHandshakeTimeout:   10 * time.Second,
				ResponseHeaderTimeout: 30 * time.Second,
				MaxIdleConns:          100,
				MaxIdleConnsPerHost:   10,
				IdleConnTimeout:       90 * time.Second,
			},
		},
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
	return json.Unmarshal(data, &c.parts)
}

func (c *openaiMessageContent) String() string {
	if c == nil {
		return ""
	}
	return c.text
}

func newOpenAITextContent(text string) *openaiMessageContent {
	return &openaiMessageContent{text: text}
}

func newOpenAIMultiContent(parts []openaiContentPart) *openaiMessageContent {
	return &openaiMessageContent{parts: parts}
}

type openaiChatMessage struct {
	Role       string                `json:"role"`
	Content    *openaiMessageContent `json:"content,omitempty"`
	ToolCalls  []openaiToolCall      `json:"tool_calls,omitempty"`
	ToolCallID string                `json:"tool_call_id,omitempty"`
}

type openaiChatRequest struct {
	Model       string              `json:"model"`
	Messages    []openaiChatMessage `json:"messages"`
	Tools       []openaiTool        `json:"tools,omitempty"`
	Temperature float64             `json:"temperature,omitempty"`
	TopP        float64             `json:"top_p,omitempty"`
	MaxTokens   int                 `json:"max_tokens,omitempty"`
	Stream      bool                `json:"stream,omitempty"`
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
			ocm.Content = newOpenAIMultiContent(buildOpenAIContentParts(m.ContentParts))
		} else if m.Content != "" {
			ocm.Content = newOpenAITextContent(m.Content)
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
				ocm.ToolCalls = append(ocm.ToolCalls, openaiToolCall{
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

// --- 非流式调用 ---

func (c *openaiClient) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	body := openaiChatRequest{
		Model:       c.model,
		Messages:    toOpenAIMessages(req.Messages),
		Temperature: req.Temperature,
		TopP:        req.TopP,
		MaxTokens:   c.maxTokens,
	}
	if len(req.Tools) > 0 {
		body.Tools = toOpenAITools(req.Tools)
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("ai: marshal request: %w", err)
	}

	return c.doChatRequest(ctx, payload)
}

func (c *openaiClient) doChatRequest(ctx context.Context, payload []byte) (*ChatResponse, error) {
	chatCtx := ctx
	timeout := c.apiTimeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	var cancel context.CancelFunc
	chatCtx, cancel = context.WithTimeout(ctx, timeout)
	defer cancel()

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-chatCtx.Done():
				return nil, chatCtx.Err()
			case <-time.After(time.Duration(attempt) * time.Second):
			}
		}

		httpReq, err := http.NewRequestWithContext(chatCtx, "POST", c.baseURL+"/chat/completions", bytes.NewReader(payload))
		if err != nil {
			return nil, fmt.Errorf("ai: create request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			lastErr = fmt.Errorf("ai: http request: %w", err)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			errBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			lastErr = fmt.Errorf("ai: api returned %d: %s", resp.StatusCode, string(errBody))
			if resp.StatusCode < 500 {
				return nil, lastErr
			}
			continue
		}

		defer resp.Body.Close()

		var openaiResp openaiChatResponse
		if err := json.NewDecoder(resp.Body).Decode(&openaiResp); err != nil {
			return nil, fmt.Errorf("ai: decode response: %w", err)
		}

		if len(openaiResp.Choices) == 0 {
			return nil, fmt.Errorf("ai: empty response")
		}

		choice := openaiResp.Choices[0]
		result := &ChatResponse{
			Content: choice.Message.Content.String(),
		}

		if len(choice.Message.ToolCalls) > 0 {
			tcs, err := parseOpenAIToolCalls(choice.Message.ToolCalls)
			if err != nil {
				return nil, err
			}
			result.ToolCalls = tcs
		}

		return result, nil
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("ai: unexpected retry exit")
}

// --- 流式调用 ---

func (c *openaiClient) ChatStream(ctx context.Context, req *ChatRequest) (<-chan StreamEvent, error) {
	ch := make(chan StreamEvent, 64)

	body := openaiChatRequest{
		Model:       c.model,
		Messages:    toOpenAIMessages(req.Messages),
		Temperature: req.Temperature,
		TopP:        req.TopP,
		MaxTokens:   c.maxTokens,
		Stream:      true,
	}
	if len(req.Tools) > 0 {
		body.Tools = toOpenAITools(req.Tools)
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("ai: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("ai: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ai: http request: %w", err)
	}

	go func() {
		defer resp.Body.Close()
		defer close(ch)

		if resp.StatusCode != http.StatusOK {
			errBody, _ := io.ReadAll(resp.Body)
			ch <- StreamEvent{Type: StreamEventError, Err: fmt.Errorf("ai: api returned %d: %s", resp.StatusCode, string(errBody))}
			return
		}

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)

		var pendingToolCalls []openaiToolCall

		for scanner.Scan() {
			line := scanner.Text()

			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				if len(pendingToolCalls) > 0 {
					tcs, err := parseOpenAIToolCalls(pendingToolCalls)
					if err != nil {
						ch <- StreamEvent{Type: StreamEventError, Err: err}
						return
					}
					for _, tc := range tcs {
						tcCopy := tc
						ch <- StreamEvent{Type: StreamEventToolCall, ToolCall: &tcCopy}
					}
				}
				ch <- StreamEvent{Type: StreamEventDone}
				return
			}

			// 解析流式 delta 块
			var streamResp struct {
				Choices []struct {
					Delta struct {
						Role      string           `json:"role"`
						Content   string           `json:"content"`
						ToolCalls []openaiToolCall `json:"tool_calls"`
					} `json:"delta"`
					FinishReason string `json:"finish_reason"`
				} `json:"choices"`
			}

			if err := json.Unmarshal([]byte(data), &streamResp); err != nil {
				continue
			}

			if len(streamResp.Choices) == 0 {
				continue
			}

			delta := streamResp.Choices[0].Delta

			if delta.Content != "" {
				ch <- StreamEvent{Type: StreamEventText, Content: delta.Content}
			}

			for _, tc := range delta.ToolCalls {
				// DeepSeek 等部分提供商可能在流结束时发送空 tool_call chunk
				// （name=""、id=""、arguments=""），将其合并会导致后续产生空 ID
				// 的幽灵 tool call。此处跳过完全空的 delta。
				if tc.Function.Name == "" && tc.ID == "" && tc.Function.Arguments == "" {
					continue
				}
				mergeOrAppendToolCall(&pendingToolCalls, tc)
			}

			if streamResp.Choices[0].FinishReason == "tool_calls" {
				tcs, err := parseOpenAIToolCalls(pendingToolCalls)
				if err != nil {
					ch <- StreamEvent{Type: StreamEventError, Err: err}
					return
				}
				for _, tc := range tcs {
					tcCopy := tc
					ch <- StreamEvent{Type: StreamEventToolCall, ToolCall: &tcCopy}
				}
				pendingToolCalls = nil
			}
		}

		if err := scanner.Err(); err != nil {
			ch <- StreamEvent{Type: StreamEventError, Err: fmt.Errorf("ai: stream read: %w", err)}
			return
		}

		ch <- StreamEvent{Type: StreamEventDone}
	}()

	return ch, nil
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
func mergeOrAppendToolCall(calls *[]openaiToolCall, tc openaiToolCall) {
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
