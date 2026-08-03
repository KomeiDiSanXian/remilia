# 25 — RoutingStrategy：路由规划的稳定边界

> [02 — 六路合并匹配器路由](02-six-way-merge-matcher.md) 解决了"从大量 Matcher 中找到候选"
> 的性能问题，但 V5 的实现把索引组织方式硬编码进了 `processEventMatchers`：
> 它知道 Permanent / Command / Temp 三个来源、知道 specific / generic 两个维度，
> 甚至知道 `commandIndex`、`tempManager` 的存在。`matcherMergeIter` 的 6 路归并
> 只是替这种设计"擦屁股"。
>
> 本文是 V6 设计：把"如何组织索引、如何规划一次路由"从执行主循环中抽出来，
> 形成 `RoutingStrategy → CandidatePlan → MatcherIndex` 三层稳定边界。
> 核心动机不是性能——瓶颈早已不是性能，而是**扩展性**。

## 问题背景

`processEventMatchers`（process_platform.go）目前承担两种职责：

1. **Routing**：决定有哪些 Matcher 需要执行（读六个列表、门控、归并）
2. **Execution**：执行 Matcher（hasHandler 过滤、Match、blocking、入池、invokeHandler）

所有索引结构的知识都泄漏在 Routing 部分：

```go
permSpecific := state.sortedCache[eventType]          // Permanent
permGeneric := state.sortedCache[""]                  // Permanent
cmdSpecific := state.commandIndex[cmd][eventType]     // Command
cmdGeneric := state.commandIndex[cmd][""]             // Command
tempSpecific := e.internals.tempManager.Get(eventType) // Temp
tempGeneric := e.internals.tempManager.Get("")        // Temp
```

以后每增加一种索引（Regex / Mention / Prefix / Permission…），`processEventMatchers`
就会多几行。这种增长没有上限。

### 设计原则

本次重构遵循三条原则：

1. **不为抽象而抽象**——每个新抽象都必须有至少一个真实消费者
2. **YAGNI**——不为"未来五年可能用到"的扩展点预先实现机制
3. **稳定边界提前抽**——但抽象边界要提前划出来，避免演进时反复动执行主循环

## 架构总览

```
Engine（协调者，持有 atomic.Value[RoutingStrategy]）
    │
    ▼ Plan(st, ctx)                    ← 唯一对外对象，按值返回，零分配
CandidatePlan（自包含执行计划：Next/Matcher/Release）
    ├── 阶段 0（Fast）：Permanent / Command / Temp ← 急切构建
    └── 阶段 1（Slow）：Regex ← 惰性物化（fast 阻断时零查询）
    │
matcherRouter（默认 RoutingStrategy 实现）
    ├── phases []candidatePhase        ← 注册期按 Band 分组（升序）
    ├── Source Budget 执行（框架 panic / 插件 warn）
    └── 归并工厂（K≤16 线性，池化零分配）
    │
processEventMatchers（纯执行，不再接触任何索引）
```

职责边界：

| 角色 | 职责 |
|------|------|
| `Engine` | 协调者：持有 Strategy、State、执行主循环 |
| `RoutingStrategy` | 决定如何规划一次路由（阶段编排、剪枝、跳过） |
| `CandidatePlan` | 描述本次路由的执行计划，支持资源释放 |
| `MatcherIndex` | 提供候选来源（每个索引自带检索算法与门控） |
| `matcherMergeIter` | 归并算法实现细节（可替换，不暴露给执行层） |
| `processEventMatchers` | 只负责执行，不知道任何索引组织方式 |

## 核心抽象

### RoutingStrategy

```go
// RoutingStrategy 决定"本次路由如何组织索引"。
// 阶段编排、索引跳过、剪枝等策略性决策都发生在这里。
type RoutingStrategy interface {
    // Plan 返回本次路由的执行计划（惰性、零分配）。
    Plan(st *state, ctx *context.Context) CandidatePlan
}
```

