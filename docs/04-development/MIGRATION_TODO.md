# 多平台抽象迁移待办清单

> 分支：`feature/multi-platform-abstraction`  
> 更新日期：2026-03-11  
> 状态：**✅ 全部完成（50 个包测试全通过）**

---

## 已完成项（P0 / P1 — 本次迁移前）

| 模块 | 内容 |
|------|------|
| `platform/` | 核心接口定义：`Event`、`Sender`、`PlatformAdapter`、`Registry`、`OutboundMessage`、`EventContext` |
| `platform/qq/` | QQ 完整适配器 |
| `platform/discord/` `telegram/` `wechat/` | 骨架适配器 |
| `core/context/platform_event.go` | `AcquireContextFromEvent`、`Reply(OutboundMessage)` 等 |
| `core/engine/process_platform.go` | `ProcessPlatformEvent` |
| `adapter.go` / `webhook_adapter.go` | `StartPlatform` 新路径 |
| `bot.go` | `handlePlatformEvent` |
| `plugins/broadcast/` `sendqueue/` | 平台无关路径 |

---

## ✅ 本次迁移完成项

### P2 — 架构核心

| 编号 | 文件 | 变更内容 |
|------|------|---------|
| **2-A** | `openapi/dto/event.go` | `type EventType = string`（类型别名，向后兼容） |
| **2-A** | `core/engine/types.go` | 新增 `type EventType = string`；`MatcherLifecycle`、`PluginCoordinator` 接口改用 `EventType` |
| **2-A** | `core/engine/matcher.go` | `Matcher.EventType` 字段类型改为 `EventType`，移除 `dto` 导入 |
| **2-A** | `core/engine/state.go` | 所有 map key 从 `dto.EventType` 改为 `EventType`，移除 `dto` 导入 |
| **2-A** | `core/engine/temp_manager.go` | `matcherIndex` key 类型改为 `EventType` |
| **2-A** | `core/engine/engine_command.go` | `CommandInfo.EventType`、`OnCommand`、`RegisterCommandDef` 签名更新 |
| **2-A** | `core/engine/engine_matcher_ops.go` | `On`、`OnTemp`、`InvalidateSortedCache` 签名更新 |
| **2-B** | `plugin/registry.go` | `RegistryWriter` 接口参数从 `dto.EventType` 改为 `string`，移除 `dto` 导入 |
| **2-C** | `core/context/rules.go` | `OnEventType` 接受 `string`；新增 `OnEventKind(platform.EventKind)`；`InGroup` 双路径兼容 |
| **2-D** | `core/context/convenience.go` | `OnGroupWhitelist/Blacklist` 使用 `groupChatID()` 平台无关提取，QQ 降级兼容 |

### P3 — 插件 / 内部模块

| 编号 | 文件 | 变更内容 |
|------|------|---------|
| **3-A** | `core/context/decode.go` | 新增 `GetSenderInfo() platform.UserInfo`；`GetAuthor()` 标记 Deprecated |
| **3-B** | `infra/dlq/compat.go` | 新增 `PlatformEventQueue/Item/Consumer/Config` 及 `NewPlatformEventQueue` |
| **3-B** | `infra/dlq/types.go` | `DeadLetterItem` 标记 Deprecated；新增 `MarshalPlatformEventItem` |
| **3-C** | `helper/helper.go` | `ParseEvent` 标记 Deprecated；新增 `ExtractContent/SenderID/ChatID` |
| **3-C** | `helper/parse.go` | 所有 `*dto.Payload` 函数标记 Deprecated |
| **3-D** | `plugins/cooldown/cooldown.go` | 回复改为 `ctx.Reply(platform.TextMessage(...))` |
| **3-E** | `plugins/conversation/conversation.go` | `sendPrompt` 改为 `ctx.Reply` |
| **3-F** | `plugins/ratelimitui/ratelimitui.go` | 注册用 `""`，回复用 `ctx.Reply` |
| **3-G** | `plugins/core/admin/admin.go` | 所有命令注册改为 `""`，`reply` 改为 `ctx.Reply` |
| **3-H** | `plugins/core/help/help.go` | 注册改为 `""`，`sendMessage` 改为 `ctx.Reply` |
| **3-I** | `plugins/dev/debug/debug.go` | 注册改为 `""`，`reply` 改为 `ctx.Reply`，统计 map key 为 `string` |
| **3-J** | `platform/bridge.go` | `LegacyEventHandler`、`LegacyAdapter` 标记 Deprecated |

