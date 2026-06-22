<div align="center">

# Remilia

</div>

> Remilia 受 [wdvxdr1123/ZeroBot](https://github.com/wdvxdr1123/ZeroBot) 架构启发，
> 但为 **完全独立的实现**，不包含 ZeroBot 的任何受版权保护的代码。
> ZeroBot 是 GPL-3.0 许可的开源项目。

<div align="center">

![Go Version](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go)
![License](https://img.shields.io/badge/license-MIT-green)

一个现代化、高性能、易于扩展的多平台机器人框架

[快速开始](#快速开始) • [文档](#文档) • [示例](#示例) • [贡献](#贡献)

</div>

---

## ✨ 特性

### 🚀 核心能力

- **高性能 COW 引擎** — Copy-on-Write 并发模型，无锁读取，单实例端到端吞吐量 ~450,000 msg/s
- **插件系统** — 函数式 Descriptor 设计，自动依赖注入，蓝绿热重载，Smart 注册自动推断依赖
- **WASM 跨语言插件** — TinyGo / Rust / C 编写 WASM 插件，wazero 沙箱隔离，TLV 序列化
- **多平台适配** — QQ / Discord / OneBot / Satori / Telegram / WeChat / Milky，统一 Adapter 接口
- **FSM 状态机引擎** — 声明式多步骤对话管理，上下文感知的状态迁移
- **Adaptive Router** — 策略路由层，优先级驱动的 FSM / Engine 事件分发
- **6 路合并 Matcher** — commandIndex O(1) 命令路由 + 6 路优先级排序合并
- **中间件链** — 洋葱模型，限流 / 熔断 / 降级 / 重试 / 去重 / 超时，支持热更新阈值
- **配置热更新** — YAML + 环境变量，fsnotify 监听，Bridge 推模式实时推送

### 🛡️ 可靠性保障

- **优雅关闭** — 完整的生命周期管理（lifecycle 包），组件按序启动 / 逆序停止 / 自动回滚
- **自适应限流** — 根据系统负载动态调整并发限制
- **熔断降级** — 自动熔断，CPU / 内存阈值可配置热更新
- **死信队列** — 失败消息持久化，支持人工干预与重放
- **自适应 Recover** — panic 捕获时自适应堆栈缓冲（4KB → 64KB）

### 📊 可观测性

- **Prometheus 指标** — 独立 Registry，多实例安全，完整 metrics 暴露
- **OpenTelemetry 追踪** — OTLP HTTP 导出，自适应采样
- **结构化日志** — 基于 zerolog，零分配热路径日志
- **健康检查** — HTTP 端点，多层级健康探针（Bot / Adapter / DLQ）
- **pprof 性能分析** — 内置 pprof 服务器，自动 CPU / 堆转储

---

## 📦 安装

```bash
go get github.com/KomeiDiSanXian/remilia
```

**要求**: Go 1.26+

---

## 🚀 快速开始

### 1. QQ 平台基础示例

```go
package main

import (
    "github.com/KomeiDiSanXian/remilia"
    eventctx "github.com/KomeiDiSanXian/remilia/core/context"
    "github.com/KomeiDiSanXian/remilia/core/engine"
    "github.com/KomeiDiSanXian/remilia/platform"
    "github.com/KomeiDiSanXian/remilia/platform/qq"
    "github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
)

func main() {
    eng := engine.NewEngine()

    eng.OnCommand(platform.EventKindGroupMessage, "/echo").
        Handle(func(ctx *eventctx.Context) error {
            return ctx.Reply(platform.TextMessage("你说: " + ctx.GetMessageContent()))
        })

    botInfo := &dto.BotInfo{
        AppID:     123456,
        Token:     "your-token",
        AppSecret: "your-secret",
    }
    adapter := qq.NewWebhookServerAdapter(":8080", botInfo)

    bot, err := remilia.NewBotBuilder().
        WithPlatformAdapter(adapter).
        WithEngine(eng).
        Build()
    if err != nil {
        panic(err)
    }

    bot.Start()
    bot.WaitForShutdown()
}
```

### 2. 多平台部署

```go
registry := platform.NewRegistry()
registry.Register(qqAdapter)
registry.Register(discordAdapter)

bot, err := remilia.NewBotBuilder().
    WithPlatformRegistry(registry).
    WithEngine(eng).
    Build()
```

### 3. 使用中间件

```go
import "github.com/KomeiDiSanXian/remilia/middleware"

eng.Use(
    middleware.Logging(),
    middleware.Recover(),
    middleware.SimpleRateLimit(20),
    middleware.Timeout(5*time.Second),
    middleware.Retry(3),
)
```

### 4. 插件开发

```go
package myplugin

import (
    "github.com/KomeiDiSanXian/remilia/plugin"
    eventctx "github.com/KomeiDiSanXian/remilia/core/context"
    "github.com/KomeiDiSanXian/remilia/platform"
)

func New() *plugin.Descriptor {
    return &plugin.Descriptor{
        Name:    "myplugin",
        Version: "1.0.0",
        Setup: func(ctx *plugin.SetupContext) (any, error) {
            ctx.Reg.RegisterCommand(platform.EventKindGroupMessage, "/hello").
                Handle(func(c *eventctx.Context) error {
                    return c.Reply(platform.TextMessage("Hello from plugin!"))
                })
            return nil, nil
        },
    }
}
```

**注册插件**:

```go
manager := plugin.NewManager(eng)
manager.Register(myplugin.New())
```

**完整指南**: [docs/02-user-guides/PLUGIN_DEVELOPMENT_GUIDE.md](docs/02-user-guides/PLUGIN_DEVELOPMENT_GUIDE.md)

---

## 📚 文档

**完整文档**: 👉 [docs/README.md](./docs/README.md)

### 快速导航

#### 🚀 新手入门
- [10 分钟快速开始](./docs/01-getting-started/GETTING_STARTED.md)
- [故障排除](./docs/01-getting-started/TROUBLESHOOTING.md)

#### 🔌 插件开发
- [插件开发指南](./docs/02-user-guides/PLUGIN_DEVELOPMENT_GUIDE.md)
- [插件接口速查](./docs/02-user-guides/PLUGIN_OPTIONAL_INTERFACES.md)
- [插件开发最佳实践](./docs/04-development/plugin-best-practices.md)
- [WASM 跨语言插件](./docs/04-development/wasm-plugin-development.md)

#### 📖 用户指南
- [最佳实践](./docs/02-user-guides/BEST_PRACTICES.md)
- [配置快速参考](./docs/02-user-guides/CONFIGURATION_QUICKREF.md)
- [配置热更新](./docs/02-user-guides/CONFIG_HOTRELOAD_QUICKREF.md)
- [错误处理](./docs/02-user-guides/ERROR_HANDLING.md)
- [分布式追踪](./docs/02-user-guides/tracing.md)
- [访问控制列表](./docs/02-user-guides/access-control-list.md)

#### 🏗️ 架构设计
- [并发事件处理（COW）](./docs/03-architecture/CONCURRENT_EVENT_PROCESSING.md)
- [多平台抽象](./docs/03-architecture/MULTI_PLATFORM.md)
- [Context 传播模式](./docs/03-architecture/CONTEXT_PROPAGATION.md)
- [权限系统](./docs/03-architecture/permission-system.md)
- [架构演进总览](./docs/03-architecture/ARCHITECTURE_EVOLUTION.md)

---

## 💡 示例

查看 [examples](./examples) 目录获取更多示例：

- [showcase](./examples/showcase) ⭐ — 综合示例（含 WASM 插件）
- [basic-bot](./examples/basic-bot) — 最简单的 bot
- [command-bot](./examples/command-bot) — 命令处理
- [plugin-example](./examples/plugin-example) — 自定义插件
- [middleware-example](./examples/middleware-example) — 中间件组合
- [config_hotreload](./examples/config_hotreload) — 配置热更新
- [error-handling](./examples/error-handling) — 错误处理
- [production-ready](./examples/production-ready) — 生产级最佳实践
- [async-tasks](./examples/async-tasks) — goroutine 管理与背压控制
- [httpclient-demo](./examples/httpclient-demo) — HTTP 客户端
- [metrics-monitoring](./examples/metrics-monitoring) — Prometheus 指标
- [tracing-demo](./examples/tracing-demo) — OpenTelemetry 追踪
- [sqlite-storage-demo](./examples/sqlite-storage-demo) — SQLite 存储
- [wasm-plugin](./examples/wasm-plugin) — WASM 插件
- [benchmark](./examples/benchmark) — 引擎吞吐量压测

---

## 🏗️ 架构

### 整体架构

```
┌─────────────────────────────────────────────────────────┐
│                     Application                         │
│                   (Your Bot Logic)                      │
└────────────────────┬────────────────────────────────────┘
                     │
┌────────────────────▼────────────────────────────────────┐
│                       Bot                               │
│               (Lifecycle Manager)                       │
│   ┌──────────────────┐  ┌──────────────────────────┐    │
│   │  Platform        │  │     Engine (COW)         │    │
│   │  Adapters        │  │  ┌──────┐ ┌──────────┐   │    │
│   │  ┌──────────┐    │  │  │Match │ │Middleware│   │    │
│   │  │ QQ       │    │  │  │er    │ │Chain     │   │    │
│   │  │ Discord  │    │  │  └──────┘ └──────────┘   │    │
│   │  │ Telegram │────┼──┤  ┌──────┐ ┌──────────┐   │    │
│   │  │ OneBot   │    │  │  │Cmd   │ │Temp      │   │    │
│   │  │ Satori   │    │  │  │Index │ │Manager   │   │    │
│   │  │ WeChat   │    │  │  └──────┘ └──────────┘   │    │
│   │  │ Milky    │    │  └──────────────────────────┘    │
│   │  └──────────┘    │                                  │
│   └──────────────────┘                                  │
├─────────────────────────────────────────────────────────┤
│  ┌────────────────────────────────────────────────────┐ │
│  │               Adaptive Router                      │ │
│  │         (FSM Engine / Engine 策略分发)              │ │
│  └────────────────────────────────────────────────────┘ │
├─────────────────────────────────────────────────────────┤
│                   Plugin System                         │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌───────────┐   │
│  │Descriptor│ │Container │ │ EventBus │ │ 34 B-I    │   │
│  │  Pattern │ │   (DI)   │ │          │ │  Plugins  │   │
│  └──────────┘ └──────────┘ └──────────┘ └───────────┘   │
├─────────────────────────────────────────────────────────┤
│            Observability (Metrics / Trace / Log)        │
│      Prometheus · OpenTelemetry · zerolog · pprof       │
└─────────────────────────────────────────────────────────┘
```

### 关键特性

#### 1. COW 无锁引擎
- **无锁读取**: ProcessEvent 完全无锁，atomic.Load 原子获取快照
- **写时复制**: 修改创建新状态，不影响正在进行的读取
- **6 路合并**: State(perm/cmd) × Specific/Generic + Temp 优先级排序

#### 2. 智能路由
- **commandIndex**: `/` 开头消息 O(1) 直接命中
- **FSM 状态机**: 多步骤对话，声明式状态迁移
- **Adaptive Router**: 策略路由层，FSM / Engine 优先级分发

#### 3. 插件系统
- **函数式 Descriptor**: 无继承，无样板代码
- **读写分离**: PluginInfo（只读）/ ManagerWriter（写，需 Privileged）
- **Smart 注册**: DryRun 自动推断依赖图，拓扑排序

#### 4. WASM 跨语言插件
- **跨语言**: TinyGo / Rust / C
- **沙箱隔离**: wazero 运行时，内存安全
- **资源控制**: 限流 / 超时 / 大小上限

#### 5. 中间件链
- **洋葱模型**: Pre-process → Handler → Post-process
- **热更新**: Bridge 推送配置变更
- **完整集合**: Recover / Logging / RateLimit / CircuitBreaker / Retry / Dedup / Degradation / Timeout / RequestID / ConcurrencyLimit

---

## 📊 性能

| 指标 | 值 | 说明 |
|------|-----|------|
| 端到端吞吐量（无匹配器） | **~450,000 msg/s** | 16 核端到端压测，100% 成功率 |
| 端到端吞吐量（5K 匹配器） | **~235,000 msg/s** | 16 核，含 2500 个事件类型匹配器 |
| Engine ProcessEvent（micro） | ~285 ns/op | 引擎分发热路径 |
| 命令解析 | ~1,250 ns/op | 双索引 O(1) 路由 |
| Context Get/Set | 0 allocs/op | 免 GC 上下文访问 |
| 堆内存（50K msg/s）| ~12-17 MB | 极低内存占用 |

> 端到端压测使用 `examples/benchmark/throughput_bench.go`（已修复 drain、延迟测量等设计问题）
> 详细报告: [docs/05-performance/PERFORMANCE_REPORT.md](docs/05-performance/PERFORMANCE_REPORT.md)

---

## 🤝 贡献

1. Fork 本仓库
2. 创建特性分支 (`git checkout -b feature/AmazingFeature`)
3. 提交更改 (`git commit -m 'Add some AmazingFeature'`)
4. 推送到分支 (`git push origin feature/AmazingFeature`)
5. 开启 Pull Request

### 开发环境

```bash
git clone https://github.com/KomeiDiSanXian/remilia.git
cd remilia
go mod download
go test ./...
```

---

## 📄 许可证

本项目采用 MIT 许可证 - 查看 [LICENSE](LICENSE) 文件了解详情。

---

## 🙏 致谢

- [QQ 官方机器人文档](https://bot.q.qq.com/wiki/)
- [Go 社区](https://golang.org/)
- 所有贡献者

---

<div align="center">

**⭐ 如果这个项目对你有帮助，请给一个 Star！⭐**

Made with ❤️ by [KomeiDiSanXian](https://github.com/KomeiDiSanXian)

</div>
