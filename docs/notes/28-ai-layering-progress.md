# 28 — AI 插件分层重构：执行进度与对照基线

> 配套 [26 — AI 插件分层](26-ai-layering.md)（目标架构）与
> [27 — 现状行为冻结清单](27-ai-freeze-checklist.md)（不变量与测试缺口）。
>
> 本文是**长期维护的执行记录**：已完成内容、当前状态、遗留问题、下一步计划。
> 每次提交前更新，作为"改动是否仍在既定轨道上"的唯一对照。
>
> 工作分支：`refactor/ai-layer-boundaries`（主干迁移）→ `fix/ai-behavior-backlog`（行为修正、包边界收口与标记订正）。
> 硬约束：只换实现写法，不改可观测行为；每步以全量测试回归；提交粒度 = 一个工作流。

## 一、对照基线（重构前的测试快照）

在 `master`（提交 `5d5281f`）上执行：

```powershell
go test ./builtin/ai/... -count=1
go test ./builtin/ai/... -count=1 -v
```

| 指标 | 数值 |
|------|------|
| 结果 | `ok` |
| 通过用例 | **526** |
| 失败用例 | **0** |
| 跳过用例 | **1**（`TestRetrievalContractLive`，需 `REMILIA_EMBED_TEST_URL`） |
| 包耗时 | 约 10–15s（冷跑 ≈ 28s） |

> ⚠️ 基线是**回归标准**：任何一次提交后，上述数字与结果都不得劣化。
> 若某次改动导致用例数变化，必须在本文记录原因（新增用例属预期增量）。

## 二、工作流与状态

状态图例：✅ 完成 · 🚧 进行中 · ⏳ 未开始

| # | 工作流（对应 26 迁移顺序） | 目标 | 状态 |
|---|---------------------------|------|------|
| 1 | 能力端口（Capability） | 消除 `toolSource → *Plugin`，工具只依赖最小端口 | ✅ |
| 2 | 动作描述（Action） | `ActionSpec` + `ActionPolicy` + `SelectionClass` | ✅ |
| 3 | 调用器（Invoker） | `FuncInvoker` / `CommandInvoker` / `SkillInvoker` | ✅ |
| 4a | 结果收敛（Result） | typed `Err` 退役 `isToolErrorResult` 字符串判定 | ✅ 已由 C1 交付（见第十五节） |
| 4b | 输出捕获退役评估 | `captureSender` 依赖 vevent/engine；评估完成：附件在回合级捕获，整段退役不可行 | ✅ 已裁决：不退役（见 L6） |
| 5 | 检索骨架（Retrieval） | `RetrievalCore`：三个消费者共享分词/向量获取/排序/截断骨架 | ✅ |
| 6 | 上下文管线（Context） | Provider + Builder（未引入 Fragment/Snapshot 类型，见第二十一节） | ✅ |
| 7 | 选择与决策（Selection + Decision） | Candidate Discovery / Retrieval / Selection / Stability + `needAction` gate | ✅（闸门当前保守恒真） |
| 8 | 代理状态（Agent State） | Plan / Todo / Reminder / PendingImage / Interrupt 的逻辑归属与持久化分离 | ✅ |
| 9 | Jev | 二级决策器（仅处理规则无法判定的歧义决策） | ⏳ 按需（真实需求出现再做） |
| F | 冻结用例（27 §H） | 现状契约的可执行断言 | ✅ 20/20（17 契约 + 3 负向） |

## 三、已完成：能力端口（Capability）

**提交**：`refactor(ai): 工具能力端口化，消除工具上下文对插件实例的反向依赖`

### 改动范围

| 文件 | 改动 |
|------|------|
| `builtin/ai/capability.go`（新增） | 定义 `memoryPort` / `todoPort` / `reminderPort` / `reminderSchedulerPort` 四个最小端口与请求级 `capabilitySet`；组合根 `(*Plugin).toolCapabilities()`；`withCapabilities` / `capabilitiesFromContext` |
| `builtin/ai/toolctx.go` | `toolSource` 移除 `p *Plugin` 字段，只保留身份与平台发送器 |
| `builtin/ai/execute.go` | `executeTool` 注入 `p.toolCapabilities()` |
| `builtin/ai/memorytool.go` | 三个工具改依赖 `caps.Memory` |
| `builtin/ai/todotool.go` | 四个工具改依赖 `caps.Todos` |
| `builtin/ai/remindtool.go` | `set_reminder` 依赖 `caps.Scheduler`，`list/cancel` 依赖 `caps.Reminders` |
| `builtin/ai/testutil_test.go` | 新增 `toolCtxForTest` 辅助（装配身份 + 能力端口） |
| `builtin/ai/capability_test.go`（新增） | 端口装配与 nil 安全回归测试 |
| 三个工具测试文件 | 上下文构造改为 `toolCtxForTest` |

### 设计要点

- **端口是最小职责**：只读/取消提醒（`reminderPort`）与创建提醒（`reminderSchedulerPort`）刻意分开，
  因为创建需要捕获平台发送器。
- **组合根隔离**：`pluginReminderScheduler` 是唯一仍持有 `*Plugin` 的适配器，
  它位于装配边界，工具侧完全看不到插件实例。
- **nil 安全**：`toolCapabilities()` 只在具体管理器非 nil 时赋值端口，
  避免"类型化 nil 指针 → 非 nil 接口"导致工具降级分支失效（有专项测试守护）。

### 验证

- `go build ./builtin/ai/...`、`go vet ./builtin/ai/...` 通过。
- `go test ./builtin/ai/... -count=1` → `ok`，无失败。
- 新增 `TestToolCapabilitiesNilSafety` / `TestToolCapabilitiesComposition` /
  `TestCapabilitiesContextRoundTrip` 通过。

### 行为影响

无。工具的错误文案、降级条件、返回值与改动前逐字一致；
仅上下文中的能力获取方式由"插件实例"改为"注入端口"。

## 四、遗留问题

| # | 问题 | 状态 |
|---|------|------|
| L1 | `loopToolSender`（`sendtool.go`）仍持有 `*Plugin` | 可接受：它已在窄接口 `ToolSender` 之后，属装配层 |
| L2 | 27 的 §H 冻结用例 | **已完成**：20/20 全部落地（17 契约 + 3 负向，见第十四节与第二十一节） |
| L3 | `ActionSpec` 命名需与 `platform.Capabilities` / `plugin.TryService` 区隔 | ✅ 已裁决：定名 `ActionSpec`（工作流 2，见第十三节） |
| L4 | 结果收敛（4a）会改变"命令/Skill 正常输出恰好以 `错误:` 开头"的归类 | ✅ 已由 C1 交付（见第十五节） |
| L5 | 审批被拒文案"工具已被用户拒绝执行"当前不算失败（无 `错误:` 前缀） | 属既有行为，保持不动 |
| L6 | 附件在回合级而非工具级捕获，`ActionResult` 暂不含 `Attachments` | 记录：4b 的退役范围因此收窄 |
| L7 | `builtin/knowledgebase/retrieve.go` 自行实现 `tokenize` / `tokenOverlap` / `tokenJaccard`（已复用 `ai.CosineSimilarity`） | ✅ 已实施：导出 `ai.TokenizeText` / `TokenOverlap` / `TokenJaccard` 并删除本地实现（见第二十节） |
| L8 | `needAction` 闸门当前保守恒为真（不抑制可选动作） | 收紧判定（如纯闲聊/纯知识问答判否）会改变发给模型的工具集，属独立变更；本工作流只交付闸门骨架与保留语义 |

### 已裁决事项

- ~~**Q1**~~（已裁决）：**保留兼容兜底，保持现有可观测行为。**
  退役 `错误:` 前缀判定不作为迁移的一部分，转入"行为修正待办"。
- ~~**Q2**~~（已解决）：`ActionSpec` / `ActionPolicy` 已引入，并以选择、执行闸门、
  协议序列化为真实消费者，符合不变量 9。

