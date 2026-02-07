# Plugin 与 Lifecycle 包集成方案

**日期**: 2026-02-07  
**目标**: 统一插件生命周期管理

---

## 🎯 设计目标

将 `plugin` 包与 `lifecycle` 包集成，实现：
1. ✅ **统一生命周期管理** - 插件使用 lifecycle 包的标准生命周期
2. ✅ **保持独立性** - 两个包不强耦合，可独立使用
3. ✅ **保留 LifecycleListener** - 作为插件特定的轻量级事件通知

---

## 📋 集成架构

### 架构图

```
┌─────────────────────────────────────────┐
│         Lifecycle Manager               │
│  (统一生命周期管理)                       │
└───────────────┬─────────────────────────┘
                │
                │ Register(Component)
                │
     ┌──────────▼──────────┐
     │  PluginComponent    │  ← 适配器
     │  (Adapter)          │
     └──────────┬──────────┘
                │
                │ wraps
                │
     ┌──────────▼──────────┐
     │      Plugin         │
     │  (BasePlugin等)     │
     └─────────────────────┘
```

### 关键组件

1. **PluginComponent** - 适配器，将 Plugin 转换为 lifecycle.Component
2. **PluginManager** - 提供集成方法
3. **LifecycleListener** - 插件专用事件通知

---

## 🔧 实现细节

### 1. PluginComponent 适配器

```go
// PluginComponent 将插件适配为 lifecycle.Component
type PluginComponent struct {
    plugin      Plugin
    coordinator *engine.Engine
    manager     *Manager
}

func (pc *PluginComponent) Name() string {
    return "plugin:" + pc.plugin.Name()
}

func (pc *PluginComponent) OnStart(ctx context.Context) error {
    // 加载插件
    // 设置状态为 Loading -> Loaded
    // 触发 LifecycleListener.OnPluginLoaded
}

func (pc *PluginComponent) OnRun(ctx context.Context) error {
    // 插件通常无需持续运行逻辑
    // 它们通过注册的 Matcher 被动响应事件
    <-ctx.Done()
    return nil
}

func (pc *PluginComponent) OnStop(ctx context.Context) error {
    // 卸载插件
    // 设置状态为 Unloaded
    // 触发 LifecycleListener.OnPluginUnloaded
}
```

**特点**:
- ✅ 实现 `lifecycle.Component` 接口
- ✅ 自动管理插件状态
- ✅ 触发 LifecycleListener 通知
- ✅ 支持 StatefulPlugin 接口

---

### 2. Manager 集成方法

#### AsLifecycleComponent

```go
// 将单个插件转换为 lifecycle.Component
func (pm *Manager) AsLifecycleComponent(plugin Plugin) interface{}
```

**用法**:
```go
component := pluginManager.AsLifecycleComponent(myPlugin).(lifecycle.Component)
lifecycleManager.Register(component)
```

#### RegisterToLifecycle

```go
// 批量注册所有插件到 lifecycle.Manager
func (pm *Manager) RegisterToLifecycle(lm interface{}) error
```

**用法**:
```go
pluginManager.Register(plugin1)
pluginManager.Register(plugin2)

lifecycleManager := lifecycle.NewManager()
pluginManager.RegisterToLifecycle(lifecycleManager)

lifecycleManager.Start(ctx)
```

---

## 💡 使用场景

### 场景 1：独立使用插件管理器

```go
// 传统方式，不使用 lifecycle 包
engine := engine.NewEngine()
pluginManager := plugin.NewManager(engine)

plugin1 := NewMyPlugin()
pluginManager.Register(plugin1)  // 自动 Load

// ... 使用插件

pluginManager.Unregister("myplugin")  // 手动 Unload
```

**适用**:
- 简单应用
- 不需要统一生命周期管理
- 插件数量少

---

### 场景 2：使用 lifecycle 统一管理

```go
// 使用 lifecycle 包统一管理所有组件
engine := engine.NewEngine()
pluginManager := plugin.NewManager(engine)
lifecycleManager := lifecycle.NewManager()

// 注册插件（不自动 Load）
plugin1 := NewMyPlugin()
component := pluginManager.AsLifecycleComponent(plugin1).(lifecycle.Component)
lifecycleManager.Register(component)

// 统一启动所有组件
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
lifecycleManager.Start(ctx)

// ... 应用运行

// 统一停止所有组件
stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
lifecycleManager.Stop(stopCtx)
```

**适用**:
- 复杂应用
- 多类型组件（adapter、engine、plugins 等）
- 需要统一的启动/停止顺序
- 需要超时控制

---

### 场景 3：批量注册插件到 lifecycle

```go
// 先使用 plugin.Manager 管理依赖和配置
pluginManager := plugin.NewManager(engine)
pluginManager.SetViper(config)

// 注册插件（会自动 Load）
pluginManager.Register(plugin1)
pluginManager.Register(plugin2)

// 批量转换到 lifecycle（注意：这种情况插件已经 Load 了）
lifecycleManager := lifecycle.NewManager()
pluginManager.RegisterToLifecycle(lifecycleManager)

// lifecycle.Start 不会重复 Load
lifecycleManager.Start(ctx)
```

