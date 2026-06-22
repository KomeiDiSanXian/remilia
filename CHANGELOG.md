# Changelog

## v1.12.3 (2026-06-22)

### 🚀 性能优化

- **过滤无 Handler 匹配器** — `hasHandler` atomic 标记 + `makeRunnableSlice()` 在 sortedCache 构建时排除无 Handler 匹配器，运行时 5K 匹配器场景吞吐量从 10K 提升至 3M msg/s
- **ExecProfile 预分配缓冲区** — `snapshotBuf` 复用避免热路径 `make()` 分配，消除 GC 风暴（GC 3181 次→5 次/10s）
- **ExecProfile demoted 快速路径** — `demoted` atomic 标记避免已确认的快 Handler 重复排序，`ShouldPool` CPU 占比从 20.54% 降至 1.48%

### 📝 文档重构

- 重写 README，更新特性列表和架构图
- 删除已归档设计文档（`docs/06-archived/`）和过时代码审查报告
- 修复所有文档中的 `logrus` → `zerolog` 引用
- 新增 `docs/05-performance/PERFORMANCE_REPORT.md` 性能报告
- 新增 `docs/02-user-guides/PLUGIN_DEVELOPMENT_GUIDE.md` 插件开发指南

### 🧪 Benchmark 修复

- 修复无限压力模式下 semaphore 无效的问题（acquire/release 在同一迭代，无实际并发控制）
- Drain 等待从 3s 扩展至 30s
- 延迟测量修正为 `time.Since(ev.Timestamp())`
- 添加 P50/P95/P99 百分位延迟统计
- 添加 `--inject-mode blocking` 模式以支持背压测试

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

#### ⚠️ 注意事项

- **Telegram 和 WeChat 适配器**为骨架实现，暂不可用于生产环境
- 要求 Go 1.26+
- 许可证：MIT
