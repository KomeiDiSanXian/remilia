# 分布式追踪配置

> **最后更新**: 2026-08-04  


## 启用追踪

在 `config.yaml` 中添加追踪配置：

```yaml
tracing:
  enable: true
  service_name: "my-remilia-bot"
  service_version: "1.0.0"
  environment: "production"
  exporter: "otlp"  # otlp, zipkin, stdout
  endpoint: "http://localhost:4318"
  sampling_rate: 1.0  # 0.0 - 1.0 (1.0 = 100% 采样)
  include_event_detail: false  # 是否包含事件详情（可能包含敏感信息）
  headers:  # 可选：认证头
    # Authorization: "Bearer YOUR_TOKEN"
```

## 支持的追踪后端

### 1. Grafana Tempo（推荐）

```yaml
tracing:
  enable: true
  service_name: "remilia-bot"
  exporter: "otlp"  # 或 "tempo"
  endpoint: "http://localhost:4318"
  sampling_rate: 0.1  # 生产环境建议降低采样率
```

**本地启动 Tempo:**
```bash
docker run -d --name tempo \
  -p 4318:4318 \
  -p 3200:3200 \
  grafana/tempo:latest
```

### 2. Zipkin

```yaml
tracing:
  enable: true
  service_name: "remilia-bot"
  exporter: "zipkin"
  endpoint: "http://localhost:9411/api/v2/spans"
  sampling_rate: 1.0
```

**本地启动 Zipkin:**
```bash
docker run -d --name zipkin \
  -p 9411:9411 \
  openzipkin/zipkin:latest
```

### 3. Grafana Cloud（生产环境推荐）

```yaml
tracing:
  enable: true
  service_name: "remilia-bot"
  exporter: "otlp"
  endpoint: "https://otlp-gateway-prod-xx.grafana.net/otlp"
  sampling_rate: 0.1
  headers:
    Authorization: "Basic <YOUR_BASE64_TOKEN>"
```

### 4. 控制台输出（调试用）

```yaml
tracing:
  enable: true
  service_name: "remilia-bot"
  exporter: "stdout"
  sampling_rate: 1.0
```

## 代码中使用追踪

### 1. 自动追踪（推荐）

```go
// 在 Engine 上启用追踪中间件
func main() {
    eng := engine.NewEngine()
    
    // 添加追踪中间件（会自动追踪所有事件）
    eng.Use(telemetry.Tracing(telemetry.DefaultTracingConfig()))
    
    // 创建适配器并启动
    adapter := qq.NewWebhookServerAdapter(":8080", &dto.BotInfo{AppID: 123456})
    bot, _ := remilia.NewBotBuilder().WithPlatformAdapter(adapter).WithEngine(eng).Build()
    bot.Start()
}
```

### 2. 追踪特定中间件

```go
// 为自定义中间件添加追踪
func MyMiddleware() context.Middleware {
    return telemetry.TracingNamed("my-middleware", func(next context.Handler) context.Handler {
        return func(ctx *context.Context) error {
            // 中间件逻辑
            return next(ctx)
        }
    })
}
```

### 3. 追踪特定 Handler

```go
// 追踪命令处理器
eng.OnCommand(eventctx.EventGroup, "/ping").Handle(
    telemetry.TracingHandler("ping", func(ctx *context.Context) error {
        ctx.Reply(platform.TextMessage("Pong!"))
            return nil
    }),
)
```

### 4. 手动创建 Span

```go
import (
    "go.opentelemetry.io/otel"
    "github.com/KomeiDiSanXian/remilia/infra/tracing"
)

func myHandler(ctx *context.Context) error {
    // 获取 tracer
    tracer := otel.Tracer("my-service")
    
    // 创建 span
    stdCtx, span := tracer.Start(ctx.Context(), "my-operation")
    defer span.End()
    
    // 注入 trace context
    ctx.SetStdContext(stdCtx)
    
    // 添加属性
    span.SetAttributes(
        attribute.String("user_id", "12345"),
        attribute.Int("item_count", 10),
    )
    
    // 业务逻辑
    // ...
    
    return nil
}
```

### 5. 日志关联（Trace Context）

```go
import "github.com/KomeiDiSanXian/remilia/middleware/telemetry"

func myHandler(ctx *context.Context) error {
    // 获取 trace ID（用于日志关联）
    traceID := telemetry.GetTraceID(ctx)
    spanID := telemetry.GetSpanID(ctx)
    
    logger.WithFields(logger.Fields{
        "trace_id": traceID,
        "span_id":  spanID,
    }).Info("Processing event")
    
    return nil
}
```

## 最佳实践

### 1. 采样策略

- **开发环境**: `sampling_rate: 1.0` (100%)
- **测试环境**: `sampling_rate: 0.5` (50%)
- **生产环境**: `sampling_rate: 0.1` (10%) 或更低

### 2. 敏感信息保护

默认情况下，事件内容不会被记录。如需启用：

```yaml
tracing:
  include_event_detail: true  # 谨慎启用！
```

建议在生产环境关闭此选项，避免泄露用户消息内容。

### 3. 性能影响

| 采样率 | 性能影响 | 适用场景 |
|--------|---------|----------|
| 1.0 (100%) | ~5-10% CPU | 开发/调试 |
| 0.1 (10%) | < 1% CPU | 生产环境 |
| 0.01 (1%) | 可忽略 | 高负载生产环境 |

### 4. 与日志系统集成

结合日志记录 trace ID，实现日志和 trace 的关联：

```go
eng.Use(middleware.Logging())  // 日志中间件
eng.Use(telemetry.Tracing(telemetry.DefaultTracingConfig()))  // 追踪中间件
```

日志输出会自动包含 `trace_id` 和 `span_id` 字段。

## 查看追踪数据

### Grafana Tempo

1. 访问 Grafana: `http://localhost:3000`
2. 配置 Tempo 数据源: `http://localhost:3200`
3. 在 Explore 中查询 trace

### Zipkin

访问 Zipkin UI: `http://localhost:9411`

## 故障排查

### 1. 追踪数据未显示

检查：
- 配置的 `endpoint` 是否正确
- 后端服务是否正常运行
- 网络连接是否正常
- 采样率是否过低

### 2. 性能下降

- 降低采样率
- 检查后端是否响应缓慢
- 考虑使用批量导出

### 3. 日志中显示追踪错误

```
[Tracing] Failed to export spans: ...
```

通常是网络或配置问题，不会影响业务逻辑。

## 架构说明

```
Event → Tracing Middleware → 创建 Root Span
  ↓
  → Middleware 1 → 创建 Child Span
  ↓
  → Middleware 2 → 创建 Child Span
  ↓
  → Handler → 创建 Child Span
  ↓
  → API Call → 创建 Child Span
```

每个层级都会创建子 span，形成完整的调用链路。

## 配置示例

### 最小配置（开发环境）

```yaml
tracing:
  enable: true
  service_name: "remilia-bot"
  exporter: "stdout"
  sampling_rate: 1.0
```

### 完整配置（生产环境）

```yaml
tracing:
  enable: true
  service_name: "remilia-prod-bot"
  service_version: "1.2.3"
  environment: "production"
  exporter: "otlp"
  endpoint: "https://tempo.example.com/otlp"
  sampling_rate: 0.1
  include_event_detail: false
  headers:
    Authorization: "Bearer SECRET_TOKEN"
```

## 相关资源

- [OpenTelemetry 官方文档](https://opentelemetry.io/docs/)
- [Grafana Tempo 文档](https://grafana.com/docs/tempo/)
- [Zipkin 文档](https://zipkin.io/)

