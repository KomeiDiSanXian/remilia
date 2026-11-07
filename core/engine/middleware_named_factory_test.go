package engine

// middleware_named_factory_test.go — Named 中间件的包装时机与追踪钩子
//
// 回归背景：Named 把 mw(next) 写在了返回的 handler 内部，导致每个事件都会
// 重新调用一次中间件工厂（多一次分配，且工厂内捕获的状态每事件重置）。

import (
	"sync/atomic"
	"testing"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
)

// TestEngine_NamedMiddlewareFactoryCalledOnce 验证 Named 包装的中间件工厂
// 在链构建时只调用一次，而不是每事件一次。
func TestEngine_NamedMiddlewareFactoryCalledOnce(t *testing.T) {
	eng := newEngineForTest(t)
	et := string(platform.EventKindPrivateMessage)

	var factoryCalls atomic.Int32
	mw := eng.Named("probe", func(next context.Handler) context.Handler {
		factoryCalls.Add(1)
		return func(c *context.Context) error { return next(c) }
	})

	m := eng.On(et)
	m.Use(mw)
	m.Handle(func(*context.Context) error { return nil })

	for range 5 {
		eng.ProcessEvent(context.NewContextFromEvent(
			newTestPlatformEventWithContent(platform.EventKindPrivateMessage, "hi"), nil))
	}
	assert.Equal(t, int32(1), factoryCalls.Load(),
		"中间件工厂应在建链时调用一次，而不是每事件重建")
}
