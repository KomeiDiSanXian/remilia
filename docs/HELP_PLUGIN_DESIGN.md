# Help Plugin 设计方案与命令发现机制

**日期**: 2026-01-24  
**问题**: 插件在 handler 中动态注册命令，Help Plugin 如何自动发现和生成帮助信息？

---

## 问题分析

### 当前架构

```go
// 插件在 Load 中注册命令
func (p *MyPlugin) Load(eng *engine.Engine) error {
    m := eng.OnCommand(dto.GroupAtMessageCreate, "/mycommand")
    m.Handle(func(ctx *context.Context) error {
        return ctx.Reply("Hello!")
    })
    p.AddMatcher(m)
    return nil
}
```

**挑战**:
1. ❌ Matcher 只有 `command` 字段，没有描述、用法等元数据
2. ❌ Help Plugin 无法区分哪些是用户命令，哪些是内部匹配器
3. ❌ 无法获取命令的别名、参数、权限等信息
4. ❌ 命令可能在运行时动态添加/删除

---

## 解决方案

### 方案 1: 扩展 Matcher 元数据 ⭐ **推荐**

**核心思路**: 在 Matcher 中添加可选的命令元数据字段

#### 1.1 扩展 Matcher 结构

```go
// core/engine/matcher.go

type Matcher struct {
    // ...existing fields...
    
    // 命令元数据（可选，用于 Help 生成）
    metadata *MatcherMetadata
}

// MatcherMetadata 匹配器元数据
type MatcherMetadata struct {
    // 基本信息
    Description string   // 命令描述
    Usage       string   // 使用方法
    Aliases     []string // 别名
    Category    string   // 分类（如 "管理"、"娱乐"）
    
    // 高级信息
    Examples    []string // 使用示例
    Permissions []string // 所需权限
    Hidden      bool     // 是否在帮助中隐藏
    
    // 参数定义（可选）
    Arguments   []*ArgumentMeta
    Flags       []*FlagMeta
}

type ArgumentMeta struct {
    Name        string
    Description string
    Required    bool
    Type        string // "string", "int", "bool"
}

type FlagMeta struct {
    Name        string
    ShortName   string
    Description string
    Default     string
}
```

#### 1.2 添加 Metadata 设置方法

```go
// SetMetadata 设置匹配器元数据
func (m *Matcher) SetMetadata(meta *MatcherMetadata) *Matcher {
    if m.isNoop() {
        return m
    }
    m.rt.mu.Lock()
    m.metadata = meta
    m.rt.mu.Unlock()
    return m
}

// GetMetadata 获取匹配器元数据
func (m *Matcher) GetMetadata() *MatcherMetadata {
    m.rt.mu.RLock()
    defer m.rt.mu.RUnlock()
    return m.metadata
}

// 便捷方法
func (m *Matcher) SetDescription(desc string) *Matcher {
    m.rt.mu.Lock()
    if m.metadata == nil {
        m.metadata = &MatcherMetadata{}
    }
    m.metadata.Description = desc
    m.rt.mu.Unlock()
    return m
}

func (m *Matcher) SetUsage(usage string) *Matcher {
    m.rt.mu.Lock()
    if m.metadata == nil {
        m.metadata = &MatcherMetadata{}
    }
    m.metadata.Usage = usage
    m.rt.mu.Unlock()
    return m
}

func (m *Matcher) SetCategory(category string) *Matcher {
    m.rt.mu.Lock()
    if m.metadata == nil {
        m.metadata = &MatcherMetadata{}
    }
    m.metadata.Category = category
    m.rt.mu.Unlock()
    return m
}

func (m *Matcher) SetAliases(aliases ...string) *Matcher {
    m.rt.mu.Lock()
    if m.metadata == nil {
        m.metadata = &MatcherMetadata{}
    }
    m.metadata.Aliases = aliases
    m.rt.mu.Unlock()
    return m
}

func (m *Matcher) SetHidden(hidden bool) *Matcher {
    m.rt.mu.Lock()
    if m.metadata == nil {
        m.metadata = &MatcherMetadata{}
    }
    m.metadata.Hidden = hidden
    m.rt.mu.Unlock()
    return m
}
```

