# Remilia 插件系统增强方案

**文档版本**: v1.0  
**创建日期**: 2026-01-23  
**目标版本**: v1.0.0

---

## 📋 目录

1. [现状分析](#1-现状分析)
2. [核心增强方案](#2-核心增强方案)
3. [高级特性](#3-高级特性)
4. [架构优化](#4-架构优化)
5. [实施路线图](#5-实施路线图)
6. [风险评估](#6-风险评估)

---

## 1. 现状分析

### 1.1 当前插件系统功能

**核心能力**:
```go
type Plugin interface {
    Name() string
    Load(coordinator *engine.Engine) error
    Unload(coordinator *engine.Engine) error
    Reload(coordinator *engine.Engine) error
    Dependencies() []string
}
```

**已有特性**:
- ✅ 基本的插件注册和卸载
- ✅ 依赖管理（Dependencies）
- ✅ 原子性重载机制
- ✅ 生命周期监听器
- ✅ 级联卸载
- ✅ 插件分组和中间件支持

### 1.2 当前限制

#### 限制 1: 功能单一
- 插件只能注册 matcher，无法扩展其他能力
- 缺少插件间通信机制
- 无法动态修改系统行为

#### 限制 2: 配置管理
- 插件配置混杂在全局配置中
- 无法独立管理插件配置
- 缺少配置校验和热更新

#### 限制 3: 资源管理
- 插件资源（数据库连接、HTTP客户端等）管理混乱
- 缺少资源池和复用机制
- 插件卸载时可能残留资源

#### 限制 4: 可观测性
- 插件行为缺少监控
- 无法追踪插件性能影响
- 调试困难

#### 限制 5: 隔离性
- 插件可以随意访问 Engine
- 缺少权限控制
- 插件崩溃可能影响全局

---

## 2. 核心增强方案

### 2.1 插件上下文系统

#### 方案概述
为每个插件提供独立的上下文环境，隔离资源和权限。

#### 接口设计

```go
// PluginContext 插件上下文接口
type PluginContext interface {
    // 基础信息
    GetName() string
    GetVersion() string
    GetConfig() PluginConfig
    
    // Engine 访问（受限）
    RegisterMatcher(matcher *engine.Matcher) error
    UnregisterMatcher(id string) error
    RegisterHandler(eventType dto.EventType, handler context.Handler) error
    
    // 资源管理
    GetResource(key string) (interface{}, bool)
    SetResource(key string, value interface{})
    ReleaseResources() error
    
    // 事件总线
    Publish(topic string, data interface{}) error
    Subscribe(topic string, handler EventHandler) (Subscription, error)
    
    // 存储访问
    GetStorage() PluginStorage
    
    // 日志记录
    Logger() *logrus.Entry
    
    // 指标采集
    Metrics() PluginMetrics
}

// PluginConfig 插件配置
type PluginConfig interface {
    Get(key string) interface{}
    GetString(key string, defaultVal string) string
    GetInt(key string, defaultVal int) int
    GetBool(key string, defaultVal bool) bool
    GetDuration(key string, defaultVal time.Duration) time.Duration
    Set(key string, value interface{}) error
    Reload() error
}

// PluginStorage 插件存储接口
type PluginStorage interface {
    Get(key string) ([]byte, error)
    Set(key string, value []byte) error
    Delete(key string) error
    List(prefix string) ([]string, error)
    Clear() error
}

// PluginMetrics 插件指标接口
type PluginMetrics interface {
    IncCounter(name string, labels map[string]string)
    ObserveHistogram(name string, value float64, labels map[string]string)
    SetGauge(name string, value float64, labels map[string]string)
}
```

#### 实现示例

```go
package plugin

type pluginContext struct {
    name       string
    version    string
    config     PluginConfig
    resources  sync.Map
    engine     *engine.Engine
    eventBus   *EventBus
    storage    PluginStorage
    logger     *logrus.Entry
    metrics    PluginMetrics
    
    // 权限控制
    permissions map[string]bool
}

func NewPluginContext(name, version string, eng *engine.Engine) *pluginContext {
    return &pluginContext{
        name:    name,
        version: version,
        engine:  eng,
        eventBus: NewEventBus(),
        storage: NewMemoryStorage(name),
        logger:  logrus.WithField("plugin", name),
        metrics: NewPluginMetrics(name),
        permissions: make(map[string]bool),
    }
}

func (pc *pluginContext) RegisterMatcher(matcher *engine.Matcher) error {
    if !pc.hasPermission("register_matcher") {
        return fmt.Errorf("permission denied")
    }
    
    // 设置插件标识
    matcher.SetSource("plugin:" + pc.name)
    matcher.SetGroup(pc.name)
    
    return pc.engine.RegisterMatcher(matcher)
}

func (pc *pluginContext) GetResource(key string) (interface{}, bool) {
    return pc.resources.Load(key)
}

func (pc *pluginContext) SetResource(key string, value interface{}) {
    pc.resources.Store(key, value)
}

func (pc *pluginContext) ReleaseResources() error {
    var errors []error
    
    pc.resources.Range(func(key, value interface{}) bool {
        // 如果资源实现了 io.Closer，则关闭它
        if closer, ok := value.(io.Closer); ok {
            if err := closer.Close(); err != nil {
                errors = append(errors, err)
            }
        }
        pc.resources.Delete(key)
        return true
    })
    
    if len(errors) > 0 {
        return fmt.Errorf("failed to release resources: %v", errors)
    }
    return nil
}

func (pc *pluginContext) hasPermission(perm string) bool {
    allowed, exists := pc.permissions[perm]
    return exists && allowed
}
```

#### 使用示例

```go
// 增强的插件接口
type EnhancedPlugin interface {
    Plugin
    
    // LoadWithContext 使用上下文加载
    LoadWithContext(ctx PluginContext) error
    
    // UnloadWithContext 使用上下文卸载
    UnloadWithContext(ctx PluginContext) error
}

// 示例插件实现
type WeatherPlugin struct {
    *BasePlugin
    httpClient *http.Client
}

func (wp *WeatherPlugin) LoadWithContext(ctx PluginContext) error {
    // 创建 HTTP 客户端并存储为资源
    client := &http.Client{Timeout: 10 * time.Second}
    ctx.SetResource("http_client", client)
    
    // 注册命令处理器
    matcher := engine.NewMatcher().
        OnCommand("/weather").
        SetHandler(wp.handleWeather)
    
    if err := ctx.RegisterMatcher(matcher); err != nil {
        return err
    }
    
    // 订阅系统事件
    _, err := ctx.Subscribe("system.config_reload", func(data interface{}) {
        wp.onConfigReload(ctx, data)
    })
    
    return err
}

func (wp *WeatherPlugin) handleWeather(eventCtx *context.Context) error {
    // 从上下文获取资源
    pluginCtx := GetPluginContext(wp.Name())
    client, _ := pluginCtx.GetResource("http_client")
    
    // 使用客户端查询天气
    // ...
    
    // 记录指标
    pluginCtx.Metrics().IncCounter("weather_requests", nil)
    
    return nil
}
```

### 2.2 事件总线系统

#### 方案概述
实现插件间异步通信机制，解耦插件依赖。

#### 接口设计

```go
// EventBus 事件总线接口
type EventBus interface {
    // 发布事件
    Publish(topic string, event Event) error
    
    // 订阅事件
    Subscribe(topic string, handler EventHandler) (Subscription, error)
    
    // 订阅匹配模式的事件
    SubscribePattern(pattern string, handler EventHandler) (Subscription, error)
    
    // 取消订阅
    Unsubscribe(sub Subscription) error
    
    // 获取统计信息
    GetStats() EventBusStats
}

// Event 事件结构
type Event struct {
    Topic     string                 // 主题
    Publisher string                 // 发布者
    Data      interface{}            // 数据
    Metadata  map[string]string      // 元数据
    Timestamp time.Time              // 时间戳
}

// EventHandler 事件处理器
type EventHandler func(event Event) error

// Subscription 订阅句柄
type Subscription interface {
    Topic() string
    Unsubscribe() error
    ID() string
}

// EventBusStats 事件总线统计
type EventBusStats struct {
    TotalPublished   int64
    TotalSubscribers int
    TopicCount       int
}
```

#### 实现示例

```go
package plugin

type eventBus struct {
    subscribers map[string][]subscriber
    mu          sync.RWMutex
    stats       eventBusStats
}

type subscriber struct {
    id      string
    topic   string
    handler EventHandler
}

type eventBusStats struct {
    published atomic.Int64
}

func NewEventBus() *eventBus {
    return &eventBus{
        subscribers: make(map[string][]subscriber),
    }
}

func (eb *eventBus) Publish(topic string, event Event) error {
    eb.stats.published.Add(1)
    
    event.Topic = topic
    event.Timestamp = time.Now()
    
    eb.mu.RLock()
    subs := eb.subscribers[topic]
    eb.mu.RUnlock()
    
    // 异步调用订阅者
    for _, sub := range subs {
        go func(s subscriber) {
            defer func() {
                if r := recover(); r != nil {
                    logrus.WithFields(logrus.Fields{
                        "topic":      topic,
                        "subscriber": s.id,
                        "panic":      r,
                    }).Error("[EventBus] Handler panic")
                }
            }()
            
            if err := s.handler(event); err != nil {
                logrus.WithError(err).WithFields(logrus.Fields{
                    "topic":      topic,
                    "subscriber": s.id,
                }).Warn("[EventBus] Handler error")
            }
        }(sub)
    }
    
    return nil
}

func (eb *eventBus) Subscribe(topic string, handler EventHandler) (Subscription, error) {
    eb.mu.Lock()
    defer eb.mu.Unlock()
    
    sub := subscriber{
        id:      fmt.Sprintf("%s-%d", topic, time.Now().UnixNano()),
        topic:   topic,
        handler: handler,
    }
    
    eb.subscribers[topic] = append(eb.subscribers[topic], sub)
    
    return &subscription{
        id:    sub.id,
        topic: topic,
        bus:   eb,
    }, nil
}

type subscription struct {
    id    string
    topic string
    bus   *eventBus
}

func (s *subscription) ID() string {
    return s.id
}

func (s *subscription) Topic() string {
    return s.topic
}

func (s *subscription) Unsubscribe() error {
    return s.bus.Unsubscribe(s)
}
```

#### 使用示例

```go
// 插件 A: 发布事件
func (pa *PluginA) OnUserLogin(ctx PluginContext, userID string) {
    ctx.Publish("user.login", Event{
        Publisher: pa.Name(),
        Data: map[string]interface{}{
            "user_id": userID,
            "time":    time.Now(),
        },
    })
}

// 插件 B: 订阅事件
func (pb *PluginB) LoadWithContext(ctx PluginContext) error {
    _, err := ctx.Subscribe("user.login", func(event Event) error {
        data := event.Data.(map[string]interface{})
        userID := data["user_id"].(string)
        
        pb.logger.Infof("User %s logged in", userID)
        // 处理登录事件...
        
        return nil
    })
    return err
}
```

### 2.3 插件配置管理

#### 方案概述
为每个插件提供独立的配置命名空间，支持热更新和验证。

#### 配置文件结构

```yaml
# config.yaml
plugins:
  weather:
    enabled: true
    api_key: "your-api-key"
    cache_ttl: "5m"
    timeout: "10s"
    endpoints:
      - "https://api.weather.com"
      - "https://api2.weather.com"
  
  notification:
    enabled: true
    channels:
      - email
      - webhook
    webhook_url: "https://example.com/webhook"
```

#### 实现示例

```go
// PluginConfigManager 插件配置管理器
type PluginConfigManager struct {
    configs map[string]*PluginConfigImpl
    watcher *fsnotify.Watcher
    mu      sync.RWMutex
}

type PluginConfigImpl struct {
    name      string
    data      map[string]interface{}
    mu        sync.RWMutex
    validator func(config map[string]interface{}) error
    onChange  []func()
}

func NewPluginConfigManager() *PluginConfigManager {
    return &PluginConfigManager{
        configs: make(map[string]*PluginConfigImpl),
    }
}

func (pcm *PluginConfigManager) LoadFromFile(path string) error {
    data, err := os.ReadFile(path)
    if err != nil {
        return err
    }
    
    var config struct {
        Plugins map[string]map[string]interface{} `yaml:"plugins"`
    }
    
    if err := yaml.Unmarshal(data, &config); err != nil {
        return err
    }
    
    pcm.mu.Lock()
    defer pcm.mu.Unlock()
    
    for pluginName, pluginConfig := range config.Plugins {
        if _, exists := pcm.configs[pluginName]; !exists {
            pcm.configs[pluginName] = &PluginConfigImpl{
                name:     pluginName,
                data:     pluginConfig,
                onChange: make([]func(), 0),
            }
        } else {
            pcm.configs[pluginName].update(pluginConfig)
        }
    }
    
    return nil
}

func (pci *PluginConfigImpl) Get(key string) interface{} {
    pci.mu.RLock()
    defer pci.mu.RUnlock()
    return pci.data[key]
}

func (pci *PluginConfigImpl) GetString(key string, defaultVal string) string {
    if val := pci.Get(key); val != nil {
        if str, ok := val.(string); ok {
            return str
        }
    }
    return defaultVal
}

func (pci *PluginConfigImpl) SetValidator(validator func(map[string]interface{}) error) {
    pci.validator = validator
}

func (pci *PluginConfigImpl) OnChange(callback func()) {
    pci.onChange = append(pci.onChange, callback)
}

func (pci *PluginConfigImpl) update(newData map[string]interface{}) error {
    // 验证新配置
    if pci.validator != nil {
        if err := pci.validator(newData); err != nil {
            return err
        }
    }
    
    pci.mu.Lock()
    pci.data = newData
    callbacks := pci.onChange
    pci.mu.Unlock()
    
    // 触发回调
    for _, cb := range callbacks {
        cb()
    }
    
    return nil
}
```

#### 使用示例

```go
func (wp *WeatherPlugin) LoadWithContext(ctx PluginContext) error {
    config := ctx.GetConfig()
    
    // 设置配置验证器
    config.SetValidator(func(data map[string]interface{}) error {
        apiKey, ok := data["api_key"].(string)
        if !ok || apiKey == "" {
            return errors.New("api_key is required")
        }
        return nil
    })
    
    // 监听配置变更
    config.OnChange(func() {
        wp.logger.Info("Configuration reloaded")
        wp.reloadHTTPClient(config)
    })
    
    // 读取配置
    apiKey := config.GetString("api_key", "")
    timeout := config.GetDuration("timeout", 10*time.Second)
    
    // 使用配置初始化
    wp.apiKey = apiKey
    wp.timeout = timeout
    
    return nil
}
```

### 2.4 插件存储系统

#### 方案概述
为插件提供持久化存储能力，支持多种后端。

#### 接口设计

```go
// PluginStorage 插件存储接口
type PluginStorage interface {
    // 基础 KV 操作
    Get(key string) ([]byte, error)
    Set(key string, value []byte) error
    Delete(key string) error
    Exists(key string) (bool, error)
    
    // 批量操作
    MGet(keys []string) (map[string][]byte, error)
    MSet(kvs map[string][]byte) error
    MDelete(keys []string) error
    
    // 列表操作
    List(prefix string) ([]string, error)
    ListWithValues(prefix string) (map[string][]byte, error)
    
    // 范围操作
    Scan(prefix string, handler func(key string, value []byte) bool) error
    
    // 事务
    BeginTx() (StorageTransaction, error)
    
    // 清理
    Clear() error
    
    // 统计
    Size() (int64, error)
    Count() (int64, error)
}

// StorageTransaction 存储事务接口
type StorageTransaction interface {
    Get(key string) ([]byte, error)
    Set(key string, value []byte) error
    Delete(key string) error
    Commit() error
    Rollback() error
}

// StorageBackend 存储后端接口
type StorageBackend interface {
    Name() string
    Connect(config map[string]interface{}) error
    Close() error
    CreateNamespace(namespace string) (PluginStorage, error)
}
```

#### 内存实现

```go
package plugin

type memoryStorage struct {
    namespace string
    data      sync.Map
}

func NewMemoryStorage(namespace string) *memoryStorage {
    return &memoryStorage{
        namespace: namespace,
    }
}

func (ms *memoryStorage) Get(key string) ([]byte, error) {
    fullKey := ms.buildKey(key)
    val, ok := ms.data.Load(fullKey)
    if !ok {
        return nil, ErrKeyNotFound
    }
    return val.([]byte), nil
}

func (ms *memoryStorage) Set(key string, value []byte) error {
    fullKey := ms.buildKey(key)
    ms.data.Store(fullKey, value)
    return nil
}

func (ms *memoryStorage) Delete(key string) error {
    fullKey := ms.buildKey(key)
    ms.data.Delete(fullKey)
    return nil
}

func (ms *memoryStorage) List(prefix string) ([]string, error) {
    fullPrefix := ms.buildKey(prefix)
    keys := make([]string, 0)
    
    ms.data.Range(func(key, value interface{}) bool {
        keyStr := key.(string)
        if strings.HasPrefix(keyStr, fullPrefix) {
            // 移除命名空间前缀
            trimmed := strings.TrimPrefix(keyStr, ms.namespace+":")
            keys = append(keys, trimmed)
        }
        return true
    })
    
    return keys, nil
}

func (ms *memoryStorage) buildKey(key string) string {
    return fmt.Sprintf("%s:%s", ms.namespace, key)
}
```

#### Redis 实现

```go
package plugin

import "github.com/go-redis/redis/v8"

type redisStorage struct {
    namespace string
    client    *redis.Client
}

func NewRedisStorage(namespace string, client *redis.Client) *redisStorage {
    return &redisStorage{
        namespace: namespace,
        client:    client,
    }
}

func (rs *redisStorage) Get(key string) ([]byte, error) {
    ctx := context.Background()
    fullKey := rs.buildKey(key)
    
    val, err := rs.client.Get(ctx, fullKey).Bytes()
    if err == redis.Nil {
        return nil, ErrKeyNotFound
    }
    return val, err
}

func (rs *redisStorage) Set(key string, value []byte) error {
    ctx := context.Background()
    fullKey := rs.buildKey(key)
    return rs.client.Set(ctx, fullKey, value, 0).Err()
}

func (rs *redisStorage) List(prefix string) ([]string, error) {
    ctx := context.Background()
    fullPrefix := rs.buildKey(prefix)
    
    var cursor uint64
    var keys []string
    
    for {
        var batch []string
        var err error
        
        batch, cursor, err = rs.client.Scan(ctx, cursor, fullPrefix+"*", 100).Result()
        if err != nil {
            return nil, err
        }
        
        // 移除命名空间前缀
        for _, key := range batch {
            trimmed := strings.TrimPrefix(key, rs.namespace+":")
            keys = append(keys, trimmed)
        }
        
        if cursor == 0 {
            break
        }
    }
    
    return keys, nil
}

func (rs *redisStorage) buildKey(key string) string {
    return fmt.Sprintf("%s:%s", rs.namespace, key)
}
```

#### 使用示例

```go
func (wp *WeatherPlugin) LoadWithContext(ctx PluginContext) error {
    storage := ctx.GetStorage()
    
    // 缓存天气数据
    weatherData, err := wp.fetchWeather("Beijing")
    if err != nil {
        return err
    }
    
    data, _ := json.Marshal(weatherData)
    storage.Set("cache:beijing", data)
    
    // 读取缓存
    cachedData, err := storage.Get("cache:beijing")
    if err == nil {
        var cached WeatherData
        json.Unmarshal(cachedData, &cached)
    }
    
    // 列出所有缓存
    keys, _ := storage.List("cache:")
    wp.logger.Infof("Found %d cached items", len(keys))
    
    return nil
}
```

### 2.5 插件权限系统

#### 方案概述
实现细粒度的权限控制，限制插件可访问的功能和资源。

#### 权限模型

```go
// Permission 权限定义
type Permission string

const (
    // Engine 相关权限
    PermRegisterMatcher   Permission = "engine.register_matcher"
    PermUnregisterMatcher Permission = "engine.unregister_matcher"
    PermAccessState       Permission = "engine.access_state"
    
    // 事件总线权限
    PermPublishEvent   Permission = "eventbus.publish"
    PermSubscribeEvent Permission = "eventbus.subscribe"
    
    // 存储权限
    PermStorageRead  Permission = "storage.read"
    PermStorageWrite Permission = "storage.write"
    
    // 网络权限
    PermNetworkHTTP Permission = "network.http"
    PermNetworkTCP  Permission = "network.tcp"
    
    // 系统权限
    PermAccessConfig  Permission = "system.config"
    PermAccessMetrics Permission = "system.metrics"
)

// PermissionSet 权限集合
type PermissionSet struct {
    permissions map[Permission]bool
    mu          sync.RWMutex
}

func NewPermissionSet() *PermissionSet {
    return &PermissionSet{
        permissions: make(map[Permission]bool),
    }
}

func (ps *PermissionSet) Grant(perm Permission) {
    ps.mu.Lock()
    defer ps.mu.Unlock()
    ps.permissions[perm] = true
}

func (ps *PermissionSet) Revoke(perm Permission) {
    ps.mu.Lock()
    defer ps.mu.Unlock()
    ps.permissions[perm] = false
}

func (ps *PermissionSet) Has(perm Permission) bool {
    ps.mu.RLock()
    defer ps.mu.RUnlock()
    return ps.permissions[perm]
}

func (ps *PermissionSet) Check(perm Permission) error {
    if !ps.Has(perm) {
        return fmt.Errorf("permission denied: %s", perm)
    }
    return nil
}
```

#### 预定义角色

```go
// Role 角色定义
type Role string

const (
    RoleBasic    Role = "basic"    // 基础权限
    RoleStandard Role = "standard" // 标准权限
    RoleFull     Role = "full"     // 完整权限
    RoleAdmin    Role = "admin"    // 管理员权限
)

// RolePermissions 角色权限映射
var RolePermissions = map[Role][]Permission{
    RoleBasic: {
        PermRegisterMatcher,
        PermStorageRead,
        PermStorageWrite,
        PermSubscribeEvent,
    },
    RoleStandard: {
        PermRegisterMatcher,
        PermUnregisterMatcher,
        PermStorageRead,
        PermStorageWrite,
        PermPublishEvent,
        PermSubscribeEvent,
        PermNetworkHTTP,
    },
    RoleFull: {
        PermRegisterMatcher,
        PermUnregisterMatcher,
        PermAccessState,
        PermStorageRead,
        PermStorageWrite,
        PermPublishEvent,
        PermSubscribeEvent,
        PermNetworkHTTP,
        PermNetworkTCP,
        PermAccessMetrics,
    },
    RoleAdmin: {
        // 所有权限
        PermRegisterMatcher,
        PermUnregisterMatcher,
        PermAccessState,
        PermStorageRead,
        PermStorageWrite,
        PermPublishEvent,
        PermSubscribeEvent,
        PermNetworkHTTP,
        PermNetworkTCP,
        PermAccessConfig,
        PermAccessMetrics,
    },
}

// ApplyRole 应用角色权限
func (ps *PermissionSet) ApplyRole(role Role) {
    perms, exists := RolePermissions[role]
    if !exists {
        return
    }
    
    for _, perm := range perms {
        ps.Grant(perm)
    }
}
```

#### 权限检查

```go
type securePluginContext struct {
    *pluginContext
    permissions *PermissionSet
}

func (spc *securePluginContext) RegisterMatcher(matcher *engine.Matcher) error {
    if err := spc.permissions.Check(PermRegisterMatcher); err != nil {
        return err
    }
    return spc.pluginContext.RegisterMatcher(matcher)
}

func (spc *securePluginContext) Publish(topic string, data interface{}) error {
    if err := spc.permissions.Check(PermPublishEvent); err != nil {
        return err
    }
    return spc.pluginContext.Publish(topic, data)
}
```

#### 配置示例

```yaml
plugins:
  weather:
    enabled: true
    role: standard  # 使用预定义角色
    # 或者自定义权限
    permissions:
      - engine.register_matcher
      - storage.read
      - storage.write
      - network.http
```

---

## 3. 高级特性

### 3.1 插件热重载

#### 当前问题
- `Reload` 方法需要重启整个插件
- 配置变更需要卸载再加载
- 重载期间服务中断

#### 增强方案

```go
// HotReloadable 热重载接口
type HotReloadable interface {
    Plugin
    
    // CanHotReload 检查是否支持热重载
    CanHotReload() bool
    
    // PrepareReload 准备重载（预加载新状态）
    PrepareReload(ctx PluginContext) (ReloadState, error)
    
    // CommitReload 提交重载（原子切换）
    CommitReload(state ReloadState) error
    
    // RollbackReload 回滚重载
    RollbackReload(state ReloadState) error
}

// ReloadState 重载状态
type ReloadState interface {
    Snapshot() interface{}
    Restore(snapshot interface{}) error
}
```

#### 实现示例

```go
type HotReloadableWeatherPlugin struct {
    *WeatherPlugin
    
    currentState atomic.Value // *pluginState
}

type pluginState struct {
    httpClient *http.Client
    apiKey     string
    endpoints  []string
}

func (wp *HotReloadableWeatherPlugin) PrepareReload(ctx PluginContext) (ReloadState, error) {
    // 读取新配置
    config := ctx.GetConfig()
    apiKey := config.GetString("api_key", "")
    endpoints := config.GetStringSlice("endpoints")
    
    // 创建新状态
    newState := &pluginState{
        httpClient: &http.Client{
            Timeout: config.GetDuration("timeout", 10*time.Second),
        },
        apiKey:    apiKey,
        endpoints: endpoints,
    }
    
    // 验证新状态
    if err := wp.validateState(newState); err != nil {
        return nil, err
    }
    
    return &reloadState{
        oldState: wp.currentState.Load().(*pluginState),
        newState: newState,
    }, nil
}

func (wp *HotReloadableWeatherPlugin) CommitReload(state ReloadState) error {
    rs := state.(*reloadState)
    
    // 原子切换状态
    wp.currentState.Store(rs.newState)
    
    // 清理旧状态资源
    if rs.oldState.httpClient != nil {
        rs.oldState.httpClient.CloseIdleConnections()
    }
    
    return nil
}

type reloadState struct {
    oldState *pluginState
    newState *pluginState
}

func (rs *reloadState) Snapshot() interface{} {
    return rs.oldState
}

func (rs *reloadState) Restore(snapshot interface{}) error {
    rs.newState = snapshot.(*pluginState)
    return nil
}
```

### 3.2 插件沙箱隔离

#### 方案概述
使用 goroutine 和资源限制实现插件隔离，防止单个插件影响全局。

#### 设计

```go
// PluginSandbox 插件沙箱
type PluginSandbox struct {
    plugin      Plugin
    limits      SandboxLimits
    monitor     *ResourceMonitor
    ctx         context.Context
    cancel      context.CancelFunc
}

// SandboxLimits 沙箱限制
type SandboxLimits struct {
    MaxGoroutines int           // 最大 goroutine 数
    MaxMemory     int64         // 最大内存（字节）
    MaxCPUTime    time.Duration // 最大 CPU 时间
    Timeout       time.Duration // 操作超时
}

// ResourceMonitor 资源监控器
type ResourceMonitor struct {
    goroutineCount atomic.Int32
    memoryUsage    atomic.Int64
    cpuTime        atomic.Int64
}

func NewPluginSandbox(plugin Plugin, limits SandboxLimits) *PluginSandbox {
    ctx, cancel := context.WithCancel(context.Background())
    
    return &PluginSandbox{
        plugin:  plugin,
        limits:  limits,
        monitor: &ResourceMonitor{},
        ctx:     ctx,
        cancel:  cancel,
    }
}

func (ps *PluginSandbox) Execute(fn func() error) error {
    // 检查资源限制
    if ps.monitor.goroutineCount.Load() >= int32(ps.limits.MaxGoroutines) {
        return errors.New("goroutine limit exceeded")
    }
    
    ps.monitor.goroutineCount.Add(1)
    defer ps.monitor.goroutineCount.Add(-1)
    
    // 带超时执行
    done := make(chan error, 1)
    go func() {
        defer func() {
            if r := recover(); r != nil {
                done <- fmt.Errorf("panic: %v", r)
            }
        }()
        done <- fn()
    }()
    
    select {
    case err := <-done:
        return err
    case <-time.After(ps.limits.Timeout):
        return errors.New("execution timeout")
    case <-ps.ctx.Done():
        return ps.ctx.Err()
    }
}

func (ps *PluginSandbox) GetStats() SandboxStats {
    return SandboxStats{
        GoroutineCount: ps.monitor.goroutineCount.Load(),
        MemoryUsage:    ps.monitor.memoryUsage.Load(),
        CPUTime:        time.Duration(ps.monitor.cpuTime.Load()),
    }
}

type SandboxStats struct {
    GoroutineCount int32
    MemoryUsage    int64
    CPUTime        time.Duration
}
```

### 3.3 插件市场和分发

#### 方案概述
实现插件的发现、下载、安装和更新机制。

#### 插件清单格式

```yaml
# plugin.yaml
name: weather
version: 1.0.0
description: Weather query plugin
author: Your Name
homepage: https://github.com/yourname/weather-plugin
license: MIT

# 依赖
dependencies:
  - name: http-client
    version: ">=1.0.0"

# 权限要求
permissions:
  - engine.register_matcher
  - network.http
  - storage.read
  - storage.write

# 配置模板
config_schema:
  api_key:
    type: string
    required: true
    description: Weather API key
  timeout:
    type: duration
    default: 10s
    description: HTTP request timeout
  endpoints:
    type: array
    item_type: string
    default:
      - https://api.weather.com
    description: API endpoints

# 兼容性
compatibility:
  remilia: ">=0.9.0"
  go: ">=1.19"
```

#### 插件仓库接口

```go
// PluginRepository 插件仓库接口
type PluginRepository interface {
    // 搜索插件
    Search(query string) ([]PluginInfo, error)
    
    // 获取插件信息
    GetInfo(name string, version string) (*PluginInfo, error)
    
    // 下载插件
    Download(name string, version string, dest string) error
    
    // 列出所有插件
    List() ([]PluginInfo, error)
    
    // 检查更新
    CheckUpdates(installed []InstalledPlugin) ([]UpdateInfo, error)
}

// PluginInfo 插件信息
type PluginInfo struct {
    Name         string
    Version      string
    Description  string
    Author       string
    Homepage     string
    License      string
    Dependencies []Dependency
    Permissions  []Permission
    DownloadURL  string
    Checksum     string
}

// InstalledPlugin 已安装插件
type InstalledPlugin struct {
    Name    string
    Version string
    Path    string
}

// UpdateInfo 更新信息
type UpdateInfo struct {
    Name           string
    CurrentVersion string
    LatestVersion  string
    ChangeLog      string
}
```

#### CLI 命令

```bash
# 搜索插件
remilia plugin search weather

# 安装插件
remilia plugin install weather@1.0.0

# 列出已安装插件
remilia plugin list

# 更新插件
remilia plugin update weather

# 卸载插件
remilia plugin uninstall weather

# 查看插件信息
remilia plugin info weather
```

---

## 4. 架构优化

### 4.1 插件生命周期增强

#### 完整生命周期

```
┌─────────────┐
│  Registered │ 注册
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  Configured │ 配置
└──────┬──────┘
       │
       ▼
┌─────────────┐
│ Initialized │ 初始化
└──────┬──────┘
       │
       ▼
┌─────────────┐
│   Loaded    │ 加载
└──────┬──────┘
       │
       ▼
┌─────────────┐
│   Running   │ 运行中
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  Paused     │ 暂停（可选）
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  Stopping   │ 停止中
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  Unloaded   │ 卸载
└──────┬──────┘
       │
       ▼
┌─────────────┐
│ Unregistered│ 注销
└─────────────┘
```

#### 增强接口

```go
// AdvancedPlugin 高级插件接口
type AdvancedPlugin interface {
    Plugin
    
    // 生命周期钩子
    OnRegistered() error
    OnConfigured(config PluginConfig) error
    OnInitialized(ctx PluginContext) error
    OnLoaded() error
    OnStarted() error
    OnPaused() error
    OnResumed() error
    OnStopping() error
    OnUnloaded() error
    OnUnregistered() error
    
    // 健康检查
    HealthCheck() HealthStatus
}

// HealthStatus 健康状态
type HealthStatus struct {
    Healthy bool
    Message string
    Details map[string]interface{}
}
```

### 4.2 插件依赖注入

#### 方案概述
实现依赖注入容器，简化插件间协作。

#### 设计

```go
// DIContainer 依赖注入容器
type DIContainer struct {
    services map[string]interface{}
    mu       sync.RWMutex
}

func NewDIContainer() *DIContainer {
    return &DIContainer{
        services: make(map[string]interface{}),
    }
}

// Register 注册服务
func (dic *DIContainer) Register(name string, service interface{}) {
    dic.mu.Lock()
    defer dic.mu.Unlock()
    dic.services[name] = service
}

// Resolve 解析服务
func (dic *DIContainer) Resolve(name string) (interface{}, error) {
    dic.mu.RLock()
    defer dic.mu.RUnlock()
    
    service, exists := dic.services[name]
    if !exists {
        return nil, fmt.Errorf("service not found: %s", name)
    }
    return service, nil
}

// Inject 注入依赖到插件
func (dic *DIContainer) Inject(plugin Plugin) error {
    // 使用反射注入依赖
    val := reflect.ValueOf(plugin)
    if val.Kind() == reflect.Ptr {
        val = val.Elem()
    }
    
    for i := 0; i < val.NumField(); i++ {
        field := val.Type().Field(i)
        tag := field.Tag.Get("inject")
        
        if tag != "" {
            service, err := dic.Resolve(tag)
            if err != nil {
                return err
            }
            
            val.Field(i).Set(reflect.ValueOf(service))
        }
    }
    
    return nil
}
```

#### 使用示例

```go
type WeatherPlugin struct {
    *BasePlugin
    
    // 依赖注入
    HTTPClient *http.Client `inject:"http.client"`
    Cache      CacheService `inject:"cache.service"`
    Logger     *logrus.Entry `inject:"logger"`
}

// 在插件管理器中自动注入
func (pm *Manager) Register(plugin Plugin) error {
    // 注入依赖
    if err := pm.container.Inject(plugin); err != nil {
        return err
    }
    
    // 继续正常注册流程
    return pm.register(plugin)
}
```

### 4.3 插件 Metrics 和 Tracing

#### Metrics 集成

```go
type PluginMetrics struct {
    registry *prometheus.Registry
    
    // 通用指标
    loadTime      prometheus.Histogram
    handlerTime   prometheus.Histogram
    errorCount    prometheus.Counter
    activeHandlers prometheus.Gauge
}

func NewPluginMetrics(pluginName string) *PluginMetrics {
    pm := &PluginMetrics{
        registry: prometheus.NewRegistry(),
    }
    
    pm.loadTime = promauto.NewHistogram(prometheus.HistogramOpts{
        Name: "plugin_load_duration_seconds",
        Help: "Plugin load duration",
        ConstLabels: prometheus.Labels{
            "plugin": pluginName,
        },
    })
    
    pm.handlerTime = promauto.NewHistogram(prometheus.HistogramOpts{
        Name: "plugin_handler_duration_seconds",
        Help: "Plugin handler execution duration",
        ConstLabels: prometheus.Labels{
            "plugin": pluginName,
        },
    })
    
    return pm
}

func (pm *PluginMetrics) RecordHandlerDuration(duration time.Duration) {
    pm.handlerTime.Observe(duration.Seconds())
}
```

#### Tracing 集成

```go
import "go.opentelemetry.io/otel"

func (pc *pluginContext) executeWithTracing(handler context.Handler) context.Handler {
    return func(ctx *context.Context) error {
        tracer := otel.Tracer("plugin")
        spanCtx, span := tracer.Start(ctx.Context(), "plugin.handler",
            trace.WithAttributes(
                attribute.String("plugin.name", pc.name),
                attribute.String("event.type", string(ctx.GetEventType())),
            ),
        )
        defer span.End()
        
        ctx.SetStdContext(spanCtx)
        
        err := handler(ctx)
        if err != nil {
            span.RecordError(err)
            span.SetStatus(codes.Error, err.Error())
        }
        
        return err
    }
}
```

---

## 5. 实施路线图

### 阶段 1: 基础增强（2-3 周）

**目标**: 实现插件上下文和配置管理

- [ ] 实现 PluginContext 接口和基础实现
- [ ] 实现插件配置管理系统
- [ ] 实现内存存储后端
- [ ] 更新 BasePlugin 支持新接口
- [ ] 编写单元测试和文档

**交付物**:
- plugin/context.go
- plugin/config.go
- plugin/storage.go
- docs/PLUGIN_CONTEXT.md

### 阶段 2: 事件总线（1-2 周）

**目标**: 实现插件间通信机制

- [ ] 实现 EventBus 核心功能
- [ ] 实现订阅管理和模式匹配
- [ ] 添加事件过滤和转换
- [ ] 集成到 PluginContext
- [ ] 编写测试和示例

**交付物**:
- plugin/eventbus.go
- plugin/eventbus_test.go
- examples/plugin_communication/

### 阶段 3: 权限系统（1-2 周）

**目标**: 实现插件权限控制

- [ ] 设计权限模型
- [ ] 实现权限检查机制
- [ ] 定义预定义角色
- [ ] 集成到 PluginContext
- [ ] 配置文件支持

**交付物**:
- plugin/permissions.go
- plugin/roles.go
- docs/PLUGIN_PERMISSIONS.md

### 阶段 4: 存储后端（2 周）

**目标**: 支持多种存储后端

- [ ] 实现 Redis 存储后端
- [ ] 实现文件系统存储后端
- [ ] 实现存储事务
- [ ] 性能优化和测试
- [ ] 配置和文档

**交付物**:
- plugin/storage/redis.go
- plugin/storage/filesystem.go
- plugin/storage/transaction.go

### 阶段 5: 高级特性（3-4 周）

**目标**: 实现热重载和沙箱

- [ ] 实现热重载接口
- [ ] 实现插件沙箱
- [ ] 实现资源监控
- [ ] 集成 Metrics 和 Tracing
- [ ] 完善生命周期管理

**交付物**:
- plugin/hotreload.go
- plugin/sandbox.go
- plugin/lifecycle.go
- plugin/metrics.go

### 阶段 6: 插件市场（可选，4-6 周）

**目标**: 实现插件分发机制

- [ ] 设计插件清单格式
- [ ] 实现插件仓库接口
- [ ] 实现插件下载和安装
- [ ] 实现版本管理和更新
- [ ] 开发 CLI 工具

**交付物**:
- plugin/repository.go
- plugin/installer.go
- cmd/remilia-plugin/main.go
- docs/PLUGIN_MARKET.md

---

## 6. 风险评估

### 6.1 兼容性风险

**风险**: 新接口可能破坏现有插件

**缓解措施**:
- 保持向后兼容，新接口为可选
- 提供适配器层兼容旧插件
- 详细的迁移文档和工具
- 充分的测试和验证期

### 6.2 性能风险

**风险**: 额外的抽象层可能影响性能

**缓解措施**:
- 使用对象池减少分配
- 关键路径优化
- 性能基准测试
- 可配置的性能开关

### 6.3 复杂度风险

**风险**: 系统复杂度显著增加

**缓解措施**:
- 模块化设计，功能可选
- 清晰的文档和示例
- 循序渐进的实施
- 充分的代码审查

### 6.4 安全风险

**风险**: 权限系统可能被绕过

**缓解措施**:
- 严格的权限检查
- 沙箱隔离机制
- 安全审计和测试
- 最小权限原则

---

## 7. 总结

### 7.1 核心价值

1. **解耦**: 插件间通过事件总线通信，降低耦合
2. **隔离**: 权限和沙箱机制保护系统稳定性
3. **灵活**: 丰富的接口支持多种插件类型
4. **可控**: 完善的生命周期和资源管理
5. **可扩展**: 清晰的扩展点和接口设计

### 7.2 预期收益

| 维度 | 改进 | ROI |
|------|------|-----|
| 开发效率 | +40% | ⭐⭐⭐⭐⭐ |
| 系统稳定性 | +50% | ⭐⭐⭐⭐⭐ |
| 功能丰富度 | +100% | ⭐⭐⭐⭐ |
| 可维护性 | +60% | ⭐⭐⭐⭐⭐ |

### 7.3 建议优先级

**P0 (立即实施)**:
- 插件上下文系统
- 配置管理系统

**P1 (近期实施)**:
- 事件总线系统
- 存储系统

**P2 (中期实施)**:
- 权限系统
- 热重载机制

**P3 (长期规划)**:
- 沙箱隔离
- 插件市场

---

**文档维护**: GitHub Copilot  
**最后更新**: 2026-01-23  
**联系方式**: 如有疑问请通过 Issue 反馈
