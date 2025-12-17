# 快速开始 - 5分钟搭建你的第一个 QQ 机器人

## 前置要求

1. Go 1.19 或更高版本
2. 已申请的 QQ 机器人（Bot AppID、AppSecret、Token）
3. 配置好的 WebHook 地址

## 第一步：安装依赖

```bash
go get github.com/KomeiDiSanXian/remilia
```

## 第二步：配置机器人信息

创建 `global/global.go` 文件（如果还没有）：

```go
package global

import "github.com/KomeiDiSanXian/remilia/openapi/dto"

var Info = &dto.BotInfo{
    AppID:     "你的AppID",
    AppSecret: "你的AppSecret",
    Token:     "你的Token",
    ServeAddr: ":8080", // WebHook 监听地址
}
```

## 第三步：编写主程序

创建 `main.go`：

```go
package main

import (
    "context"
    "github.com/KomeiDiSanXian/remilia"
    "github.com/KomeiDiSanXian/remilia/global"
    "github.com/KomeiDiSanXian/remilia/openapi/dto"
    "github.com/KomeiDiSanXian/remilia/openapi/protocol/webhook"
    "github.com/sirupsen/logrus"
)

func init() {
    logrus.SetLevel(logrus.InfoLevel)
}

func main() {
    ctx := context.Background()
    engine := remilia.GetGlobalEngine()

    // 全局错误处理（推荐）：通过中间件统一接收 handler 错误和 panic
    engine.Use(
        middleware.Recover(engine), // Panic 恢复（生产环境推荐开启）
        middleware.ErrorHandler(func(c *remilia.Context, err error) {
            logrus.WithError(err).Error("handler failed")
        }),
    )

    // 1. 注册"你好"命令（v1.2.0 链式 API）
    engine.On(remilia.OnGroupAtMessage()).
        FullMatch("你好").
        Handle(func(ctx *remilia.Context) {
            _, _ = ctx.ReplyGroup(&dto.Message{
                Content: "你好！我是机器人 👋",
                Type:    dto.TextMessage,
            })
        })

    // 2. 注册 /help 命令
    engine.On(remilia.OnGroupAtMessage()).
        Command("/help").
        HandleE(func(ctx *remilia.Context) error { // 使用返回值处理器，错误会进入错误处理中间件
            _, err := ctx.ReplyGroup(&dto.Message{
                Content: "📖 帮助信息\n\n可用命令：\n/help - 显示帮助\n/ping - 测试响应\n你好 - 打招呼",
                Type:    dto.TextMessage,
            })
            return err
        })

    // 3. 注册 /ping 命令
    engine.On(remilia.OnGroupAtMessage()).
        Command("/ping").
        Handle(func(ctx *remilia.Context) {
            if _, err := ctx.ReplyGroup(&dto.Message{
                Content: "🏓 Pong!",
                Type:    dto.TextMessage,
            }); err != nil {
                logrus.WithError(err).Warn("reply /ping failed")
            }
        })

    // 4. 启动机器人
    bot := remilia.New(global.Info, remilia.WithWebHook(webhook.New(ctx, global.Info)))

    // 最简热重载用例：监听 config.yaml，动态调整日志级别（可选）
    // 仅示例：生产环境建议在此基础上扩展需要热应用的配置项
    if cfg, err := config.LoadDefault(); err == nil {
        if lvl, err := logrus.ParseLevel(cfg.Log.Level); err == nil { logrus.SetLevel(lvl) }
        if stop, err := config.Watch("config.yaml", func(n *config.Config) {
            if lvl, err := logrus.ParseLevel(n.Log.Level); err == nil {
                logrus.SetLevel(lvl)
                logrus.WithField("level", n.Log.Level).Info("log level hot-reloaded")
            }
        }); err == nil {
            defer stop()
        }
    }

    bot.Run()
}
```

## 第四步：运行

```bash
go run main.go
```

看到以下输出表示启动成功：

```
INFO[0000] [Remilia] Starting bot with webhook
INFO[0000] [Remilia] Bot is running
```

## 第五步：测试

1. 在 QQ 群中 @ 你的机器人
2. 发送 "你好"，机器人会回复 "你好！我是机器人 👋"
3. 发送 "/help"，机器人会显示帮助信息
4. 发送 "/ping"，机器人会回复 "🏓 Pong!"

## 常见命令模式

### 1. 完全匹配

```go
engine.On(remilia.OnGroupAtMessage()).
    FullMatch("具体内容").
    Handle(func(ctx *remilia.Context) {
        if _, err := ctx.ReplyGroup(&dto.Message{Content: "OK", Type: dto.TextMessage}); err != nil {
            // 记录/处理错误
        }
    })
```

### 2. 命令匹配（带参数）

```go
engine.On(remilia.OnGroupAtMessage()).
    Command("/echo").
    Handle(func(ctx *remilia.Context) {
        content := ctx.GetMessageContent()
        text := strings.TrimPrefix(content, "/echo ")
        if _, err := ctx.ReplyGroup(&dto.Message{Content: text, Type: dto.TextMessage}); err != nil {
            // 记录/处理错误
        }
    })
```

