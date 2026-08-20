# 常见问题 (FAQ)

本文档汇集部署、配置、插件开发与故障排除中的高频问题。

## 🚀 部署

### 如何部署一个 Remilia 机器人？

1. 克隆仓库：`git clone https://github.com/KomeiDiSanXian/remilia.git`
2. 参考 `cmd/bot/`（完整可运行的 Bot 示例）与 `cmd/bot/config.default.yaml` 编写配置
3. 构建：`go build ./...`（或参考 [快速开始](01-getting-started/GETTING_STARTED.md) 以库方式集成）
4. 运行并查看日志确认平台连接成功

### 有什么运行要求？

- **Go 1.26+**（框架与 `cmd/bot` 均要求）
- Linux / Windows / macOS 均可运行；Windows 下存在大量专项适配（updater 进程管理、控制台等），行为与 Unix 等价
- 无强制数据库要求：内存态插件（antispam/stats 等）可选对接 LevelDB 持久化

### 多实例部署需要注意什么？

同一 `data/` 目录被多个实例共用时，使用 LevelDB 的插件（antispam、stats 等）会互相抢占数据库锁，报 `The process cannot access the file because it is being used by another process`。

每个实例必须使用**独立的 data 目录**。

### Windows 上更新后新进程没有起来？

若启动器/终端把进程放入带 `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` 的 Job Object，父进程退出时会连带杀死子进程。updater 已携带 `CREATE_BREAKAWAY_FROM_JOB` 尝试脱离（不允许脱离时自动回退，行为不劣化）。

另外注意：**不要**将 updater 的 `child_console` 配置为继承父进程控制台（曾导致子进程被连带终止），推荐 `child_console: "new"` 或 `"file"`。

### 更新后新进程日志不可见？

将 `plugins.updater.child_console` 配置为 `"new"`（独立控制台窗口）或 `"file"`（输出重定向到 `data/updater/child.log`），并配合 `log.file` 落盘。

## ⚙️ 配置

### 配置文件在哪？格式是什么？

YAML 格式。`cmd/bot/config.default.yaml` 是完整参考；以库方式集成时在 Bot 组装中传入配置（参见 [工厂函数指南](02-user-guides/FACTORY_FUNCTIONS_GUIDE.md)）。

### 配置支持热更新吗？

支持。fsnotify 监听配置文件变更，Bridge 推模式实时推送；中间件（限流/熔断/降级阈值等）支持热更新阈值。详见 [配置热更新](02-user-guides/CONFIG_HOTRELOAD_QUICKREF.md)。

### 环境变量可以覆盖配置吗？

可以，YAML + 环境变量双通道，环境变量优先（详见 [配置快速参考](02-user-guides/CONFIGURATION_QUICKREF.md)）。

### QQ 平台用 webhook 还是 websocket？

两者都支持：`qq.NewWebhookServerAdapter`（HTTP 回调）或 websocket 适配器。webhook 模式需要公网可达的 HTTPS 回调地址。

## 🔌 插件开发

### 如何开始写一个插件？

参考 [插件开发指南](06-plugins/PLUGIN_DEVELOPMENT_GUIDE.md)：函数式 `plugin.Descriptor` + `Setup` 中注册命令/Matcher，无需继承。

### 插件之间如何共享服务和通信？

- **服务共享**：Setup 返回值自动注入容器，其他插件通过 `ctx.Service[T]` 获取；依赖用 `Deps`/`OptionalDeps` 声明（决定加载顺序与重载级联）
- **事件通信**：`ctx.EventBus` 发布/订阅，订阅推荐 `ctx.Scope().Subscribe`（插件卸载自动清理）

### 热重载对插件有什么要求？

默认 `ReloadUnloadLoad` 策略（卸载再加载）；无状态插件可声明 `ReloadBlueGreen`（零停机）。需要状态迁移时实现 `SaveState/RestoreState` + `MigrateState`。详见 [插件开发指南](06-plugins/PLUGIN_DEVELOPMENT_GUIDE.md)。

### 可以用其他语言写插件吗？

可以。WASM 插件支持 TinyGo / Rust / C，经 wazero 沙箱隔离，见 [WASM 插件开发](06-plugins/wasm-plugin-development.md)。

### 为什么我的插件在注册时提示依赖未找到？

`Deps` 中声明的插件必须在同一批注册或已加载；可选依赖请用 `OptionalDeps` + `TryService`。第三方插件不会被自动探测执行（依赖推断对其为纯静态）。

## 🛠️ 故障排除

### 如何查看日志？

zerolog 结构化日志输出到 stdout（终端），配置 `log.file` 后同时落盘。WASM/插件内部错误会带插件名前缀。

### 健康检查如何确认 Bot 正常？

启动健康 HTTP 端点（默认端口见配置），`/health` 返回多层级健康状态（Bot / Adapter / DLQ）；开启 pprof 后可进一步排查性能问题。

### QQ 平台用 webhook 还是 websocket？

两者都支持：`qq.NewWebhookServerAdapter`（HTTP 回调）或 websocket 适配器（`qq` 包 v1.25.0 起）。

- **webhook 模式**：需要公网可达的 HTTPS 回调地址；回调采用签名校验（`X-Signature`），事件回包必须按协议返回 `op=12`（HTTP Callback ACK），否则平台视为投递未确认而重试
- **websocket 模式**：由机器人主动建立长连接，无需公网入口，适合内网/无固定公网 IP 的部署

### QQ 平台按钮回调不稳定？

已知问题：QQ webhook 模式下互动回调（type=1）可能不被投递。内置插件（如 about 的"查看命令列表"）在 QQ 上使用指令按钮（type=2）规避；自研插件请避免依赖 QQ 互动回调。

### 命令没有响应？

1. 确认命令前缀（默认 `/`）与命令名正确
2. 群消息需 @机器人 或匹配 `OnMentionedBotOrNoMentions` 规则
3. 检查中间件链是否拦截（限流/黑名单/ACL）
4. 查看日志中 matcher 匹配与权限判定记录

### 更多问题？

先在 [故障排除](01-getting-started/TROUBLESHOOTING.md) 与 [最佳实践](02-user-guides/BEST_PRACTICES.md) 中查找；仍无法解决请提交 [GitHub Issue](https://github.com/KomeiDiSanXian/remilia/issues)（附日志与复现步骤）。
