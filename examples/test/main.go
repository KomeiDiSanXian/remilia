package main

import (
	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/config"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/global"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/sirupsen/logrus"
)

func main() {
	logrus.SetLevel(logrus.DebugLevel)
	logrus.SetFormatter(&logrus.TextFormatter{
		ForceColors:   true,
		FullTimestamp: true,
	})
	// 加载配置·
	cfg, err := config.LoadDefault()
	if err != nil {
		panic("Failed to load config: " + err.Error())
	}
	global.MustInitFromConfig(cfg)
	logrus.Infof("Bot info: %+v", global.Info)
	// 创建 Engine
	eng := engine.NewEngine()
	// 注册处理器
	eng.OnCommand(dto.C2CMessageCreate, "/echo").Handle(func(ctx *eventctx.Context) error {
		var c2c dto.C2CMessageCreateEvent
		if err := ctx.DecodeEvent(&c2c); err != nil {
			return err
		}
		logrus.Infof("Received message: %+v", c2c.Content)
		msg := &dto.Message{
			Type:    dto.TextMessage,
			Content: "收到消息: " + c2c.Content,
		}
		_, err := ctx.ReplyPrivate(msg)
		return err
	})
	// 创建内置 HTTP 服务器的 Webhook 适配器
	adapter := remilia.NewWebhookServerAdapter(":9000", global.Info)
	// 创建 Bot - 使用 NewBotWithInfo 自动初始化 OpenAPI client
	bot := remilia.NewBotWithInfo(adapter, eng, global.Info)
	// 启动 Bot
	logrus.Info("Starting bot...")
	if err := bot.Start(); err != nil {
		logrus.WithError(err).Fatal("Failed to start bot")
	}
	logrus.Info("Bot is running on :9000")
	// 等待退出信号
	bot.WaitForShutdown()
}
