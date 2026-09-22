# 27 — AI 插件 Phase 0：现状行为冻结清单

> 配套 [26 — AI 插件分层](26-ai-layering.md)。26 回答"目标架构长什么样"，
> 本文回答"**动手之前必须先锁死哪些现状行为**"。
>
> 目的只有一个——避免后续 `Capability / Action / Invoker / Retrieval / Context / Decision`
> 重构把已经验证、已经调优过的性能与语义**悄悄改坏**。这里冻结的是**可观测行为**，
> 不是实现：允许换写法，不允许换结果。

## 一、冻结原则

1. **冻结可观测行为，不冻结实现。** 内部结构可以改，输入/输出、排序、字节序列、
   指标名与 label 不变。
2. **Phase 0 只做两件事：记录 + 补测试。** 不改任何 `builtin/ai` 非测试代码；
   缺口用新测试补上，而不是先改逻辑。
3. **能测成断言的就别只写在文档里。** 文档描述意图，测试负责防回归；两者都要有。
4. **回滚友好。** 每个 Phase 一个 PR；冻结项对应一组测试，任何一步失败都能定位到具体契约。
5. **编号空间。** `A–G` 是**契约分组**（`A1`、`B2`…）；`GAP-n` 是**测试缺口编号**;
   `N-n` 是**负向不变量**。三者前缀不同，互不复用（`G1` 是契约项，`GAP-1` 是缺口）。

## 二、契约分组总览

| 组 | 主题 | 主文件 | 契约数 |
|----|------|--------|--------|
| **A** | 选择与稳定（ToolSet） | `select.go`、`toolset.go`、`tool.go` | A1–A13 |
| **B** | 执行语义（Execute 三态） | `execute.go`、`discovery.go` | B1–B8 |
| **C** | 检索契约（Retrieval） | `select.go`、`rag.go`、`memory.go` | C1–C6 |
| **D** | 前缀缓存（Prompt Prefix） | `handler.go`、`prompt.go` | D1–D5 |
| **E** | 策略闸门（Policy Gate） | `process.go`、`grouppolicy.go` | E1–E5 |
| **F** | 运行时语义（Runtime） | `process.go`、`retry.go` | F1–F6 |
| **G** | 配置 / 指标 / 持久化 | `config.go`、`metrics.go`、`session.go`、`storage.go` | G1–G4 |

## 三、A — 选择与稳定（ToolSet）

### A1 短路阈值（`select.go:251`）

`len(tools) <= ToolSelectMax && (emb == nil || !emb.Enabled())` 时**原样全量返回**，
不打分、不调用 embedding。这是零开销快路径，不是"最终工具数上限"。

- 默认 `ToolSelectMax = 20`（`config.go:189`）。

### A2 必保集不受 `ToolSelectMax` 约束（`select.go:331`）

必保集在 `max` **之前**追加，**不受 `max` 约束**——必保数量可以 `> max`。
`ToolSelectMax` 只约束"补充项"（`select.go:345`）。

### A3 必保判定：空 `Categories` 也算 general（`select.go:133`）

```go
func isGeneralTool(t Tool) bool {
    if len(t.Categories) == 0 { return true }
    return slices.Contains(t.Categories, CategoryGeneral)
}
```

于是当前进入必保集的是：

```
memory_add / memory_query / memory_forget
send_message / send_to
todo_add / todo_list / todo_done / todo_remove
remind_add / remind_list / remind_cancel
create_plan / update_plan_step
系统 Skill（registerSkillAsTool 未设 Categories）
用户 Skill（buildUserSkillTools 未设 Categories）
```

即 **≥15 个内置工具 + 系统 Skill + 用户 Skill**。每新增一个未标注 `Categories` 的内置能力
或技能，必保集就线性膨胀一项——这是 P1 推高 ToolSet、让 `ToolSelectMax` 逐步失效的结构性机制。

### A4 会话已用工具进入必保（SessionUsed）

