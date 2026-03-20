# 多平台抽象层全面审查报告

> 日期：2026-03-20  
> 分支：多平台抽象实现分支  
> 范围：`platform/`、`platform/qq/`、`core/engine/`、`core/context/`、`bot.go`、`bot_builder.go`

---

## 一、当前进度总览

### ✅ 已完成

| 组件 | 状态 | 说明 |
|------|------|------|
| `platform.Event` 接口 | ✅ 完整 | Kind/RawType/Sender/Chat/Content/Timestamp/ID/RawPayload |
| `platform.Adapter` 接口 | ✅ 完整 | Platform/Start/Stop/Sender/Capabilities |
| `platform.Sender` 接口 | ✅ 完整 | ChatInfo 通过 ctx 注入 |
| `platform.Registry` | ✅ 完整 | 多适配器管理，并发安全 |
| `platform.OutboundMessage` | ✅ 完整 | 链式 Builder，字段覆盖所有场景 |
| `platform.Capabilities` | ✅ 完整 | 渐进增强特性检测 |
| `platform.EventKind` | ✅ 完整 | 14 种通用事件类型 |
| `qq.Adapter` | ✅ 完整 | 有界 worker pool，Start 阻塞语义 |
| `qq.WebhookServerAdapter` | ✅ 完整 | 内置 HTTP server，Token 自动管理 |
| `qq.qqEvent` | ✅ 完整 | gjson 一次扫描，C2C/Group/Guild/Notice |
| `qq.qqSender` | ⚠️ 部分 | 仅支持 Text/Markdown，缺附件/按钮/@/Guild |
| `engine.ProcessPlatformEvent` | ✅ 完整 | 按 EventKind 路由，复用核心逻辑 |
| `context.AcquireContextFromEvent` | ✅ 完整 | 对象池，平台无关初始化 |
| `ctx.Reply()` / `ctx.ReplyWithContext()` | ✅ 完整 | ChatInfo 自动注入 |
| `ctx.GetPlatformEvent()` / `GetEventKind()` 等 | ✅ 完整 | 全路径可访问 |
| `BotBuilder.WithPlatformAdapter/Registry` | ✅ 完整 | 单/多平台两种模式 |
| `Bot.OnEventKind()` | ✅ 完整 | 平台无关 Matcher 注册 |
| telegram/discord/wechat 适配器 | 🚧 骨架 | placeholder，仅接口声明 |

---

## 二、Bug 与错误（需立即修复）

### 🔴 P0 — 关键 Bug

#### B1：`ctx.GetUserID()` 从字符串状态读取，平台路径永远返回空字符串

**文件**：`core/context/state.go`、`core/context/rules.go`、`core/context/convenience.go`、所有 builtin 插件

```go
// 当前实现（错误）
func (ctx *Context) GetUserID() string {
    return ctx.GetString("user_id")  // 只读字符串状态 store，平台路径从不写入
}
```

**影响范围**：所有使用 `ctx.GetUserID()` 的地方均返回 ""，包括：
- `OnHasPermission` / `OnHasRole` → 永远返回 false，权限检查完全失效
- `HasPermission(checker, ...)` / `NotBanned(checker, ...)` → 用户 ID 为空，检查失效
- `builtin/core/permission`、`builtin/core/admin`、`builtin/ratelimitui`、`builtin/dev/debug` → 所有用户相关功能失效

**修复方案**：将 `GetUserID()` 改为优先读取 `platformEvent.Sender().ID`：

```go
func (ctx *Context) GetUserID() string {
    if ctx == nil {
        return ""
    }
    // 优先从平台事件获取（平台路径）
    if ctx.platformEvent != nil {
        if id := ctx.platformEvent.Sender().ID; id != "" {
            return id
        }
    }
    // fallback：兼容手动 ctx.SetUserID() 的场景
    return ctx.GetString("user_id")
}
```

---

#### B2：`qq/sender.go` 的 `buildDTOMessage` 未处理 `Mentions` 字段

**文件**：`platform/qq/sender.go`

`platform.OutboundMessage.Mentions` 是 @ 用户 ID 列表，QQ 平台需要转换为 `<qqbot-at-user id="xxx" />` 标签嵌入 Content，但 `buildDTOMessage` 完全忽略此字段。

```go
// 修复：在 buildDTOMessage 中处理 Mentions
if len(msg.Mentions) > 0 {
    var sb strings.Builder
    for _, uid := range msg.Mentions {
        sb.WriteString(dto.At(uid))
    }
    sb.WriteString(dtoMsg.Content)
    dtoMsg.Content = sb.String()
}
```

