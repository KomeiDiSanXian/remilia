# 权限系统架构

Remilia 的权限系统分为**两个层次**，包名相同但职责完全不同。初次使用时请先阅读本文档。

---

## 架构全景

```
┌───────────────────────────────────────────────────────────────┐
│                       Bot / 插件开发者                          │
│                                                               │
│  plugin.Service[permission.Plugin](ctx, "permission")            │
│  perm.CheckPermission(ctx, Permission{"admin","kick"})        │
└─────────────────────────┬─────────────────────────────────────┘
                          │ 使用
┌─────────────────────────▼─────────────────────────────────────┐
│            builtin/core/permission  （管理插件层）               │
│                                                               │
│  · Plugin struct — 插件 API 对象                               │
│  · /acl add/rm/list 命令                                       │
│  · VerificationManager — 身份验证管理                          │
│  · AccessControlList — ACL 黑白名单                            │
│  · StorageBackend — 可选持久化接口                             │
└─────────────────────────┬─────────────────────────────────────┘
                          │ 依赖
┌─────────────────────────▼─────────────────────────────────────┐
│               core/permission  （内核层）                        │
│                                                               │
│  · Permission — (resource, action) 权限对，支持通配符           │
│  · Role       — 权限集合                                       │
│  · Manager    — 用户↔角色↔权限映射表（线程安全）               │
│  · Provider   — 可选外部角色数据源接口                         │
│                                                               │
│  零依赖：不引入插件系统、不引入 core/context                    │
└───────────────────────────────────────────────────────────────┘
```

---

## `core/permission`：权限原语（内核层）

**包路径**：`github.com/KomeiDiSanXian/remilia/core/permission`

**职责**：定义 RBAC 的数据结构和算法，不暴露任何命令或 HTTP 接口。

**特点**：
- 零外部依赖，可在非 Bot 场景（HTTP 服务、CLI 工具）中独立使用
- `Manager` 是线程安全的内存权限表
- 支持通配符权限（`"*"` 匹配任意资源/动作，`"prefix:*"` 匹配前缀）
- **通配符只在授权侧展开**（2026-07 收紧）：请求侧（`HasPermission` 的 target 参数）
  出现的 `"*"` 一律按字面值处理——否则把用户可控字符串透传进权限检查的调用方
  可被 `"*"` 探测/绕过。另外 `Provider` 外部查询移到锁外执行，慢查询不再阻塞写操作

**通过 Context 访问**：

`core/context` 将 `*Manager` 存储在 typed-extension 中，提供两个便捷方法：

```go
// 在中间件中注入 Manager
ctx.SetPermissionManager(myManager)

// 在 handler 中获取 Manager
pm := ctx.GetPermissionManager()
if pm != nil {
    allowed := pm.CheckPermission(userID, eventctx.NewPermission("message", "send"))
}
```

---

## `builtin/core/permission`：权限管理插件（插件层）

**包路径**：`github.com/KomeiDiSanXian/remilia/builtin/core/permission`

**职责**：基于 `core/permission` 构建，向 Bot 用户暴露运行时权限管理命令。

**提供的功能**：
- `/acl add <user> <role>` — 为用户授予角色
- `/acl rm  <user> <role>` — 撤销用户角色
- `/acl list <user>`       — 查询用户角色和权限
- 身份验证（VerificationManager）
- ACL 黑白名单（AccessControlList）
- 可选对接 `plugins/core/storage` 实现持久化

**注册方式**：

```go
// 仅注册权限管理插件（内存模式，重启丢失）
pm.Register(permission.New())

// 带持久化（先注册 storage 插件）
pm.Register(storage.New())
pm.Register(permission.New()) // 自动通过 Try[storage.Plugin] 获取存储后端
```

**在其他插件中使用**：

```go
import permission "github.com/KomeiDiSanXian/remilia/builtin/core/permission"

Setup: func(ctx *plugin.SetupContext) (any, error) {
    perm := plugin.Service[permission.Plugin](ctx, "permission")
    ctx.Reg.RegisterCommand(dto.C2CMessageCreate, "/kick").Handle(func(c *eventctx.Context) error {
        if !perm.CheckPermission(c, eventctx.NewPermission("admin", "kick")) {
            c.ReplyText("权限不足")
            return nil
        }
        // ...
        return nil
    })
    return p, nil
},
```

---

## 常见误区

| 问题 | 正确理解 |
|---|---|
| 为什么有两个 `permission` 包？ | 一个是数据结构（内核），一个是功能插件（应用层），职责分离 |
| 我只需要在代码中做权限检查，用哪个？ | 用 `core/permission` 的 `Manager`，通过 `ctx.GetPermissionManager()` 访问 |
| 我需要用户在聊天中管理权限，用哪个？ | 注册 `builtin/core/permission` 插件，通过 `/acl` 命令管理 |
| 两者能混用吗？ | 可以。插件层内部使用内核层的 `Manager`，通过 `ctx.SetPermissionManager` 注入到 Context |

