# core / middleware 模块设计质量深度分析

> **分析日期**：2026-02-25  
> **最后更新**：2026-02-25（P0 / P1 / P2 全部完成 ✅）  
> **分析范围**：`core/context`、`core/engine`、`middleware`（含 `middleware/hotreload`）  
> **项目状态**：未发布，可接受重构  
> **前置背景**：plugin/plugins 系统已完成两轮重构优化，本文聚焦其他核心模块

---

## 0. 执行摘要

`core/engine` 和 `middleware` 是整个框架的性能与稳定性基础，`core/context` 是插件开发者每天接触频率最高的类型。三个模块的总体设计**已接近优秀**，但存在若干影响插件开发体验和长期可维护性的问题。

**总体评级：A-**

| 模块 | 评分 | 核心说明 |
|------|------|---------|
| `core/context` | B+ | 功能丰富、COW/Pool 优化到位；但有两套扩展 API（`ctx.Set`/`ctx.Get` 字符串键 vs `ExtGet/ExtSet` 类型键）并存的割裂感；`Set` 传入 `nil` 会静默 Delete（违反直觉）|
| `core/engine` | A | COW 无锁读路径、命令索引、编译链、degraded 分组—设计成熟；单文件 1207 行偏长；`ProcessEvent` 内合并 6 路排序列表的逻辑不够自解释 |
| `middleware` | A- | 功能齐全（Retry/CB/Dedup/Adaptive/Degradation）；`Timeout` 中间件用 goroutine 处理存在 race 隐患；`degradation.go` 在包级别用 `promauto` 注册 Prometheus 指标，测试时会重复注册 panic；`hotreload.Bridge` 缺少 `Dedup`/`Degradation` 的热更新支持 |

---

## 1. `core/context` 分析

### 1.1 亮点

| 特性 | 说明 |
|------|------|
| 类型安全扩展 `ExtGet[T]` / `ExtSet[T]` | 基于 `reflect.Type` 键，编译期类型检查，零运行时字符串对比 |
| `decodeCache` typed-union | 避免 `any` boxing，重复 Decode 同一事件零分配 |
| `sync.Pool` + `AcquireContext` / `ReleaseContext` | 高吞吐下 GC 压力降低 ~50% |
| 字段级懒缓存（`sync.Once` + `contentOnce` / `authorOnce`）| 同一请求多次调用 `GetMessageContent` / `GetAuthor` 无重复解析 |
| `Clone()` 正确传播 trace span，独立取消 | 异步场景安全，不引发级联取消 |

### 1.2 问题

---

#### C1：`ctx.Set(key, nil)` 静默删除——违反直觉

**问题**：

```go
// context.go
func (ctx *Context) Set(key string, value any) {
    if value == nil {
        ctx.Delete(key)  // ← nil 被当作"删除"操作，无任何提示
        return
    }
    // ...
}
```

插件开发者调用 `ctx.Set("state", nil)` 时，原意可能是"清空某个指针字段"，但实际执行的是删除。若后续 `ctx.Get("state")` 返回 `nil, false`，则调用 `exists` 判断时行为完全不同于预期的 `nil, true`。

**修复方案**：禁止传入 `nil`，或者将删除操作显式要求调用 `ctx.Delete(key)`：

```go
func (ctx *Context) Set(key string, value any) {
    if value == nil {
        // 静默忽略，并在 debug 模式下输出警告；显式删除请调用 ctx.Delete(key)
        logger.WithField("key", key).Debug("[Context] Set(nil) is a no-op; use Delete() to remove a key")
        return
    }
    // ...
}
```

---

#### C2：两套状态 API 并存，插件开发者不知道该用哪套

**问题**：`Context` 暴露了两套不同语义的 K-V 扩展机制：

| API | 键类型 | 访问方式 | 用途说明 |
|-----|--------|---------|---------|
| `ctx.Set(key, value)` / `ctx.Get(key)` | `string` | 值语义（拷贝）| 用户自定义状态 |
| `ExtGet[T](ctx.Ext())` / `ExtSet[T](ctx.Ext())` | `reflect.Type` | 泛型类型安全 | 框架内部 + 中间件扩展 |

