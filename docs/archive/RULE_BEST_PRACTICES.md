# 规则函数最佳实践指南

> 创建日期: 2025-12-02  
> 适用版本: v0.8.0+

---

## 📋 规则函数的设计原则

### Rule 函数定义

```go
type Rule func(ctx *Context) bool
```

Rule 是一个接受 Context 并返回布尔值的函数，用于判断事件是否匹配特定条件。

---

## ⚠️ 重要：规则函数应该是纯函数

### 什么是纯函数？

纯函数具有以下特性：
1. **无副作用** - 不修改外部状态
2. **幂等性** - 相同输入总是返回相同输出
3. **无依赖** - 不依赖外部可变状态

### 为什么规则函数应该是纯函数？

#### 1. 短路优化

And 和 Or 规则使用短路优化：

```go
// And 逻辑与
func And(rules ...Rule) Rule {
    return func(ctx *Context) bool {
        for _, rule := range rules {
            if !rule(ctx) {
                return false  // ✅ 短路：后续规则不执行
            }
        }
        return true
    }
}

// Or 逻辑或
func Or(rules ...Rule) Rule {
    return func(ctx *Context) bool {
        for _, rule := range rules {
            if rule(ctx) {
                return true  // ✅ 短路：后续规则不执行
            }
        }
        return false
    }
}
```

**问题场景**：

```go
// ❌ 错误示例：规则有副作用
counter := 0
rule1 := func(ctx *Context) bool {
    counter++  // 副作用：修改外部变量
    return ctx.GetString("type") == "message"
}

rule2 := func(ctx *Context) bool {
    counter++  // 副作用：修改外部变量
    return ctx.GetString("user") == "admin"
}

// 使用 And
engine.OnC2C(And(rule1, rule2)).Handle(handler)

// 如果 rule1 返回 false，rule2 不会执行
// 导致 counter 只增加 1，不是 2
// 行为不确定，难以调试
```

#### 2. 规则可能被多次调用

框架内部可能会：
- 多次评估同一个规则
- 在不同上下文中调用规则
- 缓存规则结果（未来优化）

如果规则有副作用，这些操作会导致不可预测的行为。

---

## ✅ 正确的规则编写方式

### 示例 1: 检查事件类型（推荐）

```go
// ✅ 纯函数：只读取，不修改
func OnCommand(cmd string) Rule {
    return func(ctx *Context) bool {
        content := ctx.GetMessageContent()
        return strings.HasPrefix(content, cmd)
    }
}
```

### 示例 2: 检查用户权限（推荐）

```go
// ✅ 纯函数：只检查状态，不修改
func OnlyAdmin() Rule {
    return func(ctx *Context) bool {
        user := ctx.GetAuthor()
        return user.ID == "admin_id"
    }
}
```

### 示例 3: 复杂条件检查（推荐）

```go
// ✅ 纯函数：可以读取多个值，但不修改
func OnWorkingHours() Rule {
    return func(ctx *Context) bool {
        hour := time.Now().Hour()
        return hour >= 9 && hour <= 17
    }
}
```

---

## ❌ 错误的规则编写方式

### 反例 1: 修改外部状态

```go
// ❌ 错误：修改外部变量
counter := 0
rule := func(ctx *Context) bool {
    counter++  // 副作用！
    return true
}
```

**问题**: 
- 短路时 counter 不一致
- 并发调用导致竞态条件
- 难以测试和调试

**正确做法**: 在 Handler 中计数，不在 Rule 中

```go
// ✅ 正确：在 Handler 中处理副作用
var counter int32
engine.OnC2C().Handle(func(ctx *Context) {
    atomic.AddInt32(&counter, 1)  // Handler 中计数
})
```

---

### 反例 2: 修改 Context 状态

```go
// ❌ 错误：在规则中修改 Context
rule := func(ctx *Context) bool {
    ctx.SetState("checked", true)  // 副作用！
    return ctx.GetString("type") == "message"
}
```

**问题**:
- 如果规则因短路不执行，状态不一致
- 规则应该是"检查"，不是"操作"

**正确做法**: 在 Handler 中设置状态

