# 命令系统——双索引 O(1) 路由 + 前缀树补全

> **ZeroBot 基因**：ZeroBot 通过 `CommandRule("cmd")` 将命令匹配混入普通 Rule 中，与其他 Matcher 一起线性扫描。Remilia 引入 `commandIndex` 独立索引 + Trie 前缀树，将命令路由从 O(n) 降至 O(1)。参阅 [`11-zerobot-inspiration.md`](11-zerobot-inspiration.md#44-rule规则)。

## 架构概览

命令系统是框架中最频繁使用的功能。Remilia 使用**双索引**策略：

```
命令注册 → command.Trie（前缀树，用于模糊搜索和补全）
         → engine.commandIndex（hash map，用于 O(1) 路由）
         → engine.matcherIndex（事件类型索引，用于全量排序）
```

三个索引各司其职：

| 索引 | 位置 | 用途 | 时间复杂度 |
|------|------|------|-----------|
| `commandIndex` | Engine state | 事件热路径精确路由 | **O(1)** |
| `command.Trie` | command 包 | 命令补全、模糊搜索 | O(k) k=命令长度 |
| `matcherIndex` | Engine state | 非命令事件路由 | O(n) n=匹配器数 |

## 1. commandIndex：O(1) 命令路由

```go
// state.commandIndex 结构
commandIndex map[string]map[EventType][]*Matcher
//  命令名        事件类型      匹配器列表
//  "/help"  →   ""        →  [matchers]
//                "C2C"     →  [matchers]
```

当事件消息以触发前缀（如 `/`）开头时，提取命令名直接从 `commandIndex` 获取候选匹配器：

```go
func processMessage(ctx *context.Context) {
    msg := ctx.GetMessageContent()

    // 检测命令前缀
    if len(msg) > 0 && msg[0] == '/' {
        cmdName, args := parseCommand(msg)
        // O(1) 查找
        if cmdMap, ok := state.commandIndex[cmdName]; ok {
            matchers := mergeCmdMatchers(cmdMap, eventType)
            // ...
        }
    }
}
```

这个优化使得**命令路由延迟完全不受匹配器总数影响**——即使有 10 万个匹配器，/help 仍然在一两微秒内完成路由。

### 命令索引维护

```go
func (s *state) withAddedMatcher(m *Matcher) *state {
    if m.commandIndexed.Load() {
        cmdName := extractCmdName(m)
        newCommandIndex := copyCommandIndex(s.commandIndex)
        // 插入到特定事件类型下
        newCommandIndex[cmdName][m.EventType] =
            append(newCommandIndex[cmdName][m.EventType], m)
        // ...
    }
}
```

`commandIndexed` 标志由 `OnCommand` 注册时设置，只有命令类匹配器进入此索引。

## 2. Trie 前缀树：命令发现与补全

```go
type TrieNode struct {
    children map[rune]*TrieNode
    commands []*Meta              // 该节点关联的命令
    isEnd    bool
}

type Trie struct {
    root *TrieNode
    mu   sync.RWMutex
}

type Meta struct {
    Name        string
    Description string
    Usage       string
    Aliases     []string
    Category    string
    Plugin      string
}
```

### 插入

```go
func (t *Trie) Insert(name string, meta *Meta) {
    t.mu.Lock()
    defer t.mu.Unlock()

    node := t.root
    for _, r := range []rune(name) {
        if node.children[r] == nil {
            node.children[r] = &TrieNode{
                children: make(map[rune]*TrieNode),
                commands: make([]*Meta, 0),
            }
        }
        node = node.children[r]
    }
    node.commands = append(node.commands, meta)
    node.isEnd = true
}
```

### 前缀搜索（命令补全）

```go
func (t *Trie) Search(prefix string) []*Meta {
    t.mu.RLock()
    defer t.mu.RUnlock()

    node := t.root
    for _, r := range []rune(prefix) {
        if node.children[r] == nil {
            return nil  // 无匹配
        }
        node = node.children[r]
    }

    // DFS 收集所有终端节点命令
    var result []*Meta
    seen := make(map[*Meta]bool)
    t.collectTerminalCommands(node, seen, &result)
    return result
}
```

输入 `/he` 时，通过 Trie Search 可以即时返回 `/help`、`/hello`、`/heartbeat` 等候选命令。

### 删除与修剪

```go
func (t *Trie) Remove(name string, meta *Meta) {
    t.mu.Lock()
    defer t.mu.Unlock()

    node := t.root
    path := make([]*TrieNode, 0, len(name)+1)
    path = append(path, node)

    for _, r := range []rune(name) {
        if node.children[r] == nil { return }
        node = node.children[r]
        path = append(path, node)
    }

    node.commands = removeCommandFromSlice(node.commands, meta)
    if len(node.commands) == 0 {
        node.isEnd = false
    }

    // 从叶到根修剪空节点
    for i := len(path) - 1; i > 0; i-- {
        n := path[i]
        if !n.isEnd && len(n.children) == 0 && len(n.commands) == 0 {
            delete(path[i-1].children, runes[i-1])
        } else {
            break
        }
    }
}
```

自动修剪确保无命令的分支被及时回收，避免内存泄漏。

## 3. 命令注册 API

```go
// Engine 层
func (e *Engine) OnCommand(eventType EventType, cmdPattern string, extraRules ...context.Rule) *Matcher {
    // 1. 创建匹配器，标记 commandIndexed
    // 2. 注册到 engine state（自动进 commandIndex）
    // 3. 同步到外部队列 command.Trie
    // 4. 绑定别名自动注册
}
```

### 别名支持

```go
type Definition struct {
    Name        string
    Aliases     []string    // 别名列表
    Description string
    Usage       string
    Category    string
    // ...
}
```

别名注册通过 `AliasRegistrar` 回调自动完成：

```go
primary.SetAliasRegistrar(func(def *command.Definition, h context.Handler) {
    for _, alias := range def.Aliases {
        // 为每个别名创建新的匹配器（共享 handler）
        // 注册到 commandIndex 和 Trie
    }
})
```

## 4. 命令信息缓存

```go
type CommandInfo struct {
    Command     string
    Description string
    Usage       string
    Aliases     []string
    Category    string
    Examples    []string
    Permissions []string
    Plugin      string
    Source      string
    EventType   EventType
}
```

引擎维护 `commandInfoCache` 和 `commandListCache` 两个缓存：

```go
type state struct {
    commandInfoCache map[string]*CommandInfo  // 单命令信息
    commandListCache []CommandInfo            // 展开后的只读切片
    commandListVer   int64                    // 版本号，缓存失效检测
}
```

`commandListCache` 用于 `GetAllCommands()` 的 O(1) 返回，避免每次调用重新分配切片并遍历 map。

```go
func (e *Engine) GetAllCommands() []CommandInfo {
    state := e.state.Load()
    if state.commandListCache != nil {
        return state.commandListCache  // 直接返回缓存（零分配）
    }
    // 首次调用时构建
}
```

## 5. Fuzz 测试保障

命令系统的解析器（`parser.go`）有专门的 Fuzz 测试：

```go
func FuzzParser(f *testing.F) {
    f.Add("/test arg1 arg2")
    f.Add("!command --flag value")
    f.Add("   ")
    f.Add("/")
    f.Fuzz(func(t *testing.T, input string) {
        result := Parse(input)
        // 永不 panic、永不返回不一致的结果
    })
}
```

确保恶意输入不会造成解析器崩溃。

## 迭代过程

### V0：引擎内嵌的简单命令提取

初始版本的命令系统极简单——直接在 Engine 的 `processEvent` 中硬编码匹配：

```go
// V0 代码 — 在 engine.go 中硬编码
func (e *Engine) processEvent(ctx *Context) {
    msg := extractContent(ctx)
    for _, m := range e.state.Load().matchers {
        // 每个匹配器逐一检查规则，命令和普通规则没有区分
        if matchesAllRules(m, ctx) {
            // ...
        }
    }
}
```

命令信息（`CommandInfo`）也没有独立的缓存，每次 `Help` 插件调用 `GetAllCommands()` 都需要遍历所有匹配器重新构建。

**问题**：
- 命令事件和普通事件没有区分——/help 跟一条普通群消息的处理路径完全一样
- 没有 commandIndex，每次都需要遍历所有匹配器
- 命令信息没有缓存——Help 命令每次都重新构建命令列表
- 没有命令补全搜索

### V1：commandIndex 分离

引入 `commandIndex` 作为硬哈希表，实现 O(1) 命令路由：

```go
// V1 — commandIndex
type state struct {
    // 新增
    commandIndex map[string]map[string][]*Matcher
    // 命令名       事件类型     匹配器列表
    // "/help"  →   ""        →  [matchers]
}
```

注册时，如果规则中包含 `OnCommand("/help")`，则自动添加到 `commandIndex`：

```go
// RegisterCommand 自动添加 commandIndex
func (e *Engine) OnCommand(eventType string, cmd string, rules ...Rule) *Matcher {
    m := &Matcher{
        Rules:     append([]Rule{OnCommand(cmd)}, rules...),
        EventType: eventType,
    }
    m.commandIndexed.Store(true)
    e.registerMatcher(m)
    return m
}
```

**效果**：命令路由从 O(n) 降为 O(1)，1K 匹配器场景下延迟从 5μs 降为 ~50ns（map 查找）。

### V2：Trie 树 + 命令信息缓存

```go
// V2 — command/trie.go（独立包）
type Trie struct {
    root *TrieNode
    mu   sync.RWMutex
}

type Meta struct {
    Name        string
    Description string
    Usage       string
    Aliases     []string
    Category    string
    Plugin      string
}
```

Trie 树的引入解决了两个问题：
1. **前缀搜索**：用户输入 `/he` 时，Trie 即时返回 `/help`、`/hello`、`/heartbeat` 等候选
2. **命令发现**：Help 插件遍历 Trie 获取所有命令，无需遍历 Engine 的匹配器

命令信息也引入了缓存层：

```go
// V2 — 命令信息缓存
type state struct {
    commandInfoCache map[string]*CommandInfo   // 单命令
    commandListCache []CommandInfo             // 展开的只读切片
    commandListVer   int64                     // 缓存失效版本
}

func (e *Engine) GetAllCommands() []CommandInfo {
    state := e.state.Load()
    if state.commandListCache != nil {
        return state.commandListCache  // 零分配返回
    }
    // 首次构建
}
```

`commandListCache` 的设计原因是：Help 命令可能被频繁触发，每次遍历 map 构建切片在匹配器较多时消耗可观。缓存后首次调用构建，后续 O(1) 返回。

### V3：别名自动注册

```go
// V3 — 别名自动注册（engine/engine_command.go）
primary.SetAliasRegistrar(func(def *command.Definition, h context.Handler) {
    for _, alias := range def.Aliases {
        // 为每个别名创建匹配器，共享 handler
        aliasMatcher := &Matcher{
            EventType: primary.EventType,
            Rules:     []Rule{OnCommand(alias)},
            Handler:   h,
            Source:    primary.Source,
        }
        aliasMatcher.commandIndexed.Store(true)
        e.registerMatcher(aliasMatcher)
    }
})
```

之前的版本需要插件开发者手动为别名注册匹配器——这导致了很多遗漏（Help 中显示了别名，但实际输入别名没反应）。V3 改为引擎层自动处理。

### V4：解析器改进——Fuzz 测试 + 自定义前缀

早期解析器在恶意输入下有 panic 风险：

```go
// V0 解析器 — 未考虑边缘情况
func Parse(input string) *ParsedCommand {
    parts := strings.Split(input, " ")
    if len(parts) == 0 {
        return nil  // 空的字符串返回 nil，但调用方没检查
    }
    cmd := parts[0][1:] // 假设 parts[0] 长度至少为 2
    // ...
}
```

V4 使用 Fuzz 测试覆盖所有边缘情况：

```go
// V4 — Fuzz 测试
func FuzzParser(f *testing.F) {
    f.Add("/test arg1 arg2")
    f.Add("!command --flag value")
    f.Add("   ")          // 纯空白
    f.Add("/")            // 只有前缀
    f.Add("")             // 空字符串
    f.Fuzz(func(t *testing.T, input string) {
        result := Parse(input)
        // 永不 panic、永不返回不一致的结果
    })
}
```

同时支持自定义命令前缀（不限于 `/`），通过 `triggerPrefix` 字段实现：

```go
// V4 — 自定义前缀
m.triggerPrefix = string(trimmedPattern[0])
cmdName := trimmedPattern[1:]
```

这样 `!help`、`.help`、`#help` 等都可以作为命令前缀。

## 迭代历程

| 版本 | 核心变化 | 延迟 | 解决的问题 |
|------|---------|------|-----------|
| V0 | 引擎内嵌，命令跟普通事件混在一起 | O(n) 遍历 | 快速实现 |
| V1 | commandIndex 硬哈希表 | O(1) ~50ns | 命令事件从 1K 匹配器中 O(1) 路由 |
| V2 | Trie 树 + 命令信息缓存 | ~500ns 搜索 + 零分配 Get | 命令补全 + 频繁 Help 调用优化 |
| V3 | 别名自动注册 | — | 消除手动注册遗漏 |
| V4（当前）| Fuzz 测试 + 自定义前缀 | — | 健壮性 + 灵活性 |

## 性能基准

```go
// BenchmarkCommandParsing
// 命令解析 ~1-2 μs/op
// Trie 搜索 ~500 ns/op （前缀长度 3-5）
// commandIndex 查找 ~50 ns/op （一次 map 查找）
```

| 操作 | 延迟 | 说明 |
|------|------|------|
| `OnCommand` 注册 | ~1 μs | 含索引更新 |
| 命令事件路由 | ~50 ns | commandIndex O(1) |
| 前缀补全 | ~500 ns | Trie DFS |
| GetAllCommands | 0 ns（缓存命中） | 预计算列表缓存 |
