package main

import (
	"fmt"
	"log"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/plugin"
	"github.com/KomeiDiSanXian/remilia/plugins/core/admin"
	"github.com/KomeiDiSanXian/remilia/plugins/core/cache"
	"github.com/KomeiDiSanXian/remilia/plugins/core/permission"
	"github.com/KomeiDiSanXian/remilia/plugins/core/storage"
)

func main() {
	// 初始化日志
	logCfg := logger.Config{
		Level:      "info",
		Console:    true,
		TimeFormat: "2006-01-02 15:04:05",
	}
	if err := logger.Init(logCfg); err != nil {
		log.Fatalf("Failed to init logger: %v", err)
	}

	fmt.Println("=== Remilia 核心插件演示 ===\n")

	// 创建引擎
	eng := engine.NewEngine()

	// 创建插件管理器
	manager := plugin.NewManager(eng)

	// 1. 注册 Storage Plugin
	fmt.Println("📦 注册 Storage Plugin...")
	storagePlugin := storage.New()
	if err := manager.Register(storagePlugin); err != nil {
		log.Fatalf("Failed to register storage plugin: %v", err)
	}

	// 测试存储功能
	fmt.Println("✅ Storage Plugin 已加载")
	storagePlugin.Set("test-key", []byte("test-value"), 5*time.Second)
	if value, err := storagePlugin.Get("test-key"); err == nil {
		fmt.Printf("   测试存储: %s = %s\n", "test-key", string(value))
	}

	// 测试 JSON 存储
	type User struct {
		Name string
		Age  int
	}
	user := User{Name: "Alice", Age: 25}
	storagePlugin.SetJSON("user:1", user, 0)
	var retrieved User
	storagePlugin.GetJSON("user:1", &retrieved)
	fmt.Printf("   JSON存储: %+v\n\n", retrieved)

	// 2. 注册 Permission Plugin
	fmt.Println("🔐 注册 Permission Plugin...")
	permPlugin := permission.New()
	if err := manager.Register(permPlugin); err != nil {
		log.Fatalf("Failed to register permission plugin: %v", err)
	}
	fmt.Println("✅ Permission Plugin 已加载")

	// 测试权限功能
	permPlugin.AssignRole("user123", "user")
	permPlugin.Grant("user123", "test.write")
	fmt.Printf("   用户权限: %v\n", permPlugin.GetUserPermissions("user123"))
	fmt.Printf("   用户角色: %v\n\n", permPlugin.GetUserRoles("user123"))

	// 3. 注册 Cache Plugin
	fmt.Println("⚡ 注册 Cache Plugin...")
	cachePlugin := cache.NewWithCapacity(100)
	if err := manager.Register(cachePlugin); err != nil {
		log.Fatalf("Failed to register cache plugin: %v", err)
	}
	fmt.Println("✅ Cache Plugin 已加载")

	// 测试缓存功能
	cachePlugin.Set("cached-key", []byte("cached-value"), 10*time.Second)
	if value, found := cachePlugin.Get("cached-key"); found {
		fmt.Printf("   缓存测试: %s = %s\n", "cached-key", string(value))
	}

	// 测试缓存统计
	cachePlugin.Get("cached-key")  // Hit
	cachePlugin.Get("nonexistent") // Miss
	stats := cachePlugin.Stats()
	fmt.Printf("   缓存统计: Hits=%d, Misses=%d, HitRate=%.2f%%\n\n",
		stats.Hits, stats.Misses, stats.HitRate()*100)

	// 4. 注册 Admin Plugin
	fmt.Println("⚙️  注册 Admin Plugin...")
	adminPlugin := admin.New()
	adminPlugin.SetPluginManager(manager)
	adminPlugin.SetPermissionPlugin(permPlugin)
	if err := manager.Register(adminPlugin); err != nil {
		log.Fatalf("Failed to register admin plugin: %v", err)
	}
	fmt.Println("✅ Admin Plugin 已加载\n")

	// 显示所有已加载的插件
	fmt.Println("📋 已加载的插件:")
	plugins := manager.ListWithMetadata()
	for name, meta := range plugins {
		fmt.Printf("   • %s v%s - %s\n", name, meta.Version, meta.Description)
	}

	// 显示插件依赖关系
	fmt.Println("\n🔗 插件依赖关系:")
	for _, name := range manager.List() {
		if p, ok := manager.GetMetadata(name); ok {
			if len(p.Dependencies) > 0 {
				fmt.Printf("   • %s 依赖: %v\n", name, p.Dependencies)
			}
		}
	}

	// 性能测试
	fmt.Println("\n⚡ 性能测试:")

	// Storage 性能
	start := time.Now()
	for i := 0; i < 10000; i++ {
		storagePlugin.Set(fmt.Sprintf("key-%d", i), []byte("value"), 0)
	}
	for i := 0; i < 10000; i++ {
		storagePlugin.Get(fmt.Sprintf("key-%d", i))
	}
	fmt.Printf("   Storage: 10000次写+10000次读 = %v\n", time.Since(start))

	// Cache 性能
	start = time.Now()
	for i := 0; i < 10000; i++ {
		cachePlugin.Set(fmt.Sprintf("key-%d", i), []byte("value"), 0)
	}
	for i := 0; i < 10000; i++ {
		cachePlugin.Get(fmt.Sprintf("key-%d", i))
	}
	fmt.Printf("   Cache: 10000次写+10000次读 = %v\n", time.Since(start))

	// Permission 性能
	start = time.Now()
	for i := 0; i < 10000; i++ {
		permPlugin.HasPermission("user123", "test.write")
	}
	fmt.Printf("   Permission: 10000次权限检查 = %v\n", time.Since(start))

	fmt.Println("\n✨ 所有核心插件演示完成！")
}
