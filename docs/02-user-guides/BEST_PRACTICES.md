# Remilia 最佳实践

> **最后更新**: 2026-02-25  
> **适用版本**: v2.0.0+

本文档总结了使用 Remilia 框架开发 QQ 机器人的最佳实践和常见模式。

## 📋 目录

1. [项目结构](#项目结构)
2. [错误处理](#错误处理)
3. [并发控制](#并发控制)
4. [资源管理](#资源管理)
5. [日志规范](#日志规范)
6. [性能优化](#性能优化)
7. [安全实践](#安全实践)
8. [测试策略](#测试策略)

---

## 1. 项目结构

### 推荐的目录结构

```
my-bot/
├── cmd/
│   └── bot/
│       └── main.go           # 入口文件
├── internal/
│   ├── handlers/             # 处理器
│   │   ├── command.go
│   │   ├── event.go
│   │   └── middleware.go
│   ├── plugins/              # 插件
│   │   ├── weather/
│   │   ├── admin/
│   │   └── utility/
│   ├── services/             # 业务逻辑
│   │   ├── weather.go
│   │   └── database.go
│   └── models/               # 数据模型
│       └── user.go
├── config/
│   ├── config.yaml           # 配置文件
│   └── config.example.yaml   # 配置示例
├── deployments/              # 部署配置
│   ├── docker/
│   └── kubernetes/
├── scripts/                  # 脚本
│   ├── build.sh
│   └── deploy.sh
├── docs/                     # 文档
├── go.mod
├── go.sum
└── README.md
```

### 模块化设计

#### ✅ DO

```go
// handlers/commands.go
package handlers

import (
    "github.com/KomeiDiSanXian/remilia/core/engine"
    "github.com/KomeiDiSanXian/remilia/openapi/dto"
)

func RegisterCommands(eng *engine.Engine) {
    eng.OnCommand(dto.GroupAtMessageCreate, "/help").Handle(HandleHelp)
    eng.OnCommand(dto.GroupAtMessageCreate, "/weather").Handle(HandleWeather)
}

// handlers/help.go
func HandleHelp(ctx *eventctx.Context) error {
    return ctx.Reply(helpText)
}
```

#### ❌ DON'T

```go
// main.go - 所有逻辑都在 main 中
func main() {
    eng := engine.NewEngine()
    
    // 100+ 行的处理器定义...
    eng.OnCommand(dto.GroupAtMessageCreate, "/help",
        func(ctx *eventctx.Context) error {
            // 大量逻辑...
            return nil
        })
}
```

---

## 2. 错误处理

### 统一错误处理

#### ✅ DO

```go
// 定义错误类型
var (
    ErrInvalidInput = errors.New("invalid input")
    ErrNotFound     = errors.New("not found")
    ErrUnauthorized = errors.New("unauthorized")
)

// 处理器中返回明确的错误
func HandleCommand(ctx *eventctx.Context) error {
    data := ctx.GetPlainText()
    if data == "" {
        return ErrInvalidInput
    }
    
    result, err := service.Process(data)
    if err != nil {
        return fmt.Errorf("process failed: %w", err)
    }
    
    return ctx.Reply(result)
}

// 使用中间件统一处理错误
func ErrorHandler() eventctx.Middleware {
    return func(next eventctx.Handler) eventctx.Handler {
        return func(ctx *eventctx.Context) error {
            err := next(ctx)
            if err != nil {
                log.WithError(err).Error("Handler failed")
                
                // 根据错误类型返回不同消息
                switch {
                case errors.Is(err, ErrInvalidInput):
                    ctx.Reply("输入错误，请检查格式")
                case errors.Is(err, ErrNotFound):
                    ctx.Reply("未找到相关信息")
                case errors.Is(err, ErrUnauthorized):
                    ctx.Reply("权限不足")
                default:
                    ctx.Reply("处理失败，请稍后重试")
                }
            }
            return err
        }
    }
}
```

#### ❌ DON'T

```go
// 忽略错误
func HandleCommand(ctx *eventctx.Context) error {
    result, _ := service.Process(data)  // ❌ 忽略错误
    ctx.Reply(result)
    return nil
}

// 使用 panic
func HandleCommand(ctx *eventctx.Context) error {
    if data == "" {
        panic("invalid input")  // ❌ 不要使用 panic
    }
    return nil
}
```

---

## 3. 并发控制

### 使用中间件限流

#### ✅ DO

```go
// 简单全局限流（推荐默认选择）
eng.Use(middleware.SimpleRateLimit(10)) // 每秒最多 10 个事件

// 按用户/Group 限流
eng.Use(middleware.RateLimitTokenBucket(2, 4, func(ctx *context.Context) string {
    if a := ctx.GetAuthor(); a != nil { return a.UserOpenID }
    return ""
}))

// 自适应限流（自动根据负载调整）
config := middleware.DefaultAdaptiveConfig()
config.MinConcurrency = 10
config.MaxConcurrency = 500

limiter := middleware.NewAdaptiveRateLimiter(config)
limiter.Start()
defer limiter.Stop()
eng.Use(limiter.Middleware())
```

> **选型建议**：
> - 简单固定限流 → `SimpleRateLimit(n)`
> - 按 key（用户/群组）限流 → `RateLimitTokenBucket`
> - 根据 CPU/内存自动调整 → `NewAdaptiveRateLimiter`

### 使用 Context 控制超时

#### ✅ DO

```go
func HandleLongTask(ctx *eventctx.Context) error {
    // 使用 context 控制超时
    taskCtx, cancel := context.WithTimeout(ctx.Context(), 10*time.Second)
    defer cancel()
    
    result := make(chan string, 1)
    go func() {
        // 执行耗时任务
        data := performLongTask()
        result <- data
    }()
    
    select {
    case data := <-result:
        return ctx.Reply(data)
    case <-taskCtx.Done():
        return ctx.Reply("处理超时，请稍后重试")
    }
}
```

### 使用 sync.WaitGroup 等待任务

#### ✅ DO

```go
func HandleBatch(ctx *eventctx.Context) error {
    var wg sync.WaitGroup
    results := make(chan string, 10)
    
    for i := 0; i < 10; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            result := processItem(id)
            results <- result
        }(i)
    }
    
    // 等待所有任务完成
    go func() {
        wg.Wait()
        close(results)
    }()
    
    // 收集结果
    var allResults []string
    for r := range results {
        allResults = append(allResults, r)
    }
    
    return ctx.Reply(strings.Join(allResults, "\n"))
}
```

---

## 4. 资源管理

### 数据库连接

#### ✅ DO

```go
// 使用连接池
type Service struct {
    db *sql.DB
}

func NewService(dsn string) (*Service, error) {
    db, err := sql.Open("mysql", dsn)
    if err != nil {
        return nil, err
    }
    
    // 配置连接池
    db.SetMaxOpenConns(25)
    db.SetMaxIdleConns(5)
    db.SetConnMaxLifetime(5 * time.Minute)
    
    return &Service{db: db}, nil
}

func (s *Service) Close() error {
    return s.db.Close()
}

// 使用 context 控制查询
func (s *Service) Query(ctx context.Context, query string) ([]Row, error) {
    rows, err := s.db.QueryContext(ctx, query)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    
    var results []Row
    for rows.Next() {
        var row Row
        if err := rows.Scan(&row); err != nil {
            return nil, err
        }
        results = append(results, row)
    }
    
    return results, rows.Err()
}
```

### HTTP 客户端

#### ✅ DO

```go
// 复用 HTTP 客户端
var httpClient = &http.Client{
    Timeout: 10 * time.Second,
    Transport: &http.Transport{
        MaxIdleConns:        100,
        MaxIdleConnsPerHost: 10,
        IdleConnTimeout:     90 * time.Second,
    },
}

func FetchData(url string) ([]byte, error) {
    resp, err := httpClient.Get(url)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    
    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
    }
    
    return io.ReadAll(resp.Body)
}
```

#### ❌ DON'T

```go
// 每次创建新的客户端
func FetchData(url string) ([]byte, error) {
    client := &http.Client{Timeout: 10 * time.Second}  // ❌ 浪费资源
    resp, _ := client.Get(url)  // ❌ 忽略错误
    defer resp.Body.Close()
    return io.ReadAll(resp.Body)
}
```

---

## 5. 日志规范

### 结构化日志

#### ✅ DO

```go
import "github.com/sirupsen/logrus"

// 使用结构化字段
func HandleCommand(ctx *eventctx.Context) error {
    log := logrus.WithFields(logrus.Fields{
        "command": "/weather",
        "user":    ctx.GetAuthor(),
        "guild":   ctx.GetGuildID(),
    })
    
    log.Info("Processing command")
    
    result, err := service.Process()
    if err != nil {
        log.WithError(err).Error("Command failed")
        return err
    }
    
    log.WithField("result_len", len(result)).Info("Command succeeded")
    return ctx.Reply(result)
}
```

### 日志级别

```go
// 设置合适的日志级别
logrus.SetLevel(logrus.InfoLevel)

// Debug - 详细的调试信息
logrus.Debug("Cache hit", "key", key)

// Info - 一般信息
logrus.Info("User logged in", "user_id", userID)

// Warn - 警告信息
logrus.Warn("Rate limit approaching", "usage", "90%")

// Error - 错误信息
logrus.WithError(err).Error("Database query failed")

// Fatal - 致命错误（会退出程序）
logrus.Fatal("Failed to start server")
```

---

## 6. 性能优化

### 使用对象池

#### ✅ DO

```go
// 复用频繁创建的对象
var bufferPool = sync.Pool{
    New: func() interface{} {
        return new(bytes.Buffer)
    },
}

func ProcessData(data string) string {
    buf := bufferPool.Get().(*bytes.Buffer)
    defer bufferPool.Put(buf)
    
    buf.Reset()
    buf.WriteString(data)
    // ... 处理
    
    return buf.String()
}
```

### 批量处理

#### ✅ DO

```go
// 批量处理消息
type MessageBatcher struct {
    messages chan *Message
    batch    []*Message
    mu       sync.Mutex
}

func (mb *MessageBatcher) Add(msg *Message) {
    mb.messages <- msg
}

func (mb *MessageBatcher) Start() {
    ticker := time.NewTicker(1 * time.Second)
    
    for {
        select {
        case msg := <-mb.messages:
            mb.batch = append(mb.batch, msg)
            
            if len(mb.batch) >= 100 {
                mb.flush()
            }
            
        case <-ticker.C:
            if len(mb.batch) > 0 {
                mb.flush()
            }
        }
    }
}

func (mb *MessageBatcher) flush() {
    // 批量处理
    processBatch(mb.batch)
    mb.batch = mb.batch[:0]
}
```

### 缓存策略

#### ✅ DO

```go
import "github.com/patrickmn/go-cache"

// 使用缓存减少重复计算
var dataCache = cache.New(5*time.Minute, 10*time.Minute)

func GetData(key string) (string, error) {
    // 尝试从缓存获取
    if cached, found := dataCache.Get(key); found {
        return cached.(string), nil
    }
    
    // 缓存未命中，执行计算
    data, err := expensiveOperation(key)
    if err != nil {
        return "", err
    }
    
    // 存入缓存
    dataCache.Set(key, data, cache.DefaultExpiration)
    
    return data, nil
}
```

---

## 7. 安全实践

### 输入验证

#### ✅ DO

```go
// 验证和清理用户输入
func HandleCommand(ctx *eventctx.Context) error {
    input := ctx.GetPlainText()
    
    // 长度检查
    if len(input) > 1000 {
        return ctx.Reply("输入过长")
    }
    
    // 格式验证
    if !isValidFormat(input) {
        return ctx.Reply("格式错误")
    }
    
    // SQL 注入防护 - 使用参数化查询
    _, err := db.Exec("SELECT * FROM users WHERE name = ?", input)
    
    return err
}

// XSS 防护 - 转义输出
func SafeReply(ctx *eventctx.Context, text string) error {
    escaped := html.EscapeString(text)
    return ctx.Reply(escaped)
}
```

### 权限控制

#### ✅ DO

```go
// 定义权限检查中间件
func RequireAdmin() eventctx.Middleware {
    return func(next eventctx.Handler) eventctx.Handler {
        return func(ctx *eventctx.Context) error {
            userID := ctx.GetAuthor()
            
            if !isAdmin(userID) {
                return ctx.Reply("权限不足")
            }
            
            return next(ctx)
        }
    }
}

// 使用
eng.Use(RequireAdmin())
eng.OnCommand("/admin", handleAdminCommand)
```

### 敏感信息保护

#### ✅ DO

```go
// 不要硬编码敏感信息
// ❌ token := "abc123"

// ✅ 使用环境变量
token := os.Getenv("BOT_TOKEN")

// ✅ 使用配置文件（不提交到版本控制）
config, _ := config.Load("config.yaml")
token := config.Bot.Token

// 日志中隐藏敏感信息
func logRequest(req *http.Request) {
    log.Info("Request",
        "url", req.URL.String(),
        "token", "***",  // ✅ 隐藏 token
    )
}
```

---

## 8. 测试策略

### 单元测试

#### ✅ DO

```go
package handlers

import (
    "testing"
    
    eventctx "github.com/KomeiDiSanXian/remilia/core/context"
    "github.com/KomeiDiSanXian/remilia/openapi/dto"
    "github.com/stretchr/testify/assert"
)

func TestHandleHelp(t *testing.T) {
    // 创建测试上下文
    event := &dto.Payload{
        ID:   "test-id",
        Type: dto.EventTypeAtMessageCreate,
    }
    ctx := eventctx.NewContext(event, nil)
    
    // 执行处理器
    err := HandleHelp(ctx)
    
    // 断言
    assert.NoError(t, err)
    // 验证回复内容...
}

func TestServiceWithMock(t *testing.T) {
    // 使用 mock 隔离外部依赖
    mockDB := &MockDatabase{}
    service := NewService(mockDB)
    
    result, err := service.Query("test")
    
    assert.NoError(t, err)
    assert.Equal(t, "expected", result)
}
```

### 集成测试

```go
func TestBotIntegration(t *testing.T) {
    // 创建测试 Bot
    eng := engine.NewEngine()
    RegisterHandlers(eng)
    
    adapter := qq.NewWebhookServerAdapter(":0", &dto.BotInfo{AppID: 123456})
    bot := remilia.NewBot(adapter, eng)
    
    // 启动
    err := bot.Start()
    assert.NoError(t, err)
    defer bot.Shutdown()
    
    // 发送测试消息
    // ... 测试逻辑
}
```

### Benchmark 测试

```go
func BenchmarkHandleCommand(b *testing.B) {
    ctx := createTestContext()
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        HandleCommand(ctx)
    }
}
```

---

## 9. Context 数据操作

### ctx.Set / ctx.Delete 行为差异

```go
// ✅ 正确删除 key
ctx.Delete("session")

// ❌ 不会删除——ctx.Set(key, nil) 是 no-op，静默忽略
ctx.Set("session", nil)
```

> `ctx.Set(key, nil)` 被设计为 no-op 是为了防止误删保留 key（框架内部 key 以
> `_remilia_internal_` 为前缀），需要显式删除时必须调用 `ctx.Delete`。

### 跨 Handler 传递数据

```go
// 在中间件中注入数据
func AuthMiddleware() eventctx.Middleware {
    return func(next eventctx.Handler) eventctx.Handler {
        return func(ctx *eventctx.Context) error {
            user := resolveUser(ctx)
            ctx.Set("current_user", user)  // 注入
            return next(ctx)
        }
    }
}

// 在 Handler 中读取
func HandleCommand(ctx *eventctx.Context) error {
    user, ok := ctx.Get("current_user")
    if !ok {
        return ctx.Reply("未登录")
    }
    u := user.(*User)
    return ctx.Reply("你好, " + u.Name)
}
```

---

## 10. 插件开发

使用 v2 `Descriptor` API，无需继承：

```go
func New() *plugin.Descriptor {
    p := &MyPlugin{}
    return &plugin.Descriptor{

        Meta: &plugin.Metadata{
            Description: "示例插件",
            Category:    "工具",
        },
        Setup: func(ctx *plugin.SetupContext) (any, error) {
            ctx.Reg.RegisterCommand(dto.GroupAtMessageCreate, "/cmd").
                Handle(p.handle)
            return p, nil
        },
        Teardown: func(ctx *plugin.TeardownContext) error {
            ctx.Log.Info("stopped")
            return nil
        },
    }
}
```

详细指南：[插件开发最佳实践](../../04-development/plugin-best-practices.md)

---

## 💡 其他建议

### 1. 使用依赖注入

```go
type Bot struct {
    engine  *engine.Engine
    db      *Database
    cache   *Cache
    logger  *logrus.Logger
}

func NewBot(eng *engine.Engine, db *Database, cache *Cache) *Bot {
    return &Bot{
        engine: eng,
        db:     db,
        cache:  cache,
        logger: logrus.StandardLogger(),
    }
}
```

### 2. 配置分离

```go
// config/config.go
type Config struct {
    Bot        BotConfig
    Database   DatabaseConfig
    Redis      RedisConfig
    Middleware MiddlewareConfig
}

func Load(path string) (*Config, error) {
    // 加载配置...
}
```

### 3. 优雅关闭

```go
func main() {
    bot := setupBot()
    
    // 捕获信号
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
    
    // 启动
    bot.Start()
    
    // 等待信号
    <-sigCh
    
    // 优雅关闭
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    
    if err := bot.ShutdownWithContext(ctx); err != nil {
        log.Error("Shutdown failed:", err)
    }
}
```

### 4. 监控和告警

```go
// 暴露健康检查端点
http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
    if bot.IsHealthy() {
        w.WriteHeader(http.StatusOK)
        w.Write([]byte("OK"))
    } else {
        w.WriteHeader(http.StatusServiceUnavailable)
    }
})

// 暴露 Prometheus 指标
http.Handle("/metrics", promhttp.Handler())
```

---

## 📚 相关文档

- [快速上手](../01-getting-started/GETTING_STARTED.md)
- [故障排查](../01-getting-started/TROUBLESHOOTING.md)

---

**记住**: 代码质量比功能数量更重要！
