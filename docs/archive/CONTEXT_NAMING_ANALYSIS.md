# Context 命名与标准库集成分析

> 分析日期: 2025-12-02  
> 版本: v1.2.1  
> 问题: 是否需要集成标准库 context.Context？是否需要重命名以避免混淆？

---

## 📋 问题背景

当前框架有自己的 `remilia.Context` 类型，主要职责是：
- 事件数据访问（`event *dto.Payload`）
- 状态管理（`state State`）
- API 调用封装（`api openapi.OpenAPI`）
- 引用计数和对象池复用（`refs int32`）

**核心问题**: 缺少标准库 `context.Context` 集成，导致：
1. 无法使用标准库的超时、取消、截止时间功能
2. 与 Go 生态不兼容（database/sql, http.Client, grpc 等）
3. 无法传播 request-scoped 值（如 trace ID）

---

## 🔍 需要性分析

### 1. 是否需要集成标准库 context.Context？

**答案: 🔴 强烈建议集成**

#### 1.1 实际场景分析

##### 场景 1: 数据库查询
```go
// ❌ 当前无法实现
engine.OnC2C(OnCommand("/query")).HandleE(func(ctx *remilia.Context) error {
    // 想要设置数据库查询超时，但无法传递 context
    rows, err := db.Query("SELECT * FROM users WHERE id = ?", userID)
    // 如果数据库查询很慢，无法主动取消
    return err
})

// ✅ 集成后可以实现
engine.OnC2C(OnCommand("/query")).HandleE(func(ctx *remilia.Context) error {
    // 设置 5 秒超时
    dbCtx, cancel := context.WithTimeout(ctx.Context(), 5*time.Second)
    defer cancel()
    
    rows, err := db.QueryContext(dbCtx, "SELECT * FROM users WHERE id = ?", userID)
    // 超时后自动取消查询
    return err
})
```

##### 场景 2: HTTP 请求
```go
// ❌ 当前无法实现
engine.OnC2C(OnCommand("/weather")).HandleE(func(ctx *remilia.Context) error {
    // 调用第三方天气 API，无法设置超时
    resp, err := http.Get("https://api.weather.com/...")
    // 如果 API 很慢，会一直等待
    return err
})

// ✅ 集成后可以实现
engine.OnC2C(OnCommand("/weather")).HandleE(func(ctx *remilia.Context) error {
    httpCtx, cancel := context.WithTimeout(ctx.Context(), 3*time.Second)
    defer cancel()
    
    req, _ := http.NewRequestWithContext(httpCtx, "GET", "https://api.weather.com/...", nil)
    resp, err := http.DefaultClient.Do(req)
    // 3 秒后自动取消请求
    return err
})
```

##### 场景 3: 多步骤处理 + 主动取消
```go
// ❌ 当前无法实现
engine.OnC2C(OnCommand("/process")).HandleE(func(ctx *remilia.Context) error {
    step1() // 耗时 1s
    step2() // 耗时 2s
    step3() // 耗时 3s
    // 如果用户想中途取消，无法实现
    return nil
})

// ✅ 集成后可以实现
engine.OnC2C(OnCommand("/process")).HandleE(func(ctx *remilia.Context) error {
    if err := step1(ctx.Context()); err != nil {
        return err
    }
    
    // 检查是否被取消
    select {
    case <-ctx.Done():
        return ctx.Err() // 返回 "context canceled"
    default:
    }
    
    if err := step2(ctx.Context()); err != nil {
        return err
    }
    
    select {
    case <-ctx.Done():
        return ctx.Err()
    default:
    }
    
    return step3(ctx.Context())
})
```

##### 场景 4: 分布式追踪
```go
// ❌ 当前无法实现
engine.OnC2C(OnCommand("/order")).HandleE(func(ctx *remilia.Context) error {
    // 无法传播 trace ID 到下游服务
    orderService.CreateOrder(orderData)
    return nil
})

// ✅ 集成后可以实现
engine.OnC2C(OnCommand("/order")).HandleE(func(ctx *remilia.Context) error {
    // trace ID 自动通过 context 传播
    span, ctx := opentracing.StartSpanFromContext(ctx.Context(), "create_order")
    defer span.Finish()
    
    // 传递到数据库、缓存、下游服务
    orderService.CreateOrder(ctx, orderData)
    return nil
})
```

