// Package retrieval 检索与排序的公共骨架。
//
// 工具选择（builtin/ai/select.go）、历史消息检索（builtin/ai/rag.go）、长期记忆检索（builtin/ai/memory.go）
// 三个消费者共享同一套算法骨架：
//
//	tokenize → 候选筛选 → 关键词打分 →（可选）语义精排 → 排序 → Top-K
//
// 本文件只承载骨架本身（分词、重叠度、语义权重与向量获取、确定性排序、截断），
// 不承载领域模型：候选来源、入选门槛与筛选策略仍由各消费者自行决定。
package retrieval

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// ScoreEmbedW 语义余弦相似度权重（工具选择、历史检索、记忆检索共用）。
const ScoreEmbedW = 2.0

var wordRegexp = regexp.MustCompile(`[a-z0-9]+`)

// TokenizeText 将文本切分为加权 token 集合。
// 英文按小写单词切分；连续汉字按二元组切分（中文领域词无需分词库）；
// 孤立汉字（如 "B站" 中的 站）单独成 token，保证中英混合脚本可匹配。
func TokenizeText(text string) map[string]float64 {
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

// TokenOverlap 计算两个 token 集合的重叠加权和。
// 取 min 防止长文本重复 token 造成偏置。
func TokenOverlap(a, b map[string]float64) float64 {
	var s float64
	for k, av := range a {
		if bv, ok := b[k]; ok {
			s += min(av, bv)
		}
	}
	return s
}

// JaccardSimilarity 计算两个 token 集合的 Jaccard 相似度。
// 任一为空返回 0。
func JaccardSimilarity(a, b map[string]float64) float64 {
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

// SemanticCosine 返回查询向量与候选向量的余弦相似度；任一为空返回 0。
// 三个消费者共用同一语义项计算，避免各处重复判空。
func SemanticCosine(queryVec, candVec []float32) float64 {
	if queryVec == nil || candVec == nil {
		return 0
	}
	return float64(CosineSimilarity(queryVec, candVec))
}

// TokenJaccard 导出文本级 Jaccard 相似度（先按 TokenizeText 分词，再按 token
// 集合计算）；任一文本分词为空返回 0。
func TokenJaccard(a, b string) float64 {
	return JaccardSimilarity(TokenizeText(a), TokenizeText(b))
}

// RetrievalScore 组合关键词分与语义分：keyword + cosine × ScoreEmbedW。
func RetrievalScore(keyword, cosine float64) float64 {
	return keyword + cosine*ScoreEmbedW
}

// SemanticFallbackLog 语义向量获取失败时的降级日志文案。
// 三个消费者的文案各不相同，逐字保留——抽取骨架不得改变可观测日志。
type SemanticFallbackLog struct {
	TextsFailed string
	QueryFailed string
}

// AcquireSemanticVectors 获取查询与候选文本的语义向量：
//   - 候选文本走共享缓存（命中不重复嵌入，见 textVectorCache）
//   - 查询向量每次请求
//   - timeout > 0 时对本次嵌入施加超时
//
// 任一步失败即返回已取得的部分并记录既有降级日志，调用方据此回退纯关键词打分。
func AcquireSemanticVectors(ctx context.Context, cache *TextVectorCache, timeout time.Duration,
	query string, texts []string, log SemanticFallbackLog) ([]float32, map[string][]float32) {
	if cache == nil || !cache.Enabled() {
		return nil, nil
	}
	embCtx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		embCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	textVecs, err := cache.EmbedTexts(embCtx, texts)
	if err != nil {
		logger.Debugf(log.TextsFailed, err)
		return nil, nil
	}
	queryVec, err := cache.EmbedQuery(embCtx, query)
	if err != nil {
		logger.Debugf(log.QueryFailed, err)
		return nil, textVecs
	}
	return queryVec, textVecs
}

// RankByScore 按分数降序排序。
// tieBreak 为 nil 时保持 sort.Slice 的既有语义（同分不保证相对次序）；
// 提供 tieBreak 时用于确定性排序，保证同一输入产生同一序列。
func RankByScore[T any](items []T, score func(T) float64, tieBreak func(a, b T) bool) {
	sort.Slice(items, func(i, j int) bool {
		si, sj := score(items[i]), score(items[j])
		if si != sj {
			return si > sj
		}
		if tieBreak == nil {
			return false
		}
		return tieBreak(items[i], items[j])
	})
}

// TopK 截取排序结果的前 k 条（k 必须 ≥ 0）；候选不足时原样返回。
func TopK[T any](items []T, k int) []T {
	if len(items) > k {
		return items[:k]
	}
	return items
}
