package main

import (
	"log"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/config"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/middleware"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
)

func main() {
	// 加载配置文件
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v\nPlease copy config.example.yaml to config.yaml and fill in your bot info", err)
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
		QQNum:     cfg.Bot.BotID,
		AppID:     cfg.Bot.AppID,
		Token:     cfg.Bot.Token,
		AppSecret: cfg.Bot.Secret,
	}

	// 使用 BotBuilder 创建 Bot
	bot, err := remilia.NewBotBuilder().
		WithBotInfo(botInfo).
		WithWebhook(":8080").
		WithName("basic-bot").
		Build()
	if err != nil {
		log.Fatalf("Failed to build bot: %v", err)
	}

	// 使用开发环境中间件集
	bot.Engine().Use(middleware.DevelopmentSet()...)

	// 注册命令处理器
	registerHandlers(bot.Engine())

	// 启动 Bot
	logger.Info("[BasicBot] Starting bot...")
	if err := bot.Start(); err != nil {
		logger.WithError(err).Fatal("[BasicBot] Failed to start bot")
	}

	logger.Info("[BasicBot] Bot started successfully! Press Ctrl+C to stop")

	// 等待优雅关闭
	bot.WaitForShutdown()
	logger.Info("[BasicBot] Bot stopped")
}

func registerHandlers(eng *engine.Engine) {
	// Echo 命令 - 回显用户消息
	eng.OnCommand(dto.C2CMessageCreate, "/echo").Handle(func(ctx *eventctx.Context) error {
		var c2c dto.C2CMessageCreateEvent
		if err := ctx.DecodeEvent(&c2c); err != nil {
			return err
		}
		return ctx.Reply(platform.TextMessage("回声: " + c2c.Content))
	})

	// Ping 命令
	eng.OnCommand(dto.C2CMessageCreate, "/ping").Handle(func(ctx *eventctx.Context) error {
		return ctx.Reply(platform.TextMessage("Pong! 🏓"))
	})

	// Help 命令
	eng.OnCommand(dto.C2CMessageCreate, "/help").Handle(func(ctx *eventctx.Context) error {
		help := `可用命令:
/echo <消息> - 回显你的消息
/ping - 测试机器人是否在线
/help - 显示此帮助信息`
		return ctx.Reply(platform.TextMessage(help))
	})

	logger.Info("[BasicBot] Handlers registered")
}
