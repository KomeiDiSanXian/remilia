package plugin

import (
	"sync"
	"time"
)

// manager_stats.go — 插件运行时统计控制器

// GoroutineSummary 汇总所有插件 goroutine 的统计信息。
type GoroutineSummary struct {
	// Total 受管 goroutine 总数
	Total int
	// ByPlugin 各插件的 goroutine 数量
	ByPlugin map[string]int
}

// statsController 管理插件运行时统计和蓝绿重载的 draining 追踪。
type statsController struct {
	pm                *Manager
	mu                sync.RWMutex // draining 专用锁，与 Manager.mu 分离
	drainingInstances map[string]*drainingEntry
}

func newStatsController(pm *Manager) *statsController {
	return &statsController{
		pm:                pm,
		drainingInstances: make(map[string]*drainingEntry),
	}
}

// Snapshot 返回插件管理器的运行时统计快照（用于监控/调试）。
func (sc *statsController) Snapshot() ManagerStats {
	sc.pm.mu.RLock()
	stateCount := make(map[string]int)
	for _, inst := range sc.pm.plugins {
		s := inst.GetState().String()
		stateCount[s]++
	}
	loadOrder := make([]string, len(sc.pm.loadOrder))
	copy(loadOrder, sc.pm.loadOrder)
	strictDeps := sc.pm.config.strictDeps
	ebStats := sc.pm.eventBus.GetStats()
	sc.mu.RLock()
	drainingCount := len(sc.drainingInstances)
	sc.mu.RUnlock()
	totalPlugins := len(sc.pm.plugins)
	sc.pm.mu.RUnlock()

	containerSvcCount := 0
	if c := sc.pm.container; c != nil {
		c.services.Range(func(_, _ any) bool {
			containerSvcCount++
			return true
		})
	}

	return ManagerStats{
		PluginsTotal:      totalPlugins,
		PluginsByState:    stateCount,
		LoadOrder:         loadOrder,
		GoroutineSummary:  sc.GoroutineSummary(),
		EventBusStats:     ebStats,
		DrainingCount:     drainingCount,
		ContainerFrozen:   sc.pm.container != nil && sc.pm.container.frozen.Load(),
		ContainerServices: containerSvcCount,
		StrictDeps:        strictDeps,
		Uptime:            time.Since(startTime).Round(time.Second).String(),
	}
}

// ListGoroutines 返回所有插件的受管后台 goroutine 全局聚合视图。
func (sc *statsController) ListGoroutines() []GoroutineInfo {
	now := time.Now()

	sc.pm.mu.RLock()
	instances := make([]*Instance, 0, len(sc.pm.plugins))
	for _, inst := range sc.pm.plugins {
		instances = append(instances, inst)
	}
	sc.pm.mu.RUnlock()

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

// GoroutineSummary 返回所有插件 goroutine 的聚合统计。
func (sc *statsController) GoroutineSummary() GoroutineSummary {
	all := sc.ListGoroutines()
	summary := GoroutineSummary{
		Total:    len(all),
		ByPlugin: make(map[string]int),
	}
	for _, g := range all {
		summary.ByPlugin[g.Plugin]++
	}
	return summary
}

// --- Draining 追踪 ---

// trackDraining 记录蓝绿重载中正在清理的旧实例。
func (sc *statsController) trackDraining(name string, inst *Instance) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.drainingInstances[name] = &drainingEntry{
		inst:      inst,
		startedAt: time.Now(),
	}
}

// markDrainingDone 标记 draining 实例清理完成（或失败）。
func (sc *statsController) markDrainingDone(name string, err error) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if e, ok := sc.drainingInstances[name]; ok {
		e.done = true
		e.err = err
	}
}

// drainDrainingInstances 等待所有 draining 实例清理完成（StopAll 时调用）。
func (sc *statsController) drainDrainingInstances() {
	sc.mu.RLock()
	names := make([]string, 0, len(sc.drainingInstances))
	for name, e := range sc.drainingInstances {
		if !e.done {
			names = append(names, name)
		}
	}
	sc.mu.RUnlock()
	if len(names) == 0 {
		return
	}
	done := make(chan struct{}, len(names))
	for _, name := range names {
		go func(n string) {
			for {
				sc.mu.RLock()
				e, ok := sc.drainingInstances[n]
				sc.mu.RUnlock()
				if !ok || e.done {
					done <- struct{}{}
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
		}(name)
	}
	for range names {
		<-done
	}
}

// ListDraining 返回所有正在清理或已完成的旧实例状态。
func (sc *statsController) ListDraining() map[string]*DrainingInfo {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	result := make(map[string]*DrainingInfo, len(sc.drainingInstances))
	for name, e := range sc.drainingInstances {
		info := &DrainingInfo{
			Name:      name,
			StartedAt: e.startedAt,
			Done:      e.done,
		}
		if e.err != nil {
			info.Err = e.err.Error()
		}
		result[name] = info
	}
	return result
}