#### 1.2 竞品对比

| 框架 | Context 类型 | 标准库集成方式 | 是否封装标准库方法 | 备注 |
|------|-------------|--------------|------------------|------|
| **gin** | `gin.Context` | `ctx.Request.Context()` | ❌ 不封装 | ✅ 正确做法 |
| **echo** | `echo.Context` | `ctx.Request().Context()` | ❌ 不封装 | ✅ 正确做法 |
| **fiber** | `fiber.Ctx` | `ctx.Context()` | ❌ 不封装 | ✅ 正确做法 |
| **beego** | `context.Context` | 直接使用标准库 | N/A | ✅ 原生支持 |
| **go-zero** | `svc.ServiceContext` | 字段 `ctx context.Context` | ❌ 不封装 | ✅ 正确做法 |
| **kratos** | `context.Context` | 直接使用标准库 | N/A | ✅ 原生支持 |
| **remilia** | `remilia.Context` | ❌ **无** | N/A | **需要改进** |

**关键发现**:
1. ✅ 所有框架都集成了标准库 `context.Context`
2. ✅ **没有任何框架封装** `WithTimeout`, `WithCancel` 等标准库方法
3. ✅ 都是通过方法或字段**直接暴露**标准库 context，让用户自己使用

**结论**: 提供访问接口即可，不要封装标准库功能。

#### 1.3 不集成的后果

1. **生态隔离**: 无法使用标准库和第三方库的 Context API
   - `database/sql`: `QueryContext`, `ExecContext`
   - `net/http`: `NewRequestWithContext`
   - `grpc`: 所有方法都需要 `context.Context`
   - `redis`: `redis.Client.Get(ctx, key)`

2. **功能受限**: 无法实现超时、取消、截止时间控制
   - 长时间运行的 Handler 无法主动中断
   - 资源泄漏风险（goroutine、连接等）

3. **可观测性差**: 无法传播 trace ID、request ID
   - 无法实现完整的分布式追踪
   - 日志关联困难

4. **用户体验差**: 与用户预期不符
   - Go 开发者习惯使用 `context.Context`
   - 学习成本增加（需要了解为什么不用标准库）

**评估结果**: 🔴 **必须集成**（必要性等级最高）

---

## 🔍 命名冲突分析

### 2. 是否需要重命名 remilia.Context？

**答案: 🟡 不建议重命名，但需要明确职责分离**

#### 2.1 Go 生态中的命名惯例

##### 例子 1: gin.Context
```go
// gin 框架
type Context struct {
    Request   *http.Request   // 包含 Request.Context()
    Writer    ResponseWriter
    Params    Params
    Keys      map[string]interface{}  // 类似我们的 State
    // ...
}

// 访问标准库 context
func (c *Context) Request.Context() context.Context

// 实际使用
engine.GET("/user/:id", func(c *gin.Context) {
    // gin 的 Context
    userID := c.Param("id")
    
    // 标准库 context
    ctx := c.Request.Context()
    user, err := db.GetUserContext(ctx, userID)
    
    c.JSON(200, user)
})
```

##### 例子 2: echo.Context
```go
// echo 框架
type Context interface {
    Request() *http.Request
    Response() *Response
    Get(key string) interface{}  // 类似我们的 State
    // ...
}

// 访问标准库 context
func (c Context) Request().Context() context.Context

// 实际使用
e.GET("/user/:id", func(c echo.Context) error {
    // echo 的 Context
    userID := c.Param("id")
    
    // 标准库 context
    ctx := c.Request().Context()
    user, err := db.GetUserContext(ctx, userID)
    
    return c.JSON(200, user)
})
```

##### 例子 3: fiber.Ctx
```go
// fiber 框架（选择了简短命名）
type Ctx struct {
    // ...
}

// 访问标准库 context
func (c *Ctx) Context() context.Context

// 实际使用
app.Get("/user/:id", func(c *fiber.Ctx) error {
    // fiber 的 Ctx
    userID := c.Params("id")
    
    // 标准库 context
    ctx := c.Context()
    user, err := db.GetUserContext(ctx, userID)
    
    return c.JSON(user)
})
```

**共同特点**:
1. 框架都保留了自己的 Context 类型名称
2. 通过方法访问标准库 context（不直接暴露）
3. 框架 Context 是 "rich context"（包含更多功能）
4. 标准库 context 是 "pure context"（只管生命周期）

