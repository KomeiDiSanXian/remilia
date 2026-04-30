# platform 接口设计问题分析

> 文档状态：Draft  
> 创建日期：2026-03-23  
> 涉及路径：`platform/`、`core/context/`

---

## 概览

本文档对 `platform` 抽象层进行系统性设计审查，列举接口契约中存在的语义歧义、
信息丢失、抽象泄漏及防御性缺失等问题，并给出修改方向。

问题按影响范围划分为四个优先级：

| 优先级 | 标签   | 含义                                           |
| ------ | ------ | ---------------------------------------------- |
| P0     | 严重   | 导致核心功能无法实现，必须修复                 |
| P1     | 高     | 导致明显的错误行为或运行时异常，应尽快修复     |
| P2     | 中     | 接口设计缺陷，不影响当前功能但会阻碍未来扩展   |
| P3     | 低     | 一致性/可读性问题，可在合适时机改善            |

---

## P0 — 严重

### 问题 1：`Sender.Send` 无响应体，后续操作（撤回、编辑）无法实现

**位置**：`platform/adapter.go` → `Sender` 接口；`core/context/platform_event.go` → `ctx.Reply`

**现状**

```go
type Sender interface {
    Send(ctx context.Context, req SendRequest) error  // 只有 error
}

func (ctx *Context) Reply(msg platform.OutboundMessage) error { ... }
```

**问题描述**

发送成功后，平台会返回包含 `message_id`、`timestamp` 等字段的响应体（QQ API 返回
`{"id": "...", "timestamp": ...}`）。当前 `Send` 将这些信息全部丢弃，导致以下场景
**在平台无关层完全无法实现**：

- **定时撤回**：发送消息 → 10 秒后撤回，需要 `message_id`
- **消息编辑**：发送消息 → 修改内容，需要 `message_id`（`MessageEditor.Edit` 已定义
  但无法获取 ID）
- **消息追踪**：关联发送日志、告警链路
- **幂等重试**：判断消息是否已发出，避免重复

`MessageEditor` 和 `MessageDeleter` 两个可选接口的参数签名依赖 `messageID`，但唯一能
获取这个 ID 的入口（`Send`）却不返回它——两者在设计上相互依赖，却形成了断层。

**修改方向**

引入 `SendResult` 结构体，`Send` 改为返回 `(SendResult, error)`：

```go
// SendResult 消息发送成功后的响应摘要。
// 平台未提供的字段返回零值（ID="" 表示无法获取，不影响业务逻辑）。
type SendResult struct {
    // MessageID 平台返回的已发送消息唯一 ID，用于后续撤回/编辑操作
    MessageID string
    // Timestamp 平台确认的消息发送时间（零值表示平台未返回）
    Timestamp time.Time
    // Platform 来源平台标识符
    Platform string
}

type Sender interface {
    Send(ctx context.Context, req SendRequest) (SendResult, error)
}
```

`ctx.Reply` 和 `ctx.ReplyWithContext` 同步调整为返回 `(platform.SendResult, error)`。

`MessageDeleter.Delete` 和 `MessageEditor.Edit` 可新增接受 `SendResult` 的重载或直接
利用其中的 `MessageID`，无需调用方手动管理字符串。

---

## P1 — 高

### 问题 2：`OutboundMessage.IsEmpty()` 语义不准确，导致 `Validate()` 误拒合法消息

**位置**：`platform/message.go` → `IsEmpty()`；`platform/adapter.go` → `SendRequest.Validate()`

**现状**

```go
func (m OutboundMessage) IsEmpty() bool {
    return m.Text == "" &&
        m.Markdown == "" &&
        len(m.Attachments) == 0 &&
        len(m.Embeds) == 0
    // Buttons、Mentions 不计入"内容"判断
}
```

**问题描述**

在 Discord（以及部分 QQ 交互场景）中，只含交互按钮的消息是**完全合法**的发送操作
（"按钮面板"），不需要任何文本或附件。当前实现会将此类消息判定为 `IsEmpty()=true`，
导致 `Validate()` 返回 `ErrEmptyMessage`，在 `Send` 入口就被拒绝。

类似地，只含 `Mentions`（@某人 但无正文）的消息在某些频道场景也是合法的。

**修改方向**

将 `Buttons` 和 `Mentions` 纳入"有意义内容"的判断范围：

