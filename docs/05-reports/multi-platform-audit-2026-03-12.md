# 多平台抽象层审计报告

> 审计日期：2026-03-12  
> 项目状态：未发布（pre-release），可安全删除 Deprecated 符号  
> 范围：`platform/`、`core/engine/`、`core/context/`、`adapter.go`、`webhook_adapter.go`、`bot.go`、`plugin/`、`plugins/`

---

## 零、测试修复进度（2026-03-15 更新）

根据本报告已对代码库执行了部分清理，导致以下测试编译失败。**以下问题的测试已全部修复完成**：

| 问题 | 测试修复内容 | 受影响文件 |
|------|-------------|-----------|
| **#6** ✅ | `WebhookServerAdapter.Start()` 已删除，测试改用 `StartPlatform(ctx, func(platform.Event))`，补充 `platform` 包导入和 `ctx` 变量定义，删除残留旧签名 handler | `remilia_plus_test.go` |
| **#11** ✅ | `engine.NewBlockError` 别名已删除，测试改用 `errutil.NewBlockError` | `core/engine/engine_test.go` |
| **#12** ✅ | `engine.Close()` 已删除，所有测试改用 `engine.Shutdown(stdctx.Background())`，补充 `stdctx "context"` 导入 | `core/engine/matcher_deletion_race_test.go`、`core/engine/optimization_test.go`、`core/engine/batch_test.go`、`plugin/phase2_test.go`、`plugin/phase3_test.go`、`plugin/bugfix_test.go` |
| **#13** ✅ | `Require[T]()` / `Optional[T]()` 已删除，测试改用 `Must[T]()` / `Try[T]()`，测试函数名同步重命名；修复因新旧代码混入导致的 composite literal 语法错误 | `plugin/phase2_test.go` |
| **#3/#4** ✅ | `engine.PlatformAdapter` 接口要求实现 `Platform()` / `StartPlatform()` / `Stop()` / `Sender()`；`tests/` 中的 `mockAdapter` 删除旧 `Start()` 方法，补充 `Platform()`、`StartPlatform()`、`Stop()` 实现，补充 `platform` 包导入，删除孤立多余 `}` | `tests/fixes_validation_test.go` |
| **testbot** ✅ | `SendPlatformEvent` 不再每次调用前清空 `MockSender`，消息在多次 Send 间正确累积，`TestBot_AssertSentCount` 可验证两条消息 | `testbot/testbot.go` |

**尚未修复的问题**（代码清理工作本身未完成，不在本次测试修复范围内）：

| 问题 | 状态 |
|------|------|
| #1：`platform.EventContext` 孤立死代码 | ⏳ 待处理 |
| #2：`platform/bridge.go` 废弃文件 | ⏳ 待处理 |
| #3：`engine.Adapter` 旧接口 | ⏳ 待处理 |
| #4：Bot 双路径架构 | ⏳ 待处理 |
| #5：根包两个 QQ 适配器重复 | ⏳ 待处理 |
| #7：`qq/sender.go` Deprecated chatType | ⏳ 待处理 |
| #8：`core/context/decode.go` Deprecated 方法 | ⏳ 待处理 |
| #9：`core/context/rules.go` Deprecated 规则 | ⏳ 待处理 |
| #10：QQ 便捷方法未标 Deprecated | ⏳ 待处理 |
| #14：`plugins/sendqueue` Deprecated | ⏳ 待处理 |
| #15：`plugins/broadcast` Deprecated | ⏳ 待处理 |
| #16：`ErrNoPlatformSender` 错误信息 | ⏳ 待处理 |
| 缺失 3.2：`OnEventKind` 便捷方法 | ⏳ 待处理 |
| 缺失 3.3：多平台示例 | ⏳ 待处理 |
| 缺失 3.5：Guild Name 字段 | ⏳ 待处理 |

---

## 一、当前实现进度

### 1.1 `platform/` 核心抽象层

| 文件 | 状态 | 说明 |
|------|------|------|
| `platform/event.go` | ✅ 完整 | `Event` 接口、`EventKind` 枚举、`UserInfo`、`ChatInfo` |
| `platform/message.go` | ✅ 完整 | `OutboundMessage`、工厂函数、链式方法 |
| `platform/adapter.go` | ✅ 完整 | `PlatformAdapter`、`Sender`、`NoopSender`、`Registry` |
| `platform/context.go` | ⚠️ 孤立 | `EventContext` 接口已定义但从未被引擎使用（见问题 #1） |
| `platform/bridge.go` | ❌ 废弃 | `LegacyAdapter`、`LegacyEventHandler` 全部标记 Deprecated，可删除 |

