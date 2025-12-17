# 增强命令解析器 - 快速开始

## 5 分钟上手

### 1. 最简单的命令

```go
parser := remilia.NewCommandParser("/")

parser.Register(&remilia.CommandDefinition{
    Name:        "ping",
    Description: "测试连接",
    Handler: func(ctx *remilia.Context) {
        ctx.Reply("Pong!")
    },
})

// 使用: /ping
```

### 2. 带参数的命令

```go
parser.Register(&remilia.CommandDefinition{
    Name: "echo",
    Arguments: []*remilia.Argument{
        {Name: "message", Type: remilia.ArgTypeString, Required: true},
    },
    Handler: func(ctx *remilia.Context) {
        parsed, _ := parser.Parse(ctx.GetMessageContent())
        ctx.Reply(parsed.GetString("message"))
    },
})

// 使用: /echo Hello World
```

### 3. 带选项的命令

```go
parser.Register(&remilia.CommandDefinition{
    Name: "search",
    Arguments: []*remilia.Argument{
        {Name: "query", Type: remilia.ArgTypeString, Required: true},
    },
    Flags: []*remilia.Flag{
        {Name: "limit", ShortName: "l", Type: remilia.ArgTypeInt, Default: 10},
    },
    Handler: func(ctx *remilia.Context) {
        parsed, _ := parser.Parse(ctx.GetMessageContent())
        query := parsed.GetString("query")
        limit := parsed.GetInt("limit")
        
        ctx.Reply(fmt.Sprintf("搜索 '%s'，限制 %d 条结果", query, limit))
    },
})

// 使用: /search golang
// 使用: /search golang --limit 20
// 使用: /search golang -l 5
```

### 4. 子命令

```go
parser.Register(&remilia.CommandDefinition{
    Name: "admin",
    SubCommands: []*remilia.CommandDefinition{
        {
            Name: "user",
            SubCommands: []*remilia.CommandDefinition{
                {
                    Name: "list",
                    Handler: func(ctx *remilia.Context) {
                        ctx.Reply("用户列表...")
                    },
                },
                {
                    Name: "add",
                    Arguments: []*remilia.Argument{
                        {Name: "username", Type: remilia.ArgTypeString, Required: true},
                    },
                    Handler: func(ctx *remilia.Context) {
                        parsed, _ := parser.Parse(ctx.GetMessageContent())
                        ctx.Reply("已添加用户: " + parsed.GetString("username"))
                    },
                },
            },
        },
    },
})

// 使用: /admin user list
// 使用: /admin user add john
```

### 5. 在 Bot 中集成

```go
bot := remilia.New(info)

// 方法 1: 全局处理器
bot.OnCommand("/").Handle(func(ctx *remilia.Context) {
    parsed, err := parser.Parse(ctx.GetMessageContent())
    if err != nil {
        ctx.Reply("❌ " + err.Error() + "\n使用 /help 查看帮助")
        return
    }
    
    if parsed.Definition.Handler != nil {
        parsed.Definition.Handler(ctx)
    }
})

// 方法 2: 单独注册每个命令
bot.OnCommand("/ping").Handle(func(ctx *remilia.Context) {
    ctx.Reply("Pong!")
})
```

---

## 常用模式

### 自动帮助命令

```go
parser.Register(&remilia.CommandDefinition{
    Name:        "help",
    Aliases:     []string{"h", "?"},
    Description: "显示帮助",
    Arguments: []*remilia.Argument{
        {Name: "command", Type: remilia.ArgTypeString, Required: false},
    },
    Handler: func(ctx *remilia.Context) {
        parsed, _ := parser.Parse(ctx.GetMessageContent())
        cmd := parsed.GetString("command")
        
        if cmd != "" {
            ctx.Reply(parser.GenerateHelp(cmd))
        } else {
            ctx.Reply(parser.GenerateHelp())
        }
    },
})
```

### 参数验证

```go
parser.Register(&remilia.CommandDefinition{
    Name: "setage",
    Arguments: []*remilia.Argument{
        {
            Name:     "age",
            Type:     remilia.ArgTypeInt,
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
})
```

### 必需和可选参数混合

```go
parser.Register(&remilia.CommandDefinition{
    Name: "weather",
    Arguments: []*remilia.Argument{
        {Name: "city", Type: remilia.ArgTypeString, Required: true},
        {Name: "days", Type: remilia.ArgTypeInt, Required: false, Default: 3},
    },
    Flags: []*remilia.Flag{
        {Name: "unit", Type: remilia.ArgTypeString, Default: "celsius"},
        {Name: "verbose", ShortName: "v", Type: remilia.ArgTypeBool, Default: false},
    },
})
```

