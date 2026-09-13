# Remilia Stats Package

**基础统计原语**，提供零依赖、线程安全的统计数据收集功能，面向宿主应用与插件作者。

> **与 `builtin/stats` 的区别**
>
> | 包 | 定位 | 使用者 |
> |---|---|---|
> | `stats/`（本包）| 基础数据结构：Counter、Gauge、Histogram、QuantileHistogram | 宿主应用与插件作者 |
> | `builtin/stats/` | 用户行为统计插件：命令次数、活跃 UV | Bot 业务层，通过插件系统注册 |
>
> 框架内部的时间序列指标**不经过本包**：`middleware/ratelimit` 为自适应限流器实现了
> 固定桶 + ping-pong 双缓冲的专用直方图，`core/engine` 使用自身的 `MatcherStats` /
> `TempManagerStats`。因此修改本包不会影响限流或引擎行为。

## 功能

### Counter - 计数器

原子计数器，用于累计统计：

```go
counter := stats.NewCounter()
counter.Inc()        // +1
counter.Add(10)      // +10
value := counter.Get() // 获取当前值
counter.Reset()      // 重置为 0
```

### Gauge - 计量器

原子计量器，用于记录瞬时值：

```go
gauge := stats.NewGauge()
gauge.Set(100)      // 设置值
gauge.Inc()         // +1
gauge.Dec()         // -1
value := gauge.Get() // 获取当前值
```

### Histogram - 直方图

记录数值分布统计：

```go
histogram := stats.NewHistogram()
histogram.Observe(100)
histogram.Observe(200)
histogram.Observe(150)

count := histogram.Count()  // 观测次数
sum := histogram.Sum()      // 总和
min := histogram.Min()      // 最小值
max := histogram.Max()      // 最大值
avg := histogram.Avg()      // 平均值
```

## 类型定义

### BatchStats（已废弃）

> **Deprecated**：无内部使用者，且 `core/engine` 使用自身的统计类型。仅为兼容保留。

批量处理统计信息：

```go
type BatchStats struct {
    TotalBatches    uint64
    TotalEvents     uint64
    TotalDuration   time.Duration
    AvgBatchSize    float64
    AvgDuration     time.Duration
    EventsPerSecond float64
}
```

### EngineStats（已废弃）

> **Deprecated**：无内部使用者。`core/engine` 使用 `MatcherStats` 与 `TempManagerStats`
> （见 `core/engine/engine_query.go`、`core/engine/temp_manager.go`）。

Engine 统计信息：

```go
type EngineStats struct {
    MatcherCount      int
    EventsProcessed   int64
    EventsFailed      int64
    ActiveMatchers    int
    TempMatchersCount int
}
```

## 性能

所有统计类型都是线程安全的，使用原子操作实现：

- Counter/Gauge: 无锁操作
- Histogram: CAS 操作确保一致性
- 并发性能优秀

## 示例

```go
import "github.com/KomeiDiSanXian/remilia/stats"

// 统计请求数
requestCounter := stats.NewCounter()

// 统计活跃连接数
activeConnections := stats.NewGauge()

// 统计响应时间
responseTime := stats.NewHistogram()

func HandleRequest() {
    requestCounter.Inc()
    activeConnections.Inc()
    defer activeConnections.Dec()
    
    start := time.Now()
    // ... 处理请求 ...
    duration := time.Since(start).Milliseconds()
    responseTime.Observe(duration)
}

func GetStats() {
    fmt.Printf("Total requests: %d\n", requestCounter.Get())
    fmt.Printf("Active connections: %d\n", activeConnections.Get())
    fmt.Printf("Avg response time: %.2fms\n", responseTime.Avg())
}
```
