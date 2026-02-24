package plugin

import (
	stdctx "context"
	"fmt"

	"github.com/KomeiDiSanXian/remilia/core/engine"
)

// context.go — Setup/Teardown 上下文及类型安全依赖获取

// setupContextInternal 框架内部字段（外部 godoc 不可见）
type setupContextInternal struct {
	container           *Container
	pluginName          string
	instance            *PluginInstance
	trackedDeps         map[string]bool // 必要依赖（Get 成功 + MustGet）
	trackedOptionalDeps map[string]bool // 可选依赖（Get 成功但通过 ok 判断）
	autoTrackEnabled    bool
	goroutineMgr        *goroutineManager
	eng                 *engine.Engine // 注册 Matcher 的 engine（reload 时复用）
}

// SetupContext 插件 Setup 阶段的上下文。
//
//   - [SetupContext.Reg]      — Matcher/Command 注册（自动追踪）
//   - [SetupContext.Log]      — 带插件名前缀的结构化日志
//   - [SetupContext.Info]     — 插件系统只读视图
//   - [SetupContext.Admin]    — 插件系统管理视图（仅 Privileged 插件可用）
//   - [SetupContext.Go]       — 生命周期绑定后台 goroutine
//   - [SetupContext.Config]   — 插件配置
//   - [SetupContext.EventBus] — 插件间事件总线
//   - [SetupContext.Get] / [SetupContext.MustGet] — 获取依赖（弱类型）
//   - [Must] / [Try]          — 获取依赖（类型安全，推荐）
type SetupContext struct {
	// Reg Matcher/Command 注册接口，DryRun 阶段自动变为 no-op。
	Reg RegistryWriter

	// Log 带插件名前缀的结构化日志器。
	Log PluginLogger

	// Info 插件系统只读视图，可查询其他插件状态和 engine 命令列表。
	Info PluginInfo

	// Admin 插件系统管理视图（仅 Privileged: true 的插件可用）。
	// 未声明 Privileged 的插件此字段为 nil；误用会在运行时立即 panic。
	Admin ManagerWriter

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
// 用于 RegisterMultipleV2Smart 的依赖推断（拓扑排序），
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

// Require 获取必需依赖（类型安全，不存在或类型不符则 panic）
//
// Deprecated: 使用 Must 替代。
//
//	perm := plugin.Require[permission.Plugin](ctx, "permission")
func Require[T any](ctx *SetupContext, name string) *T {
	return Must[T](ctx, name)
}

// Optional 获取可选依赖（类型安全，不存在时返回 nil, false）
//
// Deprecated: 使用 Try 替代。
//
//	if sb, ok := plugin.Optional[storage.Plugin](ctx, "storage"); ok { p.storage = sb }
func Optional[T any](ctx *SetupContext, name string) (*T, bool) {
	return Try[T](ctx, name)
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
