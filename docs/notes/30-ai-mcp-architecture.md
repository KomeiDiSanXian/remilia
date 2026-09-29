# 30 — AI 插件接入 MCP 的架构评审

> 前置阅读：[26 — AI 插件分层](26-ai-layering.md)、[27 — 冻结清单](27-ai-freeze-checklist.md)、
> [29 — 分包边界](29-ai-package-boundaries.md)。
>
> 本文回答一个问题：**当 AI 插件要接入 MCP（Model Context Protocol）时，
> 现有架构会在哪些地方被顶穿，哪些抽象必须现在确定，哪些改动可以延后。**
> 全文按九个问题展开，每题给"问题本质 / 影响范围 / 可选方案 / 推荐方案 /
> 与现有设计原则的契合度"，最后一节给总体结论与接入分期。

## 零、结论摘要（TL;DR）

一句话：**MCP 在抽象上确实是"又一个 ToolProvider"，但在实现上是一个自带
传输、协议与进程生命周期的子系统；架构上必须把它当作"动态目录来源"，
而不是"更多 Tool"。**

必须先确定（现在就要定型，否则后面每一步都会返工）：

1. **身份模型**：内部引入 `ActionID{Source, Name}`；`Tool.Name` 退役为
   "模型函数名（model function name）"这一展示层的可重命名投影。全局
   `map[string]Tool` 不能继续兼任身份键（见 §3）。
2. **目录生命周期**：把 Tool 目录正式建模为**不可变、可版本化的
   `CatalogSnapshot`（带 `Generation`）**；`Tool` 本身保持不可变值类型；
   Provider 可以动态，快照不可变，二者由 CatalogManager 单点衔接（见 §1、§9）。
3. **可用性语义**：在目录层把可用性建模为**显式状态机**（而非布尔），区分
   "策略禁止（Hard，立即收缩）"与"暂时不可用（Soft，保留集合、调用时报错）"；
   否则 `StabilizeToolSet` 的 "available 立即收缩"会把一次断连放大成整段
   前缀缓存失效（见 §2）。
4. **安全边界归属**：策略由 Remilia 的 `ActionPolicy` 定义，MCP 的
   annotations/hints 只作为**输入建议**；所有 MCP Tool 必须经
   `Selection → ActionPolicy → Approval/Permission → Invoker`，禁止任何
   "AI client 直连 MCP"的旁路（见 §6）。
5. **结果容器**：结果模型要可扩展，但**不要把 `ActionResult` 定义成 MCP
   `CallResult` 的镜像**；由 MCP adapter 做 `CallResult → ActionResult` 转换，
   复用框架已有的 `platform.Attachment` / `protocol.ContentPart`（见 §5）。
6. **包与配置边界**：MCP 独立成子系统（`builtin/ai/mcp` 或独立插件），
   自持其配置与生命周期；AI 根只负责装配（见 §7、§8）。

可以延后：工具结果的多模态回灌（图/音进回上下文）、MCP 目录的
event-sourcing、HTTP/SSE 与 stdio 的完整矩阵、管理端 server UI、
指标/审批键全量迁移到 `ActionID`。§10 给出分期。

---

## 一、Tool 的静态假设与 Tool Catalog 生命周期

### 1.1 问题本质

现状把 Tool 当作**进程启动期一次性注册的静态资源**：

- `toolkit.ToolRegistry` 是一个按名索引的可变 `map[string]registeredAction`；
  `Register` 语义是"同名仅首次注册生效，后续静默忽略"，仅 `Remove` + `Register`
  用于"显式注册覆盖自动发现"（`catalogState.RegisterToolProvider` 正是这么做的）。
- `ToolProvider.ListTools() []Tool` 是**一次性 pull**：`RegisterToolProvider`
  在 Setup/`DiscoverToolProviders`（`FreezeContainer` 之后）遍历一次就结束，
  Provider 之后的变化框架完全不知道，也没有"下线/变更"概念。
- 目录本身没有版本号、没有来源、没有"这次目录是从哪些 Provider 聚合出来的"记录。

MCP 打破的正是这三点。MCP 的 Tool 集合会在这些场景下变化：

| 场景 | 变化 |
|------|------|
| `initialize` + `tools/list` 完成 | 首次出现 |
| Server 重启 | 名字可能全变（连接即新会话） |
| `notifications/tools/list_changed` | 增 / 删 / 改（同名改 schema 也算） |
| Server 下线 / 重连 | 暂时消失，随后可能以不同集合回来 |
| 用户启用 / 禁用某个 server | 批量增删 |

### 1.2 影响范围

- **注册表语义**：`first-wins` + 无下线，与"集合可变"直接冲突。
- **选择与稳定**：`decision.SelectToolsForTurn` 用 `cached.ToolCount == len(actions)`
  判断缓存是否可复用；`decision.StabilizeToolSet` 用 `available` 做子集/交集。
  目录颤动会连锁触发缓存失效与工具集抖动。
- **前缀缓存**：`tools` 是请求顶级字段，序列化字节一变，其后 system + history
  全部前缀失效（详见 §2）。
- **可观测性与审批**：指标 label、审批记录都按 tool name，重名/改名的语义会漂移。

### 1.3 可选方案

- **A. 保持可变注册表，加细粒度增删事件。** 改动最小；但没有版本、没有原子
  快照，选择/缓存/序列化会在"半更新"状态下读到不一致集合（并且 map 迭代序
  不稳定，需要额外排序）。**不推荐。**
- **B. 目录正式建模为不可变快照 + 代数（generation）。** Provider 的变化被
  CatalogManager 归一为"发布一个新快照"，下游只读快照。**推荐。**
- **C. 完整 event-sourcing 目录（保留每次变更事件流、可回溯）。** 对当前需求
  过重，没有真实消费者（违反不变量 9）。**延后。**

### 1.4 推荐方案

**落地方式：先拆职责，再谈命名——不要把 `toolkit.ToolRegistry` 直接改造成
一个超级 Manager。** 拆分的重点是把"聚合 Provider 与发布快照"从"兼容现有
注册 API"里**移出**，而不是给现有类型加一堆新方法。推荐的两层分工：