#### 1.3 插件使用方式

```go
func (p *EchoPlugin) Load(eng *engine.Engine) error {
    m := eng.OnCommand(dto.GroupAtMessageCreate, "/echo")
    m.SetDescription("回显用户发送的消息").
      SetUsage("/echo <消息内容>").
      SetCategory("实用工具").
      SetAliases("/repeat", "/mirror").
      SetMetadata(&engine.MatcherMetadata{
          Examples: []string{
              "/echo Hello World",
              "/echo 你好，世界",
          },
          Arguments: []*engine.ArgumentMeta{
              {
                  Name:        "message",
                  Description: "要回显的消息内容",
                  Required:    true,
                  Type:        "string",
              },
          },
      })
    
    m.Handle(p.handleEcho)
    p.AddMatcher(m)
    return nil
}
```

#### 1.4 Engine 添加命令发现 API

```go
// core/engine/engine.go

// CommandInfo 命令信息
type CommandInfo struct {
    Command     string
    Description string
    Usage       string
    Aliases     []string
    Category    string
    Examples    []string
    Plugin      string // 所属插件
    Source      string // 来源（如 "plugin:echo"）
    EventType   dto.EventType
    Metadata    *MatcherMetadata
}

// GetAllCommands 获取所有已注册的命令信息
func (e *Engine) GetAllCommands() []CommandInfo {
    state := e.state.Load().(*engineState)
    
    commands := make([]CommandInfo, 0)
    seen := make(map[string]bool) // 去重
    
    // 遍历所有 matcher
    for _, m := range state.matchers {
        // 只包含有命令的 matcher
        cmd := m.GetCommand()
        if cmd == "" {
            continue
        }
        
        // 去重
        if seen[cmd] {
            continue
        }
        seen[cmd] = true
        
        // 获取元数据
        meta := m.GetMetadata()
        
        // 跳过隐藏命令
        if meta != nil && meta.Hidden {
            continue
        }
        
        info := CommandInfo{
            Command:   cmd,
            EventType: m.EventType,
            Source:    m.GetSource(),
            Metadata:  meta,
        }
        
        // 填充元数据
        if meta != nil {
            info.Description = meta.Description
            info.Usage = meta.Usage
            info.Aliases = meta.Aliases
            info.Category = meta.Category
            info.Examples = meta.Examples
        }
        
        // 提取插件名
        if strings.HasPrefix(m.GetSource(), "plugin:") {
            info.Plugin = strings.TrimPrefix(m.GetSource(), "plugin:")
        }
        
        commands = append(commands, info)
    }
    
    return commands
}

// GetCommandsByPlugin 按插件分组获取命令
func (e *Engine) GetCommandsByPlugin() map[string][]CommandInfo {
    commands := e.GetAllCommands()
    grouped := make(map[string][]CommandInfo)
    
    for _, cmd := range commands {
        plugin := cmd.Plugin
        if plugin == "" {
            plugin = "global" // 全局命令
        }
        grouped[plugin] = append(grouped[plugin], cmd)
    }
    
    return grouped
}

// GetCommandsByCategory 按分类获取命令
func (e *Engine) GetCommandsByCategory() map[string][]CommandInfo {
    commands := e.GetAllCommands()
    grouped := make(map[string][]CommandInfo)
    
    for _, cmd := range commands {
        category := cmd.Category
        if category == "" {
            category = "其他"
        }
        grouped[category] = append(grouped[category], cmd)
    }
    
    return grouped
}

// FindCommand 查找特定命令（支持别名）
func (e *Engine) FindCommand(name string) *CommandInfo {
    commands := e.GetAllCommands()
    
    for _, cmd := range commands {
        // 匹配命令名
        if cmd.Command == name {
            return &cmd
        }
        
        // 匹配别名
        for _, alias := range cmd.Aliases {
            if alias == name {
                return &cmd
            }
        }
    }
    
    return nil
}
```

