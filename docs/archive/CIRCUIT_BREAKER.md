# Circuit Breaker 熔断器

## 概述

熔断器（Circuit Breaker）是一种防止故障级联的保护机制。当连续失败次数达到阈值时，熔断器会自动开启，拒绝后续请求，避免继续对故障服务发起调用。经过一段时间后，熔断器会进入半开状态，尝试恢复服务。

## 熔断器状态

熔断器有三种状态：

### 1. Closed（闭合）
- 正常工作状态
- 所有请求正常处理
- 失败会被记录，成功会重置失败计数

### 2. Open（开启）
- 熔断状态
- 拒绝所有请求，直接返回错误
- 经过 `ResetTimeout` 后自动进入半开状态

### 3. Half-Open（半开）
- 尝试恢复状态
- 允许限定数量的请求通过（`HalfOpenMaxRequests`）
- 如果请求成功，转为 Closed 状态
- 如果请求失败，重新转为 Open 状态

## 状态转换图

```
       失败达到阈值         超时           成功
Closed ---------> Open ---------> Half-Open ---------> Closed
                   ^                  |
                   |       失败        |
                   +------------------+
```

## 使用方法

### 基本用法

```go
package main

import (
    "time"
    "github.com/KomeiDiSanXian/remilia/middleware"
)

func main() {
    // 创建熔断器
    cb := middleware.NewCircuitBreaker(middleware.CircuitBreakerConfig{
        MaxFailures:         5,                // 最大失败次数
        ResetTimeout:        30 * time.Second, // 重置超时时间
        HalfOpenMaxRequests: 3,                // 半开状态下允许的请求数
    })

    // 注册为中间件
    engine.Use(middleware.CircuitBreakerMiddleware(cb))
}
```

### 配置说明

```go
type CircuitBreakerConfig struct {
    // MaxFailures 触发熔断的最大失败次数
    // 默认: 5
    MaxFailures int

    // ResetTimeout 熔断器重置超时时间（从 Open 到 Half-Open）
    // 默认: 30 秒
    ResetTimeout time.Duration

    // HalfOpenMaxRequests 半开状态下允许的最大请求数
    // 用于测试服务是否恢复
    // 默认: 1
    HalfOpenMaxRequests int

    // OnStateChange 状态变化回调（可选）
    OnStateChange func(from, to CircuitBreakerState)
}
```

### 状态监控

```go
// 获取当前状态
state := cb.GetState()
fmt.Printf("Current state: %s\n", state)

// 获取失败次数
failures := cb.GetFailures()
fmt.Printf("Failures: %d\n", failures)

// 获取统计信息
stats := cb.Stats()
fmt.Printf("State: %s, Failures: %d, Last Failure: %s\n",
    stats.State, stats.Failures, stats.LastFailure)
```

### 手动控制

```go
// 手动重置熔断器
cb.Reset()
```

### 状态变化回调

```go
cb := middleware.NewCircuitBreaker(middleware.CircuitBreakerConfig{
    MaxFailures:  5,
    ResetTimeout: 30 * time.Second,
    OnStateChange: func(from, to middleware.CircuitBreakerState) {
        log.Printf("Circuit breaker state changed: %s -> %s", from, to)
        
        // 可以在这里发送告警
        if to == middleware.StateOpen {
            alert.Send("Circuit breaker opened!")
        }
    },
})
```

## 使用场景

### 1. 保护外部服务调用

```go
// 为调用外部 API 的 handler 添加熔断保护
apiCircuitBreaker := middleware.NewCircuitBreaker(middleware.CircuitBreakerConfig{
    MaxFailures:  3,
    ResetTimeout: 60 * time.Second,
})

engine.On(dto.C2CMessageCreate, handler).
    Use(middleware.CircuitBreakerMiddleware(apiCircuitBreaker))
```

### 2. 数据库连接保护

```go
dbCircuitBreaker := middleware.NewCircuitBreaker(middleware.CircuitBreakerConfig{
    MaxFailures:  10,
    ResetTimeout: 30 * time.Second,
    OnStateChange: func(from, to middleware.CircuitBreakerState) {
        if to == middleware.StateOpen {
            log.Error("Database circuit breaker opened - too many failures")
            metrics.Inc("db_circuit_breaker_open")
        }
    },
})

engine.Use(middleware.CircuitBreakerMiddleware(dbCircuitBreaker))
```

