// Package ai select.go — 工具选择器：每轮按用户消息本地检索 Top-K 工具。
//
// 本文件替代旧的"LLM 单分类路由"（routeToolCategory），实现纯本地计算
// 的工具选择：
//   - tokenizeText：CJK 二元组 + 英文单词的词袋分词
//   - scoreTool：关键词命中（工具名 > 描述/分类）+ 会话热用加成 + 可选 embedding 余弦
//   - selectToolsForTurn：必保集（通用工具 + 会话已用）+ 高分补充 + token 预算封顶
//   - 会话级选择缓存：关键词 Jaccard 相似或 TTL 内复用上次选择，避免每轮重算
//
// 相比旧路由的三点优势：零额外 LLM 调用、跨域任务自然覆盖多个分类、纯函数可测试。
package ai

import (
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// 打分权重。
const (
	// scoreNameHit 工具名完整出现在查询中。
	scoreNameHit = 4.0
	// scoreNameTok 工具名 token 与查询重叠。
	scoreNameTok = 3.0
	// scoreDescTok 描述/分类 token 与查询重叠。
	scoreDescTok = 1.0
	// scoreUsedBonus 会话内已调用过的工具加成（保持工具连续性）。
	scoreUsedBonus = 1.5
	// scoreEmbedW embedding 余弦相似度权重。
	scoreEmbedW = 2.0
)

// selectionCacheTTL 工具选择缓存的 TTL。
// 仅当上次选择的查询与本轮查询关键词 Jaccard 相似且未超 TTL 时复用，
// 避免话题漂移后仍沿用陈旧工具集。
const selectionCacheTTL = 10 * time.Minute

// selectionCacheJaccard 复用缓存所需的最小关键词 Jaccard 相似度。
const selectionCacheJaccard = 0.5

var wordRegexp = regexp.MustCompile(`[a-z0-9]+`)

// tokenizeText 将文本切分为加权 token 集合。
// 英文按小写单词切分；连续汉字按二元组切分（中文领域词无需分词库）；
// 孤立汉字（如 "B站" 中的 站）单独成 token，保证中英混合脚本可匹配。
func tokenizeText(text string) map[string]float64 {
	tokens := make(map[string]float64)
	for _, m := range wordRegexp.FindAllString(strings.ToLower(text), -1) {
		tokens[m]++
	}
	runes := []rune(text)
	n := len(runes)
	for i := 0; i < n; {
		if !unicode.Is(unicode.Han, runes[i]) {
			i++
			continue
		}
		j := i
		for j < n && unicode.Is(unicode.Han, runes[j]) {
			j++
		}
		if j-i == 1 {
			tokens[string(runes[i])]++
		}
		for k := i; k < j-1; k++ {
			tokens[string(runes[k:k+2])]++
		}
		i = j
	}
	return tokens
}

// tokenOverlap 计算两个 token 集合的重叠加权和。
// 取 min 防止长文本重复 token 造成偏置。
func tokenOverlap(a, b map[string]float64) float64 {
	var s float64
	for k, av := range a {
		if bv, ok := b[k]; ok {
			s += min(av, bv)
		}
	}
	return s
}

// jaccardSimilarity 计算两个 token 集合的 Jaccard 相似度。
// 任一为空返回 0。
func jaccardSimilarity(a, b map[string]float64) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	intersection := 0
	union := make(map[string]struct{}, len(a)+len(b))
	for k := range a {
		union[k] = struct{}{}
	}
	for k := range b {
		if _, ok := union[k]; ok {
			intersection++
		} else {
			union[k] = struct{}{}
		}
	}
	return float64(intersection) / float64(len(union))
}

