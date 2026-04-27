# Remilia 框架审查报告 v2

> 审查日期：2026-04-26（第二轮）
> 审查范围：全项目（v1 修复后的最新代码库）
> 审查方法：静态分析 + 架构审查 + 并发安全审查 + 测试覆盖审计 + API 设计审计

---

## 0. v1 修复情况回顾

v1 审查报告的 18 个问题已全部处理，其中 17 个已修复、1 个（lifecycle 声明式依赖排序）作为 known limitation 保留。引擎 COW 模型做了部分简化（eventGate 替换 + mergeSortedMatchersSix 简化）。

---

## 1. 严重问题（Critical）

### 1.1 [CRITICAL] `remilia.Config` 与 `config.Config` 命名冲突 ✅ 已修复

**涉及**: `bot.go:94`, `config/config.go:49`

`remilia.Config` 已重命名为 `remilia.BotMeta`，消除了与 `config.Config` 的命名冲突。所有引用已同步更新。

### 1.2 [CRITICAL] Telegram / WeChat 适配器是非功能性骨架

**涉及**: `platform/telegram/adapter.go:58`, `platform/wechat/adapter.go:57`

两个适配器的 `Start()` 直接返回 `"not yet implemented"` 错误：
```go
func (a *Adapter) Start(_ stdctx.Context, _ func(platform.Event)) error {
    return fmt.Errorf("telegram adapter: not yet implemented")
}
```
加入 Registry 并调用 `StartAll()` 会直接报错。应用层难以区分"真正失败"和"未实现"。

### 1.3 [CRITICAL] 7 个适配器中 3 个未实现 `RecoverableAdapter` ✅ 部分修复

| 适配器 | RecoverableAdapter | BotIdentity |
|--------|-------------------|-------------|
| discord | ✅ | ✅ |
| milky | ✅ | ✅ |
| onebot | ✅ **已添加编译时断言** | ✅ |
| qq | ✅ | ✅ |
| satori | ✅ | ✅ |
| telegram | **❌**（未实现，待后续）| **❌** |
| wechat | **❌**（未实现，待后续）| **❌** |

修复：onebot 实际已有 `OnDisconnect` 和 `notifyDisconnect` 实现，但缺少编译时断言。已添加 `var _ platform.RecoverableAdapter = (*ForwardWSAdapter)(nil)`。Telegram/WeChat 适配器按指示保留现状。

---

## 2. 严重问题 — 资源泄漏（High）

### 2.1 [HIGH] `discordSender.cleanupLoop()` 永久 goroutine 泄漏 ✅ 已修复

**文件**: `platform/discord/sender.go:50`

修复：添加 `stopCh chan struct{}` + `stopCleanup()` 方法，适配器 `Stop()` 时调用。cleanupLoop 改为 `select { case <-ticker.C: ...; case <-stopCh: return }`。

### 2.2 [HIGH] `AdaptiveRateLimit()` 便捷函数泄漏 2 个 goroutine ✅ 已修复

**文件**: `middleware/adaptive.go:623-627`

修复：`AdaptiveRateLimit()` 现在接受 `context.Context` 参数，后台 goroutine 通过父 context 取消终止。签名从 `AdaptiveRateLimit(config)` 改为 `AdaptiveRateLimit(ctx, config)`。

### 2.3 [HIGH] `PprofServer` 未集成进 Bot 生命周期 ✅ 已修复

**文件**: `pprof.go:129,138`, `options.go`, `bot.go`

修复：添加 `WithPprof(cfg)` Option 函数将 PprofServer 注入 Bot。Bot.Start() 中自动调用 `pprofServer.Start()`，Bot.Stop() 中自动调用 `pprofServer.Stop(ctx)`。

### 2.4 [HIGH] `tempMatcherManager.cleanToWatermark()` 未追踪的 goroutine ✅ 已修复

**文件**: `core/engine/temp_manager.go:150`

修复：移除 `go` 关键字改为同步调用。水位线清理（默认 10000 触发）频率极低，同步调用的性能影响可忽略。

---

## 3. 中等问题 — 测试覆盖（Medium）

### 3.1 [MEDIUM] 11 个包完全无测试

