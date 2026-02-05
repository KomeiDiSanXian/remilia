package main

import (
	"fmt"
	"log"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/config"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/middleware"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// 这个示例展示如何使用中间件模式组合多个处理器

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
		QQNum:     cfg.Bot.BotID,
		AppID:     cfg.Bot.AppID,
		Token:     cfg.Bot.Token,
		AppSecret: cfg.Bot.Secret,
	}

	// 创建 Bot
	bot, err := remilia.NewBotBuilder().
		WithBotInfo(botInfo).
		WithWebhook(":8080").
		WithName("handler-chain").
		Build()
	if err != nil {
		log.Fatalf("Failed to build bot: %v", err)
	}

	// 使用开发环境中间件
	bot.Engine().Use(middleware.DevelopmentSet()...)

	// 示例：使用中间件链处理命令
	bot.Engine().OnCommand(dto.C2CMessageCreate, "/chain").Handle(
		// 中间件会按顺序执行
		chainedHandler(),
	)

	logger.Info("[HandlerChain] Bot started! Try: /chain")
	bot.Start()
	bot.WaitForShutdown()
}

// chainedHandler 展示处理器链模式
func chainedHandler() eventctx.Handler {
	return func(ctx *eventctx.Context) error {
		var c2c dto.C2CMessageCreateEvent
		if err := ctx.DecodeEvent(&c2c); err != nil {
			return err
		}

		// 步骤1：验证
		logger.Info("[HandlerChain] Step 1: Validation")

		// 步骤2：处理
		logger.Info("[HandlerChain] Step 2: Processing")

		// 步骤3：响应
		logger.Info("[HandlerChain] Step 3: Response")

		msg := &dto.Message{
			Type:    dto.TextMessage,
			Content: fmt.Sprintf("处理链完成！\n用户: %s", c2c.Author.UserOpenID),
		}
		_, err := ctx.ReplyPrivate(msg)
		return err
	}
}
