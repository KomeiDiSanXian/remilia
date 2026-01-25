# Command 包集成示例

本示例演示如何使用 `command.Definition` 统一命令定义和元数据管理。

## 核心特性

### 🎯 统一定义

**Before** (分开定义):
```go
// 需要维护两套
def := &command.Definition{...}
metadata := &engine.MatcherMetadata{...}
```

**After** (统一定义):
```go
// 只需一套定义
def := &command.Definition{
    Name:        "search",
    Description: "搜索内容",
    Category:    "实用工具",  // ✨ 新增
    Examples:    []string{...}, // ✨ 新增
    Arguments:   [...],
    Flags:       [...],
}
m := eng.RegisterCommandDef(dto.GroupAtMessageCreate, def)
```

### ⚡ 自动转换

```
command.Definition
       ↓
   自动转换
       ↓
engine.MatcherMetadata
       ↓
   Help Plugin 使用
```

### 🔧 参数解析

```go
def := &command.Definition{
    Arguments: []*command.Argument{
        {Name: "keyword", Type: command.ArgTypeString, Required: true},
    },
    Flags: []*command.Flag{
        {Name: "count", Type: command.ArgTypeInt, Default: 5},
    },
}

// Handler 中自动解析
func handleSearch(ctx *context.Context) error {
    parsed := ctx.GetParsedCommand()
    keyword := parsed.GetString("keyword")  // 类型安全
    count := parsed.GetInt("count")         // 自动转换
    // ...
}
```

## 运行示例

```bash
cd examples/command-integration
go run main.go
```

## 输出示例

```
===== Command 包集成示例 =====

✅ 注册简单命令: /ping (原有方式)
✅ 注册命令: /status (使用 command.Definition)
✅ 注册复杂命令: /search (完整参数和标志)
✅ 注册命令: /echo (Handler 在 Definition 中)

===== 命令发现演示 =====

1. 所有命令:
  - /ping: 测试连接
  - /status: 查看Bot状态
    别名: [/stat /info]
  - /search: 搜索网络内容
    别名: [/find /query]
  - /echo: 回显消息
    别名: [/repeat]

2. 按分类分组:
  [系统]
    - /ping: 测试连接
    - /status: 查看Bot状态
  [实用工具]
    - /search: 搜索网络内容
    - /echo: 回显消息

3. 查找命令 '/search':
  命令: /search
  描述: 搜索网络内容
  用法: /search <关键词> [--engine google|bing] [--count 5]
  分类: 实用工具
  别名: [/find /query]
  参数:
    - keyword (string) (必需): 搜索关键词
  选项:
    - --engine, -e (默认: google): 搜索引擎
    - --count, -n (默认: 5): 结果数量
  示例:
    /search Go语言
    /search Python --engine bing
    /search 机器学习 --count 10
  所需权限: [use_search]

4. 查找别名 '/find':
  找到命令: /search (通过别名 /find)

5. Definition → MatcherMetadata 自动转换:
  ✅ Definition.Arguments → MatcherMetadata.Arguments
  ✅ Definition.Flags → MatcherMetadata.Flags
  ✅ Definition.Category → MatcherMetadata.Category
  ✅ Definition.Examples → MatcherMetadata.Examples
  ✅ Definition.Permissions → MatcherMetadata.Permissions
  ✅ 一次定义，自动生成 Help 和解析逻辑
```

## 使用方式

### 1. 简单命令（原有方式 - 仍然支持）

```go
m := eng.OnCommand(dto.GroupAtMessageCreate, "/ping")
m.SetDescription("测试连接")
m.SetCategory("系统")
m.Handle(handlePing)
```

**适用场景**: 
- 无参数的简单命令
- 快速原型
- 已有代码

### 2. 基础 Definition

```go
def := &command.Definition{
    Name:        "status",
    Aliases:     []string{"stat", "info"},
    Description: "查看状态",
    Category:    "系统",
}
m := eng.RegisterCommandDef(dto.GroupAtMessageCreate, def)
m.Handle(handleStatus)
```

