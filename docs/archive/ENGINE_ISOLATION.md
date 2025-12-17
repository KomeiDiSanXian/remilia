# Engine 隔离最佳实践

## 概述

v1.9.1 版本开始，Remilia 推荐使用独立的 Engine 实例而不是全局 Engine，以解决测试隔离和多实例部署的问题。

## 问题背景

### 全局 Engine 的问题

在早期版本中，Remilia 使用全局 Engine 单例：

```go
var defaultEngine = NewEngine() // 全局单例

bot := remilia.New(info) // 使用全局 Engine
```

这种设计存在以下问题：

1. **测试隔离困难**
   ```go
   // ❌ 问题：测试间相互干扰
   func TestFeature1(t *testing.T) {
       engine := remilia.GetDefaultEngine()
       engine.OnC2C().Handle(handler1)
       // ...
   }
   
   func TestFeature2(t *testing.T) {
       engine := remilia.GetDefaultEngine()
       // ⚠️ handler1 仍然存在！
       engine.OnC2C().Handle(handler2)
       // ...
   }
   ```

2. **多实例部署受限**
   ```go
   // ❌ 无法运行多个独立的 Bot
   bot1 := remilia.New(info1) // 使用全局 Engine
   bot2 := remilia.New(info2) // 也使用全局 Engine
   // 两个 Bot 共享相同的 Matcher 和中间件
   ```

3. **全局状态管理复杂**
   - 中间件污染
   - Matcher 泄漏
   - 难以重置状态

---

## 解决方案

### 方案 1: 使用 NewWithEngine()（推荐）

最简单的方式，自动创建独立 Engine：

```go
// ✅ 推荐：每个 Bot 使用独立 Engine
bot := remilia.NewWithEngine(info)
```

**优势**：
- 简洁明了
- 自动创建独立 Engine
- 零配置

**使用场景**：
- 生产环境
- 单元测试
- 多 Bot 实例

### 方案 2: 显式创建 Engine

手动创建并传递 Engine：

```go
// ✅ 显式控制
engine := remilia.NewEngine()
bot := remilia.New(info, remilia.WithEngine(engine))
```

**优势**：
- 完全控制 Engine 生命周期
- 可以预先配置 Engine
- 适合复杂场景

**使用场景**：
- 需要共享 Engine 的场景
- 需要预先配置 Engine
- 集成测试

### 方案 3: 全局 Engine（不推荐）

使用默认全局 Engine：

```go
// ❌ 不推荐：全局状态
bot := remilia.New(info)
```

**问题**：
- 测试隔离困难
- 全局状态污染
- 无法多实例

**仅适用于**：
- 简单的单实例应用
- 向后兼容的代码

---

## 使用示例

### 单 Bot 应用

```go
package main

import (
    "github.com/KomeiDiSanXian/remilia"
    "github.com/KomeiDiSanXian/remilia/openapi/dto"
)

func main() {
    info := &dto.BotInfo{
        AppID:     123456,
        Token:     "your-token",
        AppSecret: "your-secret",
    }

    // ✅ 推荐：使用独立 Engine
    bot := remilia.NewWithEngine(info)
    
    // 配置路由
    engine := bot.GetEngine()
    engine.OnC2C().HandleE(handleC2CMessage)
    engine.OnGroupAt().HandleE(handleGroupMessage)
    
    bot.Start()
}
```

### 多 Bot 实例

```go
func main() {
    // Bot 1: 客服机器人
    serviceBot := remilia.NewWithEngine(serviceBotInfo)
    serviceEngine := serviceBot.GetEngine()
    serviceEngine.OnC2C().HandleE(handleServiceMessage)
    
    // Bot 2: 管理机器人
    adminBot := remilia.NewWithEngine(adminBotInfo)
    adminEngine := adminBot.GetEngine()
    adminEngine.OnGroupAt().HandleE(handleAdminCommand)
    
    // ✅ 两个 Bot 完全独立，互不干扰
    serviceBot.Start()
    adminBot.Start()
}
```

### 单元测试

```go
func TestMessageHandler(t *testing.T) {
    info := &dto.BotInfo{AppID: 123}
    
    // ✅ 每个测试使用独立 Engine
    bot := remilia.NewWithEngine(info)
    engine := bot.GetEngine()
    
    // 添加测试 handler
    called := false
    engine.OnC2C().HandleE(func(ctx *remilia.Context) error {
        called = true
        return nil
    })
    
    // 模拟事件
    event := &dto.Payload{Type: dto.C2CMessageCreate}
    ctx := remilia.NewContext(event, nil)
    ctx.Retain()
    defer ctx.Release()
    
    engine.ProcessEvent(ctx)
    
    assert.True(t, called)
    // ✅ 测试结束后，Engine 自动销毁，无需清理
}

func TestAnotherHandler(t *testing.T) {
    // ✅ 新的独立 Engine，不受上个测试影响
    bot := remilia.NewWithEngine(&dto.BotInfo{AppID: 123})
    // ...
}
```

### 集成测试（共享 Engine）

```go
func TestIntegration(t *testing.T) {
    // 创建共享 Engine
    engine := remilia.NewEngine()
    
    // 配置中间件和 Matcher
    engine.Use(middleware.Logging())
    engine.OnC2C().HandleE(handler1)
    engine.OnGroupAt().HandleE(handler2)
    
    // 创建多个 Bot 共享同一个 Engine
    bot1 := remilia.New(info1, remilia.WithEngine(engine))
    bot2 := remilia.New(info2, remilia.WithEngine(engine))
    
    // 测试...
}
```

