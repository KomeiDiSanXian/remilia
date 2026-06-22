# Configuration Hot-Reload Quick Reference

> **最后更新**: 2026-02-25



## 快速开始

### 1. 基本使用（3行代码）

```go
watcher, _ := config.NewWatcher("config.yaml")
defer watcher.Stop()
watcher.Start()
```

### 2. 添加变更处理

```go
watcher.AddCallback(func(old, new *config.Config) error {
    // 处理配置变更
    return nil
})
```

### 3. 获取当前配置

```go
cfg := watcher.GetConfig()
```

---

## API 速查

### 创建监听器

```go
// 默认配置
watcher, err := config.NewWatcher("config.yaml")

// 自定义防抖延迟
watcher, err := config.NewWatcher(
    "config.yaml",
    config.WithDebounceDelay(500*time.Millisecond),
)

// 仅验证模式
watcher, err := config.NewWatcher(
    "config.yaml",
    config.WithValidateOnly(true),
)
```

### 生命周期管理

```go
watcher.Start()              // 启动监听
watcher.Stop()               // 停止监听（阻塞等待）
watcher.ForceReload()        // 手动触发重载
```

### 配置访问

```go
cfg := watcher.GetConfig()   // 获取当前配置（线程安全）
stats := watcher.GetStats()  // 获取统计信息
```

### 回调函数

```go
type ReloadCallback func(oldConfig, newConfig *Config) error

watcher.AddCallback(func(old, new *Config) error {
    // 返回 nil: 接受配置
    // 返回 error: 拒绝配置
    return nil
})
```

---

## 常见用例

### 动态日志级别

```go
watcher.AddCallback(func(old, new *config.Config) error {
    if old.Log.Level != new.Log.Level {
        _ = logger.SetLevel(new.Log.Level)
    }
    return nil
})
```

### 拒绝危险变更

```go
watcher.AddCallback(func(old, new *config.Config) error {
    if old.Bot.AppID != new.Bot.AppID {
        return fmt.Errorf("AppID change requires restart")
    }
    return nil
})
```

### 组件重启

```go
watcher, _ := config.WatchWithAutoRestart("config.yaml", func(cfg *config.Config) error {
    // 重启组件
    component.Restart(cfg)
    return nil
})
```

### 多个回调

```go
watcher.AddCallback(validateCallback)  // 验证
watcher.AddCallback(applyCallback)     // 应用
watcher.AddCallback(notifyCallback)    // 通知
```

---

## 配置示例

### 修改前（config.yaml）

```yaml
log:
  level: "info"
  format: "text"

concurrency:
  limit: 100
```

### 修改后

```yaml
log:
  level: "debug"    # ← 修改
  format: "text"

concurrency:
  limit: 200        # ← 修改
```

**效果**: 保存文件后 100ms 内自动重载

---

## 中间件热更新（Bridge API）

`hotreload.Bridge` 将配置变更推送给各中间件组件，无需重启进程：

```go
import "github.com/KomeiDiSanXian/remilia/middleware/hotreload"

// 创建桥接器
bridge := hotreload.NewBridge()

// 注册需要热更新的中间件组件
bridge.WatchRateLimit(tokenBucketMiddleware)   // 令牌桶限流
bridge.WatchAdaptive(adaptiveController)        // 自适应限流
bridge.WatchDedup(dedupFilter)                  // 去重过滤器（MaxSize / DefaultTTL）
bridge.WatchDegradation(adaptiveDeg)            // 降级阈值（CPU / Memory threshold）

// 订阅 Watcher 变更
token := bridge.Subscribe()
defer token.Cancel()

// 启动监听
watcher.Start()
```

### WatchDedup — 去重过滤器热更新

当 `config.yaml` 中的 `middleware.dedup_max_size` 或 `middleware.dedup_default_ttl` 变更时，
自动调用 `DedupFilter.UpdateConfig()`：

```go
// 可热更新的字段
type DedupConfig struct {
    MaxSize    int           // 缓存最大条数（0 = 不更新）
    DefaultTTL time.Duration // 默认 TTL（0 = 不更新）
    // CleanupInterval 变更需要重建过滤器，不支持热更新
}
```

### WatchDegradation — 降级阈值热更新

当 `config.yaml` 中的 `middleware.degradation_cpu_threshold` 或
`middleware.degradation_memory_threshold` 变更时，
自动调用 `AdaptiveDegradation.UpdateConfig()`：

