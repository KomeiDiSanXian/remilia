# [ARCHIVED] Per-Channel Engine——通道级引擎隔离

> **状态：v1.3.0 已移除。** Per-Channel Engine 架构已被 Matcher 级 per-channel blocking 替代。
> 详见 `plugin/scope.go` 中的 `Matcher.BlockForChannel()` 和 `notes/16-plugin-scope.md`。
>
> 移除原因：shared `*Matcher` 指针导致 Block 隔离不生效；每 channel 2+ 后台 goroutine 开销。
> 新方案使用单例 Engine + per-channel block metadata，删除 ~700 行代码。

## 核心设计

### EngineManager

```go
type EngineManager struct {
    template  *Engine
    instances sync.Map       // map[ChannelKey]*Engine
    maxIdle   time.Duration
    createMu  sync.Mutex     // 保护并发首次创建
    stopGC    chan struct{}
    once      sync.Once
}
```

- **template**：全局模板引擎，插件注册到此引擎
- **instances**：per-channel 引擎实例池（惰性创建）
- **createMu**：防止并发首次访问时重复创建
- **GC**：首次 Dispatch 自动启动，每 5 分钟淘汰空闲引擎

### Fork + syncTemplates

```go
func (e *Engine) ForkFrom(template *Engine, channelKey ChannelKey)
```

ForkFrom 在创建子引擎时同步以下资源：
- **Matchers**：从模板 COW 复制 matcher 列表（指针共享）
- **Middleware**：通过 `copyMiddlewareState` 复制模板的中间件链
- **ExecPool**：共享模板的线程池，避免每个 channel 独立创建

```
template Engine                    channel Engine (fork)
┌───────────────────┐           ┌──────────────────────┐
│ state (matchers)  │──指针共享→│ state (matchers)     │
│ middlewareState   │──COW复制→│ middlewareState       │
│ ExecPool          │──指针共享→│ ExecPool (同一池)     │
│ templateVer       │           │ fork.templateVer     │
│                   │           │ fork.lastUsed        │
└───────────────────┘           └──────────────────────┘
```

### 懒同步

每次 ProcessEvent → processEventGuard 检查：
```go
if e.fork != nil && e.fork.template.Version() != e.fork.templateVer {
    e.syncTemplates()
}
```

模板的 bumpVersion 在 matcher 增删改时自动递增（registerMatcher、DeleteMatcher、BatchRegisterMatchers、RemoveGroup 等写操作均埋点）。

### GC 生命周期

- 首次 Dispatch → `sync.Once` → `go gcLoop()`
- `gcLoop` 每 5 分钟扫描 `sync.Map`，淘汰 `LastUsed > maxIdle` 的引擎
- Bot.Stop → lifecycle 调用 `em.Close()` → 关闭 stopGC 通道 → gcLoop 退出

### 并发安全

首次访问同一 channel 时，`createMu` 确保 `newChannelEngine` 只执行一次：

```go
em.createMu.Lock()
if actual, ok := em.instances.Load(channelKey); ok {
    em.createMu.Unlock()
    actual.(*Engine).ProcessEvent(ctx)
    return
}
chEngine := em.newChannelEngine(channelKey)
em.instances.Store(channelKey, chEngine)
em.createMu.Unlock()
```

## 集成到 Bot

```go
engMgr := engine.NewEngineManager(template)
bot.UseEngineManager(engMgr)
rtr.WithEngineManager(engMgr)
```

handlePlatformEvent 中的路由：`router > engineManager > engine`

## 文件清单

```
core/engine/
├── channel.go   — ChannelKey, MakeChannelKey
├── fork.go      — ForkFrom, syncTemplates, Version, bumpVersion, touch, IsFork, LastUsed, forkState
├── manager.go   — EngineManager, Dispatch, newChannelEngine, GetChannel, Stats, Close, gcLoop, evictIdle
├── fork_test.go     — 7 tests
└── manager_test.go  — 7 tests
```

## 设计权衡

| 方面 | 选择 | 理由 |
|------|------|------|
| matcher 同步 | 指针共享 + 懒同步 | 避免全量复制，版本变化时惰性更新 |
| 中间件同步 | ForkFrom 时 COW 复制 | 使去重、限流等中间件对子引擎生效 |
| ExecPool | 共享模板池 | 避免 1000 channel × 64 goroutine |
| 创建竞态 | createMu | LoadOrStore 的 value 参数在 Go 中总是 eager evaluate |
| GC 启动 | 首次 Dispatch 自动 | 无需调用方手动 StartGC |
| 生命周期 | Bot.Stop → lifecycle → Close | GC goroutine 不泄漏 |
