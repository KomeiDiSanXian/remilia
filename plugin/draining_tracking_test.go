package plugin

// draining_tracking_test.go — 蓝绿重载 draining 追踪相互独立的回归测试
//
// 回归背景：draining 追踪以插件名为 key。同一插件连续两次蓝绿重载会互相覆盖
// 条目：第一次的清理回调 markDrainingDone(name) 会把第二次标成完成，
// StopAll 从此不再等待第二次的旧实例 Teardown（旧 handler 在停机后仍留在
// 总线上处理事件）。

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDrainingTracking_TwoReloadsAreIndependent(t *testing.T) {
	pm := NewManager(nil)

	first := pm.stats.trackDraining("bg")
	second := pm.stats.trackDraining("bg")
	require.NotEqual(t, first, second, "每次追踪必须拥有独立令牌")

	// 第一次清理完成
	pm.stats.markDrainingDone(first, nil)

	infos := pm.ListDrainingInstances()
	require.Contains(t, infos, "bg")
	assert.False(t, infos["bg"].Done, "第一次完成不得把第二次标成完成")

	// StopAll 的等待必须覆盖尚未完成的第二次清理（ctx 到期才返回）
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	start := time.Now()
	pm.stats.drainDrainingInstances(ctx)
	assert.GreaterOrEqual(t, time.Since(start), 80*time.Millisecond,
		"必须等待未完成的第二次清理")

	pm.stats.markDrainingDone(second, nil)
	infos = pm.ListDrainingInstances()
	assert.True(t, infos["bg"].Done, "全部清理完成后应显示 Done")

	// 全部完成后立即返回
	doneCtx, doneCancel := context.WithTimeout(context.Background(), time.Second)
	defer doneCancel()
	pm.stats.drainDrainingInstances(doneCtx)
}