```
ToolRegistry（保留）
    └── 兼容现有注册 API：Register / Remove / 按名查找（冷路径，装配期与兼容层用）

CatalogState / CatalogManager（新增）
    └── 聚合各 ToolProvider → 物化 → 原子发布 CatalogSnapshot（带 Generation）
        独占：Provider 生命周期订阅、ModelName 分配、可用性归一、缓存失效联动
```

判据仍是 §9 的四角色"问句互不重叠"：如果 `ToolRegistry` 同时长出
`Register / Remove / Provider lifecycle / Snapshot / Generation / ModelName
allocation / Availability / Cache invalidation`，那就是把旧 `Tool` 的语义过载
复制到了注册表上——**又一个 God Object**。因此：

- `ToolRegistry` **只做"注册 / 查找"**，保持兼容；
- 聚合、物化、版本、可用性、名称分配**全部归 CatalogManager**；
- 二者通过"Manager 从 Registry/Provider 取料 → 发布快照"**单向**衔接，
  Selector/Invoker 只认快照。

在这些职责拆开、边界稳定之后，再决定是否重命名——**命名是最后一步，不是第一步。**

引入两个概念，且只在**目录层**引入：

```
CatalogSnapshot{                     // 不可变、带版本
    Generation uint64                // 每次内容变化 +1
    Entries    []CatalogEntry        // 稳定排序（按 ActionID 排序）
}
CatalogEntry{
    ID         ActionID              // {Source, Name} —— 身份（见 §3）
    Spec       toolkit.ActionSpec    // 模型可见描述
    Policy     toolkit.ActionPolicy  // 选择与安全策略
    ModelName  string                // 本轮交给模型的函数名（投影，见 §3）
    Revision   uint64 / hash         // 定义版本（schema/描述变化也升）
    State      Availability          // 见 §2
}
```

- **Tool 保持不可变**：`toolkit.Tool` 仍是值类型；快照里放的是从 Provider
  物化出来的动作视图（`Action` 已有 `Spec/Policy`，天然适合做快照条目）。
- **Provider 动态 ⇄ 快照不变**：Provider 是"可变的来源"（会 push 变化），
  快照是"某一时刻的物化投影"。**只有 CatalogManager 监听 Provider 变化，
  原子替换快照并 `Generation++`**。Selector / Decision / 缓存 / 协议序列化
  一律只读快照，完全不知道 stdio/HTTP/重连。
- CatalogManager 的消费者是装配侧（`catalogState` 所在），替换快照用
  与 `core/engine` 一致的 **COW + `atomic.Value`** 风格，读路径无锁。
- 下游比较缓存新鲜度只看 `Generation`，不看 Provider。

### 1.5 契合度

- **高。** COW + 不可变状态是本仓库的既有范式（`01-cow-engine`），把目录也
  做成 COW 快照是顺势而为。
- **不破坏**"Tool 是协议边界概念"（不变量 1）：快照条目仍是动作视图，
  执行载荷不在快照里。
- **不破坏**"插件根只负责装配与编排"：CatalogManager 是装配侧编排的，
  纯算法（打分/选择/稳定）仍在 `decision`。
- **注意**：`session.ToolSetState.Generation` 已经存在，但它是**会话级稳定集合**
  的代数，语义与"目录代数"不同。务必命名区分（如 `CatalogGeneration` 对
  `ToolSetGeneration`），否则两代混用会造成难以复现的缓存 bug。

---

## 二、ToolSet Cache / Prompt Cache 的稳定性

### 2.1 问题本质

现状对抖动已有两道闸（见 `prompt_cache_test.go`、`decision/stabilize.go`）：

1. **序列化稳定**：`ToolRegistry.List` / `Actions` 按名称升序输出，集合不变时
   字节序列稳定。
2. **会话级稳定集合**：`StabilizeToolSet` 做"滞回 + 单调并集 + 空闲衰减"，
   让工具集从"每轮可能变"收敛为"只在真需要新工具时变一次"。

MCP 引入的是**此前不存在的一类抖动**：不是"这一轮选没选中"，而是
**"可用集合本身在变"**。新来源：

| 抖源 | 对 `tools` 段的影响 |
|------|--------------------|
| Selector 查询漂移 | 已有：靠 sticky 抑制（上表第 2 条） |
| MCP Tool 增删 | **新增**：`available` 变化 → 工具集变化 → 整段前缀失效 |
| Server 重连 | **新增**：瞬时消失再回 → **可能每轮都在 `[] ↔ [...]` 之间跳** |
| Server 刷新（同名改 schema/描述） | **新增**：名字没变、**字节变了** → 现有"按名字判缓存"发现不了 |
| 用户启用/禁用 server | **新增**：批量变化 |

### 2.2 影响范围（含一个隐藏陷阱）

现有 `StabilizeToolSet` 有一句：

```go
prev = intersectNames(st.Names, avail)   // 可用集收缩立即生效
```

这条对 **RBAC/群策略** 是**正确且必须**的（不变量 5：权限收缩立即生效）。
但 MCP 的**瞬时断连**也会让 `avail` 收缩——于是"一次网络抖动"会立刻把历史
稳定集合裁掉，等重连后又把它加回去，正好击穿前缀缓存。**这是 MCP 接入后最
容易踩的坑：把"安全语义的立即收缩"误用到"可用性语义"上。**

另外，现有选择缓存的**身份**是隐式的：`SelectionCache{ToolCount, Names, At,
QueryTokens}`，复用条件是"`ToolCount` 相等 + TTL 内 + 查询 Jaccard ≥ 0.5"。
它只认**名字与数量**，认不出"同名但 schema 变了"。

### 2.3 可选方案

- **A. 不动。** MCP 抖动直接进选择/稳定层。**不推荐**：缓存命中率会被打回，
  且与不变量 3/5 冲突。
- **B. 缓存身份升级 + 可用性状态机 + 代数关联。** **推荐。**
- **C. 会话开始时冻结 MCP 工具集（快照钉死）。** 简单、前缀最稳，但会用到
  陈旧工具、无法响应 server 更新。可作为**降级开关**，不作为默认。

### 2.4 推荐方案

