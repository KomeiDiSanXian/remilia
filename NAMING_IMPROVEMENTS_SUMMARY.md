# 命名改进建议速查表

## 快速概览

本文档是 `NAMING_REVIEW.md` 的精简版，列出最关键的命名改进建议，便于快速参考和实施。

---

## 🔴 高优先级 (建议立即修改)

### 1. Import 别名统一

**影响范围**: 全项目  
**问题**: `import context2` 不够优雅且语义不清

| 文件模式 | 当前代码 | 建议修改 |
|---------|---------|---------|
| 所有使用处 | `import context2 "github.com/.../core/context"` | `import eventctx "github.com/.../core/context"` |

**修改文件**: 
- `bot.go`
- `middleware/*.go`
- `core/engine/*.go`
- 其他所有相关文件

---

### 2. Engine 核心字段重命名

**文件**: `core/engine/engine.go`

```go
// 当前 ❌
type Engine struct {
    s engineServices  // 单字母，难以理解
    // ...
}

// 建议 ✅
type Engine struct {
    services engineServices  // 清晰明了
    // ...
}
```

**影响**: 需要修改所有访问 `e.s` 的代码为 `e.services`

---

### 3. 根包构造函数重命名

**文件**: `factory.go`

```go
// 当前 ❌
func New(info *dto.BotInfo, opts ...Option) *Bot

// 建议 ✅ (选择其一)
func NewWithDefaults(info *dto.BotInfo, opts ...Option) *Bot
// 或
func NewBot(info *dto.BotInfo, opts ...Option) *Bot
```

**原因**: 根包的 `New()` 过于通用，容易与其他包混淆

---

### 4. OpenAPI Service 类型重命名

**文件**: `openapi/openapi.go`

```go
// 当前 ❌
type Service struct {
    manager *token.Manager
}

// 建议 ✅ (选择其一)
type APIService struct {
    manager *token.Manager
}
// 或
type Client struct {
    manager *token.Manager
}
```

---

### 5. Webhook Connection 类型重命名

**文件**: `openapi/protocol/webhook/webhook.go`

```go
// 当前 ❌
type Conn struct {
    // ...
}

// 建议 ✅
type Connection struct {
    // ...
}
```

---

## 🟡 中优先级 (下个版本修改)

### 6. WebHook 大小写修正

**文件**: `adapter.go`

```go
// 当前 ❌
type WebHook interface {
    EventStream() <-chan *dto.Payload
}

// 建议 ✅
type Webhook interface {
    EventStream() <-chan *dto.Payload
}
```

**原因**: Webhook 应该作为一个词，不是 Web Hook

---

### 7. webhookAdapter 字段重命名

**文件**: `adapter.go`

```go
// 当前 ❌
type webhookAdapter struct {
    wh     WebHook  // 缩写不清晰
    ctx    context.Context
    cancel context.CancelFunc
}

// 建议 ✅
type webhookAdapter struct {
    webhook Webhook
    ctx     context.Context
    cancel  context.CancelFunc
}
```

---

### 8. 统一生命周期方法命名

**问题**: 当前混用 `Stop()` 和 `Shutdown()`

**建议**: 统一使用 `Shutdown()` (语义更明确，表示优雅关闭)

```go
// 统一所有接口
type Component interface {
    Start(ctx context.Context) error
    Shutdown(ctx context.Context) error  // ✅ 统一使用
}

type Adapter interface {
    Start(ctx context.Context, handler func(*dto.Payload)) error
    Shutdown(ctx context.Context) error  // ✅ 统一使用
}
```

---

### 9. MatcherInterface 重命名

**文件**: `core/context/context.go`

```go
// 当前 ❌
type MatcherInterface interface {
    GetSource() string
}

// 建议 ✅ (选择其一)
type SourceProvider interface {  // 强调功能
    GetSource() string
}
// 或
type Matcher interface {  // 如果不会循环依赖
    GetSource() string
}
```

---

### 10. 扩展常用缩写

**影响文件**: 多个

| 当前 | 建议 | 位置 |
|-----|------|------|
| `cfg` | `config` | 函数参数 |
| `mu` | `lock` 或 `stateMutex` | 关键结构体字段 |
| `wh` | `webhook` | 变量名 |
| `msg` | `message` | 变量名 |

