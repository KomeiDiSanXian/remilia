package main

import (
	"fmt"
	"strings"

	"github.com/KomeiDiSanXian/remilia/plugins/core/permission"
)

func main() {
	fmt.Println("🛡️  黑白名单功能演示")
	fmt.Println(strings.Repeat("=", 50))

	// 创建权限插件
	permPlugin := permission.New()

	fmt.Println("\n📋 功能演示:")
	fmt.Println(strings.Repeat("-", 50))

	// 1. 默认模式（禁用）
	fmt.Println("\n1️⃣  默认模式 - 禁用")
	mode := permPlugin.GetACLMode()
	fmt.Printf("当前模式: %s\n", mode.String())
	allowed, reason := permPlugin.IsUserAllowed("user123")
	fmt.Printf("用户 user123 是否允许: %v (原因: %s)\n", allowed, reason)

	// 2. 黑名单模式
	fmt.Println("\n2️⃣  黑名单模式")
	permPlugin.SetACLMode(permission.ModeBlacklist)
	fmt.Printf("切换到: %s\n", permPlugin.GetACLMode().String())

	// 添加用户到黑名单
	permPlugin.AddToACL("baduser", "违反规则")
	fmt.Println("✅ 添加用户 baduser 到黑名单（原因: 违反规则）")

	// 检查用户
	allowed, reason = permPlugin.IsUserAllowed("baduser")
	fmt.Printf("用户 baduser 是否允许: %v (原因: %s)\n", allowed, reason)

	allowed, reason = permPlugin.IsUserAllowed("gooduser")
	fmt.Printf("用户 gooduser 是否允许: %v (原因: %s)\n", allowed, reason)

	// 3. 白名单模式
	fmt.Println("\n3️⃣  白名单模式")
	permPlugin.SetACLMode(permission.ModeWhitelist)
	permPlugin.ClearACL() // 清空之前的黑名单
	fmt.Printf("切换到: %s\n", permPlugin.GetACLMode().String())

	// 添加用户到白名单
	permPlugin.AddToACL("vipuser1", "VIP会员")
	permPlugin.AddToACL("vipuser2", "管理员")
	permPlugin.AddToACL("vipuser3", "")
	fmt.Println("✅ 添加3个用户到白名单")

	// 检查用户
	allowed, reason = permPlugin.IsUserAllowed("vipuser1")
	fmt.Printf("用户 vipuser1 是否允许: %v (原因: %s)\n", allowed, reason)

	allowed, reason = permPlugin.IsUserAllowed("normaluser")
	fmt.Printf("用户 normaluser 是否允许: %v (原因: %s)\n", allowed, reason)

	// 4. 列出所有用户
	fmt.Println("\n4️⃣  列出白名单中的用户")
	users := permPlugin.ListACL()
	fmt.Printf("共 %d 个用户:\n", len(users))
	for i, user := range users {
		fmt.Printf("  %d. %s", i+1, user.UserID)
		if user.Note != "" {
			fmt.Printf(" (备注: %s)", user.Note)
		}
		fmt.Println()
	}

	// 5. 移除用户
	fmt.Println("\n5️⃣  移除用户")
	removed := permPlugin.RemoveFromACL("vipuser2")
	if removed {
		fmt.Println("✅ 已移除用户 vipuser2")
	}
	fmt.Printf("剩余用户数: %d\n", permPlugin.GetACLCount())

	// 6. 统计信息
	fmt.Println("\n6️⃣  统计信息")
	stats := permPlugin.GetACLStats()
	fmt.Printf("当前模式: %s\n", stats.Mode.String())
	fmt.Printf("用户数量: %d\n", stats.UserCount)

	// 7. 黑名单场景演示
	fmt.Println("\n7️⃣  实际场景 - 黑名单模式")
	permPlugin.SetACLMode(permission.ModeBlacklist)
	permPlugin.ClearACL()

	// 模拟封禁几个违规用户
	bannedUsers := []struct {
		id     string
		reason string
	}{
		{"USER_SPAM", "发送垃圾信息"},
		{"USER_ABUSE", "辱骂他人"},
		{"USER_CHEAT", "使用作弊工具"},
	}

	fmt.Println("封禁违规用户:")
	for _, user := range bannedUsers {
		permPlugin.AddToACL(user.id, user.reason)
		fmt.Printf("  ⚠️  %s - %s\n", user.id, user.reason)
	}

	// 检查各种用户
	fmt.Println("\n用户访问检查:")
	testUsers := []string{"USER_SPAM", "USER_NORMAL", "USER_ABUSE", "USER_VIP"}
	for _, userID := range testUsers {
		allowed, reason := permPlugin.IsUserAllowed(userID)
		if allowed {
			fmt.Printf("  ✅ %s - 允许访问\n", userID)
		} else {
			fmt.Printf("  ❌ %s - %s\n", userID, reason)
		}
	}

	// 8. 白名单场景演示
	fmt.Println("\n8️⃣  实际场景 - 白名单模式（内测）")
	permPlugin.SetACLMode(permission.ModeWhitelist)
	permPlugin.ClearACL()

	// 添加内测用户
	betaTesters := []struct {
		id   string
		note string
	}{
		{"TESTER_001", "核心测试员"},
		{"TESTER_002", "功能测试员"},
		{"ADMIN_001", "项目管理员"},
	}

	fmt.Println("添加内测用户:")
	for _, tester := range betaTesters {
		permPlugin.AddToACL(tester.id, tester.note)
		fmt.Printf("  ✅ %s - %s\n", tester.id, tester.note)
	}

	// 检查访问
	fmt.Println("\n用户访问检查:")
	testUsers2 := []string{"TESTER_001", "RANDOM_USER", "ADMIN_001", "PUBLIC_USER"}
	for _, userID := range testUsers2 {
		allowed, reason := permPlugin.IsUserAllowed(userID)
		if allowed {
			fmt.Printf("  ✅ %s - 允许访问（内测用户）\n", userID)
		} else {
			fmt.Printf("  ❌ %s - %s\n", userID, reason)
		}
	}

	// 9. 清空并禁用
	fmt.Println("\n9️⃣  清空列表并禁用")
	count := permPlugin.ClearACL()
	fmt.Printf("✅ 清空了 %d 个用户\n", count)

	permPlugin.SetACLMode(permission.ModeDisabled)
	fmt.Printf("✅ 切换到: %s\n", permPlugin.GetACLMode().String())

	// 最终检查
	fmt.Println("\n禁用模式下的访问:")
	for _, userID := range []string{"USER1", "USER2", "USER3"} {
		allowed, _ := permPlugin.IsUserAllowed(userID)
		if allowed {
			fmt.Printf("  ✅ %s - 允许访问\n", userID)
		}
	}

	fmt.Println("\n" + strings.Repeat("=", 50))
	fmt.Println("✅ 演示完成！")
	fmt.Println("\n💡 在实际使用中:")
	fmt.Println("   黑名单模式:")
	fmt.Println("     /acl mode blacklist     # 启用黑名单")
	fmt.Println("     /acl add <用户ID> <原因> # 添加到黑名单")
	fmt.Println("     /acl list               # 查看黑名单")
	fmt.Println("\n   白名单模式:")
	fmt.Println("     /acl mode whitelist     # 启用白名单")
	fmt.Println("     /acl add <用户ID> <备注> # 添加到白名单")
	fmt.Println("     /acl list               # 查看白名单")
	fmt.Println("\n   禁用:")
	fmt.Println("     /acl mode disabled      # 禁用黑白名单")
	fmt.Println(strings.Repeat("=", 50))
}
