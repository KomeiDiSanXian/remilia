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
	httpClient *http.Client
}

func NewAnthropicProvider(cfg *Config) (Provider, error) {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	baseURL = strings.TrimRight(baseURL, "/")

	return &anthropicClient{
		baseURL:    baseURL,
		apiKey:     cfg.APIKey,
		model:      cfg.Model,
		maxTokens:  cfg.MaxTokens,
		httpClient: &http.Client{},
	}, nil
}

// --- Anthropic API 请求/响应结构 ---

// anthropicMessage 对应 Anthropic Messages API 的 content block 数组格式。
// 与 OpenAI 不同，Anthropic 的 content 是数组而非字符串。
type anthropicReqMessage struct {
	Role    string                    `json:"role"`
	Content []anthropicReqContentBlock `json:"content"`
}

type anthropicReqContentBlock struct {
	Type  string `json:"type"`
	Text  string `json:"text,omitempty"`
	ID    string `json:"id,omitempty"`    // tool_result 时填写对应的 tool_use id
	Name  string `json:"name,omitempty"`  // tool_use 时填写工具名
	Input any    `json:"input,omitempty"` // tool_use 时填写参数
}

type anthropicChatRequest struct {
	Model       string           `json:"model"`
	MaxTokens   int              `json:"max_tokens"`
	System      string           `json:"system,omitempty"`
	Messages    []anthropicReqMessage `json:"messages"`
	Tools       []anthropicTool  `json:"tools,omitempty"`
	Temperature float64          `json:"temperature,omitempty"`
	Stream      bool             `json:"stream,omitempty"`
}

type anthropicChatResponse struct {
	ID      string                     `json:"id"`
	Type    string                     `json:"type"`
	Role    string                     `json:"role"`
	Content []anthropicContentBlock    `json:"content"`
	StopReason string                  `json:"stop_reason"`
	Error   *anthropicErrorBody        `json:"error,omitempty"`
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
			flush()
			blocks := []anthropicReqContentBlock{}
			if m.Content != "" {
				blocks = append(blocks, anthropicReqContentBlock{
					Type: "text",
					Text: m.Content,
				})
			}
			if len(m.ToolCalls) > 0 {
				for _, tc := range m.ToolCalls {
					blocks = append(blocks, anthropicReqContentBlock{
						Type:  "tool_result",
						ID:    tc.ID,
						Input: tc.Arguments,
					})
				}
			}
			current = &anthropicReqMessage{
				Role:    "user",
				Content: blocks,
			}

		case RoleTool:
			flush()
			current = &anthropicReqMessage{
				Role: "user",
				Content: []anthropicReqContentBlock{
					{
						Type: "tool_result",
						ID:   m.ToolCallID,
						Text: m.Content,
					},
				},
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

// --- 非流式调用 ---

func (c *anthropicClient) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	systemPrompt := extractAnthropicSystem(req.Messages)

	body := anthropicChatRequest{
		Model:       c.model,
		MaxTokens:   c.maxTokens,
		System:      systemPrompt,
		Messages:    toAnthropicMessages(req.Messages),
		Temperature: req.Temperature,
	}
	if len(req.Tools) > 0 {
		body.Tools = toAnthropicTools(req.Tools)
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("ai: anthropic marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/messages", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("ai: anthropic create request: %w", err)
	}
	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ai: anthropic http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ai: anthropic api returned %d: %s", resp.StatusCode, string(body))
	}

	var anthropicResp anthropicChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&anthropicResp); err != nil {
		return nil, fmt.Errorf("ai: anthropic decode response: %w", err)
	}

	if anthropicResp.Error != nil {
		return nil, fmt.Errorf("ai: anthropic api error: %s: %s", anthropicResp.Error.Type, anthropicResp.Error.Message)
	}

	result := &ChatResponse{}
	for _, block := range anthropicResp.Content {
		if block.Type == "text" {
			result.Content += block.Text
		}
	}

	if anthropicResp.StopReason == "tool_use" {
		result.ToolCalls = parseAnthropicToolCalls(anthropicResp.Content)
	}

	return result, nil
}

// --- 流式调用 ---

