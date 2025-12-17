# 优雅降级机制 (Graceful Degradation)

## 概述

优雅降级机制是一个自适应的系统保护机制，当系统负载过高时，自动丢弃或延迟处理低优先级事件，确保关键业务不受影响，保护系统稳定性。

## 特性

✅ **自适应监控** - 自动监控 CPU、内存、协程数量等系统指标  
✅ **多级降级** - 支持 4 个降级级别（Normal/Light/Moderate/Severe）  
✅ **多种策略** - 支持丢弃、延迟、简化三种降级策略  
✅ **事件优先级** - 根据事件类型自动分类优先级  
✅ **自定义分类器** - 支持自定义事件优先级分类逻辑  
✅ **实时统计** - 提供实时的降级统计信息  
✅ **零内存分配** - 基准测试显示 0 allocs/op  
✅ **超低开销** - 正常模式下仅 1.9 ns/op

## 快速开始

### 基本用法

```go
package main

import (
    "context"
    "github.com/KomeiDiSanXian/remilia"
    "github.com/KomeiDiSanXian/remilia/middleware"
)

func main() {
    // 创建降级控制器
    deg := middleware.NewAdaptiveDegradation(middleware.DegradationConfig{
        CPUThreshold:    80.0,  // CPU 超过 80% 开始降级
        MemoryThreshold: 85.0,  // 内存超过 85% 开始降级
        Strategy:        middleware.DegradationDrop,  // 丢弃策略
    })

    // 启动监控（后台goroutine）
    go deg.StartMonitor(context.Background())

    // 注册中间件
    engine := remilia.NewEngine()
    engine.Use(deg.Middleware())

    // 注册处理器
    engine.OnAny().Handle(func(ctx *remilia.Context) {
        // 业务逻辑
    })

    // ...
}
```

## 降级级别

### LevelNormal (0) - 正常状态
- **行为**: 所有事件正常处理
- **触发条件**: 系统负载正常

### LevelLight (1) - 轻度降级
- **行为**: 丢弃低优先级事件（PriorityLow）
- **触发条件**: 系统负载得分 >= 10
- **保留事件**: Normal, High, Critical 优先级

### LevelModerate (2) - 中度降级
- **行为**: 只处理高优先级和关键事件
- **触发条件**: 系统负载得分 >= 20
- **保留事件**: High, Critical 优先级

### LevelSevere (3) - 重度降级
- **行为**: 只处理关键事件
- **触发条件**: 系统负载得分 >= 30
- **保留事件**: Critical 优先级

## 事件优先级

### 默认优先级分类

```go
PriorityLow      = 0  // 未知事件类型
PriorityNormal   = 1  // MESSAGE_CREATE（普通消息）
PriorityHigh     = 2  // GROUP_AT_MESSAGE_CREATE（@消息）、C2C_MESSAGE_CREATE（私聊）
PriorityCritical = 3  // GUILD_CREATE/DELETE、GROUP_ADD/DEL_ROBOT（系统事件）
```

### 自定义优先级分类器

```go
deg := middleware.NewAdaptiveDegradation(middleware.DegradationConfig{
    PriorityClassifier: func(ctx *remilia.Context) middleware.EventPriority {
        // 自定义逻辑
        eventType := ctx.GetEventType()
        
        // 例如：将管理员消息设为最高优先级
        if isAdmin(ctx.GetAuthor()) {
            return middleware.PriorityCritical
        }
        
        // 使用默认分类
        return middleware.PriorityNormal
    },
})
```

## 降级策略

### 1. DegradationDrop（丢弃策略）

直接丢弃低优先级事件，不占用系统资源。

```go
deg := middleware.NewAdaptiveDegradation(middleware.DegradationConfig{
    Strategy: middleware.DegradationDrop,
})
```

**适用场景**: 高流量场景，对事件处理的实时性要求高

### 2. DegradationDelay（延迟策略）

延迟处理低优先级事件，给系统缓冲时间。

```go
deg := middleware.NewAdaptiveDegradation(middleware.DegradationConfig{
    Strategy: middleware.DegradationDelay,
})
```

**适用场景**: 希望尽可能处理所有事件，但允许低优先级事件延迟

### 3. DegradationSimplify（简化策略）