文档未清晰说明：
- 中间件开发者应该用哪套？
- 为什么 `retryMetadata`、`middlewareTrace`、`parsedCommand` 用 `ExtSet` 而不是 `ctx.Set`？
- 插件作者什么情况下应该用 `ExtGet[T]`？

**建议**：在 `context.go` 的包注释或者 `extensions.go` 顶部增加清晰的「两套 API 使用指南」，明确：
- `ctx.Set/Get`：插件/handler 层的临时字符串键值状态（在 handler 间传递信息）
- `ExtGet/ExtSet`：框架内部扩展点（中间件、框架组件共享强类型数据）
- **不建议**插件直接使用 `ExtGet/ExtSet`（虽然无法阻止，但应通过文档引导）

---

#### C3：`ctx.Context()` 和 `ctx.SetStdContext()` 可能被误用为传递业务数据

**问题**：`ctx.SetStdContext()` 设计用于中间件注入 trace / timeout，但没有任何运行时防护。插件开发者可能误用其传递 `context.WithValue(ctx.Context(), "user_data", data)` 这类反模式。

**建议**：在 godoc 中明确禁止通过 `context.WithValue` 传递业务数据，并给出正确替代：

```go
// SetStdContext 仅供中间件使用：注入 trace context（OpenTelemetry）或超时控制。
//
// ⚠️  不要通过 context.WithValue 传递业务数据——请使用 ctx.Set(key, value)。
// 通过 context.WithValue 传递数据会导致类型不安全和可测试性下降。
```

---

#### C4：`isReservedUserStateKey` 的保留键检查不完整

**问题**：当前只检查 `"mw_trace"`、`"retry_attempt"` 和 `"_remilia_internal_"` 前缀，但框架内部还使用 `ExtSet` 存储 `parsedCommand`、`commandArgsCache` 等——这些用 `reflect.Type` 键，不走 `isReservedUserStateKey`，所以实际上没有冲突。

但问题在于：`ctx.Set("parsed_command", someValue)` 这样的用户调用并不会被拦截（因为 `parsedCommand` 的实际键是 `reflect.Type`，而用户的是字符串），反而会造成混乱——文档未说明这两套系统完全隔离，用户可能误以为字符串键 `"parsed_command"` 会覆盖框架的解析结果。

**建议**：在 `isReservedUserStateKey` 的 godoc 明确说明：字符串键系统与 `ExtGet/ExtSet` 类型键系统**完全隔离**，不存在覆盖关系。

---

#### C5：`convenience.go` 中的 `OnGroupBlacklist` / `OnGroupWhitelist` 对非群组事件行为不一致

**问题**：

```go
// OnGroupBlacklist
return func(ctx *Context) bool {
    if err := ctx.DecodeEvent(&event); err != nil {
        return true // ← 解码失败放行（注释为"解码失败，放行"）
    }
    // ...
}

// OnGroupWhitelist
return func(ctx *Context) bool {
    if err := ctx.DecodeEvent(&event); err != nil {
        return false // ← 解码失败拒绝
    }
    // ...
}
```

黑名单解码失败放行，白名单解码失败拒绝——语义相反且都有问题：
- 黑名单解码失败意味着不是群组消息，应该**拒绝**（黑名单只用于群组，其他类型事件不应走黑名单规则）
- 或者两者都应该保持一致的"解码失败 → 不适用此规则"的语义

**修复建议**：统一为"解码失败 → 规则不适用 → 返回 true（放行，让其他规则决定）"，并在 godoc 说明这些规则**仅用于群组消息**：

```go
// OnGroupBlacklist 创建群组黑名单规则（仅对群组消息有效）。
// 非群组消息的事件（解码失败）视为不适用此规则，直接放行。
```

---

#### C6：`rules.go` 中 `OnRegex` 缺少 LRU 编译缓存的大小可配置化