**适用场景**:
- 有别名的命令
- 需要分类管理
- 需要在 Help 中显示

### 3. 完整 Definition（推荐）

```go
def := &command.Definition{
    Name:        "search",
    Aliases:     []string{"find"},
    Description: "搜索内容",
    Usage:       "/search <keyword> [--count 5]",
    Category:    "实用工具",
    Examples:    []string{"/search golang"},
    Permissions: []string{"use_search"},
    Arguments: []*command.Argument{
        {
            Name:        "keyword",
            Description: "搜索关键词",
            Required:    true,
            Type:        command.ArgTypeString,
        },
    },
    Flags: []*command.Flag{
        {
            Name:        "count",
            ShortName:   "n",
            Description: "结果数量",
            Type:        command.ArgTypeInt,
            Default:     5,
        },
    },
}
m := eng.RegisterCommandDef(dto.GroupAtMessageCreate, def)
m.Handle(handleSearch)
```

**适用场景**:
- 有参数和标志的命令
- 需要参数验证
- 需要类型转换
- 需要完整文档

### 4. Handler 在 Definition 中

```go
def := &command.Definition{
    Name:        "echo",
    Description: "回显消息",
    Arguments: []*command.Argument{
        {Name: "message", Type: command.ArgTypeString, Required: true},
    },
    Handler: func(ctx any) {
        eventCtx := ctx.(*context.Context)
        parsed := eventCtx.GetParsedCommand()
        message := parsed.GetString("message")
        // 处理逻辑
    },
}
eng.RegisterCommandDef(dto.GroupAtMessageCreate, def)
// 无需手动设置 Handler
```

**适用场景**:
- 命令逻辑简单
- 想要自包含的 Definition
- 便于测试和复用

## 参数类型

command 包支持以下参数类型：

```go
command.ArgTypeString       // string
command.ArgTypeInt          // int
command.ArgTypeBool         // bool
command.ArgTypeFloat        // float64
command.ArgTypeStringSlice  // []string
```

## 参数获取

在 Handler 中获取解析后的参数：

```go
func handleCommand(ctx *context.Context) error {
    parsed := ctx.GetParsedCommand()
    if parsed == nil {
        return fmt.Errorf("command not parsed")
    }
    
    // 字符串参数
    keyword := parsed.GetString("keyword")
    
    // 整数参数
    count := parsed.GetInt("count")
    
    // 布尔参数
    verbose := parsed.GetBool("verbose")
    
    // 浮点数参数
    threshold := parsed.GetFloat("threshold")
    
    // 字符串切片参数
    tags := parsed.GetStringSlice("tags")
    
    // 检查参数是否存在
    if !parsed.Has("optional_param") {
        // 使用默认值
    }
    
    return nil
}
```

## 优势对比

### 传统方式

```go
// ❌ 需要手动解析
m := eng.OnCommand(dto.GroupAtMessageCreate, "/search")
m.SetDescription("搜索")
m.SetUsage("/search <keyword>")
m.Handle(func(ctx *context.Context) error {
    content := ctx.GetMessageContent()
    // 手动分割、解析、验证
    parts := strings.Split(content, " ")
    if len(parts) < 2 {
        return fmt.Errorf("missing keyword")
    }
    keyword := parts[1]
    // ...
})
```

### command.Definition 方式

```go
// ✅ 自动解析、验证、类型转换
def := &command.Definition{
    Name:        "search",
    Description: "搜索",
    Usage:       "/search <keyword>",
    Arguments: []*command.Argument{
        {Name: "keyword", Required: true, Type: command.ArgTypeString},
    },
}
m := eng.RegisterCommandDef(dto.GroupAtMessageCreate, def)
m.Handle(func(ctx *context.Context) error {
    parsed := ctx.GetParsedCommand()
    keyword := parsed.GetString("keyword")  // 已验证、已解析
    // ...
})
```

