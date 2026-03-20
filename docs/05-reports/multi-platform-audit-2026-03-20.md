# 多平台抽象分支审计报告

> 分支：`feature/multi-platform-abstraction`
> 审计日期：2026-03-20
> 状态：进行中

---

## 一、总体进度

### 已完成 ✅

| 模块 | 说明 |
|------|------|
| `platform/` 抽象层 | `Event`、`PlatformAdapter`、`Sender`、`OutboundMessage`、`Registry` 接口定义完整 |
| `platform/qq` 完整实现 | `WebhookServerAdapter`、`Adapter`（轻量包装）、`qqSender`、`qqEvent` 均已实现 |
| `core/context` 迁移 | 完全切换到平台无关路径（`AcquireContextFromEvent`），旧 `dto.Payload` 字段已清除 |
| `core/engine` 新入口 | `ProcessPlatformEvent` 已实现，与旧 `ProcessEvent` 共用同一套路由逻辑 |
| `Bot` 多平台注册表 | `UsePlatformRegistry` / `BotBuilder.WithPlatformRegistry` 完整实现 |
| `ctx.Reply()` | 平台无关的发送方法，自动注入 `ChatInfo` 供平台 Sender 路由 |
| `platform.EventKind` 路由 | `OnEventKind()`、`OnEventType()` 支持跨平台规则匹配 |
| `builtin/` 插件解耦 | 所有内置插件已迁移到 `platform.Event`，无 `dto.*` 直接依赖 |

### 骨架占位，尚未实现 🚧

| 模块 | 说明 |
|------|------|
| `platform/discord` | `StartPlatform` 直接返回错误，Sender/Event 全为空实现 |
| `platform/telegram` | 同上 |
| `platform/wechat` | 同上 |

---

## 二、需要修复的问题（Bug / 编译错误）

### P0 — 编译错误

#### 2.1 所有 `examples/` 无法编译

**影响文件：** `examples/async-tasks/`、`basic-bot/`、`command-bot/`、`debug-subcommand-demo/`、`error-handling/`、`help-discovery/`、`metrics-monitoring/`、`middleware-example/`、`plugin-example/`、`plugin-v2-demo/`、`production-ready/`、`showcase/`（共 12 个）

**错误信息：**
```
remilia.NewBotBuilder().WithBotInfo undefined (type *remilia.BotBuilder has no field or method WithBotInfo)
```

**原因：** 平台抽象重构时 `BotBuilder.WithBotInfo()` 和 `BotBuilder.WithWebhook()` 被删除，但所有示例未同步更新。

**修复方法：** 将旧写法改为新的 `WithPlatformAdapter` 方式：

```go
// 旧（已删除）
bot, err := remilia.NewBotBuilder().
    WithBotInfo(botInfo).
    WithWebhook(":8080").
    Build()

// 新（正确）
adapter := qq.NewWebhookServerAdapter(":8080", botInfo)
bot, err := remilia.NewBotBuilder().
    WithPlatformAdapter(adapter).
    Build()
```

同时，示例中的命令注册也需更新（见 2.4）。

---

### P1 — 逻辑问题

#### 2.2 `core/context/state.go` 存在孤立错误变量 `ErrNilAPI`

**文件：** `core/context/state.go` 第 19–20 行

```go
// ErrNilAPI 表示 OpenAPI 未初始化
var ErrNilAPI = errors.New("openAPI is nil")
```

**问题：** 该变量在整个代码库中没有任何引用（通过全局搜索确认），是旧版 `context.Context` 持有 `openapi.OpenAPI` 字段时的遗留产物。平台抽象完成后此字段已被移除，但错误变量未清理。

**修复：** 直接删除。

---

#### 2.3 `platform/adapter.go` `StartAll` 错误被吞掉

**文件：** `platform/adapter.go` `Registry.StartAll`

```go
wg.Go(func() {
    if err := a.StartPlatform(ctx, handler); err != nil {
        logger.WithFields(...).Error(...)  // 只记录日志
    }
})
// ...
return nil  // 永远返回 nil
```

**问题：** 每个平台适配器退出时的错误只被记录，不被收集。`StartAll` 始终返回 `nil`，调用方无法感知平台退出原因。

**建议：** 引入 `errgroup` 或手动收集错误，或至少返回第一个非 `context.Canceled` 错误。

---

### P2 — 文档/注释与实现不一致

#### 2.4 示例代码和 HelpText 中仍使用已删除/不存在的 API

**文件：**
- `builtin/cooldown/cooldown.go` 第 20 行：`ctx.Reply(dto.TextMsg(...))` → 应改为 `ctx.Reply(platform.TextMessage(...))`
- `builtin/keywordfilter/keywordfilter.go` 第 15 行：同上
- `builtin/acl/acl.go` HelpText 第 16/110/151 行：`engine.OnGroupAt(...)` → `Engine` 上不存在此方法，应改为 `engine.On(string(platform.EventKindGroupMessage), ...)`
- `builtin/antispam/antispam.go` HelpText：`engine.OnGroupAt(...)` 同上
- `builtin/keywordfilter/keywordfilter.go` HelpText：`engine.OnGroupAt(...)` 同上

