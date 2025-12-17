# 错误处理机制

本文档阐述 Remilia 在核心模块中的错误处理约定与最佳实践（基于 v1.2.0 中间件体系）。

## 总体原则
- 尽量通过 **返回 error** 或 **日志记录后降级运行** 来处理错误，而不是直接退出进程。
- 上层可以根据 error 做重试、熔断或告警。
- 日志应包含充分的上下文信息（模块名、关键字段），便于排查问题。
- 使用标准化错误结构，携带完整上下文信息（trace、source、attempt 等）。

## 核心能力

### 1) 标准化错误结构（HandlerError）
```go
type HandlerError struct {
    Message string   `json:"message"`  // 错误消息
    Source  string   `json:"source"`   // 触发来源（plugin:<name> 或 global）
    Attempt int      `json:"attempt"`  // 当前重试次数
    Trace   []string `json:"trace"`    // 中间件链名称序列（需启用 SetTrace）
    EventID string   `json:"event_id"` // 事件 ID
}
```

所有通过 handler 调用产生的错误都会被统一包装为 `HandlerError`，携带完整的上下文信息。

### 2) 错误处理中间件（middleware.ErrorHandler）

v1.2.0 中，原有的 `engine.AddErrorHandler` 已移除，统一通过 **中间件** 处理错误：

```go
engine := remilia.NewEngine()

engine.Use(
    middleware.ErrorHandler(func(ctx *remilia.Context, err error) {
        // 提取标准化错误信息（如果存在）
        var herr remilia.HandlerError
        if errors.As(err, &herr) {
            log.Printf("Error from %s, attempt %d, trace: %v",
                herr.Source, herr.Attempt, herr.Trace)
        } else {
            log.Printf("Handler error: %v", err)
        }
        // 集中处理：日志、告警、指标上报等
    }),
)

// 推荐使用 HandleE，将错误交给错误处理中间件
engine.On(remilia.OnC2CMessage()).
    HandleE(func(ctx *remilia.Context) error {
        if err := someOperation(); err != nil {
            return err // 会被 WrapError 包装并进入 ErrorHandler
        }
        return nil
    })
```

触发时机：
- `HandleE` 返回的错误
- 由 `middleware.Recover` 捕获的 panic 转换成的错误

### 3) Panic 恢复（middleware.Recover）
```go
import "github.com/KomeiDiSanXian/remilia/middleware"

// 添加 Panic 恢复中间件
engine.Use(middleware.Recover(engine))
```
- Recover 中间件会捕获 Handler 中的 panic，避免引擎崩溃。
- panic 会被转换为 error，再被错误处理中间件捕获。
- 开发阶段可以不加 Recover，以便直接暴露 panic 问题。

### 4) Handler 返回值支持（Matcher.HandleE）
```go
engine.On(remilia.OnC2CMessage()).
    HandleE(func(ctx *remilia.Context) error {
        // 业务处理，错误会被 ErrorHandler 捕获
        if err := someOperation(); err != nil {
            return err
        }
        return nil
    })
```
- 向后兼容：仍然支持 `Handle(func(ctx *Context))`。
- 推荐优先使用 `HandleE`，以便错误能统一交由中间件处理。

### 5) 重试与死信队列（通过 Retry 中间件）

v1.2.0 中，原有的 `engine.EnableRetry`、`AddDeadLetterConsumer`、`DeadLetter()` 等 API 已移除，统一通过 **重试中间件 + 可插拔死信消费逻辑** 实现：

```go
// 自定义死信处理逻辑
type DeadLetterHandler func(ctx *remilia.Context, err error)

func FileDeadLetter(path string) DeadLetterHandler {
    return func(ctx *remilia.Context, err error) {
        // 将错误写入文件，或转发到外部系统
    }
}

// 配置重试中间件
engine.Use(middleware.Retry(middleware.RetryConfig{
    MaxAttempts: 3,
    BackoffBase: 200 * time.Millisecond,
    BackoffMax:  2 * time.Second,
    ShouldRetry: func(err error) bool {
        // 仅对网络类错误进行重试
        return isNetworkError(err)
    },
    OnGiveUp: func(ctx *remilia.Context, err error) {
        // 在这里实现你的“死信队列”逻辑
        // 例如写文件、发 Webhook、推送到 Kafka 等
    },
}))
```

相比旧的内建 deadletter 队列，新方案有几个优点：
- 消费方式完全由业务自定义（文件、Webhook、Kafka 等）。
- 不再在 Engine 内部维护固定的 channel 和消费者。
- 与中间件生态一致，易于组合和扩展。

