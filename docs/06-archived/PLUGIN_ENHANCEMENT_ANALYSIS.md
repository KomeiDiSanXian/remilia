# Plugin 系统增强需求分析

**日期**: 2026-02-07  
**当前版本**: v0.9.0  
**分析范围**: 插件系统的功能完善和用户体验优化

---

## 📊 当前已实现功能

### ✅ 核心功能（已完成）

1. **基础插件接口**
   - `Name()` - 获取插件名称
   - `Load()` / `Unload()` / `Reload()` - 生命周期管理
   - `Dependencies()` - 依赖声明

2. **元数据系统**（✨ 新增）
   - `PluginMetadata` 结构：名称、版本、作者、描述、帮助文本等
   - `MetadataProvider` 接口：可选实现
   - 与 Help 插件集成

3. **便捷方法**（✨ 新增）
   - `OnCommand()` - 自动注册命令并添加到 Matcher 列表
   - `On()` / `OnAny()` - 自动注册规则
   - 避免手动调用 `AddMatcher()`

4. **插件管理器**
   - 插件注册、卸载、重载
   - 依赖解析和拓扑排序
   - 循环依赖检测
   - 级联卸载

5. **生命周期监听器**
   - `OnPluginLoaded` / `OnPluginUnloaded` / `OnPluginReloaded`
   - `OnPluginError`

---

## 🎯 需要的增强功能

### 🔥 高优先级（用户体验关键）

#### 1. 插件配置管理 ⭐⭐⭐⭐⭐

**问题**：
- 插件配置混杂在全局 config.yaml 中
- 无法独立管理每个插件的配置
- 配置变更需要重启整个 Bot

**建议方案**：

```go
// 插件配置接口
type PluginConfig interface {
    // 获取配置
    Get(key string) interface{}
    GetString(key string, defaultVal string) string
    GetInt(key string, defaultVal int) int
    GetBool(key string, defaultVal bool) bool
    GetDuration(key string, defaultVal time.Duration) time.Duration
    
    // 设置配置（运行时）
    Set(key string, value interface{}) error
    
    // 重载配置
    Reload() error
    
    // 监听配置变化
    OnChange(handler func(key string, oldVal, newVal interface{}))
}

// 增强 BasePlugin
type BasePlugin struct {
    // ...existing fields...
    config PluginConfig
}

// 插件可以访问自己的配置
func (p *MyPlugin) Load(eng *engine.Engine) error {
    // 读取配置
    timeout := p.GetConfig().GetDuration("timeout", 10*time.Second)
    apiKey := p.GetConfig().GetString("api_key", "")
    
    // 监听配置变化
    p.GetConfig().OnChange(func(key string, oldVal, newVal interface{}) {
        logger.Infof("[Plugin] Config changed: %s = %v", key, newVal)
    })
    
    return nil
}
```

**配置文件结构**：

```yaml
# config.yaml
plugins:
  weather:
    enabled: true
    api_key: "your-api-key"
    timeout: "10s"
    cache_duration: "5m"
  
  echo:
    enabled: true
    max_length: 1000
```

**收益**：
- ✅ 配置独立管理
- ✅ 支持热更新（无需重启）
- ✅ 配置验证和默认值
- ✅ 更清晰的配置结构

---

#### 2. 插件状态查询 ⭐⭐⭐⭐⭐

**问题**：
- 无法查询插件的运行状态
- 不知道插件是否已加载、是否出错
- 调试困难

**建议方案**：

```go
// 插件状态
type PluginState int

const (
    PluginStateUnloaded PluginState = iota  // 未加载
    PluginStateLoading                      // 加载中
    PluginStateLoaded                       // 已加载
    PluginStateError                        // 错误状态
    PluginStateReloading                    // 重载中
)

// 插件状态信息
type PluginStatus struct {
    Name         string
    State        PluginState
    LoadTime     time.Time
    LastError    error
    MatcherCount int
    Metadata     *PluginMetadata
}

// Manager 新增方法
func (pm *Manager) GetStatus(name string) (*PluginStatus, error)
func (pm *Manager) ListStatus() map[string]*PluginStatus
func (pm *Manager) IsLoaded(name string) bool
func (pm *Manager) GetLoadOrder() []string  // 按加载顺序返回
```

**使用示例**：