// estimateToolTokens 粗略估算工具 schema 占用的 token 数。
// 用于工具选择时的 token 预算控制。
func estimateToolTokens(t Tool) int {
	n := 12 + len(t.Name)
	n += len(t.Description) / 4
	if t.Parameters.Type != "" {
		n += 16
	}
	for k, p := range t.Parameters.Properties {
		n += 10 + len(k)
		n += len(p.Description) / 4
		if len(p.Enum) > 0 {
			n += len(p.Enum) * 3
		}
	}
	return n
}

// isGeneralTool 判断工具是否属于通用集（Categories 为空或含 general）。
// 通用工具恒被选中，作为模型的基础兜底能力。
func isGeneralTool(t Tool) bool {
	if len(t.Categories) == 0 {
		return true
	}
	return slices.Contains(t.Categories, CategoryGeneral)
}

// scoreTool 计算工具与查询的相关度得分。
// queryVec/toolVec 为可选 embedding 向量，为 nil 时跳过语义项。
func scoreTool(query string, queryTokens map[string]float64, t Tool, used bool, queryVec, toolVec []float32) float64 {
	var score float64
	q := strings.ToLower(query)

	if name := strings.ToLower(t.Name); name != "" && strings.Contains(q, name) {
		score += scoreNameHit
	}
	score += scoreNameTok * tokenOverlap(queryTokens, tokenizeText(t.Name))
	score += scoreDescTok * tokenOverlap(queryTokens, tokenizeText(t.Description+" "+strings.Join(t.Categories, " ")))

	if used {
		score += scoreUsedBonus
	}
	if queryVec != nil && toolVec != nil {
		score += float64(cosineSimilarity(queryVec, toolVec)) * scoreEmbedW
	}
	return score
}

// selectionCache 会话级工具选择缓存（json:"-" 不持久化）。
type selectionCache struct {
	QueryTokens map[string]float64
	At          time.Time
	ToolCount   int
	Names       []string
}

// Tools 按工具名解析缓存结果；工具集内容变化（同名缺失）时回退全量。
func (c *selectionCache) Tools(tools []Tool) []Tool {
	byName := make(map[string]Tool, len(tools))
	for _, t := range tools {
		byName[t.Name] = t
	}
	out := make([]Tool, 0, len(c.Names))
	for _, n := range c.Names {
		if t, ok := byName[n]; ok {
			out = append(out, t)
		}
	}
	if len(out) == len(c.Names) {
		return out
	}
	return tools
}

// toolSelection 返回会话缓存的工具选择结果（无缓存返回 nil）。
func (s *Session) toolSelection() *selectionCache {
	s.Lock()
	defer s.Unlock()
	return s.selCache
}

// setToolSelection 写入会话工具选择缓存。
func (s *Session) setToolSelection(c *selectionCache) {
	s.Lock()
	defer s.Unlock()
	s.selCache = c
}

// sessionUsedTools 收集本会话中已调用过的工具名集合。
// 用于会话热用加成与必保集，保证多轮任务中工具不抖动。
func (p *Plugin) sessionUsedTools(session *Session) map[string]bool {
	used := make(map[string]bool)
	for _, m := range session.SnapshotMessages() {
		for _, tc := range m.ToolCalls {
			if tc.Name != "" {
				used[tc.Name] = true
			}
		}
	}
	return used
}

// selectToolsForTurn 从可用工具中选择本轮发送给 LLM 的子集。
//
// 选择策略：
//  1. 工具总数 ≤ ToolSelectMax 时直接全部发送（零开销，无 embedding 调用）
//  2. 否则按最后一条用户消息本地打分，取 Top-K（默认 20），
//     受 token 预算（tool_budget，默认 8000）约束
//  3. 必保集：通用工具（Categories 空或含 general）与会话已用工具恒被选中
//  4. embedding 配置启用时叠加余弦相似度；请求失败自动降级纯关键词
//  5. 会话缓存：TTL 内且关键词 Jaccard ≥ 0.5 时复用上次选择
//     （避免每轮重算与重复嵌入；话题漂移自动失效）。
//
// scoredTool 携带分数的工具候选。
type scoredTool struct {
	tool  Tool
	score float64
}

