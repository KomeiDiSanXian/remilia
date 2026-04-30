# Command 包集成方案

**日期**: 2026-01-24  
**状态**: 设计中

---

## 问题分析

### 现状

目前有两套相似的系统：

1. **Matcher 元数据系统** (`engine.MatcherMetadata`)
   - 用于 Help 生成
   - 简单的元数据存储
   - 手动设置字段

2. **Command 定义系统** (`command.Definition`)
   - 完整的命令定义和解析
   - 支持参数验证、类型转换
   - 支持子命令

### 相似性分析

| 特性 | MatcherMetadata | command.Definition |
|------|----------------|-------------------|
| 命令名 | - | ✅ Name |
| 别名 | ✅ Aliases | ✅ Aliases |
| 描述 | ✅ Description | ✅ Description |
| 用法 | ✅ Usage | ✅ Usage |
| 参数定义 | ✅ Arguments | ✅ Arguments |
| 标志定义 | ✅ Flags | ✅ Flags |
| 分类 | ✅ Category | ❌ |
| 示例 | ✅ Examples | ❌ |
| 权限 | ✅ Permissions | ❌ |
| 隐藏标记 | ✅ Hidden | ❌ |
| 子命令 | ❌ | ✅ SubCommands |
| 验证器 | ❌ | ✅ Validator |
| 处理器 | ❌ | ✅ Handler |

### 集成目标

1. ✅ **统一接口**: 使用 `command.Definition` 作为主要定义方式
2. ✅ **自动转换**: `Definition` → `MatcherMetadata`
3. ✅ **增强字段**: 为 `Definition` 添加 Category, Examples, Permissions, Hidden
4. ✅ **便捷 API**: 提供 `Engine.RegisterCommandDef()` 简化注册
5. ✅ **向后兼容**: 保留原有的 `SetDescription()` 等方法

---

## 集成方案

### Phase 1: 增强 command.Definition

在 `command/enhanced_system.go` 中扩展 Definition：

```go
type Definition struct {
    Name        string
    Aliases     []string
    Description string
    Usage       string

    Arguments []*Argument
    Flags     []*Flag

    SubCommands []*Definition

    Validator func(*Parsed) error
    Handler   Handler
    
    // ===== 新增字段（用于 Help 生成）=====
    Category    string   // 命令分类
    Examples    []string // 使用示例
    Permissions []string // 所需权限
    Hidden      bool     // 是否在帮助中隐藏
}
```

### Phase 2: 添加转换函数

在 `core/engine/command_integration.go` 中：

```go
// DefinitionToMetadata 将 command.Definition 转换为 MatcherMetadata
func DefinitionToMetadata(def *command.Definition) *MatcherMetadata {
    if def == nil {
        return nil
    }
    
    return &MatcherMetadata{
        Description: def.Description,
        Usage:       def.Usage,
        Aliases:     def.Aliases,
        Category:    def.Category,
        Examples:    def.Examples,
        Permissions: def.Permissions,
        Hidden:      def.Hidden,
        Arguments:   convertArguments(def.Arguments),
        Flags:       convertFlags(def.Flags),
    }
}

// convertArguments 转换参数定义
func convertArguments(args []*command.Argument) []*ArgumentMeta {
    if len(args) == 0 {
        return nil
    }
    
    result := make([]*ArgumentMeta, len(args))
    for i, arg := range args {
        result[i] = &ArgumentMeta{
            Name:        arg.Name,
            Description: arg.Description,
            Required:    arg.Required,
            Type:        argTypeToString(arg.Type),
        }
    }
    return result
}

// convertFlags 转换标志定义
func convertFlags(flags []*command.Flag) []*FlagMeta {
    if len(flags) == 0 {
        return nil
    }
    
    result := make([]*FlagMeta, len(flags))
    for i, flag := range flags {
        defaultStr := ""
        if flag.Default != nil {
            defaultStr = fmt.Sprint(flag.Default)
        }
        
        result[i] = &FlagMeta{
            Name:        flag.Name,
            ShortName:   flag.ShortName,
            Description: flag.Description,
            Default:     defaultStr,
        }
    }
    return result
}

// argTypeToString 转换参数类型
func argTypeToString(t command.ArgType) string {
    switch t {
    case command.ArgTypeString:
        return "string"
    case command.ArgTypeInt:
        return "int"
    case command.ArgTypeBool:
        return "bool"
    case command.ArgTypeFloat:
        return "float"
    case command.ArgTypeStringSlice:
        return "[]string"
    default:
        return "string"
    }
}
```

