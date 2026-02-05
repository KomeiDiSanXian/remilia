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

// 简化的Help Discovery示例
// 展示如何自动发现和列出所有可用命令

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

	// 注册一些命令
	bot.Engine().OnCommand(dto.C2CMessageCreate, "/cmd1").Handle(simpleHandler("Command 1"))
	bot.Engine().OnCommand(dto.C2CMessageCreate, "/cmd2").Handle(simpleHandler("Command 2"))
	bot.Engine().OnCommand(dto.C2CMessageCreate, "/cmd3").Handle(simpleHandler("Command 3"))

	// Help命令 - 列出所有命令
	bot.Engine().OnCommand(dto.C2CMessageCreate, "/help").Handle(func(ctx *eventctx.Context) error {
		var c2c dto.C2CMessageCreateEvent
		ctx.DecodeEvent(&c2c)

		help := "可用命令:\n/cmd1 - 命令1\n/cmd2 - 命令2\n/cmd3 - 命令3\n/help - 帮助"
		msg := &dto.Message{Type: dto.TextMessage, Content: help}
		ctx.ReplyPrivate(msg)
		return nil
	})

	logger.Info("[HelpDiscovery] Started")
	bot.Start()
	bot.WaitForShutdown()
}

func simpleHandler(name string) eventctx.Handler {
	return func(ctx *eventctx.Context) error {
		var c2c dto.C2CMessageCreateEvent
		ctx.DecodeEvent(&c2c)
		msg := &dto.Message{Type: dto.TextMessage, Content: name + " executed"}
		ctx.ReplyPrivate(msg)
		return nil
	}
}