#### 2.5 架构文档 `MULTI_PLATFORM.md` 接口签名有误

**文件：** `docs/03-architecture/MULTI_PLATFORM.md`

文档中展示的 `PlatformAdapter` 接口方法名为 `Start()`，但实际代码是 `StartPlatform()`：

```go
// 文档（错误）
type PlatformAdapter interface {
    Start(ctx context.Context, handler func(Event)) error
    ...
}

// 实际代码（正确）
type PlatformAdapter interface {
    StartPlatform(ctx context.Context, handler func(Event)) error
    ...
}
```

---

## 三、未实现/需要补充的内容

### 3.1 Discord / Telegram / WeChat 适配器完全未实现

三个占位适配器（`platform/discord/adapter.go`、`platform/telegram/adapter.go`、`platform/wechat/adapter.go`）内容完全相同：
- `StartPlatform` 直接 `return fmt.Errorf("... not yet implemented")`
- `discordEvent` / `telegramEvent` / `wechatEvent` 结构体所有方法返回零值
- 均无测试文件

**这是已知的待实现项**，此处仅记录当前状态，不作为错误。

---

### 3.2 根包测试 `remilia_test.go` 仍耦合 QQ 类型

**文件：** `remilia_test.go`（第 26、37、79 行）

```go
type mockAdapter struct {
    events chan *dto.Payload  // 直接依赖 QQ dto 类型
    ...
}
```

根包的测试辅助 `mockAdapter` 内部使用 `*dto.Payload` 作为事件通道类型。在多平台架构下，根包测试应使用 `platform.Event` 构造事件，而不依赖 QQ 特定类型。

**建议：** 将 `mockAdapter` 的内部事件通道改为 `chan platform.Event`，使用 `testutil.MakePlatformC2CEvent` 等辅助函数构建测试事件（该函数已在 `testutil` 包中存在）。

---

### 3.3 `testbot.Inject()` 仍强依赖 `*dto.Payload`

**文件：** `testbot/testbot.go` 第 179 行

```go
func (tb *Bot) Inject(payload *dto.Payload) {
```

`testbot` 包是供插件开发者编写测试的工具包，其 `Inject` 方法接受 `*dto.Payload`，在多平台框架下不够通用。

**建议：** 新增 `InjectEvent(event platform.Event)` 方法，`Inject(*dto.Payload)` 可保留作为 QQ 专属便捷方法（内部调用 `InjectEvent(qq.NewEvent(payload))`）。

---

### 3.4 `OutboundMessage` 缺少富媒体支持

**文件：** `platform/message.go`

当前 `OutboundMessage` 仅支持：
- `Text`（纯文本）
- `Markdown`
- `ImageURL`（图片 URL）
- `ReplyToID`（回复引用）
- `Extra`（平台扩展字段）

**缺失：**
- 语音/音频消息
- 视频消息
- 文件附件
- 按钮/交互组件（对 Discord/Telegram 尤为重要）
- At/提及用户

目前的 `Extra map[string]any` 可作为临时兜底，但长期来看需要更结构化的扩展点。

---

### 3.5 `platform/qq/event.go` 通知类事件缺少字段提取

**文件：** `platform/qq/event.go` `populate()` 方法

对于 `EventKindNotice` 类事件（`FriendAdd`、`FriendDel`、`GroupAddRobot`、`GroupDelRobot`、`GroupMsgReject` 等），`populate()` 只设置了 `kind`，没有解析 `sender`、`chat`、`content` 等字段。这导致处理通知事件时 `ctx.GetSenderInfo()` 和 `ctx.GetMessageContent()` 均返回零值。

---

### 3.6 `platform/qq/dlq` 无测试

**文件：** `platform/qq/dlq/compat.go`

该文件只有类型别名定义，无测试文件（`go test ./platform/...` 输出 `[no test files]`）。建议补充至少一个编译时类型断言测试，确保别名与原类型一致。

---

### 3.7 `CHANGELOG.md` 未记录多平台抽象工作

`CHANGELOG.md` 的 `[Unreleased]` 节为空，建议将本分支的主要变更补录进去。

---

## 四、项目结构问题

### 4.1 `factory.go` 中的 QQ 专属函数放置在根包

**文件：** `factory.go`

```go
// NewBotWithDefault 是 QQ 平台专属的便捷构造函数
func NewBotWithDefault(addr string, info *dto.BotInfo, opts ...Option) (*Bot, error) {
```

根包 (`package remilia`) 已实现了良好的平台无关抽象，但 `NewBotWithDefault` 直接依赖 `dto.BotInfo`（QQ 专属类型），破坏了这一原则。

**建议：** 将其移至 `platform/qq` 包，命名为 `qq.NewBot(addr, botInfo)` 或类似形式。根包保持平台中立。

---

### 4.2 `platform/qq/dlq/compat.go` 文件命名混淆

**文件：** `platform/qq/dlq/compat.go`

