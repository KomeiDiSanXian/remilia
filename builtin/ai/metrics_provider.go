// metrics_provider.go — Provider 装饰器：在协议层之外采集调用指标。
//
// 装饰器留在这里而不是协议层：协议层只描述线格式，不承担可观测性依赖。
// 指标定义见 metrics_defs.go，记录函数见 metrics_record.go。
package ai

import (
	"context"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
)

// metricsProvider 装饰 Provider，采集 LLM 调用指标。
type metricsProvider struct {
	next         protocol.Provider
	defaultModel string
}

// Chat 包装：所有 LLM 调用自动计入指标。
func (m *metricsProvider) Chat(ctx context.Context, req *protocol.ChatRequest) (*protocol.ChatResponse, error) {
	start := time.Now()
	meta := llmCallMetaOf(ctx, req, m.defaultModel)
	resp, err := m.next.Chat(ctx, req)
	result := "ok"
	if err != nil {
		result = "error"
	}
	var usage *protocol.TokenUsage
	if resp != nil {
		usage = resp.Usage
	}
	recordLLMCall(meta, llmCallResult{duration: time.Since(start), usage: usage, result: result, err: err})
	return resp, err
}

// ChatStream 包装流式调用：耗时覆盖整个流的消费过程。
func (m *metricsProvider) ChatStream(ctx context.Context, req *protocol.ChatRequest) (<-chan protocol.StreamEvent, error) {
	start := time.Now()
	meta := llmCallMetaOf(ctx, req, m.defaultModel)

	inner, err := m.next.ChatStream(ctx, req)
	if err != nil {
		recordLLMCall(meta, llmCallResult{duration: time.Since(start), result: "error", err: err})
		return nil, err
	}

	outer := make(chan protocol.StreamEvent, 64)
	go func() {
		defer close(outer)
		var (
			usage        *protocol.TokenUsage
			done         bool
			finishReason string
			streamErr    error
			// firstAt 第一个内容事件（正文/推理/工具调用/附件）的时间：
			// 推理片段也算首字——它代表模型真正开始产出，而不是首包/心跳。
			firstAt time.Time
		)
		// record 统一收尾：每次调用只计一次，ttft 只在观测到首字时才有值。
		record := func(result string) {
			var ttft time.Duration
			if !firstAt.IsZero() {
				ttft = firstAt.Sub(start)
			}
			// 只有按失败收尾时才计入错误分类；主动停止不算错误。
			var recErr error
			if result == "error" {
				recErr = streamErr
				if recErr == nil {
					recErr = errStreamEndedWithoutDone
				}
			}
			recordLLMCall(meta, llmCallResult{
				duration: time.Since(start),
				ttft:     ttft,
				usage:    usage,
				result:   result,
				finish:   finishReason,
				err:      recErr,
			})
		}
		for {
			select {
			case ev, open := <-inner:
				if !open {
					// 流关闭但未收到 Done（provider 内部异常收尾）
					result := "stopped"
					if done {
						result = "ok"
					} else if ctx.Err() == nil {
						result = "error"
					}
					record(result)
					return
				}
				switch ev.Type {
				case protocol.StreamEventText, protocol.StreamEventReasoning,
					protocol.StreamEventToolCall, protocol.StreamEventAttachment:
					if firstAt.IsZero() {
						firstAt = time.Now()
					}
				case protocol.StreamEventDone:
					done = true
					usage = ev.Usage
					finishReason = ev.FinishReason
				case protocol.StreamEventError:
					streamErr = ev.Err
				}
				select {
				case outer <- ev:
				case <-ctx.Done():
					// 下游停止消费：按停止收尾并排空 inner，
					// 避免 provider 的发送 goroutine 阻塞
					for range inner {
					}
					record("stopped")
					return
				}
			case <-ctx.Done():
				// 上游/回合取消且暂无事件可转发：同样排空 inner，避免 provider
				// 的发送 goroutine 因无人接收而永久阻塞泄漏。
				for range inner {
				}
				record("stopped")
				return
			}
		}
	}()
	return outer, nil
}
