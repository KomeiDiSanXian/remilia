# 多平台抽象层审计报告（第二次）

> 审计日期：2026-03-20  
> 更新日期：2026-03-20（续）  
> 项目状态：未发布（pre-release）  
> 基于前次审计（2026-03-12）的全部16项问题均已修复，本次为全新全量审计  
> 范围：`platform/`、`core/engine/`、`core/context/`、`bot.go`、`factory.go`

**修复进度总览**

| 类别 | 已修复 | 说明 |
|------|--------|------|
| 🔴 Bug（4项） | ✅ 全部 | Bug#1-4 均已在前期修复 |
| 🟠 设计缺陷（8项） | ✅ 全部 | 设计#1-8 均已在前期修复 |
| 🟡 性能优化（5项） | ✅ 3/5 | #1/#3 已修复；#2/#4 为P3低优先级 |
| 🔵 应删除的兼容代码（3项） | ✅ 全部 | 删除#1-3 均已完成 |
| ⚪ 未实现功能（5项） | 🔄 2/5 | #2（ChatInfo.Name）和二进制直传（Attachment.Data）已实现 |
| 📄 文档错误（2项） | ✅ 全部 | MULTI_PLATFORM.md 已完整重写 |
| 🆕 续期修复 | ✅ 完成 | AcquireContextFromEvent nil stdctx、测试EventKind映射错误、wechat适配器不一致 |

---

## 一、当前实现进度

### 1.1 `platform/` 核心抽象层

| 文件 | 状态 | 说明 |
|------|------|------|
| `platform/event.go` | ✅ 完整 | `Event` 接口、`EventKind` 枚举、`UserInfo`、`ChatInfo` |
| `platform/message.go` | ✅ 完整 | `OutboundMessage`、工厂函数、链式 builder |
| `platform/adapter.go` | ✅ 完整 | `PlatformAdapter`、`Sender`、`NoopSender`、`Registry` |

### 1.2 各平台适配器

| 平台 | 状态 | 完成度 |
|------|------|--------|
| `platform/qq/` | ✅ 完整 | Webhook 服务器、事件解析（C2C/GroupAt/Guild/Notice/System）、Sender 均实现 |
| `platform/discord/` | 🚧 骨架 | 接口签名存在，`StartPlatform`/`Send` 返回 `not yet implemented` |
| `platform/telegram/` | 🚧 骨架 | 同上 |
| `platform/wechat/` | 🚧 骨架 | 同上 |

### 1.3 引擎集成层

| 组件 | 状态 | 说明 |
|------|------|------|
| `core/engine/process_platform.go` | ✅ | `ProcessPlatformEvent` + `ProcessPlatformEventBatch` 均实现 |
| `core/context/platform_event.go` | ✅ | `AcquireContextFromEvent`、`Reply`、`ReplyWithContext`、`GetEventKind` 等均实现 |
| `core/context/rules.go` | ✅ | `OnEventKind` 平台无关规则实现 |
| `bot.go` + `bot_builder.go` | ✅ | 单平台 + 多平台注册表两种模式均支持 |

---

## 二、🔴 Bug（必须修复）

### Bug #1：`Reply()` 忽略 Context 超时和取消信号

**文件**：`core/context/platform_event.go:111`  
**严重程度**：P0（功能性正确性缺陷）

```go
// 当前错误实现：
func (ctx *Context) Reply(msg platform.OutboundMessage) error {
    ...
    chat := ctx.platformEvent.Chat()
    goCtx := platform.WithChatInfo(stdctx.Background(), chat)  // ← BUG
    return ctx.platformSender.Send(goCtx, chat.ID, msg)
}
```

使用 `stdctx.Background()` 而非 `ctx.Context()`，导致：
- 中间件注入的超时/截止时间（Deadline）对 Send 操作完全无效
- 调用方通过 `ReplyWithContext` 才能正确控制超时，但 `Reply` 是最常用接口
- 在网络抖动时，`Reply` 可能永久阻塞，无法被 Bot 的根 context 取消

