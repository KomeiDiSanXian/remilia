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
	// 1. Greeter 插件
	greeter := NewGreeterPlugin(eng)
	if err := manager.Register(greeter); err != nil {
		logger.WithError(err).Error("[PluginExample] Failed to register greeter plugin")
	}

	// 2. Counter 插件
	counter := NewCounterPlugin(eng)
	if err := manager.Register(counter); err != nil {
		logger.WithError(err).Error("[PluginExample] Failed to register counter plugin")
	}

	logger.Info("[PluginExample] All plugins registered: greeter, counter")
}

// ===== Greeter Plugin =====

type GreeterPlugin struct {
	*plugin.BasePlugin
	greeting string
	engine   *engine.Engine
}

func NewGreeterPlugin(eng *engine.Engine) *GreeterPlugin {
	return &GreeterPlugin{
		BasePlugin: plugin.NewBasePlugin("greeter"),
		greeting:   "你好",
		engine:     eng,
	}
}

func (p *GreeterPlugin) Load(eng *engine.Engine) error {
	logger.Info("[Greeter] Loading plugin...")

	// 注册 /greet 命令
	eng.OnCommand(dto.C2CMessageCreate, "/greet").Handle(func(ctx *eventctx.Context) error {
		var c2c dto.C2CMessageCreateEvent
		if err := ctx.DecodeEvent(&c2c); err != nil {
			return err
		}

		response := fmt.Sprintf("%s, %s!", p.greeting, c2c.Author.UserOpenID)
		msg := &dto.Message{
			Type:    dto.TextMessage,
			Content: response,
		}
		_, err := ctx.ReplyPrivate(msg)
		return err
	})

	// 注册 /setgreeting 命令
	eng.OnCommand(dto.C2CMessageCreate, "/setgreeting").Handle(func(ctx *eventctx.Context) error {
		var c2c dto.C2CMessageCreateEvent
		if err := ctx.DecodeEvent(&c2c); err != nil {
			return err
		}

		// 简单实现：从Content中提取新问候语
		if c2c.Content == "/setgreeting" || c2c.Content == "" {
			msg := &dto.Message{
				Type:    dto.TextMessage,
				Content: "用法: /setgreeting <问候语>",
			}
			_, err := ctx.ReplyPrivate(msg)
			return err
		}

		// 这里简化处理，实际应该解析参数
		p.greeting = "Hello"
		msg := &dto.Message{
			Type:    dto.TextMessage,
			Content: "问候语已更新",
		}
		_, err := ctx.ReplyPrivate(msg)
		return err
	})

	logger.Info("[Greeter] Plugin loaded successfully")
	return nil
}

func (p *GreeterPlugin) Unload(eng *engine.Engine) error {
	logger.Info("[Greeter] Unloading plugin...")
	// 清理资源
	return nil
}

// ===== Counter Plugin =====

type CounterPlugin struct {
	*plugin.BasePlugin
	count  atomic.Int64
	engine *engine.Engine
}

func NewCounterPlugin(eng *engine.Engine) *CounterPlugin {
	return &CounterPlugin{
		BasePlugin: plugin.NewBasePlugin("counter"),
		engine:     eng,
	}
}

func (p *CounterPlugin) Load(eng *engine.Engine) error {
	logger.Info("[Counter] Loading plugin...")

	// 注册 /count 命令
	eng.OnCommand(dto.C2CMessageCreate, "/count").Handle(func(ctx *eventctx.Context) error {
		var c2c dto.C2CMessageCreateEvent
		if err := ctx.DecodeEvent(&c2c); err != nil {
			return err
		}

		count := p.count.Add(1)
		msg := &dto.Message{
			Type:    dto.TextMessage,
			Content: fmt.Sprintf("计数: %d", count),
		}
		_, err := ctx.ReplyPrivate(msg)
		return err
	})

	// 注册 /reset 命令
	eng.OnCommand(dto.C2CMessageCreate, "/reset").Handle(func(ctx *eventctx.Context) error {
		var c2c dto.C2CMessageCreateEvent
		if err := ctx.DecodeEvent(&c2c); err != nil {
			return err
		}

		p.count.Store(0)
		msg := &dto.Message{
			Type:    dto.TextMessage,
			Content: "计数已重置",
		}
		_, err := ctx.ReplyPrivate(msg)
		return err
	})

	// 注册 /stats 命令
	eng.OnCommand(dto.C2CMessageCreate, "/stats").Handle(func(ctx *eventctx.Context) error {
		var c2c dto.C2CMessageCreateEvent
		if err := ctx.DecodeEvent(&c2c); err != nil {
			return err
		}

		count := p.count.Load()
		msg := &dto.Message{
			Type:    dto.TextMessage,
			Content: fmt.Sprintf("统计信息:\n当前计数: %d", count),
		}
		_, err := ctx.ReplyPrivate(msg)
		return err
	})

	logger.Info("[Counter] Plugin loaded successfully")
	return nil
}

func (p *CounterPlugin) Unload(eng *engine.Engine) error {
	logger.Info("[Counter] Unloading plugin...")
	return nil
}
