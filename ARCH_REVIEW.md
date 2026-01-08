# Remilia 代码结构/职责边界审查（不参考旧文档）

> 生成日期：2026-01-08
>
> 范围：仅基于仓库内 Go 源码与测试的静态审查（未阅读任何 md 文档）。

## 0. TL;DR（结论概览）

- **总体架构是“可用且偏工程化”的**：`Engine` 采用 COW（Copy-on-Write）状态快照做无锁读，很适合读多写少的事件路由；并且引入了运行时组件（cleaner / pending delete processor）的统一 shutdown 语义。
- **主要问题集中在“职责边界不够单一/重叠”和“遗留 API 迁移层扩散”**：
  - `Engine` 同时承担：路由匹配 + 运行时后台组件管理 + 临时 matcher 生命周期 + 兼容 API（Snapshot/Restore/Deprecated 字段）+ metrics holder。
  - 命令体系存在“两套实现并存”（`command_parser.go` 与 `command_enhanced.go`），易造成用户/维护者困惑。
  - DeadLetter / Retry 体系存在“多个入口与 source/attempt 字段来源不一致”的问题。
- **最高优先级建议**：先收敛 API 边界（Engine/Matcher/Plugin/Context/中间件之间的契约），把“兼容层/可选能力/运行时组件”从核心职责中剥离，避免未来继续膨胀。

---

## 1. 当前关键组件职责（按你现在的代码表现，而非理想化设计）

### 1.1 `Bot`
- 责任：生命周期总控（Start/Run/Shutdown），绑定 `Adapter` 把事件变成 `Engine.ProcessEvent` 调用；管理 bot-level `context.Context` 用于 shutdown 时取消 handler。
- 特点：
  - `Bot.Start()` 中每个事件都会 `go` 一个 goroutine 跑 `engine.ProcessEvent(ctx)`，并用 `Bot.wg` 等待。
  - `Bot.Shutdown()` 先 cancel，再 shutdown adapter，再 wait wg，再调用 `engine.Shutdown(ctx)`。

### 1.2 `Adapter` / `WebhookAdapter`
- 责任：事件源接入（Webhook HTTP server + 事件流 loop）。
- 特点：
  - `WebhookAdapter` 自己维护 `startCtx/startCancel` + `WaitGroup`，保证 `Shutdown(ctx)` 等到 loop 退出。

### 1.3 `Engine`
- 责任（实际代码体现）：
  1) **匹配/路由核心**：维护 `engineState`（matchers / index / cache），提供 `On*` 注册、`ProcessEvent` 事件分发。
  2) **中间件系统**：全局/分组中间件 COW 配置与 matcher chain cache（`middlewareState`, `Engine.Named`, `RebuildMatcherChain`）。
  3) **运行时组件管理**：temp cleaner / pending delete processor 注册到 `engineRuntime`，`Engine.Shutdown` 统一 stop+wait。
  4) **临时 matcher 生命周期**：`tempMatcherManager` + 清理器 + 迁移函数 `MigrateMatcherToTemp/FromTemp`。
  5) **兼容/迁移能力**：`Snapshot/Restore`，`Matcher.Engine` deprecated 字段仍存在等。
  6) **错误/死信/指标接入点**：`SetMetricsCollector` 持有 collector；`invokeHandler` 内部包含 panic recover / error log /（后续部分应还有 DLQ/metrics 更新）。

### 1.4 `Matcher`
- 责任：定义某类事件匹配规则（Rule 列表 + EventType + command + priority + blocking）与 handler；并维护 runtime 状态（deleted/临时次数/过期等）。
- 特点：
  - 同时持有 `Engine *Engine // Deprecated` 与 `coordinator MatcherCoordinator`，造成“两个主人”的感觉。
  - 自己缓存 combined middleware chain（global+group+local）并带 generation。

### 1.5 插件体系：`Plugin` / `BasePlugin` / `PluginManager`
- 责任：以 plugin 为单位批量注册 matcher、统一卸载、支持依赖、支持 reload（通过 Engine Snapshot/Restore 回滚）。
- 特点：
  - `BasePlugin.AddMatcher` 会写 matcher 的 `group` 与 `Source`，并调用 `Engine.UpdateMatcherIndex` 重建索引。

### 1.6 `Context`
- 责任：事件处理上下文（携带 stdlib context、event、api、state）。
- 特点：
  - 把 state 分成 `userState` 与 `internalState` 是很好的隔离。
  - 但一些中间件/错误包装仍使用 `ctx.GetState("mw_trace")` 等 userState key（见后文）。

