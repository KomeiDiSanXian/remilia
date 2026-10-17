# Remilia 核心模块审查记录（core / command / plugin）

**基线**：`b36377f`（审查时的 HEAD；本文档中的行号引用均对应此提交）
**范围**：`core/`（engine / context / fsm / permission）、`command/`、`plugin/`。`builtin/`、`cmd/`、`desktop/`、`dashboard/` 不在本次范围。
**方法**：逐文件精读 + 按模块并行交叉核查 + 仓库外最小复现实验（见附录一），随后对每个问题先复核、再修复、每项单独提交并配回归测试。
**验证**：`go vet ./...`（无输出）、`go test -count=1 -race -p 4 ./...`（根模块与 `cmd/bot` 全绿，含修复后的 19 个提交）。

编制说明：文中编号（`E`、`C`、`C-`、`PL`、`F`、`PER`）只用于相互引用，与处理顺序或轻重无关；本文不使用分级标签。事项状态只有两种：**已修复**（附提交）与**待处理**（附建议方向）。

---

## 一、架构与协作速览

| 组件 | 核心机制 | 协作方向 |
|---|---|---|
| `core/engine` | COW 不可变 `state`（`atomic.Pointer`）+ 多索引（matcher/command/regex/group/sortedCache）；`writeMu` 串行写者；`ExecPool` 卸载慢 handler；中间件链按代际号缓存；`RoutingStrategy` 产出候选流经 `mergeIter` 归并（上限 16 流） | 实现 `PluginCoordinator` 供 `plugin` 调用；消费 `core/context` 规则与 `command.Definition` |
| `core/context` | 每事件新鲜分配的 `Context`；平台事件/发送器抽象；规则求值（含 `pendingRuleEffects` 延迟副作用协议）；Dispatcher 接口反转出站 | 依赖 `command`（`OnParseCommand`）；被 engine/fsm/router/plugin 依赖 |
| `command` | `Registry`（Trie + Meta）O(1) 命令查找；`Parser`/`ParseFromDefinition` + 分层解析负责参数/flag/子命令 | 被 `core/context`、`plugin` 依赖，不反向依赖 core |
| `plugin` | `Manager` + 四个子控制器；`Instance` 状态机（Unloaded/Loading/Loaded/Disabled/Reloading/Unloading/Error）；`SetupContext`（Reg/Scope/EventBus/Container/goroutineManager/cron）；三种重载策略 | 依赖 `core/engine`、`core/context`、`command` |
| `core/fsm` | 声明式 FSM + per-session 锁 + Storage 抽象 | 依赖 `core/context`；被 `router`、`plugin` 使用 |
| `core/permission` | RBAC（角色/直授权/Provider），`Match` 支持 `*` 与 `prefix:*` | 经 `core/context` 注入与规则桥接 |

分层规则总体成立（`core/`、`infra/` 未导入 `builtin/`）。一处值得注意的边界：`core/context` 正向依赖 `command`（`core/context/command_helper.go`），因此命令解析的缺陷会直接表现为「消息被静默丢弃」。

---

## 二、已修复事项

### 2.1 `core/engine`

**E1 中间件链可能被过期快照填充缓存** — 已修复（`539e725`）

`invokeHandler` 慢路径先取 `e.middleware.Load()`，随后在 `getOrBuildIterChain` 内部读 `compiledVersion` 给新链盖章（基线 `core/engine/process.go:100-104`、`181-194`）。若两次读取之间发生 `Use()`/`ResetGroupMiddleware`（`core/engine/matcher.go:665-679` 清空缓存并自增版本），事件 goroutine 会把「无中间件」的链以最新版本号写入缓存，此后快路径版本比对恒命中，该 matcher 到下次中间件变更前不再执行任何中间件——鉴权、限流、日志被整体跳过。

**修复**：慢路径构建后重新校验中间件代际（全局 + 分组）与 `compiledVersion`，不一致则有界重试，连续失败退化为「不写缓存」。回归测试 `core/engine/middleware_chain_race_test.go`，并已用临时回退旧路径确认其能失败。

**E2 `ProcessEventSync` 绕过事件守卫** — 已修复（`15851d1`）

守卫提供 shutdown 门、`eventWg` 计数与 panic 兜底（基线 `core/engine/process.go:39-48`），而同步入口直接调用 `processEventMatchers`（基线 `core/engine/process_platform.go:150-153`）：停机期间的同步派发不受控，规则/链构造期 panic 直接抛给调用方。

**修复**：抽出 `processEventGuarded(ctx, allowPool)`，同步入口与异步入口共用 shutdown 检查、计数与 panic 恢复。回归测试 `core/engine/process_sync_guard_test.go`。

**E3 `ExecPool.Drain` 可无视超时** — 已修复（`f6626db`）

`Drain` 先在调用者 goroutine 同步执行 `drainQueue()`，之后才进入带 `ctx.Done()` 的 select（基线 `core/engine/exec_pool.go:199-200`、`208-213`），队列里有阻塞任务时 `Stop()` 的 30s 超时形同虚设。

