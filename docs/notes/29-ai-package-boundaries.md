# 29 — AI 插件分包边界与归位记录

> 配套 [26 — AI 插件分层](26-ai-layering.md)（目标职责分层）与
> [28 — 执行进度](28-ai-layering-progress.md)（行为冻结与迁移记录）。
>
> 本文记录 `builtin/ai` 的**物理分包边界**：哪些职责成包、依赖朝哪个方向，
> 以及逐个归位的顺序与状态。26 解决的是语义分层，本文解决的是包边界。

## 一、目标与硬约束

目标：把 103 个平铺文件按职责归位到子包，让 `ai` 只保留装配与门面，
**而不是为了抽象而抽象**——每一刀都必须同时满足：

1. 有真实消费者（不变量 9）；
2. 边界稳定（外部/包内契约不再频繁变动）；
3. 不依赖 AI Runtime 的内部状态（不持有 `*Plugin`）。

三条硬约束决定了做法：

- **Go 不允许方法跨包定义。** 165 个 `*Plugin` 方法必须与 `Plugin` 同包。
  让它们归位的唯一机制是**方法提升**：`Plugin` 嵌入各层 owner 结构体，
  提升后 `*ai.Plugin` 的方法集不变，外部契约（服务查找键、`Discover*`、
  `Register*`、`HealthCheckers`）继续成立。
- **门面不留别名。** 实现搬到子包后，包内与仓库内消费者一律写限定名
  （`session.Session`、`toolkit.Tool`），不再在 `ai` 里以 `type X = pkg.X` 再导出。
  中间轮次曾用 `builtin/ai/aliases.go` 统一转发；该文件已在“门面退役”一轮
  整套删除（见 §九），`ai` 现在只保留 `New` / `Plugin` 两个装配面。
- **只换实现写法，不改可观测行为。** 每步以冻结用例（27 §H）与全量测试回归。

## 二、现状量化（拆分依据）

| 指标 | 数值 |
|------|------|
| 文件 | 103（48 非测试 / 55 测试） |
| 行数 | 29,335 总计 / **14,609 非测试** |
| `*Plugin` 方法 | **165 个**（名字全部唯一 → 方法提升无歧义） |
| `Plugin` 字段 | 28 个 |
| 与 `Plugin` 绑定的代码 | **10,125 行（69%）** |
| 可纯搬迁的代码 | ~4,300 行 |
| `p.cfg` / `p.sm` / `p.reg` 引用 | 183 / 33 / 16 |
| 持有 `*Plugin` 的类型 | 4 处（`pluginReminderScheduler`、`pluginCommandCatalog`、`pluginSkillRunner`、`loopToolSender`），全在装配边界 |
| 测试包形态 | 54 个 `package ai`（内部）+ 1 个 `package ai_test` |
| 外部消费者 | 31 个文件，仅用到 20 个导出符号 |

结论：**只抽叶子层最多解决 31% 的代码**，剩下 69% 必须靠 `Plugin` 分解。

## 三、目标包与依赖方向

依赖单向，不允许回边（回边即循环）：

```
config  →  protocol  →  toolkit  →  retrieval
                            ↓
                         session
                            ↓
        catalog / decision / context / execution
                            ↓
                  runtime / admin   （依赖全部下层）
                            ↓
                    ai（Plugin + Setup 装配）
```

| owner | 方法数 | 自有字段 | 建议包 |
|-------|-------:|----------|--------|
| shell（描述符 + Setup 装配根） | 6 | — | `ai`（保留） |
| 工具/技能目录（发现、注册、内置工具构建） | 15 | `reg` `skillReg` `perms` `cmdPatterns` `coord` `syncer` | `ai/catalog` |
| 决策（候选→选择→稳定→调用策略评估） | 13 | — | `ai/decision` |
| 上下文（动态上下文 / RAG / 记忆注入 / 群窗口） | 16 | `emb` `memory` `history` | `ai/context` |
| 执行（工具与技能执行、审批闸门、重试） | 11 | `realCmdMu` `approvals` | `ai/execution` |
| 回合运行时（消息入口、LLM 循环、计划推进、校验、抽取） | 44 | `sm` `summaries` `lifecycleCtx` | `ai/runtime` |
| 管理与命令（子命令、群策略、用量、QQ 按钮、提醒、待办） | 41 | `groupPolicies` `actionMu` `reminders` `todos` `fsmEngine` | `ai/admin` |
| 纯搬迁层（无 owner） | — | `cfg` `prov` | `ai/config` `ai/protocol` `ai/toolkit` `ai/retrieval` `ai/session` |

## 四、推进阶段

| 阶段 | 内容 | 包风险 | 状态 |
|------|------|--------|------|
| S1 | 纯搬迁 5 个叶子/契约包：`config` → `protocol` → `toolkit` → `retrieval` → `session` | 低 | ✅ |
| S2 | 字段分区：owner 结构体**先留在 `ai` 包内**，`Plugin` 嵌入它们，验证分区与方法归属 | 无（纯包内） | 🚧 字段分区完成，见 §七 |
| S3 | owner 逐个释放到子包（每个一次提交、一次全量回归） | 中高 | 🚧 `execution` / `catalog` / `promptctx` / `decision` 已收口，`runtime` 进行中，见 §八 |
| S4 | `ai` 退化为门面，决定是否迁移 31 个外部调用点 | 低 | ⏳ |

S2 是成败检查点：**S2 全绿之前不动 S3**。否则一旦分区不成立，S3 只会把
一个上帝对象复制成六个中型上帝对象。

## 五、S1 归位状态

| 包 | 归位文件 | 出线行数 | 状态 |
|----|---------|---------:|------|
| `ai/config` | `config.go` + `config_test.go` + `loadconfig_test.go` + 新增 `mockconfig_test.go` | 648 | ✅ |
| `ai/protocol` | `provider.go` `provider_openai.go` `provider_anthropic.go` + `toolschema.go`（工具声明与调用解析） | ~1,400 | ✅ |
| `ai/toolkit` | `tool.go` `action.go` `skill.go` + 新增 `naming.go` | ~640 | ✅ |
| `ai/retrieval` | `retrieval.go` `embedding.go` | ~500 | ✅ |
| `ai/session` | `session.go` `manager.go` `attachment.go` `record.go` `trace.go` `plan.go` `cache.go` `storage.go` | ~1,150 | ✅ |

### `ai/config`（已归位）

- `Config` / `DefaultConfig` / `loadConfig`→`Load` 迁入 `builtin/ai/config`；包内
  私有助手 `configFloat`/`configInt` 改名 `floatOption`/`intOption`（原名与包名重复）。
- `mockConfig` 测试替身只被 config 相关用例使用，随测试一并迁出，`ai` 侧
  `testutil_test.go` 只保留 `toolCtxForTest`。
- `ai` 侧保留 `type Config = config.Config` 与 `var DefaultConfig = config.DefaultConfig`
  门面别名（无写入方，语义不变）。

### `ai/protocol`（已归位）

- 迁入 `builtin/ai/protocol`：`provider.go`（`Provider` 接口、`Role`/`Message`/
  `ToolCall`/`ContentPart`/`ChatRequest`/`ChatResponse`/`TokenUsage`/`StreamEvent`）、
  `provider_openai.go`、`provider_anthropic.go`，以及新拆出的 `toolschema.go`
  （`ToolParamSchema`、`ToolSpec`、`OpenAITool`/`OpenAIFunction`/`AnthropicTool`、
  两种格式的调用解析）。
- 线格式类型与解析函数**全部导出**（`OpenAIToolCall`/`AnthropicContentBlock`/
  `ToOpenAITools`/`ToAnthropicTools`/`ParseOpenAIToolCalls`/`ParseAnthropicToolCalls`），
  否则 `ai` 侧的 `wire_test.go` 无法把包内断言指向真实实现。
- `ChatRequest.Tools` 由 `[]Action` 改为 `[]protocol.ToolSpec`：协议层只认识
  "发给模型看的那几个字段"，`ai` 侧以 `wireSpecs` 收敛动作视图。
  **`ActionSpec` 暂留 `ai`**（含类别与保留级别语义，不属于线格式）。
- **装配职责留在 `ai`**：`NewProvider`（提供商选择 + 指标包装）与全部
  `ai_llm_*` / `ai_tool_*` 指标不进入协议层，协议层不引入 prometheus 依赖；
  为此 `requestModel` 导出为 `protocol.RequestModel` 供装饰器调用。
- `cachedContent` 属于会话附件缓存而非协议类型，留在 `ai/session.go`。
- 测试侧按"断言真实实现"归位：`protocol` 内部函数（`toOpenAIMessages`、
  `toAnthropicMessages`、`toAnthropicUserBlocks`、`extractAnthropicSystem`、
  `buildOpenAIContentParts`、`openaiMessageContent`、`mergeOrAppendToolCall`、
  `attachmentFromImageURI`）的用例迁入 `protocol` 内测文件，不再以等价替身复刻；
  `ai` 侧 `wire_test.go` 为工具声明/调用解析保留直通真实实现的包内名字。

### `ai/toolkit`（已归位）

- 迁入 `builtin/ai/toolkit`：`tool.go`（`Tool`、`ToolRegistry`、`ToolProvider` /
  `SkillProvider`、`ToolSender` / `ChatTarget` 与调用者身份注入）、`action.go`
  （`Action` / `ActionSpec` / `ActionPolicy` / `SelectionClass` 与保真派生）、
  `skill.go`（`Skill` / `SkillRegistry` 与所有者常量），另把工具名规范
  （`ValidToolName` / `SanitizeToolName`，原在 `ai/discovery.go`）一并归位，
  使"注册表校验"与"命令包装器取名"共用同一套字符约束。