**(a) 缓存身份：从 `session` 扩展为 `session + catalog generation + query/session state`。**

- `SelectionCache` 增加 `CatalogGeneration` 字段；复用条件加一条
  `cached.CatalogGeneration == snapshot.Generation`。目录没变 → 老逻辑照旧；
  目录变了 → 立即重算（而不是靠 `ToolCount` 巧合判断）。
- `ToolSetState` 记录它是在哪个 `CatalogGeneration` 下收敛的。
- 对"同名改 schema"：目录条目的 `Revision`（schema+描述哈希）变化也要**升
  `Generation`**，让下游能感知"字节变了"。缓存键可从"名字集合"升级为
  "`ActionID` + `Revision` 集合"。

**(b) 可用性状态机：把"策略禁止"和"暂时不可用"显式分成多个状态。**

**（这是全文实现风险最高的设计选择，必须做成显式状态机，而不是一个
`Available bool`。）**

Soft Availability 的本质是"拿一次工具调用失败，换整段 `tools` 前缀不变"。
它天生会引入多个**来源不同、后果不同、恢复条件也不同**的状态；一旦压成一个
布尔，下面这些迟早会挤进同一个字段，然后靠补丁互相区分：

| 状态 | 来源 | 对集合的角色 | 恢复条件 | 升 `Generation`？ |
|------|------|--------------|----------|-------------------|
| `Ready` | 正常 | 可用 | — | — |
| `UnavailablePolicy`（Hard） | RBAC / 群策略 / 用户禁用 | **立即从 `available` 移除** | 策略放开 | 否（可用集过滤，非目录内容变化） |
| `UnavailableProvider`（Soft） | Provider 未加载 / 初始化失败 | 保留但标记 | Provider 注册成功 | 否 |
| `Disconnected`（Soft） | MCP server 断线 / 重连中 | 保留但标记 | 重连成功 | 否 |
| `ToolDisabled`（Soft） | 该 server 的 tools allow/deny 命中 | 保留但标记 | 配置变更 | 否 |
| `ToolRemoved` | `tools/list` 确已不含该工具 | **从目录移除** | 重新出现 | **是** |

要点：

- **区分"策略禁止"（Hard，立即收缩）与"暂时不可用"（Soft，保留集合）** 是
  这个状态机的唯一目的；`UnavailablePolicy` 必须保持不变量 5（权限收缩
  立即生效），其余 Soft 状态**不触发** `available` 交集收缩。
- **区分"被禁用"（配置意图，可逆）与"被移除"（目录事实）**：前者只改
  `Availability`、**不改快照内容**（避免 cache 抖动）；后者必须改快照并升
  `Generation`。这正是"重连不失效前缀缓存、但真删了就失效"的落点。
- **迁移要单向、可枚举、可观测**：每次 `Ready → Soft` / `Soft → Ready` /
  `* → Removed` 都应可打点，并显式声明"是否升 `CatalogGeneration`"。
- `StabilizeToolSet` 的 `available` 只对 **Hard** 做交集收缩；Soft 工具仍参与
  集合，被调用时由 Invoker 返回"provider 暂时不可用"的**类型化错误**
  （见 §5、§6），重连后原样恢复。
- 与 MCP 通知联动：`tools/list_changed` 后给新增工具一个"稳定窗口"再纳入
  `available`，避免 server 启动抖动期间集合反复变化。

> 落地约束：该状态机在目录层用**枚举 + 显式迁移函数**表达，并配一组
> "状态 × 是否升代 × 是否收缩 available"的契约测试；**禁止**退化成单个
> `bool` 或多个并列 `bool`。

**(c) 缓存结果与 Catalog 版本的关联方式。**

- 选择缓存 / 稳定状态都携带 `CatalogGeneration`，**只有代数相同才复用**。
- `Generation` 只在**内容真正变化**时升（增/删/改 schema），重连但集合不变
  时**不升**——这样重连本身不产生缓存失效。
- 观测：把"目录代数变化率"与既有 `ai_llm_tokens_total{type="prompt_cached"}`
  命中率并列，用于验证命中率是否被 MCP 拉低（现有 ~75.73% 是基线）。

### 2.5 契合度

- **高。** 直接服务于不变量 3（Decision 不破坏 ToolSet 缓存稳定性）与不变量 5
  （ToolSet ⊆ available，权限收缩立即生效）——关键是**把这两条拆开**
  应用到 Hard/Soft 两类可用性上。
- **中风险的取舍**：Soft 工具"留在集合里但调用时失败"会在一次真实调用上暴露
  不可用；这是**有意的**——用一次工具级错误换整段前缀缓存稳定，符合
  "稳定优先"的既有取向。

---

## 三、Tool 身份模型：Name / Model Function Name / Identity

### 3.1 问题本质

现状是**全局 `map[string]Tool`，Name 即身份**：

- `ToolRegistry.Register` 同名 first-wins；`RegisterToolProvider` 同名
  Remove+Register（覆盖）。**没有"两个来源的同名工具"这一概念，第二个会被
  静默覆盖或跳过。**
- MCP 的现实是：**不同 Server 之间重名是常态**（`search`、`fetch`、`list`、
  `get`），且 MCP 只保证"server 内唯一"，不保证全局唯一。
- 结果：`search` 到底是 SQL 的 `search` 还是 HTTP 的 `search`？工具结果回填、
  指标 label、审批记录、sticky 集合全都靠名字，无法区分。

### 3.2 三个概念必须拆开

| 概念 | 作用域 | 约束 | 现状 |
|------|--------|------|------|
| **Tool Name** | Provider/Server 内 | Provider 自定（可能含 `.`/`/`） | 直接当身份用 |
| **Model Function Name** | 单次请求的 `tools` 数组内全局唯一 | `^[a-zA-Z0-9_-]+$`，有长度上限（OpenAI 64） | `Tool.Name`，经 `SanitizeToolName` |
| **Tool Identity** | 框架全局稳定 | 与来源绑定、与展示解耦 | **不存在** |

身份应是 `ActionID{Source, Name}`，例如
`{Source: "mcp:github", Name: "search"}` 与 `{Source: "mcp:sqlite", Name:
"search"}` 是**两个**动作。`Source` 也是长期需要的一维：插件市场、远程
Provider、内置动作各自是一个命名空间，天然用它区分。