### Phase 3: 增强 Engine API

在 `core/engine/engine.go` 中：

```go
// RegisterCommandDef 注册 command.Definition（自动设置元数据）
//
// 这是推荐的命令注册方式，集成了命令解析和元数据管理。
//
// 参数:
//   - eventType: 事件类型（空字符串表示所有类型）
//   - def: 命令定义
//   - extraRules: 额外的匹配规则（可选）
//
// 返回:
//   - 注册的 Matcher
//
// 示例:
//   def := &command.Definition{
//       Name:        "search",
//       Aliases:     []string{"find", "query"},
//       Description: "搜索内容",
//       Usage:       "/search <keyword> [--engine google]",
//       Category:    "实用工具",
//       Examples:    []string{"/search Go语言"},
//       Arguments: []*command.Argument{
//           {Name: "keyword", Description: "搜索关键词", Required: true, Type: command.ArgTypeString},
//       },
//       Flags: []*command.Flag{
//           {Name: "engine", ShortName: "e", Description: "搜索引擎", Default: "google"},
//       },
//   }
//   m := engine.RegisterCommandDef(dto.GroupAtMessageCreate, def)
func (e *Engine) RegisterCommandDef(eventType dto.EventType, def *command.Definition, extraRules ...context.Rule) *Matcher {
    if def == nil {
        logrus.Warn("[Engine] RegisterCommandDef: definition is nil")
        return noopMatcher
    }
    
    trigger := "/" + def.Name
    
    // 构造解析规则
    parseRule := func(ctx *context.Context) bool {
        content := ctx.GetMessageContent()
        parsed, err := command.ParseFromDefinition(content, def, "/")
        if err != nil {
            logrus.WithError(err).
                WithField("trigger", trigger).
                Debug("[Engine] Command parse failed")
            return false
        }
        ctx.SetParsedCommand(parsed)
        return true
    }
    
    // 组合规则
    finalRules := make([]context.Rule, 0, len(extraRules)+1)
    finalRules = append(finalRules, parseRule)
    finalRules = append(finalRules, extraRules...)
    
    // 注册命令
    m := e.OnCommand(eventType, trigger, finalRules...)
    
    // 自动转换并设置元数据
    metadata := DefinitionToMetadata(def)
    m.SetMetadata(metadata)
    
    // 如果 Definition 有 Handler，自动设置
    if def.Handler != nil {
        m.Handle(func(ctx *context.Context) error {
            def.Handler(ctx)
            return nil
        })
    }
    
    return m
}

// RegisterCommandDefWithPrefix 带自定义前缀的 RegisterCommandDef
func (e *Engine) RegisterCommandDefWithPrefix(
    eventType dto.EventType,
    prefix string,
    def *command.Definition,
    extraRules ...context.Rule,
) *Matcher {
    if def == nil {
        logrus.Warn("[Engine] RegisterCommandDefWithPrefix: definition is nil")
        return noopMatcher
    }
    
    if prefix == "" {
        prefix = "/"
    }
    
    trigger := prefix + def.Name
    
    // 构造解析规则
    parseRule := func(ctx *context.Context) bool {
        content := ctx.GetMessageContent()
        parsed, err := command.ParseFromDefinition(content, def, prefix)
        if err != nil {
            return false
        }
        ctx.SetParsedCommand(parsed)
        return true
    }
    
    finalRules := make([]context.Rule, 0, len(extraRules)+1)
    finalRules = append(finalRules, parseRule)
    finalRules = append(finalRules, extraRules...)
    
    m := e.OnCommand(eventType, trigger, finalRules...)
    
    // 自动设置元数据
    metadata := DefinitionToMetadata(def)
    m.SetMetadata(metadata)
    
    // 自动设置 Handler
    if def.Handler != nil {
        m.Handle(func(ctx *context.Context) error {
            def.Handler(ctx)
            return nil
        })
    }
    
    return m
}
```

