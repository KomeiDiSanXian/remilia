package plugin

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// instance.go — Instance：运行时实例及公开 API
//
// 生命周期方法（load/unload）在此文件，
// reload/reloadBlueGreen 独立于 reload.go。

// Instance v2 插件实例
//
// 通过 [Manager.Get] 获取，可用于查询状态、元数据和已注册的 Matcher。
// 生命周期操作（加载/卸载/重载）由 Manager 通过内部 pluginInternal 接口驱动。
type Instance struct {
	desc         *Descriptor
	state        State
	setupContext *SetupContext
	matchers     []*engine.Matcher // 插件注册的匹配器
	loadTime     time.Time         // 加载时间
	lastError    error             // 最后的错误
	goroutineMgr *goroutineManager // 生命周期绑定 goroutine 管理器
	exportedAPI  any               // Setup 返回的 API 对象
	loadedVer    string            // 当前加载的版本号（用于迁移检测）
	mu           sync.RWMutex
	manager      *Manager // 所属插件管理器（用于蓝绿重载 draining 跟踪）

	// depsModified 标记 Register 是否通过 COW 合并了未声明依赖。
	// 供 RegisterMultiple 在事后修复 loadOrder 时快速跳过无需修正的插件。
	depsModified bool
}

// managerRef 返回所属 Manager（包内使用，供 reload/teardown 反向引用）。
func (pi *Instance) managerRef() *Manager {
	pi.mu.RLock()
	defer pi.mu.RUnlock()
	return pi.manager
}

// tryTransition 原子地尝试将状态从 from 切换到 to。
//
// 生命周期批处理（StartAll/StopAll）用它消除"状态检查与状态变更分离"的
// 竞态窗口：并发 StartAll×2 或 StartAll 恰逢 UnloadLoad 重载的 Unloaded
// 窗口时，两个 goroutine 可能同时通过检查并触发二次 Setup（matcher 翻倍）。
// 返回 false 表示当前状态不是 from，调用方应跳过。
func (pi *Instance) tryTransition(from, to State) bool {
	pi.mu.Lock()
	defer pi.mu.Unlock()
	if pi.state != from {
		return false
	}
	pi.state = to
	return true
}

// loadingWaitInterval 是等待在飞 Setup 结束的轮询间隔。
// 注销/重载是管理冷路径，10ms 级延迟可接受。
const loadingWaitInterval = 10 * time.Millisecond

// waitForLoading 等待实例离开 Loading 状态（Setup 在飞时阻塞到其结束）。
//
// 实例在 Setup 开始前就以 Loading 入表、Setup 在锁外执行（见 registerPreSetup
// 与 load）。若此时直接 unload/reload：
//   - 清理会先于 Setup 完成执行：rootScope 可能尚未创建（其订阅/钩子无人
//     Dispose）、Teardown 与在飞的 Setup 并发；
//   - 随后 Setup 成功收尾会把状态改回 Loaded、把 API 写回容器、Matcher 注册
//     进引擎——这些资源已无人管理（幽灵命令持续响应、容器残留已卸载插件）。
//
// 因此 Unregister/reload 先在此排队等待 Setup 结束，再走各自的正常流程。
// 返回错误：等待期间 ctx 到期（Setup 可能挂起，此时应使用 ForceUnregister
// 强制移除，其语义即"不等待清理"）。
func (pi *Instance) waitForLoading(ctx context.Context) error {
	for {
		pi.mu.RLock()
		loading := pi.state == Loading
		name := pi.desc.Name
		pi.mu.RUnlock()
		if !loading {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("plugin %q is still loading: %w", name, ctx.Err())
		case <-time.After(loadingWaitInterval):
		}
	}
}

// --- pluginInternal 实现（包私有，供 Manager 内部使用）---