---

#### B3：`qq/sender.go` 不支持 `GuildMessage` 发送路径

**文件**：`platform/qq/sender.go`

`Send()` 只按 `ChatInfo.IsGroup` 分 `GroupChat`/`SingleChat` 两路，不处理频道消息（需要 guild_id + channel_id 以及完全不同的 API 端点）。当 `EventKindGuildMessage` 的 handler 调用 `ctx.Reply()` 时，会错误地走 `GroupChat` 路径，导致发送失败或发到错误目标。

**修复方案**：在 `ChatInfo` 中增加 `IsGuild bool` 字段或通过 `ParentID != ""` 判断，同时在 `OpenAPI` 接口中添加 `GuildChannelMessage` 方法。

---

### 🟠 P1 — 重要 Bug

#### B4：`discord/adapter.go` 与 `telegram/adapter.go` 重复定义私有 `noopSender`

**文件**：`platform/discord/adapter.go`、`platform/telegram/adapter.go`

两个文件各自定义了私有的 `noopSender` 结构体，而公共的 `platform.NoopSender` 已存在。`wechat/adapter.go` 已正确使用 `platform.NoopSender{}`，应统一。

```go
// 错误（discord 和 telegram 中）
type noopSender struct{}
func (s *noopSender) Send(...) error { return fmt.Errorf("...") }

// 正确（应改为）
func (a *Adapter) Sender() platform.Sender { return &platform.NoopSender{} }
```

---

#### B5：~~`qqEvent.Content()` 未过滤 @ 机器人的标签前缀~~（已确认：不存在此 Bug）

QQ 官方下发 Webhook 事件时会在服务端自动剥离 @ 机器人的前缀，`content` 字段直接为纯净的用户输入内容，无需客户端过滤。**此条目已关闭。**

---

#### B6：`context.Clone()` 中 `*extensionState` 被双重拷贝

**文件**：`core/context/context.go`

`Clone()` 先通过 `Extensions.Snapshot()` 将所有类型键（包括 `*extensionState` 的指针）浅拷贝到新 Context，然后又单独对 `*extensionState` 做深拷贝覆盖。第一次拷贝是无效操作，浪费一次 map 写入。

```go
// 当前：第一个 for 循环把所有 ext 包括 *extensionState 都浅拷贝进去
// 第二个 if 再把 *extensionState 深拷贝覆盖掉
// 修复：在第一个循环中跳过 *extensionState 键，或合并两个操作
```

---

#### B7：`bot.go` 的 `handlePlatformEvent` 使用 NoopSender 兜底时无 Warn 日志

**文件**：`bot.go`

当 `platformRegistry` 中找不到对应平台的适配器，且 `Bot.adapter` 也为 nil 时，静默使用 `NoopSender{}` 兜底，所有 `ctx.Reply()` 调用会被吞掉，不会有任何日志提示。

```go
if sender == nil {
    // 应加上 warn 日志
    logger.WithField("platform", event.Platform()).Warn(
        "[Bot] No sender found for platform, replies will be silently dropped")
    sender = &platform.NoopSender{}
}
```

---

## 三、未实现的功能

### 🔴 必须实现（核心设计承诺但缺失）

#### F1：`platform.Event` 接口缺少 `Attachments()` 方法

`platform.OutboundMessage` 支持发送附件，但 `platform.Event` 接口没有任何方法访问**入站消息的附件**。接收图片/文件的 handler 只能通过 `event.RawPayload().(type)` 类型断言，完全破坏了跨平台抽象。

`dto.MessageCreateEvent` 中已有 `Attachments []dto.Attachment` 字段，但没有抽象到接口层。

**建议**：在 `platform.Event` 接口中增加：
```go
// Attachments 返回消息中携带的附件列表（平台不支持或无附件时返回 nil）
Attachments() []InboundAttachment
```

增加对应的 `InboundAttachment` 结构体（URL、MIME、Size 等通用字段）。

---

#### F2：`context.GetPlatformCapabilities()` 未实现

`platform/adapter.go` 的文档注释中（第 104 行）提到了 `ctx.GetPlatformCapabilities()` 的使用示例，但该方法在 `core/context/` 中**从未实现**。

Handler 无法通过 Context 获取当前平台的能力集合，只能通过 `ctx.GetPlatformEvent().Platform()` 获取平台名再手动查询，违背了设计意图。

---

#### F3：QQ Sender 不支持附件发送（图片/视频/文件）