本会话已被调用过的工具进入候选（`select.go` 必保路径），保证"用过一次下次还在"，
避免同一话题反复回访时工具集抖动。

### A5 补充项（SelectedOptional）受 `ToolSelectMax` 与 `tool_budget` 约束

打分排序后的补充分数截断在 `ToolSelectMax`，随预算 `tool_budget` 一并收敛。

### A6 embedding 结果缓存（`textVectorCache`）

同一 query 的向量不重复请求 embedding；`contract_test.go` / 现有缓存已覆盖。
**冻结要求**：以"embedding 调用次数"为断言，而不是以耗时断言。

### A7 `tool_budget` 只约束补充项与 sticky fillers，不回收必保集（`toolset.go:258`）

预算检查发生在**追加 StickyFillers** 时；候选集（Mandatory ∪ SessionUsed ∪ SelectedOptional）
本身不被 `tool_budget` 回收。默认 `ToolBudget = 8000`（`config.go:190`）。

### A8 sticky 策略开关与上限

`tool_set_sticky`（默认 `true`，`config.go:202`）、`tool_set_sticky_max`（默认 `8`，
`config.go:203`）。`stabilizeToolSet`（`toolset.go:106`）在候选之上追加上一稳定集合的补充项；
策略关闭（`tool_set_sticky=false`）或 `session == nil` 时**原样返回候选**（`toolset.go:107`）。

### A9 最终 ToolSet 的精确语义（不是 `≈`）

```
Candidate    = Mandatory(静态 general) ∪ SessionUsed(会话已用) ∪ SelectedOptional
               （SelectedOptional 受 ToolSelectMax 与 tool_budget 约束）

最终 ToolSet = Candidate ∪ StickyFillers
StickyFillers ⊆ (上一稳定集合 \ Candidate)
|StickyFillers| ≤ tool_set_sticky_max
```

> ⚠️ 不要写成 `≈ max(必保集, ToolSelectMax) + sticky`。这是解释性近似，
> 作为冻结契约会误导实现。要注意：**候选集不受 `tool_budget` 回收**（A7），
> 且 `ToolSelectMax` 只约束 `SelectedOptional`（A2）。

### A10 单调并集 + 回访保持（`toolset.go:144-155`）

`len(prev) == 0` → `next = candNames`（首次收敛 / 历史被清空）；
`candNames ⊆ prev` → `next = prev`（**保持现状**，回访或话题往复不产生新的缓存失效）；
否则 → `mergeToolSet(candNames, prev, avail, lastSeen)`。

### A11 可用集收缩立即生效（`toolset.go:124-131`）

`prev = st.Names ∩ available`；不在 `available` 里的历史状态**直接丢弃、不写入新状态**。
即权限收缩 / 群策略收紧 / 插件注销后，sticky 里对应工具**立即消失**（见 N2）。

### A12 空闲衰减（TTL，`toolset.go:157-174`）

不在本轮候选中的补充项，超过 `effectiveToolSetTTL()` 未被候选命中即**批量移除**
（`reason = decay`），不做逐轮零敲碎打。负 TTL（如 `-1s`）表示关闭衰减，
此时仅受 `ToolSetStickyMax` 约束（`config.go:212`）。

### A13 确定性排序与 Generation / Reason

- 输出按**名称升序**：注册表出口 `tool.go:219`、sticky 合并 `toolset.go:266`、
  工具名 `toolset.go:277`。同一集合跨请求序列化为**相同字节序列**。
- `Generation` 只在集合变化时自增（`toolset.go:177-189`），
  `reason ∈ {init, grow, shrink, decay}`（另有 `keep`），供
  `toolset_changes_total{reason}` 观测。

## 四、B — 执行语义（Execute 三态）

### B1 `executeToolResult` 四路解析顺序（`execute.go:40`）

```
Skill(owner) → Skill(system) → real command → tool.Execute（兜底）
```