// load 加载插件（实现 pluginInternal）
// ctx 用于控制超时：若 context 在 Setup 完成前到期则返回 ctx.Err()。
//
// 并发安全说明：Setup 在子 goroutine 中执行，其结果通过带缓冲 channel 传回，
// 主 goroutine 不与 Setup goroutine 共享可变变量（修复此前对命名返回值 loadErr 的数据竞争）。
// 若 ctx 先到期，Setup goroutine 仍会跑完，但其返回的 API 不会被导出、
// 其间 Spawn 的 goroutine 会在 Setup 结束后被统一停止，避免"僵尸 Setup"污染容器。
func (pi *Instance) load(ctx context.Context) error {
	pi.mu.Lock()
	pi.state = Loading
	gm := newGoroutineManagerForPlugin(pi.desc.Name)
	pi.goroutineMgr = gm
	setupCtx := pi.setupContext
	if setupCtx != nil {
		setupCtx.goroutineMgr = gm
	}
	pi.mu.Unlock()

	startTime := time.Now()

	type setupResult struct {
		api any
		err error
	}
	resCh := make(chan setupResult, 1)
	go func() {
		var res setupResult
		defer func() {
			if r := recover(); r != nil {
				if e, ok := r.(error); ok {
					res.err = fmt.Errorf("plugin %q: Setup panicked: %w", pi.desc.Name, e)
				} else {
					res.err = fmt.Errorf("plugin %q: Setup panicked: %v", pi.desc.Name, r)
				}
				res.api = nil
			}
			resCh <- res
		}()
		res.api, res.err = pi.desc.callSetup(setupCtx)
	}()

	var res setupResult
	select {
	case res = <-resCh:
	case <-ctx.Done():
		// Setup 仍在后台运行：标记失败，等它自然结束后回收其 goroutine。
		// 不导出其 API、不改写容器——调用方会按失败流程清理本实例。
		//
		// 收尾 goroutine 带超时：Setup 里 Spawn 的 goroutine 若不响应取消，
		// 无超时的 stopAndWait 会永久挂在这里（泄漏一个 goroutine）。
		pi.mu.Lock()
		pi.state = Error
		pi.lastError = ctx.Err()
		pi.goroutineMgr = nil
		pi.mu.Unlock()
		go func() {
			<-resCh
			// Setup 结束后再清理一次（幂等）：超时那一刻它可能还在注册订阅/
			// Matcher，只有等它真正结束才能保证这些资源被回收。
			pi.cleanupFailedLoad(pi.coordinatorOrNil(), gm)
		}()
		return ctx.Err()
	}

	if res.err != nil {
		// Setup 失败：回收其可能已创建的 Scope 资源与 goroutine（幂等清理，
		// 否则 rootScope 上的订阅/OnDispose 钩子会永久泄漏）
		pi.cleanupFailedLoad(pi.coordinatorOrNil(), gm)
		pi.mu.Lock()
		pi.state = Error
		pi.lastError = res.err
		pi.goroutineMgr = nil
		pi.mu.Unlock()
		return res.err
	}

	if res.api != nil {
		pi.mu.Lock()
		pi.exportedAPI = res.api
		pi.mu.Unlock()
		if setupCtx != nil {
			setupCtx.exportAs(pi.desc.Name, res.api)
		}
	}

	pi.mu.Lock()
	pi.state = Loaded
	pi.loadTime = startTime
	pi.lastError = nil
	pi.loadedVer = pi.desc.Version
	pi.mu.Unlock()

	return nil
}

// buildTeardownContext 构建 TeardownContext
func (pi *Instance) buildTeardownContext() *TeardownContext {
	pi.mu.RLock()
	api := pi.exportedAPI
	setupCtx := pi.setupContext
	name := pi.desc.Name
	pi.mu.RUnlock()

	var cfg Config
	var bus EventBus
	var info Info
	if setupCtx != nil {
		cfg = setupCtx.Config
		bus = setupCtx.EventBus
		info = setupCtx.Info
	}
	return &TeardownContext{
		API:      api,
		Config:   cfg,
		EventBus: bus,
		Log:      newPluginLogger(name),
		Info:     info,
	}
}

// unload 卸载插件（实现 pluginInternal）
// ctx 用于控制超时：若 context 到期，跳过剩余步骤并返回 ctx.Err()。
func (pi *Instance) unload(ctx context.Context, coordinator engine.GroupWriter) error {
	pi.mu.Lock()
	pi.state = Unloading
	gm := pi.goroutineMgr
	setupCtx := pi.setupContext
	pi.mu.Unlock()

	// Step 0: 清理 Scope 追踪的资源（subscriptions、middleware、child scopes、dispose hooks）
	if setupCtx != nil {
		if sc := setupCtx.rootScopeOrNil(); sc != nil {
			if err := sc.Dispose(ctx); err != nil {
				logger.WithField("plugin", pi.desc.Name).WithError(err).Warn("[Instance] Scope dispose failed during unload")
			}
		}
	}

	// Step 1: 停止所有生命周期绑定的 goroutine（在 Teardown 前）
	//
	// 受调用方 ctx 约束：不受限的等待会让 unload 在插件 goroutine 卡住时
	// 无限阻塞，与"卸载/停机受 ctx 超时约束"的契约矛盾。
	if gm != nil {
		if err := gm.stopAndWaitCtx(ctx); err != nil {
			logger.WithField("plugin", pi.desc.Name).WithError(err).
				Warn("[Instance] Timed out waiting for plugin goroutines during unload")
			pi.mu.Lock()
			pi.state = Error
			pi.lastError = err
			pi.mu.Unlock()
			return err
		}
	}

	select {
	case <-ctx.Done():
		// 置为 Error 而不是留在 Unloading：Unloading 没有任何恢复路径，
		// Error 状态可通过 Retry/ForceUnregister 处理。
		pi.mu.Lock()
		pi.state = Error
		pi.lastError = ctx.Err()
		pi.mu.Unlock()
		return ctx.Err()
	default:
	}

	// Step 2: 清理 Matcher
	if coordinator != nil {
		coordinator.RemoveGroup(pi.desc.Name)
	}
	pi.mu.Lock()
	pi.matchers = pi.matchers[:0]
	pi.goroutineMgr = nil
	pi.mu.Unlock()

	// Step 3: 调用 Teardown
	tctx := pi.buildTeardownContext()
	err := pi.desc.callTeardown(tctx)

	pi.mu.Lock()
	if err != nil {
		pi.state = Error
		pi.lastError = err
	} else {
		pi.state = Unloaded
		pi.exportedAPI = nil
	}
	pi.mu.Unlock()

	return err
}