---

## 2. 职责重叠/边界模糊点（重点）

### 2.1 `Engine` 与 `Bot` 的“优雅关停”语义有重叠
- 现状：
  - `Bot` 用自己的 `ctx cancel + wg` 管理每次事件 goroutine；
  - `Engine` 又有自己的 `eventWg` 统计 in-flight 执行；`Engine.Shutdown` 也会 wait。
- 风险/代价：
  - 同一件事（等待事件处理结束）在两个层重复维护，长期演进时容易出现语义不一致：例如 future 变更只改了 Bot.wg 没改 Engine.eventWg。
  - `Bot` cancel 会影响 handler，但 `Engine.eventWg` 仍会 wait 到 handler 返回（符合预期），只是“控制面分裂”。
- 建议（结构优化）：
  - 明确契约：
    - **Bot 只负责“停止输入源+传递取消信号”**（cancel parent ctx + adapter shutdown）。
    - **Engine 负责“事件处理的 in-flight 追踪与等待”**（eventWg）。
  - Bot 侧可以考虑不再维护单独的 wg（或改为只用于 adapter 内部 goroutine），把 in-flight wait 完全交给 Engine.Shutdown。

### 2.2 `Engine` 同时管理“核心路由状态”和“运行时后台工作”
- 现状：你已经通过 `engineServices`、`engineRuntime` 做了拆分，这是正确方向；但 `Engine` 仍然暴露/承载了很多 runtime 相关 public 方法（如 temp cleaner interval、pending delete、metrics holder）。
- 风险：Engine 会持续膨胀，最终变成“everything bagel”。
- 建议：
  - 把 runtime 能力继续内部化：
    - 对外只暴露必要的 option（`EngineOption`）与 `Shutdown(ctx)`。
    - temp matcher / pending delete / cleaner 细节尽量不要成为公共 API。

### 2.3 命令体系出现“两套并行实现”，职责重叠
- `command_parser.go`：
  - 提供 `ctx.ParseCommand()`（带 internal cache）+ `ParseCommandLine`（原始 tokens/flags/positional）。
- `command_enhanced.go`：
  - 提供 `CommandParser + CommandDefinition`（命令树/参数类型/校验/handler）。
  - 解析仍依赖 `ParseCommandLine`（说明它试图复用基础解析）。
- 问题：
  - 对用户来说，不清楚应该用哪套：`Engine.OnCommand` + `ctx.ParseCommand()` 还是“高级命令定义”体系。
  - 对维护者来说：两套系统都需要测试与 bugfix，长期必然分叉。
- 建议：
  - 明确“一个官方入口”：
    - 若目标是 SDK 简洁：保留 `ParseCommandLine/ctx.ParseCommand()`，把 enhanced 作为独立子包（例如 `remilia/command`）或明确标记实验。
    - 若目标是框架能力：将 enhanced 作为主线，基础解析只作为内部实现，不再在 root package 暴露太多入口。

### 2.4 DeadLetter 与 Retry 体系的 source/attempt 字段来源重叠且不一致
- `middleware/DeadLetter`：
  - 从 `ctx.GetString("source")` 与 `ctx.GetInt("retry_attempts")` 取值。
- `middleware/Retry`：
  - 写入的 key 是 `retry_attempt`（不是 `retry_attempts`）。
- `errors.WrapError`：
  - `Trace` 从 `ctx.GetState("mw_trace")` 读取（user state）。
- 风险：
  - DLQ item 的元信息字段在不同中间件之间没有统一契约，容易出现“写了但读不到”的隐蔽 bug（你现在就存在 `retry_attempt` vs `retry_attempts` 的不一致）。
  - `source` 的归属也不清晰：Engine/Matcher 已经有 `m.Source`，但 middleware 仍靠 ctx state。
- 建议：
  - 给“框架内部字段”统一迁移到 `Context.internalState`（你已经有 internalState 机制）：
    - 例如：`_remilia_internal_retry_attempt`、`_remilia_internal_source`、`_remilia_internal_mw_trace`。
  - 输出到用户态（比如日志）时再转成可读字段。
  - 统一由 Engine 在 `invokeHandler` 把 matcher source 写入 internalState（或直接在 WrapError 用 `m.Source`）。

