# Remilia Stats Package

统计指标包，提供线程安全的统计数据收集功能。

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

### BatchStats

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

### EngineStats

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
