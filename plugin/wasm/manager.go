package wasm

import (
	"context"
	"fmt"
	"sync"

	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// PluginInstance 表示一个已注册的 WASM 插件的运行时实例。
type PluginInstance struct {
	Desc   *Descriptor
	Module *Module
	Bridge *Bridge
}

// Manager 管理 WASM 插件的完整生命周期：加载、初始化、注册 Matcher、清理。
//
// 使用方式：
//
//	wasmMgr := wasm.NewManager(pluginManager, nil)
//	wasmMgr.Register(ctx, &wasm.Descriptor{
//	    Name: "myplugin", Path: "./plugins/myplugin.wasm",
//	})
type Manager struct {
	pluginMgr *plugin.Manager
	engine    engine.MatcherWriter
	wasmRt    *Runtime

	mu      sync.Mutex
	plugins map[string]*PluginInstance
}

// NewManager 创建一个 WASM 插件管理器。
//
// pluginMgr 用于注册 WASM 插件生成的 Matcher 到插件系统（实现 Disable/Enable 联动）。
// 为 nil 时仅注册到 Engine，不纳入插件生命周期管理。
// hostRegistry 为 nil 时使用默认宿主函数集。
func NewManager(eng engine.MatcherWriter, hostRegistry *HostFuncRegistry) *Manager {
	return &Manager{
		engine:  eng,
		plugins: make(map[string]*PluginInstance),
	}
}

// SetRuntime 设置或替换 WASM 运行时。在 Register 前调用。
func (m *Manager) SetRuntime(rt *Runtime) {
	m.wasmRt = rt
}

// getRuntime 返回当前 WASM 运行时，必要时创建默认运行时。
func (m *Manager) getRuntime(ctx context.Context) (*Runtime, error) {
	if m.wasmRt != nil {
		return m.wasmRt, nil
	}
	rt, err := NewRuntime(ctx, nil)
	if err != nil {
		return nil, err
	}
	m.wasmRt = rt
	return rt, nil
}

// Register 加载并注册一个 WASM 插件。
func (m *Manager) Register(ctx context.Context, desc *Descriptor) (*PluginInstance, error) {
	if err := desc.Validate(); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.plugins[desc.Name]; exists {
		return nil, fmt.Errorf("wasm: plugin %q already registered", desc.Name)
	}

	rt, err := m.getRuntime(ctx)
	if err != nil {
		return nil, fmt.Errorf("wasm: runtime init failed: %w", err)
	}

	mod, err := rt.LoadModule(ctx, desc.Name, desc.Path)
	if err != nil {
		return nil, fmt.Errorf("wasm: load %q: %w", desc.Name, err)
	}

	if err := mod.CallInit(ctx); err != nil {
		mod.Close(ctx)
		return nil, fmt.Errorf("wasm: init %q: %w", desc.Name, err)
	}

	bridge := NewBridge(mod, m.engine)
	inst := &PluginInstance{
		Desc:   desc,
		Module: mod,
		Bridge: bridge,
	}
	m.plugins[desc.Name] = inst
	return inst, nil
}

// Unregister 卸载一个 WASM 插件。
func (m *Manager) Unregister(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	inst, exists := m.plugins[name]
	if !exists {
		return fmt.Errorf("wasm: plugin %q not found", name)
	}

	inst.Bridge.Cleanup()
	inst.Module.Close(ctx)
	delete(m.plugins, name)
	return nil
}

// Get 返回已注册的 WASM 插件实例。
func (m *Manager) Get(name string) *PluginInstance {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.plugins[name]
}

// List 返回所有已注册的 WASM 插件名称。
func (m *Manager) List() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	names := make([]string, 0, len(m.plugins))
	for n := range m.plugins {
		names = append(names, n)
	}
	return names
}

// Close 卸载所有 WASM 插件并关闭运行时。
func (m *Manager) Close(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name, inst := range m.plugins {
		inst.Bridge.Cleanup()
		inst.Module.Close(ctx)
		delete(m.plugins, name)
	}

	if m.wasmRt != nil {
		return m.wasmRt.Close(ctx)
	}
	return nil
}
