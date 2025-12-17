# 优雅关闭与 Context 管理改进

> 文档版本: v1.3.0  
> 更新日期: 2025-12-07

本文档介绍 Remilia v1.3.0 中引入的优雅关闭机制改进和 Context 管理增强功能。

---

## 📋 目录

1. [优雅关闭改进](#优雅关闭改进)
2. [Context Cancellation 传播](#context-cancellation-传播)
3. [Context Clone 方法](#context-clone-方法)
4. [引用计数运行时检测](#引用计数运行时检测)
5. [最佳实践](#最佳实践)
6. [迁移指南](#迁移指南)

---

## 优雅关闭改进

### 问题背景

在 v1.2.x 版本中，Bot 的优雅关闭存在以下问题：

1. **无法主动取消 handler**：关闭时只能被动等待 handler 完成，无法主动中断长时间运行的操作
2. **超时处理不完善**：没有合理的超时机制，可能导致关闭过程卡死
3. **事件通道排空不完整**：可能导致 goroutine 阻塞

### 新的关闭流程

v1.3.0 引入了完善的五步关闭流程：

```go
// Shutdown 优雅关闭 Bot
func (b *Bot) Shutdown(ctx context.Context) {
    // 1. 主动取消所有正在执行的 handler（通过 context）
    // 2. 停止事件循环（不再接收新事件）
    // 3. 关闭 HTTP 服务器（停止接收新连接）
    // 4. 排空事件通道（防止 goroutine 阻塞）
    // 5. 等待所有 handler 完成（带超时）
}
```

### 使用示例

#### 基本用法

```go
bot := remilia.New(info)
bot.Start()

// ... bot 运行 ...

// 优雅关闭（5 秒超时）
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
bot.Shutdown(ctx)
```

#### 在信号处理中使用

```go
func main() {
    bot := remilia.New(info)
    bot.Start()

    // 监听系统信号
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
    
    // 等待信号
    <-sigChan
    log.Println("Received shutdown signal")

    // 优雅关闭（10 秒超时）
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    
    bot.Shutdown(ctx)
    log.Println("Shutdown complete")
}
```

#### 处理超时

```go
ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
defer cancel()

bot.Shutdown(ctx)

// 检查是否超时
if ctx.Err() == context.DeadlineExceeded {
    log.Warn("Shutdown timeout, some handlers may still be running")
} else {
    log.Info("Shutdown completed successfully")
}
```

---

## Context Cancellation 传播

### 问题背景

在之前的版本中，handler 无法感知 Bot 正在关闭，导致：

- 长时间运行的 handler 会阻塞优雅关闭
- 无法主动中断 handler 中的阻塞操作（如数据库查询、API 调用）

### 解决方案

v1.3.0 引入了 Bot 级别的 context，在优雅关闭时会主动取消所有正在执行的 handler。

### Handler 中响应取消

#### 基本模式

```go
bot.engine.OnC2C().Handle(func(ctx *remilia.Context) error {
    // 使用 ctx.Context() 获取标准库 context
    select {
    case <-time.After(5 * time.Second):
        // 正常完成
        return ctx.SendText("Done")
    case <-ctx.Context().Done():
        // Bot 正在关闭，应该尽快返回
        log.Info("Handler cancelled due to shutdown")
        return ctx.Context().Err()
    }
})
```

#### 在数据库操作中使用

```go
bot.engine.OnC2C().Handle(func(ctx *remilia.Context) error {
    // 数据库查询会自动响应 context 取消
    query := "SELECT * FROM users WHERE id = ?"
    rows, err := db.QueryContext(ctx.Context(), query, userID)
    if err != nil {
        if err == context.Canceled {
            log.Info("Database query cancelled")
        }
        return err
    }
    defer rows.Close()
    
    // 处理结果...
    return nil
})
```

#### 在 HTTP 请求中使用

```go
bot.engine.OnC2C().Handle(func(ctx *remilia.Context) error {
    // HTTP 请求会自动响应 context 取消
    req, err := http.NewRequestWithContext(
        ctx.Context(), 
        "GET", 
        "https://api.example.com/data", 
        nil,
    )
    if err != nil {
        return err
    }
    
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        if err == context.Canceled {
            log.Info("HTTP request cancelled")
        }
        return err
    }
    defer resp.Body.Close()
    
    // 处理响应...
    return nil
})
```

#### 在长时间运行的任务中使用

```go
bot.engine.OnC2C().Handle(func(ctx *remilia.Context) error {
    // 定期检查 context 是否被取消
    for i := 0; i < 100; i++ {
        select {
        case <-ctx.Context().Done():
            log.Infof("Task cancelled at step %d/100", i)
            return ctx.Context().Err()
        default:
            // 执行任务步骤
            doWork(i)
        }
    }
    return nil
})
```

---

## Context Clone 方法

### 问题背景

在 v1.2.x 版本中，异步使用 Context 需要手动管理引用计数：

```go
// 旧方式：需要手动 Retain/Release
ctx.Retain()
go func() {
    defer ctx.Release()  // 容易忘记或在 panic 时遗漏
    // 使用 ctx
}()
```

引用计数容易出错：
- 忘记 Release 导致内存泄漏
- 多次 Release 导致 Context 被错误释放
- 嵌套 goroutine 中计数复杂

### 解决方案

v1.3.0 引入 `Clone()` 方法，提供更安全的异步使用方式。

### Clone vs Retain

| 特性 | Clone | Retain |
|------|-------|--------|
| **状态共享** | 独立状态（深拷贝） | 共享状态 |
| **引用计数** | 独立计数 | 共享计数 |
| **安全性** | 更安全，不会影响原 Context | 需要小心管理 |
| **性能** | 略慢（需要复制 State） | 更快（原子操作） |
| **对象池** | 不使用池 | 使用池 |
| **适用场景** | 独立的异步任务 | 需要共享状态的场景 |

### 使用示例

#### 基本用法

```go
bot.engine.OnC2C().Handle(func(ctx *remilia.Context) error {
    // 异步任务：使用 Clone
    go func() {
        asyncCtx := ctx.Clone()
        defer asyncCtx.Release()
        
        // asyncCtx 是独立的，修改不会影响原 Context
        asyncCtx.SetState("task_status", "running")
        
        // 执行耗时任务
        result := doHeavyWork()
        
        asyncCtx.SetState("task_status", "completed")
        log.Info("Async task completed")
    }()
    
    // 立即返回，不阻塞主 handler
    return ctx.SendText("任务已启动")
})
```

#### 多个独立任务

```go
bot.engine.OnC2C().Handle(func(ctx *remilia.Context) error {
    // 启动多个独立的异步任务
    for i := 0; i < 5; i++ {
        go func(taskID int) {
            taskCtx := ctx.Clone()
            defer taskCtx.Release()
            
            // 每个任务有独立的状态
            taskCtx.SetState("task_id", taskID)
            taskCtx.SetState("status", "running")
            
            // 执行任务
            result := processTask(taskID)
            
            taskCtx.SetState("status", "done")
            taskCtx.SetState("result", result)
            
            log.Infof("Task %d completed", taskID)
        }(i)
    }
    
    return ctx.SendText("5个任务已启动")
})
```

#### Clone vs Retain 对比

```go
// 场景1：需要独立状态 - 使用 Clone
go func() {
    asyncCtx := ctx.Clone()
    defer asyncCtx.Release()
    
    // 修改不会影响原 Context
    asyncCtx.SetState("modified", true)
}()

// ctx 的状态不受影响
// ctx.GetBool("modified") == false

// 场景2：需要共享状态 - 使用 Retain
ctx.Retain()
go func() {
    defer ctx.Release()
    
    // 修改会影响原 Context
    ctx.SetState("shared", true)
}()

// ctx 的状态被修改
// ctx.GetBool("shared") == true
```

#### 与 WithRetainAsync 对比

```go
// 旧方式：WithRetainAsync（共享状态）
ctx.WithRetainAsync(func(ctx *remilia.Context) {
    ctx.SetState("value", 1)  // 修改原 Context
})

// 新方式：Clone（独立状态）
go func() {
    asyncCtx := ctx.Clone()
    defer asyncCtx.Release()
    asyncCtx.SetState("value", 1)  // 不影响原 Context
}()
```

---

## 引用计数运行时检测

### 问题背景

引用计数错误是常见的 bug 来源：

```go
ctx.Retain()
go func() {
    // 忘记 Release
    doSomething(ctx)
}()
// 内存泄漏！

ctx.Release()
ctx.Release()  // 过度释放
// Context 被多次放回池中！
```

### 解决方案

v1.3.0 在 `Release()` 中添加了运行时检测：

```go
func (ctx *Context) Release() {
    newRefs := atomic.AddInt32(&ctx.refs, -1)
    
    // 检测过度释放
    if newRefs < 0 {
        logrus.Error("[Context] Over-released: refs < 0, this is a bug!")
        return
    }
    
    // ... 正常清理 ...
}
```

### 错误检测

#### 过度释放检测

```go
ctx := remilia.NewContext(event, api)
ctx.Release()
ctx.Release()  // 第二次释放会被检测到

// 日志输出：
// ERROR [Context] Over-released: refs < 0, this is a bug!
```

#### 在开发环境启用 Panic

如果希望在开发环境中更早发现问题，可以修改代码启用 panic：

```go
// 在 context.go 中修改
if newRefs < 0 {
    if os.Getenv("ENV") == "development" {
        panic(fmt.Sprintf("Context over-released: refs=%d", newRefs))
    }
    logrus.Error("[Context] Over-released: refs < 0, this is a bug!")
    return
}
```

---

## 最佳实践

### Handler 中的 Context 使用

#### ✅ 推荐：使用 defer

```go
bot.engine.OnC2C().Handle(func(ctx *remilia.Context) error {
    ctx.Retain()
    go func() {
        defer ctx.Release()  // ✅ 使用 defer 确保释放
        doAsyncWork(ctx)
    }()
    return nil
})
```

#### ✅ 推荐：使用 Clone 进行独立任务

```go
bot.engine.OnC2C().Handle(func(ctx *remilia.Context) error {
    go func() {
        asyncCtx := ctx.Clone()
        defer asyncCtx.Release()  // ✅ Clone 的 Context 需要单独释放
        doIndependentWork(asyncCtx)
    }()
    return nil
})
```

#### ✅ 推荐：使用 WithRetainAsync

```go
bot.engine.OnC2C().Handle(func(ctx *remilia.Context) error {
    ctx.WithRetainAsync(func(ctx *remilia.Context) {
        // ✅ 自动处理 Retain/Release
        doAsyncWork(ctx)
    })
    return nil
})
```

#### ❌ 避免：忘记 Release

```go
bot.engine.OnC2C().Handle(func(ctx *remilia.Context) error {
    ctx.Retain()
    go func() {
        doAsyncWork(ctx)
        // ❌ 忘记 Release，内存泄漏！
    }()
    return nil
})
```

#### ❌ 避免：多次 Release

```go
bot.engine.OnC2C().Handle(func(ctx *remilia.Context) error {
    ctx.Retain()
    go func() {
        defer ctx.Release()
        doAsyncWork(ctx)
        ctx.Release()  // ❌ 重复 Release！
    }()
    return nil
})
```

### 响应 Context 取消

#### ✅ 推荐：定期检查取消

```go
bot.engine.OnC2C().Handle(func(ctx *remilia.Context) error {
    for i := 0; i < 1000; i++ {
        select {
        case <-ctx.Context().Done():
            return ctx.Context().Err()  // ✅ 及时响应取消
        default:
            doWork(i)
        }
    }
    return nil
})
```

#### ✅ 推荐：在阻塞操作中使用 Context

```go
bot.engine.OnC2C().Handle(func(ctx *remilia.Context) error {
    // ✅ 数据库、HTTP 等操作都支持 context
    rows, err := db.QueryContext(ctx.Context(), query, args...)
    if err != nil {
        return err
    }
    defer rows.Close()
    return nil
})
```

#### ❌ 避免：忽略 Context 取消

```go
bot.engine.OnC2C().Handle(func(ctx *remilia.Context) error {
    // ❌ 长时间睡眠，不响应取消
    time.Sleep(1 * time.Hour)
    return nil
})
```

---

## 迁移指南

### 从 v1.2.x 迁移到 v1.3.0

#### 1. 优雅关闭

**v1.2.x:**
```go
bot.Shutdown(ctx)  // 可能卡死
```

**v1.3.0:**
```go
// 无需修改，但建议添加超时
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
bot.Shutdown(ctx)
```

#### 2. 异步 Handler

**v1.2.x:**
```go
ctx.Retain()
go func() {
    defer ctx.Release()
    doWork(ctx)
}()
```

**v1.3.0（推荐）:**
```go
// 如果需要独立状态，使用 Clone
go func() {
    asyncCtx := ctx.Clone()
    defer asyncCtx.Release()
    doWork(asyncCtx)
}()

// 如果需要共享状态，继续使用 Retain
ctx.Retain()
go func() {
    defer ctx.Release()
    doWork(ctx)
}()
```

#### 3. 长时间运行的 Handler

**v1.2.x:**
```go
bot.engine.OnC2C().Handle(func(ctx *remilia.Context) error {
    // 长时间运行，无法中断
    for i := 0; i < 1000; i++ {
        doWork(i)
    }
    return nil
})
```

**v1.3.0（推荐）:**
```go
bot.engine.OnC2C().Handle(func(ctx *remilia.Context) error {
    // 响应 context 取消
    for i := 0; i < 1000; i++ {
        select {
        case <-ctx.Context().Done():
            return ctx.Context().Err()
        default:
            doWork(i)
        }
    }
    return nil
})
```

### 兼容性

- **向后兼容**：v1.3.0 完全兼容 v1.2.x 的代码
- **无需修改**：现有代码可以直接运行
- **建议升级**：新代码建议使用新特性以获得更好的安全性和可靠性

### 性能影响

- **Clone 方法**：比 Retain 稍慢（需要复制 State），但对大多数应用可忽略
- **Context 取消检测**：几乎无性能影响
- **运行时检测**：无性能影响（仅在错误时记录日志）

---

## 示例代码

### 完整示例：带优雅关闭的 Bot

```go
package main

import (
    "context"
    "log"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/KomeiDiSanXian/remilia"
    "github.com/KomeiDiSanXian/remilia/openapi/dto"
)

func main() {
    info := &dto.BotInfo{
        AppID:  os.Getenv("BOT_APPID"),
        Secret: os.Getenv("BOT_SECRET"),
        Token:  os.Getenv("BOT_TOKEN"),
    }

    bot := remilia.New(info)

    // 注册响应 context 取消的 handler
    bot.GetEngine().OnC2C().Handle(func(ctx *remilia.Context) error {
        // 异步任务使用 Clone
        go func() {
            asyncCtx := ctx.Clone()
            defer asyncCtx.Release()

            // 模拟长时间任务，但响应取消
            for i := 0; i < 10; i++ {
                select {
                case <-asyncCtx.Context().Done():
                    log.Printf("Task cancelled at step %d", i)
                    return
                case <-time.After(time.Second):
                    log.Printf("Processing step %d", i)
                }
            }
        }()

        return ctx.SendText("任务已启动")
    })

    // 启动 bot
    bot.Start()
    log.Println("Bot started")

    // 等待关闭信号
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
    sig := <-sigChan
    log.Printf("Received signal: %v", sig)

    // 优雅关闭（10 秒超时）
    log.Println("Starting graceful shutdown...")
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    bot.Shutdown(ctx)

    if ctx.Err() == context.DeadlineExceeded {
        log.Println("Shutdown timeout, some handlers may still be running")
    } else {
        log.Println("Shutdown completed successfully")
    }
}
```

---

## 常见问题

### Q: Clone 和 Retain 应该如何选择？

**A:** 
- 如果需要**独立状态**（修改不影响原 Context）→ 使用 `Clone()`
- 如果需要**共享状态**（多个 goroutine 协作）→ 使用 `Retain()`
- 如果不确定 → 使用 `Clone()` 更安全

### Q: 是否必须响应 Context 取消？

**A:** 不是必须，但**强烈推荐**。响应取消可以：
- 加快 Bot 关闭速度
- 释放资源
- 避免无用的计算

### Q: 如何调试引用计数问题？

**A:** 
1. 查看错误日志（过度释放会自动记录）
2. 使用 `atomic.LoadInt32(&ctx.refs)` 检查引用计数
3. 在开发环境启用 panic（见上文）

### Q: Clone 的性能如何？

**A:** 
- **小 State**（<10 个键）：几乎无影响
- **大 State**（>100 个键）：比 Retain 慢 2-3 倍，但仍然很快（纳秒级）
- 大多数场景下，安全性比性能更重要

### Q: 旧代码需要立即升级吗？

**A:** 不需要。v1.3.0 完全兼容旧代码。但建议：
- 新代码使用新特性
- 关键 handler 添加 context 取消响应
- 异步操作考虑使用 Clone

---

## 总结

v1.3.0 的改进使 Remilia 更加**可靠**和**安全**：

✅ **优雅关闭**：五步关闭流程，确保资源正确释放  
✅ **Context 传播**：主动取消 handler，加快关闭速度  
✅ **Clone 方法**：更安全的异步使用方式  
✅ **运行时检测**：及早发现引用计数错误  
✅ **向后兼容**：无需修改现有代码  

开始使用这些新特性，让你的 Bot 更加健壮！

---

**相关文档:**
- [CONTEXT_USAGE_GUIDE.md](./CONTEXT_USAGE_GUIDE.md)
- [ERROR_HANDLING.md](./ERROR_HANDLING.md)
- [GUIDE.md](./GUIDE.md)

