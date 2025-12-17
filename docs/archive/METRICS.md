# 指标收集系统文档

## 概述

Remilia v0.7.1 引入了增强的指标收集系统，提供完整的 Prometheus 集成，帮助你监控和分析 Bot 的运行状况。

## 🌟 主要特性

- ✅ **对象池指标** - 监控 Context 对象池的命中率和使用情况
- ✅ **死信队列指标** - 跟踪失败事件的堆积和消费情况
- ✅ **插件统计指标** - 监控每个插件的处理器和匹配器数量
- ✅ **重试指标** - 跟踪重试次数、成功率和失败率
- ✅ **事件处理指标** - 监控事件处理延迟和吞吐量
- ✅ **Prometheus 集成** - 完整的 Prometheus 指标导出

## 📚 快速开始

### 启用指标收集

```go
engine := remilia.NewEngine()

// 方式 1: 启用默认指标收集
engine.EnableMetrics("remilia")

// 方式 2: 使用自定义收集器
mc := remilia.NewMetricsCollector("my_bot")
engine.SetMetricsCollector(mc)
```

### 暴露 Prometheus 端点

```go
import (
    "net/http"
    "github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
    engine := remilia.NewEngine()
    engine.EnableMetrics("remilia")
    
    // 启动 Prometheus HTTP 服务器
    go func() {
        http.Handle("/metrics", promhttp.Handler())
        http.ListenAndServe(":9090", nil)
    }()
    
    // 启动 Bot...
}
```

## 📊 可用指标

### 对象池指标

| 指标名称 | 类型 | 说明 |
|---------|------|------|
| `remilia_pool_gets_total` | Counter | 对象池 Get 总次数 |
| `remilia_pool_puts_total` | Counter | 对象池 Put 总次数 |
| `remilia_pool_news_total` | Counter | 新建对象总次数（池未命中） |
| `remilia_pool_hit_rate` | Gauge | 对象池命中率（0-1） |
| `remilia_pool_size` | Gauge | 当前对象池大小 |

**使用示例**:
```go
mc := engine.GetMetricsCollector()
snapshot := mc.GetPoolMetrics()

fmt.Printf("命中率: %.2f%%\n", snapshot.HitRate*100)
fmt.Printf("总获取: %d, 新建: %d\n", snapshot.Gets, snapshot.News)
```

### 死信队列指标

| 指标名称 | 类型 | 说明 |
|---------|------|------|
| `remilia_deadletter_queue_size` | Gauge | 死信队列当前大小 |
| `remilia_deadletter_consumed_total` | Counter | 已消费的死信总数 |
| `remilia_deadletter_consumer_duration_seconds` | Histogram | 死信消费耗时 |

**使用示例**:
```go
// 在死信消费器中记录
type MyConsumer struct {
    mc *remilia.MetricsCollector
}

func (c *MyConsumer) Consume(item remilia.DeadLetterItem) {
    start := time.Now()
    // 处理死信...
    duration := time.Since(start)
    c.mc.RecordDeadLetterConsumed(duration)
}
```

### 插件指标

| 指标名称 | 类型 | 标签 | 说明 |
|---------|------|------|------|
| `remilia_plugin_handlers_total` | Gauge | plugin | 插件注册的处理器数量 |
| `remilia_plugin_matchers_total` | Gauge | plugin | 插件注册的匹配器数量 |
| `remilia_plugin_load_duration_seconds` | Histogram | plugin | 插件加载耗时 |
| `remilia_plugin_unload_duration_seconds` | Histogram | plugin | 插件卸载耗时 |

**使用示例**:
```go
// 在插件管理器中自动记录
pm := remilia.NewPluginManager(engine)
pm.Register(myPlugin)  // 自动记录加载时间和插件指标
```

### 重试指标

| 指标名称 | 类型 | 标签 | 说明 |
|---------|------|------|------|
| `remilia_retry_attempts_total` | Counter | attempt | 重试尝试次数 |
| `remilia_retry_successes_total` | Counter | - | 重试成功总数 |
| `remilia_retry_failures_total` | Counter | - | 重试失败总数（进入死信队列） |
| `remilia_retry_delay_seconds` | Histogram | - | 重试延迟时间 |

