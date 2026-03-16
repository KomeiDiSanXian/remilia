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

**已完成的代码清理**（2026-03-15 更新）：

| 问题 | 清理内容 | 受影响文件 |
|------|---------|-----------|
| **#1** ✅ | `platform/context.go` 已删除（`EventContext`、`baseEventContext`、`NewEventContext` 全部移除） | `platform/context.go` |
| **#2** ✅ | `platform/bridge.go` 已删除（`LegacyAdapter`、`LegacyEventHandler` 全部移除，消除 platform → openapi/dto 反向依赖） | `platform/bridge.go` |
| **#3** ✅ | 旧 `Adapter` 接口从 `core/engine/types.go` 删除，根包 `adapter.go`（含 `type Adapter = engine.Adapter` 别名）已整体删除，`Bot.adapter` 字段已改为 `engine.PlatformAdapter`，`WebhookServerAdapter` 注释中的残留引用已清理 | `core/engine/types.go`、`adapter.go`（已删除）、`bot.go`、`webhook_adapter.go` |

**已完成的代码清理**（2026-03-16 更新）：

| 问题 | 清理内容 | 受影响文件 |
|------|---------|-----------|
| **#4** ✅ | 修复 `handlePlatformEvent` Sender 回退 Bug：无注册表时正确使用 `b.adapter.Sender()`；`NewBot` 允许 nil adapter（调试日志代替 panic）；生命周期注册添加 nil 守卫；`Build()` 在注册表已提供时不再要求 adapter；`WithPlatformRegistry` 注释更新（去除"旧 QQ 路径并行"误导说法） | `bot.go`、`bot_builder.go`、`bot_nil_test.go` |
| **#5** ✅ | 根包 `adapter.go` 已于 #3 整体删除；修正 `platform/qq/adapter.go` 中错误的 Usage 示例（`WebhookServerAdapter` 不实现 `Webhook` 接口）；文档明确两种用法的适用场景 | `platform/qq/adapter.go` |
| **#7** ✅ | `platform/qq/sender.go` 中 `chatType`/`chatTypeContextKey` 等 Deprecated 代码已于前序清理中删除，当前文件（80 行）仅保留正确实现 | `platform/qq/sender.go`（无需变更） |
| **#8** ✅ | `core/context/decode.go` 中 `GetAuthor()`、`SendGroupMessage()`、`ReplyGroup()` 等 Deprecated QQ 专属方法已于前序清理中删除，当前文件仅保留平台无关实现 | `core/context/decode.go`（无需变更） |
| **#9** ✅ | `core/context/rules.go` 中 `OnC2CMessage()`、`OnGroupAtMessage()` 等 Deprecated QQ 规则已于前序清理中删除；修正 `convenience.go` 中注释里对已删除函数的引用，替换为 `OnEventKind(platform.EventKindGroupMessage)` | `core/context/rules.go`（无需变更）、`core/context/convenience.go` |
| **#10** ✅ | `core/engine/engine_matcher_ops.go` 及 `bot.go` 中 `OnC2C()`、`OnGroupAt()` 等 QQ 专属便捷方法已于前序清理中删除，当前仅保留平台无关方法 `OnEventKind()` | `core/engine/engine_matcher_ops.go`、`bot.go`（无需变更） |
| **#14** ✅ | `plugins/sendqueue`：`EnqueueGroup()`、`EnqueueC2C()`、`SetDefaultAPI()`、`Plugin.defaultAPI`、`sendJob` 中的 `isGroup`/`msg *dto.Message`/`api openapi.OpenAPI` 字段均已于前序清理中删除；当前实现仅使用 `platform.Sender`，无 dto/openapi 依赖 | `plugins/sendqueue/sendqueue.go`（无需变更） |
| **#15** ✅ | `plugins/broadcast`：`SetAPI()`、`ToGroups()`、`ToUsers()`、`ToBoth()`、`BroadcastToGroups()`、`BroadcastToC2C()`、`Plugin.api openapi.OpenAPI` 字段均已于前序清理中删除；修正包级 doc 注释中残留的"旧 QQ 路径（仍然有效）"示例（`SetAPI`/`ToGroups`），替换为仅保留平台无关用法 | `plugins/broadcast/broadcast.go` |
| **#16** ✅ | `ErrNoPlatformSender` 错误信息已于前序清理中更新为正确文本，当前值为 `"no platform sender available: context was not created from a platform event"`，无误导性说法 | `core/context/state.go`（无需变更） |

