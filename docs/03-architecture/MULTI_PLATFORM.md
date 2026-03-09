# 多平台消息处理架构

## 背景

原框架的事件处理与 QQ 官方数据结构（`dto.Payload`）深度耦合，导致：

- `core/engine.Adapter` 接口直接依赖 `func(*dto.Payload)`
- `core/context.Context` 内部持有 `*dto.Payload` 和 `openapi.OpenAPI`
- 无法在不修改引擎核心的情况下接入其他平台

## 设计目标

1. **平台无关**：框架核心不直接引用任何平台 SDK 类型
2. **向后兼容**：现有 QQ 机器人代码零修改继续运行
3. **可扩展**：新平台只需实现标准接口，无需修改框架

## 层次架构

```
┌─────────────────────────────────────────┐
│           Bot / BotBuilder              │  用户 API 层
│  UsePlatformRegistry / WithPlatformRegistry │
├─────────────────────────────────────────┤
│         platform/ 抽象层                │  接口定义层（本包）
│  PlatformAdapter / Event / Sender       │
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
    Platform() string       // 平台标识，如 "qq"、"discord"
    Kind() EventKind        // 平台无关的事件类别（私聊、群聊、通知等）
    RawType() string        // 平台原始事件类型字符串
    Sender() UserInfo       // 发送者信息
    Chat() ChatInfo         // 会话信息
    Content() string        // 消息文本内容
    Timestamp() time.Time   // 事件时间戳
    RawPayload() any        // 原始 payload（类型断言访问平台特定字段）
}
```

### `platform.PlatformAdapter`

平台适配器的核心接口：

```go
type PlatformAdapter interface {
    Platform() string
    Start(ctx context.Context, handler func(Event)) error
    Stop(ctx context.Context) error
    Sender() Sender
}
```

### `platform.Sender`

平台无关的消息发送接口：

```go
type Sender interface {
    Send(ctx context.Context, chatID string, msg OutboundMessage) error
}
```

### `platform.OutboundMessage`

平台无关的出站消息模型，各平台 Sender 负责转换：

```go
type OutboundMessage struct {
    Text      string         // 文本内容
    Markdown  string         // Markdown 内容（不支持时降级为 Text）
    ImageURL  string         // 图片 URL
    ReplyToID string         // 回复的目标消息 ID
    Extra     map[string]any // 平台特定扩展字段
}
```

## EventKind 映射

| EventKind              | QQ 平台事件类型                          | 说明       |
|------------------------|------------------------------------------|------------|
| `EventKindPrivateMessage` | `C2C_MESSAGE_CREATE`                  | 私聊消息   |
| `EventKindGroupMessage`   | `GROUP_AT_MESSAGE_CREATE`             | 群消息     |
| `EventKindGuildMessage`   | `AT_MESSAGE_CREATE` / `MESSAGE_CREATE` | 频道消息   |
| `EventKindNotice`         | `FRIEND_ADD` / `GROUP_ADD_ROBOT` 等   | 通知事件   |
| `EventKindSystem`         | `READY` / `RESUMED`                   | 系统事件   |

## 使用方式

### 多平台注册表（推荐）

```go
// 创建 webhook 连接
webhookConn := remilia.NewWebhookServerAdapter(":8080", botInfo)

// 创建 QQ 平台适配器
api := openapi.New(tokenManager)
qqAdapter := qq.NewAdapter(webhookConn, api)

// 注册到多平台注册表
registry := platform.NewRegistry()
registry.Register(qqAdapter)
// registry.Register(discord.NewAdapter(...)) // 未来接入其他平台

// 构建 Bot
bot, err := remilia.NewBotBuilder().
    WithBotInfo(botInfo).
    WithPlatformRegistry(registry).
    Build()
```

### 旧方式（零修改，继续兼容）

```go
// 现有代码无需任何修改
bot, err := remilia.NewBotBuilder().
    WithBotInfo(botInfo).
    WithWebhook(":8080").
    Build()
```

## 访问平台原始数据

当需要访问 QQ 平台特定字段时，通过 `RawPayload()` 类型断言：

```go
engine.On(dto.C2CMessageCreate).Handle(func(ctx *context.Context) error {
    // 旧方式（仍然支持）：直接使用 ctx
    content := ctx.GetMessageContent()

    // 若需要平台原始数据（如从 platform.Event 中访问）：
    // event.RawPayload().(*dto.Payload) 获取原始 QQ payload
    return nil
})
```

## 实现新平台适配器

以实现 Telegram 适配器为例：

```go
// platform/telegram/adapter.go
type Adapter struct {
    bot    *tgbotapi.BotAPI // Telegram SDK
    sender *telegramSender
}

func (a *Adapter) Platform() string { return "telegram" }

func (a *Adapter) Start(ctx context.Context, handler func(platform.Event)) error {
    updates := a.bot.GetUpdatesChan(config)
    for {
        select {
        case <-ctx.Done():
            return nil
        case update := <-updates:
            handler(newTelegramEvent(update))
        }
    }
}
```

## 迁移路径

当前实现处于**过渡阶段**：

- `platform/qq` 的 `NewEvent` 将 `*dto.Payload` 包装为 `platform.Event`
- `Bot.handlePlatformEvent` 目前将 `platform.Event` 还原为 `*dto.Payload`，再走原有引擎路径
- 待 `core/engine` 直接支持 `platform.Event` 后，`handlePlatformEvent` 将直接调用新引擎，彻底消除耦合

## 目录结构

```
platform/
  event.go          # Event / UserInfo / ChatInfo / EventKind 接口定义
  message.go        # OutboundMessage 统一消息模型
  adapter.go        # PlatformAdapter / Registry / Sender / NoopSender
  context.go        # EventContext 接口 + baseEventContext 实现
  bridge.go         # LegacyAdapter 接口（向后兼容用）
  platform_test.go  # 接口与工具函数测试
  qq/
    adapter.go      # QQ PlatformAdapter 实现
    event.go        # qqEvent（包装 *dto.Payload 为 platform.Event）
    sender.go       # QQ Sender（包装 openapi.OpenAPI）
    event_test.go   # QQ 事件解析测试
  discord/
    adapter.go      # Discord 骨架（待实现）
  telegram/
    adapter.go      # Telegram 骨架（待实现）
  wechat/
    adapter.go      # 微信骨架（待实现）
```