### 3.3 影响范围

- 路由：`executeToolResult` 目前按名字解析来源（Skill → 命令 → Execute）。
  有了多个 MCP server，必须**按 `ActionID.Source` 分发**到正确的
  `MCPInvoker`/连接。
- 指标：`recordToolCall(name, ...)`、`recordToolSet(...)` 都是 name label；
  重名会污染观测。
- 审批与权限：`decideToolInvocation` 按 name 取 `Action`；重名会取到错的那个。
- 缓存：`SelectionCache.Names` / `ToolSetState.Names` 都是 name。

### 3.4 可选方案

- **A. 维持 name 即身份，靠命名约定（如强制 `mcp_<server>_<tool>` 前缀）避重。**
  成本最低，但约定无强制力，且把"展示名"和"身份"继续绑死；长期不可维护。
- **B. 现在引入 `ActionID{Source, Name}` 作为内部身份，但保持 `Tool` API
  表面不变（`Tool.Name` 退化为默认 Model Function Name）。** **推荐。**
- **C. 现在重构 `Tool` API（加 ID 字段、要求 Provider 提供 ID）。** 破坏性大，
  且没有外部插件作者（当前仓库内 34 处消费者），但会牵动 26/27 冻结项。
  可拆成 B 的后续增量。

### 3.5 推荐方案（分层、渐进）

1. **定义 `ActionID{Source, Name}`**（放在 `toolkit`，因为它是工具契约的一部分）。
2. **注册表键从 `string` 升级为 `ActionID`**，另建一张
   `modelName → ActionID` 的投影索引（只在快照物化/序列化时生成）。
3. **Model Function Name 由 CatalogManager 在物化快照时分配**，规则：
   - 无冲突 → 直接用 `Name`；
   - 有冲突 → 用限定名（`<Source>_<Name>`，`Source` 先按 `SanitizeToolName`
     规范化）；
   - 仍超长 → 截断 + 短哈希后缀。
   - **分配必须确定**（同一 `Generation` 下稳定），否则会击穿前缀缓存。
4. **现有 `Tool.Name` 语义不变**：对老 Provider 而言 `Source` 缺省为
   "builtin" 或插件名，行为零变化；MCP adapter 负责填 `Source`。
5. **指标/审批/缓存键渐进迁移**：先加 `source` label（保持旧 label 不变，
   兼容），缓存键逐步改用 `ActionID`。

### 3.6 与长期兼容性（插件市场 / 远程 Provider）

- `Source` 就是"这个工具归谁"的稳定命名空间：市场里的每个插件、每个远程
  Provider、每个 MCP server 各占一个 `Source`。
- Model Function Name 是**可重命名投影**：它随同一次请求里"有哪些工具"
  动态决定，因此**不能作为持久身份的键**。现在把二者分开，未来接入市场/远程
  Provider 时无需再拆。
- `Revision`（定义版本）也挂在身份上，用于"同名改 schema"的缓存失效。

### 3.7 契合度

- **高。** 身份是"朝内"的概念（`toolkit`/目录层），Model Function Name 是
  协议层投影，符合不变量 1（Tool 是协议边界概念）。
- **与 27 冻结项的关系**：现有冻结用例断言的是**行为**（名字排序、字节稳定、
  必保集组成）。引入 `ActionID` 时若保持"单来源场景下 Model Function Name ==
  原 Name"，冻结用例可原样通过；多来源场景是新增行为，不构成回归。

---

## 四、Selector 与 MCP 动态性的边界

### 4.1 问题本质

MCP 的复杂性（连接、重连、stdio/HTTP、server 生命周期、`tools/list`）如果
泄漏进 Selector，Selector 就会退化成"又一个上帝模块"——这正是
`26-ai-layering.md` 反复警告的："不要先动 `select.go`"，否则只是换名重现旧
`Tool` 的语义过载。

### 4.2 论证

Selector 的职责是**"在本轮可用动作集合上做打分/选择/稳定/预算"**，它需要
知道的是：

- 有哪些动作（`ActionSpec`：名称、描述、类别、参数）；
- 它们的保留级别与策略（`ActionPolicy`：`Selection`、审批、权限）；
- 它们的可用性（显式状态机的 Hard/Soft 状态，见 §2）。

它**不需要也不应该**知道：

- 这个动作来自 stdio 还是 HTTP；
- 这个 server 现在是否在重连；
- `tools/list` 最近变过没有；
- MCP 协议版本、capabilities、cancellation。

**结论：MCP 的动态性必须被目录层"吸收"成一个快照**（§1），Selector 只认识
`CatalogSnapshot`。由此得到清晰分层：

```
Builtin / Plugin / Command / MCP        （来源，各自可变）
        │  实现 ToolProvider / 内置目录
        ▼
ToolProvider → CatalogManager → CatalogSnapshot   （物化 + 版本化）
        │
        ▼
Selector (decision)                     （只读快照：打分/选择/稳定/预算）
        │
        ▼
Invoker (runtime)                       （执行：Func/Command/Skill/MCP）
```

Selector 里唯一新增的、与 MCP 沾边的概念，是**可用性的两分**——而它读到的
也只是 `Action.State` 这一**目录属性**，不是"MCP 状态"。这正是"分层"与
"泄漏"的分界线。

### 4.3 影响范围

- 若 Selector 感知 MCP：`decision` 会被迫 import mcp/transport 类型，
  违反"子包依赖朝内"（`decision` 目前只依赖 retrieval/session/textutil/toolkit）。
- 若 Selector 不变（推荐）：MCP 的变化全部止步于目录层，`decision` 零改动，
  冻结项（A1–A13、E1–E5）保持绿。

### 4.4 契合度

- **高。** 完全符合 `29` 的依赖方向与 `26` 的"不要先动 select.go"。
- 关键词：**Selector 认识"目录"，不认识"MCP"。**

---

## 五、结果模型的丰富性

### 5.1 问题本质

现状结果是**偏 Text + Err**：

