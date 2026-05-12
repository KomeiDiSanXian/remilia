# Per-Channel Engine——按频道隔离的事件引擎

> 当同一个 Bot 接入多个 Discord 服务器或 QQ 群时，所有频道共享一个 Engine——A 频道的匹配器变更会影响 B 频道。Per-Channel Engine 为每个频道（guild/group/private chat）维护独立的 Engine 实例，支持模板同步和空闲回收。

## 问题背景

单引擎架构的共享问题：

| 场景 | 问题 |
|------|------|
| 多服务器 | A 服的插件注册了 `/weather`，B 服也看到了这个命令 |
| 频道级配置 | 每个频道的需求不同——A 服启用 moderation 插件，B 服禁用 |
| 命令冲突 | 两个插件在不同频道注册了同名命令 |
| 插件生命周期 | 一个频道的插件卸载不应影响其他频道 |

解决方案：每个频道一个 Engine 实例，共享一个模板 Engine（定义全局匹配器）。

## 核心设计

### engine.go 扩展

```go
type Engine struct {
    // ... 原有字段

    // v1.2.0 新增
    templateVer atomic.Int64    // 模板版本号，检测是否需要 sync
    fork        *forkState      // fork 状态（nil 表示非 fork 引擎）
}

type forkState struct {
    template     *Engine        // 模板引擎引用
    lastSyncVer  int64          // 上次同步的模板版本
    syncedAt     time.Time
}
```

### ForkFrom：创建频道引擎

```go
func (e *Engine) ForkFrom(template *Engine, opts ...Option) *Engine {
    forked := e.forkFrom(template, opts...)
    forked.syncTemplates()
    return forked
}

func (e *Engine) forkFrom(template *Engine, opts ...Option) *Engine {
    forked := &Engine{
        state:       infraatomic.NewValue(template.state.Load()),
        middleware:  infraatomic.NewValue(template.middleware.Load()),
        writeMu:     sync.Mutex{},
        shutdown:    atomic.Bool{},
        eventWg:     sync.WaitGroup{},
        templateVer: atomic.Int64{},
        fork: &forkState{
            template:    template,
            lastSyncVer: 0,
        },
    }
    for _, opt := range opts {
        opt(forked)
    }
    return forked
}
```

Fork 瞬间同步模板的 matcher 列表。此后，fork 引擎可以独立注册/删除匹配器（COW 复制自己的 state），模板的变更不会自动传播——通过 syncTemplates 机制按需同步。

### syncTemplates：按需同步

```go
func (e *Engine) syncTemplates() {
    if e.fork == nil {
        return
    }
    template := e.fork.template
    templateVer := template.templateVer.Load()
    if e.fork.lastSyncVer == templateVer {
        return
    }
    // 复制模板的 matcher 列表到当前引擎
    templateState := template.state.Load()
    currentState := e.state.Load()
    merged := mergeMatchers(templateState.matchers, currentState.matchers)
    e.state.Store(&state{
        matchers:     merged,
        matcherIndex: rebuildIndex(merged),
        commandIndex: rebuildCommandIndex(merged),
        groupIndex:   rebuildGroupIndex(merged),
        block:        currentState.block,
        maxMatchers:  currentState.maxMatchers,
    })
    e.fork.lastSyncVer = templateVer
}
```

### bumpVersion：模板版本递增

所有修改 matcher 的操作（registerMatcher, DeleteMatcher, DeleteMatchers, BatchRegisterMatchers, RemoveGroup）之后调用 `bumpVersion`：

```go
func (e *Engine) bumpVersion() {
    e.templateVer.Add(1)
}
```

### processEventGuard：惰性同步

```go
func (e *Engine) processEventGuard(ctx *context.Context) {
    if e.fork != nil {
        templateVer := e.fork.template.templateVer.Load()
        if e.fork.lastSyncVer != templateVer {
            e.syncTemplates()
        }
    }
    // ... 原有事件处理逻辑
}
```

在 `ProcessEvent` 入口处 check 模板版本，发现变更则触发 syncTemplates。保证每次事件处理时 matcher 列表是最新的。

### EngineManager

```go
type ChannelKey struct {
    Platform string
    ChatID   string
}

type EngineManager struct {
    engines       sync.Map    // map[ChannelKey]*Engine
    template      *Engine     // 所有频道引擎的模板
    evictDuration time.Duration  // 空闲回收时间
    metrics       MetricsCollector
}

func (m *EngineManager) GetOrCreate(key ChannelKey) *Engine {
    actual, loaded := m.engines.LoadOrStore(key, m.template.ForkFrom(m.template))
    if !loaded {
        m.metrics.OnEngineCreated()
    }
    return actual.(*Engine)
}

func (m *EngineManager) evictIdle() {
    m.engines.Range(func(key, value any) bool {
        eng := value.(*Engine)
        // 检测是否空闲：最近一次事件处理时间 > evictDuration
        if eng.IsIdle(m.evictDuration) {
            m.engines.Delete(key)
            m.metrics.OnEngineEvicted()
        }
        return true
    })
}

func (m *EngineManager) Stats() EngineManagerStats {
    stats := EngineManagerStats{}
    m.engines.Range(func(key, value any) bool {
        stats.TotalEngines++
        stats.ActiveEngines++
        return true
    })
    return stats
}
```

### 完整路由

```go
// bot.go 三阶段路由
func (b *Bot) handlePlatformEvent(event platform.Event) {
    key := makeChannelKey(event)

    // Phase 1: EngineManager — 频道级隔离
    if b.engineManager != nil {
        if eng := b.engineManager.GetOrCreate(key); eng != nil {
            eng.ProcessPlatformEvent(event, b.getSender(event))
            return
        }
    }

    // Phase 2: Router — 策略链
    if b.router != nil {
        if b.router.Route(event, b.getSender(event)) {
            return
        }
    }

    // Phase 3: 默认 Engine — 兼容旧行为
    b.engine.ProcessPlatformEvent(event, b.getSender(event))
}
```

## 用法示例

```go
// 创建模板引擎
template := engine.NewEngine()

// 创建 EngineManager
em := engine.NewEngineManager(template, engine.WithEvictDuration(30*time.Minute))

// 每个频道自动获得独立引擎
// 修改模板会影响所有频道（通过 syncTemplates 同步）
template.RegisterMatcher(myMatcher)

// 频道级别的专属匹配器
channelEng := em.GetOrCreate(channelKey)
channelEng.RegisterMatcher(channelOnlyMatcher)  // 仅该频道可见
```

## 文件清单

```
core/engine/
├── fork.go          # ForkFrom, syncTemplates, bumpVersion, forkState
├── manager.go       # EngineManager: GetOrCreate, evictIdle, Stats, Close
├── channel.go       # ChannelKey, makeChannelKey, 频道工具函数
```

## 依赖

- `core/engine`：自身扩展，向后兼容
- `router`：可选，若无 Router 则直接 fallback 到单引擎

## 设计权衡

| 方面 | 选择 | 理由 |
|------|------|------|
| 同步策略 | 惰性同步（事件处理时检测版本） | 避免模板每次变更时广播到所有 fork |
| 版本检测 | atomic.Int64 | 无锁，O(1) 检测 |
| 空闲回收 | 独立 goroutine 定期 evictIdle | 防止频道数过多导致内存膨胀 |
| COW 状态独立 | fork 引擎修改不影响模板 | 频道级隔离，不改模板逻辑 |
| 三阶段路由 | engineManager > router > engine | 分层清晰，每个阶段各司其职 |
| 模板变更传播 | syncTemplates 合并策略 | 模板 matcher + fork 独立 matcher 共存 |
