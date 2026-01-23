# 插件系统增强方案 - 快速参考

## 🎯 核心增强点

### 1. 插件上下文系统

**当前问题**: 插件直接访问 Engine，缺少隔离

**解决方案**: 提供统一的 PluginContext 接口

```go
type PluginContext interface {
    // 配置
    GetConfig() PluginConfig
    
    // 资源管理
    GetResource(key string) (interface{}, bool)
    SetResource(key string, value interface{})
    
    // 事件总线
    Publish(topic string, data interface{}) error
    Subscribe(topic string, handler EventHandler) error
    
    // 存储
    GetStorage() PluginStorage
    
    // 日志和指标
    Logger() *logrus.Entry
    Metrics() PluginMetrics
}
```

**使用示例**:
```go
func (p *MyPlugin) LoadWithContext(ctx PluginContext) error {
    // 获取配置
    apiKey := ctx.GetConfig().GetString("api_key", "")
    
    // 创建并存储资源
    client := &http.Client{Timeout: 10*time.Second}
    ctx.SetResource("http_client", client)
    
    // 订阅事件
    ctx.Subscribe("user.login", p.onUserLogin)
    
    return nil
}
```

---

### 2. 事件总线

**当前问题**: 插件间通信困难，耦合度高

**解决方案**: 实现发布-订阅模式的事件总线

```go
// 发布事件
ctx.Publish("user.login", Event{
    Data: map[string]interface{}{
        "user_id": "123",
        "time": time.Now(),
    },
})

// 订阅事件
ctx.Subscribe("user.login", func(event Event) error {
    userID := event.Data["user_id"]
    log.Printf("User %s logged in", userID)
    return nil
})
```

**收益**: 插件解耦，易于扩展

---

### 3. 插件配置管理

**当前问题**: 配置混杂，难以管理

**解决方案**: 独立的插件配置命名空间

```yaml
# config.yaml
plugins:
  weather:
    api_key: "your-key"
    timeout: "10s"
    cache_ttl: "5m"
```

```go
// 读取配置
config := ctx.GetConfig()
apiKey := config.GetString("api_key", "")
timeout := config.GetDuration("timeout", 10*time.Second)

// 监听配置变更
config.OnChange(func() {
    log.Info("Config reloaded")
})
```

**收益**: 配置隔离，支持热更新

---

### 4. 插件存储

**当前问题**: 缺少持久化能力

**解决方案**: 提供统一的存储接口

```go
storage := ctx.GetStorage()

// 存储数据
data, _ := json.Marshal(weatherData)
storage.Set("cache:beijing", data)

// 读取数据
cached, _ := storage.Get("cache:beijing")

// 列出键
keys, _ := storage.List("cache:")
```

**支持后端**:
- 内存（默认）
- Redis
- 文件系统
- 自定义后端

---

### 5. 权限系统

**当前问题**: 插件权限过大，存在安全风险

**解决方案**: 细粒度权限控制

```yaml
plugins:
  weather:
    role: standard  # 预定义角色
    # 或自定义权限
    permissions:
      - engine.register_matcher
      - network.http
      - storage.read
      - storage.write
```

**权限类别**:
- Engine 访问
- 事件总线
- 存储访问
- 网络访问
- 系统配置

**预定义角色**:
- `basic`: 基础权限
- `standard`: 标准权限
- `full`: 完整权限
- `admin`: 管理员权限

---

### 6. 热重载

**当前问题**: 重载需要卸载再加载，服务中断

**解决方案**: 支持无缝热重载

```go
type HotReloadable interface {
    // 准备新状态
    PrepareReload(ctx PluginContext) (ReloadState, error)
    
    // 原子切换
    CommitReload(state ReloadState) error
    
    // 回滚
    RollbackReload(state ReloadState) error
}
```

**收益**: 零停机配置更新

---

### 7. 沙箱隔离

**当前问题**: 插件崩溃影响全局

**解决方案**: 资源限制和隔离

```go
limits := SandboxLimits{
    MaxGoroutines: 100,
    MaxMemory:     100 * 1024 * 1024, // 100MB
    Timeout:       30 * time.Second,
}

sandbox := NewPluginSandbox(plugin, limits)
```

