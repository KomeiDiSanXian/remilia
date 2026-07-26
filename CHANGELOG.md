# Changelog

## [Unreleased]

## v1.21.1 (2026-07-26)

### 🐛 API 安全加固

- **config 密钥脱敏静默失效修复**: 因 Go 结构体只有 yaml 标签、json.Marshal 输出 Go 字段名，与脱敏路径不匹配导致密钥明文返回。新增 `normalizeKey`/`lookupKey` 归一化匹配，覆盖 `api_key`/`token`/`secret`/`password`/`access_token`
- **CORS 通配放行修复**: `Access-Control-Allow-Origin: *` 改为仅放行 `tauri://` 和本机回环地址，避免恶意网页跨域调用管理 API
- **未配置 api_key 时远程访问修复**: 降级策略从"无条件放行"改为"仅本机回环可访问"，防止监听 `0.0.0.0` 时管理 API 对整个网络裸奔

### 🐛 Engine 核心修复

- **OnCommand handler 同步阻塞修复**: 未设置 `execProfile` 导致命令 handler 始终同步执行在平台派发 goroutine，慢命令（AI、外部 API）阻塞该平台所有会话。现为每个命令匹配器创建执行 profile，使其正确进入 ExecPool 异步调度
- **ExecPool 任务 panic 兜底修复**: 池中 handler 逃逸的 panic（中间件链构造、中间件自身）直接终止进程。新增 `runPoolTask` 带 `defer/recover` 保护
- **processEventMatchers Block 语义失效修复**: 阻断判定在入池成功后被 `continue` 跳过，导致 `SetBlock(true)` 行为不确定、会话等待回复继续命中后续匹配器造成重复处理。现修复为入池前计算阻断标记，入池后立即 break
- **Context 异步执行数据竞争修复**: 入池前缺少 `Clone`，池 goroutine 与派发循环并发写入同一个 `*Context`，造成 SetMatcher 撕裂、deadline/span 互相覆盖

### 🔒 凭据泄露修复

- **milky 适配器**: WebSocket 连接错误中原样返回含 `access_token` 的 URL，现予以脱敏
- **QQ token 管理器**: `BotInfo` debug 日志泄漏 `AppSecret` 长期凭据；`access_token` 刷新日志直接记录活凭据内容，现改为仅记录 token 长度
- **Telegram 客户端**: `net/http` 传输错误（超时/DNS 抖动）以 `*url.Error` 形式携带完整请求 URL，bot token 嵌入路径中。新增 `redactedError` 包装器，格式化时抹掉 token
- **OneBot 适配器**: 使用 `WriteMessage` 发送关闭帧导致 `panic("concurrent write to websocket connection")`，改为 `WriteControl` 解决

### 🐛 中间件修复

- **BackpressureBlock 永久堆积修复**: 阻塞在信号量上时不监听 `ctx.Done()`，一旦 maxInFlight 个 handler 卡在不响应 ctx 的 IO 上，后续事件 goroutine 永久堆积直至 OOM。现添加 `select` 双路监听
- **CircuitBreaker 无限震荡修复**: `SuccessThreshold > HalfOpenMaxRequests` 时，半开窗口永远达不到闭合所需的成功数，熔断器在 开↔半开 之间无限震荡。新增启动和热更新时的钳制逻辑
- **重试退避溢出修复**: `BackoffBase * (1<<shift)` 在 attempt 较大时溢出为负值，导致零延迟重试。现钳制到 `BackoffMax`
- **SlowHandler 错误屏蔽修复**: 此前用 `stdCtx.Err() != nil` 判断是否超时，但该错误可能来自父级取消而非本中间件注入的监控 deadline，导致真实业务错误被静默丢弃。现改为检查 `errors.Is(err, DeadlineExceeded) && originalCtx.Err() == nil`

### 🐛 基础设施修复

- **audit/pprof 二次 Close panic 修复**: 多条停机路径（显式 Close 与 defer 清理）先后关闭 `stopCh`，无条件 close 已关闭的 channel 会 panic。新增 `sync.Once` 保护
- **DLQ Close 死锁修复**: `DropPolicyBlockUntilSpace` 生产者持有 `enqueueMu` 阻塞在 channel 发送上，Close 抢不到该锁造成死锁。新增 `closing` 通道先唤醒生产者，再安全 close channel
- **HTTP Server Slowloris 防护**: 零值 `ReadHeaderTimeout` 表示"永不超时"，攻击者可逐字节发送请求头长期占住连接。设置 `ReadHeaderTimeout=10s`、`ReadTimeout=30s`、`IdleTimeout=120s`、`MaxHeaderBytes=1MB`

