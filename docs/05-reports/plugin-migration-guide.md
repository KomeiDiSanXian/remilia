# 插件迁移指南：从 v1 到 v2

## 快速对比

### v1 插件结构
```go
type MyPlugin struct {
    *plugin.BasePlugin
    Engine        *engine.Engine     `inject:"engine"`
    PluginManager *plugin.Manager    `inject:"manager"`
    DepPlugin     *other.Plugin      `inject:"plugin:other"`
}

func NewMyPlugin() *MyPlugin {
    return &MyPlugin{
        BasePlugin: plugin.NewBasePlugin("myplugin"),
    }
}

func (p *MyPlugin) Load(eng *engine.Engine) error {
    p.BasePlugin.Load(eng)  // 容易忘记！
    
    eng.OnCommand(dto.C2CMessageCreate, "/hello").
        Handle(p.handleHello)
    
    return nil
}

func (p *MyPlugin) Unload(eng *engine.Engine) error {
    return p.BasePlugin.Unload(eng)
}

func (p *MyPlugin) SetPluginManager(pm *plugin.Manager) {
    p.PluginManager = pm
}

func (p *MyPlugin) SetDepPlugin(dp *other.Plugin) {
    p.DepPlugin = dp
}

func (p *MyPlugin) handleHello(ctx *eventctx.Context) error {
    // 处理逻辑
}
```

### v2 插件结构
```go
func NewMyPlugin() *plugin.PluginDescriptor {
    // 状态变量（如果需要）
    var state SomeState
    
    return &plugin.PluginDescriptor{
        Name:    "myplugin",
        Version: "2.0.0",
        Deps:    []string{"other"},
        
        Setup: func(ctx *plugin.SetupContext) error {
            // 获取依赖（自动注入）
            dep := ctx.MustGet("other").(*other.Plugin)
            
            // 注册命令
            ctx.Engine.OnCommand(dto.C2CMessageCreate, "/hello").
                Handle(func(c *eventctx.Context) error {
                    // 处理逻辑（可以访问 state 和 dep）
                    return nil
                })
            
            return nil
        },
        
        Teardown: func() error {
            // 清理资源
            return nil
        },
    }
}
```

## 逐步迁移

### 步骤 1：修改构造函数

**v1:**
```go
func NewMyPlugin() *MyPlugin {
    return &MyPlugin{
        BasePlugin: plugin.NewBasePlugin("myplugin"),
    }
}
```

**v2:**
```go
func NewMyPlugin() *plugin.PluginDescriptor {
    return &plugin.PluginDescriptor{
        Name:    "myplugin",
        Version: "1.0.0",
        Author:  "Your Name",
        // ...其他元数据
    }
}
```

### 步骤 2：移动 Load 逻辑到 Setup

**v1:**
```go
func (p *MyPlugin) Load(eng *engine.Engine) error {
    p.BasePlugin.Load(eng)
    
    eng.OnCommand(dto.C2CMessageCreate, "/hello").
        Handle(p.handleHello)
    
    return nil
}
```

**v2:**
```go
Setup: func(ctx *plugin.SetupContext) error {
    ctx.Engine.OnCommand(dto.C2CMessageCreate, "/hello").
        Handle(func(c *eventctx.Context) error {
            // 内联处理或调用外部函数
            return handleHello(c)
        })
    
    return nil
}
```

### 步骤 3：移动 Unload 逻辑到 Teardown

**v1:**
```go
func (p *MyPlugin) Unload(eng *engine.Engine) error {
    // 清理资源
    return p.BasePlugin.Unload(eng)
}
```

**v2:**
```go
Teardown: func() error {
    // 清理资源
    return nil
}
```

### 步骤 4：转换依赖注入

**v1:**
```go
type MyPlugin struct {
    *plugin.BasePlugin
    DepPlugin *other.Plugin `inject:"plugin:other"`
}

func (p *MyPlugin) SetDepPlugin(dp *other.Plugin) {
    p.DepPlugin = dp
}
```

**v2:**
```go
Deps: []string{"other"},

Setup: func(ctx *plugin.SetupContext) error {
    dep := ctx.MustGet("other").(*other.Plugin)
    // 使用 dep...
    return nil
}
```

### 步骤 5：转换状态管理

**v1:**
```go
type MyPlugin struct {
    *plugin.BasePlugin
    count int
    config Config
}

func (p *MyPlugin) handleCommand(ctx *eventctx.Context) error {
    p.count++
    // 使用 p.config
}
```

**v2:**
```go
func NewMyPlugin() *plugin.PluginDescriptor {
    // 使用闭包捕获状态
    count := 0
    config := Config{}
    
    return &plugin.PluginDescriptor{
        Setup: func(ctx *plugin.SetupContext) error {
            ctx.Engine.OnCommand(...).Handle(func(c *eventctx.Context) error {
                count++  // 直接使用闭包变量
                // 使用 config
                return nil
            })
            return nil
        },
    }
}
```

### 步骤 6：修改注册代码

**v1:**
```go
manager.Register(NewMyPlugin())
```

**v2:**
```go
manager.RegisterV2(NewMyPlugin())
```

## 完整迁移示例

### 迁移前（v1）