### 1.2 各平台适配器

| 平台 | 状态 | 说明 |
|------|------|------|
| `platform/qq/` | ✅ 完整 | 事件解析（C2C/GroupAt/Guild/Notice/System）、Sender、Adapter 均已实现 |
| `platform/discord/` | 🚧 骨架 | 仅实现接口签名，`StartPlatform`/`Send` 返回 `not yet implemented` |
| `platform/telegram/` | 🚧 骨架 | 同上 |
| `platform/wechat/` | 🚧 骨架 | 同上 |

### 1.3 引擎集成层

| 组件 | 状态 | 说明 |
|------|------|------|
| `core/engine/process_platform.go` | ✅ 完整 | `ProcessPlatformEvent` 实现，与旧 `ProcessEvent` 复用同一路由核心 |
| `core/context/platform_event.go` | ✅ 完整 | `AcquireContextFromEvent`、`Reply`、`GetEventKind`、`GetEventPlatform` |
| `core/context/rules.go` — `OnEventKind` | ✅ 完整 | 平台无关规则，覆盖所有平台 |
| `bot.go` — `handlePlatformEvent` | ✅ 完整 | 通过 `platformRegistry` 驱动新路径 |
| `bot.go` — `UsePlatformRegistry` | ✅ 完整 | 注入注册表，生命周期绑定 |
| `bot_builder.go` — `WithPlatformRegistry` | ✅ 完整 | Builder 模式支持 |

---

## 二、需要修复的问题

### 🔴 问题 #1：`platform.EventContext` 是孤立的死代码

**文件**：`platform/context.go`

`platform` 包定义了完整的 `EventContext` 接口和 `baseEventContext` 实现，但引擎从未使用它——引擎使用的是 `core/context.Context`（通过 `AcquireContextFromEvent` 创建）。两套 Context 抽象并存造成概念混乱：

- 实际使用的是 `*core/context.Context`（带对象池、中间件链、规则引擎集成）
- `platform.EventContext` 只有基础的 `Get/Set/Reply/Send`，没有规则、中间件等框架能力

**建议**：删除 `platform/context.go`（`EventContext`、`baseEventContext`、`NewEventContext`）。若未来需要给外部插件暴露轻量上下文，届时再设计。

---

### 🔴 问题 #2：`platform/bridge.go` 应立即删除

**文件**：`platform/bridge.go`

```go
// Deprecated: P2 引擎迁移完成后本文件将被删除。
type LegacyEventHandler func(*dto.Payload)
type LegacyAdapter interface { ... }
```

- `LegacyAdapter` 与 `engine.Adapter`（同样 Deprecated）完全重复
- 该文件使 `platform` 包反向依赖 `openapi/dto`，破坏了抽象层的隔离目标
- 没有任何代码引用此接口

**建议**：直接删除 `platform/bridge.go`。

---

### 🔴 问题 #3：`engine.Adapter`（旧接口）仍在核心类型中，强迫 `core/engine` 导入 `openapi/dto`

**文件**：`core/engine/types.go`

```go
// Deprecated: Use PlatformAdapter instead.
type Adapter interface {
    Start(ctx stdctx.Context, handleFunc func(*dto.Payload)) error
    Stop(ctx stdctx.Context) error
}
```

这是 `core/engine` 包唯一一处对 `openapi/dto` 的直接依赖，已可删除。

**建议**：
1. 删除 `core/engine/types.go` 中的 `Adapter` 接口定义
2. 删除 `adapter.go` 根包中 `type Adapter = engine.Adapter` 的别名
3. `Bot.adapter` 字段改为 `engine.PlatformAdapter`（或直接删除该字段，统一走 `platformRegistry`）

---

### 🔴 问题 #4：`Bot` 存在双路径架构，逻辑分裂

**文件**：`bot.go`

`Bot` 同时持有：
```go
adapter          Adapter           // 旧路径：func(*dto.Payload)
platformRegistry *platform.Registry // 新路径：func(platform.Event)
```

旧路径 `b.adapter.Start(ctx, b.handleEvent)` 仍在生命周期中硬编码，而 `webhookAdapter.Start()` 内部其实把 `*dto.Payload` 重新包装成 `platform.Event` 再传给引擎——存在不必要的双重转换。

**建议**：
- 将 `Bot.adapter` 类型改为 `engine.PlatformAdapter`（新接口），生命周期统一调用 `StartPlatform`
- 删除 `Bot.handleEvent(payload *dto.Payload)` 方法
- 删除 `adapter Adapter` 字段，所有适配器统一通过 `platformRegistry` 或直接的 `PlatformAdapter` 管理

