// stabilize.go — 会话级工具集稳定策略（提示词前缀缓存的第二道闸）。
//
// 背景：tools 是请求的顶级字段，在 LLM 的提示词序列里排在 system 之前，
// 等价于一个"全局前缀开关"——工具集合一变，其后的稳定 System Prompt 与
// 整段会话历史全部失去前缀缓存。而工具检索（SelectToolsForTurn）是按当前
// query 本地打分取 Top-K，话题一抖动就会换掉集合里的补充项，于是历史被反复
// 判为缓存未命中。
//
// 本文件在检索结果之上加一层"滞回 + 单调并集 + 空闲衰减"，把集合变化从
// "每轮都可能变"收敛为"只在真正需要新工具时变一次"：
// （稳定状态本身是会话级槽位，定义见 builtin/ai/session）
//
//	候选 == 当前集合      → 保持
//	候选 ⊆  当前集合      → 保持（补充项留在集合里：一轮没选中 != 话题不再需要）
//	其余（新增了工具）    → 并集（只增不减，避免话题来回摆动时反复替换）
//	补充项长期未被命中    → 按 TTL 批量衰减；超出上限按最近使用时间淘汰
//
// 与提示词结构优化的关系：System 段稳定解决了"缓存前缀从第一条历史就开始
// 失效"的问题；本文件解决的是它前面的那个开关。两者都做到，一次工具集切换
// 之后的后续多轮才真正形成可复用的稳定前缀。
//
// 硬约束：最终集合始终是 available 的子集。available 已经过群策略白名单与
// RBAC 过滤，权限收缩必须立即生效——工具集状态不得成为绕过过滤的旁路。
package decision

