package plugin

import (
	stdctx "context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/errutil"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// PluginMeta 插件元数据（可选，影响 /help 显示）
//
// 将 Author/Description/Category/Tags/HelpText/Hidden 从顶层字段迁移到此结构，
// 减少 PluginDescriptor 的认知负担——大多数核心插件只需 Name/Deps/Setup/Teardown。
type PluginMeta struct {
	Author      string   // 作者
	Description string   // 描述
	HelpText    string   // 帮助文本
	Category    string   // 分类
	Tags        []string // 标签
	Hidden      bool     // 是否在帮助中隐藏
}

// ReloadStrategy 定义插件热重载策略
type ReloadStrategy int

const (
	// ReloadUnloadLoad 停机重载：先卸载旧实例（移除 Matcher、停止 goroutine），再加载新实例。
	// 存在短暂不可用窗口，但支持完整的状态迁移（SaveState/RestoreState）。
	// 这是默认策略（兼容旧行为）。
	ReloadUnloadLoad ReloadStrategy = iota

	// ReloadInPlace 原地重载：调用 Advanced.Reload 函数，插件自行处理状态迁移。
	// 适合能在不停止旧 Matcher 的情况下完成状态更新的插件。
	// 若 Advanced.Reload 为 nil，回退为 ReloadUnloadLoad。
	ReloadInPlace

	// ReloadBlueGreen 蓝绿重载：并行运行新实例的 Setup，完成后原子切换，最后停止旧实例。
	// 零停机：切换过程中旧 Matcher 仍然处理消息，新实例就绪后一次性接管。
	// 适合无状态或自带快照能力的插件。
	ReloadBlueGreen
)

// PluginAdvanced 插件高级选项（可选）
//
// 热重载、状态迁移、依赖回调等高级功能。仅在需要时填写，减少普通插件的复杂度。
type PluginAdvanced struct {
	// Strategy 热重载策略（可选，默认 ReloadUnloadLoad）
	Strategy ReloadStrategy

	// Reload 自定义热重载函数（可选）
	// 若为 nil 且 Strategy == ReloadInPlace，回退为 ReloadUnloadLoad。
	// 若 Strategy == ReloadBlueGreen，此字段被忽略（框架自动处理）。
	Reload ReloadFunc

	// OnDependencyReloaded 依赖热重载通知回调（可选）
	// 当本插件的某个依赖完成热重载后被调用
	OnDependencyReloaded func(reloadedDep string)

	// SaveState 热重载前保存内存态（可选，仅 ReloadUnloadLoad 策略生效）
	SaveState SaveStateFunc

	// RestoreState 热重载后恢复内存态（可选，仅 ReloadUnloadLoad 策略生效）
	RestoreState RestoreStateFunc

	// ConfigSchema 配置结构（可选）
	// 可使用 map[string]plugin.SchemaField 或带 `schema:"required"` tag 的 struct 指针。
	// 框架在插件注册时自动校验，校验失败返回 SchemaValidationError。
	ConfigSchema any
}

// TeardownContext Teardown 阶段的上下文（P3-2）
//
// 新签名 TeardownFuncV3 的参数，提供 Teardown 阶段合理可用的资源，
// 替代旧的无参数 `func() error` 闭包模式。
//
// 示例：
//
//	Teardown: func(ctx *plugin.TeardownContext) error {
//	    ctx.API.(*MyPlugin).Save()
//	    ctx.Log.Info("plugin stopped")
//	    return nil
//	},
type TeardownContext struct {
	// API 是 SetupFuncV3 返回的插件 API 对象
	// 旧签名（func() error）下此字段为 nil
	API any

	// Config 插件配置
	Config Config

	// EventBus 插件间事件总线
	EventBus EventBus

	// Log 带插件名前缀的日志器
	Log PluginLogger
}

// ReloadFunc 插件热重载函数
// 实现热重载逻辑（可选）
// 如果不实现，将使用默认的 Teardown + Setup 策略
type ReloadFunc = func(ctx *SetupContext) error

// SaveStateFunc 插件状态保存函数
// 在热重载前保存插件状态（可选）
// 返回的状态数据将传递给 RestoreStateFunc
type SaveStateFunc = func() (any, error)

// RestoreStateFunc 插件状态恢复函数
// 在热重载后恢复插件状态（可选）
// 接收 SaveStateFunc 返回的状态数据
type RestoreStateFunc = func(state any) error

// PluginDescriptor 插件描述符
//
// # 最简用法（仅需 Name + Setup）
//
//	&plugin.PluginDescriptor{
//	    Name:  "myplugin",
//	    Setup: func(ctx *plugin.SetupContext) (any, error) {
//	        p := NewPlugin()
//	        return p, nil  // 框架自动导出到容器
//	    },
//	}
//
// # 完整用法（含元数据和高级选项）
//
//	&plugin.PluginDescriptor{
//	    Name:    "myplugin",
//	    Version: "1.0.0",
//	    Deps:    []string{"storage"},
//	    Meta: &plugin.PluginMeta{
//	        Author:      "Team",
//	        Description: "My plugin",
//	        Category:    "core",
//	    },
//	    Setup:    func(ctx *plugin.SetupContext) (any, error) { ... },
//	    Teardown: func(ctx *plugin.TeardownContext) error { ... },
//	}
type PluginDescriptor struct {
	// Name 插件名称（必需，全局唯一）
	Name string

	// Version 版本号（建议填写，格式：semver）
	Version string

	// Deps 依赖的插件名称列表
	// 框架保证 Deps 中的插件在本插件 Setup 前已完成加载
	Deps []string

	// Setup 插件初始化函数（必需）
	// 签名：func(*SetupContext) (any, error)
	// 返回值 any 是插件导出的 API 对象，框架自动注入容器。
	// 若不导出 API（如纯命令注册插件），返回 nil, nil。
	Setup func(*SetupContext) (any, error)

	// Teardown 插件清理函数（可选）
	// 签名：func(*TeardownContext) error
	// TeardownContext 提供 API、Log、Config 等资源，无需闭包捕获。
	Teardown func(*TeardownContext) error

	// Meta 插件元数据（可选，影响 /help 显示）
	Meta *PluginMeta

	// Advanced 高级选项（可选）
	// 包含 Reload/OnDependencyReloaded/SaveState/RestoreState/ConfigSchema
	Advanced *PluginAdvanced
}

// --- PluginDescriptor 辅助方法 ---

// effectiveMeta 返回元数据（Meta 为 nil 时返回零值）
func (d *PluginDescriptor) effectiveMeta() PluginMeta {
	if d.Meta != nil {
		return *d.Meta
	}
	return PluginMeta{}
}

// effectiveAdvanced 返回高级选项（Advanced 为 nil 时返回零值）
func (d *PluginDescriptor) effectiveAdvanced() PluginAdvanced {
	if d.Advanced != nil {
		return *d.Advanced
	}
	return PluginAdvanced{}
}

// callSetup 调用 Setup 函数
func (d *PluginDescriptor) callSetup(ctx *SetupContext) (any, error) {
	if d.Setup == nil {
		return nil, fmt.Errorf("plugin %q: Setup function is nil", d.Name)
	}
	return d.Setup(ctx)
}

// callTeardown 调用 Teardown 函数（nil 则跳过）
func (d *PluginDescriptor) callTeardown(tctx *TeardownContext) error {
	if d.Teardown == nil {
		return nil
	}
	return d.Teardown(tctx)
}

// getReloadFunc 获取 Reload 函数
func (d *PluginDescriptor) getReloadFunc() ReloadFunc {
	if d.Advanced != nil {
		return d.Advanced.Reload
	}
	return nil
}

// getSaveStateFunc 获取 SaveState 函数
func (d *PluginDescriptor) getSaveStateFunc() SaveStateFunc {
	if d.Advanced != nil {
		return d.Advanced.SaveState
	}
	return nil
}

// getRestoreStateFunc 获取 RestoreState 函数
func (d *PluginDescriptor) getRestoreStateFunc() RestoreStateFunc {
	if d.Advanced != nil {
		return d.Advanced.RestoreState
	}
	return nil
}

// getOnDependencyReloaded 获取 OnDependencyReloaded 回调
func (d *PluginDescriptor) getOnDependencyReloaded() func(string) {
	if d.Advanced != nil {
		return d.Advanced.OnDependencyReloaded
	}
	return nil
}

// SetupContext 插件初始化上下文
//
//   - [SetupContext.Reg]      — Matcher/Command 注册（自动追踪）
//   - [SetupContext.Log]      — 带插件名前缀的结构化日志
//   - [SetupContext.Info]     — 插件系统只读视图
//   - [SetupContext.Go]       — 生命周期绑定后台 goroutine
//   - [SetupContext.Config]   — 插件配置
//   - [SetupContext.EventBus] — 插件间事件总线
//   - [SetupContext.Get] / [SetupContext.MustGet] — 获取依赖（弱类型）
//   - [Require] / [Optional]  — 获取依赖（类型安全，推荐）
//
// setupContextInternal 框架内部字段，外部 godoc 不可见。
type setupContextInternal struct {
	container        *Container
	pluginName       string
	instance         *PluginInstance
	trackedDeps      map[string]bool
	autoTrackEnabled bool
	goroutineMgr     *goroutineManager
	eng              *engine.Engine // 注册 Matcher 的 engine（reload 时复用）
}

// SetupContext 插件 Setup 阶段的上下文。
//
// 通过此上下文注册命令、访问依赖、启动后台 goroutine 等。
// 内部字段（框架专用）已隐藏，对外仅暴露公开 API。
type SetupContext struct {
	// Reg Matcher/Command 注册接口，DryRun 阶段自动变为 no-op。
	Reg RegistryWriter

	// Log 带插件名前缀的结构化日志器。
	Log PluginLogger

	// Info 插件系统只读视图，可查询其他插件状态。
	Info PluginInfo

	// Go 启动一个与插件生命周期绑定的后台 goroutine。
	// 框架在 Teardown 前自动 cancel 并等待所有 goroutine 退出。
	//
	//   ctx.Go(func(runCtx context.Context) {
	//       ticker := time.NewTicker(time.Minute)
	//       defer ticker.Stop()
	//       for {
	//           select {
	//           case <-ticker.C: cleanup()
	//           case <-runCtx.Done(): return
	//           }
	//       }
	//   })
	Go func(fn func(ctx stdctx.Context))

	// Config 插件配置
	Config Config

	// EventBus 插件间事件总线
	EventBus EventBus

	// 内部字段（框架使用，外部不可访问）
	setupContextInternal
}

// ExportAs 将插件 API 对象以指定名称导出到容器。
//
// 这是手动调用 ctx.Manager.GetContainer().Register(name, api) 的类型安全替代。
// ExportAs 将插件 API 对象以指定名称导出到容器。
//
// 通常配合旧签名 Setup 使用；新签名 Setup 直接 return api, nil 即可，
// 框架会自动以插件名为 key 注入容器。
// 当需要以自定义 key（不同于插件名）导出时，仍可手动调用此方法。
func (ctx *SetupContext) ExportAs(name string, api any) {
	if ctx.container != nil {
		ctx.container.Register(name, api)
	}
}

// Get 获取依赖插件（弱类型）
// 自动记录依赖关系，用于 Smart 注册的依赖推断。
// 推荐改用类型安全的 [Require] / [Optional]。
func (ctx *SetupContext) Get(name string) (any, bool) {
	if ctx.container == nil {
		return nil, false
	}
	if ctx.autoTrackEnabled && name != "" && name != ctx.pluginName {
		if ctx.trackedDeps == nil {
			ctx.trackedDeps = make(map[string]bool)
		}
		ctx.trackedDeps[name] = true
	}
	return ctx.container.Get(name)
}

// MustGet 获取依赖插件（弱类型，不存在则 panic）
// 推荐改用类型安全的 [Require]。
func (ctx *SetupContext) MustGet(name string) any {
	v, ok := ctx.Get(name)
	if !ok {
		panic(fmt.Sprintf("plugin %q: required dependency %q not found", ctx.pluginName, name))
	}
	return v
}

// GetTrackedDependencies 获取自动跟踪到的依赖列表（框架内部使用）
func (ctx *SetupContext) GetTrackedDependencies() []string {

	if ctx.trackedDeps == nil {
		return []string{}
	}
	deps := make([]string, 0, len(ctx.trackedDeps))
	for name := range ctx.trackedDeps {
		deps = append(deps, name)
	}
	return deps
}

// --- 类型安全依赖获取 ---

// GetPlugin 获取依赖插件（类型安全，返回 (*T, error)）
//
//	p, err := plugin.GetPlugin[permission.Plugin](ctx, "permission")
func GetPlugin[T any](ctx *SetupContext, name string) (*T, error) {
	v, ok := ctx.Get(name)
	if !ok {
		return nil, fmt.Errorf("plugin %q: dependency %q not found", ctx.pluginName, name)
	}
	typed, ok := v.(*T)
	if !ok {
		return nil, fmt.Errorf("plugin %q: dependency %q has wrong type: expected *%T, got %T", ctx.pluginName, name, typed, v)
	}
	return typed, nil
}

// Require 获取必需依赖（类型安全，不存在或类型不符则 panic）
//
//	perm := plugin.Require[permission.Plugin](ctx, "permission")
func Require[T any](ctx *SetupContext, name string) *T {
	v, ok := ctx.Get(name)
	if !ok {
		panic(fmt.Sprintf("plugin %q: required dependency %q not found", ctx.pluginName, name))
	}
	typed, ok := v.(*T)
	if !ok {
		panic(fmt.Sprintf("plugin %q: dependency %q has wrong type: expected *%T, got %T", ctx.pluginName, name, typed, v))
	}
	return typed
}

// Optional 获取可选依赖（类型安全，不存在时返回 nil, false）
//
//	if sb, ok := plugin.Optional[storage.Plugin](ctx, "storage"); ok { p.storage = sb }
func Optional[T any](ctx *SetupContext, name string) (*T, bool) {
	v, ok := ctx.Get(name)
	if !ok {
		return nil, false
	}
	typed, ok := v.(*T)
	if !ok {
		return nil, false
	}
	return typed, true
}

// Must 获取必需依赖（Require 的简洁别名，推荐使用）。
// 类型安全，不存在或类型不符则 panic。
//
//	perm := plugin.Must[permission.Plugin](ctx, "permission")
func Must[T any](ctx *SetupContext, name string) *T {
	return Require[T](ctx, name)
}

// Try 获取可选依赖（Optional 的简洁别名，推荐使用）。
// 类型安全，不存在时返回 nil, false。
//
//	if sb, ok := plugin.Try[storage.Plugin](ctx, "storage"); ok { p.storage = sb }
func Try[T any](ctx *SetupContext, name string) (*T, bool) {
	return Optional[T](ctx, name)
}

// Container 依赖注入容器
//
// 支持两阶段使用模式：
//  1. 注册阶段（Register/Remove）：使用 sync.Map 保证并发安全
//  2. 冻结阶段（Freeze 后）：Get/Has 切换为原子指针只读快照，读性能提升 2-3x
//
// 并发安全说明：
//   - 冻结后调用 Register/Remove 会通过 snapshotMu 互斥地重建快照，
//     再通过 atomic.Pointer 原子替换，确保 Get 读到的始终是完整一致的快照。
//
// 插件全部加载完成后调用 Freeze()，后续 Get 仅需一次原子 Load，无锁竞争。
type Container struct {
	services sync.Map // 注册阶段及冻结后的写操作

	// 冻结后的只读快照，使用 atomic.Pointer 原子替换，消除 data race
	frozen     atomic.Bool
	frozenMap  atomic.Pointer[map[string]any]
	snapshotMu sync.Mutex // 保护 refreshSnapshot 的并发重建
}

// NewContainer 创建依赖注入容器
func NewContainer() *Container {
	return &Container{}
}

// Register 注册服务。冻结后会自动刷新只读快照，支持热重载/动态注册场景。
func (c *Container) Register(name string, service any) {
	c.services.Store(name, service)
	// 若已冻结，同步刷新快照保持一致性
	if c.frozen.Load() {
		c.refreshSnapshot()
	}
}

// Freeze 将容器切换为只读快照模式。
// 调用后 Get/Has 使用原子指针快照，读性能提升 2-3x。
// 冻结后仍可调用 Register/Remove，会自动原子替换快照。
func (c *Container) Freeze() {
	c.frozen.Store(true)
	c.refreshSnapshot()
}

// refreshSnapshot 重建只读快照并原子替换（并发安全）。
// snapshotMu 防止多个 goroutine 同时重建导致读取到旧快照指针。
func (c *Container) refreshSnapshot() {
	c.snapshotMu.Lock()
	defer c.snapshotMu.Unlock()

	snapshot := make(map[string]any)
	c.services.Range(func(k, v any) bool {
		snapshot[k.(string)] = v
		return true
	})
	// 原子替换快照指针，Get 读取时不会看到部分更新
	c.frozenMap.Store(&snapshot)
}

// Get 获取服务。冻结后通过原子 Load 读取快照，无锁竞争。
func (c *Container) Get(name string) (any, bool) {
	if c.frozen.Load() {
		if m := c.frozenMap.Load(); m != nil {
			v, ok := (*m)[name]
			return v, ok
		}
	}
	return c.services.Load(name)
}

// Has 检查服务是否存在
func (c *Container) Has(name string) bool {
	_, ok := c.Get(name)
	return ok
}

// Remove 移除服务。冻结后会自动刷新只读快照。
func (c *Container) Remove(name string) {
	c.services.Delete(name)
	if c.frozen.Load() {
		c.refreshSnapshot()
	}
}

// PluginInstance v2 插件实例
//
// 通过 [Manager.Get] 获取，可用于查询状态、元数据和已注册的 Matcher。
// 生命周期操作（加载/卸载/重载）由 Manager 通过内部 pluginInternal 接口驱动。
type PluginInstance struct {
	desc         *PluginDescriptor
	state        State
	setupContext *SetupContext
	matchers     []*engine.Matcher // 插件注册的匹配器
	loadTime     time.Time         // 加载时间
	lastError    error             // 最后的错误
	goroutineMgr *goroutineManager // 生命周期绑定 goroutine 管理器
	exportedAPI  any               // SetupFuncV3 返回的 API 对象（P3-1）
	mu           sync.RWMutex
}

// --- pluginInternal 实现（包私有，供 Manager 内部使用）---

// name 返回插件名称（实现 pluginInternal）
func (pi *PluginInstance) name() string {
	return pi.desc.Name
}

// load 加载插件（实现 pluginInternal）
func (pi *PluginInstance) load(coordinator *engine.Engine) error {
	pi.mu.Lock()
	pi.state = Loading
	gm := newGoroutineManager()
	pi.goroutineMgr = gm
	if pi.setupContext != nil {
		pi.setupContext.goroutineMgr = gm
		pi.setupContext.Go = gm.go_
	}
	pi.mu.Unlock()

	startTime := time.Now()

	// 调用 Setup（支持新旧两种签名，P3-1）
	api, err := pi.desc.callSetup(pi.setupContext)
	if err != nil {
		gm.stopAndWait()
		pi.mu.Lock()
		pi.state = Error
		pi.lastError = err
		pi.goroutineMgr = nil
		pi.mu.Unlock()
		return err
	}

	// 新签名：将 API 对象保存，框架自动 ExportAs（P3-1）
	// 旧签名返回 nil，插件通过 ctx.ExportAs 手动导出（向后兼容）
	if api != nil {
		pi.mu.Lock()
		pi.exportedAPI = api
		pi.mu.Unlock()
		if pi.setupContext != nil {
			pi.setupContext.ExportAs(pi.desc.Name, api)
		}
	}

	pi.mu.Lock()
	pi.state = Loaded
	pi.loadTime = startTime
	pi.lastError = nil
	pi.mu.Unlock()

	return nil
}

// buildTeardownContext 构建 TeardownContext（P3-2）
func (pi *PluginInstance) buildTeardownContext() *TeardownContext {
	pi.mu.RLock()
	api := pi.exportedAPI
	pi.mu.RUnlock()

	var cfg Config
	var bus EventBus
	if pi.setupContext != nil {
		cfg = pi.setupContext.Config
		bus = pi.setupContext.EventBus
	}
	return &TeardownContext{
		API:      api,
		Config:   cfg,
		EventBus: bus,
		Log:      newPluginLogger(pi.desc.Name),
	}
}

// unload 卸载插件（实现 pluginInternal）
func (pi *PluginInstance) unload(coordinator *engine.Engine) error {
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

	// Step 3: 调用 Teardown（支持新旧两种签名，P3-2）
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

// reload 重载插件（实现 pluginInternal）
func (pi *PluginInstance) reload(coordinator *engine.Engine) error {
	pi.mu.Lock()
	oldContext := pi.setupContext
	pi.state = Reloading
	pi.mu.Unlock()

	// 保存状态（P3：通过 effectiveAdvanced 获取钩子）
	adv := pi.desc.effectiveAdvanced()
	var savedState any
	if adv.SaveState != nil {
		var saveErr error
		savedState, saveErr = adv.SaveState()
		if saveErr != nil {
			logger.WithError(saveErr).Warn("[plugin] Failed to save state before reload")
		}
	}

	// 重新创建 SetupContext
	newContext := &SetupContext{
		Reg:      newLiveRegistryWriter(oldContext.eng, oldContext.pluginName, oldContext.instance),
		Log:      newPluginLogger(oldContext.pluginName),
		Info:     oldContext.Info,
		Config:   oldContext.Config,
		EventBus: oldContext.EventBus,
		setupContextInternal: setupContextInternal{
			container:        oldContext.container,
			pluginName:       oldContext.pluginName,
			instance:         oldContext.instance,
			autoTrackEnabled: true,
			eng:              oldContext.eng,
		},
	}
	newContext.Go = func(fn func(ctx stdctx.Context)) {
		if newContext.goroutineMgr != nil {
			newContext.goroutineMgr.go_(fn)
		}
	}

	pi.mu.Lock()
	pi.setupContext = newContext
	pi.mu.Unlock()

	// P4-5: 根据 ReloadStrategy 选择重载策略
	switch adv.Strategy {
	case ReloadInPlace:
		// 原地重载：调用 Advanced.Reload；若为 nil 则回退为 UnloadLoad
		if adv.Reload != nil {
			if err := adv.Reload(newContext); err != nil {
				pi.mu.Lock()
				pi.state = Error
				pi.lastError = err
				pi.mu.Unlock()
				return err
			}
			pi.mu.Lock()
			pi.state = Loaded
			pi.loadTime = time.Now()
			pi.lastError = nil
			pi.mu.Unlock()
			return nil
		}
		// Reload 为 nil，回退为 UnloadLoad
		fallthrough
	case ReloadUnloadLoad:
		// 停机重载（默认）：
		// 向后兼容：若 Advanced.Reload 有值，优先调用它（等价于 ReloadInPlace 行为）
		if adv.Reload != nil {
			if err := adv.Reload(newContext); err != nil {
				pi.mu.Lock()
				pi.state = Error
				pi.lastError = err
				pi.mu.Unlock()
				return err
			}
			pi.mu.Lock()
			pi.state = Loaded
			pi.loadTime = time.Now()
			pi.lastError = nil
			pi.mu.Unlock()
			if savedState != nil && adv.RestoreState != nil {
				if err := adv.RestoreState(savedState); err != nil {
					logger.WithError(err).Warn("[plugin] Failed to restore state after reload")
				}
			}
			return nil
		}
		// 无自定义 Reload：执行完整 unload → load 流程
		if err := pi.unload(coordinator); err != nil {
			return err
		}
		if err := pi.load(coordinator); err != nil {
			return err
		}
		if savedState != nil && adv.RestoreState != nil {
			if err := adv.RestoreState(savedState); err != nil {
				logger.WithError(err).Warn("[plugin] Failed to restore state after reload")
			}
		}
	case ReloadBlueGreen:
		// 蓝绿重载：并行运行新 Setup，就绪后原子切换，最后停止旧实例
		if err := pi.reloadBlueGreen(coordinator, newContext); err != nil {
			return err
		}
	default:
		// 未知策略，回退为 UnloadLoad
		if err := pi.unload(coordinator); err != nil {
			return err
		}
		if err := pi.load(coordinator); err != nil {
			return err
		}
	}

	return nil
}

// reloadBlueGreen 实现蓝绿（极短停机）重载策略（P4-5）
//
// 执行步骤：
//  1. 新实例的 Setup 在临时 group（name+".__bg"）中注册 Matcher，旧实例继续处理消息
//  2. 新 Setup 就绪后，RemoveGroup(name) 移除旧 Matcher（极短停机窗口）
//  3. 将新 Matcher group 改回插件名，使其立即接管（停机窗口结束）
//  4. 原子切换 pi 的内部状态指针
//  5. 异步停止旧 goroutine 并调用旧 Teardown
//
// 与 ReloadUnloadLoad 对比：
//   - 停机窗口从"整个 Setup 时间"缩短为"两次 engine 操作的间隔"（微秒级）
//   - 新实例的初始化（可能涉及 I/O）在后台并行完成
func (pi *PluginInstance) reloadBlueGreen(coordinator *engine.Engine, newContext *SetupContext) error {
	pluginName := pi.desc.Name
	tempGroup := pluginName + ".__bg"

	// 构建新实例，注册时使用临时 group 避免与旧 Matcher 冲突
	newInstance := &PluginInstance{
		desc:         pi.desc,
		state:        Unloaded,
		matchers:     make([]*engine.Matcher, 0),
		setupContext: newContext,
	}
	newContext.instance = newInstance
	// 新 Matcher 注册到临时 group，旧实例继续在 pluginName group 处理消息
	newContext.Reg = newLiveRegistryWriter(coordinator, tempGroup, newInstance)

	// Step 1: 并行运行新 Setup（旧实例零影响地继续处理消息）
	if err := newInstance.load(coordinator); err != nil {
		if coordinator != nil {
			coordinator.RemoveGroup(tempGroup)
		}
		return fmt.Errorf("blue-green reload: new instance setup failed: %w", err)
	}

	// Step 2+3: 极短停机窗口——移除旧 group，将新 Matcher 切换到实际 group
	if coordinator != nil {
		// 移除旧 Matcher（停机开始）
		coordinator.RemoveGroup(pluginName)
		// 将新 Matcher 的 group 从临时名改为实际插件名（停机结束）
		newInstance.mu.RLock()
		for _, m := range newInstance.matchers {
			m.SetGroup(pluginName)
		}
		newInstance.mu.RUnlock()
		// 清理临时 group 索引（Matcher 已改 group，此操作无害）
		coordinator.RemoveGroup(tempGroup)
	}

	// Step 4: 原子切换内部状态
	pi.mu.Lock()
	oldGM := pi.goroutineMgr
	oldAPI := pi.exportedAPI

	newInstance.mu.RLock()
	pi.matchers = newInstance.matchers
	pi.goroutineMgr = newInstance.goroutineMgr
	pi.exportedAPI = newInstance.exportedAPI
	newInstance.mu.RUnlock()

	pi.setupContext = newContext
	pi.state = Loaded
	pi.loadTime = time.Now()
	pi.lastError = nil
	pi.mu.Unlock()

	// Step 5: 异步停止旧 goroutine 并调用旧 Teardown
	go func() {
		if oldGM != nil {
			oldGM.stopAndWait()
		}
		tctx := &TeardownContext{
			API:      oldAPI,
			Config:   newContext.Config,
			EventBus: newContext.EventBus,
			Log:      newPluginLogger(pluginName),
		}
		if teardownErr := pi.desc.callTeardown(tctx); teardownErr != nil {
			logger.WithError(teardownErr).Warnf("[plugin] Blue-green: old instance teardown failed for %s", pluginName)
		}
	}()

	return nil
}

// dependencies 返回依赖列表（实现 pluginInternal）
func (pi *PluginInstance) dependencies() []string {
	return pi.desc.Deps
}

// --- 公开 API ---

// Name 返回插件名称
func (pi *PluginInstance) Name() string {
	return pi.desc.Name
}

// Metadata 返回元数据
func (pi *PluginInstance) Metadata() *Metadata {
	m := pi.desc.effectiveMeta()
	return &Metadata{
		Name:         pi.desc.Name,
		Version:      pi.desc.Version,
		Author:       m.Author,
		Description:  m.Description,
		HelpText:     m.HelpText,
		Category:     m.Category,
		Tags:         m.Tags,
		Dependencies: pi.desc.Deps,
		Hidden:       m.Hidden,
	}
}

// GetState 获取插件状态（实现 StatefulPlugin 接口）
func (pi *PluginInstance) GetState() State {
	pi.mu.RLock()
	defer pi.mu.RUnlock()
	return pi.state
}

// SetState 设置插件状态（实现 StatefulPlugin 接口）
func (pi *PluginInstance) SetState(state State) {
	pi.mu.Lock()
	defer pi.mu.Unlock()
	pi.state = state
}

// GetLoadTime 获取加载时间（实现 StatefulPlugin 接口）
func (pi *PluginInstance) GetLoadTime() time.Time {
	pi.mu.RLock()
	defer pi.mu.RUnlock()
	return pi.loadTime
}

// SetLoadTime 设置加载时间（实现 StatefulPlugin 接口）
func (pi *PluginInstance) SetLoadTime(t time.Time) {
	pi.mu.Lock()
	defer pi.mu.Unlock()
	pi.loadTime = t
}

// GetLastError 获取最后的错误（实现 StatefulPlugin 接口）
func (pi *PluginInstance) GetLastError() error {
	pi.mu.RLock()
	defer pi.mu.RUnlock()
	return pi.lastError
}

// SetLastError 设置最后的错误（实现 StatefulPlugin 接口）
func (pi *PluginInstance) SetLastError(err error) {
	pi.mu.Lock()
	defer pi.mu.Unlock()
	pi.lastError = err
}

// GetUptime 获取运行时长（实现 StatefulPlugin 接口）
// Disabled 状态视为仍在运行，继续累计 uptime。
func (pi *PluginInstance) GetUptime() time.Duration {
	pi.mu.RLock()
	loadTime := pi.loadTime
	state := pi.state
	pi.mu.RUnlock()

	// Loaded 和 Disabled 都计算 uptime（禁用的插件仍在内存中保持状态）
	if (state != Loaded && state != Disabled) || loadTime.IsZero() {
		return 0
	}

	return time.Since(loadTime)
}

// GetMatchers 获取插件注册的所有匹配器（实现 MatcherProvider 接口）
func (pi *PluginInstance) GetMatchers() []*engine.Matcher {
	pi.mu.RLock()
	defer pi.mu.RUnlock()
	result := make([]*engine.Matcher, len(pi.matchers))
	copy(result, pi.matchers)
	return result
}

// addMatcher 添加 Matcher 到追踪列表（内部方法）
func (pi *PluginInstance) addMatcher(matcher *engine.Matcher) {
	pi.mu.Lock()
	defer pi.mu.Unlock()
	pi.matchers = append(pi.matchers, matcher)
}

// GetConfig 获取插件配置（实现 ConfigurablePlugin 接口）
func (pi *PluginInstance) GetConfig() Config {
	pi.mu.RLock()
	defer pi.mu.RUnlock()
	if pi.setupContext != nil {
		return pi.setupContext.Config
	}
	return nil
}

// SetConfig 设置插件配置（实现 ConfigurablePlugin 接口）
func (pi *PluginInstance) SetConfig(config Config) {
	pi.mu.Lock()
	defer pi.mu.Unlock()
	if pi.setupContext != nil {
		pi.setupContext.Config = config
	}
}

// RegisterV2 注册 v2 风格的插件（使用 PluginDescriptor）
func (pm *Manager) RegisterV2(desc *PluginDescriptor) error {
	if desc == nil {
		return fmt.Errorf("plugin descriptor is nil")
	}

	if desc.Name == "" {
		return fmt.Errorf("plugin name is required")
	}

	if desc.Setup == nil {
		return fmt.Errorf("plugin setup function is required")
	}

	name := desc.Name

	pm.mu.Lock()

	// 检查重复
	if _, exists := pm.plugins[name]; exists {
		pm.mu.Unlock()
		logger.Warnf("[pluginManager] Plugin %s already registered", name)
		return errutil.ErrPluginAlreadyExists
	}

	// 检查依赖（P4-2: 富错误信息; P4-3: 版本约束检查）
	registeredList := func() []string {
		names := make([]string, 0, len(pm.plugins))
		for n := range pm.plugins {
			names = append(names, n)
		}
		return names
	}
	for _, rawDep := range desc.Deps {
		spec := parseDepSpec(rawDep)
		depInst, exists := pm.plugins[spec.name]
		if !exists {
			pm.mu.Unlock()
			return &PluginError{
				PluginName:        name,
				Operation:         "register",
				Cause:             fmt.Errorf("missing required dependency %q", spec.name),
				RegisteredPlugins: registeredList(),
				Hint:              fmt.Sprintf("register %q before %q", spec.name, name),
			}
		}
		// 验证依赖插件已完成加载（状态为 Loaded）
		state := depInst.GetState()
		if state != Loaded {
			pm.mu.Unlock()
			return &PluginError{
				PluginName:        name,
				Operation:         "register",
				Cause:             fmt.Errorf("dependency %q is not ready (state: %s)", spec.name, state),
				RegisteredPlugins: registeredList(),
				Hint:              "register plugins in dependency order",
			}
		}
		// P4-3: 版本约束检查（仅当依赖规格包含 @ 约束时）
		if spec.constraint != "" {
			ok, _ := checkVersionConstraint(depInst.desc.Version, spec.constraint)
			if !ok {
				pm.mu.Unlock()
				return &VersionConstraintError{
					Plugin:     name,
					Dependency: spec.name,
					Required:   spec.constraint,
					Have:       depInst.desc.Version,
				}
			}
		}
	}

	// 确保容器已初始化
	pm.ensureContainerInitialized()

	// 创建插件配置
	var config Config
	if pm.viper != nil {
		config = NewPluginConfig(name, pm.viper)
	}

	// P4-4: ConfigSchema 验证（仅当 config 和 schema 均不为 nil 时）
	if config != nil && desc.Advanced != nil && desc.Advanced.ConfigSchema != nil {
		if schemaErr := ValidateConfigSchema(name, desc.Advanced.ConfigSchema, config); schemaErr != nil {
			pm.mu.Unlock()
			return schemaErr
		}
	}

	// 创建插件实例
	instance := &PluginInstance{
		desc:     desc,
		state:    Unloaded,
		matchers: make([]*engine.Matcher, 0),
	}

	// 构建 SetupContext
	setupCtx := &SetupContext{
		Reg:      newLiveRegistryWriter(pm.coordinator, name, instance),
		Log:      newPluginLogger(name),
		Info:     newPluginInfo(pm),
		Config:   config,
		EventBus: pm.eventBus,
		setupContextInternal: setupContextInternal{
			container:        pm.container,
			pluginName:       name,
			instance:         instance,
			autoTrackEnabled: true,
			eng:              pm.coordinator,
		},
	}
	// Go 函数在 load() 时由 goroutineManager 注入，此处先设为 nil-safe 空实现
	setupCtx.Go = func(fn func(ctx stdctx.Context)) {
		if setupCtx.goroutineMgr != nil {
			setupCtx.goroutineMgr.go_(fn)
		}
	}

	instance.setupContext = setupCtx

	// 先添加到 plugins map，并设置为 Loading 状态
	// 这样其他 goroutine 通过 Get() 获取时可以检测到插件正在加载
	instance.state = Loading
	pm.plugins[name] = instance

	pm.mu.Unlock()

	// 加载插件（在锁外执行，避免长时间持锁）
	loadErr := instance.load(pm.coordinator)

	pm.mu.Lock()

	if loadErr != nil {
		// 加载失败，回滚
		delete(pm.plugins, name)
		pm.container.Remove(name)
		pm.mu.Unlock()

		logger.WithError(loadErr).Errorf("[pluginManager] Failed to load plugin %s", name)
		pm.notifyError(name, "load", loadErr)
		return loadErr
	}

	// 验证自动跟踪的依赖与声明的依赖是否一致
	trackedDeps := setupCtx.GetTrackedDependencies()
	if len(trackedDeps) > 0 {
		// 检查是否有未声明的依赖
		declaredDeps := make(map[string]bool)
		for _, dep := range desc.Deps {
			declaredDeps[dep] = true
		}

		undeclaredDeps := make([]string, 0)
		for _, tracked := range trackedDeps {
			if !declaredDeps[tracked] {
				undeclaredDeps = append(undeclaredDeps, tracked)
			}
		}

		if len(undeclaredDeps) > 0 {
			if pm.strictDeps {
				// 严格模式：拒绝注册，回滚
				// Setup 已执行（有副作用），需调用 unload 做清理
				delete(pm.plugins, name)
				pm.container.Remove(name)
				pm.mu.Unlock()
				// 在锁外执行 Teardown，避免死锁
				if teardownErr := instance.unload(pm.coordinator); teardownErr != nil {
					logger.WithError(teardownErr).Warnf("[pluginManager] Failed to teardown plugin %s during strict-mode rollback", name)
				}
				return fmt.Errorf(
					"plugin %q uses undeclared dependencies %v (declared: %v); "+
						"add them to Deps or disable strict mode via manager.SetStrictDeps(false)",
					name, undeclaredDeps, desc.Deps,
				)
			}
			// 宽松模式：仅警告
			logger.WithFields(logger.Fields{
				"plugin":          name,
				"undeclared_deps": undeclaredDeps,
				"declared_deps":   desc.Deps,
			}).Warn("[pluginManager] Plugin uses dependencies not declared in Deps field")
		}
	}

	// 加载成功，完成注册
	pm.loadOrder = append(pm.loadOrder, name)

	// 若插件在 Setup 中已通过 ExportAs 主动导出 API，则不覆盖容器中的值；
	// 否则以 *PluginInstance 作为默认回退（向后兼容）。
	if !pm.container.Has(name) {
		pm.container.Register(name, instance)
	}

	pm.mu.Unlock()

	logger.Infof("[pluginManager] Plugin %s registered (v2)", name)
	pm.notifyLoaded(name)
	return nil
}

// RegisterMultipleV2 批量注册多个 v2 插件，自动处理依赖顺序
//
// 此方法会：
// 1. 检测循环依赖（使用拓扑排序算法）
// 2. 按正确的依赖顺序注册插件
// 3. 如果任何插件注册失败，已注册的插件不会自动回滚
//
// 使用示例：
//
//	plugins := []*PluginDescriptor{
//	    NewPluginA(), // 依赖 B
//	    NewPluginB(), // 依赖 C
//	    NewPluginC(), // 无依赖
//	}
//	if err := manager.RegisterMultipleV2(plugins); err != nil {
//	    log.Fatal(err)
//	}
func (pm *Manager) RegisterMultipleV2(descriptors []*PluginDescriptor) error {
	if len(descriptors) == 0 {
		return nil
	}

	// 验证所有描述符
	for i, desc := range descriptors {
		if desc == nil {
			return fmt.Errorf("descriptor at index %d is nil", i)
		}
		if desc.Name == "" {
			return fmt.Errorf("descriptor at index %d has empty name", i)
		}
		if desc.Setup == nil {
			return fmt.Errorf("descriptor %s has no setup function", desc.Name)
		}
	}

	// 拓扑排序，检测循环依赖
	sorted, err := pm.topologicalSortV2(descriptors)
	if err != nil {
		return fmt.Errorf("dependency resolution failed: %w", err)
	}

	// 按依赖顺序注册
	for _, desc := range sorted {
		if err := pm.RegisterV2(desc); err != nil {
			return fmt.Errorf("failed to register plugin %s: %w", desc.Name, err)
		}
	}

	logger.Infof("[pluginManager] Successfully registered %d plugins in dependency order", len(sorted))
	return nil
}

// topologicalSortV2 使用 Kahn 算法进行拓扑排序
// 返回按依赖顺序排列的插件列表，如果存在循环依赖则返回错误
//
// 增强版本：检测批次内和跨批次的循环依赖
// RegisterMultipleV2Atomic 原子批量注册：任意插件失败时，自动回滚已注册的插件。
//
// 与 RegisterMultipleV2 的区别：
//   - RegisterMultipleV2       失败后保留已注册的插件（半初始化状态）
//   - RegisterMultipleV2Atomic 失败后逆序回滚所有已注册的插件
//
// 使用示例：
//
//	if err := manager.RegisterMultipleV2Atomic(plugins); err != nil {
//	   // 所有插件均未注册，系统处于干净状态
//	   log.Fatalf("plugin batch registration failed: %v", err)
//	}
func (pm *Manager) RegisterMultipleV2Atomic(descriptors []*PluginDescriptor) error {
	if len(descriptors) == 0 {
		return nil
	}
	for i, desc := range descriptors {
		if desc == nil {
			return fmt.Errorf("descriptor at index %d is nil", i)
		}
		if desc.Name == "" {
			return fmt.Errorf("descriptor at index %d has empty name", i)
		}
		if desc.Setup == nil {
			return fmt.Errorf("descriptor %s has no setup function", desc.Name)
		}
	}
	sorted, err := pm.topologicalSortV2(descriptors)
	if err != nil {
		return &PluginError{
			Operation: "batch register",
			Cause:     err,
			Hint:      "check for circular or missing dependencies",
		}
	}
	// 记录已成功注册的插件（用于失败时回滚）
	registered := make([]string, 0, len(sorted))
	for _, desc := range sorted {
		if err := pm.RegisterV2(desc); err != nil {
			// 注册失败，逆序回滚已注册的插件
			for i := len(registered) - 1; i >= 0; i-- {
				if rollbackErr := pm.Unregister(registered[i]); rollbackErr != nil {
					logger.WithError(rollbackErr).Warnf("[pluginManager] Rollback failed for plugin %s", registered[i])
				}
			}
			// 收集当前已注册的插件列表用于诊断
			pm.mu.RLock()
			existingNames := make([]string, 0, len(pm.plugins))
			for n := range pm.plugins {
				existingNames = append(existingNames, n)
			}
			pm.mu.RUnlock()
			return &PluginError{
				PluginName:        desc.Name,
				Operation:         "register",
				Cause:             err,
				RegisteredPlugins: existingNames,
				Hint:              "all previously registered plugins in this batch have been rolled back",
			}
		}
		registered = append(registered, desc.Name)
	}
	logger.Infof("[pluginManager] Atomic registration of %d plugins succeeded", len(sorted))
	return nil
}

func (pm *Manager) topologicalSortV2(descriptors []*PluginDescriptor) ([]*PluginDescriptor, error) {
	// 构建映射：名称 -> 描述符
	descMap := make(map[string]*PluginDescriptor)
	for _, desc := range descriptors {
		if _, exists := descMap[desc.Name]; exists {
			return nil, fmt.Errorf("duplicate plugin name: %s", desc.Name)
		}
		descMap[desc.Name] = desc
	}

	// 检查跨批次循环依赖
	if err := pm.checkCrossBatchCyclicDependency(descriptors, descMap); err != nil {
		return nil, err
	}

	// 构建依赖图和入度表
	// inDegree[name] = 依赖该插件的数量
	// graph[name] = 依赖于 name 的插件列表
	inDegree := make(map[string]int)
	graph := make(map[string][]string)

	// 初始化入度
	for name := range descMap {
		inDegree[name] = 0
		graph[name] = make([]string, 0)
	}

	// 计算入度和构建图
	for _, desc := range descriptors {
		for _, dep := range desc.Deps {
			// 检查依赖是否存在（可能已在 manager 中注册，或在当前批次中）
			pm.mu.RLock()
			depInst, existsInManager := pm.plugins[dep]
			pm.mu.RUnlock()

			_, existsInBatch := descMap[dep]

			if !existsInManager && !existsInBatch {
				return nil, fmt.Errorf("plugin %s has missing dependency: %s", desc.Name, dep)
			}

			// 验证已注册的依赖插件状态（批次外的依赖必须已 Loaded）
			if existsInManager && !existsInBatch {
				if depInst.GetState() != Loaded {
					return nil, fmt.Errorf("plugin %s dependency '%s' is not ready (state: %s)", desc.Name, dep, depInst.GetState())
				}
			}

			// 只处理批次内的依赖关系
			if existsInBatch {
				inDegree[desc.Name]++
				graph[dep] = append(graph[dep], desc.Name)
			}
		}
	}

	// Kahn 算法：拓扑排序
	queue := make([]string, 0)

	// 找出所有入度为 0 的节点（无依赖或依赖已满足）
	for name, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, name)
		}
	}

	result := make([]*PluginDescriptor, 0, len(descriptors))
	processed := 0

	for len(queue) > 0 {
		// 取出一个入度为 0 的节点
		current := queue[0]
		queue = queue[1:]

		// 添加到结果
		result = append(result, descMap[current])
		processed++

		// 减少所有依赖于 current 的节点的入度
		for _, dependent := range graph[current] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}

	// 检查是否所有节点都被处理（如果没有，说明存在循环依赖）
	if processed != len(descriptors) {
		// 找出形成循环的插件
		unprocessed := make([]string, 0)
		for name, degree := range inDegree {
			if degree > 0 {
				unprocessed = append(unprocessed, name)
			}
		}
		return nil, fmt.Errorf("circular dependency detected among plugins: %v", unprocessed)
	}

	return result, nil
}

