package plugin

import (
	stdctx "context"
	"fmt"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	corectx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
)

// engineMWRegistrar 是可选的引擎中间件注册接口。
// *engine.Engine 实现此接口，允许 Privileged 插件向引擎注册/清除全局/分组中间件。
// 使用接口而非具体类型，避免 plugin 包与 engine 包的紧耦合，且对 mock 友好。
type engineMWRegistrar interface {
	Use(mw ...corectx.Middleware) *engine.Engine
	UseForGroup(groupName string, mw ...corectx.Middleware) *engine.Engine
	// ResetGroupMiddleware 清除指定分组的中间件链。
	// 配合 UseForGroup 实现幂等写：先 Reset 再 UseForGroup，防止重复追加。
	ResetGroupMiddleware(groupName string) *engine.Engine
}

// context.go — Setup/Teardown 上下文及类型安全依赖获取

// setupContextInternal 框架内部字段（外部 godoc 不可见）
type setupContextInternal struct {
	container           *Container
	pluginName          string
	instance            *Instance
	trackedDeps         map[string]bool // 必要依赖（Get 成功 + MustGet）
	trackedOptionalDeps map[string]bool // 可选依赖（Get 成功但通过 ok 判断）
	autoTrackEnabled    bool
	goroutineMgr        *goroutineManager
	eng                 registryBackend // 注册 Matcher 的 engine（reload 时复用，同时满足 MatcherWriter + Reader）

	// 资源追踪：Scope 级联清理
	rootScope *Scope // 插件根 Scope，unload 时自动 Dispose

	// RegisterCron 懒初始化字段（framework #31）
	cronInitOnce  sync.Once
	cronScheduler *cron.Cron
}

// SetupContext 插件 Setup 阶段的上下文。
//
//   - [SetupContext.Reg]      — Matcher/Command 注册（自动追踪）
//   - [SetupContext.Log]      — 带插件名前缀的结构化日志
//   - [SetupContext.Info]     — 插件系统只读视图
//   - [SetupContext.Admin]    — 插件系统管理视图（仅 Privileged 插件可用）
//   - [SetupContext.DryRun]   — 是否处于 Smart 依赖推断阶段
//   - [SetupContext.Go]       — 生命周期绑定后台 goroutine
//   - [SetupContext.Config]   — 插件配置
//   - [SetupContext.EventBus] — 插件间事件总线（发布用；订阅请用 [SetupContext.Scope]）
//   - [SetupContext.Scope]    — 资源 Scope（订阅自动清理、级联销毁）
//   - [Service] / [TryService] — 获取依赖（防过期代理，推荐）
type SetupContext struct {
	// Reg Matcher/Command 注册接口，DryRun 阶段自动变为 no-op。
	Reg RegistryWriter

	// Log 带插件名前缀的结构化日志器。
	Log Logger

	// Info 插件系统只读视图，可查询其他插件状态和 engine 命令列表。
	Info Info

	// Admin 插件系统管理视图（仅 Privileged: true 的插件可用）。
	// 未声明 Privileged 的插件此字段为 nil；误用会在运行时立即 panic。
	Admin ManagerWriter

	// DryRun 标识当前 Setup 调用是否处于 RegisterMultipleSmart 的依赖推断阶段。
	//
	// 推断阶段框架会多次调用 Setup 以分析依赖关系。此时：
	//   - ctx.Reg 已替换为 no-op（不会注册真实 Matcher）
	//   - ctx.EventBus 已替换为 no-op（不会注册真实订阅）
	//   - ctx.Go / ctx.GoNamed 自动退化为 no-op（goroutineMgr 未初始化）
	//
	// 对于大多数插件，无需检查此字段——框架已通过 no-op 替换消除了常见副作用。
	// 仅当 Setup 中存在以下情况时才需要判断：
	//   - 调用外部 HTTP/DB 请求（网络 I/O）
	//   - 写入进程级全局变量
	//   - 其他无法通过 no-op 消除的副作用
	//
	//	Setup: func(ctx *plugin.SetupContext) (any, error) {
	//	    if !ctx.DryRun {
	//	        p.metrics = initMetrics() // 仅在真实运行时初始化
	//	    }
	//	    return p, nil
	//	},
	DryRun bool

	// Config 插件配置
	Config Config

	// EventBus 插件间事件总线。DryRun 阶段替换为 no-op，订阅操作不产生真实副作用。
	//
	// 发布事件：直接使用 ctx.EventBus.Publish(topic, data)
	// 订阅事件：请使用 ctx.Scope().Subscribe(topic, handler) 代替 ctx.EventBus.Subscribe，
	// Scope 会自动追踪订阅并在插件卸载时取消，避免忘记 Unsubscribe 导致的内存泄漏。
	EventBus EventBus

	// 内部字段（框架使用，外部不可访问）
	setupContextInternal
}

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

