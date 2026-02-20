# Debug 子命令演示程序

这个示例展示了如何使用 Debug 插件的子命令功能，以及如何通过子命令模式简化命令注册和管理。

## 功能特性

### 1. 子命令架构演示

Debug 插件使用了优化的子命令架构：
- ✅ 单个主命令 `/debug` 包含 8 个子命令
- ✅ 统一的命令分发逻辑
- ✅ 清晰的命令结构和帮助信息
- ✅ 易于扩展新的调试功能

### 2. 完整的调试工具集

#### 事件调试
- `/debug event` - 查看当前事件的详细信息
- `/debug ctx` - 查看上下文的所有数据
- `/debug matcher <命令>` - 查看命令匹配器信息

#### 系统调试
- `/debug runtime` - 查看运行时信息（内存、goroutine）
- `/debug commands` - 列出所有注册的命令
- `/debug plugins` - 查看所有插件状态

#### 性能分析
- `/debug bench <命令>` - 测试命令执行性能
- `/debug stats` - 查看系统统计信息

### 3. 权限控制

- 支持基于角色的权限控制
- 开发模式下允许所有用户访问
- 生产模式下仅限授权用户

## 快速开始

### 1. 设置环境变量

```bash
# Windows PowerShell
$env:BOT_APPID="你的机器人AppID"
$env:BOT_TOKEN="你的机器人Token"
$env:ADMIN_USER_ID="管理员用户ID"  # 可选

# Linux/macOS
export BOT_APPID="你的机器人AppID"
export BOT_TOKEN="你的机器人Token"
export ADMIN_USER_ID="管理员用户ID"  # 可选
```

### 2. 运行程序

```bash
cd examples/debug-subcommand-demo
go run main.go
```

### 3. 测试命令

在私聊中向机器人发送以下命令：

```
/debug                    # 查看所有调试子命令
/help                     # 查看所有可用命令
/debug commands           # 查看命令注册情况
/debug plugins            # 查看插件状态
/debug event              # 查看事件详情
/debug runtime            # 查看运行时信息
/debug matcher hello      # 查看 hello 命令的匹配器
/debug bench echo         # 测试 echo 命令性能
```

## 代码结构

### 插件加载顺序

```go
1. Permission Plugin  → 权限管理（Debug 插件依赖）
2. Debug Plugin       → 调试工具
3. Help Plugin        → 帮助系统
```

### 示例命令注册

```go
// 注册示例命令用于测试
eng.OnCommand(dto.C2CMessageCreate, "/hello").
    SetDescription("打招呼命令").
    SetUsage("/hello").
    SetCategory("示例").
    Handle(func(ctx *eventctx.Context) error {
        return ctx.Reply("你好！👋")
    })
```

### Debug 插件配置

```go
debugPlugin := debug.New()
debugPlugin.SetDevMode(true)                    // 开发模式
debugPlugin.SetPermissionPlugin(permPlugin)     // 设置权限插件
```

## 子命令实现原理

### 1. 命令定义

```go
debugCmd := &command.Definition{
    Name:        "debug",
    Description: "开发调试工具集合",
    SubCommands: []*command.Definition{
        {Name: "event", Description: "显示事件信息"},
        {Name: "ctx", Description: "显示上下文信息"},
        // ... 更多子命令
    },
}
```

### 2. 命令注册

只需注册一次主命令：

```go
p.OnCommand(eng, dto.C2CMessageCreate, "/debug").
    SetDefinition(debugCmd).
    Handle(p.handleDebugCommand)
```

### 3. 命令分发

统一的分发逻辑：

```go
func (p *Plugin) handleDebugCommand(ctx *eventctx.Context) error {
    args, _ := command.ParseCommandLine(ctx.GetMessageContent())
    subCommand := args.Get(0)
    
    switch subCommand {
    case "event":
        return p.handleDebugEvent(ctx)
    case "ctx":
        return p.handleDebugContext(ctx)
    // ... 其他子命令
    default:
        return p.showDebugHelp(ctx)  // 显示帮助
    }
}
```

## 测试场景

### 场景 1: 命令发现

```
用户: /help
Bot: [显示所有命令列表，包括 /debug]

用户: /debug
Bot: [显示所有调试子命令的帮助信息]

用户: /help debug
Bot: [显示 debug 命令的详细信息和子命令]
```

### 场景 2: 系统诊断

```
用户: /debug commands
Bot: [列出所有注册的命令: /hello, /echo, /weather, /debug, /help]

用户: /debug plugins
Bot: [显示插件状态: Permission(已加载), Debug(已加载), Help(已加载)]

用户: /debug runtime
Bot: [显示: Goroutines: 15, 内存使用: 25.3 MB, GC次数: 3]
```