### P4 — 测试工具 / 文档

| 编号 | 文件 | 变更内容 |
|------|------|---------|
| **4-A** | `testutil/testutil.go` | 新增 `PlatformResponse`、`mockSender`、`sender` 字段、`SendPlatformEvent` 方法 |
| **4-B** | `testbot/testbot.go` | 新增 `MockSender`、`sender` 字段、`SendPlatformEvent`、`AssertPlatformReplied` |
| **4-D** | `doc.go` | 重写包文档，平台无关 API 为主，QQ 路径为兼容路径 |

---

## 仍需完成（未来工作）

| 编号 | 说明 |
|------|------|
| **4-C** | `examples/` 全部示例更新为平台无关 API（属于文档性工作，不影响功能） |
| **4-E** | `platform/discord`、`telegram`、`wechat` 完成真实实现（需对应平台 SDK） |
| **future** | 待所有 QQ 用户迁移后，删除 `platform/bridge.go` 和 `engine.Adapter` 旧接口 |
| **future** | `testutil/testbot` 旧 QQ 路径（`mockAPI`/`SendC2C`）可在下一主版本中删除 |

---

## 架构变更总结

### `dto.EventType` 的变化

```go
// 旧：命名类型（与 string 不可直接互赋值）
type EventType string

// 新：类型别名（与 string 完全等价，零迁移成本）
type EventType = string
```

### 插件命令注册 API 变化

```go
// 旧（仍然有效，QQ 专属）：
ctx.Reg.RegisterCommand(dto.C2CMessageCreate, "/hello").Handle(h)

// 新（推荐，所有平台）：
ctx.Reg.RegisterCommand("", "/hello").Handle(h)
// 空字符串 "" 表示通配所有事件类型
```

### Handler 回复方式变化

```go
// 旧（仍然有效，QQ 专属）：
ctx.ReplyPrivate(&dto.Message{Type: dto.TextMessage, Content: "hello"})

// 新（推荐，所有平台）：
ctx.Reply(platform.TextMessage("hello"))
```


> 分支：`feature/multi-platform-abstraction`  
> 更新日期：2026-03-11  
> 目标：将框架从 QQ 官方 SDK（`openapi/dto`）中完全解耦，使核心引擎平台无关。

---

## 已完成项（P0 / P1）

| 模块 | 内容 |
|------|------|
| `platform/` | 核心接口定义：`Event`、`Sender`、`PlatformAdapter`、`Registry`、`OutboundMessage`、`EventContext` |
| `platform/qq/` | QQ 完整适配器：`QQEvent`（包装 `*dto.Payload`）、`QQSender`、`Adapter` |
| `platform/discord/` `telegram/` `wechat/` | 骨架适配器（占位，待实现） |
| `core/context/platform_event.go` | `AcquireContextFromEvent` / `ReleaseContextFromEvent`、`GetPlatformEvent`、`GetEventKind`、`GetEventPlatform`、`Reply(OutboundMessage)` |
| `core/engine/process_platform.go` | `ProcessPlatformEvent`、共享 `processEventContext` |
| `core/engine/types.go` | `PlatformAdapter` 接口加入，`Adapter`（旧）标记 Deprecated |
| `adapter.go` / `webhook_adapter.go` | 添加 `StartPlatform`，QQ 兼容路径保留 |
| `bot.go` | `handlePlatformEvent` 直接调用 `ProcessPlatformEvent` |
| `plugins/broadcast/` | `SetSender` / `Broadcast` 平台无关路径，QQ 旧路径保留 |
| `plugins/sendqueue/` | `SetDefaultSender` / `Enqueue` 平台无关路径，QQ 旧路径保留 |

---

## 待完成项

优先级说明：  
- **P2** — 架构核心，阻塞后续所有平台接入  
- **P3** — 内部模块/插件迁移，可渐进推进  
- **P4** — 文档、示例、测试工具，最后收尾  

---

## P2 — 架构核心

### 2-A 定义平台无关的 `engine.EventType`（高优先级）

**文件：** `core/engine/` 全包、`plugin/registry.go`

