# Context 标准库集成使用指南

> 版本: v1.3.0  
> 更新日期: 2025-12-03  
> 状态: ✅ 已实施

---

## 📋 概述

从 v1.3.0 开始，`remilia.Context` 集成了标准库 `context.Context`，支持：

- ✅ 超时控制（`context.WithTimeout`）
- ✅ 主动取消（`context.WithCancel`）
- ✅ 截止时间（`context.WithDeadline`）
- ✅ 值传播（`context.WithValue`）
- ✅ 与标准库和第三方库无缝集成

**设计原则**：
- 只提供 `Context()` 访问方法
- 不封装标准库方法（直接使用标准库 API）
- 向后完全兼容

---

## 🚀 快速开始

### 基本使用

```go
package main

import (
    "context"
    "time"
    
    "github.com/KomeiDiSanXian/remilia"
    "github.com/KomeiDiSanXian/remilia/openapi/dto"
)

func main() {
    engine := remilia.GetGlobalEngine()
    
    // 数据库查询超时示例
    engine.OnC2C(remilia.OnCommand("/users")).HandleE(func(ctx *remilia.Context) error {
        // 获取标准库 context
        stdCtx := ctx.Context()
        
        // 创建带超时的 context
        dbCtx, cancel := context.WithTimeout(stdCtx, 5*time.Second)
        defer cancel()
        
        // 传递给数据库查询
        users, err := db.QueryContext(dbCtx, "SELECT * FROM users LIMIT 100")
        if err == context.DeadlineExceeded {
            ctx.ReplyGroup(&dto.Message{Content: "查询超时，请稍后重试"})
            return nil
        }
        if err != nil {
            return err
        }
        
        ctx.ReplyGroup(&dto.Message{Content: fmt.Sprintf("找到 %d 个用户", len(users))})
        return nil
    })
}
```

---

## 📚 使用场景

### 1. 数据库查询超时

```go
engine.OnC2C(remilia.OnCommand("/query")).HandleE(func(ctx *remilia.Context) error {
    // 创建 5 秒超时的 context
    dbCtx, cancel := context.WithTimeout(ctx.Context(), 5*time.Second)
    defer cancel()
    
    // 查询数据库
    var users []User
    err := db.SelectContext(dbCtx, &users, "SELECT * FROM users WHERE active = ?", true)
    if err != nil {
        if err == context.DeadlineExceeded {
            return fmt.Errorf("数据库查询超时")
        }
        return err
    }
    
    // 处理结果...
    return nil
})
```

---

### 2. HTTP 请求超时

```go
engine.OnC2C(remilia.OnCommand("/weather")).HandleE(func(ctx *remilia.Context) error {
    city := ctx.GetString("city")
    
    // 创建 3 秒超时的 HTTP 请求
    httpCtx, cancel := context.WithTimeout(ctx.Context(), 3*time.Second)
    defer cancel()
    
    url := fmt.Sprintf("https://api.weather.com/v1/weather?city=%s", city)
    req, err := http.NewRequestWithContext(httpCtx, "GET", url, nil)
    if err != nil {
        return err
    }
    
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        if err == context.DeadlineExceeded {
            ctx.ReplyGroup(&dto.Message{Content: "天气查询超时"})
            return nil
        }
        return err
    }
    defer resp.Body.Close()
    
    // 处理响应...
    return nil
})
```

---

### 3. 多步骤处理 + 取消检测

```go
engine.OnC2C(remilia.OnCommand("/process")).HandleE(func(ctx *remilia.Context) error {
    stdCtx := ctx.Context()
    
    // Step 1
    if err := processStep1(stdCtx); err != nil {
        return err
    }
    
    // 检查是否被取消
    select {
    case <-stdCtx.Done():
        return stdCtx.Err()
    default:
    }
    
    // Step 2
    if err := processStep2(stdCtx); err != nil {
        return err
    }
    
    // 检查取消
    select {
    case <-stdCtx.Done():
        return stdCtx.Err()
    default:
    }
    
    // Step 3
    return processStep3(stdCtx)
})

func processStep1(ctx context.Context) error {
    // 支持取消的长时间操作
    select {
    case <-ctx.Done():
        return ctx.Err()
    case <-time.After(2 * time.Second):
        // 处理逻辑...
        return nil
    }
}
```