#### 2.2 命名方案对比

##### 方案 A: 保持 remilia.Context，内嵌标准库 context ✅ **推荐**

```go
// context.go
type Context struct {
    ctx     context.Context    // 内嵌标准库 context
    cancel  context.CancelFunc
    
    matcher *Matcher
    event   *dto.Payload
    state   State
    stateMu *sync.RWMutex
    api     openapi.OpenAPI
    refs    int32
}

// 访问标准库 context
func (c *Context) Context() context.Context {
    return c.ctx
}

func (c *Context) Done() <-chan struct{} {
    return c.ctx.Done()
}

func (c *Context) Err() error {
    return c.ctx.Err()
}

// 使用示例
engine.OnC2C(OnCommand("/query")).HandleE(func(ctx *remilia.Context) error {
    // remilia 的功能
    userID := ctx.GetString("user_id")
    
    // 标准库 context（通过方法访问）
    dbCtx, cancel := context.WithTimeout(ctx.Context(), 5*time.Second)
    defer cancel()
    
    user, err := db.QueryContext(dbCtx, "SELECT * FROM users WHERE id = ?", userID)
    return err
})
```

**优点**:
- ✅ 符合 Go 生态惯例（gin, echo, fiber 都这样做）
- ✅ 向后兼容（不破坏现有代码）
- ✅ 职责清晰（框架 Context 是增强版）
- ✅ 命名自然（`ctx.Context()` 语义明确）

**缺点**:
- ⚠️ 需要 `ctx.Context()` 调用（略显冗余）

---

##### 方案 B: 重命名为 remilia.EventContext ❌ **不推荐**

```go
// context.go
type EventContext struct {
    ctx     context.Context
    event   *dto.Payload
    state   State
    api     openapi.OpenAPI
    // ...
}

// 使用示例
engine.OnC2C(OnCommand("/query")).HandleE(func(ctx *remilia.EventContext) error {
    // ...
})
```

**优点**:
- ✅ 名称更具体，避免歧义

**缺点**:
- ❌ 破坏向后兼容（所有用户代码需要修改）
- ❌ 不符合 Go 生态惯例（gin 不叫 gin.RequestContext）
- ❌ 冗长且不够简洁
- ❌ 迁移成本极高

---

##### 方案 C: 重命名为 remilia.Ctx ❌ **不推荐**

```go
// context.go
type Ctx struct {
    ctx     context.Context
    event   *dto.Payload
    state   State
    api     openapi.OpenAPI
    // ...
}

// 使用示例
engine.OnC2C(OnCommand("/query")).HandleE(func(ctx *remilia.Ctx) error {
    // ...
})
```

**优点**:
- ✅ 简短（参考 fiber.Ctx）

**缺点**:
- ❌ 破坏向后兼容
- ❌ `Ctx` 缩写不够正式（不适合严肃框架）
- ❌ 迁移成本高

---

##### 方案 D: 直接使用 context.Context ❌ **不推荐**

```go
// 不再有自己的 Context 类型
type Handler func(ctx context.Context, event *dto.Payload, api openapi.OpenAPI)

// 使用示例
engine.OnC2C(OnCommand("/query")).HandleE(func(ctx context.Context, event *dto.Payload, api openapi.OpenAPI) error {
    // 需要手动管理所有参数
})
```

**缺点**:
- ❌ 参数过多，不方便使用
- ❌ 失去状态管理功能（State）
- ❌ 失去对象池优化
- ❌ 完全破坏现有 API

---

#### 2.3 方案推荐

**最终推荐: 方案 A（保持 remilia.Context，内嵌标准库 context）**

**理由**:
1. ✅ **符合 Go 生态惯例** - gin、echo、fiber 都这样做
2. ✅ **向后兼容** - 现有代码无需修改
3. ✅ **职责清晰** - 框架 Context 是功能增强版
4. ✅ **迁移简单** - 只需添加新方法，不需要改名

---

## 📝 实施方案

### 3. 推荐实施方案（方案 A）

#### 3.0 设计原则

**为什么不封装 WithTimeout, WithCancel 等方法？**

❌ **错误做法**（过度封装）:
```go
type Context struct {
    ctx    context.Context
    cancel context.CancelFunc
}

func (c *Context) WithTimeout(timeout time.Duration) {
    c.ctx, c.cancel = context.WithTimeout(c.ctx, timeout)
}

func (c *Context) Done() <-chan struct{} {
    return c.ctx.Done()
}
// ... 重复封装大量标准库方法
```