- 派生函数随之导出（`ActionOf` / `ActionsOf` / `ActionSpecOf` / `ActionPolicyOf` /
  `SelectionClassOf` / `IsGeneralTool`），内部谓词 `keepsWhenNoAction` 导出为
  `KeepsWhenNoAction`（同批导出的 `hasCategory` → `HasCategory` 因生产无调用方，
  已在收口时删除，见 v28）。
- **调用方一律写限定名**（`toolkit.ActionsOf(...)`），不在 `ai` 里加同名的私有转发函数：
  转发函数留在 `ai` 会在 owner 下沉到子包后变成"子包反向引用父包"，届时又得改一遍；
  限定名可以随文件一起搬。
- **`ai` 只再导出公共名**（`Tool` / `Skill` / `ToolRegistry` / `SkillRegistry` /
  `ToolProvider` / `SkillProvider` / `ToolSender` / `ChatTarget` / `Action*` /
  `SelectionClass` / 相关常量 / `New*Registry` / 工具上下文注入 4 个函数），
  因为仓库内有 20 个外部文件（`cmd/bot/plugins/*`、`builtin/*`）以 `ai.Tool{...}`、
  `ai.Skill{...}`、`ai.CategoryGeneral` 等名构造返回值——它们是插件作者契约，
  经 `aliases.go` 逐名转发后零改动。
- **`toolctx.go` 本次留在 `ai`**：它的注入形态（`toolSource`）携带
  `platform.Sender`，而读侧公开视图 `ToolSource` 不带；拆成两处会让"谁持有平台发送器"
  这件事变得含糊，且导出它等于把平台发送器写进公共契约。它与消费者
  （`remindtool.go` / `todotool.go` 等内置工具）同属 `admin` / `catalog` owner，
  随 owner 下沉时一并归位更自然（见 §四 S3）。

### `ai/retrieval`（已归位）

- 迁入 `builtin/ai/retrieval`：`retrieval.go`（分词 / 重叠度 / 语义权重与向量获取 /
  确定性排序 / 截断）与 `embedding.go`（`Embedder`、OpenAI 兼容嵌入客户端与熔断器、
  `TextVectorCache`）。三个消费者（工具选择 / 历史检索 / 记忆检索）共享这套骨架。
- **顺手退役同义包装**：`TokenizeText`/`TokenOverlap`/`CosineSimilarity`/
  `NewOpenAIEmbedder` 原本是"导出名 + 与之等价的私有实现"，跨包后私有实现已无意义，
  直接让导出名成为唯一实现。这属于写法收敛，可观测行为不变——
  `TestExportedRetrievalPrimitives` 相应改为直接断言语义，而不是断言"包装与实现相等"。
- 跨包必需的内部结构导出：`ScoreEmbedW`、`SemanticFallbackLog`（字段
  `TextsFailed`/`QueryFailed`）、`JaccardSimilarity`、`SemanticCosine`、
  `RetrievalScore`、`AcquireSemanticVectors`、`RankByScore`、`TopK`、
  `TextVectorCache`/`NewTextVectorCache`。
- **工具嵌入文本不下沉**：`toolEmbeddingText` 只认识 `Action` 的类别与描述，
  是工具选择域的知识而非检索算法，回到 `ai/select.go`；其用例随迁 `select_test.go`。
- 测试按"断言真实实现"归位：`retrieval_test.go` 与 `embedding_test.go` 整体迁入
  `retrieval` 内测文件（后者去掉 `TestToolEmbeddingText`），不再需要替身。
- 对外仍是 `ai` 名：`Embedder` / `NewOpenAIEmbedder` / `CosineSimilarity` /
  `TokenizeText` / `TokenOverlap` / `TokenJaccard` 继续可用
  （`builtin/knowledgebase` 调用零改动）。

### `ai/session`（已归位）

- 迁入 `builtin/ai/session`：`Session` 与会话锁/回合生命周期、`SessionManager`
  （LRU + TTL + 窗口裁剪）、`SessionStore` 与 `GormStore`/`NoopStore`、
  持久化记录 `Record` 与双向转换、`Plan`/`PlanStep` 及会话存取、
  附件缓存与"先发图再发字"的合并窗口、工具调用追踪与连续失败计数。
- **三类会话级缓存随 Session 一并归位**（`SelectionCache` / `RAGCache` /
  `ToolSetState`）：它们本就声明为"按会话复用上一轮结果"的槽位、`json:"-"` 不持久化，
  生命周期与会话完全一致。若把它们留在各自消费者里，`Session` 就得反向认识
  消费者类型；留在会话侧则只需 `session → toolkit` 一条下行依赖（与 §三 一致）。
- **依赖方向**：`session` 只依赖 `protocol`（消息与内容片段）与 `toolkit`（动作视图），
  不认识 `Plugin`、不引入 prometheus、不含任何选择/检索/执行策略。
- **先改名后搬迁**：跨包访问所需的成员先在本包内改成导出名（独立一次提交，
  行为不变、全量回归通过），再做物理搬迁——这样搬迁提交的 diff 只有移动与
  别名，不混入命名调整。
- **跨包必需的导出**：`Record`（`ai` 侧以 `SessionRecord` 再导出，避免泛名进入
  `ai` 公共面）、`PendingImageRef`/`PendingImageState`、`TrimMessages`、
  `MessagesForPersistence`、`SaveLocked`（原 `saveNoLock`，装配层在已持锁时调用），
  以及计划状态常量与 `FormatPlan`/`TerminalStatus`。
- **承载字段名保持原样**：`plan`/`pendingImage`/`trace`/`toolFailures` 等仍是未导出字段，
  只有随类型同名的 `ragCache` 字段跟着类型一起变成 `RAGCache`；因此代理状态清单
  （`agentstate.go`）与 `TestAgentStateCoversSessionState` 的字段对照无需重写。
- **测试按"断言真实实现"归位**：会话自身的语义用例（含持久化往返、
  "其余内存态必须重置为初始值"）迁入 `session` 包内测试（31 个）；
  `TestPrepareRequestMessages*`（请求窗口裁剪）与 `TestTruncateToolResult` 属于
  请求构建路径，留在 `ai`；`TestNoopSessionStore` 随存储实现迁入。
- 对外仍是 `ai` 名：`Session` / `SessionManager` / `Plan` 等经 `aliases.go` 转发，
  包内其余代码与外部插件零改动。

## 六、风险登记

| # | 风险 | 缓解 |
|---|------|------|
| R1 | 安全网自身要迁移（54/55 为内部测试） | 门面别名让包内测试大部分零改动；冻结用例走 `*Plugin` 方法路径，方法提升后仍可从 `ai` 调用 |
| R2 | 未导出类型跨包必须导出，公共面扩大 | 已确认接受；只在必要时导出，其余继续留在 `ai` |
| R3 | owner 之间耦合（`p.cfg` 183 处） | 构造注入，**禁止 owner 反向持有 `*Plugin`**；现有 4 处持有属装配边界，保留 |
| R4 | 循环依赖（最危险是 decision ↔ execution） | G-B 已把调用策略评估收敛进 `decideToolInvocation`，execution 只消费结论，该环已拆掉 |
| R5 | 方法提升歧义 | 已核对 165 个方法名无重复；S2 仍需审计 owner 方法名与 `ai` 保留方法名是否撞名 |

## 七、Plugin 字段分区（S2）

S2 的第一刀是**字段分区**：把 `Plugin` 的 28 个字段按 owner 归组，各组以匿名
结构体嵌入 `Plugin`（`pluginparts.go`），方法仍留在 `Plugin` 上。

**关键事实：Go 1.27 允许 keyed 复合字面量使用提升字段名**（
`Plugin{coord: x}` 里的 `coord` 来自嵌入的 `catalogState` 也能编译）。
因此分区对调用点零影响：`p.reg` 这类读取、以及全部 249 处 `Plugin{...}`
字面量（含 40 个测试文件）都不需要改写。

| owner | 承载字段 | 依据 |
|-------|---------|------|
| `catalogState` | `coord` `reg` `skillReg` `perms` `cmdMu` `cmdPatterns` | 工具/技能目录的注册表与发现期映射 |
| `contextState` | `history` `emb` `memory` | 上下文供给的三个数据源 |
| `executionState` | `syncer` `realCmdMu` `approvals` | 执行期独占：合成事件、串行化互斥、审批闸门 |
| `runtimeState` | `sm` `triggerCmd` `defOnce` `def` `lifecycleCtx` `lifecycleCancel` | 回合运行时与生命周期 |
| `adminState` | `fsmEngine` `reminders` `todos` `groupPolicies` `summaryMu` `summaries` `actionMu` `actionRate` | 管理与命令侧的状态机与限流 |
| （直属 `Plugin`） | `cfg` `prov` | 被几乎所有分组读写，装配根自持的共享依赖 |

守卫用例 `pluginparts_test.go` 把分区钉成契约：owner 必须真的是匿名嵌入字段、
承载字段与对照表逐项一致、`Plugin` 的每个字段要么直属要么恰好属于一个 owner、
同一字段名不得出现在两个 owner（否则提升二义）。

### 面向 S3 的跨 owner 读取清单

字段分区暴露了 S3 必须用构造注入替换的边。按下表（引用次数 / 涉及文件数）：
**跨组读取**的字段就是未来的注入点，**单组读取**的字段可以随 owner 一起搬走。