### 6) 生命周期管理（Engine.Stop / Bot.Shutdown）
```go
// 优雅关闭时停止引擎
bot.Shutdown(ctx) // 内部会调用 engine.Stop()

// 手动停止
engine.Stop() // 阻止新的重试调度等内部后台任务
```
- `Stop()` 会设置停止标志，使后台调度逻辑（如重试）尽快退出。
- 防止在应用关闭后仍有后台 goroutine 持续运行。

## 其他模块的错误处理约定

### Context 模块
- `SendGroupMessage` / `SendSingleMessage`：当 `OpenAPI` 尚未初始化时返回 `ErrNilAPI`，并记录错误日志；调用方应显式处理该错误。

### Webhook 模块
- `webhook.New`：bigcache 创建失败时不再直接退出进程，而是记录错误并以 “无去重缓存” 模式运行（`bigCache=nil`）。
- `handleDispatch`：`bigCache=nil` 时直接分发事件并记录 warn；通道满时不阻塞，记录 warn 并丢弃事件。

### Bot 模块
- HTTP 服务器启动失败不再 `Fatalf` 退出，而是记录 error，由上层决定是否退出或重试。

## 示例

```go
engine := remilia.NewEngine()

engine.Use(
    middleware.Recover(engine),
    middleware.ErrorHandler(func(ctx *remilia.Context, err error) {
        logrus.WithError(err).WithField("source", ctx.Source()).Error("handler failed")
    }),
)

engine.On(remilia.OnC2CMessage()).
    HandleE(func(ctx *remilia.Context) error {
        if _, err := ctx.SendSingleMessage("uid", &dto.Message{Content: "hi"}); err != nil {
            return err
        }
        return nil
    })
```

## 测试建议
- 验证 `HandleE` 返回错误时，错误处理中间件会被调用。
- 验证开启/关闭 `Recover` 中间件时 panic 行为的差异。
- 验证 `ErrNilAPI` 错误的返回路径和日志输出。
- 验证在配置 bigcache 失败或通道满时，Webhook 仍能以可预期方式工作。

<!--
// 本文档基于 v1.2.0 中间件体系，旧的 `engine.AddErrorHandler` 接口已在 CHANGELOG 中标记为移除，这里不再赘述旧用法。

# 错误处理机制

本文档阐述 Remilia 在核心模块中的错误处理约定与最佳实践（基于 v1.2.0 中间件体系）。

## 总体原则
- 尽量通过 **返回 error** 或 **日志记录后降级运行** 来处理错误，而不是直接退出进程。
- 上层可以根据 error 做重试、熔断或告警。
- 日志应包含充分的上下文信息（模块名、关键字段），便于排查问题。
- 使用标准化错误结构，携带完整上下文信息（trace、source、attempt 等）。

## 核心能力

### 1) 标准化错误结构（HandlerError）
```go
type HandlerError struct {
    Message string   `json:"message"`  // 错误消息
    Source  string   `json:"source"`   // 触发来源（plugin:<name> 或 global）
    Attempt int      `json:"attempt"`  // 当前重试次数
    Trace   []string `json:"trace"`    // 中间件链名称序列（需启用 SetTrace）
    EventID string   `json:"event_id"` // 事件 ID
}
```

所有通过 handler 调用产生的错误都会被统一包装为 `HandlerError`，携带完整的上下文信息。

### 2) 错误处理中间件（middleware.ErrorHandler）

v1.2.0 中，原有的 `engine.AddErrorHandler` 已移除，统一通过 **中间件** 处理错误：

```go
engine := remilia.NewEngine()

engine.Use(
    middleware.ErrorHandler(func(ctx *remilia.Context, err error) {
        // 提取标准化错误信息（如果存在）
        var herr remilia.HandlerError
        if errors.As(err, &herr) {
            log.Printf("Error from %s, attempt %d, trace: %v",
                herr.Source, herr.Attempt, herr.Trace)
        } else {
            log.Printf("Handler error: %v", err)
        }
        // 集中处理：日志、告警、指标上报等
    }),
)

// 推荐使用 HandleE，将错误交给错误处理中间件
engine.On(remilia.OnC2CMessage()).
    HandleE(func(ctx *remilia.Context) error {
        if err := someOperation(); err != nil {
            return err // 会被 WrapError 包装并进入 ErrorHandler
        }
        return nil
    })
```