func (c *anthropicClient) ChatStream(ctx context.Context, req *ChatRequest) (<-chan StreamEvent, error) {
	ch := make(chan StreamEvent, 64)

	systemPrompt := extractAnthropicSystem(req.Messages)

	body := anthropicChatRequest{
		Model:       c.model,
		MaxTokens:   c.maxTokens,
		System:      systemPrompt,
		Messages:    toAnthropicMessages(req.Messages),
		Temperature: req.Temperature,
		Stream:      true,
	}
	if len(req.Tools) > 0 {
		body.Tools = toAnthropicTools(req.Tools)
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("ai: anthropic marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/messages", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("ai: anthropic create request: %w", err)
	}
	c.setHeaders(httpReq)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ai: anthropic http request: %w", err)
	}

	go func() {
		defer resp.Body.Close()
		defer close(ch)

		if resp.StatusCode != http.StatusOK {
			errBody, _ := io.ReadAll(resp.Body)
			ch <- StreamEvent{Type: StreamEventError, Err: fmt.Errorf("ai: anthropic api returned %d: %s", resp.StatusCode, string(errBody))}
			return
		}

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)

		var pendingToolUse struct {
			id    string
			name  string
			input string
		}
		var hasPendingTool bool

		for scanner.Scan() {
			line := scanner.Text()

			// Anthropic SSE 格式: event: ${event_name} 后跟 data: ${json}
			if strings.HasPrefix(line, "event: ") {
				continue
			}
			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")
			if data == "" {
				continue
			}

			// 解析流式事件
			var streamEvent struct {
				Type string `json:"type"`
				Index int    `json:"index"`
				Delta *struct {
					Type        string `json:"type"`
					Text        string `json:"text"`
					PartialJSON string `json:"partial_json"`
				} `json:"delta,omitempty"`
				ContentBlock *struct {
					Type  string `json:"type"`
					ID    string `json:"id"`
					Name  string `json:"name"`
					Input any    `json:"input"`
				} `json:"content_block,omitempty"`
				Message *struct {
					StopReason string `json:"stop_reason"`
				} `json:"message,omitempty"`
			}

			if err := json.Unmarshal([]byte(data), &streamEvent); err != nil {
				continue
			}

			switch streamEvent.Type {
			case "content_block_start":
				if streamEvent.ContentBlock != nil && streamEvent.ContentBlock.Type == "tool_use" {
					pendingToolUse.id = streamEvent.ContentBlock.ID
					pendingToolUse.name = streamEvent.ContentBlock.Name
					pendingToolUse.input = ""
					hasPendingTool = true
				}

			case "content_block_delta":
				if streamEvent.Delta != nil {
					switch streamEvent.Delta.Type {
					case "text_delta":
						if streamEvent.Delta.Text != "" {
							ch <- StreamEvent{Type: StreamEventText, Content: streamEvent.Delta.Text}
						}
					case "input_json_delta":
						if hasPendingTool {
							pendingToolUse.input += streamEvent.Delta.PartialJSON
						}
					}
				}

			case "content_block_stop":
				if hasPendingTool {
					args := make(map[string]any)
					if pendingToolUse.input != "" {
						json.Unmarshal([]byte(pendingToolUse.input), &args)
					}
					ch <- StreamEvent{Type: StreamEventToolCall, ToolCall: &ToolCall{
						ID:        pendingToolUse.id,
						Name:      pendingToolUse.name,
						Arguments: args,
					}}
					hasPendingTool = false
				}

			case "message_delta":
				// 可在此处处理 stop_reason

			case "message_stop":
				ch <- StreamEvent{Type: StreamEventDone}
				return

			case "error":
				errMsg := "unknown anthropic error"
				if streamEvent.Message != nil {
					errMsg = streamEvent.Message.StopReason
				}
				ch <- StreamEvent{Type: StreamEventError, Err: fmt.Errorf("ai: anthropic stream error: %s", errMsg)}
				return
			}
		}

		if err := scanner.Err(); err != nil {
			ch <- StreamEvent{Type: StreamEventError, Err: fmt.Errorf("ai: anthropic stream read: %w", err)}
			return
		}

		ch <- StreamEvent{Type: StreamEventDone}
	}()

	return ch, nil
}

// setHeaders 设置 Anthropic API 请求头。
func (c *anthropicClient) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
}