### 3. 关键字匹配

```go
engine.On(remilia.OnGroupAtMessage()).
    Keyword("天气").
    HandleE(func(ctx *remilia.Context) error { // 示例：使用返回值处理器
        _, err := ctx.ReplyGroup(&dto.Message{Content: "今天天气不错！☀️", Type: dto.TextMessage})
        return err
    })
```

### 4. 多条件组合

```go
engine.On(remilia.OnGroupAtMessage()).
    Where(remilia.Or(
        remilia.OnKeyword("你好"),
        remilia.OnKeyword("hello"),
        remilia.OnKeyword("hi"),
    )).
    Handle(handler)
```

## 添加私聊支持

```go
// 处理私聊消息
engine.On(remilia.OnC2CMessage()).
    Handle(func(ctx *remilia.Context) {
        content := ctx.GetMessageContent()
        _, _ = ctx.ReplyPrivate(&dto.Message{
            Content: "收到你的消息：" + content,
            Type:    dto.TextMessage,
        })
    })
```

## 添加机器人加入群聊欢迎

```go
engine.On(remilia.OnGroupAddRobot()).
    Handle(func(ctx *remilia.Context) {
        var event dto.GroupAddRobotEvent
        _ = ctx.DecodeEvent(&event)
        if _, err := ctx.SendGroupMessage(event.GroupOpenID, &dto.Message{
            Content: "👋 大家好！我是新来的机器人！",
            Type:    dto.TextMessage,
        }); err != nil {
            // 记录/处理错误
        }
    })
```

## 下一步

现在你已经有了一个可以工作的机器人！接下来可以：

1. 📚 阅读 [GUIDE.md](./GUIDE.md) 了解更多功能
2. 🏗️ 阅读 [ARCHITECTURE.md](./ARCHITECTURE.md) 了解架构设计
3. 🔌 查看 [example/plugins/](./example/plugins/) 学习如何编写插件
4. 💡 阅读 [ERROR_HANDLING.md](./ERROR_HANDLING.md) 了解错误处理最佳实践

## 常见问题

### Q: 机器人没有响应？

A: 检查以下几点：
1. WebHook 地址是否正确配置在 QQ 开放平台
2. 机器人是否有权限接收消息（需要在开放平台配置）
3. 是否正确 @ 了机器人（群聊需要@才能触发）

### Q: 如何调试？

A: 设置日志级别为 Debug 或 Trace：

```go
func init() {
    logrus.SetLevel(logrus.DebugLevel) // 或 logrus.TraceLevel
}
```

### Q: 如何部署到生产环境？

A: 
1. 确保 WebHook 地址可以从公网访问
2. 使用 HTTPS（QQ 要求）
3. 建议使用反向代理（如 Nginx）
4. 使用进程管理器（如 systemd、supervisor）

## 示例：一个简单的计算器机器人

```go
package main

import (
    "context"
    "fmt"
    "strconv"
    "strings"
    
    "github.com/KomeiDiSanXian/remilia"
    "github.com/KomeiDiSanXian/remilia/global"
    "github.com/KomeiDiSanXian/remilia/openapi/dto"
    "github.com/KomeiDiSanXian/remilia/openapi/protocol/webhook"
    "github.com/sirupsen/logrus"
)

func main() {
    ctx := context.Background()
    engine := remilia.GetGlobalEngine()

    // 加法：/add 1 2
    engine.On(remilia.OnGroupAtMessage(), remilia.OnCommand("/add ")).
        Handle(func(ctx *remilia.Context) {
            content := ctx.GetMessageContent()
            parts := strings.Fields(strings.TrimPrefix(content, "/add "))
            
            if len(parts) != 2 {
                _, _ = ctx.ReplyGroup(&dto.Message{Content: "❌ 用法：/add 数字1 数字2", Type: dto.TextMessage})
                return
            }
            
            a, err1 := strconv.ParseFloat(parts[0], 64)
            b, err2 := strconv.ParseFloat(parts[1], 64)
            
            if err1 != nil || err2 != nil {
                _, _ = ctx.ReplyGroup(&dto.Message{Content: "❌ 请输入有效的数字", Type: dto.TextMessage})
                return
            }
            
            result := a + b
            if _, err := ctx.ReplyGroup(&dto.Message{Content: fmt.Sprintf("✅ %g + %g = %g", a, b, result), Type: dto.TextMessage}); err != nil {
                logrus.WithError(err).Warn("reply add failed")
            }
        })

    bot := remilia.New(global.Info, remilia.WithWebHook(webhook.New(ctx, global.Info)))
    bot.Run()
}
```

现在你可以在群里 @ 机器人发送 "/add 1 2"，它会回复 "✅ 1 + 2 = 3"！

---

🎉 恭喜！你已经完成了第一个 QQ 机器人的开发！
