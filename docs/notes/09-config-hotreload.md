# 配置管理与热更新——运行时动态调整

> **ZeroBot 基因**：ZeroBot 无配置热更新能力——配置在启动时一次性加载。这是 Remilia 从零独立设计的能力。参阅 [`11-zerobot-inspiration.md`](11-zerobot-inspiration.md#7-remilia-有但-zerobot-没有的)。

## 设计目标

1. **零重启配置更新**：修改配置文件后，Bot 无需重启即可生效
2. **安全回滚**：配置校验失败或应用后出错的配置应能拒绝
3. **解耦桥接**：config 包和 engine 包之间不应有双向依赖
4. **生命周期绑定**：配置监视器应随 Bot 启停自动管理

## 1. 配置加载（config.Load）

```go
type Config struct {
    Bot       BotConfig       `yaml:"bot"`
    Server    ServerConfig    `yaml:"server"`
    Engine    EngineConfig    `yaml:"engine"`
    Middleware MiddlewareConfig `yaml:"middleware"`
    Retry     RetryConfig     `yaml:"retry"`
    // ...
}
```

支持从 YAML 文件和/或环境变量加载：

```go
cfg, err := config.Load("config.yaml")
// 也支持：
cfg, err := config.LoadWithEnv("config.yaml", "REMILIA_")  // REMILIA_BOT_APP_ID
```

环境变量优先级高于 YAML 配置，适合容器化部署。

## 2. 配置验证

```go
func (c *Config) Validate() error {
    if c.Bot.AppID == 0 {
        return fmt.Errorf("bot.app_id is required")
    }
    // ...
}
```

### 引擎配置桥接

```go
// config/bridge.go — 消除双向依赖
func EngineOptions(cfg EngineConfig) []engine.Option {
    var opts []engine.Option
    if d, err := time.ParseDuration(cfg.TempMatcherCleanupInterval); err == nil {
        opts = append(opts, engine.WithCleanupInterval(d))
    }
    // ...
    return opts
}
```

`core/engine` 不导入 `config` 包，通过桥接函数 `EngineOptions` 将 YAML 配置转换为 `engine.Option`。

## 3. 配置监视器（Watcher）

```go
type Watcher struct {
    configPath    string
    watcher       *fsnotify.Watcher
    callbacks     []ReloadCallback
    currentConfig infraatomic.Value[*Config]
    debounceDelay time.Duration
    // 生命周期绑定
    ctx      context.Context
    cancel   context.CancelFunc
    parentCtx context.Context
}
```

### 文件监听原理

使用 `fsnotify` 监听**配置文件所在目录**而非文件本身：

```go
dir := filepath.Dir(absPath)
fsWatcher.Add(dir)
```

这样做的原因是：很多编辑器（Vim、VSCode）保存文件时采用**写入临时文件再重命名**的策略，直接监听单个文件会丢失事件。

### 防抖（Debounce）

```go
const DefaultDebounceDelay = 100 * time.Millisecond

// 文件变更事件到达后，等待 100ms 内的后续变更合并处理
select {
case <-debounceTimer.C:
    // 执行重载
case <-nextEvent:
    debounceTimer.Reset(debounceDelay)  // 新事件到来，重新计时
}
```

避免单次保存触发多次重载（编辑器可能触发多个 fsnotify 事件：`WRITE` → `RENAME` → `WRITE`）。

### 重载回调

```go
type ReloadCallback func(oldConfig, newConfig *Config) error

func (w *Watcher) OnReload(callback ReloadCallback) {
    w.mu.Lock()
    w.callbacks = append(w.callbacks, callback)
    w.mu.Unlock()
}
```

回调可以**拒绝**应用新配置：

```go
watcher.OnReload(func(old, new *Config) error {
    if new.Middleware.RateLimit == nil {
        return fmt.Errorf("rate_limit config is required")
    }
    return nil  // 接受配置
})
```

## 4. 热更新桥接器（hotreload.Bridge）

```go
type Bridge struct {
    mu              sync.RWMutex
    adaptives       []*middleware.AdaptiveRateLimiter
    retries         []*middleware.ConfigurableRetry
    circuitBreakers []*middleware.CircuitBreaker
    dedups          []*middleware.DedupFilter
    degradations    []*middleware.AdaptiveDegradation
}
```

Bridge 将 config.Watcher 与中间件连接起来：

```go
bridge := hotreload.NewBridge()
bridge.WatchAdaptive(limiter)
bridge.WatchRetry(retrier)
bridge.WatchCircuitBreaker(cb)
bridge.WatchDedup(dedup)
bridge.WatchDegradation(degradation)

token := config.Subscribe(bridge.OnConfigChange)
defer token.Cancel()  // Bot 停止时取消订阅
```

当配置文件变更后，`OnConfigChange` 将新配置推送到所有已注册的中间件：

```go
func (b *Bridge) OnConfigChange(newCfg *config.Config) {
    b.mu.RLock()
    defer b.mu.RUnlock()

    // 更新限流器
    for _, arl := range b.adaptives {
        arl.UpdateConfig(middleware.AdaptiveConfig{
            AdjustStep: newCfg.Middleware.RateLimit.Burst,
        })
    }

    // 更新重试配置
    for _, cr := range b.retries {
        cr.UpdateConfig(middleware.RetryConfig{
            MaxAttempts: newCfg.Retry.MaxAttempts,
            BackoffBase: parseDuration(newCfg.Retry.BackoffBase, 200*time.Millisecond),
        })
    }

    // 更新去重器
    for _, df := range b.dedups {
        df.UpdateConfig(middleware.DedupConfig{
            MaxSize:    newCfg.Middleware.Dedup.MaxSize,
            DefaultTTL: parseDuration(newCfg.Middleware.Dedup.DefaultTTL, 0),
        })
    }
    // ...
}
```

### 典型用法组合

```go
func main() {
    // 1. 加载初始配置
    cfg, err := config.Load("config.yaml")

    // 2. 构建平台适配器
    adapter := qq.NewWebhookServerAdapter(
        fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
        &dto.BotInfo{...},
    )

    // 3. 构建 Bot
    bot, err := remilia.NewBotBuilder().
        WithPlatformAdapter(adapter).
        WithEngineOptions(config.EngineOptions(cfg.Engine)...).
        Build()

    // 4. 创建配置监视器（与 Bot 生命周期绑定）
    watcher, err := config.NewWatcherWithContext(bot.Context(), "config.yaml")
    watcher.OnReload(func(old, new *config.Config) error {
        logger.Info("config reloaded")
        return nil
    })

    // 5. 热更新桥接
    bridge := hotreload.NewBridge()
    // ... 注册中间件 ...
    watcher.OnReload(bridge.OnConfigChange)

    // 6. 启动
    bot.Start()
    watcher.Start()

    bot.WaitForShutdown()
}
```

## 5. 生命周期绑定

Watcher 支持与外部 Context 绑定，Bot 关闭时自动停止：

```go
watcher, err := config.NewWatcherWithContext(bot.Context(), "config.yaml")
```

内部实现：

```go
func (w *Watcher) watchLoop() {
    for {
        select {
        case <-w.parentCtx.Done():
            // Bot 已关闭，自动停止监听
            return
        case event := <-w.watcher.Events:
            // 处理文件变更...
        }
    }
}
```

## 迭代过程

### V0：根包 config 结构 + Viper 集成

初始版本使用 Viper 进行配置管理（commit `1e0e5ca`）：

```go
// V0 — Viper 集成
import "github.com/spf13/viper"

type Config struct {
    Bot       BotConfig
    Server    ServerConfig
    // ...
}

func Load(path string) (*Config, error) {
    viper.SetConfigFile(path)
    viper.AutomaticEnv()           // 自动读取环境变量
    viper.SetEnvPrefix("REMILIA")  // 环境变量前缀
    if err := viper.ReadInConfig(); err != nil {
        return nil, err
    }
    var cfg Config
    if err := viper.Unmarshal(&cfg); err != nil {
        return nil, err
    }
    return &cfg, nil
}
```

**问题**：
- Viper 是一个重型依赖——引入了 YAML、JSON、TOML、etcd 等多种后端，但实际只用 YAML
- `viper.Unmarshal` 使用反射，性能开销大（每次加载 ~500μs）
- Viper 的 `AutomaticEnv` 导致意外的环境变量覆盖——debug 时经常发现配置没生效，查了半天发现是环境变量优先级更高
- 没有配置监视器——修改配置文件需要重启 Bot

### V1：移除 Viper，替换为纯 YAML 解析

决定完全移除 Viper（commit `b4973a6`），使用标准 `gopkg.in/yaml.v3` 直接解析：

```go
// V1 — 纯 YAML + 环境变量手动管理
func Load(path string) (*Config, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, err
    }
    var cfg Config
    if err := yaml.Unmarshal(data, &cfg); err != nil {
        return nil, err
    }
    // 手动处理环境变量覆盖（仅限明确需要的字段）
    if appID := os.Getenv("REMILIA_BOT_APP_ID"); appID != "" {
        cfg.Bot.AppID, _ = strconv.ParseUint(appID, 10, 64)
    }
    return &cfg, nil
}
```

**好处**：
- 依赖大幅减少（移除 Viper 及其传递依赖）
- 加载性能提升（无反射 Unmarshal，直接 yaml 解析）
- 环境变量覆盖行为明确可控——不会突然被环境变量覆盖掉配置

### V2：`fsnotify` 配置监视器

引入文件系统监听实现热更新：

```go
// V2 — config/watcher.go
type Watcher struct {
    configPath    string
    watcher       *fsnotify.Watcher
    callbacks     []ReloadCallback
    currentConfig infraatomic.Value[*Config]
    debounceDelay time.Duration
    parentCtx     context.Context  // 与 Bot 生命周期绑定
}
```

关键设计：**监听目录而非文件**。编辑器保存文件时不一定会触发目标文件的 INotify 事件（通常是 Write → Rename → Write 模式）。监听父目录确保所有变更都能捕获。

防抖设计解决了单次保存触发多次重载的问题：

```go
// 文件变更事件到达后，等待 100ms 内的后续变更合并处理
for {
    select {
    case event := <-w.watcher.Events:
        debounceTimer.Reset(w.debounceDelay)
    case <-debounceTimer.C:
        w.reload()  // 执行重载
    case <-w.parentCtx.Done():
        return  // Bot 关闭时自动停止
    }
}
```

### V3：Bridge 桥接模式（消除双向依赖）

配置变更需要传播到中间件，但 `core/engine` 不能导入 `config` 包（避免循环依赖）：

```go
// V2 方式（有双向依赖风险）
// core/engine → config: ❌ 引擎不能依赖 config 包

// V3 — bridge 桥接，只在应用层组合
// config/bridge.go — 将 config.EngineConfig 转为 []engine.Option
func EngineOptions(cfg EngineConfig) []engine.Option {
    var opts []engine.Option
    // 类型安全的转换
    if d, err := time.ParseDuration(cfg.TempMatcherCleanupInterval); err == nil {
        opts = append(opts, engine.WithCleanupInterval(d))
    }
    return opts
}

// middleware/hotreload/bridge.go — 配置热更新桥接
type Bridge struct {
    adaptives    []*middleware.AdaptiveRateLimiter
    retries      []*middleware.ConfigurableRetry
    // ...
}

func (b *Bridge) OnConfigChange(newCfg *config.Config) {
    for _, arl := range b.adaptives {
        arl.UpdateConfig(middleware.AdaptiveConfig{...})
    }
    // ...
}
```

这种方式确保：
- `core/engine` 不导入 `config` 包（无反向依赖）
- `middleware` 不导入 `config` 包（只通过 Bridge 接收更新）
- 应用层在 `main()` 中组装 `Watcher` → `Bridge` → `Middleware` 的链条

### V4：生命周期绑定

Watcher 初始版本需要手动管理 `Stop()`，与 Bot 关闭不同步：

```go
// V3 — 手动停止
watcher.Start()
defer watcher.Stop()  // 容易忘记
```

```go
// V4（当前）— 与 Bot 生命周期绑定
watcher, err := config.NewWatcherWithContext(bot.Context(), "config.yaml")
// parentCtx 取消时（Bot 关闭），watchLoop 自动退出
```

同时支持 `*WithContext` 模式——与 `AdaptiveRateLimiter`、`DedupFilter`、`token.Manager` 等组件的生命周期管理风格一致。

## 迭代历程

| 版本 | 核心变化 | 解决的问题 |
|------|---------|-----------|
| V0 | Viper 集成 | 快速实现 |
| V1 | 移除 Viper，纯 YAML | 减少依赖，性能更好 |
| V2 | fsnotify 监视器 + 防抖 | 文件变更热更新 |
| V3 | Bridge 桥接 + EngineOptions | 消除双向依赖 |
| V4（当前）| 生命周期绑定 + WithContext | 自动管理启动/停止 |

## 设计权衡

| 方面 | 选择 | 理由 |
|------|------|------|
| 文件监听 | fsnotify 目录级 | 兼容各种编辑器的保存策略 |
| 防抖 | 100ms 合并窗口 | 避免多次触发 |
| 配置源 | YAML + 环境变量 | 简单易读 + 容器友好 |
| 更新传播 | Bridge 推模式 | 中间件自身维护状态，Bridge 只推送变化 |
| 安全 | 回调可拒绝 | 校验失败时自动保留旧配置 |