**已完成的代码清理**（2026-03-16 更新，第二批）：

| 问题 | 清理内容 | 受影响文件 |
|------|---------|-----------|
| **#11** ✅ | `BlockError`/`NewBlockError`/`IsBlockError` 别名已于前序清理中从 `core/engine/errors.go` 删除；顺带清理发现的额外项：`core/engine/config.go` 中 `DeadLetterItem = dlq.DeadLetterItem` 类型别名（Deprecated）及其 `dlq` import 同步删除 | `core/engine/config.go` |
| **#12** ✅ | `engine.Close()` 已于前序清理中删除，当前 `core/engine/engine.go` 仅保留 `Shutdown(ctx)` | `core/engine/engine.go`（无需变更） |
| **#13** ✅ | `SetViper`/`ListPluginGoroutines`/`Require[T]`/`Optional[T]` 已于前序清理中从 `plugin/` 删除；顺带清理发现的额外项：`plugin/config.go` 中的 `NewPluginConfig(viper)`（Deprecated）、`Config.Set` 接口方法（Deprecated）、`pluginConfig.Set` 实现方法、`pluginConfig.viper` 字段、viper 兼容路径及 `fmt`/`viper` 导入全部删除；测试文件同步更新 `NewPluginConfig → NewPluginConfigFromProvider` | `plugin/config.go`、`plugin/p2_fixes_test.go` |

**尚未修复的问题**：

| 问题 | 状态 |
|------|------|
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
| `platform/context.go` | ✅ 已删除 | `EventContext` 孤立接口已删除（问题 #1 已修复） |
| `platform/bridge.go` | ✅ 已删除 | `LegacyAdapter`、`LegacyEventHandler` 已删除，platform 包不再依赖 `openapi/dto`（问题 #2 已修复） |

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

### ✅ 问题 #1：`platform.EventContext` 是孤立的死代码（已修复）

**文件**：`platform/context.go`（已删除）

`platform` 包定义了完整的 `EventContext` 接口和 `baseEventContext` 实现，但引擎从未使用它——引擎使用的是 `core/context.Context`（通过 `AcquireContextFromEvent` 创建）。两套 Context 抽象并存造成概念混乱：

- 实际使用的是 `*core/context.Context`（带对象池、中间件链、规则引擎集成）
- `platform.EventContext` 只有基础的 `Get/Set/Reply/Send`，没有规则、中间件等框架能力

**已执行**：`platform/context.go` 已在 2026-03-15 删除。

---

### ✅ 问题 #2：`platform/bridge.go` 应立即删除（已修复）

**文件**：`platform/bridge.go`（已删除）

```go
// Deprecated: P2 引擎迁移完成后本文件将被删除。
type LegacyEventHandler func(*dto.Payload)
type LegacyAdapter interface { ... }
```

- `LegacyAdapter` 与 `engine.Adapter`（同样 Deprecated）完全重复
- 该文件使 `platform` 包反向依赖 `openapi/dto`，破坏了抽象层的隔离目标
- 没有任何代码引用此接口

**已执行**：`platform/bridge.go` 已在 2026-03-15 删除，`platform` 包不再依赖 `openapi/dto`。

---

### ✅ 问题 #3：`engine.Adapter`（旧接口）仍在核心类型中，强迫 `core/engine` 导入 `openapi/dto`（已修复）

**文件**：`core/engine/types.go`（旧 `Adapter` 接口已删除）、`adapter.go`（已整体删除）

旧接口：
```go
// Deprecated: Use PlatformAdapter instead.
type Adapter interface {
    Start(ctx stdctx.Context, handleFunc func(*dto.Payload)) error
    Stop(ctx stdctx.Context) error
}
```

这是 `core/engine` 包唯一一处对 `openapi/dto` 的直接依赖，已可删除。