### 🐛 平台适配器修复

- **Discord handler 叠加泄漏**: `Start` 可被重复调用（重连/重启），旧 handler 未注销导致同一事件被重复分发。`registerHandlers` 返回注销函数列表，`defer` 统一清理
- **Discord send-on-closed-channel 竞态**: `close(eventCh)` 时 discordgo goroutine 可能仍停留在 `send()` 的 select 上，随机选择时可能选中"向已关闭 channel 发送"造成 panic
- **OneBot fetchBotIdentity 时序**: 在 `receiveLoop` 启动前同步调用 `get_login_info`，响应永远无法送达导致每次连接阻塞数秒、`botID` 为空引发自我回复成环
- **Satori quote 解析丢失正文**: 自闭合 `<quote/>` 被 HTML 解析器误认，后续兄弟节点变为其子节点导致整条正文丢失。正则预剥离后再解析
- **Satori XML 注入**: `%q` 用反斜杠转义双引号，XML 不识别该转义，用户输入可提前闭合属性注入任意元素

### 🐛 内置插件修复

- **Cooldown GC 绕过**: 固定 24h GC 提前删除 7 天冷却期的记录，`Allow` 把"键不存在"视为放行。现按 `max(entry.cooldown, maxAge)` 判定回收门槛
- **Admin 权限提升**: 普通 admin（`*:*` 权限即可匹配 `perm.role`）可直接 `/perm role <自己ID> superadmin` 提权。现要求授予 superadmin 必须是 superadmin 本人
- **Job panic 进程终止**: 后台作业未捕获的 panic 直接终止进程。新增 `safeInvoke` 将 panic 转为普通错误
- **Kick --ban 误判**: `len(args) >= 3` 将可选"原因"参数视为永久拉黑开关，照文档操作的用户被意外拉黑。改为显式 `--ban`/`拉黑` 标记
- **Stats 命令 map 无限增长**: 用户消息中的任意 token 被计入命令统计，map 既无淘汰又每 5 分钟整体落盘，可撑爆内存和磁盘。限制 `maxTrackedCommands=1000`、`maxCommandKeyLen=64`
- **Subscription 死锁**: `Subscribe`/`Unsubscribe` 已持写锁后调用 `save()`（内部再取读锁），RWMutex 不可重入导致自死锁。拆分为 `save()`/`saveLocked()`
- **Skill add 返回缺失**: 发送帮助提示后未 `return nil`，继续执行注册流程造成重复操作

### 🔧 HTTP 安全增强

- **HTTP Server 新增超时配置**: `ReadHeaderTimeout=10s`、`ReadTimeout=30s`、`IdleTimeout=120s`、`MaxHeaderBytes=1MB`，防止 Slowloris 攻击

## v1.21.0 (2026-07-26)

### 🚀 新平台适配器：Telegram

- **零外部依赖**: 使用 `net/http` 直接调用 Telegram Bot API，无需第三方 SDK
- **长轮询 + Webhook 双模式**: 默认长轮询（`getUpdates`），配置 `webhook` 块自动切换为 Webhook 模式
- **完整事件映射**: 私聊/群组/频道消息、编辑消息、CallbackQuery、Bot 加入/移除等 → `platform.Event`
- **Sender 全面实现**: 文本、MarkdownV2、图片/音频/视频/文件（URL 直传 + 二进制上传）、内联键盘、消息编辑、删除、Reaction、Typing 指示
- **HelloBot 可用**: 开箱即用，只需 `token` 即可运行

### 🔧 配置与构建

- **config**: 新增 `TelegramConfig`（`token` / `poll_timeout` / `webhook`），`HasChanged` 包含 Telegram
- **setup.go**: 添加 `telegram` factory，按配置自动启用
- **config.example.yaml**: 新增 Telegram 长轮询和 Webhook 两种模式示例

## v1.20.1 (2026-07-17)

### 🔒 SSRF 防护增强

