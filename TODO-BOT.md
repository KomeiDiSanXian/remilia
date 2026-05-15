# IM 机器人开发 TODO

> 基于 Remilia 框架开发可用的 IM 机器人，仅使用框架内置插件。
> 后续再添加自定义业务插件。

---

## Phase 1: 项目骨架搭建 ✅

### 1.1 创建项目目录结构

- [x] 创建 `cmd/bot/` 目录作为机器人入口
- [x] 规划文件拆分：

```
cmd/bot/
├── main.go          # 入口：配置加载 → 日志初始化 → Bot 构建 → 启动 → 优雅关闭
├── setup.go         # 中间件、路由、FSM、插件管理器的初始化函数
└── plugins.go       # 内置插件注册（从配置文件读取插件参数）
```

> `config.yaml` 复用项目根目录的已有文件（已在 `.gitignore` 中）

### 1.2 配置文件

- [x] `config.yaml` 已在项目根目录存在（`.gitignore` 已忽略）
- [x] 平台凭证直接填入根目录 `config.yaml` 对应字段
- [x] 确认以下配置节已正确配置：
  - `bot:` — 平台凭证（按需配置，未配置的平台不会启动）
  - `server:` — Webhook 监听地址与端口
  - `log:` — 日志级别与输出方式
  - `concurrency:` — 并发控制策略
  - `middleware:` — 中间件开关与参数
  - `pprof:` — pprof 性能分析与健康检查端口
  - `plugins:` — 各插件自定义参数（antispam、keywordfilter、storage 等）

### 1.3 平台适配器选择

- [x] 实现多平台配置驱动自动注册：

```
config.yaml 的 bot.* 节中：
  bot.qq       → QQ 官方 Webhook API
  bot.onebot   → OneBot V11（go-cqhttp / NapCat）
  bot.discord  → Discord Gateway
  bot.satori   → Satori 协议（Chronocat / Lagrange）
  bot.milky    → Milky NTQQ 协议端

哪些节非 nil，就注册哪些适配器。
无一配置时自动启用 Terminal 终端模式。
```

---

## Phase 2: 核心初始化 ✅

### 2.1 main.go — 启动入口

- [x] 配置加载：`config.Load("config.yaml")`
- [x] 日志初始化：`logger.Init(cfg.Log)`
- [x] 构建 Bot 实例（使用 `WithPlatformRegistry` 支持多平台）
- [x] 启动与优雅关闭：

```go
bot.Start()
bot.WaitForShutdown()
bot.Shutdown()
```

### 2.2 setup.go — 中间件

- [x] `middleware.ProductionSet()` — Recover + Logging + Adaptive 限流 + CircuitBreaker + Dedup
- [x] 自定义错误处理中间件
- [x] 自定义慢请求检测中间件（>3s 阈值告警）

### 2.3 setup.go — 路由

- [x] FSM Manager：`fsm.NewManager(nil)`
- [x] 路由器：`router.New(eng, fsmMgr.Engine())` + `router.WithCommandPrefix()`

### 2.4 setup.go — 插件管理器

- [x] `plugin.NewManager(eng)` + `bot.UsePlugins(pm)`

---

## Phase 3: 内置插件注册 ✅

### 3.1-3.4 插件清单（24 个）

| 插件 | 说明 | 配置来源 |
|------|------|----------|
| permission | 角色/权限管理 | 默认 |
| acl | 黑白名单访问控制 | 默认 |
| help | /help 命令 | 默认 |
| pluginctrl | 运行时插件开关 | 默认 |
| welcome | 入群欢迎/退群告别 | 默认 |
| autoresponder | 关键词自动回复 | 默认 |
| customcommands | 用户自定义命令 | 默认 |
| moderation | 群组管理 | 默认 |
| admin | 管理命令集 | 默认 |
| debug | 调试命令集 | 默认 |
| antispam | 防刷屏限流 | `plugins.antispam` |
| keywordfilter | 关键词过滤 | `plugins.keywordfilter` |
| cooldown | 命令冷却时间 | 默认 |
| stats | 消息/命令统计 | 默认 + 自动绑定中间件 |
| auditlog | 操作审计日志 | 默认 + 自动绑定中间件 |
| scheduler | 定时任务 | 默认 |
| ratelimitui | 限流可视化管理 | 绑定 antispam + cooldown |
| pluginstore | 插件状态持久化 | 依赖 storage |
| storage | SQLite 持久化 | `plugins.storage.dsn` |
| sendqueue | 异步发送队列 | 默认 |
| subscription | 通用推送订阅 | 默认 |
| job | 一次性后台作业 | 默认 |
| verifycode | 验证码生成与验证 | 默认 |
| vevent | 虚拟事件注入 | 默认 |