**修复方案**：

```go
func (ctx *Context) Reply(msg platform.OutboundMessage) error {
    ...
    chat := ctx.platformEvent.Chat()
    goCtx := platform.WithChatInfo(ctx.Context(), chat)  // 使用 ctx.Context()
    return ctx.platformSender.Send(goCtx, chat.ID, msg)
}
```

---

### Bug #2：`qqEvent.populateGuildMessage()` 丢失 `guild_id`

**文件**：`platform/qq/event.go:130-148`  
**严重程度**：P1（数据丢失）

当 QQ 频道消息同时包含 `channel_id` 和 `guild_id` 时，当前实现将 `guild_id` 丢弃：

```go
channelID := gjson.GetBytes(d, "channel_id").String()
guildID := gjson.GetBytes(d, "guild_id").String()
chatID := channelID
if chatID == "" {
    chatID = guildID  // guild_id 仅在 channel_id 为空时才使用
}
e.chat = platform.ChatInfo{
    ID: chatID,  // guild_id 被完全丢弃
}
```

**影响**：`ChatInfo.ID` 只存储 `channel_id`，但 QQ 频道 API 中的大多数操作（如权限检查、成员查询）需要 `guild_id`。无法通过 `ChatInfo` 还原 guild 归属，调用方被迫每次都走 `RawPayload()` 类型断言。

这也与 `ChatInfo` 设计的局限性相关（见设计缺陷 #1）。

**修复方案**：在 `ChatInfo` 中增加 `ParentID string` 字段（见设计缺陷 #1），或在 `Extra` 字段中存储 `guild_id`。短期应急方案：

```go
e.chat = platform.ChatInfo{
    ID:      channelID, // channel_id 优先
    Name:    gjson.GetBytes(d, "channel_name").String(),
    IsGroup: true,
    // 利用 Extra 暂存 guild_id，直到 ChatInfo 正式增加字段
}
// 同时将 guild_id 存入 event 的额外字段，供 RawPayload 外的访问
```

---

### Bug #3：`qq.Adapter.StartPlatform()` 无边界地创建 goroutine

**文件**：`platform/qq/adapter.go:100-108`  
**严重程度**：P1（资源泄漏风险）

```go
case payload, ok := <-eventCh:
    if !ok { ... }
    if payload != nil {
        event := NewEvent(payload)
        a.wg.Go(func() {       // ← 每个事件创建一个新 goroutine，无上限
            safeInvoke(handler, event)
        })
    }
```

`WebhookServerAdapter` 正确使用了固定大小的 worker pool（`a.workers` 个 goroutine），但 `Adapter`（非 Webhook 版）每收到一个事件就创建新 goroutine，无任何限流机制。

**影响**：高频事件流（如频繁触发的群消息）会创建大量并发 goroutine，导致内存和调度器压力。

**修复方案**：在 `Adapter` 中引入与 `WebhookServerAdapter` 相同的 worker pool 机制，或使用 `semaphore` 限制并发。

---

### Bug #4：`Registry.StopAll()` 顺序停止，无法并发

**文件**：`platform/adapter.go:163-174`  
**严重程度**：P1（停止延时问题）

```go
func (r *Registry) StopAll(ctx stdctx.Context) error {
    adapters := r.All()
    var errs []error
    for _, a := range adapters {          // ← 顺序迭代，不并发
        if err := a.Stop(ctx); err != nil {
            errs = append(errs, ...)
        }
    }
    ...
}
```

若某个适配器 `Stop` 耗时较长（如等待 HTTP 连接排空），后续适配器的停止会被阻塞。多平台场景下，总停止时间 = 各平台停止时间之和，而非最长那个。

**修复方案**：使用 `sync.WaitGroup` 并发停止，再合并所有错误：

