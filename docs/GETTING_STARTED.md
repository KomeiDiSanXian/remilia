# Remilia 快速上手指南

欢迎使用 Remilia！本指南将帮助你在 10 分钟内创建并运行你的第一个 QQ 机器人。

## 📋 前置要求

- Go 1.19 或更高版本
- QQ 机器人账号（从 [QQ 开放平台](https://bot.q.qq.com/) 申请）
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
)

func main() {
    // 创建 Engine
    eng := engine.NewEngine()
    
    // 添加中间件
    eng.Use(
        middleware.Logging(),
        middleware.Recover(),
    )
    
    // 注册命令处理器
    eng.OnCommand("/hello", func(ctx *eventctx.Context) error {
        return ctx.Reply("Hello, World!")
    })
    
    // 创建 Webhook 适配器
    adapter := remilia.NewWebhookAdapter(":8080", "your-webhook-secret")
    
    // 创建并启动 Bot
    bot := remilia.NewBot(adapter, eng)
    bot.Start()
    bot.WaitForShutdown()
}
```

### 步骤 4: 运行

```bash
go run main.go
```

### 步骤 5: 配置 Webhook

在 QQ 机器人管理后台配置 Webhook 地址：
```
http://your-server:8080/webhook
```

## 🎯 核心概念

### Engine (事件引擎)

Engine 是 Remilia 的核心，负责接收事件、路由和调度处理器。

```go
eng := engine.NewEngine()
```

### Context (上下文)

Context 提供了访问事件数据和回复消息的方法。

```go
func handler(ctx *eventctx.Context) error {
    // 获取消息内容
    text := ctx.GetPlainText()
    
    // 获取发送者
    author := ctx.GetAuthor()
    
    // 回复消息
    return ctx.Reply("Hello!")
}
```

### Matcher (匹配器)

Matcher 定义了事件的匹配规则。

```go
// 匹配命令
eng.OnCommand("/hello", handler)

// 匹配被 @ 的消息
eng.OnAtMessage(handler)

// 匹配所有消息
eng.OnMessage(handler)
```

### Middleware (中间件)

中间件在处理器执行前后提供横切功能。

```go
eng.Use(middleware.Logging())  // 日志
eng.Use(middleware.Recover())  // Panic 恢复
```

### Adapter (适配器)

适配器负责与 QQ 官方 API 通信。

```go
// Webhook 模式
adapter := remilia.NewWebhookAdapter(":8080", "secret")

// WebSocket 模式（计划中）
// adapter := remilia.NewWebSocketAdapter(config)
```

### Bot (机器人)

Bot 封装了 Engine 和 Adapter，提供完整的生命周期管理。

```go
bot := remilia.NewBot(adapter, eng)
bot.Start()       // 启动
bot.Shutdown()    // 关闭
```

## 📝 完整示例

### 功能丰富的 Bot

```go
package main

import (
    "fmt"
    "time"

    "github.com/KomeiDiSanXian/remilia"
    eventctx "github.com/KomeiDiSanXian/remilia/core/context"
    "github.com/KomeiDiSanXian/remilia/core/engine"
    "github.com/KomeiDiSanXian/remilia/middleware"
    "github.com/sirupsen/logrus"
)

func main() {
    // 设置日志级别
    logrus.SetLevel(logrus.InfoLevel)

    // 创建 Engine
    eng := engine.NewEngine()

    // 添加中间件
    eng.Use(
        middleware.Logging(),                     // 日志
        middleware.Recover(),                     // Panic 恢复
        middleware.ConcurrencyLimit(100, ...),    // 并发限制
        middleware.Timeout(5*time.Second),        // 超时控制
    )

    // === 注册命令 ===

    // 1. Echo 命令
    eng.OnCommand("/echo", func(ctx *eventctx.Context) error {
        text := ctx.GetPlainText()
        return ctx.Reply("你说: " + text)
    })

    // 2. 时间命令
    eng.OnCommand("/time", func(ctx *eventctx.Context) error {
        now := time.Now().Format("2006-01-02 15:04:05")
        return ctx.Reply("当前时间: " + now)
    })

    // 3. 帮助命令
    eng.OnCommand("/help", func(ctx *eventctx.Context) error {
        help := `可用命令:
/echo <text> - 回显文本
/time - 显示当前时间
/help - 显示此帮助
/ping - 测试连接`
        return ctx.Reply(help)
    })

    // 4. Ping 命令
    eng.OnCommand("/ping", func(ctx *eventctx.Context) error {
        return ctx.Reply("Pong! 🏓")
    })

    // === 事件处理器 ===

    // At 消息处理
    eng.OnAtMessage(func(ctx *eventctx.Context) error {
        text := ctx.GetPlainText()
        return ctx.Reply(fmt.Sprintf("收到 @ 消息: %s", text))
    })

    // 私聊消息处理
    eng.OnDirectMessage(func(ctx *eventctx.Context) error {
        return ctx.Reply("你好！发送 /help 查看命令列表")
    })

    // === 启动 Bot ===

    adapter := remilia.NewWebhookAdapter(":8080", "your-secret")
    bot := remilia.NewBot(adapter, eng)

    logrus.Info("Bot starting...")
    if err := bot.Start(); err != nil {
        logrus.Fatal(err)
    }

    logrus.Info("Bot is running. Press Ctrl+C to stop.")
    bot.WaitForShutdown()
}
```

## 🔧 使用配置文件

### 创建配置文件

`config.yaml`:
```yaml
bot:
  app_id: 123456
  bot_id: 654321
  token: "your-token"
  secret: "your-secret"

