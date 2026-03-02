package remilia

import (
	"context"

	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/openapi/protocol/webhook"
)

// NewBotWithDefault 创建一个带默认配置的 Bot 实例。
//
// 默认使用基于 webhook 的 adapter。若 opts 中包含 WithAdapter，该选项会覆盖默认 adapter。
// opts 仅被应用一次，不存在重复初始化问题。
func NewBotWithDefault(info *dto.BotInfo, opts ...Option) *Bot {
	ctx := context.Background()
	wh := webhook.NewWebhook(ctx, info)
	adapter := NewWebhookAdapter(wh)
	return NewBotWithInfo(adapter, engine.NewEngine(), info, opts...)
}
