package main

import (
	"log"
	"time"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/config"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/middleware"
	"github.com/KomeiDiSanXian/remilia/platform"
	qq "github.com/KomeiDiSanXian/remilia/platform/qq"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
)

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
		QQNum:     cfg.Bot.QQ.BotID,
		AppID:     cfg.Bot.QQ.AppID,
		Token:     cfg.Bot.QQ.Token,
		AppSecret: cfg.Bot.QQ.Secret,
	}

	// 创建 Bot
	adapter := qq.NewWebhookServerAdapter(":8080", botInfo)
	bot, err := remilia.NewBotBuilder().
		WithPlatformAdapter(adapter).
		WithName("middleware-example").
		Build()
	if err != nil {
		log.Fatalf("Failed to build bot: %v", err)
	}

	// ===== 方式1: 使用预定义中间件集 =====
	logger.Info("[MiddlewareExample] Using production middleware set")
	bot.Engine().Use(middleware.ProductionSet()...)

	// ===== 方式2: 使用简化工厂（注释掉方式1后使用） =====
	// logger.Info("[MiddlewareExample] Using simplified middleware factories")
	// bot.engine().Use(
	// 	middleware.Recover(),                    // Panic 恢复
	// 	middleware.Logging(),                    // 日志记录
	// 	middleware.SimpleAdaptive(),             // 自适应限流（默认配置）
	// 	middleware.SimpleCircuitBreaker(),       // 熔断器（默认配置）
	// 	middleware.SimpleDedup(),                // 去重（默认配置）
	// )

	// ===== 方式3: 使用中间件构建器（注释掉方式1后使用） =====
	// logger.Info("[MiddlewareExample] Using middleware builder")
	// middlewares := middleware.NewMiddlewareSet().
	// 	WithRecover().
	// 	WithLogging().
	// 	WithAdaptive().
	// 	WithCircuitBreaker().
	// 	WithDedup().
	// 	Build()
	// bot.engine().Use(middlewares...)

	// ===== 方式4: 自定义中间件配置（注释掉方式1后使用） =====
	// logger.Info("[MiddlewareExample] Using custom middleware configuration")
	// bot.engine().Use(
	// 	middleware.Recover(),
	// 	middleware.Logging(),
	// 	middleware.SimpleAdaptiveWithLimit(200),             // 自定义并发限制
	// 	middleware.SimpleDedupWithTTL(10*time.Minute),       // 自定义去重TTL
	// )

	// 注册处理器
	registerHandlers(bot.Engine())

	// 启动 Bot
	logger.Info("[MiddlewareExample] Starting bot...")
	if err := bot.Start(); err != nil {
		logger.WithError(err).Fatal("[MiddlewareExample] Failed to start bot")
	}

	logger.Info("[MiddlewareExample] Bot started! Press Ctrl+C to stop")

	// 等待优雅关闭
	bot.WaitForShutdown()
	logger.Info("[MiddlewareExample] Bot stopped")
}

func registerHandlers(eng *engine.Engine) {
	// 快速响应命令
	eng.OnCommand("", "/fast").Handle(func(ctx *eventctx.Context) error {
		ctx.Reply(platform.TextMessage("⚡ 快速响应！"))
		return nil
	})

	// 慢速响应命令（模拟耗时操作）
	eng.OnCommand("", "/slow").Handle(func(ctx *eventctx.Context) error {
		time.Sleep(2 * time.Second)
		ctx.Reply(platform.TextMessage("🐌 慢速响应（耗时2秒）"))
		return nil
	})

	// 测试重复消息
	eng.OnCommand("", "/dup").Handle(func(ctx *eventctx.Context) error {
		ctx.Reply(platform.TextMessage("此消息会被去重中间件处理"))
		return nil
	})

	// 查看中间件统计
	eng.OnCommand("", "/stats").Handle(func(ctx *eventctx.Context) error {
		ctx.Reply(platform.TextMessage("中间件已启用:\n✓ Panic恢复\n✓ 日志记录\n✓ 自适应限流\n✓ 熔断器\n✓ 去重"))
		return nil
	})

	logger.Info("[MiddlewareExample] Handlers registered")
}