### 2.5 `Plugin` 对 matcher 的“分组/来源”写入与 Engine 索引重建耦合
- `BasePlugin.AddMatcher`：写 `matcher.group` 与 `matcher.Source` 并调用 `Engine.UpdateMatcherIndex`。
- `Engine.UpdateMatcherIndex`：无视传入 matcher，直接全量 `rebuildIndex()`。
- 问题：
  - plugin add 一次 matcher 就触发全量 rebuild（如果在 Load 中批量 AddMatcher，会有明显写放大）。
  - `UpdateMatcherIndex` 接口名暗示“只更新一个 matcher”，但实现是全量重建，语义偏离。
- 建议：
  - 提供批量 API（例如 Engine 内部临时 buffer 或显式 Begin/EndBatchUpdate）或在 Plugin.Load 时由 plugin manager 统一触发一次 rebuild。
  - 或者让 `UpdateMatcherIndex` 改名为 `RebuildIndex()`（更符合事实），同时提供真正的局部更新方法（可选）。

### 2.6 `Matcher` 同时持有 `Engine` 与 `coordinator`，所有权/生命周期概念重叠
- 现状：
  - `Matcher.Engine` 被标注 deprecated，但仍广泛作为 fallback。
  - 同时还有 `coordinator MatcherCoordinator`。
- 风险：
  - 团队成员会继续依赖 `Engine` 字段；导致迁移难、API 继续扩大。
  - matcher 的“删除/重建链”到底应该走谁（engine vs coordinator）需要在多处做 fallback。
- 建议：
  - 彻底收敛：matcher 内部只保留一个抽象依赖（coordinator），Engine 字段保留但不再被新代码使用；并在内部统一设置 coordinator。

---

## 3. 代码结构可优化点（按层次拆解）

### 3.1 Package 级别：root package 过于拥挤（多领域混杂）
当前 `package remilia` 下同时存在：
- 核心路由（Engine/Matcher/Context）
- 插件系统（plugin.go）
- 权限系统（permission.go）
- 命令解析（command_*.go）
- 错误与死信结构（errors.go / deadletter_queue.go）
- 兼容层（infra_compat.go + Deprecated 文件）

建议的拆分方向（不要求一次到位）：
- `remilia/core`：Engine/Matcher/Context（核心数据结构与执行链）
- `remilia/plugin`：PluginManager/BasePlugin
- `remilia/command`：command parser + enhanced（或只留一种）
- `remilia/permission`：权限模型与中间件
- `remilia/infra/*` 已经存在：继续推进把 compat 缩成很薄的 facade

> 即使不做包迁移，也可以先做“目录层次”的软拆分（internal 子包或子目录），再逐步稳定 public API。

### 3.2 Engine：把“核心状态（engineState）”与“后台组件（runtime/services）”再隔离一层
你已经有：
- `engineState`（不可变状态）
- `engineServices`（temp manager/pool/metrics/pending delete channel）
- `engineRuntime`（统一 stop/wait）

建议继续推进：
- 把“temp matcher cleaner / pending delete processor”的启动逻辑从 `NewEngine()` 移走到一个 `startRuntimeComponents()`（内部函数），并且统一由 `engineRuntime.register()` 负责 stop/wait。
- `engineServices` 只保存依赖与配置，避免保存 stop func（stop func 更像 runtime component 的内部状态）。

### 3.3 Context：内部字段的使用要更一致
- 现状优点：`internalState` 与 `userState` 分离。
- 现状问题：错误/中间件 trace 等仍通过 userState key 传递（例如 `mw_trace`）。

建议：
- 框架内部传递信息只允许使用 internalState（带统一前缀）。
- userState 仅用于用户 SetState/GetState。

### 3.4 命令解析：减少重复入口与重复概念
- 如果保留 `CommandArgs` 体系：建议把 `CommandParser(增强)` 变成“可选模块”，不要与 Engine 强绑定。
- 如果主推增强体系：建议让 `ctx.ParseCommand()` 只返回 raw tokens（或干脆不暴露），让上层统一走 CommandDefinition。

### 3.5 错误模型：`errors.go` 有“框架错误”与“通用工具”混杂
`errors.go` 同时包含：
- handler 错误标准化（HandlerError, WrapError）
- BlockError（用于控制流）
- 大量通用错误变量 + generic wrapper（ErrorWrapper / WrapErrorf / RecoverError 等）

建议：
- 分拆为：
  - `handler_error.go`：与引擎执行链相关（WrapError, HandlerError, BlockError）
  - `errors_util.go`：通用 wrapper/validation/config error
