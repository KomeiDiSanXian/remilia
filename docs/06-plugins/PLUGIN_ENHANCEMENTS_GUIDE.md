# 插件系统功能速查

> **最后更新**: 2026-08-04  


---

## 1. 插件配置管理

配置通过 `ctx.Config` 注入，来自 `config.yaml` 的 `plugins.<name>` 节。

```go
Setup: func(ctx *plugin.SetupContext) (any, error) {
    cfg := ctx.Config  // 若 viper 未配置则为 nil
    if cfg == nil {
        return p, nil
    }

    apiKey  := cfg.GetString("api_key", "")
    timeout := cfg.GetDuration("timeout", 10*time.Second)
    retries := cfg.GetInt("max_retries", 3)
    enabled := cfg.GetBool("enabled", true)
    tags    := cfg.GetStringSlice("tags", nil)

    // 配置变更回调（支持热更新）
    cfg.OnChange(func(key string, oldVal, newVal any) {
        logger.Infof("Config changed: %s = %v → %v", key, oldVal, newVal)
    })

    // 运行时覆盖（不写回文件）
    _ = cfg.Override("debug", true)

    return p, nil
},
```

对应 `config.yaml`:

```yaml
plugins:
  weather:
    api_key: "your-api-key"
    timeout: "10s"
    max_retries: 3
    enabled: true
```

---

## 2. 插件状态查询

通过 `ctx.Info`（`PluginInfo` 接口）查询，完全只读。

```go
// 单个插件状态
status := ctx.Info.GetStatus("weather")  // *plugin.Status 或 nil
if status != nil {
    fmt.Printf("State: %s\n", status.State)
    fmt.Printf("Uptime: %v\n", status.Uptime)
}

// 批量查询
all := ctx.Info.ListWithMetadata()  // map[string]*plugin.Metadata
for name, meta := range all {
    fmt.Printf("%s (%s): %s\n", name, meta.Version, meta.Description)
}

// 布尔检查
if ctx.Info.IsLoaded("storage") { /* ... */ }
if ctx.Info.IsDisabled("debug") { /* ... */ }

// 加载顺序
order := ctx.Info.GetLoadOrder()  // []string

// 获取实例（查询运行时状态）
inst, ok := ctx.Info.Get("cache")
if ok {
    fmt.Println(inst.GetState()) // Loaded / Unloaded / Error / ...
}
```

---

## 3. 管理操作（仅 Privileged 插件）

声明 `Privileged: true` 后，`ctx.Admin` 不为 nil：

```go
&plugin.Descriptor{
    Name:       "admin",
    Privileged: true,
    Setup: func(ctx *plugin.SetupContext) (any, error) {
        a := &Admin{info: ctx.Info, admin: ctx.Admin}
        ctx.Reg.RegisterCommand(dto.GroupAtMessageCreate, "/admin reload").
            Handle(a.handleReload)
        return a, nil
    },
}

func (a *Admin) handleReload(ctx *eventctx.Context) error {
    name := ctx.GetParsedCommand().Arguments["plugin"]
    if err := a.admin.Reload(name); err != nil {
        ctx.Reply("重载失败: " + err.Error())
            return nil
    }
    ctx.Reply("重载成功")
        return nil
}
```

| 方法 | 说明 |
|------|------|
| `ctx.Admin.Reload(name)` | 热重载插件 |
| `ctx.Admin.Disable(name)` | 禁用（暂停 Matcher）|
| `ctx.Admin.Enable(name)` | 启用 |
| `ctx.Admin.Unregister(name)` | 完全注销 |
| `ctx.Admin.ForceUnregister(name)` | 强制注销（忽略错误）|

---

## 4. 插件间事件总线

通过 `ctx.EventBus` 发布/订阅插件间事件：

```go
// 发布方
Setup: func(ctx *plugin.SetupContext) (any, error) {
    p := &UserPlugin{bus: ctx.EventBus}
    return p, nil
},

func (p *UserPlugin) onLogin(userID string) {
    p.bus.Publish("user.login", UserLoginEvent{UserID: userID})
}

// 订阅方
Setup: func(ctx *plugin.SetupContext) (any, error) {
    sub := ctx.EventBus.Subscribe("user.login", func(data any) {
        evt := data.(UserLoginEvent)
        logger.Infof("login: %s", evt.UserID)
    })
    // sub.Unsubscribe() 在 Teardown 中调用
    return &NotifyPlugin{sub: sub}, nil
},
```

---

## 5. Engine 只读视图

通过 `ctx.Info.Coordinator()` 获取 `engine.Reader`，
可以查询命令列表等只读信息，但**无法**调用 `On/RegisterCommand/DeleteMatcher` 等写操作：

