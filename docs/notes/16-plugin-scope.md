# PluginScope——插件级资源追踪与级联清理

> 受 Koishi `ctx.plugin()` 启发。每个 Scope 独立追踪其创建的所有资源，
> 卸载时自动逆序级联清理。无需在 Teardown 中手动取消订阅。

## 核心设计

### Scope 类型

```go
type Scope struct {
    name    string
    parent  *Scope
    ctx     *SetupContext
    // 追踪的资源列表
    children      []*Scope
    subscriptions []Subscription
    mwResetters   []func()
    disposeHooks  []func() error
    extraKeys     []string
}
```

### 生命周期

```
Setup 阶段                     Teardown/卸载阶段
┌──────────────────┐          ┌─────────────────────┐
│ ctx.Scope()      │          │ rootScope.Dispose() │
│  ├─ root         │          │  ├─ child2.Dispose()│ (逆序)
│  │  ├─ child1    │  卸载 →  │  │  └─ grandchild   │
│  │  └─ child2    │          │  ├─ child1.Dispose()│
│  │     └─ grand..│          │  ├─ unsubscribe all │
│  └─ subscriptions│          │  ├─ reset middleware│
│     └─ hooks     │          │  └─ run hooks(逆序) │
└──────────────────┘          └─────────────────────┘
```

## 使用方式

### 基础：订阅自动清理

```go
Setup: func(ctx *plugin.SetupContext) (any, error) {
    // 通过 Scope 订阅——卸载时自动取消，无需手动维护 Subscription
    ctx.Scope().Subscribe("plugin.loaded", func(data any) {
        p.invalidateCache()
    })

    // 订阅所有事件
    ctx.Scope().SubscribeAll(func(data any) {
        p.trackEvent(data)
    })

    return p, nil
},
// 无需 Teardown！Scope 自动清理所有订阅
```

### 级联子 Scope

```go
Setup: func(ctx *plugin.SetupContext) (any, error) {
    root := ctx.Scope()

    // 特性A 的子 Scope
    featureA := root.Scope("feature-a")
    featureA.Subscribe("a.topic", handlerA)
    featureA.OnDispose(func() error {
        return cleanupA()
    })

    // 特性B 的子 Scope
    featureB := root.Scope("feature-b")
    featureB.Subscribe("b.topic", handlerB)

    // 卸载时清理顺序：featureB → featureA → root
    return p, nil
},
```

### OnDispose 回调

```go
Setup: func(ctx *plugin.SetupContext) (any, error) {
    ctx.OnDispose(func() error {
        return db.Close()  // 关闭数据库连接
    })
    ctx.OnDispose(func() error {
        return cache.Flush()  // 先清理缓存
    })
    return p, nil
    // 卸载时逆序执行：cache.Flush() → db.Close()
},
```

### 中间件自动清理

```go
Setup: func(ctx *plugin.SetupContext) (any, error) {
    s := ctx.Scope()
    // 注入分组中间件——Scope 被 Dispose 时自动 ResetGroupMiddleware
    s.UseEngineForGroup("myplugin", myMiddleware)
    return p, nil
},
```

## 资源清理顺序

Scope.Dispose() 按以下顺序执行：
1. 子 Scope（逆序，深度优先）
2. EventBus 订阅（逆序 Unsubscribe）
3. 引擎中间件（ResetGroupMiddleware）
4. 容器导出项（Container.Remove）
5. 用户注册的 dispose hooks（逆序）

## 与现有机制的关系

| 机制 | 职责 | 触发时机 |
|------|------|---------|
| `Scope.Dispose()` | 框架资源清理（订阅、中间件、容器项） | unload 阶段 Step 0（goroutine 停止前） |
| `goroutineManager.stopAndWait()` | 停止后台 goroutine | unload 阶段 Step 1 |
| `coordinator.RemoveGroup()` | 移除 Engine Matcher | unload 阶段 Step 2 |
| `Descriptor.Teardown()` | 业务清理（持久化、通知） | unload 阶段 Step 3 |
