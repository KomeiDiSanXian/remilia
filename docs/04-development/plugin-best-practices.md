# 插件开发最佳实践

本文档提供插件开发的最佳实践和规范，帮助开发者创建高质量、可维护的插件。

---

## 1. 插件依赖声明

### ✅ 推荐做法

**在元数据中声明依赖，无需手动实现 `Dependencies()` 方法**

```go
func New() *Plugin {
    metadata := &plugin.Metadata{
        Name:         "admin",
        Version:      "1.0.0",
        Description:  "管理插件",
        Category:     "系统",
        Dependencies: []string{"permission", "storage"}, // ✅ 在这里声明依赖
    }
    
    return &Plugin{
        BasePlugin: plugin.NewBasePluginWithMetadata(metadata),
    }
}

// ✅ 不需要手动实现 Dependencies() 方法
// BasePlugin 会自动从元数据中读取依赖
```

**在结构体字段中添加注释说明依赖关系**

```go
type Plugin struct {
    *plugin.BasePlugin
    
    // 依赖的插件字段，添加注释说明
    permPlugin *permission.Plugin    // 权限插件依赖 (depends on: permission)
    storage    *storage.Plugin       // 存储插件依赖 (depends on: storage)
    
    // 非插件依赖字段（不需要在 Dependencies 中声明）
    pluginManager *plugin.Manager    // 插件管理器引用（由管理器注入）
    engine        *engine.Engine     // Engine引用（Load时传入）
}
```

### ❌ 避免做法

**不要手动实现 `Dependencies()` 方法**

```go
// ❌ 避免这样做！元数据中已经声明，不需要重复
func (p *Plugin) Dependencies() []string {
    return []string{"permission"}
}
```

**不要依赖名称与插件名称不一致**

```go
// ❌ 避免
Dependencies: []string{"perm"}  // 插件名是 permission，不是 perm
```

**不要忘记更新依赖声明**

```go
type Plugin struct {
    *plugin.BasePlugin
    newPlugin *other.Plugin  // ❌ 添加了依赖但未在元数据中声明
}

// 正确做法：在元数据中添加
metadata := &plugin.Metadata{
    Dependencies: []string{"other"},  // ✅
}
```

---

## 2. 插件命令注册

### ✅ 推荐做法

**使用 `BasePlugin.OnCommand()` 等便捷方法自动管理 Matcher**

```go
func (p *Plugin) Load(eng *engine.Engine) error {
    // ✅ 使用 OnCommand，自动调用 AddMatcher
    p.OnCommand(eng, dto.C2CMessageCreate, "/help").
        Handle(p.handleHelp)
    
    // ✅ 使用 On，自动调用 AddMatcher
    p.On(eng, dto.GroupAtMessageCreate, 
        context.CommandRule("/status")).
        Handle(p.handleStatus)
    
    return nil
}
```

**为命令设置完整的定义信息**

```go
cmdDef := &command.Definition{
    Name:        "help",
    Description: "显示帮助信息",
    Usage:       "/help [命令名]",
    Category:    "系统",
    Examples:    []string{"/help", "/help weather"},
    Arguments: []*command.Argument{
        {
            Name:        "command",
            Type:        command.ArgTypeString,
            Description: "命令名称",
            Required:    false,
        },
    },
}

p.OnCommand(eng, dto.C2CMessageCreate, "/help").
    SetDefinition(cmdDef).  // ✅ 设置定义
    Handle(p.handleHelp)
```

### ❌ 避免做法

**不要直接使用 `eng.OnCommand()` 而忘记 `AddMatcher()`**

```go
// ❌ 避免这样做！卸载时无法清理
matcher := eng.OnCommand(dto.C2CMessageCreate, "/help").
    Handle(p.handleHelp)
// 忘记调用 p.AddMatcher(matcher)

// ✅ 正确做法：使用 p.OnCommand()
p.OnCommand(eng, dto.C2CMessageCreate, "/help").
    Handle(p.handleHelp)
```

---

## 3. 插件元数据

### ✅ 推荐做法

**提供完整的元数据信息**

```go
metadata := &plugin.Metadata{
    // 基本信息（必填）
    Name:        "weather",
    Version:     "1.0.0",
    Author:      "Your Name",
    Description: "天气查询插件",
    
    // 分类和标签（推荐）
    Category:    "生活",
    Tags:        []string{"天气", "生活", "查询"},
    
    // 帮助文本（强烈推荐）
    HelpText: `天气插件使用说明：
  /weather <城市> - 查询指定城市的天气
  
