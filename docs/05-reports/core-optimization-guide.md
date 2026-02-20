# Core 模块优化使用指南

本指南介绍如何使用新增的优化功能。

---

## 🚀 快速开始

### 1. 使用命令缓存（自动启用）

命令缓存已自动启用，无需额外配置。

```go
eng := NewEngine()

// 注册命令
eng.RegisterCommandDef(dto.C2CMessageCreate, &command.Definition{
    Name:        "help",
    Description: "显示帮助信息",
})

// 获取所有命令（现在使用缓存，性能提升显著）
commands := eng.GetAllCommands() // O(1) 复杂度
```

**性能**: 11µs/op, 1 次内存分配

---

### 2. 使用 Matcher 编译优化

#### 方式一：批量编译（推荐）

```go
eng := NewEngine()

// 注册所有 matchers
eng.RegisterCommandDef(dto.C2CMessageCreate, def1)
eng.RegisterCommandDef(dto.GroupAtMessageCreate, def2)
// ... 更多命令

// 一次性编译所有 matchers
eng.CompileAllMatchers()
```

#### 方式二：单独编译

```go
compiler := eng.GetCompiler()

// 编译单个 matcher
compiled := compiler.Compile(matcher)

// 使用编译后的 matcher（未来版本）
// matched := compiled.Match(ctx)
```

**优势**:
- 规则按执行成本排序（低成本优先，早失败）
- 正则表达式预编译并缓存
- 预期性能提升 20-40%

---

### 3. 临时 Matcher 过期清理（自动启用）

临时 matcher 的过期清理已自动启用。

```go
eng := NewEngine() // 自动启动清理器，默认5分钟

// 创建临时 matcher，1小时后过期
m := eng.OnTemp(dto.C2CMessageCreate, rules...)
m.SetTempWithTimeout(1 * time.Hour)

// 过期后会自动清理，无需手动管理
```

#### 自定义清理间隔

```go
eng := NewEngine(WithCleanupInterval(1 * time.Minute))

// 或者在创建后修改
eng.SetTempMatcherCleanInterval(30 * time.Second)
```

---

## 📊 性能监控

### 获取统计信息

```go
// 获取 matcher 统计
stats := eng.GetMatcherStats()
fmt.Printf("Total: %d, Global: %d\n", stats.Total, stats.Global)

// 获取临时 matcher 数量
tempCount := eng.GetTempMatcherCount()
fmt.Printf("Temp matchers: %d\n", tempCount)

// 获取命令数量
commands := eng.GetAllCommands()
fmt.Printf("Commands: %d\n", len(commands))
```

### 基准测试

```go
func BenchmarkMyFeature(b *testing.B) {
    eng := NewEngine()
    // ... 设置
    
    eng.CompileAllMatchers() // 编译优化
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        // 测试代码
    }
}
```

---

## 🎯 最佳实践

### 1. 启动时优化

```go
func setupEngine() *Engine {
    eng := NewEngine(
        WithCleanupInterval(5 * time.Minute),
    )
    
    // 批量注册所有命令
    registerAllCommands(eng)
    
    // 一次性编译
    eng.CompileAllMatchers()
    
    return eng
}
```

### 2. 插件加载优化

```go
// 使用批量注册
matchers := []*Matcher{
    eng.OnCommand(...),
    eng.OnCommand(...),
    eng.OnCommand(...),
}

// 批量注册（更高效）
eng.BatchRegisterMatchers(matchers)

// 编译新注册的 matchers
eng.CompileAllMatchers()
```

### 3. 临时 Matcher 管理

```go
// 好的做法：设置明确的过期时间
m := eng.OnTemp(eventType, rules...)
m.SetTempWithTimeout(30 * time.Minute)

// 或者设置使用次数
m.SetTempWithMaxUse(1) // 一次性 matcher
```

### 4. 命令发现

```go
// 按插件分组
byPlugin := eng.GetCommandsByPlugin()
for plugin, cmds := range byPlugin {
    fmt.Printf("%s: %d commands\n", plugin, len(cmds))
}

// 按分类分组
byCategory := eng.GetCommandsByCategory()

// 查找特定命令
cmdInfo := eng.FindCommand("help")
if cmdInfo != nil {
    fmt.Println(cmdInfo.Usage)
}
```

---

## ⚠️ 注意事项

### 1. 编译时机

- **推荐**: 在应用启动完成后调用一次 `CompileAllMatchers()`
- **不推荐**: 在每次注册 matcher 后都编译（性能浪费）

### 2. 缓存更新

命令缓存会在以下情况自动更新：
- 注册新 matcher
- 调用 `SetDefinition()`
- 删除 matcher
- 重建索引

**无需手动管理缓存**。

### 3. 内存使用

- 编译缓存会占用额外内存
- 命令缓存会占用少量内存
- 如果 matcher 数量非常大（>10000），考虑分批编译

### 4. 并发安全

所有优化功能都是并发安全的：
- 命令缓存使用 COW 模式
- 编译器使用 `sync.Map`
- 临时 matcher 管理使用分片锁

---

## 🔍 故障排查

### Q: GetAllCommands() 返回的命令不完整

**A**: 检查以下几点：
1. 命令是否被标记为 `Hidden: true`
2. 命令是否通过 `RegisterCommandDef` 或 `OnCommand` 注册
3. 命令定义是否在注册后设置（调用 `SetDefinition`）

### Q: 编译后性能没有明显提升

**A**: 可能的原因：
1. Matcher 数量较少（编译优化主要针对大量 matchers）
2. 规则本身已经很简单（编译带来的优化有限）
3. 瓶颈在其他地方（使用 profiling 工具分析）

### Q: 临时 matcher 没有自动清理

**A**: 检查以下几点：
1. 清理器是否已启动：`engine.services.tempMatcherCleanerDone != nil`
2. 清理间隔是否设置为 0（会禁用清理器）
3. Matcher 是否设置了过期时间（`SetTempWithTimeout`）

---

## 📚 相关文档

- [Bug 分析与修复报告](./core-analysis-bugs-improvements.md)
- [优化完成总结](./core-optimization-summary.md)
- [Core Context API 文档](../../core/context/)
- [Core Engine API 文档](../../core/engine/)

---

**最后更新**: 2026-02-20  
**适用版本**: v0.8.0+

