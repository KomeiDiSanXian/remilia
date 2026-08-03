package engine

// index_temp.go — 临时匹配器索引（BandFast）

import "github.com/KomeiDiSanXian/remilia/core/context"

// tempIndex 从 TempManager 检索临时匹配器（会话等待、一次性命令等）。
// 持有 *tempMatcherManager 引用（位于 engine.internals，COW 替换不涉及）。
type tempIndex struct {
	tm *tempMatcherManager
}

// Band 返回 BandFast。
func (tempIndex) Band() RoutingBand { return BandFast }

// Candidates 无临时匹配器时零开销短路（HasAny 原子计数）；
// 有候选时读取 RCU 快照（按 eventType 预归并、预排序），返回两路。
func (i tempIndex) Candidates(env MatcherEnv, ctx *context.Context) MatcherCandidates {
	var c MatcherCandidates
	if i.tm.HasAny() {
		c.Add(i.tm.Get(ctx.GetEventType()))
		c.Add(i.tm.Get(""))
	}
	return c
}
