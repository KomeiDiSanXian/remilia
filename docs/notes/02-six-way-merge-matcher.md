# 六路合并智能匹配器路由——从 O(n) 到可预测延迟

> **ZeroBot 基因**：ZeroBot 使用单一 `[]*Matcher` 线性扫描 + 每次注册全量排序。Remilia 将其拆分为三大索引（matcherIndex/commandIndex/groupIndex）+ 惰性缓存。参阅 [`11-zerobot-inspiration.md`](11-zerobot-inspiration.md#33-关键分叉点-②路由算法)。

## 问题背景

事件驱动框架的核心问题之一：事件到达后，需要从大量匹配器（Matcher）中找到那些应该处理该事件的匹配器。最朴素的做法是**线性遍历**——遍历所有匹配器，逐一检查规则是否匹配。当匹配器数量达到数千甚至上万时，线性遍历的延迟变得不可预测。

框架需要：
1. 匹配器数量增长时，路由延迟**不线性增长**
2. 命令类事件（如 `/help`）能够**O(1) 直接命中**
3. 临时匹配器（一次性、带过期时间的）与永久匹配器**隔离管理**
4. 支持**分组合并**和**条件规则**的复杂匹配

## 架构概览

引擎的路由系统由三个正交的索引结构 + 一个临时匹配器管理器组成：

```
事件到来
    │
    ├── 是否 shutdown？ → 直接丢弃
    │
    ├── 消息以触发前缀开头？ → commandIndex O(1) 查找
    │
    └── 普通事件 → 按 EventType 从 matcherIndex 获取
                        │
                        ▼
              六路合并排序（优先级降序）
                        │
                        ▼
                  依次执行匹配器
```

## 三大索引

### 1. matcherIndex（按 EventType 索引）

```go
type state struct {
    matcherIndex map[EventType][]*Matcher  // 事件类型 → 匹配器列表
    sortedCache  map[EventType][]*Matcher  // 按优先级排序后的缓存
}
```

- Key 为事件类型（`C2CMessageCreate`、`GroupMessageCreate` 或 `""` 代表通用）
- Value 为未排序的匹配器列表
- `sortedCache` 是排序后的副本，在索引变更时失效，读取时惰性重建

### 2. commandIndex（命令索引，O(1) 路由）

```go
commandIndex map[string]map[EventType][]*Matcher
// 第一层 key：命令词（如 "/help"）
// 第二层 key：事件类型（如 "C2CMessageCreate"，"" 表示通用）
```

消息以触发前缀开头时，提取命令名直接查找：

```go
func (e *Engine) processEventContext(ctx *context.Context) {
    msg := ctx.GetMessageContent()
    if strings.HasPrefix(msg, "/") {  // 触发前缀检测
        cmdName := extractCommand(msg)
        if cmdMatchers, ok := state.commandIndex[cmdName]; ok {
            // O(1) 拿到候选匹配器，跳过全量遍历
            matchers := mergeCandidateLists(cmdMatchers, eventType)
            // ...
        }
    }
}
```

这个优化极其重要：**含 1K 匹配器时，命令类事件的匹配开销完全不受匹配器总数影响**。

### 3. groupIndex（分组索引）

```go
groupIndex map[string][]*Matcher
```

用于 `DisableGroup` / `EnableGroup` / `RemoveGroup` 批量操作，与 `Source` 字段无关（`Source` 是只读标签）。

## 临时匹配器管理器（TempManager）

临时匹配器（一次性命令、带过期时间的冷却提示等）与永久匹配器**物理隔离**，避免污染主索引。

### 分片设计

```go
const tempMatcherShardCount = 8

type tempMatcherShard struct {
    mu           sync.RWMutex
    matcherIndex map[EventType][]*Matcher
    expiration   *matcherHeap    // 过期最小堆
    byID         map[*Matcher]struct{}  // 快速存在性检查
}

type tempMatcherManager struct {
    shards [tempMatcherShardCount]*tempMatcherShard
    config TempManagerConfig
    count  int32  // 原子计数
}
```

使用 **8 个分片** + **FNV-1a 哈希**将匹配器均匀分布，减少锁粒度：

```go
func (m *tempMatcherManager) getShard(matcher *Matcher) *tempMatcherShard {
    ptr := uintptr(unsafe.Pointer(matcher))
    hash := hashPtr(ptr)
    idx := hash % tempMatcherShardCount
    return m.shards[idx]
}
```

### 过期管理

最小堆（`matcherHeap`）按过期时间排序，后台 goroutine 定期清理：

```go
type matcherHeap []*matcherExpiry

type matcherExpiry struct {
    matcher  *Matcher
    deadline time.Time
}
```

水位线清理策略：
- 达到 `WatermarkHigh`（10K）时触发清理
- 清理到 `WatermarkLow`（8K）
- 支持自适应清理（根据清理速率动态调整）

## 六路合并算法

这是路由系统中最精巧的部分。所谓"六路"来自对匹配器来源的 2×3 分类：

### 分类维度

**维度 1：来源类型**
- **State 列表**：永久注册的匹配器（来自引擎的 `matcherIndex`）
- **Temp 列表**：临时匹配器（来自 `TempManager`）

**维度 2：是否命令优化**
- **普通匹配器**：通过 `matcherIndex` 获取
- **命令匹配器**：通过 `commandIndex` 获取

**维度 3：事件类型精确度**
- **Specific**：精确匹配 `EventType`（如 `C2CMessageCreate`）
- **Generic**：通用类型（`""`，匹配所有事件类型）

### 合并过程

```go
// 伪代码：六路合并的核心逻辑
func collectMatchers(e *Engine, eventType EventType) []*Matcher {
    state := e.state.Load()

    // 从 state.matcherIndex 获取 Specific 和 Generic 匹配器
    specificFromState := state.matcherIndex[eventType]
    genericFromState := state.matcherIndex[""]

    // 从 TempManager 获取
    tempSpecific := e.tempManager.Get(eventType)
    tempGeneric := e.tempManager.Get("")

    // 如果消息是命令，从 commandIndex 获取
    var cmdSpecific, cmdGeneric []*Matcher
    if isCommand(msg) {
        cmdName := extractCommand(msg)
        cmdMap := state.commandIndex[cmdName]
        cmdSpecific = cmdMap[eventType]
        cmdGeneric = cmdMap[""]
        tempCmdSpecific = e.tempManager.GetCommand(cmdName, eventType)
        tempCmdGeneric = e.tempManager.GetCommand(cmdName, "")
    }

    // 六路合并为一路，按优先级排序
    return mergeSorted(
        specificFromState,   // 1
        genericFromState,    // 2
        cmdSpecific,         // 3
        cmdGeneric,          // 4
        tempSpecific,        // 5
        tempGeneric,         // 6
    )
}
```

### 优先级排序

每个匹配器有一个 `priority` 字段（`atomic.Uint64`），合并后按优先级数值**升序**执行——数值越小越先运行（默认 50）。先执行的匹配器可以阻断（`block`）后续匹配。

```go
m.priority.Store(50)  // 默认优先级；SetPriority(1) 会排到默认匹配器之前
```

> 内部统一以 `uint64` 比较（`getPriority()` 返回 `uint64`）：早期版本曾转成 `uint`，
> 在 32 位平台会截断高位导致排序错乱，2026-07 复查时修正。

分组匹配器可以通过 `SetMatcherGroup` 和 `UseForGroup` 获得分组级中间件，但不影响路由优先级。

## 性能特性

| 场景 | 延迟 | 说明 |
|------|------|------|
| 空引擎 | ~0.5 μs | 几乎无开销 |
| 1K 普通匹配器 | ~5-6 μs | 合并排序主导 |
| 命令事件（1K Matcher） | ~1-2 μs | commandIndex O(1) 跳过遍历 |
| 含临时匹配器 | 额外 ~0.5 μs | 分片锁竞争（通常无竞争） |

## 与外部队列的比较

与 Celery / RabbitMQ 等消息队列的路由策略不同，Remilia 的路由是**内存内、同步、按优先级**执行的，而非基于消息属性的异步匹配。这使得延迟可预测在微秒级别，但也意味着不适合跨进程或分布式场景（需要在应用层自行处理）。

## 迭代过程

### V1：线性遍历 + RWMutex

最朴素的实现——所有匹配器放在一个切片里，事件到来时顺序遍历所有匹配器逐一检查规则。并发保护使用 `sync.RWMutex`。

```go
// V1 代码 — 线性遍历 + RWMutex（初始 engine 的简化）
type Engine struct {
    mu       sync.RWMutex
    matchers []*Matcher
}

func (e *Engine) ProcessEvent(ctx *Context) {
    e.mu.RLock()            // 读锁
    for _, m := range e.matchers {
        if m.Matches(ctx) { // 遍历检查每个匹配器
            e.mu.RUnlock()
            m.Handler.Handle(ctx)
            e.mu.RLock()
            if m.Block { break }
        }
    }
    e.mu.RUnlock()
}
```

**问题**：
- 高并发下 `RWMutex.RLock()` 存在缓存行乒乓——即使读锁允许多个 goroutine 并发，锁变量的原子操作仍导致多核 CPU 缓存一致性协议频繁同步
- O(n) 遍历延迟随匹配器数量线性增长——100 个匹配器和 1000 个匹配器差 10 倍
- 每次遍历都需对所有匹配器执行规则检查，即使只有极少匹配器响应

### V2：COW + EventType 索引（引入 matcherIndex）

引入 COW 无锁状态 + 按事件类型分组的索引，消除 RWMutex 的读锁竞争：

```go
// V2 代码 — COW + 简单索引
type state struct {
    matchers     []*Matcher
    matcherIndex map[string][]*Matcher  // EventType → Matchers
}

func (e *Engine) processEvent(ctx *Context) {
    state := e.state.Load()  // atomic.Load，无锁
    // 先按事件类型缩小范围
    candidates := state.matcherIndex[ctx.GetEventType()]
    // 再加通用匹配器
    candidates = append(candidates, state.matcherIndex[""]...)
    // 对 candidates 排序 + 执行
}
```

**优势**：读操作完全无锁，冷热分离（不同 EventType 的事件互不干扰）
**不足**：`matcherIndex[""]` 通用匹配器仍然需要遍历所有；命令事件和普通事件在同一个池子里，无法 O(1) 路由

### V3：新增 commandIndex（O(1) 命令路由）

```go
// V3 关键改进 — commandIndex 数据结构
type state struct {
    matcherIndex  map[string][]*Matcher         // EventType → Matchers
    commandIndex  map[string]map[string][]*Matcher // CmdName → EventType → Matchers
    // 已有的 matchers 列表保留用于全量遍历
}
```

额外发现了 `commandIndex` 需要手动**同步**到 `matcherIndex`（用于非命令事件的 fallback），两个索引的一致性维护增加了复杂性。实际线上场景发现，80%+ 的事件都是命令事件，所以这一优化的收益远大于成本。

**收益**：命令路由从 O(n) 降为 O(1)，1K 匹配器场景下延迟从 `~5μs` 降为 `~1μs`。

### V4（当前）：六路合并 + TempManager 分片

最后一个关键问题是"临时匹配器"（一次性命令、冷却提示等）污染主索引。它们的特点是：

- 数量大（可能瞬时达到数千）
- 生命周期短（几秒到几分钟）
- 读写频率高（每个事件都要遍历检查过期）

如果放在主索引中，每次事件处理都需要遍历它们，导致性能不可预测。

```go
// V4 引入 TempManager — 物理隔离
type tempMatcherManager struct {
    shards [tempMatcherShardCount]*tempMatcherShard  // 8 分片
    config TempManagerConfig
    count  int32  // 原子计数
}

type tempMatcherShard struct {
    mu           sync.RWMutex
    matcherIndex map[string][]*Matcher
    expiration   *matcherHeap    // 过期最小堆
    byID         map[*Matcher]struct{}
}
```

设计要点：
- **8 分片 + FNV-1a 哈希**：减少锁粒度，高并发下分片锁几乎无竞争
- **过期最小堆**：后台清理器 O(log n) 获取过期匹配器，而非 O(n) 遍历全部
- **水位线清理**：达到 10K 触发，清理到 8K，避免内存暴涨
- **自适应清理**：根据清理速率动态调整清理间隔

六路合并的引入使系统能够同时从 `matcherIndex`、`commandIndex`、`TempManager` 三个来源获取匹配器，并按优先级统一排序执行。三种来源各取 Specific 和 Generic 两路，共六路。

### V5（当前）：惰性 K 路归并 + TempManager RCU 快照增量维护

V4 之后热路径又经历了两次演进，并在 2026-07 的 core 深度复查中修掉了三个隐蔽缺陷：

**1. 惰性 K 路归并迭代器（merge_iter.go）替代堆合并**

六个已排序列表不再预先合并成一个大切片，而是用 `acquireMergeIter`（sync.Pool 复用）
做惰性 6 路线性扫描——消费到哪归并到哪，天然支持 `isBlocking` 提前终止，且零分配：

```go
it := acquireMergeIter(l1, l2, l3, l4, l5, l6)
defer releaseMergeIter(it)
for it.Next() { m := it.Matcher(); ... }
```

*教训（2026-07 修复）*：`Next()` 最初用 `bestPrio = math.MaxUint` 作哨兵、严格小于比较——
优先级恰好等于 MaxUint 的匹配器会被判为"没有候选"，连带其后所有列表被提前截断。
哨兵值吃掉合法极值是经典边界缺陷，现改用 `bestIdx == -1` 显式判定首个候选。

**2. TempManager 的 RCU 只读快照**

热路径不再对 8 个分片逐一加读锁，而是读取一个原子指针指向的 `TempSnapshot`
（按 eventType 预归并、预排序）。`HasAny()` 原子计数做快速短路，生产环境无临时
匹配器时完全零开销。

*教训 1*：快照最初在**每次** Add/Remove 后全量重建（收集 8 分片 + 整体排序，
O(N log N)/操作）——对"高频创建/销毁"这一 TempManager 的设计初衷是严重写放大。
2026-07 改为 COW 增量维护：Add/Remove 只替换受影响 eventType 的一条切片；
CleanExpired/水位清理等批量路径才全量重建。

*教训 2*：增量更新与全量重建并发时存在"幽灵 matcher"窗口（快照持有分片已删除的
matcher，或同一 matcher 被插入两次）。解决方案是一致性协议：分片（shard.byID）是
权威数据源，快照只是视图——`snapshotInsert` 在 snapMu 内先回查 byID（已被并发
Remove 则放弃插入）并做指针去重（全量重建可能已包含该 matcher）。

*教训 3*：`SetTempWithTimeout` 对已是 temp 的 matcher（如 `OnTemp` 创建后再设超时）
此前不会补登过期堆——超时形同虚设，未被消费的会话匹配器泄漏到 1 万水位强制清理。
现经 `SetExpiration` 在 shard 锁内写入过期时间并补登堆，同时消除了 expiresAt 的
无锁读写竞态。

## 迭代历程

| 版本 | 核心变化 | 延迟（1K Matcher） | 动机 |
|------|---------|-------------------|------|
| V1 | 线性遍历 + RWMutex | ~50 μs | 快速实现原型 |
| V2 | COW + EventType 索引 | ~10 μs | 消除读锁竞争 |
| V3 | + commandIndex O(1) 路由 | ~1-2 μs（命令事件） | 80% 事件是命令 |
| V4 | + TempManager 分片 + 六路合并 | ~5-6 μs（普通）/~1 μs（命令） | 临时匹配器隔离，延迟可预测 |
| V5（当前） | 惰性 K 路归并迭代器 + RCU 快照增量维护 | 同 V4，写路径大幅降低 | 消除堆合并分配与快照写放大 |

关键洞察：将命令事件与普通事件分离是量级上的优化——现实场景中 80%+ 的事件是命令事件，从 O(n) 降到 O(1) 意味着延迟从线性增长变为常数。
