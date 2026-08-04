# 并发事件处理架构

> **最后更新**: 2026-08-04

Remilia 事件引擎的并发模型与路由设计：Copy-on-Write 无锁读取、可插拔索引路由、批量注册与平台侧并发分发。

---

## COW 并发模型

Engine 的状态管理采用 COW（Copy-on-Write）模式，保证高性能无锁读取：

```
读操作（ProcessEvent）
  └── state.Load()          ← 原子指针读取，无锁
      └── matcherIndex[]    ← 不可变快照，安全并发读

写操作（RegisterMatcher、DeleteMatcher 等）
  └── writeMu.Lock()        ← 单一写锁
      ├── 复制旧状态
      ├── 修改新状态
      └── state.Store()     ← 原子指针替换
```

**性能特性**：
- 读操作：完全无锁、零分配（O(1) 原子指针读取）
- 写操作：O(N) COW 复制（N = Matcher 数量），适用于写少读多场景

### 删除语义

`DeleteMatcher` 采用批量删除路径——matcher **立即**被标记 deleted（不再命中新事件），
从索引/状态中的物理移除由后台处理器按 `WithPendingDeleteProcessInterval`（默认 100ms）
批量完成，单次 COW 重建回收整批，避免高频删除的写放大。处理器未运行或队列满时
退化为同步删除；需要确定性时机可调用 `FlushPendingDeletes()`。因此
`GetMatcherCount` 等统计在一个处理间隔内可能仍计入已删除的 matcher。

---

## 路由与索引：RoutingStrategy

> v1.32.0 起，原"固定 6 路归并"重构为 **RoutingStrategy**——路由与执行分离。

`processEventMatchers` 只负责执行；索引的组织方式抽象为三层稳定边界：

```
RoutingStrategy → CandidatePlan → MatcherIndex
```

- **可插拔 MatcherIndex**：permanent / command / temp / regex 四个内置索引成为插件点，
  第三方可通过 `WithMatcherIndex` 注册自定义索引（HashMap/Trie/DFA…），执行主循环零改动
- **K 路归并**：固定 6 路归并泛化为 K 路（上限 16），候选流按优先级归并
- **快慢带 + 惰性执行**：BandFast（permanent/command/temp）急切构建，
  BandSlow（regex）惰性执行——fast 带被 block 短路时慢带索引零查询
- **候选 Meta**：regexIndex 预匹配携带捕获组，handler 经 `ctx.RegexResult()` 读取

详细设计见 [架构笔记：RoutingStrategy 路由规划](../notes/25-routing-strategy.md)。

### 三个索引

| 索引 | 驱动操作 |
|------|---------|
| `matcherIndex[EventType]` | `ProcessEvent` 按事件类型获取 Matcher 列表 |
| `commandIndex[cmd][EventType]` | O(1) 命令路由（消息以 `/` 开头时使用） |
| `groupIndex[group]` | `DisableGroup` / `EnableGroup` / `RemoveGroup` 批量操作 |

> **Source vs group**：`Source`（如 `"plugin:admin"`）是只读标签，用于日志/统计；
> `group` 是可变字段，驱动 `DisableGroup` 等操作。两者独立，不混用。

---

## 注册批处理（RegisterBatch）

> v1.32.0 起。启动期插件 Setup 的集中注册从每次 COW 全量复制收敛为**一次提交**：
> 1000 个命令顺序注册 786ms → 1.6ms（约 480 倍）。

- `Engine.BeginRegisterBatch()` / `RegisterBatch.Flush()`；插件 Manager 在 Setup 周围自动开启
- 并发加载重叠（如不同插件并发 Reload）时自动退化为逐条注册——功能正确，仅失去批量收益

---

## Engine 文件结构（核心）

| 文件 | 职责 |
|------|------|
| `engine.go` | 结构体 / `NewEngine` / `Shutdown` / 关闭语义 |
| `routing_strategy.go` | RoutingStrategy 路由规划抽象（可插拔索引、K 路归并、快慢带） |
| `matcher_index.go` / `index_*.go` | MatcherIndex 接口与四个内置索引（permanent/command/temp/regex） |
| `merge_iter.go` | K 路归并迭代器（sync.Pool 复用） |
| `engine_matcher_ops.go` | Matcher 注册/删除/分组/迁移/索引维护 |
| `engine_command.go` | 命令注册（`OnCommand` / `RegisterCommandDef`）/ 命令查询 |
| `engine_query.go` | 只读统计 / 指标收集 / Snapshot |
| `engine_register_batch.go` | 注册批处理会话 |
| `process.go` / `process_platform.go` | 事件处理主循环 / 平台事件入口 |
| `dispatcher.go` | 出站发送调度（按 Chat FIFO） |
| `exec_pool.go` / `exec_profile.go` | 自适应执行池（ExecProfile p50 判定） |
| `state.go` | COW 状态与索引维护 |

---

## 平台侧并发分发（Webhook worker）

事件引擎本身并发安全；平台适配器侧的并发度由各自配置控制。以 QQ webhook 适配器为例：

```go
adapter := qq.NewWebhookServerAdapterWithConfig(
    ":8080",
    botInfo,
    config.WebhookConfig{WorkerCount: 8},  // 事件处理并发数
)
```

- `WebhookConfig.WorkerCount` 控制事件循环的 worker goroutine 数量（默认见配置文档）
- 多 worker 并发调用 handler：**消息处理顺序不保证**——需要严格顺序的场景
  （如连续对话）应设置 `WorkerCount: 1` 或在应用层实现队列
- Engine 内部已保证并发安全，Handler 无需额外加锁；但 Handler 自身需线程安全

---

## 相关文档

- [配置快速参考](../02-user-guides/CONFIGURATION_QUICKREF.md) — `webhook.worker_count` 等配置项
- [架构笔记：RoutingStrategy](../notes/25-routing-strategy.md) — 路由规划详细设计
- [架构笔记：COW 引擎演进](../notes/01-cow-engine.md) — 并发模型迭代历史
- [架构笔记：自适应执行](../notes/22-adaptive-execution.md) — ExecPool 设计