| 包 | 文件数 | 说明 |
|----|--------|------|
| `platform/milky/` | 9 | QQ Milky 适配器，~1500 行 |
| `platform/onebot/` | 9 | OneBot V11 适配器，~1200 行 |
| `platform/wechat/` | 1 | WeChat 占位适配器 |
| `builtin/core/admin/` | 1 | Admin 插件，805 行，20+ handlers |
| `builtin/dev/debug/` | 1 | Debug 插件，632 行，10+ handlers |
| `builtin/bundle/` | 1 | Bundle 插件工厂 |
| `builtin/calendar/` | 1 | 日历工具，13 导出函数 |
| `builtin/idiomdict/` | 2 | 成语词典，~1500 行 |
| `builtin/internal/jsonfile/` | 1 | JSON 文件 I/O 工具 |
| `infra/server/` | 2 | HTTP server 基础设施 |
| `infra/tracing/` | 3 | OpenTelemetry 追踪基础设施 |

### 3.2 [MEDIUM] 41% 的 middleware 实现文件无对应测试

| 无对应测试的文件 | 风险 |
|-----------------|------|
| `middleware/circuitbreaker.go` | 状态机（Closed/Open/HalfOpen）|
| `middleware/degradation.go` | CPU/内存监控 + 自适应降级 |
| `middleware/permission.go` | 关键 authz 中间件 |
| `middleware/tracing.go` | OpenTelemetry 集成 |
| `middleware/deadletter.go` | 错误路径 |
| `middleware/slow_handler.go` | 阈值告警 |
| `middleware/prometheus.go` | 指标采集 |
| `middleware/context_keys.go` | 常量（低风险）|
| `middleware/degraded_ext.go` | 少量代码（低风险）|

### 3.3 [MEDIUM] 关键类型无单元测试

- `errutil/errors.go`: `BlockError`、`RecoverError()`、`ValidationError`、`ConfigError`、`PluginError` 均无测试
- `errutil/stack.go`: `CaptureStack()`、`ShouldCaptureStack()` 无测试
- `lifecycle/errors.go`: `StartError`/`StopError` 无直接测试

---

## 4. 中等问题 — API 设计（Medium）

### 4.1 [MEDIUM] `plugin.Config` 接口有 12 个方法 — 违反接口隔离原则

**文件**: `plugin/config.go:10-35`

混合了三种职责：读取（Get*）、变更（Override/Reload）、观察（OnChange）。没有调用方需要全部 12 个方法。

### 4.2 [MEDIUM] `platform.Event` 接口有 8 个方法

**文件**: `platform/event.go:236-265`

核心事件抽象，每个适配器必须实现。可选接口（`RawEvent`、`EditableEvent` 等）是好的模式，但必须接口仍是单体。

### 4.3 [MEDIUM] `Get*` 前缀命名不一致

- `plugin` 包：统一使用 `Get*` 前缀（`GetString`、`GetState`、`GetMetadata` 等）
- `platform`/root 包：避免前缀（`Platform()`、`Sender()`、`Config()`）
- root 包 `Bot` 不一致：`Config()` 是 bare getter，`Engine()` 也是，但 `Health()` 也是

### 4.4 [MEDIUM] 三个 "Config" 类型语义不同 ✅ 已修复

`remilia.Config` 已重命名为 `remilia.BotMeta`。其余两个 `Config`（`plugin.Config` 接口、`config.Config` 结构体）语义不同，保留不变。

### 4.5 [MEDIUM] `BotBuilder.WithPlatformRegistry` 覆盖行为不对称 ✅ 已修复

**文件**: `bot_builder.go:138`

修复：`WithPlatformRegistry` 现在会自动迁移此前通过 `WithPlatformAdapter` 注册的适配器，以及此前另一 `registry` 中的适配器，不再覆盖丢失。

---

## 5. 中等问题 — 设计缺陷（Medium）

### 5.1 [MEDIUM] `pendingDeleteComponent.wait()` 永不等待 ✅ 已修复

**文件**: `core/engine/component_pending_delete.go:21-29`, `services.go`

修复：添加 `pendingDeleteDone chan struct{}` 字段到 services 结构，pending delete processor 启动时设置，wait() 方法监听该 channel。Shutdown 时确保 goroutine 退出后才返回。

### 5.2 [MEDIUM] `NewDedupFilter()` 安全漏洞 ✅ 已修复（文档加强）

已在 `NewDedupFilter` 的 godoc 中强化警告，明确推荐使用 `NewDedupFilterWithContext(ctx, config)`。`NewDedupFilterWithContext` 的文档也已补充示例。

