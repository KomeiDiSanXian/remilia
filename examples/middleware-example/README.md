# Middleware Example

这个示例展示了 Remilia 框架中各种内置中间件的使用方法，以及如何编写自定义中间件。

## 功能

本示例演示了以下中间件：

### 1. 基础中间件
- ✅ **Logging** - 日志记录
- ✅ **Recover** - Panic 恢复
- ✅ **RequestID** - 请求 ID 生成

### 2. 流量控制
- ✅ **ConcurrencyLimit** - 并发限制
- ✅ **Timeout** - 超时控制
- ✅ **AdaptiveRateLimit** - 自适应限流

### 3. 可靠性保障
- ✅ **Retry** - 重试机制
- ✅ **CircuitBreaker** - 熔断器
- ✅ **Degradation** - 降级机制
- ✅ **DeadLetterQueue** - 死信队列
- ✅ **Deduplicator** - 去重

### 4. 可观测性
- ✅ **PrometheusMetrics** - Prometheus 指标
- ✅ **SlowHandlerDetector** - 慢处理器检测

### 5. 自定义中间件
- ✅ Auth - 鉴权
- ✅ Counter - 计数
- ✅ ResponseTime - 响应时间记录

## 运行

```bash
# 设置环境变量
export BOT_SECRET="your-webhook-secret"
export BOT_PORT="8080"

# 运行
go run -tags example main.go
```

## 测试命令

```
/hello - 正常命令
/slow - 慢命令（2秒）
/fail - 失败命令
/panic - Panic 命令
/heavy - 高负载命令
```

## 中间件详解

### 1. Logging - 日志记录

```go
eng.Use(middleware.Logging())
```

记录每个请求的执行时间和错误信息。

**输出示例**:
```
INFO  handler execution success latency=10ms type=MESSAGE_CREATE
ERROR handler execution failed latency=100ms type=MESSAGE_CREATE error="..."
```

### 2. Recover - Panic 恢复

```go
eng.Use(middleware.Recover())
```

捕获处理器中的 panic，防止程序崩溃。

**场景**: 当处理器发生 panic 时，记录详细堆栈并返回错误，而不是让整个程序崩溃。

### 3. ConcurrencyLimit - 并发限制

```go
eng.Use(middleware.ConcurrencyLimit(
    100,                         // 最大并发数
    middleware.ConcurrencyDrop,  // 策略：丢弃
    0,                           // 超时时间
))
```

**策略**:
- `ConcurrencyDrop` - 超过限制时立即丢弃
- `ConcurrencyBlock` - 阻塞等待
- `ConcurrencyTryWait` - 等待一段时间，超时则丢弃

### 4. Timeout - 超时控制

```go
eng.Use(middleware.Timeout(5 * time.Second))
```

确保每个处理器在指定时间内完成，否则返回超时错误。

### 5. Retry - 重试机制

```go
eng.Use(middleware.Retry(3))  // 最多重试 3 次
```

当处理失败时自动重试，支持指数退避。

### 6. CircuitBreaker - 熔断器

```go
eng.Use(middleware.CircuitBreaker(
    5,                  // 失败阈值
    30*time.Second,     // 半开状态超时
))
```

**状态**:
- **Closed** - 正常状态
- **Open** - 熔断状态（快速失败）
- **Half-Open** - 半开状态（尝试恢复）

### 7. Degradation - 降级机制

```go
degradation := middleware.NewAdaptiveDegradation(
    middleware.AdaptiveDegradationConfig{
        ErrorRateThreshold:   0.5,            // 50% 错误率
        LatencyThreshold:     1 * time.Second, // 1秒延迟
        SamplingWindow:       1 * time.Minute,
        MinSamplesForDecision: 10,
    },
)
eng.Use(degradation.Middleware())
```

**降级级别**:
- **Level 0** - 正常
- **Level 1** - 轻度降级
- **Level 2** - 中度降级
- **Level 3** - 重度降级

### 8. DeadLetterQueue - 死信队列

```go
dlqConfig := middleware.DeadLetterConfig{
    MaxRetries:   3,
    RetryDelay:   5 * time.Second,
    QueueSize:    1000,
}
dlq := middleware.NewDeadLetterQueue(dlqConfig)
eng.Use(dlq.Middleware())
```

失败的消息自动进入死信队列，支持手动重试。

**查看死信**:
```go
messages := dlq.ListDeadLetters()
for _, msg := range messages {
    log.Printf("Failed: %s, Attempts: %d", msg.ID, msg.RetryCount)
}
```

### 9. Deduplicator - 去重

```go
dedup := middleware.NewDeduplicator(
    1*time.Minute,  // 去重窗口
    10000,          // 最大记录数
)
eng.Use(dedup.Middleware())
```

防止重复消息被处理多次。

### 10. AdaptiveRateLimit - 自适应限流

```go
config := middleware.DefaultAdaptiveConfig()
limiter := middleware.NewAdaptiveRateLimiter(config)
limiter.Start()
defer limiter.Stop()

eng.Use(limiter.Middleware())
```

根据 CPU、内存、延迟自动调整并发限制。

**统计信息**:
```go
stats := limiter.GetStats()
log.Printf("Limit: %d, CPU: %.2f%%", 
    stats.CurrentLimit, 
    stats.CPUUsage*100)
```