## 四之二、行为修正待办（与迁移分离）

> 原则：**不把"架构收敛"和"行为修正"混在一起。** 迁移只换写法；
> 行为修正单列，等迁移完成后再作为明确变更实施。这样回归出现差异时，
> 一定是迁移实现的问题，而不是"顺便改了一个旧行为"。

| # | 待修正行为 | 现状（本次保持不变） | 期望（后续实施） | 决策 |
|---|-----------|---------------------|-----------------|------|
| C1 | 失败判定依赖 `错误:` 字符串前缀 | `isToolErrorResult` 按前缀判定；命令/Skill 正常输出若以 `错误:` 开头会被判为失败；审批被拒文案不算失败 | 改用 `ActionResult.Err` 类型判定 | ✅ 已实施（见第十五节） |
| C2 | 空运行时上下文节的处理不对称 | `include_runtime_context=true` 且 `context_fields` 排除全部字段时：非预算路径输出"只有标题的空节"；预算路径在装入其它非空节时丢弃该节 | 两条路径统一为不输出空节 | ✅ 已实施（见第十六节） |
| C3 | 历史检索同分排序无确定次序 | 历史消息检索的两次排序都只按分数降序（同分不设次序）；工具选择与记忆检索同分时分别按工具名、事实文本升序 | 统一为确定性次序 | ✅ 已实施（见第十七节） |
| C4 | 群聊窗口的开关在两条路径上不一致 | `context_group_messages=0` 表示关闭：非预算路径据此关闭；预算路径不受该开关约束，按默认 10 条纳入 | 两路径统一遵循同一开关 | ✅ 已实施（见第十八节） |
| C5 | `send_to` 的审批门被判定两次 | `execOneTool` 先按审批模式判定，再把结果注入 `loopToolSender.sendToAllowed`，`SendTo` 内再次判定（防嵌套 Skill 绕过） | 收敛为单一闸门（26 迁移表目标） | ✅ 已实施（见第十九节，授权单一来源、行为保持） |

> 两项均不阻塞迁移：C1 不实现（保持前缀判定）；C2 在上下文管线 Provider 化时
> **逐字保留差异（含"只有标题的空节"）**。
> C3 迁移期曾由 `rankByScore(..., nil)` 显式承接，现已按第十七节统一为确定性次序。

> C5 已按"授权单一来源、行为保持"落定（见第十九节）；
> L7 已按"导出 `ai` 检索原语"落定（见第二十节）。行为修正待办 C1–C5 与 L7 全部完成。


## 五、下一步

1. 工作流 8（代理状态）：Plan / Todo / Reminder / PendingImage / Interrupt 的逻辑归属，
   与持久化 schema 分离（逻辑归属≠持久化 schema）。
2. 逐步补 27 §H 余下冻结用例（B1/B2 来源解析顺序与命令占位串）。
3. 迁移完成后再统一评估行为修正待办 C1–C5（错误前缀判定、空节、同分次序、
   群聊开关、`send_to` 双重审批）与 L8（收紧 `needAction`）。

工作流推进顺序（已确认）：**检索骨架 → 上下文管线 → 选择与决策 → 代理状态**，
后者消费前者，依赖关系最干净。

## 六、已完成：调用器（Invoker）

**提交**：`refactor(ai): 拆分动作调用器，三种执行语义各自承载`

### 改动范围

| 文件 | 改动 |
|------|------|
| `builtin/ai/invoker.go`（新增） | `ActionResult{Text, Err}`；`invoker` 接口；`funcInvoker` / `skillInvoker` / `commandInvoker`；`toolFailureText` |
| `builtin/ai/execute.go` | `executeTool` 改为"解析来源 + 施加闸门"，执行委托给对应调用器；移除行内错误格式化 |
| `builtin/ai/invoker_test.go`（新增） | 三类调用器的成功/失败/超时文案与回退语义回归测试 |

### 设计要点

- **三种语义各自承载**：普通动作真执行、命令走合成事件、Skill 启动子代理循环，
  不再由同一个 `Execute` 字段假装一致。
- **解析与执行分离**：来源解析顺序（Skill(owner) → Skill(system) → real command → Execute）
  与权限闸门仍留在 `executeTool`，调用器只负责"调用并产出结果"。
- **串行化不内移**：真实命令路径的 `realCmdMu` 加锁仍在调用方，避免调用器承担并发职责。
- **`ActionResult.Err` 只表示失败语义**：底层错误链已格式化进 `Text`，不额外暴露。

### 行为影响

无。失败/超时/权限/未找到的文案与改动前逐字一致；`RecordToolCall` 的调用位置与次数不变；
真实命令路径"捕获为空则回退"的语义不变。

### 验证

- `go build` / `go vet` 通过；`go test ./builtin/ai/... -count=1` → `ok`。
- 新增 6 个调用器用例通过。

## 七、已完成：动作描述（Action）

**提交**：`refactor(ai): 引入动作描述与策略，拆分"类别"与"必保"双重语义`

### 改动范围

| 文件 | 改动 |
|------|------|
| `builtin/ai/action.go`（新增） | `SelectionClass`（Optional/Baseline/Mandatory）；`ActionSpec`；`ActionPolicy`；`actionSpecOf` / `actionPolicyOf` / `selectionClassOf`；`keepsWhenNoAction` |
| `builtin/ai/select.go` | 相关度打分改用动作描述；必保判定改用保留级别 |
| `builtin/ai/execute.go` | 工具级权限校验改用动作策略 |
| `builtin/ai/process.go` | 执行前权限预检、`AlwaysRequireApproval` 判定改用动作策略 |
| `builtin/ai/tool.go` | OpenAI / Anthropic 协议序列化改用动作描述 |
| `builtin/ai/action_test.go`（新增） | 映射等价性、无损派生、序列化消费者、内置规划工具保留级别 |

### 设计要点

- **拆开双重语义**：类别（`ActionSpec.Categories`）回答"这是什么"；
  保留级别（`ActionPolicy.Selection`）回答"该不该留下"。
  "空类别视为通用"只由 `selectionClassOf` 定义一次，不再散落在选择逻辑里。
- **无新存储、纯派生**：`Action` 由 `Tool` 保真派生，注册表仍存 `Tool`；
  等消费者稳定后再迁移存储（新抽象只增不减，可独立回滚）。
- **保留级别三分**：`Optional` / `Baseline` / `Mandatory`。
  `Baseline` 与 `Mandatory` 在"本轮无需主动动作"时都必须保留——
  这为后续 `needAction` 闸门预留了正确语义（对应 26 §3.10）。

### 行为影响

无。保留级别映射与旧 `isGeneralTool` 逐一等价（有等价性断言守护）；
权限、审批、协议序列化取值逐字段不变。

### 验证

- `go build` / `go vet` 通过；`go test ./builtin/ai/... -count=1` → `ok`。
- 新增 6 个用例通过。

## 八、已完成：冻结用例（27 §H 第一批）

**提交**：`test(ai): 补齐现状行为冻结用例（选择语义/指标/权重/持久化/前缀边界）`

新增 `builtin/ai/freeze_test.go`，把 27 §H 中缺失且高价值的契约变成可执行断言：

| 用例 | 覆盖 |
|------|------|
| `TestSelectBypassWhenToolsWithinMax` | A1 短路阈值：不超上限且无 embedding 时原样返回 |
| `TestMandatoryMayExceedSelectMax` | A2/A9 默认保留级别不受 `ToolSelectMax` 约束 |
| `TestBudgetDoesNotDropMandatory` | A7 预算不回收默认保留级别 |
| `TestMetricsDescriptorsRegistered` | G2 指标族名与标签集合只增不改 |
| `TestToolSelectMaxConfigCompat` | G1 旧配置键语义 |
| `TestRetrievalWeightsFrozen` | C1 权重与门槛常量 |
| `TestSessionPersistenceSchemaSnapshot` | G3/G4 持久化字段集合、且当前无版本字段（round-trip 见 `TestAgentStatePersistenceRoundTrip`） |
| `TestToolSetGenerationStableOnRepeatedCandidate` | N5/A13 集合代数稳定 |
| `TestToolSetChangeCreatesDistinctToolsBytes` | D4/N4 前缀字节边界（不断言 Provider 缓存） |

