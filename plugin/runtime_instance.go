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

// --- pluginInternal 实现（包私有，供 Manager 内部使用）---

func (pi *Instance) name() string { return pi.desc.Name } //nolint:unused

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
		pi.mu.Lock()
		pi.state = Error
		pi.lastError = ctx.Err()
		pi.goroutineMgr = nil
		pi.mu.Unlock()
		go func() {
			<-resCh
			gm.stopAndWait()
		}()
		return ctx.Err()
	}

	if res.err != nil {
		gm.stopAndWait()
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
			setupCtx.ExportAs(pi.desc.Name, res.api)
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
	if setupCtx != nil && setupCtx.rootScope != nil {
		if err := setupCtx.rootScope.Dispose(ctx); err != nil {
			logger.WithField("plugin", pi.desc.Name).WithError(err).Warn("[Instance] Scope dispose failed during unload")
		}
	}

	// Step 1: 停止所有生命周期绑定的 goroutine（在 Teardown 前）
	if gm != nil {
		gm.stopAndWait()
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

// exportedKeys 返回本实例 Setup 期间通过 ExportAs/ExportIface 导出的所有容器 key 快照。
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