**问题**：

```go
// rules.go
var regexCache, _ = lru.New[string, *regexp.Regexp](128) // 硬编码 128
```

框架使用了第三方 `golang-lru/v2` 库，但缓存大小硬编码为 128。若 Bot 注册了大量不同正则规则，可能频繁淘汰，导致正则反复编译。128 对于小 Bot 过大（浪费内存），对于大 Bot 可能不够。

**建议**：提供 `SetRegexCacheSize(n int)` 函数，或在 `NewEngine` 选项中允许配置。

---

### 1.3 `core/context` 改进优先级汇总

| # | 问题 | 优先级 | 工作量 |
|---|------|--------|--------|
| C1 | `ctx.Set(nil)` 静默 Delete | P1 | 极小 |
| C2 | 两套扩展 API 无使用文档 | P1 | 小（文档） |
| C3 | `SetStdContext` 误用风险 | P2 | 极小（注释） |
| C4 | 保留键说明不清晰 | P2 | 极小（注释） |
| C5 | `OnGroupBlacklist` 解码失败行为不一致 | P1 | 极小 |
| C6 | 正则缓存大小硬编码 | P2 | 小 |

---

## 2. `core/engine` 分析

### 2.1 亮点

| 特性 | 说明 |
|------|------|
| COW（Copy-on-Write）无锁读路径 | 通过 `infraatomic.Value[*engineState]` 实现，读性能提升 5-6x |
| 命令索引（`commandIndex`）| 快速从 O(n) 匹配降至 O(1) 命令查找 |
| 编译链（`compiledChain`）| 预构建中间件调用链，避免每次 handler 调用时动态构造嵌套闭包 |
| 批量删除处理器（`pendingDeleteProcessor`）| 避免高频删除造成写锁争用 |
| 临时 Matcher 清理器 | 防止一次性 Matcher 的内存泄漏 |
| `Shutdown()` + `eventWg` 关闭语义 | 优雅排空在途事件后再退出 |
| `EngineReader` 只读包装 | 防止运行时穿透，第一轮分析已解决 |
| `DisableGroup` / `EnableGroup` | Matcher 级别的暂停/恢复，不用物理删除 |

### 2.2 问题

---

#### E1：`engine.go` 单文件 1207 行，读写方法混杂

**问题**：`engine.go` 包含：
- `NewEngine` 和选项（~50 行）
- 所有 Matcher 增删改查（~300 行）
- 命令注册（~150 行）
- Matcher 查询/统计（~200 行）
- Group 管理（~100 行）
- Shutdown（~50 行）
- 各种辅助方法（~300 行）

与之对比，`process.go`（626 行）、`middleware.go`（186 行）、`matcher.go`（722 行）已经独立出去，但主文件仍过长。

**建议拆分**：

| 新文件 | 内容 |
|--------|------|
| `engine.go` | `Engine` 结构体定义 + `NewEngine` + `Shutdown`（保留核心） |
| `engine_matcher_ops.go` | `On` / `OnC2C` / `RegisterMatcher` / `DeleteMatcher` / `RemoveGroup` 等写操作 |
| `engine_command.go` | `RegisterCommand` / `GetAllCommands` / `FindCommand` 等命令 API |
| `engine_query.go` | `GetMatcherCount` / `GetMatcherStats` / `GetMaxMatchers` 等只读查询 |
| `engine_group.go` | `DisableGroup` / `EnableGroup` / `RemoveGroup` / `UseForGroup` |

---

#### E2：`ProcessEvent` 中 6 路合并排序逻辑无注释，维护困难

**问题**：`process.go` 中合并 `permSpecific`、`cmdSpecific`、`tempSpecific`、`permGeneric`、`cmdGeneric`、`tempGeneric` 六个已排序切片的代码约 60 行，逻辑正确但没有说明合并的**优先级语义**和**合并算法**（实际上是一个 6 路归并的简化版本，并非归并排序，因为各子列表已经预排序）。

