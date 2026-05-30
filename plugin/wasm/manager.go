package wasm

import (
	"context"
	"fmt"
	"sync"

	"github.com/KomeiDiSanXian/remilia/core/engine"
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
//	wasmMgr := wasm.NewManager(eng, nil)
//	wasmMgr.Register(ctx, &wasm.Descriptor{
//	    Name: "myplugin", Path: "./plugins/myplugin.wasm",
//	    Commands: []wasm.CommandDef{{Command: "/mycmd"}},
//	})
type Manager struct {
	engine       engine.MatcherWriter
	wasmRt       *Runtime
	hostRegistry *HostFuncRegistry

	mu      sync.Mutex
	plugins map[string]*PluginInstance
}

// NewManager 创建一个 WASM 插件管理器。
// hostRegistry 为 nil 时使用默认宿主函数集。
func NewManager(eng engine.MatcherWriter, hostRegistry *HostFuncRegistry) *Manager {
	return &Manager{
		engine:       eng,
		hostRegistry: hostRegistry,
		plugins:      make(map[string]*PluginInstance),
	}
}

// SetRuntime 设置或替换 WASM 运行时。在 Register 前调用。
func (m *Manager) SetRuntime(rt *Runtime) {
	m.wasmRt = rt
}

// getRuntime 返回当前 WASM 运行时，必要时使用 m.hostRegistry 创建默认运行时。
func (m *Manager) getRuntime(ctx context.Context) (*Runtime, error) {
	if m.wasmRt != nil {
		return m.wasmRt, nil
	}
	rt, err := NewRuntime(ctx, m.hostRegistry)
	if err != nil {
		return nil, err
	}
	m.wasmRt = rt
	return rt, nil
}

// Register 从文件路径加载并注册一个 WASM 插件。
//
// 流程：验证描述符 → 获取运行时 → 从 Path 加载模块 → 创建 Bridge →
// 注册声明式命令 → 调用 plugin_init → 返回实例。
func (m *Manager) Register(ctx context.Context, desc *Descriptor) (*PluginInstance, error) {
	if desc.Path == "" {
		return nil, fmt.Errorf("wasm: Descriptor.Path is required for Register, use Instantiate for raw bytes")
	}
	return m.register(ctx, desc, nil)
}

// Instantiate 从字节数据加载并注册一个 WASM 插件。
// 与 Register 的区别在于 wasmBytes 直接传入，不依赖文件系统。
func (m *Manager) Instantiate(ctx context.Context, desc *Descriptor, wasmBytes []byte) (*PluginInstance, error) {
	return m.register(ctx, desc, wasmBytes)
}

// register 是 Register 和 Instantiate 的内部实现。
// wasmBytes 为 nil 时从 desc.Path 加载，否则直接使用。
func (m *Manager) register(ctx context.Context, desc *Descriptor, wasmBytes []byte) (*PluginInstance, error) {
	if desc.Name == "" {
		return nil, fmt.Errorf("wasm: Descriptor.Name is required")
	}
	if wasmBytes == nil && desc.Path == "" {
		return nil, fmt.Errorf("wasm: Descriptor.Path is required for Register, use Instantiate for raw bytes")
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

	// 根据描述符创建沙箱
	sandbox := NewSandbox(desc.EffectiveResourceLimit())

	var mod *Module
	if wasmBytes != nil {
		mod, err = rt.InstantiateModule(ctx, desc.Name, wasmBytes, sandbox)
	} else {
		mod, err = rt.LoadModule(ctx, desc.Name, desc.Path, sandbox)
	}
	if err != nil {
		return nil, fmt.Errorf("wasm: load %q: %w", desc.Name, err)
	}

	// 存储配置，使 get_config 宿主函数能查询
	rt.SetModuleConfig(desc.Name, desc.Config)

	bridge := NewBridge(mod, m.engine)

	// 注册声明式命令（在 CallInit 之前，确保 Matcher 可用）
	for _, cmd := range desc.Commands {
		if _, err := bridge.RegisterCommand(RegistrationRequest{
			EventType: cmd.EventType,
			Command:   cmd.Command,
		}); err != nil {
			bridge.Cleanup()
			mod.Close(ctx)
			return nil, fmt.Errorf("wasm: register command %q for %q: %w", cmd.Command, desc.Name, err)
		}
	}

	// 调用 plugin_init（模块可在此执行内部初始化）
	if err := mod.CallInit(ctx); err != nil {
		bridge.Cleanup()
		mod.Close(ctx)
		return nil, fmt.Errorf("wasm: init %q: %w", desc.Name, err)
	}

	inst := &PluginInstance{
		Desc:   desc,
		Module: mod,
		Bridge: bridge,
	}
	m.plugins[desc.Name] = inst
	return inst, nil
}

// GetRuntime 返回 Manager 使用的 WASM 运行时。
func (m *Manager) GetRuntime() *Runtime {
	return m.wasmRt
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
	if m.wasmRt != nil {
		m.wasmRt.RemoveModuleConfig(name)
	}
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
