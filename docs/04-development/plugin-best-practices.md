# 插件开发最佳实践

> **最后更新**: 2026-02-25  
> **适用版本**: v2.0.0+（v1 `BasePlugin` 已移除）

---

## 1. 插件结构

### ✅ 推荐

```go
// plugins/weather/weather.go
package weather

type Plugin struct {
    apiKey string
    cache  *cachePlugin.Plugin
}

func New() *plugin.Descriptor {
    p := &MyPlugin{}
    return &plugin.Descriptor{
        Name:    "weather",
        Version: "1.0.0",
        Deps:    []string{"cache"}, // 明确声明依赖
        Meta: &plugin.Metadata{
            Description: "天气查询",
            Category:    "工具",
        },
        Setup: func(ctx *plugin.SetupContext) (any, error) {
            // 依赖注入（ServiceProxy 在依赖热重载后仍有效）
            p.cache = plugin.Service[cachePlugin.Plugin](ctx, "cache")

            // 配置
            if ctx.Config != nil {
                p.apiKey = ctx.Config.GetString("api_key", "")
            }

            // 注册命令
            ctx.Reg.RegisterCommand(dto.GroupAtMessageCreate, "/weather").
                Handle(p.handle)

            return p, nil
        },
    }
}
```

### ❌ 避免

```go
// ❌ 在 main.go 中内联所有插件逻辑
func main() {
    eng := engine.NewEngine()
    eng.OnCommand("/weather", func(ctx *eventctx.Context) error {
        // 大量业务逻辑…
    })
}
```

---

## 2. 依赖声明

### ✅ 推荐：在 `Deps` 中声明

```go
&plugin.Descriptor{
    Name: "admin",
    Deps: []string{"permission", "storage"}, // 明确声明所有依赖
    Setup: func(ctx *plugin.SetupContext) (any, error) {
        perm    := plugin.Service[permission.Plugin](ctx, "permission")
        storage := plugin.Service[storage.Plugin](ctx, "storage")
        // ...
    },
}
```

### ✅ Smart 注册（自动推断依赖图）

```go
// 无需手动排序，框架 DryRun 推断依赖关系
err := plugin.RegisterMultipleV2Smart(manager,
    admin.New(),
    storage.New(),
    permission.New(),
)
```

### ❌ 避免

```go
// ❌ 依赖已通过 Service[T] 使用，却未在 Deps 声明
// (使用 RegisterMultipleV2Smart 可免去这个困扰)
Deps: []string{}, // 漏了 storage
Setup: func(ctx *plugin.SetupContext) (any, error) {
    s := plugin.Service[storage.Plugin](ctx, "storage") // 运行时报错
    // ...
},
```

---

## 3. 错误处理

### ✅ 推荐

```go
Setup: func(ctx *plugin.SetupContext) (any, error) {
    apiKey := ""
    if ctx.Config != nil {
        apiKey = ctx.Config.GetString("api_key", "")
    }
    if apiKey == "" {
        return nil, fmt.Errorf("weather: api_key is required")
    }
    return &Plugin{apiKey: apiKey}, nil
},
```

### Handler 中

```go
func (p *Plugin) handle(ctx *eventctx.Context) error {
    result, err := p.fetch(city)
    if err != nil {
        // 记录详细错误，回复用户友好消息
        ctx.Log().WithError(err).Error("fetch failed")
        return ctx.Reply("查询暂时不可用，请稍后再试")
    }
    return ctx.Reply(result)
}
```

---

## 4. 后台 goroutine

**始终使用 `ctx.Spawn`，不要裸起 goroutine**：

```go
Setup: func(ctx *plugin.SetupContext) (any, error) {
    // ✅ 生命周期绑定，Teardown 自动停止
    ctx.Spawn(func(runCtx context.Context) {
        ticker := time.NewTicker(5 * time.Minute)
        defer ticker.Stop()
        for {
            select {
            case <-ticker.C:      p.refresh()
            case <-runCtx.Done(): return
            }
        }
    })

    // ❌ 避免：goroutine 泄漏，无法在 Teardown 时停止
    // go func() { for { time.Sleep(5*time.Minute); p.refresh() } }()

    return p, nil
},
```

---

## 5. 并发任务组（TaskGroup）

需要**并发执行一批短任务并等待结果**时，使用 `ctx.NewTaskGroup()`，不要用 `ctx.Spawn`。

