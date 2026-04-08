package plugin

import (
	stdctx "context"
	"fmt"

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
	eng                 engine.PluginCoordinator // 注册 Matcher 的 engine（reload 时复用）
}

// SetupContext 插件 Setup 阶段的上下文。
//
//   - [SetupContext.Reg]      — Matcher/Command 注册（自动追踪）
//   - [SetupContext.Log]      — 带插件名前缀的结构化日志
//   - [SetupContext.Info]     — 插件系统只读视图
//   - [SetupContext.Admin]    — 插件系统管理视图（仅 Privileged 插件可用）
//   - [SetupContext.DryRun]   — 是否处于 Smart 依赖推断阶段（推断阶段不应产生副作用）
//   - [SetupContext.Go]       — 生命周期绑定后台 goroutine
//   - [SetupContext.Config]   — 插件配置
//   - [SetupContext.EventBus] — 插件间事件总线
//   - [SetupContext.Get] / [SetupContext.MustGet] — 获取依赖（弱类型）
//   - [Must] / [Try]          — 获取依赖（类型安全，推荐）
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

// Get 获取依赖插件（弱类型）
// 自动记录依赖关系，用于 Smart 注册的依赖推断。
// 推荐改用类型安全的 [Must] / [Try]。
//
// 追踪语义：只有在容器中找到该依赖时才追踪，且标记为可选依赖。
// 若需要声明必要依赖，使用 MustGet / Must。
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

// MustGet 获取依赖插件（弱类型，不存在则 panic）
// 推荐改用类型安全的 [Must]。
//
// 追踪语义：标记为必要依赖，影响 notifyDependents 和 UnregisterCascade。
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

// Must 获取必需依赖（类型安全，不存在或类型不符则 panic）
//
//	perm := plugin.Must[permission.Plugin](ctx, "permission")
func Must[T any](ctx *SetupContext, name string) *T {
	// 使用 MustGet 路径，确保被追踪为必要依赖
	v := ctx.MustGet(name)
	typed, ok := v.(*T)
	if !ok {
		panic(fmt.Sprintf("plugin %q: dependency %q has wrong type: expected *%T, got %T", ctx.pluginName, name, typed, v))
	}
	return typed
}

// Try 获取可选依赖（类型安全，不存在时返回 nil, false）
//
//	if sb, ok := plugin.Try[storage.Plugin](ctx, "storage"); ok { p.storage = sb }
func Try[T any](ctx *SetupContext, name string) (*T, bool) {
	// 使用 Get 路径，追踪为可选依赖
	v, ok := ctx.Get(name)
	if !ok {
		return nil, false
	}
	typed, ok := v.(*T)
	if !ok {
		return nil, false
	}
	return typed, ok
}

// MustAs 以接口类型获取必需依赖（面向接口，符合依赖倒置原则）。
//
// 与 [Must] 的区别：Must 返回 *T（具体指针类型），MustAs 返回 T（通常为接口类型），
// 允许消费者只依赖接口契约而非具体实现。
//
//	// 仅依赖 storage.Client 接口，不依赖 *storage.Plugin 具体类型
//	client := plugin.MustAs[storage.Client](ctx, "storage")
//	client.Get("key")
//
// 若依赖不存在或类型不满足 T 接口，则 panic（带明确错误信息）。
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
// 与 [Try] 的区别：Try 返回 *T（具体指针类型），TryAs 返回 T（通常为接口类型）。
// 不存在时返回零值和 false；类型不满足时同样返回零值和 false。
//
//	// 可选接口依赖
//	if client, ok := plugin.TryAs[storage.Client](ctx, "storage"); ok {
//	    p.storage = client
//	}
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

// ExportInterface 将插件 API 以接口类型额外导出到容器。
//
// 配合 [MustAs] / [TryAs] 使用，让消费者可以通过接口而非具体类型访问依赖。
// 通常在 Setup 末尾配合主 key 一起使用：
//
//	Setup: func(ctx *plugin.SetupContext) (any, error) {
//	    p := NewPlugin()
//	    // 主 key 导出具体类型（return p 自动以插件名注册）
//	    // 额外以接口类型导出，供依赖接口的消费者使用
//	    plugin.ExportInterface[storage.Client](ctx, "storage.Client", p)
//	    plugin.ExportInterface[storage.Storage](ctx, "storage.Storage", p)
//	    return p, nil
//	},
//
//	// 消费者：面向接口，不依赖具体实现
//	client := plugin.MustAs[storage.Client](ctx, "storage.Client")
func ExportInterface[T any](ctx *SetupContext, key string, impl T) {
	ctx.ExportAs(key, impl)
}