```go
// 查询插件状态
status, _ := pluginManager.GetStatus("weather")
fmt.Printf("Plugin: %s, State: %v, Matchers: %d\n", 
    status.Name, status.State, status.MatcherCount)

// 列出所有插件
for name, status := range pluginManager.ListStatus() {
    fmt.Printf("- %s: %v\n", name, status.State)
}
```

**收益**：
- ✅ 实时查询插件状态
- ✅ 便于监控和调试
- ✅ 可用于管理界面展示

---

#### 3. 插件资源管理 ⭐⭐⭐⭐

**问题**：
- 插件自己管理资源（HTTP Client、数据库连接等）
- 卸载时可能忘记释放资源，导致泄漏
- 资源无法复用

**建议方案**：

```go
// 资源管理接口
type ResourceManager interface {
    // 注册资源
    Register(key string, resource interface{}) error
    
    // 获取资源
    Get(key string) (interface{}, bool)
    
    // 释放所有资源
    ReleaseAll() error
}

// 可关闭资源
type CloseableResource interface {
    Close() error
}

// BasePlugin 增强
type BasePlugin struct {
    // ...existing fields...
    resources ResourceManager
}

// 插件使用
func (p *MyPlugin) Load(eng *engine.Engine) error {
    // 创建资源
    client := &http.Client{Timeout: 10 * time.Second}
    
    // 注册资源（卸载时自动关闭）
    p.RegisterResource("http_client", client)
    
    db, _ := sql.Open("mysql", "...")
    p.RegisterResource("database", db)
    
    return nil
}

// 使用资源
func (p *MyPlugin) handleRequest(ctx *context.Context) error {
    client, _ := p.GetResource("http_client")
    httpClient := client.(*http.Client)
    
    // 使用 client
    resp, _ := httpClient.Get("...")
    // ...
}

// Unload 时自动释放
func (p *BasePlugin) Unload(eng *engine.Engine) error {
    // 自动关闭所有资源
    p.resources.ReleaseAll()
    
    // ...existing code...
}
```

**收益**：
- ✅ 自动管理资源生命周期
- ✅ 防止资源泄漏
- ✅ 统一的资源管理模式

---

#### 4. 插件间通信（事件总线）⭐⭐⭐⭐

**问题**：
- 插件间无法通信
- 紧耦合，必须通过依赖关系
- 无法实现松耦合的插件协作

**建议方案**：

```go
// 事件总线
type EventBus interface {
    // 发布事件
    Publish(topic string, data interface{}) error
    
    // 订阅事件
    Subscribe(topic string, handler func(data interface{})) (Subscription, error)
    
    // 取消订阅
    Unsubscribe(sub Subscription) error
}

// 使用示例
func (p *MyPlugin) Load(eng *engine.Engine) error {
    // 订阅其他插件的事件
    p.SubscribeEvent("user.login", func(data interface{}) {
        userID := data.(string)
        logger.Infof("User logged in: %s", userID)
    })
    
    return nil
}

func (p *MyPlugin) handleLogin(ctx *context.Context) error {
    userID := "user123"
    
    // 发布事件，其他插件可以监听
    p.PublishEvent("user.login", userID)
    
    return nil
}
```

**内置事件**：
- `plugin.loaded` - 插件加载完成
- `plugin.unloaded` - 插件卸载
- `plugin.error` - 插件错误
- `system.config_reload` - 系统配置重载
- `system.shutdown` - 系统关闭

**收益**：
- ✅ 插件解耦
- ✅ 灵活的协作模式
- ✅ 易于扩展功能

---

### 🔶 中优先级（开发体验提升）

#### 5. 插件存储抽象 ⭐⭐⭐

**问题**：
- 插件需要持久化数据时，自己管理存储
- 没有统一的存储接口
- 难以切换存储后端

**建议方案**：

```go
// 插件存储接口
type PluginStorage interface {
    // KV 操作
    Get(key string) ([]byte, error)
    Set(key string, value []byte) error
    Delete(key string) error
    
    // 列出键
    List(prefix string) ([]string, error)
    
    // 清空
    Clear() error
}

// BasePlugin 提供存储访问
func (p *BasePlugin) GetStorage() PluginStorage

// 使用示例
func (p *MyPlugin) saveUserData(userID string, data interface{}) error {
    bytes, _ := json.Marshal(data)
    return p.GetStorage().Set("user:"+userID, bytes)
}

func (p *MyPlugin) loadUserData(userID string) (interface{}, error) {
    bytes, err := p.GetStorage().Get("user:" + userID)
    if err != nil {
        return nil, err
    }
    
    var data interface{}
    json.Unmarshal(bytes, &data)
    return data, nil
}
```

