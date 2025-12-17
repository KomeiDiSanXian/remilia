# 全局 Engine 移除指南

## 概述

v2.0.0 版本**完全移除**了全局 Engine，所有代码必须显式提供 Engine 实例。这一改动简化了架构，消除了全局状态，提升了测试友好性。

## 变更内容

### 已移除的 API

以下函数和变量已被完全移除：

```go
// ❌ 已移除
var defaultEngine = NewEngine()
func GetDefaultEngine() *Engine
func GetGlobalEngine() *Engine  
func ResetDefaultEngine()
```

### 新的要求

`New()` 函数现在**必须**提供 `WithEngine()` 选项，否则会 panic：

```go
// ❌ 错误：会 panic
bot := remilia.New(info)

// ✅ 正确：提供 Engine
engine := remilia.NewEngine()
bot := remilia.New(info, remilia.WithEngine(engine))

// ✅ 推荐：使用便捷函数
bot := remilia.NewWithEngine(info)
```

## 迁移步骤

### 步骤 1: 识别使用全局 Engine 的代码

搜索以下模式：

```bash
# 查找 GetDefaultEngine
grep -r "GetDefaultEngine" .

# 查找 ResetDefaultEngine  
grep -r "ResetDefaultEngine" .

# 查找不带 WithEngine 的 New()
grep -r "remilia.New(" . | grep -v "WithEngine"
```

### 步骤 2: 替换为显式 Engine

#### 模式 1: 使用默认 Engine

**旧代码**:
```go
bot := remilia.New(info)
```

**新代码**:
```go
bot := remilia.NewWithEngine(info)
```

#### 模式 2: 获取全局 Engine

**旧代码**:
```go
engine := remilia.GetDefaultEngine()
engine.OnC2C().Handle(handler)
```

**新代码**:
```go
bot := remilia.NewWithEngine(info)
engine := bot.GetEngine()
engine.OnC2C().Handle(handler)
```

#### 模式 3: 测试中使用

**旧代码**:
```go
func TestFeature(t *testing.T) {
    defer remilia.ResetDefaultEngine()
    
    engine := remilia.GetDefaultEngine()
    // ...
}
```

**新代码**:
```go
func TestFeature(t *testing.T) {
    engine := remilia.NewEngine()
    // ... 无需清理
}
```

### 步骤 3: 更新测试

所有测试都需要提供 Engine：

**旧代码**:
```go
func TestHandler(t *testing.T) {
    bot := remilia.New(info)
    // ...
}
```

**新代码**:
```go
func TestHandler(t *testing.T) {
    bot := remilia.NewWithEngine(info)
    // ...
}
```

## 迁移示例

### 示例 1: 简单应用

**v1.x 代码**:
```go
package main

import "github.com/KomeiDiSanXian/remilia"

func main() {
    info := &dto.BotInfo{...}
    
    // 使用全局 Engine
    bot := remilia.New(info)
    
    engine := remilia.GetDefaultEngine()
    engine.OnC2C().Handle(handler)
    
    bot.Run()
}
```

**v2.0 代码**:
```go
package main

import "github.com/KomeiDiSanXian/remilia"

func main() {
    info := &dto.BotInfo{...}
    
    // 使用独立 Engine
    bot := remilia.NewWithEngine(info)
    
    engine := bot.GetEngine()
    engine.OnC2C().Handle(handler)
    
    bot.Run()
}
```

### 示例 2: 测试代码

**v1.x 测试**:
```go
func TestMessageHandler(t *testing.T) {
    defer remilia.ResetDefaultEngine() // 需要重置
    
    info := &dto.BotInfo{AppID: 123}
    bot := remilia.New(info)
    
    engine := remilia.GetDefaultEngine()
    engine.OnC2C().Handle(handler)
    
    // 测试...
}

func TestAnotherHandler(t *testing.T) {
    defer remilia.ResetDefaultEngine() // 需要重置
    
    engine := remilia.GetDefaultEngine()
    // 测试...
}
```

**v2.0 测试**:
```go
func TestMessageHandler(t *testing.T) {
    // 无需重置
    bot := remilia.NewWithEngine(&dto.BotInfo{AppID: 123})
    
    engine := bot.GetEngine()
    engine.OnC2C().Handle(handler)
    
    // 测试...
}

func TestAnotherHandler(t *testing.T) {
    // 每个测试独立 Engine，自动隔离
    engine := remilia.NewEngine()
    // 测试...
}
```

