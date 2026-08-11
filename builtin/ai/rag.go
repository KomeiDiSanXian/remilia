// Package ai rag.go — 消息级 RAG（历史消息语义检索）。
//
// 本文件实现从 messagelog 历史中检索相关消息注入系统提示：
//   - buildRAGContext：构建"相关历史消息"节（context_rag_messages > 0 时启用）
//   - ragCandidates：候选筛选——SQL 时间窗查询（复用 messagelog.QueryGroupFromDB /
//     QueryUserFromDB，群聊仅检索当前群，私聊按当前用户，不做跨群检索）
//   - rankHistory：两阶段排序——本地关键词预筛（零成本门槛，无命中不花
//     embedding）→ 对候选集做 embedding 语义精排（复用共享 textVectorCache）
//   - 与最近消息窗口（context_group_messages）按 EventID 去重
//   - 会话级缓存：TTL + 查询关键词 Jaccard 复用，避免连续消息重复检索
//
// 设计取舍：关键词预筛作为硬门槛——语义仅靠 embedding 召回的能力由
// 长期事实记忆承担（其检索自带语义加权），RAG 专注"记得原话关键词"的
// 历史细节查询，从而把 embedding 调用控制在命中候选时。
package ai

import (
	"context"
	"fmt"
	"maps"
	"sort"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/messagelog"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// ragRankCandidates 关键词预筛后进入 embedding 精排的候选数上限。
const ragRankCandidates = 20

// ragKeywordMinScore 关键词预筛的最小重叠分数（命中的二元组数）。
// 过滤"什么/怎么/的"等高频二元组单独命中造成的弱噪声；
// 短查询（如只含"食堂"两个汉字）无法达到门槛时由事实记忆承接。
const ragKeywordMinScore = 2

// ragHit 一条检索命中的历史消息。
type ragHit struct {
	Entry messagelog.RecordEntry
	Score float64
}

// ragCache 会话级历史检索缓存（json:"-" 不持久化）。
// 缓存完整注入文本：命中即零工作（含无结果的空缓存）。
type ragCache struct {
	QueryTokens map[string]float64
	At          time.Time
	ChatKey     string
	Text        string
}

// ragCacheAccessors 会话上的检索缓存读写。
func (s *Session) ragCacheGet() *ragCache {
	s.Lock()
	defer s.Unlock()
	if s.ragCache == nil {
		return nil
	}
	cp := *s.ragCache
	cp.QueryTokens = make(map[string]float64, len(s.ragCache.QueryTokens))
	maps.Copy(cp.QueryTokens, s.ragCache.QueryTokens)
	return &cp
}

func (s *Session) ragCacheSet(c *ragCache) {
	s.Lock()
	defer s.Unlock()
	s.ragCache = c
}

// buildRAGContext 检索并格式化相关历史消息注入文本（无命中返回空串）。
func (p *Plugin) buildRAGContext(ctx *eventctx.Context, session *Session) string {
	return p.buildRAGContextN(ctx, session, p.cfg.ContextRAGMessages)
}

// buildRAGContextN 同上，注入条数上限由调用方给定（预算编排时可动态缩减）。
// max 为注入条数上限（<=0 表示关闭本功能）。
func (p *Plugin) buildRAGContextN(ctx *eventctx.Context, session *Session, max int) string {
	if p.history == nil || max <= 0 {
		return ""
	}
	chat := ctx.GetChatInfo()
	if chat.ID == "" {
		return ""
	}
	query := getLastUserMessage(session)
	if query == "" {
		return ""
	}
	queryTokens := tokenizeText(query)

	// 会话缓存：TTL 内且关键词 Jaccard 相似复用（含空结果缓存）。
	chatKey := "u:" + chat.ID
	if chat.IsGroup {
		chatKey = "g:" + chat.ID
	}
	if cached := session.ragCacheGet(); cached != nil && cached.ChatKey == chatKey {
		if time.Since(cached.At) <= selectionCacheTTL &&
			jaccardSimilarity(queryTokens, cached.QueryTokens) >= selectionCacheJaccard {
			return cached.Text
		}
	}

	text := ""
	if entries, err := p.ragCandidates(chat); err == nil && len(entries) > 0 {
		// 注入上限 = min(启用条数, context_rag_inject_max)。
		injectMax := max
		if p.cfg.ContextRAGInjectMax > 0 && p.cfg.ContextRAGInjectMax < injectMax {
			injectMax = p.cfg.ContextRAGInjectMax
		}
		text = p.formatRAGHitsN(ctx, entries, query, injectMax)
	}

	session.ragCacheSet(&ragCache{
		QueryTokens: queryTokens,
		At:          time.Now(),
		ChatKey:     chatKey,
		Text:        text,
	})
	return text
}

// ragCandidates 查询候选消息：当前群/当前用户、最近 N 天、上限 M 条（仅入站）。
func (p *Plugin) ragCandidates(chat platform.ChatInfo) ([]messagelog.RecordEntry, error) {
	days := p.cfg.ContextRAGDays
	if days <= 0 {
		days = 7
	}
	limit := p.cfg.ContextRAGCandidates
	if limit <= 0 {
		limit = 500
	}
	since := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	if chat.IsGroup {
		return p.history.QueryGroupFromDB(chat.ID, since, time.Now(), limit)
	}
	return p.history.QueryUserFromDB(chat.ID, since, time.Now(), limit)
}

// formatRAGHitsN 两阶段排序并格式化命中消息。
// 阶段 1 关键词预筛（tokenOverlap ≥ ragKeywordMinScore 才入选，最多
// ragRankCandidates 条）；零命中且 embedding 可用时走语义兜底——
// 取最近候选集直接嵌入精排（覆盖"记不清原话"的查询，仅零命中才花费
// embedding 调用）；阶段 2 对候选做 embedding 精排（失败降级纯关键词），
// 取 Top-N（max）注入。与最近消息窗口（context_group_messages）按 EventID 去重。
func (p *Plugin) formatRAGHitsN(ctx *eventctx.Context, entries []messagelog.RecordEntry, query string, max int) string {
	if max <= 0 {
		return ""
	}
	queryTokens := tokenizeText(query)

	// 与最近窗口去重：窗口内已有的事件不再重复注入。
	skip := make(map[string]bool)
	if p.cfg.ContextGroupMessages > 0 {
		var recent []messagelog.RecordEntry
		if chat := ctx.GetChatInfo(); chat.IsGroup {
			recent = p.history.QueryGroupRecent(chat.ID, p.cfg.ContextGroupMessages)
		} else {
			recent = p.history.QueryUser(chat.ID, p.cfg.ContextGroupMessages)
		}
		for _, e := range recent {
			if e.EventID != "" {
				skip[e.EventID] = true
			}
		}
	}

	var scored []ragHit
	for _, e := range entries {
		if e.Content == "" || e.Platform == "synthetic" {
			continue
		}
		text := strings.TrimSpace(stripMentionMarkup(e.Content))
		if text == "" {
			continue
		}
		if e.EventID != "" && skip[e.EventID] {
			continue
		}
		if score := tokenOverlap(queryTokens, tokenizeText(text)); score >= ragKeywordMinScore {
			scored = append(scored, ragHit{Entry: e, Score: score})
		}
	}

	// 语义兜底：关键词零命中但 embedding 可用时，取最近候选直接语义精排
	// （覆盖"上次说的那个方案"这类记不清原话的查询）。
	if len(scored) == 0 {
		if p.emb == nil || !p.emb.Enabled() {
			return ""
		}
		for _, e := range entries {
			if e.Content == "" || e.Platform == "synthetic" {
				continue
			}
			if e.EventID != "" && skip[e.EventID] {
				continue
			}
			if strings.TrimSpace(stripMentionMarkup(e.Content)) == "" {
				continue
			}
			scored = append(scored, ragHit{Entry: e})
			if len(scored) >= ragRankCandidates {
				break
			}
		}
		if len(scored) == 0 {
			return ""
		}
	} else {
		// 关键词预筛：取分数最高的候选进入 embedding 精排
		sort.Slice(scored, func(i, j int) bool { return scored[i].Score > scored[j].Score })
		if len(scored) > ragRankCandidates {
			scored = scored[:ragRankCandidates]
		}
	}

	// 阶段 2：embedding 语义精排（复用共享缓存；失败降级纯关键词排序）。
	queryVec, textVecs := p.embedRAGTexts(ctx.Context(), query, scored)
	for i := range scored {
		if queryVec != nil {
			if v, ok := textVecs[scored[i].Entry.Content]; ok {
				scored[i].Score += float64(cosineSimilarity(queryVec, v)) * scoreEmbedW
			}
		}
	}
	sort.Slice(scored, func(i, j int) bool { return scored[i].Score > scored[j].Score })

	if len(scored) > max {
		scored = scored[:max]
	}

	var b strings.Builder
	for _, h := range scored {
		name := h.Entry.UserName
		if name == "" {
			name = h.Entry.UserID
		}
		if name == "" {
			name = "未知"
		}
		ts := h.Entry.Timestamp.Format("01-02 15:04")
		text := truncateRunes(strings.TrimSpace(stripMentionMarkup(h.Entry.Content)), 200)
		fmt.Fprintf(&b, "[%s] %s: %s\n", ts, name, text)
	}
	return "（以下为近期历史消息中检索到的相关内容，回答时可参考，注意消息时效性）\n" +
		strings.TrimRight(b.String(), "\n")
}

// embedRAGTexts 对候选消息与查询做嵌入。失败返回 nil 向量（调用方降级）。
func (p *Plugin) embedRAGTexts(ctx context.Context, query string, hits []ragHit) ([]float32, map[string][]float32) {
	if p.emb == nil || !p.emb.Enabled() || len(hits) == 0 {
		return nil, nil
	}
	texts := make([]string, 0, len(hits))
	for _, h := range hits {
		texts = append(texts, h.Entry.Content)
	}
	embCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	textVecs, err := p.emb.EmbedTexts(embCtx, texts)
	if err != nil {
		logger.Debugf("[AI] RAG embedding failed, keyword-only ranking: %v", err)
		return nil, nil
	}
	qv, err := p.emb.EmbedQuery(embCtx, query)
	if err != nil {
		logger.Debugf("[AI] RAG embedding query failed, keyword-only ranking: %v", err)
		return nil, textVecs
	}
	return qv, textVecs
}
