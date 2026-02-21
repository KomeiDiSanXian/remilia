# Remilia 错误处理规范

## 概述

本文档定义了 Remilia 项目中统一的错误处理规范，基于 `errutil` 包实现。

---

## 核心原则

1. **不丢失错误链**：包装错误时必须使用 `%w` 或 `errutil.Wrapf`，确保 `errors.Is`/`errors.As` 能正常工作
2. **哨兵错误可比较**：框架层公共错误定义在 `errutil/errors.go`，包内私有错误用 `errutil.New` 定义
3. **消息清晰简洁**：小写开头，不以句号结尾，描述"做什么失败了"而非"出了什么错"

---

## API 速查

```go
import "github.com/KomeiDiSanXian/remilia/errutil"

// 1. 哨兵错误（包级别）
var ErrFoo = errutil.New("foo operation failed")

// 2. 包装外部错误
return errutil.Wrap(err, "failed to load config")
return errutil.Wrapf(err, "plugin %s load failed", name)
return errutil.WrapWithContext(err, "query failed", "table=users, id=123")

// 3. 动态错误（不可用 errors.Is 比较）
return errutil.Newf("invalid value: %d, expected >= 1", v)

// 4. 错误链查询
errutil.Is(err, errutil.ErrPluginNotFound)
errutil.As(err, &target)

// 5. 错误聚合
return errutil.Join(err1, err2)
```

---

## 依赖层错误规范

### infra 层（最底层）

- 可定义自己的包级别错误（`var ErrQueueClosed = errutil.New(...)`）
- 包装外层传入的错误时使用 `errutil.Wrapf`
- **不允许**依赖上层包（core/engine、plugin 等）

### core 层

- 引用 `errutil/errors.go` 中已定义的通用错误
- 包内专有错误用 `errutil.New` 定义，放在 `errors.go` 文件
- 使用 `errutil.Wrapf` 包装来自 infra 层的错误

### bot/plugin 层（最顶层）

- 引用 core 和 errutil 的错误
- 使用 `errutil.WrapWithContext` 添加请求级别的上下文

---

## 依赖层次图（接口隔离）

```
bot / plugin 层
      ↓ 依赖
core/engine 层
      ↓ 依赖
infra 层（health, dlq, metrics 等）
      ↓ 依赖
errutil / openapi/dto（基础层）
```

**关键约束**：
- `infra/health` 不依赖 `core/engine`（使用 `health.EngineStats` 接口替代）
- `infra/health` 不依赖 `infra/dlq`（使用 `health.DLQStats` 接口替代）
- `infra/*` 不相互交叉依赖

---

## 示例

### 插件加载错误

```go
func (pm *Manager) loadPlugin(desc *PluginDescriptor) error {
    if err := desc.Setup(ctx); err != nil {
        // 包装具体错误，保留上下文
        return errutil.Wrapf(err, "plugin %s setup failed", desc.Name)
    }
    return nil
}

// 调用方检查
err := pm.loadPlugin(desc)
if errutil.Is(err, errutil.ErrPluginLoadFailed) {
    // 处理
}
```

### 配置加载错误

```go
func Load(path string) (*Config, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, errutil.Wrapf(err, "failed to read config file")
    }
    // ...
}
```

### 健康检查适配器（接口隔离示例）

```go
// bot_health.go — 在 bot 层做适配，infra/health 不依赖 infra/dlq
type DLQHealthAdapter struct{ q *dlq.DeadLetterQueue }

func (a *DLQHealthAdapter) Stats() health.DLQStatsSnapshot {
    s := a.q.Stats()
    return health.DLQStatsSnapshot{...}
}

// 注册
check.AddChecker(health.NewDeadLetterQueueHealthChecker(
    &DLQHealthAdapter{q: myDLQ}, 1000, 0.1,
))
```

---

## 反模式（避免）

```go
// ❌ 断链
return fmt.Errorf("load failed: %v", err)   // %v 不保留链

// ❌ 字符串比较错误
if err.Error() == "plugin not found" { }     // 脆弱

// ❌ 在 infra 包中引用 core 包
import "github.com/KomeiDiSanXian/remilia/core/engine"  // infra 不应依赖 core

// ❌ 重复定义已有哨兵错误
var ErrPluginNotFound = errors.New("plugin not found")  // errutil 已有定义
```

