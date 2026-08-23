package knowledgebase

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/KomeiDiSanXian/remilia/builtin/ai"
)

// Hit 一条检索命中。
type Hit struct {
	Source  string
	Heading string
	Content string
	Score   float32
}

// Search 检索知识库：embedding 可用时语义精排，否则关键词重叠兜底。
func (p *Plugin) Search(ctx context.Context, query string, limit int) ([]Hit, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = p.cfg.MaxResults
	}

	p.mu.RLock()
	idx := p.index
	embedder := p.embedder
	p.mu.RUnlock()
	if len(idx) == 0 {
		return nil, nil
	}

	// 语义检索：查询嵌入一次，对全量内存索引做余弦精排。
	if embedder != nil {
		if vecs, err := embedder.Embed(ctx, []string{query}); err == nil && len(vecs) == 1 && len(vecs[0]) > 0 {
			scored := make([]Hit, 0, len(idx))
			for _, c := range idx {
				if len(c.Vector) == 0 || len(c.Vector) != len(vecs[0]) {
					continue
				}
				s := ai.CosineSimilarity(vecs[0], c.Vector)
				if s <= 0 {
					continue
				}
				scored = append(scored, Hit{Source: c.Source, Heading: c.Heading, Content: c.Content, Score: s})
			}
			return topHits(scored, limit), nil
		}
	}

	// 关键词兜底：CJK 二元组 + 英文单词重叠打分。
	queryTokens := tokenize(query)
	scored := make([]Hit, 0)
	for _, c := range idx {
		s := tokenOverlap(queryTokens, tokenize(c.Heading+"\n"+c.Content))
		if s > 0 {
			scored = append(scored, Hit{Source: c.Source, Heading: c.Heading, Content: c.Content, Score: float32(s)})
		}
	}
	return topHits(scored, limit), nil
}

// topHits 按分数降序、同分按来源稳定排序，去重后取前 n 条。
func topHits(hits []Hit, n int) []Hit {
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		return hits[i].Source < hits[j].Source
	})
	// 仅在 Top 候选中去重（重复只会出现在高分段），避免对全量候选做 O(n²) 分词。
	// 候选数取 max(n*3, 30)：去重后仍能填满 n 条。
	pool := max(n*3, 30)
	if len(hits) > pool {
		hits = hits[:pool]
	}
	out := dedupNearDuplicates(hits)
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// dedupNearDuplicates 丢弃同源同标题且内容高度重复的命中。
// 分块重叠会让同一小节产生两条几乎相同的候选，去重后结果更多样。
func dedupNearDuplicates(hits []Hit) []Hit {
	out := make([]Hit, 0, len(hits))
	for _, h := range hits {
		dup := false
		for _, kept := range out {
			if kept.Source != h.Source || kept.Heading != h.Heading {
				continue
			}
			if tokenJaccard(kept.Content, h.Content) > 0.6 {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, h)
		}
	}
	return out
}

// tokenJaccard 两个文本 token 集合的 Jaccard 相似度。
func tokenJaccard(a, b string) float64 {
	ta, tb := tokenize(a), tokenize(b)
	if len(ta) == 0 || len(tb) == 0 {
		return 0
	}
	inter := 0
	for k := range ta {
		if _, ok := tb[k]; ok {
			inter++
		}
	}
	union := len(ta) + len(tb) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// tokenize 分词：英文小写单词 + 连续汉字二元组（与 AI 插件工具选择同思路）。
func tokenize(text string) map[string]float64 {
	tokens := make(map[string]float64)
	lower := strings.ToLower(text)
	var word []rune
	flushWord := func() {
		if len(word) > 0 {
			tokens[string(word)]++
			word = word[:0]
		}
	}
	runes := []rune(lower)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		// 仅 ASCII 字母/数字作为词 token；汉字走下方二元组分词，
		// 避免 unicode.IsLetter 把连续汉字误判为单个英文单词。
		if r < utf8.RuneSelf && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			word = append(word, r)
			continue
		}
		flushWord()
		if !unicode.Is(unicode.Han, r) {
			continue
		}
		// 连续汉字段：二元组 + 孤立单字。
		j := i
		for j < len(runes) && unicode.Is(unicode.Han, runes[j]) {
			j++
		}
		if j-i == 1 {
			tokens[string(runes[i])]++
		}
		for k := i; k < j-1; k++ {
			tokens[string(runes[k:k+2])]++
		}
		i = j - 1
	}
	flushWord()
	return tokens
}

// tokenOverlap 两个 token 集合的重叠加权（取 min 防长文本偏置）。
func tokenOverlap(a, b map[string]float64) float64 {
	var s float64
	for k, av := range a {
		if bv, ok := b[k]; ok {
			if av < bv {
				s += av
			} else {
				s += bv
			}
		}
	}
	return s
}

// FormatHits 将检索结果格式化为工具返回文本。
func FormatHits(query string, hits []Hit, maxContentRunes int) string {
	if maxContentRunes <= 0 {
		maxContentRunes = 800
	}
	var b strings.Builder
	if len(hits) == 0 {
		return "知识库中没有找到与「" + query + "」相关的内容。可尝试换一种问法或补充关键词。"
	}
	b.WriteString("知识库检索结果（按相关度排序）：\n\n")
	for i, h := range hits {
		fmt.Fprintf(&b, "%d. 来源：`%s`", i+1, h.Source)
		if h.Heading != "" {
			b.WriteString(" ｜ 标题：" + h.Heading)
		}
		fmt.Fprintf(&b, " ｜ 相关度：%.3f\n", h.Score)
		content := truncateRunes(h.Content, maxContentRunes)
		b.WriteString(content)
		b.WriteString("\n\n")
	}
	b.WriteString("（引用来源时请注明文档路径。若以上内容不足，可换关键词再次检索。）")
	return b.String()
}

// truncateRunes 按 rune 截断并追加省略号。
func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}