**修复**：队列排空改为每取一个任务前检查 `ctx.Err()`，上下文取消即返回（已开始的任务按 Go 语义无法中断）。回归测试 `core/engine/exec_pool_drain_test.go`。

**E4 空命令名的 `OnCommand` 在索引重建后变成通配匹配** — 已修复（`112f610`）

`OnCommand("/")`、`OnCommand("!!")` 这类模式提取不到命令名，却仍被标记 `commandIndexed`（`Match` 据此跳过 `Rules[0]` 前缀检查，基线 `core/engine/engine_command.go:58-70`、`core/engine/matcher.go:301-303`）。结果：注册后该 matcher 不进命令索引、`Handle()` 又跳过索引更新，静默不匹配；任一次全量重建后它进入通用候选流且不再做前缀检查，匹配一切。`RegisterCommandWithPrefix` 传入空 `Name` 时同样退化到该路径。

**修复**：只有真正提取到命令名时才置位 `commandIndexed`，其余交给普通规则流（`Rules[0]` 每次都执行），注册前后（含重建后）行为一致。回归测试 `core/engine/oncommand_prefix_only_test.go`。

**E8 事件类型为空时同一 matcher 双路候选** — 已修复（`fa0664c`）

`ctx.GetEventType()` 为空（未绑定平台事件的 Context）时，各索引的 specific 与 generic 指向同一桶，却仍把两路都并入归并迭代器（基线 `core/engine/index_permanent.go:15-19`、`index_temp.go:18-24`、`index_command.go:16-29`；正则索引同样把同一桶预匹配两遍）：每个通用 matcher 在候选流中出现两次，handler 每事件执行两次。

**修复**：事件类型为空时只投递一路。回归测试 `core/engine/empty_event_type_test.go` 覆盖永久/命令/正则/临时四类索引。

### 2.2 `core/context`

**C1 `WithTimeout` 破坏延迟副作用的会话边界** — 已修复（`a9581ac`）

`WithTimeout` 把被包装的规则放到独立 goroutine 并直接传入原 Context（基线 `core/context/rules.go:336-348`），而引擎在超时返回后立即继续推进（丢弃副作用、`SetMatcher`、处理下一个候选）。仍在运行的规则因此会：与引擎并发读写 Context 字段；把迟到的 `DeferRuleEffect` 登记写进「后一个 matcher」的匹配会话，被它的提交执行——按冷却的语义，等于用另一个事件消耗了冷却（基线 `core/context/context.go:62-66`、`77-104`，`core/engine/process_platform.go:192`、`196`）。

**修复**：规则改在触发时的 `Context` 克隆上执行——克隆共享平台事件等只读数据并复制已有扩展状态，但拥有独立的匹配状态与副作用缓冲。规则在超时前返回时，其登记的副作用被并回本次匹配会话（语义不变）；超时路径不并回，迟到登记随副本一起丢弃。回归测试 `core/context/rules_withtimeout_effects_test.go`、`core/engine/withtimeout_effect_isolation_test.go`（后者构造「超时规则迟到登记 + 后一个 matcher 提交」的确定性交错，旧实现下同时触发数据竞争与误提交）。

**C2 `OnCooldown` 默认 key 无命名空间** — 已修复（`b1aac7d`）

冷却表是进程级全局单表，默认 key 仅为发送者 ID，判断与写入都不带 matcher/命令维度（基线 `core/context/rules.go:446-460`、`485-489`、`496`、`506`）：用户在 `/sign` 触发冷却后，同样用默认 key 的 `/daily` 等命令在窗口内静默不响应。

**修复**：默认 key 加上「本规则实例」的命名空间。规则求值时引擎尚未注入当前 matcher（`SetMatcher` 在 `Match` 之后才调用），规则实例是唯一始终可用的身份：不同 `OnCooldown` 调用（通常一个命令一条）互相隔离；同一规则实例挂到多个 matcher 仍是显式的「共享冷却」；显式 `keyFn` 完全由调用方控制（框架不加前缀）。重新注册（如插件重载）会得到新命名空间、冷却随之重置，已在文档注释中说明。回归测试 `core/context/cooldown_namespace_test.go`、`core/engine/cooldown_namespace_test.go`。

**C3 `ReplyWithContext` 忽略调用方 context** — 已修复（`030558e`）

函数体从未引用 `stdCtx`，发送始终使用调度器自建的 `sendCtx`（基线 `core/context/platform_event.go:175-204`、`core/engine/dispatcher.go:277-279`）：调用方传入的取消/超时无效，文档承诺的控制不存在。

**修复**：提交前若调用方 context 已结束则不入队、Future 直接以该错误结束；否则用 `context.AfterFunc` 把调用方的结束信号桥接为发送 context 的取消（调度器自身的 `SendTimeout` 仍然生效，先到者胜出），取消原因可用 `context.Cause` 读取。回归测试 `core/context/replywithcontext_ctx_test.go`。

