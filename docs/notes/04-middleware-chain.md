# 中间件链与自适应能力——可观测性、弹性、热更新

## 设计理念

中间件是 Remilia 框架的"弹性层"，采用经典的**洋葱模型**设计：

```
Inbound → [Logging] → [RateLimit] → [CircuitBreaker] → Handler → Outbound
               ↑           ↑               ↑              │
               └───────────┴───────────────┴──────────────┘
                          Error path
```

中间件的职责分离：
- **横切关注点**：日志、指标、追踪——与业务无关
- **弹性模式**：限流、熔断、降级、重试、去重——保障系统稳定性
- **安全控制**：权限校验、panic 恢复——防御性编程

## 架构实现

### 1. 三层中间件结构

引擎的中间件分为三个层级：

```go
type middlewareState struct {
    global           *middlewareSnapshot           // 全局（所有匹配器）
    groupMiddlewares map[string]*middlewareSnapshot // 分组
}

// 匹配器级中间件存储在 Matcher.middlewares 中
type Matcher struct {
    middlewares []context.Middleware  // 匹配器局部中间件
}
```

合并优先级：**全局中间件 → 分组中间件 → 匹配器局部中间件**

```go
func (e *Engine) ensureMatcherChainWithState(m *Matcher, mwState *middlewareState) {
    groupName := m.group
    groupSnap := mwState.groupMiddlewares[groupName]
    globalSnap := mwState.global

    // 使用代际号（gen）避免不必要的重建
    m.ensureChain(globalSnap.chain, globalSnap.gen,
                  groupSnap.chain, groupSnap.gen)
}
```

### 2. 三级调用路径（性能关键优化）

`invokeHandler` 是事件处理的热路径，经优化后有三条路径：

```go
func (e *Engine) invokeHandler(ctx *context.Context, m *Matcher) {
    // ── 快速终止 ──
    if m.rt.deleted.Load() { return }

    // ── 超高速路径（99.9% 的调用）──
    cv := m.compiledVersion.Load()
    if cc := m.compiledHandlers.Load(); cc != nil && cc.version == cv {
        finalHandler = cc.head  // 直接使用缓存的编译后处理器
        // ↑ 只用了 2 次 atomic.Load + 1 次整数比较
    }

    // ── 慢速路径（首次/变更后）──
    if finalHandler == nil {
        // 重建中间件链，重新编译
        e.ensureMatcherChainWithState(m, mwState)
        chain, _, _ := m.getChainCache()
        finalHandler = e.getOrBuildIterChain(m, chain, handlerErr)
    }

    err := finalHandler(ctx)
}
```

**版本计数器代替 reflect 指纹**是关键优化：

```go
// 旧方案：每次调用都执行 handlerID(reflect.ValueOf.Pointer) + chainSignature
// → 1000-matcher 场景产生 ~11000 次 reflect 调用/事件，占总 CPU 的 21%

// 新方案：m.compiledVersion（atomic.Uint64 计数器）
// → Fast path：1 次 atomic.Load + 1 次整数比较，零 reflect 调用
//
// compiledVersion 在以下时机递增：
//   - invalidateCombinedChain()：中间件链重建时
//   - ensureChain()：combined chain 实际更新时
//   - Handle()：handler 更换时
```

### 3. 迭代式中间件执行

```go
func (e *Engine) getOrBuildIterChain(m *Matcher, chain []context.Middleware, he context.Handler) context.Handler {
    if len(chain) == 0 {
        return he  // 无中间件时直接返回 handler，零开销
    }

    // 从右到左构建，只缓存头部
    tmp := make([]context.Handler, len(chain)+1)
    tmp[len(chain)] = he
    for i := len(chain) - 1; i >= 0; i-- {
        tmp[i] = chain[i](tmp[i+1])
    }
    // 只有 tmp[0] 被缓存，其余通过闭包链保持存活
    return tmp[0]
}
```

相比递归调用的嵌套闭包方案，迭代构建避免了过深的调用栈和闭包捕获开销。

## 内置中间件详解

### Recover——自适应堆栈缓冲

```go
func Recover() eventctx.Middleware {
    return func(next eventctx.Handler) eventctx.Handler {
        return func(ctx *eventctx.Context) (err error) {
            defer func() {
                if r := recover(); r != nil {
                    stack := captureStack() // 4KB → 64KB 自适应
                    logger.WithField("stack", stack).
                        Error("[Recover] Panic recovered")
                    err = fmt.Errorf("panic recovered: %v", r)
                }
            }()
            return next(ctx)
        }
    }
}
```

`captureStack()` 使用自适应堆栈缓冲：先从 4KB 开始，如果截断则扩大到 64KB，避免深调用栈信息丢失。

### AdaptiveRateLimiter——自适应限流

