/*
Package remilia 是对核心引擎的高级封装，提供完整的生命周期管理。

Bot 是使用 Remilia 框架构建事件驱动应用的主入口，提供：
  - 生命周期管理（启动/停止）
  - 健康检查
  - 配置管理
  - 通过 platform.Adapter 处理多平台事件

# 平台无关用法（推荐）

使用平台无关的事件匹配注册处理器：

	import (
	    "context"
	    "log"

	    "github.com/KomeiDiSanXian/remilia"
	    "github.com/KomeiDiSanXian/remilia/core/engine"
	    eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	    "github.com/KomeiDiSanXian/remilia/platform"
	)

	eng := engine.NewEngine()

	// 注册一个适用于任意平台的命令处理器
	eng.OnCommand("", "/hello").
	    Handle(func(ctx *eventctx.Context) error {
	        return ctx.Reply(platform.TextMessage("Hello!"))
	    })

	// 通过 platform.Adapter 构建 Bot
	bot, err := remilia.NewBotBuilder().
	    WithPlatformAdapter(qqAdapter). // platform.Adapter
	    WithEngine(eng).
	    Build()

# QQ 用法

对于 QQ 机器人，使用 Bot 凭证创建 [qq.WebhookServerAdapter] 并传入构建器。
适配器内部自动管理 Token 刷新和消息发送：

	import "github.com/KomeiDiSanXian/remilia/platform/qq"
	import "github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"

	botInfo := &dto.BotInfo{
	    AppID:     123456,
	    Token:     "your-token",
	    AppSecret: "your-secret",
	}
	adapter := qq.NewWebhookServerAdapter(":8080", botInfo)

	bot, err := remilia.NewBotBuilder().
	    WithPlatformAdapter(adapter).
	    WithEngine(eng).
	    Build()

	// 平台无关匹配器（推荐用于所有平台）
	eng.OnEventKind(platform.EventKindPrivateMessage, eventctx.OnCommand("/ping")).Handle(pingHandler)
	eng.OnEventKind(platform.EventKindGroupMessage, eventctx.OnCommand("/hello")).Handle(helloHandler)

# 多平台

通过 [platform.Registry] 将多个平台接入同一个 Bot 实例。
每次调用 [BotBuilder.WithPlatformAdapter] 会覆盖前一个适配器；
若需注册多个平台，请使用 [BotBuilder.WithPlatformRegistry]：

	registry := platform.NewRegistry()
	registry.Register(qqAdapter)    // platform.PlatformAdapter
	registry.Register(discordAdapter)

	bot, err := remilia.NewBotBuilder().
	    WithPlatformRegistry(registry).
	    WithEngine(eng).
	    Build()

# 适配器接口

支持一种适配器接口：

  - platform.Adapter（推荐）：平台无关，处理器接收 platform.Event

# 健康检查

	status := bot.Health()
	fmt.Printf("Status: %s, Uptime: %v\n", status.Status, status.Uptime)

# 生命周期

Bot 使用 lifecycle 包：组件按顺序启动，按逆序停止。
启动失败会触发自动回滚。
*/
package remilia
