# `openapi` 包迁移分析：是否应移入 `platform/qq`

> 日期：2026-03-16  
> 分析范围：`openapi/` 全部子包 × 项目所有调用方  
> **状态：✅ 已按方案 A 完成迁移（2026-03-16）**

---

## 1. 现状概览

### 1.1 `openapi` 包结构

```
openapi/
├── iface.go                  # OpenAPI 接口定义（发消息、撤消息）
├── openapi.go                # Client：HTTP POST/DELETE + token 注入
├── auth/
│   └── token/
│       └── token.go          # QQ AccessToken 定时刷新管理器
├── constant/
│   └── constant.go           # QQ 全部 API URL 常量
├── dto/
│   ├── bot.go                # BotInfo（AppID / AppSecret / Token / ServeAddr）
│   ├── builder.go            # MessageBuilder 链式构建器
│   ├── dto.go                # 空文件（占位）
│   ├── event.go              # QQ 事件类型常量 + 事件结构体
│   ├── message.go            # Message / Markdown / Ark / Media 等结构体
│   ├── payload.go            # Payload（WebSocket/Webhook 帧）+ 对象池
│   ├── pool.go               # AcquirePayload / ReleasePayload
│   └── webhook.go            # ValidationReq / ValidationRsp
├── errs/
│   ├── errs.go               # ErrCode 类型
│   └── openapi.go            # QQ 错误码常量（UnknownAccount = 10001）
├── intents/
│   ├── intents.go            # QQ 订阅意图位掩码（Guilds / GroupAndC2CEvent …）
│   └── event.go              # 意图对应的事件名常量
└── protocol/
    ├── protocol.go            # 空文件（占位）
    └── webhook/
        ├── webhook.go         # Conn：HTTP 服务器 + eventChan + BigCache 去重
        ├── sign.go            # Ed25519 签名验证 / 生成
        ├── dedup_test.go
        ├── webhook_stats_test.go
        └── webhook_test.go
```

### 1.2 `platform/qq` 包结构（现有）

```
platform/qq/
├── adapter.go    # PlatformAdapter 实现（消费 Webhook EventStream）
├── event.go      # dto.Payload → platform.Event 适配
├── event_test.go
└── sender.go     # platform.Sender 实现（桥接 openapi.OpenAPI）
```

---

## 2. 依赖关系全图

```
调用方                          依赖的 openapi 子包
─────────────────────────────────────────────────
platform/qq/adapter.go          openapi  openapi/dto
platform/qq/sender.go           openapi  openapi/dto
platform/qq/event.go                     openapi/dto
webhook_adapter.go (根包)       openapi  openapi/dto  openapi/protocol/webhook
testbot/testbot.go              openapi  openapi/dto
testutil/testutil.go            openapi  openapi/dto
tests/*                                  openapi/dto
remilia_test.go                          openapi/dto
remilia_plus_test.go                     openapi/dto
```

**内部依赖：**

```
openapi/openapi.go
  └── openapi/auth/token     (→ openapi/constant, openapi/dto)
  └── openapi/constant
  └── openapi/dto

openapi/protocol/webhook
  └── openapi/dto

openapi/auth/token
  └── config                 (← 跨包，依赖框架配置层)
  └── openapi/constant
  └── openapi/dto
```

---

## 3. QQ 专属性评估

| 子包 | 内容 | QQ 专属度 |
|------|------|-----------|
| `openapi/` | `Client`、`OpenAPI` 接口 | ★★★★★ 100% QQ API |
| `openapi/constant/` | `https://api.sgroup.qq.com/…`、`https://bots.qq.com/…` | ★★★★★ |
| `openapi/dto/` | `BotInfo`、`Payload`、`C2CMessageCreateEvent`、`GroupAtMessageCreate`… | ★★★★★ |
| `openapi/intents/` | QQ 订阅意图位掩码（`GroupAndC2CEvent = 1<<25`）| ★★★★★ |
| `openapi/errs/` | QQ 错误码（`UnknownAccount = 10001`）| ★★★★★ |
| `openapi/auth/token/` | QQ AccessToken 自动刷新（HMAC-SHA256 + QQ 接口）| ★★★★★ |
| `openapi/protocol/webhook/` | QQ Webhook Ed25519 签名验证 | ★★★★★ |

结论：**`openapi` 包及所有子包 100% 属于 QQ 官方机器人平台专属实现**，无任何通用/平台无关内容。

---

## 4. 迁移理由分析

### 4.1 支持迁移的理由

**（1）架构一致性**  
项目已建立 `platform/<name>/` 的多平台分层结构：
```
platform/
├── discord/
├── qq/         ← QQ 适配器的语义主目录
├── telegram/
└── wechat/
```
`openapi/` 独立于此体系之外，破坏了"所有平台实现都在 `platform/` 下"的约定。

