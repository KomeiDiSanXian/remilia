# Basic Bot Example

这是一个最简单的 Remilia Bot 示例，展示了框架的基本用法。

## 功能

- ✅ Echo 命令 - 回显用户消息
- ✅ Ping 命令 - 测试连接
- ✅ Help 命令 - 显示帮助
- ✅ Info 命令 - 显示机器人信息
- ✅ Status 命令 - 显示系统状态
- ✅ At 消息处理 - 处理 @ 机器人的消息
- ✅ 私聊处理 - 处理私聊消息

## 运行

### 1. 设置环境变量

```bash
# Linux/macOS
export BOT_SECRET="your-webhook-secret"
export BOT_PORT="8080"

# Windows PowerShell
$env:BOT_SECRET="your-webhook-secret"
$env:BOT_PORT="8080"
```

### 2. 运行程序

```bash
go run -tags example main.go
```

### 3. 配置 QQ 机器人 Webhook

在 QQ 机器人管理后台配置 Webhook 地址：
```
http://your-server:8080/webhook
```

## 使用

在 QQ 群或频道中发送命令：

```
/echo Hello World
/ping
/help
/info
/status
```

或者 @ 机器人：
```
@你的机器人 你好
```

## 代码说明

### 创建 Engine

```go
eng := engine.NewEngine()
```

Engine 是事件处理的核心，负责路由和调度。

### 添加中间件

```go
eng.Use(
    middleware.Logging(),  // 日志记录
    middleware.Recover(),  // Panic 恢复
    middleware.Metrics(),  // 指标收集
)
```

中间件按顺序执行，提供横切关注点。

### 注册处理器

```go
eng.OnCommand("/echo", func(ctx *eventctx.Context) error {
    text := ctx.GetPlainText()
    return ctx.Reply("你说: " + text)
})
```

使用 `OnCommand` 注册命令处理器。

### 创建适配器

```go
adapter := remilia.NewWebhookAdapter(":8080", secret)
```

Webhook 适配器负责接收 QQ 官方的事件推送。

### 启动 Bot

```go
bot := remilia.NewBot(adapter, eng)
bot.Start()
```

Bot 封装了完整的生命周期管理。

## 扩展

### 添加新命令

```go
eng.OnCommand("/mycommand", func(ctx *eventctx.Context) error {
    // 你的处理逻辑
    return ctx.Reply("响应消息")
})
```

### 添加中间件

```go
eng.Use(func(next eventctx.Handler) eventctx.Handler {
    return func(ctx *eventctx.Context) error {
        // 前置处理
        err := next(ctx)
        // 后置处理
        return err
    }
})
```

### 使用配置文件

参考 [config-example](../config-example) 示例。

## 下一步

- 查看 [command-bot](../command-bot) 了解更复杂的命令处理
- 查看 [middleware-example](../middleware-example) 了解中间件使用
- 查看 [plugin-example](../plugin-example) 了解插件开发
