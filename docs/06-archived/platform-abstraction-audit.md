# Platform 抽象层审计报告

> 审计日期：2026-03-21  
> 审计范围：`platform/`、`platform/qq/`、`core/context/platform_event.go`、`core/engine/process_platform.go`、`bot.go`、`bot_builder.go`

---

## 一、总体进度评估

| 模块 | 状态 |
|---|---|
| `platform/` 核心抽象接口 | ✅ 完整，设计合理 |
| `platform/qq/` 完整适配器 | 🟡 功能部分完整（频道发送、二进制附件未实现） |
| `core/context` 平台集成层 | 🟡 基本完整，有一处 bug |
| `core/engine` 处理入口 | ✅ 完整 |
| `bot.go / bot_builder.go` | ✅ 完整 |
| `platform/discord/` | ❌ 仅 stub，未实现 |
| `platform/telegram/` | ❌ 仅 stub，未实现 |
| `platform/wechat/` | ❌ 仅 stub，未实现 |

---

## 二、Bug / 问题（需修复）

### B1. `Context.Clone()` 未复制 `platformCaps` ⚠️ 高优先级

**文件**：`core/context/context.go` → `Clone()`

`Clone()` 会复制 `platformEvent` 和 `platformSender`，但没有复制 `platformCaps`。  
这导致在中间件或 handler 中调用 `ctx.Clone()` 后异步使用 `ctx.GetPlatformCapabilities()` 时，返回空的 `Capabilities{}`，无法做渐进增强判断。

```go
// 当前代码（有问题）
newCtx := &Context{
    platformEvent:  ctx.platformEvent,
    platformSender: ctx.platformSender,
    // platformCaps 未复制！
}

// 应改为：
newCtx := &Context{
    platformEvent:  ctx.platformEvent,
    platformSender: ctx.platformSender,
    platformCaps:   ctx.platformCaps,  // 补充
}
```

---

### B2. `platform/qq/dlq/compat.go` 与 D5 设计冲突 ⚠️ 高优先级

**文件**：`platform/qq/dlq/compat.go`

D5 优化（`qq/event.go`）在 `NewEvent` 返回后立即将 `*dto.Payload` 释放回对象池。  
而 `dlq.PayloadQueue`（`dlq.Queue[*dto.Payload]`）用于存储和延迟处理 `*dto.Payload` 指针——这意味着：

- 如果 DLQ 入队的是已调用 `dto.ReleasePayload` 后的指针，消费时访问的是被复用（可能已污染）的对象。
- 当前 QQ 处理路径中 `NewEvent` 会立即释放 payload，因此这个类型实际上**已无安全的使用场景**。

**建议**：将此包改为基于 `platform.Event` 的泛型死信队列（`dlq.Queue[platform.Event]`），或直接删除。若需要存储原始字节以便重放，应在释放前拷贝 `payload.Raw`。

---

### B3. `qq/sender.go` 的 `sendAttachment` 忽略 context

**文件**：`platform/qq/sender.go`

```go
func (s *qqSender) sendAttachment(_ stdctx.Context, chat platform.ChatInfo, ...) error {
```

context 参数被丢弃，而底层 `api.GroupRichMedia` / `api.SingleRichMedia` 调用的 httpclient 无法感知调用方的超时/取消信号。应将 ctx 传递给每个 API 调用（需要同步更新 `openapi.OpenAPI` 接口和 `openapi.Client` 实现）。

---

### B4. `WebhookConn` 同时注册 `/webhook` 和 `/` 路由

**文件**：`platform/qq/webhook_conn.go`

```go
mux.HandleFunc("/webhook", c.webhookImpl.Handle)
mux.HandleFunc("/", c.webhookImpl.Handle)
```

`/` 会匹配所有未匹配的路径（Go 的 `ServeMux` 行为），包括 `/health`、`/favicon.ico` 等，导致非 QQ webhook 请求也被尝试解析，可能产生噪音日志和错误。

**建议**：只保留 `/webhook`，或提供配置项让用户指定路径，删除 `"/"` 的兜底注册。

---

### B5. `Registry.StartAll()` 使用了已废弃的 `wg.Go()` 注释

**文件**：`platform/adapter.go`

代码中有注释：
```go
wg.Go(func() { // 这部分依赖 Go 1.22+ 语义（闭包捕获）
```

