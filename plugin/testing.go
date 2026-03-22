package plugin

import (
	stdctx "context"

	"github.com/KomeiDiSanXian/remilia/core/engine"
)

// TestSetupOptions 测试用 SetupContext 的可选配置
type TestSetupOptions struct {
	Config    Config
	EventBus  EventBus
	Container *Container
	Engine    *engine.Engine
}

// NewTestSetupContext 创建用于单元测试的 SetupContext。
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

	instance := &Instance{
		desc:  &Descriptor{Name: pluginName},
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
	ctx.GoNamed = func(name string, fn func(runCtx stdctx.Context)) {
		gm.goNamed_(name, fn)
	}

	return ctx
}

// StopTestSetupContext 停止 NewTestSetupContext 创建的 ctx 内的所有 goroutine。
func StopTestSetupContext(ctx *SetupContext) {
	if ctx != nil && ctx.goroutineMgr != nil {
		ctx.goroutineMgr.stopAndWait()
	}
}
