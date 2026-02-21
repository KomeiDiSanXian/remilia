package remilia

import (
	"context"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ctxMockAdapter 用于测试的 Adapter stub，阻塞直到 ctx 取消
type ctxMockAdapter struct {
	startedCh chan struct{}
}

func newCtxMockAdapter() *ctxMockAdapter {
	return &ctxMockAdapter{startedCh: make(chan struct{})}
}
func (a *ctxMockAdapter) Start(ctx context.Context, _ func(*dto.Payload)) error {
	close(a.startedCh)
	<-ctx.Done()
	return nil
}
func (a *ctxMockAdapter) Stop(_ context.Context) error { return nil }

// waitBotRunning 等待 Bot 进入 running 状态
func waitBotRunning(t *testing.T, b *Bot) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if b.IsRunning() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("bot did not reach running state in time")
}

// TestBot_Context_BeforeStart Start 前 Context() 应返回不被取消的 context
func TestBot_Context_BeforeStart(t *testing.T) {
	b := NewBot(newCtxMockAdapter(), engine.NewEngine())
	ctx := b.Context()
	assert.NotNil(t, ctx)
	select {
	case <-ctx.Done():
		t.Fatal("context must not be cancelled before Start()")
	default:
	}
}

// TestBot_Context_AfterStart Start 后 Context() 应存在且活跃
func TestBot_Context_AfterStart(t *testing.T) {
	adapter := newCtxMockAdapter()
	b := NewBot(adapter, engine.NewEngine())
	go func() { _ = b.Start() }()
	waitBotRunning(t, b)
	ctx := b.Context()
	assert.NotNil(t, ctx)
	select {
	case <-ctx.Done():
		t.Fatal("context must be active while bot is running")
	default:
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, b.Stop(stopCtx))
}

// TestBot_Context_CancelledAfterStop Stop 后 rootCtx 应被取消
func TestBot_Context_CancelledAfterStop(t *testing.T) {
	adapter := newCtxMockAdapter()
	b := NewBot(adapter, engine.NewEngine())
	go func() { _ = b.Start() }()
	// 等待 Bot 完全进入 running 状态（rootCtx 已赋值）
	waitBotRunning(t, b)
	rootCtx := b.Context()
	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, b.Stop(stopCtx))
	select {
	case <-rootCtx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("rootCtx must be cancelled after Stop()")
	}
}
