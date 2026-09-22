# 26 — AI 插件分层：职责边界、语义过载与 Decision 层

> 起始动机：为引入 **Jev** 做准备。在讨论"Jev 应该放在哪里"时暴露出一件事——
> 当前 AI 插件里 `Tool` 一个抽象同时承担了 Protocol / Capability / Action /
> Context / Runtime 五种语义，`Context`、`Capability`、`Action`、`Runtime`
> 四层的边界已经开始互相渗透。如果在这种状态下把 Jev 直接接进去，它只会变成
> "又一个塞进 Tool Selection 的判断补丁"。
>
> 本文是对当前 AI 插件（`builtin/ai/`）的架构审查定稿：先冻结现状的精确行为，
> 再给出目标分层与分阶段迁移顺序。核心结论是——**不要急着改 `select.go`**，
> 先确定边界，再让现有代码逐个归位。

## 一、一级问题（P0–P3）

| 级别 | 问题 | 核心 |
|------|------|------|
| **P0** | 职责与依赖倒置 | `Tool` 经 `toolSource` 反向持有 `*Plugin`；两个 Runtime 共用 `Tool`；Runtime / Capability / Action 边界不清 |
| **P1** | Tool 语义过载 | Action / Command / Skill / Context-Retrieval / Baseline 全塞进 `Tool`，且 `Execute` 契约不统一 |
| **P2** | Decision / Retrieval / Context 混杂 | `Decision` 层尚不存在；检索骨架在三个消费者里各写一遍；`buildDynamicContext` 已开始膨胀 |
| **P3** | ToolSet 驱动的缓存抖动 | Dynamic Context 已正确后置；当前唯一跨 History 边界影响前缀缓存的变量只有 `ToolSet` |

### P0：依赖已经倒置

最值得警惕的是 `toolSource` 里直接存了插件实例：

```go
// builtin/ai/toolctx.go
type toolSource struct {
    userID   string
    chatID   string
    platform string
    isGroup  bool
    sender   platform.Sender
    p        *Plugin   // ← 反向依赖的核心
}
```

于是 `Tool` 并不是一个独立的能力接口，而是可以顺着 `p` 反向拿到整个 Runtime：

```
Execution → Tool → *Plugin → { Memory, Todo, Reminder, Sender, Session, ... }
```

这不是我们希望的方向：

```
Runtime → Capability → Action → Execution
```

**依赖倒置比"Tool 分类太多"更严重**：它让 `Tool` 无法脱离插件单测、无法复用，
也让"这个工具依赖了什么"无法从签名看出来——真正的依赖藏在 `context.Value` 和
`*Plugin` 里。此外还有第二个 Runtime：主循环 `processWithTools`（流式）与
Skill 循环 `executeSkill`（非流式、自维护 tool set）**消费同一个 `Tool` 抽象**，
这是 `Tool` 难以抽象的第二重原因。

### P1：Tool 已经同时承担五种语义

```
Tool
├── 1. Action                真正产生副作用（send_message / todo_* / memory_add …）
├── 2. Command Adapter       Execute 只是占位，真正执行走 command runtime
├── 3. Skill Adapter         Execute 启动另一套 LLM loop
├── 4. Context / Retrieval   memory_query 这类"读取上下文"的能力
└── 5. General / Baseline    永久进入工具集的内置能力
```

关键结论：**`Tool.Execute()` 不是一个统一的执行契约**。模型看到的 A/B/C 在协议层
都是 `function/tool`，但运行时：

| 工具来源 | `Execute` 的实际语义 | 真正执行路径 |
|----------|--------------------|-------------|
| 普通 Action | Go 闭包真执行 | `tool.Execute` |
| 自动发现的命令 | 占位串 `[命令 X 已触发]` | `cmdPatterns` + `executeRealCommand`（合成事件） |
| Skill / 用户 Skill | 启动子代理 LLM 循环 | `executeSkill`（非流式、独立 tool set） |

`executeToolResult` 因此要按名字做四路解析：`Skill(owner) → Skill(system) → real command
→ Execute 兜底`（`builtin/ai/execute.go:59`）。这是后续一切重构困难的直接来源。

### P2：Decision 缺失，Retrieval 骨架重复，Context 膨胀

