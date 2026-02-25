# 插件 v2 接口速查

> **最后更新**: 2026-02-25  
> **适用版本**: v2.0.0+（v1 BasePlugin 已移除）

---

## 核心描述符

所有插件通过 `PluginDescriptor` 定义，**无需继承任何基类**。

```go
&plugin.PluginDescriptor{
    Name:    "myplugin",          // 必填，全局唯一
    Version: "1.0.0",             // 建议填写（semver）
    Deps:    []string{"storage"}, // 前置依赖

    // 元数据（影响 /help 显示）
    Meta: &plugin.PluginMeta{
        Author:      "Team",
        Description: "我的插件",
        HelpText:    "/hello - 打招呼",
        Category:    "工具",
        Tags:        []string{"示例"},
    },

    // 初始化（必填）
    Setup: func(ctx *plugin.SetupContext) (any, error) {
        p := &MyPlugin{}
        ctx.Reg.On(dto.GroupAtMessageCreate).Handle(p.handle)
        return p, nil // 导出到容器（nil 也合法）
    },

    // 清理（可选）
    Teardown: func(ctx *plugin.TeardownContext) error {
        ctx.Log.Info("stopping")
        return nil
    },
}
```

---

## SetupContext — Setup 阶段的全部 API

| 字段 | 类型 | 说明 |
|------|------|------|
| `ctx.Reg` | `RegistryWriter` | 注册 Matcher / Command |
| `ctx.Log` | `PluginLogger` | 带插件名前缀的结构化日志 |
| `ctx.Info` | `PluginInfo` | 插件系统**只读**视图 |
| `ctx.Admin` | `ManagerWriter` | 管理写视图（仅 `Privileged:true` 时非 nil）|
| `ctx.Config` | `plugin.Config` | 插件配置（来自 config.yaml plugins 节）|
| `ctx.EventBus` | `EventBus` | 插件间事件总线 |
| `ctx.DryRun` | `bool` | Smart 注册依赖推断阶段为 true |
| `ctx.Go(fn)` | - | 启动生命周期绑定的后台 goroutine |
| `ctx.GoNamed(name, fn)` | - | 有名称的后台 goroutine |

### 注册 Matcher

```go
// 事件匹配器
ctx.Reg.On(dto.GroupAtMessageCreate, context.OnCommand("/ping")).
    Handle(func(c *eventctx.Context) error {
        return c.Reply("Pong!")
    })

// 命令匹配器（自动 O(1) 索引）
ctx.Reg.RegisterCommand(dto.GroupAtMessageCreate, "/status").
    Handle(p.handleStatus)
```

### 依赖获取

```go
// Require[T]：找不到则 panic（推荐用于必要依赖）
store := plugin.Require[storage.Plugin](ctx, "storage")

// Optional[T]：找不到返回 nil, false
cache, ok := plugin.Optional[cache.Plugin](ctx, "cache")

// MustAs[T]：目标是接口类型时使用
var writer io.Writer = plugin.MustAs[io.Writer](ctx, "log-writer")
```

---

## PluginInfo — 只读查询接口

通过 `ctx.Info` 访问，也可在插件之间传递。

```go
// 状态查询
ctx.Info.IsLoaded("storage")        // bool
ctx.Info.IsDisabled("debug")        // bool
ctx.Info.GetStatus("weather")       // *plugin.Status, nil if not found
ctx.Info.List()                     // []string — 所有已注册插件名
ctx.Info.Count()                    // int
ctx.Info.GetMetadata("weather")     // *plugin.Metadata, bool
ctx.Info.ListWithMetadata()         // map[string]*plugin.Metadata
ctx.Info.GetLoadOrder()             // []string
ctx.Info.Get("storage")             // *plugin.PluginInstance, bool

// Engine 只读视图（不能调用任何写操作）
reader := ctx.Info.Coordinator()    // engine.EngineReader
cmds   := reader.GetAllCommands()   // []engine.CommandInfo
```

---

## ManagerWriter — 管理写视图（Privileged 插件）

声明 `Privileged: true` 后，`ctx.Admin` 为非 nil，可调用写操作：