顺序即语义：先按当前 Skill 自有工具，再系统 Skill，再真实命令，最后普通 `Execute`。

### B2 命令工具的 `Execute` 是占位（`discovery.go:204`）

自动发现的命令工具，`Execute` 只返回占位串 `[命令 X 已触发]`；**真正执行**走
`executeRealCommand`（合成事件 + `captureSender` 捕获输出，`execute.go:126`）。

### B3 普通 Action：`Execute` 闭包真执行

最常见的一类，直接调用 Go 闭包。

### B4 Skill：`Execute` 启动子代理 LLM 循环（`execute.go:159`）

`executeSkill` 内部持有自己的 LLM 循环与 tool set；它不是普通函数执行。

### B5 工具失败判定（`retry.go:25`）

冻结时的实现：工具失败结果统一以 `错误:` / `错误：` 前缀开头，`isToolErrorResult`
依赖该约定判定"模型可见失败"，进而驱动重试与反思引导。**这是脆弱点**：退役依赖
`ActionResult.Err`（26 的 D6 / Phase 4a），但 Phase 0 必须先把它**冻结成测试**。

> 后续状态（见 `28-ai-layering-progress.md` 第十五节）：失败判定已改为类型化
> `ActionResult.Err`，`isToolErrorResult` 已删除；结果**文本前缀保留**（可观测行为不变），
> 冻结用例随之改名为 `TestToolResultFailureIsTyped`。

### B6 输出旁路捕获：`captureSender`（`execute.go:29`）

`captureSender` 实现 `platform.Sender`，拦截 handler 的 `Send` 调用，记录文本内容与
**附件**，供工具结果回填。附件合并行为必须冻结（见 GAP-8），退役评估单列 Phase 4b。

### B7 协议层三态统一

模型看到的三种来源**在协议层完全一致**（都是 `function/tool`），差异只在内部执行语义。
冻结要求：内部拆 `Invoker` 时**不得**改动协议层 schema。

### B8 Skill 循环非流式、自维护 tool set（`execute.go:165,197`）

`buildSkillTools(skill)` = 自己的 Tools + 其他系统 Skill；子代理循环非流式。

调用记账：`executeSkill` 入口按技能自身 owner + name 记一次 `IncrementUsage`，
嵌套调用同样计被执行的技能；未注册技能无可增键、只执行不计数
（`TestSkillLoopCountsUsage`）。

## 五、C — 检索契约（Retrieval）

### C1 权重与门槛常量冻结

| 常量 | 值 | 位置 |
|------|----|------|
| `scoreEmbedW` | `2.0` | `select.go:37` |
| `ragKeywordMinScore` | `2` | `rag.go:38` |
| `memoryMergeJaccard` | `0.6` | `memory.go:37` |
| `selectionCacheJaccard` | `0.5` | `select.go:46` |
| `selectionCacheTTL` | `10 * time.Minute` | `select.go:43` |

### C2 Tools 检索质量基线

`contract_test.go` + `testdata/retrieval_cases.json` 锁定 Top1 / Recall@K / MRR。

### C3 RAG 检索骨架（`rag.go`）

两段式：关键词预筛（`tokenOverlap ≥ ragKeywordMinScore`）→ 叠加
`cosineSimilarity × scoreEmbedW` 后排序。

### C4 Memory 检索骨架（`memory.go`）

Jaccard ≥ `memoryMergeJaccard` 视为同义合并；检索时叠加 `cosine × scoreEmbedW`。

### C5 三者共享骨架、各自持有参数

`tokenize → candidate → prefilter → score → rank → top-K/budget` 结构相同，
**参数与门槛各自维护**（`rag.go:240` 等）。Phase 5 抽 `Retrieval` 时：共享算法骨架，
**不共享领域模型**（见 26 §3.5）。

### C6 契约测试是 env-gated

embedding 分支依赖 `REMILIA_EMBED_TEST_URL`；未设置时跳过 embedding，只跑关键词/结构分支。