任何修改此代码的开发者都需要先花时间理解这 6 路的含义和顺序。

**建议**：抽取为 `mergeSortedMatcherLists` 函数，并在函数注释中说明：
- 6 路的含义和优先级
- 为什么此处不用归并（已预排序）
- Temp Matcher 与 Permanent Matcher 的优先级关系

---

#### E3：`On` / `OnC2C` / `OnGroupAt` 系列方法返回 `*Matcher`，但 `*Matcher` 的写方法（`Use`、`Handle` 等）是否线程安全未文档化

**问题**：`Matcher` 的链式调用：

```go
engine.OnC2C(OnCommand("/hello")).
    Use(rateLimiter).
    Handle(handler)
```

`Matcher.Use()` 和 `Matcher.Handle()` 在 Matcher 注册到 Engine **之后**是否可以安全修改？当前代码中，`compiledChain` 用 `atomic.Value` 存储，但 `Rules`、`Handler`、`middlewares` 字段的修改是否受到保护并未明确。

**建议**：明确文档化「Matcher 链式调用必须在注册到 Engine 之前完成」，且注册后不应调用任何写方法；或者为后注册修改添加线程安全保护。

---

#### E4：`config.go` 中的 `Option` 函数类型未导出文档中的默认值

**问题**：`NewEngine(options ...Option)` 的各 `With*` 函数（如 `WithCleanupInterval`、`WithMaxMatchers`、`WithPendingDeleteBatchSize`）没有说明默认值是多少，开发者不知道不传选项时的行为：

```go
// 当前 config.go
func WithMaxMatchers(max int) Option {
    return func(e *Engine) {
        e.state.Load().maxMatchers = max  // 默认值是？
    }
}
```

**建议**：在 `config.go` 顶部集中列出所有默认值常量，并在各 `With*` 函数中引用：

```go
const (
    DefaultMaxMatchers              = 0 // 0 = 无限制
    DefaultTempMatcherCleanerInterval = 5 * time.Minute
    DefaultPendingDeleteBatchSize     = 50
    // ...
)
```

---

#### E5：`Matcher.GetSource()` 与 `group` 字段语义混淆

**问题**：`Matcher` 有两个相似概念：
- `Source string`：标识 Matcher 来源（如 `"global"` / `"plugin:admin"`），用于调试/标签
- `group string`：标识所属分组，用于 `UseForGroup`/`DisableGroup`/`EnableGroup` 操作

两者在设计上有明确分工（代码注释中已说明），但 `engine.go` 的 `On()`、`OnC2C()` 等方法设置 `Source` 的地方容易被误解为影响分组行为。

同时，`GroupMatchers`（按 `group` 分组）和 `CommandIndex`（按命令前缀分组）是两个正交的索引机制，设计正确但没有任何架构说明文档。

**建议**：在 `engine.go` 顶部添加「索引设计说明」注释，解释 `group`、`commandIndex`、`sortedCache` 三者的用途和关系。

---

### 2.3 `core/engine` 改进优先级汇总

| # | 问题 | 优先级 | 工作量 |
|---|------|--------|--------|
| E1 | `engine.go` 1207 行过长 | P1 | 中（拆文件，无逻辑改动） |
| E2 | `ProcessEvent` 6 路合并无注释 | P2 | 极小（注释） |
| E3 | `Matcher` 注册后写方法线程安全未说明 | P1 | 极小（文档 + 运行时检查） |
| E4 | `With*` 选项缺少默认值说明 | P2 | 极小（注释 + 常量） |
| E5 | `Source` vs `group` 语义区别无架构说明 | P2 | 极小（注释） |

---

## 3. `middleware` 分析

### 3.1 亮点

