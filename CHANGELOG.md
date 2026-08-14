# Changelog

## v1.40.2 (2026-08-14)

### 🐛 QQ 平台：OpenAPI 错误响应被静默吞掉（修复）

- **根因**: `infra/httpclient.DoJSON` 不检查 HTTP 状态码，QQ 的错误响应（4xx/5xx + 错误码体）被当作成功解析——消息发送类接口拿到空 `MessageID` + `nil` 错误，表现为"handler 执行成功但消息未发出"且日志无任何报错
- **修复**: `platform/qq/openapi` 的 `Post`/`Put`/`Patch`/`Get`/`Delete` 统一走 `doAndCheck`——检查 HTTP 状态码与响应体 `code`/`err_code` 字段，错误以结构化错误返回
- **补充**: `core/context` 的 `submitReply` 对发送失败记录 WARN（chat_id + 消息预览），所有 `ctx.Reply` 出站失败不再静默
- **测试**: 新增错误体（4xx）、2xx 内嵌 `code`、2xx 内嵌 `err_code` 三类用例

### ✨ QQ 平台：入群欢迎消息不再依赖群"允许主动在群聊内发言"开关

- `GROUP_MEMBER_ADD` 事件实测支持 `event_id` 被动回复（官方文档的支持清单未列出），欢迎消息以被动回复发送，无需开启群主动消息权限
- `GROUP_MEMBER_REMOVE` 实测**不支持** `event_id` 被动回复（错误码 40034027），告别消息只能以主动消息发送，仍依赖群开关——设置告别消息时 QQ 平台附加提醒

### 🐛 QQ 平台：群成员进出事件字段解析错误（修复）

- `GROUP_MEMBER_ADD`/`GROUP_MEMBER_REMOVE` 事件体的成员字段为 `member_openid`（非 `op_member_openid`），原解析导致进出成员 ID 为空（`{user}` 占位符渲染为空）
- 两个事件的解析合并为 `populateGroupMemberEvent`，token 差异由事件类型决定

### 🐛 welcome 插件：群配置遮蔽全局配置（修复）

- **根因**: 群内执行任意 `set`/`off`（如 `/farewell set`）会新建群配置条目，而生效配置"存在条目即整体使用"——半空的群配置意外屏蔽全局欢迎/告别
- **修复**: 改为**按字段回退**——`GroupConfig` 新增 `WelcomeSet`/`FarewellSet` 显式配置标记，群内未显式配置的字段继续回退到全局；`load()` 对旧数据（无标记）做字段推断兼容
- **测试**: 新增 4 项——只配告别不影响全局欢迎、只配欢迎不影响全局告别、群级关闭优先、群级配置优先

## v1.40.1 (2026-08-12)

### 💬 AI 插件：长任务进度汇报引导（send_message 主动使用）

- **背景**: 实测"搜索一下最近关于显卡方面的消息"这类单查询任务不会调用 send_message 分步汇报——工具虽恒被选中（general 必保集），但框架提示词无任何引导、工具描述为被动许可（"用于多步骤任务"），模型默认跳过
- **修复**: 双管齐下（措辞均限定"多轮任务"防小任务刷屏）——
  - `DefaultFrameworkPrompt` 新增"进度汇报"节：需要多轮工具调用的任务每完成一步先用 send_message 报告进度（"正在搜索…""第 1 步完成"），再继续下一步；单轮即可完成的任务不要调用
  - `send_message` 工具描述改为祈使句："每完成一步先调用本工具报告进度…；单轮即可完成的任务不要调用"
- **测试**: 新增守卫用例，断言两处引导文本同时存在（防止后续重构删掉任一侧）

### 🐛 AI 插件：send_message / send_to 消息格式跟随 markdown 配置（修复）

- **根因**: 发送工具组装消息时一律走纯文本 `Text` 字段，仅显式 `format: "markdown"` 才填 `Markdown`——与 AI 最终回复的行为（`markdown` 配置默认 true 即 Markdown 渲染）不一致，中间进度消息即使包含 Markdown 也按纯文本发送
- **修复**: `buildOutboundMessage` 默认跟随插件 `markdown` 配置（与最终回复一致，平台不支持时由 Sender 自动降级纯文本），`format` 参数仅作显式覆盖（text / markdown）
- **测试**: 更新 6 项——默认跟随（true→Markdown / false→纯文本）、双向显式覆盖、图片/@提及、空消息与超长校验

### 🐛 AI 插件：多轮工具任务被全局 30s 超时整段切断（修复）

- **根因**: `cmd/bot` 全局 `middleware.Timeout(30s)`（setup.go）给事件上下文注入单次 deadline，AI 多轮工具循环（每轮 LLM 调用 + 工具执行）共享该 deadline——第一轮 LLM 与工具执行耗掉部分预算后，第二轮请求在 30s 整被切断（`context deadline exceeded` → 用户看到"请求超时"）。插件的 `api_timeout`（默认 60s）因继承更短 deadline 永远到不了
- **修复**: `processWithTools` 入口以插件独立预算替换事件上下文 deadline——`context.WithoutCancel` 剥离全局中间件注入的 deadline（保留 values/tracing），再套 `turn_timeout`（默认 0 = 自动推导 `api_timeout × max(2, min(max_depth, 5))`，如默认配置 60s×5=5 分钟）；退出时恢复原上下文
- **配置**: 新增 `turn_timeout`（见 `config.example.yaml`），显式设置可精确控制单次 AI 处理总预算
- **测试**: 新增 3 个用例——默认推导/封顶/回退/显式优先、deadline 替换与恢复、多轮循环在事件 deadline 已过期时仍完整跑完（复现生产场景）

### 🐛 AI 插件：工具调用序列损坏导致 LLM API 400（修复）

- **根因**: OpenAI/Anthropic 硬性要求 `assistant(tool_calls)` 后必须紧跟每个 `tool_call_id` 的 tool 消息，缺失即 400 `insufficient tool messages following tool_calls message`。三条损坏路径：① 中断抢占跳过的工具调用不入历史；② `replanMsg`（user 角色）插在 assistant(tool_calls) 与 tool 消息之间；③ 进程异常退出/持久化损坏导致历史残缺
- **修复**: ① 跳过的工具调用补占位 tool 消息（"对话被新消息打断"）；② 重规划指令挪到全部工具结果之后追加；③ 新增 `repairToolCallSequence`——每轮发请求前扫描历史，缺失的 `tool_call_id` 自动补占位 tool 消息，**已损坏的持久化会话自愈**，无需手动 `/ai reset`
- **测试**: 新增 7 个用例——修复函数表驱动 6 项（完整序列不变/缺失中间/末尾全缺/孤儿消息/多轮隔离/无工具调用）+ 中断跳过补位集成用例

## v1.40.0 (2026-08-12)

### 💬 AI 插件：消息发送工具（send_message / send_to）

为 AI 补齐消息发送能力，覆盖两种场景：多步骤任务中向用户展示**中间进度**，以及**跨会话推送**（提醒某人、给某群发通知）。

- **`send_message`**（当前会话，无需审批）: 向当前会话发送中间进度消息——文本/Markdown/图片/@提及（`format`/`image_url`/`mention_ids` 可选参数），支持现有 `OutboundMessage` 全消息类型，经 messagelog 记录（群聊窗口可见）
- **`send_to`**（跨会话推送，强制审批）: 向指定用户/群推送消息——`AlwaysRequireApproval` 保证 **`tool_approval=off` 模式下也强制审批**，审批之上叠加 **`ai.message.send` RBAC 权限**（权限不足直接拒绝、不发起审批）
- **目标自动解析（零配置）**，按优先级：
  1. 内置别名：`本群/这个群/当前群`（→ 当前群）、`我/我自己`（→ 调用者）、`对方/本会话`（私聊 → 会话对方）
  2. **当前会话近期发言者昵称**（messagelog，全平台可用）：精确 → 大小写不敏感，按用户去重
  3. **已加入群群名**（平台实现 `GroupInfoProvider` 时，如 QQ 官方不支持则自动跳过）
  4. **原始 ID 兜底**：仅接受 ASCII 标识符（`[A-Za-z0-9_-]`），避免中文昵称被静默当作 ID；`is_group` 参数指定类型
- **同名歧义**: 匹配到多个用户时返回候选 ID 列表错误（如「张三」匹配到多个近期发言者（u1、u2）），模型读到后改用原始 ID 重试
- **审批前预解析**: `send_to` 的审批消息展示**解析后的目标**（如 `target=张三（u1）`），批准者明确知道消息将发送给谁；目标无法解析时模型可在批准前获知并调整
- **能力注入模型**: `Tool.Execute` 签名不变——发送能力经 context 注入（`WithToolSender`/`ToolSenderFromContext`，复用 `WithCallerInfo` 模式）；**`SendTo` 仅在本次调用通过审批门后注入**（`sendToAllowed`），嵌套 Skill 调用继承同一 context，无法绕过审批；`ResolveTarget` 为只读操作不受门控
- **防滥用**: `max_sends_per_round` 配置（默认 5，<=0=不限）——一次对话处理内发送总次数上限，原子计数，并行工具执行下同样生效
- **实现**: 新增 `sendtool.go`（loopToolSender + 工具定义 + 目标解析）、`ToolSender`/`ChatTarget` 类型与 `Tool.AlwaysRequireApproval` 字段；审批流优化——权限校验前移至审批之前（避免"先批准后告知无权"）
- **测试**: 新增 15 个用例——目标解析表驱动（内置别名/群成员昵称/私聊对方昵称/大小写不敏感/歧义候选/群名匹配/原始 ID/无效目标）、发送路径（NotifyUser/NotifyGroup/普通 Send 回退/未授权拒绝/预算上限）、审批链路（off 模式强制审批、无权限不发起审批、审批消息显示解析后目标）

## v1.39.0 (2026-08-11)

### 🎯 AI 插件：工具选择器（本地检索 Top-K，替代 LLM 单分类路由）

- **背景**: 工具已 60+（显式注册约 45 个 + 自动发现命令），旧的"LLM 单分类路由"（toolThreshold=20 触发）有四个缺陷——每轮多一次路由 LLM 调用、单分类无法覆盖跨域任务、缓存 TTL 内换话题仍复用旧分类、路由行为不可测试
- **`selectToolsForTurn` 纯本地打分选择**: 按最后一条用户消息分词（英文单词 + CJK 二元组 + 孤立汉字，零分词库依赖）对全部工具打分——
  - 关键词命中: 工具名完整出现权重最高 > 工具名 token > 描述/分类 token
  - 会话热用 +1.5: 本会话已调用过的工具加权，多轮任务工具不抖动
  - 必保集: 通用工具（Categories 空或含 general，即 9 个 `xxx_query` 兜底工具天然常驻）+ 会话已用工具恒被选中
  - 跨域自动覆盖: Top-K 从不同域各取高分者，单分类限制消除
- **token 预算**: `tool_budget`（默认 8000）按 schema 估算逐条累加封顶，防止工具描述撑爆上下文
- **会话级选择缓存**: TTL 10 分钟 **且** 关键词 Jaccard ≥ 0.5 才复用（修复旧路由"TTL 内换话题误复用"缺陷）；命中则跳过打分与 embedding 调用
- **确定性**: 同分按工具名升序，选择结果不依赖 map 迭代随机序；最终按原始注册序返回（利于 prompt 缓存）

### 🔮 AI 插件：语义检索加权（embedding）

- **`embedding_base_url` 配置**（OpenAI 兼容 /embeddings 端点）: 启用后工具选择叠加余弦相似度（权重 2.0）——未配置或请求失败自动降级纯关键词，不影响对话主流程
- **工具向量惰性缓存**: 工具文本（名+描述+分类）只嵌入一次；查询向量每轮一次（命中会话缓存则零调用）
- **Anthropic 无 embedding API**: 文档注明可配第三方兼容服务或本地 Ollama（OpenAI 兼容协议）

### 🔁 AI 插件：工具失败重试预算 + 反思引导（Self-reflection）

- **`tool_retry_limit` 配置**（默认 2）: 同一工具连续失败的允许次数——失败结果照常回填 LLM（模型天然可重试），连续失败达到 limit+1 次时**优雅中止本轮**，回复说明"已停止尝试 + 最后错误"，替代原先撞 `max_depth` 的裸错误
- **显式反思轮**: 同一工具第 2 次连续失败起，追加一条"反思指令"用户消息（要求模型分析失败原因：参数/结果格式/工具选择，并采用不同策略；无法解决则直接告知用户），强制造型思维再行动，避免模型反复执行相同的失败操作
- **按工具独立计数**: 各工具连续失败计数互不影响；某工具执行成功后计数清零——单次失败不注入反思、不中止（回归测试覆盖）
- **审批拒绝不计入失败**: 用户拒绝执行的工具调用跳过计数（`approval` 分支在失败统计之前 continue）
- **实现**: 计数存于 Session（`toolFailures`，不持久化），逻辑集中在 `processWithTools` 工具循环 + 新增 `retry.go`（错误识别/反思消息/中止消息），无新依赖
- **测试**: 新增 8 个用例——错误识别（含审批拒绝不算失败）、反思/中止消息内容、重试上限默认值、会话计数与重置、连续失败 3 次中止（含反思注入校验）、失败-反思-成功完整链路、单次失败不反思不中止

### 🧠 AI 插件：长期事实记忆（自动抽取，无新增命令）

- **`memory_enabled` 配置**（默认 false，隐私考虑）: 开启后每轮对话回复完成后**异步调用一次 LLM 自动提取**稳定长期事实（偏好/习惯/约定），按作用域持久化——零新增命令，用户无需任何操作
- **两级作用域**: `user:<id>`（跨群用户事实）+ `group:<id>`（群内公共事实，私聊强制归入用户作用域）；存储于独立 LevelDB `data/ai_memory`（与群策略 `data/ai` 分开，避免 LevelDB 锁冲突）
- **自动抽取链路**: 对话结束后取最近一轮 用户消息+助手回复 → 非流式 LLM 调用（30s 超时，插件生命周期 context，不依赖事件 context）→ 鲁棒解析 JSON 数组（容忍前后缀文本/坏 JSON/空数组）→ 写入存储；失败静默降级不影响对话
- **节流**: `memory_min_interval`（默认 10 分钟）同一作用域两次抽取最小间隔，防止高频对话 LLM 调用成本失控
- **去重合并**: 精确相等 / 二元组 Jaccard ≥ 0.6 / **字符集包含度 ≥ 0.75**（短中文句的补充信号，实测"喜欢喝咖啡"vs"爱喝咖啡"0.86 合并、vs"喜欢玩原神"0.43 不合并）——合并保留原文本、次数 +1
- **上限淘汰**: `memory_max_facts`（默认 50/作用域）超出按出现次数最少、最旧优先淘汰
- **检索注入**: `buildSystemPrompt` 新增"长期记忆"节——按用户最近消息关键词对事实打分（复用工具选择分词器，出现次数弱加成），取 Top-N（`memory_inject_max` 默认 8）注入，附"冲突以用户当前说法为准"提示；无关事实不注入
- **测试**: 新增 14 个用例——存取/精确合并/相似合并（含正反例）/淘汰/检索排序/持久化重开/节流/解析鲁棒性/最近一轮提取/抽取入库（群+用户）/私聊作用域回退/禁用不 panic/注入检索

### 📋 AI 插件：任务规划层（Plan 工具）

- **`create_plan` / `update_plan_step` 内置工具**（general 类别，恒被选中）: 模型按需调用——复杂任务（≥3 步）先创建计划再逐步执行；简单任务不建计划零开销
- **计划状态注入**: 存在进行中的计划时，每轮 LLM 调用前把"当前计划+进度"作为 system 消息注入（插在主系统提示后）——模型有状态可依，不再靠隐式记忆推进多步任务，中途失焦/重复工具调用大幅减少
- **用户可见**: 创建计划时同步把计划文本发送到聊天（含 ✅ 完成/失败状态标注），后续步骤更新静默不打扰
- **步骤状态机**: `pending → in_progress → done/failed`，支持备注（执行结果/失败原因）；全部步骤 done/failed 后计划结束、停止注入
- **约束**: `plan_max_steps`（默认 8）防滥用——超限报错提示模型合并步骤；步骤数 < 2 拒绝；非法状态/未知 step_id/无计划时更新均返回可读错误
- **上下文传递**: `WithPlanSession` 把会话注入工具 context（复用 WithCallerInfo 模式），内置工具 Execute 内读写计划；计划存于 Session（`json:"-"`，不持久化）
- **框架提示词**: `DefaultFrameworkPrompt` 新增"任务规划"节（复杂任务先建计划、逐步更新、完成后总结）
- **测试**: 新增 7 个用例——计划格式化/完成判定/会话存取（快照隔离+完成停止注入）/建计划（正常+步骤不足+超限）/更新步骤（全分支+完成闭环）/计划注入请求消息/完整链路（建计划→逐步更新→总结，4 轮 LLM 调用）

### ⚖️ AI 插件：回答质量校验（LLM-as-judge）

