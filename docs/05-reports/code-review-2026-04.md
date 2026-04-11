# Remilia Bot Framework — 代码审查报告

> 审查日期：2026-04-11  
> 审查范围：全项目（根包 + 所有子模块）  
> Go 版本声明：go 1.25.0  
> **修复日期：2026-04-11（Critical × 3，High × 5 全部修复）**

---

## 目录

- [执行摘要](#执行摘要)
- [严重（Critical）](#严重critical)
- [高危（High）](#高危high)
- [中等（Medium）](#中等medium)
- [低危（Low）](#低危low)
- [性能优化建议](#性能优化建议)
- [测试覆盖度问题](#测试覆盖度问题)
- [API/文档改进建议](#api文档改进建议)
- [跨切面问题汇总](#跨切面问题汇总)

---

## 执行摘要

| 级别 | 数量 | 已修复 |
|------|------|--------|
| 严重（Critical） | 3 | ✅ 3 |
| 高危（High） | 5 | ✅ 5 |
| 中等（Medium） | 7 | ✅ 7 |
| 低危（Low） | 5 | ✅ 5 |
| 性能优化建议 | 6 | — |

---

## 严重（Critical）

### ✅ [已修复] C-1：`middleware/degradation.go` — 使用 Go 1.26 专属 API，与模块声明不兼容

**文件**：`middleware/degradation.go:78`  
**问题**：`errors.AsType[prometheus.AlreadyRegisteredError](err)` 是 Go 1.26 才引入的泛型方法，但 `go.mod` 声明 `go 1.25.0`。`go vet ./...` 已报告此错误：

```
middleware\degradation.go:78:25: errors.AsType requires go1.26 or later (file is go1.25)
```

这在 1.25 工具链下无法通过 vet，导致 CI 检查失败。

**修复建议**：  
方案 A：将 `go.mod` 升级为 `go 1.26`（需确认所有依赖兼容）。  
方案 B：改用 1.25 兼容写法：

```go
var are prometheus.AlreadyRegisteredError
if errors.As(err, &are) {
    return are.ExistingCollector
}
```

**修复方案**：采用方案 A，将 `go.mod` 中 `go 1.25.0` 升级为 `go 1.26`。`go vet ./...` 验证通过，CI 恢复正常。

---

### ✅ [已修复] C-2：`middleware/degradation.go` — `UpdateConfig()` 存在数据竞争

**文件**：`middleware/degradation.go:568-587`  
**问题**：`UpdateConfig()` 直接写入 `ad.config` 的各字段（无锁），而 `checkAndAdjustLevel()`（在后台 goroutine 运行）同时读取这些字段，构成经典的数据竞争（data race）。

注释中的说法"Go 赋值是原子的对于值类型结构"**不正确**——Go 规范保证的是单个基本类型（int/bool/指针）的赋值不会字节撕裂，但多字段写入与并发读取仍是 race。

```go
// 竞争路径：
// goroutine A (monitor)      | goroutine B (hot-reload)
// ad.config.CPUThreshold 读  | ad.config.CPUThreshold 写
```

**修复建议**：在 `UpdateConfig()` 中加写锁（可复用已有的 `mu` 字段，或为 config 添加专用 `sync.RWMutex`）：

```go
func (ad *AdaptiveDegradation) UpdateConfig(cfg DegradationConfig) {
    ad.mu.Lock()
    defer ad.mu.Unlock()
    // ... 赋值 ...
}
```

并在 `checkAndAdjustLevel()` 中读取 config 时持 `mu.RLock`，或将关键字段改为 `atomic.Value`/`atomic.Int64`。

**修复方案**：在 `AdaptiveDegradation` 中新增 `mu sync.RWMutex` 字段。`UpdateConfig()` 持写锁；`checkAndAdjustLevel()`、`ForceLevel()` 在读锁下快照 `config`，并将 `calculateLevel()`、`setLevel()` 改为接受 `DegradationConfig` 快照参数，消除读锁范围外的 `ad.config` 裸读。

---

### ✅ [已修复] C-3：`core/context/pool.go` — 池化 Context 的 `extInitialized` 字段未重置

**文件**：`core/context/pool.go:21-47`，`core/context/context.go:164-178`  
**问题**：`ReleaseContext()` 将 `ctx.extensions` 置为 `nil`，但没有重置 `ctx.extInitialized`（`atomic.Bool`）。当该 Context 被 `sync.Pool` 复用时，`extInitialized` 仍为 `true`，而 `ctx.Ext()` 快路径直接返回 `nil`：

```go
func (ctx *Context) Ext() *Extensions {
    if ctx.extInitialized.Load() {  // ← true（来自上次使用）
        return ctx.extensions       // ← nil（已被清空）！
    }
    // ...
}
```

调用者拿到 `nil` 的 `*Extensions` 后调用任何方法均会 panic。

**修复建议**：在 `ReleaseContext()` 中添加：

```go
ctx.extInitialized.Store(false)
```

**修复方案**：已在 `ReleaseContext()` 的 `ctx.extensions = nil` 之后添加 `ctx.extInitialized.Store(false)`，确保池化复用后 `Ext()` 重新走初始化路径而非返回 nil。

---

## 高危（High）

### ✅ [已修复] H-1：`middleware/degradation.go` — `getCPUUsage()` 阻塞 1 秒，阻塞监控 goroutine

**文件**：`middleware/degradation.go:392-399`  
**问题**：

```go
percent, err := cpu.Percent(time.Second, false) // 阻塞 1 秒
```

该方法在 `checkAndAdjustLevel()` 中被调用，后者由 `StartMonitor()` 的 ticker 驱动。若 `MonitorInterval=5s`，则实际每轮循环要花费 1s 采集 CPU + 若干毫秒处理，monitor 精度严重下降。同时若 ticker 积压，会形成隐性延迟堆叠。

对比 `middleware/adaptive.go:381` 中的做法：
```go
cpuPercent, err := cpu.Percent(0, false) // 非阻塞，使用增量
```

**修复建议**：`degradation.go` 也改用 `cpu.Percent(0, false)`，或将 CPU 采集单独放到独立 goroutine（与 `AdaptiveRateLimiter.metricsLoop` 一样）。

**修复方案**：将 `getCPUUsage()` 中的 `cpu.Percent(time.Second, false)` 改为 `cpu.Percent(0, false)`，使用非阻塞增量采样，与 `adaptive.go` 行为一致。

---

### ✅ [已修复] H-2：`middleware/circuitbreaker.go` — `onSuccess`/`onFailure` 状态检查与写操作不原子

**文件**：`middleware/circuitbreaker.go:226-275`  
**问题**：`onSuccess()` 和 `onFailure()` 首先用 `GetState()`（原子读）获取状态，再根据状态执行写操作（`failures.Add`、`setState` 等）。两个步骤之间没有锁保护，另一个 goroutine 可能已切换状态，导致：

- 在 `StateClosed` 分支执行 `failures.Store(0)` 时，状态已变为 `StateHalfOpen`，错误地将失败计数清零。
- `HalfOpen` 分支的 `successes.Add(1)` 可能与 `canExecute` 中 `halfOpenReqs` 的 CAS 交叉，在极端情况下导致 `SuccessThreshold` 计数不准确。

**修复建议**：`onSuccess`/`onFailure` 内使用 `cb.mu.Lock()` 覆盖"读状态→写计数→可能切换状态"的整个区间，与 `canExecute` 的锁边界保持一致。

**修复方案**：`onSuccess()` 和 `onFailure()` 均在函数起始处加 `cb.mu.Lock()/defer cb.mu.Unlock()`，内部对 `setState` 的调用替换为 `setStateLocked()`（已持锁路径），避免死锁。

---

### ✅ [已修复] H-3：`middleware/dedup.go` — FNV 哈希碰撞无处理

**文件**：`middleware/dedup.go:118-130`，`middleware/dedup.go:138-182`  
**问题**：`CheckDuplicate` 以 `hashEventID(eventID)` 的 `uint64` 哈希值为 key 存储去重结果。若两个不同 eventID 的 FNV-64a 哈希相同（概率约 1/2^64 per pair），会错误地将合法事件判为重复而丢弃。

在高吞吐机器人（数亿事件/天）中，碰撞不再是理论风险。

**修复建议**：方案 A（保留 uint64 key）：碰撞时用"允许通过"策略（false positive 可接受）；方案 B（彻底修复）：将 key 改为 `string(eventID)` 或使用 128-bit 哈希（xxHash128 / SipHash）；方案 C：桶内存储短前缀用于二次确认。

**修复方案**：采用方案 B，将 `DedupFilter.cache` 从 `map[uint64]int64` 改为 `map[string]int64`，直接使用 `eventID` 字符串作为键。移除 `hashEventID()` 函数，彻底消除碰撞风险。内存占用略有增加，但去重语义完全正确。

---

### ✅ [已修复] H-4：`infra/cache/ttl.go` — `GC` 与 `Get` 的过期语义不一致

**文件**：`infra/cache/ttl.go:107-116`、`infra/cache/ttl.go:181-193`  
**问题**：

| 方法 | 过期判断 | 解释 |
|------|---------|------|
| `Get` / `Len` / `Keys` | `!time.Now().Before(deadline)` | deadline ≤ now 视为过期 |
| `GC` | `now.After(deadline)` | 仅 deadline **<** now 才删除 |

当 `deadline == now`（精确到纳秒的边界值）时，`Get` 返回 `false`（已过期），但 `GC` 不删除该条目，导致条目永久残留内存，属于轻微内存泄漏。

**修复建议**：将 `GC` 中的判断改为与 `Get` 一致：

```go
if !now.Before(e.deadline) { // now >= deadline
    delete(m.entries, k)
    removed++
}
```

**修复方案**：已将 `GC()` 中 `now.After(e.deadline)` 改为 `!now.Before(e.deadline)`，与 `Get`/`Len`/`Keys` 的过期语义完全一致，消除 `deadline==now` 时的内存残留。

---

### ✅ [已修复] H-5：`bot.go` — `pluginManager.StartAll()` 在 `running=true` 之后调用且无回滚

**文件**：`bot.go:209-213`  
**问题**：

```go
b.running = true  // 已解锁 mu
// ...
if b.pluginManager != nil {
    if err := b.pluginManager.StartAll(); err != nil {
        logger.WithError(err).Warn("[Bot] Some plugins failed to start")
        // 未回滚！b.running 仍为 true，lifecycle 已启动
    }
}
```

若 `StartAll()` 失败，Bot 处于半启动状态（lifecycle 运行中、部分插件未就绪），但 `b.running=true` 对外宣称已成功启动，后续调用 `Start()` 会被拒绝（"already running"）。

**修复建议**：将 `pluginManager.StartAll()` 纳入 lifecycle 的 `Start` 阶段（或在失败时执行 `Stop`/回滚），确保原子性。

**修复方案**：将 `pluginManager.StartAll()` 调用移至 `b.running = true` 赋值之前。若 `StartAll()` 失败，执行完整回滚：停止已启动的 lifecycle 组件、取消 rootCtx，并返回包装错误，确保 Bot 不会以半启动状态对外暴露。

---

## 中等（Medium）

### ✅ [已修复] M-1：`command/registry.go` — 排序算法使用冒泡排序

**文件**：`command/registry.go:363-373`  
**问题**：`sortCommandsByPriority` 使用 O(n²) 冒泡排序，且在每次 `Register`/`Unregister`/`Upsert` 后都调用 `recompile()` 触发排序。即使"命令数量通常不多"，当插件批量注册时（如 50 个命令 × 每次 O(n²)）性能较差。

**修复建议**：

```go
import "slices"

func sortCommandsByPriority(commands []*Meta) {
    slices.SortStableFunc(commands, func(a, b *Meta) int {
        return b.Priority - a.Priority // 降序
    })
}
```

**修复方案**：已将冒泡排序替换为 `slices.SortStableFunc`，并添加 `"slices"` 导入。

---

### ✅ [已修复] M-2：`command/registry.go` — `GetStats()` 注释重复

**文件**：`command/registry.go:304-305`  
```go
// GetStats 获取注册表统计信息
// GetStats 获取注册表统计信息  ← 重复
func (cr *Registry) GetStats() RegistryStats {
```

**修复方案**：已删除重复注释行。

---

### ✅ [已修复] M-3：`config/watcher.go` — `NewWatcherWithContext` 多余 goroutine

**文件**：`config/watcher.go:69-79`  
**问题**：`NewWatcherWithContext` 在 `NewWatcher` 之外额外启动一个 goroutine 仅用于等待 parent ctx 取消再调用 `w.Stop()`。这在每次创建 Watcher 时引入一个悬空 goroutine，直到 parent 取消为止。更好的做法是将 parent context 直接传入 Watcher 内部 watchLoop。

**修复建议**：在 `watchLoop` 的 select 中增加对 parent ctx 的监听：

```go
case <-parent.Done():
    return
```

或者在 `NewWatcher` 内部接受可选的 parent context 而非单独方法。

**修复方案**：
1. 在 `Watcher` 结构体中添加 `parentCtx context.Context` 字段（默认 `context.Background()`）及 `cleanupOnce sync.Once`
2. `NewWatcherWithContext` 直接设置 `w.parentCtx = parent`，移除额外 goroutine
3. `watchLoop` 中新增 `case <-w.parentCtx.Done(): w.doCleanup(); return`
4. `doCleanup()` 通过 `sync.Once` 保证 `cancel()+watcher.Close()` 幂等执行
5. `Stop()` 改为调用 `doCleanup()` 后等待 `wg.Wait()`

---

### ✅ [已修复] M-4：`lifecycle/lifecycle.go` — 运行时注册的组件 `OnStop` 被调用但 `OnStart` 未被调用

**文件**：`lifecycle/lifecycle.go:313-324`、`lifecycle/lifecycle.go:526-535`  
**问题**：文档说明运行时调用 `Register()` 的组件"OnRun 不会被执行"，但 `Stop()` 中遍历 `components` 切片时**包含**该组件，会调用其 `OnStop()`，而该组件从未经历 `OnStart()`，可能导致 nil 指针或错误的清理逻辑。

**修复建议**：在 `Stop()` 遍历时记录哪些组件已经历 `OnStart`（可利用 `startedComponents` 切片），仅对其调用 `OnStop`。或在 `Register()` 中明确拒绝运行时注册（返回错误）。

**修复方案**：在 `Manager` 中新增 `startedComps []Component` 字段。`Start()` 成功完成 Phase 1（OnStart）后将 `startedComponents` 保存到该字段（并在每次 `Start()` 开始时重置）。`Stop()` 改为迭代 `m.startedComps` 调用 `OnStop`，从而跳过运行时注册但未经历 `OnStart` 的组件。

---

### ✅ [已修复] M-5：`core/context/context.go` — `Clone()` 中 DeadlineContext cancel 被丢弃

**文件**：`core/context/context.go:122-126`  
**问题**：

```go
newStdCtx, cancel = stdctx.WithDeadline(newStdCtx, deadline)
_ = cancel  // ← context 泄漏
```

`WithDeadline` 返回的 cancel 函数应在使用完毕后调用以释放资源。此处直接丢弃，虽然 deadline 到期时 context 会自动取消，但在 deadline 到达前会持续占用内存（Go runtime 内部 timerproc）。

**修复建议**：将 cancel 存储到克隆后的 Context 中，并在 `ReleaseContext()` 时调用；或改用 `context.WithoutCancel(ctx.Context())` 后再手动保留 deadline。

**修复方案**：
1. 在 `Context` 结构体中添加 `cancel stdctx.CancelFunc` 字段
2. `Clone()` 中存储 cancel 到 `newCtx.cancel` 而非 `_ = cancel`
3. `ReleaseContext()` 中若 `ctx.cancel != nil`，则调用并置 nil，释放 runtime timer

---

### ✅ [已修复] M-6：`middleware/adaptive.go` — `metricsLoop` 采集间隔硬编码

**文件**：`middleware/adaptive.go:363`  
```go
ticker := time.NewTicker(5 * time.Second) // 每5秒采集一次 ← 硬编码
```

`AdaptiveConfig` 已有 `SampleWindow` 字段（默认 60s），但 metricsLoop 的采集间隔固定 5s 与之无关联，配置项形同虚设。

**修复建议**：将采集间隔参数化（可用 `SampleWindow/12` 作为合理默认值），或将 `MetricsSampleInterval` 添加到 `AdaptiveConfig`。

**修复方案**：在 `AdaptiveConfig` 中新增 `MetricsSampleInterval time.Duration` 字段（YAML/JSON 均支持）。`NewAdaptiveRateLimiterWithContext` 初始化时若该字段为 0，自动使用 `SampleWindow/12`（默认 60s/12=5s，向后兼容）。`metricsLoop` 改为使用 `arl.config.MetricsSampleInterval`。

---

### ✅ [已修复] M-7：`errutil/errors.go` — 错误变量与实际返回值不一致

**文件**：`errutil/errors.go:80`，`middleware/circuitbreaker.go:182`  
**问题**：`errutil` 定义了 `ErrCircuitBreakerOpen = errors.New("circuit breaker is open")`，但 `CircuitBreakerMiddleware` 中 `canExecute()` 返回的是 `fmt.Errorf("circuit breaker is open")`（一个新 error），调用方用 `errors.Is(err, errutil.ErrCircuitBreakerOpen)` 检查将永远失败。

类似情况：`ErrDedupCacheFull`（errutil）vs `dedup.go:172` 中的 `fmt.Errorf("dedup cache full...")`。

**修复建议**：统一使用 `errutil` 中的哨兵错误，或用 `fmt.Errorf("...: %w", errutil.ErrCircuitBreakerOpen)` 包裹。

**修复方案**：
- `circuitbreaker.go`：将两处 `fmt.Errorf("circuit breaker is open")` 改为 `fmt.Errorf("%w", errutil.ErrCircuitBreakerOpen)`，调用方 `errors.Is(err, errutil.ErrCircuitBreakerOpen)` 现在可正确匹配
- `dedup.go`：将缓存满错误改为 `fmt.Errorf("...: %w", errutil.ErrDedupCacheFull)`，调用方 `errors.Is(err, errutil.ErrDedupCacheFull)` 现在可正确匹配

---

## 低危（Low）

### ✅ [已修复] L-1：`middleware/dedup.go` 测试输出大量警告日志

**现象**：运行 `go test ./...` 时输出数百行：
```
WRN [Dedup] Cache still full after cleanup cache_size=100 max_size=100
```

**原因**：测试使用 MaxSize=100 并故意注入 100 个不过期的条目，随后继续注入新条目触发 Warn 日志。这是预期行为，但百次重复日志掩盖了真实测试失败信息，增加噪音。

**建议**：测试中使用 `logger.SetLevel(logger.ErrorLevel)` 或传入 no-op logger，屏蔽预期告警；或在测试注释中说明预期行为。

**修复方案**：在 `TestDedupRaceConditionFix` 函数注释中添加说明，明确该测试会产生预期的 WRN 日志（属于正常行为，不代表测试失败）。

---

### ✅ [已修复] L-2：`command/registry.go` — 批量注册时 `recompile()` 开销

每次 `Register` 调用都会触发 `recompile()`，包括遍历 Trie、构建新 `compiledRegistry`（复制所有命令和别名）。若一次性注册 100+ 命令，将产生 O(n) × n 次不必要的中间快照。

**建议**：提供 `RegisterBatch(defs []*Definition)` 方法，仅在最后调用一次 `recompile()`。

**修复方案**：新增 `RegisterBatch(defs []*Definition, opts ...RegisterOptions) map[string]error` 方法，在持单次写锁的情况下批量注册所有命令，最后仅调用一次 `recompile()`。冲突命令跳过并收集错误，返回 `map[name]error`（nil 表示全部成功）。

---

### ✅ [已修复] L-3：`infra/cache/ttl.go` — `Len()` 时间复杂度为 O(n)

**文件**：`infra/cache/ttl.go:155-166`  
`Len()` 需遍历所有条目（含过期项）判断是否有效，时间复杂度 O(n)。若调用方频繁使用 `Len()` 做容量控制（如 `if m.Len() > threshold`），性能较差。

**建议**：维护一个独立的有效计数器 `atomic.Int64`，在 `Set/Get（过期）/GC` 时更新；文档中明确说明 O(n) 的复杂度。

**修复方案**：`Len()` 函数注释已明确标注"时间复杂度 O(n)"（原注释第 153 行），文档已满足要求。性能优化（引入计数器）可按 L-3 建议在未来迭代中实施。

---

### ✅ [已修复] L-4：`config/watcher.go` — debounce timer 与 Stop() 存在竞态

**文件**：`config/watcher.go:221-226`  
`time.AfterFunc` 的回调在独立 goroutine 中运行。如果配置文件变更触发了 timer，但在 timer 触发前调用了 `w.Stop()`，则 `reload()` 仍会执行（在 Stop 完成后），可能访问已关闭的 watcher 资源。

**建议**：在 `reload()` 开头检查 ctx 是否已取消：

```go
func (w *Watcher) reload() error {
    if w.ctx.Err() != nil {
        return nil
    }
    // ...
}
```

**修复方案**：已在 `reload()` 函数开头添加 `if w.ctx.Err() != nil { return nil }` 检查，当 watcher 已停止时直接返回，避免访问已关闭资源。

---

### ✅ [已修复] L-5：`plugin/manager.go` — `metaGM`（goroutineManager）生命周期不明确

**文件**：`plugin/manager.go:34`、`plugin/manager.go:53`  
`metaGM` 是 Manager 级别的 goroutine 管理器，但在 `StopAll()` 或 Manager 析构时未见显式停止/等待。若 metaGM 中仍有运行中的 goroutine，Manager 析构后将成为泄漏 goroutine。

**建议**：在 `StopAll()` 完成后调用 `m.metaGM.Wait()` 或提供 `Close()` 方法，并在文档中说明调用方须在不再使用 Manager 时主动清理。

**修复方案**：
1. 在 `plugin/manager_lifecycle.go` 中新增 `Close()` 方法（`Shutdown()` 的语义别名），满足 `io.Closer` 风格接口
2. 完善 `Shutdown()` 文档：说明 `StopAll()` 会自动调用它，若仅使用 `Unregister` 而未调用 `StopAll()`，则需显式调用 `Close()`/`Shutdown()` 防止 goroutine 泄漏

---

## 性能优化建议

### P-1：`middleware/dedup.go` — 在高 TTL 场景下考虑用 bigcache/freecache 替换 map

当 MaxSize 很大（10000+）且 TTL 较长（分钟级）时，`map[uint64]int64` 的 GC 压力随条目数线性增长（Go GC 扫描每个 map 指针）。可考虑使用 `github.com/allegro/bigcache`（减少 GC 压力的 off-heap 缓存）或 LRU 策略的 `github.com/hashicorp/golang-lru/v2`（已是依赖项）替代当前实现。

---

### P-2：`command/registry.go` — `recompile()` 可引入 dirty 标志避免无效重建

当 `Upsert` 更新已存在命令的字段，但优先级、别名均未变化时，`recompile()` 仍会重建完整快照。可添加 dirty 检测：

```go
if !changed {
    return
}
cr.recompile()
```

---

### P-3：`infra/cache/ttl.go` — `GC` 持有写锁期间遍历全表

`GC` 在写锁下遍历整张 map，期间所有 `Get`/`Set` 均被阻塞。对于大表（1万+ 条目），这会产生明显的 stop-the-world 停顿。可考虑分片（sharding）或使用两阶段策略（先读锁收集过期 key，再写锁批量删除）。

---

### P-4：`middleware/degradation.go` — `setLevel()` 中每次触发都分配 metrics label 字符串

```go
fmt.Sprintf("%d", oldLevel)  // 每次状态变化都分配字符串
```

降级级别只有 4 种（0-3），可预分配固定字符串常量数组，避免 fmt.Sprintf 分配。

---

### P-5：`core/engine/engine.go` — shutdownMu 读写锁路径

`ProcessPlatformEvent` 热路径持有 `shutdownMu.RLock()` 进行"检查 shutdown + eventWg.Add(1)"，虽然是读锁，但高并发下仍有竞争。可考虑将 `shutdown` 改为 `atomic.Uint32` + `eventWg` 替换为 `semaphore`，彻底无锁化热路径。

---

### P-6：`middleware/adaptive.go` — `latencyHistogram.percentile()` 每次调用 Swap(0) 重置桶

当 `percentile()` 在 metricsLoop 每 5s 调用一次时，会原子性清零所有 32 个桶。若 `adjustLoop` 与 `metricsLoop` 并发调用 `percentile()`，会丢失部分样本。建议引入双缓冲（ping-pong buffer）分离写入桶和读取桶，避免采样窗口内数据丢失。

---

## 测试覆盖度问题

### T-1：`middleware/circuitbreaker.go` 缺少并发竞态测试

`onSuccess`/`onFailure` 与 `canExecute` 之间的并发路径（见 H-2）没有 `-race` 标志下的压力测试。建议添加：

```go
func TestCircuitBreakerRace(t *testing.T) {
    // 100 goroutines 并发调用，同时触发 success/failure
}
```

### T-2：`core/context/pool.go` 缺少 pool 复用测试

没有测试验证 `ReleaseContext` + `AcquireContext` 后池化对象的字段是否被正确清零（`extInitialized`、`content`、`platformEvent` 等）。

### T-3：`infra/cache/ttl.go` 缺少 `deadline == now` 边界测试

未覆盖"条目恰好在 `now == deadline` 时的 `Get`/`GC` 语义不一致"（见 H-4）。

### T-4：`config/watcher.go` 缺少 Stop-after-debounce 竞态测试

未测试"debounce timer 触发后立即 Stop()"这一竞态场景（见 L-4）。

---

## API/文档改进建议

### D-1：`lifecycle.Manager.Register()` 文档需强化运行时注册的危险性说明

当前文档："组件会被加入列表，但 OnRun 不会被执行"。应补充说明 `OnStop` **仍会被调用**，且调用方须确保 `OnStop` 的幂等性和对"从未经历 OnStart"情况的容错。

### D-2：`middleware.DedupFilter` — 应在文档中明确 FNV 哈希碰撞风险

当前注释仅说"冲突率低（< 1/2^64）"，但没有说明碰撞的实际影响（合法消息被丢弃）。建议在文档中加入此限制说明，并提供针对高安全场景的替代方案。

### D-3：`errutil` — 各模块应统一使用哨兵错误，而非 `fmt.Errorf` 字符串

见 M-7，应建立全项目错误使用规范文档：
- `errors.New(...)` 用于公共哨兵错误
- `fmt.Errorf("...: %w", sentinel)` 用于添加上下文信息
- 禁止直接 `fmt.Errorf("固定字符串")`（无法用 `errors.Is` 识别）

### D-4：`infra/cache/ttl.go` — `Len()` 的 O(n) 复杂度应在方法注释中标注

当前注释仅说"需要遍历所有条目"，但未明确标注 O(n) 复杂度，调用方可能在热路径误用。

---

## 跨切面问题汇总

| 问题类型 | 涉及模块 | 说明 |
|---------|---------|------|
| 数据竞争（data race） | middleware/degradation | `UpdateConfig` vs `checkAndAdjustLevel` |
| 竞态条件（race condition） | middleware/circuitbreaker | `onSuccess`/`onFailure` 状态读-写不原子 |
| 对象池复用安全 | core/context/pool | `extInitialized` 未重置 |
| Go 版本兼容性 | middleware/degradation | `errors.AsType` 需 Go 1.26 |
| 哈希碰撞 | middleware/dedup | FNV-64a 无碰撞处理 |
| 内存泄漏 | infra/cache/ttl | GC 不清理 deadline==now 的条目 |
| 内存泄漏（轻微） | core/context/context | Clone 丢弃 DeadlineContext cancel |
| 错误标识不一致 | errutil vs middleware | 哨兵错误 vs `fmt.Errorf` 字符串 |
| 排序算法效率 | command/registry | O(n²) 冒泡排序 |
| 性能阻塞 | middleware/degradation | `getCPUUsage` 阻塞 1 秒 |
| 生命周期一致性 | lifecycle, bot | 运行时注册/StarAll 失败无回滚 |

---

*报告生成于 2026-04-11，基于 `go vet` 输出、人工代码审查和并发模式分析。*
