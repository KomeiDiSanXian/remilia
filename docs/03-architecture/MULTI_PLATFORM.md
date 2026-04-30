# 多平台消息处理架构

## 背景

原框架的事件处理与 QQ 官方数据结构（`dto.Payload`）深度耦合，导致：

- `core/engine.Adapter` 接口直接依赖 `func(*dto.Payload)`
- `core/context.Context` 内部持有 `*dto.Payload` 和 `openapi.OpenAPI`
- 无法在不修改引擎核心的情况下接入其他平台

## 设计目标

1. **平台无关**：框架核心不直接引用任何平台 SDK 类型
2. **渐进增强**：通过 `PlatformCapabilities` 运行时检测特性，优雅降级
3. **可扩展**：新平台只需实现标准接口，无需修改框架

## 层次架构

```
┌─────────────────────────────────────────┐
│           Bot / BotBuilder              │  用户 API 层
│  UsePlatformRegistry / WithPlatformRegistry │
├─────────────────────────────────────────┤
│         platform/ 抽象层                │  接口定义层（本包）
│  PlatformAdapter / Event / Sender       │
│  PlatformCapabilities / Registry        │
├──────────────┬──────────────────────────┤
│ platform/qq  │ platform/discord         │  平台实现层
│ platform/..  │ platform/telegram        │
│              │ platform/wechat          │
└──────────────┴──────────────────────────┘
```

## 核心接口

### `platform.Event`

平台无关的事件抽象，所有平台事件均实现此接口：

```go
type Event interface {
    // EventIdentity 平台标识与事件分类
    Platform() string     // 平台标识，如 "qq"、"discord"
    ID()       string     // 平台级唯一事件 ID（用于去重/追踪）
    Kind()     EventKind  // 平台无关的事件类别（私聊、群聊、通知等）

    // EventBody 事件正文
    Content() string   // 消息文本内容
    Attachments() []Attachment

    // 发送者和会话
    Sender() UserInfo    // 发送者信息
    Chat()   ChatInfo    // 会话信息

    Timestamp() time.Time // 事件时间戳
}

// 可选接口：访问平台原始数据
type RawEvent interface {
    RawType()    string  // 平台原始事件类型字符串
    RawPayload() any     // 原始 payload（类型断言访问平台特定字段）
}
```

### `platform.ChatInfo`

```go
type ChatInfo struct {
    ID       string  // 直接会话 ID（channel_id / group_id / user_id）
    ParentID string  // 父容器 ID（guild_id / 服务器 ID），私聊和普通群为空
    Name     string  // 会话名称（可选，部分平台不提供）
    IsGroup  bool    // 是否为群组/频道消息（false = 私聊）
}
```

### `platform.Adapter`

平台适配器的核心接口：

```go
type Adapter interface {
    Platform()     string
    StartPlatform(ctx context.Context, handler func(Event)) error
    Stop(ctx context.Context) error
    Sender()       Sender
    Capabilities() Capabilities  // 平台特性声明
}
```

### `platform.Capabilities`

平台特性声明，用于 Handler 做渐进增强决策：

```go
type Capabilities struct {
    Markdown        bool  // 是否支持 Markdown
    Buttons         bool  // 是否支持交互按钮
    MultiAttachment bool  // 是否支持多附件
    MessageEdit     bool  // 是否支持消息编辑（MessageEditor 接口）
    MessageDelete   bool  // 是否支持消息删除（MessageDeleter 接口）
    Embeds          bool  // 是否支持富文本嵌入卡片
    FileUpload      bool  // 是否支持二进制文件直传
    GuildSupport    bool  // 是否有服务器/频道层级（ChatInfo.ParentID 有效）
}
```

### `platform.Sender`

平台无关的消息发送接口。目标会话信息由框架自动从 Context 中的 `ChatInfo` 读取，
无需额外的 `chatID` 参数：

```go
type Sender interface {
    // 目标会话信息从 ctx 中的 ChatInfo 读取（由 Reply / WithChatInfo 注入）
    Send(ctx context.Context, req SendRequest) (SendResult, error)
}

// 可选接口：支持消息编辑的平台实现此接口
type MessageEditor interface {
    Edit(ctx context.Context, messageID string, msg OutboundMessage) error
}

// 可选接口：支持消息删除的平台实现此接口
type MessageDeleter interface {
    Delete(ctx context.Context, messageID string) error
}
```

### `platform.OutboundMessage`

平台无关的出站消息模型，支持多附件、富文本嵌入卡片和交互按钮：

