# 多平台抽象层——进度报告与待办事项

> 生成日期：2026-03-11  
> 分支目标：将事件处理与消息发送从 QQ 官方数据结构（`dto.Payload` / `openapi.OpenAPI`）解耦，构建平台无关的抽象层，支持多平台（Discord、Telegram、微信等）。

---

## 一、已完成的内容

| 模块 | 文件 | 说明 |
|---|---|---|
| 平台事件抽象 | `platform/event.go` | `Event` 接口、`EventKind` 枚举、`UserInfo`/`ChatInfo` 结构体 |
| 出站消息抽象 | `platform/message.go` | `OutboundMessage` + 快捷构造函数 |
| 适配器注册表 | `platform/adapter.go` | `PlatformAdapter`、`Sender`、`NoopSender`、`Registry`（含 `StartAll/StopAll`）|
| 平台上下文 | `platform/context.go` | `EventContext` 接口 + `baseEventContext` 实现 |
| QQ 适配器 | `platform/qq/adapter.go` | 从 Webhook 读 `*dto.Payload`，转换为 `platform.Event`，原子并发控制 |
| QQ 事件映射 | `platform/qq/event.go` | C2C、GroupAt、Guild、Notice、System 全类型映射 |
| QQ 发送器 | `platform/qq/sender.go` | 桥接 `platform.Sender → openapi.OpenAPI`，含私聊/群聊路由 |
| 引擎平台路径 | `core/engine/process_platform.go` | `ProcessPlatformEvent(event, sender)` 无 dto 依赖，复用核心路由逻辑 |
| 平台上下文适配 | `core/context/platform_event.go` | `AcquireContextFromEvent`、`Reply`、`GetEventKind`、`GetEventPlatform`、`IsPlatformContext` |
| 上下文双路径解码 | `core/context/decode.go`（部分）| `GetMessageContent`、`GetSenderInfo` 均已支持双路径 |
| Bot 平台入口 | `bot.go` `handlePlatformEvent` | 直接调用 `engine.ProcessPlatformEvent`，从 Registry 获取 Sender |
| Bot 注册器 | `bot_builder.go` `WithPlatformRegistry` | lifecycle 自动注册每个平台适配器 |

---

## 二、仍与 QQ 耦合的部分

### 2.1 `core/context/convenience.go` ⚠️ 高优先级

- `OnUserWhitelist` / `OnUserBlacklist` 调用 `GetAuthor().UserOpenID`（QQ 专属字段）。
- 新平台路径下 Handler 接收到的规则永远不会匹配，导致白名单/黑名单功能在非 QQ 平台失效。
- **修复方案**：改为 `ctx.GetSenderInfo().ID`，该字段在新旧两条路径下均有值。

### 2.2 `core/context/decode.go` ⚠️ 中优先级

- `ReplyGroup(msg *dto.Message)` / `ReplyPrivate(msg *dto.Message)` / `SendGroupMessage` / `SendSingleMessage`：全部依赖 QQ `*dto.Message`，不支持新平台路径。
  - 新路径已有 `ctx.Reply(platform.OutboundMessage)` 作为替代。
  - 旧方法需加 `// Deprecated` 注释并提供迁移说明。
- `GetEvent()` 返回 `*dto.Payload`：旧路径 Handler 仍依赖此方法，但直接暴露 QQ 类型，需标注 Deprecated。
- `DecodeEvent()` 仅支持 QQ payload 解码，新路径下始终返回错误。
- `GetEventType()` 在新路径下将 `platform.Event.RawType()` 强转 `dto.EventType`；语义上保持与 Engine 路由的兼容，但应加注释说明限制。

### 2.3 `core/context/rules.go` ⚠️ 中优先级

- `OnC2CMessage()` / `OnGroupAtMessage()` / `OnGroupAddRobot()` / `OnAtBot()` 硬编码 QQ dto 常量。
- 平台无关版本 `OnEventKind(EventKindPrivateMessage)` 已存在，但缺乏文档和推广。
- `InGroup()` 已有双路径 ✅；`OnGroupWhitelist/OnGroupBlacklist` 对新路径透明 ✅。

### 2.4 `core/engine/process.go` ⚠️ 低优先级

- `ProcessEventBatch([]*dto.Payload, openapi.OpenAPI)` 仍为纯 QQ 路径。
- 缺少对应的平台无关重载 `ProcessPlatformEventBatch([]platform.Event, platform.Sender)`。

### 2.5 `plugins/sendqueue` + `plugins/broadcast` ⚠️ 中优先级

