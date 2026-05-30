package plugin

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// GoroutineInfo 描述一个受框架管理的后台 goroutine 的运行时信息。
type GoroutineInfo struct {
	// Name goroutine 的名称（通过 SpawnNamed 设置）；匿名 goroutine 为空字符串。
	Name string
	// Plugin 所属插件名称
	Plugin string
	// StartTime goroutine 启动时间
	StartTime time.Time
	// Uptime 运行时长（从 StartTime 到查询时的时长）
	// 由 ListAllGoroutines / ListPluginGoroutines 填充；零值表示未填充。
	Uptime time.Duration
	// IsAlive 标识该 goroutine 是否仍在运行。
	// listGoroutines 只返回 IsAlive=true 的条目；
	// goroutine 函数返回后框架自动将其置为 false，防止历史条目积累。
	IsAlive bool
}

// goroutineManager 管理插件生命周期绑定的后台 goroutine。
//
// 通过 [SetupContext.Spawn] / [SetupContext.SpawnNamed] 启动的 goroutine 会被自动纳管：
//   - 框架在 Teardown 前调用 cancel()，通知所有 goroutine 退出；
//   - cancel 后调用 Wait() 等待所有 goroutine 退出，再执行 TeardownFunc，
//     保证清理顺序：goroutine停止 → Teardown。
type goroutineManager struct {
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	mu         sync.Mutex
	goroutines []GoroutineInfo
	pluginName string
}

func newGoroutineManager() *goroutineManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &goroutineManager{ctx: ctx, cancel: cancel}
}

func newGoroutineManagerForPlugin(pluginName string) *goroutineManager {
	gm := newGoroutineManager()
	gm.pluginName = pluginName
	return gm
}

// go_ 启动一个与插件生命周期绑定的匿名 goroutine。
// fn 接收一个 context.Context，当插件即将 Teardown 时该 ctx 会被 cancel。
// 插件的 goroutine 应在 select 中监听 ctx.Done() 以响应退出信号。
func (gm *goroutineManager) go_(fn func(ctx context.Context)) {
	gm.goNamed_("", fn)
}

// goNamed_ 启动一个命名的生命周期绑定 goroutine。
// goroutine 退出时自动将对应条目的 IsAlive 置为 false，
// 防止历史已退出条目在 listGoroutines 中长期积累（内存泄漏）。
func (gm *goroutineManager) goNamed_(name string, fn func(ctx context.Context)) {
	gm.mu.Lock()
	idx := len(gm.goroutines)
	gm.goroutines = append(gm.goroutines, GoroutineInfo{
		Name:      name,
		Plugin:    gm.pluginName,
		StartTime: time.Now(),
		IsAlive:   true,
	})
	gm.mu.Unlock()

	gm.wg.Go(func() {
		defer func() {
			if r := recover(); r != nil {
				logger.WithField("plugin", gm.pluginName).WithField("goroutine", name).
					Errorf("[goroutineManager] Spawn goroutine panicked: %v", r)
			}
		}()
		defer func() {
			gm.mu.Lock()
			gm.goroutines[idx].IsAlive = false
			gm.mu.Unlock()
		}()
		fn(gm.ctx)
	})
}

// listGoroutines 返回当前所有仍在运行的受管 goroutine 快照（线程安全）。
// 只返回 IsAlive=true 的条目，已退出的 goroutine 不会出现在结果中。
func (gm *goroutineManager) listGoroutines() []GoroutineInfo {
	gm.mu.Lock()
	defer gm.mu.Unlock()
	var result []GoroutineInfo
	for _, g := range gm.goroutines {
		if g.IsAlive {
			result = append(result, g)
		}
	}
	if result == nil {
		result = []GoroutineInfo{}
	}
	return result
}

// stopAndWait 取消所有 goroutine 的 context 并等待它们全部退出。
// 由框架在调用 TeardownFunc 之前调用。
func (gm *goroutineManager) stopAndWait() {
	gm.cancel()
	gm.wg.Wait()
}

// taskGroup 受生命周期管理的并发任务组。
// 用于短生命周期并发任务（如并发网络请求后聚合结果），
// 区别于生命周期绑定的长驻 goroutine。
type taskGroup struct {
	wg   sync.WaitGroup
	mu   sync.Mutex
	errs []error
	ctx  context.Context
	gm   *goroutineManager
}

func (gm *goroutineManager) newTaskGroup() *taskGroup {
	return &taskGroup{ctx: gm.ctx, gm: gm}
}

func (g *taskGroup) goTask(fn func(ctx context.Context) error) {
	g.wg.Add(1)
	g.gm.go_(func(runCtx context.Context) {
		defer g.wg.Done()
		if err := fn(runCtx); err != nil {
			g.mu.Lock()
			g.errs = append(g.errs, err)
			g.mu.Unlock()
		}
	})
}

func (g *taskGroup) wait() error {
	g.wg.Wait()
	return errors.Join(g.errs...)
}
