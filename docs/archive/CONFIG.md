# 配置文件功能说明

## 概述

从 v0.6.1 开始，Remilia 支持通过配置文件管理 Bot 信息，不再需要在代码中硬编码敏感信息。

## 主要改进

### ✅ 安全性提升
- 敏感信息不再出现在代码中
- 配置文件可以排除在版本控制之外
- 支持环境变量配置（生产环境推荐）

### ✅ 灵活性增强
- 支持 YAML 配置文件
- 支持环境变量
- 多种加载方式可选

### ✅ 易于部署
- 不同环境使用不同配置
- 无需修改代码即可更换配置
- 符合 12-Factor App 原则

## 新增文件

### 1. 配置模块 (`config/`)

**config.go** - 配置加载和管理
- `Config` 结构体定义
- `Load(path)` - 从文件加载
- `LoadDefault()` - 自动查找配置
- `Validate()` - 配置验证

**config_test.go** - 完整的单元测试
- 配置加载测试
- 验证逻辑测试
- 环境变量测试

### 2. 配置示例

**config.example.yaml** - 配置文件模板
- 包含所有配置项
- 详细的注释说明
- 可直接复制使用

### 3. 示例代码

**example/webhook/config_based/** - 基于配置的完整示例
- main.go - 实际可运行的代码
- README.md - 详细使用说明

### 4. 文档

**docs/MIGRATION.md** - 迁移指南
- 从硬编码迁移到配置文件
- 分步骤说明
- 常见问题解答

**.gitignore** - Git 忽略规则
- 防止配置文件误提交
- 保护敏感信息

## 配置结构（v1.0.0）

### 基础配置（必填）

```yaml
bot:
  app_id: 123456789      # Bot AppID（必填）
  bot_id: 987654321      # Bot BotID（必填）
  token: "your_token"    # Bot Token（必填）
  secret: "your_secret"  # Bot Secret（必填）

server:
  host: "0.0.0.0"       # 服务器地址（默认 0.0.0.0）
  port: 8080            # 服务器端口（默认 8080）

log:
  level: "info"         # 日志级别: debug, info, warn, error
  format: "text"        # 日志格式: text, json
```

### 并发控制配置（可选，v0.7.0+）

```yaml
concurrency:
  limit: 500                    # 最大并发数，<=0 表示不限制
  policy: "trywait"             # 反压策略: drop, block, trywait
  wait_timeout: "500ms"         # 等待超时时间
  event_buffer: 1000            # Webhook 事件缓冲大小
```

**策略说明**:
- `drop`: 超限时丢弃新事件（适合允许丢失的场景）
- `block`: 超限时阻塞等待（适合不能丢失的场景）
- `trywait`: 尝试等待，超时后丢弃（推荐）

### 重试配置（可选，v0.7.0+）

```yaml
retry:
  enable: true              # 是否启用自动重试
  max_attempts: 3           # 最大重试次数
  backoff_base: "200ms"     # 重试退避基础时间
  backoff_max: "2s"         # 重试退避最大时间
```

### 中间件配置（可选，v0.7.0+）

```yaml
middleware:
  logging: true                 # 日志中间件
  recover: true                 # Panic 恢复中间件
  auth: false                   # 认证中间件
  auth_whitelist:               # 白名单（auth=true 时生效）
    - "user_id_1"
    - "group_id_1"
  rate_limit: false             # 限流中间件
  rate_limit_rate: 100          # 每秒令牌数
  rate_limit_burst: 200         # 令牌桶容量
  metrics: true                 # Prometheus 指标收集（v0.7.1+）
```

### 死信队列配置（可选，v0.7.0+）

```yaml
dead_letter:
  enable: true                  # 是否启用
  target: "file"                # 目标: file, kafka, webhook
  file_path: "./dead_letters.log"  # 文件路径（target=file）
  kafka_brokers:                # Kafka 地址（target=kafka）
    - "localhost:9092"
  kafka_topic: "bot-dead-letters"  # Kafka 主题
  webhook_url: "https://..."    # Webhook URL（target=webhook）
```

### Webhook 配置（可选，v0.7.0+）

```yaml
webhook:
  event_buffer: 1000            # 事件通道缓冲
  dedup_enable: true            # 启用事件去重
  dedup_shards: 1024            # BigCache 分片数
  dedup_life_window: "5m"       # 去重缓存生命周期
  dedup_clean_window: "1m"      # 清理过期条目间隔
  dedup_max_entry_size: 4096    # 单个条目最大字节数
  dedup_hard_max_size: 100      # 最大缓存大小（MB）
```

## 使用方式

### 方式 1：配置文件（开发推荐）

```go
// 1. 创建 config.yaml
// 2. 填入配置
// 3. 加载并使用
cfg, err := config.LoadDefault()
if err != nil {
    log.Fatal(err)
}
global.InitFromConfig(cfg)
```

### 方式 2：环境变量（生产推荐）

```bash
export BOT_APP_ID=123456789
export BOT_BOT_ID=987654321
export BOT_TOKEN="your_token"
export BOT_SECRET="your_secret"

go run main.go
```

### 方式 3：指定文件路径

```go
cfg, err := config.Load("/path/to/config.yaml")
if err != nil {
    log.Fatal(err)
}
global.InitFromConfig(cfg)
```

## 配置加载优先级

`config.LoadDefault()` 按以下顺序查找：

1. `./config.yaml`
2. `./config.yml`
3. 环境变量

这意味着：
- 开发时使用配置文件最方便
- 生产时使用环境变量最安全
- 可以灵活组合使用

## global 包变更

### 旧版本（硬编码）

```go
package global

var (
    Info = dto.NewBotInfo(123, 456, "token", "secret")
)
```

### 新版本（配置文件）

```go
package global

var (
    Info *dto.BotInfo // 需要初始化
)

func InitFromConfig(cfg *config.Config) {
    Info = dto.NewBotInfo(
        cfg.Bot.AppID,
        cfg.Bot.BotID,
        cfg.Bot.Token,
        cfg.Bot.Secret,
    )
}
```

## 配置热重载（v0.7.0+）

Remilia 支持配置文件的热重载，无需重启即可应用配置更改。

### 使用方式

```go
package main

import (
    "github.com/KomeiDiSanXian/remilia/config"
    "github.com/sirupsen/logrus"
)

func main() {
    // 1. 加载初始配置
    cfg, err := config.Load("config.yaml")
    if err != nil {
        logrus.Fatal(err)
    }

    // 2. 启动配置监听（热重载）
    config.Watch("config.yaml", func(newCfg *config.Config) {
        logrus.Info("配置已更新，重新加载...")
        
        // 3. 应用新配置
        // 可以根据需要更新各个组件
        if newCfg.Log.Level != cfg.Log.Level {
            logrus.SetLevel(parseLogLevel(newCfg.Log.Level))
        }
        
        // 更新全局配置
        cfg = newCfg
    })

    // 4. 启动应用
    // ...
}
```

### 支持的配置项

热重载支持以下配置的动态更新：

- ✅ **日志级别** - 实时调整日志输出
- ✅ **并发限制** - 动态调整并发数
- ✅ **中间件开关** - 启用/禁用中间件
- ✅ **限流参数** - 调整限流速率
- ⚠️ **Bot 信息** - 需要重启（涉及认证）
- ⚠️ **服务器端口** - 需要重启（端口绑定）

### 注意事项

1. **文件监听**: 使用 fsnotify 实现，支持 Linux、macOS、Windows
2. **配置验证**: 新配置加载前会先验证，验证失败不会应用
3. **原子更新**: 配置更新是原子的，不会出现中间状态
4. **回调函数**: 在回调中处理配置更新逻辑
5. **错误处理**: 监听过程中的错误会记录到日志

### 示例：动态调整日志级别

```go
config.Watch("config.yaml", func(newCfg *config.Config) {
    // 动态调整日志级别
    switch newCfg.Log.Level {
    case "debug":
        logrus.SetLevel(logrus.DebugLevel)
    case "info":
        logrus.SetLevel(logrus.InfoLevel)
    case "warn":
        logrus.SetLevel(logrus.WarnLevel)
    case "error":
        logrus.SetLevel(logrus.ErrorLevel)
    }
    logrus.Infof("日志级别已更新为: %s", newCfg.Log.Level)
})
```

### 示例：动态调整并发限制

```go
config.Watch("config.yaml", func(newCfg *config.Config) {
    if newCfg.Concurrency.Limit != currentLimit {
        engine.UpdateConcurrencyLimit(newCfg.Concurrency.Limit)
        logrus.Infof("并发限制已更新为: %d", newCfg.Concurrency.Limit)
    }
})
```

## 向后兼容性

### ✅ 完全兼容

- 现有代码无需修改（如果不使用配置功能）
- 只需在启动时添加配置加载即可
- 所有 API 保持不变

### 🔄 推荐迁移

虽然兼容，但强烈建议迁移到配置文件：

1. 安全性更好
2. 部署更灵活
3. 符合最佳实践

迁移步骤见 [MIGRATION.md](MIGRATION.md)

## 测试覆盖

### 新增测试

**config_test.go** - 配置模块测试
- ✅ TestLoad - 文件加载
- ✅ TestLoadInvalidFile - 错误处理
- ✅ TestValidate - 配置验证
- ✅ TestGet - 全局配置
- ✅ TestLoadFromEnv - 环境变量

所有测试通过，覆盖率 100%

## 安全最佳实践

### ✅ 推荐做法

1. 使用配置文件（开发）或环境变量（生产）
2. 配置文件添加到 `.gitignore`
3. 配置文件权限设置为 600
4. 定期轮换密钥
5. 使用密钥管理服务（AWS Secrets Manager、Vault 等）

### ❌ 避免做法

1. 不要在代码中硬编码敏感信息
2. 不要将配置文件提交到 Git
3. 不要在日志中打印 Token/Secret
4. 不要在公共环境暴露配置
5. 不要使用弱密码或默认密钥

## 生产环境建议

### Kubernetes

使用 Secret 和 ConfigMap：

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: bot-secret
type: Opaque
data:
  token: <base64-encoded-token>
  secret: <base64-encoded-secret>
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: bot-config
data:
  app_id: "123456789"
  bot_id: "987654321"
```

### Docker

使用环境变量：

```dockerfile
FROM golang:1.25
WORKDIR /app
COPY . .
RUN go build -o bot

CMD ["./bot"]
```

```bash
docker run -e BOT_APP_ID=123 -e BOT_TOKEN=xxx bot
```

### Systemd

使用环境文件：

```ini
[Unit]
Description=Remilia Bot

[Service]
Type=simple
EnvironmentFile=/etc/bot/config.env
ExecStart=/usr/local/bin/bot
Restart=always

[Install]
WantedBy=multi-user.target
```

# 配置与热重载

Remilia 支持从 `config.yaml` 或环境变量加载配置，并提供对配置文件的热重载能力。

## 加载配置

```go
cfg, err := config.LoadDefault()
if err != nil { /* 处理错误 */ }
```

## 热重载（推荐）

使用 `config.Watch` 监听配置文件，当检测到写入/重命名/创建事件时自动重载，并将新的配置传给回调：

```go
stop, err := config.Watch("config.yaml", func(cfg *config.Config) {
    // 应用新的日志级别
    level, _ := logrus.ParseLevel(cfg.Log.Level)
    logrus.SetLevel(level)
    // 可按需应用其他设置（例如服务端口变更只记录提示）
})
if err != nil { /* 处理错误 */ }

