# 配置系统改进文档

## 概述

本文档总结了项目中发现的魔数（Magic Numbers）和配置改进建议。这些魔数目前硬编码在代码中，应该从配置文件中获取，以提高灵活性和可维护性。

## 发现的魔数和改进建议

### 1. Webhook 适配器配置

#### 当前问题
- **文件**: `webhook_adapter.go`
- **魔数**: 
  - `bufferSize = 100` (默认 webhook 事件缓冲区大小)
  - `workers = runtime.NumCPU()` (默认并发处理器数量)

#### 改进建议
```yaml
webhook:
  event_buffer: 100        # Webhook 事件通道缓冲大小
  worker_count: 0          # 并发事件处理的 worker 数量，0 表示使用 CPU 核心数
```

**影响**: 高吞吐量场景下，这两个参数对性能影响巨大。根据测试，8 并发可达到 6127 msg/s 吞吐量。

---

### 2. Token 管理器配置

#### 当前问题
- **文件**: `openapi/auth/token/token.go`
- **魔数**:
  - `retryDelay = 10 * time.Second` (Token 获取失败重试延迟)
  - `refreshAfter = expiresIn - 30` (提前 30 秒刷新 Token)
  - 最小刷新时间为 `expiresIn / 2`

#### 改进建议
```yaml
token:
  retry_delay: "10s"           # Token 获取失败重试延迟
  refresh_advance: "30s"       # 提前多久刷新 Token
  min_refresh_ratio: 0.5       # 最小刷新时间比例（expires_in * ratio）
```

**影响**: Token 刷新策略影响 API 调用的可用性和稳定性。

---

### 3. BigCache 去重配置

#### 当前问题
- **文件**: `openapi/protocol/webhook/webhook.go`
- **魔数**:
  - `Shards: 1024` (BigCache 分片数)
  - `LifeWindow: 5 * time.Minute` (去重窗口时长)
  - `CleanWindow: 1 * time.Minute` (清理间隔)
  - `MaxEntrySize: 4096` (单个条目最大字节数)
  - `HardMaxCacheSize: 1024` (MB) (缓存最大内存限制)
  - `MaxEntriesInWindow: 1000 * 10 * 60` (最大条目数)

#### 改进建议
```yaml
webhook:
  dedup_enable: true
  dedup_shards: 1024                    # BigCache 分片数（建议 2 的幂次）
  dedup_life_window: "5m"               # 去重缓存生命周期
  dedup_clean_window: "1m"              # 清理过期条目的间隔时间
  dedup_max_entry_size: 4096            # 单个缓存条目的最大字节数
  dedup_hard_max_size: 1024             # 最大缓存大小（MB）
  dedup_max_entries_in_window: 600000   # 最大条目数（默认 1000*10*60）
```

**当前状态**: ✅ 已在 `config.example.yaml` 中配置，但部分参数（如 `max_entries_in_window`）未暴露。

---

### 4. Engine 核心配置

#### 当前问题
- **文件**: `core/engine/config.go`
- **魔数**:
  - `DefaultTempMatcherCleanerInterval = 5 * time.Minute` (临时 Matcher 清理间隔)
  - `DefaultPendingDeleteBufferSize = 1000` (批量删除通道大小)
  - `DefaultMatcherPoolCapacity = 16` (Matcher 池初始容量)
  - `MaxMatcherPoolRetainCapacity = 1024` (Matcher 池最大容量)
  - `DefaultPendingDeleteProcessInterval = 100 * time.Millisecond` (批量删除处理间隔)
  - `DefaultPendingDeleteBatchSize = 1000` (每次批量删除数量)
  - `tempMatcherShardCount = 8` (临时 Matcher 分片数)

#### 改进建议
```yaml
engine:
  temp_matcher_cleanup_interval: "5m"       # 临时 Matcher 清理间隔
  pending_delete_buffer_size: 1000          # 批量删除通道大小
  matcher_pool_capacity: 16                 # Matcher 池初始容量
  matcher_pool_max_capacity: 1024           # Matcher 池最大容量
  pending_delete_process_interval: "100ms"  # 批量删除处理间隔
  pending_delete_batch_size: 1000           # 每次批量删除数量
  temp_matcher_shard_count: 8               # 临时 Matcher 分片数
```

**影响**: 这些参数影响 Engine 的内存使用和性能表现。

---

### 5. 中间件配置

#### 5.1 限流中间件
- **文件**: `middleware/middleware.go`
- **魔数**:
  - `rateLimitBucketTTL = 10 * time.Minute` (限流桶过期时间)
  - `rateLimitCleanupInterval = 5 * time.Minute` (限流桶清理间隔)

#### 5.2 去重中间件
- **文件**: `middleware/dedup.go`
- **魔数**:
  - `DefaultTTL: 5 * time.Minute` (默认去重 TTL)
  - `CleanupInterval: 1 * time.Minute` (清理间隔)