- **`verify_enabled` 配置**（默认 false）: 最终回答发送给用户前，先经一次非流式评审 LLM 调用检查——是否直接完整回答了用户问题、是否捏造无工具结果支撑的事实、是否清晰；不通过则**注入修正指令**（附评审原因）重新生成
- **重试语义**: `verify_max_retries`（默认 1）——每次生成（含修正稿）都先评审；通过即返回；达到上限仍不通过则返回最后一份回答，不无限重试
- **评审输出鲁棒解析**: JSON `{"verdict":"pass|fail","reason":...}` 优先（容忍前后缀文本）；无 JSON 按关键词兜底（fail 关键词先于 pass 检查，规避"不通过"含"通过"子串的误判）；无法判定默认通过（校验失败不阻塞对话）
- **降级安全**: 校验器自身调用出错（超时/API 故障）时跳过校验直接返回原回答，绝不让评审故障卡住对话
- **评审输入**: 固定使用用户原始问题（重试后会话最后一条 user 消息是修正指令，已提前缓存），修正稿以同一问题为基准评审
- **接入点**: `handleAIChat` 改调 `generateVerified`（生成 → 评审 → 重试循环），未开启校验时与旧路径零差异（零评审调用，测试覆盖）
- **测试**: 新增 7 个用例——解析（JSON/前后缀/纯文本/子串规避/无法判定）、评审调用与错误传播、修正指令内容、通过零重试、失败-修正-通过链路（校验 2 次）、达上限返回最后回答（3 次生成）、未开启零调用

### 🔎 AI 插件：记忆检索语义化（embedding 复用）

- **`toolVectorCache` 重构为通用 `textVectorCache`**: 向量缓存键从"工具名"改为"文本内容"——工具描述与记忆事实共用同一缓存实例，相同文本只嵌入一次（实测：记忆事实文本与工具嵌入文本相同时，事实批量嵌入零调用）
- **记忆检索叠加语义余弦**: `memoryStore.Retrieve` 注入共享 embedder 后，打分 = 关键词重叠 + 出现次数弱加成 + 余弦相似度 ×2.0——无关键词重叠但语义相近的事实也能命中（"今天想喝点东西"→"用户喜欢喝咖啡"）
- **信号门槛修正**: 计数加成不再单独构成入选理由——基础信号（关键词重叠 或 语义余弦）必须 > 0，防止零重叠噪声事实入选；未配置/嵌入失败时维持纯关键词行为（回归测试覆盖）
- **惰性嵌入 + 10s 超时**: 事实向量缺失才嵌入（缓存文本级复用）；检索在主对话路径上，嵌入失败立即降级纯关键词，不拖慢 system prompt 构建
- **零新配置**: 复用 `embedding_base_url` 一套配置；未配置 embedding 时记忆功能行为与旧版完全一致
- **测试**: 新增 3 个用例——语义命中（含嵌入调用计数/事实缓存复用）、嵌入失败降级纯关键词、与工具共享缓存零重复嵌入；全量测试与 `-race` 通过

### 📚 AI 插件：消息级 RAG（历史消息语义检索）

- **`context_rag_messages` 配置**（默认 0 = 关闭）: 开启后按用户消息在 messagelog 历史（默认最近 7 天）中检索相关消息注入系统提示——补上"上周讨论的方案""上次谁说的"这类**时间线/细节查询**（最近窗口只覆盖 10 条、事实记忆只存稳定事实，两边都覆盖不到）
- **两阶段检索**: ① 本地关键词预筛（复用工具选择分词器，零成本）→ ② 对候选集做 embedding 语义精排（复用共享 `textVectorCache`）——**无关键词命中即返回，日常消息零 embedding 调用**；语义仅靠 embedding 召回的能力由事实记忆承担（其检索自带语义加权）
- **候选筛选**: `context_rag_days`（7 天）/`context_rag_candidates`（500 条）——复用 messagelog 现成 `QueryGroupFromDB`/`QueryUserFromDB`（时间窗 + 仅入站 + 限额），**messagelog 零改动**；群聊仅检索当前群、私聊仅当前用户（不跨群，隐私边界）
- **关键词门槛**: 最小重叠 2 个二元组（`ragKeywordMinScore`），过滤"什么/怎么"等高频二元组单独命中造成的弱噪声
- **与最近窗口去重**: `context_group_messages` 窗口内（含 ring 热缓存）已有的事件按 EventID 跳过，不重复注入；窗口外更早的历史才是 RAG 的增量价值
- **注入格式**: `===== 相关历史消息 =====`（时间戳+昵称: 内容，单条 200 字截断），附"注意消息时效性"提示；`context_rag_inject_max`（默认 3）上限
- **会话级缓存**: TTL + 查询关键词 Jaccard 复用（含空结果缓存），连续消息零重复检索；embedding 失败自动降级纯关键词
- **测试**: 新增 8 个用例——关键词预筛（含无关过滤）/关闭零开销/无命中空结果/最近窗口去重（窗口内跳过、更早命中保留）/embedding 精排翻转顺序/会话缓存命中零嵌入/嵌入失败降级/注入条数上限

### 🛡️ AI 插件：三项加固（工具权限强制校验 / 计划持久化 / 调用链追踪）

- **工具级权限强制校验**: `Tool` 新增 `Permissions []string`（如 `"bilibili.manage"`）——显式注册工具在 `executeTool` 调用前**强制校验**调用者 RBAC 权限（任一命中即放行；权限管理器缺失时拒绝，安全默认），不再依赖插件自觉实现校验；支持 `resource.action`/`resource:action`/`resource` 三种格式（与框架命令权限同语义）；权限不足返回可读错误、不执行
- **计划跨重启持久化**: `sessionRecord` 新增 `Plan` 列（AutoMigrate 自动加列）——`toRecord`/`toSession` 序列化/还原进行中的任务计划，重启后 `create_plan` 建立的计划与步骤进度继续生效（`planText` 注入链路不变）
- **调用链可观测性**: 每次工具调用记录 `ToolTraceEntry`（时间/工具名/参数摘要 80 字/耗时/错误摘要 120 字，会话内保留最近 50 条）；新增 `/ai trace` 子命令查看（✅/❌ 标记 + 耗时 + 参数 + 错误）——调试 agent 行为不再只靠日志；`/ai status`/`/ai stats` 之外第三个诊断入口
- **实现**: 权限校验在 `execute.go`（`hasToolPermission`/`parseToolPermission`）；计划持久化在 `session.go`/`storage.go`；追踪在 `session.go`（`appendToolTrace`/`ToolTrace`）+ `process.go`（`recordToolTrace`，工具执行前后计时）
- **测试**: 新增 7 个用例——权限解析三格式、无管理器/未授权/错权限拒绝、精确授权/任一命中/无声明放行、计划持久化往返（含步骤状态）、追踪上限淘汰、成功+失败追踪记录（错误细节/耗时/参数摘要）、`/ai trace` 空与有记录路径

### 🤖 AI 插件：后台任务执行（计划自动推进）

- **`plan_auto_continue` 配置**（默认 false）: 开启后创建计划即可放手——机器人按 `plan_auto_interval`（默认 15s）**自动继续执行未完成步骤并主动汇报结果**，无需用户逐条消息推动（"帮我整理一下这周的通知，整理完发我"这类任务可一次完成）
- **安全约束**: 用户发新消息立即重置推进预算（用户永远优先）；用户对话进行中跳过本轮不抢话（`TryLockTurn`）；`plan_auto_rounds`（默认 3）限制单计划自动推进轮次；**无进度自动停止**——连续两轮计划状态无变化即停，防止模型空转刷屏
- **与计划持久化衔接**: 计划本就跨重启持久化（见上文"三项加固"），后台任务在机器人重启后仍可继续

### 🎛️ AI 插件：上下文预算编排（`context_window`）

- **`context_window` 配置**（默认 0 = 不限）: 按模型上下文窗口控制系统提示注入总量——此前群聊窗口/长期记忆/相关历史/运行时上下文各自按上限注入，小上下文模型（本地 8-16K）可能被撑爆；现在核心指令优先保障，其余内容按优先级（运行时上下文 → 群聊窗口 → 长期记忆 → 相关历史）自动缩减，装不下则丢弃

### 🧮 AI 插件：多模型分工与并行工具执行

- **`verify_model` / `extract_model` 配置**（默认空 = 跟随主模型）: 回答校验、记忆抽取可指定独立的轻量模型——主对话用强模型、辅助任务用便宜模型，成本按任务分层
- **`tool_parallel` 配置**（默认 4）: 模型一次请求多个工具时**并发执行**（结果按原始顺序回填）——"同时查天气和查 B 站直播"这类多查询回合的延迟从逐个执行收敛到最慢一个；真实命令路径自动串行化保证安全；并发度 1 退化为原行为

### ⏭️ AI 插件：对话抢占（用户可打断）

- 用户发新消息时自动**请求中断进行中的回合**——长任务/慢工具不再阻塞后续对话，进行中的回合在轮次与工具之间尽快收尾，未开始的轮次/工具直接跳过；新消息随后立即处理（用户侧永远优先）

### 📋 AI 插件：计划执行约束（顺序强制 + 失败重规划）

- **顺序强制**: `update_plan_step` 拒绝跳步——前序步骤未完成（终态）时不能推进后续步骤，模型必须按计划顺序执行
- **失败自动重规划**: 某步骤执行失败后自动追加重规划指令，要求模型调整剩余计划（跳过/替换失败步骤）或明确告知用户无法完成——不再依赖模型自觉决定下一步

### 🔎 AI 插件：记忆检索增强

- **RAG 语义兜底**: 历史消息关键词零命中且 embedding 可用时，自动对最近候选做语义检索——"上次说的那个方案"这类**记不清原话**的查询也能命中（仅零命中才产生 embedding 调用）
- **事实合并更严谨**: 相似事实合并新增长度比例约束（短/长 ≥ 0.5），防止仅共享片段的长短句被错误合并

### 🧪 测试

- 新增 15 个用例——并行执行（并发度实测 + 顺序回填）、中断抢占（轮次前零 LLM 调用 + 信号生命周期）、顺序强制（跳步拒绝/失败后推进）、重规划闭环与去重、多模型分工、预算编排（极小预算只保核心/大预算全节纳入）、后台推进完整链路与无进度停止、RAG 语义兜底、合并长度约束；全量测试与 `-race` 通过

### 🌐 新插件：websearch — 通用网页搜索与网页抓取

- **填补实时信息空白**: 模型知识有截止日期，"今天的热点新闻""某产品最新版本"这类问题此前无工具可用——AI 现在可自动调用 `web_search(query, max_results?)` / `fetch_url(url)` 工具；用户也可直接 `/search <关键词>` / `/fetch <URL>`
- **双搜索引擎**:
  - **SearXNG**（默认）: 自建实例免费无 key；**支持公共实例**——JSON 输出（`format=json`）多被公共实例禁用，插件自动回退 HTML 输出并解析结果块（`<article class="result">`），两种部署开箱即用
  - **Serper**（Google 搜索 API）: `serper_api_key` 配置后使用，含 answerBox 直接答案置顶
- **fetch_url 安全设计**（只读请求，无权限要求）:
  - **SSRF 防护**: 仅 https 公网地址（禁内网/保留地址），重定向逐跳复检
  - 响应大小上限（2MB）+ 正文截断（4000 字）+ 超时
- **正文提取**: `x/net/html` 解析（跳过 script/nav/footer/header 等非正文节点），标题 + 段落合并，畸形 HTML 降级去标签提取
- **基建复用**: 代理配置与 pic/sauce 同语义（共享 Transport）；工具注册为 general 类别恒被选中；AI 工具经 `ListTools` 自动发现
- **测试**: 13 个用例——SearXNG 解析/条数上限/错误状态/**JSON→HTML 自动回退**/HTML 结果块解析（含空链接丢弃与条数上限）、Serper key 校验/请求头与体/answerBox 置顶、正文提取（含导航跳过）、SSRF 四场景（http/内网/私网/重定向内网）、大小与字数截断、降级提取、结果格式化、AI 工具执行与 http 拒绝

### 🗑️ 移除

- 删除分类路由全部代码: `getOrRouteCategory`/`routeToolCategory`/`routeSystemPrompt`/`collectToolCategories`/`filterToolsByCategory`/`toolHasCategory`/`buildCategorySelectTool`/`toolThreshold`/`routeCacheTTL` 及 `Session` 的 `routeCategory` 缓存字段、`select_toolset` 工具常量
- 每次对话少一次路由 LLM 调用（工具选择变为本地计算）

### 🧪 测试

- 新增 `select_test.go`（分词/重叠/Jaccard/预算/必保集/跨域/缓存命中/话题漂移失效/embedding 降级）与 `embedding_test.go`（余弦/OpenAI 端点 mock/惰性缓存/错误状态）；`process_test.go` 新增"工具选择子集"集成用例；删除 9 个旧路由测试用例
- `go vet ./...` 与 `cmd/bot` 独立 module 双平台编译通过；`-race` 通过

### 📝 配置

- `config.example.yaml`: `plugins.ai` 新增 `tool_select_max`/`tool_budget`/`embedding_base_url`/`embedding_api_key`/`embedding_model`/`tool_retry_limit`/`memory_enabled`/`memory_min_interval`/`memory_max_facts`/`memory_inject_max`/`plan_max_steps`/`verify_enabled`/`verify_max_retries`/`context_rag_messages`/`context_rag_days`/`context_rag_candidates`/`context_rag_inject_max`/`context_window`/`verify_model`/`extract_model`/`tool_parallel`/`plan_auto_continue`/`plan_auto_interval`/`plan_auto_rounds` 配置说明；新增 `plugins.websearch` 节（engine/searxng_url/serper_api_key/proxy/timeout/max_results/max_fetch_bytes/max_fetch_runes）

### 🧹 golangci-lint 清理（存量问题）

- 修复 5 处存量 lint 问题：minecraft MOTD 渲染无效赋值（`fontPath`）、pic 标签拼接循环改 `append` 展开（S1011）、sauce 测试 nil context 改 `context.TODO()`（SA1012）、bilibili 未使用类型 `watchState`、updater 未使用方法 `stateStore.save`——均为无行为变化的安全清理，`golangci-lint run ./...` 两模块 0 issues

## v1.38.0 (2026-08-11)

### 🤖 QQ 平台：官方 OpenAPI 全面同步（2026-08 更新）

- **接口域名统一**: `OpenAPIURL` 从 `api.sgroup.qq.com` 改为 `api.bot.qq.com`（官方 20260810 变更记录要求）
- **群聊管理接口（12 个）**: 新增 `GroupManager` 子接口——获取群基本信息、获取机器人群内状态、入群申请列表拉取（cursor/limit 分页）、入群申请审批（approve/decline）、查询群禁言状态、设置群成员禁言，以及入群自动审批策略全量管理（列表/创建/修改/删除/执行/白名单号码）
- **事件补充**: 新增 `GROUP_JOIN_REQUEST`（用户申请加群，含邀请审批 inviteID 编码进 ChatInfo.Tokens）；`MESSAGE_AUDIT_PASS/REJECT`（修正旧 `MESSAGE_AUDIT` 事件名）；`PUBLIC_MESSAGE_DELETE`（公域频道消息删除）；`DIRECT_MESSAGE_DELETE`；论坛事件 8 个（`FORUM_THREAD/POST/REPLY_*`、`FORUM_PUBLISH_AUDIT_RESULT`）；音频事件 4 个（`AUDIO_START/FINISH/ON_MIC/OFF_MIC`）；音视频/直播子频道成员进出事件 2 个
- **机器人接口**: 新增 `BotManager.GenerateURLLink`（生成机器人分享链接，callback_data 透传）
- **消息 DTO**: `Markdown` 新增 `force_verify_image_resource`（图片转存失败即中断发送）；`MessageAudited` 补全 `audit_time`/`seq_in_channel` 字段
- **平台可选接口落地**: `qqSender` 实现 `GroupManager.BanMember`（群禁言）、`InvitationHandler`（入群申请审批）、`GroupInfoProvider.GetGroupInfo`（群信息查询）——群聊管理能力可经框架统一入口使用

### 🧠 AI 插件：QQ Agent 能力对齐（官方 OpenClaw 插件）

- **语音转写（STT）**: 平台附带的官方 ASR 文本（QQ `asr_refer_text`）自动注入对话为 `[语音转写] xxx`——任何模型都能理解语音，无需下载音频/依赖多模态音频能力（无 ASR 时回退原音频直传）
- **打字指示器（TypingIndicator）**: `qqSender` 实现 `platform.TypingNotifier`（C2C 用 `input_notify` msg_type=6），修复 `Capabilities.TypingIndicator: true` 声明与实现不一致；AI 在 LLM 处理前调用 `ctx.TrySendTyping()` 给用户即时反馈（平台不支持自动降级）
- **大文件分片上传**: `sendAttachment` 对 >5MB 的本地附件自动走官方四步分片流程（`upload_prepare` → 分片 PUT → `upload_part_finish` → 合并），对齐官方 100MB 大文件能力（此前分片接口仅存在于 openapi 层、发送链路未接线）
- **群聊自主发言（`group_autonomous`）**: 新配置，默认 false；开启后不 @ 机器人也响应群内非命令消息（等价 OpenClaw `requireMention=false`，需平台授予群全量消息权限），命令消息仍排除避免与命令 handler 竞态
- **对话式定时提醒（`/ai remind`）**: `/ai remind <时长> <内容>` / `remind list` / `remind cancel`；时长支持 `30秒/5分钟/1小时/2天` 与 `30s/5m/1h/2d/纯数字`；到期经 `SessionNotifier`（QQ 已实现）主动推送；插件 Teardown 自动停止全部提醒

### 🔓 QQ 平台：移除客户端被动回复限制

