# Health Check 健康检查

## 概述

健康检查（Health Check）是微服务架构中的重要组件，用于监控服务的运行状态。Remilia 提供了完整的健康检查框架，支持自定义检查器、HTTP 端点和 Kubernetes 探针集成。

## 核心特性

- ✅ **可扩展**: 支持自定义健康检查器
- ✅ **并发检查**: 多个检查器并发执行
- ✅ **超时控制**: 防止检查器阻塞
- ✅ **三种状态**: Healthy / Degraded / Unhealthy
- ✅ **HTTP 端点**: 开箱即用的 HTTP handler
- ✅ **K8s 集成**: 支持 Liveness 和 Readiness 探针
- ✅ **内置检查器**: Engine、对象池、死信队列

## 健康状态

### HealthStatusHealthy（健康）
- 所有检查器都正常
- HTTP 返回 200
- 可以接收流量

### HealthStatusDegraded（降级）
- 部分功能受损，但服务可用
- HTTP 返回 200（可配置）
- Readiness 探针返回 503

### HealthStatusUnhealthy（不健康）
- 服务不可用
- HTTP 返回 503
- Liveness 和 Readiness 探针都返回 503

## 基本用法

### 1. 创建健康检查管理器

```go
package main

import (
    "net/http"
    "github.com/KomeiDiSanXian/remilia"
)

func main() {
    // 创建健康检查管理器
    hc := remilia.NewHealthCheck()
    
    // 设置超时
    hc.SetTimeout(5 * time.Second)
    
    // 注册 HTTP 端点
    http.HandleFunc("/health", hc.HTTPHandler)
    http.HandleFunc("/health/ready", hc.ReadinessHandler)
    http.HandleFunc("/health/live", hc.LivenessHandler)
    
    http.ListenAndServe(":8080", nil)
}
```

### 2. 添加检查器

```go
// 添加 Engine 健康检查
hc.AddChecker(remilia.NewEngineHealthChecker(engine))

// 添加对象池健康检查
hc.AddChecker(remilia.NewContextPoolHealthChecker(80.0)) // 最小命中率 80%

// 添加死信队列健康检查
hc.AddChecker(remilia.NewDeadLetterQueueHealthChecker(
    dlq,
    1000,  // 最大队列大小
    0.1,   // 最大丢弃率 10%
))
```

## 自定义检查器

### 实现 HealthChecker 接口

```go
type HealthChecker interface {
    Name() string
    Check(ctx context.Context) HealthCheckResult
}
```

### 示例：数据库健康检查

```go
type DBHealthChecker struct {
    db *sql.DB
}

func (c *DBHealthChecker) Name() string {
    return "database"
}

func (c *DBHealthChecker) Check(ctx context.Context) remilia.HealthCheckResult {
    // 设置超时
    ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
    defer cancel()
    
    // Ping 数据库
    if err := c.db.PingContext(ctx); err != nil {
        return remilia.HealthCheckResult{
            Status: remilia.HealthStatusUnhealthy,
            Error:  err.Error(),
        }
    }
    
    // 检查连接池状态
    stats := c.db.Stats()
    metadata := map[string]any{
        "open_connections": stats.OpenConnections,
        "idle_connections": stats.Idle,
        "in_use":          stats.InUse,
    }
    
    // 连接数过多，返回降级状态
    if stats.OpenConnections > 90 {
        return remilia.HealthCheckResult{
            Status:   remilia.HealthStatusDegraded,
            Error:    "connection pool nearly full",
            Metadata: metadata,
        }
    }
    
    return remilia.HealthCheckResult{
        Status:   remilia.HealthStatusHealthy,
        Metadata: metadata,
    }
}

// 使用
hc.AddChecker(&DBHealthChecker{db: db})
```

### 示例：Redis 健康检查

```go
type RedisHealthChecker struct {
    client *redis.Client
}

func (c *RedisHealthChecker) Name() string {
    return "redis"
}

func (c *RedisHealthChecker) Check(ctx context.Context) remilia.HealthCheckResult {
    ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
    defer cancel()
    
    // Ping Redis
    if err := c.client.Ping(ctx).Err(); err != nil {
        return remilia.HealthCheckResult{
            Status: remilia.HealthStatusUnhealthy,
            Error:  fmt.Sprintf("redis ping failed: %v", err),
        }
    }
    
    // 获取内存使用情况
    info, err := c.client.Info(ctx, "memory").Result()
    if err != nil {
        return remilia.HealthCheckResult{
            Status: remilia.HealthStatusDegraded,
            Error:  fmt.Sprintf("failed to get redis info: %v", err),
        }
    }
    
    return remilia.HealthCheckResult{
        Status: remilia.HealthStatusHealthy,
        Metadata: map[string]any{
            "info": info,
        },
    }
}
```

