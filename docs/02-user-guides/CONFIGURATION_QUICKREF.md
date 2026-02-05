# 配置系统快速参考

## 如何使用新增的配置项

### 1. 基础使用

```go
package main

import (
    "github.com/KomeiDiSanXian/remilia/config"
    "github.com/sirupsen/logrus"
)

func main() {
    // 加载配置
    cfg, err := config.LoadDefault()
    if err != nil {
        logrus.Fatalf("Failed to load config: %v", err)
    }
    
    // 访问配置
    logrus.Infof("Webhook workers: %d", cfg.Webhook.WorkerCount)
    logrus.Infof("Event buffer: %d", cfg.Webhook.EventBuffer)
    logrus.Infof("Token retry delay: %s", cfg.Token.RetryDelay)
}
```

### 2. 性能关键配置

#### Webhook 并发配置

```yaml
webhook:
  # 并发处理器数量（0 = CPU 核心数）
  # 测试数据：8 并发可达 6127 msg/s
  worker_count: 8
  
  # 事件缓冲区大小
  # 推荐：高流量场景 1000-5000
  event_buffer: 2000
```

```go
// 代码中访问
workers := cfg.Webhook.WorkerCount
if workers == 0 {
    workers = runtime.NumCPU()
}
buffer := cfg.Webhook.EventBuffer
if buffer <= 0 {
    buffer = 100 // 默认值
}
```

#### Token 管理配置

```yaml
token:
  retry_delay: "10s"        # 获取失败重试延迟
  refresh_advance: "30s"    # 提前刷新时间
  min_refresh_ratio: 0.5    # 最小刷新比例
```

```go
import "time"

// 解析配置
retryDelay, _ := time.ParseDuration(cfg.Token.RetryDelay)
refreshAdvance, _ := time.ParseDuration(cfg.Token.RefreshAdvance)
```

#### Engine 配置

```yaml
engine:
  temp_matcher_cleanup_interval: "5m"
  pending_delete_buffer_size: 1000
  matcher_pool_capacity: 16
```

```go
import "time"

// 使用配置创建 Engine
cleanupInterval, _ := time.ParseDuration(cfg.Engine.TempMatcherCleanupInterval)
engine := engine.NewEngine(
    engine.WithCleanupInterval(cleanupInterval),
    engine.WithPendingDeleteBufferSize(cfg.Engine.PendingDeleteBufferSize),
)
```

### 3. 中间件配置

#### 限流配置

```yaml
middleware:
  rate_limit: true
  rate_limit_rate: 100
  rate_limit_burst: 200
  rate_limit_bucket_ttl: "10m"
  rate_limit_cleanup_interval: "5m"
```

#### 去重配置

```yaml
middleware:
  dedup_enable: true
  dedup_default_ttl: "5m"
  dedup_cleanup_interval: "1m"
```

#### 慢处理器配置

```yaml
middleware:
  slow_handler_enable: true
  slow_handler_threshold: "1s"  # 超过 1 秒记录警告
```

### 4. 高级功能：自适应降级

```yaml
degradation:
  enable: true
  cpu_threshold: 80.0           # CPU 超过 80% 开始降级
  memory_threshold: 85.0        # 内存超过 85% 开始降级
  latency_threshold: "500ms"    # 延迟超过 500ms 开始降级
  monitor_interval: "5s"        # 每 5 秒检查一次
  strategy: "drop"              # 降级策略：drop/delay/simplify
```

```go
// 使用降级配置
if cfg.Degradation.Enable {
    deg := middleware.NewAdaptiveDegradation(middleware.DegradationConfig{
        CPUThreshold:    cfg.Degradation.CPUThreshold,
        MemoryThreshold: cfg.Degradation.MemoryThreshold,
        // ... 其他配置
    })
    go deg.StartMonitor(ctx)
    engine.Use(deg.Middleware())
}
```

## 配置优先级

1. **显式配置文件** (config.yaml)
2. **环境变量** (BOT_APP_ID, BOT_TOKEN 等)
3. **默认值** (代码中定义)

## 配置验证

所有配置在加载时都会进行验证：

```go
cfg, err := config.Load("config.yaml")
if err != nil {
    // 配置无效，err 包含详细错误信息
    logrus.Fatalf("Invalid config: %v", err)
}
// 配置有效，可以安全使用
```

## 常见配置场景

### 场景 1：低流量场景（个人 Bot）

```yaml
webhook:
  event_buffer: 100
  worker_count: 2
  
engine:
  pending_delete_buffer_size: 100
  matcher_pool_capacity: 8
```

### 场景 2：中等流量场景（小型企业 Bot）

```yaml
webhook:
  event_buffer: 1000
  worker_count: 4
  
engine:
  pending_delete_buffer_size: 1000
  matcher_pool_capacity: 16
```

