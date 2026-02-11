package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/plugins/core/permission"
)

func main() {
	fmt.Println("🤖 验证码权限系统演示")
	fmt.Println(strings.Repeat("=", 50))

	// 创建权限插件并演示验证码功能
	permPlugin := permission.New()

	fmt.Println("\n📋 功能演示:")
	fmt.Println(strings.Repeat("-", 50))

	// 1. 生成验证码
	fmt.Println("\n1️⃣  生成管理员验证码...")
	code, err := permPlugin.GenerateVerificationCode("admin", 1*time.Hour, 0)
	if err != nil {
		fmt.Printf("❌ 生成失败: %v\n", err)
		return
	}
	fmt.Printf("✅ 验证码: %s (有效期: 1小时, 一次性)\n", code)

	// 2. 列出验证码
	fmt.Println("\n2️⃣  列出所有验证码...")
	codes := permPlugin.ListVerificationCodes()
	fmt.Printf("✅ 当前有 %d 个有效验证码\n", len(codes))
	for i, c := range codes {
		fmt.Printf("   %d. %s - 角色: %s, 过期: %s\n",
			i+1, c.Code, c.Role, c.ExpiresAt.Format("15:04:05"))
	}

	// 3. 验证并授予角色
	fmt.Println("\n3️⃣  使用验证码获取权限...")
	userID := "USER123ABC"
	role, err := permPlugin.VerifyAndGrantRole(code, userID)
	if err != nil {
		fmt.Printf("❌ 验证失败: %v\n", err)
		return
	}
	fmt.Printf("✅ 用户 %s 获得角色: %s\n", userID, role)

	// 4. 检查权限
	fmt.Println("\n4️⃣  检查用户权限...")
	roles := permPlugin.GetUserRoles(userID)
	fmt.Printf("✅ 用户 %s 的角色: %v\n", userID, roles)

	// 5. 再次尝试使用（应该失败，因为是一次性）
	fmt.Println("\n5️⃣  尝试再次使用验证码（应该失败）...")
	_, err = permPlugin.VerifyAndGrantRole(code, "USER456DEF")
	if err != nil {
		fmt.Printf("✅ 预期失败: %v\n", err)
	} else {
		fmt.Println("❌ 不应该成功")
	}

	// 6. 生成多次使用的验证码
	fmt.Println("\n6️⃣  生成多次使用验证码...")
	multiCode, err := permPlugin.GenerateVerificationCode("user", 30*time.Minute, 3)
	if err != nil {
		fmt.Printf("❌ 生成失败: %v\n", err)
		return
	}
	fmt.Printf("✅ 验证码: %s (有效期: 30分钟, 可用3次)\n", multiCode)

	// 7. 使用多次验证码
	fmt.Println("\n7️⃣  使用多次验证码...")
	for i := 1; i <= 4; i++ {
		testUserID := fmt.Sprintf("USER%d", i)
		_, err := permPlugin.VerifyAndGrantRole(multiCode, testUserID)
		if err != nil {
			fmt.Printf("   第%d次使用: ❌ %v\n", i, err)
		} else {
			fmt.Printf("   第%d次使用: ✅ 成功 (用户: %s)\n", i, testUserID)
		}
	}

	// 8. 撤销验证码
	fmt.Println("\n8️⃣  生成并撤销验证码...")
	revokeCode, _ := permPlugin.GenerateVerificationCode("moderator", 1*time.Hour, 0)
	fmt.Printf("✅ 生成验证码: %s\n", revokeCode)
	err = permPlugin.RevokeVerificationCode(revokeCode)
	if err != nil {
		fmt.Printf("❌ 撤销失败: %v\n", err)
	} else {
		fmt.Println("✅ 验证码已撤销")
	}

	// 尝试使用已撤销的验证码
	_, err = permPlugin.VerifyAndGrantRole(revokeCode, "USER999")
	if err != nil {
		fmt.Printf("✅ 撤销后无法使用: %v\n", err)
	} else {
		fmt.Println("❌ 不应该成功")
	}

	// 9. 最终状态
	fmt.Println("\n9️⃣  最终状态...")
	codes = permPlugin.ListVerificationCodes()
	fmt.Printf("✅ 剩余有效验证码: %d 个\n", len(codes))

	fmt.Println("\n" + strings.Repeat("=", 50))
	fmt.Println("✅ 演示完成！")
	fmt.Println("\n💡 在实际使用中:")
	fmt.Println("   - 管理员使用 /code gen 命令生成验证码")
	fmt.Println("   - 用户私聊机器人使用 /code verify <验证码> 获取权限")
	fmt.Println("   - 管理员使用 /code list 查看所有验证码")
	fmt.Println("   - 管理员使用 /code revoke <验证码> 撤销验证码")
	fmt.Println(strings.Repeat("=", 50))
}