- **附件下载 DNS 级 IP 校验**: `isAllowedDownloadURL` 对域名执行 DNS 解析，检查所有解析结果是否为公网 IP
- **专有 HTTP client**: 引入 `attachmentHTTPClient`，在 dial 阶段二次校验目标 IP，防止 DNS 重绑定攻击
- **重定向安全检查**: 对 3xx 跳转目标同样执行 `isAllowedDownloadURL` 校验

### 🧠 AI 插件改进

- **SkillRegistry 命名空间隔离**: 由全局 `name→Skill` 改为 `ownerID+name→Skill` 复合主键，避免用户之间的技能名冲突
- **命令参数安全过滤**: `executeRealCommand` 对 LLM 生成的参数做 ASCII 可打印字符校验，防止命令注入
- **ToolAllowlist 配置**: 新增 `tool_allowlist` 配置项，显式控制哪些命令自动暴露为 AI 工具
- **ToolRegistry 并发安全**: 新增 `sync.RWMutex` 保护，消除 Setup 后并发读写的竞态风险
- **会话并发串行化**: 新增 `Session.turnMu`，确保同一会话的对话回合严格串行
- **CleanupExpired 加锁修复**: 清理过期会话时先持有 session 锁再检查 TTL
- **发现时机延迟**: `discoverTools` 从插件 Setup 阶段移至所有插件注册完成后执行
- **配置校验增强**: `temperature`/`top_p`/`max_retrie`s 加载时检查合法范围，`configFloat`/`configInt` 支持 `json.Number`
- **断连修复**: `trigger_cmd` 配置加载简化，消除死分支

## v1.20.0 (2026-06-30)

### 🧩 新 Context 便利方法

- **TryEditMessage** — 编辑 bot 已发送的消息（基于 MessageEditor）
- **TryAddReaction / TryRemoveReaction** — 对消息添加/移除表情回应（基于 ReactionSender）
- **TryDeleteMessage** — 撤回 bot 自己发送的消息（基于 MessageDeleter）
- **TrySendTyping** — 发送"正在输入"指示（基于 TypingNotifier）

## v1.19.0 (2026-06-30)

### 🚀 性能优化

- **SQLite WAL + synchronous=NORMAL**: messagelog & 业务存储启用 WAL 模式和 NORMAL 同步级别，消除 `FlushFileBuffers` 瓶颈（pprof 中占 80ms/20%）
- **auditlog 批量写入**: channel + flushLoop 替代 per-entry goroutine，消除高频 goroutine 创建（pprof 中 272 次/15s）
- **messagelog flush 频率**: 100ms → 500ms，减少写事务频率
- **auto_vacuum**: 新增 `PRAGMA auto_vacuum = INCREMENTAL`，Clear 时自动回收空闲页
- **Future 分配基准测试**: 实测 ~35 ns/op，112 B/op
- **Dispatcher 队列注入基准测试**: 实测 ~4.5 ns/op，0 allocs

## v1.18.0 (2026-06-30)

### 🚀 新特性

- **OutboundDispatcher**: 新增出站任务调度器，`ctx.Reply()` 现在返回 `*future.Future[platform.SendResult]`，提交即返回，发送在后台执行。
  - 同一 Chat 消息严格 FIFO，不同 Chat 并发发送
  - 不阻塞 Handler goroutine（ExecPool 与 SendPool 解耦）
  - 支持三种 Shutdown 语义：Close / Drain / ForceClose
  - 支持 Retry、Timeout、Metrics、Logging 装饰器（sender.Chain）
  - DispatcherHooks 提供完整的发送生命周期观察

- **infra/future**: 新增泛型 Future 类型
  - `Wait(ctx)` / `Result()` / `IsDone()` / `MustWait(ctx)` / `Done()`
  - `sync.Once` 保护，多次 Resolve 安全
  - 零额外 GC 压力

### 🧾 Reply 语义变更

`ctx.Reply()` 由同步发送改为异步提交：

之前：
```go
res, err := ctx.Reply(msg)  // 阻塞直到发送完成
```

之后：
```go
ctx.Reply(msg)               // 提交即返回，发送在后台

// 仍可等待结果：
future := ctx.Reply(msg)
res, err := future.Wait(ctx)
```

**兼容性**: Go 允许忽略返回值，所有 `ctx.Reply(msg)` 调用零修改编译通过。

完整设计文档：[docs/05-performance/OUTBOUND_DISPATCHER_PLAN.md](docs/05-performance/OUTBOUND_DISPATCHER_PLAN.md)

## v1.17.1 (2026-06-29)

