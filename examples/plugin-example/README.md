# Plugin Example

这个示例展示了如何开发和使用 Remilia 插件系统，包括插件注册、生命周期管理和热重载。

## 功能

- ✅ 插件开发 - 创建自定义插件
- ✅ 插件管理 - 注册、卸载、重载
- ✅ 生命周期监听 - 监听插件事件
- ✅ 状态保持 - 插件重载时保持状态
- ✅ 依赖管理 - 插件间依赖关系

## 插件列表

### Greeter Plugin
问候插件，支持自定义问候语

**命令**:
- `/greet [name]` - 问候用户
- `/setgreeting <text>` - 设置问候语

**示例**:
```
/greet
/greet Alice
/setgreeting 欢迎
```

### Counter Plugin
计数器插件，记录调用次数

**命令**:
- `/count` - 增加计数
- `/resetcount` - 重置计数

**示例**:
```
/count      # 输出: 计数: 1
/count      # 输出: 计数: 2
/resetcount # 重置为 0
```

### Timer Plugin
时间插件，显示运行时间和当前时间

**命令**:
- `/uptime` - 显示运行时间
- `/time` - 显示当前时间

**示例**:
```
/uptime  # 输出: 运行时间: 1h23m45s
/time    # 输出: 当前时间: 2026-01-23 15:00:00
```

## 运行

```bash
# 设置环境变量
export BOT_SECRET="your-webhook-secret"
export BOT_PORT="8080"

# 运行
go run -tags example main.go
```

程序启动 30 秒后会自动演示插件热重载。

## 代码说明

> **注意**: v1 插件 API（`BasePlugin`）已在 v1.0.0 中完全移除。  
> 以下示例均使用 **v2 API**（`PluginDescriptor`）。详见 [Plugin v2 迁移指南](../../docs/02-user-guides/PLUGIN_V1_TO_V2_MIGRATION.md)。

### 1. 创建插件描述符

```go
package myplugin

import (
    "github.com/KomeiDiSanXian/remilia/plugin"
    eventctx "github.com/KomeiDiSanXian/remilia/core/context"
    "github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// GreeterPlugin 保存插件运行时状态
type GreeterPlugin struct {
    greeting string
}

// New 返回插件描述符，供 pm.Register 使用
func New() *plugin.Descriptor {
    p := &GreeterPlugin{greeting: "你好"}
    return &plugin.Descriptor{
        Name:    "greeter",
        Version: "1.0.0",
        Meta: &plugin.Metadata{
            Description: "问候插件",
            Category:    "工具",
        },
        Setup: func(ctx *plugin.SetupContext) (any, error) {
            ctx.Reg.RegisterCommand(platform.EventKindGroupMessage, "/greet").
                Handle(func(c *eventctx.Context) error {
                    name := c.GetMessageContent()
                    if name == "" {
                        name = "朋友"
                    }
                    return c.Reply(platform.TextMessage(p.greeting + ", " + name + "!"))
                })

            ctx.Reg.RegisterCommand(platform.EventKindGroupMessage, "/setgreeting").
                Handle(func(c *eventctx.Context) error {
                    p.greeting = c.GetMessageContent()
                    return c.Reply(platform.TextMessage("问候语已更新: " + p.greeting))
                })

            return p, nil
        },
    }
}
```

### 2. 注册插件

```go
manager := plugin.NewManager(eng)

// 单个注册
if err := manager.Register(myplugin.New()); err != nil {
    log.Fatal(err)
}

// 批量注册（自动按依赖顺序排序）
if err := manager.RegisterMultipleSmart(
    []*plugin.Descriptor{
        storage.New(),
        myplugin.New(),   // 若依赖 storage，会自动在其后注册
    },
); err != nil {
    log.Fatal(err)
}
```

### 3. 声明插件依赖

```go
func New() *plugin.PluginDescriptor {
    return &plugin.PluginDescriptor{
        Name: "my-plugin",
        Deps: []string{"storage"},  // 显式声明依赖
        Setup: func(ctx *plugin.SetupContext) (any, error) {
            // 获取依赖的插件实例
            storagePlugin := ctx.Require("storage")
            _ = storagePlugin
            return nil, nil
        },
    }
}
```

### 4. 使用生命周期钩子

```go
func New() *plugin.PluginDescriptor {
    return &plugin.PluginDescriptor{
        Name: "my-plugin",
        Setup: func(ctx *plugin.SetupContext) (any, error) {
            // 启动后台 goroutine（生命周期与 Bot 绑定）
            ctx.Go(func() {
                ticker := time.NewTicker(time.Minute)
                defer ticker.Stop()
                for {
                    select {
                    case <-ctx.Context().Done():
                        return
                    case <-ticker.C:
                        // 定时任务
                    }
                }
            })
            return nil, nil
        },
        Advanced: &plugin.PluginAdvanced{
            Teardown: func() error {
                // 插件卸载时清理资源
                return nil
            },
        },
    }
}
```

## 插件开发最佳实践

### 1. 并发安全的状态管理

```go
type SafePlugin struct {
    mu    sync.RWMutex
    state map[string]any
}

func (p *SafePlugin) get(key string) any {
    p.mu.RLock()
    defer p.mu.RUnlock()
    return p.state[key]
}

func (p *SafePlugin) set(key string, value any) {
    p.mu.Lock()
    defer p.mu.Unlock()
    p.state[key] = value
}
```

### 2. 错误处理

```go
Setup: func(ctx *plugin.SetupContext) (any, error) {
    if err := initialize(); err != nil {
        return nil, fmt.Errorf("greeter: initialization failed: %w", err)
    }
    return &GreeterPlugin{}, nil
},
```

### 3. 日志记录（使用 infra/logger，而非 logrus）

```go
import "github.com/KomeiDiSanXian/remilia/infra/logger"

Setup: func(ctx *plugin.SetupContext) (any, error) {
    logger.WithField("plugin", "greeter").Debug("Plugin setup started")
    // ...
    logger.WithField("plugin", "greeter").Info("Plugin setup completed")
    return &GreeterPlugin{}, nil
},
```

## 下一步

- 查看 [plugin-v2-demo](../plugin-v2-demo) 了解更完整的 v2 API 演示
- 查看 [middleware-example](../middleware-example) 了解中间件开发
- 阅读 [Plugin v2 迁移指南](../../docs/02-user-guides/PLUGIN_V1_TO_V2_MIGRATION.md)
- 查看 [showcase](../showcase) 了解所有内置插件的用法
