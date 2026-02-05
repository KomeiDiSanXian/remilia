# 配置系统迁移指南

## 概述

本指南帮助现有用户从旧版配置迁移到新版配置系统（v0.7.0+）。

## 向后兼容性

✅ **好消息**：所有新增配置项都是**可选的**，并有合理的默认值。

- 现有的 `config.yaml` **无需修改**即可继续使用
- 所有新增配置项都会使用默认值
- 代码行为保持一致（除非你显式配置）

## 迁移步骤

### 步骤 1：了解新增配置项

查看新增的配置项：

1. **Webhook 配置**
   - `worker_count` - 并发处理器数量
   - `dedup_max_entries_in_window` - BigCache 最大条目数

2. **Token 管理器配置**（全新）
   - `token.retry_delay`
   - `token.refresh_advance`
   - `token.min_refresh_ratio`

3. **Engine 配置**（全新）
   - `engine.temp_matcher_cleanup_interval`
   - `engine.pending_delete_buffer_size`
   - 等等...

4. **Degradation 配置**（全新）
   - 自适应降级功能（默认禁用）

5. **Middleware 配置**
   - 新增多个时间参数配置

### 步骤 2：（可选）更新 config.yaml

如果你想使用新功能，可以逐步添加配置：

#### 最小更新（推荐）

只添加你需要调整的配置：

```yaml
# 现有配置保持不变
bot:
  app_id: 123456789
  # ...

# 新增：如果你想调整并发数
webhook:
  worker_count: 8  # 0 表示使用 CPU 核心数
```

#### 完整更新

复制 `config.example.yaml` 的新增部分到你的 `config.yaml`：

```bash
# 1. 备份现有配置
cp config.yaml config.yaml.backup

# 2. 查看示例配置的新增部分
# 从 config.example.yaml 复制你需要的部分

# 3. 合并到你的 config.yaml
```

### 步骤 3：验证配置

```bash
# 使用你的应用验证配置
go run ./examples/test/main.go

# 或者编写简单的验证程序
```

```go
package main

import (
    "github.com/KomeiDiSanXian/remilia/config"
    "github.com/sirupsen/logrus"
)

func main() {
    cfg, err := config.LoadDefault()
    if err != nil {
        logrus.Fatalf("Config validation failed: %v", err)
    }
    logrus.Infof("✅ Config is valid")
    logrus.Infof("Webhook workers: %d", cfg.Webhook.WorkerCount)
    logrus.Infof("Event buffer: %d", cfg.Webhook.EventBuffer)
}
```

## 默认值对照表

所有新增配置项的默认值：

| 配置项 | 默认值 | 说明 |
|--------|--------|------|
| `webhook.worker_count` | `0` | 0 表示使用 CPU 核心数 |
| `webhook.dedup_max_entries_in_window` | `600000` | BigCache 默认值 |
| `token.retry_delay` | `"10s"` | Token 获取失败重试延迟 |
| `token.refresh_advance` | `"30s"` | 提前刷新 Token |
| `token.min_refresh_ratio` | `0.5` | 最小刷新时间比例 |
| `engine.temp_matcher_cleanup_interval` | `"5m"` | 临时 Matcher 清理间隔 |
| `engine.pending_delete_buffer_size` | `1000` | 批量删除缓冲区大小 |
| `engine.pending_delete_process_interval` | `"100ms"` | 批量删除处理间隔 |
| `engine.pending_delete_batch_size` | `1000` | 每次批量删除数量 |
| `engine.matcher_pool_capacity` | `16` | Matcher 池初始容量 |
| `engine.matcher_pool_max_capacity` | `1024` | Matcher 池最大容量 |
| `engine.temp_matcher_shard_count` | `8` | 临时 Matcher 分片数 |
| `middleware.rate_limit_bucket_ttl` | `"10m"` | 限流桶过期时间 |
| `middleware.rate_limit_cleanup_interval` | `"5m"` | 限流桶清理间隔 |
| `middleware.dedup_default_ttl` | `"5m"` | 默认去重 TTL |
| `middleware.dedup_cleanup_interval` | `"1m"` | 去重清理间隔 |
| `middleware.slow_handler_threshold` | `"1s"` | 慢处理阈值 |
| `degradation.enable` | `false` | 自适应降级默认禁用 |

## 常见迁移场景

### 场景 1：我只想提升性能

**问题**：当前吞吐量不够

**解决方案**：只添加 webhook 配置

```yaml
# 在你的 config.yaml 中添加或修改
webhook:
  worker_count: 8     # 根据你的 CPU 核心数调整
  event_buffer: 2000  # 增大缓冲区
```

### 场景 2：我遇到 Token 刷新问题

**问题**：Token 经常过期导致 API 调用失败

**解决方案**：添加 token 配置

```yaml
# 在你的 config.yaml 中添加
token:
  refresh_advance: "60s"  # 提前 60 秒刷新
  retry_delay: "5s"       # 失败后 5 秒重试
```

### 场景 3：我需要启用自适应降级

**问题**：高峰期服务器过载

**解决方案**：添加 degradation 配置

```yaml
# 在你的 config.yaml 中添加
degradation:
  enable: true
  cpu_threshold: 80.0
  memory_threshold: 85.0
  strategy: "drop"
```

