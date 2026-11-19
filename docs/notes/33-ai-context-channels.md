# 33 — AI 上下文通道：从"字符串后缀"到"语义分道"

> 本文是 `builtin/ai` 上下文体系的**新一轮迭代**的设计文档，接替
> [26](26-ai-layering.md) / [27](27-ai-freeze-checklist.md) 中与 Prompt 前缀、
> 动态上下文相关的部分。26–29 记录的是上一轮"分层与分包"重构，其中
> **§D2/D3/D5 与"没有真实消费者不抽象"已在本轮作废**，其余契约（A/B/C/E/F/G
> 组、分包依赖方向）继续有效。
>
> 状态：`P0`–`P3` 已实现（裁剪时机、环境事实、通道模型、原生编码与探测降级）；
> `P4` 待真实流量验证后再决定是否把 `context_channel` 默认值切到 `auto`。
> v1.83.0 之后 `auto` 分支有过一次线上 400 修复（形状选择不再绑定厂商私有
> 角色、开关作用域覆盖编码侧、思考型端点的历史推理回传，见 §三），`P4` 的
> 对照数据需在该修复之后重采。

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
这是唯一能突破 76% 天花板的形状，也是各家"上下文缓存按请求前缀逐块比对"的
直接推论：历史里回放的每一条都在命中范围之内。

同时它天然满足放置约束：`ctx` 紧跟自己的 user 消息，工具轮在其后追加
assistant/tool，不会把 `ctx` 挤到 `tool_use` 与 `tool_result` 之间。

**去重规则**（实现时的必要补充）：环境事实只按**日期粒度**表达（见
`protocol.EnvFacts` 刻意不含时钟），因此同一份事实在多轮之间逐字节相同。
写入前与历史里最近一条环境消息比较，相同则不重复追加——事实已经在历史里，
模型随时能读到；重复追加只会按回合线性占用窗口预算。跨日后事实变化，才写
新的一条。这也让 `ctx_n` 在一天之内保持稳定，不再逐轮增长。

## 三、厂商适配：只用两家公开协议里的形状

框架只声明兼容两种线格式（`config.provider`）：OpenAI 兼容 / Anthropic 兼容。
两种协议里各有一个官方定义的"应用级指令"形状，都能承载环境事实：

| 线格式 | 原生形状 | 硬约束 |
|---|---|---|
| OpenAI 兼容 | `role: "developer"` | 官方定义于 o1 之后；部分兼容网关不认（会 400/422）。位置无约束 |
| OpenAI 兼容（退路） | 数组内 `role: "system"` | 同上；作为 developer 被拒后的第二候选 |
| Anthropic 兼容 | 数组内 `role: "system"` | 不能是首条；必须紧跟 user 或以 tool_result 结尾的 assistant；不得夹在 `tool_use`/`tool_result` 之间；只有部分模型支持 |

**刻意不使用任何厂商私有角色。** 例如某些端点提供了 `latest_reminder` 这类
私有角色，字段取值也由该厂商单方面定义（`platform` 只允许 `App`/`Web`、
`weekday` 必须用该语言书写……）。把它按 `base_url` 识别后就发出去，等价于在
框架里内置一张**必然腐化的厂商名单**：新增一个兼容网关要改代码，改错就是整轮
对话 400，而且用户配置的模型是未知的。生产事故（`unknown platform 'qq'`）正是
这条路径的直接结果。

**形状只看 `provider`，不看模型名或域名**：

```go
protocol.NativeContextCandidates(provider)
//   openai    → developer → 中途 system → none
//   anthropic → 中途 system → none
```

8080 网关、Ollama、自建 vLLM 在 OpenAI 兼容线上与 OpenAI 官方完全一样，
因此不需要、也不允许按域名分叉。OpenAI 那条多一个候选，因为"不认 developer、
但认 system"是兼容网关最常见的差异；Anthropic 没有这个退路（官方要求中途
system 紧跟 user，而 developer 角色它根本不认），因此被拒后直接折进 user 文本。

失败处理是逐级探测，而不是一刀切：

1. 收到 400/422 且错误体指向该形状（`role` / `developer` / `system` /
   `unknown variant` 等）→ 记入 `ProbeMemory`（键 = provider + base_url + 形状），
   下一次请求直接跳到下一个候选；全被拒后折进 user 文本。
2. 400/422 在推理前拒绝、不消耗 token，因此"探测失败一次"的成本可接受，
   换来的是**新模型/新网关零维护**。

三条相关的修复也在这一层（生产事故的直接原因）：

- **开关必须同时作用于编码侧**：`envChannelShape()` 早期只按端点能力返回形状、
  不看 `context_channel`，于是"曾经开过 auto"留下的历史记录在改回 `user_suffix`
  之后仍会被编成原生角色发出去——配置已关掉却依然报错。现在写入与编码共用同一
  判定：关闭时形状为 `None`，旧记录折回 user 文本。
- **`include_runtime_context=false` 不被绕过**：它是"完全不注入"的隐私开关，
  现在连折回 user 文本都不做（`runtime.DropEnvContext`）。