**支持的后端**：
- 内存存储（默认）
- 文件存储（JSON/YAML）
- Redis
- 数据库（MySQL/PostgreSQL）

**收益**：
- ✅ 统一的存储接口
- ✅ 易于切换存储后端
- ✅ 插件数据隔离

---

#### 6. 插件指标收集 ⭐⭐⭐

**问题**：
- 无法了解插件的性能表现
- 不知道插件处理了多少请求、耗时多久
- 缺少监控数据

**建议方案**：

```go
// 插件指标接口
type PluginMetrics interface {
    // 计数器
    IncCounter(name string, labels map[string]string)
    
    // 直方图
    ObserveHistogram(name string, value float64, labels map[string]string)
    
    // 仪表盘
    SetGauge(name string, value float64, labels map[string]string)
}

// BasePlugin 提供指标访问
func (p *BasePlugin) Metrics() PluginMetrics

// 使用示例
func (p *MyPlugin) handleRequest(ctx *context.Context) error {
    start := time.Now()
    defer func() {
        duration := time.Since(start).Seconds()
        
        // 记录指标
        p.Metrics().IncCounter("requests_total", map[string]string{
            "plugin": p.Name(),
            "status": "success",
        })
        
        p.Metrics().ObserveHistogram("request_duration_seconds", duration, 
            map[string]string{"plugin": p.Name()})
    }()
    
    // 处理请求
    // ...
    
    return nil
}
```

**自动收集的指标**：
- `plugin_load_duration_seconds` - 加载耗时
- `plugin_matcher_count` - Matcher 数量
- `plugin_errors_total` - 错误总数
- `plugin_uptime_seconds` - 运行时间

**收益**：
- ✅ 实时性能监控
- ✅ 问题排查
- ✅ 容量规划

---

#### 7. 插件日志增强 ⭐⭐⭐

**问题**：
- 插件日志混在一起，难以区分
- 无法单独控制插件的日志级别

**建议方案**：

```go
// BasePlugin 提供专用 Logger
func (p *BasePlugin) Logger() *logger.Logger

// 使用示例
func (p *MyPlugin) Load(eng *engine.Engine) error {
    p.Logger().Info("Loading plugin...")
    
    // 日志自动带插件名称
    // 输出: [Plugin:weather] Loading plugin...
    
    return nil
}

// 支持动态调整日志级别
pluginManager.SetLogLevel("weather", "debug")
```

**收益**：
- ✅ 日志隔离和过滤
- ✅ 独立的日志级别控制
- ✅ 更好的调试体验

---

#### 8. 插件版本管理 ⭐⭐⭐

**问题**：
- 无法检查插件版本兼容性
- 升级插件时可能出现不兼容

**建议方案**：

```go
// 版本约束
type VersionConstraint struct {
    MinVersion string  // 最低版本
    MaxVersion string  // 最高版本
    Exact      string  // 精确版本
}

// 插件依赖增强
type Dependency struct {
    Name       string
    Version    *VersionConstraint
    Optional   bool
}

// Plugin 接口增强
type Plugin interface {
    // ...existing methods...
    
    // 返回详细依赖信息
    GetDependencies() []Dependency
}

// 使用示例
func (p *MyPlugin) GetDependencies() []Dependency {
    return []Dependency{
        {
            Name: "database",
            Version: &VersionConstraint{
                MinVersion: "1.0.0",
                MaxVersion: "2.0.0",
            },
        },
        {
            Name: "cache",
            Optional: true,
        },
    }
}
```

**Manager 增强**：

```go
// 检查版本兼容性
func (pm *Manager) CheckCompatibility(plugin Plugin) error

// 获取可用更新
func (pm *Manager) GetAvailableUpdates() []PluginUpdate
```

**收益**：
- ✅ 版本兼容性检查
- ✅ 安全的插件升级
- ✅ 更清晰的依赖关系

---

### 🔷 低优先级（高级特性）

#### 9. 插件权限控制 ⭐⭐

**问题**：
- 插件可以随意访问 Engine
- 缺少权限隔离
- 安全风险