// UseEngineGlobal 为所有 Matcher 注册全局中间件（跨插件生效）。
//
// 仅供声明了 Privileged: true 的插件使用，用于注册跨切面守卫
// （如全局封禁检查、群静默检查）。DryRun 阶段自动为 no-op。
//
// 注意：全局中间件对每个 Matcher 均生效，包括基础设施插件的 Matcher。
// 若只需对特定插件生效，请使用 UseEngineForGroup。
func (ctx *SetupContext) UseEngineGlobal(mw ...corectx.Middleware) {
	if ctx.DryRun || ctx.eng == nil || len(mw) == 0 {
		return
	}
	if e, ok := ctx.eng.(engineMWRegistrar); ok {
		e.Use(mw...)
	}
}

// UseEngineForGroup 为指定分组（即某个插件名）的所有 Matcher 注册中间件。
//
// 仅供声明了 Privileged: true 的插件使用，用于自动注入 per-plugin 访问管控守卫。
// DryRun 阶段自动为 no-op。
//
// group 通常为目标插件的名称，与 Descriptor.Name 一致。
func (ctx *SetupContext) UseEngineForGroup(group string, mw ...corectx.Middleware) {
	if ctx.DryRun || ctx.eng == nil || group == "" || len(mw) == 0 {
		return
	}
	if e, ok := ctx.eng.(engineMWRegistrar); ok {
		e.UseForGroup(group, mw...)
	}
}

// NewGroupMiddlewareApplier 返回一个可在 Setup 完成后调用的分组中间件注册函数。
//
// 返回的函数封装了对 engine.UseForGroup 的调用，供 LifecycleListener 在新插件
// 加载时自动注入分组守卫，无需在 pluginctrl 之外暴露 engine 引用。
//
// DryRun 或引擎不支持时返回 no-op 函数。
func (ctx *SetupContext) NewGroupMiddlewareApplier() func(group string, mw ...corectx.Middleware) {
	e, ok := ctx.eng.(engineMWRegistrar)
	if !ok {
		return func(_ string, _ ...corectx.Middleware) {} // no-op：测试/DryRun 场景
	}
	return func(group string, mw ...corectx.Middleware) {
		if group != "" && len(mw) > 0 {
			e.UseForGroup(group, mw...)
		}
	}
}

// NewGroupMiddlewareResetter 返回一个可在 Setup 完成后调用的分组中间件清除函数。
//
// 返回的函数封装了对 engine.ResetGroupMiddleware 的调用，供 LifecycleListener 使用：
//   - OnPluginLoaded：先 Reset 再 wire，保证幂等（避免 unload+re-register 双重守卫）
//   - OnPluginUnloaded：调用 Reset 清除 phantom 中间件条目，防止内存持续累积
//
// DryRun 或引擎不支持时返回 no-op 函数。
func (ctx *SetupContext) NewGroupMiddlewareResetter() func(group string) {
	e, ok := ctx.eng.(engineMWRegistrar)
	if !ok {
		return func(_ string) {} // no-op
	}
	return func(group string) {
		if group != "" {
			e.ResetGroupMiddleware(group)
		}
	}
}

