# 多平台适配器体系——平台无关的事件驱动抽象

## 设计动机

机器人框架天然需要支持多平台：QQ、Discord、Telegram、微信等。每个平台的 API 风格、通信协议、消息格式各不相同。框架的目标是让**业务逻辑完全与平台解耦**——同一个 Handler 可以在任意平台上运行。

## 核心抽象：Adapter 接口

```go
type Adapter interface {
    Platform() string                                          // 平台标识
    Start(ctx context.Context, handler func(Event)) error      // 启动事件循环
    Stop(ctx context.Context) error                            // 优雅停止
    Sender() Sender                                            // 消息发送器
    Capabilities() Capabilities                                // 平台能力声明
    IsRunning() bool                                           // 运行状态
}
```

每个平台（QQ、Discord、Telegram 等）实现此接口，框架核心通过此接口接收事件和发送消息——**不依赖任何平台 SDK**。

### 事件抽象

```go
type Event interface {
    Platform() string           // 来源平台
    Kind() EventKind            // 事件类别（平台无关）
    Raw() any                   // 原始平台事件
    // 平台无关字段
    GetMessage() Message        // 消息内容
    GetSender() UserInfo        // 发送者
    GetGroup() GroupInfo        // 群组信息（群消息时）
    GetChannel() ChannelInfo    // 频道信息
    // ...
}
```

平台无关的 `EventKind` 分类：

```go
type EventKind string

const (
    EventKindUnknown          EventKind = ""
    EventKindPrivateMessage   EventKind = "private_message"
    EventKindGroupMessage     EventKind = "group_message"
    EventKindChannelMessage   EventKind = "channel_message"
    EventKindGuildMessage     EventKind = "guild_message"
    EventKindFriendAdd        EventKind = "friend_add"
    EventKindMemberJoin       EventKind = "member_join"
    EventKindMemberLeave      EventKind = "member_leave"
    EventKindReactionAdd      EventKind = "reaction_add"
    // ...
)
```

### 消息抽象

```go
type Sender interface {
    SendMessage(ctx context.Context, target MessageTarget, msg Message) error
    EditMessage(ctx context.Context, target EditTarget, msg Message) error
    DeleteMessage(ctx context.Context, target DeleteTarget) error
}
```

### 能力声明

```go
type Capabilities struct {
    Markdown         bool   // 支持 Markdown 格式
    Buttons          bool   // 支持按钮交互
    MultiAttachment  bool   // 支持多附件
    MessageEdit      bool   // 支持编辑消息
    MessageDelete    bool   // 支持删除消息
    Embeds           bool   // 支持 Embed 卡片
    MaxTextLength    int    // 最大文本长度
    MaxAttachmentMB  int    // 最大附件大小
    // ...
}
```

Handler 可以通过 `ctx.GetPlatformCapabilities()` 获取平台能力，实现渐进增强策略——例如同一份代码在 QQ 上使用按钮交互，在 Telegram 上退化为文字回复。

### 能力合并

当适配器通过 `Bot` 层被多个来源注入能力时，使用取 OR 并集 + 取最小限制的策略：

```go
func mergePlatformCaps(caps []Capabilities) Capabilities {
    var m Capabilities
    for _, c := range caps {
        // 布尔能力取 OR（并集）
        m.Markdown = m.Markdown || c.Markdown
        m.Buttons = m.Buttons || c.Buttons
        // 量化限制取最小非零值（更保守）
        m.MaxTextLength = minNonZero(m.MaxTextLength, c.MaxTextLength)
        m.MaxAttachmentMB = minNonZero(m.MaxAttachmentMB, c.MaxAttachmentMB)
    }
    return m
}
```

## 已实现平台

### QQ（platform/qq）

最完善的平台实现，支持两种接入模式：

**Webhook 模式**（推荐）：
```go
botInfo := &dto.BotInfo{
    AppID:     123456,
    Token:     "your-token",
    AppSecret: "your-secret",
}
adapter := qq.NewWebhookServerAdapter(":8080", botInfo)
```