文件名 `compat.go` 通常暗示「向后兼容层」，但实际内容是为 `dlq.Queue[*dto.Payload]` 提供的类型别名。

**建议：** 重命名为 `alias.go` 或 `types.go`，使意图更清晰。

---

### 4.3 `platform/` 包测试覆盖不足

`platform/platform_test.go` 仅测试了 `OutboundMessage`、`NoopSender` 和空 `Registry`。以下场景缺少测试：
- `WithChatInfo` / `ChatInfoFromContext` 的注入与读取
- `Registry.StartAll` 的并发行为（含 ctx 取消场景）
- `Registry.StopAll` 的错误聚合

---

### 4.4 示例目录仍使用 QQ 专属注册方式

`examples/` 下的示例（修复编译错误后仍然需要更新）大量使用：
```go
eng.OnCommand(dto.C2CMessageCreate, "/ping").Handle(...)
```

在多平台框架下，示例应优先展示平台无关写法：
```go
eng.OnEventKind(platform.EventKindPrivateMessage, eventctx.OnCommand("/ping")).Handle(...)
// 或
eng.OnCommand("", "/ping").Handle(...)  // 空字符串 = 匹配所有平台
```

---

## 五、结构合理性评估

### 整体评价

```
platform/
├── adapter.go       # 接口定义 + Registry —— 合理
├── event.go         # Event 接口 + EventKind + UserInfo/ChatInfo —— 合理
├── message.go       # OutboundMessage —— 合理，但富媒体扩展性有限（见 3.4）
├── platform_test.go # 测试覆盖不足（见 4.3）
├── discord/         # 骨架 —— 正常，待实现
├── qq/              # 完整实现 —— 结构清晰
│   ├── adapter.go   # 轻量包装，适合已有 Webhook 实例的场景
│   ├── dlq/compat.go# 命名有歧义（见 4.2）
│   ├── event.go     # 通知类事件字段缺失（见 3.5）
│   ├── openapi/     # 保持独立，职责清晰
│   ├── sender.go    # 实现完整
│   └── webhook_server.go # 内置 HTTP 服务器，生命周期管理完善
├── telegram/        # 骨架
└── wechat/          # 骨架
```

**核心抽象层（`platform/`）设计合理**，接口边界清晰，`Event`/`Sender`/`PlatformAdapter` 三者职责分明。`core/context` 与 `core/engine` 的平台无关迁移完成度高。

**主要结构问题**总结：
1. `factory.go` 不应在根包暴露 QQ 专属 API（见 4.1）
2. 示例未同步更新是当前最大的可见问题（见 2.1）
3. 骨架适配器的占位错误消息过于简陋，缺少统一的 `ErrNotImplemented` 错误类型

---

## 六、待办清单（优先级排序）

| 优先级 | 任务 | 类型 |
|--------|------|------|
| 🔴 P0 | 修复所有 `examples/` 的编译错误（`WithBotInfo` → `WithPlatformAdapter`） | Bug |
| 🟠 P1 | 删除 `core/context/state.go` 中的孤立 `ErrNilAPI` 变量 | 清理 |
| 🟠 P1 | 修复 `MULTI_PLATFORM.md` 中 `Start()` → `StartPlatform()` 的接口描述错误 | 文档 |
| 🟠 P1 | 修复 `builtin/cooldown`、`builtin/keywordfilter` HelpText 中的 `dto.TextMsg` → `platform.TextMessage` | 文档 |
| 🟠 P1 | 修复 `builtin/acl`、`builtin/antispam`、`builtin/keywordfilter` HelpText 中不存在的 `engine.OnGroupAt()` | 文档 |
| 🟡 P2 | 将 `factory.go` 中 `NewBotWithDefault` 迁移到 `platform/qq` 包 | 重构 |
| 🟡 P2 | 将 `remilia_test.go` 中 `mockAdapter` 的 `chan *dto.Payload` 改为 `chan platform.Event` | 重构 |
| 🟡 P2 | 为 `testbot.Bot` 新增 `InjectEvent(platform.Event)` 方法 | 功能 |
| 🟡 P2 | 补充 `platform/adapter.go` `StartAll` 的错误收集 | 改进 |
| 🟢 P3 | 补全 `platform/qq/event.go` 对通知类事件字段的解析 | 功能 |
| 🟢 P3 | 重命名 `platform/qq/dlq/compat.go` 为 `alias.go` | 清理 |
| 🟢 P3 | 补充 `platform/` 的测试覆盖（`WithChatInfo`、`Registry.StartAll`） | 测试 |
| 🟢 P3 | 为 `platform/qq/dlq` 添加基础测试 | 测试 |
| 🟢 P3 | 更新 `examples/` 示例，统一使用平台无关注册方式 | 文档 |
| 🟢 P3 | 更新 `CHANGELOG.md`，记录多平台抽象的变更 | 文档 |
| ⚪ P4 | 评估 `OutboundMessage` 的富媒体扩展方案（音频/视频/文件/按钮） | 设计 |
| ⚪ P4 | 为占位适配器定义统一的 `ErrNotImplemented` 错误，替代硬编码字符串 | 改进 |