server:
  host: "0.0.0.0"
  port: 8080

middleware:
  logging: true
  recover: true
  concurrency_limit: 100
```

### 加载配置

```go
package main

import (
    "github.com/KomeiDiSanXian/remilia"
    "github.com/KomeiDiSanXian/remilia/config"
)

func main() {
    // 加载配置
    cfg, err := config.Load("config.yaml")
    if err != nil {
        panic(err)
    }

    // 使用配置创建 Bot
    bot, err := remilia.NewBotFromConfig(cfg)
    if err != nil {
        panic(err)
    }

    bot.Start()
    bot.WaitForShutdown()
}
```

## 🎨 添加中间件

### 内置中间件

```go
import "github.com/KomeiDiSanXian/remilia/middleware"

// 基础中间件
eng.Use(middleware.Logging())   // 日志
eng.Use(middleware.Recover())   // Panic 恢复
eng.Use(middleware.RequestID()) // 请求 ID

// 流量控制
eng.Use(middleware.ConcurrencyLimit(100, ...))
eng.Use(middleware.Timeout(5*time.Second))

// 可靠性
eng.Use(middleware.Retry(3))
eng.Use(middleware.CircuitBreaker(5, 30*time.Second))

// 自适应限流
config := middleware.DefaultAdaptiveConfig()
limiter := middleware.NewAdaptiveRateLimiter(config)
limiter.Start()
eng.Use(limiter.Middleware())
```

### 自定义中间件

```go
func MyMiddleware() eventctx.Middleware {
    return func(next eventctx.Handler) eventctx.Handler {
        return func(ctx *eventctx.Context) error {
            // 前置处理
            log.Println("Before")
            
            // 执行处理器
            err := next(ctx)
            
            // 后置处理
            log.Println("After")
            
            return err
        }
    }
}

eng.Use(MyMiddleware())
```

## 🔌 使用插件

### 创建插件

```go
package main

import (
    "github.com/KomeiDiSanXian/remilia/plugin"
    "github.com/KomeiDiSanXian/remilia/core/engine"
    eventctx "github.com/KomeiDiSanXian/remilia/core/context"
)

type MyPlugin struct {
    *plugin.BasePlugin
}

func NewMyPlugin() *MyPlugin {
    return &MyPlugin{
        BasePlugin: plugin.NewBasePlugin("my-plugin"),
    }
}

func (p *MyPlugin) Load(eng *engine.Engine) error {
    matcher := engine.NewMatcher().
        OnCommand("/myplugin").
        SetHandler(p.handleCommand)
    
    p.AddMatcher(matcher)
    eng.RegisterMatcher(matcher)
    
    return nil
}

func (p *MyPlugin) handleCommand(ctx *eventctx.Context) error {
    return ctx.Reply("My Plugin is working!")
}
```

### 注册插件

```go
// 创建插件管理器
manager := plugin.NewManager(eng)

// 注册插件
myPlugin := NewMyPlugin()
manager.Register(myPlugin)
```

## 📚 下一步

### 学习更多

- [命令系统](../command/README.md) - 高级命令处理
- [中间件指南](./MIDDLEWARE.md) - 深入了解中间件
- [插件开发](./PLUGIN.md) - 开发可复用的插件
- [配置管理](./CONFIGURATION.md) - 配置文件和热更新

### 查看示例

- [基础 Bot](../examples/basic-bot) - 最简单的示例
- [命令 Bot](../examples/command-bot) - 命令系统示例
- [插件示例](../examples/plugin-example) - 插件开发示例
- [中间件示例](../examples/middleware-example) - 中间件使用示例

### 参考文档

- [API 文档](https://pkg.go.dev/github.com/KomeiDiSanXian/remilia)
- [QQ 机器人文档](https://bot.q.qq.com/wiki/)

## ❓ 常见问题

### 1. 如何部署到服务器？

```bash
# 编译
go build -o bot main.go

# 运行
./bot

# 使用 systemd 或 supervisor 管理进程
```

### 2. 如何处理图片消息？

```go
eng.OnMessage(func(ctx *eventctx.Context) error {
    // 获取消息内容（包括图片）
    content := ctx.GetContent()
    
    // 检查是否包含图片
    if hasImage(content) {
        return ctx.Reply("收到图片消息")
    }
    
    return nil
})
```

### 3. 如何实现定时任务？

```go
// 在单独的 goroutine 中运行定时任务
go func() {
    ticker := time.NewTicker(1 * time.Hour)
    defer ticker.Stop()
    
    for range ticker.C {
        // 执行定时任务
        sendScheduledMessage()
    }
}()
```

### 4. 如何存储数据？

```go
// 使用数据库或缓存
import "database/sql"

db, _ := sql.Open("mysql", "...")

eng.OnCommand("/save", func(ctx *eventctx.Context) error {
    data := ctx.GetPlainText()
    
    // 存储到数据库
    _, err := db.Exec("INSERT INTO data VALUES (?)", data)
    if err != nil {
        return ctx.Reply("保存失败")
    }
    
    return ctx.Reply("保存成功")
})
```

## 🆘 获取帮助

- GitHub Issues: https://github.com/KomeiDiSanXian/remilia/issues
- 文档: https://github.com/KomeiDiSanXian/remilia/docs

---

**祝你使用愉快！** 🎉
