# `core/context` 解耦 QQ SDK 方案

> 文档类型：架构重构方案  
> 关联审查报告：`docs/05-reports/multi-platform-audit.md` §5.2  
> 状态：**待实施**  
> 目标分支：`feature/multi-platform-abstraction`

---

## 目录

1. [问题现状](#1-问题现状)
2. [影响分析](#2-影响分析)
3. [解决方案总览](#3-解决方案总览)
4. [第一阶段：代码隔离（立即可行）](#4-第一阶段代码隔离立即可行)
5. [第二阶段：接口提取（中期重构）](#5-第二阶段接口提取中期重构)
6. [第三阶段：完全移除（长期目标）](#6-第三阶段完全移除长期目标)
7. [各文件变更清单](#7-各文件变更清单)
8. [测试策略](#8-测试策略)
9. [迁移指南（调用方）](#9-迁移指南调用方)

---

## 1. 问题现状

### 1.1 受污染的导入列表

以下 `core/context` 包内的文件目前直接导入了 QQ 平台 SDK：

| 文件 | 导入路径 | 用途 |
|------|---------|------|
| `context.go` | `platform/qq/openapi` | `openapi.OpenAPI` 字段类型 |
| `context.go` | `platform/qq/openapi/dto` | `dto.Payload`、`dto.Author`、`dto.C2CMessageCreateEvent`、`dto.GroupAtMessageCreateEvent` |
| `decode.go` | `platform/qq/openapi/dto` | `DecodeEvent()` 类型断言、`GetSenderInfo()` author 解析 |
| `pool.go` | `platform/qq/openapi` | `AcquireContext()` 参数类型 |
| `pool.go` | `platform/qq/openapi/dto` | `AcquireContext()` 参数类型 |
| `rules.go` | `platform/qq/openapi/dto` | `InGroup()` 中 `dto.GroupAtMessageCreate` 常量 |
| `platform_event.go` | `platform/qq/openapi/dto` | `GetEventKind()` 旧路径的事件类型映射 |

### 1.2 `Context` 结构体中的 QQ 专属字段

```go
// context.go — 当前状态（存在问题）
type Context struct {
    // ... 通用字段 ...

    event   *dto.Payload     // ❌ QQ 专属：原始消息包
    api     openapi.OpenAPI  // ❌ QQ 专属：QQ OpenAPI 客户端

    // decode cache（也混入了 QQ 专属类型）
    decoded decodeCache

    // hot-path cache（依赖 dto.Author）
    authorOnce sync.Once
    author     *dto.Author   // ❌ QQ 专属
}

// decodeCache — 硬编码了 QQ 事件类型
type decodeCache struct {
    kind    uint8
    c2c     dto.C2CMessageCreateEvent      // ❌
    groupAt dto.GroupAtMessageCreateEvent  // ❌
    generic any
}
```

### 1.3 QQ 专属方法

| 方法 / 函数 | 文件 | QQ 依赖点 |
|---|---|---|
| `NewContext(event *dto.Payload, api openapi.OpenAPI)` | `context.go` | 参数类型 |
| `NewContextWithContext(ctx, event, api)` | `context.go` | 参数类型 |
| `Clone()` | `context.go` | `ctx.event.Clone()` 调用 `dto.Payload.Clone()` |
| `AcquireContext(event *dto.Payload, api openapi.OpenAPI)` | `pool.go` | 参数类型 |
| `GetEvent() *dto.Payload` | `decode.go` | 返回值类型 |
| `GetEventType() dto.EventType` | `decode.go` | 返回值类型 |
| `DecodeEvent(v any)` | `decode.go` | 类型断言 `*dto.C2CMessageCreateEvent` 等 |
| `GetSenderInfo()` 旧路径 | `decode.go` | `dto.Author` 解析 |
| `GetEventKind()` 旧路径 | `platform_event.go` | `dto.C2CMessageCreate` 等常量 |
| `InGroup()` 旧路径 | `rules.go` | `dto.GroupAtMessageCreate` 常量 |

---

## 2. 影响分析

### 2.1 架构层次违反

当前依赖关系：

```
core/context  ──imports──►  platform/qq/openapi
core/context  ──imports──►  platform/qq/openapi/dto
```

期望的依赖关系：

```
core/context  ──imports──►  platform   (接口层，OK)
platform/qq   ──imports──►  platform   (实现依赖接口，OK)
```

`core/context` 是框架的核心抽象层，其职责是持有事件处理上下文并提供平台无关 API。当它直接导入 `platform/qq`，就意味着：

- **任何引用 `core/context` 的包**都会传递性地依赖 QQ SDK
- **`core/engine`、`middleware`、`plugin`、`helper` 等包**全部通过 `core/context` 传递依赖了 QQ SDK

### 2.2 横向扩展成本高

当新增 Telegram 或 Discord 适配器时，如果 `core/context` 仍绑定 QQ 类型，则：
- 新平台的 Handler 开发者如果调用 `ctx.DecodeEvent()` 会得到 QQ 特有类型，造成困惑
- `decodeCache` 的 typed union 只优化 QQ 的两种事件类型，对其他平台无效
- 测试新平台时不得不依赖 QQ SDK（即使完全不使用 QQ 功能）

### 2.3 可测试性差

所有对 `core/context` 的测试目前都必须构造 `*dto.Payload`：

```go
// 当前测试写法（被迫依赖 QQ DTO）
ctx := NewContext(&dto.Payload{
    ID:   "test-1",
    Type: dto.C2CMessageCreate,
}, nil)
```

理想状态下，测试应直接使用 `platform.Event` mock，不需要 QQ SDK 的任何导入。

---

## 3. 解决方案总览

本方案分三个阶段渐进式推进，每个阶段均可独立交付且不破坏现有功能：

```
阶段一（立即）: 代码隔离
  ↓ 将 QQ 代码物理上集中到 legacy_qq.go
  ↓ 不改变公开 API，不删除任何导入
  ↓ 提升可读性，建立"迁移线"

阶段二（中期）: 接口提取
  ↓ 用接口替换 Context 中的 QQ 具体类型
  ↓ context.go / pool.go / decode.go 不再需要 QQ 导入
  ↓ 只有 legacy_qq.go 保留 QQ 导入

阶段三（长期）: 完全移除
  ↓ 所有调用方迁移到新路径
  ↓ 删除 legacy_qq.go
  ↓ core/context 对 QQ SDK 零依赖
```

---

## 4. 第一阶段：代码隔离（立即可行）

### 4.1 目标

物理上将所有 QQ 相关代码集中到一个新文件 `core/context/legacy_qq.go`，并在文件头部明确标注其过渡性质。**此阶段不改变任何公开 API，不移除任何导入，仅重组代码。**

### 4.2 创建 `core/context/legacy_qq.go`

将以下内容从各文件移入此文件，并保留在原文件中的函数改为调用此文件中的逻辑：

```go
// legacy_qq.go
//
// ⚠️  过渡代码 — 此文件是 core/context 对 QQ SDK 的唯一依赖点。
//
// 长期目标：当所有调用方完成迁移（旧路径 AcquireContext 不再被调用），
// 本���件可整体删除，届时 core/context 将对 QQ SDK 零依赖。
//
// 迁移状态追踪：
//   - engine/process.go:ProcessEventBatch → 待迁移到 ProcessPlatformEvent
//   - 所有测试中的 NewContext(dto.Payload, ...) → 待迁移到 NewContextFromEvent
//
// 禁止：在此文件以外的 core/context/*.go 中新增任何 platform/qq 导入。

package context

import (
    "github.com/KomeiDiSanXian/remilia/platform/qq/openapi"
    "github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
    "github.com/KomeiDiSanXian/remilia/platform"
    "github.com/tidwall/gjson"
)

// ── 一、QQ 专属 Context 构造函数 ───────────────────────────────────────────

// NewContext 创建绑定 QQ dto.Payload 的 Context（旧路径）。
//
// Deprecated: 新代码请使用 NewContextFromEvent(event platform.Event, sender platform.Sender)。
// 此函数在所有调用方迁移完成后将被删除。
func NewContext(event *dto.Payload, api openapi.OpenAPI) *Context { ... }

// NewContextWithContext 创建带自定义 stdlib context 的 QQ Context（旧路径）。
//
// Deprecated: 见 NewContext。
func NewContextWithContext(ctx stdctx.Context, event *dto.Payload, api openapi.OpenAPI) *Context { ... }

// AcquireContext 从对象池获取 QQ Context（旧路径）。
//
// Deprecated: 新代码请使用 NewContextFromEvent。
func AcquireContext(event *dto.Payload, api openapi.OpenAPI) *Context { ... }

// ── 二、QQ 专属 Context 访问方法 ──────────────────────────────────────────

// GetEvent 返回 QQ 原始事件 payload（旧路径专用）。
//
// Deprecated: 新代码请使用 GetPlatformEvent()。
func (ctx *Context) GetEvent() *dto.Payload { ... }

// GetEventType 返回 QQ 事件类型字符串（旧路径返回原始类型，新路径返回 EventKind）。
//
// 注意：新旧路径返回值格式不同，不应混用。
func (ctx *Context) GetEventType() dto.EventType { ... }

// DecodeEvent 解码 QQ 事件详情（旧路径专用）。
//
// Deprecated: 新路径下通过 GetPlatformEvent() 访问强类型事件。
func (ctx *Context) DecodeEvent(v any) error { ... }

// ── 三、QQ 专属内部逻辑 ───────────────────────────────────────────────────

// qqGetMessageContent 从 dto.Payload.Detail 中 gjson 提取消息内容（旧路径）。
func qqGetMessageContent(detail []byte) string {
    return gjson.GetBytes(detail, "content").String()
}

// qqGetSenderInfo 从 dto.Payload.Detail 提取发送者信息（旧路径）。
func qqGetSenderInfo(detail []byte) platform.UserInfo { ... }

// qqClonePayload 深复制 dto.Payload（用于 Clone()）。
func qqClonePayload(p *dto.Payload) *dto.Payload {
    if p == nil {
        return nil
    }
    return p.Clone()
}

// qqMapEventKind 将 QQ dto.EventType 映射到 platform.EventKind（旧路径）。
func qqMapEventKind(t dto.EventType) platform.EventKind { ... }

// qqIsGroupAtCreate 检查是否为群消息事件（用于 InGroup 旧路径）。
func qqIsGroupAtCreate(t dto.EventType) bool {
    return t == dto.GroupAtMessageCreate
}
```

### 4.3 修改 `context.go`

从 `context.go` **移除** `platform/qq/openapi` 和 `platform/qq/openapi/dto` 的 import（转由 `legacy_qq.go` 承载）。

`Context` 结构体暂时保留 `event`、`api`、`decoded`、`author` 字段，但在字段注释中明确标注：

```go
// Context 上下文
type Context struct {
    // ...通用字段...

    // ── QQ 旧路径字段（过渡期保留，迁移完成后删除）──
    // 以下字段仅在 legacy_qq.go 中读写，其余文件禁止直接访问。
    // 删除时机：core/context/legacy_qq.go 整体删除时。
    event   *dto.Payload     // QQ 原始事件包（旧路径）
    api     openapi.OpenAPI  // QQ OpenAPI 客户端（旧路径）
    decoded decodeCache      // QQ 事件解码缓存（旧路径）
    author  *dto.Author      // QQ 发送者信息缓存（旧路径）
}
```

> **注**：此阶段由于 `Context` 结构体中仍有 QQ 类型字段，`context.go` 仍需保留 QQ 导入。**完整移除导入是第二阶段的任务。**

### 4.4 修改 `rules.go`

将 `InGroup()` 中的 QQ fallback 代码提取为一个在 `legacy_qq.go` 中定义的内部函数：

```go
// rules.go（修改后）
func InGroup(groupIDs ...string) Rule {
    // ...
    return func(ctx *Context) bool {
        if e := ctx.GetPlatformEvent(); e != nil {
            return set[e.Chat().ID]
        }
        // 旧路径：委托给 legacy_qq.go 中的函数
        return qqInGroupFallback(ctx, set)
    }
}
```

```go
// legacy_qq.go（新增）
func qqInGroupFallback(ctx *Context, set map[string]bool) bool {
    if ctx.event != nil && ctx.event.Type == dto.GroupAtMessageCreate {
        var event struct {
            GroupOpenID string `json:"group_openid"`
        }
        if err := ctx.event.Decode(&event); err == nil {
            return set[event.GroupOpenID]
        }
    }
    return false
}
```

这样 `rules.go` 就不需要直接导入 `dto`（尽管第一阶段 `context.go` 仍有此导入，但逻辑上已解耦）。

### 4.5 修改 `platform_event.go`

将 `GetEventKind()` 中的 QQ 映射表提取到 `legacy_qq.go`：

```go
// platform_event.go（修改后）
func (ctx *Context) GetEventKind() platform.EventKind {
    if ctx.platformEvent != nil {
        return ctx.platformEvent.Kind()
    }
    if ctx.event == nil {
        return platform.EventKindUnknown
    }
    return qqMapEventKind(ctx.event.Type) // 调用 legacy_qq.go
}
```

---

## 5. 第二阶段：接口提取（中期重构）

### 5.1 目标

将 `Context` 结构体中的 QQ 具体类型替换为接口，使 `context.go`、`pool.go`、`decode.go`、`rules.go`、`platform_event.go` **完全不再需要 QQ 导入**。执行后，整个 `core/context` 包对 QQ SDK 的唯一依赖点就是 `legacy_qq.go` 一个文件。

### 5.2 定义 `legacyQQAdapter` 接口

在 `context.go`（无 QQ 导入）中定义以下接口，**不引入任何 QQ 类型**：

```go
// context.go（新增接口定义）

// legacyQQAdapter 封装旧路径（QQ dto.Payload）所需的全部操作。
//
// 此接口是 core/context 与 QQ SDK 之间的唯一隔离边界。
// 实现（qqAdapterImpl）位于 legacy_qq.go，context.go 本身无需导入 QQ SDK。
//
// 接口设计原则：方法均返回平台无关类型（string、platform.UserInfo 等），
// 绝不暴露任何 dto.* 类型。
type legacyQQAdapter interface {
    // eventType 返回 QQ 原始事件类型字符串（如 "C2C_MESSAGE_CREATE"）。
    eventType() string

    // messageContent 从事件中提取消息文本内容。
    messageContent() string

    // senderInfo 提取发送者信息，转换为平台无关的 UserInfo。
    senderInfo() platform.UserInfo

    // decode 将事件详情解析到 v 指向的结构体。
    decode(v any) error

    // isGroupAt 返回此事件是否为群消息事件。
    isGroupAt() bool

    // groupOpenID 返回群组 OpenID（仅对群消息事件有效）。
    groupOpenID() string

    // cloneAdapter 深复制自身，用于 Context.Clone()。
    cloneAdapter() legacyQQAdapter
}
```

### 5.3 `Context` 结构体变更

```go
// context.go（修改后，无 QQ 导入）
type Context struct {
    ctxMu   sync.RWMutex
    ctx     stdctx.Context
    matcher Matcher

    // ── QQ 旧路径字段（接口化后，context.go 不再导入 QQ SDK）──
    legacyQQ legacyQQAdapter // nil = 新路径（platform.Event）

    // ── 平台无关字段（新路径）──
    platformEvent  platform.Event
    platformSender platform.Sender

    extInitialized atomic.Bool
    extMu          sync.Mutex
    extensions     *Extensions

    // decode cache（从结构体移入 legacyQQAdapter 实现中）
    // 旧路径的 decodeCache 不再暴露在 Context 层

    contentOnce sync.Once
    content     string
    // author 字段移入 legacyQQAdapter 实现中
}
```

> **关键点**：`decodeCache` 和 `author *dto.Author` 字段不再出现在 `Context` 结构体中。它们被内聚到 `legacy_qq.go` 的 `qqAdapterImpl` 结构体内，只有 QQ 路径才会持有这些状态。

### 5.4 `legacy_qq.go` 中的 `qqAdapterImpl` 实现

```go
// legacy_qq.go（新增实现）

// qqAdapterImpl 是 legacyQQAdapter 的唯一实现，持有全部 QQ 专属状态。
//
// 此类型只存在于 core/context 包内，不对外暴露。
// 外部只通过 legacyQQAdapter 接口访问，确保 QQ SDK 仅被 legacy_qq.go 导入。
type qqAdapterImpl struct {
    payload *dto.Payload
    api     openapi.OpenAPI

    // 内聚的缓存字段（原来散落在 Context 中）
    decodeMu sync.Mutex
    decoded  decodeCache  // decodeCache 定义也移到此文件

    authorOnce sync.Once
    author     *dto.Author
}

func (a *qqAdapterImpl) eventType() string {
    if a.payload == nil {
        return ""
    }
    return string(a.payload.Type)
}

func (a *qqAdapterImpl) messageContent() string {
    return gjson.GetBytes(a.payload.Detail, "content").String()
}

func (a *qqAdapterImpl) senderInfo() platform.UserInfo {
    a.authorOnce.Do(func() {
        res := gjson.GetBytes(a.payload.Detail, "author")
        if !res.Exists() {
            return
        }
        a.author = &dto.Author{
            ID:           res.Get("id").String(),
            MemberOpenID: res.Get("member_openid").String(),
            UnionOpenID:  res.Get("union_openid").String(),
            UserOpenID:   res.Get("user_openid").String(),
        }
    })
    if a.author == nil {
        return platform.UserInfo{}
    }
    id := a.author.UserOpenID
    if id == "" { id = a.author.MemberOpenID }
    if id == "" { id = a.author.ID }
    return platform.UserInfo{ID: id}
}

func (a *qqAdapterImpl) decode(v any) error {
    // 原 DecodeEvent 逻辑（typed union 缓存）移入此处
    a.decodeMu.Lock()
    defer a.decodeMu.Unlock()
    // ... 完整的 typed union switch-case ...
}

func (a *qqAdapterImpl) isGroupAt() bool {
    return a.payload != nil && a.payload.Type == dto.GroupAtMessageCreate
}

func (a *qqAdapterImpl) groupOpenID() string {
    var event struct {
        GroupOpenID string `json:"group_openid"`
    }
    if err := a.decode(&event); err != nil {
        return ""
    }
    return event.GroupOpenID
}

func (a *qqAdapterImpl) cloneAdapter() legacyQQAdapter {
    if a == nil {
        return nil
    }
    return &qqAdapterImpl{
        payload: a.payload.Clone(),
        api:     a.api,
        // 注意：缓存字段不复制（clone 后是新的处理链，缓存重建）
    }
}
```

### 5.5 `pool.go` 变更

```go
// pool.go（修改后，无 QQ 导入）
// NewContextFromEvent 已在 platform_event.go，无需改动

// legacy_qq.go 中新增：
func AcquireContext(event *dto.Payload, api openapi.OpenAPI) *Context {
    ctx := contextPool.Get().(*Context)
    ctx.legacyQQ = &qqAdapterImpl{payload: event, api: api}
    ctx.platformEvent = nil
    ctx.platformSender = nil
    // ... 其余 reset 逻辑 ...
    return ctx
}
```

### 5.6 `decode.go` 变更

```go
// decode.go（修改后，无 QQ 导入）

func (ctx *Context) DecodeEvent(v any) error {
    if ctx.legacyQQ == nil {
        return errors.New("DecodeEvent is only available in legacy QQ path; use GetPlatformEvent() for new path")
    }
    return ctx.legacyQQ.decode(v)
}

func (ctx *Context) GetMessageContent() string {
    if ctx.platformEvent != nil {
        ctx.contentOnce.Do(func() {
            ctx.content = ctx.platformEvent.Content()
        })
        return ctx.content
    }
    if ctx.legacyQQ != nil {
        ctx.contentOnce.Do(func() {
            ctx.content = ctx.legacyQQ.messageContent()
        })
        return ctx.content
    }
    return ""
}

func (ctx *Context) GetSenderInfo() platform.UserInfo {
    if ctx.platformEvent != nil {
        return ctx.platformEvent.Sender()
    }
    if ctx.legacyQQ != nil {
        return ctx.legacyQQ.senderInfo()
    }
    return platform.UserInfo{}
}

func (ctx *Context) GetEvent() any {
    // 注意：返回类型从 *dto.Payload 改为 any，
    // 调用方需要做类型断言。这是有意的：迫使调用方感知到
    // "这是 QQ 专属行为"，推动迁移。
    if ctx.legacyQQ == nil {
        return nil
    }
    return ctx.legacyQQ.(*qqAdapterImpl).payload  // 内部访问
}
```

> **兼容性说明**：`GetEvent()` 的返回值类型从 `*dto.Payload` 变为 `any` 是一个破坏性变更。由于项目尚未发布，这是可接受的。调用方（主要是 `core/engine/process.go`）需同步修改。

### 5.7 `rules.go` 变更

```go
// rules.go（修改后，无 QQ 导入）

func InGroup(groupIDs ...string) Rule {
    // ...
    return func(ctx *Context) bool {
        if e := ctx.GetPlatformEvent(); e != nil {
            return set[e.Chat().ID]
        }
        if ctx.legacyQQ != nil && ctx.legacyQQ.isGroupAt() {
            return set[ctx.legacyQQ.groupOpenID()]
        }
        return false
    }
}
```

### 5.8 `platform_event.go` 变更

```go
// platform_event.go（修改后，无 QQ 导入）

func (ctx *Context) GetEventKind() platform.EventKind {
    if ctx.platformEvent != nil {
        return ctx.platformEvent.Kind()
    }
    if ctx.legacyQQ != nil {
        return qqEventTypeToKind(ctx.legacyQQ.eventType()) // 在 legacy_qq.go 中定义
    }
    return platform.EventKindUnknown
}

func (ctx *Context) GetEventType() string {
    if ctx.platformEvent != nil {
        return string(ctx.platformEvent.Kind())
    }
    if ctx.legacyQQ != nil {
        return ctx.legacyQQ.eventType()
    }
    return ""
}
```

### 5.9 阶段二完成后的依赖关系图

```
core/context/context.go         ──imports──► platform  (OK)
core/context/decode.go          ──imports──► platform  (OK)
core/context/pool.go            — 无 QQ 导入
core/context/rules.go           — 无 QQ 导入
core/context/platform_event.go  ──imports──► platform  (OK)
core/context/legacy_qq.go       ──imports──► platform/qq/openapi     (唯一 QQ 依赖点)
                                ──imports──► platform/qq/openapi/dto (唯一 QQ 依赖点)
```

---

## 6. 第三阶段：完全移除（长期目标）

### 6.1 前置条件

完成以下迁移后，方可执行此阶段：

- [x] `core/engine/process.go`：`ProcessEventBatch([]*dto.Payload, openapi.OpenAPI)` 已迁移到 `ProcessPlatformEventBatch`（engine 测试全部迁移完成）
- [x] 所有依赖平台事件 ID 的测试已迁移到 mock `platform.Event`（`middleware/ratelimit_race_test.go`、`middleware/middleware_extra_test.go` "different keys"、`core/context/context_clone_test.go` 已完成）
- [x] 所有**生产代码**中的 `ctx.GetEvent()`、`ctx.DecodeEvent()` 调用已迁移到 `GetPlatformEvent()`（`builtin/dev/debug`、`core/engine/errors`、`infra/audit`、`examples/logger-demo` 已完成）
- [x] `middleware/retry.go`：`RetryWithDeadLetter` 已从 `dlq.PayloadItem` 迁移到 `dlq.PlatformEventItem`
- [x] `helper/parse.go` 已删除
- [x] `helper/helper.go` 中 `ParseEvent[T]` 已删除

> ✅ **前置条件全部满足，第三阶段可以执行。**
>
> 剩余旧路径调用（`NewContext` + `ProcessEvent`）**仅存在于测试文件**中，
> 生产代码已零旧路径依赖。删除 `legacy_qq.go` 时随之一并清理：
> - `core/context/*_test.go`（~130 处）
> - `core/engine/*_test.go`（~95 处）
> - `middleware/*_test.go` 部分测试（~43 处）
> - `tests/{benchmark,chaos,fuzzing,integration}/*_test.go`（~65 处）
> - `builtin/{antispam,conversation,i18n,stats}/*_test.go`（少量）

### 6.2 操作步骤

```
1. 删除文件  core/context/legacy_qq.go
2. 删除方法  Context.GetEvent()
3. 删除方法  Context.DecodeEvent()
4. 修改方法  Context.GetEventType()  →  仅返回 platform.EventKind 字符串
5. 删除字段  Context.legacyQQ
6. 删除函数  NewContext()
7. 删除函数  NewContextWithContext()
8. 删除函数  AcquireContext()
9. 删除方法  ReleaseContext 中与 legacyQQ 相关的清理代码
```

### 6.3 最终 `Context` 结构体（目标形态）

```go
// context.go — 最终目标形态（零 QQ 依赖）
type Context struct {
    ctxMu   sync.RWMutex
    ctx     stdctx.Context
    matcher Matcher

    // 平台无关字段（唯一来源）
    platformEvent  platform.Event
    platformSender platform.Sender

    extInitialized atomic.Bool
    extMu          sync.Mutex
    extensions     *Extensions

    contentOnce sync.Once
    content     string
}
```

---

## 7. 各文件变更清单

| 文件 | 阶段一 | 阶段二 | 阶段三 |
|------|--------|--------|--------|
| `context.go` | 添加 QQ 字段注释标注；移出逻辑到 `legacy_qq.go` | 用 `legacyQQ legacyQQAdapter` 替换具体字段；移除 QQ 导入 | 删除 `legacyQQ` 字段 |
| `decode.go` | 无变更（逻辑保持原位） | 委托给 `ctx.legacyQQ`；移除 QQ 导入 | 删除 `GetEvent()`、`DecodeEvent()` |
| `pool.go` | 无变更 | `AcquireContext` 迁移到 `legacy_qq.go`；移除 QQ 导入 | 随 `legacy_qq.go` 删除 |
| `rules.go` | `InGroup` QQ 逻辑委托给 `legacy_qq.go` 函数 | 通过接口访问，移除 QQ 导入 | 无需改动 |
| `platform_event.go` | `GetEventKind` 映射委托给 `legacy_qq.go` 函数 | 通过接口访问，移除 QQ 导入 | 无需改动 |
| **`legacy_qq.go`（新建）** | 收集所有 QQ 逻辑 | 添加 `qqAdapterImpl` 实现 | **整体删除** |

---

## 8. 测试策略

### 8.1 阶段一测试（无需新增，验证现有测试不破坏）

```bash
go test ./core/context/...
go test ./core/engine/...
```

### 8.2 阶段二测试（新增 `Context` 无 QQ 路径测试）

在 `core/context/` 新增 `legacy_qq_test.go`，测试接口正确性：

```go
// legacy_qq_test.go — 测试 qqAdapterImpl 接口实现
func TestQQAdapterImpl_EventType(t *testing.T) { ... }
func TestQQAdapterImpl_MessageContent(t *testing.T) { ... }
func TestQQAdapterImpl_IsGroupAt(t *testing.T) { ... }
func TestQQAdapterImpl_CloneAdapter(t *testing.T) { ... }
```

在 `core/context/context_test.go` 新增纯平台无关测试：

```go
// 新增：不依赖 QQ DTO 的 Context 测试
func TestNewPathContext_NoQQDependency(t *testing.T) {
    mockEvent := &mockPlatformEvent{
        kind:    platform.EventKindPrivateMessage,
        content: "hello",
    }
    ctx := NewContextFromEvent(mockEvent, nil)
    assert.Equal(t, "hello", ctx.GetMessageContent())
    assert.Equal(t, platform.EventKindPrivateMessage, ctx.GetEventKind())
}
```

### 8.3 阶段三测试（确认 QQ 导入消失）

可以使用 `go list` 或自定义 lint 规则验证：

```bash
# 验证 core/context 不再依赖 platform/qq
go list -f '{{.Imports}}' ./core/context/ | grep -v "platform/qq"
```

---

## 9. 迁移指南（调用方）

### 9.1 `AcquireContext` → `NewContextFromEvent`

**旧写法（需迁移）：**
```go
ctx := context.AcquireContext(qqPayload, qqAPI)
defer context.ReleaseContext(ctx)
```

**新写法：**
```go
ctx := context.NewContextFromEvent(platformEvent, platformSender)
defer context.ReleaseContextFromEvent(ctx)
```

如果底层仍在处理 `*dto.Payload`，需在适配器层（`platform/qq/adapter.go` 或 `platform/qq/event.go`）将其包装为 `platform.Event`，而不是将 `dto.Payload` 传入 `core/context`。

### 9.2 `ctx.GetEvent()` → `ctx.GetPlatformEvent()`

**旧写法（需迁移）：**
```go
payload := ctx.GetEvent()
if payload == nil { return }
var msg dto.C2CMessageCreateEvent
_ = ctx.DecodeEvent(&msg)
```

**新写法：**
```go
event := ctx.GetPlatformEvent()
if event == nil { return }
content := event.Content()
sender := event.Sender()
```

如果确实需要访问 QQ 专属字段（如 `GroupOpenID`），通过类型断言从 `platform.Event` 取得底层 QQ 事件：

```go
if qqEvent, ok := ctx.GetPlatformEvent().(*qq.Event); ok {
    groupID := qqEvent.GroupOpenID()
}
```

### 9.3 `ctx.DecodeEvent()` → 强类型平台事件

**旧写法：**
```go
var ev dto.GroupAtMessageCreateEvent
if err := ctx.DecodeEvent(&ev); err != nil { ... }
groupID := ev.GroupOpenID
```

**新写法：**
```go
chat := ctx.GetPlatformEvent().Chat()
groupID := chat.ID
```

### 9.4 `OnC2CMessage()` 等 QQ 规则 → `OnEventKind()`

**旧写法（QQ 专属）：**
```go
engine.On(dto.C2CMessageCreate, OnCommand("/ping")).Handle(h)
```

**新写法（多平台通用）：**
```go
engine.On(
    OnEventKind(platform.EventKindPrivateMessage),
    OnCommand("/ping"),
).Handle(h)
```

---

## 附录：各阶段预期效果

| 指标 | 当前 | 阶段一后 | 阶段二后 | 阶段三后 |
|------|------|----------|----------|----------|
| QQ SDK 导入文件数（`core/context`） | 7 个 | 7 个（集中） | **1 个**（`legacy_qq.go`） | **0 个** |
| `core/context` 包的 QQ 传递依赖 | 存在 | 存在 | 存在（仅通过 `legacy_qq.go`） | **消除** |
| 测试是否必须依赖 QQ DTO | 是 | 是 | 部分（仅旧路径测试） | **否** |
| 新平台接入成本 | 高（需了解 QQ DTO） | 高（逻辑集中但未隔离） | **低**（新路径完全独立） | **极低** |
| 可删除的代码量 | — | — | — | `legacy_qq.go`（~300 行） |
````