### 🔧 修复

- **`middleware/middleware.go` 可读性提升** — 将 `RateLimitTokenBucketWithConfig` 中内联的 120 行分片桶逻辑提取为包级类型和方法（`rateLimitShard`/`rateLimitShards`/`fnv1aHash`/`cleanupIfNeeded`/`getOrCreateLimiter`），主函数缩减至 40 行
- **`latency` 字段改为 `latency_ms`** — 结构化日志字段名明确反映单位为毫秒，避免 `time.Duration` 的 JSON 序列化歧义

### 🔄 变更

- **Metrics 中间件文档完善** — 补充 godoc 说明其与 Logging 的关系（Metrics 是轻量 Debug 版本）

## v1.17.0 (2026-06-29)

### ✨ 新功能

- **用户自定义技能（User Skill）** — 用户可通过 `/ai skill add` 上传 Markdown 注册自己的 Prompt-only Skill，名称自动 `u_` 前缀防冲突
- **两阶段注册流程** — `/ai skill add <名称>` 后，FSM 自动等待用户下一条消息或 .md 附件作为内容，`cancel`/`取消` 可放弃
- **技能提升为系统级** — 管理员可通过 `/ai skill promote` 将用户技能提升为所有用户可见的系统技能（去掉 `u_` 前缀）
- **技能启用/禁用开关** — 用户可独立控制每个技能的启用状态，禁用的技能不注入会话
- **技能调用统计** — 自动记录每个技能的调用次数

### 🔧 修复

- **AI 插件生命周期 context** — 替换 `context.Background()` 为 `lifecycleCtx`，插件关闭时及时取消后台操作（附件下载、总结生成等）
- **`doSummary` 关闭时静默退出** — 插件关闭导致的 LLM 调用取消不再向用户回复错误的总结失败消息
- **技能内联注册换行支持** — `strings.SplitN` 按空格分割 → `strings.Fields` 按任意空白分割，支持换行分隔的 Markdown 内容
- **`/ai cancel` 不再被 FSM 消费** — FSM 检查仅作用于 `@bot`/私聊路径，命令路径不受影响

### 🔄 变更

- **`Skill` 结构体新增 `OwnerID`、`Enabled`、`UsageCount` 字段** — `OwnerID = "system"` 为系统技能，用户 ID 为用户技能
- **`SkillRegistry` 重构为线程安全** — 引入 `sync.RWMutex` 和 `byOwner` 二级索引，支持运行时注册/注销
- **`RegisterSkill` 自动设置 `OwnerID = "system"`** — 向后兼容，外部插件调用不受影响
- **`buildSkillTools` 仅注入系统技能** — 用户技能的子 Agent 无法调用其他用户的技能
- **子命令定义完善** — 所有 `/ai` 子命令补充 Description，`/ai skill add/list/remove/enable/disable/promote/info` 使用嵌套 `SubCommand` 定义
- **配置项新增** — `max_user_skills`（默认 10）、`max_user_skill_prompt_len`（默认 2000）

## v1.16.0 (2026-06-25)

### ✨ 新功能

- **支持 QQ 官方平台 `GROUP_MESSAGE_CREATE` 事件** — 新增 `GroupMessageCreate`、`GroupMemberAdd`、`GroupMemberRemove` 事件类型常量和解析逻辑
- **`MentionsEvent` 接口实现** — QQ 适配器解析消息中的 `mentions` 数组，暴露 `UserInfo.IsSelf` 标记
- **`OnMentionedBot()` / `OnMentionedBotOrNoMentions()` 规则** — 插件可通过规则控制是否响应 @ 机器人的消息，避免 @ 他人时误触发
- **消息日志 `message_mentions` 关联表** — 独立表存储 @ 用户信息，零序列化开销，支持批量查询

### 🔧 修复

- **展示名称使用 `author.username`** — `populateGroupAt` 中 `DisplayName` 从 `author.id`（OpenID）改为 `author.username`（用户昵称）
- **`GROUP_MESSAGE_CREATE` 的 `content` 剥离 `<@id>` 协议编码** — 还原用户实际发送的文本，使 `OnCommand`/`OnRegex` 等规则正常工作
- **`modelToEntry` 补全 `CreatedAt` 字段** — DB 查询返回正确的入库时间

### 🔄 变更

- 所有命令类插件（builtin 9 个 + cmd/bot 12 个，共 46 个注册点）添加 `OnMentionedBotOrNoMentions()` 保护