**问题**:
1. 🔴 **违反单一职责原则** - Context 应专注于框架功能，不应成为标准库的代理
2. 🔴 **维护负担重** - 标准库每次更新都要同步
3. 🔴 **灵活性差** - 封装后的 API 可能无法覆盖所有场景
4. 🔴 **用户困惑** - 为什么要用 `ctx.WithTimeout()` 而不是 `context.WithTimeout(ctx.Context())`？

✅ **正确做法**（最小化接口）:
```go
type Context struct {
    ctx context.Context  // 只存储，不封装
}

func (c *Context) Context() context.Context {
    return c.ctx  // 只提供访问，让用户直接使用标准库
}
```

**优点**:
1. ✅ **职责清晰** - 框架 Context 专注于事件、状态、API
2. ✅ **零维护成本** - 标准库更新不影响框架
3. ✅ **灵活性高** - 用户可以使用标准库的所有功能
4. ✅ **符合 Go 惯例** - 不重复发明轮子

#### 3.1 代码实现（简洁版 - 推荐）

**核心原则**: 只提供访问接口，不重复封装标准库功能

```go
// context.go
package remilia

import (
    stdctx "context"  // 别名避免冲突
    "sync"
    "sync/atomic"
)

// Context 上下文
type Context struct {
    // 标准库 context（新增）
    ctx stdctx.Context
    
    // 原有字段
    matcher *Matcher
    event   *dto.Payload
    state   State
    stateMu *sync.RWMutex
    api     openapi.OpenAPI
    refs    int32
}

// NewContext 创建一个新的上下文
func NewContext(event *dto.Payload, api openapi.OpenAPI) *Context {
    ctx := contextPool.Get().(*Context)
    
    // 初始化标准库 context（新增）
    ctx.ctx = stdctx.Background()
    
    // 原有逻辑
    ctx.event = event
    ctx.api = api
    ctx.matcher = nil
    atomic.StoreInt32(&ctx.refs, 1)
    
    if ctx.stateMu == nil {
        ctx.stateMu = &sync.RWMutex{}
    }
    
    return ctx
}

// NewContextWithContext 创建带标准库 context 的上下文（新增）
// 用于需要自定义 context 的场景（如中间件注入 trace context）
func NewContextWithContext(ctx stdctx.Context, event *dto.Payload, api openapi.OpenAPI) *Context {
    c := NewContext(event, api)
    c.ctx = ctx
    return c
}

// Context 返回标准库 context.Context（新增）
// 这是唯一需要添加的方法，其他功能直接使用标准库
func (c *Context) Context() stdctx.Context {
    if c.ctx == nil {
        c.ctx = stdctx.Background()
    }
    return c.ctx
}

// Release 释放 Context（保持原有逻辑）
func (c *Context) Release() {
    if c == nil {
        return
    }
    if atomic.AddInt32(&c.refs, -1) > 0 {
        return
    }
    
    // 标准库 context 不需要显式清理，直接置 nil
    c.ctx = nil
    
    // 原有清理逻辑
    c.stateMu.Lock()
    for k := range c.state {
        delete(c.state, k)
    }
    c.stateMu.Unlock()
    
    c.event = nil
    c.api = nil
    c.matcher = nil
    
    contextPool.Put(c)
}
```

