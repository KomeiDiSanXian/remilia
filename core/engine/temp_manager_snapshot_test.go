package engine

// temp_manager_snapshot_test.go — TempManager RCU 快照的增量维护
//
// 回归背景：快照曾只在全量 rebuild 时更新，Add/Remove 后无法即时反映；
// 改为 COW 增量后仍需保证乱序 Add 仍按优先级有序、Remove 立即生效、
// 通用（eventType==""）列表走同一条增量路径。

import (
	"testing"
)

// TestTempManager_SnapshotIncrementalOrdering 验证乱序 Add 后快照仍按
// 优先级有序，Remove 后即时反映。
func TestTempManager_SnapshotIncrementalOrdering(t *testing.T) {
	tm := newTempMatcherManager()
	et := EventType("SNAPSHOT_ET")
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
