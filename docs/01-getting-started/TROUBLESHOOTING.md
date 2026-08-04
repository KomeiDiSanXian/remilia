# Remilia 故障排查指南

> **最后更新**: 2026-08-04  


本指南帮助你诊断和解决使用 Remilia 时可能遇到的常见问题。

## 📋 目录

1. [启动问题](#启动问题)
2. [连接问题](#连接问题)
3. [消息处理问题](#消息处理问题)
4. [性能问题](#性能问题)
5. [内存泄漏](#内存泄漏)
6. [并发问题](#并发问题)
7. [配置问题](#配置问题)
8. [调试技巧](#调试技巧)

---

## 1. 启动问题

### 问题：Bot 启动失败

**症状**:
```
FATAL Failed to start bot error="..."
```

**可能原因**:
1. 端口已被占用
2. 配置错误
3. 依赖服务不可用

**解决方案**:

#### 检查端口占用
```bash
# Linux/macOS
lsof -i :8080

# Windows
netstat -ano | findstr :8080
```

#### 检查配置
```go
// 添加详细日志
logger.SetLevel("debug")

// 验证配置
config, err := config.Load("config.yaml")
if err != nil {
    logger.Fatal("Config error: " + err.Error())
}
logger.Infof("Config loaded: %+v", config)
```

#### 测试依赖服务
```go
// 测试数据库连接
db, err := sql.Open("mysql", dsn)
if err != nil {
    log.Fatal("Database connection failed:", err)
}
if err := db.Ping(); err != nil {
    log.Fatal("Database ping failed:", err)
}
```

---

## 2. 连接问题

### 问题：Webhook 无法接收消息

**症状**:
- Bot 启动正常
- 但没有收到任何消息

**诊断步骤**:

#### 1. 检查 Webhook 配置
```bash
# 测试 Webhook 端点
curl -X POST http://localhost:8080/webhook \
  -H "Content-Type: application/json" \
  -d '{"test": "data"}'
```

#### 2. 检查防火墙
```bash
# Linux - 检查防火墙规则
sudo iptables -L

# 开放端口
sudo ufw allow 8080
```

#### 3. 检查签名验证
```go
// 临时禁用签名验证进行测试
adapter := qq.SimpleWebhookAdapter(8080)

// 添加日志
eng.Use(func(next eventctx.Handler) eventctx.Handler {
    return func(ctx *eventctx.Context) error {
        log.Info("Received event:", ctx.GetEventType())
        return next(ctx)
    }
})
```

#### 4. 使用 ngrok 进行本地测试
```bash
# 安装 ngrok
# 运行
ngrok http 8080

# 使用 ngrok 提供的 URL 配置 Webhook
```

---

## 3. 消息处理问题

### 问题：命令无响应

**症状**:
- 发送命令后没有回复
- 日志显示收到消息

**诊断**:

#### 检查 Matcher 是否注册
```go
// 添加调试日志
eng.OnCommand(eventctx.EventGroup, "/test").Handle(func(ctx *eventctx.Context) error {
    log.Info("Test command received")
    ctx.Reply(platform.TextMessage("Test reply"))
    return nil
})

// 打印 Matcher 数量
log.Info("Matcher count:", eng.GetMatcherCount())
```

#### 检查命令格式
```go
// 确保命令格式正确
text := ctx.GetMessageContent()
log.Info("Received text:", text)

// 使用命令提取
cmd := command.ExtractCommandFast(text)
log.Info("Extracted command:", cmd)
```

#### 检查中间件拦截
```go
// 临时移除所有中间件
eng := engine.NewEngine()
// eng.Use(...)  // 注释掉
eng.OnCommand("/test", handler)
```

### 问题：消息丢失

**症状**:
- 部分消息没有被处理

**可能原因**:
1. 并发限制导致丢弃
2. 超时导致放弃
3. Panic 导致中断

**解决方案**:

#### 检查并发限制
```go
// 背压：超过上限丢弃
eng.Use(middleware.Backpressure(100, middleware.BackpressureDrop, 100*time.Millisecond))

// 添加监控
var dropped atomic.Int64
eng.Use(func(next eventctx.Handler) eventctx.Handler {
    return func(ctx *eventctx.Context) error {
        err := next(ctx)
        if err != nil && strings.Contains(err.Error(), "concurrency limit") {
            dropped.Add(1)
            log.Warn("Message dropped due to concurrency limit")
        }
        return err
    }
})
```

#### 检查超时设置
```go
// 增加超时时间
eng.Use(middleware.Timeout(30 * time.Second))  // 从 5s 增加到 30s

// 监控超时
var timeouts atomic.Int64
eng.Use(func(next eventctx.Handler) eventctx.Handler {
    return func(ctx *eventctx.Context) error {
        err := next(ctx)
        if err != nil && strings.Contains(err.Error(), "timeout") {
            timeouts.Add(1)
            log.Warn("Handler timeout")
        }
        return err
    }
})
```

---

## 4. 性能问题

### 问题：响应延迟高

**症状**:
- 消息处理缓慢
- CPU 使用率高

**诊断工具**:

#### 1. 使用 pprof 分析
```go
import _ "net/http/pprof"

// 启动 pprof 服务器
go func() {
    log.Println(http.ListenAndServe("localhost:6060", nil))
}()
```

访问 http://localhost:6060/debug/pprof/

分析 CPU:
```bash
go tool pprof http://localhost:6060/debug/pprof/profile?seconds=30
```

分析内存:
```bash
go tool pprof http://localhost:6060/debug/pprof/heap
```

#### 2. 添加性能监控
```go
eng.Use(func(next eventctx.Handler) eventctx.Handler {
    return func(ctx *eventctx.Context) error {
        start := time.Now()
        err := next(ctx)
        duration := time.Since(start)
        
        if duration > 100*time.Millisecond {
            logger.WithFields(logger.Fields{
                "duration": duration,
                "type":     ctx.GetEventType(),
            }).Warn("Slow handler")
        }
        
        return err
    }
})
```

#### 3. 检查数据库查询
```go
// 记录慢查询
func (s *Service) Query(query string) ([]Row, error) {
    start := time.Now()
    defer func() {
        duration := time.Since(start)
        if duration > 100*time.Millisecond {
            log.Warn("Slow query:",
                "query", query,
                "duration", duration)
        }
    }()
    
    return s.db.Query(query)
}
```

**优化方案**:

1. **添加缓存**
```go
var cache = cache.New(5*time.Minute, 10*time.Minute)

func GetData(key string) (string, error) {
    if cached, found := cache.Get(key); found {
        return cached.(string), nil
    }
    
    data, err := fetchData(key)
    if err != nil {
        return "", err
    }
    
    cache.Set(key, data, cache.DefaultExpiration)
    return data, nil
}
```

2. **使用连接池**
```go
db.SetMaxOpenConns(25)
db.SetMaxIdleConns(5)
db.SetConnMaxLifetime(5 * time.Minute)
```

3. **并行处理**
```go
// 使用 goroutine 并行处理
var wg sync.WaitGroup
for _, item := range items {
    wg.Add(1)
    go func(i Item) {
        defer wg.Done()
        process(i)
    }(item)
}
wg.Wait()
```

---

## 5. 内存泄漏

### 问题：内存持续增长

**症状**:
- 内存使用率持续上升
- 最终导致 OOM

**诊断**:

#### 1. 检查内存使用
```go
import "runtime"

func printMemStats() {
    var m runtime.MemStats
    runtime.ReadMemStats(&m)
    
    log.Printf("Memory Stats:")
    log.Printf("  Alloc: %d MB", m.Alloc/1024/1024)
    log.Printf("  TotalAlloc: %d MB", m.TotalAlloc/1024/1024)
    log.Printf("  Sys: %d MB", m.Sys/1024/1024)
    log.Printf("  NumGC: %d", m.NumGC)
}

// 定期打印
go func() {
    ticker := time.NewTicker(1 * time.Minute)
    for range ticker.C {
        printMemStats()
    }
}()
```

#### 2. 使用 pprof 分析内存
```bash
# 生成堆内存快照
curl http://localhost:6060/debug/pprof/heap > heap.out

# 分析
go tool pprof heap.out

# 交互式命令
(pprof) top
(pprof) list <function_name>
```

**常见原因**:

1. **Goroutine 泄漏**
```go
// ❌ 错误：goroutine 永不退出
func BadHandler(ctx *eventctx.Context) error {
    go func() {
        for {
            // 无退出条件
            time.Sleep(1 * time.Second)
        }
    }()
    return nil
}

// ✅ 正确：使用 context 控制
func GoodHandler(ctx *eventctx.Context) error {
    taskCtx, cancel := context.WithCancel(ctx.Context())
    defer cancel()
    
    go func() {
        ticker := time.NewTicker(1 * time.Second)
        defer ticker.Stop()
        
        for {
            select {
            case <-ticker.C:
                // 执行任务
            case <-taskCtx.Done():
                return  // 退出 goroutine
            }
        }
    }()
    return nil
}
```

2. **资源未释放**
```go
// ❌ 错误：连接未关闭
func BadFetch() error {
    resp, _ := http.Get(url)
    return processResponse(resp)
}

// ✅ 正确：确保关闭
func GoodFetch() error {
    resp, err := http.Get(url)
    if err != nil {
        return err
    }
    defer resp.Body.Close()
    
    return processResponse(resp)
}
```

3. **无限增长的缓存**
```go
// ❌ 错误：缓存无限增长
var cache = make(map[string]string)

// ✅ 正确：使用带过期的缓存
var cache = cache.New(5*time.Minute, 10*time.Minute)
```

---

## 6. 并发问题

### 问题：数据竞争

**症状**:
- 运行时 panic
- 数据不一致
- 使用 `-race` 检测到竞争

**诊断**:

```bash
# 使用 race detector
go run -race main.go

# 或编译时启用
go build -race -o bot main.go
```

**解决方案**:

#### 1. 使用互斥锁
```go
type SafeCounter struct {
    mu    sync.Mutex
    count int
}

func (c *SafeCounter) Inc() {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.count++
}

func (c *SafeCounter) Value() int {
    c.mu.Lock()
    defer c.mu.Unlock()
    return c.count
}
```

#### 2. 使用原子操作
```go
var counter atomic.Int64

func inc() {
    counter.Add(1)
}

func value() int64 {
    return counter.Load()
}
```

#### 3. 使用 sync.Map
```go
var cache sync.Map

func set(key, value string) {
    cache.Store(key, value)
}

func get(key string) (string, bool) {
    val, ok := cache.Load(key)
    if !ok {
        return "", false
    }
    return val.(string), true
}
```

---

## 7. 配置问题

### 问题：配置加载失败

**诊断**:

```go
// 添加详细日志
config, err := config.Load("config.yaml")
if err != nil {
    log.Fatal("Failed to load config:", err)
}

// 打印配置
configJSON, _ := json.MarshalIndent(config, "", "  ")
log.Info("Loaded config:\n", string(configJSON))
```

### 问题：环境变量不生效

**检查**:

```go
// 打印所有环境变量
for _, env := range os.Environ() {
    log.Println(env)
}

// 检查特定变量
token := os.Getenv("BOT_TOKEN")
log.Info("BOT_TOKEN:", token)
```

---

## 8. 调试技巧

### 启用详细日志

```go
// 设置 Debug 级别
logger.SetLevel("debug")
```

### 添加请求追踪

```go
// 使用 RequestID 中间件
eng.Use(middleware.RequestID())

// 在处理器中获取
eng.On("", context.OnAny()).Handle(func(ctx *eventctx.Context) error {
    requestID, _ := ctx.Get(middleware.CtxKeyRequestID)
    l := logger.WithField("request_id", requestID)
    
    l.Info("Processing message")
    // ...
    return nil
})
```

### 使用断点调试

```go
// 在关键位置添加调试点
func HandleCommand(ctx *eventctx.Context) error {
    // 打印所有相关信息
    log.Debug("Handler called",
        "type", ctx.GetEventType(),
        "sender_id", ctx.GetSenderInfo().ID,
        "content", ctx.GetMessageContent())
    
    // 继续处理...
    return nil
}
```

### 生成诊断报告

```go
func generateDiagnostics() string {
    var buf bytes.Buffer
    
    // 系统信息
    fmt.Fprintf(&buf, "=== System Info ===\n")
    fmt.Fprintf(&buf, "GOOS: %s\n", runtime.GOOS)
    fmt.Fprintf(&buf, "GOARCH: %s\n", runtime.GOARCH)
    fmt.Fprintf(&buf, "NumCPU: %d\n", runtime.NumCPU())
    fmt.Fprintf(&buf, "NumGoroutine: %d\n", runtime.NumGoroutine())
    
    // 内存信息
    var m runtime.MemStats
    runtime.ReadMemStats(&m)
    fmt.Fprintf(&buf, "\n=== Memory ===\n")
    fmt.Fprintf(&buf, "Alloc: %d MB\n", m.Alloc/1024/1024)
    fmt.Fprintf(&buf, "TotalAlloc: %d MB\n", m.TotalAlloc/1024/1024)
    fmt.Fprintf(&buf, "Sys: %d MB\n", m.Sys/1024/1024)
    fmt.Fprintf(&buf, "NumGC: %d\n", m.NumGC)
    
    // 中间件统计
    fmt.Fprintf(&buf, "\n=== Middleware Stats ===\n")
    // 添加各种统计...
    
    return buf.String()
}

// 暴露诊断端点
http.HandleFunc("/debug/diagnostics", func(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "text/plain")
    w.Write([]byte(generateDiagnostics()))
})
```

---

## 🆘 获取帮助

如果以上方法都无法解决你的问题：

1. **查看文档**
   - [快速上手](./GETTING_STARTED.md)
   - [最佳实践](../02-user-guides/BEST_PRACTICES.md)

2. **搜索 Issues**
   - https://github.com/KomeiDiSanXian/remilia/issues

3. **提交 Issue**
   - 提供详细的错误信息
   - 包含可复现的示例代码
   - 附上相关日志

4. **社区讨论**
   - GitHub Discussions
   - QQ 群

---

## 📝 问题报告模板

```markdown
## 环境信息
- Go 版本: 
- Remilia 版本: 
- 操作系统: 

## 问题描述
简要描述问题...

## 复现步骤
1. 
2. 
3. 

## 预期行为
应该...

## 实际行为
但是...

## 错误日志
```
日志内容...
```

## 相关代码
```go
代码片段...
```
```

---

**记住**: 良好的日志是最好的调试工具！
