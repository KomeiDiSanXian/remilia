# Remilia Bot 使用指南

## 概述

Remilia 是一个基于 QQ 官方机器人 API 的 Go 语言框架，支持灵活的事件处理和插件系统。

## 核心概念

### 1. Engine (引擎)

引擎是事件处理的核心，负责接收事件并分发给对应的处理器。

```go
engine := remilia.GetGlobalEngine()
```

### 2. Matcher (匹配器)

Matcher 用于匹配特定的事件和消息，并执行相应的处理函数。

```go
en := remilia.GetGlobalEngine()

// 推荐：显式事件类型 + 规则
engine.OnGroupAt(remilia.OnCommand("/hello")).
    Handle(func(ctx *remilia.Context) {
        if _, err := ctx.ReplyGroup(&dto.Message{Content: "Hello!", Type: dto.TextMessage}); err != nil {
            // 记录/处理错误
        }
    })
```

#### 链式条件匹配 (v1.2.0+)

Remilia 支持链式调用来构建匹配条件，提高代码可读性：

```go
// ✅ 链式调用（推荐）
engine.OnGroupAt().
    Command("/admin").
    Keyword("delete").
    Handle(handler)

// 旧写法（不再推荐，仅用于理解历史代码）
// engine.On(
//     remilia.OnGroupAtMessage(),
//     remilia.OnCommand("/admin"),
//     remilia.OnKeyword("delete"),
// ).Handle(handler)
```

**可用的链式方法**:
- `Command(cmd string)` - 命令匹配
- `Keyword(keyword string)` - 关键词匹配
- `Prefix(prefix string)` - 前缀匹配
- `Suffix(suffix string)` - 后缀匹配
- `FullMatch(text string)` - 完全匹配
- `Regex(pattern string)` - 正则匹配
- `Where(rule Rule)` - 自定义规则

**使用建议**:
- 简单条件：两种方式都可以
- 多个条件：推荐链式调用（可读性更好）
- 复杂逻辑：使用 Rule 组合（`And`/`Or`/`Not`）

#### 多重处理器

**重要特性**: Remilia 允许为同一规则注册多个处理器，所有匹配的处理器都会依次执行。

```go
// ✅ 这是允许且推荐的
// 处理器 1: 记录日志
engine.On(remilia.OnCommand("/ping")).
    Handle(func(ctx *remilia.Context) {
        logrus.Info("收到 ping 命令")
    })

// 处理器 2: 实际响应
engine.On(remilia.OnCommand("/ping")).
    Handle(func(ctx *remilia.Context) {
        ctx.ReplyGroup(&dto.Message{Content: "Pong!", Type: dto.TextMessage})
    })

// 处理器 3: 统计计数
engine.On(remilia.OnCommand("/ping")).
    Handle(func(ctx *remilia.Context) {
        atomic.AddInt64(&pingCount, 1)
    })
```

**适用场景**:
- **关注点分离**: 将日志、响应、统计等逻辑分离到不同的处理器
- **插件共存**: 多个插件可以独立增强同一功能
- **功能扩展**: 基础功能和扩展功能分开实现
- **优先级控制**: 通过 `SetPriority()` 控制执行顺序

**注意事项**:
- 如果任一处理器设置了 `SetBlock(true)`，后续匹配器将不再执行
- 建议使用优先级控制执行顺序（数值越小优先级越高）
- 避免在初始化代码中重复注册（可能导致无意的重复）

### 3. Context (上下文)

Context 包含了事件信息和便捷的消息发送方法。

```go
func handler(ctx *remilia.Context) {
    // 获取消息内容
    content := ctx.GetMessageContent()
    
    // 获取事件类型
    eventType := ctx.GetEventType()
    
    // 回复群聊消息（注意显式处理错误）
    if _, err := ctx.ReplyGroup(&dto.Message{Content: "回复内容", Type: dto.TextMessage}); err != nil {
        // 记录/处理错误
    }
    
    // 回复私聊消息（注意显式处理错误）
    if _, err := ctx.ReplyPrivate(&dto.Message{Content: "回复内容", Type: dto.TextMessage}); err != nil {
        // 记录/处理错误
    }
}
```

