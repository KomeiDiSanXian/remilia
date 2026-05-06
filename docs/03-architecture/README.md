# 架构设计

> **最后更新**: 2026-05-05

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

### [BUILTIN_PLUGINS_DESIGN.md](./BUILTIN_PLUGINS_DESIGN.md) 🔌
**内置插件系统设计**

学习：
- 插件系统架构（v2 Descriptor）
- 插件生命周期管理
- PluginInfo / ManagerWriter 权限模型
- 依赖管理与注入

**适合**: 插件开发者和架构研究者

---

### [HELP_PLUGIN_DESIGN.md](./HELP_PLUGIN_DESIGN.md) ❓
**Help 插件设计**

了解：
- 自动帮助文档生成
- 命令发现机制（Reader 只读视图）
- 文档格式化
- 扩展性设计

**适合**: 需要理解帮助系统的开发者

---

### [HANDLE_METHOD_DESIGN_ANALYSIS.md](./HANDLE_METHOD_DESIGN_ANALYSIS.md) 🎯
**Handle 方法设计分析**

深入：
- Handler 设计模式
- 错误处理策略
- 生命周期管理
- 最佳实践
- 性能考量

**适合**: 需要编写高质量 Handler 的开发者

---

### [PLUGIN_ENHANCEMENT_PROPOSAL.md](./PLUGIN_ENHANCEMENT_PROPOSAL.md) 💡
**插件增强架构参考**

记录了已实现的插件系统增强：
- PluginInfo / ManagerWriter 读写分离
- Container 依赖注入
- RegisterMultipleV2Smart 自动依赖排序
- EventBus 插件间通信

**适合**: 对框架架构设计感兴趣的开发者

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
2. [Handle 方法设计](./HANDLE_METHOD_DESIGN_ANALYSIS.md)

### 中级：深入组件设计
1. [插件系统设计](./BUILTIN_PLUGINS_DESIGN.md)
2. [命令系统集成](./COMMAND_INTEGRATION_PLAN.md)
3. [多平台抽象](./MULTI_PLATFORM.md)
4. [Context 传播模式](./CONTEXT_PROPAGATION.md)

### 高级：参与架构演进
1. [插件增强架构参考](./PLUGIN_ENHANCEMENT_PROPOSAL.md)
2. [Help 插件设计](./HELP_PLUGIN_DESIGN.md)
3. **[架构演进总览](./ARCHITECTURE_EVOLUTION.md)** — 理解"为什么"

> 每个模块的 V0→当前 详细迭代记录在 [notes/](../../notes/) 目录中（含代码对比），强烈建议结合阅读。

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
- [命令系统集成](./COMMAND_INTEGRATION_PLAN.md) - 了解 Trie 优化

### 想要开发插件？
阅读：
- [插件接口速查](../02-user-guides/PLUGIN_OPTIONAL_INTERFACES.md)
- [插件开发最佳实践](../04-development/plugin-best-practices.md)

### 想要贡献代码？
阅读：
- 所有架构文档
- [最佳实践](../02-user-guides/BEST_PRACTICES.md)

---

## 🔗 相关资源

- [用户指南](../02-user-guides/)
- [质量报告](../05-reports/)
- [主文档](../README.md)
- [示例代码](../../examples/)
