// admission.go — 准入控制拒绝 AI 事件时的用户反馈。

package ai

import (
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// handleBusy 是聊天路由的「忙时处理器」（Matcher.OnReject）。
//
// 准入控制的判定发生在业务 handler 之前，被拒事件不会进入插件，所以文案必须
// 由插件自己声明。只挂在"本来就会回复"的路由上（@ 机器人 / 私聊 / 群自主
// 发言），因此这里直接回一句不会在群里变成刷屏。
func (p *Plugin) handleBusy(ctx *eventctx.Context) error {
	if ctx == nil {
		return nil
	}
	ctx.Reply(platform.TextMessage("⏳ 当前对话较多，请稍后再发一条。"))
	return nil
}
