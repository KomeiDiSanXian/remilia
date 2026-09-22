// Package decision 回合动作决策：候选打分 → 本轮选择 → 会话级稳定策略。
//
// 决策只回答"这一轮把哪些动作交给模型"，从不执行动作本身：执行路径只消费
// 结论，不重新推导策略。
//
//	打分    ScoreTool / ScoreToolParts  关键词命中 + 会话热用 + 可选语义余弦
//	选择    SelectToolsForTurn          必保集 + 高分补充 + token 预算 + 会话缓存
//	稳定    StabilizeToolSet            滞回 + 单调并集 + 空闲衰减（见 stabilize.go）
//
// 与检索骨架的分工：分词、重叠度、语义权重与确定性排序见 builtin/ai/retrieval；
// 本包只承载动作选择这一领域的候选来源、入选门槛与稳定策略，不承载通用算法。
//
// 候选来源（注册表、用户 Skill、群策略、RBAC）与审批交互依赖 AI 运行时状态，
// 仍留在装配侧：本包的输入是已经过滤好的可用动作与调用方给定的本轮查询。
package decision

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/retrieval"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/textutil"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// SelectionCacheTTL 工具选择缓存的 TTL。
// 仅当上次选择的查询与本轮查询关键词 Jaccard 相似且未超 TTL 时复用，
// 避免话题漂移后仍沿用陈旧工具集。
// 与历史检索缓存共用同一套复用策略（见 session 的缓存策略常量）。
const SelectionCacheTTL = session.CacheReuseTTL

// SelectionCacheJaccard 复用缓存所需的最小关键词 Jaccard 相似度。
const SelectionCacheJaccard = session.CacheReuseJaccard

// SessionUsedTools 收集本会话中已调用过的工具名集合。
// 用于会话热用加成与必保集，保证多轮任务中工具不抖动。
func SessionUsedTools(sess *session.Session) map[string]bool {
	used := make(map[string]bool)
	for _, m := range sess.SnapshotMessages() {
		for _, tc := range m.ToolCalls {
			if tc.Name != "" {
				used[tc.Name] = true
			}
		}
	}
	return used
}

// RetainedActions 收敛到"无需主动动作也应保留"的动作子集。
// 保留输入顺序，与选择路径一致地交给 StabilizeToolSet 处理稳定集合。
func RetainedActions(actions []toolkit.Action, used map[string]bool) []toolkit.Action {
	out := make([]toolkit.Action, 0, len(actions))
	for _, a := range actions {
		if retainedAction(a, used) {
			out = append(out, a)
		}
	}
	return out
}

// retainedAction 判断动作是否"无需主动动作也应保留"：
// 默认保留级别（Mandatory/Baseline）或本会话已调用过（会话热用）。
func retainedAction(a toolkit.Action, used map[string]bool) bool {
	return a.Policy.Selection.KeepsWhenNoAction() || used[a.Spec.Name]
}

// scoredTool 携带分数的动作候选。
type scoredTool struct {
	action toolkit.Action
	score  float64
	kw     float64
	cos    float64
}

