# builtin — 内置模块说明

`builtin/` 目录收录了 Remilia 框架附带的所有内置插件与基础设施适配层。
模块按照**与框架核心的耦合程度**分为三个层次：

---

## 第一层：框架基础设施（非插件，框架内部使用）

这些模块**不是** `plugin.Descriptor`，而是纯基础设施或框架内部工具，无需用户手动注册。

| 模块 | 说明 |
|---|---|
| `core/` | 框架内置核心插件（permission、help 等），按需单独注册 |
| `internal/` | 包私有工具，不对外暴露 API |
| `permission/` | 权限判定的共享辅助（如 `permcheck`），供各插件复用 |

---

## 第二层：核心可选插件（强烈推荐，大多数机器人需要）

这些模块以 `plugin.Descriptor` 形式提供，可独立注册。
它们覆盖几乎所有机器人都会用到的通用能力，**不依赖特定业务逻辑**。

| 模块 | 说明 |
|---|---|
| `acl/` | 黑白名单访问控制，可作规则函数直接用于 `engine.On()` |
| `antispam/` | 反垃圾/防刷屏，按用户/群组独立限流，支持临时封禁 |
| `cooldown/` | 单命令冷却时间控制，比 antispam 更轻量，专注单命令粒度 |
| `keywordfilter/` | 关键词过滤（敏感词/违禁词），基于 Aho-Corasick 风格批量匹配 |
| `ratelimitui/` | 限流策略的可视化管理接口，配合 antispam/cooldown 使用 |
| `scheduler/` | 周期性计划任务（`Every` 固定间隔 / `Cron` 表达式），与 Bot 生命周期自动绑定 |
| `job/` | 一次性后台作业，支持延迟触发、自动重试和顺序链，与 scheduler 互补 |
| `sendqueue/` | 异步消息发送队列，内置速率控制，防止因发送过快触发平台限流 |
| `storage/` | 将 `infra/storage` 的 `Client` 接口包装为 `plugin.Descriptor`，使存储适配器能参与插件生命周期管理和依赖注入 |
| `pluginstore/` | 插件配置/状态持久化，shutdown 时自动将实现了 `Stateful` 接口的插件状态序列化到 storage |
| `pluginctrl/` | 运行时插件开关控制（启用/禁用单个插件），支持管理员通过命令动态管理 |
| `stats/` | 用户行为统计（消息数、命令调用频次等） |
| `auditlog/` | 操作审计日志，通过中间件自动记录命令调用和权限变更 |
| `logviewer/` | 审计日志的聊天内查询（`/logs`），依赖 `auditlog` |
| `messagelog/` | 群消息历史记录（环形缓冲区，按群/用户分片），用于词频统计、词云等场景 |

---

## 第三层：业务场景插件（按需引入）

这些模块面向特定业务场景，与平台或业务强相关，**按需引入**，不建议无差别全量注册。

| 模块 | 说明 |
|---|---|
| `broadcast/` | 向多个群/用户批量推送消息，内置发送速率控制 |
| `subscription/` | 通用推送订阅框架，将数据源（Source）与订阅目标（群/私聊）解耦 |
| `conversation/` | 多轮对话状态管理 |
| `verifycode/` | 验证码生成与验证（绑定角色/权限，支持一次性和多次使用） |
| `i18n/` | 国际化/本地化支持，从 YAML 文件加载语言包，支持 config.Watcher 热更新 |
| `dev/` | 开发调试辅助工具（仅建议在开发/测试环境注册，勿用于生产） |
| `vevent/` | 虚拟事件注入，允许插件或测试代码向引擎注入合成事件 |

---

## 依赖规则

1. **框架核心 (`core/`, `infra/`) 不得反向依赖 `builtin/` 中任何模块。**
   `builtin/` 模块可以依赖 `core/`、`infra/`、`plugin/`，反方向禁止。

2. **第三层业务插件不得被第一、二层模块依赖。**
   若发现跨层依赖，应将共用逻辑下沉到 `infra/` 或 `core/`。

3. **未来可独立拆包的候选模块**：`i18n/`、`vevent/`。
   这些模块无框架特有接口依赖，拆出后可作为通用 Go 库单独发布。

---

## 注册方式

```go
// 方式一：按需单独注册（推荐，依赖清晰）
pm.Register(acl.New())
pm.Register(scheduler.New())
pm.Register(storage.New(client))

// 方式二：通过依赖注入，让框架自动发现并注册
// （参见各模块 README 或 doc.go）
```

> v1.79.0 起 `builtin/bundle`（批量注册入口）与 `builtin/calendar`、
> `builtin/idiomdict`（无消费者的独立素材/工具库）已移除。