---

### 4. 异步场景

#### 场景 A: 只使用 stdCtx（推荐）✅

```go
engine.OnC2C(remilia.OnCommand("/async")).HandleE(func(ctx *remilia.Context) error {
    stdCtx := ctx.Context()  // 复制引用
    
    go func() {
        // 只使用 stdCtx，不访问 ctx 的其他字段
        time.Sleep(5 * time.Second)
        
        dbCtx, cancel := context.WithTimeout(stdCtx, 3*time.Second)
        defer cancel()
        
        result, err := db.QueryContext(dbCtx, "SELECT ...")
        if err != nil {
            logrus.Error(err)
        }
    }()
    
    return nil
    // ctx.Release() 被自动调用，但 stdCtx 仍然有效（GC 管理）
})
```

**要点**：
- ✅ 不需要 `Retain/Release`
- ✅ `stdCtx` 生命周期独立，由 GC 管理
- ⚠️ 不能访问 `ctx.State`, `ctx.ReplyGroup` 等

---

#### 场景 B: 需要访问 ctx 字段⚠️

```go
engine.OnC2C(remilia.OnCommand("/async")).HandleE(func(ctx *remilia.Context) error {
    // 使用 WithRetainAsync 自动管理引用计数
    ctx.WithRetainAsync(func(ctx *remilia.Context) {
        stdCtx := ctx.Context()
        
        // 可以访问所有字段
        userID := ctx.GetString("user_id")
        
        time.Sleep(5 * time.Second)
        
        dbCtx, cancel := context.WithTimeout(stdCtx, 3*time.Second)
        defer cancel()
        
        result, err := db.QueryContext(dbCtx, "SELECT * FROM users WHERE id = ?", userID)
        if err != nil {
            logrus.Error(err)
            return
        }
        
        // 可以使用 Context 方法
        ctx.ReplyGroup(&dto.Message{Content: "处理完成"})
    })
    
    return nil
})
```

**规则**：
- 只用 `stdCtx` → 不需要 `Retain`
- 访问 `ctx.State`, `ctx.ReplyGroup` 等 → 必须 `Retain` 或使用 `WithRetainAsync`

---

### 5. 分布式追踪

```go
// 使用 OpenTelemetry
import (
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/trace"
)

// 中间件注入 trace context
func Tracing() remilia.HandlerMiddleware {
    tracer := otel.Tracer("remilia")
    
    return func(next remilia.HandlerE) remilia.HandlerE {
        return func(ctx *remilia.Context) error {
            // 从当前 context 创建 span
            stdCtx, span := tracer.Start(ctx.Context(), "handler",
                trace.WithAttributes(
                    attribute.String("event_type", string(ctx.GetEventType())),
                ),
            )
            defer span.End()
            
            // 替换为带 trace 的 context
            ctx.ctx = stdCtx
            
            return next(ctx)
        }
    }
}

// 使用
engine.Use(Tracing())

engine.OnC2C(remilia.OnCommand("/order")).HandleE(func(ctx *remilia.Context) error {
    // ctx.Context() 自动包含 trace 信息
    span := trace.SpanFromContext(ctx.Context())
    span.SetAttributes(attribute.String("user_id", ctx.GetString("user_id")))
    
    // trace 自动传播到下游服务
    order, err := orderService.CreateOrder(ctx.Context(), orderData)
    if err != nil {
        span.RecordError(err)
        return err
    }
    
    return nil
})
```

---

### 6. 使用中间件全局控制超时

```go
// 方式 1: 使用框架提供的 Timeout 中间件
import "github.com/KomeiDiSanXian/remilia/middleware"

engine.Use(middleware.Timeout(5 * time.Second))

// 方式 2: 自定义超时中间件
func TimeoutMiddleware(timeout time.Duration) remilia.HandlerMiddleware {
    return func(next remilia.HandlerE) remilia.HandlerE {
        return func(ctx *remilia.Context) error {
            // 创建带超时的 context
            timeoutCtx, cancel := context.WithTimeout(ctx.Context(), timeout)
            defer cancel()
            
            // 替换 context
            ctx.ctx = timeoutCtx
            
            // 在 goroutine 中执行 handler
            done := make(chan error, 1)
            go func() {
                done <- next(ctx)
            }()
            
            // 等待完成或超时
            select {
            case err := <-done:
                return err
            case <-timeoutCtx.Done():
                return fmt.Errorf("handler timeout after %v", timeout)
            }
        }
    }
}

engine.Use(TimeoutMiddleware(5 * time.Second))
```

