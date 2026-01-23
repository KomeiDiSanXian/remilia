package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/config"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/global"
	"github.com/KomeiDiSanXian/remilia/middleware"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/sirupsen/logrus"
)

func main() {
	// 设置日志级别
	logrus.SetLevel(logrus.DebugLevel)
	logrus.SetFormatter(&logrus.TextFormatter{
		ForceColors:   true,
		FullTimestamp: true,
	})

	// 创建 Engine
	eng := engine.NewEngine()

	// 添加全局中间件
	eng.Use(
		middleware.Logging(), // 日志记录
		middleware.Recover(), // Panic 恢复
		middleware.Metrics(), // 指标收集
	)

	// 注册命令处理器
	registerHandlers(eng)
	cfg, err := config.LoadDefault()
	if err != nil {
		panic("Failed to load config: " + err.Error())
	}
	global.MustInitFromConfig(cfg)
	global.Info.ServeAddr = "127.0.0.1:9000"
	// 创建 Bot
	bot := remilia.NewBotWithDefault(global.Info)

	// 启动 Bot
	logrus.Info("Starting bot...")
	if err := bot.Start(); err != nil {
		logrus.WithError(err).Fatal("Failed to start bot")
	}

	logrus.Info("Press Ctrl+C to stop")

	// 等待退出信号
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	logrus.Info("Shutting down gracefully...")
	_ = bot.Shutdown()
	logrus.Info("Bot stopped")
}

func registerHandlers(eng *engine.Engine) {
	eng.Use(middleware.ErrorHandler(func(ctx *eventctx.Context, err error) {
		logrus.WithError(err).Error("Handler failed")
	}))
	// 1. Echo 命令 - 回显用户消息
	eng.OnCommand(dto.C2CMessageCreate, "/echo").Handle(func(context *eventctx.Context) error {
		var c2c dto.C2CMessageCreateEvent
		if err := context.DecodeEvent(&c2c); err != nil {
			return err
		}
		msg := &dto.Message{
			Type:    dto.TextMessage,
			Content: c2c.Content,
		}
		_, err := context.ReplyPrivate(msg)
		if err != nil {
			return err
		}
		return nil
	})

	// 2. Ping 命令 - 测试机器人是否在线
	//eng.OnCommand("/ping", func(ctx *eventctx.Context) error {
	//	return ctx.Reply("Pong! 🏓")
	//})

	//	// 3. Help 命令 - 显示帮助信息
	//	eng.OnCommand("/help", func(ctx *eventctx.Context) error {
	//		help := `可用命令:
	///echo <消息> - 回显你的消息
	///ping - 测试机器人是否在线
	///help - 显示此帮助信息
	///info - 显示机器人信息
	///status - 显示系统状态`
	//		return ctx.Reply(help)
	//	})
	//
	//	// 4. Info 命令 - 显示机器人信息
	//	eng.OnCommand("/info", func(ctx *eventctx.Context) error {
	//		info := `Remilia Bot
	//版本: 0.9.0
	//框架: Remilia Framework
	//Go 版本: 1.21+`
	//		return ctx.Reply(info)
	//	})
	//
	//	// 5. Status 命令 - 显示系统状态
	//	eng.OnCommand("/status", func(ctx *eventctx.Context) error {
	//		// 这里可以获取真实的系统状态
	//		status := `系统状态: ✅ 正常
	//运行时间: [待实现]
	//消息处理: [待实现]
	//内存使用: [待实现]`
	//		return ctx.Reply(status)
	//	})
	//
	//	// 6. At 消息处理器 - 当机器人被 @ 时
	//	eng.OnAtMessage(func(ctx *eventctx.Context) error {
	//		text := ctx.GetPlainText()
	//		if text == "" {
	//			return ctx.Reply("你好！发送 /help 查看可用命令")
	//		}
	//		// 简单的智能回复
	//		return ctx.Reply("收到你的消息: " + text)
	//	})
	//
	//	// 7. 私聊消息处理器
	//	eng.OnDirectMessage(func(ctx *eventctx.Context) error {
	//		text := ctx.GetPlainText()
	//		logrus.WithField("text", text).Info("Received direct message")
	//		return ctx.Reply("你好！这是私聊回复。发送 /help 查看命令列表")
	//	})
	//
	//	// 8. 所有消息的默认处理器（低优先级）
	//	eng.OnMessage(func(ctx *eventctx.Context) error {
	//		// 记录日志但不回复
	//		logrus.WithFields(logrus.Fields{
	//			"type":   ctx.GetEventType(),
	//			"author": ctx.GetAuthor(),
	//		}).Debug("Received message")
	//		return nil
	//	})
}