**已执行**：
1. `core/engine/types.go` 中的 `Adapter` 接口已在 2026-03-15 删除
2. 根包 `adapter.go`（含 `type Adapter = engine.Adapter` 别名及 `webhookAdapter`）已整体删除
3. `Bot.adapter` 字段已改为 `engine.PlatformAdapter`
4. `WebhookServerAdapter` 注释中残留的旧接口引用已清理

---

### ✅ 问题 #4：`Bot` 存在双路径架构，逻辑分裂（已修复）

**文件**：`bot.go`、`bot_builder.go`、`bot_nil_test.go`

**已执行**：
1. `handlePlatformEvent` Sender 回退修复：无注册表时正确使用 `b.adapter.Sender()`（修复前永远使用 `NoopSender`）
2. `NewBot` 允许 nil adapter（调试日志代替 panic），支持纯注册表模式
3. 适配器生命周期注册添加 nil 守卫（`if b.adapter != nil { ... }`）
4. `Build()` 在注册表已提供时不再强制要求 adapter（`if b.adapter == nil && b.platformRegistry == nil`）
5. `WithPlatformRegistry` 注释更新（去除"旧 QQ 路径并行"误导说法）
6. `bot_nil_test.go` 更新：nil adapter 不再触发 panic

---

### ✅ 问题 #5：`adapter.go` 与 `webhook_adapter.go` 在根包重复（已修复）

**文件**：`adapter.go`（已删除）、`platform/qq/adapter.go`

**已执行**：
1. `adapter.go` 已于 #3 整体删除（`NewWebhookAdapter`/`NewWebhookAdapterWithAPI` 随之消失）
2. 修正 `platform/qq/adapter.go` 中错误的 Usage 示例：原示例将 `WebhookServerAdapter` 误作 `Webhook` 接口（`EventStream()`）的实现；现已替换为正确的两种场景说明：
   - `qq.NewAdapter(webhookConn, api)` 适用于已有 `webhook.Conn` 的场景
   - `WebhookServerAdapter` 直接作为 `PlatformAdapter` 适用于单平台简单场景

---

### ✅ 问题 #6：`WebhookServerAdapter.Start()` 是 Deprecated 的旧签名（已修复）

已删除，详见 2026-03-15 清理记录。

---

### ✅ 问题 #7：`qq/sender.go` 保留了 Deprecated 的 `chatType` 降级路径（已修复）

`chatType`、`chatTypeContextKey`、`chatTypePrivate`、`chatTypeGroup` 等 Deprecated 代码已在前序清理中删除。当前 `platform/qq/sender.go`（80 行）仅保留基于 `platform.ChatInfoFromContext` 的正确实现，无需变更。

---

### ✅ 问题 #8：`core/context/decode.go` — 多个 Deprecated 的 QQ 专属方法（已修复）

`GetAuthor()`、`SendGroupMessage()`、`SendSingleMessage()`、`ReplyGroup()`、`ReplyPrivate()` 等方法已在前序清理中删除。当前 `decode.go` 仅保留平台无关实现，无需变更。

---

### ✅ 问题 #9：`core/context/rules.go` — 多个 Deprecated 的 QQ 专属规则（已修复）

`OnC2CMessage()`、`OnGroupAtMessage()`、`OnGroupAddRobot()`、`OnGroupDelRobot()`、`OnAtBot()` 已在前序清理中删除。

**额外修复**：`core/context/convenience.go` 中 `OnGroupWhitelist`/`OnGroupBlacklist` 的注释引用了已删除的 `OnGroupAtMessage()`，已替换为 `OnEventKind(platform.EventKindGroupMessage)`。

---

### ✅ 问题 #10：`core/engine/engine_matcher_ops.go` — QQ 专属便捷方法未标记 Deprecated（已修复）

`OnC2C()`、`OnGroupAt()`、`OnGroupAdd()`、`OnGroupDel()` 等 QQ 专属方法已在前序清理中删除。`bot.go` 中的对应方法同样已删除。当前仅保留平台无关方法 `OnEventKind()`，无需变更。

---

### ✅ 问题 #11：`core/engine/errors.go` — 重复导出 Deprecated 别名（已修复）

`BlockError`/`NewBlockError`/`IsBlockError` 已于前序清理（2026-03-15）中从 `core/engine/errors.go` 全部删除。