- `toolkit.Tool.Execute` 签名是 `func(ctx, args) (string, error)`；
- 执行结果类型 `runtime.ActionResult{ Text string; Err error }`；
- 回填给模型的 `RoleTool` 消息是 `runtime.TruncateToolResult(result)`——**一个字符串**。

MCP `tools/call` 的返回要丰富得多：

| MCP 内容 | 含义 | 现状是否能表达 |
|----------|------|----------------|
| `content[].type=text` | 文本 | ✅（Text） |
| `content[].type=image` | 图片（base64/uri） | ❌ 会丢 |
| `content[].type=audio` | 音频 | ❌ 会丢 |
| `content[].type=resource` / `resource_link` | 资源/链接 | ❌ 会丢 |
| `structuredContent` | 结构化 JSON | ❌ 被 flatten |
| `isError` | 工具级错误 | 半（靠 `Err` 约定） |

直接 flatten 成 string 会**信息损失**：模型看不到图、结构化数据退化成
"看起来像 JSON 的文本"、资源链接丢失。

### 5.2 影响范围

- 结果回填路径（`process.go` 的 `RoleTool` 组装、`TruncateToolResult`）。
- Skill 子循环里的结果处理（`executeSkillTool` 也是 string）。
- 如果要让"工具返回的图片"进入下一轮模型输入，还需打通多模态输入侧
  （`protocol.Message.ContentParts`，已有 `text/image/audio`）。

### 5.3 可选方案

- **A. 维持 Text-only，MCP adapter 把 `CallResult` 拼成字符串。** 简单，但有损；
  对图片/结构化结果不可用。
- **B. `ActionResult` 扩展为 Remilia 自定义的通用结果模型，MCP adapter 负责
  转换。** **推荐的目标形态。**
- **C. 让 `ActionResult` 直接等价于 MCP `CallResult`。** **反对**：会把框架
  结果契约绑死到 MCP 语义上，`FuncInvoker`/`CommandInvoker`/`SkillInvoker`
  都要迁就 MCP 的字段形状；而且框架已有自己的媒体模型
  （`platform.Attachment`）与输入内容模型（`protocol.ContentPart`），
  再引入一套 MCP 原生类型是重复建模。

### 5.4 推荐方案（现在做 / 延后做）

**现在（最小、可扩展）：**

- 把 `ActionResult` 定义为**可扩展容器**，但保持向后兼容：
  - 保留 `Text`（纯文本路径不变，四路 Invoker 默认只填 `Text`）；
  - 保留 `Err`（`isError` 归一到 `Err`，文本仍照常回填）；
  - **新增可选的 Parts 字段**（如 `Parts []ResultPart`），`ResultPart` 复用
    `protocol.ContentPart` 的 `text/image/audio` 语义，并补 `resource`/结构化
    两种类型；
  - 新增可选 `Attachments []platform.Attachment`（媒体直接走框架既有附件通道）。
- **MCP adapter 是唯一的转换点**：`mcp.CallResult → runtime.ActionResult`
  （text → `Text`；image/audio → `Parts`/`Attachments`；resource → `Parts`；
  structuredContent → `Parts`/`Text` 的 JSON 投影；`isError → Err`）。
- 回填策略（`RoleTool` 内容）先保持"以 `Text` 为准"，对其他 Parts 做**保守
  降级**（如文本化 + 附件随最终回复发送），**不发散**到"每轮都把工具图塞回模型"。

**延后：**

- 把工具返回的图/音**回灌**成下一轮 `Message.ContentParts`（需要真实消费者，
  且要考虑 token 预算与 provider 支持差异）。
- 结构化结果的专门富化渲染。

### 5.5 契合度

- **目标形态 B 契合度高**：保持"What/How 分离"（不变量 11）与"MCP 语义不外泄"；
  `ActionResult` 是插件内部契约，不是 MCP 的镜像。
- **反对 C** 的理由也与不变量 1 一致：MCP 是协议边界概念，不应统治内部结果模型。
- 复用 `platform.Attachment` / `protocol.ContentPart` 避免重复建模，符合
  "不依赖 AI Runtime 内部状态、边界稳定"的既有取向。

---

## 六、安全模型：MCP 让 AI 获得间接外部执行能力

### 6.1 问题本质

MCP 的两种传输本质上都是"把外部能力接进 AI"：

- **stdio**：启动外部进程（`command`/`args`/`env`/`cwd`），等于让 AI 间接
  拿到**本机进程执行能力**；
- **HTTP/Streamable HTTP/SSE**：请求外部服务器，等于让 AI 间接拿到**任意外呼
  能力**（含 SSRF 面）。

因此**"AI 不能绕过权限系统"是接入 MCP 的第一硬约束**。

### 6.2 必须维持的流程

MCP Tool 必须与内置/插件/命令工具走同一条管线，**禁止 AI 经 MCP client 直接
`tools/call`**：

```
Selection(暴露哪些) → ActionPolicy(审批/权限/保留级别) → Approval/Permission(闸门)
        → Invoker(执行：MCPInvoker 调 tools/call)
```

对应到现有代码，这意味着：

- MCP Tool 必须登记进**同一份目录/注册表**，从而自动享受：
  - `actionCandidates` 的群策略过滤（`FilterTools`）与 RBAC 过滤
    （`filterToolsByPermission`）；
  - `decideToolInvocation` 的**唯一策略闸门**（先 RBAC，后审批）；
  - `StabilizeToolSet` 的 `available` 收缩（Hard 可用性，见 §2）。
- 执行侧只能新增一个 **`MCPInvoker`**（与 `FuncInvoker`/`CommandInvoker`/
  `SkillInvoker` 并列），由 `executeToolResult` 按 `ActionID.Source` 分发到它。
  **没有第二条调用路径。**
- MCP 的 tool annotations（如 `readOnlyHint`/`destructiveHint`）只能作为
  **策略推导的输入建议**，**不能作为策略本身**：最终 `RequiresApproval`/
  `AlwaysRequireApproval`/`Permissions` 由 Remilia 配置与 `ActionPolicy` 决定。

### 6.3 MCP adapter / security 层必须覆盖的问题清单

