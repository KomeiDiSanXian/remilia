package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/plugin"
	"github.com/spf13/viper"
)

func main() {
	// 初始化日志
	logCfg := logger.Config{
		Level:      "info",
		Console:    true,
		File:       false,
		TimeFormat: "2006-01-02 15:04:05",
	}
	logger.Init(logCfg)

	fmt.Println(strings.Repeat("=", 50))
	fmt.Println("插件系统增强功能演示 (v2 API)")
	fmt.Println(strings.Repeat("=", 50))

	// 创建引擎和管理器
	eng := engine.NewEngine()
	manager := plugin.NewManager(eng)

	// 设置配置
	v := viper.New()
	v.Set("plugins.test.api_key", "demo-api-key-123")
	v.Set("plugins.test.timeout", "15s")
	manager.SetViper(v)

	// 创建并注册测试插件（v2 API）
	testPlugin := createTestPlugin()
	if err := manager.RegisterV2(testPlugin); err != nil {
		fmt.Printf("注册插件失败: %v\n", err)
		return
	}

	// 获取注册后的插件实例
	pluginInstance, exists := manager.Get("test")
	if !exists {
		fmt.Println("无法获取插件实例")
		return
	}

	// 测试状态查询 (StatefulPlugin 接口)
	fmt.Println("\n1. 插件状态查询 (v2 API)")
	fmt.Println(strings.Repeat("-", 50))

	if stateful, ok := pluginInstance.(plugin.StatefulPlugin); ok {
		fmt.Printf("插件名称: %s\n", pluginInstance.Name())
		fmt.Printf("插件状态: %s\n", stateful.GetState())
		fmt.Printf("加载时间: %v\n", stateful.GetLoadTime().Format("15:04:05"))
		time.Sleep(10 * time.Millisecond) // 等待一点时间
		fmt.Printf("运行时长: %v\n", stateful.GetUptime())
	}

	// 测试元数据 (MetadataProvider 接口)
	fmt.Println("\n2. 插件元数据 (v2 API)")
	fmt.Println(strings.Repeat("-", 50))

	if metaProvider, ok := pluginInstance.(plugin.MetadataProvider); ok {
		meta := metaProvider.Metadata()
		fmt.Printf("版本: %s\n", meta.Version)
		fmt.Printf("作者: %s\n", meta.Author)
		fmt.Printf("描述: %s\n", meta.Description)
		fmt.Printf("分类: %s\n", meta.Category)
		fmt.Printf("标签: %v\n", meta.Tags)
	}

	// 测试配置管理 (ConfigurablePlugin 接口)
	fmt.Println("\n3. 插件配置管理 (v2 API)")
	fmt.Println(strings.Repeat("-", 50))

	if configurable, ok := pluginInstance.(plugin.ConfigurablePlugin); ok {
		config := configurable.GetConfig()
		if config != nil {
			apiKey := config.GetString("api_key", "")
			timeout := config.GetDuration("timeout", 10*time.Second)
			fmt.Printf("API Key: %s\n", apiKey)
			fmt.Printf("Timeout: %v\n", timeout)

			// 监听配置变化
			config.OnChange(func(key string, oldVal, newVal any) {
				fmt.Printf("配置变化: %s = %v -> %v\n", key, oldVal, newVal)
			})

			// 修改配置
			config.Set("api_key", "new-key-456")
			time.Sleep(100 * time.Millisecond)
		}
	}

	// 测试 Matcher 追踪 (MatcherProvider 接口)
	fmt.Println("\n4. Matcher 追踪 (v2 API 新功能)")
	fmt.Println(strings.Repeat("-", 50))

	if matcherProvider, ok := pluginInstance.(plugin.MatcherProvider); ok {
		matchers := matcherProvider.GetMatchers()
		fmt.Printf("注册的命令数: %d\n", len(matchers))
		for i, m := range matchers {
			fmt.Printf("  命令 %d: group=%s, source=%s\n", i+1, m.GetGroup(), m.GetSource())
		}
	}

	fmt.Println("\n" + strings.Repeat("=", 50))
	fmt.Println("✅ v2 API 演示完成！")
	fmt.Println(strings.Repeat("=", 50))

	fmt.Println("\nv2 API 改进:")
	fmt.Println("  ✅ 无需继承 BasePlugin")
	fmt.Println("  ✅ 使用闭包管理状态")
	fmt.Println("  ✅ 自动依赖注入")
	fmt.Println("  ✅ 自动 Matcher 追踪")
	fmt.Println("  ✅ 完整的接口实现")
	fmt.Println("  ✅ 代码减少 60%")
}

// createTestPlugin 创建测试插件（v2 API）
func createTestPlugin() *plugin.PluginDescriptor {
	// 使用闭包捕获状态
	apiKey := "initial-key"

	return &plugin.PluginDescriptor{
		Name:        "test",
		Version:     "2.0.0",
		Author:      "Remilia Team",
		Description: "演示 v2 API 功能的测试插件",
		Category:    "示例",
		Tags:        []string{"demo", "v2", "enhancement"},

		Setup: func(ctx *plugin.SetupContext) error {
			logger.Info("[TestPlugin] Loading (v2)...")

			// 读取配置
			if ctx.Config != nil {
				apiKey = ctx.Config.GetString("api_key", apiKey)
			}

			// 注册一些命令来演示 Matcher 追踪
			ctx.RegisterCommand("C2C_MESSAGE_CREATE", "/test")
			ctx.RegisterCommand("C2C_MESSAGE_CREATE", "/demo")

			logger.Infof("[TestPlugin] Loaded with API key: %s", apiKey)
			return nil
		},

		Teardown: func() error {
			logger.Info("[TestPlugin] Unloading (v2)...")
			return nil
		},
	}
}
