package platform

// registry.go — 多平台适配器注册表
//
// Registry 支持同时管理多个平台适配器的生命周期：
//   - [Registry.Register] / [Registry.Remove] / [Registry.Replace] — 注册管理
//   - [Registry.StartAll] / [Registry.StopAll]                     — 并发启动/停止
//   - [Registry.SenderFor] / [Registry.CapabilitiesFor]            — 按平台查询

import (
	stdctx "context"
	"errors"
	"fmt"
	"sync"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// Registry 多平台适配器注册表。
//
// 支持同时运行多个平台适配器，框架通过 Registry 管理它们的生命周期。
type Registry struct {
	mu       sync.RWMutex
	adapters map[string]Adapter
	observer AdapterObserver // 可选，nil = 无操作

	// fatalCh 用于向调用方实时推送适配器的致命错误（见 FatalErrors）。
	// 有缓冲且非阻塞写入：没有消费者时直接丢弃，绝不拖慢适配器 goroutine。
	fatalCh chan error

	// disconnectUnregs 保存每个平台 RecoverableAdapter 的断连回调注销函数。
	// StartAll 每次启动前先调用旧的注销函数，再注册新的，防止多次调用时回调累积。
	// StopAll 完成后统一清理，释放对 Registry 的引用，避免 GC 泄漏。
	disconnectUnregs map[string]func()
}

// NewRegistry 创建空的适配器注册表
func NewRegistry() *Registry {
	return &Registry{
		adapters:         make(map[string]Adapter),
		disconnectUnregs: make(map[string]func()),
		fatalCh:          make(chan error, fatalErrBuffer),
	}
}

// WithObserver 注册适配器生命周期观察者，返回 *Registry 支持链式调用。
//
// 必须在 StartAll 之前调用；并发调用是线程安全的（使用写锁）。
// 传入 nil 表示清除当前观察者。
func (r *Registry) WithObserver(o AdapterObserver) *Registry {
	r.mu.Lock()
	r.observer = o
	r.mu.Unlock()
	return r
}

// fatalErrBuffer 是致命错误 channel 的缓冲长度。
const fatalErrBuffer = 8

// FatalErrors 返回适配器致命错误的实时通知 channel。
//
// StartAll 只在**所有**适配器退出后才返回，健康平台会一直阻塞到 ctx 取消，
// 因此单个平台的致命失败可能在数天内都无人知晓。订阅这个 channel 可以在
// 错误发生的当下就拿到它，用于告警、重启或降级：
//
//	go func() {
//	    for err := range reg.FatalErrors() {
//	        alert(err)
//	    }
//	}()
//	_ = reg.StartAll(ctx, handler)
//
// channel 有缓冲且写入是非阻塞的：没有消费者时错误会被直接丢弃，
// 不会拖慢适配器 goroutine。需要不丢事件的完整通知请改用 AdapterObserver。
//
// channel 由 Registry 持有，不会被关闭。
func (r *Registry) FatalErrors() <-chan error {
	if r.fatalCh == nil {
		// 零值 Registry（未经 NewRegistry 构造）：返回已关闭的 channel，
		// 使文档中的 for-range 用法立即结束而不是永久阻塞在 nil channel 上。
		closed := make(chan error)
		close(closed)
		return closed
	}
	return r.fatalCh
}

// publishFatal 以非阻塞方式推送一个致命错误。
func (r *Registry) publishFatal(err error) {
	if r.fatalCh == nil {
		return
	}
	select {
	case r.fatalCh <- err:
	default:
		// 无人消费或缓冲已满：丢弃。observer 与 StartAll 的返回值仍保留该错误。
	}
}

// notifyObserver 内部辅助函数，无锁调用 observer（调用方自行保证 observer 安全读取）。
func (r *Registry) notifyObserver(fn func(AdapterObserver)) {
	r.mu.RLock()
	o := r.observer
	r.mu.RUnlock()
	if o != nil {
		fn(o)
	}
}

// isFatalErr 判断适配器退出错误是否属于 fatal 错误（需要上报的真实故障）。
//
// context 取消/超时属于正常关闭路径，不视为 fatal error。
// 此辅助函数用于消除 StartAll 中两处相同的过滤逻辑（问题 2.3）。
func isFatalErr(err error) bool {
	return err != nil &&
		!errors.Is(err, stdctx.Canceled) &&
		!errors.Is(err, stdctx.DeadlineExceeded)
}

// Register 注册一个平台适配器
//
// 若同一平台已注册，会覆盖旧适配器。
func (r *Registry) Register(adapter Adapter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// 覆盖注册同样要释放旧适配器的断线回调，否则被顶掉的实例仍会以同一个
	// 平台名回调进 Registry，并被 disconnectUnregs 长期持有而无法回收。
	r.releaseDisconnectHookLocked(adapter.Platform())
	r.adapters[adapter.Platform()] = adapter
}

// Get 获取指定平台的适配器
func (r *Registry) Get(platform string) (Adapter, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.adapters[platform]
	return a, ok
}

// Remove 注销指定平台的适配器，返回 true 表示成功移除，false 表示不存在。
//
// 注意：仅从注册表中移除，不调用 Stop()；若适配器正在运行，
// 调用方应先调用 adapter.Stop() 再调用 Remove。
func (r *Registry) Remove(platform string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.adapters[platform]; !ok {
		return false
	}
	delete(r.adapters, platform)
	r.releaseDisconnectHookLocked(platform)
	return true
}

// releaseDisconnectHookLocked 注销 StartAll 为该平台注册的断线回调。
//
// 调用方必须持有 r.mu 写锁。
//
// 该回调闭包同时捕获了 Registry 与 Adapter，正是本结构体注释里要避免的引用环：
// 不注销的话，已被移除的适配器仍能回调进 Registry，为一个已经下线的平台刷出
// "waiting for recovery" 警告与断线指标；同时 disconnectUnregs 会一直持有该
// 适配器使其无法被 GC。此前只有 StopAll 会清理，Remove/Replace 均遗漏。
func (r *Registry) releaseDisconnectHookLocked(platform string) {
	if unreg, ok := r.disconnectUnregs[platform]; ok {
		if unreg != nil {
			unreg()
		}
		delete(r.disconnectUnregs, platform)
	}
}

// All 返回所有已注册适配器的快照（切片顺序不保证）
//
// 无论注册表是否为空，始终返回非 nil 切片，保持与 Go 惯例的一致性。
func (r *Registry) All() []Adapter {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Adapter, 0, len(r.adapters))
	for _, a := range r.adapters {
		out = append(out, a)
	}
	return out
}