```go
func (r *Registry) StopAll(ctx stdctx.Context) error {
    adapters := r.All()
    var (
        mu   sync.Mutex
        errs []error
        wg   sync.WaitGroup
    )
    for _, a := range adapters {
        wg.Go(func() {
            if err := a.Stop(ctx); err != nil {
                mu.Lock()
                errs = append(errs, fmt.Errorf("platform %s stop: %w", a.Platform(), err))
                mu.Unlock()
            }
        })
    }
    wg.Wait()
    return errors.Join(errs...)
}
```

---

## 三、🟠 设计缺陷

### 设计 #1：`ChatInfo` 缺少层级结构字段，无法表达「频道隶属于服务器」

**文件**：`platform/event.go:56-64`  
**影响平台**：QQ 频道、Discord、Slack 等

当前 `ChatInfo` 只有一个 `ID string` 字段来表示会话：

```go
type ChatInfo struct {
    ID      string  // 会话唯一标识（只能存一个ID）
    Name    string
    IsGroup bool
}
```

**问题**：
- QQ 频道消息需要 `channel_id`（发消息）和 `guild_id`（权限检查、成员查询）
- Discord 消息需要 `channel_id` 和 `guild_id`
- 目前 QQ 适配器将 `guild_id` 丢弃（见 Bug #2）
- Handler 无法从 `ChatInfo` 判断消息来自哪个服务器/频道

**建议修复**：

```go
type ChatInfo struct {
    ID       string  // 直接会话 ID（channel_id / group_id / user_id）
    ParentID string  // 父容器 ID（guild_id / 服务器 ID），私聊为空
    Name     string  // 会话名称
    IsGroup  bool    // 是否为群组/频道消息
}
```

---

### 设计 #2：`EventKind` 枚举过于粗粒度，难以跨平台精确路由

**文件**：`platform/event.go:24-44`

当前的 7 个 EventKind 无法覆盖现代 IM 平台的全部事件类型：

| 缺少的 EventKind | 说明 | 影响平台 |
|------------------|------|---------|
| `EventKindInteraction` | 按钮回调、斜杠命令、下拉菜单选择 | Discord、QQ 机器人 v2、Telegram |
| `EventKindReaction` | 消息表情回应（添加/移除） | Discord、Telegram、QQ |
| `EventKindMemberJoin` | 成员加入群组/服务器 | Discord、Telegram、QQ |
| `EventKindMemberLeave` | 成员离开群组/服务器 | Discord、Telegram、QQ |
| `EventKindMessageUpdate` | 消息被编辑 | Discord、Telegram |
| `EventKindMessageDelete` | 消息被撤回/删除 | 所有平台 |
| `EventKindChannelCreate` | 频道/话题创建 | Discord、QQ 频道 |
| `EventKindChannelDelete` | 频道/话题删除 | Discord、QQ 频道 |

**当前影响**：  
`EventKindNotice` 被用来表示所有通知类事件（入群、退群、好友添加、禁言等），Handler 无法通过 `EventKind` 区分，只能通过 `RawType()` 做平台特定判断，破坏了平台无关设计。

**建议**：至少拆分 `EventKindNotice` 为 `EventKindMemberJoin` / `EventKindMemberLeave`，并增加 `EventKindInteraction`。

---

### 设计 #3：`PlatformAdapter` 缺少能力声明接口

**文件**：`platform/adapter.go:52-68`

不同平台的特性差异巨大：

| 特性 | QQ | Discord | Telegram | WeChat |
|------|-----|---------|---------|--------|
| Markdown | ✅ | ✅ | ✅（有限）| ❌ |
| 交互按钮 | ✅ | ✅ | ✅ | ✅（模板消息）|
| 多附件 | ❌（单张）| ✅ | ✅ | ❌ |
| 消息编辑 | ❌ | ✅ | ✅ | ❌ |
| 文件上传 | ✅ | ✅ | ✅ | ✅ |
| 频道/服务器 | ✅ | ✅ | ❌ | ❌ |