---

### 🟡 问题 #5：`adapter.go` 与 `webhook_adapter.go` 在根包重复

**文件**：`adapter.go`、`webhook_adapter.go`

两个文件都实现了 QQ Webhook 适配器，且都有 `safeHandlePlatform` 函数（在 `adapter.go` 定义，供 `webhook_adapter.go` 调用）。

- `webhookAdapter`（`adapter.go`）：简单 Webhook 转发，无 HTTP 服务器
- `WebhookServerAdapter`（`webhook_adapter.go`）：内置 HTTP 服务器，多 Worker

实际上 `NewWebhookAdapter` 已基本被 `NewWebhookServerAdapter` 取代。同时，`platform/qq/Adapter` 也实现了同样的功能（读取 `Webhook.EventStream()`），三者之间存在重复。

**建议**：
1. 删除 `adapter.go` 中的 `webhookAdapter`（`NewWebhookAdapter` / `NewWebhookAdapterWithAPI`）
2. 保留 `WebhookServerAdapter`（`webhook_adapter.go`），作为 QQ 官方推荐入口
3. `platform/qq/Adapter` 保留，作为使用已有 `Webhook` 连接时的轻量包装

---

### 🟡 问题 #6：`WebhookServerAdapter.Start()` 是 Deprecated 的旧签名

**文件**：`webhook_adapter.go`（第 129 行）

```go
// Deprecated: 请使用 StartPlatform 替代
func (a *WebhookServerAdapter) Start(ctx context.Context, handler func(*dto.Payload)) error {
```

由于项目未发布，可直接删除。

---

### 🟡 问题 #7：`qq/sender.go` 保留了 Deprecated 的 `chatType` 降级路径

**文件**：`platform/qq/sender.go`（第 100-111 行）

`chatType`、`chatTypeContextKey`、`chatTypePrivate`、`chatTypeGroup` 全部标记 Deprecated，且 `Send()` 内部仍有降级到这些类型的代码。新路径（`platform.ChatInfoFromContext`）已完全覆盖旧路径的功能。

**建议**：删除 `chatType` 相关代码，简化 `Send()` 的路由逻辑。

---

### 🟡 问题 #8：`core/context/decode.go` — 多个 Deprecated 的 QQ 专属方法

以下方法全部标记 Deprecated，可直接删除：

| 方法 | 替代 |
|------|------|
| `GetAuthor() *dto.Author` | `GetSenderInfo() platform.UserInfo` |
| `SendGroupMessage(groupID, msg)` | `ctx.Reply(platform.TextMessage(...))` |
| `SendSingleMessage(openID, msg)` | `ctx.Reply(platform.TextMessage(...))` |
| `ReplyGroup(msg)` | `ctx.Reply(platform.TextMessage(...))` |
| `ReplyPrivate(msg)` | `ctx.Reply(platform.TextMessage(...))` |
| `GetEvent() *dto.Payload` | `ctx.GetPlatformEvent()` |

---

### 🟡 问题 #9：`core/context/rules.go` — 多个 Deprecated 的 QQ 专属规则

以下规则全部标记 Deprecated，可直接删除：

| 规则函数 | 替代 |
|------|------|
| `OnC2CMessage()` | `OnEventKind(platform.EventKindPrivateMessage)` |
| `OnGroupAtMessage()` | `OnEventKind(platform.EventKindGroupMessage)` |
| `OnGroupAddRobot()` | `OnEventKind(platform.EventKindNotice)` |
| `OnGroupDelRobot()` | `OnEventKind(platform.EventKindNotice)` |
| `OnAtBot(botOpenID)` | 自定义 Rule 或 `OnKeyword` |

---

### 🟡 问题 #10：`core/engine/engine_matcher_ops.go` — QQ 专属便捷方法未标记 Deprecated

以下方法直接引用 `dto.C2CMessageCreate` 等 QQ 常量，但**未标记 Deprecated**，容易误导新用户：

```go
func (e *Engine) OnC2C(rules ...context.Rule) *Matcher  // 应 Deprecated 或删除
func (e *Engine) OnGroupAt(rules ...context.Rule) *Matcher
func (e *Engine) OnGroupAdd(rules ...context.Rule) *Matcher
func (e *Engine) OnGroupDel(rules ...context.Rule) *Matcher
```

同理，`bot.go` 中也有 `b.OnC2C()`、`b.OnGroupAt()`、`b.On(eventType dto.EventType, ...)` 未标记 Deprecated。

