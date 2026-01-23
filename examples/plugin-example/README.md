# Plugin Example

这个示例展示了如何开发和使用 Remilia 插件系统，包括插件注册、生命周期管理和热重载。

## 功能

- ✅ 插件开发 - 创建自定义插件
- ✅ 插件管理 - 注册、卸载、重载
- ✅ 生命周期监听 - 监听插件事件
- ✅ 状态保持 - 插件重载时保持状态
- ✅ 依赖管理 - 插件间依赖关系

## 插件列表

### Greeter Plugin
问候插件，支持自定义问候语

**命令**:
- `/greet [name]` - 问候用户
- `/setgreeting <text>` - 设置问候语

**示例**:
```
/greet
/greet Alice
/setgreeting 欢迎
```

### Counter Plugin
计数器插件，记录调用次数

**命令**:
- `/count` - 增加计数
- `/resetcount` - 重置计数

**示例**:
```
/count      # 输出: 计数: 1
/count      # 输出: 计数: 2
/resetcount # 重置为 0
```

### Timer Plugin
时间插件，显示运行时间和当前时间

**命令**:
- `/uptime` - 显示运行时间
- `/time` - 显示当前时间

**示例**:
```
/uptime  # 输出: 运行时间: 1h23m45s
/time    # 输出: 当前时间: 2026-01-23 15:00:00
```

## 运行

```bash
# 设置环境变量
export BOT_SECRET="your-webhook-secret"
export BOT_PORT="8080"

# 运行
go run -tags example main.go
```

程序启动 30 秒后会自动演示插件热重载。

## 代码说明

### 1. 创建插件

```go
type GreeterPlugin struct {
    *plugin.BasePlugin  // 继承基础插件
    greeting string     // 插件状态
}

func NewGreeterPlugin() *GreeterPlugin {
    return &GreeterPlugin{
        BasePlugin: plugin.NewBasePlugin("greeter"),
        greeting:   "你好",
    }
}
```

### 2. 实现 Load 方法

```go
func (p *GreeterPlugin) Load(eng *engine.Engine) error {
    // 创建 Matcher
    matcher := engine.NewMatcher().
        OnCommand("/greet").
        SetHandler(p.handleGreet)
    
    // 添加到插件
    p.AddMatcher(matcher)
    
    // 注册到引擎
    eng.RegisterMatcher(matcher)
    
    return nil
}
```

### 3. 创建插件管理器

```go
manager := plugin.NewManager(eng)
```

### 4. 注册插件

```go
greeter := NewGreeterPlugin()
if err := manager.Register(greeter); err != nil {
    log.Fatal(err)
}
```

### 5. 添加生命周期监听器

```go
type LoggingListener struct{}

func (l *LoggingListener) OnPluginLoaded(name string) {
    log.Printf("Plugin %s loaded", name)
}

func (l *LoggingListener) OnPluginUnloaded(name string) {
    log.Printf("Plugin %s unloaded", name)
}

func (l *LoggingListener) OnPluginReloaded(name string) {
    log.Printf("Plugin %s reloaded", name)
}

func (l *LoggingListener) OnPluginError(name string, op string, err error) {
    log.Printf("Plugin %s error in %s: %v", name, op, err)
}

manager.AddListener(&LoggingListener{})
```

## 高级特性

### 1. 插件重载

```go
// 自定义重载逻辑，保持状态
func (p *CounterPlugin) Reload(eng *engine.Engine) error {
    // 保存状态
    oldCount := p.count
    
    // 卸载
    if err := p.Unload(eng); err != nil {
        return err
    }
    
    // 重新加载
    if err := p.Load(eng); err != nil {
        return err
    }
    
    // 恢复状态
    p.count = oldCount
    
    return nil
}

// 手动触发重载
manager.Reload("counter")
```

### 2. 插件依赖

```go
type DependentPlugin struct {
    *plugin.BasePlugin
}

// 声明依赖
func (p *DependentPlugin) Dependencies() []string {
    return []string{"base-plugin", "utility-plugin"}
}

// 插件管理器会确保依赖插件先加载
```

### 3. 插件中间件

```go
func (p *MyPlugin) Load(eng *engine.Engine) error {
    // 为插件的所有处理器添加中间件
    p.Use(eng, 
        middleware.Logging(),
        customMiddleware(),
    )
    
    // 注册处理器
    matcher := engine.NewMatcher().OnCommand("/cmd")
    p.AddMatcher(matcher)
    eng.RegisterMatcher(matcher)
    
    return nil
}
```

### 4. 插件卸载

```go
// 卸载单个插件
manager.Unregister("greeter")

// 级联卸载（包括依赖此插件的其他插件）
manager.UnregisterCascade("base-plugin")
```

### 5. 插件信息查询

```go
// 列出所有插件
plugins := manager.List()

// 获取插件
plugin, exists := manager.Get("greeter")

// 检查插件是否已注册
if manager.Has("greeter") {
    log.Println("Greeter plugin is registered")
}
```

## 插件开发最佳实践

### 1. 资源清理

```go
func (p *MyPlugin) Unload(eng *engine.Engine) error {
    // 清理资源
    p.closeConnections()
    p.stopTimers()
    
    // 调用基类卸载
    return p.BasePlugin.Unload(eng)
}
```

### 2. 错误处理

```go
func (p *MyPlugin) Load(eng *engine.Engine) error {
    if err := p.initialize(); err != nil {
        return fmt.Errorf("initialization failed: %w", err)
    }
    
    // 继续加载...
    return nil
}
```

### 3. 并发安全

```go
type SafePlugin struct {
    *plugin.BasePlugin
    mu    sync.RWMutex
    state map[string]interface{}
}

func (p *SafePlugin) GetState(key string) interface{} {
    p.mu.RLock()
    defer p.mu.RUnlock()
    return p.state[key]
}

func (p *SafePlugin) SetState(key string, value interface{}) {
    p.mu.Lock()
    defer p.mu.Unlock()
    p.state[key] = value
}
```

### 4. 配置管理

```go
type ConfigurablePlugin struct {
    *plugin.BasePlugin
    config PluginConfig
}

type PluginConfig struct {
    Enabled  bool
    Timeout  time.Duration
    MaxRetry int
}

func (p *ConfigurablePlugin) Load(eng *engine.Engine) error {
    // 加载配置
    p.config = loadConfig()
    
    if !p.config.Enabled {
        return fmt.Errorf("plugin is disabled")
    }
    
    // 继续加载...
    return nil
}
```

## 调试技巧

### 1. 启用调试日志

```go
logrus.SetLevel(logrus.DebugLevel)
```

### 2. 添加详细日志

```go
func (p *MyPlugin) Load(eng *engine.Engine) error {
    logrus.WithFields(logrus.Fields{
        "plugin": p.Name(),
        "status": "loading",
    }).Debug("Plugin load started")
    
    // 加载逻辑...
    
    logrus.WithField("plugin", p.Name()).Info("Plugin loaded successfully")
    return nil
}
```

### 3. 监控插件状态

```go
// 定期打印插件状态
go func() {
    ticker := time.NewTicker(1 * time.Minute)
    for range ticker.C {
        plugins := manager.List()
        log.Printf("Active plugins: %d", len(plugins))
        for _, p := range plugins {
            log.Printf("  - %s", p.Name())
        }
    }
}()
```

## 下一步

- 查看 [middleware-example](../middleware-example) 了解中间件开发
- 阅读 [插件系统增强方案](../../docs/PLUGIN_ENHANCEMENT_PROPOSAL.md) 了解未来规划
- 查看 [plugin package](../../plugin/README.md) 了解更多 API
