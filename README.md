# Remilia

<div align="center">

![Remilia Logo](https://img.shields.io/badge/Remilia-QQ%20Bot%20Framework-blue)
![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go)
![License](https://img.shields.io/badge/license-MIT-green)
![Build Status](https://img.shields.io/badge/build-passing-brightgreen)

一个现代化、高性能、易于扩展的 QQ 机器人框架

[快速开始](#快速开始) • [文档](#文档) • [示例](#示例) • [贡献](#贡献)

</div>

---

## 🎉 v2.0.0

> **✅ Plugin v2 API 正式版**
>
> v1 API (BasePlugin) 已完全移除，v2 API 是目前唯一的插件开发方式。
>
> **v2 API 优势**:
> - ✅ 无需继承，函数式设计
> - ✅ 自动依赖注入（`Require` / `Optional` / `MustAs`）
> - ✅ 生命周期绑定的后台 goroutine（`ctx.Go`）
> - ✅ 读写分离权限模型（`PluginInfo` 只读 / `ManagerWriter` 写）
> - ✅ Smart 注册自动推断依赖图（无需手写 `Deps`）
>
> **快速上手**: [docs/02-user-guides/PLUGIN_V1_TO_V2_MIGRATION.md](docs/02-user-guides/PLUGIN_V1_TO_V2_MIGRATION.md)  
> **变更日志**: [CHANGELOG.md](CHANGELOG.md)

---

## ✨ 特性

### 🚀 核心能力

- **高性能引擎** — COW 并发模型，无锁读取，支持海量消息（8 workers 可达 6000+ msg/s）
- **插件系统** — v2 函数式插件，热重载（InPlace / BlueGreen 策略），自动依赖排序
- **中间件机制** — 限流 / 重试 / 降级 / 死信队列 / 去重，支持热更新阈值
- **命令解析** — Trie 树 + commandIndex 双索引，O(1) 命令路由
- **配置管理** — YAML / 环境变量，配置热更新（`hotreload.Bridge`）

### 🛡️ 可靠性保障

- **优雅关闭** — 完整的生命周期管理，确保消息不丢失
- **自适应限流** — 根据系统负载自动调整并发限制（每实例独立 Prometheus 指标）
- **熔断降级** — 自动熔断，CPU / 内存阈值热更新
- **死信队列** — 失败消息持久化，支持人工干预
- **自适应 Recover** — panic 捕获时自适应堆栈缓冲（4KB → 64KB）

### 📊 可观测性

- **Prometheus 集成** — 完整的 metrics 暴露
- **结构化日志** — 基于 logrus 的结构化日志
- **健康检查** — HTTP 健康检查端点
- **性能分析** — 内置 pprof 支持

---

## 📦 安装

```bash
go get github.com/KomeiDiSanXian/remilia
```

**要求**: Go 1.21+

---

## 🚀 快速开始

### 1. 基础示例

```go
package main

import (
    "github.com/KomeiDiSanXian/remilia"
    "github.com/KomeiDiSanXian/remilia/core/engine"
    eventctx "github.com/KomeiDiSanXian/remilia/core/context"
    "github.com/KomeiDiSanXian/remilia/openapi/dto"
)

func main() {
    eng := engine.NewEngine()

    // 注册命令处理器
    eng.OnCommand(dto.GroupAtMessageCreate, "/echo").
        Handle(func(ctx *eventctx.Context) error {
            return ctx.Reply("你说: " + ctx.GetMessageContent())
        })

    adapter := remilia.NewWebhookAdapter(":8080", "your-secret")
    bot := remilia.NewBot(adapter, eng)
    bot.Start()
    bot.WaitForShutdown()
}
```

### 2. 使用中间件

```go
import "github.com/KomeiDiSanXian/remilia/middleware"

eng.Use(
    middleware.Logging(),             // 日志记录
    middleware.Recover(),             // Panic 恢复（自适应堆栈）
    middleware.SimpleRateLimit(20),   // 全局限流：每秒最多 20 个事件
    middleware.Timeout(5*time.Second),// 超时（context deadline 注入）
    middleware.Retry(3),              // 重试
)
```

> **注意**：`ctx.Set(key, nil)` 是 no-op，删除 key 请用 `ctx.Delete(key)`。

### 3. 插件开发（v2 API）

```go
package myplugin

import (
    "github.com/KomeiDiSanXian/remilia/plugin"
    eventctx "github.com/KomeiDiSanXian/remilia/core/context"
    "github.com/KomeiDiSanXian/remilia/openapi/dto"
)

func New() *plugin.PluginDescriptor {
    p := &MyPlugin{}
    return &plugin.PluginDescriptor{
        Name:    "myplugin",
        Version: "1.0.0",
        Meta: &plugin.PluginMeta{
            Description: "我的第一个插件",
            Category:    "工具",
        },
        Setup: func(ctx *plugin.SetupContext) (any, error) {
            ctx.Reg.RegisterCommand(dto.GroupAtMessageCreate, "/hello").
                Handle(func(c *eventctx.Context) error {
                    return c.Reply("Hello from plugin!")
                })
            return p, nil
        },
    }
}
```

**注册插件**：

```go
manager := plugin.NewManager(eng)
manager.RegisterV2(myplugin.New())

// 或：批量注册，自动推断依赖顺序
plugin.RegisterMultipleV2Smart(manager,
    storage.New(),
    cache.New(),
    myplugin.New(),
)
```

**完整指南**: [docs/02-user-guides/PLUGIN_V1_TO_V2_MIGRATION.md](docs/02-user-guides/PLUGIN_V1_TO_V2_MIGRATION.md)

### 4. 使用配置文件

```yaml
# config.yaml
bot:
  app_id: 123456
  bot_id: 654321
  token: "your-token"
  secret: "your-secret"

server:
  host: "0.0.0.0"
  port: 8080

engine:
  temp_matcher_cleanup_interval: "5m"
  pending_delete_buffer_size: 1000

middleware:
  logging: true
  rate_limit: true
  rate_limit_rate: 100
  degradation_cpu_threshold: 80.0
  degradation_memory_threshold: 85.0
```

```go
cfg, err := config.Load("config.yaml")
if err != nil { panic(err) }

bot, err := remilia.NewBotFromConfig(cfg)
if err != nil { panic(err) }

bot.Start()
bot.WaitForShutdown()
```

---

## 📚 文档

**完整文档**: 👉 [docs/README.md](./docs/README.md)

### 快速导航

#### 🚀 新手入门
- [快速开始指南](./docs/01-getting-started/GETTING_STARTED.md) — 10 分钟上手
- [故障排除](./docs/01-getting-started/TROUBLESHOOTING.md) — 常见问题解答

#### 🔌 插件开发
- [Plugin v2 快速上手](./docs/02-user-guides/PLUGIN_V1_TO_V2_MIGRATION.md) — 从零开始写插件
- [插件接口速查](./docs/02-user-guides/PLUGIN_OPTIONAL_INTERFACES.md) — API 签名速查
- [插件开发最佳实践](./docs/04-development/plugin-best-practices.md) — 规范与模式

#### 📖 用户指南
- [最佳实践](./docs/02-user-guides/BEST_PRACTICES.md) — 推荐的使用模式
- [配置快速参考](./docs/02-user-guides/CONFIGURATION_QUICKREF.md) — 配置项速查
- [配置热更新](./docs/02-user-guides/CONFIG_HOTRELOAD_QUICKREF.md) — Bridge API
- [Matcher 链式调用](./docs/02-user-guides/MATCHER_CHAINING_BEST_PRACTICES.md) — 高级模式

#### 🏗️ 架构设计
- [并发事件处理](./docs/03-architecture/CONCURRENT_EVENT_PROCESSING.md) — COW 并发模型 + 6 路合并
- [插件系统设计](./docs/03-architecture/BUILTIN_PLUGINS_DESIGN.md) — 插件架构详解
- [命令系统集成](./docs/03-architecture/COMMAND_INTEGRATION_PLAN.md) — 命令解析器

#### 📊 质量报告
- [Core & Middleware 设计评审](./docs/05-reports/core-middleware-design-review.md) — P0/P1/P2 全部完成

---

## 💡 示例

查看 [examples](./examples) 目录获取更多示例：

- [基础 Bot](./examples/basic-bot) — 最简单的 bot 示例
- [命令系统](./examples/command-bot) — 完整的命令处理示例
- [插件开发](./examples/plugin-example) — 自定义插件示例（v2 API）
- [中间件使用](./examples/middleware-example) — 中间件组合使用
- [配置热更新](./examples/config_hotreload) — 配置热更新示例
- [Plugin v2 演示](./examples/plugin-v2-demo) — v2 插件完整演示

---

## 🏗️ 架构

### 整体架构

```
┌─────────────────────────────────────────────────────┐
│                    Application                       │
│                  (Your Bot Logic)                    │
└───────────────────┬─────────────────────────────────┘
                    │
┌───────────────────▼─────────────────────────────────┐
│                      Bot                             │
│              (Lifecycle Manager)                     │
└───────────────────┬─────────────────────────────────┘
                    │
        ┌───────────┴───────────┐
        │                       │
┌───────▼────────┐    ┌────────▼────────────────────────┐
│    Adapter     │    │     Engine (COW)                 │
│  (接收事件)     │    │  engine.go / matcher_ops.go     │
└───────┬────────┘    │  command.go / query.go           │
        │             └────────┬────────────────────────┘
        │                      │
        │              ┌───────┴────────┐
        │              │                │
        │        ┌─────▼─────┐  ┌──────▼──────┐
        │        │  Matcher  │  │ Middleware  │
        │        │  (路由)    │  │  (中间件链)  │
        │        └─────┬─────┘  └──────┬──────┘
        │              └────────┬───────┘
        │                       │
        │                ┌──────▼──────┐
        │                │   Handler   │
        │                │  (业务逻辑)  │
        │                └──────┬──────┘
        │                ┌──────▼──────┐
        │                │  OpenAPI    │
        │                │ (发送消息)   │
        │                └─────────────┘
        │
┌───────▼─────────────────────────────────────────────┐
│   Plugin System (v2)                                 │
│   descriptor / context / container                   │
│   instance / reload / register                       │
└─────────────────────────────────────────────────────┘
        │
┌───────▼─────────────────────────────────────────────┐
│              QQ Official API / Webhook               │
└─────────────────────────────────────────────────────┘
```

### 关键特性

#### 1. COW (Copy-On-Write) 引擎
- **无锁读取**: `ProcessEvent` 完全无锁，`atomic.Load()` 原子获取快照
- **写时复制**: 修改操作创建新状态，不影响正在进行的读取
- **文件拆分**: `engine.go`（核心）/ `engine_matcher_ops.go`（写操作）/ `engine_command.go`（命令）/ `engine_query.go`（只读查询）

#### 2. 智能匹配器路由（6 路合并）
- **commandIndex**: 消息以 `/` 开头时 O(1) 直接命中，跳过全量遍历
- **6 路合并**: State(perm/cmd) × Specific/Generic + Temp，按优先级线性合并
- **Temp 隔离**: State 列表跳过已迁移到 TempManager 的 Matcher

#### 3. 插件系统（v2）
- **函数式设计**: `PluginDescriptor` 替代继承，无样板代码
- **读写分离**: `PluginInfo`（只读）/ `ManagerWriter`（写，需 `Privileged: true`）
- **Smart 注册**: DryRun 阶段自动推断依赖图，无需手写 `Deps`

#### 4. 中间件链
- **洋葱模型**: Pre-process → Handler → Post-process
- **热更新**: `hotreload.Bridge` 推送配置变更给 `DedupFilter` / `AdaptiveDegradation`
- **自适应 Recover**: `captureStack()` 4KB→64KB 自适应堆栈缓冲

---

## 📊 性能

| 指标 | 值 | 说明 |
|------|-----|------|
| 消息吞吐量（8 workers）| ~6,000 msg/s | Webhook 多 worker 并发 |
| Engine ProcessEvent | ~5-6 μs/op | COW 无锁读取 |
| 命令解析 | ~1-2 μs/op | Trie + commandIndex |
| Context Pool | 0 allocs/op | 对象池复用 |

测试环境: AMD Ryzen 7 5800H, 16GB RAM, Go 1.21

---

## 🤝 贡献

我们欢迎所有形式的贡献！

### 如何贡献

1. Fork 本仓库
2. 创建特性分支 (`git checkout -b feature/AmazingFeature`)
3. 提交更改 (`git commit -m 'Add some AmazingFeature'`)
4. 推送到分支 (`git push origin feature/AmazingFeature`)
5. 开启 Pull Request

### 贡献指南

- 遵循 Go 代码规范和最佳实践
- 添加必要的测试用例
- 更新相关文档
- 确保所有测试通过（`go test ./...`）

### 开发环境

```bash
# 克隆仓库
git clone https://github.com/KomeiDiSanXian/remilia.git
cd remilia

# 安装依赖
go mod download

# 运行测试
go test ./...

# 运行示例
go run examples/basic-bot/main.go
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

## 📮 联系方式

- **Issues**: [GitHub Issues](https://github.com/KomeiDiSanXian/remilia/issues)
- **讨论**: [GitHub Discussions](https://github.com/KomeiDiSanXian/remilia/discussions)

---

## 🗺️ 路线图

### v2.1.0（规划中）

- [ ] WebSocket 模式支持
- [ ] 消息队列集成
- [ ] 分布式部署支持
- [ ] 可视化管理界面

---

<div align="center">

**⭐ 如果这个项目对你有帮助，请给一个 Star！⭐**

Made with ❤️ by [KomeiDiSanXian](https://github.com/KomeiDiSanXian)

</div>
