package engine

import (
	"testing"

	corectx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// TestMatcherOnReject 被准入控制拒绝时，引擎调用 matcher 声明的忙时处理器，
// 而不是把它当成 handler 错误（也不算业务 handler 执行）。
func TestMatcherOnReject(t *testing.T) {
	e := newEngineForTest(t)

	businessCalled := false
	rejected := 0
	e.OnCommand(string(platform.EventKindPrivateMessage), "/busy").
		Use(func(next corectx.Handler) corectx.Handler {
			return func(ctx *corectx.Context) error {
				// 模拟准入拒绝：不调用业务 handler，直接返回哨兵。
				return corectx.ErrBusy
			}
		}).
		Handle(func(*corectx.Context) error {
			businessCalled = true
			return nil
		}).
		OnReject(func(ctx *corectx.Context) error {
			rejected++
			return nil
		})

	e.ProcessPlatformEvent(&engineTestEvent{
		kind:    platform.EventKindPrivateMessage,
		content: "/busy",
		id:      "e",
	}, &platform.NoopSender{})

	if businessCalled {
		t.Fatal("business handler must not run when admission rejects")
	}
	if rejected != 1 {
		t.Fatalf("reject handler called %d times, want 1", rejected)
	}
}
