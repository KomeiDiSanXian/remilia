package plugin

import (
	"context"
	"sync"
)

// goroutineManager 管理插件生命周期绑定的后台 goroutine。
//
// 通过 [SetupContext.Go] 启动的 goroutine 会被自动纳管：
//   - 框架在 Teardown 前调用 cancel()，通知所有 goroutine 退出；
//   - cancel 后调用 Wait() 等待所有 goroutine 退出，再执行 TeardownFunc，
//     保证清理顺序：goroutine停止 → Teardown。
type goroutineManager struct {
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func newGoroutineManager() *goroutineManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &goroutineManager{ctx: ctx, cancel: cancel}
}

// go_ 启动一个与插件生命周期绑定的 goroutine。
// fn 接收一个 context.Context，当插件即将 Teardown 时该 ctx 会被 cancel。
// 插件的 goroutine 应在 select 中监听 ctx.Done() 以响应退出信号。
func (gm *goroutineManager) go_(fn func(ctx context.Context)) {
	gm.wg.Go(func() {
		fn(gm.ctx)
	})
}

// stopAndWait 取消所有 goroutine 的 context 并等待它们全部退出。
// 由框架在调用 TeardownFunc 之前调用。
func (gm *goroutineManager) stopAndWait() {
	gm.cancel()
	gm.wg.Wait()
}
