package promptctx

import (
	"fmt"
	"strings"
	"testing"
)

var benchmarkPromptContextSink string

func benchmarkPromptSource(header string, count, width int, keepTail bool) Source {
	rows := make([]string, count)
	filler := strings.Repeat("任务上下文 weather scheduler 记录内容 ", max(1, width/48))
	for i := range rows {
		rows[i] = fmt.Sprintf("%s #%03d %s", header, i, filler)
	}
	return Source{
		Header:   header,
		Limit:    count,
		Shrink:   true,
		KeepTail: keepTail,
		Body: func(limit int) string {
			if limit < 0 {
				limit = 0
			}
			if limit > len(rows) {
				limit = len(rows)
			}
			return strings.Join(rows[:limit], "\n")
		},
	}
}

func benchmarkPromptSources() []Source {
	return []Source{
		benchmarkPromptSource("运行时上下文", 16, 80, false),
		benchmarkPromptSource("群聊最近消息", 120, 96, true),
		benchmarkPromptSource("长期记忆", 24, 96, false),
		benchmarkPromptSource("相关历史消息", 60, 96, false),
	}
}

func BenchmarkBuildDynamicContext(b *testing.B) {
	sources := benchmarkPromptSources()
	staticPrompt := strings.Repeat("稳定系统提示词与用户指令。", 40)

	b.Run("unbudgeted", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			benchmarkPromptContextSink = Build(sources)
		}
	})

	b.Run("budgeted_full_fit", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			benchmarkPromptContextSink = BuildBudgeted(sources, 16384, staticPrompt)
		}
	})

	b.Run("budgeted_shrink", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			benchmarkPromptContextSink = BuildBudgeted(sources, 4096, staticPrompt)
		}
	})

	b.Run("budgeted_cached_body_truncate", func(b *testing.B) {
		cachedBody := strings.Repeat("历史消息缓存命中，保留近期内容。\n", 500)
		cachedSource := []Source{{
			Header: "相关历史消息",
			Limit:  500,
			Shrink: true,
			Body:   func(int) string { return cachedBody },
		}}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			benchmarkPromptContextSink = BuildBudgeted(cachedSource, 2048, staticPrompt)
		}
	})
}