// cleanupFailedLoad 幂等回收"加载失败/超时"留下的资源。
//
// 加载失败（Setup 返回错误、ctx 超时）此前只停掉 goroutine 管理器：rootScope
// 上的 EventBus 订阅、中间件、OnDispose 钩子、子 Scope 全部无人回收——插件
// 已被判定失败，这些钩子却还在总线上响应事件、清理逻辑永不执行。超时路径更
// 隐蔽：Setup 仍在后台运行，清理之后它继续注册的 Matcher 会落进已清空的分组。
//
// 因此调用两次：
//   - 失败路径立即调用（回收已产生的资源）；
//   - 超时路径在 Setup 自然结束后再调用一次（幂等，回收其后创建的资源）。
//
// gm 为 nil 时使用实例当前的 goroutine 管理器（超时路径已把它从实例上摘除，
// 需显式传入）。幂等：Scope.Dispose 有 disposed 标记，gm 停止与 RemoveGroup
// 都可重复调用。
func (pi *Instance) cleanupFailedLoad(coordinator engine.GroupWriter, gm *goroutineManager) {
	if pi == nil {
		return
	}
	pi.mu.RLock()
	name := ""
	if pi.desc != nil {
		name = pi.desc.Name
	}
	setupCtx := pi.setupContext
	if gm == nil {
		gm = pi.goroutineMgr
	}
	pi.mu.RUnlock()

	if setupCtx != nil {
		if sc := setupCtx.rootScopeOrNil(); sc != nil {
			if err := sc.Dispose(context.Background()); err != nil {
				logger.WithField("plugin", name).WithError(err).
					Warn("[Instance] Scope dispose failed after failed load")
			}
		}
	}
	if gm != nil {
		waitCtx, cancel := context.WithTimeout(context.Background(), pluginGoroutineStopTimeout)
		if err := gm.stopAndWaitCtx(waitCtx); err != nil {
			logger.WithField("plugin", name).WithError(err).
				Warn("[Instance] Timed out stopping goroutines after failed load")
		}
		cancel()
	}
	if coordinator != nil {
		coordinator.RemoveGroup(name)
	}
}

// coordinatorOrNil 返回实例所属 Manager 的引擎协调器（无 Manager 时返回 nil）。
func (pi *Instance) coordinatorOrNil() engine.GroupWriter {
	if m := pi.managerRef(); m != nil {
		return m.coordinator
	}
	return nil
}

// dependencies 返回依赖列表（实现 pluginInternal）
func (pi *Instance) dependencies() []string { return pi.desc.Deps }

// descriptor 返回当前描述符指针。
// finalizeRegistration 会在合并未声明依赖时 COW 替换 desc（持 inst.mu），
// 无锁调用方（Metadata/GetStatus 等）应通过此方法读取，避免数据竞争。
func (pi *Instance) descriptor() *Descriptor {
	pi.mu.RLock()
	defer pi.mu.RUnlock()
	return pi.desc
}

// setDescriptor 替换描述符（COW 合并未声明依赖时由 Manager 调用）。
func (pi *Instance) setDescriptor(d *Descriptor) {
	pi.mu.Lock()
	pi.desc = d
	pi.depsModified = true
	pi.mu.Unlock()
}

