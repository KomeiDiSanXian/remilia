# Remilia 中间件系统

> 最后更新: 2025-11-30 | 版本: v1.2.0+

---

## 📖 概述

Remilia 提供统一的中间件系统，支持**三个层次**的中间件注册，实现横切关注点的优雅处理。

---

## 🎯 中间件架构

### 三层中间件

```
┌─────────────────────────────────────────┐
│         Global Middleware               │  ← 全局：应用于所有匹配器
│  (Logging, Recover, Metrics...)         │
└──────────────┬──────────────────────────┘
               ↓
┌──────────────▼──────────────────────────┐
│         Plugin Middleware               │  ← 插件级：应用于特定插件
│  (Auth, RateLimit...)                   │
└──────────────┬──────────────────────────┘
               ↓
┌──────────────▼──────────────────────────┐
│         Matcher Middleware              │  ← 匹配器级：应用于单个匹配器
│  (Custom logic...)                      │
└──────────────┬──────────────────────────┘
               ↓
          Handler Execution
```

**执行顺序**: Global → Plugin → Matcher → Handler

---

## 🔧 中间件类型

### HandlerMiddleware

```go
type HandlerMiddleware func(next HandlerE) HandlerE
type HandlerE func(ctx *Context) error
```

**特点**:
- 洋葱模型（Onion Model）
- 支持前置/后置处理
- 支持错误处理和短路

### 洋葱模型执行流程详解

中间件采用**洋葱模型（Onion Model）**，每个中间件包裹内层中间件和 handler，形成嵌套结构：

```
注册代码：
    engine.Use(MiddlewareA)    // 全局中间件 A
    engine.Use(MiddlewareB)    // 全局中间件 B
    matcher.Use(MiddlewareC)   // 匹配器中间件 C
    matcher.Handle(handler)    // 业务处理器

实际执行流程（洋葱模型）：

    ┌─────────────────────────────────────────────────────────────┐
    │ MiddlewareA (Before)                                        │
    │  ┌──────────────────────────────────────────────────────┐  │
    │  │ MiddlewareB (Before)                                 │  │
    │  │  ┌───────────────────────────────────────────────┐  │  │
    │  │  │ MiddlewareC (Before)                          │  │  │
    │  │  │  ┌─────────────────────────────────────────┐  │  │  │
    │  │  │  │ Handler (业务逻辑)                      │  │  │  │
    │  │  │  └─────────────────────────────────────────┘  │  │  │
    │  │  │ MiddlewareC (After)                           │  │  │
    │  │  └───────────────────────────────────────────────┘  │  │
    │  │ MiddlewareB (After)                                  │  │
    │  └──────────────────────────────────────────────────────┘  │
    │ MiddlewareA (After)                                         │
    └─────────────────────────────────────────────────────────────┘

执行顺序：
    1. MiddlewareA (Before) - 最外层开始
    2. MiddlewareB (Before)
    3. MiddlewareC (Before)
    4. Handler - 核心业务逻辑
    5. MiddlewareC (After) - 从内层开始返回
    6. MiddlewareB (After)
    7. MiddlewareA (After) - 最外层结束
```

**实际代码示例**：

```go
// 定义中间件
func TimingMiddleware() HandlerMiddleware {
    return func(next HandlerE) HandlerE {
        return func(ctx *Context) error {
            start := time.Now()
            
            // ⬇️ Before: 执行前置逻辑
            fmt.Println("Before handler")
            
            // ➡️ 调用下一个中间件或 handler
            err := next(ctx)
            
            // ⬆️ After: 执行后置逻辑
            fmt.Printf("After handler, took %v\n", time.Since(start))
            
            return err
        }
    }
}

// 注册示例
engine.Use(TimingMiddleware())  // 全局
matcher.Use(LoggingMiddleware()) // 匹配器级
matcher.Handle(func(ctx *Context) {
    fmt.Println("Handler executing")
})

// 输出顺序：
// Before handler              (TimingMiddleware before)
// Before logging              (LoggingMiddleware before)
// Handler executing           (Handler)
// After logging               (LoggingMiddleware after)
// After handler, took 5ms     (TimingMiddleware after)
```

**关键点**：
- ✅ 外层中间件先进后出（LIFO）
- ✅ `next(ctx)` 之前是前置处理，之后是后置处理
- ✅ 可以通过不调用 `next(ctx)` 来短路执行
- ✅ 错误会沿着中间件链向外传播

---

## 📦 内置中间件

### 1. Logging - 日志记录

**功能**: 记录处理耗时和错误

```go
engine.Use(middleware.Logging())
```

**输出**:
```
INFO[0001] handler executed successfully  duration=15ms event_type=GROUP_AT_MESSAGE_CREATE
ERROR[0002] handler failed  duration=120ms error="API timeout"
```

