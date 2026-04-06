# Storage 插件废弃分析

> **结论摘要**：storage 插件应当废弃，各插件应自行管理持久化逻辑。
> 该插件的设计理念（统一 KV 抽象）与 Remilia 插件系统的职责边界相冲突，
> 且无论如何演进都将面临"职责过重"或"能力不足"的两难困境。

---

## 1. 背景

`builtin/core/storage` 是一个三层抽象的 KV 存储插件：

| 层次 | 类型 | 职责 |
|------|------|------|
| Layer 1 | `Backend` | 原始字节存取（`[]byte`），由 `MemoryStorage` / `SQLiteStorage` / `RedisStorage` 实现 |
| Layer 2 | `Store` | 命名空间 + Codec 包装，供插件消费者使用 |
| Layer 3 | `Plugin` | 插件生命周期接入点，注册到 Plugin Manager |

目前 `acl`、`antispam`、`stats`、`verifycode`、`pluginstore` 五个内置插件将其声明为 `OptionalDeps`，在可用时接入持久化。

---

## 2. 不应实现（废弃）的理由

### 2.1 底层以 `[]byte` 存储导致外部可读性极差

所有数据在落盘时均以 JSON blob 形式写入 SQLite 的 `BLOB` 列：

```sql
-- kv_store 表结构
CREATE TABLE kv_store (
    key          TEXT PRIMARY KEY,
    value        BLOB NOT NULL,   -- 这里存储的是 JSON 序列化后的字节流
    expires_at_ms INTEGER,
    ...
);
```

使用 `sqlite3` 命令行或任何数据库工具直接查看数据库时，`value` 列呈现的是不可读的二进制/JSON 混合内容，无法像普通关系型数据库那样直接 `SELECT * FROM kv_store WHERE key LIKE 'acl:%'` 并读懂数据。对运维、调试、数据迁移均造成障碍。

**对比**：若 acl 插件自行维护一张 `acl_entries(user_id, mode, remark, added_at)` 表，任何 SQL 工具均可直接查询和修改。

### 2.2 自定义类型对 Codec 的隐性依赖

虽然泛型辅助函数 `Get[T]` / `Set[T]` 通过 `JSONCodec` 自动处理了序列化，但这掩盖了一个深层约束：**所有存储的类型必须是 JSON 可序列化的**。

一旦插件的数据结构包含：

- 非导出字段（如 `type entry struct { until time.Time }` 中的 `until`）
- 接口类型字段
- 循环引用
- 需要自定义序列化的类型（如 `time.Time` 的纳秒精度问题）

插件就必须引入专用的 JSON 中间结构（参见 `antispam` 中的 `banEntryJSON`），增加了维护成本，且这种适配逻辑本不应属于 storage 插件的责任范围。

### 2.3 KV 模型对复杂查询能力严重不足

当前各插件的实际持久化需求已超出 KV 语义：

| 插件 | 潜在查询需求 | KV 模型能否满足 |
|------|-------------|----------------|
| `acl` | 按添加时间范围查询条目 | ❌ 需全量加载后内存过滤 |
| `antispam` | 统计封禁频率、清理过期封禁 | ❌ 无法原子性按时间过滤 |
| `stats` | 按时间窗口聚合消息统计 | ❌ 根本无法在 KV 层表达 |
| `verifycode` | 按过期时间批量清理验证码 | ❌ 只能全量扫描 |

为了支持上述需求，storage 插件要么永远无法满足，要么必须引入结构化查询——而这正是 ORM 的职责。

### 2.4 重构路径必然越界为 ORM

若要改善以上问题，重构方向只有两条：

**方向 A：增强 KV 接口**
添加 `Scan`、`Filter`、`Count` 等方法 → 本质上是在 KV 上重新发明 SQL，实现一个性能更差、功能更弱的查询引擎。

**方向 B：引入 ORM 层**
接入 `gorm`、`ent` 等 → storage 插件从"KV 抽象"演变为"ORM 包装器"，体积和依赖急剧膨胀，远超一个插件的合理边界。

两条路径都违背了 Remilia 插件系统的设计原则：**插件应小而专注**。

### 2.5 共享 Backend 导致插件间的隐性耦合

所有插件共享同一个 `Backend`（同一个 SQLite 文件 / 同一个 Redis 实例）。这带来：

- **故障传播**：storage 插件初始化失败（如 SQLite 文件锁、Redis 连接超时），所有依赖它持久化的插件将静默降级为无持久化状态，这种失败是不可见的，因为 storage 是 `OptionalDeps`。
- **性能竞争**：高频写入的插件（如 antispam 的封禁持久化）会与其他插件争抢同一个写锁（`SQLiteStorage.mu`）。
- **Schema 冲突**：所有数据混在同一张 `kv_store` 表中，`Keys("*")` 会返回所有插件的键，`Clear()` 在误操作时会清空全部数据。

### 2.6 `pluginstore` 插件的存在揭示了设计的自我矛盾

`pluginstore` 是建立在 `storage` 之上的第二层抽象，用于"插件状态快照的跨重启持久化"。
这说明 `storage` 插件本身的抽象层次不够，需要再套一层才能满足实际需求。
**两层抽象叠加，但底层仍然是 `[]byte`，表达能力并未提升**。