#### 5.3 慢处理器中间件
- **文件**: `middleware/slow_handler.go`
- **魔数**:
  - `Threshold: 1 * time.Second` (慢处理阈值)

#### 5.4 重试中间件
- **文件**: `middleware/retry.go`
- **魔数**:
  - `BackoffMax: 2 * time.Second` (最大重试退避时间)

#### 改进建议
```yaml
middleware:
  # 限流中间件
  rate_limit: true
  rate_limit_rate: 100                  # 每秒生成的令牌数
  rate_limit_burst: 200                 # 令牌桶容量
  rate_limit_bucket_ttl: "10m"          # 限流桶过期时间
  rate_limit_cleanup_interval: "5m"     # 限流桶清理间隔
  
  # 去重中间件
  dedup_enable: true
  dedup_default_ttl: "5m"               # 默认去重 TTL
  dedup_cleanup_interval: "1m"          # 清理间隔
  
  # 慢处理器中间件
  slow_handler_enable: false
  slow_handler_threshold: "1s"          # 慢处理阈值
  
  # 重试中间件（已存在部分配置）
  retry_backoff_max: "2s"               # 最大重试退避时间
```

**当前状态**: ⚠️ 部分配置已存在，但缺少新增的时间参数。

---

### 6. 自适应降级配置

#### 当前问题
- **文件**: `middleware/degradation.go`
- **魔数**:
  - `CPUThreshold: 80.0` (CPU 使用率阈值)
  - `MemoryThreshold: 85.0` (内存使用率阈值)
  - `LatencyThreshold: 500 * time.Millisecond` (延迟阈值)
  - `MonitorInterval: 5 * time.Second` (监控间隔)
  - `RecoveryInterval: 10 * time.Second` (恢复检查间隔)
  - `DelayQueueSize: 1000` (延迟队列大小)
  - `GoroutineThreshold: 10000` (协程数量阈值)

#### 改进建议
```yaml
degradation:
  enable: false                         # 是否启用自适应降级
  cpu_threshold: 80.0                   # CPU 使用率阈值（%）
  memory_threshold: 85.0                # 内存使用率阈值（%）
  latency_threshold: "500ms"            # 延迟阈值
  monitor_interval: "5s"                # 监控间隔
  recovery_interval: "10s"              # 恢复检查间隔
  delay_queue_size: 1000                # 延迟队列大小
  goroutine_threshold: 10000            # 协程数量阈值
  strategy: "drop"                      # 降级策略: drop, delay, simplify
```

**影响**: 自适应降级是保护系统稳定性的重要机制，配置化后可以根据实际场景调整。

---

## 优先级评估

### 高优先级 ⚠️ (必须配置化)
1. **Webhook 配置** (`event_buffer`, `worker_count`)
   - 原因: 直接影响消息接收性能和吞吐量
   - 影响: 测试显示 8 并发可达 6127 msg/s，配置不当会导致消息丢失

2. **Token 管理器配置** (`retry_delay`, `refresh_advance`)
   - 原因: 影响 API 调用的可用性
   - 影响: Token 过期会导致所有 API 调用失败

3. **Engine 核心配置** (`pending_delete_buffer_size`, `matcher_pool_capacity`)
   - 原因: 影响内存使用和性能
   - 影响: 配置不当可能导致内存泄漏或性能下降

### 中优先级 ⚙️ (建议配置化)
4. **BigCache 去重配置** (已部分配置)
   - 原因: 防止重复消息处理
   - 影响: 配置不当会导致内存占用过高或去重失效

5. **中间件时间参数** (`rate_limit_bucket_ttl`, `dedup_cleanup_interval`)
   - 原因: 影响中间件的行为和资源占用
   - 影响: 中等，但在高流量场景下需要调优

### 低优先级 ℹ️ (可选配置化)
6. **自适应降级配置**
   - 原因: 高级功能，默认禁用
   - 影响: 仅在需要时启用

7. **慢处理器配置**
   - 原因: 调试和监控功能
   - 影响: 对核心功能无影响

---

## 配置文件结构建议

### 完整的 `config.example.yaml` 结构