### 已由既有测试覆盖、无需重复的项

| 契约 | 既有用例 |
|------|----------|
| A3 空类别视为通用 | `TestIsGeneralTool`、`TestSelectionClassMapping` |
| A6 embedding 结果缓存 | `TestSelectToolsForTurnCacheHit` |
| A10/A12 单调并集、回访保持、字节稳定 | `TestToolSetStickyGrowsMonotonicallyAndKeepsOnRevisit`、`TestToolSetStickyIsByteStableAcrossRepeatTurns` |
| A11/N2 可用集收缩立即生效 | `TestToolSetStickyNeverExceedsAvailable` |
| A12 空闲衰减 | `TestToolSetStickyDecaysIdleFillers` |
| D1–D3 稳定前缀与动态上下文后置 | `TestPromptPrefixStableAcrossTurns`、`TestInjectDynamicContextAttachesToLastUserMessage` |
| G3 持久化 round-trip | `TestSessionToRecordRoundTrip`、`TestSessionRecordJSONRoundTrip` |

### 当时仍待补的条目（已全部交付，保留作历史记录）

> 下表是本节当时的缺口清单：`N3`、`B5`、`B1/B2` 均已在第十四节随冻结用例补齐（§H 20/20）交付，不再是未完成项。

| 契约 | 当时的阻塞原因 | 交付位置 |
|------|------------------|----------|
| N3 `NeedAction=false` 保留基线动作 | `needAction` 闸门属选择与决策工作流 | `TestDecideTurnActionsRetainsStickyWhenNoAction`（第十四节） |
| B5 工具错误前缀 | 与 Q1 结果收敛同时处理 | `TestToolResultFailureIsTyped`（第十五节） |
| B1/B2 来源解析顺序、命令占位串 | 与调用器工作流的补充用例一起 | `TestExecuteToolResolutionOrder` / `TestCommandToolExecuteIsPlaceholder`（第十四节） |

### 对照基线更新

`go test ./builtin/ai/... -count=1 -v` → **550 通过 / 0 失败 / 1 跳过**（基线 526 + 新增 24）。

## 九、已完成：检索骨架（Retrieval）

**提交**：`refactor(ai): 抽取检索骨架，三个消费者共用分词/向量/排序原语`

### 改动范围

| 文件 | 改动 |
|------|------|
| `builtin/ai/retrieval.go`（新增） | `scoreEmbedW`；`tokenizeText` / `tokenOverlap` / `jaccardSimilarity`（自 `select.go` 迁入）；`semanticCosine`；`retrievalScore`；`semanticFallbackLog` + `acquireSemanticVectors`；`rankByScore`；`topK` |
| `builtin/ai/select.go` | 移除已迁出的分词/重叠度原语；打分改用 `semanticCosine` + `retrievalScore`；向量获取改用 `acquireSemanticVectors`；排序改用 `rankByScore` |
| `builtin/ai/rag.go` | 两次排序与 Top-K 改用 `rankByScore` / `topK`；语义项改用 `retrievalScore`；`embedRAGTexts` 委托 `acquireSemanticVectors` |
| `builtin/ai/memory.go` | 匿名打分结构收敛为 `memoryScored`；语义项改用 `retrievalScore`；排序与截断改用 `rankByScore` / `topK`；向量获取改用 `acquireSemanticVectors` |
| `builtin/ai/retrieval_test.go`（新增） | 骨架契约用例 9 例 |

### 设计要点

- **共享算法骨架，不共享领域模型**：`retrieval.go` 只提供 tokenize / 重叠度 /
  语义权重 / 向量获取 / 排序 / 截断原语；候选来源、入选门槛与筛选策略仍留在
  各自消费者中（工具：关键词+热用门槛与预算；历史：`ragKeywordMinScore` 预筛与
  语义兜底；记忆：`signal <= 0` 门槛与出现次数加成）。
- **排序语义显式化**：`rankByScore` 的 `tieBreak` 参数把"同分如何定序"变成调用点
  的显式选择。工具/记忆传确定性 tie-break；历史检索传 `nil`，保持既有的
  "同分不设次序"（记录为 C3，不在迁移中修正）。
- **降级路径集中一处**：`acquireSemanticVectors` 统一"文本批量嵌入 → 查询嵌入"的
  顺序与失败降级（文本失败返回 `nil,nil`；查询失败保留已取到的文本向量）。
  三个消费者的降级日志文案逐字传入，日志输出不变。
- **按需嵌入语义不变**：调用点仍先判 `Enabled()` 再构建候选文本切片，
  避免 embedding 关闭时产生无谓分配（性能不倒退）。

### 行为影响

无。要点与验证方式：

- **数值逐位一致**：语义项由 `keyword + cosine×scoreEmbedW` 组合；无语义信号时
  `retrievalScore(x, 0) == x` 精确成立（有专项断言）。
- **非稳定排序保留**：`rankByScore(..., nil)` 与旧 `sort.Slice` 纯分数比较器
  在同一输入上产生相同序列（有专项断言）；历史检索仍不做 tie-break。
- **向量获取语义一致**：`Enabled()` 判定、文本→查询的调用顺序、缓存复用次数、
  失败时的返回值形状均不变（以"embedding 调用次数"断言，见 A6 冻结要求）。
- **超时不变**：历史/记忆仍为 10s；工具选择仍不设内层超时（直接用请求上下文）。

### 验证

- `go build ./...`、`go vet ./builtin/ai/...`、`gofmt -l builtin/ai/` 通过。
- `go test ./builtin/ai/... -count=1 -v` → **559 通过 / 0 失败 / 1 跳过**
  （上一基线 550 + 新增 9）；包耗时 ~10s，与基线持平。

## 十、已完成：上下文管线（Context）

**提交**：`refactor(ai): 动态上下文 Provider 化，节序/预算装配收敛到单一管线`

### 改动范围

| 文件 | 改动 |
|------|------|
| `builtin/ai/context_pipeline.go`（新增） | `dynamicContextSource`（节点来源/Provider）、`dynamicContextSources`（节序与参与条件声明）、`budgetParticipates` / `fit` / `render`、`buildDynamicContext`、`buildDynamicContextBudgeted` |
| `builtin/ai/handler.go` | 移除内联的 `buildDynamicContext`（节序、标题、开关散落在函数体里） |
| `builtin/ai/budget.go` | 移除内联的 `buildDynamicContextBudgeted`；文件职责收窄为预算原语（`estimateTextTokens` / `fitSection` / `promptReserveTokens`） |
| `builtin/ai/context_pipeline_test.go`（新增） | 节序、空节不对称（C2）、群聊开关不对称（C4）契约用例 |

### 设计要点

- **Provider 只管"我能提供什么"**：每个节声明标题、参与条件、正文生成与注入上限；
  节序、预算编排、标题拼接、最终串联由 Builder 统一负责。
  原先的开关与标题在两条路径各写一遍，现在只在 `dynamicContextSources` 声明一次。
- **正文惰性求值**：节未参与时不求值——群聊查询、记忆检索、历史检索都不会发生，
  与改动前一致。群聊的"机器人回复去重集合"用 `sync.OnceValue` 按需构建、只构建一次
  （预算缩减会多次重试生成正文，去重集合必须在重试间复用，与改动前一致）。
- **预算策略显式声明**：`shrink=true` 走 `fitSection` 计数减半重试（群聊/记忆/历史）；
  `shrink=false` 走单次估测（运行时上下文），与既有行为一致。