```go
// ✅ 正确：规则只检查
rule := func(ctx *Context) bool {
    return ctx.GetString("type") == "message"
}

// Handler 中设置状态
engine.OnC2C(rule).Handle(func(ctx *Context) {
    ctx.SetState("checked", true)  // Handler 中设置
})
```

---

### 反例 3: 调用外部 API

```go
// ❌ 错误：在规则中调用 API
rule := func(ctx *Context) bool {
    user := ctx.GetAuthor()
    isVIP := checkVIPStatus(user.ID)  // 副作用：外部调用！
    return isVIP
}
```

**问题**:
- 性能问题（每次匹配都调用 API）
- 副作用（可能有配额限制）
- 短路时行为不一致

**正确做法**: 缓存结果或在 Middleware 中检查

```go
// ✅ 方案1：缓存 VIP 状态
vipCache := make(map[string]bool)
rule := func(ctx *Context) bool {
    user := ctx.GetAuthor()
    isVIP, ok := vipCache[user.ID]
    if !ok {
        // 在初始化时加载，不在规则中
        return false
    }
    return isVIP
}

// ✅ 方案2：在 Middleware 中预加载
engine.Use(func(next HandlerE) HandlerE {
    return func(ctx *Context) error {
        user := ctx.GetAuthor()
        isVIP := checkVIPStatus(user.ID)
        ctx.SetState("isVIP", isVIP)
        return next(ctx)
    }
})

// 规则中只读取
rule := func(ctx *Context) bool {
    return ctx.GetBool("isVIP")
}
```

---

## 🎯 最佳实践总结

### DO ✅

1. **只读取 Context** - 使用 Get 系列方法
2. **只读取 Event** - 检查事件类型和内容
3. **返回布尔值** - 简单明确的判断
4. **保持简单** - 复杂逻辑移到 Handler
5. **可测试** - 纯函数易于单元测试

### DON'T ❌

1. **不修改 Context** - 不调用 SetState
2. **不修改外部变量** - 不增加计数器等
3. **不调用外部 API** - 不进行 IO 操作
4. **不依赖可变状态** - 不读取全局变量
5. **不执行耗时操作** - 保持快速返回

---

## 🧪 测试规则函数

### 纯函数易于测试

```go
func TestOnCommand(t *testing.T) {
    rule := OnCommand("/ping")
    
    // 创建测试 Context
    ctx := &Context{
        event: &dto.Payload{
            Type: dto.C2CMessageCreate,
            // ...
        },
    }
    
    // 多次调用应该返回相同结果
    result1 := rule(ctx)
    result2 := rule(ctx)
    assert.Equal(t, result1, result2)  // ✅ 幂等性
}
```

### 有副作用的规则难以测试

```go
// ❌ 有副作用的规则
counter := 0
rule := func(ctx *Context) bool {
    counter++
    return true
}

// 测试困难
result1 := rule(ctx)  // counter = 1
result2 := rule(ctx)  // counter = 2
// result1 == result2，但副作用不同
```

---

## 📚 相关文档

- [And/Or 短路优化实现](rules.go:135-165)
- [Context API 文档](context.go)
- [Handler 最佳实践](GUIDE.md)

---

## ⚠️ 注意事项

### 框架无法强制纯函数

Go 语言无法在类型系统层面强制函数是纯函数，因此：
- **依赖开发者自律**
- **代码审查很重要**
- **遵循本指南的建议**

### 如果确实需要副作用

如果你的逻辑确实需要副作用（如计数、记录日志等）：

1. **使用 Handler** - 副作用应该在 Handler 中
2. **使用 Middleware** - 预处理逻辑放在 Middleware
3. **不要依赖短路** - 不要假设规则一定会执行

---

## 🔧 调试技巧

### 检测规则副作用

```go
// 添加日志检测规则调用
func LoggingRule(name string, rule Rule) Rule {
    return func(ctx *Context) bool {
        result := rule(ctx)
        logrus.Debugf("Rule %s: %v", name, result)
        return result
    }
}

// 使用
rule := LoggingRule("checkAdmin", OnlyAdmin())
```

### 检测重复调用