// checkCrossBatchCyclicDependency 检查跨批次循环依赖
// 即：已注册插件和批次内插件之间是否形成循环
func (pm *Manager) checkCrossBatchCyclicDependency(descriptors []*PluginDescriptor, descMap map[string]*PluginDescriptor) error {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	// 对于批次中的每个插件
	for _, desc := range descriptors {
		// 检查它依赖的每个已注册插件
		for _, depName := range desc.Deps {
			existingInst, existsInManager := pm.plugins[depName]
			if !existsInManager {
				continue // 不是已注册插件，跳过
			}

			// 检查已注册插件是否（直接或间接）依赖批次中的插件
			if err := pm.detectCycleThroughExisting(existingInst, desc.Name, descMap, make(map[string]bool)); err != nil {
				return fmt.Errorf("cross-batch circular dependency: %w", err)
			}
		}
	}

	return nil
}

// detectCycleThroughExisting 检测已注册插件是否依赖批次中的插件（可能形成循环）
// existingInst: 已注册的插件实例
// targetName: 批次中的插件名称
// batchPlugins: 批次中的插件映射
// visited: 已访问的插件集合（防止无限递归）
func (pm *Manager) detectCycleThroughExisting(existingInst *PluginInstance, targetName string, batchPlugins map[string]*PluginDescriptor, visited map[string]bool) error {
	pluginName := existingInst.Name()

	// 防止无限递归
	if visited[pluginName] {
		return nil
	}
	visited[pluginName] = true

	// 获取已注册插件的依赖
	deps := existingInst.dependencies()

	for _, dep := range deps {
		// 如果已注册插件依赖批次中的插件，形成循环
		if dep == targetName {
			return fmt.Errorf("plugin %s (registered) depends on %s (in batch), which depends on %s",
				pluginName, dep, pluginName)
		}

		// 如果依赖是批次中的其他插件
		if batchDesc, inBatch := batchPlugins[dep]; inBatch {
			if pm.batchPluginDependsOn(batchDesc, targetName, batchPlugins, make(map[string]bool)) {
				return fmt.Errorf("plugin %s (registered) -> %s (batch) -> %s (batch) forms a cycle",
					pluginName, dep, targetName)
			}
		}

		// 如果依赖是另一个已注册插件，递归检查
		if depInst, exists := pm.plugins[dep]; exists {
			if err := pm.detectCycleThroughExisting(depInst, targetName, batchPlugins, visited); err != nil {
				return err
			}
		}
	}

	return nil
}