- **既有不对称显式承接**：`budgetEnabled` 只用于表达"预算路径与配置路径参与条件不同"
  的群聊窗口（C4）；运行时上下文的空节差异由 `emitWhenEmpty` + 预算策略共同表达（C2）。

### 行为影响

无。除全量测试外，另用**差分对拍**验证：把改动前的两个装配函数原样复制一份，
在「运行时开关 × 字段白名单 × 群聊条数 × RAG 条数 × 记忆上限 × 是否含历史 ×
是否含记忆 × 预算窗口」共 **3456 组配置**下逐字节比较，输出完全一致
（对拍脚本为一次性验证，未进入提交）。

### 验证

- `go build ./...`、`go vet ./builtin/ai/...`、`gofmt -l builtin/ai/` 通过。
- `go test ./builtin/ai/... -count=1 -v` → **562 通过 / 0 失败 / 1 跳过**
  （上一基线 559 + 新增 3）；包耗时 ~10s，与基线持平。

### 新发现并记录

- **C4**：`context_group_messages=0` 在预算路径上不生效（按默认 10 条纳入），
  与文档"0 = 关闭"不一致。属既有行为，本次保留并加测试锁定。
- **C2 精确化**：预算路径整体为空时会回退到配置路径，此时空运行时节仍会出现；
  即"预算路径丢弃空节"只在装入其它非空节时可见。

## 十一、已完成：选择与决策（Selection + Decision）

**提交**：`refactor(ai): 回合动作决策分层，候选发现/选择/稳定与无需动作闸门收敛`

### 改动范围

| 文件 | 改动 |
|------|------|
| `builtin/ai/decision.go`（新增） | `actionCandidates`（候选发现：注册表 + 用户 Skill → 群策略 → RBAC）；`decideTurnActions`（决策入口）；`retainedActions` / `retainedAction`（无需主动动作时的保留集）；`needAction`（闸门） |
| `builtin/ai/process.go` | 工具筛选三处内联逻辑收敛为一次 `decideTurnActions(ctx, session, needAction)` |
| `builtin/ai/select.go` | 必保判定改用 `retainedAction`，与保留集共用同一判据 |
| `builtin/ai/decision_test.go`（新增） | 闸门保守性、保留集、N3 稳定集合不清空、过滤语义（E4）、候选来源 |

### 设计要点

- **决策链单一入口**：候选发现 → 选择（`selectToolsForTurn`）→ 稳定策略
  （`stabilizeToolSet`）。群策略与 RBAC 的过滤条件、日志文案与顺序均未改变。
- **保留判据收敛一处**：`retainedAction`（默认保留级别 或 会话已用）同时服务
  选择路径的必保集与闸门关闭时的保留集，避免两处语义漂移。
- **闸门只抑制"可选挑选"**：`needAction=false` 时保留默认保留级别、会话已用与
  稳定集合（sticky），不挑选可选动作、不读写选择缓存——缓存读写一并跳过，
  避免把收敛后的集合缓存成后续回合的复用结果。
- **闸门当前保守恒真**：`needAction()` 返回 true，即不抑制，保持既有工具集行为。
  收紧判定属行为变更，记入 L8。

### 行为影响

无。生产路径上 `needAction()` 恒为真，`decideTurnActions` 等价于原来的
「候选发现 + `selectToolsForTurn`」；`retainedAction` 与旧内联判据逐一等价。

### 验证

- `go build ./...`、`go vet ./builtin/ai/...`、`gofmt -l builtin/ai/` 通过。
- `go test ./builtin/ai/... -count=1 -v` → **567 通过 / 0 失败 / 1 跳过**
  （上一基线 562 + 新增 5）；包耗时 ~10s，与基线持平。

### 新记录

- **C5**：`send_to` 审批门被判定两次（`execOneTool` 判定 + `sendToAllowed` 运行时
  再判定）。收敛为单一闸门会改变审批语义，属行为变更，本次保留。

## 十二、已完成：代理状态（Agent State）

**提交**：`refactor(ai): 代理状态归属成表，持久化边界与内存态以守卫用例钉死`

### 改动范围

| 文件 | 改动 |
|------|------|
| `builtin/ai/agentstate.go`（新增） | `agentStateClass`（落库 / 会话内存 / 进程级内存）与 `agentStateInventory()` 归属表；`agentStateInfrastructure()` 声明并发原语豁免 |
| `builtin/ai/agentstate_test.go`（新增） | 归属表与结构体字段的对应关系、分类与承载形态一致性、落库并集 = 持久化 schema、Session 字段全覆盖、非落库字段不进入 JSON、持久化往返语义 |

### 设计要点

- **逻辑归属 ≠ 持久化 schema**：归属表只回答"这项状态是什么、跨重启是否保留"，
  不改变任何存储。落库仍由 `toRecord` / `toSession` 与 `json:"-"` 标签共同决定。
- **归属表即持久化边界的事实来源**：落库类目的 `Record` 并集必须**恰为**
  `sessionRecord` 的字段集合；新增列却忘记登记会被测试拦下（G3/G4）。
- **会话内存态不得序列化**：非落库的 `Session` 字段必须是未导出或显式 `json:"-"`。
- **豁免名单只放并发原语**：`mu` / `turnMu` 由 `agentStateInfrastructure()` 豁免（该声明现已与守卫用例同址于 `agentstate_test.go`，见 29 §十），
  守卫用例校验其类型必须是 `sync.Mutex`，防止借豁免夹带真实状态。
- **新增状态必被登记**：`Session` 上每个字段要么在归属表里，要么在豁免名单里；
  临时加一个未登记字段会让用例立刻失败（已用临时字段验证过守卫有效）。

### 行为影响

无。归属表是纯声明，不参与运行时；`json:"-"` 标签、`sessionRecord` 字段集合与
`toRecord` / `toSession` 的序列化语义均未改动。

### 验证

- `go build ./...`、`go vet ./builtin/ai/...`、`gofmt -l builtin/ai/` 通过。
- `go test ./builtin/ai/... -count=1 -v` → **574 通过 / 0 失败 / 1 跳过**
  （上一基线 567 + 新增 7）；包耗时 ~10s，与基线持平。
- G3/G4（GAP-10）至此**完全覆盖**：字段集合快照 + `toRecord → toSession` 往返语义。

### 边界裁决

本工作流只修**边界**，不引入运行时抽象：归属表没有一个"必须由生产代码消费"的
自然消费者（`Session` 已为每项状态提供访问器），因此不为了架构对称再造一层包装。
这与"没有真实消费者就不要为抽象而抽象"是同一原则。

## 十三、收口阶段：范围与裁决

主干迁移（工作流 1–8）已完成，此后不再扩大架构改动。收口只做两件事：

1. **补完冻结用例**：`F` 由 9/17 补到 17/17（27 §H 余下条目），随后全量回归；
2. **确认迁移无意外变化**：回归数字与文案、排序、字节口径均与基线一致。

### 遗留项裁决

| # | 结论 | 说明 |
|---|------|------|
| L1 | 保持现状 | `loopToolSender` 已在窄接口 `ToolSender` 之后，属装配层；与"核心能力依赖整个 Plugin"是两回事 |
| L2 | 本阶段处理 | 补完 §H 余下条目 |
| L3 | 定名 `ActionSpec` | 与 `platform.Capabilities`（平台能力集合）、`plugin.TryService`（插件服务）语义层次不同，无需为避嫌改名 |
| L4 | ✅ 已由 C1 交付 | `isToolErrorResult` 已退役，失败判定改用 `ActionResult.Err`（见第十五节） |
| L5 | 保持现状 | 审批被拒文案不算失败，属既有行为 |
| L6 | 保持现状 | 附件为回合级捕获，`ActionResult` 不承载附件 |
| L7 | 后续候选 | 跨包检索 API 清理，独立课题（已由第二十节落地） |
| L8 | 保持现状 | `needAction` 恒真是兼容实现，收紧属独立行为变更 |