**attempt 标签值**: "0", "1", "2", ..., "9", "10+"

### 事件处理指标

| 指标名称 | 类型 | 标签 | 说明 |
|---------|------|------|------|
| `remilia_events_processed_total` | Counter | type, source | 已处理事件总数 |
| `remilia_events_dropped_total` | Counter | reason | 丢弃事件总数 |
| `remilia_event_processing_duration_seconds` | Histogram | type | 事件处理耗时 |

**标签说明**:
- `type`: 事件类型（如 C2C_MESSAGE_CREATE）
- `source`: 来源（global, plugin:xxx）
- `reason`: 丢弃原因（concurrency_limit, invalid_event 等）

## 🔧 高级用法

### 自定义指标命名空间

```go
// 使用自定义命名空间
mc := remilia.NewMetricsCollector("my_custom_namespace")
engine.SetMetricsCollector(mc)

// 指标名称将变为: my_custom_namespace_pool_gets_total
```

### 手动记录指标

```go
mc := engine.GetMetricsCollector()
if mc != nil {
    // 记录对象池操作
    mc.RecordPoolGet(false) // 命中
    mc.RecordPoolPut()
    
    // 记录插件指标
    mc.SetPluginHandlers("my_plugin", 10)
    mc.SetPluginMatchers("my_plugin", 5)
    
    // 记录重试
    mc.RecordRetryAttempt(1, 200*time.Millisecond)
    mc.RecordRetrySuccess()
    
    // 记录事件处理
    mc.RecordEventProcessed("C2C_MESSAGE_CREATE", "global", 50*time.Millisecond)
}
```

### 获取指标快照

```go
mc := engine.GetMetricsCollector()
snapshot := mc.GetPoolMetrics()

// 分析对象池性能
if snapshot.HitRate < 0.8 {
    log.Printf("警告: 对象池命中率过低 (%.2f%%)", snapshot.HitRate*100)
}
```

## 📈 Prometheus 查询示例

### 对象池命中率

```promql
# 当前命中率
remilia_pool_hit_rate

# 5分钟内平均命中率
avg_over_time(remilia_pool_hit_rate[5m])
```

### 事件处理速率

```promql
# 每秒处理的事件数
rate(remilia_events_processed_total[1m])

# 按事件类型分组
sum(rate(remilia_events_processed_total[1m])) by (type)
```

### 事件处理延迟

```promql
# P99 延迟
histogram_quantile(0.99, rate(remilia_event_processing_duration_seconds_bucket[5m]))

# 平均延迟
rate(remilia_event_processing_duration_seconds_sum[5m]) / 
rate(remilia_event_processing_duration_seconds_count[5m])
```

### 重试成功率

```promql
# 重试成功率
remilia_retry_successes_total / 
(remilia_retry_successes_total + remilia_retry_failures_total)
```

### 插件健康度

```promql
# 各插件的处理器数量
remilia_plugin_handlers_total

# 插件加载时间
histogram_quantile(0.95, rate(remilia_plugin_load_duration_seconds_bucket[5m]))
```

### 死信队列堆积

```promql
# 死信队列大小
remilia_deadletter_queue_size

# 死信队列增长率
deriv(remilia_deadletter_queue_size[5m])
```

## 🎯 监控告警

### Prometheus 告警规则