| 字段 | 引用/文件 | 读取方 |
|------|----------:|--------|
| `cfg` | 204 / 26 | 全部 owner（装配根共享依赖，留在 `ai`） |
| `sm` | 137 / 22 | runtime + admin + execution + catalog（多数 owner 都要会话） |
| `reg` | 59 / 14 | catalog（自有）+ execution + admin（`decision` 不再反读，候选由装配侧传入） |
| `memory` | 50 / 11 | promptctx（`MemoryReader` 端口）+ runtime（`MemoryWriter` 端口）+ execution + admin |
| `skillReg` | 37 / 9 | catalog（自有）+ execution + runtime + admin |
| `history` | 27 / 6 | promptctx（自有，直接用 `messagelog`）+ runtime + catalog |
| `groupPolicies` | 21 / 6 | admin（自有）+ runtime（策略过滤在装配侧的 `actionCandidates`） |
| `todos` `reminders` | 16 / 4、12 / 3 | admin（自有）+ 能力端口（`capability.go`） |
| `emb` | 10 / 4 | context（自有）+ decision（作为 `SelectToolsForTurn` 入参注入） |
| `syncer` `realCmdMu` `approvals` | 4 / 2、2 / 1、19 / 2 | execution 独占 |
| `cmdMu` `cmdPatterns` `coord` | 6 / 2、7 / 4、2 / 1 | catalog 独占（`cmdPatterns` 执行期只读） |
| `actionMu` `actionRate` | 4 / 1、8 / 1 | admin 独占（`qqaction.go`） |
| `def` `defOnce` `triggerCmd` `fsmEngine` `summaryMu` `summaries` `lifecycleCtx` `lifecycleCancel` | 单文件为主 | runtime / admin 独占 |

结论：**分区成立但耦合仍重**——真正独占的只有 execution 的执行期互斥、
catalog 的发现期映射、admin 的限流与状态机；`cfg`/`sm`/`reg`/`memory`/`skillReg`
是跨组主干，S3 必须在拆分时为每个 owner 显式注入这几项，而不是让 owner 反向
持有 `*Plugin`。

## 八、S3 归位状态（进行中）

### `ai/execution`（已收口）

S3 的试点 owner。选它是因为在 §七 的清单里它的**独占度最高**：执行期互斥
（`realCmdMu`）、合成事件通道（`syncer`）、审批闸门（`approvals`）都只被自己读，
跨 owner 的边少而清晰，适合先把"owner 下沉"的做法跑通再复制到别的 owner。

归位方式仍守 §一 的三条硬约束，并按**消费方定义窄接口**处理跨 owner 依赖：
执行侧不反向持有 `*Plugin`；需要别的 owner 的数据时，由 `execution` 作为消费方
声明最小端口，在装配点（`ai`）把具体实现适配进去，端口只暴露完成该职责所需的操作。

已释放：

| 迁入 | 内容 | 依赖 |
|------|------|------|
| `builtin/ai/execution/safety.go` | `IsSafeCommandArg`（命令参数安全校验）、`ParseToolPermission`（权限串解析） | 标准库 |
| `builtin/ai/execution/retry.go` | `BuildReflectionMessage`、`BuildRetryAbortMessage`（失败提示文案） | `protocol` |
| `builtin/ai/execution/capture.go` | `CaptureSender`（拦截命令 handler 输出的发送器） | `platform` |
| `builtin/ai/execution/approval.go` | `ApprovalRequest` / `ApprovalManager`（待审批请求的登记、应答、超时清理）、按钮 ID 前缀与 `ApprovalAction` / `ApproveDenyText` / `ArgsNote` / `FormatApprovalTimeout` | 标准库 |
| `builtin/ai/execution/command.go` | `RunCommand`（把动作调用重放成合成命令事件并取回回复）与消费方端口 `CommandPatterns` / `EventProcessor` | `platform`、`core/context` |

- **调用方一律写限定名**（`execution.IsSafeCommandArg` / `execution.ParseToolPermission` /
  `execution.BuildReflectionMessage` / `execution.BuildRetryAbortMessage`），与 §五
  `ai/toolkit` 同一约定，不在 `ai` 里留同义转发。
- **测试按"断言真实实现"归位**：`TestIsSafeCommandArg`、`TestParseToolPermission`
  从 `internal_test.go` / `agentfix_test.go` 迁入 `execution/safety_test.go`；
  `TestBuildReflectionMessage`、`TestBuildRetryAbortMessage` 从 `retry_test.go`
  迁入 `execution/retry_test.go`。两个函数释放后 `ai/retry.go` 已无剩余内容，整体出线。
- **依赖方向**：`execution` 只依赖 `protocol` 与框架层，不依赖 `ai`，不持有 `*Plugin`。
- **跨包读取形态先约定再下沉**：`CaptureSender` 的捕获结果在别的包被读（回合运行时
  据此判断"命令是否真的回复了"，用例据此断言），因此下沉时把它收敛为两个导出字段
  `CapturedText` / `CapturedAttachments`（写入仍只发生在 `Send` 内），而不是把互斥量
  与私有字段暴露出去。类型本身在 `ai` 侧用 `type captureSender = execution.CaptureSender`
  转发（见 `aliases.go`），读侧构造与持有方式不变；同时复用 `ai/toolkit` 的
  `ToolSender` 注入形态，不新增第二套发送器抽象。
- **状态机下沉、交互入口留在装配侧**：审批闸门（`ApprovalManager`）是纯状态与判定，
  整体释放；而"审批请求发给谁、等多久、按哪个前缀提示"读的是 `cfg` 与回复能力，
  只在装配点可得，因此 `requestApproval` / `handleApprovalButton` / `handleApprovalCommand`
  仍是 `*Plugin` 方法（`ai/approval.go` 只保留这三个入口）。`ApprovalRequest` 的结果
  通道不导出字段，改为 `Result()` 只读返回；包内读侧另加 `PendingIDs()` / `PendingCount()`
  两个只读查询——用例与 `sendtool_test.go` 原先直接读 `mu`/`pending`，现在改为读查询，
  不把锁与私有映射暴露成跨包契约。
- **消费方端口替换 `*Plugin` 反向读取**：真实命令通道原实现直接读 catalog 的
  `cmdPatterns`/`cmdMu` 与 execution 的 `syncer`。释放后由 `execution` 声明两个最小端口
  ——`CommandPatterns`（只回答"这个动作名对应哪条命令"）与 `EventProcessor`（只声明
  同步处理事件这一个方法），`ai` 侧由 `pluginCommandCatalog` 一并实现并在装配点注入。
  插件方法 `executeRealCommand` 随之出线（原先只有 `pluginCommandCatalog` 一个调用点），
  执行侧因此不再出现 `*Plugin`。

仍在 `ai`、以及不随本次释放的原因：

| 留在 `ai` | 原因 |
|-----------|------|
| `executeToolResult` / `executeSkill` / `buildSkillTools` / `executeSkillTool` | 把"选调用器 + 闸门 + 回退 + 子代理循环"串起来，读 `reg`/`skillReg`/`cfg`，属编排而非执行侧细节 |
| `hasToolPermission` / `filterToolsByPermission` | 权限评估属策略/决策语义（决策链与按角色注入都要用），读 catalog 的 `perms`；判定不进执行侧，避免执行层承担策略 |
| 审批的三个交互入口 | `requestApproval` / `handleApprovalButton` / `handleApprovalCommand` 是 `*Plugin` 方法（按钮回调与子命令派发的目标），依赖 `cfg.TriggerCmd` 与回复能力 |

`execution` 的后续释放顺序（同一 owner 内继续，仍是一次提交一次全量回归）：
审批入口（消费方端口：回复能力 + 触发命令前缀）。权限评估不在本 owner 范围内。

### `ai/promptctx`（已收口）

上下文 owner（动态上下文 / RAG / 记忆注入 / 群窗口）。包名取 `promptctx` 而不是
§三 表中的 `context`：`context` 会与标准库 `context`、框架的 `core/context`
在所有调用点撞名，注入位置又会大量出现在同时使用三者的文件里，重名带来的阅读
成本高于名字的直观性。语义不变——它承载的正是"提示词上下文"。

归位方式仍是"消费方定义窄接口 + 装配侧注入"：

| 迁入 | 内容 |
|------|------|
| `promptctx/reply.go` | `PrependReplyContext` / `ResolveReplyChain` / `QuoteFromSegments` / `QuotedForwardRecordFromSegments` |
| `promptctx/group.go` | `BuildGroupWindowN`（窗口大小由调用方给定；无参变体已删除，见 v28） |
| `promptctx/pipeline.go` | `Source`（节来源）、`Build` / `BuildBudgeted`（装配与预算编排）、`EstimateTokens` |
| `promptctx/runtime.go` | `BuildRuntimeContext`（`context_fields` 白名单渲染）、`GroupRoleName` |
| `promptctx/history.go` | 消息级 RAG 全流程；导出 `RankCandidates` / `KeywordMinScore` |
| `promptctx/memory.go` | `BuildMemoryContext` + 消费方端口 `MemoryReader`；`BotReplyContents` |
| `textutil/`（叶子包） | `TruncateRunes` / `StripMentionMarkup` / `FirstNonEmpty` |

- **共享算法骨架不共享领域模型**：三节共用的是"生成正文 + 按预算装入"的骨架
  （`Source` / `fitSection` / 渲染），而"正文是什么"仍由各自函数决定——
  没有为了复用而把工具检索、历史检索、记忆检索统一成一个泛型 Retriever。
- **消息历史直接用框架类型**：`messagelog.Logger` 与 `retrieval.TextVectorCache`
  都是下层框架/契约类型，不额外声明端口；真正需要端口的是插件自有状态
  （长期记忆存储），因为它是 owner 状态而不是框架能力。
- **跨 owner 的纯函数独立成叶子包**：`TruncateRunes` / `StripMentionMarkup` /
  `FirstNonEmpty` 被 context、decision、admin、runtime 四个 owner 使用，
  留在任一 owner 都会迫使其他 owner 反向依赖，因此独立成 `textutil`。
- **常量上移而不是复制**：会话级缓存的复用策略（TTL / Jaccard 阈值）由工具选择
  缓存与历史检索缓存共用，上移到 `session`（`CacheReuseTTL` /
  `CacheReuseJaccard`）；`ai/select.go` 的常量改为引用它，冻结用例的断言值不变。