**设计理念**:
- ✅ 只提供 `Context()` 方法访问标准库 context
- ✅ 不封装 `WithTimeout`, `WithCancel` 等标准库方法
- ✅ 用户直接使用标准库 API，更灵活、更符合 Go 惯例
- ✅ 减少代码维护负担
```

#### 3.2 使用示例

##### 示例 1: 数据库查询超时
```go
engine.OnC2C(OnCommand("/users")).HandleE(func(ctx *remilia.Context) error {
    // 设置 5 秒超时
    dbCtx, cancel := context.WithTimeout(ctx.Context(), 5*time.Second)
    defer cancel()
    
    // 查询数据库
    var users []User
    err := db.SelectContext(dbCtx, &users, "SELECT * FROM users LIMIT 100")
    if err != nil {
        if err == context.DeadlineExceeded {
            ctx.ReplyGroup(&dto.Message{Content: "查询超时，请稍后重试"})
            return nil
        }
        return err
    }
    
    ctx.ReplyGroup(&dto.Message{Content: fmt.Sprintf("找到 %d 个用户", len(users))})
    return nil
})
```

##### 示例 2: HTTP 请求超时
```go
engine.OnC2C(OnCommand("/weather")).HandleE(func(ctx *remilia.Context) error {
    city := ctx.GetString("city")
    
    // 创建带超时的 HTTP 请求
    httpCtx, cancel := context.WithTimeout(ctx.Context(), 3*time.Second)
    defer cancel()
    
    url := fmt.Sprintf("https://api.weather.com/v1/weather?city=%s", city)
    req, _ := http.NewRequestWithContext(httpCtx, "GET", url, nil)
    
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        if err == context.DeadlineExceeded {
            ctx.ReplyGroup(&dto.Message{Content: "天气查询超时"})
            return nil
        }
        return err
    }
    defer resp.Body.Close()
    
    // 处理响应...
    return nil
})
```

##### 示例 3: 多步骤处理 + 取消检测
```go
engine.OnC2C(OnCommand("/process")).HandleE(func(ctx *remilia.Context) error {
    // 获取标准库 context
    stdCtx := ctx.Context()
    
    // Step 1
    if err := processStep1(stdCtx); err != nil {
        return err
    }
    
    // 检查是否被取消（直接使用标准库 API）
    select {
    case <-stdCtx.Done():
        return stdCtx.Err()
    default:
    }
    
    // Step 2
    if err := processStep2(stdCtx); err != nil {
        return err
    }
    
    select {
    case <-stdCtx.Done():
        return stdCtx.Err()
    default:
    }
    
    // Step 3
    return processStep3(stdCtx)
})

func processStep1(ctx context.Context) error {
    // 长时间操作，支持取消
    select {
    case <-ctx.Done():
        return ctx.Err()
    case <-time.After(2 * time.Second):
        return nil
    }
}
```

##### 示例 4: 分布式追踪
```go
// 使用 OpenTelemetry
engine.OnC2C(OnCommand("/order")).HandleE(func(ctx *remilia.Context) error {
    // 从 context 中提取 trace ID（由中间件注入）
    span := trace.SpanFromContext(ctx.Context())
    span.SetAttributes(attribute.String("user_id", ctx.GetString("user_id")))
    
    // trace context 自动传播
    order, err := orderService.CreateOrder(ctx.Context(), orderData)
    if err != nil {
        span.RecordError(err)
        return err
    }
    
    span.SetAttributes(attribute.String("order_id", order.ID))
    return nil
})
```

#### 3.3 迁移指南

**向后兼容性**: ✅ 完全兼容，现有代码无需修改

**新功能使用**:
```go
// 旧代码（继续工作）
engine.OnC2C(OnCommand("/hello")).Handle(func(ctx *remilia.Context) {
    ctx.ReplyGroup(&dto.Message{Content: "Hello"})
})