- `sendqueue.sendJob` 内部双路径并存（旧：`*dto.Message`+`openapi.OpenAPI`；新：`platform.Sender`+`OutboundMessage`），旧路径仍在运行。
- `broadcast.Plugin.ToGroups()` / `ToSubscribedGroups()` 依赖 `*dto.Message`。
- `broadcast.Broadcast()` 方法已平台无关 ✅。
- 两个插件的 `SetAPI()` 已标 Deprecated 但未移除，造成双路径维护负担。

### 2.6 `platform/qq/sender.go` chatType 注入缺口 🔴 高优先级

- `qq.sender.Send()` 从 `context.Value(chatTypeContextKey{})` 读取会话类型。
- `core/context/platform_event.go` 中的 `Reply()` 调用时传入 `stdctx.Background()`，**chat type 不会被自动注入**。
- 结果：私聊消息也会 fallback 走 `GroupChat` 路径，导致私聊消息发送失败或发到错误频道。
- **修复方案**：在 `platform/qq/adapter.go` 的 `StartPlatform` 处理事件前，根据 `event.Chat().IsGroup` 将 `chatType` 注入到传给 handler 的 Go context。

---

## 三、尚未实现的部分

### 3.1 平台适配器骨架（仅有存根）

| 目录 | 状态 | 说明 |
|---|---|---|
| `platform/discord/` | 🚧 骨架 | `StartPlatform` 直接返回 `"not yet implemented"` |
| `platform/telegram/` | 🚧 骨架 | 同上 |
| `platform/wechat/` | 🚧 骨架 | 同上 |

每个平台需实现：
1. SDK 接入与认证
2. 接收原始消息 → 包装为 `platform.Event`
3. `platform.Sender` 实现（调用平台 API 发送消息）
4. 完整的 `PlatformAdapter`（`StartPlatform` / `StopPlatform` / `Name`）

### 3.2 测试基础设施迁移 ⚠️ 高优先级

目前所有测试辅助工具都与 QQ SDK 强绑定：

| 文件 | 问题 |
|---|---|
| `testbot/testbot.go` | `SendGroupAt` / `SendC2C` 内部构造 `dto.GroupAtMessageCreateEvent` / `dto.C2CMessageCreateEvent` |
| `testutil/testutil.go` | `dispatch`/`c2cPayload`/`groupAtPayload` 全部使用 `*dto.Payload` |
| `tests/integration/e2e_test.go` | 16 处手工构造 `*dto.Payload` |
| `plugins/*/` 测试文件 | `antispam_test.go`、`stats_test.go`、`conversation_test.go`、`i18n_test.go` 等直接构造 QQ 结构体 |

**建议目标**：在 `testutil` 中新增 `MakePlatformC2CEvent` / `MakePlatformGroupEvent` 等工厂函数，让测试通过 `platform.Event` 创建，消除对 dto 的直接依赖。

### 3.3 `platform/bridge.go` 删除计划

- 文件顶部已有注释标注"P2 迁移完成后删除"。
- 目前尚无明确的删除 milestone，需在旧路径完全清退后执行。

---

## 四、待完成事项（按优先级）

### P0 — 当前功能正确性（影响已有功能）

- [ ] **修复 `qq.Sender` chatType 注入缺口**  
  文件：`platform/qq/adapter.go`  
  在 `StartPlatform` 中，基于 `event.Chat().IsGroup` 将 `chatType` 注入到传给 handler 的 Go context，确保私聊消息路由正确。

- [ ] **修复 `OnUserWhitelist` / `OnUserBlacklist`**  
  文件：`core/context/convenience.go`  
  将 `GetAuthor().UserOpenID` 改为 `ctx.GetSenderInfo().ID`（双路径均有值）。

### P1 — 完成平台无关层核心功能

- [ ] **新增测试基础设施平台无关工厂函数**  
  文件：`testutil/testutil.go`、`testbot/testbot.go`  
  添加 `MakePlatformC2CEvent` / `MakePlatformGroupEvent` 等基于 `platform.Event` 的辅助函数，并迁移 plugin 单元测试。

- [ ] **补充 `ProcessPlatformEventBatch`**  
  文件：`core/engine/process_platform.go`  
  新增 `ProcessPlatformEventBatch([]platform.Event, platform.Sender)` 重载，与现有 QQ 路径对称。

