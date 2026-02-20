# 插件系统重构分析报告

## 问题概述

当前插件系统存在以下主要问题：

### 1. **继承导向设计，不符合 Go 语言习惯**
- 所有插件必须嵌入 `*plugin.BasePlugin`
- 需要手动调用父类方法（如 `BasePlugin.Load()`）
- 容易忘记调用关键方法，导致 bug

### 2. **接口过于复杂和臃肿**
```go
// 当前有 7 个接口，职责分散
type Plugin interface { ... }              // 核心接口，4个必需方法
type MetadataProvider interface { ... }    // 可选
type ConfigurablePlugin interface { ... }  // 可选，2个方法
type StatefulPlugin interface { ... }      // 可选，6个方法
type MatcherProvider interface { ... }     // 可选
type EventAwarePlugin interface { ... }    // 可选，4个方法
type DependencyInjector interface { ... }  // 可选
```

### 3. **依赖注入方式混乱**
- 同时使用结构体标签 `inject:"plugin:xxx"` 和手动 Setter 方法
- 需要手动调用 `SetPluginManager`、`SetPermissionPlugin` 等
- 依赖注入逻辑分散在多个地方

### 4. **样板代码过多**
```go
// 每个插件都需要写这些重复代码
type MyPlugin struct {
    *plugin.BasePlugin
    Engine        *engine.Engine     `inject:"engine"`
    PluginManager *plugin.Manager    `inject:"manager"`
    DepPlugin     *other.Plugin      `inject:"plugin:other"`
}

func (p *MyPlugin) SetPluginManager(pm *plugin.Manager) {
    p.PluginManager = pm
}

func (p *MyPlugin) Load(eng *engine.Engine) error {
    p.BasePlugin.Load(eng)  // 容易忘记
    // 实际逻辑
}
```

### 5. **状态管理复杂**
- BasePlugin 内部维护状态，但外部也可能修改
- 状态更新需要手动调用多个方法
- 锁的粒度难以控制

---

## 重构方案

### 核心思想

**组合优于继承，函数优于方法**

采用 Go 惯用的设计模式：
1. 使用**函数式选项模式**替代继承
2. 使用**简单接口**替代复杂接口层次
3. 使用**依赖注入容器**替代手动注入
4. 使用**钩子函数**替代方法重写

---

## 详细设计

### 1. 简化核心接口

```go
// 插件只需要实现一个方法
type Plugin interface {
    Name() string
}

// Setup 是插件的初始化函数（不是接口方法）
type SetupFunc func(ctx *SetupContext) error

// SetupContext 提供插件初始化所需的所有资源
type SetupContext struct {
    Engine   *engine.Engine
    Manager  *Manager
    Config   Config
    Logger   *logger.Logger
    
    // 依赖注入
    Get(name string) (Plugin, bool)
    MustGet(name string) Plugin
}
```

### 2. 函数式插件注册

```go
// 插件使用函数注册，而不是结构体
func NewMyPlugin() *PluginDescriptor {
    return &PluginDescriptor{
        Name:    "myplugin",
        Version: "1.0.0",
        Deps:    []string{"permission", "storage"},
        Setup: func(ctx *SetupContext) error {
            // 获取依赖（自动注入）
            perm := ctx.MustGet("permission").(*permission.Plugin)
            
            // 注册命令
            ctx.Engine.OnCommand(dto.C2CMessageCreate, "/hello").
                Handle(func(c *eventctx.Context) error {
                    return c.Reply("Hello!")
                })
            
            return nil
        },
    }
}
```

### 3. 插件描述符

```go
type PluginDescriptor struct {
    // 基本信息
    Name        string
    Version     string
    Author      string
    Description string
    
    // 依赖
    Deps []string
    
    // 生命周期钩子
    Setup    SetupFunc            // 初始化（必需）
    Teardown func() error         // 清理（可选）
    Reload   func() error         // 热重载（可选）
    
    // 配置
    ConfigSchema interface{}      // 配置结构（可选）
    
    // 可见性
    Hidden bool
}
```

### 4. 智能依赖注入

```go
// Manager 内置依赖注入容器
type Manager struct {
    plugins   map[string]*PluginInstance
    container *Container
}

type Container struct {
    services map[string]interface{}
}

// 自动注入依赖
func (m *Manager) Register(desc *PluginDescriptor) error {
    // 1. 检查依赖
    for _, dep := range desc.Deps {
        if !m.Has(dep) {
            return fmt.Errorf("missing dependency: %s", dep)
        }
    }
    
    // 2. 创建 SetupContext
    ctx := &SetupContext{
        Engine:    m.engine,
        Manager:   m,
        container: m.container,
    }
    
    // 3. 调用 Setup
    if err := desc.Setup(ctx); err != nil {
        return err
    }
    
    // 4. 注册到容器
    m.plugins[desc.Name] = &PluginInstance{
        desc: desc,
        // ...
    }
    
    return nil
}
```

### 5. 示例：简化后的插件

#### 旧方式（复杂）
```go
type GreeterPlugin struct {
    *plugin.BasePlugin
    Engine   *engine.Engine  `inject:"engine"`
    greeting string
}

func NewGreeterPlugin() *GreeterPlugin {
    return &GreeterPlugin{
        BasePlugin: plugin.NewBasePlugin("greeter"),
        greeting:   "Hello",
    }
}

func (p *GreeterPlugin) Load(eng *engine.Engine) error {
    p.BasePlugin.Load(eng)  // 容易忘记！
    
    eng.OnCommand(dto.C2CMessageCreate, "/greet").
        Handle(p.handleGreet)
    
    return nil
}

func (p *GreeterPlugin) handleGreet(ctx *eventctx.Context) error {
    // 处理逻辑
}
```

