# 命令权限系统文档

## 概述

Remilia v0.9.0 引入了完整的 RBAC（基于角色的访问控制）权限系统，提供细粒度的命令和资源访问控制。

## 🌟 主要特性

- ✅ **基于角色的访问控制（RBAC）** - 角色和权限分离
- ✅ **通配符支持** - 灵活的权限匹配（如 `command:*`）
- ✅ **直接授权** - 支持直接给用户授予权限
- ✅ **权限提供者** - 可从外部系统获取权限
- ✅ **中间件集成** - 简单易用的权限检查中间件
- ✅ **并发安全** - 完全的线程安全保证

## 📚 快速开始

### 基本用法

```go
// 1. 创建权限管理器
pm := remilia.NewPermissionManager()

// 2. 注入到 Engine
engine := remilia.NewEngine()
engine.Use(remilia.RequirePermissionMiddleware(pm))

// 3. 分配角色给用户
pm.AssignRole("user123", "admin")

// 4. 使用权限中间件保护命令
engine.On(remilia.OnGroupAtMessage(), remilia.OnCommand("/admin")).
    Use(remilia.RequireRole("admin")).
    HandleE(func(ctx *remilia.Context) error {
        return ctx.ReplyGroup(&dto.Message{
            Content: "管理员命令执行成功",
            Type:    dto.TextMessage,
        })
    })
```

## 📖 核心概念

### 1. Permission (权限)

权限由**资源**和**操作**组成：

```go
type Permission struct {
    Resource string // 资源名称
    Action   string // 操作类型
}

// 示例
perm := remilia.Permission{
    Resource: "command:weather",
    Action:   "execute",
}
```

**通配符支持**:
- `*:*` - 所有资源的所有操作
- `command:*` - command 开头的所有资源
- `admin:users:*` - 操作通配符

### 2. Role (角色)

角色是权限的集合：

```go
// 创建角色
role := remilia.NewRole("moderator",
    remilia.Permission{Resource: "command:*", Action: "execute"},
    remilia.Permission{Resource: "user:*", Action: "view"},
)

// 动态添加权限
role.AddPermission(remilia.Permission{
    Resource: "message:*",
    Action:   "delete",
})
```

### 3. PermissionManager (权限管理器)

管理角色、用户和权限：

```go
pm := remilia.NewPermissionManager()

// 角色管理
pm.RegisterRole(role)
pm.AssignRole("user123", "moderator")
pm.RevokeRole("user123", "guest")

// 直接授权
pm.GrantPermission("user123", remilia.Permission{
    Resource: "special", 
    Action:   "execute",
})

// 权限检查
if pm.HasPermission("user123", remilia.Permission{
    Resource: "command:test",
    Action:   "execute",
}) {
    // 用户有权限
}
```

## 🎯 默认角色

### admin (管理员)
```go
Permission{Resource: "*", Action: "*"}
```
- 拥有所有权限

### user (普通用户)
```go
Permission{Resource: "command:*", Action: "execute"}
Permission{Resource: "query:*", Action: "view"}
```
- 可以执行所有命令
- 可以查询信息

### guest (访客)
```go
Permission{Resource: "query:*", Action: "view"}
```
- 仅可查询信息
- 不能执行命令

## 🔧 使用示例

### 示例 1: 简单权限检查

```go
engine.On(remilia.OnGroupAtMessage(), remilia.OnCommand("/info")).HandleE(func(ctx *remilia.Context) error {
    // 检查权限
    if !ctx.HasPermission("query:info", "view") {
        return ctx.ReplyGroup(&dto.Message{
            Content: "❌ 没有权限查看信息",
            Type:    dto.TextMessage,
        })
    }
    
    // 执行操作
    return ctx.ReplyGroup(&dto.Message{
        Content: "✅ 信息内容...",
        Type:    dto.TextMessage,
    })
})
```

### 示例 2: 使用中间件

```go
// 要求特定权限
engine.On(remilia.OnGroupAtMessage(), remilia.OnCommand("/delete")).
    Use(remilia.RequirePermission("message", "delete")).
    HandleE(func(ctx *remilia.Context) error {
        // 有权限才会执行到这里
        return ctx.ReplyGroup(&dto.Message{
            Content: "消息已删除",
            Type:    dto.TextMessage,
        })
    })

// 要求特定角色
engine.On(remilia.OnGroupAtMessage(), remilia.OnCommand("/admin")).
    Use(remilia.RequireRole("admin")).
    HandleE(func(ctx *remilia.Context) error {
        // 只有 admin 角色才能执行
        return ctx.ReplyGroup(&dto.Message{
            Content: "管理面板...",
            Type:    dto.TextMessage,
        })
    })
```

### 示例 3: 任意权限（OR）

```go
// 满足任意一个权限即可
engine.On(remilia.OnGroupAtMessage(), remilia.OnCommand("/moderate")).
    Use(remilia.RequireAnyPermission(
        remilia.Permission{Resource: "admin:*", Action: "*"},
        remilia.Permission{Resource: "moderator:*", Action: "*"},
    )).
    HandleE(func(ctx *remilia.Context) error {
        return ctx.ReplyGroup(&dto.Message{
            Content: "管理操作成功",
            Type:    dto.TextMessage,
        })
    })
```

