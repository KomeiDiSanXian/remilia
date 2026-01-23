# Remilia 项目命名规范审查报告

## 概述

本文档对 Remilia 项目中的模块、组件、类型、变量和函数的命名进行全面审查，指出不合适的命名并提供改进建议。

## 审查标准

- **清晰性**: 名称应清楚表达其用途和意图
- **一致性**: 同类事物应遵循统一的命名模式
- **Go 语言规范**: 遵循 Go 社区的最佳实践
- **可读性**: 避免使用缩写、拼音或模糊的名称
- **语义准确**: 名称应准确反映其功能和职责

---

## 1. 包级命名问题

### 1.1 包名使用

| 当前包名 | 问题 | 建议 | 优先级 |
|---------|------|------|--------|
| `context2` (import别名) | 与标准库 context 冲突，使用数字后缀不优雅 | 使用 `remiliacontext` 或 `ctx` 作为别名，或重构包名为 `eventctx` | 高 |
| `infrapool` (import别名) | 缩写不清晰 | 使用 `resourcepool` 或直接使用 `pool` | 中 |
| `dlq` | 缩写晦涩 (Dead Letter Queue) | 改为 `deadletter` 或保持但加强文档 | 中 |

**改进建议**:
```go
// 当前
import context2 "github.com/KomeiDiSanXian/remilia/core/context"

// 建议 1: 使用更清晰的别名
import eventctx "github.com/KomeiDiSanXian/remilia/core/context"

// 建议 2: 或者重命名包本身
// 将 core/context 重命名为 core/eventcontext
```

---

## 2. 核心类型命名

### 2.1 结构体命名

#### ✅ 命名良好的类型

| 类型名 | 位置 | 优点 |
|--------|------|------|
| `Engine` | core/engine | 简洁明了，符合其职责 |
| `Adapter` | adapter.go | 清晰表达适配器模式 |
| `HealthChecker` | health.go | 意图明确 |
| `LifecycleManager` | lifecycle | 职责清晰 |
| `Plugin` | plugin | 简单直观 |

#### ⚠️ 需要改进的类型

| 当前名称 | 位置 | 问题 | 建议名称 | 优先级 |
|---------|------|------|----------|--------|
| `Conn` | openapi/protocol/webhook | 名称过于通用，不能表达是 webhook 连接 | `WebhookConn` 或 `Connection` | 高 |
| `Service` | openapi/openapi.go | 名称过于宽泛，不能表达是 API 服务 | `APIService` 或 `OpenAPIClient` | 高 |
| `Manager` (多处) | plugin/manager.go, lifecycle/lifecycle.go | 过于通用，多处使用导致歧义 | 保持当前名，但确保包名提供上下文 | 低 |
| `state` | core/context/context.go | 小写且过于通用，难以理解用途 | `extensionState` 或 `contextState` | 中 |
| `retryAttempt` | core/context/context.go | 可以更具体 | `retryMetadata` 或 `retryContext` | 低 |
| `middlewareTrace` | core/context/context.go | 语义不够明确 | `middlewareExecutionTrace` | 低 |
| `parsedCommand` | core/context/context.go | 可以更简洁 | `commandData` 或保持 | 低 |

### 2.2 接口命名

#### ✅ 命名良好的接口

| 接口名 | 位置 | 优点 |
|--------|------|------|
| `Adapter` | adapter.go | 清晰的适配器语义 |
| `Plugin` | plugin/plugin.go | 简洁明了 |
| `Component` | lifecycle/lifecycle.go | 通用且准确 |
| `Checker` | infra/health/health.go | 符合 Go 的 -er 命名习惯 |

#### ⚠️ 需要改进的接口

| 当前名称 | 位置 | 问题 | 建议名称 | 优先级 |
|---------|------|------|----------|--------|
| `WebHook` | adapter.go | 大小写不规范 (应该是 Webhook) | `Webhook` | 中 |
| `MatcherInterface` | core/context/context.go | 冗余的 Interface 后缀 | `Matcher` 或 `MatcherContract` | 中 |
| `DeadLetterConsumer` | core/engine/config.go | 可以更简洁 | `Consumer` (在 dlq 包内) | 低 |
| `engineComponent` | core/engine/component.go | 小写且不导出，但命名可以更具体 | `runtimeComponent` | 低 |

---

