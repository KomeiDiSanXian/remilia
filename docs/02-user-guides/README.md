# 用户指南

> **最后更新**: 2026-08-04

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
👉 阅读 [插件文档索引](../06-plugins/README.md)

#### 快速查阅插件 API
👉 阅读 [插件接口速查](../06-plugins/PLUGIN_OPTIONAL_INTERFACES.md)

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
1. [插件开发指南](../06-plugins/PLUGIN_DEVELOPMENT_GUIDE.md)
2. [插件接口速查](../06-plugins/PLUGIN_OPTIONAL_INTERFACES.md)
3. [插件开发最佳实践](../06-plugins/plugin-best-practices.md)

### 进阶路径
1. [Matcher 链式调用](./MATCHER_CHAINING_BEST_PRACTICES.md)
2. [配置热更新](./CONFIG_HOTRELOAD_QUICKREF.md)
3. [并发事件处理](../03-architecture/CONCURRENT_EVENT_PROCESSING.md)

---

## 🔗 相关资源

- [快速开始](../01-getting-started/)
- [架构设计](../03-architecture/)
- [插件文档](../06-plugins/)
- [主文档](../README.md)
