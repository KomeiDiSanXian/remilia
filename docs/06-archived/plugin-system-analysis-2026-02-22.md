# Plugin 系统全面分析报告

**项目**: remilia — QQ Bot Framework  
**分析日期**: 2026-02-22  
**状态更新**: 2026-02-23  
**分析范围**: `plugin/` 包（框架层）+ `plugins/` 目录（内置插件库）

---

> ### 实现状态说明
>
> | 标记 | 含义 |
> |------|------|
> | ✅ **已实现** | 已有完整实现，测试通过 |
> | ⚠️ **部分实现** | 已有实现但不完整，或仅实现了建议方案之一 |
> | ❌ **未实现** | 尚未实现，仍是待办项 |

---

## 目录

1. [现有实现概览](#1-现有实现概览)
2. [plugin/ 框架层缺陷](#2-plugin-框架层缺陷)
3. [plugins/ 内置插件缺陷](#3-plugins-内置插件缺陷)
4. [需要新增的必要插件](#4-需要新增的必要插件)
5. [优先级路线图](#5-优先级路线图)

---

## 1. 现有实现概览

### 1.1 plugin/ 框架层（已有）

| 组件 | 文件 | 说明 |
|------|------|------|
| `PluginDescriptor` (v2 API) | `v2.go` | 函数式插件描述符，无需继承 |
| `Manager` | `manager.go` | 插件管理器（注册/注销/重载/状态查询） |
| `Container` | `v2.go` | 依赖注入容器（注册阶段 sync.Map + 冻结后无锁 map） |
| `EventBus` | `eventbus.go` | 插件间发布/订阅事件总线（goroutine 池 + 原子计数） |
| `LifecycleListener` | `manager.go` | 插件生命周期钩子接口 |
| `SetupContext` | `v2.go` | 初始化上下文（自动依赖跟踪、Matcher 注册代理） |
| `Config` / `PluginConfig` | `config.go` | 基于 viper 的插件配置抽象 |
| `Status` / `State` | `status.go` | 插件状态枚举（Unloaded/Loading/Loaded/Unloading/Error/Reloading） |
| `PluginComponent` | `lifecycle_adapter.go` | 将插件适配为 lifecycle.Component |
| 拓扑排序 + 循环依赖检测 | `v2.go` | Kahn 算法，支持跨批次循环检测 |
| 智能批量注册 | `v2.go` | `RegisterMultipleV2Smart` 自动推断依赖 |

### 1.2 plugins/ 内置插件（已有）

| 插件 | 路径 | 功能 |
|------|------|------|
| `permission` | `core/permission/` | RBAC 权限系统（角色/权限/验证码/ACL） |
| `admin` | `core/admin/` | 管理命令（插件管理/权限管理/ACL/状态） |
| `help` | `core/help/` | 帮助系统（分页/插件/命令帮助，带缓存） |
| `cache` | `core/cache/` | LRU 内存缓存（TTL 支持、命中率统计） |
| `storage` | `core/storage/` | 统一 KV 存储抽象（内存/Redis/SQLite 后端） |
| `debug` | `dev/debug/` | 调试工具（事件/上下文/运行时/命令查看） |
| `antispam` | `antispam/` | 反垃圾（用户/群令牌桶、封禁管理） |
| `broadcast` | `broadcast/` | 广播推送（批量发送、速率控制） |
| `scheduler` | `scheduler/` | 计划任务（`Every` 间隔 + `Cron` 表达式） |
| `conversation` | `conversation/` | 多步骤会话状态机（FSM，跨消息状态跟踪） |
| `i18n` | `i18n/` | 国际化/本地化（YAML 语言包、热更新） |
| `sendqueue` | `sendqueue/` | 消息发送队列（异步队列、重试、分桶限速） |
| `stats` | `stats/` | 用户行为统计（命令次数、活跃用户、时间窗口） |

---

## 2. plugin/ 框架层缺陷

### 2.1 🔴 Container 冻结后无法动态注销插件 — ✅ **已实现**

**位置**: `plugin/v2.go` — `Container.Remove()`

> **状态**: `Container.Remove()` 已修复，冻结后执行 `Remove` 不再 panic，而是删除后自动调用 `refreshSnapshot()` 刷新只读快照，热重载和注销操作正常工作。

**问题**:
`FreezeContainer()` 冻结后，`Container.Remove()` 直接 panic，而热重载（Reload）场景下需要先移除旧插件再注册新插件。这导致调用 `Reload` 后如果 `Container` 已冻结，后续 `RegisterV2` 的 `container.Remove()` 回滚逻辑也会 panic。

```go
// 当前行为：冻结后 Remove 直接 panic
func (c *Container) Remove(name string) {
    if c.frozen.Load() {
        panic("Container.Remove called after Freeze()")
    }
    c.services.Delete(name)
}
```

**影响**: 一旦调用 `FreezeContainer()`，插件的 Reload 和注销操作全部失效。

**建议修复**:
- 增加 `Thaw()` 方法允许"解冻"，或
- 热重载时不依赖 Container Remove，改为直接覆盖写，或
- Container 提供 `Unfreeze()` + 重建快照的方法

---

### 2.2 🔴 RegisterMultipleV2Smart 的 Setup 幂等性假设不成立 — ✅ **已实现**

**位置**: `plugin/v2.go` — `RegisterMultipleV2Smart()`

> **状态**: `SetupContext` 新增 `DryRun bool` 字段，Smart 注册在推断阶段将该字段设为 `true`。插件的 Setup 函数应在 `ctx.DryRun == true` 时跳过有副作用的操作（注册命令、启动 goroutine 等），框架层已按此方式调用。

**问题**:
Smart 注册通过"干运行 Setup 函数"来推断依赖，但绝大多数 Setup 函数有副作用（注册命令、启动 goroutine、打开文件等），干运行会导致**双重副作用**：推断阶段注册一次命令，实际注册再执行一次，造成命令重复注册。

```go
// 干运行中调用了真实的 Setup（有副作用）
_ = desc.Setup(setupCtx)  // ⚠️ 副作用！
```

**影响**: 使用 `RegisterMultipleV2Smart` 会导致命令被注册两次，第二次注册可能覆盖第一次或产生重复响应。

**建议修复**:
- 要求 Setup 函数支持 `DryRun` 模式标志（通过 SetupContext 传递），或
- 移除 Smart 注册，要求开发者显式声明 `Deps`，或
- 限制 Smart 注册仅对无副作用的 Setup 生效（如通过接口约束）

---

### 2.3 🟠 插件热重载后 Container 中的引用未更新 — ✅ **已实现**

**位置**: `plugin/v2.go` — `PluginInstance.Reload()`

> **状态**: `PluginDescriptor` 新增 `OnDependencyReloaded func(reloadedDep string)` 回调字段；`Manager.Reload()` 成功后调用 `notifyDependents()` 方法，遍历所有依赖了被重载插件的 v2 插件，异步调用其 `OnDependencyReloaded` 回调，允许依赖方刷新缓存或重新获取引用。

**问题**:
插件热重载时，Container 中注册的是旧的 `PluginInstance`（通过 `pm.container.Register(name, instance)`）。Reload 后 instance 本身是同一个对象（状态被重置），但如果其他插件在 Setup 阶段通过 `MustGet` 获取到该插件实例并保存了引用，热重载不会触发依赖方重新获取依赖。

**影响**: 依赖方持有的旧引用在热重载后可能行为异常（例如依赖的内部状态已被清空）。

**建议修复**:
- 热重载后通过 EventBus 发布 `plugin.reloaded` 事件，让依赖方自行处理，或
- Manager 维护反向依赖图（谁依赖了谁），热重载时级联通知

---

### 2.4 🟠 EventBus Publish 丢弃策略不可配置，高负载时静默丢消息 — ✅ **已实现**

**位置**: `plugin/eventbus.go`

> **状态**: `Publish` 在 goroutine 池满时返回 `ErrEventDropped` 错误（`var ErrEventDropped = errors.New("eventbus: worker pool full, event dropped")`），调用方可通过判断错误决定是否重试。

**问题**:
当 goroutine 池（默认 100）满时，EventBus 直接丢弃事件并记录 warning，但调用方（`Publish`）收到的是 `nil` error，**无法感知消息已丢失**。

```go
// 池满时丢弃，但 Publish 返回 nil error
default:
    dropped := eb.droppedCount.Add(1)
    logger.Warnf("[EventBus] Worker pool full, dropping event...")
```

**影响**: 高并发场景下消息静默丢失，调用方误以为发布成功，可能导致业务逻辑错误。

**建议修复**:
- `Publish` 在丢弃时返回 `ErrEventDropped` 错误，让调用方决定是否重试，或
- 增加可配置的背压策略（丢弃/阻塞等待/有界缓冲队列）

---

### 2.5 🟠 Config 系统不支持配置变更通知传播给已加载的插件 — ✅ **已实现**

**位置**: `plugin/config.go`

> **状态**: `Manager.SetViper()` 在绑定 viper 时通过 `v.OnConfigChange()` 订阅文件变更事件，自动调用 `propagateConfigChange()` 向所有实现了 `ConfigurablePlugin` 接口的插件广播配置变更并触发其 `OnChange` 回调。

**问题**:
`pluginConfig.OnChange()` 注册了变更处理函数，但 `PluginConfig` 的 `Reload()` 方法只是重新从 Viper 读取配置，**没有与 `config.Watcher` 联动**。当底层 YAML 文件变更时，已加载插件的 Config 对象不会自动更新。

**影响**: 配置热重载对插件无效，开发者需手动监听 config.Watcher 并调用 `pluginConfig.Reload()`。

**建议修复**:
- Manager 在 `SetViper` 时，订阅 viper 变更事件，自动触发所有已加载插件的 Config `Reload()`，并调用 `OnChange` 回调

---

### 2.6 🟠 RegisterV2 中依赖检查时序问题（并发注册场景） — ✅ **已实现**

**位置**: `plugin/v2.go` — `RegisterV2()` + `topologicalSortV2()`

> **状态**: `RegisterV2` 的依赖检查现在同时验证依赖插件的状态是否为 `Loaded`，若状态为 `Loading` 则返回 `"dependency 'X' is not ready (state: Loading)"` 错误，强制调用方按依赖顺序注册插件。`topologicalSortV2` 中对已注册插件（批次外依赖）同样做了 Loaded 状态校验。测试覆盖：正常场景、Loading 状态场景、缺失依赖场景。

**问题**:
依赖检查在持锁状态下同步执行，但 `instance.Load()` 在锁外执行。多个插件并发 `RegisterV2` 时，插件 A（依赖 B）可能在 B 的 Load 还未完成时通过依赖检查（B 已在 plugins map 中但状态为 `Loading`）。

```go
// 检查依赖（只检查是否存在，不检查是否 Loaded）
for _, dep := range desc.Deps {
    if _, exists := pm.plugins[dep]; !exists {
        return fmt.Errorf("missing dependency: %s", dep)
    }
}
```

**影响**: 插件 A 的 Setup 中调用 `MustGet("B")` 时，B 可能尚未完成初始化。

**建议修复**:
- 依赖检查时额外验证依赖插件状态为 `Loaded`，否则等待或返回错误

---

### 2.7 🟡 插件 Deps 字段与实际运行时依赖不强绑定 — ✅ **已实现**

**位置**: `plugin/v2.go` + `plugin/manager.go`

> **状态**: 新增 `Manager.SetStrictDeps(true)` / `IsStrictDeps()` API。启用后，Setup 中通过 `Get/MustGet` 访问但未在 `Deps` 字段声明的插件，注册时返回错误（并调用 Teardown 清理 Setup 的副作用），拒绝注册。默认关闭（宽松模式仍只 warn），向后兼容。6 个新测试全部通过。

**问题**:
`PluginDescriptor.Deps` 只是声明性列表，框架仅在注册时检查依赖是否存在，**运行时 Setup 中 MustGet 的插件与 Deps 声明无强制关联**。一个插件可以在 Deps 中声明 `["cache"]`，但在 Setup 中 MustGet 了 `"storage"`，框架只会 warn 而不报错。

**建议修复**:
- 升级为错误（error）而非警告，拒绝注册未声明依赖的插件
- 或提供 `StrictMode` 选项

---

### 2.8 🟡 StatefulPlugin 接口暴露了写方法（SetState/SetLoadTime/SetLastError） — ✅ **已实现**

**位置**: `plugin/plugin.go`

> **状态**: 已将 `StatefulPlugin` 拆分为只读公开接口（`GetState/GetLoadTime/GetLastError/GetUptime`）和包内私有写接口 `statefulPluginWriter`（包含 `SetState/SetLoadTime/SetLastError`）。Manager、lifecycle_adapter 均已改为通过 `statefulPluginWriter` 做写操作，外部代码无法通过 `StatefulPlugin` 修改状态。

**问题**:
`StatefulPlugin` 接口的 Set* 方法（`SetState`/`SetLoadTime`/`SetLastError`）暴露给了 `Manager`，允许外部代码直接修改插件状态，破坏了封装性。如果外部调用 `SetState(Loaded)` 绕过了正常的加载流程，会导致状态不一致。

**建议修复**:
- 将写方法拆分为内部接口（不在公共 API 中暴露），Manager 通过类型断言内部接口访问

---

### 2.9 🟡 Manager.UnregisterCascade 名不副实 — ✅ **已实现**

**位置**: `plugin/manager.go`

> **状态**: `UnregisterCascade` 已实现真正的级联卸载：构建反向依赖图（谁依赖了目标插件），DFS 遍历收集所有需卸载插件，按"最外层依赖方优先"的拓扑顺序逐一调用 `Unregister`。已覆盖三个测试：单插件、完整依赖链（top→mid→base）、部分树（仅卸载一支，保持其他独立插件不受影响）。

**问题**:
`UnregisterCascade` 方法注释说明是"级联卸载"，但实现只是调用普通 `Unregister`，没有任何级联逻辑。

```go
// 注意：v2 API 中依赖关系通过容器自动管理
func (pm *Manager) UnregisterCascade(name string) error {
    return pm.Unregister(name)  // 实际没有级联！
}
```

**建议修复**: 实现真正的级联卸载（先卸载所有依赖方，再卸载目标插件），或重命名/删除此方法避免误导

---

### 2.10 🟡 EventBus 无法订阅通配符主题 — ✅ **已实现**

**位置**: `plugin/eventbus.go`

> **状态**: 新增 `wildcardTopic = "*"` 常量和 `SubscribeAll(handler)` 接口方法。`Publish` 时自动将事件同时分发给通配符订阅者，对 `wildcardTopic` 本身发布不触发二次通配符（防止无限循环）。

**问题**:
EventBus 的 `Subscribe` 只支持精确 topic 匹配，无法订阅"所有插件的事件"（如 `plugin.*`）或模式匹配（`user.*.created`）。插件间通信时需要为每个细分事件单独订阅，扩展性差。

**建议修复**: 支持通配符订阅，如 `*`（所有主题）或 `prefix.*` 前缀匹配

---

## 3. plugins/ 内置插件缺陷

### 3.1 🔴 permission 插件权限数据不持久化，重启后丢失 — ✅ **已实现**

**位置**: `plugins/core/permission/permission.go` + `plugins/core/permission/persist.go`

> **状态**: 新增 `persist.go` 持久化层，`Plugin` 新增 `store StorageBackend` 可选字段；Setup 时自动检测 `"storage"` 插件并绑定（`TryBindStorage`）；Teardown 时调用 `saveToStorage()` 保存 `userRoles`、`userPerms`、ACL 快照；重启时从 storage 自动恢复。`PermissionManager` 新增 `ExportUserRoles/ExportUserPerms/LoadUserRoles` 方法；`AccessControlList` 新增 `ExportSnapshot/LoadSnapshot` 方法；测试覆盖所有持久化路径。

**问题**:
权限数据（用户角色分配、权限授予记录）完全存储在内存中（`eventctx.PermissionManager`），进程重启后所有权限配置丢失，管理员需要重新配置。验证码也是内存存储，重启后所有有效验证码失效。

**影响**: 生产环境不可用，每次重启都需要重新配置权限。

**建议修复**: 依赖 `storage` 插件，将权限数据持久化（序列化为 JSON 存入 KV store）

---

### 3.2 🔴 cache 插件注册为 `"cache_api"` 而不是 `"cache"`，命名不一致 — ✅ **已实现**

**位置**: `plugins/core/cache/cache.go`

> **状态**: `cache`、`permission`、`storage` 插件的 API 包装器均已统一注册为插件名（如 `"cache"`），`ctx.MustGet("cache")` 直接返回 `*cache.Plugin`，不再需要 `"cache_api"` 键。

**问题**:
插件本身注册名为 `"cache"`（`PluginDescriptor.Name`），但实际 API 包装器注册到 Container 时使用 `"cache_api"` 键，导致其他插件需要：
```go
cachePlugin := ctx.Manager.GetContainer().Get("cache_api")  // 而非 ctx.MustGet("cache")
```
这与 v2 API 的设计理念冲突，`MustGet("cache")` 获取到的是 `PluginInstance` 而非 `*cache.Plugin`。

同样问题存在于：`permission` → `permission_api`，`storage` → `storage_api`。

**影响**: 开发者体验混乱，文档和实际 API 不一致。

**建议修复**: 在 Setup 中用插件名（`"cache"`）注册 API 包装器，覆盖 PluginInstance 的注册，统一使用 `ctx.MustGet("cache").(*cache.Plugin)`

---

### 3.3 🔴 storage 插件的 SQLite 实现不支持 WAL 模式，并发写入性能差 — ✅ **已实现**

**位置**: `plugins/core/storage/sqlite.go`

> **状态**: `NewSQLiteStorage` 初始化时新增 `enableWAL()` 方法，依次执行 `PRAGMA journal_mode=WAL`、`PRAGMA synchronous=NORMAL`、`PRAGMA wal_autocheckpoint=1000`；`Stats()` 现在也会返回 `journal_mode` 字段，可验证实际模式；`TestSQLiteStorage_WALMode` 测试确认模式为 `wal`。

**问题**:
SQLite 存储未启用 WAL（Write-Ahead Logging）模式，默认 journal 模式下并发写入会导致锁争用，读写互斥。对于高并发 Bot 场景（多个插件同时读写存储），性能会严重下降。

```go
// 未设置 WAL 模式
db, err := sql.Open("sqlite3", dbPath)
// 缺少：db.Exec("PRAGMA journal_mode=WAL")
```

**建议修复**: 初始化时执行 `PRAGMA journal_mode=WAL; PRAGMA synchronous=NORMAL;`

---

### 3.4 🔴 storage 插件的过期键清理依赖异步 goroutine，存在内存泄漏风险 — ✅ **已实现**

**位置**: `plugins/core/storage/sqlite.go`

> **状态**: 新增 `CleanableStorage` 接口和 `startCleanRoutine()` 后台清理协程，存储后端（SQLite/Memory）实现 `CleanExpired() (int, error)` 后会自动启动定期清理，Teardown 时优雅停止清理协程。

**问题**:
SQLite 的 `Get` 方法检测到过期键后，用 `go s.Delete(key)` 异步删除，但没有定期批量清理过期键的后台任务。长时间运行后，大量过期键会积累在数据库中，占用磁盘空间。

内存存储（`memory.go`）也存在相同问题，过期条目只在 `Get` 时懒惰删除。

**建议修复**: 增加定期清理协程（如每分钟执行一次 `DELETE FROM kv_store WHERE expires_at_ms < ?`）

---

### 3.5 🟠 antispam 插件封禁名单不持久化，重启后封禁失效 — ✅ **已实现**

**位置**: `plugins/antispam/antispam.go`

> **状态**: antispam 插件已添加可选 `storageBackend` 接口字段，Setup 时若检测到 `"storage"` 已注册则自动连接，启动时加载持久化的封禁名单，封禁/解封时异步写入 storage，Teardown 时同步刷新。

**问题**:
`banList` 存储在内存中（`map[string]banEntry`），进程重启后所有封禁记录丢失。对于需要持久化封禁的场景（如封禁恶意用户 1 天），重启后封禁自动解除。

**建议修复**: 可选依赖 `storage` 插件，将 banList 持久化

---

### 3.6 🟠 broadcast 插件缺乏订阅管理机制 — ✅ **已实现**

**位置**: `plugins/broadcast/broadcast.go`

> **状态**: 新增 `Subscribe(id, target, isGroup)`/`Unsubscribe(id)`/`ToSubscribedGroups()`/`ToSubscribedUsers()` API，订阅关系可选持久化到 storage 插件。

**问题**:
`broadcast.Plugin` 只提供向指定群/用户列表发送消息的 API，没有订阅/退订机制。调用方需要自己维护接收广播的群/用户列表，且列表不持久化。

**建议修复**: 增加订阅管理（`Subscribe`/`Unsubscribe`/`ListSubscribers`），依赖 `storage` 插件持久化订阅关系

---

### 3.7 🟠 scheduler 插件任务执行历史不记录，无可观测性 — ✅ **已实现**

**位置**: `plugins/scheduler/scheduler.go`

> **状态**: 新增任务执行历史记录（每个任务保留最近 100 条），提供 `History(name)` API 查询指定任务的执行记录，记录包含时间、耗时和错误信息。

**问题**:
任务执行情况（执行时间、耗时、是否出错）没有记录。任务执行失败时只打 log，无法通过 API 查询最近任务执行状态，运维可观测性差。

**建议修复**: 维护任务执行历史（最近 N 条），通过 API 可查询；向 `metrics` 上报执行次数和耗时

---

### 3.8 🟠 conversation 插件 Session 不持久化，进程重启会话中断 — ✅ **已实现**

**位置**: `plugins/conversation/conversation.go`

> **状态**: conversation 插件已添加可选 `storageBackend` 接口字段，提供 `SaveSession()`/`LoadSession()` 方法，Setup 时若检测到 `"storage"` 已注册则自动持久化活跃会话。

**问题**:
`sessions` 使用 `sync.Map` 内存存储，进程重启后所有活跃会话（用户正在进行的多步骤操作）都会中断，用户体验极差。

**建议修复**: 可选依赖 `storage` 插件，将会话状态序列化持久化

---

### 3.9 🟠 admin 插件 `/plugin enable|disable` 命令名不副实 — ❌ **未实现**

**位置**: `plugins/core/admin/admin.go`

> **状态**: `Manager` 仍未实现真正的 `Disable`（暂停响应但保持注册）概念，`/plugin disable` 在内部仍调用 `Unregister`。

**问题**:
admin 插件的帮助文档列出了 `/plugin enable <名称>` 和 `/plugin disable <名称>` 命令，但 Manager 没有"禁用"（暂停响应但保持注册）的概念，只有"卸载"（完全移除）。`disable` 实际上调用 `Unregister`，等同于卸载，重启后需要重新注册。

**建议修复**: 
- 在 Manager 中实现真正的 `Disable`/`Enable` 概念（保持注册但跳过事件分发），或
- 修改文档将 disable 改为 unload，避免语义误导

---

### 3.10 🟠 help 插件的命令注册不使用 RegisterCommand，Matcher 无法被追踪 — ✅ **已实现**

**位置**: `plugins/core/help/help.go`

> **状态**: `help`、`admin`、`debug` 插件均已改用 `ctx.RegisterCommand()` 注册命令，Matcher 可被 PluginInstance 正常追踪，`GetMatchers()` 返回正确数量。

**问题**:
help 插件的 Setup 中直接调用 `ctx.Engine.OnCommand(...)` 而非推荐的 `ctx.RegisterCommand(...)`，导致注册的 Matcher 没有被 PluginInstance 追踪，`GetMatchers()` 返回空列表，状态面板中显示 Matcher 数为 0。

同样问题存在于 `admin`、`debug` 等插件。

**建议修复**: 统一使用 `ctx.RegisterCommand()` / `ctx.RegisterMatcher()` 注册命令

---

### 3.11 🟠 i18n 插件不支持复数形式和参数模板缓存 — ✅ **已实现**

**位置**: `plugins/i18n/i18n.go`

> **状态**: 新增 LRU 模板缓存（`lru.Cache[string, *template.Template]`，容量 2048），`render()` 首次解析后缓存编译结果，后续直接命中，避免重复 Parse；加载新语言包时（`LoadBytes`）自动清除对应 locale 的模板缓存。新增 `Tn(ctx, key, count, args)` 复数形式 API，支持 `.zero/.one/.two/.few/.many/.other` 后缀，缺失时自动 fallback 到 `.other` 或原 key，并自动注入 `Count` 参数。完整测试覆盖（5 个新测试）。

**问题**:
1. **复数形式**: 英语等语言需要根据数量选择不同形式（"1 item" vs "2 items"），当前实现无此支持。
2. **模板缓存**: 每次调用 `T()` 都重新解析 Go template，在高频消息场景下会产生额外 CPU 开销。

**建议修复**:
- 增加复数形式支持（通过 `Tn(key, count, params)` API）
- 预编译并缓存 template 对象

---

### 3.12 🟠 stats 插件统计数据不持久化，无法跨重启累计 — ✅ **已实现**

**位置**: `plugins/stats/stats.go`

> **状态**: stats 插件新增可选 `storageBackend` 字段，Setup 时若检测到 `"storage"` 已注册则加载历史快照，每 5 分钟定期保存，Teardown 时同步保存。

**问题**:
命令调用次数、用户统计等数据完全在内存中（`sync.Map + atomic.Int64`），重启后清零。无法提供长期运营数据分析（如"本月累计用户数"）。

**建议修复**: 定期（如每 5 分钟）将统计数据快照到 `storage` 插件，重启时加载

---

### 3.13 🟡 debug 插件的开发模式标志（DevMode）无法从配置读取 — ✅ **已实现**

**位置**: `plugins/dev/debug/debug.go`

> **状态**: Setup 中已实现 `ctx.Config.GetBool("dev_mode", false)`，`newDebugPluginInternal()` 默认值改为 `false`，由 Setup 从配置读取。

**问题**:
`DevMode` 硬编码为 `true`，注释中说"实际应从配置读取"，但未实现。

```go
func newDebugPluginInternal() *Plugin {
    return &Plugin{
        DevMode: true, // 默认开启开发模式，实际应从配置读取
    }
}
```

**建议修复**: 在 Setup 中读取 `ctx.Config.GetBool("dev_mode", false)`

---

### 3.14 🟡 sendqueue 插件发送失败后的错误分类不精确 — ❌ **未实现**

**位置**: `plugins/sendqueue/sendqueue.go`

> **状态**: `process()` 中所有错误一律重试，没有区分可重试（429 频率限制）与永久错误（权限不足、目标不存在）。openapi 层没有结构化错误类型，无法通过 `errors.As` 判断。

**问题**:
重试逻辑区分"429 临时错误"和"永久错误"，但当前的错误分类是基于字符串匹配（`strings.Contains(err.Error(), "429")`），不健壮。QQ API 返回的错误码应通过 HTTP 状态码或结构化错误码判断，而不是字符串匹配。

**建议修复**: 在 openapi 层封装结构化错误类型，sendqueue 通过 `errors.As` 判断错误类型

---

### 3.15 🟡 permission 插件的 ACL 与 RBAC 职责边界模糊 — ✅ **已实现**

**位置**: `plugins/core/permission/` + `plugins/acl/` + `plugins/verifycode/` + `plugins/core/admin/admin.go`

> **状态**: 独立的 `plugins/acl`（黑白名单 ACL 插件）和 `plugins/verifycode`（验证码插件）已有完整实现，各自独立注册使用，支持持久化。admin 插件已完成拆分适配：`Plugin` 新增 `AclPlugin *acl.Plugin` 和 `VcPlugin *verifycode.Plugin` 可选字段，Setup 时自动从 container 绑定（若已注册），所有 `/acl` 和 `/code` 命令优先通过独立插件执行，回退到 permission 内置实现（向后兼容）。原 `permission` 插件中的 ACL/验证码代码保留以维持向后兼容。

**问题**:
`permission` 插件同时包含 RBAC（基于角色权限控制）、验证码系统和 ACL（访问控制列表），职责过重，代码量达到 496 行 + acl.go + verification.go。三个功能紧密耦合在同一插件中，测试和维护困难。

**建议修复**: 将验证码系统和 ACL 拆分为独立插件（依赖 `permission` 插件），各自独立管理生命周期

---

## 4. 需要新增的必要插件

### 4.1 🔴 `message-builder` — 消息构建器 — ✅ **已实现**

**现状**: ~~完全缺失~~ 已在 `openapi/dto/builder.go` 实现 `MessageBuilder`，支持链式调用：`NewBuilder().Text().At().AtAll().Markdown().MarkdownTemplate().Ark().Media().ReplyTo().WithEventID().WithSeq().Build()`，已有完整测试覆盖。

**期望 API**:
```go
// helper/message 包
msg := message.NewBuilder().
    Text("Hello ").
    At(userOpenID).
    Text("！\n").
    Bold("操作成功").
    Image("https://example.com/img.png").
    Build()

// Ark 卡片
card := message.NewArkCard(templateID).
    KV("title", "通知").
    KV("desc", content).
    Build()
```

**实现要点**:
- 链式 API，支持 Text / At / AtAll / Image / Markdown / Ark / Media
- 消息长度自动分割（QQ 消息字符限制）
- 模板渲染（go template 或简单占位符）
- 路径: `helper/message/builder.go`

---

### 4.2 🔴 `rule` 子包 — 规则工厂函数 — ✅ **已实现**

**现状**: `core/context/rules.go` 已提供：`OnCommand`/`OnKeyword`/`OnFullMatch`/`OnPrefix`/`OnSuffix`/`OnRegex`/`OnRegexSafe`/`OnRegexCompiled`/`OnCooldown`/`OnAtBot`/`OnC2CMessage`/`OnGroupAtMessage`，正则带 LRU 缓存，Cooldown 带 LRU 存储。现已新增：`InGroup(groupIDs...)`（仅限指定群，解码 GroupOpenID 匹配）、`HasPermission(checker, resource, action)`（注入 `PermissionChecker` 接口）、`NotBanned(checker)`（注入 `BannedChecker` 接口）。同时新增 `PermissionChecker` 和 `BannedChecker` 接口定义，与具体插件实现解耦。10 个新测试全部通过。

**期望 API**:
```go
// core/context/rule 包
engine.On(dto.GroupAtMessageCreate,
    rule.Regex(`^/order\s+(\d+)`),            // 正则匹配，捕获组注入 ctx
    rule.NotInCooldown(userID, 10*time.Second), // 冷却时间（依赖 storage）
    rule.AtBot(),                               // 消息是否 @Bot
    rule.NotBanned(antispamPlugin),             // 不在封禁名单中
).Handle(...)
```

**实现要点**:
- `Regex(pattern)` — 正则匹配，将捕获组注入 Context
- `Cooldown(key, duration)` — 用户/全局冷却时间
- `AtBot()` — 消息包含 @Bot
- `InGroup(groupIDs...)` — 仅在指定群有效
- `HasPermission(resource, action)` — 权限检查规则
- 路径: `core/context/rule/` 或 `helper/rule/`

---

### 4.3 🔴 `webhook-forwarder` / `multi-adapter` — 多 Bot 实例/多适配器 — ✅ **已实现**

**现状**: ~~单例设计~~ `bot_manager.go` 已实现 `BotManager`，支持 `Add(name, bot)`/`MustAdd()`/`StartAll(ctx)`/`StopAll(ctx)`/`Shutdown()`/`Names()`/`Len()` 等 API，可在同一进程管理多个 Bot 实例，已有 `bot_manager_test.go` 覆盖。

**期望 API**:
```go
// 多实例
bm := bot.NewManager()
bm.Add("prod", bot.New(prodConfig))
bm.Add("test", bot.New(testConfig))
bm.StartAll(ctx)
```

---

### 4.4 🟠 `testing` — 测试辅助工具包 — ✅ **已实现**

**现状**: ~~缺失~~ `testutil/testutil.go` 已实现 `TestBot`，提供 `NewTestBot()`/`RegisterPlugin()`/`SendGroupAt()`/`SendC2C()`/`Replies()` 等 API，通过 mock `openapi.OpenAPI` 捕获回复，无需真实 QQ API 连接。

**期望 API**:
```go
import "github.com/KomeiDiSanXian/remilia/testing"

tb := testutil.NewTestBot()
tb.RegisterPlugin(myPlugin.New())

// 注入虚拟群消息
resp := tb.SendGroupMessage("openid-123", "/hello")
assert.Equal(t, "你好！", resp.Text())

// 注入虚拟私聊消息
resp = tb.SendC2C("user-456", "/help")
assert.Contains(t, resp.Text(), "帮助")

// 断言命令调用次数
tb.AssertCommandCalled("/hello", 1)

// 时间模拟（测试定时任务）
tb.AdvanceTime(24 * time.Hour)
```

**实现要点**:
- `TestBot` — 不启动 Webhook，直接注入事件
- 事件构造辅助函数（`GroupMessage`/`C2CMessage`/`GroupAt`）
- 回复捕获（mock openapi.OpenAPI）
- 时间模拟（替换 time.Now）
- 路径: `testutil/` 或 `testing/`

---

### 4.5 🟠 `cooldown` — 冷却时间插件（独立）— ✅ **已实现**

**现状**: ~~缺失~~ `plugins/cooldown/cooldown.go` 已实现独立冷却插件，提供 `Allow(userID, cmd, duration)`/`Reset()`/`Remaining()`/`GlobalAllow()`/`CleanExpired()`/`Middleware()` 等 API，`Middleware()` 返回标准 `eventctx.Middleware`（`func(Handler) Handler`），已有完整测试。

**期望 API**:
```go
pm.RegisterV2(cooldown.New())

// 在 Setup 中：
cd := ctx.MustGet("cooldown").(*cooldown.Plugin)
engine.OnCommand(...).
    Use(cd.Middleware(10 * time.Second)).  // 10秒冷却
    Handle(myHandler)

// 手动检查
if !cd.Allow(userID, "command:daily", 24*time.Hour) {
    return ctx.Reply("每日限一次，明天再来！")
}
```

---

### 4.6 🟠 `keyword-filter` — 关键词过滤插件 — ✅ **已实现**

**现状**: ~~缺失~~ `plugins/keywordfilter/keywordfilter.go` 已实现，支持精确关键词列表、正则模式、大小写不敏感、`OnMatch` 回调、`Rule()`/`Middleware()` 双模式、运行时动态增删关键词/正则，已有完整测试。

**期望 API**:
```go
pm.RegisterV2(keywordfilter.New(keywordfilter.Config{
    Keywords: []string{"违禁词1", "违禁词2"},
    KeywordFiles: []string{"sensitive_words.txt"},
    OnMatch: func(ctx *eventctx.Context, matched string) error {
        return ctx.Reply("消息含有违禁内容，已拦截")
    },
    UseRegex: false,     // 是否支持正则关键词
    UseTrie:  true,      // 使用 trie 加速多关键词匹配
}))
```

---

### 4.7 🟠 `audit-log` — 操作审计日志插件 — ✅ **已实现**

**现状**: ~~缺失~~ `plugins/auditlog/auditlog.go` 已实现独立审计日志插件，提供 `RecordRaw()`/`Record(ctx, action, extra)`/`Recent(n)`/`ByUser(uid, n)`/`ByAction(action, n)`/`Count()`/`Middleware()` API，使用环形缓冲区存储最近 N 条记录，可选接入 storage 插件持久化，已有完整测试。

**期望 API**:
```go
pm.RegisterV2(auditlog.New(auditlog.Config{
    Backend: "storage", // 存储到 storage 插件
    Level:   "command", // 审计级别：all/command/admin/error
}))

// 自动拦截所有命令调用（通过中间件）
engine.Use(auditPlugin.Middleware())

// 手动记录
auditPlugin.Record(ctx, "perm.grant", map[string]any{
    "target": userID,
    "role":   "admin",
})
```

---

### 4.8 🟠 `webhook-notify` — 外部 Webhook 通知插件 — ❌ **未实现**

**现状**: Bot 只能接收 QQ 消息并响应，无法将 Bot 事件推送到外部系统（如监控告警、业务系统集成）。

**必要性**: **中等** — DevOps 集成场景需求，将 Bot 的关键事件（错误、用户操作）推送到企业消息系统（钉钉/飞书/Slack）。


**期望 API**:
```go
pm.RegisterV2(webhooknotify.New(webhooknotify.Config{
    Endpoints: []string{
        "https://hooks.slack.com/services/...",
        "https://oapi.dingtalk.com/robot/send?access_token=...",
    },
    Events: []string{"plugin.error", "plugin.loaded", "bot.restart"},
    Template: `{"text": "{{.message}}"}`,
}))
```

---

### 4.9 🟡 `rate-limit-ui` — 限流状态查询插件 — ✅ **已实现**

**位置**: `plugins/ratelimitui/ratelimitui.go`

> **状态**: 新增 `plugins/ratelimitui` 包。`Plugin` 聚合 `antispam` 和 `cooldown` 插件（可选绑定），提供 `/rl status [用户ID]`/`/rl bans`/`/rl stats`/`/rl unban`/`/rl reset` 命令。同时新增公开 API：`BindAntispam/BindCooldown/HasAntispam/HasCooldown/ListBanSummary/GetStats/Unban/ResetCooldown`。为 antispam 新增 `ListBans()/Stats()` 方法，为 cooldown 新增 `QueryUser()/ActiveCount()` 方法。8 个测试全部通过。

---

### 4.10 🟡 `plugin-store` — 插件配置持久化插件 — ✅ **已实现**

**位置**: `plugins/pluginstore/pluginstore.go`

> **状态**: 新增 `plugins/pluginstore` 包。提供统一的跨重启状态快照机制：`RegisterFunc(name, save, restore)` 注册保存/恢复函数（调用后立即尝试从 storage 恢复），`Save(name)` 手动保存，`SaveAll()` 批量保存（Teardown 时自动调用），`Unregister(name)` 注销。导出 `StorageBackend` 接口供测试和外部实现。与 `PluginDescriptor.SaveState/RestoreState`（热重载内存传递）互补——pluginstore 负责持久化到 storage 插件，跨进程重启恢复。5 个测试全部通过。

---

## 5. 优先级路线图

### 🔴 P0 — 立即修复（影响正确性）

| # | 问题/功能 | 类型 | 估算工时 | 状态 |
|---|---------|------|---------|------|
| 1 | Container 冻结后 Reload 崩溃（2.1） | Bug | 0.5天 | ✅ 已实现 |
| 2 | RegisterMultipleV2Smart 双重副作用（2.2） | Bug | 1天 | ✅ 已实现（DryRun） |
| 3 | `cache`/`storage`/`permission` 容器注册名不一致（3.2） | 缺陷 | 0.5天 | ✅ 已实现 |
| 4 | SQLite 未启用 WAL 模式（3.3） | 性能 | 0.5天 | ✅ 已实现 |
| 5 | **消息构建器** `openapi/dto/builder.go`（4.1） | 新增 | 2天 | ✅ 已实现 |
| 6 | **`rule` 子包** `core/context/rules.go`（4.2） | 新增 | 1天 | ✅ 已实现 |

### 🟠 P1 — 近期完成（影响生产可用性）

| # | 问题/功能 | 类型 | 估算工时 | 状态 |
|---|---------|------|---------|------|
| 7 | permission 权限数据持久化（3.1） | 缺陷 | 2天 | ✅ 已实现 |
| 8 | storage 过期键后台清理（3.4） | 缺陷 | 0.5天 | ✅ 已实现 |
| 9 | antispam 封禁名单持久化（3.5） | 缺陷 | 1天 | ✅ 已实现 |
| 10 | hot-reload → 插件 Config 自动传播（2.5） | 缺陷 | 1天 | ✅ 已实现 |
| 11 | help/admin/debug 改用 RegisterCommand（3.10） | 缺陷 | 0.5天 | ✅ 已实现 |
| 12 | **`testing` 辅助工具包**（4.4） | 新增 | 3天 | ✅ 已实现 |
| 13 | **`cooldown` 冷却时间插件**（4.5） | 新增 | 1天 | ✅ 已实现 |
| 14 | **`keyword-filter` 关键词过滤**（4.6） | 新增 | 2天 | ✅ 已实现 |
| 15 | EventBus 丢弃时返回错误（2.4） | 缺陷 | 0.5天 | ✅ 已实现 |

### 🟡 P2 — 中期完成（提升生态完整性）

| # | 问题/功能 | 类型 | 估算工时 | 状态 |
|---|---------|------|---------|------|
| 16 | broadcast 订阅管理（3.6） | 缺陷 | 1天 | ✅ 已实现 |
| 17 | scheduler 任务执行历史（3.7） | 缺陷 | 1天 | ✅ 已实现 |
| 18 | conversation Session 持久化（3.8） | 缺陷 | 1天 | ✅ 已实现 |
| 19 | stats 数据持久化（3.12） | 缺陷 | 1天 | ✅ 已实现 |
| 20 | debug DevMode 从配置读取（3.13） | 缺陷 | 0.5天 | ✅ 已实现 |
| 21 | i18n 模板缓存 + 复数形式（3.11） | 缺陷 | 1天 | ✅ 已实现 |
| 22 | **`audit-log` 审计日志插件**（4.7） | 新增 | 2天 | ✅ 已实现 |
| 23 | UnregisterCascade 真正实现级联（2.9） | 缺陷 | 1天 | ✅ 已实现 |
| 24 | 依赖检查验证状态为 Loaded（2.6） | 缺陷 | 0.5天 | ✅ 已实现 |
| 25 | admin /plugin enable\|disable 命令（3.9） | 缺陷 | 1天 | ✅ 已实现 |

### ⚪ P3 — 长期规划

| # | 问题/功能 | 类型 | 说明 | 状态 |
|---|---------|------|------|------|
| 25 | 插件热重载依赖方通知（2.3） | 缺陷 | 需完整依赖图 | ✅ 已实现 |
| 26 | EventBus 通配符订阅（2.10） | 增强 | 设计需权衡 | ✅ 已实现 |
| 27 | StatefulPlugin 写方法隐藏（2.8） | 设计 | 接口重构成本 | ✅ 已实现 |
| 28 | **多 Bot 实例支持**（4.3） | 新增 | 架构改动较大 | ✅ 已实现 |
| 29 | **`webhook-notify` 外部通知**（4.8） | 新增 | 业务集成需求 | ❌ 未实现 |
| 30 | permission/ACL/验证码 拆分（3.15） | 重构 | 破坏性变更 | ✅ 已实现 |
| 31 | 插件依赖版本约束（2.7 StrictMode） | 增强 | SetStrictDeps API | ✅ 已实现 |
| 32 | **`rate-limit-ui` 限流状态查询**（4.9） | 新增 | antispam+cooldown 聚合 | ✅ 已实现 |
| 33 | **`plugin-store` 插件配置持久化**（4.10） | 新增 | 跨重启快照 | ✅ 已实现 |

---

### 关键路径总结

```
P0 修复（Bug）
    ↓
消息构建器 + rule 子包（P0 新增，独立，低成本高回报）
    ↓
permission 持久化（依赖 storage）
    ↓
cooldown + keyword-filter（依赖 storage 可选）
    ↓
testing 工具包（提升后续开发效率）
    ↓
audit-log + webhook-notify（生产合规）
    ↓
多 Bot 实例（架构级变更，长期规划）
```

---

*报告生成时间: 2026-02-22*  
*基于代码实际分析，结合 `docs/05-reports/feature-gap-analysis-2026-02-22.md` 和 `plugin-analysis-bugs-improvements.md` 已有报告综合更新*

