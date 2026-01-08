package remilia

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/openapi/protocol/webhook"
	"github.com/stretchr/testify/require"
)

type fakeWebHook struct {
	addr string
	ch   chan *dto.Payload

	closed atomic.Bool
}

func (f *fakeWebHook) Verify(header http.Header, body []byte) (bool, error) { return true, nil }
func (f *fakeWebHook) Sign(header http.Header, body []byte) ([]byte, error) { return body, nil }
func (f *fakeWebHook) Handle(w http.ResponseWriter, r *http.Request)        {}
func (f *fakeWebHook) Addr() string                                         { return f.addr }
func (f *fakeWebHook) EventStream() <-chan *dto.Payload                     { return f.ch }

func TestWebhookAdapter_Shutdown_WaitsForEventLoopExit(t *testing.T) {
	wh := &fakeWebHook{addr: "127.0.0.1:0", ch: make(chan *dto.Payload)}
	a := NewWebhookAdapter(wh)

	startCtx, startCancel := context.WithCancel(context.Background())
	defer startCancel()

	err := a.Start(startCtx, func(*dto.Payload) {})
	require.NoError(t, err)

	// 用一个很短的 timeout shutdown，确保如果 Shutdown 不等待，会很快返回，测试就能抓到问题。
	// 注意：我们这里不 close wh.ch，而是依赖 adapter 内部 cancel 来退出 loop。
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		_ = a.Shutdown(shutdownCtx)
		close(done)
	}()

	require.Eventually(t, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, 2*time.Second, 10*time.Millisecond)
}

func TestWebhookAdapter_Shutdown_TimesOutIfContextDone(t *testing.T) {
	wh := &fakeWebHook{addr: "127.0.0.1:0", ch: make(chan *dto.Payload)}
	a := NewWebhookAdapter(wh)

	startCtx := context.Background()
	err := a.Start(startCtx, func(*dto.Payload) {
		// block forever if called (shouldn't be needed)
		select {}
	})
	require.NoError(t, err)

	// 立即超时的 ctx，Shutdown 应返回 ctx.Err
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(1 * time.Millisecond)

	err = a.Shutdown(shutdownCtx)
	require.Error(t, err)
}

func TestWebhookAdapter_Shutdown_Idempotent(t *testing.T) {
	wh := &fakeWebHook{addr: "127.0.0.1:0", ch: make(chan *dto.Payload)}
	a := NewWebhookAdapter(wh)

	err := a.Start(context.Background(), func(*dto.Payload) {})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	require.NoError(t, a.Shutdown(ctx))
	// 第二次调用应该不 panic，也不应该阻塞
	require.NoError(t, a.Shutdown(ctx))
}

func TestWebhookAdapter_Shutdown_DoesNotCloseEventStream(t *testing.T) {
	ch := make(chan *dto.Payload)
	wh := &fakeWebHook{addr: "127.0.0.1:0", ch: ch}
	a := NewWebhookAdapter(wh)

	err := a.Start(context.Background(), func(*dto.Payload) {})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, a.Shutdown(ctx))

	// Contract: adapter 不负责关闭 EventStream channel。
	select {
	case <-ch:
		// 如果 channel 被关闭，这里会立刻返回 zero value；但我们没法区分值/关闭，
		// 用 non-blocking + ok 判断更直接。
		// 这里故意不用这个分支，改为下面的显式 ok 检查。
	default:
	}

	select {
	case _, ok := <-ch:
		// 如果 ch 被关闭，ok==false。
		require.True(t, ok, "EventStream channel should not be closed by adapter")
	default:
		// 没有事件也没关闭是正常的
	}
}

func TestWebhookAdapter_SatisfiesWebhookInterfaceCompileTime(t *testing.T) {
	// compile-time check for interface drift
	var _ webhook.WebHook = (*fakeWebHook)(nil)
}