- **`Decision` 层不存在。** 系统现在没有"这一轮该不该动手 / 该暴露哪些能力 /
  是否允许执行"的决策模块，这些判断被分散在 `selectToolsForTurn`、`execOneTool`、
  Skill 循环、Memory 路径里。所谓 `NeedTool` 在代码里**根本不存在**——系统
  **无条件开启工具**，从不判断需不需要。
- **Retrieval 骨架重复。** `tokenize → candidate → prefilter → semantic score →
  ranking → top-K/budget` 这套骨架在 `select.go`、`rag.go`、`memory.go` 各写一遍；
  好在 `textVectorCache`（embedding 缓存）与 `scoreEmbedW` 等常量已经共用，重复的
  是**流水线结构与门槛参数**，不是从零实现。
- **Context 膨胀。** `buildDynamicContext` 已同时编排 Runtime / Group / Memory /
  RAG，并为预算做了 `buildDynamicContextBudgeted`；继续加来源必然变成
  `if memory / if rag / if group / if runtime / if todo …` 的大杂烩。

### P3：缓存问题现在可以精确描述

删掉旧表述"Dynamic Context 导致历史缓存失效"——这是**错的**。当前已经做对的隔离是：

```
Stable Prefix
├── System            （稳定）
├── Tools             （ToolSet 决定）
├── History
└── User Message
      └── Dynamic Context  ← Memory / RAG / Runtime / Group 全部后置到这里
```

`Memory / RAG / Runtime` 变化**不会**击穿 History 前缀（见
`builtin/ai/handler.go:870`、`docs/06-plugins/AI_PLUGIN.md`）。

准确的因果链只有一条：

```
ToolSet change → tools 序列化变化 → History 前缀失效 → cache miss
```

而 `ToolSet` 受 **Selection + Mandatory + Sticky + Session Used + Budget** 共同影响。
因此 P3 定稿为：

> 动态状态中，只有 `ToolSet` 跨越 History 边界影响前缀缓存；而 `ToolSet` 又受
> Selection、Mandatory、Sticky 与 Session State 的共同影响，因此 **ToolSet Jitter
> 是当前 Prompt Cache 的主要结构性扰动源**。

## 二、现状的精确行为（重构前必须冻结）

这一节的目的是**避免重构时误删已经做好的性能优化**。以下行为都已验证、且被测试覆盖，
重构前应视作契约。

### 2.1 `ToolSelectMax` 的真实语义

`tool_select_max`（默认 20）**不是**"最终工具集硬上限"，而是：

1. `len(tools) <= max && 无 embedding` 时**直接全量返回**，连选择都不做
   （`builtin/ai/select.go:251`）——它是一个**短路阈值**；
2. 必保集（general + 会话已用）在它**之前**追加，**不受 `max` 约束**
   （`builtin/ai/select.go:331`）——必保的数量可以超过 `max`；
3. 补充项才受 `max` 封顶（`builtin/ai/select.go:345`）；
4. 之后 `stabilizeToolSet` 再做单调并集，最多追加 `tool_set_sticky_max` 个
   （`builtin/ai/toolset.go:232`）。

精确语义（不是 `≈`，冻结时应按此描述）：

```
Candidate = Mandatory(静态 general) ∪ SessionUsed(会话已用) ∪ SelectedOptional
            （SelectedOptional 受 ToolSelectMax 与 tool_budget 约束）

最终 ToolSet = Candidate ∪ StickyFillers
StickyFillers ⊆ (上一稳定集合 \ Candidate)
|StickyFillers| ≤ tool_set_sticky_max
token(Candidate) + token(StickyFillers) ≤ tool_budget
```

注意两处容易写错的细节：候选集本身**不受** `tool_budget` 回收（预算只在追加
StickyFillers 时检查，`builtin/ai/toolset.go:258`）；`ToolSelectMax` 只约束
`SelectedOptional`，不约束 `Mandatory`/`SessionUsed`。这解释了"94 tools → 31 tools"：
既不是 `94 → Top 20`，也不是简单的预算裁剪。重构时必须把概念拆清楚：
`CandidateLimit` / `NormalSelectionLimit` / `HardToolLimit` / `TokenBudget` /
`MandatoryPolicy` 是五个不同的量。

### 2.2 必保集（Mandatory）的真实构成

