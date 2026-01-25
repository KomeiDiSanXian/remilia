# Help Plugin 命令发现示例

本示例演示 Help Plugin 如何自动发现插件注册的命令，无需手动维护命令列表。

## 核心机制

### 1. Matcher 元数据

每个 Matcher 现在可以包含丰富的元数据：

```go
m := eng.OnCommand(dto.GroupAtMessageCreate, "/echo")
m.SetDescription("回显用户发送的消息").
  SetUsage("/echo <消息内容>").
  SetCategory("实用工具").
  SetAliases("/repeat", "/mirror").
  SetExamples(
      "/echo Hello World",
      "/echo 你好，世界",
  )
```

### 2. Engine 命令发现 API

Engine 提供了多个 API 用于发现命令：

- `GetAllCommands()` - 获取所有命令
- `GetCommandsByPlugin()` - 按插件分组
- `GetCommandsByCategory()` - 按分类分组
- `FindCommand(name)` - 查找特定命令（支持别名）

### 3. 自动发现流程

```
插件注册命令                 Engine维护索引              Help Plugin查询
    │                           │                          │
    ├─ OnCommand()              │                          │
    ├─ SetDescription()    ──>  │ state.matchers[]    <──  │ GetAllCommands()
    ├─ SetUsage()               │ + metadata               │
    ├─ SetCategory()            │                          │
    └─ Handle()                 │                          └─ 生成帮助文本
```

## 运行示例

```bash
cd examples/help-discovery
go run main.go
```

## 输出示例

```
===== Help Plugin 命令发现演示 =====

1. 所有命令:
  - /echo: 回显用户发送的消息
  - /search: 搜索网络内容

2. 按插件分组:
  [global]
    - /echo: 回显用户发送的消息
    - /search: 搜索网络内容

3. 按分类分组:
  [实用工具]
    - /echo: 回显用户发送的消息
    - /search: 搜索网络内容

4. 查找命令 '/search':
  命令: /search
  描述: 搜索网络内容
  用法: /search <关键词> [--engine google|bing] [--count 5]
  分类: 实用工具
  示例:
    /search Go语言
    /search Python --engine bing
    /search 机器学习 --count 10
  参数:
    - query (string) (必需): 搜索关键词
  选项:
    - --engine, -e (默认: google): 搜索引擎
    - --count, -n (默认: 5): 结果数量
  所需权限: [use_search]

5. 查找别名 '/repeat':
  找到命令: /echo (别名 /repeat)

6. 生成帮助文本:
📖 可用命令列表

【实用工具】
  /echo - 回显用户发送的消息
    别名: /repeat, /mirror
    用法: /echo <消息内容>
  /search - 搜索网络内容
    用法: /search <关键词> [--engine google|bing] [--count 5]

💡 使用 /help <命令> 查看详细信息
```

## 插件开发最佳实践

### 1. 始终设置描述和用法

```go
m := eng.OnCommand(dto.GroupAtMessageCreate, "/mycommand")
m.SetDescription("命令的简短描述").  // ✅ 必需
  SetUsage("/mycommand <参数>")     // ✅ 必需
```

### 2. 使用有意义的分类

推荐分类：
- "系统" - 系统管理命令
- "管理" - Bot管理命令
- "实用工具" - 通用工具
- "娱乐" - 娱乐功能
- "AI" - AI相关功能
- "其他" - 默认分类

### 3. 提供丰富的示例

```go
m.SetExamples(
    "/command arg1",
    "/command arg1 --flag value",
)
```

### 4. 隐藏内部命令

```go
m := eng.OnCommand(dto.GroupAtMessageCreate, "/internal")
m.SetHidden(true)  // 不在帮助中显示
```

### 5. 复杂命令使用完整元数据

```go
m.SetMetadata(&engine.MatcherMetadata{
    Description: "详细描述",
    Usage:       "详细用法",
    Category:    "分类",
    Examples:    []string{"示例1", "示例2"},
    Permissions: []string{"permission1"},
    Arguments: []*engine.ArgumentMeta{
        {
            Name:        "arg1",
            Description: "参数描述",
            Required:    true,
            Type:        "string",
        },
    },
    Flags: []*engine.FlagMeta{
        {
            Name:        "flag1",
            ShortName:   "f",
            Description: "标志描述",
            Default:     "default",
        },
    },
})
```

## 相关文档

- [Help Plugin 设计方案](../../docs/HELP_PLUGIN_DESIGN.md)
- [内置插件设计](../../docs/BUILTIN_PLUGINS_DESIGN.md)
- [命令系统文档](../../command/README.md)

## 技术细节

### Matcher 元数据结构

```go
type MatcherMetadata struct {
    // 基本信息
    Description string   // 命令描述
    Usage       string   // 使用方法
    Aliases     []string // 别名
    Category    string   // 分类

    // 高级信息
    Examples    []string // 使用示例
    Permissions []string // 所需权限
    Hidden      bool     // 是否在帮助中隐藏

    // 参数定义
    Arguments []*ArgumentMeta
    Flags     []*FlagMeta
}
```

### CommandInfo 结构

```go
type CommandInfo struct {
    Command     string            // 命令名
    Description string            // 命令描述
    Usage       string            // 使用方法
    Aliases     []string          // 别名列表
    Category    string            // 分类
    Examples    []string          // 使用示例
    Permissions []string          // 所需权限
    Plugin      string            // 所属插件名
    Source      string            // 来源标识
    EventType   dto.EventType     // 事件类型
    Metadata    *MatcherMetadata  // 完整元数据
}
```

### 性能特性

- **无锁读取**: `GetAllCommands()` 使用 COW 模式，完全无锁
- **O(N) 复杂度**: N 为 Matcher 数量（通常很小）
- **零内存分配**: 命令发现过程高效
- **缓存友好**: 元数据存储在 Matcher 中，局部性好

## 常见问题

### Q: 命令如何去重？

A: `GetAllCommands()` 会自动去重，同一个命令只返回一次。

### Q: 如何支持命令别名？

A: 使用 `SetAliases()` 设置别名，`FindCommand()` 会自动匹配别名。

### Q: 隐藏命令会被发现吗？

A: 不会。设置 `SetHidden(true)` 的命令不会出现在 `GetAllCommands()` 的结果中。

### Q: 动态添加/删除命令会实时反映吗？

A: 是的。Help Plugin 每次调用时都会重新查询 Engine，获取最新的命令列表。

### Q: 性能影响如何？

A: 非常小。命令发现使用无锁读取，通常在微秒级别完成。