```yaml
middleware:
  degradation_cpu_threshold: 80.0     # CPU 使用率阈值（0-100）
  degradation_memory_threshold: 85.0  # 内存使用率阈值（0-100）
```

---

## 最佳实践

### ✅ 推荐

```go
// 1. 使用 defer 确保清理
watcher, _ := config.NewWatcher("config.yaml")
defer watcher.Stop()

// 2. 启动前添加所有回调
watcher.AddCallback(callback1)
watcher.AddCallback(callback2)
watcher.Start()

// 3. 回调中处理错误
watcher.AddCallback(func(old, new *config.Config) error {
    if err := validate(new); err != nil {
        logger.WithError(err).Warn("Invalid config")
        return err
    }
    return nil
})
```

### ❌ 避免

```go
// 1. 不要在回调中阻塞
watcher.AddCallback(func(old, new *config.Config) error {
    time.Sleep(10 * time.Second) // ❌ 不要阻塞
    return nil
})

// 2. 不要在回调中panic
watcher.AddCallback(func(old, new *config.Config) error {
    panic("error") // ❌ 使用 return error
})

// 3. 不要忘记 Stop
watcher, _ := config.NewWatcher("config.yaml")
// ❌ 缺少 defer watcher.Stop()
```

---

## 故障排查

### 配置未生效？

1. 检查日志是否有重载消息
2. 验证文件路径是否正确
3. 确认文件保存成功
4. 检查回调是否返回错误

```go
// 启用详细日志
logger.SetLevel("debug")
```

### 重载失败？

```go
// 查看统计信息
stats := watcher.GetStats()
fmt.Printf("Failed: %d\n", stats.FailedCount)

// 手动重载测试
if err := watcher.ForceReload(); err != nil {
    fmt.Printf("Error: %v\n", err)
}
```

### 性能问题？

```go
// 增加防抖延迟
watcher, _ := config.NewWatcher(
    "config.yaml",
    config.WithDebounceDelay(1*time.Second), // 延长延迟
)
```

---

## 性能数据

| 操作 | 延迟 | 吞吐量 |
|------|------|--------|
| 文件监听 | ~1ms | 事件驱动 |
| 配置加载 | ~1ms | - |
| 回调执行 | 用户定义 | - |
| 并发读取 | <1μs | >1M ops/s |

---

## 集成示例

### 与 Bot 集成

```go
func main() {
    watcher, _ := config.NewWatcher("config.yaml")
    defer watcher.Stop()
    
    watcher.AddCallback(func(old, new *config.Config) error {
        // 动态更新日志
        _ = logger.SetLevel(new.Log.Level)
        return nil
    })
    
    watcher.Start()
    
    eng := engine.NewEngine()
    bot, _ := remilia.NewBotBuilder().WithEngine(eng).Build()
    bot.Start()
    defer bot.Shutdown()
    
    // 阻塞主线程
    select {}
}
```

### 与 HTTP 服务器集成

```go
watcher.AddCallback(func(old, new *config.Config) error {
    if old.Server.Port != new.Server.Port {
        // 端口变更需要重启服务器
        server.Restart(new.Server)
    }
    return nil
})
```

---

## 配置字段说明

### 支持动态更新的字段

- ✅ `log.level` - 日志级别
- ✅ `log.format` - 日志格式
- ✅ `middleware.*` - 中间件配置
- ✅ `concurrency.limit` - 并发限制
- ✅ `retry.*` - 重试配置

### 需要重启的字段

- ⚠️ `bot.app_id` - Bot AppID
- ⚠️ `bot.bot_id` - Bot ID
- ⚠️ `bot.token` - Token
- ⚠️ `bot.secret` - Secret
- ⚠️ `server.port` - 服务器端口

---

## 测试

### 单元测试

```bash
go test ./config -v -run TestWatcher
```

### 手动测试

```bash
# 1. 启动应用
go run examples/config_hotreload/main.go

# 2. 编辑 config.yaml
vim config.yaml

# 3. 观察日志输出
# [ConfigWatcher] Configuration reloaded successfully
```

---

## 更多资源

- 💡 [使用示例](../examples/config_hotreload/main.go)
- 🔧 [配置模板](../config.example.yaml)

---

**最后更新**: 2026-02-25
