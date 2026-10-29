package promptctx

import (
	"fmt"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/messagelog"
)

var benchmarkRAGContextSink string

func benchmarkRAGEntries(count int) []messagelog.RecordEntry {
	entries := make([]messagelog.RecordEntry, count)
	baseTime := time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)
	for i := range entries {
		content := fmt.Sprintf("关于代码 review、任务 scheduler 与插件扩展的讨论，记录编号 %04d。", i)
		if i%4 == 0 {
			content = fmt.Sprintf("Tokyo weather forecast：第 %04d 天预计晴朗，适合安排户外活动。", i)
		}
		entries[i] = messagelog.RecordEntry{
			Platform:  "qq",
			EventID:   fmt.Sprintf("event_%04d", i),
			UserID:    fmt.Sprintf("user_%02d", i%20),
			UserName:  fmt.Sprintf("用户%02d", i%20),
			Content:   content,
			Timestamp: baseTime.Add(time.Duration(i) * time.Minute),
		}
	}
	return entries
}

func BenchmarkFormatRAGHits(b *testing.B) {
	const query = "Tokyo weather forecast 明天的天气"
	for _, count := range []int{20, 100, 500} {
		entries := benchmarkRAGEntries(count)
		b.Run(fmt.Sprintf("history_%d", count), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				benchmarkRAGContextSink = formatRAGHits(RAGEnv{}, entries, query, 8)
			}
		})
	}
}
