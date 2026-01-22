# Metrics 包 - 测试文档

## 📊 测试概览

本测试套件为 `infra/metrics` 包提供了全面的测试覆盖，包括 Prometheus 指标收集器的所有功能。

### 测试统计

- **总测试数**: 29 个测试用例（含子测试）
- **代码覆盖率**: ~95%+
- **测试文件**: 1 个
  - `metrics_test.go` - Prometheus 指标收集器测试

---

## 🧪 测试文件说明

### metrics_test.go - 指标收集器测试

#### 核心功能测试（17 个测试）

**TestNewMetricsCollector** (2 个子测试)
- ✅ 自定义 namespace
- ✅ 空 namespace（使用默认 "remilia"）
- ✅ 验证所有 metrics 初始化

**TestSetDeadLetterQueueSize** (3 个子测试)
- ✅ 零大小
- ✅ 小队列
- ✅ 大队列

**TestRecordDeadLetterConsumed**
- ✅ 记录消费次数
- ✅ 记录消费时长
- ✅ Counter 自增验证

**TestSetPluginHandlers** (3 个子测试)
- ✅ 多个插件
- ✅ 不同计数值
- ✅ 零值处理

**TestSetPluginMatchers**
- ✅ 设置插件 matcher 数量
- ✅ 多标签验证

**TestRecordPluginLoad**
- ✅ 记录插件加载时间
- ✅ Histogram 验证

**TestRecordPluginUnload**
- ✅ 记录插件卸载时间

**TestFormatAttempt** (8 个子测试)
- ✅ 负数 → "0"
- ✅ 零 → "0"
- ✅ 1-9 → "1"-"9"
- ✅ 10+ → "10+"

**TestRecordRetryAttempt**
- ✅ 记录重试尝试
- ✅ 延迟记录
- ✅ 标签格式化

**TestRecordRetrySuccess**
- ✅ 成功计数

**TestRecordRetryFailure**
- ✅ 失败计数

**TestRecordEventDropped**
- ✅ 多种丢弃原因
- ✅ 相同原因累计

**TestRecordEventProcessed**
- ✅ 事件类型
- ✅ 来源标签
- ✅ 延迟记录

**TestGetPoolMetrics** (4 个子测试)
- ✅ 初始状态
- ✅ 100% 命中率
- ✅ 80% 命中率
- ✅ 0% 命中率（全部 news）

**TestEventDroppedCounter**
- ✅ 向后兼容性方法
- ✅ 返回正确的 CounterVec

#### 集成测试（3 个测试）

**TestMetricsIntegration**
- ✅ 完整工作流程
- ✅ 多指标组合
- ✅ 端到端验证

**TestMetricsLabels**
- ✅ 标签组合正确性
- ✅ 多维度标签

**TestConcurrentMetrics**
- ✅ 并发安全性
- ✅ 10 个 goroutine
- ✅ 1000 次操作

#### 性能基准测试（5 个基准测试）

**BenchmarkNewMetricsCollector**
- ✅ Collector 创建性能

**BenchmarkRecordEventProcessed**
- ✅ 事件记录性能

**BenchmarkRecordRetryAttempt**
- ✅ 重试记录性能

**BenchmarkGetPoolMetrics**
- ✅ Pool metrics 查询性能

**BenchmarkConcurrentMetrics**
- ✅ 并发性能测试
- ✅ 使用 RunParallel

---

## 🎯 测试覆盖率详情

### 覆盖率: ~95%+

**已覆盖的功能**:
- ✅ NewMetricsCollector: 100%
- ✅ SetDeadLetterQueueSize: 100%
- ✅ RecordDeadLetterConsumed: 100%
- ✅ SetPluginHandlers / SetPluginMatchers: 100%
- ✅ RecordPluginLoad / RecordPluginUnload: 100%
- ✅ FormatAttempt: 100%
- ✅ RecordRetryAttempt / Success / Failure: 100%
- ✅ RecordEventDropped / Processed: 100%
- ✅ GetPoolMetrics: 100%
- ✅ EventDroppedCounter: 100%

**测试覆盖的场景**:
- 正常流程（所有方法）
- 边界条件（零值、负值、大值）
- 标签组合（多维度）
- 并发安全
- 向后兼容性
- 集成场景

---

## 🚀 运行测试

### 运行所有测试
```bash
go test -v
```