### 4. Rule (规则)

Rule 是一个函数，用于判断事件是否满足特定条件。

```go
type Rule func(ctx *Context) bool
```

## 内置规则

#### 事件类型匹配

- `OnC2CMessage()` - 匹配私聊消息（Rule，用于组合逻辑）
- `OnGroupAtMessage()` - 匹配群聊 @ 消息（Rule，用于组合逻辑）
- `OnGroupAddRobot()` - 匹配机器人加入群聊
- `OnGroupDelRobot()` - 匹配机器人退出群聊

> 事件绑定推荐使用引擎方法：`engine.OnC2C(...)`、`engine.OnGroupAt(...)`、`engine.OnGroupAdd(...)`、`engine.OnGroupDel(...)`，而不是将事件 Rule 作为 `engine.On` 的第一个参数。

#### 消息内容匹配

- `OnCommand(prefix)` - 匹配以指定前缀开头的命令
- `OnKeyword(keyword)` - 匹配包含关键词的消息
- `OnFullMatch(text)` - 匹配完全相同的消息
- `OnPrefix(prefix)` - 匹配指定前缀
- `OnSuffix(suffix)` - 匹配指定后缀

### 逻辑组合

- `And(rules...)` - 所有规则都满足
- `Or(rules...)` - 任一规则满足
- `Not(rule)` - 规则不满足

## 快速开始

### 基础示例

```go
package main

import (
    "context"
    "github.com/KomeiDiSanXian/remilia"
    "github.com/KomeiDiSanXian/remilia/global"
    "github.com/KomeiDiSanXian/remilia/openapi/dto"
    "github.com/KomeiDiSanXian/remilia/openapi/protocol/webhook"
)

func main() {
    ctx := context.Background()
    engine := remilia.GetGlobalEngine()

    // 注册一个简单的命令处理器
    engine.OnGroupAt(remilia.OnCommand("/hello")).
        Handle(func(ctx *remilia.Context) {
            _, _ = ctx.ReplyGroup(&dto.Message{Content: "你好！", Type: dto.TextMessage})
        })

    // 启动 Bot
    bot := remilia.New(global.Info, remilia.WithWebHook(webhook.New(ctx, global.Info)))
    bot.Run()
}
```

### 使用中间件

Remilia 提供统一的中间件系统，支持全局、插件级、匹配器级三个层次的中间件。

```go
import (
    "time"
    "github.com/KomeiDiSanXian/remilia"
    "github.com/KomeiDiSanXian/remilia/middleware"
)

// 全局中间件 - 应用于所有匹配器
engine.Use(middleware.Logging())
engine.Use(middleware.Recover(engine))

// 自定义中间件
engine.Use(func(next remilia.HandlerE) remilia.HandlerE {
    return func(ctx *remilia.Context) error {
        start := time.Now()
        logrus.WithField("type", ctx.GetEventType()).Debug("收到事件")
        
        // 执行下一个中间件或处理器
        err := next(ctx)
        
        logrus.WithField("duration", time.Since(start)).Debug("处理完成")
        return err
    }
})

// 插件级中间件 - 仅应用于特定插件的匹配器
engine.UseForPlugin("admin", middleware.Auth(isAdmin))
engine.UseForPlugin("admin", middleware.RateLimitTokenBucket(10, 20, nil))

// 匹配器级中间件 - 仅应用于单个匹配器
engine.On(remilia.OnCommand("/admin")).
    Use(func(next remilia.HandlerE) remilia.HandlerE {
        return func(ctx *remilia.Context) error {
            if !hasPermission(ctx) {
                return remilia.NewBlockError("no permission")
            }
            return next(ctx)
        }
    }).
    HandleE(adminHandler)

// 启用中间件追踪（调试用）
engine.SetTrace(true)
engine.Use(engine.Named("auth", middleware.Auth(isAllowed)))
```

