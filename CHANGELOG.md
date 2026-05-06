# Changelog

## v1.1.0 (2026-05-07)

### 🛡️ 稳定性与正确性修复

本次发布聚焦于**数据竞争消除、goroutine 泄漏修复和逻辑正确性改进**，涉及 core/engine、middleware、plugin、command、bot、infra/health 六大模块，共计修复 24 个问题。

#### 核心引擎 (core/engine)

- **修复 `execProfile` 懒初始化数据竞争** — 预初始化 `ExecProfile`，消除事件并发处理时的竞态
- **修复 `ctx.matcher` 跨 goroutine 竞争** — 池化执行时将 `SetMatcher` 移入闭包内，避免主循环覆盖
- **修复 `Matcher.GetSource()` 锁缺失** — 加 `RLock` 保护，与 `SetSource` 保持一致
- **重写 `ExecPool`** — 消除 `Drain` 与 `TrySubmit` 的 wg.Add 竞争窗口；执行 goroutine 退出前自动 drain 队列，防止队列任务停滞
- **修复 `MigrateMatcherToTemp/FromTemp` 双倍执行** — 反转迁移顺序，防止 matcher 同时在 state 和 temp 出现
- **清理死代码与未使用字段** — 删除重复的 `isTemp` 检查、未使用的 `runtimeMu`

#### 上下文 (core/context)

- **修复 `regexCache` 数据竞争** — `maxSize` 和 `desiredSize` 改用 `atomic.Int64`

#### 中间件 (middleware)

- **修复熔断器配置竞争** — `ResetTimeout`/`HalfOpenTimeout`/`HalfOpenMaxRequests` 热路径读取加锁
- **修复 `SimpleDedup` goroutine 泄漏** — `NewDedupFilter` 不再启动后台 goroutine，由 `NewDedupFilterWithContext` 替代
- **修复 LRU 驱逐只淘汰一条的 Bug** — 循环重置 `eldestTime`，正确驱逐超量条目
- **修复 `SlowHandler` 错误屏蔽** — 仅屏蔽 deadline 错误，保留真实业务错误
- **防止 `Start`/`StartMonitor` 重复调用** — 添加 `sync.Once`/`atomic.Bool` 幂等保护

#### 插件系统 (plugin)

- **修复 `OnConfigChange` 写入竞争** — 加 `p.mu` 保护 `p.onChange` 回调字段
- **修复 `buildTeardownContext` 数据竞争** — 在 `RLock` 内读取 `pi.setupContext`
- **修复 `notifyDependents` 指针竞争** — 在锁内拷贝 `inst.desc.Deps`/`cb`

#### 命令系统 (command)

- **修复 `Upsert` 静默吞错误** — pattern 编译失败改为 `panic`（与 `RegisterWithOptions` 一致）
- **修复 `Lookup` 返回 `*Meta` 数据竞争** — COW 方式创建 Meta 副本
- **修复 `GetString`/`GetInt`/`GetBool`/`GetFloat` 裸类型断言风险** — 改用安全类型断言
- **修复 `Meta` 值拷贝 noCopy 字段** — 手动逐字段构建副本

#### Bot 生命周期

- **修复 `lifecycle`/`health` 指针竞争** — 读写全部加 `b.mu` 保护

#### 基础设施 (infra/health)

- **修复 `timeout` 数据竞争** — 改用 `atomic.Int64`
- **修复 `SetCacheTTL` 返回过期缓存** — 变更 TTL 时清空 `cachedResult`

---

## v1.0.0 (2026-04-30)

### 🎉 初始发布

Remilia 是一个现代化、高性能、易于扩展的多平台聊天机器人框架。

#### 🚀 核心特性

- **高性能 COW 引擎** — Copy-on-Write 并发模型，无锁读取，单实例吞吐量 475,000+ msg/s
- **v2 插件系统** — 函数式设计，自动依赖注入，Smart 注册拓扑排序，支持热重载
- **多平台适配器** — QQ（Webhook）、Discord（Gateway）、Satori（Chronocat/Lagrange）、Milky、OneBot V11
- **中间件链** — 日志/限流/重试/熔断/降级/去重/死信队列/Tracing/Metrics，支持热更新阈值
- **命令系统** — Trie + commandIndex 双索引，O(1) 命令路由
- **可观测性** — Prometheus 指标、OpenTelemetry 分布式追踪、结构化日志（zerolog）、pprof 性能分析
- **可靠性** — 优雅关闭、自适应限流、熔断降级、死信队列（文件/Kafka/Webhook）
- **配置管理** — YAML + 环境变量，配置热更新
- **32 个内置插件** — Admin、Help、Permission、ACL、AntiSpam、AuditLog、Broadcast、i18n、Scheduler、Storage 等

#### 📦 安装

```bash
go get github.com/KomeiDiSanXian/remilia
```

#### 📊 性能指标

| 指标 | 值 |
|------|-----|
| 消息吞吐量（空 Handler） | ~475,000 msg/s |
| Engine ProcessEvent | ~5-6 μs/op |
| 命令解析 | ~1-2 μs/op |
| 堆内存（50,000 msg/s） | ~12-14 MB |

#### ⚠️ 注意事项

- **Telegram 和 WeChat 适配器**为骨架实现，暂不可用于生产环境
- 要求 Go 1.26+
- 许可证：MIT
