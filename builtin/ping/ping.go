package ping

import (
	"fmt"
	"time"

	"github.com/KomeiDiSanXian/remilia/command"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

func New() *plugin.Descriptor {
	return &plugin.Descriptor{
		Name:    "ping",
		Version: "1.0.0",
		Meta: &plugin.Metadata{
			Author:      "Remilia Team",
			Description: "消息处理延迟检测",
			Category:    "工具",
			Tags:        []string{"ping", "延迟"},
			HelpText: `ping — 消息处理延迟检测

衡量 bot 从平台接收到消息到回复的端到端耗时，
包含网络传输、平台推送、事件排队、中间件链、路由匹配等环节，
并非纯"服务端处理时间"。

用法：
  /ping

示例：
  /ping`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			pingDef := command.NewDef("ping").Description("消息处理延迟检测").Build()
			ctx.OnCommandDefWith("", "/ping", pingDef, handlePing, eventctx.OnMentionedBotOrNoMentions())
			return nil, nil
		},
	}
}

func handlePing(ctx *eventctx.Context) error {
	latency := time.Since(ctx.GetPlatformEvent().Timestamp())
	ctx.Reply(platform.TextMessage(
		fmt.Sprintf("pong %.3f ms（平台 → bot 端到端延迟）", float64(latency)/float64(time.Millisecond))))
	return nil
}