---

## v1.15.1 (2026-06-25)

### 🐛 修复

- **修改 `log.level` 等无关字段不再导致 Adapter 断连** — 平台热更新监听器仅在 `bot.*` 配置实际变化时触发
- **修改 `sampling_rate` 不再在固定采样模式下打印误报警告** — 仅当采样率值变化时才调用 `SetSamplingRate`
- **修改无关配置不再触发 logger 重初始化** — 仅当 `log.format/console/file/file_path` 变化时才调用 `logger.Init`
- **修改无关配置不再重复推送中间件更新** — retry/dedup/degradation/pprof 均带变化检测栅栏，跳过无操作调用
- **修复 `lastBotCfg` 数据竞争** — 并发 API 请求修改配置时 `lastBotCfg` 读写加锁保护

---

## v1.15.0 (2026-06-25)

### 🔥 全栈配置热更新

所有标记为 `[H]`（即时生效）和 `[H⚠]`（有条件生效）的配置字段现在支持运行时修改，**无需重启 Bot**：

- **中间件结构开关** — `middleware.recover`、`logging`、`metrics` 等）改为运行时通过 `Bridge.GetMiddlewareConfig()` 检查，`config.yaml` 中开关变化即时生效
- **白名单热读** — `auth.whitelist` 增删用户即时生效（无需重启）
- **日志级别/时间格式** — `log.level` / `time_format` 修改后即时生效
- **日志输出格式** — `log.format` / `console` / `file` / `file_path` 支持热更新（自动关旧文件、建新 writer）
- **重试配置** — `retry.*` 全部支持热更新
- **限流/去重参数** — `rate_limit.burst`、`dedup.max_size` / `default_ttl` 修改后即时生效
- **慢处理监测** — `slow_handler.enable` / `threshold` 每次请求热读
- **反压** — `backpressure.limit>0` 启用，`<=0` 关闭（limit 值本身因固定 semaphore 保持创建时值）
- **自适应降级** — `degradation.*`（除 `recovery_interval` / `delay_queue_size`）下一监控周期生效，`monitor_interval` 支持 ticker 重建，`strategy` 运行时读取
- **分布式追踪** — `tracing.include_event_detail` 运行时开关；`sampling_rate`（仅自适应模式）即时生效
- **pprof 参数** — `auto_profile` / `profile_interval` / `profile_duration` / `enable_mutex` / `enable_block` 运行时生效

### 🔄 平台适配器热替换（SyncPlatforms）

`Bot.SyncPlatforms(desired map[string]Adapter)` 支持运行时**增、删、改**平台适配器：

- **增** — `registry.Register()` + `Start()`，其他平台不受影响
- **删** — `registry.Remove()` + `Stop()`，平滑断开连接
- **改** — `registry.Replace()` + Stop 旧 + Start 新，连接零停机切换
- 重建 `adapterSnapshot` 原子替换，热路径零锁读取
- `Bot.Stop()` 自动关闭所有热替换的适配器

### 🧩 模块化重构

- **`cmd/bot` 拆分为独立 Go 模块** — 创建 `cmd/bot/go.mod`（module `github.com/KomeiDiSanXian/remilia/cmd/bot`），插件重型依赖隔离在子模块
- **`go.work`** 增加 `cmd/bot` 和 `examples/httpclient-demo`
- **提取单平台工厂函数** — `platformFactories()` / `buildDesiredAdapters()` / `registerPlatforms()` 供热更新 listener 复用
- **`config.example.yaml` 热更新标注** — 每字段 `[H]` / `[H⚠]` / `[R]` 标记

### 🧪 测试

- **新增 6 个测试文件/函数**：Bot.SyncPlatforms（5 测试）、AdaptiveSampler（6 测试）、Bridge 扩展、Degradation MonitorInterval/Strategy、SetIncludeEventDetail、Logger SetLevel/SetTimeFormat、Pprof UpdateConfig
- **pprof 测试改用动态端口** — `net.Listen("127.0.0.1:0")` 消除并行端口冲突
- **修复时序敏感断言** — engine shutdown / retry sleep 测试放宽并行下界
- **修复 zerolog 全局状态污染** — logger 测试 `saveZerologState` + `t.Cleanup`
- **`-race` 全量通过** — 91 个包零 data race 警告

### 📝 配置文档