Engine 持 `atomic.Value[RoutingStrategy]`（与 `SetCommandRegistry` 的
先例一致，engine_command.go:331）。读侧 `atomic.Load` 无锁。

### CandidatePlan

`CandidatePlan` 是 RoutingStrategy **唯一暴露的对象**——执行层看不到
`matcherMergeIter`。它自包含：持有阶段数据快照与执行上下文，不引用 Router，
生命周期就是 `Plan() → Next() → Release()`。

**当前实现（单迭代器 + 惰性慢带追加，热路径优化后）**：

```go
type CandidatePlan struct {
    env   matcherEnv
    ctx   *context.Context
    slow  [16]MatcherIndex   // 慢带索引快照（内联数组，惰性追加）
    slowN int
    iter  *matcherMergeIter  // 单归并迭代器（快带急切并入）
}

func (p *CandidatePlan) Next() bool {
    // 快带耗尽后，惰性查询慢带索引并追加到同一迭代器（各流游标已耗尽，
    // 追加不破坏归并顺序）——fast 被 block 短路时慢带索引零查询
}
```

实现要点（消除热路径"抽象税"的演进，均为语义等价优化）：

- **单迭代器**：早期双迭代器（快带一个 + 慢带一个）每事件两次
  acquire/release；现改为快带急切并入 + 慢带耗尽后追加到同一迭代器，
  每事件一次池化生命周期
- **内置索引直引**：permanent/command/temp 三个快带索引在注册时按类型
  识别为 matcherRouter 的直引字段，Plan 静态分派调用（可内联）；
  第三方索引仍走 phases 接口路径，扩展性不变
- **池化零化裁剪**：归并迭代器 acquire 只重置游标（idx/n/cur），
  lists/metas/hasMeta 槽位由 add/addMeta 覆写、Next 只读 < n，
  每事件省去 ~500B 的数组 memset

实测（同机：Ryzen 7 5800H / Windows / Go 1.26.5，中位数）：

| 基准 | 优化前 | 优化后(v3) | HEAD（v1.31.0） |
|---|---|---|---|
| RoutingPlan/Empty（纯路由层） | 142 ns | **94 ns** | — |
| RoutingPlan/CommandHit | 658 ns | **653 ns** | — |
| RoutingPlan/RegexHit（慢带+Meta） | 1,577 ns | **1,448 ns** | — |
| HotPath/Empty | 294 ns | **207 ns** | 128 ns |
| HotPath/Light | 655 ns | **565 ns** | 493 ns |
| HotPath/Medium | 3,010 ns | **3,033 ns** | 2,743 ns |
| HotPath/Heavy | 24.0 µs | **24.8 µs** | 22.9 µs |
| HotPath/AllMatch | 43.2 µs | **42.1 µs** | 42.2 µs |
| 全程分配 | 0 allocs | **0 allocs** | 0 allocs |

三轮迭代（单迭代器 → 内置索引直引 → 池化零化裁剪）回收了 166ns
抽象税中的 ~86ns（52%）：Empty 差距从 +129% 收敛到 +61%
（绝对 0.2µs）。剩余差距 ~80ns 的成分：extractCommand 与索引 map
查找（HEAD 同样支付）+ 每事件 4 次候选查询的结构成本——再压缩需
将内置索引硬编码回 Plan（重蹈 V6 前覆辙）或提前 V8（generic
构建期合并）。重负载（真实 bot 负载形态）与 HEAD 持平，端到端
吞吐无差异。

执行主循环从 V6 起就是 v2 形状，多阶段过渡对 processEvent 零感知：

```go
plan := e.strategy.Load().Plan(st, ctx)
defer plan.Release()
for plan.Next() {
    m := plan.Matcher()
    // ……原执行体原封不动……
    if blocking {
        return // 阻断 = 终止所有阶段（慢带索引零查询）
    }
}
```

### MatcherIndex

