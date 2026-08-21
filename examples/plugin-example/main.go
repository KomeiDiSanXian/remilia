package main

import (
	"fmt"
	"log"
	"sync/atomic"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/config"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/middleware"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/plugin"
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

	// 使用 BotBuilder 创建 Bot
	adapter := qq.NewWebhookServerAdapter(":8080", botInfo)
	bot, err := remilia.NewBotBuilder().
		WithPlatformAdapter(adapter).
		WithName("plugin-example").
		Build()
	if err != nil {
		log.Fatalf("Failed to build bot: %v", err)
	}

	// 使用开发环境中间件
	bot.Engine().Use(middleware.DevelopmentSet()...)

	// 创建插件管理器
	manager := plugin.NewManager(bot.Engine())

	// 注册插件
	registerPlugins(manager, bot.Engine())

	// 启动 Bot
	logger.Info("[PluginExample] Starting bot with plugins...")
	if err := bot.Start(); err != nil {
		logger.WithError(err).Fatal("[PluginExample] Failed to start bot")
	}

	logger.Info("[PluginExample] Bot started! Try commands: /greet, /count, /stats")
	bot.WaitForShutdown()
	logger.Info("[PluginExample] Bot stopped")
}

func registerPlugins(manager *plugin.Manager, eng *engine.Engine) {
	// 注册插件
	if err := manager.Register(NewGreeterPlugin()); err != nil {
		logger.WithError(err).Error("[PluginExample] Failed to register greeter plugin")
	}

	if err := manager.Register(NewCounterPlugin()); err != nil {
		logger.WithError(err).Error("[PluginExample] Failed to register counter plugin")
	}

	logger.Info("[PluginExample] All plugins registered: greeter, counter")
}

// ===== Greeter Plugin (v2 API) =====

func NewGreeterPlugin() *plugin.Descriptor {
	greeting := "你好"

	return &plugin.Descriptor{
		Name:    "greeter",
		Version: "2.0.0",
		Meta: &plugin.Metadata{
			Author:      "Remilia",
			Description: "问候插件示例 - 演示 v2 API 的基本用法",
			Category:    "示例",
			Tags:        []string{"greeting", "example", "v2"},
			HelpText:    "/greet - 发送问候\n/setgreeting - 设置问候语",
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			logger.Info("[Greeter] Loading plugin (v2)...")

			ctx.Reg.RegisterCommand("", "/greet").Handle(func(c *eventctx.Context) error {
				c.Reply(platform.TextMessage(fmt.Sprintf("%s, %s!", greeting, c.GetSenderInfo().ID)))
				return nil
			})

			ctx.Reg.RegisterCommand("", "/setgreeting").Handle(func(c *eventctx.Context) error {
				content := c.GetMessageContent()
				if content == "/setgreeting" || content == "" {
					c.Reply(platform.TextMessage("用法: /setgreeting <问候语>"))
					return nil
				}
				greeting = "Hello"
				c.Reply(platform.TextMessage("问候语已更新"))
				return nil
			})

			logger.Info("[Greeter] Plugin loaded successfully (v2)")
			return nil, nil
		},
		Teardown: func(ctx *plugin.TeardownContext) error {
			ctx.Log.Info("Greeter plugin unloaded")
			return nil
		},
	}
}

// ===== Counter Plugin (v2 API) =====

func NewCounterPlugin() *plugin.Descriptor {
	var count atomic.Int64

	return &plugin.Descriptor{
		Name:    "counter",
		Version: "2.0.0",
		Meta: &plugin.Metadata{
			Author:      "Remilia",
			Description: "计数器插件示例 - 演示 v2 API 的状态管理",
			Category:    "示例",
			Tags:        []string{"counter", "example", "v2"},
			HelpText:    "/count - 增加计数\n/reset - 重置计数\n/stats - 查看统计",
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			logger.Info("[Counter] Loading plugin (v2)...")

			ctx.Reg.RegisterCommand("", "/count").Handle(func(c *eventctx.Context) error {
				currentCount := count.Add(1)
				c.Reply(platform.TextMessage(fmt.Sprintf("计数: %d", currentCount)))
				return nil
			})

			ctx.Reg.RegisterCommand("", "/reset").Handle(func(c *eventctx.Context) error {
				count.Store(0)
				c.Reply(platform.TextMessage("计数已重置"))
				return nil
			})

			ctx.Reg.RegisterCommand("", "/stats").Handle(func(c *eventctx.Context) error {
				c.Reply(platform.TextMessage(fmt.Sprintf("当前计数: %d", count.Load())))
				return nil
			})

			logger.Info("[Counter] Plugin loaded successfully (v2)")
			return nil, nil
		},
		Teardown: func(ctx *plugin.TeardownContext) error {
			ctx.Log.Info("Counter plugin unloaded")
			return nil
		},
	}
}
