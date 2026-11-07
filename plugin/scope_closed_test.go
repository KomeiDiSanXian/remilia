package plugin

// scope_closed_test.go — Scope 关闭态拒绝新资源的回归测试
//
// 回归背景：Subscribe 只在"订阅成功之后"检查 disposed，而 disposed 要等全部
// 清理完成才置位；OnDispose 与子 Scope 创建则完全不检查。于是 dispose hook
// 内部（以及 dispose 被取消后的重试间隙）创建的订阅/钩子/子 Scope 都错过了
// 已拍摄的清理快照，永久留在 EventBus 上无人回收。

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScope_RejectsResourcesAfterDispose(t *testing.T) {
	ctx := NewTestSetupContext("scope-closed", nil)
	defer StopTestSetupContext(ctx)
	bus := ctx.EventBus
	root := ctx.Scope()

	var hookLeaked, childLeaked bool
	var child *Scope
	root.OnDispose(func() error {
		if _, err := root.Subscribe("hook.topic", func(context.Context, any) error { return nil }); err == nil {
			hookLeaked = true
		}
		child = root.Scope("hook-child")
		if _, err := child.Subscribe("child.topic", func(context.Context, any) error { return nil }); err == nil {
			childLeaked = true
		}
		return nil
	})

	require.NoError(t, root.Dispose(context.Background()))
	assert.False(t, hookLeaked, "Dispose 期间的订阅必须被拒绝")
	assert.False(t, childLeaked, "已 Dispose 父作用域下的子 Scope 不得接受订阅")
	assert.Equal(t, 0, bus.GetStats().SubscriptionCount)

	// 完成之后注册的钩子立即执行，而不是静默丢失
	ran := false
	root.OnDispose(func() error { ran = true; return nil })
	assert.True(t, ran, "Dispose 之后注册的钩子应立即执行")
}

// TestScope_PanicInDisposeHookDoesNotAbortChain 清理钩子里的 panic 不得中断
// 其它钩子/订阅的清理。
func TestScope_PanicInDisposeHookDoesNotAbortChain(t *testing.T) {
	ctx := NewTestSetupContext("scope-panic", nil)
	defer StopTestSetupContext(ctx)
	bus := ctx.EventBus
	root := ctx.Scope()

	_, err := root.Subscribe("panic.topic", func(context.Context, any) error { return nil })
	require.NoError(t, err)

	ran := 0
	root.OnDispose(func() error { ran++; return nil })
	root.OnDispose(func() error { panic("boom") })
	root.OnDispose(func() error { ran++; return nil })

	require.NoError(t, root.Dispose(context.Background()))
	assert.Equal(t, 2, ran, "panic 之外的钩子仍必须执行")
	assert.Equal(t, 0, bus.GetStats().SubscriptionCount, "订阅仍必须退订")
}
