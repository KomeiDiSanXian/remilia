# 命令系统（command 包）

`command` 包提供命令的定义、注册与解析：声明式 `Definition`（含参数/标志/子命令）、
命令解析器（`Parser`/`Args`/`Parsed`）与命令注册表。

## 命令定义

### Definition 字段

```go
type Definition struct {
    Name        string      // 命令名（不含触发前缀）
    Description string      // 描述（/help 展示）
    Usage       string      // 用法说明
    Category    string      // 分类（/help 分组展示）
    Aliases     []string    // 别名
    Arguments   []*Argument // 位置参数
    Flags       []*Flag     // 标志参数
    SubCommands []*Definition // 子命令
    Examples    []string    // 示例
    Permissions []string    // 所需权限
    Hidden      bool        // 是否在帮助中隐藏
}
```

### 使用 Builder 构建（推荐）

```go
def := command.NewDef("search").
    Description("搜索内容").
    Usage("/search <关键词> [--page N]").
    Category("工具").
    Arg("keyword", "搜索关键词", true).      // 必需位置参数
    ArgWithType("count", "返回数量", false, command.ArgTypeInt).
    Flag("page", "p", "页码", command.ArgTypeInt).
    Flag("verbose", "v", "详细输出", command.ArgTypeBool).
    Example("/search remilia --page 2").
    Alias("s").
    Build()
```

参数类型：`ArgTypeString` / `ArgTypeInt` / `ArgTypeBool` / `ArgTypeFloat`（默认 String）。

### 注册命令

```go
// 方式 1：注册时附带 Definition（/help 可展示完整信息）
ctx.Reg.RegisterCommand(eventctx.EventGroup, "/search").
    SetDefinition(def).
    Handle(p.handleSearch)

// 方式 2：SetupContext 便捷方法（handler + 规则一次完成）
ctx.OnCommandDef(eventctx.EventGroup, "/search", def, eventctx.OnMentionedBotOrNoMentions())
ctx.OnCommandDefWith(eventctx.EventGroup, "/search", def, p.handleSearch)
```

## 解析命令内容

### Args — 命令行分词（简单场景）

```go
args, err := command.ParseCommandLine(ctx.GetMessageContent())
if err != nil { return err }

sub := args.Get(0)      // 第一个 token（如 "/debug" 后的子命令名）
val := args.Get(1)      // 第二个 token
args.Count()            // token 数量
```

适用于 `/debug <sub>` 这类"按位置取词"的分发（参考 debug 插件）。

### Parsed — 结构化解析（定义感知）

```go
parsed, err := command.ParseFromDefinition(ctx.GetMessageContent(), def, "/")
if err != nil { ... }

parsed.CommandPath   // []string：命令路径（含子命令）
parsed.Arguments     // map[string]any：位置参数（按定义名）
parsed.Flags         // map[string]any：标志值（含短标志展开）
```

### handler 中读取解析结果

```go
func (p *Plugin) handleSearch(ctx *eventctx.Context) error {
    parsed := ctx.GetParsedCommand()   // 引擎已自动解析（若命令带 Definition）
    if parsed == nil {
        // 未解析：手动解析
        parsed, _ = command.ParseFromDefinition(ctx.GetMessageContent(), searchDef, "/")
    }
    keyword, _ := parsed.GetString("keyword")
    page, _     := parsed.GetInt("page")
    verbose, _  := parsed.GetBool("verbose")
    // ...
}
```

`Parsed` 辅助方法：`GetString(name)` / `GetInt(name)` / `GetBool(name)` / `GetFloat(name)`
——按定义中的参数/标志名取值（不区分位置参数与标志，同名校后者覆盖）。

## 命令注册表

`command.NewCommandRegistry()` 提供命令索引（`Register` / `Find` / 统计），
供 `/help` 等需要全量命令信息的组件使用。引擎内部以 `commandIndex` 做 O(1) 命令路由，
插件通常不需要直接使用 Registry。

## 实用工具

| 函数 | 说明 |
|------|------|
| `ExtractCommandFast(content, prefix)` | 快速提取命令名（无分配） |
| `ExtractCommandAndArgs(content, prefix)` | 提取命令名与剩余参数 |
| `ValidateCommandName(name, prefix)` | 校验命令名合法性 |
| `ParseInt(s)` | 严格整数解析 |

---

*完整示例见 `examples/command-bot` 与 `builtin/core/help` 插件。*