`sync.WaitGroup.Go()` 是 Go 1.22 新增的方法（当前已不是新特性）。注释应更新为说明这是标准 API，移除"依赖 Go 1.22+"的措辞，避免混淆。同时 `StopAll` 使用同样的方式但没有注释，两处风格不统一。

---

## 三、未实现的功能

### N1. QQ 频道消息（Guild Channel）发送

**文件**：`platform/qq/sender.go`

```go
if chat.ParentID != "" {
    return fmt.Errorf("qq sender: guild channel message sending is not yet supported ...")
}
```

QQ 频道（v1 API）的发送接口与群聊/私聊（v2 API）完全不同，需要独立实现。  
`openapi.OpenAPI` 接口和 `openapi.Client` 均没有频道消息发送方法。

**补充范围**：
- `openapi.OpenAPI` 接口添加 `ChannelMessage(channelID string, msg *dto.Message) (gjson.Result, error)`
- `openapi.Client` 实现上述方法
- `qq/sender.go` 根据 `chat.ParentID != ""` 分支调用频道 API

---

### N2. QQ 二进制附件上传

**文件**：`platform/qq/sender.go`

```go
if len(att.Data) > 0 {
    return fmt.Errorf("qq sender: binary attachment upload is not yet supported; use URL attachment instead")
}
```

目前仅支持 URL 附件（服务器拉取），不支持本地 `[]byte` 数据直传。

---

### N3. QQ 按钮/键盘（Keyboard）发送

`platform.OutboundMessage.Buttons` 字段在 `qq/sender.go` 的 `buildDTOMessage()` 中**完全被忽略**。  
QQ v2 API 支持 Keyboard 组件（需申请权限），应在 `dto.Message` 中添加 `Keyboard` 字段并在 `buildDTOMessage` 中映射。

---

### N4. QQ Markdown 模板参数（`CustomTemplateID` / `Params`）

`dto.Markdown` 支持 `CustomTemplateID` 和 `Params`，但 `platform.OutboundMessage.Markdown` 仅是纯字符串，无法传递模板参数。

**建议**：在 `platform.OutboundMessage` 中添加可选的 `MarkdownTemplate` 字段，或通过 `qq.ApplyExtra` 机制传递模板参数。

---

### N5. 平台适配器健康检查无实际内容

**文件**：`bot_health.go` → `AdapterHealthChecker.Check()`

该检查器对非 nil adapter 始终返回 `Healthy`，没有检查适配器是否真实运行中（无 `IsRunning()` 方法）。

同时，使用 `registry-only` 模式（`BotBuilder.WithPlatformRegistry`）时，`NewBot(nil, eng)` 分支**不为任何平台注册** `AdapterHealthChecker`，健康检查只有 bot 状态和 engine 状态。

**建议**：
1. 在 `platform.Adapter` 接口添加 `IsRunning() bool`（或通过 `platform.HealthReporter` 可选接口实现）
2. 在 `Bot.Start()` 时为 registry 中所有适配器注册 health checker

---

### N6. 缺少 `OnPlatform(platform string)` 规则

**文件**：`core/context/rules.go`

多平台架构下，有时需要只对特定平台的事件响应（如某命令只在 QQ 生效，某命令只在 Telegram 生效）。目前没有内置规则实现此筛选，用户需要手动实现。

```go
// 建议添加：
func OnPlatform(platformID string) Rule {
    return func(ctx *Context) bool {
        return ctx.GetEventPlatform() == platformID
    }
}
```

---

### N7. `platform.Event` 接口缺少 `ReplyToID()`

某些平台（Telegram、Discord）的入站消息包含"被回复消息 ID"（用于消息线程）。目前 `platform.Event` 接口没有 `ReplyToID() string` 方法，框架层无法感知回复链。

---

## 四、可以删除的冗余/兼容性代码

### R1. `dto.Payload.Decode()` + `decodeMessageCreateEvent()` — 死代码

**文件**：`platform/qq/openapi/dto/payload.go`

`qq/event.go` 使用 `gjson.GetManyBytes()` 直接解析 `detail`，从未调用 `payload.Decode()`。  
`payload.go` 中的 `Decode()` 方法（包含 `C2CMessageCreateEvent` / `GroupAtMessageCreateEvent` fast-path）和 `decodeMessageCreateEvent()` 函数均为**死代码**。

