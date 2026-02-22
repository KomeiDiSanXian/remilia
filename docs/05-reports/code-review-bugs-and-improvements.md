# 代码审查报告 — Bug 与改进分析

**项目**: remilia  
**日期**: 2026-02-22  
**范围**: 全模块 Bug 扫描 + 高收益改进点分析  
**审查人**: GitHub Copilot

---

## 目录

1. [严重等级说明](#严重等级说明)
2. [汇总表](#汇总表)
3. [core/engine](#1-coreengine)
4. [infra/dlq](#2-infradlq)
5. [bot.go](#3-botgo)
6. [middleware](#4-middleware)
7. [plugin](#5-plugin)
8. [config](#6-config)
9. [infra/logger](#7-infralogger)
10. [infra/httpclient](#8-infrahttpclient)
11. [helper](#9-helper)
12. [修复优先级路线图](#修复优先级路线图)

---

## 严重等级说明

| 等级 | 含义 |
|------|------|
| 🔴 Critical | 必然或极有可能在生产环境触发崩溃 / 数据丢失 |
| 🟠 High | 高并发或特定时序下会引发错误，影响可靠性 |
| 🟡 Medium | 低概率触发或仅影响资源效率，有明确改进方案 |
| 🟢 Low | 代码质量、可维护性改进，无直接功能影响 |

---

## 汇总表

| # | 模块 | 文件:行 | 等级 | 类型 | 简述 |
|---|------|---------|------|------|------|
| 1 | core/engine | process.go:151 | 🔴 Critical | Bug | `ProcessEventBatch` 未受 `shutdownMu` 保护，存在 WaitGroup Add/Wait 竞态 |
| 2 | core/engine | process.go:348 | 🟡 Medium | Bug | 中间件链缓存仅按长度校验，内容变更时命中旧缓存 |
| 3 | infra/dlq | dlq.go:143 | 🟠 High | Bug | worker `ctx.Done()` 分支提前退出，queue 中剩余消息静默丢失 |
| 4 | infra/dlq | dlq.go:103 | 🟡 Medium | Bug | `Start()` 缺少幂等保护，多次调用启动重复 worker |
| 5 | middleware | circuitbreaker.go:122 | 🟠 High | Bug | `setState()` 锁外执行，`OnStateChange` 回调在并发下重复触发 |
| 6 | middleware | retry.go:91 | 🟢 Low | Bug | 退避计算在 `attempt >= 63` 时整数溢出 |
| 7 | bot.go | bot.go:443 | 🟡 Medium | Bug | `WaitForShutdown()` 未调用 `signal.Stop(sigCh)`，信号监听泄漏 |
| 8 | plugin | v2.go:553 | 🟡 Medium | Bug | `RegisterV2` 锁外执行 Setup，热重载时依赖插件可能被并发卸载 |
| 9 | helper | helper.go:18 | 🟠 High | Bug | `StringToBytes` unsafe 转换读取 `cap` 字段超出 string 结构范围（UB） |
| 10 | core/engine | state.go | 🟢 Low | 改进 | COW 写操作每次深拷贝全部 map，大规模匹配器时写延迟随数量线性增长 |
| 11 | plugin | eventbus.go:96 | 🟡 Medium | 改进 | EventBus 池满降级时无限新建 goroutine，慢消费者场景 goroutine 无上界 |
| 12 | config | config.go:197 | 🟡 Medium | 改进 | `Subscribe` 无法单独移除某个监听器，长期运行服务无法清理特定监听器 |
| 13 | config | watcher.go | 🟢 Low | 改进 | `Watcher` 使用 `context.Background()`，无法从外部生命周期控制停止 |
| 14 | infra/logger | logger.go:79 | 🟡 Medium | 改进 | 打开的日志文件句柄未保存，无法在运行时关闭或轮转 |
| 15 | infra/httpclient | client.go:422 | 🟡 Medium | 改进 | 重试等待使用 `time.After`，高并发下 timer 不被提前回收，累积内存压力 |
| 16 | middleware | dedup.go:226 | 🟢 Low | 改进 | `cleanup()` 方法是死代码，与内联 goroutine 逻辑重复但从未被调用 |

---

## 1. core/engine

### 🔴 [BUG-1] `ProcessEventBatch` 未受 `shutdownMu` 保护

**文件**: `core/engine/process.go:151-156`

**问题描述**:

本次已修复的 `ProcessEvent` 用 `shutdownMu.RLock()` 保护了"检查 shutdown → Add(1)"的原子性，但同文件的 `ProcessEventBatch` 仍使用裸操作：

```go
// process.go:151-156
if e.shutdown.Load() {   // ← 无 shutdownMu 保护
    return
}
e.eventWg.Add(1)         // ← 与 Shutdown 的 eventWg.Wait() 存在竞态
defer e.eventWg.Done()
```

**影响**: 与 `ProcessEvent` 修复前相同的 WaitGroup Add/Wait 竞态。在以下时序下触发：
1. `ProcessEventBatch` 读取 `shutdown=false` → 通过检查
2. `Shutdown` 执行 `shutdown.Store(true)`，启动 `eventWg.Wait()` goroutine
3. `ProcessEventBatch` 执行 `eventWg.Add(1)` → **与 Wait() 并发**

Go 运行时行为：`sync.WaitGroup` 明确禁止在计数为零时并发 `Add` 和 `Wait`，可能引发 panic。

**修复方案**:

```go
func (e *Engine) ProcessEventBatch(events []*dto.Payload, api openapi.OpenAPI) {
    if len(events) == 0 {
        return
    }
    e.shutdownMu.RLock()          // ← 新增
    if e.shutdown.Load() {
        e.shutdownMu.RUnlock()    // ← 新增
        return
    }
    e.eventWg.Add(1)
    e.shutdownMu.RUnlock()        // ← 新增
    defer e.eventWg.Done()
    // ... 其余代码不变
}
```

---

### 🟡 [BUG-2] 中间件链缓存按长度校验，内容变更时误命中旧缓存

**文件**: `core/engine/process.go:348`

**问题描述**:

`getOrBuildIterChain` 的缓存命中条件：

```go
// process.go:348
if cc.handlerSig == hid && len(cc.handlers) == len(chain)+1 {
    return cc.handlers[0]   // ← 只检查切片长度，不检查内容
}
```

**触发场景**: 用户先注册 3 个中间件 `[A, B, C]`，再调用 `engine.Use(...)` 替换为不同的 3 个中间件 `[X, Y, Z]`（数量相同）。缓存不失效，实际执行的仍是旧的 `[A, B, C]` 链。

**影响**: 热更新中间件配置后，行为不符合预期，且难以排查（无任何日志提示缓存命中）。

**修复方案**: 对 `chain` 中各中间件函数指针计算指纹，加入缓存命中条件：

```go
type compiledChain struct {
    handlers   []context.Handler
    handlerSig uintptr
    chainSig   uint64   // 新增：中间件内容指纹
}

func chainSignature(chain []Middleware) uint64 {
    var sig uint64
    for _, m := range chain {
        sig ^= uint64(reflect.ValueOf(m).Pointer())
    }
    return sig
}

// 命中检查
sig := chainSignature(chain)
if cc.handlerSig == hid && cc.chainSig == sig {
    return cc.handlers[0]
}
```

---

### 🟢 [IMPROVE-10] COW 写操作全量深拷贝 map 的性能瓶颈

**文件**: `core/engine/state.go`（`copyEngineState` 函数）

**问题描述**:

每次注册/删除 Matcher 都会通过 `copyEngineState` 深拷贝 `matcherIndex`、`sortedCache`、`commandIndex`、`commandListCache` 等全部 map。在 Matcher 数量较多（>1000）时，写操作延迟会线性增长。

**影响**: 主要影响启动阶段批量注册场景（如 plugin 批量注册命令时）。

**建议**:
1. 提供 `BatchRegister(matchers []*Matcher)` 接口，将多次 COW 写合并为一次。
2. `commandListCache`（仅用于 help 插件列举，读取远多于写入）可以单独惰性重建，不在每次写 `matcherIndex` 时同步拷贝。

---

## 2. infra/dlq

### 🟠 [BUG-3] Worker `ctx.Done()` 分支提前退出，channel 中剩余消息静默丢失

**文件**: `infra/dlq/dlq.go:143-148`

**问题描述**:

`Shutdown()` 的执行顺序为：`cancel()` → （`sendMu.Lock`/`Unlock`）→ `close(queue)`。但 worker 的 `select` 中：

```go
// dlq.go:143
case <-dlq.ctx.Done():
    // 注释称 "we'll naturally exit via ok==false above"
    // 但 select 的分支选择是随机的！
    return   // ← 若此分支先被选中，queue 中剩余消息被丢弃
```

**触发时序**:
1. `cancel()` 被调用，`dlq.ctx.Done()` 变为就绪
2. 与此同时，`queue` 中有未处理的消息（`queue <- item` case 也就绪）
3. Go 运行时**随机**选择其中一个 case — 选中 `ctx.Done()` 时，剩余消息永远不会被消费
4. 这些消息**不会**触发 `OnDropped` 回调，属于静默丢失

**修复方案**: 删除 worker 中的 `ctx.Done()` case，改用 `range` 自动感知 `close`：

```go
func (dlq *DeadLetterQueue) worker(id int) {
    defer dlq.wg.Done()
    for item := range dlq.queue {   // close(queue) 后自动退出，不遗漏任何消息
        consumers := dlq.consumerSnap.Load().([]DeadLetterConsumer)
        // ... 处理 item
    }
}
```

`cancel()` 的唯一职责是打断 `enqueueBlockUntilSpace` 中的阻塞生产者，不应影响消费侧。

---

### 🟡 [BUG-4] `Start()` 缺少幂等保护，多次调用导致消息重复消费

**文件**: `infra/dlq/dlq.go:103`

**问题描述**:

```go
func (dlq *DeadLetterQueue) Start() {
    // 没有任何已启动检查
    for i := 0; i < dlq.config.Workers; i++ {
        dlq.wg.Add(1)
        go dlq.worker(i)    // 每次调用都新增 Workers 个 goroutine
    }
}
```

**影响**: 若调用方多次调用 `Start()`（如热重载或防御性代码），同一消息会被多组 worker 竞争消费，导致 `Consume()` 被多次调用。

**修复方案**:

```go
type DeadLetterQueue struct {
    // ... 现有字段
    startOnce sync.Once   // 新增
}

func (dlq *DeadLetterQueue) Start() {
    dlq.startOnce.Do(func() {
        for i := 0; i < dlq.config.Workers; i++ {
            dlq.wg.Add(1)
            go dlq.worker(i)
        }
        // ... 日志
    })
}
```

---

## 3. bot.go

### 🟡 [BUG-7] `WaitForShutdown()` 信号监听 channel 泄漏

**文件**: `bot.go:443`

**问题描述**:

```go
func (b *Bot) WaitForShutdown() {
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

    <-sigCh   // 收到信号后直接往下走，未清理注册
    // 缺少 signal.Stop(sigCh)
    // ...
}
```

**影响**:
1. 每次调用 `WaitForShutdown()`（如测试中多次启停）都会遗留一个 `sigCh` 的信号注册，后续信号会投递到已废弃的 channel。
2. Go 官方文档明确要求 `signal.Notify` 后应配对调用 `signal.Stop`，否则该 channel 不会被垃圾回收。
3. 在测试环境中，累积的注册可能导致信号丢失或测试卡死。

**修复方案**（一行修改）:

```go
func (b *Bot) WaitForShutdown() {
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
    defer signal.Stop(sigCh)   // ← 新增此行

    <-sigCh
    // ...
}
```

---

## 4. middleware

### 🟠 [BUG-5] `CircuitBreaker.setState()` 锁外执行，`OnStateChange` 并发下重复触发

**文件**: `middleware/circuitbreaker.go:122`

**问题描述**:

```go
func (cb *CircuitBreaker) setState(newState CircuitBreakerState) {
    oldState := cb.GetState()    // ← atomic.Load（非原子性的 Load-Compare-Store）
    if oldState == newState {
        return
    }
    cb.state.Store(newState)     // ← atomic.Store
    // ...
    cb.config.OnStateChange(oldState, newState)   // ← 回调
}
```

**并发触发路径** (`onFailure` → `setState`)：

```go
// 10 个并发 goroutine 都执行 failures.Add(1) 并得到 >= MaxFailures
case StateClosed:
    failures := cb.failures.Add(1)
    if failures >= int32(cb.config.MaxFailures) {
        cb.setState(StateOpen)   // ← 10 个 goroutine 都进入这里
    }
```

每个 goroutine 在 `setState` 中都读到 `oldState==Closed`（因为 Store 还未发生），然后各自 Store 并触发 `OnStateChange(Closed → Open)` 回调——**回调被调用 10 次**，而期望只调用 1 次。

**影响**: 若 `OnStateChange` 回调有副作用（如发送告警、记录指标），会产生重复操作。

**修复方案**: 使用锁保证整个 Load-Compare-Store-Callback 序列的原子性：

```go
func (cb *CircuitBreaker) setState(newState CircuitBreakerState) {
    cb.mu.Lock()
    defer cb.mu.Unlock()
    oldState := cb.state.Load().(CircuitBreakerState)
    if oldState == newState {
        return
    }
    cb.state.Store(newState)
    // ... 日志
    if cb.config.OnStateChange != nil {
        cb.config.OnStateChange(oldState, newState)
    }
}
```

注意：`canExecute()` 中已存在锁保护的状态转换路径（`StateOpen → StateHalfOpen`），`setState` 加锁后需确认这些路径不会产生死锁（`canExecute` 中的锁是独立的 `cb.mu.Lock()`，与 `setState` 使用同一把锁会死锁）。正确方案是提供 `setStateUnderLock` 内部方法在已持锁时调用：

```go
// setStateLocked 在已持有 mu 的情况下使用
func (cb *CircuitBreaker) setStateLocked(oldState, newState CircuitBreakerState) bool {
    if cb.state.Load().(CircuitBreakerState) != oldState {
        return false
    }
    cb.state.Store(newState)
    if cb.config.OnStateChange != nil {
        cb.config.OnStateChange(oldState, newState)
    }
    return true
}
```

---

### 🟢 [BUG-6] Retry 退避计算整数溢出

**文件**: `middleware/retry.go:91`

**问题描述**:

```go
delay := min(cfg.BackoffBase*time.Duration(1<<uint(attempt)), cfg.BackoffMax)
```

`1 << uint(attempt)` 在 `attempt >= 63`（64 位平台）时溢出，得到 0 或负数。`BackoffBase * 负数` 为负 `time.Duration`，`time.Sleep(负数)` 立即返回，退避机制完全失效，导致无间隔重试。

**影响**: 默认 `MaxAttempts=3` 时安全，但用户可自定义 `MaxAttempts=100`，届时从第 64 次重试开始退避失效。

**修复方案**（两行）:

```go
const maxBackoffShift = 62
shift := uint(attempt)
if shift > maxBackoffShift {
    shift = maxBackoffShift
}
delay := min(cfg.BackoffBase*time.Duration(1<<shift), cfg.BackoffMax)
```

---

### 🟢 [IMPROVE-16] `DedupFilter.cleanup()` 是死代码

**文件**: `middleware/dedup.go:226`

**问题描述**:

`cleanup()` 方法（`dedup.go:226`）实现了与 `NewDedupFilterWithContext` 中内联 goroutine 完全相同的逻辑，但**从未被调用**。这是一段死代码。

维护风险：若修改内联 goroutine 的逻辑，容易忘记同步修改 `cleanup()`，导致两份代码分叉。

**建议**: 删除 `cleanup()` 方法，或将内联 goroutine 改为调用 `d.cleanup(interval)`。

---

## 5. plugin

### 🟡 [BUG-8] `RegisterV2` 锁外执行 Setup，热重载时依赖插件可能被并发卸载

**文件**: `plugin/v2.go:553`

**问题描述**:

`RegisterV2` 的执行流程：

```
加锁 → 写入 plugins[name](state=Loading) → 解锁
                                            ↓
                               【锁外】 instance.Load() → desc.Setup(setupCtx)
                                            ↓
加锁 → 处理结果 → 解锁
```

在 `desc.Setup(setupCtx)` 执行期间（可能耗时较长），`setupCtx.Get("depPlugin")` 获取的依赖插件指针是"当前时刻"的实例。若此时另一个 goroutine 调用 `pm.Unregister("depPlugin")` 卸载该依赖：

1. `Unregister` 获取写锁，将依赖从 `pm.plugins` 移除，调用 `Teardown()`
2. `Setup` 函数继续使用已被 Teardown 的依赖实例 → **悬空指针 / 资源已释放后使用**

**影响**: 仅在并发调用 `RegisterV2` + `Unregister`（热重载场景）时出现。

**建议**:

在 `RegisterV2` 的 Setup 执行期间，对依赖插件持有"引用"以防止其被卸载：

```go
// 在 pm.mu.Unlock() 之前，记录所有依赖名称
// Setup 执行完后再允许依赖被卸载
// 实现方式：依赖引用计数，或 Setup 期间持有读锁（不推荐，因 Setup 可能耗时）
```

---

### 🟡 [IMPROVE-11] EventBus 池满降级时 goroutine 无上界

**文件**: `plugin/eventbus.go:96`

**问题描述**:

```go
default:
    // 池已满：不阻塞调用方，直接启动 goroutine（不计入池）
    logger.Warnf("[EventBus] Worker pool full for topic %s, running handler without pool limit", topic)
    go func(h EventHandler) { h(data) }(sub.handler)
```

当 `workerPool`（容量 100）满载时，每次 `Publish` 都无限制地新建 goroutine。若订阅者处理缓慢（如调用外部 API、写数据库），突发流量下 goroutine 数量可快速增长至数万，耗尽内存。

**建议**:

将降级策略改为**有界丢弃**，并暴露丢弃计数指标：

```go
type eventBus struct {
    // ...
    droppedCount atomic.Int64   // 新增
}

// Publish 中
default:
    eb.droppedCount.Add(1)
    logger.Warnf("[EventBus] Worker pool full, dropping event for topic %s (total dropped: %d)",
        topic, eb.droppedCount.Load())
    // 不启动 goroutine，直接丢弃
```

---

## 6. config

### 🟡 [IMPROVE-12] `Subscribe` 无法单独取消注册，导致监听器累积

**文件**: `config/config.go:197`

**问题描述**:

```go
func Subscribe(listener ChangeListener) {
    changeListeners = append(changeListeners, listener)
}
// 只有 UnsubscribeAll()，无精确��消
```

**影响**:
1. 插件卸载后，其配置监听器仍保留在全局列表中，每次配置变更都会触发已卸载插件的回调（可能引发 nil pointer）。
2. 若插件每次加载时调用 `Subscribe`，且使用了 `UnsubscribeAll` 之外的清理方式，监听器数量随热重载次数线性增长。

**建议**: 返回不透明 token 支持精确取消：

```go
type ListenerToken struct {
    id   int64
    once sync.Once
}

func (t *ListenerToken) Cancel() {
    t.once.Do(func() { unsubscribeByID(t.id) })
}

func Subscribe(listener ChangeListener) *ListenerToken {
    id := atomic.AddInt64(&listenerCounter, 1)
    changeListenersMu.Lock()
    changeListeners = append(changeListeners, listenerEntry{id: id, fn: listener})
    changeListenersMu.Unlock()
    return &ListenerToken{id: id}
}
```

---

### 🟢 [IMPROVE-13] `Watcher` 无法从外部生命周期控制停止

**文件**: `config/watcher.go`

**问题描述**:

`Watcher` 内部使用 `context.Background()`，生命周期完全自管理。调用方必须显式调用 `watcher.Stop()`，否则 goroutine 持续运行。与项目中 `AdaptiveRateLimiter`、`DedupFilter`、`token.Manager` 的 `WithContext` 模式不一致。

**建议**: 提供 `NewWatcherWithContext` 构造函数：

```go
func NewWatcherWithContext(ctx context.Context, path string) (*Watcher, error) {
    w, err := NewWatcher(path)
    if err != nil {
        return nil, err
    }
    go func() {
        <-ctx.Done()
        w.Stop()
    }()
    return w, nil
}
```

---

## 7. infra/logger

### 🟡 [IMPROVE-14] 日志文件句柄未保存，无法关闭或轮转

**文件**: `infra/logger/logger.go:79`

**问题描述**:

```go
file, err := os.OpenFile(cfg.FilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
if err != nil {
    // ...
} else {
    writers = append(writers, file)
    // file 在函数结束后超出作用域，没有保存到任何地方
}
```

**影响**:
1. `Init()` 被多次调用时（如测试或配置热重载），前次的 `*os.File` 没有被 `Close()`，文件描述符泄漏。
2. 不支持日志文件轮转（如按天切换日志文件）。
3. 进程退出时，缓冲区中未 flush 的日志可能丢失（`os.File` 在 GC 时调用 `Finalize` 关闭，但不保证 flush 时机）。

**建议**:

```go
var (
    logFile    *os.File
    logFileMu  sync.Mutex
)

func Init(cfg LogConfig) {
    logFileMu.Lock()
    defer logFileMu.Unlock()

    if logFile != nil {
        _ = logFile.Close()
        logFile = nil
    }
    // ...
    if ok {
        logFile = file
    }
}

// CloseLogFile 关闭日志文件（在 Bot.Stop() 中调用）
func CloseLogFile() {
    logFileMu.Lock()
    defer logFileMu.Unlock()
    if logFile != nil {
        _ = logFile.Close()
        logFile = nil
    }
}
```

---

## 8. infra/httpclient

### 🟡 [IMPROVE-15] 重试等待使用 `time.After`，timer 不被提前回收

**文件**: `infra/httpclient/client.go:422`

**问题描述**:

```go
select {
case <-time.After(waitTime):   // ← 每次调用创建一个新 Timer
case <-req.Context().Done():
    return nil, req.Context().Err()
}
```

`time.After` 创建的 `Timer` 只有在 `waitTime` 到期后才能被 GC 回收。若 context 先取消（走 `ctx.Done()` 分支），该 timer 在 `waitTime` 期间持续占用内存和 goroutine 调度资源。

**影响**: 在高并发场景下（如 1000 QPS × 3 次重试 × 1s 等待），同一时刻可能存在数千个仍在计时的 timer 对象，形成隐性内存压力。

**修复方案**（4 行）:

```go
retryTimer := time.NewTimer(waitTime)
select {
case <-retryTimer.C:
case <-req.Context().Done():
    retryTimer.Stop()   // ← 立即释放 timer 资源
    return nil, req.Context().Err()
}
```

---

## 9. helper

### 🟠 [BUG-9] `StringToBytes` 的 unsafe 转换读取超出 string 结构范围

**文件**: `helper/helper.go:18`

**问题描述**:

```go
func StringToBytes(s string) (b []byte) { return *(*[]byte)(unsafe.Pointer(&s)) }
```

Go 内存布局对比：
```
string: { data *byte, len int }         ← 2 个字段，16 字节（64 位）
[]byte: { data *byte, len int, cap int } ← 3 个字段，24 字节（64 位）
```

将 `&s`（16 字节）强制解读为 `*[]byte`（24 字节）时，`cap` 字段读取的是 `s` 在栈上后面相邻的 8 字节——可能是另一个局部变量、函数返回地址，或其他任意内存。

**实际影响**:
1. `cap(result)` 返回垃圾值，任何基于 `cap` 的操作（如 `append`、切片扩容）行为未定义。
2. 若调用方对返回的 `[]byte` 执行写操作，会修改 string 底层的只读内存（在只读数据段时导致 SIGSEGV）。
3. 在 `-gcflags=all=-d=checkptr` 下会直接 panic。
4. 编译器的逃逸分析和内联可能改变栈布局，导致行为随版本变化。

> `BytesToString`（`helper.go:13`）的反向方向是安全的：`[]byte`（24 字节）转 `string`（只读前 2 个字段 = 16 字节），不会越界。

**修复方案**（推荐 Go 1.20+）:

```go
// StringToBytes 零拷贝将 string 转为只读 []byte
// 注意：返回的 []byte 不可修改，其底层是 string 的只读内存
func StringToBytes(s string) []byte {
    if s == "" {
        return nil
    }
    return unsafe.Slice(unsafe.StringData(s), len(s))
}
```

若需要可修改的 `[]byte`，请使用标准转换 `[]byte(s)`（有内存拷贝，但完全安全）。

---

## 修复优先级路线图

### 第一批（立即）—— 正确性/安全性

| # | 文件 | 修改内容 | 预计工时 |
|---|------|---------|---------|
| BUG-1 | process.go | `ProcessEventBatch` 加 `shutdownMu.RLock()` 保护 | 0.5h |
| BUG-9 | helper.go | 修复 `StringToBytes` 使用 `unsafe.Slice` + `unsafe.StringData` | 0.5h |
| BUG-3 | dlq.go | worker 改用 `range dlq.queue`，移除 `ctx.Done()` case | 1h |
| BUG-5 | circuitbreaker.go | `setState` 使用 `setStateLocked` 模式，防止重复回调 | 2h |

### 第二批（本月）—— 可靠性提升

| # | 文件 | 修改内容 | 预计工时 |
|---|------|---------|---------|
| BUG-7 | bot.go | `WaitForShutdown` 加 `defer signal.Stop(sigCh)` | 0.25h |
| BUG-4 | dlq.go | `Start()` 加 `startOnce sync.Once` 幂等保护 | 0.5h |
| IMPROVE-15 | client.go | 重试等待改用 `time.NewTimer` + Stop | 0.5h |
| IMPROVE-14 | logger.go | 保存 `logFile` 句柄，提供 `CloseLogFile()` | 1h |
| IMPROVE-12 | config.go | `Subscribe` 返回可取消 token | 2h |
| BUG-2 | process.go | 中间件链缓存加内容指纹 `chainSig` | 2h |

### 第三批（下季度）—— 质量 & 架构改进

| # | 文件 | 修改内容 | 预计工时 |
|---|------|---------|---------|
| BUG-6 | retry.go | 退避移位添加上界保护 | 0.25h |
| IMPROVE-16 | dedup.go | 删除死代码 `cleanup()` 方法 | 0.25h |
| IMPROVE-11 | eventbus.go | EventBus 降级改为有界丢弃 + 指标 | 2h |
| IMPROVE-13 | watcher.go | 提供 `NewWatcherWithContext` 构造函数 | 1h |
| BUG-8 | v2.go | RegisterV2 热重载期间依赖引用计数 | 3h |
| IMPROVE-10 | state.go | Engine COW 写操作按需拷贝优化 | 4h |

---

*报告生成时间: 2026-02-22*  
*建议在完成第一批修复后执行 `go test -race ./...` 全量验证*

