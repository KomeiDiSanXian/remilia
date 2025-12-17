# 优雅关闭最佳实践指南

## 概述

Remilia 框架支持优雅关闭（Graceful Shutdown），允许 Bot 在关闭时正确完成正在进行的任务，避免数据丢失或处理中断。

## 关闭流程

当调用 `bot.Shutdown(ctx)` 时，框架执行以下步骤：

1. **取消所有 Handler** - 通过 Context 传播取消信号
2. **停止事件循环** - 不再接收新事件
3. **关闭 HTTP 服务器** - 停止接收新连接
4. **排空事件通道** - 处理或丢弃待处理事件
5. **等待 Handler 完成** - 等待所有正在执行的 Handler 完成（带超时）

```go
func main() {
    bot := remilia.New(info)
    bot.Start()
    
    // 监听系统信号
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
    <-sigCh
    
    // 优雅关闭（5秒超时）
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    bot.Shutdown(ctx)
}
```

---

## Handler 最佳实践

### 1. 短时间任务（推荐）

对于快速完成的任务，无需特殊处理：

```go
engine.OnC2C().HandleE(func(ctx *remilia.Context) error {
    // 快速操作（< 1秒）
    user := ctx.GetUserID()
    return ctx.Reply(&dto.Message{
        Content: fmt.Sprintf("Hello, %s!", user),
    })
})
```

**优点**: 简单直接，大部分场景适用

---

### 2. 长时间任务 - 检查取消信号

对于可能长时间运行的任务，应该定期检查 Context 是否被取消：

#### 方式 1: 使用 select 检查

```go
engine.OnC2C().HandleE(func(ctx *remilia.Context) error {
    for i := 0; i < 10; i++ {
        // 检查是否被取消
        select {
        case <-ctx.Context().Done():
            logrus.Info("Handler cancelled, cleaning up...")
            return ctx.Context().Err()
        default:
            // 继续处理
        }
        
        // 执行任务
        processStep(i)
        time.Sleep(time.Second)
    }
    return nil
})
```

#### 方式 2: 使用带 Context 的 API

```go
engine.OnC2C().HandleE(func(ctx *remilia.Context) error {
    // HTTP 请求自动响应取消
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
        // 如果 context 被取消，err 会是 context.Canceled
        return err
    }
    defer resp.Body.Close()
    
    // 处理响应
    return processResponse(resp)
})
```

#### 方式 3: 使用 Context 的超时

```go
engine.OnC2C().HandleE(func(ctx *remilia.Context) error {
    // 为操作设置超时（会继承父 context 的取消）
    timeoutCtx, cancel := context.WithTimeout(ctx.Context(), 3*time.Second)
    defer cancel()
    
    // 数据库查询
    rows, err := db.QueryContext(timeoutCtx, "SELECT * FROM users WHERE id = ?", userID)
    if err != nil {
        if err == context.Canceled {
            logrus.Info("Query cancelled during shutdown")
        }
        return err
    }
    defer rows.Close()
    
    return processRows(rows)
})
```

---

### 3. 后台任务 - 使用 Goroutine

如果 Handler 需要启动后台任务，应该正确传播 Context：

```go
engine.OnGroupAt().HandleE(func(ctx *remilia.Context) error {
    // 启动后台任务
    go func() {
        // 使用 context 支持取消
        ticker := time.NewTicker(time.Second)
        defer ticker.Stop()
        
        for {
            select {
            case <-ctx.Context().Done():
                logrus.Info("Background task cancelled")
                return
            case <-ticker.C:
                // 执行定期任务
                doPeriodicWork()
            }
        }
    }()
    
    // 主任务立即返回
    return ctx.Reply(&dto.Message{
        Content: "后台任务已启动",
    })
})
```

**注意**: 后台 Goroutine 不会阻止 Bot 关闭，确保它们能够正确响应取消信号。

---

### 4. 清理资源

Handler 应该在返回前清理资源，即使被取消：

```go
engine.OnC2C().HandleE(func(ctx *remilia.Context) error {
    // 打开文件
    file, err := os.Open("data.txt")
    if err != nil {
        return err
    }
    defer file.Close() // 确保资源被释放
    
    // 长时间处理
    scanner := bufio.NewScanner(file)
    for scanner.Scan() {
        // 检查取消
        select {
        case <-ctx.Context().Done():
            logrus.Info("File processing cancelled")
            return ctx.Context().Err()
        default:
        }
        
        // 处理每一行
        processLine(scanner.Text())
    }
    
    return scanner.Err()
})
```

