# 插件开发指南

> **最后更新**: 2026-08-04  
> **说明**: 本文是插件 API 的完整开发指南。

---

## 最简示例

```go
package myplugin

import (
    "github.com/KomeiDiSanXian/remilia/plugin"
    eventctx "github.com/KomeiDiSanXian/remilia/core/context"
    "github.com/KomeiDiSanXian/remilia/platform"
)

func New() *plugin.Descriptor {
    p := &MyPlugin{}
    return &plugin.Descriptor{
        Name:    "myplugin",
        Version: "1.0.0",

        Setup: func(ctx *plugin.SetupContext) (any, error) {
            ctx.Reg.RegisterCommand(eventctx.EventGroup, "/hello").
                Handle(p.handleHello)
            return p, nil // 导出到容器；nil 也合法
        },

        Teardown: func(ctx *plugin.TeardownContext) error {
            ctx.Log.Info("myplugin stopped")
            return nil
        },
    }
}
```

---

## Descriptor 字段速查

```go
&plugin.Descriptor{
    Name:       "myplugin",           // 必填，全局唯一
    Version:    "1.0.0",              // 建议 semver
    Deps:       []string{"storage"},  // 依赖插件名（顺序由框架保证）
    Privileged: false,                // true = ctx.Admin 非 nil

    Meta: &plugin.Metadata{
        Author:      "Your Name",
        Description: "插件功能简介",
        HelpText:    "/cmd - 命令说明",
        Category:    "工具",
        Tags:        []string{"tag1"},
        Hidden:      false,           // true = 不在 /help 中显示
    },

    Setup:    func(ctx *plugin.SetupContext) (any, error) { ... },
    Teardown: func(ctx *plugin.TeardownContext) error { ... },

    Advanced: &plugin.Advanced{
        Strategy:             plugin.ReloadInPlace,
        Reload:               func(ctx *plugin.SetupContext) error { ... },
        SaveState:            func() (any, error) { ... },
        RestoreState:         func(state any) error { ... },
        OnDependencyReloaded: func(depName string) { ... },
    },
}
```

---

## 注册插件

### 单个注册

```go
err := manager.Register(myplugin.New())
```

### 批量注册（显式声明 Deps，框架保证拓扑顺序与依赖校验）

```go
err := manager.RegisterBatch(ctx, []*plugin.Descriptor{
    storage.New(),
    cache.New(),    // Deps: ["storage"]
    weather.New(),  // Deps: ["cache"]
})
```

### Smart 注册（自动推断依赖 + 拓扑排序）

```go
// 推荐：显式声明 Deps（对所有插件都适用，第三方插件作者只需遵守此契约）
err := manager.RegisterBatch(ctx, []*plugin.Descriptor{
    weather.New(),   // Deps: ["storage"]
    admin.New(),
    storage.New(),   // 任意顺序
})

// 可选：自动推断未声明依赖。只有显式声明 DryRunSafe 的插件才会被探测执行
err := manager.RegisterBatch(ctx, []*plugin.Descriptor{
    weather.New(),
    admin.New(),
    storage.New(), // 任意顺序
}, plugin.WithInferDeps())
```

> **DryRunSafe 契约（仅自动推断模式涉及）**：
> 默认情况下（未声明 `DryRunSafe: true`），框架**绝不会**为依赖推断执行插件的
> `Setup`——第三方插件的 Setup 在任何路径下都只执行一次，无需担心探测副作用。
>
> 只有插件作者显式声明 `DryRunSafe: true`，框架才会在 `WithInferDeps` 批量注册
> 时额外执行一次探测 Setup（总计两次），以自动发现未声明的依赖。声明此选项
> 意味着 Setup 必须无副作用或幂等。不确定时保持默认（false）并显式声明 `Deps`。

---

## SetupContext 所有字段

