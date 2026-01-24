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
	fmt.Println("===== 命令冲突测试 =====\n")

	eng := engine.NewEngine()

	// 场景 1: 命令名冲突 - /echo 作为独立命令
	fmt.Println("场景 1: 注册独立命令 /echo")
	echoMatcher := eng.OnCommand(dto.GroupAtMessageCreate, "/echo")
	echoMatcher.SetDescription("Echo 命令").
		SetCategory("工具")
	echoMatcher.Handle(func(ctx *context.Context) error {
		fmt.Println("  → 触发: Echo 命令")
		return nil
	})

	// 场景 2: 另一个命令 /repeat，别名是 echo（不带 /）
	fmt.Println("场景 2: 注册命令 /repeat，别名包含 'echo'")
	def := &command.Definition{
		Name:        "repeat",
		Aliases:     []string{"echo", "mirror"}, // 注意：别名是 "echo" 不是 "/echo"
		Description: "Repeat 命令",
		Category:    "工具",
	}
	repeatMatcher := eng.RegisterCommandDef(dto.GroupAtMessageCreate, def)
	repeatMatcher.Handle(func(ctx *context.Context) error {
		fmt.Println("  → 触发: Repeat 命令")
		return nil
	})

	fmt.Println()

	// 测试命令发现
	fmt.Println("=== 测试 1: GetAllCommands ===")
	commands := eng.GetAllCommands()
	for _, cmd := range commands {
		fmt.Printf("命令: %s\n", cmd.Command)
		if len(cmd.Aliases) > 0 {
			fmt.Printf("  别名: %v\n", cmd.Aliases)
		}
	}
	fmt.Println()

	// 测试 FindCommand
	fmt.Println("=== 测试 2: FindCommand ===")

	fmt.Println("查找 '/echo':")
	result := eng.FindCommand("/echo")
	if result != nil {
		fmt.Printf("  找到: %s (描述: %s)\n", result.Command, result.Description)
	} else {
		fmt.Println("  未找到")
	}

	fmt.Println("查找 'echo' (不带/):")
	result = eng.FindCommand("echo")
	if result != nil {
		fmt.Printf("  找到: %s (描述: %s)\n", result.Command, result.Description)
	} else {
		fmt.Println("  未找到")
	}

	fmt.Println("查找 '/repeat':")
	result = eng.FindCommand("/repeat")
	if result != nil {
		fmt.Printf("  找到: %s (描述: %s)\n", result.Command, result.Description)
		if len(result.Aliases) > 0 {
			fmt.Printf("  别名: %v\n", result.Aliases)
		}
	} else {
		fmt.Println("  未找到")
	}
	fmt.Println()

	// 测试命令索引
	fmt.Println("=== 测试 3: 命令索引结构 ===")
	fmt.Println("commandIndex 的键应该是:")
	fmt.Println("  - /echo  (来自 echo 命令)")
	fmt.Println("  - /repeat (来自 repeat 命令)")
	fmt.Println()
	fmt.Println("注意: 别名 'echo' 存储在 Definition.Aliases 中")
	fmt.Println("      不会作为单独的 commandIndex 键")
	fmt.Println()

	// 分析
	fmt.Println("=== 分析 ===")
	fmt.Println()
	fmt.Println("1. 命令索引层面（commandIndex）:")
	fmt.Println("   ✅ 无冲突 - /echo 和 /repeat 是不同的键")
	fmt.Println()
	fmt.Println("2. 命令发现层面（FindCommand）:")
	fmt.Println("   ⚠️  可能混淆:")
	fmt.Println("   - FindCommand('/echo') → 找到 echo 命令")
	fmt.Println("   - FindCommand('echo')  → 找到 repeat 命令（通过别名）")
	fmt.Println()
	fmt.Println("3. 实际使用场景:")
	fmt.Println("   - 用户发送 '/echo xxx'")
	fmt.Println("     → extractCommand 提取 '/echo'")
	fmt.Println("     → commandIndex['/echo'] 查找")
	fmt.Println("     → 触发 echo 命令 ✅")
	fmt.Println()
	fmt.Println("   - 用户发送 '/repeat xxx'")
	fmt.Println("     → extractCommand 提取 '/repeat'")
	fmt.Println("     → commandIndex['/repeat'] 查找")
	fmt.Println("     → 触发 repeat 命令 ✅")
	fmt.Println()
	fmt.Println("   - 用户发送 'echo xxx' (不带/)")
	fmt.Println("     → extractCommand 提取 'echo'")
	fmt.Println("     → commandIndex['echo'] 查找")
	fmt.Println("     → 未找到（别名不在索引中）❌")
	fmt.Println()

	// 结论
	fmt.Println("=== 结论 ===")
	fmt.Println()
	fmt.Println("当前实现:")
	fmt.Println("  - commandIndex 只索引 m.GetCommand() 的值（如 '/echo', '/repeat'）")
	fmt.Println("  - 别名存储在 Definition.Aliases 中，不参与命令路由")
	fmt.Println("  - 别名仅用于 Help 系统的命令查找（FindCommand）")
	fmt.Println()
	fmt.Println("因此:")
	fmt.Println("  ✅ '/echo' 和 'echo'（别名）不会冲突")
	fmt.Println("  ✅ 用户输入 '/echo' → 触发 echo 命令")
	fmt.Println("  ✅ 用户输入 '/repeat' → 触发 repeat 命令")
	fmt.Println("  ⚠️  别名 'echo' 当前不参与命令路由")
	fmt.Println()
	fmt.Println("潜在问题:")
	fmt.Println("  ❌ 别名不能作为命令触发器使用")
	fmt.Println("  ❌ 如果用户希望 'echo' 别名也能触发 repeat 命令，当前实现不支持")
}
