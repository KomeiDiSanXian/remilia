# Debug Plugin

调试插件 - 提供开发调试工具集合

## 概述

Debug 插件是 Remilia 框架的开发者工具插件，提供事件查看、上下文检查、命令调试、性能分析等功能。该插件主要用于开发和调试阶段，帮助开发者快速定位问题。

## 功能特性

### 事件调试

- **查看事件详情** (`/debug event`)
  - 显示当前事件的完整信息
  - 包括事件类型、ID、消息内容、用户信息、群组信息等
  - 显示原始事件数据

- **查看上下文信息** (`/debug ctx`)
  - 显示上下文的所有扩展数据
  - 显示中间件执行链
  - 显示解析的命令信息
  - 显示重试次数和其他元数据

- **查看命令匹配器** (`/debug matcher <命令>`)
  - 显示指定命令的详细定义
  - 显示命令参数、别名、示例
  - 显示支持的事件类型和匹配器数量

### 系统调试

- **运行时信息** (`/debug runtime`)
  - Goroutine 数量
  - 内存使用情况（分配、总分配、系统内存、GC 次数）
  - CPU 信息（核心数、GOMAXPROCS）
  - Go 版本和操作系统信息

- **命令列表** (`/debug commands`)
  - 列出所有注册的命令
  - 按事件类型分组显示
  - 显示每个命令的匹配器数量

- **插件状态** (`/debug plugins`)
  - 列出所有插件及其状态
  - 显示插件版本、作者、分类
  - 显示插件运行时长和最后错误
  - 显示插件依赖关系

### 性能分析

- **命令性能测试** (`/debug bench <命令>`)
  - 测试指定命令的执行性能
  - 显示平均耗时和总耗时
  - 支持多次迭代测试

- **系统统计** (`/debug stats`)
  - 命令总数和分布
  - 匹配器总数
  - 插件总数和状态分布
  - 运行时资源使用

## 安装

```go
import (
    "github.com/KomeiDiSanXian/remilia"
    "github.com/KomeiDiSanXian/remilia/plugins/dev/debug"
)

func main() {
    bot := remilia.NewBotBuilder().
        // ... 其他配置
        Build()

    // 创建并加载 Debug 插件
    debugPlugin := debug.New()
    bot.PluginManager().Register("debug", debugPlugin)
}
```

## 使用方法

### 基本命令

```bash
# 查看当前事件详情
/debug event

# 查看上下文信息
/debug ctx

# 查看命令匹配器
/debug matcher help

# 查看运行时信息
/debug runtime

# 查看所有命令
/debug commands

# 查看所有插件状态
/debug plugins

# 测试命令性能
/debug bench help

# 查看系统统计
/debug stats
```

### 使用示例

#### 1. 调试事件处理

当你需要查看机器人收到的事件详情时：

```
用户: /debug event

机器人:
🔍 事件详情
========================================

📋 事件类型: C2C_MESSAGE_CREATE
🆔 事件ID: 123456789
💬 消息内容: /debug event
👤 用户ID: user_123
👤 用户名: 张三
📨 消息ID: msg_456

📦 原始数据:
```json
{
  "type": "C2C_MESSAGE_CREATE",
  "id": "123456789",
  ...
}
```
```

#### 2. 检查上下文状态

查看当前上下文的所有扩展数据和中间件链：

```
用户: /debug ctx

机器人:
🔍 上下文信息
========================================

📝 标准 Context:
  - Err: <nil>
  - Deadline: 无

🔌 扩展数据:
  - user_role: admin
  - request_id: req_123

🔗 中间件链:
  1. logger
  2. recovery
  3. permission

⚙️ 解析的命令:
  - 命令: debug ctx
  - 参数数量: 0

🔄 重试信息:
  - 重试次数: 0
```

#### 3. 查看命令定义

查看某个命令的详细定义和用法：

```
用户: /debug matcher help

机器人:
🔍 命令匹配器: help
========================================

📋 命令定义:
  - 名称: help
  - 描述: 显示帮助信息
  - 用法: /help [命令|插件|页码]
  - 分类: 系统
  - 示例:
    · /help
    · /help 2
    · /help plugins

📡 支持的事件类型:
  - C2C_MESSAGE_CREATE

🎯 匹配器数量: 2
```

#### 4. 性能分析

测试命令的执行性能：

```
用户: /debug bench help

机器人:
🔍 性能测试: help
========================================

📊 测试次数: 10
⏱️ 平均耗时: 123µs
⏱️ 总耗时: 1.23ms

⚠️ 注意: 此测试仅测量上下文创建开销，不包括实际命令执行
```

## 配置

### 开发模式

```go
debugPlugin := debug.New()

// 设置开发模式（默认开启）
debugPlugin.SetDevMode(true)

// 关闭开发模式（生产环境建议关闭）
debugPlugin.SetDevMode(false)
```

### 权限配置

Debug 插件需要以下权限：

- `debug.view` - 查看调试信息（事件、上下文、命令等）
- `debug.bench` - 执行性能测试

