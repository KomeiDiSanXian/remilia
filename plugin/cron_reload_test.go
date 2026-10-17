package plugin

// cron_reload_test.go — InPlace 重载不得累积 cron 调度器的回归测试
//
// 回归背景：cron 调度器懒创建并绑定在 goroutine 管理器上，而 InPlace 重载
// 不经过 unload（goroutine 管理器被继承）。旧 Setup 的调度器继续运行，
// 新 Setup 每次 RegisterCron 又建一个——同一个定时任务按重载次数重复执行。

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInPlaceReload_StopsOldCronScheduler(t *testing.T) {
	pm := NewManager(nil)

	var setups atomic.Int32
	oldCtxCh := make(chan *SetupContext, 1)

	desc := &Descriptor{
		Name: "cron-reload",
		Setup: func(ctx *SetupContext) (any, error) {
			// 注意：Setup 运行在独立 goroutine，不能用 require（FailNow 只能从
			// 测试 goroutine 调用），错误必须作为返回值传出。
			if err := ctx.RegisterCron("0 0 2 * * *", func() {}); err != nil {
				return nil, err
			}
			if setups.Add(1) == 1 {
				oldCtxCh <- ctx
			}
			return nil, nil
		},
		Advanced: &Advanced{
			Strategy: ReloadInPlace,
			Reload: func(ctx *SetupContext) error {
				// InPlace 重载：重新注册定时任务
				return ctx.RegisterCron("0 0 2 * * *", func() {})
			},
		},
	}

	require.NoError(t, pm.Register(desc))
	oldCtx := <-oldCtxCh
	require.NotNil(t, oldCtx.cronScheduler, "首次 Setup 应创建调度器")

	require.NoError(t, pm.Reload(context.Background(), "cron-reload"))

	assert.Nil(t, oldCtx.cronScheduler, "InPlace 重载必须停掉旧 Setup 的调度器")

	// 新上下文仍拥有可用调度器（重载时注册的定时任务照常生效）
	pm.mu.RLock()
	inst := pm.plugins["cron-reload"]
	pm.mu.RUnlock()
	require.NotNil(t, inst)
	inst.mu.RLock()
	newCtx := inst.setupContext
	inst.mu.RUnlock()
	require.NotNil(t, newCtx)
	assert.NotNil(t, newCtx.cronScheduler, "重载后的上下文应可再次注册定时任务")

	// 卸载后调度器被生命周期回收
	require.NoError(t, pm.StopAll(context.Background()))
	assert.Nil(t, newCtx.cronScheduler, "停机应停掉调度器")
}

func TestSetupContext_StopCronIsIdempotentAndRestartable(t *testing.T) {
	ctx := NewTestSetupContext("cron-unit", nil)
	t.Cleanup(func() { StopTestSetupContext(ctx) })

	require.NoError(t, ctx.RegisterCron("0 0 2 * * *", func() {}))
	first := ctx.cronScheduler
	require.NotNil(t, first)

	ctx.stopCron()
	assert.Nil(t, ctx.cronScheduler)
	ctx.stopCron() // 幂等：再次调用不应 panic

	// stopCron 后仍可重新注册（不会再出现 nil 解引用）
	require.NoError(t, ctx.RegisterCron("0 0 3 * * *", func() {}))
	require.NotNil(t, ctx.cronScheduler)
	assert.NotSame(t, first, ctx.cronScheduler, "应创建新的调度器")
}