在Context中设置降级标记，由业务层决定如何简化处理。

```go
deg := middleware.NewAdaptiveDegradation(middleware.DegradationConfig{
    Strategy: middleware.DegradationSimplify,
})

engine.OnAny().Handle(func(ctx *remilia.Context) {
    degraded, _ := ctx.GetState("degraded")
    if degraded != nil {
        // 简化处理逻辑
        ctx.ReplyText("系统繁忙，请稍后重试")
        return
    }
    
    // 正常处理逻辑
    // ...
})
```

**适用场景**: 需要根据业务逻辑灵活调整处理复杂度

## 配置选项

### 完整配置

```go
deg := middleware.NewAdaptiveDegradation(middleware.DegradationConfig{
    // 阈值配置
    CPUThreshold:    80.0,              // CPU 使用率阈值（%）
    MemoryThreshold: 85.0,              // 内存使用率阈值（%）
    LatencyThreshold: 500 * time.Millisecond,  // 延迟阈值（暂未使用）
    
    // 监控配置
    MonitorInterval:  5 * time.Second,  // 监控间隔
    RecoveryInterval: 10 * time.Second, // 恢复检查间隔
    
    // 策略配置
    Strategy: middleware.DegradationDrop,  // 降级策略
    
    // 高级配置
    EnableGoroutineLimit: true,         // 启用协程数量监控
    GoroutineThreshold:   10000,        // 协程数量阈值
    DelayQueueSize:       1000,         // 延迟队列大小
    
    // 自定义配置
    PriorityClassifier: func(ctx *remilia.Context) middleware.EventPriority {
        // 自定义优先级分类逻辑
        return middleware.PriorityNormal
    },
    
    // 回调函数
    OnLevelChange: func(from, to middleware.DegradationLevel) {
        log.Printf("降级级别变化: %v -> %v", from, to)
    },
})
```

### 默认值

```go
CPUThreshold:         80.0
MemoryThreshold:      85.0
LatencyThreshold:     500 * time.Millisecond
MonitorInterval:      5 * time.Second
RecoveryInterval:     10 * time.Second
DelayQueueSize:       1000
GoroutineThreshold:   10000
PriorityClassifier:   defaultPriorityClassifier
```

## 统计信息

### 获取统计

```go
stats := deg.Stats()

fmt.Printf("降级级别: %v\n", stats.Level)
fmt.Printf("总事件数: %d\n", stats.TotalEvents)
fmt.Printf("丢弃事件数: %d\n", stats.DroppedEvents)
fmt.Printf("延迟事件数: %d\n", stats.DelayedEvents)
fmt.Printf("丢弃率: %.2f%%\n", stats.DropRate)
fmt.Printf("CPU使用率: %.2f%%\n", stats.CPU)
fmt.Printf("内存使用率: %.2f%%\n", stats.Memory)
fmt.Printf("协程数量: %d\n", stats.Goroutines)
```

### 重置统计

```go
deg.Reset()
```

## 手动控制

### 强制设置降级级别

```go
// 强制进入轻度降级
deg.ForceLevel(middleware.LevelLight)

// 恢复正常
deg.ForceLevel(middleware.LevelNormal)
```

**使用场景**: 
- 测试
- 预知的高流量事件前预防性降级
- 运维手动干预

## 负载计算算法

降级级别根据系统负载分数计算：

```
负载分数 = CPU超额部分 × 0.4 + 内存超额部分 × 0.4 + 协程超额部分 × 0.2

其中:
- CPU超额 = (当前CPU% - 阈值) × 0.4
- 内存超额 = (当前内存% - 阈值) × 0.4
- 协程超额 = ((当前协程数 - 阈值) / 阈值 × 100) × 0.2

降级级别:
- score >= 30 -> LevelSevere
- score >= 20 -> LevelModerate
- score >= 10 -> LevelLight
- score < 10  -> LevelNormal
```

## 最佳实践

### 1. 合理设置阈值

```go
// 低负载环境（开发/测试）
DegradationConfig{
    CPUThreshold:    90.0,
    MemoryThreshold: 90.0,
}

// 中等负载环境（预生产）
DegradationConfig{
    CPUThreshold:    80.0,
    MemoryThreshold: 85.0,
}

// 高负载环境（生产）
DegradationConfig{
    CPUThreshold:    70.0,
    MemoryThreshold: 75.0,
}
```

