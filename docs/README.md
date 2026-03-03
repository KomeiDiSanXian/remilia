# Remilia 文档中心

**项目**: Remilia — 高性能 QQ 机器人框架  
**最后更新**: 2026-02-25  
**文档版本**: v2.0

---

## 📚 文档导航

### 🚀 [01-getting-started](./01-getting-started/) — 快速开始

新手必读，帮助你快速上手 Remilia。

- **[GETTING_STARTED.md](./01-getting-started/GETTING_STARTED.md)** — 快速入门指南
  - 安装配置
  - 创建第一个机器人（v2 API）
  - 核心概念介绍
  - 中间件速查

- **[TROUBLESHOOTING.md](./01-getting-started/TROUBLESHOOTING.md)** — 故障排除指南
  - 常见问题解答
  - 调试技巧
  - 错误处理

---

### 📖 [02-user-guides](./02-user-guides/) — 用户指南

使用 Remilia 的最佳实践和参考手册。

- **[BEST_PRACTICES.md](./02-user-guides/BEST_PRACTICES.md)** ⭐ — 最佳实践指南
  - 项目结构 / 错误处理 / 并发控制
  - `ctx.Set` vs `ctx.Delete` 行为说明
  - 插件 v2 开发模式

- **[PLUGIN_V1_TO_V2_MIGRATION.md](./02-user-guides/PLUGIN_V1_TO_V2_MIGRATION.md)** — Plugin v2 快速上手
  - `PluginDescriptor` 完整字段说明
  - `SetupContext` 所有字段（Reg / Log / Info / Admin / Config / EventBus / Go / DryRun）
  - 三种注册方式（RegisterV2 / Atomic / Smart）
  - 完整示例

- **[PLUGIN_OPTIONAL_INTERFACES.md](./02-user-guides/PLUGIN_OPTIONAL_INTERFACES.md)** — 插件接口速查
  - PluginInfo / ManagerWriter / TeardownContext / Advanced

- **[PLUGIN_ENHANCEMENTS_GUIDE.md](./02-user-guides/PLUGIN_ENHANCEMENTS_GUIDE.md)** — 插件功能速查
  - 配置 / 状态查询 / 管理操作 / 事件总线 / Engine 只读视图

- **[PLUGIN_HELP_SYSTEM.md](./02-user-guides/PLUGIN_HELP_SYSTEM.md)** — 插件帮助系统
  - PluginMeta / GetAllCommands / 自定义 Help 实现

- **[MATCHER_CHAINING_BEST_PRACTICES.md](./02-user-guides/MATCHER_CHAINING_BEST_PRACTICES.md)** — Matcher 链式调用
- **[CONFIGURATION_QUICKREF.md](./02-user-guides/CONFIGURATION_QUICKREF.md)** — 配置快速参考（含 WithMaxMatchers / degradation 热更新阈值）
- **[CONFIG_HOTRELOAD_QUICKREF.md](./02-user-guides/CONFIG_HOTRELOAD_QUICKREF.md)** — 配置热更新（含 Bridge.WatchDedup / WatchDegradation）
- **[ERROR_HANDLING.md](./02-user-guides/ERROR_HANDLING.md)** — 错误处理指南
- **[tracing.md](./02-user-guides/tracing.md)** — 链路追踪
- **[access-control-list.md](./02-user-guides/access-control-list.md)** — 访问控制列表
- **[verification-code-system.md](./02-user-guides/verification-code-system.md)** — 验证码系统

---

### 🏗️ [03-architecture](./03-architecture/) — 架构设计

Remilia 的架构设计文档，深入理解框架内部。

- **[CONCURRENT_EVENT_PROCESSING.md](./03-architecture/CONCURRENT_EVENT_PROCESSING.md)** — 并发事件处理架构
  - COW 并发模型 / 6 路合并机制 / Engine 文件拆分

- **[BUILTIN_PLUGINS_DESIGN.md](./03-architecture/BUILTIN_PLUGINS_DESIGN.md)** — 插件系统设计
  - v2 PluginDescriptor / PluginInfo / ManagerWriter 权限模型

- **[HELP_PLUGIN_DESIGN.md](./03-architecture/HELP_PLUGIN_DESIGN.md)** — Help 插件设计
- **[HANDLE_METHOD_DESIGN_ANALYSIS.md](./03-architecture/HANDLE_METHOD_DESIGN_ANALYSIS.md)** — Handle 方法设计分析
- **[PLUGIN_ENHANCEMENT_PROPOSAL.md](./03-architecture/PLUGIN_ENHANCEMENT_PROPOSAL.md)** — 插件增强架构参考（已实现）
- **[COMMAND_INTEGRATION_PLAN.md](./03-architecture/COMMAND_INTEGRATION_PLAN.md)** — 命令系统集成

---

### 🔧 [04-development](./04-development/) — 开发指南

- **[plugin-best-practices.md](./04-development/plugin-best-practices.md)** ⭐ — 插件开发最佳实践
  - PluginDescriptor 结构规范 / DryRun 保护 / Privileged 声明 / 测试

---

### 📊 [05-reports](./05-reports/) — 质量报告

- **[core-middleware-design-review.md](06-archived/core-middleware-design-review.md)** — Core & Middleware 设计评审（P0/P1/P2 全部完成）

---

### 📦 [06-archived](./06-archived/) — 归档文档

历史设计文档、迁移过程记录、已解决问题的分析报告。不再维护，仅供历史参考。

---

## 🎯 快速导航

| 我想做... | 阅读 |
|----------|------|
| 创建第一个 Bot | [快速入门](./01-getting-started/GETTING_STARTED.md) |
| 开发插件（从零） | [Plugin v2 快速上手](./02-user-guides/PLUGIN_V1_TO_V2_MIGRATION.md) |
| 查阅插件 API | [插件接口速查](./02-user-guides/PLUGIN_OPTIONAL_INTERFACES.md) |
| 了解最佳实践 | [最佳实践](./02-user-guides/BEST_PRACTICES.md) |
| 配置机器人 | [配置快速参考](./02-user-guides/CONFIGURATION_QUICKREF.md) |
| 了解框架架构 | [并发事件处理](./03-architecture/CONCURRENT_EVENT_PROCESSING.md) |
| 查看设计评审 | [Core & Middleware 评审](06-archived/core-middleware-design-review.md) |

---

## 🏗️ 核心架构速览

```
Bot
 └── Adapter (Webhook)
      └── Engine (COW)
           ├── engine.go              — 核心结构 / NewEngine / Shutdown
           ├── engine_matcher_ops.go  — Matcher 生命周期写操作
           ├── engine_command.go      — 命令注册与查询
           └── engine_query.go        — 只读统计 / Snapshot
 └── Plugin System (v2)
      ├── descriptor.go   — PluginDescriptor / PluginAdvanced
      ├── context.go      — SetupContext / Must / Optional / Try
      ├── container.go    — 依赖注入容器
      ├── instance.go     — 运行时实例
      ├── reload.go       — 热重载策略
      └── register.go     — RegisterV2 / Smart / 拓扑排序
 └── Middleware
      ├── Recover         — 自适应堆栈缓冲 captureStack()
      ├── Timeout         — context deadline 注入（非 goroutine）
      ├── SimpleRateLimit — 全局固定限流
      ├── AdaptiveDegradation — 每实例 Prometheus 指标
      └── hotreload.Bridge — WatchDedup / WatchDegradation
```
