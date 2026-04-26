package main

import (
	"fmt"
	"log"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/config"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/middleware"
	"github.com/KomeiDiSanXian/remilia/platform"
	qq "github.com/KomeiDiSanXian/remilia/platform/qq"
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
		WithName("plugin-v2-example").
		Build()
	if err != nil {
		log.Fatalf("Failed to build bot: %v", err)
	}

	// 使用开发环境中间件
	bot.Engine().Use(middleware.DevelopmentSet()...)

	// 创建插件管理器
	manager := plugin.NewManager(bot.Engine())

	// 注册插件（使用 v2 API）
	registerPlugins(manager)

	// 启动 Bot
	logger.Info("[V2Example] Starting bot with v2 plugins...")
	if err := bot.Start(); err != nil {
		logger.WithError(err).Fatal("[V2Example] Failed to start bot")
	}

	logger.Info("[V2Example] Bot started! Try commands: /greet, /count, /calc")
	bot.WaitForShutdown()
	logger.Info("[V2Example] Bot stopped")
}

func registerPlugins(manager *plugin.Manager) {
	// 1. Greeter 插件（v2 风格）
	if err := manager.Register(NewGreeterPlugin()); err != nil {
		logger.WithError(err).Error("[V2Example] Failed to register greeter plugin")
	}

	// 2. Counter 插件（v2 风格）
	if err := manager.Register(NewCounterPlugin()); err != nil {
		logger.WithError(err).Error("[V2Example] Failed to register counter plugin")
	}

	// 3. Calculator 插件（v2 风格，演示更复杂的功能）
	if err := manager.Register(NewCalculatorPlugin()); err != nil {
		logger.WithError(err).Error("[V2Example] Failed to register calculator plugin")
	}

	logger.Info("[V2Example] All v2 plugins registered")
}

// NewGreeterPlugin 创建问候插件（v2 风格）
func NewGreeterPlugin() *plugin.Descriptor {
	greeting := "你好"

	return &plugin.Descriptor{
		Name:    "greeter",
		Version: "2.0.0",
		Meta: &plugin.Metadata{
			Author:      "Remilia Team",
			Description: "简单的问候插件（v2 API 演示）",
			Category:    "示例",
			Tags:        []string{"问候", "演示"},
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			logger.Info("[Greeter] Setting up plugin (v2)...")

			ctx.Reg.RegisterCommand("", "/greet").
				Handle(func(c *eventctx.Context) error {
					_, err := c.Reply(platform.TextMessage(greeting + ", " + c.GetSenderInfo().ID + "!"))
					return err
				})

			ctx.Reg.RegisterCommand("", "/setgreeting").
				Handle(func(c *eventctx.Context) error {
					content := c.GetMessageContent()
					if len(content) <= 13 {
						_, err := c.Reply(platform.TextMessage("用法: /setgreeting <问候语>"))
						return err
					}
					greeting = content[13:]
					_, err := c.Reply(platform.TextMessage("问候语已更新为: " + greeting))
					return err
				})

			logger.Info("[Greeter] Plugin setup complete")
			return nil, nil
		},
		Teardown: func(ctx *plugin.TeardownContext) error {
			ctx.Log.Info("Greeter plugin torn down")
			return nil
		},
	}
}

// NewCounterPlugin 创建计数器插件（v2 风格）
func NewCounterPlugin() *plugin.Descriptor {
	count := 0

	return &plugin.Descriptor{
		Name:    "counter",
		Version: "2.0.0",
		Meta: &plugin.Metadata{
			Author:      "Remilia Team",
			Description: "简单的计数器插件（v2 API 演示）",
			Category:    "示例",
			Tags:        []string{"计数器", "演示"},
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			logger.Info("[Counter] Setting up plugin (v2)...")

			if ctx.Config != nil {
				count = ctx.Config.GetInt("initial_value", 0)
				logger.Infof("[Counter] Initial value from config: %d", count)
			}

			ctx.Reg.RegisterCommand("", "/count").
				Handle(func(c *eventctx.Context) error {
					count++
					_, err := c.Reply(platform.TextMessage(fmt.Sprintf("计数: %d", count)))
					return err
				})

			ctx.Reg.RegisterCommand("", "/reset").
				Handle(func(c *eventctx.Context) error {
					count = 0
					_, err := c.Reply(platform.TextMessage("计数已重置"))
					return err
				})

			ctx.Reg.RegisterCommand("", "/get").
				Handle(func(c *eventctx.Context) error {
					_, err := c.Reply(platform.TextMessage(fmt.Sprintf("当前计数: %d", count)))
					return err
				})

			logger.Info("[Counter] Plugin setup complete")
			return nil, nil
		},
		Teardown: func(ctx *plugin.TeardownContext) error {
			ctx.Log.Infof("Counter plugin torn down (final count: %d)", count)
			return nil
		},
		Advanced: &plugin.Advanced{
			Reload: func(ctx *plugin.SetupContext) error {
				logger.Info("[Counter] Reloading plugin...")
				return nil
			},
		},
	}
}

// NewCalculatorPlugin 创建计算器插件（v2 风格）
func NewCalculatorPlugin() *plugin.Descriptor {
	return &plugin.Descriptor{
		Name:    "calculator",
		Version: "2.0.0",
		Deps:    []string{},
		Meta: &plugin.Metadata{
			Author:      "Remilia Team",
			Description: "简单的计算器插件（v2 API 演示）",
			Category:    "工具",
			Tags:        []string{"计算器", "工具"},
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			logger.Info("[Calculator] Setting up plugin (v2)...")

			ctx.Reg.RegisterCommand("", "/calc").
				Handle(func(c *eventctx.Context) error {
					content := c.GetMessageContent()
					if len(content) <= 6 {
						_, err := c.Reply(platform.TextMessage("用法: /calc <表达式>\n示例: /calc 1 + 2"))
						return err
					}
					expr := content[6:]
					result, err := simpleCalc(expr)
					if err != nil {
						_, err = c.Reply(platform.TextMessage("计算错误: " + err.Error()))
						return err
					}
					_, err = c.Reply(platform.TextMessage(fmt.Sprintf("%s = %d", expr, result)))
					return err
				})

			logger.Info("[Calculator] Plugin setup complete")
			return nil, nil
		},
	}
}

// simpleCalc 简单的计算器实现（仅支持两个数字的加减乘除）
func simpleCalc(expr string) (int, error) {
	var a, b int
	var op string

	// 简化解析：只支持 "数字 运算符 数字" 格式
	n, err := fmt.Sscanf(expr, "%d %s %d", &a, &op, &b)
	if err != nil || n != 3 {
		return 0, fmt.Errorf("无效的表达式格式")
	}

	switch op {
	case "+":
		return a + b, nil
	case "-":
		return a - b, nil
	case "*":
		return a * b, nil
	case "/":
		if b == 0 {
			return 0, fmt.Errorf("除数不能为零")
		}
		return a / b, nil
	default:
		return 0, fmt.Errorf("不支持的运算符: %s", op)
	}
}