## 3. 变量和字段命名

### 3.1 结构体字段

#### ⚠️ 需要改进的字段名

| 类型 | 字段名 | 问题 | 建议 | 优先级 |
|------|--------|------|------|--------|
| `Bot` | `mu` | 缩写不够清晰 | `stateMutex` 或 `lock` | 低 |
| `Engine` | `s` | 单字母命名，难以理解 | `services` | 高 |
| `webhookAdapter` | `wh` | 缩写不清晰 | `webhook` | 中 |
| `Context` | `api` | 可以更具体 | `apiClient` 或 `openAPI` | 低 |
| `Context` | `ext` | 缩写 | `extensions` | 中 |
| `Context` | `extOnce` | 语义不够明确 | `extensionsInitOnce` | 低 |
| `DedupFilter` | `mu` | 缩写 | `cacheMutex` 或 `lock` | 低 |
| `Manager` (plugin) | `mu` | 缩写 | `pluginsMutex` | 低 |

**改进示例**:
```go
// 当前
type Engine struct {
    s engineServices  // ❌ 单字母，难以理解
    // ...
}

// 建议
type Engine struct {
    services engineServices  // ✅ 清晰明了
    // ...
}
```

### 3.2 局部变量

#### ⚠️ 常见问题

| 模式 | 问题 | 建议 | 优先级 |
|------|------|------|--------|
| `wh` | 过度缩写 | `webhook` | 中 |
| `ctx` | 在某些情况下与标准库冲突 | `eventCtx` 或 `handlerCtx` | 低 |
| `e` (Engine) | 单字母，但在小范围内可接受 | 保持或使用 `eng`/`engine` | 低 |
| `m` (Matcher) | 单字母 | `matcher` 或 `match` | 低 |
| `h` (Handler) | 单字母 | `handler` | 低 |
| `cfg` | 缩写 | `config` | 低 |

**注意**: Go 语言中，在很小的作用域内（如循环、简短函数），单字母变量是可接受的。但在较大作用域中应避免。

---

## 4. 函数和方法命名

### 4.1 构造函数

#### ✅ 命名良好

| 函数名 | 位置 | 优点 |
|--------|------|------|
| `NewBot()` | bot.go | 标准的 Go 构造函数 |
| `NewEngine()` | core/engine/engine.go | 清晰明了 |
| `NewHealthChecker()` | health.go | 准确表达用途 |
| `NewManager()` | 多处 | 标准构造函数 |

#### ⚠️ 需要改进

| 当前名称 | 位置 | 问题 | 建议名称 | 优先级 |
|---------|------|------|----------|--------|
| `New()` | factory.go | 过于通用，在根包下容易混淆 | `NewBotWithDefaults()` 或 `NewDefaultBot()` | 高 |
| `New()` | openapi/protocol/webhook | 与其他 New 函数容易混淆 | `NewWebhook()` | 中 |
| `New()` | openapi/openapi.go | 在子包中可以接受 | `NewService()` 或保持 | 低 |

**改进建议**:
```go
// 当前 (factory.go)
func New(info *dto.BotInfo, opts ...Option) *Bot  // ❌

// 建议
func NewWithDefaults(info *dto.BotInfo, opts ...Option) *Bot  // ✅
// 或
func NewDefaultBot(info *dto.BotInfo, opts ...Option) *Bot  // ✅
```

### 4.2 方法命名

#### ⚠️ 需要改进

| 方法 | 类型 | 问题 | 建议 | 优先级 |
|------|------|------|------|--------|
| `EventStream()` | WebHook | 返回 channel，应该遵循 Go 习惯 | `Events()` 或 `Stream()` | 低 |
| `Start()` / `Shutdown()` | 多处不一致 | 有的用 Stop，有的用 Shutdown | 统一使用 `Stop()` 或都用 `Shutdown()` | 中 |
| `IsDuplicate()` | DedupFilter | Is 开头通常返回 bool，但这里返回 (bool, error) | `CheckDuplicate()` 或 `MarkAsSeen()` | 中 |

---

## 5. 常量命名

### 5.1 命名模式

#### ✅ 命名良好

| 常量 | 位置 | 优点 |
|------|------|------|
| `DefaultTempMatcherCleanerInterval` | core/engine/config.go | 清晰的默认值语义 |
| `StateCreated` / `StateRunning` | lifecycle | 枚举值有清晰的前缀 |
| `Healthy` / `Unhealthy` | infra/health | 简洁明了 |

