# COW（Copy-On-Write）无锁引擎——高性能事件处理核心

> **ZeroBot 基因**：ZeroBot 的 Engine 使用 `sync.Mutex` 保护匹配器列表。COW 引擎是对这一模式的彻底重构——从"有锁共享"变为"无锁不可变"。参阅 [`11-zerobot-inspiration.md`](11-zerobot-inspiration.md#32-关键分叉点-①并发模型) 了解分叉动机。

## 问题背景

在机器人框架中，事件引擎是最核心的组件。它需要：

1. **高并发读取**：每秒处理数十万条消息，每条消息都需要遍历匹配器列表
2. **低频写操作**：匹配器的注册/删除频率远低于事件处理
3. **无锁安全**：读操作不能阻塞，也不允许数据竞争

传统的 `sync.RWMutex` 方案在高并发下存在严重的缓存行乒乓（cache-line bouncing）问题：即使读锁允许并发，锁变量的原子操作仍会导致多核 CPU 的缓存一致性协议（MESI）频繁同步，大幅降低吞吐量。

## 设计目标

- 读操作**完全无锁**，消除锁竞争开销
- 写操作不阻塞正在进行的读操作
- 内存分配可控，热路径零分配
- 简单直观的正确性推理

## 核心实现

### 不可变状态

引擎状态被建模为不可变结构体 `state`，包含所有匹配器索引和缓存：

```go
type state struct {
    matchers        []*Matcher
    matcherIndex    map[EventType][]*Matcher
    commandIndex    map[string]map[EventType][]*Matcher
    groupIndex      map[string][]*Matcher
    sortedCache     map[EventType][]*Matcher
    commandInfoCache map[string]*CommandInfo
    commandListCache []CommandInfo
    commandListVer   int64
    block           bool
    maxMatchers     int
}
```

所有 map 和 slice 在创建后不再修改——修改操作会创建新的副本。

### 原子状态切换

```go
type Engine struct {
    state      *infraatomic.Value[*state]           // 引擎核心状态
    middleware *infraatomic.Value[*middlewareState]  // 中间件配置
    writeMu    sync.Mutex                            // 仅写操作持有
    shutdown   atomic.Bool
    eventWg    sync.WaitGroup
}
```

`infraatomic.Value` 是基于 `atomic.Value` 的泛型封装，提供类型安全的 `Load()` 和 `Store()`。

### 读操作：完全无锁

```go
func (e *Engine) GetMatcherCount() int {
    return len(e.state.Load().matchers)
}
```

一次 `atomic.Load` 拿到整个状态快照，后续所有操作都在快照上进行。多个 goroutine 同时读取各自看到的可能是不同的版本号，但每个版本都是完整的、一致的状态——这就是 COW 的核心保证。

### 写操作：复制-修改-替换

```go
func (e *Engine) registerMatcher(m *Matcher) *Matcher {
    e.writeMu.Lock()
    defer e.writeMu.Unlock()

    oldState := e.state.Load()
    e.state.Store(oldState.withAddedMatcher(m))
    return m
}
```

写操作的完整流程：
1. 持有 `writeMu`
2. `Load()` 当前状态
3. 调用 `withAddedMatcher`（或 `withDeletedMatcher` 等）创建新状态
4. `Store()` 新状态原子替换

`withAddedMatcher` 的实现体现了 COW 的精髓——共享不变部分：

```go
func (s *state) withAddedMatcher(m *Matcher) *state {
    newMatchers := append(append([]*Matcher(nil), s.matchers...), m)
    // 仅复制必要的 map——其他字段共享原对象
    newMatcherIndex := copyMap(s.matcherIndex)
    // ... 更新索引 ...
    return &state{
        matchers:     newMatchers,
        matcherIndex: newMatcherIndex,
        commandIndex: s.commandIndex,  // 共享，后续写时才复制
        groupIndex:   s.groupIndex,    // 共享
        sortedCache:  s.sortedCache,   // 下一次读取会重建
        block:        s.block,
        maxMatchers:  s.maxMatchers,
    }
}
```

## 性能数据

| 指标 | 值 | 说明 |
|------|-----|------|
| 空 Handler 吞吐量 | **~475,000 msg/s** | 16 核 CPU 80%，GOMAXPROCS=16 |
| ProcessEvent 延迟 | **~5-6 μs/op** | COW 无锁读取 + 6 路合并排序 |
| Context 分配 | **~272 B/op, 3 allocs** | 新鲜分配（去池化，消除 UAF） |
| 堆内存（50,000 msg/s） | **~12-14 MB** | 极低内存占用，无泄漏 |

相比传统的 `sync.RWMutex` 实现，COW 模型在读多写少场景下性能提升约 **5-6 倍**，内存分配降低 **93%**。

## 关闭语义

```go
func (e *Engine) Shutdown(ctx stdctx.Context) error {
    e.shutdown.Store(true)     // 1. 阻止新事件进入
    e.internals.stopAll()      // 2. 停止后台 goroutine
    // 3. 等待所有活跃的 ProcessEvent 完成
    done := make(chan struct{})
    go func() { e.eventWg.Wait(); close(done) }()
    select {
    case <-done:
        return nil
    case <-ctx.Done():
        return ctx.Err()
    }
}
```

使用 `atomic.Bool`（shutdown 标志）+ `sync.WaitGroup`（eventWg）替代了原有的 `eventGate` sentinel 编码设计，性能提升约 **3 倍**且语义更直观。

> 上面是简化示意。当前完整的关闭序列为：置 shutdown 标志 → 停止后台组件并等待退出
> （含 TempManager 水位清理 goroutine）→ 等待在途 ProcessEvent → Drain ExecPool
> （共享池除外，其生命周期归调用方）→ Drain Dispatcher → `FlushPendingDeletes`
> 收尾批量删除队列。每一步都受 ctx 超时控制。

## 迭代过程

### V1：裸 `atomic.Value` + 手动类型断言

初始版本 COW 引擎使用标准库 `atomic.Value`，每次 Load 后需要手动类型断言：

```go
// V1 代码 — 类型不安全，每次 Load 都需要断言
type Engine struct {
    state      atomic.Value // 存储 *engineState
    middleware atomic.Value // 存储 *middlewareState
    writeMu    sync.Mutex
}

// 每次读取都必须手动断言
func (e *Engine) GetMatcherCount() int {
    return len(e.state.Load().(*engineState).matchers)
}
```

**缺点**：
- 调用方必须记住返回类型，类型断言错误在编译期不报错，运行时才 panic
- 代码冗长，可读性差
- 没有统一的创建方式（`NewValue` vs 直接 `atomic.Value{}`）

### V2：`infraatomic.Value[T]` 泛型封装

引入泛型包装，消除类型断言：

```go
// V2 — infra/atomic/value.go
type Value[T any] struct {
    inner atomic.Value
}

func NewValue[T any](v T) *Value[T] {
    val := &Value[T]{}
    val.Store(v)
    return val
}

func (v *Value[T]) Load() T {
    return v.inner.Load().(T)  // 类型断言隐藏在泛型方法中
}

func (v *Value[T]) Store(val T) {
    v.inner.Store(val)
}
```

**优势**：
- 编译期类型安全——错误类型在编译时就报错
- 调用方不再需要写类型断言
- 统一的构造方式 `NewValue(initialState)`
- 后续可扩展（如添加快照、指标收集等通用功能）

这个模式也被 `syncx.Map`、`syncx.Lazy` 等泛型包装复用，贯穿整个 `infra/` 包的设计。

### V3：COW 写操作优化——从全量复制到选择性复制

初始的 COW 实现每次写操作都复制所有 map，对于只有少量索引变更的场景非常浪费：

```go
// V1 方式：无差别全量复制
func (s *state) withAddedMatcher(m *Matcher) *state {
    newState := &state{
        matchers:     copySlice(s.matchers),
        matcherIndex: copyMap(s.matcherIndex),     // 全部复制
        commandIndex: copyMap(s.commandIndex),      // 全部复制
        groupIndex:   copyMap(s.groupIndex),        // 全部复制
        sortedCache:  copyMap(s.sortedCache),       // 全部复制
        // ...
    }
    // 再修改
    newState.matchers = append(newState.matchers, m)
    newState.matcherIndex[m.EventType] = append(newState.matcherIndex[m.EventType], m)
    return newState
}
```

优化后只复制必要的索引，其他共享：

```go
// V2 优化：选择性复制 + 共享不变部分
func (s *state) withAddedMatcher(m *Matcher) *state {
    newMatchers := append(append([]*Matcher(nil), s.matchers...), m)
    newMatcherIndex := copyMap(s.matcherIndex)
    // commandIndex 只在命令类匹配器注册时才复制
    var newCommandIndex map[string]map[EventType][]*Matcher
    if m.isCommandMatcher() {
        newCommandIndex = copyCommandIndex(s.commandIndex)
    }
    return &state{
        matchers:     newMatchers,
        matcherIndex: newMatcherIndex,
        commandIndex: newCommandIndex,     // 非命令时不复制，共享
        groupIndex:   s.groupIndex,        // 共享（只追加时不需要复制）
        sortedCache:  nil,                 // 惰性重建
        block:        s.block,
        maxMatchers:  s.maxMatchers,
    }
}
```

`BatchRegisterMatchers` 进一步将批量注册的复制次数从 O(n) 降到 O(1)：

```go
func (e *Engine) BatchRegisterMatchers(matchers []*Matcher) []*Matcher {
    e.writeMu.Lock()
    defer e.writeMu.Unlock()
    // 只需一次 COW 复制，一次 Store
    oldState := e.state.Load()
    e.state.Store(oldState.withAddedMatchers(matchers)) // 批量添加
    return matchers
}
```

在插件初始化批量注册匹配器的场景下，性能提升 **3-5 倍**。

### V4：关闭语义改进——从 sentinel 编码到 `atomic.Bool` + `WaitGroup`

最初的关闭机制使用 `eventGate` 结构体，通过 `atomic.Int64` 的 sentinel 编码来同时追踪 shutdown 状态和活跃事件数：

```go
// V1 关闭机制：eventGate sentinel 编码
const eventGateShutdownSentinel = int64(-1) << 40 // -1,099,511,627,776

type eventGate struct {
    n            atomic.Int64   // 正常运行≥0，shutdown 后 < sentinel
    zeroCh       chan struct{}  // 活跃归零时关闭
    signalOnce   sync.Once     // 确保 zeroCh 只关闭一次
    shutdownOnce sync.Once     // 确保 sentinel 只加一次
}

func (g *eventGate) acquire() bool {
    for {
        n := g.n.Load()
        if n < 0 { return false } // 已 shutdown
        if g.n.CompareAndSwap(n, n+1) { return true }
    }
}

func (g *eventGate) release() {
    if n := g.n.Add(-1); n == eventGateShutdownSentinel {
        g.signalOnce.Do(func() { close(g.zeroCh) })
    }
}

func (g *eventGate) shutdown() {
    g.shutdownOnce.Do(func() {
        if n := g.n.Add(eventGateShutdownSentinel); n == eventGateShutdownSentinel {
            g.signalOnce.Do(func() { close(g.zeroCh) })
        }
    })
}
```

**缺点**：
- sentinel 编码过于精巧，正确性需要仔细推理——sentinel 值选取不当可能导致整数溢出
- `acquire()` 的 CAS 循环在极端高竞争下有活锁风险
- `zeroCh` + `signalOnce` 的配合容易出错
- 代码维护成本高，新人难以理解

```go
// V2（当前）：atomic.Bool + sync.WaitGroup，语义简单直接
func (e *Engine) Shutdown(ctx stdctx.Context) error {
    e.shutdown.Store(true)     // 1. 标志位阻止新事件
    e.internals.stopAll()      // 2. 停止后台 goroutine
    done := make(chan struct{})
    go func() { e.eventWg.Wait(); close(done) }()
    select {
    case <-done:
        return nil
    case <-ctx.Done():
        return ctx.Err()
    }
}
```

**优势**：
- 语义直观：`shutdown` 是门控，`eventWg` 追踪活跃数
- 无 CAS 循环，无溢出风险
- 性能提升约 **3 倍**（从 CAS 循环降为 Load + Add/Wait）
- 新的 ProcessEvent 入口进不来（`e.shutdown.Load()` 检查），已在处理的等 `WaitGroup` 完成

### V5（2026-07）：COW 契约修复——"复制"不等于"可以就地修改"

core 深度复查发现选择性 COW 里埋着一个契约裂缝。为了让 append 触发重分配，
`copyCommandIndex`/`copyMatcherIndex` 用容量封顶的方式"复制"子切片：

```go
dst[k] = v[:len(v):len(v)]   // cap=len：append 会重分配 → COW 安全
                             // 但底层数组仍与旧 state 共享！
```

这个技巧对 **append** 是对的，对**就地排序**是灾难：`withUpdatedMatcherIndex` 和
`withBatchMatchers` 拿到"复制"出的切片后直接 `sort.Slice`——排序的 swap 发生在与
已发布旧 state 共享的底层数组上，正被 `processEventMatchers` 无锁遍历的读者会看到
元素凭空重复/丢失。触发条件全是运行期常规操作：对命令匹配器 `SetPriority`、
`BindCommand`、插件批量注册，同时有事件在处理。

修复原则：**排序之前必须拥有数组**——

```go
sorted := append([]*Matcher(nil), lst...)  // 逐元素完整拷贝，脱离共享
sortMatchersByPriority(sorted)
dst.commandIndex[cmd][et] = sorted
```

批量注册路径则只重排本批次 append 过的桶（append 已使其底层数组独立），
未触及的桶严禁排序。

**为什么 race detector 一直没抓到？** 因为没有任何测试在"事件处理中"并发做
`SetPriority`/批量注册——写路径的单元测试都是串行的。修复时补了指针快照断言的
回归测试（旧 state 切片在重排后必须保持原元素原顺序），串行即可验证契约，
不依赖竞态时序。

这一课的普适表述：*COW 结构里每一处"共享底层数组的浅复制"，都必须显式标注
"只可 append、不可原位改写"；任何 sort/copy/swap 前先问一句——这个数组是谁的？*

### V6（注册批处理）：顺序注册写放大与 `RegisterBatch`

COW 的顺序注册存在 O(n²) 写放大：每次 `OnCommand`/`Handle` 都会全量复制
commandIndex（复制所有命令键 + 内层 map + 切片头）。实测 1000 个命令
顺序注册约 800ms，而一次 `BatchRegisterMatchers` 仅 1.6ms（480 倍差距）。

修复分两层：

1. **Handle 首次绑定跳过命令 matcher 的无谓重建**：命令桶不过滤
   hasHandler（执行循环兜底检查）、优先级变更走 `SetPriority` 专属路径，
   首次绑定的重排是恒等操作却触发一次全量 commandIndex 复制。
   实测单独此项使 1000 命令注册 786ms → 261ms。

2. **`Engine.BeginRegisterBatch()` / `RegisterBatch.Flush()` 注册批处理会话**：
   会话期间所有注册（On/OnCommand/OnTemp）与批内 matcher 的链式索引维护
   （SetMatcherGroup/UpdateMatcherIndex/UpdateCommandCache/InvalidateSortedCache）
   自动降级为收集，Flush 以一次 `withBatchMatchers` 提交。
   插件 Manager 通过 `RegisterBatchStarter` 接口在插件 Setup 周围自动开启
   （1000 命令 → 1.6ms）。语义约束：批期注册的 matcher 在 Flush 前不可路由，
   插件 Setup 阶段无事件派发，安全。

```go
batch := e.BeginRegisterBatch()
for _, cmd := range commands {
    e.OnCommand("", cmd).Handle(handler)
}
batch.Flush() // 一次 COW 提交
```

## 迭代历程

| 版本 | 核心变化 | 动机 |
|------|---------|------|
| V1 | `atomic.Value` 裸用 | 快速实现 COW 原型 |
| V2 | `infraatomic.Value[T]` 泛型封装 | Go 1.26 泛型可用，消除类型断言 |
| V3 | 选择性 COW 复制 + BatchRegister | 批量场景性能优化 |
| V4 | `shutdown` + `eventWg` 替代 `eventGate` sentinel | 简化正确性推理，提升性能 |
| V5 | 修复就地排序打破 COW 契约的数据竞争 | 共享底层数组只可 append、不可原位改写 |
| V6（当前） | `RegisterBatch` 批处理会话 + Handle 命令重建跳过 | 顺序注册 O(n²) 写放大（1000 命令 786ms → 1.6ms） |

## 设计权衡

**优势**：
- 读操作完全无锁，多核扩展性极佳
- 正确性容易推理：不存在锁顺序、死锁、活锁等问题
- 实现简单，每步都是确定的复制-修改-替换

**代价**：
- 写操作有复制开销（批量注册时使用 `BatchRegisterMatchers` 可降低到单次复制）
- 内存短暂升高（旧状态在 GC 时才会释放）
- 读操作可能看到稍旧的状态（但最终一致性对事件处理完全可接受）

## 适用场景

COW 模型最适用于**读多写少**的场景——这正是框架引擎的完美匹配。匹配器注册在启动阶段和插件加载时发生，而事件处理是持续高并发的。对于写频繁的场景（如每事件创建一个新匹配器），COW 的复制开销会成为瓶颈，需谨慎评估。