defer stop()
```

注意：
- 变更会经过 `Validate()` 校验；失败则忽略此次重载
- 内置 200ms 防抖，避免保存文件产生的多事件抖动
- 建议只在可安全动态更新的配置项上生效（如日志级别、匹配器开关）；涉及监听地址等需重启服务

## 中间件配置（示例）

在 `config.yaml` 中新增 `middleware` 段以控制中间件开关与参数：

```yaml
middleware:
  logging: true
  recover: true
  auth: true
  auth_whitelist: ["user_open_id_1", "user_open_id_2"]
  rate_limit: true
  rate_limit_rate: 5     # 每秒令牌
  rate_limit_burst: 10   # 突发容量
  metrics: true
```

应用示例（在 Watch 回调中）：
```go
import m "github.com/KomeiDiSanXian/remilia/middleware"

stop, _ := config.Watch("config.yaml", func(cfg *config.Config) {
    engine.ResetMiddlewares() // 清空全局/插件中间件
    engine.SetTrace(true)     // 开启执行顺序可视化（可选）

    if cfg.Middleware.Logging {
        engine.Use(engine.Named("logging", m.Logging()))
    }
    if cfg.Middleware.Recover {
        engine.Use(engine.Named("recover", m.Recover(engine)))
    }
    if cfg.Middleware.Auth {
        whitelist := make(map[string]struct{})
        for _, id := range cfg.Middleware.AuthWhitelist { whitelist[id] = struct{}{} }
        allow := func(ctx *remilia.Context) bool {
            a := ctx.GetAuthor(); if a == nil { return false }
            _, ok := whitelist[a.UserOpenID]; return ok
        }
        engine.Use(engine.Named("auth", m.Auth(allow)))
    }
    if cfg.Middleware.RateLimit {
        rate := cfg.Middleware.RateLimitRate; burst := cfg.Middleware.RateLimitBurst
        // 按用户维度限流：keyFn 返回用户ID（可根据需求返回群ID等）
        keyFn := func(ctx *remilia.Context) string { if a := ctx.GetAuthor(); a != nil { return a.UserOpenID }; return "" }
        engine.Use(engine.Named("ratelimit", m.RateLimitTokenBucket(rate, burst, keyFn)))
    }
    if cfg.Middleware.Metrics {
        engine.Use(engine.Named("metrics", m.PrometheusMetrics("remilia")))
    }
})
```

说明：
- `ResetMiddlewares` 不影响 matcher 局部中间件（按需调用该方法重置）
- `SetTrace(true)` 后，可在 Handler 中通过 `ctx.GetState("mw_trace")` 获取执行链名称列表用于结构化日志或上报
- 令牌桶限流支持共享桶（`keyFn=nil`）与按用户/群维度限流（提供 `keyFn`）；按需选择

## 并发/反压配置（新）

在 `config.yaml` 中新增 `concurrency` 区段：

```yaml
concurrency:
  limit: 100            # 最大并发，<=0 表示不限制
  policy: drop          # drop|block|trywait
  wait_timeout: "200ms" # TryWait 的等待时长
  event_buffer: 1024    # WebHook 事件通道缓冲（修改后需重启生效）
