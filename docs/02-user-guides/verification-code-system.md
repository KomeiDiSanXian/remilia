# 验证码权限系统

## 概述

验证码权限系统允许管理员通过生成临时验证码来授予用户权限，无需手动输入冗长的用户ID。用户只需私聊机器人发送验证码即可获得相应权限。

## 核心优势

### ✅ 便捷性
- **无需记忆用户ID**: 不用手动复制粘贴长长的用户ID（如 `F72FF2D66D2C8B0FDCE5AC40F8192AB0`）
- **简单易用**: 用户只需输入6位验证码（如 `ABC123`）即可获得权限
- **即时生效**: 验证成功后立即获得权限，无需等待

### 🔒 安全性
- **临时有效**: 验证码可设置过期时间（如30分钟、1小时、24小时）
- **使用限制**: 支持一次性、多次或无限次使用
- **可撤销**: 管理员随时可以撤销未使用的验证码
- **安全字符集**: 避免易混淆字符（0/O/I/l/1）

### 📊 可管理性
- **列表查看**: 查看所有有效验证码及其状态
- **使用追踪**: 记录验证码的使用情况
- **自动清理**: 过期验证码自动清理

## 架构设计

```
┌─────────────────────────────────────────────────────────────┐
│                      权限系统架构                             │
├─────────────────────────────────────────────────────────────┤
│                                                               │
│  ┌──────────────┐       ┌──────────────┐                    │
│  │              │       │              │                    │
│  │ Admin Plugin │◄──────┤ Permission   │                    │
│  │              │       │   Plugin     │                    │
│  └──────────────┘       └──────┬───────┘                    │
│         │                       │                            │
│         │                       │                            │
│         ▼                       ▼                            │
│  ┌─────────────────────────────────────┐                    │
│  │   Verification Manager               │                    │
│  ├─────────────────────────────────────┤                    │
│  │ - GenerateCode()                    │                    │
│  │ - VerifyCode()                      │                    │
│  │ - RevokeCode()                      │                    │
│  │ - ListCodes()                       │                    │
│  │ - CleanupExpired()                  │                    │
│  └─────────────────────────────────────┘                    │
│                                                               │
└─────────────────────────────────────────────────────────────┘
```

## 核心组件

### 1. VerificationManager (验证码管理器)

负责验证码的生成、验证、撤销和清理。

**主要方法:**
```go
// 生成验证码
GenerateCode(role string, expiry time.Duration, maxUses int) (string, error)

// 验证并使用验证码
VerifyCode(code, userID string) (string, bool, error)

// 撤销验证码
RevokeCode(code string) error

// 列出所有有效验证码
ListCodes() []*VerificationCode

// 清理过期验证码
CleanupExpired() int
```

### 2. VerificationCode (验证码信息)

```go
type VerificationCode struct {
    Code      string        // 验证码（6位字符）
    Role      string        // 授予的角色
    ExpiresAt time.Time     // 过期时间
    UsedBy    string        // 使用者ID
    CreatedAt time.Time     // 创建时间
    MaxUses   int           // 最大使用次数（0=一次性，-1=无限）
    UseCount  int           // 已使用次数
}
```

### 3. Permission Plugin (权限插件)

扩展了权限系统，集成验证码功能。

**新增方法:**
```go
// 生成验证码
GenerateVerificationCode(role string, expiry time.Duration, maxUses int) (string, error)

// 验证并授予角色
VerifyAndGrantRole(code, userID string) (string, error)

// 撤销验证码
RevokeVerificationCode(code string) error

// 列出验证码
ListVerificationCodes() []*VerificationCode
```

### 4. Admin Plugin (管理插件)

提供命令接口，方便用户操作。

## 使用指南

### 命令列表

#### 1. 生成验证码

```bash
/code gen <角色> [有效期] [最大使用次数]
```

**参数说明:**
- `角色`: 要授予的角色（如 admin, user, moderator）
- `有效期`: 可选，默认30分钟（如 30m, 1h, 24h）
- `最大使用次数`: 可选，默认0（一次性）
  - `0`: 一次性使用
  - `-1`: 无限次使用
  - `>0`: 指定次数

**示例:**
```bash
# 生成一个1小时有效、一次性使用的管理员验证码
/code gen admin 1h 0

# 生成一个24小时有效、可使用5次的用户验证码
/code gen user 24h 5

# 生成一个永久有效、无限次使用的moderator验证码
/code gen moderator 999999h -1
```

**输出示例:**
```
✅ 验证码已生成
========================================

🔑 验证码: ABC123
👤 授予角色: admin
⏰ 有效期: 1h0m0s
🎫 使用次数: 一次性

💡 使用方法:
  私聊机器人发送: /code verify ABC123

⚠️ 请妥善保管验证码，不要泄露给他人！
```

#### 2. 使用验证码

```bash
/code verify <验证码>
```

**示例:**
```bash
/code verify ABC123
```

**输出示例:**
```
✅ 验证成功！
========================================

🎉 您已获得角色: admin
👤 用户ID: F72FF2D66D2C8B0FDCE5AC40F8192AB0

💡 您现在可以使用该角色的所有权限！
```

#### 3. 列出验证码

```bash
/code list
```

**输出示例:**
```
📋 有效验证码列表 (共 3 个)
========================================

1. 验证码: ABC123
   角色: admin
   过期时间: 2026-02-11 17:00:00
   使用情况: 0/1 (一次性)

2. 验证码: XYZ789
   角色: user
   过期时间: 2026-02-12 10:00:00
   使用情况: 2/5

3. 验证码: DEF456
   角色: moderator
   过期时间: 2026-03-11 16:00:00
   使用情况: 15/∞
   最后使用者: USER123ABC
```

