package plugin

import (
	"context"
	"sync"
	"time"
)

// GoroutineInfo 描述一个受框架管理的后台 goroutine 的运行时信息。
type GoroutineInfo struct {
	// Name goroutine 的名称（通过 GoNamed 设置）；匿名 goroutine 为空字符串。
	Name string
	// Plugin 所属插件名称
	Plugin string
	// StartTime goroutine 启动时间
	StartTime time.Time
}

// goroutineManager 管理插件生命周期绑定的后台 goroutine。
//
// 通过 [SetupContext.Go] / [SetupContext.GoNamed] 启动的 goroutine 会被自动纳管：
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
func (gm *goroutineManager) goNamed_(name string, fn func(ctx context.Context)) {
	gm.mu.Lock()
	gm.goroutines = append(gm.goroutines, GoroutineInfo{
		Name:      name,
		Plugin:    gm.pluginName,
		StartTime: time.Now(),
	})
	gm.mu.Unlock()

	gm.wg.Go(func() {
		fn(gm.ctx)
	})
}

// listGoroutines 返回当前所有受管 goroutine 的快照（线程安全）。
func (gm *goroutineManager) listGoroutines() []GoroutineInfo {
	gm.mu.Lock()
	defer gm.mu.Unlock()
	result := make([]GoroutineInfo, len(gm.goroutines))
	copy(result, gm.goroutines)
	return result
}

// stopAndWait 取消所有 goroutine 的 context 并等待它们全部退出。
// 由框架在调用 TeardownFunc 之前调用。
func (gm *goroutineManager) stopAndWait() {
	gm.cancel()
	gm.wg.Wait()
}
