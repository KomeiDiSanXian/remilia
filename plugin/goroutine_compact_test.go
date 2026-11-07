package plugin

// goroutine_compact_test.go — 受管 goroutine 条目表压实的回归测试
//
// 回归背景：goNamed_ 每次追加一条 GoroutineInfo，goroutine 退出只把 IsAlive
// 置 false、条目永不移除。ctx.After 这类一次性任务会让条目数随调用次数单调
// 增长（长驻插件上就是稳定的内存泄漏）。

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGoroutineList_CompactsExitedEntries(t *testing.T) {
	gm := newGoroutineManagerForPlugin("compact")
	defer gm.stopAndWait()

	const spawns = goroutineListCompactThreshold * 2
	for range spawns {
		gm.goNamed_("once", func(context.Context) {})
	}
	require.NoError(t, gm.stopAndWaitCtx(context.Background()))

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		// 通过公开读路径触发压实（未超过阈值时写路径也不会压实）
		_ = gm.listGoroutines()
		gm.mu.Lock()
		total := len(gm.goroutines)
		gm.mu.Unlock()
		if total <= goroutineListCompactThreshold {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	gm.mu.Lock()
	total := len(gm.goroutines)
	gm.mu.Unlock()
	assert.LessOrEqual(t, total, goroutineListCompactThreshold,
		"已退出条目必须被压实（实际 %d 条，共 Spawn %d 次）", total, spawns)
	assert.Empty(t, gm.listGoroutines())
}
