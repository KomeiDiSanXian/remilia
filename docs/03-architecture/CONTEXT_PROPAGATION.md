# Context 传播模式

> **最后更新**: 2026-04-27

## 概述

Remilia 中有两种 context 传播模式，服务于不同目的：

| 模式 | 位置 | 核心行为 | 适用场景 |
|------|------|----------|----------|
| **A — 剥离超时 + 分层** | `lifecycle.Manager` | `context.WithoutCancel` 剥离 Start 超时，创建 parentCtx/runCtx 层级 | 组件生命周期（OnStart/OnRun/OnStop） |
| **B — 绑定父级** | DedupFilter / AdaptiveRateLimiter / DLQ / TokenManager / 等 | `context.WithCancel(parent)`，goroutine 通过 `parent.Done()` 自动退出 | 有后台 goroutine 的基础设施组件 |

---

## 模式 A：Lifecycle Manager 分层架构

### Context 层级

```
Bot.Start() ctx (带 DefaultStartTimeout=30s)
  │
  ├─ OnStart(ctx)       ← 使用原始 ctx（受超时控制）
  │
  └─ context.WithoutCancel(ctx)   ← 剥离超时，保留 Values
       │
       └─ parentCtx (WithCancel)  ← Start() 完成后创建
            │  ▲  parentCancel() 在 Stop() 末尾调用（所有 OnStop 完成后）
            │
            └─ runCtx (WithCancel) ← 传给 OnRun goroutine
                 │  runCancel() 在 Stop() 开始时立即调用
                 │
                 └─ 插件可通过 ctx.Spawn() 获得 runCtx 的派生 ctx
```

### 设计意图

1. **OnStart 受超时控制**：调用者传入的 ctx 带超时（如 30s），Start() 串行调用各组件的 OnStart，若超时则返回错误。

2. **OnRun 不受 Start 超时控制**：`context.WithoutCancel(ctx)` 剥离超时后创建 parentCtx/runCtx。OnRun 是长时间运行的 goroutine，不应被 Start 阶段的超时限制。

3. **OnStop 在 runCtx 取消后执行**：Stop() 先调用 `runCancel()` 通知所有 OnRun 退出，等待它们结束后再逆序调用 OnStop。

4. **OnStop 的超时**：若等待 OnRun 退出超时，用 `context.WithoutCancel(ctx)` + 新超时创建 fresh context 执行 OnStop（防止 OnRun 慢导致 OnStop 无时间窗口）。

### 使用方式

```go
// Component 接口
type Component interface {
    Name() string
    OnStart(ctx context.Context) error   // ctx 带 Start 超时
    OnRun(ctx context.Context) error     // ctx 是 runCtx，Stop 时取消
    OnStop(ctx context.Context) error    // ctx 带 stop 超时
}
```

**OnRun 实现**必须监听 `<-ctx.Done()`：

```go
func (c *MyComponent) OnRun(ctx context.Context) error {
    for {
        select {
        case <-ctx.Done():
            return nil
        case <-c.ticker.C:
            // do work
        }
    }
}
```

---

## 模式 B：绑定父级（WithContext 模式）

### 模式

```go
func NewMyComponentWithContext(parent context.Context, config Config) *MyComponent {
    ctx, cancel := context.WithCancel(parent)
    return &MyComponent{
        ctx:    ctx,
        cancel: cancel,
    }
}
```

后台 goroutine 通过 `<-c.ctx.Done()` 感知父级取消：

```go
func (c *MyComponent) cleanupLoop() {
    for {
        select {
        case <-c.ctx.Done():
            return
        default:
            c.removeExpired()
        }
    }
}
```

### 现有实现

| 组件 | 文件 | 父级来源 |
|------|------|----------|
| `NewDedupFilterWithContext` | `middleware/dedup.go` | bot.Context() |
| `NewAdaptiveRateLimiterWithContext` | `middleware/adaptive.go` | bot.Context() |
| `NewManagerFromConfigWithContext` (token) | `platform/qq/openapi/auth/token/token.go` | bot.Context() |
| `NewWithContext` (DLQ) | `infra/dlq/queue_generic.go` | bot.Context() |
| platform adapter 的 WithCancel | `platform/*/adapter.go` | OnRun 传来的 runCtx |

### 设计意图

1. **生命周期自动绑定**：后台 goroutine 随 bot 停止自动退出，无需手动调用 `Stop()`。

2. **从 lifecycle 获取父级**：通过 `bot.Context()` 获取 lifecycle 的 `parentCtx`（Start 后可用）。当 bot 停止时，parentCtx 被取消，所有绑定组件的 goroutine 自动退出。

---

## 模式对比