```go
package myplugin

import (
    "github.com/KomeiDiSanXian/remilia/core/engine"
    "github.com/KomeiDiSanXian/remilia/plugin"
)

type Plugin struct {
    *plugin.BasePlugin
    Engine   *engine.Engine `inject:"engine"`
    count    int
}

func New() *Plugin {
    return &Plugin{
        BasePlugin: plugin.NewBasePlugin("myplugin"),
        count:      0,
    }
}

func (p *Plugin) Load(eng *engine.Engine) error {
    p.BasePlugin.Load(eng)
    
    eng.OnCommand(dto.C2CMessageCreate, "/count").
        Handle(p.handleCount)
    
    return nil
}

func (p *Plugin) Unload(eng *engine.Engine) error {
    logger.Info("Final count:", p.count)
    return p.BasePlugin.Unload(eng)
}

func (p *Plugin) handleCount(ctx *eventctx.Context) error {
    p.count++
    _, err := ctx.ReplyPrivate(&dto.Message{
        Type:    dto.TextMessage,
        Content: fmt.Sprintf("Count: %d", p.count),
    })
    return err
}
```

### 迁移后（v2）

```go
package myplugin

import (
    "github.com/KomeiDiSanXian/remilia/plugin"
)

func New() *plugin.PluginDescriptor {
    count := 0  // 闭包状态
    
    return &plugin.PluginDescriptor{
        Name:        "myplugin",
        Version:     "2.0.0",
        Description: "My awesome plugin",
        
        Setup: func(ctx *plugin.SetupContext) error {
            ctx.Engine.OnCommand(dto.C2CMessageCreate, "/count").
                Handle(func(c *eventctx.Context) error {
                    count++
                    _, err := c.ReplyPrivate(&dto.Message{
                        Type:    dto.TextMessage,
                        Content: fmt.Sprintf("Count: %d", count),
                    })
                    return err
                })
            
            return nil
        },
        
        Teardown: func() error {
            logger.Info("Final count:", count)
            return nil
        },
    }
}
```

## 常见模式转换

### 1. 方法变函数

**v1:**
```go
func (p *MyPlugin) handleCommand(ctx *eventctx.Context) error {
    // 使用 p.field
}
```

**v2:**
```go
// 选项 A：内联
Handle(func(c *eventctx.Context) error {
    // 使用闭包变量
})

// 选项 B：外部函数
func handleCommand(dep *SomeDep) func(*eventctx.Context) error {
    return func(c *eventctx.Context) error {
        // 使用 dep
    }
}
```

### 2. 配置读取

**v1:**
```go
func (p *MyPlugin) Load(eng *engine.Engine) error {
    if p.config != nil {
        value := p.config.GetString("key", "default")
    }
}
```

**v2:**
```go
Setup: func(ctx *plugin.SetupContext) error {
    if ctx.Config != nil {
        value := ctx.Config.GetString("key", "default")
    }
}
```

### 3. 依赖多个插件

**v1:**
```go
type MyPlugin struct {
    *plugin.BasePlugin
    Dep1 *plugin1.Plugin `inject:"plugin:plugin1"`
    Dep2 *plugin2.Plugin `inject:"plugin:plugin2"`
}
```

**v2:**
```go
Deps: []string{"plugin1", "plugin2"},

Setup: func(ctx *plugin.SetupContext) error {
    dep1 := ctx.MustGet("plugin1").(*plugin1.Plugin)
    dep2 := ctx.MustGet("plugin2").(*plugin2.Plugin)
    // 使用依赖...
}
```

### 4. 热重载

**v1:**
```go
func (p *MyPlugin) Reload(eng *engine.Engine) error {
    // 自定义重载逻辑
    return p.BasePlugin.Reload(eng)
}
```

**v2:**
```go
Reload: func(ctx *plugin.SetupContext) error {
    // 自定义重载逻辑
    // 如果不定义，默认使用 Teardown + Setup
    return nil
}
```

## 检查清单

迁移完成后，确认：

- [ ] 移除了 `*plugin.BasePlugin` 嵌入
- [ ] 移除了所有 `inject` 标签
- [ ] 移除了所有 Setter 方法（`SetXxx`）
- [ ] `Load()` 逻辑已移至 `Setup`
- [ ] `Unload()` 逻辑已移至 `Teardown`（可选）
- [ ] 依赖使用 `ctx.MustGet()` 获取
- [ ] 状态使用闭包变量而非结构体字段
- [ ] 使用 `manager.RegisterV2()` 注册
- [ ] 测试所有功能正常

## 常见问题

### Q: 可以保留 v1 和 v2 共存吗？
A: 可以！v1 和 v2 完全兼容，可以在同一项目中混用。

### Q: 需要一次性迁移所有插件吗？
A: 不需要。可以逐个迁移，新插件用 v2，旧插件继续用 v1。

### Q: v2 性能如何？
A: 完全一样。v2 只是更简洁的 API，无运行时开销。

### Q: 如何处理复杂状态？
A: 使用闭包捕获结构体：
```go
func New() *plugin.PluginDescriptor {
    state := &ComplexState{
        Field1: value1,
        Field2: value2,
    }
    
    return &plugin.PluginDescriptor{
        Setup: func(ctx *plugin.SetupContext) error {
            // 使用 state
        },
    }
}
```

### Q: 如何共享处理函数？
A: 使用闭包或高阶函数：
```go
func New() *plugin.PluginDescriptor {
    handleCommand := func(c *eventctx.Context) error {
        // 共享逻辑
    }
    
    return &plugin.PluginDescriptor{
        Setup: func(ctx *plugin.SetupContext) error {
            ctx.Engine.OnCommand(...).Handle(handleCommand)
            ctx.Engine.OnCommand(...).Handle(handleCommand)  // 复用
        },
    }
}
```

## 需要帮助？

- 查看示例：`examples/plugin-v2-demo/`
- 阅读分析：`docs/05-reports/plugin-refactoring-analysis.md`
- 参考 API：`plugin/v2.go`