**监控**:
```go
stats := sandbox.GetStats()
log.Printf("Goroutines: %d, Memory: %dMB", 
    stats.GoroutineCount,
    stats.MemoryUsage/1024/1024)
```

---

### 8. 插件市场

**当前问题**: 缺少插件分发机制

**解决方案**: CLI 工具和仓库

```bash
# 搜索插件
remilia plugin search weather

# 安装插件
remilia plugin install weather@1.0.0

# 更新插件
remilia plugin update weather

# 列出插件
remilia plugin list
```

**插件清单**:
```yaml
name: weather
version: 1.0.0
description: Weather query plugin
dependencies:
  - name: http-client
    version: ">=1.0.0"
permissions:
  - network.http
  - storage.read
```

---

## 📊 对比总结

| 特性 | 当前 | 增强后 | 提升 |
|------|------|--------|------|
| 插件隔离 | ❌ | ✅ 上下文隔离 | +100% |
| 插件通信 | ⚠️ 直接依赖 | ✅ 事件总线 | +80% |
| 配置管理 | ⚠️ 混杂 | ✅ 独立命名空间 | +90% |
| 存储能力 | ❌ | ✅ 多后端支持 | +100% |
| 权限控制 | ❌ | ✅ 细粒度权限 | +100% |
| 热重载 | ⚠️ 中断式 | ✅ 无缝切换 | +100% |
| 资源隔离 | ❌ | ✅ 沙箱机制 | +100% |
| 分发机制 | ❌ | ✅ 插件市场 | +100% |

---

## 🛠️ 迁移指南

### 从旧接口迁移

**旧代码**:
```go
type MyPlugin struct {
    *BasePlugin
}

func (p *MyPlugin) Load(eng *engine.Engine) error {
    matcher := engine.NewMatcher().OnCommand("/test")
    p.AddMatcher(matcher)
    return eng.RegisterMatcher(matcher)
}
```

**新代码**:
```go
type MyPlugin struct {
    *BasePlugin
}

func (p *MyPlugin) LoadWithContext(ctx PluginContext) error {
    // 使用上下文注册
    matcher := engine.NewMatcher().OnCommand("/test")
    return ctx.RegisterMatcher(matcher)
}
```

### 兼容性

- ✅ 保持向后兼容
- ✅ 旧接口继续可用
- ✅ 新接口为可选
- ✅ 提供适配器层

---

## 📈 实施优先级

### P0 - 立即实施（2-3周）
- ✅ 插件上下文系统
- ✅ 配置管理系统
- ✅ 内存存储后端

### P1 - 近期实施（3-4周）
- ⏳ 事件总线系统
- ⏳ Redis/文件存储后端
- ⏳ 基础权限系统

### P2 - 中期实施（1-2月）
- ⏳ 热重载机制
- ⏳ 沙箱隔离
- ⏳ Metrics 集成

### P3 - 长期规划（3-6月）
- ⏳ 插件市场
- ⏳ CLI 工具
- ⏳ 可视化管理

---

## 💡 最佳实践

### DO ✅

```go
// 1. 使用上下文管理资源
func (p *Plugin) LoadWithContext(ctx PluginContext) error {
    client := createClient()
    ctx.SetResource("client", client)
    return nil
}

// 2. 通过事件总线解耦
ctx.Publish("data.updated", event)

// 3. 使用配置命名空间
config := ctx.GetConfig()
apiKey := config.GetString("api_key", "")

// 4. 请求最小权限
permissions:
  - engine.register_matcher
  - storage.read
```

### DON'T ❌

```go
// 1. 不要直接访问全局状态
// ❌ globalEngine.RegisterMatcher(m)

// 2. 不要硬编码配置
// ❌ apiKey := "hardcoded-key"

// 3. 不要创建无限 goroutine
// ❌ for { go process() }

// 4. 不要请求不必要的权限
// ❌ permissions: [admin]
```

---

## 🔗 相关文档

- 📖 [完整增强方案](./PLUGIN_ENHANCEMENT_PROPOSAL.md)
- 📖 [插件开发指南](./PLUGIN_DEVELOPMENT_GUIDE.md)
- 💡 [示例代码](../examples/plugins/)

---

**最后更新**: 2026-01-23  
**维护者**: GitHub Copilot
