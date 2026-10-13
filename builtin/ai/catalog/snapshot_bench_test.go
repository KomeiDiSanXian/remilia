package catalog

import (
	"fmt"
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
)

var (
	benchmarkSnapshotSink CatalogSnapshot
	benchmarkActionsSink  []toolkit.Action
	benchmarkActionSink   toolkit.Action
	benchmarkLookupSink   bool
	benchmarkNamesSink    []string
)

func benchmarkCatalogActions(count int) []toolkit.Action {
	actions := make([]toolkit.Action, count)
	for i := range actions {
		actions[i] = toolkit.ActionOf(toolkit.Tool{
			Name:        fmt.Sprintf("tool_%04d", i),
			Description: "查询城市天气与预报，并支持按日期和温度范围筛选。",
			Categories:  []string{"weather"},
			Permissions: []string{"weather.read"},
			Parameters: protocol.ToolParamSchema{
				Type: "object",
				Properties: map[string]protocol.ToolParamSchema{
					"city": {Type: "string", Description: "城市名称"},
					"date": {Type: "string", Description: "查询日期"},
					"filter": {
						Type: "object",
						Properties: map[string]protocol.ToolParamSchema{
							"min_temperature": {Type: "number"},
							"max_temperature": {Type: "number"},
						},
					},
				},
				Required: []string{"city"},
			},
		})
	}
	return actions
}

func benchmarkCatalogRegistry(count int) *toolkit.ToolRegistry {
	registry := toolkit.NewToolRegistry()
	for _, action := range benchmarkCatalogActions(count) {
		registry.Register(toolkit.Tool{
			Name:        action.Spec.Name,
			Description: action.Spec.Description,
			Categories:  action.Spec.Categories,
			Permissions: action.Policy.Permissions,
			Parameters:  action.Spec.Parameters,
		})
	}
	return registry
}

// BenchmarkCatalogCandidateSnapshot 覆盖每轮候选动作装配中的三次防御性拷贝：
// 注册表导出动作、创建目录快照、从快照导出本轮候选。
func BenchmarkCatalogCandidateSnapshot(b *testing.B) {
	for _, count := range []int{20, 100, 500} {
		registry := benchmarkCatalogRegistry(count)
		b.Run(fmt.Sprintf("tools_%d", count), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				benchmarkSnapshotSink = NewSnapshot(1, registry.Actions())
				benchmarkActionsSink = benchmarkSnapshotSink.Actions()
			}
		})
	}
}

func BenchmarkCatalogSnapshot(b *testing.B) {
	for _, count := range []int{20, 100, 500} {
		actions := benchmarkCatalogActions(count)
		snapshot := NewSnapshot(1, actions)

		b.Run(fmt.Sprintf("new_snapshot/tools_%d", count), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				benchmarkSnapshotSink = NewSnapshot(1, actions)
			}
		})

		b.Run(fmt.Sprintf("actions/tools_%d", count), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				benchmarkActionsSink = snapshot.Actions()
			}
		})

		b.Run(fmt.Sprintf("sorted_names/tools_%d", count), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				benchmarkNamesSink = snapshot.SortedNames()
			}
		})

		for _, target := range []struct {
			name  string
			index int
		}{
			{name: "first", index: 0},
			{name: "middle", index: count / 2},
			{name: "last", index: count - 1},
			{name: "miss", index: -1},
		} {
			name := "missing_tool"
			if target.index >= 0 {
				name = actions[target.index].Spec.Name
			}
			b.Run(fmt.Sprintf("lookup_%s/tools_%d", target.name, count), func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					benchmarkActionSink, benchmarkLookupSink = snapshot.Lookup(name)
				}
			})
		}
	}
}