#### 4. 撤销验证码

```bash
/code revoke <验证码>
```

**示例:**
```bash
/code revoke ABC123
```

**输出:**
```
✅ 验证码 ABC123 已撤销
```

## 使用场景

### 场景 1: 授予首位管理员权限

**问题:** 刚启动机器人，需要给第一位管理员授权，但没有人有权限。

**解决方案:**
```go
// 在程序启动时自动生成初始管理员验证码
code, _ := permPlugin.GenerateVerificationCode("admin", 24*time.Hour, 1)
fmt.Println("初始管理员验证码:", code)
```

然后将验证码私发给第一位管理员，让他们使用 `/code verify` 命令。

### 场景 2: 临时授予权限

**问题:** 需要临时授予某人管理员权限，但不想永久添加。

**解决方案:**
```bash
# 生成30分钟有效的临时管理员验证码
/code gen admin 30m 1
```

30分钟后验证码自动失效，即使该用户没有使用也会自动清理。

### 场景 3: 批量邀请

**问题:** 需要邀请多个用户加入某个角色。

**解决方案:**
```bash
# 生成可使用10次的用户验证码
/code gen user 7d 10
```

将验证码分享给需要邀请的用户，他们各自使用即可。

### 场景 4: 紧急撤销

**问题:** 发现验证码泄露，需要紧急撤销。

**解决方案:**
```bash
# 立即撤销验证码
/code revoke ABC123
```

已使用的权限不受影响，但未使用的验证码立即失效。

## 安全考虑

### 1. 验证码强度

- **长度**: 6位字符
- **字符集**: 23456789ABCDEFGHJKLMNPQRSTUVWXYZ（避免混淆字符）
- **唯一性**: 自动检查重复
- **随机性**: 使用 `crypto/rand` 生成

### 2. 使用限制

- **过期时间**: 强制设置过期时间，防止永久有效
- **使用次数**: 限制使用次数，防止滥用
- **一次性使用**: 默认一次性使用，用后即销毁

### 3. 权限检查

- **生成验证码**: 需要管理员权限
- **列出验证码**: 需要管理员权限
- **撤销验证码**: 需要管理员权限
- **使用验证码**: 任何人可用（但验证码本身是秘密）

### 4. 自动清理

- **后台任务**: 每5分钟自动清理过期验证码
- **内存管理**: 及时释放无用数据

## API 使用示例

### 编程方式使用

```go
package main

import (
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/core/permission"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

func main() {
	eng := engine.NewEngine()
	pm := plugin.NewManager(eng)

	// 注册权限插件
	pm.Register(permission.New())

	// 获取插件实例
	p := pm.GetContainer().MustGetService[permission.Plugin]("permission")

	// 生成验证码
	code, err := p.GenerateVerificationCode(
		"admin",     // 角色
		1*time.Hour, // 有效期
		0,           // 一次性使用
	)
	if err != nil {
		panic(err)
	}

	println("验证码:", code)

	// 验证并授予角色
	role, err := p.VerifyAndGrantRole(code, "USER_ID_HERE")
	if err != nil {
		panic(err)
	}

	println("授予角色:", role)

	// 列出所有验证码
	codes := p.ListVerificationCodes()
	for _, c := range codes {
		println("Code:", c.Code, "Role:", c.Role)
	}
}
```

## 测试

运行测试：

```bash
cd builtin/core/permission
go test -v
```

**测试覆盖:**
- ✅ 验证码生成
- ✅ 验证码唯一性
- ✅ 验证码验证
- ✅ 一次性使用限制
- ✅ 多次使用限制
- ✅ 无限次使用
- ✅ 过期检查
- ✅ 撤销功能
- ✅ 列表查询
- ✅ 自动清理

## 配置建议

### 推荐配置

```go
// 生产环境推荐配置参数
const (
    defaultExpiry   = 30 * time.Minute  // 默认30分钟
    maxExpiry       = 24 * time.Hour    // 最长24小时
    cleanupInterval = 5 * time.Minute   // 每5分钟清理
    codeLength      = 6                 // 6位验证码
)
```

### 不同场景配置

**高安全场景:**
```go
expiry := 10 * time.Minute  // 短有效期
maxUses := 0                // 一次性
```

**便捷场景:**
```go
expiry := 7 * 24 * time.Hour  // 长有效期
maxUses := 10                 // 多次使用
```

**演示/测试场景:**
```go
expiry := 24 * time.Hour  // 中等有效期
maxUses := -1             // 无限次
```

## 常见问题

### Q: 验证码会重复吗？

**A:** 不会。生成时会自动检查重复，确保唯一性。

### Q: 验证码过期后会怎样？

**A:** 过期的验证码无法使用，且会被自动清理（每5分钟一次）。

### Q: 可以修改验证码吗？

**A:** 不可以。验证码生成后不可修改，但可以撤销后重新生成。

### Q: 忘记验证码怎么办？

**A:** 使用 `/code list` 命令查看所有有效的验证码。

### Q: 验证码可以分享吗？

**A:** 验证码是秘密凭证，不建议公开分享。如需批量邀请，可生成多次使用的验证码。

### Q: 如何撤回已授予的权限？

**A:** 验证码只负责授权，权限撤回需要使用权限管理命令 `/perm revoke`。

## 未来改进

- [ ] 支持验证码别名/备注
- [ ] 邮件/短信发送验证码
- [ ] 验证码使用日志
- [ ] 验证码模板
- [ ] IP白名单限制
- [ ] 二次验证