```go
type AdaptiveRateLimiter struct {
    mu         sync.RWMutex
    currentRate int64          // 原子变量
    maxRate    int64
    adjustStep int64           // 热更新
    // 系统负载指标
    cpuUsage   atomic.Int64
    memUsage   atomic.Int64
}

func (arl *AdaptiveRateLimiter) allow() bool {
    cpu := arl.cpuUsage.Load()
    if cpu > 80 {  // CPU > 80% 时主动降速
        arl.adjustRate(-arl.adjustStep)
    } else if cpu < 40 {
        arl.adjustRate(arl.adjustStep)
    }
    return arl.currentRate > 0
}
```

每个 Bot 实例有独立的 Prometheus 指标，支持热更新速率阈值。

### CircuitBreaker——熔断器

```go
type CircuitBreaker struct {
    state       atomic.Int32  // 0=closed, 1=open, 2=half-open
    failCount   atomic.Int64
    threshold   int64         // 熔断阈值
    recoveryTimeout time.Duration

    // 自动恢复
    lastFailure atomic.Value  // time.Time
}
```

三态有限状态机：`Closed → Open（超时）→ Half-Open（试探请求成功）→ Closed`

### DedupFilter——去重

使用 LRU 缓存 + TTL 实现事件去重。支持 `hotreload.Bridge` 热更新 `MaxSize` 和 `DefaultTTL`。

### AdaptiveDegradation——自适应降级

根据系统 CPU、内存、延迟三个维度综合判断是否需要降级：

```go
func (ad *AdaptiveDegradation) shouldDegrade() bool {
    cpu := ad.cpuThreshold.Load()
    mem := ad.memThreshold.Load()
    lat := ad.latencyThreshold.Load()

    metrics := ad.getSystemMetrics()
    return metrics.CPUPercent > cpu ||
           metrics.MemPercent > mem ||
           metrics.AvgLatency > lat
}
```

### SlowHandler——慢调用检测

```go
func SlowHandler(threshold time.Duration) eventctx.Middleware {
    return func(next eventctx.Handler) eventctx.Handler {
        return func(ctx *eventctx.Context) error {
            start := time.Now()
            err := next(ctx)
            if elapsed := time.Since(start); elapsed > threshold {
                logger.WithFields(logger.Fields{
                    "elapsed": elapsed,
                    "threshold": threshold,
                }).Warn("[SlowHandler] handler exceeded threshold")
            }
            return err
        }
    }
}
```

### DeadLetter——死信队列

当重试耗尽时，将失败消息持久化到死信队列：

```go
type DeadLetterConfig struct {
    MaxRetries  int
    BackoffBase time.Duration
    QueueSize   int
    // 持久化存储
    Store func(msg DeadLetterMessage) error
}
```

支持消费者机制，可以人工重新消费死信消息。

## 配置热更新集成

中间件与 `config.hotreload.Bridge` 集成，实现运行时参数调整：

```go
bridge := hotreload.NewBridge()
bridge.WatchAdaptive(limiter)      // 限流阈值
bridge.WatchRetry(retrier)         // 重试次数/退避
bridge.WatchCircuitBreaker(cb)     // 熔断阈值
bridge.WatchDedup(dedup)           // 去重缓存大小
bridge.WatchDegradation(degradation)  // 降级阈值

config.Subscribe(bridge.OnConfigChange)
```

配置文件变更后，中间件参数**秒级生效**，无需重启 Bot。

## 迭代过程

### V1：中间件在根包，结构杂乱

最初的中间件实现在根包（`middleware/middleware.go`），使用全局函数 + 闭包组合。当时只有 `Logging`、`Recover`、`RateLimit` 三个中间件，中间件类型直接定义为 `func(Handler) Handler`：

```go
// V1 代码 — 根包 middleware/middleware.go
type Handler func(ctx *Context) error

// 中间件将 Handler 包裹为新的 Handler
type Middleware func(Handler) Handler
```

**问题**：
- 没有分组中间件概念——所有中间件对全部匹配器生效
- 每次事件处理都重新遍历中间件列表构建链，无缓存
- 中间件类型与 `core/context` 中的 Handler 类型分离，需要在不同包之间转换
- 没有慢路径/快路径分离——每次调用都走完整的 reflect 指纹比对

### V2：三层结构 + 代际号缓存

中间件演进为三层（全局 / 分组 / 匹配器级）+ 引入 `middlewareSnapshot` 代际号：

```go
// V2 — middlewareSnapshot + 代际号
type middlewareSnapshot struct {
    chain []context.Middleware
    gen   uint64  // 每次更新递增
}

type middlewareState struct {
    global           *middlewareSnapshot
    groupMiddlewares map[string]*middlewareSnapshot
}
```

**核心优化**：Matcher 缓存自己编译后的中间件链，只有在 `gen` 不匹配时才重建。