**建议**：新增平台无关便捷方法 `OnEventKind(kind platform.EventKind, rules...)` 并将 QQ 专属方法标记 Deprecated（或直接删除）。

---

### 🟡 问题 #11：`core/engine/errors.go` — 重复导出 Deprecated 别名

```go
type BlockError = remiliaerrors.BlockError  // Deprecated
var NewBlockError = remiliaerrors.NewBlockError  // Deprecated
var IsBlockError = remiliaerrors.IsBlockError  // Deprecated
```

由于项目未发布，可直接删除这三个别名，强制用户使用 `errutil` 包。

---

### 🟡 问题 #12：`core/engine/engine.go` — `Close()` 方法

```go
// Deprecated-ish：优先使用 Shutdown(ctx)
func (e *Engine) Close() { _ = e.Shutdown(stdctx.Background()) }
```

由于项目未发布，直接删除，只保留 `Shutdown(ctx)`。

---

### 🟡 问题 #13：`plugin/` — 多个 Deprecated 符号

| 位置 | 符号 | 替代 |
|------|------|------|
| `plugin/manager.go` | `SetViper(_ any)` | `SetConfigProvider(NewViperConfigProvider(v))` |
| `plugin/manager_goroutines.go` | `ListPluginGoroutines()` | `ListAllGoroutines()` |
| `plugin/context.go` | `Require[T]()` | `Must[T]()` |
| `plugin/context.go` | `Optional[T]()` | `Try[T]()` |

项目未发布，可直接删除。

---

### 🟡 问题 #14：`plugins/sendqueue` — Deprecated 旧 QQ 路径

以下内容可删除：

- `sendJob` 中的 `isGroup bool`、`msg *dto.Message`、`api openapi.OpenAPI` 字段
- `EnqueueGroup()` 方法
- `EnqueueC2C()` 方法
- `SetDefaultAPI()` 方法
- `Plugin.defaultAPI` 字段

---

### 🟡 问题 #15：`plugins/broadcast` — 多个 Deprecated 方法

可删除：
- `SetAPI(api openapi.OpenAPI)`（用 `SetSender` 代替）
- `ToGroups()` / `ToUsers()` / `ToBoth()`
- `BroadcastToGroups()` / `BroadcastToC2C()`
- `Plugin.api openapi.OpenAPI` 字段

---

### 🟢 问题 #16：`ErrNoPlatformSender` 错误信息具有误导性

**文件**：`core/context/state.go`

```go
var ErrNoPlatformSender = errors.New("no platform sender: use ReplyGroup/ReplyPrivate for legacy QQ path")
```

错误信息建议使用 Deprecated 方法，应更新为：
```go
var ErrNoPlatformSender = errors.New("no platform sender available: context was not created from a platform event")
```

---

## 三、缺失的实现

### 3.1 `discord/telegram/wechat` 骨架内部的 `Event` 结构体从未使用

`platform/discord/adapter.go` 中定义了 `discordEvent struct{}`（同理 telegram、wechat），但：
1. 没有任何地方实例化它
2. `StartPlatform` 永远返回错误，不会产生事件

这些文件目前只是让编译通过的占位符，实际功能为零。可以考虑用构建标签（build tag）或接口兜底，避免暴露永远报错的 `NewAdapter()`。

### 3.2 缺少 `engine.OnEventKind()` 平台无关便捷方法

目前注册平台无关 Matcher 的方式：
```go
engine.On(string(platform.EventKindPrivateMessage), rules...)
```

需要手动做类型转换，不符合框架的人体工程学设计。应新增：
```go
func (e *Engine) OnEventKind(kind platform.EventKind, rules ...context.Rule) *Matcher {
    return e.On(string(kind), rules...)
}
```

以及 `bot.go` 对应的便捷包装。

### 3.3 缺少多平台使用示例

`examples/` 目录下没有任何使用 `platform.Registry` 多平台注册功能的示例，新用户无法参考。建议新增 `examples/multi-platform/` 示例。

### 3.4 `config` 包 — Kafka DLQ 目标未实现

**文件**：`config/validate.go`（第 191 行）

```go
return fmt.Errorf("dead_letter.target 'kafka' is not yet implemented; ...")
```

该功能在验证层硬编码报错，需补充实现或在文档中明确标注为 roadmap 项。

### 3.5 `platform/qq/event.go` — `populateGuildMessage` 中 `ChatInfo.Name` 字段赋值错误

**文件**：`platform/qq/event.go`（第 130-135 行）

