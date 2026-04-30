# 用户指南

> **最后更新**: 2026-02-25

本目录包含 Remilia 的使用指南和最佳实践。

---

## 📚 文档列表

### [BEST_PRACTICES.md](./BEST_PRACTICES.md) ⭐
**最佳实践指南**

涵盖：
- 项目结构规范
- 错误处理模式
- 并发控制（SimpleRateLimit / 自适应限流 / 按 key 限流选型）
- Context 数据操作（Set vs Delete）
- 性能优化建议
- 安全性考虑
- 插件 v2 开发模式

**适合**: 所有用户，特别是准备上线的开发者

---

### [PLUGIN_V1_TO_V2_MIGRATION.md](./PLUGIN_V1_TO_V2_MIGRATION.md) 🔌
**Plugin v2 快速上手**

学习：
- Descriptor 完整字段说明
- SetupContext 所有字段（Reg / Log / Info / Admin / Config / EventBus / Go / DryRun）
- 三种注册方式（Register / RegisterMultipleAtomic / RegisterMultipleSmart）
- 依赖获取（Require / Optional / MustAs）
- 完整示例：天气插件

**适合**: 所有插件开发者（v1 已移除，本文是唯一入口）

---

### [PLUGIN_OPTIONAL_INTERFACES.md](./PLUGIN_OPTIONAL_INTERFACES.md) 📋
**插件接口速查**

速查：
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

包含：
- 配置管理（ctx.Config）
- 插件状态查询（ctx.Info）
- 管理操作（ctx.Admin）
- 插件间事件总线（ctx.EventBus）
- Engine 只读视图（ctx.Info.Coordinator()）

**适合**: 需要查阅插件高级功能的开发者

---

### [PLUGIN_HELP_SYSTEM.md](./PLUGIN_HELP_SYSTEM.md) ❓
**插件帮助系统**

了解：
- Metadata 字段说明
- Help 插件命令发现（Reader / GetAllCommands）
- 自定义 Help 插件实现
- 最佳实践

**适合**: 需要为插件添加帮助文档的开发者

---

### [MATCHER_CHAINING_BEST_PRACTICES.md](./MATCHER_CHAINING_BEST_PRACTICES.md) 🔗
**Matcher 链式调用最佳实践**

学习：
- 中间件模式
- 责任链模式
- Handler 组合
- 性能考虑
- 常见陷阱

**适合**: 需要构建复杂事件处理逻辑的开发者

---

### [CONFIGURATION_QUICKREF.md](./CONFIGURATION_QUICKREF.md) ⚙️
**配置快速参考**

包含：
- 所有配置项说明（含 WithMaxMatchers / degradation 阈值）
- 示例配置文件
- Engine / Middleware / 正则缓存调优
- 常用配置场景

**适合**: 需要配置机器人的开发者

---

### [CONFIG_HOTRELOAD_QUICKREF.md](./CONFIG_HOTRELOAD_QUICKREF.md) 🔄
**配置热更新快速参考**

了解：
- 热更新机制
- Bridge API（WatchDedup / WatchDegradation）
- 回调函数编写
- 注意事项和限制

**适合**: 需要动态更新配置的开发者

---

### [ERROR_HANDLING.md](./ERROR_HANDLING.md) ⚠️
**错误处理指南**

**适合**: 需要深入了解错误处理的开发者

---

### [tracing.md](./tracing.md) 📊
**链路追踪**

**适合**: 需要接入分布式追踪的开发者

---

### [access-control-list.md](./access-control-list.md) 🔒
**访问控制列表（ACL）**

**适合**: 需要实现权限管控的开发者

---

### [verification-code-system.md](./verification-code-system.md) 🔑
**验证码系统**

**适合**: 需要人机验证功能的开发者

---

## 🎯 使用场景导航

### 我想...

#### 编写高质量代码
👉 阅读 [BEST_PRACTICES.md](./BEST_PRACTICES.md)

#### 开发一个插件（从零开始）
👉 阅读 [PLUGIN_V1_TO_V2_MIGRATION.md](./PLUGIN_V1_TO_V2_MIGRATION.md)

#### 快速查阅插件 API
👉 阅读 [PLUGIN_OPTIONAL_INTERFACES.md](./PLUGIN_OPTIONAL_INTERFACES.md)

#### 构建复杂的事件处理逻辑
👉 阅读 [MATCHER_CHAINING_BEST_PRACTICES.md](./MATCHER_CHAINING_BEST_PRACTICES.md)

#### 配置我的机器人
👉 阅读 [CONFIGURATION_QUICKREF.md](./CONFIGURATION_QUICKREF.md)

#### 实现配置动态更新
👉 阅读 [CONFIG_HOTRELOAD_QUICKREF.md](./CONFIG_HOTRELOAD_QUICKREF.md)

---

## 📖 推荐阅读顺序

### 新手路径
1. [快速入门](../01-getting-started/GETTING_STARTED.md)
2. [最佳实践](./BEST_PRACTICES.md)
3. [配置快速参考](./CONFIGURATION_QUICKREF.md)

### 插件开发路径
1. [Plugin v2 快速上手](./PLUGIN_V1_TO_V2_MIGRATION.md)
2. [插件接口速查](./PLUGIN_OPTIONAL_INTERFACES.md)
3. [插件开发最佳实践](../04-development/plugin-best-practices.md)

### 进阶路径
1. [Matcher 链式调用](./MATCHER_CHAINING_BEST_PRACTICES.md)
2. [配置热更新](./CONFIG_HOTRELOAD_QUICKREF.md)
3. [并发事件处理](../03-architecture/CONCURRENT_EVENT_PROCESSING.md)

---

## 🔗 相关资源

- [快速开始](../01-getting-started/)
- [架构设计](../03-architecture/)
- [开发指南](../04-development/)
- [质量报告](../05-reports/)
- [主文档](../README.md)