---

## 类型对照表

| ArgType | Go 类型 | 示例值 | 获取方法 |
|---------|---------|--------|----------|
| `ArgTypeString` | `string` | `"hello"` | `parsed.GetString(name)` |
| `ArgTypeInt` | `int` | `42` | `parsed.GetInt(name)` |
| `ArgTypeBool` | `bool` | `true` | `parsed.GetBool(name)` |
| `ArgTypeFloat` | `float64` | `3.14` | `parsed.GetFloat(name)` |

---

## 错误处理

### 友好的错误提示

```go
bot.OnCommand("/").Handle(func(ctx *remilia.Context) {
    parsed, err := parser.Parse(ctx.GetMessageContent())
    if err != nil {
        // 友好的错误消息
        msg := "❌ 命令格式错误\n\n"
        msg += err.Error() + "\n\n"
        msg += "💡 使用 /help 查看正确的命令格式"
        ctx.Reply(msg)
        return
    }
    
    // 执行命令...
})
```

---

## 完整示例

```go
package main

import (
    "fmt"
    "github.com/KomeiDiSanXian/remilia"
    "github.com/KomeiDiSanXian/remilia/openapi/dto"
)

func main() {
    // 创建解析器
    parser := remilia.NewCommandParser("/")
    
    // 注册命令
    registerCommands(parser)
    
    // 创建 Bot
    info := &dto.BotInfo{
        AppID:     12345,
        Token:     "your-token",
        AppSecret: "your-secret",
    }
    
    bot := remilia.New(info)
    
    // 集成解析器
    bot.OnCommand("/").Handle(func(ctx *remilia.Context) {
        content := ctx.GetMessageContent()
        
        parsed, err := parser.Parse(content)
        if err != nil {
            ctx.Reply(fmt.Sprintf("❌ %s\n\n使用 /help 查看帮助", err.Error()))
            return
        }
        
        if parsed.Definition.Handler != nil {
            parsed.Definition.Handler(ctx)
        }
    })
    
    // 启动
    bot.Start()
}

func registerCommands(parser *remilia.CommandParser) {
    // Ping
    parser.Register(&remilia.CommandDefinition{
        Name:        "ping",
        Description: "测试连接",
        Handler: func(ctx *remilia.Context) {
            ctx.Reply("🏓 Pong!")
        },
    })
    
    // Echo
    parser.Register(&remilia.CommandDefinition{
        Name:        "echo",
        Description: "回显消息",
        Arguments: []*remilia.Argument{
            {Name: "message", Type: remilia.ArgTypeString, Required: true},
            {Name: "times", Type: remilia.ArgTypeInt, Required: false, Default: 1},
        },
        Handler: func(ctx *remilia.Context) {
            parsed, _ := parser.Parse(ctx.GetMessageContent())
            message := parsed.GetString("message")
            times := parsed.GetInt("times")
            
            for i := 0; i < times; i++ {
                ctx.Reply(message)
            }
        },
    })
    
    // Help
    parser.Register(&remilia.CommandDefinition{
        Name:        "help",
        Aliases:     []string{"h", "?"},
        Description: "显示帮助",
        Arguments: []*remilia.Argument{
            {Name: "command", Type: remilia.ArgTypeString, Required: false},
        },
        Handler: func(ctx *remilia.Context) {
            parsed, _ := parser.Parse(ctx.GetMessageContent())
            cmd := parsed.GetString("command")
            
            if cmd != "" {
                ctx.Reply(parser.GenerateHelp(cmd))
            } else {
                ctx.Reply(parser.GenerateHelp())
            }
        },
    })
}
```

---

## 下一步

- 📖 查看 [CommandParser增强完成报告.md](CommandParser增强完成报告.md) 了解更多功能
- 💻 查看 [example/command_enhanced_example.go](example/command_enhanced_example.go) 获取完整示例
- 🧪 运行测试 `go test -run TestCommandParser -v` 查看所有测试用例
- 📝 查看 [command_enhanced.go](command_enhanced.go) 了解 API 详情

---

**提示**: 原有的 `Context.ParseCommand()` 仍然可用，适合简单场景。新的增强解析器适合复杂命令系统。