QQ 富媒体发送需要两步：  
1. 调用 `SingleRichMedia` / `GroupRichMedia` 上传媒体获取 `file_uuid`  
2. 使用 `MediaMessage` 类型携带 `file_uuid` 发送消息

当前 `buildDTOMessage` 完全忽略 `OutboundMessage.Attachments`，`QQCapabilities.FileUpload = true` 承诺了此能力但未实现。

---

#### F4：QQ Sender 不支持按钮/键盘发送

`QQCapabilities.Buttons = true` 声明支持按钮，但 `buildDTOMessage` 完全忽略 `OutboundMessage.Buttons`，QQ 的 Keyboard API 没有接入。

---

#### F5：`platform.Registry` 缺少 `Remove(platform string)` 方法

热更新场景需要在运行时注销某个平台适配器，当前 `Registry` 只有 `Register/Get/All`，无法移除。

---

#### F6：QQ 频道（Guild）消息收发不完整

**接收**：`dto/event.go` 中所有 Guild/Channel 相关的事件结构体（`GuildCreateEvent`、`GuildMemberAddEvent`、`ChannelMessageCreateEvent` 等）均被**注释掉**，实际上不会被解析。

**发送**：`openapi.OpenAPI` 接口没有频道消息发送方法，但 `QQCapabilities.GuildSupport = true`。

---

### 🟡 应该实现（设计文档有描述但未落地）

#### F7：跨平台消息降级策略未实现

`OutboundMessage` 的注释明确描述了字段优先级：`Embeds > Attachments > Markdown > Text`。但实际发送器（如 QQ Sender）中没有任何降级逻辑，只是硬编码判断 Markdown/Text，不按优先级处理。

#### F8：`openapi.OpenAPI` 缺少频道消息及 Guild 相关 API

当前接口只有 6 个方法，缺少：`GuildChannelMessage`、`DirectMessage`、`GuildMemberList` 等。

#### F9：`platform.OutboundMessage` 缺少 `IsEphemeral`（临时消息）字段

Discord 支持只有发送者可见的 ephemeral 消息，Telegram 支持 `reply_to_message_id` 等。部分平台能力无法通过当前 `OutboundMessage` 表达。

---

## 四、设计问题

### D1：`openapi.OpenAPI` 接口暴露了 `gjson.Result` 依赖

**文件**：`platform/qq/openapi/iface.go`

```go
SingleChat(openid string, msg *dto.Message) (gjson.Result, error)
```

返回值类型是 `gjson.Result`，这将 `tidwall/gjson` 作为公共 API 的一部分暴露出去。调用方需要 import gjson 才能处理返回值，不合理。

**建议**：返回更简洁的类型，如 `(messageID string, err error)` 或 `(*dto.SendResponse, error)`。

---

### D2：`qq/sender.go` 通过 `Extra` magic string 传递平台参数，设计脆弱

```go
if v, ok := msg.Extra["msg_seq"]; ok { ... }
if v, ok := msg.Extra["event_id"]; ok { ... }
```

依赖未约束的字符串 key，容易拼写错误且无编译期检查。由于项目未发布，可以设计类型安全的 QQ 专属扩展结构：

```go
// platform/qq 包内
type QQMessageExtra struct {
    MsgSeq  uint64
    EventID string
}
// 使用 platform.OutboundMessage.Extra 存储，但改为类型安全包装
```

---

### D3：`Bot` 同时支持单 adapter 和 registry 两种模式，API 复杂度高

由于项目未发布，可以统一为 **registry-only** 模式。单平台场景只是 registry 中只注册一个 adapter，不需要额外的 `Bot.adapter` 字段和相应的逻辑分支。这可以显著简化 `bot.go`（约 50 行）和 `bot_builder.go`（约 20 行）。

---

### D4：`WebhookServerAdapter` 与 `Adapter` 职责混合

`WebhookServerAdapter` 同时承担"HTTP Webhook 服务器"和"平台适配器"两个职责，内部状态（`server`、`webhook`、`tokenMgr`）混合，导致结构体过于复杂（307 行）。可以考虑拆分：

- `WebhookConn`：仅负责接收事件（HTTP server + event stream）
- `qq.Adapter`：仅负责事件路由（已有，复用即可）

`WebhookServerAdapter` 可以退化为一个工厂函数，而非独立类型。

---

### D5：`qq/event.go` 未使用 `dto.Payload` 对象池

`dto.AcquirePayload()`/`ReleasePayload()` 已在 `dto/pool.go` 中实现，但 `qqEvent` 持有 `payload *dto.Payload` 引用，导致 Payload 在事件处理完之前无法归还池中。