可以安全删除：`Decode()`、`decodeMessageCreateEvent()`。

---

### R2. `dto.C2CMessageCreateEvent` / `dto.GroupAtMessageCreateEvent` / `dto.MessageCreateEvent` 结构体 — 几乎死代码

**文件**：`platform/qq/openapi/dto/event.go`

这些结构体在生产代码路径（`qq/event.go`）中不再使用，仅在 `testbot/testbot.go` 的 `inject()` 便捷方法中用于构造测试 payload。  
可以将 `testbot` 改为直接构造 `json.RawMessage`，然后删除这些结构体，减少维护面。

---

### R3. `platform/qq/bot.go` — 空文件

**文件**：`platform/qq/bot.go`

文件内容仅有注释（说明不能直接调用 `remilia.NewBotBuilder()` 的原因），没有任何可执行代码。  
这些文档应移入 `platform/qq/` 包的 `doc.go` 或 README，然后删除 `bot.go`。

---

### R4. `dto.OperationCodeName` map — 疑似未使用

**文件**：`platform/qq/openapi/dto/payload.go`

```go
var OperationCodeName = map[OperationCode]string{ ... }
```

在整个代码库中找不到对该 map 的引用。可以删除，或仅在调试日志确实需要时保留。

---

### R5. `dto.Payload.Clone()` — 使用场景消失

D5 优化后，`Payload` 在 `NewEvent` 结束时即被释放，不应再被 Clone 和持有。  
`Clone()` 的存在暗示可以安全持有 Payload，与当前设计矛盾。  
**建议删除**，如有异步重放需求应改为拷贝 `payload.Raw []byte`。

---

### R6. `dto.MessageBuilder` — 与 `platform.OutboundMessage` 并联冗余

**文件**：`platform/qq/openapi/dto/builder.go`

`dto.MessageBuilder` 提供链式构建 `dto.Message` 的能力，但当前架构中应通过 `platform.OutboundMessage` + `buildDTOMessage()` 统一流程。  
`dto.MessageBuilder` 在生产代码路径中**没有被使用**，仅存在于 dto 层。  
**建议删除**，测试/示例代码改用 `platform.OutboundMessage` 构建。

---

## 五、平台抽象设计合理性评估

### 5.1 优点

| 设计点 | 评价 |
|---|---|
| `Adapter` 接口 + `Registry` 多平台管理 | ✅ 简洁合理，生命周期清晰 |
| `platform.Event` 接口 10 个方法 | ✅ 覆盖了大多数用例，方法数适中 |
| `OutboundMessage` + 链式 Builder | ✅ 不可变值语义，渐进增强清晰 |
| `Capabilities` 能力声明 | ✅ 运行时特性检测优于编译期类型断言 |
| `ChatInfo` 携带 `ParentID` 支持频道层级 | ✅ 能表达 Discord guild/channel 层级 |
| `WithChatInfo` / `ChatInfoFromContext` 注入路由 | ✅ 解耦 Sender 与目标会话 |
| `qq.ApplyExtra` 类型安全扩展 | ✅ 避免 magic string，类型安全 |
| `qqEvent` 不持有 `*dto.Payload` 引用（D5） | ✅ 及时释放，降低 GC 压力 |
| Worker Pool 有界事件分发 | ✅ 防止高频事件下 goroutine 泄漏 |

### 5.2 待改进的设计点

#### D1. `Capabilities` 缺少数值型能力

当前 `Capabilities` 全为 `bool`，无法表达：
- `MaxAttachments int`（QQ = 1，Discord = 10）
- `MaxMessageLength int`（各平台不同）
- `MaxButtonsPerRow int`（Discord = 5，QQ 有限制）

这些数值在实现多平台富媒体降级时是必要的。

**建议补充：**
```go
type Capabilities struct {
    // ...existing bool fields...

    // MaxAttachments 单条消息最多附件数（0 = 不支持，-1 = 不限制）
    MaxAttachments int
    // MaxMessageLength 消息正文最大字节数（0 = 不限制）
    MaxMessageLength int
    // MaxButtonsPerRow 每行最多按钮数（0 = 不支持）
    MaxButtonsPerRow int
    // MaxButtonRows 最多行数（0 = 不支持）
    MaxButtonRows int
}
```

