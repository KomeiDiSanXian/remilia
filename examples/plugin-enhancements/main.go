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
	fmt.Println("插件系统增强功能演示")
	fmt.Println(strings.Repeat("=", 50))

	// 创建引擎和管理器
	eng := engine.NewEngine()
	manager := plugin.NewManager(eng)

	// 设置配置
	v := viper.New()
	v.Set("plugins.test.api_key", "demo-api-key-123")
	v.Set("plugins.test.timeout", "15s")
	manager.SetViper(v)

	// 创建测试插件
	testPlugin := plugin.NewBasePlugin("test")

	// 测试配置管理
	fmt.Println("\n1. 插件配置管理")
	fmt.Println(strings.Repeat("-", 50))

	config := plugin.NewPluginConfig("test", v)
	testPlugin.SetConfig(config)

	apiKey := config.GetString("api_key", "")
	timeout := config.GetDuration("timeout", 10*time.Second)
	fmt.Printf("API Key: %s\n", apiKey)
	fmt.Printf("Timeout: %v\n", timeout)

	// 监听配置变化
	config.OnChange(func(key string, oldVal, newVal interface{}) {
		fmt.Printf("配置变化: %s = %v -> %v\n", key, oldVal, newVal)
	})

	// 修改配置
	config.Set("api_key", "new-key-456")
	time.Sleep(100 * time.Millisecond)

	// 测试插件间通信
	fmt.Println("\n2. 插件间通信 (事件总线)")
	fmt.Println(strings.Repeat("-", 50))

	// 订阅事件
	testPlugin.SubscribeEvent("test.event", func(data interface{}) {
		fmt.Printf("收到事件: %v\n", data)
	})

	// 发布事件
	testPlugin.PublishEvent("test.event", "Hello from plugin!")
	time.Sleep(100 * time.Millisecond)

	// 测试状态查询
	fmt.Println("\n3. 插件状态查询")
	fmt.Println(strings.Repeat("-", 50))

	testPlugin.SetState(plugin.Loaded)
	testPlugin.SetLoadTime(time.Now())

	fmt.Printf("插件名称: %s\n", testPlugin.Name())
	fmt.Printf("插件状态: %s\n", testPlugin.GetState())
	fmt.Printf("加载时间: %v\n", testPlugin.GetLoadTime().Format("15:04:05"))
	fmt.Printf("运行时长: %v\n", testPlugin.GetUptime())

	// 测试事件总线统计
	fmt.Println("\n4. 事件总线统计")
	fmt.Println(strings.Repeat("-", 50))

	eventBus := testPlugin.GetEventBus()
	stats := eventBus.GetStats()
	fmt.Printf("主题数量: %d\n", stats.TopicCount)
	fmt.Printf("订阅数量: %d\n", stats.SubscriptionCount)
	fmt.Printf("发布事件总数: %d\n", stats.PublishCount)

	fmt.Println("\n" + strings.Repeat("=", 50))
	fmt.Println("✅ 演示完成！")
	fmt.Println(strings.Repeat("=", 50))

	fmt.Println("\n已实现的功能:")
	fmt.Println("  ✅ 插件配置管理 - 独立配置、热更新、变更监听")
	fmt.Println("  ✅ 插件状态查询 - 实时状态、运行时长、加载顺序")
	fmt.Println("  ✅ 插件间通信 - 事件总线、发布订阅、异步通信")
}
