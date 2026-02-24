# 功能缺口与插件需求分析报告

**项目**: remilia — QQ Bot Framework  
**日期**: 2026-02-22  
**分析人**: GitHub Copilot  
**基准版本**: v2.0.0

---

## 目录

1. [现有能力全景图](#1-现有能力全景图)
2. [缺失功能分析](#2-缺失功能分析)
3. [缺失插件分析](#3-缺失插件分析)
4. [openapi / DTO 层缺口](#4-openapi--dto-层缺口)
5. [框架级能力缺口](#5-框架级能力缺口)
6. [价值评估与优先级路线图](#6-价值评估与优先级路线图)

---

## 1. 现有能力全景图

### 核心引擎
| 能力 | 状态 |
|------|------|
| 事件驱动处理引擎（COW 无锁读取） | ✅ |
| Matcher 注册 / 规则匹配 / 优先级 | ✅ |
| 命令解析器（子命令、参数、trie 索引） | ✅ |
| 中间件链（可热替换、内容指纹缓存） | ✅ |
| Webhook 适配器 | ✅ |
| 优雅关闭 / 生命周期管理 | ✅ |

### 插件系统
| 插件 | 状态 |
|------|------|
| `permission` — RBAC 权限系统 | ✅ |
| `admin` — 插件管理 + 权限管理指令 | ✅ |
| `help` — 命令帮助查询 | ✅ |
| `cache` — LRU 内存缓存 | ✅ |
| `storage` — 统一存储抽象（内存后端） | ✅ |
| `debug` — 事件/上下文/运行时调试 | ✅ |

### 中间件
| 中间件 | 状态 |
|--------|------|
| `Logging` / `Recover` / `Auth` / `Timeout` | ✅ |
| `RateLimit`（令牌桶，基于 x/time/rate） | ✅ |
| `AdaptiveRateLimiter`（CPU 感知自适应限流） | ✅ |
| `Retry`（指数退避，可配置策略） | ✅ |
| `CircuitBreaker`（熔断器，三态） | ✅ |
| `Degradation`（系统降级，CPU/内存感知） | ✅ |
| `DeadLetter`（死信队列集成） | ✅ |
| `DedupFilter`（事件去重） | ✅ |
| `SlowHandler`（慢处理器检测） | ✅ |
| `Tracing`（OpenTelemetry 集成） | ✅ |
| `Prometheus`（metrics 导出） | ✅ |

### 基础设施
| 组件 | 状态 |
|------|------|
| `logger`（zerolog 结构化日志） | ✅ |
| `metrics`（Prometheus，独立 Registry） | ✅ |
| `tracing`（OTLP/Zipkin） | ✅ |
| `health`（健康检查 HTTP 端点） | ✅ |
| `audit`（审计日志） | ✅ |
| `dlq`（死信队列，多策略） | ✅ |
| `httpclient`（带重试的 HTTP 客户端） | ✅ |
| `pool`（对象池） | ✅ |
| `pprof`（性能分析服务） | ✅ |

---

## 2. 缺失功能分析

### 2.1 🔴 会话/状态机（Conversation / FSM）

**现状**：框架只有无状态的 Matcher — 事件到来，规则匹配，处理，结束。没有跨消息的状态跟踪。

**典型需求场景**：
- 用户输入 `/register` → bot 回复"请输入昵称" → 等待用户下一条消息 → 记录昵称 → 继续问邮箱…
- 多步骤表单填写、问卷、引导式配置
- 游戏类 Bot（文字 RPG、猜谜）

**缺失内容**：
- `ConversationManager`：按 `(userOpenID, sessionID)` 存储当前会话步骤
- `Step` 定义：每步期望的输入格式、超时、取消指令
- `Conversation` 中间件或 Matcher 扩展：拦截符合条件的后续消息路由给活跃会话

**价值评估**：⭐⭐⭐⭐⭐ **极高**  
这是与用户互动类 Bot 的核心需求，几乎所有功能型 Bot（注册、配置、游戏）都需要。当前完全缺失，用户需要自己实现状态机，门槛极高。

---

### 2.2 🔴 消息模板 / 消息构建器（Message Builder）

**现状**：`dto.Message` 结构直接暴露给开发者，需要手动填充 `Type`、`Content`、`Markdown` 等字段；`dto.At()` / `dto.AtAll()` 是唯一的辅助函数。发送 Ark 消息需要手动构造嵌套 `KV`，极易出错。

**缺失内容**：
```go
// 期望的 API 形态
msg := message.NewBuilder().
    Text("Hello ").At(userOpenID).Text("!\n").
    Bold("操作成功").
    Image("https://...").
    Build()

// Ark 卡片构建器
card := message.NewArkCard(templateID).
    KV("title", "通知").
    KV("desc", "内容").
    Build()
```
- 链式消息构建器，封装 `Text / At / Image / Markdown / Ark / Media`
- 模板渲染（`go template` 或简单占位符替换）
- 消息长度自动分割（QQ 消息有长度限制）

**价值评估**：⭐⭐⭐⭐⭐ **极高**  
影响每一个功能实现，降低开发体验负担，是框架易用性的核心。`context.Reply` 目前只接受 `*dto.Message`，没有高层辅助。

---

### 2.3 🟠 计划任务 / 定时器（Scheduler / Cron）

**现状**：完全没有。Bot 内无法做"每天9点发送日报"、"每5分钟清理缓存"这类计划任务，用户必须自己引入 `robfig/cron` 并手动管理生命周期。

**缺失内容**：
- `Scheduler` 组件，实现 `lifecycle.Component` 接口，随 Bot 启停
- 支持 cron 表达式 和 `time.Duration` 两种方式
- 任务注册 API：`bot.Cron("0 9 * * *", func() { ... })`
- 任务并发保护（同一任务不允许重入）
- 与 `audit` / `logger` 集成，记录任务执行历史

**价值评估**：⭐⭐⭐⭐ **高**  
运营类 Bot（推送通知、定时问候、数据汇报）的必备功能，接入成本极低，生命周期管理已有成熟基础设施可复用。

---

### 2.4 🟠 事件过滤 / 路由增强（Advanced Routing）

**现状**：规则（`Rule`）是 `func(*Context) bool` 的简单谓词，且仅在注册时静态绑定。无法做：
- 动态路由（运行时按配置决定哪个 Matcher 处理）
- 正则内容匹配规则（`OnRegex(pattern)`）
- 内容关键词匹配（`OnContains("关键词")`，有但仅作为示例未封装为 Rule）
- 群/用户范围的动态黑白名单（需实时读取数据库）

**缺失内容**：
```go
engine.On(dto.GroupAtMessageCreate,
    rule.Regex(`^/order\s+(\d+)`),    // 正则提取
    rule.NotInCooldown(userID, 10*time.Second), // 冷却时间规则
    rule.AtBot(),                      // 仅当消息 @了 Bot
).Handle(...)
```
- `rule` 子包，提供常用规则工厂函数
- `OnRegex` — 正则匹配并将捕获组注入 context
- `OnAtBot` — 消息是否 @ 了 Bot
- `OnCooldown` — 用户级冷却时间规则（防刷）
- 动态路由表（运行时可增删路由，不重启）

**价值评估**：⭐⭐⭐⭐ **高**  
正则匹配是最常见的消息触发需求，`OnCooldown` 是防刷的刚需，两者开发成本低而收益极高。

---

### 2.5 🟠 消息发送队列 / 频控（Message Queue / Rate Control）

**现状**：`ctx.Reply()` 直接调用 OpenAPI HTTP 接口，无任何队列缓冲。QQ 官方 API 有频控限制（如每秒最多发送 N 条）。大量并发回复时可能触发 429，且无重试/排队机制。

**缺失内容**：
- 发送队列，异步 + 令牌桶频控，防止 API 被打满
- 按群/用户分桶的独立频控（不同群的频控互不影响）
- 发送失败自动重试（区分 429 临时错误和永久错误）
- 批量消息合并（短时间内对同一用户/群的多条消息合并发送）

**价值评估**：⭐⭐⭐⭐ **高**  
生产环境必需，当前架构在高并发回复时存在被封禁的风险。

---

### 2.6 🟡 插件间通信增强（Plugin IPC）

**现状**：`plugin.EventBus` 提供基础的发布/订阅，但：
- 无同步 RPC 语义（请求-响应模式）
- 无消息持久化
- 无跨进程通信（多实例部署）
- 订阅者没有消息过滤能力（只能按 topic 粗粒度过滤）

**缺失内容**：
- 请求-响应模式：`bus.Request("plugin.query", data, timeout)` 等待响应
- 消息过滤器：订阅时附带谓词 `Subscribe(topic, filter, handler)`

**价值评估**：⭐⭐⭐ **中等**  
单进程场景下已够用，复杂插件编排时才成为瓶颈。

---

### 2.7 🟡 热重载配置到中间件的传播（Config Hot-Reload Bridge）

**现状**：`config.Watcher` 能检测文件变更并触发 `Subscribe` 回调，但中间件（`RateLimit`、`CircuitBreaker`、`Retry`）的参数是在创建时固定的，没有 `Update(newConfig)` 方法。

**缺失内容**：
- 各中间件实现 `Reconfigure(cfg)` 接口
- `config.Subscribe` 回调中统一推送新参数给所有已注册的可重配置中间件
- 示例：QPS 限制从配置文件热改，不重启 Bot 即生效

**价值评估**：⭐⭐⭐ **中等**  
运营场景有需求，但可通过 Watcher + 重建中间件绕过，优先级中等。

---

### 2.8 🟡 消息撤回 / 编辑 API

**现状**：`OpenAPI` 接口只有 `SingleChat / GroupChat / SingleRichMedia / GroupRichMedia / SingleReset / GroupReset`，其中 `Reset` 语义不清晰（代码注释为"reset a message"）。没有明确的消息撤回（`Recall`）、消息编辑 API。

**缺失内容**：
- `RecallMessage(groupID/openID, messageID)` — 撤回消息
- `EditMessage(...)` — 编辑已发送消息（如有 API 支持）
- 语义化命名：`Reset` → `Recall`，避免歧义

**价值评估**：⭐⭐⭐ **中等**  
依赖 QQ 官方 API 支持，若 API 已存在则只需封装；若不存在则无法实现。

---

## 3. 缺失插件分析

### 3.1 🔴 `i18n` — 国际化/本地化插件

**现状**：所有内置插件的回复文本（`help`、`admin`、`permission`）均为**硬编码中文**，无法适应多语言需求。

**设计方案**：
```go
// plugins/core/i18n
type Plugin struct {
    bundles map[string]*Bundle  // locale -> Bundle
    default_ string
}

// 使用方式
i18n := ctx.MustGet("i18n").(*i18n.Plugin)
msg := i18n.T(ctx, "welcome.message", map[string]any{"name": user})
```
- 支持 YAML/JSON 语言文件
- 自动从用户 Profile 检测语言偏好（若 API 提供）
- 回退链：用户语言 → 群语言 → 默认语言
- 复数形式支持
- 与 `config.Watcher` 集成，热更新语言包

**价值评估**：⭐⭐⭐⭐ **高**  
一旦 Bot 面向多语言用户，硬编码文本是致命缺陷。且该插件一旦缺失，所有其他插件都需要各自维护多语言文本，代价极高。

---

### 3.2 🔴 `sqlite-storage` / `redis-storage` — 持久化存储后端

**现状**：`storage` 插件提供了良好的 `Storage` 接口抽象，但只有内存实现（`MemoryStorage`）。进程重启后所有数据丢失，无法用于：
- 权限数据持久化（当前权限系统重启后需重新初始化）
- 用户统计、签到记录
- 会话状态持久化（对应 2.1 的 Conversation）

**设计方案**：

**`sqlite-storage`**（examples 目录已有 demo，但未作为正式插件）：
```go
// plugins/storage/sqlite
backend := sqlite.NewSQLiteStorage("data/bot.db",
    sqlite.WithMaxConnections(10),
    sqlite.WithWALMode(),
)
storage := storage.NewV2WithBackend(backend)
```

**`redis-storage`**（多实例部署必需）：
```go
// plugins/storage/redis
backend := redis.NewRedisStorage(redis.Config{
    Addr: "localhost:6379",
    DB:   0,
    KeyPrefix: "remilia:",
})
```

**价值评估**：⭐⭐⭐⭐⭐ **极高（SQLite）/ ⭐⭐⭐⭐ 高（Redis）**  
SQLite 是单机部署的刚需，零额外依赖，examples 中已有原型代码，完善成插件的工作量极低。Redis 面向分布式场景，优先级次之。

---

### 3.3 🟠 `scheduler` — 计划任务插件

**现状**：框架缺失（见 2.3）。

**设计方案**：
```go
// plugins/core/scheduler
desc := scheduler.New()
// 在其他插件的 Setup 中使用：
sched := ctx.MustGet("scheduler").(*scheduler.Plugin)
sched.Every(24*time.Hour).At("09:00").Do(func() {
    bot.SendGroupMessage(groupID, dailyReport())
})
sched.Cron("*/5 * * * *", cleanupExpiredSessions)
```
- 依赖 `lifecycle.Component`，随 Bot 自动启停
- 任务注册 API 同时支持 `time.Duration`（简单间隔）和 cron 表达式
- 任务幂等保护（同一任务执行中不重入）
- 任务执行历史记录到 `audit`
- 向 `metrics` 上报任务执行次数和耗时

**价值评估**：⭐⭐⭐⭐ **高**  
运营型 Bot 的核心需求，与现有基础设施高度契合，实现成本低。

---

### 3.4 🟠 `anti-spam` — 反垃圾/防刷插件

**现状**：`DedupFilter` 中间件只做事件去重（按 `eventID`），`AdaptiveRateLimiter` 是全局限流。没有针对单个用户/群的细粒度防刷插件。

**设计方案**：
```go
// plugins/core/anti-spam
desc := antispam.New(antispam.Config{
    UserRateLimit:   5,                // 单用户每分钟最多5条命令
    GroupRateLimit:  30,               // 单群每分钟最多30条
    MuteOnViolation: true,             // 违规时自动静默（加入黑名单 N 分钟）
    MuteDuration:    5 * time.Minute,
    OnViolation: func(ctx *eventctx.Context, reason string) {
        ctx.Reply(message.Text("操作太频繁，请稍后再试"))
    },
})
```
- 用户级令牌桶（与 `storage` 插件集成实现持久化）
- 群级令牌桶
- 关键词过滤（支持正则）
- 违规累积计分，达到阈值自动封禁
- 封禁名单管理（临时 / 永久）

**价值评估**：⭐⭐⭐⭐ **高**  
公开运营的 Bot 必须具备防刷能力，否则一个恶意用户即可打垮服务。

---

### 3.5 🟠 `broadcast` — 广播/推送插件

**现状**：没有主动推送机制。Bot 只能响应用户消息，无法主动向多个用户/群发送消息（如公告、通知）。

**设计方案**：
```go
// plugins/core/broadcast
broadcaster := ctx.MustGet("broadcast").(*broadcast.Plugin)

// 向所有订阅的群发送公告
broadcaster.ToGroups(groupIDs...).Send(msg)

// 向标签为 "vip" 的用户发送推送
broadcaster.ToTaggedUsers("vip").SendTemplate("vip.notify", data)

// 配合 scheduler 定时推送
sched.Daily("08:00", func() {
    broadcaster.ToAll().Send(morningGreeting())
})
```
- 发送队列 + 频控（对应 2.5 的消息队列）
- 订阅管理：用户/群主动订阅/退订
- 发送报告：成功/失败统计
- 依赖 `storage` 存储订阅关系

**价值评估**：⭐⭐⭐⭐ **高**  
通知类、运营类 Bot 的核心功能，且可复用消息队列基础设施。

---

### 3.6 🟠 `stats` — 用户行为统计插件

**现状**：`infra/metrics` 有 Prometheus 指标，但面向运维（QPS、延迟）。没有面向业务的用户行为统计：哪些命令最常用、活跃用户排名、日/周/月 UV 等。

**设计方案**：
```go
// plugins/core/stats
stats := ctx.MustGet("stats").(*stats.Plugin)

// 自动中间件模式（无需手动调用）
engine.Use(stats.Middleware())

// 查询 API
top := stats.TopCommands(10, stats.Last7Days)
active := stats.ActiveUsers(stats.Today)
```
- 自动记录每次命令执行（通过中间件）
- 按时间窗口聚合（今日/本周/本月）
- 数据存储到 `storage` 后端
- 管理命令：`/stats commands`、`/stats users`
- 与 `admin` 插件集成，管理员可查看统计

**价值评估**：⭐⭐⭐ **中等**  
对运营决策有价值，但非核心功能，优先级低于 storage 和 scheduler。

---

### 3.7 🟡 `verification` — 入群验证插件

**现状**：框架处理了 `GROUP_ADD_ROBOT`（机器人被添加到群）、`FRIEND_ADD` 等生命周期事件，但没有入群验证场景的插件。

**设计方案**：
- 监听 `GROUP_ADD_ROBOT` 事件，自动发送欢迎语 + 验证题
- 结合 2.1 会话系统实现多步骤验证
- 验证通过后通过 `permission` 插件授权

**价值评估**：⭐⭐ **低**  
需要依赖 Conversation 系统（2.1）才能完整实现，且场景较特定。

---

### 3.8 🟡 `oauth` — 账号绑定插件

**现状**：无用户账号体系。Bot 内的用户身份仅限 `UserOpenID`（QQ 的匿名标识），无法与业务系统的用户账号关联。

**设计方案**：
```go
// plugins/core/oauth
// 用户发 /bind → Bot 返回绑定链接 → 用户在 Web 完成 OAuth → Bot 收到回调
oauth := ctx.MustGet("oauth").(*oauth.Plugin)
if !oauth.IsBound(userOpenID) {
    url := oauth.GenerateBindURL(userOpenID, "state")
    ctx.Reply(message.Text("请点击链接绑定账号: ").Link(url))
}
```
- 生成绑定链接（内置 HTTP 服务器接收回调）
- 支持多种 OAuth Provider（微信、GitHub 等）
- 绑定关系持久化（依赖 `storage`）
- 权限提升（绑定后自动授予更高角色）

**价值评估**：⭐⭐ **低**  
需要外部 HTTP 服务支持，实现复杂，适合有特定业务需求的项目自行实现。

---

## 4. openapi / DTO 层缺口

### 4.1 🔴 频道（Guild/Channel）事件缺失

**现状**：`event.go` 末尾有注释 `// TODO: Add channel event`，说明频道相关事件类型（文字频道消息、私信等）尚未实现。

**QQ Bot API v2 支持但框架未覆盖的事件**：

| 事件类型 | 常量 | 说明 |
|---------|------|------|
| `DIRECT_MESSAGE_CREATE` | 未定义 | 私信频道消息 |
| `AT_MESSAGE_CREATE` | 未定义 | 频道 @Bot 消息 |
| `MESSAGE_CREATE` | 未定义 | 频道所有消息（需订阅） |
| `AUDIO_OR_LIVE_CHANNEL_MEMBER_ENTER` | 未定义 | 进入音视频频道 |

**价值评估**：⭐⭐⭐⭐ **高**  
QQ 频道场景用户量大，完全缺失意味着框架不支持该场景，影响适用范围。

### 4.2 🟠 消息引用（Reply-to）支持

**现状**：`dto.Message` 没有 `reference` / `reply_to` 字段。QQ API 支持引用回复（在消息下方显示被引用的消息），但 DTO 层和 `ctx.Reply()` 均不支持。

```go
// 期望 API
ctx.ReplyTo(originalMsgID, message.Text("收到！"))
// 或
msg := message.NewBuilder().
    ReplyTo(originalMsgID).
    Text("收到！").
    Build()
```

**价值评估**：⭐⭐⭐ **中等**

### 4.3 🟡 按钮 / 键盘（Interactive Components）支持

**现状**：QQ Bot API v2 支持发送带有按钮的交互消息（内联键盘），用户点击按钮可触发回调事件。`dto.Message` 完全没有此类型。

**价值评估**：⭐⭐⭐⭐ **高**  
按钮交互是现代 Bot 体验的核心组成部分，大幅提升 UX，且 QQ API 已支持，主要是框架封装工作。

### 4.4 🟡 消息接收附件解析

**现状**：`MessageCreateEvent.Attachments` 字段已定义，但 `context.GetMessageContent()` 只返回文本内容，附件（图片、文件、语音）无对应的辅助方法。

```go
// 期望 API
images := ctx.GetAttachments(dto.ImageFile)
for _, img := range images {
    process(img.URL)
}
```

**价值评估**：⭐⭐⭐ **中等**

---

## 5. 框架级能力缺口

### 5.1 🔴 多适配器 / 多 Bot 实例支持

**现状**：`Bot` 是单例设计，`adapter` 是单个字段。无法在同一进程内运行多个 Bot 实例（如同时运营多个账号、测试账号与生产账号并行）。

**缺失内容**：
- `BotManager` 管理多个 `Bot` 实例
- 多适配器注册（如同时监听 Webhook 和长轮询）
- 实例间共享 `Engine` 或各自独立 Engine 的选择

**价值评估**：⭐⭐⭐ **中等**  
单 Bot 场景不需要，多账号运营场景是刚需。

### 5.2 🟠 插件依赖版本约束

**现状**：`PluginDescriptor.Deps` 只是 `[]string`（插件名），没有版本约束。插件 A 声明依赖 `permission`，但无法指定"需要 permission >= 2.0.0"。

**缺失内容**：
```go
Deps: []string{"permission@>=2.0.0", "storage@>=1.0.0"},
```
- SemVer 版本解析
- 依赖版本冲突检测
- 依赖图可视化（用于调试复杂依赖关系）

**价值评估**：⭐⭐⭐ **中等**  
插件生态成熟后才成为瓶颈，当前插件数量少，优先级低。

### 5.3 🟠 测试辅助工具（Testing Utilities）

**现状**：框架内部测试完善，但没有面向**框架使用者**的测试辅助库（如 Mock Bot、虚拟事件注入）。

**缺失内容**：
```go
// testing 子包
import "github.com/KomeiDiSanXian/remilia/testing"

// 创建测试 Bot（不启动 Webhook，直接注入事件）
tb := testing.NewTestBot()
tb.RegisterPlugin(myPlugin.New())
tb.Start()

// 注入虚拟事件
resp := tb.Send(testing.GroupMessage("hello", testing.User("openid-123")))
assert.Equal(t, "你好！", resp.Text())

// 断言命令调用
tb.AssertCommandCalled("/help", 1)
```
- `TestBot`：轻量 Bot 实例，无网络依赖
- 事件构造辅助函数
- 回复捕获和断言
- 时间模拟（用于测试定时任务）

**价值评估**：⭐⭐⭐⭐ **高**  
极大降低插件单元测试门槛，提升整个生态的代码质量。当前使用者基本无法对自己的插件写单元测试。

### 5.4 🟡 插件市场 / 远程加载

**现状**：插件必须在编译时引入，不支持运行时动态加载（Go 的 `plugin` 包有诸多限制，各平台行为不一致）。

**替代方案**：
- 插件配置中心：从 URL/Git 拉取插件配置（不是代码），将插件行为参数化
- 脚本插件：集成 `goja`（JS 引擎）或 `Lua`，允许运行时加载脚本插件

**价值评估**：⭐⭐ **低**  
Go 原生动态加载限制多，实现复杂，收益有限。Lua/JS 脚本方案可行但引入了语言边界，优先级低。

### 5.5 🟡 Web 管理面板

**现状**：`infra/server` 提供 HTTP 服务器，`health` 提供健康检查端点，`metrics` 提供 Prometheus 端点，`pprof` 提供性能分析端点。但没有可视化管理面板。

**缺失内容**：
- 嵌入式 Web UI（`embed.FS`）
- 实时查看插件状态、中间件指标
- 在线修改配置（通过 config.Watcher）
- 发送测试消息
- 查看审计日志

**价值评估**：⭐⭐⭐ **中等**  
开发和运维阶段有价值，但非核心功能，可作为独立的 `admin-ui` 示例项目。

---

## 6. 价值评估与优先级路线图

### 优先级评估矩阵

| # | 功能/插件 | 影响范围 | 实现成本 | 依赖关系 | 综合优先级 |
|---|-----------|---------|---------|---------|-----------|
| 1 | `sqlite-storage` 持久化后端 | 极高 | **低**（原型已存在） | storage 接口 | 🔴 P0 |
| 2 | 消息构建器（Message Builder） | 极高 | 低 | 无 | 🔴 P0 |
| 3 | 按钮/键盘 DTO 支持 | 高 | 低 | 无 | 🔴 P0 |
| 4 | 频道事件 DTO | 高 | 低 | 无 | 🔴 P0 |
| 5 | 计划任务插件（Scheduler） | 高 | 中 | lifecycle | 🟠 P1 |
| 6 | 会话/状态机（Conversation） | 极高 | **高** | storage, scheduler | 🟠 P1 |
| 7 | 测试辅助工具（TestBot） | 高 | 中 | 无 | 🟠 P1 |
| 8 | 反垃圾插件（Anti-Spam） | 高 | 中 | storage | 🟠 P1 |
| 9 | i18n 插件 | 高 | 中 | 无 | 🟠 P1 |
| 10 | 广播插件（Broadcast） | 高 | 中 | storage, scheduler | 🟠 P1 |
| 11 | 消息队列/频控（Send Queue） | 高 | 中 | 无 | 🟠 P1 |
| 12 | `rule` 子包（Regex/Cooldown） | 高 | 低 | 无 | 🟠 P1 |
| 13 | `redis-storage` 后端 | 中 | 中 | storage 接口 | 🟡 P2 |
| 14 | 消息引用（Reply-to） | 中 | 低 | dto | 🟡 P2 |
| 15 | 用户统计插件（Stats） | 中 | 中 | storage | 🟡 P2 |
| 16 | 配置热更新 → 中间件传播 | 中 | 中 | config.Watcher | 🟡 P2 |
| 17 | 多 Bot 实例（BotManager） | 中 | 高 | 无 | 🟡 P2 |
| 18 | 插件依赖版本约束 | 低 | 中 | plugin | ⚪ P3 |
| 19 | Web 管理面板 | 中 | 高 | 所有基础设施 | ⚪ P3 |
| 20 | 入群验证插件 | 低 | 中 | Conversation | ⚪ P3 |
| 21 | OAuth 账号绑定 | 低 | 高 | storage, HTTP | ⚪ P3 |

---

### 建议实施路线图

#### 第一阶段（1-2 周）—— 低成本高回报

**目标**：填补开发体验的最明显缺口，工作量小，价值立竿见影。

1. **消息构建器** `helper/message/builder.go`
   - 链式 API：`Text / At / Image / Ark / Markdown`
   - 消息长度自动分割
   
2. **按钮/键盘 DTO** `openapi/dto/keyboard.go`
   - `InlineKeyboard` 结构体
   - `Message.Keyboard` 字段

3. **频道事件 DTO** `openapi/dto/event.go`
   - 补充 `AT_MESSAGE_CREATE`、`DIRECT_MESSAGE_CREATE` 等常量和结构体

4. **`rule` 子包** `core/context/rule/`
   - `Regex(pattern)` — 正则匹配并注入捕获组
   - `Cooldown(duration)` — 用户冷却时间
   - `AtBot()` — 消息是否 @Bot

5. **`sqlite-storage`** `plugins/storage/sqlite/`
   - 将 `examples/sqlite-storage-demo` 提炼为正式插件

---

#### 第二阶段（2-4 周）—— 核心功能补完

**目标**：补齐生产环境运营 Bot 必需的功能。

6. **计划任务插件** `plugins/core/scheduler/`
7. **消息发送队列** `openapi/sendqueue/`（内部基础设施）
8. **反垃圾插件** `plugins/core/anti-spam/`
9. **广播插件** `plugins/core/broadcast/`（依赖 scheduler + storage）
10. **测试辅助工具** `testing/` 子包

---

#### 第三阶段（1-2 月）—— 生态完善

**目标**：支持复杂应用场景，构建差异化优势。

11. **会话/状态机** `plugins/core/conversation/`（最复杂，需详细设计）
12. **i18n 插件** `plugins/core/i18n/`
13. **Redis 存储后端** `plugins/storage/redis/`
14. **用户统计插件** `plugins/core/stats/`
15. **配置 → 中间件热更新传播**

---

### 关键路径说明

```
sqlite-storage (P0)
    ↓
anti-spam ──→ broadcast ──→ stats
    ↑               ↑
scheduler ──────────┘
    ↑
lifecycle (已有，直接复用)

conversation (最复杂)
    ↑
storage (已有接口) + scheduler (第二阶段)

Message Builder (P0, 独立)
rule 子包 (P0, 独立)
```

`sqlite-storage` 和 `scheduler` 是解锁后续大量功能的关键依赖，应优先实现。消息构建器和 `rule` 子包完全独立，且对开发体验影响最大，应第一个完成。

---

*报告生成时间: 2026-02-22*  
*建议定期（每季度）重新评估，根据实际用户反馈调整优先级*