### 示例 4: 所有权限（AND）

```go
// 需要同时拥有所有权限
engine.On(remilia.OnGroupAtMessage(), remilia.OnCommand("/export")).
    Use(remilia.RequireAllPermissions(
        remilia.Permission{Resource: "data", Action: "read"},
        remilia.Permission{Resource: "data", Action: "export"},
    )).
    HandleE(func(ctx *remilia.Context) error {
        return ctx.ReplyGroup(&dto.Message{
            Content: "数据导出中...",
            Type:    dto.TextMessage,
        })
    })
```

### 示例 5: 自定义角色

```go
// 创建自定义角色
moderator := remilia.NewRole("moderator",
    remilia.Permission{Resource: "message:*", Action: "delete"},
    remilia.Permission{Resource: "user:*", Action: "mute"},
    remilia.Permission{Resource: "command:*", Action: "execute"},
)

pm.RegisterRole(moderator)

// 分配给用户
pm.AssignRole("user123", "moderator")

// 使用角色
engine.On(remilia.OnGroupAtMessage(), remilia.OnCommand("/mute")).
    Use(remilia.RequireRole("moderator")).
    HandleE(func(ctx *remilia.Context) error {
        // 执行禁言操作
        return nil
    })
```

### 示例 6: 动态权限管理

```go
// 授予权限命令
engine.On(remilia.OnGroupAtMessage(), remilia.OnCommand("/grant")).
    Use(remilia.RequireRole("admin")).
    HandleE(func(ctx *remilia.Context) error {
        args, _ := ctx.ParseCommand()
        targetUser := args.Get(0)
        resource := args.Get(1)
        action := args.Get(2)
        
        pm.GrantPermission(targetUser, remilia.Permission{
            Resource: resource,
            Action:   action,
        })
        
        return ctx.ReplyGroup(&dto.Message{
            Content: fmt.Sprintf("已授予 %s 权限: %s:%s", targetUser, resource, action),
            Type:    dto.TextMessage,
        })
    })

// 撤销权限命令
engine.On(remilia.OnGroupAtMessage(), remilia.OnCommand("/revoke")).
    Use(remilia.RequireRole("admin")).
    HandleE(func(ctx *remilia.Context) error {
        args, _ := ctx.ParseCommand()
        targetUser := args.Get(0)
        resource := args.Get(1)
        action := args.Get(2)
        
        pm.RevokePermission(targetUser, remilia.Permission{
            Resource: resource,
            Action:   action,
        })
        
        return ctx.ReplyGroup(&dto.Message{
            Content: fmt.Sprintf("已撤销 %s 权限: %s:%s", targetUser, resource, action),
            Type:    dto.TextMessage,
        })
    })
```

### 示例 7: 查询用户权限

```go
engine.On(remilia.OnGroupAtMessage(), remilia.OnCommand("/myperms")).HandleE(func(ctx *remilia.Context) error {
    pm, _ := ctx.GetState("permission_manager")
    permManager := pm.(*remilia.PermissionManager)
    
    userID := ctx.getUserID()
    
    // 获取角色
    roles := permManager.GetUserRoles(userID)
    rolesStr := strings.Join(roles, ", ")
    
    // 获取权限
    perms := permManager.GetUserPermissions(userID)
    var permsStr []string
    for _, p := range perms {
        permsStr = append(permsStr, p.String())
    }
    
    return ctx.ReplyGroup(&dto.Message{
        Content: fmt.Sprintf("您的角色: %s\n您的权限: %s", rolesStr, strings.Join(permsStr, ", ")),
        Type:    dto.TextMessage,
    })
})
```

## 🔌 权限提供者

支持从外部系统（如数据库）获取权限：

```go
// 实现 PermissionProvider 接口
type DatabasePermissionProvider struct {
    db *sql.DB
}

func (p *DatabasePermissionProvider) GetUserRoles(userID string) ([]string, error) {
    var roles []string
    rows, err := p.db.Query("SELECT role FROM user_roles WHERE user_id = ?", userID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    
    for rows.Next() {
        var role string
        rows.Scan(&role)
        roles = append(roles, role)
    }
    return roles, nil
}

func (p *DatabasePermissionProvider) GetUserPermissions(userID string) ([]remilia.Permission, error) {
    var perms []remilia.Permission
    rows, err := p.db.Query("SELECT resource, action FROM user_permissions WHERE user_id = ?", userID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    
    for rows.Next() {
        var perm remilia.Permission
        rows.Scan(&perm.Resource, &perm.Action)
        perms = append(perms, perm)
    }
    return perms, nil
}

// 使用
pm := remilia.NewPermissionManager()
pm.SetPermissionProvider(&DatabasePermissionProvider{db: db})
```

## 💡 最佳实践

### 1. 资源命名规范

```go
// ✅ 推荐：使用冒号分隔的层级命名
"command:weather"
"admin:users"
"data:export"
"message:delete"

// ❌ 不推荐：不清晰的命名
"weather"
"admin"
"export"
```