// Go 启动一个与插件生命周期绑定的匿名后台 goroutine。
// 框架在 Teardown 前自动 cancel 传入的 context 并等待所有 goroutine 退出。
// DryRun 阶段（goroutineMgr 为 nil）自动退化为 no-op，不会启动 goroutine。
//
//	ctx.Go(func(runCtx context.Context) {
//	    ticker := time.NewTicker(time.Minute)
//	    defer ticker.Stop()
//	    for {
//	        select {
//	        case <-ticker.C: cleanup()
//	        case <-runCtx.Done(): return
//	        }
//	    }
//	})
func (ctx *SetupContext) Go(fn func(stdctx.Context)) {
	if ctx.goroutineMgr != nil {
		ctx.goroutineMgr.go_(fn)
	}
}

// GoNamed 启动一个带名称标签的生命周期绑定后台 goroutine。
// 名称用于调试时区分不同插件的后台任务（可通过 Manager.ListPluginGoroutines 查询）。
// DryRun 阶段（goroutineMgr 为 nil）自动退化为 no-op，不会启动 goroutine。
//
//	ctx.GoNamed("cleanup-gc", func(runCtx context.Context) { ... })
func (ctx *SetupContext) GoNamed(name string, fn func(stdctx.Context)) {
	if ctx.goroutineMgr != nil {
		ctx.goroutineMgr.goNamed_(name, fn)
	}
}

// After 注册一个一次性延迟任务（框架修复 #35）。
//
// d 之后调用 fn；若插件在 d 到期前被 Teardown，fn 被静默取消，不会调用。
// name 用于调试标识（与 GoNamed 的 name 语义相同）。
// DryRun 阶段（goroutineMgr 为 nil）自动退化为 no-op。
//
// 适用于"N分钟后执行一次"的场景（如定时提醒、倒计时），
// 区别于 RegisterCron（重复执行）。
//
// 示例：
//
//	ctx.After("remind:"+userID, 5*time.Minute, func() {
//	    sender.NotifyUser(ctx, userID, platform.TextMessage("提醒时间到！"))
//	})
func (ctx *SetupContext) After(name string, d time.Duration, fn func()) {
	if ctx.goroutineMgr == nil {
		return
	}
	ctx.goroutineMgr.goNamed_(name, func(runCtx stdctx.Context) {
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-t.C:
			fn()
		case <-runCtx.Done():
			// 插件 Teardown，静默取消
		}
	})
}

// RegisterCron 在插件 Setup 阶段注册生命周期绑定的 cron 定时任务。
//
// expr 为 cron 表达式，支持 6 段含秒格式（秒 分 时 日 月 周）
// 和 5 段标准格式（分 时 日 月 周），由 robfig/cron/v3 解析。
// fn 在每次触发时调用；插件 Teardown（goroutineManager 被 cancel）时 cron 自动停止。
//
// DryRun 阶段（goroutineMgr 为 nil）自动退化为 no-op，不启动任何任务。
// 同一插件多次调用 RegisterCron 共享同一个 cron scheduler 实例（懒初始化）。
//
// 插件在 Setup 中直接注册内置定时任务，无需依赖外部 scheduler 插件，
// 实现插件生命周期完全自包含（创建即启动，Teardown 自动停止）。
//
//	Setup: func(ctx *plugin.SetupContext) (any, error) {
//	    // 每天凌晨2点清理过期数据
//	    if err := ctx.RegisterCron("0 2 * * *", func() {
//	        cleanup()
//	    }); err != nil {
//	        ctx.Log.Warnf("RegisterCron failed: %v", err)
//	    }
//	    return nil, nil
//	},
func (ctx *SetupContext) RegisterCron(expr string, fn func()) error {
	if ctx.goroutineMgr == nil {
		// DryRun 阶段或 goroutineMgr 未初始化，no-op
		return nil
	}

	// 懒初始化：首次 RegisterCron 调用时创建 cron scheduler 并绑定生命周期
	ctx.cronInitOnce.Do(func() {
		c := cron.New(cron.WithSeconds())
		c.Start()
		ctx.cronScheduler = c
		// 绑定插件生命周期：goroutineMgr cancel 时停止 cron
		ctx.goroutineMgr.goNamed_("plugin-cron:"+ctx.pluginName, func(runCtx stdctx.Context) {
			<-runCtx.Done()
			stopCtx := ctx.cronScheduler.Stop()
			<-stopCtx.Done()
		})
	})

	if _, err := ctx.cronScheduler.AddFunc(expr, fn); err != nil {
		return fmt.Errorf("plugin %q: RegisterCron invalid expr %q: %w", ctx.pluginName, expr, err)
	}
	return nil
}