---

## ⚠️ 注意事项

### 1. 不要封装标准库方法

```go
// ❌ 错误：不要这样做
type Context struct {
    // ...
}

func (c *Context) WithTimeout(timeout time.Duration) {
    c.ctx, c.cancel = context.WithTimeout(c.ctx, timeout)
}

// ✅ 正确：直接使用标准库
stdCtx, cancel := context.WithTimeout(ctx.Context(), 5*time.Second)
defer cancel()
```

**原因**：
- 违反单一职责原则
- 维护成本高
- 灵活性差
- 不符合 Go 生态惯例

---

### 2. 记得 defer cancel()

```go
// ✅ 正确
dbCtx, cancel := context.WithTimeout(ctx.Context(), 5*time.Second)
defer cancel()  // 确保资源释放
result, err := db.QueryContext(dbCtx, "SELECT ...")

// ❌ 错误：忘记 cancel
dbCtx, _ := context.WithTimeout(ctx.Context(), 5*time.Second)
result, err := db.QueryContext(dbCtx, "SELECT ...")  // 可能导致资源泄漏
```

---

### 3. 异步场景的引用计数

```go
// 场景判断：
// - 只用 stdCtx → 不需要 Retain
// - 访问 ctx.State 等 → 必须 Retain

// ✅ 只用 stdCtx - 不需要 Retain
stdCtx := ctx.Context()
go func() {
    db.QueryContext(stdCtx, "SELECT ...")  // 安全
}()

// ✅ 访问 ctx 字段 - 使用 WithRetainAsync
ctx.WithRetainAsync(func(ctx *remilia.Context) {
    stdCtx := ctx.Context()
    userID := ctx.GetString("user_id")  // 访问 State
    db.QueryContext(stdCtx, "SELECT * FROM users WHERE id = ?", userID)
    ctx.ReplyGroup(&dto.Message{Content: "Done"})  // 访问 API
})
```

---

## 📊 性能对比

### 基准测试结果

```
BenchmarkContextWithPool-16              17869002        66.38 ns/op       0 B/op    0 allocs/op
BenchmarkContext_WithStdContext-16       57456679        20.19 ns/op       0 B/op    0 allocs/op
BenchmarkContext_ContextAccess-16        1000000000       0.50 ns/op       0 B/op    0 allocs/op
BenchmarkContext_WithTimeout-16           3959877       290.0 ns/op      272 B/op    4 allocs/op
```

**结论**：
- ✅ 对象池性能不变（仍然是 0 allocs/op）
- ✅ 访问 `Context()` 几乎无开销（0.5 ns/op）
- ⚠️ 创建 `WithTimeout` 有开销（标准库行为，不可避免）

---

## 🎓 最佳实践

### 1. 优先使用同步方式

```go
// ✅ 推荐：同步使用，简单清晰
engine.OnC2C(remilia.OnCommand("/query")).HandleE(func(ctx *remilia.Context) error {
    dbCtx, cancel := context.WithTimeout(ctx.Context(), 5*time.Second)
    defer cancel()
    
    result, err := db.QueryContext(dbCtx, "SELECT ...")
    return err
})
```

---

### 2. 异步场景优先只用 stdCtx

```go
// ✅ 推荐：只使用 stdCtx，不需要 Retain
engine.OnC2C(remilia.OnCommand("/async")).HandleE(func(ctx *remilia.Context) error {
    stdCtx := ctx.Context()
    
    go func() {
        db.QueryContext(stdCtx, "SELECT ...")
    }()
    
    return nil
})
```

---

### 3. 合理设置超时时间

