# Plugin v1 到 v2 迁移指南

**创建日期**: 2026-02-19  
**目标用户**: Remilia 插件开发者  
**难度**: 简单

---

## 📝 概述

本指南帮助你将 v1 插件迁移到 v2 API。v2 API 提供了更简洁的代码、自动依赖注入和更好的类型安全。

**迁移后的收益**:
- ✅ 代码减少 60%
- ✅ 无需继承 BasePlugin
- ✅ 自动依赖注入
- ✅ 自动 Matcher 追踪
- ✅ 更符合 Go 习惯用法

---

## 🔄 迁移步骤

### 步骤 1: 理解基本差异

**v1 插件** (基于继承):
```go
type MyPlugin struct {
    *plugin.BasePlugin
    state *MyState
}

func NewMyPlugin() *MyPlugin {
    return &MyPlugin{
        BasePlugin: plugin.NewBasePlugin("myplugin"),
        state:      &MyState{},
    }
}

func (p *MyPlugin) Load(eng *engine.Engine) error {
    eng.OnCommand(dto.C2CMessageCreate, "/hello").
        Handle(func(ctx *eventctx.Context) error {
            // 使用 p.state
            return ctx.Reply("Hello!")
        })
    return nil
}

func (p *MyPlugin) Unload(eng *engine.Engine) error {
    return p.BasePlugin.Unload(eng)
}
```

**v2 插件** (函数式):
```go
func New() *plugin.PluginDescriptor {
    // 使用闭包捕获状态
    state := &MyState{}
    
    return &plugin.PluginDescriptor{
        Name:    "myplugin",
        Version: "2.0.0",
        
        Setup: func(ctx *plugin.SetupContext) error {
            // 使用 RegisterCommand 自动追踪
            ctx.RegisterCommand(dto.C2CMessageCreate, "/hello").
                Handle(func(c *eventctx.Context) error {
                    // 使用 state
                    return c.Reply("Hello!")
                })
            return nil
        },
        
        Teardown: func() error {
            // 清理 state
            return nil
        },
    }
}
```

### 步骤 2: 转换插件结构

#### 1. 删除结构体定义
```go
// ❌ v1: 需要定义结构体
type MyPlugin struct {
    *plugin.BasePlugin
    field1 string
    field2 int
}
```

```go
// ✅ v2: 使用闭包捕获状态
func New() *plugin.PluginDescriptor {
    field1 := ""
    field2 := 0
    
    return &plugin.PluginDescriptor{
        // ...
    }
}
```

#### 2. 转换构造函数
```go
// ❌ v1
func NewMyPlugin() *MyPlugin {
    return &MyPlugin{
        BasePlugin: plugin.NewBasePlugin("myplugin"),
        field1:     "initial",
    }
}
```

```go
// ✅ v2
func New() *plugin.PluginDescriptor {
    field1 := "initial"
    
    return &plugin.PluginDescriptor{
        Name:    "myplugin",
        Version: "2.0.0",
        // ...
    }
}
```

#### 3. 转换 Load 方法
```go
// ❌ v1
func (p *MyPlugin) Load(eng *engine.Engine) error {
    eng.OnCommand(dto.C2CMessageCreate, "/cmd").
        Handle(p.handleCommand)
    return nil
}

func (p *MyPlugin) handleCommand(ctx *eventctx.Context) error {
    // 使用 p.field1
    return ctx.Reply(p.field1)
}
```

```go
// ✅ v2
Setup: func(ctx *plugin.SetupContext) error {
    // 使用 RegisterCommand 自动追踪
    ctx.RegisterCommand(dto.C2CMessageCreate, "/cmd").
        Handle(func(c *eventctx.Context) error {
            // 直接使用闭包中的 field1
            return c.Reply(field1)
        })
    return nil
},
```

#### 4. 转换 Unload 方法
```go
// ❌ v1
func (p *MyPlugin) Unload(eng *engine.Engine) error {
    // 清理资源
    p.field1 = ""
    return p.BasePlugin.Unload(eng)
}
```

```go
// ✅ v2
Teardown: func() error {
    // 清理资源
    field1 = ""
    return nil
},
```

### 步骤 3: 处理依赖

#### v1: 手动依赖注入
```go
type MyPlugin struct {
    *plugin.BasePlugin
    dep *OtherPlugin
}

func (p *MyPlugin) Load(eng *engine.Engine) error {
    // 需要手动获取依赖
    // ...
}
```

#### v2: 自动依赖注入
```go
func New() *plugin.PluginDescriptor {
    return &plugin.PluginDescriptor{
        Name: "myplugin",
        Deps: []string{"otherplugin"},  // 声明依赖
        
        Setup: func(ctx *plugin.SetupContext) error {
            // 自动注入
            dep := ctx.MustGet("otherplugin").(*OtherPlugin)
            
            // 使用依赖
            dep.DoSomething()
            return nil
        },
    }
}
```

