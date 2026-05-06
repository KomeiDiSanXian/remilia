# 架构演进总览

> **最后更新**: 2026-05-05

本文档从宏观视角梳理 Remilia 框架的架构演进历程、核心设计原则和各模块的迭代路径。帮助开发者理解"为什么这样设计"背后的决策上下文。

---

## 一、架构演进概述

Remilia 从 2025 年 12 月的第一个 commit 至今，经历了 7 个关键演进阶段：

```
2025-12    第一阶段：Monolithic QQ Bot
            ↓ 引擎抽取、Context 池化
2026-01    第二阶段：多引擎 + Context v2
            ↓ 平台抽象、lifecycle 独立包
2026-02    第三阶段：Plugin v2 正式版
            ↓ 多平台扩展、infra 沉淀
2026-03    第四阶段：平台迁移完成、v1.0.0 发布
```

每个阶段解决的核心问题不同，但贯穿始终的设计原则是**从具体到抽象、从专用到通用**——先跑通再重构，不做过度设计。

### 第一阶段：Monolithic QQ Bot

**初始状态**：所有代码在根包 `remilia/` 下，Bot 紧耦合 QQ 的 `openapi/dto` 和 `openapi/auth/token`。

```go
// 初始 bot.go — 强依赖 QQ 类型
type Bot struct {
    wh     webhook.WebHook    // QQ Webhook
    tm     *token.Manager     // QQ Token
    api    openapi.OpenAPI    // QQ OpenAPI
    engine *Engine
}
```

**关键问题**：
- Context 持有 `*dto.Payload` 和 `openapi.OpenAPI`——只能处理 QQ 事件
- 插件系统基于继承（`BasePlugin`）——框架强耦合、测试困难
- 日志使用 logrus——热路径分配多、GC 压力大
- 生命周期在 Bot 内部手动编排——新组件加入时必须修改 Bot

**演进驱动**：需要支持 Discord 等多平台、插件系统越来越复杂、性能基准测试暴露瓶颈。

### 第二阶段：引擎抽取与 Context 改造

- `core/engine` 独立包——Engine 从 800+ 行拆分为 4 个职责分明的文件
- Context Pool（`sync.Pool`）——0 allocs/op，消除 GC 压力
- COW 写操作优化——选择性复制 + `BatchRegisterMatchers`
- `commandIndex` 引入——命令事件 O(1) 路由

### 第三阶段：插件革命 v1 → v2

- 函数式 Descriptor 替代继承——插件从"类"变为"数据"
- DryRun 自动依赖推断——消除手写 `Dependencies()` 的遗漏
- 三种热重载策略（含 BlueGreen）——零停机更新
- 读写分离权限模型——SetupContext vs ManagerWriter

### 第四阶段：多平台抽象与生命周期系统化

- `platform.Event` / `platform.Adapter` / `platform.Registry` 三大抽象
- lifecycle 独立包 + Component 接口 + 双层 Context
- zerolog 替换 logrus——热路径日志零分配
- 自定义 Prometheus Registry——消除多实例冲突

---

## 二、核心架构思路

### 原则 1：从具体到抽象，从专用到通用

**过早抽象是万恶之源**。Remilia 的策略是先用具体实现跑通 QQ Bot，等到 Discord 适配器需求出现时才做 `platform.Adapter` 抽象。这个决策让框架在早期阶段能够快速迭代验证，同时确保抽象层恰好满足需求，不过度设计。

### 原则 2：热路径零锁，冷路径随便锁

事件引擎是读多写少的典型场景。基于这个基本假设：
- **热路径（事件处理）**：COW 无锁读 + atomic.Value + 原子计数器——完全无锁
- **冷路径（插件注册、配置更新）**：sync.Mutex + 常规锁——正确性优先

这个哲学也延伸到多平台适配器——启动时构建只读快照，热路径直接 `atomic.Load`，避免每事件加锁。

