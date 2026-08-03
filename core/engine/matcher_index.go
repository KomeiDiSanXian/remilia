package engine

// matcher_index.go — 候选匹配器检索源抽象
//
// MatcherIndex 是 RoutingStrategy 的候选来源插件点：
// 每个索引拥有自己的索引结构与检索算法（HashMap/Trie/DFA…），
// 引擎只消费 Candidates 返回的有序候选流做全局归并。
//
// 约定：
//   - 每个子列表必须已按优先级升序排列（引擎按全局优先级归并）
//   - Candidates 必须是纯读（引擎在无锁读路径上调用）
//   - Band 由索引决定；matcher 无 Band 概念（框架级，非用户配置）
//
// 相关设计：docs/notes/25-routing-strategy.md
//
// 注意：Candidates 通过 MatcherEnv（而非 *state）访问引擎数据，
// 以保证第三方包可以独立实现 MatcherIndex（*state 为未导出类型，
// 出现在接口签名中会封闭外部实现）。

import "github.com/KomeiDiSanXian/remilia/core/context"

// MatcherIndex 是一类 Matcher 的候选检索源。
type MatcherIndex interface {
	// Candidates 返回当前事件下的候选匹配器流（specific/generic 分槽）。
	// 返回的 MatcherCandidates 按值传递，零分配。
	Candidates(env MatcherEnv, ctx *context.Context) MatcherCandidates

	// Band 返回该索引所属的优先级带（BandFast/BandSlow）。
	// 由索引决定，matcher 不暴露 Band API。
	Band() RoutingBand
}

// MatcherEnv 是 MatcherIndex 在无锁读路径上可访问的引擎只读数据视图。
//
// 由引擎每次 Plan 时以值形式提供（零分配）。索引不得缓存或修改
// 返回值引用的数据——底层数组可能被 COW 写路径共享。
type MatcherEnv interface {
	// SortedCache 返回指定事件类型的永久匹配器（已按优先级升序、
	// 已过滤无 handler 的匹配器）。eventType 传 "" 获取通用类型。
	SortedCache(eventType EventType) []*Matcher

	// CommandCandidates 返回命令词的候选匹配器。
	// 第一返回值是事件类型精确匹配，第二返回值是通用类型。
	// 命令词不存在时返回 (nil, nil)。
	CommandCandidates(cmd string, eventType EventType) (specific, generic []*Matcher)

	// RegexCandidates 返回正则索引中指定事件类型的匹配器桶
	// （Regex() 注册，已按优先级升序，未过滤正则是否命中——
	// 由 regexIndex 在候选生成阶段逐条预匹配）。
	RegexCandidates(eventType EventType) []*Matcher
}

// matcherEnv 是 MatcherEnv 的默认实现（引擎内部）。
type matcherEnv struct {
	st *state
}

func (e matcherEnv) SortedCache(eventType EventType) []*Matcher {
	return e.st.sortedCache[eventType]
}

func (e matcherEnv) CommandCandidates(cmd string, eventType EventType) ([]*Matcher, []*Matcher) {
	if m, ok := e.st.commandIndex[cmd]; ok {
		return m[eventType], m[""]
	}
	return nil, nil
}

func (e matcherEnv) RegexCandidates(eventType EventType) []*Matcher {
	return e.st.regexIndex[eventType]
}

// MatcherCandidates 是一个索引贡献的候选流集合（按值返回，零分配）。
//
// 槽 0 = specific（精确事件类型），槽 1 = generic（"" 通用类型）。
// generic 构建期合并落地后只填槽 0。引擎内部直接访问槽位，
// 第三方索引通过 Add / AddMeta 填充。
//
// metas 是与 lists 平行的可选 Meta 数组（1:1 对齐）——只有候选生成
// 阶段已产生匹配结果（如 regexIndex 的捕获组）的索引才填写，
// 快带索引保持无 Meta（零成本）。
type MatcherCandidates struct {
	lists [2][]*Matcher
	metas [2][]any
	n     int
}

// Add 追加一个非空候选流（无 Meta）。空列表被忽略；超过容量（2 路）的流被丢弃。
func (c *MatcherCandidates) Add(list []*Matcher) {
	if len(list) > 0 && c.n < len(c.lists) {
		c.lists[c.n] = list
		c.n++
	}
}

// AddMeta 追加一个携带逐条 Meta 的候选流（metas 与 list 1:1 对齐）。
// 长度不匹配或超容量时静默丢弃。
func (c *MatcherCandidates) AddMeta(list []*Matcher, metas []any) {
	if len(list) > 0 && len(metas) == len(list) && c.n < len(c.lists) {
		c.lists[c.n] = list
		c.metas[c.n] = metas
		c.n++
	}
}