示例：
  /weather 北京
  /weather 上海`,
    
    // 依赖关系（如有）
    Dependencies: []string{"httpclient", "cache"},
    
    // 可见性（可选）
    Hidden: false,  // 是否在帮助列表中隐藏
    
    // 联系方式（可选）
    Homepage:   "https://example.com/weather",
    Repository: "https://github.com/example/weather-plugin",
}
```

**帮助文本应清晰、结构化**

```go
HelpText: `插件使用说明：

基础命令：
  /cmd1 <参数> - 命令1的说明
  /cmd2 [可选参数] - 命令2的说明

高级功能：
  /cmd3 --option - 命令3的说明

示例：
  /cmd1 hello
  /cmd2 world
  /cmd3 --verbose

注意事项：
- 注意事项1
- 注意事项2`,
```

### ❌ 避免做法

**不要提供不完整或不准确的元数据**

```go
// ❌ 元数据太简单
metadata := &plugin.Metadata{
    Name: "myplugin",
}

// ❌ 帮助文本不清晰
HelpText: "使用 /help 查看帮助"  // 应该直接说明用法
```

---

## 4. 插件生命周期

### ✅ 推荐做法

**正确实现生命周期方法**

```go
// Load - 初始化资源
func (p *Plugin) Load(eng *engine.Engine) error {
    logger.Info("[Plugin] Loading plugin...")
    
    // 1. 初始化插件状态
    p.engine = eng
    
    // 2. 注册命令（使用 OnCommand 等便捷方法）
    p.OnCommand(eng, dto.C2CMessageCreate, "/cmd").
        Handle(p.handleCmd)
    
    // 3. 启动后台任务（如果需要）
    go p.backgroundTask()
    
    logger.Info("[Plugin] Plugin loaded successfully")
    return nil
}

// Unload - 清理资源
func (p *Plugin) Unload(eng *engine.Engine) error {
    logger.Info("[Plugin] Unloading plugin...")
    
    // 1. 停止后台任务
    close(p.stopChan)
    
    // 2. 清理匹配器（BasePlugin 会自动处理）
    // 调用父类的 Unload
    return p.BasePlugin.Unload(eng)
}
```

**对于有后台任务的插件，提供优雅关闭**

```go
type Plugin struct {
    *plugin.BasePlugin
    stopChan chan struct{}  // 用于停止后台任务
    wg       sync.WaitGroup // 用于等待任务结束
}

func (p *Plugin) Load(eng *engine.Engine) error {
    p.stopChan = make(chan struct{})
    
    p.wg.Add(1)
    go p.backgroundTask()
    
    return nil
}

func (p *Plugin) backgroundTask() {
    defer p.wg.Done()
    
    ticker := time.NewTicker(5 * time.Minute)
    defer ticker.Stop()
    
    for {
        select {
        case <-ticker.C:
            // 执行定期任务
            p.doWork()
        case <-p.stopChan:
            logger.Info("[Plugin] Background task stopped")
            return
        }
    }
}

func (p *Plugin) Unload(eng *engine.Engine) error {
    // 停止后台任务
    close(p.stopChan)
    
    // 等待任务结束
    p.wg.Wait()
    
    return p.BasePlugin.Unload(eng)
}
```

### ❌ 避免做法

**不要在 Load 中执行耗时操作**

```go
// ❌ 避免
func (p *Plugin) Load(eng *engine.Engine) error {
    // 不要在这里执行耗时操作
    time.Sleep(10 * time.Second)  // ❌
    
    // 不要在这里等待外部服务
    p.waitForExternalService()    // ❌
    
    return nil
}

// ✅ 正确做法：异步初始化
func (p *Plugin) Load(eng *engine.Engine) error {
    go p.asyncInit()  // ✅ 异步初始化
    return nil
}
```

**不要忘记清理资源**

```go
// ❌ 避免
func (p *Plugin) Unload(eng *engine.Engine) error {
    // 忘记停止后台任务
    // 忘记关闭连接
    // 忘记释放资源
    return nil
}
```

---

## 5. 错误处理

### ✅ 推荐做法

**使用统一的错误处理**

