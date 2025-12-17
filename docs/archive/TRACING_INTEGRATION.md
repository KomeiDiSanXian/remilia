# Remilia 分布式追踪集成指南

> 版本: v1.2.1+  
> 最后更新: 2025-12-07

---

## 📖 概述

Remilia 通过 `context.Context` 原生支持分布式追踪（Distributed Tracing），可以轻松集成 OpenTelemetry、Jaeger、Zipkin 等追踪系统。

本文档介绍如何在 Remilia 中集成和使用分布式追踪功能。

---

## 🎯 为什么需要分布式追踪

在生产环境中，分布式追踪可以帮助你：

- 📊 **可视化请求链路**：查看事件处理的完整链路
- ⏱️ **性能分析**：识别性能瓶颈和慢操作
- 🐛 **问题定位**：快速定位错误发生的位置
- 📈 **依赖关系**：了解服务间的依赖和调用关系

---

## 🔧 OpenTelemetry 集成

### 1. 安装依赖

```bash
go get go.opentelemetry.io/otel
go get go.opentelemetry.io/otel/exporters/jaeger
go get go.opentelemetry.io/otel/sdk/trace
go get go.opentelemetry.io/otel/sdk/resource
go get go.opentelemetry.io/otel/semconv/v1.17.0
```

### 2. 初始化 Tracer

```go
package main

import (
    "context"
    "log"

    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/exporters/jaeger"
    "go.opentelemetry.io/otel/sdk/resource"
    "go.opentelemetry.io/otel/sdk/trace"
    semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
)

// 初始化 OpenTelemetry
func initTracer() (*trace.TracerProvider, error) {
    // 创建 Jaeger exporter
    exporter, err := jaeger.New(jaeger.WithCollectorEndpoint(
        jaeger.WithEndpoint("http://localhost:14268/api/traces"),
    ))
    if err != nil {
        return nil, err
    }

    // 创建 TracerProvider
    tp := trace.NewTracerProvider(
        trace.WithBatcher(exporter),
        trace.WithResource(resource.NewWithAttributes(
            semconv.SchemaURL,
            semconv.ServiceName("remilia-bot"),
            semconv.ServiceVersion("v1.2.1"),
            attribute.String("environment", "production"),
        )),
    )

    // 设置全局 TracerProvider
    otel.SetTracerProvider(tp)

    return tp, nil
}
```

### 3. 创建追踪中间件

```go
package middleware

import (
    "fmt"

    "github.com/KomeiDiSanXian/remilia"
    "github.com/KomeiDiSanXian/remilia/openapi/dto"
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/codes"
    "go.opentelemetry.io/otel/trace"
)

// Tracing 创建分布式追踪中间件
func Tracing() remilia.HandlerMiddleware {
    tracer := otel.Tracer("remilia")

    return func(next remilia.HandlerE) remilia.HandlerE {
        return func(ctx *remilia.Context) error {
            // 从事件中提取 trace 信息（如果有）
            spanName := fmt.Sprintf("handle_%s", ctx.GetEventType())

            // 创建 span
            stdCtx, span := tracer.Start(
                ctx.Context(),
                spanName,
                trace.WithAttributes(
                    attribute.String("event.type", string(ctx.GetEventType())),
                    attribute.String("event.id", string(ctx.GetEventID())),
                    attribute.String("guild.id", ctx.GetGuildID()),
                    attribute.String("channel.id", ctx.GetChannelID()),
                    attribute.String("author.id", ctx.GetAuthorID()),
                ),
                trace.WithSpanKind(trace.SpanKindServer),
            )
            defer span.End()

            // 替换 Context 的标准 context
            originalCtx := ctx.Context()
            ctx.SetStdContext(stdCtx)
            defer ctx.SetStdContext(originalCtx)

            // 执行 handler
            err := next(ctx)

            // 记录错误
            if err != nil {
                span.RecordError(err)
                span.SetStatus(codes.Error, err.Error())
            } else {
                span.SetStatus(codes.Ok, "")
            }

            return err
        }
    }
}
```