---

## 中间件支持

### 自动超时中间件

创建中间件自动为 Handler 添加超时：

```go
func TimeoutMiddleware(timeout time.Duration) remilia.HandlerMiddleware {
    return func(next remilia.HandlerE) remilia.HandlerE {
        return func(ctx *remilia.Context) error {
            // 创建带超时的 context
            timeoutCtx, cancel := context.WithTimeout(ctx.Context(), timeout)
            defer cancel()
            
            // 替换 context
            ctx.SetContext(timeoutCtx)
            
            // 在 goroutine 中执行 handler
            errCh := make(chan error, 1)
            go func() {
                errCh <- next(ctx)
            }()
            
            // 等待完成或超时
            select {
            case err := <-errCh:
                return err
            case <-timeoutCtx.Done():
                return fmt.Errorf("handler timeout after %v", timeout)
            }
        }
    }
}

// 使用
engine.Use(TimeoutMiddleware(5 * time.Second))
```

### 取消检测中间件

记录被取消的 Handler：

```go
func CancellationLogger() remilia.HandlerMiddleware {
    return func(next remilia.HandlerE) remilia.HandlerE {
        return func(ctx *remilia.Context) error {
            err := next(ctx)
            
            // 检查是否因取消而失败
            if err == context.Canceled {
                logrus.WithFields(logrus.Fields{
                    "event_id":   ctx.GetEvent().ID,
                    "event_type": ctx.GetEventType(),
                }).Info("Handler was cancelled during shutdown")
            }
            
            return err
        }
    }
}

engine.Use(CancellationLogger())
```

---

## 测试优雅关闭

### 测试 Handler 响应取消

```go
func TestHandlerCancellation(t *testing.T) {
    engine := remilia.NewEngine()
    
    cancelled := false
    engine.OnC2C().HandleE(func(ctx *remilia.Context) error {
        // 模拟长时间任务
        select {
        case <-ctx.Context().Done():
            cancelled = true
            return ctx.Context().Err()
        case <-time.After(10 * time.Second):
            return nil
        }
    })
    
    // 创建可取消的 context
    ctx := context.Background()
    cancelCtx, cancel := context.WithCancel(ctx)
    
    // 创建事件 context
    event := &dto.Payload{Type: dto.C2CMessageCreate}
    remiliaCtx := remilia.NewContextWithContext(cancelCtx, event, nil)
    remiliaCtx.Retain()
    defer remiliaCtx.Release()
    
    // 在 goroutine 中处理
    done := make(chan struct{})
    go func() {
        engine.ProcessEvent(remiliaCtx)
        close(done)
    }()
    
    // 等待一小段时间后取消
    time.Sleep(100 * time.Millisecond)
    cancel()
    
    // 等待完成
    <-done
    
    // 验证 handler 被取消
    assert.True(t, cancelled, "Handler should be cancelled")
}
```

### 测试 Bot 优雅关闭

```go
func TestBotGracefulShutdown(t *testing.T) {
    bot := remilia.New(info)
    engine := bot.GetEngine()
    
    handlerCompleted := false
    engine.OnC2C().HandleE(func(ctx *remilia.Context) error {
        // 模拟处理
        time.Sleep(100 * time.Millisecond)
        handlerCompleted = true
        return nil
    })
    
    bot.Start()
    
    // 触发事件处理
    // ... (通过 webhook 或其他方式)
    
    // 优雅关闭
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    bot.Shutdown(ctx)
    
    // 验证 handler 完成
    assert.True(t, handlerCompleted, "Handler should complete before shutdown")
}
```

---

## 常见场景

### 场景 1: 数据库事务

```go
engine.OnC2C().HandleE(func(ctx *remilia.Context) error {
    tx, err := db.BeginTx(ctx.Context(), nil)
    if err != nil {
        return err
    }
    defer tx.Rollback() // 失败时回滚
    
    // 执行操作
    _, err = tx.ExecContext(ctx.Context(), "INSERT INTO logs ...")
    if err != nil {
        return err
    }
    
    // 提交
    return tx.Commit()
})
```

### 场景 2: 批量处理