```go
// MatcherIndex 是一类 Matcher 的候选检索源。
// 每个索引拥有自己的索引结构与检索算法（HashMap/Trie/DFA…），
// 引擎只消费 Candidates 返回的有序候选流做归并。
//
// 约定：
//   - 每个子列表必须已按优先级升序排列
//   - Candidates 必须是纯读（引擎在无锁读路径上调用）
//   - Band 由索引决定，matcher 无 Band 概念
type MatcherIndex interface {
    Candidates(env MatcherEnv, ctx *context.Context) MatcherCandidates
    Band() RoutingBand
}

// MatcherCandidates 按值返回，零分配。
// 槽 0 = specific，槽 1 = generic；generic 构建期合并落地后只填槽 0。
// 引擎内部直接访问槽位，第三方索引通过 Add 填充。
type MatcherCandidates struct {
    lists [2][]*Matcher
    n     int
}

func (c *MatcherCandidates) Add(list []*Matcher)
```

**实现偏差（相对早期草图）**：Candidates 不直接收 `*state`，而是收
`MatcherEnv`——一个只读数据视图接口（`SortedCache` / `CommandCandidates`）。
原因：`*state` 是未导出类型，出现在接口签名中会**封闭外部实现**（第三方包
无法写出该签名），而 Source Budget 的 external 分支明确支持第三方索引。
`matcherEnv` 是引擎内部的默认实现（两个指针，按值传递，零分配）。
`matcherCandidates` 也因此导出为 `MatcherCandidates`。

内置四个索引，门控逻辑从 processEventMatchers 下沉到各自的 `Candidates`：

- **PermanentIndex**（BandFast）：`st.sortedCache[et]` + `st.sortedCache[""]`（无门控，纯查找）
- **CommandIndex**（BandFast）：`ctx.GetMessageContent()` 为空 → 跳过；`extractCommand` 命中
  `st.commandIndex[cmd]` 才取 specific/generic
- **TempIndex**（BandFast）：`tm.HasAny()` 为空 → 跳过；持有 `*tempMatcherManager` 引用
- **RegexIndex**（BandSlow）：内容为空 → 跳过；逐条正则预匹配（见 Fast/Slow 语义）

### RoutingBand（框架级概念）

```go
type RoutingBand uint8

const (
    BandFast RoutingBand = iota // 永久/命令/临时
    BandSlow                    // 正则/提及（未来）
)
```

**Band 是框架级概念，不是用户配置**：

- matcher 没有 `SetBand(...)`——否则 Band 会失控
- Band 由**索引**决定（`MatcherIndex.Band()`），粒度是索引级，天然收敛
- 注册期 Router 按 Band 分组为 `phases []candidatePhase`（升序，与注册顺序无关）

### matcherRouter（默认 RoutingStrategy 实现）

```go
type matcherRouter struct {
    phases []candidatePhase // 注册期按 Band 分组，升序
}

type candidatePhase struct {
    band    RoutingBand
    indexes []MatcherIndex
}
```

职责：注册索引（分组 + 预算）、`Plan()` 构建候选流、归并工厂。

### matcherMergeIter（归并算法实现细节）

由固定 6 路泛化为 K 路（上限 16），保持池化零分配：

```go
const mergeIterMaxStreams = 16

func acquireMergeIter() *matcherMergeIter
func (it *matcherMergeIter) add(list []*Matcher) // 跳过空列表，实际流数 K_act 自然衰减
func (it *matcherMergeIter) Next() bool
func (it *matcherMergeIter) Matcher() *Matcher
func releaseMergeIter(it *matcherMergeIter)
```

K≤16 时线性扫描通常仍优于 heap（顺序访问、无跳转、缓存友好）——
heap 归并是兜底方案，不是默认升级路线（见 Source Budget）。

## 关键设计决策

### D1：三层，不是四层

最初的草图是 `Strategy → Router → Indexes` 四层。收敛后确认：
Router 的全部职责（Phase 编排、Merge Factory、Budget、调用 Index）都只是
**一种 RoutingStrategy 的实现细节**，不存在第二个 Router。`matcherRouter`
直接作为默认 Strategy 实现，未来 ProfilingStrategy / DebugStrategy 实现
同一接口即可。