#### 新方式（简洁）
```go
func NewGreeterPlugin() *PluginDescriptor {
    greeting := "Hello"  // 闭包捕获状态
    
    return &PluginDescriptor{
        Name:    "greeter",
        Version: "1.0.0",
        Setup: func(ctx *SetupContext) error {
            // 直接注册，无需保存 engine 引用
            ctx.Engine.OnCommand(dto.C2CMessageCreate, "/greet").
                Handle(func(c *eventctx.Context) error {
                    return c.Reply(greeting + ", " + c.GetUserID())
                })
            
            ctx.Engine.OnCommand(dto.C2CMessageCreate, "/setgreeting").
                Handle(func(c *eventctx.Context) error {
                    args, _ := command.ParseCommandLine(c.GetMessageContent())
                    greeting = args.Get(0)
                    return c.Reply("Greeting updated")
                })
            
            return nil
        },
    }
}
```

---

## 迁移计划

### Phase 1: 新增简化 API（不破坏兼容性）

1. 创建新文件 `plugin/v2.go`
2. 实现 `PluginDescriptor` 和简化的注册机制
3. `Manager` 同时支持旧接口和新接口

```go
// 同时支持两种方式
func (m *Manager) Register(plugin interface{}) error {
    switch p := plugin.(type) {
    case Plugin:
        return m.registerLegacy(p)
    case *PluginDescriptor:
        return m.registerV2(p)
    default:
        return errors.New("invalid plugin type")
    }
}
```

### Phase 2: 重构现有插件

逐步将现有插件迁移到新 API：
- `plugins/core/help` → 新 API
- `plugins/core/cache` → 新 API
- `plugins/core/storage` → 新 API
- `plugins/core/permission` → 新 API
- `plugins/core/admin` → 新 API
- `plugins/dev/debug` → 新 API

### Phase 3: 标记旧 API 废弃

```go
// Deprecated: Use PluginDescriptor instead
type Plugin interface { ... }

// Deprecated: Use NewPluginDescriptor instead
type BasePlugin struct { ... }
```

### Phase 4: 移除旧 API（v2.0.0）

在主版本升级时完全移除旧 API。

---

## 对比总结

| 方面 | 旧设计 | 新设计 |
|------|--------|--------|
| **代码行数** | ~50行/插件 | ~20行/插件 |
| **继承层次** | 需要嵌入 BasePlugin | 无继承 |
| **依赖注入** | 手动 Setter + 标签 | 自动容器注入 |
| **状态管理** | 分散在多个方法 | 闭包捕获 |
| **易用性** | 需要记住多个方法 | 只需实现 Setup |
| **错误风险** | 高（容易忘记调用父类） | 低 |
| **Go 习惯** | ❌ 继承导向 | ✅ 组合导向 |

---

## 示例对比

### admin 插件重构前后

#### 旧代码（~1200 行）
```go
type Plugin struct {
    *plugin.BasePlugin
    PluginManager *plugin.Manager    `inject:"manager"`
    PermPlugin    *permission.Plugin `inject:"plugin:permission"`
}

func (p *Plugin) Load(eng *engine.Engine) error {
    p.registerPluginCommand(eng)
    p.registerPermCommand(eng)
    p.registerCodeCommand(eng)
    p.registerACLCommand(eng)
    p.registerSystemCommands(eng)
    return nil
}

func (p *Plugin) SetPluginManager(pm *plugin.Manager) {
    p.PluginManager = pm
}

func (p *Plugin) SetPermissionPlugin(pp *permission.Plugin) {
    p.PermPlugin = pp
}
```

#### 新代码（~800 行，-33%）
```go
func NewAdminPlugin() *PluginDescriptor {
    return &PluginDescriptor{
        Name:    "admin",
        Version: "1.0.0",
        Deps:    []string{"permission"},
        Setup: func(ctx *SetupContext) error {
            perm := ctx.MustGet("permission").(*permission.Plugin)
            
            registerPluginCommands(ctx.Engine, ctx.Manager)
            registerPermCommands(ctx.Engine, perm)
            registerCodeCommands(ctx.Engine, perm)
            registerACLCommands(ctx.Engine, perm)
            registerSystemCommands(ctx.Engine)
            
            return nil
        },
    }
}

// 命令注册变成纯函数
func registerPluginCommands(eng *engine.Engine, mgr *Manager) {
    eng.OnCommand(dto.C2CMessageCreate, "/plugin").Handle(...)
}
```

---

## 风险评估

### 低风险
- ✅ 新旧 API 可以共存
- ✅ 不影响现有用户代码
- ✅ 逐步迁移，不需要一次性重写

### 中风险
- ⚠️ 需要更新文档和示例
- ⚠️ 需要重新培训用户

### 高风险
- ❌ 无

---

## 建议

**立即实施**，理由：
1. 代码未发布，无历史包袱
2. 重构成本低，收益高
3. 符合 Go 语言最佳实践
4. 提升开发者体验

**实施顺序**：
1. 本周：实现 v2 API 核心（`PluginDescriptor`、`SetupContext`）
2. 下周：迁移 2-3 个核心插件作为示例
3. 第三周：更新文档和所有示例
4. 第四周：迁移剩余插件，标记旧 API 废弃

---

## 附录：完整示例

见下一节的实现代码。

