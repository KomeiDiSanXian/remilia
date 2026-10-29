// stream_anthropic.go — Anthropic Messages API 的 SSE 负载解释。
package protocol

import (
	"encoding/json"
	"fmt"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// anthropicStreamEvent 单条 SSE 负载的载荷结构。
type anthropicStreamEvent struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
	Delta *struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		PartialJSON string `json:"partial_json"`
		StopReason  string `json:"stop_reason"`
		// Thinking 思考型模型的思考增量片段（thinking_delta），
		// 与 OpenAI 侧的 DeepSeek reasoning_content 对应：
		// 只产生推理事件，不进入正文，避免内部推理被当成回复发给用户。
		Thinking string `json:"thinking"`
	} `json:"delta,omitempty"`
	// Usage 出现在两个位置：message_start 的 message.usage（输入侧）
	// 与 message_delta 的 usage（累计输出侧）。
	Usage        *anthropicUsageBody `json:"usage,omitempty"`
	ContentBlock *struct {
		Type   string           `json:"type"`
		ID     string           `json:"id"`
		Name   string           `json:"name"`
		Input  any              `json:"input"`
		Source *anthropicImgSrc `json:"source,omitempty"` // type=image 时的图片来源
	} `json:"content_block,omitempty"`
	Message *struct {
		StopReason string `json:"stop_reason"`
		// message_start 携带输入 token 用量
		Usage *anthropicUsageBody `json:"usage,omitempty"`
	} `json:"message,omitempty"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// anthropicStreamToolUse 累积中的 tool_use content block。
//
// Anthropic 支持并行工具调用，每个 tool_use block 有自己的 index，
// 因此按 index 分别累积输入参数。
type anthropicStreamToolUse struct {
	id    string
	name  string
	input string
}

// startContentBlock 处理 content_block_start：登记工具块、转发原生图像。
func startContentBlock(event anthropicStreamEvent, pending map[int]*anthropicStreamToolUse,
	send func(StreamEvent) bool) error {
	if event.ContentBlock == nil {
		return nil
	}
	switch event.ContentBlock.Type {
	case "tool_use":
		pending[event.Index] = &anthropicStreamToolUse{
			id:   event.ContentBlock.ID,
			name: event.ContentBlock.Name,
		}
	case "image":
		// 原生图像输出的 image block 在 content_block_start 一并携带完整
		// base64，转为附件事件交由回合侧累积。
		if att, ok := attachmentFromAnthropicImage(event.ContentBlock.Source); ok {
			send(StreamEvent{Type: StreamEventAttachment, Attachment: &att})
		}
	}
	return nil
}

// applyContentBlockDelta 处理 content_block_delta：正文、推理、工具参数增量。
func applyContentBlockDelta(event anthropicStreamEvent, pending map[int]*anthropicStreamToolUse,
	send func(StreamEvent) bool) error {
	if event.Delta == nil {
		return nil
	}
	switch event.Delta.Type {
	case "text_delta":
		if event.Delta.Text != "" {
			send(StreamEvent{Type: StreamEventText, Content: event.Delta.Text})
		}
	case "input_json_delta":
		if tool, ok := pending[event.Index]; ok {
			tool.input += event.Delta.PartialJSON
		}
	case "thinking_delta":
		if event.Delta.Thinking != "" {
			send(StreamEvent{Type: StreamEventReasoning, Content: event.Delta.Thinking})
		}
	}
	return nil
}

// finishContentBlock 处理 content_block_stop：把累积完的工具块发成一次调用。
func finishContentBlock(event anthropicStreamEvent, pending map[int]*anthropicStreamToolUse,
	send func(StreamEvent) bool) error {
	tool, ok := pending[event.Index]
	if !ok {
		return nil
	}
	args := make(map[string]any)
	if tool.input != "" {
		json.Unmarshal([]byte(tool.input), &args)
	}
	send(StreamEvent{Type: StreamEventToolCall, ToolCall: &ToolCall{
		ID:        tool.id,
		Name:      tool.name,
		Arguments: args,
	}})
	delete(pending, event.Index)
	return nil
}

// newAnthropicStreamDecoder 返回带工具块与 token 累积状态的负载解释器。
func newAnthropicStreamDecoder() streamDecoder {
	pendingTools := make(map[int]*anthropicStreamToolUse)
	var promptTokens, completionTokens, cacheReadTokens int

	return func(data string, state *streamState, send func(StreamEvent) bool) (bool, error) {
		var event anthropicStreamEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			logger.Debugf("[AI] skip non-JSON anthropic stream line: %v", err)
			return false, nil
		}

		switch event.Type {
		case "message_start":
			// 输入 token 用量在 message_start 的 message.usage 里。
			if event.Message != nil && event.Message.Usage != nil {
				promptTokens = event.Message.Usage.totalInputTokens()
				cacheReadTokens = event.Message.Usage.CacheReadInputTokens
			}

		case "content_block_start":
			return false, startContentBlock(event, pendingTools, send)

		case "content_block_delta":
			return false, applyContentBlockDelta(event, pendingTools, send)

		case "content_block_stop":
			return false, finishContentBlock(event, pendingTools, send)

		case "message_delta":
			// 输出 token 用量在 message_delta 的 usage（累计值）。
			if event.Usage != nil {
				completionTokens = event.Usage.OutputTokens
			}
			if event.Delta != nil && event.Delta.StopReason != "" {
				state.finishReason = event.Delta.StopReason
			}

		case "message_stop":
			state.usage = &TokenUsage{
				PromptTokens:     promptTokens,
				CompletionTokens: completionTokens,
				CachedTokens:     cacheReadTokens,
			}
			return true, nil

		case "error":
			errMsg := "unknown anthropic error"
			if event.Error != nil {
				switch {
				case event.Error.Message != "":
					errMsg = event.Error.Message
				case event.Error.Type != "":
					errMsg = event.Error.Type
				}
			}
			return false, fmt.Errorf("ai: anthropic stream error: %s", errMsg)
		}

		return false, nil
	}
}