```go
&plugin.PluginDescriptor{
    Name:       "admin",
    Privileged: true,   // ← 声明需要管理权限
    Setup: func(ctx *plugin.SetupContext) (any, error) {
        // 只读：通过 ctx.Info
        plugins := ctx.Info.List()

        // 写操作：通过 ctx.Admin
        if err := ctx.Admin.Reload("weather"); err != nil { ... }
        if err := ctx.Admin.Disable("debug"); err != nil { ... }
        if err := ctx.Admin.Enable("debug"); err != nil { ... }
        if err := ctx.Admin.Unregister("old"); err != nil { ... }
        return &AdminPlugin{admin: ctx.Admin, info: ctx.Info}, nil
    },
}
```

| 方法 | 说明 |
|------|------|
| `Reload(name)` | 热重载插件 |
| `Disable(name)` | 禁用（暂停 Matcher，保留容器条目）|
| `Enable(name)` | 启用已禁用的插件 |
| `Unregister(name)` | 注销插件（完全卸载）|
| `ForceUnregister(name)` | 强制注销（忽略 Unload 错误）|

---

## TeardownContext — Teardown 阶段

```go
Teardown: func(ctx *plugin.TeardownContext) error {
    ctx.Log.Info("plugin stopping")
    ctx.API   // Setup 返回的 API 对象
    ctx.Config // 插件配置
    return nil
},
```

---

## Advanced 高级选项

```go
Advanced: &plugin.PluginAdvanced{
    // 热重载策略
    Strategy: plugin.ReloadInPlace,     // 默认：原地重载
    // Strategy: plugin.ReloadBlueGreen  // 蓝绿重载（先启动新实例再停旧实例）

    // 原地重载时调用（Strategy == ReloadInPlace 时有效）
    Reload: func(ctx *plugin.SetupContext) error {
        // 重新注册 Matcher 等
        return nil
    },

    // 热重载状态保存/恢复
    SaveState:    func() (any, error)    { return myState, nil },
    RestoreState: func(state any) error  { myState = state.(MyState); return nil },

    // 依赖重载通知
    OnDependencyReloaded: func(depName string) {
        // 某依赖插件被重载时调用
    },
},
```

---

## 后台 goroutine（生命周期绑定）

```go
Setup: func(ctx *plugin.SetupContext) (any, error) {
    ctx.Go(func(runCtx context.Context) {
        ticker := time.NewTicker(time.Minute)
        defer ticker.Stop()
        for {
            select {
            case <-ticker.C:
                cleanup()
            case <-runCtx.Done():
                return
            }
        }
    })
    return p, nil
},
```

框架在 Teardown 前自动 cancel 所有 goroutine 并等待退出，无需手动管理。

---

## DryRun 保护

Smart 注册模式会多次执行 Setup 进行依赖推断，此时 `ctx.DryRun == true`。
`ctx.Reg`、`ctx.EventBus`、`ctx.Go` 已自动替换为 no-op，**大多数插件无需判断**。

仅当 Setup 中有网络 I/O、进程级全局变量写入等副作用时才需要：

```go
Setup: func(ctx *plugin.SetupContext) (any, error) {
    if !ctx.DryRun {
        p.metrics = initPrometheusMetrics() // 全局注册，只做一次
    }
    ctx.Reg.RegisterCommand(...)
    return p, nil
},
```

---

## 插件配置

```go
Setup: func(ctx *plugin.SetupContext) (any, error) {
    cfg := ctx.Config
    if cfg != nil {
        apiKey  := cfg.GetString("api_key", "")
        timeout := cfg.GetDuration("timeout", 10*time.Second)
        retries := cfg.GetInt("max_retries", 3)
        enabled := cfg.GetBool("enabled", true)

        cfg.OnChange(func(key string, oldVal, newVal any) {
            // 配置变更回调
        })
    }
    return p, nil
},
```

对应 `config.yaml`:

```yaml
plugins:
  myplugin:
    api_key: "your-key"
    timeout: "10s"
    max_retries: 3
    enabled: true
```
