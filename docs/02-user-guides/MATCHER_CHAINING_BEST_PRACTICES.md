# Matcher 链式调用最佳实践

**版本**: v1.0  
**日期**: 2026-01-25

---

## 📋 概述

Remilia 的 `Matcher` 支持完整的链式调用，所有配置方法都返回 `*Matcher`。
本文档提供链式调用的最佳实践指南。

---

## 🎯 核心原则

### 原则 1: Handle 应该最后调用

```go
// ✅ 推荐：配置在前，Handle 在后
eng.OnCommand("/ping").
    SetDescription("测试连接").
    SetPriority(100).
    Handle(handler)  // ← 最后（终结点，无返回值）

// ❌ 无法编译：Handle 是链式调用的终结点（返回 void），
// 从编译期杜绝 .Handle(h1).SetDescription(...) 这类误用。
// 如需在 Handle 之后继续操作，请提前保存 *Matcher：
m := eng.OnCommand("/ping").SetDescription("描述")
m.Handle(handler)
m.SetTemp(true) // 仍可操作 m
```

**原因**:
- ✅ 逻辑清晰：先配置，后设置行为
- ✅ 易于阅读：符合自然的思维顺序
- ✅ 易于维护：配置集中在一起

> **元数据即时生效（2026-07 起）**：`SetDescription`/`SetUsage`/`SetAliases`/
> `SetHidden` 等元数据 setter 无论在注册前后调用，都会即时刷新命令缓存
> （`GetAllCommands`//help 立即可见）。此前存在"注册瞬间生成缓存、事后修改
> 不刷新"的缺陷，链式写法的描述会在 /help 中显示为空。推荐顺序保持不变——
> 配置前置仍是最清晰的写法。

### 原则 2: 相关配置应该分组

```go
// ✅ 推荐：相关配置分组
eng.OnCommand("/admin").
    // 基本信息
    SetDescription("管理命令").
    SetCategory("管理").
    SetUsage("/admin <action>").
    // 行为配置
    SetPriority(100).
    SetBlock(true).
    // 中间件
    Use(middleware.RequireAdmin()).
    Use(middleware.Logging()).
    // 处理器（最后）
    Handle(adminHandler)

// ⚠️ 可以工作，但不够清晰
eng.OnCommand("/admin").
    SetDescription("管理命令").
    SetPriority(100).
    SetCategory("管理").
    Use(middleware.RequireAdmin()).
    SetUsage("/admin <action>").
    SetBlock(true).
    Use(middleware.Logging()).
    Handle(adminHandler)
```

### 原则 3: 复杂配置使用分步方式

```go
// ✅ 推荐：复杂配置分步进行
m := eng.OnCommand("/complex")

// 基本信息
m.SetDescription("复杂命令")
m.SetCategory("高级")
m.SetUsage("/complex [options]")

// 添加多个中间件
m.Use(middleware.RequireAuth())
m.Use(middleware.RateLimit(10))
m.Use(middleware.Logging())

// 动态配置
if config.IsProduction() {
    m.SetPriority(100)
} else {
    m.SetPriority(50)
}

// 最后设置 Handler
m.Handle(complexHandler)
```

---

## 📖 完整示例

### 示例 1: 简单命令

```go
// ✅ 最佳实践：简单命令使用完整链式
func registerPingCommand(eng *engine.Engine) {
    eng.OnCommand(platform.EventKindGroupMessage, "/ping").
        SetDescription("测试机器人连接").
        SetCategory("系统").
        SetUsage("/ping").
        Handle(func(ctx *context.Context) error {
            return ctx.Reply(platform.TextMessage("Pong! 🏓"))
        })
}
```

### 示例 2: 带参数的命令