| 特性 | 说明 |
|------|------|
| `Retry` 指数退避 + context 感知 | 正确处理 context 取消，返回 `ctx.Err()` 而非 `BlockError` |
| `CircuitBreaker` 三状态完整实现 | Closed→Open→HalfOpen→Closed/Open，支持 `OnStateChange` 回调 |
| `DedupFilter` hash(eventID) 优化 | `uint64` 替代 `string` 键，内存减少 50-70% |
| `AdaptiveRateLimiter` 自适应 | 基于 CPU/P99 延迟自动调整并发限制，带 P99 直方图 |
| `ManagedAdaptive` + `WithContext` 模式 | goroutine 生命周期与外部 context 联动，无需手动 Stop |
| `hotreload.Bridge` | config 变更自动推送到 Adaptive/Retry/CircuitBreaker，无需重启 |
| `context_keys.go` 集中字符串常量 | 避免跨文件散落魔法字符串 |

### 3.2 问题

---

#### M1：`Timeout` 中间件在超时后存在 goroutine 泄漏和并发写 Context 的 Race

**问题**：`middleware.go` 中的 `Timeout` 实现在单独 goroutine 中运行 handler，通过 channel 等待结果：

```go
go func() {
    // ...
    done <- next(ctx)  // 在独立 goroutine 中调用 handler
}()

select {
case err := <-done:
    return err
case <-timer.C:
    return context.DeadlineExceeded // 超时后返回
}
```

**已知问题**：
1. 超时发生后，goroutine 仍在后台运行并继续操作同一个 `ctx`（写 ctx.Set、调用 ctx.Reply 等），而主 goroutine 已经返回——这是经典的**并发写同一 Context** 竞态
2. `done` 是带缓冲（`cap=1`）的 channel，超时后 goroutine 写入不阻塞，但 `ctx` 对象已被归还到 pool，goroutine 仍持有对它的引用
3. 如果超时后 goroutine 尝试回复消息（`ctx.Reply`），会使用一个已被重置的 Context

**正确实现方案**：通过 `ctx.SetStdContext()` 注入带 deadline 的 `stdCtx`，让 handler 自己监听 `runCtx.Done()`，不用额外 goroutine：

```go
func Timeout(timeout time.Duration) eventctx.Middleware {
    return func(next eventctx.Handler) eventctx.Handler {
        return func(ctx *eventctx.Context) error {
            stdCtx, cancel := context.WithTimeout(ctx.Context(), timeout)
            defer cancel()
            ctx.SetStdContext(stdCtx)
            err := next(ctx) // 同步调用，handler 内部通过 ctx.Context().Done() 感知超时
            if errors.Is(err, context.DeadlineExceeded) {
                return fmt.Errorf("handler timeout after %s: %w", timeout, err)
            }
            return err
        }
    }
}
```

此方案的前提是 handler 遵循 context 取消语义（通过 `ctx.Context().Done()` 退出），这正是 Go 的标准实践，也与框架 `ctx.Go`/goroutine 管理的设计一致。

---

#### M2：`degradation.go` 包级 `promauto` 注册导致测试 panic

**问题**：

```go
// degradation.go — 包级变量，import 即注册
var (
    degradationLevelGauge = promauto.NewGauge(...)
    degradationActiveGauge = promauto.NewGauge(...)
    // ...共 8 个指标
)
```

`promauto` 在包被 import 时**立即向默认 Prometheus 注册器注册指标**。若测试中多次 import 此包（例如不同测试包都 import middleware），会因重复注册同名指标而 panic。

这是 Go 中 Prometheus 指标注册的经典反模式。`middleware/adaptive.go` 没有这个问题（使用局部注册）。

**修复方案**：改为惰性注册（使用 `prometheus.MustRegister` 在 `NewDegradationManager` 构造时注册，而不是在包级别）：

```go
type degradationMetrics struct {
    levelGauge   prometheus.Gauge
    activeGauge  prometheus.Gauge
    // ...
}

func newDegradationMetrics(reg prometheus.Registerer) *degradationMetrics {
    if reg == nil {
        reg = prometheus.DefaultRegisterer
    }
    m := &degradationMetrics{
        levelGauge: prometheus.NewGauge(prometheus.GaugeOpts{...}),
        // ...
    }
    reg.MustRegister(m.levelGauge, ...)
    return m
}
```