```yaml
# ====================================
# Bot 基础配置（必填）
# ====================================
bot:
  app_id: 123456789
  bot_id: 987654321
  token: "your_bot_token_here"
  secret: "your_bot_secret_here"

# ====================================
# 服务器配置
# ====================================
server:
  host: "0.0.0.0"
  port: 8080

# ====================================
# 日志配置
# ====================================
log:
  level: "info"
  format: "text"

# ====================================
# Engine 引擎配置
# ====================================
engine:
  # 临时 Matcher 清理间隔
  temp_matcher_cleanup_interval: "5m"
  
  # 批量删除配置
  pending_delete_buffer_size: 1000
  pending_delete_process_interval: "100ms"
  pending_delete_batch_size: 1000
  
  # Matcher 池配置
  matcher_pool_capacity: 16
  matcher_pool_max_capacity: 1024
  
  # 临时 Matcher 分片数
  temp_matcher_shard_count: 8

# ====================================
# Webhook 配置
# ====================================
webhook:
  # 事件通道缓冲大小（高优先级）
  event_buffer: 1000
  
  # 并发事件处理的 worker 数量（0=CPU核心数）
  worker_count: 0
  
  # 去重配置
  dedup_enable: true
  dedup_shards: 1024
  dedup_life_window: "5m"
  dedup_clean_window: "1m"
  dedup_max_entry_size: 4096
  dedup_hard_max_size: 1024
  dedup_max_entries_in_window: 600000

# ====================================
# Token 管理器配置
# ====================================
token:
  # Token 获取失败重试延迟
  retry_delay: "10s"
  
  # 提前多久刷新 Token
  refresh_advance: "30s"
  
  # 最小刷新时间比例（expires_in * ratio）
  min_refresh_ratio: 0.5

# ====================================
# 并发控制配置
# ====================================
concurrency:
  limit: 500
  policy: "trywait"
  wait_timeout: "500ms"

# ====================================
# 重试配置
# ====================================
retry:
  enable: true
  max_attempts: 3
  backoff_base: "200ms"
  backoff_max: "2s"

# ====================================
# 中间件配置
# ====================================
middleware:
  # 日志中间件
  logging: true
  
  # 恢复中间件
  recover: true
  
  # 认证中间件
  auth: false
  auth_whitelist: []
  
  # 限流中间件
  rate_limit: false
  rate_limit_rate: 100
  rate_limit_burst: 200
  rate_limit_bucket_ttl: "10m"
  rate_limit_cleanup_interval: "5m"
  
  # 去重中间件
  dedup_enable: false
  dedup_default_ttl: "5m"
  dedup_cleanup_interval: "1m"
  
  # 慢处理器中间件
  slow_handler_enable: false
  slow_handler_threshold: "1s"
  
  # 指标收集中间件
  metrics: true

# ====================================
# 自适应降级配置（高级功能）
# ====================================
degradation:
  enable: false
  cpu_threshold: 80.0
  memory_threshold: 85.0
  latency_threshold: "500ms"
  monitor_interval: "5s"
  recovery_interval: "10s"
  delay_queue_size: 1000
  goroutine_threshold: 10000
  strategy: "drop"

# ====================================
# 死信队列配置
# ====================================
dead_letter:
  enable: true
  target: "file"
  file_path: "./dead_letters.log"
```

---

## 实施计划

### 第一阶段：高优先级配置（必须完成）
1. ✅ 更新 `config/config.go`，添加新的配置结构
2. ✅ 更新 `config.example.yaml`，添加所有配置项
3. 🔄 修改 `webhook_adapter.go`，从配置读取参数
4. 🔄 修改 `openapi/auth/token/token.go`，从配置读取参数
5. 🔄 修改 `core/engine/`，从配置读取参数

### 第二阶段：中优先级配置（建议完成）
6. 🔄 完善 BigCache 配置项（添加 `max_entries_in_window`）
7. 🔄 修改中间件代码，从配置读取时间参数

### 第三阶段：低优先级配置（可选）
8. 🔄 添加自适应降级配置支持
9. 🔄 添加慢处理器配置支持

---

## 代码示例

### 使用配置的示例代码

```go
// 从配置创建 Webhook 适配器
cfg := config.Get()
adapter := remilia.NewWebhookServerAdapterWithConfig(":9000", global.Info, cfg.Webhook)

// 从配置创建 Token Manager
tokenMgr := token.NewManagerWithConfig(global.Info, cfg.Token)

// 从配置创建 Engine
engine := engine.NewEngine(
    engine.WithCleanupInterval(cfg.Engine.TempMatcherCleanupInterval),
    engine.WithPendingDeleteBufferSize(cfg.Engine.PendingDeleteBufferSize),
)
```

---

## 测试建议

1. **性能测试**: 测试不同 `worker_count` 和 `event_buffer` 配置下的吞吐量
2. **压力测试**: 测试在高并发下配置的稳定性
3. **内存测试**: 测试不同配置下的内存占用
4. **兼容性测试**: 确保配置缺失时使用合理的默认值

---

## 注意事项

1. **向后兼容**: 所有新增配置项必须有合理的默认值
2. **配置验证**: 添加配置验证逻辑，防止无效配置
3. **文档更新**: 更新用户文档，说明新增的配置项
4. **环境变量**: 考虑支持通过环境变量覆盖配置
5. **热重载**: 部分配置支持热重载（如日志级别、限流参数）

---

## 总结

通过配置化这些魔数，可以实现：
- ✅ 提高系统的灵活性和可维护性
- ✅ 便于不同环境下的调优
- ✅ 降低代码修改的成本
- ✅ 提升系统的可观测性和可控性

**预期收益**：
- 性能调优更简单（无需重新编译）
- 生产环境快速响应问题
- 降低配置错误的风险
- 提升用户体验

---

*文档版本: v1.0*  
*生成时间: 2026-01-24*  
*作者: GitHub Copilot*