- **历史工具轮的推理必须能回传**（见 §3.2）。

### 3.1 存储角色与线格式角色必须分离

会话历史里存的是 provider 中立的 `RoleEnvContext` + JSON 载荷（`protocol.EnvFacts`），
**不是**某家的线格式。原因：历史一旦落库就长期存在，而端点可以换
（OpenAI 兼容 ↔ Anthropic 兼容 ↔ 任意自建网关）；两种形状的角色名与位置约束
都不同（developer 在 user 之前 / 中途 system 必须在 user 之后），存成其中一种
就只能投递给对应的线格式。

编码由装配侧的 `runtime.EncodeEnvContext` 一处完成，适配器只负责序列化：

| 形状 | 放置 | 正文 |
|---|---|---|
| developer | 保持在 user 之前 | `<environment>` 指令块 |
| 中途 system | **挪到 user 之后**（否则 400） | `<environment>` 指令块 |
| 兜底 | 删除独立消息，正文折进 user | 散文（不加标签） |

最后一行就是"探测失败后回退到 user 文本"的实现，与 `user_suffix` 的表达等价。

### 3.2 思考型模型的推理必须能随历史回传

有的端点在思考模式下、且请求携带 `tools` 时，要求历史里每个
`assistant(tool_calls)` 都带回它当初产出的推理文本，缺了直接 400
（`The reasoning_content in the thinking mode must be passed back to the API.`）。
这是端点侧语义，不是厂商判断，因此处理方式同样是"探测 + 记性"，而不是名单：

- `Message.Reasoning` / `ChatResponse.Reasoning` / `StreamEvent.Reasoning`
  是 provider 中立的推理文本；OpenAI 适配器把它映射成线格式的
  `reasoning_content`，**只在非空时才发出**——从不产出推理的端点完全不受影响。
- 带工具调用的轮次会把推理写回会话历史（普通对话轮不写，避免无谓的历史膨胀）。
- 旧版本落库的轮次没有推理可回传：探测到上面那条 400/422 后，记入
  `ProbeMemory.RequiresReasoningEcho`，后续请求为这些轮次显式补一个空串
  （端点接受空串、拒绝缺字段），并重试同一轮。
- Anthropic 线格式不受影响：那边的推理是带签名的 thinking 块，拿不到签名就
  无法回传，官方允许缺省。

这条路径同样不认厂商：判据只有"端点返回了这条错误"，因此永远不会给不需要它的
端点多发字段。


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
| **P1** ✅ | `EnvironmentFacts` + `EnvFacts` 载荷：补 weekday / timezone / language / location / conversation kind；`RenderEnvProse` 保持默认输出逐字节不变 | `facts_test.go` 的逐字节用例 + 既有 `context_pipeline_test.go` 不改即绿 |
| **P2** ✅ | `Item` / `Channel` / `Snapshot` / `Render`：信任边界由类型保证（只有 `EnvironmentFacts.PersistentItems` 能产 `Persistent`） | `item_test.go`（含指纹稳定性与预算共享） |
| **P3** ✅ | `RoleEnvContext` 存储角色 + 适配器编码（developer / 中途 system / 兜底）+ 端点探测降级 + `context_channel` 开关 | `envcontext_test.go`（放置规则）、`capability_test.go`（候选顺序与降级边界）、`append_prefix_test.go`（跨轮严格前缀）、`envcontext_channel_test.go`（默认不变 + 逐级降级） |
| **P3.1** ✅ | 线上修复：形状只看 provider（删除厂商私有角色）、开关作用域覆盖编码侧、`include_runtime_context=false` 不被绕过、思考型端点要求历史工具轮回传推理时的探测与回补 | `capability_test.go`（候选/降级/推理回传判据）、`provider_test.go`（reasoning 三种线上形状）、`runtime/envcontext_test.go`（编码失败折回 user）、`envcontext_channel_test.go`（关闭后不得再发原生角色、全部形状被拒后折进 user） |
| **P4** ⏳ | 真实流量对照 `ai_llm_tokens_total{type="prompt_cached"}` 与 `ai_context_channel_total`，决定是否把默认值切到 `auto` | 一周数据（在 P3.1 之后采样） |

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

- 不使用任何厂商私有消息角色（如 `latest_reminder`）来投递框架事实：字段取值由
  该厂商单方面定义、其它端点一律拒绝，写进框架等于内置一张会腐化的厂商名单；
  通用形状（developer / 中途 system）+ 逐级探测已能覆盖所有 OpenAI/Anthropic
  兼容端点。

- `Source` 接口外置给第三方插件注册（需要注册/权限/超时/隔离一整套，当前无消费者）；
- `Priority × Relevance × Freshness / TokenCost` 评分公式（现有节序 + 预算已够用）；
- `Resource Resolver` 独立子模块（`attachments.go` 的 `hydrateAttachmentParts`
  已经是资源解析，`protocol.ContentPart` 已实现引用与二进制分离）；
- `Context Delta` / 回合内快照版本化（先保证单回合一致性）。