并在 `NewDegradationManager` 中调用：

```go
func NewDegradationManager(config DegradationConfig) *DegradationManager {
    dm := &DegradationManager{
        metrics: newDegradationMetrics(nil), // 或传入自定义 Registerer
        // ...
    }
    return dm
}
```

这样测试中每个 `DegradationManager` 实例使用独立的指标集，或者测试可以传入 `prometheus.NewRegistry()` 完全隔离。

---

#### M3：`hotreload.Bridge` 不支持 `DedupFilter` 和 `DegradationManager` 的热更新

**问题**：`hotreload.Bridge` 只支持 `AdaptiveRateLimiter`、`ConfigurableRetry`、`CircuitBreaker` 三种中间件的热更新，但 `DedupFilter` 和 `DegradationManager` 也有可配置参数（TTL、MaxSize、CPU 阈值等），却不能通过配置文件热更新。

**建议**：为 `DedupFilter` 和 `DegradationManager` 实现 `UpdateConfig` 方法，并在 `Bridge` 中添加 `WatchDedup` / `WatchDegradation`：

```go
// Bridge 扩展
func (b *Bridge) WatchDedup(df *middleware.DedupFilter) *Bridge { ... }
func (b *Bridge) WatchDegradation(dm *middleware.DegradationManager) *Bridge { ... }
```

---

#### M4：`middleware.go` 中的 `RateLimit` 使用 `golang.org/x/time/rate`，与 `AdaptiveRateLimiter` 功能重叠，选择困难

**问题**：框架提供了两种限流：

```go
// middleware.go
func RateLimit(r rate.Limit, b int) eventctx.Middleware  // 使用 token bucket (golang.org/x/time/rate)

// adaptive.go  
func NewManagedAdaptive() *ManagedAdaptiveRateLimiter    // 自适应限流
```

对于插件开发者来说：
- 什么时候用 `RateLimit`？什么时候用 `NewManagedAdaptive`？
- 它们能否组合使用？
- `RateLimit` 的参数 `rate.Limit`（`float64`，每秒请求数）对不熟悉令牌桶的开发者不友好

**建议**：
1. 在 `simple.go` 中添加更友好的封装函数：

```go
// SimpleRateLimit 创建简单固定速率限流器（每秒最多 n 个请求）
// 适用于已知固定峰值的场景；对于未知负载请使用 NewManagedAdaptive()
func SimpleRateLimit(perSecond float64) eventctx.Middleware {
    return RateLimit(rate.Limit(perSecond), int(perSecond*2)) // burst = 2x rate
}
```

2. 在两者的 godoc 中明确选择指南：
   - `SimpleRateLimit`：已知峰值，简单稳定场景
   - `NewManagedAdaptive`：未知负载，需要根据 CPU/延迟自动调整

---

#### M5：`Recover` 中间件的 `stack` 字段使用 `string(stack[:length])` 拷贝，但 4096 字节可能截断深调用栈

**问题**：

```go
stack := make([]byte, 4096)
length := runtime.Stack(stack, false)
```

深调用栈（如 middleware 嵌套 > 10 层 + 插件回调）可能超过 4096 字节，导致栈信息截断，调试困难。

**建议**：使用自适应缓冲区（先尝试小缓冲区，不足则翻倍）：

```go
func captureStack() string {
    buf := make([]byte, 4096)
    for {
        n := runtime.Stack(buf, false)
        if n < len(buf) {
            return string(buf[:n])
        }
        buf = make([]byte, len(buf)*2)
        if len(buf) > 64*1024 { // 上限 64KB
            return string(buf[:n]) + "\n[stack truncated]"
        }
    }
}
```

---

#### M6：`context_keys.go` 中的常量只有 3 个，但实际上 `middleware` 包中还散落着其他字符串常量

**问题**：检查各文件，发现以下字符串常量没有集中到 `context_keys.go`：