- [x] `pm.RegisterMultiple()` 自动按依赖拓扑排序
- [x] `stats.Middleware()` + `auditlog.Middleware()` 绑定到 Engine

---

## Phase 4: 生命周期与运维 ✅

### 4.1 优雅关闭

- [x] `bot.WaitForShutdown()` 监听 SIGINT/SIGTERM
- [x] `bot.Shutdown()` 自动停止事件流 + 触发插件 Teardown + 关闭适配器

### 4.2 健康检查

- [x] HTTP `/health` 端点，返回 JSON：

```json
{
  "status": "ok",
  "running": true,
  "uptime": "5m30s",
  "platforms": [{"name": "qq"}]
}
```

- [x] pprof 启用时 `/health` 嵌入 pprof 服务器；未启用时启动独立 HTTP 服务器
- [x] 默认监听 `:9001`，通过 `pprof.addr` 配置

### 4.3 性能分析

- [x] pprof 服务器，通过 `config.yaml` 的 `pprof.*` 配置：

```yaml
pprof:
  enabled: false        # 是否启用
  addr: ":9001"         # 监听地址（同时承载 /health）
  auto_profile: false   # 是否定时生成分析文件
  output_dir: "data/profiles"
  enable_mutex: false
  enable_block: false
```

### 4.4 FSM 多轮对话

- [x] `fsm.Manager` 已创建，可随时注册状态机
- [ ] （待实现）注册具体 FSM 状态机

---

## Phase 5: 测试与调试 ✅

### 5.1 本地调试

- [x] 无平台配置时自动 Terminal 终端模式
- [x] 内置命令：`/help`、`/perm`、`/plugin`、`/debug` 等

### 5.2-5.3 测试

- [ ] （待实现）集成测试（testbot 包）
- [ ] （待实现）日志自动化验证

---

## Phase 6: 生产部署（暂未实施）

- [ ] `config.yaml` 配置目标平台凭证
- [ ] 日志级别调为 `info` 或 `warn`
- [ ] 并发/限流/降级参数根据实际流量调优
- [ ] 启用死信队列
- [ ] `go build -o bin/bot.exe ./cmd/bot/` 构建二进制

---

## 任务优先级速查

| 优先级 | 阶段 | 状态 |
|--------|------|------|
| P0 | Phase 1 + 2 — 骨架搭建 + 核心初始化 | ✅ |
| P1 | Phase 3 — 24 个内置插件注册 | ✅ |
| P1 | Phase 5.1 — Terminal 本地调试 | ✅ |
| P2 | Phase 4 — 生命周期 + 健康检查 + pprof | ✅ |
| P3 | Phase 5.2-5.3 — 集成测试、日志验证 | ⏳ |
| P3 | Phase 6 — 生产部署与调优 | ⏳ |

---

## 代码文件结构

```
cmd/bot/
├── main.go       # 入口：配置加载 → 日志 → Bot 构建 → pprof/health → 启动 → 优雅关闭
├── setup.go      # 多平台适配器工厂 + 中间件 + 路由 + 插件管理器
└── plugins.go    # 24 个内置插件注册，从 cfg.Plugins 读取配置参数

data/
├── db/bot.db      # SQLite 持久化存储
├── logs/          # 运行时日志目录
└── profiles/      # pprof 性能分析数据

config/
├── config.go      # + PprofConfig 配置类型
├── validate.go    # + PprofConfig.Validate()
└── config.example.yaml  # + pprof 配置节 + plugins 示例

pprof.go           # + PprofServer.AddHandler() 注入自定义端点
```

## 后续扩展

- [ ] 开发自定义业务插件（v2 API `plugin.Descriptor`）
- [ ] 接入外部 API（天气、翻译、AI 等）
- [ ] i18n 多语言支持
- [ ] subscription 数据源订阅（RSS、API 轮询）
- [ ] broadcast 批量推送
- [ ] FSM 多轮对话注册
- [ ] 集成测试（testbot 包）