- `config.example.yaml` 和 `cmd/bot/config.default.yaml` 完整标注每个字段的热更新能力

---

## v1.14.1 (2026-06-25)

### 🐛 修复

- **修复 `config.Watcher` 未启动导致热更新不生效** — `cmd/bot/main.go` 创建并启动 `config.NewWatcher("config.yaml")`
- **统一日志前缀** — `[bot]` → `[remilia]`（4 个文件）

---

## v1.14.0 (2026-06-25)

### 🏗️ 配置系统重构

- **配置结构重组** — 所有平台相关的配置归入 `bot.<platform>` 下：
  - `server` → `bot.qq.webhook`（`webhook` 和 `token_manager` 也已归入 `bot.qq`）
  - `concurrency` → `middleware.backpressure`（同时更名为 `backpressure`）
- **删除 BigCache 残留字段** — 移除 `WebhookConfig` 中 7 个死字段（`DedupEnable`、`Shards`、`LifeWindow`、`CleanWindow`、`MaxEntrySize`、`HardMaxCacheSize`、`MaxEntriesInWindow`）
- **删除 `ServerConfig` 类型** — `Host`/`Port`/`ShutdownTimeout` 合并到 `WebhookConfig`
- **统一配置默认值模式** — 所有平台 Config 使用指针接收者 `setDefaults()`，在构造函数中调用
- **`${VAR}` 环境变量替换支持** — `loadRaw()` 中加入 `os.ExpandEnv`，YAML 中的 `${VAR}` 语法现在可用
- **修复 `getEnvInt` 解析失败回退问题** — 非数值环境变量不再返回 0，而是回退到默认值

### 🎯 中间件配置化

- **配置驱动中间件注册** — `Recover`/`Logging`/`Metrics`/`Auth`/`Dedup`/`Backpressure`/`SlowHandler` 现在根据配置的 `enable` 标志和参数注册，而非硬编码无条件注册
- **热更新增加 Enable 检查** — `RateLimit.Enable`/`Dedup.Enable`/`Degradation.Enable` 现在在热重载时被检查，禁用的中间件不再推送更新

### 🔧 平台层修复与增强

- **修复 `discord.InteractionsAdapter.Stop()` session 泄漏** — 未调用 `session.Close()`
- **修复 `discord.InteractionsAdapter` send-on-closed-channel 竞态** — 移除冗余的 eventCh 关闭
- **修复 `qq.Adapter.Start()` nil eventCh 静默成功** — 改为返回错误
- **统一 `SendError` 包装** — 所有 5 个平台的 `Send()` 现在都返回 `platform.SendError` 结构化错误
- **QQ 平台补齐 `ReactionSender` + `MessageDeleter`** — 通过 OpenAPI 实现表情表态和消息撤回
- **`safeInvoke`/`safeDispatch` 去重** — 移除 4 个平台中相同的包装函数，直接调用 `platform.SafeDispatch`
- **移除未使用的字段** — `qq.Adapter.wg`、`discord.GatewayAdapter.ctx`

### 🔄 命名变更

- `middleware.ConcurrencyLimit()` → `middleware.Backpressure()`
- `middleware.ConcurrencyPolicy` → `middleware.BackpressurePolicy`
- `ConcurrencyDrop/Block/TryWait` → `BackpressureDrop/Block/TryWait`
- `config.ConcurrencyConfig` → `config.BackpressureConfig`
- `config.TokenConfig` → `config.TokenManagerConfig`

### 🧹 其他清理

- 清除所有 YAML 配置文件中的 `vX.Y.Z+` 版本标记
- 同步 `config.default.yaml` 与 `config.example.yaml`
- 更新所有示例和测试文件以匹配新配置结构

## v1.12.4 (2026-06-22)

### 🛡️ CI 稳定性修复

- **修复 `ExecProfile.ShouldPool` 数据竞争** — 栈分配固定数组替代共享 `snapshotBuf`，消除多 goroutine 并发排序的数据竞争
- **修复 `TestEngineShutdownWithPendingEvents` 时序依赖** — channel 同步替代 `assert.Eventually` 等待 goroutine 调度
- **修复 `TestHealthChecker` 超时** — `waitBotRunning`/`waitBotHealthy` 超时从 3s 提升至 10s
- **`addMatcher` 自动推导 `hasHandler`** — 直接设置 `Handler` 字段的场景自动标记 `hasHandler`
- **测试代码使用标准 API** — 统一使用 `Handle()` 而非直接赋值 `Handler` 字段