---

## 🟢 低优先级 (持续改进)

### 11. helper 包重构

**当前**: 单一 `helper` 包包含多种不相关功能

**建议**: 按功能拆分

```
helper/  ❌
  helper.go

// 建议 ✅
encoding/
  convert.go      // BytesToString, StringToBytes
hash/
  hash.go         // FNVHash  
event/
  parser.go       // ParseEvent
url/
  formatter.go    // HideURL
```

---

### 12. 配置参数命名统一

在函数参数中，统一使用完整单词：

```go
// 当前 ❌
func Retry(cfg RetryConfig) Middleware

// 建议 ✅
func Retry(config RetryConfig) Middleware
```

---

## 📋 实施建议

### 步骤 1: 准备工作

1. 创建新分支: `git checkout -b refactor/naming-improvements`
2. 确保所有测试通过: `go test ./...`
3. 提交当前改动

### 步骤 2: 按优先级实施

#### 高优先级改动 (一次性完成)

```bash
# 1. 全局替换 context2 为 eventctx
find . -name "*.go" -exec sed -i 's/context2/eventctx/g' {} \;

# 2. 运行测试确保没有问题
go test ./...

# 3. 手动修改其他高优先级项目 (2-5)
# 4. 提交
git add .
git commit -m "refactor: high priority naming improvements"
```

#### 中优先级改动 (分批完成)

每次选择 1-2 项进行修改，确保：
- 修改后所有测试通过
- 更新相关文档
- 独立提交

#### 低优先级改动 (新代码遵循)

- 在代码审查时逐步改进
- 新增代码严格遵循新规范
- 不强制修改旧代码

---

## 🔍 验证检查清单

完成每项修改后，检查：

- [ ] 所有单元测试通过
- [ ] 所有基准测试通过
- [ ] 代码可以成功编译
- [ ] 没有引入新的 linter 警告
- [ ] 更新了相关文档
- [ ] 更新了示例代码

---

## 📊 影响分析

| 改动项 | 影响文件数 | 破坏性 | 难度 | 预估时间 |
|-------|-----------|--------|------|---------|
| 1. context2→eventctx | ~30+ | 无 (内部) | 低 | 30分钟 |
| 2. Engine.s→services | ~10 | 无 (内部) | 低 | 15分钟 |
| 3. factory.New重命名 | 1 | **有** (API) | 中 | 10分钟 |
| 4. Service重命名 | ~5 | 无 (内部) | 低 | 15分钟 |
| 5. Conn重命名 | ~5 | **有** (API) | 中 | 20分钟 |
| 6. WebHook修正 | ~3 | **有** (API) | 低 | 10分钟 |
| 7. webhookAdapter.wh | 1 | 无 (内部) | 低 | 5分钟 |
| 8. 生命周期统一 | ~10 | 可能 | 中 | 30分钟 |

**总预估**: 约 2-3 小时完成高优先级改动

---

## ⚠️ 破坏性变更说明

以下改动会影响公共 API，需要：
1. 更新版本号 (考虑 semver)
2. 提供迁移指南
3. 在 CHANGELOG 中说明

### 破坏性改动清单

1. ❌ `factory.New()` → `factory.NewWithDefaults()`
   - 影响: 用户需要更新导入和调用
   - 迁移: 全局搜索替换

2. ❌ `adapter.WebHook` → `adapter.Webhook`
   - 影响: 实现了此接口的用户代码
   - 迁移: 类型名称修改

3. ❌ `webhook.Conn` → `webhook.Connection`
   - 影响: 直接使用此类型的用户
   - 迁移: 类型名称修改

### 平滑迁移方案

为减少破坏性，可以提供别名：

```go
// 临时兼容性别名 (deprecated)
type Conn = Connection

// 或提供包装函数
// Deprecated: Use NewWithDefaults instead
func New(info *dto.BotInfo, opts ...Option) *Bot {
    return NewWithDefaults(info, opts...)
}
```

---

## 📚 相关资源

- 完整审查报告: `NAMING_REVIEW.md`
- Go 命名规范: https://golang.org/doc/effective_go#names
- 代码审查指南: https://github.com/golang/go/wiki/CodeReviewComments

---

**更新日期**: 2026-01-23  
**维护者**: 项目团队
