# 插件系统功能速查

> **最后更新**: 2026-02-25  
> **适用版本**: v2.0.0+

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
    name := ctx.GetParsedCommand().Args["plugin"]
    if err := a.admin.Reload(name); err != nil {
        return ctx.Reply("重载失败: " + err.Error())
    }
    return ctx.Reply("重载成功")
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