### 行为修正待办不变

### 行为修正：当时决定不在收口阶段实施（后续已逐个完成）

`C1`–`C5` 当时一律**不在收口阶段实施**：它们是五个彼此独立的行为变更，逐个开独立变更
（建议按影响面 C1 → C2/C3 → C4 → C5），避免与迁移回归混在一起难以归因。

> 后续状态：该安排已执行完毕——C1 至 C5 分别在第十五、十六、十七、十八、十九节逐项实施，行为修正待办清零。本节保留作历史记录。

### 契约表述勘误（仅文档，不改行为）

- **A13 / §H-6** 原文写作"同一组工具以不同输入顺序给出 → 输出名称升序一致"。
  实际冻结语义是：**输出顺序跟随输入顺序**（`selectToolsForTurn` 按输入下标排序），
  跨请求字节稳定由 `ToolRegistry.List()` 的名称升序保证。补测试时按实际语义冻结。

## 十四、已完成：冻结用例补全（收口）

**提交**：`test(ai): 补齐选择与稳定 / 执行语义 / 前缀缓存冻结用例`

把 27 §H 余下条目补齐到 **17/17**（§H 含负向不变量用例共 20 条，口径见第二十一节）；
全部为新增测试，本轮收口生产代码零改动。

| §H | 用例 | 覆盖 |
|----|------|------|
| 3 | `TestEmptyCategoryIsMandatory` | A3/GAP-4 空 `Categories` 与显式 general 同判必保 |
| 5 | `TestEmbeddingCacheNoReembed` | A6 同 query 不重复嵌入（按调用次数断言） |
| 6 | `TestToolSetDeterministicOrdering` | A13/N4 注册表名称升序、输出跟随输入顺序、字节稳定 |
| 7 | `TestCommandToolExecuteIsPlaceholder` | B2 命令工具占位执行，无副作用 |
| 8 | `TestExecuteToolResolutionOrder` | B1 owner Skill > 系统 Skill > 真实命令 > 自身 Execute |
| 9 | `TestCaptureSenderAttachmentsMerged` | B6/GAP-8 正文与附件都不丢，附件合并而非覆盖 |
| 10 | `TestToolResultErrorPrefix` | B5 错误前缀判定与正常结果判假 |
| 11 | `TestSkillLoopNonStreaming` | B8/F6 非流式循环 + 可见工具构成 |
| 12 | `TestDynamicContextChangeKeepsPrefix` | D3/N1/N6/GAP-5 动态内容不进稳定前缀 |
| 13 | `TestToolSetChangeCreatesDistinctPrefix` | D4/GAP-6 ToolSet 变化产生新前缀边界 |
| 17 | `TestSessionPersistenceSchemaSnapshot` | G3/G4/GAP-10 字段集合 + 无版本字段（round-trip 见 `TestAgentStatePersistenceRoundTrip`） |

测试侧仅新增一个夹具 `fakeEventProcessor`（冒充引擎事件处理器，模拟真实命令
路径），其余复用既有 `mockProvider` / `toolSet` / `toolsRequestBytes`。

### 验证

- `go build ./...`、`go vet ./builtin/ai/...`、`gofmt -l builtin/ai/` 通过。
- `go test ./builtin/ai/... -count=1 -v` → **584 通过 / 0 失败 / 1 跳过**。
- 收口阶段的提交只包含 `*_test.go` 与 `docs/`，生产代码未改动。

### 结论

主干迁移（工作流 1–8）与冻结用例（17/17）均已收口，`builtin/ai` 的分层重构到此为止。
`C1`–`C5` 与 `L7` 作为独立变更另行开展；`L1` / `L5` / `L6` / `L8` 保持现状。

## 十五、行为修正 C1：类型化失败判定

**提交**：`fix(ai): 失败判定改用类型化错误，退役结果文本前缀推断`

### 改动范围

| 文件 | 改动 |
|------|------|
| `builtin/ai/execute.go` | 新增 `executeToolResult`（返回 `ActionResult`，携带失败语义）；`executeTool` 退化为文本视图包装 |
| `builtin/ai/process.go` | `toolExecResult` 增加 `err`；重试预算、反思引导、计划展示、调用追踪全部改用 `err != nil` 判定 |
| `builtin/ai/retry.go` | 删除 `isToolErrorResult`；文件头说明改为类型化判定 |
| `builtin/ai/retry_test.go`、`builtin/ai/freeze_execute_test.go` | 前缀断言替换为类型化断言 |

### 语义边界（刻意区分）

- **调用本身失败**（`Err` 非空）：工具不存在、权限不足、回调返回错误、技能执行失败、执行超时。
- **调用成功但正文像错误**（`Err` 为空）：正文逐字回填，不再计入失败预算。
- **审批被拒**（`Err` 为空）：与既有行为一致——用户已拒绝的动作不需要模型"重试"。

### 需要留意的连带影响

`create_plan` / `update_plan_step` 的校验类文案（如"错误: 计划至少需要 2 个步骤"）
历来以 `nil` error 正常返回，此前仅因文本前缀被判为失败；按类型化语义它们现在
**不再消耗重试预算**（模型仍能看到文案并在下一轮自我纠正）。若希望这类校验继续
计入失败，应另开变更让工具返回真实错误，而不是恢复前缀推断。

### 验证

- `go build ./...`、`go vet ./builtin/ai/...`、`gofmt -l builtin/ai/` 通过。
- `go test ./builtin/ai/... -count=1 -v` → **584 通过 / 0 失败 / 1 跳过**。
- 冻结清单 §H-10（`TestToolResultErrorPrefix`）已被本变更取代，替换为
  `TestToolResultFailureIsTyped`；其余 16 项不受影响（原用例断言的前缀文案
  仍由新用例逐字冻结）。

## 十六、行为修正 C2：空运行时上下文节

**提交**：`fix(ai): 空运行时上下文章节两条路径统一不输出`

### 改动范围

| 文件 | 改动 |
|------|------|
| `builtin/ai/context_pipeline.go` | 退役 `emitWhenEmpty` 字段；空正文一律跳过，两路径一致 |
| `builtin/ai/context_pipeline_test.go` | `TestRuntimeSectionEmptinessDiffersBetweenPaths` → `TestRuntimeSectionEmptyDroppedOnBothPaths`，断言两路径都不产生只有标题的空节 |

### 语义边界

- 仅统一**空正文**的处理：`include_runtime_context=true` 且 `context_fields`
  排除全部字段时，配置路径与预算路径都不再输出"只有标题的空节"。
- **群聊窗口开关不对称（C4）继续保留**：本次 Provider 化抽取只收敛空节语义，
  预算路径仍以 `budgetEnabled` 承接既有不对称，留待 C4 单独处理。

### 验证

- `go test ./builtin/ai/... -count=1 -v` → **584 通过 / 0 失败 / 1 跳过**（用例数不变，仅改名）。

## 十七、行为修正 C3：历史检索同分确定性

**提交**：`fix(ai): 历史检索同分采用确定性次序`

### 改动范围

| 文件 | 改动 |
|------|------|
| `builtin/ai/rag.go` | 新增 `ragHitTieBreak`；两处 `rankByScore` 由 `nil` 改为该次序 |
| `builtin/ai/rag_test.go` | 新增 `TestRAGHitTieBreakNewerFirst`（纯排序）与 `TestBuildRAGContextTieBreakNewerFirst`（完整路径） |

### 语义边界

- 仅在**分数完全相等**时生效，分数不同者次序不变。
- 次序规则：较新的消息优先；同一时刻按 `EventID` 升序；再按内容升序兜底。
- 与工具选择（工具名升序）、记忆检索（事实文本升序）保持"同分确定性"的一致性，
  具体排序键仍各自贴合领域（本次不改动工具/记忆）。

