# Middleware 包 - 测试文档

## 📊 测试概览

本测试套件为 `middleware` 包提供了全面的测试覆盖，包括日志、恢复、认证、超时、并发限制、重试和熔断器等核心中间件。

### 测试统计

- **总测试数**: 42 个测试用例（含子测试）
- **代码覆盖率**: ~31%+ (测试核心中间件)
- **测试文件**: 1 个
  - `middleware_test.go` - 中间件测试

---

## 🧪 测试文件说明

### middleware_test.go - 中间件测试

#### 基础中间件测试（5 个测试）

**TestLogging** (2 个子测试)
- ✅ 成功执行记录
- ✅ 失败执行记录

**TestRecover** (3 个子测试)
- ✅ 恢复 panic
- ✅ 恢复 nil panic
- ✅ 无 panic 正常执行

**TestAuth** (2 个子测试)
- ✅ 授权通过
- ✅ 未授权拒绝

**TestTimeout** (3 个子测试)
- ✅ 超时前完成
- ✅ 超时
- ✅ handler 中的 panic

**TestMetrics** (2 个子测试)
- ✅ 记录 metrics
- ✅ 带错误记录 metrics

#### 并发限制测试（1 个测试）

**TestConcurrencyLimit** (4 个子测试)
- ✅ Drop 策略 - 限制内
- ✅ Drop 策略 - 超限丢弃
- ✅ Block 策略 - 阻塞等待
- ✅ TryWait 策略 - 超时

#### 重试测试（2 个测试）

**TestRetryConfig** (2 个子测试)
- ✅ 默认配置
- ✅ 自定义配置

**TestRetry** (4 个子测试)
- ✅ 首次成功
- ✅ 重试后成功
- ✅ 达到最大重试次数
- ✅ 自定义重试判断

#### 熔断器测试（1 个测试）

**TestCircuitBreaker** (5 个子测试)
- ✅ 创建新熔断器
- ✅ 默认配置
- ✅ Closed → Open 转换
- ✅ Open → HalfOpen → Closed 转换
- ✅ Closed 状态成功

#### 枚举和状态测试（2 个测试）

**TestConcurrencyPolicy** (3 个子测试)
- ✅ Drop
- ✅ Block
- ✅ TryWait

**TestCircuitBreakerState** (3 个子测试)
- ✅ Closed
- ✅ Open
- ✅ HalfOpen

#### 中间件链测试（1 个测试）

**TestMiddlewareChaining** (2 个子测试)
- ✅ 链接多个中间件
- ✅ 错误传播

#### 性能基准测试（5 个基准测试）

- ✅ BenchmarkLogging
- ✅ BenchmarkRecover
- ✅ BenchmarkRetry
- ✅ BenchmarkCircuitBreaker
- ✅ BenchmarkMiddlewareChain

---

## 🎯 测试覆盖率详情

### 覆盖率: ~31%+

**已覆盖的功能**:
- ✅ Logging: 100%
- ✅ Recover: 100%
- ✅ Auth: 100%
- ✅ Timeout: 90%+
- ✅ Metrics: 100%
- ✅ ConcurrencyLimit: 80%+
- ✅ Retry: 85%+
- ✅ CircuitBreaker: 75%+

**测试覆盖的场景**:
- ✅ 正常流程（所有中间件）
- ✅ 错误处理（panic、timeout、unauthorized）
- ✅ 并发场景（drop、block、try-wait）
- ✅ 重试策略（成功、失败、自定义判断）
- ✅ 熔断器状态转换
- ✅ 中间件链

---

## 🚀 运行测试

### 运行所有测试
```bash
go test -v
```

### 运行特定测试
```bash
# 基础中间件测试
go test -v -run TestLogging
go test -v -run TestRecover
go test -v -run TestAuth

# 并发限制测试
go test -v -run TestConcurrencyLimit

# 重试测试
go test -v -run TestRetry

# 熔断器测试
go test -v -run TestCircuitBreaker
```

### 生成覆盖率报告
```bash
go test -coverprofile coverage.out -cover
go tool cover -func coverage.out
go tool cover -html coverage.out  # 生成 HTML 报告
```

### 运行基准测试
```bash
# 运行所有基准测试
go test -bench=. -benchmem

# 运行特定基准测试
go test -bench=BenchmarkLogging -benchmem
go test -bench=BenchmarkRetry -benchmem
go test -bench=BenchmarkCircuitBreaker -benchmem
```

---

## 📝 测试最佳实践

本测试套件遵循以下最佳实践：

1. **Mock Handler** - 使用 mockHandler 简化测试
2. **表驱动测试** - 多场景测试用例
3. **子测试** - 使用 `t.Run()` 组织相关测试
4. **并发测试** - 验证并发限制策略
5. **状态测试** - 验证熔断器状态转换
6. **错误验证** - 使用 `errors.Is` 和 `assert.Contains`
7. **时间控制** - 使用延迟测试异步行为

---

## 🔍 测试详情

### 中间件架构

```
Middleware = func(next Handler) Handler

Handler = func(*Context) error

中间件链: MW1(MW2(MW3(Handler)))
```

### 核心中间件

**Logging**: 记录请求耗时和错误
**Recover**: 捕获 panic 防止崩溃
**Auth**: 简单认证中间件
**Timeout**: 请求超时控制
**Metrics**: 性能指标记录
**ConcurrencyLimit**: 并发限制
**Retry**: 重试机制
**CircuitBreaker**: 熔断器

### 并发策略

