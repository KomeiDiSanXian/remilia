# Remilia 组件风险与改进清单（2025-12-09）

> 依据当前仓库源码（README 提示的文档版本较旧仅作旁证）进行静态审查，聚焦易出错点、可预期的行为偏差以及可落地的改进方案。紧急程度分为：高（High）/中（Medium）/低（Low）。

## 总览

| 紧急度 | 模块 | 问题 / 改进点 |
| --- | --- | --- |
| High | Engine | `DeleteAllMatchers` 未清理 `sortedCache`，导致已删除 matcher 仍会触发执行 |
| High | Context | `At()`、`SendPrivateFile()` 未校验 `GetAuthor()` 结果，非消息事件会 panic |
| High | DeadLetterQueue | `Enqueue` 在 `Shutdown` 之后写入已关闭 channel，直接 panic 无法拦截 |
| Medium | DeadLetterQueue | `Start()` 仅在启动时复制消费者；运行期新增消费者永远得不到调用 |
| Medium | DeadLetterQueue | Worker 未做 panic recover，任意一个 consumer panic 会悄然停工 |
| Medium | Engine | 批量处理仍逐事件重复排序+分配 Context，偏离批量优化目标 |
| Medium | Engine/Metrics | 定义了 `MetricsCollector` 却未在引擎调用链中记录成功/耗时指标 |
| Medium | Middleware | `Retry` / `WithTimeout` 忽略 `ctx.Context()` 取消信号，退避期间无法在优雅关闭时立刻收敛 |
| Medium | Context | `GetAuthor()` 每次都重新 JSON 反序列化，热点路径 GC 压力大 |
| Low | Plugin | `BasePlugin.Reload` 回滚路径没有恢复插件级中间件统计、指标钩子 |
| Low | Rules | `WithTimeout` 每条规则每次匹配都起 goroutine + timer，存在 DoS 风险 |
| Low | Metrics | `internalPool*` 计数器只暴露 getter，实际从未写入，统计结果恒为 0 |

---

## 详细分析与建议

### Engine (`engine.go`)

1. **[High] 删除全部 matcher 后缓存仍指向旧数据**  
   - 位置：`DeleteAllMatchers()`（约 L118-L130）。清空 `matchers`、`matcherIndex` 后没有同步清理 `sortedCache` ，调用 `ProcessEvent` 时仍会直接读取缓存并执行旧 matcher。  
   - 影响：运维层面认为“已彻底删除全部规则”，但实际仍会执行旧逻辑直到缓存被动失效，等同于功能失控。  
   - 建议：在删除时一并 `sortedCache = make(map[dto.EventType][]*Matcher)`，必要时循环标记 `matcher.deleted = true`，与 `DeleteMatcher` 行为保持一致。

2. **[Medium] 批量 API 未充分复用缓存与上下文**  
   - 位置：`ProcessEventBatch()`（约 L190-L280）。每个事件都重新 `NewContext` 且在通用+特定匹配器并存时再次 `sort.Slice`。  
   - 影响：批量入口仍有 O(n log n) 排序与大量分配，难以达到 README 所述“锁开销下降 99%”指标。  
   - 建议：
     - 引入 `sync.Pool` 复用 `Context`；
     - 对已排序的 `specificMatchers` 与 `genericMatchers` 做线性归并而非全量再排；
     - 批量前生成 `[]*Matcher` 的复用缓存，减少临时切片。

3. **[Medium] MetricsCollector 未接入主路径**  
   - 位置：`invokeHandler()`（约 L420-L520）只在错误时递增 `eventDropped`，成功/耗时/插件维度指标完全缺失，`MetricsCollector` 实际采集不到关键数据。  
   - 影响：Prometheus 仅能观察失败计数，无法做性能告警、SLA。  
   - 建议：在处理前后调用 `RecordEventProcessed` / `RecordEventDropped` 并记录耗时；插件统计可在 matcher 链路中追加 labels。

### Context (`context.go`)

1. **[High] Author 为空时的 panic**  
   - 位置：`SendPrivateFile()`（约 L330-L360）、`At()`（约 L420-L450）。`ctx.GetAuthor()` 可能返回 `nil`（系统事件、非消息类 payload、解析失败），直接访问 `UserOpenID` 或 `ID` 会触发 panic。  
   - 建议：在调用点判空、返回可观察的错误；或在 `GetAuthor()` 中提供缓存与默认值。

