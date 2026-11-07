package engine

// matcher_delete_pending_test.go — DeleteMatcher 的批量删除路径与同步回退
//
// 语义：DeleteMatcher 立即把 matcher 标记为 deleted（马上停止匹配），
// 物理移除交给后端批量处理器；处理器未运行或队列满时退化为同步删除。
// FlushPendingDeletes 用于需要确定性时机的场景（测试、Shutdown 收尾）。

import (
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// TestEngine_DeleteMatcherBatchedWithProcessor 验证批量删除路径：
// DeleteMatcher 立即置 deleted，物理移除由处理器按配置间隔异步完成。
func TestEngine_DeleteMatcherBatchedWithProcessor(t *testing.T) {
	eng := newEngineForTest(t, WithPendingDeleteProcessInterval(5*time.Millisecond))

	m := eng.On(string(platform.EventKindPrivateMessage))
	m.Handle(func(ctx *context.Context) error { return nil })

	eng.DeleteMatcher(m)

	if !m.IsDeleted() {
		t.Fatal("DeleteMatcher must mark matcher deleted immediately")
	}

	deadline := time.Now().Add(2 * time.Second)
	for eng.GetMatcherCount() != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("pending delete processor did not remove matcher, count=%d", eng.GetMatcherCount())
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// TestEngine_DeleteMatcherSyncFallbackAndFlush 验证处理器未运行时
// DeleteMatcher 退化为同步删除，以及 FlushPendingDeletes 的确定性收尾。
func TestEngine_DeleteMatcherSyncFallbackAndFlush(t *testing.T) {
	eng := newEngineForTest(t) // WithNoBackgroundWorkers：处理器未运行

	m := eng.On(string(platform.EventKindPrivateMessage))
	m.Handle(func(ctx *context.Context) error { return nil })
	before := eng.GetMatcherCount()

	eng.DeleteMatcher(m)
	if got := eng.GetMatcherCount(); got != before-1 {
		t.Fatalf("without processor DeleteMatcher must be synchronous: count=%d want %d", got, before-1)
	}

	// FlushPendingDeletes 消费通道中已排队的待删除项
	m2 := eng.On(string(platform.EventKindPrivateMessage))
	m2.Handle(func(ctx *context.Context) error { return nil })
	m2.rt.deleted.Store(true)
	eng.internals.pendingDeleteCh <- m2
	eng.FlushPendingDeletes()
	if got := eng.GetMatcherCount(); got != before-1 {
		t.Fatalf("FlushPendingDeletes should remove queued matcher: count=%d want %d", got, before-1)
	}
}