```go
// dedup.go 内部隐式使用 "degraded" 字符串的地方
// degraded_ext.go 中的一些操作键

// retry.go
const internalStateKeyRetryAttempt = "_remilia_internal_retry_attempt" // 已废弃的内部键（在 extension 中）
```

**建议**：将所有跨文件引用的字符串常量集中到 `context_keys.go`（保持一致）。

---

### 3.3 `middleware` 改进优先级汇总

| # | 问题 | 优先级 | 工作量 |
|---|------|--------|--------|
| M1 | `Timeout` goroutine race + context 泄漏 | **P0** | 小 |
| M2 | `degradation.go` promauto 包级注册导致测试 panic | **P0** | 中 |
| M3 | `hotreload.Bridge` 缺少 Dedup/Degradation 支持 | P1 | 小 |
| M4 | `RateLimit` vs `AdaptiveRateLimiter` 选择指南缺失 | P1 | 极小（文档 + 封装函数） |
| M5 | `Recover` 栈缓冲区固定 4096 字节可能截断 | P2 | 极小 |
| M6 | 字符串常量未完全集中 | P2 | 极小 |

---

## 4. 插件开发者体验综合评估

从插件开发者（使用 `ctx`、`engine`、`middleware`）的视角：

### 4.1 良好的体验

- **规则系统**：`OnC2CMessage()`、`OnCommand()`、`OnRegex()` 等组合规则直观，`OnUserWhitelist`/`OnGroupWhitelist` 等便捷规则开箱即用
- **Context 扩展**：`ctx.Set/Get` 足够简单，中间件间状态传递无障碍
- **中间件组合**：`engine.Use(Retry(...), CircuitBreaker(...), Timeout(...))` 链式调用自然
- **AdaptiveRateLimiter**：`NewManagedAdaptiveWithContext(bot.Context())` 生命周期绑定优雅
- **Recover/Logging**：两个最常用的观测型中间件使用成本极低

### 4.2 仍有摩擦的体验

| 摩擦点 | 影响 |
|--------|------|
| 不知道该用 `ctx.Set` 还是 `ExtGet[T]` | 第一次写中间件时困惑 |
| `Timeout` 存在 race（若插件依赖 timeout 中间件做保护）| 可能导致偶现 data race 测试失败 |
| `SimpleAdaptive()` 会泄漏后台 goroutine（文档有说明但容易忽略）| 在 Bot 生命周期短的测试中造成 goroutine leak |
| `degradation.go` 测试因 promauto 重复注册失败 | 测试中 import middleware 包时崩溃 |
| `RateLimit` 参数 `rate.Limit` 类型不直观 | 需要查文档才知道单位是"每秒请求数" |

---

## 5. 改进建议总优先级汇总

### P0（正确性，发布前必须修复）

| # | 问题 | 文件 | 工作量 | 状态 |
|---|------|------|--------|------|
| M1 | ~~`Timeout` 中间件 goroutine race + Context 泄漏~~ | `middleware/middleware.go` | 小 | ✅ 已完成 |
| M2 | ~~`degradation.go` promauto 包级注册~~ | `middleware/degradation.go` | 中 | ✅ 已完成 |

### P1（一致性/开发者体验，发布前建议修复）

| # | 问题 | 文件 | 工作量 | 状态 |
|---|------|------|--------|------|
| C1 | ~~`ctx.Set(nil)` 静默 Delete~~ | `core/context/context.go` | 极小 | ✅ 已完成 |
| C2 | ~~两套扩展 API 缺少使用文档~~ | `core/context/extensions.go` | 小 | ✅ 已完成 |
| C5 | ~~`OnGroupBlacklist` 解码失败行为不一致~~ | `core/context/convenience.go` | 极小 | ✅ 已完成 |
| E1 | ~~`engine.go` 1207 行过长~~ | `core/engine/engine.go` | 中 | ✅ 已完成 |
| E3 | ~~`Matcher` 注册后写操作线程安全未说明~~ | `core/engine/matcher.go` | 极小 | ✅ 已完成 |
| M3 | ~~`hotreload.Bridge` 缺少 Dedup/Degradation 支持~~ | `middleware/hotreload/hotreload.go` | 小 | ✅ 已完成 |
| M4 | ~~`RateLimit` vs `AdaptiveRateLimiter` 选择指南缺失~~ | `middleware/simple.go` | 极小 | ✅ 已完成 |