### 示例：外部 API 健康检查

```go
type APIHealthChecker struct {
    url    string
    client *http.Client
}

func (c *APIHealthChecker) Name() string {
    return "external_api"
}

func (c *APIHealthChecker) Check(ctx context.Context) remilia.HealthCheckResult {
    ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
    defer cancel()
    
    req, _ := http.NewRequestWithContext(ctx, "GET", c.url+"/health", nil)
    resp, err := c.client.Do(req)
    if err != nil {
        return remilia.HealthCheckResult{
            Status: remilia.HealthStatusUnhealthy,
            Error:  err.Error(),
        }
    }
    defer resp.Body.Close()
    
    if resp.StatusCode != http.StatusOK {
        return remilia.HealthCheckResult{
            Status: remilia.HealthStatusDegraded,
            Error:  fmt.Sprintf("API returned status %d", resp.StatusCode),
        }
    }
    
    return remilia.HealthCheckResult{
        Status: remilia.HealthStatusHealthy,
        Metadata: map[string]any{
            "status_code": resp.StatusCode,
        },
    }
}
```

## HTTP 端点

### 1. 通用健康检查端点

```
GET /health
```

**响应示例**:
```json
{
  "status": "healthy",
  "time": "2025-12-08T00:00:00Z",
  "checks": {
    "engine": {
      "status": "healthy",
      "duration_ms": 0.123,
      "metadata": {
        "matcher_count": 42
      }
    },
    "database": {
      "status": "healthy",
      "duration_ms": 1.234,
      "metadata": {
        "open_connections": 10,
        "idle_connections": 5
      }
    }
  }
}
```

**HTTP 状态码**:
- `200 OK`: Healthy 或 Degraded
- `503 Service Unavailable`: Unhealthy

### 2. Readiness 探针

```
GET /health/ready
```

**用途**: Kubernetes Readiness Probe，决定是否可以接收流量

**HTTP 状态码**:
- `200 OK`: Healthy（可以接收流量）
- `503 Service Unavailable`: Degraded 或 Unhealthy（不能接收流量）

### 3. Liveness 探针

```
GET /health/live
```

**用途**: Kubernetes Liveness Probe，决定是否需要重启容器

**HTTP 状态码**:
- `200 OK`: Healthy 或 Degraded（不需要重启）
- `503 Service Unavailable`: Unhealthy（需要重启）

## Kubernetes 集成

### Deployment 配置

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: remilia-bot
spec:
  replicas: 3
  template:
    spec:
      containers:
        - name: bot
          image: remilia-bot:latest
          ports:
            - containerPort: 8080
              name: http

          # Liveness 探针
          livenessProbe:
            httpGet:
              path: /health/live
              port: 8080
            initialDelaySeconds: 30
            periodSeconds: 10
            timeoutSeconds: 5
            failureThreshold: 3

          # Readiness 探针
          readinessProbe:
            httpGet:
              path: /health/ready
              port: 8080
            initialDelaySeconds: 10
            periodSeconds: 5
            timeoutSeconds: 3
            failureThreshold: 2
  selector:
  selector:




```

## 内置检查器

### 1. EngineHealthChecker

检查 Engine 的运行状态。

```go
checker := remilia.NewEngineHealthChecker(engine)
hc.AddChecker(checker)
```

**检查项**:
- Engine 是否为 nil
- Matcher 数量

**元数据**:
```json
{
  "matcher_count": 42
}
```

### 2. ContextPoolHealthChecker

检查对象池的命中率。

```go
checker := remilia.NewContextPoolHealthChecker(80.0) // 最小命中率 80%
hc.AddChecker(checker)
```

**检查项**:
- 命中率是否低于阈值

**元数据**:
```json
{
  "hit_rate": 95.5,
  "gets": 1000,
  "puts": 1000,
  "news": 45
}
```

**状态**:
- Healthy: 命中率 >= 阈值
- Degraded: 命中率 < 阈值

### 3. DeadLetterQueueHealthChecker

检查死信队列的运行状态。

```go
checker := remilia.NewDeadLetterQueueHealthChecker(
    dlq,
    1000,  // 最大队列大小阈值
    0.1,   // 最大丢弃率 10%
)
hc.AddChecker(checker)
```

**检查项**:
- 队列大小是否超过阈值
- 丢弃率是否过高

**元数据**:
```json
{
  "queue_size": 150,
  "max_size": 10000,
  "processed": 5000,
  "dropped": 50,
  "workers": 3,
  "dropped_rate": 0.01
}
```

**状态**:
- Healthy: 队列正常
- Degraded: 队列堆积或丢弃率过高

## 监控集成

### Prometheus 导出

```go
import (
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promauto"
)