// 未来展望：待 Go 支持泛型方法后，可改为 ctx.get[permission.Plugin]("permission") 的调用方式。

// Get 获取依赖插件（弱类型，内部使用）。
// 自动记录依赖关系，用于 Smart 注册的依赖推断。
// 插件开发者应使用 [Service] / [TryService] 代替。
func (ctx *SetupContext) Get(name string) (any, bool) {
	if ctx.container == nil {
		return nil, false
	}
	v, ok := ctx.container.Get(name)
	if ok && ctx.autoTrackEnabled && name != "" && name != ctx.pluginName {
		// 找到时才追踪，且标记为可选（不影响 UnregisterCascade 级联语义）
		if ctx.trackedOptionalDeps == nil {
			ctx.trackedOptionalDeps = make(map[string]bool)
		}
		ctx.trackedOptionalDeps[name] = true
	}
	return v, ok
}

// MustGet 获取依赖插件（弱类型，不存在则 panic，内部使用）。
// 插件开发者应使用 [Service] 代替。
func (ctx *SetupContext) MustGet(name string) any {
	v, ok := ctx.container.Get(name)
	if !ok {
		panic(fmt.Sprintf("plugin %q: required dependency %q not found", ctx.pluginName, name))
	}
	// 必要依赖追踪
	if ctx.autoTrackEnabled && name != "" && name != ctx.pluginName {
		if ctx.trackedDeps == nil {
			ctx.trackedDeps = make(map[string]bool)
		}
		ctx.trackedDeps[name] = true
	}
	return v
}

// GetTrackedDependencies 获取自动追踪到的必要依赖列表（框架内部使用）
//
// 必要依赖：通过 MustGet / Must 访问的依赖，合并到 instance.desc.Deps 中，
// 影响 notifyDependents 和 UnregisterCascade 的行为。
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

// GetTrackedOptionalDependencies 获取自动追踪到的可选依赖列表（框架内部使用）
//
// 可选依赖：通过 Get（有 ok 判断）访问且存在的依赖。
// 用于 RegisterMultipleSmart 的依赖推断（拓扑排序），
// 但不影响 notifyDependents 和 UnregisterCascade。
func (ctx *SetupContext) GetTrackedOptionalDependencies() []string {
	if ctx.trackedOptionalDeps == nil {
		return []string{}
	}
	deps := make([]string, 0, len(ctx.trackedOptionalDeps))
	for name := range ctx.trackedOptionalDeps {
		deps = append(deps, name)
	}
	return deps
}

// --- 类型安全依赖获取 ---

// GetPlugin 获取依赖插件（类型安全，返回 (*T, error)）
//
// 追踪语义：标记为必要依赖（与 Must 相同，区别仅在于错误处理方式）。
//
//	p, err := plugin.GetPlugin[permission.Plugin](ctx, "permission")
func GetPlugin[T any](ctx *SetupContext, name string) (*T, error) {
	// 先查容器，成功后追踪为必要依赖
	v, ok := ctx.container.Get(name)
	if !ok {
		return nil, fmt.Errorf("plugin %q: dependency %q not found", ctx.pluginName, name)
	}
	if ctx.autoTrackEnabled && name != "" && name != ctx.pluginName {
		if ctx.trackedDeps == nil {
			ctx.trackedDeps = make(map[string]bool)
		}
		ctx.trackedDeps[name] = true
	}
	typed, ok := v.(*T)
	if !ok {
		return nil, fmt.Errorf("plugin %q: dependency %q has wrong type: expected *%T, got %T", ctx.pluginName, name, typed, v)
	}
	return typed, nil
}