```go
type OutboundMessage struct {
    Text        string         // 纯文本内容（最广泛兼容）
    Markdown    string         // Markdown 内容（不支持时降级为 Text）
    Attachments []Attachment   // 附件列表（图片/音频/视频/文件，支持多附件）
    Embeds      []Embed        // 富文本嵌入卡片（Discord 原生，其他平台降级）
    Mentions    []string       // @用户 ID 列表
    Buttons     []Button       // 交互按钮（Discord 组件/Telegram 内联键盘等）
    ReplyToID   string         // 回复的目标消息 ID
    Extra       map[string]any // 平台特定扩展字段
}
```

## EventKind 映射

| EventKind | QQ 平台事件类型 | 说明 |
|-----------|----------------|------|
| `EventKindPrivateMessage` | `C2C_MESSAGE_CREATE` | 私聊消息 |
| `EventKindGroupMessage` | `GROUP_AT_MESSAGE_CREATE` | 群 @机器人消息 |
| `EventKindGuildMessage` | `AT_MESSAGE_CREATE` / `MESSAGE_CREATE` | 频道消息 |
| `EventKindNotice` | `GROUP_MSG_REJECT` / `GROUP_MSG_RECEIVE` / `C2C_MSG_REJECT` / `C2C_MSG_RECEIVE` | 通知事件 |
| `EventKindSystem` | `READY` / `RESUMED` | 系统事件 |
| `EventKindMemberJoin` | `GROUP_ADD_ROBOT` / `FRIEND_ADD` | 成员加入/机器人被加入 |
| `EventKindMemberLeave` | `GROUP_DEL_ROBOT` / `FRIEND_DEL` | 成员离开/机器人被移除 |
| `EventKindInteraction` | — | 按钮回调/斜杠命令（待 QQ v2 适配） |
| `EventKindReaction` | — | 消息表情回应 |
| `EventKindMessageUpdate` | — | 消息被编辑 |
| `EventKindMessageDelete` | — | 消息被撤回/删除 |

## 使用方式

### 单平台（推荐入门用法）

```go
adapter := qq.NewWebhookServerAdapter(":8080", botInfo)
bot, err := remilia.NewBotBuilder().
    WithPlatformAdapter(adapter).
    Build()
```

### 多平台注册表

```go
registry := platform.NewRegistry()
registry.Register(qq.NewWebhookServerAdapter(":8080", botInfo))
// registry.Register(discord.NewAdapter(...)) // 未来接入其他平台

bot, err := remilia.NewBotBuilder().
    WithPlatformRegistry(registry).
    Build()
```

### Handler 中的跨平台路由

```go
// 匹配所有平台的私聊消息
engine.On(context.OnEventKind(platform.EventKindPrivateMessage),
    context.OnCommand("/ping"),
).Handle(func(ctx *context.Context) error {
    return ctx.Reply(platform.TextMessage("pong"))
})

// 渐进增强：根据平台能力选择消息格式
engine.OnAny().Handle(func(ctx *context.Context) error {
    event := ctx.GetPlatformEvent()
    if event == nil {
        return nil
    }
    // 通过 Registry 获取平台能力（或从 Context 中读取）
    // 根据能力选择合适的消息格式
    return ctx.Reply(platform.TextMessage("hello"))
})
```

### 使用 `ctx.Reply` 发送消息

```go
// 简单文本回复
ctx.Reply(platform.TextMessage("pong"))

// Markdown 回复（不支持的平台自动降级为纯文本）
ctx.Reply(platform.MarkdownMessage("# 标题\n正文"))

// 图片消息
ctx.Reply(platform.ImageMessage("https://example.com/img.png"))

// 富文本消息（多平台降级处理）
ctx.Reply(platform.TextMessage("").WithEmbeds(platform.Embed{
    Title:       "通知",
    Description: "这是一条测试消息",
}))

// 使用 ChatInfo 直接发送（不依赖 ctx）
sendCtx := platform.WithChatInfo(context.Background(), platform.ChatInfo{
    ID:      "group-001",
    IsGroup: true,
})
sender.Send(sendCtx, platform.TextMessage("公告"))
```

## 访问平台原始数据

```go
engine.On(context.OnEventKind(platform.EventKindGuildMessage)).Handle(func(ctx *context.Context) error {
    // 平台无关方式获取消息内容
    content := ctx.GetMessageContent()
    platform := ctx.GetEventPlatform()  // "qq" / "discord" / ...
    chat := ctx.GetPlatformEvent().Chat()

    // 若需要 QQ 平台特定字段，通过 RawPayload 类型断言
    if payload, ok := ctx.GetPlatformEvent().RawPayload().(*dto.Payload); ok {
        // 访问 QQ 原始数据
        _ = payload
    }
    return nil
})
```

## 实现新平台适配器

以实现 Telegram 适配器为例：