**特性**:
- ✅ 自动记录处理时间
- ✅ 自动记录错误信息
- ✅ 结构化日志输出

---

### 2. Recover - Panic 恢复

**功能**: 捕获 panic，防止单个处理器崩溃影响整个系统

```go
engine.Use(middleware.Recover(engine))
```

**特性**:
- ✅ 捕获 panic 并转换为 error
- ✅ 记录堆栈信息
- ✅ 允许系统继续运行

**示例**:
```go
engine.On(OnCommand("/crash")).HandleE(func(ctx *Context) error {
    panic("something went wrong")  // 被 Recover 捕获
    return nil
})
```

---

### 3. Auth - 权限验证

**功能**: 验证用户权限

```go
isAdmin := func(ctx *Context) bool {
    author := ctx.GetAuthor()
    return author != nil && author.UserOpenID == "admin_id"
}

engine.UseForPlugin("admin", middleware.Auth(isAdmin))
```

**特性**:
- ✅ 自定义权限检查函数
- ✅ 未授权自动返回错误
- ✅ 记录未授权尝试

---

### 4. RateLimit - 简单限流

**功能**: 限制处理器执行频率

```go
engine.On(OnCommand("/api")).
    Use(middleware.RateLimit(time.Second)).
    HandleE(handler)
```

**特性**:
- ✅ 时间窗口限流
- ✅ 超过限制返回错误
- ✅ 简单易用

---

### 5. RateLimitTokenBucket - 令牌桶限流

**功能**: 更精细的限流控制

```go
// 全局限流：每秒 10 个，突发 20 个
engine.Use(middleware.RateLimitTokenBucket(10, 20, nil))

// 按用户限流
keyFn := func(ctx *Context) string {
    if author := ctx.GetAuthor(); author != nil {
        return author.UserOpenID
    }
    return "anonymous"
}
engine.Use(middleware.RateLimitTokenBucket(5, 10, keyFn))
```

**参数**:
- `rate`: 每秒产生的令牌数
- `burst`: 突发容量
- `keyFn`: 分桶函数（nil 为全局桶）

**特性**:
- ✅ 平滑限流
- ✅ 支持突发流量
- ✅ 支持按用户/群/自定义维度限流

---

### 6. Metrics - 指标收集

**功能**: 收集处理器执行指标

```go
engine.Use(middleware.Metrics())
```

**收集指标**:
- Handler 执行次数
- 执行耗时
- 成功/失败次数
- 错误率

---

### 7. PrometheusMetrics - Prometheus 集成

**功能**: 导出 Prometheus 格式的指标

```go
collector := engine.GetMetricsCollector()
engine.Use(middleware.PrometheusMetrics(collector))
```

**指标类型**:
- Counter: 事件处理总数、错误总数
- Histogram: 处理耗时分布
- Gauge: 当前并发数

**Prometheus 查询示例**:
```promql
# 请求速率
rate(remilia_events_processed_total[5m])

# P95 延迟
histogram_quantile(0.95, remilia_handler_duration_seconds_bucket)

# 错误率
rate(remilia_events_dropped_total[5m]) / rate(remilia_events_processed_total[5m])
```

---

### 8. Retry - 错误重试

**功能**: 自动重试失败的处理器

```go
engine.Use(middleware.Retry(middleware.RetryConfig{
    MaxAttempts: 3,
    BackoffBase: 200 * time.Millisecond,
    BackoffMax:  2 * time.Second,
}))
```

**特性**:
- ✅ 指数退避
- ✅ 最大重试次数
- ✅ 最大退避时间

---

### 9. RetryWithDeadLetter - 重试+死信队列

**功能**: 重试失败后发送到死信队列

```go
deadLetterCh := make(chan remilia.DeadLetterItem, 128)

// 启动死信消费者
go func() {
    for item := range deadLetterCh {
        log.Printf("Dead letter: %v, error: %v", item.Event, item.Err)
        // 可以写入文件、数据库、Kafka等
    }
}()

engine.Use(middleware.RetryWithDeadLetter(
    middleware.RetryConfig{
        MaxAttempts: 3,
        BackoffBase: 200 * time.Millisecond,
    },
    deadLetterCh,
))
```

**特性**:
- ✅ 失败不丢失
- ✅ 异步处理死信
- ✅ 可插拔的死信消费器

**内置消费器**:
```go
// 文件消费器
fileConsumer := remilia.FileDeadLetterConsumer{Path: "deadletter.log"}

// Webhook 消费器
webhookConsumer := remilia.WebhookDeadLetterConsumer{URL: "http://..."}

// Kafka 消费器（自定义）
kafkaConsumer := &KafkaDeadLetterConsumer{...}
```

---

### 10. Timeout - 超时控制

**功能**: 为处理器设置执行超时

```go
engine.Use(middleware.Timeout(5 * time.Second))
```