var (
    healthStatus = promauto.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "health_check_status",
            Help: "Health check status (0=unhealthy, 1=degraded, 2=healthy)",
        },
        []string{"checker"},
    )
)

// 定期更新 Prometheus 指标
go func() {
    ticker := time.NewTicker(10 * time.Second)
    defer ticker.Stop()
    
    for range ticker.C {
        response := hc.Check(context.Background())
        
        for name, result := range response.Checks {
            var value float64
            switch result.Status {
            case remilia.HealthStatusHealthy:
                value = 2
            case remilia.HealthStatusDegraded:
                value = 1
            case remilia.HealthStatusUnhealthy:
                value = 0
            }
            healthStatus.WithLabelValues(name).Set(value)
        }
    }
}()
```

### 日志记录

```go
go func() {
    ticker := time.NewTicker(1 * time.Minute)
    defer ticker.Stop()
    
    for range ticker.C {
        response := hc.Check(context.Background())
        
        log.Printf("[Health] Overall status: %s", response.Status)
        
        for name, result := range response.Checks {
            if result.Status != remilia.HealthStatusHealthy {
                log.Printf("[Health] %s: %s - %s",
                    name, result.Status, result.Error)
            }
        }
    }
}()
```

## 最佳实践

### 1. 合理设置超时

```go
// 全局超时
hc.SetTimeout(5 * time.Second)

// 检查器内部超时（更短）
func (c *DBHealthChecker) Check(ctx context.Context) remilia.HealthCheckResult {
    ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
    defer cancel()
    // ...
}
```

### 2. 区分 Liveness 和 Readiness

```go
// Liveness: 只检查关键组件
livenesshc := remilia.NewHealthCheck()
livenesshc.AddChecker(remilia.NewEngineHealthChecker(engine))

// Readiness: 检查所有依赖
readinessHC := remilia.NewHealthCheck()
readinessHC.AddChecker(remilia.NewEngineHealthChecker(engine))
readinessHC.AddChecker(&DBHealthChecker{db: db})
readinessHC.AddChecker(&RedisHealthChecker{client: redis})

http.HandleFunc("/health/live", livenessHC.LivenessHandler)
http.HandleFunc("/health/ready", readinessHC.ReadinessHandler)
```

### 3. 提供详细的元数据

```go
return remilia.HealthCheckResult{
    Status: remilia.HealthStatusHealthy,
    Metadata: map[string]any{
        "version":        "1.0.0",
        "uptime":         time.Since(startTime).String(),
        "goroutines":     runtime.NumGoroutine(),
        "memory_mb":      m.Alloc / 1024 / 1024,
        "connections":    activeConnections,
    },
}
```

### 4. 处理检查器错误

```go
func (c *MyChecker) Check(ctx context.Context) remilia.HealthCheckResult {
    defer func() {
        if r := recover(); r != nil {
            log.Printf("[Health] Checker panicked: %v", r)
        }
    }()
    
    // 检查逻辑
    // ...
}
```

### 5. 定期移除过时的检查器

```go
// 动态移除检查器
if !featureEnabled {
    hc.RemoveChecker("feature_x")
}
```

## 故障排查

### 健康检查超时

**症状**: 健康检查请求很慢或超时

**原因**:
- 某个检查器阻塞
- 数据库查询慢
- 网络延迟

**解决方案**:
```go
// 1. 减少检查器超时
hc.SetTimeout(3 * time.Second)

// 2. 异步检查
type AsyncChecker struct {
    lastResult atomic.Value
}

func (c *AsyncChecker) Check(ctx context.Context) remilia.HealthCheckResult {
    return c.lastResult.Load().(remilia.HealthCheckResult)
}

// 后台更新
go func() {
    for range time.Tick(10 * time.Second) {
        result := actualCheck()
        c.lastResult.Store(result)
    }
}()
```

### 频繁返回 Degraded

**症状**: 服务频繁进入降级状态

**原因**:
- 阈值设置过严
- 资源不足
- 依赖服务不稳定

**解决方案**:
```go
// 调整阈值
checker := remilia.NewContextPoolHealthChecker(70.0) // 从 80% 降到 70%

// 增加容忍度
if result.Status == remilia.HealthStatusDegraded {
    // 连续多次 Degraded 才告警
}
```

## 示例项目

完整示例请参考：`example/healthcheck/main.go`

## 参考资料

- [Kubernetes Probes](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/)
- [Health Check API Pattern](https://microservices.io/patterns/observability/health-check-api.html)