### 2. 配合其他中间件使用

```go
engine := remilia.NewEngine()

// 1. 并发限流
engine.Use(middleware.ConcurrencyLimit(1000, middleware.ConcurrencyDrop, 0))

// 2. 优雅降级（降级前的最后防线）
engine.Use(deg.Middleware())

// 3. 熔断器
engine.Use(middleware.CircuitBreakerMiddleware(cb))

// 4. 重试
engine.Use(middleware.Retry(middleware.RetryConfig{
    MaxAttempts: 3,
}))
```

### 3. 监控告警集成

```go
deg := middleware.NewAdaptiveDegradation(middleware.DegradationConfig{
    OnLevelChange: func(from, to middleware.DegradationLevel) {
        // 发送告警
        if to >= middleware.LevelModerate {
            alerting.Send("系统进入中度降级")
        }
        
        // 记录指标
        metrics.RecordDegradationLevel(int(to))
    },
})
```

### 4. 定期统计报告

```go
go func() {
    ticker := time.NewTicker(1 * time.Minute)
    for range ticker.C {
        stats := deg.Stats()
        log.Printf("降级统计: Level=%v, Dropped=%d, DropRate=%.2f%%", 
            stats.Level, stats.DroppedEvents, stats.DropRate)
    }
}()
```

## 性能指标

基准测试结果（AMD Ryzen 7 5800H）:

```
BenchmarkAdaptiveDegradation_Normal-16      605307333    1.909 ns/op    0 B/op    0 allocs/op
BenchmarkAdaptiveDegradation_Degraded-16    252185820    4.788 ns/op    0 B/op    0 allocs/op
```

- **正常模式**: 1.9 ns/op，零内存分配
- **降级模式**: 4.8 ns/op，零内存分配
- **性能影响**: 几乎可以忽略不计

## 依赖

需要安装 gopsutil 库用于系统监控：

```bash
go get github.com/shirou/gopsutil/v3
```

## 示例项目

完整示例请参考：`example/middleware/degradation/`

## 常见问题

### Q1: 降级会影响性能吗？

A: 几乎没有影响。基准测试显示正常模式下仅 1.9 ns/op，即使在降级模式下也只有 4.8 ns/op，且零内存分配。

### Q2: 如何调试降级逻辑？

A: 使用 ForceLevel 方法手动设置降级级别进行测试：

```go
deg.ForceLevel(middleware.LevelModerate)
// 测试业务逻辑
deg.ForceLevel(middleware.LevelNormal)
```

### Q3: 可以禁用系统监控吗？

A: 可以。不调用 `StartMonitor()` 即可禁用自动监控，仅通过 `ForceLevel()` 手动控制。

### Q4: 降级会丢失事件吗？

A: 是的，在 Drop 策略下，低优先级事件会被直接丢弃。如果不希望丢失事件，请使用 Delay 或 Simplify 策略。

### Q5: 如何与配置系统集成？

A: 可以从配置文件读取降级参数：

```yaml
degradation:
  enabled: true
  cpu_threshold: 80.0
  memory_threshold: 85.0
  strategy: drop
```

```go
cfg := loadConfig()
if cfg.Degradation.Enabled {
    deg := middleware.NewAdaptiveDegradation(middleware.DegradationConfig{
        CPUThreshold:    cfg.Degradation.CPUThreshold,
        MemoryThreshold: cfg.Degradation.MemoryThreshold,
        Strategy:        parseStrategy(cfg.Degradation.Strategy),
    })
    go deg.StartMonitor(context.Background())
    engine.Use(deg.Middleware())
}
```

## 相关文档

- [中间件系统](MIDDLEWARE.md)
- [熔断器](CIRCUIT_BREAKER.md)
- [并发控制](CONCURRENCY.md)
- [性能优化](PERFORMANCE.md)

## 更新日志

### v1.3.0 (2025-12-08)
- ✅ 新增优雅降级机制
- ✅ 支持 4 级降级策略
- ✅ 支持 3 种降级模式
- ✅ 自动系统监控
- ✅ 零内存分配实现