`isGeneralTool` 对 **Categories 为空** 也返回 `true`（`builtin/ai/select.go:133`）。
于是当前进入必保集的是：

```
Memory（memory_add / memory_query / memory_forget）
send_message / send_to
todo_*（add / list / done / remove）
remind_*（add / list / cancel）
create_plan / update_plan_step
系统 Skill（registerSkillAsTool 未设 Categories）
用户 Skill（buildUserSkillTools 未设 Categories）
```

也就是说：**每新增一个内置能力或技能，必保集就线性膨胀一个**——这是 P1 直接推高
`ToolSet`、并让 `ToolSelectMax` 逐步失效的结构性机制。Memory 只是最显眼的样本，
不是唯一来源。

### 2.3 `Tool.Execute` 的三种执行语义

见 P1 表格。重构时需保留三种语义，但把它们放进不同的 `Invoker`，而不是继续让
`Execute` 一个字段承担全部。

### 2.4 动态上下文的后置隔离

`buildDynamicContext` 的输出挂在本轮用户消息尾部，不写回会话历史、不进 System 消息。
这是已经正确的部分，重构 Context Pipeline 时必须保持"动态内容不进稳定前缀"的性质。

## 三、目标分层

### 3.1 一张图

```
                         AI Runtime
                             │
        ┌────────────────────┼────────────────────┐
        │                    │                    │
        ↓                    ↓                    ↓
   Conversation          Agent State          Context
        │                    │                    │
        │              ┌─────┴─────┐        ┌────┴─────┐
        │              │           │        │          │
        │            Plan        Session   History    ContextProvider
        │                                      │       ├── Memory
        │                                      │       ├── RAG
        │                                      │       ├── Runtime
        │                                      │       └── Group
        └────────────────────┬─────────────────┘
                             ↓
                      ContextSnapshot

                     ┌──────────────┐
                     │  Retrieval   │   （横切服务）
                     │   / Ranking  │
                     └──────┬───────┘
                            │
              ┌─────────────┼─────────────┐
              ↓             ↓             ↓
        ToolRetriever  MemoryRetriever  KnowledgeRetriever
              │             │
              ↓             ↓
         Decision        Context

                          Capability
                              │
               ┌──────────────┼──────────────┐
               ↓              ↓              ↓
          MessageSink    MemoryStore    CommandCatalog
          TodoStore      Scheduler      SubAgentRunner
               │
               ↓
                            Action
                              │
                        ┌─────┴─────┐
                        │ ActionSpec │
                        └─────┬─────┘
                              ↓
                           Decision
                   ┌──────────┼──────────┐
                   ↓          ↓          ↓
              NeedAction   Select     Policy
                              │
                              ↓
                          Execution
                              │
                   ┌──────────┼──────────┐
                   ↓          ↓          ↓
                Timeout     Retry      Trace
                              │
                              ↓
                           Invoker
                       ┌─────┼─────┐
                       ↓     ↓     ↓
                      Func Command SubAgent
```

### 3.2 核心思想：Capability ≠ Action

- **Capability** 是"系统**能**做什么"——稳定的接口/原语：
  `MessageSink`、`TargetResolver`、`MemoryStore`、`TodoStore`、`Scheduler`、
  `CommandCatalog`、`SubAgentRunner`。
- **Action** 是"这一轮允许模型**调用**什么"——面向模型的绑定：
  `ActionSpec` + `Policy` + `Invoker`。

例如：`MemoryStore` 是 Capability，`memory_add` / `memory_forget` 才是 Action；
`CommandCatalog` 是 Capability，某个具体命令才是 Action。这样 Command / Skill /
Action 在**协议层**依然统一（都是 `function/tool`），但在**内部语义层**各归其位。

**硬约束：Capability 必须是单一资源/单一能力的最小 Port，不负责选择、策略、模型决策与
跨能力编排。** 反例是把 `Add/Delete/Query/ShouldRetrieve/Select/BuildContext` 塞进一个
`MemoryCapability`——那只是把 `Plugin` 换成了另一个大接口继续过载。选择归 Selection，
策略归 Decision，编排归 Runtime。

### 3.3 Decision：目前缺失的一层

很多"应该做什么"的决定，现在散落在 Tool Selection / Execution / Skill Loop /
Memory 路径里：是否需要工具、是否需要搜索、该暴露哪些能力、是否允许执行、是否需要
approval、是否继续、是否重试——**这些都是 Decision**。目标形态：

