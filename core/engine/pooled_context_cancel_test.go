package engine

// pooled_context_cancel_test.go — 池化任务必须释放克隆 context 的回归测试
//
// 回归背景：ExecPool 路径为每个入池事件 Clone 一个 Context（含独立的 std
// context），但任务结束后无人调用 Cancel——每个入池事件都留下一个永不取消
// 的派生 context（其上的 trace span/资源随事件数与队列时长累积）。

import (
	stdctx "context"
	"testing"
	"time"

	ctx "github.com/KomeiDiSanXian/remilia/core/context"
)

func TestEngine_PooledCloneContextIsCanceled(t *testing.T) {
	// 不禁用 ExecPool：首次执行时 ExecProfile 默认走池
	eng := NewEngine(WithNoBackgroundWorkers())
	t.Cleanup(func() {
		shutdownCtx, cancel := stdctx.WithTimeout(stdctx.Background(), 5*time.Second)
		defer cancel()
		_ = eng.Shutdown(shutdownCtx)
	})

	handled := make(chan stdctx.Context, 1)
	m := eng.OnAny()
	m.Handle(func(c *ctx.Context) error {
		handled <- c.Context()
		return nil
	})

	eng.ProcessEvent(newTestCtx("hello"))

	var pooled stdctx.Context
	select {
	case pooled = <-handled:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not run")
	}

	select {
	case <-pooled.Done():
		// 池任务结束后克隆的 std context 应已取消
	case <-time.After(2 * time.Second):
		t.Fatal("pooled clone context must be canceled after the task")
	}
}
