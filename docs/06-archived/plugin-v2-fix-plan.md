# Plugin v2 问题修复计划

## 🎯 修复目标

将 v2 实现从 7.25/10 提升到 9/10，使其达到生产可用水平。

---

## 📋 P0 优先修复列表

### 1. Matcher 追踪机制

#### 问题
`PluginInstance.matchers` 字段存在但从未使用，导致无法追踪插件注册的命令。

#### 解决方案
在 `SetupContext` 中包装 Engine 的命令注册方法，自动追踪。

#### 代码实现
```go
// v2.go 中添加

// RegisterCommand 注册命令并自动追踪
func (ctx *SetupContext) RegisterCommand(eventType dto.EventType, pattern string, extraRules ...context.Rule) *engine.Matcher {
    matcher := ctx.Engine.OnCommand(eventType, pattern, extraRules...)
    
    // 设置分组（用于卸载）
    if matcher != nil {
        // 从 context 中获取当前插件名称
        if ctx.pluginName != "" {
            matcher.SetGroup(ctx.pluginName)
            matcher.SetSource("plugin:" + ctx.pluginName)
        }
        
        // 追踪 matcher
        if ctx.instance != nil {
            ctx.instance.addMatcher(matcher)
        }
    }
    
    return matcher
}

// RegisterMatcher 注册自定义 Matcher 并追踪
func (ctx *SetupContext) RegisterMatcher(eventType dto.EventType, rules ...context.Rule) *engine.Matcher {
    matcher := ctx.Engine.On(eventType, rules...)
    
    if matcher != nil && ctx.pluginName != "" {
        matcher.SetGroup(ctx.pluginName)
        matcher.SetSource("plugin:" + ctx.pluginName)
    }
    
    if ctx.instance != nil {
        ctx.instance.addMatcher(matcher)
    }
    
    return matcher
}

// SetupContext 添加字段
type SetupContext struct {
    Engine     *engine.Engine
    Manager    *Manager
    Config     Config
    
    container  *Container
    pluginName string           // 新增：当前插件名称
    instance   *PluginInstance  // 新增：插件实例引用
}

// PluginInstance 添加方法
func (pi *PluginInstance) addMatcher(matcher *engine.Matcher) {
    pi.mu.Lock()
    defer pi.mu.Unlock()
    pi.matchers = append(pi.matchers, matcher)
}

// GetMatchers 实现 MatcherProvider 接口
func (pi *PluginInstance) GetMatchers() []*engine.Matcher {
    pi.mu.RLock()
    defer pi.mu.RUnlock()
    result := make([]*engine.Matcher, len(pi.matchers))
    copy(result, pi.matchers)
    return result
}
```

#### 使用示例
```go
// 旧方式（仍然支持，但不追踪）
ctx.Engine.OnCommand(dto.C2CMessageCreate, "/hello")

// 新方式（推荐，自动追踪）
ctx.RegisterCommand(dto.C2CMessageCreate, "/hello")
```

---

### 2. 完整的 StatefulPlugin 实现

#### 问题
缺少 `GetLoadTime`、`SetLoadTime`、`GetLastError`、`SetLastError`、`GetUptime` 方法。

#### 代码实现
```go
// PluginInstance 添加字段
type PluginInstance struct {
    desc         *PluginDescriptor
    state        State
    setupContext *SetupContext
    matchers     []*engine.Matcher
    
    // 新增状态字段
    loadTime     time.Time
    lastError    error
    
    mu           sync.RWMutex
}

// GetLoadTime 获取加载时间
func (pi *PluginInstance) GetLoadTime() time.Time {
    pi.mu.RLock()
    defer pi.mu.RUnlock()
    return pi.loadTime
}

// SetLoadTime 设置加载时间
func (pi *PluginInstance) SetLoadTime(t time.Time) {
    pi.mu.Lock()
    defer pi.mu.Unlock()
    pi.loadTime = t
}

// GetLastError 获取最后的错误
func (pi *PluginInstance) GetLastError() error {
    pi.mu.RLock()
    defer pi.mu.RUnlock()
    return pi.lastError
}

// SetLastError 设置最后的错误
func (pi *PluginInstance) SetLastError(err error) {
    pi.mu.Lock()
    defer pi.mu.Unlock()
    pi.lastError = err
}

// GetUptime 获取运行时长
func (pi *PluginInstance) GetUptime() time.Duration {
    pi.mu.RLock()
    loadTime := pi.loadTime
    state := pi.state
    pi.mu.RUnlock()
    
    if state != Loaded || loadTime.IsZero() {
        return 0
    }
    
    return time.Since(loadTime)
}

// Load 方法更新
func (pi *PluginInstance) Load(coordinator *engine.Engine) error {
    pi.mu.Lock()
    pi.state = Loading
    pi.mu.Unlock()

    startTime := time.Now()  // 记录开始时间
    
    if err := pi.desc.Setup(pi.setupContext); err != nil {
        pi.mu.Lock()
        pi.state = Error
        pi.lastError = err      // 新增：记录错误
        pi.mu.Unlock()
        return err
    }

    pi.mu.Lock()
    pi.state = Loaded
    pi.loadTime = startTime     // 新增：记录加载时间
    pi.lastError = nil          // 新增：清除错误
    pi.mu.Unlock()

    return nil
}
```

