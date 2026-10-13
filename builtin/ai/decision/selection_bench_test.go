package decision

import (
	"fmt"
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
)

var (
	benchmarkSelectedToolsSink []toolkit.Action
	benchmarkUsedToolsSink     map[string]bool
)

func benchmarkDecisionActions(count int) []toolkit.Action {
	actions := make([]toolkit.Action, count)
	for i := range actions {
		actions[i] = toolkit.ActionOf(toolkit.Tool{
			Name:        fmt.Sprintf("tool_%04d", i),
			Description: fmt.Sprintf("查询 weather forecast 和 location information，候选编号 %d。", i),
			Categories:  []string{"weather"},
		})
	}
	return actions
}

func benchmarkDecisionSession(messageCount int) *session.Session {
	messages := make([]protocol.Message, messageCount)
	for i := range messages {
		messages[i] = protocol.Message{Role: protocol.RoleAssistant}
		if i%4 == 0 {
			messages[i].ToolCalls = []protocol.ToolCall{{Name: fmt.Sprintf("tool_%04d", i)}}
		}
	}
	return &session.Session{Messages: messages}
}

func BenchmarkSelectToolsForTurn(b *testing.B) {
	const query = "查询 weather forecast for tokyo tomorrow"
	opts := SelectionOptions{Max: DefaultSelectMax, Budget: DefaultToolBudget, CatalogGeneration: 1}

	for _, count := range []int{20, 100, 500} {
		actions := benchmarkDecisionActions(count)
		if count <= opts.Max {
			b.Run(fmt.Sprintf("tools_%d_fast_path", count), func(b *testing.B) {
				sess := benchmarkDecisionSession(20)
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					benchmarkSelectedToolsSink = SelectToolsForTurn(opts, nil, nil, sess, query, actions, nil)
				}
			})
			continue
		}

		b.Run(fmt.Sprintf("tools_%d_cold_selection", count), func(b *testing.B) {
			sess := benchmarkDecisionSession(20)
			current := opts
			current.CatalogGeneration = 1
			generation := current.CatalogGeneration
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				current.CatalogGeneration = generation
				benchmarkSelectedToolsSink = SelectToolsForTurn(current, nil, nil, sess, query, actions, nil)
				generation++
			}
		})

		b.Run(fmt.Sprintf("tools_%d_cache_hit", count), func(b *testing.B) {
			sess := benchmarkDecisionSession(20)
			current := opts
			current.CatalogGeneration = 1
			benchmarkSelectedToolsSink = SelectToolsForTurn(current, nil, nil, sess, query, actions, nil)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				benchmarkSelectedToolsSink = SelectToolsForTurn(current, nil, nil, sess, query, actions, nil)
			}
		})
	}
}

func BenchmarkSessionUsedTools(b *testing.B) {
	for _, count := range []int{0, 20, 100, 500} {
		sess := benchmarkDecisionSession(count)
		b.Run(fmt.Sprintf("messages_%d", count), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				benchmarkUsedToolsSink = SessionUsedTools(sess)
			}
		})
	}
}
