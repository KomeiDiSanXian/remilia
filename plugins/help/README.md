# 帮助插件 (Help Plugin)

一个内置的帮助插件，用于自动生成和显示所有可用命令的帮助信息。

## 功能特性

### ✅ 多种查看模式

1. **查看所有命令（分页）** - `/help` 或 `/help <页码>`
   - 按分类组织显示所有命令
   - 支持分页浏览（每页10个命令）
   - 显示命令统计信息

2. **查看插件命令** - `/help <插件名>`
   - 显示指定插件的所有命令
   - 包括命令描述和用法

3. **查看命令详情** - `/help <命令名>`
   - 显示命令的详细信息
   - 包括别名、用法、参数、选项、示例等

### ✅ 智能功能

- **自动命令发现**：从 CommandRegistry 自动获取所有已注册的命令
- **智能建议**：当命令不存在时，提供相似命令建议
- **多事件支持**：自动适配群聊和私聊消息
- **统计信息**：显示命令调用次数、命令总数等

## 快速开始

### 1. 创建命令注册表

```go
registry := command.NewCommandRegistry()
```

### 2. 注册业务命令

```go
weatherDef := &command.Definition{
    Name:        "weather",
    Aliases:     []string{"w", "天气"},
    Description: "查询指定城市的天气信息",
    Usage:       "/weather <城市> [--unit celsius|fahrenheit]",
    Category:    "实用工具",  // 用于分类显示
    Examples: []string{
        "/weather 北京",
        "/weather 上海 --unit fahrenheit",
    },
    Handler: func(ctx any) {
        // 处理逻辑
    },
}

registry.RegisterWithOptions(weatherDef, command.RegisterOptions{
    Category: "实用工具",
    Priority: 10,
})
```

### 3. 创建并注册帮助插件

```go
// 创建引擎和插件管理器
eng := engine.NewEngine()
manager := plugin.NewManager(eng)

// 创建帮助插件
helpPlugin := plugins.NewHelpPlugin(registry)

// 设置插件管理器（可选，用于支持插件级别的帮助）
helpPlugin.SetPluginManager(manager)

// 注册插件
if err := manager.Register(helpPlugin); err != nil {
    logrus.WithError(err).Fatal("Failed to register help plugin")
}
```

## 使用方式

### 查看所有命令（第1页）

```
/help
```

输出示例：
```
📖 可用命令列表 (第 1/3 页)
==============================

【实用工具】
  /weather (/w, /天气)
    查询指定城市的天气信息
  /calc (/计算, /c)
    执行简单的数学计算
  ...

【查询】
  /search (/s, /搜索)
    在指定来源搜索内容
  ...

==============================
💡 使用方法:
  /help <命令名> - 查看命令详情
  /help <插件名> - 查看插件的所有命令
  /help <页码> - 查看其他页 (共 3 页)

📊 统计: 共 25 个命令，40 个别名
```

### 查看指定页

```
/help 2
```

### 查看插件的所有命令

```
/help 实用工具
```

或使用插件名：
```
/help utility
```

输出示例：
```
🔌 插件【实用工具】的命令
==============================

/weather (/w, /天气)
  查询指定城市的天气信息
  用法: /weather <城市> [--unit celsius|fahrenheit]

/calc (/计算, /c)
  执行简单的数学计算
  用法: /calc <表达式>

==============================
💡 使用 /help <命令名> 查看命令的详细用法
```

### 查看特定命令详情

```
/help weather
```

或带 `/` 前缀：
```
/help /weather
```

输出示例：
```
📝 命令详情
==============================

命令: /weather
别名: /w, /天气
分类: 实用工具

描述:
  查询指定城市的天气信息

用法:
  /weather <城市> [--unit celsius|fahrenheit] [--days 3]

示例:
  /weather 北京
  /weather 上海 --unit fahrenheit
  /weather 广州 --days 7

📊 该命令已被调用 42 次
```

### 命令不存在时的智能建议

```
/help wea
```

输出示例：
```
❌ 未找到: wea

💡 你可能想要:
  /weather
  /w
  /天气

📦 可用插件:
  实用工具
  查询
  管理

使用 /help <插件名> 查看插件命令
```