### Phase 4: 插件使用示例

```go
// Before (手动设置元数据)
func (p *SearchPlugin) Load(eng *engine.Engine) error {
    m := eng.OnCommand(dto.GroupAtMessageCreate, "/search")
    m.SetDescription("搜索网络内容").
      SetUsage("/search <关键词> [--engine google|bing]").
      SetCategory("实用工具").
      SetMetadata(&engine.MatcherMetadata{
          Arguments: []*engine.ArgumentMeta{...},
          Flags: []*engine.FlagMeta{...},
      })
    m.Handle(p.handleSearch)
    p.AddMatcher(m)
    return nil
}

// After (使用 command.Definition)
func (p *SearchPlugin) Load(eng *engine.Engine) error {
    def := &command.Definition{
        Name:        "search",
        Aliases:     []string{"find", "query"},
        Description: "搜索网络内容",
        Usage:       "/search <关键词> [--engine google|bing]",
        Category:    "实用工具",
        Examples: []string{
            "/search Go语言",
            "/search Python --engine bing",
        },
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
                Name:        "engine",
                ShortName:   "e",
                Description: "搜索引擎",
                Type:        command.ArgTypeString,
                Default:     "google",
            },
            {
                Name:        "count",
                ShortName:   "n",
                Description: "结果数量",
                Type:        command.ArgTypeInt,
                Default:     5,
            },
        },
        Handler: func(ctx any) {
            // 可选：直接在 Definition 中定义 Handler
            p.handleSearch(ctx.(*context.Context))
        },
    }
    
    m := eng.RegisterCommandDef(dto.GroupAtMessageCreate, def)
    p.AddMatcher(m)
    return nil
}
```

---

## 优势分析

### 1. 统一定义

**Before**:
```go
// 需要维护两套定义
def := &command.Definition{...}
metadata := &engine.MatcherMetadata{...}
```

**After**:
```go
// 只需一套定义
def := &command.Definition{
    Name: "search",
    Description: "...",
    Arguments: [...],
    Category: "实用工具",  // 新增
    Examples: [...],       // 新增
}
```

### 2. 自动解析

```go
// Definition 包含完整的解析逻辑
m := eng.RegisterCommandDef(dto.GroupAtMessageCreate, def)

// 在 Handler 中直接使用解析结果
func handleSearch(ctx *context.Context) error {
    parsed := ctx.GetParsedCommand()
    keyword := parsed.GetString("keyword")
    engine := parsed.GetString("engine")
    count := parsed.GetInt("count")
    // ...
}
```

### 3. 类型安全

```go
// command.Definition 提供类型转换
Arguments: []*command.Argument{
    {Name: "count", Type: command.ArgTypeInt, Default: 5},
}

// 获取时自动转换
count := parsed.GetInt("count")  // int 类型
```

### 4. 参数验证

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

### 5. 子命令支持

```go
def := &command.Definition{
    Name: "admin",
    SubCommands: []*command.Definition{
        {
            Name:        "reload",
            Description: "重载插件",
            Arguments: []*command.Argument{
                {Name: "plugin", Type: command.ArgTypeString, Required: true},
            },
        },
        {
            Name:        "status",
            Description: "查看状态",
        },
    },
}
```

---

## 实施步骤

### Step 1: 扩展 command.Definition (30分钟)

- [ ] 添加 `Category string`
- [ ] 添加 `Examples []string`
- [ ] 添加 `Permissions []string`
- [ ] 添加 `Hidden bool`

