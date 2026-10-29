// stream_openai.go — OpenAI 兼容端点的 SSE 负载解释。
package protocol

import (
	"encoding/json"
	"fmt"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// openaiStreamChunk 单条 SSE 负载的载荷结构。
type openaiStreamChunk struct {
	Choices []struct {
		Delta struct {
			Role    string               `json:"role"`
			Content openaiMessageContent `json:"content"`
			// ReasoningContent 思考型模型（DeepSeek 等）的推理增量片段：
			// 与 content 走不同字段；预算被耗尽时只有它有内容、content 恒为空。
			// 单独成事件而不是拼进正文，避免内部推理被当成回复发给用户。
			ReasoningContent string `json:"reasoning_content"`
			// Images 部分聚合网关（如 OpenRouter）在 delta.images 里携带
			// 模型直接输出的图片，与 content 并列。
			Images    []openaiContentPart `json:"images"`
			ToolCalls []OpenAIToolCall    `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *openaiUsageBody `json:"usage"`
	// Error 部分聚合器（非 OpenAI 官方端点）把错误放在负载尾部：
	// 响应 HTTP 状态码是 200，不显式识别会被当成"模型什么都没说"的空回复。
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    any    `json:"code"`
	} `json:"error"`
}

// newOpenAIStreamDecoder 返回带工具调用累积状态的负载解释器。
//
// 工具调用在 OpenAI 协议里是跨多条 SSE 负载分片到达的（name 只在首片给出，
// arguments 逐片追加），因此累积状态必须活到本轮结束——这也是它不能放进
// streamState（那是"结束时回填给 Done 事件"的字段）的原因。
func newOpenAIStreamDecoder() streamDecoder {
	var pendingToolCalls []OpenAIToolCall

	return func(data string, state *streamState, send func(StreamEvent) bool) (bool, error) {
		if data == "[DONE]" {
			return true, nil
		}

		var chunk openaiStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			// 默认 continue 会把"网关偶发非 JSON 片段"变成静默丢字：整段流不报错，
			// 却缺了内容，排查时无从下手。留一条 Debug 日志即可定位。
			logger.Debugf("[AI] skip non-JSON stream line: %v", err)
			return false, nil
		}

		if chunk.Error != nil {
			msg := chunk.Error.Message
			if msg == "" {
				msg = chunk.Error.Type
			}
			return false, fmt.Errorf("ai: api stream error: %s", msg)
		}

		// usage 块（include_usage 开启时）在 [DONE] 之前到达，choices 为空。
		if chunk.Usage != nil {
			state.usage = &TokenUsage{
				PromptTokens:     chunk.Usage.PromptTokens,
				CompletionTokens: chunk.Usage.CompletionTokens,
				CachedTokens:     chunk.Usage.cachedPromptTokens(),
				ReasoningTokens:  chunk.Usage.reasoningTokens(),
			}
		}

		if len(chunk.Choices) == 0 {
			return false, nil
		}

		delta := chunk.Choices[0].Delta
		if fr := chunk.Choices[0].FinishReason; fr != "" {
			state.finishReason = fr
		}

		if rc := delta.ReasoningContent; rc != "" {
			if !send(StreamEvent{Type: StreamEventReasoning, Content: rc}) {
				return false, nil
			}
		}

		if txt := delta.Content.String(); txt != "" {
			if !send(StreamEvent{Type: StreamEventText, Content: txt}) {
				return false, nil
			}
		}

		// 模型直接输出的图片（原生图像输出/多模态响应）作为附件事件透传，
		// 由回合侧累积后随最终回复发送给用户。
		for _, att := range openaiOutputImageAttachments(&delta.Content, delta.Images) {
			if !send(StreamEvent{Type: StreamEventAttachment, Attachment: &att}) {
				return false, nil
			}
		}

		for _, tc := range delta.ToolCalls {
			if tc.Function.Name == "" && tc.ID == "" && tc.Function.Arguments == "" {
				continue
			}
			mergeOrAppendToolCall(&pendingToolCalls, tc)
		}

		if chunk.Choices[0].FinishReason == "tool_calls" {
			tcs, err := ParseOpenAIToolCalls(pendingToolCalls)
			pendingToolCalls = nil
			if err != nil {
				return false, err
			}
			for _, tc := range tcs {
				tcCopy := tc
				if !send(StreamEvent{Type: StreamEventToolCall, ToolCall: &tcCopy}) {
					return false, nil
				}
			}
		}

		return false, nil
	}
}
