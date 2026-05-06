# 生命周期管理——优雅启动、有序关闭

## 设计目标

一个分布式系统组件的生命周期管理需要解决：

1. **启动顺序**：组件之间有依赖关系，必须先启动基础服务再启动上层业务
2. **优雅关闭**：收到停止信号后，必须先停止上层业务，再关闭基础服务
3. **错误回滚**：启动过程中某个组件失败，已启动的组件应被回滚
4. **Context 语义清晰**：OnStart、OnRun、OnStop 各自使用不同语义的 Context
5. **幂等停止**：Stop 多次调用应该安全

## 核心抽象：Component 接口

```go
type Component interface {
    Name() string
    OnStart(ctx context.Context) error  // 初始化（非阻塞）
    OnRun(ctx context.Context) error    // 运行（阻塞）
    OnStop(ctx context.Context) error   // 清理（幂等）
}
```

**关键设计决策**：OnRun 的 `ctx` 参数是**运行时 Context**（v2 的核心改进）。

```go
type MyAdapter struct {
    events chan Event
}

func (a *MyAdapter) OnRun(ctx context.Context) error {
    for {
        select {
        case <-ctx.Done():
            return nil  // ⚡ 响应停止信号
        case event := <-a.events:
            a.handleEvent(event)
        }
    }
}
```

OnStart 的 ctx 控制的是"初始化本身的超时"（如连接数据库的超时），OnRun 的 ctx 才是"组件应当运行的时长控制"。这种分离解决了 v1 中 Context 语义不清晰的问题。

## Manager：统一编排

### 状态机

```
StateCreated → StateStarting → StateRunning → StateStopping → StateStopped
                    │                                │
                    ↓                                │
              失败回滚（StateStopped）                 │
                    ↑                                │
                    └────────────────────────────────┘
```

### 启动过程

```go
func (m *Manager) Start(ctx context.Context) error {
    if !m.transition(StateStarting) {
        return ErrInvalidState
    }
    defer m.transition(StateRunning)

    for _, comp := range m.components {
        if err := comp.OnStart(ctx); err != nil {
            m.rollback(comp)  // 回滚已启动的组件
            return fmt.Errorf("component %s start failed: %w", comp.Name(), err)
        }
    }

    // 创建运行时 Context，在 Stop 时取消
    m.runCtx, m.runCancel = context.WithCancel(context.Background())

    for _, comp := range m.components {
        c := comp
        go func() {
            defer m.handleRunExit(c)
            err := c.OnRun(m.runCtx)
            // 处理 OnRun 返回错误
        }()
    }
    return nil
}
```

**回滚机制**：`rollback(comp)` 从失败组件的上一个组件开始，逆序调用 `OnStop`，确保已分配的资源被完全释放。回滚过程中的错误会聚合返回，不会中断其他组件的回滚。

### 停止过程

```go
func (m *Manager) Stop(ctx context.Context) error {
    if !m.transition(StateStopping) {
        return ErrInvalidState
    }

    // 1. 取消运行时 Context → 所有 OnRun 收到 Done()
    if m.runCancel != nil {
        m.runCancel()
    }

    // 2. 等待所有 OnRun goroutine 退出
    m.wg.Wait()

    // 3. 逆序调用 OnStop
    errs := make([]error, 0)
    for i := len(m.components) - 1; i >= 0; i-- {
        if err := m.components[i].OnStop(ctx); err != nil {
            errs = append(errs, err)
        }
    }

    m.transition(StateStopped)
    return errors.Join(errs...)
}
```

停止三阶段：
1. **取消 runCtx**：通知所有运行中的 goroutine 准备退出
2. **等待 OnRun 归零**：使用 `sync.WaitGroup` 追踪所有 OnRun goroutine
3. **逆序 OnStop**：按注册的逆序清理资源

### 双层 Context 设计

```
Bot.Start()
    │
    ├─ parentCtx（Bot 根 context）
    │   ├─ 在 lifecycle.Start() 时创建
    │   ├─ 在 lifecycle.Stop() 全部完成后取消
    │   └─ 可用于后台 goroutine、健康检查等
    │
    └─ runCtx（运行时 context）
        ├─ 在 lifecycle.Start() 的 OnStart 完成后创建
        ├─ 在 lifecycle.Stop() 时立即取消
        └─ 传递给每个组件的 OnRun()
```

设计要点：
- `parentCtx` 在停止阶段仍然有效——插件 `Teardown` 阶段还可以发消息
- `runCtx` 取消后，OnRun 应立即退出——不对运行时资源做进一步操作
- 这种双层设计隔离了"组件运行"和"组件清理"两个阶段的资源访问策略

### SimpleComponent——零样板组件

```go
comp := lifecycle.NewSimpleComponent(
    "my-component",
    func(ctx context.Context) error { return nil },           // onStart
    func(ctx context.Context) error { <-ctx.Done(); return nil },  // onRun
    func(ctx context.Context) error { return nil },           // onStop
)
```