### 4. 添加 SetStdContext 方法

在 `context.go` 中添加方法（如果还没有）：

```go
// SetStdContext 设置标准库 context
// 用于中间件注入自定义 context（如 tracing context）
func (ctx *Context) SetStdContext(stdCtx context.Context) {
    ctx.ctx = stdCtx
}
```

### 5. 注册中间件

```go
package main

import (
    "github.com/KomeiDiSanXian/remilia"
    "github.com/KomeiDiSanXian/remilia/middleware"
)

func main() {
    // 初始化 tracer
    tp, err := initTracer()
    if err != nil {
        log.Fatal(err)
    }
    defer tp.Shutdown(context.Background())

    // 创建引擎
    engine := remilia.NewEngine()

    // 注册追踪中间件（应该是第一个中间件）
    engine.Use(middleware.Tracing())
    engine.Use(middleware.Logging())
    engine.Use(middleware.Recover(engine))

    // 注册 handler
    engine.On(dto.C2CMessageCreate, OnCommand("/hello")).HandleE(func(ctx *remilia.Context) error {
        // handler 中可以使用 ctx.Context() 创建子 span
        return handleHello(ctx)
    })

    // 启动 bot...
}
```

### 6. 在 Handler 中创建子 Span

```go
func handleHello(ctx *remilia.Context) error {
    tracer := otel.Tracer("remilia")

    // 创建子 span
    _, span := tracer.Start(ctx.Context(), "fetch_user_info")
    defer span.End()

    // 执行业务逻辑
    userInfo, err := fetchUserInfo(ctx)
    if err != nil {
        span.RecordError(err)
        return err
    }

    span.SetAttributes(
        attribute.String("user.name", userInfo.Name),
        attribute.Int64("user.level", userInfo.Level),
    )

    // 回复消息
    return ctx.ReplyText(fmt.Sprintf("Hello, %s!", userInfo.Name))
}

func fetchUserInfo(ctx *remilia.Context) (*UserInfo, error) {
    tracer := otel.Tracer("remilia")

    // 创建数据库查询 span
    dbCtx, span := tracer.Start(ctx.Context(), "db.query",
        trace.WithAttributes(
            attribute.String("db.system", "postgresql"),
            attribute.String("db.operation", "SELECT"),
        ),
    )
    defer span.End()

    // 使用带 trace 的 context 进行数据库查询
    var user UserInfo
    err := db.QueryRowContext(dbCtx, "SELECT * FROM users WHERE id = ?", ctx.GetAuthorID()).Scan(&user)
    if err != nil {
        span.RecordError(err)
        return nil, err
    }

    return &user, nil
}
```

---

## 🔍 查看追踪数据

### 启动 Jaeger

使用 Docker 快速启动 Jaeger：

```bash
docker run -d --name jaeger \
  -e COLLECTOR_ZIPKIN_HOST_PORT=:9411 \
  -p 5775:5775/udp \
  -p 6831:6831/udp \
  -p 6832:6832/udp \
  -p 5778:5778 \
  -p 16686:16686 \
  -p 14268:14268 \
  -p 14250:14250 \
  -p 9411:9411 \
  jaegertracing/all-in-one:latest
```

访问 Jaeger UI：`http://localhost:16686`

### 追踪示例

在 Jaeger UI 中，你可以看到：

```
Service: remilia-bot
├─ handle_C2C_MESSAGE_CREATE (150ms)
   ├─ fetch_user_info (80ms)
   │  └─ db.query (75ms)
   └─ send_message (60ms)
      └─ http.post (55ms)
```

---

## 📊 高级用法

### 1. 传播 Trace Context

如果需要调用外部 HTTP 服务并传播 trace：

