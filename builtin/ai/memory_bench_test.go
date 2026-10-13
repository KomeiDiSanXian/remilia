package ai

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/retrieval"
)

var benchmarkMemoryFactsSink []memoryFact

type memoryBenchmarkEmbedder struct{}

func (memoryBenchmarkEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	vectors := make([][]float32, len(texts))
	for i := range vectors {
		vectors[i] = []float32{0.5, 0.25, 0.75, 0.125}
	}
	return vectors, nil
}

func (memoryBenchmarkEmbedder) Model() string { return "memory-benchmark" }

func benchmarkMemoryFacts(count int) []memoryFact {
	facts := make([]memoryFact, count)
	updatedAt := time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)
	for i := range facts {
		facts[i] = memoryFact{
			Text:      fmt.Sprintf("用户关注天气预报与日程安排，长期偏好记录编号 %04d。", i),
			UpdatedAt: updatedAt.Add(time.Duration(i) * time.Minute),
			Count:     i%5 + 1,
		}
	}
	return facts
}

func benchmarkMemoryStore(scope string, facts []memoryFact, emb *retrieval.TextVectorCache) *memoryStore {
	return &memoryStore{
		scopes: map[string][]memoryFact{scope: facts},
		emb:    emb,
	}
}

func BenchmarkMemoryRetrieve(b *testing.B) {
	const (
		scope = "user:benchmark"
		query = "最近的天气预报和日程安排是什么"
	)
	ctx := context.Background()

	for _, count := range []int{50, 200, 500} {
		facts := benchmarkMemoryFacts(count)
		b.Run(fmt.Sprintf("facts_%d/keyword_only", count), func(b *testing.B) {
			store := benchmarkMemoryStore(scope, facts, nil)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				benchmarkMemoryFactsSink = store.Retrieve(ctx, scope, query, 8)
			}
		})

		b.Run(fmt.Sprintf("facts_%d/cached_embeddings", count), func(b *testing.B) {
			cache := retrieval.NewTextVectorCache(memoryBenchmarkEmbedder{})
			texts := make([]string, len(facts))
			for i, fact := range facts {
				texts[i] = fact.Text
			}
			if _, err := cache.EmbedTexts(ctx, texts); err != nil {
				b.Fatal(err)
			}
			store := benchmarkMemoryStore(scope, facts, cache)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				benchmarkMemoryFactsSink = store.Retrieve(ctx, scope, query, 8)
			}
		})
	}
}