```
Decision
   ├── NeedAction     这一轮需不需要动手？
   ├── SelectAction   该暴露/选择哪些 Action？
   └── Policy         approval / RBAC / rate 的统一闸门
```

`NeedAction` 的判定必须按**任务意图**，而不是**数据可得性**，否则 Memory 会让它恒为
`true`（"系统里可能有有用的信息" ≠ "这一轮需要主动执行 Action"）。

### 3.4 NeedAction 定义（定稿）

> **Action 是需要通过显式 invocation 改变外部状态，或主动向当前上下文之外请求新的外部
> 观测的行为。**

关键在"**主动、外部**"：`memory_query` 由框架 Context Pipeline 自动完成，属 Retrieval；
而 `web_search` 虽然也是 retrieval，但它是**模型主动向外部世界**请求观测，因此是 Action。
这条边界决定 Retrieval 会不会把 RAG / Memory / Web / KB / API lookup 全部揉在一起。

| 行为 | 是否 Action | 层 |
|------|-------------|-----|
| `web_search` / `get_weather` / `fetch_url` | ✅ | Action → Retrieval/Capability |
| 发送消息 | ✅ | Action |
| 创建 Todo | ✅ | Action |
| 添加 / 删除 Memory | ✅ | Action |
| 查询 Memory | ❌ | Retrieval / Context |
| RAG 检索 | ❌ | Retrieval / Context |
| History / Session / Runtime Context | ❌ | Context |

于是 Memory 的定位立刻清晰：

```
memory_query   → Retrieval / Context
memory_add     → Action
memory_forget  → Action
```

### 3.5 Retrieval 是横切服务，不是 Context 的兄弟层

`Retrieval/Ranking` 同时喂 Context（Memory / RAG 注入）和 Decision（Action 选择），
因此它是**横切能力**，下面挂不同的 Retriever：

```
ToolRetriever / MemoryRetriever / KnowledgeRetriever
    = 不同数据源 + 不同策略，共用同一骨架
```

抽出来之后，`candidate prefilter` / `embedding cache` / `cosine` / `reranking`
只需改一处，而不是 `select.go` + `rag.go` + `memory.go` 三处。

**但不要为了复用而把三者强行统一成同一个 `Retriever[T]` interface**：`ToolRetriever`
返回 Action candidate，`MemoryRetriever` 返回 Memory fragment，`KnowledgeRetriever`
返回 Knowledge fragment，领域模型不同。共享的是算法骨架（tokenize / candidate filter /
embedding / similarity / ranking / top-K / budget），**不共享领域模型**。形态应为
`RetrievalCore` 被三者组合使用，而不是一个泛型接口套三种语义。

### 3.6 Context 从 Dynamic Context 演进为 Context Pipeline

```
ContextProvider → ContextFragment → ContextBuilder → ContextSnapshot
```

每个 Provider 只回答"我能提供什么上下文"，不回答"整个 AI 这一轮应该怎么办"：

```
ContextProvider
├── HistoryProvider
├── MemoryProvider
├── RAGProvider
├── RuntimeProvider
└── GroupProvider
```

### 3.7 两个 Runtime 共享的不是 Tool，而是 ActionSpec

```
              Agent Runtime
         ┌─────────┴─────────┐
         ↓                   ↓
  Conversation Loop    Skill/SubAgent Loop
         │                   │
         └─────────┬─────────┘
                   ↓
              ActionSpec
                   ↓
                Invoker
```

两个 Runtime 的**调度语义可以不同**（流式 vs 非流式），但它们共享的是
`ActionSpec` / `Capability` / `Policy` / `Execution`，而不是一个"万能 Tool"。

### 3.8 `general` 拆成两个概念

现在 `CategoryGeneral` 同时表达"这是什么类别"与"它应该默认保留吗"，且"空 Categories"
也被解释为 general→mandatory。应拆成：

- `Category`：描述它是什么。
- `SelectionPolicy`：描述它什么时候应该被保留（`Baseline` / `Optional` /
  `Mandatory` / …）。

### 3.9 `ActionSpec` 绝不包含执行

必须守住 `What` / `How` 的分离，否则 `Tool → ActionSpec` 只是改名字：

