package engine

// review_fixes_test.go — 2026-07 core 深度复查修复的回归测试
//
// 覆盖：
//  1. COW 状态不再被就地排序改写（命令桶重排 / 批量注册）
//  2. 批量注册维护命令元数据缓存
//  3. WithSharedExecPool 不被 NewEngine 覆盖，Shutdown 不停共享池
//  4. Use 的中间件变更传播到 TempManager 中的活跃临时 matcher
//  5. OnTemp 之后 SetTempWithTimeout 能正确入过期堆并被清理
//  6. OnCommand 链式元数据设置即时反映到命令缓存
//  7. DeleteMatcher 批量路径 / 同步回退 / FlushPendingDeletes
//  8. 优先级为 MaxUint64 的 matcher 不再被归并迭代器跳过

import (
	stdctx "context"
	"math"
	"testing"
	"testing/synctest"
	"time"

	"github.com/KomeiDiSanXian/remilia/command"
	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// TestReviewFix_CommandReorderDoesNotMutateOldState 验证对命令 matcher 调用
// SetPriority 触发的命令桶重排不会就地改写旧 state 共享的底层数组。
func TestReviewFix_CommandReorderDoesNotMutateOldState(t *testing.T) {
	eng := newEngineForTest(t)
	et := string(platform.EventKindPrivateMessage)

	m1 := eng.OnCommand(et, "/prio")
	m1.Handle(func(ctx *context.Context) error { return nil })
	m2 := eng.OnCommand(et, "/prio")
	m2.Handle(func(ctx *context.Context) error { return nil })

	oldList := eng.state.Load().commandIndex["/prio"][et]
	if len(oldList) != 2 {
		t.Fatalf("expected 2 matchers in command bucket, got %d", len(oldList))
	}
	snapshot := append([]*Matcher(nil), oldList...)

	// 触发 withUpdatedMatcherIndex 的命令桶重排路径
	m2.SetPriority(1)

	// 旧 state 的切片必须保持原有元素与顺序（COW 契约：不得被就地排序改写）
	for i := range snapshot {
		if oldList[i] != snapshot[i] {
			t.Fatalf("old state command bucket mutated in place at index %d: COW broken", i)
		}
	}

	// 新 state 中优先级更小的 m2 应排在前面
	newList := eng.state.Load().commandIndex["/prio"][et]
	if len(newList) != 2 || newList[0] != m2 || newList[1] != m1 {
		t.Fatal("new state command bucket should be re-sorted with m2 first")
	}
}

// TestReviewFix_BatchRegisterMaintainsCommandInfoCache 验证批量注册的命令
// matcher 会进入 commandIndex 并同步维护命令元数据缓存（此前批量路径漏掉了
// commandInfoCache 更新，/help 看不到批量注册的命令）。
func TestReviewFix_BatchRegisterMaintainsCommandInfoCache(t *testing.T) {
	eng := newEngineForTest(t)
	et := string(platform.EventKindPrivateMessage)

	m := &Matcher{
		EventType:     et,
		Rules:         []context.Rule{context.OnCommand("/batchcmd")},
		coordinator:   eng,
		Source:        "global",
		triggerPrefix: "/",
		definition:    &command.Definition{Name: "batchcmd", Description: "批量注册"},
		Handler:       func(ctx *context.Context) error { return nil },
		execProfile:   newExecProfile(),
	}
	m.commandIndexed.Store(true)
	m.priority.Store(50)

	eng.BatchRegisterMatchers([]*Matcher{m})

	info := eng.FindCommand("/batchcmd")
	if info == nil {
		t.Fatal("batch-registered command missing from command info cache")
	}
	if info.Description != "批量注册" {
		t.Fatalf("Description = %q, want %q", info.Description, "批量注册")
	}
	if lst := eng.state.Load().commandIndex["/batchcmd"][et]; len(lst) != 1 || lst[0] != m {
		t.Fatal("batch-registered command matcher not present in commandIndex")
	}
}

// TestReviewFix_SharedExecPoolNotOverwritten 验证 WithSharedExecPool 注入的
// 共享池不再被 NewEngine 覆盖，且 Engine.Shutdown 不会停掉共享池。
func TestReviewFix_SharedExecPoolNotOverwritten(t *testing.T) {
	pool := NewExecPool(ExecPoolConfig{MaxConcurrency: 2, QueueSize: 4})
	eng := NewEngine(WithNoBackgroundWorkers(), WithSharedExecPool(pool))

	if eng.internals.execPool != pool {
		t.Fatal("WithSharedExecPool pool was overwritten by NewEngine")
	}

	if err := eng.Shutdown(stdctx.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if pool.stopped.Load() {
		t.Fatal("Engine.Shutdown must not stop a shared ExecPool")
	}

	// 共享池在 Engine 关闭后仍可用，由调用方负责停止
	done := make(chan struct{})
	if !pool.TrySubmit(func() { close(done) }) {
		t.Fatal("shared pool should still accept tasks after engine shutdown")
	}
	<-done
	pool.Stop()
}

// TestReviewFix_MiddlewareChangeReachesTempMatcher 验证运行期 Engine.Use 注册的
// 全局中间件会传播到 TempManager 中已存在的临时 matcher（此前快路径永远走旧链）。
func TestReviewFix_MiddlewareChangeReachesTempMatcher(t *testing.T) {
	eng := newEngineForTest(t)
	et := string(platform.EventKindPrivateMessage)

	var order []string
	m := eng.OnTemp(et)
	m.SetTempWithMaxUse(10)
	m.Handle(func(ctx *context.Context) error {
		order = append(order, "handler")
		return nil
	})

	evt := newTestPlatformEvent(platform.EventKindPrivateMessage)
	eng.ProcessEvent(context.NewContextFromEvent(evt, nil))
	if len(order) != 1 || order[0] != "handler" {
		t.Fatalf("first event should hit bare handler, got %v", order)
	}

	// 运行期新增全局中间件：已存在的临时 matcher 也必须重建链
	eng.Use(func(next context.Handler) context.Handler {
		return func(ctx *context.Context) error {
			order = append(order, "mw")
			return next(ctx)
		}
	})

	order = order[:0]
	eng.ProcessEvent(context.NewContextFromEvent(evt, nil))
	if len(order) != 2 || order[0] != "mw" || order[1] != "handler" {
		t.Fatalf("temp matcher should run through the new middleware chain, got %v", order)
	}
}

// TestReviewFix_TempTimeoutAfterOnTempExpires 验证 OnTemp 创建的 matcher
// 再调用 SetTempWithTimeout 后能正确登记过期堆并在超时后被清理
// （此前已是 temp 的 matcher 设超时不会入堆，永不过期）。
func TestReviewFix_TempTimeoutAfterOnTempExpires(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		eng := newEngineForTest(t)

		m := eng.OnTemp(string(platform.EventKindPrivateMessage))
		m.Handle(func(ctx *context.Context) error { return nil })
		m.SetTempWithTimeout(50 * time.Millisecond)

		if got := eng.GetTempMatcherCount(); got != 1 {
			t.Fatalf("temp count = %d, want 1", got)
		}

		time.Sleep(100 * time.Millisecond)
		eng.cleanExpiredMatchers()

		if !m.IsDeleted() {
			t.Fatal("temp matcher with timeout set after OnTemp should expire and be marked deleted")
		}
		if got := eng.GetTempMatcherCount(); got != 0 {
			t.Fatalf("temp count after cleanup = %d, want 0", got)
		}
	})
}

