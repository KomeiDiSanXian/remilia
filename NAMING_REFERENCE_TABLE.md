# 命名改进对照表

本文档提供了项目中所有需要改进的命名的详细对照表，按类别组织，方便查找和实施。

---

## 📦 包和导入 (Packages & Imports)

| 序号 | 当前名称 | 建议名称 | 文件位置 | 优先级 | 原因 |
|-----|---------|---------|---------|--------|------|
| 1 | `context2` (别名) | `eventctx` | 所有使用处 | 🔴 高 | 数字后缀不优雅，语义不清 |
| 2 | `infrapool` (别名) | `resourcepool` | 使用处 | 🟡 中 | 缩写不够清晰 |
| 3 | `dlq` (包名) | 保持或 `deadletter` | infra/dlq/ | 🟡 中 | 缩写晦涩，需要加强文档 |
| 4 | `httpreq` (包名) | `httpclient` | httpreq/ | 🟡 中 | 缩写且用途不明 |
| 5 | `helper` (包名) | 拆分为多个专用包 | helper/ | 🟡 中 | 过于通用，功能杂乱 |
| 6 | `errors` (包名) | `errutil` | errors/ | 🟡 中 | 与标准库冲突 |

---

## 🏗️ 结构体类型 (Struct Types)

### 核心类型

| 序号 | 当前名称 | 建议名称 | 文件位置 | 优先级 | 原因 |
|-----|---------|---------|---------|--------|------|
| 7 | `Service` | `APIService` 或 `Client` | openapi/openapi.go | 🔴 高 | 名称过于宽泛 |
| 8 | `Conn` | `Connection` | openapi/protocol/webhook/webhook.go | 🔴 高 | 不能体现是 webhook 连接 |
| 9 | `state` | `extensionState` | core/context/context.go | 🟡 中 | 小写且过于通用 |
| 10 | `retryAttempt` | `retryMetadata` | core/context/context.go | 🟢 低 | 可以更具体 |
| 11 | `middlewareTrace` | `middlewareExecutionTrace` | core/context/context.go | 🟢 低 | 语义不够明确 |

### 配置结构体

| 序号 | 当前名称 | 建议名称 | 文件位置 | 优先级 | 原因 |
|-----|---------|---------|---------|--------|------|
| 12 | `DedupConfig` | 保持 | middleware/dedup.go | ✅ 良好 | 清晰准确 |
| 13 | `RetryConfig` | 保持 | middleware/retry.go | ✅ 良好 | 清晰准确 |
| 14 | `DeadLetterQueueConfig` | 保持 | infra/dlq/dlq.go | ✅ 良好 | 清晰准确 |

---

## 🔌 接口类型 (Interface Types)

| 序号 | 当前名称 | 建议名称 | 文件位置 | 优先级 | 原因 |
|-----|---------|---------|---------|--------|------|
| 15 | `WebHook` | `Webhook` | adapter.go | 🟡 中 | Webhook 是一个词，不需要驼峰 |
| 16 | `MatcherInterface` | `SourceProvider` 或 `Matcher` | core/context/context.go | 🟡 中 | 冗余的 Interface 后缀 |
| 17 | `DeadLetterConsumer` | `Consumer` (在 dlq 包内) | core/engine/config.go | 🟢 低 | 包名已提供上下文 |
| 18 | `engineComponent` | `runtimeComponent` | core/engine/component.go | 🟢 低 | 更准确描述职责 |
| 19 | `Adapter` | 保持 | adapter.go | ✅ 良好 | 清晰的适配器语义 |
| 20 | `Plugin` | 保持 | plugin/plugin.go | ✅ 良好 | 简洁明了 |

---

## 📐 结构体字段 (Struct Fields)

### Engine 结构体

| 序号 | 结构体 | 当前字段名 | 建议字段名 | 文件位置 | 优先级 | 原因 |
|-----|--------|-----------|-----------|---------|--------|------|
| 21 | `Engine` | `s` | `services` | core/engine/engine.go | 🔴 高 | 单字母难以理解 |
| 22 | `Engine` | `writeMu` | 保持 | core/engine/engine.go | ✅ 良好 | 清晰表达用途 |
| 23 | `Engine` | `eventWg` | 保持 | core/engine/engine.go | ✅ 良好 | 清晰表达用途 |

### Bot 结构体

