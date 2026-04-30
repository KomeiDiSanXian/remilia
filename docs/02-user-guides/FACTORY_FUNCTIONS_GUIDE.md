# 工厂函数简化指南

**最后更新**: 2026年4月30日

本文档说明Remilia框架中简化的工厂函数使用方法。

---

## 📋 目录

1. [Bot创建](#bot创建)
2. [中间件使用](#中间件使用)
3. [适配器创建](#适配器创建)
4. [迁移指南](#迁移指南)

---

## 🤖 Bot创建

### 方式1: 使用Builder模式（推荐）⭐

最灵活且清晰的方式：

```go
adapter := qq.NewWebhookServerAdapter(":8080", botInfo)
bot, err := remilia.NewBotBuilder().
    WithPlatformAdapter(adapter).
    WithName("my-bot").
    WithDebug(true).
    Build()
if err != nil {
    log.Fatal(err)
}
```

如果确信配置正确，可以使用`MustBuild()`:

```go
adapter := qq.NewWebhookServerAdapter(":8080", botInfo)
bot := remilia.NewBotBuilder().
    WithPlatformAdapter(adapter).
    MustBuild() // 配置错误会panic
```

### 方式2: 直接使用工厂函数

#### 使用 Builder（推荐）

```go
adapter := qq.NewWebhookServerAdapter(":8080", nil)
eng := engine.NewEngine()
bot, err := remilia.NewBotBuilder().
    WithPlatformAdapter(adapter).
    WithEngine(eng).
    Build()
```

#### 使用 NewBot（简单场景）

```go
adapter := qq.NewWebhookServerAdapter(":8080", botInfo)
eng := engine.NewEngine()
bot, err := remilia.NewBot(adapter, eng)
```

### 对比说明

| 方式 | 优点 | 缺点 | 适用场景 |
|------|------|------|----------|
| Builder | 最清晰，最灵活 | 略微冗长 | 生产环境，复杂配置 |
| NewBot | 简单直接 | 扩展性有限 | 简单场景 |

---

## 🔌 中间件使用

### 方式1: 使用预定义中间件集（推荐）⭐

#### 生产环境

```go
eng.Use(middleware.ProductionSet()...)
```

包含：Recover + Logging + Adaptive + CircuitBreaker + Dedup

#### 开发环境

```go
eng.Use(middleware.DevelopmentSet()...)
```

#### 测试环境

```go
eng.Use(middleware.BasicSet()...)
```

仅包含：Recover

### 方式2: 使用简化工厂函数

```go
engine.Use(
    middleware.Recover(),           // panic恢复
    middleware.Logging(),           // 日志
    middleware.SimpleAdaptive(),    // 自适应限流（默认配置）
    middleware.SimpleCircuitBreaker(), // 熔断器（默认配置）
    middleware.SimpleDedup(),       // 去重（默认配置）
)
```

#### 带参数的简化工厂

```go
engine.Use(
    middleware.SimpleAdaptiveWithLimit(200),     // 最大200并发
    middleware.SimpleDedupWithTTL(5*time.Minute), // 5分钟去重
)
```

### 方式3: 使用Builder模式

```go
middlewares := middleware.NewMiddlewareSet().
    WithRecover().
    WithLogging().
    WithAdaptive().
    WithCircuitBreaker().
    Build()

engine.Use(middlewares...)
```

### 方式4: 完全自定义配置

```go
config := middleware.AdaptiveConfig{
    MinConcurrency: 10,
    MaxConcurrency: 500,
    InitialLimit:   100,
    TargetCPU:      0.70,
    // ... 其他配置
}
arl := middleware.NewAdaptiveRateLimiter(config)
arl.Start()
engine.Use(arl.Middleware())
```

### 对比说明

| 方式 | 简洁度 | 灵活度 | 适用场景 |
|------|--------|--------|----------|
| 预定义集 | ⭐⭐⭐⭐⭐ | ⭐⭐ | 快速开始，标准场景 |
| 简化工厂 | ⭐⭐⭐⭐ | ⭐⭐⭐ | 需要微调个别参数 |
| Builder | ⭐⭐⭐ | ⭐⭐⭐⭐ | 需要组合多个中间件 |
| 完全自定义 | ⭐⭐ | ⭐⭐⭐⭐⭐ | 需要精确控制所有参数 |

---

## 🌐 适配器创建

### Webhook适配器

#### 方式1: 最简单（无botInfo）

```go
adapter := qq.SimpleWebhookAdapter(8080)
```

#### 方式2: 带botInfo

```go
adapter := qq.NewWebhookServerAdapter(":8080", botInfo)
```

#### 方式3: 完全自定义

```go
adapter := qq.NewWebhookServerAdapter(":8080", botInfo)
```

---

## 🔄 迁移指南

### 从复杂工厂迁移到简化工厂

#### 之前（复杂）

```go
// 创建中间件（需要了解所有配置参数）
cfg := middleware.AdaptiveConfig{
    MinConcurrency: 10,
    MaxConcurrency: 1000,
    InitialLimit:   100,
    TargetCPU:      0.70,
    TargetMemory:   0.80,
    TargetLatency:  500 * time.Millisecond,
    AdjustInterval: 10 * time.Second,
    AdjustStep:     10,
    CooldownPeriod: 30 * time.Second,
    SampleWindow:   60 * time.Second,
    MetricsEnabled: true,
}
arl := middleware.NewAdaptiveRateLimiter(cfg)
arl.Start()

// 创建Bot（参数较多）
adapter := qq.NewWebhookServerAdapter(":8080", botInfo)
eng := engine.NewEngine()
bot, err := remilia.NewBot(adapter, eng)
```

#### 之后（简化）

```go
// 使用预定义中间件集
eng.Use(middleware.ProductionSet()...)

// 使用Builder创建Bot
adapter := qq.NewWebhookServerAdapter(":8080", botInfo)
bot := remilia.NewBotBuilder().
    WithPlatformAdapter(adapter).
    MustBuild()
```

#### 之后（简化）

```go
// 使用预定义中间件集
engine.Use(middleware.ProductionSet()...)

// 使用Builder创建Bot
bot := remilia.NewBotBuilder().
    WithBotInfo(botInfo).
    WithWebhook(":8080").
    MustBuild()
```

**代码减少**: ~20行 → ~5行（75%减少）

---

## 📊 快速参考

### 最常用的工厂函数

| 功能 | 推荐工厂 | 示例 |
|------|----------|------|
| 创建Bot | `NewBotBuilder()` | `NewBotBuilder().WithPlatformAdapter(adapter).Build()` |
| 中间件集 | `ProductionSet()` | `eng.Use(middleware.ProductionSet()...)` |
| 自适应限流 | `SimpleAdaptive()` | `eng.Use(middleware.SimpleAdaptive())` |
| 熔断器 | `SimpleCircuitBreaker()` | `eng.Use(middleware.SimpleCircuitBreaker())` |
| 去重 | `SimpleDedup()` | `eng.Use(middleware.SimpleDedup())` |
| Webhook (QQ) | `qq.NewWebhookServerAdapter()` | `qq.NewWebhookServerAdapter(":8080", botInfo)` |

---

## ✨ 最佳实践

### 1. 快速原型开发

```go
func main() {
    adapter := qq.SimpleWebhookAdapter(8080)
    bot := remilia.NewBotBuilder().
        WithPlatformAdapter(adapter).
        MustBuild()
    
    bot.Engine().Use(middleware.DevelopmentSet()...)
    
    // 注册处理器
    bot.Engine().OnMessage(func(ctx *eventctx.Context) error {
        return ctx.Reply(platform.TextMessage("Hello!"))
    })
    
    bot.Start()
    bot.WaitForShutdown()
}
```

### 2. 生产环境

```go
func main() {
    // 加载配置
    botInfo := &dto.BotInfo{
        AppID:  123456,
        BotID:  654321,
        Token:  "your-token",
        Secret: "your-secret",
    }
    
    // 创建Bot
    adapter := qq.NewWebhookServerAdapter(":8080", botInfo)
    bot := remilia.NewBotBuilder().
        WithPlatformAdapter(adapter).
        WithName("production-bot").
        MustBuild()
    
    // 使用生产中间件集
    bot.Engine().Use(middleware.ProductionSet()...)
    
    // 注册处理器
    setupHandlers(bot.Engine())
    
    // 启动
    if err := bot.Start(); err != nil {
        log.Fatal(err)
    }
    bot.WaitForShutdown()
}
```

### 3. 自定义配置

```go
func main() {
    // 使用Builder + 自定义中间件
    adapter := qq.NewWebhookServerAdapter(":8080", botInfo)
    bot := remilia.NewBotBuilder().
        WithPlatformAdapter(adapter).
        Build()
    
    // 自定义中间件组合
    bot.Engine().Use(
        middleware.Recover(),
        middleware.Logging(),
        middleware.SimpleAdaptiveWithLimit(500), // 自定义限制
        middleware.SimpleDedupWithTTL(10*time.Minute), // 自定义TTL
    )
    
    bot.Start()
    bot.WaitForShutdown()
}
```

---

## 🆘 常见问题

### Q: Builder和直接工厂函数有什么区别？

**A:** Builder更灵活和清晰，适合生产环境；直接工厂函数更简单，适合简单场景。

### Q: 什么时候使用预定义中间件集？

**A:** 大多数情况下使用预定义集就够了：
- `ProductionSet()` - 生产环境
- `DevelopmentSet()` - 开发环境
- `BasicSet()` - 测试环境

只有需要特殊配置时才使用自定义。

### Q: SimpleWebhookAdapter和NewWebhookServerAdapter有什么区别？

**A:** 
- `SimpleWebhookAdapter(port)` - 最简单，不支持主动API调用
- `NewWebhookServerAdapter(addr, botInfo)` - 完整功能，支持API调用

### Q: 如何知道使用哪个工厂函数？

**A:** 遵循这个原则：
1. 优先使用最简单的（Simple*、*Set）
2. 如果需要自定义，使用Builder
3. 如果需要完全控制，使用完整配置

---

## 📚 相关文档

- [快速开始](../01-getting-started/GETTING_STARTED.md)
- [最佳实践](./BEST_PRACTICES.md)
- [架构设计](../03-architecture/)

---

**最后更新**: 2026年2月5日  
**适用版本**: v1.0.0+