### 5.3 [MEDIUM] TTL `Map` cache 缺少 context 版本

**文件**: `infra/cache/ttl.go:81`

GC goroutine 仅通过 `close(m.stopCh)` 退出，无 `NewWithContext()` 变体。

### 5.4 [MEDIUM] `platform/errors.go` 重实现 `errors.As` 逻辑 ✅ 已修复

`asErr` 函数已简化为直接使用 Go 1.26 标准库的 `errors.As` + `errors.AsType`，移除了手动错误链遍历逻辑。

### 5.5 [MEDIUM] `BotError.Unwrap()` 使用值接收器，`BotManagerError.Unwrap()` 使用指针接收器 ✅ 已修复

`BotError.Error()` 和 `BotError.Unwrap()` 已改为指针接收器，与 `BotManagerError` 保持一致。

---

## 6. 轻微问题（Low）

### 6.1 [LOW] `stdctx "context"` import 别名跨包不一致

9 个 platform 文件使用 `stdctx "context"`，但 root 包使用裸 `"context"`。

### 6.2 [LOW] root 包中 3 个错误类型缺少类型级 godoc ✅ 已修复

`BotError`、`BotManagerError`、`BotHealthResult` 已添加完整的类型级 godoc 注释，包含使用示例。

### 6.3 [LOW] `remilia.Config` 缺少 yaml/mapstructure 标签

与其他所有配置结构体不一致（虽然有意不用 YAML 反序列化）。

### 6.4 [LOW] QQ 适配器 `BotID()` fallback 行为与其他适配器不同

QQ 适配器在 API 登录未完成时返回配置 UserID；discord 返回 `""`。

### 6.5 [LOW] `plugin.Logger` 与 `infra/logger.Logger` Error 签名不一致

前者 `Error(msg, err)`，后者 `Error(msg)` + `.WithError(err)`。

### 6.6 [LOW] 3 个活跃的 TODO 遗留

| 文件 | 内容 |
|------|------|
| `lifecycle/lifecycle.go:385` | Manager 不支持声明式依赖排序（TODO） |
| `infra/coredump/coredump_windows.go:225` | Windows 平台实现不完整（TODO） |
| `plugin/context.go:287` | 等待 Go 1.27 泛型方法支持（TODO） |

---

## 7. 统计汇总

### 7.1 测试覆盖

| 指标 | 数值 |
|------|------|
| 总 Go 源文件 | 477 |
| 测试文件 | 186 |
| 完全无测试的包 | 11 个 |
| 无对应测试的文件 | ~150+ |
| Middleware 未测试文件 | 9/22（41%） |
| 未测试的平台适配器 | 3/8（37.5%）|
| 未测试的 builtin 插件 | 7/27（26%） |

### 7.2 资源管理

| 严重度 | 数量 | 关键问题 |
|--------|------|---------|
| HIGH | 4 | discordSender cleanup loop、AdaptiveRateLimit 泄漏、PprofServer 未集成、cleanToWatermark 未追踪 |
| MEDIUM | 4 | pendingDelete wait no-op、DedupFilter 安全问题、TTL cache 缺少 context、EventBus 缺少 drain |
| LOW | 0 | 所有 timer/ticker/WaitGroup/channel 使用正确 |

### 7.3 API 设计

| 严重度 | 数量 | 关键问题 |
|--------|------|---------|
| CRITICAL | 3 | Config 命名冲突、Telegram/WeChat 骨架适配器、RecoverableAdapter 缺失 |
| HIGH | 4 | plugin.Config 12 方法、platform.Event 8 方法、Get* 不一致、三个 Config 类型 |
| MEDIUM | 7 | 其他命名/设计/文档问题 |
| LOW | 6 | 小范围不一致问题 |

### 7.4 问题优先级总表