## v1.12.3 (2026-06-22)

### 🚀 性能优化

- **过滤无 Handler 匹配器** — `hasHandler` atomic 标记在 sortedCache 构建时排除无 Handler 匹配器，5K 匹配器场景吞吐量从 10K 提升至 3M msg/s
- **ExecProfile 预分配缓冲区** — `snapshotBuf` 复用消除热路径 `make()` 分配，GC 从 3181 次降至 5 次/10s
- **ExecProfile demoted 快速路径** — 已确认的快 Handler 跳过排序，`ShouldPool` CPU 占比从 20.54% 降至 1.48%

### 📝 文档

- 重写 README，更新特性列表和架构图
- 删除已归档设计文档和过时代码审查报告
- 修复文档中的 `logrus` → `zerolog` 引用
- 新增 `docs/05-performance/PERFORMANCE_REPORT.md`

### 🧪 Benchmark 修复

- 修复无限压力模式 semaphore 无效问题
- Drain 等待从 3s 扩展至 30s
- 延迟测量修正为 `time.Since(ev.Timestamp())`
- 添加 P50/P95/P99 百分位延迟统计

## v1.12.2

### 🛡️ 稳定性修复

- 修复所有 `-race` 检测到的数据竞争（platform/qq/adapter、plugin/infra_container、core/engine/exec_profile 等）
- 修复 bot 重启、健康探针、适配器生命周期超时
- 修复 sidecar 二进制未正确 gitignore

### ✨ 新功能

- 管理 API：配置深拷贝、健康检查调优
- Dashboard 全面 UI 重构：侧边栏、Toast、骨架屏、表单、权限管理
- Tauri 桌面壳：启动选择、按需 sidecar

### 🔧 其他

- 更新 GitHub Actions 到最新大版本
- 修复 golang.org/x/net CVE

## v1.12.1

### 🐛 修复

- 为 scheduler、admin、pluginctrl、css、coc、dnd 等插件添加 DryRun 副作用保护

## v1.12.0

### ✨ 新功能

- 新增插件：RPG 骰子系统、COC 7th 规则、D&D 5e 规则、BiliBili 客户端增强
- Bangumi API 客户端添加自定义 DNS 和代理支持
- Minecraft 服务器状态查询支持 mcsrvstat.us 回退
- 所有社区插件添加命令定义（Description/Usage/Examples）
- ping、status、info 等内置命令添加定义

### ⚡ 优化

- MergeIter + tempManager RCU 重构
- 无中间件的 handler 缓存到 compiledHandlers

## v1.11.0

### ✨ 新功能

- 多模态输入支持（图片、音频附件）
- AI 工具分类管理

### 🛡️ 安全修复

- 修复 AI 插件 SSRF 漏洞
- 修复多个数据竞争问题
- 修复 goroutine 泄漏和 double body close

### 🐛 修复

- 插件注册超时处理改进
- 多项插件 panic 和数据正确性修复

## v1.10.x

### v1.10.3

- 改进 OEM 数据刷新逻辑，添加签名头验证

### v1.10.2

- 增强 Bot 上下文（botName），改进系统提示词构建

### v1.10.1

- 优化循环和字符串拼接（client、image 模块）

### v1.10.0

- **统一 HealthNode 树模型**：summary/full 视图、kind 推导、增加 godoc

## v1.9.0

### ✨ 新功能

- **健康检查全面增强**：APIProbe headers/acceptStatus/MaxSeverity、HealthDetailer 接口、分组响应、adapter/token 健康详情
- AI 技能系统：注册、执行、自动发现
- 新增插件：CSS、ISS、Weather（含 API Probe 健康检查）

### 🐛 修复

- AI GORM session 存储在 DryRun 期间跳过（避免 nil DB panic）

## v1.8.0

### ✨ 新功能

- **AI 插件工具调用**：子命令重构、工具超时、统计追踪
- **Skill 系统**：注册和执行框架
- **自动发现**：SkillProvider 自动发现
- AI 工具：ACL 检查、反垃圾状态、审计日志查询
- `ProcessPlatformEventSync` 支持

## v1.7.0

### 🔧 重构

- **Phase 0 插件管理器重构**：Manager 拆分、注册统一、Scope 清理
- Service[T] 直接返回 T（不再返回 error），锁争用优化，ExportAs 弃用
- 替换所有已弃用的 RegisterMultiple/Smart/Atomic 调用

