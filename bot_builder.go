package remilia

import (
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// BotBuilder 提供流畅的Bot构建接口
//
// 使用示例:
//
//	bot := remilia.NewBotBuilder().
//	    WithBotInfo(botInfo).
//	    WithWebhook(":8080").
//	    WithName("my-bot").
//	    Build()
type BotBuilder struct {
	adapter  Adapter
	engine   *engine.Engine
	botInfo  *dto.BotInfo
	options  []Option
	hasError error
}

// NewBotBuilder 创建Bot构建器
func NewBotBuilder() *BotBuilder {
	return &BotBuilder{
		engine:  engine.NewEngine(), // 默认Engine
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
// 使用示例:
//
//	builder.WithWebhook(":8080")
func (b *BotBuilder) WithWebhook(addr string) *BotBuilder {
	if b.botInfo != nil {
		b.adapter = NewWebhookServerAdapter(addr, b.botInfo)
	}
	// 如果botInfo还没设置，稍后在Build时处理
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

// Build 构建Bot实例
//
// 如果构建过程中有错误，返回nil和错误
func (b *BotBuilder) Build() (*Bot, error) {
	// 验证必需参数
	if b.adapter == nil {
		return nil, ErrAdapterRequired
	}

	if b.engine == nil {
		b.engine = engine.NewEngine()
	}

	// 创建Bot
	var bot *Bot
	if b.botInfo != nil {
		bot = NewBotWithInfo(b.adapter, b.engine, b.botInfo, b.options...)
	} else {
		bot = NewBot(b.adapter, b.engine, b.options...)
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