触发时机：
- `HandleE` 返回的错误
- 由 `middleware.Recover` 捕获的 panic 转换成的错误

### 3) Panic 恢复（middleware.Recover）
```go
import "github.com/KomeiDiSanXian/remilia/middleware"

// 添加 Panic 恢复中间件
engine.Use(middleware.Recover(engine))
```
- Recover 中间件会捕获 Handler 中的 panic，避免引擎崩溃。
- panic 会被转换为 error，再被错误处理中间件捕获。
- 开发阶段可以不加 Recover，以便直接暴露 panic 问题。

### 4) Handler 返回值支持（Matcher.HandleE）
```go
engine.On(remilia.OnC2CMessage()).
    HandleE(func(ctx *remilia.Context) error {
        // 业务处理，错误会被 ErrorHandler 捕获
        if err := someOperation(); err != nil {
            return err
        }
        return nil
    })
```
- 向后兼容：仍然支持 `Handle(func(ctx *Context))`。
- 推荐优先使用 `HandleE`，以便错误能统一交由中间件处理。

### 5) 重试与死信队列（通过 Retry 中间件）

v1.2.0 中，原有的 `engine.EnableRetry`、`AddDeadLetterConsumer`、`DeadLetter()` 等 API 已移除，统一通过 **重试中间件 + 可插拔死信消费逻辑** 实现：

```go
// 自定义死信处理逻辑
type DeadLetterHandler func(ctx *remilia.Context, err error)

func FileDeadLetter(path string) DeadLetterHandler {
    return func(ctx *remilia.Context, err error) {
        // 将错误写入文件，或转发到外部系统
    }
}

// 配置重试中间件
engine.Use(middleware.Retry(middleware.RetryConfig{
    MaxAttempts: 3,
    BackoffBase: 200 * time.Millisecond,
    BackoffMax:  2 * time.Second,
    ShouldRetry: func(err error) bool {
        // 仅对网络类错误进行重试
        return isNetworkError(err)
    },
    OnGiveUp: func(ctx *remilia.Context, err error) {
        // 在这里实现你的“死信队列”逻辑
        // 例如写文件、发 Webhook、推送到 Kafka 等
    },
}))
```

相比旧的内建 deadletter 队列，新方案有几个优点：
- 消费方式完全由业务自定义（文件、Webhook、Kafka 等）。
- 不再在 Engine 内部维护固定的 channel 和消费者。
- 与中间件生态一致，易于组合和扩展。

### 6) 生命周期管理（Engine.Stop / Bot.Shutdown）
```go
// 优雅关闭时停止引擎
bot.Shutdown(ctx) // 内部会调用 engine.Stop()

// 手动停止
engine.Stop() // 阻止新的重试调度等内部后台任务
```
- `Stop()` 会设置停止标志，使后台调度逻辑（如重试）尽快退出。
- 防止在应用关闭后仍有后台 goroutine 持续运行。

## 其他模块的错误处理约定

### Context 模块
- `SendGroupMessage` / `SendSingleMessage`：当 `OpenAPI` 尚未初始化时返回 `ErrNilAPI`，并记录错误日志；调用方应显式处理该错误。

### Webhook 模块
- `webhook.New`：bigcache 创建失败时不再直接退出进程，而是记录错误并以 “无去重缓存” 模式运行（`bigCache=nil`）。
- `handleDispatch`：`bigCache=nil` 时直接分发事件并记录 warn；通道满时不阻塞，记录 warn 并丢弃事件。

### Bot 模块
- HTTP 服务器启动失败不再 `Fatalf` 退出，而是记录 error，由上层决定是否退出或重试。

## 示例

```go
engine := remilia.NewEngine()

engine.Use(
    middleware.Recover(engine),
    middleware.ErrorHandler(func(ctx *remilia.Context, err error) {
        logrus.WithError(err).WithField("source", ctx.Source()).Error("handler failed")
    }),
)

engine.On(remilia.OnC2CMessage()).
    HandleE(func(ctx *remilia.Context) error {
        if _, err := ctx.SendSingleMessage("uid", &dto.Message{Content: "hi"}); err != nil {
            return err
        }
        return nil
    })
```

## 测试建议
- 验证 `HandleE` 返回错误时，错误处理中间件会被调用。
- 验证开启/关闭 `Recover` 中间件时 panic 行为的差异。
- 验证 `ErrNilAPI` 错误的返回路径和日志输出。
- 验证在配置 bigcache 失败或通道满时，Webhook 仍能以可预期方式工作。
-->
