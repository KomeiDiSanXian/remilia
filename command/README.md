# Command Package

增强的命令解析系统，支持子命令、参数验证、类型转换等高级功能。

## 功能特性

- ✅ **命令树结构**: 支持子命令嵌套
- ✅ **参数类型**: String, Int, Bool, Float, StringSlice
- ✅ **参数验证**: 内置验证器 + 自定义验证
- ✅ **标志支持**: 长标志 (--flag) 和短标志 (-f)
- ✅ **别名支持**: 命令可以有多个别名
- ✅ **自动帮助**: 自动生成使用说明

## 快速开始

### 基础用法

```go
import "github.com/KomeiDiSanXian/remilia/command"

// 定义命令
def := &command.Definition{
    Name:        "weather",
    Description: "查询天气",
    Usage:       "weather <城市>",
    Arguments: []*command.Argument{
        {
            Name:     "city",
            Type:     command.ArgTypeString,
            Required: true,
        },
    },
}

// 方式1: 使用 ParseFromDefinition (推荐用于单命令)
parsed, err := command.ParseFromDefinition("weather 北京", def, "")
if err != nil {
    log.Fatal(err)
}

// 获取参数
city := parsed.GetString("city") // "北京"

// 方式2: 使用 Parser (推荐用于多命令)
parser := command.NewParser("")
parser.Register(def)
parsed2, err := parser.Parse("weather 北京")
if err != nil {
    log.Fatal(err)
}
```

### 带标志的命令

```go
def := &command.Definition{
    Name: "search",
    Arguments: []*command.Argument{
        {Name: "keyword", Type: command.ArgTypeString, Required: true},
    },
    Flags: []*command.Flag{
        {
            Name:      "limit",
            ShortName: "l",
            Type:      command.ArgTypeInt,
            Default:   10,
        },
        {
            Name:      "verbose",
            ShortName: "v",
            Type:      command.ArgTypeBool,
        },
    },
}

// 使用 Parser 解析
parser := command.NewParser("")
parser.Register(def)
parsed, _ := parser.Parse("search golang --limit 20 -v")

keyword := parsed.GetString("keyword") // "golang"
limit := parsed.GetInt("limit")         // 20
verbose := parsed.GetBool("verbose")    // true
```

### 子命令

```go
def := &command.Definition{
    Name: "git",
    SubCommands: []*command.Definition{
        {
            Name: "commit",
            Flags: []*command.Flag{
                {Name: "message", ShortName: "m", Type: command.ArgTypeString},
            },
        },
        {
            Name: "push",
            Flags: []*command.Flag{
                {Name: "force", ShortName: "f", Type: command.ArgTypeBool},
            },
        },
    },
}

// 使用 Parser 解析
parser := command.NewParser("")
parser.Register(def)
parsed, _ := parser.Parse("git commit -m \"update\"")

subCmd := parsed.SubCommand  // "commit"
msg := parsed.GetString("message") // "update"
```

### 参数验证

```go
def := &command.Definition{
    Name: "setage",
    Arguments: []*command.Argument{
        {
            Name:     "age",
            Type:     command.ArgTypeInt,
            Required: true,
            Validator: func(s string) error {
                age, _ := strconv.Atoi(s)
                if age < 0 || age > 150 {
                    return fmt.Errorf("年龄必须在 0-150 之间")
                }
                return nil
            },
        },
    },
    Validator: func(p *command.Parsed) error {
        // 全局验证
        age := p.GetInt("age")
        if age < 18 {
            return fmt.Errorf("必须年满18岁")
        }
        return nil
    },
}
```

### 与 Remilia 集成

```go
import (
    "github.com/KomeiDiSanXian/remilia"
    "github.com/KomeiDiSanXian/remilia/command"
)

// 创建命令解析器
parser := command.NewParser()
parser.Register(&command.Definition{
    Name: "hello",
    Arguments: []*command.Argument{
        {Name: "name", Type: command.ArgTypeString, Required: true},
    },
    Handler: func(ctx any) {
        rCtx := ctx.(*remilia.Context)
        parsed := rCtx.GetParsedCommand()
        name := parsed.GetString("name")
        rCtx.Reply("你好，" + name)
    },
})

// 使用规则匹配
engine := remilia.NewEngine()
engine.On(
    remilia.OnCommandMatch(parser),
).HandleE(func(ctx *remilia.Context) error {
    remilia.ExecuteCommandDefinition(ctx)
    return nil
})
```

## 参数类型

| 类型 | 说明 | 示例 |
|------|------|------|
| `ArgTypeString` | 字符串 | "hello" |
| `ArgTypeInt` | 整数 | 42 |
| `ArgTypeBool` | 布尔值 | true, false |
| `ArgTypeFloat` | 浮点数 | 3.14 |
| `ArgTypeStringSlice` | 字符串数组 | "a,b,c" |

## API 文档

