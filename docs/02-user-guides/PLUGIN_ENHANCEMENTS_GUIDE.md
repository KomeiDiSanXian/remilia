# 插件系统增强功能快速参考

## ✅ 已实现的三大功能

### 1. 插件配置管理

**读取配置**:
```go
config := p.GetConfig()
apiKey := config.GetString("api_key", "default")
timeout := config.GetDuration("timeout", 10*time.Second)
retries := config.GetInt("max_retries", 3)
enabled := config.GetBool("enabled", true)
```

**监听配置变化**:
```go
config.OnChange(func(key string, oldVal, newVal interface{}) {
    logger.Infof("Config changed: %s = %v -> %v", key, oldVal, newVal)
})
```

**运行时修改配置**:
```go
config.Set("api_key", "new-key")
```

**配置文件格式**:
```yaml
plugins:
  weather:
    api_key: "your-api-key"
    timeout: "10s"
    max_retries: 3
    enabled: true
```

---

### 2. 插件状态查询

**查询单个插件状态**:
```go
status, err := manager.GetStatus("weather")
if err == nil {
    fmt.Printf("State: %s\n", status.State)
    fmt.Printf("Uptime: %v\n", status.Uptime)
    fmt.Printf("Matchers: %d\n", status.MatcherCount)
}
```

**列出所有插件状态**:
```go
statuses := manager.ListStatus()
for name, status := range statuses {
    fmt.Printf("%s: %s\n", name, status.State)
}
```

**检查是否加载**:
```go
if manager.IsLoaded("weather") {
    fmt.Println("Weather plugin is loaded")
}
```

**获取加载顺序**:
```go
order := manager.GetLoadOrder()
fmt.Printf("Load order: %v\n", order)
```

**插件状态**:
```go
state := p.GetState()  // Unloaded/Loading/Loaded/Error/Reloading
uptime := p.GetUptime()
loadTime := p.GetLoadTime()
lastError := p.GetLastError()
```

---

### 3. 插件间通信 (事件总线)

**发布事件**:
```go
p.PublishEvent("user.login", map[string]string{
    "user_id": "123",
    "timestamp": time.Now().String(),
})
```

**订阅事件**:
```go
sub, err := p.SubscribeEvent("user.login", func(data interface{}) {
    loginData := data.(map[string]string)
    logger.Infof("User %s logged in", loginData["user_id"])
})
```

**取消订阅**:
```go
p.UnsubscribeEvent(sub)
```

**获取统计信息**:
```go
eventBus := p.GetEventBus()
stats := eventBus.GetStats()
fmt.Printf("Topics: %d, Subscriptions: %d, Published: %d\n",
    stats.TopicCount, stats.SubscriptionCount, stats.PublishCount)
```

---

## 🔧 Manager 设置

**启用配置管理**:
```go
v := viper.New()
v.SetConfigFile("config.yaml")
v.ReadInConfig()

manager := plugin.NewManager(eng)
manager.SetViper(v)  // 必须调用才能启用配置管理
```

---

## 📝 完整示例

```go
package main

import (
    "github.com/KomeiDiSanXian/remilia/plugin"
    "github.com/KomeiDiSanXian/remilia/core/engine"
    "github.com/spf13/viper"
)

type MyPlugin struct {
    *plugin.BasePlugin
}

func NewMyPlugin() *MyPlugin {
    return &MyPlugin{
        BasePlugin: plugin.NewBasePlugin("myplugin"),
    }
}

func (p *MyPlugin) Load(eng *engine.Engine) error {
    // 1. 使用配置
    config := p.GetConfig()
    if config != nil {
        apiKey := config.GetString("api_key", "")
        config.OnChange(func(key string, old, new interface{}) {
            // 配置变更处理
        })
    }
    
    // 2. 订阅事件
    p.SubscribeEvent("system.ready", func(data interface{}) {
        // 处理系统就绪事件
    })
    
    // 3. 注册命令
    p.OnCommand(eng, dto.C2CMessageCreate, "/mycommand").
        Handle(func(ctx *context.Context) error {
            // 发布事件
            p.PublishEvent("command.executed", "mycommand")
            return nil
        })
    
    return nil
}

func main() {
    // 创建引擎
    eng := engine.NewEngine()
    
    // 创建管理器并设置配置
    manager := plugin.NewManager(eng)
    v := viper.New()
    v.SetConfigFile("config.yaml")
    v.ReadInConfig()
    manager.SetViper(v)
    
    // 注册插件
    plugin := NewMyPlugin()
    manager.Register(plugin)
    
    // 查询状态
    status, _ := manager.GetStatus("myplugin")
    fmt.Printf("Plugin state: %s\n", status.State)
}
```

---

## 📊 测试

所有功能已通过完整测试：
```bash
go test ./plugin/... -v
```

测试文件：`plugin/enhancement_test.go`

---

## 📚 文档

- **实施报告**: `docs/05-reports/PLUGIN_ENHANCEMENTS_IMPLEMENTATION.md`
- **需求分析**: `docs/05-reports/PLUGIN_ENHANCEMENT_ANALYSIS.md`