| | `ctx.Spawn` / `SpawnNamed` | `ctx.NewTaskGroup` |
|---|---|---|
| 适用场景 | 长驻后台 daemon（定时清理、自动保存、监听循环） | 短生命周期并发任务（并发网络请求、批量计算） |
| 生命周期 | 跟随插件，Teardown 时自动 cancel + wait | 调用者主动 `Wait()`，Teardown 时也会 cancel |
| 返回值 | 无（fire-and-forget） | `Wait()` 返回聚合 error |
| 任务签名 | `func(context.Context)` | `func(context.Context) error` |

```go
Setup: func(ctx *plugin.SetupContext) (any, error) {
    results := make([]Result, 0, len(urls))
    var mu sync.Mutex

    g := ctx.NewTaskGroup()
    for _, url := range urls {
        url := url
        g.Go(func(taskCtx context.Context) error {
            data, err := fetchWithCtx(taskCtx, url)
            if err != nil {
                return err
            }
            mu.Lock()
            results = append(results, data)
            mu.Unlock()
            return nil
        })
    }
    if err := g.Wait(); err != nil {
        ctx.Log.Warnf("部分请求失败: %v", err)
    }
    // 处理全部 results ...

    return p, nil
},
```

如果任务集合已知且无需处理中间结果，可用 `Batch` 快捷方式：

```go
err := ctx.Batch(task1, task2, task3).Wait()
```

---

## 6. DryRun 保护（Smart 注册时）

Smart 模式会多次执行 `Setup`（DryRun 阶段）推断依赖关系。
`ctx.Reg`、`ctx.EventBus`、`ctx.Spawn` 已自动 no-op，**大多数插件无需处理**。

**仅当 Setup 中有以下副作用时才需要判断**：
- 调用外部 HTTP/DB 请求
- 写入进程级全局变量（如 Prometheus 注册）

```go
Setup: func(ctx *plugin.SetupContext) (any, error) {
    if !ctx.DryRun {
        // 仅在真实注册时执行
        p.metrics = initPrometheusMetrics()
    }
    // 正常注册逻辑
    ctx.Reg.RegisterCommand(...)
    return p, nil
},
```

---

## 6. 管理类插件（Privileged）

需要调用 Reload/Disable/Enable 等写操作的插件，
**必须声明 `Privileged: true`**，并通过 `ctx.Admin` 访问：

```go
&plugin.Descriptor{
    Name:       "admin",
    Privileged: true,  // ← 显式声明，代码审查可见
    Setup: func(ctx *plugin.SetupContext) (any, error) {
        // 只读查询用 ctx.Info
        all := ctx.Info.List()
        // 写操作用 ctx.Admin
        _ = ctx.Admin.Reload("weather")
        return &Admin{info: ctx.Info, admin: ctx.Admin}, nil
    },
}
```

**不要**将 `ctx.Admin` 存储到可被非特权代码访问的位置。

---

## 7. 测试

使用 `plugintest` 包测试插件的 Setup 逻辑：

```go
import "github.com/KomeiDiSanXian/remilia/plugin/plugintest"

func TestMyPlugin(t *testing.T) {
    // 创建测试上下文（含依赖容器）
    deps := map[string]any{"cache": cachePlugin}
    ctx := plugintest.NewSetupContextWithDeps("myplugin", deps, nil)
    defer plugintest.StopSetupContext(ctx)

    // 运行 Setup
    api, err := myDescriptor.Setup(ctx)
    require.NoError(t, err)
    assert.NotNil(t, api)
}
```

需要测试完整生命周期（Register → Setup → Teardown）时，直接使用 `plugin.Manager`：

```go
func TestFullLifecycle(t *testing.T) {
    pm := plugin.NewManager(nil)
    require.NoError(t, pm.Register(myplugin.New()))
    require.NoError(t, pm.Unregister(context.Background(), "myplugin"))
}
```

---

## 8. ctx.Set / ctx.Delete 注意事项

从 Context 中删除 key 须使用 `ctx.Delete`，`ctx.Set(key, nil)` 是 **no-op**：

```go
// ✅ 正确删除
ctx.Delete("session")

// ❌ 不会删除，nil 值被静默忽略
ctx.Set("session", nil)
```

---

## 9. 不要在 Setup 中做重型初始化

重型初始化（DB 连接、预加载大量数据）应放在 goroutine 中异步完成，
或用 `sync.Once` + 懒加载方式在第一次调用时初始化：

```go
Setup: func(ctx *plugin.SetupContext) (any, error) {
    p := &Plugin{}
    ctx.Spawn(func(runCtx context.Context) {
        if err := p.initDB(runCtx); err != nil {
            ctx.Log.WithError(err).Error("db init failed")
        }
    })
    ctx.Reg.RegisterCommand(...)
    return p, nil
},
```