```go
reader := ctx.Info.Coordinator()

// 获取所有命令（用于 /help 生成等）
cmds := reader.GetAllCommands()  // []engine.CommandInfo
for _, cmd := range cmds {
    fmt.Printf("%s — %s\n", cmd.Command, cmd.Description)
}

// 按分类/插件分组
byPlugin   := reader.GetCommandsByPlugin()
byCategory := reader.GetCommandsByCategory()

// 查找命令
info := reader.FindCommand("/weather")
```

---

## 6. 资源追踪

通过 Scope 管理插件创建的所有资源，卸载时自动级联清理，无需手动编写 Teardown。

```go
Setup: func(ctx *plugin.SetupContext) (any, error) {
    ctx.Scope().Subscribe("plugin.reloaded", func(data any) {
        p.invalidateCache()
    })
    ctx.OnDispose(func() error { return db.Close() })
    ctx.OnDispose(func() error { return cache.Flush() })
    return p, nil
},
```
详见 `docs/notes/16-plugin-scope.md`。

---

## 7. 依赖热重载与引用刷新

`ctx.Service[T]` / `ctx.TryService[T]` 在 Setup 时**一次性解析**依赖并返回具体值（非代理）。
若依赖插件之后热重载，已保存的引用不会自动更新；需要持续访问的依赖通过 `Advanced.OnDependencyReloaded` 在依赖重载后重新解析。

```go
// Setup 中一次性解析
p.ctx  = ctx // 保存 ctx 供运行时重新解析
p.perm = ctx.Service[*permission.Plugin]("permission")

// Advanced.OnDependencyReloaded：依赖插件热重载后重新解析
OnDependencyReloaded: func(dep string) {
    if dep == "permission" {
        p.perm = p.ctx.Service[*permission.Plugin]("permission")
    }
},
```

> `ctx.Service[T]` 每次调用都会从容器动态解析最新实现，重新调用即可拿到热重载后的新实例。
详见 `docs/notes/17-service-proxy.md`。

---

## 8. 状态迁移

```go
Advanced: &plugin.Advanced{
    SaveState:    func() (any, error) { return oldData, nil },
    MigrateState: func(old any, oldVer, newVer string) (any, error) { ... },
    RestoreState: func(s any) error { ... },
},
```
详见 `docs/notes/18-state-migration.md`。

---

## 9. 定时任务（RegisterCron / After）

在 Setup 阶段注册**生命周期绑定**的定时任务，插件 Teardown 时自动停止，无需手动清理。

```go
Setup: func(ctx *plugin.SetupContext) (any, error) {
    // cron 定时（支持 5 段标准格式与 6 段含秒格式）
    if err := ctx.RegisterCron("0 2 * * *", func() {
        p.cleanupExpired()
    }); err != nil {
        ctx.Log.Warnf("RegisterCron failed: %v", err)
    }

    // 一次性延迟任务（"N 分钟后执行一次"，如倒计时提醒）
    ctx.After("remind:"+userID, 5*time.Minute, func() {
        sender.NotifyUser(ctx, userID, platform.TextMessage("提醒时间到！"))
    })
    return p, nil
},
```

- `RegisterCron(expr, fn)`：表达式由 `robfig/cron/v3` 解析，同一插件共享一个 scheduler，Teardown 自动停止
- `After(name, duration, fn)`：到期执行一次；插件在到期前卸载则静默取消
- 区别于 `ctx.Spawn`（长驻后台循环）：定时任务适合"到点触发"场景

---

## 10. 出站消息观察者（OutboundObserver）

观察 `ctx.Reply*` 发送的**出站消息结果**（成功/失败/平台响应），用于消息记录、审计、失败统计等。
与"包装 `platform.Sender`"不同：不依赖任何平台可选接口（MessageDeleter/GroupManager 等），只挂钩 `ctx.Reply` 的发送任务。

```go
// 中间件中注入观察者
eng.Use(func(next eventctx.Handler) eventctx.Handler {
    return func(ctx *eventctx.Context) error {
        ctx.Ext().SetTyped(context.OutboundObserverExt{
            Observer: &MyOutboundRecorder{},
        })
        return next(ctx)
    }
})

// 观察者实现
type MyOutboundRecorder struct{}

func (r *MyOutboundRecorder) OnOutbound(
    chatID string,
    req platform.SendRequest,
    res platform.SendResult,
    err error,
) {
    // chatID 目标会话；req/res 请求与平台响应；err 发送错误（成功为 nil）
    logger.Info("outbound", "chat", chatID, "err", err)
}
```

> 注意：观察者只观察经 `ctx.Reply*` 发送的消息；直接调用 `platform.Sender.Send`（绕过 ctx.Reply，如 sendqueue 插件）不会被观察到。
> 参考实现：`builtin/messagelog`（出站消息落库）。