```go
func (m OutboundMessage) IsEmpty() bool {
    return m.Text == "" &&
        m.Markdown == "" &&
        len(m.Attachments) == 0 &&
        len(m.Embeds) == 0 &&
        len(m.Buttons) == 0 &&     // 纯按钮消息是合法的
        len(m.Mentions) == 0       // 纯 @ 消息在部分平台合法
}
```

---

### 问题 3：`Attachment.URL` 与 `Attachment.Data` 互斥约束未在接口层强制执行

**位置**：`platform/message.go` → `Attachment`；`platform/adapter.go` → `SendRequest.Validate()`

**现状**

```go
// Attachment — 文档注释说两者互斥，但没有任何校验
type Attachment struct {
    URL  string
    Data []byte
    // ...
}
```

`SendRequest.Validate()` 只检查 `Target.ID` 和 `IsEmpty()`，不校验附件的合法性。
QQ sender 优先使用 `Data`（`len(att.Data) > 0` 分支），Discord sender 可能有不同策略。
调用方在两者都设置时，**无法知道哪个会被实际使用**，行为依赖各平台 sender 的私有约定。

**修改方向**

在 `SendRequest.Validate()` 中增加附件校验，或在 `Attachment` 上添加构造函数强制互斥：

```go
// 方案一：Validate 中增加校验
func (r SendRequest) Validate() error {
    // ...existing checks...
    for _, att := range r.Message.Attachments {
        if att.URL != "" && len(att.Data) > 0 {
            return fmt.Errorf("%w: attachment cannot have both URL and Data set", ErrInvalidMessage)
        }
        if att.URL == "" && len(att.Data) == 0 {
            return fmt.Errorf("%w: attachment must have either URL or Data", ErrInvalidMessage)
        }
    }
    return nil
}

// 方案二（更彻底）：Attachment 提供工厂函数，构造时即强制互斥
func AttachmentFromURL(kind AttachmentKind, url string) Attachment  { ... }
func AttachmentFromData(kind AttachmentKind, data []byte) Attachment { ... }
```

---

### 问题 4：`ReactionSender` 的 `emoji` 参数设计无法满足 QQ 平台的实际需求

**位置**：`platform/adapter.go` → `ReactionSender`

**现状**

```go
type ReactionSender interface {
    AddReaction(ctx context.Context, chatID, messageID, emoji string) error
    RemoveReaction(ctx context.Context, chatID, messageID, emoji string) error
}
```

**问题描述**

不同平台的 emoji 表示方式差异显著：

| 平台    | 实际参数          | 描述                                    |
| ------- | ----------------- | --------------------------------------- |
| QQ      | `(type int, id string)` | type=1 系统表情，type=2 emoji；两者缺一不可 |
| Discord | `"name:id"` or `"name"` | 自定义 emoji 用 `name:id`，标准 emoji 直接字符 |
| Telegram | emoji 字符        | 直接 Unicode 字符串                     |

QQ 的 `AddReaction` 需要两个参数（类型+ID），但接口只有一个 `string`。QQ 适配器若实现
此接口，**必须在单个字符串内编码两个字段**（如 `"1:405"` 表示 type=1, id=405），形成
不透明的平台特定编码约定，与接口"平台无关抽象"的目标背道而驰。

目前代码中没有任何平台实现了 `ReactionSender`，这也间接证明该接口难以落地。

**修改方向**

引入平台无关的 `Emoji` 值对象，平台 sender 各自负责映射：

```go
// Emoji 平台无关的表情标识。
// 各平台 sender 根据 Kind 和 ID 映射到平台特定格式。
type Emoji struct {
    // Kind 表情种类（"unicode"、"custom"、"system"）
    Kind string
    // ID 平台内部 emoji ID（标准 Unicode emoji 此字段为空，直接用 Value）
    ID string
    // Value emoji 字面量或名称（Unicode 表情直接填字符，如 "👍"）
    Value string
}

type ReactionSender interface {
    AddReaction(ctx context.Context, chatID, messageID string, emoji Emoji) error
    RemoveReaction(ctx context.Context, chatID, messageID string, emoji Emoji) error
}
```

QQ 平台可将 `Emoji{Kind: "system", ID: "405"}` 映射到 `(emojiType=1, emojiID="405")`。

---

## P2 — 中

### 问题 5：`Capabilities` 只有布尔标志，缺少量化限制指标