- **根因**: 官方文档虽写"群聊 5 分钟/5 次、C2C 60 分钟/4 次"，但实测平台限制已放宽（agent 场景远超文档值）；官方 SDK（botgo）与 OpenClaw 插件均**不做客户端拦截**，限制完全由服务端校验（错误码 40034128"被动回复时间或次数超限"）
- **修复**: 移除 `checkReplyLimit` 发送前硬拦截（5 次/5 分钟）及 `maxPassiveReplies` 常量、`msgSeqEntry.count` 计数、死代码 `resolveMsgID`；保留 `msg_seq` 递增防重放（相同 `msg_id+msg_seq` 组合会被平台去重）；`passiveReplyTTL` 调整为 60 分钟（C2C 最长有效期）仅作 `sweepExpired` 内存清理基准，防止有效期内误清导致 seq 重置撞上平台去重；`errutil` 两个废弃错误标记 deprecated（保留定义兼容）
- **验证**: 新增同 msg_id 连续 20 次回复不拦截测试、sweep 回收后 seq 重置测试

### 📝 配置与测试

- `config.example.yaml`: `plugins.ai` 新增 `group_autonomous` 配置说明
- 新增测试: AI remind 时长/参数解析、fireReminder 群/私聊/容错、附件 ASR 优先注入、分片上传辅助函数（md5/PUT/截断）、`AttachmentTranscript` 全分支、`nextMsgSeq` 多回复/回收

### 🔐 AI 插件：命令执行审批（Command Execution Approval）

- **`tool_approval` 配置**（off 默认 / restricted / always）: 对齐官方 OpenClaw 插件的命令执行审批——
  - `restricted`: 仅审批标记 `RequiresApproval=true` 的工具（`Tool` 结构体新增字段，显式注册的敏感工具建议标记）
  - `always`: 审批所有工具调用
- **审批交互双通道**:
  - **按钮**: 平台支持回调按钮（CapButtons）时发送"✅ 允许 / ❌ 拒绝"按钮，经 `EventKindInteraction` 回调处理（ID 形如 `ai:approve:A1`）
  - **文本命令**: 任何平台可用 `/ai approve <ID>` / `/ai deny <ID>`（含"批准/拒绝/允许/驳回"自然语言），可靠兜底
- **安全约束**: 只有**发起该工具调用的用户**能审批（防他人代批）；审批等待超时（`approval_timeout`，默认 60s）按拒绝处理，不中断对话（拒绝时返回工具级结果）

### 📐 AI 插件：per-group 工具策略/提示词（Group Policy）

- **`/ai group` 管理命令**（群管理员权限；全局默认需 superadmin）:
  - `group status` — 查看本群生效配置
  - `group set prompt <text>` — 设置群提示词（覆盖全局 system_prompt）
  - `group set tools all|none|工具名,工具名` — 设置群工具白名单
  - `group set approval off|restricted|always` — 设置群审批模式（覆盖全局 tool_approval）
  - `group set mention on|off` — 设置群 @ 触发要求（on=必须 @，off=自主发言）
  - `group reset [字段|all]` / `group global status|reset` — 重置群/全局配置
- **存储**: LevelDB `data/ai` 持久化（仿 welcome 插件，`__global__` 全局回退链）
- **运行时生效**: 群提示词覆盖 `buildSystemPrompt`、群工具白名单过滤 `processWithTools`、群审批模式覆盖 `effectiveApprovalMode`、群 @ 触发要求门控 `handleAI` 与兜底 matcher（全局 GroupAutonomous 下按群过滤）
- **测试**: 新增 24+ 用例（审批 ID/文本解析、管理器生命周期、模式判定、审批全链路、超时拒绝；群策略回退/持久化/JSON/工具过滤/mention 门控/管理员权限）

## v1.37.2 (2026-08-08)

### 🐛 AI 插件：群聊消息窗口默认开启（修复 AI 无法读取群聊历史）

- **根因**: `context_group_messages`（把同群最近 N 条入站消息注入系统提示，让 AI 感知多人对话）默认值为 `0`（关闭）——未显式配置时 AI 永远看不到群聊历史，只会回答"只能看到当前这一条消息"。实测 QQ 官方平台群聊复现
- **修复**: 默认值 `0 → 10`——AI 默认即可读取同群最近 10 条入站消息（`昵称: 内容`，旧到新）；显式配置 `context_group_messages: N` 可调整条数，`0` 仍可关闭（显式配置行为与旧版完全一致）

### 🐛 messagelog：QueryGroupRecent SQLite 兜底（重启后群历史仍可读）

- **根因**: `QueryGroup` 只读内存 ring buffer——机器人重启后热缓存为空，重启前的消息只持久化在 SQLite、不会被加载回内存。实测：重启后 AI 只能读到重启后新产生的消息，重启前的群聊历史不可见（非时间范围限制）
- **修复**: 新增 `messagelog.QueryGroupRecent`——内存 ring 条数不足时自动以 SQLite 最近记录补齐（按 EventID 去重、排除出站消息、旧→新排序）；AI `buildGroupContext` 改用它，重启后仍能读到重启前的群聊历史
- **验证**: 新增 `QueryGroupRecent` DB 兜底/去重/顺序测试

### 🧠 AI 插件：群聊窗口包含机器人回复开关（context_group_include_bot）

- **`context_group_include_bot`**（默认 false）: 群聊消息窗口是否包含机器人的出站回复——AI 自己的回复与其他插件的回复均进入窗口（`messagelog.QueryGroupRecentWithBot`，入站/出站 ring 与 SQLite 合并、时间排序、去重）
- **身份标注与归属提示**: 出站消息以**机器人自身名称**（`ctx.GetBotName()`，未注入时兜底"机器人"）标注，且窗口顶部附加说明行"标注「名称」的消息由本机器人账号发出——AI 对话回复或插件命令输出，均为自己/本账号的发言"，消除 AI 把自身回复误认为其他账号发言的歧义
- **会话历史防重复**: AI 自己的回复已以 assistant 轮次存在于会话历史，开启开关后 `buildSystemPrompt` 收集会话内 AI 已回复的内容集合，`buildGroupContext` 按内容精确匹配跳过同一条回复——窗口与对话历史不会重复注入同一文本；其他插件的回复不进会话、不受影响
- **验证**: `QueryGroupRecentWithBot` 混合/排序/截断测试 + `buildGroupContext` 含机器人/名称标注/归属提示/去重测试

### 📝 配置

- **config.example.yaml**: `plugins.ai` 节补全全部 33 个配置项（提供商与模型/模型参数/触发方式/输出与多模态/工具白名单/技能系统/隐私控制/结构级上下文），逐项标注 `[R]` 标记、默认值与取值范围

## v1.37.1 (2026-08-07)

### 🐛 sauce 插件修复

- **`-engine` 参数失效修复**: 框架解析器只识别 `--engine`（双横线）与单字符短标志，单横线多字符 `-engine` 落入 Positional 被静默忽略（实际跑全部引擎）。改为手动从 Positional 解析（与 pic 的 `-site` 同策略），并支持 `-engine=value` 等号形式
- **`-e` 短标志**: `sauceDef` Flag 声明 shortName `e`，增强解析器原生识别 `-e iqdb`；配合手动解析，`-e` / `-engine` / `--engine` 三种形式均可指定引擎
- **检索超时隔离 handler 生命周期**: `runSearch` 的 reqCtx 改从 `context.Background()` 派生，仅受 `search_timeout` 控制——中间件注入的短 deadline 不再让 `waitIQDBOutcome` 在宽限期前被 `reqCtx.Done()` 提前触发，避免 `IQDB: context deadline exceeded` 混入主结果而非走排队补发
- **IQDB 错误提示友好化**: 请求阶段超时（连接/上传被卡，实测 iqdb.org 高峰期 POST 阶段即超时）提示"IQDB 无法访问（可能排队过长或网络异常）"；补发路径超时类错误提示"排队过久或暂时不可用"，不再透传裸 `context deadline exceeded`

### ♻️ go fix 现代化重构

- `cmd/bot` 全插件应用 `go fix`（range-over-int `SplitSeq`/`range`、`slices.Contains`、`max()` 等）与 `gofmt` 格式化：anime/css/fortune/genshin/iss/pic/rpg/starrail/sauce/updater/weather/setup.go 等 30 个文件

## v1.37.0 (2026-08-07)

### 🔎 sauce 插件 v2.1：多引擎重构 + 输入增强 + IQDB 排队处理

- **移除 ASCII2D 引擎**: 一直被 Cloudflare 拦截（实测 403 挑战页），彻底删除（含解析/测试文件与配置项）
- **新增 AnimeTrace 引擎**: 免费无 key 的动画/Galgame 角色识别（`POST api.animetrace.com/v1/search`），与 trace.moe 的逐帧找番互补；业务状态码映射可读错误（17701 过大 / 17702 繁忙 / 17704 维护等）
- **`-engine` 参数**: `/sauce -engine saucenao,tracemoe` 指定引擎集合（`all` = 全部，默认）；AI 工具保持默认全引擎
- **代理配置**: `plugins.sauce.proxy` 统一覆盖全部引擎与图片下载（共享 Transport，各引擎独立超时）
- **输入增强（三种方式）**:
  - 引用消息图片：reply 段 `Extra["raw_quote"]`/`parallel_message` 提取被引用图片（QQ 等平台）
  - 等待补图：`/sauce` 无图时注册一次性临时 Matcher 等待（`image_wait_timeout` 默认 60s），收到图片即检索、**非图片消息即取消**、超时提示取消
  - 原有"图片 + 标题附带命令"保持不变
- **IQDB 排队处理**（真实实测：高峰期队列 1300+，单次请求可达 60s+）:
  - `iqdb_timeout`（默认 45s）/ `iqdb_retries`（默认 1，排队/断流时重试拿新队列位置）
  - **流式快速失败**: 排队页头部即回传 `queue('N','0')` 标记，检测到长排队（位置 > 20）毫秒级中止，不再挂满超时
  - **中途断流可重试**: 读取错误包装为可重试错误（等价浏览器刷新重新提交表单，服务端队列任务不因连接断开取消）
  - **分批发送**: 其他引擎结果先回并提示"IQDB 排队中"（`iqdb_grace` 默认 10s 宽限期），IQDB 完成后补发一条（成功/失败/超时均覆盖）
- **配置整理**: getter 移入 `config.go`；`search_timeout`（默认 90s）控制检索总预算；`report_errors` 上报引擎失败

### 🖼️ pic 插件：随机图片年份过滤

- **`recent_days` 配置**（默认 730 天 ≈ 近两年，0 = 不过滤）: 随机图片默认取近两年内上传，避免总是抽到老图
- **`/pic -recent <N>` 参数**: 单次命令覆盖年份窗口（`0`/`all` = 不过滤）；AI 工具 `get_random_image` 同步支持 `recent` 参数
- **双协议实现**（官方文档核实 + 实测）:
  - konachan / yande.re（Moebooru）：服务端 `date:YYYY-MM-DD..` meta-tag 过滤（实测 2020/2024 窗口准确生效）
  - safebooru / gelbooru / rule34（Gelbooru 系）：官方 cheatsheet 无 date meta-tag，按 `change` 字段**客户端过滤**——随机池放大 3 倍 + 累积补充至凑够张数；站点不提供时间字段时自动降级不过滤（避免永久空结果）

### 📝 配置与文档

- `config.example.yaml`: sauce 节（proxy/iqdb_timeout/iqdb_retries/iqdb_grace/enable_animetrace/search_timeout/image_wait_timeout）与 pic 节（recent_days）新增配置及注释
- `docs/06-plugins/APP_PLUGINS.md`: sauce 三输入方式、`-engine` 参数、IQDB 排队提示；pic `-recent` 参数与时间过滤说明

## v1.36.0 (2026-08-06)

### 🖼️ bilibili 插件重构增强：WBI 签名修复 + 视频/番剧/开播订阅

- **WBI 签名两处致命 bug 修复**（实测复现）:
  - **混合密钥算法错误**: 旧实现 `sub_key[:4] + img_key[:4]` 已被 B 站废弃，`wbi/acc/info` 返回 `-403 访问权限不足`；改为现行算法——取 `img_key+sub_key` 拼接串按官方 `mixinKeyEncTab` 索引表取 32 位
  - **签名串尾随 `&`**: 参数拼接循环每个 key 后追加 `&`，签名串结尾多一个 `&` 导致 w_rid 无效；改为参数间以 `&` 连接（首项不带前缀）
  - 两处修复均用真实 SESSDATA 实测验证：`FetchUserInfo` 正常返回「陈哥1」粉丝 57 万
- **SESSDATA 编码修复**: 含逗号/星号等特殊字符的 SESSDATA 必须 URL 编码后发送（浏览器标准形式），否则被 -352 风控拦截；已编码值（含 `%`）不重复编码
- **直播字段解析修复**: `LiveInfo` JSON 字段名与真实 API（camelCase `liveStatus`/`online`/`roomStatus`）不匹配，导致直播状态、观看人数永远读不到、恒显示"未开播"
- **新增 `FetchLiveInfoByRoom`**: 按房间号查询（`get_info` 接口为 snake_case，独立解析结构），`/bili live room:<房间号>` 消除 UID/房间号歧义，纯数字输入自动回退
- **`/bili video <BV号>`**: 播放/弹幕/点赞/硬币/收藏/分享、时长、发布日、封面（`view` 接口）
- **`/bili videos <uid/用户名>`**: 接线原有 `FetchVideos`，修复 `wbi/arc/search` 带 Origin 头被 412 风控问题（该接口对 Origin 检查更严格，须移除）
- **`/bili bangumi <关键词>`**: 番剧/影视搜索，修正 `areas`（字符串）/`media_score`（对象）/`pubtime`（unix 时间戳）三处字段类型
- **开播订阅通知**: `/bili watch <uid>` / `unwatch` / `watch list`，持久化到 `data/bilibili/watch.json`，后台轮询（`watch_interval`，下限 30s）检测开播并主动推送群通知
  - **notifier 持久化**: 新增 `bilibili.WithPlatformRegistry` 注入 `platform.Registry`，Setup 阶段即注册推送能力——**重启后订阅通知自动恢复，无需任何命令触发**；未注入时回退事件上下文方式
- **代理支持**: `plugins.bilibili.proxy`（API + 封面图下载），海外/云服务器部署（数据中心 IP 风控严格）推荐配置
- **AI 工具**: 新增 `get_bilibili_video_info` / `get_bilibili_user_videos` / `search_bilibili_bangumi`，共 7 个工具
- **测试**: watchManager 增删/状态转换/持久化/notifier 4 项 + notifier 注册 3 项集成测试

### 🖼️ anime 插件：代理与 API 镜像支持

- **`plugins.anime.proxy`**: 配置代理（如 `http://127.0.0.1:7890`），空值沿用环境变量代理或直连；API 与封面图共享 transport
- **`plugins.anime.api_base`**: 可更换 Bangumi API 基础 URL 为镜像/代理地址
- **Cloudflare DNS 保留说明**: 自定义 Resolver 仅直连时生效，代理模式下域名解析由代理端完成（注释澄清）

### 📤 QQ 平台：SessionNotifier 主动推送支持

- `qqSender` 实现 `platform.SessionNotifier`（`NotifyUser`/`NotifyGroup`）——QQ 开放平台 API 本身支持主动消息，此前未实现该接口导致 bilibili 开播通知等主动推送在 QQ 平台不可用

### 🖼️ minecraft 插件：卡片渲染重设计 + 边界修复

- **卡片重设计**: MC 风格渐变背景；favicon 圆角缩略图 + 服务器地址头部；状态徽章行（● 在线/Java·Bedrock/延迟）；玩家进度条（<55% 绿、55-85% 金、>85% 红）收窄居中不再分割画面；版本/协议标签行；底部查询时间戳；MOTD 深色阴影提升可读性；离线卡片统一 600 宽同款渐变布局
- **MOTD 超长修复**: 图片宽度钳制到画布宽度，超长段按字符级二分截断（兼容 CJK），修复"首段超长整行空白 + 图片被撑宽"
- **玩家名截断**: 异常 API 数据（超 16 字符名）截断处理，防止 badge 过宽
- **压力验证**: 200+ 玩家、50 个 100 字符名、300 字符单段 MOTD 均正常渲染，宽度恒 600 不溢出

### 📝 配置

- `config.example.yaml`: 新增 anime（api_base/proxy）与 bilibili（sessdata/proxy/watch_interval）配置节及注释

## v1.35.0 (2026-08-06)

### 🎯 Event 接口收窄：Content/Attachments 移出接口，正文从段统一派生

- **Event 最小化**: `Event` 从「EventIdentity + EventBody + 4 方法」收窄为「EventIdentity + Segments/Sender/Chat/Timestamp」；`EventBody` 接口删除（0 外部引用）
- **帮助函数**: 新增 `platform.Content(e)`（段拼接 + 首尾空白裁剪）与 `platform.Attachments(e)`（段派生附件）——正文/附件只能从段派生，**平台实现不可能再与段不一致**（强制单一真相源）
- **TrimSpace 语义统一**: Content 统一裁剪首尾空白（QQ/Satori 既有行为，QQ 群消息 @ 占位符前有平台分隔空格）；onebot 等平台正文尾随空白同步收敛，跨平台行为一致
- **平台实现精简**: 删除 8 个事件类型共 16 个 Content/Attachments 方法（onebot/milky/qq/satori/discord/telegram/terminal/synthetic）
- **调用方迁移**: `ctx.GetMessageContent()` 热路径内部改调 `platform.Content`（103 处调用零改动）；ai/about/messagelog/helper/examples 同步
- **MentionsEvent 文档修正**: "不含机器人自身" → 包含被 @ 的机器人自身（IsSelf=true 标记）
- **破坏性变更**: Event 接口移除 Content()/Attachments() 方法

### 🐛 AI 回复上下文对 QQ 引用消息生效（段兜底）