无 `onRun` 时默认行为是 `<-ctx.Done()`——即只保持组件在运行状态，不做实际工作（用于纯资源管理场景）。

### ResourceComponent——资源生命周期绑定

```go
comp := lifecycle.NewResourceComponent(
    "database",
    func(ctx context.Context) (interface{}, error) {
        return sql.Open("postgres", dsn)  // 打开资源
    },
    func(ctx context.Context, res interface{}) error {
        return res.(*sql.DB).Close()      // 清理资源
    },
)
```

资源的打开对应 OnStart，关闭对应 OnStop，中间的长生命周期由框架管理。

## Bot 中的实践

### 启动顺序

```
1. Engine 初始化（无实际工作，仅创建数据结构）
2. Platform Adapter 启动（开始接收事件）
3. Plugin Manager 启动（加载插件，注册 Matcher）
```

### 停止顺序（逆序）

```
1. Plugin Manager Stop（插件 Teardown → 此时 parentCtx 仍有效）
2. Platform Adapter Stop（断开平台连接 → 不再接收新事件）
3. Engine Shutdown（等待在途事件完成 → 释放索引）
```

这种顺序确保：插件在关闭时仍能通过平台 API 发送"正在关闭"的消息。

### 优雅关闭

```go
func (b *Bot) WaitForShutdown(timeout ...time.Duration) {
    sigCh := make(chan os.Signal, 2)
    signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

    <-sigCh // 第一次信号 → 优雅关闭
    logger.Info("Shutting down gracefully... (Ctrl+C again to force)")

    done := make(chan struct{})
    go func() {
        ctx, cancel := context.WithTimeout(b.Context(), shutdownTimeout)
        defer cancel()
        b.Stop(ctx)
        close(done)
    }()

    select {
    case <-done:        // 优雅关闭完成
    case <-sigCh:       // 第二次信号 → 强制退出
        logger.Warn("Forced exit by second signal")
        os.Exit(1)
    }
}
```

双信号设计：首次 SIGINT 触发优雅关闭，等待后台清理完成；再次 SIGINT 强制退出。这符合大多数 CLI 工具的行为预期。

### 全局单例保护

```go
var shutdownListenerActive atomic.Bool

func acquireShutdownListener(caller string) bool {
    if !shutdownListenerActive.CompareAndSwap(false, true) {
        logger.Warn("WaitForShutdown already active")
        return false
    }
    return true
}
```

确保一个进程内只有一个 `WaitForShutdown` 在监听信号。多 Bot 场景使用 `BotManager.WaitForShutdown` 统一管理。

## 迭代过程

### V0：无生命周期管理（Bot 内嵌启动逻辑）

初始版本没有独立的生命周期管理。Bot 直接在 `Start()` 方法里手动编排启动顺序，Stop 同理：

```go
// V0 代码 — bot.go 内嵌所有启动逻辑
type Bot struct {
    wh     webhook.WebHook
    tm     *token.Manager
    engine *Engine
    api    openapi.OpenAPI
    srv    *http.Server
    stopCh chan struct{}
    wg     sync.WaitGroup
    ctx    context.Context
    cancel context.CancelFunc
}

func (b *Bot) Start() error {
    // 1. 创建根 context
    b.ctx, b.cancel = context.WithCancel(context.Background())

    // 2. 启动 Token 刷新
    err := b.tm.Start(b.ctx)

    // 3. 启动 Webhook
    b.wg.Add(1)
    go b.wh.Start(b.ctx, b.handleEvent)

    // 4. 启动 HTTP Server
    go b.srv.ListenAndServe()
    return nil
}

func (b *Bot) Stop(ctx context.Context) error {
    // 硬编码逆序停止
    err1 := b.srv.Shutdown(ctx)   // 先停 HTTP
    err2 := b.wh.Stop(ctx)        // 再停 Webhook
    b.cancel()                     // 取消根 context
    b.wg.Wait()                    // 等待 goroutine
    return errors.Join(err1, err2)
}
```

**问题**：
- 启动/停止逻辑跟 Bot 类型绑定——Engine 的清理器、Plugin Manager 等新组件加入时必须修改 `Bot.Start()`
- 新增组件时容易忘记在 Stop 中处理，导致资源泄漏
- Context 只有一个，全部组件共享——插件 Teardown 时 parentCtx 已经被取消，无法使用平台 API
- 没有状态机，多次调用 `Stop()` 可能导致 panic（`close(b.stopCh)` 重复关闭）
- 没有回滚机制——如果第 3 步启动失败，第 2 步已启动的组件不会回滚

### V1：Context v1 改造

中期引入了一些 Context 的改进，但核心问题未解决：

```go
// V1 Context 改造 — 区分了 Start 和 Stop 的 Context
func (b *Bot) Start() error {
    startCtx, startCancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer startCancel()
    // startCtx 用于控制启动超时
    if err := b.tm.Start(startCtx); err != nil {
        return err
    }
    // ...
}

func (b *Bot) Stop(ctx context.Context) error {
    // ctx 由调用方传入，控制停止超时
    // ...
}
```

