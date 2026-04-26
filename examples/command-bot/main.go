package main

import (
	"fmt"
	"log"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/config"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/middleware"
	"github.com/KomeiDiSanXian/remilia/platform"
	qq "github.com/KomeiDiSanXian/remilia/platform/qq"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
)

func main() {
	// 加载配置
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v\nPlease copy config.example.yaml to config.yaml", err)
	}

	// 初始化日志
	logCfg := logger.Config{
		Level:      cfg.Log.Level,
		Console:    true,
		File:       false,
		TimeFormat: "2006-01-02 15:04:05",
	}
	if err := logger.Init(logCfg); err != nil {
		log.Fatalf("Failed to init logger: %v", err)
	}

	// 创建 BotInfo
	botInfo := &dto.BotInfo{
		QQNum:     cfg.Bot.QQ.BotID,
		AppID:     cfg.Bot.QQ.AppID,
		Token:     cfg.Bot.QQ.Token,
		AppSecret: cfg.Bot.QQ.Secret,
	}

	// 使用 BotBuilder 创建 Bot
	adapter := qq.NewWebhookServerAdapter(":8080", botInfo)
	bot, err := remilia.NewBotBuilder().
		WithPlatformAdapter(adapter).
		WithName("command-bot").
		Build()
	if err != nil {
		log.Fatalf("Failed to build bot: %v", err)
	}

	// 使用开发环境中间件
	bot.Engine().Use(middleware.DevelopmentSet()...)

	// 注册命令处理器
	registerCommands(bot)

	// 启动 Bot
	logger.Info("[CommandBot] Starting bot...")
	if err := bot.Start(); err != nil {
		logger.WithError(err).Fatal("[CommandBot] Failed to start bot")
	}

	logger.Info("[CommandBot] Bot started! Press Ctrl+C to stop")
	bot.WaitForShutdown()
	logger.Info("[CommandBot] Bot stopped")
}

func registerCommands(bot *remilia.Bot) {
	// 1. 天气命令
	bot.Engine().OnCommand("", "/weather").Handle(func(ctx *eventctx.Context) error {
		_, err := ctx.Reply(platform.TextMessage("天气查询功能\n用法: /weather <城市>\n(实际天气查询待实现)"))
		return err
	})

	// 2. 计算器命令
	bot.Engine().OnCommand("", "/calc").Handle(func(ctx *eventctx.Context) error {
		_, err := ctx.Reply(platform.TextMessage("计算器功能\n用法: /calc <表达式>\n(实际计算待实现)"))
		return err
	})

	// 3. 搜索命令
	bot.Engine().OnCommand("", "/search").Handle(func(ctx *eventctx.Context) error {
		_, err := ctx.Reply(platform.TextMessage("搜索功能\n用法: /search <关键词>\n(实际搜索待实现)"))
		return err
	})

	// 4. 帮助命令
	bot.Engine().OnCommand("", "/help").Handle(func(ctx *eventctx.Context) error {
		help := `可用命令:
/weather - 查询天气
/calc - 计算器
/search - 搜索
/help - 显示帮助
/user - 用户信息`
		_, err := ctx.Reply(platform.TextMessage(help))
		return err
	})

	// 5. 用户信息命令
	bot.Engine().OnCommand("", "/user").Handle(func(ctx *eventctx.Context) error {
		_, err := ctx.Reply(platform.TextMessage(fmt.Sprintf("你的用户ID: %s", ctx.GetSenderInfo().ID)))
		return err
	})

	logger.Info("[CommandBot] Commands registered: /weather, /calc, /search, /help, /user")
}
