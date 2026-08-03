# Remilia 框架架构笔记

> 一个现代化、高性能、易于扩展的多平台机器人框架

## 总览

Remilia 基于 Go 1.26+ 构建，核心设计围绕**写时复制（COW）无锁引擎**展开，提供了从事件路由、中间件链、插件系统、多平台适配到可观测性的完整能力。

## 笔记目录

| # | 主题 | 核心内容 |
|---|------|----------|
| 00 | [架构演进之路](00-evolution.md) | 从 ZeroBot 启蒙到通用框架的 8 个阶段演进，每个阶段的动机和决策 |
| 01 | [COW 无锁引擎](01-cow-engine.md) | `atomic.Value` + 不可变状态，读操作完全无锁，475K msg/s 吞吐 |
| 02 | [六路合并匹配器路由](02-six-way-merge-matcher.md) | commandIndex O(1) + 6 路排序 + TempManager 分片 |
| 03 | [插件系统 v2](03-plugin-system-v2.md) | 函数式描述符、依赖注入容器、蓝绿热重载、读写分离权限 |
| 04 | [中间件链与自适应能力](04-middleware-chain.md) | 洋葱模型、三级调用路径优化、自适应限流/熔断/降级 |
| 05 | [生命周期管理](05-lifecycle-management.md) | Component 接口、双层 Context、按序启动/逆序停止/回滚 |
| 06 | [多平台适配器体系](06-multi-platform-adapter.md) | Adapter 接口、能力声明、Registry 多平台管理 |
| 07 | [可观测性体系](07-observability.md) | Prometheus + OpenTelemetry + zerolog + HealthCheck + pprof |
| 08 | [命令系统](08-command-system.md) | 双索引 O(1) 路由 + Trie 前缀树补全 + 别名自动注册 |
| 09 | [配置管理与热更新](09-config-hotreload.md) | fsnotify 目录监听、防抖合并、Bridge 推模式中间件更新 |
| 10 | [基础设施工具包](10-infra-toolkit.md) | 20+ 独立包（并发原语、存储、图像、中文处理等） |
| 11 | [ZeroBot 基因溯源](11-zerobot-inspiration.md) | ZeroBot 架构对比、遗传基因图谱、关键分叉分析、逐组件深度对比 |
| 12 | [FSM 有限状态机](12-fsm-engine.md) | 声明式多步骤对话引擎、状态迁移、会话管理 |
| 13 | [Adaptive Router 自适应路由](13-adaptive-router.md) | 优先级驱动的策略路由层 |
| 14 | [WASM 插件沙箱](14-wasm-plugin.md) | wazero 运行时、ABI 约定、资源限流 |
| 15 | [Per-Channel Engine](15-per-channel-engine.md) | **[v1.3.0 已归档]** 通道级引擎隔离（已被 Matcher.BlockForChannel 替代） |
| 16 | [PluginScope 资源追踪](16-plugin-scope.md) | Scope 级联清理、订阅自动取消、子 Scope 管理 |
| 17 | [ServiceProxy 服务代理](17-service-proxy.md) | 防过期的插件间同步调用、热重载安全 |
| 18 | [状态迁移](18-state-migration.md) | 版本化状态迁移管线、MigrateState 自动触发 |
| 19 | [三色标记法与依赖推断](19-three-color-dryrun.md) | DryRun 预跑 Setup 发现依赖、三色标记拓扑排序 |
| 20 | [Core 深度复查实录](20-core-review-lessons.md) | 2026-07 全量复查提炼的八个并发缺陷模式与契约方法论 |
| 21 | [OutboundDispatcher 出站调度](21-outbound-dispatcher.md) | 以会话为单位的 FIFO 调度、按需 worker、Future 集成 |
| 22 | [自适应执行](22-adaptive-execution.md) | ExecProfile p50 判定 + ExecPool 有界池 + 退出协议 |
| 23 | [Context 设计](23-context-design.md) | 双键扩展系统、Clone 语义、延迟副作用、Try* 能力探测 |
| 24 | [Bot 装配层](24-bot-assembly.md) | Bot/BotBuilder/BotManager、平台热替换、优雅关闭、健康检查树 |
| 25 | [RoutingStrategy 路由规划](25-routing-strategy.md) | 路由与执行分离、CandidatePlan 执行计划、MatcherIndex 插件化、Source Budget、快慢带惰性阶段 |
| 附 | [Trie 前缀树](trie.md) | 命令补全的前缀树实现细节（08 的配套深潜） |

## 架构思路

### 核心理念

Remilia 的架构设计围绕几条核心原则展开，这些原则不是一开始就明确的，而是在七阶段的演进中逐步沉淀：

**1. 从具体到抽象，从专用到通用**

项目起步于一个纯粹的 QQ 机器人：Bot 持有 `webhook.WebHook` + `token.Manager` + `openapi.OpenAPI`，Context 持有 `*dto.Payload`。随着 Discord、Telegram 等平台需求出现，才抽象出 `platform.Adapter` 接口和 `platform.Event` 事件模型。

架构决策时机很重要——过早抽象会导致过度设计，过晚抽象会导致重构成本高昂。Remilia 的策略是：**先用具体实现跑通，等到第二个平台需求出现时做抽象**。Discord 适配器的开发直接推动了平台抽象层的诞生。

**2. 读多写少场景的极致优化**