**问题依然存在**：
- Bot 仍然是所有组件的编排者
- 没有通用的 Component 接口——每个组件的生命周期方法签名不统一
- 插件 Teardown 时 parentCtx 已被取消的问题未解决（见下文 V2 双层 Context）

### V2：lifecycle 独立包 + Component 接口（当前）

关键决策：将生命周期管理从 Bot 中抽取为独立的 `lifecycle` 包，Bot 只是它的一个使用者：

```go
// V2 — lifecycle 包的核心
type Component interface {
    Name() string
    OnStart(ctx context.Context) error  // ctx 控制 OnStart 本身的超时
    OnRun(ctx context.Context) error    // ctx 是运行时 context
    OnStop(ctx context.Context) error   // ctx 控制 OnStop 本身的超时
}

type Manager struct {
    mu         sync.Mutex
    state      State               // 状态机
    components []Component
    runCtx     context.Context     // 运行时 context
    runCancel  context.CancelFunc
    wg         sync.WaitGroup      // 追踪 OnRun goroutine
    parentCtx  context.Context     // 根 context（Stop 全部完成后才取消）
}
```

**双层 Context 解决的核心问题**：

```
场景：插件 Teardown 时需要发送"机器人正在关闭"的消息
┌─ 错误的做法（V0/V1）────
bot.Stop()
    ├─ cancel(parentCtx)     ← 所有 Context 都取消了
    ├─ plugin.Teardown()     ← ctx.Reply() 立刻失败
    └─ adapter.Stop()
└────────────────────────

┌─ 正确的做法（V2）────────
bot.Stop()
    ├─ cancel(runCtx)        ← 运行时 Context 取消
    ├─ wait OnRun goroutine
    ├─ plugin.Teardown()     ← parentCtx 仍有效，ctx.Reply() 成功
    ├─ adapter.Stop()
    └─ cancel(parentCtx)     ← 最后才取消根 Context
└────────────────────────
```

**停止顺序如何保证**：
```go
// lifecycle.Stop() — 逆序执行 OnStop
for i := len(m.components) - 1; i >= 0; i-- {
    m.components[i].OnStop(ctx)  // 插件(3) → 适配器(2) → Engine(1)
}
```

顺序通过注册顺序控制：
```go
// Bot.Start() 中注册顺序
b.lifecycle.Register(engineComp)   // 1
b.lifecycle.Register(adapterComp)  // 2
b.lifecycle.Register(pluginComp)   // 3 — 逆序时先执行
```

**状态机保证幂等性**：

```go
func (m *Manager) transition(target State) bool {
    // 1. StateCreated → StateStarting（Start 调用）
    // 2. StateStarting → StateRunning（Start 完成）
    // 3. StateRunning → StateStopping（Stop 调用）
    // 4. StateStopping → StateStopped（Stop 完成）
    // StateStopped 是终止状态，任何操作都返回错误
    if !m.state.CAS(expected, target) {
        return false
    }
    return true
}
```

这意味着 `Stop()` 调用两次时，第二次直接返回错误，不会 panic。

**回滚机制处理启动失败**：

```go
// 组件 C 启动失败时，回滚 A 和 B
func (m *Manager) rollback(failedComp Component) {
    // 找到 failedComp 之前的组件（已成功 OnStart 的）
    for i := len(m.components) - 1; i >= 0; i-- {
        if m.components[i].Name() == failedComp.Name() {
            break
        }
        // 逆序调用 OnStop
        m.components[i].OnStop(ctx)
    }
}
```

引擎清理器（Cleaner、PendingDeleteProcessor）也通过 `SimpleComponent` 管理：

```go
// engine/services.go — 通过 runtimeComponent 接口统一管理
func (e *Engine) Shutdown(ctx context.Context) error {
    e.shutdown.Store(true)
    e.internals.stopAll()   // 停止所有后台组件
    e.internals.waitAll(ctx)
    e.eventWg.Wait()
    return nil
}
```

## 迭代历程

| 版本 | 核心变化 | 解决的问题 |
|------|---------|-----------|
| V0 | Bot 内嵌启动逻辑 | 快速实现原型 |
| V1 | Context 区分 Start/Stop Context | 启动超时可控 |
| V2（当前） | lifecycle 独立包 + Component 接口 + 双层 Context + 状态机 + 回滚 | 组件化、幂等、有序关闭 |

## 设计对比

| 方面 | 传统方案 | Remilia |
|------|---------|---------|
| Context 语义 | 一个 Context 贯穿始终 | OnStart/OnRun/OnStop 各有语义 |
| 启动失败 | 可能泄露资源 | 自动回滚已启动组件 |
| 停止幂等 | 需自行保证 | 状态机保证 |
| 组件依赖 | 手动编排 | RegisterOrdered 显式声明 |
| 信号处理 | 每个组件各自监听 | 统一 Manager 协调 |