自动管理：
- Token 定期刷新（使用 `golang.org/x/time/rate` 限制刷新频率）
- Webhook 签名验证（HMAC-SHA256）
- 断连检测 + 自动重连

**协议结构**：
```
platform/qq/
├── adapter.go         # Adapter 接口实现
├── webhook_server.go  # Webhook HTTP Server
├── webhook_conn.go    # WebSocket 连接管理
├── sender.go          # 消息发送封装
├── event.go           # QQ 事件 → platform.Event 转换
├── result.go          # API 响应处理
└── openapi/
    ├── openapi.go     # OpenAPI HTTP 客户端
    ├── dto/           # 数据模型
    ├── iface.go       # API 接口
    └── auth/token/    # Token 管理
```

### Discord（platform/discord）

基于 `bwmarrin/discordgo` SDK 实现：

```go
type Adapter struct {
    session *discordgo.Session  // DiscordGo SDK
    handler func(platform.Event)
}

func (a *Adapter) Start(ctx context.Context, handler func(Event)) error {
    a.handler = handler
    a.session.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
        event := a.convertMessage(m)
        handler(event)
    })
    return a.session.Open()
}
```

支持 Slash Commands 和交互式组件。

### Telegram（platform/telegram）

基于 long-polling 模式，使用 Telegram Bot API。

### OneBot（platform/onebot）

支持 WebSocket 反向连接和 HTTP POST 两种模式，兼容 OneBot v11 协议：

```go
// WebSocket 反向连接（OneBot 主动连接 Bot）
adapter := onebot.NewReverseWSAdapter("ws://127.0.0.1:6700")
```

### Satori（platform/satori）

支持 Satori 协议标准，提供 WebSocket 和 Webhook 两种接入方式：

```go
adapter := satori.NewWebSocketAdapter("ws://127.0.0.1:5500")
adapter := satori.NewWebhookAdapter(":8550")
```

### 微信（platform/wechat）

微信官方 API 适配，支持公众号和企业微信消息。

### Milky（platform/milky）

自定义协议适配器，使用 WebSocket 长连接：

```go
type Client struct {
    conn     *websocket.Conn
    api      *API
    handler  func(platform.Event)
    botID    string
}
```

## Registry：多平台统一管理

```go
type Registry struct {
    adapters map[string]Adapter  // platform → Adapter
    mu       sync.RWMutex
}

func (r *Registry) Register(adapter Adapter) {
    r.adapters[adapter.Platform()] = adapter
}

func (r *Registry) All() []Adapter {
    // 返回所有注册的适配器
}
```

Bot 通过 Registry 支持同时接入多个平台：

```go
registry := platform.NewRegistry()
registry.Register(qqAdapter)
registry.Register(discordAdapter)
registry.Register(telegramAdapter)

bot, err := remilia.NewBotBuilder().
    WithPlatformRegistry(registry).
    WithEngine(eng).
    Build()
```

### 适配器快照（热路径优化）

```go
// 在 Bot.Start() 时构建快照
snapshot := make(map[string]adapterCache)
for _, pa := range reg.All() {
    snapshot[pa.Platform()] = adapterCache{
        adapter: pa,
        caps:    pa.Capabilities(),
    }
}
b.adapterSnapshot.Store(snapshot)

// 热路径：atomic.Value.Load，零锁开销
snapshot := b.adapterSnapshot.Load()
if c, ok := snapshot[event.Platform()]; ok {
    sender = c.adapter.Sender()
    caps = c.caps
}
```

## 扩展新平台

添加一个新平台只需：
1. 实现 `platform.Adapter` 接口（4 个方法 + Start/Stop）
2. 将平台事件转换为 `platform.Event`
3. 实现 `Sender` 发送消息

无需修改框架核心代码——所有平台差异化逻辑封装在适配器内部。

## 迭代过程

### V0：Bot 与 QQ API 深度绑定（Monolithic）

初始版本根本没有"平台"概念。Bot 直接依赖 QQ 的 `openapi/dto` 和 `openapi/auth/token`：