// MustAs 以接口类型获取必需依赖（面向接口，符合依赖倒置原则）。
// 若依赖不存在或类型不满足 T 接口，则 panic。
//
// Deprecated: 使用 [Service] 代替。MustAs 返回的对象在依赖热重载后会变成过期引用。
func MustAs[T any](ctx *SetupContext, name string) T {
	v := ctx.MustGet(name)
	typed, ok := v.(T)
	if !ok {
		var zero T
		panic(fmt.Sprintf("plugin %q: dependency %q does not implement %T (got %T)", ctx.pluginName, name, zero, v))
	}
	return typed
}

// TryAs 以接口类型获取可选依赖（面向接口，符合依赖倒置原则）。
//
// Deprecated: 使用 [TryService] 代替。TryAs 返回的对象在依赖热重载后会变成过期引用。
func TryAs[T any](ctx *SetupContext, name string) (T, bool) {
	v, ok := ctx.Get(name)
	if !ok {
		var zero T
		return zero, false
	}
	typed, ok := v.(T)
	if !ok {
		var zero T
		return zero, false
	}
	return typed, true
}

// ExportIface 将插件 API 以接口类型额外导出到容器。
//
// 配合 [MustAs] / [TryAs] 使用，让消费者可以通过接口而非具体类型访问依赖。
// 通常在 Setup 末尾配合主 key 一起使用：
//
//	Setup: func(ctx *plugin.SetupContext) (any, error) {
//	    p := NewPlugin()
//	    // 主 key 导出具体类型（return p 自动以插件名注册）
//	    // 额外以接口类型导出，供依赖接口的消费者使用
//	    plugin.ExportIface[storage.Client](ctx, "storage.Client", p)
//	    plugin.ExportIface[storage.Storage](ctx, "storage.Storage", p)
//	    return p, nil
//	},
//
//	// 消费者：面向接口，不依赖具体实现
//	client := plugin.MustAs[storage.Client](ctx, "storage.Client")
func ExportIface[T any](ctx *SetupContext, key string, impl T) {
	ctx.ExportAs(key, impl)
}

// --- 资源追踪：Scope / Subscribe / OnDispose ---

// Scope 返回插件的根资源 Scope。所有通过此 Scope 创建的订阅、中间件、
// 清理钩子都会在插件卸载时自动级联清理。
//
//	root := ctx.Scope()
//	root.Subscribe("plugin.loaded", func(data any) { ... })
//	root.OnDispose(func() error { return cleanup() })
func (ctx *SetupContext) Scope() *Scope {
	if ctx.rootScope == nil {
		ctx.rootScope = &Scope{
			name: ctx.pluginName + ":root",
			ctx:  ctx,
		}
	}
	return ctx.rootScope
}

// Subscribe 在 EventBus 上订阅，生命周期绑定到当前插件根 Scope。
// 插件卸载时自动 Unsubscribe，无需在 Teardown 中手动取消。
func (ctx *SetupContext) Subscribe(topic string, handler EventHandler) (Subscription, error) {
	return ctx.Scope().Subscribe(topic, handler)
}

// SubscribeAll 在 EventBus 上订阅所有事件，生命周期绑定到当前插件根 Scope。
func (ctx *SetupContext) SubscribeAll(handler EventHandler) (Subscription, error) {
	return ctx.Scope().SubscribeAll(handler)
}

// OnDispose 注册清理回调。插件卸载时逆序调用所有注册的回调。
// 发生在 Teardown 之前，Matcher 已从 Engine 移除之后。
func (ctx *SetupContext) OnDispose(fn func() error) {
	ctx.Scope().OnDispose(fn)
}