- **装配门面保留方法集**：`buildDynamicContext`（预算路径 → 非预算路径的回退）、
  `dynamicContextSources`（按 `cfg` 声明节序）、`buildRuntimeContext` /
  `buildMemoryContext(N)` / `buildRAGContext(N)` 仍是 `*Plugin` 方法，
  只把插件持有的输入交给 promptctx；节序、参与条件与优先级留在装配侧。
- **用例随实现归位**：RAG 同分次序用例与 `GroupRoleName` 用例迁入 promptctx 内测；
  依赖真实装配的 RAG/记忆/群窗口用例仍在 `ai`（它们断言的是"配置 → 注入文本"）。

### `ai/catalog`（已收口）

catalog 是**工具/技能目录** owner：回答"有哪些动作可用、它们各自是什么"
（发现、注册、内置工具构建），不回答"这一轮选哪几个"（那属于 decision）。
它同时是 §七 清单里另一个独占度较高的 owner——发现期映射
（`coord`/`reg`/`skillReg`/`perms`/`cmdPatterns`/`cmdMu`）只被自己写。

已释放：

| 迁入 | 内容 |
|------|------|
| `catalog/discovery.go` | `IsCommandSafeForAI` / `DeriveToolCategory` / `ToolFromCommand` / `Discover` + `Options` + 消费方端口 `Sink`（`Has`/`Add`） |
| `catalog/skill.go` | `UserSkillNamePattern` / `ApplyDefaultParamSchema`，以及注册形态 `SystemSkill` / `UserSkill` / `ToolFromSkill` |
| `toolkit/toolctx.go` | 工具调用上下文约定 `ToolSource` + `WithToolSource` / `WithToolInvocation` + `ToolSourceFromContext` / `PlatformSenderFromContext`（原 `ai/toolctx.go`） |
| `catalog/capabilities.go` | 能力端口 `MemoryAccess` / `TodoAccess`（含 `TodoItem`）/ `ReminderAccess`（含 `ReminderItem`）/ `ReminderScheduler` 与 `Capabilities` + `WithCapabilities`（原 `ai/capability.go` 的端口部分） |
| `catalog/sendtool.go` | `BuildSendTools`（send_message / send_to）+ `BuildOutboundMessage`，动作名与 `SendToPermission` / `MaxSendMessageRunes` / `SendTimeout` 常量 |
| `catalog/remindtool.go` | `BuildReminderTools`（set_reminder / list_reminders / cancel_reminder） |
| `catalog/todotool.go` | `BuildTodoTools`（todo_add / todo_list / todo_done / todo_remove） |
| `catalog/memorytool.go` | `BuildMemoryTools`（memory_add / memory_forget）+ 记忆作用域参数解析 |
| `catalog/duration.go` | `ParseRemindDuration` / `FormatRemindDuration`（动作侧与 `/ai remind` 子命令共用） |
| `catalog/plantool.go` | `BuildPlanTools`（create_plan / update_plan_step）+ 动作名常量 + 消费方端口 `PlanAccess` + `WithPlanAccess` |

调用方与装配：

- `ai.DiscoverCommands` 只做"读 `cfg` 组装发现选项 + 注入落点"，落点 `pluginCatalogSink`
  在 `ai` 侧实现 `catalog.Sink`：`Has` 查注册表、`Add` 写命令映射与注册表。
  因此 `catalog` 不认识 `Plugin`，注册表仍然只在装配侧被写。
- 技能注册形态按同一方式拆：`RegisterSkill` / `RegisterUserSkill` 仍是 `*Plugin` 方法
  （方法集不变），把"名称校验、前缀、所有者、默认参数"交给 `catalog.SystemSkill` /
  `catalog.UserSkill`，自己只保留需要 `cfg` 的上限检查（数量、Prompt 长度）与注册表写入。
- `registerSkillAsTool` 保留为 `*Plugin` 方法（`subcommand.go` 的提升技能流程也用它），
  内部改为 `catalog.ToolFromSkill(skill, run)`：占位动作的**形状**由 catalog 决定，
  "技能怎么跑"仍由 `ai` 注入（技能真正执行走 `skillReg` 分支，占位动作只保证
  模型能发现它）。
- **工具调用上下文并入 `toolkit`**：`ToolSource`（这次调用在哪个会话、由谁发起）
  与 `WithCallerInfo` / `WithToolSender` 是同一类契约——都是"工具 Execute 只能
  经 context 拿到的东西"。写成 `toolkit` 而不是 catalog，是因为它的两个方向
  消费者不同：`ai` 负责注入（执行路径），目录侧内置工具负责读取；放在下层
  两边都能用，且不产生 catalog → 执行路径的回边。
  同时把"公开视图"与"平台发送器"分开：`ToolSource` 保持不含发送器
  （外部插件契约不变），需要主动推送的工具改经 `PlatformSenderFromContext` 取用，
  避免为了内部需要把平台发送器写进公共面。
- **内置动作整体归入 catalog，端口由消费方声明**：四个内置动作集（发送 / 提醒 /
  待办 / 记忆）连同它们读取执行期上下文的方式一起成包。动作侧不再认识 `*Plugin`：
  需要长期记忆、待办、提醒时，由 catalog 声明最小端口（`MemoryAccess` /
  `TodoAccess` / `ReminderAccess` / `ReminderScheduler`），`ai` 在组合根
  （`toolCapabilities`）把内部管理器适配后经 `catalog.WithCapabilities` 注入。
   - **作用域键格式留在 `ai`**：端口以"种类 + 标识"（user / group + ID）表达，
    `user:<id>` / `group:<id>` 的拼装仍由 `memory.go` 的 `userScope` / `groupScope`
    决定，避免同一约定在两处定义。
  - **数据管理器的名字与归属不变**：`todoManager` / `reminderManager` /
    `memoryStore` 仍是各自 owner 的状态（`/ai todo`、`/ai remind`、`/ai memory`
    等子命令直接使用），因此只加端口适配、不改它们的存储与并发语义；
    `todoManager` 从 `todotool.go` 独立成 `todo.go`（文件内只剩状态与并发控制）。
  - **发送器留在装配侧**：`loopToolSender`（`ToolSender` 实现，读事件上下文、
    经 messagelog 记录、受审批授权与每轮预算约束）与 `sendBudget` 仍在 `ai`
    （`sendtool.go` 现在只放它们），因为它依赖事件上下文与插件回复能力；
    它复用 `catalog.SendTimeout` 保持"单次发送超时"只有一个来源。
- **计划动作随同一 owner 成包**：`create_plan` / `update_plan_step` 的**形状与参数
  校验**（步骤数上下限、非法状态、前序步骤未完成的顺序强制）迁入
  `catalog/plantool.go`；计划的数据结构与会话存取仍属 `session`，因此动作侧通过
  消费方端口 `PlanAccess`（读快照 / 写回 / 重置自动推进预算）访问。端口方法名与
  会话已有 API 一致，装配侧注入会话本身即可，不再多一层适配。
  - **为什么依赖方向成立**：§三 中 `session` 在 `catalog` 之下，故
    `catalog → session` 是下行边，不产生回边。
  - **编排留在 `ai`**：供动作取端口的 `WithPlanSession`（签名与行为不变，改为转发
    `catalog.WithPlanAccess`）、重规划指令 `buildReplanMessage`、无进度签名
    `planSignature` 都是回合运行时的编排语义，不随动作下沉。
  - **测试留在真实装配**：`plan_test.go` / `agentadv_test.go` / `action_test.go`
    的计划用例断言的是"模型调用 → 计划落库 → 回合推进"，属动作级集成，仍走
    `Plugin` 装配，只把调用改成限定名。
  - 动作名常量（`SendMessageToolName` / `SendToToolName` / `todo_*` / `memory_*` /
    `set_reminder` 等）随动作成包并导出：调用方（回合运行时的审批豁免判定、
    权限过滤用例）写限定名，日后这些文件再搬迁时不需要改名。

仍在 `ai`（catalog owner 收口后的固定归属）：

| 留在 `ai` | 原因 |
|-----------|------|
| `DiscoverCommands` / `DiscoverToolProviders` / `DiscoverSkillProviders` / `RegisterToolProvider` / `RegisterSkillProvider` | 导出 API（插件作者契约）。它们的共同动作是**扫描 `plugin.Manager` 容器 + 写回插件目录**：`ToolProvider` / `SkillProvider` 是 `ai` 暴露给插件作者的接口，`catalog` 若识别它们就必须反向依赖 `ai`；而"扫容器"本身只有装配点做得到。把这段拆成端口只会得到一个仅有一种实现的抽象，属于为抽象而抽象，故整体留在装配侧。`DiscoverCommands` 已把"扫描与工具构造"交给 `catalog.Discover`，本方法只剩读 `cfg` 与注入落点 |
| `toolCapabilities` + 端口适配器（`memoryAccessor` / `todoAccessor` / `reminderAccessor` / `pluginReminderScheduler`） | 组合根：把插件内部管理器适配为 catalog 端口；`pluginReminderScheduler` 需要插件生命周期与推送能力，只能在装配点构造 |
| `pluginCatalogSink` | `catalog.Sink` 在装配侧的实现：写注册表与命令模式映射，读的是插件目录状态 |
| `loopToolSender` / `sendBudget` | `ToolSender` 实现，依赖事件上下文、messagelog、审批授权与每轮预算 |
| `todoManager` / `reminderManager` / `memoryStore` | owner 自己的状态与子命令共用实现，动作侧只经端口访问 |

### `ai/decision`（已收口）

决策 owner 的算法主体——**动作相关度打分、本轮选择与会话级稳定策略**——成包为
`builtin/ai/decision`。收口后 `ai/decision.go` 只剩装配门面：候选发现与审批交互。

