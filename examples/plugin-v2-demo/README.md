# Plugin V2 Demo

这个示例演示如何使用新的 v2 插件 API 创建插件。

## 新 API 的优势

### 旧方式（v1）
```go
type MyPlugin struct {
    *plugin.BasePlugin
    Engine *engine.Engine `inject:"engine"`
}

func (p *MyPlugin) Load(eng *engine.Engine) error {
    p.BasePlugin.Load(eng)  // 容易忘记！
    // 注册命令...
    return nil
}
```

### 新方式（v2）
```go
func NewMyPlugin() *plugin.PluginDescriptor {
    return &plugin.PluginDescriptor{
        Name: "myplugin",
        Setup: func(ctx *plugin.SetupContext) error {
            // 直接注册命令，无需继承
            ctx.Engine.OnCommand(dto.C2CMessageCreate, "/hello").
                Handle(func(c *eventctx.Context) error {
                    return c.Reply("Hello!")
                })
            return nil
        },
    }
}
```

## 主要改进

1. **无需继承** - 不再需要嵌入 `*plugin.BasePlugin`
2. **自动依赖注入** - 通过 `ctx.MustGet()` 获取依赖
3. **闭包状态管理** - 使用闭包捕获状态，无需结构体字段
4. **函数式风格** - 更符合 Go 语言习惯
5. **减少样板代码** - 代码量减少 40-60%

## 示例插件

### 1. Greeter Plugin（问候插件）
- 演示基本命令注册
- 演示状态管理（使用闭包）
- 命令：`/greet`, `/setgreeting <问候语>`

### 2. Counter Plugin（计数器插件）
- 演示配置读取
- 演示多命令注册
- 演示生命周期钩子
- 命令：`/count`, `/reset`, `/get`

### 3. Calculator Plugin（计算器插件）
- 演示更复杂的命令处理
- 演示错误处理
- 命令：`/calc <表达式>` （例如：`/calc 1 + 2`）

## 运行示例

1. 复制配置文件：
```bash
cp ../../config.example.yaml config.yaml
```

2. 编辑 `config.yaml`，填入你的机器人信息

3. 运行：
```bash
go run main.go
```

## 依赖注入示例

如果你的插件依赖其他插件：

```go
func NewMyPlugin() *plugin.PluginDescriptor {
    return &plugin.PluginDescriptor{
        Name: "myplugin",
        Deps: []string{"permission", "storage"},  // 声明依赖
        Setup: func(ctx *plugin.SetupContext) error {
            // 获取依赖插件
            perm := ctx.MustGet("permission").(*permission.Plugin)
            storage := ctx.MustGet("storage").(*storage.Plugin)
            
            // 使用依赖...
            return nil
        },
    }
}
```

## 热重载示例

```go
func NewMyPlugin() *plugin.PluginDescriptor {
    state := &MyState{}
    
    return &plugin.PluginDescriptor{
        Name: "myplugin",
        Setup: func(ctx *plugin.SetupContext) error {
            // 初始化
            return nil
        },
        Teardown: func() error {
            // 清理资源
            return nil
        },
        Reload: func(ctx *plugin.SetupContext) error {
            // 自定义热重载逻辑
            // 如果不定义，将使用默认的 Teardown + Setup
            return nil
        },
    }
}
```

## 与 v1 API 对比

| 特性 | v1 API | v2 API |
|------|--------|--------|
| 代码行数 | ~50行 | ~20行 |
| 继承 | 需要嵌入 BasePlugin | 无需继承 |
| 依赖注入 | 手动 Setter + 标签 | 自动注入 |
| 状态管理 | 结构体字段 | 闭包捕获 |
| 易用性 | 需要记住多个方法 | 只需实现 Setup |
| Go 惯用性 | ❌ | ✅ |

## 迁移指南

如果你有现有的 v1 插件，迁移步骤：

1. 将 `type MyPlugin struct` 改为函数 `func NewMyPlugin()`
2. 将 `Load()` 方法的内容移到 `Setup` 函数
3. 将 `Unload()` 方法的内容移到 `Teardown` 函数（可选）
4. 将结构体字段改为闭包变量
5. 移除所有 `inject` 标签，使用 `ctx.MustGet()` 获取依赖
6. 移除所有 Setter 方法（如 `SetPluginManager`）
7. 使用 `manager.RegisterV2()` 而不是 `manager.Register()`

## 注意事项

- v2 API 与 v1 API 完全兼容，可以混用
- 建议新插件使用 v2 API
- 旧插件可以逐步迁移
- v1 API 目前不会被移除，但推荐使用 v2