```go
// platform/telegram/adapter.go
package telegram

import (
    stdctx "context"
    tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
    "github.com/KomeiDiSanXian/remilia/platform"
)

type Adapter struct {
    bot    *tgbotapi.BotAPI
    sender *telegramSender
}

func (a *Adapter) Platform() string { return "telegram" }
func (a *Adapter) Sender() platform.Sender { return a.sender }
func (a *Adapter) Capabilities() platform.PlatformCapabilities {
    return platform.PlatformCapabilities{
        Markdown: true, Buttons: true, MultiAttachment: true,
        MessageEdit: true, MessageDelete: true, FileUpload: true,
    }
}

func (a *Adapter) StartPlatform(ctx stdctx.Context, handler func(platform.Event)) error {
    u := tgbotapi.NewUpdate(0)
    updates := a.bot.GetUpdatesChan(u)
    for {
        select {
        case <-ctx.Done():
            return nil
        case update := <-updates:
            handler(newTelegramEvent(update))  // 包装为 platform.Event
        }
    }
}
```

事件包装参考 `platform/qq/event.go` 的实现。

## 完整数据流

```
平台适配器事件循环
  │
  ▼
platform.PlatformAdapter.StartPlatform()
  │  handler(platform.Event)
  ▼
Bot.handlePlatformEvent(event)
  │  获取该平台的 Sender（从 Registry 或 adapter.Sender()）
  │  engine.ProcessPlatformEvent(event, sender)
  ▼
context.AcquireContextFromEvent(event, sender)
  │  创建 *context.Context（ctx.ctx = Background()）
  ▼
engine.processEventContext(ctx)
  │  GetEventType() → string(event.Kind())  // 如 "PRIVATE_MESSAGE"
  │  GetMessageContent() → event.Content()
  ▼
Matcher.Match(ctx) → Handler(ctx)
  │  ctx.Reply(platform.OutboundMessage)
  │    → platform.WithChatInfo(ctx.Context(), event.Chat())
  │    → platform.Sender.Send(ctx, msg)
  ▼
context.ReleaseContextFromEvent(ctx)  ← 归还对象池
```

## 目录结构

```
platform/
  event.go          # Event / UserInfo / ChatInfo / EventKind 接口定义
  message.go        # OutboundMessage / Attachment / Embed / Button 统一消息模型
  adapter.go        # PlatformAdapter / Sender / MessageEditor / MessageDeleter
                    # PlatformCapabilities / Registry / NoopSender
  platform_test.go  # 接口与工具函数测试
  qq/
    adapter.go      # QQ Adapter（轻量，包装 Webhook）
    bot.go          # QQ 平台 Bot 辅助函数
    event.go        # qqEvent（包装 *dto.Payload 为 platform.Event）
    sender.go       # QQ Sender + QQCapabilities
    webhook_server.go  # WebhookServerAdapter（内置 HTTP 服务器）
    event_test.go   # QQ 事件解析测试
    webhook_server_test.go
    dlq/            # 死信队列支持
    openapi/        # QQ OpenAPI 客户端
  discord/
    adapter.go      # Discord 骨架（待社区贡献）
  telegram/
    adapter.go      # Telegram 骨架（待社区贡献）
  wechat/
    adapter.go      # WeChat 骨架（待社区贡献）

core/context/
  platform_event.go      # AcquireContextFromEvent / Reply / GetEventKind 等
  platform_event_test.go # 平台无关路径单元测试

core/engine/
  process_platform.go      # ProcessPlatformEvent / processEventContext（共享核心）
  process_platform_test.go # 多平台路由集成测试
```

## 迁移完成状态

| 阶段 | 状态 | 说明 |
|------|------|------|
| `platform/` 抽象层 | ✅ 完成 | Event / PlatformAdapter / Sender / OutboundMessage / PlatformCapabilities / Registry |
| `platform/qq` 完整实现 | ✅ 完成 | WebhookServerAdapter / Adapter / qqSender / qqEvent 均实现 |
| `core/context` 迁移 | ✅ 完成 | 完全切换到平台无关路径，旧 dto.Payload 字段已清除 |
| `core/engine` 新入口 | ✅ 完成 | ProcessPlatformEvent 与旧 ProcessEvent 共用同一路由逻辑 |
| Bot 多平台注册表 | ✅ 完成 | UsePlatformRegistry / WithPlatformRegistry 完整实现 |
| `ctx.Reply()` | ✅ 完成 | 平台无关发送，自动注入 ChatInfo，正确传播超时/取消信号 |
| `platform.EventKind` 路由 | ✅ 完成 | OnEventKind() 支持跨平台规则匹配 |
| `platform/discord` | 🚧 骨架 | 接口签名存在，待社区贡献完整实现 |
| `platform/telegram` | 🚧 骨架 | 同上 |
| `platform/wechat` | 🚧 骨架 | 同上 |