| 序号 | 结构体 | 当前字段名 | 建议字段名 | 文件位置 | 优先级 | 原因 |
|-----|--------|-----------|-----------|---------|--------|------|
| 24 | `Bot` | `mu` | `stateMutex` 或保持 | bot.go | 🟢 低 | mu 是惯用缩写，但可更明确 |
| 25 | `Bot` | `engine` | 保持 | bot.go | ✅ 良好 | 清晰 |
| 26 | `Bot` | `adapter` | 保持 | bot.go | ✅ 良好 | 清晰 |

### Adapter 相关

| 序号 | 结构体 | 当前字段名 | 建议字段名 | 文件位置 | 优先级 | 原因 |
|-----|--------|-----------|-----------|---------|--------|------|
| 27 | `webhookAdapter` | `wh` | `webhook` | adapter.go | 🟡 中 | 缩写不清晰 |
| 28 | `webhookAdapter` | `ctx` | 保持 | adapter.go | ✅ 良好 | 标准缩写 |
| 29 | `webhookAdapter` | `cancel` | 保持 | adapter.go | ✅ 良好 | 清晰 |

### Context 结构体

| 序号 | 结构体 | 当前字段名 | 建议字段名 | 文件位置 | 优先级 | 原因 |
|-----|--------|-----------|-----------|---------|--------|------|
| 30 | `Context` | `api` | `apiClient` 或 `openAPI` | core/context/context.go | 🟢 低 | 可以更具体 |
| 31 | `Context` | `ext` | `extensions` | core/context/context.go | 🟡 中 | 避免缩写 |
| 32 | `Context` | `extOnce` | `extensionsInitOnce` | core/context/context.go | 🟢 低 | 更明确意图 |
| 33 | `Context` | `matcher` | 保持 | core/context/context.go | ✅ 良好 | 清晰 |
| 34 | `Context` | `event` | 保持 | core/context/context.go | ✅ 良好 | 清晰 |

### DedupFilter 结构体

| 序号 | 结构体 | 当前字段名 | 建议字段名 | 文件位置 | 优先级 | 原因 |
|-----|--------|-----------|-----------|---------|--------|------|
| 35 | `DedupFilter` | `mu` | `cacheMutex` 或保持 | middleware/dedup.go | 🟢 低 | mu 可接受 |
| 36 | `DedupFilter` | `cache` | 保持 | middleware/dedup.go | ✅ 良好 | 清晰 |
| 37 | `DedupFilter` | `maxSize` | 保持 | middleware/dedup.go | ✅ 良好 | 清晰 |
| 38 | `DedupFilter` | `defaultTTL` | 保持 | middleware/dedup.go | ✅ 良好 | TTL 是标准缩写 |

---

## 🔧 函数和方法 (Functions & Methods)

### 构造函数

| 序号 | 当前名称 | 建议名称 | 文件位置 | 优先级 | 原因 |
|-----|---------|---------|---------|--------|------|
| 39 | `New` | `NewWithDefaults` 或 `NewBot` | factory.go | 🔴 高 | 根包中过于通用 |
| 40 | `New` (webhook) | `NewWebhook` | openapi/protocol/webhook/webhook.go | 🟡 中 | 与其他 New 混淆 |
| 41 | `New` (openapi) | `NewService` 或保持 | openapi/openapi.go | 🟢 低 | 在子包中可接受 |
| 42 | `NewBot` | 保持 | bot.go | ✅ 良好 | 标准构造函数 |
| 43 | `NewEngine` | 保持 | core/engine/engine.go | ✅ 良好 | 标准构造函数 |
| 44 | `NewWebhookAdapter` | 保持 | adapter.go | ✅ 良好 | 清晰明确 |

### 方法命名

| 序号 | 类型 | 当前方法名 | 建议方法名 | 文件位置 | 优先级 | 原因 |
|-----|------|-----------|-----------|---------|--------|------|
| 45 | `WebHook` | `EventStream` | `Events` 或保持 | adapter.go | 🟢 低 | 返回 channel 的简化命名 |
| 46 | `Bot` | `Start` | 保持 | bot.go | ✅ 良好 | 清晰 |
| 47 | `Bot` | `Shutdown` | 保持或统一为 `Stop` | bot.go | 🟡 中 | 与 Stop 混用 |
| 48 | `Adapter` | `Start` | 保持 | adapter.go | ✅ 良好 | 清晰 |
| 49 | `Adapter` | `Shutdown` | 保持或统一 | adapter.go | 🟡 中 | 与 Stop 混用 |
| 50 | `DedupFilter` | `IsDuplicate` | `CheckDuplicate` | middleware/dedup.go | 🟡 中 | Is 开头但返回 (bool, error) |
| 51 | `Context` | `SetRetryAttempt` | 保持 | core/context/context.go | ✅ 良好 | 清晰 |
| 52 | `Context` | `GetEventType` | 保持 | core/context/context.go | ✅ 良好 | 清晰 |

