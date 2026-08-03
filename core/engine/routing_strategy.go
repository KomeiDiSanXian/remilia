package engine

// routing_strategy.go — 路由规划抽象
//
// 将"如何组织索引、如何规划一次路由"从执行主循环中抽离，
// 形成 RoutingStrategy → CandidatePlan → MatcherIndex 三层稳定边界：
//
//	Engine（协调者）→ RoutingStrategy → CandidatePlan → MatcherIndex → matcherMergeIter
//
// processEventMatchers 只负责执行，不知道任何索引组织方式。
// 相关设计：docs/notes/25-routing-strategy.md
//
// 多阶段执行（随第一个慢带索引 regexIndex 落地）：
// CandidatePlan 按 Band 分组为阶段，快带急切构建、慢带惰性物化——
// fast 阶段被 block 短路时，慢阶段索引零查询（正则免费跳过）。

import (
	"strconv"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// RoutingBand 是索引所属的优先级带（框架级概念，不是用户配置）。
//
// matcher 没有 Band API——Band 由索引决定（MatcherIndex.Band()），
// 粒度是索引级，天然收敛，避免逐 matcher 配置导致的带失控。
type RoutingBand uint8

const (
	// BandFast 覆盖永久/命令/临时等快速索引。
	BandFast RoutingBand = iota
	// BandSlow 覆盖正则等慢速索引：仅在快带未阻断时参与执行，
	// 且优先级应低于快带 matcher（文档化契约，见 Fast/Slow 语义）。
	BandSlow
)

// maxRoutingPhases 是阶段数量上限，等于框架带常量数。
// CandidatePlan 以 [maxRoutingPhases] 内联数组存放阶段数据（零分配）。
const maxRoutingPhases = int(BandSlow) + 1

// 路由源预算（Source Budget）。
//
// 两个 K 严格区分：
//   - K_reg（注册索引数）：架构预算。推荐 ≤8、允许 ≤16、超过 16 需要
//     重新审视路由设计——超限首先意味着职责拆分过碎（如 Permission/Role/Guild
//     应合并为 Metadata Index），其次才意味着需要考虑 heap 归并
//   - K_act（实际非空流数）：归并算法输入，add() 跳过空列表自然衰减
//
// 执行规则：框架内部注册超限 panic（框架 Bug，不是用户输入）；
// 第三方注册超限 warn 后继续运行（扩展行为，不应中断）。
const (
	routingBudgetRecommended = 8
	routingBudgetLimit       = 16
)

// RoutingStrategy 决定"本次路由如何组织索引"。
//
// 阶段编排、索引跳过、剪枝等策略性决策都发生在这里。
// 默认实现为 matcherRouter；未来 ProfilingStrategy / DebugStrategy
// 实现同一接口即可，执行主循环无需改动。
type RoutingStrategy interface {
	// Plan 返回本次路由的执行计划（按值返回，零分配）。
	Plan(st *state, ctx *context.Context) CandidatePlan
}

// matcherRouter 是默认 RoutingStrategy 实现：
// 持有索引列表（按 Band 分组为阶段）、Source Budget 执行与归并工厂。
type matcherRouter struct {
	phases     []candidatePhase // 第三方/慢带索引，按 Band 升序分组
	indexCount int              // 注册索引总数（K_reg，预算依据）

	// 内置快带索引直引（注册时按类型识别；热路径免类型断言/接口分发，
	// 允许 Candidates 内联进 Plan）。未注册时 hasXxx 为 false。
	perm    permanentIndex
	cmd     commandIndex
	tmp     tempIndex
	hasPerm bool
	hasCmd  bool
	hasTemp bool
}

// candidatePhase 是一个优先级带的候选索引组。
type candidatePhase struct {
	band    RoutingBand
	indexes []MatcherIndex
}

// newMatcherRouter 创建默认路由策略。
func newMatcherRouter() *matcherRouter {
	return &matcherRouter{}
}

// addIndex 注册一个索引并执行 Source Budget。
//
// internal=true 表示框架内部注册（超限 panic，属框架 Bug）；
// false 表示第三方注册（超限 warn 后继续运行）。
func (r *matcherRouter) addIndex(idx MatcherIndex, internal bool) {
	if idx == nil {
		return
	}

	next := r.indexCount + 1
	if next > routingBudgetLimit {
		if internal {
			panic("engine: routing source budget exceeded: framework registered " +
				strconv.Itoa(next) + " matcher indexes (hard limit " +
				strconv.Itoa(routingBudgetLimit) + ") — this is a framework bug, review routing design")
		}
		logger.Warnf("[engine] routing source budget exceeded: %d matcher indexes registered (recommended ≤ %d, hard limit %d). Review whether indexes are over-split and can be merged.",
			next, routingBudgetRecommended, routingBudgetLimit)
	}
	r.indexCount = next

	// 内置快带索引走直引字段，不进入 phases（热路径静态分派）
	switch t := idx.(type) {
	case permanentIndex:
		r.perm = t
		r.hasPerm = true
		return
	case commandIndex:
		r.cmd = t
		r.hasCmd = true
		return
	case tempIndex:
		r.tmp = t
		r.hasTemp = true
		return
	}

	band := idx.Band()
	// 未知带（>= maxRoutingPhases）按 BandSlow 处理：
	// 更高编号的带尚不存在，慢带是"后执行"语义的合理落点。
	if band >= RoutingBand(maxRoutingPhases) {
		logger.Warnf("[engine] routing band %d is not a framework-defined band, treated as slow", band)
		band = BandSlow
	}

	// 按 Band 升序插入阶段（快带先于慢带执行，与注册顺序无关）
	pos := len(r.phases)
	for i := range r.phases {
		if r.phases[i].band >= band {
			pos = i
			break
		}
	}
	if pos == len(r.phases) || r.phases[pos].band != band {
		r.phases = append(r.phases, candidatePhase{})
		copy(r.phases[pos+1:], r.phases[pos:])
		r.phases[pos] = candidatePhase{band: band, indexes: []MatcherIndex{idx}}
	} else {
		r.phases[pos].indexes = append(r.phases[pos].indexes, idx)
	}
}

// Plan 构建本次路由的执行计划（单迭代器 + 惰性慢带追加）。
//
// 内置快带索引（permanent/command/temp）静态分派直插——热路径免
// 接口分发且允许内联；第三方/慢带索引走 phases。慢带在快带耗尽后
// 的首个 Next() 才被查询——fast 被 block 短路时慢带索引零查询。
func (r *matcherRouter) Plan(st *state, ctx *context.Context) CandidatePlan {
	it := acquireMergeIter()
	env := matcherEnv{st: st}
	plan := CandidatePlan{env: env, ctx: ctx, iter: it}
	if r.hasPerm {
		mergeInto(it, r.perm.Candidates(env, ctx))
	}
	if r.hasCmd {
		mergeInto(it, r.cmd.Candidates(env, ctx))
	}
	if r.hasTemp {
		mergeInto(it, r.tmp.Candidates(env, ctx))
	}
	for i := range r.phases {
		ph := &r.phases[i]
		if ph.band == BandFast {
			for j := range ph.indexes {
				mergeInto(it, ph.indexes[j].Candidates(env, ctx))
			}
		} else {
			for j := range ph.indexes {
				if plan.slowN < len(plan.slow) {
					plan.slow[plan.slowN] = ph.indexes[j]
					plan.slowN++
				}
			}
		}
	}
	return plan
}

// mergeInto 将索引返回的候选流并入归并迭代器。
// 携带 Meta 的候选流（如 regexIndex 捕获组）经 addMeta 传入，
// 快带流保持无 Meta（零成本）。
func mergeInto(it *matcherMergeIter, c MatcherCandidates) {
	for k := 0; k < c.n; k++ {
		if c.metas[k] != nil {
			it.addMeta(c.lists[k], c.metas[k])
		} else {
			it.add(c.lists[k])
		}
	}
}

// addCandidates 将索引的候选流并入归并迭代器（惰性慢带路径用）。
// 内置索引经类型断言直接调用（静态分派）；第三方索引走接口。
func addCandidates(it *matcherMergeIter, idx MatcherIndex, env matcherEnv, ctx *context.Context) {
	var c MatcherCandidates
	switch t := idx.(type) {
	case permanentIndex:
		c = t.Candidates(env, ctx)
	case commandIndex:
		c = t.Candidates(env, ctx)
	case tempIndex:
		c = t.Candidates(env, ctx)
	case regexIndex:
		c = t.Candidates(env, ctx)
	default:
		c = idx.Candidates(env, ctx)
	}
	mergeInto(it, c)
}

// CandidatePlan 描述本次路由的执行计划（RoutingStrategy 唯一暴露的对象）。
//
// 自包含：持有快带候选流（已并入归并迭代器）与慢带索引快照，
// 不引用 Router；生命周期为 Plan() → Next() → Release()。
// 按值返回，零分配（慢带快照为内联数组）；池化迭代器由 Release 归还（幂等）。
//
// 单迭代器 + 惰性慢带追加：快带耗尽后慢带索引才被查询并追加到同一
// 归并迭代器（各流 idx 已耗尽，追加不破坏归并顺序）；相对双迭代器
// 每事件省去一次 acquire/release 与阶段游标嵌套。
type CandidatePlan struct {
	env   matcherEnv
	ctx   *context.Context
	slow  [routingBudgetLimit]MatcherIndex // 慢带索引快照（惰性追加）
	slowN int
	iter  *matcherMergeIter
}

// Next 前进到下一个候选 Matcher。
//
// 快带耗尽后自动惰性查询慢带索引并继续；返回 false 表示全部执行完毕。
// fast 阶段被 block 短路时（执行循环直接 return，不再调用 Next），
// 慢带索引零查询。
func (p *CandidatePlan) Next() bool {
	if p.iter == nil {
		return false
	}
	if p.iter.Next() {
		return true
	}
	if p.slowN > 0 {
		for i := 0; i < p.slowN; i++ {
			addCandidates(p.iter, p.slow[i], p.env, p.ctx)
		}
		p.slowN = 0
		return p.iter.Next()
	}
	return false
}

// Matcher 返回当前 Matcher（Next 后有效）。
func (p *CandidatePlan) Matcher() *Matcher {
	return p.iter.Matcher()
}

// Meta 返回当前 Matcher 携带的匹配结果 Meta（Next 后有效；无 Meta 时为 nil）。
// 由执行循环在 invokeHandler 前注入 ctx（SetCandidateMeta），
// handler 通过 ctx 的类型化 getter（如 RegexResult）读取。
func (p *CandidatePlan) Meta() any {
	return p.iter.Meta()
}

// Release 归还池化迭代器。必须在计划用完后调用（defer 即可），幂等。
// 释放后计划进入终态：后续 Next() 恒返回 false。
func (p *CandidatePlan) Release() {
	if p.iter != nil {
		releaseMergeIter(p.iter)
		p.iter = nil
	}
	p.slowN = 0
}

// WithMatcherIndex 注册一个自定义候选检索源（MatcherIndex 插件点）。
//
// 索引在构造期注册，参与每次路由的候选归并；不支持运行时增删
// （运行时增删会污染 COW 生命周期，见 25 D5）。
//
// 第三方注册的索引受 Source Budget 约束：超过硬上限（16）时
// log.Warn 并继续运行；框架内部注册超限则 panic（框架 Bug）。
func WithMatcherIndex(idx MatcherIndex) Option {
	return func(e *Engine) {
		if idx == nil {
			return
		}
		r, ok := e.strategy.Load().(*matcherRouter)
		if !ok {
			logger.Warn("[engine] WithMatcherIndex ignored: current routing strategy is not the default matcherRouter")
			return
		}
		r.addIndex(idx, false)
	}
}