| 类别 | 必须处理 |
|------|----------|
| **Server 准入** | 该 server 是否允许启动/连接（allowlist + 可选的显式启用）；默认禁用、需显式开启 |
| **stdio** | `command` 白名单（拒绝任意命令/参数注入）；`args` 校验；`env` 白名单（禁止把整个宿主环境透传，尤其密钥）；`cwd` 限制；进程隔离与随插件生命周期回收；崩溃/重启策略；**Windows 进程树处理**（job object / 优雅 kill 子树）；启动与调用超时；stdout/stderr 大小上限与背压 |
| **HTTP** | URL 白名单；**SSRF 防护**（禁止私网/回环/链路本地/云元数据 `169.254.169.254`，处理 DNS rebinding）；重定向策略；连接/请求超时；不可用时的降级 |
| **TLS** | 强制校验（禁止 `InsecureSkipVerify`）；可选自定义 CA；证书错误不得静默忽略 |
| **Auth / 凭据** | Bearer/Basic/自定义 header；凭据走 `${ENV}` 间接引用（复用现有 `${AI_API_KEY}` 约定），不落明文；避免凭据进入日志/审批摘要 |
| **审批** | destructive/执行类 MCP Tool 默认 `RequiresApproval`；跨会话影响类默认 `AlwaysRequireApproval`；审批摘要脱敏 |
| **不可信输入** | server 返回的 tool 描述/schema/结果均视为**不可信**：描述可能诱导模型（prompt injection）；结果只能作为"工具输出"而非"指令"进入上下文；schema 需校验/规范化后再序列化给模型 |
| **版本与取消** | 协议版本协商失败要明确报错；调用取消/超时要收敛到 `Err`，不能挂死 |

**核心立场：安全边界由 Remilia 定义，不由 MCP 定义。** MCP server 是**不可信
对端**，其声明的一切（能力、注解、权限）都是建议。

### 6.4 契合度

- **最高。** 这正是"AI 不能绕过权限系统"的落地；`catalog.IsCommandSafeForAI`
  的"只自动发现无权限命令"、`decideToolInvocation` 的单一闸门、
  `filterToolsByPermission` 的纵深防御，都是同一条原则的既有实现。
- **一个安全取向提醒**：MCP Tool 默认应是 `SelectionOptional` +
  `RequiresApproval`，**绝不能默认 `Baseline`/`general`**。否则会同时造成
  两个问题：(1) 未标注类别的工具线性膨胀必保集（27 的 A3），让
  `ToolSelectMax` 失效；(2) 把外部不可信能力变成"恒被选中"。

---

## 七、MCP Provider 的复杂度与包划分

### 7.1 问题本质

抽象上 `MCP → ToolProvider` 很自然，但**实现面极宽**：

- **stdio**：进程启动、stdin/stdout JSON-RPC 分帧、stderr 处理、崩溃检测、
  重启、优雅关闭、**Windows 进程处理**、环境变量、工作目录、超时。
- **HTTP / Streamable HTTP / SSE**：连接、会话 id、鉴权、流式读取、重连、
  超时、不可用状态。
- **MCP 协议**：`initialize`、capabilities 协商、版本协商、`tools/list`、
  `tools/call`、`notifications`（含 `tools/list_changed`）、errors、cancellation、
  progress。

把这一切堆进 `builtin/ai` 根包或 `toolkit`，会立刻违反"子包依赖朝内"
（例如 `decision` 不得依赖 mcp）并把 God Object 重新养大。

### 7.2 影响范围

- 依赖方向：MCP 必须依赖 `toolkit`（产出 `Tool`/动作），但**不得**被
  `decision`/`runtime`/`execution` 依赖。
- 生命周期：stdio 进程必须绑定插件生命周期（`ctx.Spawn` + `lifecycleCtx`，
  见 `plugin.go` 的 Setup/Teardown 模式），否则进程泄漏。
- 测试：传输层需要独立可测（mock server / 子进程 fixture）。

### 7.3 可选方案

- **A. 继续在 AI core 堆叠。** 把 MCP 传输/协议写进 `builtin/ai`。
  **反对**：直接破坏分层与依赖方向。
- **B. 独立为 `builtin/ai/mcp` 适配包（作为 AI 插件的子系统）。** **推荐其一。**
- **C. 独立为**插件**（自带 Setup/Teardown/config），通过既有
  `DiscoverToolProviders` 被 AI 自动发现。** **推荐其二（更彻底）。**

### 7.4 推荐方案

**首选 C，退而求其次 B。** 关键判据是"配置与生命周期归谁"（与 §8 联动）：

- 若 MCP 是**独立插件**：它自持 `Setup/Teardown`、自己的配置节、自己的
  server 生命周期；它实现 `toolkit.ToolProvider` 并把服务注册进容器。
  AI 侧**零改动**即可通过既有的 `catalogState.DiscoverToolProviders(mgr)`
  自动发现——这是现成的扩展点（`discovery.go`）。
- 若 MCP 必须随 AI 插件内置：放在 `builtin/ai/mcp`，但**内部仍按子系统组织**
  （`transport/`、`jsonrpc/`、`session/`、`catalog adapter`），并在 AI 根只做
  "构造 + 注册 ToolProvider"的装配。

内部分层建议：

```
builtin/ai/mcp/
    transport/     stdio、http、sse 的实现（可替换、可单测）
    jsonrpc/       MCP 报文编解码与协议状态机（initialize/version/capabilities）
    conn/          单连接/会话管理（重连、超时、取消、tools 缓存）
    provider.go    ToolProvider 适配：把连接里的工具物化为 toolkit.Tool（带 Source）
    config.go      自持配置（见 §8）
    security.go    准入、SSRF/TLS、凭据、命令/环境白名单
```

**执行侧**新增 `runtime.MCPInvoker`（或 `builtin/ai/mcp.Invoker` 由 runtime
按端口调用），与现有三种 Invoker 并列，经 `executeToolResult` 分发。

### 7.5 契合度

- **高。** 独立包/插件保证 `decision`/`runtime`/`execution` 不依赖 MCP，
  符合"子包依赖朝内"与"插件根只负责装配与编排"。
- **依赖方向**：`mcp → toolkit`（+ 传输/协议自身依赖），`ai → mcp` **仅限
  装配点**（若走插件方案则连这一条也不需要）。