框架的核心是事件引擎——读操作（事件处理）远多于写操作（匹配器注册）。基于这个基本假设，选择了 COW 并发模型：读操作完全无锁，写操作复制-修改-替换。这个决策在 475K msg/s 的基准测试中验证了其正确性。

推广到整个框架：热路径（事件处理、上下文获取、服务查找）都用无锁或原子操作；管理路径（插件注册、配置更新）才使用传统锁。这种"热路径零锁，冷路径随便"的哲学贯穿始终。

**3. 插件系统作为第一等公民**

框架不是为了插件系统而做插件系统——插件的本质是"可组合、可卸载的业务模块"。v1 的继承模式简单直接，但限制了灵活性和测试性。v2 的函数式 Descriptor 模式将插件从"类"变为"数据"：

```go
// 插件是一个数据对象，不是类继承
plugin.Descriptor{
    Name: "myplugin",
    Setup: func(ctx *SetupContext) (any, error) { ... },
}
```

这个转变使得插件可以序列化（插件商店）、可以 DryRun 测试、可以通过 DI 容器实现依赖解耦。

**4. 基础设施下沉与复用**

随着项目发展，`pool/`、`atomic/`、`health/`、`metrics/` 等通用组件从业务代码中反复提取到 `infra/` 目录。这是"三次法则"（Rule of Three）的实践：同一个模式出现三次就抽取为通用组件。

`infra/` 包的设计原则：**零外部依赖**（除 Prometheus、OpenTelemetry 等必须的 SDK），**泛型安全**（Go 1.26 泛型消除类型断言），**零值可用**。

**5. 可观测性是功能，不是附件**

从最早的版本就有 metric、health check。Prometheus 指标、OpenTelemetry 追踪、zerolog 结构化日志不是后期添加的，而是伴随框架成长的核心能力。关键设计：Prometheus 使用独立 Registry 而非全局默认注册表，避免多实例冲突——这个问题在使用全局注册表的框架中非常常见。

**6. 生命周期是一切的基础**

随着组件越来越多（Engine、Adapter、PluginManager、PprofServer、Watcher...），启动和关闭顺序变成复杂问题。lifecycle 包是最后一个被抽取的独立包，但一旦稳定下来，它的大三阶段模型（Start → Run → Stop）就成了所有组件的标准生命模式。

双层 Context（parentCtx/runCtx）的设计是经过踩坑后得出的——插件 Teardown 时需要访问平台 API 发消息，所以 parentCtx 不能在 Stop 一开始就取消。

## 架构图

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

## 与官方文档的关系

本 `notes/` 目录是框架架构的**深度技术笔记**，强调每个模块的**迭代过程**（从 V0 的初始实现到当前版本的完整演进），适合作为技术博客发表。笔记中的代码片段来自 git commit 历史，每个演进阶段都有对应的 commit 可以追溯。

官方架构文档位于 [`docs/03-architecture/`](../03-architecture/)，其中 [`ARCHITECTURE_EVOLUTION.md`](../03-architecture/ARCHITECTURE_EVOLUTION.md) 是本文的总览。

**推荐阅读路径**：
1. [架构演进总览](../03-architecture/ARCHITECTURE_EVOLUTION.md) — 宏观脉络
2. [演进故事](00-evolution.md) — 8 个阶段的完整故事（含 ZeroBot 启蒙）
3. [ZeroBot 基因溯源](11-zerobot-inspiration.md) — 了解框架设计理念的来源
4. 各模块技术笔记（01-10）— 每个模块的迭代细节

## 如何阅读

推荐阅读顺序：

1. **先读 [00-evolution.md](00-evolution.md)** — 了解框架从何而来，每个设计决策的上下文
2. **读 [11-zerobot-inspiration.md](11-zerobot-inspiration.md)** — 理解 ZeroBot 基因对我们设计的影响（本文所有笔记都标注了 ZeroBot 遗传关系）
3. **再读 [01-cow-engine.md](01-cow-engine.md) 和 [02-six-way-merge-matcher.md](02-six-way-merge-matcher.md)** — 理解核心引擎的设计
4. **读 [03-plugin-system-v2.md](03-plugin-system-v2.md)** — 插件系统是框架最大的特色
5. **读 [05-lifecycle-management.md](05-lifecycle-management.md)** — 理解组件如何被管理
6. **其余可按兴趣阅读**

> 💡 每篇笔记都独立成文，可以直接用于博客发表。如果发布博客，建议将 [00-evolution.md](00-evolution.md) 作为开篇，先讲故事的"为什么"，再讲技术的"怎么做"。

## 关键技术栈

| 类别 | 核心技术 |
|------|----------|
| 并发模型 | COW 无锁 + atomic.Value + 泛型 |
| 插件系统 | 函数式 Descriptor + DI Container + 蓝绿部署 |
| 中间件 | 洋葱模型 + 版本计数器优化 + hotreload |
| 路由 | commandIndex O(1) + 6 路合并 + TempManager 分片 |
| 生命周期 | Component 接口 + 双层 Context + 自动回滚 |
| 平台适配 | Adapter 接口 + Capabilities 能力声明 |
| 指标 | Prometheus 独立 Registry |
| 追踪 | OpenTelemetry + 自适应采样 |
| 日志 | zerolog 零分配结构化日志 |
| 配置 | YAML + 环境变量 + fsnotify 热更新 |
| 命令 | Trie 树 + commandIndex 双索引 |
| 存储 | GORM + SQLite |
| 图像 | gg + textimage 引擎（Badge/图表/模糊/渐变） |
