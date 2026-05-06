# ZeroBot 基因溯源——从借鉴到超越

> Remilia 的架构并非凭空诞生。它的核心模式——引擎 + 匹配器 + 规则 + 处理器——继承自 [wdvxdr1123/ZeroBot](https://github.com/wdvxdr1123/ZeroBot)。
> 本文档详细追溯这些设计基因，对比两套框架在每个组件上的异同，并解释每一次分叉背后的理由。

## 目录

1. [ZeroBot 架构鸟瞰](#1-zerobot-架构鸟瞰)
2. [基因继承图谱——哪些模式来自 ZeroBot](#2-基因继承图谱)
3. [分叉分析——我们在哪里分道扬镳](#3-分叉分析)
4. [逐组件深度对比](#4-逐组件深度对比)
5. [代码演进实例：从 ZeroBot 到 Remilia](#5-代码演进实例)
6. [ZeroBot 有但 Remilia 没有的](#6-zerobot-有但-remilia-没有的)
7. [Remilia 有但 ZeroBot 没有的](#7-remilia-有但-zerobot-没有的)
8. [启示与总结](#8-启示与总结)

---

## 1. ZeroBot 架构鸟瞰

### 1.1 项目定位

ZeroBot 是一个 **Go 语言 OneBot v11 框架**。它的设计哲学是"小而美"：

- 一个文件解决一个问题（bot.go / engine.go / matcher.go / context.go / rules.go ...）
- 通过 `init()` 模式实现"类插件"系统
- 三阶段管道引擎（preHandler → Rules → midHandler → Handler → postHandler）
- 支持正向/反向 WebSocket 和 HTTP 三种通信驱动

### 1.2 核心交互流程

```
OneBot 实现 (go-cqhttp / NapCat)
    │ WebSocket / HTTP
    ▼
┌──────────────┐
│   Driver     │  Connect() → Listen(func([]byte, APICaller))
└──────┬───────┘
       │ event bytes
       ▼
processEventAsync()
    │ json.Unmarshal → Event
    │ 预处理（IsToMe、消息解析）
    ▼
match(ctx, matcherList)  ← 线性扫描，按 Priority 排序
    │
    ├── preHandler (前置过滤)
    ├── Rules       (条件检查)
    ├── midHandler  (限流/反并发)
    ├── Handler     (业务逻辑)
    └── postHandler (清理)
    │
    ▼
ctx.Send() / ctx.CallAction() → Driver.CallAPI()
```

### 1.3 ZeroBot 类型系统（简化版）

```go
// 匹配器——最核心的抽象
type Matcher struct {
    Type     EventType        // 事件类型过滤
    Rules    []Rule           // 规则链
    Handler  Handler          // 最终处理器
    Priority int64            // 优先级（越大越优先）
    Block    bool             // 是否阻止后续匹配器
    Temp     bool             // 临时匹配器（用完即删）
}

type Rule    func(*Ctx) bool  // 规则：决定是否处理
type Handler func(*Ctx)       // 处理器：执行业务逻辑

// 引擎——匹配器容器
type Engine struct {
    mu         sync.Mutex        // 写操作锁
    matchers   []*Matcher        // 全局匹配器列表
    preHandler []Handler         // 前置处理器
    midHandler []Handler         // 中间处理器
    postHandler []Handler        // 后置处理器
}

// 上下文——事件处理的载体
type Ctx struct {
    Event *Event
    State State            // map[string]any
    caller APICaller
}
```

---

## 2. 基因继承图谱

### 2.1 直接继承（概念 + API 均相似）

| ZeroBot | Remilia | 相似度 | 说明 |
|---------|---------|--------|------|
| `Matcher` | `core/engine.Matcher` | 高 | 核心字段完全一致：Type/Rules/Handler/Priority/Block/Temp |
| `Rule func(*Ctx) bool` | `Rule func(*Context) bool` | 高 | 类型签名几乎相同，只是 Ctx 变为泛化 Context |
| `Handler func(*Ctx)` | `Handler func(*Context)` | 高 | 语义完全一致 |
| `Engine.OnXxx()` | `engine.OnEvent()` / `RegisterCommand()` | 中 | ZeroBot 的 `OnMessage/OnNotice` 等工厂方法变为统一的 `OnEvent` |
| `ctx.Send()` | `ctx.Reply()` / `ctx.Send()` | 高 | 同一语义：回复消息 |
| `State map[string]any` | `State map[string]any` | 高 | 命名一致，用途一致 |
| `Priority` 排序 | `Priority` 排序 | 高 | 均按 int64 降序排列 |
| `Block` 阻止传播 | `Block` 阻止传播 | 高 | 语义完全相同 |
| `Temp` 临时匹配器 | `TempManager` | 中 | 概念一致，但实现方式有巨大差异 |
| 三阶段管道 | middleware 三层级 | 中 | 从硬编码三阶段转为可组合的洋葱模型 |

### 2.2 概念继承但实现重写

| ZeroBot | Remilia | 差异 |
|---------|---------|------|
| `Engine.mu sync.Mutex` | `atomic.Value` + 不可变 state | 从有锁变为完全无锁读 |
| `[]*Matcher` 线性扫描 | `commandIndex` + `matcherIndex` + `sortedCache` | 从 O(n) 变为 O(1) + 惰性排序 |
| `processEventAsync` 单路 | `ProcessPlatformEvent` 多路 + `TempManager` 分片 | 从一种事件来源变为多种 |
| `FutureEvent` 基于 channel | `TempManager` 基于队列 | 从 sync 模式变为更通用的队列 |
| `APICaller` 接口 | `platform.Sender` 接口 | 从 OneBot 专有变为平台无关 |

### 2.3 Remilia 独立发明（ZeroBot 没有）

参见第 7 节完整列表。这是 Remilia 从"借鉴"走向"创造"的部分。

---

## 3. 分叉分析

### 3.1 为什么没有直接 fork ZeroBot？

这是第一个需要回答的问题。如果 ZeroBot 那么好，为什么不直接在它上面改？

根本原因：**ZeroBot 的 OneBot 单平台假设太深了**。

```go
// ZeroBot context.go — 平台假设无处不在
type Ctx struct {
    Event  *Event           // Event 直接对应 OneBot JSON 结构
    caller APICaller        // APICaller 直接发送 OneBot API 请求
}

// ZeroBot Event — 字段命名和类型绑定 QQ/OneBot
type Event struct {
    PostType      string         `json:"post_type"`
    MessageType   string         `json:"message_type"`
    NoticeType    string         `json:"notice_type"`
    SubType       string         `json:"sub_type"`
    UserID        int64          `json:"user_id"`       // QQ 号
    GroupID       int64          `json:"group_id"`      // QQ 群号
    Message       any            `json:"message"`       // OneBot 消息格式
    Sender        Sender         `json:"sender"`
    RawMessage    gjson.Result   `json:"-"`
}
```

支持多平台需要在每一层都引入抽象——Event、Sender、Message、API 调用——这相当于重写整个框架。**与其在 ZeroBot 上叠床架屋，不如重写它的核心模式，但采用更通用的抽象**。

### 3.2 关键分叉点 ①：并发模型

| | ZeroBot | Remilia |
|---|---|---|
| 读操作 | sync.Mutex 锁定全部 | 完全无锁（atomic.Value.Load） |
| 写操作 | sync.Mutex | sync.Mutex + 复制-修改-替换 |
| 一致性 | 直接修改 `[]*Matcher` | 创建新 `state` 副本，原子替换 |
| 读性能 | 受锁竞争影响 | 线性可扩展（多核无争用） |

**ZeroBot 的方式**：整个 `match()` 过程持有 `Engine.mu` 锁。当并发事件处理量增加时，锁竞争加剧。

**Decided to diverge**: 因为 Remilia 的目标是高性能（475K msg/s），这在共享锁模型下很难达到。

### 3.3 关键分叉点 ②：路由算法

| | ZeroBot | Remilia |
|---|---|---|
| 事件路由 | 遍历全部 `[]*Matcher` | `matcherIndex[EventType]` 预过滤 |
| 命令路由 | `CommandRule` 与普通 Rule 同等对待 | `commandIndex[cmd][type]` O(1) 直击 |
| 排序时机 | 每次注册都排序 | 惰性排序 + 代际缓存失效 |
| 临时匹配器 | 与永久匹配器在同一个列表 | `TempManager` 独立管理 |

**ZeroBot 的方式**：命令和普通事件走同一套线性扫描逻辑。一个包含 200 个 Matcher 的 Bot 需要遍历 200 次才能找到匹配的处理器。

**Decided to diverge**: Remilia 从实际运营经验中发现命令类事件占总量的 80%+ 且要求低延迟，因此引入了独立的 commandIndex。

### 3.4 关键分叉点 ③：平台抽象

这是 Remilia 与 ZeroBot 最大的结构性差异：

```
ZeroBot:  Bot → Driver → OneBot → QQ
Remilia:  Bot → Adapter → platform.Event → 7+ 平台
```

ZeroBot 的 `driver/` 包只负责通信方式（WS/HTTP），不负责数据模型转换。而 Remilia 的 `platform/` 包既要处理通信，还要将平台专有数据模型转换为 `platform.Event` 统一接口。

### 3.5 关键分叉点 ④：插件系统

```
ZeroBot:  init() → StoreMatcher → 全局 list
Remilia:  Descriptor → SetupContext → DI Container → Engine
```

ZeroBot 的"插件"其实只是"在 init() 中注册 Matcher 的 Go 包"。没有生命周期管理，没有依赖注入，没有热重载。这在小型个人项目中足够，但在需要 25+ 内置模块、企业级部署的场景中就捉襟见肘了。

### 3.6 关键分叉点 ⑤：中间件

```
ZeroBot:  preHandler → Rules → midHandler → Handler → postHandler
          ↑ 硬编码三个阶段，所有 Matcher 共用
          
Remilia:  Middleware chain (Onion Model)
          ↑ 每个 Matcher 拥有独立的链，支持全局/分组/局部三级
          ↑ 支持 RateLimit / CircuitBreaker / Retry / Dedup / Tracing...
```

ZeroBot 的三阶段管道是中间件的雏形，但它是全局的、不可组合的。Remilia 将其抽象为可组合的中间件链，每个中间件可以独立开关、独立配置。

---

## 4. 逐组件深度对比

### 4.1 Engine（引擎）

| 维度 | ZeroBot engine.go | Remilia core/engine/ |
|------|-------------------|---------------------|
| 并发模型 | `sync.Mutex` 保护 `[]*Matcher` | `atomic.Value[*state]` + 不可变状态 |
| 匹配器存储 | 单一全局 `[]*Matcher` | `state.matchers` + `matcherIndex` + `commandIndex` + `groupIndex` |
| 命令支持 | 无独立命令索引 | `commandIndex map[string]map[EventType][]*Matcher` O(1) |
| 临时匹配器 | 在全局列表中标记 `Temp=true` | `TempManager` 独立分片管理 |
| 超时控制 | `MaxProcessTime` + channel select | 同左但支持 `NoTimeout` 标记 |
| 事件预处理 | `preprocessMessageEvent` | 委托给各 platform Adapter 的 `Start()` |
| 死信队列 | 无 | `middleware.DeadLetter` 可选 |
| 组件化 | 无（单文件 ~数百行） | 拆分为 10+ 文件（state/matcher/command/middleware/temp/process...） |

### 4.2 Matcher（匹配器）

```go
// ZeroBot — 所有字段在 Matcher 结构体上
type Matcher struct {
    Type     EventType
    Rules    []Rule
    Handler  Handler
    Priority int64
    Block    bool
    Temp     bool
}

// Remilia — 核心字段一致，扩展了中间件
type Matcher struct {
    Type        EventType
    Rules       []Rule
    Handler     Handler
    Priority    int64
    Block       bool
    Temp        bool
    // Remilia 扩展：
    middlewares []context.Middleware  // 匹配器级中间件
    group       string               // 分组标识
}
```

**Key insight**: Remilia 保留了 ZeroBot Matcher 的全部核心字段，这是最明显的"基因继承"证据。每个字段的语义完全相同，以至于在初期版本中可以逐行对照。

### 4.3 Context（上下文）

```go
// ZeroBot Ctx
type Ctx struct {
    Event  *Event
    State  State
    caller APICaller
}

// Remilia V1 — 几乎逐行复制
type Context struct {
    event *dto.Payload     // ← Event 的类型从 ZeroBot.Event 变成了 dto.Payload
    api   openapi.OpenAPI  // ← APICaller 变成了具体的 openapi.OpenAPI
    state State
}

// Remilia V3 (current) — 完全抽象
type Context struct {
    platformEvent  platform.Event    // 平台无关事件
    platformSender platform.Sender   // 平台无关发送
    botID          string
    state          State
    // ...
}
```

这里的演进路径清晰地展示了"从具体到抽象"的过程：开始时 Context 只是把 ZeroBot Ctx 的 Event 换成了 QQ 的 `dto.Payload`，API 调用换成了 `openapi.OpenAPI`。随着多平台需求出现，才逐步抽象为 `platform.Event` 和 `platform.Sender`。

### 4.4 Rule（规则）

```go
// ZeroBot — 内置规则
func PrefixRule(prefix string) Rule  // 前缀匹配
func CommandRule(cmd string) Rule    // 命令精确匹配
func RegexRule(pattern string) Rule  // 正则匹配
func KeywordRule(keywords ...string) Rule // 关键词
func OnlyGroup() Rule                // 仅群聊
func OnlyPrivate() Rule              // 仅私聊
func OnlyAdmin(caller ...) Rule      // 仅管理员
func CheckSuperAdmin(caller ...)     // 超级用户检查

// Remilia — 同样的内置规则集
func PrefixRule(prefix string) Rule
func CommandRule(cmd string) Rule        // 概念相同，但底层有 commandIndex 优化
func RegexRule(pattern string) Rule
func OnlyGroup() Rule
func OnlyPrivate() Rule
func OnlyAdmin() Rule
// Remilia 新增:
func WithPermission(resource, action string) Rule  // RBAC 权限规则
func WithCooldown(d time.Duration) Rule              // 冷却规则
func WithTracing(spanName string) Rule               // 追踪规则
```

Rule 系统的继承是最完整的——两个框架的 Rule 类型签名完全一致，内置规则集高度重叠。Remilia 在之上扩展了 RBAC 权限、冷却、追踪等企业级规则。

### 4.5 事件处理流程对比

```
ZeroBot:
  processEventAsync
    → json.Unmarshal → Event
    → 预处理（IsToMe、ParseMessage）
    → match(ctx, matcherList)
      → for _, m := range matchers {
          if m.Type matches && all rules pass {
            m.Handler(ctx)
            if m.Block { break }
          }
        }

Remilia:
  ProcessPlatformEvent
    → 由 Adapter 将平台事件转为 platform.Event（平台各自实现）
    → eventID / shard / dedup 检查
    → engine.processEventContext(ctx)
      → checkShutdown
      → commandIndex lookup (if command event)
      → matcherIndex lookup (if normal event)
      → 6-way merge sort
      → for each matcher:
          middleware chain (global → group → local)
          → Rules → Handler
          → if Block { break }
      → TempManager cleanup expired
```

关键差异：
1. ZeroBot 收到的是字节流（JSON），Remilia 收到的是抽象事件（platform.Event）
2. ZeroBot 线性扫描，Remilia 索引路由
3. ZeroBot 的中间件是全局固定三阶段，Remilia 中间件是灵活的链
4. Remilia 有 TempManager 独立清理，ZeroBot 的临时匹配器混在全局列表中

### 4.6 平台驱动 vs 平台适配器

```go
// ZeroBot Driver — 仅处理通信协议
type Driver interface {
    Connect() error
    Listen(func([]byte, APICaller)) error
}

// 三个实现:
// - WSClient:  正向 WebSocket（客户端主动连 OneBot）
// - WSServer:  反向 WebSocket（服务端等待 OneBot 连入）
// - HTTP:      HTTP 服务器 + HTTP 客户端

// Remilia Adapter — 处理通信 + 数据模型转换
type Adapter interface {
    Platform() string
    Start(ctx context.Context, handler func(Event)) error
    Stop(ctx context.Context) error
    Sender() Sender
    Capabilities() Capabilities
    IsRunning() bool
}
```

ZeroBot 的 Driver 只解决"怎么收到事件字节"的问题，不关心字节的含义。Remilia 的 Adapter 还要负责"字节到 platform.Event 的转换"，以及"platform.SendRequest 到平台 API 的转换"。

### 4.7 事件等待（FutureEvent vs TempManager）

```go
// ZeroBot FutureEvent — 基于 channel
func (c *Ctx) FutureEvent(eventType, subType string) *FutureEvent {
    done := make(chan struct{})
    m := &Matcher{Temp: true, ...}
    // 匹配时向 ch 发送 event，通过 channel 同步返回
}

// Remilia TempManager — 基于队列
type TempManager struct {
    temps    []*tempEntry    // 临时匹配器条目
    maxTemps int
}

type tempEntry struct {
    Matcher  *Matcher
    deadline time.Time       // 过期时间
    maxMatch int             // 最大匹配次数
    count    int64           // 已匹配次数
}
```

ZeroBot 的 FutureEvent 使用 channel 进行同步，适合"等待一次"的场景。Remilia 的 TempManager 使用队列 + 超时清理器，适合"等待 N 次或超时"的场景，更灵活且不阻塞事件处理 goroutine。

---

## 5. 代码演进实例

### 5.1 Event 处理循环

```go
// ─── ZeroBot (bot.go:processEventAsync) ───
func (bot *ZeroBot) processEventAsync(data []byte) {
    var event Event
    json.Unmarshal(data, &event)
    preprocessMessageEvent(&event)
    ctx := &Ctx{Event: &event, caller: caller}
    // 计时 + 日志
    bot.match(ctx)
}

func (bot *ZeroBot) match(ctx *Ctx) {
    bot.engine.mu.Lock()
    defer bot.engine.mu.Unlock()
    for _, m := range bot.engine.matchers {
        if !matchType(m, ctx) { continue }
        if !matchRules(m, ctx) { continue }
        m.Handler(ctx)
        if m.Block { break }
    }
}

// ─── Remilia V1（初始 — 仍然是线性扫描 + 锁）───
func (e *Engine) process(ctx *eventctx.Context) {
    e.mu.RLock()     // 从 Mutex 升级为 RWMutex
    defer e.mu.RUnlock()
    for _, m := range e.matchers {
        if !matchType(m, ctx) { continue }
        if !matchRules(m, ctx) { continue }
        m.Handler(ctx)
        if m.Block { break }
    }
}

// ─── Remilia V3（当前 — COW + 索引 + 中间件链）───
func (e *Engine) processEventContext(ctx *eventctx.Context) {
    if e.shutdown.Load() { return }

    state := e.state.Load()       // ← 完全无锁
    // 命令索引 O(1) 查找
    if strings.HasPrefix(msg, "/") {
        if matchers := lookupCommandIndex(state, cmd, evType); len(matchers) > 0 {
            e.executeMatchers(ctx, matchers)
            return
        }
    }
    // 普通事件索引查找 + 6 路合并排序
    matchers := e.mergeSortedMatchers(state, evType)
    e.executeMatchers(ctx, matchers)
}

func (e *Engine) executeMatchers(ctx *eventctx.Context, matchers []*Matcher) {
    for _, m := range matchers {
        if !matchType(m, ctx) { continue }
        // 中间件链（全局 → 分组 → 局部）
        chain := m.ensureChain(globalSnap, globalGen, groupSnap, groupGen)
        chain.Execute(ctx)   // ← 洋葱模型
        if ctx.IsAborted() || m.Block { break }
    }
}
```

### 5.2 Matcher 注册

```go
// ─── ZeroBot — init() 全局注册 ───
func init() {
    engine := zero.NewEngine()
    engine.OnCommand("help").Handle(func(ctx *zero.Ctx) {
        ctx.Send("帮助信息")
    })
    zero.StoreMatcher(engine.Matchers()...)
}

// ─── Remilia V1 — 同样的 init() 模式 ───
func init() {
    engine := remilia.NewEngine()
    engine.OnCommand("help", remilia.OnlyGroup).Handle(func(ctx *remilia.Context) {
        ctx.Reply("帮助信息")
    })
    // StoreMatcher 概念完全来自 ZeroBot
    remilia.Store(engine.Matchers()...)
}

// ─── Remilia V3 — 插件描述符 ───
func New() *plugin.Descriptor {
    return &plugin.Descriptor{
        Name: "help",
        Setup: func(ctx *plugin.SetupContext) (any, error) {
            ctx.Reg.RegisterCommand(groupEvent, "/help").
                Handle(func(ctx *eventctx.Context) {
                    ctx.Reply("帮助信息")
                })
            return nil, nil
        },
    }
}
```

### 5.3 Context 获取/释放

```go
// ─── ZeroBot — 每次都 new ───
ctx := &Ctx{Event: &event, caller: caller}

// ─── Remilia V1 — 开始用 Pool（第一次进化）───
ctx := contextPool.Get().(*Context)
ctx.event = event

// ─── Remilia V3 — Pool + 平台抽象 ───
ctx := NewContextFromEvent(event, sender)

// ─── Remilia V4 — 去池化，新鲜分配 ───
func NewContextFromEvent(event platform.Event, sender platform.Sender) *Context {
    return &Context{platformEvent: event, platformSender: sender}
}
// 去池化原因：池化 + go 关键字 = UAF。474k msg/s 下 1.8% CPU 开销可接受
```

---

## 6. ZeroBot 有但 Remilia 没有的

| ZeroBot 特性 | Remilia 情况 | 原因 |
|-------------|-------------|------|
| **CQ 码解析/编码** | `platform/onebot/` 适配器内有 | OneBot 专有，不放在框架核心 |
| **Shell 命令解析**（ParseShell + flag.FlagSet） | 无直接等价 | Remilia 的 `command/` 包使用不同的命令模型 |
| **消息 ID 双重格式**（int64 + string 互转） | 无 | Remilia 使用 `platform.MessageID` 抽象 |
| **内置 GIF 动图支持** | `infra/gif/` 目录已创建 | 基于 `image/gif`，非定制方案 |
| **HMAC-SHA1 签名验证** | 无 | 由 HTTP Server 层处理，非框架核心 |
| **`extension/kv` LevelDB 存储** | `infra/storage/` 使用 GORM | 技术栈不同 |
| **eventRing 环形缓冲区** | 无 | Remilia 用 `middleware.Dedup` 取代 |
| **无 base64 自动转码** | 自动 base64 发送 | 可选配置，非默认 |
| **onebot 扩展 API**（NapCat/NapNeko） | 无 | Remilia 不绑定 OneBot |

这些差异的核心原因：**ZeroBot 是 OneBot 框架，功能围绕 OneBot 协议设计；Remilia 是多平台框架，不假设底层协议**。

---

## 7. Remilia 有但 ZeroBot 没有的

| Remilia 特性 | ZeroBot 状态 | 创新层级 |
|-------------|-------------|---------|
| COW 无锁引擎（atomic.Value + 不可变 state） | sync.Mutex | **架构创新** |
| 多平台适配器体系（QQ/Discord/Telegram/WeChat/Satori/Milky） | 仅 QQ/OneBot | **架构创新** |
| 插件系统 v2（Descriptor + DI Container + BlueGreen 重载） | init() 全局注册 | **架构创新** |
| 生命周期管理（Component + Manager + 双层 Context） | 无 | **架构创新** |
| 命令 Trie 树 + commandIndex O(1) 前缀匹配 | 线性扫描 O(n) | **性能创新** |
| 6 路合并匹配器排序 + 惰性缓存 | 每次注册排序 | **性能创新** |
| TempManager 独立分片管理 | 混入全局列表 | **性能创新** |
| 企业级中间件（RBAC/熔断/限流/重试/DLQ/自适应/追踪） | pre/mid/post 三阶段 | **能力扩展** |
| Prometheus 指标 + OpenTelemetry 追踪 | 无 | **能力扩展** |
| 配置热重载（fsnotify + Bridge 推模式） | 无 | **能力扩展** |
| infra 基础设施（atomic/pool/health/tracing/textimage/dlq...） | 仅有 utils/helper | **能力扩展** |
| Context Pool（已废弃 V4）→ 新鲜分配 | 每次都 new | **安全性优先 → UAF 消除。1.8% 开销可接受** |
| zerolog 零分配日志 | 普通日志 | **性能优化** |
| 500+ 测试文件，>90% 关键路径覆盖率 | 单一 all_test.go | **工程质量** |
| BlueGreen 热重载 + InPlace 热重载 | unload-load 单策略 | **运维能力** |

---

## 8. 实测性能对比

> 以下数据来自 AMD Ryzen 7 5800H, Windows, Go 1.26。  
> ZeroBot 侧使用 `go:linkname` 调用内部 `processEventAsync` 以绕过 `Run()` 全局状态问题（见下文 8.6 节）。  
> 等待 ZeroBot 异步 handler 完成使用纯 spin-wait（`runtime.Gosched()` + 原子 load，无 `time.Sleep`）。  
> 测试代码位于 `tests/benchmark/zerobot_comparison/bench_test.go.bak`，重命名为 `.bak` 后缀以避免编译进框架。  
> 用户运行前需执行 `go get github.com/wdvxdr1123/ZeroBot@latest` 添加依赖。

### 8.1 命令路由

ZeroBot 管线：JSON unmarshal → 事件预处理 → matcherLock → 线性扫描 → goroutine-per-rule → handler  
Remilia 管线：context pool 获取 → commandIndex O(1) 查找 → handler

| Matcher 数 | ZeroBot (ns/op) | 分配 | Remilia (ns/op) | 分配 | 差距 |
|-----------|----------------|------|----------------|------|------|
| 10        | 8,300          | 4.9KB, 62 allocs | **660** | 208B, 5 allocs | **12x** |
| 50        | 16,300         | 14KB, 182 allocs | **410** | 208B, 5 allocs | **40x** |
| 200       | 40,600         | 49KB, 632 allocs | **400** | 208B, 5 allocs | **101x** |
| 1000      | 178,000        | 237KB, 3028 allocs | **390** | 208B, 5 allocs | **456x** |

Remilia 的 `commandIndex` 是 **O(1)**，路由时间与 Matcher 数无关。ZeroBot 的 `CommandRule` 是 **O(n)**，每个额外 Matcher 增加 ~170ns。ZeroBot 的事件处理分配也随 Matcher 数线性增长，而 Remilia 恒定 208B/5 allocs。

### 8.2 普通事件路由

ZeroBot 管线如前，但无命令前缀优化，需线性扫描所有 matcher。Remilia 使用 `matcherIndex` 按 EventType 预过滤 + 排序缓存。

| Matcher 数 | ZeroBot (ns/op) | 分配 | Remilia (ns/op) | 分配 | 差距 |
|-----------|----------------|------|----------------|------|------|
| 10        | 6,600          | 4.5KB, 60 allocs | **470** | 208B, 5 allocs | **14x** |
| 50        | 15,700         | 14KB, 180 allocs | **370** | 208B, 5 allocs | **42x** |
| 200       | 35,500         | 49KB, 630 allocs | **370** | 208B, 5 allocs | **96x** |
| 1000      | 166,000        | 237KB, 3030 allocs | **345** | 208B, 5 allocs | **481x** |

### 8.3 吞吐量

| 场景 | ZeroBot | Remilia | 差距 |
|------|---------|---------|------|
| 10 Matcher | 146K ev/s | **2.0M ev/s** | **13.7x** |
| 200 Matcher | 24K ev/s | **2.38M ev/s** | **99x** |

### 8.4 注册 Matcher（冷路径）

| Matcher 数 | ZeroBot | Remilia (COW) | 差距 |
|-----------|---------|---------------|------|
| 10        | 2.5 µs, 2.5KB | 34 µs, 28KB | ZeroBot 快 13x |
| 50        | 19 µs, 12KB | 113 µs, 129KB | ZeroBot 快 6x |
| 200       | 150 µs, 48KB | 760 µs, 1.1MB | ZeroBot 快 5x |

ZeroBot 的 append+sort 比 COW 拷贝轻量，但注册是冷路径，不影响运行时。

### 8.5 ZeroBot 性能瓶颈分析

| 瓶颈 | 根因 | 量化影响 |
|------|------|---------|
| goroutine-per-rule | 每个 rule/handler 都在独立 goroutine 中执行 + channel 同步 | 200 Matcher = 200+ goroutines/事件，调度 + channel 开销占 ~15µs |
| 全局锁 | `processEventAsync` 中 `matcherLock` | 高并发时所有事件串行化 |
| JSON + gjson | 每个事件 `json.Unmarshal` + `gjson.Parse` | ~1µs 固定开销 |
| 消息预处理 | `ParseMessage` + `processAt` + 日志 | 额外分配和 CPU |
| **分配量级** | 每次事件都 new Ctx + goroutine stack + channel buffer | 1000 Matcher 时每事件 237KB！GC 压力巨大 |

### 8.6 编写跨框架 Benchmark 的陷阱

#### 陷阱 1：ZeroBot Run() 全局状态

`Run()` 只能用一次，因为包级 `isrunning` 原子变量无法重置：

```go
func Run(op *Config) {
    if !atomic.CompareAndSwapUintptr(&isrunning, 0, 1) {
        log.Warnln("[bot] 已忽略重复调用的 Run")
        return
    }
    runinit(op)
    // ...
}
```

Go testing framework 的 calibration 阶段会多次调用 benchmark 函数，每次都需要干净的全局状态。但 ZeroBot 的全局 Matcher 列表、BotConfig 无法在调用间重置，导致竞态和卡死。

**解决方案**：通过 `go:linkname` 绕过 `Run()`，直接调用 `processEventAsync`，手动设置 `BotConfig`。

#### 陷阱 2：异步 vs 同步处理

ZeroBot 的 `processEventAsync` 是异步的——`go match(...)` 后立即返回。必须等待 handler 执行完成才能测量。用 channel 同步会引入 channel 开销；用 `time.Sleep` 轮询会引入 sleep 延迟（~1-2µs/次）。**最终方案**：纯 spin-wait（`runtime.Gosched()` + 原子 load），将等待开销降至 ~200-500ns/次，对结果影响 <1%。

### 8.7 Benchmark 公平性说明

| 关切 | 实际影响 | 结论 |
|------|---------|------|
| ZeroBot 吃 JSON vs Remilia 吃 struct | 架构固有差异——ZeroBot 是一体化 OneBot 框架，输入就是 JSON；Remilia 是多平台框架，Adapter 层负责将平台事件转为 platform.Event | ✅ **公平**——测的是"各框架下用户实际付出的总开销"，包含各自架构的必要处理 |
| `go:linkname` 绕过 `Run()` | 跳过了 event ring 等可选环节，但核心管线（JSON parse → preprocess → match → handler）完全一致。实际上对 ZeroBot 有利（少了 ring buffer I/O） | ✅ **可接受**——偏差方向偏向 ZeroBot |
| 纯 spin-wait 等待异步 handler | 每次迭代 ~200-500ns，相对 ZeroBot 的 6,000-178,000ns/op 偏差 <5% | ✅ **可忽略** |
| 空 Handler | 双方都是空函数 | ✅ **公平**——只测量框架开销，不测量业务逻辑 |
| logrus 被 suppress | ZeroBot 内部有大量 log 调用，suppress 后节省了 I/O 时间 | ✅ **偏向 ZeroBot**——不 suppress 差距更大 |
| 多 worker 并发不可比 | ZeroBot 的 `matcherLock` + goroutine-per-rule 设计导致 4+ worker 时 benchmark 无法收敛（超时）。仅 1 worker 可比 | ⚠️ **部分不可比**——但 1 worker 下已测出 ~90x 差距。在多核场景下 Remilia 的"无锁读"优势会更显著 |

**综合结论**：benchmark 在可比较的范围内是公平的。存在的偏差（3-5%）相对观测到的信号（12-480x）小了两个数量级。如果存在系统性误差，方向是**偏向 ZeroBot**（suppress log、跳过 ring buffer），意味着真实差距可能更大。

---

## 9. 启示与总结

### 9.1 我们保留了 ZeroBot 的什么？

**设计理念**：
- Matcher 作为第一等公民（而非 Router 或 Controller）
- Rule 作为函数式过滤器（组合优于继承）
- Handler 就是普通函数（不强制框架类型）
- 事件驱动 + 管道处理

**API 风格**：
- `OnXxx().Handle(func)` 的链式调用
- Rule 作为闭包函数传递
- Context 作为事件载体

这些模式**经受住了时间考验**。从最初的 ZeroBot 借鉴，到 Remilia V3，它们一直被保留。

### 9.2 我们改变了什么？

1. **并发模型** — 从"有锁"到"无锁"：这是性能质变的关键
2. **路由算法** — 从"线性"到"索引"：这是规模化的基础
3. **平台模型** — 从"单平台"到"多平台"：这是定位的彻底转变
4. **插件系统** — 从"init()"到"描述符"：这是从"脚本"到"工程"的跨越
5. **中间件** — 从"三阶段"到"洋葱模型"：这是灵活性的飞跃
6. **生命周期** — 从"无"到"完整管理"：这是生产化的必要条件

### 9.3 反思：我们是否应该保留更多 ZeroBot 的简洁性？

ZeroBot 最大的优点是**简单**——一个新用户可以在 5 分钟内理解整个框架。Remilia 在进化的过程中不可避免地增加了复杂度。这是否值得？

答案是：**对框架的目标用户群来说，值得**。

- ZeroBot 面向的是"个人开发者写一个 QQ Bot"
- Remilia 面向的是"团队构建多平台 Bot 基础设施"

这是两个不同的市场。ZeroBot 的简洁性是它的核心竞争力；Remilia 的完整性和可扩展性是它的核心竞争力。

### 9.4 给后来者的建议

1. **学习 ZeroBot** 是理解 Remilia 架构的最佳起点——读懂了 ZeroBot 的 matcher.go、engine.go、context.go，就理解了 Remilia 的一半设计意图。

2. **保持继承部分的清晰可辨**——Remilia 中来自 ZeroBot 的代码（Matcher 核心字段、Rule 类型、Handler 签名）应该保持稳定，不要随意改动，因为它们是框架的"公理"。

3. **分叉要果断**——当确定 ZeroBot 的模式不再适用时，不要犹豫去重写。Remilia 的 COW 引擎、多平台抽象、插件 v2 都是从零开始重写的，每一次重写都带来了质的飞跃。

### 9.5 相关文档

- [`00-evolution.md`](00-evolution.md) — 架构演进之路（含 ZeroBot 启蒙阶段）
- [`../06-archived/comparison-zerobot-floattech.md`](../06-archived/comparison-zerobot-floattech.md) — 框架层与 FloatTech 系列库对比
- [`../06-archived/comparison-zerobotplugin.md`](../06-archived/comparison-zerobotplugin.md) — 业务插件层对比
- [wdvxdr1123/ZeroBot GitHub](https://github.com/wdvxdr1123/ZeroBot) — 源头仓库