---

## 3. 应当保留的理由（反驳及回应）

### 3.1 统一接口减少各插件的重复代码

**原论点**：各插件无需各自管理数据库连接，storage 插件统一初始化。

**回应**：Remilia 提供了 `plugin.SetupContext.Go()` 和 Teardown 生命周期钩子，插件完全可以在 Setup 中初始化自己的存储连接（如 `sql.Open`），在 Teardown 中关闭。对于简单场景，`encoding/json` + 标准库文件读写足够；对于复杂场景，插件应当选择最合适的存储方案，而非被强制使用 KV 模型。

### 3.2 命名空间隔离防止键冲突

**原论点**：`Store.prefix` 自动给键加命名空间前缀，防止不同插件的键冲突。

**回应**：这个问题在"各插件自行管理"的方案下根本不存在——每个插件使用独立的数据库文件或独立的数据库表，天然隔离。

### 3.3 TTL 支持开箱即用

**原论点**：内置 TTL 对 session、验证码等场景方便。

**回应**：
- 若使用 Redis 后端，插件可直接调用 Redis SDK 的 `EXPIRE` 命令，功能更强大（如 `PEXPIRE`、`TTL` 查询）。
- 若使用 SQLite，插件可以自行维护 `expires_at` 列，并在查询时加上 `WHERE expires_at IS NULL OR expires_at > NOW()` 过滤，这是标准做法，没有任何额外成本。

---

## 4. 替代方案建议

### 方案 A：各插件直接集成轻量级存储（推荐）

对于需要持久化的内置插件，直接在插件内引入最小依赖：

| 场景 | 推荐方案 |
|------|---------|
| 简单配置 / 小数据集 | 标准库 `encoding/json` + 文件读写 |
| 需要结构化查询 | 插件内嵌 SQLite（`database/sql` + `go-sqlite3`），自建表 |
| 会话 / TTL | 插件内嵌 Redis 客户端，或内存 map + GC goroutine（如 `cooldown` 插件现有实现） |
| 跨插件共享数据 | 通过插件 API 暴露，而非通过共享 KV 后端 |

**示例**：`acl` 插件自行维护 SQLite

```go
// acl 插件直接持有自己的 DB
type Plugin struct {
    db      *sql.DB
    mode    Mode
    entries map[string]Entry
    mu      sync.RWMutex
}

// 表结构：直接可读、可查询
// CREATE TABLE acl_entries (
//     user_id  TEXT PRIMARY KEY,
//     remark   TEXT,
//     added_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
// );
```

### 方案 B：保留 `Backend` 接口但仅作为可选工具包（非插件）

如果确有多插件共享同一存储后端的需求（如部署在嵌入式设备上，不允许多个 SQLite 文件），可以将 `Backend` / `Store` 等类型保留为一个 **纯工具库**（不作为 Plugin 注册），由宿主应用在启动时创建并直接传递给各插件：

```go
// 宿主应用负责创建并分发，不经过 Plugin Manager
db, _ := storage.NewSQLiteStorage("bot.db")
pm.Register(acl.NewWithBackend(db.NS("acl")))
pm.Register(antispam.NewWithBackend(db.NS("antispam")))
```

这保留了共享底层连接的好处，同时移除了"storage 插件作为 Plugin Manager 中一等公民"的地位，避免 `OptionalDeps` 带来的隐性依赖。

---

## 5. 迁移影响评估

| 受影响插件 | 当前依赖方式 | 迁移复杂度 |
|-----------|------------|-----------|
| `acl` | `OptionalDeps["storage"]`，全量 JSON 快照 | 低：自建 SQLite 表替换 |
| `antispam` | `OptionalDeps["storage"]`，封禁名单 JSON | 低：自建 SQLite 表替换 |
| `stats` | `OptionalDeps["storage"]`，统计快照 JSON | 中：快照结构可直接映射到关系表 |
| `verifycode` | `OptionalDeps["storage"]`，验证码 map JSON | 低：直接使用内存 map + TTL GC |
| `pluginstore` | 强依赖 `storage.Store`，作为二级抽象 | 中：需与 `storage` 一同废弃或重设计 |

---

## 6. 结论

storage 插件的核心问题不在于实现质量，而在于**设计定位的错误**：

1. **职责错位**：KV 抽象层不是"插件"，它是基础设施。作为 Plugin Manager 的一等公民，它的生命周期绑定、可选依赖模式以及对所有消费者透明的 `Backend` 都超出了插件应有的边界。

2. **能力天花板**：KV 模型无法支撑插件实际所需的结构化查询，而扩展该模型将不可避免地走向 ORM 的领域。

3. **外部可读性**：数据以 blob 形式存储，失去了关系数据库最核心的价值——可直接查询、可直接运维。

**建议**：废弃 `storage` 插件（含 `pluginstore`），各内置插件按需自行集成最适合其数据模型的持久化方案。如有共享底层连接的刚需，以工具库（非 Plugin）形式保留 `Backend` 接口。

