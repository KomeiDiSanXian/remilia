package plugin

// force_unregister_cleanup_test.go — 强制注销/重试路径的资源回收回归测试

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestForceUnregister_CleansScopeAndGoroutines(t *testing.T) {
	pm := NewManager(nil)

	var (
		disposed atomic.Bool
		exited   atomic.Bool
	)
	enterGM := make(chan struct{})

	desc := &Descriptor{
		Name: "broken",
		Setup: func(ctx *SetupContext) (any, error) {
			root := ctx.Scope()
			root.OnDispose(func() error {
				disposed.Store(true)
				return nil
			})
			ctx.Spawn(func(gctx context.Context) {
				close(enterGM)
				<-gctx.Done()
				exited.Store(true)
			})
			return "broken-api", nil
		},
	}
	require.NoError(t, pm.Register(desc))

	<-enterGM // 确认 goroutine 已启动（Setup 返回后仍活着）

	// 强制注销：不执行 Teardown，但 Scope 与 goroutine 必须回收
	require.NoError(t, pm.ForceUnregister("broken"))

	require.Eventually(t, disposed.Load, 2*time.Second, 5*time.Millisecond,
		"强制注销必须执行 Scope 清理钩子")
	require.Eventually(t, exited.Load, 2*time.Second, 5*time.Millisecond,
		"强制注销必须停止受管 goroutine")
	assert.Equal(t, 0, pm.Count())
	for _, n := range pm.GetLoadOrder() {
		assert.NotEqual(t, "broken", n, "loadOrder 应清理")
	}
}

func TestRetry_DoesNotDoubleHoldResources(t *testing.T) {
	pm := NewManager(nil)

	var setups, disposes atomic.Int32
	desc := &Descriptor{
		Name: "retryable",
		Setup: func(ctx *SetupContext) (any, error) {
			setups.Add(1)
			ctx.Scope().OnDispose(func() error {
				disposes.Add(1)
				return nil
			})
			return nil, nil
		},
	}

	require.NoError(t, pm.Register(desc))

	// 模拟损坏：置为 Error 后 Retry（内部走 ForceUnregister → Register）
	pm.mu.RLock()
	inst := pm.plugins["retryable"]
	pm.mu.RUnlock()
	require.NotNil(t, inst)
	inst.SetState(Error)

	require.NoError(t, pm.Retry("retryable", desc))

	assert.Equal(t, int32(2), setups.Load(), "Retry 应重新执行 Setup")
	require.Eventually(t, func() bool { return disposes.Load() >= 1 }, 2*time.Second, 5*time.Millisecond,
		"Retry 前的强制注销必须回收旧的 Scope 资源")

	// loadOrder 中只应有一份
	count := 0
	for _, n := range pm.GetLoadOrder() {
		if n == "retryable" {
			count++
		}
	}
	assert.Equal(t, 1, count, "重试注册不应在 loadOrder 中重复追加")
	_ = pm.StopAll(context.Background())
}