### 2.3 `command`

**C-1 `Registry.Upsert`/`Unregister` 与 Trie 追加语义冲突** — 已修复（`095e029`）

Trie 的 `Insert` 对同一 key 追加值、`Remove` 只删一个值（基线 `infra/trie/trie.go:50`、`56-88`）；`Upsert` 重复注册不会替换旧值也不会更新快照，`Unregister` 只删 `values[0]`（基线 `command/registry.go:174-227`、`328-349`）。后果：更新命令不生效（`ExactMatch` 永远返回首个旧元数据）、`List`/`Complete` 出现重复项、注销后仍能命中、内存随重复注册线性增长。

**修复**：Trie 新增 `Replace`（同 key 替换）供 `Upsert` 使用并总是重建元数据快照；`Unregister` 清空该 key 的全部值。`Upsert` 遇到非法正则模式由 panic 改为告警忽略（`Upsert` 没有 error 返回值，与 `RegisterWithOptions` 的口径差距已在注释中说明）。回归测试 `command/registry_upsert_test.go`、`infra/trie/trie_test.go`。

**C-2 子命令命中后父层 flag/参数被丢弃** — 已修复（`29a39e0`）

`buildParsedFromLevels` 只绑定叶子层，父层收集到的 flags/位置参数无人消费（基线 `command/enhanced_system.go:332-360`、`230-327`）：`/cmd --mode fast sub` 的 `Parsed.Flags` 为空、父层 `Required` flag 缺失不报错、子命令前的位置参数被静默吞掉。

**修复**：逐层绑定已声明的 flag 与位置参数（显式值不被默认值覆盖）；`required` 只在实际执行的叶子层校验，父层 `Validator` 不运行（Cobra 语义，避免 `/ai reset` 这类「父命令只是命名空间」的场景因父层必填失败）。回归测试 `command/parser_parent_scope_test.go`。

**C-3 分词/命令词提取的空白口径不一致** — 已修复（`2257428`）

引擎侧用 `unicode.IsSpace` 提取命令词（基线 `core/engine/process.go:388-394`），而 `tokenize` 只把 `' '`、`\t`、`\n` 当分隔符（基线 `command/parser.go:216-224`）：`/cmd　hello`（全角空格）被解析成单个 token，`ParseFromDefinition` 报 command mismatch，消息被静默丢弃。

**修复**：分词与命令词提取统一按 Unicode 空白（`unicode.IsSpace`）切分，并按 rune 宽度切片，避免多字节空白被按字节切断。回归测试 `command/parser_unicode_whitespace_test.go`。

**C-5 负数位置参数被当成短标志** — 已修复（`6a1ff80`）

`ParseCommandLine` 用「以 `-` 开头且长度 2」判定短标志，而 `isShortFlag` 要求第二个字符是字母（基线 `command/parser.go:99-104`、`144-156`）：`/roll -1 100` 得到 `Flags[1]=100`、位置参数为空。

**修复**：统一使用 `isShortFlag`，`-1`、`-9` 等按位置参数处理。回归测试 `command/parser_negative_args_test.go`。

**C-6 未定义的转义序列吞掉反斜杠** — 已修复（`83437b1`）

`tokenize` 对未列出的转义一律「输出反斜杠后的字符」（基线 `command/parser.go:177-195`），Windows 路径被静默改写：`/ai C:\Users\name` → `C:Users<换行>ames`。

**修复**：未定义的转义保留反斜杠；`\\`、引号与空白在引号内外都可转义，引号内 `\n`/`\t`/`\r` 仍按控制字符处理（既有测试断言的 `\n` 行为保留，仅限引号内）。回归测试 `command/parser_escape_regression_test.go`，并同步更新 `command/parser_test.go` 中两处未加引号的断言。

### 2.4 `plugin`

**PL1 状态守卫不含 `Loading`** — 已修复（`688fcea`）

实例在 Setup 开始前就以 `Loading` 入表、Setup 在锁外执行（基线 `plugin/manager.go:840-854`），但 `Unregister` 只拦 `Unloading`、`reload` 只拦 `Reloading`/`Unloading`（基线 `plugin/manager.go:133-137`、`plugin/register_reload.go:19-26`），`Loading` 全部落穿：清理先于 Setup 完成执行（`rootScope` 可能尚未创建、Teardown 与在飞的 Setup 并发、重载可能启动第二次 Setup），随后 Setup 正常收尾把状态改回 `Loaded`、把 API 写回容器、把 Matcher 注册进引擎——这些资源已无人管理。

**修复**：`Unregister` 与 `reload` 在锁外等待实例离开 `Loading` 再进入各自流程（等待受调用方 context 约束，Setup 挂起时返回「still loading」错误，`ForceUnregister` 仍是兜底）；等待与状态检查之间若又出现新的 Setup，本次操作被拒绝并由调用方重试。回归测试 `plugin/loading_guard_test.go`。

**PL5 `SetupContext.Scope()` 无锁惰性初始化** — 已修复（`3d8eb3f`）