### 原则 3：插件系统是第一等公民

插件不是"附加功能"，而是框架的核心扩展机制。v2 的函数式 Descriptor 将插件从"类"变为"数据"——可序列化、可 DryRun、可通过 DI 容器解耦。内置 25+ 插件覆盖机器人场景的常见需求，每个插件都可以独立拆卸。

### 原则 4：可观测性是功能，不是附件

从最早版本就有 metrics 和 health check。三个关键设计：
- **Prometheus 独立 Registry**——避免多实例 metric 冲突
- **zerolog 零分配日志**——热路径无害
- **自适应采样追踪**——高负载自动降采样

### 原则 5：基础设施下沉与复用

"三次法则"：同一个模式出现三次就抽取为通用组件。`infra/atomic`、`infra/pool`、`infra/syncx`、`infra/health` 等都是这样诞生的。Go 1.26 泛型让这些包做到了类型安全，且零外部依赖。

---

## 三、各模块迭代路径

| 模块 | V0 | V1 | V2 | V3 | V4（当前） |
|------|----|----|----|----|-----------|
| **事件引擎** | bare `atomic.Value` + 手动断言 | `infraatomic.Value[T]` 泛型 | 选择性 COW 复制 | eventGate sentinel | `shutdown`+`WaitGroup` |
| **匹配器路由** | RWMutex 线性遍历 | COW + EventType 索引 | +commandIndex O(1) | +TempManager 分片 | 六路合并 |
| **插件系统** | 继承 BasePlugin | Coordinator 接口隔离 | 函数式 Descriptor | +OptionalDeps | DryRun+BlueGreen |
| **中间件链** | 全局无缓存 | 三层+代际号 | 版本计数器替代 reflect | 迭代式构建 | 三级调用路径 |
| **生命周期** | Bot 内嵌启动逻辑 | Context 区分 Start/Stop | lifecycle 独立包 | 双层 Context | 状态机+回滚 |
| **多平台** | QQ 紧耦合 | Context 双路径 | 三大抽象（Event/Adapter/Registry） | 6 个适配器 | 快照+断连感知 |
| **可观测性** | logrus | zerolog | 自定义 Prometheus Registry | 健康检查框架 | 自适应采样 |
| **命令系统** | 引擎内嵌遍历 | commandIndex | Trie 树 + 缓存 | 别名自动注册 | Fuzz+自定义前缀 |
| **配置热更新** | Viper 集成 | 纯 YAML | fsnotify 监视器 | Bridge 桥接 | 生命周期绑定 |
| **基础设施** | 散落各模块 | 按功能提取 | 泛型化 | 跨平台支持 | 向后兼容 |

---

## 四、各模块详细演进文档

详细的演进过程和技术实现位于 `notes/` 目录（独立于官方文档，适合博客发布）：

| 主题 | 笔记链接 | 核心迭代内容 |
|------|---------|------------|
| COW 无锁引擎 | [notes/01-cow-engine.md](../../notes/01-cow-engine.md) | `atomic.Value` 裸用 → 泛型封装 → eventGate sentinel → shutdown+WaitGroup |
| 六路合并路由 | [notes/02-six-way-merge-matcher.md](../../notes/02-six-way-merge-matcher.md) | 线性遍历 → COW+索引 → commandIndex → TempManager |
| 插件系统 v2 | [notes/03-plugin-system-v2.md](../../notes/03-plugin-system-v2.md) | 继承模式 → 函数式 Descriptor → DryRun → BlueGreen |
| 中间件链 | [notes/04-middleware-chain.md](../../notes/04-middleware-chain.md) | 全局无缓存 → 三层+代际号 → 版本计数器 → 迭代构建 |
| 生命周期管理 | [notes/05-lifecycle-management.md](../../notes/05-lifecycle-management.md) | Bot 内嵌 → lifecycle 包 → 双层 Context → 状态机+回滚 |
| 多平台适配器 | [notes/06-multi-platform-adapter.md](../../notes/06-multi-platform-adapter.md) | QQ 紧耦合 → 双路径 → 三大抽象 → 6 适配器 |
| 可观测性体系 | [notes/07-observability.md](../../notes/07-observability.md) | logrus → zerolog → 自定义 Registry → 自适应采样 |
| 命令系统 | [notes/08-command-system.md](../../notes/08-command-system.md) | 引擎内嵌 → commandIndex → Trie → 别名+Fuzz |
| 配置热更新 | [notes/09-config-hotreload.md](../../notes/09-config-hotreload.md) | Viper → 纯 YAML → fsnotify → Bridge → 生命周期绑定 |
| 基础设施工具包 | [notes/10-infra-toolkit.md](../../notes/10-infra-toolkit.md) | 散落各处 → 按功能提取 → 泛型化 → 跨平台 |
| 完整演进故事 | [notes/00-evolution.md](../../notes/00-evolution.md) | 7 个阶段的完整演进脉络 |

