# 插件系统文档

## 概述

Remilia 提供了强大的插件系统，支持模块化开发和热重载功能。插件系统允许开发者将功能封装成独立的模块，便于管理和维护。

## 核心接口

### Plugin 接口

```go
type Plugin interface {
    // Name 返回插件名称
    Name() string
    // Load 加载插件到引擎，返回错误信息
    Load(engine *Engine) error
    // Unload 卸载插件，返回错误信息
    Unload(engine *Engine) error
    // Dependencies 返回插件依赖列表（v0.7.1 新增）
    Dependencies() []string
}
```

**v0.7.1 新增**: `Dependencies()` 方法用于声明插件依赖关系，插件管理器会自动按依赖顺序加载插件。

### BasePlugin 基础实现

框架提供了 `BasePlugin` 基础插件结构，实现了基本的插件管理功能：

```go
type BasePlugin struct {
    name     string
    matchers []*Matcher
    mu       sync.RWMutex
}
```

**主要方法：**

- `Name()` - 返回插件名称
- `AddMatcher(matcher *Matcher)` - 添加匹配器（线程安全）
- `GetMatchers()` - 获取所有匹配器（返回副本，线程安全）
- `Load(engine *Engine) error` - 加载插件（默认实现为空，子类重写）
- `Unload(engine *Engine) error` - 卸载插件，自动清理所有匹配器

## PluginManager 插件管理器

### 创建管理器

```go
engine := remilia.NewEngine()
pluginManager := remilia.NewPluginManager(engine)
```

### 注册插件

```go
plugin := NewMyPlugin()
err := pluginManager.Register(plugin)
if err != nil {
    // 处理错误（如插件已存在、加载失败等）
    log.Printf("Failed to register plugin: %v", err)
}
```

**注册流程：**
1. 检查插件是否已存在
2. 调用插件的 `Load()` 方法
3. 将插件添加到管理器

**可能的错误：**
- `ErrPluginAlreadyExists` - 插件名称已存在
- 加载失败 - 插件 `Load()` 方法返回的错误

### 注销插件

```go
err := pluginManager.Unregister("plugin-name")
if err != nil {
    log.Printf("Failed to unregister plugin: %v", err)
}
```

### 依赖管理（v0.7.1 新增）⭐

插件系统支持声明式依赖管理，自动解析依赖顺序并按正确顺序加载插件。

#### 声明依赖

```go
type DatabasePlugin struct {
    *remilia.BasePlugin
}

// 重写 Dependencies 方法声明依赖
func (p *DatabasePlugin) Dependencies() []string {
    return []string{"config", "logger"} // 依赖 config 和 logger 插件
}

func (p *DatabasePlugin) Load(engine *remilia.Engine) error {
    // config 和 logger 已经加载完成，可以安全使用
    // 初始化数据库连接...
    return nil
}
```

#### 批量注册（自动解析依赖）

```go
// 创建插件（顺序不重要）
configPlugin := NewConfigPlugin()      // 无依赖
loggerPlugin := NewLoggerPlugin()      // 依赖 config
databasePlugin := NewDatabasePlugin()  // 依赖 config, logger
cachePlugin := NewCachePlugin()        // 依赖 database

// 使用 RegisterWithDependencies 自动解析依赖顺序
err := pluginManager.RegisterWithDependencies([]remilia.Plugin{
    databasePlugin,  // 乱序提交
    cachePlugin,
    loggerPlugin,
    configPlugin,
})

// 实际加载顺序：config -> logger -> database -> cache
```

**特性**：
- ✅ 自动拓扑排序
- ✅ 循环依赖检测
- ✅ 缺失依赖检测
- ✅ 支持复杂依赖关系（DAG）

#### 依赖检测

```go
// 循环依赖示例
pluginA.Dependencies() // returns ["B"]
pluginB.Dependencies() // returns ["C"]
pluginC.Dependencies() // returns ["A"]  // 循环！

err := pluginManager.RegisterWithDependencies([]remilia.Plugin{pluginA, pluginB, pluginC})
// 返回: CircularDependencyError{Cycle: ["A", "B", "C", "A"]}

// 缺失依赖示例
pluginB.Dependencies() // returns ["missing-plugin"]

err := pluginManager.RegisterWithDependencies([]remilia.Plugin{pluginB})
// 返回: DependencyError{Plugin: "B", Dependency: "missing-plugin"}
```

