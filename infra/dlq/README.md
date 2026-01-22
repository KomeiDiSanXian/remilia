# Remilia Dead Letter Queue (DLQ)

[![Go Reference](https://pkg.go.dev/badge/github.com/KomeiDiSanXian/remilia/infra/dlq.svg)](https://pkg.go.dev/github.com/KomeiDiSanXian/remilia/infra/dlq)

基础设施级死信队列（Dead Letter Queue）实现，用于处理失败的事件。

## 📦 安装

```go
import "github.com/KomeiDiSanXian/remilia/infra/dlq"
```

## 🎯 核心功能

### 1. 死信队列

死信队列用于收集和处理失败的事件：

```go
// 创建配置
config := dlq.DeadLetterQueueConfig{
    MaxSize:    10000,         // 队列最大容量
    Workers:    3,             // 工作协程数
    DropPolicy: dlq.DropOldest, // 队列满时丢弃策略
}

// 创建并启动队列
queue := dlq.NewDeadLetterQueue(config)
queue.Start()
defer queue.Shutdown(context.Background())

// 添加消费者
queue.AddConsumer(dlq.FileConsumer{Path: "deadletter.jsonl"})

// 入队死信
item := dlq.DeadLetterItem{
    Event:   payload,
    Err:     err,
    Attempt: 3,
    Source:  "my-handler",
}
queue.Enqueue(item)
```

### 2. 丢弃策略

支持三种丢弃策略：

```go
const (
    DropOldest      // 丢弃最旧的项目
    DropNewest      // 丢弃最新的项目
    BlockUntilSpace // 阻塞直到有空间
)
```

### 3. 内置消费者

#### FileConsumer - 文件消费者

将死信写入 JSON Lines 文件：

```go
consumer := dlq.FileConsumer{
    Path: "/var/log/deadletters.jsonl",
}
queue.AddConsumer(consumer)
```

**文件格式**（JSON Lines）：
```jsonl
{"event":{"id":"evt-1","type":"C2C_MESSAGE_CREATE"},"error":{"message":"handler failed","source":"plugin:auth","attempt":3}}
{"event":{"id":"evt-2","type":"GROUP_AT_MESSAGE_CREATE"},"error":{"message":"timeout","source":"global","attempt":5}}
```

#### WebhookConsumer - Webhook 消费者

通过 HTTP POST 发送死信到 Webhook：

```go
consumer := dlq.WebhookConsumer{
    URL:        "https://example.com/webhook/deadletter",
    Timeout:    5 * time.Second,
    MaxRetries: 3, // 重试次数（-1 = 默认 3 次）
}
queue.AddConsumer(consumer)
```

**重试策略**：
- 指数退避：1s, 2s, 4s, 8s...
- 非 2xx 响应会重试
- 网络错误会重试

#### KafkaConsumer - Kafka 消费者（占位）

```go
consumer := dlq.KafkaConsumer{
    Brokers: []string{"localhost:9092"},
    Topic:   "deadletter",
}
queue.AddConsumer(consumer)
```

> ⚠️ **注意**：这是占位实现。实际使用需要引入 Kafka 客户端库（如 kafka-go, sarama）。

### 4. 回调函数

监控队列行为：

```go
config := dlq.DeadLetterQueueConfig{
    MaxSize: 100,
    Workers: 2,
    
    // 丢弃回调
    OnDropped: func(item dlq.DeadLetterItem, reason string) {
        log.Printf("Dropped: %s, reason: %s", item.Event.ID, reason)
    },
    
    // 处理完成回调
    OnProcessed: func(item dlq.DeadLetterItem, duration time.Duration) {
        log.Printf("Processed: %s in %v", item.Event.ID, duration)
    },
}
```

### 5. 统计信息

获取队列状态：

```go
stats := queue.Stats()

fmt.Printf("Queue Size: %d/%d\n", stats.QueueSize, stats.MaxSize)
fmt.Printf("Processed: %d\n", stats.Processed)
fmt.Printf("Dropped: %d\n", stats.Dropped)
fmt.Printf("Workers: %d\n", stats.Workers)
fmt.Printf("Consumers: %d\n", stats.Consumers)
fmt.Printf("Closed: %v\n", stats.IsClosed)
```

## 📚 完整示例

### 基础用法

```go
package main

import (
    "context"
    "log"
    "time"
    
    "github.com/KomeiDiSanXian/remilia/infra/dlq"
    "github.com/KomeiDiSanXian/remilia/openapi/dto"
)

func main() {
    // 创建队列
    config := dlq.DeadLetterQueueConfig{
        MaxSize:    1000,
        Workers:    2,
        DropPolicy: dlq.DropOldest,
    }
    
    queue := dlq.NewDeadLetterQueue(config)
    
    // 添加文件消费者
    queue.AddConsumer(dlq.FileConsumer{
        Path: "deadletters.jsonl",
    })
    
    // 启动队列
    queue.Start()
    defer queue.Shutdown(context.Background())
    
    // 入队死信
    item := dlq.DeadLetterItem{
        Event: &dto.Payload{
            ID:   "evt-123",
            Type: dto.C2CMessageCreate,
        },
        Err:     fmt.Errorf("handler failed"),
        Attempt: 3,
        Source:  "my-handler",
    }
    queue.Enqueue(item)
    
    // 等待处理
    time.Sleep(100 * time.Millisecond)
    
    // 查看统计
    stats := queue.Stats()
    log.Printf("Processed: %d, Dropped: %d", stats.Processed, stats.Dropped)
}
```

### 多消费者

```go
// 同时发送到文件和 Webhook
queue.AddConsumer(dlq.FileConsumer{
    Path: "deadletters.jsonl",
})

queue.AddConsumer(dlq.WebhookConsumer{
    URL:        "https://alerts.example.com/deadletter",
    Timeout:    5 * time.Second,
    MaxRetries: 3,
})

queue.Start()
```

### 自定义消费者

```go
type SlackConsumer struct {
    WebhookURL string
}

func (s SlackConsumer) Consume(item dlq.DeadLetterItem) {
    message := fmt.Sprintf(
        "🔴 Dead Letter: Event %s failed after %d attempts\nError: %s",
        item.Event.ID,
        item.Attempt,
        item.Err.Error(),
    )
    
    // 发送到 Slack（简化示例）
    http.Post(s.WebhookURL, "application/json", 
        strings.NewReader(fmt.Sprintf(`{"text": "%s"}`, message)))
}

// 使用
queue.AddConsumer(SlackConsumer{
    WebhookURL: "https://hooks.slack.com/services/YOUR/WEBHOOK/URL",
})
```

### 优雅关闭

```go
// 带超时的关闭
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

if err := queue.Shutdown(ctx); err != nil {
    log.Printf("Shutdown error: %v", err)
} else {
    log.Println("Queue shutdown successfully")
}
```

## 🎨 最佳实践

### 1. 选择合适的队列大小

```go
// ✅ 好：根据实际负载调整
config := dlq.DeadLetterQueueConfig{
    MaxSize: 10000, // 预期峰值的 2-3 倍
    Workers: 4,     // CPU 核心数或 I/O 密集型任务数
}

// ❌ 避免：队列太小导致频繁丢弃
config := dlq.DeadLetterQueueConfig{
    MaxSize: 10, // 太小了
}
```

### 2. 选择合适的丢弃策略

```go
// 时序敏感场景：丢弃旧的
config.DropPolicy = dlq.DropOldest

// 优先级场景：丢弃新的（需要结合业务逻辑）
config.DropPolicy = dlq.DropNewest

// 关键数据：阻塞等待（注意可能影响性能）
config.DropPolicy = dlq.BlockUntilSpace
```

### 3. 监控和告警

```go
// 定期检查统计信息
go func() {
    ticker := time.NewTicker(1 * time.Minute)
    defer ticker.Stop()
    
    for range ticker.C {
        stats := queue.Stats()
        
        // 告警：队列接近满
        if float64(stats.QueueSize)/float64(stats.MaxSize) > 0.8 {
            log.Printf("[WARN] DLQ is 80%% full: %d/%d", 
                stats.QueueSize, stats.MaxSize)
        }
        
        // 告警：丢弃率高
        if stats.Dropped > 100 {
            log.Printf("[WARN] High drop rate: %d items dropped", stats.Dropped)
        }
    }
}()
```

### 4. 使用回调函数

```go
config := dlq.DeadLetterQueueConfig{
    MaxSize: 1000,
    Workers: 2,
    
    OnDropped: func(item dlq.DeadLetterItem, reason string) {
        // 记录到监控系统
        metrics.Increment("dlq.dropped", 1)
        
        // 发送告警
        if reason == "queue_full" {
            alerts.Send("DLQ queue full, dropping items")
        }
    },
    
    OnProcessed: func(item dlq.DeadLetterItem, duration time.Duration) {
        // 记录处理延迟
        metrics.Histogram("dlq.process_duration", duration.Milliseconds())
    },
}
```

## 🔧 配置参考

### DeadLetterQueueConfig

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| MaxSize | int | 10000 | 队列最大容量 |
| Workers | int | 1 | 工作协程数 |
| DropPolicy | DropPolicy | DropOldest | 丢弃策略 |
| OnDropped | func | nil | 丢弃回调 |
| OnProcessed | func | nil | 处理完成回调 |

### WebhookConsumer 配置

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| URL | string | - | Webhook 地址 |
| Timeout | time.Duration | 5s | 请求超时 |
| MaxRetries | int | -1 | 最大重试次数（-1 = 默认 3） |

## ⚠️ 注意事项

### 1. 消费者 Panic 恢复

DLQ 会自动恢复消费者的 panic：

```go
// 即使消费者 panic，队列也会继续工作
queue.AddConsumer(buggyConsumer{}) // panic 不会影响其他消费者
```

### 2. 多消费者执行

所有消费者按顺序执行：

```go
// consumer1 和 consumer2 会依次处理每个死信
queue.AddConsumer(consumer1)
queue.AddConsumer(consumer2)
```

### 3. 队列满时行为

根据 DropPolicy 决定：
- `DropOldest`: 自动丢弃最旧的项目
- `DropNewest`: 忽略新项目
- `BlockUntilSpace`: 阻塞等待（可能影响性能）

## 🆚 与 Root 包的关系

本包是基础设施层实现，root 包提供兼容层：

```go
// 新 API（推荐）
import "github.com/KomeiDiSanXian/remilia/infra/dlq"

queue := dlq.NewDeadLetterQueue(config)
queue.AddConsumer(dlq.FileConsumer{Path: "dead.jsonl"})

// 旧 API（兼容）
import "github.com/KomeiDiSanXian/remilia"

queue := remilia.NewDeadLetterQueue(config)
queue.AddConsumer(remilia.FileDeadLetterConsumer{Path: "dead.jsonl"})
```

## 📖 API 文档

完整的 API 文档请查看：
- [pkg.go.dev](https://pkg.go.dev/github.com/KomeiDiSanXian/remilia/infra/dlq)
- [GoDoc](https://godoc.org/github.com/KomeiDiSanXian/remilia/infra/dlq)

## 📝 变更日志

### v0.9.0 (2026-01-20)

- ✨ 迁移到 infra/dlq/
- 🎯 独立的基础设施包
- 📦 消费者实现分离
- ✅ 100% 向后兼容
- 📖 完整文档

## 📄 许可证

Apache License 2.0 - 查看 [LICENSE](../../LICENSE) 文件
