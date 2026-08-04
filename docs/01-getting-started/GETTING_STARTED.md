# Remilia 快速上手指南

> **最后更新**: 2026-08-04

欢迎使用 Remilia！本指南将帮助你在 10 分钟内创建并运行你的第一个机器人。

## 📋 前置要求

- Go 1.26 或更高版本
- 一个机器人平台账号（如 [QQ 开放平台](https://bot.q.qq.com/)）
- 基础的 Go 语言知识

## 🚀 5 分钟快速开始

### 步骤 1: 安装 Remilia

```bash
go get github.com/KomeiDiSanXian/remilia
```

### 步骤 2: 创建项目

```bash
mkdir my-bot
cd my-bot
go mod init my-bot
go get github.com/KomeiDiSanXian/remilia
```

### 步骤 3: 编写代码

创建 `main.go`:

```go
package main

import (
    "github.com/KomeiDiSanXian/remilia"
    eventctx "github.com/KomeiDiSanXian/remilia/core/context"
    "github.com/KomeiDiSanXian/remilia/core/engine"
    "github.com/KomeiDiSanXian/remilia/middleware"
    "github.com/KomeiDiSanXian/remilia/platform"
    "github.com/KomeiDiSanXian/remilia/platform/qq"
    "github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
)

func main() {
    // 创建 Engine
    eng := engine.NewEngine()

    // 添加中间件
    eng.Use(
        middleware.Logging(),
        middleware.Recover(),
    )

    // 注册命令处理器。
    // 事件类型参数是 string：请使用 eventctx.EventGroup 等常量
    // （等价于 string(platform.EventKindGroupMessage)）；
    // 若想直接用 platform.EventKindXxx 常量，请改用 eng.OnEventKind。
    eng.OnCommand(eventctx.EventGroup, "/hello").
        Handle(func(ctx *eventctx.Context) error {
            // Reply 是异步的，返回 Future；忽略返回值即“发出即忘”
            ctx.Reply(platform.TextMessage("Hello, World!"))
            return nil
        })

    // 创建 QQ Webhook 适配器
    botInfo := &dto.BotInfo{
        AppID:     123456,
        Token:     "your-token",
        AppSecret: "your-secret",
    }
    adapter := qq.NewWebhookServerAdapter(":8080", botInfo)

    // 使用 Builder 创建并启动 Bot
    bot, err := remilia.NewBotBuilder().
        WithPlatformAdapter(adapter).
        WithEngine(eng).
        Build()
    if err != nil {
        panic(err)
    }
    bot.Start()
    bot.WaitForShutdown()
}
```

### 步骤 4: 运行

```bash
go run main.go
```

### 步骤 5: 配置平台 Webhook

在对应机器人管理后台配置 Webhook 地址（以 QQ 为例）：
```
http://your-server:8080/webhook
```

---

## 🎯 核心概念

### Engine（事件引擎）

Engine 是 Remilia 的核心，负责接收事件、路由和调度处理器。
内部采用 Copy-on-Write 模式实现高性能无锁读取。

```go
eng := engine.NewEngine()

// 可选：限制 Matcher 上限（防止恶意注册）
eng := engine.NewEngine(engine.WithMaxMatchers(5000))
```

### Context（上下文）

Context 提供访问事件数据和回复消息的方法。

```go
func handler(ctx *eventctx.Context) error {
    text := ctx.GetMessageContent()     // 消息内容
    sender := ctx.GetSenderInfo()       // 发送者信息

    // 存储自定义数据
    ctx.Set("key", "value")
    val, ok := ctx.Get("key")

    // 删除数据（注意：ctx.Set(key, nil) 是 no-op！）
    ctx.Delete("key")

    ctx.ReplyText("Hello!")
    return nil
}
```

### Matcher（匹配器）

Matcher 定义了事件的匹配规则和处理器。

```go
// 命令匹配（自动 O(1) 分发索引）
eng.OnCommand(eventctx.EventGroup, "/ping").
    Handle(handler)

// 事件类型匹配 + 额外规则（私聊）
eng.On(eventctx.EventPrivate, eventctx.OnFullMatch("hello")).
    Handle(handler)

// 平台常量写法：OnEventKind 直接接受 platform.EventKindXxx
eng.OnEventKind(platform.EventKindGroupMessage, eventctx.OnKeyword("签到")).
    Handle(handler)

// 通配（所有事件类型）
eng.OnAny(eventctx.OnRegex(`^\d+$`)).
    Handle(handler)
```

### Middleware（中间件）

中间件在处理器执行前后提供横切功能。

```go
eng.Use(
    middleware.Logging(),                   // 请求日志
    middleware.Recover(),                   // panic → error（自适应堆栈缓冲）
    middleware.Timeout(5*time.Second),      // 超时（context 注入，非 goroutine）
    middleware.Backpressure(100, middleware.BackpressureDrop, 100*time.Millisecond), // 背压
    middleware.SimpleRateLimit(10),         // 全局限流：每秒最多 10 个事件
)
```

> **Timeout 说明**：`Timeout` 通过向 ctx 注入 deadline 实现，
> 不再创建额外 goroutine。Handler 需要监听 `ctx.Context().Done()` 才能被中断；
> panic 不会被 `Timeout` 捕获，需配合 `Recover()` 使用。

### Adapter（适配器）

适配器负责与平台 API 通信。以 QQ 为例：

```go
botInfo := &dto.BotInfo{AppID: 123456, Token: "your-token", AppSecret: "your-secret"}
adapter := qq.NewWebhookServerAdapter(":8080", botInfo)
```

---

## 📝 功能丰富的 Bot 示例

```go
package main

import (
    "fmt"
    "time"

    "github.com/KomeiDiSanXian/remilia"
    eventctx "github.com/KomeiDiSanXian/remilia/core/context"
    "github.com/KomeiDiSanXian/remilia/core/engine"
    "github.com/KomeiDiSanXian/remilia/middleware"
    "github.com/KomeiDiSanXian/remilia/platform"
    "github.com/KomeiDiSanXian/remilia/platform/qq"
    "github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
)

func main() {
    eng := engine.NewEngine()

    // 中间件
    eng.Use(
        middleware.Logging(),
        middleware.Recover(),
        middleware.Timeout(5*time.Second),
        middleware.SimpleRateLimit(20), // 全局每秒 20 个事件
    )

    // 命令注册
    eng.OnCommand(eventctx.EventGroup, "/echo").
        Handle(func(ctx *eventctx.Context) error {
            text := ctx.GetMessageContent()
            ctx.Reply(platform.TextMessage("你说: " + text))
            return nil
        })

    eng.OnCommand(eventctx.EventGroup, "/time").
        Handle(func(ctx *eventctx.Context) error {
            ctx.Reply(platform.TextMessage("当前时间: " + time.Now().Format("2006-01-02 15:04:05")))
            return nil
        })

    eng.OnCommand(eventctx.EventGroup, "/ping").
        Handle(func(ctx *eventctx.Context) error {
            ctx.Reply(platform.TextMessage("Pong! 🏓"))
            return nil
        })

    // 事件处理器
    eng.On(eventctx.EventGroup).
        Handle(func(ctx *eventctx.Context) error {
            ctx.Reply(platform.TextMessage(fmt.Sprintf("收到消息: %s", ctx.GetMessageContent())))
            return nil
        })

    // 创建适配器
    botInfo := &dto.BotInfo{AppID: 123456, Token: "your-token", AppSecret: "your-secret"}
    adapter := qq.NewWebhookServerAdapter(":8080", botInfo)

    // 启动
    bot, err := remilia.NewBotBuilder().
        WithPlatformAdapter(adapter).
        WithEngine(eng).
        Build()
    if err != nil { panic(err) }
    bot.Start()
    bot.WaitForShutdown()
}
```

---

## 🔧 使用配置文件

框架提供 `config` 包加载 YAML 配置，并可从配置节构建平台适配器：

`config.yaml`:
```yaml
bot:
  qq:
    app_id: 123456
    bot_id: 654321
    token: "your-token"
    secret: "your-secret"
    webhook:
      host: "0.0.0.0"
      port: 8080

engine:
  temp_matcher_cleanup_interval: "5m"
  pending_delete_buffer_size: 1000
```

```go
cfg, err := config.Load("config.yaml")
if err != nil { panic(err) }

// 从配置节构建 QQ 适配器（cfg.Bot.QQ 对应 config.yaml 的 bot.qq 节）
q := cfg.Bot.QQ
adapter := qq.NewWebhookServerAdapter(
    fmt.Sprintf("%s:%d", q.Webhook.Host, q.Webhook.Port),
    &dto.BotInfo{AppID: q.AppID, Token: q.Token, AppSecret: q.Secret},
)

bot, err := remilia.NewBotBuilder().
    WithPlatformAdapter(adapter).
    WithEngineOptions(config.EngineOptions(cfg.Engine)...).
    Build()
if err != nil { panic(err) }

bot.Start()
bot.WaitForShutdown()
```

> 完整可运行的示例见仓库 `cmd/bot/`（含多平台适配、插件装配与配置热更新）。

---

## 🎨 中间件速查

```go
import "github.com/KomeiDiSanXian/remilia/middleware"

// 基础
eng.Use(middleware.Logging())
eng.Use(middleware.Recover())     // panic → error（自适应堆栈）
eng.Use(middleware.RequestID())   // 注入 request_id

// 流量控制
eng.Use(middleware.SimpleRateLimit(10))                                                  // 全局限流，每秒 10
eng.Use(middleware.RateLimitTokenBucket(2, 4, keyFunc))                                  // 按 key 限流
eng.Use(middleware.Backpressure(100, middleware.BackpressureDrop, 100*time.Millisecond)) // 背压：超过上限丢弃
eng.Use(middleware.Timeout(5*time.Second))                                               // 超时（context-based）

// 可靠性（子包：middleware/resilience）
eng.Use(resilience.Retry(resilience.RetryConfig{MaxAttempts: 3}))   // 重试（指数退避）
eng.Use(resilience.SimpleCircuitBreaker())                          // 熔断

// 去重（子包：middleware/dedup）
filter := dedup.NewDedupFilter(dedup.DedupConfig{MaxSize: 10000})
eng.Use(dedup.Dedup(filter))

// 自适应限流（子包：middleware/ratelimit）
ctrl := ratelimit.NewManagedAdaptiveWithContext(ctx)
eng.Use(ctrl.Middleware())
```

---

## 🔌 使用插件

```go
package main

import (
    "github.com/KomeiDiSanXian/remilia"
    "github.com/KomeiDiSanXian/remilia/core/engine"
    eventctx "github.com/KomeiDiSanXian/remilia/core/context"
    "github.com/KomeiDiSanXian/remilia/plugin"
    "github.com/KomeiDiSanXian/remilia/platform"
    "github.com/KomeiDiSanXian/remilia/platform/qq"
    "github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
)

// 定义插件
func newMyPlugin() *plugin.Descriptor {
    return &plugin.Descriptor{
        Name:    "my-plugin",
        Version: "1.0.0",
        Meta: &plugin.Metadata{
            Description: "示例插件",
            Category:    "工具",
        },
        Setup: func(ctx *plugin.SetupContext) (any, error) {
            ctx.Reg.RegisterCommand(eventctx.EventGroup, "/myplugin").
                Handle(func(c *eventctx.Context) error {
                    c.Reply(platform.TextMessage("My Plugin is working!"))
                    return nil
                })
            return nil, nil
        },
    }
}

func main() {
    eng := engine.NewEngine()
    manager := plugin.NewManager(eng)

    // 注册插件
    manager.Register(newMyPlugin())

    adapter := qq.NewWebhookServerAdapter(":8080", &dto.BotInfo{AppID: 123456})
    bot, err := remilia.NewBotBuilder().
        WithPlatformAdapter(adapter).
        WithEngine(eng).
        Build()
    if err != nil { panic(err) }

    // 将插件管理器接入 Bot 生命周期（Start/Stop/优雅关闭）
    bot.UsePlugins(manager)

    bot.Start()
    bot.WaitForShutdown()
}
```

---

## 📚 下一步

- [插件开发指南](../06-plugins/PLUGIN_DEVELOPMENT_GUIDE.md)
- [插件最佳实践](../06-plugins/plugin-best-practices.md)
- [配置系统](../02-user-guides/CONFIGURATION_QUICKREF.md)
- [中间件链最佳实践](../02-user-guides/MATCHER_CHAINING_BEST_PRACTICES.md)
- [示例代码](../../examples/)

---

## ❓ 常见问题

### `ctx.Set(key, nil)` 为什么不起作用？

`ctx.Set(key, nil)` 是 no-op（不删除），请使用 `ctx.Delete(key)` 显式删除。

### Timeout 中间件不起作用？

新版 `Timeout` 通过注入 context deadline 实现，Handler 需要调用会检查 `ctx.Context().Done()` 的方法（如网络请求）才能被中断。纯计算密集型 Handler 不受影响。

### 如何使用 panic 恢复？

将 `middleware.Recover()` 放在中间件链**最外层**（第一个 `Use`），确保它能捕获内层中间件和 handler 的 panic。

### 如何部署到服务器？

```bash
go build -o bot main.go
./bot
```

## 🆘 获取帮助

- GitHub Issues: https://github.com/KomeiDiSanXian/remilia/issues