#### 复杂依赖示例

```go
// 依赖关系图：
//       CachePlugin
//      /     |     \
//  AuthPl  LogPl  DataPl
//     \      |      /
//      \     |     /
//       ConfigPlugin

type ConfigPlugin struct {
    *remilia.BasePlugin
}

func (p *ConfigPlugin) Dependencies() []string {
    return []string{} // 基础插件，无依赖
}

type LoggerPlugin struct {
    *remilia.BasePlugin
}

func (p *LoggerPlugin) Dependencies() []string {
    return []string{"config"}
}

type AuthPlugin struct {
    *remilia.BasePlugin
}

func (p *AuthPlugin) Dependencies() []string {
    return []string{"config"}
}

type DatabasePlugin struct {
    *remilia.BasePlugin
}

func (p *DatabasePlugin) Dependencies() []string {
    return []string{"config"}
}

type CachePlugin struct {
    *remilia.BasePlugin
}

func (p *CachePlugin) Dependencies() []string {
    return []string{"auth", "logger", "database"}
}

// 注册所有插件
plugins := []remilia.Plugin{
    NewCachePlugin(),
    NewAuthPlugin(),
    NewDatabasePlugin(),
    NewLoggerPlugin(),
    NewConfigPlugin(),
}

err := pluginManager.RegisterWithDependencies(plugins)
// 自动解析为：config -> (auth, logger, database) -> cache
```

#### 最佳实践

1. **声明最小依赖**：只声明直接依赖，不需要声明传递依赖
   ```go
   // ✅ 正确：只声明直接依赖
   func (p *CachePlugin) Dependencies() []string {
       return []string{"database"}
   }
   
   // ❌ 错误：不需要声明传递依赖
   func (p *CachePlugin) Dependencies() []string {
       return []string{"database", "config"} // config 是 database 的依赖
   }
   ```

2. **避免循环依赖**：设计插件时注意依赖方向
   ```go
   // ✅ 正确：单向依赖
   config -> logger -> database
   
   // ❌ 错误：循环依赖
   config -> logger -> database -> config
   ```

3. **基础插件优先**：将基础设施类插件（如配置、日志）设为无依赖
   ```go
   type ConfigPlugin struct {
       *remilia.BasePlugin
   }
   
   func (p *ConfigPlugin) Dependencies() []string {
       return []string{} // 基础插件
   }
   ```

4. **混合使用**：已注册的插件可作为依赖
   ```go
   // 先单独注册基础插件
   pluginManager.Register(configPlugin)
   
   // 再批量注册有依赖的插件
   pluginManager.RegisterWithDependencies([]remilia.Plugin{
       loggerPlugin,   // 依赖 config（已注册）
       databasePlugin, // 依赖 logger
   })
   ```


**注销流程：**
1. 检查插件是否存在
2. 调用插件的 `Unload()` 方法
3. 从管理器中移除插件

**可能的错误：**
- `ErrPluginNotFound` - 插件不存在
- 卸载失败 - 插件 `Unload()` 方法返回的错误

### 热重载插件

```go
err := pluginManager.Reload("plugin-name")
if err != nil {
    log.Printf("Failed to reload plugin: %v", err)
}
```

**热重载流程：**
1. 检查插件是否存在
2. 调用插件的 `Unload()` 方法清理资源
3. 再次调用插件的 `Load()` 方法重新加载
4. 如果加载失败，插件会从管理器中移除

**使用场景：**
- 开发调试时快速更新插件逻辑
- 生产环境修改插件配置后重新加载
- 临时禁用/启用插件排查问题

**注意事项：**
- 热重载会清理插件的所有 matcher
- 插件内的状态（如计数器）需要自行保存和恢复
- 如果 Load 失败，插件将被从管理器中移除

### 查询插件

```go
// 获取单个插件
plugin, exists := pluginManager.Get("plugin-name")
if exists {
    // 使用插件
}

// 列出所有插件名称
names := pluginManager.List()
for _, name := range names {
    fmt.Println(name)
}
```