| 维度 | 模式 A (Lifecycle) | 模式 B (WithContext) |
|------|-------------------|---------------------|
| 超时传播 | **阻断**（WithoutCancel） | **保持**（WithCancel 继承） |
| 取消传播 | **保持**（WithoutCancel 保留取消链） | **保持** |
| Value 传播 | **保持** | **保持** |
| 适用方 | Component 开发者 | 基础设施组件消费者 |
| 入口 | 实现 Component 接口 | `NewXxxWithContext(ctx, cfg)` |
| goroutine 管理 | Lifecycle Manager 的 runWg | 组件内部管理或手动 Stop |

### 关键区别：超时 vs 取消

- **模式 A** 剥离**超时（deadline）**但保留**取消（cancellation）**。OnRun 不受 Start 超时限制，但 Stop 时仍会通过 `runCancel()` 通知退出。
- **模式 B** 通过 `context.WithCancel(parent)` 继承父级的所有属性（超时 + 取消）。如果父级有超时，子 ctx 也会在超时后取消。这在大多数场景下是正确的，但需要注意父级超时可能意外终止后台 goroutine。

### 选择指南

```
你的组件有后台 goroutine 吗？
├── 是 → 它是 lifecycle Component 吗？
│   ├── 是 → 模式 A：实现 Component 接口，OnRun 监听 ctx.Done()
│   └── 否 → 模式 B：提供 `NewXxxWithContext(parent)` 构造
└── 否 → 不需要 context 绑定
```

---

## 最佳实践

### 1. 所有后台 goroutine 组件都应提供 `WithContext` 变体

```go
// ✅ 推荐
func NewDedupFilterWithContext(parent context.Context, config DedupConfig) *DedupFilter

// ❌ 不推荐（goroutine 会泄漏）
func NewDedupFilter(config DedupConfig) *DedupFilter
```

无参版本可使用 `context.Background()` 作为默认父级，但需提供 `Stop()` 方法供手动清理。

### 2. 插件内启动 goroutine 使用 `ctx.Spawn()`

插件 Setup 中通过 `ctx.Spawn()` / `ctx.SpawnNamed()` 启动的 goroutine 自动绑定到插件生命周期：

```go
func (p *MyPlugin) Setup(ctx *plugin.SetupContext) (any, error) {
    ctx.SpawnNamed("worker", func(ctx context.Context) {
        <-ctx.Done()  // 插件卸载或 bot 停止时取消
    })
    return p, nil
}
```

### 3. 短生命周期并发任务使用 `ctx.NewTaskGroup()`

需要并发执行一批短任务并等待结果时（如并发 API 请求），使用 `NewTaskGroup` 而非 `Spawn`：

```go
g := ctx.NewTaskGroup()
for _, url := range urls {
    url := url
    g.Go(func(taskCtx context.Context) error {
        return fetchWithCtx(taskCtx, url)
    })
}
if err := g.Wait(); err != nil {
    ctx.Log.Errorf("请求失败: %v", err)
}
```

区别：`Spawn` 启动的 goroutine 生命周期=**插件生命周期**，fire-and-forget 无需等待；`TaskGroup` 的 goroutine 生命周期受调用方控制，通过 `Wait()` 聚合结果和错误。

---

### 4. 不要把 Start 超时传入后台 goroutine

```go
// ❌ 错误：runCtx 继承了 Start 超时
startCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
manager.Start(startCtx)     // OnRun goroutine 会在 30s 后被取消

// ✅ 正确：Lifecycle Manager 内部已剥离超时
bot.Start(ctx)  // OnRun 不受 Start 超时影响
```

### 4. OnRun 必须处理 ctx.Done()

```go
// ❌ 错误：不监听 ctx，Stop 无法通知退出
func (c *MyComponent) OnRun(ctx context.Context) error {
    for { time.Sleep(time.Second) }  // 死循环，永不退出
}

// ✅ 正确：监听 ctx.Done()
func (c *MyComponent) OnRun(ctx context.Context) error {
    for {
        select {
        case <-ctx.Done():
            return nil
        case <-c.ticker.C:
            // do work
        }
    }
}
```

---

## 实现参考

### 模式 A — Lifecycle Manager

`lifecycle/lifecycle.go:483-489`：

```go
base := context.WithoutCancel(ctx)          // 剥离超时
m.parentCtx, m.parentCancel = context.WithCancel(base)
m.runCtx, m.runCancel = context.WithCancel(m.parentCtx)
```

### 模式 B — WithContext 组件

`middleware/dedup.go:96-111`：

```go
func NewDedupFilterWithContext(parent context.Context, config DedupConfig) *DedupFilter {
    ctx, cancel := context.WithCancel(parent)
    f := &DedupFilter{
        ctx:    ctx,
        cancel: cancel,
        config: config,
    }
    go f.cleanupLoop()  // 监听 ctx.Done()
    return f
}
```

---

## 相关文档

- [并发事件处理](./CONCURRENT_EVENT_PROCESSING.md)