**内置中间件**:
- `middleware.Logging()` - 记录处理耗时和错误
- `middleware.Recover(engine)` - Panic 恢复
- `middleware.Auth(allow func)` - 权限验证
- `middleware.RateLimit(interval)` - 简单限流
- `middleware.RateLimitTokenBucket(rate, burst, keyFn)` - 令牌桶限流
- `middleware.Metrics()` - 性能指标收集
- `middleware.PrometheusMetrics(collector)` - Prometheus 集成
- `middleware.SlowHandler(threshold, onSlow)` - 慢处理监控


### 使用 Matcher 优先级

```go
// 高优先级的命令（数字越小优先级越高）
engine.OnGroupAt(remilia.OnCommand("/admin")).
    SetPriority(10).
    Handle(adminHandler)

// 普通优先级的命令
engine.OnGroupAt(remilia.OnCommand("/help")).
    SetPriority(50).
    Handle(helpHandler)

// 低优先级的通配符处理
engine.OnGroupAt().
    SetPriority(100).
    Handle(defaultHandler)
```

### 阻塞后续匹配

```go
// 设置 IsBlock 为 true，匹配成功后不会继续匹配后续的 Matcher
engine.On(remilia.OnGroupAtMessage()).
    Command("/block").
    SetBlock(true).
    Handle(func(ctx *remilia.Context) {
        _, _ = ctx.ReplyGroup(&dto.Message{Content: "这条消息会阻塞后续匹配", Type: dto.TextMessage})
    })
```

## 错误处理与恢复（推荐配置）

```go
// 统一错误处理（v1.2.0+ 推荐：使用中间件而非 AddErrorHandler）
engine.Use(
    middleware.Recover(engine), // Panic 恢复
    middleware.ErrorHandler(func(ctx *remilia.Context, err error) {
        logrus.WithError(err).Error("handler failed")
    }),
)

// 使用返回值处理器，将错误交给错误处理中间件
engine.On(remilia.OnGroupAtMessage()).
    Command("/hello").
    HandleE(func(ctx *remilia.Context) error {
        _, err := ctx.ReplyGroup(&dto.Message{Content: "Hello!", Type: dto.TextMessage})
        return err
    })
```

## 插件系统

### 创建插件

```go
package plugins

import (
    "github.com/KomeiDiSanXian/remilia"
    "github.com/KomeiDiSanXian/remilia/openapi/dto"
)

type MyPlugin struct {
    *remilia.BasePlugin
}

func NewMyPlugin() *MyPlugin {
    return &MyPlugin{
        BasePlugin: remilia.NewBasePlugin("myplugin"),
    }
}

func (p *MyPlugin) Load(engine *remilia.Engine) {
    // 注册命令处理器
    matcher := engine.On(
        remilia.OnGroupAtMessage(),
        remilia.OnCommand("/mycommand"),
    ).HandleE(func(ctx *remilia.Context) error {
        _, err := ctx.ReplyGroup(&dto.Message{Content: "插件响应", Type: dto.TextMessage})
        return err
    })
    
    // 将 matcher 添加到插件，以便卸载时清理
    p.AddMatcher(matcher)
}
```

### 使用插件管理器

```go
// 创建插件管理器
pluginManager := remilia.NewPluginManager(engine)

// 注册插件
pluginManager.Register(NewMyPlugin())
pluginManager.Register(NewAnotherPlugin())

// 列出所有插件
plugins := pluginManager.List()

// 卸载插件
pluginManager.Unregister("myplugin")
```

## 消息类型

### 文本消息

```go
ctx.ReplyGroup(&dto.Message{
    Content: "这是一条文本消息",
    Type:    dto.TextMessage,
})
```

### Markdown 消息

```go
ctx.ReplyGroup(&dto.Message{
    Type: dto.MarkdownMessage,
    Markdown: &dto.Markdown{
        Content: "# 标题\n**粗体**",
    },
})
```