## 六、D — 前缀缓存（Prompt Prefix）

### D1 稳定前缀

```
Stable Prefix
├── System
├── Tools        （ToolSet 决定）
└── History
```

### D2 Dynamic Context 只挂在用户消息尾部

`buildDynamicContext` 的输出附在**本轮用户消息尾部**，不写回会话历史、不进 System 消息
（`handler.go`）。

### D3 动态上下文变化不改变 History 前缀 ⭐

`Memory / RAG / Runtime / Group` 内容变化 **不改变** History 之前的前缀字节。
这**不是普通优化，而是 Context Pipeline 的基础约束**：

> 任何 `ContextProvider` 新增的数据，默认不得进入 Stable Prefix（suffix-only）。

### D4 ToolSet 变化必须产生新的请求前缀边界 ⭐

`ToolSet` 一变，`tools` 段序列化字节即变，前缀随之改变。**冻结的是字节结构，不是 Provider 缓存命中**：
测试应断言"两个 ToolSet → 两个不同的 serialized request prefix"，
**不得**断言 "provider cache hit == false"（缓存命中是 Provider 行为，不由本插件契约保证）。

### D5 `ToolSet` 是唯一跨 History 边界的动态变量

动态状态里只有 `ToolSet` 位于 History **之前**；其余动态内容全部后置。因此 ToolSet Jitter
是当前 Prompt Cache 的主要结构性扰动源（26 P3）。

## 七、E — 策略闸门（Policy Gate）

### E1 可用集先过群策略（`decision.go` `actionCandidates`）

`gp.FilterTools(actions)`（群策略方法 `GroupPolicy.FilterTools`）定义本轮**可用边界**。

### E2 RBAC 再过滤（`process.go:130`）

`filterToolsByPermission(ctx, activeTools)` 在群策略结果内进一步按角色/权限过滤。

### E3 审批闸门（`process.go:513-527`）

`approvalModeFor` 决定某工具是否需用户批准；`EffectiveApprovalMode = 群策略 > 全局配置 > 默认 off`；
`AlwaysRequireApproval` 工具（如 `send_to`）在 `off` 模式下**仍强制审批**。
审批授予是 **request-scoped**（随本轮 TurnContext 生效）——这正是 Phase 1 `Capability`
改造必须保持的行为。

### E4 过滤语义（不把"顺序"当独立契约）

冻结的语义是：**群策略定义可用边界，RBAC 在可用集内再过滤。**

群策略与 RBAC **都是集合交集**（`A ∩ B == B ∩ A`），最终工具集**无法观察执行顺序**。
因此不冻结"先群策略后 RBAC"这种顺序，只冻结：

- 二者都必须生效；
- 结果始终是 `可用集` 的子集；
- 权限收缩**立即**反映到本轮 ToolSet（配合 A11 / N2）。

（代码里确实是 group → rbac，但那是实现事实，不是可观测契约。）

### E5 执行前校验顺序

`execOneTool`（`process.go:416-429`）：先 RBAC 权限校验，再命令审批——
保证**无权工具不会进入审批流程**（否则会向用户暴露不该看到的工具）。

## 八、F — 运行时语义（Runtime）

### F1 回合超时

`turn_timeout`（`config.go:290`）以插件独立预算替换全局 30s 中间件超时
（`process.go:101-104`，`effectiveTurnTimeout`）。

### F2 工具超时

`tool_timeout`，默认 `30s`（`config.go:83`）。

### F3 重试预算

`effectiveToolRetryLimit()`（`process.go:552`）：≤0 时用默认 `2`；成功执行后失败计数归零
（`session.go:66`）。

### F4 中断 / 抢占

`BeginTurn` / `RequestInterrupt` / `EndTurn`（`session.go:47-52`）；
`processWithTools` 在**轮次之间与工具之间**检查中断信号。

### F5 并行执行

