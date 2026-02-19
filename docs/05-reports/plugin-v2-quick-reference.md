# Plugin v2 API 快速参考

## 基本模板

```go
func NewMyPlugin() *plugin.PluginDescriptor {
    return &plugin.PluginDescriptor{
        Name:        "myplugin",
        Version:     "1.0.0",
        Author:      "Your Name",
        Description: "Plugin description",
        Category:    "分类",
        Tags:        []string{"tag1", "tag2"},
        
        Setup: func(ctx *plugin.SetupContext) error {
            // 初始化逻辑
            return nil
        },
    }
}
```

## 注册命令

```go
Setup: func(ctx *plugin.SetupContext) error {
    // 私聊命令
    ctx.Engine.OnCommand(dto.C2CMessageCreate, "/hello").
        Handle(func(c *eventctx.Context) error {
            _, err := c.ReplyPrivate(&dto.Message{
                Type:    dto.TextMessage,
                Content: "Hello!",
            })
            return err
        })
    
    // 群聊命令（需要 @）
    ctx.Engine.OnCommand(dto.GroupAtMessageCreate, "/hello").
        Handle(func(c *eventctx.Context) error {
            return c.Reply("Hello!")
        })
    
    return nil
}
```

## 状态管理

```go
func NewMyPlugin() *plugin.PluginDescriptor {
    // 使用闭包捕获状态
    count := 0
    config := map[string]string{}
    
    return &plugin.PluginDescriptor{
        Name: "myplugin",
        Setup: func(ctx *plugin.SetupContext) error {
            ctx.Engine.OnCommand(dto.C2CMessageCreate, "/count").
                Handle(func(c *eventctx.Context) error {
                    count++  // 修改状态
                    // ...
                })
            return nil
        },
    }
}
```

## 依赖注入

```go
Deps: []string{"permission", "storage"},

Setup: func(ctx *plugin.SetupContext) error {
    // 方式 1：类型断言
    perm := ctx.MustGet("permission").(*permission.Plugin)
    
    // 方式 2：类型安全（需要检查错误）
    storage, ok := ctx.Get("storage")
    if !ok {
        return errors.New("storage not found")
    }
    
    // 使用依赖
    if perm.HasPermission(userID, "admin") {
        // ...
    }
    
    return nil
}
```

## 读取配置

```go
Setup: func(ctx *plugin.SetupContext) error {
    // 读取插件配置
    if ctx.Config != nil {
        host := ctx.Config.GetString("host", "localhost")
        port := ctx.Config.GetInt("port", 8080)
        enabled := ctx.Config.GetBool("enabled", true)
    }
    
    return nil
}
```

## 生命周期钩子

```go
return &plugin.PluginDescriptor{
    Name: "myplugin",
    
    Setup: func(ctx *plugin.SetupContext) error {
        // 初始化：注册命令、获取依赖等
        return nil
    },
    
    Teardown: func() error {
        // 清理：关闭连接、保存状态等
        return nil
    },
    
    Reload: func(ctx *plugin.SetupContext) error {
        // 热重载：重新加载配置等
        // 如果不定义，默认执行 Teardown + Setup
        return nil
    },
}
```

## 错误处理

```go
Handle(func(c *eventctx.Context) error {
    // 业务错误
    if someCondition {
        return errors.New("something went wrong")
    }
    
    // 回复错误消息
    _, err := c.ReplyPrivate(&dto.Message{
        Type:    dto.TextMessage,
        Content: "❌ 操作失败：" + err.Error(),
    })
    return err
})
```

## 解析命令参数

```go
Handle(func(c *eventctx.Context) error {
    content := c.GetMessageContent()
    
    // 简单解析
    if len(content) <= 7 {  // "/cmd "
        return c.Reply("参数不足")
    }
    arg := content[5:]
    
    // 使用命令解析器
    args, err := command.ParseCommandLine(content)
    if err != nil {
        return c.Reply("解析失败")
    }
    
    arg1 := args.Get(0)  // 第一个参数
    arg2 := args.Get(1)  // 第二个参数
    
    return nil
})
```

## 权限检查

```go
Setup: func(ctx *plugin.SetupContext) error {
    perm := ctx.MustGet("permission").(*permission.Plugin)
    
    ctx.Engine.OnCommand(dto.C2CMessageCreate, "/admin").
        Handle(func(c *eventctx.Context) error {
            userID := c.GetUserID()
            
            // 检查权限
            if !perm.HasPermission(userID, "admin") {
                return c.Reply("❌ 权限不足")
            }
            
            // 执行管理操作
            return c.Reply("✅ 操作成功")
        })
    
    return nil
}
```

## 异步任务

```go
Setup: func(ctx *plugin.SetupContext) error {
    // 启动后台任务
    go func() {
        ticker := time.NewTicker(5 * time.Minute)
        defer ticker.Stop()
        
        for range ticker.C {
            // 定期执行任务
        }
    }()
    
    return nil
}
```

## 注册多个命令