已释放：

| 迁入 | 内容 | 依赖 |
|------|------|------|
| `builtin/ai/decision/score.go` | `EstimateToolTokens`（schema token 估算）、`ToolEmbeddingText`（嵌入文本构造）、`ScoreToolParts`（关键词 + 会话热用 + 语义余弦的三元组出口；只取总分的 `ScoreTool` 便利函数已删除，见 v28） | `toolkit`、`retrieval` |
| `builtin/ai/decision/selection.go` | `SelectToolsForTurn`（必保集 + 高分补充 + token 预算 + 会话缓存 + 稳定策略）、`SessionUsedTools`、`RetainedActions`、`SelectionCacheTTL` / `SelectionCacheJaccard` | `config`、`toolkit`、`retrieval`、`session`、`textutil` |
| `builtin/ai/decision/stabilize.go` | `ToolSetObserver`（观测端口）、`StabilizeToolSet`（滞回 + 单调并集 + 空闲衰减）与全部名称集合助手 | `config`、`session`、`toolkit` |

- **`ai/select.go` 与 `ai/toolset.go` 整体出线**：两个文件的全部内容（含文件头的
  设计说明）迁入 `decision`；`ai/decision.go` 保留三个薄方法
  （`decideTurnActions` / `selectToolsForTurn` / `stabilizeToolSet`）把插件持有的
  配置、嵌入器、会话与本轮查询交给它们。方法签名与 `*Plugin` 方法集不变。
- **观测依赖以端口注入，指标契约不变**：稳定策略原本直接调 `ai` 的 `recordToolSet`
  （`ai_toolset_changes_total` / `ai_toolset_size`）。下沉后由 `decision.ToolSetObserver`
  声明端口，装配侧继续注入同一个 `recordToolSet`——**指标名、标签与记录时机逐字不变**
  （`observe` 为 nil 时不记录；装配侧恒非 nil，且与既有实现一样只在稳定策略真正
  生效时上报）。`decision` 因此不引入 prometheus 依赖。
- **候选来源与审批交互留在装配侧**：`actionCandidates`（注册表 + 用户 Skill + 群策略 +
  RBAC）、`needAction`（当前恒真）、`decideToolInvocation` / `needsApproval` /
  `approvalModeFor` 依赖插件状态、群策略与审批提示能力，仍是 `*Plugin` 方法；
  `decision` 不反向读取 `reg` / `groupPolicies`，只接收已过滤好的可用动作。
- **查询文本由调用方给定**：`SelectToolsForTurn` 不再自行提取最后一条用户消息，
  改为接收 `query` 参数（与 promptctx 的 RAG / 记忆注入复用同一份查询的既有约定一致），
  因此 `decision` 不依赖 `getLastUserMessage` 所在的回合运行时文件。
- **测试按"断言真实实现"归位**：选择与稳定策略的用例（`select_test.go` /
  `toolset_test.go` / `freeze_test.go`）仍走 `Plugin` 装配，只把对纯函数与策略常量的
  引用改成限定名（`decision.ScoreToolParts` /
  `decision.EstimateToolTokens` / `decision.ToolEmbeddingText` /
  `decision.SelectionCacheTTL` / `decision.SelectionCacheJaccard`）；
  `toolset_test.go` 的 `namesEqual` 断言等价改写为 `slices.Equal`（前者即后者的封装），
  未新增包内测试。
- **对外零改动**：`Plugin` 方法集不变，`aliases.go` 无需新增别名（决策层没有导出
  给插件作者的契约类型）。

### `ai/runtime`（进行中）

回合运行时 owner 的第一个释放切片：**单轮 LLM 调用、回答校验（LLM-as-judge）
与事实抽取**成包为 `builtin/ai/runtime`。回合编排本身（主工具循环
`processWithTools`、消息入口 `handleAI` / `handleAIChat`、计划后台推进、
审批摘要等）仍留在 `ai`，它们读写的都是运行时状态。

已释放：

| 迁入 | 内容 | 依赖 |
|------|------|------|
| `builtin/ai/runtime/client.go` | `Client`（单轮非流式 LLM 调用，请求形状由配置决定）、`SingleRoundResult` | `config`、`protocol` |
| `builtin/ai/runtime/verify.go` | `VerifyPrompt`、`Verifier`（`Verify` / `MaxRetries`）、`ParseVerdict`、`BuildVerifyRetryMessage` | `config`、`protocol` |
| `builtin/ai/runtime/extract.go` | `Extractor`（跑一轮抽取并写入）、`MemoryExtractPrompt`、`ParseExtractedFacts`、`LastRoundForMemory`、`MemoryMessageText`，以及消费方端口 `MemoryWriter` / `ScopeKeys` | `protocol`、`session`、`platform` |
| `builtin/ai/runtime/message.go` | 消息与载荷的纯形状工具：`MergeChatAttachments`、`LastUserIsReplan`、`SummarizeArgs`、`LastUserMessage`、`MessageText`、`CountImageParts`、`RepairToolCallSequence`、`StripBinaryParts`、`TruncateToolResult`、`FormatAIError`，以及常量 `ToolResultMissing` / `MaxToolResultLen` | `protocol`、`session`、`logger`、`platform` |
| `builtin/ai/runtime/request.go` | 请求消息副本构造：`Retention`（`MaxTurns` / `Window` / `MaxPerRequest`）、`InjectDynamicContext`、`PrepareRequestMessages` | `protocol` |
| `builtin/ai/runtime/turn.go` | 回合内工具调用的并行编排 `ExecuteToolCallsParallel` 与结果 `ToolExecResult`、调用追踪 `RecordToolTrace` | `protocol`、`session`、`textutil` |
| `builtin/ai/runtime/limits.go` | 由配置推导的运行预算：`EffectiveToolRetryLimit`、`EffectiveApprovalTimeout`、`EffectiveTurnTimeout`、`EffectivePlanAutoRounds` | `config` |
| `builtin/ai/runtime/deadline.go` | `LiftEventDeadline`（以回合预算替换事件上下文 deadline） | `core/context` |
| `builtin/ai/runtime/inbound.go` | 入站消息归一化的纯形状工具：`BotMentioned`、附件类型判定（`HasImageAttachment` / `IsImageAttachment` / `IsAudioAttachment` / `IsImageAttachmentMeta`）、待合并图片引用与拼装（`PendingImageRefsFromAttachments` / `MergePendingImageParts`）、`HasSubstantiveText`、`InferPartType` / `InferAudioFormat`、`HasTriggerPrefix` / `IsCommandMessage` / `CleanMessage`、`AppendMentionInfo`、`SetSystemMessage`、`MakeSessionID` | `protocol`、`session`、`textutil`、`messagelog`、`core/context`、`platform` |
| `builtin/ai/runtime/forward.go` | 合并转发记录识别与触发：`ForwardRecordFromEvent`、`ForwardTriggerContent`、`ForwardRecordImageAtts`、`QuotedImageFromSegments` | `platform` |
| `builtin/ai/runtime/plan.go` | 计划的编排使用约定：`PlanSignature`（无进度检测）、`WithPlanSession`（计划端口注入）、`BuildReplanMessage`（失败步骤重规划指令）；`ai/plan.go` 整体出线 | `catalog`、`protocol`、`session` |
| `builtin/ai/runtime/invoke.go` | 动作调用器与结果：`ActionResult`、`FuncInvoker`（含观测回调 `Record`）、`SkillInvoker` + 端口 `SkillRunner`、`CommandInvoker` + 端口 `CommandCatalog`、`ToolFailureText` | `execution`、`toolkit`、`core/context` |

- **LLM 单轮调用整体出线**：`runSingleRound` / `runSingleRoundModel` 收敛为
  两行装配（`p.runtimeClient()` + `wireSpecs`），请求构造与调用 ID 补齐移入
  `runtime.Client`；`singleRoundResult` 经 `aliases.go` 以
  `type singleRoundResult = runtime.SingleRoundResult` 转发，读侧（`execute.go`
  的 Skill 子循环）零改动。`wireSpecs` 仍留在 `ai`——它做的是"动作 → 协议声明"
  的收敛，runtime 只接收已收敛好的 `[]protocol.ToolSpec`。
- **跨 owner 依赖走消费方端口**：抽取需要长期记忆与作用域键，`runtime` 声明
  `MemoryWriter`（`Enabled` / `CanExtract` / `MarkExtracted` / `Add`）与 `ScopeKeys`
  （`UserScope` / `GroupScope`）两个最小端口，装配侧注入 `memoryStore` 与
  `memoryScopeKeys`（后者仍调用 `ai` 的 `userScope` / `groupScope`，键格式只有
  一处定义）。记忆未启用时装配侧返回 **nil 接口**而非类型化 nil 指针，避免端口方
  把"未启用"误判为已启用（与 `capability.go` 同一约定）。
- **编排与生命周期留在装配侧**：节流（`memory_min_interval`）、异步调度
  （`lifecycleSpawn`）、"生成 → 校验 → 重生成"循环，以及校验失败/中断时的降级
  分支都读 `cfg` / `sm` / 生命周期，因此仍是 `*Plugin` 方法；`runtime` 只提供
  `Extractor.Extract`（一轮抽取）与 `Verifier.Verify`（一次评审）。
- **测试按"断言真实实现"归位**：`TestParseVerdict` / `TestBuildVerifyRetryMessage`
  与 `TestParseExtractedFacts` / `TestLastRoundForMemory` 的断言改为限定名
  （`runtime.ParseVerdict` 等）；`p.verifyAnswer` / `p.generateVerified` /
  `p.maybeExtractMemory` / `p.extractAndStore` 的调用点不变。