// Len 返回已注册适配器数量，无需分配切片。
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.adapters)
}

// Replace 原子替换指定平台的适配器，返回被替换的旧适配器。
//
// 无论原有适配器是否存在，新适配器都会被注册。
// 若该平台此前无适配器，returned old 为 nil，replaced 为 false。
//
// 典型用法（热替换运行中的适配器）：
//
//	old, ok := registry.Replace(newAdapter)
//	if ok {
//	    _ = old.Stop(ctx)
//	}
func (r *Registry) Replace(adapter Adapter) (old Adapter, replaced bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	old, replaced = r.adapters[adapter.Platform()]
	// 旧适配器的断线回调必须随其一起退场，否则它会继续以新适配器的
	// 平台名回调进 Registry，并让旧实例常驻内存。
	r.releaseDisconnectHookLocked(adapter.Platform())
	r.adapters[adapter.Platform()] = adapter
	return old, replaced
}

// SenderFor 返回指定平台的消息发送器。
//
// 若平台未注册，返回 (nil, false)。
func (r *Registry) SenderFor(platform string) (Sender, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.adapters[platform]
	if !ok {
		return nil, false
	}
	return a.Sender(), true
}

// CapabilitiesFor 返回指定平台的能力声明。
//
// 若平台未注册，返回零值 Capabilities 和 false。
func (r *Registry) CapabilitiesFor(platform string) (Capabilities, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.adapters[platform]
	if !ok {
		return Capabilities{}, false
	}
	return a.Capabilities(), true
}