### 验证

- `go test ./builtin/ai/... -count=1 -v` → **586 通过 / 0 失败 / 1 跳过**（新增 2 个用例）。

## 十八、行为修正 C4：群聊窗口开关两路径统一

**提交**：`fix(ai): 群聊窗口开关对两条上下文路径一致`

### 改动范围

| 文件 | 改动 |
|------|------|
| `builtin/ai/context_pipeline.go` | 退役 `budgetEnabled` 字段与 `budgetParticipates`，两路径共用 `participates`；群聊节参与条件统一为 `context_group_messages > 0` |
| `builtin/ai/context_pipeline_test.go` | `TestGroupSectionGatingDiffersBetweenPaths` → `TestGroupSectionHonorsSwitchOnBothPaths`（关闭/开启两态两路径一致） |

### 语义边界

- `context_group_messages = 0` 现在对预算路径同样表示"关闭"：预算路径不再按
  默认 10 条纳入群聊窗口。
- `context_group_messages > 0` 时两条路径行为不变（均按该条数纳入，预算路径
  可继续按预算缩减）。
- 受影响的下游：预算路径关闭群聊后若其余节为空，预算装配返回空串并**回退**到
  配置路径；由于配置路径同样关闭群聊，最终动态上下文为空——与"关闭"语义一致。

### 验证

- `go build ./...` 通过。
- `go test ./builtin/ai/... -count=1 -v` → **586 通过 / 0 失败 / 1 跳过**。

## 十九、行为修正 C5：审批门成为 SendTo 授权的唯一来源

**提交**：`refactor(ai): 审批门收敛为 SendTo 授权的唯一来源`

### 改动范围

| 文件 | 改动 |
|------|------|
| `builtin/ai/process.go` | 新增 `toolApprovalDecision` 与 `decideToolApproval`：审批判定与 SendTo 授权同源；`execOneTool` 只消费该结论 |
| `builtin/ai/sendtool.go` | 注释明确 `sendToAllowed` 由审批门一次性授予，`SendTo` 只消费、不重新推导 |
| `builtin/ai/approval_test.go` | 新增 `TestDecideToolApprovalSingleSource` |

### 语义边界（行为保持）

- **授权判定只有一处**：`decideToolApproval` 按生效审批模式判定、必要时发起审批，
  并返回 `{allowed, rejectText, sendToGranted}`；`execOneTool` 不再自行推导
  `sendToAllowed`。
- **可观测行为零变化**：`sendToGranted` 与迁移前的 `needApproval` 取值等价
  （拒绝路径提前返回），`SendTo` 的拒绝文案、审批消息、发送结果全部逐字保持。
- **嵌套 Skill 防护保留**：嵌套 Skill 内的工具调用不经 `decideToolApproval`，
  拿到的是未授权 sender，仍无法绕过审批直接 `SendTo`。

> 后续（见第二十三节）：`decideToolApproval` 迁入 `decision.go` 并与 RBAC 预检合并为
> `decideToolInvocation`，本节描述的语义与文案全部保持。

### 验证

- `go build ./...`、`go vet ./builtin/ai/...`、`gofmt -l builtin/ai/` 通过。
- `go test ./builtin/ai/... -count=1 -v` → **587 通过 / 0 失败 / 1 跳过**。

## 二十、遗留项 L7：知识库复用 AI 检索原语

**提交**：`refactor(knowledgebase): 复用 AI 插件导出的检索原语`

### 改动范围

| 文件 | 改动 |
|------|------|
| `builtin/ai/retrieval.go` | 导出 `TokenizeText` / `TokenOverlap` / `TokenJaccard`（薄封装包内 `tokenizeText` / `tokenOverlap` / `jaccardSimilarity`，与既有 `CosineSimilarity` 同风格） |
| `builtin/knowledgebase/retrieve.go` | 删除本地 `tokenize` / `tokenOverlap` / `tokenJaccard`，改调 `ai.*`；移除 `unicode`、`unicode/utf8` 导入 |
| `builtin/ai/retrieval_test.go` | 新增 `TestExportedRetrievalPrimitives` 冻结导出原语与包内骨架等价 |
| `builtin/knowledgebase/knowledgebase_test.go` | `TestTokenize` → `TestTokenizeText`，改断共享原语 |

### 语义边界（行为保持）

- 两份分词实现在 ASCII 单词切分、连续汉字二元组、孤立汉字单字、token 计数上等价；
  本次只消除重复实现，不改变关键词兜底打分、去重阈值与排序结果。
- 未新增独立包：`knowledgebase` 本就依赖 `builtin/ai`（复用 `ai.CosineSimilarity`），
  导出薄封装即可完成复用，符合"不因抽象而抽象"。
- 包内调用点仍走未导出的 `tokenizeText` 等，导出面仅 3 个符号。

### 验证

- `go build ./...`、`go vet ./builtin/ai/... ./builtin/knowledgebase/...`、`gofmt -l` 通过。
- `go test ./builtin/ai/... -count=1 -v` → **588 通过 / 0 失败 / 1 跳过**。
- `go test ./builtin/knowledgebase/... -count=1 -v` → **18 通过 / 0 失败 / 1 跳过**。

## 二十一、遗留项登记（对照 26 目标，避免重复发现）

对 26 目标分层逐条核对后的**已知未交付项**（G-A 已交付，保留行以记录闭环）。均为知情延期或收窄交付，
不是回归；后续若要推进，各自开独立变更，不并入迁移。

| # | 26 目标 | 现状 | 为什么保留 | 后续动作 |
|---|---------|------|-----------|---------|
| G-A | `memory_query` 降级为 Retrieval / Context（26 §四映射表） | ✅ 已实施（见第二十二节）：不再是模型可见动作，检索由上下文管线承担 | — | — |
| G-B | Policy 作为 Decision 子层统一评估（26 §3.3 / §四"评估在 Decision"） | ✅ 已实施（见第二十三节）：`decideToolInvocation` 统一 RBAC + 审批，执行路径只消费结论 | — | — |
| G-C | 命令目录 / Skill 运行器抽成 Capability 端口（26 §四） | ✅ 已实施（见第二十四节）：`commandCatalogPort` / `skillRunnerPort`，调用器不再依赖插件实例 | — | — |
| G-D | `Action` 取代 `Tool` 成为内部存储，`Execute` 只存在于 `Invoker`（26 不变量 1/11） | ✅ 已实施（见第二十五节）：注册表内部只存动作视图，管线（选择/稳定/序列化/策略）不见 `Execute` | — | — |
| G-E | Context `Provider → Fragment → Builder → Snapshot`（26 §3.6） | 交付 Provider（`dynamicContextSource`）+ Builder；无 `Fragment` / `Snapshot` 类型 | 不变量 9：无消费者不为抽象而抽象；节间不需要独立寻址 | 出现"跨 Provider 片段级重排/去重"需求时再引入 |

> 另有两项**已裁决保留**的既有行为，不属本表：`captureSender` 未退役（L6，4b 范围收窄）、
> `needAction` 恒真（L8，收紧属独立行为变更）。

### 检索契约实测（关闭原验证盲区）

27 §十四 判据 4 要求"设置 `REMILIA_EMBED_TEST_URL` 后 embedding 契约测试全绿"。
已用本地 OpenAI 兼容 embedding 服务（`Qwen3-Embedding-0.6B-Q8_0.gguf`，1024 维）实测：

```powershell
$env:REMILIA_EMBED_TEST_URL  = "http://10.0.0.20:8080/v1"
$env:REMILIA_EMBED_TEST_MODEL = "Qwen3-Embedding-0.6B-Q8_0.gguf"
go test ./builtin/ai/ -run TestRetrievalContractLive -count=1 -v
```