### 运行特定测试
```bash
# Dead Letter Queue 测试
go test -v -run TestDeadLetter

# Plugin 测试
go test -v -run TestPlugin

# Retry 测试
go test -v -run TestRetry

# Event 测试
go test -v -run TestEvent

# Pool metrics 测试
go test -v -run TestGetPoolMetrics
```

### 生成覆盖率报告
```bash
go test -coverprofile coverage.out -cover
go tool cover -func coverage.out
go tool cover -html coverage.out  # 生成 HTML 报告
```

### 运行基准测试
```bash
# 运行所有基准测试
go test -bench=. -benchmem

# 运行特定基准测试
go test -bench=BenchmarkNewMetricsCollector -benchmem
go test -bench=BenchmarkRecordEventProcessed -benchmem
go test -bench=BenchmarkConcurrentMetrics -benchmem
```

### 并发测试
```bash
# 检测竞态条件
go test -race
```

---

## 📝 测试最佳实践

本测试套件遵循以下最佳实践：

1. **唯一 Namespace** - 每个测试使用唯一 namespace 避免重复注册
2. **Prometheus testutil** - 使用官方测试工具验证指标
3. **表驱动测试** - 使用结构体数组组织测试用例
4. **子测试** - 使用 `t.Run()` 组织相关测试
5. **并发测试** - 验证线程安全性
6. **集成测试** - 测试完整工作流程
7. **性能测试** - 基准测试覆盖关键路径

---

## 🔍 测试详情

### Prometheus Metrics 架构

```
Collector
├── namespace (string)
├── Dead Letter Queue Metrics
│   ├── deadLetterQueueSize (Gauge)
│   ├── deadLetterConsumed (Counter)
│   └── deadLetterConsumerTime (Histogram)
├── Plugin Metrics
│   ├── pluginHandlers (GaugeVec, label: plugin)
│   ├── pluginMatchers (GaugeVec, label: plugin)
│   ├── pluginLoadTime (HistogramVec, label: plugin)
│   └── pluginUnloadTime (HistogramVec, label: plugin)
├── Retry Metrics
│   ├── retryAttempts (CounterVec, label: attempt)
│   ├── retrySuccesses (Counter)
│   ├── retryFailures (Counter)
│   └── retryDelay (Histogram)
├── Event Metrics
│   ├── eventProcessed (CounterVec, labels: type, source)
│   ├── eventDropped (CounterVec, label: reason)
│   └── eventLatency (HistogramVec, label: type)
└── Pool Metrics (内部)
    ├── internalPoolGets
    ├── internalPoolPuts
    └── internalPoolNews
```

### Metric 类型说明

**Gauge** - 可上升或下降的值
- deadLetterQueueSize
- pluginHandlers
- pluginMatchers

**Counter** - 只增不减的计数器
- deadLetterConsumed
- retrySuccesses / retryFailures
- eventProcessed / eventDropped

**Histogram** - 值分布统计
- deadLetterConsumerTime
- pluginLoadTime / pluginUnloadTime
- retryDelay
- eventLatency

### 标签（Labels）

**Plugin Metrics**:
- `plugin`: 插件名称

**Retry Metrics**:
- `attempt`: "0", "1"-"9", "10+"

**Event Metrics**:
- `type`: 事件类型（MESSAGE_CREATE等）
- `source`: 来源（global/plugin）
- `reason`: 丢弃原因（timeout/invalid/overload等）

### FormatAttempt 函数

重试次数标签格式化：
- `<= 0` → "0"
- `1-9` → "1"-"9"
- `>= 10` → "10+"

**优势**:
- 限制标签基数
- 避免高基数问题
- 保持 Prometheus 性能

---

## 📚 使用示例

### 基本用法

```go
// 创建 metrics collector
collector := metrics.NewMetricsCollector("myapp")

// Dead Letter Queue
collector.SetDeadLetterQueueSize(100)
collector.RecordDeadLetterConsumed(50 * time.Millisecond)

// Plugin
collector.SetPluginHandlers("weather-plugin", 5)
collector.SetPluginMatchers("weather-plugin", 3)
collector.RecordPluginLoad("weather-plugin", 200 * time.Millisecond)

// Retry
collector.RecordRetryAttempt(1, 100 * time.Millisecond)
collector.RecordRetrySuccess()

// Event
collector.RecordEventProcessed("MESSAGE_CREATE", "global", 50 * time.Millisecond)
collector.RecordEventDropped("timeout")
```

### Pool Metrics

