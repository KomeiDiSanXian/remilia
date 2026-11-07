package engine

// merge_iter_max_priority_test.go — 归并迭代器的哨兵值边界
//
// 回归背景：Next() 曾用 MaxUint64 作"无候选"哨兵，优先级恰为 MaxUint64
// 的 matcher 会被判为"没有候选"而整体跳过（提前终止整个归并）。

import (
	"math"
	"testing"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// TestEngine_MaxPriorityMatcherNotSkipped 验证优先级为 MaxUint64 的
// matcher 仍然会被执行（排在最后）。
func TestEngine_MaxPriorityMatcherNotSkipped(t *testing.T) {
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
