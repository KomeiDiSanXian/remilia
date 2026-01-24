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
	fmt.Println("===== Command 包集成示例（重构版）=====\n")

	// 创建 Engine
	eng := engine.NewEngine()

	// 示例 1: OnCommand 自动创建 Definition
	registerWithOnCommand(eng)

	// 示例 2: 使用 RegisterCommandDef 完整定义
	registerWithDefinition(eng)

	// 演示命令发现
	demonstrateDiscovery(eng)
}

// 示例 1: OnCommand 自动创建 Definition
func registerWithOnCommand(eng *engine.Engine) {
	// OnCommand 现在会自动创建 command.Definition
	m := eng.OnCommand(dto.GroupAtMessageCreate, "/ping")

	// 可以继续使用便捷方法设置元数据
	m.SetDescription("测试连接").
		SetUsage("/ping").
		SetCategory("系统")

	m.Handle(func(ctx *context.Context) error {
		fmt.Println("Ping command executed")
		return nil
	})

	fmt.Println("✅ 注册命令: /ping (OnCommand 自动创建 Definition)")
}

// 示例 2: 使用完整 Definition
func registerWithDefinition(eng *engine.Engine) {
	def := &command.Definition{
		Name:        "search",
		Aliases:     []string{"find", "query"},
		Description: "搜索网络内容",
		Usage:       "/search <关键词> [--engine google|bing] [--count 5]",
		Category:    "实用工具",
		Examples: []string{
			"/search Go语言",
			"/search Python --engine bing",
			"/search 机器学习 --count 10",
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
				Type:        command.ArgTypeString,
				Default:     "google",
			},
			{
				Name:        "count",
				ShortName:   "n",
				Description: "结果数量",
				Type:        command.ArgTypeInt,
				Default:     5,
			},
		},
	}

	m := eng.RegisterCommandDef(dto.GroupAtMessageCreate, def)
	m.Handle(func(ctx *context.Context) error {
		parsed := ctx.GetParsedCommand()
		if parsed != nil {
			keyword := parsed.GetString("keyword")
			engine := parsed.GetString("engine")
			count := parsed.GetInt("count")
			fmt.Printf("Search: keyword=%s, engine=%s, count=%d\n", keyword, engine, count)
		}
		return nil
	})

	fmt.Println("✅ 注册命令: /search (完整 Definition)")
}

// 演示命令发现
func demonstrateDiscovery(eng *engine.Engine) {
	fmt.Println("\n===== 命令发现演示 =====\n")

	// 1. 获取所有命令
	fmt.Println("1. 所有命令:")
	commands := eng.GetAllCommands()
	for _, cmd := range commands {
		fmt.Printf("  - %s: %s\n", cmd.Command, cmd.Description)
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

	// 3. 查找特定命令
	fmt.Println("3. 查找命令 '/search':")
	searchCmd := eng.FindCommand("/search")
	if searchCmd != nil {
		fmt.Printf("  命令: %s\n", searchCmd.Command)
		fmt.Printf("  描述: %s\n", searchCmd.Description)
		fmt.Printf("  用法: %s\n", searchCmd.Usage)
		fmt.Printf("  分类: %s\n", searchCmd.Category)

		if searchCmd.Definition != nil {
			if len(searchCmd.Definition.Arguments) > 0 {
				fmt.Println("  参数:")
				for _, arg := range searchCmd.Definition.Arguments {
					required := ""
					if arg.Required {
						required = " (必需)"
					}
					fmt.Printf("    - %s (%v)%s: %s\n",
						arg.Name, arg.Type, required, arg.Description)
				}
			}

			if len(searchCmd.Definition.Flags) > 0 {
				fmt.Println("  选项:")
				for _, flag := range searchCmd.Definition.Flags {
					short := ""
					if flag.ShortName != "" {
						short = fmt.Sprintf(", -%s", flag.ShortName)
					}
					def := ""
					if flag.Default != nil {
						def = fmt.Sprintf(" (默认: %v)", flag.Default)
					}
					fmt.Printf("    - --%s%s%s: %s\n",
						flag.Name, short, def, flag.Description)
				}
			}
		}
	}
	fmt.Println()

	// 4. 重构总结
	fmt.Println("4. 重构亮点:")
	fmt.Println("  ✅ 移除 Matcher.command 字段")
	fmt.Println("  ✅ 移除 MatcherMetadata 等冗余类型")
	fmt.Println("  ✅ OnCommand 自动创建 Definition")
	fmt.Println("  ✅ GetCommand() 从 Definition.Name 获取")
	fmt.Println("  ✅ 零转换开销，直接使用 command.Definition")
	fmt.Println("  ✅ 单一数据源，完美统一")
}
