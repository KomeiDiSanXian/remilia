// retrieval_test.go — 检索骨架的契约用例。
//
// 三个消费者（工具选择 / 历史消息检索 / 长期记忆检索）共享本包的
// 算法骨架；这些断言锁定骨架自身的语义：语义权重组合、向量获取的降级路径、
// 文本向量缓存的复用次数、确定性排序与 Top-K 截断。
package retrieval

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scriptedEmbedder 按调用形态决定成功/失败，用于区分"批量文本嵌入"与"单条查询嵌入"。
type scriptedEmbedder struct {
	calls     int
	failBatch bool
	failQuery bool
}

func (e *scriptedEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	e.calls++
	if len(texts) == 1 && e.failQuery {
		return nil, errors.New("query embed failed")
	}
	if len(texts) > 1 && e.failBatch {
		return nil, errors.New("batch embed failed")
	}
	out := make([][]float32, len(texts))
	for i := range out {
		out[i] = []float32{1, 0, 0}
	}
	return out, nil
}

func (e *scriptedEmbedder) Model() string { return "scripted" }

func TestSemanticCosineNilAndEmptySafety(t *testing.T) {
	assert.Equal(t, 0.0, SemanticCosine(nil, []float32{1, 0}))
	assert.Equal(t, 0.0, SemanticCosine([]float32{1, 0}, nil))
	assert.Equal(t, 0.0, SemanticCosine([]float32{}, []float32{1, 0}))
	assert.Equal(t, 0.0, SemanticCosine([]float32{1, 0}, []float32{}))
	assert.Equal(t, 0.0, SemanticCosine([]float32{0, 0}, []float32{1, 0}))
	assert.InDelta(t, 1.0, SemanticCosine([]float32{1, 0}, []float32{1, 0}), 1e-6)
}

func TestRetrievalScoreComposition(t *testing.T) {
	// 组合公式：keyword + cosine × ScoreEmbedW。
	assert.Equal(t, 3.0+0.5*ScoreEmbedW, RetrievalScore(3.0, 0.5))
	// 无语义信号时必须与原关键词分逐位相等（降级路径不得引入浮点扰动）。
	base := 2.7000000000000002
	assert.Equal(t, base, RetrievalScore(base, 0))
}

func TestAcquireSemanticVectorsDisabled(t *testing.T) {
	q, texts := AcquireSemanticVectors(context.Background(), nil, 0, "q", []string{"a"}, SemanticFallbackLog{})
	assert.Nil(t, q)
	assert.Nil(t, texts)

	emb := &scriptedEmbedder{}
	cache := NewTextVectorCache(nil)
	q, texts = AcquireSemanticVectors(context.Background(), cache, 0, "q", []string{"a"}, SemanticFallbackLog{})
	assert.Nil(t, q)
	assert.Nil(t, texts)
	assert.Zero(t, emb.calls)
}

func TestAcquireSemanticVectorsBatchFailureDegrades(t *testing.T) {
	emb := &scriptedEmbedder{failBatch: true}
	q, texts := AcquireSemanticVectors(context.Background(), NewTextVectorCache(emb), 0,
		"query", []string{"a", "b"}, SemanticFallbackLog{})
	assert.Nil(t, q)
	assert.Nil(t, texts)
	assert.Equal(t, 1, emb.calls)
}

func TestAcquireSemanticVectorsQueryFailureKeepsTextVectors(t *testing.T) {
	emb := &scriptedEmbedder{failQuery: true}
	q, texts := AcquireSemanticVectors(context.Background(), NewTextVectorCache(emb), 0,
		"query", []string{"a", "b"}, SemanticFallbackLog{})
	assert.Nil(t, q)
	require.Len(t, texts, 2, "文本向量失败前已取得的缓存必须保留")
	assert.Contains(t, texts, "a")
	assert.Contains(t, texts, "b")
	assert.Equal(t, 2, emb.calls)
}

func TestAcquireSemanticVectorsReusesTextCache(t *testing.T) {
	emb := &scriptedEmbedder{}
	cache := NewTextVectorCache(emb)
	texts := []string{"tool a", "tool b"}

	q, vecs := AcquireSemanticVectors(context.Background(), cache, 0, "first", texts, SemanticFallbackLog{})
	require.NotNil(t, q)
	require.Len(t, vecs, 2)
	assert.Equal(t, 2, emb.calls, "首次：1 次文本批量嵌入 + 1 次查询嵌入")

	q, vecs = AcquireSemanticVectors(context.Background(), cache, 0, "second", texts, SemanticFallbackLog{})
	require.NotNil(t, q)
	require.Len(t, vecs, 2)
	assert.Equal(t, 3, emb.calls, "再次：文本命中缓存，只新增 1 次查询嵌入")
}

// rankItem 排序用例的简单载体。
type rankItem struct {
	name  string
	score float64
}

func TestRankByScoreDescendingWithTieBreak(t *testing.T) {
	items := []rankItem{{"b", 1}, {"a", 3}, {"c", 1}, {"d", 0}}
	RankByScore(items,
		func(it rankItem) float64 { return it.score },
		func(a, b rankItem) bool { return a.name < b.name })

	assert.Equal(t, []string{"a", "b", "c", "d"}, []string{items[0].name, items[1].name, items[2].name, items[3].name})
}

func TestRankByScoreNilTieBreakMatchesLegacyComparator(t *testing.T) {
	mk := func() []rankItem {
		return []rankItem{{"b", 1}, {"e", 2}, {"a", 3}, {"c", 1}, {"d", 0}, {"f", 2}}
	}
	got := mk()
	RankByScore(got, func(it rankItem) float64 { return it.score }, nil)

	// 既有实现：sort.Slice + 仅按分数降序（同分不设次序）。
	want := mk()
	sort.Slice(want, func(i, j int) bool { return want[i].score > want[j].score })

	assert.Equal(t, want, got)
}

func TestTopKTruncatesAndPreserves(t *testing.T) {
	items := []int{5, 4, 3, 2, 1}
	assert.Equal(t, []int{5, 4, 3}, TopK(items, 3))
	assert.Equal(t, items, TopK(items, 5))
	assert.Equal(t, items, TopK(items, 7))
}

// TestExportedRetrievalPrimitives 冻结对外原语的语义：同仓插件
// （如 knowledgebase）直接复用这些符号，行为不得漂移。
func TestExportedRetrievalPrimitives(t *testing.T) {
	text := "如何实现定时任务 scheduler plugin"
	assert.Contains(t, TokenizeText(text), "scheduler")
	assert.Contains(t, TokenizeText(text), "定时")

	a := TokenizeText("服务器 方案 选型")
	b := TokenizeText("服务器 方案 落地")
	assert.Positive(t, TokenOverlap(a, b))
	assert.Equal(t, JaccardSimilarity(a, b), TokenJaccard("服务器 方案 选型", "服务器 方案 落地"))
	assert.Zero(t, TokenJaccard("", "x"))
	assert.Equal(t, 1.0, TokenJaccard("定时任务", "定时任务"))
}
