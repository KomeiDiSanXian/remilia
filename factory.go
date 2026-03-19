package remilia

import (
	qqplatform "github.com/KomeiDiSanXian/remilia/platform/qq"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
)

// NewBotWithDefault 创建一个带默认 Webhook 配置的 QQ Bot 实例。
//
// 这是一个 QQ 平台专属的便捷构造函数。内部创建 [qq.WebhookServerAdapter]（它自动管理
// token 刷新等 QQ 认证逻辑），再委托给 [BotBuilder] 完成 Bot 构建，等价于：
//
//	adapter := qq.NewWebhookServerAdapter(addr, info)
//	bot, err := remilia.NewBotBuilder().
//	    WithPlatformAdapter(adapter).
//	    Build()
//
// 若需要更多定制选项，请直接使用上述方式。
func NewBotWithDefault(addr string, info *dto.BotInfo, opts ...Option) (*Bot, error) {
	adapter := qqplatform.NewWebhookServerAdapter(addr, info)
	b := NewBotBuilder().WithPlatformAdapter(adapter)
	for _, opt := range opts {
		b.WithOption(opt)
	}
	return b.Build()
}