**（2）`platform/qq` 已是 `openapi` 的唯一有意义使用者**  
`platform/qq` 对 `openapi` 的依赖呈**全包覆盖**：  
- `adapter.go` → `openapi` + `openapi/dto`  
- `sender.go` → `openapi` + `openapi/dto`  
- `event.go` → `openapi/dto`  

`platform/qq` 实质上是 `openapi` 的**上层封装层**，两者合并到同一目录树更符合 Go 的包内聚原则。

**（3）根包（`remilia`）中的 `WebhookServerAdapter` 本身也是 QQ 专属**  
`webhook_adapter.go` 直接导入 `openapi`、`openapi/dto`、`openapi/protocol/webhook`，并在 `Platform()` 中硬编码 `qq`。它在架构上应属于 QQ 平台层，位于根包是历史遗留，与迁移决策相互关联。

**（4）包名歧义**  
`openapi` 这个名字会被误认为是 OpenAPI 规范（Swagger），实际内容是 QQ 官方机器人 HTTP API 客户端。放在 `platform/qq/` 下可消除歧义。

**（5）符合 Go 模块可选依赖的最佳实践**  
未来若将 `platform/qq` 拆分为独立模块（`module github.com/KomeiDiSanXian/remilia/platform/qq`），其所有依赖（包括 `openapi`）可一并打包，不会污染其他平台使用者的依赖树。

---

### 4.2 反对迁移 / 需要注意的问题

**（1）破坏性的 import 路径变更（最大风险）**  
所有直接引用 `openapi/*` 的调用方均需修改导入路径：

| 调用方文件 | 变更前 | 变更后 |
|---|---|---|
| `webhook_adapter.go` | `openapi` | `platform/qq/openapi` |
| `webhook_adapter.go` | `openapi/dto` | `platform/qq/openapi/dto` |
| `webhook_adapter.go` | `openapi/protocol/webhook` | `platform/qq/openapi/protocol/webhook` |
| `testbot/testbot.go` | `openapi` | `platform/qq/openapi` |
| `testbot/testbot.go` | `openapi/dto` | `platform/qq/openapi/dto` |
| `testutil/testutil.go` | `openapi` | `platform/qq/openapi` |
| `testutil/testutil.go` | `openapi/dto` | `platform/qq/openapi/dto` |
| `tests/*` | `openapi/dto` | `platform/qq/openapi/dto` |
| `remilia_test.go` | `openapi/dto` | `platform/qq/openapi/dto` |

对于**下游外部使用者**（已将本项目作为 Go 模块依赖），此变更属于不可向后兼容的 Breaking Change，需要配合主版本号升级（`v2`）。

**（2）`testbot` / `testutil` 的语义问题**  
`testbot` 和 `testutil` 是**框架级**的测试辅助包，如果它们需要导入 `platform/qq/openapi`，则隐式声明"框架测试工具默认依赖 QQ 平台"，这与其通用定位相悖。  
→ 解决方案：将 `testbot` / `testutil` 中的 `MockAPI` 和 `mockAPI` 移入 `platform/qq` 内部，或通过接口注入而非依赖具体类型。

**（3）`dto` 包被广泛引用**  
`openapi/dto` 是引用最广的子包，连框架根包的测试文件（`remilia_test.go`、`remilia_plus_test.go`）也直接依赖它。路径变更的影响面最大，需要专项评估。

---

## 5. 推荐方案

### 方案 A：完整迁移（推荐）

将 `openapi/` 整体移动为 `platform/qq/openapi/`：

```
platform/qq/
├── openapi/
│   ├── iface.go
│   ├── openapi.go
│   ├── auth/token/
│   ├── constant/
│   ├── dto/
│   ├── errs/
│   ├── intents/
│   └── protocol/webhook/
├── adapter.go
├── event.go
└── sender.go
```

**迁移步骤：**
1. 物理移动文件，更新所有 `package` 声明为原有包名（仅路径变化，包名不变）
2. 全量替换 import 路径（`sed` / `gofmt` / IDE 重构）
3. 将 `WebhookServerAdapter` 从根包移入 `platform/qq/`（或在根包保留薄包装层）
4. 将 `testbot.MockAPI` / `testutil.mockAPI` 的 QQ 专属部分迁移至 `platform/qq/testhelper/`
5. 更新模块 Major Version（若有外部用户）
6. 全量运行测试

**收益：**
- `platform/qq` 完全自包含，无外部 QQ 专属泄漏
- 根包彻底与 QQ 数据结构解耦
- 架构语义清晰，新平台接入参考路径明确

---

### 方案 B：渐进式迁移（低风险过渡）

