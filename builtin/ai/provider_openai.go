package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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
		httpClient: &http.Client{},
	}, nil
}

// --- 请求/响应结构体 ---

type openaiChatMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content,omitempty"`
	ToolCalls  []openaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type openaiChatRequest struct {
	Model       string              `json:"model"`
	Messages    []openaiChatMessage `json:"messages"`
	Tools       []openaiTool        `json:"tools,omitempty"`
	Temperature float64             `json:"temperature,omitempty"`
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
			Role:    string(m.Role),
			Content: m.Content,
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

// --- 非流式调用 ---

func (c *openaiClient) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	body := openaiChatRequest{
		Model:       c.model,
		Messages:    toOpenAIMessages(req.Messages),
		Temperature: req.Temperature,
		MaxTokens:   c.maxTokens,
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
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ai: http request: %w", err)
	}
	defer resp.Body.Close()

	var openaiResp openaiChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&openaiResp); err != nil {
		return nil, fmt.Errorf("ai: decode response: %w", err)
	}

	if openaiResp.Error != nil {
		return nil, fmt.Errorf("ai: api error: %s: %s", openaiResp.Error.Type, openaiResp.Error.Message)
	}

	if len(openaiResp.Choices) == 0 {
		return nil, fmt.Errorf("ai: empty response")
	}

	choice := openaiResp.Choices[0]
	result := &ChatResponse{
		Content: choice.Message.Content,
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

// --- 流式调用 ---

func (c *openaiClient) ChatStream(ctx context.Context, req *ChatRequest) (<-chan StreamEvent, error) {
	ch := make(chan StreamEvent, 64)

	body := openaiChatRequest{
		Model:       c.model,
		Messages:    toOpenAIMessages(req.Messages),
		Temperature: req.Temperature,
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
		var contentBuf strings.Builder

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
				contentBuf.WriteString(delta.Content)
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
