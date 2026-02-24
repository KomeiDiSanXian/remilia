package plugin

import (
	stdctx "context"

	"github.com/KomeiDiSanXian/remilia/core/engine"
)

// TestSetupOptions 测试用 SetupContext 的可选配置
type TestSetupOptions struct {
	// Config 插件配置（nil 时不提供配置）
	Config Config
	// EventBus 事件总线（nil 时创建一个新的空总线）
	EventBus EventBus
	// Container 依赖容器（nil 时创建空容器）
	Container *Container
	// Engine 引擎（nil 时 ctx.Reg 为 no-op）
	Engine *engine.Engine
}

// NewTestSetupContext 创建用于单元测试的 SetupContext。
//
// 生成的 ctx 与真实注册流程隔离：
//   - ctx.Reg 默认为 no-op（不影响真实 engine）
//   - ctx.Log 输出到标准日志
//   - ctx.Info 为 nil-safe 空实现
//   - ctx.Go 会实际调度 goroutine（可通过 StopTestSetupContext 停止）
//
// 使用示例：
//
//	ctx := plugin.NewTestSetupContext("myplugin", nil)
//	defer plugin.StopTestSetupContext(ctx)
//	api, err := myDescriptor.Setup(ctx)
func NewTestSetupContext(pluginName string, opts *TestSetupOptions) *SetupContext {
	if opts == nil {
		opts = &TestSetupOptions{}
	}

	container := opts.Container
	if container == nil {
		container = NewContainer()
	}

	bus := opts.EventBus
	if bus == nil {
		bus = NewEventBus()
	}

	gm := newGoroutineManager()

	instance := &PluginInstance{
		desc:  &PluginDescriptor{Name: pluginName},
		state: Unloaded,
	}

	var rw RegistryWriter
	if opts.Engine != nil {
		rw = newLiveRegistryWriter(opts.Engine, pluginName, instance)
	} else {
		rw = &noopRegistryWriter{}
	}

	ctx := &SetupContext{
		Reg:      rw,
		Log:      newPluginLogger(pluginName),
		Info:     &nullPluginInfo{},
		Config:   opts.Config,
		EventBus: bus,
		setupContextInternal: setupContextInternal{
			container:        container,
			pluginName:       pluginName,
			instance:         instance,
			autoTrackEnabled: true,
			goroutineMgr:     gm,
		},
	}
	ctx.Go = func(fn func(runCtx stdctx.Context)) {
		gm.go_(fn)
	}

	return ctx
}

// StopTestSetupContext 停止 NewTestSetupContext 创建的 ctx 内的所有 goroutine。
// 在测试 teardown 阶段调用，防止 goroutine 泄漏。
//
//	ctx := plugin.NewTestSetupContext("myplugin", nil)
//	defer plugin.StopTestSetupContext(ctx)
func StopTestSetupContext(ctx *SetupContext) {
	if ctx != nil && ctx.goroutineMgr != nil {
		ctx.goroutineMgr.stopAndWait()
	}
}