```go
// V0 代码 — bot.go（初始 commit）
type Bot struct {
    wh     webhook.WebHook       // QQ Webhook 协议
    tm     *token.Manager        // QQ Token 管理
    engine *Engine               // 事件引擎
    api    openapi.OpenAPI       // QQ OpenAPI HTTP 客户端
    srv    *http.Server
}

// 事件处理直接操作 dto.Payload
func (b *Bot) handleEvent(payload *dto.Payload) {
    ctx := NewContext(payload, b.api)
    b.engine.ProcessEvent(ctx)
}
```

```go
// V0 Context — 强依赖 dto.Payload
type Context struct {
    event *dto.Payload       // 只能处理 QQ 事件
    api   openapi.OpenAPI    // 只能通过 QQ API 回复
    state State
}
```

```go
// V0 Engine — 死信队列也依赖 dto.Payload
type DeadLetterItem struct {
    Event   *dto.Payload  // QQ 专用
    Err     error
    Attempt int
}
```

**问题**：
- 所有代码都耦合 `dto.Payload`（QQ 的消息结构体）
- 添加 Discord 支持？需要修改 Bot、Context、Engine、DeadLetter...几乎所有类型
- 测试需要 mock `openapi.OpenAPI`，依赖整个 QQ SDK
- 插件的 `plugin.Load(engine)` 持有整个 Engine，可以对任意平台 API 做操作

### V1：Context 双路径（过渡方案）

在平台抽象层正式引入前，采取了过渡策略——Context 支持双路径：

```go
// V1 Context — 双路径（旧路径 + 新路径）
type Context struct {
    // 旧路径（QQ 专用）
    event *dto.Payload
    api   openapi.OpenAPI

    // 新路径（平台无关）
    platformEvent  platform.Event
    platformSender platform.Sender
    isPlatformPath bool  // 路由选择器
}
```

```go
// 两种创建方式
func NewContext(event *dto.Payload, api openapi.OpenAPI) *Context {
    return &Context{event: event, api: api, isPlatformPath: false}
}

func AcquireContextFromEvent(event platform.Event, sender platform.Sender) *Context {
    ctx := contextPool.Get().(*Context)
    ctx.platformEvent = event
    ctx.platformSender = sender
    ctx.isPlatformPath = true
    return ctx
}
```

```go
// Handler 中的兼容处理
func (ctx *Context) Reply(msg interface{}) error {
    if ctx.isPlatformPath {
        return ctx.platformSender.SendMessage(...)  // 新路径
    }
    return ctx.api.SendMessage(...)                   // 旧路径
}
```

**好处**：已有的 QQ Handler 代码不需要修改，新平台代码走新路径。
**问题**：双路径的 runtime 分支增加了心智负担；`if isPlatformPath` 散落在各处；错误路径的选择器 bug 难以调试。

### V2：完整平台抽象（当前）

关键 commit `0709f98` 完成迁移，一次性引入 `platform.Event`、`platform.Adapter`、`platform.Registry` 三大抽象：

```go
// V2 — 核心抽象
type Event interface {
    Platform() string       // "qq" / "discord" / "telegram"
    Kind() EventKind        // "private_message" / "group_message"
    Raw() any               // 原始事件（调试用）
    GetMessage() Message
    GetSender() UserInfo
    GetGroup() GroupInfo
    // ...
}

type Adapter interface {
    Platform() string
    Start(ctx context.Context, handler func(Event)) error
    Stop(ctx context.Context) error
    Sender() Sender
    Capabilities() Capabilities
    IsRunning() bool
}

type Registry struct {
    adapters map[string]Adapter
    mu       sync.RWMutex
}
```

**Engine 的入口也随之改变**：

```go
// V2 — 新增平台无关入口
func (e *Engine) ProcessPlatformEvent(event platform.Event, sender platform.Sender, caps ...platform.Capabilities) {
    ctx := context.AcquireContextFromEvent(event, sender)
    defer context.ReleaseContextFromEvent(ctx)
    if len(caps) > 0 {
        ctx.SetPlatformCapabilities(mergePlatformCaps(caps))
    }
    e.processEventContext(ctx)  // 共享核心逻辑
}

// 旧入口保留兼容，但内部也转换为新路径
func (e *Engine) ProcessEvent(ctx *context.Context) {
    // 现在只是 processEventContext 的包装
    e.processEventContext(ctx)
}
```

