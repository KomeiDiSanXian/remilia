package main

import (
	"fmt"
	"log"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/config"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
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
	if err := manager.RegisterV2(NewGreeterPlugin()); err != nil {
		logger.WithError(err).Error("[V2Example] Failed to register greeter plugin")
	}

	// 2. Counter 插件（v2 风格）
	if err := manager.RegisterV2(NewCounterPlugin()); err != nil {
		logger.WithError(err).Error("[V2Example] Failed to register counter plugin")
	}

	// 3. Calculator 插件（v2 风格，演示更复杂的功能）
	if err := manager.RegisterV2(NewCalculatorPlugin()); err != nil {
		logger.WithError(err).Error("[V2Example] Failed to register calculator plugin")
	}

	logger.Info("[V2Example] All v2 plugins registered")
}

// NewGreeterPlugin 创建问候插件（v2 风格）
//
// 演示：
//   - 基本命令注册
//   - 使用闭包管理状态
//   - 无需结构体和继承
func NewGreeterPlugin() *plugin.PluginDescriptor {
	// 使用闭包捕获状态，无需结构体
	greeting := "你好"

	return &plugin.PluginDescriptor{
		Name:        "greeter",
		Version:     "2.0.0",
		Author:      "Remilia Team",
		Description: "简单的问候插件（v2 API 演示）",
		Category:    "示例",
		Tags:        []string{"问候", "演示"},

		Setup: func(ctx *plugin.SetupContext) error {
			logger.Info("[Greeter] Setting up plugin (v2)...")

			// 注册 /greet 命令
			ctx.Engine.OnCommand(dto.C2CMessageCreate, "/greet").
				Handle(func(c *eventctx.Context) error {
					var event dto.C2CMessageCreateEvent
					if err := c.DecodeEvent(&event); err != nil {
						return err
					}

					response := greeting + ", " + event.Author.UserOpenID + "!"
					_, err := c.ReplyPrivate(&dto.Message{
						Type:    dto.TextMessage,
						Content: response,
					})
					return err
				})

			// 注册 /setgreeting 命令
			ctx.Engine.OnCommand(dto.C2CMessageCreate, "/setgreeting").
				Handle(func(c *eventctx.Context) error {
					content := c.GetMessageContent()
					if len(content) <= 13 { // "/setgreeting "
						_, err := c.ReplyPrivate(&dto.Message{
							Type:    dto.TextMessage,
							Content: "用法: /setgreeting <问候语>",
						})
						return err
					}

					// 更新闭包中的变量
					greeting = content[13:]
					_, err := c.ReplyPrivate(&dto.Message{
						Type:    dto.TextMessage,
						Content: "问候语已更新为: " + greeting,
					})
					return err
				})

			logger.Info("[Greeter] Plugin setup complete")
			return nil
		},

		Teardown: func() error {
			logger.Info("[Greeter] Tearing down plugin...")
			return nil
		},
	}
}

// NewCounterPlugin 创建计数器插件（v2 风格）
//
// 演示：
//   - 状态管理
//   - 多命令注册
//   - 配置使用
func NewCounterPlugin() *plugin.PluginDescriptor {
	// 状态变量
	count := 0

	return &plugin.PluginDescriptor{
		Name:        "counter",
		Version:     "2.0.0",
		Author:      "Remilia Team",
		Description: "简单的计数器插件（v2 API 演示）",
		Category:    "示例",
		Tags:        []string{"计数器", "演示"},

		Setup: func(ctx *plugin.SetupContext) error {
			logger.Info("[Counter] Setting up plugin (v2)...")

			// 从配置读取初始值（如果有）
			if ctx.Config != nil {
				count = ctx.Config.GetInt("initial_value", 0)
				logger.Infof("[Counter] Initial value from config: %d", count)
			}

			// /count - 增加计数
			ctx.Engine.OnCommand(dto.C2CMessageCreate, "/count").
				Handle(func(c *eventctx.Context) error {
					count++
					_, err := c.ReplyPrivate(&dto.Message{
						Type:    dto.TextMessage,
						Content: fmt.Sprintf("计数: %d", count),
					})
					return err
				})

			// /reset - 重置计数
			ctx.Engine.OnCommand(dto.C2CMessageCreate, "/reset").
				Handle(func(c *eventctx.Context) error {
					count = 0
					_, err := c.ReplyPrivate(&dto.Message{
						Type:    dto.TextMessage,
						Content: "计数已重置",
					})
					return err
				})

			// /get - 获取当前计数
			ctx.Engine.OnCommand(dto.C2CMessageCreate, "/get").
				Handle(func(c *eventctx.Context) error {
					_, err := c.ReplyPrivate(&dto.Message{
						Type:    dto.TextMessage,
						Content: fmt.Sprintf("当前计数: %d", count),
					})
					return err
				})

			logger.Info("[Counter] Plugin setup complete")
			return nil
		},

		Teardown: func() error {
			logger.Infof("[Counter] Tearing down plugin... (final count: %d)", count)
			// 可以在这里保存状态到数据库
			return nil
		},

		Reload: func(ctx *plugin.SetupContext) error {
			logger.Info("[Counter] Reloading plugin...")
			// 热重载：保持计数不变，只重新注册命令
			// 注意：这是一个简化示例，实际应该更仔细地处理
			return nil
		},
	}
}

// NewCalculatorPlugin 创建计算器插件（v2 风格）
//
// 演示：
//   - 依赖其他插件（如果有的话）
//   - 更复杂的命令处理
//   - 错误处理
func NewCalculatorPlugin() *plugin.PluginDescriptor {
	return &plugin.PluginDescriptor{
		Name:        "calculator",
		Version:     "2.0.0",
		Author:      "Remilia Team",
		Description: "简单的计算器插件（v2 API 演示）",
		Category:    "工具",
		Tags:        []string{"计算器", "工具"},
		Deps:        []string{}, // 如果依赖 counter，可以写 []string{"counter"}

		Setup: func(ctx *plugin.SetupContext) error {
			logger.Info("[Calculator] Setting up plugin (v2)...")

			// 如果有依赖，可以这样获取：
			// counter := ctx.MustGet("counter")

			// /calc 命令
			ctx.Engine.OnCommand(dto.C2CMessageCreate, "/calc").
				Handle(func(c *eventctx.Context) error {
					content := c.GetMessageContent()
					if len(content) <= 6 { // "/calc "
						_, err := c.ReplyPrivate(&dto.Message{
							Type:    dto.TextMessage,
							Content: "用法: /calc <表达式>\n示例: /calc 1 + 2",
						})
						return err
					}

					expr := content[6:]
					result, err := simpleCalc(expr)
					if err != nil {
						_, err2 := c.ReplyPrivate(&dto.Message{
							Type:    dto.TextMessage,
							Content: "计算错误: " + err.Error(),
						})
						return err2
					}

					_, err = c.ReplyPrivate(&dto.Message{
						Type:    dto.TextMessage,
						Content: fmt.Sprintf("%s = %d", expr, result),
					})
					return err
				})

			logger.Info("[Calculator] Plugin setup complete")
			return nil
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