**建议方案**：

```go
// 权限定义
type Permission string

const (
    PermRegisterMatcher Permission = "register_matcher"
    PermAccessStorage   Permission = "access_storage"
    PermAccessNetwork   Permission = "access_network"
    PermAccessDatabase  Permission = "access_database"
)

// 插件声明所需权限
func (p *MyPlugin) RequiredPermissions() []Permission {
    return []Permission{
        PermRegisterMatcher,
        PermAccessNetwork,
    }
}

// Manager 检查权限
func (pm *Manager) Register(plugin Plugin) error {
    // 检查权限
    for _, perm := range plugin.RequiredPermissions() {
        if !pm.hasPermission(plugin.Name(), perm) {
            return fmt.Errorf("permission denied: %s", perm)
        }
    }
    
    // ...existing code...
}
```

---

#### 10. 插件热重载增强 ⭐⭐

**问题**：
- 重载可能影响正在处理的请求
- 无法平滑过渡

**建议方案**：

```go
// 优雅重载
type ReloadStrategy int

const (
    ReloadImmediately ReloadStrategy = iota  // 立即重载
    ReloadGracefully                         // 优雅重载（等待请求完成）
    ReloadBlueGreen                          // 蓝绿部署
)

// Manager 支持不同重载策略
func (pm *Manager) ReloadWithStrategy(name string, strategy ReloadStrategy) error
```

---

#### 11. 插件市场支持 ⭐

**问题**：
- 无法发现和安装第三方插件
- 插件分发困难

**建议方案**：

```go
// 插件市场客户端
type PluginMarket interface {
    // 搜索插件
    Search(keyword string) ([]PluginInfo, error)
    
    // 安装插件
    Install(name, version string) error
    
    // 更新插件
    Update(name string) error
    
    // 卸载插件
    Uninstall(name string) error
}

// 使用
market := NewPluginMarket("https://plugins.remilia.dev")
market.Install("weather", "1.0.0")
```

---

## 📈 实施优先级建议

### Phase 1: 核心体验（1-2周）
1. ✅ **便捷方法**（已完成）- `OnCommand` 等
2. ✅ **元数据系统**（已完成）- 帮助文本支持
3. 🔥 **插件配置管理** - 独立配置
4. 🔥 **插件状态查询** - 实时状态

### Phase 2: 开发体验（2-3周）
5. **插件资源管理** - 自动释放资源
6. **插件日志增强** - 独立日志
7. **插件存储抽象** - 统一存储接口

### Phase 3: 高级特性（3-4周）
8. **插件间通信** - 事件总线
9. **插件指标收集** - 性能监控
10. **插件版本管理** - 兼容性检查

### Phase 4: 企业特性（可选）
11. **插件权限控制** - 安全隔离
12. **插件热重载增强** - 优雅重载
13. **插件市场支持** - 插件分发

---

## 💡 快速实施建议

### 最小可行方案（MVP）

如果时间有限，建议**优先实施以下 3 项**：

1. **插件配置管理**（高收益）
   - 时间：2-3天
   - 收益：配置独立、热更新
   - 实现难度：中

2. **插件状态查询**（高收益）
   - 时间：1-2天
   - 收益：实时监控、调试方便
   - 实现难度：低

3. **插件资源管理**（高收益）
   - 时间：2-3天
   - 收益：防止资源泄漏
   - 实现难度：中

这 3 项加起来约 **1 周**时间，可以显著提升用户体验。

---

## 🎯 总结

### 当前已实现（✅）
- 基础生命周期管理
- 依赖解析和拓扑排序
- 元数据系统和 Help 集成
- 便捷的 OnCommand 等方法

### 最需要的增强（🔥）
1. **插件配置管理** - 独立配置、热更新
2. **插件状态查询** - 实时状态监控
3. **插件资源管理** - 自动释放资源
4. **插件间通信** - 事件总线

### 实施建议
- **短期**（1周）：配置管理 + 状态查询 + 资源管理
- **中期**（1月）：事件总线 + 日志增强 + 存储抽象
- **长期**（3月）：指标收集 + 版本管理 + 权限控制

---

**结论**：当前插件系统的基础已经很扎实，主要需要补充的是**运维友好**和**开发体验**相关的功能。建议优先实施配置管理、状态查询和资源管理这三项，可以在较短时间内显著提升易用性。