- **根因**: QQ 引用消息（message_type=103）的回复标识是 `ref_msg_idx`（`REFIDX_`/`TMP_` 前缀，平台内部引用标识），与 messagelog 存储的事件 ID（`ROBOT1.0_...`）不对应——`include_reply_context` 在 QQ 上始终查不到被回复消息（其余平台回复 ID 均为真实消息 ID，正常命中）
- **修复**: `prependReplyContext` 在 messagelog 未命中时走段兜底——解析 reply 段 `Extra["parallel_message"]`（v1.34.0 起事件解析时保留的被引用消息并行视图，`msg_nodes[0].content` 含完整文本）注入被回复内容（发送者标注"对方"）；富媒体引用（占位文本 `"[图片] "`）净化后与空文本同样跳过
- **验证**: 新增 QQ 兜底路径与 `replyQuoteFromSegments`（文本/媒体/无 parallel_message/无 reply 段）测试

### 🐛 messagelog 回复 ID 记录改走段真相源

- `eventToEntry` 原先直接断言 `ReplyEvent` 接口取回复 ID——绕过段模型（回复单一真相源）；统一改调 `platform.GetReplyToID`（段优先、接口兜底），与 AI 回复上下文修复同类问题一并收敛

## v1.34.0 (2026-08-06)

### 🚀 统一消息段模型：`Segments()` 成为跨平台消息唯一真相源

- **新增 `platform.Segment` / `SegmentType`（12 种）**: `Event.Segments()` 保序输出原子消息段（text/at/mention_all/image/audio/video/file/face/reply/forward/button/unknown）；`Content()`/`Attachments()` 变为派生视图（`SegmentsContent`/`SegmentsAttachments`），调用方零改动
- **at 统一剥离**: `@机器人 /ping` 在全部平台 `Content()` 统一为 `/ping`——修复 onebot/satori/discord 现状把 `@id` 混入正文导致 `OnCommand` 不匹配的隐性问题（行为修正）；@ 保真由段承担（含分散 at 交错、相邻 at）
- **聚合视图**: `SegmentsMentions(segs, botID)` 保序去重、含 @ 机器人自身（IsSelf=true）；`Reply` 单一真相源：首个 `SegmentReply` 为 canonical，平台 `ReplyToID()` 委托段查找

### ✨ 六平台段映射（入站 + 出站对称）

- **入站**: onebot 22 种 CQ 段全映射、milky 13 种段保序输出、satori 元素树输出段（quote 前置）、telegram 按 UTF-16 实体切分文本/at 交错、qq `<@openid>` 占位符内联交错 + 引用消息解析、discord `<@id>` 占位符位置还原
- **出站**: `OutboundMessage.Segments` 段优先路径，六平台 Sender 按段保序发送（onebot CQ 链 / milky 消息段 / satori XML / telegram 文本+reply / qq 内联 AT 标签 / discord `<@id>` 内联），便捷字段路径保留为降级

### ✨ 跨平台转发辅助 `MessageToOutbound`

- 段 → 段直转保留交错位置；`WithTargetPlatform` 同平台透传（reply/face/forward/unknown 还原）、跨平台保守降级（reply/unknown 剥离、face 降 text、forward 摘要）；新增 `CapForward` 能力标志

### ♻️ milky ChatInfo.ID 语义对齐

- `ChatInfo.ID` 改为平台原生纯数字（`"group:123"` → `"123"`），场景由 `IsGroup`/`IsDM`（temp 置 IsDM）表达，原始 scene 保留在 `Tokens[milky_message_scene]`；`Delete` 双尝试（先群撤回、失败再私聊撤回）；`decodeChatID`/`parseUin` 保留旧格式缓存兼容

### 🐛 qq 富文本解析按真实报文校准（v1.33.1 实测）

- **引用消息（message_type=103）**: 引用目标 ID 实测在外层 `message_scene.ext` 的 `ref_msg_idx=`（REFIDX_/TMP_ 前缀）而非 msg_elements；`parallel_message.msg_nodes` 为被引用消息并行视图；C2C 引用消息补齐解析
- **表情内联标记**: `<faceType=1,faceId="9",ext="...">`（ext 为 base64 的 `{"text":"显示文本"}`）→ `SegmentFace`，不再泄漏进 Content
- **结构化卡片（message_type=3）**: `ark_data` → `SegmentUnknown` + 原始 JSON 保留（群/C2C）

### 🐛 OnMentionedBot 六平台统一生效

- onebot/milky/telegram/discord 事件解析注入机器人自身 ID（onebot 用事件 `self_id`，其余用适配器 BotID），`@机器人` 在全部平台可触发 `OnMentionedBot` / `OnMentionedBotOrNoMentions`（此前仅 qq/satori 有效）

## v1.33.1 (2026-08-04)

### 🐛 fortune：`/tarot` 处理中不可达代码修复（go vet 检出）

- **修复**: `handleTarot` 渲染卡片失败分支中 `return nil` 后的不可达 `continue`（`go vet` unreachable code 告警）
- **背景**: CI 新增 `cmd/bot` 独立 module 的 `go vet` 检查——该 module 此前从未被静态检查覆盖，此问题随之暴露；属静态检查级修复，运行时行为无变化
- **验证**: `cmd/bot` 全量 build/vet/test 通过（含 updater Windows 路径测试）

## v1.33.0 (2026-08-04)

### ✨ about：`/about` 展示增强（运行状态 + 系统资源）

- **新增「运行状态」小节**: 机器人名称（`ctx.GetBotName()`）、当前平台、注册命令数、Matcher 数（经 `Info.Coordinator()` 只读视图，`p.info` 为 nil 或 Coordinator 缺失时自动跳过，兼容测试/无管理上下文场景）
- **新增「系统信息」小节**: 操作系统/架构（`GOOS/GOARCH`）、CPU 核心数、Goroutine 数，以及两类内存指标：
  - **系统内存**: 宿主机当前占用 / 总量（含占用百分比），数据源 `gopsutil mem.VirtualMemory()`（`middleware/degradation` 同源依赖，无新增依赖）
  - **进程内存**: 改用 `runtime.MemStats.Sys`（进程从 OS 持有的总内存，含保留页），并补充占系统内存百分比——相较 `Alloc`（仅当前堆存活字节）更符合普通用户对"进程占用"的理解
- **字节格式化**: 新增 `formatBytes`（1024 进制自动选 B/KB/MB/GB），避免大内存显示为冗长的 MB 数值
- **容错**: 宿主内存获取失败（平台不支持/无权限）时系统内存显示占位符，进程内存仅显示数值不加百分比
- **版本**: about 插件 1.0.0 → 1.1.0，更新包注释与元数据 HelpText

## v1.32.4 (2026-08-04)

### 🐛 updater：更新后新进程启动撞 LevelDB 锁修复（等待完整终止 + 确定性释放数据文件）

- **根因（实测复现）**: 子进程的等待用 `GetExitCodeProcess` 轮询——进程退出代码一可见即返回，但内核此时可能还没关完父进程的文件句柄；子进程"等待 0s"就继续启动并打开共享 LevelDB（antispam/stats 等），报 `The process cannot access the file because it is being used by another process`。E2E 对照：子进程 0s 继续 → 父进程 3 秒后才退出 → DB 打开失败
- **等待完整终止**: `waitProcessExit` 改用 `WaitForSingleObject`（`SYNCHRONIZE` 权限）等待进程**完整终止**，不再轮询退出代码；原 500ms 宽限降级为异常退出（崩溃/未接 hook）时的兜底保险
- **确定性释放数据文件**: 新增 `updater.SetShutdownHook`，cmd/bot 注入 `pm.StopAll`——Windows 退出前先跑全部插件 Teardown（确定性关闭 LevelDB 句柄）再 `os.Exit(0)`，子进程打开数据文件零时序依赖（与 Unix SIGTERM 优雅关闭路径等价；Windows 无法自发 SIGTERM，故进程内直接调用）。不用 `bot.Shutdown()`：从更新 handler 内部触发会等 engine 30s 超时
- **等待诊断带时间戳**: 子进程"等待旧进程退出"日志现在带毫秒时间戳（`T=... 等待 528ms`），异常时一眼可查；`OpenProcess` 失败也记录原因
- **多实例体检**: 更新前检测同目录其他 remilia 实例并警告（多实例共用 data 目录同样会撞 LevelDB 锁）
- **验证**: 全量 E2E（mock GitHub + Windows）：子进程等待 ~0.5s，42 插件全部注册成功，重启总耗时 <1s；测试全绿，双平台编译通过

## v1.32.3 (2026-08-03)

### 🐛 updater：撤回"继承父进程控制台"方案（实测会杀死子进程），新增 `"file"` 模式

- **实测教训（win + pwsh）**: v1.32.2 默认的"把父进程 stdout/stderr 句柄传给子进程"方案在 Windows 上是致命的——`DETACHED_PROCESS` 子进程持有父进程控制台句柄会被系统视为与该控制台关联，父进程退出时子进程被连带终止（`Get-Process` 找不到、无任何输出）。对照组：v1.32.1（NUL 输出）与 `child_console: "new"`（独立控制台窗口）子进程均存活；唯一差别就是控制台句柄继承
- **默认 `""` 恢复为安全路径**: 不再向子进程传任何句柄（标准输出接 NUL），保证更新后子进程必然存活；日志靠 `log.file` 落盘。配置文件注释中明确警告不要采用"继承父进程控制台"的做法
- **新增 `child_console: "file"`**: 子进程 stdout/stderr 重定向到 `data/updater/child.log`——无窗口、无控制台依赖，服务化场景友好（存活 + 输出落盘）
- **回滚路径 `restartCurrent` 同步遵守策略**（通过环境变量传递），同样不再传父进程控制台句柄
- **建议**: pwsh/终端启动推荐 `child_console: "new"`（唯一"日志可见且子进程存活"的方案）；`""` 默认值安全但子进程终端输出不可见

## v1.32.2 (2026-08-03)

### 🐛 updater：更新后新进程日志不可见修复 + 独立控制台支持

- **新进程日志不可见修复**: Go 的 `os/exec` 在 `Stdout/Stderr` 为 nil 时会把子进程标准输出接到 NUL（而非继承父进程控制台），更新后新进程"活着但看不到任何日志"。拉起子进程时现显式传递父进程的 stdout/stderr 句柄——新进程日志继续出现在启动它的终端里，父进程退出后仍持续输出（实测验证）
- **新增 `plugins.updater.child_console: "new"` 配置**: Windows 上为子进程创建独立控制台窗口（`CREATE_NEW_CONSOLE`），子进程启动早期（`HandlePendingUpdate`，早于 logger 初始化）把 stdin/stdout/stderr 重绑定到 `CONIN$`/`CONOUT$`，日志自成一窗；默认 `""` 为继承模式。Unix 无控制台概念，配置不生效
- **修复 v1.32.1 回退路径隐藏 bug**: Job Object 不允许脱离时，回退逻辑用 `*cmd` 复制已启动的 `exec.Cmd` 二次 `Start()`，但 `startCalled` 标志不可重置，必然报 `"exec: already started"`——处于此类 Job 的环境更新会错误触发"启动新进程失败"并回滚。现重建全新命令回退，并新增 Windows 回归测试（在真实 `KILL_ON_JOB_CLOSE` Job 中验证回退成功）
- **验证**: 全量测试通过（含 `-race`），Windows/Linux 双平台编译通过；新增 `restart_test.go`（控制台策略优先级）与 `process_windows_test.go`（Job 回退回归）

## v1.32.1 (2026-08-03)

### 🐛 updater：Windows Job Object 杀死重启子进程修复

- **Windows 下更新后"新进程没起来"修复**: 某些启动器/终端/进程守护会把进程放进带 `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` 的 Job Object，父进程（旧进程）退出瞬间 Job 关闭会连带杀死刚拉起的子进程——实测复现为"父进程关闭了但子进程没有起来"，且子进程无任何输出（分离进程、无控制台）。拉起新进程时现携带 `CREATE_BREAKAWAY_FROM_JOB` 尝试脱离父进程所在 Job；Job 不允许脱离（未置 BREAKAWAY_OK）时回退普通分离启动（行为不劣化）
- **覆盖两条拉起路径**: `/update now` 与自动更新的 `spawnNewProcess`、回滚路径的 `restartCurrent` 统一走 `startDetachedChild`（`process_windows.go` / `process_unix.go`）
- **验证**: Windows/Linux 双平台编译通过，updater 测试全绿；`KILL_ON_JOB_CLOSE + BREAKAWAY_OK` 环境实测子进程在父进程退出后继续存活

## v1.32.0 (2026-08-03)

### 🧭 核心引擎：RoutingStrategy 路由规划抽象（路由与执行分离）

- **路由/执行分离**: `processEventMatchers` 只负责执行；索引组织方式抽象为 `RoutingStrategy → CandidatePlan → MatcherIndex` 三层稳定边界（docs/notes/25-routing-strategy.md）
- **可插拔 MatcherIndex**: permanent / command / temp / regex 四个内置索引成为插件点，第三方可通过 `WithMatcherIndex` 注册自定义索引（HashMap/Trie/DFA…），执行主循环零改动
- **K 路归并**: 固定 6 路归并泛化为 K 路（上限 16），Source Budget 治理——框架内部超限 panic（框架 Bug）、第三方超限 warn 后继续
- **快慢带 + 惰性执行**: BandFast（permanent/command/temp）急切构建，BandSlow（regex）惰性执行——fast 被 block 短路时慢带索引零查询，正则免费跳过
- **候选 Meta**: regexIndex 预匹配携带捕获组，handler 经 `ctx.RegexResult()` 读取捕获组，无需重新执行正则（`matcher.Regex()` 注册，Match 跳过 Rules[0]）
- **热路径优化**: 单归并迭代器 + 内置索引直引 + 池化零化裁剪——路由层 Empty 142→94 ns，重负载持平，全程 0 allocs

### ⚡ 注册批处理会话 RegisterBatch（顺序注册写放大修复）

- 插件 Setup 等启动期集中注册从每次 COW 全量复制收敛为一次提交：1000 个命令顺序注册 786ms → 1.6ms（约 480 倍）
- `Engine.BeginRegisterBatch()` / `RegisterBatch.Flush()`；插件 Manager 在 Setup 周围自动开启（含 StartAll / UnloadLoad / 蓝绿重载路径）
- 并发加载重叠（如不同插件并发 Reload）时自动退化为逐条注册——功能正确，仅失去批量收益
- 附带修复: Handle() 首次绑定跳过命令 matcher 的无谓索引重建（顺序注册写放大主因之一）

## v1.31.0 (2026-08-03)

### ✨ 新插件：updater — 自动更新（应用级）

- **`/update` 命令族**（superadmin 角色或 `updater.manage` 权限）:
  - `/update check` — 检查 GitHub Releases 是否有新版本
  - `/update status` — 查看版本/更新源/上次检查/备份/容器环境
  - `/update now [--force]` — 立即下载 → 校验 → 替换 → 重启（`--force` 重装同版本）
  - `/update auto on|off` — 切换后台自动检查（默认开启，仅检查不自动应用）
  - `/update rollback` — 回滚到上一个备份版本并重启
- **发布源**: 默认 `KomeiDiSanXian/remilia` 的 GitHub Releases，资产名按 goreleaser 命名约定匹配当前平台（Linux/Darwin tar.gz、Windows zip，含 armv6/v7/riscv64 等全矩阵）
- **安全设计**:
  - 下载资产必须通过 goreleaser `checksums.txt` 的 sha256 校验，缺失即中止
  - HTTPS 强制（含重定向最终 URL 复核）；归档解压防路径穿越；资产大小上限
  - 先写更新标记、后替换二进制——任何失败路径要么二进制未动、要么标记在场可自愈
- **原子替换与回滚**:
  - 替换前备份旧二进制（`remilia.old.<版本>`），跨平台两步改名（Windows 运行中 exe 可改名不可覆盖）
  - 新进程启动时校验版本（`HandlePendingUpdate`，main 最早期）：一致 → 确认成功并清理残留；不一致 → 自动回滚旧备份并重新执行
  - 覆盖"替换后、拉起前崩溃"窗口：无环境变量时按默认数据目录探测标记
  - 拉起新进程失败 → 自动回滚；备份不可用（未启用/未到替换阶段）→ 清除标记继续启动，不阻塞
- **进程切换**: 新进程以分离会话启动并等待旧进程退出（Unix kill 探测 / Windows 进程句柄），避免端口冲突；旧进程通过自 SIGTERM 走既有优雅关闭路径（Windows 直接退出，注释说明）
- **运行环境**:
  - 容器（`/.dockerenv`）自动禁用自更新，提示 `docker pull` 拉取新镜像（可配置关闭）
  - 代理配置（与 pic 相同语义）适配 GitHub 不可直连环境
  - `check_interval` 低于 10 分钟自动钳制（GitHub 匿名 API 限流 60 次/小时）
- **配置**: `plugins.updater` 节（`repo`/`check_interval`/`auto_apply`/`backup`/`allow_prerelease`/`disable_in_container`/`proxy`/`timeout`），`[H]` 字段热读，`[R]` 字段需重启
- **放置位置**: `cmd/bot/plugins/updater/`（应用级，框架核心零改动）；`main.go` 最早期接入 `HandlePendingUpdate`
- **测试**: 30 个单元/集成测试（semver 比较、GitHub API mock、checksums 校验、tar/zip 解压与路径穿越、原子替换/回滚、全链路 mock 服务器、崩溃窗口兜底、`-race` 并发验证），Windows/Linux 双平台编译通过

## v1.30.0 (2026-08-02)

