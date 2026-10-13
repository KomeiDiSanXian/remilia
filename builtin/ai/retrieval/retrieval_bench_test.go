package retrieval

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

var (
	benchmarkTokensSink      map[string]float64
	benchmarkScoreSink       float64
	benchmarkRankedSink      []benchmarkScoredCandidate
	benchmarkQueryVectorSink []float32
	benchmarkTextVectorsSink map[string][]float32
)

type benchmarkScoredCandidate struct {
	name  string
	score float64
}

func benchmarkCandidateScore(candidate benchmarkScoredCandidate) float64 {
	return candidate.score
}

func benchmarkCandidateTieBreak(a, b benchmarkScoredCandidate) bool {
	return a.name < b.name
}

func benchmarkTexts(count int) []string {
	texts := make([]string, count)
	for i := range texts {
		texts[i] = fmt.Sprintf("天气 forecast weather scheduler 日程任务 #%04d，需要结合历史记录查询相关信息。", i)
	}
	return texts
}

type benchmarkEmbedder struct{}

func (benchmarkEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	vectors := make([][]float32, len(texts))
	for i := range vectors {
		vectors[i] = []float32{0.5, 0.25, 0.75, 0.125}
	}
	return vectors, nil
}

func (benchmarkEmbedder) Model() string { return "benchmark-embedder" }

func BenchmarkTokenizeText(b *testing.B) {
	inputs := []struct {
		name string
		text string
	}{
		{name: "short_mixed_query", text: "查询 Tokyo weather forecast 明天"},
		{name: "chinese_query", text: "请找出上周讨论过的任务调度和天气预报方案"},
		{name: "long_context", text: strings.Repeat("用户讨论 weather forecast、任务 scheduler 和历史记录。", 40)},
	}
	for _, input := range inputs {
		b.Run(input.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				benchmarkTokensSink = TokenizeText(input.text)
			}
		})
	}
}

func BenchmarkKeywordCandidateRanking(b *testing.B) {
	queryTokens := TokenizeText("查询 Tokyo weather forecast 明天的 scheduler 任务安排")
	for _, count := range []int{20, 100, 500} {
		texts := benchmarkTexts(count)
		names := make([]string, count)
		for i := range names {
			names[i] = fmt.Sprintf("record_%04d", i)
		}
		b.Run(fmt.Sprintf("candidates_%d", count), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				scored := make([]benchmarkScoredCandidate, 0, len(texts))
				for j, text := range texts {
					score := TokenOverlap(queryTokens, TokenizeText(text))
					if score > 0 {
						scored = append(scored, benchmarkScoredCandidate{
							name:  names[j],
							score: score,
						})
					}
				}
				RankByScore(scored, benchmarkCandidateScore, benchmarkCandidateTieBreak)
				benchmarkRankedSink = TopK(scored, min(20, len(scored)))
			}
		})
	}
}

func BenchmarkSimilarityPrimitives(b *testing.B) {
	for _, count := range []int{8, 64, 256} {
		query := make(map[string]float64, count)
		candidate := make(map[string]float64, count)
		for i := range count {
			token := fmt.Sprintf("token_%04d", i)
			query[token] = 1
			if i%2 == 0 {
				candidate[token] = 1
			} else {
				candidate[fmt.Sprintf("candidate_%04d", i)] = 1
			}
		}
		b.Run(fmt.Sprintf("tokens_%d/overlap", count), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				benchmarkScoreSink = TokenOverlap(query, candidate)
			}
		})
		b.Run(fmt.Sprintf("tokens_%d/jaccard", count), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				benchmarkScoreSink = JaccardSimilarity(query, candidate)
			}
		})
	}
}

func BenchmarkCosineSimilarity(b *testing.B) {
	for _, dimensions := range []int{128, 768, 1536} {
		a := make([]float32, dimensions)
		other := make([]float32, dimensions)
		for i := range a {
			a[i] = float32(i%17+1) / 17
			other[i] = float32((i*7)%19+1) / 19
		}
		b.Run(fmt.Sprintf("dimensions_%d", dimensions), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				benchmarkScoreSink = float64(CosineSimilarity(a, other))
			}
		})
	}
}

func BenchmarkAcquireSemanticVectorsCacheHit(b *testing.B) {
	ctx := context.Background()
	for _, count := range []int{20, 100, 500} {
		texts := benchmarkTexts(count)
		cache := NewTextVectorCache(benchmarkEmbedder{})
		if _, err := cache.EmbedTexts(ctx, texts); err != nil {
			b.Fatal(err)
		}
		b.Run(fmt.Sprintf("candidates_%d", count), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				benchmarkQueryVectorSink, benchmarkTextVectorsSink = AcquireSemanticVectors(
					ctx, cache, 0, "当前查询 weather forecast", texts, SemanticFallbackLog{},
				)
			}
		})
	}
}