#### D2. `Registry.StartAll()` 部分失败语义不清晰

当前行为：若某个平台 adapter 非 context 取消原因退出，返回**第一个**错误。但此时其他平台可能还在正常运行，调用方无法区分"全部失败"与"部分失败"。

**建议**：
- 引入 `StartAllWithObserver(ctx, handler, onError func(platform string, err error))` 变体
- 或返回 `map[string]error` 而非单个错误

#### D3. `platform.Event` 接口无法表达"消息是否为编辑/转发"

部分场景（反垃圾、审计日志）需要区分原始消息与编辑消息，目前无标准接口方法。

**建议可选接口：**
```go
type EditableEvent interface {
    IsEdited() bool
    OriginalTimestamp() time.Time
}
```

#### D4. QQ 被动回复需手动注入 `EventID`，用户体验差

用户每次主动回复都需要：
```go
msg = qq.ApplyExtra(msg, qq.MessageExtra{EventID: ctx.GetPlatformEvent().ID()})
```

这是一个高频操作，应在框架层自动化。

**建议**：在 `qq.Sender.Send()` 内部，若 `chat` 信息存在且 `MessageExtra.EventID` 为空，从 context 读取当前事件 ID 并自动填充。这需要在 `ctx.Reply()` 路径中携带事件 ID，或提供 `qq.ReplyTo(event)` 便捷包装。

#### D5. `platform/qq/dlq` 包定位模糊

既然 D5 已经让 `*dto.Payload` 生命周期极短，以 `*dto.Payload` 为泛型参数的 DLQ 类型实际上已不安全（见 B2）。整个 `platform/qq/dlq` 包的存在价值需要重新评估：

- 若 DLQ 是为了重放失败处理的 `platform.Event`，应该用 `dlq.Queue[platform.Event]`（平台无关）
- `platform/qq/dlq` 包可以仅作为指向 `infra/dlq` 的使用示例，而非独立包

---

## 六、性能优化建议

### P1. `WebhookConn.stop()` 不必要的 goroutine

**文件**：`platform/qq/webhook_conn.go`

```go
done := make(chan struct{})
go func() {
    c.wg.Wait()
    close(done)
}()
select {
case <-done:
case <-ctx.Done():
    ...
}
```

每次 Stop 都额外创建一个 goroutine 来桥接 `wg.Wait()` 和 context。可以直接在 select 中用 channel 封装：

```go
waitCh := make(chan struct{})
go func() { c.wg.Wait(); close(waitCh) }()
```

这已经是该模式，性能影响不大，但可以用 `context.AfterFunc`（Go 1.21+）替代，消除临时 goroutine：

```go
stop := context.AfterFunc(ctx, func() { /* noop, just unblock */ })
defer stop()
c.wg.Wait()
```

若 `wg.Wait()` 在 ctx 超时前完成，`AfterFunc` 的 goroutine 被取消，零开销。

---

### P2. `openapi.Client.Post()` / `Delete()` 未接受 context

**文件**：`platform/qq/openapi/openapi.go`

所有发送 API 都不接受 `context.Context`，无法传播超时/取消信号。高并发或慢响应场景下，发送请求会一直阻塞直到网络超时。

建议 `OpenAPI` 接口所有方法签名加 `ctx context.Context` 参数，并在 httpclient 调用时传递。

---

### P3. `OutboundMessage.Extra` 的 map 分配

`WithExtra` 在每次调用时懒初始化 `map[string]any`，这是正确的。但如果消息被多次复制（链式调用），map 只被浅拷贝（slice copy 只拷贝引用）。目前 `WithExtra` 实现是：
```go
func (m OutboundMessage) WithExtra(key string, value any) OutboundMessage {
    if m.Extra == nil {
        m.Extra = make(map[string]any)
    }
    m.Extra[key] = value  // 直接修改，不拷贝
    return m
}
```

这意味着如果两个 `OutboundMessage` 从同一个原始消息派生，它们会**共享同一个 map**，导致意外修改互相影响。  
**建议**：`WithExtra` 应拷贝 Extra map，与 `WithMentions`/`WithButtons`/`WithAttachments` 的 `slices.Clone` 行为一致。

