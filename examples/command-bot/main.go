//go:build example
// +build example

package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/command"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/middleware"
	"github.com/sirupsen/logrus"
)

func main() {
	logrus.SetLevel(logrus.DebugLevel)

	// 创建命令注册表
	registry := command.NewCommandRegistry()

	// 注册命令
	registerCommands(registry)

	// 创建 Engine
	eng := engine.NewEngine()

	// 添加中间件
	eng.Use(
		middleware.Logging(),
		middleware.Recover(),
	)

	// 注册命令处理器
	eng.OnMessage(func(ctx *eventctx.Context) error {
		content := ctx.GetPlainText()

		// 提取命令
		cmd := command.ExtractCommandFast(content)
		if cmd == "" {
			return nil
		}

		// 查找命令
		meta, found := registry.Lookup(cmd)
		if !found {
			// 尝试命令补全
			suggestions := registry.Complete(cmd)
			if len(suggestions) > 0 {
				var names []string
				for _, s := range suggestions {
					names = append(names, s.Name)
				}
				return ctx.Reply(fmt.Sprintf("未知命令 %s\n你是否想要: %s",
					cmd, strings.Join(names, ", ")))
			}
			return ctx.Reply("未知命令: " + cmd)
		}

		// 解析命令参数
		args, err := command.ParseCommandLine(content)
		if err != nil {
			return ctx.Reply("命令解析失败: " + err.Error())
		}

		// 执行命令
		return meta.Definition.Handler(ctx)
	})

	// 创建并启动 Bot
	secret := getEnv("BOT_SECRET", "your-webhook-secret")
	port := getEnv("BOT_PORT", "8080")

	adapter := remilia.NewWebhookAdapter(":"+port, secret)
	bot := remilia.NewBot(adapter, eng)

	logrus.Info("Starting command bot...")
	if err := bot.Start(); err != nil {
		logrus.Fatal(err)
	}

	bot.WaitForShutdown()
}

func registerCommands(registry *command.CommandRegistry) {
	// 1. 天气命令 - 带参数和标志
	weatherDef := &command.Definition{
		Name:        "/weather",
		Aliases:     []string{"/w", "/天气"},
		Description: "查询天气",
		Usage:       "/weather <城市> [--unit celsius|fahrenheit] [--days 3]",
		Handler: func(ctx any) error {
			eventCtx := ctx.(*eventctx.Context)
			text := eventCtx.GetPlainText()

			args, err := command.ParseCommandLine(text)
			if err != nil {
				return eventCtx.Reply("参数解析失败")
			}

			city := args.Get(0)
			if city == "" {
				return eventCtx.Reply("用法: /weather <城市>")
			}

			unit := args.GetFlagOrDefault("unit", "celsius")
			days := args.GetFlagIntOrDefault("days", 1)

			response := fmt.Sprintf("查询 %s 的天气\n单位: %s\n天数: %d",
				city, unit, days)
			return eventCtx.Reply(response)
		},
	}
	registry.RegisterWithOptions(weatherDef, command.RegisterOptions{
		Category: "utility",
		Priority: 10,
	})

	// 2. 计算器命令 - 复杂参数
	calcDef := &command.Definition{
		Name:        "/calc",
		Aliases:     []string{"/计算"},
		Description: "简单计算器",
		Usage:       "/calc <表达式>",
		Handler: func(ctx any) error {
			eventCtx := ctx.(*eventctx.Context)
			text := eventCtx.GetPlainText()

			args, err := command.ParseCommandLine(text)
			if err != nil {
				return eventCtx.Reply("参数解析失败")
			}

			expression := strings.Join(args.Positional, " ")
			if expression == "" {
				return eventCtx.Reply("用法: /calc <表达式>\n例如: /calc 1 + 2 * 3")
			}

			// 这里应该实现真实的计算逻辑
			result := fmt.Sprintf("计算 %s 的结果\n(实际计算逻辑待实现)", expression)
			return eventCtx.Reply(result)
		},
	}
	registry.Register(calcDef)

	// 3. 搜索命令 - 多个参数
	searchDef := &command.Definition{
		Name:        "/search",
		Aliases:     []string{"/s", "/搜索"},
		Description: "搜索内容",
		Usage:       "/search <关键词> [--source google|bing] [--limit 10]",
		Handler: func(ctx any) error {
			eventCtx := ctx.(*eventctx.Context)
			text := eventCtx.GetPlainText()

			args, err := command.ParseCommandLine(text)
			if err != nil {
				return eventCtx.Reply("参数解析失败")
			}

			keyword := strings.Join(args.Positional, " ")
			if keyword == "" {
				return eventCtx.Reply("用法: /search <关键词>")
			}

			source := args.GetFlagOrDefault("source", "google")
			limit := args.GetFlagIntOrDefault("limit", 10)

			response := fmt.Sprintf("搜索: %s\n来源: %s\n数量: %d",
				keyword, source, limit)
			return eventCtx.Reply(response)
		},
	}
	registry.Register(searchDef)

	// 4. 帮助命令 - 列出所有命令
	helpDef := &command.Definition{
		Name:        "/help",
		Aliases:     []string{"/h", "/帮助"},
		Description: "显示帮助信息",
		Usage:       "/help [命令名]",
		Handler: func(ctx any) error {
			eventCtx := ctx.(*eventctx.Context)
			text := eventCtx.GetPlainText()

			args, err := command.ParseCommandLine(text)
			if err != nil {
				return eventCtx.Reply("参数解析失败")
			}

			cmdName := args.Get(0)
			if cmdName != "" {
				// 显示特定命令的帮助
				meta, found := registry.Lookup(cmdName)
				if !found {
					return eventCtx.Reply("命令不存在: " + cmdName)
				}

				help := fmt.Sprintf("命令: %s\n描述: %s\n用法: %s",
					meta.Name, meta.Description, meta.Usage)

				if len(meta.Aliases) > 0 {
					help += "\n别名: " + strings.Join(meta.Aliases, ", ")
				}

				return eventCtx.Reply(help)
			}

			// 列出所有命令
			var help strings.Builder
			help.WriteString("可用命令:\n\n")

			for _, meta := range registry.List() {
				help.WriteString(fmt.Sprintf("%s - %s\n",
					meta.Name, meta.Description))
			}

			help.WriteString("\n发送 /help <命令名> 查看详细用法")
			return eventCtx.Reply(help.String())
		},
	}
	registry.Register(helpDef)

	// 5. 用户信息命令
	userDef := &command.Definition{
		Name:        "/user",
		Aliases:     []string{"/u"},
		Description: "查看用户信息",
		Usage:       "/user [用户ID]",
		Handler: func(ctx any) error {
			eventCtx := ctx.(*eventctx.Context)
			text := eventCtx.GetPlainText()

			args, err := command.ParseCommandLine(text)
			if err != nil {
				return eventCtx.Reply("参数解析失败")
			}

			userID := args.Get(0)
			if userID == "" {
				// 显示当前用户信息
				author := eventCtx.GetAuthor()
				return eventCtx.Reply(fmt.Sprintf("你的用户ID: %s", author))
			}

			// 查询指定用户
			return eventCtx.Reply(fmt.Sprintf("查询用户 %s 的信息\n(实际查询逻辑待实现)", userID))
		},
	}
	registry.Register(userDef)

	// 显示注册统计
	stats := registry.GetStats()
	logrus.WithFields(logrus.Fields{
		"commands": stats.CommandCount,
		"aliases":  stats.AliasCount,
	}).Info("Commands registered")
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
