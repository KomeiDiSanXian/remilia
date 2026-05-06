# Platform 多平台抽象层 —— 审查报告

> 分支：`feature/multi-platform-abstraction`  
> 审查时间：2026-03-21  
> 状态：**未发布（可大幅调整）**

---

## 目录

1. [整体设计评估](#1-整体设计评估)
2. [已确认 Bug / 需修复问题](#2-已确认-bug--需修复问题)
3. [未实现 / 功能缺口](#3-未实现--功能缺口)
4. [设计层面的改进建议](#4-设计层面的改进建议)
5. [性能优化清单](#5-性能优化清单)
6. [测试覆盖缺口](#6-测试覆盖缺口)
7. [优先级总览](#7-优先级总览)

---

## 1. 整体设计评估

### 1.1 架构层次

```
BotBuilder / Bot
    └── platform.Registry          ← 多适配器注册表
            ├── qq.WebhookServerAdapter   (已实现)
            ├── discord.Adapter           (skeleton)
            ├── telegram.Adapter          (skeleton)
            └── wechat.Adapter            (skeleton)

platform.Adapter  ← 核心接口
    ├── Platform() string
    ├── Start(ctx, handler)
    ├── Stop(ctx)
    ├── Sender() platform.Sender   ← 发送器
    ├── Capabilities()             ← 能力声明
    └── IsRunning() bool

platform.Event    ← 入站事件抽象
    ├── 必选字段：Kind/ID/Sender/Chat/Content/Timestamp/Attachments
    └── 可选接口扩展：RawEvent / EditableEvent / ReplyEvent

platform.OutboundMessage  ← 出站消息（值类型，链式 Builder）
```

### 1.2 设计优点

| 优点 | 说明 |
|------|------|
| **接口最小化** | `Adapter` 接口仅 5 个方法，实现门槛低 |
| **可选接口模式** | `RawEvent`、`EditableEvent`、`ReplyEvent` 通过类型断言按需使用，不强迫低能力平台实现 |
| **渐进增强** | `Capabilities` + `GetPlatformCapabilities()` 使 Handler 可在运行时降级 |
| **显式路由** | `SendRequest{Target, EventID, Message}` 替代 context.WithValue 注入，契约可见且编译期安全 |
| **Worker Pool** | QQ 适配器使用有界 worker pool，高频事件下不会无限创建 goroutine |
| **对象池复用** | `dto.Payload` + `context.Context` 均通过 `sync.Pool` 复用，热路径零堆分配 |
| **COW 引擎** | Engine 读操作完全无锁，5–6× 性能提升 |
| **gjson 快路径** | QQ 事件解析使用 `gjson.GetManyBytes` 一次线性扫描，避免多次 JSON 遍历 |

### 1.3 设计层面的核心问题（见第 4 节详述）

- `Capabilities` 缺少若干关键能力字段（Reactions、ThreadSupport 等）
- 入站 `Event` 接口没有暴露 `Mentions()`（@用户列表）
- `Adapter` 接口没有重连/断线通知机制
- `Sender()` 返回的是 `platform.Sender`，但 `MessageEditor`/`MessageDeleter` 是单独接口——要删/编辑消息需要先断言 Adapter，路径不直观
- 发送成功后没有返回 `MessageID`，无法实现无状态 Edit/Delete

---

## 2. 已确认 Bug / 需修复问题

### B-1 `wechat/adapter.go` 注释方法名错误（低危）

**文件**：`platform/wechat/adapter.go:43`

```go
// StartPlatform implements platform.Adapter (not yet implemented).   // ← 错误
func (a *Adapter) Start(_ stdctx.Context, _ func(platform.Event)) error {
```

实际方法名是 `Start`，注释写的是 `StartPlatform`，应统一为：

```go
// Start implements platform.Adapter (not yet implemented).
```

---

### B-2 `adapter.go` 注释中 `wg.Go()` 版本标注错误（低危）

**文件**：`platform/adapter.go:240`

```go
wg.Go(func() { // 这部分依赖 Go 1.22+ 语义（闭包捕获）
```

`sync.WaitGroup.Go()` 是 **Go 1.25** 新增方法（`go.mod` 已声明 `go 1.25.0`）。闭包按值捕获循环变量是 **Go 1.22** 的改动，二者不是同一件事。注释应拆开说明：

```go
// wg.Go 是 Go 1.25 新增方法；循环变量按值捕获依赖 Go 1.22+ 语义。
wg.Go(func() {
```

---

### B-3 `WebhookConn.stop()` 在外部注入 API 时 `webhookImpl` 未清空（中危）

**文件**：`platform/qq/webhook_conn.go:196–213`

```go
if tokenMgr != nil {
    tokenMgr.Stop()
    c.mu.Lock()
    c.tokenMgr = nil
    c.api = nil
    c.webhookImpl = nil   // ← 只有 tokenMgr != nil 时才清空
    c.mu.Unlock()
}
```

当通过 `WithAPI()` 外部注入 API 时，`tokenMgr == nil`，`stop()` 结束后 `c.webhookImpl` 不会被置 `nil`。
下次重启时 `EventStream()` 返回旧的（已关闭）channel，导致 QQ Adapter 立即退出。

**修复**：将 `webhookImpl` 清空逻辑移到 tokenMgr 判断之外：

```go
if tokenMgr != nil {
    tokenMgr.Stop()
}
c.mu.Lock()
c.tokenMgr = nil
c.api = nil
c.webhookImpl = nil
c.mu.Unlock()
```

---

### B-4 `Registry.StartAll` 非 ctx 取消错误只返回第一个（中危）

**文件**：`platform/adapter.go:261–267`

```go
for _, err := range errs {
    if !errors.Is(err, stdctx.Canceled) && !errors.Is(err, stdctx.DeadlineExceeded) {
        return err   // ← 只返回第一个，其余平台的错误丢失
    }
}
```

多个平台同时崩溃时只上报一个错误，调试困难。应使用 `errors.Join`：

```go
var fatalErrs []error
for _, err := range errs {
    if !errors.Is(err, stdctx.Canceled) && !errors.Is(err, stdctx.DeadlineExceeded) {
        fatalErrs = append(fatalErrs, err)
    }
}
return errors.Join(fatalErrs...)
```

---

### B-5 `Bot.Start()` 每次重启重复注册适配器健康检查器（中危）

**文件**：`bot.go:140–155`

```go
for _, pa := range reg.All() {
    b.health.AddChecker(NewAdapterHealthChecker(pa))  // ← 没有去重
    ...
}
```

热重启（`Stop()` + `Start()`）后健康检查器会重复叠加，导致同一平台多次出现在健康报告中。

**修复**：记录已注册的 checker 名称，或在 `buildBaseLifecycle` 时重置 `health.Check`：

```go
b.health = health.NewCheck()  // 重建，避免叠加
b.health.AddChecker(NewBotStatusChecker(b))
b.health.AddChecker(health.NewEngineHealthChecker(b.engine))
```

---

### B-6 ~~`webhook.handleDispatch` 在 `bigCache` 路径存储已归还的 `Raw` slice~~（已解决）

**已通过移除 bigcache 整体解决**。

`bigcache` 的职责（事件去重）已从 webhook 层剥离，由上层中间件负责。
`handleDispatch` 现在是无分支的单路径：直接非阻塞投递 payload 到 `eventChan`，
channel 满时立即归还 payload 到对象池并记录统计信息。

同步移除了：`DedupOptions` 结构体、`NewWithOptions` 构造函数、`maxInt`/`ifZeroDuration` 辅助函数，
`NewWebhook`/`NewWithBuffer` 不再需要 `context.Context` 参数（bigcache 的后台清理 goroutine 是唯一的 ctx 消费者）。
`github.com/allegro/bigcache/v3` 已从 `go.mod` 移除。

---

## 3. 未实现 / 功能缺口

### 3.1 平台适配器（skeleton 状态）

| 平台 | 状态 | 缺少内容 |
|------|------|----------|
| `platform/discord` | 🔴 skeleton | 全部：事件循环、发送器、Webhook/WS 连接 |
| `platform/telegram` | 🔴 skeleton | 全部：事件循环、发送器、Bot API 连接 |
| `platform/wechat` | 🔴 skeleton | 全部：事件循环、发送器、企业微信/公众号连接 |

（Discord/Telegram/WeChat 适配器本次不要求完成，但 skeleton 应能优雅报错，目前已实现。）

---

### 3.2 QQ 适配器中的功能缺口

#### F-1 频道消息发送（GuildMessage）未实现

**文件**：`platform/qq/sender.go:50–52`

```go
if chat.ParentID != "" {
    return fmt.Errorf("qq sender: guild channel message sending is not yet supported ...")
}
```

QQ 频道 API（`/channels/{channel_id}/messages`）与群/私聊 API 完全不同。
需要在 `openapi.OpenAPI` 接口和 `Client` 中新增 `ChannelChat` 方法，并在 `qqSender.Send` 中分发。

#### F-2 按钮/键盘（Buttons）未传递给 QQ API

**文件**：`platform/qq/sender.go:buildDTOMessage`

`OutboundMessage.Buttons` 字段在 `buildDTOMessage` 中**完全被忽略**，
但 `QQCapabilities.Buttons = true` 声称支持。
需要将 `[]platform.Button` 映射为 QQ 的 Keyboard 结构体并设置到 `dto.Message`。

#### F-3 二进制附件上传（Attachment.Data）未实现

**文件**：`platform/qq/sender.go:74`

```go
if len(att.Data) > 0 {
    return fmt.Errorf("qq sender: binary attachment upload is not yet supported; ...")
}
```

QQ 富媒体 API 要求先上传文件获取 `file_info`，直传功能待实现。

#### F-4 `qqEvent` 未实现 `ReplyEvent` 接口

**文件**：`platform/qq/event.go`

QQ 频道消息（`AT_MESSAGE_CREATE`）的 Detail 包含 `message_reference.message_id` 字段（被回复消息 ID），
但 `qqEvent` 没有实现 `platform.ReplyEvent`（`ReplyToID() string`），
导致 `platform.GetReplyToID(event)` 永远返回空字符串。

**补充字段**：

```go
type qqEvent struct {
    ...
    replyToID string  // 新增
}
```

在 `populateGuildMessage` 中提取：

```go
e.replyToID = results[...].Get("message_reference.message_id").String()
```

并添加方法：

```go
func (e *qqEvent) ReplyToID() string { return e.replyToID }
```

#### F-5 `MsgSeq`（消息序列号）需手动注入，无自动管理

QQ v2 API 要求每条消息携带递增的 `msg_seq`，目前完全依赖调用方通过
`qq.ApplyExtra(msg, qq.MessageExtra{MsgSeq: n})` 手动传递，框架没有提供自动管理机制，
容易导致重复序列号或遗漏。

建议在 `qqSender` 内维护一个 `atomic.Uint64` 作为自增计数器。

---

### 3.3 platform 抽象层中的功能缺口

#### F-6 `Event` 接口缺少 `Mentions()` 方法

入站消息中 @ 的用户列表（QQ `group_at_message`、Discord mentions、Telegram entities 中的 mention）
无法通过平台无关接口获取，逼迫 Handler 使用 `RawPayload()` 绕过抽象层。

建议在 `Event` 接口中新增（或作为可选接口 `MentionsEvent`）：

```go
// MentionsEvent 可选接口：消息携带 @ 用户列表时实现。
type MentionsEvent interface {
    Mentions() []UserInfo
}
```

#### F-7 发送响应中无 `MessageID` 返回

`Sender.Send()` 返回 `error`，发送成功后无法得知平台分配的消息 ID，
导致无法实现无状态的 Edit/Delete（必须在外部自行缓存）。

建议新增 `SendResult` 返回值：

```go
type SendResult struct {
    MessageID string  // 平台分配的消息 ID，不支持时为空
}

// Sender 接口方法签名变更
Send(ctx stdctx.Context, req SendRequest) (SendResult, error)
```

> 注意：这是一个破坏性 API 变更，应在发布前确定。

#### F-8 `Adapter` 接口缺少重连/断线通知机制

适配器意外断连时，`Start()` 返回 error，上层（`Registry.StartAll`）收到后只能整体报错，
没有内置的重连策略或事件通知钩子。

建议在接口中增加（可选）：

```go
// RecoverableAdapter 可选接口：支持自动重连的适配器实现此接口。
type RecoverableAdapter interface {
    Adapter
    // OnDisconnect 注册断连回调，用于触发重连或告警
    OnDisconnect(fn func(err error))
}
```

或在 `Adapter.Start` 的约定中明确：返回 `ErrRetryable` 表示可重试，`ErrFatal` 表示不可重试。

---

### 3.4 其他缺口

| 编号 | 位置 | 描述 |
|------|------|------|
| F-9 | `platform/qq/openapi/iface.go` | `OpenAPI` 接口缺少频道消息方法（`ChannelChat`） |
| F-10 | `platform/adapter.go` | `Registry` 没有 `Replace` 方法（原子替换运行中的适配器） |
| F-11 | `platform/adapter.go` | `Registry.StartAll` 没有各平台独立重试策略 |
| F-12 | `core/engine/process_platform.go` | `ProcessPlatformEventBatch` 是顺序处理，无并发批处理 |

---

## 4. 设计层面的改进建议

### D-1 `Capabilities` 补充缺失字段

```go
type Capabilities struct {
    // ...已有字段...

    // Reactions 是否支持表情回应（Discord/Telegram/QQ 均支持）
    Reactions bool
    // ThreadReply 是否支持消息回复链/引用回复
    ThreadReply bool
    // TypingIndicator 是否支持"正在输入"状态
    TypingIndicator bool
    // MentionAll 是否支持 @全体成员
    MentionAll bool
    // VoiceChannel 是否支持语音频道（Discord Stage/VC）
    VoiceChannel bool
}
```

---

### D-2 `MessageEditor`/`MessageDeleter` 访问路径优化

目前要编辑或删除消息，需要：

```go
sender := adapter.Sender()
if editor, ok := sender.(platform.MessageEditor); ok { ... }
```

但 `Sender()` 返回的是 `platform.Sender`，其底层类型是平台内部类型，外部难以断言。
建议在 `Adapter` 接口中直接暴露可选能力：

```go
// EditorAdapter 可选接口：支持消息编辑的适配器实现此接口。
type EditorAdapter interface {
    Adapter
    Editor() MessageEditor
}
```

或统一通过 `Capabilities()` + 帮助函数访问：

```go
func GetEditor(a Adapter) (MessageEditor, bool) {
    if e, ok := a.Sender().(MessageEditor); ok {
        return e, true
    }
    return nil, false
}
```

---

### D-3 `OutboundMessage.Extra` 类型安全改进

`map[string]any` 的 `Extra` 字段缺乏类型约束，且每次 `WithExtra` 都需要 `maps.Copy`（O(n)）。

建议引入 `PlatformExtra` 接口，让平台专属参数实现该接口并直接存储：

```go
// PlatformExtra 平台专属扩展参数标记接口。
type PlatformExtra interface {
    platformExtraTag()
}

type OutboundMessage struct {
    ...
    PlatformExtra PlatformExtra  // 替代 Extra map，类型安全，零拷贝
}
```

QQ 适配器：

```go
type QQExtra struct {
    MsgSeq  uint64
    EventID string
}
func (QQExtra) platformExtraTag() {}
```

---

### D-4 `Registry.Sender()` / `Registry.Capabilities()` 快捷方法

`Bot.handlePlatformEvent` 每次收到事件都需要：

```go
if pa, ok := reg.Get(event.Platform()); ok {
    sender = pa.Sender()
    caps = pa.Capabilities()
}
```

建议在 `Registry` 上直接提供：

```go
func (r *Registry) SenderFor(platform string) (Sender, bool)
func (r *Registry) CapabilitiesFor(platform string) (Capabilities, bool)
```

以减少调用方模板代码，同时便于后续加缓存优化（见性能 P-1）。

---

## 5. 性能优化清单

### P-1 热路径中 `reg.Get()` 每事件加 RLock（高优先级）

**位置**：`bot.go:handlePlatformEvent`

```go
if pa, ok := reg.Get(event.Platform()); ok {  // ← 每事件加 RLock
    sender = pa.Sender()
    caps = pa.Capabilities()
}
```

每个入站事件都要经历一次 `sync.RWMutex.RLock/RUnlock` + map 查找。
在高并发场景（多平台同时涌入事件）下，RLock 的竞争会成为瓶颈。

**优化**：适配器 `Start()` 后，在 `Bot` 内建立一个 `map[string]adapterCache`（无锁 snapshot），
仅在 `UsePlatformRegistry()` 时重建，热路径直接读 snapshot：

```go
type adapterCache struct {
    sender platform.Sender
    caps   platform.Capabilities
}
// 在 Bot.Start() 时构建，之后只读
var adapterSnapshot map[string]adapterCache
```

---

### P-2 `NewContextFromEvent` 中多余的 `ctxMu.Lock()`（中优先级）

**位置**：`core/context/platform_event.go:25–29`

```go
ctx.ctxMu.Lock()
if ctx.ctx == nil {
    ctx.ctx = stdctx.Background()
}
ctx.ctxMu.Unlock()
```

`ReleaseContext` 已经在归还时将 `ctx.ctx` 设为 `stdctx.Background()`，
从池中取出的 Context `ctx.ctx` 不可能为 `nil`。
该检查及锁可安全删除：

```go
// ctx.ctx 在 ReleaseContext 时已被设为 stdctx.Background()，无需再检查
```

---

### P-3 `parseAttachments` 缺少预分配（中优先级）

**位置**：`platform/qq/event.go:parseAttachments`

```go
var out []platform.InboundAttachment
r.ForEach(func(_, v gjson.Result) bool {
    ...
    out = append(out, att)  // ← 无预分配，最坏 O(log n) 次扩容
    return true
})
```

`gjson.Result` 的数组长度可通过 `r.Raw` 估算，或先用 `r.ForEach` 统计长度再分配：

```go
count := 0
r.ForEach(func(_, _ gjson.Result) bool { count++; return true })
out := make([]platform.InboundAttachment, 0, count)
r.ForEach(func(_, v gjson.Result) bool { ... })
```

或利用 `gjson` 的 `r.Array()` 先获取切片（会分配一次但避免多次扩容）。

---

### P-4 `webhook.handleDispatch` 重复的丢弃日志代码（低优先级）

**位置**：`webhook.go:handleDispatch`

`bigCache == nil` 和有 `bigCache` 两个分支各有一段完全相同的"丢弃事件"日志代码（约 15 行）。
提取为内联函数可减少代码量并避免后续维护不一致：

```go
dropPayload := func(payload *dto.Payload) {
    dto.ReleasePayload(payload)
    dropped := c.droppedEvents.Add(1)
    total := c.totalEvents.Load()
    // ... 统一的日志逻辑
}
```

---

### P-5 `WithExtra` 的 `maps.Copy` 在高频场景的开销（低优先级）

**位置**：`platform/message.go:WithExtra`

```go
newExtra := make(map[string]any, len(m.Extra)+1)
maps.Copy(newExtra, m.Extra)
```

每次调用都分配新 map 并拷贝所有已有 kv。
对于只有 1 个 extra 的高频场景（QQ `EventID`），可考虑用固定大小的结构体替代 map（见 D-3），
或在 `Extra` 为空时跳过 Copy 直接创建单元素 map。

```go
func (m OutboundMessage) WithExtra(key string, value any) OutboundMessage {
    if len(m.Extra) == 0 {
        m.Extra = map[string]any{key: value}
        return m
    }
    newExtra := make(map[string]any, len(m.Extra)+1)
    maps.Copy(newExtra, m.Extra)
    newExtra[key] = value
    m.Extra = newExtra
    return m
}
```

---

### P-6 `Registry.All()` 在每次事件时重复分配切片

**位置**：`bot.go:handlePlatformEvent` → `reg.Get()` 已经是 O(1) RLock+map，
但 `Registry.StartAll` 内部调用 `r.All()` 创建临时切片。
该路径只在启动时调用，不是热路径，可暂缓。
但如果后续有"在事件处理时遍历所有适配器"的需求，需要提前规避。

---

### P-7 QQ `qqEvent` 结构体内存布局优化

**位置**：`platform/qq/event.go:qqEvent`

```go
type qqEvent struct {
    kind        platform.EventKind     // string（16B）
    sender      platform.UserInfo      // 3x string（48B）
    chat        platform.ChatInfo      // 4x string + bool（65B+padding）
    content     string                 // 16B
    timestamp   time.Time              // 24B
    attachments []platform.InboundAttachment  // 24B（slice header）
    id          string                 // 16B
    rawType     string                 // 16B
}
```

当前字段排列未按对齐优化。`bool` 字段（`ChatInfo.IsGroup`）嵌入在 `ChatInfo` 中，
拖尾 padding 明显。可考虑将高频访问字段（`kind`、`content`、`id`）前置。
（影响较小，可作为低优先级 follow-up）

---

## 6. 测试覆盖缺口

| 文件 | 问题 |
|------|------|
| `platform/discord/adapter.go` | 无测试文件 |
| `platform/telegram/adapter.go` | 无测试文件 |
| `platform/wechat/adapter.go` | 无测试文件 |
| `platform/qq/sender.go` | 无独立测试；频道消息、附件、Buttons 均无覆盖 |
| `core/engine/process_platform.go` | `ProcessPlatformEventBatch` 无测试 |
| `platform/adapter.go` | `Registry.StartAll` 的多错误合并路径（B-4）无测试 |
| `bot.go` | `handlePlatformEvent` sender=nil 降级路径无测试 |
| `bot.go` | 热重启（Stop+Start）后健康检查器重复问题（B-5）无测试 |

---

## 7. 优先级总览

### 🔴 发布前必须修复

| ID | 描述 | 文件 |
|----|------|------|
| B-3 | `WebhookConn.stop()` 外部注入 API 时 `webhookImpl` 未清空（热重启泄漏） | `webhook_conn.go` |
| B-4 | `Registry.StartAll` 多平台错误只返回第一个 | `adapter.go` |
| B-5 | `Bot.Start()` 热重启时重复注册健康检查器 | `bot.go` |
| F-2 | QQ Sender 声称支持 Buttons 但 `buildDTOMessage` 完全忽略 `Buttons` 字段 | `sender.go` |
| F-7 | `Sender.Send` 无法返回 `MessageID`（API 破坏性变更须发布前定稿） | `adapter.go` |

### 🟡 发布前建议修复

| ID | 描述 | 文件 |
|----|------|------|
| B-1 | `wechat/adapter.go` 注释方法名错误 | `wechat/adapter.go` |
| B-2 | `wg.Go()` 版本注释不准确 | `adapter.go` |
| D-1 | `Capabilities` 补充缺失字段 | `adapter.go` |
| F-4 | QQ `qqEvent` 未实现 `ReplyEvent` 接口 | `event.go` |
| F-5 | QQ MsgSeq 无自动管理机制 | `sender.go`, `extra.go` |
| F-6 | `Event` 接口缺少 `Mentions()`/`MentionsEvent` | `event.go` |
| P-1 | 热路径 `handlePlatformEvent` 每事件 RLock，建议缓存 sender/caps snapshot | `bot.go` |
| P-2 | `NewContextFromEvent` 中多余 `ctxMu.Lock()` | `platform_event.go` |
| P-5 | `WithExtra` 空 map 时跳过 `maps.Copy` | `message.go` |

### 🟢 可延后（发布后迭代）

| ID | 描述 |
|----|------|
| ~~B-6~~ | ~~bigcache 路径存储 `payload.Raw` 引用注释说明~~ | **已解决**：bigcache 已整体移除 |
| D-2 | `MessageEditor`/`MessageDeleter` 访问路径优化 |
| D-3 | `Extra map[string]any` 类型安全改进 |
| D-4 | `Registry.SenderFor` / `CapabilitiesFor` 快捷方法 |
| F-1 | QQ 频道消息发送（GuildMessage） |
| F-3 | QQ 二进制附件直传 |
| F-8 | `Adapter` 接口重连/断线通知机制 |
| F-9 | `OpenAPI` 接口补充 `ChannelChat` |
| F-10 | `Registry.Replace` 原子替换适配器 |
| P-3 | `parseAttachments` 预分配 |
| P-4 | `webhook.handleDispatch` 提取丢弃日志函数 |
| P-6 | `Registry.All()` 切片分配优化 |
| P-7 | `qqEvent` 结构体内存布局优化 |
| 测试 | discord/telegram/wechat skeleton 测试、Sender 单元测试、热重启测试 |

