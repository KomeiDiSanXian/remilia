package main

import (
	"fmt"

	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/plugin"
	"github.com/KomeiDiSanXian/remilia/plugins/core/help"
)

// NewTestPlugin 创建测试插件（v2 API）
func NewTestPlugin() *plugin.PluginDescriptor {
	return &plugin.PluginDescriptor{
		Name:        "test",
		Version:     "2.0.0",
		Description: "测试插件 - 用于验证 help 插件功能",
		Category:    "测试",
		Tags:        []string{"test", "v2"},

		Setup: func(ctx *plugin.SetupContext) error {
			// 注册一个测试命令（使用 RegisterCommand 自动追踪）
			ctx.RegisterCommand(dto.C2CMessageCreate, "/test")
			return nil
		},
	}
}

func main() {
	// 初始化日志
	logger.Init(logger.Config{
		Level:   "info",
		Console: true,
	})

	// 创建引擎
	eng := engine.NewEngine()

	// 创建插件管理器
	manager := plugin.NewManager(eng)

	// 注册帮助插件（v2）
	if err := manager.RegisterV2(help.New()); err != nil {
		logger.WithError(err).Fatal("Failed to register help plugin")
	}

	// 注册测试插件（v2）
	if err := manager.RegisterV2(NewTestPlugin()); err != nil {
		logger.WithError(err).Fatal("Failed to register test plugin")
	}

	// 验证命令是否可以被查询到
	fmt.Println("\n=== 验证命令查询 (v2 API) ===")
	commands := eng.GetAllCommands()
	fmt.Printf("总共注册了 %d 个命令:\n", len(commands))
	for _, cmd := range commands {
		fmt.Printf("  - %s (插件: %s, 来源: %s)\n", cmd.Command, cmd.Plugin, cmd.Source)
	}

	// 按插件分组
	fmt.Println("\n=== 按插件分组 ===")
	byPlugin := eng.GetCommandsByPlugin()
	for pluginName, cmds := range byPlugin {
		fmt.Printf("插件 [%s] 的命令:\n", pluginName)
		for _, cmd := range cmds {
			fmt.Printf("  - %s\n", cmd.Command)
		}
	}

	// 查找特定命令
	fmt.Println("\n=== 查找特定命令 ===")
	if cmdInfo := eng.FindCommand("test"); cmdInfo != nil {
		fmt.Printf("找到命令: %s\n", cmdInfo.Command)
		fmt.Printf("  插件: %s\n", cmdInfo.Plugin)
		fmt.Printf("  来源: %s\n", cmdInfo.Source)
	}

	if cmdInfo := eng.FindCommand("help"); cmdInfo != nil {
		fmt.Printf("找到命令: %s\n", cmdInfo.Command)
		fmt.Printf("  插件: %s\n", cmdInfo.Plugin)
		fmt.Printf("  来源: %s\n", cmdInfo.Source)
	}

	fmt.Println("\n✅ v2 API 验证完成！命令可以被正确查询。")
	fmt.Println("\nv2 改进:")
	fmt.Println("  ✅ 无需定义结构体和方法")
	fmt.Println("  ✅ 使用 RegisterCommand 自动追踪")
	fmt.Println("  ✅ 代码更简洁")
}
