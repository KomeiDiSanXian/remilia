# infra 工具包指南

`infra/` 目录提供框架的基础设施工具包：存储、日志、并发原语、HTTP、可观测性等。
插件开发中常用这些包，本文按功能分类介绍。

## 📦 包总览

| 包 | 用途 | 插件开发场景 |
|----|------|-------------|
| `kv` | LevelDB 键值存储 | 插件状态持久化（antispam/stats 用） |
| `storage` | 统一持久化存储抽象（GORM，默认 SQLite） | 结构化数据存储（messagelog 用） |
| `persist` | JSON 文件持久化 | 简单配置/状态落盘 |
| `future` | Future 异步结果 | `ctx.Reply` 返回值；等待发送结果 |
| `logger` | 结构化日志（zerolog） | 插件日志（`ctx.Log` 同源） |
| `dlq` | 死信队列（文件/Webhook 目标） | 发送失败消息持久化 |
| `httpclient` | 增强 HTTP 客户端（重试/超时/中间件） | 调用外部 API |
| `tracing` | OpenTelemetry 分布式追踪 | 链路追踪（见 tracing 文档） |
| `metrics` | Prometheus 指标 | 暴露插件自定义指标 |
| `health` | 健康检查树 | Bot/Adapter/DLQ 健康探针 |
| `audit` | 结构化审计日志 | 操作审计（auditlog 插件用） |
| `atomic` | 泛型原子值（`atomic.Value[T]`） | 并发安全的配置/状态 |
| `syncx` | 泛型并发数据结构 | 并发容器扩展 |
| `pool` | 对象池（带统计） | 高分配热路径复用 |
| `cache` | 通用缓存工具 | 热点数据缓存 |
| `option` | Option 模式工具 | 配置化构造 |
| `trie` | 前缀树 + Aho-Corasick | 关键词匹配 |
| `expr` | 安全数学表达式求值 | 计算/校验逻辑 |
| `server` | HTTP 服务器封装（优雅关闭） | 插件内置 HTTP 服务 |
| `fs` | 文件系统工具（惰性资源） | 文件读写 |
| `gif` / `textimage` / `webimage` / `zhtext` | 图片/文本生成 | 趣味插件渲染 |
| `coredump` | 崩溃时自动生成 core dump | 排障 |

## 🔑 常用包详解

### kv — 键值存储

```go
db, err := kv.Open("data/myplugin")   // 打开（或创建）LevelDB
if err != nil { return err }
defer db.Close()

db.Set([]byte("key"), []byte("value"))        // 写
v, err := db.Get([]byte("key"))               // 读（ErrNotFound 时 err != nil）
db.Delete([]byte("key"))                      // 删
```

适合"键值对"形态的插件状态（计数器、开关、最近时间戳）。持久化在插件生命周期内管理：
Setup 打开、Teardown 关闭（参考 `builtin/stats`）。

### storage — 结构化存储

`infra/storage` 提供 GORM 统一存储抽象；内置 `builtin/storage` 插件已封装
（默认 SQLite，支持 Postgres/MySQL），通过容器获取：

```go
// 插件中获取存储客户端（依赖声明 Deps: ["storage"]）
client := ctx.Service[storage.Client]("storage")

// 或直接使用 Plugin 方法（与 GORM 同构）
p := ctx.Service[*storage.Plugin]("storage")
p.AutoMigrate(&MyRecord{})
p.Create(&rec)
p.Where("user_id = ?", uid).First(&dest)
p.Find(&list)
```

### future — 异步结果

```go
f := future.New[platform.SendResult]()
// 异步完成：
// f.Resolve(result, nil)
// 等待：
result, err := f.Wait(ctx)          // ctx 取消则返回错误
```

`ctx.Reply(...)` 返回 `*future.Future[platform.SendResult]`——需要同步确认发送结果时
`future.Wait(ctx)`；忽略返回值即"发出即忘"。

### logger — 结构化日志

```go
logger.Info("message")
logger.Infof("count=%d", n)
logger.WithField("plugin", "mine").Warn("note")
logger.WithFields(logger.Fields{"user": id}).WithError(err).Error("failed")
```

插件内优先使用 `ctx.Log`（自动带插件名前缀）；框架级/工具函数中使用全局 `logger`。

### httpclient — HTTP 客户端

```go
resp, err := httpclient.Get("https://api.example.com/data").
    SetHeader("Accept", "application/json").
    SetQuery("page", "1").
    Do()                       // 或 DoJSON() / DoString()
```

- 链式配置：`SetHeader` / `SetQuery` / `SetJSON` / `SetTimeout` / `Use(middleware)` 等
- 内置重试（默认重试策略）与超时；`httpclient.NewClient()` 可定制 Transport 与中间件
  （`AuthBearerMiddleware` / `RateLimitMiddleware` / `TimeoutMiddleware` 等）

### audit — 审计日志

```go
alog, _ := audit.NewLogger(audit.DefaultConfig())
eng.Use(alog.Middleware())                     // 全事件审计
eng.Use(audit.CommandMiddleware(alog, "/ban")) // 指定命令审计
```

### pool / atomic / syncx — 并发原语

```go
// 对象池（带统计）
p := pool.NewInstrumentedPool(func() any { return &MyObj{} })
obj := p.Get().(*MyObj)
// ... 使用后放回
p.Put(obj)

// 泛型原子值
v := atomic.Value[int64]{}   // 或 atomic.Value[any]
v.Store(42)
n := v.Load()
```

### dlq — 死信队列

`infra/dlq` 支持文件（追加 JSON 行）与 Webhook 两种消费者，用于发送失败消息的
持久化与重放（配置见 `dead_letter` 节）。

---

*性能与热路径设计见 [架构笔记：infra 工具包](../notes/10-infra-toolkit.md)。*