```go
import (
    "net/http"
    "go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func callExternalAPI(ctx *remilia.Context) error {
    // 使用 otelhttp.Transport 自动传播 trace context
    client := &http.Client{
        Transport: otelhttp.NewTransport(http.DefaultTransport),
    }

    req, _ := http.NewRequestWithContext(ctx.Context(), "GET", "https://api.example.com/data", nil)
    resp, err := client.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    // 处理响应...
    return nil
}
```

### 2. 添加自定义属性

```go
func handleCommand(ctx *remilia.Context) error {
    span := trace.SpanFromContext(ctx.Context())

    // 添加自定义属性
    span.SetAttributes(
        attribute.String("command", ctx.GetMessageContent()),
        attribute.Int("user.level", getUserLevel(ctx)),
        attribute.Bool("is.admin", isAdmin(ctx)),
    )

    // 添加事件
    span.AddEvent("command_validated", trace.WithAttributes(
        attribute.String("result", "success"),
    ))

    // 业务逻辑...
    return nil
}
```

### 3. 错误追踪

```go
func handleWithError(ctx *remilia.Context) error {
    span := trace.SpanFromContext(ctx.Context())

    err := processMessage(ctx)
    if err != nil {
        // 记录错误到 span
        span.RecordError(err)
        span.SetStatus(codes.Error, "failed to process message")

        // 添加错误详情
        span.SetAttributes(
            attribute.String("error.type", fmt.Sprintf("%T", err)),
            attribute.String("error.message", err.Error()),
        )

        return err
    }

    span.SetStatus(codes.Ok, "")
    return nil
}
```

---

## 🎨 采样策略

为了减少性能开销，可以配置采样策略：

```go
func initTracerWithSampling() (*trace.TracerProvider, error) {
    exporter, err := jaeger.New(jaeger.WithCollectorEndpoint(
        jaeger.WithEndpoint("http://localhost:14268/api/traces"),
    ))
    if err != nil {
        return nil, err
    }

    tp := trace.NewTracerProvider(
        trace.WithBatcher(exporter),
        // 采样策略：10% 的请求被追踪
        trace.WithSampler(trace.TraceIDRatioBased(0.1)),
        trace.WithResource(resource.NewWithAttributes(
            semconv.SchemaURL,
            semconv.ServiceName("remilia-bot"),
        )),
    )

    otel.SetTracerProvider(tp)
    return tp, nil
}
```

---

## 📚 最佳实践

### 1. Span 命名规范

- 使用小写和下划线：`fetch_user_info`
- 包含操作类型：`db.query`, `http.get`, `cache.set`
- 描述性强：避免使用 `process`, `handle` 等模糊名称

### 2. 属性命名

遵循 OpenTelemetry 语义约定：
- HTTP: `http.method`, `http.status_code`, `http.url`
- DB: `db.system`, `db.operation`, `db.statement`
- 自定义: 使用点号分隔，如 `user.id`, `order.amount`

### 3. 性能考虑

- 不要在循环中创建过多 span
- 合理设置采样率
- 避免在 span 属性中存储大量数据

### 4. 错误处理

- 始终调用 `span.End()`（使用 defer）
- 记录错误：`span.RecordError(err)`
- 设置状态：`span.SetStatus(codes.Error, msg)`

---

## 🔗 相关资源

- [OpenTelemetry Go 文档](https://opentelemetry.io/docs/instrumentation/go/)
- [Jaeger 文档](https://www.jaegertracing.io/docs/)
- [OpenTelemetry 语义约定](https://opentelemetry.io/docs/reference/specification/trace/semantic_conventions/)

---

## 💡 示例项目

完整示例代码请参考：`example/tracing/`

```bash
cd example/tracing
go run main.go
```

然后访问 `http://localhost:16686` 查看追踪数据。

---

**下一步**: 参考 [性能分析指南](PERFORMANCE_ANALYSIS.md) 了解如何使用 pprof 进行性能优化。