`Scope()` 裸读裸写 `rootScope`（基线 `plugin/runtime_context.go:525-533`），`Subscribe/SubscribeAll` 都经它。并发首调（插件后台 goroutine 与 Setup 收尾）会各建一个根 Scope：落败方的订阅与 `OnDispose` 钩子不在根上，卸载时泄漏；字段访问本身也是数据竞争。

**修复**：字段改为 `atomic.Pointer[Scope]`，用 CAS 发布，保证所有调用方拿到同一实例；`unload`/蓝绿清理与 InPlace 重载分别走 `rootScopeOrNil()`/`setRootScope()`。回归测试 `plugin/scope_atomic_init_test.go`（64 个 goroutine 同时首调，断言拿到同一 Scope）。

**PL6 `metaGM` 无同步读写** — 已修复（`3006a9c`)

元数据 goroutine 管理器被 `StopAll`（停机）停止、被 `StartAll`（重启）重建，又被 `notifyDependents`、配置变更重试定时器读取（基线 `plugin/manager_lifecycle.go:149-152`、`161-162`、`plugin/manager.go:1029`、`plugin/manager_config.go:100`），全程无同步：停机与重启交叠时既是数据竞争，重建结果也可能丢失。

**修复**：改为 `atomic.Pointer`，重建用 CAS 循环保证只发布一个管理器，读取统一经 `metaManager()`。回归测试 `plugin/lifecycle_metagm_test.go`。

### 2.5 `core/fsm` 与 `core/permission`

**F2 `canTransitionFrom` 忽略 `From:"*"`** — 已修复（`13f8b00`）

运行时在 `TryTransition` 与 `TryStartSession` 都接受 `From:"*"`（任意状态迁移，基线 `core/fsm/fsm.go:392`、`305`），但 `canTransitionFrom` 只比较字面量（基线 `core/fsm/fsm.go:139-146`），判定失败即 `stores.Delete`（基线 `286-287`）。后果：「任意状态 /cancel」类流程在用户发一句无关消息后，会话连同 `Data` 被静默清除；`Validate` 也只认字面量 `Initial`，只有通配启动事件的 FSM 无法注册。

**修复**：`canTransitionFrom` 与 `Validate` 都把 `"*"` 计入（`canTransitionFrom` 的注释本就承诺了这一语义）。回归测试 `core/fsm/wildcard_from_test.go`。

**PER1 前缀通配缺少命名空间边界** — 已修复（`2c094e6`）

`matchWithWildcard` 对 `prefix:*` 只做裸前缀比较（基线 `core/permission/permission.go:74-81`），授权范围被意外放大：`command:*` 命中 `commander`，`admin:*` 命中 `administrator`。权限判定是鉴权的最后一跳，边界错误即越权。

**修复**：要求值等于命名空间本身或以 `prefix:` 开头。角色权限与直授权都经同一匹配函数，一并生效。回归测试 `core/permission/wildcard_boundary_test.go`。

**PER4 `RevokeRole`/`RevokePermission` 留下空条目** — 已修复（`b17e36d`）

两个方法无条件把（可能为空的）过滤结果写回映射（基线 `core/permission/permission.go:282-293`、`308-319`）：对不存在或已无角色/权限的用户调用会写入空切片条目，映射随调用次数无界增长，并被 `ExportUserRoles`/`ExportUserPerms` 带进持久化数据（`RemoveRole` 已有 `delete` 语义，基线 `257-260`）。

**修复**：不存在的用户直接返回；结果为空时删除条目。回归测试 `core/permission/revoke_empty_entry_test.go`。

---

## 三、待处理事项

本节是第二轮修复开始前的清单。**除 `plugin/wasm` 的三项外，以下事项已全部修复**，逐项提交见本节末尾的对照表；其中的现象与行号引用保留（对应基线 `b36377f`），便于回溯原始证据。


以下事项已核实但本次未修改，按模块列出，含影响与建议方向。

### 3.1 `core/engine`

- **E5 批注册会话与 `SetTemp*`/`Delete` 组合语义**（基线 `core/engine/engine_register_batch.go:59-65`、`core/engine/state.go:336-401`、`894-903`、`core/engine/engine_matcher_ops.go:424-427`）：`Flush` 无条件提交会话收集的 matcher，`withBatchMatchers`/`makeRunnableSlice` 不过滤 `deleted`。批内 `SetTemp(true)` 会让同一 matcher 双索引并存；批内 `Delete()` 在 Flush 后留下 `deleted=true` 的僵尸条目（`Match` 会跳过，但命令表/内存不回收）。建议 Flush 前过滤 `deleted`，并让批内 `SetTemp`/`Delete` 同步修正收集列表。
- **E6 Matcher 可变字段读侧无锁**（基线 `core/engine/matcher.go:347-349`、`193-204` 写入；`state.go:271`、`367`、`389` 与 `process_platform.go:181`、`process.go:108`、`122` 无锁读取）：与运行期 `Handle`/`SetSource`/`SetMatcherGroup` 并发时构成数据竞争。建议读取统一走锁或原子快照。
- **E7 候选流上限与索引预算不一致**（基线 `core/engine/merge_iter.go:39`、`57-73`）：`mergeIterMaxStreams=16` 与「索引数 × 2」的预算上限不匹配，超出的流被静默丢弃。建议对齐二者并在丢流时告警。