// TestReviewFix_ChainedMetadataRefreshesCommandCache 验证文档推荐的
// OnCommand().SetDescription().Handle() 链式写法的元数据能立即反映到
// GetAllCommands/FindCommand（此前缓存停留在注册瞬间的空元数据）。
func TestReviewFix_ChainedMetadataRefreshesCommandCache(t *testing.T) {
	eng := newEngineForTest(t)
	et := string(platform.EventKindPrivateMessage)

	m := eng.OnCommand(et, "/desc")
	m.SetDescription("描述文本").SetUsage("/desc <x>")
	m.Handle(func(ctx *context.Context) error { return nil })

	info := eng.FindCommand("/desc")
	if info == nil {
		t.Fatal("command not found in cache")
	}
	if info.Description != "描述文本" || info.Usage != "/desc <x>" {
		t.Fatalf("stale command cache: Description=%q Usage=%q", info.Description, info.Usage)
	}

	// SetHidden(true) 应立即将命令从列表缓存移除
	m.SetHidden(true)
	if eng.FindCommand("/desc") != nil {
		t.Fatal("hidden command should disappear from command cache immediately")
	}
}

// TestReviewFix_DeleteMatcherBatchedWithProcessor 验证批量删除路径：
// DeleteMatcher 立即置 deleted，物理移除由处理器按配置间隔异步完成。
func TestReviewFix_DeleteMatcherBatchedWithProcessor(t *testing.T) {
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

// TestReviewFix_DeleteMatcherSyncFallbackAndFlush 验证处理器未运行时
// DeleteMatcher 退化为同步删除，以及 FlushPendingDeletes 的确定性收尾。
func TestReviewFix_DeleteMatcherSyncFallbackAndFlush(t *testing.T) {
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

// TestReviewFix_CooldownDeferredCommit 验证 OnCooldown 的延迟副作用语义：
// 规则通过但所在 matcher 未命中（后续规则失败）时不消耗冷却，
// 只有确认命中的 matcher 才提交冷却写入。
func TestReviewFix_CooldownDeferredCommit(t *testing.T) {
	eng := newEngineForTest(t)
	et := string(platform.EventKindPrivateMessage)
	key := func(*context.Context) string { return "review_cd_user" }

	// matcher A：OnCooldown 在前，后续规则恒失败 → 永不命中，也不得消耗冷却
	mA := eng.On(et,
		context.OnCooldown(time.Hour, key),
		func(*context.Context) bool { return false },
	)
	mA.Handle(func(ctx *context.Context) error {
		t.Error("matcher A must never fire")
		return nil
	})

	// matcher B：优先级更低（后执行），带同 key 的 OnCooldown
	var fired int
	mB := eng.On(et, context.OnCooldown(time.Hour, key))
	mB.SetPriority(60)
	mB.Handle(func(ctx *context.Context) error {
		fired++
		return nil
	})

	evt := newTestPlatformEvent(platform.EventKindPrivateMessage)

	// 第一个事件：A 的冷却检查通过但被丢弃（规则链失败），B 正常命中并提交冷却
	eng.ProcessEvent(context.NewContextFromEvent(evt, nil))
	if fired != 1 {
		t.Fatalf("matcher B should fire on first event (cooldown not pre-consumed by A), fired=%d", fired)
	}

	// 第二个事件：冷却已由 B 提交 → B 不再命中
	eng.ProcessEvent(context.NewContextFromEvent(evt, nil))
	if fired != 1 {
		t.Fatalf("cooldown should block second event, fired=%d", fired)
	}
}

// TestReviewFix_TempSnapshotIncrementalOrdering 验证 TempManager 快照的
// 增量维护：乱序 Add 后 Get 仍按优先级有序，Remove 后即时反映。
func TestReviewFix_TempSnapshotIncrementalOrdering(t *testing.T) {
	tm := newTempMatcherManager()
	et := EventType("REVIEW_TEMP_ET")
	mk := func(p uint64) *Matcher {
		m := &Matcher{EventType: et}
		m.priority.Store(p)
		return m
	}
	m30, m10, m20 := mk(30), mk(10), mk(20)

	tm.Add(m30)
	tm.Add(m10)
	tm.Add(m20)

	got := tm.Get(et)
	if len(got) != 3 || got[0] != m10 || got[1] != m20 || got[2] != m30 {
		t.Fatalf("incremental snapshot must stay priority-sorted, got %d entries", len(got))
	}

	tm.Remove(m20)
	got = tm.Get(et)
	if len(got) != 2 || got[0] != m10 || got[1] != m30 {
		t.Fatalf("snapshot must reflect removal immediately, got %d entries", len(got))
	}

	// 通用（eventType==""）列表同样走增量路径
	g := &Matcher{EventType: ""}
	g.priority.Store(5)
	tm.Add(g)
	if lst := tm.Get(""); len(lst) != 1 || lst[0] != g {
		t.Fatal("generic list must be maintained incrementally")
	}
	tm.Remove(g)
	if lst := tm.Get(""); len(lst) != 0 {
		t.Fatal("generic list must be empty after removal")
	}
}

// TestReviewFix_MaxPriorityMatcherNotSkipped 验证优先级为 MaxUint64 的
// matcher 不再被归并迭代器的哨兵值判定提前终止（此前会被整体跳过）。
func TestReviewFix_MaxPriorityMatcherNotSkipped(t *testing.T) {
	eng := newEngineForTest(t)
	et := string(platform.EventKindPrivateMessage)

	var hits []string
	m1 := eng.On(et)
	m1.Handle(func(ctx *context.Context) error {
		hits = append(hits, "normal")
		return nil
	})
	m2 := eng.On(et)
	m2.SetPriority(math.MaxUint64)
	m2.Handle(func(ctx *context.Context) error {
		hits = append(hits, "max")
		return nil
	})

	evt := newTestPlatformEvent(platform.EventKindPrivateMessage)
	eng.ProcessEvent(context.NewContextFromEvent(evt, nil))

	if len(hits) != 2 || hits[0] != "normal" || hits[1] != "max" {
		t.Fatalf("matcher with MaxUint64 priority must still run (last), got %v", hits)
	}
}