### 示例 3: 多 Bot 实例

**v1.x 代码**:
```go
// ❌ 问题：共享全局 Engine
bot1 := remilia.New(info1)
bot2 := remilia.New(info2)
// bot1 和 bot2 会相互干扰
```

**v2.0 代码**:
```go
// ✅ 正确：各自独立 Engine
bot1 := remilia.NewWithEngine(info1)
bot2 := remilia.NewWithEngine(info2)
// bot1 和 bot2 完全隔离
```

## 优势对比

| 方面 | v1.x (全局 Engine) | v2.0 (独立 Engine) |
|------|-------------------|-------------------|
| **测试隔离** | ❌ 需要 Reset | ✅ 自动隔离 |
| **多实例** | ❌ 相互干扰 | ✅ 完全独立 |
| **代码清晰度** | ⚠️ 隐式依赖 | ✅ 显式依赖 |
| **并发安全** | ⚠️ 全局锁竞争 | ✅ 无全局状态 |
| **内存管理** | ⚠️ 永不释放 | ✅ 可回收 |

## 常见问题

### Q1: 为什么移除全局 Engine？

**A**: 全局 Engine 带来的问题：
- 测试间相互干扰，难以隔离
- 无法运行多个独立 Bot 实例
- 全局状态管理复杂，容易出错
- 内存泄漏风险（全局 Engine 永不释放）

### Q2: 如何在多个地方共享同一个 Engine？

**A**: 通过依赖注入传递：

```go
// 创建共享 Engine
engine := remilia.NewEngine()

// 方式 1: 通过 Bot 传递
bot := remilia.New(info, remilia.WithEngine(engine))

// 方式 2: 直接传递给需要的模块
setupHandlers(engine)
setupMiddleware(engine)

func setupHandlers(engine *remilia.Engine) {
    engine.OnC2C().Handle(handler1)
    engine.OnGroupAt().Handle(handler2)
}
```

### Q3: 升级后编译报错怎么办？

**A**: 根据错误类型修复：

**错误 1: undefined: GetDefaultEngine**
```go
// 替换
engine := remilia.GetDefaultEngine()
// 为
bot := remilia.NewWithEngine(info)
engine := bot.GetEngine()
```

**错误 2: undefined: ResetDefaultEngine**
```go
// 删除
defer remilia.ResetDefaultEngine()
// 改用独立 Engine（无需重置）
```

**错误 3: panic: Engine is required**
```go
// 修改
bot := remilia.New(info)
// 为
bot := remilia.NewWithEngine(info)
```

### Q4: 性能有影响吗？

**A**: 几乎没有：
- Engine 创建开销：~12μs（可忽略）
- 运行时性能：完全相同
- 内存：每个 Engine ~2MB（合理）

实际上，移除全局 Engine 后性能可能略有提升（无全局锁竞争）。

### Q5: 我的项目很大，如何批量迁移？

**A**: 使用脚本辅助：

```bash
# 1. 替换 New() 调用
find . -name "*.go" -type f -exec sed -i 's/remilia\.New(/remilia.NewWithEngine(/g' {} +

# 2. 删除 ResetDefaultEngine
find . -name "*.go" -type f -exec sed -i '/ResetDefaultEngine/d' {} +

# 3. 替换 GetDefaultEngine
find . -name "*.go" -type f -exec sed -i 's/GetDefaultEngine()/NewEngine()/g' {} +

# 4. 运行测试验证
go test ./...
```

## 检查清单

迁移完成后，确认：

- [ ] 代码中不再有 `GetDefaultEngine()`
- [ ] 代码中不再有 `ResetDefaultEngine()`  
- [ ] 所有 `New()` 调用都提供了 `WithEngine()` 或改用 `NewWithEngine()`
- [ ] 所有测试都能通过
- [ ] 编译无错误和警告

## 获取帮助

如遇问题：
1. 查看 [ENGINE_ISOLATION.md](ENGINE_ISOLATION.md) 最佳实践
2. 查看示例代码：`example/` 目录
3. 提交 Issue: https://github.com/KomeiDiSanXian/remilia/issues

---

**版本**: v2.0.0  
**更新日期**: 2025-12-08  
**破坏性变更**: 是 ⚠️