```go
engine.OnC2C().HandleE(func(ctx *remilia.Context) error {
    items := []string{"item1", "item2", "item3", "item4", "item5"}
    
    for i, item := range items {
        // 每批次检查一次取消
        if i%100 == 0 {
            select {
            case <-ctx.Context().Done():
                logrus.Infof("Batch processing cancelled at item %d", i)
                return ctx.Context().Err()
            default:
            }
        }
        
        // 处理项目
        if err := processItem(item); err != nil {
            return err
        }
    }
    
    return nil
})
```

### 场景 3: 外部 API 调用

```go
engine.OnC2C().HandleE(func(ctx *remilia.Context) error {
    // 创建带重试的客户端
    client := &http.Client{
        Timeout: 3 * time.Second,
    }
    
    for retry := 0; retry < 3; retry++ {
        // 检查是否被取消
        select {
        case <-ctx.Context().Done():
            return ctx.Context().Err()
        default:
        }
        
        // 发送请求
        req, _ := http.NewRequestWithContext(
            ctx.Context(),
            "POST",
            "https://api.example.com/webhook",
            bytes.NewReader(data),
        )
        
        resp, err := client.Do(req)
        if err == nil && resp.StatusCode == 200 {
            resp.Body.Close()
            return nil
        }
        
        // 重试前等待
        time.Sleep(time.Second * time.Duration(retry+1))
    }
    
    return fmt.Errorf("api call failed after retries")
})
```

---

## 最佳实践总结

### ✅ 推荐做法

1. **快速任务** - 无需特殊处理，让它们自然完成
2. **长任务** - 定期检查 `ctx.Context().Done()`
3. **外部调用** - 使用 `http.NewRequestWithContext(ctx.Context(), ...)`
4. **数据库** - 使用 `db.QueryContext(ctx.Context(), ...)`
5. **清理资源** - 使用 `defer` 确保资源释放
6. **后台任务** - 传播 Context 并响应取消

### ❌ 避免做法

1. **忽略 Context** - 不检查 `Done()` 导致无法被取消
2. **阻塞操作** - 使用不带 context 的阻塞调用
3. **无限循环** - 没有退出条件的循环
4. **资源泄漏** - 忘记关闭文件、连接等
5. **超长超时** - 设置过长的超时时间

---

## 调试技巧

### 启用调试日志

```go
logrus.SetLevel(logrus.DebugLevel)
```

输出示例：
```
[Remilia] Starting graceful shutdown...
[Remilia] Cancelled all running handlers
[Remilia] Stopped event loop
[Remilia] HTTP server closed
[Remilia] Drained 3 pending events
[Remilia] All handlers completed successfully
[Remilia] Bot shutdown complete
```

### 检测慢 Handler

```go
func SlowHandlerDetector(threshold time.Duration) remilia.HandlerMiddleware {
    return func(next remilia.HandlerE) remilia.HandlerE {
        return func(ctx *remilia.Context) error {
            start := time.Now()
            err := next(ctx)
            elapsed := time.Since(start)
            
            if elapsed > threshold {
                logrus.WithFields(logrus.Fields{
                    "elapsed":    elapsed,
                    "event_type": ctx.GetEventType(),
                    "event_id":   ctx.GetEvent().ID,
                }).Warn("Slow handler detected")
            }
            
            return err
        }
    }
}
```

---

## 性能考虑

### Context 检查开销

定期检查 `ctx.Context().Done()` 的开销非常小：

```go
// ✅ 推荐：每100次迭代检查一次
for i := 0; i < 1000000; i++ {
    if i%100 == 0 {
        select {
        case <-ctx.Context().Done():
            return ctx.Context().Err()
        default:
        }
    }
    // 处理
}
```

### 超时设置建议

| 场景 | 推荐超时 |
|------|---------|
| HTTP API 调用 | 3-5 秒 |
| 数据库查询 | 5-10 秒 |
| 文件处理 | 10-30 秒 |
| 批量操作 | 30-60 秒 |
| Bot Shutdown | 5-10 秒 |

---

## 参考资料

- [Context 使用指南](./CONTEXT_USAGE_GUIDE.md)
- [优雅关闭实现](./GRACEFUL_SHUTDOWN.md)
- [中间件开发](./MIDDLEWARE.md)

---

**版本**: v2.0.0  
**更新日期**: 2025-12-08  
**作者**: Remilia Team

