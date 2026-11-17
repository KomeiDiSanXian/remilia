# 33 — AI 上下文通道：从"字符串后缀"到"语义分道"

> 本文是 `builtin/ai` 上下文体系的**新一轮迭代**的设计文档，接替
> [26](26-ai-layering.md) / [27](27-ai-freeze-checklist.md) 中与 Prompt 前缀、
> 动态上下文相关的部分。26–29 记录的是上一轮"分层与分包"重构，其中
> **§D2/D3/D5 与"没有真实消费者不抽象"已在本轮作废**，其余契约（A/B/C/E/F/G
> 组、分包依赖方向）继续有效。
>
> 状态：`P0` 已实现（窗口裁剪时机）；`P1`–`P3` 待实施。

## 一、问题重述

上一轮的结论是"动态上下文只挂在最后一条 user 消息尾部，不写回历史"
（27 §D2/D3）。它解决了**污染问题**：

- 动态内容不进 System，避免每轮前缀从最前面失效；
- 动态内容不写回会话，避免旧时间/旧群状态长期占用上下文。

但它留下了**两个未解的成本**，本轮直接以它们为目标：

| 成本 | 现状 | 后果 |
|---|---|---|
| 上一轮问答的尾部永久不可复用 | `user_N` 的内容含 `dyn_N`，下一轮 `user_N` 字节已变 | 每轮只保住"上一轮之前"，实测命中率天花板 ≈76% |
| 框架事实与用户发言混在同一槽位 | 日期/平台/星期写成 `===== 运行时上下文 =====` 拼进 user | 模型无法区分"用户说的"与"框架提供的"，也不便按厂商协议投递 |

因此本轮要回答的问题不是"要不要优雅"，而是：**哪些上下文可以随历史回放，
哪些只能属于当前回合？**

## 二、核心决策：按"可回放性"分道，而不是按厂商协议分道

### 2.1 两个通道

```go
// builtin/ai/promptctx
type Channel int

const (
	// Persistent 随会话历史持久化并逐轮原样回传。
	Persistent Channel = iota
	// Ephemeral 只属于当前回合，永不写回会话。
	Ephemeral
)
```

判据只有一条：**回放这条内容，会不会给模型新的指令权限？**

- 框架掌握的事实（时间、时区、星期、平台、会话类型）回放不改变权限 → `Persistent`；
- 群聊窗口、长期记忆、RAG 命中、执行计划、反思/校验指令来自任意成员或被模型
  抽取 → 回放等于把不可信内容永久钉成历史 → `Ephemeral`。

### 2.2 信任边界用类型保证，不用字段+运行时检查

上一版提案是"每条 Item 带一个 `Trust` 枚举，Renderer 按 Trust 决定注入位置"。
本轮改为**类型级隔离**：

```go
// 只有框架事实能构造 Persistent 项
func (f EnvironmentFacts) Items() []Item

// 其余来源只能产 Ephemeral 项
func (s Source) Collect(...) []Item
```

`Trust` 枚举被取消。理由：枚举是"可以被赋错的值"，而类型是"编译期就写不进去
的位置"。RAG 结果被提升成 system 指令这个隐患，在新模型下**不存在可表达路径**，
因此也不需要单独的"Trust Boundary"回归用例去守。

### 2.3 顺序：只追加（append-only）

`Persistent` 项作为独立消息落在**对应那一轮的 user 消息之前**，随历史一起增长：

```
turn1 请求: [sys] ctx1  u1            a1
turn2 请求: [sys] ctx1  u1  a1  ctx2  u2
turn3 请求: [sys] ctx1  u1  a1  ctx2  u2  a2  ctx3  u3
```

上一轮的请求是下一轮请求的**严格字节前缀**，`ctx_n`/`u_n`/`a_n` 全部永久可复用。
这是唯一能突破 76% 天花板的形状，也是 DeepSeek 官方要求的"历史轮次的
`latest_reminder` 要随历史一起回传，否则降低缓存命中"。

同时它天然满足放置约束：`ctx` 紧跟自己的 user 消息，工具轮在其后追加
assistant/tool，不会把 `ctx` 挤到 `tool_use` 与 `tool_result` 之间。

## 三、厂商适配：按端点身份定首选 + 失败一次永久降级

三家都没有"通用的环境元信息协议"，可用形状各不相同：

| 端点 | 原生形状 | 硬约束 |
|---|---|---|
| DeepSeek（OpenAI 兼容） | `role: "latest_reminder"`，内容 `date,weekday,location,platform,language` | 必须紧跟 user；不得夹在 `tool_use`/`tool_result` 之间；content 必须是字符串（否则 422） |
| DeepSeek（Anthropic 兼容） | 同上（文档明确支持） | 同上 |
| Anthropic 官方 | messages 数组内 `role: "system"` | 模型白名单（Fable/Mythos/Opus 5.x、Sonnet 5.5、Haiku 5.5，**Sonnet 5 不支持**）；不能是首条；必须紧跟 user 或 server-tool-result 结尾的 assistant；不得夹在 `tool_use`/`tool_result` 之间 |
| OpenAI 及兼容网关 | `role: "developer"` | 部分兼容网关不认 |

**不采用"模型白名单表"**：那是一张必然腐化、且新模型上线必须改代码的表。
改用：