---

## 八、配置系统膨胀

### 8.1 问题本质

`builtin/ai/config` 已经是**大配置**：`config.Config` 现有约 **75 个 yaml 键**，
且它是**叶子包**，只有 `ai` 根引用它；子包各自声明最小参数结构，由
`builtin/ai/options.go` 做**唯一一次**"插件配置 → 子包参数"映射。这是 29 §十一
明确的设计。

MCP 的配置面同样宽：`transport`、`command`、`args`、`env`、`cwd`、`url`、
`headers`、`timeout`、`reconnect`、`tools`（allow/deny）、`disabled`、
缺省审批策略……如果全部塞进 `Config`，会：

- 把 `config` 从"AI 的配置"变成"AI + 传输 + 协议 + 安全的大杂烩"；
- 违背"`config` 是 leaf，`ai` 只负责 assembly"（每个 MCP 字段都得穿一遍
  `options.go`）；
- 让 MCP 的演进（新增 transport、协议版本）反复改动 AI 的配置契约。

### 8.2 可选方案

- **A. 全塞进 `ai/config`。** **反对**：直接造成上述冲突。
- **B. MCP 包自持 `mcp.Config`，AI 只传一个最小引用/开关。** **推荐。**
- **C. MCP 作为独立插件，完全自持配置节（如 `plugins.mcp`），AI 不持有任何
  MCP 配置。** **更推荐（与 §7 的首选一致）。**

### 8.3 推荐方案

- **MCP 自持其配置类型与加载**（`transport/command/args/env/url/headers/
  timeout/reconnect/tools/disabled/审批缺省`全部留在 `mcp.Config`）。
- **AI 侧最多保留一个开关**（如 `mcp.enabled`）或**一个已构造好的
  `ToolProvider` 句柄**，绝不持有 server 拓扑。
- 若走**独立插件方案**：MCP 在自身 `Setup` 读自己的配置节，向容器注册服务；
  AI 通过 `DiscoverToolProviders` 发现，**配置零耦合**。这与 `storage`、
  `permission`、`messagelog` 等既有插件的自持配置模式完全一致。
- 安全配置（allowlist、SSRF、TLS、凭据）同样归属 MCP 的安全层（§6），
  不进 `ai/config`。

### 8.4 契合度

- **高。** 直接守住"`config` 是 leaf，`ai` 只负责 assembly"：子包不读整份配置、
  配置字段只在自己 owner 内出现，新增量通过"新 owner 的新配置"引入，而不是
  往 `ai/config` 里再堆一层。

---

## 九、ToolProvider 职责边界

### 9.1 四个角色的定义