- **消息载荷规整整体出线**：文本提取、多模态占位、工具调用序列自愈、参数摘要、
  工具结果截断与错误文案都只读写协议消息本身，不依赖插件状态，因此从
  `process.go` / `session.go` 独立为 `runtime` 的纯函数，`ai/process.go` 由约
  906 行收敛到约 529 行。`Retention` 以导出字段承载 `imageRetentionConfig()`
  的三个策略参数；`PrepareRequestMessages` / `InjectDynamicContext` 只产出请求
  副本、不写回会话历史，稳定前缀（System + 历史）的字节边界保持不变。
  调用点一律改限定名（`runtime.TruncateToolResult` 等），并同步修正用例。
- **并行编排与运行预算去插件化**：`ExecuteToolCallsParallel` 只接收调用列表、
  并发度、中断信号与"执行单个调用"的闭包，`ai` 侧 `execOneTool`（策略评估 +
  审批 + 执行装配）仍留在装配面、以闭包传入，`execOneTool` 的返回值改用
  `runtime.ToolExecResult`（字段导出）。`RecordToolTrace` 只写会话，三个
  `Effective*` 只读配置，`LiftEventDeadline` 只改事件上下文，因此都从插件方法
  收拢为 `runtime` 的函数式构件；`ai` 不再保留同名私有转发，调用点与用例一律
  写限定名。
- **入站消息归一化出线**：`ai/handler.go` 由 915 行收敛到约 703 行、`ai/message.go`
  由 127 行收敛到 56 行，只余消息入口编排（`handleAI` / `handleAIChat`）、入站
  附件下载/水合与出站回复面。附件类型判定、图片引用拼装、命令样式识别、会话 ID、
  稳定系统消息与合并转发识别/触发/图片提取都不读插件状态，全部收拢到 `runtime`；
  触发前缀改由调用方传入（`CleanMessage` / `HasTriggerPrefix` 收参数），因此 `ai`
  不再保留 `cleanMessage` / `hasTriggerPrefix` 方法，跨文件调用点（`approval.go` /
  `groupcmd.go` / `managecmd.go` / `plugin.go` / `qqaction.go` / `subcommand.go` /
  `usage.go`）与用例一律写限定名。
- **调用器整体出线、适配器留在装配侧**：三个调用器与 `ActionResult` 成包为
  `runtime/invoke.go`，`ai/invoker.go` 只余两个适配器（`pluginCommandCatalog`
  交出命令模式映射与事件处理器、`pluginSkillRunner` 交出子代理循环）。
  `FuncInvoker` 的指标上报改为显式观测回调 `Record`，装配点传入 `RecordToolCall`
  （指标名与标签不变），调用器因此不再依赖 `ai` 的指标实现；`ActionResult` 经
  `aliases.go` 以 `type ActionResult = runtime.ActionResult` 转发，`execute.go` 与
  用例零改动。调用器契约用例迁入 `runtime` 包内测（并补一条观测回调用例，
  ai 包族用例数 594→595）。
- **仍留在 `ai`（后续切片）**：`processWithTools`（主工具循环）与
  `execOneTool`（回合编排，读会话 / 配置 / 执行路径）、`handleAI` / `handleAIChat`
  与入站附件下载/引用图片水合（读 `history` / 会话内容缓存 / `cfg`）、计划后台推进
  （`runner.go`：读 `lifecycleCtx` 与生命周期调度，签名检测已改用
  `runtime.PlanSignature`）、审批摘要与生效配置读取（`approvalSummaryForTool`
  读 `cfg` / 群策略，`effectiveApprovalMode` 读群策略）。

## 九、门面退役（删除 `aliases.go`）

`aliases.go` 曾是唯一的再导出点：实现搬进子包后，`ai` 用 `type X = pkg.X`
把公共名重新贴回父包，让包内测试与仓库内调用点零改动。代价是**双份命名面**——
同一个契约既有 `ai.Tool` 又有 `toolkit.Tool`，读代码时先要判断当前用的是哪一份，
`ai` 也无法真正收敛为“只留装配”。

本插件没有仓库外调用方，因此本轮把门面整套删除，不留任何兼容 shim：

| 处理 | 内容 |
|------|------|
| 删除文件 | `builtin/ai/aliases.go`（353 行：类型别名 + 常量/变量/函数转发） |
| 包内改写 | `builtin/ai/*.go` 约 2,000 处标识符改限定名（`Config`→`config.Config`、`Message`→`protocol.Message`、`Tool`→`toolkit.Tool`、`Session`→`session.Session`、`captureSender`→`execution.CaptureSender`、`NewToolRegistry`→`toolkit.NewToolRegistry` 等） |
| 仓库内消费者 | 29 个文件（`builtin/{acl,antispam,auditlog,keywordfilter,stats,knowledgebase}`、`cmd/bot/**`）改 import 子包；最后只有 `cmd/bot/plugins.go` 用 `ai.New`、`cmd/bot/discovery.go` 用 `ai.Plugin` |
| 局部命名冲突 | 包内 27 个文件里名为 `session` 的局部变量/参数改名 `sess`（`session` 现在是子包名），`prompt.go` 的 `runtimeLocal` 改名 `runtimeCtx`；注释与断言文案里的“会话”原样保留 |
| 回归装配文件 | `NewProvider` 移回 `plugin.go`、`wireSpecs` 移回 `process.go`——两者此前分文件只是为了共用别名 |

`ai` 保留的公共面只剩两个，且都不是别名：`ai.New`（插件描述符构造）与
`ai.Plugin`（服务查找用的类型）。此后若要新增公共名，应先回答“它属于哪一层”，
把名字加在那一层的子包里，而不是回到 `ai` 做二次导出。

### 与历史小节的关系

§五、§八与修订记录 v1–v24 里所有“经 `aliases.go` 转发 / 对外零改动 /
外部插件零改动”的表述都是**当时**的事实，保留为历史记录；自本轮起这层
不再存在，读历史小节时请以本节为准。

## 十、精简收口（兼容层清理与方法归位）

`aliases.go` 退役后，`ai` 包内仍留有三类“可再收一层”的东西：只为旧接口存在的
转发、只做一次转手的薄封装、以及只依赖单一 owner 却挂在装配根上的方法。
本节记录这三类的判据与处理结果。

### 判据

- **只留业务语义，不留转发**：类型别名、`X` 等价于 `Y` 的别名函数、以及只为
  兼容旧调用方而存在的包装一律删除，不留 shim。
- **方法按依赖归属**：只依赖单一 owner 字段（含该 owner 的其他方法）的方法
  定义在该 owner 上。owner 是 `Plugin` 的匿名嵌入字段，因此提升后
  `p.<方法名>` 的全部调用点零改动；只有需要跨 owner 编排（典型是同时读 `cfg`
  与某个 owner 状态）的方法留在装配根 `Plugin` 上。
- **单处调用的纯转手内联回调用点**；但上下文键注入、消息文案、端口适配器这类
  “命名即语义”的构造器不按行数清理——它们承载的是封装，不是转手。
- **生产不可达的导出名按判据收口**：非测试引用数为 0 的导出名逐名裁决，删除、
  恢复调用或登记保留，判据与结果见下「已裁决」一节。

### 删除

| 处理 | 内容 |
|------|------|
| 删除方法 | `toolkit.SkillRegistry.Get`——注释即“为向后兼容保留，等价于 `GetSystem`”，唯一调用方是只测这条旧路径的用例；方法与用例一并删除（`GetSystem` 已有独立用例覆盖） |
| 删除 6 个导出名 | `Plugin.executeTool`、`Plugin.extractAndStore`、`promptctx.BuildGroupWindow`、`decision.ScoreTool`、`toolkit.ActionSpec.HasCategory`、`session.Session.ClearPendingImage`——生产调用方为 0，用例改到真实入口；逐名判据与改法见下「已裁决」表 |

### 内联

| 原位置 | 处理 |
|--------|------|
| `decision.namesEqual` | 纯 `slices.Equal` 封装、单处调用 → 调用点直接写 `slices.Equal(next, prev)`，函数删除 |
| `agentStateInfrastructure`（`agentstate.go`） | 仅 `agentstate_test.go` 使用的豁免名单，属测试夹具而非生产声明 → 与唯一消费者同址，移入 `agentstate_test.go` |

### 方法归位（owner）

| owner | 迁入方法 | 依据 |
|-------|----------|------|
| `catalogState` | `RegisterToolProvider`、`DiscoverToolProviders`、`hasToolPermission`、`filterToolsByPermission`、`rolesOf`、`hasAdminRole`、`hasSuperAdminRole`、`authorizeUsageQuery`、`userSkillActions` | 目录/注册表与 RBAC 权限评估，读写 `reg` / `skillReg` / `perms` / `cmdPatterns` |
| `adminState` | `groupPolicyFor`、`groupRequireMention`、`qqActionClickAllowed`、`qqActionBusyNoticeAllowed`、`notifyQQActionBusy`、`pruneQQActionRateLocked`、`resolveUsageTarget`，以及原包级函数 `qqActionRateKey`（成为 `adminState.rateKey`） | 群策略、按钮限流、用量命令解析，读写 `groupPolicies` / `actionMu` / `actionRate` |
| `contextState` | `buildMemoryContextN`、`memoryWriter`、`replyAndRecord` | 记忆检索与出站记录补偿，读写 `memory` / `history` |
| `executionState` | `handleApprovalButton` | 审批闸门，读写 `approvals` |
| `runtimeState` | `aiDefinition`、`triggerParses`、`autoReplyEligible`、`handleStopCommand`、`sessionTurnActive`、`subCommandRest`、`lifecycleSpawn` | 触发命令定义、会话中断/活跃判定与生命周期，读写 `def` / `defOnce` / `triggerCmd` / `sm` / `lifecycleCtx` |

