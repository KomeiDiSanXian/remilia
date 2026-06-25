# Changelog

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