### 场景 3: 命令调试

```
用户: /debug matcher hello
Bot: [显示 hello 命令的匹配器详细信息]

用户: /debug event
Bot: [显示当前消息事件的所有字段]

用户: /debug ctx
Bot: [显示上下文中的所有数据]
```

### 场景 4: 性能测试

```
用户: /debug bench echo
Bot: [显示 echo 命令的性能测试结果]
```

## 权限配置

### 开发模式（默认）

```go
debugPlugin.SetDevMode(true)
// 允许所有用户使用所有 debug 命令
```

### 生产模式

```go
debugPlugin.SetDevMode(false)
debugPlugin.SetPermissionPlugin(permPlugin)

// 创建管理员角色
permPlugin.AddRole("admin")
permPlugin.GrantPermission("admin", "debug.*")

// 授权特定用户
permPlugin.AssignRole(adminUserID, "admin")
```

### 细粒度权限

```go
// 只允许查看，不允许执行敏感操作
permPlugin.GrantPermission("viewer", "debug.commands")
permPlugin.GrantPermission("viewer", "debug.plugins")
permPlugin.GrantPermission("viewer", "debug.event")

// 允许性能测试
permPlugin.GrantPermission("tester", "debug.bench")
permPlugin.GrantPermission("tester", "debug.stats")
```

## 优势对比

### 传统方式（16 个独立命令）

```go
// 私聊
OnCommand(eng, C2C, "/debug event").Handle(handleEvent)
OnCommand(eng, C2C, "/debug ctx").Handle(handleCtx)
OnCommand(eng, C2C, "/debug matcher").Handle(handleMatcher)
// ... 5 个更多

// 群聊
OnCommand(eng, Group, "/debug event").Handle(handleEvent)
OnCommand(eng, Group, "/debug ctx").Handle(handleCtx)
OnCommand(eng, Group, "/debug matcher").Handle(handleMatcher)
// ... 5 个更多

// 缺点：
// ❌ 代码重复度高
// ❌ 难以维护
// ❌ 添加新命令需要修改多处
```

### 子命令方式（2 个主命令 + 8 个子命令）

```go
// 定义一次
debugCmd := &command.Definition{
    SubCommands: [...],
}

// 注册两次（私聊和群聊）
OnCommand(eng, C2C, "/debug").SetDefinition(debugCmd).Handle(handleDebug)
OnCommand(eng, Group, "/debug").SetDefinition(debugCmd).Handle(handleDebug)

// 统一分发
func handleDebug(ctx) {
    switch subCommand {
    case "event": return handleEvent(ctx)
    case "ctx": return handleCtx(ctx)
    // ...
    }
}

// 优点：
// ✅ 代码简洁清晰
// ✅ 易于维护
// ✅ 添加新命令只需修改 switch 和定义
// ✅ 自动生成帮助信息
```

## 扩展示例

### 添加新的调试子命令

只需三步：

1. 在 `SubCommands` 中添加定义：

```go
{
    Name:        "config",
    Description: "显示配置信息",
    Usage:       "/debug config",
    Examples:    []string{"/debug config"},
}
```

2. 在 `switch` 中添加分支：

```go
case "config":
    return p.handleDebugConfig(ctx)
```

3. 实现处理函数：

```go
func (p *Plugin) handleDebugConfig(ctx *eventctx.Context) error {
    // 显示配置信息
    return ctx.Reply("配置信息：...")
}
```

## 注意事项

1. **环境变量**: 确保设置了 `BOT_APPID` 和 `BOT_TOKEN`
2. **权限控制**: 生产环境建议关闭开发模式
3. **性能影响**: `debug bench` 命令会执行多次测试，注意性能影响
4. **敏感信息**: `debug event` 和 `debug ctx` 可能包含敏感信息

## 相关文档

- [Debug Plugin 源码](../../plugins/dev/debug/)
- [子命令优化报告](../../docs/06-archived/debug-plugin-subcommand-optimization.md)
- [Command 包文档](../../command/)
- [Plugin 系统文档](../../plugin/)

## 总结

这个示例展示了：
- ✅ 子命令模式的优势（代码减少 87.5%）
- ✅ 统一的命令分发逻辑
- ✅ 完整的调试工具集
- ✅ 权限控制集成
- ✅ 易于扩展的架构

子命令模式非常适合具有多个相关功能的插件，推荐在类似场景中使用。