---

### 3. 热重载时更新 SetupContext

#### 问题
Reload 时复用旧的 `setupContext`，容器中的插件可能已过期。

#### 代码实现
```go
// Reload 重载插件
func (pi *PluginInstance) Reload(coordinator *engine.Engine) error {
    pi.mu.Lock()
    pi.state = Reloading
    pi.mu.Unlock()

    // 如果定义了自定义 Reload 函数
    if pi.desc.Reload != nil {
        // 创建新的 SetupContext（从 Manager 获取最新容器）
        newCtx := pi.setupContext.Manager.createSetupContext(pi.desc.Name, pi)
        
        if err := pi.desc.Reload(newCtx); err != nil {
            pi.mu.Lock()
            pi.state = Error
            pi.lastError = err
            pi.mu.Unlock()
            return err
        }
        
        // 更新 setupContext
        pi.setupContext = newCtx
        
        pi.mu.Lock()
        pi.state = Loaded
        pi.loadTime = time.Now()
        pi.lastError = nil
        pi.mu.Unlock()

        return nil
    }

    // 默认策略：Unload + Load
    if err := pi.Unload(coordinator); err != nil {
        return err
    }
    return pi.Load(coordinator)
}

// Manager 添加辅助方法
func (pm *Manager) createSetupContext(pluginName string, instance *PluginInstance) *SetupContext {
    pm.mu.RLock()
    defer pm.mu.RUnlock()
    
    var config Config
    if pm.viper != nil {
        config = NewPluginConfig(pluginName, pm.viper)
    }
    
    return &SetupContext{
        Engine:     pm.coordinator,
        Manager:    pm,
        Config:     config,
        container:  pm.container,
        pluginName: pluginName,
        instance:   instance,
    }
}
```

---

## 📋 P1 修复列表

### 4. 并发安全改进

#### 代码实现
```go
func (pm *Manager) RegisterV2(desc *PluginDescriptor) error {
    if desc == nil {
        return fmt.Errorf("plugin descriptor is nil")
    }

    if desc.Name == "" {
        return fmt.Errorf("plugin name is required")
    }

    if desc.Setup == nil {
        return fmt.Errorf("plugin setup function is required")
    }

    name := desc.Name

    pm.mu.Lock()
    
    // 检查是否已存在
    if _, exists := pm.plugins[name]; exists {
        pm.mu.Unlock()
        logger.Warnf("[pluginManager] Plugin %s already registered", name)
        return errutil.ErrPluginAlreadyExists
    }

    // 检查依赖（并检查状态）
    for _, dep := range desc.Deps {
        plugin, exists := pm.plugins[dep]
        if !exists {
            pm.mu.Unlock()
            return fmt.Errorf("missing dependency: %s", dep)
        }
        
        // 检查依赖插件状态
        if stateful, ok := plugin.(StatefulPlugin); ok {
            if stateful.GetState() != Loaded {
                pm.mu.Unlock()
                return fmt.Errorf("dependency '%s' is not loaded (state: %v)", dep, stateful.GetState())
            }
        }
    }
    
    // 检查循环依赖
    if err := pm.checkCircularDependency(name, desc.Deps); err != nil {
        pm.mu.Unlock()
        return err
    }

    // 初始化容器
    if pm.container == nil {
        pm.container = NewContainer()
    }
    
    // 注册特殊服务（只在第一次）
    if !pm.container.Has("manager") {
        pm.container.Register("manager", pm)
        pm.container.Register("engine", pm.coordinator)
        pm.container.Register("coordinator", pm.coordinator)
    }

    // 创建插件实例（在锁内）
    setupCtx := pm.createSetupContext(name, nil)
    instance := &PluginInstance{
        desc:         desc,
        state:        Unloaded,
        setupContext: setupCtx,
        matchers:     make([]*engine.Matcher, 0),
    }
    
    // 更新 setupContext 的 instance 引用
    setupCtx.instance = instance
    
    // 先注册到 plugins（预注册，标记为 Loading）
    pm.plugins[name] = instance
    instance.state = Loading
    
    pm.mu.Unlock()

    // 加载插件（在锁外，允许长时间操作）
    err := instance.Load(pm.coordinator)
    
    pm.mu.Lock()
    defer pm.mu.Unlock()
    
    if err != nil {
        // 加载失败，���滚
        delete(pm.plugins, name)
        logger.WithError(err).Errorf("[pluginManager] Failed to load plugin %s", name)
        pm.notifyError(name, "load", err)
        return err
    }

    // 成功，添加到容器和加载顺序
    pm.container.Register(name, instance)
    pm.loadOrder = append(pm.loadOrder, name)
    
    logger.Infof("[pluginManager] Plugin %s registered (v2)", name)
    pm.notifyLoaded(name)
    
    return nil
}

// 循环依赖检查
func (pm *Manager) checkCircularDependency(pluginName string, deps []string) error {
    visited := make(map[string]bool)
    
    var check func(name string) error
    check = func(name string) error {
        if visited[name] {
            return fmt.Errorf("circular dependency detected: %s", name)
        }
        
        visited[name] = true
        defer func() { visited[name] = false }()
        
        plugin, exists := pm.plugins[name]
        if !exists {
            return nil
        }
        
        for _, dep := range plugin.Dependencies() {
            if dep == pluginName {
                return fmt.Errorf("circular dependency: %s -> %s -> %s", pluginName, name, dep)
            }
            if err := check(dep); err != nil {
                return err
            }
        }
        
        return nil
    }
    
    for _, dep := range deps {
        if err := check(dep); err != nil {
            return err
        }
    }
    
    return nil
}
```

