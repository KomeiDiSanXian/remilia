# Remilia v1.0.0 发布前问题清单

> **生成时间**: 2026-03-09  
> **最后更新**: 2026-03-09（全部问题已处理）  
> **适用版本**: v1.0.0  
> **状态**: ✅ 满足发布条件

本文档记录在 v1.0.0 正式发布前，通过代码审查发现的所有需要关注或修复的问题。  
按优先级分为三级：**P0（阻塞发布）**、**P1（强烈建议修复）**、**P2（改进建议）**。

---

## 目录

- [P0 — 阻塞发布问题](#p0--阻塞发布问题)
- [P1 — 强烈建议修复](#p1--强烈建议修复)
- [P2 — 改进建议](#p2--改进建议)
- [问题统计](#问题统计)

---

## P0 — 阻塞发布问题

### ✅ P0-1：`go.mod` 声明了未发布的 Go 版本

| 属性 | 内容 |
|------|------|
| **文件** | `go.mod:3` |
| **问题** | `go 1.25.0` — Go 1.25 截至发布时尚未正式发布，部分 CI 环境无法构建 |
| **修复** | 已改为 `go 1.24.0` |

---

### ✅ P0-2：README 示例引用了不存在的函数签名 `NewWebhookAdapter(addr, secret)`

| 属性 | 内容 |
|------|------|
| **文件** | `README.md` |
| **问题** | 示例写的是 `remilia.NewWebhookAdapter(":8080", "your-secret")`，实际该函数只接受一个 `Webhook` 接口参数 |
| **修复** | 已改为 `remilia.SimpleWebhookAdapter(8080)` |

---

### ✅ P0-3：README 示例引用了不存在的函数 `NewBotFromConfig`

| 属性 | 内容 |
|------|------|
| **文件** | `README.md` |
| **问题** | `remilia.NewBotFromConfig(cfg)` 在整个代码库中不存在 |
| **修复** | 已改为正确的 `NewBotBuilder().WithBotInfo(...).WithWebhook(...).Build()` 调用 |

---

### ✅ P0-4：README 声称日志基于 `logrus`，实际使用 `zerolog`

| 属性 | 内容 |
|------|------|
| **文件** | `README.md` |
| **问题** | 「基于 logrus 的结构化日志」描述有误，实际依赖为 `github.com/rs/zerolog` |
| **修复** | 已改为「基于 zerolog 的结构化日志」 |

---

### ✅ P0-5：`NewWebhookAdapterWithServer` 接收 `secret` 参数但完全忽略（安全漏洞）

| 属性 | 内容 |
|------|------|
| **文件** | `adapter.go` |
| **问题** | secret 参数被丢弃并留有 `// TODO` 注释，底层 ed25519 签名验证未被启用 |
| **修复** | 已将 secret 写入 `BotInfo.AppSecret` 并透传给 `WebhookServerAdapter`，底层签名验证生效；移除 TODO 注释 |

---

## P1 — 强烈建议修复

### ✅ P1-1：`NewBot` 使用 `logger.Panic` 而非返回错误

| 属性 | 内容 |
|------|------|
| **文件** | `bot.go` |
| **问题** | 公共构造函数在 nil 参数时直接 panic，破坏「构造函数不应 panic」的 Go 惯例 |
| **修复** | 已为 `NewBot` 添加 GoDoc，明确说明 nil 时会 panic，并推荐使用 `BotBuilder.Build()` |

---

### ✅ P1-2：`ProcessEventBatch` 中 `ReleaseContext` 在 panic 路径可能被跳过

| 属性 | 内容 |
|------|------|
| **文件** | `core/engine/process.go` |
| **问题** | 若 `invokeHandler` 内部 panic，循环末尾的 `ReleaseContext` 会被跳过，高并发下导致对象池内存泄漏 |
| **修复** | 已将每次循环迭代包装在匿名函数中，使用 `defer context.ReleaseContext(ctx)` 保证任何路径下都能回收 |

---

### ✅ P1-3：`getMatchersForEvent` 是语义不一致的死代码

| 属性 | 内容 |
|------|------|
| **文件** | `core/engine/process.go` |
| **问题** | 方法逻辑不完整（无命令索引、无排序、无 TempMatcher），与正式处理路径不一致，仅在测试中直接调用 |
| **修复** | 已添加 `Deprecated` GoDoc，明确标注「仅供测试/调试使用，不保证与 ProcessEvent 行为一致」 |

---

### ✅ P1-4：`KafkaConsumer.Consume` 是未完成的占位实现

| 属性 | 内容 |
|------|------|
| **文件** | `infra/dlq/consumers.go`、`config/validate.go` |
| **问题** | 调用后静默丢弃死信消息；配置层不拒绝 `target: kafka`，用户无感知数据丢失 |
| **修复** | ① `Consume` 改为打印 ERROR 级别日志，注明「消息已丢失，请勿在生产环境使用」；② 添加 `Deprecated` GoDoc；③ `config.Validate` 对 `target: kafka` 返回明确错误，阻止配置生效；④ 同步更新受影响的测试断言 |

---

### ✅ P1-5：`BotManager.MustGet` / `MustAdd` 的 panic 语义未限制使用场景

| 属性 | 内容 |
|------|------|
| **文件** | `bot_manager.go` |
| **问题** | 两个方法在错误时直接 panic，GoDoc 未说明「仅适用于初始化阶段」 |
| **修复** | 已在两个方法的 GoDoc 中明确写明：「仅适用于 main() 初始化阶段，运行时请使用 Add/Get」 |

---

### ✅ P1-6：`openapi/dto/event.go` 缺失 Channel 频道事件类型

| 属性 | 内容 |
|------|------|
| **文件** | `openapi/dto/event.go` |
| **问题** | 文件末尾有 `// TODO: Add channel event`，频道消息事件类型长期缺失 |
| **修复** | 已补全 12 个频道 `EventType` 常量（`GuildCreate/Update/Delete`、`GuildMember*`、`Channel*`、`AtMessageCreate`、`DirectMessageCreate`）及对应的 8 个事件结构体；移除 TODO 注释 |

---

### ✅ P1-7：`config/config.go` 全局包级变量与并行测试不兼容

| 属性 | 内容 |
|------|------|
| **文件** | `config/config.go` |
| **问题** | 包级 `listenerEntries` 等全局变量导致并行测试互相干扰 |
| **修复** | 已在变量声明处和 `UnsubscribeAll()` 添加注释，明确说明：并行测试须调用 `t.Cleanup(config.UnsubscribeAll)` 进行清理 |

---

### ⚠️ P1-8：`infra/server` 和 `infra/tracing` 缺失测试

| 属性 | 内容 |
|------|------|
| **文件** | `infra/server/`、`infra/tracing/` |
| **问题** | 两个基础设施包均无测试文件，回归风险高 |
| **状态** | 已记录，补充基础冒烟测试纳入 **v1.0.1** 计划 |

---

### ✅ P1-9：多处文档中残留 `logrus` 的过时代码片段

| 属性 | 内容 |
|------|------|
| **文件** | `infra/logger/TESTING.md`、`infra/dlq/TESTING.md`、`examples/plugin-example/README.md` |
| **问题** | 多个文档中的示例代码仍使用 `logrus.XXX` API，与实际使用 `zerolog` 的代码不符 |
| **修复** | 已将上述文档中所有 `logrus.*` 引用替换为 `zerolog` / `infra/logger` API |

---

## P2 — 改进建议

### ⚠️ P2-1：`go.mod` 中同时引入了两套 YAML 库

| 属性 | 内容 |
|------|------|
| **文件** | `go.mod` |
| **问题** | 同时依赖 `gopkg.in/yaml.v3`（直接使用）和 `go.yaml.in/yaml/v3`（viper 间接引入），轻微增大二进制体积 |
| **状态** | 功能正常；统一为单套 YAML 库纳入 **v1.1.0** 依赖整理 |

---

### ⚠️ P2-2：`degradation.go` 多实例场景下指标共享问题

| 属性 | 内容 |
|------|------|
| **文件** | `middleware/degradation.go` |
| **问题** | 多实例时共享同一套 Prometheus 指标，与 `metrics.NewMetricsCollector` 使用独立 Registry 的设计原则不一致 |
| **状态** | 现有实现功能正常；多实例独立 Registry 纳入 **v1.1.0** 优化 |

---

### ✅ P2-3：`doc.go` 中示例 API 无法编译

| 属性 | 内容 |
|------|------|
| **文件** | `doc.go` |
| **问题** | 示例使用了不存在的 `eng.OnC2C`、`ctx.ReplyPrivate` API |
| **修复** | 已重写为当前可用的 `eng.OnCommand` + `BotBuilder` API，示例可直接编译 |

---

### ✅ P2-4：性能数据的测试环境 Go 版本过旧

| 属性 | 内容 |
|------|------|
| **文件** | `README.md` |
| **问题** | 性能表格注明「Go 1.21」，安装要求也写的 `Go 1.21+` |
| **修复** | 已将「Go 1.21」更新为「Go 1.24」；安装要求同步更新为 `Go 1.24+` |

---

### ✅ P2-5：`CHANGELOG.md` 缺少版本比较链接

| 属性 | 内容 |
|------|------|
| **文件** | `CHANGELOG.md` |
| **问题** | 不符合 Keep a Changelog 规范，版本号无 GitHub Compare URL |
| **修复** | 已在末尾添加 `[Unreleased]` 和 `[1.0.0]` 的 GitHub 比较链接 |

---

### ✅ P2-6：`WebhookServerAdapter.Start` workers 启动等待有硬编码 500ms 超时

| 属性 | 内容 |
|------|------|
| **文件** | `webhook_adapter.go` |
| **问题** | `time.After(500ms)` 硬编码超时，慢速 CI/ARM 环境下 workers 未就绪即接收请求 |
| **修复** | 已移除硬编码超时，改为直接等待 `workersReady` channel（由父 ctx 统一控制超时） |

---

### ✅ P2-7：`infra/dlq/TESTING.md` 中依赖列表包含已废弃的 `logrus`

| 属性 | 内容 |
|------|------|
| **文件** | `infra/dlq/TESTING.md` |
| **修复** | 已更新为 `github.com/rs/zerolog` |

---

### ✅ P2-8：`examples/` 目录中多个示例未在 README 主文档中列出

| 属性 | 内容 |
|------|------|
| **文件** | `README.md` |
| **问题** | 主文档示例列表只有 6 项，实际 examples 目录有 19 个子目录 |
| **修复** | 已将示例列表扩充为 19 项，与 `examples/` 目录实际内容对齐，并引导至 `examples/README.md` |

---

### ✅ P2-9：`infra/tracing`、`infra/audit` 等包缺少 package-level 文档注释

| 属性 | 内容 |
|------|------|
| **文件** | `infra/tracing/tracing.go`、`infra/audit/audit.go` |
| **修复** | 已为两个包添加完整的 package doc 注释（含功能说明和用法示例） |

---

### ✅ P2-10：`i18n` 插件 `lru.New` 错误处理注释不准确

| 属性 | 内容 |
|------|------|
| **文件** | `plugins/i18n/i18n.go` |
| **问题** | 注释写「仅当 size <= 0 时出错」，未说明此处实际永不触发 |
| **修复** | 已更新注释为「templateCacheSize 为正数常量，此处实际永不触发」，消除歧义 |

---

### ⚠️ P2-11：`WebhookServerAdapter.GetHealth()` 未反映实际运行状态

| 属性 | 内容 |
|------|------|
| **文件** | `webhook_adapter.go` |
| **问题** | 健康检查未返回 HTTP server 实际监听状态、worker 数、event drop rate 等动态指标 |
| **状态** | 已记录，动态健康指标纳入 **v1.0.1** 计划 |

---

### ✅ P2-12：`examples/plugin-example/README.md` 使用已废弃的 v1 插件 API

| 属性 | 内容 |
|------|------|
| **文件** | `examples/plugin-example/README.md` |
| **问题** | 大量 `BasePlugin`、`logrus` 的 v1 API 示例，v1 API 已在 v1.0.0 中完全移除 |
| **修复** | 已重写全部代码说明部分，改为 v2 `PluginDescriptor` API；移除所有 logrus 引用 |

---

### ✅ P2-13：`tests/integration/e2e_test.go` 中集成测试跳过逻辑不清晰

| 属性 | 内容 |
|------|------|
| **文件** | `tests/integration/e2e_test.go` |
| **问题** | `t.Skip("暂时跳过")` + 大量注释代码，未说明启用条件 |
| **修复** | 已改为环境变量守卫（`E2E_BOT_TOKEN`），并注明「设置该变量即可启用端到端测试」 |

---

## 问题统计

| 优先级 | 数量 | 状态 |
|--------|------|------|
| **P0** | 5 个 | ✅ 全部已修复 |
| **P1** | 9 个 | ✅ 已修复 8 个；P1-8 补测试纳入 v1.0.1 |
| **P2** | 13 个 | ✅ 已修复 10 个；P2-1/2/11 纳入后续版本 |
| **合计** | **27 个** | ✅ **满足发布条件** |

---

## 遗留至 v1.0.1 的后续工作

| 编号 | 内容 | 优先级 |
|------|------|--------|
| P1-8 | 为 `infra/server`、`infra/tracing` 补充基础单元测试 | 高 |
| P2-11 | `WebhookServerAdapter.GetHealth()` 返回实际运行状态动态指标 | 中 |
| P2-1 | 统一 YAML 库（`gopkg.in/yaml.v3` → `go.yaml.in/yaml/v3`） | 低 |
| P2-2 | `AdaptiveDegradation` 多实例场景下使用独立 Prometheus Registry | 低 |

---

*本文档由代码审查生成并已完成修复确认。如有新问题请通过 GitHub Issues 补充。*