## 插件开发

### 简单插件示例

```go
package plugins

import (
    "github.com/KomeiDiSanXian/remilia"
    "github.com/KomeiDiSanXian/remilia/openapi/dto"
    "github.com/sirupsen/logrus"
)

// EchoPlugin 回声插件
type EchoPlugin struct {
    *remilia.BasePlugin
}

func NewEchoPlugin() *EchoPlugin {
    return &EchoPlugin{
        BasePlugin: remilia.NewBasePlugin("echo"),
    }
}

func (p *EchoPlugin) Load(engine *remilia.Engine) error {
    logrus.Info("[EchoPlugin] Loading plugin")
    
    matcher := engine.On(
        remilia.OnGroupAtMessage(),
        remilia.OnCommand("/echo "),
    ).Handle(func(ctx *remilia.Context) {
        content := ctx.GetMessageContent()
        echoText := strings.TrimPrefix(content, "/echo ")
        
        _, err := ctx.ReplyGroup(&dto.Message{
            Content: "🔊 " + echoText,
            Type:    dto.TextMessage,
        })
        if err != nil {
            logrus.WithError(err).Error("[EchoPlugin] 发送消息失败")
        }
    })
    
    p.AddMatcher(matcher)
    return nil
}
```

### 带状态的插件示例

```go
type StatsPlugin struct {
    *remilia.BasePlugin
    messageCount int
    mu           sync.Mutex
}

func NewStatsPlugin() *StatsPlugin {
    return &StatsPlugin{
        BasePlugin: remilia.NewBasePlugin("stats"),
    }
}

func (p *StatsPlugin) Load(engine *remilia.Engine) error {
    logrus.Info("[StatsPlugin] Loading plugin")
    
    // 添加前置处理器统计消息
    engine.AddPreHandler(func(ctx *remilia.Context) bool {
        p.mu.Lock()
        p.messageCount++
        p.mu.Unlock()
        return true
    })
    
    // 查询统计
    matcher := engine.On(
        remilia.OnGroupAtMessage(),
        remilia.OnCommand("/stats"),
    ).Handle(func(ctx *remilia.Context) {
        p.mu.Lock()
        count := p.messageCount
        p.mu.Unlock()
        
        _, _ = ctx.ReplyGroup(&dto.Message{
            Content: fmt.Sprintf("总消息数：%d", count),
            Type:    dto.TextMessage,
        })
    })
    
    p.AddMatcher(matcher)
    return nil
}

func (p *StatsPlugin) Unload(engine *remilia.Engine) error {
    logrus.Info("[StatsPlugin] Saving stats before unload")
    // 可以在这里保存状态到文件或数据库
    return p.BasePlugin.Unload(engine)
}
```

### 复杂插件示例

```go
type TimerPlugin struct {
    *remilia.BasePlugin
    ticker   *time.Ticker
    stopChan chan bool
}

func NewTimerPlugin() *TimerPlugin {
    return &TimerPlugin{
        BasePlugin: remilia.NewBasePlugin("timer"),
        stopChan:   make(chan bool),
    }
}

func (p *TimerPlugin) Load(engine *remilia.Engine) error {
    logrus.Info("[TimerPlugin] Loading plugin")
    
    // 启动定时任务
    p.ticker = time.NewTicker(1 * time.Hour)
    go func() {
        for {
            select {
            case <-p.ticker.C:
                // 执行定时任务
                logrus.Info("[TimerPlugin] Running scheduled task")
            case <-p.stopChan:
                return
            }
        }
    }()
    
    matcher := engine.On(
        remilia.OnGroupAtMessage(),
        remilia.OnCommand("/timer"),
    ).Handle(func(ctx *remilia.Context) {
        _, _ = ctx.ReplyGroup(&dto.Message{
            Content: "定时器正在运行",
            Type:    dto.TextMessage,
        })
    })
    
    p.AddMatcher(matcher)
    return nil
}

func (p *TimerPlugin) Unload(engine *remilia.Engine) error {
    logrus.Info("[TimerPlugin] Stopping timer")
    
    // 停止定时器
    if p.ticker != nil {
        p.ticker.Stop()
    }
    
    // 停止 goroutine
    close(p.stopChan)
    
    return p.BasePlugin.Unload(engine)
}
```