// 新功能（按需使用）
engine.OnC2C(OnCommand("/query")).HandleE(func(ctx *remilia.Context) error {
    dbCtx, cancel := context.WithTimeout(ctx.Context(), 5*time.Second)
    defer cancel()
    
    result, err := db.QueryContext(dbCtx, "SELECT ...")
    return err
})
```

---

## 📊 影响范围评估

### 4. 破坏性影响评估

| 方案 | 破坏性影响 | 迁移成本 | 用户体验 | 推荐度 |
|------|-----------|---------|---------|--------|
| **方案 A: 保持名称** | ✅ 无 | ✅ 无 | ⭐⭐⭐⭐⭐ | 🔴 强烈推荐 |
| 方案 B: 改名 EventContext | ❌ 极大 | ❌ 极高 | ⭐⭐ | ❌ 不推荐 |
| 方案 C: 改名 Ctx | ❌ 极大 | ❌ 极高 | ⭐⭐⭐ | ❌ 不推荐 |
| 方案 D: 去掉 Context | ❌ 极大 | ❌ 极高 | ⭐ | ❌ 不推荐 |

### 5. 工作量评估

| 任务 | 预估工作量 | 优先级 | 说明 |
|------|-----------|--------|------|
| 添加标准库 context 字段 | 0.5h | P0 | 只需添加一个字段 |
| 实现 Context() 访问方法 | 0.5h | P0 | 只需一个简单的 getter |
| 实现 NewContextWithContext | 0.5h | P0 | 可选的构造函数 |
| 修改 NewContext 初始化 | 0.5h | P0 | 添加 ctx = Background() |
| 修改 Release 清理逻辑 | 0.5h | P0 | 添加 ctx = nil |
| 编写单元测试 | 2h | P0 | 测试 Context() 访问 |
| 编写集成测试 | 2h | P0 | 测试实际场景 |
| 编写使用文档 | 2h | P1 | 示例和最佳实践 |
| 更新示例代码 | 2h | P1 | 演示正确用法 |

**总计**: 10.5 小时（~1.5 个工作日）

**对比原方案**:
- 原方案（含封装）: 17 小时
- 新方案（不封装）: 10.5 小时
- **节省**: 6.5 小时（38% 减少）

---

## ✅ 结论

### 6. 最终建议

#### 6.1 是否需要集成标准库 context.Context？

**答案**: 🔴 **强烈建议集成**（必要性等级最高）

**理由**:
1. ✅ 生态兼容：所有 Go 主流框架的标准做法
2. ✅ 功能需求：超时、取消、追踪是实际需求
3. ✅ 用户预期：Go 开发者的习惯和预期
4. ✅ 可观测性：分布式追踪、日志关联的基础

**不实施的风险**:
- 🔴 用户无法使用标准库 API（database/sql, http.Client）
- 🔴 无法实现超时控制和主动取消
- 🔴 可观测性受限
- 🔴 框架竞争力下降

#### 6.2 是否需要重命名 remilia.Context？

**答案**: 🟢 **不需要重命名**

**理由**:
1. ✅ 符合 Go 生态惯例（gin.Context, echo.Context, fiber.Ctx）
2. ✅ 向后兼容，零破坏性
3. ✅ 职责清晰，语义明确
4. ✅ 实施简单，工作量小

**推荐方案**: 
- 保持 `remilia.Context` 名称
- 内嵌标准库 `context.Context`
- 通过 `ctx.Context()` 方法访问
- 提供 `WithTimeout()`, `WithCancel()` 等便捷方法

#### 6.3 实施优先级

| 优先级 | 任务 | 版本 | 时间 |
|--------|------|------|------|
| 🔴 P0 | 集成标准库 context | v1.3.0 | Week 1 |
| 🟡 P1 | 完善文档和示例 | v1.3.0 | Week 2 |
| 🟢 P2 | 推广最佳实践 | v1.3.0 | Week 3 |

---

## 📚 参考资料

### 7. 相关链接

- [Go Context 官方文档](https://pkg.go.dev/context)
- [Gin Context 实现](https://github.com/gin-gonic/gin/blob/master/context.go)
- [Echo Context 实现](https://github.com/labstack/echo/blob/master/context.go)
- [Fiber Ctx 实现](https://github.com/gofiber/fiber/blob/master/ctx.go)

### 8. 相关文档

- [COMPONENT_ANALYSIS_2025_12_02.md](./COMPONENT_ANALYSIS_2025_12_02.md) - 组件深度分析
- [组件分析摘要_2025_12_02.md](./组件分析摘要_2025_12_02.md) - 中文摘要

---

---

## ❓ 常见问题解答

### Q1: 为什么不封装 WithTimeout, WithCancel 等标准库方法？

**A:** 这是过度设计，会导致：
1. 维护负担重（标准库更新需同步）
2. 灵活性差（无法覆盖所有场景）
3. 用户困惑（不符合 Go 惯例）
4. 违反单一职责原则

**正确做法**: 只提供 `Context()` 访问方法，让用户直接使用标准库 API。

**示例对比**:
```go
// ❌ 错误：封装标准库方法
ctx.WithTimeout(5*time.Second)
ctx.WithCancel()
ctx.Done()

// ✅ 正确：直接使用标准库
stdCtx, cancel := context.WithTimeout(ctx.Context(), 5*time.Second)
defer cancel()

select {
case <-stdCtx.Done():
    return stdCtx.Err()
default:
    // ...
}
```

---

### Q2: gin/echo/fiber 都是怎么做的？

**A:** 所有主流框架都是**只提供访问接口，不封装标准库方法**。

```go
// gin 框架
func (c *gin.Context) Request.Context() context.Context  // 只提供访问