#### ⚠️ 需要改进

| 常量名 | 位置 | 问题 | 建议 | 优先级 |
|--------|------|------|------|--------|
| `DropOldest` / `DropNewest` | infra/dlq/dlq.go | 缺少类型前缀 | `DropPolicyOldest` / `DropPolicyNewest` | 中 |
| `ArgTypeString` / `ArgTypeInt` | command/enhanced_system.go | 可以简化为 `TypeString` (在 Arg 前缀已明确时) | 保持或简化 | 低 |

---

## 6. 包结构和组织

### 6.1 包命名

#### ✅ 命名良好的包

- `bot.go` - 清晰的顶层 API
- `core/engine` - 核心引擎逻辑
- `core/context` - 上下文管理
- `middleware` - 中间件
- `plugin` - 插件系统
- `lifecycle` - 生命周期管理
- `command` - 命令解析

#### ⚠️ 需要考虑的包

| 包名 | 问题 | 建议 | 优先级 |
|------|------|------|--------|
| `infra` | 基础设施的缩写 | 考虑 `infrastructure` 或拆分为独立包 | 低 |
| `httpreq` | 缩写且用途不明确 | `httpclient` 或 `request` | 中 |
| `stats` | 功能单一，可考虑合并 | 合并到 `metrics` 或保持独立 | 低 |
| `helper` | 过于通用，容易成为"垃圾桶" | 按功能拆分为 `encoding`, `hash`, `parser` 等 | 中 |
| `errors` | 与标准库同名 | `errutil` 或 `errorwrap` | 中 |

---

## 7. 具体改进建议

### 7.1 高优先级改进

#### 1. Import 别名冲突 (context2)

**问题**: 当前代码大量使用 `context2` 作为 import 别名，不够优雅。

**解决方案 A - 修改 import 别名** (推荐):
```go
// 在所有文件中统一修改
import eventctx "github.com/KomeiDiSanXian/remilia/core/context"

// 使用
func Handler(ctx *eventctx.Context) error {
    // ...
}
```

**解决方案 B - 重命名包**:
```bash
# 将 core/context 重命名为 core/eventcontext
mv core/context core/eventcontext
```

#### 2. Engine.s 字段重命名

**当前**:
```go
type Engine struct {
    s engineServices  // ❌
}
```

**建议**:
```go
type Engine struct {
    services engineServices  // ✅
}
```

#### 3. 根包 New() 函数重命名

**当前**:
```go
func New(info *dto.BotInfo, opts ...Option) *Bot
```

**建议**:
```go
func NewWithDefaults(info *dto.BotInfo, opts ...Option) *Bot
// 或
func NewBot(info *dto.BotInfo, opts ...Option) *Bot
```

#### 4. openapi.Service 重命名

**当前**:
```go
type Service struct {
    // ...
}
```

**建议**:
```go
type APIService struct {
    // ...
}
// 或
type Client struct {
    // ...
}
```

#### 5. webhook.Conn 重命名

**当前**:
```go
type Conn struct {
    // ...
}
```

**建议**:
```go
type Connection struct {
    // ...
}
// 或
type WebhookConn struct {
    // ...
}
```

### 7.2 中优先级改进

#### 1. 统一生命周期方法

当前混用 `Stop()` 和 `Shutdown()`，建议统一：

```go
// 统一使用 Shutdown，因为它语义更明确（优雅关闭）
type Component interface {
    Start(ctx context.Context) error
    Shutdown(ctx context.Context) error  // ✅ 统一
}
```

#### 2. WebHook 大小写修正

```go
// 当前
type WebHook interface {  // ❌

// 建议
type Webhook interface {  // ✅
}
```

#### 3. 缩写字段名扩展

```go
// 当前
type webhookAdapter struct {
    wh     WebHook  // ❌
}

// 建议
type webhookAdapter struct {
    webhook Webhook  // ✅
}
```

#### 4. helper 包拆分

```go
// 当前结构
helper/
  helper.go  // 包含多种不相关的工具函数

// 建议结构
encoding/
  convert.go      // BytesToString, StringToBytes
hash/
  hash.go         // FNVHash
event/
  parser.go       // ParseEvent
url/
  formatter.go    // HideURL
```