---

### 5. 改进错误处理

#### 代码实现
```go
// Unload 时状态修复
func (pi *PluginInstance) Unload(coordinator *engine.Engine) error {
    pi.mu.Lock()
    oldState := pi.state
    pi.state = Unloading  // 使用正确的中间状态
    pi.mu.Unlock()

    // 清理注册的匹配器
    if coordinator != nil {
        coordinator.RemoveGroup(pi.desc.Name)
    }
    
    // 清空 matchers 列表
    pi.mu.Lock()
    pi.matchers = make([]*engine.Matcher, 0)
    pi.mu.Unlock()

    // 调用 Teardown 函数
    var err error
    if pi.desc.Teardown != nil {
        err = pi.desc.Teardown()
    }

    pi.mu.Lock()
    if err != nil {
        pi.state = Error
        pi.lastError = err
    } else {
        pi.state = Unloaded
        pi.lastError = nil
    }
    pi.mu.Unlock()

    return err
}
```

---

## 📋 P2 修复列表

### 6. ConfigurablePlugin 实现

```go
// GetConfig 获取插件配置
func (pi *PluginInstance) GetConfig() Config {
    pi.mu.RLock()
    defer pi.mu.RUnlock()
    if pi.setupContext != nil {
        return pi.setupContext.Config
    }
    return nil
}

// SetConfig 设置插件配置
func (pi *PluginInstance) SetConfig(config Config) {
    pi.mu.Lock()
    defer pi.mu.Unlock()
    if pi.setupContext != nil {
        pi.setupContext.Config = config
    }
}
```

---

## 🧪 测试计划

### 单元测试
```go
// v2_test.go

func TestPluginInstance_MatcherTracking(t *testing.T) {
    // 测试 Matcher 追踪功能
}

func TestPluginInstance_StatefulInterface(t *testing.T) {
    // 测试所有 StatefulPlugin 方法
}

func TestPluginInstance_Reload_UpdatesContext(t *testing.T) {
    // 测试热重载时 Context 更新
}

func TestManager_RegisterV2_Concurrent(t *testing.T) {
    // 测试并发注册
}

func TestManager_CircularDependency(t *testing.T) {
    // 测试循环依赖检测
}
```

---

## 📊 修复进度跟踪

| 问题编号 | 优先级 | 状态 | 预计时间 |
|---------|--------|------|---------|
| 1. Matcher 追踪 | P0 | ⏳ Pending | 2小时 |
| 2. StatefulPlugin | P0 | ⏳ Pending | 1小时 |
| 3. Reload Context | P0 | ⏳ Pending | 1小时 |
| 4. 并发安全 | P1 | ⏳ Pending | 3小时 |
| 5. 错误处理 | P1 | ⏳ Pending | 1小时 |
| 6. 依赖检查 | P1 | ⏳ Pending | 2小时 |
| 7. ConfigurablePlugin | P2 | ⏳ Pending | 1小时 |
| 8. Container 优化 | P2 | ⏳ Pending | 1小时 |
| 9. MatcherProvider | P2 | ⏳ Pending | 0.5小时 |

**总预计时间**: 12.5 小时

---

## 🎯 完成标准

- [ ] 所有 P0 问题已修复
- [ ] 所有 P1 问题已修复
- [ ] 至少 80% 的 P2 问题已修复
- [ ] 单元测试覆盖率 > 80%
- [ ] 所有测试通过
- [ ] 文档已更新
- [ ] 示例代码已更新
- [ ] 性能没有明显下降

修复完成后，v2 评分预计：**9/10** ⭐