### 步骤 4: 更新注册代码

```go
// ❌ v1
plugin := NewMyPlugin()
manager.Register(plugin)
```

```go
// ✅ v2
manager.RegisterV2(New())
```

---

## 📚 完整示例

### 示例 1: 简单插件

**v1 代码** (50 行):
```go
type EchoPlugin struct {
    *plugin.BasePlugin
}

func NewEchoPlugin() *EchoPlugin {
    return &EchoPlugin{
        BasePlugin: plugin.NewBasePlugin("echo"),
    }
}

func (p *EchoPlugin) Load(eng *engine.Engine) error {
    eng.OnCommand(dto.C2CMessageCreate, "/echo").
        Handle(func(ctx *eventctx.Context) error {
            msg := ctx.GetPlainText()
            return ctx.Reply("Echo: " + msg)
        })
    return nil
}

func (p *EchoPlugin) Unload(eng *engine.Engine) error {
    return p.BasePlugin.Unload(eng)
}
```

**v2 代码** (20 行):
```go
func New() *plugin.PluginDescriptor {
    return &plugin.PluginDescriptor{
        Name:        "echo",
        Version:     "2.0.0",
        Description: "Echo 插件",
        
        Setup: func(ctx *plugin.SetupContext) error {
            ctx.RegisterCommand(dto.C2CMessageCreate, "/echo").
                Handle(func(c *eventctx.Context) error {
                    msg := c.GetPlainText()
                    return c.Reply("Echo: " + msg)
                })
            return nil
        },
    }
}
```

### 示例 2: 有状态的插件

**v1 代码**:
```go
type CounterPlugin struct {
    *plugin.BasePlugin
    count atomic.Int64
}

func NewCounterPlugin() *CounterPlugin {
    return &CounterPlugin{
        BasePlugin: plugin.NewBasePlugin("counter"),
    }
}

func (p *CounterPlugin) Load(eng *engine.Engine) error {
    eng.OnCommand(dto.C2CMessageCreate, "/count").
        Handle(func(ctx *eventctx.Context) error {
            count := p.count.Add(1)
            return ctx.Reply(fmt.Sprintf("Count: %d", count))
        })
    return nil
}

func (p *CounterPlugin) Unload(eng *engine.Engine) error {
    return p.BasePlugin.Unload(eng)
}
```

**v2 代码**:
```go
func New() *plugin.PluginDescriptor {
    var count atomic.Int64  // 闭包捕获
    
    return &plugin.PluginDescriptor{
        Name:    "counter",
        Version: "2.0.0",
        
        Setup: func(ctx *plugin.SetupContext) error {
            ctx.RegisterCommand(dto.C2CMessageCreate, "/count").
                Handle(func(c *eventctx.Context) error {
                    n := count.Add(1)
                    return c.Reply(fmt.Sprintf("Count: %d", n))
                })
            return nil
        },
    }
}
```

### 示例 3: 带依赖的插件

**v1 代码**:
```go
type MyPlugin struct {
    *plugin.BasePlugin
    perm *permission.Plugin
}

func (p *MyPlugin) Dependencies() []string {
    return []string{"permission"}
}

func (p *MyPlugin) Load(eng *engine.Engine) error {
    // 需要手动获取依赖...
}
```

**v2 代码**:
```go
func New() *plugin.PluginDescriptor {
    return &plugin.PluginDescriptor{
        Name: "myplugin",
        Deps: []string{"permission"},
        
        Setup: func(ctx *plugin.SetupContext) error {
            // 自动注入
            perm := ctx.MustGet("permission").(*permission.Plugin)
            
            ctx.RegisterCommand(dto.C2CMessageCreate, "/admin").
                Handle(func(c *eventctx.Context) error {
                    if !perm.HasPermission(c.UserID, "admin") {
                        return c.Reply("无权限")
                    }
                    return c.Reply("管理命令")
                })
            return nil
        },
    }
}
```

---

## ✅ 迁移检查清单

### 代码修改
- [ ] 删除结构体定义（使用闭包替代）
- [ ] 转换构造函数为 `func New() *plugin.PluginDescriptor`
- [ ] 将 `Load()` 转换为 `Setup` 函数
- [ ] 将 `Unload()` 转换为 `Teardown` 函数（可选）
- [ ] 使用 `ctx.RegisterCommand()` 替代 `eng.OnCommand()`
- [ ] 声明依赖到 `Deps` 字段
- [ ] 使用 `ctx.MustGet()` 获取依赖

### 元数据
- [ ] 添加 `Version` 字段
- [ ] 添加 `Description` 字段
- [ ] 添加 `Category` 和 `Tags`（可选）
- [ ] 添加 `HelpText`（可选）

### 注册
- [ ] 使用 `manager.RegisterV2()` 替代 `manager.Register()`

