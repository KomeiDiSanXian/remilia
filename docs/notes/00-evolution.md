# 架构演进之路——从 QQ 专用 Bot 到通用框架

> 这个框架不是一天建成的。从 2025 年 12 月的第一个 commit 到今天，经历了数百次重构。
> 回顾演进路径，可以看到每个阶段解决的核心问题以及背后的设计决策。
> 而一切的起点，来自对 [wdvxdr1123/ZeroBot](https://github.com/wdvxdr1123/ZeroBot) 的研读与借鉴。

## 第零阶段：ZeroBot 启蒙——从使用者到创作者

**关键词**：学习借鉴、模式识别、意识到天花板

### 起点：为什么要看 ZeroBot？

Remilia 最初的作者是 ZeroBot 的深度用户。ZeroBot 是一个优雅的 OneBot 框架——小而美、API 简洁、上手快。但在实际使用中，逐渐遇到了瓶颈：

1. **单平台锁定**：ZeroBot 紧绑 OneBot 协议，无法对接 Discord/Telegram
2. **性能天花板**：事件路由是线性扫描，100+ Matcher 后延迟不可控
3. **无基础设施**：熔断、限流、链路追踪等企业级能力全无
4. **插件系统原始**：基于 `init()` + 全局变量，无法热重载、无法 DI
5. **生命周期空白**：启动关闭全靠手动编排

这些痛点催生了一个想法：**能不能以 ZeroBot 的设计为起点，构建一个更通用、更健壮、多平台支持的框架？**

### ZeroBot 的核心架构

```
ZeroBot/
├── bot.go          # 主入口：Run/RunAndBlock，事件处理循环
├── api.go          # OneBot API 绑定
├── context.go      # Ctx：Send、SendChain、Echo、FutureEvent
├── engine.go       # Engine：On* 触发器工厂 + Matcher 管理
├── matcher.go      # Matcher、Rule、Handler、State、Priority
├── types.go        # Event、User、File、Group 等类型
├── rules.go        # PrefixRule、CommandRule、RegexRule
├── event_channel.go # FutureEvent：交互式事件等待
├── pattern.go      # 链式消息段匹配器
├── driver/         # 通信驱动（wsclient/wsserver/http）
└── message/        # 消息段模型 + CQ 码编解码
```

核心事件流：

```
OneBot 实现 → Driver → json.Unmarshal → Event
    → match(ctx, matcherList)  [线性扫描，按优先级排序]
    → preHandler → Rules → midHandler → Handler → postHandler
    → ctx.Send() / ctx.CallAction()
```

### 从 ZeroBot 继承的设计基因

| ZeroBot 模式 | Remilia 继承方式 | 后续演变 |
|---|---|---|
| Matcher + Rule + Handler 三元组 | 概念完全继承，类型签名几乎一致 | 新增 commandIndex O(1) 路由 + TempManager 分片 |
| Engine 作为 Matcher 容器 | 继承，但改为 COW 不可变状态 | atomic.Value + 泛型，消除锁竞争 |
| Priority 优先级排序 | 继承 | 扩展为 6 路合并排序 + 缓存失效 |
| preHandler / midHandler / postHandler | 继承管道概念 | 扩展为三层洋葱模型 + 版本计数器优化 |
| Ctx.Send() / Ctx.CallAction() | 保持相同的调用风格 | 扩展 platform.Sender 跨平台发送 |
| FutureEvent 临时匹配器 | 继承 | 抽离为 TempManager 独立管理 |
| Event 预处理（IsToMe、At 检测） | 继承 | 平台无关化，每个 Adapter 各自实现 |

### 最初的 Remilia 长什么样？

```go
// 第一个 commit 的 bot.go — 与 ZeroBot 高度相似
type Bot struct {
    wh     webhook.WebHook          // QQ Webhook → 参考 ZeroBot 的 driver
    tm     *token.Manager           // QQ Token
    api    openapi.OpenAPI          // QQ OpenAPI  → 参考 ZeroBot 的 APICaller
    engine *Engine                  // Engine       → 概念源于 ZeroBot
}

// 第一个 commit 的 context.go — 几乎就是 ZeroBot Ctx 的翻版
type Context struct {
    event *dto.Payload    // 参考 ZeroBot Event
    api   openapi.OpenAPI // 参考 ZeroBot APICaller
    state State           // 完全复制 ZeroBot State
}
```

最初的目标是：**做 ZeroBot 能做的事，但做到更好**。后来目标变成了：**做 ZeroBot 做不到的事**。

### 从借鉴到超越的关键分叉点

| 决策 | ZeroBot 做法 | Remilia 最初做法 | 最终演进 |
|------|-------------|-----------------|---------|
| 并发模型 | sync.Mutex | sync.RWMutex | COW + atomic.Value（完全无锁读） |
| 平台支持 | 仅 QQ/OneBot | 仅 QQ(openapi) | 7 个平台适配器 + 插件系统 |
| 插件系统 | init() 全局注册 | 继承 BasePlugin | 函数式 Descriptor + DI 容器 |
| 生命周期 | 无 | Bot 内嵌管理 | lifecycle 独立包 |
| 中间件 | pre/mid/post 三阶段 | 同左 | 洋葱模型 + 企业级中间件 |
| 路由 | 线性 O(n) | 线性 O(n) | commandIndex O(1) + 6 路合并 |
| 日志 | 简单日志 | logrus | zerolog 零分配 |
| 观测 | 无 | 无 | Prometheus + OTel + Health |

### 参考文档

关于 ZeroBot 与 Remilia 的详细逐项对比（代码级），请参阅：
- [`11-zerobot-inspiration.md`](11-zerobot-inspiration.md) — 本文的姊妹篇，完整技术对比
- [`../06-archived/comparison-zerobot-floattech.md`](../06-archived/comparison-zerobot-floattech.md) — 框架层与 FloatTech 系列库对比
- [`../06-archived/comparison-zerobotplugin.md`](../06-archived/comparison-zerobotplugin.md) — 业务插件层对比

## 第一阶段：Monolithic QQ Bot（初始态）

**关键词**：单一职责、快速验证、紧耦合

### 初始目录结构

```
remilia/                   # 所有代码都在根包
├── bot.go                 # Bot 入口（紧耦合 QQ openapi）
├── engine.go              # 事件引擎（内含死信队列、状态管理）
├── context.go             # 事件上下文（携带 dto.Payload + openapi.OpenAPI）
├── plugin.go              # 插件系统 v1
├── matcher.go             # 匹配器
├── rules.go               # 匹配规则
├── middleware/             # 中间件（独立的子包）
├── openapi/               # QQ 官方 API SDK（内置）
├── errors.go              # 错误处理
├── config/                # 配置管理
├── helper/                # 辅助函数
└── docs/archive/          # 大量历史文档
```

### 核心问题

**1. Bot 与 QQ API 深度绑定**

```go
// 初始 bot.go
type Bot struct {
    wh     webhook.WebHook          // QQ Webhook
    tm     *token.Manager           // QQ Token
    api    openapi.OpenAPI          // QQ OpenAPI
    engine *Engine
}
```

所有核心结构都直接引用 `dto.Payload`（QQ 的消息结构）：

```go
// 初始 context.go
type Context struct {
    event *dto.Payload    // 直接依赖 QQ 数据结构
    api   openapi.OpenAPI // 直接依赖 QQ API
    state State
}

// 初始 engine.go 的死信队列
type DeadLetterItem struct {
    Event   *dto.Payload  // 强依赖 QQ 类型
    Err     error
    Attempt int
}
```

**2. 插件系统基于继承（v1 OOP 模式）**

```go
// 初始 plugin.go — 经典的 OOP 继承模式
type Plugin interface {
    Name() string
    Load(engine *Engine) error
    Unload(engine *Engine) error
    Reload(engine *Engine) error
    Dependencies() []string
}

type BasePlugin struct {
    name     string
    matchers []*Matcher
    mu       sync.RWMutex
}
```

每个插件继承 `BasePlugin`，覆写生命周期方法。这种方式的问题是：
- 样板代码多：每个插件都要写相同的结构
- 框架耦合：插件持有 `*Engine` 引用，可以做任何操作
- 依赖关系需要手写 `Dependencies()` 方法

**3. 日志系统使用 logrus**

```go
// 初始代码使用 logrus
"github.com/sirupsen/logrus"
```

**4. 庞大的 docs/archive 目录**

记录了从 v1.2 到 v2.0 的每一次升级的详细设计文档——这是框架演进的重要见证。

### 本阶段典型用法

```go
func main() {
    eng := remilia.NewEngine()           // 引擎在根包
    adapter := webhook.NewWebHook(...)    // QQ Webhook
    bot := remilia.NewBot(adapter, eng)   // Bot 在根包
    bot.Start()
    bot.WaitForShutdown()
}
```

## 第二阶段：引擎抽取 → 可测试性

**驱动因素**：引擎逻辑不断膨胀，根包 `engine.go` 达到 800+ 行，包含死信队列、状态管理、匹配器操作、清理器等。需要拆分 + 可测试性。

### 关键变化

```bash
# 引擎从根包拆分为独立子包
remilia/engine/ → core/engine/
remilia/context → core/context/
remilia/matcher → 合并入 core/engine/
remilia/rules → 合并入 core/context/
```

### COW 引擎的诞生

在初始版本中引擎就已经使用了 COW 模式，但当时的实现较为粗糙：

```go
// 初始引擎状态 -- 类型不安全
type Engine struct {
    state      atomic.Value // *engineState — 需要到处类型断言
    middleware atomic.Value // *middlewareState
    writeMu    sync.Mutex
}
```

演进到泛型版本：

```go
// 后续引入 infraatomic.Value 泛型封装
type Engine struct {
    state      *infraatomic.Value[*state]
    middleware *infraatomic.Value[*middlewareState]
}
```

### 引擎内部的演进路径

```
V1（初始）                    V2（抽出 TempManager）           V3（当前）
engine.go 800+ 行            core/engine/                   core/engine/
├── Engine + 状态             ├── engine.go (核心定义)         ├── engine.go (核心定义)
├── 死信队列                   ├── engine_state.go             ├── state.go (不可变状态)
├── 匹配器操作                 ├── engine_matcher_ops.go       ├── engine_matcher_ops.go
├── 清理器                     ├── engine_command.go           ├── engine_command.go
├── 批量处理                   ├── engine_query.go             ├── engine_query.go
                              ├── temp_manager.go (新增)       ├── temp_manager.go
                              ├── middleware.go               ├── middleware.go
                              ├── process.go                  ├── process.go
                              ├── process_platform.go (新增)   ├── process_platform.go
                              ├── component.go (新增组件抽象)   ├── component.go
                              ├── services.go (新增集中管理)    ├── services.go
                              └── runtime.go                  └── runtime.go
```

关键洞察：`temp_manager.go` 的引入是为了隔离"临时 Matcher"（一次性/带过期时间）与永久 Matcher。如果没有这个隔离，每次事件处理都需要遍历所有临时 Matcher 检查过期，导致性能不可预测。

## 第三阶段：Context 改造 → Pool 化 + 平台无关

**驱动因素**：
- Context 在高并发下大量创建/销毁，GC 压力大
- Context 仍紧耦合 `dto.Payload` 和 `openapi.OpenAPI`
- 需要支持平台无关的事件传递

### Context Pool

```go
// 引入对象池，高并发下显著降低 GC 压力
var contextPool = sync.Pool{
    New: func() any { return &Context{state: make(State)} },
}

func AcquireContext() *Context {
    return contextPool.Get().(*Context)
}

func ReleaseContext(ctx *Context) {
    ctx.reset()
    contextPool.Put(ctx)
}

// 扩展为双路径
func AcquireContextFromEvent(event platform.Event, sender platform.Sender) *Context {
    ctx := contextPool.Get().(*Context)
    ctx.platformEvent = event
    ctx.platformSender = sender
    ctx.isPlatformPath = true
    return ctx
}
```

性能提升：`0 allocs/op`，无 GC 压力。

### Context 平台无关化

```go
// V1: Context 持有 dto.Payload
type Context struct {
    event *dto.Payload
    api   openapi.OpenAPI
}

// V2: Context 支持双路径（适配器模式）
type Context struct {
    // 旧路径
    event *dto.Payload
    api   openapi.OpenAPI
    // 新路径
    platformEvent  platform.Event
    platformSender platform.Sender
    isPlatformPath bool  // 路径选择器
}
```

最终演进为完全抛弃旧路径：

```go
// V3（当前）：完全基于 platform.Event
type Context struct {
    platformEvent  platform.Event
    platformSender platform.Sender
    botID          string
    // ... 其他通用字段
}
```

## 第四阶段：插件系统革命 v1 → v2

**驱动因素**：
- 继承模式不够灵活，插件与框架紧耦合
- 热重载只有一种策略（unload-load）
- 权限控制缺失——插件持有 `*Engine` 可以做任何事
- 依赖管理靠手写 `Dependencies()`，容易出错

### 演进路径

```
v1 (继承模式)                     v2 (函数式描述符)
━━━━━━━━━━━━━━━━━━━             ━━━━━━━━━━━━━━━━━━━
Plugin 接口 (Load/Unload/Reload) Descriptor + Setup/Teardown
BasePlugin 基类                  无基类，纯函数
Dependencies() 手写              Smart DryRun 自动推断
Engine 完全访问                  SetupContext 受限视图
热重载: 仅 unload-load           + InPlace + BlueGreen
插件在根包 plugin.go             独立 plugin/ 包
```

### BlueGreen 重载的引入

```go
// v2 新增蓝绿部署策略
ReloadBlueGreen: 新实例 Setup → 原子切换 → 旧实例 Teardown

// 停机窗口对比
// unload-load:  Teardown旧 → Setup新      → 有窗口（~100ms-1s）
// in-place:     Reload函数                 → 无窗口（但开发者负责）
// blue-green:   Setup新 → 原子切换 → 旧Tear → 无窗口（框架保证）
```

### 依赖注入容器

```go
// v1: 插件之间通过全局变量或 Engine 互相访问
// 问题：隐式依赖，测试困难，竞态条件

// v2: 依赖注入容器
type Container struct {
    services  sync.Map           // 注册阶段
    frozen    atomic.Bool        // 冻结标志
    frozenMap atomic.Pointer[map[string]any]  // 只读快照
}

// 插件通过 SetupContext.Require("storage") 显式声明依赖
```

### 内置插件的演进

```
v1: plugin.go (一个文件管理所有插件注册)
v2: builtin/ (25+ 独立包，每个包一个插件)
    ├── core/help/           # 帮助命令自动发现
    ├── core/admin/          # 管理命令
    ├── core/permission/     # 权限管理
    ├── acl/                 # 访问控制
    ├── antispam/            # 反垃圾
    ├── auditlog/            # 审计日志
    ├── broadcast/           # 广播
    ├── bundle/              # 资源包
    ├── calendar/            # 日历
    ├── conversation/        # 对话
    ├── cooldown/            # 冷却
    ├── i18n/                # 国际化
    ├── idiomdict/           # 成语词典
    ├── job/                 # 任务系统
    ├── keywordfilter/       # 关键词过滤
    ├── messagelog/          # 消息日志
    ├── pluginctrl/          # 插件控制
    ├── pluginstore/         # 插件商店
    ├── ratelimitui/         # 限流 UI
    ├── scheduler/           # 调度器
    ├── sendqueue/           # 发送队列
    ├── stats/               # 统计
    ├── storage/             # 存储
    ├── subscription/        # 订阅
    ├── verifycode/          # 验证码
    └── vevent/              # 虚拟事件
```

## 第五阶段：多平台抽象（最关键的重构）

**驱动因素**：需要支持 Discord、Telegram、微信等更多平台。初始架构所有代码都依赖 `openapi/dto`。

### 三层抽象

```
┌──────────────────────────────────────────────┐
│               platform.Event                  │  ← 平台无关事件
│  Platform() string                            │
│  Kind() EventKind                             │
│  Raw() any                                    │
│  GetMessage() / GetSender() / GetGroup()      │
└──────────────────────────────────────────────┘

┌──────────────────────────────────────────────┐
│              platform.Adapter                 │  ← 平台适配器接口
│  Platform() string                            │
│  Start(ctx, func(Event)) error                │
│  Stop(ctx) error                              │
│  Sender() Sender                              │
│  Capabilities() Capabilities                  │
│  IsRunning() bool                             │
└──────────────────────────────────────────────┘

┌──────────────────────────────────────────────┐
│             platform.Registry                 │  ← 多平台注册表
│  Register(adapter)                            │
│  All() []Adapter                              │
└──────────────────────────────────────────────┘
```

### 适配器演进

```bash
# QQ 适配器是第一个，也是最复杂的
platform/qq/
├── adapter.go           # 包装 dto.Payload → platform.Event
├── webhook_server.go    # Webhook HTTP Server
├── webhook_conn.go      # WebSocket 连接
├── sender.go            # platform.Sender 实现
└── event.go             # QQ 事件转换

# 后来逐步添加
platform/discord/   # DiscordGo SDK 封装
platform/telegram/  # Telegram Bot API
platform/onebot/    # OneBot v11 协议
platform/satori/    # Satori 协议
platform/wechat/    # 微信
platform/milky/     # Milky QQ 协议
```

### 最关键的 commit

`0709f98` — `feat(platform): complete multi-platform abstraction migration`

这个 commit 完成了：
1. 定义 `platform.Event` / `platform.Sender` / `platform.Adapter` 接口
2. `core/context` 双路径改造（兼容旧 `dto.Payload` 路径）
3. `core/engine.ProcessPlatformEvent` 新入口
4. 所有测试 21 个新增

```go
// 新增平台无关入口
func (e *Engine) ProcessPlatformEvent(event platform.Event, sender platform.Sender) {
    ctx := context.AcquireContextFromEvent(event, sender)
    defer context.ReleaseContextFromEvent(ctx)
    e.processEventContext(ctx)
}
```

## 第六阶段：生命周期系统化

**驱动因素**：启动/关闭顺序越来越复杂，Engine、PluginManager、Adapters 之间的启动依赖和关闭顺序需要统一管理。

### 演进

```
V1: bot.go 内嵌启动逻辑         V2: lifecycle.Component 接口         V3（当前）
bot.Start() 里手动编排          lifecycle.NewManager()              lifecycle + 双层 Context
bot.Stop() 里手动编排           component.OnStart/OnRun/OnStop
                              SimpleComponent 简化创建
```

关键变化：从"Bot 内部编排"演进到"独立的 lifecycle 包"，Bot 只是其中一个使用者。

```go
// V1 — Bot 手动管理
func (b *Bot) Start() {
    b.wh.Start(b.handleEvent)       // 1. 启动 Webhook
    go b.engine.StartCleaner()      // 2. 启动清理器
}

func (b *Bot) Stop() {
    b.wh.Stop()                     // 逆序
    b.engine.StopCleaner()
}

// V3（当前）— lifecycle 统一管理
func (b *Bot) Start() {
    b.lifecycle.Register(engineComp)
    b.lifecycle.Register(adapterComp)
    b.lifecycle.Register(pluginComp)
    b.lifecycle.Start(ctx)  // 框架管理顺序
}
```

双层 Context 的设计也是演变出来的：

```
V1: 一个 Context 贯穿全部             V2: 区分 parentCtx / runCtx
bot.Start(ctx) → context 传递给所有     Start: parentCtx
                                                      ├─ OnStart (准备)
                                                      └─ runCtx (运行时)
                                                      Stop:
                                                      ├─ cancel(runCtx)
                                                      ├─ 等待 OnRun 退出
                                                      ├─ 逆序 OnStop
                                                      └─ cancel(parentCtx)
```

这个演进的关键动机：**插件 Teardown 时还需要使用平台 API 发消息**。如果 parentCtx 在 Stop 一开始就被取消，插件在 Teardown 中调用 `ctx.Reply()` 将立刻失败。

## 第七阶段：基础设施沉淀

随着 `core/engine`、`plugin/`、`middleware/` 逐渐稳定，通用工具代码被提取到 `infra/`：

```bash
infra/
├── atomic/      # 泛型 atomic.Value（从 engine 中提取）
├── pool/        # 泛型对象池（从 context pool 中抽象）
├── syncx/       # 并发工具（从各模块提取）
├── health/      # 健康检查框架（从 bot 中提取）
├── metrics/     # Prometheus 封装（从 engine 中提取）
├── tracing/     # OpenTelemetry 封装（从 middleware 中提取）
├── server/      # HTTP Server 封装（从 webhook 中提取）
├── httpclient/  # HTTP 客户端（从 openapi 中提取）
├── textimage/   # 文本渲染引擎
├── dlq/         # 泛型死信队列
├── zhtext/      # 中文文本处理
├── audit/       # 操作审计
├── cache/       # TTL 缓存
├── coredump/    # 跨平台 coredump
├── option/      # Option 模式封装
└── fs/          # 懒加载文件系统
```

## 演进总结

```
2025-11    ZeroBot 启蒙——深度使用 → 发现瓶颈 → 萌生自研想法
    │
    ├── 研究 ZeroBot 架构（Matcher/Engine/Context 模式）
    ├── 识别 ZeroBot 天花板（单平台/O(n)路由/无生命周期）
    └── 初期目标："做 ZeroBot 能做的事，但做得更好"
    │
2025-12    Monolithic QQ Bot (根包、dto.Payload、logrus、v1 插件)
    │                           ↑ 保留了 ZeroBot 的核心模式，
    │                           但开始使用不同技术栈
    ├── 引擎抽取 core/engine
    ├── Context 池化
    ├── 插件描述符 + 依赖注入
    │
2026-01    多引擎 + Context v2
    │
    ├── platform/adapter 抽象  ← 此时已决定"超越 ZeroBot"
    ├── 日志替换 zerolog
    ├── lifecycle 独立包
    │
2026-02    Plugin v2 正式版
    │
    ├── 蓝绿热重载
    ├── 多平台 (Discord/Telegram/OneBot)
    ├── infra 基础设施沉淀
    │
2026-03    平台迁移完成
    │
    ├── 内置 25+ 插件
    ├── textimage 引擎
    └── v1.0.0 发布
```

### 关键决策时刻

| 决策 | 当时的选择 | 替代方案 | 为什么 |
|------|-----------|---------|--------|
| COW 引擎 | 写时复制 + atomic.Value | ZeroBot 的 sync.Mutex | 读多写少场景，5-6x 性能提升 |
| 插件 v2 | 函数式 Descriptor | ZeroBot 的 init() 全局注册 | 解耦、可测试、灵活 |
| 多平台抽象 | Adapter 接口 | ZeroBot 的 OneBot 单平台 | 跨平台 Handler 复用 |
| Context Pool | sync.Pool | ZeroBot 每次都 new Ctx | 0 allocs/op 是硬性要求 |
| 生命周期 | lifecycle 独立包 | ZeroBot 无生命周期，Bot 内手工编排 | 组件化、可复用的生命周期管理 |
| 日志 | zerolog | ZeroBot 无零分配要求 | 零分配日志对性能关键 |
| O(1) 命令路由 | commandIndex + Trie | ZeroBot 线性扫描 O(n) | 100+ 命令时延迟不增长 |
| 中间件链 | 洋葱模型 + 三层级 | ZeroBot pre/mid/post 三阶段 | 从"三阶段"升级为"可组合链" |