---

## 五、架构图（当前）

```
┌──────────────────────────────────────────────────────┐
│                    Application                        │
│                  (Your Bot Logic)                     │
└────────────────────┬──────────────────────────────────┘
                     │
┌────────────────────▼──────────────────────────────────┐
│                      Bot                               │
│              (Lifecycle Manager)                       │
├───────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌────────────────────────────────┐  │
│  │  Platform    │  │      Engine (COW)              │  │
│  │  Adapters    │  │  ┌──────────┐ ┌─────────────┐  │  │
│  │ ┌─────────┐  │  │  │ Matcher  │ │ Middleware  │  │  │
│  │ │ QQ      │  │  │  │ Indexes  │ │ Chain      │  │  │
│  │ │ Discord │  │  │  └──────────┘ └─────────────┘  │  │
│  │ │ Telegram│──┼──┤  ┌──────────┐ ┌─────────────┐  │  │
│  │ │ OneBot  │  │  │  │ Command  │ │ Temp        │  │  │
│  │ │ Satori  │  │  │  │ Index    │ │ Manager     │  │  │
│  │ │ WeChat  │  │  │  └──────────┘ └─────────────┘  │  │
│  │ │ Milky   │  │  └────────────────────────────────┘  │
│  │ └─────────┘  │                                      │
│  └─────────────┘                                       │
├───────────────────────────────────────────────────────┤
│                  Plugin System (v2)                    │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐ │
│  │ Descriptor│ │ Container│ │  EventBus│ │ 25+ B-I  │ │
│  │  Pattern  │ │   (DI)   │ │          │ │ Plugins  │ │
│  └──────────┘ └──────────┘ └──────────┘ └──────────┘ │
├───────────────────────────────────────────────────────┤
│              Observability (Metrics/Trace/Log)         │
└───────────────────────────────────────────────────────┘
```

---

## 六、关键设计模式

| 模式 | 应用位置 | 说明 |
|------|---------|------|
| **COW（Copy-on-Write）** | Engine 状态 | 读操作无锁，写操作复制-修改-替换 |
| **Adapter 模式** | platform/ 层 | 统一多平台事件源接口 |
| **Descriptor 模式** | 插件系统 | 纯数据描述替代类继承 |
| **依赖注入** | 插件 Container | sync.Map + 原子快照 |
| **洋葱模型** | 中间件链 | Pre-process → Handler → Post-process |
| **状态机** | lifecycle Manager | StateCreated → ... → StateStopped |
| **迭代器模式** | 中间件执行 | 迭代式构建替代嵌套闭包 |
| **Observer 模式** | AdapterObserver | 适配器断连通知 |
| **Strategy 模式** | 热重载策略 | UnloadLoad / InPlace / BlueGreen |
| **分片（Sharding）** | TempManager | 8 分片 + FNV-1a 哈希减少锁粒度 |
