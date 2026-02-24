package plugin

import (
	stdctx "context"

	"github.com/KomeiDiSanXian/remilia/core/engine"
)

// TestSetupOptions 测试用 SetupContext 的可选配置
//
// Deprecated: 使用 plugintest.SetupOptions 替代（plugin/plugintest 子包）。
type TestSetupOptions struct {
	Config    Config
	EventBus  EventBus
	Container *Container
	Engine    *engine.Engine
}

// NewTestSetupContext 创建用于单元测试的 SetupContext。
//
// Deprecated: 使用 plugintest.NewSetupContext 替代（plugin/plugintest 子包）。
// 此函数保留以维持向后兼容性。
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
//
// Deprecated: 使用 plugintest.StopSetupContext 替代（plugin/plugintest 子包）。
func StopTestSetupContext(ctx *SetupContext) {
	if ctx != nil && ctx.goroutineMgr != nil {
		ctx.goroutineMgr.stopAndWait()
	}
}