```go
func (p *Plugin) handleCommand(ctx *context.Context) error {
    // 使用 errutil 包装错误
    result, err := p.doSomething()
    if err != nil {
        return errutil.WrapErrorf(err, "failed to do something")
    }
    
    // 返回用户友好的错误信息
    if result == nil {
        return p.reply(ctx, "❌ 操作失败：未找到数据")
    }
    
    return p.reply(ctx, "✅ 操作成功")
}
```

**记录详细的日志**

```go
func (p *Plugin) processData(data string) error {
    logger.Debugf("[Plugin] Processing data: %s", data)
    
    result, err := p.parse(data)
    if err != nil {
        logger.WithError(err).Errorf("[Plugin] Failed to parse data: %s", data)
        return err
    }
    
    logger.Infof("[Plugin] Successfully processed data, result: %v", result)
    return nil
}
```

### ❌ 避免做法

**不要忽略错误**

```go
// ❌ 避免
result, _ := p.doSomething()  // 忽略错误

// ✅ 正确处理错误
result, err := p.doSomething()
if err != nil {
    logger.WithError(err).Error("[Plugin] Operation failed")
    return err
}
```

**不要使用 panic**

```go
// ❌ 避免
func (p *Plugin) handleCommand(ctx *context.Context) error {
    if ctx == nil {
        panic("context is nil")  // ❌ 不要使用 panic
    }
    return nil
}

// ✅ 返回错误
func (p *Plugin) handleCommand(ctx *context.Context) error {
    if ctx == nil {
        return errutil.NewPluginError(p.Name(), "context is nil")
    }
    return nil
}
```

---

## 6. 线程安全

### ✅ 推荐做法

**使用互斥锁保护共享状态**

```go
type Plugin struct {
    *plugin.BasePlugin
    mu    sync.RWMutex
    cache map[string]string
}

func (p *Plugin) Get(key string) (string, bool) {
    p.mu.RLock()
    defer p.mu.RUnlock()
    
    val, ok := p.cache[key]
    return val, ok
}

func (p *Plugin) Set(key, value string) {
    p.mu.Lock()
    defer p.mu.Unlock()
    
    p.cache[key] = value
}
```

**使用 sync.Once 进行一次性初始化**

```go
type Plugin struct {
    *plugin.BasePlugin
    initOnce sync.Once
    client   *http.Client
}

func (p *Plugin) getClient() *http.Client {
    p.initOnce.Do(func() {
        p.client = &http.Client{
            Timeout: 10 * time.Second,
        }
    })
    return p.client
}
```

### ❌ 避免做法

**不要在没有锁保护的情况下访问共享状态**

```go
// ❌ 避免
type Plugin struct {
    cache map[string]string  // 并发访问不安全
}

func (p *Plugin) Get(key string) string {
    return p.cache[key]  // ❌ 数据竞争
}
```

---

## 7. 测试

### ✅ 推荐做法

**为插件编写单元测试**

```go
func TestPlugin_Load(t *testing.T) {
    // 创建测试环境
    eng := engine.New()
    plugin := New()
    
    // 测试加载
    err := plugin.Load(eng)
    assert.NoError(t, err)
    
    // 验证状态
    assert.Equal(t, plugin.GetState(), plugin.Loaded)
}

func TestPlugin_HandleCommand(t *testing.T) {
    // 创建模拟上下文
    ctx := createMockContext()
    plugin := New()
    
    // 测试命令处理
    err := plugin.handleCommand(ctx)
    assert.NoError(t, err)
}
```

**使用表驱动测试**

```go
func TestPlugin_ParseInput(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    string
        wantErr bool
    }{
        {"valid input", "hello", "HELLO", false},
        {"empty input", "", "", true},
        {"special chars", "hello@world", "HELLO@WORLD", false},
    }
    
    plugin := New()
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := plugin.parseInput(tt.input)
            if tt.wantErr {
                assert.Error(t, err)
            } else {
                assert.NoError(t, err)
                assert.Equal(t, tt.want, got)
            }
        })
    }
}
```

---

## 8. 性能优化

### ✅ 推荐做法

**缓存频繁访问的数据**

```go
type Plugin struct {
    *plugin.BasePlugin
    cache *cache.Plugin  // 使用缓存插件
}

func (p *Plugin) GetData(key string) (string, error) {
    // 先查缓存
    if data, ok := p.cache.Get(key); ok {
        return string(data), nil
    }
    
    // 缓存未命中，从数据源获取
    data, err := p.fetchFromSource(key)
    if err != nil {
        return "", err
    }
    
    // 写入缓存
    p.cache.Set(key, []byte(data), 5*time.Minute)
    
    return data, nil
}
```