可以在事件解析完成后（`populate()` 执行完）立即提取所有需要的字段到 qqEvent 的独立字段，然后释放 payload，避免事件对象长期持有 payload。

---

## 五、可删除的兼容性代码（项目未发布，可直接移除）

### R1：`factory.go`（根包）

```go
// factory.go — 此文件已清空（仅注释，无代码）
```

整个文件只有注释，无实际代码。可直接删除。

---

### R2：`platform/qq/bot.go` 的 `DefaultWebhookAdapter`

```go
func DefaultWebhookAdapter(addr string, info *dto.BotInfo) *WebhookServerAdapter {
    return NewWebhookServerAdapter(addr, info)
}
```

这是 `NewWebhookServerAdapter` 的无意义别名，增加 API 表面积。可删除。

---

### R3：`platform/qq/openapi/dto/dto.go`

```go
package dto
// （空文件）
```

空文件，只有包声明。可删除。

---

### R4：`core/context/pool.go` 的 `ContextPoolStats` placeholder

```go
type ContextPoolStats struct {
    PoolEnabled bool  // Note: sync.Pool doesn't expose size metrics
}
func GetContextPoolStats() ContextPoolStats {
    return ContextPoolStats{PoolEnabled: true}
}
```

无实际功能的 placeholder，要么实现（用 atomic counter 统计 pool hit/miss），要么删除。

---

### R5：`core/context/permission.go` 中的大量类型别名

由于项目未发布，可以直接让调用方 import `core/permission`，无需在 context 包中维护 `Permission = coreperm.Permission` 等别名，减少间接依赖。

---

### R6：`errors.go`（根包）中对 `errutil` 的冗余别名

```go
var ErrAdapterRequired = errutil.ErrAdapterRequired
```

由于未发布，可以移除根包的错误别名，让调用方直接使用 `errutil.ErrAdapterRequired`，保持单一真相来源。

---

## 六、性能优化建议

### P1：`OutboundMessage` Builder 方法中冗余的临时 slice

**文件**：`platform/message.go`

```go
// 当前（每次都 make + copy + append，分配两次）
func (m OutboundMessage) WithMentions(userIDs ...string) OutboundMessage {
    n := make([]string, len(m.Mentions), len(m.Mentions)+len(userIDs))
    copy(n, m.Mentions)
    m.Mentions = append(n, userIDs...)
    return m
}

// 优化（Go 1.21+ slices.Clone，一次分配）
func (m OutboundMessage) WithMentions(userIDs ...string) OutboundMessage {
    m.Mentions = append(slices.Clone(m.Mentions), userIDs...)
    return m
}
```

同理适用于 `WithButtons`、`WithAttachments`、`WithEmbeds`。

---

### P2：`qq/webhook_server.go` worker 模型与 `qq/adapter.go` 不一致

- `qq/Adapter`：一个专用分发 goroutine 将事件投递到有界 `workCh`，workers 从 `workCh` 消费（Fan-out 模型，分发可控）
- `qq/WebhookServerAdapter`：多个 workers 直接竞争 `eventStream` channel（直接竞争模型）

两种模型都是线程安全的，但前者负载分布更均匀，且可以在 `workCh` 满时施加背压。建议统一使用前者（有界 workCh 分发）。

---

### P3：`dto.Payload` 对象池应在 parse 完成后立即释放

见设计问题 D5。`qqEvent` 不应持有 `payload *dto.Payload`，应在 `populate()` 完成后立即 `dto.ReleasePayload(payload)` 并将 payload 置 nil，减少对象存活时间，降低 GC 压力。

```go
// 修复后 qqEvent 结构
type qqEvent struct {
    // 不再持有 payload 指针
    kind      platform.EventKind
    sender    platform.UserInfo
    chat      platform.ChatInfo
    content   string
    timestamp time.Time
    rawType   string
    id        string
    rawPayload any // 可选，仅供 RawPayload() 使用，可在 populate 后置 nil
}
```

---

### P4：`context.Clone()` 中对 `*extensionState` 的双重拷贝

见 Bug B6，同时也是性能问题。消除第一个 for 循环中对 `*extensionState` 类型键的拷贝（或合并两次操作），减少一次 map 写入和一次不必要的指针浅拷贝。

---

### P5：`Registry.StartAll` / `StopAll` 中 goroutine 对 adapter 变量的捕获

**文件**：`platform/adapter.go`

```go
for _, a := range adapters {
    wg.Go(func() {
        if err := a.Start(ctx, handler); err != nil { ... }  // 捕获 a
    })
}
```

