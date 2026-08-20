# ServiceProxy——防过期的插件间同步调用

> 插件 A 在 Setup 中获取插件 B 的指针，但 B 热重载后 A 持有的是过时指针。
> ServiceProxy 每次调用时从 Container 动态解析最新实现，彻底消除此问题。

## 问题场景

```go
// ❌ 旧方式：B 热重载后 perm 指针过时
type PluginA struct {
    perm *permission.Plugin  // Setup 时获取，热重载后失效
}

func (a *PluginA) checkPerm(userID string) bool {
    return a.perm.HasPermission(userID, "admin")  // 可能 nil pointer
}
```

## 解决方案

```go
// ✅ 新方式：ServiceProxy 总是拿到最新实现
type PluginA struct {
    permSvc *plugin.ServiceProxy[*permission.Plugin]
}

func (a *PluginA) checkPerm(userID string) bool {
    // 每次调用都从 Container 解析最新实现
    pp, ok := a.permSvc.Get()
    if !ok {
        return false
    }
    return pp.HasPermission(userID, "admin")
}
```

## API 一览

### 创建代理

```go
// 硬依赖（不存在则 panic）
permSvc := ctx.Service[*permission.Plugin]("permission")

// 可选依赖（返回 nil, false 不 panic）
if svc, ok := ctx.TryService[*storage.Plugin]("storage"); ok {
    p.storageSvc = svc
}
```

### 获取实例

```go
// Get — 安全获取
if pp, ok := p.permSvc.Get(); ok {
    pp.Grant(userID, "admin")
}

// Must — panic on failure
pp := p.permSvc.Must()
pp.Grant(userID, "admin")

// Do — 静默跳过（可选依赖）
p.storageSvc.Do(func(s *storage.Plugin) {
    s.Save(data)
})

// Call — 带错误返回
err := p.permSvc.Call(func(pp *permission.Plugin) error {
    return pp.Grant(userID, "admin")
})
```

## 薄包装模式

当调用点很多时，推荐用 getter 方法封装：

```go
type Plugin struct {
    permSvc *plugin.ServiceProxy[*permission.Plugin]
    // 保留直接指针用于测试/手动绑定
    PermPlugin *permission.Plugin
}

// perm() 优先 ServiceProxy，回退到直接指针
func (p *Plugin) perm() *permission.Plugin {
    if p.permSvc != nil {
        pp, _ := p.permSvc.Get()
        if pp != nil {
            return pp
        }
    }
    return p.PermPlugin
}

// 调用点零改动：p.perm().HasPermission(...)
```

## 与现有机制的关系

| 机制 | 适用场景 | 热重载安全 |
|------|---------|-----------|
| `plugin.Must[T]` | Setup 一次性获取 | ❌ 重载后指针过时 |
| `plugin.Try[T]` | 可选依赖获取 | ❌ 重载后指针过时 |
| `ctx.Service[T]` | 需要持续访问的硬依赖 | ✅ 每次动态解析 |
| `ctx.TryService[T]` | 需要持续访问的可选依赖 | ✅ 每次动态解析 |
| `OnDependencyReloaded` | 手动刷新指针 | ⚠️ 容易遗漏 |
