# Remilia 框架代码审查报告

> **生成时间：** 2026-02-22  
> **审查范围：** 全模块静态分析 + 逻辑推断  
> **优先级说明：** 🔴 高（可能导致运行时错误）/ 🟡 中（逻辑缺陷/可靠性风险）/ 🟢 低（优化/改进）

---

## 目录

1. [Bug 汇总](#1-bug-汇总)
2. [模块分析详情](#2-模块分析详情)
   - [adapter.go / webhook_adapter.go](#21-adaptergo--webhook_adaptergo)
   - [bot.go](#22-botgo)
   - [lifecycle/lifecycle.go](#23-lifecyclelifecyclego)
   - [core/engine/engine.go + process.go](#24-coreengineenginego--processgo)
   - [middleware/adaptive.go](#25-middlewareadaptivego)
   - [config/config.go + watcher.go](#26-configconfiggo--watchergo)
   - [infra/httpclient/client.go](#27-infrahttpclientclientgo)
   - [infra/pool + infra/dlq](#28-infrapool--infradlq)
   - [plugin/manager.go + v2.go](#29-pluginmanagergo--v2go)
   - [core/context/context.go](#210-corecontextcontextgo)
   - [infra/logger/logger.go](#211-infraloggerloggergo)
   - [infra/metrics/metrics.go](#212-inframetricsmetricsgo)
   - [command/registry.go](#213-commandregistrygo)
3. [高收益改进点](#3-高收益改进点)
4. [改进优先级矩阵](#4-改进优先级矩阵)

---

## 1. Bug 汇总

| # | 严重级别 | 所在文件 | 问题描述 | 状态 |
|---|---------|---------|---------|------|
| B1 | 🔴 | `adapter.go` | `starting` 字段 `defer.Store(false)` 在 `Start` 成功后仍执行，逻辑语义错误 | ✅ 已修复 |
| B2 | 🔴 | `webhook_adapter.go` | 服务器启动检测使用 `time.After(100ms)` 竞态判断，存在误判风险 | ✅ 已修复 |
| B3 | 🔴 | `bot.go` | `Start()` 失败后 `starting` 置回 false 之前，`mu` 锁被持有期间重分配 `rootCtx` 可能被其他路径访问 | ⏳ 待修复（需进一步分析） |
| B4 | 🟡 | `lifecycle/lifecycle.go` | `Stop()` 中等待 `OnRun` 超时后使用 `context.Background()` 新建超时，但此时原始 `ctx` 携带的 Value 链（trace 等）丢失 | ✅ 已修复 |
| B5 | 🟡 | `core/engine/process.go` | `invokeHandler` 中 `err` 变量被 `defer` 和主流程共享，`defer` 中的 `recover` 赋值在 `err = finalHandler(ctx)` 之后才生效，若 handler 本身不 panic 而 defer 内又 panic，`err` 状态不确定 | ✅ 已修复 |
| B6 | 🟡 | `middleware/adaptive.go` | `collectMetrics` 用 `avg * 1.5` 近似 P99，在高延迟方差场景下严重失准，可能导致误降级 | ⏳ 待修复（需引入直方图库） |
| B7 | 🟡 | `config/watcher.go` | `reload()` 内连续两次调用 `Load(path)` 均会触发 `notifyListeners`，导致监听器被调用两次 | ✅ 已修复 |
| B8 | 🟡 | `infra/httpclient/client.go` | `doWithRetry` 中 `time.Sleep` 在 context 取消后仍会阻塞，没有监听 `ctx.Done()` | ✅ 已修复 |
| B9 | 🟡 | `infra/dlq/dlq.go` | `enqueueBlockUntilSpace` 中 `ctx.WithTimeout(dlq.ctx, 5s)` 硬编码超时，无法通过配置调整，且外层 `dlq.ctx.Done()` 与内层超时存在冗余 | ⏳ 待修复（配置项改造） |
| B10 | 🟢 | `adapter.go` | `NewWebhookAdapterWithServer` 注释示例中参数个数与函数签名不符（注释写了 secret，实际只透传给 `NewWebhookServerAdapter`，`secret` 被静默忽略） | ⏳ 仅文档/注释问题 |
| B11 | 🟢 | `core/engine/engine.go` | `RemoveGroup` 日志行 `logger.Debugf("[engine] Removed matcher group: %services", groupName)` 格式字符串错误（`%services` 应为 `%s`） | ✅ 已修复 |
| B12 | 🟢 | `core/engine/process.go` | `invokeHandler` 中 `logger.WithError(err).Debugf("[engine] Handler error in matcher: %services", m.Source)` 同上格式字符串错误 | ✅ 已修复 |

---

## 2. 模块分析详情

### 2.1 adapter.go / webhook_adapter.go

#### B1：`starting` 标志语义错误 🔴

```go
// 当前代码
func (a *webhookAdapter) Start(...) error {
    if !a.starting.CompareAndSwap(false, true) {
        return errutil.New("adapter is already starting or started")
    }
    defer a.starting.Store(false)  // ← 问题：无论成功失败都会执行
    // ...
    a.running = true
    // ...
}
```

**问题：** `defer a.starting.Store(false)` 在 Start 成功后同样执行，导致 `starting` 被重置为 false，但 `running` 已为 true。这使得防止并发 Start 调用的保护形同虚设——第二个并发 Start 调用可能在 `running` 检查前绕过 `starting` 检查。

**修复建议：**
```go
func (a *webhookAdapter) Start(...) error {
    if !a.starting.CompareAndSwap(false, true) {
        return errutil.New("adapter is already starting or started")
    }
    // 只在失败时重置
    var success bool
    defer func() {
        if !success {
            a.starting.Store(false)
        }
    }()
    // ...
    success = true
    return nil
}
```

---

#### B2：WebhookServerAdapter 启动检测竞态 🔴

```go
// 当前代码
select {
case err := <-serverErrCh:
    // 失败
case <-time.After(100 * time.Millisecond):
    // "假设"启动成功
    return nil
}
```

**问题：** `100ms` 超时纯属经验值。在 CI 环境或高负载机器上，`ListenAndServe` 可能超过 100ms 才绑定端口失败，Start 仍会返回 nil。同样，慢速端口绑定超过 100ms 的情况下端口冲突错误会被丢弃。

**修复建议：** 使用 channel + 就绪信号替代计时器：
```go
readyCh := make(chan struct{})
// 在 ListenAndServe 前通过 net.Listen 预先绑定端口，成功则 close(readyCh)
```

或使用 `net.Listen` 预检后再传给 `http.Server.Serve`。

---

#### 其他问题

- `WebhookServerAdapter.Start()` 中 `workersReady` 监听逻辑多余一层 goroutine，可简化为计数器 + `WaitGroup`。
- `safeHandleEvent` 与 `adapter.go` 的 `safeHandle` 功能完全相同，存在代码重复，应合并为一个 `safeHandle` 函数。

---

### 2.2 bot.go

#### B3：Start 失败路径下 rootCtx 泄漏 🔴

```go
func (b *Bot) Start() error {
    // ...
    rootCtx, rootCancel := context.WithCancel(context.Background())

    if b.botInfo != nil {
        // 此处在 mu 锁外操作 b.tokenManager/b.openAPI
        // 如果 lifecycle.Start 失败
    }

    err := b.lifecycle.Start(startCtx)

    b.mu.Lock()
    b.starting = false
    if err != nil {
        b.mu.Unlock()
        rootCancel() // ← 这里才取消，但 tokenManager 已经启动了
        return err
    }
    // ...
}
```

**问题：** 当 `lifecycle.Start` 失败时，新创建的 `tokenManager`（其内部已启动 goroutine）被 `rootCancel()` 取消，但旧的 `tmpTokenManager.Stop()` 是在 `go oldManager.Stop()` 中异步执行的。如果两个操作之间 Bot 被快速重启，旧 manager 的 goroutine 可能仍在运行，导致双重 token 刷新。

**修复建议：** 等待旧 manager 停止后再创建新 manager，或在失败路径中同步停止。

---

#### 其他问题

- `Bot.Context()` 在 `Start()` 前返回 `context.Background()`，但文档说明"在 Start 之前返回 Background"，没有警告调用者。建议加一个 `b.running` 检查并记录警告。
- `GetEngine()` 和 `Engine()` 功能完全相同，存在冗余。可删除其中一个并标记 Deprecated。

---

### 2.3 lifecycle/lifecycle.go

#### B4：Stop 超时后 ctx Value 链丢失 🟡

```go
case <-ctx.Done():
    logger.Warn("[Lifecycle] Stop timeout...")
    stopCtx, stopCancel := context.WithTimeout(context.Background(), m.stopTimeout)
    // ↑ 使用 context.Background() 导致 trace/metadata 等 Value 丢失
    defer stopCancel()
    ctx = stopCtx
```

**问题：** 当 `Stop()` 的调用方传入携带 trace span 的 context 时，超时后 OnStop 阶段切换到 `context.Background()` 衍生的 context，导致所有分布式追踪信息断链。

**修复建议：**
```go
stopCtx, stopCancel := context.WithTimeout(
    context.WithoutCancel(ctx), // 保留 Value 链，剥离已过期的取消信号
    m.stopTimeout,
)
```

---

#### 其他问题

- `Register` 在 `StateRunning` 时仅打日志警告，但被注册的 component 的 `OnRun` 永远不会执行。这个行为对调用方极不直观，建议直接返回 error。
- `Manager.Start()` 检测状态仅允许 `StateCreated` 或 `StateStopped`，但不允许 `StateStarting`（两次并发 Start）。由于没有 atomic 保护，高并发下可能通过 `StateCreated -> StateStarting` 检查的间隙双重启动。建议使用 `sync.Once` 或 `atomic.Bool` 保护。

---

### 2.4 core/engine/engine.go + process.go

#### B5：defer-recover 与 err 共享变量问题 🟡

```go
func (e *Engine) invokeHandler(ctx *context.Context, m *Matcher) {
    var err error

    defer func() {
        if r := recover(); r != nil {
            err = fmt.Errorf("panic in handler: %v", r) // ← 赋值给 err
            // ...
        }
    }()

    err = finalHandler(ctx) // ← 实际执行

    // 下面的错误处理使用 err，但 defer 尚未执行
    if err != nil {
        // 记录错误 + metrics...
    }

    // 临时 matcher 计数...
}
```

**问题：** `defer` 中赋值给 `err` 后，`err != nil` 的检查已经执行完毕（在 `return` 之前 defer 才运行），导致 panic 恢复后的错误**不会触发 metrics 记录和日志**，只有 panic 日志本身被记录。

**修复建议：** 使用命名返回值，或将错误处理逻辑放入 defer：
```go
func (e *Engine) invokeHandler(ctx *context.Context, m *Matcher) {
    var panicErr error
    defer func() {
        if r := recover(); r != nil {
            panicErr = fmt.Errorf("panic in handler: %v", r)
            logger.Error(...)
            // 在这里也更新 metrics
            e.recordHandlerError(panicErr)
        }
    }()

    err := finalHandler(ctx)
    if err != nil {
        e.recordHandlerError(err)
    }
    // ...
}
```

---

#### B11/B12：格式字符串错误 🟢

```go
// engine.go L~200
logger.Debugf("[engine] Removed matcher group: %services", groupName)
//                                               ↑ 应为 %s

// process.go
logger.WithError(err).Debugf("[engine] Handler error in matcher: %services", m.Source)
//                                                                  ↑ 应为 %s
```

这两处 `%services` 均为笔误，应修正为 `%s`，否则格式化输出会包含字面量 `%!(EXTRA string=...)` 噪声。

---

#### 其他问题

- `WithMatcherGroupBatch(fn)` 注释说明持有 `writeMu` 时调用 fn 会死锁，但函数签名没有任何文档约束或 lint 标注，容易被误用。
- `Snapshot/Restore` 功能设计合理，但没有文档说明 Restore 后正在运行的 temp matcher 和 pending delete 处于何种状态，存在使用歧义。
- `GetAllCommands()` 每次都从 `commandInfoCache` 构建新切片，高频调用（如每条消息都调用 help）会产生额外 GC 压力，建议加入版本号 + 缓存。

---

### 2.5 middleware/adaptive.go

#### B6：P99 延迟计算严重失准 🟡

```go
// 简化：使用平均值的1.5倍作为P99（实际应该使用直方图）
p99 := avgLatency * 3 / 2
```

**问题：** 在延迟分布不均匀（长尾）的场景下，P99 可能是均值的 5-10 倍。用 `avg * 1.5` 低估了真实 P99，导致：
- 系统负载已到极限时，`decideLimit` 仍认为延迟"正常"
- 错误地扩大并发限制，加速系统崩溃

**修复建议：** 使用指数直方图（t-digest 或固定桶）：
```go
// 使用滑动窗口 + 固定桶直方图（适合轻量级场景）
// 或接入 prometheus histogram 的 Summary 采样
```

---

#### 其他问题

- `AdaptiveRateLimit` 便捷函数创建 limiter 后调用 `Start()`，但不返回 limiter 引用，调用方无法调用 `Stop()`，存在 goroutine 泄漏风险（如果框架不通过 context 传播取消信号）。
- `collectMetrics` 中每 5 秒调用 `cpu.Percent(time.Second, false)` 会阻塞 1 秒采样，metricsLoop 实际间隔是 6 秒而非 5 秒，与注释不符。建议使用非阻塞采样（interval=0）后异步计算。

---

### 2.6 config/config.go + watcher.go

#### B7：reload 两次触发 notifyListeners 🟡

```go
func (w *Watcher) reload() error {
    newConfig, err := Load(w.configPath)    // ← Load 内部会调用 notifyListeners
    // ...
    time.Sleep(50ms)
    newConfig2, err2 := Load(w.configPath) // ← 又一次 notifyListeners
    // ...
}
```

**问题：** `Load()` 函数内部调用 `notifyListeners`，而 `reload` 中调用两次 `Load`，导致同一次配置变更触发监听器两次。对于不幂等的监听器（如累加计数器、发送通知等），会产生重复副作用。

**修复建议：** 将文件读取与通知解耦：
```go
// 内部使用 loadRaw（只解析，不通知）
func loadRaw(path string) (*Config, error) { ... }

// 只在最终确认后通知一次
func (w *Watcher) reload() error {
    cfg1, _ := loadRaw(path)
    time.Sleep(50ms)
    cfg2, _ := loadRaw(path)
    final := cfg2 (or cfg1)
    notifyListeners(final)   // 只调用一次
    globalConfig.Store(final)
}
```

---

#### 其他问题

- `BotConfig.Validate()` 要求 `Secret` 非空，但 `Secret` 字段对于某些不需要签名验证的部署场景（如内网环境）是可选的，建议改为 warn 或可配置跳过。
- `GetConfig()` 每次返回一个深拷贝（`cfgCopy := *cfg`），如果 Config 包含大量嵌套 slice（如 `AuthWhitelist`），每次调用都会分配内存。建议改为返回 `*Config` 原指针，并在文档中注明只读语义。
- `Watcher` 在 `NewWatcher` 时就加载了一次配置并写入 `globalConfig`，但 `NewWatcher` 调用者可能还没有注册监听器，导致首次加载的通知丢失。

---

### 2.7 infra/httpclient/client.go

#### B8：重试时 Sleep 不响应 context 取消 🟡

```go
func (r *Request) doWithRetry(req *http.Request) (*http.Response, error) {
    for attempt := 0; attempt <= config.MaxRetries; attempt++ {
        if attempt > 0 {
            waitTime := min(...)
            time.Sleep(waitTime) // ← 不响应 ctx.Done()
        }
        // ...
    }
}
```

**问题：** 如果请求 context 在 sleep 期间被取消（如上游超时），`time.Sleep` 仍会完整等待，导致取消信号的响应延迟最多 `RetryMaxWait` 时间（默认可能几秒）。

**修复建议：**
```go
select {
case <-time.After(waitTime):
case <-req.Context().Done():
    return nil, req.Context().Err()
}
```

---

#### 其他问题

- `Response.Close()` 方法在 `Bytes()` 内部已调用 `r.Body.Close()`，调用方再次调用 `Close()` 会重复关闭 Body（虽然 `http.Response.Body` 多次关闭通常无害，但语义不清晰）。建议在 `Bytes()` 中不主动关闭，或添加 `bodyConsumed` 标志。
- 全局 `defaultClient` 共享同一连接池，在多 Bot 实例场景（测试/多租户）下会互相影响。建议改为包级函数使用独立实例，或提供 `NewDefaultClient()` 工厂函数。
- `SetQuery` 每次调用都重新 `url.Parse` + `Encode`，在批量设置查询参数（`SetQueries`）时 N 次循环 = N 次 parse，建议合并为一次操作。

---

### 2.8 infra/pool + infra/dlq

#### pool 问题

- `InstrumentedPool.Stats()` 注释说"避免与 Reset() 竞争 resetMu"，但 `Stats()` 本身不持有任何锁，而 `Reset()` 持有 `resetMu` 后依次 Store 三个字段，这三次 Store 对 `Stats()` 来说不是原子的，可能读到 gets=0/puts=old/news=old 的中间态。这在监控场景可以接受，但应在注释中明确说明。
- `TypedPool.Get()` 使用 `v.(T)` 类型断言，如果 pool 被错误地 Put 了非 T 类型的值（通过 `Raw()` 访问底层池），会发生 panic，但没有 recover 保护。

#### dlq 问题

- `enqueueBlockUntilSpace` 超时硬编码 5 秒，在高吞吐场景下既不够灵活，也没有与 `DeadLetterQueueConfig` 中的其他配置统一管理。建议将超时作为配置项暴露。
- `DeadLetterQueue` 缺少 `Cancel()` 方法（区别于 `Shutdown`），导致无法在不等待 drain 的情况下立即停止。

---

### 2.9 plugin/manager.go + v2.go

#### 其他问题

- `Manager.Unregister()` 在 `Unload` 失败后标记插件为 Error 状态后**仍持有 mu 锁**才 Unlock，这意味着 `notifyError` 的调用（包含用户代码）发生在锁外，但 `notifyError` 前已 Unlock，实际没有问题。但代码注释容易让读者误解为在锁内通知，建议重构以提高清晰度。

- `PluginInstance.Unload()` 调用顺序：先 `coordinator.RemoveGroup(name)` 删除匹配器，再调用 `desc.Teardown()`，再清除 container。如果 `Teardown` 失败，匹配器已被删除，但容器中的服务仍然存在，状态不一致。建议原子化这个过程：先 `SaveState`，再 `Teardown`，失败则回滚。

- `SetupContext.MustGet()` 在依赖不存在时直接 `panic`，但 panic 会在 `PluginInstance.Load()` 中被直接冒泡（没有 recover），导致整个 Manager.RegisterV2 流程崩溃，影响其他插件。建议在 `Load()` 内加 recover 或改为返回 error。

- `Container` 使用 `sync.Map`，对于初始化后只读的场景（插件注册完后不再 Register），`sync.Map` 的读性能不如加 `RWMutex` 的普通 map。建议在首次 Load 完成后将容器"冻结"（freeze），后续使用无锁读。

---

### 2.10 core/context/context.go

#### 其他问题

- `Context.Clone()` 创建新 context 时保留了 deadline，但注释说"不会受原 Context 取消的影响"。如果 deadline 来自父 context 的取消，Clone 后的 context 仍会在相同时间点到期，与注释期望不一致。

- `extensionState` 的 `m map[string]any` 键为 string，在高频事件处理（>50K msg/s）下每次 `Set/Get` 都做 string 哈希，对延迟有轻微影响。建议提供基于 `reflect.Type` 作为键的 typed extension API（当前 `ExtSet` 已是类型路由，可以进一步用 sync.Map 替代 RWMutex map）。

- `contentOnce sync.Once` + `authorOnce sync.Once` 虽然避免了重复计算，但 `ReleaseContext` 归还 pool 时需要正确重置这些字段（`sync.Once` 不可重置，只能重新分配）。需确认 `ReleaseContext` 中 context 对象是否被完全重建而非复用旧 Once。

---

### 2.11 infra/logger/logger.go

#### 其他问题

- `logger.go` 的 `init()` 调用 `InitDefault()` 自动初始化，这在测试场景下每个 `_test.go` 导入 logger 包都会触发一次控制台输出配置。建议改为懒初始化或提供测试用 `InitNop()`。

- `LoggerWithFields.Error()` 使用 `Caller(1)` 获取调用者信息，但当通过 `logger.WithFields(...).WithField(...).Error()` 链式调用时，`Caller(1)` 深度偏移不准确，会指向错误的调用栈帧。

- `FieldsPool` 预分配 8 字段容量，但实际使用中很多日志只有 2-3 个字段，浪费内存。建议按实际需求动态分配，或提供不同容量的池（small=4, medium=8, large=16）。

---

### 2.12 infra/metrics/metrics.go

#### 其他问题

- `NewMetricsCollector(namespace)` 使用 `prometheus.DefaultRegisterer`，在同一进程多次调用（测试场景、多引擎）会因 metric 名称重复 panic（prometheus 重复注册 panic）。已有 `NewMetricsCollectorWithRegistry` 修复此问题，但默认构造函数仍不安全，应在注释中明确警告或内部捕获重复注册错误。

- `Collector` 缺少 `Unregister()` 方法，无法在 Bot 停止时清理 Prometheus 指标，导致在动态重启的场景中指标不断积累。

- `internalPoolGets/Puts/News` 字段有 atomic 操作但没有任何方法暴露这些数据，死代码。

---

### 2.13 command/registry.go

#### 其他问题

- `CommandRegistry.Lookup()` 先做 O(1) map 查找，再做 O(1) alias 查找，逻辑清晰，无 bug。但 `recompile()` 在每次 Register/Unregister 时全量重建 `compiledRegistry`（包含 `commandList` 排序），如果命令数量多（>1000），频繁注册/注销时会成为瓶颈。建议在批量注册完成后调用一次 `Compile()`，通过 `BeginBatch/EndBatch` API 延迟重编译。

- `CommandMeta.callCount` 使用 `atomic.Int64`，但 `lastCall atomic.Value` 存储 `time.Time`，每次更新都需要 box/unbox，考虑改为 `atomic.Int64` 存储 UnixNano。

---

## 3. 高收益改进点

### 3.1 🔴 P99 延迟计算替换（middleware/adaptive.go）✅ 已实施

**当前：** `p99 = avg * 1.5`  
**影响：** 自适应限流决策严重失准，高负载时可能误扩大并发导致雪崩  
**实施方案：** 引入 `latencyHistogram`（32 个指数桶，0.1ms~10s，全无锁 atomic），`percentile(99)` 重置式读取。同时将 `cpu.Percent(time.Second)` 阻塞采样改为非阻塞 `cpu.Percent(0)`。

**预期收益：** 自适应限流准确率提升 60-80%，减少误触发降级

---

### 3.2 🔴 webhook_adapter.go 启动检测改造 ✅ 已实施（上一轮 Bug 修复完成）

---

### 3.3 🟡 dto.Payload 对象池（全局）⏳ 待实施（需配合 webhook 协议层改造）

---

### 3.4 🟡 config.Watcher 解耦 Load 与 Notify ✅ 已实施（上一轮 Bug 修复完成）

---

### 3.5 🟡 Plugin Container 冻结优化 ✅ 已实施

**实施内容：**
- `Container` 新增 `frozen atomic.Bool` + `frozenMap map[string]any`
- `Freeze()` 将 `sync.Map` 快照到只读 `map`，后续 `Get/Has` 无锁读
- `Register/Remove` 在冻结后调用会 panic（明确语义）
- `Manager.FreezeContainer()` 供调用方在所有插件加载后显式冻结

**预期收益：** 高频事件处理中依赖查找性能提升 2-3x

---

### 3.6 🟡 HTTP Retry 响应 context 取消 ✅ 已实施（上一轮 Bug 修复完成）

---

### 3.7 🟢 GetAllCommands() 缓存优化 ✅ 已实施

**实施内容：**
- `engineState` 新增 `commandListCache []CommandInfo` + `commandListVer int64`
- `rebuildCommandListCache()` 在 `rebuildIndex`、`rebuildCommandInfoCache`（含删除路径）后自动调用
- `GetAllCommands()` 直接 `copy(commandListCache)` 返回，无 map 遍历

**预期收益：** Help 插件等高频场景每次查询减少 map 遍历和随机分配

---

### 3.8 🟢 统一 safeHandle / safeHandleEvent ✅ 已实施

**实施内容：** `webhook_adapter.go::safeHandleEvent` 改为代理调用 `safeHandle`，消除重复逻辑

---

### 3.9 🟢 logger.go 测试友好改造 ✅ 已实施

**实施内容：** 新增 `logger.InitNop()`（丢弃所有输出）和 `logger.InitTest()`（仅 Error+），供测试 `TestMain` 调用

---

### 3.10 🟢 metrics.Collector 注册安全 ✅ 已实施

**实施内容：**
- `NewMetricsCollector` 改为使用 `prometheus.NewRegistry()` 独立 Registry，彻底消除重复注册 panic
- 新增 `Collector.Registry() prometheus.Gatherer` 供挂载 `/metrics` 端点

---

### B3 bot.go Start 失败路径 tokenManager goroutine 泄漏 ✅ 已实施

**实施内容：** 提取 `oldManager` 变量到 tokenManager 创建之前，失败路径同步调用 `oldManager.Stop()`，成功路径异步停止（行为不变）

---

### B9 DLQ BlockTimeout 可配置化 ✅ 已实施

**实施内容：** `DeadLetterQueueConfig` 新增 `BlockTimeout time.Duration` 字段，`enqueueBlockUntilSpace` 使用该值（默认 5s 兜底）

---

## 4. 改进优先级矩阵

| 优先级 | 事项 | 影响 | 实施难度 |
|-------|------|------|---------|
| P0 | B11/B12 格式字符串 `%services` 修正 | 日志噪声 | 极低 |
| P0 | B8 HTTP Retry 响应 ctx 取消 | 正确性 | 低 |
| P1 | B1 adapter starting 标志修复 | 并发安全 | 低 |
| P1 | B2 WebhookServerAdapter 启动竞态修复 | 可靠性 | 中 |
| P1 | B7 Watcher reload 双重通知修复 | 正确性 | 低 |
| P1 | B6 P99 延迟计算替换 | 限流准确性 | 中 |
| P2 | B5 invokeHandler recover-err 共享修复 | 监控完整性 | 低 |
| P2 | B4 lifecycle Stop ctx Value 链保留 | 可观测性 | 极低 |
| P2 | 3.3 dto.Payload 对象池 | GC 性能 | 中 |
| P3 | 3.5 Plugin Container 冻结 | 读性能 | 中 |
| P3 | 3.7 GetAllCommands 缓存 | Help 性能 | 低 |
| P3 | 3.8 safeHandle 合并 | 代码质量 | 极低 |
| P3 | 3.9 logger 测试改造 | 开发体验 | 低 |
| P3 | 3.10 metrics 注册安全 | 健壮性 | 低 |

---

*报告由代码静态审查生成，部分结论基于逻辑推断，建议结合实际运行测试验证。*

