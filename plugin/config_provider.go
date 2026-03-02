package plugin

import (
	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

// config_provider.go — 配置提供者接口与 viper 适配器
//
// Manager 通过 ConfigProvider 接口获取配置，viper 是可选的推荐实现。
// 若不需要配置热重载，可以不注入任何 ConfigProvider（Manager 零依赖可用）。
//
// 使用方式：
//
//	// 方式一（推荐）：通过 WithConfigProvider 注入 viper
//	pm := plugin.NewManager(eng, plugin.WithConfigProvider(plugin.NewViperConfigProvider(v)))
//
//	// 方式二：不注入，插件 Config 为 nil（适用于不需要配置的场景）
//	pm := plugin.NewManager(eng)

// ConfigProvider 是框架对外部配置源的抽象接口。
//
// Manager 通过此接口获取插件配置（不直接依赖 viper），
// 使得框架核心可以在零 viper 依赖下运行。
//
// 标准实现：ViperConfigProvider（见下方）。
// 自定义实现可对接任意配置源（etcd、consul、env 等）。
type ConfigProvider interface {
	// Sub 返回插件专属的配置视图（类似 viper.Sub）。
	// 返回 nil 表示该插件没有专属配置。
	Sub(pluginName string) map[string]any

	// OnConfigChange 注册配置变更回调（配置文件/来源发生变化时触发）。
	// 若配置源不支持热监听，可以实现为空操作。
	OnConfigChange(callback func())
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
		m.configProvider = cp
	}
}

// ViperConfigProvider 是 ConfigProvider 的 viper 实现。
//
// 使用示例：
//
//	v := viper.New()
//	v.SetConfigFile("config.yaml")
//	v.ReadInConfig()
//	v.WatchConfig()
//
//	pm := plugin.NewManager(eng,
//	    plugin.WithConfigProvider(plugin.NewViperConfigProvider(v)),
//	)
type ViperConfigProvider struct {
	v *viper.Viper
}

// NewViperConfigProvider 创建基于 viper 的配置提供者。
func NewViperConfigProvider(v *viper.Viper) *ViperConfigProvider {
	return &ViperConfigProvider{v: v}
}

// Sub 返回 plugins.<pluginName> 下的所有配置项。
func (vcp *ViperConfigProvider) Sub(pluginName string) map[string]any {
	if vcp.v == nil {
		return nil
	}
	sub := vcp.v.Sub("plugins." + pluginName)
	if sub == nil {
		return nil
	}
	return sub.AllSettings()
}

// OnConfigChange 订阅 viper 配置文件变更事件。
func (vcp *ViperConfigProvider) OnConfigChange(callback func()) {
	if vcp.v == nil {
		return
	}
	vcp.v.OnConfigChange(func(_ fsnotify.Event) {
		callback()
	})
}
