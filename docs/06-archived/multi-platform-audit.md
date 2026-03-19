# 多平台抽象分支现状审查报告

> 分支：`feature/multi-platform-abstraction`  
> 审查日期：2026-03-16  
> 状态：**进行中（QQ 适配器已完整，其余平台为占位符）**

---

## 目录

1. [已实现内容概述](#1-已实现内容概述)
2. [未实现 / TODO 内容](#2-未实现--todo-内容)
3. [待删除的 Deprecated 代码](#3-待删除的-deprecated-代码)
4. [Bug 与设计问题](#4-bug-与设计问题)
5. [项目结构合理性评估](#5-项目结构合理性评估)
6. [改进建议优先级总表](#6-改进建议优先级总表)

---

## 1. 已实现内容概述

### 1.1 平台抽象层（`platform/`）

| 文件 | 状态 | 说明 |
|------|------|------|
| `platform/adapter.go` | ✅ 完整 | `PlatformAdapter`、`Sender`、`Registry`、`NoopSender` 接口与实现 |
| `platform/event.go` | ✅ 完整 | `Event` 接口、`EventKind` 枚举、`UserInfo`、`ChatInfo` 结构体 |
| `platform/message.go` | ✅ 完整 | `OutboundMessage`、`TextMessage/MarkdownMessage/ImageMessage` 便捷构造函数 |
| `platform/qq/adapter.go` | ✅ 完整 | QQ 平台适配器，完整实现 `StartPlatform`/`Stop`/`Sender` |
| `platform/qq/event.go` | ✅ 完整 | QQ 事件映射（C2C、GroupAt、Guild、Notice、System） |
| `platform/qq/sender.go` | ✅ 完整 | QQ 消息发送，支持私聊/群聊自动路由 |
| `platform/qq/webhook_server.go` | ✅ 完整 | 内置 HTTP 服务器的 Webhook 适配器，支持多 Worker |
| `platform/telegram/adapter.go` | ⚠️ 占位符 | 仅骨架，未实现 |
| `platform/discord/adapter.go` | ⚠️ 占位符 | 仅骨架，未实现 |
| `platform/wechat/adapter.go` | ⚠️ 占位符 | 仅骨架，未实现 |

### 1.2 引擎层集成（`core/engine/`、`core/context/`）

| 内容 | 状态 | 说明 |
|------|------|------|
| `engine.ProcessPlatformEvent` | ✅ 完整 | 平台无关事件处理入口，复用 `processEventContext` 核心逻辑 |
| `context.AcquireContextFromEvent` | ✅ 完整 | 从 `platform.Event` 创建 Context（新路径） |
| `context.ReleaseContextFromEvent` | ✅ 完整 | 归还对象池 |
| `ctx.GetPlatformEvent()` | ✅ 完整 | 访问原始 `platform.Event` |
| `ctx.GetEventKind()` | ✅ 完整 | 新/旧路径双适配，返回 `platform.EventKind` |
| `ctx.GetMessageContent()` | ✅ 完整 | 新路径读 `event.Content()`，旧路径读 gjson |
| `ctx.GetSenderInfo()` | ✅ 完整 | 新路径读 `event.Sender()`，旧路径读 QQ author |
| `ctx.Reply()` | ✅ 完整 | 通过 `platform.Sender` 发送，自动注入 ChatInfo |
| `ctx.ReplyWithContext()` | ✅ 完整 | 支持调用方传入超时 context |
| `OnEventKind()` 规则 | ✅ 完整 | 多平台通用事件类型路由 |

### 1.3 Bot 层（根包）

| 内容 | 状态 | 说明 |
|------|------|------|
| `Bot.UsePlatformRegistry()` | ✅ 完整 | 注入多平台注册表 |
| `Bot.handlePlatformEvent()` | ✅ 完整 | 自动从 Registry 获取对应平台 Sender |
| `BotBuilder.WithPlatformRegistry()` | ✅ 完整 | Builder 支持多平台注册表模式 |
| `Bot.OnEventKind()` | ✅ 完整 | Bot 级别的平台无关事件注册 |

---

## 2. 未实现 / TODO 内容

### 2.1 平台适配器（优先级：低，按需实现）

以下三个平台适配器目前均为空骨架，`StartPlatform` 直接返回 `not yet implemented` 错误：

```
platform/telegram/adapter.go  — telegramEvent 为空结构体，所有方法返回零值
platform/discord/adapter.go   — discordEvent 为空结构体，所有方法返回零值
platform/wechat/adapter.go    — wechatEvent 为空结构体，所有方法返回零值
```

**各平台实现时需要完成：**
- 引入对应平台 SDK（如 `github.com/go-telegram-bot-api/telegram-bot-api`）
- 实现 `StartPlatform`：建立 WebSocket/轮询连接，将原始事件转为 `platform.Event`
- 实现 `Sender.Send`：将 `OutboundMessage` 转换为平台特定格式并调用 API
- 实现具体的 `Event` 包装结构体，填充所有接口方法

### 2.2 DLQ（死信队列）未迁移至 `platform.Event`

`infra/dlq/types.go` 中虽已定义 `Item[T]` 泛型类型并标注 `DeadLetterItem` 为 Deprecated，但：
- `MarshalDeadLetterItem` 仍硬编码访问 `*dto.Payload` 的 `.ID` 和 `.Type` 字段
- `KafkaConsumer.Consume(item DeadLetterItem)` 仍接受旧类型
- 没有基于 `Item[platform.Event]` 的序列化函数
- **Kafka 消费者未实现**（见[第 3 节](#3-待删除的-deprecated-代码)）

需要新增：
```go
// 需要实现的函数
func MarshalPlatformEventItem(item Item[platform.Event]) ([]byte, error)
type PlatformEventConsumer interface { Consume(item Item[platform.Event]) }
```

### 2.3 `Context.Clone()` 未拷贝平台字段

`core/context/context.go` 中的 `Clone()` 方法不复制 `platformEvent` 和 `platformSender`：

```go
// 当前实现（有缺陷）
newCtx := &Context{
    ctx:     newStdCtx,
    matcher: ctx.matcher,
    event:   clonedEvent,
    api:     ctx.api,
    // ❌ platformEvent 和 platformSender 未被拷贝
}
```

**影响**：Handler 在新平台路径下调用 `ctx.Clone()` 后，克隆的 Context 无法调用 `ctx.Reply()`，会返回 `ErrNoPlatformSender`。

**需要修复**（见[第 4 节](#4-bug-与设计问题)）。

### 2.4 缺少多平台集成测试

目前没有任何测试覆盖以下场景：
- `platform.Registry` 同时运行多个模拟平台适配器
- `Bot.handlePlatformEvent` 在多平台模式下的 Sender 路由
- 新平台路径下的 `ctx.Clone()` + `ctx.Reply()` 组合
- `platform_event_test.go` 仅测试了基础的 `GetEventKind`/`GetEventPlatform`，未覆盖 `Reply`

---

## 3. 待删除的 Deprecated 代码

项目尚未发布，以下 Deprecated 代码可直接删除，无需保留向后兼容：

### 3.1 `helper/parse.go` — 整个文件可删除

文件头部已标注该包绑定 `*dto.Payload`，全部 7 个函数均已 Deprecated：

| 函数 | 行号 | 替代方式 |
|------|------|---------|
| `MustParseEvent[T]` | 7 | `ctx.GetPlatformEvent()` + 类型断言 |
| `ParseEvent[T]` | 23 | 同上 |
| `ParseEventWithDefault[T]` | 42 | 同上 |
| `TryParseEvent[T]` | 64 | 同上 |
| `GetContent` | 93 | `helper.ExtractContent(e)` |
| `GetAuthorID` | 118 | `helper.ExtractSenderID(e)` |
| `GetGroupOpenID` | 142 | `helper.ExtractChatID(e)` |

### 3.2 `helper/helper.go` — 删除 `ParseEvent[T]`

第 49 行的 `ParseEvent[T]` 已 Deprecated（绑定 QQ dto.Payload）。  
`ExtractContent`、`ExtractSenderID`、`ExtractChatID` 是其平台无关替代，保留。

### 3.3 `errutil/wrapper.go` — 删除两个旧函数

| 函数 | 行号 | 替代 |
|------|------|------|
| `WrapErrorf(err, message)` | 27 | `errutil.Wrap(err, msg)` |
| `WrapErrorWithContextf(err, msg, ctx)` | 38 | `errutil.WrapWithContext(err, msg, ctx)` |

`ErrorWrapper` 结构体本身可一并删除（新 API 使用 `fmt.Errorf("%w")` 标准链）。

### 3.4 `pprof.go` — 删除 `StartPprofServer`

第 332 行的 `StartPprofServer(addr)` 已 Deprecated，替代为 `NewPprofServer(PprofConfig{...})`。

### 3.5 `core/engine/process.go` — 删除 `getMatchersForEvent`

第 55 行的 `getMatchersForEvent` 标注为仅供测试/调试，行为与 `ProcessEvent` 不一致。  
测试应直接调用 `ProcessEvent`/`ProcessPlatformEvent`，该方法可删除。

### 3.6 `plugins/core/help/help.go` — 删除 `SetPluginManager`

第 146 行的 `SetPluginManager` 已 Deprecated，替代为 `ctx.Info`（`PluginInfo` 接口）。

### 3.7 `infra/dlq/` — `DeadLetterItem` 与 `KafkaConsumer`

| 内容 | 位置 | 处理建议 |
|------|------|---------|
| `DeadLetterItem` struct | `types.go:12` | 迁移完成后删除，当前先保留标注 |
| `MarshalDeadLetterItem` | `types.go` | 迁移到 `MarshalPlatformEventItem` 后删除 |
| `KafkaConsumer` struct | `consumers.go:251` | 实现完成前删除占位实现，避免被误用于生产 |

---

## 4. Bug 与设计问题

### 4.1 🔴 严重：`Context.Clone()` 不复制平台字段

**位置**：`core/context/context.go` `Clone()` 方法

**问题**：新平台路径下 Clone 出的 Context 丢失 `platformEvent` 和 `platformSender`，导致异步 Handler 无法调用 `ctx.Reply()`。

**修复**：
```go
newCtx := &Context{
    ctx:            newStdCtx,
    matcher:        ctx.matcher,
    event:          clonedEvent,
    api:            ctx.api,
    platformEvent:  ctx.platformEvent,   // ✅ 补充
    platformSender: ctx.platformSender,  // ✅ 补充
}
```

### 4.2 🔴 严重：`Registry.StartAll()` 静默丢弃适配器启动错误

**位置**：`platform/adapter.go` `StartAll()` 方法

**问题**：`errCh` channel 被创建但从未被读取，所有适配器启动错误（如端口被占用、配置错误）被完全忽略：

```go
errCh := make(chan error, len(adapters))
for _, a := range adapters {
    go func() {
        if err := a.StartPlatform(ctx, handler); err != nil {
            errCh <- ...  // ❌ 写入但无人读取
        }
    }()
}
<-ctx.Done()
return nil  // ❌ 始终返回 nil
```

**修复**：通过 goroutine 读取 `errCh`，或改用 `errgroup`：
```go
eg, egCtx := errgroup.WithContext(ctx)
for _, a := range adapters {
    eg.Go(func() error {
        return a.StartPlatform(egCtx, handler)
    })
}
go func() { _ = eg.Wait() }()
<-ctx.Done()
return nil
```

### 4.3 🟡 中：`platform/qq/adapter.go` 中 `wg` 未使用

**位置**：`platform/qq/adapter.go`

**问题**：`Adapter` 结构体定义了 `wg sync.WaitGroup`，但事件循环中从未调用 `wg.Add(1)/Done()`，`Stop()` 中的 `wg.Wait()` 会立即返回而非等待正在处理的事件完成。

**修复**：在 `safeInvoke` 调用前后包裹 `a.wg.Add(1)/Done()`，或删除 `wg`（`WebhookServerAdapter` 已正确使用 `wg`）。

### 4.4 🟡 中：`NewBot()` 中 `platformRegistry` 生命周期注册时序问题

**位置**：`bot.go` `NewBot()` 函数

**问题**：`NewBot()` 在构造时检查 `b.platformRegistry != nil` 并注册生命周期组件。但 `UsePlatformRegistry()` 是在构造后调用的，此时注册已经错过：

```go
// NewBot() 中的检查
if b.platformRegistry != nil {  // ❌ 此时永远为 nil（除非通过 Option 传入）
    for _, pa := range b.platformRegistry.All() { ... }
}
```

实际上 `BotBuilder.Build()` 先调用 `NewBot()` 再调用 `b.UsePlatformRegistry(registry)`，导致平台适配器的生命周期组件**从未被注册**。

**需验证**：通过测试确认此路径，若确认为 Bug 则需要将注册逻辑移至 `Start()` 阶段。

### 4.5 🟡 中：`BotBuilder` 有两个相同方法

**位置**：`bot_builder.go`

`WithAdapter` 和 `WithPlatformAdapter` 实现完全一致，造成 API 重复：

```go
func (b *BotBuilder) WithAdapter(adapter engine.PlatformAdapter) *BotBuilder { ... }
func (b *BotBuilder) WithPlatformAdapter(adapter engine.PlatformAdapter) *BotBuilder { ... }
```

建议删除 `WithAdapter`，仅保留语义更明确的 `WithPlatformAdapter`。

### 4.6 🟢 低：`context_clone_test.go` 未覆盖新平台路径

所有克隆测试均通过 `NewContext(dto.Payload, nil)` 构造（旧路径），没有通过 `AcquireContextFromEvent` 的测试，无法发现 4.1 中的 Clone Bug。

---

## 5. 项目结构合理性评估

### 5.1 整体层次结构

```
remilia/
├── platform/          ✅ 平台抽象接口层（职责清晰）
│   ├── qq/            ✅ QQ 平台实现（唯一完整实现）
│   ├── telegram/      ⚠️  仅骨架
│   ├── discord/       ⚠️  仅骨架
│   └── wechat/        ⚠️  仅骨架
├── core/
│   ├── engine/        ✅ 事件引擎（已支持平台无关路径）
│   ├── context/       ⚠️  仍导入 QQ SDK（见 5.2）
│   └── permission/    ✅ 独立权限模型
├── plugin/            ✅ 插件框架 SDK
├── plugins/           ✅ 内置插件实现
├── infra/             ✅ 基础设施（logger、metrics、tracing 等）
├── helper/            ⚠️  残留大量 QQ 专属 Deprecated 函数
├── middleware/        ✅ 中间件层
├── command/           ✅ 命令解析器
├── config/            ✅ 配置管理
└── errutil/           ⚠️  残留 Deprecated 函数
```

### 5.2 问题：`core/context` 仍依赖 QQ SDK

`core/context/context.go` 导入了：
```go
"github.com/KomeiDiSanXian/remilia/platform/qq/openapi"
"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
```

`Context` 结构体包含 `event *dto.Payload` 和 `api openapi.OpenAPI` 字段，本质上是 QQ 专属字段混入了 core 层。

**建议**：长期目标是将旧路径完全迁移后，从 `core/context` 中删除这两个导入与字段。在此之前，可以将相关字段和方法隔离到 `core/context/legacy_qq.go`，明确标注为过渡代码。

### 5.3 问题：`plugin/` 与 `plugins/` 命名易混淆

| 目录 | 实际用途 |
|------|---------|
| `plugin/` | 插件系统框架 SDK（Manager、Container、Registry 等） |
| `plugins/` | 内置插件实现（help、cooldown、scheduler 等） |

两个目录名称太相近，新贡献者容易混淆。建议：
- `plugin/` → 保持不变（与 Go 生态习惯一致）
- `plugins/` → 考虑改为 `builtin/` 或 `stdplugins/`，语义更清晰

### 5.4 问题：`factory.go` 文件价值低

`factory.go` 仅 21 行，只有一个 `NewBotWithDefault` 函数，该函数只是 `BotBuilder` 的简单包装。建议合并至 `bot_builder.go`。

### 5.5 `infra/dlq` 设计割裂

DLQ 模块同时存在：
- `DeadLetterItem`（绑定 `*dto.Payload`，Deprecated）
- `Item[T]`（泛型，新设计）

但泛型版本没有对应的 Consumer 接口、序列化函数和实现。整个模块处于"一半新一半旧"的过渡状态，需要完成迁移。

### 5.6 结构建议总结

| 问题 | 建议 | 优先级 |
|------|------|--------|
| `core/context` 导入 QQ SDK | 将旧路径字段隔离到 `legacy_qq.go` | 中 |
| `plugins/` 与 `plugin/` 命名混淆 | 考虑重命名 `plugins/` → `builtin/` | 低 |
| `factory.go` 过于简单 | 合并到 `bot_builder.go` | 低 |
| `helper/parse.go` 全 Deprecated | 直接删除整个文件 | 高 |
| `infra/dlq` 迁移未完成 | 补全泛型 Consumer 接口和序列化函数 | 中 |

---

## 6. 改进建议优先级总表

| 优先级 | 类别 | 具体内容 | 位置 |
|--------|------|---------|------|
| 🔴 P0 | Bug | `Context.Clone()` 不复制 `platformEvent`/`platformSender` | `core/context/context.go` |
| 🔴 P0 | Bug | `Registry.StartAll()` 静默丢弃适配器错误 | `platform/adapter.go` |
| 🔴 P0 | 删除 | 删除 `helper/parse.go` 整个文件 | `helper/parse.go` |
| 🟡 P1 | Bug | `qq.Adapter.wg` 未使用，`Stop()` 无法等待事件处理完成 | `platform/qq/adapter.go` |
| 🟡 P1 | Bug | `NewBot()` 中 `platformRegistry` 生命周期注册时序问题 | `bot.go` |
| 🟡 P1 | 删除 | 删除 `errutil.WrapErrorf` / `WrapErrorWithContextf` | `errutil/wrapper.go` |
| 🟡 P1 | 删除 | 删除 `pprof.StartPprofServer` | `pprof.go` |
| 🟡 P1 | 删除 | 删除 `helper.ParseEvent[T]` | `helper/helper.go` |
| 🟡 P1 | 删除 | 删除 `engine.getMatchersForEvent` | `core/engine/process.go` |
| 🟡 P1 | 删除 | 删除 `KafkaConsumer`（未实现占位）| `infra/dlq/consumers.go` |
| 🟡 P1 | 删除 | 删除 `help.SetPluginManager` | `plugins/core/help/help.go` |
| 🟡 P1 | 重构 | `BotBuilder.WithAdapter` 与 `WithPlatformAdapter` 二选一 | `bot_builder.go` |
| 🟢 P2 | 未实现 | 完成 DLQ `Item[platform.Event]` 的 Consumer 接口和序列化 | `infra/dlq/` |
| 🟢 P2 | 测试 | 补充 `Clone()` 在新平台路径下的测试 | `core/context/` |
| 🟢 P2 | 测试 | 补充多平台 Registry 集成测试 | `platform/` |
| 🟢 P2 | 结构 | 将 `core/context` 旧路径代码隔离到 `legacy_qq.go` | `core/context/` |
| 🟢 P2 | 结构 | 合并 `factory.go` 到 `bot_builder.go` | 根包 |
| 🔵 P3 | 结构 | 考虑重命名 `plugins/` 为 `builtin/` | 项目根 |
| 🔵 P3 | 实现 | Telegram / Discord / WeChat 适配器（后续按需实现） | `platform/` |