如果没有配置权限插件，在开发模式下默认允许所有操作。

## 安全建议

1. **仅在开发环境使用**
   - Debug 插件会暴露系统内部信息
   - 生产环境应禁用或严格限制权限

2. **限制访问权限**
   - 仅授予可信用户 `debug.*` 权限
   - 建议只在私聊中使用

3. **敏感信息保护**
   - 注意事件数据中可能包含敏感信息
   - 不要在群组中使用 `/debug event` 等命令

## 与其他插件集成

### 与 Permission 插件集成

```go
import (
    "github.com/KomeiDiSanXian/remilia/plugins/core/permission"
    "github.com/KomeiDiSanXian/remilia/plugins/dev/debug"
)

// 创建权限插件
permPlugin := permission.New()

// 创建 Debug 插件
debugPlugin := debug.New()
debugPlugin.SetPermissionPlugin(permPlugin)

// 授予用户调试权限
permPlugin.GrantPermission("user_123", "debug.view")
permPlugin.GrantPermission("user_123", "debug.bench")
```

### 与 Plugin Manager 集成

```go
// Debug 插件会自动集成插件管理器
// 这样可以查看所有插件的状态

bot := remilia.NewBotBuilder().Build()
pm := bot.PluginManager()

debugPlugin := debug.New()
pm.Register("debug", debugPlugin)

// Debug 插件会自动获取 PluginManager 引用
```

## API 文档

### Plugin 结构

```go
type Plugin struct {
    *plugin.BasePlugin
    engine        *engine.Engine
    permPlugin    *permission.Plugin
    devMode       bool
    pluginManager *plugin.Manager
}
```

### 方法

#### New() *Plugin

创建一个新的 Debug 插件实例。

**返回值:**
- `*Plugin`: Debug 插件实例

#### SetDevMode(enabled bool)

设置开发模式。

**参数:**
- `enabled`: 是否启用开发模式

#### SetPermissionPlugin(pp *permission.Plugin)

设置权限插件。

**参数:**
- `pp`: 权限插件实例

#### SetPluginManager(pm *plugin.Manager)

设置插件管理器。

**参数:**
- `pm`: 插件管理器实例

## 故障排除

### 问题：权限不足

**症状**: 使用调试命令时返回 "❌ 权限不足"

**解决方案**:
1. 确认已授予用户相应权限（`debug.view` 或 `debug.bench`）
2. 或者在开发模式下禁用权限检查：
   ```go
   debugPlugin.SetDevMode(true)
   ```

### 问题：插件管理器未初始化

**症状**: 使用 `/debug plugins` 时返回 "❌ 插件管理器未初始化"

**解决方案**:
确保 Debug 插件已正确注册到插件管理器：
```go
bot.PluginManager().Register("debug", debugPlugin)
```

### 问题：命令未注册

**症状**: 使用 `/debug matcher <命令>` 时返回 "❌ 未找到命令"

**解决方案**:
1. 确认命令名称正确（不需要 `/` 前缀）
2. 使用 `/debug commands` 查看所有已注册的命令

## 开发指南

### 添加新的调试命令

```go
// 在 registerDebugCommands 方法中添加新命令
func (p *Plugin) registerDebugCommands(eng *engine.Engine) {
    // ... 现有命令

    // 添加新的调试命令
    p.OnCommand(eng, dto.C2CMessageCreate, "/debug mycommand").
        Handle(p.handleDebugMyCommand)
}

// 实现处理函数
func (p *Plugin) handleDebugMyCommand(ctx *eventctx.Context) error {
    // 检查权限
    if !p.checkPermission(ctx, "debug.view") {
        return p.reply(ctx, "❌ 权限不足")
    }

    // 实现你的调试逻辑
    // ...

    return p.reply(ctx, "调试信息")
}
```

### 扩展权限检查

```go
// 添加新的权限类型
const (
    PermDebugView  = "debug.view"
    PermDebugBench = "debug.bench"
    PermDebugAdmin = "debug.admin" // 新的权限类型
)

// 在处理函数中使用
func (p *Plugin) handleSensitiveDebug(ctx *eventctx.Context) error {
    if !p.checkPermission(ctx, PermDebugAdmin) {
        return p.reply(ctx, "❌ 需要管理员权限")
    }
    // ...
}
```

## 版本历史

### v1.0.0 (2026-02-10)

- ✨ 初始版本
- ✅ 事件调试功能
- ✅ 上下文检查功能
- ✅ 命令调试功能
- ✅ 运行时信息查看
- ✅ 性能测试功能
- ✅ 系统统计功能
- ✅ 权限控制支持

## 许可证

本插件遵循 Remilia 框架的许可证。

## 贡献

欢迎提交 Issue 和 Pull Request！

## 相关资源

- [Remilia 框架文档](../../docs/README.md)
- [插件开发指南](../../docs/04-development/plugin-development.md)
- [其他开发工具插件](../dev/)