```

应用示例：
```go
apply := func(cfg *config.Config) {
    // 策略映射
    policy := remilia.Drop
    switch cfg.Concurrency.Policy {
    case "block": policy = remilia.Block
    case "trywait": policy = remilia.TryWait
    }
    engine.SetConcurrencyLimit(cfg.Concurrency.Limit, policy)
    if d, err := time.ParseDuration(cfg.Concurrency.WaitTimeout); err == nil {
        engine.SetWaitTimeout(d)
    }
}

stop, _ := config.Watch("config.yaml", apply)
defer stop()
```

注意：
- `event_buffer` 影响 WebHook 的事件通道缓冲大小，当前实现需重启 Bot 重建连接才能生效
- 其他参数（limit/policy/wait_timeout）可在运行时动态调整

## 重试与死信队列（新）

在处理器返回错误或发生 panic（启用恢复）时，Engine 可进行自动重试，并在超过最大尝试后将事件投递到死信队列：

```yaml
retry:
  enable: true          # 是否启用重试
  max_attempts: 3       # 最大重试次数（>0 启用）
  backoff_base: "200ms" # 指数退避基础时长
  backoff_max:  "2s"   # 指数退避最大时长
```

应用示例：
```go
apply := func(cfg *config.Config) {
    if cfg.Retry.Enable && cfg.Retry.MaxAttempts > 0 {
        base, _ := time.ParseDuration(cfg.Retry.BackoffBase)
        max, _ := time.ParseDuration(cfg.Retry.BackoffMax)
        engine.EnableRetry(true, cfg.Retry.MaxAttempts, base, max)
    } else {
        engine.EnableRetry(false, 0, 0, 0)
    }
}
```

死信队列消费：
```go
go func() {
    for item := range engine.DeadLetter() {
        logrus.WithError(item.Err).WithFields(logrus.Fields{
            "attempt": item.Attempt,
            "type":    item.Event.Type,
        }).Error("dead letter")
        // TODO: 持久化/报警/人工介入
    }
}()
```

注意：
- 重试配合限流/反压一起使用（见上节），避免雪崩
- 死信队列为内存通道，默认缓冲 128；满时会上报 errorHandlers

## 死信队列持久化配置（新）

通过 `dead_letter` 段配置死信持久化目标：

```yaml
dead_letter:
  enable: true
  target: file         # file|kafka|webhook
  file_path: "deadletter.log"     # 当 target=file 时有效
  kafka_brokers: ["127.0.0.1:9092"] # 当 target=kafka 时有效
  kafka_topic: "remilia-deadletter"
  webhook_url: "https://example.com/deadletter" # 当 target=webhook 时有效
