# Basic Bot Example

这是一个基础的 Remilia Bot 示例，展示了如何使用新的 BotBuilder 创建简单的机器人。

## 功能

- `/echo <消息>` - 回显你的消息
- `/ping` - 测试机器人是否在线
- `/help` - 显示帮助信息

## 快速开始

### 1. 配置

复制配置文件并填入你的机器人信息：

```bash
cp config.example.yaml config.yaml
```

编辑 `config.yaml`，填入你的：
- `app_id` - 你的 AppID
- `bot_id` - 你的机器人 QQ 号
- `token` - 你的 Token
- `secret` - 你的 Secret

### 2. 运行

```bash
go run main.go
```

如果一切正常，你会看到：

```
[BasicBot] Starting bot...
[BasicBot] Bot started successfully! Press Ctrl+C to stop
```

### 3. 测试

向你的机器人发送私聊消息：
- `/ping` - 应该回复 "Pong! 🏓"
- `/echo 你好` - 应该回复 "回声: 你好"
- `/help` - 显示帮助信息

## 代码说明

### 使用 BotBuilder

```go
adapter := qq.NewWebhookServerAdapter(":8080", botInfo)
bot, err := remilia.NewBotBuilder().
    WithPlatformAdapter(adapter).
    WithName("basic-bot").
    Build()
```

### 使用简化中间件

```go
// 开发环境中间件集（Recover + Logging）
bot.Engine().Use(middleware.DevelopmentSet()...)
```

### 注册命令处理器

```go
eng.OnCommand(platform.EventKindC2CMessage, "/echo").Handle(func(ctx *eventctx.Context) error {
    // 处理命令
})
```

## 注意事项

1. **config.yaml 不要提交到 git**
   - 已在 .gitignore 中排除
   - 只提交 config.example.yaml 作为模板

2. **端口配置**
   - 默认监听 8080 端口
   - 确保端口未被占用

3. **日志级别**
   - 开发环境建议使用 `debug` 或 `info`
   - 生产环境建议使用 `info` 或 `warn`

## 下一步

- 查看 [middleware-example](../middleware-example/) 了解中间件使用
- 查看 [command-bot](../command-bot/) 了解命令系统
- 查看 [plugin-example](../plugin-example/) 了解插件开发

## 相关文档

- [快速开始](../../docs/01-getting-started/GETTING_STARTED.md)
- [工厂函数指南](../../docs/02-user-guides/FACTORY_FUNCTIONS_GUIDE.md)
- [最佳实践](../../docs/02-user-guides/BEST_PRACTICES.md)

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
adapter := qq.NewWebhookServerAdapter(":8080", &dto.BotInfo{AppID: 123456})
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
