// history.go — 消息级 RAG（历史消息语义检索）。
//
// 从 messagelog 历史中检索相关消息，作为动态上下文的"相关历史消息"节：
//   - ragCandidates：候选筛选——时间窗查询（messagelog.QueryRange，热缓存 +
//     SQLite 合并；群聊仅检索当前群，私聊按当前会话，不做跨群检索）
//   - formatRAGHits：两阶段排序——本地关键词预筛（零成本门槛，无命中不花
//     embedding）→ 对候选集做 embedding 语义精排（复用共享 retrieval.TextVectorCache）
//   - 与最近消息窗口（context_group_messages）按 EventID 去重
//   - 会话级缓存：TTL + 查询关键词 Jaccard 复用，避免连续消息重复检索
//     （缓存槽位与会话同生命周期，定义见 builtin/ai/session）
//
// 设计取舍：关键词预筛作为硬门槛——语义仅靠 embedding 召回的能力由
// 长期事实记忆承担（其检索自带语义加权），RAG 专注"记得原话关键词"的
// 历史细节查询，从而把 embedding 调用控制在命中候选时。
package promptctx

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/retrieval"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/textutil"
	"github.com/KomeiDiSanXian/remilia/builtin/messagelog"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// RankCandidates 关键词预筛后进入 embedding 精排的候选数上限。
const RankCandidates = 20

// KeywordMinScore 关键词预筛的最小重叠分数（命中的二元组数）。
// 过滤"什么/怎么/的"等高频二元组单独命中造成的弱噪声；
// 短查询（如只含"食堂"两个汉字）无法达到门槛时由事实记忆承接。
const KeywordMinScore = 2

// ragHit 一条检索命中的历史消息。
type ragHit struct {
	Entry messagelog.RecordEntry
	Score float64
}

// ragHitTieBreak 为历史检索提供确定性同分次序：分数相同时较新的消息优先，
// 同一时刻再按事件 ID、内容升序，避免排序结果随底层加载顺序抖动。
func ragHitTieBreak(a, b ragHit) bool {
	if !a.Entry.Timestamp.Equal(b.Entry.Timestamp) {
		return a.Entry.Timestamp.After(b.Entry.Timestamp)
	}
	if a.Entry.EventID != b.Entry.EventID {
		return a.Entry.EventID < b.Entry.EventID
	}
	return a.Entry.Content < b.Entry.Content
}

// BuildRAGContext 检索并格式化相关历史消息注入文本（无命中返回空串）。
//
// query 为本轮检索用的用户消息文本（通常取最后一条用户消息）；
// 由调用方给定，因为同一份查询也被记忆检索与工具选择复用。
func BuildRAGContext(history *messagelog.Logger, emb *retrieval.TextVectorCache, cfg *config.Config, ctx *eventctx.Context, sess *session.Session, query string, max int) string {
	if history == nil || max <= 0 {
		return ""
	}
	chat := ctx.GetChatInfo()
	if chat.ID == "" {
		return ""
	}
	if query == "" {
		return ""
	}
	queryTokens := retrieval.TokenizeText(query)

	// 会话缓存：TTL 内且关键词 Jaccard 相似复用（含空结果缓存）。
	chatKey := "u:" + chat.ID
	if chat.IsGroup {
		chatKey = "g:" + chat.ID
	}
	if cached := sess.RAGCacheSnapshot(); cached != nil && cached.ChatKey == chatKey {
		if time.Since(cached.At) <= session.CacheReuseTTL &&
			retrieval.JaccardSimilarity(queryTokens, cached.QueryTokens) >= session.CacheReuseJaccard {
			return cached.Text
		}
	}

	text := ""
	if entries, err := ragCandidates(history, cfg, chat); err == nil && len(entries) > 0 {
		// 注入上限 = min(启用条数, context_rag_inject_max)。
		injectMax := max
		if cfg.ContextRAGInjectMax > 0 && cfg.ContextRAGInjectMax < injectMax {
			injectMax = cfg.ContextRAGInjectMax
		}
		text = formatRAGHits(history, emb, cfg, ctx, entries, query, injectMax)
	}

	sess.SetRAGCache(&session.RAGCache{
		QueryTokens: queryTokens,
		At:          time.Now(),
		ChatKey:     chatKey,
		Text:        text,
	})
	return text
}

// ragCandidates 查询候选消息：当前会话、最近 N 天、上限 M 条（仅入站）。
// 统一走 QueryRange（热缓存 + SQLite 合并，私聊刚发未 flush 的消息也能命中），
// 返回最新在前——语义兜底"取最近候选"依赖此顺序（与旧 QueryGroupFromDB /
// QueryUserFromDB 语义一致）。
func ragCandidates(history *messagelog.Logger, cfg *config.Config, chat platform.ChatInfo) ([]messagelog.RecordEntry, error) {
	days := cfg.ContextRAGDays
	if days <= 0 {
		days = 7
	}
	limit := cfg.ContextRAGCandidates
	if limit <= 0 {
		limit = 500
	}
	since := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	entries, err := history.QueryRange(chat.ID, since, time.Now(), limit,
		messagelog.QueryOptions{Direction: messagelog.DirectionInbound})
	if err != nil {
		return nil, err
	}
	slices.Reverse(entries)
	return entries, nil
}