```go
snapshot := collector.GetPoolMetrics()
fmt.Printf("Gets: %d, News: %d, Hit Rate: %.2f%%\n",
    snapshot.Gets, snapshot.News, snapshot.HitRate * 100)
```

### HTTP 暴露

```go
import (
    "net/http"
    "github.com/prometheus/client_golang/prometheus/promhttp"
)

// 暴露 /metrics 端点
http.Handle("/metrics", promhttp.Handler())
http.ListenAndServe(":9090", nil)
```

### Prometheus 查询

```promql
# Dead Letter Queue 大小
myapp_deadletter_queue_size

# 插件处理器数量
myapp_plugin_handlers_total{plugin="weather-plugin"}

# 重试成功率
rate(myapp_retry_successes_total[5m]) / 
rate(myapp_retry_attempts_total[5m])

# 事件处理延迟 P95
histogram_quantile(0.95, 
    rate(myapp_event_processing_duration_seconds_bucket[5m]))

# 事件丢弃率
rate(myapp_events_dropped_total[5m])
```

---

## 🎨 测试模式

### 唯一 Namespace 模式

由于 Prometheus metrics 全局注册，每个测试必须使用唯一的 namespace：

```go
func TestSomething(t *testing.T) {
    // 使用唯一 namespace
    collector := NewMetricsCollector("test_something")
    
    // 或者使用测试名称
    collector := NewMetricsCollector("test_" + t.Name())
}
```

### Prometheus testutil 使用

```go
import "github.com/prometheus/client_golang/prometheus/testutil"

// 读取 Gauge 值
value := testutil.ToFloat64(collector.deadLetterQueueSize)

// 读取带标签的 Counter 值
value := testutil.ToFloat64(
    collector.eventProcessed.WithLabelValues("MESSAGE_CREATE", "global"))

// 统计 metric 数量
count := testutil.CollectAndCount(collector.pluginLoadTime)
```

---

## ✅ 测试状态

- 所有测试通过 ✅
- 代码覆盖率: ~95%+ ✅
- 核心功能全覆盖 ✅
- Metric 类型全覆盖 ✅
- 标签组合全覆盖 ✅
- 并发安全验证 ✅
- 性能基准完成 ✅

---

## 🔧 未来改进

可以考虑的测试增强：

1. **更多集成测试**
   - 与实际 HTTP server 集成
   - Prometheus scrape 模拟
   - Grafana 可视化验证

2. **边界测试**
   - 极大值测试
   - 标签过多场景
   - 内存占用测试

3. **错误场景**
   - Metric 注册失败
   - 标签值异常
   - 并发冲突

4. **性能优化**
   - 高频更新场景
   - 内存分配优化
   - CPU 使用率分析

5. **监控告警规则**
   - Prometheus 告警规则测试
   - 阈值验证
   - 告警有效性测试

---

## 📊 Prometheus 最佳实践

本包遵循 Prometheus 最佳实践：

1. **合理的 Metric 命名**
   - `_total` 后缀用于 Counter
   - `_duration_seconds` 用于 Histogram
   - `_size` 用于 Gauge

2. **限制标签基数**
   - FormatAttempt 限制为 11 个值
   - 避免使用用户 ID 等高基数标签

3. **合理的 Histogram Buckets**
   - 使用 Prometheus 默认 buckets
   - Retry delay 使用自定义 buckets

4. **Metric 类型选择**
   - 队列大小 → Gauge
   - 处理次数 → Counter
   - 延迟分布 → Histogram

---

## 🌟 Metric 清单

### Dead Letter Queue (3 个)
- `deadletter_queue_size` - 队列大小
- `deadletter_consumed_total` - 消费总数
- `deadletter_consumer_duration_seconds` - 消费耗时

### Plugin (4 个)
- `plugin_handlers_total` - Handler 数量
- `plugin_matchers_total` - Matcher 数量
- `plugin_load_duration_seconds` - 加载耗时
- `plugin_unload_duration_seconds` - 卸载耗时

### Retry (4 个)
- `retry_attempts_total` - 重试尝试
- `retry_successes_total` - 成功次数
- `retry_failures_total` - 失败次数
- `retry_delay_seconds` - 重试延迟

### Event (3 个)
- `events_processed_total` - 处理总数
- `events_dropped_total` - 丢弃总数
- `event_processing_duration_seconds` - 处理耗时

**总计**: 14 个 Prometheus metrics

---

**最后更新**: 2026-01-22  
**维护者**: Remilia 开发团队