| 角色 | 回答的问题 | 输入 | 输出 | 谁实现 |
|------|-----------|------|------|--------|
| **ToolProvider** | 我能提供什么 | 自身来源状态（连接/文件/命令表） | `[]Tool`（带 `Source`） | 内置目录 / 插件 / **MCP** |
| **CatalogManager / Registry** | 当前系统有哪些 Tool | 各 Provider 的物化结果 + 变化事件 | 不可变 `CatalogSnapshot`（`Generation`） | 装配侧（`catalogState` 所在层） |
| **Selector** | 本次应选择哪些 Tool | 快照 + 本轮 query + 会话状态 | 选中动作子集 | `decision` |
| **Invoker** | 如何执行 Tool | 一次动作调用 + 已放行的策略结论 | `ActionResult` | `runtime`（Func/Command/Skill/**MCP**） |

### 9.2 现状与边界对齐中的命名问题

- 现有 `toolkit.ToolRegistry` 既当"来源聚合"又当"查找表"。**不要把
  `ToolRegistry` 直接重命名/改造成一个超级 Manager**——那会长出
  `Register / Remove / Provider lifecycle / Snapshot / Generation / ModelName
  allocation / Availability / Cache invalidation` 一整套方法，复刻出一个新的
  God Object。正确做法是**先拆职责，再谈命名**：
  - `ToolRegistry` **保留**，只做"注册 / 查找"的兼容 API（装配期与兼容层用）；
  - 新增 `CatalogState / CatalogManager`，负责**聚合各 `ToolProvider` → 物化 →
    原子发布 `CatalogSnapshot`**，并**独占** Provider 生命周期订阅、`Generation`、
    ModelName 分配、可用性归一与缓存失效联动；
  - 二者**单向**衔接（Manager 取料 → 发布快照），而"来源"语义归还给
    `ToolProvider`。
  命名是最后一步，不是第一步。
- 现有 `builtin/ai/catalog` 包名容易误解：它其实是**内置动作目录 + 能力端口 +
  命令发现**（`catalog.Build*Tools`、`Discover`、`Capabilities`），**不是**运行时
  的 CatalogManager。建议在文档/命名上把"内置动作目录"与"运行时目录管理器"
  明确区分（必要时给后者起名 `catalogmgr` 或 `snapshot`），避免"MCP 往 catalog
  里塞"的误判。
- `ToolProvider.ListTools()` 现在是**一次性 pull**；要支持动态来源，它必须
  变成"可订阅/可查询当前集合"的来源，或由 CatalogManager 负责轮询/订阅
  （推荐后者：Provider 只暴露"当前集合 + 变化通知"，Manager 负责物化）。

### 9.3 职责落位的要点

- **Provider 不产出"当前系统有哪些"**——那是 Manager 的聚合结果；Provider 只
  描述**自己**能提供什么。
- **Selector 只读快照**，不读 Provider，不读 MCP（见 §4）。
- **Invoker 只在策略闸门之后被构造**（与 `executeToolResult` 现在"先 `decideToolInvocation`
  再 Invoker"的顺序一致）；MCP 不得在 Invoker 里自行决定是否调用。
- **来源解析按 `ActionID.Source` 分发**（不再是"按名字猜"）：Skill → Command →
  Func → **MCP**，各来源一个 Invoker。

### 9.4 契合度

- **高。** 这是把 26 §3.2（Capability ≠ Action）与 §3.11（What/How 分离）
  延伸到"目录"这一层；四个角色的问句互不重叠，正是既有分层的精神。
- **与不变量 1/9/11 一致**：Provider 是来源边界，快照是聚合边界，Selector 是
  决策边界，Invoker 是执行边界；新抽象（CatalogManager 快照）有明确消费者
  （MCP + 现有命令发现 + 插件注册）。

---

## 十、总体结论与接入分期

### 10.1 现在必须确定（否则后续返工）

1. **身份模型**：`ActionID{Source, Name}` + Model Function Name 作为可重命名投影
   （§3）。
2. **目录生命周期**：`CatalogSnapshot` + `CatalogGeneration`，`Tool` 保持不可变，
   Provider 动态由 CatalogManager 单点吸收；**先拆职责再命名**——`ToolRegistry`
   保留兼容注册 API，聚合/快照/Generation/ModelName/可用性归新增 CatalogManager，
   不要让 `ToolRegistry` 长成超级 Manager（§1、§9）。
3. **职责边界**：ToolProvider / CatalogManager / Selector / Invoker 四角色
   划分；MCP 是"又一个 ToolProvider 来源"（§9、§4）。
4. **可用性状态机**：枚举 `Ready / UnavailablePolicy(Hard) / UnavailableProvider /
   Disconnected / ToolDisabled / ToolRemoved`，显式迁移 + "是否升代"契约；
   Hard 立即收缩、Soft 保留集合 + 调用时报错（§2）。
5. **缓存身份**：选择缓存/稳定状态从 `session` 升级为
   `session + CatalogGeneration + query/session state`；`Revision` 覆盖"同名改
   schema"（§2）。
6. **结果容器**：`ActionResult` 保持 `Text`/`Err` 兼容，新增可选 `Parts`/
   `Attachments`；MCP adapter 唯一转换点（§5）。
7. **安全边界**：`ActionPolicy` 权威、MCP 注解仅建议；所有 MCP Tool 经同一
   闸门；MCP 默认 `SelectionOptional` + 审批（§6）。
8. **包与配置边界**：MCP 独立子系统/插件，自持配置与生命周期（§7、§8）。

### 10.2 建议的分期

| 阶段 | 内容 | 依赖 | 风险 |
|------|------|------|------|
| **M0** | 定型身份与目录抽象：`ActionID`、`CatalogSnapshot`+`Generation`、`Tool` 保持不可变；**不改可观测行为**，补契约测试 | 无 | 低（纯抽象） |
| **M1** | MCP 子系统骨架：`builtin/ai/mcp`（或独立插件），先做 **stdio + initialize/tools/list**；接 `ToolProvider` seam | M0 | 中 |
| **M2** | 执行与安全：`MCPInvoker` + 策略映射（默认审批/权限）+ 准入/SSRF/TLS/凭据/命令与环境白名单 | M1 | 中高（安全面） |
| **M3** | 动态性与缓存：`tools/list_changed`、重连、可用性状态机、缓存 `Generation` 关联 | M2 | 中（缓存回归） |
| **M4** | 结果丰富化与更多传输：`Parts`/`Attachments`、HTTP/Streamable HTTP/SSE、resource/structured | M3 | 中 |
| **延后** | 工具结果多模态回灌、目录 event-sourcing、管理端 server UI、指标/审批键全量迁 `ActionID`、插件市场/远程 Provider 命名空间落地 | — | — |

### 10.3 推荐的最小接入接缝（seam）

**MCP 作为独立插件**：`Setup` 读自己的配置 → 建立连接与生命周期 → 实现并
注册 `toolkit.ToolProvider` → AI 侧经由既有的
`catalogState.DiscoverToolProviders(mgr)` **零改动**发现 → CatalogManager 物化
带 `ActionID` 的快照 → Selector/Decision **不变** → 新 `MCPInvoker` 在策略闸门
之后按 `Source` 分发执行。

这条缝合线的好处：**AI 核心（decision/runtime/execution）几乎零改动**，
MCP 的全部复杂度（传输/协议/进程/安全/配置）都收敛在独立子系统内，
同时天然满足"AI 不绕过权限系统"。

### 10.4 一句话回答"哪些现在定、哪些延后"

- **现在定**：**身份（ActionID）、目录快照与代数、可用性状态机、结果容器形状、
  安全边界归属、MCP 独立子系统 + 自持配置**。
- **延后**：**多模态结果回灌、event-sourcing 目录、管理 UI、指标/审批键迁移、
  完整传输矩阵**。
- **绝不改**：`decision` 不得认识 MCP；`Tool` 不得等价于 `CallResult`；
  MCP 不得绕过 `Selection → ActionPolicy → Approval/Permission → Invoker`。

---

## 附：九个问题与既有设计原则的契合度总表

| # | 问题 | 推荐方案的契合度 | 关键原则 |
|---|------|------------------|----------|
| 1 | Tool 静态假设 / Catalog 生命周期 | 高 | COW 不可变范式；Tool 不可变；Provider→快照单点吸收 |
| 2 | ToolSet / Prompt Cache 稳定性 | 高（含一处有意取舍） | 不变量 3（选择不破坏缓存稳定）、不变量 5（权限收缩立即生效，但不外溢到"可用性"） |
| 3 | Tool 身份模型 | 高 | 不变量 1（Tool 是协议边界概念）；身份朝内、函数名朝外 |
| 4 | Selector 与 MCP 动态性边界 | 高 | "不要先动 select.go"；子包依赖朝内；Selector 只认目录 |
| 5 | 结果模型丰富性 | 高（反对 C） | 不变量 11（What/How 分离）；MCP 语义不外泄 |
| 6 | 安全模型 | 最高 | "AI 不能绕过权限系统"；`decideToolInvocation` 单一闸门 |
| 7 | MCP Provider 复杂度与包划分 | 高 | 子包依赖朝内；插件根只负责装配与编排 |
| 8 | 配置系统膨胀 | 高 | config 是 leaf；ai 只负责 assembly |
| 9 | ToolProvider 职责边界 | 高 | Capability ≠ Action 的延伸；四角色问句互不重叠 |

## 修订记录

| 版本 | 修订 |
|------|------|
| v1 | 初次定稿：九个问题的本质/影响/方案/推荐/契合度；总体结论与 M0–M4 接入分期；契合度总表 |
