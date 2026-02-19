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
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
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
		QQNum:     cfg.Bot.BotID,
		AppID:     cfg.Bot.AppID,
		Token:     cfg.Bot.Token,
		AppSecret: cfg.Bot.Secret,
	}

	// 使用 BotBuilder 创建 Bot
	bot, err := remilia.NewBotBuilder().
		WithBotInfo(botInfo).
		WithWebhook(":8080").
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
	// 使用 v2 API 注册插件
	if err := manager.RegisterV2(NewGreeterPlugin()); err != nil {
		logger.WithError(err).Error("[PluginExample] Failed to register greeter plugin")
	}

	if err := manager.RegisterV2(NewCounterPlugin()); err != nil {
		logger.WithError(err).Error("[PluginExample] Failed to register counter plugin")
	}

	logger.Info("[PluginExample] All plugins registered (v2): greeter, counter")
}

// ===== Greeter Plugin (v2 API) =====

func NewGreeterPlugin() *plugin.PluginDescriptor {
	// 使用闭包捕获状态
	greeting := "你好"

	return &plugin.PluginDescriptor{
		Name:        "greeter",
		Version:     "2.0.0",
		Author:      "Remilia",
		Description: "问候插件示例 - 演示 v2 API 的基本用法",
		Category:    "示例",
		Tags:        []string{"greeting", "example", "v2"},
		HelpText:    "/greet - 发送问候\n/setgreeting - 设置问候语",

		Setup: func(ctx *plugin.SetupContext) error {
			logger.Info("[Greeter] Loading plugin (v2)...")

			// 注册 /greet 命令（使用 RegisterCommand 自动追踪）
			ctx.RegisterCommand(dto.C2CMessageCreate, "/greet").Handle(func(c *eventctx.Context) error {
				var c2c dto.C2CMessageCreateEvent
				if err := c.DecodeEvent(&c2c); err != nil {
					return err
				}

				response := fmt.Sprintf("%s, %s!", greeting, c2c.Author.UserOpenID)
				msg := &dto.Message{
					Type:    dto.TextMessage,
					Content: response,
				}
				_, err := c.ReplyPrivate(msg)
				return err
			})

			// 注册 /setgreeting 命令
			ctx.RegisterCommand(dto.C2CMessageCreate, "/setgreeting").Handle(func(c *eventctx.Context) error {
				var c2c dto.C2CMessageCreateEvent
				if err := c.DecodeEvent(&c2c); err != nil {
					return err
				}

				if c2c.Content == "/setgreeting" || c2c.Content == "" {
					msg := &dto.Message{
						Type:    dto.TextMessage,
						Content: "用法: /setgreeting <问候语>",
					}
					_, err := c.ReplyPrivate(msg)
					return err
				}

				// 更新闭包中的状态
				greeting = "Hello"
				msg := &dto.Message{
					Type:    dto.TextMessage,
					Content: "问候语已更新",
				}
				_, err := c.ReplyPrivate(msg)
				return err
			})

			logger.Info("[Greeter] Plugin loaded successfully (v2)")
			return nil
		},

		Teardown: func() error {
			logger.Info("[Greeter] Unloading plugin (v2)...")
			return nil
		},
	}
}

// ===== Counter Plugin (v2 API) =====

func NewCounterPlugin() *plugin.PluginDescriptor {
	// 使用闭包捕获状态
	var count atomic.Int64

	return &plugin.PluginDescriptor{
		Name:        "counter",
		Version:     "2.0.0",
		Author:      "Remilia",
		Description: "计数器插件示例 - 演示 v2 API 的状态管理",
		Category:    "示例",
		Tags:        []string{"counter", "example", "v2"},
		HelpText:    "/count - 增加计数\n/reset - 重置计数\n/stats - 查看统计",

		Setup: func(ctx *plugin.SetupContext) error {
			logger.Info("[Counter] Loading plugin (v2)...")

			// 注册 /count 命令
			ctx.RegisterCommand(dto.C2CMessageCreate, "/count").Handle(func(c *eventctx.Context) error {
				var c2c dto.C2CMessageCreateEvent
				if err := c.DecodeEvent(&c2c); err != nil {
					return err
				}

				currentCount := count.Add(1)
				msg := &dto.Message{
					Type:    dto.TextMessage,
					Content: fmt.Sprintf("计数: %d", currentCount),
				}
				_, err := c.ReplyPrivate(msg)
				return err
			})

			// 注册 /reset 命令
			ctx.RegisterCommand(dto.C2CMessageCreate, "/reset").Handle(func(c *eventctx.Context) error {
				var c2c dto.C2CMessageCreateEvent
				if err := c.DecodeEvent(&c2c); err != nil {
					return err
				}

				count.Store(0)
				msg := &dto.Message{
					Type:    dto.TextMessage,
					Content: "计数已重置",
				}
				_, err := c.ReplyPrivate(msg)
				return err
			})

			// 注册 /stats 命令
			ctx.RegisterCommand(dto.C2CMessageCreate, "/stats").Handle(func(c *eventctx.Context) error {
				var c2c dto.C2CMessageCreateEvent
				if err := c.DecodeEvent(&c2c); err != nil {
					return err
				}

				currentCount := count.Load()
				msg := &dto.Message{
					Type:    dto.TextMessage,
					Content: fmt.Sprintf("统计信息:\n当前计数: %d", currentCount),
				}
				_, err := c.ReplyPrivate(msg)
				return err
			})

			logger.Info("[Counter] Plugin loaded successfully (v2)")
			return nil
		},

		Teardown: func() error {
			logger.Info("[Counter] Unloading plugin (v2)...")
			return nil
		},
	}
}
