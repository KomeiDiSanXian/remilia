# plugins/stats — 用户行为统计插件

**用户行为统计插件**，自动记录 Bot 与用户的交互数据（命令调用次数、活跃用户 UV），并提供查询 API。

> **与根目录 `stats/` 包的区别**
>
> | 包 | 定位 | 依赖 |
> |---|---|---|
> | `stats/`（根目录）| 基础统计原语（Counter/Gauge/Histogram），供框架内部使用 | 零依赖 |
> | `plugins/stats/`（本包）| 用户行为统计插件，记录业务层数据 | 插件系统 |

---

## 功能

- **命令统计**：通过中间件自动记录每个命令的调用次数
- **用户统计**：记录活跃用户及其消息数（支持按日/周/月筛选）
- **TopN 查询**：`TopCommands(n)` 返回调用最多的 N 个命令
- **持久化**：可选对接 `plugins/core/storage` 插件，Bot 重启后保留历史数据

## 快速使用

```go
// 1. 注册插件
pm.RegisterV2(stats.New())

// 2. 挂载中间件（自动统计所有事件）
engine.Use(statsPlugin.Middleware())

// 3. 查询统计数据
sp := plugin.Must[stats.Plugin](ctx, "stats")
top := sp.TopCommands(10)
active := sp.ActiveUsers(stats.Last7Days)
total := sp.TotalMessages()
```

## 带持久化

```go
// 先注册 storage 插件
pm.RegisterV2(storage.New())
// stats 插件会自动检测并使用 storage 后端
pm.RegisterV2(stats.New())
```

## API

| 方法 | 说明 |
|---|---|
| `TopCommands(n int) []CommandStat` | 返回调用次数最多的 N 个命令 |
| `CommandCount(cmd string) int64` | 返回指定命令的调用次数 |
| `ActiveUsers(window TimeWindow) []UserStat` | 返回时间窗口内的活跃用户 |
| `TotalMessages() int64` | 返回总消息数 |
| `Middleware() Middleware` | 返回自动统计中间件 |
| `RecordCommand(cmd string)` | 手动记录命令调用 |
| `Reset()` | 重置所有统计数据 |

## 时间窗口

```go
stats.Today      // 今天（从 00:00 开始）
stats.Last7Days  // 最近 7 天
stats.Last30Days // 最近 30 天
stats.AllTime    // 全部时间
```

## 依赖

| 依赖 | 类型 | 说明 |
|---|---|---|
| `plugins/core/storage` | 可选 | 数据持久化，不注册则使用内存存储 |

