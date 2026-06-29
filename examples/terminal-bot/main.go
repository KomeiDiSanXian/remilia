// Package main 提供终端 Bot 的交互式示例。
//
// 该程序演示如何使用 terminal 适配器配合内置插件系统在本地命令行中调试 Bot，
// 无需连接任何外部平台即可测试命令和插件功能。
package main

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/KomeiDiSanXian/remilia/builtin/core/help"
	"github.com/KomeiDiSanXian/remilia/builtin/core/permission"
	"github.com/KomeiDiSanXian/remilia/builtin/dev/debug"
	"github.com/KomeiDiSanXian/remilia/command"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/middleware"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/terminal"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

func main() {
	eng := engine.NewEngine()
	eng.Use(middleware.DevelopmentSet()...)

	// 创建命令注册表，注入引擎（启用统一命令系统）
	reg := command.NewCommandRegistry()
	eng.SetCommandRegistry(reg)

	// 注册内置插件
	pm := plugin.NewManager(eng)
	loadPlugins(pm, eng)

	adapter := terminal.NewAdapter(
		terminal.WithPrompt("User> "),
		terminal.WithBotID("terminal-bot"),
		terminal.WithBotName("终端调试 Bot"),
		// Tab 补全：支持任意前缀符号（/ ! . 等），trie 存储纯命令名
		terminal.WithCompletionFunc(func(prefix string) []string {
			nameStart := strings.IndexFunc(prefix, func(r rune) bool {
				return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
			})
			if nameStart < 0 {
				return nil
			}
			prefixChar := prefix[:nameStart]
			cmdPrefix := prefix[nameStart:]
			metas := reg.Complete(cmdPrefix)
			names := make([]string, len(metas))
			for i, m := range metas {
				names[i] = prefixChar + m.Name
			}
			return names
		}),
	)

	sender := adapter.Sender()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		_ = adapter.Start(ctx, func(event platform.Event) {
			eng.ProcessPlatformEventEx(event, sender, adapter.BotID(), adapter.Capabilities())
		})
		close(done)
	}()

	<-done
	fmt.Println("程序已退出。")
}

func loadPlugins(pm *plugin.Manager, eng *engine.Engine) {
	// 1. 权限插件（debug 的依赖）
	if err := pm.Register(permission.New()); err != nil {
		fmt.Printf("注册 permission 插件失败: %v\n", err)
		return
	}

	// 2. 调试插件（依赖 permission）
	if err := pm.Register(debug.New()); err != nil {
		fmt.Printf("注册 debug 插件失败: %v\n", err)
		return
	}

	// 3. 帮助插件（无依赖）
	helpPlugin := help.New()
	if err := pm.Register(helpPlugin); err != nil {
		fmt.Printf("注册 help 插件失败: %v\n", err)
		return
	}

	// 4. 注册一些示例命令
	registerDemoCommands(eng)
}

func registerDemoCommands(eng *engine.Engine) {
	eng.OnCommand("", "/ping").
		SetDescription("测试 Bot 是否在线").
		SetUsage("/ping").
		SetCategory("示例").
		Handle(func(ctx *eventctx.Context) error {
			ctx.Reply(platform.TextMessage("Pong! 🏓"))
		return nil
		})

	eng.OnCommand("", "/echo").
		SetDescription("回显输入的消息").
		SetUsage("/echo <消息>").
		SetCategory("示例").
		Handle(func(ctx *eventctx.Context) error {
			content := ctx.GetMessageContent()
			ctx.Reply(platform.TextMessage("回声: " + content))
		return nil
		})

	eng.OnCommand("", "/info").
		SetDescription("查看当前事件信息").
		SetUsage("/info").
		SetCategory("调试").
		Handle(func(ctx *eventctx.Context) error {
			sender := ctx.GetSenderInfo()
			chat := ctx.GetChatInfo()
			msg := fmt.Sprintf(
				"事件信息:\n  平台: %s\n  类型: %s\n  会话: %s\n  发送者: %s (%s)\n  内容: %s",
				ctx.GetEventPlatform(),
				ctx.GetEventType(),
				chat.ID,
				sender.ID,
				sender.DisplayName,
				ctx.GetMessageContent(),
			)
			ctx.Reply(platform.TextMessage(msg))
		return nil
		})

	eng.OnCommand("", "/caps").
		SetDescription("查看平台能力声明").
		SetUsage("/caps").
		SetCategory("调试").
		Handle(func(ctx *eventctx.Context) error {
			caps := ctx.GetPlatformCapabilities()
			msg := fmt.Sprintf(
				"平台能力:\n  Markdown: %v\n  消息编辑: %v\n  消息删除: %v\n  表情回应: %v\n  输入指示: %v",
				caps.Markdown, caps.MessageEdit, caps.MessageDelete, caps.Reactions, caps.TypingIndicator,
			)
			ctx.Reply(platform.TextMessage(msg))
		return nil
		})
}