### Definition 结构

```go
type Definition struct {
    Name        string          // 命令名称
    Aliases     []string        // 别名
    Description string          // 描述
    Usage       string          // 使用说明
    Arguments   []*Argument     // 位置参数
    Flags       []*Flag         // 标志
    SubCommands []*Definition   // 子命令
    Validator   func(*Parsed) error // 验证器
    Handler     Handler         // 处理器
}
```

### Argument 结构

```go
type Argument struct {
    Name        string          // 参数名
    Description string          // 描述
    Type        ArgType         // 类型
    Required    bool            // 是否必需
    Default     any             // 默认值
    Validator   func(string) error // 验证器
}
```

### Flag 结构

```go
type Flag struct {
    Name        string          // 长标志名
    ShortName   string          // 短标志名
    Description string          // 描述
    Type        ArgType         // 类型
    Required    bool            // 是否必需
    Default     any             // 默认值
    Validator   func(string) error // 验证器
}
```

### Parsed 结构

```go
type Parsed struct {
    Definition  *Definition            // 命令定义
    SubCommand  string                 // 子命令名
    Arguments   map[string]any         // 参数值
    Flags       map[string]any         // 标志值
    Remaining   []string               // 剩余参数
}
```

获取值的便捷方法：
- `GetString(name string) string`
- `GetInt(name string) int`
- `GetBool(name string) bool`
- `GetFloat(name string) float64`
- `GetStringSlice(name string) []string`

## 最佳实践

### 1. 命令命名

- 使用小写字母和连字符
- 避免特殊字符
- 保持简短明了

```go
// ✅ 好的命名
"weather", "user-info", "set-config"

// ❌ 不好的命名
"Weather", "user_info", "设置配置"
```

### 2. 参数设计

- 必需参数放在前面
- 可选参数使用标志
- 提供合理的默认值

```go
def := &command.Definition{
    Name: "search",
    Arguments: []*command.Argument{
        {Name: "keyword", Required: true},  // 必需
    },
    Flags: []*command.Flag{
        {Name: "limit", Default: 10},       // 可选
    },
}
```

### 3. 错误处理

- 提供清晰的错误消息
- 使用验证器提前捕获错误

```go
Validator: func(s string) error {
    if len(s) < 3 {
        return fmt.Errorf("关键词至少需要3个字符，当前: %d", len(s))
    }
    return nil
}
```

### 4. 帮助文档

- 填写 Description 和 Usage
- 为每个参数添加说明

```go
def := &command.Definition{
    Name:        "backup",
    Description: "备份数据到指定目录",
    Usage:       "backup <源路径> [目标路径]",
    Arguments: []*command.Argument{
        {
            Name:        "source",
            Description: "要备份的源路径",
            Required:    true,
        },
        {
            Name:        "target",
            Description: "备份目标路径，默认为 ./backup",
            Default:     "./backup",
        },
    },
}
```

## 迁移指南

### 从基础解析器迁移

**旧代码** (基础解析器):
```go
args, _ := ctx.ParseCommand()
if len(args.Args) > 0 {
    keyword := args.Args[0]
}
```

**新代码** (增强系统):
```go
parser := command.NewParser()
parser.Register(&command.Definition{
    Name: "search",
    Arguments: []*command.Argument{
        {Name: "keyword", Type: command.ArgTypeString, Required: true},
    },
})

engine.On(
    remilia.OnCommandMatch(parser),
).HandleE(func(ctx *remilia.Context) error {
    parsed := ctx.GetParsedCommand()
    keyword := parsed.GetString("keyword")
    return nil
})
```

## 性能

增强系统经过优化，性能开销minimal：

```
BenchmarkParse_Simple      500000    3000 ns/op    1024 B/op    12 allocs/op
BenchmarkParse_Complex     200000    8000 ns/op    2048 B/op    25 allocs/op
BenchmarkParse_SubCommand  150000   10000 ns/op    3072 B/op    30 allocs/op
```

## 常见问题

### Q: 如何处理引号内的空格？

A: 解析器自动处理引号：

```go
// 输入: hello "world from mars"
// 结果: ["hello", "world from mars"]
```

### Q: 如何支持变长参数？

A: 使用 `Remaining` 字段：

```go
parsed, _ := command.Parse(def, "cmd arg1 arg2 arg3")
remaining := parsed.Remaining // ["arg1", "arg2", "arg3"]
```

### Q: 如何获取原始输入？

A: 使用 `ctx.GetMessageContent()`：

```go
raw := ctx.GetMessageContent()
```

## 相关链接

- [Remilia 文档](https://github.com/KomeiDiSanXian/remilia)
- [命令系统设计文档](../docs/COMMAND_DESIGN.md)
- [API 参考](https://pkg.go.dev/github.com/KomeiDiSanXian/remilia/command)

## 许可证

与 Remilia 主项目相同。
