// stream.go — 两个 provider 共用的流式响应骨架。
//
// 职责边界：本文件只管"HTTP 响应 → 事件 channel"这一段公共机制
// （状态码检查、错误体读取、事件投递与取消、goroutine 生命周期），
// 负载解释由各自的 ChatStream 提供。
package protocol

import (
	"context"
	"io"
	"net"
	"net/http"
	"time"
)

// newHTTPClient 构造两个 provider 共用的 HTTP 客户端。
//
// 这些调优参数原本在两个 New*Provider 里各抄一份：数值一旦漂移，
// 同一份部署里两个提供商的超时行为会不一致。
func newHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   10,
			IdleConnTimeout:       90 * time.Second,
		},
	}
}

// streamState 流式解释器维护的跨事件状态：结束时回填进 Done 事件。
type streamState struct {
	usage        *TokenUsage
	finishReason string
}

// streamDecoder 解释一条 SSE 负载。
//
// done 表示这是终止标记，骨架会补发 Done 事件并关闭 channel。
// state 由解释器就地更新（token 用量、finish_reason 等跨事件累积）。
type streamDecoder func(data string, state *streamState, send func(StreamEvent) bool) (done bool, err error)

// pumpSSE 在独立 goroutine 里把 HTTP 响应解释成 [StreamEvent] 并写入 out。
//
// 骨架承担所有"与具体协议无关"的事情：
//   - 非 200 响应读错误体并投递 [StreamEventError]；
//   - 每条事件投递都监听 ctx.Done()，下游不消费时不至于永久阻塞；
//   - 结束/出错时统一补发 StreamEventDone / StreamEventError，并关闭 channel。
func pumpSSE(ctx context.Context, resp *http.Response, out chan<- StreamEvent, decode streamDecoder) {
	defer resp.Body.Close()
	defer close(out)

	send := func(ev StreamEvent) bool {
		select {
		case out <- ev:
			return true
		case <-ctx.Done():
			return false
		}
	}

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		send(StreamEvent{Type: StreamEventError, Err: &APIError{StatusCode: resp.StatusCode, Body: string(errBody)}})
		return
	}

	var state streamState
	sc := newSSEScanner(resp.Body)
	for {
		data, ok := sc.next()
		if !ok {
			break
		}
		done, err := decode(data, &state, send)
		if err != nil {
			send(StreamEvent{Type: StreamEventError, Err: err})
			return
		}
		if done {
			send(StreamEvent{Type: StreamEventDone, Usage: state.usage, FinishReason: state.finishReason})
			return
		}
		if ctx.Err() != nil {
			return
		}
	}
	if err := sc.err(); err != nil {
		send(StreamEvent{Type: StreamEventError, Err: err})
		return
	}
	send(StreamEvent{Type: StreamEventDone, Usage: state.usage, FinishReason: state.finishReason})
}
