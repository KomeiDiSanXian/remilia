# Remilia 文档

欢迎使用 Remilia 文档！本目录包含框架的完整文档。

## 📖 文档导航

### 🚀 新手入门

- [快速开始指南](./01-getting-started/GETTING_STARTED.md) — 10 分钟上手
- [故障排除](./01-getting-started/TROUBLESHOOTING.md) — 常见问题解答

### 🔌 插件文档

- [插件文档索引](./06-plugins/README.md) — 开发与使用总览
- [插件开发指南](./06-plugins/PLUGIN_DEVELOPMENT_GUIDE.md) — 从零开始写插件
- [插件接口速查](./06-plugins/PLUGIN_OPTIONAL_INTERFACES.md) — API 签名速查
- [插件增强能力](./06-plugins/PLUGIN_ENHANCEMENTS_GUIDE.md) — 只读协调器、事件总线等
- [命令系统](./06-plugins/COMMAND_SYSTEM.md) — 命令定义与解析
- [infra 工具包](./06-plugins/INFRA_TOOLKIT.md) — kv/future/logger/httpclient 等基础设施
- [插件帮助系统](./06-plugins/PLUGIN_HELP_SYSTEM.md) — /help 集成与命令元数据
- [插件开发最佳实践](./06-plugins/plugin-best-practices.md) — 规范与模式
- [内置插件指南](./06-plugins/BUILTIN_PLUGINS.md) — antispam / moderation / broadcast 等 30+ 内置插件
- [应用级插件指南](./06-plugins/APP_PLUGINS.md) — updater / pic / sauce / welcome 等
- [AI 对话插件](./06-plugins/AI_PLUGIN.md) — /ai 对话、工具调用与自定义技能
- [WASM 插件开发](./06-plugins/wasm-plugin-development.md) — 沙箱插件

### 📖 用户指南

- [最佳实践](./02-user-guides/BEST_PRACTICES.md) — 推荐的使用模式
- [FSM 状态机指南](./02-user-guides/FSM_GUIDE.md) — 声明式多步骤对话
- [Matcher 链式调用](./02-user-guides/MATCHER_CHAINING_BEST_PRACTICES.md) — 高级模式
- [工厂函数指南](./02-user-guides/FACTORY_FUNCTIONS_GUIDE.md) — Bot 组装方式
- [错误处理](./02-user-guides/ERROR_HANDLING.md) — 错误分类与处理策略
- [配置快速参考](./02-user-guides/CONFIGURATION_QUICKREF.md) — 配置项速查
- [配置热更新](./02-user-guides/CONFIG_HOTRELOAD_QUICKREF.md) — Bridge API
- [API 服务器](./02-user-guides/API_SERVER.md) — 管理 REST API 端点
- [访问控制列表](./02-user-guides/access-control-list.md) — ACL 使用
- [验证码系统](./02-user-guides/verification-code-system.md) — 验证流程
- [链路追踪](./02-user-guides/tracing.md) — OpenTelemetry 接入

### 🏗️ 架构设计

- [并发事件处理](./03-architecture/CONCURRENT_EVENT_PROCESSING.md) — COW 并发模型
- [多平台抽象](./03-architecture/MULTI_PLATFORM.md) — 跨平台适配器设计
- [权限系统架构](./03-architecture/permission-system.md) — RBAC 权限模型
- [Context 传播模式](./03-architecture/CONTEXT_PROPAGATION.md) — 生命周期与上下文
- [架构演进总览](./03-architecture/ARCHITECTURE_EVOLUTION.md) — 7 个阶段演进 + 设计原则
- [架构笔记](./notes/) 📝 — 各模块 V0→当前 详细迭代记录（含代码对比，适合博客）

### ⚡ 性能

- [性能报告](./05-performance/PERFORMANCE_REPORT.md) — 吞吐/延迟/内存基准

---

*完整文档持续更新中。如有问题请提交 [GitHub Issue](https://github.com/KomeiDiSanXian/remilia/issues)。*