#### 1.5 Help Plugin 实现

```go
// plugin/builtin/core/help/help.go

package help

import (
    "fmt"
    "sort"
    "strings"
    
    "github.com/KomeiDiSanXian/remilia/core/context"
    "github.com/KomeiDiSanXian/remilia/core/engine"
    "github.com/KomeiDiSanXian/remilia/openapi/dto"
    "github.com/KomeiDiSanXian/remilia/plugin"
)

type Plugin struct {
    *plugin.BasePlugin
    config Config
    engine *engine.Engine
}

type Config struct {
    Trigger        string
    GroupByPlugin  bool
    GroupByCategory bool
    ShowAliases    bool
    ShowUsage      bool
    ShowExamples   bool
    Format         string // "text" or "markdown"
}

func New(cfg Config) *Plugin {
    if cfg.Trigger == "" {
        cfg.Trigger = "/help"
    }
    
    return &Plugin{
        BasePlugin: plugin.NewBasePlugin("help"),
        config:     cfg,
    }
}

func (p *Plugin) Load(eng *engine.Engine) error {
    p.engine = eng
    
    // 注册 /help 命令
    m := eng.OnCommand(dto.GroupAtMessageCreate, p.config.Trigger)
    m.SetDescription("显示所有可用命令的帮助信息").
      SetUsage(fmt.Sprintf("%s [命令名]", p.config.Trigger)).
      SetCategory("系统").
      SetMetadata(&engine.MatcherMetadata{
          Examples: []string{
              p.config.Trigger,
              fmt.Sprintf("%s echo", p.config.Trigger),
          },
          Arguments: []*engine.ArgumentMeta{
              {
                  Name:        "command",
                  Description: "要查询的命令名（可选）",
                  Required:    false,
                  Type:        "string",
              },
          },
      })
    
    m.Handle(p.handleHelp)
    p.AddMatcher(m)
    
    return nil
}

func (p *Plugin) handleHelp(ctx *context.Context) error {
    content := ctx.GetMessageContent()
    args := strings.Fields(strings.TrimPrefix(content, p.config.Trigger))
    
    if len(args) > 0 {
        // 显示特定命令的帮助
        return p.showCommandHelp(ctx, args[0])
    }
    
    // 显示所有命令
    return p.showAllCommands(ctx)
}

func (p *Plugin) showAllCommands(ctx *context.Context) error {
    var buf strings.Builder
    
    if p.config.Format == "markdown" {
        buf.WriteString("# 📖 命令列表\n\n")
    } else {
        buf.WriteString("📖 可用命令列表\n\n")
    }
    
    if p.config.GroupByPlugin {
        p.renderByPlugin(&buf)
    } else if p.config.GroupByCategory {
        p.renderByCategory(&buf)
    } else {
        p.renderFlat(&buf)
    }
    
    buf.WriteString(fmt.Sprintf("\n💡 使用 %s <命令> 查看详细信息", p.config.Trigger))
    
    return ctx.Reply(buf.String())
}

func (p *Plugin) renderByPlugin(buf *strings.Builder) {
    grouped := p.engine.GetCommandsByPlugin()
    
    // 排序插件名
    plugins := make([]string, 0, len(grouped))
    for name := range grouped {
        plugins = append(plugins, name)
    }
    sort.Strings(plugins)
    
    for _, pluginName := range plugins {
        commands := grouped[pluginName]
        
        if p.config.Format == "markdown" {
            buf.WriteString(fmt.Sprintf("## %s\n\n", pluginName))
        } else {
            buf.WriteString(fmt.Sprintf("【%s】\n", pluginName))
        }
        
        for _, cmd := range commands {
            p.renderCommand(buf, cmd)
        }
        
        buf.WriteString("\n")
    }
}

func (p *Plugin) renderByCategory(buf *strings.Builder) {
    grouped := p.engine.GetCommandsByCategory()
    
    // 排序分类名
    categories := make([]string, 0, len(grouped))
    for name := range grouped {
        categories = append(categories, name)
    }
    sort.Strings(categories)
    
    for _, category := range categories {
        commands := grouped[category]
        
        if p.config.Format == "markdown" {
            buf.WriteString(fmt.Sprintf("## %s\n\n", category))
        } else {
            buf.WriteString(fmt.Sprintf("【%s】\n", category))
        }
        
        for _, cmd := range commands {
            p.renderCommand(buf, cmd)
        }
        
        buf.WriteString("\n")
    }
}

func (p *Plugin) renderFlat(buf *strings.Builder) {
    commands := p.engine.GetAllCommands()
    
    // 按命令名排序
    sort.Slice(commands, func(i, j int) bool {
        return commands[i].Command < commands[j].Command
    })
    
    for _, cmd := range commands {
        p.renderCommand(buf, cmd)
    }
}

func (p *Plugin) renderCommand(buf *strings.Builder, cmd engine.CommandInfo) {
    if p.config.Format == "markdown" {
        buf.WriteString(fmt.Sprintf("- **%s**", cmd.Command))
        if cmd.Description != "" {
            buf.WriteString(fmt.Sprintf(" - %s", cmd.Description))
        }
        buf.WriteString("\n")
        
        if p.config.ShowAliases && len(cmd.Aliases) > 0 {
            buf.WriteString(fmt.Sprintf("  - 别名: %s\n", 
                strings.Join(cmd.Aliases, ", ")))
        }
    } else {
        buf.WriteString(fmt.Sprintf("  %s", cmd.Command))
        if cmd.Description != "" {
            buf.WriteString(fmt.Sprintf(" - %s", cmd.Description))
        }
        buf.WriteString("\n")
        
        if p.config.ShowAliases && len(cmd.Aliases) > 0 {
            buf.WriteString(fmt.Sprintf("    别名: %s\n", 
                strings.Join(cmd.Aliases, ", ")))
        }
    }
}

func (p *Plugin) showCommandHelp(ctx *context.Context, name string) error {
    cmdInfo := p.engine.FindCommand(name)
    if cmdInfo == nil {
        return ctx.Reply(fmt.Sprintf("❌ 未找到命令: %s", name))
    }
    
    var buf strings.Builder
    
    if p.config.Format == "markdown" {
        buf.WriteString(fmt.Sprintf("# %s\n\n", cmdInfo.Command))
    } else {
        buf.WriteString(fmt.Sprintf("📖 命令详情: %s\n\n", cmdInfo.Command))
    }
    
    // 描述
    if cmdInfo.Description != "" {
        buf.WriteString(fmt.Sprintf("**描述**: %s\n\n", cmdInfo.Description))
    }
    
    // 用法
    if p.config.ShowUsage && cmdInfo.Usage != "" {
        buf.WriteString(fmt.Sprintf("**用法**: %s\n\n", cmdInfo.Usage))
    }
    
    // 别名
    if p.config.ShowAliases && len(cmdInfo.Aliases) > 0 {
        buf.WriteString(fmt.Sprintf("**别名**: %s\n\n", 
            strings.Join(cmdInfo.Aliases, ", ")))
    }
    
    // 参数
    if cmdInfo.Metadata != nil && len(cmdInfo.Metadata.Arguments) > 0 {
        buf.WriteString("**参数**:\n")
        for _, arg := range cmdInfo.Metadata.Arguments {
            required := ""
            if arg.Required {
                required = " (必需)"
            }
            buf.WriteString(fmt.Sprintf("  - `%s` (%s)%s: %s\n", 
                arg.Name, arg.Type, required, arg.Description))
        }
        buf.WriteString("\n")
    }
    
    // 标志
    if cmdInfo.Metadata != nil && len(cmdInfo.Metadata.Flags) > 0 {
        buf.WriteString("**选项**:\n")
        for _, flag := range cmdInfo.Metadata.Flags {
            short := ""
            if flag.ShortName != "" {
                short = fmt.Sprintf(", -%s", flag.ShortName)
            }
            def := ""
            if flag.Default != "" {
                def = fmt.Sprintf(" (默认: %s)", flag.Default)
            }
            buf.WriteString(fmt.Sprintf("  - `--%s%s`%s: %s\n", 
                flag.Name, short, def, flag.Description))
        }
        buf.WriteString("\n")
    }
    
    // 示例
    if p.config.ShowExamples && len(cmdInfo.Examples) > 0 {
        buf.WriteString("**示例**:\n")
        for _, example := range cmdInfo.Examples {
            if p.config.Format == "markdown" {
                buf.WriteString(fmt.Sprintf("```\n%s\n```\n", example))
            } else {
                buf.WriteString(fmt.Sprintf("  %s\n", example))
            }
        }
        buf.WriteString("\n")
    }
    
    // 权限
    if cmdInfo.Metadata != nil && len(cmdInfo.Metadata.Permissions) > 0 {
        buf.WriteString(fmt.Sprintf("**所需权限**: %s\n\n", 
            strings.Join(cmdInfo.Metadata.Permissions, ", ")))
    }
    
    // 所属插件
    if cmdInfo.Plugin != "" {
        buf.WriteString(fmt.Sprintf("**所属插件**: %s\n", cmdInfo.Plugin))
    }
    
    return ctx.Reply(buf.String())
}
```