### 富媒体消息

```go
// 先上传媒体文件
result, err := ctx.api.GroupRichMedia(groupID, &dto.Media{
    Type: dto.ImageFile,
    URL:  "https://example.com/image.jpg",
})

// 发送媒体消息
ctx.ReplyGroup(&dto.Message{
    Type: dto.MediaMessage,
    Media: &dto.Media{
        Type: dto.ImageFile,
        URL:  result.Get("file_info").String(),
    },
})
```

## 高级用法

### 自定义规则

```go
// 创建自定义规则
func OnUserID(userID string) remilia.Rule {
    return func(ctx *remilia.Context) bool {
        author := ctx.GetAuthor()
        return author != nil && author.UserOpenID == userID
    }
}

// 使用自定义规则
engine.OnGroupAt(OnUserID("specific-user-id")).
    Handle(handler)
```

### 状态管理

```go
engine.OnGroupAt(remilia.OnCommand("/start")).
    Handle(func(ctx *remilia.Context) {
        // 在上下文中保存状态
        ctx.State["step"] = 1
        ctx.State["data"] = "some data"
        
        ctx.ReplyGroup(&dto.Message{
            Content: "流程已开始",
            Type:    dto.TextMessage,
        })
    })

engine.OnGroupAt(remilia.OnCommand("/next")).
    Handle(func(ctx *remilia.Context) {
        // 读取状态
        if step, ok := ctx.State["step"].(int); ok {
            ctx.State["step"] = step + 1
        }
    })
```

### 错误处理

```go
engine.OnGroupAt(remilia.OnCommand("/api")).
    Handle(func(ctx *remilia.Context) {
        result, err := ctx.ReplyGroup(&dto.Message{
            Content: "响应内容",
            Type:    dto.TextMessage,
        })
        
        if err != nil {
            logrus.WithError(err).Error("发送消息失败")
            return
        }
        
        // 处理 API 返回结果
        msgID := result.Get("id").String()
        logrus.WithField("msg_id", msgID).Info("消息发送成功")
    })
```

## 最佳实践

1. **使用插件系统组织代码** - 将不同功能封装为独立的插件
2. **合理设置优先级** - 重要的命令使用较高优先级
3. **使用中间件进行通用处理** - 如日志记录、权限检查、频率限制
4. **错误处理** - 始终检查 API 调用的错误返回值
5. **资源清理** - 使用临时 Matcher 时记得清理
6. **日志记录** - 使用 logrus 记录关键操作和错误

## 示例代码

完整示例代码请参考：
- `example/webhook/main_advanced.go` - 高级用法示例
- `example/webhook/main_with_plugins.go` - 插件系统示例
- `example/plugins/example_plugins.go` - 插件实现示例

## 优雅关闭（Graceful Shutdown）

Remilia 支持优雅关闭 Bot：
- 非阻塞启动：`bot.Start()`
- 在收到信号或外部触发时：`bot.Shutdown(ctx)` 关闭 HTTP 服务器与事件循环，并等待后台协程结束

示例：
```go
b := remilia.New(global.Info, remilia.WithWebHook(webhook.New(ctx, global.Info)))
b.Start()
// ... 其他工作 ...
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
b.Shutdown(ctx)
```

`bot.Run()` 内部已处理 SIGINT/SIGTERM 并调用上述流程，无需额外代码。建议使用 5s 以上的超时用于网络连接的关闭。

## 处理器中间件（新）

Remilia 支持对 Handler 进行链式中间件包裹，统一实现日志、鉴权、限流、恢复等通用逻辑。