- 并在命名上区分“框架控制流错误”（BlockError）与“业务错误”。

---

## 4. 问题清单（按紧急性/必要性分级）

> 评估维度：
> - **紧急性**：是否可能引起线上 bug、数据丢失、难以排查的问题。
> - **必要性**：是否会显著降低维护成本/降低未来演进风险。

### P0（高紧急 + 高必要）：建议尽快修

1) **Retry 与 DeadLetter 的 state key 不一致导致 attempt 丢失**
   - 表现：`Retry` 写 `retry_attempt`，`DeadLetter` 读 `retry_attempts`。
   - 影响：死信记录 attempt 不准确，排障困难；更糟时用户以为重试次数不生效。
   - 建议：统一 key；更推荐迁移到 `internalState` 并提供 accessor。

2) **DLQ item 的 source 依赖 ctx.State，而框架已存在 matcher.Source（信息来源混乱）**
   - 影响：source 不可靠，导致定位插件/匹配器失败。
   - 建议：Engine 在执行时已有 `m.Source`，DLQ/WrapError 直接使用它，避免 ctx 传递。

3) **Engine/Bot 双重 in-flight 追踪（wg 重叠），未来改动易引入 shutdown 语义 bug**
   - 影响：优雅关停时可能出现“提前返回/永不返回”的边界 bug；这类 bug 很难排查。
   - 建议：收敛到一个权威实现（推荐 Engine）。

### P1（中紧急 + 高必要）：建议在下一轮重构做

4) **命令体系两套并存（command_parser vs command_enhanced）导致 API 分裂**
   - 影响：用户困惑 + 维护成本翻倍。
   - 建议：明确主线并收敛入口；把另一套降级为 experimental 或迁移到独立子包。

5) **Plugin.AddMatcher 每次触发全量索引重建（写放大）**
   - 影响：插件加载/热更新时性能抖动，写路径不必要变慢。
   - 建议：批量 rebuild 或真正局部更新。

6) **Context 内部信息仍落在 userState（mw_trace 等）使用户 key 冲突风险回升**
   - 影响：用户误覆盖 key 或依赖内部 key，形成隐式耦合。
   - 建议：统一 internalState，提供只读导出（如果需要暴露）。

### P2（低紧急 + 高必要）：长期演进建议

7) **root package 职责过多，建议拆分子包**
   - 影响：认知负担大、循环依赖风险上升。
   - 建议：逐步迁移，不需要一次性大手术。

8) **Matcher 同时持有 Engine 与 coordinator（deprecated 混用）**
   - 影响：迁移困难、调用路径复杂。
   - 建议：内部统一只用 coordinator；Engine 字段只保留兼容读，不参与新逻辑。

9) **errors.go 混杂框架错误与工具错误**
   - 影响：文件过大、依赖扩散。
   - 建议：按语义拆分文件。

---

## 5. 建议的“收敛契约”（可作为重构的北极星）

为了避免未来继续发生“职责重叠”，建议显式定义这些契约：

1) **事件处理契约**
- 输入：`Adapter` 只负责把 `*dto.Payload` 投递给 Bot/Engine。
- 执行：`Engine` 负责 `ProcessEvent/ProcessEventBatch` 的并发模型、in-flight 追踪、panic recover、middleware 链。
- 退出：`Engine.Shutdown(ctx)` 是唯一权威的“等待所有事件结束”方法。

2) **元信息契约（source/attempt/trace 等）**
- `source`：以 `Matcher.Source` 为准。
- `attempt`：由 Retry 中间件写入 internalState（统一 key + accessor）。
- `trace`：由 Engine.Named 的 hook 写入 internalState（或 hook 直接输出到 metrics/log）。

3) **插件契约**
- 插件只负责“声明/注册 matcher + group”。
- Engine 负责索引维护策略（批量/局部），插件不应显式触发全量 rebuild。

---

## 6. 下一步（可选）

如果你希望我直接在代码里落地一轮“低风险收敛”（不改变对外主 API），我建议优先做：
- 修复 Retry/DeadLetter attempt key 不一致。
- 让 DeadLetter/WrapError 的 source 直接使用 matcher.Source（避免 ctx.State 传递）。
- 将 mw_trace 从 userState 迁移到 internalState，并保留旧 key 兼容读取一段时间。

（以上属于小改动但收益非常高的一类。）