### 3.2 `core/context`

- **C4 dispatcher panic 时整队丢弃**（基线 `core/engine/dispatcher.go:238-256`、`227-234`；`core/context/outbound_observer.go:73-81`）：worker panic 后队列被整体换新并删除，已入队任务与其 Future 无人生效解析，调用方 `Wait` 可能长期等待；worker 在 `baseCtx.Done()` 时直接退出也会留下队列。建议丢弃/关闭时逐个回填错误并解析 Future。
- **C5 延迟副作用只绑定 Context、不绑定匹配会话**（基线 `core/context/context.go:77-82`；`core/fsm/fsm.go:56`；`router/router.go:131-133`）：FSM 的 `Event.Match` 等直接调用规则的路径会把副作用登记进同一个 Context，随后被无关 matcher 的提交执行（与「FSM 路径不提交副作用」的文档承诺相悖）。建议为规则求值引入显式的匹配会话标记，未在会话内的登记直接丢弃。
- **C6 `Cancel()` 非原子；Clone 的 std context 从不取消**（基线 `core/context/context.go:109-117`；`core/engine/process_platform.go:215`）：并发 `Cancel()` 即竞争；池化任务的独立 std context 无人调用 `Cancel()`。建议 once/atomic 保护并在池任务结束时 `defer Cancel()`。
- **C7 克隆注释与实现不符**（基线 `core/context/context.go:171-194`）：注释自称深拷贝，实际只复制 string 键 map 容器，类型键条目与嵌套值在两个 Context 间共享。建议先修注释并列明共享值清单，再考虑对已知可变类型做拷贝。
- **C8 `candidateMeta` 无法清除**（基线 `core/context/candidate.go:29-33`；`core/engine/process_platform.go:220-222`、`238-240`）：`SetCandidateMeta(nil)` 是空操作，无 Meta 的候选可能继承前一个候选的 Meta。当前被「慢带候选必带 Meta」的不变式掩盖。建议允许显式清除或每候选统一写入。
- **C9 其他**：`TryGetGroupMembers` 把 API 错误吞成「平台不支持」（`core/context/platform_event.go:324-328`）；`event_types.go:15-47` 是 `platform.EventKind` 的字面量副本（枚举改名会静默失配）；`extensions.go`/`context.go` 残留「对象池」注释与事实不符；`OnParseCommand` 解析失败无 debug 日志（`core/context/command_helper.go:39-50`），与「消息被静默丢弃」叠加后排障困难。

### 3.3 `command`

- **C-4 `Upsert` 的返回值缺口**：非法正则模式已改为告警忽略（见 C-1），但 `Upsert` 没有 error 返回值，调用方无法感知模式被忽略也无法重试；建议补返回值或提供带 error 的变体。
- **C-7 其他**：别名冲突检测不全、静默覆盖（`command/registry.go`）；`Parser.rootCommands` 无锁（`command/enhanced_system.go:105-155`，生产路径主要走 `ParseFromDefinition`）；Registry 与 Engine 命令元数据双写存在漂移（`plugin/util_oncommand.go:48-65` 对 `core/engine/engine_command.go`）；`List`/`Complete` 顺序依赖 map 迭代；`List()` 返回内部切片且未过滤 Hidden。

### 3.4 `plugin`

