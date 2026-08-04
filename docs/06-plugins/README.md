# 插件文档

> **最后更新**: 2026-08-04

本目录汇集 Remilia 插件的**开发**与**使用**文档：从零编写插件、应用级内置插件的使用方式、插件增强能力与 WASM 跨语言插件。

---

## 📚 文档列表

### [PLUGIN_DEVELOPMENT_GUIDE.md](./PLUGIN_DEVELOPMENT_GUIDE.md) 🔌
**插件开发指南**

- Descriptor 完整字段说明
- SetupContext 所有字段（Reg / Log / Info / Admin / Config / EventBus / Spawn / SpawnNamed / NewTaskGroup / DryRun）
- 三种注册方式（Register / RegisterBatch / RegisterBatch + WithInferDeps）
- 依赖获取（Service[T] / TryService[T] / ExportIface[T]）
- 完整示例：天气插件

**适合**: 所有插件开发者

---

### [PLUGIN_OPTIONAL_INTERFACES.md](./PLUGIN_OPTIONAL_INTERFACES.md) 📋
**插件接口速查**

- Descriptor / Metadata 结构
- SetupContext 字段表格
- PluginInfo 只读查询接口
- ManagerWriter 管理写视图
- TeardownContext
- Advanced 高级选项（热重载策略 / SaveState）
- goroutine 生命周期绑定
- DryRun 保护

**适合**: 需要快速查阅 API 签名的开发者

---

### [PLUGIN_ENHANCEMENTS_GUIDE.md](./PLUGIN_ENHANCEMENTS_GUIDE.md) 🛠️
**插件系统功能速查**

- 配置管理（ctx.Config）
- 插件状态查询（ctx.Info）
- 管理操作（ctx.Admin）
- 事件总线（ctx.EventBus / ctx.Scope().Subscribe）
- Engine 只读视图（ctx.Info.Coordinator）
- 资源追踪（Scope / OnDispose）
- 防过期服务代理（ServiceProxy）
- 状态迁移（SaveState / MigrateState / RestoreState）
- 定时任务（RegisterCron / After）
- 出站消息观察者（OutboundObserver）

---

### [COMMAND_SYSTEM.md](./COMMAND_SYSTEM.md) ⌨️
**命令系统**

- Definition 与 NewDef Builder（参数/标志/子命令/别名）
- 注册命令（RegisterCommand + SetDefinition / OnCommandDef）
- 解析（ParseCommandLine / ParseFromDefinition / Parsed）
- handler 中读取参数与标志

---

### [INFRA_TOOLKIT.md](./INFRA_TOOLKIT.md) 🧰
**infra 工具包**

- 存储：kv（LevelDB）/ storage（GORM）/ persist
- 异步与并发：future / atomic / syncx / pool
- 可观测性：logger / tracing / metrics / health / audit
- 网络与基础设施：httpclient / dlq / server / option / trie / expr 等

---

### [PLUGIN_HELP_SYSTEM.md](./PLUGIN_HELP_SYSTEM.md) 📖
**插件帮助系统**

- /help 集成与命令元数据
- 命令定义（Definition）与分类
- 隐藏命令 / 权限要求

---

### [plugin-best-practices.md](./plugin-best-practices.md) ⭐
**插件开发最佳实践**

- 插件文件结构规范（v2 Descriptor）
- 依赖声明与 Smart 注册
- 错误处理模式
- 后台 goroutine（ctx.Spawn）与并发任务组（ctx.NewTaskGroup）
- DryRun 保护
- Privileged 管理类插件
- 测试（plugintest 包）
- ctx.Set vs ctx.Delete 行为

**适合**: 所有插件开发者

---

### [BUILTIN_PLUGINS.md](./BUILTIN_PLUGINS.md) 🧩
**内置插件指南**

- 内容安全（antispam / keywordfilter / moderation）
- 自动化（autoresponder / broadcast / subscription / scheduler / job / customcommands）
- 用户互动（cooldown / verifycode / vevent）
- 管理与观测（auditlog / logviewer / ratelimitui / pluginstore / i18n）
- 消息通道（sendqueue / messagelog）

---

### [APP_PLUGINS.md](./APP_PLUGINS.md) 🚀
**应用级插件指南**

随 `cmd/bot` 与应用发行版附带的插件使用方式：updater / pic / sauce / welcome / messagelog / about 等，含完整配置参考（`plugins.<name>` 节）。

---

### [AI_PLUGIN.md](./AI_PLUGIN.md) 🤖
**AI 对话插件**

- `/ai` 命令与子命令（reset / undo / retry / summary / status / stats / tools / skill）
- 配置（provider / model / vision 等）
- 工具调用（自动发现 + 显式注册优先）
- 自定义技能管理

---

### [wasm-plugin-development.md](./wasm-plugin-development.md) 🆕
**WASM 跨语言插件开发**

- ABI 合约（导出/导入/TLV 序列化）
- TinyGo 插件开发（含最小示例）
- Rust / C 等其他语言 ABI 模板
- 安全模型与资源限制
- 宿主集成代码示例

**适合**: 需要跨语言插件的开发者

---

## 🎯 学习路径

1. **入门**: [插件开发指南](./PLUGIN_DEVELOPMENT_GUIDE.md)
2. **进阶**: [插件接口速查](./PLUGIN_OPTIONAL_INTERFACES.md)
3. **规范**: [插件开发最佳实践](./plugin-best-practices.md)
4. **使用内置插件**: [应用级插件指南](./APP_PLUGINS.md)
5. **跨语言**: [WASM 插件开发](./wasm-plugin-development.md)

---

## 🔗 相关资源

- [用户指南](../02-user-guides/)
- [架构设计](../03-architecture/)
- [示例代码](../../examples/)
