package remilia

import (
	"context"

	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/openapi/protocol/webhook"
)

// New 创建一个带默认配置的 Bot 实例
// 如果提供了 opts 中包含 adapter，则使用自定义 adapter，否则创建默认 webhook adapter
func New(info *dto.BotInfo, opts ...Option) *Bot {
	// 创建默认 newEngine
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
		wh := webhook.New(ctx, info)
		bot.adapter = NewWebhookAdapter(wh)
	}

	// 完成初始化
	return NewBot(bot.adapter, newEngine)
}