**位置**：`platform/adapter.go` → `Capabilities`

**现状**

```go
type Capabilities struct {
    Markdown        bool
    Buttons         bool
    MultiAttachment bool
    // ... 全部为 bool
}
```

**问题描述**

Handler 无法通过 `Capabilities` 获取平台的量化约束，需要硬编码平台特定常量：

| 需要知道的量化约束  | 示例平台差异                                              |
| ------------------- | --------------------------------------------------------- |
| 单条消息最大字符数  | QQ 文字子频道约 4000 字，Discord 2000 字，Telegram 4096 字 |
| 最大附件大小        | Discord 8MB（免费）/50MB（Nitro），Telegram 50MB          |
| 每行最多按钮数      | QQ 最多 5 个，Discord 最多 5 个，Telegram 无硬性限制      |
| 最多行数            | QQ/Discord 最多 5 行                                      |
| Embed 字段数上限    | Discord 最多 25 个字段                                    |

缺少这些信息，Handler 要么硬编码平台值（破坏平台无关性），要么发送超限消息后依赖运行时
错误（`SendErrMsgTooLong`）来发现问题，无法提前裁剪。

**修改方向**

在 `Capabilities` 中增加限制字段，用 `0` 表示"无限制或平台未公开"：

```go
type Capabilities struct {
    // ...existing bool fields...

    // 量化限制（0 = 无已知限制）
    MaxTextLength    int // 单条文本最大字符数
    MaxAttachmentMB  int // 单个附件最大大小（MB）
    MaxButtonsPerRow int // 每行最多按钮数
    MaxButtonRows    int // 最多按钮行数
    MaxEmbedFields   int // 单个 Embed 最多字段数
}
```

---

### 问题 6：`ChatInfo.Tokens` 使用弱类型 `map[string]string`，键名拼写错误在运行时无法发现

**位置**：`platform/event.go` → `ChatInfo.Tokens`；`platform/qq/extra.go`

**现状**

```go
type ChatInfo struct {
    // ...
    Tokens map[string]string  // 用字符串键存储平台专属授权 token
}

// QQ 平台键名（qq/extra.go）
const (
    TokenMsgID   = "msg_id"
    TokenEventID = "event_id"
)
```

**问题描述**

1. **拼写错误无编译期检查**：若某处写成 `chat.Tokens["msg_Id"]`，编译通过但运行时静默
   返回空字符串，被动消息变为主动消息发出，可能触发频控或权限错误。

2. **跨包透明度为零**：`Tokens` 的可能键名散落在各平台的 `extra.go` 中，没有集中的
   文档或类型定义，新开发者难以发现和理解。

3. **类型不匹配无法表达**：部分平台的 token 可能是整数或复合结构，强制转为 `string` 
   需要额外的序列化/反序列化，引入隐式约定。

**修改方向**

引入强类型的 `PassiveToken`，或将 Tokens 改为每个平台实现 `TokenProvider` 接口：

```go
// 方案：在 ChatInfo 中使用 struct 而非裸 map
// （适合长期方案，需要各平台约定统一键结构）

// 短期：至少在 Validate 中检查已知键名的合法性
// 并在 ChatInfo 上提供类型安全的访问方法
func (c ChatInfo) Token(key string) string {
    if c.Tokens == nil {
        return ""
    }
    return c.Tokens[key]
}
```

更彻底的方案是将 `Tokens` 替换为平台专属的接口扩展（类似 `Extra any`），由平台 sender
自行解包，框架层不感知其内部结构。

---

### 问题 7：`Registry.StartAll` 丢弃 `OnDisconnect` 的注销函数，存在回调累积泄漏

**位置**：`platform/adapter.go` → `Registry.StartAll`

**现状**

```go
func (r *Registry) StartAll(ctx context.Context, handler func(Event)) error {
    for _, a := range adapters {
        if ra, ok := a.(RecoverableAdapter); ok {
            _ = ra.OnDisconnect(func(err error) {  // ← 返回值 unregister 被丢弃
                // ...
            })
        }
        // ...
    }
}
```

**问题描述**

`RecoverableAdapter.OnDisconnect` 返回一个 `unregister func()` 用于注销回调。
`StartAll` 用 `_ =` 将其直接丢弃。

这在以下场景导致问题：