### 11. PrometheusMetrics - Prometheus 指标

```go
config := middleware.PrometheusConfig{
    Namespace: "remilia",
    Subsystem: "bot",
}
eng.Use(middleware.PrometheusMetrics(config))
```

导出 Prometheus 指标到 `/metrics` 端点。

**可用指标**:
- `remilia_bot_messages_total` - 消息总数
- `remilia_bot_message_duration_seconds` - 处理延迟
- `remilia_bot_errors_total` - 错误总数

### 12. SlowHandlerDetector - 慢处理器检测

```go
eng.Use(middleware.SlowHandlerDetector(1 * time.Second))
```

检测并记录超过阈值的慢处理器。

## 自定义中间件

### 编写中间件

```go
func MyMiddleware() eventctx.Middleware {
    return func(next eventctx.Handler) eventctx.Handler {
        return func(ctx *eventctx.Context) error {
            // 前置处理
            log.Println("Before handler")
            
            // 调用下一个处理器
            err := next(ctx)
            
            // 后置处理
            log.Println("After handler")
            
            return err
        }
    }
}

// 使用
eng.Use(MyMiddleware())
```

### 带状态的中间件

```go
type StatefulMiddleware struct {
    counter int64
    mu      sync.Mutex
}

func (m *StatefulMiddleware) Middleware() eventctx.Middleware {
    return func(next eventctx.Handler) eventctx.Handler {
        return func(ctx *eventctx.Context) error {
            m.mu.Lock()
            m.counter++
            count := m.counter
            m.mu.Unlock()
            
            log.Printf("Request #%d", count)
            return next(ctx)
        }
    }
}

// 使用
sm := &StatefulMiddleware{}
eng.Use(sm.Middleware())
```

### 条件中间件

```go
func ConditionalMiddleware(condition func(*eventctx.Context) bool) eventctx.Middleware {
    return func(next eventctx.Handler) eventctx.Handler {
        return func(ctx *eventctx.Context) error {
            if condition(ctx) {
                // 执行特殊逻辑
                log.Println("Condition matched")
            }
            return next(ctx)
        }
    }
}

// 使用
eng.Use(ConditionalMiddleware(func(ctx *eventctx.Context) bool {
    return ctx.GetEventType() == dto.EventTypeAtMessageCreate
}))
```

## 中间件组合

### 按需组合

```go
// 开发环境
if os.Getenv("ENV") == "dev" {
    eng.Use(
        middleware.Logging(),
        middleware.Recover(),
    )
}

// 生产环境
if os.Getenv("ENV") == "prod" {
    eng.Use(
        middleware.Logging(),
        middleware.Recover(),
        middleware.PrometheusMetrics(config),
        middleware.ConcurrencyLimit(1000, ...),
        adaptiveLimiter.Middleware(),
    )
}
```

### 分组中间件

```go
// 全局中间件
eng.Use(
    middleware.Logging(),
    middleware.Recover(),
)

// 特定组的中间件
eng.UseForGroup("admin", 
    middleware.Auth(isAdmin),
    middleware.Logging(),
)
```

## 性能考虑

### 中间件顺序

中间件按注册顺序执行，建议顺序：

1. **Logging** - 最外层，记录所有请求
2. **Recover** - 捕获 panic
3. **RequestID** - 生成追踪 ID
4. **Metrics** - 收集指标
5. **RateLimit** - 限流控制
6. **Timeout** - 超时控制
7. **Retry** - 重试逻辑
8. **CircuitBreaker** - 熔断保护
9. **Degradation** - 降级策略
10. **业务中间件** - 自定义逻辑

### 性能优化

```go
// ❌ 不好：每次都创建新对象
eng.Use(func(next eventctx.Handler) eventctx.Handler {
    return func(ctx *eventctx.Context) error {
        data := make(map[string]interface{})  // 每次分配
        // ...
        return next(ctx)
    }
})

// ✅ 好：复用对象
pool := &sync.Pool{
    New: func() interface{} {
        return make(map[string]interface{})
    },
}

eng.Use(func(next eventctx.Handler) eventctx.Handler {
    return func(ctx *eventctx.Context) error {
        data := pool.Get().(map[string]interface{})
        defer pool.Put(data)
        // ...
        return next(ctx)
    }
})
```

## 监控和调试

### 查看统计信息

```go
// 定期打印统计
ticker := time.NewTicker(1 * time.Minute)
go func() {
    for range ticker.C {
        // 降级统计
        degradationStats := degradation.GetStats()
        
        // 自适应限流统计
        adaptiveStats := adaptive.GetStats()
        
        // 死信队列统计
        dlqStats := dlq.GetStats()
        
        log.Printf("Stats: degradation=%v, adaptive=%v, dlq=%v",
            degradationStats, adaptiveStats, dlqStats)
    }
}()
```

### Prometheus 可视化

访问 `http://localhost:9090/metrics` 查看所有指标，然后使用 Grafana 创建仪表板。

## 下一步

- 查看 [自适应限流文档](../../docs/ADAPTIVE_RATE_LIMITING.md)
- 查看 [死信队列文档](../../infra/dlq/README.md)
- 查看 [中间件源码](../../middleware/)