| 检索 | 纯关键词 | +embedding（`scoreEmbedW=2`） |
|------|---------|------------------------------|
| 工具选择 Top1 / R@5 / MRR | 54.2% / 66.7% / 0.607 | **91.7% / 100% / 0.958** |
| RAG 关键词预筛保留正确文档 | 2/6 | 语义兜底找回 **4/4** |
| 记忆正确事实入 Top-3 | 0/2 | **2/2** |

- 断言"embedding 不劣于纯关键词"通过；权重扫描 `w=0.5…3.0` 指标持平，
  说明 `scoreEmbedW=2` 落在平台区，无需调整。
- 设置环境变量后全量基线为 **589 通过 / 0 失败 / 0 跳过**（原 SKIP 项转为通过）。

## 二十二、遗留项 G-A：`memory_query` 降级为检索

**提交**：`fix(ai): 记忆检索降级为上下文注入，退役 memory_query 动作`

### 改动范围

| 文件 | 改动 |
|------|------|
| `builtin/ai/memorytool.go` | 移除 `memory_query` 动作（含 `memoryQueryToolName`、`memoryToolMaxLimit`、`memoryToolLimit`）；`memory_forget` 的说明与文案改为引用注入的记忆条目 / `/ai memory` |
| `builtin/ai/memorytool_test.go` | `TestMemoryTools_AddQueryForget` → `TestMemoryTools_AddForget`；删除 `TestMemoryToolLimit`；新增 `TestMemoryQueryIsRetrievalNotAction` |
| `builtin/ai/capability.go` | `memoryPort` 注释同步（检索由上下文管线消费） |

### 可观测行为变化（本次是行为变更，非纯迁移）

- **工具集组成变了**：模型可见工具不再包含 `memory_query`；长期记忆的 `general`（Baseline）
  必保项由 3 个减为 2 个，ToolSet 也随之变化——这是本次预期的行为变更。
- **检索能力由 Context 承担**：`buildMemoryContextN` 每轮按最后一条用户消息检索
  （用户记忆 + 群记忆，Top-N 注入）。用户显式问"你还记得…"时，该消息本身就是检索查询，
  事实仍会进入模型视野（由 `TestMemoryQueryIsRetrievalNotAction` 冻结）。
- **失去的能力**：模型不能再以**自选查询词**做一次针对性检索。若后续确需该能力，
  应作为 Decision 触发的 Retrieval 请求重新设计（当前无消费者，不提前造，见不变量 9）。
- `memory_add` / `memory_forget` 的写入、删除与去重语义未变；`/ai memory` 用户侧命令未变。

### 验证

- `go build ./...`、`go vet ./builtin/ai/...`、`gofmt -l builtin/ai` 通过。
- `go test ./builtin/ai/... -count=1 -v`（带 `REMILIA_EMBED_TEST_URL`）→ **589 通过 / 0 失败 / 0 跳过**。

## 二十三、遗留项 G-B：调用策略评估收敛到 Decision

**提交**：`refactor(ai): 调用策略评估收敛到决策层`

### 改动范围

| 文件 | 改动 |
|------|------|
| `builtin/ai/decision.go` | 新增 `toolInvocationDecision` 与 `decideToolInvocation`（唯一评估点：RBAC → 审批）；迁入 `needsApproval` / `approvalModeFor`；文件头说明决策同时回答"发给模型什么"与"这次调用是否放行" |
| `builtin/ai/process.go` | `execOneTool` 改为消费 `decideToolInvocation` 的结论（删除内联 RBAC 预检与 `decideToolApproval` / `toolApprovalDecision` / `approvalModeFor`）；`effectiveApprovalMode` / `effectiveApprovalTimeout` 仍留在配置解析处 |
| `builtin/ai/approval.go` | 移除 `needsApproval`（迁入决策层）；审批交互本身（发起 / 解析 / 按钮 / 文本命令）原样保留 |
| `builtin/ai/decision_test.go` | 迁入并冻结 `TestNeedsApproval`、`TestApprovalModeFor`；`TestDecideToolApprovalSingleSource` → `TestDecideToolInvocationSingleSource` 并新增"声明权限但无权"子用例 |
| `builtin/ai/approval_test.go` | 移出策略评估类用例，只保留审批交互契约 |

### 语义边界（行为保持）

- **唯一评估点**：RBAC 与审批在 `decideToolInvocation` 内按既有顺序（先权限后审批）
  判定，返回 `{allowed, rejectText, err, sendToGranted}`；执行路径不再自行推导策略。
- **拒绝语义分工不变**：权限不足带 `err`（计入重试预算），审批被拒不带 `err`
  （不消耗重试预算）——与迁移前逐字一致。
- **SendTo 授权同源**：`sendToGranted` 仍只由审批门放行时授予，`SendTo` 只消费、
  不重新推导（C5 结论保持）；嵌套 Skill 内调用不经此处，仍拿不到授权。
- **纵深防御保留**：`execute.go` 内的权限校验与 RBAC 预过滤未删除。
- **执行路径不认识业务策略**：`execOneTool` 只做"计数 → 消费决策 → 执行 → 追踪"。

### 验证

- `go build ./...`、`go vet ./builtin/ai/...` 通过。
- `go test ./builtin/ai/... -count=1 -v`（带 `REMILIA_EMBED_TEST_URL`）→ **589 通过 / 0 失败 / 0 跳过**，与迁移前基线一致。

## 二十四、遗留项 G-C：命令目录 / Skill 运行器抽成能力端口

**提交**：`refactor(ai): 命令目录与技能运行器抽成能力端口`

### 改动范围

| 文件 | 改动 |
|------|------|
| `builtin/ai/invoker.go` | 新增 `commandCatalogPort` / `skillRunnerPort` 两个最小端口与 `pluginCommandCatalog` / `pluginSkillRunner` 适配器；`commandInvoker` 由持 `*Plugin` 改为持端口，`skillInvoker` 的 `run` 函数字段改为端口 |
| `builtin/ai/execute.go` | 在装配点构造两个适配器并注入调用器（与 `capabilitySet` 同一处装配） |
| `builtin/ai/invoker_test.go` | 新增 `stubSkillRunner` 端口桩，技能调用器契约不再需要构造插件实例；命令调用器用例改用真实适配器 |

### 语义边界（行为保持）

- **端口只描述一件事**：命令端口回答"这个动作名对应哪条真实命令、跑完说了什么"，
  技能端口回答"把这个 Skill 跑完并给出最终文本"。命令表、合成事件、平台同步器、
  子代理 Prompt / 工具集 / 轮次仍全部留在装配侧。
- **回退语义不变**：命令端口返回空文本仍表示"无对应真实命令"，调用方照旧回退到
  普通动作路径；串行化（syncer 非线程安全）仍由调用方加锁。
- **不进入 `capabilitySet`**：这两个端口是执行路径的依赖，刻意不暴露给工具回调，
  避免工具获得"跑命令 / 跑技能"的能力而扩大工具侧能力面。
- **失败文案逐字不变**：技能失败文本、命令捕获文本、错误与 `Err` 语义均未改动。

### 验证

- `go build ./...`、`go vet ./builtin/ai/...`、`gofmt -l builtin/ai` 通过。
- `go test ./builtin/ai/... -count=1 -v`（带 `REMILIA_EMBED_TEST_URL`）→ **589 通过 / 0 失败 / 0 跳过**，与迁移前基线一致。

## 二十五、遗留项 G-D：`Action` 取代 `Tool` 成为内部存储

**提交**：`refactor(ai): 动作与执行载荷分离，注册表改存动作视图`

### 改动范围

