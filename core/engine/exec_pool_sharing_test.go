package engine

// exec_pool_sharing_test.go — 共享 ExecPool 的注入与生命周期归属
//
// 回归背景：NewEngine 在应用选项后无条件创建自有池，把
// WithSharedExecPool 注入的池覆盖掉（选项静默失效）。

import (
	stdctx "context"
	"testing"
)

// TestEngine_SharedExecPoolNotOverwritten 验证 WithSharedExecPool 注入的
// 共享池不被 NewEngine 覆盖，且 Engine.Shutdown 不会停掉共享池。
func TestEngine_SharedExecPoolNotOverwritten(t *testing.T) {
	pool := NewExecPool(ExecPoolConfig{MaxConcurrency: 2, QueueSize: 4})
	eng := NewEngine(WithNoBackgroundWorkers(), WithSharedExecPool(pool))

	if eng.internals.execPool != pool {
		t.Fatal("WithSharedExecPool pool was overwritten by NewEngine")
	}

	if err := eng.Shutdown(stdctx.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if pool.stopped.Load() {
		t.Fatal("Engine.Shutdown must not stop a shared ExecPool")
	}

	// 共享池在 Engine 关闭后仍可用，由调用方负责停止
	done := make(chan struct{})
	if !pool.TrySubmit(func() { close(done) }) {
		t.Fatal("shared pool should still accept tasks after engine shutdown")
	}
	<-done
	pool.Stop()
}
