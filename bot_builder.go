package remilia

import (
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// BotBuilder 提供流畅的Bot构建接口
//
// 链式调用顺序无关：WithWebhook 与 WithBotInfo 可任意顺序调用。
//
// 使用示例:
//
// bot, err := remilia.NewBotBuilder().
//
//	WithBotInfo(botInfo).
//	WithWebhook(":8080").
//	WithPlugins(plugin1.New(), plugin2.New()).
//	Build()
type BotBuilder struct {
	adapter        Adapter
	engine         *engine.Engine
	botInfo        *dto.BotInfo
	webhookAddr    string                     // 延迟创建：仅保存地址，Build() 时统一初始化 adapter
	pluginManager  *plugin.Manager            // 可选，通过 WithPluginManager 或 WithPlugins 注入
	pendingPlugins []*plugin.PluginDescriptor // WithPlugins 收集的描述符，Build() 时批量注册
	options        []Option
	hasError       error
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

// WithAdapter 设置适配器
func (b *BotBuilder) WithAdapter(adapter Adapter) *BotBuilder {
	b.adapter = adapter
	return b
}

// WithWebhook 快速创建Webhook适配器
//
// 与 WithBotInfo 顺序无关，可在 WithBotInfo 之前或之后调用。
// adapter 的实际创建在 Build() 阶段统一完成。
func (b *BotBuilder) WithWebhook(addr string) *BotBuilder {
	b.webhookAddr = addr
	return b
}

// WithBotInfo 设置Bot信息（用于API调用）
func (b *BotBuilder) WithBotInfo(info *dto.BotInfo) *BotBuilder {
	b.botInfo = info
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
// bot, err := remilia.NewBotBuilder().
//
//	WithBotInfo(info).
//	WithWebhook(":8080").
//	WithPlugins(
//	    myPlugin.New(),
//	    anotherPlugin.New(),
//	).
//	Build()
//
// bot.Start() // 自动触发所有插件 Setup
// bot.Stop()  // 自动触发所有插件 Teardown（逆序）
func (b *BotBuilder) WithPlugins(descriptors ...*plugin.PluginDescriptor) *BotBuilder {
	b.pendingPlugins = append(b.pendingPlugins, descriptors...)
	return b
}

// Build 构建Bot实例
//
// Build() 负责完成所有延迟初始化逻辑：
//   - 若设置了 WithWebhook 地址且有 BotInfo，自动创建 WebhookServerAdapter
//   - 若调用了 WithPlugins，自动创建/复用 plugin.Manager 并按依赖序批量注册插件
//   - WithWebhook 与 WithBotInfo 的调用顺序不影响结果
//
// 如果构建过程中有错误，返回nil和错误
func (b *BotBuilder) Build() (*Bot, error) {
	// Build 阶段统一完成 webhook adapter 初始化（与调用顺序无关）
	if b.adapter == nil && b.webhookAddr != "" {
		if b.botInfo == nil {
			return nil, ErrBotInfoRequired
		}
		b.adapter = NewWebhookServerAdapter(b.webhookAddr, b.botInfo)
	}

	// 验证必需参数
	if b.adapter == nil {
		return nil, ErrAdapterRequired
	}

	if b.engine == nil {
		b.engine = engine.NewEngine()
	}
	// 若有 WithPlugins 描述符，确保 PluginManager 存在并批量注册
	if len(b.pendingPlugins) > 0 {
		if b.pluginManager == nil {
			b.pluginManager = plugin.NewManager(b.engine)
		}
		if err := b.pluginManager.RegisterMultipleV2Smart(b.pendingPlugins); err != nil {
			return nil, err
		}
	}
	// 创建Bot
	var bot *Bot
	if b.botInfo != nil {
		bot = NewBotWithInfo(b.adapter, b.engine, b.botInfo, b.options...)
	} else {
		bot = NewBot(b.adapter, b.engine, b.options...)
	}
	// 注入插件管理器（若已配置）
	if b.pluginManager != nil {
		bot.UsePlugins(b.pluginManager)
	}
	return bot, nil
}

// MustBuild 构建Bot，如果失败则panic
//
// 适用于确信配置正确的场景，简化错误处理
func (b *BotBuilder) MustBuild() *Bot {
	bot, err := b.Build()
	if err != nil {
		panic("failed to build bot: " + err.Error())
	}
	return bot
}