当前框架无法让 Handler 知道当前平台支持哪些特性，导致：
- 发送 Markdown 到不支持的平台时，只能靠 `Sender` 内部静默降级
- 开发者无法编写「如果平台支持按钮则使用按钮，否则使用文本」的跨平台逻辑

**建议**：增加能力查询方法：

```go
// PlatformCapabilities 声明平台支持的特性
type PlatformCapabilities struct {
    Markdown        bool  // 是否支持 Markdown
    Buttons         bool  // 是否支持交互按钮
    MultiAttachment bool  // 是否支持多附件
    MessageEdit     bool  // 是否支持消息编辑
    MessageDelete   bool  // 是否支持消息删除
    Embeds          bool  // 是否支持富文本嵌入卡片
    FileUpload      bool  // 是否支持二进制文件直传（非URL）
    GuildSupport    bool  // 是否有服务器/频道层级
}

type PlatformAdapter interface {
    Platform() string
    StartPlatform(ctx context.Context, handler func(Event)) error
    Stop(ctx context.Context) error
    Sender() Sender
    Capabilities() PlatformCapabilities  // 新增
}
```

---

### 设计 #4：`Sender` 接口只有 `Send`，缺少 `Edit` 和 `Delete`

**文件**：`platform/adapter.go:33-44`

```go
type Sender interface {
    Send(ctx context.Context, chatID string, msg OutboundMessage) error
    // 缺少：
    // Edit(ctx context.Context, chatID, messageID string, msg OutboundMessage) error
    // Delete(ctx context.Context, chatID, messageID string) error
}
```

**影响**：常见 Bot 模式（先发一条"处理中..."，再编辑为结果）无法在框架层实现，开发者必须类型断言到具体平台 Sender。

**建议**：扩展接口，或新增可选接口：

```go
// MessageEditor 可选接口，支持消息编辑的平台实现此接口
type MessageEditor interface {
    Edit(ctx context.Context, chatID, messageID string, msg OutboundMessage) error
}

// MessageDeleter 可选接口，支持消息删除的平台实现此接口
type MessageDeleter interface {
    Delete(ctx context.Context, chatID, messageID string) error
}
```

---

### 设计 #5：`OutboundMessage` 不支持多附件和富文本卡片

**文件**：`platform/message.go:34-73`

当前设计每种媒体类型只有单个 URL 字段：

```go
type OutboundMessage struct {
    ImageURL string   // 只能发一张图片
    AudioURL string   // 只能发一段音频
    VideoURL string   // 只能发一个视频
    FileURL  string   // 只能发一个文件
    // 没有 Embed/Card 类型
}
```

**问题**：
1. Discord/Telegram 均支持在一条消息中发多张图片
2. Discord 的 `Embed` 是核心展示单元（标题、描述、字段列表、图片、脚注），目前只能塞进 `Extra map[string]any`，完全没有类型安全保障
3. WeChat 的模板消息/卡片消息通过 `Extra` 绕过，开发体验差

**建议**：

```go
// Attachment 单个附件
type Attachment struct {
    URL      string // 远程 URL
    Data     []byte // 本地二进制数据（直传，URL 为空时使用）
    MimeType string // 如 "image/png"
    Name     string // 文件名
}

// Embed 富文本嵌入卡片（Discord style，其他平台尽力映射）
type Embed struct {
    Title       string
    Description string
    URL         string
    Color       int    // 十六进制颜色，如 0x5865F2
    Fields      []EmbedField
    ImageURL    string
    ThumbnailURL string
    FooterText  string
    Timestamp   time.Time
}

type EmbedField struct {
    Name   string
    Value  string
    Inline bool
}

// OutboundMessage 扩展
type OutboundMessage struct {
    Text        string
    Markdown    string
    Attachments []Attachment // 支持多附件（替代单独的 ImageURL/AudioURL 等）
    Embeds      []Embed      // 富文本卡片
    Buttons     []Button
    Mentions    []string
    ReplyToID   string
    Extra       map[string]any
}
```