// batchPluginDependsOn 检查批次中的插件是否（直接或间接）依赖目标插件
func (pm *Manager) batchPluginDependsOn(plugin *PluginDescriptor, targetName string, batchPlugins map[string]*PluginDescriptor, visited map[string]bool) bool {
	// 防止无限递归
	if visited[plugin.Name] {
		return false
	}
	visited[plugin.Name] = true

	// 检查直接依赖
	for _, dep := range plugin.Deps {
		if dep == targetName {
			return true
		}

		// 检查间接依赖（只在批次内）
		if depDesc, inBatch := batchPlugins[dep]; inBatch {
			if pm.batchPluginDependsOn(depDesc, targetName, batchPlugins, visited) {
				return true
			}
		}
	}

	return false
}

// ValidateDependencies 验证一组插件的依赖关系（不注册）
// 返回错误如果存在循环依赖或缺失依赖
//
// 使用示例：
//
//	if err := manager.ValidateDependencies(plugins); err != nil {
//	    log.Printf("Dependency validation failed: %v", err)
//	}
func (pm *Manager) ValidateDependencies(descriptors []*PluginDescriptor) error {
	_, err := pm.topologicalSortV2(descriptors)
	return err
}

// ensureContainerInitialized 确保依赖注入容器已初始化
// 此方法应该在持有 Manager 锁的情况下调用
func (pm *Manager) ensureContainerInitialized() {
	if pm.container == nil {
		pm.container = NewContainer()
	}

	// 注册已存在的插件到容器（只在容器中没有该 key 时注入，保护 ExportAs 的值）
	for pluginName, plugin := range pm.plugins {
		if !pm.container.Has(pluginName) {
			pm.container.Register(pluginName, plugin)
		}
	}

	// 注册特殊服务（只在不存在时注册，避免重复）
	if !pm.container.Has("manager") {
		pm.container.Register("manager", pm)
	}
	if !pm.container.Has("engine") {
		pm.container.Register("engine", pm.coordinator)
	}
	if !pm.container.Has("coordinator") {
		pm.container.Register("coordinator", pm.coordinator)
	}
}