`tool_parallel`（默认 `4`，`config.go:269`）。一轮内**全部**工具调用并行执行，
信号量控制并发度（`process.go:377-403`）；并发度 `≤1` 或调用数 `≤1` 时**退化为顺序执行**
（行为与旧版一致）。

### F6 Skill 循环非流式

见 B8——子代理循环不走主循环的流式路径。

## 九、G — 配置 / 指标 / 持久化

### G1 配置键与默认值冻结

| 键 | 默认值 | 位置 |
|----|--------|------|
| `tool_select_max` | `20` | `config.go:189` |
| `tool_budget` | `8000` | `config.go:193` |
| `tool_set_sticky` | `true` | `config.go:202` |
| `tool_set_sticky_max` | `8` | `config.go:208` |
| `tool_parallel` | `4` | `config.go:271` |
| `tool_approval` | `off`（`off`/`restricted`/`always`） | `config.go:183` |
| `approval_timeout` | `60s` | `config.go:185` |
| `api_timeout` | `60s` | `config.go:78` |
| `tool_timeout` | `30s` | `config.go:83` |
| `turn_timeout` | 见 `config.go:290` | |

**旧键语义不得静默改变**：`tool_select_max` 永远保持"短路阈值 + 补充项上限"语义，
不做"最终硬上限"。新增量通过**新增键**引入（如未来的 `candidate_limit` /
`hard_tool_limit`）。

### G2 指标描述符冻结（`metrics.go:37-87`）

```
llm_calls_total
llm_latency_seconds
llm_tokens_total
tool_calls_total{tool,result}
toolset_changes_total{reason}     // reason: init/grow/shrink/decay
toolset_size
```

**名字与现有 label 只增不改**：不得把 `tool_calls_total` 改成 `action_calls_total`
（否则生产监控"归零"）。新增指标可以加（如 `action_decisions_total`、`retrieval_latency`）。

### G3 Session 持久化字段集合与 round-trip

持久化实体是 `sessionRecord`（`session.go:691-704`），字段集合恰为：

```
ID, UserID, ChatID, Messages, CallCount, ToolCount,
Plan, PendingImages, CreatedAt, UpdatedAt
```

`Save` 经 `Session.toRecord()`（Messages 过 `messagesForPersistence`、Plan/PendingImage 过 JSON），
`Load` 经 `record.toSession()`。冻结：**字段集合 + JSON round-trip 行为**。

> 注意：`Session` 上 `plan` / `pendingImage` 标着 `json:"-"`，但它们**确实被持久化**——
> 因为走的是 `toRecord` 显式转换，不是 `Session` 直接序列化。冻结时以 `sessionRecord` 为准。

### G4 当前**没有** `SchemaVersion` 字段

`sessionRecord` 里不存在版本字段。**Phase 0 不引入它**（引入版本号 = 改结构，属 Phase 8 Agent State）。
Phase 0 只冻结"字段集合 + round-trip"这一现状快照（GAP-10）。

## 十、负向不变量（Negative Invariants）

正向测试说"应该发生什么"；负向测试说"**绝对不能发生什么**"。这组对缓存问题尤其关键。

| # | 不变量 | 关联 |
|---|--------|------|
| **N1** | 动态上下文变化 **≠** History 前缀变化（字节完全相同） | D2/D3 |
| **N2** | 权限收缩 → sticky 工具**必须立即消失**（不得被 sticky 保留） | A11、E2 |
| **N3** | `NeedAction=false` → Mandatory / Baseline / SessionUsed / Sticky **不得消失** | A2/A4/A8、26 §3.10 |
| **N4** | ToolSet 不变 → 序列化 tools 字节不变 | A13 |
| **N5** | Candidate 不变 + available 不变 → ToolSet `Generation` 不变 | A13 |
| **N6** | 同 ToolSet + 同 History + 不同动态上下文 → stable prefix 字节**完全相同** | D3/D5 |