// SelectToolsForTurn 从可用工具中选择本轮发送给 LLM 的子集。
//
// 选择策略：
//  1. 工具总数 ≤ ToolSelectMax 且未启用 embedding 时直接全部发送（零开销，无嵌入调用）
//  2. 否则按 query 本地打分，取 Top-K（默认 20），受 token 预算（tool_budget，默认 8000）约束
//  3. 必保集：默认保留级别（原"通用工具"）与会话已用工具恒被选中
//  4. embedding 启用时叠加余弦相似度；请求失败自动降级纯关键词
//  5. 会话缓存：TTL 内且关键词 Jaccard ≥ 0.5 时复用上次选择
//     （避免每轮重算与重复嵌入；话题漂移自动失效）
//  6. 会话级稳定策略：在候选之上叠加滞回与单调并集，抑制工具集抖动
//     （见 stabilize.go；tool_set_sticky=false 时跳过）。
//
// query 为本轮检索用的用户消息文本，由调用方给定（历史检索与记忆检索复用同一份查询）。
// observe 为工具集观测端口，可为 nil（观测依赖由装配侧注入）。
func SelectToolsForTurn(cfg *config.Config, emb *retrieval.TextVectorCache, ctx *eventctx.Context,
	sess *session.Session, query string, actions []toolkit.Action, observe ToolSetObserver) []toolkit.Action {
	max := cfg.ToolSelectMax
	if max <= 0 {
		max = 20
	}
	if len(actions) <= max && (emb == nil || !emb.Enabled()) {
		return actions
	}

	if query == "" {
		return actions
	}
	queryTokens := retrieval.TokenizeText(query)

	if cached := sess.ToolSelection(); cached != nil && cached.ToolCount == len(actions) {
		if time.Since(cached.At) <= SelectionCacheTTL &&
			retrieval.JaccardSimilarity(queryTokens, cached.QueryTokens) >= SelectionCacheJaccard {
			cand := cached.Tools(actions)
			if cfg.ToolSetSticky {
				// 稳定策略下按缓存名剪枝，而非回退全量：个别工具失效不应
				// 让整个稳定集合瞬间膨胀。
				cand = resolveToolsByName(actions, cached.Names)
			}
			return StabilizeToolSet(cfg, sess, actions, cand, observe)
		}
	}

	used := SessionUsedTools(sess)

	// 可选 embedding：查询向量 + 工具向量（工具文本向量缓存，缺失才嵌入）。
	var queryVec []float32
	var textVecs map[string][]float32
	if emb != nil && emb.Enabled() {
		texts := make([]string, 0, len(actions))
		for _, a := range actions {
			texts = append(texts, ToolEmbeddingText(a))
		}
		queryVec, textVecs = retrieval.AcquireSemanticVectors(ctx.Context(), emb, 0, query, texts,
			retrieval.SemanticFallbackLog{
				TextsFailed: "[AI] Embedding tools failed, keyword-only scoring: %v",
				QueryFailed: "[AI] Embedding query failed, keyword-only scoring: %v",
			})
	}

	scored := make([]scoredTool, 0, len(actions))
	for _, a := range actions {
		kw, cos, final := ScoreToolParts(query, queryTokens, a, used[a.Spec.Name], queryVec, textVecs[ToolEmbeddingText(a)])
		scored = append(scored, scoredTool{action: a, score: final, kw: kw, cos: cos})
	}
	// 分数降序，同分按工具名升序，保证选择结果确定（工具注册表为 map，原始顺序随机）。
	retrieval.RankByScore(scored,
		func(s scoredTool) float64 { return s.score },
		func(a, b scoredTool) bool { return a.action.Spec.Name < b.action.Spec.Name })

	// 可观测性：每轮输出 Top-5 排名（关键词分/余弦/总分），
	// 用于核对 embedding 权重是否合适、以及失败降级是否影响选择。
	if len(scored) > 0 {
		top := scored
		if len(top) > 5 {
			top = top[:5]
		}
		var b strings.Builder
		for i, s := range top {
			fmt.Fprintf(&b, " #%d=%s(kw %.2f cos %.3f tot %.2f)", i+1, s.action.Spec.Name, s.kw, s.cos, s.score)
		}
		logger.Debugf("[AI] ToolSelect query=%q tools=%d embed=%v%s", textutil.TruncateRunes(query, 60), len(scored), queryVec != nil, b.String())
	}

	budget := cfg.ToolBudget
	if budget <= 0 {
		budget = 8000
	}
	seen := make(map[string]bool, len(actions))
	out := make([]toolkit.Action, 0, max)
	usedBudget := 0

	// 必保集优先：默认保留级别（原"通用工具"）的动作 + 会话已用工具。
	for _, s := range scored {
		if seen[s.action.Spec.Name] {
			continue
		}
		if retainedAction(s.action, used) {
			seen[s.action.Spec.Name] = true
			out = append(out, s.action)
			usedBudget += EstimateToolTokens(s.action)
		}
	}

	// 高分补充，受数量与 token 预算约束。
	for _, s := range scored {
		if len(out) >= max {
			break
		}
		if seen[s.action.Spec.Name] {
			continue
		}
		if len(out) > 0 && usedBudget+EstimateToolTokens(s.action) > budget {
			continue
		}
		seen[s.action.Spec.Name] = true
		out = append(out, s.action)
		usedBudget += EstimateToolTokens(s.action)
	}

	// 按输入工具列表的顺序排序返回（ToolRegistry.List 已按工具名升序，
	// 因此可保证跨请求字节稳定，利于 prompt 缓存与可测试性）。
	idx := make(map[string]int, len(actions))
	for i, a := range actions {
		idx[a.Spec.Name] = i
	}
	slices.SortFunc(out, func(a, b toolkit.Action) int { return idx[a.Spec.Name] - idx[b.Spec.Name] })

	logger.Debugf("[AI] Selected %d/%d tools (budget %d/%d)", len(out), len(actions), usedBudget, budget)

	names := make([]string, 0, len(out))
	for _, a := range out {
		names = append(names, a.Spec.Name)
	}
	sess.SetToolSelection(&session.SelectionCache{
		QueryTokens: queryTokens,
		At:          time.Now(),
		ToolCount:   len(actions),
		Names:       names,
	})

	return StabilizeToolSet(cfg, sess, actions, out, observe)
}