**双路径清理**：`isPlatformPath` 字段在 V2 中被移除——Context 不再持有 `dto.Payload` 和 `openapi.OpenAPI`，只保留 `platform.Event` 和 `platform.Sender`。旧代码库中根包对 `openapi/dto` 的依赖被彻底移除（见 commit `93fd3a2`）。

**测试迁移**——测试不再需要 QQ 类型的 mock：

```go
// V2 测试 — 平台无关
// 之前（V0）：
// mockAdapter.events <- &dto.Payload{Type: dto.C2CMessageCreate, ...}

// 之后（V2）：
mockAdapter.events <- testutil.MakePlatformC2CEvent("hello")
```

### V3 扩展：多平台适配器实现

在抽象稳定后，逐步添加平台适配器：

```bash
platform/
├── qq/         # V0 就有（从根包 openapi/ 迁移）
├── discord/    # 新增（基于 discordgo SDK）
├── telegram/   # 新增（Bot API long-polling）
├── onebot/     # 新增（OneBot v11 协议）
├── satori/     # 新增（Satori 协议）
├── wechat/     # 新增
└── milky/      # 新增（自定义 WS 协议）
```

### V4 优化：适配器快照（零锁热路径）

多平台引入了一个性能隐患：每次事件处理都需要从 Registry 查找适配器。最初实现使用 `sync.RWMutex`：

```go
// V3 方式 — 每事件加锁
func (b *Bot) handlePlatformEvent(event platform.Event) {
    b.mu.RLock()
    pa := b.platformRegistry.Get(event.Platform())
    b.mu.RUnlock()
    // ...
}
```

虽然读锁允许多并发，但在 475K msg/s 下锁的开销仍然显著。V4 改为启动时构建**只读快照**：

```go
// V4（当前）— 启动时构建快照，热路径零锁
// Start() 时构建
snapshot := make(map[string]adapterCache)
for _, pa := range reg.All() {
    snapshot[pa.Platform()] = adapterCache{...}
}
b.adapterSnapshot.Store(snapshot)

// 热路径 — 仅 atomic.Load
snapshot := b.adapterSnapshot.Load()
if c, ok := snapshot[event.Platform()]; ok {
    sender = c.adapter.Sender()
    caps = c.caps
}
```

**适配器断连感知**：新增可选接口 `RecoverableAdapter`，适配器可以注册断连回调：

```go
type RecoverableAdapter interface {
    OnDisconnect(fn func(err error)) (unregister func())
}
```

配合 `AdapterObserver` 和 Prometheus 指标，实现断连告警和自动恢复。

## 迭代历程

| 版本 | 核心变化 | 解决的问题 |
|------|---------|-----------|
| V0 | Bot 紧耦合 QQ openapi/dto | 快速实现，只支持 QQ |
| V1 | Context 双路径（旧 dto + 新 platform） | 过渡方案，兼容旧代码 |
| V2（核心）| platform.Event / Adapter / Registry 三大抽象 | 彻底解耦，多平台支持 |
| V3 扩展 | QQ/Discord/Telegram/OneBot/Satori 等适配器 | 丰富的平台生态 |
| V4（当前）| 适配器快照 + 断连感知 + BotIdentity | 热路径零锁，运维友好 |

## 设计权衡

| 方面 | 选择 | 理由 |
|------|------|------|
| 协议 | Adapter 接口抽象 | 平台无关，不依赖特定 SDK |
| 事件分类 | EventKind 枚举 + Raw 原始事件 | 业务用枚举，调试用原始事件 |
| Sender | 每个平台独立实现 | 适配各平台 API 差异 |
| 多平台 | Registry + 快照 | 零锁热路径 + 统一管理 |
| Capabilities | 声明式能力模型 | 渐进增强策略 |