```go
// 正确：只描述"是什么、怎么被选择与治理"
type ActionSpec struct {
    Name        string
    Description string
    Parameters  Schema
    Category    ActionCategory
}
// 执行是单独的 Invoker，不在 Spec 里
```

`Execute` / 执行函数**不得**出现在 `ActionSpec`（或 `ActionPolicy`）中；执行由
`ActionInvocation → Invoker` 承担。一旦 Spec 里重新出现 `Execute`，分层立刻退回旧状态。

### 3.10 保留来源（Retention Sources）的硬定义

实现 Decision 前必须钉死"什么被保留、什么可被 `NeedAction=false` 抑制"，否则很容易写成
`if !needAction { tools = nil }`，缓存稳定性立刻回退。当前代码实际存在四种保留来源：

| 来源 | 现状对应 | `NeedAction=false` 时 |
|------|----------|----------------------|
| **Mandatory（静态）** | `Categories` 含/为空 general（`select.go:133`） | **永远保留** |
| **SessionUsed（动态）** | 本会话已调用过的工具（`select.go:336`） | **保留**（多轮任务不中断） |
| **Sticky（历史）** | 上一稳定集合的补充项（`toolset.go`） | **不得被主动清空** |
| **Optional（候选）** | 打分补充项（`select.go:344`） | 受 Selection / NeedAction / Budget 控制 |

> 静态声明层用 `SelectionClass`（`Baseline`/`Optional`/`Mandatory`）表达，动态来源
> （SessionUsed / Sticky）由 Decision 计算，两者不可混为一个字段。

### 3.11 Jev 的正式边界

Jev 只处理 **deterministic policy 无法低成本判断的歧义语义决策**，不接管全部决策：

```
Selection / Decision
   ├── deterministic policy（规则命中即返回，成本低、可解释）
   └── Jev（仅当规则无法判定时介入：ambiguous decision）
```

禁止 `Message → Jev → Everything`，否则 Selection / Retrieval / Policy / Context 会被
再次吞并。

## 四、代码归位映射

| 现有代码 | 处置 | 目标层 | 备注 |
|----------|------|--------|------|
| `processWithTools`（主循环）、`executeSkill`（Skill 循环） | 保留 + 换层 | Agent Runtime | 两个 Loop，共享 ActionSpec |
| `toolSource.p *Plugin` | **删除** | — | 换成具体 Capability（request-scoped） |
| `ToolSender`（拆出 `ResolveTarget`）、`memoryStore`、`todoManager`、`reminderManager`、命令目录、`executeSkill` | 改造 | Capability | 纯接口/原语，每回合实例注入 |
| `buildDynamicContext`、`buildGroupContext`、`buildMemoryContext`、`buildRAGContext`、`buildRuntimeContext` | 改造 | Context / ContextProvider | 拆 Provider，复用 `budget.go` |
| `textVectorCache`、`tokenizeText`、`tokenOverlap`、`cosineSimilarity`、`jaccardSimilarity` | 抽取 | Retrieval/Ranking | 抽公共骨架，以 `contract_test.go` 验收 |
| `select.go`（打分）、`rag.go`（预筛）、`memory.go`（Retrieve） | 抽取 | Retrieval | 各自成为 Retriever |
| 当前 `Tool` 的 Name/Description/Parameters/Categories | 改造 | Action（`ActionSpec`） | 模型可见；**不加 ID** |
| `CategoryGeneral`（含空 Categories） | 拆分 | Action | `Category` 与 `SelectionClass` 分离 |
| `RequiresApproval` / `AlwaysRequireApproval` / `Permissions` | 迁移 | Action（`ActionPolicy`） | 声明在 Action，评估在 Decision |
| `Execute`（Func / 命令占位 / Skill） | 改造 | Execution（`Invoker`） | `FuncInvoker` / `CommandInvoker` / `SkillInvoker` |
| `selectToolsForTurn` + `toolset.go` 的选择与稳定策略 | 拆分保留 | Decision（Selection） | 保实现、保测试结果 |
| `execOneTool` 的 timeout/retry/parallel/trace/result | 保留 | Execution | 已成熟 |
| `sendtool.go:96` `sendToAllowed` 二次判定 | **收敛** | Decision | 与 `execOneTool` 合并为单一闸门 |
| `memory_query` | 降级 | Retrieval / Context | 不再是 Action |
| `isToolErrorResult` 字符串前缀判定 | **退役** | — | 由 `ActionResult.Err` 取代 |
| `captureSender`（附件旁路捕获） | 评估退役 | — | 依赖 vevent/engine，可能不由本次完成 |
| `Session`（plan / todos / reminders / pendingImage / interrupt） | 保留 | Agent State | 逻辑归属≠持久化 schema |
| `provider.go` / `provider_*.go` | 保留 | Model Provider | 已干净 |