// formatRAGHits 两阶段排序并格式化命中消息。
// 阶段 1 关键词预筛（tokenOverlap ≥ KeywordMinScore 才入选，最多
// RankCandidates 条）；零命中且 embedding 可用时走语义兜底——取最近候选集
// 直接嵌入精排（覆盖"记不清原话"的查询，仅零命中才花费 embedding 调用）；
// 阶段 2 对候选做 embedding 精排（失败降级纯关键词），取 Top-N 注入。
// 与最近消息窗口（context_group_messages）按 EventID 去重。
func formatRAGHits(history *messagelog.Logger, emb *retrieval.TextVectorCache, cfg *config.Config, ctx *eventctx.Context, entries []messagelog.RecordEntry, query string, max int) string {
	if max <= 0 {
		return ""
	}
	queryTokens := retrieval.TokenizeText(query)

	// 与最近窗口去重：窗口内已有的事件不再重复注入。
	skip := make(map[string]bool)
	if cfg.ContextGroupMessages > 0 {
		var recent []messagelog.RecordEntry
		if chat := ctx.GetChatInfo(); chat.IsGroup {
			recent = history.QueryGroupRecent(chat.ID, cfg.ContextGroupMessages)
		} else {
			recent = history.QueryUser(chat.ID, cfg.ContextGroupMessages)
		}
		for _, e := range recent {
			if e.EventID != "" {
				skip[e.EventID] = true
			}
		}
	}

	prefilterHits := 0
	var scored []ragHit
	for _, e := range entries {
		if e.Content == "" || e.Platform == "synthetic" {
			continue
		}
		text := strings.TrimSpace(textutil.StripMentionMarkup(e.Content))
		if text == "" {
			continue
		}
		if e.EventID != "" && skip[e.EventID] {
			continue
		}
		if score := retrieval.TokenOverlap(queryTokens, retrieval.TokenizeText(text)); score >= KeywordMinScore {
			prefilterHits++
			scored = append(scored, ragHit{Entry: e, Score: score})
		}
	}

	// 语义兜底：关键词零命中但 embedding 可用时，取最近候选直接语义精排
	// （覆盖"上次说的那个方案"这类记不清原话的查询）。
	usedFallback := false
	if len(scored) == 0 {
		if emb == nil || !emb.Enabled() {
			return ""
		}
		usedFallback = true
		for _, e := range entries {
			if e.Content == "" || e.Platform == "synthetic" {
				continue
			}
			if e.EventID != "" && skip[e.EventID] {
				continue
			}
			if strings.TrimSpace(textutil.StripMentionMarkup(e.Content)) == "" {
				continue
			}
			scored = append(scored, ragHit{Entry: e})
			if len(scored) >= RankCandidates {
				break
			}
		}
		if len(scored) == 0 {
			return ""
		}
	} else {
		// 关键词预筛：取分数最高的候选进入 embedding 精排
		retrieval.RankByScore(scored, func(h ragHit) float64 { return h.Score }, ragHitTieBreak)
		scored = retrieval.TopK(scored, RankCandidates)
	}
	// 可观测性：预筛监控。keyword_hits 为达到门槛的原始命中数；
	// fallback=true 表示关键词零命中、走语义兜底（覆盖"记不清原话"查询）。
	logger.Debugf("[AI] RAG prefilter query=%q keyword_hits=%d candidates=%d fallback=%v",
		textutil.TruncateRunes(query, 60), prefilterHits, len(scored), usedFallback)

	// 阶段 2：embedding 语义精排（复用共享缓存；失败降级纯关键词排序）。
	queryVec, textVecs := embedRAGTexts(ctx.Context(), emb, query, scored)
	for i := range scored {
		scored[i].Score = retrieval.RetrievalScore(scored[i].Score,
			retrieval.SemanticCosine(queryVec, textVecs[scored[i].Entry.Content]))
	}
	// 分数降序，同分按 ragHitTieBreak 给出确定次序（较新的消息优先），
	// 与工具/记忆检索一致，避免同分结果随底层顺序抖动。
	retrieval.RankByScore(scored, func(h ragHit) float64 { return h.Score }, ragHitTieBreak)

	scored = retrieval.TopK(scored, max)
	// 可观测性：最终注入的 Top-3（含分数），用于核对 embedding 精排是否改变结果。
	for i := 0; i < len(scored) && i < 3; i++ {
		h := scored[i]
		logger.Debugf("[AI] RAG hit #%d score=%.2f %s", i+1, h.Score,
			textutil.TruncateRunes(textutil.StripMentionMarkup(h.Entry.Content), 60))
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
		text := textutil.TruncateRunes(strings.TrimSpace(textutil.StripMentionMarkup(h.Entry.Content)), 200)
		fmt.Fprintf(&b, "[%s] %s: %s\n", ts, name, text)
	}
	return "（以下为近期历史消息中检索到的相关内容，回答时可参考，注意消息时效性）\n" +
		strings.TrimRight(b.String(), "\n")
}

// embedRAGTexts 对候选消息与查询做嵌入。失败返回 nil 向量（调用方降级）。
func embedRAGTexts(ctx context.Context, emb *retrieval.TextVectorCache, query string, hits []ragHit) ([]float32, map[string][]float32) {
	if emb == nil || !emb.Enabled() || len(hits) == 0 {
		return nil, nil
	}
	texts := make([]string, 0, len(hits))
	for _, h := range hits {
		texts = append(texts, h.Entry.Content)
	}
	return retrieval.AcquireSemanticVectors(ctx, emb, 10*time.Second, query, texts, retrieval.SemanticFallbackLog{
		TextsFailed: "[AI] RAG embedding failed, keyword-only ranking: %v",
		QueryFailed: "[AI] RAG embedding query failed, keyword-only ranking: %v",
	})
}