### Step 2: 创建转换函数 (30分钟)

- [ ] 实现 `DefinitionToMetadata()`
- [ ] 实现 `convertArguments()`
- [ ] 实现 `convertFlags()`
- [ ] 实现 `argTypeToString()`

### Step 3: 增强 Engine API (1小时)

- [ ] 实现 `RegisterCommandDef()`
- [ ] 实现 `RegisterCommandDefWithPrefix()`
- [ ] 添加单元测试

### Step 4: 创建示例 (30分钟)

- [ ] 创建 `examples/command-integration/`
- [ ] 演示 Definition 使用
- [ ] 演示自动解析
- [ ] 演示参数验证

### Step 5: 文档更新 (30分钟)

- [ ] 更新 `BUILTIN_PLUGINS_DESIGN.md`
- [ ] 更新 `HELP_PLUGIN_DESIGN.md`
- [ ] 创建集成指南

---

## 向后兼容性

### 保留现有 API

```go
// ✅ 仍然支持
m := eng.OnCommand(dto.GroupAtMessageCreate, "/echo")
m.SetDescription("...")
m.SetUsage("...")
m.Handle(handler)

// ✅ 新增推荐方式
def := &command.Definition{...}
m := eng.RegisterCommandDef(dto.GroupAtMessageCreate, def)
```

### 两种方式可以混用

```go
// 使用 Definition 注册
m := eng.RegisterCommandDef(dto.GroupAtMessageCreate, def)

// 仍然可以手动设置额外元数据
m.SetExamples("/search golang", "/search python")
```

---

## 最佳实践

### 1. 简单命令：使用链式调用

```go
m := eng.OnCommand(dto.GroupAtMessageCreate, "/ping")
m.SetDescription("测试连接")
m.Handle(handlePing)
```

### 2. 中等命令：使用 Definition（无参数）

```go
def := &command.Definition{
    Name:        "status",
    Description: "查看状态",
    Category:    "管理",
}
m := eng.RegisterCommandDef(dto.GroupAtMessageCreate, def)
m.Handle(handleStatus)
```

### 3. 复杂命令：使用完整 Definition

```go
def := &command.Definition{
    Name:        "search",
    Aliases:     []string{"find"},
    Description: "搜索内容",
    Usage:       "/search <keyword> [flags]",
    Category:    "实用工具",
    Examples:    []string{"/search golang"},
    Arguments: []*command.Argument{
        {Name: "keyword", Type: command.ArgTypeString, Required: true},
    },
    Flags: []*command.Flag{
        {Name: "count", Type: command.ArgTypeInt, Default: 10},
    },
    Handler: handleSearch,
}
m := eng.RegisterCommandDef(dto.GroupAtMessageCreate, def)
```

---

## 性能影响

### 转换开销

- `DefinitionToMetadata()`: O(N) - N 为参数+标志数量
- 通常 < 100 个字段，开销可忽略
- 只在命令注册时执行一次

### 内存开销

- Definition: ~200 bytes
- MatcherMetadata: ~150 bytes
- 总计: ~350 bytes per command
- 100 个命令: ~35KB

### 执行性能

- 无影响：元数据只在 Help 生成时使用
- 命令解析性能由 command 包保证

---

## 总结

### 集成前

- 两套系统：Definition + MatcherMetadata
- 手动维护：需要同时设置两处
- 不一致风险：可能忘记更新元数据

### 集成后

- 统一系统：Definition 作为唯一定义
- 自动转换：DefinitionToMetadata 自动生成元数据
- 类型安全：完整的参数解析和验证
- 功能增强：支持子命令、验证器
- 向后兼容：保留所有现有 API

### 推荐使用

✅ **简单命令** → 链式调用  
✅ **复杂命令** → command.Definition  
✅ **渐进迁移** → 逐步替换为 Definition

---

**下一步**: 实施 Step 1-5