```go
// 检测规则是否被多次调用
callCount := make(map[string]int)
func CountingRule(name string, rule Rule) Rule {
    return func(ctx *Context) bool {
        callCount[name]++
        if callCount[name] > 1 {
            logrus.Warnf("Rule %s called %d times", name, callCount[name])
        }
        return rule(ctx)
    }
}
```

---

**总结**: 规则函数应该是纯函数，只进行判断，不执行副作用。所有修改操作应该在 Handler 或 Middleware 中进行。

---

## ⚡ 性能最佳实践

### 规则函数应该快速返回

**重要**: 规则函数在事件处理的关键路径上，应该快速返回（< 1ms）。

### 性能目标

| 类别 | 耗时 | 说明 |
|-----|------|------|
| ✅ 优秀 | < 100μs | 理想的规则性能 |
| ⚠️ 可接受 | < 1ms | 可接受范围 |
| ❌ 需优化 | > 10ms | 会影响吞吐量 |
| 🚫 严重 | > 100ms | 严重阻塞 |

### 为什么快速很重要？

假设每个事件需要检查 10 个 matcher：

| 规则耗时 | 总耗时 | 吞吐量 |
|---------|-------|-------|
| 100μs | 1ms | **1000 事件/秒** ✅ |
| 1ms | 10ms | 100 事件/秒 ⚠️ |
| 10ms | 100ms | 10 事件/秒 ❌ |
| 100ms | 1s | 1 事件/秒 🚫 |

---

## ✅ 快速规则示例

### 1. 简单比较

```go
// ✅ 非常快（< 100μs）
func CheckType(ctx *Context) bool {
    return ctx.GetString("type") == "message"
}

// ✅ 字符串操作
func CheckCommand(ctx *Context) bool {
    content := ctx.GetMessageContent()
    return strings.HasPrefix(content, "/ping")
}
```

### 2. 缓存数据

```go
// ✅ 预先缓存，O(1) 查找
var userCache = map[string]bool{
    "admin": true,
    "user1": false,
}

func IsAdmin(ctx *Context) bool {
    user := ctx.GetAuthor()
    return userCache[user.ID]  // O(1)
}
```

### 3. 在 Middleware 中预处理

```go
// ✅ Middleware 中预处理
engine.Use(func(next HandlerE) HandlerE {
    return func(ctx *Context) error {
        // 提前查询并缓存到 Context
        user := ctx.GetAuthor()
        isVIP := checkVIPStatus(user.ID)  // 只查一次
        ctx.SetState("isVIP", isVIP)
        return next(ctx)
    }
})

// 规则中只读取（非常快）
func VIPRule(ctx *Context) bool {
    return ctx.GetBool("isVIP")  // O(1)
}
```

---

## ❌ 慢规则反例

### 1. 数据库查询

```go
// ❌ 非常慢（可能 > 100ms）
func CheckUserInDB(ctx *Context) bool {
    user := ctx.GetAuthor()
    result := db.Query("SELECT * FROM users WHERE id = ?", user.ID)
    return result != nil
}

// ✅ 正确做法：在 Middleware 中预加载
engine.Use(func(next HandlerE) HandlerE {
    return func(ctx *Context) error {
        user := ctx.GetAuthor()
        userData := db.Query("SELECT * FROM users WHERE id = ?", user.ID)
        ctx.SetState("userData", userData)
        return next(ctx)
    }
})

// 规则中只读取
func HasUserData(ctx *Context) bool {
    _, ok := ctx.GetState("userData")
    return ok
}
```

### 2. HTTP 请求

```go
// ❌ 非常慢（可能 > 1s）
func CheckAPIStatus(ctx *Context) bool {
    resp, _ := http.Get("https://api.example.com/status")
    return resp != nil && resp.StatusCode == 200
}

// ✅ 正确做法：定期后台更新状态
var apiStatus bool
func updateAPIStatus() {
    ticker := time.NewTicker(30 * time.Second)
    for range ticker.C {
        resp, _ := http.Get("https://api.example.com/status")
        apiStatus = resp != nil && resp.StatusCode == 200
    }
}

// 规则中只读取缓存的状态
func IsAPIAvailable(ctx *Context) bool {
    return apiStatus
}
```

### 3. 复杂计算

