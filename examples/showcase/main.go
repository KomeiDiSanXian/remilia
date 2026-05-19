// Package main 是 showcase 示例程序的入口。
//
// 此示例演示了 Remilia 框架的以下核心特性：
//   - 路由层架构（Router → Engine）
//   - FSM 有限状态机（内置一级路由）
//   - Per-Channel Block 隔离（通过 Matcher.BlockForChannel）
//   - 插件系统（25+ 内置插件）
//   - 中间件链（去重、限流、超时追踪）
//   - 事件总线与调度器
package main

import (
	"context"
	"embed"
	"log"
	"time"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/config"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform/qq"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
)

//go:embed locales/*.yaml
var localeFS embed.FS

func main() {
	// ── 加载配置 ────────────────────────────────────────────────────────────
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if err := logger.Init(logger.Config{
		Level: cfg.Log.Level, Console: true, TimeFormat: "2006-01-02 15:04:05",
	}); err != nil {
		log.Fatalf("init logger: %v", err)
	}

	// ── 性能分析 ────────────────────────────────────────────────────────────
	pprofSrv := remilia.NewPprofServer(remilia.PprofConfig{
		Enabled: true, Addr: "localhost:9001",
		AutoProfile: true, ProfileInterval: 30 * time.Minute,
		ProfileDuration: 30 * time.Second, OutputDir: "./profiles",
		EnableMutex: true, EnableBlock: true,
	})
	if err := pprofSrv.Start(); err != nil {
		log.Fatalf("start pprof: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = pprofSrv.Stop(ctx)
	}()

	// ── 构建 Bot ────────────────────────────────────────────────────────────
	bot, err := remilia.NewBotBuilder().
		WithPlatformAdapter(qq.NewWebhookServerAdapter(":9000", &dto.BotInfo{
			QQNum: cfg.Bot.QQ.BotID, AppID: cfg.Bot.QQ.AppID,
			Token: cfg.Bot.QQ.Token, AppSecret: cfg.Bot.QQ.Secret,
		})).
		WithName("showcase-bot").WithVersion("1.0.0").
		Build()
	if err != nil {
		log.Fatalf("build bot: %v", err)
	}

	// ── 初始化各组件 ─────────────────────────────────────────────────────────
	eng := bot.Engine()
	setupMiddleware(eng)
	fsmMgr := setupRouter(bot, eng)
	pm := setupPluginManager(bot, eng)
	setupPlugins(pm, eng)
	loadLocales(pm)
	registerCommands(pm)
	registerSignupFSM(fsmMgr, pm)

	// WASM 插件演示（使用程序生成的 WASM 模块加载到 Engine 中）
	setupWasmDemo(pm, eng)

	logger.Info("[showcase] Starting... send /help to see all commands")
	if err := bot.Start(); err != nil {
		logger.WithError(err).Fatal("[showcase] stopped")
	}
	bot.WaitForShutdown()
}