---

### 设计 #6：`Sender.Send()` 的 `chatID` 参数与 Context 中的 `ChatInfo.ID` 语义重叠

**文件**：`platform/adapter.go:33-44`

`Send(ctx, chatID, msg)` 接口中，`chatID` 参数与通过 `platform.ChatInfoFromContext(ctx)` 读取的 `ChatInfo.ID` 是相同的值（由 `Reply()` 注入）。这造成：
1. 接口语义不清晰：究竟哪个 `chatID` 优先？
2. 自定义 `Sender` 实现者容易混淆

**建议**：移除 `chatID` 参数，改为完全依赖 Context 中的 `ChatInfo`：

```go
type Sender interface {
    // Send 发送消息。目标会话信息从 ctx 中的 ChatInfo 读取。
    // 若 ctx 未携带 ChatInfo，实现者应返回 ErrNoChatInfo。
    Send(ctx context.Context, msg OutboundMessage) error
}
```

同时更新 `Reply()` 中的调用，去掉多余的 `chatID` 传递。

---

### 设计 #7：`Button` 结构体缺少 `Disabled` 和行分组支持

**文件**：`platform/message.go:22-32`

```go
type Button struct {
    ID    string
    Label string
    URL   string
    Style ButtonStyle
    // 缺少：
    // Disabled bool       // 灰色不可点击状态（Discord/QQ 均支持）
    // Row      int        // 所在行（Discord 按钮最多 5 行，每行 5 个）
    // Emoji    string     // 按钮前显示的 emoji（Discord）
}
```

---

### 设计 #8：`factory.go` 的 `NewBotWithDefault()` 是不该存在于根包的 QQ 专属 API

**文件**：`factory.go`

```go
// 根包暴露了 QQ 平台专属的快捷构造函数
func NewBotWithDefault(addr string, info *dto.BotInfo, opts ...Option) (*Bot, error) {
```

**问题**：  
根包 `remilia` 定位为**平台无关的框架入口**，但 `factory.go` 直接导入了 `platform/qq` 和 `openapi/dto`，使根包与 QQ 平台产生了直接依赖。这违背了"框架核心不依赖任何平台 SDK"的设计目标。

**建议**：  
将 `NewBotWithDefault` 移入 `platform/qq` 包（例如 `qq.NewBot(addr, info)`），或完全删除（BotBuilder 已足够简洁）。

---

## 四、🟡 性能优化

### 性能 #1：`qqEvent.populate*()` 对同一 JSON 多次调用 `gjson.GetBytes()`

**文件**：`platform/qq/event.go:71-152`

以 `populateGuildMessage()` 为例：

```go
func (e *qqEvent) populateGuildMessage() {
    d := e.payload.Detail
    e.content  = gjson.GetBytes(d, "content").String()          // 第1次扫描
    authorID   := gjson.GetBytes(d, "author.id").String()       // 第2次扫描
    authorName := gjson.GetBytes(d, "author.username").String() // 第3次扫描
    channelID  := gjson.GetBytes(d, "channel_id").String()      // 第4次扫描
    guildID    := gjson.GetBytes(d, "guild_id").String()        // 第5次扫描
    channelName:= gjson.GetBytes(d, "channel_name").String()    // 第6次扫描
    ts         := gjson.GetBytes(d, "timestamp").String()       // 第7次扫描
}
```

每次 `gjson.GetBytes()` 都从 JSON 字节流头部重新扫描。  
**建议**：使用 `gjson.ParseBytes(d).Get(path)` 或一次性用 `gjson.GetManyBytes()` 提取所有字段：