```go
// 根据操作类型设置不同超时
var (
    dbTimeout   = 5 * time.Second   // 数据库查询
    apiTimeout  = 3 * time.Second   // 第三方 API
    fileTimeout = 10 * time.Second  // 文件操作
)

engine.OnC2C(remilia.OnCommand("/data")).HandleE(func(ctx *remilia.Context) error {
    // 数据库查询
    dbCtx, cancel := context.WithTimeout(ctx.Context(), dbTimeout)
    defer cancel()
    
    // API 调用
    apiCtx, cancel := context.WithTimeout(ctx.Context(), apiTimeout)
    defer cancel()
    
    // ...
})
```

---

### 4. 使用中间件统一管理

```go
// 全局超时中间件
engine.Use(middleware.Timeout(5 * time.Second))

// 特定插件的超时
plugin.Use(engine, middleware.Timeout(10 * time.Second))

// 特定 matcher 的超时
engine.OnC2C(remilia.OnCommand("/slow")).
    Use(middleware.Timeout(30 * time.Second)).
    HandleE(slowHandler)
```

---

## 🛡️ 过度释放检测（v1.8.1 新增）

从 v1.8.1 开始，`Context.Release()` 增加了过度释放检测机制，防止 Context 被多次放回对象池。

### 检测机制

1. **释放标志检测**：防止 Context 被重复放回池
2. **引用计数检测**：防止 `Release()` 调用次数超过 `Retain()` 次数
3. **开发模式支持**：通过 `REMILIA_DEV_MODE` 环境变量启用 panic

### 使用指南

```bash
# 开发模式（过度释放会 panic，帮助快速定位问题）
export REMILIA_DEV_MODE=true

# 生产模式（过度释放只记录日志）
unset REMILIA_DEV_MODE
```

### 错误示例与修复

```go
// ❌ 错误：重复释放
ctx.Release()
ctx.Release() // 第二次会被拦截，记录日志

// ✅ 正确：使用 defer 确保只释放一次
defer ctx.Release()

// ❌ 错误：Release 多于 Retain
ctx := NewContext(event, api) // refs=1
ctx.Release()                 // refs=0
ctx.Release()                 // refs=-1，被拦截

// ✅ 正确：Retain/Release 成对使用
ctx.Retain()
go func() {
    defer ctx.Release()
    // 使用 ctx
}()
```

详见：[Context 过度释放检测增强](CONTEXT_RELEASE_PROTECTION.md)

---

## 🔗 相关文档

- [标准库 context 包文档](https://pkg.go.dev/context)
- [Context 过度释放检测增强](CONTEXT_RELEASE_PROTECTION.md) - v1.8.1 新增 ⭐
- [CONTEXT_INTEGRATION_SUMMARY.md](./CONTEXT_INTEGRATION_SUMMARY.md) - 集成方案总结
- [CONTEXT_NAMING_ANALYSIS.md](./CONTEXT_NAMING_ANALYSIS.md) - 详细分析
- [CONTEXT_POOL_IMPACT_ANALYSIS.md](./CONTEXT_POOL_IMPACT_ANALYSIS.md) - 性能影响分析

---

## ❓ 常见问题

### Q: 为什么不提供 WithTimeout() 方法？

**A**: 为了保持设计简洁和符合 Go 惯例。所有主流框架（gin, echo, fiber）都不封装标准库方法，而是让用户直接使用标准库 API。

---

### Q: Context() 返回 nil 怎么办？

**A**: 不会返回 nil。`Context()` 方法会自动返回 `context.Background()` 如果内部 context 为 nil。

---

### Q: 需要手动 cancel 吗？

**A**: 
- 你创建的 `WithTimeout`, `WithCancel` 等 → **必须 `defer cancel()`**
- 框架的 `ctx.Context()` → **不需要 cancel**（是 Background context）

---

### Q: 异步场景如何判断是否需要 Retain？

**A**: 
- 只用 `stdCtx` → **不需要 Retain**
- 访问 `ctx.State`, `ctx.ReplyGroup` 等 → **必须 Retain**

---

## 📝 更新日志

### v1.3.0 (2025-12-03)

- ✅ 添加 `Context()` 方法返回标准库 context
- ✅ 添加 `NewContextWithContext()` 构造函数
- ✅ 完善测试覆盖（15+ 测试用例）
- ✅ 向后完全兼容

---

**文档版本**: v1.0  
**最后更新**: 2025-12-03  
**维护者**: Remilia 开发团队