2. **[Medium] `GetAuthor()` 高频 JSON 反序列化**  
   - 位置：`GetAuthor()`（约 L360-L410）。每次都 `json.Unmarshal`，热点命令下 GC 抖动明显。  
   - 建议：
     - 将解析结果缓存在 `Context`（例如 `ctx.author atomic.Value`）；
     - 或直接用 `gjson` 取字段（zero-copy）并只解析必需字段。

### Dead Letter Queue (`deadletter_queue.go` & `deadletter_consumers.go`)

1. **[High] Shutdown 后继续入队会 panic**  
   - 位置：`Enqueue()`（约 L150-L240）。`Shutdown()` 会 `close(dlq.queue)`，但 `Enqueue` 未检测队列已关闭，后续写入必然 panic。  
   - 建议：新增 `closed` 原子标记或用 `sync.Once` 封装 `close`，在 `Enqueue` 早期返回并记录告警。

2. **[Medium] 动态消费者无效**  
   - 位置：`Start()`（约 L100-L150）。启动前复制 `consumers`，启动后再 `AddConsumer` 不会影响正在运行的 worker。  
   - 影响：运维在运行时追加消费者时无任何提示但不会生效。  
   - 建议：采用 `sync.RWMutex` + `atomic.Value` 提供实时快照，worker 循环中读取最新列表。

3. **[Medium] Worker 缺少 panic 容错**  
   - 位置：`worker()`（约 L150-L210）。消费者 panic 会直接终止该 worker，`wg` 计数减 1 后不再补齐，导致死信无人消费。  
   - 建议：在 worker 层包裹 `recover`，记录 panic 并继续循环；或在 `Consume` 层加装饰器。

4. **[Medium] KafkaConsumer 仍为占位**  
   - 位置：`deadletter_consumers.go`（约 L200-L240）。当前仅打 warn 日志，README 却宣称支持 Kafka 死信。需说明限制或补齐实现。

### Middleware (`middleware/retry.go`, `rules.go`)

1. **[Medium] Retry 无法响应取消**  
   - 退避通过 `time.Sleep`，即便 `ctx.Context()` 已被 `Bot.Shutdown()` 取消仍要睡满，阻碍优雅关闭。  
   - 建议：使用 `select { case <-time.After(delay): case <-ctx.Context().Done(): ... }`，并在 `ShouldRetry` 中额外判断取消。

2. **[Low] `WithTimeout` goroutine 风险**  
   - 每次匹配都 `go func`，在高 QPS + 恶意长耗规则时容易堆积 goroutine。建议改为可配置 worker / 单次 `context.WithTimeout` 包装或至少加入 `sync.Pool` + 限流。

### Metrics (`metrics.go`)

1. **[Low] 对象池指标永远为零**  
   - 公开的 `internalPoolGets/News` 只在 `GetPoolMetrics` 里读取，从未被 Engine / Pool 写入。  
   - 建议：在 `InstrumentedPool` 中注入 hook 或新增 `RecordPoolStats(pool PoolStats)`，确保指标真实。

### Plugin (`plugin.go`)

1. **[Low] Reload 回滚路径遗漏联动信息**  
   - 当 `Load` 失败回滚旧 matcher 时，没有同步插件级统计/指标（如 `mc.SetPluginMatchers`），也不会重新触发 `notifyLoaded`。  
   - 建议：将指标更新、监听器通知纳入同一事务；回滚后发出 `OnPluginError(..., "reload-rollback", err)` 供监控。

### 其他可见改进

- **Bot / Engine 上下文池化**：当前每条事件都分配新 `Context`，可考虑在 `Engine` 层维护 `sync.Pool`，并在 handler 完成后回收（已有测试提及“Context 过度释放检测”但主线尚未使用）。
- **文档版本一致性**：README 声称 2025-12-07 发布多个版本，但代码库无 git 元信息，建议在 CHANGELOG 中标注真实 tag，对齐用户认知。
- **测试补齐**：新增对 `DeleteAllMatchers`、`DeadLetterQueue.Enqueue` after shutdown、`Context.At()` 非消息事件等场景的单测，防止回归。

---

## 建议的修复优先级

1. **立即处理**：Engine 缓存失效、Context Author 判空、DeadLetterQueue 关闭后的入队保护。  
2. **下一迭代**：DeadLetterQueue 消费者热更新 & panic recover、引擎批量性能、Retry 可取消化、Metrics 接入路径。  
3. **持续改进**：Kafka consumer 真正实现、规则 goroutine 限速、指标完善、插件回滚事件化。

