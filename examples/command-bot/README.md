# Command Bot Example

这个示例展示了如何使用 Remilia 的命令系统，包括命令注册表、参数解析和命令补全等高级特性。

## 功能

- ✅ 命令注册表 - 高性能命令查找
- ✅ 命令别名 - 支持多个别名
- ✅ 参数解析 - 支持位置参数和标志
- ✅ 命令补全 - 智能提示相似命令
- ✅ 帮助系统 - 自动生成帮助信息

## 命令列表

### /weather (别名: /w, /天气)
查询天气信息

```
/weather Beijing
/weather Beijing --unit fahrenheit
/weather Beijing --days 3
```

**参数**:
- `city` - 城市名称（必需）
- `--unit` - 温度单位（可选，默认 celsius）
- `--days` - 天数（可选，默认 1）

### /calc (别名: /计算)
简单计算器

```
/calc 1 + 2 * 3
/calc (10 + 5) / 3
```

### /search (别名: /s, /搜索)
搜索内容

```
/search golang
/search golang --source bing
/search golang --limit 20
```

**参数**:
- `keyword` - 搜索关键词（必需）
- `--source` - 搜索引擎（可选，默认 google）
- `--limit` - 结果数量（可选，默认 10）

### /user (别名: /u)
查看用户信息

```
/user
/user 123456
```

### /help (别名: /h, /帮助)
显示帮助信息

```
/help
/help weather
```

## 运行

```bash
# 设置环境变量
export BOT_SECRET="your-webhook-secret"
export BOT_PORT="8080"

# 运行
go run -tags example main.go
```

## 代码说明

### 创建命令注册表

```go
registry := command.NewCommandRegistry()
```

命令注册表提供了高性能的命令查找和管理。

### 注册命令

```go
def := &command.Definition{
    Name:        "/weather",
    Aliases:     []string{"/w", "/天气"},
    Description: "查询天气",
    Usage:       "/weather <城市>",
    Handler:     weatherHandler,
}

registry.RegisterWithOptions(def, command.RegisterOptions{
    Category: "utility",
    Priority: 10,
})
```

**支持的选项**:
- `Category` - 命令分类
- `Priority` - 优先级（数字越大越优先）
- `Pattern` - 正则匹配模式

### 解析命令参数

```go
args, err := command.ParseCommandLine(text)
if err != nil {
    return err
}

// 位置参数
city := args.Get(0)
keyword := strings.Join(args.Positional, " ")

// 标志参数
unit := args.GetFlagOrDefault("unit", "celsius")
days := args.GetFlagIntOrDefault("days", 1)
verbose := args.GetFlagBool("verbose")
```

### 命令查找

```go
// 精确查找
meta, found := registry.Lookup("/weather")

// 别名查找
meta, found := registry.Lookup("/w")

// 命令补全
suggestions := registry.Complete("/we")
// 返回: ["/weather"]
```

### 命令统计

```go
stats := registry.GetStats()
log.Printf("Commands: %d", stats.CommandCount)
log.Printf("Aliases: %d", stats.AliasCount)
log.Printf("Lookups: %d", stats.LookupCount)
log.Printf("Hit Rate: %.2f%%", stats.HitRate*100)
```

## 高级用法

### 1. 正则模式匹配

```go
def := &command.Definition{
    Name: "/user",
}

registry.RegisterWithOptions(def, command.RegisterOptions{
    Pattern: `^/user-\d+`, // 匹配 /user-123, /user-456
})

// 查找匹配的命令
matches := registry.LookupByPattern("/user-789")
```

### 2. 命令分类

```go
// 按分类注册
registry.RegisterWithOptions(weatherDef, command.RegisterOptions{
    Category: "utility",
})

registry.RegisterWithOptions(adminDef, command.RegisterOptions{
    Category: "admin",
})

// 按分类列出
utilityCommands := registry.ListByCategory("utility")
adminCommands := registry.ListByCategory("admin")
```

### 3. 子命令

```go
// 主命令
def := &command.Definition{
    Name: "/config",
    SubCommands: []*command.Definition{
        {
            Name: "get",
            Handler: configGetHandler,
        },
        {
            Name: "set",
            Handler: configSetHandler,
        },
    },
}
```

### 4. 参数验证

```go
def := &command.Definition{
    Name: "/age",
    Arguments: []*command.Argument{
        {
            Name:     "age",
            Type:     command.ArgTypeInt,
            Required: true,
            Validator: func(s string) error {
                age, _ := strconv.Atoi(s)
                if age < 0 || age > 150 {
                    return errors.New("age must be between 0 and 150")
                }
                return nil
            },
        },
    },
}
```

## 性能

命令注册表经过优化，提供了出色的性能：

| 操作 | 延迟 | 吞吐量 |
|------|------|--------|
| 命令查找 | ~13ns | 86M ops/s |
| 命令提取 | ~149ns | 8M ops/s |

## 下一步

- 查看 [plugin-example](../plugin-example) 了解插件开发
- 查看 [middleware-example](../middleware-example) 了解中间件使用
- 阅读 [命令索引优化文档](../../docs/COMMAND_INDEX_OPTIMIZATION.md) 了解更多细节
