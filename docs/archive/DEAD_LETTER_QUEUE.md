# Dead Letter Queue 死信队列

## 概述

死信队列（Dead Letter Queue, DLQ）是一种用于处理失败消息的机制。当事件处理失败且重试次数耗尽后，事件会被发送到死信队列，由专门的消费者处理，避免丢失重要数据。

v1.8.0 引入了带容量限制和丢弃策略的死信队列管理器，防止内存堆积风险。

## 核心特性

- ✅ **容量限制**: 防止内存无限增长
- ✅ **丢弃策略**: 队列满时自动处理（丢弃最旧/最新/阻塞）
- ✅ **多消费者**: 支持同时注册多个死信消费者
- ✅ **多 Worker**: 并发处理死信，提高吞吐量
- ✅ **优雅关闭**: 等待所有死信处理完成
- ✅ **监控指标**: 实时统计队列大小、丢弃数等

## 基本用法

### 1. 创建死信队列

```go
package main

import (
    "context"
    "github.com/KomeiDiSanXian/remilia"
)

func main() {
    // 创建死信队列
    dlq := remilia.NewDeadLetterQueue(remilia.DeadLetterQueueConfig{
        MaxSize:    10000,                   // 最大队列容量
        Workers:    3,                       // Worker 数量
        DropPolicy: remilia.DropOldest,     // 丢弃策略
    })

    // 添加消费者
    dlq.AddConsumer(remilia.FileDeadLetterConsumer{
        Path: "dead_letters.log",
    })

    // 启动死信队列
    dlq.Start()
    defer dlq.Shutdown(context.Background())
}
```

### 2. 与重试中间件集成

```go
// 创建死信队列
dlq := remilia.NewDeadLetterQueue(remilia.DeadLetterQueueConfig{
    MaxSize: 1000,
    Workers: 2,
})
dlq.AddConsumer(remilia.FileDeadLetterConsumer{Path: "deadletter.log"})
dlq.Start()

// 创建死信通道
deadLetterCh := make(chan remilia.DeadLetterItem, 128)

// 启动死信处理 goroutine
go func() {
    for item := range deadLetterCh {
        dlq.Enqueue(item)
    }
}()

// 使用重试中间件
engine.Use(middleware.RetryWithDeadLetter(
    middleware.RetryConfig{
        MaxAttempts:  3,
        InitialDelay: time.Second,
    },
    deadLetterCh,
))
```

## 配置选项

```go
type DeadLetterQueueConfig struct {
    // MaxSize 队列最大容量，0 表示不限制（默认 10000）
    MaxSize int

    // Workers 消费者 worker 数量（默认 1）
    Workers int

    // DropPolicy 队列满时的丢弃策略（默认 DropOldest）
    DropPolicy DropPolicy

    // OnDropped 死信被丢弃时的回调（可选）
    OnDropped func(item DeadLetterItem, reason string)

    // OnProcessed 死信被处理时的回调（可选）
    OnProcessed func(item DeadLetterItem, duration time.Duration)
}
```

## 丢弃策略

### 1. DropOldest（推荐）

丢弃队列中最旧的死信，为新死信腾出空间。

```go
dlq := remilia.NewDeadLetterQueue(remilia.DeadLetterQueueConfig{
    MaxSize:    1000,
    DropPolicy: remilia.DropOldest,
})
```

**适用场景**: 
- 实时性要求高的系统
- 最新数据更重要

### 2. DropNewest

丢弃当前提交的死信。

```go
dlq := remilia.NewDeadLetterQueue(remilia.DeadLetterQueueConfig{
    MaxSize:    1000,
    DropPolicy: remilia.DropNewest,
})
```

**适用场景**:
- 历史数据更重要
- 需要保留最早的失败记录

### 3. BlockUntilSpace

阻塞直到队列有空间（带 30 秒超时）。

```go
dlq := remilia.NewDeadLetterQueue(remilia.DeadLetterQueueConfig{
    MaxSize:    1000,
    DropPolicy: remilia.BlockUntilSpace,
})
```

**适用场景**:
- 不能丢失任何死信
- 可以接受处理延迟

**注意**: 可能导致事件处理阻塞，谨慎使用。

## 内置消费者

### 1. FileDeadLetterConsumer

将死信写入文件，每行一个 JSON。

```go
dlq.AddConsumer(remilia.FileDeadLetterConsumer{
    Path: "dead_letters.log",
})
```

