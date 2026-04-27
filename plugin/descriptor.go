package plugin

import "fmt"

// descriptor.go — 插件描述符及相关类型定义
//
// 包含：
//   - ReloadStrategy / Advanced / TeardownContext / 函数类型别名
//   - Descriptor 及其辅助方法

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

// Advanced 插件高级选项（可选）
//
// 热重载、状态迁移、依赖回调等高级功能。仅在需要时填写，减少普通插件的复杂度。
type Advanced struct {
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

// TeardownContext Teardown 阶段的上下文
//
// 提供 Teardown 阶段合理可用的资源，替代旧的无参数 `func() error` 闭包模式。
//
// 示例：
//
//	Teardown: func(ctx *plugin.TeardownContext) error {
//	    ctx.API.(*MyPlugin).Save()
//	    ctx.Log.Info("plugin stopped")
//	    // 条件性清理：若依赖的存储插件仍在运行才执行持久化
//	    if ctx.Info != nil && ctx.Info.IsLoaded("storage") {
//	        ctx.API.(*MyPlugin).PersistData()
//	    }
//	    return nil
//	},
type TeardownContext struct {
	// API 是 Setup 返回的插件 API 对象
	API any

	// Config 插件配置
	Config Config

	// EventBus 插件间事件总线
	EventBus EventBus

	// Log 带插件名前缀的日志器
	Log Logger

	// Info 插件系统只读视图（可能为 nil，使用前需判断）。
	// 可用于 Teardown 时查询兄弟插件状态、决定是否执行条件性清理。
	Info Info
}

// ReloadFunc 插件热重载函数
// 如果不实现，将使用默认的 Teardown + Setup 策略
type ReloadFunc = func(ctx *SetupContext) error

// SaveStateFunc 插件状态保存函数
// 在热重载前保存插件状态（可选），返回的状态数据将传递给 RestoreStateFunc
type SaveStateFunc = func() (any, error)

// RestoreStateFunc 插件状态恢复函数
// 在热重载后恢复插件状态（可选），接收 SaveStateFunc 返回的状态数据
type RestoreStateFunc = func(state any) error

// Descriptor 插件描述符
//
// # 最简用法（仅需 Name + Setup）
//
//	&plugin.Descriptor{
//	    Name:  "myplugin",
//	    Setup: func(ctx *plugin.SetupContext) (any, error) {
//	        p := NewPlugin()
//	        return p, nil  // 框架自动导出到容器
//	    },
//	}
//
// # 完整用法（含元数据和高级选项）
//
//	&plugin.Descriptor{
//	    Name:    "myplugin",
//	    Version: "1.0.0",
//	    Deps:    []string{"storage"},
//	    Meta: &plugin.Meta{
//	        Author:      "Team",
//	        Description: "My plugin",
//	        Category:    "core",
//	    },
//	    Setup:    func(ctx *plugin.SetupContext) (any, error) { ... },
//	    Teardown: func(ctx *plugin.TeardownContext) error { ... },
//	}
type Descriptor struct {
	// Name 插件名称（必需，全局唯一）
	Name string

	// Version 版本号（建议填写，格式：semver）
	Version string

	// Deps 依赖的插件名称列表（必须依赖）
	// 框架保证 Deps 中的插件在本插件 Setup 前已完成加载。
	// Deps 中列出的插件必须已注册且处于 Loaded 状态，否则注册失败。
	// 适用于通过 plugin.Must / ctx.MustGet 访问的强依赖。
	Deps []string

	// OptionalDeps 可选依赖的插件名称列表（仅影响加载顺序）
	// 与 Deps 的区别：
	//   - Deps：依赖必须存在，否则注册失败；
	//   - OptionalDeps：依赖存在时保证先加载，不存在时插件仍可正常注册。
	// 适用于通过 plugin.Try / ctx.Get 访问的弱依赖（插件可在无该依赖的情况下正常运行）。
	// 在 RegisterMultiple / RegisterMultipleAtomic 批量注册时，OptionalDeps 中
	// 存在于同一批次的依赖会被纳入拓扑排序，确保可选依赖先于当前插件加载。
	OptionalDeps []string

	// Privileged 声明为管理类插件（可选，默认 false）
	//
	// 设为 true 后，框架在 Setup 时向 ctx.Admin 注入非 nil 的 ManagerWriter，
	// 允许插件调用 Reload/Disable/Enable/Unregister 等写操作。
	// 未声明 Privileged 的插件，ctx.Admin 为 nil。
	//
	// 此字段在代码审查中作为显眼的安全检查点。
	Privileged bool

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
	Meta *Metadata

	// Advanced 高级选项（可选）
	// 包含 Reload/OnDependencyReloaded/SaveState/RestoreState/ConfigSchema
	Advanced *Advanced
}

// --- Descriptor 辅助方法 ---

// effectiveMeta 返回元数据指针（Meta 为 nil 时返回 nil）
func (d *Descriptor) effectiveMeta() *Metadata {
	if d.Meta != nil {
		return d.Meta
	}
	return nil
}

// effectiveAdvanced 返回高级选项（Advanced 为 nil 时返回零值）
func (d *Descriptor) effectiveAdvanced() Advanced {
	if d.Advanced != nil {
		return *d.Advanced
	}
	return Advanced{}
}

// callSetup 调用 Setup 函数
func (d *Descriptor) callSetup(ctx *SetupContext) (any, error) {
	if d.Setup == nil {
		return nil, fmt.Errorf("plugin %q: Setup function is nil", d.Name)
	}
	return d.Setup(ctx)
}

// callTeardown 调用 Teardown 函数（nil 则跳过）
func (d *Descriptor) callTeardown(tctx *TeardownContext) error {
	if d.Teardown == nil {
		return nil
	}
	return d.Teardown(tctx)
}

// getOnDependencyReloaded 获取 OnDependencyReloaded 回调
func (d *Descriptor) getOnDependencyReloaded() func(string) {
	if d.Advanced != nil {
		return d.Advanced.OnDependencyReloaded
	}
	return nil
}
