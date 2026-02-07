# 插件可选接口快速参考

## 🎯 核心接口

```go
type Plugin interface {
    Name() string
    Load(coordinator *engine.Engine) error
    Unload(coordinator *engine.Engine) error
    Reload(coordinator *engine.Engine) error
    Dependencies() []string
}
```

**说明**: 所有插件必须实现

---

## 🔧 可选接口

### 1. MetadataProvider

```go
type MetadataProvider interface {
    Metadata() *Metadata
}
```

**功能**: 提供插件元数据（名称、版本、作者、描述等）  
**实现者**: `BasePlugin`

---

### 2. ConfigurablePlugin

```go
type ConfigurablePlugin interface {
    GetConfig() PluginConfig
    SetConfig(config PluginConfig)
}
```

**功能**: 支持插件配置管理  
**实现者**: `BasePlugin`  
**用法**:
```go
config := plugin.GetConfig()
apiKey := config.GetString("api_key", "default")
```

---

### 3. StatefulPlugin

```go
type StatefulPlugin interface {
    GetState() PluginState
    SetState(state PluginState)
    GetLoadTime() time.Time
    SetLoadTime(t time.Time)
    GetLastError() error
    SetLastError(err error)
    GetUptime() time.Duration
}
```

**功能**: 支持插件状态查询  
**实现者**: `BasePlugin`  
**用法**:
```go
state := plugin.GetState()  // Loaded/Loading/Unloaded/Error/Reloading
uptime := plugin.GetUptime()
```

---

### 4. MatcherProvider

```go
type MatcherProvider interface {
    GetMatchers() []*engine.Matcher
}
```

**功能**: 提供插件注册的 Matcher 列表  
**实现者**: `BasePlugin`

---

### 5. EventAwarePlugin

```go
type EventAwarePlugin interface {
    PublishEvent(topic string, data interface{}) error
    SubscribeEvent(topic string, handler EventHandler) (Subscription, error)
    UnsubscribeEvent(sub Subscription) error
    GetEventBus() EventBus
}
```

**功能**: 支持插件间事件通信  
**实现者**: `BasePlugin`  
**用法**:
```go
plugin.PublishEvent("user.login", userData)
plugin.SubscribeEvent("system.ready", handler)
```

---

## 📝 使用示例

### 使用 BasePlugin (推荐)

```go
type MyPlugin struct {
    *plugin.BasePlugin  // ✅ 自动实现所有可选接口
}

func NewMyPlugin() *MyPlugin {
    return &MyPlugin{
        BasePlugin: plugin.NewBasePlugin("myplugin"),
    }
}

// 现在可以使用所有功能
func (p *MyPlugin) Load(eng *engine.Engine) error {
    // 配置管理
    config := p.GetConfig()
    
    // 事件通信
    p.SubscribeEvent("test", handler)
    
    // 状态自动管理
    return nil
}
```

---

### 自定义实现

```go
type CustomPlugin struct {
    name   string
    state  PluginState
    config plugin.PluginConfig
}

// 实现核心接口
func (p *CustomPlugin) Name() string { return p.name }
func (p *CustomPlugin) Load(eng *engine.Engine) error { /* ... */ }
// ...

// 可选：实现状态管理
func (p *CustomPlugin) GetState() PluginState { return p.state }
func (p *CustomPlugin) SetState(s PluginState) { p.state = s }
// ...

// 可选：实现配置管理
func (p *CustomPlugin) GetConfig() plugin.PluginConfig { return p.config }
func (p *CustomPlugin) SetConfig(c plugin.PluginConfig) { p.config = c }
```

---

## 🔍 Manager 中的使用

### 检查接口支持

```go
// ✅ 好的做法 - 使用可选接口
if stateful, ok := plugin.(StatefulPlugin); ok {
    state := stateful.GetState()
}

if configurable, ok := plugin.(ConfigurablePlugin); ok {
    config := configurable.GetConfig()
}

// ❌ 避免 - 依赖具体类型
if basePlugin, ok := plugin.(*BasePlugin); ok {
    // 不要这样做
}
```

---

## 📊 接口实现矩阵

| 接口 | BasePlugin | 自定义插件 |
|------|------------|-----------|
| Plugin | ✅ | ✅ 必须实现 |
| MetadataProvider | ✅ | ⭕ 可选 |
| ConfigurablePlugin | ✅ | ⭕ 可选 |
| StatefulPlugin | ✅ | ⭕ 可选 |
| MatcherProvider | ✅ | ⭕ 可选 |
| EventAwarePlugin | ✅ | ⭕ 可选 |

---

## 🎯 选择指南

### 何时使用 BasePlugin？

✅ **推荐场景**:
- 普通插件开发
- 需要完整功能支持
- 快速原型开发

### 何时自定义实现？

✅ **适用场景**:
- 需要特殊的内存布局
- 需要自定义状态管理逻辑
- 有特殊的性能要求
- 需要极简实现

---

## 📚 相关文档

- **详细设计**: `docs/05-reports/PLUGIN_INTERFACE_REFACTORING.md`
- **使用指南**: `docs/02-user-guides/PLUGIN_ENHANCEMENTS_GUIDE.md`