**额外清理**：同次发现 `core/engine/config.go` 中的 `DeadLetterItem = dlq.DeadLetterItem` 类型别名仍保留且标记 Deprecated，已同步删除，连带移除仅为此别名服务的 `dlq` import。

---

### ✅ 问题 #12：`core/engine/engine.go` — `Close()` 方法（已修复）

`engine.Close()` 已于前序清理（2026-03-15）中删除，当前 `core/engine/engine.go` 仅保留 `Shutdown(ctx context.Context) error`，无需变更。

---

### ✅ 问题 #13：`plugin/` — 多个 Deprecated 符号（已修复）

`SetViper`、`ListPluginGoroutines`、`Require[T]`、`Optional[T]` 已于前序清理（2026-03-15）中全部删除。

**额外清理**（2026-03-16）：扫描发现 `plugin/config.go` 中存在以下额外 Deprecated 项，已一并删除：

| 删除项 | 说明 |
|--------|------|
| `NewPluginConfig(pluginName, *viper.Viper)` | Deprecated 函数；改用 `NewPluginConfigFromProvider` |
| `Config.Set(key, value)` | Deprecated 接口方法（`Override` 的别名） |
| `pluginConfig.Set()` | 对应实现方法 |
| `pluginConfig.viper *viper.Viper` | viper 兼容字段 |
| `loadFromGlobal()` 中的 viper 兜底路径 | 仅在 `NewPluginConfig` 创建时有效的旧路径 |
| `"fmt"` / `"github.com/spf13/viper"` | 随 viper 路径一起移除的无用 import |

测试文件 `plugin/p2_fixes_test.go` 同步更新：3 处 `NewPluginConfig("testplugin", nil)` 替换为 `NewPluginConfigFromProvider("testplugin", nil)`。

---

### ✅ 问题 #14：`plugins/sendqueue` — Deprecated 旧 QQ 路径（已修复）

`sendJob` 中的 `isGroup bool`、`msg *dto.Message`、`api openapi.OpenAPI` 字段，以及 `EnqueueGroup()`、`EnqueueC2C()`、`SetDefaultAPI()`、`Plugin.defaultAPI` 已在前序清理中全部删除。当前 `sendqueue.go` 仅使用 `platform.Sender`/`platform.OutboundMessage`，无任何 dto/openapi 依赖，无需变更。

---

### ✅ 问题 #15：`plugins/broadcast` — 多个 Deprecated 方法（已修复）

`SetAPI()`、`ToGroups()`、`ToUsers()`、`ToBoth()`、`BroadcastToGroups()`、`BroadcastToC2C()`、`Plugin.api openapi.OpenAPI` 字段已在前序清理中全部删除。

**额外修复**：包级 doc 注释中残留的"旧 QQ 路径（仍然有效）"示例（含 `SetAPI`/`ToGroups` 调用）已替换，现仅保留平台无关用法说明。

---

### ✅ 问题 #16：`ErrNoPlatformSender` 错误信息具有误导性（已修复）

`core/context/state.go` 中的 `ErrNoPlatformSender` 已在前序清理中更新为正确文本：

```go
var ErrNoPlatformSender = errors.New("no platform sender available: context was not created from a platform event")
```

无误导性的 Deprecated 方法引用，无需变更。

---

## 三、缺失的实现

### 3.1 `discord/telegram/wechat` 骨架内部的 `Event` 结构体从未使用

`platform/discord/adapter.go` 中定义了 `discordEvent struct{}`（同理 telegram、wechat），但：
1. 没有任何地方实例化它
2. `StartPlatform` 永远返回错误，不会产生事件

这些文件目前只是让编译通过的占位符，实际功能为零。可以考虑用构建标签（build tag）或接口兜底，避免暴露永远报错的 `NewAdapter()`。

### ✅ 3.2 `engine.OnEventKind()` 平台无关便捷方法（已实现）

`engine.OnEventKind(kind, rules...)` 和 `bot.OnEventKind(kind, rules...)` 均已实现，无需手动类型转换。

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

### 4.1 QQ 适配器三重实现，职责重叠（✅ 已修复）