```go
Setup: func(ctx *plugin.SetupContext) (any, error) {
    // Matcher / Command 注册
    ctx.Reg.RegisterMatcher(eventctx.EventPrivate).Handle(handler)
    ctx.Reg.RegisterCommand(eventctx.EventGroup, "/cmd").Handle(handler)

    // 带前缀结构化日志
    ctx.Log.Info("starting")
    ctx.Log.WithField("key", val).Warn("note")

    // 插件系统只读视图
    if !ctx.Info.IsLoaded("storage") {
        return nil, fmt.Errorf("storage required")
    }
    reader := ctx.Info.Coordinator()       // engine.Reader（只读）
    cmds   := reader.GetAllCommands()      // []engine.CommandInfo

    // 依赖获取（ServiceProxy 在依赖插件热重载后仍有效）
    store     := plugin.Service[storage.Plugin](ctx, "storage")
    cache, ok := plugin.TryService[cache.Plugin](ctx, "cache")

    // 插件配置
    if ctx.Config != nil {
        timeout := ctx.Config.GetDuration("timeout", 10*time.Second)
        ctx.Config.OnChange(func(key string, old, newVal any) { })
    }

    // 事件总线（推荐 ctx.Scope().Subscribe：插件卸载自动取消订阅）
    sub, err := ctx.Scope().Subscribe("user.login", func(data any) { })
    _ = sub // sub.Unsubscribe() 取消

    // 生命周期绑定 goroutine
    ctx.Spawn(func(runCtx context.Context) {
        ticker := time.NewTicker(time.Minute)
        defer ticker.Stop()
        for {
            select {
            case <-ticker.C:    doWork()
            case <-runCtx.Done(): return
            }
        }
    })

    // 管理写视图（仅 Privileged:true 时非 nil）
    if ctx.Admin != nil {
        _ = ctx.Admin.Reload("weather")
        _ = ctx.Admin.Disable("debug")
    }

    return p, nil
},
```

---

## 导出 API 给其他插件

```go
// 方式 1：直接返回（框架以 Name 注入容器，消费方用 plugin.Service[T] 获取）
return &WeatherPlugin{}, nil

// 方式 2：按接口导出（消费方用 plugin.Service[WeatherAPI] 获取）
plugin.ExportIface[WeatherAPI](ctx, "weather", impl)
return impl, nil
```

---

## 完整示例：天气插件

```go
package weather

import (
    "context"
    "fmt"
    "time"

    eventctx "github.com/KomeiDiSanXian/remilia/core/context"
    "github.com/KomeiDiSanXian/remilia/plugin"
    "github.com/KomeiDiSanXian/remilia/platform"
)

type Plugin struct {
    apiKey  string
    timeout time.Duration
}

func New() *plugin.Descriptor {
    p := &Plugin{}
    return &plugin.Descriptor{
        Name:    "weather",
        Version: "1.0.0",
        Meta: &plugin.Metadata{
            Description: "天气查询插件",
            Category:    "工具",
            HelpText:    "/weather <城市> — 查询天气",
        },
        Setup: func(ctx *plugin.SetupContext) (any, error) {
            if ctx.Config != nil {
                p.apiKey  = ctx.Config.GetString("api_key", "")
                p.timeout = ctx.Config.GetDuration("timeout", 10*time.Second)
                ctx.Config.OnChange(func(key string, _, newVal any) {
                    if key == "api_key" {
                        if s, ok := newVal.(string); ok {
                            p.apiKey = s
                        }
                    }
                })
            }

            ctx.Reg.RegisterCommand(eventctx.EventGroup, "/weather").
                Handle(p.handleWeather)

            ctx.Spawn(func(runCtx context.Context) {
                ticker := time.NewTicker(time.Hour)
                defer ticker.Stop()
                for {
                    select {
                    case <-ticker.C:      p.prefetchCache()
                    case <-runCtx.Done(): return
                    }
                }
            })

            return p, nil
        },
        Teardown: func(ctx *plugin.TeardownContext) error {
            ctx.Log.Info("weather plugin stopped")
            return nil
        },
    }
}

func (p *Plugin) handleWeather(ctx *eventctx.Context) error {
    cmd := ctx.GetParsedCommand()
    if cmd == nil {
        ctx.Reply(platform.TextMessage("用法：/weather <城市>"))
        return nil
    }
    city, _ := cmd.Arguments["city"].(string)
    result, err := p.fetch(city)
    if err != nil {
        ctx.Reply(platform.TextMessage(fmt.Sprintf("查询失败: %v", err)))
        return nil
    }
    ctx.Reply(platform.TextMessage(result))
    return nil
}

func (p *Plugin) fetch(city string) (string, error) { /* ... */ return "", nil }
func (p *Plugin) prefetchCache()                    { /* ... */ }
```