```go
// ❌ 慢（可能 > 10ms）
func ComplexCheck(ctx *Context) bool {
    content := ctx.GetMessageContent()
    // 复杂的算法
    for i := 0; i < 100000; i++ {
        // heavy computation
    }
    return len(content) > 0
}

// ✅ 正确做法：简化算法或缓存结果
func SimpleCheck(ctx *Context) bool {
    content := ctx.GetMessageContent()
    return len(content) > 0  // 简单快速
}
```

### 4. 文件 IO

```go
// ❌ 慢（IO 操作）
func CheckFile(ctx *Context) bool {
    data, _ := ioutil.ReadFile("config.txt")
    return len(data) > 0
}

// ✅ 正确做法：启动时加载
var configData []byte
func init() {
    configData, _ = ioutil.ReadFile("config.txt")
}

func HasConfig(ctx *Context) bool {
    return len(configData) > 0
}
```

---

## 🔧 处理慢规则

### 方案 1: 使用 WithTimeout（不推荐）

```go
// ⚠️ 如果确实需要慢规则，可以使用超时保护
slowRule := func(ctx *Context) bool {
    // 可能很慢的操作
    return expensiveCheck()
}

// 添加超时保护（但有性能开销）
engine.OnC2C(WithTimeout(slowRule, 100*time.Millisecond)).Handle(handler)
```

**注意**:
- 超时后 goroutine 仍在运行
- 每次调用都创建 goroutine
- 有性能开销

### 方案 2: 监控慢规则（推荐）

```go
// ✅ 监控规则性能
rule := MonitorRule("checkUser", func(ctx *Context) bool {
    return expensiveCheck()
}, 10*time.Millisecond)  // 超过 10ms 会警告

engine.OnC2C(rule).Handle(handler)
```

**优点**:
- 帮助发现性能问题
- 指导优化方向
- 生产环境可用

### 方案 3: 重构为 Handler（最佳）

```go
// ✅ 最佳做法：慢操作移到 Handler
engine.OnC2C().HandleE(func(ctx *Context) error {
    // Handler 中可以做慢操作
    if !expensiveCheck() {
        return errors.New("check failed")
    }
    // 继续处理
    return nil
})
```

---

## 📊 性能监控

### 添加性能指标

```go
import "github.com/prometheus/client_golang/prometheus"

var ruleDuration = prometheus.NewHistogramVec(
    prometheus.HistogramOpts{
        Name: "rule_duration_seconds",
        Help: "Rule execution duration",
    },
    []string{"rule_name"},
)

func TimedRule(name string, rule Rule) Rule {
    return func(ctx *Context) bool {
        start := time.Now()
        result := rule(ctx)
        duration := time.Since(start).Seconds()
        
        ruleDuration.WithLabelValues(name).Observe(duration)
        return result
    }
}
```

### 分析慢规则

```bash
# Prometheus 查询
# 查找平均耗时 > 10ms 的规则
rate(rule_duration_seconds_sum[5m]) / rate(rule_duration_seconds_count[5m]) > 0.01

# 查找 P99 耗时
histogram_quantile(0.99, rule_duration_seconds_bucket)
```

---

## 🎯 优化检查清单

### 规则性能检查

- [ ] 规则中没有数据库查询
- [ ] 规则中没有 HTTP 请求
- [ ] 规则中没有文件 IO
- [ ] 规则中没有复杂计算
- [ ] 规则执行时间 < 1ms
- [ ] 已缓存需要的数据
- [ ] 慢操作已移到 Handler
- [ ] 已添加性能监控

### 如果规则很慢

1. **分析原因** - 为什么慢？
2. **缓存数据** - 能否预先加载？
3. **简化逻辑** - 能否更快的算法？
4. **移到 Handler** - 能否在匹配后再做？
5. **后台更新** - 能否定期更新状态？

---

## 📚 相关文档

- [Match 方法阻塞分析](MATCH_BLOCKING_ANALYSIS.md)
- [性能优化指南](PERFORMANCE.md)
- [Middleware 最佳实践](MIDDLEWARE.md)

---

**总结**: 规则函数应该快速返回（< 1ms）。慢操作应该在 Middleware 预处理或在 Handler 中执行。
