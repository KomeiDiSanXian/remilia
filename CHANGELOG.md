# Changelog

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