```go
func (m OutboundMessage) WithExtra(key string, value any) OutboundMessage {
    newExtra := make(map[string]any, len(m.Extra)+1)
    for k, v := range m.Extra {
        newExtra[k] = v
    }
    newExtra[key] = value
    m.Extra = newExtra
    return m
}
```

---

## 七、已实现但值得关注的设计

以下设计是正确的，记录供参考：

- **`qqEvent` 不持有 `*dto.Payload`（D5）**：`NewEvent` 完成后立即释放，设计正确，但与 `dlq/compat.go` 冲突（见 B2）。
- **Worker Pool 有界分发**：避免高频事件下无限 goroutine，设计正确。
- **COW Engine 状态**：无锁读取，适合读多写少场景，设计合理。
- **`ProcessPlatformEvent` + `ProcessPlatformEventBatch`**：清晰的入口，无重复逻辑。
- **`qq.ApplyExtra` 类型安全扩展**：避免 magic string，优于 `map[string]any` 直接操作。

---

## 八、待办清单（优先级排序）

| 优先级 | 类型 | 项目 | 状态 |
|---|---|---|---|
| 🔴 高 | Bug | B1：`Context.Clone()` 补充 `platformCaps` 复制 | ✅ 已修复 |
| 🔴 高 | Bug | B2：重新设计或删除 `platform/qq/dlq/compat.go` | ✅ 已修复（改为 `platform.Event` DLQ） |
| 🔴 高 | Bug | B3：`sendAttachment` 传递 context 到 httpclient | ✅ 已修复 |
| 🟠 中 | 删除 | R1：删除 `dto.Payload.Decode()` + `decodeMessageCreateEvent()` | 待处理 |
| 🟠 中 | 删除 | R2：清理 `dto.C2CMessageCreateEvent` 等结构体 / 简化 testbot | 待处理 |
| 🟠 中 | 删除 | R3：删除空文件 `platform/qq/bot.go` | 待处理 |
| 🟠 中 | 删除 | R4：删除 `dto.OperationCodeName` | 待处理 |
| 🟠 中 | 删除 | R5：删除 `dto.Payload.Clone()` | 待处理 |
| 🟠 中 | 删除 | R6：删除 `dto.MessageBuilder` | 待处理 |
| 🟡 中 | 未实现 | N1：实现 QQ 频道（Guild Channel）消息发送 | 待处理 |
| 🟡 中 | 未实现 | N3：实现 QQ Keyboard/Button 发送 | 待处理 |
| 🟡 中 | 未实现 | N5：完善平台适配器健康检查 | ✅ 已修复（IsRunning + 真实状态检查） |
| 🟡 中 | 未实现 | N6：添加 `OnPlatform(string) Rule` | ✅ 已修复 |
| 🟡 中 | 未实现 | N7：`platform.Event` 接口拆分 + `ReplyToID` | ✅ 已修复（RawEvent/EditableEvent/ReplyEvent 可选接口） |
| 🟡 中 | 性能 | P2：`openapi.Client` 所有方法添加 context 参数 | ✅ 已修复 |
| 🟡 中 | 性能 | P3：修复 `OutboundMessage.WithExtra` map 共享 bug | ✅ 已修复 |
| 🟢 低 | Bug | B4：移除 `WebhookConn` 的 `"/"` 兜底路由 | 待处理 |
| 🟢 低 | Bug | B5：更新 `Registry.StartAll()` 注释 | 待处理 |
| 🟢 低 | 未实现 | N2：实现 QQ 二进制附件上传 | 待处理 |
| 🟢 低 | 未实现 | N4：支持 QQ Markdown 模板参数 | 待处理 |
| 🟢 低 | 设计 | D1：`Capabilities` 添加数值型能力字段 | 待处理 |
| 🟢 低 | 设计 | D2：`Registry.StartAll()` 部分失败语义 | 待处理 |
| 🟢 低 | 设计 | D3：`EditableEvent` 可选接口 | ✅ 已添加（接口定义完成） |
| 🟢 低 | 设计 | D4：QQ 被动回复自动注入 EventID | ✅ 已修复（`ctx.Reply()` 自动注入，`qq/sender.go` 自动读取） |
| 🟢 低 | 设计 | D5：`platform/qq/dlq` 包定位问题 | ✅ 已修复（改用 `platform.Event`） |