## 高级特性

### 参数验证

```go
Arguments: []*command.Argument{
    {
        Name:     "age",
        Type:     command.ArgTypeInt,
        Required: true,
        Validator: func(s string) error {
            age, _ := strconv.Atoi(s)
            if age < 0 || age > 120 {
                return fmt.Errorf("age must be between 0 and 120")
            }
            return nil
        },
    },
}
```

### 默认值

```go
Flags: []*command.Flag{
    {
        Name:    "count",
        Type:    command.ArgTypeInt,
        Default: 10,  // 未提供时使用默认值
    },
}
```

### 隐藏命令

```go
def := &command.Definition{
    Name:        "debug",
    Description: "调试命令",
    Hidden:      true,  // 不在 Help 中显示
}
```

### 自定义前缀

```go
// 使用 "!" 作为命令前缀
m := eng.RegisterCommandDefWithPrefix(
    dto.GroupAtMessageCreate,
    "!",
    def,
)
// 触发: !search golang
```

## 迁移指南

### 从手动元数据迁移

**Before**:
```go
m := eng.OnCommand(dto.GroupAtMessageCreate, "/search")
m.SetDescription("搜索内容")
m.SetUsage("/search <keyword>")
m.SetCategory("实用工具")
m.SetMetadata(&engine.MatcherMetadata{
    Arguments: []*engine.ArgumentMeta{...},
    Flags: []*engine.FlagMeta{...},
})
m.Handle(handleSearch)
```

**After**:
```go
def := &command.Definition{
    Name:        "search",
    Description: "搜索内容",
    Usage:       "/search <keyword>",
    Category:    "实用工具",
    Arguments:   []*command.Argument{...},
    Flags:       []*command.Flag{...},
}
m := eng.RegisterCommandDef(dto.GroupAtMessageCreate, def)
m.Handle(handleSearch)
```

### 渐进式迁移

1. **第一步**: 使用 `RegisterCommandDef()` 注册新命令
2. **第二步**: 保持旧命令不变（向后兼容）
3. **第三步**: 逐步迁移旧命令到 Definition
4. **完成**: 所有命令统一使用 Definition

## 相关文档

- [Command 包集成方案](../../docs/COMMAND_INTEGRATION_PLAN.md)
- [Help Plugin 设计](../../docs/HELP_PLUGIN_DESIGN.md)
- [Command 系统文档](../../command/README.md)

## 最佳实践

1. ✅ **新命令使用 Definition** - 获得完整特性
2. ✅ **参数命令使用 Definition** - 自动解析和验证
3. ✅ **简单命令可用原有方式** - 保持简洁
4. ✅ **设置 Category** - 便于 Help 分组
5. ✅ **提供 Examples** - 帮助用户理解
6. ✅ **设置 Permissions** - 权限管理
7. ✅ **使用类型安全的 Get 方法** - 避免类型错误

## 常见问题

### Q: 是否必须迁移到 Definition？

A: 不必须。原有的链式调用方式仍然支持。Definition 是推荐方式，尤其是对于有参数的命令。

### Q: Definition.Handler 和 Matcher.Handle() 有什么区别？

A: 
- `Definition.Handler`: 接收 `any` 类型，需要类型断言
- `Matcher.Handle()`: 接收 `context.Handler` 类型，类型安全

推荐使用 `Matcher.Handle()`，更加类型安全。

### Q: 如何混用两种方式？

A: 完全兼容，可以混用：
```go
m := eng.RegisterCommandDef(dto.GroupAtMessageCreate, def)
m.SetExamples("额外示例")  // 仍可使用链式调用
m.SetPriority(100)         // 设置优先级
```

### Q: 转换有性能开销吗？

A: 转换只在命令注册时执行一次，运行时无开销。

### Q: 支持子命令吗？

A: `command.Definition` 支持子命令，但目前 Help Plugin 只显示顶层命令。未来会增强。
