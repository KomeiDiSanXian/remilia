package plugintest

import (
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// MockPluginInfo 是 plugin.PluginInfo 的可配置 mock，用于单元测试。
//
// 允许测试代码精确控制 ctx.Info 的返回值，无需启动真实的 Manager。
//
//	info := &plugintest.MockPluginInfo{
//	    LoadedPlugins: map[string]bool{"storage": true},
//	    Plugins:       map[string]*plugin.Metadata{"storage": {Name: "storage"}},
//	}
//	ctx := plugintest.NewSetupContextWithInfo("myplugin", info, nil)
type MockPluginInfo struct {
	// LoadedPlugins 控制 IsLoaded(name) 的返回值
	LoadedPlugins map[string]bool
	// DisabledPlugins 控制 IsDisabled(name) 的返回值
	DisabledPlugins map[string]bool
	// Plugins 存储插件元数据，同时作为 List()/Count() 的数据来源
	Plugins map[string]*plugin.Metadata
	// Statuses 存储插件状态，用于 GetStatus(name) 返回
	Statuses map[string]*plugin.Status
	// LoadOrder 控制 GetLoadOrder() 的返回值
	LoadOrder []string
	// CoordinatorValue 控制 Coordinator() 的返回值（可为 nil）
	CoordinatorValue engine.EngineReader
	// Instances 存储插件实例，用于 Get(name) 返回
	Instances map[string]*plugin.PluginInstance
}

// IsLoaded 实现 plugin.PluginInfo
func (m *MockPluginInfo) IsLoaded(name string) bool {
	if m.LoadedPlugins == nil {
		return false
	}
	return m.LoadedPlugins[name]
}

// IsDisabled 实现 plugin.PluginInfo
func (m *MockPluginInfo) IsDisabled(name string) bool {
	if m.DisabledPlugins == nil {
		return false
	}
	return m.DisabledPlugins[name]
}

// GetStatus 实现 plugin.PluginInfo
func (m *MockPluginInfo) GetStatus(name string) *plugin.Status {
	if m.Statuses == nil {
		return nil
	}
	return m.Statuses[name]
}

// List 实现 plugin.PluginInfo，返回 Plugins map 的所有 key
func (m *MockPluginInfo) List() []string {
	if m.Plugins == nil {
		return nil
	}
	names := make([]string, 0, len(m.Plugins))
	for name := range m.Plugins {
		names = append(names, name)
	}
	return names
}

// Count 实现 plugin.PluginInfo
func (m *MockPluginInfo) Count() int {
	return len(m.Plugins)
}

// GetMetadata 实现 plugin.PluginInfo
func (m *MockPluginInfo) GetMetadata(name string) (*plugin.Metadata, bool) {
	if m.Plugins == nil {
		return nil, false
	}
	meta, ok := m.Plugins[name]
	return meta, ok
}

// ListWithMetadata 实现 plugin.PluginInfo
func (m *MockPluginInfo) ListWithMetadata() map[string]*plugin.Metadata {
	if m.Plugins == nil {
		return nil
	}
	result := make(map[string]*plugin.Metadata, len(m.Plugins))
	for k, v := range m.Plugins {
		result[k] = v
	}
	return result
}

// GetLoadOrder 实现 plugin.PluginInfo
func (m *MockPluginInfo) GetLoadOrder() []string {
	return m.LoadOrder
}

// Get 实现 plugin.PluginInfo
func (m *MockPluginInfo) Get(name string) (*plugin.PluginInstance, bool) {
	if m.Instances == nil {
		return nil, false
	}
	inst, ok := m.Instances[name]
	return inst, ok
}

// Coordinator 实现 plugin.PluginInfo
func (m *MockPluginInfo) Coordinator() engine.EngineReader {
	return m.CoordinatorValue
}

// NewSetupContextWithInfo 创建带自定义 PluginInfo mock 的测试 SetupContext。
//
//	info := &plugintest.MockPluginInfo{
//	    LoadedPlugins: map[string]bool{"storage": true},
//	}
//	ctx := plugintest.NewSetupContextWithInfo("help", info, nil)
//	defer plugintest.StopSetupContext(ctx)
func NewSetupContextWithInfo(pluginName string, info *MockPluginInfo, opts *SetupOptions) *plugin.SetupContext {
	ctx := NewSetupContext(pluginName, opts)
	ctx.Info = info
	return ctx
}