保留 `openapi/` 原路径，在 `platform/qq/openapi/` 创建**类型别名转发层**：

```go
// platform/qq/openapi/dto/alias.go
package dto

import origin "github.com/KomeiDiSanXian/remilia/openapi/dto"

type (
    BotInfo                  = origin.BotInfo
    Payload                  = origin.Payload
    Message                  = origin.Message
    C2CMessageCreateEvent    = origin.C2CMessageCreateEvent
    // ...
)
```

**优点：** 无破坏性变更，外部调用方路径不变  
**缺点：** 存在两个"真相来源"，长期维护负担重，仅适合作为临时过渡

---

### 方案 C：不迁移，仅重命名（最小变更）

将 `openapi/` 重命名为 `qqapi/` 或 `qqopenapi/`，保持顶层位置：

```
remilia/
├── qqapi/          ← 原 openapi/
│   ├── dto/
│   └── ...
└── platform/qq/
```

**优点：** 消除包名歧义，路径变更量与方案 A 相同，但无需调整目录层次  
**缺点：** 仍然违反"QQ 专属代码在 platform/qq 下"的架构原则，只解决了命名问题

---

## 6. 综合结论

| 维度 | 评价 |
|------|------|
| QQ 专属性 | 100%，无争议 |
| 架构一致性 | 强烈建议迁移 |
| 迁移代价 | 中等（项目内全量路径替换，约 20 处文件变更） |
| 对外部用户影响 | Breaking Change，需 Major Version |
| 最终建议 | **推荐方案 A**，在下一个 Major Version 窗口执行完整迁移 |

> **核心结论：`openapi` 包在架构上确实应当归属于 `platform/qq`。**  
> 它不是框架核心 API，而是 QQ 官方机器人平台的专属 SDK 实现。  
> 将其置于 `platform/qq/openapi/` 既符合项目既有分层约定，也使 `platform/qq` 成为真正自包含的 QQ 平台适配单元。  
> 建议配合 `WebhookServerAdapter` 的同步迁移，彻底清理根包中的 QQ 专属引用。

---

## 7. 实际迁移执行记录（2026-03-16）

### 执行步骤

| # | 操作 | 说明 |
|---|------|------|
| 1 | `Copy-Item openapi/ → platform/qq/openapi/` | 递归复制整个目录树 |
| 2 | 全量 import 路径替换（118 个文件） | PowerShell 正则，最长路径优先替换，零遗漏 |
| 3 | `Remove-Item openapi/` | 删除原目录 |
| 4 | 创建 `platform/qq/webhook_server.go` | 将 `WebhookServerAdapter` 从根包 `remilia` 迁入 `package qq`，移除 `qqplatform` 自引用，复用 `safeInvoke` |
| 5 | 删除 `webhook_adapter.go`（根包） | 根包彻底移除 QQ 专属 HTTP 适配器 |
| 6 | 更新 `bot_builder.go` | 添加 `qqplatform` 导入，调用 `qqplatform.NewWebhookServerAdapter` |
| 7 | 更新 `examples/config-integration/main.go` | 改用 `qq.NewWebhookServerAdapterWithConfig` |
| 8 | 创建 `platform/qq/webhook_server_test.go` | 将 4 个 `WebhookServerAdapter` 测试迁入 `package qq`（需访问私有字段） |
| 9 | 清理 `remilia_plus_test.go` | 删除已迁走的测试，移除 `config`/`platform` 多余导入 |
| 10 | 更新 `bot_builder_test.go`、`bot_manager_test.go` | `remilia.SimpleWebhookAdapter` → `qq.SimpleWebhookAdapter` |

### 结果

```
go build ./...  → 无错误
go vet ./...    → 无警告
go test ./...   → 全部 ok，零 FAIL
```

### 最终目录结构

```
platform/qq/
├── adapter.go               # PlatformAdapter（消费 Webhook EventStream）
├── event.go                 # dto.Payload → platform.Event
├── event_test.go
├── sender.go                # platform.Sender（桥接 openapi.OpenAPI）
├── webhook_server.go        # ★ 从根包迁入：WebhookServerAdapter
├── webhook_server_test.go   # ★ 新增：WebhookServerAdapter 测试
└── openapi/                 # ★ 从顶层 openapi/ 迁入
    ├── iface.go             # OpenAPI 接口
    ├── openapi.go           # Client 实现
    ├── auth/token/          # QQ AccessToken 管理器
    ├── constant/            # QQ API URL 常量
    ├── dto/                 # QQ 数据结构（Payload、Message、BotInfo 等）
    ├── errs/                # QQ 错误码
    ├── intents/             # QQ 订阅意图位掩码
    └── protocol/webhook/    # QQ Webhook HTTP 服务器 + 签名验证
```

