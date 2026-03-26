package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/core/storage"
)

func main() {
	fmt.Println("=== SQLite Storage Plugin 演示 ===")

	// 创建数据目录
	if err := os.MkdirAll("data", 0755); err != nil {
		log.Fatalf("Failed to create data directory: %v", err)
	}

	// 创建 SQLite 存储后端
	fmt.Println("📦 创建 SQLite 存储后端...")
	sqliteStorage, err := storage.NewSQLiteStorage("data/example.db")
	if err != nil {
		log.Fatalf("Failed to create SQLite storage: %v", err)
	}
	defer sqliteStorage.Close()

	// 通过 NewPlugin 直接创建存储插件（无需 lifecycle 系统）
	storagePlugin := storage.NewPlugin(sqliteStorage)
	fmt.Println("✅ SQLite Storage Plugin 已加载")

	// 1. 基本操作
	fmt.Println("1️⃣  基本操作演示")
	fmt.Println(strings.Repeat("-", 40))

	if err := storagePlugin.Set("user:1", []byte("Alice"), 0); err != nil {
		log.Fatalf("Set user:1: %v", err)
	}
	if err := storagePlugin.Set("user:2", []byte("Bob"), 0); err != nil {
		log.Fatalf("Set user:2: %v", err)
	}
	fmt.Println("✓ 设置了 2 个用户")

	if value, err := storagePlugin.Get("user:1"); err == nil {
		fmt.Printf("✓ 获取用户: %s\n", string(value))
	} else {
		fmt.Printf("✗ 获取用户失败: %v\n", err)
	}

	if storagePlugin.Exists("user:2") {
		fmt.Println("✓ user:2 存在")
	}

	fmt.Println()

	// 2. JSON 操作
	fmt.Println("2️⃣  JSON 操作演示")
	fmt.Println(strings.Repeat("-", 40))

	type User struct {
		Name  string
		Age   int
		Email string
	}

	user := User{Name: "Charlie", Age: 30, Email: "charlie@example.com"}
	if err := storagePlugin.SetJSON("user:json:3", user, 0); err != nil {
		log.Fatalf("SetJSON: %v", err)
	}
	fmt.Printf("✓ 保存用户: %+v\n", user)

	var retrieved User
	if err := storagePlugin.GetJSON("user:json:3", &retrieved); err != nil {
		fmt.Printf("✗ GetJSON 失败: %v\n", err)
	} else {
		fmt.Printf("✓ 读取用户: %+v\n", retrieved)
	}

	fmt.Println()

	// 3. TTL 过期
	fmt.Println("3️⃣  TTL 过期演示")
	fmt.Println(strings.Repeat("-", 40))

	if err := storagePlugin.Set("session:abc", []byte("session-data"), 2*time.Second); err != nil {
		log.Fatalf("Set session: %v", err)
	}
	fmt.Println("✓ 设置会话，TTL=2秒")

	fmt.Println("  等待 1 秒...")
	time.Sleep(1 * time.Second)
	if storagePlugin.Exists("session:abc") {
		fmt.Println("  ✓ 会话仍然存在")
	}

	fmt.Println("  等待 2 秒...")
	time.Sleep(2 * time.Second)
	if !storagePlugin.Exists("session:abc") {
		fmt.Println("  ✓ 会话已过期")
	}

	fmt.Println()

	// 4. 键查询
	fmt.Println("4️⃣  键查询演示")
	fmt.Println(strings.Repeat("-", 40))

	for _, kv := range []struct{ k, v string }{
		{"product:1", "Laptop"},
		{"product:2", "Mouse"},
		{"product:3", "Keyboard"},
	} {
		storagePlugin.Set(kv.k, []byte(kv.v), 0)
	}

	keys, _ := storagePlugin.Keys("user:*")
	fmt.Printf("✓ 查询 'user:*': %d 个键\n", len(keys))
	for _, key := range keys {
		fmt.Printf("  - %s\n", key)
	}

	keys, _ = storagePlugin.Keys("product:*")
	fmt.Printf("✓ 查询 'product:*': %d 个键\n", len(keys))

	fmt.Println()

	// 5. 数据统计
	fmt.Println("5️⃣  数据统计演示")
	fmt.Println(strings.Repeat("-", 40))

	stats, err := sqliteStorage.Stats()
	if err != nil {
		fmt.Printf("✗ 获取统计信息失败: %v\n", err)
	} else {
		fmt.Printf("✓ 总键数: %v\n", stats["total_keys"])
		fmt.Printf("✓ 有效键数: %v\n", stats["valid_keys"])
		fmt.Printf("✓ 过期键数: %v\n", stats["expired_keys"])
		fmt.Printf("✓ 数据库大小: %v 字节\n", stats["db_size_bytes"])
		fmt.Printf("✓ 数据库路径: %v\n", stats["db_path"])
	}

	fmt.Println()

	// 6. 数据持久化验证
	fmt.Println("6️⃣  数据持久化验证")
	fmt.Println(strings.Repeat("-", 40))

	if err := storagePlugin.Set("persistent:key", []byte("persistent-value"), 0); err != nil {
		fmt.Printf("✗ 持久化写入失败: %v\n", err)
	} else {
		fmt.Println("✓ 保存了持久化数据")
		fmt.Println("  （重启应用后数据仍然存在）")
	}

	fmt.Println()

	// 7. 性能测试
	fmt.Println("7️⃣  性能测试")
	fmt.Println(strings.Repeat("-", 40))

	start := time.Now()
	for i := 0; i < 1000; i++ {
		storagePlugin.Set(fmt.Sprintf("bench:key:%d", i), []byte("value"), 0)
	}
	writeTime := time.Since(start)
	fmt.Printf("✓ 1000 次写入: %v (%.2f op/s)\n", writeTime, 1000.0/writeTime.Seconds())

	start = time.Now()
	for i := 0; i < 1000; i++ {
		storagePlugin.Get(fmt.Sprintf("bench:key:%d", i))
	}
	readTime := time.Since(start)
	fmt.Printf("✓ 1000 次读取: %v (%.2f op/s)\n", readTime, 1000.0/readTime.Seconds())

	fmt.Println()

	// 8. 清理操作
	fmt.Println("8️⃣  清理操作演示")
	fmt.Println(strings.Repeat("-", 40))

	for i := 0; i < 10; i++ {
		storagePlugin.Set(fmt.Sprintf("temp:key:%d", i), []byte("temp"), 100*time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)

	count, err := sqliteStorage.CleanExpired()
	if err != nil {
		fmt.Printf("✗ 清理过期键失败: %v\n", err)
	} else {
		fmt.Printf("✓ 清理了 %d 个过期键\n", count)
	}

	fmt.Println("✓ 压缩数据库...")
	if err := sqliteStorage.Compact(); err != nil {
		fmt.Printf("✗ 压缩失败: %v\n", err)
	}

	size, err := sqliteStorage.Size()
	if err != nil {
		fmt.Printf("✗ 获取大小失败: %v\n", err)
	} else {
		fmt.Printf("✓ 当前有效键数: %d\n", size)
	}

	fmt.Println()
	fmt.Println("✨ SQLite Storage Plugin 演示完成！")
	fmt.Println()
	fmt.Println("💡 提示: 数据已保存到 data/example.db")
	fmt.Println("   可以使用 sqlite3 命令行工具查看数据库内容")
}
