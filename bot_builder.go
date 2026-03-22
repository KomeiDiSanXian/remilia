package remilia

import (
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/errutil"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// BotBuilder 提供流畅的Bot构建接口。
//
// 平台无关：不直接依赖任何具体平台（QQ、Discord 等）。
// 平台适配器由调用方创建后通过 [BotBuilder.WithPlatformAdapter] 或
// [BotBuilder.WithPlatformRegistry] 注入，适配器自行管理其认证和发送逻辑。
//
// 使用示例（QQ Webhook）:
//
//	adapter := qq.NewWebhookServerAdapter(":8080", botInfo)
//	bot, err := remilia.NewBotBuilder().
//	    WithPlatformAdapter(adapter).
//	    WithPlugins(plugin1.New(), plugin2.New()).
//	    Build()
type BotBuilder struct {
	adapter          platform.Adapter
	engine           *engine.Engine
	pluginManager    *plugin.Manager      // 可选，通过 WithPluginManager 或 WithPlugins 注入
	pendingPlugins   []*plugin.Descriptor // WithPlugins 收集的描述符，Build() 时批量注册
	platformRegistry *platform.Registry   // 可选，多平台适配器注册表
	options          []Option
}

// NewBotBuilder 创建Bot构建器
func NewBotBuilder() *BotBuilder {
	return &BotBuilder{
		engine:  engine.NewEngine(),
		options: make([]Option, 0),
	}
}

// WithEngine 设置自定义Engine
func (b *BotBuilder) WithEngine(eng *engine.Engine) *BotBuilder {
	if eng != nil {
		b.engine = eng
	}
	return b
}

// WithPlatformAdapter 设置单平台适配器。
//
// 每次调用会覆盖上一次设置的适配器；若需要同时运行多个平台，
// 请改用 [BotBuilder.WithPlatformRegistry]。
func (b *BotBuilder) WithPlatformAdapter(adapter platform.Adapter) *BotBuilder {
	b.adapter = adapter
	return b
}

// WithName 设置Bot名称
func (b *BotBuilder) WithName(name string) *BotBuilder {
	b.options = append(b.options, WithName(name))
	return b
}

// WithVersion 设置Bot版本
func (b *BotBuilder) WithVersion(version string) *BotBuilder {
	b.options = append(b.options, WithVersion(version))
	return b
}

// WithDebug 启用调试模式
func (b *BotBuilder) WithDebug(debug bool) *BotBuilder {
	b.options = append(b.options, WithDebug(debug))
	return b
}

// WithOption 添加自定义选项
func (b *BotBuilder) WithOption(opt Option) *BotBuilder {
	b.options = append(b.options, opt)
	return b
}

// WithPluginManager 注入插件管理器，将其生命周期与 Bot 绑定。
//
// Build() 后，Bot.Start() 会自动触发所有插件的 Setup，
// Bot.Stop() 会自动按逆序触发所有插件的 Teardown。
//
// 注意：WithPluginManager 与 WithPlugins 可同时使用；
// WithPlugins 注册的描述符会追加到此 Manager 中。
func (b *BotBuilder) WithPluginManager(pm *plugin.Manager) *BotBuilder {
	b.pluginManager = pm
	return b
}

// WithPlugins 一步注册多个插件描述符，无需手动创建 plugin.Manager。
//
// Build() 阶段自动创建（或复用已有的）plugin.Manager 并批量注册，
// 注册顺序自动按依赖拓扑排序（等同于 RegisterMultipleV2Smart）。
//
// 这是框架推荐的最简洁插件集成方式：
//
//	adapter := qq.NewWebhookServerAdapter(":8080", botInfo)
//	bot, err := remilia.NewBotBuilder().
//	    WithPlatformAdapter(adapter).
//	    WithPlugins(myPlugin.New(), anotherPlugin.New()).
//	    Build()
func (b *BotBuilder) WithPlugins(descriptors ...*plugin.Descriptor) *BotBuilder {
	b.pendingPlugins = append(b.pendingPlugins, descriptors...)
	return b
}

// WithPlatformRegistry 注入多平台适配器注册表。
//
// 注入后 Bot.Start() 会为每个已注册的平台适配器启动独立事件循环。
// 当仅使用注册表时，可省略 WithPlatformAdapter，Build() 不要求单一适配器存在：
//
//	registry := platform.NewRegistry()
//	registry.Register(qq.NewAdapter(webhookConn, api))
//	registry.Register(discord.NewAdapter())
//
//	bot, err := remilia.NewBotBuilder().
//	    WithPlatformRegistry(registry).
//	    Build()
func (b *BotBuilder) WithPlatformRegistry(r *platform.Registry) *BotBuilder {
	b.platformRegistry = r
	return b
}

// Build 构建Bot实例。
//
// D3：内部统一使用 registry 模式。若只设置了单个适配器（WithPlatformAdapter），
// Build() 会自动将其注册到 Registry 中，两种使用方式完全透明。
func (b *BotBuilder) Build() (*Bot, error) {
	// 需要至少一个事件来源
	if b.adapter == nil && b.platformRegistry == nil {
		return nil, errutil.ErrAdapterRequired
	}

	if b.engine == nil {
		b.engine = engine.NewEngine()
	}

	if len(b.pendingPlugins) > 0 {
		if b.pluginManager == nil {
			b.pluginManager = plugin.NewManager(b.engine)
		}
		if err := b.pluginManager.RegisterMultipleV2Smart(b.pendingPlugins); err != nil {
			return nil, err
		}
	}

	// 将单适配器合并到 registry（D3：统一来源）
	reg := b.platformRegistry
	if b.adapter != nil {
		if reg == nil {
			reg = platform.NewRegistry()
		}
		reg.Register(b.adapter)
	}

	// NewBot 传 nil adapter，registry 已经包含所有适配器
	bot := NewBot(nil, b.engine, b.options...)
	if reg != nil {
		bot.UsePlatformRegistry(reg)
	}
	if b.pluginManager != nil {
		bot.UsePlugins(b.pluginManager)
	}
	return bot, nil
}

// MustBuild 构建Bot，如果失败则panic。
// 适用于确信配置正确的场景，简化错误处理。
func (b *BotBuilder) MustBuild() *Bot {
	bot, err := b.Build()
	if err != nil {
		panic("failed to build bot: " + err.Error())
	}
	return bot
}
