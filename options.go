package remilia

import (
	"github.com/KomeiDiSanXian/remilia/core/engine"
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

// WithAdapter 设置自定义适配器
func WithAdapter(adapter engine.PlatformAdapter) Option {
	return func(b *Bot) {
		b.adapter = adapter
	}
}

// WithEngine 设置自定义 engine
func WithEngine(engine *engine.Engine) Option {
	return func(b *Bot) {
		b.engine = engine
	}
}
