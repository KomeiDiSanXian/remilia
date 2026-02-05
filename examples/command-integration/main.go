package main

import (
	"log"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/config"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/middleware"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// 命令集成测试示例

func main() {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	logCfg := logger.Config{
		Level:      cfg.Log.Level,
		Console:    true,
		File:       false,
		TimeFormat: "2006-01-02 15:04:05",
	}
	logger.Init(logCfg)

	botInfo := &dto.BotInfo{
		QQNum:     cfg.Bot.BotID,
		AppID:     cfg.Bot.AppID,
		Token:     cfg.Bot.Token,
		AppSecret: cfg.Bot.Secret,
	}

	bot, _ := remilia.NewBotBuilder().
		WithBotInfo(botInfo).
		WithWebhook(":8080").
		Build()

	bot.Engine().Use(middleware.DevelopmentSet()...)

	// 测试命令集成
	bot.Engine().OnCommand(dto.C2CMessageCreate, "/test").Handle(func(ctx *eventctx.Context) error {
		var c2c dto.C2CMessageCreateEvent
		ctx.DecodeEvent(&c2c)
		msg := &dto.Message{Type: dto.TextMessage, Content: "Command integration test OK"}
		ctx.ReplyPrivate(msg)
		return nil
	})

	logger.Info("[CommandIntegration] Started")
	bot.Start()
	bot.WaitForShutdown()
}
