# Remilia 分布式追踪示例

本示例展示如何在 Remilia Bot 中使用 OpenTelemetry 进行分布式追踪。

## 特性

- ✅ 基于 OpenTelemetry 标准
- ✅ 支持多种追踪后端（Tempo, Zipkin, Grafana Cloud）
- ✅ 自动追踪事件处理链路
- ✅ 与日志系统集成（Trace Context）
- ✅ 零侵入式设计
- ✅ 避免使用已归档的 Jaeger

## 快速开始

### 1. 运行示例

```bash
cd examples/tracing-demo
go run main.go
```

### 2. 配置追踪后端

#### 使用 Grafana Tempo（推荐）

```bash
# 启动 Tempo
docker run -d --name tempo \
  -p 4318:4318 \
  -p 3200:3200 \
  grafana/tempo:latest

# 启动 Grafana
docker run -d --name grafana \
  -p 3000:3000 \
  grafana/grafana:latest
```

修改代码中的配置：
```go
tracingConfig := tracing.Config{
    Enabled:      true,
    ServiceName:  "remilia-bot",
    Exporter:     "otlp",
    Endpoint:     "http://localhost:4318",
    SamplingRate: 1.0,
}
```

#### 使用 Zipkin

```bash
# 启动 Zipkin
docker run -d --name zipkin \
  -p 9411:9411 \
  openzipkin/zipkin:latest
```

修改配置：
```go
tracingConfig := tracing.Config{
    Enabled:      true,
    ServiceName:  "remilia-bot",
    Exporter:     "zipkin",
    Endpoint:     "http://localhost:9411/api/v2/spans",
    SamplingRate: 1.0,
}
```

### 3. 查看追踪数据

#### Grafana Tempo

1. 访问 Grafana: http://localhost:3000
2. 添加 Tempo 数据源: http://tempo:3200
3. 在 Explore 中查询 trace

#### Zipkin

访问 Zipkin UI: http://localhost:9411

## 代码说明

### 初始化追踪

```go
// 1. 创建追踪配置
tracingConfig := tracing.Config{
    Enabled:        true,
    ServiceName:    "my-bot",
    ServiceVersion: "1.0.0",
    Environment:    "production",
    Exporter:       "otlp",
    Endpoint:       "http://localhost:4318",
    SamplingRate:   0.1,  // 10% 采样
}

// 2. 初始化追踪提供者
tracingProvider, err := tracing.NewProvider(tracingConfig)
if err != nil {
    log.Fatal(err)
}
defer tracingProvider.Shutdown(context.Background())
```

### 添加追踪中间件

```go
// 全局追踪中间件（推荐）
bot.Use(middleware.Tracing(middleware.DefaultTracingConfig()))

// 或自定义配置
bot.Use(middleware.Tracing(middleware.TracingConfig{
    TracerName:         "my-bot",
    IncludeEventDetail: false,  // 生产环境建议关闭
    MaxContentLength:   200,
}))
```

### 追踪特定 Handler

```go
bot.OnCommand("/ping").Handle(
    middleware.TracingHandler("ping", func(ctx *context.Context) error {
        // 获取 trace ID
        traceID := middleware.GetTraceID(ctx)
        
        logger.WithField("trace_id", traceID).Info("Processing")
        
        return ctx.ReplyGroup(&dto.Message{
            Content: "Pong!",
        })
    }),
)
```

### 日志关联

```go
func myHandler(ctx *context.Context) error {
    // 自动在日志中包含 trace context
    traceID := middleware.GetTraceID(ctx)
    spanID := middleware.GetSpanID(ctx)
    
    logger.WithFields(logger.Fields{
        "trace_id": traceID,
        "span_id":  spanID,
    }).Info("Processing event")
    
    return nil
}
```

## 追踪层级

```
Root Span: event.MESSAGE
├── Span: middleware.auth
├── Span: middleware.ratelimit
├── Span: middleware.dedup
├── Span: handler.ping
│   ├── Span: api.send_message
│   └── Span: db.query
└── Span: middleware.logging
```

## 配置文件示例

### config.yaml

```yaml
tracing:
  enable: true
  service_name: "remilia-bot"
  service_version: "1.0.0"
  environment: "production"
  exporter: "otlp"
  endpoint: "http://localhost:4318"
  sampling_rate: 0.1
  include_event_detail: false
```

## 性能影响

| 采样率 | CPU 开销 | 适用场景 |
|--------|---------|----------|
| 1.0 (100%) | ~5-10% | 开发/调试 |
| 0.1 (10%) | < 1% | 生产环境 |
| 0.01 (1%) | 可忽略 | 高负载场景 |

## 最佳实践

1. **生产环境降低采样率**: `sampling_rate: 0.1` 或更低
2. **关闭敏感信息**: `include_event_detail: false`
3. **使用批量导出**: OpenTelemetry SDK 默认启用
4. **日志 trace 关联**: 在日志中记录 trace_id
5. **设置超时**: 配置导出器超时时间

## 故障排查

### 追踪数据未显示

1. 检查后端服务是否运行
2. 验证 endpoint 配置是否正确
3. 检查网络连接
4. 查看日志中的错误信息

### 性能下降

1. 降低采样率
2. 检查后端响应时间
3. 使用本地化部署

## 支持的追踪后端

| 后端 | 协议 | 推荐场景 | 状态 |
|------|------|---------|------|
| Grafana Tempo | OTLP | 生产环境 | ✅ 推荐 |
| Zipkin | Zipkin | 兼容场景 | ✅ 支持 |
| Grafana Cloud | OTLP | 云服务 | ✅ 支持 |
| Jaeger | - | - | ❌ 已归档 |

## 相关文档

- [OpenTelemetry 官方文档](https://opentelemetry.io/)
- [Grafana Tempo 文档](https://grafana.com/docs/tempo/)
- [Zipkin 文档](https://zipkin.io/)
- [完整配置指南](../../docs/02-user-guides/tracing.md)

## 许可证

MIT

