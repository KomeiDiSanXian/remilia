# Remilia Bot 框架架构深度评审报告

> 评审时间：2026-03-02  
> 项目状态：**未发布（Pre-release）**，可接受破坏性重构  
> 评审范围：`core`、`middleware`、`plugin`、`command` 及顶层 `Bot` 入口  
> 最后更新：2026-03-02 — P0-1 / P0-2 / P0-3 / P1-1 / P1-2 / P1-3 / P1-4 / P1-5 / P2-1 / P2-4 / P2-5 / P2-6 / P2-7 已完成

---

## 目录

1. [总体评价](#1-总体评价)
2. [模块逐项分析](#2-模块逐项分析)
   - 2.1 [core/engine — 事件引擎](#21-coreengine--事件引擎)
   - 2.2 [core/context — 事件上下文](#22-corecontext--事件上下文)
   - 2.3 [core/permission — 权限系统](#23-corepermission--权限系统)
   - 2.4 [middleware — 中间件系统](#24-middleware--中间件系统)
   - 2.5 [plugin — 插件系统](#25-plugin--插件系统)
   - 2.6 [command — 命令系统](#26-command--命令系统)
   - 2.7 [Bot 顶层入口](#27-bot-顶层入口)
3. [插件开发友好性评估](#3-插件开发友好性评估)
4. [核心问题汇总](#4-核心问题汇总)
5. [重构优化路线图](#5-重构优化路线图)
   - 5.1 [P0 — 结构性问题（必须修复）](#51-p0--结构性问题必须修复)
   - 5.2 [P1 — 设计改善（强烈建议）](#52-p1--设计改善强烈建议)
   - 5.3 [P2 — 体验优化（建议）](#53-p2--体验优化建议)
   - 5.4 [P3 — 长期演进](#54-p3--长期演进)
6. [总结与优先级决策](#6-总结与优先级决策)

---

## 1. 总体评价

### 亮点

Remilia 框架整体架构思路清晰，在并发安全、性能优化方面有较强的工程意识：

- **COW（Copy-on-Write）无锁读模型**：Engine 使用 `infraatomic.Value` 管理不可变状态，读操作完全无锁，性能优异
- **插件系统 v2 的函数式设计**：`PluginDescriptor` 大幅降低样板代码，依赖注入容器两阶段设计（注册→冻结）合理
- **并发安全意识强**：`shutdownMu + eventWg` 的安全关闭设计、`sync.Pool` 对象复用、`contentOnce/authorOnce` 热路径缓存
- **基础设施完善**：`infra/` 下的 atomic、metrics、tracing、httpclient 等模块独立可复用
- **热重载三策略**（UnloadLoad / InPlace / BlueGreen）完整，BlueGreen 零停机方案优秀

### 核心问题

框架存在三个根本性问题，影响插件生态的建立：

1. **Bot ↔ Plugin 完全割裂**：`Bot` 结构体无任何插件管理字段，`plugin.Manager` 游离在主框架之外
2. **层级依赖倒置**：`middleware` 包反向导入 `core/engine`（`RetryWithDeadLetter` 依赖 `engine.DeadLetterItem`），破坏分层原则
3. **命令系统双轨并行**：`engine.commandIndex` 与 `command.Registry` 两套索引并存，概念重复，行为差异不透明

---

## 2. 模块逐项分析

### 2.1 `core/engine` — 事件引擎

#### 设计评分：7.5 / 10

#### 优点

| 特性 | 详情 |
|---|---|
| COW 无锁读 | `state` 和 `middleware` 均用 `infraatomic.Value` 包裹，读操作零锁竞争，写操作由 `writeMu` 串行保护 |
| 三正交索引 | `matcherIndex`（事件类型）、`commandIndex`（命令名→事件类型）、`groupIndex`（分组批量操作），各自服务不同场景 |
| O(1) 命令路由 | 消息内容以 `/` 开头时从 `commandIndex` 直接定位候选 Matcher，跳过全量遍历 |
| 安全关闭 | `shutdownMu`（RWMutex）+ `eventWg` 的双保险设计，彻底消除 `ProcessEvent` 通过检查但 `Wait()` 已返回的竞态窗口 |
| 中间件代际缓存 | `globalSnap.gen` / `groupSnap.gen` 惰性失效机制，避免每次事件处理时重建 Middleware 链 |

#### 问题

**问题 1：`Matcher` 结构体职责过载**

`Matcher` 在 `matcher.go` 中有 **20+ 字段**，承担了状态机（`matcherRuntime`）、规则集（`Rules`）、中间件链缓存（`compiledHandlers`）、命令定义（`definition`）等多种职责。`matcher.go` 本身达到 734 行。

```
Matcher {
  matcherRuntime   // 状态机：deleted/disabled/useCount/expiry
  Rules            // 规则集
  compiledChain    // 中间件链缓存
  definition       // 命令元数据
  coordinator      // Engine 的反向引用（强双向依赖）
  ...
}
```

**问题 2：`MatcherCoordinator` 接口方法过多**

```go
// 当前 MatcherCoordinator 有 8 个方法
type MatcherCoordinator interface {
    DeleteMatcher(m *Matcher)
    RebuildMatcherChain(m *Matcher)
    InvalidateSortedCache(eventType dto.EventType)
    UpdateTempMatcherPriority(m *Matcher)
    UpdateMatcherCommand(m *Matcher)
    UpdateCommandCache(m *Matcher)
    MigrateMatcherToTemp(m *Matcher)
    MigrateMatcherFromTemp(m *Matcher)
}
```

`Matcher` 对 `Engine` 的反向依赖（`coordinator` 字段）导致两者紧耦合，单元测试 `Matcher` 需要 mock 8 个方法。

**问题 3：`compiledChain` 在热重载场景存在隐式失效风险**

`compiledChain` 用 `chainSig`（XOR 指纹）做缓存有效性校验，但 XOR 对顺序不敏感：中间件 A+B 与 B+A 会产生相同的 XOR 值，导致错误的缓存命中。

**问题 4：`engine` 包文件数量过多**

`core/engine/` 下目前有 **35 个文件**（含测试），其中生产代码约 16 个，职责边界不清晰：

```
engine.go         # 结构体定义
engine_command.go # 命令注册
engine_matcher_ops.go  # Matcher 操作
engine_query.go   # 查询 API
process.go        # 事件处理
state.go          # 状态快照
matcher.go        # Matcher（734行）
...
```

---

### 2.2 `core/context` — 事件上下文

#### 设计评分：7 / 10

#### 优点

| 特性 | 详情 |
|---|---|
| 双层存储设计 | 字符串键（`extensionState`，`ctx.Set/Get`）vs 类型键（`Extensions` 泛型，`ExtGet/ExtSet`），隔离清晰 |
| `decodeCache` 类型联合体 | 消除 `interface{}` boxing，避免 `reflect.Type` 字符串分配，GC 扫描无额外间接层 |
| `sync.Pool` 复用 | 通过 `ReleaseContext()` 归还到 pool，减少高频事件处理的 GC 压力 |
| 热路径 `Once` 缓存 | `contentOnce`/`authorOnce` 懒加载并缓存，多次访问同一字段无重复解析开销 |

#### 问题

**问题 1：`context.go` 单文件 685 行，职责混杂**

```
context.go 职责清单（当前全部在一个文件）：
- Context 结构体定义（核心字段、生命周期）
- gjson 事件解码（DecodeEvent、decodeCache）
- 消息内容缓存（GetMessageContent、contentOnce）
- 作者缓存（GetAuthor、authorOnce）
- 扩展状态读写（Set/Get/All/ExtSet/ExtGet）
- 规则系统（Rule、SetRule、GetRule）
- 权限桥接（GetPermissionManager、SetPermissionManager）
- OpenTelemetry Span 绑定（SetSpan/GetSpan）
- 重试元数据（SetRetryAttempt/GetRetryAttempt）
- Matcher 来源（GetMatcherSource）
```

**问题 2：`Matcher` 接口仅为规避循环依赖而存在**

```go
// 仅一个方法，设计上是绕道而行
type Matcher interface {
    GetSource() string
}
```

这是结构性耦合的症状，而非合理的接口抽象。

**问题 3：`ExtSet`/`ExtGet` 的泛型 API 对插件开发者认知成本高**

```go
// 使用者需要理解泛型类型参数的含义
plugin.ExtSet[MyType](ctx, value)
v, ok := plugin.ExtGet[MyType](ctx)
```

文档和示例不足的情况下，初次上手容易与字符串键 API（`ctx.Set/Get`）混淆。

---

### 2.3 `core/permission` — 权限系统

#### 设计评分：7 / 10

#### 优点

| 特性 | 详情 |
|---|---|
| 零依赖独立包 | `permission` 包无 `context` 依赖，可在 HTTP 服务、CLI 工具中单独使用 |
| 完整 RBAC | 支持 Role→Permission 映射、通配符（`command:*`、`*:*`）、`Provider` 接口接外部存储 |
| 通过扩展桥接 | `PermissionManagerExt` 作为类型键存储在 `core/context`，解耦合理 |

#### 问题

**问题 1：`Role.Permissions` 线性扫描**

```go
type Role struct {
    Name        string
    Permissions []Permission  // slice，HasPermission = O(n)
    mu          sync.RWMutex
}
```

大规模角色（如数百个权限点）时，`HasPermission` 是 O(n) 线性扫描。建议改为 `map[string]Permission`（以 `resource:action` 为键）。

**问题 2：权限系统与插件系统完全脱节**

插件无法声明"需要哪些权限"，管理员无法通过框架 API 查询"插件 X 需要哪些权限点"。`PluginDescriptor` 中没有 `RequiredPermissions` 字段。

**问题 3：与 `middleware.Auth` 无官方桥接**

`middleware.Auth` 的鉴权函数签名是 `func(ctx *eventctx.Context) bool`，完全由用户自行实现，框架没有提供 `permission.Manager` 到 `Auth` 中间件的标准适配器。

---

### 2.4 `middleware` — 中间件系统

#### 设计评分：7.5 / 10

#### 优点

| 特性 | 详情 |
|---|---|
| 标准 `Handler→Handler` 模型 | `Middleware = func(Handler) Handler`，符合 Go 生态惯例，组合简单 |
| 内置中间件丰富 | Logging / Recover / Auth / Retry / RateLimit / CircuitBreaker / Dedup / Adaptive / Tracing 全覆盖 |
| `AdaptiveRateLimiter` | 内置 P99 延迟直方图，根据负载自动调整限流阈值，无外部依赖 |
| 生命周期绑定 | 大部分中间件支持 `WithContext` 模式，随 Bot 生命周期正确关闭 |
| `ConfigurableRetry` | 支持运行时热更新重试配置，为热重载场景设计 |

#### 问题

**问题 1：`RetryWithDeadLetter` 反向依赖 `core/engine`（层级倒置）**

```go
// middleware/retry.go
import "github.com/KomeiDiSanXian/remilia/core/engine"

func RetryWithDeadLetter(cfg RetryConfig, deadLetterCh chan engine.DeadLetterItem) eventctx.Middleware {
```

`middleware` 依赖 `core/engine` 破坏了分层原则：
```
理想依赖方向：
  adapter → bot → core/engine → core/context → middleware
  
实际：
  middleware → core/engine  ← 反向依赖！
```

**解决方案**：将 `DeadLetterItem` 迁移到 `core/context` 或独立的 `infra/dlq` 包，`middleware` 仅依赖该包。

**问题 2：`DedupFilter` 后台 goroutine 生命周期需手动管理**

```go
// 调用方需要自己确保调用 Stop()
filter := middleware.NewDedupFilter(cfg)
engine.Use(filter.Middleware())
// 忘记调用 filter.Stop() 会导致后台 goroutine 泄露
```

**问题 3：缺乏运行时中间件链可观测性**

无法在运行时查询当前 Engine 生效的中间件链（名称、顺序、配置），调试困难。

**问题 4：插件级中间件作用域无隔离**

插件通过 `ctx.Reg.On(...).Use(mw)` 添加的 Matcher 级中间件与全局中间件在链的构建上没有明确的命名空间，难以追踪来源。

---

### 2.5 `plugin` — 插件系统

#### 设计评分：8 / 10（单看插件包本身）；但与 Bot 集成评分：4 / 10

#### 优点

| 特性 | 详情 |
|---|---|
| `PluginDescriptor` 函数式设计 | 无需继承，无 BasePlugin，最少化样板代码 |
| `SetupContext` 能力完备 | Reg / Log / Info / Admin / EventBus / Go / Config 全部通过上下文注入 |
| 依赖注入 `Container` | 两阶段（注册→冻结）设计，冻结后读无锁，性能优秀 |
| 热重载三策略 | UnloadLoad / InPlace / BlueGreen，零停机蓝绿方案完整 |
| Smart 依赖推断 | `RegisterMultipleV2Smart` 自动拓扑排序，DryRun 阶段检测隐式依赖 |
| `Privileged` 权限控制 | 插件声明是否需要管理权限，代码审查中的安全检查点 |
| `LifecycleListener` | 加载/卸载/重载/错误事件全覆盖 |

#### 问题

**问题 1：`plugin.Manager` 与 `Bot` 完全割裂（最严重）**

```go
// bot.go - Bot 结构体
type Bot struct {
    engine    *engine.Engine
    adapter   Adapter
    lifecycle *lifecycle.Manager
    // ... 没有 pluginManager 字段！
}

// 用户现在必须手动关联：
eng := engine.NewEngine()
bot := remilia.NewBot(adapter, eng)
pm := plugin.NewManager(eng)
pm.RegisterV2(myPlugin)
// Bot.Start() 不会触发插件的任何生命周期，用户需要自行协调
```

这意味着：
- `Bot.Stop()` 不会自动触发插件 `Teardown`
- 插件 goroutine 泄露无法通过 `Bot` 的生命周期自动回收
- 用户初次上手时不明白为何 Bot 和插件是两个独立对象

**问题 2：`manager.go` 强依赖 `viper`/`fsnotify`**

```go
// plugin/manager.go
import (
    "github.com/fsnotify/fsnotify"
    "github.com/spf13/viper"
)

type Manager struct {
    viper *viper.Viper  // 即使不使用配置热重载，也会被编译进来
}
```

这给想要零依赖使用插件系统的用户增加了不必要的间接依赖。

**问题 3：`register.go` 单函数逻辑过于复杂（586 行）**

`RegisterV2` 函数内内联了：版本约束检查、依赖状态验证、Schema 校验、Container 注册、状态机迁移等多个逻辑，可测试性差。

**问题 4：goroutine 状态只能按插件查询，无全局视图**

```go
// 现在只能查询单个插件的 goroutine
instance.ListGoroutines()

// 无法做到：
pm.ListAllGoroutines() // ← 不存在
```

**问题 5：插件间 EventBus 无 Schema 约束**

```go
ctx.EventBus.Publish("user.login", payload)
// payload 类型完全由发布者决定，订阅者需要自行类型断言
// 没有编译期或运行期的事件类型约束
```

---

### 2.6 `command` — 命令系统

#### 设计评分：6.5 / 10

#### 优点

| 特性 | 详情 |
|---|---|
| Trie 树 + 编译注册表双索引 | O(prefix-length) 前缀查找，适合命令自动补全场景 |
| `ParseCommandLine` 功能完整 | 支持 `--key value` / `-k v` / 位置参数 / 引号转义 |
| `Definition` 元数据完整 | aliases / description / usage / category / hidden 全覆盖 |

#### 问题

**问题 1：命令系统被三层同时导入，成为公共依赖**

```
core/context ──imports──▶ command
core/engine  ──imports──▶ command
plugin       ──imports──▶ command（通过 engine）
```

`command` 包成了隐式的全局公共依赖，变更风险高。

**问题 2：两条注册路径行为差异不透明**

```go
// 路径 1：通过 Rule 注册（Matcher 级别）
engine.On(dto.C2CMessageCreate, OnCommand("/hello")).Handle(handler)

// 路径 2：通过 Definition 注册（Registry 级别）
engine.RegisterCommand(dto.C2CMessageCreate, "/hello").Handle(handler)
```

两者在 `commandIndex`（O(1) 快速路由）、Trie 树（前缀搜索）、`/help` 显示等方面行为有差异，文档不足，容易混用。

**问题 3：`engine.commandIndex` 与 `command.Registry` 概念重复**

Engine 内部维护一套命令→EventType 的索引（`commandIndex`），`command.Registry` 维护另一套 Trie + 别名索引，两者在注册时需要分别更新，存在不一致风险。

---

### 2.7 Bot 顶层入口

#### 设计评分：6.5 / 10

#### 优点

| 特性 | 详情 |
|---|---|
| `BotBuilder` 流式 API | 链式调用，符合 Go Builder 模式惯例 |
| 生命周期完整 | Start / Stop / WaitForShutdown，`rootCtx` 统一取消传播 |
| 健康检查组件化 | `health.Check` 自动集成 Bot、Adapter、Engine 的 checker |

#### 问题

**问题 1：4 条构建路径，维护负担重**

```go
// 用户面对 4 种构建方式：
bot1 := remilia.NewBot(adapter, engine, opts...)
bot2 := remilia.NewBotWithInfo(adapter, engine, botInfo, opts...)
bot3 := remilia.NewBotWithDefault(botInfo, addr, opts...)
bot4 := remilia.NewBotBuilder().WithBotInfo(info).WithWebhook(":8080").Build()
```

`NewBot` / `NewBotWithInfo` / `NewBotWithDefault` 语义重叠，只需保留 `BotBuilder` 一条路径。

**问题 2：`BotBuilder.WithWebhook` 顺序依赖**

```go
// 必须先调用 WithBotInfo，再调用 WithWebhook，否则 adapter 不会被创建
builder.WithBotInfo(info).WithWebhook(":8080")  // ✅ 正常
builder.WithWebhook(":8080").WithBotInfo(info)  // ❌ adapter 为 nil，Build() 会失败
```

这违反了 Builder 模式"链式调用与顺序无关"的原则。

**问题 3：`Bot` 直接暴露 `Engine()` 方法**

插件开发者需要通过 `bot.Engine()` 绕过 `Bot` 的抽象层直接操作引擎，是封装泄漏的信号。

**问题 4：`Bot` 无插件集成点**

```go
// 期望中的 API（不存在）：
bot.UsePlugins(pm *plugin.Manager)
bot.Plugins() *plugin.Manager
```

---

## 3. 插件开发友好性评估

| 维度 | 评分 | 说明 |
|---|---|---|
| **上手曲线** | ★★★☆☆ 3/5 | `PluginDescriptor` 本身设计好，但 Bot ↔ Manager 需手动关联，初次上手有迷惑感 |
| **依赖管理** | ★★★★☆ 4/5 | Smart 自动拓扑排序好，semver 版本约束清晰 |
| **调试能力** | ★★★☆☆ 3/5 | 缺少全局 goroutine 状态视图，插件级别日志前缀有，但运行时 introspect 能力弱 |
| **热重载** | ★★★★☆ 4/5 | 三策略完整，BlueGreen 零停机方案优秀，状态迁移（SaveState/RestoreState）完善 |
| **与框架集成** | ★★☆☆☆ 2/5 | Bot 无内置 Plugin 管理，需手动构建 Engine→Manager→Plugin 调用链 |
| **中间件隔离** | ★★★☆☆ 3/5 | 全局/分组中间件可用，但插件级中间件无命名空间隔离 |
| **权限集成** | ★★☆☆☆ 2/5 | 权限系统独立完整，但与插件描述符脱节，无声明式权限注册 |
| **EventBus 类型安全** | ★★☆☆☆ 2/5 | 发布订阅无 Schema 约束，完全依赖运行时类型断言 |
| **综合评分** | **3.0 / 5** | 机制完备，但集成体验待提升 |

---

## 4. 核心问题汇总

以下是影响框架质量的 **8 个核心问题**，按严重程度排序：

| # | 问题 | 影响范围 | 严重度 |
|---|---|---|---|
| 1 | ✅ `Bot` 与 `plugin.Manager` 完全割裂，无生命周期联动 | 插件生态、用户体验 | 🔴 致命 |
| 2 | ✅ `middleware` 反向导入 `core/engine`（`RetryWithDeadLetter`） | 架构分层、可测试性 | 🔴 严重 |
| 3 | ✅ `BotBuilder.WithWebhook` 顺序依赖，违反 Builder 原则 | API 可用性 | 🟠 重要 |
| 4 | ✅ `MatcherCoordinator` 接口 8 个方法，`Matcher` 双向耦合 Engine | 可测试性、可维护性 | 🟠 重要 |
| 5 | ✅ `compiledChain` XOR 指纹对中间件顺序不敏感（潜在 bug） | 正确性 | 🟠 重要 |
| 6 | ✅ `plugin.Manager` 强依赖 `viper`/`fsnotify`（可选功能强制引入） | 依赖管理 | 🟡 建议 |
| 7 | ✅ 命令系统双轨：`engine.commandIndex` ↔ `command.Registry` 概念重复 | 一致性 | 🟡 建议 |
| 8 | 权限系统与插件描述符脱节，无声明式权限注册 | 安全模型 | 🟡 建议 |

---

## 5. 重构优化路线图

### 5.1 P0 — 结构性问题（必须修复）

---

#### P0-1：Bot 集成插件管理器 ✅ 已完成（2026-03-02）

**变更文件**：`bot.go`、`bot_builder.go`、`plugin/manager.go`

**实际实现**：
- `Bot` 结构体新增 `pluginManager *plugin.Manager` 字段
- `Bot.UsePlugins(pm *plugin.Manager) *Bot`：注入插件管理器，链式调用友好
- `Bot.Plugins() *plugin.Manager`：获取已注入的插件管理器
- `Bot.Start()` 成功后自动调用 `pluginManager.StartAll(rootCtx)`
- `Bot.Stop()` 在 lifecycle 停止前先调用 `pluginManager.StopAll(ctx)`（逆序 Teardown）
- `plugin.Manager` 新增 `StartAll(ctx)` / `StopAll(ctx)` 方法
- `BotBuilder.WithPluginManager(pm)` 方法，支持 Builder 模式注入

**效果**：
```go
// 现在的使用方式
bot, err := remilia.NewBotBuilder().
    WithBotInfo(info).
    WithWebhook(":8080").
    WithPluginManager(pm).
    Build()

bot.Start() // 自动触发所有插件 Setup
bot.Stop()  // 自动触发所有插件 Teardown（逆序）
```

---

#### P0-2：消除 middleware 对 core/engine 的反向依赖 ✅ 已完成（2026-03-02）

**变更文件**：`middleware/retry.go`、`middleware/retry_test.go`、`middleware/middleware_extra_test.go`、`core/engine/config.go`

**实际实现**：
- `middleware/retry.go` 的 import 由 `core/engine` 改为 `infra/dlq`
- `RetryWithDeadLetter` 函数签名由 `chan engine.DeadLetterItem` 改为 `chan dlq.DeadLetterItem`
- `core/engine/config.go` 中的 `DeadLetterItem` 改为 `infra/dlq.DeadLetterItem` 的类型别名（向后兼容）
- 测试文件中的 `engine.DeadLetterItem` / `engine.NewBlockError` 对应更新为 `dlq.DeadLetterItem` / `errutil.NewBlockError`

**修正后依赖方向**：
```
middleware ──▶ infra/dlq ✅（不再反向依赖 core/engine）
core/engine ──▶ infra/dlq ✅（通过类型别名保持向后兼容）
```

---

#### P0-3：修复 BotBuilder 顺序依赖 ✅ 已完成（2026-03-02）

**变更文件**：`bot_builder.go`

**实际实现**：
- `BotBuilder` 新增 `webhookAddr string` 字段，`WithWebhook(addr)` 只保存地址
- `Build()` 阶段统一完成 `webhookAddr + botInfo → adapter` 初始化
- `WithWebhook` 和 `WithBotInfo` 现在可任意顺序调用

```go
// 以下两种顺序完全等价，均可正确构建
bot, err := remilia.NewBotBuilder().
    WithBotInfo(info).WithWebhook(":8080").Build()  // ✅

bot, err := remilia.NewBotBuilder().
    WithWebhook(":8080").WithBotInfo(info).Build()  // ✅ 之前会失败，现在正确
```

---

### 5.2 P1 — 设计改善（强烈建议）

---

#### P1-1：拆分 `core/context/context.go`（685 行）✅ 已完成（2026-03-02）

**变更文件**：`core/context/context.go`（重写）、新增 `decode.go`、`state.go`、`metadata.go`、`permission.go`（补充方法）

**实际实现**：
| 文件 | 职责 |
|---|---|
| `context.go` | 结构体定义（Context / decodeCache / extensionState）、生命周期（NewContext / Clone / SetStdContext / Ext / Tracer） |
| `decode.go` | 事件解码（DecodeEvent）、热路径缓存（GetMessageContent / GetAuthor）、消息发送（SendGroupMessage / ReplyGroup / ...） |
| `state.go` | 字符串键扩展状态（Set / Get / Delete / All 及类型便捷方法） |
| `metadata.go` | 框架内部元数据（RetryAttempt / MiddlewareTrace / ParsedCommand / GetMatcherSource） |
| `permission.go` | 权限桥接方法（GetPermissionManager / SetPermissionManager） |

原 685 行单文件 → 5 个文件，每文件职责单一，最大约 180 行。

---

#### P1-2：裁剪 `MatcherCoordinator` 接口 ✅ 已完成（2026-03-02）

**变更文件**：`core/engine/types.go`

**实际实现**：
- `MatcherCoordinator`（8方法）拆分为两个子接口：
  - `MatcherLifecycle`（4方法）：核心高频路径（Delete / RebuildChain / InvalidateCache / UpdateCommandCache）
  - `MatcherMigration`（4方法）：临时 Matcher 迁移（UpdateTempPriority / UpdateCommand / MigrateToTemp / MigrateFromTemp）
- `MatcherCoordinator = MatcherLifecycle + MatcherMigration`（组合，向后兼容）
- 单元测试 Matcher 现在只需 mock 4 个方法即可覆盖核心路径

---

#### P1-3：修复 `compiledChain` XOR 指纹 bug ✅ 已完成（2026-03-02）

**变更文件**：`core/engine/process.go`、`core/engine/matcher.go`

**实际实现**：
- `chainSignature` 从 XOR（顺序不敏感）改为 FNV-1a 链式哈希（顺序敏感）：
  ```go
  // 修复前（XOR）：[A,B] == [B,A]，会错误命中缓存
  sig ^= uint64(reflect.ValueOf(m).Pointer())

  // 修复后（FNV-1a chained）：[A,B] != [B,A]，顺序不同产生不同签名
  h = (h ^ ptr) * fnvPrime
  ```
- `compiledChain.chainSig` 注释同步更新，说明已改为顺序敏感哈希

---

#### P1-4：plugin.Manager viper 依赖可选化 ✅ 已完成（2026-03-02）

**变更文件**：`plugin/manager.go`（移除 viper/fsnotify import）、`plugin/config.go`（新增 configProvider 路径）、新增 `plugin/config_provider.go`

**实际实现**：
- 新增 `ConfigProvider` 接口（`Sub` / `OnConfigChange`），Manager 通过接口与配置源解耦
- 新增 `ViperConfigProvider`：`ConfigProvider` 的 viper 实现，在 `config_provider.go` 中（仅此文件依赖 viper/fsnotify）
- 新增 `ManagerOption` 函数类型和 `WithConfigProvider(cp ConfigProvider) ManagerOption`
- `NewManager(eng, opts ...ManagerOption)` 支持可选注入
- `Manager.SetConfigProvider(cp ConfigProvider)` 运行时注入
- `Manager.SetViper(_ any)` 保留为 Deprecated 警告方法（向后兼容）
- `plugin/manager.go` 不再直接 import `viper`/`fsnotify`

```go
// 新 API（推荐）
pm := plugin.NewManager(eng,
    plugin.WithConfigProvider(plugin.NewViperConfigProvider(v)),
)

// 零配置（不使用任何配置文件）
pm := plugin.NewManager(eng)
```

---

#### P1-5：拆分 `plugin/register.go` 的单函数逻辑 ✅ 已完成（2026-03-02）

**变更文件**：`plugin/register.go`（RegisterV2 简化）、新增 `plugin/register_validate.go`

**实际实现**：
新增 `register_validate.go` 包含 4 个独立验证函数：
- `validateDescriptor(desc)`：基础合法性（无锁、无 Manager 依赖，可独立单测）
- `checkDependencies(pm, desc, registeredList)`：依赖存在性与就绪状态
- `validateVersionConstraints(pm, desc)`：依赖版本约束
- `validateConfigSchema(name, desc, config)`：ConfigSchema 校验

`RegisterV2` 重构为 5 个明确阶段（每阶段一行调用），函数主体从 ~80 行压缩到 ~50 行。


---

### 5.3 P2 — 体验优化（建议）

---

#### P2-1：统一命令系统，消除双轨 ✅ 已完成（2026-03-02）

**变更文件**：`core/engine/services.go`、`core/engine/engine_command.go`

**实际实现**：

- `engineServices` 新增 `commandRegistry *command.Registry` 字段（可选，nil 时向后兼容）
- 新增 `Engine.SetCommandRegistry(reg *command.Registry)` 运行时注入 / `engine.WithCommandRegistry(reg)` 构造时 Option
- 新增内部 `syncToRegistry(def, source)`：`OnCommand` / `RegisterCommandDef` / `RegisterCommandDefWithPrefix` 注册后自动同步到 Registry

```go
reg := command.NewCommandRegistry()
eng := engine.NewEngine(engine.WithCommandRegistry(reg))
eng.RegisterCommandDef(dto.GroupAtMessageCreate, &command.Definition{Name: "hello"})
// commandIndex（O(1) 路由）和 Registry（Trie + /help）始终保持一致
matches := reg.Search("/hel")    // Trie 前缀搜索
cmd := eng.FindCommand("/hello") // commandIndex 精确查找
```

---

#### P2-2：插件声明式权限注册（待实现）

**文件**：`plugin/descriptor.go`、`core/permission/permission.go`  
**目标**：`PluginDescriptor.RequiredPermissions []string`，加载时框架自动注册权限点。

---

#### P2-3：中间件链运行时可观测性（待实现）

**文件**：`core/engine/middleware.go`  
**目标**：运行时可查询当前 Engine 生效的中间件链（名称、顺序、来源）。

---

#### P2-4：全局 goroutine 状态视图 ✅ 已完成（2026-03-02）

**变更文件**：`plugin/goroutine.go`、`plugin/manager.go`

**实际实现**：

- `GoroutineInfo` 新增 `Uptime time.Duration` 字段（查询时自动填充）
- `ListPluginGoroutines` 保留为 `Deprecated`（委托给 `ListAllGoroutines`）
- 新增 `Manager.ListAllGoroutines() []GoroutineInfo`：全局聚合视图，自动填充 Uptime
- 新增 `Manager.GoroutineSummary() GoroutineSummary`：低开销统计（Total + ByPlugin map）

```go
goroutines := pm.ListAllGoroutines()
for _, g := range goroutines {
    fmt.Printf("[%s] %s — uptime: %v\n", g.Plugin, g.Name, g.Uptime)
}
summary := pm.GoroutineSummary()
fmt.Printf("total=%d  by_plugin=%v\n", summary.Total, summary.ByPlugin)
```

---

#### P2-5：`permission.Role` 改用 map 存储，O(1) 查找 ✅ 已完成（2026-03-02）

**变更文件**：`core/permission/permission.go`、`core/context/context_extended_test.go`、`plugins/core/permission/permission.go`

**实际实现**：

| 项目 | 改动前 | 改动后 |
|---|---|---|
| `Role.permissions` | `[]Permission`（O(n)） | `map[string]Permission`（key="resource:action"） |
| `HasPermission` | O(n) 全量扫描 | O(1) 精确匹配 + O(wildcards) 通配符回退 |
| `AddPermission` | append | map 写（幂等） |
| `RemovePermission` | O(n) 重建 slice | O(1) delete |
| 外部访问 | `role.Permissions`（字段） | `role.Permissions()`（方法，返回副本） |

---

#### P2-6：EventBus 泛型类型安全 API ✅ 已完成（2026-03-02）

**变更文件**：`plugin/eventbus.go`（泛型 API 完整重写）、新增 `plugin/eventbus_typed_test.go`（20 个测试）

**实际实现**：

| 函数 / 类型 | 签名 | 说明 |
|---|---|---|
| `Subscribe[T]` | `(bus, topic, func(T)) → (Subscription, error)` | 类型安全订阅；类型不匹配时静默跳过 |
| `MustSubscribe[T]` | `(bus, topic, func(T)) → Subscription` | 订阅，失败 panic（适用于 Setup 阶段） |
| `SubscribeAllTyped[T]` | `(bus, func(T)) → (Subscription, error)` | 通配符订阅，只接收 T 类型事件 |
| `PublishTyped[T]` | `(bus, topic, T) → error` | 编译期类型检查的 Publish |
| `MustPublishTyped[T]` | `(bus, topic, T)` | 发布，失败 panic |
| `TypedChannel[T]` | struct | topic + type 绑定契约对象 |
| `NewTypedChannel[T]` | `(bus, topic) → TypedChannel[T]` | 创建通道（bus 可为 nil，用于全局契约常量） |
| `TypedChannel.WithBus` | `(bus) → TypedChannel[T]` | 绑定真实 EventBus（不修改原对象，线程安全） |
| `TypedChannel.Publish` | `(T) → error` | 类型安全发布 |
| `TypedChannel.Subscribe` | `(func(T)) → (Subscription, error)` | 类型安全订阅 |
| `TypedChannel.MustPublish` | `(T)` | 发布，失败 panic |
| `TypedChannel.MustSubscribe` | `(func(T)) → Subscription` | 订阅，失败 panic |

**推荐用法（跨插件事件契约）**：

```go
// events.go — 在共享类型文件中定义契约（bus=nil 表示"待绑定"）
var UserLoginEvent = plugin.NewTypedChannel[UserLogin](nil, "user.login")

// 发布方 Plugin（Setup 阶段）
ch := UserLoginEvent.WithBus(ctx.EventBus)
ch.MustPublish(UserLogin{UserID: "alice", IP: "1.2.3.4"})

// 订阅方 Plugin（Setup 阶段）
ch := UserLoginEvent.WithBus(ctx.EventBus)
ch.MustSubscribe(func(e UserLogin) {
    log.Printf("user %s logged in from %s", e.UserID, e.IP)
})
```

**向后兼容**：所有泛型函数构建在现有 `EventBus` 接口之上，旧代码无需修改。`EventHandler = func(any)` 的原始 API 继续有效。

---

#### P2-7：简化 Bot 构建路径（BotBuilder 统一入口）✅ 已完成（2026-03-02）

**变更文件**：`bot_builder.go`（新增 `WithPlugins`）、`factory.go`（`NewBotWithDefault` 委托给 BotBuilder）

**实际实现**：

- `BotBuilder.WithPlugins(...)` 一步注册多个插件，`Build()` 自动调用 `RegisterMultipleV2Smart`
- `NewBotWithDefault` 不再直接构建 adapter，统一委托给 `BotBuilder`

```go
bot, err := remilia.NewBotBuilder().
    WithBotInfo(info).
    WithWebhook(":8080").
    WithPlugins(myPlugin.New(), anotherPlugin.New()).
    Build()

bot.Start() // 自动触发所有插件 Setup（依赖序）
bot.Stop()  // 自动触发所有插件 Teardown（逆序）
```

---

### 5.4 P3 — 长期演进

| 方向 | 说明 |
|---|---|
| **插件 Sandbox** | 为不信任插件提供 goroutine 资源限制（CPU/内存 quota），防止单插件耗尽资源 |
| **Plugin Market** | 标准化 `PluginDescriptor.Meta` 中的 `Repository` 字段，支持从远程仓库安装插件 |
| **Schema 驱动的 /help** | `command.Definition` 中的元数据自动生成 `/help` 命令响应，无需插件手写帮助文本 |
| **OpenTelemetry 深度集成** | 跨插件调用的 Trace Context 传播，span 自动绑定到 `core/context.Context` |
| **配置 JSON Schema 导出** | `PluginAdvanced.ConfigSchema` 支持导出为 JSON Schema，为配置编辑器提供自动补全 |

---

## 6. 总结与优先级决策

### 当前完成状态（截至 2026-03-02）

```
✅ P0-1: Bot 集成 plugin.Manager
✅ P0-2: 消除 middleware → core/engine 反向依赖
✅ P0-3: 修复 BotBuilder 顺序依赖
✅ P1-1: 拆分 core/context/context.go（685行 → 5文件）
✅ P1-2: 裁剪 MatcherCoordinator 接口（8方法 → 2×4）
✅ P1-3: 修复 compiledChain XOR 指纹 bug（改为 FNV-1a）
✅ P1-4: plugin.Manager viper 依赖可选化（ConfigProvider 接口）
✅ P1-5: 拆分 plugin/register.go 单函数逻辑
✅ P2-1: 统一命令系统（syncToRegistry 双写）
✅ P2-4: 全局 goroutine 状态视图（ListAllGoroutines + GoroutineSummary）
✅ P2-5: permission.Role map 化（O(1) 查找）
✅ P2-7: 简化 Bot 构建路径（WithPlugins + NewBotWithDefault 委托）

✅ P2-6: EventBus 泛型类型安全 API（Subscribe[T] / TypedChannel[T]）

⏳ P2-2: 插件声明式权限注册（待实现）
⏳ P2-3: 中间件链运行时可观测性（待实现）
⏳ P3:   长期演进项目（按需推进）
```

### 最终结论

Remilia 框架的**内部机制质量较高**（COW 引擎、插件 DI 容器、热重载策略等）。经过本轮重构，**所有 P0/P1 结构性问题和主要 P2 体验问题均已解决**，框架现在具备：

- **统一的 Bot 构建入口**：`BotBuilder.WithPlugins(...)` 一步到位
- **正确的架构分层**：middleware → infra/dlq ← core/engine，无反向依赖
- **O(1) 权限查找**：Role.permissions 改为 map
- **命令系统双写一致**：commandIndex + Registry 自动同步
- **全局可观测性**：ListAllGoroutines + GoroutineSummary
- **类型安全 EventBus**：`TypedChannel[T]` 跨插件事件契约，编译期类型保证

```go
// 重构后的使用体验
bot, err := remilia.NewBotBuilder().
    WithBotInfo(info).
    WithWebhook(":8080").
    WithPlugins(myPlugin1.New(), myPlugin2.New()).
    Build()

bot.Start() // 自动触发所有插件 Setup
bot.Stop()  // 自动触发所有插件 Teardown（逆序）
```

---

*报告生成时间：2026-03-02*  
*基于代码快照分析，实际问题以最新代码为准*

