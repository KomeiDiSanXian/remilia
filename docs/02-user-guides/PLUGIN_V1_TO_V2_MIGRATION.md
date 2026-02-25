# Plugin v2 快速上手

> **最后更新**: 2026-02-25  
> **适用版本**: v2.0.0+  
> **说明**: v1 `BasePlugin` 已在 v2.0.0 中完全移除。本文是 v2 API 的完整快速入门。

---

## 最简示例

```go
package myplugin

import (
    "github.com/KomeiDiSanXian/remilia/plugin"
    eventctx "github.com/KomeiDiSanXian/remilia/core/context"
    "github.com/KomeiDiSanXian/remilia/openapi/dto"
)

func New() *plugin.PluginDescriptor {
    p := &MyPlugin{}
    return &plugin.PluginDescriptor{
        Name:    "myplugin",
        Version: "1.0.0",

        Setup: func(ctx *plugin.SetupContext) (any, error) {
            ctx.Reg.On(dto.GroupAtMessageCreate, eventctx.OnCommand("/hello")).
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

## PluginDescriptor 字段速查

```go
&plugin.PluginDescriptor{
    Name:       "myplugin",           // 必填，全局唯一
    Version:    "1.0.0",              // 建议 semver
    Deps:       []string{"storage"},  // 依赖插件名（顺序由框架保证）
    Privileged: false,                // true = ctx.Admin 非 nil

    Meta: &plugin.PluginMeta{
        Author:      "Your Name",
        Description: "插件功能简介",
        HelpText:    "/cmd - 命令说明",
        Category:    "工具",
        Tags:        []string{"tag1"},
        Hidden:      false,           // true = 不在 /help 中显示
    },

    Setup:    func(ctx *plugin.SetupContext) (any, error) { ... },
    Teardown: func(ctx *plugin.TeardownContext) error { ... },

    Advanced: &plugin.PluginAdvanced{
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
err := manager.RegisterV2(myplugin.New())
```

### 批量注册（手动声明顺序，Deps 字段保证依赖校验）

```go
err := plugin.RegisterMultipleV2Atomic(manager,
    storage.New(),
    cache.New(),    // Deps: ["storage"]
    weather.New(),  // Deps: ["cache"]
)
```

### Smart 注册（自动推断依赖 + 拓扑排序）

```go
// 无需手写 Deps，框架通过 DryRun 自动分析依赖图
err := plugin.RegisterMultipleV2Smart(manager,
    weather.New(),
    admin.New(),
    storage.New(), // 任意顺序
)
```

> Smart 模式在 DryRun 阶段会多次执行 `Setup`。
> 对于有全局副作用的初始化，用 `ctx.DryRun` 保护：
> ```go
> if !ctx.DryRun { p.metrics = initPrometheusMetrics() }
> ```

---

## SetupContext 所有字段

```go
Setup: func(ctx *plugin.SetupContext) (any, error) {
    // Matcher / Command 注册
    ctx.Reg.On(dto.C2CMessageCreate).Handle(handler)
    ctx.Reg.RegisterCommand(dto.GroupAtMessageCreate, "/cmd").Handle(handler)

    // 带前缀结构化日志
    ctx.Log.Info("starting")
    ctx.Log.WithField("key", val).Warn("note")

    // 插件系统只读视图
    if !ctx.Info.IsLoaded("storage") {
        return nil, fmt.Errorf("storage required")
    }
    reader := ctx.Info.Coordinator()       // engine.EngineReader（只读）
    cmds   := reader.GetAllCommands()      // []engine.CommandInfo

    // 依赖获取
    store     := plugin.Require[storage.Plugin](ctx, "storage")
    cache, ok := plugin.Optional[cache.Plugin](ctx, "cache")

    // 插件配置
    if ctx.Config != nil {
        timeout := ctx.Config.GetDuration("timeout", 10*time.Second)
        ctx.Config.OnChange(func(key string, old, newVal any) { })
    }

    // 事件总线
    sub := ctx.EventBus.Subscribe("user.login", func(data any) { })
    _ = sub // sub.Unsubscribe() 取消

    // 生命周期绑定 goroutine
    ctx.Go(func(runCtx context.Context) {
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
// 方式 1：直接返回（框架以 Name 注入容器，消费方用 plugin.Require[T] 获取）
return &WeatherPlugin{}, nil

// 方式 2：按接口导出（消费方用 plugin.MustAs[WeatherAPI] 获取）
plugin.ExportInterface[WeatherAPI](ctx, "weather", impl)
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
    "github.com/KomeiDiSanXian/remilia/openapi/dto"
    "github.com/KomeiDiSanXian/remilia/plugin"
)

type Plugin struct {
    apiKey  string
    timeout time.Duration
}

func New() *plugin.PluginDescriptor {
    p := &Plugin{}
    return &plugin.PluginDescriptor{
        Name:    "weather",
        Version: "1.0.0",
        Meta: &plugin.PluginMeta{
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

            ctx.Reg.RegisterCommand(dto.GroupAtMessageCreate, "/weather").
                Handle(p.handleWeather)

            ctx.Go(func(runCtx context.Context) {
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
        return ctx.Reply("用法：/weather <城市>")
    }
    city, _ := cmd.Args["city"]
    result, err := p.fetch(city)
    if err != nil {
        return ctx.Reply(fmt.Sprintf("查询失败: %v", err))
    }
    return ctx.Reply(result)
}

func (p *Plugin) fetch(city string) (string, error) { /* ... */ return "", nil }
func (p *Plugin) prefetchCache()                    { /* ... */ }
```