| # | 优先级 | 类别 | 问题 |
|---|--------|------|------|
| 1 | CRITICAL | 命名 | `remilia.Config` vs `config.Config` 冲突 |
| 2 | CRITICAL | 适配器 | Telegram/WeChat 非功能性骨架 |
| 3 | CRITICAL | 适配器 | 3/7 适配器缺失 RecoverableAdapter |
| 4 | HIGH | 泄漏 | discordSender.cleanupLoop 永久泄漏 |
| 5 | HIGH | 泄漏 | AdaptiveRateLimit 便捷函数泄漏 |
| 6 | HIGH | 泄漏 | PprofServer 未集成进 Bot 生命周期 |
| 7 | HIGH | 泄漏 | cleanToWatermark 未追踪 goroutine |
| 8 | HIGH | 接口 | plugin.Config 12 方法 |
| 9 | HIGH | 接口 | platform.Event 8 方法 |
| 10 | HIGH | 命名 | Get* 前缀不一致 |
| 11 | HIGH | 命名 | 三个 Config 类型语义不同 |
| 12 | MEDIUM | 测试 | 11 个包完全无测试 |
| 13 | MEDIUM | 测试 | 41% middleware 无对应测试 |
| 14 | MEDIUM | 测试 | 关键错误类型无单元测试 |
| 15 | MEDIUM | 设计 | pendingDeleteComponent.wait() no-op |
| 16 | MEDIUM | 安全 | NewDedupFilter 默认不安全 |
| 17 | MEDIUM | 设计 | TTL cache 缺少 context 版本 |
| 18 | MEDIUM | 设计 | platform/errors.go 重实现 errors.As |
| 19 | MEDIUM | 设计 | BotBuilder.WithPlatformRegistry 覆盖 |
| 20 | MEDIUM | 设计 | BotError Unwrap 接收器不一致 |
| 21 | LOW | 风格 | stdctx alias 不一致 |
| 22 | LOW | 文档 | 3 错误类型缺 godoc |
| 23 | LOW | 风格 | remilia.Config 缺 tags |
| 24 | LOW | 适配器 | QQ BotID fallback 不一致 |
| 25 | LOW | 风格 | plugin.Logger vs infra/logger 签名 |
| 26 | LOW | 遗留 | 3 个 TODO |

---

## 8. 修复进度

| 优先级 | 总计 | 已修复 | 待处理 |
|--------|------|--------|--------|
| CRITICAL | 3 | 2 ✅ | 1 ⏳ |
| HIGH | 8 | 6 ✅ | 2 ⏳ |
| MEDIUM | 9 | 5 ✅ | 4 ⏳ |
| LOW | 6 | 1 ✅ | 5 ⏳ |
| **合计** | **26** | **14** | **12** |

### 已修复（14 项）

| # | 问题 | 修复方式 |
|---|------|---------|
| 1 | `remilia.Config` 命名冲突 | → `BotMeta`，全量重命名 |
| 3 | onebot RecoverableAdapter | 添加编译时断言 `var _ platform.RecoverableAdapter`（已有实现）|
| 4 | discordSender cleanupLoop 泄漏 | 添加 `stopCh` + `stopCleanup()`，适配器 `Stop()` 时调用 |
| 5 | AdaptiveRateLimit 泄漏 | 签名改为 `AdaptiveRateLimit(ctx, config)` |
| 6 | PprofServer 未集成 | 添加 `WithPprof()` Option，Bot.Start/Stop 自动管理 |
| 7 | cleanToWatermark untracked | 改为同步调用 |
| 11 | 三个 Config 类型 | `remilia.Config → BotMeta` |
| 15 | pendingDeleteComponent.wait | 添加 `pendingDeleteDone` channel |
| 16 | NewDedupFilter 安全 | 文档警告增强 |
| 18 | platform/errors.go asErr | 改为标准 `errors.As` + `errors.AsType` |
| 19 | BotBuilder.WithPlatformRegistry | 改为合并模式，不再覆盖 |
| 20 | BotError Unwrap 接收器 | 改为指针接收器 |
| 22 | godoc 缺失 | 3 个类型添加注释 |

### 待处理（12 项）

| # | 问题 | 原因 |
|---|------|------|
| 2 | Telegram/WeChat 骨架适配器 | 按指示暂搁 |
| 8 | plugin.Config 接口拆分 | 需要更多架构讨论 |
| 9 | platform.Event 接口拆分 | 影响 7 个适配器 |
| 10 | Get* 命名一致性 | 项目级重构，需统一规范 |
| 12-14 | 测试覆盖 | 涉及 11 个包，大量工作 |
| 17 | TTL cache 缺少 context 版本 | 低频使用场景 |
| 21 | stdctx import 别名 | 低优先级风格问题 |
| 23 | remilia.BotMeta 缺 tags | 有意不提供，不修复 |
| 24 | QQ BotID fallback | 低影响 |
| 25 | plugin.Logger 签名差异 | 低影响 |
| 26 | 3 个 TODO 遗留 | 计划内功能 |