### 中间件函数

| 序号 | 当前名称 | 建议名称 | 文件位置 | 优先级 | 原因 |
|-----|---------|---------|---------|--------|------|
| 53 | `Logging` | 保持 | middleware/middleware.go | ✅ 良好 | 清晰 |
| 54 | `Recover` | 保持 | middleware/middleware.go | ✅ 良好 | 清晰 |
| 55 | `Auth` | 保持 | middleware/middleware.go | ✅ 良好 | 清晰 |
| 56 | `Timeout` | 保持 | middleware/middleware.go | ✅ 良好 | 清晰 |
| 57 | `Retry` | 保持 | middleware/retry.go | ✅ 良好 | 清晰 |

---

## 📊 常量 (Constants)

### 枚举类型

| 序号 | 当前名称 | 建议名称 | 文件位置 | 优先级 | 原因 |
|-----|---------|---------|---------|--------|------|
| 58 | `DropOldest` | `DropPolicyOldest` | infra/dlq/dlq.go | 🟡 中 | 缺少类型前缀 |
| 59 | `DropNewest` | `DropPolicyNewest` | infra/dlq/dlq.go | 🟡 中 | 缺少类型前缀 |
| 60 | `BlockUntilSpace` | `DropPolicyBlockUntilSpace` | infra/dlq/dlq.go | 🟡 中 | 缺少类型前缀 |
| 61 | `StateCreated` | 保持 | lifecycle/lifecycle.go | ✅ 良好 | 有清晰前缀 |
| 62 | `StateRunning` | 保持 | lifecycle/lifecycle.go | ✅ 良好 | 有清晰前缀 |
| 63 | `Healthy` | 保持 | infra/health/health.go | ✅ 良好 | 简洁明了 |
| 64 | `Unhealthy` | 保持 | infra/health/health.go | ✅ 良好 | 简洁明了 |

### 参数类型常量

| 序号 | 当前名称 | 建议名称 | 文件位置 | 优先级 | 原因 |
|-----|---------|---------|---------|--------|------|
| 65 | `ArgTypeString` | 保持 | command/enhanced_system.go | ✅ 良好 | 清晰前缀 |
| 66 | `ArgTypeInt` | 保持 | command/enhanced_system.go | ✅ 良好 | 清晰前缀 |
| 67 | `ArgTypeBool` | 保持 | command/enhanced_system.go | ✅ 良好 | 清晰前缀 |
| 68 | `ArgTypeFloat` | 保持 | command/enhanced_system.go | ✅ 良好 | 清晰前缀 |
| 69 | `ArgTypeStringSlice` | 保持 | command/enhanced_system.go | ✅ 良好 | 清晰前缀 |

### 默认值常量

| 序号 | 当前名称 | 建议名称 | 文件位置 | 优先级 | 原因 |
|-----|---------|---------|---------|--------|------|
| 70 | `DefaultTempMatcherCleanerInterval` | 保持 | core/engine/config.go | ✅ 良好 | 清晰语义 |
| 71 | `DefaultPendingDeleteBufferSize` | 保持 | core/engine/config.go | ✅ 良好 | 清晰语义 |
| 72 | `DefaultMatcherPoolCapacity` | 保持 | core/engine/config.go | ✅ 良好 | 清晰语义 |

---

## 🔤 变量命名模式 (Variable Naming Patterns)

### 常见局部变量

| 模式 | 问题 | 建议 | 使用场景 | 优先级 |
|-----|------|------|---------|--------|
| `cfg` | 缩写 | `config` | 函数参数 | 🟡 中 |
| `wh` | 缩写 | `webhook` | 所有场景 | 🟡 中 |
| `msg` | 缩写 | `message` | 所有场景 | 🟡 中 |
| `ctx` | 标准缩写 | 保持 | 标准库 context | ✅ 良好 |
| `err` | 标准缩写 | 保持 | error | ✅ 良好 |
| `ok` | 标准模式 | 保持 | map/channel 检查 | ✅ 良好 |

