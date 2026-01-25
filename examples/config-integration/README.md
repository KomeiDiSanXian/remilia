# 配置集成示例

本目录包含展示如何使用新的配置系统的示例代码。

## 概述

从 v0.7.0+ 开始，Remilia Bot 框架支持通过配置文件控制各个组件的行为。本示例展示如何：

1. 从配置文件加载配置
2. 使用配置创建 Token Manager
3. 使用配置创建 Engine
4. 使用配置创建 Webhook Adapter
5. 启动和优雅关闭 Bot

## 前置要求

1. 复制 `config.example.yaml` 为 `config.yaml`
2. 填入你的 Bot 信息（app_id, bot_id, token, secret）
3. 根据需要调整配置参数

## 运行示例

```bash
# 确保在项目根目录
cd E:\project\Go\remilia

# 复制配置文件
copy config.example.yaml config.yaml

# 编辑 config.yaml，填入你的 Bot 信息

# 运行示例
go run examples/config-integration/main.go
```

## 配置说明

### 关键配置项

#### Webhook 配置
```yaml
webhook:
  # 并发处理器数量（0 = CPU 核心数）
  worker_count: 8
  
  # 事件缓冲区大小
  event_buffer: 2000
```

**性能影响**：
- worker_count=8 可达约 6127 msg/s 吞吐量
- event_buffer 过小会导致消息丢失

#### Token 配置
```yaml
token:
  # Token 获取失败重试延迟
  retry_delay: "10s"
  
  # 提前多久刷新 Token
  refresh_advance: "30s"
  
  # 最小刷新时间比例
  min_refresh_ratio: 0.5
```

**稳定性影响**：
- retry_delay 控制失败重试间隔
- refresh_advance 防止 Token 过期导致的 API 调用失败

#### Engine 配置
```yaml
engine:
  # 临时 Matcher 清理间隔
  temp_matcher_cleanup_interval: "5m"
  
  # 批量删除缓冲区大小
  pending_delete_buffer_size: 1000
  
  # 批量删除处理间隔
  pending_delete_process_interval: "100ms"
  
  # 每次批量删除数量
  pending_delete_batch_size: 1000
  
  # Matcher 池初始容量
  matcher_pool_capacity: 16
  
  # Matcher 池最大容量
  matcher_pool_max_capacity: 1024
```

**内存和性能影响**：
- cleanup_interval 控制内存清理频率
- buffer_size 和 batch_size 影响批量删除效率

## 代码示例

### 1. 加载配置

```go
import "github.com/KomeiDiSanXian/remilia/config"

// 加载配置（按优先级：config.yaml -> config.yml -> 环境变量）
cfg, err := config.LoadDefault()
if err != nil {
    logrus.Fatalf("Failed to load config: %v", err)
}
```

### 2. 使用配置创建 Token Manager

```go
import "github.com/KomeiDiSanXian/remilia/openapi/auth/token"

// 使用配置创建 Token Manager
tokenMgr := token.NewManagerWithConfig(botInfo, cfg.Token)
defer tokenMgr.Stop()

// 等待 Token 就绪
tokenMgr.WaitReady()
```

### 3. 使用配置创建 Engine

```go
import "github.com/KomeiDiSanXian/remilia/core/engine"

// 使用配置创建 Engine
eng := engine.NewEngine(engine.WithConfig(cfg.Engine))
defer eng.Shutdown(context.Background())
```

### 4. 使用配置创建 Webhook Adapter

```go
import "github.com/KomeiDiSanXian/remilia"

// 使用配置创建 Webhook Adapter
adapter := remilia.NewWebhookServerAdapterWithConfig(
    fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
    botInfo,
    cfg.Webhook,
)
```

### 5. 创建和启动 Bot

```go
// 创建 Bot
bot := remilia.NewBot(adapter, eng)

// 启动 Bot
if err := bot.Start(); err != nil {
    logrus.Fatalf("Failed to start bot: %v", err)
}

// 优雅关闭
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
bot.Shutdown(ctx)
```

## 向后兼容性

所有新的配置功能都是可选的，现有代码无需修改即可继续使用：

```go
// 旧的方式（仍然支持）
adapter := remilia.NewWebhookServerAdapter(":8080", botInfo)
tokenMgr := token.NewManager(botInfo)
eng := engine.NewEngine()

// 新的方式（推荐）
adapter := remilia.NewWebhookServerAdapterWithConfig(":8080", botInfo, cfg.Webhook)
tokenMgr := token.NewManagerWithConfig(botInfo, cfg.Token)
eng := engine.NewEngine(engine.WithConfig(cfg.Engine))
```

## 性能调优建议

### 低流量场景（< 100 msg/s）
```yaml
webhook:
  worker_count: 2
  event_buffer: 100

engine:
  pending_delete_buffer_size: 100
```

### 中等流量场景（100-1000 msg/s）
```yaml
webhook:
  worker_count: 4
  event_buffer: 1000

engine:
  pending_delete_buffer_size: 1000
```

### 高流量场景（> 1000 msg/s）
```yaml
webhook:
  worker_count: 8-16
  event_buffer: 5000

engine:
  pending_delete_buffer_size: 10000
  pending_delete_batch_size: 5000
```

## 监控配置效果

启动 Bot 后，观察日志输出以了解配置是否生效：

```
INFO[...] [WebhookServerAdapter] Config: workers=8, buffer=2000
INFO[...] [Token] Config: retry_delay=10s, refresh_advance=30s, min_refresh_ratio=0.50
INFO[...] [Engine] Config applied: cleanup_interval=5m0s, delete_buffer=1000, ...
```

## 故障排查

### 问题 1：消息丢失

**现象**：
```
WARN[...] [Webhook] Event channel is full, dropping payload
```

**解决方案**：
```yaml
webhook:
  worker_count: 16     # 增加并发处理
  event_buffer: 5000   # 增大缓冲区
```

### 问题 2：内存占用过高

**解决方案**：
```yaml
engine:
  temp_matcher_cleanup_interval: "2m"   # 更频繁清理
  matcher_pool_max_capacity: 512        # 限制池大小

webhook:
  dedup_hard_max_size: 50              # 减小去重缓存（MB）
```

### 问题 3：Token 频繁失效

**解决方案**：
```yaml
token:
  refresh_advance: "60s"  # 提前更多时间刷新
  retry_delay: "5s"       # 减少重试延迟
```

## 相关文档

- [配置快速参考](../../docs/CONFIGURATION_QUICKREF.md)
- [配置迁移指南](../../docs/CONFIGURATION_MIGRATION.md)
- [配置改进详细分析](../../docs/CONFIGURATION_IMPROVEMENTS.md)
- [配置示例文件](../../config.example.yaml)

## 下一步

1. 阅读完整的配置文档
2. 根据你的场景调整配置
3. 进行性能测试验证配置效果
4. 监控生产环境，根据需要调优

---

*最后更新: 2026-01-24*