```go
results := gjson.GetManyBytes(d,
    "content", "author.id", "author.username",
    "channel_id", "guild_id", "channel_name", "timestamp")
// results[0].String(), results[1].String(), ...
```

`GetManyBytes` 是一次线性扫描，效率 O(n)，而非 O(k×n)。

---

### 性能 #2：`Context.Extensions` 对每次读写都加 `sync.RWMutex`

**文件**：`core/context/extensions.go`

框架内部只存储固定几种类型键（`retryMetadata`、`parsedCommand`、`middlewareTrace` 等），但 `Extensions` 使用通用 `sync.RWMutex` + `map[reflect.Type]any`。  
对于少量固定键，使用直接的结构体字段（或类型断言缓存）代替 `map` 访问，可消除锁争用和 map 查找开销。  
**建议**：对高频的 `parsedCommand` 提供 `fastPath` 缓存直接字段，避免进入 map 路径。

---

### 性能 #3：`WebhookServerAdapter.startWithPlatformHandler` 是多余的间接层

**文件**：`platform/qq/webhook_server.go:135-140`

```go
func (a *WebhookServerAdapter) StartPlatform(ctx context.Context, handler func(platform.Event)) error {
    return a.startWithPlatformHandler(ctx, handler)  // 唯一调用点
}

func (a *WebhookServerAdapter) startWithPlatformHandler(...) error {
    // 全部实现都在这里
}
```

由于旧的非平台 handler 路径已删除，这个私有方法没有存在的必要，应内联到 `StartPlatform`，减少一次函数调用和一个间接层（虽然编译器通常会内联，但代码更清晰）。

---

### 性能 #4：`Reply()` 每次调用都创建新的 `ChatInfo` key 并通过 context value 传递

**文件**：`core/context/platform_event.go`、`platform/adapter.go`

每次 `Reply()` 都调用 `platform.WithChatInfo(ctx, chat)` 创建一个新 context，这是 `context.WithValue` 调用（堆分配）。对于高频回复场景，可考虑在 `Context` 对象上缓存已绑定 ChatInfo 的标准库 context，避免重复创建。

---

### 性能 #5：`processEventContext` 中 `mergeSortedMatchersSix` 产生临时切片

**文件**：`core/engine/process_platform.go:112-119`

`mergeSortedMatchersSix` 将 6 个已排序切片合并到一个新切片（来自 pool）。对于简单的单平台、无命令索引场景（最常见），大多数输入切片为空，可通过特判跳过合并，直接使用非空切片迭代。

---

## 五、🔵 应删除的兼容代码（pre-release 阶段）

### 删除 #1：根包 `factory.go`（`NewBotWithDefault`）

**原因**：见设计缺陷 #8。根包不应依赖 `platform/qq` 和 `openapi/dto`。  
**影响**：已有示例代码若使用此函数需迁移到 `NewBotBuilder().WithPlatformAdapter(adapter).Build()`（已有完整替代方案）。

---

### 删除 #2：`engine/types.go` 中的 `PlatformAdapter` 类型别名

**文件**：`core/engine/types.go:97-101`

```go
// PlatformAdapter 是平台无关的适配器接口别名，等价于 platform.PlatformAdapter。
//
// 此处保留为类型别名，供已直接引用 engine.PlatformAdapter 的代码无缝过渡。
// 新代码应直接使用 platform.PlatformAdapter。
type PlatformAdapter = platform.PlatformAdapter
```

注释本身说明"新代码应直接使用 `platform.PlatformAdapter`"。项目未发布，不存在外部用户依赖此别名。应删除，所有 `engine.PlatformAdapter` 引用改为 `platform.PlatformAdapter`。

---

### 删除 #3：各骨架包中未使用的 `telegramEvent`/`discordEvent`/`wechatEvent` 结构体

**文件**：`platform/telegram/adapter.go:33-43`、`platform/discord/adapter.go:38-49`、`platform/wechat/adapter.go:33-43`