### 🖼️ pic：内容分级重构为区间模型 + gelbooru 迁移适配

- **修复 gelbooru 不随机**: gelbooru.com 已迁移至 Danbooru 式 4 档分级（general/sensitive/questionable/explicit），旧标签 `rating:safe` 仅剩 4 张 2008 年遗留图，导致 `/pic -site gelbooru` 永远返回同一批图。现已按站点映射为 `rating:general`，实测每次返回全新随机结果
- **rating 改为精确档位/区间模型**: 原"内容分级上限"语义（含下限档位内容）改为精确档位或区间——
  - 单档: `rating: "safe"` 只发安全级；`rating: "explicit"` 只发露骨级
  - 区间: `rating: "safe..questionable"` 发 safe+sensitive+questionable（不含 explicit）；`questionable..explicit` 不含安全图
  - `"all"` 全部档位不限制
- **新增 sensitive 档位**: 轻度敏感级（泳装、暗示等），与 gelbooru 新体系对齐；旧三档站点（safebooru/rule34/konachan/yande.re）无此档，精确配置 sensitive 时仅 gelbooru 可用，其他站点自动跳过
- **站点可用性按区间筛选**: 站点仅在其可提供的档位与请求区间有交集时参与请求（如 `rating: "explicit"` 时 safebooru/konachan 自动不可用），避免空结果降级浪费
- **请求标签按区间生成**: 单档用正向标签（`rating:general`），多档区间用排除法（`-rating:explicit` 等，Gelbooru 系多排除实测可靠；moebooru 档位少天然规避其多排除失效问题）
- **各站档位映射自动处理**（实测验证）: gelbooru general↔safe 一一对应；safebooru/konachan 整站仅 safe 不加过滤（新旧评级并存全覆盖）；rule34 定位仅 explicit；yande.re 保留旧体系
- **配置注释与 HelpText 补全**: 档位定义、区间语法、示例、各站映射关系、sensitive 档位来源说明

## v1.29.0 (2026-08-02)

### ✨ welcome：全局默认欢迎/告别设置

- **`/welcome global <set|on|off|status>`**: 设置全局默认欢迎消息（所有未单独配置的群生效），支持 `{user}` `{group}` 占位符
- **`/farewell global <set|on|off|status>`**: 设置全局默认告别消息
- **回退语义**: 群内显式配置（set/off 过任意字段）优先，未配置的群自动继承全局默认；`/welcome status` 展示生效配置
- **权限**: 全局设置要求 `superadmin` 角色或 `welcome.global` 权限；群级设置仍为 `welcome.manage`
- **持久化**: 全局配置与群配置一同存入 kv（保留键 `__global__`）

## v1.28.0 (2026-08-02)

### ✨ 新插件：about — 机器人自我介绍

- **`/about` 命令（别名 `/botinfo`）**: 展示框架版本、Git commit（短哈希）、构建时间、Go 版本、仓库地址、已加载插件数与运行时长，并提示 `/help`
- **渐进增强**: 平台支持 Markdown 时以 Markdown 渲染；支持按钮时附带"查看命令列表"快捷按钮
- **`api.GetBuildInfo()`**: 新增导出函数，暴露 main 包注入的构建信息（commit/date）
- **默认注册**: 加入 `bundle.All()` 与 `cmd/bot` 插件集

### 🔘 平台按钮：指令按钮支持（Button.Command）

- **`platform.Button.Command` 新字段**: 指令按钮——点击后在输入框插入命令文本（如 `/help`），由用户自行发送，不产生交互回调事件
- **QQ 映射**: `convertButtons` 操作类型扩展——`Command` 非空 → `action.type=2`（指令按钮）、Link+URL → `type=0`（跳转）、默认 → `type=1`（回调）
- **QQ webhook 回调按钮实测不可靠**（互动事件偶发不投递、PUT 成功后客户端仍报"请求第三方失败"），故 about 按钮采用指令按钮规避互动生命周期

### 🐛 QQ webhook 互动回调链路修复

- **webhook 回包 op=12（HTTP Callback ACK）**: Dispatch 事件回包由空 200 改为 `{"op":12,"d":0}`（协议必需，否则平台视为投递未确认而重试，互动事件因此超时）；事件通道满被丢弃时回 `d=1` 让平台重试
- **`RespondInteraction` 补全**: 携带 `X-Callback-AppID` 请求头（对照官方 botgo SDK）；显式检查 HTTP 状态码与 `err_code`，失败不再被静默吞掉
- **互动事件自动回应**: 适配器收到 `INTERACTION_CREATE` 后自动异步调用 `PUT /interactions/{id}`（code=0），防止客户端 loading 超时
- **送达延迟诊断**: 记录互动事件"触发→送达"延迟，超过 3 秒响应窗口时告警
- **event_id 取值修正**: 被动回复 event_id 取事件最外层 id（payload.ID），与群消息发送接口文档"从事件最外层的 id 获取"一致
- **token.Manager.GetAppID()**: 暴露机器人应用 ID

## v1.27.1 (2026-08-01)

### 🐛 插件系统修复

- **drainDrainingInstances 数据竞争修复**: BG 重载测试触发的预存 race——轮询 goroutine 在 `RLock` 释放后读取 `e.done`，与 `markDrainingDone` 的锁内写入并发。现 `e.done` 读取移回锁内

## v1.27.0 (2026-08-01)

### 🖼️ 新插件：pic — 按标签随机发图

- **`/pic` 命令**: 聚合 5 个 booru 图库（Safebooru / Gelbooru / rule34 / Konachan / Yande.re）按标签随机发送图片，附作品信息（画师/评分/标签/来源链接）
- **内容分级策略**: `rating` 配置控制全局上限（默认 safe），rule34 等 NSFW 站点仅在 explicit/all 下可用；`sites` 白名单限定可用站点
- **服务端随机**: 依据各站官方文档实测（Gelbooru cheatsheet `sort:random` / Moebooru `order:random` meta-tag），每次请求附加唯一参数 + `Cache-Control: no-cache` 防缓存，连续请求不重复
- **站点并发降级**: 多站并发请求取最快成功者（慢站点不再拖垮整次命令）；多图并发下载后按序发送
- **参数消歧**: 末尾 `xN` 数量后缀 + `-count N` 显式张数（想搜 `x3` 标签时 `-count 1` 即可）；`-site` 指定站点；命令定义标注 Arg/Flag
- **AI 工具**: `get_random_image(tags, count?, site?)` 返回图片 URL 与作品信息（不依赖多模态）；`/pic` 命令自动发现即可被 AI 触发
- **认证**: gelbooru.com / rule34.xxx 支持 user_id + api_key 配置；konachan 使用 SFW 镜像 konachan.net 绕过 Cloudflare
- **凭据安全**: 传输错误 URL 中的 api_key/user_id 自动脱敏，不再泄露进日志或回复

### 📤 平台能力：图文同发（CapCaption）

- **`Capabilities.Caption` 新能力**: 声明"同一条消息内可同时携带文本与附件"。Telegram（媒体 caption）、Discord（content+附件）、OneBot（CQ 码混排）、Satori（元素列表）、Terminal 声明支持；QQ 富媒体消息会丢弃文本，不声明
- **pic / sauce 发送策略**: 支持图文同发的平台图片+信息一条消息；其他平台图片逐张发、作品信息汇总一条（Markdown 优先，纯文本降级），修复 QQ 上 sauce 缩略图模式文字丢失问题

### 🔒 sauce 插件：AI 工具与错误脱敏

- **AI 工具**: 实现 ToolProvider，`sauce` 工具接受 image_url 参数走完整多引擎检索（命令与工具共用 searchAll 流程）；显式注册覆盖自动发现的同名命令工具
- **错误脱敏**: SauceNAO 请求失败时 URL 中的 api_key 不再泄露（与 pic 相同的 redactTransportError）

### 🧠 AI 插件：显式注册优先

- **`RegisterToolProvider` 覆盖自动发现**: 显式注册的工具（同名）移除自动发现的命令工具及其命令映射（cmdPatterns），保证 LLM 调用插件自实现的 Execute；`discoverTools` 跳过已显式注册的名称
- **`ToolRegistry.Remove`**: 新增按名删除工具的方法

### ⚡ 性能优化（补记）

- **`Context.Clone` 空存储短路**: `copyExtensions` 不再无条件初始化源/目标的扩展存储（此前克隆裸 Context 也会分配 Extensions+map+Snapshot 整表）。裸 Clone 从 7 allocs/op 降至 4 allocs/op（-43%，289→169ns）；带扩展场景 12→10 allocs。基准矩阵补充 ExecPool 池化场景（PooledLight/PooledHeavyCmdHit/PooledExtreme），覆盖此前缺失的 Clone+TrySubmit 分配路径
- **EventBus.Publish 订阅者切片零拷贝**: 订阅切片按 COW 管理（Subscribe 只写超出旧 len 的下标、Unsubscribe 换新数组），RUnlock 后直接迭代安全，仅当主题与通配符订阅者同时存在才合并拷贝。Publish(1 订阅者) 4→3 allocs（371→329ns）

### 🧩 插件系统设计修复（补记）

- **DryRun 副作用隔离（`Descriptor.DryRunSafe`）**: 依赖推断的探测执行从"批级选项决定"下沉为"插件作者声明"。只有显式声明 `DryRunSafe: true` 的插件在 `WithInferDeps` 下才会被探测执行 Setup（总计两次）；**第三方插件在任何路径下 Setup 恒执行一次**，`ctx.DryRun` 对未声明插件恒为 false
- **依赖声明契约化**: `Deps`/`OptionalDeps` 声明成为插件正确性的必要契约（加载顺序/循环检测/重载级联/级联注销均基于声明）；未声明时框架仅对"批内依赖顺序错误"提供自动重试兜底
- **依赖感知重试**: 批量注册中因依赖缺失失败的插件不再中断整批——进入 pending，等本批依赖就绪后自动重试；失败时错误信息给出可操作提示（声明 Deps 或 DryRunSafe）；原子模式下重试最终失败触发整体回滚
- **InPlace 重载 goroutineMgr 丢失修复**: `newContext` 继承旧 GM，`adv.Reload` 内 Spawn/NewTaskGroup/RegisterCron 不再静默 no-op
- **StartAll/StopAll 状态转换原子化**: `tryTransition` CAS 消除并发双 Setup 导致的 matcher 翻倍窗口
- **StopAll 后可重启**: metaGM 支持重建（Stop → Start 循环恢复 notifyDependents 等元数据任务）
- **热重载语义一致性**: InPlace/BlueGreen 补 `RestoreState`+`MigrateState`（SaveState 不再白存）；BlueGreen swap 回拷 `loadedVer`（版本迁移比较正确）、`ctx.Reg` 组名回写正式插件组（运行期注册不再落入 `__bg`）、旧实例 Teardown 使用旧 Config/EventBus
- **配置热更新**: `propagateConfigChange` 重试定时器在 Manager Shutdown 后丢弃；StopAll 错误聚合格式与 StartAll 统一

### 🧪 测试与文档（补记）

- **新增回归套件**: `core/context/context_escape_test.go`（Clone 分配预算护栏）、`plugin/benchmark_escape_test.go`（Container.Get/EventBus 分配基准）、`plugin/design_review_fixes_test.go`（DryRunSafe/生命周期原子性/BG 语义 7 项）
- **插件开发指南更新**: Smart 注册章节重写（DryRunSafe 契约）、`ctx.DryRun` 字段文档、`Deps` godoc、三色 DryRun 架构笔记补充前提

### 📝 配置

- **config.example.yaml**: 新增 pic 配置节（rating/sites/max_count/凭据），更新 sauce send_thumbnails 注释

## v1.26.1 (2026-08-01)

### 🐛 sauce 插件解析修复

- **SauceNAO 字符串 ID 字段解析失败修复**: 部分索引（Kemono / Patreon 等）将 `pixiv_id` / `member_id` / `twitter_user_id` 等 ID 以字符串返回（如 `"14792976"`），原 `int` 字段解析报 `cannot unmarshal string into ... of type int`，导致整次搜索失败。新增 `flexInt` 类型同时接受 JSON 数字与字符串，无法解析时置 0 而非报错；新增回归测试覆盖字符串/数字混合返回场景

## v1.26.0 (2026-07-31)

### 🎯 出站消息观察者（Outbound Observer）

- **`ctx.Reply` 出站观察钩子**: 新增 `core/context/outbound_observer.go`，通过 `OutboundObserverExt` 上下文扩展注入，`ctx.Reply` / `ReplyWithContext` 在出站发送完成、Future 解析后同步回调观察者（`OnOutbound(chatID, req, res, err)`）
- **不破坏平台能力接口**: 观察者仅依赖 ctx.Reply 的调度任务，区别于包装 `platform.Sender`——后者会破坏 `MessageDeleter`、`GroupManager`、`AutoModerator` 等可选接口断言。回调在 dispatcher 发送任务内同步执行，不阻塞 handler
- **注意**: 仅覆盖经 `ctx.Reply*` 的出站消息；插件直接调用 `platform.Sender.Send`（如 sendqueue）不会被观察到

### 📝 messagelog 出站消息记录

- **记录所有机器人出站消息**: 新增 `RecordOutbound` / `OnOutbound`（实现 `OutboundObserver`），经 `ctx.Reply*` 发送的 bot 回复（含 AI 对话）自动入库；`RecordEntry`/`MessageRecord` 新增 `IsOutbound` 字段，出站消息存于独立 `botBuf` ring，`QueryGroup`/`QueryUser` 不含出站消息
- **`QueryByEventID` 事件 ID 查找**: 按 `chat_id + event_id` 查找消息内容（入站 ring → 出站 ring → SQLite 兜底），支撑"回复机器人上一条消息"的语义
- **AI 插件迁移**: `replyAndRecord` 薄封装确保 AI 的对话回复总能被记录（上下文缺少观察者时用 `p.history` 补上），替代原先 per-call goroutine 记录方案

### 🧠 AI 插件：上下文注入与隐私控制

- **回复上下文（`include_reply_context`）**: 群聊中"回复某条消息并 @ 机器人"时，把被回复消息内容前置到用户消息；命中机器人自己的出站消息时以"机器人"标注发送者
- **群聊消息窗口（`context_group_messages`）**: 把同群最近 N 条入站消息（`昵称: 内容`，旧到新）注入系统提示，让 AI 感知多人对话
- **提及净化与结构化**: `stripMentionMarkup` 剥离各平台 @ 提及标记（Discord `<@id>`/`@everyone`、onebot/QQ `@QQ号`/`@全体成员` 等，不误伤 Telegram `@username`）；`appendMentionInfo` 将本条消息 @ 的其他用户昵称结构化追加到用户消息
- **运行时上下文字段白名单（`include_runtime_context` / `context_fields`）**: 可关闭整个运行时上下文注入，或通过白名单精确控制随请求发送给第三方 LLM 的字段（time/platform/bot_id/user_id/chat_id/group_role 等 12 项），保护用户 ID、群 ID 等隐私

### 🧠 AI 插件：工具循环与会话加固

- **工具结果截断**: `truncateToolResult` 单条工具结果回填上限 8000 rune，防止命令巨型输出撑爆上下文窗口
- **历史附件二进制降级**: `prepareRequestMessages` / `stripBinaryParts` 仅保留当前轮用户消息的多模态附件，历史消息中的图片/音频降级为文本占位，避免每轮对话重复上传附件
- **工具分类路由缓存**: `getOrRouteCategory` 按会话缓存路由决策（10 分钟 TTL + 工具集数量校验），避免每条消息都多一次路由 LLM 调用；`filterToolsByCategory` 始终保留通用工具作为缓存过期/误判时的兜底
- **`CleanupExpired` 锁外持久化**: 先持锁收集过期会话并移出缓存，释放锁后再执行 DB 删除，不再阻塞其他会话操作
- **会话保存 upsert**: GORM `OnConflict(UpdateAll)` 单语句 upsert 替代 `SELECT + INSERT/UPDATE` 两次往返

### 🧠 AI 插件：LLM 提供商加固

- **请求级模型覆盖**: `ChatRequest` 支持请求级 `Model` / `MaxTokens`，未指定时回退客户端默认值（OpenAI / Anthropic 双实现）
- **流式请求重试**: `doStreamRequest` 对传输错误与 5xx 响应按 `max_retries` 配置指数退避重试（每次重建请求体；4xx 不重试）
- **Anthropic 并行工具调用**: 按 `tool_use` block 的 index 独立累积流式分片，支持并行工具调用不再互相覆盖
- **错误信息脱敏**: `formatAIError` 未处理错误不再向用户暴露原始 API 错误体（可能含组织/计费信息），改为记录详细日志后返回通用提示

### 🐛 AI / FSM 修复

- **触发命令排除**: `discoverTools` 不再把 AI 自身触发命令（默认 `/ai`，跟随 `trigger_cmd`）自动发现为工具，避免 AI 自我递归调用
- **类型化 FSM 错误**: 新增 `fsm.ErrSessionExists`，`StartSession` 会话已存在时通过 `errors.Is` 判断（skill add 两阶段注册已迁移）
- **总结任务单飞**: 同一会话已有总结任务在跑时不再启动新的 goroutine，避免重复总结
- **Skill 整词匹配**: `handleSubCommand` 仅整词匹配 `skill`/`技能` 前缀，避免误命中 `skillful`、`技能介绍` 等普通聊天
- **Skill promote 工具注册修复**: 用去除 `u_` 前缀的新名称查询系统技能并注册为工具

### 🖼️ sauce 插件 v2.0：多引擎聚合搜图