### 2. 使用角色而非直接授权

```go
// ✅ 推荐：通过角色管理权限
pm.RegisterRole(moderator)
pm.AssignRole("user123", "moderator")

// ⚠️ 谨慎：直接授权（仅特殊情况）
pm.GrantPermission("user123", perm)
```

### 3. 最小权限原则

```go
// ✅ 推荐：只授予必要的权限
role := remilia.NewRole("viewer",
    remilia.Permission{Resource: "query:*", Action: "view"},
)

// ❌ 不推荐：过度授权
role := remilia.NewRole("viewer",
    remilia.Permission{Resource: "*", Action: "*"},
)
```

### 4. 使用通配符简化管理

```go
// ✅ 推荐：使用前缀通配符
remilia.Permission{Resource: "command:*", Action: "execute"}

// 而不是单独授予每个命令
remilia.Permission{Resource: "command:weather", Action: "execute"}
remilia.Permission{Resource: "command:time", Action: "execute"}
// ...
```

### 5. 错误处理

```go
// ✅ 推荐：通过中间件统一处理权限错误
engine.Use(
    remilia.RequirePermissionMiddleware(pm), // 注入权限管理器
    middleware.ErrorHandler(func(ctx *remilia.Context, err error) {
        // 根据错误内容返回友好的提示
        if strings.Contains(err.Error(), "permission denied") {
            _, _ = ctx.ReplyGroup(&dto.Message{
                Content: "❌ 权限不足",
                Type:    dto.TextMessage,
            })
            return
        }

        // 其他错误按常规方式处理
        logrus.WithError(err).Error("handler failed")
    }),
)

// 说明：旧版文档中通过 engine.AddErrorHandler 捕获权限错误的写法在 v1.2.0 中已被统一替换为 ErrorHandler 中间件方案，这里仅保留中间件写法作为推荐实践。
```

## 📊 权限匹配规则

| 模式 | 说明 | 示例 | 匹配 |
|------|------|------|------|
| `*:*` | 全通配符 | 匹配任何资源和操作 | 所有 |
| `command:*` | 前缀通配符 | 匹配以 command: 开头 | `command:weather` ✅ |
| `admin:users:*` | 操作通配符 | 匹配特定资源的所有操作 | `admin:users:view` ✅ |
| `query:info:view` | 精确匹配 | 只匹配完全相同的 | `query:info:view` ✅ |

## ⚠️ 注意事项

### 1. 用户 ID 获取

权限检查依赖用户 ID，确保正确设置：

```go
// 方式 1: 手动设置
ctx.SetState("user_id", userID)

// 方式 2: 从事件自动获取（需要 Author 信息）
// Context 会自动从 event.Author.UserOpenID 获取
```

### 2. 权限管理器注入

必须通过中间件注入权限管理器：

```go
// ✅ 正确
engine.Use(remilia.RequirePermissionMiddleware(pm))

// ❌ 错误：忘记注入
// ctx.HasPermission() 将返回 false
```

### 3. 并发安全

PermissionManager 是线程安全的，可以安全地并发使用：

```go
// ✅ 安全
go func() {
    pm.AssignRole("user1", "admin")
}()
go func() {
    pm.HasPermission("user1", perm)
}()
```

### 4. 性能考虑

- 权限检查开销很小（~100-200ns）
- 建议缓存频繁使用的角色和权限
- 对于大量用户，考虑使用 PermissionProvider 从数据库加载

## 🔗 API 参考

### PermissionManager

| 方法 | 说明 |
|------|------|
| `NewPermissionManager()` | 创建权限管理器 |
| `RegisterRole(role)` | 注册角色 |
| `AssignRole(userID, roleName)` | 分配角色 |
| `RevokeRole(userID, roleName)` | 撤销角色 |
| `GrantPermission(userID, perm)` | 授予权限 |
| `RevokePermission(userID, perm)` | 撤销权限 |
| `HasPermission(userID, perm)` | 检查权限 |
| `GetUserRoles(userID)` | 获取用户角色 |
| `GetUserPermissions(userID)` | 获取用户权限 |

### Context 方法

| 方法 | 说明 |
|------|------|
| `HasPermission(resource, action)` | 检查权限 |
| `RequirePermission(resource, action)` | 要求权限 |

### 中间件

| 中间件 | 说明 |
|--------|------|
| `RequirePermissionMiddleware(pm)` | 注入权限管理器 |
| `RequirePermission(resource, action)` | 要求特定权限 |
| `RequireRole(roleName)` | 要求特定角色 |
| `RequireAnyPermission(perms...)` | 要求任意权限 |
| `RequireAllPermissions(perms...)` | 要求所有权限 |

## 🆕 v0.9.0 新增

- ✅ 完整的 RBAC 权限系统
- ✅ 角色和权限管理
- ✅ 通配符支持
- ✅ 权限提供者接口
- ✅ 丰富的中间件
- ✅ 并发安全

---

**版本**: v0.9.0  
**更新日期**: 2025-11-29
