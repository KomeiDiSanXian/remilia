// Package plugintest 提供插件单元测试辅助工具。
//
// 参考 net/http/httptest 的惯例，测试辅助代码独立于主包，
// 避免污染生产包的导出符号。
//
// 使用示例：
//
//	func TestMyPlugin(t *testing.T) {
//	    ctx := plugintest.NewSetupContext("myplugin", nil)
//	    defer plugintest.StopSetupContext(ctx)
//
//	    api, err := myDescriptor.Setup(ctx)
//	    // ...
//	}
package plugintest

import (
	stdctx "context"

	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// SetupOptions 测试用 SetupContext 的可选配置
type SetupOptions struct {
	// Config 插件配置（nil 时不提供配置）
	Config plugin.Config
	// EventBus 事件总线（nil 时创建一个新的空总线）
	EventBus plugin.EventBus
	// Container 依赖容器（nil 时创建空容器）
	Container *plugin.Container
	// Engine 引擎（nil 时 ctx.Reg 为 no-op）
	Engine *engine.Engine
}

// NewSetupContext 创建用于单元测试的 SetupContext。
//
// 生成的 ctx 与真实注册流程隔离：
//   - ctx.Reg 默认为 no-op（不影响真实 engine）
//   - ctx.Log 输出到标准日志
//   - ctx.Info 为 nil-safe 空实现
//   - ctx.Admin 为 nil（测试中如需测试管理操作，需使用 NewPrivilegedSetupContext）
//   - ctx.Spawn 会实际调度 goroutine（可通过 StopSetupContext 停止）
func NewSetupContext(pluginName string, opts *SetupOptions) *plugin.SetupContext {
	if opts == nil {
		opts = &SetupOptions{}
	}
	return plugin.NewTestSetupContext(pluginName, &plugin.TestSetupOptions{
		Config:    opts.Config,
		EventBus:  opts.EventBus,
		Container: opts.Container,
		Engine:    opts.Engine,
	})
}

// StopSetupContext 停止 NewSetupContext 创建的 ctx 内的所有 goroutine。
// 在测试 teardown 阶段调用，防止 goroutine 泄漏。
//
//	ctx := plugintest.NewSetupContext("myplugin", nil)
//	defer plugintest.StopSetupContext(ctx)
func StopSetupContext(ctx *plugin.SetupContext) {
	plugin.StopTestSetupContext(ctx)
}

// NewPrivilegedSetupContext 创建具有管理权限的测试 SetupContext。
// 适用于测试声明了 Privileged: true 的插件（admin、debug 等）。
//
//	ctx := plugintest.NewPrivilegedSetupContext("admin", nil, pm)
//	defer plugintest.StopSetupContext(ctx)
func NewPrivilegedSetupContext(pluginName string, opts *SetupOptions, writer plugin.ManagerWriter) *plugin.SetupContext {
	ctx := NewSetupContext(pluginName, opts)
	ctx.Admin = writer
	return ctx
}

// NewSetupContextWithDeps 创建带有预注册依赖的测试 SetupContext。
//
//	deps := map[string]any{
//	    "storage": storagePlugin,
//	    "permission": permPlugin,
//	}
//	ctx := plugintest.NewSetupContextWithDeps("myplugin", deps, nil)
//	defer plugintest.StopSetupContext(ctx)
func NewSetupContextWithDeps(pluginName string, deps map[string]any, opts *SetupOptions) *plugin.SetupContext {
	if opts == nil {
		opts = &SetupOptions{}
	}
	if opts.Container == nil {
		opts.Container = plugin.NewContainer()
	}
	for name, dep := range deps {
		opts.Container.Register(name, dep)
	}
	return NewSetupContext(pluginName, opts)
}

// RunSetup 快捷函数：创建 ctx，运行 Setup，返回 API 和错误，自动清理 goroutine。
// 适用于只需要测试 Setup 逻辑而不关心上下文细节的场景。
func RunSetup(desc *plugin.Descriptor, opts *SetupOptions) (api any, err error, stop func()) {
	ctx := NewSetupContext(desc.Name, opts)
	api, err = desc.Setup(ctx)
	return api, err, func() { StopSetupContext(ctx) }
}

// NewGoContext 创建一个已取消的 context.Context，用于测试 ctx.Spawn 中监听 runCtx.Done() 的逻辑。
func NewGoContext() (stdctx.Context, stdctx.CancelFunc) {
	return stdctx.WithCancel(stdctx.Background())
}