**注意**: 这种方式下插件已经通过 `pluginManager.Register` 加载了，不适合 lifecycle 管理。

**推荐**: 使用场景 2 的方式。

---

## 🔄 生命周期对比

### 传统方式（plugin.Manager）

```
Register() ─→ Load() ─→ [运行中] ─→ Unregister() ─→ Unload()
     │                                    │
     └────────── 手动管理 ─────────────────┘
```

### Lifecycle 方式

```
Register(Component) ─→ Start() ─→ [OnStart] ─→ [OnRun] ─→ Stop() ─→ [OnStop]
                          │                                  │
                          └────────── 自动管理 ───────────────┘
```

---

## ⚖️ LifecycleListener vs lifecycle.Component

### LifecycleListener（保留）

```go
type LifecycleListener interface {
    OnPluginLoaded(name string)
    OnPluginUnloaded(name string)
    OnPluginReloaded(name string)
    OnPluginError(name string, operation string, err error)
}
```

**特点**:
- ✅ 轻量级事件通知
- ✅ 插件专用
- ✅ 不依赖 context
- ✅ 适合日志、监控、统计

**用途**:
- 记录插件加载日志
- 统计插件数量
- 监控插件状态
- 触发告警

---

### lifecycle.Component（新增适配）

```go
type Component interface {
    Name() string
    OnStart(ctx context.Context) error
    OnRun(ctx context.Context) error
    OnStop(ctx context.Context) error
}
```

**特点**:
- ✅ 完整生命周期管理
- ✅ 支持超时控制
- ✅ 统一的组件接口
- ✅ 自动回滚机制

**用途**:
- 统一管理多类型组件
- 控制启动/停止顺序
- 超时保护
- 错误回滚

---

## 🎯 最佳实践

### 1. 选择合适的方式

**使用 plugin.Manager**:
- ✅ 只有插件需要管理
- ✅ 不需要统一生命周期
- ✅ 需要插件热重载

**使用 lifecycle.Manager**:
- ✅ 多类型组件（adapter + engine + plugins）
- ✅ 需要统一启动/停止
- ✅ 需要超时控制

---

### 2. 状态管理

```go
// 实现 StatefulPlugin 接口
type MyPlugin struct {
    *plugin.BasePlugin
}

// 自动享受状态管理
// - PluginComponent 会自动设置状态
// - 可通过 plugin.GetState() 查询
```

---

### 3. 事件通知

```go
// 添加监听器监控插件生命周期
listener := &MyListener{}
pluginManager.AddListener(listener)

// PluginComponent 会自动触发这些事件
// - OnPluginLoaded
// - OnPluginUnloaded
// - OnPluginError
```

---

## ✅ 测试验证

### 测试用例

1. **TestPluginLifecycleIntegration/PluginComponent_Basic_Lifecycle**
   - 单个插件的完整生命周期
   - 验证状态转换
   - 验证事件通知

2. **TestPluginLifecycleIntegration/Multiple_Plugins_With_Lifecycle**
   - 多个插件同时管理
   - 验证启动/停止顺序
   - 验证状态独立性

3. **TestPluginLifecycleIntegration/Lifecycle_Listener_Notification**
   - 验证 LifecycleListener 触发
   - 验证事件参数正确性

### 测试结果

```bash
go test ./plugin/... -v -run TestPluginLifecycle

✅ PluginComponent_Basic_Lifecycle - PASS
✅ Multiple_Plugins_With_Lifecycle - PASS
✅ Lifecycle_Listener_Notification - PASS
```

---

## 📚 相关文档

- **Lifecycle 包文档**: `lifecycle/lifecycle.go`
- **Plugin 包文档**: `plugin/plugin.go`
- **适配器实现**: `plugin/lifecycle_adapter.go`
- **测试代码**: `plugin/lifecycle_test.go`

---

## 🎯 总结

### 设计优势

1. ✅ **解耦** - plugin 和 lifecycle 包不强耦合
2. ✅ **灵活** - 可以选择使用或不使用 lifecycle
3. ✅ **兼容** - 现有代码无需修改
4. ✅ **统一** - 提供统一的生命周期管理接口

### 实现特点

1. ✅ **适配器模式** - PluginComponent 作为适配器
2. ✅ **接口鸭子类型** - 避免循环依赖
3. ✅ **保留独立性** - LifecycleListener 仍可独立使用
4. ✅ **自动状态管理** - 与 StatefulPlugin 集成

### 使用建议

1. **简单应用** - 使用 plugin.Manager
2. **复杂应用** - 使用 lifecycle.Manager + PluginComponent
3. **监控需求** - 使用 LifecycleListener
4. **统一管理** - 使用 RegisterToLifecycle

---

**实施时间**: 2026-02-07  
**测试状态**: ✅ 全部通过  
**破坏性变更**: 无  
**向后兼容**: 100%