```go
func (m *Matcher) ensureChain(global []Middleware, globalGen uint64,
    group []Middleware, groupGen uint64) {
    // 只在实际变更时才重建，大多数 Event 处理走缓存
    if m.cachedGlobalGen == globalGen && m.cachedGroupGen == groupGen {
        return // 缓存有效，跳过
    }
    m.rebuildChain(global, group)
}
```

这个优化使大多数事件处理无需重建中间件链。

### V3：版本计数器替代 reflect 指纹（性能关键）

V2 的中间件链缓存有一个严重问题：**缓存命中检测依赖 reflect 指纹**。

```go
// V2 缓存检测 — 使用 reflect 对比 handler 和 chain 的标识
func handlerID(h Handler) uintptr {
    return reflect.ValueOf(h).Pointer()
}

func chainSignature(chain []Middleware) string {
    sig := make([]byte, len(chain)*8)
    for i, m := range chain {
        binary.LittleEndian.PutUint64(sig[i*8:],
            uint64(reflect.ValueOf(m).Pointer()))
    }
    return string(sig)
}
```

在 1000 匹配器的场景下，每次事件处理都需要：
- 调用 `handlerID` 1 次（reflect）
- `chainSignature` 遍历所有中间件并调用 reflect 每个指针

总共 **~11000 次 reflect 调用/事件**，占总 CPU 的 **21%**。

根本原因：reflect 无法区分同一类型的两个不同闭包实例——所以用指针地址来当"标识"，但获取指针地址必须经过 `reflect.ValueOf().Pointer()`，这本身就很慢。

```go
// V3（当前）— 版本计数器，零 reflect
// Matcher 维护一个 atomic.Uint64 版本号
type Matcher struct {
    compiledVersion atomic.Uint64        // 递增计数器
    compiledHandlers atomic.Value        // 缓存编译后的 handler 链
}

// 版本号在以下时机递增：
// - invalidateCombinedChain(): 中间件链重建
// - ensureChain(): combined chain 实际更新
// - Handle(): handler 更换

func (e *Engine) invokeHandler(ctx *context.Context, m *Matcher) {
    // Fast path: 1 次 atomic.Load + 1 次整数比较
    cv := m.compiledVersion.Load()
    if cc := m.compiledHandlers.Load(); cc != nil && cc.version == cv {
        finalHandler = cc.head  // 直接使用，零 reflect
    }
    // Slow path: 重建链并更新版本号
    // ...
}
```

**效果**：单次事件处理从 11K reflect 调用降为 **2 次 atomic.Load + 1 次整数比较**。

### V4：从嵌套闭包到迭代构建

V3 之前，中间件链的构建方式是递归嵌套闭包：

```go
// V3 构建方式 — 嵌套闭包（递归）
func buildChain(handler Handler, chain []Middleware, index int) Handler {
    if index >= len(chain) {
        return handler
    }
    next := buildChain(handler, chain, index+1)
    return chain[index](next)
}
```

虽然正确，但递归构建导致：
- 深调用栈（中间件多时可能十几层）
- 编译器难以内联
- 每层闭包都捕获了 `next`，GC 压力大

```go
// V4（当前）— 迭代构建 + 只缓存头部
func (e *Engine) getOrBuildIterChain(m *Matcher, chain []context.Middleware,
    he context.Handler) context.Handler {
    if len(chain) == 0 {
        return he  // 无中间件时零开销
    }
    // 从右到左构建，使用临时切片避免递归
    tmp := make([]context.Handler, len(chain)+1)
    tmp[len(chain)] = he
    for i := len(chain) - 1; i >= 0; i-- {
        tmp[i] = chain[i](tmp[i+1])
    }
    // 只有 tmp[0] 被缓存，其余通过闭包链保持存活
    return tmp[0]
}
```

关键设计：只有 `tmp[0]`（最外层）被缓存，`tmp[1..N]` 通过闭包引用链保持存活，不需要额外的切片引用——这防止了大切片阻止 GC 回收。

## 迭代历程

| 版本 | 核心变化 | 动机 |
|------|---------|------|
| V1 | 根包全局中间件，无缓存 | 快速实现 |
| V2 | 三层结构 + 代际号缓存 | 分组支持 + 避免重复构建 |
| V3 | 版本计数器替代 reflect 指纹 | reflect 调用占总 CPU 21% |
| V4（当前） | 迭代构建替代嵌套闭包 | 减少调用栈深度和 GC 压力 |

## 性能基准

| 中间件 | 单次开销 | 说明 |
|--------|---------|------|
| Logging | ~100 ns | 零分配日志 |
| Recover | ~5 ns | 仅 defer 开销 |
| RateLimit | ~50 ns | 原子计数 |
| CircuitBreaker | ~30 ns | 原子状态检查 |
| Dedup | ~80 ns | LRU 缓存查找 |
| 全链（8 个中间件） | ~500 ns | 相对于 Handler 处理时间可忽略 |