```go
// 定义中间件：HandlerE -> HandlerE
engine.Use(func(next remilia.HandlerE) remilia.HandlerE {
    return func(ctx *remilia.Context) error {
        start := time.Now()
        err := next(ctx)
        logrus.WithError(err).WithField("latency", time.Since(start)).Info("handler done")
        return err
    }
})

// 适用于 HandleE
engine.OnGroupAt(remilia.OnCommand("/secure")).
    HandleE(func(ctx *remilia.Context) error {
        // 业务逻辑
        return nil
    })

// 兼容 Handle（自动适配为 HandleE）
engine.OnGroupAt(remilia.OnCommand("/hello")).
    Handle(func(ctx *remilia.Context) { /* ... */ })
```

中间件常见模式：
- 日志记录：统计延迟、错误、来源
- 鉴权检查：根据用户/群权限决定是否继续
- 限流/反压：结合 `SetConcurrencyLimit` 控制系统负载
- 审计与指标：统一出口打点
- 恢复与告警：在错误处理器基础上扩展（注意不要吞掉错误）

## 中间件作用域与可视化（新）

支持三种作用域：
- 全局：`engine.Use(...)`，作用于所有 Matcher
- 插件级：`engine.UseForPlugin("myplugin", ...)`，仅作用于该插件注册的 Matcher（`Source = "plugin:myplugin"`）
- 匹配器级：`matcher.Use(...)`，仅作用于该匹配器

执行顺序：`global -> plugin -> matcher`（同级按注册顺序）

启用 trace（执行顺序可视化）：
```go
engine.SetTrace(true)
```