> **落地状态**（2026-09，详见 [28 — 执行进度](28-ai-layering-progress.md) 第二十一 ~ 二十五节）：
> 本表大部分行已交付；以下一行**尚未交付**，属已知遗留，不宜当作新发现：
>
> - Context 交付形态为 **Provider + Builder**，未引入 `ContextFragment` / `ContextSnapshot` /
>   `ContextBudgeter` 类型（见 §3.6 目标图与不变量 9 的取舍，28 第二十一节 G-E）。
>
> 已交付：`Tool` 不再作为内部存储承载 `Execute`——注册表内部只存动作视图，
> `Action` 成为选择 / 稳定 / 序列化 / 策略评估的内部货币（见 28 第二十五节）；
> `RequiresApproval` / `AlwaysRequireApproval` / `Permissions` 的**评估**已归 Decision
> （`decideToolInvocation`，含 RBAC 与审批，见 28 第二十三节）；
> 命令目录 / Skill 运行器已抽成能力端口（`commandCatalogPort` / `skillRunnerPort`，见 28 第二十四节）；
> `memory_query` 已降级为 Retrieval / Context（不再是 Action，见 28 第二十二节）。

## 五、迁移顺序（修订版）

总体目标不是"重构出一套漂亮的新架构"，而是：**在不破坏现有 Tool Selection、Embedding、
Retrieval、Sticky、Prompt Cache、Execution、Metrics 与持久化行为的前提下，把
`Plugin → Tool → Execute` 这条过度耦合的链，逐步拆成
`Capability → Action → Decision → Execution → Invoker`，每一步都能独立验证与回滚。**

| Phase | 内容 | 关键约束 |
|-------|------|----------|
| **0** | **冻结现状行为**（见 [27 — 冻结清单](27-ai-freeze-checklist.md)） | 只记录+补测试，不改任何行为 |
| **1** | Capability：消除 `toolSource → *Plugin`；建立最小接口；Plugin 降为组装器 | Capability 是 **request-scoped**，每回合由 TurnContext 构造/注入 |
| **2** | Action：`ActionSpec` + `ActionPolicy` + `SelectionClass` + `ActionInvocation`，**并定义 `ActionResult` 类型** | 旧配置语义不得静默改变 |
| **3** | Invoker：`FuncInvoker` / `CommandInvoker` / `SkillInvoker`（委托现有 `executeSkill`） | 协议层零破坏；先委托不重写 |
| **4a** | ActionResult 收敛：typed `Err` 退役 `isToolErrorResult` 字符串判定 | 先锁测试再改行为 |
| **4b** | `captureSender` 退役**评估**（依赖 vevent/engine，可能延后或取消） | 不承诺在本次完成 |
| **5** | Retrieval：`ToolRetriever` / `MemoryRetriever` / `KnowledgeRetriever` | 保权重、缓存与 `contract_test.go` |
| **6** | Context：Provider → Fragment → Builder → Snapshot | 复用 `budget.go`，保现有优先级排序 |
| **7** | Selection + Decision 合并：Selection 内置 `needAction` gate（默认 true）；`ActionPolicy` 评估；`NeedAction` | 见下方 D2/D3、§3.10 |
| **8** | Agent State 演进（Plan / Todo / Reminder / PendingImage / Interrupt 的逻辑归属与持久化分离） | 逻辑归属≠持久化 schema；不阻塞 Jev |
| **9** | Jev（按真实需求决定，且只作为二级决策器） | 见 §3.11，不接管全部决策 |

阶段顺序相对早期方案的两处修订：

- **`Result` 类型从 Phase 4 提到 Phase 2**——因为 Phase 2/3 的 `Invoker` 签名已经返回
  `Result`，类型必须先存在；Phase 4 只做"行为收敛"，`captureSender` 退役另列 4b。