### ✨ 新功能

- **AI 聊天插件**：多供应商支持、工具调用
- 插件容器启动后冻结

## v1.6.x

### v1.6.5

- DryRun Logger 在依赖推断期间抑制插件 INFO 日志

### v1.6.4

- DryRun SetupContext 提供真实 goroutineMgr，无需插件检查 ctx.DryRun

### v1.6.3

- 为 scheduler/sendqueue/subscription 添加 DryRun 保护

### v1.6.2

- nil Matcher 作为 noop 处理，防止 DryRun nil 指针解引用

### v1.6.1

- noopRegistryWriter 返回 noopMatcher 避免 DryRun panic
- 替换 reflect.TypeOf 为 reflect.TypeFor
- Makefile Windows 兼容

### v1.6.0

- **三色 DryRun 依赖推断**：类型解析、循环检测、计时日志
- WASM 插件配置管理和生命周期集成
- 依赖注入容器添加值变更通知
- 蓝绿部署 draining 追踪
- EventBus 和 DI 上下文支持

## v1.5.0

### ✨ 新功能

- **WASM 插件运行时（ABI v2）**：wazero 沙箱、TLV 序列化
- 多语言插件支持：TinyGo、Rust、C
- 限流/超时/安全约束沙箱
- 35 个集成测试
- Showcase 7 个 WASM 命令演示
- 跨语言插件开发文档

### 🔧 其他

- QQ 平台 Markdown/ARK 模板消息
- 被动回复限制和过期
- 追踪和 Metrics 中间件集成
- Ping 插件

## v1.4.0

### 🔧 重构

- 中间件拆分为子包
- 插件文件标准化命名

### ✨ 新功能

- pprof 性能分析配置和验证
- Superadmin 角色和权限增强
- LevelDB 数据持久化迁移
- DryRun 模式跳过 I/O 操作
- CircuitBreaker/DedupFilter 状态持久化
- SQLite 消息日志
- Kubernetes 部署配置

## v1.3.x

### v1.3.4

- 删除已弃用的 MustAs/TryAs/GetPlugin
- 取消导出 Get/MustGet
- Service/TryService 自动追踪

### v1.3.3

- 删除 plugin.Must/Try
- 标记 Get/MustGet 为内部使用

### v1.3.2

- 弃用 legacy Must/Try/Get/MustGet
- 推荐 Service/TryService 和 Scope.Subscribe

### v1.3.1

- PluginScope 资源追踪
- ServiceProxy 防过期依赖
- 状态迁移管线

### v1.3.0

- **Matcher 级 per-channel 阻塞**替代 Per-Channel Engine

## v1.2.x

### v1.2.6

- Showcase 拆分为 8 个文件

### v1.2.5

- Router 使用优先级排序规则 + Handle 回调

### v1.2.4

- FSM 作为内置 Router 优先级，非策略规则

### v1.2.3

- 移除 builtin/conversation（由 core/fsm 替代）

### v1.2.2

- FSM 生命周期修复
- i18n 持久化修复
- 数据竞争修复

### v1.2.1

- Router + EngineManager 组合
- 共享 ExecPool
- Showcase FSM 演示

### v1.2.0

- **FSM 有限状态机引擎**
- **Adaptive Router 策略路由**
- **WASM 跨语言插件**
- **Per-Channel Engine**
- LevelDB 键值存储
- 自动回复插件
- 命令前缀自定义

## v1.1.0 (2026-05-07)

### 🛡️ 稳定性与正确性修复

- ExecProfile 懒初始化数据竞争修复
- Context matcher 跨 goroutine 竞争修复
- Matcher.GetSource 锁缺失修复
- ExecPool Drain/Submit 竞争窗口修复
- regexCache 数据竞争修复
- 熔断器配置竞争修复
- SimpleDedup goroutine 泄漏修复
- LRU 驱逐 bug 修复
- AI 插件文本编码修复
- 超长堆栈截断修复

## v1.0.0

### 🎉 初始发布

Remilia 是一个现代化、高性能、易于扩展的多平台聊天机器人框架。

#### ⚠️ 注意事项

- **Telegram 和 WeChat 适配器**为骨架实现，暂不可用于生产环境
- 要求 Go 1.26+
- 许可证：MIT
