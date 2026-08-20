# 依赖热重载与引用刷新

> 插件 A 在 Setup 中获取插件 B 的 API，B 热重载后 A 持有的引用可能过期。
> `ctx.Service[T]` 每次调用都会从容器动态解析最新实现；结合 `OnDependencyReloaded`
> 通知即可在依赖重载后安全刷新引用。

## 问题场景

```go
// ❌ B 热重载后 perm 引用过期
type PluginA struct {
	perm *permission.Plugin // Setup 时获取，热重载后可能失效
}

func (a *PluginA) checkPerm(userID string) bool {
	return a.perm.HasPermission(userID, "admin") // 可能 nil / 过期实例
}
```

## 解决方案

`ctx.Service[T]` 在 Setup 时一次性解析并返回具体值（非代理）。需要持续访问的依赖，
保存 `SetupContext`，在 `OnDependencyReloaded` 回调中重新解析：

```go
type PluginA struct {
	ctx  *plugin.SetupContext
	perm *permission.Plugin
}

Setup: func(ctx *plugin.SetupContext) (any, error) {
	p.ctx  = ctx
	p.perm = ctx.Service[*permission.Plugin]("permission")
	return p, nil
},

Advanced: &plugin.Advanced{
	// 依赖插件热重载后触发，重新解析引用
	OnDependencyReloaded: func(dep string) {
		if dep == "permission" {
			p.perm = p.ctx.Service[*permission.Plugin]("permission")
		}
	},
},
```

## 获取 API

```go
// 硬依赖（不存在则 panic）
perm := ctx.Service[*permission.Plugin]("permission")

// 可选依赖（返回 zero 值, false 不 panic）
if cache, ok := ctx.TryService[*storage.Plugin]("storage"); ok {
	p.storage = cache
}

// 按接口导出 / 消费
ctx.ExportIface[io.Writer]("log-writer", impl) // 生产者
writer := ctx.Service[io.Writer]("log-writer") // 消费者
```

## 薄包装模式

调用点很多时，用 getter 方法封装，重载刷新只改一处：

```go
func (p *Plugin) perm() *permission.Plugin {
	if p.ctx != nil {
		return p.ctx.Service[*permission.Plugin]("permission")
	}
	return p.PermPlugin // 测试 / 手动绑定
}
```

## 机制一览

| 机制 | 适用场景 | 热重载安全 |
|------|---------|-----------|
| `ctx.Service[T]` | 需要持续访问的硬依赖 | ✅ 重新调用即动态解析 |
| `ctx.TryService[T]` | 需要持续访问的可选依赖 | ✅ 重新调用即动态解析 |
| `OnDependencyReloaded` | 依赖重载通知，触发引用刷新 | ✅（需在回调中重新解析） |
| Setup 时保存的指针 | 一次性获取 | ❌ 重载后可能过期 |