// echo 框架
func (c echo.Context) Request().Context() context.Context  // 只提供访问

// fiber 框架
func (c *fiber.Ctx) Context() context.Context  // 只提供访问

// 用户自己使用标准库
ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
defer cancel()
```

**没有任何框架**封装 `WithTimeout`, `WithCancel` 等方法。

---

### Q3: 那为什么要集成标准库 context？

**A:** 不是为了封装，而是为了**生态兼容**：

```go
// 使用数据库
db.QueryContext(ctx.Context(), "SELECT ...")

// 使用 HTTP 客户端
req, _ := http.NewRequestWithContext(ctx.Context(), "GET", url, nil)

// 使用 gRPC
client.GetUser(ctx.Context(), &pb.GetUserRequest{})

// 使用 Redis
rdb.Get(ctx.Context(), key)
```

所有标准库和第三方库都需要 `context.Context`，我们必须提供访问接口。

---

### Q4: 如果用户需要超时控制怎么办？

**A:** 直接使用标准库 API：

```go
engine.OnC2C(OnCommand("/query")).HandleE(func(ctx *remilia.Context) error {
    // 方式 1: 使用 context.WithTimeout
    dbCtx, cancel := context.WithTimeout(ctx.Context(), 5*time.Second)
    defer cancel()
    
    result, err := db.QueryContext(dbCtx, "SELECT ...")
    if err == context.DeadlineExceeded {
        return fmt.Errorf("查询超时")
    }
    return err
})
```

或者使用中间件（全局控制）：

```go
// 使用框架提供的 Timeout 中间件
engine.Use(middleware.Timeout(5*time.Second))
```

---

### Q5: 是否需要提供 Done(), Err() 等便捷方法？

**A:** **不需要**。理由：

1. **标准库 API 已经足够简洁**:
   ```go
   // 不需要封装成 ctx.Done()
   stdCtx := ctx.Context()
   <-stdCtx.Done()  // 只多了一行代码，可接受
   ```

2. **保持一致性**: 如果封装 `Done()`, 是否也要封装 `Err()`, `Deadline()`, `Value()`？这会导致无限扩展。

3. **违反最小接口原则**: 框架应该提供最小必要接口，而不是便捷封装。

**最终方案**: 只提供 `Context()` 方法，其他一律使用标准库。

---

### Q6: 其他框架也只提供一个方法吗？

**A:** 是的，看实际案例：

```go
// gin - 只提供访问，不封装
type Context struct {
    Request *http.Request
    // ...
}
// 用户使用: c.Request.Context()

// echo - 只提供访问，不封装
type Context interface {
    Request() *http.Request
    // ...
}
// 用户使用: c.Request().Context()

// fiber - 只提供访问，不封装
type Ctx struct {
    fasthttp *fasthttp.RequestCtx
    // ...
}
func (c *Ctx) Context() context.Context { ... }
// 用户使用: c.Context()
```

**共同特点**: 都是最小接口，没有封装标准库方法。

---

## 🎯 最终设计方案

### 代码实现（最简化版本）

```go
// context.go
package remilia

import (
    stdctx "context"
    // ...
)

type Context struct {
    ctx     stdctx.Context  // 唯一新增字段
    event   *dto.Payload
    state   State
    api     openapi.OpenAPI
    // ...
}

// 唯一需要添加的方法
func (c *Context) Context() stdctx.Context {
    if c.ctx == nil {
        c.ctx = stdctx.Background()
    }
    return c.ctx
}

// 可选：支持自定义 context
func NewContextWithContext(ctx stdctx.Context, event *dto.Payload, api openapi.OpenAPI) *Context {
    c := NewContext(event, api)
    c.ctx = ctx
    return c
}
```

### 用户使用示例

```go
engine.OnC2C(OnCommand("/query")).HandleE(func(ctx *remilia.Context) error {
    // 1. 获取标准库 context
    stdCtx := ctx.Context()
    
    // 2. 根据需要创建子 context
    dbCtx, cancel := context.WithTimeout(stdCtx, 5*time.Second)
    defer cancel()
    
    // 3. 传递给标准库/第三方库
    result, err := db.QueryContext(dbCtx, "SELECT * FROM users")
    return err
})
```

---

**分析完成时间**: 2025-12-02  
**分析人员**: AI 代码审查系统  
**报告状态**: ✅ 已完成（已优化）