**问题：**  
`Matcher.EventType`、`engineState.matcherIndex / sortedCache / commandIndex`、`tempMatcherManager`、`On()` / `OnCommand()` / `InvalidateSortedCache()` 等所有路由 key 均为 `dto.EventType`，强制 engine 包导入 `openapi/dto`。

**方案：**  
在 `core/engine/types.go` 中定义：
```go
// EventType 平台无关的事件类型标识（字符串别名）
type EventType = string
```
由于 `dto.EventType` 本身是 `type EventType string`，替换后 API 层面完全兼容，无需修改调用方。  
同步修改 `engineState`、`Matcher`、`tempMatcherManager`、`PluginCoordinator` 接口中所有 `dto.EventType` 为 `engine.EventType`（即 `string`）。

**涉及文件：**
- `core/engine/types.go`
- `core/engine/matcher.go`（`Matcher.EventType` 字段）
- `core/engine/state.go`（`matcherIndex`、`commandIndex`、`sortedCache` map key）
- `core/engine/temp_manager.go`
- `core/engine/engine_command.go`（`OnCommand`、`RegisterCommandDef` 签名）
- `core/engine/engine_matcher_ops.go`（`On`、`OnTemp`、`InvalidateSortedCache` 签名）

---

### 2-B 迁移 `plugin/registry.go` 中 `RegistryWriter` 接口

**文件：** `plugin/registry.go`

**问题：**  
`RegistryWriter.RegisterCommand(eventType dto.EventType, ...)` 强制所有插件导入 `openapi/dto`。

**方案：**  
完成 2-A 后，将签名改为 `RegisterCommand(eventType string, ...)` 或使用 `engine.EventType` 类型别名：
```go
type RegistryWriter interface {
    RegisterCommand(eventType string, pattern string, extraRules ...context.Rule) *engine.Matcher
    RegisterMatcher(eventType string, rules ...context.Rule) *engine.Matcher
}
```
同步修改 `liveRegistryWriter` 和 `noopRegistryWriter` 实现。

**涉及文件：**
- `plugin/registry.go`

---

### 2-C 迁移 `core/context/rules.go` 中 QQ 专属规则

**文件：** `core/context/rules.go`

**问题：**  
- `OnEventType(eventType dto.EventType)` — 依赖 `dto.EventType`  
- `OnC2CMessage()` / `OnGroupAtMessage()` / `OnGroupAddRobot()` / `OnGroupDelRobot()` — 硬编码 QQ 事件类型  
- `InGroup(groupIDs ...string)` 第 502 行 — 内部 `DecodeEvent(&dto.GroupAtMessageCreateEvent{})` 解码 QQ 专属结构体

**方案：**  
- `OnEventType` 签名改为接收 `string`，或添加 `OnEventKind(kind platform.EventKind) Rule` 平台无关版本  
- `OnC2CMessage()` 等规则改为调用 `OnEventKind(platform.EventKindPrivateMessage)` 等  
- `InGroup` 改为优先走 `ctx.GetPlatformEvent().Chat().ID`，降级时走 `DecodeEvent`（QQ 兼容）

**涉及文件：**
- `core/context/rules.go`

---

### 2-D 迁移 `core/context/convenience.go` 中 QQ 专属 decode

**文件：** `core/context/convenience.go`

**问题：**  
`OnGroupWhitelist` / `OnGroupBlacklist` 第 77、106 行内部 `DecodeEvent(&dto.GroupAtMessageCreateEvent{})` 解码 QQ 专属结构体，其他平台无法触发。

**方案：**  
改为使用 `ctx.GetPlatformEvent().Chat()` 提取 group ID（双路径兼容）：
```go
func groupID(ctx *Context) string {
    if e := ctx.GetPlatformEvent(); e != nil {
        return e.Chat().ID
    }
    var ev dto.GroupAtMessageCreateEvent
    if err := ctx.DecodeEvent(&ev); err == nil {
        return ev.GroupOpenID
    }
    return ""
}
```

**涉及文件：**
- `core/context/convenience.go`

---

## P3 — 内部模块 / 插件

### 3-A `core/context/decode.go` — 去除 `GetAuthor` 对 `*dto.Author` 的依赖

**文件：** `core/context/decode.go`