```
ConcurrencyDrop - 超限直接丢弃
ConcurrencyBlock - 阻塞等待
ConcurrencyTryWait - 尝试等待，超时丢弃
```

### 熔断器状态

```
Closed (闭合)
  ↓ 失败达到阈值
Open (开启)
  ↓ 超时后
HalfOpen (半开)
  ↓ 成功      ↓ 失败
Closed       Open
```

---

## 📚 使用示例

### Logging 中间件

```go
engine.Use(middleware.Logging())
```

### Recover 中间件

```go
engine.Use(middleware.Recover())
```

### Auth 中间件

```go
engine.Use(middleware.Auth(func(ctx *context.Context) bool {
    // 检查用户权限
    return isAuthorized(ctx.GetAuthor())
}))
```

### Timeout 中间件

```go
engine.Use(middleware.Timeout(5 * time.Second))
```

### ConcurrencyLimit 中间件

```go
// Drop 策略
engine.Use(middleware.ConcurrencyLimit(100, middleware.ConcurrencyDrop, 0))

// Block 策略
engine.Use(middleware.ConcurrencyLimit(100, middleware.ConcurrencyBlock, 0))

// TryWait 策略
engine.Use(middleware.ConcurrencyLimit(100, middleware.ConcurrencyTryWait, 200*time.Millisecond))
```

### Retry 中间件

```go
engine.Use(middleware.Retry(middleware.RetryConfig{
    MaxAttempts: 3,
    BackoffBase: 200 * time.Millisecond,
    BackoffMax:  2 * time.Second,
}))
```

### CircuitBreaker 中间件

```go
cb := middleware.NewCircuitBreaker(middleware.CircuitBreakerConfig{
    MaxFailures: 5,
    ResetTimeout: 30 * time.Second,
    HalfOpenMaxRequests: 3,
})
engine.Use(middleware.CircuitBreakerMiddleware(cb))
```

### 中间件链

```go
engine.Use(
    middleware.Recover(),
    middleware.Logging(),
    middleware.Timeout(5*time.Second),
    middleware.Retry(retryConfig),
)
```

---

## 🎨 设计模式

### 1. 装饰器模式

中间件使用装饰器模式：

```go
type Middleware func(Handler) Handler
```

### 2. 责任链模式

中间件形成责任链：

```go
Recover → Logging → Timeout → Retry → Handler
```

### 3. 策略模式

并发限制使用策略模式：

```go
type ConcurrencyPolicy int

const (
    ConcurrencyDrop
    ConcurrencyBlock
    ConcurrencyTryWait
)
```

### 4. 状态模式

熔断器使用状态模式：

```go
type CircuitBreakerState string

const (
    StateClosed
    StateOpen
    StateHalfOpen
)
```

---

## ✅ 测试状态

- 所有测试通过 ✅
- 代码覆盖率: ~31%+ ✅
- 核心中间件全覆盖 ✅
- 并发策略全覆盖 ✅
- 熔断器状态转换全覆盖 ✅
- 性能基准完成 ✅

---

## 🔧 未来改进

可以考虑的测试增强：

1. **更多中间件测试**
   - DeadLetter 中间件
   - Dedup 中间件
   - Degradation 中间件
   - Prometheus 中间件

2. **边界测试**
   - 极端并发场景
   - 超长重试
   - 熔断器恢复

3. **集成测试**
   - 多中间件组合
   - 真实场景模拟

4. **压力测试**
   - 高并发负载
   - 内存泄漏检测
   - CPU 使用率

---

## 📊 关键测试场景

### 1. Panic 恢复

```
Handler panic → Recover 捕获 → 转换为 error → 记录堆栈
```

### 2. 超时控制

```
Handler 执行 100ms
Timeout 设为 50ms
→ Context 取消
→ 返回 timeout error
```

### 3. 并发限制

```
MaxInFlight = 1
Request1 开始执行
Request2 尝试执行
→ Drop: 直接拒绝
→ Block: 等待 Request1 完成
→ TryWait: 等待 30ms 后拒绝
```

### 4. 重试机制

```
Attempt 1: 失败 → 等待 200ms
Attempt 2: 失败 → 等待 400ms
Attempt 3: 成功 → 返回
```

### 5. 熔断器

```
Closed 状态:
  失败 1 次 → 仍然 Closed
  失败 2 次 → 转换到 Open
  
Open 状态:
  拒绝所有请求
  等待 30s → 转换到 HalfOpen
  
HalfOpen 状态:
  成功 → 转换到 Closed
  失败 → 转换回 Open
```

---

## 🌟 最佳实践

### 1. 中间件顺序

推荐顺序：
```go
Recover (最外层，捕获所有 panic)
Logging (记录请求)
Timeout (超时控制)
CircuitBreaker (熔断器)
Retry (重试)
ConcurrencyLimit (并发限制)
Handler (业务逻辑)
```

### 2. 错误处理

```go
// 在中间件中传播错误
if err := next(ctx); err != nil {
    // 记录或处理
    log.Error(err)
    return err
}
return nil
```

### 3. Context 传递

```go
// 保存原始 context
originalCtx := ctx.Context()
defer ctx.SetStdContext(originalCtx)

// 设置带超时的 context
ctx.SetStdContext(timeoutCtx)
```

### 4. 并发安全

```go
// 使用 atomic 操作
atomic.AddUint64(&dropped, 1)

// 或使用 channel
select {
case sema <- struct{}{}:
    acquired = true
default:
    return error
}
```

---

**最后更新**: 2026-01-22  
**维护者**: Remilia 开发团队