- **新增 IQDB / TraceMoe 引擎**: IQDB（booru 图库检索，对裁切图最友好，免费）；TraceMoe（动画截图逐帧识别，给出番剧/话数/时间点，免费），两者默认开启；ASCII2D 适配新检索流程并支持特征（BOVW）搜索
- **SauceNAO 增强**: 改为 multipart 上传，可配置检索数据库 `saucenao_db`，显式上报限流错误
- **裁切图预处理**: 上传前将小图（任一维 < 400px，多为裁切图）放大 2 倍，提高命中率
- **跨引擎结果合并**: 结果去重合并、相似度阈值过滤（`similarity_threshold`），存在高匹配时只展示高匹配，否则保留全部避免裁切/滤镜图被过滤
- **输出截断**: 单条结果标题（`max_title_len`）与整条文本消息（`max_message_len`）按 rune 截断；`report_errors` 可选展示引擎失败原因
- **代码结构**: 按职责拆分为 client/merge/preprocess/format 等 14 个文件并补充 godoc，新增 6 个测试文件

### 🔐 QQ Webhook 签名校验加固

- **Ed25519 密钥缓存**: 构造时缓存密钥而非每请求重新派生；`GenerateKey(reader)` → `NewKeyFromSeed`（协议要求的种子展开字节级一致）
- **HTTP 健壮性**: 拒绝非 POST（405）、`MaxBytesReader` 限制请求体（413）、所有路径关闭包裹 body、返回 `http.Error` 错误体
- **测试补充**: sign/key/round-trip/handler 测试（sign_test.go / webhook_test.go）

### 🧪 测试与 CI

- **新增测试**: `api/handler_test.go`（HTTP handler 与 auth 中间件）、`platform/discord/sender_internal_test.go`、`platform/milky/api_test.go`、`platform/discord|onebot/event_test.go` 等，共 2400+ 行
- **CI**: 覆盖率排除 `examples/` 与 `cmd/bot/plugins/`；coverage 输出文件加入 gitignore
- **静态检查**: 修复 staticcheck lint 与 gofmt 问题；`go fix` 字段对齐（`builtin/messagelog`）

## v1.25.0 (2026-07-30)

### ✨ QQ 平台 WebSocket 支持

- **WebSocket 协议实现**: 新增 `platform/qq/wsconn.go`，完整实现 QQ Bot WebSocket 协议（Hello/Identify/Resume/Heartbeat/Reconnect），支持指数退避自动重连和 Session 恢复。
  - 新增 11 个 Intents 位掩码常量，精确控制事件订阅
  - `WSConn` 通过 `EventStream()` 实现 `qq.EventSource` 接口，可作为事件源复用现有 `Adapter`
- **`WSAdapter` 工厂**: `NewWSAdapter` / `NewWSAdapterWithIntents` 开箱即用，自动管理 Token 和 WebSocket 连接
- **Gateway API**: `GetGateway()` / `GetGatewayBot()` 端点方法
- **接口改名**: `qq.Webhook` → `qq.EventSource`（WebSocket 也实现），`WebhookConn` → `WebhookService`（与 `webhook.Conn` 消歧义）

### 🚀 新消息类型

- **卡片消息 (msg_type=8)**: 新增 `dto.Card`/`CardContent` 结构体，支持群聊图文卡片
- **流式消息**: 新增 `dto.StreamMessage` DTO 和 `SingleStreamChat()` API，支持 C2C 流式分片发送
- **输入中状态 (msg_type=6)**: 新增 `dto.InputNotify` 和 `TypingIndicator: true`
- **Button 扩展**: 新增 `ButtonExtra{Enter, Reply, Anchor}`，支持指令按钮自动发送/引用回复/选图器
- **Markdown 模板参数**: `MessageExtra` 支持 `MarkdownTemplateID` 和 `MarkdownParams`
- **富媒体分片上传**: 新增 `UploadPrepare` / `UploadPartFinish` 预上传和分片确认 API

### 🎯 事件增强

- **引用消息解析**: `message_type=103` 时自动从 `msg_elements[0].content` 提取正文
- **群角色映射**: `author.member_role` 解析为 `platform.GroupRole`（owner/admin/member）
- **能力声明更新**: `Capabilities().TypingIndicator = true`

### ✅ 测试

- `wsconn_test.go`: 8 个 WebSocket payload 序列化/解析测试
- `sender_test.go`: 8 个消息发送 DTO 构建测试
- `event_test.go`: 新增 3 个事件解析测试（quote/role/mentions）

## v1.24.0 (2026-07-27)

### 🐛 AI 插件修复

- **工具名不合法导致 API 400 错误修复**: 自动发现的命令中包含中文管理命令（`/开启`、`/关闭`、`/封禁` 等），其名称不匹配 OpenAI 的 `^[a-zA-Z0-9_-]+$` 规则，导致 `Invalid 'tools[2].function.name'` 错误。
  - `isCommandSafeForAI` 新增名称为合法性检查，含不合法字符的命令不再自动暴露为 AI 工具
  - `ToolRegistry.Register` 新增名称校验，不合法时自动修正并记录警告日志
  - `buildToolFromCommand` 新增 `sanitizeToolName` 兜底清洗函数

- **自动发现工具 token 过量消耗修复**: 所有自动发现的命令全部归入 `"general"` 分类，路由机制无法有效分组，67 个工具每次请求发送 ~1500-3000 tokens。
  - 新增 `deriveToolCategory` 按来源插件名对自动发现的命令进行归类
  - 路由阶段 LLM 先选分类再发对应工具，大幅降低 token 消耗

## v1.23.0 (2026-07-27)

### ✨ 新插件