### 测试
- [ ] 编译通过
- [ ] 功能测试通过
- [ ] 依赖注入正常工作

---

## 🚨 常见问题

### Q1: 如何在闭包中修改状态？
```go
func New() *plugin.PluginDescriptor {
    count := 0  // 可以直接修改
    
    return &plugin.PluginDescriptor{
        Setup: func(ctx *plugin.SetupContext) error {
            ctx.RegisterCommand(dto.C2CMessageCreate, "/inc").
                Handle(func(c *eventctx.Context) error {
                    count++  // ✅ 可以直接修改
                    return c.Reply(fmt.Sprintf("%d", count))
                })
            return nil
        },
    }
}
```

### Q2: 如何共享状态给其他插件？
```go
Setup: func(ctx *plugin.SetupContext) error {
    api := &MyPluginAPI{
        DoSomething: func() { /* ... */ },
    }
    
    // 注册到容器供其他插件使用
    ctx.Manager.GetContainer().Register("myplugin_api", api)
    return nil
},
```

### Q3: v1 的 OnCommand 为什么不推荐？
```go
// ❌ 不推荐：无法追踪 Matcher
ctx.Engine.OnCommand(dto.C2CMessageCreate, "/cmd")

// ✅ 推荐：自动追踪，支持热重载
ctx.RegisterCommand(dto.C2CMessageCreate, "/cmd")
```

### Q4: 如何实现热重载？
```go
func New() *plugin.PluginDescriptor {
    config := &MyConfig{}
    
    return &plugin.PluginDescriptor{
        Name: "myplugin",
        
        Setup: func(ctx *plugin.SetupContext) error {
            config.Load()  // 加载配置
            return nil
        },
        
        Reload: func(ctx *plugin.SetupContext) error {
            config.Reload()  // 重新加载配置
            return nil
        },
    }
}
```

### Q5: 并发安全吗？
v2 API 的并发安全由你自己保证。对于共享状态，使用适当的同步机制：

```go
func New() *plugin.PluginDescriptor {
    var count atomic.Int64  // ✅ 原子类型
    
    // 或使用互斥锁
    var (
        mu    sync.Mutex
        state map[string]string
    )
    
    return &plugin.PluginDescriptor{
        // ...
    }
}
```

---

## 📊 v1 vs v2 对比

| 特性 | v1 | v2 | 改进 |
|------|----|----|------|
| 代码行数 | ~50 行 | ~20 行 | **-60%** |
| 需要继承 | ✅ 是 | ❌ 否 | ✅ 简化 |
| 样板代码 | 多 | 少 | **-70%** |
| 依赖注入 | 手动 | 自动 | ✅ 简化 |
| Matcher 追踪 | 手动 | 自动 | ✅ 新功能 |
| 状态管理 | 字段 | 闭包 | ✅ 更灵活 |
| 类型安全 | 低 | 高 | ✅ 提升 |
| Go 惯用法 | ❌ 否 | ✅ 是 | ✅ 符合 |

---

## 🎯 推荐实践

### 1. 使用 RegisterCommand
```go
// ✅ 推荐
ctx.RegisterCommand(dto.C2CMessageCreate, "/cmd")

// ❌ 不推荐（无法追踪）
ctx.Engine.OnCommand(dto.C2CMessageCreate, "/cmd")
```

### 2. 明确声明依赖
```go
return &plugin.PluginDescriptor{
    Deps: []string{"permission", "storage"},  // ✅ 清晰
    Setup: func(ctx *plugin.SetupContext) error {
        perm := ctx.MustGet("permission")
        storage := ctx.MustGet("storage")
        // ...
    },
}
```

### 3. 提供完整的元数据
```go
return &plugin.PluginDescriptor{
    Name:        "myplugin",
    Version:     "2.0.0",      // ✅ 版本号
    Author:      "Your Name",  // ✅ 作者
    Description: "...",        // ✅ 描述
    Category:    "工具",       // ✅ 分类
    Tags:        []string{},   // ✅ 标签
    HelpText:    "...",        // ✅ 帮助文本
}
```

### 4. 合理使用 Teardown
```go
Teardown: func() error {
    // 只在需要时定义
    // 关闭连接、保存状态、清理资源等
    return nil
},
```

---

## 📚 参考资料

- **v2 快速参考**: `docs/05-reports/plugin-v2-quick-reference.md`
- **v2 完成报告**: `docs/05-reports/plugin-v2-migration-complete.md`
- **P0 修复报告**: `docs/05-reports/plugin-v2-p0-fixes-complete.md`
- **示例代码**: `examples/plugin-v2-demo/`

---

## 🆘 需要帮助？

如果在迁移过程中遇到问题：
1. 查看 `examples/` 目录中的示例代码
2. 阅读 v2 快速参考文档
3. 检查错误日志
4. 提交 Issue

---

**最后更新**: 2026-02-19  
**文档版本**: 1.0

