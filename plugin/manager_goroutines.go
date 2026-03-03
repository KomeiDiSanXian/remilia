package plugin

// manager_goroutines.go — 插件 goroutine 管理与统计

import "time"

// GoroutineSummary 汇总所有插件 goroutine 的统计信息。
type GoroutineSummary struct {
	// Total 受管 goroutine 总数
	Total int
	// ByPlugin 各插件的 goroutine 数量
	ByPlugin map[string]int
}

// ListPluginGoroutines 返回所有插件的受管后台 goroutine 信息快照。
//
// 仅包含通过 ctx.Go / ctx.GoNamed 启动的 goroutine，不包含系统级 goroutine。
// 每个条目的 Uptime 字段自动填充（查询时间 - StartTime）。
//
// Deprecated: 使用 ListAllGoroutines 替代（语义更清晰，两者功能相同）。
func (pm *Manager) ListPluginGoroutines() []GoroutineInfo {
	return pm.ListAllGoroutines()
}

// ListAllGoroutines 返回所有插件的受管后台 goroutine 全局聚合视图。
//
// 使用场景：
//   - 调试：检查所有后台任务是否正常运行
//   - 监控：接入指标系统，统计 goroutine 总数和各插件占比
//   - 泄漏排查：对比 StartAll 前后的 goroutine 列表
//
// 返回值：
//   - 按插件名排序的 GoroutineInfo 切片
//   - 每条记录包含：Name、Plugin、StartTime、Uptime（查询时自动填充）
//   - 若所有插件均无后台 goroutine，返回空切片（非 nil）
//
// 线程安全：内部使用 RLock，可并发调用。
func (pm *Manager) ListAllGoroutines() []GoroutineInfo {
	now := time.Now()

	pm.mu.RLock()
	instances := make([]*PluginInstance, 0, len(pm.plugins))
	for _, inst := range pm.plugins {
		instances = append(instances, inst)
	}
	pm.mu.RUnlock()

	result := make([]GoroutineInfo, 0)
	for _, inst := range instances {
		inst.mu.RLock()
		gm := inst.goroutineMgr
		inst.mu.RUnlock()
		if gm == nil {
			continue
		}
		for _, g := range gm.listGoroutines() {
			g.Uptime = now.Sub(g.StartTime)
			result = append(result, g)
		}
	}
	return result
}

// GoroutineSummary 返回所有插件 goroutine 的聚合统计（不返回详细列表，开销更低）。
func (pm *Manager) GoroutineSummary() GoroutineSummary {
	all := pm.ListAllGoroutines()
	summary := GoroutineSummary{
		Total:    len(all),
		ByPlugin: make(map[string]int),
	}
	for _, g := range all {
		summary.ByPlugin[g.Plugin]++
	}
	return summary
}