```go
Setup: func(ctx *plugin.SetupContext) error {
    // 命令列表
    commands := []struct {
        pattern string
        handler func(*eventctx.Context) error
    }{
        {"/cmd1", handleCmd1},
        {"/cmd2", handleCmd2},
        {"/cmd3", handleCmd3},
    }
    
    // 批量注册
    for _, cmd := range commands {
        ctx.Engine.OnCommand(dto.C2CMessageCreate, cmd.pattern).
            Handle(cmd.handler)
    }
    
    return nil
}
```

## 使用中间件

```go
Setup: func(ctx *plugin.SetupContext) error {
    // 为插件的所有命令添加中间件
    ctx.Engine.UseForGroup("myplugin", 
        middleware.RateLimit(10, time.Minute),
        middleware.Logging(),
    )
    
    // 或为单个命令添加
    ctx.Engine.OnCommand(dto.C2CMessageCreate, "/hello").
        Use(middleware.RequirePermission("user")).
        Handle(func(c *eventctx.Context) error {
            return c.Reply("Hello!")
        })
    
    return nil
}
```

## 访问存储

```go
Deps: []string{"storage"},

Setup: func(ctx *plugin.SetupContext) error {
    storage := ctx.MustGet("storage").(*storage.Plugin)
    
    ctx.Engine.OnCommand(dto.C2CMessageCreate, "/save").
        Handle(func(c *eventctx.Context) error {
            // 保存数据
            data := []byte("some data")
            err := storage.Set("key", data, time.Hour)
            
            // 读取数据
            data, err = storage.Get("key")
            
            return err
        })
    
    return nil
}
```

## 使用缓存

```go
Deps: []string{"cache"},

Setup: func(ctx *plugin.SetupContext) error {
    cache := ctx.MustGet("cache").(*cache.Plugin)
    
    // 带缓存的处理
    ctx.Engine.OnCommand(dto.C2CMessageCreate, "/data").
        Handle(func(c *eventctx.Context) error {
            key := "data:key"
            
            // 尝试从缓存获取
            if data, ok := cache.Get(key); ok {
                return c.Reply(string(data))
            }
            
            // 缓存未命中，计算结果
            result := expensiveOperation()
            
            // 存入缓存（5分钟）
            cache.Set(key, []byte(result), 5*time.Minute)
            
            return c.Reply(result)
        })
    
    return nil
}
```

## 完整示例

```go
package myplugin

import (
    "fmt"
    "time"
    
    "github.com/KomeiDiSanXian/remilia/plugin"
    "github.com/KomeiDiSanXian/remilia/openapi/dto"
    eventctx "github.com/KomeiDiSanXian/remilia/core/context"
)

func New() *plugin.PluginDescriptor {
    // 状态
    stats := &Stats{
        startTime: time.Now(),
        requests:  0,
    }
    
    return &plugin.PluginDescriptor{
        // 元数据
        Name:        "myplugin",
        Version:     "1.0.0",
        Author:      "Your Name",
        Description: "A complete example plugin",
        Category:    "示例",
        Tags:        []string{"example", "demo"},
        
        // 依赖
        Deps: []string{"permission"},
        
        // 初始化
        Setup: func(ctx *plugin.SetupContext) error {
            perm := ctx.MustGet("permission").(*permission.Plugin)
            
            // 命令 1：基本问候
            ctx.Engine.OnCommand(dto.C2CMessageCreate, "/hello").
                Handle(func(c *eventctx.Context) error {
                    stats.requests++
                    _, err := c.ReplyPrivate(&dto.Message{
                        Type:    dto.TextMessage,
                        Content: "Hello, World!",
                    })
                    return err
                })
            
            // 命令 2：统计信息（需要权限）
            ctx.Engine.OnCommand(dto.C2CMessageCreate, "/stats").
                Handle(func(c *eventctx.Context) error {
                    if !perm.HasPermission(c.GetUserID(), "admin") {
                        return c.Reply("❌ 权限不足")
                    }
                    
                    uptime := time.Since(stats.startTime)
                    msg := fmt.Sprintf("运行时间: %s\n请求数: %d",
                        uptime, stats.requests)
                    
                    _, err := c.ReplyPrivate(&dto.Message{
                        Type:    dto.TextMessage,
                        Content: msg,
                    })
                    return err
                })
            
            return nil
        },
        
        // 清理
        Teardown: func() error {
            // 保存统计等
            return nil
        },
    }
}

type Stats struct {
    startTime time.Time
    requests  int64
}
```

## 注册插件

```go
// main.go
manager := plugin.NewManager(bot.Engine())

// 注册 v2 插件
err := manager.RegisterV2(myplugin.New())
if err != nil {
    log.Fatal(err)
}
```

## 常用代码片段

### 获取用户信息
```go
Handle(func(c *eventctx.Context) error {
    userID := c.GetUserID()
    content := c.GetMessageContent()
    // ...
})
```

### 回复消息
```go
// 私聊
_, err := c.ReplyPrivate(&dto.Message{
    Type:    dto.TextMessage,
    Content: "Hello",
})

// 群聊
err := c.Reply("Hello")
```

### 日志记录
```go
logger.Info("Message")
logger.Infof("Formatted %s", value)
logger.WithError(err).Error("Error occurred")
logger.WithField("key", value).Debug("Debug info")
```

---

**更多示例**: `examples/plugin-v2-demo/`  
**完整文档**: `docs/05-reports/plugin-refactoring-analysis.md`

