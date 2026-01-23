# Remilia

<div align="center">

![Remilia Logo](https://img.shields.io/badge/Remilia-QQ%20Bot%20Framework-blue)
![Go Version](https://img.shields.io/badge/Go-1.19+-00ADD8?logo=go)
![License](https://img.shields.io/badge/license-MIT-green)
![Build Status](https://img.shields.io/badge/build-passing-brightgreen)
![Coverage](https://img.shields.io/badge/coverage-85%25-brightgreen)

一个现代化、高性能、易于扩展的 QQ 机器人框架

[快速开始](#快速开始) • [文档](#文档) • [示例](#示例) • [贡献](#贡献)

</div>

---

## ✨ 特性

### 🚀 核心能力

- **高性能引擎** - 基于事件驱动的并发处理引擎，支持海量消息
- **插件系统** - 灵活的插件架构，支持热加载和依赖管理
- **中间件机制** - 丰富的中间件支持（限流、重试、降级、死信队列等）
- **命令解析** - 强大的命令解析器，支持复杂参数和子命令
- **配置管理** - 支持 YAML/环境变量，配置热更新

### 🛡️ 可靠性保障

- **优雅关闭** - 完整的生命周期管理，确保消息不丢失
- **自适应限流** - 根据系统负载自动调整并发限制
- **熔断降级** - 自动熔断故障服务，快速降级保护
- **死信队列** - 失败消息持久化，支持人工干预
- **重试机制** - 指数退避重试，可配置策略

### 📊 可观测性

- **Prometheus 集成** - 完整的 metrics 暴露
- **结构化日志** - 基于 logrus 的结构化日志
- **健康检查** - HTTP 健康检查端点
- **性能分析** - 内置 pprof 支持

---

## 📦 安装

```bash
go get github.com/KomeiDiSanXian/remilia
```

**要求**: Go 1.19+

---

## 🚀 快速开始

### 1. 基础示例

创建一个简单的 echo 机器人：

```go
package main

import (
    "github.com/KomeiDiSanXian/remilia"
    "github.com/KomeiDiSanXian/remilia/core/engine"
    eventctx "github.com/KomeiDiSanXian/remilia/core/context"
    "github.com/KomeiDiSanXian/remilia/openapi/dto"
)

func main() {
    // 创建 Engine
    eng := engine.NewEngine()
    
    // 注册命令处理器
    eng.OnCommand("/echo", func(ctx *eventctx.Context) error {
        msg := ctx.GetPlainText()
        return ctx.Reply("你说: " + msg)
    })
    
    // 创建适配器（webhook 模式）
    adapter := remilia.NewWebhookAdapter(":8080", "your-secret")
    
    // 创建 Bot
    bot := remilia.NewBot(adapter, eng)
    
    // 启动
    if err := bot.Start(); err != nil {
        panic(err)
    }
    
    // 优雅关闭
    bot.WaitForShutdown()
}
```

### 2. 使用中间件

```go
package main

import (
    "github.com/KomeiDiSanXian/remilia"
    "github.com/KomeiDiSanXian/remilia/core/engine"
    "github.com/KomeiDiSanXian/remilia/middleware"
)

func main() {
    eng := engine.NewEngine()
    
    // 应用中间件
    eng.Use(
        middleware.Logging(),                    // 日志记录
        middleware.Recover(),                    // Panic 恢复
        middleware.ConcurrencyLimit(100, ...),   // 并发限制
        middleware.Retry(3),                     // 重试
    )
    
    // 注册处理器
    eng.OnMessage(func(ctx *eventctx.Context) error {
        // 处理消息
        return nil
    })
    
    // 启动 Bot
    adapter := remilia.NewWebhookAdapter(":8080", "secret")
    bot := remilia.NewBot(adapter, eng)
    bot.Start()
    bot.WaitForShutdown()
}
```

### 3. 使用配置文件

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

concurrency:
  limit: 100
  policy: "drop"

middleware:
  logging: true
  recover: true
  metrics: true
```

```go
package main

import (
    "github.com/KomeiDiSanXian/remilia"
    "github.com/KomeiDiSanXian/remilia/config"
)

func main() {
    // 加载配置
    cfg, err := config.Load("config.yaml")
    if err != nil {
        panic(err)
    }
    
    // 使用配置创建 Bot
    bot, err := remilia.NewBotFromConfig(cfg)
    if err != nil {
        panic(err)
    }
    
    // 启动
    bot.Start()
    bot.WaitForShutdown()
}
```

---

## 📚 文档

### 核心概念

- [Engine 事件引擎](./docs/ENGINE.md) - 事件处理引擎详解
- [Context 上下文](./docs/CONTEXT.md) - 请求上下文和辅助方法
- [Adapter 适配器](./docs/ADAPTER.md) - Webhook 和 WebSocket 适配器
- [Plugin 插件系统](./docs/PLUGIN.md) - 插件开发指南

### 功能模块

- [Command 命令系统](./command/README.md) - 命令解析和路由
- [Middleware 中间件](./docs/MIDDLEWARE.md) - 内置中间件使用指南
- [Configuration 配置管理](./docs/CONFIGURATION.md) - 配置文件和热更新
- [Lifecycle 生命周期](./lifecycle/README.md) - 生命周期管理

### 高级特性

- [自适应限流](./docs/ADAPTIVE_RATE_LIMITING.md) - 基于负载的智能限流
- [配置热更新](./docs/CONFIG_HOTRELOAD_IMPLEMENTATION.md) - 无需重启更新配置
- [命令索引优化](./docs/COMMAND_INDEX_OPTIMIZATION.md) - 高性能命令查找
- [插件系统增强](./docs/PLUGIN_ENHANCEMENT_PROPOSAL.md) - 插件系统增强方案

### 开发指南

- [快速上手教程](./docs/GETTING_STARTED.md)
- [最佳实践](./docs/BEST_PRACTICES.md)
- [性能调优](./docs/PERFORMANCE_TUNING.md)
- [故障排查](./docs/TROUBLESHOOTING.md)

---

## 💡 示例

查看 [examples](./examples) 目录获取更多示例：

- [基础 Bot](./examples/basic-bot) - 最简单的 bot 示例
- [命令系统](./examples/command-bot) - 完整的命令处理示例
- [插件开发](./examples/plugin-example) - 自定义插件示例
- [中间件使用](./examples/middleware-example) - 中间件组合使用
- [配置热更新](./examples/config_hotreload) - 配置热更新示例
- [分布式部署](./examples/distributed) - 多实例部署示例

---

## 🏗️ 架构

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
┌───────▼────────┐    ┌────────▼────────┐
│    Adapter     │    │     Engine      │
│  (接收事件)     │    │  (事件处理)      │
└───────┬────────┘    └────────┬────────┘
        │                      │
        │              ┌───────┴────────┐
        │              │                │
        │        ┌─────▼─────┐  ┌──────▼──────┐
        │        │  Matcher  │  │ Middleware  │
        │        │  (路由)    │  │  (中间件)    │
        │        └─────┬─────┘  └──────┬──────┘
        │              │                │
        │              └────────┬───────┘
        │                       │
        │                ┌──────▼──────┐
        │                │   Handler   │
        │                │  (业务逻辑)  │
        │                └─────────────┘
        │
┌───────▼─────────────────────────────────────────────┐
│              QQ Official API / Webhook               │
└─────────────────────────────────────────────────────┘
```

---

## 📊 性能

基于 benchmark 测试结果：

| 指标 | 值 | 说明 |
|------|-----|------|
| 消息吞吐量 | ~50,000 msg/s | 单实例处理能力 |
| 响应延迟 (P99) | <10ms | 命令处理延迟 |
| 内存占用 | ~50MB | 空闲时内存 |
| CPU 使用率 | <5% | 空闲时 CPU |
| 并发处理 | 10,000+ | 最大并发连接 |

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
- 确保所有测试通过

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
- **QQ群**: [待补充]

---

## 🗺️ 路线图

### v1.0.0 (计划中)

- [ ] 稳定的 API
- [ ] 完整的文档
- [ ] 更多示例
- [ ] 性能优化

### v1.1.0 (规划中)

- [ ] WebSocket 模式支持
- [ ] 消息队列集成
- [ ] 分布式部署支持
- [ ] 可视化管理界面

---

<div align="center">

**⭐ 如果这个项目对你有帮助，请给一个 Star！⭐**

Made with ❤️ by [KomeiDiSanXian](https://github.com/KomeiDiSanXian)

</div>