```go
// ✅ 最佳实践：使用 Definition 定义参数
func registerSearchCommand(eng *engine.Engine) {
    def := &command.Definition{
        Name:        "search",
        Aliases:     []string{"find", "query"},
        Description: "搜索内容",
        Usage:       "/search <keyword> [--engine google]",
        Category:    "工具",
        Examples: []string{
            "/search Go语言",
            "/search Python --engine bing",
        },
        Arguments: []*command.Argument{
            {
                Name:        "keyword",
                Description: "搜索关键词",
                Required:    true,
                Type:        command.ArgTypeString,
            },
        },
        Flags: []*command.Flag{
            {
                Name:        "engine",
                ShortName:   "e",
                Description: "搜索引擎",
                Default:     "google",
            },
        },
    }

    eng.RegisterCommandDef(dto.GroupAtMessageCreate, def).
        SetPriority(50).
        Use(middleware.RateLimit(10)).
        Handle(func(ctx *context.Context) error {
            parsed := ctx.GetParsedCommand()
            keyword := parsed.GetString("keyword")
            engine := parsed.GetString("engine")
            // 执行搜索...
            return nil
        })
}
```

### 示例 3: 需要权限的命令

```go
// ✅ 最佳实践：分步配置复杂命令
func registerAdminCommand(eng *engine.Engine) {
    m := eng.OnCommand(dto.GroupAtMessageCreate, "/admin")

    // 基本信息
    m.SetDescription("管理命令")
    m.SetCategory("管理")
    m.SetUsage("/admin <action>")
    m.SetPermissions("admin")

    // 行为配置
    m.SetPriority(100)
    m.SetBlock(true)

    // 中间件链
    m.Use(middleware.RequireAdmin())
    m.Use(middleware.Logging())
    m.Use(middleware.Metrics())

    // 处理器（最后）
    m.Handle(func(ctx *context.Context) error {
        // 管理逻辑...
        return ctx.Reply("Admin command executed")
    })
}
```

### 示例 4: 动态配置

```go
// ✅ 最佳实践：动态配置时保存引用
func registerDynamicCommand(eng *engine.Engine, cfg *Config) {
    m := eng.OnCommand(dto.GroupAtMessageCreate, "/dynamic")

    // 基本配置
    m.SetDescription("动态命令")
    m.SetCategory(cfg.Category)

    // 根据配置动态设置
    if cfg.IsHighPriority {
        m.SetPriority(100)
    } else {
        m.SetPriority(50)
    }

    if cfg.RequireAuth {
        m.Use(middleware.RequireAuth())
    }

    if cfg.EnableRateLimit {
        m.Use(middleware.RateLimit(cfg.RateLimit))
    }

    // Handler 最后设置
    m.Handle(cfg.Handler)
}
```

---

## ⚠️ 常见错误

### 错误 1: Handle 在中间

```go
// ❌ 错误：Handle 不应该在配置中间
eng.OnCommand("/bad").
    SetDescription("描述").
    Handle(handler).          // ← 不应该在这里
    SetPriority(100).         // ← 配置应该在 Handle 之前
    Use(middleware.Logging())  // ← 中间件也应该在 Handle 之前
```

**问题**:
- 代码逻辑不清晰
- 难以理解配置顺序
- 中间件在 Handler 之后添加，容易混淆

**修复**:
```go
// ✅ 正确：所有配置在前，Handle 在后
eng.OnCommand("/good").
    SetDescription("描述").
    SetPriority(100).
    Use(middleware.Logging()).
    Handle(handler)
```

### 错误 2: 不保存引用

```go
// ❌ 错误：复杂配置不保存引用
eng.OnCommand("/complex").
    SetDescription("描述1").
    SetCategory("分类1").
    SetUsage("用法1").
    SetPriority(100).
    SetBlock(true).
    Use(middleware.A()).
    Use(middleware.B()).
    Use(middleware.C()).
    SetPermissions("perm1", "perm2").
    Handle(complexHandler)
// 太长，难以阅读
```

**问题**:
- 链式调用过长
- 难以阅读和维护
- 不便于动态配置

**修复**:
```go
// ✅ 正确：保存引用，分步配置
m := eng.OnCommand("/complex")

// 分组配置
m.SetDescription("描述1")
m.SetCategory("分类1")
m.SetUsage("用法1")

m.SetPriority(100)
m.SetBlock(true)

m.Use(middleware.A())
m.Use(middleware.B())
m.Use(middleware.C())

m.SetPermissions("perm1", "perm2")

m.Handle(complexHandler)
```

