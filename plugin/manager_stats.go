package plugin

import (
	"context"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
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
	pm     *Manager
	mu     sync.RWMutex // draining 专用锁，与 Manager.mu 分离
	nextID uint64
	// drainingInstances 以「每次追踪的令牌」为 key（不是插件名）：同一插件
	// 连续两次蓝绿重载各自拥有独立条目，第一次的清理回调不会把第二次标成完成。
	drainingInstances map[uint64]*drainingEntry
}

func newStatsController(pm *Manager) *statsController {
	return &statsController{
		pm:                pm,
		drainingInstances: make(map[uint64]*drainingEntry),
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
		Uptime:            time.Since(sc.pm.createdAt).Round(time.Second).String(),
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

// trackDraining 记录一次蓝绿重载中正在清理的旧实例，返回该次追踪的令牌。
//
// 以令牌为 key（而非插件名）：同一插件连续两次蓝绿重载各自独立，
// 第一次的清理回调不会把第二次误标为完成——那会让 StopAll 不再等待第二次
// 的旧实例 Teardown。
func (sc *statsController) trackDraining(name string) uint64 {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.nextID++
	token := sc.nextID
	sc.drainingInstances[token] = &drainingEntry{
		name:      name,
		startedAt: time.Now(),
	}
	return token
}

// markDrainingDone 标记某一次 draining 清理完成（或失败）。
func (sc *statsController) markDrainingDone(token uint64, err error) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if e, ok := sc.drainingInstances[token]; ok {
		e.done = true
		e.err = err
	}
}

// drainDrainingInstances 等待所有 draining 实例清理完成（StopAll 时调用）。
// ctx 到期时提前返回，避免单个卡死的蓝绿 Teardown 无限拖住 StopAll。
func (sc *statsController) drainDrainingInstances(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	sc.mu.RLock()
	tokens := make([]uint64, 0, len(sc.drainingInstances))
	for token, e := range sc.drainingInstances {
		if !e.done {
			tokens = append(tokens, token)
		}
	}
	sc.mu.RUnlock()
	if len(tokens) == 0 {
		return
	}
	done := make(chan struct{}, len(tokens))
	for _, token := range tokens {
		go func(tok uint64) {
			for {
				// e.done 必须在锁内读取：markDrainingDone 在锁内写该字段，
				// 锁外读取与 BG teardown 并发构成数据竞争（CI race 检出）。
				sc.mu.RLock()
				e, ok := sc.drainingInstances[tok]
				finished := ok && e.done
				sc.mu.RUnlock()
				if finished {
					done <- struct{}{}
					return
				}
				select {
				case <-ctx.Done():
					done <- struct{}{}
					return
				case <-time.After(10 * time.Millisecond):
				}
			}
		}(token)
	}
	for range tokens {
		select {
		case <-done:
		case <-ctx.Done():
			logger.Warn("[PluginManager] StopAll: context expired while waiting for draining instances")
			return
		}
	}
}

// ListDraining 返回正在清理或已完成的旧实例状态（按插件名聚合）。
//
// 同名插件可能有多条追踪（连续重载）：Done 表示该插件的全部清理都已完成，
// Err 取第一个失败原因，StartedAt 取最早一次的开始时间。
func (sc *statsController) ListDraining() map[string]*DrainingInfo {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	result := make(map[string]*DrainingInfo, len(sc.drainingInstances))
	for _, e := range sc.drainingInstances {
		info, ok := result[e.name]
		if !ok {
			info = &DrainingInfo{Name: e.name, StartedAt: e.startedAt, Done: true}
			result[e.name] = info
		}
		if e.startedAt.Before(info.StartedAt) {
			info.StartedAt = e.startedAt
		}
		if !e.done {
			info.Done = false
		}
		if e.err != nil && info.Err == "" {
			info.Err = e.err.Error()
		}
	}
	return result
}