**使用 goroutine 池限制并发**

```go
type Plugin struct {
    *plugin.BasePlugin
    workerPool chan struct{}
}

func New() *Plugin {
    return &Plugin{
        workerPool: make(chan struct{}, 10),  // 最多10个并发
    }
}

func (p *Plugin) processAsync(task func()) {
    p.workerPool <- struct{}{}  // 获取令牌
    go func() {
        defer func() { <-p.workerPool }()  // 释放令牌
        task()
    }()
}
```

### ❌ 避免做法

**不要在热路径上进行重复计算**

```go
// ❌ 避免
func (p *Plugin) handleCommand(ctx *context.Context) error {
    // 每次都解析配置（应该缓存）
    config := p.parseConfig()
    
    // 每次都编译正则表达式（应该预编译）
    re := regexp.MustCompile(`pattern`)
    
    return nil
}
```

**不要创建无限制的 goroutine**

```go
// ❌ 避免
func (p *Plugin) processMany(items []string) {
    for _, item := range items {
        go p.process(item)  // 可能创建成千上万的 goroutine
    }
}
```

---

## 9. 配置管理

### ✅ 推荐做法

**使用配置插件接口**

```go
func (p *Plugin) Load(eng *engine.Engine) error {
    // 获取配置
    config := p.GetConfig()
    
    // 读取配置项并提供默认值
    timeout := config.GetDuration("timeout", 30*time.Second)
    maxRetries := config.GetInt("max_retries", 3)
    enabled := config.GetBool("enabled", true)
    
    logger.Infof("[Plugin] Config: timeout=%v, retries=%d, enabled=%v",
        timeout, maxRetries, enabled)
    
    return nil
}
```

**监听配置变化**

```go
func (p *Plugin) Load(eng *engine.Engine) error {
    config := p.GetConfig()
    
    // 监听配置变化
    config.OnChange(func(key string, oldVal, newVal any) {
        logger.Infof("[Plugin] Config changed: %s = %v -> %v", key, oldVal, newVal)
        p.onConfigChange(key, newVal)
    })
    
    return nil
}
```

---

## 10. 文档

### ✅ 推荐做法

**提供清晰的文档注释**

```go
// Plugin 天气查询插件
//
// 功能：
//   - 查询城市天气
//   - 支持多种天气源
//   - 自动缓存查询结果
//
// 依赖：
//   - httpclient: 用于HTTP请求
//   - cache: 用于缓存天气数据
//
// 配置项：
//   - api_key: 天气API密钥（必填）
//   - cache_ttl: 缓存时间（默认30分钟）
//   - timeout: 请求超时（默认10秒）
type Plugin struct {
    *plugin.BasePlugin
    // ...
}
```

**为导出的方法提供详细注释**

```go
// GetWeather 查询指定城市的天气信息
//
// 参数：
//   - city: 城市名称，支持中英文
//
// 返回：
//   - *WeatherInfo: 天气信息，如果查询失败返回 nil
//   - error: 错误信息
//
// 示例：
//   weather, err := plugin.GetWeather("北京")
//   if err != nil {
//       return err
//   }
//   fmt.Printf("温度: %d°C\n", weather.Temperature)
func (p *Plugin) GetWeather(city string) (*WeatherInfo, error) {
    // ...
}
```

---

## 检查清单

在提交插件代码前，请检查以下项目：

- [ ] 在元数据中声明了所有依赖
- [ ] 没有手动实现 `Dependencies()` 方法
- [ ] 使用 `OnCommand()` 等便捷方法注册命令
- [ ] 为所有命令设置了完整的定义信息
- [ ] 提供了完整的元数据（名称、版本、描述、帮助文本）
- [ ] 正确实现了 `Load()` 和 `Unload()` 方法
- [ ] 清理了所有资源（关闭连接、停止后台任务）
- [ ] 使用互斥锁保护共享状态
- [ ] 编写了单元测试
- [ ] 添加了文档注释
- [ ] 处理了所有错误，没有使用 panic
- [ ] 使用日志记录重要操作
- [ ] 性能敏感路径使用了缓存
- [ ] 没有创建无限制的 goroutine

---

## 参考资料

- [插件依赖管理文档](./plugin-dependency-management.md)
- [插件系统架构](./plugin-architecture.md)
- [BasePlugin API 文档](../../plugin/plugin.go)

---

**最后更新：** 2026-02-12

