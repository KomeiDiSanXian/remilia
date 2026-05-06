# 可观测性体系——指标、追踪、日志、健康检查

> **ZeroBot 基因**：ZeroBot 无任何可观测性基础设施（无指标、无追踪、日志仅限简单打印）。这是 Remilia 从零独立设计的能力。参阅 [`11-zerobot-inspiration.md`](11-zerobot-inspiration.md#7-remilia-有但-zerobot-没有的)。

## 设计理念

可观测性不是事后补丁，而是框架的一等公民。Remilia 提供了四维可观测性：

```
    Metrics (Prometheus)
         │
    什么是发生？──── Tracing (OpenTelemetry)
         │
    谁在发生？────── Logging (zerolog)
         │
    还活着吗？────── Health Check
```

每层回答不同的问题，数据互补而不重叠。

## 1. Prometheus 指标

### 自定义注册表隔离

```go
func NewMetricsCollector(namespace string) *Collector {
    return NewMetricsCollectorWithRegistry(namespace, prometheus.NewRegistry())
}
```

每个 `Collector` 使用独立的 `prometheus.Registry`，避免同一进程多次调用时因 metric 名称重复导致 panic。这在多引擎测试场景或同一进程中运行多个 Bot 实例时至关重要。

### 指标分类

**事件层指标**：

```go
eventProcessed *prometheus.CounterVec   // 按事件类型 + 状态（成功/失败）
eventDropped   *prometheus.CounterVec   // 丢弃原因（限流/熔断/超时）
eventLatency   *prometheus.HistogramVec // 延迟分布（P50/P90/P99）
```

**业务层指标**：

```go
commandInvocations *prometheus.CounterVec  // 按命令名 + 状态
messageSent        *prometheus.CounterVec  // 按消息类型 + 状态
messageLatency     *prometheus.HistogramVec
botUptime          prometheus.Gauge         // 1=运行中, 0=停止
```

**中间件层指标**：

```go
retryAttempts  *prometheus.CounterVec   // 重试次数
retrySuccesses prometheus.Counter       // 重试成功
retryFailures  prometheus.Counter       // 重试失败
deadLetterQueueSize    prometheus.Gauge  // 死信队列积压
```

**插件层指标**：

```go
pluginHandlers *prometheus.GaugeVec    // 各插件的 Handler 数
pluginMatchers *prometheus.GaugeVec    // 各插件的 Matcher 数
pluginLoadTime *prometheus.HistogramVec
pluginUnloadTime *prometheus.HistogramVec
```

**平台层指标**：

```go
platformAdapterUp     *prometheus.GaugeVec  // 适配器运行状态
platformDisconnects   *prometheus.CounterVec // 断连次数
platformAdapterErrors *prometheus.CounterVec // 致命错误
```

### 端点暴露

```go
mc := metrics.NewMetricsCollector("mybot")
http.Handle("/metrics", promhttp.HandlerFor(mc.Registry(), promhttp.HandlerOpts{}))
```

## 2. OpenTelemetry 分布式追踪

### 架构

```go
import (
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
    "go.opentelemetry.io/otel/sdk/trace"
)
```

使用 OTLP HTTP Exporter 将 Trace 发送到兼容 OpenTelemetry 的后端（Jaeger、Tempo、Datadog 等）。

### 自适应采样

```go
type AdaptiveSampler struct {
    mu          sync.RWMutex
    targetTPS   float64          // 目标每秒采样数
    currentRate float64          // 当前采样率（0.0 ~ 1.0）
    // 动态调整
    sampleCount atomic.Int64
    windowStart time.Time
}

func (s *AdaptiveSampler) ShouldSample(parameters trace.SamplingParameters) trace.SamplingResult {
    // 根据当前 TPS 动态调整采样率
    // 高负载时自动降采样，低负载时全采样
    elapsed := time.Since(s.windowStart)
    tps := float64(s.sampleCount.Load()) / elapsed.Seconds()

    if tps > s.targetTPS {
        s.currentRate = max(0.01, s.currentRate * 0.9) // 降采样
    } else {
        s.currentRate = min(1.0, s.currentRate * 1.1) // 升采样
    }
    // ...
}
```

在大量事件处理时，自适应采样避免了 Trace 数据量过大导致的后端压力。

### Span 注入

中间件 `Tracing()` 自动为每个事件处理链创建 Span：

```go
func Tracing() eventctx.Middleware {
    return func(next eventctx.Handler) eventctx.Handler {
        return func(ctx *eventctx.Context) error {
            tracer := otel.Tracer("remilia")
            _, span := tracer.Start(ctx, "handle_event")
            defer span.End()

            span.SetAttributes(
                attribute.String("event.type", string(ctx.GetEventType())),
                attribute.String("platform", ctx.GetEventPlatform()),
            )

            return next(ctx)
        }
    }
}
```

## 3. 结构化日志

基于 `rs/zerolog`（零分配 JSON 日志器）实现：

```go
import "github.com/rs/zerolog"

logger.WithFields(logger.Fields{
    "event_type": ctx.GetEventType(),
    "platform":   ctx.GetEventPlatform(),
    "latency":    time.Since(start),
    "matcher":    m.Source,
}).Info("event processed")
```

**零分配设计**：zerolog 在热路径上避免堆分配，适合高性能场景。

**多输出**：支持同时输出到 stdout、文件、syslog 等。

**分级控制**：开发环境用 ConsoleWriter（彩色可读），生产环境用 JSON 格式。

## 4. 健康检查

```go
type Check struct {
    checkers []Checker  // 聚合多个检查器
    mu       sync.RWMutex
}

type Checker interface {
    Name() string
    Check(ctx context.Context) CheckResult
}

type CheckResult struct {
    Status  Status  // Healthy / Degraded / Unhealthy
    Message string
    Details map[string]any
}
```

### Bot 内置检查器

```go
// 适配器健康检查
type AdapterHealthChecker struct {
    adapter platform.Adapter
}

func (c *AdapterHealthChecker) Check(ctx context.Context) CheckResult {
    if c.adapter.IsRunning() {
        return CheckResult{Status: Healthy}
    }
    return CheckResult{Status: Unhealthy, Message: "adapter not running"}
}

// 引擎健康检查
type EngineHealthChecker struct {
    engine *engine.Engine
}

// Bot 状态检查
type BotStatusChecker struct {
    bot *Bot
}
```

### 聚合响应

```go
func (b *Bot) Health() health.CheckResponse {
    ctx, cancel := context.WithTimeout(b.Context(), 5*time.Second)
    defer cancel()
    return b.health.Check(ctx)
}

// 响应示例
{
    "status": "healthy",
    "uptime": "12h34m56s",
    "checks": [
        {"name": "qq-adapter", "status": "healthy"},
        {"name": "engine", "status": "healthy"},
        {"name": "bot-status", "status": "healthy"}
    ]
}
```

### HTTP 端点

```go
// 通过 infra/server 暴露
http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
    result := bot.Health()
    json.NewEncoder(w).Encode(result)
})
```

## 5. pprof 性能分析

内置 pprof 服务器，通过 `WithPprof` 选项启用：

```go
bot, err := remilia.NewBotBuilder().
    WithPlatformAdapter(adapter).
    WithPprof(":6060").
    Build()
```

自动在 Bot 启动时启动 pprof，停止时优雅关闭。

## 完整集成示例

```go
func setupObservability(bot *remilia.Bot) {
    // 1. Prometheus 指标
    mc := metrics.NewMetricsCollector("remilia")
    bot.Engine().SetMetricsCollector(mc)
    http.Handle("/metrics", promhttp.HandlerFor(mc.Registry(), promhttp.HandlerOpts{}))

    // 2. OpenTelemetry 追踪
    tp := initTracerProvider()
    otel.SetTracerProvider(tp)

    // 3. 结构化日志
    logger.SetLevel(logger.InfoLevel)

    // 4. 健康检查端点
    http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
        json.NewEncoder(w).Encode(bot.Health())
    })

    // 5. 启动 HTTP Server
    go http.ListenAndServe(":9090", nil)
}
```

## 迭代过程

### V0：logrus + 基础指标

初始版本使用 `sirupsen/logrus` 作为日志库，指标使用在根包定义的简单结构体：

```go
// V0 — logrus + 基础 metrics
import "github.com/sirupsen/logrus"

var log = logrus.New()

func (e *Engine) DoSomething() {
    log.WithField("key", "value").Info("doing something")
}
```

```go
// V0 metrics.go — 直接在根包
type MetricsCollector struct {
    // 使用全局 prometheus 注册表
    eventProcessed *prometheus.CounterVec
}
```

**问题**：
- logrus 在热路径上分配较多（entry 对象、interface{} 转换），在 475K msg/s 下 GC 压力大
- 使用全局 Prometheus 注册表：同一进程运行两个 Bot 实例时，metric 名称冲突导致 panic
- 指标收集器没有统一的管理方式——Engine、middleware、插件各自注册，容易重复
- 健康检查简陋——只有一个 `IsRunning()` 方法

### V1：zerolog 替换 logrus（零分配日志）

决定将 logrus 替换为 `rs/zerolog`，后者在设计上就是零分配：

```go
// V1 — zerolog 代码（commit 69aecda）
import "github.com/rs/zerolog"

// zerolog 的 Logger 值类型（可拷贝），热路径上不分配
logger.WithFields(logger.Fields{
    "event_type": ctx.GetEventType(),
    "latency":    time.Since(start),
}).Info("event processed")
```

**对比**：

| 方面 | logrus | zerolog |
|------|--------|---------|
| 热路径分配 | 每次调用分配 Entry + map | 零分配（值类型 + 链式调用） |
| 序列化 | 反射 | 类型化写入 |
| 性能（单条日志） | ~1-2 μs | ~100-300 ns |
| 接口风格 | 可变参数 `WithField(k, v)` | 链式 `.Str().Int().Msg()` |

这一替换使热路径日志开销降低约 **10 倍**。

### V2：自定义 Prometheus Registry 替代全局注册表

```go
// V1 方式 — 全局注册表，多实例必 panic
func NewMetricsCollector(namespace string) *Collector {
    factory := promauto.With(prometheus.DefaultRegisterer)
    factory.NewCounterVec(...) // 第二次调用时 panic: "duplicate metrics collector registration"
}
```

```go
// V2（当前）— 独立 Registry
func NewMetricsCollector(namespace string) *Collector {
    registry := prometheus.NewRegistry()
    return NewMetricsCollectorWithRegistry(namespace, registry)
}

// 暴露时使用自定义的 Handler
http.Handle("/metrics", promhttp.HandlerFor(mc.Registry(), promhttp.HandlerOpts{}))
```

同时将指标收集器通过 `infra/metrics` 包统一管理，消除各模块各自注册的问题：

```go
// 统一的指标收集器
type Collector struct {
    registry  prometheus.Registerer  // 自定义注册表

    // 事件层
    eventProcessed *prometheus.CounterVec
    eventDropped   *prometheus.CounterVec
    eventLatency   *prometheus.HistogramVec

    // 中间件层
    retryAttempts  *prometheus.CounterVec
    deadLetterQueueSize prometheus.Gauge

    // 插件层
    pluginHandlers *prometheus.GaugeVec
    pluginLoadTime *prometheus.HistogramVec

    // 平台层
    platformAdapterUp     *prometheus.GaugeVec
    platformDisconnects   *prometheus.CounterVec
}
```

### V3：健康检查框架化 + 自适应追踪采样

从 Bot 中提取通用的健康检查框架：

```go
// V0 健康的检查 — Bot 方法
func (b *Bot) IsHealthy() bool {
    return b.wh.IsRunning() && b.engine != nil
}
```

```go
// V3（当前）— 框架化
type Check struct {
    checkers []Checker  // 聚合多个检查器
}

// 各模块实现 Checker 接口
health.AddChecker(NewAdapterHealthChecker(adapter))
health.AddChecker(health.NewEngineHealthChecker(engine))
health.AddChecker(NewBotStatusChecker(bot))
```

追踪系统引入了**自适应采样**——在高负载时自动降采样以避免压垮 Trace 后端：

```go
type AdaptiveSampler struct {
    targetTPS   float64   // 目标每秒采样数
    currentRate float64   // 动态调整的采样率
}

func (s *AdaptiveSampler) ShouldSample() bool {
    tps := s.measureTPS()
    if tps > s.targetTPS {
        s.currentRate *= 0.9  // 降采样
    } else {
        s.currentRate = min(1.0, s.currentRate * 1.1)  // 升采样
    }
    return rand.Float64() < s.currentRate
}
```

TPO 配置从 Zipkin Exporter 迁移到 OTLP HTTP Exporter（OTEL 标准协议），与更广泛的生态兼容。

## 迭代历程

| 版本 | 核心变化 | 解决的问题 |
|------|---------|-----------|
| V0 | logrus + 全局 Prometheus + 简陋健康检查 | 快速实现 |
| V1 | zerolog 替换 logrus | 热路径日志零分配，降低 GC 压力 |
| V2 | 自定义 Prometheus Registry + infra/metrics 统一管理 | 多实例冲突、分散注册 |
| V3（当前） | 健康检查框架化 + 自适应追踪采样 + OTLP 标准 | 聚合检查、自适应数据量 |

## 设计权衡

| 维度 | 选择 | 理由 |
|------|------|------|
| 指标 | 独立 Prometheus Registry | 避免多实例冲突 |
| 追踪 | 自适应采样 | 控制数据量，降低后端成本 |
| 日志 | zerolog 零分配 | 高性能场景必须 |
| 健康 | 聚合多检查器 | 单一端点获取全貌 |
| 分析 | 内置 pprof | 开箱即用调试 |
