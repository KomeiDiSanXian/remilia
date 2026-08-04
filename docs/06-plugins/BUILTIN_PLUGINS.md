# 内置插件指南

Remilia 内置了 30+ 插件（`builtin/`），大多已包含在 `bundle.All()` 默认插件集中，注册即可使用。
本文按功能分类介绍各内置插件的用途与使用方式。

> 应用级插件（updater / pic / sauce / welcome 等）见 [应用级插件指南](./APP_PLUGINS.md)。

## 🛡️ 内容安全

### antispam — 防刷屏/防轰炸

检测群消息突发（`user_burst` / `group_burst` 阈值），支持违规自动封禁（`ban_on_violation` + `ban_duration`）。
无聊天命令，作为**规则/中间件**使用：

```go
engine.On(string(platform.EventKindGroupMessage), spam.Rule()).Handle(myHandler)
```

### keywordfilter — 关键词过滤

拦截包含违规关键词的消息（可配置命中策略：静默丢弃/警告/撤回）。无聊天命令，以中间件形式挂载。

### moderation — 群管理

| 命令 | 说明 |
|------|------|
| `/clean` | 清理聊天记录 |
| `/kick` | 踢出成员 |
| `/mute` | 禁言成员 |
| `/warn` | 警告成员（累计触发自动处理） |
| `/warnings` | 查看警告记录 |

## ⚙️ 自动化

### autoresponder — 关键词自动回复

`/ar` 管理自动回复规则：设置关键词与回复内容，命中即自动回复。

### broadcast — 广播

向所有群组 / 指定用户批量发送消息。

### subscription — 订阅推送

多数据源订阅框架：按配置定时查询外部资源（如 RSS）并推送更新到目标会话。

### scheduler — 计划任务

支持固定间隔与 cron 表达式的定时任务，执行历史可查（配套 API：`/api/v1/scheduler/*`）。

### job — 后台作业

结构化后台作业系统：延迟执行、自动重试（指数退避）、失败指定重试、超时控制，作业可持久化。

### customcommands — 自定义命令

`/cc` 管理用户自定义命令：无需编写 Go 代码即可注册"关键词 → 回复"的自定义命令。

## 💬 用户互动

### cooldown — 命令冷却

为命令配置冷却时间，支持用户级/群级/全局维度（如 `/daily`、`/sign` 签到类命令防刷）。

### verifycode — 验证码授权

通过一次性验证码授予用户权限（支持角色授予、入群验证、自动过期），详见 [验证码系统](../02-user-guides/verification-code-system.md)。

### vevent — 虚拟事件注入

将自定义事件合成为 `platform.Event` 注入引擎，用于测试、演示和扩展开发（含 `/cmd`、`/hello`、`/ping` 示例命令）。

## 📊 管理与观测

### auditlog — 审计日志

记录命令调用、配置变更与权限操作，供合规审查（配套 API：`/api/v1/auditlog/*`）。

### logviewer — 日志查询

`/logs` 在聊天中查询运行日志。

### ratelimitui — 限流状态

`/rl` 查看综合 antispam 与 cooldown 的实时限流状态。

### pluginstore — 插件状态持久化

插件状态存取（保存/恢复/临时状态），供其他插件做状态持久化。

### i18n — 国际化

多语言文本查找与热更新支持，为插件提供 `ctx` 语言感知的消息翻译。

## 📨 消息通道

### sendqueue — 异步发送队列

异步消息发送队列，带全局桶限速，防止 API 超限；支持队列满时的降级策略。

### messagelog — 群消息历史

内存环形缓冲 + SQLite 持久化记录群聊（含 bot 出站消息），见 [应用级插件指南](./APP_PLUGINS.md)。

## 🧩 其他核心插件

| 插件 | 说明 |
|------|------|
| `core/help` | `/help` 命令：按分类展示全部命令与插件信息 |
| `core/admin` | 管理命令集（插件热重载、状态查询等） |
| `core/permission` | RBAC 权限系统（角色/权限/分配），见 [权限系统架构](../03-architecture/permission-system.md) |
| `acl` | 黑白名单访问控制，见 [访问控制列表](../02-user-guides/access-control-list.md) |
| `dev/debug` | 调试工具集（`/debug event/ctx/matcher/runtime/commands/plugins/stats`） |
| `ping` | `/ping` 消息处理延迟检测 |
| `stats` | 用户行为统计（命令调用排行、活跃用户），可对接 AI 工具 |

---

*各插件的完整命令帮助可在机器人内使用 `/help` 查看；`bundle.All()` 为默认推荐组合。*
