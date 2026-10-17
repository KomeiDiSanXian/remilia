package plugin

// batch_retry_cleanup_test.go — 批量注册失败尝试必须回收资源的回归测试
//
// 回归背景：批注册的失败路径只删 plugins 表项 + RemoveGroup + 容器 key。
// Setup 已经跑过时，它的 rootScope（EventBus 订阅、OnDispose 钩子、子 Scope）
// 与 Spawn 的 goroutine 全部泄漏；依赖感知重试还会再跑一次 Setup，于是每个
// 重试轮次都叠加一层上一轮的订阅与后台任务。

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegisterBatch_FailedAttemptsReclaimScopeResources(t *testing.T) {
	pm := NewManager(nil)

	var (
		setups   atomic.Int32
		disposes atomic.Int32
		exited   atomic.Int32
	)

	desc := &Descriptor{
		Name: "batch-fail",
		Setup: func(ctx *SetupContext) (any, error) {
			setups.Add(1)
			ctx.Scope().OnDispose(func() error {
				disposes.Add(1)
				return nil
			})
			ctx.Spawn(func(gctx context.Context) {
				<-gctx.Done()
				exited.Add(1)
			})
			return nil, assert.AnError
		},
	}
	// 第二个描述符让注册走批量路径（单插件会退化为 registerSingle）
	okDesc := &Descriptor{
		Name:  "batch-ok",
		Setup: func(*SetupContext) (any, error) { return nil, nil },
	}

	err := pm.RegisterBatch(context.Background(), []*Descriptor{okDesc, desc})
	require.Error(t, err)
	assert.Equal(t, int32(1), setups.Load())

	require.Eventually(t, func() bool { return disposes.Load() >= 1 }, 5*time.Second, 10*time.Millisecond,
		"批量注册失败必须执行 Scope 清理钩子")
	require.Eventually(t, func() bool { return exited.Load() >= 1 }, 5*time.Second, 10*time.Millisecond,
		"批量注册失败必须停止 Setup 中启动的 goroutine")

	// 失败插件必须从表中移除（其余插件是否注册取决于拓扑顺序：
	// 非原子模式下失败即停止注册后续插件，两种顺序都合法）
	pm.mu.RLock()
	_, exists := pm.plugins["batch-fail"]
	pm.mu.RUnlock()
	assert.False(t, exists, "失败插件必须从表中移除")
}