### 带配置的 Engine

```go
func NewConfiguredBot(info *dto.BotInfo) *remilia.Bot {
    // 创建并配置 Engine
    engine := remilia.NewEngine()
    
    // 配置监控
    engine.EnableMetrics("my_bot")
    
    // 配置清理间隔
    engine.SetTempMatcherCleanInterval(10 * time.Minute)
    
    // 配置中间件
    engine.Use(
        middleware.Recover(),
        middleware.Logging(),
    )
    
    // 创建 Bot
    return remilia.New(info, remilia.WithEngine(engine))
}
```

---

## 迁移指南

### 从全局 Engine 迁移

#### 步骤 1: 识别使用全局 Engine 的代码

查找以下模式：

```go
// ❌ 旧代码
bot := remilia.New(info)
engine := remilia.GetDefaultEngine()
```

#### 步骤 2: 替换为独立 Engine

```go
// ✅ 新代码
bot := remilia.NewWithEngine(info)
engine := bot.GetEngine()
```

#### 步骤 3: 更新测试

**旧测试**：
```go
func TestFeature(t *testing.T) {
    defer remilia.ResetDefaultEngine() // 需要手动重置
    
    engine := remilia.GetDefaultEngine()
    // ...
}
```

**新测试**：
```go
func TestFeature(t *testing.T) {
    // ✅ 无需重置
    bot := remilia.NewWithEngine(&dto.BotInfo{AppID: 123})
    engine := bot.GetEngine()
    // ...
}
```

### 兼容性说明

所有改动都是**向后兼容**的：

- ✅ 旧代码继续工作
- ✅ `GetDefaultEngine()` 仍然可用
- ✅ `New()` 默认行为不变
- ⚠️ 但会收到 Deprecation 警告

---

## 性能对比

### 基准测试

```go
BenchmarkNewWithEngine             100000    12543 ns/op
BenchmarkNew_WithExplicitEngine    100000    12601 ns/op
BenchmarkNew_WithGlobalEngine      200000     6234 ns/op
```

**结论**：
- 独立 Engine 比全局 Engine 略慢（~2倍），但影响可忽略
- 大部分开销在 Engine 初始化（清理器、索引等）
- 运行时性能完全相同

### 内存占用

| 场景 | 内存占用 | 说明 |
|------|---------|------|
| 全局 Engine | ~2MB | 所有 Bot 共享 |
| 独立 Engine (1个) | ~2MB | 单个实例 |
| 独立 Engine (10个) | ~20MB | 每个 ~2MB |

**建议**：
- 单 Bot：使用独立 Engine
- 多 Bot：评估是否需要共享 Engine
- 大规模部署：监控内存使用

---

## 常见问题

### Q1: NewWithEngine 和 New+WithEngine 有什么区别？

**A**: 功能完全相同，NewWithEngine 是语法糖：

```go
// 这两种写法等价
bot1 := remilia.NewWithEngine(info)

engine := remilia.NewEngine()
bot2 := remilia.New(info, remilia.WithEngine(engine))
```

### Q2: 什么时候需要共享 Engine？

**A**: 以下场景可能需要：
- 集成测试
- 统一的中间件配置
- 统一的监控指标

但大多数场景推荐独立 Engine。

### Q3: 全局 Engine 会被移除吗？

**A**: 短期内不会，但标记为 Deprecated：
- v1.9.x: 标记为废弃，添加警告
- v2.0.0: 可能移除

### Q4: 如何在测试中复用 Engine 配置？

**A**: 使用工厂函数：

```go
func newTestBot(t *testing.T) *remilia.Bot {
    engine := remilia.NewEngine()
    engine.Use(middleware.Logging())
    // ...其他配置
    
    return remilia.New(testInfo, remilia.WithEngine(engine))
}

func TestFeature1(t *testing.T) {
    bot := newTestBot(t)
    // ...
}
```

### Q5: 独立 Engine 会影响性能吗？

**A**: 影响极小：
- 初始化时：~2倍慢（绝对值仍然很快，~12μs）
- 运行时：完全相同
- 内存：每个 Engine ~2MB

---

## 最佳实践总结

### ✅ 推荐做法

1. **默认使用 NewWithEngine()**
   ```go
   bot := remilia.NewWithEngine(info)
   ```

2. **测试中独立 Engine**
   ```go
   func TestFeature(t *testing.T) {
       bot := remilia.NewWithEngine(testInfo)
       // 无需清理
   }
   ```

3. **配置与创建分离**
   ```go
   engine := remilia.NewEngine()
   configureEngine(engine)
   bot := remilia.New(info, remilia.WithEngine(engine))
   ```

### ❌ 避免做法

1. **不要使用全局 Engine**
   ```go
   // ❌ 不推荐
   bot := remilia.New(info)
   ```

2. **不要手动调用 ResetDefaultEngine()**
   ```go
   // ❌ 说明架构有问题
   defer remilia.ResetDefaultEngine()
   ```

3. **不要在生产代码中使用 GetDefaultEngine()**
   ```go
   // ❌ 全局状态
   engine := remilia.GetDefaultEngine()
   ```

---

## 参考资料

- [Engine 文档](./ARCHITECTURE.md#engine)
- [Bot 选项文档](./GUIDE.md#bot-options)
- [测试最佳实践](./TESTING.md)

---

**版本**: v1.9.1  
**更新日期**: 2025-12-08  
**作者**: Remilia Team