### 接收器命名

| 类型 | 当前接收器 | 建议接收器 | 优先级 | 原因 |
|-----|-----------|-----------|--------|------|
| `Bot` | `b` | 保持 | ✅ 良好 | 简短且清晰 |
| `Engine` | `e` | 保持 | ✅ 良好 | 简短且清晰 |
| `Context` | `ctx` | 保持 | ✅ 良好 | 标准做法 |
| `DedupFilter` | `d` | 保持或 `df` | 🟢 低 | 小范围内可接受 |
| `Manager` | `m` | 保持或 `mgr` | 🟢 低 | 小范围内可接受 |

---

## 📝 特殊命名情况

### Option 模式函数

| 序号 | 函数名 | 评价 | 文件位置 |
|-----|--------|------|---------|
| 73 | `WithConfig` | ✅ 良好 | options.go |
| 74 | `WithName` | ✅ 良好 | options.go |
| 75 | `WithVersion` | ✅ 良好 | options.go |
| 76 | `WithDebug` | ✅ 良好 | options.go |
| 77 | `WithAdapter` | ✅ 良好 | options.go |
| 78 | `WithEngine` | ✅ 良好 | options.go |
| 79 | `WithCleanupInterval` | ✅ 良好 | core/engine/config.go |

**评价**: Option 模式命名统一且规范，保持现状。

### Helper 函数

| 序号 | 函数名 | 建议 | 文件位置 | 优先级 | 原因 |
|-----|--------|------|---------|--------|------|
| 80 | `BytesToString` | 移至 `encoding` 包 | helper/helper.go | 🟡 中 | 重组包结构 |
| 81 | `StringToBytes` | 移至 `encoding` 包 | helper/helper.go | 🟡 中 | 重组包结构 |
| 82 | `HideURL` | 移至 `url` 包 | helper/helper.go | 🟡 中 | 重组包结构 |
| 83 | `FNVHash` | 移至 `hash` 包 | helper/helper.go | 🟡 中 | 重组包结构 |
| 84 | `ParseEvent` | 移至 `event` 包 | helper/helper.go | 🟡 中 | 重组包结构 |

---

## 📈 统计总结

| 类别 | 总数 | ✅ 良好 | 🔴 高优先级 | 🟡 中优先级 | 🟢 低优先级 |
|-----|-----|--------|-----------|-----------|-----------|
| 包/导入 | 6 | 0 | 1 | 5 | 0 |
| 结构体类型 | 11 | 3 | 2 | 2 | 4 |
| 接口类型 | 8 | 2 | 0 | 2 | 4 |
| 结构体字段 | 19 | 13 | 1 | 2 | 3 |
| 函数/方法 | 19 | 12 | 1 | 4 | 2 |
| 常量 | 15 | 12 | 0 | 3 | 0 |
| 变量模式 | 6 | 3 | 0 | 3 | 0 |
| **总计** | **84** | **45** | **5** | **21** | **13** |

### 百分比统计

- ✅ **良好命名**: 53.6% (45/84)
- 🔴 **高优先级改进**: 6.0% (5/84)
- 🟡 **中优先级改进**: 25.0% (21/84)
- 🟢 **低优先级优化**: 15.5% (13/84)

---

## 🎯 改进建议优先级说明

### 🔴 高优先级 (5项)
**影响**: 代码可读性和可维护性  
**建议时间**: 立即修改  
**涉及项**: #1, #7, #8, #21, #39

### 🟡 中优先级 (21项)
**影响**: 代码一致性和专业性  
**建议时间**: 下个版本  
**分批实施**: 每次 3-5 项

### 🟢 低优先级 (13项)
**影响**: 代码细节优化  
**建议时间**: 持续改进  
**实施方式**: 在代码审查时逐步完善

### ✅ 良好命名 (45项)
**评价**: 符合 Go 语言规范  
**操作**: 保持现状

---

## 🔗 相关文档

- [完整审查报告](./NAMING_REVIEW.md)
- [改进实施指南](./NAMING_IMPROVEMENTS_SUMMARY.md)

---

**最后更新**: 2026-01-23  
**文档版本**: v1.0