- [ ] **为 `OnEventKind()` 添加文档与推广**  
  文件：`core/context/rules.go`  
  在文件顶部注释中将 `OnEventKind` 标注为"多平台推荐方式"，将 `OnC2CMessage` / `OnGroupAtMessage` 标注为"QQ 专用，将在未来版本移除"。

### P2 — 技术债清理

- [ ] **旧消息方法加 Deprecated 注释**  
  文件：`core/context/decode.go`  
  为 `ReplyGroup` / `ReplyPrivate` / `SendGroupMessage` / `SendSingleMessage` / `GetEvent` 添加 `// Deprecated: 使用 ctx.Reply(platform.OutboundMessage) 替代` 注释。

- [ ] **`sendqueue` 旧路径清理**  
  文件：`plugins/sendqueue/`  
  将旧 `sendJob` 中的 `isGroup/msg/api` 字段标注 Deprecated，推动插件用户迁移到 `sender+OutboundMessage` 路径。

- [ ] **`broadcast.ToGroups` / `ToSubscribedGroups` 迁移**  
  文件：`plugins/broadcast/`  
  用 `Broadcast(chatIDs, OutboundMessage)` 取代依赖 `*dto.Message` 的旧方法。

### P3 — 新平台实现

- [ ] **Discord 适配器完整实现**  
  目录：`platform/discord/`  
  集成 `discordgo` SDK，实现 `Event` 包装与 `Sender` 桥接。

- [ ] **Telegram 适配器完整实现**  
  目录：`platform/telegram/`  
  集成 `telegram-bot-api`，同上。

- [ ] **微信适配器完整实现**  
  目录：`platform/wechat/`  
  按微信官方 API 实现。

- [ ] **删除 `platform/bridge.go`**  
  条件：所有旧路径插件和测试均已完成迁移后执行。

---

## 五、架构决策待定项

### 5.1 `GetEventType()` 路由键语义问题

**现状**：新路径下 `GetEventType()` 将 `platform.Event.RawType()` 强转为 `dto.EventType`。若 Discord 事件的 `RawType()` 为 `"MESSAGE_CREATE"`，而 Matcher 注册时使用 `dto.C2CMessageCreate`（值为 `"C2C_MESSAGE_CREATE"`），则路由不匹配。

**三个方案**：
- **方案 A**：维持 `RawType()` 返回平台原生字符串，要求 Matcher 注册时使用平台原生字符串（多平台需分别注册）。
- **方案 B**：`RawType()` 统一映射为 `EventKind` 对应的字符串（框架内部约定），屏蔽平台差异。
- **方案 C**：引入 Engine 级别的"按 `EventKind` 路由"专用接口，与现有 `RawType` 路由并列。

> **建议**：优先考虑方案 B，兼顾向后兼容与多平台透明性。

### 5.2 `platform.EventContext` 与 `*context.Context` 并存

**现状**：Engine 内部全程使用 `*context.Context`，`platform.EventContext` 接口已定义但 Engine 未直接使用。

**问题**：是否要让 Engine 切换到只依赖 `platform.EventContext`，以实现更彻底的解耦？

> **建议**：阶段性保留 `*context.Context` 作为 Engine 内部唯一类型，`platform.EventContext` 仅作为对外暴露的接口；待测试基础设施迁移完成后再评估是否深度切换。

### 5.3 测试迁移策略

- **全量迁移**：将 `e2e_test.go` 中 16 处 `dto.Payload` 构造全部替换为 `platform.Event`，更干净但代价大。
- **渐进迁移**：保留旧测试 + 新增平台无关测试用例，冗余但风险低。

> **建议**：插件单元测试全量迁移（影响范围小）；E2E 集成测试渐进迁移（保留旧用例，逐步新增平台无关版本）。

---

## 六、当前分支结论

| 维度 | 完成度 |
|---|---|
| 平台抽象接口定义 | ✅ 100% |
| QQ 平台适配器 | ✅ 95%（chatType 注入缺口待修） |
| Engine 平台无关路径 | ✅ 90%（缺 Batch 重载）|
| Context 双路径支持 | 🔄 70%（旧方法 Deprecated 标注待完成）|
| 测试基础设施迁移 | ❌ 10%（全部依赖 QQ dto）|
| 非 QQ 平台实现 | ❌ 0%（均为骨架存根）|
| 技术债清理 | ❌ 20%（Deprecated 标注基本未做）|

**总体评估**：分支核心架构已就绪，QQ 平台的新路径可用但存在 P0 级 bug（chatType 注入）；测试基础设施和技术债清理是完成本分支目标的最大障碍。