## API 参考

### 创建帮助插件

```go
func NewHelpPlugin(registry *command.CommandRegistry) *HelpPlugin
```

**参数**：
- `registry`: 命令注册表实例

**返回**：
- `*HelpPlugin`: 帮助插件实例

### 设置插件管理器

```go
func (p *HelpPlugin) SetPluginManager(pm *plugin.Manager)
```

**参数**：
- `pm`: 插件管理器实例

**说明**：
设置插件管理器后，帮助命令可以显示插件级别的帮助信息。

## 命令定义最佳实践

为了让帮助插件生成更好的帮助信息，建议提供完整的命令元数据：

```go
&command.Definition{
    // === 基本信息（必需） ===
    Name:        "command",           // 命令名称（不带 / 前缀）
    
    // === 推荐提供 ===
    Aliases:     []string{"alias"},   // 命令别名
    Description: "命令描述",            // 简短描述（一句话）
    Usage:       "/command <参数>",    // 用法说明
    Category:    "分类名称",            // 命令分类
    
    // === 可选但很有用 ===
    Examples: []string{                // 使用示例
        "/command example1",
        "/command example2 --flag value",
    },
    Permissions: []string{"admin"},    // 所需权限
    Hidden:      false,                // 是否在帮助中隐藏
    
    // === 增强命令系统特性 ===
    Arguments: []*command.Argument{    // 位置参数定义
        {
            Name:        "city",
            Description: "城市名称",
            Type:        command.ArgTypeString,
            Required:    true,
        },
    },
    Flags: []*command.Flag{           // 选项参数定义
        {
            Name:        "unit",
            ShortName:   "u",
            Description: "温度单位",
            Type:        command.ArgTypeString,
            Default:     "celsius",
        },
    },
    
    // === 处理器（必需） ===
    Handler: func(ctx any) {
        // 命令处理逻辑
    },
}
```

## 分页配置

默认每页显示 10 个命令。如果需要修改，可以在 `plugins/help.go` 中修改常量：

```go
const (
    commandsPerPage = 10  // 修改为你想要的数值
)
```

## 集成到现有项目

如果你已经有一个使用 CommandRegistry 的项目，集成帮助插件非常简单：

```go
// 在现有代码中添加
helpPlugin := plugins.NewHelpPlugin(registry)

// 如果有插件管理器
helpPlugin.SetPluginManager(manager)

// 注册插件
if err := manager.Register(helpPlugin); err != nil {
    logrus.WithError(err).Error("Failed to register help plugin")
}
```

## 完整示例

查看 `examples/help-plugin/main.go` 获取完整的可运行示例。

运行示例：

```bash
cd examples/help-plugin
go run main.go
```

## 故障排除

### 帮助插件不响应

**检查**：
1. 插件是否成功注册
2. Engine 是否正确启动
3. 日志中是否有错误信息

### 命令列表为空

**检查**：
1. 业务命令是否已注册到 CommandRegistry
2. CommandRegistry 实例是否正确传递给帮助插件
3. 命令名称是否不带 `/` 前缀（注册时）

### 插件级别帮助不工作

**检查**：
1. 是否调用了 `SetPluginManager()`
2. 命令的 `Category` 字段是否正确设置
3. 插件名称是否与 Category 匹配

### 消息发送失败

**检查**：
1. Bot 的 OpenAPI 配置是否正确
2. 事件类型是否受支持（GroupAtMessageCreate 或 C2CMessageCreate）
3. 权限是否足够

## 注意事项

1. **命令注册顺序**：确保在注册帮助插件之前，所有业务命令都已经注册到 CommandRegistry
2. **事件类型支持**：目前支持群聊和私聊两种事件类型
3. **命令冲突**：`/help` 命令由帮助插件注册，不要在业务代码中重复注册
4. **命令名称**：注册命令时使用不带 `/` 前缀的名称（如 `"weather"`），显示时会自动添加前缀

## 贡献

欢迎提交 Issue 和 Pull Request 来改进帮助插件！

## 许可证

本插件遵循项目主许可证。