**问题：**  
`GetAuthor()` 返回 `*dto.Author`，为 QQ 专属用户信息结构体。  
`decodeCache` 中热路径缓存的 `c2c` / `groupAt` 字段也是 QQ 专属。

**方案：**  
- 新增 `GetSenderInfo() platform.UserInfo` 方法（双路径：新路径从 `platform.Event.Sender()` 读，旧路径从 `GetAuthor()` 映射）  
- `GetUserID()` 已存在，优先确认其双路径兼容  
- `GetAuthor()` 标记 `Deprecated`，保留向后兼容  
- `decodeCache` 的热路径缓存暂时保留，因为 `DecodeEvent` 仍被 QQ 路径使用

**涉及文件：**
- `core/context/decode.go`
- `core/context/context.go`（`author` / `authorOnce` 字段可降级为 QQ-only 路径）

---

### 3-B `infra/dlq` — `DeadLetterItem` 去除 `*dto.Payload`

**文件：** `infra/dlq/types.go`、`infra/dlq/compat.go`

**问题：**  
`DeadLetterItem.Event` 类型为 `*dto.Payload`，DLQ 强绑定 QQ。  
`compat.go` 提供了泛型 `Item[T]` 和 `PayloadQueue` 别名，但 `DeadLetterItem` 遗留类型未清理。

**方案：**  
- 新增 `PlatformEventItem`（`Item[platform.Event]`）或使用泛型 `Item[any]`  
- `DeadLetterItem` 标记 `Deprecated`，重定向文档  
- `MarshalDeadLetterItem` 改为接收 `Item[platform.Event]` 或用 `platform.Event` 接口提取 ID/Type  
- `NewPayloadQueue` 保留兼容

**涉及文件：**
- `infra/dlq/types.go`
- `infra/dlq/compat.go`

---

### 3-C `helper/` — `ParseEvent` 系列函数迁移到 `platform/qq/helper`

**文件：** `helper/helper.go`、`helper/parse.go`

**问题：**  
`ParseEvent[T]`、`MustParseEvent`、`ParseEventWithDefault`、`TryParseEvent`、`ParseEventSlice` 等均接受 `*dto.Payload`，绑定 QQ 平台。

**方案（两选其一）：**
1. 将这些函数迁移到 `platform/qq/helper/` 子包（平台专属）  
2. 或在 `helper/` 保留 + 标记 `Deprecated`，新增通用 `helper.ExtractContent(e platform.Event) string` 等  

**涉及文件：**
- `helper/helper.go`（`ParseEvent`）
- `helper/parse.go`（全部函数）

---

### 3-D `plugins/cooldown` — 回复逻辑迁移

**文件：** `plugins/cooldown/cooldown.go` 第 171-174 行

**问题：**  
Middleware 内部按 `dto.C2CMessageCreate` 判断事件类型，使用 `ctx.ReplyPrivate(&dto.Message{...})` / `ctx.ReplyGroup(...)` 发送回复。

**方案：**  
改为使用 `ctx.Reply(platform.TextMessage(msg))`，自动适配当前平台：
```go
_ = ctx.Reply(platform.TextMessage(msg))
```

**涉及文件：**
- `plugins/cooldown/cooldown.go`

---

### 3-E `plugins/conversation` — 提示消息发送迁移

**文件：** `plugins/conversation/conversation.go` 第 374-376 行

**问题：**  
`sendPrompt` 内部按 `dto.GroupAtMessageCreate` 判断类型，使用 `dto.Message{...}` 构建并通过 `ctx.ReplyGroup` / `ctx.ReplyPrivate` 发送。

**方案：**  
同 3-D，改为 `ctx.Reply(platform.TextMessage(prompt))`。

**涉及文件：**
- `plugins/conversation/conversation.go`

---

### 3-F `plugins/ratelimitui` — 命令注册和回复迁移

**文件：** `plugins/ratelimitui/ratelimitui.go`

**问题：**  
- 第 157 行：`ctx.Reg.RegisterCommand(dto.C2CMessageCreate, "/rl")` — QQ 专属注册  
- 第 364 行：`&dto.Message{Type: dto.TextMessage, Content: content}` — QQ 专属消息构建  
  
**方案（依赖 2-A/2-B 完成）：**  
- 注册改为：`ctx.Reg.RegisterCommand("", "/rl")` 或使用 `platform.EventKindPrivateMessage`（待 2-A 确定路由 key 策略）  
- 回复改为：`ctx.Reply(platform.TextMessage(content))`