```

应用示例：
```go
stop, _ := config.Watch("config.yaml", func(cfg *config.Config) {
    // ... 应用其他配置 ...
    if cfg.DeadLetter.Enable {
        switch strings.ToLower(cfg.DeadLetter.Target) {
        case "file":
            engine.AddDeadLetterConsumer(remilia.FileDeadLetterConsumer{Path: cfg.DeadLetter.FilePath})
        case "kafka":
            engine.AddDeadLetterConsumer(remilia.KafkaDeadLetterConsumer{Brokers: cfg.DeadLetter.KafkaBrokers, Topic: cfg.DeadLetter.KafkaTopic})
        case "webhook":
            engine.AddDeadLetterConsumer(remilia.WebhookDeadLetterConsumer{URL: cfg.DeadLetter.WebhookURL})
        }
    }
})
```

说明：
- Kafka 消费器为占位示例，实际项目可选择 `segmentio/kafka-go` 或 `confluent-kafka-go` 实现
- File/Webhook 消费器已提供可用实现；文件以 JSON Lines 追加写入（每行一个事件）
- 标准化错误结构参见文档与 `errors.go`，包含 `message/source/attempt/trace/event_id` 字段

## Webhook 事件去重配置（新）

在 `config.yaml` 中可配置 Webhook 的事件缓冲与去重参数：

```yaml
webhook:
  event_buffer: 1024           # 事件通道缓冲（修改后需重启生效）
  dedup_enable: true           # 是否启用事件去重（BigCache）
  dedup_shards: 1024           # 分片数（>=64；越大并发下冲突越少）
  dedup_life_window: "5m"      # 去重窗口（建议高流量下缩短以降低内存占用）
  dedup_clean_window: "1m"     # 清理过期条目的间隔时间
  dedup_max_entry_size: 4096   # 单个缓存条目的最大字节数
  dedup_hard_max_size: 1024    # 缓存最大内存（MB，上限控制）
