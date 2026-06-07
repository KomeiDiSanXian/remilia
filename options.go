package remilia

import (
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// WithPprof 注入 pprof 服务器，使其生命周期与 Bot 绑定（Start 时启动，Stop 时关闭）。
//
// 使用示例：
//
//	bot := remilia.NewBotBuilder().
//	    WithOption(remilia.WithPprof(remilia.DefaultPprofConfig())).
//	    Build()
func WithPprof(cfg PprofConfig) Option {
	return func(b *Bot) {
		if cfg.Enabled {
			b.pprofServer = NewPprofServer(cfg)
		}
	}
}

// Option Bot 配置选项函数类型
type Option func(*Bot)

// ensureConfig 确保 b.config 不为 nil（内部工具函数）
func ensureConfig(b *Bot) {
	if b.config == nil {
		b.config = &BotMeta{}
	}
}

// WithConfig 设置 Bot 元数据
func WithConfig(config *BotMeta) Option {
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

// WithGoroutineThreshold 设置 RuntimeChecker 的 goroutine 阈值，超过此值时标记 Degraded。
// 阈值 <= 0 表示不限制。
func WithGoroutineThreshold(n int) Option {
	return func(b *Bot) {
		b.goroutineThreshold = n
	}
}
