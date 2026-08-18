package engine

// engine_query.go — 只读查询、统计、Snapshot/Restore、指标收集器
//
// 本文件包含所有不修改 Engine 状态的查询操作：
//   - GetMatcherCount / GetTempMatcherCount / GetMaxMatchers / GetMatcherStats
//   - SetMetricsCollector / GetMetricsCollector
//   - Snapshot / Restore
//   - MatcherStats 类型定义

import (
	"sort"
	"strings"

	"github.com/KomeiDiSanXian/remilia/infra/metrics"
)

// MatcherStats 匹配器统计
type MatcherStats struct {
	Total         int
	Global        int
	ByPlugin      map[string]int
	GlobalEnabled bool
}

// MatcherGroupInfo 是匹配器分组的只读快照。
type MatcherGroupInfo struct {
	Name    string `json:"name"`
	Count   int    `json:"count"`
	Enabled bool   `json:"enabled"` // 组内所有匹配器均启用时为 true
}

// ---- 计数与统计 --------------------------------------------------------------

// GetMatcherCount 获取当前已注册的匹配器数量（COW 无锁读取）
func (e *Engine) GetMatcherCount() int {
	return len(e.state.Load().matchers)
}

// GetTempMatcherCount 获取当前已注册的临时匹配器数量
func (e *Engine) GetTempMatcherCount() int {
	return e.internals.tempManager.Count()
}

// GetMaxMatchers 获取当前的匹配器数量上限（COW 无锁读取）
func (e *Engine) GetMaxMatchers() int {
	return e.state.Load().maxMatchers
}

// GetMatcherStats 获取匹配器统计信息（COW 无锁读取）
func (e *Engine) GetMatcherStats() MatcherStats {
	state := e.state.Load()
	stats := MatcherStats{ByPlugin: make(map[string]int)}
	stats.Total = len(state.matchers)

	for _, m := range state.matchers {
		if m.Source == "global" || m.Source == "" {
			stats.Global++
			continue
		}
		if after, ok := strings.CutPrefix(m.Source, "plugin:"); ok {
			stats.ByPlugin[after]++
		}
	}

	stats.GlobalEnabled = !state.block
	return stats
}

// ListGroups 返回所有匹配器分组的只读快照，按名称排序。
func (e *Engine) ListGroups() []MatcherGroupInfo {
	state := e.state.Load()
	groups := make([]MatcherGroupInfo, 0, len(state.groupIndex))
	for name, ms := range state.groupIndex {
		enabled := true
		for _, m := range ms {
			if m != nil && m.IsDisabled() {
				enabled = false
				break
			}
		}
		groups = append(groups, MatcherGroupInfo{
			Name:    name,
			Count:   len(ms),
			Enabled: enabled,
		})
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].Name < groups[j].Name })
	return groups
}

// ---- 指标收集器 --------------------------------------------------------------

// SetMetricsCollector 设置指标收集器
func (e *Engine) SetMetricsCollector(mc *metrics.Collector) *Engine {
	e.internals.metricsCollector.Store(mc)
	return e
}

// GetMetricsCollector 获取指标收集器
func (e *Engine) GetMetricsCollector() *metrics.Collector {
	return e.internals.metricsCollector.Load()
}

// ---- Snapshot / Restore -------------------------------------------------------

// Snapshot 表示引擎状态的不透明快照
type Snapshot struct {
	data *engineSnapshot
}

// engineSnapshot 存储引擎状态快照的内部数据
type engineSnapshot struct {
	state      *state
	middleware *middlewareState
}

// Snapshot 创建当前引擎状态的快照
func (e *Engine) Snapshot() Snapshot {
	return Snapshot{
		data: &engineSnapshot{
			state:      e.state.Load(),
			middleware: e.middleware.Load(),
		},
	}
}

// Restore 从快照恢复引擎状态
func (e *Engine) Restore(s Snapshot) {
	if s.data == nil {
		return
	}
	e.writeMu.Lock()
	defer e.writeMu.Unlock()
	e.state.Store(s.data.state)
	e.middleware.Store(s.data.middleware)
}