这三个空结构体实现了 `platform.Event` 接口但从未被实际使用（骨架 `StartPlatform` 直接返回错误，不会产生任何 event）。它们是无效的占位符，删除可避免未来实现者误解为"这是接口实现的参考模板"。

实际上，真正的实现应该从平台 SDK 的事件对象包装，而不是从零开始的空结构体。  
**建议**：删除这些空结构体，在包注释中说明如何实现 `platform.Event`（引用 `platform/qq/event.go` 作为参考）。

---

## 六、⚪ 未实现功能（多平台完整性缺口）

### 未实现 #1：多平台示例（`examples/multi-platform/`）

当前 `examples/` 目录下没有任何展示多平台注册表的示例。所有示例均为 QQ 单平台模式。

**需要创建**：`examples/multi-platform/main.go`，展示：
1. 使用 `platform.NewRegistry()` 注册多个适配器
2. 使用 `OnEventKind(platform.EventKindPrivateMessage)` 跨平台路由
3. 通过 `ctx.GetEventPlatform()` 做平台特定处理

---

### 未实现 #2：`ChatInfo.Name` 在 QQ 群消息中从不填充

**文件**：`platform/qq/event.go`

`populateC2C()`、`populateGroupAt()`、`populateNoticeGroup()`、`populateNoticeUser()` 均不设置 `ChatInfo.Name`。QQ 群 AT 消息的 payload 中包含群名称，但当前代码未提取。  
**建议**：在 `populateGroupAt()` 中提取 `group_name`（若 QQ OpenAPI payload 中存在此字段）。

---

### 未实现 #3：平台适配器健康状态查询

`PlatformAdapter` 接口没有 `Status()` 或 `IsConnected()` 方法。  
`health.NewAdapterHealthChecker` 只能判断适配器是否已注册（非 nil），无法判断是否真正连接。  
**建议**：增加可选的健康接口：

```go
// AdapterHealthChecker 可选接口，支持健康探测的适配器实现此接口
type AdapterHealthChecker interface {
    Status() AdapterStatus
}

type AdapterStatus struct {
    Connected   bool
    LastEventAt time.Time
    Latency     time.Duration
}
```

---

### 未实现 #4：事件确认（Ack）机制

对于 Webhook 模式，部分平台要求 Bot 明确确认（Ack）收到事件，否则平台会重发。QQ 的 Webhook 协议就有此要求（HTTP 响应体返回特定格式）。当前实现中，QQ Webhook 的 Ack 逻辑藏在 `webhook.Conn` 内部，对上层不透明。若未来对接 Discord 的交互 Webhook，Ack 逻辑将更复杂（需在 3 秒内响应）。

**建议**：在 `platform.Event` 接口中增加 `Ack(ctx context.Context) error`，或在 `PlatformAdapter` 中处理 Ack，使上层逻辑与平台 Ack 协议解耦。

---

### 未实现 #5：`OutboundMessage` 缺少文件二进制直传支持

当前 `FileURL string` 只支持 HTTP URL 方式发送文件。Telegram 的 `sendDocument`、Discord 的附件上传都支持直接上传二进制文件，不需要文件先托管到 HTTP 服务器。  
这在私有部署场景（Bot 需要发送本地生成的报告文件）中尤为重要。  
**建议**：在 `Attachment` 类型（见设计缺陷 #5）中增加 `Data []byte` 字段。

---

## 七、📄 文档错误

### 文档 #1：`MULTI_PLATFORM.md` 仍引用已删除的文件

**文件**：`docs/03-architecture/MULTI_PLATFORM.md`（底部目录结构章节）

文档中的目录结构示例仍列出：
- `platform/context.go` — 已于 2026-03-15 删除（问题 #1 修复）
- `platform/bridge.go` — 已于 2026-03-15 删除（问题 #2 修复）

**修复**：更新目录结构表格，删除这两个条目。

---