- **PL2 失败/超时路径不清理 `rootScope`**（基线 `plugin/runtime_instance.go:124-131`、`188-191`；`plugin/manager.go:865-875`；`plugin/manager_lifecycle.go:71-78`）：Setup 返回错误只停 goroutine 管理器，Scope 上的订阅/中间件/`OnDispose` 钩子无人回收；`StartAll` 的失败路径连 `RemoveGroup` 都没有。建议失败收尾统一走幂等清理（Dispose + 停 goroutine + RemoveGroup + 清容器引用），超时路径在 Setup 自然结束后再清一次。
- **PL3 `gm.stopAndWait()` 不接受 context**（基线 `plugin/infra_goroutine.go:111-114`；`plugin/runtime_instance.go:194-197`）：`StopAll`/`Shutdown` 可能无限阻塞，与文档承诺矛盾。建议改为 `stopAndWait(ctx)`。
- **PL4 卸载失败留下半死状态**（基线 `plugin/manager.go:141-144`、`197-223`；`plugin/manager_lifecycle.go:279-285`）：`unload` 失败后状态为 `Error` 但实例留表，`StopAll` 会跳过；`ForceUnregister` 与 `Retry` 不做停 goroutine/Dispose/Teardown，订阅与后台任务残留。建议把这些清理做成幂等且不被 ctx 中断。
- **PL7 插件配置与全局配置映射互为别名**（基线 `plugin/config_provider.go:70-78`；`plugin/config.go:102-113`、`272-280`）：`Sub` 返回活 map，`Override` 就地改写，与「仅影响本插件」的文档相悖，并发可致 `concurrent map writes`。建议 `Sub` 返回深拷贝。
- **PL8 draining 仅按插件名索引**（基线 `plugin/manager_stats.go:118-135`）：连续两次蓝绿重载会互相覆盖条目，第一次的清理回调可能把第二次标成已完成。建议以条目引用/代际为 key。
- **PL9 InPlace 重载累积 cron 调度器**（基线 `plugin/register_reload.go:48-64`；`plugin/runtime_context.go:380-392`）：新 SetupContext 的 `cronInitOnce` 归零，重载 N 次会有 N 个调度器同时触发。建议迁移或停掉旧调度器。
- **PL10 框架生命周期通知使用阻塞 `Publish`**（基线 `plugin/manager_lifecycle.go:320-322`；`plugin/infra_eventbus.go:196-216`）：在业务 handler 调用链内阻塞发布，订阅者满时互相等待。建议框架内部通知改用带超时的发布或独立投递队列。
- **PL11 `StopAll` 跳过 `Reloading`/`Loading`**（基线 `plugin/manager_lifecycle.go:118-136`）：跳过时无告警，紧接着停掉元数据 goroutine，而在飞重载收尾仍会提交通知。建议等待在飞迁移或强制幂等卸载，并记录被跳过的插件。
- **PL12–PL16 其他**：配置变更通知重试耗尽后静默丢弃（`plugin/manager_config.go:93-109`）；`loadOrder` 注销不清、重试会重复追加（`plugin/manager.go:147-149`、`973`）；批量 pending 重试会二次 Setup（`plugin/manager.go:596-650`）；`exportAs` 无 key 冲突检测，注销可能删掉后来者的同名服务（`plugin/runtime_context.go:141-153`、`plugin/manager.go:163-173`）；`RemoveListener` 用 `!=` 比较接口，动态类型不可比较时 panic（`plugin/manager_lifecycle.go:299-305`）；蓝绿迁移窗口里后台注册的 Matcher 可能落入已被 `RemoveGroup` 的临时分组（`plugin/register_reload.go:208-223`），`RegisterWriter` 的字段改写无锁（`247-250`）。
- **PL17–PL19 `plugin/wasm`**：`Module.Close` 只关实例不关编译产物，卸载/重载累积内存（`plugin/wasm/runtime.go:196-198`，`manager.go:155-171`、`128-137`）；宿主函数声明与实现不符——`abi.go:33-38` 声明的 `register_command`/`send_message`/`storage_*`/`http_request` 全仓库无框架实现，而 `docs/notes/14-wasm-plugin.md` 仍宣称存在，导入这些函数的模块实例化会失败；`Bridge`/`adapter.go` 对 `ctx.GetPlatformEvent()` 返回 nil 无防御；`Manager.register` 持锁执行编译/初始化，阻塞其它插件操作。

### 3.5 `core/fsm`

- **F1 `ListSessions` 无同步读会话字段**（基线 `core/fsm/storage.go:79-91`；`core/fsm/fsm.go:242-256`、`423-437`）：管理 API 轮询与迁移并发时会读到撕裂的会话快照，`Data` 还是同一张活 map，消费方读/写可触发 Go 的 `concurrent map read and map write`（fatal error，进程直接退出）。建议快照走会话锁并深拷贝 `Data`。
- **F3 `TryStartSession` 遍历 map 快照**（基线 `core/fsm/fsm.go:296-303`）：多个 FSM 匹配同一消息时启动顺序不确定，先启动者对同名 sessionID 形成饥饿。建议引入显式顺序并稳定排序。
- **F4 `Manager` 与 `Engine` 双 key**（基线 `core/fsm/manager.go:46-52`、`63`；`core/fsm/fsm.go:203-206`；`descriptor.go:18-28`）：`desc.Name != FSM.Name` 时 `Unregister` 返回成功但 FSM 仍在拦截消息。建议 `Validate` 强制一致。
- **F5 Router 会话 ID 只到 chat 粒度**（基线 `router/router.go:146-152`）：群内任意成员可推进/接管他人多步流程，`Data` 全群共享。建议可选按用户隔离或加 owner 校验。
- **F6–F10 其他**：`EndSession` 不取会话锁，与迁移并发时 `Save` 可「复活」已结束会话（`core/fsm/fsm.go:437`、`509-511`）；`Storage` 未定义所有权/`Data` 非 nil 契约（`storage.go:26-37` 对 `fsm.go:398-405`、`533-535`）；`TryStartSession` 先落库后回调，回调 panic 会绕过补偿删除、回调内 `UpdateSessionData` 自死锁未列入禁止清单（`fsm.go:319-349`）；`Validate` 与运行时对 `"*"` 启动事件的口径已在本轮统一（`fsm.go:116-122` 对 `305`）；`StartCleanup` 传 nil stop 时永久常驻、重复调用叠加 goroutine 且清理无日志（`fsm.go:568-584`）。