```yaml
groups:
  - name: remilia
    rules:
      # 对象池命中率过低
      - alert: LowPoolHitRate
        expr: remilia_pool_hit_rate < 0.7
        for: 5m
        annotations:
          summary: "对象池命中率过低"
          description: "当前命中率: {{ $value }}"
      
      # 事件处理延迟过高
      - alert: HighEventLatency
        expr: |
          histogram_quantile(0.99, 
            rate(remilia_event_processing_duration_seconds_bucket[5m])
          ) > 1.0
        for: 5m
        annotations:
          summary: "事件处理延迟过高"
          description: "P99 延迟: {{ $value }}s"
      
      # 死信队列堆积
      - alert: DeadLetterQueueGrowing
        expr: remilia_deadletter_queue_size > 100
        for: 10m
        annotations:
          summary: "死信队列堆积"
          description: "队列大小: {{ $value }}"
      
      # 重试失败率过高
      - alert: HighRetryFailureRate
        expr: |
          rate(remilia_retry_failures_total[5m]) / 
          rate(remilia_retry_attempts_total[5m]) > 0.1
        for: 5m
        annotations:
          summary: "重试失败率过高"
          description: "失败率: {{ $value }}"
      
      # 事件丢弃
      - alert: EventsBeingDropped
        expr: rate(remilia_events_dropped_total[1m]) > 0
        for: 2m
        annotations:
          summary: "有事件被丢弃"
          description: "丢弃速率: {{ $value }}/s"
```

## 📊 Grafana 仪表板

### 推荐面板

1. **概览面板**
   - 事件处理速率（QPS）
   - 平均处理延迟
   - 对象池命中率
   - 当前并发数

2. **性能面板**
   - P50/P95/P99 延迟
   - 按事件类型的处理速率
   - 对象池使用情况
   - GC 压力

3. **错误面板**
   - 重试次数和成功率
   - 死信队列大小
   - 事件丢弃原因分布
   - 错误率趋势

4. **插件面板**
   - 各插件处理器数量
   - 插件加载/卸载时间
   - 插件处理事件分布

### 示例查询

```json
{
  "panels": [
    {
      "title": "事件处理速率",
      "targets": [
        {
          "expr": "sum(rate(remilia_events_processed_total[1m]))",
          "legendFormat": "总 QPS"
        }
      ]
    },
    {
      "title": "对象池命中率",
      "targets": [
        {
          "expr": "remilia_pool_hit_rate * 100",
          "legendFormat": "命中率 %"
        }
      ]
    }
  ]
}
```

## 💡 最佳实践

### 1. 合理设置命名空间

```go
// 生产环境使用应用名称
mc := remilia.NewMetricsCollector("myapp")

// 多实例部署使用不同命名空间或标签
mc := remilia.NewMetricsCollector("myapp_instance1")
```

### 2. 监控关键指标

**必须监控**:
- 对象池命中率（应 > 80%）
- 事件处理延迟（P99 < 1s）
- 死信队列大小（应接近 0）
- 事件丢弃率（应为 0）

**建议监控**:
- 重试成功率
- 插件加载时间
- 按类型的事件分布

### 3. 设置合理的告警阈值

```yaml
# 根据业务场景调整阈值
- alert: HighLatency
  expr: histogram_quantile(0.99, ...) > 500ms  # 调整为业务可接受值
  for: 5m  # 避免误报
```

### 4. 定期分析指标

```bash
# 导出指标进行分析
curl http://localhost:9090/metrics > metrics.txt

# 分析对象池性能
grep "remilia_pool" metrics.txt

# 分析事件处理
grep "remilia_events_processed" metrics.txt
```

### 5. 性能优化建议

根据指标调优：

- **命中率 < 70%**: 考虑增加对象池预热
- **P99 延迟 > 1s**: 检查处理器逻辑，考虑异步处理
- **死信队列增长**: 检查重试配置，优化错误处理
- **事件丢弃**: 增加并发限制或优化处理速度

## 🔗 相关文档

- [中间件系统](GUIDE.md#中间件系统)
- [错误处理](ERROR_HANDLING.md)
- [性能优化](PERFORMANCE.md)
- [配置文档](CONFIG.md)

## 🆕 v0.7.1 新增

- ✅ 完整的指标收集系统
- ✅ Prometheus 集成
- ✅ 对象池指标
- ✅ 死信队列指标
- ✅ 插件统计指标
- ✅ 重试和事件处理指标

---

**版本**: v0.7.1  
**更新日期**: 2025-11-29