---

### 方案 2: 全局命令注册表

**核心思路**: 创建独立的命令注册中心，插件注册时同时更新注册表

#### 2.1 创建命令注册表

```go
// core/engine/command_registry.go

package engine

import (
    "sync"
)

// GlobalCommandRegistry 全局命令注册表
var GlobalCommandRegistry = NewCommandRegistry()

// CommandRegistry 命令注册表
type CommandRegistry struct {
    mu       sync.RWMutex
    commands map[string]*RegisteredCommand
}

type RegisteredCommand struct {
    Command     string
    Description string
    Usage       string
    Aliases     []string
    Category    string
    Examples    []string
    Plugin      string
    Matcher     *Matcher // 关联的 Matcher
}

func NewCommandRegistry() *CommandRegistry {
    return &CommandRegistry{
        commands: make(map[string]*RegisteredCommand),
    }
}

func (r *CommandRegistry) Register(cmd *RegisteredCommand) {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.commands[cmd.Command] = cmd
}

func (r *CommandRegistry) Unregister(command string) {
    r.mu.Lock()
    defer r.mu.Unlock()
    delete(r.commands, command)
}

func (r *CommandRegistry) GetAll() []*RegisteredCommand {
    r.mu.RLock()
    defer r.mu.RUnlock()
    
    result := make([]*RegisteredCommand, 0, len(r.commands))
    for _, cmd := range r.commands {
        result = append(result, cmd)
    }
    return result
}

func (r *CommandRegistry) Find(name string) *RegisteredCommand {
    r.mu.RLock()
    defer r.mu.RUnlock()
    
    // 直接匹配
    if cmd, ok := r.commands[name]; ok {
        return cmd
    }
    
    // 匹配别名
    for _, cmd := range r.commands {
        for _, alias := range cmd.Aliases {
            if alias == name {
                return cmd
            }
        }
    }
    
    return nil
}
```