归位后 `Plugin` 承载的方法由 129 降到 102；`pluginparts.go` 的字段分区文档补记了
这条“字段与方法同归属”的规则。`RegisterToolProvider` / `Discover*` /
`HealthCheckers` 仍是宿主（`cmd/bot`）与插件作者调用的契约，经嵌入提升后签名与
可用性都不变。

### 明确保留的层

| 保留项 | 原因 |
|--------|------|
| `pluginCommandCatalog` / `pluginSkillRunner`（`invoker.go`）、`loopToolSender`（`sendtool.go`）、`capability.go` 的四个 accessor | 消费方端口 → 插件能力的**装配适配器**：价值在于把 `*Plugin` 挡在子包之外，子包只认端口。删掉它们等于把 `*Plugin` 交回给子包 |
| `execution.ApprovalManager.PendingIDs` / `PendingCount`（`execution/approval.go`） | 装配侧读结果走 `req.Result()`，这两个只读查询是包内读侧与用例的观察口；无副作用、无策略语义，保留成本低于让用例去翻未导出字段 |
| `needAction` 恒真 | 冻结的架构闸门，收紧判定属行为变更（见 28） |
| `agentStateInventory()` | 归属表的唯一事实来源，生产与守卫用例共用 |
| `retrieval.WithPlanAccess` | 封装的 context key 类型未导出，内联会让调用方无法构造；属必要封装 |
| `memoryScopeKeys.UserScope` / `GroupScope` | `runtime.ScopeKeys` 端口实现，键格式仍只有 `userScope` / `groupScope` 一处定义 |

### 下一步

1. 本节范围内的兼容层清理与方法归位已收口，无遗留删除项：8 个生产不可达导出名
   已全部裁决落地（见下）。
2. **行为修正不在本节范围，且已全部完成**：C1–C5 与遗留项 L7 见
   `28-ai-layering-progress.md` 第十五～二十节（类型化失败判定退役
   `isToolErrorResult`、空运行时上下文节退役 `emitWhenEmpty`、历史检索同分
   确定性次序、群聊窗口开关两路径统一退役 `budgetEnabled`、审批门成为 `SendTo`
   授权唯一来源、检索原语导出）。这些既有结论仍受冻结清单约束——再改动即行为变更，
   必须独立提交并同步 `27-ai-freeze-checklist.md`，不与代码组织混做。

### 已裁决：生产不可达、仅用例引用的导出名（收口结果）

对全目录做了一次“非测试引用数 = 0”的扫描（去掉声明本身、全仓库范围），筛出 8 个
只在用例里被调用的导出名。本轮逐个裁决：**没有生产语义可保留的一律删除**（连用例
一起改到真实入口），**唯一有展示语义的 `IncrementUsage` 恢复调用**，**只读观察口
保留并在此登记**。

| 名 | 位置 | 裁决 | 落地 |
|----|------|------|------|
| `Plugin.executeTool` | `execute.go` | 删除 | 纯文本入口与 `executeToolResult` 重复；14 处用例改断言 `p.executeToolResult(...).Text` |
| `Plugin.extractAndStore` | `extract.go` | 删除 | 一行转手；3 处用例改走 `p.memoryExtractor().Extract(...)` |
| `promptctx.BuildGroupWindow` | `promptctx/group.go` | 删除 | 生产只走 `BuildGroupWindowN`；10 处用例改传 `p.cfg.ContextGroupMessages` |
| `decision.ScoreTool` | `decision/score.go` | 删除 | 只取总分的便利函数，生产只读三元组；3 处用例改 `_, _, ws := decision.ScoreToolParts(...)` |
| `toolkit.ActionSpec.HasCategory` | `toolkit/action.go` | 删除 | 生产无调用；2 处用例改用 `assert.Contains/NotContains(t, spec.Categories, ...)` |
| `session.Session.ClearPendingImage` | `session/attachment.go` | 删除 | 唯一名义调用点（合并超限拒绝）已先走 `ConsumePendingImage` 清空，该方法自始不可达；连同其用例一起删 |
| `toolkit.SkillRegistry.IncrementUsage` | `toolkit/skill.go` | **恢复调用** | `UsageCount` 仍在 `/ai skill list` 与技能详情展示“调用 N 次”，恒为 0 属展示失真；在 `executeSkill` 入口按技能自身 owner + name 记账，嵌套调用同样计被执行的技能 |
| `execution.ApprovalManager.PendingIDs` / `PendingCount` | `execution/approval.go` | 保留 | 无副作用的只读观察口，见「明确保留的层」 |

三种删除判据（满足其一即删）：

1. **同义重导出**：与既有入口同体不同名（`executeTool`、`extractAndStore`）。
2. **可选便利函数**：生产只用更完整的出口（`ScoreTool`、`BuildGroupWindow`）。
3. **不可达方法**：文档化的调用点在真实控制流里被更早的语句覆盖
   （`ClearPendingImage`）。

`IncrementUsage` 不属于以上任何一类——它有真实消费方（计数展示），只是调用被漏掉了，
因此按“修复漏接线”处理，而不是删除。

## 修订记录