**涉及文件：**
- `plugins/ratelimitui/ratelimitui.go`

---

### 3-G `plugins/core/admin` — 命令注册和回复迁移

**文件：** `plugins/core/admin/admin.go`

**问题：**  
- 多处 `ctx.Reg.RegisterCommand(dto.C2CMessageCreate, "/plugin")` 等 QQ 专属注册  
- 第 438 行：`ctx.ReplyPrivate(&dto.Message{Type: dto.TextMessage, Content: content})` — QQ 专属回复

**方案（依赖 2-A/2-B）：**  
- 注册改为平台无关 event type  
- 回复改为 `ctx.Reply(platform.TextMessage(content))`  
- 如需同时注册到 C2C 和 GroupAt，可注册空字符串 `""` event type（通配）或提供平台判断

**涉及文件：**
- `plugins/core/admin/admin.go`

---

### 3-H `plugins/core/help` — 命令注册和回复迁移

**文件：** `plugins/core/help/help.go`

**问题：**  
- 第 65-66 行、139-143 行：分别注册到 `dto.GroupAtMessageCreate` 和 `dto.C2CMessageCreate`  
- 第 708-717 行：`&dto.Message{...}` + 按事件类型分支回复

**方案（依赖 2-A/2-B）：**  
- 注册统一为通配或平台无关类型  
- 回复改为 `ctx.Reply(platform.TextMessage(msg))`

**涉及文件：**
- `plugins/core/help/help.go`

---

### 3-I `plugins/dev/debug` — 命令注册、回复和 CommandInfo 统计迁移

**文件：** `plugins/dev/debug/debug.go`

**问题：**  
- 第 118-119 行：注册到 QQ 专属事件类型  
- 第 200-209 行：`dto.Message{...}` 回复，按事件类型分支  
- 第 472 行：`map[dto.EventType][]engine.CommandInfo` 按 QQ 事件类型统计命令  
- 第 615 行：`map[dto.EventType]int` 统计

**方案（依赖 2-A/2-B）：**  
- 注册、回复同上  
- 统计 map key 改为 `string`（完成 2-A 后 `dto.EventType` 本身就变为 `string` 别名，无需改动）

**涉及文件：**
- `plugins/dev/debug/debug.go`

---

### 3-J `platform/bridge.go` — 清理过渡桥接代码

**文件：** `platform/bridge.go`

**问题：**  
`LegacyEventHandler` / `LegacyAdapter` 仍导入 `openapi/dto`，属于临时过渡代码。  
当 engine 完成 2-A 迁移后，`bridge.go` 可整体删除或不再导出 `LegacyAdapter`。

**方案：**  
- 待 P2 全部完成后，评估是否仍需要此文件  
- 若需保留，可改为 `LegacyAdapter` 接受 `func(platform.Event)` 而非 `func(*dto.Payload)`

**涉及文件：**
- `platform/bridge.go`

---

## P4 — 测试工具 / 示例 / 文档

### 4-A `testutil/testutil.go` — 添加平台无关事件注入接口

**文件：** `testutil/testutil.go`

**问题：**  
整个测试工具基于 `*dto.Payload`，只支持 QQ 场景：
- `TestBot.dispatch` 使用 `botctx.NewContext(payload, api)` 旧路径  
- `SendC2C` / `SendGroupAt` 构造 `dto.Payload`  
- `Response` 存储 `[]*dto.Message`

**方案：**  
- 新增 `SendPlatformEvent(event platform.Event) *PlatformResponse`  
- `PlatformResponse` 存储 `[]platform.OutboundMessage`（由 mock `platform.Sender` 捕获）  
- 旧 `SendC2C` / `SendGroupAt` 方法保留（内部改为走 `platform/qq` 路径）

**涉及文件：**
- `testutil/testutil.go`

---

### 4-B `testbot/testbot.go` — 添加平台无关事件注入接口

**文件：** `testbot/testbot.go`

**问题：**  
`MockAPI` 捕获 `*dto.Message`，`SentMessage.Msg` 也是 `*dto.Message`。  
`SendC2C` / `SendGroupAt` 构造 QQ 专属 `dto.Payload`。

