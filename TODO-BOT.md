# IM 机器人开发 TODO

> 基于 Remilia 框架开发可用的 IM 机器人，仅使用框架内置插件。
> 后续再添加自定义业务插件。

---

## Phase 1: 项目骨架搭建

### 1.1 创建项目目录结构

- [ ] 创建 `cmd/bot/` 目录作为机器人入口
- [ ] 规划文件拆分：

```
cmd/bot/
├── main.go          # 入口：配置加载 → 日志初始化 → Bot 构建 → 启动 → 优雅关闭
├── setup.go         # 中间件、路由、FSM、插件管理器的初始化函数
├── plugins.go       # 内置插件注册（bundle + 需配置的插件）
└── config.yaml      # 实际运行配置（gitignore）
```

### 1.2 配置文件

- [ ] 从 `config.example.yaml` 复制为 `cmd/bot/config.yaml`
- [ ] 填入目标平台的实际凭证（QQ / OneBot / Discord 等，按需选择）
- [ ] 将 `cmd/bot/config.yaml` 加入 `.gitignore`（含敏感信息，不入库）
- [ ] 确认以下配置节已正确配置：
  - `bot:` — 平台凭证
  - `server:` — 监听地址与端口
  - `log:` — 日志级别与输出方式
  - `concurrency:` — 并发控制策略
  - `middleware:` — 中间件开关与参数

### 1.3 选择平台适配器

- [ ] 确定目标平台（以下任选其一或多个）：
  - `platform/qq` — QQ 官方 Webhook API
  - `platform/onebot` — OneBot V11（go-cqhttp / NapCat）
  - `platform/discord` — Discord Gateway
  - `platform/satori` — Satori 协议（Chronocat / Lagrange）
  - `platform/milky` — Milky NTQQ 协议端
  - `platform/telegram` — Telegram Bot API
  - `platform/terminal` — 终端模拟（开发调试用）
- [ ] 开发阶段优先使用 `terminal.NewAdapter()` 在本地命令行调试

---

## Phase 2: 核心初始化

### 2.1 main.go — 启动入口

- [ ] 配置加载：`config.Load("config.yaml")`
- [ ] 日志初始化：`logger.Init(logger.Config{...})`
  - 开发环境：`Console: true, Level: "debug"`
  - 生产环境：`Console: false, File: true, Level: "info"`
- [ ] 构建 Bot 实例：

```go
bot, err := remilia.NewBotBuilder().
    WithPlatformAdapter(adapter).  // 选定的平台适配器
    WithName("my-bot").
    WithVersion("0.1.0").
    Build()
```

- [ ] 启动与优雅关闭：

```go
bot.Start()
bot.WaitForShutdown()
bot.Shutdown()
```

### 2.2 setup.go — 中间件配置

- [ ] 注册生产中间件集：`eng.Use(middleware.ProductionSet()...)`
  - 内含：Recover + Logging + Adaptive 限流 + CircuitBreaker + Dedup
- [ ] （可选）添加自定义中间件：
  - 错误处理中间件 — 统一错误日志上报
  - 慢请求检测中间件 — 超过阈值告警

### 2.3 setup.go — 路由配置

- [ ] 创建 FSM Manager：`fsmMgr := fsm.NewManager(nil)`
- [ ] 创建路由器并绑定到 Bot：

```go
rtr := router.New(eng, fsmMgr.Engine())
rtr.Route(router.WithCommandPrefix())
bot.UseRouter(rtr)
```

### 2.4 setup.go — 插件管理器

- [ ] 创建插件管理器：`pm := plugin.NewManager(eng)`
- [ ] 绑定到 Bot：`bot.UsePlugins(pm)`
- [ ] （可选）添加生命周期监听器用于日志记录

---

## Phase 3: 内置插件注册

### 3.1 Core 核心插件（必选）

通过 `bundle.Core()` 一次性注册：

- [ ] `permission` — 角色/权限管理（其他插件的基础依赖）
- [ ] `acl` — 黑白名单访问控制
- [ ] `help` — `/help` 命令，自动聚合所有已注册命令

```go
pm.RegisterMultipleAtomic(bundle.Core())
```

### 3.2 All 通用插件（推荐）

通过 `bundle.All()` 注册（包含 Core + 以下插件）：

- [ ] `cooldown` — 单命令冷却时间控制
- [ ] `welcome` — 入群欢迎 / 退群告别消息
- [ ] `autoresponder` — 关键词触发自动回复
- [ ] `moderation` — 群组管理（禁言/踢出/警告）
- [ ] `customcommands` — 用户自定义命令

```go
pm.RegisterMultipleAtomic(bundle.All())
```

### 3.3 Dev 开发插件（仅开发环境）

- [ ] `admin` — `/plugin`、`/perm`、`/acl`、`/status` 等管理命令
- [ ] `debug` — `/debug` 调试命令集

```go
if isDev {
    pm.RegisterMultipleAtomic(bundle.Dev())
}
```

### 3.4 需手动配置的插件（按需注册）

以下插件需要传入配置参数，不包含在 bundle 中，按需单独注册：

- [ ] **antispam** — 反垃圾/防刷屏

```go
antispam.NewPlugin(antispam.Config{
    UserRate: 5, UserBurst: 10,
    GroupRate: 30, GroupBurst: 50,
    BanOnViolation: true, BanDuration: 5 * time.Minute,
})
```

- [ ] **storage** — SQLite 持久化存储

```go
builtinstorage.New(infrastorage.WithDSN("data/bot.db"))
```