// RegisterMultipleV2Smart 智能批量注册插件（自动推断依赖关系）
//
// 此方法会：
// 1. 首次尝试注册所有插件以收集依赖信息
// 2. 根据实际使用的依赖关系进行拓扑排序
// 3. 按正确顺序重新注册所有插件
//
// 优势：
//   - 不需要手动声明 Deps 字段
//   - 自动跟踪 Setup 函数中的依赖调用
//   - 自动检测循环依赖
//
// 限制：
//   - 插件的 Setup 函数必须能够多次调用而无副作用（幂等性）
//   - 或者使用 DryRun 模式（需要插件支持）
//
// 使用示例：
//
//	plugins := []*PluginDescriptor{
//	    {Name: "auth", Setup: func(ctx *SetupContext) error {
//	        // 无依赖
//	        return nil
//	    }},
//	    {Name: "permission", Setup: func(ctx *SetupContext) error {
//	        auth := ctx.MustGet("auth") // 自动检测依赖 auth
//	        return nil
//	    }},
//	}
//	// 不需要手动声明 Deps!
//	if err := manager.RegisterMultipleV2Smart(plugins); err != nil {
//	    log.Fatal(err)
//	}
func (pm *Manager) RegisterMultipleV2Smart(descriptors []*PluginDescriptor) error {
	if len(descriptors) == 0 {
		return nil
	}

	// 验证所有描述符
	for i, desc := range descriptors {
		if desc == nil {
			return fmt.Errorf("descriptor at index %d is nil", i)
		}
		if desc.Name == "" {
			return fmt.Errorf("descriptor at index %d has empty name", i)
		}
		if desc.Setup == nil {
			return fmt.Errorf("descriptor %s has no setup function", desc.Name)
		}
	}

	// 阶段1：推断依赖关系
	logger.Info("[pluginManager] Smart registration: inferring dependencies...")

	inferredDeps := make(map[string][]string)
	descMap := make(map[string]*PluginDescriptor)

	for _, desc := range descriptors {
		descMap[desc.Name] = desc
	}

	// 创建临时容器用于依赖推断
	tempContainer := NewContainer()

	// 添加已存在的插件到临时容器
	pm.mu.RLock()
	for name, plugin := range pm.plugins {
		tempContainer.Register(name, plugin)
	}
	pm.mu.RUnlock()

	// 添加所有待注册插件的占位符到临时容器
	for _, desc := range descriptors {
		tempContainer.Register(desc.Name, &PluginInstance{desc: desc})
	}

	// 为每个插件推断依赖
	for _, desc := range descriptors {
		setupCtx := &SetupContext{
			// 依赖推断阶段：注入 no-op RegistryWriter，所有注册操作无副作用
			Reg:  &noopRegistryWriter{},
			Log:  newPluginLogger(desc.Name),
			Info: newPluginInfo(pm),
			setupContextInternal: setupContextInternal{
				container:        tempContainer,
				pluginName:       desc.Name,
				autoTrackEnabled: true,
			},
		}
		setupCtx.Go = func(fn func(ctx stdctx.Context)) { /* 推断阶段：no-op */ }

		// 调用 Setup 跟踪 Get/MustGet 访问了哪些依赖（忽略错误和 panic）
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.WithFields(logger.Fields{
						"plugin": desc.Name,
						"panic":  r,
					}).Debug("[pluginManager] Setup panicked during dependency inference (expected)")
				}
			}()

			// 忽略错误，我们只关心依赖跟踪
			_, _ = desc.callSetup(setupCtx)
		}()

		// 获取跟踪到的依赖
		tracked := setupCtx.GetTrackedDependencies()
		if len(tracked) > 0 {
			inferredDeps[desc.Name] = tracked
			logger.WithFields(logger.Fields{
				"plugin": desc.Name,
				"deps":   tracked,
			}).Debug("[pluginManager] Inferred dependencies")
		}
	}

	// 阶段2：使用推断的依赖进行拓扑排序
	logger.Info("[pluginManager] Smart registration: sorting by dependencies...")

	// 创建带有推断依赖的描述符副本
	descriptorsWithDeps := make([]*PluginDescriptor, len(descriptors))
	for i, desc := range descriptors {
		descCopy := *desc // 浅拷贝

		// 合并声明的依赖和推断的依赖
		depsMap := make(map[string]bool)
		for _, dep := range desc.Deps {
			depsMap[dep] = true
		}
		for _, dep := range inferredDeps[desc.Name] {
			depsMap[dep] = true
		}

		mergedDeps := make([]string, 0, len(depsMap))
		for dep := range depsMap {
			mergedDeps = append(mergedDeps, dep)
		}

		descCopy.Deps = mergedDeps
		descriptorsWithDeps[i] = &descCopy
	}

	// 使用现有的 RegisterMultipleV2 进行注册
	return pm.RegisterMultipleV2(descriptorsWithDeps)
}