### 场景 4：我想优化内存使用

**问题**：Bot 内存占用过高

**解决方案**：调整 engine 和 webhook 配置

```yaml
# 在你的 config.yaml 中添加或修改
engine:
  temp_matcher_cleanup_interval: "2m"  # 更频繁清理
  matcher_pool_max_capacity: 512       # 限制池大小

webhook:
  dedup_hard_max_size: 50  # 减小去重缓存（MB）
```

## 配置迁移检查清单

在迁移配置后，检查以下项：

- [ ] 配置文件语法正确（YAML 格式）
- [ ] 必填字段（bot.*）仍然存在
- [ ] 新增配置项的值在有效范围内
- [ ] 时间格式正确（如 "5m", "30s", "100ms"）
- [ ] 数值配置合理（如 worker_count > 0）
- [ ] 应用可以正常启动
- [ ] 运行时行为符合预期
- [ ] 性能指标正常（如吞吐量、延迟）

## 测试你的配置

### 测试 1：配置验证

```go
package main

import (
    "github.com/KomeiDiSanXian/remilia/config"
    "log"
)

func main() {
    cfg, err := config.Load("config.yaml")
    if err != nil {
        log.Fatalf("❌ Config error: %v", err)
    }
    log.Println("✅ Config is valid")
    
    // 打印关键配置
    log.Printf("Workers: %d", cfg.Webhook.WorkerCount)
    log.Printf("Buffer: %d", cfg.Webhook.EventBuffer)
    log.Printf("Token retry: %s", cfg.Token.RetryDelay)
}
```

### 测试 2：性能测试

```go
// 测试不同 worker_count 的性能影响
// 参考 examples/benchmark/ 中的性能测试工具
```

### 测试 3：压力测试

```bash
# 使用高负载测试新配置
# 监控 CPU、内存、消息处理延迟等指标
```

## 回滚指南

如果新配置导致问题，可以快速回滚：

### 方法 1：恢复备份

```bash
# 恢复之前的配置
cp config.yaml.backup config.yaml

# 重启应用
```

### 方法 2：移除新增配置

```yaml
# 删除 config.yaml 中的以下部分
# token: ...
# engine: ...
# degradation: ...

# webhook 中删除新增字段
# webhook:
#   worker_count: ...  # 删除此行
```

### 方法 3：使用默认值

```yaml
# 将问题配置项设置为 0 或空字符串，将使用默认值
webhook:
  worker_count: 0  # 使用默认值（CPU 核心数）
```

## 疑难解答

### 问题 1：配置加载失败

**错误信息**：`failed to parse config file`

**原因**：YAML 格式错误

**解决方案**：
1. 检查缩进（YAML 使用空格，不是 Tab）
2. 检查引号匹配
3. 使用在线 YAML 验证器验证

### 问题 2：配置验证失败

**错误信息**：`invalid webhook config: webhook.worker_count must be >= 0`

**原因**：配置值超出有效范围

**解决方案**：
1. 查看错误信息，找到具体字段
2. 参考 `config.example.yaml` 中的推荐值
3. 检查数据类型（整数 vs 字符串）

### 问题 3：时间格式错误

**错误信息**：`is not a valid duration`

**原因**：时间格式不正确

**解决方案**：
```yaml
# ❌ 错误
retry_delay: 10s       # 缺少引号
retry_delay: "10 s"    # 空格错误

# ✅ 正确
retry_delay: "10s"
retry_delay: "5m"
retry_delay: "100ms"
```

### 问题 4：性能下降

**现象**：更新配置后性能下降

**排查步骤**：
1. 检查 `worker_count` 是否过小
2. 检查 `event_buffer` 是否过小
3. 查看日志是否有 "dropping payload" 警告
4. 对比更新前后的配置差异

**解决方案**：
```yaml
# 增大并发和缓冲
webhook:
  worker_count: 16     # 增大
  event_buffer: 5000   # 增大
```

## 获取帮助

如果遇到配置问题：

1. **查看文档**
   - [CONFIGURATION_QUICKREF.md](./CONFIGURATION_QUICKREF.md) - 快速参考
   - [CONFIGURATION_IMPROVEMENTS.md](./CONFIGURATION_IMPROVEMENTS.md) - 详细说明

2. **查看示例**
   - [config.example.yaml](../config.example.yaml) - 完整示例配置

3. **查看测试**
   - [config/config_test.go](../config/config_test.go) - 配置验证测试

4. **提交 Issue**
   - 提供你的配置文件（隐藏敏感信息）
   - 提供错误日志
   - 描述预期行为 vs 实际行为

## 总结

✅ **迁移要点**：
- 所有新配置项都是可选的
- 向后兼容，无需立即迁移
- 可以逐步添加需要的配置
- 遇到问题可以快速回滚

✅ **推荐步骤**：
1. 先在测试环境验证
2. 逐步添加配置项
3. 监控性能指标
4. 根据实际情况调优

✅ **记住**：
- 备份现有配置
- 验证配置有效性
- 监控运行时行为
- 根据需求调整

---

*最后更新: 2026-01-24*  
*适用版本: v0.7.0+*
