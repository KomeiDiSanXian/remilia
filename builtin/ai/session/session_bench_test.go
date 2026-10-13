package session

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var (
	benchmarkSelectionCacheSink *SelectionCache
	benchmarkRAGCacheSink       *RAGCache
	benchmarkSessionSink        *Session
)

func BenchmarkToolSelectionSnapshot(b *testing.B) {
	for _, count := range []int{0, 20, 100, 500} {
		tokens := make(map[string]float64, count)
		names := make([]string, count)
		for i := range count {
			tokens[fmt.Sprintf("token_%04d", i)] = 1
			names[i] = fmt.Sprintf("tool_%04d", i)
		}

		sess := &Session{}
		sess.SetToolSelection(&SelectionCache{
			QueryTokens:       tokens,
			At:                time.Now(),
			ToolCount:         count,
			Names:             names,
			CatalogGeneration: 1,
		})

		b.Run(fmt.Sprintf("entries_%d", count), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				benchmarkSelectionCacheSink = sess.ToolSelection()
			}
		})
	}
}

func BenchmarkRAGCacheSnapshot(b *testing.B) {
	for _, count := range []int{0, 20, 100, 500} {
		tokens := make(map[string]float64, count)
		for i := range count {
			tokens[fmt.Sprintf("token_%04d", i)] = 1
		}

		sess := &Session{}
		sess.SetRAGCache(&RAGCache{
			QueryTokens: tokens,
			At:          time.Now(),
			ChatKey:     "group:benchmark",
			Text:        strings.Repeat("历史消息检索缓存正文。", 20),
		})
		b.Run(fmt.Sprintf("query_tokens_%d", count), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				benchmarkRAGCacheSink = sess.RAGCacheSnapshot()
			}
		})
	}
}

func BenchmarkSessionManagerGetOrCreate(b *testing.B) {
	b.Run("same_session_cache_hit", func(b *testing.B) {
		manager := NewSessionManager(1024, 20, 0, nil)
		benchmarkSessionSink = manager.GetOrCreate("platform:chat:user", "user", "chat")
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			benchmarkSessionSink = manager.GetOrCreate("platform:chat:user", "user", "chat")
		}
	})

	b.Run("parallel_distinct_session_cache_hits", func(b *testing.B) {
		const sessionCount = 256
		manager := NewSessionManager(sessionCount*2, 20, 0, nil)
		for i := range sessionCount {
			manager.GetOrCreate(fmt.Sprintf("parallel:%d", i), "user", "chat")
		}

		var nextWorker atomic.Uint64
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			worker := nextWorker.Add(1) - 1
			sessionID := fmt.Sprintf("parallel:%d", worker%sessionCount)
			for pb.Next() {
				_ = manager.GetOrCreate(sessionID, "user", "chat")
			}
		})
	})
}
