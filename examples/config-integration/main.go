package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/config"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/openapi/auth/token"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// 这个示例展示如何使用配置系统来创建和配置 Bot 的各个组件
func main() {
	// 初始化日志
	logger.InitDefault()

	// 1. 加载配置
	cfg, err := config.LoadDefault()
	if err != nil {
		logger.Fatalf("Failed to load config: %v", err)
	}

	// 打印关键配置信息
	logger.Infof("=== Configuration Loaded ===")
	logger.Infof("Webhook workers: %d", cfg.Webhook.WorkerCount)
	logger.Infof("Event buffer: %d", cfg.Webhook.EventBuffer)
	logger.Infof("Token retry delay: %s", cfg.Token.RetryDelay)
	logger.Infof("Token refresh advance: %s", cfg.Token.RefreshAdvance)
	logger.Infof("Engine cleanup interval: %s", cfg.Engine.TempMatcherCleanupInterval)
	logger.Infof("Engine pending delete buffer: %d", cfg.Engine.PendingDeleteBufferSize)

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
	logger.Info("Waiting for token to be ready...")
	tokenMgr.WaitReady()
	logger.Info("✓ Token is ready")

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
	logger.Info("Starting bot...")
	if err := bot.Start(); err != nil {
		logger.Fatalf("Failed to start bot: %v", err)
	}

	logger.Info("✓ Bot started successfully")
	logger.Infof("Listening on %s", addr)
	logger.Info("Press Ctrl+C to stop")

	// 9. 等待退出信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down...")

	// 10. 优雅关闭
	if err := bot.Shutdown(); err != nil {
		logger.WithError(err).Error("Shutdown error")
	}

	logger.Info("✓ Shutdown complete")
}