### P2（细节打磨，可发布后迭代）

| # | 问题 | 文件 | 工作量 | 状态 |
|---|------|------|--------|------|
| C3 | ~~`SetStdContext` 误用风险文档~~ | `core/context/context.go` | 极小 | ✅ 已完成 |
| C4 | ~~保留键说明不清晰~~ | `core/context/context.go` | 极小 | ✅ 已完成 |
| C6 | ~~正则缓存大小硬编码~~ | `core/context/rules.go` | 小 | ✅ 已完成 |
| E2 | ~~`ProcessEvent` 6 路合并无注释~~ | `core/engine/process.go` | 极小 | ✅ 已完成 |
| E4 | ~~`With*` 选项缺少默认值说明~~ | `core/engine/config.go` | 极小 | ✅ 已完成 |
| E5 | ~~`Source` vs `group` 架构说明缺失~~ | `core/engine/engine.go` | 极小 | ✅ 已完成 |
| M5 | ~~`Recover` 栈缓冲区固定 4096~~ | `middleware/middleware.go` | 极小 | ✅ 已完成 |
| M6 | ~~字符串常量未完全集中~~ | `middleware/context_keys.go` | 极小 | ✅ 已完成 |

---

## 6. 文件健康状态速查表

| 文件 | 行数 | 状态 |
|------|------|------|
| `core/context/context.go` | 675 | ⚠️ C1（nil Set）、C2（API 文档）、C3（SetStdContext 文档）、C4（保留键说明）|
| `core/context/convenience.go` | 160 | ⚠️ C5（OnGroupBlacklist 行为不一致）|
| `core/context/rules.go` | 521 | ⚠️ C6（正则缓存大小硬编码）|
| `core/context/extensions.go` | 138 | ✅ 健康 |
| `core/context/pool.go` | 98 | ✅ 健康 |
| `core/context/permission.go` | 375 | ✅ 健康（PermissionManager 在 context 包中，语义稍重但不影响使用）|
| `core/context/command_extension.go` | 80 | ✅ 健康 |
| `core/engine/engine.go` | 1207 | ⚠️ E1（过长）、E5（Source/group 说明） |
| `core/engine/process.go` | 626 | ⚠️ E2（6 路合并注释）|
| `core/engine/matcher.go` | 722 | ⚠️ E3（Matcher 注册后写操作说明） |
| `core/engine/config.go` | ~100 | ⚠️ E4（With* 默认值未文档化）|
| `core/engine/reader.go` | 86 | ✅ 健康 |
| `core/engine/middleware.go` | 186 | ✅ 健康 |
| `core/engine/errors.go` | 157 | ✅ 健康 |
| `middleware/middleware.go` | 416 | ❌ M1（Timeout race）、⚠️ M5（Recover 栈截断） |
| `middleware/degradation.go` | 549 | ❌ M2（promauto 包级注册）|
| `middleware/adaptive.go` | 566 | ✅ 健康（局部注册，设计正确）|
| `middleware/circuitbreaker.go` | 367 | ✅ 健康 |
| `middleware/dedup.go` | 372 | ✅ 健康 |
| `middleware/retry.go` | 292 | ✅ 健康 |
| `middleware/simple.go` | 240 | ⚠️ M4（RateLimit 选择指南） |
| `middleware/context_keys.go` | 13 | ⚠️ M6（常量不完整）|
| `middleware/hotreload/hotreload.go` | 112 | ⚠️ M3（缺 Dedup/Degradation 支持）|

---

*本报告基于 2026-02-25 代码快照，项目未发布，所有修复建议均可在正式发布前实施。*

