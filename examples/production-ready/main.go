package main

import (
	"log"
	"time"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/config"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/middleware"
	"github.com/KomeiDiSanXian/remilia/platform"
	qq "github.com/KomeiDiSanXian/remilia/platform/qq"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
)

// 生产环境示例
// 展示生产环境的最佳实践配置

func main() {
	// 加载配置
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v\nPlease copy config.example.yaml to config.yaml", err)
	}

	// 初始化日志（生产环境配置）
	logCfg := logger.Config{
		Level:      cfg.Log.Level, // 使用 info 或 warn
		Console:    true,          // 控制台输出
		File:       false,         // 简化：不输出到文件
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

	// 使用 BotBuilder 创建 Bot（生产环境配置）
	adapter := qq.NewWebhookServerAdapter(":8080", botInfo)
	bot, err := remilia.NewBotBuilder().
		WithPlatformAdapter(adapter).
		WithName("production-bot").
		WithVersion("1.0.0").
		Build()
	if err != nil {
		log.Fatalf("Failed to build bot: %v", err)
	}

	// 使用生产环境中间件集
	// 包含: Recover + Logging + Adaptive + CircuitBreaker + Dedup
	bot.Engine().Use(middleware.ProductionSet()...)

	// 添加自定义错误处理中间件
	bot.Engine().Use(errorHandlerMiddleware())

	// 添加慢请求检测中间件
	bot.Engine().Use(slowRequestMiddleware(3 * time.Second))

	// 注册业务处理器
	registerHandlers(bot)

	// 启动健康检查
	startHealthCheck(bot)

	// 启动 Bot
	logger.Info("[Production] Starting bot...")
	logger.WithFields(logger.Fields{
		"name":    "production-bot",
		"version": "1.0.0",
		"port":    8080,
	}).Info("[Production] Bot configuration")

	if err := bot.Start(); err != nil {
		logger.WithError(err).Fatal("[Production] Failed to start bot")
	}

	logger.Info("[Production] Bot started successfully!")
	logger.Info("[Production] Health check available at /health")
	logger.Info("[Production] Press Ctrl+C to stop")

	// 优雅关闭
	bot.WaitForShutdown()

	// 清理资源
	logger.Info("[Production] Shutting down...")
	if err := bot.Shutdown(); err != nil {
		logger.WithError(err).Error("[Production] Shutdown error")
	}

	logger.Info("[Production] Bot stopped")
}

// registerHandlers 注册业务处理器
func registerHandlers(bot *remilia.Bot) {
	// Ping命令 - 健康检查
	bot.Engine().OnCommand("", "/ping").Handle(func(ctx *eventctx.Context) error {
		return ctx.Reply(platform.TextMessage("Pong! Bot is healthy."))
	})

	// Status命令 - 系统状态
	bot.Engine().OnCommand("", "/status").Handle(func(ctx *eventctx.Context) error {
		status := "✅ System Status: Healthy\n"
		status += "📊 Uptime: Running\n"
		status += "🔧 Version: 1.0.0\n"
		status += "🌐 Environment: Production"
		return ctx.Reply(platform.TextMessage(status))
	})

	// Help命令
	bot.Engine().OnCommand("", "/help").Handle(func(ctx *eventctx.Context) error {
		help := "🤖 生产环境Bot\n\n"
		help += "可用命令:\n"
		help += "/ping - 健康检查\n"
		help += "/status - 系统状态\n"
		help += "/help - 帮助信息"
		return ctx.Reply(platform.TextMessage(help))
	})

	logger.Info("[Production] Handlers registered")
}

// errorHandlerMiddleware 自定义错误处理中间件
func errorHandlerMiddleware() eventctx.Middleware {
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			err := next(ctx)
			if err != nil {
				// 记录错误
				logger.WithError(err).Error("[Production] Handler error")

				// 可以在这里添加错误上报到监控系统
				// reportToMonitoring(err, ctx)
			}
			return err
		}
	}
}

// slowRequestMiddleware 慢请求检测中间件
func slowRequestMiddleware(threshold time.Duration) eventctx.Middleware {
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			start := time.Now()
			err := next(ctx)
			duration := time.Since(start)

			if duration > threshold {
				logger.WithFields(logger.Fields{
					"duration":  duration,
					"threshold": threshold,
				}).Warn("[Production] Slow request detected")
			}

			return err
		}
	}
}

// startHealthCheck 启动健康检查
func startHealthCheck(bot *remilia.Bot) {
	// 这里可以启动一个HTTP服务提供健康检查端点
	// 或者定期检查Bot状态并上报到监控系统
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			// 检查Bot健康状态
			if bot.IsRunning() {
				logger.Debug("[Production] Health check: OK")
			} else {
				logger.Warn("[Production] Health check: Bot not running")
			}
		}
	}()
}