在 Go 1.22+ 中 for 循环变量语义已修正（每次迭代独立变量），但为清晰起见，可以显式捕获：`a := a`，并在注释中说明依赖 Go 1.22+ 语义。

---

## 七、平台设计能力评估

### 优点

1. **接口分离良好**：`platform.Event`（接收）、`platform.Sender`（发送）、`platform.Adapter`（生命周期）职责清晰。
2. **ChatInfo 注入机制合理**：通过 Go context 传递路由信息，避免了全局状态。
3. **Capabilities 渐进增强**：`platform.Capabilities` 设计直观，Handler 可以在运行时检测平台能力。
4. **EventKind 枚举完整**：覆盖了私聊/群组/频道/交互/表���/成员加入离开/消息编辑删除，足以支持绝大多数平台。
5. **Registry 设计简洁**：多平台并发启动/停止，错误合并，日志完善。
6. **Worker pool 设计合理**：`qq/Adapter` 使用有界 channel 的 fan-out 模型，防止高频事件下无限 goroutine 扩张。

### 不足

1. **入站消息抽象不完整**：`platform.Event` 只抽象了 `Content()`（文本），缺少附件/表情/引用消息等字段，跨平台 handler 难以统一处理富媒体消息。
2. **发送器没有返回消息 ID**：`Sender.Send()` 只返回 `error`，无法获取发送成功后的消息 ID（用于后续编辑/删除/引用回复）。
3. **`MessageEditor`/`MessageDeleter` 只有接口，没有任何实现**：QQ 支持消息撤回（`SingleReset`/`GroupReset`），但 sender 未实现 `MessageDeleter`。
4. **缺少 InboundReaction 等事件的具体字段**：`EventKindReaction` 有枚举但没有对应的 payload 访问方式（需要类型断言 RawPayload）。
5. **发送失败后没有 DLQ 集成**：`infra/dlq` 包存在，`platform/qq/dlq` 有类型别名，但 Sender 层没有自动 DLQ 策略。

---

## 八、修复优先级汇总

| 编号 | 类别 | 优先级 | 预计工作量 | 状态 |
|------|------|--------|------------|------|
| B1 | `GetUserID()` 返回空字符串 | 🔴 P0 | 小（改 1 个方法）| ✅ 已修复 |
| B2 | QQ Sender 未处理 Mentions | 🔴 P0 | 小 | ✅ 已修复 |
| B3 | QQ Sender 无 Guild 发送路径 | 🔴 P0 | 中 | ✅ 已修复（返回明确错误） |
| F1 | `platform.Event` 缺 `Attachments()` | 🔴 P0 | 中（接口+QQ实现） | ⏳ 待实现 |
| F2 | `ctx.GetPlatformCapabilities()` 未实现 | 🟠 P1 | 小 | ⏳ 待实现 |
| B4 | discord/telegram 重复 noopSender | 🟠 P1 | 小 | ✅ 已修复 |
| B5 | QQ content AT 标签未过滤 | 🟠 P1 | — | ❌ 不存在此 Bug（官方已过滤） |
| F3 | QQ Sender 不支持附件发送 | 🟠 P1 | 大 | ⏳ 待实现 |
| F4 | QQ Sender 不支持按钮 | 🟠 P1 | 大 | ⏳ 待实现 |
| F6 | QQ 频道消息收发不完整 | 🟠 P1 | 大 | ⏳ 待实现 |
| D1 | OpenAPI 返回 gjson.Result | 🟡 P2 | 中 | ⏳ 待处理 |
| D2 | Extra magic string 参数 | 🟡 P2 | 中 | ⏳ 待处理 |
| F5 | Registry 缺 Remove 方法 | 🟡 P2 | 小 | ⏳ 待实现 |
| B6 | Clone 双重拷贝 | 🟡 P2 | 小 | ✅ 已修复 |
| B7 | NoopSender fallback 无日志 | 🟡 P2 | 小 | ✅ 已修复 |
| D3 | 统一为 registry-only | 🟡 P2 | 中 | ⏳ 待处理 |
| P1-P5 | 性能优化 | 🟢 P3 | 小-中 | ⏳ 待处理 |
| R1-R6 | 删除兼容代码 | 🟢 P3 | 小 | ⏳ 待处理 |
| D4 | WebhookServerAdapter 职责拆分 | 🟢 P3 | 大 | ⏳ 待处理 |

---

*本文档由自动化代码审查生成，建议在修复 P0 级 Bug 后重新运行全量测试。*