**特性**:
- ✅ 防止处理器无限阻塞
- ✅ 超时自动返回错误
- ✅ 在 goroutine 中执行（不阻塞）

**注意**: 
- 超时后 goroutine 可能仍在运行
- 应配合 Context 超时使用

---

### 11. RequestID - 请求追踪

**功能**: 为每个请求生成唯一 ID

```go
engine.Use(middleware.RequestID())
```

**使用**:
```go
engine.On(OnCommand("/test")).HandleE(func(ctx *Context) error {
    requestID, _ := ctx.GetState("request_id")
    log.Printf("Request ID: %s", requestID)
    return nil
})
```

**特性**:
- ✅ 自动生成 UUID
- ✅ 存储在 Context.State
- ✅ 便于日志关联和问题追踪

---

### 12. SlowHandler - 慢处理监控

**功能**: 监控超过阈值的慢处理器

```go
engine.Use(middleware.SlowHandler(500*time.Millisecond, func(ctx *Context, duration time.Duration) {
    log.Printf("Slow handler detected: %v, took %v", ctx.GetEventType(), duration)
}))
```

**特性**:
- ✅ 自定义阈值
- ✅ 自定义回调处理
- ✅ 用于性能监控和优化

---

## 🎨 自定义中间件

### 基本模板

```go
func MyMiddleware() remilia.HandlerMiddleware {
    return func(next remilia.HandlerE) remilia.HandlerE {
        return func(ctx *remilia.Context) error {
            // 前置处理
            log.Println("Before handler")
            
            // 调用下一个中间件或处理器
            err := next(ctx)
            
            // 后置处理
            log.Println("After handler")
            
            return err
        }
    }
}

// 使用
engine.Use(MyMiddleware())
```

---

### 示例：IP 白名单

```go
func IPWhitelist(allowedIPs []string) remilia.HandlerMiddleware {
    allowed := make(map[string]bool)
    for _, ip := range allowedIPs {
        allowed[ip] = true
    }
    
    return func(next remilia.HandlerE) remilia.HandlerE {
        return func(ctx *remilia.Context) error {
            clientIP := ctx.GetState("client_ip")
            if clientIP == nil || !allowed[clientIP.(string)] {
                return fmt.Errorf("IP not allowed")
            }
            return next(ctx)
        }
    }
}
```

---

### 示例：缓存

```go
func Cache(ttl time.Duration) remilia.HandlerMiddleware {
    cache := make(map[string]cacheItem)
    mu := sync.RWMutex{}
    
    return func(next remilia.HandlerE) remilia.HandlerE {
        return func(ctx *remilia.Context) error {
            key := ctx.GetMessageContent()
            
            // 检查缓存
            mu.RLock()
            if item, ok := cache[key]; ok && time.Now().Before(item.expiry) {
                mu.RUnlock()
                ctx.SetState("cached", true)
                return nil
            }
            mu.RUnlock()
            
            // 执行处理器
            err := next(ctx)
            if err == nil {
                // 缓存结果
                mu.Lock()
                cache[key] = cacheItem{
                    expiry: time.Now().Add(ttl),
                }
                mu.Unlock()
            }
            
            return err
        }
    }
}
```

---

## 📊 中间件组合

### 推荐组合

#### 基础组合
```go
engine.Use(middleware.Logging())
engine.Use(middleware.Recover(engine))
```

#### 生产环境组合
```go
engine.Use(middleware.RequestID())
engine.Use(middleware.Logging())
engine.Use(middleware.Recover(engine))
engine.Use(middleware.Metrics())
engine.Use(middleware.PrometheusMetrics(collector))
engine.Use(middleware.Timeout(5 * time.Second))
engine.Use(middleware.RetryWithDeadLetter(retryConfig, deadLetterCh))
engine.Use(middleware.RateLimitTokenBucket(100, 200, nil))
```

#### 管理员命令组合
```go
engine.UseForPlugin("admin", middleware.Auth(isAdmin))
engine.UseForPlugin("admin", middleware.Logging())
engine.UseForPlugin("admin", middleware.RateLimit(time.Second))
```

---

## 🎯 最佳实践

### 1. 中间件顺序很重要

```go
// ✅ 正确顺序
engine.Use(middleware.RequestID())      // 1. 生成 ID
engine.Use(middleware.Logging())        // 2. 记录日志
engine.Use(middleware.Recover(engine))  // 3. 恢复 panic
engine.Use(middleware.Auth(isAdmin))    // 4. 鉴权
engine.Use(middleware.RateLimit(...))   // 5. 限流
engine.Use(middleware.Timeout(...))     // 6. 超时

// ❌ 错误顺序
engine.Use(middleware.Timeout(...))     // 超时在前，可能导致后续中间件不执行
engine.Use(middleware.Logging())
```