- [ ] **pluginstore** — 插件状态持久化（依赖 storage）

```go
pluginstore.New()
```

- [ ] **pluginctrl** — 运行时插件开关控制

```go
pluginctrl.New()
```

- [ ] **stats** — 消息/命令统计

```go
sp := stats.NewPlugin()
// 注册后绑定中间件
eng.Use(sp.Middleware())
```

- [ ] **auditlog** — 操作审计日志

```go
auditlog.New()
// 注册后绑定中间件
eng.Use(auditPlugin.Middleware())
```

- [ ] **scheduler** — 定时任务（固定间隔 / Cron 表达式）

```go
scheduler.NewPlugin()
```

- [ ] **sendqueue** — 异步消息发送队列（防平台限流）

```go
sendqueue.New(sendqueue.DefaultConfig())
```

- [ ] **keywordfilter** — 关键词过滤（敏感词/违禁词）

```go
keywordfilter.New(keywordfilter.Config{
    Keywords: []string{"敏感词1", "敏感词2"},
    OnMatch:  func(...) { /* 处理匹配 */ },
})
```

- [ ] **ratelimitui** — 限流可视化管理（绑定 antispam + cooldown）

### 3.5 插件中间件绑定

- [ ] 将 `stats.Middleware()` 注册到 Engine（统计所有消息）
- [ ] 将 `auditlog.Middleware()` 注册到 Engine（审计所有命令调用）

---

## Phase 4: 生命周期与运维

### 4.1 优雅关闭

- [ ] 确保 `bot.WaitForShutdown()` 正确监听系统信号（SIGINT / SIGTERM）
- [ ] `bot.Shutdown()` 中框架自动完成：
  - 停止接收新事件
  - 等待处理中的事件完成
  - 触发插件 Teardown（pluginstore 自动持久化状态）
  - 关闭平台适配器连接

### 4.2 健康检查

- [ ] 使用 `bot.IsRunning()` 定期检查 Bot 状态
- [ ] （可选）暴露 HTTP `/health` 端点供外部监控

### 4.3 性能分析（仅开发环境）

- [ ] 启用 pprof server：

```go
pprofSrv := remilia.NewPprofServer(remilia.PprofConfig{
    Enabled: true, Addr: "localhost:9001",
})
pprofSrv.Start()
defer pprofSrv.Stop(ctx)
```

### 4.4 FSM 多轮对话（可选）

- [ ] 如需多步骤交互流程（注册、问卷等），通过 `fsm.Manager` 注册状态机
- [ ] 参考 `examples/showcase/signup.go` 的实现模式

---

## Phase 5: 测试与调试

### 5.1 本地调试

- [ ] 使用 `terminal.NewAdapter()` 在命令行模拟消息收发
- [ ] 逐一验证以下内置命令：
  - `/help` — 命令列表
  - `/ping` — 连通性
  - `/status` — Bot 状态
  - `/perm` — 权限管理（Dev 插件）
  - `/plugin` — 插件管理（Dev 插件）
  - `/debug` — 调试信息（Dev 插件）

### 5.2 集成测试

- [ ] 使用 `testbot` 包编写自动化测试
- [ ] 测试插件注册顺序和依赖解析是否正确
- [ ] 测试中间件链是否按预期工作（限流、去重、熔断）

### 5.3 日志验证

- [ ] 确认各级别日志输出正确
- [ ] 确认审计日志记录了权限变更和命令调用

---

## Phase 6: 生产部署

### 6.1 切换生产适配器

- [ ] 将 `terminal` 适配器替换为目标平台适配器
- [ ] 配置正确的平台凭证和连接参数
- [ ] 关闭 Dev 插件（admin / debug），避免暴露管理接口

### 6.2 生产配置调优

- [ ] 日志级别调整为 `info` 或 `warn`
- [ ] 并发限制根据服务器性能设定（推荐 100-1000）
- [ ] 限流参数根据实际流量调整
- [ ] 启用降级策略（CPU / 内存阈值）
- [ ] 启用死信队列（失败事件持久化）

### 6.3 构建与发布

- [ ] 使用 `go build` 或 `.goreleaser.yaml` 构建二进制
- [ ] （可选）Docker 容器化部署
- [ ] 确保 `config.yaml` 通过环境变量或挂载卷注入，不打包进镜像

---

## 任务优先级速查

| 优先级 | 阶段 | 说明 |
|--------|------|------|
| P0 | Phase 1 + 2 | 骨架搭建 + 核心初始化，Bot 能启动 |
| P1 | Phase 3.1-3.2 | Core + All 内置插件注册，基础命令可用 |
| P1 | Phase 5.1 | Terminal 本地调试验证 |
| P2 | Phase 3.3-3.4 | Dev 插件 + 按需插件注册 |
| P2 | Phase 4 | 生命周期、健康检查、pprof |
| P3 | Phase 5.2-5.3 | 集成测试、日志验证 |
| P3 | Phase 6 | 生产部署与调优 |

---

## 后续扩展（本次不做）

> 以下内容在内置插件验证完毕后再开展：

- [ ] 开发自定义业务插件（v2 API `plugin.Descriptor`）
- [ ] 接入外部 API（天气、翻译、AI 等）
- [ ] i18n 多语言支持（加载 `locales/*.yaml`）
- [ ] subscription 数据源订阅（RSS、API 轮询）
- [ ] broadcast 批量推送
- [ ] 多平台同时运行（BotManager 管理多实例）
