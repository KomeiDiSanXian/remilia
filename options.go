package remilia

import (
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// Option Bot 配置选项函数类型
type Option func(*Bot)

// ensureConfig 确保 b.config 不为 nil（内部工具函数）
func ensureConfig(b *Bot) {
	if b.config == nil {
		b.config = &Config{}
	}
}

// WithConfig 设置 Bot 配置
func WithConfig(config *Config) Option {
	return func(b *Bot) {
		if config != nil {
			b.config = config
		}
	}
}

// WithName 设置 Bot 名称
func WithName(name string) Option {
	return func(b *Bot) {
		ensureConfig(b)
		b.config.Name = name
	}
}

// WithVersion 设置 Bot 版本
func WithVersion(version string) Option {
	return func(b *Bot) {
		ensureConfig(b)
		b.config.Version = version
	}
}

// WithDebug 设置调试模式
func WithDebug(debug bool) Option {
	return func(b *Bot) {
		ensureConfig(b)
		b.config.Debug = debug
	}
}

// WithAdapter 将平台适配器注册到 Bot 的内部 Registry。
//
// D3：Bot 不再有独立的 adapter 字段，所有适配器统一通过 platformRegistry 管理。
// 若 Bot 尚未初始化 Registry，此方法会自动创建。
func WithAdapter(adapter platform.Adapter) Option {
	return func(b *Bot) {
		if adapter == nil {
			return
		}
		if b.platformRegistry == nil {
			b.platformRegistry = platform.NewRegistry()
		}
		b.platformRegistry.Register(adapter)
	}
}

// WithEngine 设置自定义 engine
func WithEngine(engine *engine.Engine) Option {
	return func(b *Bot) {
		b.engine = engine
	}
}