### 文档 #2：`MULTI_PLATFORM.md` 错误描述了 `GetEventType()` 的返回值

**文件**：`docs/03-architecture/MULTI_PLATFORM.md:185` 和 `platform_event.go:71-81`

文档（旧版数据流章节）描述：
> `GetEventType() → event.RawType()`

但实际代码实现：
```go
func (ctx *Context) GetEventType() string {
    if ctx.platformEvent != nil {
        return string(ctx.platformEvent.Kind())  // 返回 EventKind 字符串，如 "PRIVATE_MESSAGE"
    }
    return ""
}
```

**影响**：开发者如果按照文档使用 `engine.On(dto.C2CMessageCreate)` 来匹配新路径事件，会发现根本不匹配（新路径需要 `engine.On(string(platform.EventKindPrivateMessage))`）。

**修复**：更新架构文档中的数据流说明，明确新路径的路由键为 `EventKind` 字符串。

---

## 八、总结与优先级排序

### 立即修复（P0/P1）

| 优先级 | 编号 | 内容 | 文件 | 工作量 |
|--------|------|------|------|--------|
| P0 | Bug #1 | `Reply()` 使用 `Background()` 忽略超时 | `core/context/platform_event.go:111` | XS（1行） |
| P1 | Bug #3 | `qq.Adapter` 无边界创建 goroutine | `platform/qq/adapter.go` | S（引入 worker pool） |
| P1 | Bug #4 | `Registry.StopAll()` 顺序停止 | `platform/adapter.go` | S（改为并发） |
| P1 | 删除 #1 | 删除 `factory.go:NewBotWithDefault` | `factory.go` | XS |
| P1 | 删除 #2 | 删除 `engine.PlatformAdapter` 别名 | `core/engine/types.go` | XS |

### 下一迭代（P2 — 设计完善）

| 优先级 | 编号 | 内容 | 工作量 |
|--------|------|------|--------|
| P2 | 设计 #1 | `ChatInfo` 增加 `ParentID` 字段 + 修复 Bug #2 | S |
| P2 | 设计 #2 | `EventKind` 增加 `Interaction`/`MemberJoin`/`MemberLeave` | S |
| P2 | 设计 #3 | `PlatformAdapter.Capabilities()` 接口 | M |
| P2 | 设计 #4 | `Sender` 可选 `MessageEditor`/`MessageDeleter` 接口 | S |
| P2 | 设计 #5 | `OutboundMessage` 支持多附件 + `Embed` 类型 | M |
| P2 | 设计 #6 | `Sender.Send()` 移除冗余 `chatID` 参数 | M（需更新所有实现） |
| P2 | 设计 #7 | `Button` 增加 `Disabled`/`Row` 字段 | XS |
| P2 | 性能 #1 | `qqEvent` 使用 `gjson.GetManyBytes()` 一次扫描 | S |
| P2 | 性能 #3 | 内联 `startWithPlatformHandler` | XS |
| P2 | 未实现 #1 | 创建多平台示例 | S |

### 后续迭代（P3 — 完整性提升）

| 编号 | 内容 | 工作量 |
|------|------|--------|
| 设计 #8 | 将 `NewBotWithDefault` 移至 `platform/qq` | XS |
| 删除 #3 | 清理骨架包的空 event 结构体 | XS |
| 未实现 #2 | QQ 群消息填充 `ChatInfo.Name` | XS |
| 未实现 #3 | 适配器健康状态可选接口 | S |
| 未实现 #4 | 事件 Ack 机制抽象 | M |
| 未实现 #5 | 文件二进制直传 `Attachment.Data` | S |
| 性能 #2 | `Extensions` 高频键快速路径 | M |
| 性能 #4 | `Reply` 缓存 ChatInfo context | S |
| 文档 #1 | 更新 `MULTI_PLATFORM.md` 目录结构 | XS |
| 文档 #2 | 修正 `GetEventType()` 文档描述 | XS |