#### 2.2 插件使用方式

```go
func (p *EchoPlugin) Load(eng *engine.Engine) error {
    m := eng.OnCommand(dto.GroupAtMessageCreate, "/echo")
    m.Handle(p.handleEcho)
    p.AddMatcher(m)
    
    // 同时注册到全局注册表
    engine.GlobalCommandRegistry.Register(&engine.RegisteredCommand{
        Command:     "/echo",
        Description: "回显用户发送的消息",
        Usage:       "/echo <消息内容>",
        Aliases:     []string{"/repeat", "/mirror"},
        Category:    "实用工具",
        Examples: []string{
            "/echo Hello World",
            "/echo 你好，世界",
        },
        Plugin:  p.Name(),
        Matcher: m,
    })
    
    return nil
}

func (p *EchoPlugin) Unload(eng *engine.Engine) error {
    // 从注册表移除
    engine.GlobalCommandRegistry.Unregister("/echo")
    return p.BasePlugin.Unload(eng)
}
```

**缺点**: 
- 需要手动维护两份数据（Matcher + Registry）
- 容易不同步
- 插件开发者容易忘记注册

---

### 方案 3: Command Definition 集成

**核心思路**: 利用现有的 `command.Definition` 系统

#### 3.1 扩展 Engine 方法

```go
// core/engine/engine.go

// RegisterCommandWithMeta 注册命令并自动记录元数据
func (e *Engine) RegisterCommandWithMeta(
    eventType dto.EventType,
    def *command.Definition,
    handler context.Handler,
) *Matcher {
    trigger := "/" + def.Name
    
    m := e.OnCommand(eventType, trigger)
    m.Handle(handler)
    
    // 自动设置元数据
    aliases := make([]string, len(def.Aliases))
    for i, alias := range def.Aliases {
        aliases[i] = "/" + alias
    }
    
    m.SetMetadata(&MatcherMetadata{
        Description: def.Description,
        Usage:       def.Usage,
        Aliases:     aliases,
        // 从 Definition 提取参数信息
        Arguments:   convertArguments(def.Arguments),
        Flags:       convertFlags(def.Flags),
    })
    
    return m
}

func convertArguments(args []*command.Argument) []*ArgumentMeta {
    result := make([]*ArgumentMeta, len(args))
    for i, arg := range args {
        result[i] = &ArgumentMeta{
            Name:        arg.Name,
            Description: arg.Description,
            Required:    arg.Required,
            Type:        arg.Type.String(),
        }
    }
    return result
}

func convertFlags(flags []*command.Flag) []*FlagMeta {
    result := make([]*FlagMeta, len(flags))
    for i, flag := range flags {
        result[i] = &FlagMeta{
            Name:        flag.Name,
            ShortName:   flag.ShortName,
            Description: flag.Description,
            Default:     fmt.Sprint(flag.Default),
        }
    }
    return result
}
```