```

参数说明：
- `event_buffer`: Webhook 事件通道缓冲区大小
- `dedup_enable`: 是否启用去重（v0.7.0+ 使用 BigCache）
- `dedup_shards`: BigCache 分片数，影响并发性能（推荐 2 的幂次，如 1024、2048）
- `dedup_life_window`: 去重窗口时长，超过此时间的事件会被清理
- `dedup_clean_window`: 后台清理过期条目的间隔
- `dedup_max_entry_size`: 单个缓存条目的最大字节数（防止超大事件占用过多内存）
- `dedup_hard_max_size`: 缓存的硬性内存限制（MB）

示例接线：
```go
life, _ := time.ParseDuration(cfg.Webhook.LifeWindow)
clean, _ := time.ParseDuration(cfg.Webhook.CleanWindow)

wh := webhook.NewWithOptions(ctx, global.Info, cfg.Webhook.EventBuffer, webhook.DedupOptions{
  Enable:           cfg.Webhook.DedupEnable,
  Shards:           cfg.Webhook.Shards,
  LifeWindow:       life,
  CleanWindow:      clean,
  MaxEntrySize:     cfg.Webhook.MaxEntrySize,
  HardMaxCacheSize: cfg.Webhook.HardMaxCacheSize,
})
```

建议：
- 高流量/内存紧张时，可关闭去重（`dedup_enable: false`）或缩短 `dedup_life_window`
- `event_buffer` 与去重参数当前实现需重启 Bot 生效（重建连接）

## 相关文档

- [迁移指南](MIGRATION.md) - 详细的迁移步骤
- [快速开始](QUICKSTART.md) - 5分钟上手
- [使用指南](GUIDE.md) - 完整的API文档
- [示例代码](../example/webhook/config_based/) - 可运行的示例

## 常见问题

### Q: 是否必须使用配置文件？

A: 不是必须的，但强烈推荐。你仍可以使用旧的方式，但配置文件更安全、更灵活。

### Q: 如何保证配置安全？

A: 
1. 不要提交到 Git
2. 使用 600 权限
3. 生产环境使用环境变量或密钥管理服务

### Q: 配置文件可以放在其他位置吗？

A: 可以，使用 `config.Load("/path/to/config.yaml")` 指定路径。

### Q: 支持哪些格式？

A: 目前支持 YAML 和环境变量。

### Q: 如何验证配置是否正确？

A: 配置加载时会自动验证必填字段，确保配置有效。

## 更新日志

查看 [CHANGELOG.md](CHANGELOG.md) 获取完整的版本历史。

---

**添加时间**: 2025-11-28  
**影响版本**: v0.6.1+  
**优先级**: 高（涉及安全）