### 3.6 `core/permission`

- **PER2 内核无等级/继承原语**（基线 `core/permission/permission.go:268-279`）：`AssignRole` 不做等级限制，`*:*` 与「superadmin/admin 角色名」是两套判定，等级判断被各命令层重复实现（漏一处即提权）。建议内核提供 `Role.Level`/`HasRoleAtLeast`/受保护角色元数据。
- **PER3 `Role` 以 `"resource:action"` 字符串为 key 存在歧义**（基线 `permission.go:95-99`、`141-143`）：`{command:echo, execute}` 与 `{command, echo:execute}` 的 key 相同，会误命中、误删除。建议改用结构体或无歧义编码。
- **PER5 Provider 错误被静默吞、每次拒绝两次外部调用**（基线 `permission.go:354-372`）：下游故障表现为「全员无权限」且无日志；消息风暴会放大外部压力。建议告警、负缓存或 singleflight。
- **PER6 `RegisterRole(nil)` panic、空名角色可注册**（基线 `permission.go:216-220`）：装配失误会 panic 或「授了角色但判定不生效」。

---

## 四、验证附录

### 附录一：仓库外最小复现实验

实验位于 `%TEMP%\remilia-repro`（只读引用仓库，未改动仓库代码），下表为上表所列问题的复现结果；括号内为对应事项编号。

| 实验 | 输入/操作 | 实测结果 | 结论 |
|---|---|---|---|
| A | `OnCommand(..., "/")` 空命令名（E4） | 阶段一命中 0 次；一处无关索引重建后命中 1 次 | 成立 |
| B | `eventType==""` 的通配 matcher（E8） | 单次事件命中 2 次 | 成立 |
| C | `/cmd　hello`（全角空格，C-3） | 单 token，`ParseFromDefinition` 报 mismatch | 成立 |
| D | `/cmd --mode fast sub`（C-2） | `Flags={}`；缺 required 不报错；子命令前位置参数被吞 | 成立 |
| E | `/roll -1 100`（C-5） | `Flags={1:100}`，位置参数为空 | 成立 |
| F | `/ai C:\Users\name`（C-6） | 变成 `C:Users\names`（`\n` 为真实换行） | 成立 |

其余事项通过代码路径精读与既有测试行为核实；本文中的每一项修复都配了回归测试，并在提交前用「临时回退旧实现 → 测试失败 → 恢复」的方式确认测试确实覆盖了缺陷。

### 附录二：修复提交

按提交顺序（均为 `master` 上的独立提交，每个提交自带回归测试）：

| 提交 | 事项 |
|---|---|
| `2257428` | C-3 分词按 Unicode 空白 |
| `6a1ff80` | C-5 负数位置参数 |
| `83437b1` | C-6 未定义转义保留反斜杠 |
| `29a39e0` | C-2 父层 flag/参数绑定 |
| `095e029` | C-1 注册表陈旧元数据（含 C-4 的 panic 部分） |
| `539e725` | E1 中间件链过期快照 |
| `15851d1` | E2 同步路径事件守卫 |
| `f6626db` | E3 ExecPool 排空响应取消 |
| `fa0664c` | E8 空事件类型候选去重 |
| `112f610` | E4 前缀模式保持规则驱动 |
| `a9581ac` | C1 `WithTimeout` 副作用隔离 |
| `b1aac7d` | C2 冷却默认 key 命名空间 |
| `030558e` | C3 `ReplyWithContext` 接入调用方 context |
| `3d8eb3f` | PL5 根 Scope 无竞争创建 |
| `3006a9c` | PL6 元数据 goroutine 管理器同步 |
| `688fcea` | PL1 `Loading` 状态守卫 |
| `13f8b00` | F2 通配 `From` 计入校验与存活判断 |
| `2c094e6` | PER1 前缀通配命名空间边界 |
| `b17e36d` | PER4 撤销操作不留空条目 |

### 附录三：验证命令

```bash
go vet ./...
go test -count=1 -race -p 4 -timeout 300s ./...          # 根模块
cd cmd/bot && go test -count=1 -race -timeout 600s ./... # 参考机器人
gofmt -l . && go fix ./...                               # CI 的 fmt 任务口径
```

`-race` 与 `-shuffle` 全绿说明本次修复多为需要特定交错或边界输入才触发的缺陷；正因如此，每一项都补了针对性的回归测试，而不是依赖端到端用例偶然覆盖。

### 附录四：第二轮修复（原「待处理事项」）对照表

下表给出第三节每条事项的修复提交；实现细节与验证方式见各提交信息与配套测试。

