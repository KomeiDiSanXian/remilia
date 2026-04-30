# Changelog

## v1.0.0 (2026-04-30)

### 🎉 初始发布

Remilia 是一个现代化、高性能、易于扩展的多平台聊天机器人框架。

#### 🚀 核心特性

- **高性能 COW 引擎** — Copy-on-Write 并发模型，无锁读取，单实例吞吐量 475,000+ msg/s
- **v2 插件系统** — 函数式设计，自动依赖注入，Smart 注册拓扑排序，支持热重载
- **多平台适配器** — QQ（Webhook）、Discord（Gateway）、Satori（Chronocat/Lagrange）、Milky、OneBot V11
- **中间件链** — 日志/限流/重试/熔断/降级/去重/死信队列/Tracing/Metrics，支持热更新阈值
- **命令系统** — Trie + commandIndex 双索引，O(1) 命令路由
- **可观测性** — Prometheus 指标、OpenTelemetry 分布式追踪、结构化日志（zerolog）、pprof 性能分析
- **可靠性** — 优雅关闭、自适应限流、熔断降级、死信队列（文件/Kafka/Webhook）
- **配置管理** — YAML + 环境变量，配置热更新
- **32 个内置插件** — Admin、Help、Permission、ACL、AntiSpam、AuditLog、Broadcast、i18n、Scheduler、Storage 等

#### 📦 安装

```bash
go get github.com/KomeiDiSanXian/remilia
```

#### 📊 性能指标

| 指标 | 值 |
|------|-----|
| 消息吞吐量（空 Handler） | ~475,000 msg/s |
| Engine ProcessEvent | ~5-6 μs/op |
| 命令解析 | ~1-2 μs/op |
| 堆内存（50,000 msg/s） | ~12-14 MB |

#### ⚠️ 注意事项

- **Telegram 和 WeChat 适配器**为骨架实现，暂不可用于生产环境
- 要求 Go 1.26+
- 许可证：MIT