**方案：**  
- 新增 `MockSender`（实现 `platform.Sender`）捕获 `platform.OutboundMessage`  
- 新增 `SendPlatformEvent(event platform.Event)` 方法  
- 现有 `MockAPI` 和方法保留（QQ 兼容）

**涉及文件：**
- `testbot/testbot.go`

---

### 4-C 更新全部示例代码

**目录：** `examples/`

**问题：**  
所有 10+ 个示例（`basic-bot`、`command-bot`、`async-tasks`、`middleware-example`、`plugin-v2-demo`、`production-ready`、`showcase` 等）仍使用：
- `eng.On(dto.GroupAtMessageCreate, ...)` / `eng.On(dto.C2CMessageCreate, ...)`  
- `ctx.ReplyGroup(&dto.Message{...})` / `ctx.ReplyPrivate(&dto.Message{...})`  
- `dto.BotInfo` 初始化

**方案（依赖 P2/P3 完成后）：**  
- 改用平台无关 API：`eng.On("GROUP_MESSAGE", ...)` 或 `eng.OnGroupAt(...)`（保留 QQ 便捷方法）  
- 回复改为 `ctx.Reply(platform.TextMessage("..."))`  
- 新增 `examples/multi-platform/` 演示同时接入多平台

**涉及目录：**
- `examples/` 全部子目录

---

### 4-D 更新 `doc.go` 包文档

**文件：** `doc.go`

**问题：**  
包文档仍描述旧的 `Adapter` 接口（`Start(ctx, func(*dto.Payload))`）和 `dto.BotInfo` 用法。

**方案：**  
- 更新示例代码使用 `PlatformAdapter` / `platform.Event`  
- 标注 `Adapter` 已废弃，推荐 `PlatformAdapter`

**涉及文件：**
- `doc.go`

---

### 4-E 补充 `platform/discord`、`telegram`、`wechat` 实现

**目录：** `platform/discord/`、`platform/telegram/`、`platform/wechat/`

**问题：**  
三个平台均为骨架，`StartPlatform` 返回 `"not yet implemented"` 错误。

**方案：**  
- 至少完成一个非 QQ 平台的完整实现（建议 Telegram，有成熟 Go SDK）  
- 每个平台需实现：`Event` 接口、`Sender`、完整 `StartPlatform` 事件循环

---

## 架构决策备忘

| 问题 | 当前状态 | 建议 |
|------|----------|------|
| `engine.EventType` vs `dto.EventType` | `dto.EventType` 仍为路由 key | 定义 `type EventType = string`（2-A） |
| 插件注册事件类型 | `dto.C2CMessageCreate` 字面量 | 完成 2-A 后自动兼容，无需改字面量 |
| `ctx.Reply` 旧 QQ 路径 | `ReplyGroup/ReplyPrivate` 保留 | 标记 Deprecated，引导用 `ctx.Reply(OutboundMessage)` |
| `platform.EventContext` vs `*context.Context` | 两套并存 | 长期目标：`*context.Context` 嵌入/实现 `platform.EventContext` |
| `platform/bridge.go` | 临时桥接，导入 dto | P2 完成后删除 |
| QQ 便捷方法 `OnC2C/OnGroupAt` | 保留为 QQ 快捷方式 | 保留，文档注明 QQ 专属 |

---

## 迁移优先级汇总

```
P2-A engine.EventType 解耦
  ↓
P2-B plugin/registry 接口解耦
  ↓
P2-C/D context/rules + convenience 解耦
  ↓
P3-D~I 各插件回复方式迁移（可并行）
  ↓
P3-A context/decode GetAuthor 迁移
P3-B infra/dlq 迁移
P3-C helper/ 迁移
  ↓
P4-A/B testutil/testbot 平台无关测试支持
P4-C examples 更新
P4-D/E 文档 + 非 QQ 平台实现
```

---

## 不需迁移的部分（保持 QQ 专属）

以下模块属于 QQ 平台实现层，保留 `dto` 依赖是合理的：

- `openapi/` — QQ Bot SDK 接口层（`dto`、`auth`、`protocol/webhook`）
- `platform/qq/` — QQ 平台适配器（内部使用 `dto`）
- `platform/bridge.go` — 过渡桥接（计划 P2 完成后删除）
- `platform/context.go` 中对旧路径的兼容降级代码