func (p *Plugin) selectToolsForTurn(ctx *eventctx.Context, session *Session, tools []Tool) []Tool {
	max := p.cfg.ToolSelectMax
	if max <= 0 {
		max = 20
	}
	if len(tools) <= max && (p.emb == nil || !p.emb.Enabled()) {
		return tools
	}

	query := getLastUserMessage(session)
	if query == "" {
		return tools
	}
	queryTokens := tokenizeText(query)

	if cached := session.toolSelection(); cached != nil && cached.ToolCount == len(tools) {
		if time.Since(cached.At) <= selectionCacheTTL &&
			jaccardSimilarity(queryTokens, cached.QueryTokens) >= selectionCacheJaccard {
			return cached.Tools(tools)
		}
	}

	used := p.sessionUsedTools(session)

	// 可选 embedding：查询向量 + 工具向量（工具文本向量缓存，缺失才嵌入）。
	var queryVec []float32
	textVecs := make(map[string][]float32)
	if p.emb != nil && p.emb.Enabled() {
		texts := make([]string, 0, len(tools))
		for _, t := range tools {
			texts = append(texts, toolEmbeddingText(t))
		}
		if vecs, err := p.emb.EmbedTexts(ctx.Context(), texts); err == nil {
			textVecs = vecs
			if qv, qerr := p.emb.EmbedQuery(ctx.Context(), query); qerr == nil {
				queryVec = qv
			} else {
				logger.Debugf("[AI] Embedding query failed, keyword-only scoring: %v", qerr)
			}
		} else {
			logger.Debugf("[AI] Embedding tools failed, keyword-only scoring: %v", err)
		}
	}

	scored := make([]scoredTool, 0, len(tools))
	for _, t := range tools {
		scored = append(scored, scoredTool{t, scoreTool(query, queryTokens, t, used[t.Name], queryVec, textVecs[toolEmbeddingText(t)])})
	}
	// 分数降序，同分按工具名升序，保证选择结果确定（工具注册表为 map，原始顺序随机）。
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].tool.Name < scored[j].tool.Name
	})

	budget := p.cfg.ToolBudget
	if budget <= 0 {
		budget = 8000
	}
	seen := make(map[string]bool, len(tools))
	out := make([]Tool, 0, max)
	usedBudget := 0

	// 必保集优先：通用工具 + 会话已用工具。
	for _, s := range scored {
		if seen[s.tool.Name] {
			continue
		}
		if isGeneralTool(s.tool) || used[s.tool.Name] {
			seen[s.tool.Name] = true
			out = append(out, s.tool)
			usedBudget += estimateToolTokens(s.tool)
		}
	}

	// 高分补充，受数量与 token 预算约束。
	for _, s := range scored {
		if len(out) >= max {
			break
		}
		if seen[s.tool.Name] {
			continue
		}
		if len(out) > 0 && usedBudget+estimateToolTokens(s.tool) > budget {
			continue
		}
		seen[s.tool.Name] = true
		out = append(out, s.tool)
		usedBudget += estimateToolTokens(s.tool)
	}

	// 按原始注册顺序排序返回（稳定，利于 prompt 缓存与可测试性）。
	idx := make(map[string]int, len(tools))
	for i, t := range tools {
		idx[t.Name] = i
	}
	slices.SortFunc(out, func(a, b Tool) int { return idx[a.Name] - idx[b.Name] })

	logger.Debugf("[AI] Selected %d/%d tools (budget %d/%d)", len(out), len(tools), usedBudget, budget)

	names := make([]string, 0, len(out))
	for _, t := range out {
		names = append(names, t.Name)
	}
	session.setToolSelection(&selectionCache{
		QueryTokens: queryTokens,
		At:          time.Now(),
		ToolCount:   len(tools),
		Names:       names,
	})

	return out
}