同一功能（读取 QQ 事件并转换为 `platform.Event`）原有三处实现：

| 实现 | 位置 | 特点 |
|------|------|------|
| ~~`webhookAdapter`~~ | ~~`adapter.go`~~（**已删除** ✅） | 简单包装，无 HTTP 服务器 |
| `WebhookServerAdapter` | `webhook_adapter.go` | 内置 HTTP 服务器、多 Worker |
| `qq.Adapter` | `platform/qq/adapter.go` | 读取 `Webhook.EventStream()`，不含 HTTP |

`adapter.go` 已整体删除（问题 #3/#5）。`platform/qq/adapter.go` 的错误 Usage 注释（将 `WebhookServerAdapter` 误作 `Webhook` 接口使用）已修正：两者适用场景已明确区分（见问题 #5 说明）。

### 4.2 `platform` 包反向依赖 `openapi/dto`（通过 bridge.go）✅ 已修复

`bridge.go` 已删除，`platform` 包完全不依赖 `openapi/dto`，实现了真正的平台无关。

### 4.3 `core/engine` 包仍导入 `openapi/dto`（通过 `types.go` 的旧 `Adapter`）✅ 已修复

`engine.Adapter` 已删除，`core/engine` 对 `openapi/dto` 的直接依赖已完全消除。

### 4.4 `BotBuilder.WithAdapter(adapter Adapter)` 接受 Deprecated 类型 ✅ 已修复

`Adapter` 别名随 `adapter.go` 整体删除（#3）。`WithAdapter` 现在接受 `engine.PlatformAdapter`。

### 4.5 `pprof.go` 有一处 Deprecated 方法

**文件**：`pprof.go`（第 332 行）— 低优先级，按需清理。

---

## 五、优先级汇总

| 优先级 | 问题 | 操作 |
|--------|------|------|
| ✅ 已完成 | 问题 #1：`platform.EventContext` 孤立死代码 | `platform/context.go` 已删除 |
| ✅ 已完成 | 问题 #2：`platform/bridge.go` 全部 Deprecated | 整个文件已删除 |
| ✅ 已完成 | 问题 #3：`engine.Adapter` 旧接口迫使 engine 依赖 dto | 旧接口已删除，`adapter.go` 整体删除，依赖已消除 |
| ✅ 已完成 | 问题 #4：Bot 双路径架构 | Sender 回退修复；nil adapter 支持；Build() 放宽；注释更新 |
| ✅ 已完成 | 问题 #5：根包两个 QQ 适配器重复 | `adapter.go` 已删除；`platform/qq/adapter.go` 错误注释已修正 |
| ✅ 已完成 | 问题 #6：Deprecated `Start()` | 已删除 |
| ✅ 已完成 | 问题 #7：`chatType` Deprecated | 已删除 |
| ✅ 已完成 | 问题 #8：Context Deprecated 方法 | 已删除 |
| ✅ 已完成 | 问题 #9：Rules Deprecated 规则 | 已删除；`convenience.go` 注释已更新 |
| ✅ 已完成 | 问题 #10：QQ 便捷方法未标注 Deprecated | 已删除，替换为 `OnEventKind` |
| ✅ 已完成 | 问题 #14：`plugins/sendqueue` Deprecated QQ 路径 | 实现已清理（前序），doc 注释无误 |
| ✅ 已完成 | 问题 #15：`plugins/broadcast` Deprecated QQ 路径 | 实现已清理（前序），包 doc 注释已更新 |
| ✅ 已完成 | 问题 #16：`ErrNoPlatformSender` 错误信息 | 已更新为正确文本（前序） |
| ✅ 已完成 | 问题 #11：engine Deprecated 别名 | `BlockError` 等别名已删除；`DeadLetterItem` 别名及 `dlq` import 同步清理 |
| ✅ 已完成 | 问题 #12：`engine.Close()` | 已删除，仅保留 `Shutdown(ctx)` |
| ✅ 已完成 | 问题 #13：plugin Deprecated 符号 | `SetViper`/`ListPluginGoroutines`/`Require[T]`/`Optional[T]` 已删除；`NewPluginConfig`/`Config.Set`/viper 路径同步清理 |
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