## 错误处理

### 自定义错误

```go
var (
    ErrPluginAlreadyExists = errors.New("plugin already exists")
    ErrPluginNotFound      = errors.New("plugin not found")
)
```

### 错误处理示例

```go
err := pluginManager.Register(plugin)
if err != nil {
    switch {
    case errors.Is(err, remilia.ErrPluginAlreadyExists):
        log.Println("Plugin already registered")
    default:
        log.Printf("Failed to register plugin: %v", err)
    }
}
```

## 最佳实践

### 1. 资源清理

插件的 `Unload()` 方法应该清理所有资源：

```go
func (p *MyPlugin) Unload(engine *Engine) error {
    // 停止 goroutine
    close(p.stopChan)
    
    // 关闭连接
    if p.conn != nil {
        p.conn.Close()
    }
    
    // 保存状态
    p.saveState()
    
    // 调用基类清理 matchers
    return p.BasePlugin.Unload(engine)
}
```

### 2. 线程安全

插件内部的状态访问应该使用互斥锁保护：

```go
type MyPlugin struct {
    *remilia.BasePlugin
    counter int
    mu      sync.Mutex
}

func (p *MyPlugin) incrementCounter() {
    p.mu.Lock()
    defer p.mu.Unlock()
    p.counter++
}
```

### 3. 错误处理

Load 和 Unload 方法应该返回有意义的错误：

```go
func (p *MyPlugin) Load(engine *Engine) error {
    conn, err := sql.Open("mysql", dsn)
    if err != nil {
        return fmt.Errorf("failed to connect to database: %w", err)
    }
    p.conn = conn
    
    // ... 其他初始化
    return nil
}
```

### 4. 日志记录

使用日志记录插件的重要操作：

```go
func (p *MyPlugin) Load(engine *Engine) error {
    logrus.Info("[MyPlugin] Loading plugin")
    
    // ... 加载逻辑
    
    logrus.Info("[MyPlugin] Plugin loaded successfully")
    return nil
}
```

### 5. 配置管理

使用结构体传递配置：

```go
type MyPluginConfig struct {
    Enabled  bool
    Interval time.Duration
}

func NewMyPluginWithConfig(config MyPluginConfig) *MyPlugin {
    p := &MyPlugin{
        BasePlugin: remilia.NewBasePlugin("my-plugin"),
        config:     config,
    }
    return p
}
```

## 使用示例

### 基本使用

```go
func main() {
    engine := remilia.GetGlobalEngine()
    pm := remilia.NewPluginManager(engine)
    
    // 注册插件
    _ = pm.Register(plugins.NewEchoPlugin())
    _ = pm.Register(plugins.NewDicePlugin())
    _ = pm.Register(plugins.NewStatsPlugin())
    
    // 启动引擎
    // ...
}
```

### 动态管理

```go
// 注册插件
if err := pm.Register(plugin); err != nil {
    log.Printf("Failed to register: %v", err)
    return
}

// 热重载插件
if err := pm.Reload("stats"); err != nil {
    log.Printf("Failed to reload: %v", err)
}

// 注销插件
if err := pm.Unregister("echo"); err != nil {
    log.Printf("Failed to unregister: %v", err)
}
```

## 注意事项

1. **插件名称唯一性** - 每个插件必须有唯一的名称
2. **资源清理** - Unload 方法必须清理所有资源（goroutine、连接等）
3. **线程安全** - 插件内部状态访问需要保护
4. **错误处理** - Load/Unload 应返回有意义的错误信息
5. **热重载限制** - 热重载会清空插件状态，需要自行保存和恢复
6. **循环依赖** - 避免插件之间的循环依赖

## 性能建议

1. **轻量级 Load** - Load 方法应该快速完成，避免阻塞
2. **批量操作** - 使用批量添加 matcher 而不是逐个添加
3. **惰性初始化** - 非必要资源可以延迟到首次使用时初始化
4. **合理使用锁** - 减少锁的持有时间，避免死锁

## 相关文档

- [快速开始](QUICKSTART.md)
- [使用指南](GUIDE.md)
- [架构文档](ARCHITECTURE.md)