// StartAll 并发启动所有已注册平台适配器
//
// 每个适配器在独立 goroutine 中运行，ctx 取消时所有适配器退出。
// handler 会收到来自所有平台的事件。
//
// # 错误语义
//
// 单个适配器致命失败**不会**中止其余平台——这是刻意的：一个平台配置有误
// 不应让整个 Bot 起不来。但这也意味着返回值来得很晚：StartAll 只在所有
// 适配器都退出后才返回，健康的平台会一直阻塞到 ctx 取消，实际可能是几天。
//
// 因此致命错误在**发生的当下**就通过两条途径立即上报，不必等到返回：
//   - AdapterObserver.OnAdapterError（通过 WithObserver 注册）
//   - FatalErrors() 返回的错误 channel
//
// 返回值仍是所有致命错误的合并结果，供关心最终状态的调用方使用。
func (r *Registry) StartAll(ctx stdctx.Context, handler func(Event)) error {
	adapters := r.All()
	if len(adapters) == 0 {
		return fmt.Errorf("platform registry: no adapters registered")
	}

	var (
		mu   sync.Mutex
		errs []error
		wg   sync.WaitGroup
	)

	for _, a := range adapters {
		// 若适配器支持断连通知，注册框架侧的告警 hook。
		// 先注销旧的注册（防止多次调用 StartAll 时回调累积），再注册新的。
		if ra, ok := a.(RecoverableAdapter); ok {
			r.mu.Lock()
			if old, exists := r.disconnectUnregs[a.Platform()]; exists && old != nil {
				old() // 注销旧回调，释放对 Registry 的引用
			}
			r.mu.Unlock()

			unregister := ra.OnDisconnect(func(err error) {
				logger.WithFields(logger.Fields{
					"platform": a.Platform(),
				}).WithError(err).Warn("[Registry] Platform adapter disconnected, waiting for recovery")
				r.notifyObserver(func(o AdapterObserver) { o.OnAdapterDisconnect(a.Platform(), err) })
			})

			r.mu.Lock()
			r.disconnectUnregs[a.Platform()] = unregister
			r.mu.Unlock()
		}
		wg.Go(func() {
			r.notifyObserver(func(o AdapterObserver) { o.OnAdapterStarted(a.Platform()) })
			err := a.Start(ctx, handler)
			r.notifyObserver(func(o AdapterObserver) { o.OnAdapterStopped(a.Platform()) })
			if err != nil {
				logger.WithFields(logger.Fields{
					"platform": a.Platform(),
				}).WithError(err).Error("[Registry] Platform adapter exited with error")
				// 仅对 fatal error（非 ctx 取消/超时）通知 observer，并追加到错误列表。
				// isFatalErr 统一封装过滤逻辑，避免此处与 wg.Wait() 后两处重复判断。
				if isFatalErr(err) {
					r.notifyObserver(func(o AdapterObserver) { o.OnAdapterError(a.Platform(), err.Error()) })
					fatal := fmt.Errorf("platform %s: %w", a.Platform(), err)
					mu.Lock()
					errs = append(errs, fatal)
					mu.Unlock()
					// 立即推送到错误 channel，让调用方无需等到 StartAll 返回。
					r.publishFatal(fatal)
				}
			}
		})
	}

	wg.Wait()

	// errs 中只包含 fatal error（已在 goroutine 内过滤），直接合并返回。
	return errors.Join(errs...)
}

// StopAll 并发停止所有已注册平台适配器，合并全部错误后返回。
//
// 所有适配器同时发起停止，总耗时取决于最慢的那一个（而非各平台停止时间之和）。
// 停止完成后统一清理断连回调注销函数，释放对 Registry 的内部引用，避免 GC 泄漏。
func (r *Registry) StopAll(ctx stdctx.Context) error {
	adapters := r.All()
	var (
		mu   sync.Mutex
		errs []error
		wg   sync.WaitGroup
	)
	for _, a := range adapters {
		wg.Go(func() {
			if err := a.Stop(ctx); err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("platform %s stop: %w", a.Platform(), err))
				mu.Unlock()
			}
		})
	}
	wg.Wait()

	// 所有适配器已停止，注销断连回调，释放闭包对 Registry 的引用
	r.mu.Lock()
	for k, unreg := range r.disconnectUnregs {
		if unreg != nil {
			unreg()
		}
		delete(r.disconnectUnregs, k)
	}
	r.mu.Unlock()

	return errors.Join(errs...)
}
