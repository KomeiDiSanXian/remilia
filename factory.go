package remilia

import (
	"context"

	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/openapi/protocol/webhook"
)

// NewBotWithDefault 创建一个带默认配置的 Bot 实例
// 如果提供了 opts 中包含 adapter，则使用自定义 adapter，否则创建默认 webhook adapter
// 这个函数会自动初始化 OpenAPI client
func NewBotWithDefault(info *dto.BotInfo, opts ...Option) *Bot {
	// 创建默认 Engine
	newEngine := engine.NewEngine()

	// 创建 bot 但先不设置 adapter
	bot := &Bot{
		engine: newEngine,
		config: &Config{
			Name:    "remilia-bot",
			Version: "0.9.0",
			Debug:   false,
		},
	}

	// 应用选项（可能包含 WithAdapter）
	for _, opt := range opts {
		opt(bot)
	}

	// 如果没有提供 adapter，创建默认的 webhook adapter
	if bot.adapter == nil {
		ctx := context.Background()
		wh := webhook.NewWebhook(ctx, info)
		bot.adapter = NewWebhookAdapter(wh)
	}

	// 使用 NewBotWithInfo 来初始化 OpenAPI
	return NewBotWithInfo(bot.adapter, newEngine, info, opts...)
}