### 错误 3: 多次调用 Handle

```go
// ⚠️ 注意：多次调用 Handle 会覆盖前一个
m := eng.OnCommand("/test")

m.Handle(func(ctx *context.Context) error {
    return ctx.Reply("Handler 1")
})

// 这会覆盖上面的 Handler
m.Handle(func(ctx *context.Context) error {
    return ctx.Reply("Handler 2")  // ← 只有这个会执行
})
```

**说明**:
- 多次调用 Handle 会覆盖，不会累加
- 最后一次调用的 Handler 生效

---

## 📐 链式调用顺序建议

### 推荐顺序

```go
matcher.
    // 1. 基本信息
    SetDescription("...").
    SetCategory("...").
    SetUsage("...").
    SetAliases("...").
    SetExamples("...").
    
    // 2. 行为配置
    SetPriority(...).
    SetBlock(...).
    SetPermissions("...").
    
    // 3. 中间件（按执行顺序）
    Use(middleware1).
    Use(middleware2).
    Use(middleware3).
    
    // 4. 处理器（最后）
    Handle(handler)
```

### 分组建议

```go
m := eng.OnCommand("/cmd")

// Group 1: 元数据（用于 Help）
m.SetDescription("描述")
m.SetCategory("分类")
m.SetUsage("用法")
m.SetExamples("示例1", "示例2")

// Group 2: 行为配置
m.SetPriority(100)
m.SetBlock(false)

// Group 3: 权限和中间件
m.SetPermissions("admin")
m.Use(middleware.RequireAuth())
m.Use(middleware.Logging())

// Group 4: 处理器（最后）
m.Handle(handler)
```

---

## 🎨 代码风格建议

### 风格 1: 链式（简单命令）

```go
// 适用于：配置简单的命令（≤5 个配置）
eng.OnCommand("/ping").
    SetDescription("测试连接").
    Handle(pingHandler)
```

### 风格 2: 分步（复杂命令）

```go
// 适用于：配置复杂的命令（>5 个配置）
m := eng.OnCommand("/admin")
m.SetDescription("管理命令")
m.SetCategory("管理")
m.SetPriority(100)
m.Use(middleware.RequireAdmin())
m.Use(middleware.Logging())
m.Handle(adminHandler)
```

### 风格 3: 混合（中等复杂度）

```go
// 适用于：部分链式 + 部分分步
m := eng.OnCommand("/search").
    SetDescription("搜索命令").
    SetCategory("工具")

// 动态配置
if needAuth {
    m.Use(middleware.RequireAuth())
}

m.Handle(searchHandler)
```

---

## 🔍 代码审查检查清单

在代码审查时，检查以下几点：

- [ ] Handle 是否在链式调用的最后？
- [ ] 相关配置是否分组？
- [ ] 复杂配置是否使用分步方式？
- [ ] 是否有 Handle 之后继续配置的情况？
- [ ] 中间件是否在 Handle 之前添加？
- [ ] 配置顺序是否清晰易读？

---

## 📚 参考

### 相关文档

- [Matcher API 文档](./MATCHER_API.md)
- [中间件使用指南](./MIDDLEWARE_GUIDE.md)
- [命令系统文档](./COMMAND_SYSTEM.md)

### 设计分析

- [Handle 方法设计分析](./HANDLE_METHOD_DESIGN_ANALYSIS.md)

---

## 🎯 总结

### 核心要点

1. **Handle 应该最后调用** - 使代码逻辑清晰
2. **相关配置应该分组** - 提高可读性
3. **复杂配置使用分步** - 便于维护

### 快速参考

```go
// ✅ 推荐模式
matcher.
    SetDescription("...").  // 1. 基本信息
    SetCategory("...").
    SetPriority(...).       // 2. 行为配置
    Use(middleware).        // 3. 中间件
    Handle(handler)         // 4. 处理器（最后）

// ❌ 避免模式
matcher.
    Handle(handler).        // ← 不应该在这里
    SetDescription("...")   // ← 配置应该在前面
```

---

**版本**: v1.0  
**维护者**: Remilia Team  
**更新日期**: 2026-01-25
