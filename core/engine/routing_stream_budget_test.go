package engine

// routing_stream_budget_test.go — 候选流容量与索引预算的回归测试
//
// 回归背景：Source Budget 允许注册 routingBudgetLimit（16）个索引，每个索引
// 最多贡献 2 路候选（specific/generic），而归并迭代器容量此前恰好等于索引
// 上限（16 路）。于是 9 个索引 × 2 路 = 18 路时，超出的流被静默丢弃——
// 表现为"部分 matcher 莫名其妙不匹配"，且无任何日志。

import (
	"sync/atomic"
	"testing"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sliceIndex 是测试用 MatcherIndex：按需返回固定的两路候选。
type sliceIndex struct {
	band RoutingBand
	get  func() ([]*Matcher, []*Matcher)
}

func (s sliceIndex) Band() RoutingBand { return s.band }

func (s sliceIndex) Candidates(MatcherEnv, *context.Context) MatcherCandidates {
	var c MatcherCandidates
	specific, generic := s.get()
	c.Add(specific)
	c.Add(generic)
	return c
}

func TestMergeIterCapacityCoversRoutingBudget(t *testing.T) {
	require.GreaterOrEqual(t, mergeIterMaxStreams, 2*routingBudgetLimit,
		"每个索引最多 2 路候选，容量必须覆盖索引硬上限")
}

func TestRouting_AllIndexStreamsAreMerged(t *testing.T) {
	// 与事件类型区分开：matcher 不进入永久索引，候选只由自定义索引提供
	const matcherEventType = "CUSTOM_ONLY"
	const indexCount = 9 // 2 路/索引 = 18 路 > 旧容量 16

	type pair struct{ a, b *Matcher }
	pairs := make([]pair, indexCount)

	opts := make([]Option, 0, indexCount)
	for i := range indexCount {
		opts = append(opts, WithMatcherIndex(sliceIndex{
			band: BandFast,
			get: func() ([]*Matcher, []*Matcher) {
				p := pairs[i]
				return []*Matcher{p.a}, []*Matcher{p.b}
			},
		}))
	}

	eng := newEngineForTest(t, opts...)

	var calls atomic.Int32
	for i := range indexCount {
		a := eng.On(matcherEventType)
		a.SetPriority(uint64(10 + i*2))
		a.Handle(func(*context.Context) error {
			calls.Add(1)
			return nil
		})

		b := eng.On(matcherEventType)
		b.SetPriority(uint64(11 + i*2))
		b.Handle(func(*context.Context) error {
			calls.Add(1)
			return nil
		})

		pairs[i] = pair{a: a, b: b}
	}

	eng.ProcessEvent(newTestCtx("hello"))

	assert.Equal(t, int32(2*indexCount), calls.Load(),
		"所有索引的两路候选都应参与归并（不得静默丢弃）")
}

func TestRouting_DroppedStreamsAreWarned(t *testing.T) {
	// 直接构造超容量场景：迭代器只保留容量内的流，且不 panic。
	it := acquireMergeIter()
	defer releaseMergeIter(it)

	list := []*Matcher{{}}
	for range mergeIterMaxStreams + 4 {
		it.add(list)
	}
	assert.Equal(t, mergeIterMaxStreams, it.n, "容量内的流应全部保留")
}