| 文件 | 改动 |
|------|------|
| `builtin/ai/action.go` | 新增 `Action`（`Spec` + `Policy`）与保真派生 `actionOf` / `actionsOf`；文件头补"动作是什么"与"动作怎么执行"的类型边界说明 |
| `builtin/ai/tool.go` | `ToolRegistry` 内部由 `map[string]Tool` 改为 `map[string]registeredAction`（动作视图 + 执行载荷）；新增 `Action` / `Actions`（管线视图），`Get` / `List` 保留为执行视图（`registeredAction.tool()` 逐字段还原）；协议序列化改读 `ActionSpec` |
| `builtin/ai/select.go` | 打分、token 估算、选择缓存与 `selectToolsForTurn` 改以 `Action` 为输入输出（日志文案逐字保留） |
| `builtin/ai/toolset.go` | 稳定策略（`stabilizeToolSet` / `mergeToolSet` / 名称解析）改以 `Action` 为输入输出 |
| `builtin/ai/decision.go` | 候选发现、保留判定、权限与审批评估均读 `Action`（`reg.Action`），`needsApproval` 改收 `Action` |
| `builtin/ai/execute.go`、`grouppolicy.go`、`embedding.go`、`provider.go`、`subcommand.go`、`process.go` | 权限/群策略过滤、嵌入文本、`ChatRequest.Tools` 与单轮调用改以 `Action` 传递；`buildUserSkillTools` → `userSkillActions`（只产出描述与策略） |
| 测试文件 | `action_test.go` 新增 `TestRegistryRoundTripsActionAndTool` 冻结两种视图保真；其余用例同步改用动作视图 |

### 语义边界（行为保持）

- **注册表是唯一持有 `Execute` 的地方**：选择、稳定、协议序列化与策略评估读的都是
  动作视图（描述 + 策略），执行载荷只在 `Get` / `List` 还原——管线在类型层面拿不到执行入口。
- **派生保真**：`actionOf` 是 `actionSpecOf` / `actionPolicyOf` 的合成，`registeredAction.tool()`
  是其逆；"注册 → `Get` 逐字段等于注册值"由新增用例冻结，投影丢字段即失败。
- **协议层零变化**：`toOpenAITools` / `toAnthropicTools` 的字段与顺序未变，取值改为
  动作描述后一一对应，模型看到的 tools 段字节稳定。
- **选择与稳定行为不变**：打分权重、token 估算公式、缓存键与回退、稳定集合合并与
  排序全部沿用，降级与选择日志文案逐字保留。
- **用户 Skill 视图更诚实**：`userSkillActions` 只产出模型可见描述；执行载荷由
  `skillReg` 在调用时按名称解析（`skillInvoker`）。这与迁移前的实际执行路径一致——
  旧闭包里的 `IncrementUsage` 在生产路径上不可达（`executeToolResult` 先按名称解析
  Skill，且 Skill 子循环的工具列表来自 Skill 自身声明，均不经候选列表的 `Execute`）。

> 后续补正（见 29-ai-package-boundaries.md v28）：计数展示本身是真实消费方，因此
> 不是删掉计数而是补回线：`executeSkill` 入口恢复 `IncrementUsage(ownerID, name)` 记账。

### 验证

- `go build ./...`、`go vet ./...`、`gofmt -l builtin/ai` 通过。
- `go test ./builtin/ai/... -count=1 -v`（带 `REMILIA_EMBED_TEST_URL`）→
  **590 通过 / 0 失败 / 0 跳过**（较上一基线 +1，为新增的保真用例）。
- `go test ./... -count=1` 全仓回归通过。

## 修订记录

| 版本 | 修订 |
|------|------|
| v1 | 建立记录：对照基线（526/0/1）、工作流清单、能力端口完成详情 |
| v2 | 记录调用器工作流完成；新增待确认事项（Q1 归类、Q2 抽象引入时机）与遗留项 L4–L6 |
| v3 | 记录行为修正待办（C1/C2）与工作流推进顺序；记录检索骨架完成（C3 同分排序不对称、L7 跨包分词重复）；基线更新至 559/0/1 |
| v4 | 记录上下文管线完成（Provider/Builder 分层、差分对拍 3456 组一致）；新增 C4 群聊开关不对称、精确化 C2 描述；基线更新至 562/0/1 |
| v5 | 记录选择与决策分层完成（候选发现/选择/稳定 + 保守闸门）；新增 C5 双重审批、L8 闸门收紧；基线更新至 567/0/1 |
| v6 | 记录代理状态完成（归属表 + 持久化边界守卫用例，G3/G4 全覆盖）；基线更新至 574/0/1 |
| v7 | 进入收口阶段：裁决 L1–L8（L2 收口、L3 定名 `ActionSpec`、其余保持）、C1–C5 不实施、A13/§H-6 表述勘误；目标 F 17/17 |
| v8 | 收口完成：冻结用例补齐到 17/17（含 §H 3/5/6/7/8/9/10/11/12/13 新增、17 改名）；基线更新至 584/0/1；主干迁移结束 |
| v9 | 行为修正 C1 实施：失败判定改用 `ActionResult.Err`，退役 `isToolErrorResult`；记录计划校验类文案不再计入失败预算的连带影响 |
| v10 | 行为修正 C2 实施：两条路径统一不输出空运行时上下文节，退役 `emitWhenEmpty`；群聊开关不对称（C4）继续保留 |
| v11 | 行为修正 C3 实施：历史检索同分采用确定性次序（较新优先，事件 ID/内容兜底）；基线更新至 586/0/1 |
| v12 | 行为修正 C4 实施：群聊窗口开关对两条上下文路径一致，退役 `budgetEnabled` |
| v13 | 记录 C5 / L7 的待确认取舍（安全回退与公开 API 面），暂停实施等待确认 |
| v14 | 行为修正 C5 实施：审批门收敛为 SendTo 授权的唯一来源（行为保持，嵌套 Skill 防护不变）；基线更新至 587/0/1 |
| v15 | 遗留项 L7 实施：导出 `ai.TokenizeText` / `TokenOverlap` / `TokenJaccard`，knowledgebase 复用并删除重复实现；基线更新至 588/0/1（知识库 18/0/1） |
| v16 | C1–C5 与 L7 全部完成，行为修正待办清空 |
| v17 | 对照 26 目标收口文档：新增第二十一节遗留项登记（G-A～G-E）与验证盲区；工作流表补 Jev 行、修正工作流 6 交付口径与冻结用例数（20/20）；27 同步用例现名与条数口径 |
| v18 | 关闭检索验证盲区：本地 embedding 服务实测契约测试通过（工具选择 Top1 54.2%→91.7%、RAG 兜底 4/4、记忆 0/2→2/2）；带环境变量基线 589/0/0 |
| v19 | 遗留项 G-A 实施：`memory_query` 降级为上下文检索，不再作为模型可见动作（预期行为变更）；G-A 标记闭环 |
| v20 | 遗留项 G-B 实施：调用策略评估（RBAC + 审批）收敛到 Decision（`decideToolInvocation`），执行路径只消费结论；基线保持 589/0/0 |
| v21 | 遗留项 G-C 实施：命令目录与技能运行器抽成能力端口，调用器不再依赖插件实例；基线保持 589/0/0 |
| v22 | 遗留项 G-D 实施：注册表改存动作视图，`Action` 成为管线内部货币、`Execute` 仅经执行视图取出；新增保真用例；基线更新至 590/0/0 |
| v23 | 代码组织收口（详见 29-ai-package-boundaries.md v28）：删除 6 个生产不可达的导出名（含 `executeTool` / `extractAndStore`）、恢复 `IncrementUsage` 调用，上文“不可达”记录已被后续补正取代；新基线：`./builtin/ai/` 565 / 0 失败、`./builtin/ai/...` 710 / 0 失败 |
| v24 | 状态标记订正（仅文档，无代码改动）：工作流 4a/4b、L3/L4 与第十三节裁决表原写“⏸ 待办”，实际已由 C1 与工作流 2 交付；第八节「仍待补」表三行（N3/B5/B1,B2）已在第十四节随 §H 20/20 补齐；第十三节“行为修正待办不变”补后续已完成的注记。修正后全文无“已完成却标为待办”的标记 |