| 事项 | 修复提交 | 说明 |
|---|---|---|
| E5 批注册中 `Delete`/`SetTemp` 未结算 | `0f2bce6` | Flush 前过滤已删除/已迁移为临时的条目；`makeRunnableSlice` 兜底过滤 |
| E6 Matcher 可变字段读侧竞争 | `4c14ff0`、`e87d280` | Handler/Source 走锁下访问器；规则改用 COW 快照（注册点固定初值），热路径零锁，基准与基线持平 |
| E7 候选流上限与预算不匹配 | `1350a08` | 容量对齐为索引预算 ×2，超限限频告警；Meta 长度不符时保留候选 |
| C4 dispatcher 丢弃队列 | `688c0e0` | 丢弃前用已取消 context 唤醒每个任务，Future 得到错误而非永久等待 |
| C5 副作用只绑定 Context | `3b957c4` | 引擎开启匹配会话，会话外登记直接丢弃（文档承诺与实现一致） |
| C6 `Cancel` 并发与克隆释放 | `5a0e57c` | 互斥保护撤销函数；池任务结束释放克隆的 std context |
| C7 克隆复制语义 | `246d8d5` | 注释改为准确契约并补测试（string 键整表拷贝、类型键共享引用） |
| C8 候选 Meta 无法清除 | `ec5d870` | `SetCandidateMeta(nil)` 清除；引擎按需清除并覆盖池化克隆 |
| C9 群成员错误/常量副本/注释/解析日志 | `d424c29` | 新增 `GetGroupMembers` 暴露错误；事件类型常量由 platform 派生；陈旧注释与解析失败日志 |
| C-4 / C-7 命令注册表 | `2814931`、`e64db2a` | `Upsert` 返回错误并检测别名遮蔽；`List`/`Complete` 去重、过滤 Hidden、顺序确定；定义变更同步 Registry |
| PL2 加载失败/超时清理 | `fb38047`、`8b46fee` | 幂等 `cleanupFailedLoad`（Scope + goroutine + 分组），超时后等 Setup 结束再清一次 |
| PL3 `stopAndWait` 无 ctx | `21c1e8a` | `stopAndWaitCtx` 受 ctx 约束；后台收尾用统一上限 |
| PL4 强制卸载/重试清理 | `16f6c55` | ForceUnregister 走同一套幂等回收；loadOrder 清理与去重 |
| PL7 配置 map 互为别名 | `2625403` | `Sub` 深拷贝、加载时再拷一层，覆盖不再回写全局配置 |
| PL8 draining 按名索引 | `66e10d4` | 每次追踪令牌独立，第一次清理不再误标第二次 |
| PL9 InPlace 重载 cron 累积 | `d3b159d` | `stopCron` + 重载切换前停旧调度器；懒初始化改互斥锁 |
| PL10 生命周期通知阻塞发布 | `65bb361` | 内部通知走 `PublishWithTimeout`，超时丢弃并告警 |
| PL11 `StopAll` 静默跳过 | `a65b148` | 等待在飞加载/重载（受 ctx 约束），跳过列表告警，错误链可 `errors.Is` |
| PL12–PL15 | `98aa87b`、`2b82059`、`4b57f3c` | 配置广播重试不再丢弃；导出 key 归属跟踪；监听器比较不 panic、生命周期入口接受 nil ctx |
| PL16 蓝绿临时组与 writer 竞争 | `9a6ffb8`、`f9a4757` | 失效实例（卸载中/卸载失败）注册被拒绝或回滚；分组名与注册后回调加锁 |
| F1 `ListSessions` 快照 | `c27eeb2` | 会话锁内深拷贝（含 Data），存储只交出引用 |
| F3 / F4 启动顺序与双 key | `fc641b8` | 按注册顺序启动；描述符名必须与 FSM 名一致 |
| F5 群内接管多步流程 | `7788554` | 新增 `OwnerScoped`（记录归属者并只接受其推进） |
| F6–F10 会话生命周期其余项 | `0596bac`、`ec92829` | `EndSession` 加锁（回调走内部无锁路径）；启动先回调后落库；`StartCleanup` 幂等且有日志；Data 兜底初始化 |
| PER2 / PER5 等级原语与 Provider 故障 | `b947ff3` | `Role.Level` + `UserLevel`/`HasLevelAtLeast`/`AssignRoleChecked`；Provider 故障日志+回调，失败即短路 |
| PER3 / PER6 权限 key 与角色校验 | `e56258a` | 内部 key 长度前缀无歧义；拒绝 nil/空名角色 |

仍待处理：`plugin/wasm` 的三项（`PL17`–`PL19`）——编译产物回收、ABI 声明与实现/文档对齐、`Bridge` 空值防御与 `Manager.register` 的加锁范围。它们只影响 WASM 插件路径，不影响基于 Go 的插件与核心框架。

> 状态汇总：基线 `b36377f` 上的问题清单（`E1`–`E8`、`C1`–`C9`、`C-1`–`C-7`、`PL1`–`PL19`、`F1`–`F10`、`PER1`–`PER6`）已完成两轮修复，共 50 个提交（41 项修复 + 文档/测试配套），其中 46 项修复落地为回归测试；仅 `plugin/wasm` 的三项待处理。