### 7.3 低优先级改进

#### 1. 简化互斥锁字段名

```go
// 虽然 mu 是 Go 社区常见缩写，但在关键结构中可以更明确
type Bot struct {
    stateMutex sync.RWMutex  // 明确锁的用途
    // 或
    mu sync.RWMutex  // 保持简洁 (可接受)
}
```

#### 2. 扩展配置参数命名

```go
// 当前
func Retry(cfg RetryConfig) context2.Middleware

// 建议
func Retry(config RetryConfig) context2.Middleware
```

---

## 8. 命名规范最佳实践总结

### 8.1 Go 语言命名黄金法则

1. **包名**: 小写、简短、单数、无下划线
   - ✅ `engine`, `plugin`, `middleware`
   - ❌ `enginePackage`, `plugins`, `middle_ware`

2. **导出标识符**: 首字母大写
   - ✅ `Engine`, `NewBot`, `Start`
   - ❌ `engine`, `newBot`, `start` (如需导出)

3. **接口命名**: 
   - 单方法接口用 -er 后缀: `Reader`, `Writer`, `Checker`
   - 多方法接口用描述性名称: `Plugin`, `Component`
   - ❌ 避免 Interface 后缀

4. **避免口吃** (stuttering):
   - ✅ `engine.New()` 而不是 `engine.NewEngine()`
   - ✅ `user.Load()` 而不是 `user.LoadUser()`

5. **缩写规则**:
   - 常见缩写全大写: `HTTP`, `API`, `URL`, `ID`
   - 在标识符中间时: `HTTPServer`, `APIKey`, `UserID`
   - ❌ 避免非标准缩写: `cfg`, `msg`, `wh`

6. **布尔值命名**:
   - ✅ `isRunning`, `hasError`, `enabled`
   - ❌ `running`, `error`, `enable` (容易混淆)

### 8.2 项目特定建议

1. **Context 使用**: 在导入 remilia/core/context 时，统一使用 `eventctx` 别名
2. **Manager 模式**: 保持各包中的 Manager 命名，依赖包名提供上下文
3. **配置结构**: 统一使用 `Config` 后缀，如 `RetryConfig`, `BotConfig`
4. **选项模式**: 统一使用 `With` 前缀，如 `WithName`, `WithAdapter`

---

## 9. 迁移计划

### 阶段 1: 高优先级修复 (建议立即进行)

1. ✅ 修改所有 `context2` import 为 `eventctx`
2. ✅ 重命名 `Engine.s` → `Engine.services`
3. ✅ 重命名 `factory.New()` → `factory.NewWithDefaults()`
4. ✅ 重命名 `openapi.Service` → `openapi.APIService`
5. ✅ 重命名 `webhook.Conn` → `webhook.Connection`

### 阶段 2: 中优先级改进 (下一版本)

1. ⚠️ 统一生命周期方法命名
2. ⚠️ 修正 `WebHook` → `Webhook`
3. ⚠️ 扩展关键结构体的缩写字段名
4. ⚠️ 重构 `helper` 包

### 阶段 3: 低优先级优化 (持续改进)

1. 📝 代码审查时逐步改进变量命名
2. 📝 新增代码严格遵循命名规范
3. 📝 文档更新和最佳实践指南

---

## 10. 检查清单

在编写新代码或进行代码审查时，使用此清单：

- [ ] 类型名称是否清晰表达其职责？
- [ ] 是否避免了不必要的缩写？
- [ ] 接口名称是否遵循 -er 规则或描述性命名？
- [ ] 是否避免了"口吃"？
- [ ] 包名是否简洁且不与标准库冲突？
- [ ] 导出标识符是否首字母大写？
- [ ] 是否使用了一致的命名模式？
- [ ] 变量名是否与其作用域大小相匹配？
- [ ] 是否遵循了 Go 社区的最佳实践？

---

## 附录: 命名参考资源

- [Effective Go - Names](https://golang.org/doc/effective_go#names)
- [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)
- [Uber Go Style Guide](https://github.com/uber-go/guide/blob/master/style.md)
- [Standard Package Layout](https://github.com/golang-standards/project-layout)

---

**生成日期**: 2026-01-23  
**项目版本**: v0.9.0  
**审查人**: GitHub Copilot  