### 2. 选择合适的层级

```go
// 全局中间件：通用功能
engine.Use(middleware.Logging())
engine.Use(middleware.Recover(engine))

// 插件级中间件：插件特有需求
engine.UseForPlugin("admin", middleware.Auth(isAdmin))

// 匹配器级中间件：单个命令特殊需求
engine.On(OnCommand("/expensive")).
    Use(middleware.RateLimit(5 * time.Second)).
    HandleE(expensiveHandler)
```

### 3. 避免过度使用

```go
// ❌ 避免：为每个匹配器都添加相同中间件
engine.On(OnCommand("/cmd1")).Use(middleware.Logging()).Handle(...)
engine.On(OnCommand("/cmd2")).Use(middleware.Logging()).Handle(...)
engine.On(OnCommand("/cmd3")).Use(middleware.Logging()).Handle(...)

// ✅ 推荐：使用全局中间件
engine.Use(middleware.Logging())
engine.On(OnCommand("/cmd1")).Handle(...)
engine.On(OnCommand("/cmd2")).Handle(...)
engine.On(OnCommand("/cmd3")).Handle(...)
```

### 4. 错误处理

```go
func MyMiddleware() remilia.HandlerMiddleware {
    return func(next remilia.HandlerE) remilia.HandlerE {
        return func(ctx *remilia.Context) error {
            // 前置检查
            if err := validateContext(ctx); err != nil {
                return err  // 短路，不执行后续中间件
            }
            
            // 执行并处理错误
            err := next(ctx)
            if err != nil {
                log.Printf("Handler failed: %v", err)
                // 可以选择：返回错误 或 处理后返回 nil
                return err
            }
            
            return nil
        }
    }
}
```

---

# 中间件执行模型

当前版本中，中间件链不再在每次事件处理时临时拼接，而是在**配置变更时预组合**：

- 全局中间件：`engine.Use(...)` 修改 `globalMiddlewares`；
- 插件中间件：`engine.UseForPlugin(...)` 修改 `pluginMiddlewares`；
- 匹配器局部中间件：`matcher.Use(...)` 修改 `Matcher.middlewares`；
- 匹配器处理函数：`matcher.Handle/HandleE` 修改 `Matcher.Handler/HandlerErr`。

在这些配置变更发生后，Engine 会为每个受影响的 `Matcher` 调用内部的 `rebuildMatcherChain`：

```go
// 伪代码示意
func (e *Engine) rebuildMatcherChain(m *Matcher) {
    globals := e.globalMiddlewares
    plugins := e.pluginMiddlewares[pluginNameFrom(m.Source)]
    locals  := m.middlewares
    m.combinedChain = globals + plugins + locals
}
```

事件处理时，`invokeHandler` 只会基于 `Matcher.combinedChain` 在锁外包裹一次 handler：

```go
func (e *Engine) invokeHandler(ctx *Context, m *Matcher) {
    base := pickBaseHandler(m) // HandlerErr 优先，其次 Handler
    chain := m.combinedChain   // 已在配置变更时组合好的链
    for i := len(chain) - 1; i >= 0; i-- {
        base = chain[i](base)
    }
    _ = base(ctx)
}
```

这样带来的好处是：

- 每个 `Matcher` 的中间件链在配置变更时构建一次即可，多次 `ProcessEvent` 复用；
- 运行时不再频繁读取 `Engine.globalMiddlewares`/`pluginMiddlewares`，降低锁竞争；
- 链结构在 `Matcher` 上是显式字段（`combinedChain`），有利于调试和后续演进；
- `ResetMiddlewares`、`Use`、`UseForPlugin` 与 `Matcher.Use/Handle/HandleE` 都会自动触发受影响 Matcher 的链重建。

> 注意：中间件的执行顺序依然是 **global → plugin → matcher**，行为和旧版本保持一致，只是构建时机从“每次执行时”改为“配置变更时”。

---

## 📚 相关文档

- [使用指南](GUIDE.md) - 完整使用说明
- [架构说明](ARCHITECTURE.md) - 架构设计
- [错误处理](ERROR_HANDLING.md) - 错误处理最佳实践
- [性能优化](PERFORMANCE.md) - 性能测试和优化

---

## 🎉 总结

Remilia 的中间件系统：

- ✅ **三层架构** - Global / Plugin / Matcher
- ✅ **丰富的内置中间件** - 12+ 个开箱即用
- ✅ **易于扩展** - 简单的接口定义
- ✅ **灵活组合** - 支持任意组合
- ✅ **性能优秀** - 洋葱模型，高效执行

通过合理使用中间件，可以实现：
- 🛡️ 统一的错误处理
- 📊 完善的监控和日志
- 🚦 流量控制和限流
- 🔐 权限验证和鉴权
- ⚡ 性能优化和缓存