使用内置中间件包：
```go
import m "github.com/KomeiDiSanXian/remilia/middleware"

engine.Use(m.Logging(), m.Metrics())
engine.UseForPlugin("myplugin", m.Auth(func(ctx *remilia.Context) bool { return true }))
matcher := engine.On(remilia.OnGroupAtMessage(), remilia.OnCommand("/hello")).Use(m.RateLimit(100 * time.Millisecond))
matcher.Handle(func(ctx *remilia.Context) { /* ... */ })
```
---
## ����Ȩ��ϵͳ��v0.9.0��
Remilia �ṩ�������� RBAC�����ڽ�ɫ�ķ��ʿ��ƣ�Ȩ��ϵͳ��
### ���ٿ�ʼ
```go
// 1. ����Ȩ�޹�����
pm := remilia.NewPermissionManager()
// 2. ע�뵽 Engine
engine.Use(remilia.RequirePermissionMiddleware(pm))
// 3. �����ɫ
pm.AssignRole("user123", "admin")
// 4. ʹ���м����������
engine.On(remilia.OnGroupAtMessage(), remilia.OnCommand("/admin")).
    Use(remilia.RequireRole("admin")).
    HandleE(func ctx *remilia.Context) error {
        return ctx.ReplyGroup(&dto.Message{
            Content: "����Ա����",
            Type:    dto.TextMessage,
        })
    })
```
### Ĭ�Ͻ�ɫ
- **admin** - ����Ȩ�� (`*:*`)
- **user** - ����ִ�кͲ�ѯȨ��
- **guest** - ����ѯȨ��
### �Զ����ɫ
```go
// �����Զ����ɫ
moderator := remilia.NewRole("moderator",
    remilia.Permission{Resource: "command:*", Action: "execute"},
    remilia.Permission{Resource: "message:*", Action: "delete"},
)
pm.RegisterRole(moderator)
pm.AssignRole("user456", "moderator")
```
### Ȩ���м��
```go
// Ҫ���ض�Ȩ��
engine.On(...).Use(remilia.RequirePermission("message", "delete"))
// Ҫ���ض���ɫ
engine.On(...).Use(remilia.RequireRole("admin"))
// Ҫ������Ȩ�ޣ�OR��
engine.On(...).Use(remilia.RequireAnyPermission(perm1, perm2))
// Ҫ������Ȩ�ޣ�AND��
engine.On(...).Use(remilia.RequireAllPermissions(perm1, perm2))
```
### �ֶ�Ȩ�޼��
```go
engine.On(remilia.OnGroupAtMessage()).HandleE(func ctx *remilia.Context) error {
    if !ctx.HasPermission("admin:users", "manage") {
        return ctx.ReplyGroup(&dto.Message{
            Content: "? Ȩ�޲���",
            Type:    dto.TextMessage,
        })
    }
    // ִ�в���...
    return nil
})
```
### ��ϸ�ĵ�
������Ȩ��ϵͳ�ĵ������ [PERMISSION.md](PERMISSION.md)��
---
**������**: 2025-11-29  
**��ǰ�汾**: v0.9.0
## 最佳实践
### 避免无意的重复注册
虽然 Remilia 允许多次注册同一规则（用于多重处理器），但要避免无意的重复：
#### 1. 只在初始化时注册一次
```go
// ✅ 推荐：在 main 函数或 init 函数中注册
func main() {
    engine := remilia.GetGlobalEngine()
    registerHandlers(engine)  // 只调用一次
    // ...
}
func registerHandlers(engine *remilia.Engine) {
    engine.On(remilia.OnCommand("/ping")).Handle(pingHandler)
    // 其他注册...
}
// ❌ 避免：重复调用初始化函数
registerHandlers(engine)
registerHandlers(engine)  // 会导致处理器重复执行
```
#### 2. 使用插件管理器避免重复加载
```go
// ✅ 插件管理器会检查重复
pluginManager := remilia.NewPluginManager(engine)
pluginManager.Load(myPlugin)  // 第二次加载会被忽略
pluginManager.Load(myPlugin)  // 不会重复注册
// ❌ 直接调用插件的 Load 方法
myPlugin.Load(engine)
myPlugin.Load(engine)  // 会重复注册
```
#### 3. 热重载前清理旧匹配器
```go
func reloadConfig() {
    engine := remilia.GetGlobalEngine()
    // ✅ 清理旧的匹配器
    engine.DeleteAllMatchers()
    // 重新注册
    registerHandlers(engine)
}
```
### 使用优先级控制执行顺序
当使用多重处理器时，建议通过优先级控制执行顺序：
```go
// 前置处理（高优先级）
engine.On(remilia.OnC2CMessage()).
    SetPriority(10).
    Handle(func(ctx *remilia.Context) {
        ctx.SetState("start_time", time.Now())
        logrus.Info("前置处理")
    })
// 主处理器（中等优先级）
engine.On(remilia.OnC2CMessage(), remilia.OnCommand("/hello")).
    SetPriority(50).
    Handle(func(ctx *remilia.Context) {
        ctx.ReplyGroup(&dto.Message{Content: "Hello!"})
    })
// 后置处理（低优先级）
engine.On(remilia.OnC2CMessage()).
    SetPriority(90).
    Handle(func(ctx *remilia.Context) {
        if startTime, ok := ctx.GetState("start_time"); ok {
            logrus.Infof("处理耗时: %v", time.Since(startTime.(time.Time)))
        }
    })
```
### 插件间的协作
多个插件可以为同一命令添加功能：
```go
// 插件 A: 基础统计功能
type StatsPluginBasic struct {
    *remilia.BasePlugin
}
func (p *StatsPluginBasic) Load(engine *remilia.Engine) error {
    matcher := engine.On(remilia.OnCommand("/stats"))
    matcher.Handle(func(ctx *remilia.Context) {
        // 显示基础统计
        ctx.ReplyGroup(&dto.Message{Content: "基础统计：xxx"})
    })
    p.AddMatcher(matcher)
    return nil
}
// 插件 B: 扩展统计功能
type StatsPluginAdvanced struct {
    *remilia.BasePlugin
}
func (p *StatsPluginAdvanced) Load(engine *remilia.Engine) error {
    matcher := engine.On(remilia.OnCommand("/stats"))
    matcher.Handle(func(ctx *remilia.Context) {
        // 显示扩展统计
        ctx.ReplyGroup(&dto.Message{Content: "扩展统计：yyy"})
    })
    p.AddMatcher(matcher)
    return nil
}
// 两个插件可以共存，用户收到 /stats 时会看到两条回复
```
