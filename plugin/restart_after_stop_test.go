package plugin

// restart_after_stop_test.go — StopAll → StartAll 重启后根 Scope 重建的回归测试
//
// 回归背景：StartAll 的文档宣称幂等可重启，但 SetupContext 会跨轮复用，而它的
// rootScope 在上一轮 unload 里已被 Dispose（disposed=true 且不再接受资源）。
// 重启后的 Setup 拿到的仍是那个死 Scope：Subscribe 被静默拒绝（插件静默失聪），
// OnDispose 钩子也不再执行。

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRestartAfterStop_RebuildsRootScope(t *testing.T) {
	eng := newTestPluginEngine(t)
	pm := NewManager(eng)
	bus := pm.GetEventBus()

	var mu sync.Mutex
	var subErrs []error
	hooks := 0

	require.NoError(t, pm.Register(&Descriptor{
		Name: "restart",
		Setup: func(ctx *SetupContext) (any, error) {
			_, err := ctx.Subscribe("restart.topic", func(context.Context, any) error { return nil })
			mu.Lock()
			subErrs = append(subErrs, err)
			mu.Unlock()
			ctx.OnDispose(func() error { hooks++; return nil })
			return &struct{}{}, nil
		},
	}))

	require.NoError(t, pm.StartAll(context.Background()))
	require.Equal(t, 1, bus.GetStats().SubscriptionCount)
	require.NoError(t, pm.StopAll(context.Background()))
	assert.Equal(t, 0, bus.GetStats().SubscriptionCount)

	require.NoError(t, pm.StartAll(context.Background()))
	mu.Lock()
	require.Len(t, subErrs, 2)
	assert.NoError(t, subErrs[1], "重启后的 Setup 必须拿到全新根 Scope")
	mu.Unlock()
	assert.Equal(t, 1, bus.GetStats().SubscriptionCount, "重启后订阅必须重新生效")

	require.NoError(t, pm.StopAll(context.Background()))
	assert.Equal(t, 2, hooks, "重启后的 OnDispose 钩子也必须执行")
}