### 3. 多级熔断

```go
// 全局熔断器
globalCB := middleware.NewCircuitBreaker(middleware.CircuitBreakerConfig{
    MaxFailures:  20,
    ResetTimeout: 60 * time.Second,
})
engine.Use(middleware.CircuitBreakerMiddleware(globalCB))

// 特定功能的熔断器
paymentCB := middleware.NewCircuitBreaker(middleware.CircuitBreakerConfig{
    MaxFailures:  5,
    ResetTimeout: 30 * time.Second,
})
engine.On(dto.GroupAtMessageCreate, paymentHandler).
    Use(middleware.CircuitBreakerMiddleware(paymentCB))
```

## 最佳实践

### 1. 合理设置阈值

```go
// 高流量服务：较高的失败阈值
highTrafficCB := middleware.NewCircuitBreaker(middleware.CircuitBreakerConfig{
    MaxFailures:  20,
    ResetTimeout: 30 * time.Second,
})

// 关键服务：较低的失败阈值
criticalCB := middleware.NewCircuitBreaker(middleware.CircuitBreakerConfig{
    MaxFailures:  3,
    ResetTimeout: 60 * time.Second,
})
```

### 2. 监控和告警

```go
cb := middleware.NewCircuitBreaker(middleware.CircuitBreakerConfig{
    MaxFailures:  5,
    ResetTimeout: 30 * time.Second,
    OnStateChange: func(from, to middleware.CircuitBreakerState) {
        // 记录到监控系统
        metrics.RecordStateChange(string(from), string(to))
        
        // 开启时发送告警
        if to == middleware.StateOpen {
            alert.Send(fmt.Sprintf("Circuit breaker opened: %s -> %s", from, to))
        }
        
        // 恢复时发送通知
        if from == middleware.StateHalfOpen && to == middleware.StateClosed {
            notification.Send("Circuit breaker recovered")
        }
    },
})
```

### 3. 与重试中间件配合

```go
// 先重试，后熔断
engine.Use(
    middleware.RetryWithBackoff(middleware.RetryConfig{
        MaxAttempts: 3,
        InitialDelay: time.Second,
    }),
    middleware.CircuitBreakerMiddleware(cb),
)
```

### 4. 降级处理

```go
// 自定义中间件，在熔断时返回降级响应
func CircuitBreakerWithFallback(cb *middleware.CircuitBreaker, fallback HandlerE) HandlerMiddleware {
    return func(next HandlerE) HandlerE {
        return func(ctx *Context) error {
            if cb.GetState() == middleware.StateOpen {
                log.Warn("Circuit is open, using fallback")
                return fallback(ctx)
            }
            
            err := next(ctx)
            if err != nil {
                cb.onFailure()
            } else {
                cb.onSuccess()
            }
            return err
        }
    }
}

// 使用
engine.Use(CircuitBreakerWithFallback(cb, func(ctx *Context) error {
    // 返回缓存数据或默认响应
    return ctx.ReplyText("服务暂时不可用，请稍后再试")
}))
```

## 性能考虑

1. **无锁设计**: 使用 `atomic.Value` 和 `atomic.Int32`，避免锁竞争
2. **快速路径**: Closed 状态下只需要一次原子操作
3. **并发安全**: 所有操作都是并发安全的

## 注意事项

1. **不要过度使用**: 只在需要保护的关键路径上使用熔断器
2. **合理设置超时**: `ResetTimeout` 应该给服务足够的恢复时间
3. **监控状态变化**: 使用 `OnStateChange` 回调监控熔断器状态
4. **测试恢复逻辑**: 确保半开状态下的恢复逻辑正常工作

## 示例项目

完整示例请参考：`example/circuitbreaker/main.go`

## 参考资料

- [Martin Fowler - CircuitBreaker](https://martinfowler.com/bliki/CircuitBreaker.html)
- [Microsoft - Circuit Breaker Pattern](https://docs.microsoft.com/en-us/azure/architecture/patterns/circuit-breaker)

