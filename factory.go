package remilia

import (
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// NewBotWithDefault 创建一个带默认 Webhook 配置的 Bot 实例。
//
// 简化 API（统一路径版本）：内部委托给 BotBuilder，与
// NewBotBuilder().WithBotInfo(info).WithWebhook(addr).Build()
// 在行为上完全等价。
//
// 若不需要指定 Webhook 地址，请直接使用 NewBotBuilder()。
func NewBotWithDefault(info *dto.BotInfo, opts ...Option) (*Bot, error) {
	b := NewBotBuilder().WithBotInfo(info)
	for _, opt := range opts {
		b.WithOption(opt)
	}
	return b.Build()
}