```go
e.chat = platform.ChatInfo{
    ID:      chatID,
    Name:    gjson.GetBytes(d, "channel_id").String(), // ← Name 和 ID 赋值相同字段
    IsGroup: true,
}
```

`Name` 字段赋的是 `channel_id` 而非频道名称，应改为 `channel_id` 对应的名称字段（如果 QQ API 提供）或保留空字符串。

---

## 四、项目结构问题

### 4.1 QQ 适配器三重实现，职责重叠

同一功能（读取 QQ 事件并转换为 `platform.Event`）目前有三处实现：

| 实现 | 位置 | 特点 |
|------|------|------|
| `webhookAdapter` | `adapter.go` | 简单包装，无 HTTP 服务器 |
| `WebhookServerAdapter` | `webhook_adapter.go` | 内置 HTTP 服务器、多 Worker |
| `qq.Adapter` | `platform/qq/adapter.go` | 读取 `Webhook.EventStream()`，不含 HTTP |

三者逻辑高度相似，均包含事件循环 + `safeInvoke`/`safeHandlePlatform` panic 恢复。建议：
- 删除 `adapter.go` 的 `webhookAdapter`
- `WebhookServerAdapter` 内部委托给 `platform/qq/Adapter` 处理事件循环

### 4.2 `platform` 包反向依赖 `openapi/dto`（通过 bridge.go）

删除 `bridge.go` 后，`platform` 包将完全不依赖 `openapi/dto`，实现真正的平台无关。

### 4.3 `core/engine` 包仍导入 `openapi/dto`（通过 `types.go` 的旧 `Adapter`）

删除 `engine.Adapter` 后，`core/engine` 对 `openapi/dto` 的依赖可以完全消除。

### 4.4 `BotBuilder.WithAdapter(adapter Adapter)` 接受 Deprecated 类型

**文件**：`bot_builder.go`

```go
func (b *BotBuilder) WithAdapter(adapter Adapter) *BotBuilder { ... }
```

`Adapter` 是 `engine.Adapter` 的别名（Deprecated）。应改为接受 `engine.PlatformAdapter`。

### 4.5 `pprof.go` 有一处 Deprecated 方法

**文件**：`pprof.go`（第 332 行）— 低优先级，按需清理。

---

## 五、优先级汇总

| 优先级 | 问题 | 操作 |
|--------|------|------|
| 🔴 高 | 问题 #1：`platform.EventContext` 孤立死代码 | 删除 `platform/context.go` |
| 🔴 高 | 问题 #2：`platform/bridge.go` 全部 Deprecated | 删除整个文件 |
| 🔴 高 | 问题 #3：`engine.Adapter` 旧接口迫使 engine 依赖 dto | 删除旧接口，消除依赖 |
| 🔴 高 | 问题 #4：Bot 双路径架构 | 统一为 `PlatformAdapter` 路径 |
| 🟡 中 | 问题 #5：根包两个 QQ 适配器重复 | 删除 `webhookAdapter`（`adapter.go`）|
| 🟡 中 | 问题 #6-7：Deprecated Start()、chatType | 直接删除 |
| 🟡 中 | 问题 #8-9：Context/Rules Deprecated 方法 | 批量删除 |
| 🟡 中 | 问题 #10：QQ 便捷方法未标注 Deprecated | 删除或替换为 `OnEventKind` |
| 🟡 中 | 问题 #11-15：engine/plugin/plugins Deprecated | 批量删除 |
| 🟢 低 | 问题 #16：错误信息文字 | 更新文字 |
| 🟢 低 | 缺失 3.2：`OnEventKind` 便捷方法 | 新增 |
| 🟢 低 | 缺失 3.3：多平台示例 | 新增 `examples/multi-platform/` |
| 🟢 低 | 缺失 3.5：Guild 事件 Name 字段 | 修复赋值 |

---

## 六、推荐清理顺序

```
1. 删除 platform/bridge.go
2. 删除 platform/context.go（EventContext 孤立接口）
3. 删除 engine.Adapter 旧接口 → engine 包不再依赖 openapi/dto
4. 删除 adapter.go 的 webhookAdapter（保留 WebhookServerAdapter）
5. Bot.adapter 字段改为 engine.PlatformAdapter，删除 handleEvent()
6. 删除各包 Deprecated 方法（context、rules、engine、plugin、plugins）
7. 新增 engine.OnEventKind()，bot.OnEventKind() 便捷方法
8. 修复 populateGuildMessage 的 Name 字段
9. 更新 ErrNoPlatformSender 错误信息
10. 新增 examples/multi-platform/ 示例
```