N2/N3 是历史上最容易踩的两个坑：`NeedAction=false` 被实现成 `tools = nil`、
或 sticky 无视权限收缩继续保留无权工具。

## 十一、测试缺口清单（GAP-1 … GAP-10）

| 缺口 | 说明 | 建议用例 | 落点 | 阻塞 Phase |
|------|------|----------|------|-----------|
| **GAP-1** | 指标描述符注册无测试 | `TestMetricsDescriptorsRegistered` | `metrics.go` | 2 / 7 |
| **GAP-2** | `tool_select_max` 配置兼容无测试 | `TestToolSelectMaxConfigCompat` | `config.go` | 2 |
| **GAP-3** | 必保集可超 `ToolSelectMax` 无测试 | `TestMandatoryMayExceedSelectMax` | `select.go` | 2 / 7 |
| **GAP-4** | 空 `Categories` 视为必保无测试 | `TestEmptyCategoryIsMandatory` | `select.go` | 2 |
| **GAP-5** | 动态上下文后置隔离无测试 | `TestDynamicContextChangeKeepsPrefix` | `handler.go` | 6 |
| **GAP-6** | ToolSet 变化 → 新前缀边界无测试 | `TestToolSetChangeCreatesDistinctPrefix` | `prompt.go` | 6 / 7 |
| **GAP-7** | 过滤顺序不可观测 | 见备注 | `process.go` | — |
| **GAP-8** | `captureSender` 附件合并无测试 | `TestCaptureSenderAttachmentsMerged` | `execute.go` | 4b |
| **GAP-9** | 检索权重常量冻结无测试 | `TestRetrievalWeightsFrozen` | C1 | 5 |
| **GAP-10** | Session 持久化 schema 快照无测试 | `TestSessionPersistenceSchemaSnapshot` | `session.go` | 8 |

**GAP-7 备注**：群策略与 RBAC **都是集合交集**，最终结果**无法观察执行顺序**，
因此不应把"顺序"当独立契约（对齐 E4）。若确需验证顺序，只能靠 filter 输入集合
或显式 pipeline trace，属于观测增强而非行为契约；Phase 0 不补此测试。

**GAP-10 备注**：当前**没有** `SchemaVersion` 字段，只冻结"持久化字段集合 + JSON round-trip"。
引入版本号属 Phase 8。

## 十二、§H 优先补齐的测试（Phase 0 直接可写）

> 全部为**新增测试**，不改生产代码。构造方式尽量用最小输入，断言尽量用"逐元素相等"
> 与"字节相等"，避免依赖耗时或随机。
>
> 共 **20 条**：第 1–17 条为契约用例，第 18–20 条为负向不变量用例（N2 / N3 / N5）。
> 用例名与冻结时的拟名可能有差异（后续变更改名），表中已标注现名。

### 选择与稳定

1. **`TestSelectBypassWhenToolsWithinMax`（A1）**
   构造 5 个工具、`ToolSelectMax=5`、embedding 关闭 → `selectToolsForTurn` 返回值与输入
   **逐元素相同**，且 **0 次 embedding 调用**。
2. **`TestMandatoryMayExceedSelectMax`（A2 / A9 / GAP-3）**
   `ToolSelectMax=1`，3 个 general（空 Categories）+ 5 个普通工具 → 结果 `len ≥ 3`，
   且**包含全部 3 个 general**。
3. **`TestEmptyCategoryIsMandatory`（A3 / GAP-4）**
   `Categories == nil` 与 `Categories == ["general"]` 都判为必保；`["web"]` 不判为必保。
4. **`TestBudgetDoesNotDropMandatory`（A7）**
   令 `tool_budget` 极小 → 必保集**不减少**；被裁掉的只可能是补充项 / sticky fillers。
5. **`TestEmbeddingCacheNoReembed`（A6）**
   mock embedder 计数；同一 query 连续两次选择 → embed 调用次数**不翻倍**。
