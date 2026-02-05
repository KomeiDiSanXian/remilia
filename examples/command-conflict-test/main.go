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

// 命令冲突测试示例

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

	// 测试命令 - 第一个注册的会被使用
	bot.Engine().OnCommand(dto.C2CMessageCreate, "/conflict").Handle(func(ctx *eventctx.Context) error {
		var c2c dto.C2CMessageCreateEvent
		ctx.DecodeEvent(&c2c)
		msg := &dto.Message{Type: dto.TextMessage, Content: "Handler 1"}
		ctx.ReplyPrivate(msg)
		return nil
	})

	// 同名命令（会被忽略或覆盖，取决于实现）
	bot.Engine().OnCommand(dto.C2CMessageCreate, "/conflict").Handle(func(ctx *eventctx.Context) error {
		var c2c dto.C2CMessageCreateEvent
		ctx.DecodeEvent(&c2c)
		msg := &dto.Message{Type: dto.TextMessage, Content: "Handler 2"}
		ctx.ReplyPrivate(msg)
		return nil
	})

	logger.Info("[ConflictTest] Started - Try /conflict")
	bot.Start()
	bot.WaitForShutdown()
}