import (
	"slices"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// ToolSetObserver 观测一次工具集决策结果：reason 为变更归因
// （init/grow/decay/shrink/keep），changed 表示集合是否真的变化，size 为集合大小。
//
// 观测依赖（指标采集）由装配侧注入，决策包本身不引入可观测性依赖；
// 为 nil 时只做决策、不上报。
type ToolSetObserver func(reason string, changed bool, size int)

// 工具集稳定策略默认值（配置值非法时生效）。
const (
	// defaultToolSetStickyMax 稳定集合中补充工具的数量上限。
	defaultToolSetStickyMax = 8
)

// effectiveToolSetStickyMax 返回补充工具数量上限（非法值回退默认）。
func effectiveToolSetStickyMax(cfg *config.Config) int {
	if cfg.ToolSetStickyMax <= 0 {
		return defaultToolSetStickyMax
	}
	return cfg.ToolSetStickyMax
}

// effectiveToolSetTTL 返回补充工具空闲存活时长（<=0 表示不衰减）。
// 生产默认值来自 config.DefaultConfig（20 分钟）；配置负值（如 -1s）可关闭衰减。
func effectiveToolSetTTL(cfg *config.Config) time.Duration {
	if cfg.ToolSetTTL <= 0 {
		return 0
	}
	return cfg.ToolSetTTL
}

// StabilizeToolSet 在检索候选之上应用会话级稳定策略，返回本轮实际发送的工具。
//
// available 是本轮过滤后的全部可用工具（群策略 + RBAC 已生效），
// candidate 是选择路径的打分结果（含通用/已用必保集）。
// 策略关闭（tool_set_sticky=false）或 session 为空时原样返回 candidate。
func StabilizeToolSet(cfg *config.Config, sess *session.Session, available, candidate []toolkit.Action, observe ToolSetObserver) []toolkit.Action {
	if !cfg.ToolSetSticky || sess == nil {
		return candidate
	}

	avail := make(map[string]toolkit.Action, len(available))
	for _, a := range available {
		avail[a.Spec.Name] = a
	}
	candNames := intersectNames(toolNames(candidate), avail)
	now := time.Now()

	st := sess.ToolSetState()
	lastSeen := make(map[string]time.Time)
	var prev []string
	var gen uint64
	if st != nil {
		gen = st.Generation
		// 可用集收缩（RBAC/群策略/插件注销）立即生效：不在 available 里的
		// 历史状态直接丢弃，不写入新状态。
		prev = intersectNames(st.Names, avail)
		for k, v := range st.LastSeen {
			if _, ok := avail[k]; ok {
				lastSeen[k] = v
			}
		}
	}
	for _, n := range candNames {
		lastSeen[n] = now
	}
	for _, n := range prev {
		if _, ok := lastSeen[n]; !ok {
			lastSeen[n] = now
		}
	}

	reason := "keep"
	var next []string
	switch {
	case len(prev) == 0:
		// 首次收敛（或历史集合已被过滤清空）：以候选为起点。
		next = candNames
	case isSubset(candNames, prev):
		// 候选是当前集合的子集（含相等）：保持现状。补充项是此前已确认需要
		// 的工具，一轮没被选中不代表话题不再需要，保留它们才能让回访
		// 或话题往复不产生新的缓存失效。
		next = prev
	default:
		next = mergeToolSet(cfg, candNames, prev, avail, lastSeen)
	}

	// 空闲衰减：补充项（不在本轮候选里）超过 TTL 未被候选命中即批量移除。
	// 放在决策之后，避免与本轮的增长判定互相干扰；到期项一次移除，
	// 不会逐轮零敲碎打地改动集合。
	decayed := false
	if ttl := effectiveToolSetTTL(cfg); ttl > 0 && len(next) > 0 {
		candSet := nameSet(candNames)
		kept := make([]string, 0, len(next))
		for _, n := range next {
			if candSet[n] || now.Sub(lastSeen[n]) <= ttl {
				kept = append(kept, n)
				continue
			}
			decayed = true
		}
		if decayed {
			next = kept
		}
	}

	// 变更归因：供观测用（"变更次数 / 请求次数"即抖动率）。
	changed := !slices.Equal(next, prev)
	if changed {
		gen++
		switch {
		case len(prev) == 0:
			reason = "init"
		case decayed:
			reason = "decay"
		case len(next) > len(prev):
			reason = "grow"
		default:
			reason = "shrink"
		}
	}

	// 回写状态：即使集合未变也要更新 LastSeen（衰减依赖它）。
	state := &session.ToolSetState{
		Names:      next,
		LastSeen:   restrictLastSeen(lastSeen, next),
		Generation: gen,
	}
	if changed || st == nil {
		state.ChangedAt = now
	} else {
		state.ChangedAt = st.ChangedAt
	}
	sess.SetToolSetState(state)

	if changed {
		logger.Debugf("[AI] ToolSet changed (gen=%d reason=%s): %d→%d tools",
			gen, reason, len(prev), len(next))
	}
	if observe != nil {
		observe(reason, changed, len(next))
	}

	return toolsByName(avail, next)
}

// mergeToolSet 计算"只增不减"的并集：候选全部保留，补充项（当前集合中不在
// 候选里的部分）按最近使用时间从新到旧保留，数量不超过 sticky_max，
// 且与候选合计的 schema token 估算不超过 tool_budget。
//
// 候选本身不受这里的 token 预算回收（通用工具与会话已用工具属必保集，
// 原有行为即允许其略微超出预算），预算只用于限制补充项。
func mergeToolSet(cfg *config.Config, cand, prev []string, avail map[string]toolkit.Action, lastSeen map[string]time.Time) []string {
	inCand := nameSet(cand)
	out := append([]string(nil), cand...)

	tokens := 0
	for _, n := range out {
		tokens += EstimateToolTokens(avail[n])
	}
	budget := cfg.ToolBudget
	if budget <= 0 {
		budget = 8000
	}
	limit := len(cand) + effectiveToolSetStickyMax(cfg)

	fillers := make([]string, 0, len(prev))
	for _, n := range prev {
		if n == "" || inCand[n] {
			continue
		}
		fillers = append(fillers, n)
	}
	// 最近使用优先；时间相同按名称升序，保证结果确定。
	slices.SortFunc(fillers, func(a, b string) int {
		ta, tb := lastSeen[a], lastSeen[b]
		if !ta.Equal(tb) {
			if ta.After(tb) {
				return -1
			}
			return 1
		}
		return strings.Compare(a, b)
	})

	for _, n := range fillers {
		if len(out) >= limit {
			break
		}
		cost := EstimateToolTokens(avail[n])
		if tokens+cost > budget {
			// 预算已满：不再追加补充项（不逐条跳过，保持结果确定）
			break
		}
		tokens += cost
		out = append(out, n)
	}

	slices.Sort(out)
	return out
}

// toolNames 返回动作名列表（升序）。排序保证同一动作集在不同请求中
// 序列化为相同字节序列。
func toolNames(actions []toolkit.Action) []string {
	names := make([]string, 0, len(actions))
	for _, a := range actions {
		names = append(names, a.Spec.Name)
	}
	slices.Sort(names)
	return names
}

// toolsByName 按名称列表解析动作，保持列表顺序（列表已升序）。
// 名称缺失时跳过（可用动作集变化后不会解析出不存在的动作）。
func toolsByName(avail map[string]toolkit.Action, names []string) []toolkit.Action {
	out := make([]toolkit.Action, 0, len(names))
	for _, n := range names {
		if a, ok := avail[n]; ok {
			out = append(out, a)
		}
	}
	return out
}

// resolveToolsByName 按名称列表从 actions 中解析子集（缺失的名字被剪掉）。
// 与 SelectionCache.Tools 的区别：不因个别名字缺失而回退全量——工具集
// 稳定策略下"少几个工具"是正常收缩，回退全量会让稳定集合瞬间失效。
func resolveToolsByName(actions []toolkit.Action, names []string) []toolkit.Action {
	avail := make(map[string]toolkit.Action, len(actions))
	for _, a := range actions {
		avail[a.Spec.Name] = a
	}
	return toolsByName(avail, names)
}

// nameSet 把名称列表转为集合。
func nameSet(names []string) map[string]bool {
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	return set
}

// isSubset 判断 a 是否为 b 的子集。
func isSubset(a, b []string) bool {
	if len(a) > len(b) {
		return false
	}
	set := nameSet(b)
	for _, n := range a {
		if !set[n] {
			return false
		}
	}
	return true
}

// intersectNames 返回 names 中存在于 avail 的部分（保持原顺序）。
func intersectNames(names []string, avail map[string]toolkit.Action) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		if _, ok := avail[n]; ok {
			out = append(out, n)
		}
	}
	return out
}

// restrictLastSeen 只保留仍在使用中的工具的时间戳，避免状态随会话无限增长。
func restrictLastSeen(lastSeen map[string]time.Time, names []string) map[string]time.Time {
	out := make(map[string]time.Time, len(names))
	for _, n := range names {
		if ts, ok := lastSeen[n]; ok {
			out[n] = ts
		}
	}
	return out
}
