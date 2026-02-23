# Remilia Examples
本目录包含 Remilia 框架的示例代码，帮助你快速上手和深入理解各个功能模块。
---
## 综合示例 (推荐从这里开始)
### [showcase](./showcase/) ⭐⭐ 最全示例
一个**尽可能覆盖所有功能和现有插件**的单文件示例。阅读 `showcase/main.go` 可以了解几乎所有框架能力。
**覆盖内容**:
- BotBuilder 配置加载 & 构建
- 生产级中间件套件 + 自定义中间件
- 插件系统 v2（RegisterMultipleV2 自动拓扑排序 / StrictDeps 严格依赖模式 / LifecycleListener）
- 所有内置插件：storage / permission / acl / verifycode / antispam / keywordfilter / cooldown / stats / auditlog / scheduler / i18n / ratelimitui / pluginstore / conversation / cache / help / admin
- 命令系统（命令定义 / 子命令 / 帮助自动聚合）
- EventBus（普通订阅 + 通配符订阅）
- 插件 Disable/Enable 热控制
- pluginstore 跨重启状态持久化
**运行**:
```sh
cp ../../config.example.yaml ../../config.yaml
# 编辑 config.yaml 填入 AppID / Token / AppSecret
go run main.go
```
---
## 入门示例
### [basic-bot](./basic-bot/)
最简单的 Bot，适合完全的新手。演示 BotBuilder、命令注册、中间件使用。
### [plugin-v2-demo](./plugin-v2-demo/)
插件系统 v2 API 基础示例。演示无需继承的函数式插件写法（PluginDescriptor）、状态管理、热重载钩子。
### [command-bot](./command-bot/)
完整命令系统示例。演示命令注册、多命令协作、帮助系统。
---
## 进阶示例
### [middleware-example](./middleware-example/)
四种使用中间件的方式：预定义套件、简化工厂、构建器、自定义配置。
### [plugin-example](./plugin-example/)
传统 v1 插件开发（实现 Plugin 接口）。演示插件创建、生命周期、依赖管理。
### [production-ready](./production-ready/)
生产环境最佳实践配置：生产级中间件、错误处理、健康检查。
### [error-handling](./error-handling/)
错误处理完整演示：自定义错误类型、错误传播、panic 恢复、errutil 使用。
### [sqlite-storage-demo](./sqlite-storage-demo/)
SQLite 持久化存储：Storage 插件 SQLite 后端、JSON 存储、WAL 模式。
---
## 基础设施示例
### [config_hotreload](./config_hotreload/) / [config-integration](./config-integration/)
配置系统：YAML 加载、热重载（config.Watcher）、Viper 集成。
### [logger-demo](./logger-demo/)
日志系统：结构化日志、级别控制、多输出目标。
### [metrics-monitoring](./metrics-monitoring/)
性能监控：自定义 Metrics、请求统计、延迟追踪。
### [tracing-demo](./tracing-demo/)
分布式追踪：OpenTelemetry 集成。
### [httpclient-demo](./httpclient-demo/)
HTTP 客户端：重试策略、超时控制、中间件链。
### [debug-subcommand-demo](./debug-subcommand-demo/)
Debug 插件：子命令架构、运行时诊断、权限控制集成。
### [help-discovery](./help-discovery/)
Help 插件：命令自动发现、帮助文本生成。
### [async-tasks](./async-tasks/) (在 showcase 内有更简洁的演示)
异步任务处理：goroutine 管理、背压控制。
---
## 性能基准
### [benchmark](./benchmark/)
吞吐量基准测试：引擎性能压测、并发处理基准。
---
## 插件速查表
| 插件 | 包路径 | 描述 |
|------|--------|------|
| storage | plugins/core/storage | KV 存储（内存/SQLite/Redis）|
| permission | plugins/core/permission | RBAC 权限 + ACL + 验证码 |
| cache | plugins/core/cache | LRU 内存缓存 |
| help | plugins/core/help | 命令帮助自动聚合 |
| admin | plugins/core/admin | 管理命令（/plugin /perm /acl /code）|
| acl | plugins/acl | 独立黑白名单（区别于 permission 内置 ACL）|
| verifycode | plugins/verifycode | 独立验证码授权 |
| antispam | plugins/antispam | 用户/群限速 + 违规封禁 |
| keywordfilter | plugins/keywordfilter | 关键词/正则过滤 |
| cooldown | plugins/cooldown | 命令冷却时间 |
| stats | plugins/stats | 命令调用统计 + 活跃用户 |
| auditlog | plugins/auditlog | 操作审计日志 |
| scheduler | plugins/scheduler | 定时任务（固定间隔 + Cron）|
| i18n | plugins/i18n | 多语言支持（YAML 语言包 + 热重载）|
| ratelimitui | plugins/ratelimitui | 限流状态查询（聚合 antispam+cooldown）|
| pluginstore | plugins/pluginstore | 插件状态跨重启持久化 |
| conversation | plugins/conversation | 多步对话状态机 |
| broadcast | plugins/broadcast | 消息广播 |
| sendqueue | plugins/sendqueue | 发送队列（限速/排队）|
---
**更新时间**: 2026-02-23