// exportedKeys 返回本实例 Setup 期间通过 exportAs/ExportIface 导出的所有容器 key 快照。
func (pi *Instance) exportedKeys() []string {
	pi.mu.RLock()
	sc := pi.setupContext
	pi.mu.RUnlock()
	if sc == nil {
		return nil
	}
	return sc.exportedKeysSnapshot()
}

// --- 公开 API ---

// Name 返回插件名称
func (pi *Instance) Name() string { return pi.desc.Name }

// Metadata 返回插件元数据。
//
// 返回的是副本：调用方修改返回值不会影响插件描述符，
// 并发调用也不会像旧实现那样并发改写共享的 desc.Meta。
func (pi *Instance) Metadata() *Metadata {
	d := pi.descriptor()
	m := Metadata{}
	if em := d.effectiveMeta(); em != nil {
		m = *em
		m.Tags = append([]string(nil), em.Tags...)
	}
	m.Name = d.Name
	m.Version = d.Version
	m.Dependencies = append([]string(nil), d.Deps...)
	return &m
}

// GetState 获取插件状态（实现 StatefulPlugin 接口）
func (pi *Instance) GetState() State {
	pi.mu.RLock()
	defer pi.mu.RUnlock()
	return pi.state
}

// SetState 设置插件状态（由 Manager 调用）
func (pi *Instance) SetState(state State) {
	pi.mu.Lock()
	defer pi.mu.Unlock()
	pi.state = state
}

// GetLoadTime 获取加载时间（实现 StatefulPlugin 接口）
func (pi *Instance) GetLoadTime() time.Time {
	pi.mu.RLock()
	defer pi.mu.RUnlock()
	return pi.loadTime
}

// SetLoadTime 设置加载时间（由 Manager 调用）
func (pi *Instance) SetLoadTime(t time.Time) {
	pi.mu.Lock()
	defer pi.mu.Unlock()
	pi.loadTime = t
}

// GetLastError 获取最后的错误（实现 StatefulPlugin 接口）
func (pi *Instance) GetLastError() error {
	pi.mu.RLock()
	defer pi.mu.RUnlock()
	return pi.lastError
}

// SetLastError 设置最后的错误（由 Manager 调用）
func (pi *Instance) SetLastError(err error) {
	pi.mu.Lock()
	defer pi.mu.Unlock()
	pi.lastError = err
}

// GetUptime 获取运行时长。Disabled 状态视为仍在运行，继续累计 uptime。
func (pi *Instance) GetUptime() time.Duration {
	pi.mu.RLock()
	loadTime := pi.loadTime
	state := pi.state
	pi.mu.RUnlock()

	if (state != Loaded && state != Disabled) || loadTime.IsZero() {
		return 0
	}
	return time.Since(loadTime)
}

// GetMatchers 获取插件注册的所有匹配器（实现 MatcherProvider 接口）
func (pi *Instance) GetMatchers() []*engine.Matcher {
	pi.mu.RLock()
	defer pi.mu.RUnlock()
	result := make([]*engine.Matcher, len(pi.matchers))
	copy(result, pi.matchers)
	return result
}

// addMatcher 添加 Matcher 到追踪列表（内部方法）
func (pi *Instance) addMatcher(matcher *engine.Matcher) {
	pi.mu.Lock()
	defer pi.mu.Unlock()
	pi.matchers = append(pi.matchers, matcher)
}

// GetConfig 获取插件配置（实现 ConfigurablePlugin 接口）
func (pi *Instance) GetConfig() Config {
	pi.mu.RLock()
	defer pi.mu.RUnlock()
	if pi.setupContext != nil {
		return pi.setupContext.Config
	}
	return nil
}

// GetAPI 返回 Setup 阶段导出的 API 对象。
//
// 框架以插件名为 key 将此对象注册到容器中；其他插件通过 [Service] / [TryService] 获取的即为此对象。
// 若插件尚未加载、Setup 返回 nil 或插件已卸载，则返回 nil。
//
// 常见用途：debug/monitor 类插件遍历所有插件实例并打印 API 类型信息。
func (pi *Instance) GetAPI() any {
	pi.mu.RLock()
	defer pi.mu.RUnlock()
	return pi.exportedAPI
}

// LoadedVersion 返回当前加载的插件版本号。用于迁移检测。
func (pi *Instance) LoadedVersion() string {
	pi.mu.RLock()
	defer pi.mu.RUnlock()
	return pi.loadedVer
}

// SetConfig 设置插件配置（实现 ConfigurablePlugin 接口）
func (pi *Instance) SetConfig(config Config) {
	pi.mu.Lock()
	defer pi.mu.Unlock()
	if pi.setupContext != nil {
		pi.setupContext.Config = config
	}
}