### D2：CandidatePlan 自包含

Plan 不引用 `matcherRouter`——它持有本次路由需要的全部信息
（v1：已构建的迭代器；v2：phase 数据快照 + 游标）。Plan 的生命周期与
Router 无关，`Plan() → Next() → Release()` 即完整生命周期。

### D3：Band 现在建，Phase 机制缓建

- **现在**：`RoutingBand` 类型、`MatcherIndex.Band()`、`candidatePhase` 结构、
  Router 注册期分组
- **缓建**：`iters [2]` 多阶段惰性物化——它唯一的消费者是未来的 RegexIndex，
  现在写就是为不存在的消费者预支代码（Premature）

### D4：Source Budget（索引预算）

**两个 K 严格区分**：

- `K_reg`（注册索引数）：架构预算。**推荐 ≤8，允许 ≤16，超过 16 需要重新审视
  路由设计**——K 超限首先意味着职责拆分过碎（如 Permission/Role/Guild 应合并为
  Metadata Index），其次才意味着算法可能不够快
- `K_act`（实际非空流数）：归并算法的输入规模，`add()` 跳过空列表天然衰减，
  非命令事件时 CommandIndex 输出 0 流

**预算执行（区分框架与插件）**：

| 注册方 | K > 16 行为 | 理由 |
|--------|------------|------|
| 框架内部（internal=true） | `panic` | 框架 Bug，不是用户输入 |
| 第三方插件（external=true） | `logger.Warn` + 继续运行，dev 下测试断言 | 扩展行为，不应中断 |

### D5：构造期注册，无运行时 AddIndex

不做运行时 `AddIndex`。原因：运行时增删索引会污染 COW 生命周期
（重新生成 Phase、重算预算、排序、atomic.Store），而真实需求几乎不存在——
`WithMatcherIndex(...)` 选项足够。

### D6：零分配不变量

热路径维持零分配：`CandidatePlan` 按值返回、`matcherMergeIter` 池化、
`matcherCandidates` 值类型、`Plan` 不分配。测试用 `AllocsPerRun` 断言。

### D7：命名——不叫 Router

bot 层已存在 `router.Router`（见 [13 — Adaptive Router](13-adaptive-router.md)，
FSM→规则→dispatchToEngine 的顶级分发器）。引擎内的新抽象命名为
`RoutingStrategy`，默认实现为 `matcherRouter`，避免两层 Router 的语义冲突。

## Fast/Slow 阶段语义（已实现，V7 随 regexIndex 落地）

Fast/Slow 是**优先级带（Priority Band）约定 + 两阶段惰性物化**，
不是"Fast 全失败才查 Slow"的朴素门控：

- **朴素门控的问题**：Fast matcher 匹配成功但不 block 时，低优先级的 Slow
  matcher 仍应有运行机会；且 Slow 索引里可能有高优先级 matcher（全局优先级
  是跨索引的），门控会打乱。
- **正确语义**：Slow 带 matcher 优先级必须低于 Fast 带（文档化契约），
  两阶段模型与统一归并语义等价；fast 阶段被 block 短路时，
  slow 阶段索引**根本不会被查询**——Regex 免费跳过。
- **惰性物化**：slow 阶段在首个 Next() 才真正构建归并迭代器，blocking 提前
  终止时其索引的检索成本（如正则执行）为零。

内置慢带索引 **regexIndex**（BandSlow 首个落地实现）：

- 消费者：`matcher.Regex(pattern)`（置位 regexIndexed + 记录 pattern，与
  commandIndexed 同模式；已注册 matcher 链式调用会触发全量重建迁移）
- 候选生成：对 specific/generic 两桶逐条 `MustGetCachedRegexp` 预匹配
  （与 OnRegex 规则共享 LRU 编译缓存）
- 跳过语义：regexIndexed matcher 的 Match() 跳过 Rules[0]（正则已由索引
  预匹配），正则不执行两次——**单阶段下平本，惰性阶段下才产生净收益**
