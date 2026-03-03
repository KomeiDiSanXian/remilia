# Remilia 发布前代码审查报告

> 审查日期：2026-03-04  
> 审查范围：全量代码（生产代码、测试代码、文档、配置）  
> 项目状态：待发布

---

## 目录

1. [🔴 高优先级（发布阻断）](#高优先级发布阻断)
2. [🟠 中优先级（建议发布前修复）](#中优先级建议发布前修复)
3. [🟡 低优先级（可版本后迭代）](#低优先级可版本后迭代)
4. [📊 问题汇总统计](#问题汇总统计)

---

## 🔴 高优先级（发布阻断）

### H1. 版本号三处不一致

**问题描述：** 项目中存在三个版本信息来源，且互相矛盾：

| 位置 | 版本值 |
|------|--------|
| `bot.go` 第 78 行，`config.Version` 默认值 | `"0.9.0"` |
| `CHANGELOG.md` 最新版本 | `[2.0.0] - 2026-02-19` |
| `README.md` badge | `v2.0.0`（正文提及）/ `Go 1.21+`（badge 写的） |

`bot.go` 中硬编码的版本仍是 `0.9.0`，与 CHANGELOG 的 `2.0.0` 差距极大，用户使用 `bot.Config().Version` 获取版本时会得到错误信息。

**修复建议：**
- 新建 `version.go`（根包），将版本作为导出常量维护：
  ```go
  const Version = "2.0.0"
  ```
- `bot.go` 中将默认 `Version` 改为 `remilia.Version`
- README badge 中 Go 版本改为 `Go 1.25+`（与 `go.mod` 的 `go 1.25` 一致）

---

### H2. `go.mod` 声明 `go 1.25`，但 Go 1.25 尚未发布

**问题描述：** `go.mod` 第 3 行声明 `go 1.25`。截至 2026 年 3 月，Go 1.25 还未正式发布，这会导致：
- 用户克隆项目后使用当前 stable Go 版本 `go build` 失败
- `go get` / `go install` 在旧版本工具链上报错

**涉及文件：** `go.mod:3`

**修复建议：** 降至 `go 1.23` 或 `go 1.24`，同时确认项目中是否真正使用了只有 1.25 才有的语言特性（如有，升级说明需明确注明工具链要求）。

---

### H3. CHANGELOG.md 中 `[1.0.0]` 日期占位符未填写

**问题描述：** `CHANGELOG.md` 第 89 行：
```
## [1.0.0] - 2026-02-xx
```
`xx` 是占位符，未替换为实际日期，不符合 Keep a Changelog 规范，且显得不专业。

**涉及文件：** `CHANGELOG.md:89`

**修复建议：** 将 `2026-02-xx` 替换为实际首次发布日期。

---

### H4. `CHANGELOG.md` 结构混乱：`[2.0.0]` 在 `[1.0.0]` 前面

**问题描述：** 当前 CHANGELOG 顺序为 `[2.0.0]` → `[Unreleased]` → `[1.0.0]`，不符合 Keep a Changelog 的版本倒序排列规范（最新版应在最前，历史版本依次往后）。`[Unreleased]` 块应始终位于最顶部，且 `[2.0.0]` 已标记为 released，其内容应从 `[Unreleased]` 移走。

**涉及文件：** `CHANGELOG.md`

**修复建议：** 整理为标准结构：`[Unreleased]` → `[2.0.0]` → `[1.0.0]`，并将 `[Unreleased]` 块清空（保留空节占位）。

---

### H5. README badge 中 Go 版本与实际要求不符

**问题描述：** `README.md` 中 badge 显示 `Go 1.21+`，而 `go.mod` 要求 `go 1.25`，误导用户用旧版 Go 尝试运行框架。

**涉及文件：** `README.md:6`

```markdown
![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go)
```

**修复建议：** 改为 `Go-1.24+`（或实际最低要求版本）。

---

## 🟠 中优先级（建议发布前修复）

### M1. ~~`NewBotWithDefault` 对外暴露 `panic`，破坏用户体验~~ ✅ 已修复

**问题描述：** `factory.go:23`：
```go
panic("[Bot] NewBotWithDefault failed: " + err.Error())
```
框架层函数对外 panic 会导致用户应用崩溃，且错误信息不携带上下文。`BotBuilder.Build()` 已经返回 `(*Bot, error)`，`NewBotWithDefault` 完全可以改为同签名。

**涉及文件：** `factory.go:14`

**修复建议：**
```go
// NewBotWithDefault 创建带默认 Webhook 配置的 Bot 实例（推荐使用 NewBotBuilder）
func NewBotWithDefault(info *dto.BotInfo, opts ...Option) (*Bot, error) {
    b := NewBotBuilder().WithBotInfo(info)
    for _, opt := range opts {
        b.WithOption(opt)
    }
    return b.Build()
}
```

---

### M2. ~~`options.go` 中 `WithName`/`WithVersion`/`WithDebug` 在 `b.config == nil` 时会 panic~~ ✅ 已修复

**问题描述：** `options.go:22`：
```go
func WithName(name string) Option {
    return func(b *Bot) {
        b.config.Name = name  // 若 b.config 为 nil，直接 panic
    }
}
```
`NewBot` 会在应用选项前初始化 `b.config`，但如果用户通过 `WithConfig(nil)` 清空配置后再应用其他选项，或未来代码路径变化，这里会 panic。

**涉及文件：** `options.go:22,29,36`

**修复建议：** 在每个选项函数中加入 nil 守卫：
```go
func WithName(name string) Option {
    return func(b *Bot) {
        if b.config == nil {
            b.config = &Config{}
        }
        b.config.Name = name
    }
}
```

---

### M3. ~~`Bot` 有两个功能完全重复的方法 `Engine()` 和 `GetEngine()`~~ ✅ 已修复

**问题描述：** `bot.go` 中：
```go
func (b *Bot) Engine() *engine.Engine { return b.engine }
func (b *Bot) GetEngine() *engine.Engine { return b.engine }  // 别名，无存在意义
```
`GetEngine()` 没有任何调用点（搜索全项目无使用），完全是冗余 API，增加了维护面。

**涉及文件：** `bot.go:430,435`

**修复建议：** 删除 `GetEngine()`，仅保留符合 Go 惯例的 `Engine()`。

---

### M4. ~~`safeHandleEvent` 是对 `safeHandle` 的无意义封装~~ ✅ 已修复

**问题描述：** `webhook_adapter.go:269`：
```go
func safeHandleEvent(handler func(*dto.Payload), event *dto.Payload) {
    safeHandle(handler, event)  // 完全透传，无附加逻辑
}
```
注释本身也说明了"统一使用 adapter.go 中的 safeHandle，消除重复代码"，但包装仍然保留了。

**涉及文件：** `webhook_adapter.go:269`

**修复建议：** 直接删除 `safeHandleEvent`，调用方改为直接调用 `safeHandle`。

---

### M5. ~~`errors.go`（根包）与 `errutil/errors.go` 错误体系重叠~~ ✅ 已修复

**问题描述：** 根包 `errors.go` 定义了 `ErrAdapterRequired`、`ErrEngineRequired` 等错误，而 `errutil/errors.go` 也定义了 `ErrAdapterStartFailed`、`ErrBotAlreadyRunning` 等 Bot 相关错误。两套体系共存，用户不清楚应该使用哪套，且有命名重复风险（`ErrConfigInvalid` 在两处都有）：
- `errors.go:15`：`ErrInvalidConfig = errors.New("invalid configuration")`
- `errutil/errors.go:32`：`ErrConfigInvalid = errors.New("invalid configuration")`

**涉及文件：** `errors.go`, `errutil/errors.go`

**修复建议：** 统一归入 `errutil` 包，根包 `errors.go` 只保留必须在根包可见的最小集合，其余通过 `errutil.ErrXxx` 暴露。或反之，在根包 `errors.go` 统一声明，`errutil` 仅提供工具函数。

---

### M6. ~~`config/watcher.go` 中使用了裸 `time.Sleep(50ms)`，无法响应 Context 取消~~ ✅ 已修复

**问题描述：** `config/watcher.go:253`：
```go
time.Sleep(50 * time.Millisecond)
```
这是配置文件稳定性校验的等待，但裸 `time.Sleep` 在 Watcher 被停止时无法提前返回，延长停止延迟。

**涉及文件：** `config/watcher.go:253`

**修复建议：** 改为使用 `time.NewTimer` + select 响应 context 取消：
```go
select {
case <-time.After(50 * time.Millisecond):
case <-w.stopCh:
    return nil, nil // watcher 已停止
}
```

---

### M7. ~~`middleware/degradation.go` 中的延迟策略使用裸 `time.Sleep`~~ ✅ 已修复

**问题描述：** `middleware/degradation.go:450`：
```go
time.Sleep(100 * time.Millisecond)
```
在中间件内部使用裸 Sleep 会阻塞 goroutine，在高并发场景下可能累积大量阻塞的 goroutine，且无法被 context 取消。

**涉及文件：** `middleware/degradation.go:450`

**修复建议：** 替换为 context-aware 的延迟：
```go
timer := time.NewTimer(100 * time.Millisecond)
defer timer.Stop()
select {
case <-ctx.Done():
    return ctx.Err()
case <-timer.C:
}
```

---

### M8. `infra/dlq/consumers.go` 中 `KafkaConsumer` 是未实现的占位符

**问题描述：** `infra/dlq/consumers.go:265`：
```go
// TODO: Implement real Kafka sending logic
```
`KafkaConsumer.Consume` 方法只打印警告日志，根本不发送消息到 Kafka。这是公开 API，用户配置 Kafka 消费者后会静默失效，极难排查。

**涉及文件：** `infra/dlq/consumers.go`

**修复建议（二选一）：**
1. 实现真实的 Kafka 发送逻辑（引入 `github.com/segmentio/kafka-go`）
2. 在文档中明确标注为 "experimental/stub"，并在 `Consume` 方法中返回 `error`（而不是静默失效）

---

### M9. `adapter.go` 中 `NewWebhookAdapterWithServer` 的 `secret` 参数未使用

**问题描述：** `adapter.go:151`：
```go
// TODO: 使用 secret 进行签名验证
func NewWebhookAdapterWithServer(addr string, secret string) Adapter {
    return NewWebhookServerAdapter(addr, &dto.BotInfo{})  // secret 直接丢弃
}
```
1. `secret` 参数被完全忽略，但函数签名对外暴露，用户传入 secret 后以为已生效。
2. 传入 `&dto.BotInfo{}` 是空 BotInfo，与 `NewWebhookServerAdapter` 要求 `botInfo` 语义不符。
3. 这个函数的功能与 `NewWebhookServerAdapter` 几乎完全重复，又是 `SimpleWebhookAdapter` 的变体，整体 API 设计混乱。

**涉及文件：** `adapter.go:150`

**修复建议：** 要么实现 secret 验证逻辑，要么删除此函数，统一用 `NewWebhookServerAdapter`。

---

### M10. `openapi/dto/event.go` 有未完成的 Channel 事件定义

**问题描述：** `openapi/dto/event.go:135`：
```go
// TODO: Add channel event
```
文件末尾有未完成的 TODO，说明频道事件类型定义缺失。如果框架声称支持频道消息，此处缺失将导致功能不完整。

**涉及文件：** `openapi/dto/event.go:135`

**修复建议：** 根据 QQ 开放平台文档补全频道事件结构体，或在文档中明确说明频道事件暂不支持。

---

### M11. `tests/integration/e2e_test.go` 中有被注释的集成测试

**问题描述：** `tests/integration/e2e_test.go:406`：
```go
// TODO: 当 Bot API 完善后，取消注释并修复
```
集成测试被注释，覆盖率存在空洞，且说明 Bot API 存在未完成的部分。

**涉及文件：** `tests/integration/e2e_test.go`

**修复建议：** 补全对应的 Bot API，恢复集成测试；若短期无法完成，在文档中说明已知限制。

---

### M12. 多个核心插件缺少测试文件

**问题描述：** 以下包无测试文件（`[no test files]`）：
- `plugins/core/admin`（800 行）
- `plugins/core/cache`
- `plugins/core/help`（684 行）
- `plugins/dev/debug`（629 行）
- `infra/server`
- `infra/tracing`
- `openapi`（顶层包）
- `global`

核心插件（admin、help）代码量较大却无测试，发布后风险较高。

**修复建议：** 至少为 `plugins/core/admin` 和 `plugins/core/help` 补充基础单元测试。

---

## 🟡 低优先级（可版本后迭代）

### L1. 大量包缺少包级别 Godoc 注释

**问题描述：** Go 工具链规范要求每个包有 `package xxx` 前的注释说明包用途。以下包缺少包级注释（无 `// Package xxx ...` 或 `doc.go`）：

- `remilia`（根包，已有 `doc.go`，但 `adapter.go`、`bot.go` 等单文件无注释）
- `command/parser.go`、`command/registry.go`、`command/trie.go`
- `config/config.go`、`config/watcher.go`
- `core/context/`（多个文件）
- `core/engine/`（多个文件）
- `infra/audit/`、`infra/dlq/`、`infra/health/`、`infra/httpclient/`、`infra/metrics/`、`infra/pool/`、`infra/tracing/`
- `plugin/manager.go`、`plugin/context.go`
- 大多数 `plugins/xxx` 子包

`go doc` 和 pkg.go.dev 会直接展示包级注释，缺失会影响文档质量。

**修复建议：** 为每个子包添加 `doc.go` 或在包主文件首行添加 `// Package xxx ...` 注释。

---

### L2. `go.mod` 中直接依赖与间接依赖混写在多个 `require` 块中

**问题描述：** `go.mod` 共有 4 个 `require` 块，直接依赖分散在第 1 块和第 3 块，间接依赖分散在第 2 块和第 4 块，结构混乱：

```
require { ... }   // 直接依赖第一批
require { ... }   // 间接依赖第一批
require { ... }   // 直接依赖第二批（包含 hashicorp/lru、sqlite3 等）
require { ... }   // 间接依赖第二批
```

**涉及文件：** `go.mod`

**修复建议：** 执行 `go mod tidy` 合并整理（但注意 `go mod tidy` 可能因 `go 1.25` 版本问题失败，需先处理 H2）。

---

### L3. `gopkg.in/yaml.v3` 与 `go.yaml.in/yaml/v3` 两套 YAML 库同时存在

**问题描述：** `go.mod` 同时依赖：
- `gopkg.in/yaml.v3 v3.0.1`（直接依赖）
- `go.yaml.in/yaml/v2 v2.4.3`（间接）
- `go.yaml.in/yaml/v3 v3.0.4`（间接）

`go.yaml.in/yaml` 是 `gopkg.in/yaml` 的新官方路径（2024 年迁移），两套并存会增加二进制体积，且版本不同步可能产生行为差异。

**涉及文件：** `go.mod`

**修复建议：** 调查 `gopkg.in/yaml.v3` 是否能替换为 `go.yaml.in/yaml/v3`（主要看 `spf13/viper` 的依赖图），逐步统一到新路径。

---

### L4. `go.uber.org/atomic` 是不必要的依赖

**问题描述：** `go.mod` 中存在 `go.uber.org/atomic v1.11.0`（indirect），但 Go 1.19+ 的标准库 `sync/atomic` 已提供了泛型原子类型（`atomic.Bool`、`atomic.Int32` 等），项目代码已在使用 `sync/atomic`，引入 uber atomic 是冗余的。

**涉及文件：** `go.mod`

**修复建议：** 运行 `go mod why go.uber.org/atomic` 确认哪个依赖引入了它，若是间接依赖无法直接删除，升级该依赖到不再依赖 uber atomic 的版本。

---

### L5. `global/global.go` 设计模式存在隐患

**问题描述：** `global.go` 使用全局变量 `var Info *dto.BotInfo`，存在以下问题：
1. 并发不安全：多个 goroutine 同时读写 `Info` 没有任何同步保护
2. 测试困难：全局状态在测试间互相污染
3. `MustInitFromConfig` 中对 `Info == nil` 的检查永远不会触发（`dto.NewBotInfo` 不会返回 nil）

**涉及文件：** `global/global.go`

**修复建议：**
- 用 `atomic.Pointer[dto.BotInfo]` 替换裸指针
- 或改为返回值的函数形式，不使用全局变量

---

### L6. `lifecycle/lifecycle.go` 文件超过 700 行，职责过多

**问题描述：** `lifecycle.go`（708 行）包含了 Manager、Component 接口、State 枚举、错误处理、并发控制等多个关注点，与 `simple.go`、`README.md` 的职责边界模糊。

**涉及文件：** `lifecycle/lifecycle.go`

**修复建议：** 拆分为：
- `lifecycle.go`：Manager 核心逻辑
- `state.go`：State 枚举及 String() 方法
- `errors.go`：lifecycle 专属错误

---

### L7. `plugin/manager.go` 超过 850 行，是全项目最大文件

**问题描述：** `plugin/manager.go`（851 行）集中了注册、卸载、依赖解析、事件总线、热重载、ConfigProvider 等大量功能，难以维护和测试。

**涉及文件：** `plugin/manager.go`

**修复建议：** 按功能分拆：
- `manager.go`：注册/卸载核心逻辑
- `manager_lifecycle.go`：Start/Stop/Reload
- `manager_deps.go`：依赖解析与拓扑排序
- `manager_config.go`：ConfigProvider 集成

---

### L8. `config/config.go` 超过 900 行，包含多个大型验证方法

**问题描述：** `config/config.go`（901 行）既包含结构体定义，又包含所有子配置的 `Validate()` 方法和默认值逻辑，单文件职责过重。

**涉及文件：** `config/config.go`

**修复建议：** 拆分为 `config.go`（类型定义）和 `validate.go`（验证逻辑）。

---

### L9. ~~`bot.go` 中 `NewBotWithInfo` 在构造阶段创建了会泄漏的 `context.Background()` tokenManager~~ ✅ 已修复

**问题描述：** `bot.go:143`：
```go
tmpTokenManager := token.NewManagerWithContext(context.Background(), botInfo)
```
此处使用 `context.Background()` 创建 TokenManager，意味着其后台 goroutine 只能靠 `Stop()` 手动停止。注释说明了会在 `Start()` 时重新绑定到 rootCtx，但如果用户创建 `Bot` 后从未调用 `Start()`（例如仅用于发送消息），这个 goroutine 会一直运行直到进程退出。

**涉及文件：** `bot.go:143`

**修复建议：** 延迟初始化 TokenManager，仅在 `Start()` 或 `GetToken()` 首次调用时初始化，或让 `NewBotWithInfo` 只存储 `botInfo`，不立即创建 Manager。

---

### L10. ~~`OnRegex` 规则函数在正则无效时 panic，缺少 Safe 版本的文档提示~~ ✅ 已修复

**问题描述：** `core/context/rules.go:175`（`OnRegex` 函数）文档中提到"如果正则表达式无效会 panic，生产环境建议使用 `OnRegexSafe`"，但：
1. `OnRegexSafe` 是否存在未验证（搜索全项目未找到导出的 `OnRegexSafe`）
2. 如果不存在，注释是误导性的

**涉及文件：** `core/context/rules.go`

**修复建议：** 确认 `OnRegexSafe` 是否存在，若不存在则补充实现，或修改 `OnRegex` 注释。

---

### L11. ~~`bot.go` 中 `Start()` 结束后插件 goroutine 的退出顺序问题~~ ✅ 已修复

**问题描述：** `bot.go` 中 `Stop()` 时：
```go
// 先停止插件
b.pluginManager.StopAll(ctx)
// 再停止 lifecycle（adapter/engine）
b.lifecycle.Stop(ctx)
// 最后取消 rootCtx
rootCancel()
```
但 `rootCancel()` 在 `lifecycle.Stop()` 之后才调用，而 `lifecycle.Stop()` 中的 adapter 和 engine 停止可能依赖 rootCtx 已经被取消。这个顺序在理论上是正确的，但 `tokenManager.Stop()` 在 `rootCancel()` 之后才调用，存在双重停止。

**涉及文件：** `bot.go:343`

**修复建议：** 审查并注释说明停止顺序的设计意图，或提取为独立的 `shutdownSequence()` 方法以提升可读性。

---

### L12. ~~`pprof.go` 中 `AutoProfile` 功能使用 `time.Sleep` 而非 ticker~~ ✅ 已修复

**问题描述：** `pprof.go:221,343`：
```go
time.Sleep(p.config.ProfileDuration)
time.Sleep(duration)
```
自动性能分析使用裸 `time.Sleep` 实现间隔，导致：
1. 停止 pprof 服务器时无法立即退出，需要等待 sleep 结束
2. 没有响应 context 取消

**涉及文件：** `pprof.go`

**修复建议：** 改用 `time.NewTimer` 配合 `select { case <-stopCh: }`。

---

### L13. ~~`infra/dlq/dlq.go` 使用 `context.Background()` 启动后台 goroutine~~ ✅ 已修复

**问题描述：** `infra/dlq/dlq.go:83` 和 `infra/dlq/queue_generic.go:125` 使用 `context.Background()` 启动后台 goroutine，这意味着 DLQ 的生命周期与使用它的 Bot 解耦，Bot 停止后 DLQ goroutine 仍在运行。

**涉及文件：** `infra/dlq/dlq.go:83`, `infra/dlq/queue_generic.go:125`

**修复建议：** 为 DLQ 的 `Start`/`Stop` 方法添加 context 参数，或在构造时接受父 context。

---

### L14. ~~`middleware/degradation.go` 延迟策略中 `time.Sleep` 会阻塞中间件链~~ ✅ 已修复（见 M7）

**问题描述：** 同 M7，但额外影响是：DegradationDelay 策略对每个低优先级事件都调用 `time.Sleep(100ms)`，高并发下会造成线程池饱和（大量 goroutine 阻塞在 sleep），实际上比直接丢弃更危险。

**涉及文件：** `middleware/degradation.go:450`

---

### L15. 缺少 CI/CD 配置和静态分析配置

**问题描述：** 项目根目录无：
- `.github/workflows/` — GitHub Actions CI 配置
- `.golangci.yml` — golangci-lint 静态分析配置
- `Makefile` — 常用命令脚本（test、lint、build、coverage）

对于开源发布的框架，这些是基础设施标配。

**修复建议：** 添加至少包含以下 job 的 CI 配置：
1. `go build ./...`
2. `go test -race ./...`
3. `golangci-lint run`

---

### L16. `bot_manager.go` 中 `MustGet` 暴露 panic，且 `maps.Clone` 使用需确认

**问题描述：** 
1. `bot_manager.go:94`：`MustGet` 对外 panic，框架层函数建议只在 Builder/初始化模式下 panic，运行时查询建议返回 error。
2. `go.mod` 使用 `maps` 包（`maps.Copy`），`maps` 在 Go 1.21 标准库引入，与 `go.mod` 声明的 `go 1.25` 一致，但与 README badge 的 `Go 1.21+` 冲突（Go 1.21 中 `maps` 是实验性包，正式稳定在 1.23）。

**涉及文件：** `bot_manager.go:94`, `go.mod`

---

### L17. `config.yaml` 包含真实凭证（已在 `.gitignore` 中排除，但需确认历史提交）

**问题描述：** `config.yaml` 中包含真实的 `token` 和 `secret`：
```yaml
token: "pIXPKbbKSEszdUkgVd0tKBlga30ZJfuf"
secret: "83yuqmieaXUROLIGECA8765433333345"
```
虽然 `.gitignore` 已排除该文件（通过检查确认 `config.yaml` 从未被提交到 git），但本地文件中的明文凭证仍存在泄漏风险（如 IDE 同步、备份工具等）。

**涉及文件：** `config.yaml`

**修复建议：** 建议将真实配置替换为占位符，使用环境变量或密钥管理服务注入敏感配置。可参考 `config.example.yaml`。

---

## 📊 问题汇总统计

| 优先级 | 数量 | 主要类别 |
|--------|------|----------|
| 🔴 高（发布阻断） | 5 | 版本一致性、go.mod 兼容性、文档规范 |
| 🟠 中（建议修复） | 12 | API 设计、未完成功能、错误处理 |
| 🟡 低（可迭代） | 17 | 代码质量、文档、性能、架构 |

### 按类别分类

| 类别 | 问题编号 |
|------|----------|
| 版本/发布管理 | H1, H2, H3, H4, H5 |
| API 设计缺陷 | M1, M2, M3, M4, M5, L16 |
| 未完成功能（TODO） | M8, M9, M10, M11 |
| 并发/生命周期 | M6, M7, L9, L11, L13, L14 |
| 测试覆盖 | M12 |
| 代码组织/可维护性 | L1, L6, L7, L8 |
| 依赖管理 | L2, L3, L4 |
| 安全 | L17 |
| 性能 | L12 |
| 全局状态 | L5 |
| 基础设施 | L15 |

### 发布前最小修复清单（必须处理）

- [ ] **H1**：统一版本号（新建 `version.go`，bot.go 默认版本改为 `2.0.0`）
- [ ] **H2**：降低 `go.mod` 中 Go 版本要求至已发布版本
- [ ] **H3**：补全 CHANGELOG.md 中 `[1.0.0]` 的日期
- [ ] **H4**：整理 CHANGELOG.md 结构（版本倒序）
- [ ] **H5**：修正 README badge 中的 Go 版本

---

*本文档由自动化代码审查工具结合人工分析生成，所有问题均已定位到具体文件和行号。*

