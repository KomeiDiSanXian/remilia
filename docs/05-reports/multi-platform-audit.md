# 多平台抽象分支审计报告

> 分支：`feature/multi-platform-abstraction`  
> 审计日期：2026-03-19  
> 状态：**P0 Critical / P1 / P2 架构问题均已修复，P3（Discord/Telegram/WeChat 实现、Kafka）待后续迭代**

---

## 目录

1. [整体进度概览](#1-整体进度概览)
2. [Critical Bug（必须修复）](#2-critical-bug必须修复)
3. [架构 / 设计问题](#3-架构--设计问题)
4. [未实现内容](#4-未实现内容)
5. [可删除的 Deprecated 符号](#5-可删除的-deprecated-符号)
6. [项目结构评估](#6-项目结构评估)
7. [优先级建议](#7-优先级建议)

---

## 1. 整体进度概览

| 模块 | 状态 | 说明 |
|------|------|------|
| `platform/` 接口定义 | ✅ 完成 | `PlatformAdapter`、`Event`、`Sender`、`Registry` 均已定义 |
| `platform/qq/` 适配器 | ✅ 完成 | Webhook 服务器适配器、事件包装、消息发送器均已实现 |
| `platform/discord/` | 🚧 骨架 | `StartPlatform` / `Send` 返回 "not yet implemented" |
| `platform/telegram/` | 🚧 骨架 | 同上 |
| `platform/wechat/` | 🚧 骨架 | 同上 |
| `core/engine` 平台无关事件处理 | ✅ 完成 | `ProcessPlatformEvent` / `processEventContext` 已实现 |
| `core/context` 平台无关 Context | ✅ 完成 | `AcquireContextFromEvent`、`Reply`、`GetEventKind` 等已实现 |
| `Bot` 多平台注册表支持 | ✅ 完成 | `UsePlatformRegistry` / `WithPlatformRegistry` 已实现 |
| `testutil` 平台无关测试工具 | ✅ 完成 | `SendPlatformC2C` / `SendPlatformGroupAt` / `SendPlatformEvent` 已实现 |
| DLQ 平台无关类型 | ✅ 完成 | `PlatformEventQueue` / `PlatformFileConsumer` 等已实现 |
| DLQ Kafka 后端 | ❌ 未实现 | validate.go 中显式阻止并报错 |
| SQLite Storage Demo 示例 | ❌ 损坏 | 整个 `main()` 被注释掉（FIXME 标记） |

---

## 2. Critical Bug（必须修复）

### BUG-01：`platform.Registry.StartAll` — 缺少 `wg.Wait()`，goroutine 泄漏

> ✅ **已修复** (`platform/adapter.go`)

**文件**：`platform/adapter.go:120-140`

```go
// 当前实现（有问题）
func (r *Registry) StartAll(ctx stdctx.Context, handler func(Event)) error {
    var wg sync.WaitGroup
    for _, a := range adapters {
        wg.Go(func() {
            a.StartPlatform(ctx, handler)
        })
    }
    <-ctx.Done()
    return nil   // ← BUG：未调用 wg.Wait()，goroutine 仍在运行时函数已返回
}
```

**影响**：ctx 取消后函数立即返回，但各平台适配器的 goroutine 可能仍在执行关闭逻辑，导致 goroutine 泄漏。

**修复**：在 `return nil` 前追加 `wg.Wait()`。

---

### BUG-02：`qq.Adapter.StartPlatform` — handler 同步调用，事件循环阻塞

> ✅ **已修复** (`platform/qq/adapter.go`)

**文件**：`platform/qq/adapter.go:103-110`

```go
// 当前实现（有问题）
if payload != nil {
    event := NewEvent(payload)
    a.wg.Add(1)
    safeInvoke(handler, event)   // ← 同步调用：事件循环阻塞到 handler 返回
    a.wg.Done()
}
```

**影响**：每个事件处理完成前，后续事件无法从 `eventCh` 读取，形成串行瓶颈。  
`wg.Add(1)/Done()` 包裹同步调用在语义上是正确的（用于关闭时等待），但未能实现并发处理。

**修复**：将 handler 分发到独立 goroutine，或记录此为"有意的串行化"并补充注释。

```go
// 推荐修复（并发处理）
if payload != nil {
    event := NewEvent(payload)
    a.wg.Add(1)
    go func() {
        defer a.wg.Done()
        safeInvoke(handler, event)
    }()
}
```

---

### BUG-03：`factory.go::NewBotWithDefault` — 函数逻辑损坏，始终返回 `ErrAdapterRequired`

> ✅ **已修复** (`factory.go`) — 采用方案 A：补充 `addr string` 参数

**文件**：`factory.go`

```go
// 注释说"等价于 NewBotBuilder().WithBotInfo(info).WithWebhook(addr).Build()"
// 但函数签名没有 addr 参数，Build() 因 adapter==nil 失败
func NewBotWithDefault(info *dto.BotInfo, opts ...Option) (*Bot, error) {
    b := NewBotBuilder().WithBotInfo(info) // 无 WithWebhook，Build 必然报 ErrAdapterRequired
    ...
    return b.Build()
}
```

**影响**：此函数任何情况下都会返回 `ErrAdapterRequired` 错误，完全无法使用。

**修复选项**：
- **方案 A**：补充 `addr` 参数：`NewBotWithDefault(addr string, info *dto.BotInfo, opts ...Option)`
- **方案 B**：删除此函数（项目未发布，可直接移除；用户应使用 `BotBuilder`）

---

### BUG-04：`bot.go::Start()` — 热重启时平台组件重复注册

> ✅ **已修复** (`bot.go`) — 提取 `buildBaseLifecycle()` 辅助方法，每次 `Start()` 重建 lifecycle

**文件**：`bot.go:232-245`

```go
// 每次 Start() 都追加注册 lifecycle 组件，Stop() 后再 Start() 会重复注册
if reg != nil {
    for _, pa := range reg.All() {
        b.lifecycle.Register(...)  // 重复追加，不去重
    }
}
```

**影响**：`Stop()` 后调用 `Start()`（热重启）时，lifecycle manager 中存在重复的平台组件，导致同一适配器被启动多次。

**修复**：
- 每次 `Start()` 时重置 lifecycle manager，或
- 在注册前检查是否已存在同名组件。

---

## 3. 架构 / 设计问题

### ARCH-01：根包对 QQ 平台硬依赖，违反多平台设计原则

**涉及文件**：`bot.go`、`bot_builder.go`、`factory.go`、`doc.go`

这四个文件在根包 (`remilia`) 中直接导入：
- `platform/qq/openapi`
- `platform/qq/openapi/auth/token`
- `platform/qq/openapi/dto`

对于声称"平台无关"的框架根包来说，这是一个严重的耦合问题。使用非 QQ 平台的开发者被迫拉取 QQ 专属依赖。

**建议**：
- 将 `bot.go` 中的 `tokenManager *token.Manager`、`openAPI openapi.OpenAPI`、`botInfo *dto.BotInfo` 字段和相关逻辑完全移到 `platform/qq/` 包内，通过 `Option` 或 `BotBuilder` 选项注入。
- 根包构造函数应只依赖 `platform.PlatformAdapter` 接口，不引用任何具体平台类型。

---

### ARCH-02：`doc.go` 包文档存在误导性示例

**文件**：`doc.go:62-67`

```go
// 文档宣称可以链式调用 WithPlatformAdapter 连接多平台
bot, err := remilia.NewBotBuilder().
    WithPlatformAdapter(qqAdapter).
    WithPlatformAdapter(discordAdapter).  // ← 实际会覆盖 qqAdapter！
    Build()
```

实际实现中 `BotBuilder.adapter` 是单字段，后一次调用覆盖前一次。多平台必须使用 `WithPlatformRegistry`。

**修复**：更新文档示例，改用 `WithPlatformRegistry`。

---

### ARCH-03：`engine.PlatformAdapter` 与 `platform.PlatformAdapter` 重复定义

**文件**：`core/engine/types.go:100-112` 与 `platform/adapter.go:52-76`

两个接口的方法签名完全相同，但定义在不同包中。这导致：
- 实现者需要同时满足两个接口（虽然 Go 结构类型系统下自动满足）
- 维护时需要同步两处定义

**建议**：`core/engine` 中的 `PlatformAdapter` 改为 `= platform.PlatformAdapter` 的类型别名，或直接删除，统一使用 `platform.PlatformAdapter`。

---

### ARCH-04：`platform.Registry.StopAll` 在生产代码中从未被调用

**文件**：`platform/adapter.go:142-156`

`StopAll` 方法定义了，但 Bot 的关闭流程是通过 lifecycle manager 逐一调用 `pa.Stop(ctx)` 完成的，`Registry.StopAll` 从未在生产路径上被调用。

**影响**：该方法是死代码（仅被测试调用），产生误导——开发者可能误以为调用 `registry.StopAll` 就能完成关闭。

**建议**：要么在 Bot 关闭流程中实际调用它（替代 lifecycle 中的逐个 Stop），要么在文档中明确标注此方法仅供外部直接使用 Registry 的场景。

---

### ARCH-05：`testbot/` 与 `testutil/` 功能重叠

两个包均提供：
- Mock API / Mock Sender
- 模拟事件注入
- 断言辅助

`testutil/` 更新、支持平台无关路径，`testbot/` 则保留了更多 QQ 特定 API。

**建议**：合并为单一测试工具包，消除重复维护负担。

---

### ARCH-06：`infra/dlq/compat.go` 中混入 QQ 特定类型

**文件**：`infra/dlq/compat.go`

`infra/` 层定位为平台无关基础设施，但 `compat.go` 引入了 `platform/qq/openapi/dto` 并导出 `PayloadQueue = Queue[*dto.Payload]` 等 QQ 特定类型别名。

**建议**：将 `PayloadQueue` 等 QQ 相关别名移至 `platform/qq/` 包（如 `platform/qq/dlq/compat.go`），保持 `infra/dlq/` 的平台无关性。

---

## 4. 未实现内容

### TODO-01：Discord / Telegram / WeChat 适配器 — 仅骨架

| 文件 | 未实现的内容 |
|------|-------------|
| `platform/discord/adapter.go` | `StartPlatform` 返回错误；`Send` 返回错误；`discordEvent` 只返回零值 |
| `platform/telegram/adapter.go` | 同上 |
| `platform/wechat/adapter.go` | 同上 |

**说明**：已知后续实现，本报告记录状态，不要求立即修复。
骨架文件本身结构合理（实现了 `platform.PlatformAdapter` 接口），可作为贡献指引。

**建议**：为每个骨架文件添加 `CONTRIBUTING.md` 或在 README 中说明贡献方式。

---

### TODO-02：Kafka DLQ 后端 — 显式阻止，功能不完整

**文件**：`config/validate.go:191`

```go
case "kafka":
    return fmt.Errorf("dead_letter.target 'kafka' is not yet implemented; ...")
```

验证层直接拒绝 Kafka 配置，导致用户无法启用。

**建议**：补充 Kafka consumer 实现（`infra/dlq/consumers.go` 中已有 `PlatformFileConsumer` / `PlatformWebhookConsumer` 可参考），或将 `kafka` 从 `validTargets` 中移除，避免用户尝试配置一个不存在的选项。

---

### TODO-03：SQLite Storage Demo — main() 完全注释

> ✅ **已修复** (`examples/sqlite-storage-demo/main.go`, `builtin/core/storage/storage.go`)

**文件**：`examples/sqlite-storage-demo/main.go`

**根本原因**：旧代码使用 `storage.NewWithBackend(sqliteStorage)` 并将返回值赋给 `storagePlugin`，
但此函数返回 `*plugin.PluginDescriptor`（非 `*Plugin`），导致无法直接调用 `storagePlugin.Set()` 等方法。

**修复**：
1. 在 `builtin/core/storage/storage.go` 中新增 `NewPlugin(s Storage) *Plugin` 直接构造函数，
   供不需要 lifecycle 系统的场景（独立 demo、单元测试）使用。
2. 重写 `main.go`：移除 engine/plugin.Manager，改用 `storage.NewPlugin(sqliteStorage)` 直接构造插件，
   同时补全所有错误处理。

---

## 5. 可删除的 Deprecated 符号

项目尚未发布（pre-release），以下符号已标记为 Deprecated，且内部已迁移到新路径，可直接删除：

### DEP-01：`testutil.TestBot.SendC2C`

**文件**：`testutil/testutil.go:222`

```go
// Deprecated: prefer SendPlatformC2C which uses the platform-agnostic path.
func (tb *TestBot) SendC2C(userOpenID, content string) *PlatformResponse { ... }
```

内部实现已通过 `dispatch` → `qqplatform.NewEvent` → `ProcessPlatformEvent` 走新路径，Deprecated 标记说明新路径 `SendPlatformC2C` 已覆盖其功能。

**动作**：删除 `SendC2C`。

---

### DEP-02：`testutil.TestBot.SendGroupAt`

**文件**：`testutil/testutil.go:232`

```go
// Deprecated: prefer SendPlatformGroupAt which uses the platform-agnostic path.
func (tb *TestBot) SendGroupAt(userOpenID, groupOpenID, content string) *PlatformResponse { ... }
```

**动作**：删除 `SendGroupAt`。

**注意**：删除上述两个方法后，`testutil/testutil.go` 中对 `platform/qq/openapi/dto`、`platform/qq/openapi` 的导入可能变为未使用——需同步清理导入，进一步降低 `testutil` 对 QQ 的依赖。

---

## 6. 项目结构评估

### 现有结构（摘要）

```
remilia/
├── bot.go / bot_builder.go / factory.go   ← 根包（QQ 硬依赖待清理）
├── platform/
│   ├── adapter.go / event.go / message.go ← 接口定义 ✅
│   ├── qq/                                ← QQ 完整实现 ✅
│   ├── discord/                           ← 骨架 🚧
│   ├── telegram/                          ← 骨架 🚧
│   └── wechat/                            ← 骨架 🚧
├── core/
│   ├── engine/                            ← 平台无关事件引擎 ✅
│   ├── context/                           ← 平台无关 Context ✅
│   └── permission/                        ← RBAC 系统 ✅
├── builtin/                               ← 内置插件
├── infra/                                 ← 基础设施（部分有 QQ 混入，见 ARCH-06）
├── plugin/                                ← 插件系统 ✅
├── testutil/                              ← 测试工具（含 deprecated 方法）
└── testbot/                               ← 重叠的测试工具（见 ARCH-05）
```

### 评估

**合理之处**：
- `platform/` 独立层次清晰，接口定义与实现分离得当
- `core/engine` 通过 `processEventContext` 实现了新旧路径的代码复用，零重复
- `platform/qq/` 的 `WebhookServerAdapter`、`Adapter`（轻量包装）、`Sender` 职责划分清晰
- `plugin/` 的依赖注入、拓扑排序、热重载设计完善

**需要调整之处**：

| 问题 | 建议 |
|------|------|
| 根包导入 QQ 特定包 | 将 `token.Manager`、`openapi.OpenAPI`、`dto.BotInfo` 的逻辑下沉到 `platform/qq/` 或通过接口注入 |
| `engine.PlatformAdapter` 与 `platform.PlatformAdapter` 重复 | 删除 `engine` 包中的重复定义，统一用 `platform.PlatformAdapter` |
| Discord/Telegram/WeChat 骨架位置 | 当前位置（`platform/discord/` 等）合理；若希望明确区分"待实现"，可加 `_PLACEHOLDER` 注释或在 README 中标注 |
| `testbot/` 与 `testutil/` 重叠 | 合并，保留 `testutil/` 作为唯一测试工具包 |
| `infra/dlq/compat.go` QQ 类型 | 移至 `platform/qq/dlq/` 或 `platform/qq/compat.go` |

---

## 7. 优先级建议

### P0 — 阻塞合并

| ID | 描述 | 状态 |
|----|------|------|
| BUG-01 | `Registry.StartAll` 缺少 `wg.Wait()` | ✅ 已修复 |
| BUG-03 | `NewBotWithDefault` 函数损坏，始终返回错误 | ✅ 已修复 |
| ARCH-02 | `doc.go` 多平台示例错误（链式 `WithPlatformAdapter` 无效） | ✅ 已修复 |

### P1 — 应在发布前修复

| ID | 描述 | 状态 |
|----|------|------|
| BUG-02 | `qq.Adapter` 事件循环阻塞问题 | ✅ 已修复 |
| BUG-04 | 热重启时 lifecycle 组件重复注册 | ✅ 已修复 |
| DEP-01/02 | 删除 `testutil.SendC2C` 和 `testutil.SendGroupAt` | ✅ 已删除 |
| TODO-03 | 修复或删除 `examples/sqlite-storage-demo` | ✅ 已修复 |

### P2 — 架构改善（建议在 v1.0 前完成）

| ID | 描述 | 状态 |
|----|------|------|
| ARCH-01 | 根包去除 QQ 硬依赖 | ✅ 已修复：从 `bot.go` 移除 QQ 导入；引入 `startHook` 机制；`BotBuilder.WithBotInfo` 通过闭包注入 token 生命周期并调用 `adapter.WithAPI(api)` |
| ARCH-02 | `doc.go` 多平台示例错误 | ✅ 已修复 |
| ARCH-03 | 合并重复的 `PlatformAdapter` 接口定义 | ✅ 已修复：`engine.PlatformAdapter` 已改为 `= platform.PlatformAdapter` 类型别名 |
| ARCH-04 | 明确 `Registry.StopAll` 使用场景 | ✅ 已修复：补充文档说明其仅适用于外部直接使用 Registry 的场景 |
| ARCH-05 | 合并 `testbot/` 与 `testutil/` | ✅ 已修复：`testutil` 新增无 TB 的 `Bot` struct；`testbot.Bot` 嵌入 `testutil.Bot`，`MockAPI` 移至 `testbot` 并补全 `Sent`/`LastSent`/`Clear` 方法；`testutil` 彻底去除 QQ 依赖 |
| ARCH-06 | 将 `infra/dlq/compat.go` 中的 QQ 类型移出 | ✅ 已修复：QQ DLQ 类型别名移至 `platform/qq/dlq/compat.go`；`infra/dlq` 彻底平台无关；`infra/health` 测试改用 `Queue[int]` |

### P3 — 后续迭代

| ID | 描述 |
|----|------|
| TODO-01 | Discord / Telegram / WeChat 适配器实现 |
| TODO-02 | Kafka DLQ 后端实现 |

