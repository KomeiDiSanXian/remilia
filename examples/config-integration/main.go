package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/config"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/openapi/auth/token"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/sirupsen/logrus"
)

// 这个示例展示如何使用配置系统来创建和配置 Bot 的各个组件
func main() {
	// 设置日志级别
	logrus.SetLevel(logrus.InfoLevel)

	// 1. 加载配置
	cfg, err := config.LoadDefault()
	if err != nil {
		logrus.Fatalf("Failed to load config: %v", err)
	}

	// 打印关键配置信息
	logrus.Infof("=== Configuration Loaded ===")
	logrus.Infof("Webhook workers: %d", cfg.Webhook.WorkerCount)
	logrus.Infof("Event buffer: %d", cfg.Webhook.EventBuffer)
	logrus.Infof("Token retry delay: %s", cfg.Token.RetryDelay)
	logrus.Infof("Token refresh advance: %s", cfg.Token.RefreshAdvance)
	logrus.Infof("Engine cleanup interval: %s", cfg.Engine.TempMatcherCleanupInterval)
	logrus.Infof("Engine pending delete buffer: %d", cfg.Engine.PendingDeleteBufferSize)

	// 2. 创建 Bot 信息
	botInfo := &dto.BotInfo{
		AppID:     cfg.Bot.AppID,
		QQNum:     cfg.Bot.BotID,
		Token:     cfg.Bot.Token,
		AppSecret: cfg.Bot.Secret,
		ServeAddr: fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
	}

	// 3. 使用配置创建 Token Manager
	tokenMgr := token.NewManagerFromConfig(botInfo, cfg.Token)
	defer tokenMgr.Stop()

	// 等待 Token 就绪
	logrus.Info("Waiting for token to be ready...")
	tokenMgr.WaitReady()
	logrus.Info("✓ Token is ready")

	// 4. 使用配置创建 Engine
	eng := engine.NewEngine(engine.WithConfig(cfg.Engine))

	// 5. 这里可以注册你的处理器
	// 例如：
	// eng.OnCommand(dto.C2CMessageCreate, "/echo").Handle(func(ctx *eventctx.Context) error {
	//     // 处理逻辑
	//     return nil
	// })

	// 6. 使用配置创建 Webhook Adapter
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	adapter := remilia.NewWebhookServerAdapterWithConfig(addr, botInfo, cfg.Webhook)

	// 7. 创建 Bot
	bot := remilia.NewBot(adapter, eng)

	// 8. 启动 Bot
	logrus.Info("Starting bot...")
	if err := bot.Start(); err != nil {
		logrus.Fatalf("Failed to start bot: %v", err)
	}

	logrus.Info("✓ Bot started successfully")
	logrus.Infof("Listening on %s", addr)
	logrus.Info("Press Ctrl+C to stop")

	// 9. 等待退出信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logrus.Info("Shutting down...")

	// 10. 优雅关闭
	if err := bot.Shutdown(); err != nil {
		logrus.WithError(err).Error("Shutdown error")
	}

	logrus.Info("✓ Shutdown complete")
}