- 分配注记：慢带候选过滤列表每事件新建（慢带路径可接受，正则执行成本
  远高于一次切片分配；快带路径保持零分配）

### 候选 Meta（V9 已实现，随 regexIndex 捕获组落地）

regexIndex 预匹配使用 `FindStringSubmatch`（而非 MatchString），捕获组作为
候选 Meta 随候选流传递：

- **流形状**：保持 `[]*Matcher` 不变，Meta 走与列表 1:1 对齐的并行数组
  （`MatcherCandidates.metas` / `matcherMergeIter.metas + hasMeta`）——
  **实现偏差（相对早期草图 `{Matcher, Meta any}`）**：只有需要携带结果的
  索引才启用，快带流零成本，无需全链路改为 `[]Candidate`
- **注入**：执行循环在 invokeHandler 前 `ctx.SetCandidateMeta(meta)`
  （同步/池化两路；池化路必须在提交前取值，异步执行时 plan 游标已前进）
- **读取**：handler 经类型化 getter `ctx.RegexResult() (RegexMatch, bool)`
  直接取捕获组，无需重新执行正则

```go
m := e.On(et).Regex(`hello (\w+)`)
m.Handle(func(ctx *context.Context) error {
    if res, ok := ctx.RegexResult(); ok {
        name := res.Groups[1] // 捕获组，零重执行
    }
    return nil
})
```

## 实施清单

| 文件 | 动作 |
|------|------|
| `core/engine/routing_strategy.go` | 新：`RoutingStrategy`、`RoutingBand`、`CandidatePlan`（v1）、`matcherRouter`（阶段分组/预算/归并工厂）、`WithMatcherIndex` 选项 |
| `core/engine/matcher_index.go` | 新：`MatcherIndex` 接口、`matcherCandidates` |
| `core/engine/index_permanent.go` | 新：PermanentIndex（BandFast） |
| `core/engine/index_command.go` | 新：CommandIndex（BandFast，门控下沉） |
| `core/engine/index_temp.go` | 新：TempIndex（BandFast，持有 tempManager） |
| `core/engine/merge_iter.go` | 改：固定 6 路 → K 路（上限 16）+ `add()` |
| `core/engine/process_platform.go` | 改：`processEventMatchers` 收敛为 Plan 循环 |
| `core/engine/engine.go` | 改：挂 `atomic.Value[RoutingStrategy]`、头部索引注释同步 |
| `core/engine/process_test.go` | 改：`collectMergeIter(l1..l6)` 适配新 API |
| `core/engine/routing_strategy_test.go` | 新：预算 panic/warn、Band 分组、自定义索引、blocking 短路、优先级交错、零分配断言 |
| `core/engine/benchmark_hotpath_test.go` | 改：适配 + Plan 级基准 |
| `docs/notes/README.md` | 追加 #25 目录条目 |

## 测试与验证

1. `go test ./core/engine/ -count=1`
2. 热路径基准前后对比（`BenchmarkMergeIter` / Plan 级基准）
3. `AllocsPerRun` 零分配断言
4. blocking 短路测试：记录型慢索引验证"fast 阻断后慢索引未被查询"

## 演进路线

| 步骤 | 内容 | 状态 |
|------|------|------|
| V6 | RoutingStrategy 抽象（RoutingStrategy → CandidatePlan → MatcherIndex） | ✅ 已实现 |
| V7 | 多阶段惰性物化（`iters [2]` + cursor）+ 首个慢带索引 regexIndex | ✅ 已实现 |
| V8 | generic 构建期合并（6 流 → 3 流，K_act == K_reg） | 触发条件：索引数量接近预算上限，账目需要可数 |
| V9 | 候选携带 Meta（regexIndex 捕获组）+ ctx 类型化 getter（`RegexResult`） | ✅ 已实现（并行数组形状，见上文实现偏差） |

每步都是纯增量：Plan 循环形状、Band 类型、Phase API 骨架都已就位，
后续演进不需要再动 `processEventMatchers`。