#### 3.2 插件使用方式

```go
func (p *EchoPlugin) Load(eng *engine.Engine) error {
    // 定义命令
    def := &command.Definition{
        Name:        "echo",
        Aliases:     []string{"repeat", "mirror"},
        Description: "回显用户发送的消息",
        Usage:       "/echo <消息内容>",
        Arguments: []*command.Argument{
            {
                Name:        "message",
                Description: "要回显的消息内容",
                Type:        command.ArgTypeString,
                Required:    true,
            },
        },
    }
    
    // 注册命令（自动设置元数据）
    m := eng.RegisterCommandWithMeta(
        dto.GroupAtMessageCreate,
        def,
        p.handleEcho,
    )
    
    p.AddMatcher(m)
    return nil
}
```

---

## 推荐方案

### ⭐ **方案 1 + 方案 3 混合**

1. **扩展 Matcher 添加元数据字段** (方案 1)
2. **提供便捷的 RegisterCommandWithMeta** (方案 3)
3. **Engine 提供命令发现 API**

**优点**:
- ✅ 数据一致性：元数据存储在 Matcher 中，单一数据源
- ✅ 灵活性：支持简单命令（链式调用）和复杂命令（Definition）
- ✅ 自动发现：Help Plugin 通过 Engine API 自动获取所有命令
- ✅ 向后兼容：不强制要求设置元数据，旧代码仍可运行

---

## 实施步骤

### Phase 1: 核心扩展 (1-2天)

1. [ ] 扩展 `Matcher` 添加 `metadata` 字段
2. [ ] 实现 `SetMetadata()`, `GetMetadata()` 等方法
3. [ ] 添加 `MatcherMetadata`, `ArgumentMeta`, `FlagMeta` 类型
4. [ ] 实现便捷方法：`SetDescription()`, `SetUsage()`, `SetCategory()` 等