### 2. WebhookDeadLetterConsumer

通过 HTTP POST 发送死信到指定 URL。

```go
dlq.AddConsumer(remilia.WebhookDeadLetterConsumer{
    URL:        "https://api.example.com/deadletter",
    Timeout:    5 * time.Second,  // 请求超时
    MaxRetries: 3,                // 最大重试次数
})
```

### 3. 自定义消费者

实现 `DeadLetterConsumer` 接口：

```go
type MyConsumer struct {
    db *sql.DB
}

func (c *MyConsumer) Consume(item remilia.DeadLetterItem) {
    // 将死信存入数据库
    _, err := c.db.Exec(
        "INSERT INTO dead_letters (event_id, event_type, error, attempt) VALUES (?, ?, ?, ?)",
        item.Event.ID,
        item.Event.Type,
        item.Err.Error(),
        item.Attempt,
    )
    if err != nil {
        log.Printf("Failed to save dead letter: %v", err)
    }
}

// 使用
dlq.AddConsumer(&MyConsumer{db: db})
```

## 监控和统计

### 获取统计信息

```go
stats := dlq.Stats()
fmt.Printf("Queue Size: %d/%d\n", stats.QueueSize, stats.MaxSize)
fmt.Printf("Processed: %d\n", stats.Processed)
fmt.Printf("Dropped: %d\n", stats.Dropped)
fmt.Printf("Workers: %d\n", stats.Workers)
```

### 回调监控

```go
dlq := remilia.NewDeadLetterQueue(remilia.DeadLetterQueueConfig{
    MaxSize: 1000,
    Workers: 2,
    
    // 死信被丢弃时的回调
    OnDropped: func(item remilia.DeadLetterItem, reason string) {
        log.Printf("Dead letter dropped: %s, reason: %s",
            item.Event.ID, reason)
        metrics.Inc("deadletter_dropped")
    },
    
    // 死信被处理时的回调
    OnProcessed: func(item remilia.DeadLetterItem, duration time.Duration) {
        log.Printf("Dead letter processed: %s, duration: %v",
            item.Event.ID, duration)
        metrics.Observe("deadletter_process_duration", duration.Seconds())
    },
})
```

### 与 Prometheus 集成

```go
import (
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promauto"
)

var (
    dlqSize = promauto.NewGauge(prometheus.GaugeOpts{
        Name: "deadletter_queue_size",
        Help: "Current size of dead letter queue",
    })
    
    dlqDropped = promauto.NewCounter(prometheus.CounterOpts{
        Name: "deadletter_dropped_total",
        Help: "Total number of dropped dead letters",
    })
)

dlq := remilia.NewDeadLetterQueue(remilia.DeadLetterQueueConfig{
    MaxSize: 1000,
    Workers: 2,
    OnDropped: func(item remilia.DeadLetterItem, reason string) {
        dlqDropped.Inc()
    },
})

// 定期更新队列大小指标
go func() {
    ticker := time.NewTicker(10 * time.Second)
    defer ticker.Stop()
    
    for range ticker.C {
        stats := dlq.Stats()
        dlqSize.Set(float64(stats.QueueSize))
    }
}()
```

## 优雅关闭

### 基本关闭

```go
// 等待所有死信处理完成
err := dlq.Shutdown(context.Background())
if err != nil {
    log.Printf("Shutdown error: %v", err)
}
```

### 带超时的关闭

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

err := dlq.Shutdown(ctx)
if err == context.DeadlineExceeded {
    log.Warn("Shutdown timeout, some dead letters may not be processed")
} else if err != nil {
    log.Printf("Shutdown error: %v", err)
}
```

## 最佳实践

### 1. 合理设置队列容量

```go
// 根据系统流量和失败率估算
// 假设: 1000 QPS, 1% 失败率, 处理延迟 100ms
// 队列容量 = 1000 * 0.01 * 0.1 * 安全系数(10) = 100
dlq := remilia.NewDeadLetterQueue(remilia.DeadLetterQueueConfig{
    MaxSize: 100,
})
```

### 2. 选择合适的 Worker 数量

```go
// CPU 密集型消费者: Workers = CPU 核数
// I/O 密集型消费者: Workers = CPU 核数 * 2-4
dlq := remilia.NewDeadLetterQueue(remilia.DeadLetterQueueConfig{
    Workers: runtime.NumCPU() * 2,
})
```

### 3. 多消费者分离关注点

```go
// 日志消费者
dlq.AddConsumer(remilia.FileDeadLetterConsumer{
    Path: "dead_letters.log",
})