1. 按**端点身份**（base_url + provider）决定首选形状——注意
   `provider: anthropic` + `base_url: https://api.deepseek.com/anthropic` 是合法组合，
   该端点认 reminder 不认中途 system；
2. 收到 400/422 且错误体指向该 role 时，记入进程内 `bad[endpointKey]`，
   后续请求直接回退到"拼进 user 文本"的兜底形状。

400/422 在推理前拒绝、不消耗 token，因此"探测失败一次"的成本可接受，
换来的是**新模型零维护**。

## 四、窗口裁剪必须收敛到回合边界（P0，已实现）

这是本轮的**前置项**：只要 `TrimMessages` 还在回合中途跑，`append-only` 的
前缀收益就会被抵消——裁剪每发生一次，其后的历史全部失效。

历史实现把裁剪挂在 `AppendMessageNoPersist`（回合内每条中间消息）和
`SessionManager.snapshot`（每次 `SaveSession`，含回合内落库）上，于是：

- 一个 4 轮工具回合会被裁多次；
- 被裁掉的最大受害者恰是本回合**刚生成、还没被任何后续请求复用**的工具结果。

现在的约定：

```go
// 回合边界唯一裁剪入口
func (sm *SessionManager) TrimWindow(session *Session)

// 回合内追加：不裁剪
func (sm *SessionManager) AppendMessageNoPersist(session *Session, msg protocol.Message)
```

- `processWithTools` 以 defer 调用：`回收附件字节 → TrimWindow → SaveSession`，
  因此落库快照看到的是裁剪后的窗口；
- `touch` / `snapshot` 只在 `!TurnActive()` 时裁剪（回合进行中不使用头部）。

`TrimMessages` 的语义本身不变：批量低水位、System 优先、不裁出孤儿 tool 消息、
不破坏 `assistant(tool_calls)` 与 tool 结果的紧邻关系。变的只是**调用次数**：
从"每条中间消息一次"降到"每次 `processWithTools` 一次"。

## 五、实施顺序

| 阶段 | 内容 | 验收 |
|---|---|---|
| **P0** ✅ | 裁剪收敛到回合边界 + 时机用例 | 回合内第 n 轮请求是第 n+1 轮的严格前缀；全量测试绿 |
| **P1** | `EnvironmentFacts` 结构体：补 weekday / timezone / language / session_type；`EnvItems()` 产出 `Item`（**仅内部**，渲染仍走现有字符串路径，字节不变） | `context_pipeline_test.go` 不改即绿 |
| **P2** | `Item` / `Channel` / `Snapshot` 落地；`BuildWindowed` 增加返回 `Snapshot` 的变体；`Render` 保持现有 `===== x =====` 拼接 | 同上 + `TestChannelTrustByType`（编译期不可写入） |
| **P3** | `Persistent` 通道 append-only 写入历史 + 适配器原生编码 + 探测降级；配置开关 `context_channel: user_suffix \| auto`（默认 `user_suffix`，即今天的行为） | 跨轮前缀逐字节断言、DeepSeek 放置规则断言、降级用例 |
| **P4** | 真实流量对照 `cached/prompt` 与 `ai_context_trimmed_messages_total`，决定是否把默认值切到 `auto` | 一周数据 |

`P3` 必须带开关：`freeze_prefix_test` 里"动态上下文不写回历史"的断言在
`user_suffix` 下仍然成立，两条通道可以共存、用数据决胜负。

## 六、对本轮作废/修改的旧结论

| 来源 | 结论 | 处置 |
|---|---|---|
| 27 §D2 | "动态上下文只挂尾部、不写回历史" | 细化为：**Ephemeral 不写回；Persistent 必须写回** |
| 27 §D3 | "任何新增 Provider 数据默认不得进入 Stable Prefix" | **作废**：Persistent 项就是要进入历史前缀 |
| 27 §D5 | "ToolSet 是唯一跨 History 边界的动态变量" | 作废：Persistent 上下文同样是跨边界变量 |
| 29 不变量 9 | "没有真实消费者不抽象" | 降为建议；P3 的持久化回放就是消费者 |
| `TestAnthropicDropsMidArraySystemButKeepsUserTail` | 把"中途 system 被丢弃"当成期望 | 保留"能力关闭时降级为 user 尾部"，新增"能力开启时原位保留" |
| `freeze_prefix_test.go` `msgs[:len-1]` | 以"除最后一条"作为稳定前缀 | 改为"首个 context-channel 消息之前的全部消息"（P2 时随之调整） |

A/B/C/E/F/G 组契约（ToolSet 语义、Execute 三态、检索权重、策略闸门、运行时语义、
配置/指标/持久化）与 29 的分包依赖方向**继续有效**。

## 七、明确不做

- `Source` 接口外置给第三方插件注册（需要注册/权限/超时/隔离一整套，当前无消费者）；
- `Priority × Relevance × Freshness / TokenCost` 评分公式（现有节序 + 预算已够用）；
- `Resource Resolver` 独立子模块（`attachments.go` 的 `hydrateAttachmentParts`
  已经是资源解析，`protocol.ContentPart` 已实现引用与二进制分离）；
- `Context Delta` / 回合内快照版本化（先保证单回合一致性）。