| 版本 | 修订 |
|------|------|
| v1 | 建立边界设计：现状量化、目标包与依赖方向、S1–S4 阶段、风险登记；记录 `ai/config` 归位完成 |
| v2 | 记录 `ai/protocol` 归位：线格式类型与解析导出、`ChatRequest.Tools` 收敛为 `ToolSpec`、装配与指标留在 `ai`；测试按"断言真实实现"随实现归位。全量回归 590/0/0 |
| v3 | 记录 `ai/toolkit` 归位：工具/动作/技能契约与注册表成包，工具名规范一并归位；公共名经 `aliases.go` 转发，调用方改限定名；`toolctx.go` 因携带平台发送器随 `admin` owner 下沉时再归位。全量回归 590/0/0 |
| v4 | 记录 `ai/retrieval` 归位：检索骨架成包并退役同义包装（导出名即唯一实现）、`SemanticFallbackLog` 等跨包结构导出、`toolEmbeddingText` 留在工具选择域；检索与嵌入用例随实现迁入包内测。全量回归 590/0/0 |
| v5 | 记录 `ai/session` 归位：会话状态、管理器、持久化记录与三类会话级缓存整体成包，依赖只朝 `protocol`/`toolkit`；先做一次纯改名提交（导出跨包成员）再做物理搬迁；会话语义用例迁入包内测。S1 五个叶子/契约包全部完成。全量回归 590/0/0 |
| v6 | 记录 S2 字段分区：`Plugin` 的 28 个字段归入 5 个 owner 结构体并匿名嵌入（`pluginparts.go`），`cfg`/`prov` 作为装配根共享依赖直属；Go 1.27 允许 keyed 字面量使用提升字段名，249 处 `Plugin{...}` 与全部 `p.<field>` 访问零改动；新增 `pluginparts_test.go` 四条分区契约；§七 给出面向 S3 的跨 owner 读取清单。全量回归 590/0/0 |
| v7 | 记录 S3 试点 `ai/execution` 首次释放：`safety.go`（`IsSafeCommandArg` / `ParseToolPermission`）与 `retry.go`（`BuildReflectionMessage` / `BuildRetryAbortMessage`）成包，调用方改限定名，四个用例随实现迁入包内测，`ai/retry.go` 整体出线；§八 记录该 owner 的已释放/仍留在 `ai` 清单与后续释放顺序。全量回归 593/0/0 |
| v8 | 记录 `ai/execution` 第二次释放：捕获发送器成包为 `CaptureSender`，捕获结果收敛为导出字段 `CapturedText`/`CapturedAttachments`，`ai` 侧以 `type captureSender = execution.CaptureSender` 转发使读侧构造方式不变。全量回归 593/0/0 |
| v9 | 记录 `ai/execution` 第三次释放：审批闸门整体成包（`ApprovalRequest` / `ApprovalManager` / 按钮前缀 / 四个纯解析函数），结果通道以 `Result()` 只读暴露、包内读侧改用 `PendingIDs()`/`PendingCount()` 查询；`requestApproval` 等三个交互入口因依赖 `cfg` 与回复能力仍留在 `ai`。四个用例随实现迁入包内测。全量回归 593/0/0 |
| v10 | 记录 `ai/execution` 第四次释放：真实命令通道成包为 `RunCommand`，`execution` 首次以**消费方端口**（`CommandPatterns` / `EventProcessor`）替换对 `*Plugin` 的反向读取，`ai` 侧由 `pluginCommandCatalog` 适配注入，插件方法 `executeRealCommand` 出线；权限评估明确不回填执行侧（属策略/决策语义）。全量回归 593/0/0 |
| v11 | 记录 `ai/catalog` 首次释放：技能注册形态成包（`SystemSkill` / `UserSkill` / `ToolFromSkill`），`RegisterSkill` / `RegisterUserSkill` / `registerSkillAsTool` 保留为 `*Plugin` 方法并交出校验与形状构造，需要 `cfg` 的上限检查与注册表写入仍留在装配侧。全量回归 593/0/0 |
| v12 | 记录工具调用上下文并入 `ai/toolkit`：`ai/toolctx.go` 整体出线，公开视图 `ToolSource` 保持不含平台发送器，执行路径改用 `WithToolInvocation` 注入、推送类工具改用 `PlatformSenderFromContext` 读取；`ai` 侧经 `aliases.go` 转发 `ToolSource` / `WithToolSource` / `ToolSourceFromContext`，外部插件（`cmd/bot/plugins/minecraft`）零改动。用例随实现迁入 `toolkit` 内测，并新增一条"发送器随调用注入"用例（594 通过）。全量回归 594/0/0 |
| v13 | 记录 `ai/catalog` 第二次释放：四个内置动作集（`BuildSendTools` / `BuildReminderTools` / `BuildTodoTools` / `BuildMemoryTools`）与能力端口整体成包，`ai` 只保留组合根与端口适配器；作用域键与存储管理器留在 `ai`，发送器与发送预算留在装配侧；提醒时长解析/格式化随动作成包并被 `/ai remind` 子命令复用；动作名常量导出，调用方改限定名。用例按"断言真实实现"归位（时长用例迁入 catalog、端口注入用例补在 catalog 内测，动作级用例仍走真实装配）。全量回归 594/0/0 |
| v14 | 记录 `ai/catalog` 收口：计划动作形状与参数校验迁入 `catalog/plantool.go`，计划状态读写经消费方端口 `PlanAccess`（`catalog → session` 属 §三 的下行边），装配侧注入会话本身；`ai` 只留注入约定 `WithPlanSession` 与重规划编排。同时明确 catalog 的收口边界：`Discover*` / `Register*Provider` 与 `pluginCatalogSink` 是"扫 `plugin.Manager` + 写插件目录"的装配动作，且 `ToolProvider` / `SkillProvider` 是 `ai` 暴露的插件作者接口，下沉会引入 `catalog → ai` 回边并制造只有一种实现的端口，故固定留在 `ai`。全量回归 594/0/0 |
| v15 | 记录上下文 owner 首次释放：新增叶子包 `textutil`（跨 4 个 owner 的纯文本工具）与 `promptctx`（回复链、群窗口、动态上下文节来源/装配/预算、`EstimateTokens`）；`ai/context.go` 收敛为 `message.go`（出站回复面 + 入站合并转发归一化），`budget.go` 出线（`effectiveCustomPrompt` 并入 `prompt.go`），`context_pipeline.go` 收敛为 `dynamiccontext.go`（只声明节序与回退）。全量回归 594/0/0 |
| v16 | 记录 promptctx 第二次释放：消息级 RAG 全流程与运行时上下文渲染成包，RAG 用例与 `GroupRoleName` 用例随实现迁入内测；会话级缓存的复用策略上移到 `session`（`CacheReuseTTL` / `CacheReuseJaccard`），工具选择缓存的常量改为引用它，冻结断言值不变；`ai/rag.go` 收敛为 `ragcontext.go` 装配门面。全量回归 594/0/0 |
| v17 | 记录 promptctx 收口：长期记忆注入（`BuildMemoryContext` + 消费方端口 `MemoryReader`）与机器人回复去重（`BotReplyContents`）成包，`memoryStore` 增加 `RetrieveTexts` 交出注入所需文本；`handler.go` 只余稳定系统提示词与会话管理装配。上下文 owner 收口。全量回归 594/0/0 |
| v18 | 记录决策 owner 收口：打分/选择/稳定策略成包为 `builtin/ai/decision`（score / selection / stabilize 三文件），`ai/select.go` 与 `ai/toolset.go` 整体出线，`ai/decision.go` 只余候选发现、闸门与审批评估三个装配面，并以 `ToolSetObserver` 端口承接原有的指标回调（指标名与标签不变）；`process.go` 里的局部变量 `decision` 改名 `verdict` 以避免与新包名同形。全量回归 594/0/0 |
| v19 | 记录回合运行时首个切片：`builtin/ai/runtime` 成包（单轮 LLM 调用 `Client`、回答校验 `Verifier` / `ParseVerdict`、事实抽取 `Extractor` / `ParseExtractedFacts`），`ai/process.go` 的单轮调用收敛为装配，`ai/verify.go` / `ai/extract.go` 只余编排与生命周期；抽取以 `MemoryWriter` / `ScopeKeys` 消费方端口替换对插件字段的反向读取，`singleRoundResult` / `verifyResult` 经 `aliases.go` 转发。全量回归 594/0/0 |
| v20 | 记录回合运行时第二个切片：消息与载荷的纯形状工具成包为 `runtime/message.go`（文本提取、多模态占位、工具调用序列自愈、参数摘要、结果截断、错误文案与占位常量）与 `runtime/request.go`（`Retention` + `InjectDynamicContext` / `PrepareRequestMessages`），`ai/process.go` 由约 906 行收敛到约 529 行、只余回合编排与装配；调用点改限定名、`imageRetentionConfig` 返回 `runtime.Retention`，用例随之改限定名。全量回归 594/0/0，全仓 124 包通过 |
| v21 | 记录回合运行时第三个切片：并行编排（`ExecuteToolCallsParallel` + `ToolExecResult`）与调用追踪（`RecordToolTrace`）成包为 `runtime/turn.go`，配置推导预算（`EffectiveToolRetryLimit` / `EffectiveApprovalTimeout` / `EffectiveTurnTimeout`）成包为 `runtime/limits.go`，事件 deadline 提升（`LiftEventDeadline`）成包为 `runtime/deadline.go`；`ai/process.go` 只余 `processWithTools` 与 `execOneTool` 装配，`execOneTool` 返回值改用 `runtime.ToolExecResult`，`ai` 不再保留同名私有转发。全量回归 594/0/0，全仓 124 包通过 |
| v22 | 记录回合运行时第四个切片：入站消息归一化成包为 `runtime/inbound.go`（附件类型判定、待合并图片引用与拼装、命令样式识别、会话 ID、稳定系统消息、触发前缀清洗）与 `runtime/forward.go`（合并转发识别/触发/图片提取），`ai/handler.go` 915→703 行、`ai/message.go` 127→56 行，`cleanMessage` / `hasTriggerPrefix` 由插件方法改为收触发前缀参数的纯函数，调用点散落 7 个文件的 `p.cleanMessage` 全部改限定名。全量回归 594/0/0，全仓 124 包通过 |
| v23 | 记录回合运行时第五个切片：计划编排约定成包为 `runtime/plan.go`（`PlanSignature` / `WithPlanSession` / `BuildReplanMessage`，`ai/plan.go` 整体出线），`EffectivePlanAutoRounds` 并入 `runtime/limits.go`；`runner.go` 的无进度签名检测与 `process.go` 的重规划指令/计划端口注入改限定名，用例随之改限定名。全量回归 594/0/0，全仓 124 包通过 |
| v24 | 记录动作调用器归位：三个调用器（`FuncInvoker` / `SkillInvoker` / `CommandInvoker`）与 `ActionResult`、消费方端口（`SkillRunner` / `CommandCatalog`）成包为 `runtime/invoke.go`，`ai/invoker.go` 收敛为两个适配器；指标改为显式观测回调注入，`ActionResult` 经 `aliases.go` 转发；调用器契约用例迁入 `runtime` 包内测并补观测回调用例。全量回归 595/0/0，全仓 125 包通过 |
| v25 | 门面退役：删除 `aliases.go`（353 行），包内约 2,000 处标识符改限定名，29 个仓库内消费者改 import 子包，`ai` 只保留 `New` / `Plugin`；`NewProvider` / `wireSpecs` 回归装配文件，`ai.go` 包文档按当前分层重写。基准口径：`go test ./builtin/ai/ -v` 565 通过 / 0 失败（与 v24 一致），`./builtin/ai/...` 711 通过 / 0 失败，全仓 125 包通过 |
| v26 | 无状态助手归位与薄封装内联：`qqActionRateKey`→`adminState.rateKey`、`reminderKey`→`reminderManager.key`、`memoryScope`→`memoryAccessor.scope`、`mergeSimilar`/`mergeLengthOK`/`charSetContainment`→`memoryStore` 方法、`filterToolsByGroupPolicy`→`GroupPolicy.FilterTools`、`isPlainID`→`loopToolSender.isPlainID`、`makeSkillAddSessionID`/`extractSkillDescription`→`catalogState` 方法、用量与帮助文本 14 个助手→`adminState` 方法；单处调用的 `havePermissionSource`、`respUsage` 内联回调用点。全量回归 711/0/0（含子测试），全仓 125 包通过 |
| v27 | 精简收口：删除 `SkillRegistry.Get` 兼容别名及其用例；`decision.namesEqual` 内联为 `slices.Equal`；`agentStateInfrastructure` 移入 `agentstate_test.go`；27 个只依赖单一 owner 的方法归位（`catalogState` 9 个、`adminState` 7 个 + 原包级 `qqActionRateKey`→`adminState.rateKey`、`contextState` 3 个、`executionState` 1 个、`runtimeState` 7 个），`Plugin` 方法数 129→102；§十 记录判据、保留层、残留项，并登记 8 个“生产不可达、仅用例引用”的导出名待裁决。回归：`./builtin/ai/` 565 通过 / 0 失败、`./builtin/ai/...` 711 通过 / 0 失败，全仓 125 包通过 |
| v28 | 收口裁决：8 个“生产不可达、仅用例引用”的导出名逐个落地——删除 6 个（`Plugin.executeTool`、`Plugin.extractAndStore`、`promptctx.BuildGroupWindow`、`decision.ScoreTool`、`toolkit.ActionSpec.HasCategory`、`session.Session.ClearPendingImage`）并把 32 处用例改到真实入口，保留只读观察口 `execution.ApprovalManager.PendingIDs` / `PendingCount`，**恢复 `toolkit.SkillRegistry.IncrementUsage` 在 `executeSkill` 入口的调用**（技能调用计数一度恒为 0，与其展示语义不符）并补冻结用例 `TestSkillLoopCountsUsage`；删除不可达方法的同时删掉其验证用例 `TestSessionPendingImageRejectPath`。基准口径：`--- PASS` 行数（含子测试）`./builtin/ai/` 565 / 0 失败、`./builtin/ai/...` 710 / 0 失败，全仓 125 包通过，`gofmt` / `go vet` 干净 |
| v29 | 文档订正：§十「下一步」原称行为修正 backlog“未开工”、并称 `isToolErrorResult` 仍在兼容位——与事实不符（该方法已在 28 第十五节退役，代码中 0 命中）。改为如实记录 C1–C5 与 L7 已在 28 第十五～二十节完成，本节只保留“再改动须独立变更并同步冻结清单”的约束。无代码改动 |