// 告警消费者
dlq.AddConsumer(&AlertConsumer{
    alertAPI: alertAPI,
})

// 持久化消费者
dlq.AddConsumer(&DBConsumer{
    db: db,
})
```

### 4. 监控关键指标

```go
// 定期检查队列状态
go func() {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()
    
    for range ticker.C {
        stats := dlq.Stats()
        
        // 队列使用率超过 80% 告警
        usage := float64(stats.QueueSize) / float64(stats.MaxSize)
        if usage > 0.8 {
            alert.Send(fmt.Sprintf("DLQ usage high: %.2f%%", usage*100))
        }
        
        // 丢弃率超过 10% 告警
        if stats.Processed+stats.Dropped > 0 {
            dropRate := float64(stats.Dropped) / float64(stats.Processed+stats.Dropped)
            if dropRate > 0.1 {
                alert.Send(fmt.Sprintf("DLQ drop rate high: %.2f%%", dropRate*100))
            }
        }
    }
}()
```

### 5. 死信回放机制

```go
// 从文件中读取死信并重新处理
func ReplayDeadLetters(filePath string, engine *remilia.Engine) error {
    file, err := os.Open(filePath)
    if err != nil {
        return err
    }
    defer file.Close()
    
    scanner := bufio.NewScanner(file)
    for scanner.Scan() {
        var item remilia.DeadLetterItem
        if err := json.Unmarshal(scanner.Bytes(), &item); err != nil {
            log.Printf("Failed to unmarshal: %v", err)
            continue
        }
        
        // 重新处理事件
        ctx := remilia.NewContext(item.Event, api)
        if err := engine.ProcessEvent(ctx); err != nil {
            log.Printf("Replay failed: %v", err)
        }
    }
    
    return scanner.Err()
}
```

## 性能优化

### 1. 批量处理

```go
type BatchConsumer struct {
    items []remilia.DeadLetterItem
    mu    sync.Mutex
}

func (c *BatchConsumer) Consume(item remilia.DeadLetterItem) {
    c.mu.Lock()
    c.items = append(c.items, item)
    
    // 批量写入
    if len(c.items) >= 100 {
        c.flush()
    }
    c.mu.Unlock()
}

func (c *BatchConsumer) flush() {
    // 批量写入数据库或文件
    log.Printf("Flushing %d items", len(c.items))
    c.items = c.items[:0]
}
```

### 2. 异步持久化

```go
type AsyncConsumer struct {
    ch chan remilia.DeadLetterItem
}

func NewAsyncConsumer() *AsyncConsumer {
    c := &AsyncConsumer{
        ch: make(chan remilia.DeadLetterItem, 1000),
    }
    
    go c.worker()
    
    return c
}

func (c *AsyncConsumer) Consume(item remilia.DeadLetterItem) {
    select {
    case c.ch <- item:
    default:
        log.Warn("Async consumer channel full, dropping item")
    }
}

func (c *AsyncConsumer) worker() {
    for item := range c.ch {
        // 实际的持久化逻辑
        saveToDatabase(item)
    }
}
```

## 故障排查

### 队列堆积

**症状**: `QueueSize` 持续增长

**原因**:
- 消费者处理速度过慢
- Worker 数量不足
- 下游服务故障

**解决方案**:
```go
// 1. 增加 Worker 数量
dlq.config.Workers = 10

// 2. 优化消费者逻辑
// 3. 使用异步处理
// 4. 临时提高队列容量
```

### 大量丢弃

**症状**: `Dropped` 计数快速增长

**原因**:
- 队列容量不足
- 突发流量
- 处理速度跟不上

**解决方案**:
```go
// 1. 增加队列容量
dlq.config.MaxSize = 20000

// 2. 改用 BlockUntilSpace 策略（谨慎）
dlq.config.DropPolicy = remilia.BlockUntilSpace

// 3. 增加 Worker
dlq.config.Workers = 5
```

## 示例项目

完整示例请参考：`example/deadletter/main.go`

## 参考资料

- [AWS SQS Dead-Letter Queues](https://docs.aws.amazon.com/AWSSimpleQueueService/latest/SQSDeveloperGuide/sqs-dead-letter-queues.html)
- [RabbitMQ Dead Letter Exchanges](https://www.rabbitmq.com/dlx.html)