### Phase 2: Engine API (1天)

1. [ ] 实现 `Engine.GetAllCommands()`
2. [ ] 实现 `Engine.GetCommandsByPlugin()`
3. [ ] 实现 `Engine.GetCommandsByCategory()`
4. [ ] 实现 `Engine.FindCommand()`
5. [ ] 实现 `Engine.RegisterCommandWithMeta()`

### Phase 3: Help Plugin (2天)

1. [ ] 实现 Help Plugin 基础功能
2. [ ] 支持多种显示格式（text/markdown）
3. [ ] 支持按插件/分类分组
4. [ ] 实现命令详情显示
5. [ ] 添加配置选项

### Phase 4: 文档和示例 (1天)

1. [ ] 更新插件开发文档
2. [ ] 添加 Help Plugin 使用示例
3. [ ] 更新 BUILTIN_PLUGINS_DESIGN.md
4. [ ] 创建示例插件

---

## 使用示例

### 简单命令

```go
func (p *GreeterPlugin) Load(eng *engine.Engine) error {
    m := eng.OnCommand(dto.GroupAtMessageCreate, "/hello")
    m.SetDescription("向用户问好").
      SetUsage("/hello [名字]").
      SetCategory("娱乐").
      Handle(func(ctx *context.Context) error {
          return ctx.Reply("Hello!")
      })
    
    p.AddMatcher(m)
    return nil
}
```

### 复杂命令（使用 Definition）

```go
func (p *SearchPlugin) Load(eng *engine.Engine) error {
    def := &command.Definition{
        Name:        "search",
        Description: "搜索网络内容",
        Usage:       "/search <关键词> [--engine google|bing] [--count 5]",
        Arguments: []*command.Argument{
            {
                Name:        "query",
                Description: "搜索关键词",
                Type:        command.ArgTypeString,
                Required:    true,
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
    }
    
    m := eng.RegisterCommandWithMeta(
        dto.GroupAtMessageCreate,
        def,
        p.handleSearch,
    )
    m.SetCategory("实用工具")
    
    p.AddMatcher(m)
    return nil
}
```

### Help Plugin 配置

```yaml
plugins:
  help:
    enabled: true
    trigger: "/help"
    group_by_plugin: true    # 按插件分组
    group_by_category: false # 按分类分组（与 group_by_plugin 互斥）
    show_aliases: true       # 显示别名
    show_usage: true         # 显示用法
    show_examples: true      # 显示示例
    format: "markdown"       # text 或 markdown
```

### Help 输出示例

```
📖 命令列表

【echo】
  /echo - 回显用户发送的消息
    别名: /repeat, /mirror
    用法: /echo <消息内容>

【search】
  /search - 搜索网络内容
    用法: /search <关键词> [--engine google|bing] [--count 5]

【help】
  /help - 显示所有可用命令的帮助信息
    用法: /help [命令名]

💡 使用 /help <命令> 查看详细信息
```

---

## 最佳实践

### 1. 始终提供描述和用法

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

### 3. 隐藏内部命令

```go
m := eng.OnCommand(dto.GroupAtMessageCreate, "/internal")
m.SetHidden(true)  // 不在帮助中显示
```

### 4. 提供丰富的示例

```go
m.SetMetadata(&engine.MatcherMetadata{
    Examples: []string{
        "/search Go语言",
        "/search Python --engine bing",
        "/search 机器学习 --count 10",
    },
})
```

---

## 总结

通过 **扩展 Matcher 元数据** + **Engine 命令发现 API** + **Help Plugin 自动生成**，我们实现了：

✅ **自动发现**: Help Plugin 自动发现所有注册的命令  
✅ **数据一致**: 元数据存储在 Matcher 中，无需手动同步  
✅ **灵活配置**: 支持多种显示格式和分组方式  
✅ **简单易用**: 插件开发者只需链式调用设置元数据  
✅ **向后兼容**: 不破坏现有代码  

这个方案完美解决了"插件动态注册命令，Help Plugin 如何发现"的问题！
