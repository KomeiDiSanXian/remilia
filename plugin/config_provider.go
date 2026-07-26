package plugin

import (
	"sync"

	"github.com/KomeiDiSanXian/remilia/config"
)

// config_provider.go — 配置提供者接口与内置实现
//
// Manager 通过 ConfigProvider 接口获取插件配置（零外部依赖）。
// 内置的 YAMLConfigProvider 基于 config.Config.Plugins 实现。
//
// 使用方式：
//
//	// 方式一（推荐）：通过 WithConfigProvider 注入内置的 YAML 配置提供者
//	cfg, _ := config.Load("config.yaml")
//	pm := plugin.NewManager(eng,
//	    plugin.WithConfigProvider(plugin.NewYAMLConfigProvider(cfg)),
//	)
//
//	// 方式二：不注入，插件 Config 为 nil（适用于不需要配置的场景）
//	pm := plugin.NewManager(eng)

// ConfigProvider 是框架对外部配置源的抽象接口。
//
// Manager 通过此接口获取插件配置，你可以实现此接口对接任意配置源
// （etcd、consul、环境变量等）。
type ConfigProvider interface {
	// Sub 返回插件专属的配置视图。
	// 返回 nil 表示该插件没有专属配置。
	Sub(pluginName string) map[string]any

	// OnConfigChange 注册配置变更回调。
	// 若配置源不支持热监听，可以实现为空操作。
	OnConfigChange(callback func())
}

// YAMLConfigProvider 是基于 config.Config 的内置 ConfigProvider 实现。
//
// 它从 config.Config.Plugins 中读取插件配置，支持通过 Subscribe 监听
// 配置变更（配合 ConfigManager 的热重载能力）。
type YAMLConfigProvider struct {
	mu       sync.RWMutex
	cfg      *config.Config
	listener *config.ListenerToken
	onChange func()
}

// NewYAMLConfigProvider 创建一个基于 config.Config 的插件配置提供者。
//
// 如果传入的 configManager 不为 nil，会自动订阅配置变更实现热更新。
func NewYAMLConfigProvider(cfg *config.Config, configManager ...*config.Manager) *YAMLConfigProvider {
	p := &YAMLConfigProvider{cfg: cfg}
	if len(configManager) > 0 && configManager[0] != nil {
		p.listener = configManager[0].Subscribe(func(newCfg *config.Config) {
			p.mu.Lock()
			p.cfg = newCfg
			cb := p.onChange // 在锁内快照，避免与 OnConfigChange 的写并发
			p.mu.Unlock()
			if cb != nil {
				cb()
			}
		})
	}
	return p
}

// Sub 返回 plugins.<pluginName> 下的所有配置项。
func (p *YAMLConfigProvider) Sub(pluginName string) map[string]any {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.cfg == nil {
		return nil
	}
	cfg, _ := p.cfg.PluginConfig(pluginName)
	return cfg
}

// OnConfigChange 注册配置变更回调。
func (p *YAMLConfigProvider) OnConfigChange(callback func()) {
	p.mu.Lock()
	p.onChange = callback
	p.mu.Unlock()
}

// Stop 取消配置变更订阅。
// 应在不再需要此提供者时调用，防止 listener 泄漏。
func (p *YAMLConfigProvider) Stop() {
	if p.listener != nil {
		p.listener.Cancel()
	}
}

// ManagerOption 是 Manager 的可选配置函数。
type ManagerOption func(*Manager)

// WithConfigProvider 注入外部配置提供者（可选）。
//
// 若不调用此选项，插件的 Config 字段将为 nil。
// 插件应在使用 Config 前检查其是否为 nil：
//
//	if ctx.Config != nil {
//	    val := ctx.Config.GetString("key", "default")
//	}
func WithConfigProvider(cp ConfigProvider) ManagerOption {
	return func(m *Manager) {
		m.config.configProvider = cp
	}
}
