package engine

// index_permanent.go — 永久匹配器索引（BandFast）

import "github.com/KomeiDiSanXian/remilia/core/context"

// permanentIndex 从 COW state 的 sortedCache 检索永久注册的匹配器。
// 无门控，纯查找：列表由写路径维护（已过滤无 handler、已按优先级排序）。
type permanentIndex struct{}

// Band 返回 BandFast。
func (permanentIndex) Band() RoutingBand { return BandFast }

// Candidates 返回事件类型精确匹配 + 通用类型两路候选流。
//
// 事件类型为空时（未绑定平台事件的 Context）specific 与 generic 是
// 同一个列表，只取一路——叠加两路会让每个通用 matcher 在候选流中出现
// 两次、handler 被执行两次。
func (permanentIndex) Candidates(env MatcherEnv, ctx *context.Context) MatcherCandidates {
	var c MatcherCandidates
	eventType := ctx.GetEventType()
	c.Add(env.SortedCache(eventType))
	if eventType != "" {
		c.Add(env.SortedCache(""))
	}
	return c
}