- **cmd/bot/plugins/sauce/**: 新增以图搜图插件，通过 SauceNAO API 查找图片来源。
  - `/sauce` 命令，发送图片并在标题中附带命令即可搜索
  - 支持 `send_thumbnails` 配置项，将缩略图嵌入结果消息一起发送
  - 80% 相似度阈值：高匹配度结果优先展示，不再显示其他候选
  - 所有配置字段支持热更新 `[H]`
  - ASCII2D 搜索可选（默认关闭，被 Cloudflare 拦截时无法使用）

## v1.22.0 (2026-07-27)

### 🐛 Engine 核心修复（2026-07 core 深度复查）

- **COW 就地排序数据竞争修复**: `withUpdatedMatcherIndex`/`withBatchMatchers` 对 `v[:len:len]` 复制（共享底层数组）出的切片就地排序，改写已发布旧 state 中正被 `processEventMatchers` 无锁读取的 commandIndex 数组。命令 matcher 的 `SetPriority`/`BindCommand`、插件 `BatchRegisterMatchers` 与事件处理并发时构成 data race。现排序前做逐元素完整拷贝，批量注册只重排本批次实际触及的桶
- **`WithSharedExecPool` 失效修复**: NewEngine 在应用 options 后无条件覆盖 `execPool`，共享池被静默丢弃。现仅在未注入时创建自有池；`Engine.Shutdown` 不再 Drain 共享池（生命周期归调用方）
- **中间件热更新不达临时 matcher 修复**: `Use`/`UseForGroup`/`ResetMiddlewares`/`ResetGroupMiddleware` 只失效 state 中的 matcher，TempManager 中的活跃会话 matcher 因 `compiledVersion` 快路径永久沿用旧链（插件重载的守卫中间件可被绕过）。新增 `tempManager.ForEach` 统一失效
- **OnTemp 后设超时永不过期修复**: 已是 temp 的 matcher 调 `SetTempWithTimeout` 不会登记过期堆，未被使用的会话 matcher 泄漏至高水位强制清理。现通过 `SetExpiration` 在 shard 锁内写入过期时间并补登堆（同时消除 expiresAt 无锁读写竞态）
- **`DeleteMatcher` 恢复批量删除路径**: 生产代码中批量删除处理器的入队端已不存在（纯死代码，每 100ms 空转）。现 `DeleteMatcher` 立即标记 deleted（即刻停止匹配）并入队，由处理器按配置间隔批量 COW 删除；处理器未运行或队列满时退化为同步删除。新增 `FlushPendingDeletes` 供确定性收尾；修复 ticker 误用常量导致 `WithPendingDeleteProcessInterval` 配置被忽略的问题
- **链式元数据缓存陈旧修复**: `OnCommand().SetDescription()` 等文档推荐写法的元数据停留在注册瞬间的空值（/help 显示空描述、`SetHidden` 不生效），直到无关的全量索引重建。现 `SetDescription`/`SetUsage`/`SetCategory`/`SetAliases`/`SetExamples`/`SetPermissions`/`SetHidden`/`BindCommand` 统一触发 `UpdateCommandCache`
- **批量注册命令元数据缺失修复**: `withBatchMatchers` 未更新 commandInfoCache，批量注册的命令不出现在 `GetAllCommands`//help 中
- **命令索引分类收紧**: 索引重建改以 `commandIndexed` 标志（OnCommand/RegisterCommandDef 置位）判定命令 matcher；普通 matcher 事后补充 `Definition.Name` 不再于重建后被迁入 commandIndex 并跳过 Rules[0]（语义漂移）
- **Dispatcher 同 chat 双 worker 竞态修复**: worker 摘除空队列与 Submit 获取队列并发时，同一 chat 可能出现两个并存队列与 worker，破坏 FIFO 保证。Submit 现持锁复查映射并重试；同时消除每次 Submit 的 chatQueue 预分配
- **ExecPool 队列滞留修复**: 任务在所有 worker 完成最终 drain 之后入队时无人消费，直到下次提交才被处理。新增 exitMu 退出协议：入队后要么现存 worker 必然看到任务，要么立即抢到令牌启动 drain worker
- **归并迭代器哨兵值边界修复**: 优先级恰为 `MaxUint` 的 matcher 会被 `bestPrio` 哨兵比较跳过并提前终止整个归并；`getPriority` 在 32 位平台截断 uint64。改用显式 found 判定 + uint64 全程比较
- **TempManager 快照写放大修复**: 此前每次 Add/Remove 都全量收集 8 个 shard 并整体排序重建 RCU 快照（O(N log N)/操作），高频会话场景（OnTemp 一问一答 = 两次全量重建）写放大严重。现 Add/Remove 改为 COW 增量插入/移除（仅替换受影响 eventType 的切片），CleanExpired/水位清理保留全量重建；含防"幽灵 matcher"的 byID 二次校验与并发全量重建去重
- **温和修复**: `StartTempMatcherCleaner` 公开入口补 writeMu（消除与 Shutdown 组件的竞态）；TempManager 水位/过期清理统一置 deleted 标志并重建 RCU 快照（周期清理路径此前快照不更新）；`cleanupWg` 纳入 Shutdown 等待；processEventMatchers 兜底分支加读锁读取 Handler；`EnableGlobalMatchers` 补充真实语义说明

### 🐛 Context 规则修复

- **`OnMentionedBot` 平台不支持时永不触发修复**: 文档承诺"平台未实现 MentionsEvent 时放行"，实现却恒返回 false。现按文档语义放行，由 EventType 路由过滤
- **`OnCooldown` 误消耗冷却修复（延迟副作用机制）**: 此前规则命中瞬间即写入冷却——排在前面时，后续规则失败或其他 matcher 才是处理者，用户冷却已被白白消耗。新增 `Context.DeferRuleEffect/CommitPendingRuleEffects/DiscardPendingRuleEffects`：引擎在 matcher 全部规则通过后统一提交副作用、失败则丢弃，OnCooldown 改为延迟写入，规则顺序不再影响正确性

### 🔒 Permission 内核修复

- **请求侧通配符绕过修复**: `Permission.Match` 把 target 侧的 `"*"` 当通配符放行，用户可控字符串透传进 `HasPermission` 时可用 `"*"` 探测/绕过检查（与文档 "wildcards in target are treated literally" 相反）。现 target 侧一律按字面值处理
- **`GrantPermission` 幂等化**、**`HasPermission` Provider 调用移出锁外**（慢查询不再长时间阻塞写操作）

### 🐛 FSM 修复

- **Session 并发保护**: 同一 sessionID 的启动/迁移现由 per-session 互斥锁串行化，消除并发事件对 `Session.Current`/`Data`（map）的数据竞争；两个并发 `StartSession` 不再互相覆盖。回调重入约束见 `FSMContext` 文档
- **`TryStartSession` 持锁执行回调修复**: 此前整个遍历持有 `e.mu.RLock`，回调内 `Register`/`Unregister` 直接死锁。现锁内快照 FSM 列表、锁外执行回调
- **孤儿会话永久阻塞修复**: 关联 FSM 被 `Unregister` 后，遗留会话导致该 sessionID 永远无法开启新会话（无 Timeout 时）。现自动清理孤儿会话
- **`GetSession` 改为返回副本**（与文档一致，外部修改不再绕过会话锁）；新增 `UpdateSessionData` 作为外部写入 Data 的受支持方式（builtin/ai 已迁移）；`Timeout` 文档明确"自创建起计"
- **新增 `FSM.RefreshOnActivity` 滑动 TTL**: 为 true 时每次成功迁移把过期时间重置为 now+Timeout，长对话保持活跃即不过期；默认 false 保持既有"自创建起计"语义

### 🐛 平台适配器修复

- **Discord handler 叠加泄漏修复**: `Start` 可被重复调用（重连/重启），旧 handler 未注销导致同一事件被重复分发。`registerHandlers` 返回注销函数列表，`defer` 统一清理。send-on-closed-channel 竞态修复：消除 `close(eventCh)` 与 goroutine 发送的竞态窗口
- **Milky 适配器增强**: 错误响应脱敏，sender 补充内联键盘、消息编辑、消息删除、Reaction 等功能
- **Mock 适配器**: 补充 `MessageDeleter`/`TypingNotifier` 接口实现，支持测试中模拟平台操作
- **OneBot 深度修复**: `fetchBotIdentity` 时序修复（receiveLoop 启动前同步调用导致响应永远无法送达）；消息解析重构（支持 `CQ:xml`、`CQ:json`、`CQ:card` 等富媒体类型）；并发写入 WebSocket 竞态修复；补充大量单元测试
- **QQ 平台修复**: Token 管理器 debug 日志泄漏 `AppSecret`/`access_token` 凭据，改为仅记录 token 长度；`openapi` 超时与重试增强；Webhook signing 签名算法补充；Sender 补充 Reply/成员接口
- **Satori 修复**: quote 解析丢失正文（自闭合 `<quote/>` 被 HTML 解析器误认导致后续兄弟节点丢失）；XML 注入修复（`%q` 反斜杠转义不被 XML 识别，用户输入可提前闭合属性注入任意元素）；事件的 `message_id` 统一；WebSocket 重连增强；全面测试补充
- **Telegram 全面增强**: 修复高频场景下的超时/重试逻辑；事件映射补全（频道、超级群组、话题组等消息类型）；Sender 补充 MarkdownV2 转义、内联键盘、编辑/删除/Reaction 全链路；消息类型定义标准化
- **Terminal 重大重构**: 输入解析重写（支持补全、历史、多行）；新增 `sanitize.go` 输入净化；VT 控制序列增强；启动/窗口尺寸检测兼容性修复；全面测试覆盖
- **Registry 增强**: `FindAdapter`/`GetAdapterNames`/`GetDefaultAdapters` 等新方法

### 🐛 插件系统修复

- **Manager 并发安全重构**: `StopAll`/`Manager.Stop` 竞态修复，`InvokeOnEvent` 锁协议重写；Shutdown 阶段等待所有插件平稳退出；`Manager.diag` 命令支持运行时诊断
- **注册流程优化**: 依赖排序稳定性修复；批量注册场景下 `RegisterAll` 的并发安全；`ValidateDependencies` 死锁修复
- **热重载修复**: `ReloadAll`/`Reload` 重入保护；配置变化时按拓扑序安全重启受影响插件
- **Runtime 修复**: `runtime_context` 生命周期隔离（插件间不互相干扰）；`runtime_instance` 并发写保护
- **WASM 运行时**: ABI 序列化边界修复；沙箱超时兜底；Sandbox 重连隔离；扩展点注册性能优化
- **EventBus/Scope/Container**: Scope 销毁时的事件清理；Container 值变更通知并发安全；Proxy 调用超时传递

### 🐛 中间件修复

- **Dedup 并发加强**: 修复 `max_size` 缩减时的 LRU 淘汰竞态，热路径增加 `sync.Map` 兜底
- **CircuitBreaker 无限震荡修复**: `SuccessThreshold > HalfOpenMaxRequests` 时，半开窗口永远达不到闭合所需的成功数，熔断器在 开↔半开 之间无限震荡。新增启动和热更新时的钳制逻辑；更新测试验证边界条件

### 🐛 内置插件修复

- **AI SubCommand**: 子命令错误处理链路修复，确保 `Skill add` 失败时正确返回错误而非静默继续

### 🐛 杂项修复

- **`bot.go` 关闭信号监听**: `WaitForShutdown` 已有监听者时后续调用从 panic 改为 warn 日志并直接返回，避免两个监听者竞争同一信号
- **`doc.go` 文档更新**: 更新 Reply 异步语义和 Handler 示例

### 📝 测试与验证

- **新增复查回归测试套件**: `core/engine/review_fixes_test.go`（364 行，覆盖 Engine 核心修复的所有边界场景）；`core/fsm/fsm_review_test.go`（106 行，覆盖 FSM 并发/孤儿会话/RefreshOnActivity）；`core/permission/permission_review_test.go`（36 行，覆盖通配符绕过/GrantPermission 幂等/Provider 锁外调用）
- **`tests/fixes_validation_test.go`**: 补充 22 行验证用例
- **Router 测试**: 补充 `WaitForAsyncHandlers` 等待，确保 v1.21.1 的 ExecPool 异步化后断言正确

### 📚 文档

- **新增 5 篇架构笔记**: `20-core-review-lessons.md`（八个并发缺陷模式与契约方法论）、`21-outbound-dispatcher.md`（FIFO 调度与 Future 集成）、`22-adaptive-execution.md`（ExecProfile + ExecPool + 退出协议）、`23-context-design.md`（双键扩展系统、Clone 语义、延迟副作用）、`24-bot-assembly.md`（Bot/BotBuilder/BotManager 装配层）
- **更新现有笔记**: 01-cow-engine、02-six-way-merge-matcher、08-command-system、12-fsm-engine、13-adaptive-router 全面修订；notes/README.md 索引更新
- **用户指南更新**: `GETTING_STARTED.md`、`TROUBLESHOOTING.md`、`CONFIGURATION_QUICKREF.md`、`MATCHER_CHAINING_BEST_PRACTICES.md`、`README.md` 同步修订
- **架构文档更新**: `CONCURRENT_EVENT_PROCESSING.md`、`permission-system.md`、`OUTBOUND_DISPATCHER_PLAN.md`
- **测试文档**: `command/TESTING.md`、`config/TESTING.md`、`plugin/TESTING.md`

## v1.21.1 (2026-07-26)

### 🐛 API 安全加固

- **config 密钥脱敏静默失效修复**: 因 Go 结构体只有 yaml 标签、json.Marshal 输出 Go 字段名，与脱敏路径不匹配导致密钥明文返回。新增 `normalizeKey`/`lookupKey` 归一化匹配，覆盖 `api_key`/`token`/`secret`/`password`/`access_token`
- **CORS 通配放行修复**: `Access-Control-Allow-Origin: *` 改为仅放行 `tauri://` 和本机回环地址，避免恶意网页跨域调用管理 API
- **未配置 api_key 时远程访问修复**: 降级策略从"无条件放行"改为"仅本机回环可访问"，防止监听 `0.0.0.0` 时管理 API 对整个网络裸奔

### 🐛 Engine 核心修复

- **OnCommand handler 同步阻塞修复**: 未设置 `execProfile` 导致命令 handler 始终同步执行在平台派发 goroutine，慢命令（AI、外部 API）阻塞该平台所有会话。现为每个命令匹配器创建执行 profile，使其正确进入 ExecPool 异步调度
- **ExecPool 任务 panic 兜底修复**: 池中 handler 逃逸的 panic（中间件链构造、中间件自身）直接终止进程。新增 `runPoolTask` 带 `defer/recover` 保护
- **processEventMatchers Block 语义失效修复**: 阻断判定在入池成功后被 `continue` 跳过，导致 `SetBlock(true)` 行为不确定、会话等待回复继续命中后续匹配器造成重复处理。现修复为入池前计算阻断标记，入池后立即 break
- **Context 异步执行数据竞争修复**: 入池前缺少 `Clone`，池 goroutine 与派发循环并发写入同一个 `*Context`，造成 SetMatcher 撕裂、deadline/span 互相覆盖

### 🔒 凭据泄露修复

- **milky 适配器**: WebSocket 连接错误中原样返回含 `access_token` 的 URL，现予以脱敏
- **QQ token 管理器**: `BotInfo` debug 日志泄漏 `AppSecret` 长期凭据；`access_token` 刷新日志直接记录活凭据内容，现改为仅记录 token 长度
- **Telegram 客户端**: `net/http` 传输错误（超时/DNS 抖动）以 `*url.Error` 形式携带完整请求 URL，bot token 嵌入路径中。新增 `redactedError` 包装器，格式化时抹掉 token
- **OneBot 适配器**: 使用 `WriteMessage` 发送关闭帧导致 `panic("concurrent write to websocket connection")`，改为 `WriteControl` 解决

### 🐛 中间件修复

- **BackpressureBlock 永久堆积修复**: 阻塞在信号量上时不监听 `ctx.Done()`，一旦 maxInFlight 个 handler 卡在不响应 ctx 的 IO 上，后续事件 goroutine 永久堆积直至 OOM。现添加 `select` 双路监听
- **CircuitBreaker 无限震荡修复**: `SuccessThreshold > HalfOpenMaxRequests` 时，半开窗口永远达不到闭合所需的成功数，熔断器在 开↔半开 之间无限震荡。新增启动和热更新时的钳制逻辑
- **重试退避溢出修复**: `BackoffBase * (1<<shift)` 在 attempt 较大时溢出为负值，导致零延迟重试。现钳制到 `BackoffMax`
- **SlowHandler 错误屏蔽修复**: 此前用 `stdCtx.Err() != nil` 判断是否超时，但该错误可能来自父级取消而非本中间件注入的监控 deadline，导致真实业务错误被静默丢弃。现改为检查 `errors.Is(err, DeadlineExceeded) && originalCtx.Err() == nil`

### 🐛 基础设施修复

- **audit/pprof 二次 Close panic 修复**: 多条停机路径（显式 Close 与 defer 清理）先后关闭 `stopCh`，无条件 close 已关闭的 channel 会 panic。新增 `sync.Once` 保护
- **DLQ Close 死锁修复**: `DropPolicyBlockUntilSpace` 生产者持有 `enqueueMu` 阻塞在 channel 发送上，Close 抢不到该锁造成死锁。新增 `closing` 通道先唤醒生产者，再安全 close channel
- **HTTP Server Slowloris 防护**: 零值 `ReadHeaderTimeout` 表示"永不超时"，攻击者可逐字节发送请求头长期占住连接。设置 `ReadHeaderTimeout=10s`、`ReadTimeout=30s`、`IdleTimeout=120s`、`MaxHeaderBytes=1MB`

### 🐛 平台适配器修复

- **Discord handler 叠加泄漏**: `Start` 可被重复调用（重连/重启），旧 handler 未注销导致同一事件被重复分发。`registerHandlers` 返回注销函数列表，`defer` 统一清理
- **Discord send-on-closed-channel 竞态**: `close(eventCh)` 时 discordgo goroutine 可能仍停留在 `send()` 的 select 上，随机选择时可能选中"向已关闭 channel 发送"造成 panic
- **OneBot fetchBotIdentity 时序**: 在 `receiveLoop` 启动前同步调用 `get_login_info`，响应永远无法送达导致每次连接阻塞数秒、`botID` 为空引发自我回复成环
- **Satori quote 解析丢失正文**: 自闭合 `<quote/>` 被 HTML 解析器误认，后续兄弟节点变为其子节点导致整条正文丢失。正则预剥离后再解析
- **Satori XML 注入**: `%q` 用反斜杠转义双引号，XML 不识别该转义，用户输入可提前闭合属性注入任意元素

### 🐛 内置插件修复

- **Cooldown GC 绕过**: 固定 24h GC 提前删除 7 天冷却期的记录，`Allow` 把"键不存在"视为放行。现按 `max(entry.cooldown, maxAge)` 判定回收门槛
- **Admin 权限提升**: 普通 admin（`*:*` 权限即可匹配 `perm.role`）可直接 `/perm role <自己ID> superadmin` 提权。现要求授予 superadmin 必须是 superadmin 本人
- **Job panic 进程终止**: 后台作业未捕获的 panic 直接终止进程。新增 `safeInvoke` 将 panic 转为普通错误
- **Kick --ban 误判**: `len(args) >= 3` 将可选"原因"参数视为永久拉黑开关，照文档操作的用户被意外拉黑。改为显式 `--ban`/`拉黑` 标记
- **Stats 命令 map 无限增长**: 用户消息中的任意 token 被计入命令统计，map 既无淘汰又每 5 分钟整体落盘，可撑爆内存和磁盘。限制 `maxTrackedCommands=1000`、`maxCommandKeyLen=64`
- **Subscription 死锁**: `Subscribe`/`Unsubscribe` 已持写锁后调用 `save()`（内部再取读锁），RWMutex 不可重入导致自死锁。拆分为 `save()`/`saveLocked()`
- **Skill add 返回缺失**: 发送帮助提示后未 `return nil`，继续执行注册流程造成重复操作

### 🔧 HTTP 安全增强

- **HTTP Server 新增超时配置**: `ReadHeaderTimeout=10s`、`ReadTimeout=30s`、`IdleTimeout=120s`、`MaxHeaderBytes=1MB`，防止 Slowloris 攻击

## v1.21.0 (2026-07-26)

### 🚀 新平台适配器：Telegram

- **零外部依赖**: 使用 `net/http` 直接调用 Telegram Bot API，无需第三方 SDK
- **长轮询 + Webhook 双模式**: 默认长轮询（`getUpdates`），配置 `webhook` 块自动切换为 Webhook 模式
- **完整事件映射**: 私聊/群组/频道消息、编辑消息、CallbackQuery、Bot 加入/移除等 → `platform.Event`
- **Sender 全面实现**: 文本、MarkdownV2、图片/音频/视频/文件（URL 直传 + 二进制上传）、内联键盘、消息编辑、删除、Reaction、Typing 指示
- **HelloBot 可用**: 开箱即用，只需 `token` 即可运行

### 🔧 配置与构建

- **config**: 新增 `TelegramConfig`（`token` / `poll_timeout` / `webhook`），`HasChanged` 包含 Telegram
- **setup.go**: 添加 `telegram` factory，按配置自动启用
- **config.example.yaml**: 新增 Telegram 长轮询和 Webhook 两种模式示例

## v1.20.1 (2026-07-17)

### 🔒 SSRF 防护增强

- **附件下载 DNS 级 IP 校验**: `isAllowedDownloadURL` 对域名执行 DNS 解析，检查所有解析结果是否为公网 IP
- **专有 HTTP client**: 引入 `attachmentHTTPClient`，在 dial 阶段二次校验目标 IP，防止 DNS 重绑定攻击
- **重定向安全检查**: 对 3xx 跳转目标同样执行 `isAllowedDownloadURL` 校验

### 🧠 AI 插件改进

- **SkillRegistry 命名空间隔离**: 由全局 `name→Skill` 改为 `ownerID+name→Skill` 复合主键，避免用户之间的技能名冲突
- **命令参数安全过滤**: `executeRealCommand` 对 LLM 生成的参数做 ASCII 可打印字符校验，防止命令注入
- **ToolAllowlist 配置**: 新增 `tool_allowlist` 配置项，显式控制哪些命令自动暴露为 AI 工具
- **ToolRegistry 并发安全**: 新增 `sync.RWMutex` 保护，消除 Setup 后并发读写的竞态风险
- **会话并发串行化**: 新增 `Session.turnMu`，确保同一会话的对话回合严格串行
- **CleanupExpired 加锁修复**: 清理过期会话时先持有 session 锁再检查 TTL
- **发现时机延迟**: `discoverTools` 从插件 Setup 阶段移至所有插件注册完成后执行
- **配置校验增强**: `temperature`/`top_p`/`max_retrie`s 加载时检查合法范围，`configFloat`/`configInt` 支持 `json.Number`
- **断连修复**: `trigger_cmd` 配置加载简化，消除死分支

## v1.20.0 (2026-06-30)

### 🧩 新 Context 便利方法

- **TryEditMessage** — 编辑 bot 已发送的消息（基于 MessageEditor）
- **TryAddReaction / TryRemoveReaction** — 对消息添加/移除表情回应（基于 ReactionSender）
- **TryDeleteMessage** — 撤回 bot 自己发送的消息（基于 MessageDeleter）
- **TrySendTyping** — 发送"正在输入"指示（基于 TypingNotifier）

## v1.19.0 (2026-06-30)

### 🚀 性能优化

- **SQLite WAL + synchronous=NORMAL**: messagelog & 业务存储启用 WAL 模式和 NORMAL 同步级别，消除 `FlushFileBuffers` 瓶颈（pprof 中占 80ms/20%）
- **auditlog 批量写入**: channel + flushLoop 替代 per-entry goroutine，消除高频 goroutine 创建（pprof 中 272 次/15s）
- **messagelog flush 频率**: 100ms → 500ms，减少写事务频率
- **auto_vacuum**: 新增 `PRAGMA auto_vacuum = INCREMENTAL`，Clear 时自动回收空闲页
- **Future 分配基准测试**: 实测 ~35 ns/op，112 B/op
- **Dispatcher 队列注入基准测试**: 实测 ~4.5 ns/op，0 allocs

## v1.18.0 (2026-06-30)

### 🚀 新特性

- **OutboundDispatcher**: 新增出站任务调度器，`ctx.Reply()` 现在返回 `*future.Future[platform.SendResult]`，提交即返回，发送在后台执行。
  - 同一 Chat 消息严格 FIFO，不同 Chat 并发发送
  - 不阻塞 Handler goroutine（ExecPool 与 SendPool 解耦）
  - 支持三种 Shutdown 语义：Close / Drain / ForceClose
  - 支持 Retry、Timeout、Metrics、Logging 装饰器（sender.Chain）
  - DispatcherHooks 提供完整的发送生命周期观察

- **infra/future**: 新增泛型 Future 类型
  - `Wait(ctx)` / `Result()` / `IsDone()` / `MustWait(ctx)` / `Done()`
  - `sync.Once` 保护，多次 Resolve 安全
  - 零额外 GC 压力

### 🧾 Reply 语义变更

`ctx.Reply()` 由同步发送改为异步提交：

之前：
```go
res, err := ctx.Reply(msg)  // 阻塞直到发送完成
```

之后：
```go
ctx.Reply(msg)               // 提交即返回，发送在后台

// 仍可等待结果：
future := ctx.Reply(msg)
res, err := future.Wait(ctx)
```

**兼容性**: Go 允许忽略返回值，所有 `ctx.Reply(msg)` 调用零修改编译通过。

完整设计文档：[docs/05-performance/OUTBOUND_DISPATCHER_PLAN.md](docs/05-performance/OUTBOUND_DISPATCHER_PLAN.md)

## v1.17.1 (2026-06-29)

### 🔧 修复

- **`middleware/middleware.go` 可读性提升** — 将 `RateLimitTokenBucketWithConfig` 中内联的 120 行分片桶逻辑提取为包级类型和方法（`rateLimitShard`/`rateLimitShards`/`fnv1aHash`/`cleanupIfNeeded`/`getOrCreateLimiter`），主函数缩减至 40 行
- **`latency` 字段改为 `latency_ms`** — 结构化日志字段名明确反映单位为毫秒，避免 `time.Duration` 的 JSON 序列化歧义

### 🔄 变更

- **Metrics 中间件文档完善** — 补充 godoc 说明其与 Logging 的关系（Metrics 是轻量 Debug 版本）

## v1.17.0 (2026-06-29)

### ✨ 新功能

- **用户自定义技能（User Skill）** — 用户可通过 `/ai skill add` 上传 Markdown 注册自己的 Prompt-only Skill，名称自动 `u_` 前缀防冲突
- **两阶段注册流程** — `/ai skill add <名称>` 后，FSM 自动等待用户下一条消息或 .md 附件作为内容，`cancel`/`取消` 可放弃
- **技能提升为系统级** — 管理员可通过 `/ai skill promote` 将用户技能提升为所有用户可见的系统技能（去掉 `u_` 前缀）
- **技能启用/禁用开关** — 用户可独立控制每个技能的启用状态，禁用的技能不注入会话
- **技能调用统计** — 自动记录每个技能的调用次数

### 🔧 修复

- **AI 插件生命周期 context** — 替换 `context.Background()` 为 `lifecycleCtx`，插件关闭时及时取消后台操作（附件下载、总结生成等）
- **`doSummary` 关闭时静默退出** — 插件关闭导致的 LLM 调用取消不再向用户回复错误的总结失败消息
- **技能内联注册换行支持** — `strings.SplitN` 按空格分割 → `strings.Fields` 按任意空白分割，支持换行分隔的 Markdown 内容
- **`/ai cancel` 不再被 FSM 消费** — FSM 检查仅作用于 `@bot`/私聊路径，命令路径不受影响

### 🔄 变更

- **`Skill` 结构体新增 `OwnerID`、`Enabled`、`UsageCount` 字段** — `OwnerID = "system"` 为系统技能，用户 ID 为用户技能
- **`SkillRegistry` 重构为线程安全** — 引入 `sync.RWMutex` 和 `byOwner` 二级索引，支持运行时注册/注销
- **`RegisterSkill` 自动设置 `OwnerID = "system"`** — 向后兼容，外部插件调用不受影响
- **`buildSkillTools` 仅注入系统技能** — 用户技能的子 Agent 无法调用其他用户的技能
- **子命令定义完善** — 所有 `/ai` 子命令补充 Description，`/ai skill add/list/remove/enable/disable/promote/info` 使用嵌套 `SubCommand` 定义
- **配置项新增** — `max_user_skills`（默认 10）、`max_user_skill_prompt_len`（默认 2000）

## v1.16.0 (2026-06-25)

### ✨ 新功能

- **支持 QQ 官方平台 `GROUP_MESSAGE_CREATE` 事件** — 新增 `GroupMessageCreate`、`GroupMemberAdd`、`GroupMemberRemove` 事件类型常量和解析逻辑
- **`MentionsEvent` 接口实现** — QQ 适配器解析消息中的 `mentions` 数组，暴露 `UserInfo.IsSelf` 标记
- **`OnMentionedBot()` / `OnMentionedBotOrNoMentions()` 规则** — 插件可通过规则控制是否响应 @ 机器人的消息，避免 @ 他人时误触发
- **消息日志 `message_mentions` 关联表** — 独立表存储 @ 用户信息，零序列化开销，支持批量查询

### 🔧 修复

- **展示名称使用 `author.username`** — `populateGroupAt` 中 `DisplayName` 从 `author.id`（OpenID）改为 `author.username`（用户昵称）
- **`GROUP_MESSAGE_CREATE` 的 `content` 剥离 `<@id>` 协议编码** — 还原用户实际发送的文本，使 `OnCommand`/`OnRegex` 等规则正常工作
- **`modelToEntry` 补全 `CreatedAt` 字段** — DB 查询返回正确的入库时间

### 🔄 变更

- 所有命令类插件（builtin 9 个 + cmd/bot 12 个，共 46 个注册点）添加 `OnMentionedBotOrNoMentions()` 保护

---

## v1.15.1 (2026-06-25)

### 🐛 修复

- **修改 `log.level` 等无关字段不再导致 Adapter 断连** — 平台热更新监听器仅在 `bot.*` 配置实际变化时触发
- **修改 `sampling_rate` 不再在固定采样模式下打印误报警告** — 仅当采样率值变化时才调用 `SetSamplingRate`
- **修改无关配置不再触发 logger 重初始化** — 仅当 `log.format/console/file/file_path` 变化时才调用 `logger.Init`
- **修改无关配置不再重复推送中间件更新** — retry/dedup/degradation/pprof 均带变化检测栅栏，跳过无操作调用
- **修复 `lastBotCfg` 数据竞争** — 并发 API 请求修改配置时 `lastBotCfg` 读写加锁保护

---

## v1.15.0 (2026-06-25)

### 🔥 全栈配置热更新

所有标记为 `[H]`（即时生效）和 `[H⚠]`（有条件生效）的配置字段现在支持运行时修改，**无需重启 Bot**：

- **中间件结构开关** — `middleware.recover`、`logging`、`metrics` 等）改为运行时通过 `Bridge.GetMiddlewareConfig()` 检查，`config.yaml` 中开关变化即时生效
- **白名单热读** — `auth.whitelist` 增删用户即时生效（无需重启）
- **日志级别/时间格式** — `log.level` / `time_format` 修改后即时生效
- **日志输出格式** — `log.format` / `console` / `file` / `file_path` 支持热更新（自动关旧文件、建新 writer）
- **重试配置** — `retry.*` 全部支持热更新
- **限流/去重参数** — `rate_limit.burst`、`dedup.max_size` / `default_ttl` 修改后即时生效
- **慢处理监测** — `slow_handler.enable` / `threshold` 每次请求热读
- **反压** — `backpressure.limit>0` 启用，`<=0` 关闭（limit 值本身因固定 semaphore 保持创建时值）
- **自适应降级** — `degradation.*`（除 `recovery_interval` / `delay_queue_size`）下一监控周期生效，`monitor_interval` 支持 ticker 重建，`strategy` 运行时读取
- **分布式追踪** — `tracing.include_event_detail` 运行时开关；`sampling_rate`（仅自适应模式）即时生效
- **pprof 参数** — `auto_profile` / `profile_interval` / `profile_duration` / `enable_mutex` / `enable_block` 运行时生效

### 🔄 平台适配器热替换（SyncPlatforms）

`Bot.SyncPlatforms(desired map[string]Adapter)` 支持运行时**增、删、改**平台适配器：

- **增** — `registry.Register()` + `Start()`，其他平台不受影响
- **删** — `registry.Remove()` + `Stop()`，平滑断开连接
- **改** — `registry.Replace()` + Stop 旧 + Start 新，连接零停机切换
- 重建 `adapterSnapshot` 原子替换，热路径零锁读取
- `Bot.Stop()` 自动关闭所有热替换的适配器

### 🧩 模块化重构

- **`cmd/bot` 拆分为独立 Go 模块** — 创建 `cmd/bot/go.mod`（module `github.com/KomeiDiSanXian/remilia/cmd/bot`），插件重型依赖隔离在子模块
- **`go.work`** 增加 `cmd/bot` 和 `examples/httpclient-demo`
- **提取单平台工厂函数** — `platformFactories()` / `buildDesiredAdapters()` / `registerPlatforms()` 供热更新 listener 复用
- **`config.example.yaml` 热更新标注** — 每字段 `[H]` / `[H⚠]` / `[R]` 标记

### 🧪 测试

- **新增 6 个测试文件/函数**：Bot.SyncPlatforms（5 测试）、AdaptiveSampler（6 测试）、Bridge 扩展、Degradation MonitorInterval/Strategy、SetIncludeEventDetail、Logger SetLevel/SetTimeFormat、Pprof UpdateConfig
- **pprof 测试改用动态端口** — `net.Listen("127.0.0.1:0")` 消除并行端口冲突
- **修复时序敏感断言** — engine shutdown / retry sleep 测试放宽并行下界
- **修复 zerolog 全局状态污染** — logger 测试 `saveZerologState` + `t.Cleanup`
- **`-race` 全量通过** — 91 个包零 data race 警告

### 📝 配置文档

- `config.example.yaml` 和 `cmd/bot/config.default.yaml` 完整标注每个字段的热更新能力

---

## v1.14.1 (2026-06-25)

### 🐛 修复

- **修复 `config.Watcher` 未启动导致热更新不生效** — `cmd/bot/main.go` 创建并启动 `config.NewWatcher("config.yaml")`
- **统一日志前缀** — `[bot]` → `[remilia]`（4 个文件）

---

## v1.14.0 (2026-06-25)

### 🏗️ 配置系统重构

- **配置结构重组** — 所有平台相关的配置归入 `bot.<platform>` 下：
  - `server` → `bot.qq.webhook`（`webhook` 和 `token_manager` 也已归入 `bot.qq`）
  - `concurrency` → `middleware.backpressure`（同时更名为 `backpressure`）
- **删除 BigCache 残留字段** — 移除 `WebhookConfig` 中 7 个死字段（`DedupEnable`、`Shards`、`LifeWindow`、`CleanWindow`、`MaxEntrySize`、`HardMaxCacheSize`、`MaxEntriesInWindow`）
- **删除 `ServerConfig` 类型** — `Host`/`Port`/`ShutdownTimeout` 合并到 `WebhookConfig`
- **统一配置默认值模式** — 所有平台 Config 使用指针接收者 `setDefaults()`，在构造函数中调用
- **`${VAR}` 环境变量替换支持** — `loadRaw()` 中加入 `os.ExpandEnv`，YAML 中的 `${VAR}` 语法现在可用
- **修复 `getEnvInt` 解析失败回退问题** — 非数值环境变量不再返回 0，而是回退到默认值

### 🎯 中间件配置化

- **配置驱动中间件注册** — `Recover`/`Logging`/`Metrics`/`Auth`/`Dedup`/`Backpressure`/`SlowHandler` 现在根据配置的 `enable` 标志和参数注册，而非硬编码无条件注册
- **热更新增加 Enable 检查** — `RateLimit.Enable`/`Dedup.Enable`/`Degradation.Enable` 现在在热重载时被检查，禁用的中间件不再推送更新

### 🔧 平台层修复与增强

- **修复 `discord.InteractionsAdapter.Stop()` session 泄漏** — 未调用 `session.Close()`
- **修复 `discord.InteractionsAdapter` send-on-closed-channel 竞态** — 移除冗余的 eventCh 关闭
- **修复 `qq.Adapter.Start()` nil eventCh 静默成功** — 改为返回错误
- **统一 `SendError` 包装** — 所有 5 个平台的 `Send()` 现在都返回 `platform.SendError` 结构化错误
- **QQ 平台补齐 `ReactionSender` + `MessageDeleter`** — 通过 OpenAPI 实现表情表态和消息撤回
- **`safeInvoke`/`safeDispatch` 去重** — 移除 4 个平台中相同的包装函数，直接调用 `platform.SafeDispatch`
- **移除未使用的字段** — `qq.Adapter.wg`、`discord.GatewayAdapter.ctx`

### 🔄 命名变更

- `middleware.ConcurrencyLimit()` → `middleware.Backpressure()`
- `middleware.ConcurrencyPolicy` → `middleware.BackpressurePolicy`
- `ConcurrencyDrop/Block/TryWait` → `BackpressureDrop/Block/TryWait`
- `config.ConcurrencyConfig` → `config.BackpressureConfig`
- `config.TokenConfig` → `config.TokenManagerConfig`

### 🧹 其他清理

- 清除所有 YAML 配置文件中的 `vX.Y.Z+` 版本标记
- 同步 `config.default.yaml` 与 `config.example.yaml`
- 更新所有示例和测试文件以匹配新配置结构

## v1.12.4 (2026-06-22)

### 🛡️ CI 稳定性修复

- **修复 `ExecProfile.ShouldPool` 数据竞争** — 栈分配固定数组替代共享 `snapshotBuf`，消除多 goroutine 并发排序的数据竞争
- **修复 `TestEngineShutdownWithPendingEvents` 时序依赖** — channel 同步替代 `assert.Eventually` 等待 goroutine 调度
- **修复 `TestHealthChecker` 超时** — `waitBotRunning`/`waitBotHealthy` 超时从 3s 提升至 10s
- **`addMatcher` 自动推导 `hasHandler`** — 直接设置 `Handler` 字段的场景自动标记 `hasHandler`
- **测试代码使用标准 API** — 统一使用 `Handle()` 而非直接赋值 `Handler` 字段

## v1.12.3 (2026-06-22)

### 🚀 性能优化

- **过滤无 Handler 匹配器** — `hasHandler` atomic 标记在 sortedCache 构建时排除无 Handler 匹配器，5K 匹配器场景吞吐量从 10K 提升至 3M msg/s
- **ExecProfile 预分配缓冲区** — `snapshotBuf` 复用消除热路径 `make()` 分配，GC 从 3181 次降至 5 次/10s
- **ExecProfile demoted 快速路径** — 已确认的快 Handler 跳过排序，`ShouldPool` CPU 占比从 20.54% 降至 1.48%

### 📝 文档

- 重写 README，更新特性列表和架构图
- 删除已归档设计文档和过时代码审查报告
- 修复文档中的 `logrus` → `zerolog` 引用
- 新增 `docs/05-performance/PERFORMANCE_REPORT.md`

### 🧪 Benchmark 修复

- 修复无限压力模式 semaphore 无效问题
- Drain 等待从 3s 扩展至 30s
- 延迟测量修正为 `time.Since(ev.Timestamp())`
- 添加 P50/P95/P99 百分位延迟统计

## v1.12.2

### 🛡️ 稳定性修复

- 修复所有 `-race` 检测到的数据竞争（platform/qq/adapter、plugin/infra_container、core/engine/exec_profile 等）
- 修复 bot 重启、健康探针、适配器生命周期超时
- 修复 sidecar 二进制未正确 gitignore

### ✨ 新功能

- 管理 API：配置深拷贝、健康检查调优
- Dashboard 全面 UI 重构：侧边栏、Toast、骨架屏、表单、权限管理
- Tauri 桌面壳：启动选择、按需 sidecar

### 🔧 其他

- 更新 GitHub Actions 到最新大版本
- 修复 golang.org/x/net CVE

## v1.12.1

### 🐛 修复

- 为 scheduler、admin、pluginctrl、css、coc、dnd 等插件添加 DryRun 副作用保护

## v1.12.0

### ✨ 新功能

- 新增插件：RPG 骰子系统、COC 7th 规则、D&D 5e 规则、BiliBili 客户端增强
- Bangumi API 客户端添加自定义 DNS 和代理支持
- Minecraft 服务器状态查询支持 mcsrvstat.us 回退
- 所有社区插件添加命令定义（Description/Usage/Examples）
- ping、status、info 等内置命令添加定义

### ⚡ 优化

- MergeIter + tempManager RCU 重构
- 无中间件的 handler 缓存到 compiledHandlers

## v1.11.0

### ✨ 新功能

- 多模态输入支持（图片、音频附件）
- AI 工具分类管理

### 🛡️ 安全修复

- 修复 AI 插件 SSRF 漏洞
- 修复多个数据竞争问题
- 修复 goroutine 泄漏和 double body close

### 🐛 修复

- 插件注册超时处理改进
- 多项插件 panic 和数据正确性修复

## v1.10.x

### v1.10.3

- 改进 OEM 数据刷新逻辑，添加签名头验证

### v1.10.2

- 增强 Bot 上下文（botName），改进系统提示词构建

### v1.10.1

- 优化循环和字符串拼接（client、image 模块）

### v1.10.0

- **统一 HealthNode 树模型**：summary/full 视图、kind 推导、增加 godoc

## v1.9.0

### ✨ 新功能

- **健康检查全面增强**：APIProbe headers/acceptStatus/MaxSeverity、HealthDetailer 接口、分组响应、adapter/token 健康详情
- AI 技能系统：注册、执行、自动发现
- 新增插件：CSS、ISS、Weather（含 API Probe 健康检查）

### 🐛 修复

- AI GORM session 存储在 DryRun 期间跳过（避免 nil DB panic）

## v1.8.0

### ✨ 新功能

- **AI 插件工具调用**：子命令重构、工具超时、统计追踪
- **Skill 系统**：注册和执行框架
- **自动发现**：SkillProvider 自动发现
- AI 工具：ACL 检查、反垃圾状态、审计日志查询
- `ProcessPlatformEventSync` 支持

## v1.7.0

### 🔧 重构

- **Phase 0 插件管理器重构**：Manager 拆分、注册统一、Scope 清理
- Service[T] 直接返回 T（不再返回 error），锁争用优化，ExportAs 弃用
- 替换所有已弃用的 RegisterMultiple/Smart/Atomic 调用

### ✨ 新功能

- **AI 聊天插件**：多供应商支持、工具调用
- 插件容器启动后冻结

## v1.6.x

### v1.6.5

- DryRun Logger 在依赖推断期间抑制插件 INFO 日志

### v1.6.4

- DryRun SetupContext 提供真实 goroutineMgr，无需插件检查 ctx.DryRun

### v1.6.3

- 为 scheduler/sendqueue/subscription 添加 DryRun 保护

### v1.6.2

- nil Matcher 作为 noop 处理，防止 DryRun nil 指针解引用

### v1.6.1

- noopRegistryWriter 返回 noopMatcher 避免 DryRun panic
- 替换 reflect.TypeOf 为 reflect.TypeFor
- Makefile Windows 兼容

### v1.6.0

- **三色 DryRun 依赖推断**：类型解析、循环检测、计时日志
- WASM 插件配置管理和生命周期集成
- 依赖注入容器添加值变更通知
- 蓝绿部署 draining 追踪
- EventBus 和 DI 上下文支持

## v1.5.0

### ✨ 新功能

- **WASM 插件运行时（ABI v2）**：wazero 沙箱、TLV 序列化
- 多语言插件支持：TinyGo、Rust、C
- 限流/超时/安全约束沙箱
- 35 个集成测试
- Showcase 7 个 WASM 命令演示
- 跨语言插件开发文档

### 🔧 其他

- QQ 平台 Markdown/ARK 模板消息
- 被动回复限制和过期
- 追踪和 Metrics 中间件集成
- Ping 插件

## v1.4.0

### 🔧 重构

- 中间件拆分为子包
- 插件文件标准化命名

### ✨ 新功能

- pprof 性能分析配置和验证
- Superadmin 角色和权限增强
- LevelDB 数据持久化迁移
- DryRun 模式跳过 I/O 操作
- CircuitBreaker/DedupFilter 状态持久化
- SQLite 消息日志
- Kubernetes 部署配置

## v1.3.x

### v1.3.4

- 删除已弃用的 MustAs/TryAs/GetPlugin
- 取消导出 Get/MustGet
- Service/TryService 自动追踪

### v1.3.3

- 删除 plugin.Must/Try
- 标记 Get/MustGet 为内部使用

### v1.3.2

- 弃用 legacy Must/Try/Get/MustGet
- 推荐 Service/TryService 和 Scope.Subscribe

### v1.3.1

- PluginScope 资源追踪
- ServiceProxy 防过期依赖
- 状态迁移管线

### v1.3.0

- **Matcher 级 per-channel 阻塞**替代 Per-Channel Engine

## v1.2.x

### v1.2.6

- Showcase 拆分为 8 个文件

### v1.2.5

- Router 使用优先级排序规则 + Handle 回调

### v1.2.4

- FSM 作为内置 Router 优先级，非策略规则

### v1.2.3

- 移除 builtin/conversation（由 core/fsm 替代）

### v1.2.2

- FSM 生命周期修复
- i18n 持久化修复
- 数据竞争修复

### v1.2.1

- Router + EngineManager 组合
- 共享 ExecPool
- Showcase FSM 演示

### v1.2.0

- **FSM 有限状态机引擎**
- **Adaptive Router 策略路由**
- **WASM 跨语言插件**
- **Per-Channel Engine**
- LevelDB 键值存储
- 自动回复插件
- 命令前缀自定义

## v1.1.0 (2026-05-07)

### 🛡️ 稳定性与正确性修复

- ExecProfile 懒初始化数据竞争修复
- Context matcher 跨 goroutine 竞争修复
- Matcher.GetSource 锁缺失修复
- ExecPool Drain/Submit 竞争窗口修复
- regexCache 数据竞争修复
- 熔断器配置竞争修复
- SimpleDedup goroutine 泄漏修复
- LRU 驱逐 bug 修复
- AI 插件文本编码修复
- 超长堆栈截断修复

## v1.0.0

### 🎉 初始发布

Remilia 是一个现代化、高性能、易于扩展的多平台聊天机器人框架。

#### ⚠️ 注意事项

- **Telegram 和 WeChat 适配器**为骨架实现，暂不可用于生产环境
- 要求 Go 1.26+
- 许可证：MIT