1. **多次调用 `StartAll`**（如重启或热重载）：每次都追加一个新回调，旧回调无法被清除，
   累积执行 N 次（N = 调用次数）。
2. **`StopAll` 后回调依然存活**：即使适配器已停止，挂在适配器上的 Registry 回调仍持有
   对 Registry 的引用，阻碍 GC。

**修改方向**

存储并在适当时机调用 unregister：

```go
// 存储每个适配器的 unregister 函数，在 StopAll 或 StartAll 重入时调用
type Registry struct {
    // ...existing fields...
    disconnectUnregs map[string]func() // platform → unregister
}

// StartAll 中
if ra, ok := a.(RecoverableAdapter); ok {
    // 先注销旧的（如果有）
    if old, exists := r.disconnectUnregs[a.Platform()]; exists {
        old()
    }
    unregister := ra.OnDisconnect(func(err error) { ... })
    r.disconnectUnregs[a.Platform()] = unregister
}
```

---

## P3 — 低

### 问题 8：`Capabilities` 是构建时静态快照，无法表达运行时动态能力

**位置**：`platform/adapter.go` → `Adapter.Capabilities()`

**现状**

QQ sender 的 `Capabilities` 是包级 `var`（编译时常量），在整个生命周期内不变。

**问题描述**

部分平台能力需要在运行时确认（例如：QQ 频道机器人是否拥有主动推送权限取决于后台配置，
`MessageDelete` 是否可用取决于机器人身份组权限）。当前设计无法区分"平台原则上支持"
和"当前 bot 实例实际可用"两种状态。

**修改方向**

允许 `Capabilities()` 在适配器连接并获取 bot 信息后动态更新，或提供 `RuntimeCapabilities(ctx)` 方法进行实时查询。短期内，至少将 QQ sender 中的 `Capabilities` 从包级变量改为方法，保留未来更新的可能：

```go
// 现在
var Capabilities = platform.Capabilities{ ... }

// 改为
func (a *WebhookServerAdapter) Capabilities() platform.Capabilities {
    return platform.Capabilities{ ... }
}
```

---

### 问题 9：`BotIdentity` 仅在 `Adapter` 层存在，无法便捷地从事件层判断"是否机器人自身触发"

**位置**：`platform/adapter.go` → `BotIdentity`；`platform/event.go` → `UserInfo`

**现状**

防止自回复需要两步：

```go
// 需要同时持有 adapter 和 event 两个对象
botID := platform.GetBotID(adapter)
if event.Sender().ID == botID { return }
```

**问题描述**

`Event` 和 `Adapter` 在 `context.Context` 中分离存储，handler 若只接收 `*context.Context`
（不持有 `Adapter` 引用），无法直接完成"自回复过滤"，除非引擎层注入了额外辅助信息。

**修改方向**

在 `Context` 层提供封装好的便捷方法，由引擎负责注入 BotID：

```go
// core/context/context.go 中
func (ctx *Context) IsFromSelf() bool {
    botID := ctx.botID // 引擎在 ProcessPlatformEvent 时注入
    if botID == "" { return false }
    return ctx.GetPlatformEvent().Sender().ID == botID
}
```

---

## 变更影响汇总

| 问题编号 | 改动范围               | 是否破坏性变更              | 优先级 |
| -------- | ---------------------- | --------------------------- | ------ |
| 1        | `Sender`、`ctx.Reply`  | **是**（接口签名变更）      | P0     |
| 2        | `OutboundMessage`      | 否（行为扩展，更宽松）      | P1     |
| 3        | `SendRequest.Validate` | 否（新增校验，更严格）      | P1     |
| 4        | `ReactionSender`       | **是**（接口签名变更）      | P1     |
| 5        | `Capabilities`         | 否（新增字段）              | P2     |
| 6        | `ChatInfo.Tokens`      | 视方案而定                  | P2     |
| 7        | `Registry`             | 否（内部实现变更）          | P2     |
| 8        | QQ `Capabilities`      | 否（内部实现变更）          | P3     |
| 9        | `core/context`         | 否（新增方法）              | P3     |

破坏性变更（问题 1、4）建议在同一个 major 版本迭代中集中处理，减少 API 碎片化。
问题 1 的 `Sender` 接口变更影响所有平台适配器实现和 `testbot.MockAPI`，改动范围最大，
需要配合充分的迁移文档。