### 场景 3：高流量场景（大型企业 Bot）

```yaml
webhook:
  event_buffer: 5000
  worker_count: 16
  
engine:
  pending_delete_buffer_size: 10000
  matcher_pool_capacity: 64
  matcher_pool_max_capacity: 4096

degradation:
  enable: true
  cpu_threshold: 75.0
  memory_threshold: 80.0
```

### 场景 4：极限性能场景

```yaml
webhook:
  event_buffer: 10000
  worker_count: 32  # 或使用 0 自动使用 CPU 核心数
  dedup_enable: false  # 为了性能可以禁用去重
  
engine:
  temp_matcher_cleanup_interval: "10m"  # 减少清理频率
  pending_delete_buffer_size: 50000
  pending_delete_process_interval: "50ms"  # 更频繁地批量删除
  
middleware:
  logging: false  # 禁用日志以提升性能
```

## 配置调优建议

### 1. Worker Count 调优

| 场景 | 推荐值 | 说明 |
|------|--------|------|
| CPU 密集型 | CPU 核心数 | worker_count: 0 (自动) |
| IO 密集型 | CPU 核心数 × 2 | worker_count: 16 (8核) |
| 混合场景 | CPU 核心数 × 1.5 | worker_count: 12 (8核) |

### 2. Buffer Size 调优

| 流量级别 | 推荐值 | 说明 |
|----------|--------|------|
| < 100 msg/s | 100-500 | 小缓冲即可 |
| 100-1000 msg/s | 1000-2000 | 中等缓冲 |
| > 1000 msg/s | 5000-10000 | 大缓冲，防止丢失 |

### 3. Engine 调优

```yaml
# 高频创建临时 Matcher
engine:
  temp_matcher_cleanup_interval: "2m"  # 更频繁清理
  pending_delete_buffer_size: 5000     # 更大的删除缓冲

# 低频创建临时 Matcher
engine:
  temp_matcher_cleanup_interval: "10m" # 减少清理开销
  pending_delete_buffer_size: 500      # 小缓冲节省内存
```

## 故障排查

### 问题 1：消息丢失

**现象**：日志显示 "Event channel is full, dropping payload"

**解决方案**：
```yaml
webhook:
  event_buffer: 5000  # 增大缓冲区
  worker_count: 16    # 增加并发处理
```

### 问题 2：内存占用过高

**现象**：内存持续增长

**解决方案**：
```yaml
engine:
  temp_matcher_cleanup_interval: "2m"  # 更频繁清理
  matcher_pool_max_capacity: 512       # 限制池大小

webhook:
  dedup_hard_max_size: 50  # 减小去重缓存
```

### 问题 3：CPU 占用过高

**现象**：CPU 持续 100%

**解决方案**：
```yaml
webhook:
  worker_count: 4  # 减少并发数

middleware:
  logging: false  # 禁用日志中间件

degradation:
  enable: true    # 启用自适应降级
  cpu_threshold: 70.0
```

### 问题 4：Token 频繁失效

**现象**：API 调用频繁失败

**解决方案**：
```yaml
token:
  refresh_advance: "60s"  # 提前更多时间刷新
  retry_delay: "5s"       # 减少重试延迟
```

## 环境变量覆盖

所有配置都可以通过环境变量覆盖：

```bash
# Bot 配置
export BOT_APP_ID=123456789
export BOT_BOT_ID=987654321
export BOT_TOKEN="your_token"
export BOT_SECRET="your_secret"

# 服务器配置
export SERVER_HOST="0.0.0.0"
export SERVER_PORT=8080

# 日志配置
export LOG_LEVEL="debug"
export LOG_FORMAT="json"
```

## 配置热重载

某些配置支持热重载（无需重启）：

```go
import "github.com/KomeiDiSanXian/remilia/config"

// 监听配置变化
watcher, err := config.NewWatcher("config.yaml")
if err != nil {
    panic(err)
}

watcher.OnReload(func(oldCfg, newCfg *config.Config) error {
    // 配置已更新，可以在这里更新运行时配置
    logrus.Infof("Config reloaded: workers %d -> %d", 
        oldCfg.Webhook.WorkerCount, 
        newCfg.Webhook.WorkerCount)
    return nil
})

// 启动监听
if err := watcher.Start(); err != nil {
    panic(err)
}
```

## 相关文档

- [配置改进详细分析](./CONFIGURATION_IMPROVEMENTS.md)
- [配置改进总结](./CONFIGURATION_SUMMARY.md)
- [配置示例文件](../config.example.yaml)
- [性能测试报告](./PERFORMANCE_TEST_REPORT.md) (如果存在)

---

*最后更新: 2026-01-24*