- **Selection 与 Decision 合并为 Phase 7**——`NeedAction` 是 Selection 的输入（只抑制
  Optional），把它排在 Selection 之后会导致 Selection 白做一遍；合并后一次到位。

> ⚠️ 不要先动 `select.go`。在没有 Decision 层的情况下直接改 Selector，很容易变成
> `ToolSelector v2`，最后只是换个名字重新制造一个超级模块。

### 5.1 已确认的设计决策

| # | 决策 | 理由 |
|---|------|------|
| D1 | `ActionResult` 类型提前到 Phase 2；`captureSender` 退役单列 4b | 类型先于消费者；命令输出捕获是框架层依赖 |
| D2 | `NeedAction` 与 Selection 合并，Selection 内置 `needAction` gate（默认 true） | 二者互为输入输出 |
| D3 | `Capability` 为 request-scoped，每回合由 TurnContext 构造/注入，**不是全局单例** | `MessageSink` 必须知道当前会话/发送者/预算/审批授予 |
| D4 | `SelectionClass` 采用枚举（`Optional`/`Baseline`/`Mandatory`），**不用多个 bool** | 避免非法状态 |
| D5 | 现有 `general`/空 Categories **全部保真映射为 `Baseline`**，本次不改变 ToolSet 组成 | 改变会违反不变量 3/4；调整属独立课题并配指标观测 |
| D6 | `ActionResult.Err` 表示"模型可见的工具失败"，据此退役 `isToolErrorResult` | 现状字符串前缀判定脆弱 |
| D7 | 子代理内 `NeedAction` 可重跑；Policy 授予是继承还是重评必须显式定义 | 防止安全或审批体验回退 |
| D8 | Phase 2 的 `SelectionClass` 与旧 `isGeneralTool` 之间加翻译函数，Phase 7 才切换 | 保持中间态测试绿 |
| D9 | 回滚机制：新抽象只增不减、旧路径保留到测试绿、每 Phase 一个 PR、行为差异用 config 开关而非 build tag | 保证"可独立验证与回滚" |
| D10 | `Capability` 命名与 `platform.Capabilities` / `plugin.TryService` 区隔，建议改用 `Service`/`Port` 或限定 `ai.Service` | 避免同仓库双语义 |
| D11 | 保留来源分静态声明（`SelectionClass`）与动态来源（SessionUsed / Sticky）两层，`NeedAction=false` 不得清空 Mandatory/Baseline/SessionUsed/Sticky | 见 §3.10，防止缓存稳定性回退 |
| D12 | `ActionSpec` 不含执行函数；`Execute` 只存在于 `Invoker` | 守住 What/How 分离，防止换名重现旧 Tool |

## 六、重构不变量（检查清单）

1. **Tool 是协议边界概念，不统治内部架构。** `tools`/`function` 保留在 LLM API 边界，
   离开协议边界后不得继续作为万能抽象。
2. **Capability 不依赖整个 Plugin。** 禁止 `Action → Plugin → Everything`；依赖必须能从
   签名/构造处看出，不再藏在 `context.Value` 或 `*Plugin` 里。
3. **Decision 不得破坏 ToolSet 缓存稳定性。** 任何新决策都不能让 tools 段在
   `[] ↔ [...]` 之间来回切换；`NeedAction=false` 只抑制 Optional，不移除 Baseline/Sticky。
4. **动态上下文不得重新污染 History 前缀（Context Pipeline 必须 suffix-only）。** Memory/RAG/Runtime/Group
   只附在用户消息尾部；任何新 ContextProvider 默认不得进入 Stable Prefix，除非明确证明其稳定。
5. **ToolSet 按名称升序、集合不变时字节稳定，且始终 ⊆ 本轮可用集**（群策略 + RBAC 过滤后），
   权限收缩立即生效。
6. **Retrieval 行为不得回归。** Top1 / Recall@K / MRR 与现有权重常量保持不变。
7. **Execution 不承载业务策略。** 只管 timeout / retry / parallel / trace / result；
   approval/RBAC/rate 只有一个闸门，且在 Execution 之前。