6. **`TestToolSetDeterministicOrdering`（A13 / N4）**
   同一组工具以不同输入顺序给出 → 输出**名称升序**一致；
   `serialize(tools)` 两次调用**字节相等**。

### 执行三态

7. **`TestCommandToolExecuteIsPlaceholder`（B2）**
   自动发现命令工具 → 其 `Execute(...)` 返回 `[命令 X 已触发]`，且**不产生真实副作用**。
8. **`TestExecuteToolResolutionOrder`（B1）**
   同名冲突构造：owner Skill / system Skill / real command / 普通 Execute 四种来源 →
   分派命中 owner 优先。
9. **`TestCaptureSenderAttachmentsMerged`（B6 / GAP-8）**
   handler 通过 `captureSender` 发送含附件消息 → 附件出现在工具结果中，文本与附件都不丢。
10. **`TestToolResultFailureIsTyped`（B5）**（冻结时名为 `TestToolResultErrorPrefix`）
    失败判定只认类型化 `ActionResult.Err`；正文以 `错误:` 开头但 `Err` 为空**不算失败**
    （文本前缀仍逐字冻结，供模型阅读）。
11. **`TestSkillLoopNonStreaming`（B8 / F6）**
    子代理执行 Skill → 走非流式路径，且 `buildSkillTools` 含"自有工具 + 其他系统 Skill"。

### 前缀缓存

12. **`TestDynamicContextChangeKeepsPrefix`（D3 / N1 / N6 / GAP-5）**
    固定 System + Tools + History，只改 Memory/RAG/Runtime/Group 内容 →
    → stable prefix 字节**完全相同**，仅用户消息尾部变化。
13. **`TestToolSetChangeCreatesDistinctPrefix`（D4 / GAP-6）**
    构建两个不同 ToolSet 的请求 → 两者 serialized prefix **不同**。
    **不断言** Provider cache hit/miss。

### 配置 / 指标 / 持久化

14. **`TestMetricsDescriptorsRegistered`（G2 / GAP-1）**
    注册表包含 6 个指标名与既有 label 集合。
15. **`TestToolSelectMaxConfigCompat`（G1 / GAP-2）**
    旧键 `tool_select_max` 生效后语义仍为"短路阈值 + 补充项上限"。
16. **`TestRetrievalWeightsFrozen`（C1 / GAP-9）**
    `scoreEmbedW / ragKeywordMinScore / memoryMergeJaccard / selectionCacheJaccard /
    selectionCacheTTL` 值锁定。
17. **`TestSessionPersistenceSchemaSnapshot`（G3 / G4 / GAP-10）**
    `sessionRecord` 字段集合**恰为** `{ID, UserID, ChatID, Messages, CallCount, ToolCount,
    Plan, PendingImages, CreatedAt, UpdatedAt}`，**无** `SchemaVersion`；
    `toRecord → toSession` round-trip 保持语义。

### 负向不变量

18. **`TestToolSetStickyNeverExceedsAvailable`（N2 / A11）**（冻结时拟名 `TestPermissionShrinkRemovesSticky`）
    sticky 中已含工具 T；下一轮 `available` 去掉 T → 结果**不含 T**。
19. **`TestDecideTurnActionsRetainsStickyWhenNoAction`（N3）**（冻结时拟名 `TestNeedActionKeepsBaseline`）
    `NeedAction=false` → Mandatory / Baseline / SessionUsed / Sticky **一个都不少**。
20. **`TestToolSetGenerationStableOnRepeatedCandidate`（N5 / A13）**
    Candidate 与 available 不变 → `Generation` 不变、`reason == "keep"`。

> 可选补充（非阻塞）：`TestStickyTTLDecayReason`（A12，断言 `reason=decay`）、
> `TestParallelDegradesToSequential`（F5，并发度 1 时顺序执行）、
> `TestTurnTimeoutOverridesMiddleware`（F1）。

## 十三、执行方式

