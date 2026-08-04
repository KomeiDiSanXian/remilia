# 架构设计

> **最后更新**: 2026-08-04

本目录包含 Remilia 的架构设计文档，帮助你深入理解框架内部。

---

## 📚 文档列表

### [CONCURRENT_EVENT_PROCESSING.md](./CONCURRENT_EVENT_PROCESSING.md) ⚡
**并发事件处理架构**

深入了解：
- COW (Copy-on-Write) 并发模型
- 无锁读取设计
- 事件处理流程（6 路合并机制）
- Engine 文件拆分结构
- 性能优化细节

**适合**: 想要理解高性能事件处理实现的开发者

---

### [CONTEXT_PROPAGATION.md](./CONTEXT_PROPAGATION.md) 🔗
**Context 传播模式**

详解：
- Lifecycle 分层架构（WithoutCancel + parentCtx/runCtx）
- WithContext 组件绑定模式
- 两种模式的对比与选择指南
- 最佳实践与常见错误

**适合**: 所有需要理解 Context 生命周期的开发者

---

### [HANDLE_METHOD_DESIGN_ANALYSIS.md](../notes/HANDLE_METHOD_DESIGN_ANALYSIS.md) 🎯
**Handle 方法设计分析**（内部设计讨论文档，已移入架构笔记）

---

### [MULTI_PLATFORM.md](./MULTI_PLATFORM.md) 🌐
**多平台抽象架构**

了解：
- Adapter 接口设计
- 平台注册与发现机制
- 跨平台事件规范化
- 能力声明（Capabilities）

**适合**: 需要理解多平台支持的开发者

---

### [permission-system.md](./permission-system.md) 🔒
**权限系统架构**

详解：
- RBAC 权限模型实现
- 权限检查流程
- 内置角色与自定义角色
- ACL 集成

**适合**: 需要实现权限管控的开发者

---

## 🏗️ 架构概览

### 核心组件

```
┌─────────────────────────────────────────┐
│              Bot (高层封装)               │
├─────────────────────────────────────────┤
│         Adapter (事件源适配)              │
├─────────────────────────────────────────┤
│     Engine (COW 并发事件处理引擎)         │
│  engine.go / engine_matcher_ops.go      │
│  engine_command.go / engine_query.go    │
├─────────────────────────────────────────┤
│  Middleware (中间件链: 限流/重试/降级)     │
├─────────────────────────────────────────┤
│    Matcher (规则匹配: 命令/消息/事件)      │
├─────────────────────────────────────────┤
│      Context (请求上下文 + 扩展)          │
├───────��─────────────────────────────────┤
│   Plugin System (v2 Descriptor)        │
│  descriptor / context / container      │
│  instance / reload / register          │
└─────────────────────────────────────────┘
```

### 关键设计模式

| 模式 | 应用位置 | 说明 |
|------|---------|------|
| **COW** | Engine 状态 | 读操作无锁，写时复制 |
| **Adapter** | platform/ | 统一多平台接口 |
| **Descriptor** | 插件系统 | 纯数据替代继承 |
| **洋葱模型** | 中间件链 | 请求/响应包裹 |
| **状态机** | lifecycle | 五态转换 + 回滚 |
| **Strategy** | 热重载 | 三种策略可选 |
| **Sharding** | TempManager | 8 分片减少锁粒度 |

---

## 🎯 学习路径

### 初级：理解核心流程
1. [并发事件处理](./CONCURRENT_EVENT_PROCESSING.md)
2. [Context 传播模式](./CONTEXT_PROPAGATION.md)

### 中级：深入组件设计
1. [多平台抽象](./MULTI_PLATFORM.md)
2. [权限系统架构](./permission-system.md)

### 高级：参与架构演进
1. **[架构演进总览](./ARCHITECTURE_EVOLUTION.md)** — 理解"为什么"

> 每个模块的 V0→当前 详细迭代记录在 [docs/notes/](../notes/) 目录中（含代码对比），强烈建议结合阅读。

---

### [ARCHITECTURE_EVOLUTION.md](./ARCHITECTURE_EVOLUTION.md) 📈
**架构演进总览**

阅读：
- 7 个阶段的完整演进脉络
- 五大核心架构原则
- 各模块迭代路径一览
- 架构图与设计模式总结

**适合**: 想要理解"为什么这样设计"的架构研究者

---

> 💡 每个模块的详细迭代过程（含 V0→当前 的代码对比）在独立的 `notes/` 目录中，适合作为技术博客发布。

---

## 🔍 深入研究建议

### 想要优化性能？
阅读：
- [并发事件处理](./CONCURRENT_EVENT_PROCESSING.md) - 了解 COW 优化
- [路由策略](../notes/25-routing-strategy.md) - 了解路由与索引优化

### 想要开发插件？
阅读：
- [插件文档索引](../06-plugins/README.md)
- [插件接口速查](../06-plugins/PLUGIN_OPTIONAL_INTERFACES.md)
- [插件开发最佳实践](../06-plugins/plugin-best-practices.md)

### 想要贡献代码？
阅读：
- 所有架构文档
- [最佳实践](../02-user-guides/BEST_PRACTICES.md)

---

## 🔗 相关资源

- [用户指南](../02-user-guides/)
- [插件文档](../06-plugins/)
- [主文档](../README.md)
- [示例代码](../../examples/)