8. **旧配置语义不得静默改变。** `tool_select_max` 等键保持现有行为，新增量通过新增键引入。
9. **新抽象必须有 ≥1 个真实消费者。** `SubAgentRuntime`、`DecisionEngine`、独立 package、
   复杂 Policy framework 都不得"为以后可能用"提前造。
10. **每步可独立验证与回滚。** 新抽象只增不减；旧路径保留到对应冻结测试转绿。
11. **What / How 分离。** `ActionSpec`（描述）不得包含执行函数；执行只经 `Invoker`。
12. **Jev 只处理歧义决策。** 规则可判定的一律走 deterministic policy，Jev 不得接管全部决策。

> 具体"Phase 0 先锁哪些行为、还缺哪些测试、怎么跑"，见配套的
> [27 — AI 插件 Phase 0：现状行为冻结清单](27-ai-freeze-checklist.md)。

## 修订记录

| 版本 | 修订 |
|------|------|
| v1 | 初次定稿：P0–P3、现状精确行为、目标分层、代码归位映射、Phase 1–6 |
| v2 | Capability 改回 Invoker 依赖；单包优先；Phase 0 冻结；ActionSpec+Policy+Invoker；去 ID；SelectionClass 枚举；SkillInvoker 委托；配置兼容；NeedAction 不控 tools 开关；Retrieval 先于 Context；复用 budget.go；Agent State≠持久化；指标兼容；不变量 1–10 |
| v3 | ToolSet 公式改为 `Candidate ∪ StickyFillers` 精确语义（§2.1）；Capability 增加"最小 Port"约束（§3.2）；NeedAction 改为"主动、外部"措辞（§3.4）；Retrieval 明确"共享骨架不共享领域模型"（§3.5）；新增 ActionSpec 不含执行（§3.9）、保留来源硬定义（§3.10）、Jev 边界（§3.11）；Phase 8 改为 Agent State、新增 Phase 9 Jev；不变量增补 11/12 |
| v4 | 对齐落地现状（仅文档）：§四 表下补"落地状态"说明（5 项未交付遗留，指向 28 第二十一节）；附录 `buildDynamicContext` 位置修正为 `context_pipeline.go` |
| v5 | `memory_query` 降级交付后同步 §四 落地状态（未交付遗留 5 项 → 4 项） |
| v6 | 调用策略评估（RBAC + 审批）收敛到 Decision（未交付遗留 4 项 → 3 项），附录代码位置同步 |
| v7 | 命令目录 / Skill 运行器抽成能力端口（未交付遗留 3 项 → 2 项），附录代码位置同步 |
| v8 | `Action` 取代 `Tool` 成为内部存储、`Execute` 仅经执行视图取出（未交付遗留 2 项 → 1 项），附录代码位置同步 |

## 附：关键代码位置

| 主题 | 位置 |
|------|------|
| `Tool` 定义与注册表（内部存动作视图，`Get` / `List` 还原执行载荷） | `builtin/ai/tool.go` |
| `Action` / `ActionSpec` / `ActionPolicy` 定义与保真派生 | `builtin/ai/action.go` |
| 工具执行上下文注入（`toolSource` / `*Plugin`） | `builtin/ai/toolctx.go` |
| 执行分派（四路解析） | `builtin/ai/execute.go` |
| 命令自动发现（占位 Execute） | `builtin/ai/discovery.go` |
| Skill 包装为动作（`userSkillActions`） | `builtin/ai/discovery.go`、`builtin/ai/process.go` |
| 主循环 / 并行执行 | `builtin/ai/process.go` |
| 动作决策与调用策略评估 | `builtin/ai/decision.go`（选择 + `decideToolInvocation`）；审批交互在 `builtin/ai/approval.go` |
| 动作调用器与执行路径端口 | `builtin/ai/invoker.go`（`commandCatalogPort` / `skillRunnerPort` + 适配器） |
| 选择与打分 | `builtin/ai/select.go` |
| ToolSet 稳定策略 | `builtin/ai/toolset.go` |
| 消息发送 Capability + 工具 | `builtin/ai/sendtool.go` |
| 记忆检索 | `builtin/ai/memory.go` |
| RAG 检索 | `builtin/ai/rag.go` |
| 动态上下文构建 | `builtin/ai/context_pipeline.go`（Provider/Builder；`buildRuntimeContext`、`buildMemoryContext` 仍在 `handler.go`） |
