package plugin

import (
	"fmt"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/engine"
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
	mu           sync.RWMutex
}

// --- pluginInternal 实现（包私有，供 Manager 内部使用）---

func (pi *Instance) name() string { return pi.desc.Name }

// load 加载插件（实现 pluginInternal）
func (pi *Instance) load() (loadErr error) {
	pi.mu.Lock()
	pi.state = Loading
	gm := newGoroutineManagerForPlugin(pi.desc.Name)
	pi.goroutineMgr = gm
	if pi.setupContext != nil {
		pi.setupContext.goroutineMgr = gm
	}
	pi.mu.Unlock()

	startTime := time.Now()

	// 捕获 Setup 中的 panic（如 MustGet 找不到依赖），转换为错误返回。
	// 不捕获会导致 panic 穿透 Register 直接崩溃整个进程。
	func() {
		defer func() {
			if r := recover(); r != nil {
				if e, ok := r.(error); ok {
					loadErr = fmt.Errorf("plugin %q: Setup panicked: %w", pi.desc.Name, e)
				} else {
					loadErr = fmt.Errorf("plugin %q: Setup panicked: %v", pi.desc.Name, r)
				}
			}
		}()
		var api any
		api, loadErr = pi.desc.callSetup(pi.setupContext)
		if loadErr == nil && api != nil {
			pi.mu.Lock()
			pi.exportedAPI = api
			pi.mu.Unlock()
			if pi.setupContext != nil {
				pi.setupContext.ExportAs(pi.desc.Name, api)
			}
		}
	}()

	if loadErr != nil {
		gm.stopAndWait()
		pi.mu.Lock()
		pi.state = Error
		pi.lastError = loadErr
		pi.goroutineMgr = nil
		pi.mu.Unlock()
		return loadErr
	}

	pi.mu.Lock()
	pi.state = Loaded
	pi.loadTime = startTime
	pi.lastError = nil
	pi.mu.Unlock()

	return nil
}

// buildTeardownContext 构建 TeardownContext
func (pi *Instance) buildTeardownContext() *TeardownContext {
	pi.mu.RLock()
	api := pi.exportedAPI
	pi.mu.RUnlock()

	var cfg Config
	var bus EventBus
	var info Info
	if pi.setupContext != nil {
		cfg = pi.setupContext.Config
		bus = pi.setupContext.EventBus
		info = pi.setupContext.Info // 复用 Setup 阶段的 Info 只读视图
	}
	return &TeardownContext{
		API:      api,
		Config:   cfg,
		EventBus: bus,
		Log:      newPluginLogger(pi.desc.Name),
		Info:     info,
	}
}

// unload 卸载插件（实现 pluginInternal）
func (pi *Instance) unload(coordinator engine.GroupWriter) error {
	pi.mu.Lock()
	pi.state = Unloading
	gm := pi.goroutineMgr
	pi.mu.Unlock()

	// Step 1: 停止所有生命周期绑定的 goroutine（在 Teardown 前）
	if gm != nil {
		gm.stopAndWait()
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

// --- 公开 API ---

// Name 返回插件名称
func (pi *Instance) Name() string { return pi.desc.Name }

// Metadata 返回插件元数据
func (pi *Instance) Metadata() *Metadata {
	m := pi.desc.effectiveMeta()
	if m == nil {
		m = &Metadata{}
	}
	m.Name = pi.desc.Name
	m.Version = pi.desc.Version
	m.Dependencies = pi.desc.Deps
	return m
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
// 框架以插件名为 key 将此对象注册到容器中；其他插件通过 [Must] / [Try] 获取的即为此对象。
// 若插件尚未加载、Setup 返回 nil 或插件已卸载，则返回 nil。
//
// 常见用途：debug/monitor 类插件遍历所有插件实例并打印 API 类型信息。
func (pi *Instance) GetAPI() any {
	pi.mu.RLock()
	defer pi.mu.RUnlock()
	return pi.exportedAPI
}

// SetConfig 设置插件配置（实现 ConfigurablePlugin 接口）
func (pi *Instance) SetConfig(config Config) {
	pi.mu.Lock()
	defer pi.mu.Unlock()
	if pi.setupContext != nil {
		pi.setupContext.Config = config
	}
}
