//go:build example
// +build example

package main

import (
	"fmt"

	"github.com/KomeiDiSanXian/remilia/command"
	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

func main() {
	fmt.Println("===== Help Plugin 命令发现演示（重构版）=====\n")

	// 创建 Engine
	eng := engine.NewEngine()

	// 注册一些示例命令
	registerEchoCommand(eng)
	registerSearchCommand(eng)
	registerHiddenCommand(eng)

	// 演示命令发现
	demonstrateHelpPlugin(eng)
}

// 注册 echo 命令
func registerEchoCommand(eng *engine.Engine) {
	m := eng.OnCommand(dto.GroupAtMessageCreate, "/echo")
	m.SetDescription("回显用户发送的消息").
		SetUsage("/echo <消息内容>").
		SetCategory("实用工具").
		SetAliases("/repeat", "/mirror").
		SetExamples(
			"/echo Hello World",
			"/echo 你好，世界",
		)

	m.Handle(func(ctx *context.Context) error {
		fmt.Println("Echo command executed")
		return nil
	})
}

// 注册 search 命令
func registerSearchCommand(eng *engine.Engine) {
	def := &command.Definition{
		Name:        "search",
		Aliases:     []string{"find", "query"},
		Description: "搜索网络内容",
		Usage:       "/search <关键词> [flags]",
		Category:    "实用工具",
		Examples: []string{
			"/search Go语言",
			"/search Python --engine bing",
		},
		Permissions: []string{"use_search"},
		Arguments: []*command.Argument{
			{
				Name:        "keyword",
				Description: "搜索关键词",
				Required:    true,
				Type:        command.ArgTypeString,
			},
		},
		Flags: []*command.Flag{
			{
				Name:        "engine",
				ShortName:   "e",
				Description: "搜索引擎",
				Default:     "google",
			},
		},
	}

	m := eng.RegisterCommandDef(dto.GroupAtMessageCreate, def)
	m.Handle(func(ctx *context.Context) error {
		fmt.Println("Search command executed")
		return nil
	})
}

// 注册隐藏命令
func registerHiddenCommand(eng *engine.Engine) {
	m := eng.OnCommand(dto.GroupAtMessageCreate, "/internal")
	m.SetDescription("内部命令").
		SetHidden(true)

	m.Handle(func(ctx *context.Context) error {
		fmt.Println("Internal command executed")
		return nil
	})
}

// 演示 Help Plugin 如何发现命令
func demonstrateHelpPlugin(eng *engine.Engine) {
	fmt.Println("===== 命令发现演示 =====\n")

	// 1. 获取所有命令（自动过滤隐藏命令）
	fmt.Println("1. 所有可见命令:")
	commands := eng.GetAllCommands()
	for _, cmd := range commands {
		fmt.Printf("  - %s: %s\n", cmd.Command, cmd.Description)
		if len(cmd.Aliases) > 0 {
			fmt.Printf("    别名: %v\n", cmd.Aliases)
		}
	}
	fmt.Println()

	// 2. 按分类分组
	fmt.Println("2. 按分类分组:")
	byCategory := eng.GetCommandsByCategory()
	for category, cmds := range byCategory {
		fmt.Printf("  [%s]\n", category)
		for _, cmd := range cmds {
			fmt.Printf("    - %s: %s\n", cmd.Command, cmd.Description)
		}
	}
	fmt.Println()

	// 3. 查找命令详情
	fmt.Println("3. 查找命令 '/search' 详情:")
	searchCmd := eng.FindCommand("/search")
	if searchCmd != nil {
		fmt.Printf("  命令: %s\n", searchCmd.Command)
		fmt.Printf("  描述: %s\n", searchCmd.Description)
		fmt.Printf("  用法: %s\n", searchCmd.Usage)
		fmt.Printf("  分类: %s\n", searchCmd.Category)

		if searchCmd.Definition != nil && len(searchCmd.Definition.Arguments) > 0 {
			fmt.Println("  参数:")
			for _, arg := range searchCmd.Definition.Arguments {
				fmt.Printf("    - %s: %s\n", arg.Name, arg.Description)
			}
		}
	}
	fmt.Println()

	// 4. 别名查找
	fmt.Println("4. 通过别名 '/repeat' 查找:")
	aliasCmd := eng.FindCommand("/repeat")
	if aliasCmd != nil {
		fmt.Printf("  找到命令: %s (通过别名)\n", aliasCmd.Command)
	}
	fmt.Println()

	// 5. 重构总结
	fmt.Println("5. 重构总结:")
	fmt.Println("  ✅ 移除 Matcher.command 字段")
	fmt.Println("  ✅ GetCommand() 从 Definition.Name 获取")
	fmt.Println("  ✅ OnCommand 自动创建 Definition")
	fmt.Println("  ✅ 命令索引自动更新（commandIndex）")
	fmt.Println("  ✅ Help Plugin 零感知，完全透明")
}