```powershell
# 全部 AI 插件测试
go test ./builtin/ai/...

# 检索契约测试（embedding 分支需要服务地址）
$env:REMILIA_EMBED_TEST_URL = "http://127.0.0.1:8080/v1/embeddings"
go test ./builtin/ai/... -run 'Contract|Retrieval'
```

未设置 `REMILIA_EMBED_TEST_URL` 时只跑关键词/结构分支（C6）。

## 十四、Phase 0 完成判据

1. A–G 全部契约项**要么有对应测试，要么显式标注"仅记录不测试"**（当前仅 G4 属后者）。
2. 负向不变量 **N1–N6 全绿**。
3. §H 的 20 条用例全部落地并通过（17 条契约用例 + 3 条负向不变量用例）。
4. `go test ./builtin/ai/...` 全绿；设置 `REMILIA_EMBED_TEST_URL` 后 embedding 契约测试全绿。
   已实测通过（本地 `Qwen3-Embedding-0.6B-Q8_0.gguf`，指标见 `28` 第二十一节）。
5. **未改动任何生产代码**：`git diff --stat -- builtin/ai` 只出现 `*_test.go`
   （以及 `testdata/`）。该判据只约束冻结这一步；后续工作流按各自目标改动生产代码
   （见 `28-ai-layering-progress.md`）。

## 十五、冻结项 ↔ Phase 映射

| Phase | 内容 | 必须保持绿的冻结项 |
|-------|------|-------------------|
| **0** | 冻结现状 | A–G 全部 + N1–N6 |
| **1** | Capability | **E3** |
| **2** | Action / `SelectionClass` / `ActionResult` | A3、A6、G1（GAP-2）、GAP-4、C1（GAP-9） |
| **3** | Invoker | B1、B2、B3、B4、B5、B7 |
| **4a** | `ActionResult` 收敛 | B5（退役字符串判定） |
| **4b** | `captureSender` 退役评估 | B6、GAP-8 |
| **5** | Retrieval | C2、C3、C4、C5、C6 |
| **6** | Context | D1–D5 |
| **7** | Selection + Decision | A1–A13、E1–E5、N2、N3、N5 |
| **8** | Agent State | G3、G4（GAP-10） |
| **9** | Jev | 无新增冻结项 |

> 附注：**Phase 1 只绑 E3**。Session 持久化（G3/G4）与 Capability 无直接关系，
> 归 **Phase 8 Agent State**——不要在 Phase 1 顺势改动 Session 结构。

## 修订记录

| 版本 | 修订 |
|------|------|
| v1 | 初次定稿：冻结原则、A–G 契约表、缺口清单 G1–G10、完成判据、Phase 映射 |
| v2 | 落 GPT 审查：① A7 补"预算不回收必保集"；② A9 改 `Candidate ∪ StickyFillers` 精确语义；③ E4 改为"群策略定义可用边界、RBAC 在可用集内过滤"，不再把执行顺序当独立契约；④ G3 改为持久化字段集合 + round-trip；⑤ G4 明确当前无 `SchemaVersion`，引入版本号属 Phase 8；⑥ 缺口编号 `G1–G10` → `GAP-1–GAP-10`，消除与契约分组冲突；⑦ 新增负向不变量 N1–N6；⑧ 新增 §H 优先补齐测试 17 例；⑨ Phase 映射修正：Phase 1 只绑 E3，Session 归 Phase 8 |
| v3 | 与实际落地对齐（仅文档）：§H 用例数 17 → 20（17 契约 + 3 负向）并单列负向分组；用例现名标注（`TestToolResultFailureIsTyped`、`TestToolSetStickyNeverExceedsAvailable`、`TestDecideTurnActionsRetainsStickyWhenNoAction`、`TestToolSetGenerationStableOnRepeatedCandidate`）；B5 补"失败判定已类型化、文本前缀保留"的后续状态；§十四 判据 3/5 口径同步 |
