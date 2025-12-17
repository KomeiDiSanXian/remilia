# 性能与测试指南

## 📊 性能概览

### 核心性能指标（v1.2.1 - 2025年12月）

**测试环境**:
- CPU: AMD Ryzen 7 5800H (16 Core)
- OS: Windows 11
- Go: 1.23.x
- 最后测试: 2025-12-02

**v1.2.1 性能改进**:
- ⚡ ProcessEvent 并发吞吐量提升 **20-30%**（锁优化）
- ⚡ 锁等待时间减少 **50%**
- ✅ 消除 Timer 和信号量泄漏
- ✅ 新增 100+ 测试场景

#### ⚠️ 简化测试结果（仅供参考）
```
⚠️ 单核性能:        4,881,542 ops/sec (247.2 ns/op) - 简化测试
⚠️ 多核并发性能:     8,319,531 ops/sec (142.8 ns/op) - 简化测试
⚠️ 复杂规则匹配:     2,729,286 ops/sec (435.6 ns/op) - 简化测试
```
*注意: 上述数据来自简化测试，可能包含编译器优化和缓存效应*

#### ✅ 真实工作负载测试结果（推荐参考）
```
✅ 真实事件处理:      106,116 ops/sec (9,418 ns/op) - 包含复杂业务逻辑
✅ 网络I/O场景:       348 ops/sec (2.88 ms/op) - 模拟API调用延迟
✅ API限流场景:       17,611 ops/sec (56.8 μs/op) - 模拟QQ API限制
✅ 数据库场景:        约100-1000 ops/sec - 模拟DB查询延迟
✅ 代码覆盖率:       92%+ (新增 100+ 测试场景)
```

#### 📊 性能对比分析
| 测试类型 | 简化测试 | 真实测试 | 性能差异 | 说明 |
|---------|---------|---------|---------|------|
| 基础处理 | 4.88M ops/s | 106K ops/s | **46倍差异** | 编译器优化影响 |
| 网络I/O | N/A | 348 ops/s | N/A | 网络延迟主导 |
| API限流 | N/A | 17.6K ops/s | N/A | 真实API瓶颈 |

**结论**: 真实性能约为简化测试的 **1/50**，更符合生产环境预期

### 对象池性能提升

| 指标 | 无对象池 | 有对象池 | 提升 |
|------|---------|---------|------|
| Context创建 | 313.3 ns | 71.0 ns | **77.3% ↑** |
| 内存分配 | 507 B | 0 B | **100% ↓** |
| 分配次数 | 4 次 | 0 次 | **100% ↓** |
| 并发创建 | - | 14.25 ns | **95% ↑** |
| 池命中率 | - | 97.8%+ | - |


### 承载能力评估（基于真实测试）

基于真实工作负载测试结果的承载能力评估：

#### 🎯 框架层面性能（不含网络I/O）
| Bot规模 | 群组数量 | 消息量/小时 | 推荐配置 | QPS预估 | 评估 |
|---------|---------|------------|----------|---------|------|
| 小型 | <10群 | <1K | 2核/512MB | ~300 | ✅ 完美 |
| 中型 | 10-100群 | 1K-10K | 4核/2GB | ~3K | ✅ 轻松 |
| 大型 | 100-1000群 | 10K-100K | 8核/4GB | ~30K | ✅ 胜任 |
| 超大型 | >1000群 | >100K | 16核/8GB+ | ~50K | ✅ 理论可行 |

#### ⚠️ 实际瓶颈分析
| 场景类型 | 框架性能 | 实际瓶颈 | 最终QPS | 限制因素 |
|---------|---------|---------|---------|---------|
| 纯事件处理 | 106K ops/s | 无 | ~100K | 框架处理能力 |
| 包含API调用 | 106K ops/s | 348 ops/s | **348** | **网络延迟** |
| QQ API限制 | 106K ops/s | ~20 ops/s | **20** | **官方限流** |
| 数据库查询 | 106K ops/s | 100-1K ops/s | **100-1K** | **DB延迟** |

#### 📊 真实性能计算说明
- **框架处理**: 基于真实测试 ~106K ops/s（单核）
- **网络I/O**: 实测约348 ops/s（1-5ms延迟）
- **QQ API**: 官方限制约20 QPS
- **数据库**: 典型查询10-50ms，约100-1K ops/s

#### 🎯 200 QPS 能力确认
**结论**: 框架完全支持200 QPS
- 纯框架处理: 106,000 vs 200 = **530倍余量** ✅
- 包含网络I/O: 348 vs 200 = **1.7倍余量** ✅  
- **真实瓶颈**: QQ API限制（20 QPS）❌ 需要10个Bot账号

**资源使用预估**：
- 内存：平均每个Context ~33B，对象池可大幅减少分配
- CPU：主要消耗在规则匹配，简单规则性能更佳
- 网络：取决于消息发送频率和大小

**优化建议**：
1. **使用对象池（默认启用）**：内存减少100%，性能提升77%
2. **批量处理**：10-50个事件批量处理效果最佳
3. **规则优化**：避免复杂正则，优先使用字符串匹配
4. **合理设置匹配器数量**：建议<50个，超过100个性能显著下降
5. **网络I/O优化**：
   - 使用连接池减少连接开销
   - 实施API限流避免被官方限制
   - 考虑异步处理网络请求
6. **内存管理**：
   - 定期触发GC避免内存堆积
   - 避免大对象长时间持有
   - 使用内存池处理大数据包

**真实瓶颈优化**：
- 对于200 QPS需求，**主要瓶颈在网络I/O和API限制**
- 框架本身性能充足（106K ops/s vs 200需求）
- 重点优化网络调用和API使用策略

---

## 🧪 测试指南

### 快速开始

#### 运行所有测试
```bash
go test -v .
```

#### 快速测试（跳过长时间运行的测试）
```bash
go test -v -short .
```

#### 查看测试覆盖率
```bash
go test -cover .
```

### 测试类型

#### 1. 单元测试
```bash
# 运行所有单元测试
go test -v .

# 运行特定测试
go test -v -run TestEngine
go test -v -run TestContext
go test -v -run TestPlugin
```

#### 2. 并发测试
```bash
# 基本并发测试
go test -v -run TestConcurrent -short

# 高负载测试
go test -v -run TestHighLoad

# 压力测试
go test -v -run TestStress
```

#### 3. 基准测试

##### 简化基准测试（快速）
```bash
# 运行所有基准测试
go test -bench=. -benchmem

# 运行特定基准测试
go test -bench=BenchmarkEngine -benchmem

# 并发性能测试
go test -bench=Parallel -benchmem
```

##### ✅ 真实工作负载测试（推荐）
```bash
# 运行完整的真实性能测试套件
realistic_bench.bat

# 或者单独运行各项测试：

# 真实事件处理测试（包含复杂业务逻辑）
go test -bench=BenchmarkRealisticEventProcessing -benchmem

# 网络I/O性能测试（1-5ms延迟）
go test -bench=BenchmarkWithNetworkIO -benchmem

# QQ API限流模拟测试（20 QPS限制）
go test -bench=BenchmarkQQAPILimitSimulation -benchmem

# 内存压力测试（512MB压力 + GC）
go test -bench=BenchmarkMemoryPressure -benchmem

# 大数据包处理测试（1KB-10KB消息）
go test -bench=BenchmarkLargePayload -benchmem
```

##### 📊 性能对比分析
```bash
# 对比简化测试 vs 真实测试
echo "=== 简化测试 ==="
go test -bench=BenchmarkEngineProcessEvent$ -benchmem -count=1

echo "=== 真实测试 ==="
go test -bench=BenchmarkRealisticEventProcessing -benchmem -count=1
```

### 性能分析

#### CPU Profiling
```bash
# 生成CPU性能分析文件
go test -bench=. -cpuprofile=cpu.prof

# 查看分析结果
go tool pprof cpu.prof

# 在浏览器中查看
go tool pprof -http=:8080 cpu.prof
```

#### 内存 Profiling
```bash
# 生成内存分析文件
go test -bench=. -memprofile=mem.prof

# 查看分析结果
go tool pprof mem.prof

# 在浏览器中查看
go tool pprof -http=:8080 mem.prof
```

#### 在线监控
框架已集成 pprof，运行后访问：
```
http://localhost:6060/debug/pprof/
```

可用的端点：
- `/debug/pprof/profile` - CPU profile
- `/debug/pprof/heap` - 堆内存
- `/debug/pprof/goroutine` - Goroutine信息
- `/debug/pprof/block` - 阻塞分析

---

## 📈 基准测试结果

### ⚠️ 简化测试基准（2025-11-29）- 仅供参考

| 测试项 | 吞吐量 (ops/s) | 延迟 (ns/op) | 内存 (B/op) | 分配次数 |
|-------|---------------|-------------|------------|---------|
| 单事件处理 | 4,881,542 | 247.2 | 32 | 2 |
| 多核并发处理 | 8,319,531 | 142.8 | 33 | 2 |
| 10个匹配器 | 793,236 | 1,548 | 491 | 22 |
| 复杂规则 | 2,729,286 | 435.6 | 67 | 4 |

⚠️ **注意**: 上述数据可能受编译器优化影响，实际性能请参考下方真实测试

### ✅ 真实工作负载基准（2025-11-29）- 推荐参考

| 测试项 | 吞吐量 (ops/s) | 延迟 (ns/op) | 内存 (B/op) | 分配次数 | 说明 |
|-------|---------------|-------------|------------|---------|------|
| **真实事件处理** | **106,116** | **9,418** | 355 | 8 | 包含复杂逻辑+256MB内存压力 |
| **网络I/O模拟** | **348** | **2,877,905** | 4,941 | 61 | 包含1-5ms网络延迟 |
| **API限流模拟** | **17,611** | **56,800** | 151 | 4 | 模拟20 QPS限制 |

### ✅ 扩展真实负载套件（2025-12-02）

最新一次完整压测覆盖了单聊/群聊/批量全链路，推荐使用 `realistic_bench.bat` 一键复现实验：

```powershell
realistic_bench.bat
```

| 测试项 | 吞吐量 (ops/s) | 延迟 (ns/op) | 内存 (B/op) | 分配次数 | 额外指标 |
|-------|---------------|-------------|------------|---------|-----------|
| 真实事件处理 | ~158,000 | 6,300 | 329 | 11 | handlers_executed≈2.9e5/轮 |
| 网络I/O模拟 | ~314 | 3,200,000 | 565 | 9 | api_calls≈360/轮（1-5ms延迟） |
| 数据库模拟 | ~32 | 31,000,000 | 551 | 9 | db_queries≈50/轮（10-50ms） |
| 内存压力 | ~34,000 | 29,500 | 57,000 | 106 | 512MB ballast + GC 抖动 |
| 真实并发 | ~710 | 1,400,000 | 25,000 | 380 | total_handlers≈1.3k/轮 |
| C2C消息混合 | ~195,000 | 5,150 | 990 | 24 | attachments≈2.4e5，commands≈8.2e4 |
| 群聊富媒体突发 | ~925,000 | 1,080 | 1,295 | 28 | group_events≈1.0e6，media_bytes≈1.8e13 |
| 批量混合消息 | ~3,400 | 292,000 | 53,000 | 1,170 | 每批64个事件（群聊+单聊） |
| QQ API 限流 | ~16,300 | 61,300 | 141 | 6 | api_success≈24/QPS受20QPS限制 |
| 大数据包处理 | ~4,500 | 220,000 | 45,000 | 470 | 1-10KB 文本分片 |

> 💡 与“简化测试”对比：单事件处理依旧保持 4.8M ops/s 级别，但真实场景（复杂逻辑+IO+富媒体）更能反映最终部署数据。


### 性能差异对比

| 场景 | 简化测试 | 真实测试 | 性能比例 | 主要差异原因 |
|------|---------|---------|---------|-------------|
| 基础处理 | 4.88M ops/s | 106K ops/s | **1:46** | 编译器优化 + 缓存效应 |
| 网络场景 | 无测试 | 348 ops/s | - | 网络延迟主导 |
| API限制 | 无测试 | 17.6K ops/s | - | 限流机制 |

### Context & 对象池性能

| 测试项 | 吞吐量 (ops/s) | 延迟 (ns/op) | 内存 (B/op) | 分配次数 |
|-------|---------------|-------------|------------|---------|
| Context无池 | 3,642,304 | 313.3 | 507 | 4 |
| Context有池 | 16,344,253 | 71.00 | 0 | 0 |
| Context并行 | 93,820,364 | 14.25 | 0 | 0 |
| Engine自动释放 | 7,118,180 | 170.6 | 16 | 1 |
| Engine手动释放 | 7,063,239 | 168.3 | 16 | 1 |

### 规则匹配性能

| 规则类型 | 性能 (ops/s) | 延迟 (ns) | 内存 (B/op) |
|---------|-------------|----------|------------|
| 关键词匹配 | 11,519,398 | 102.4 | 24 |
| 复杂组合 | 6,092,176 | 199.9 | 33 |
| JSON解析规则 | 1,624,520 | 774.2 | 576 |
| Gjson规则 | 12,056,968 | 98.37 | 24 |

### 批量处理性能

| 批量大小 | 吞吐量 (ops/s) | 延迟 (ns/op) | 内存 (B/op) |
|---------|---------------|-------------|------------|
| 批量-1 | 4,846,533 | 247.8 | 33 |
| 批量-10 | 553,012 | 2,185 | 331 |
| 批量-50 | 112,021 | 11,180 | 1,692 |
| 批量-100 | 48,103 | 22,232 | 3,316 |
| 批量-200 | 28,003 | 42,635 | 6,646 |

### 匹配器扩展性能

| 匹配器数量 | 吞吐量 (ops/s) | 延迟 (ns/op) | 内存 (B/op) |
|----------|---------------|-------------|------------|
| 单个匹配器 | 4,896,243 | 247.6 | 33 |
| 10个匹配器 | 966,081 | 1,172 | 331 |
| 100个匹配器 | 114,877 | 10,420 | 3,480 |
| 复杂规则 | 3,094,825 | 379.5 | 50 |

### 零拷贝优化性能

| 解析方式 | 吞吐量 (ops/s) | 延迟 (ns/op) | 内存 (B/op) |
|---------|---------------|-------------|------------|
| 标准JSON解析 | 632,227 | 1,842 | 772 |
| Gjson解析 | 9,955,159 | 115.5 | 48 |
| 当前实现 | 9,782,246 | 132.4 | 49 |

### 正则表达式性能

| 类型 | 吞吐量 (ops/s) | 延迟 (ns/op) | 说明 |
|-----|---------------|-------------|------|
| 预编译简单正则 | 4,745,385 | 253.8 | 数字匹配 |
| 未编译简单正则 | 1,000,000 | 1,043 | 数字匹配 |
| 预编译复杂正则 | 798,423 | 1,545 | URL匹配 |
| 未编译复杂正则 | 15,758 | 79,434 | URL匹配 |
| 字符串包含 | 11,371,938 | 105.1 | 性能对比 |
| 字符串前缀 | 11,840,215 | 104.2 | 性能对比 |

---

## 🎯 性能优化建议

### DO ✅

1. **使用批量处理（v0.3.0+）** ⭐⭐⭐⭐⭐ 强烈推荐
   ```go
   // Webhook批量接收
   func handleWebhook(events []*dto.Payload) {
       engine.ProcessEventBatch(events, api)
       // 锁操作减少 99%+
       // 适用于 10+ 个事件批量
   }
   
   // 最佳批量大小
   const OptimalBatchSize = 50  // 平衡延迟和吞吐
   ```

2. **使用对象池（v0.2.0+）** ⭐⭐⭐⭐⭐ 推荐
   ```go
   // 推荐：使用自动释放（默认开启）
   engine := NewEngine()  // autoRelease = true
   
   engine.On(OnC2CMessage()).Handle(func(ctx *Context) {
       // 处理逻辑
   })
   
   // ProcessEvent 会自动释放 Context
   engine.ProcessEvent(NewContext(event, api))
   
   // 性能提升: 43%, 内存减少: 69%
   ```

2. **手动管理对象池（高级用法）**
   ```go
   // 禁用自动释放
   engine.SetAutoRelease(false)
   
   ctx := NewContext(event, api)
   engine.ProcessEvent(ctx)
   
   // 使用完后手动释放
   ctx.Release()
   ```

3. **限制匹配器数量**
   ```go
   // 推荐：使用规则组合
   engine.On(Or(
       OnKeyword("hello"),
       OnKeyword("hi"),
   )).Handle(handler)
   ```

4. **使用阻塞模式**
   ```go
   // 找到匹配后停止继续匹配
   engine.On(OnCommand("/start")).
       Handle(handler).
       SetBlock(true)
   ```

5. **设置优先级**
   ```go
   // 高频命令设置高优先级
   engine.On(OnKeyword("help")).
       Handle(handler).
       SetPriority(1)
   ```

6. **异步处理耗时操作（使用 Retain/Release 保护）**
   ```go
   engine.On(OnC2CMessage()).Handle(func(ctx *Context) {
       // 如果需要在 goroutine 中延长 ctx 生命周期
       ctx.Retain()
       go func() {
           defer ctx.Release()
           doHeavyWork(ctx)
       }()
   })
   ```

### DON'T ❌

1. ❌ **在 Release 后继续使用 Context**
   ```go
   ctx := NewContext(event, api)
   ctx.Release()
   // ❌ 错误：不要在释放后使用
   ctx.SetState("key", "value")  // 危险！
   ```

2. ❌ **忘记释放 Context（禁用自动释放时）**
   ```go
   engine.SetAutoRelease(false)
   ctx := NewContext(event, api)
   engine.ProcessEvent(ctx)
   // ❌ 错误：忘记释放会导致内存泄漏
   // ✅ 正确：ctx.Release()
   ```

3. ❌ **创建过多的匹配器**（建议 <50 个）
4. ❌ **在handler中执行阻塞操作**
5. ❌ **在并发环境直接访问 `ctx.State()`**
6. ❌ **忽略错误处理**

---

## 🔍 并发安全

### 线程安全的组件

✅ **Context.State**
```go
// ✅ 推荐：使用线程安全API
ctx.SetState("user_id", 123)
value, ok := ctx.GetState("user_id")
ctx.DeleteState("user_id")

// ❌ 不推荐：并发环境下不安全
ctx.State()["key"] = "value"
```

✅ **Engine**
```go
// ✅ 所有操作都是线程安全的
go engine.On(OnC2CMessage()).Handle(handler1)
go engine.On(OnGroupAtMessage()).Handle(handler2)
go engine.ProcessEvent(ctx)
```

✅ **PluginManager**
```go
// ✅ 所有操作都是线程安全的
go pm.Register(plugin1)
go pm.Register(plugin2)
```

### 并发测试

框架已通过以下并发测试：
- ✅ 100 goroutines 并发事件处理
- ✅ 50 goroutines 并发注册匹配器
- ✅ 1000 goroutines 高负载测试
- ✅ 50 goroutines 并发插件注册

---

## 📋 测试套件详情

### 测试统计

```
总测试数:     93 个
通过率:       100%
代码覆盖率:   87.9%
测试文件:     8 个
```

### 测试文件列表

| 文件 | 测试数 | 说明 |
|------|--------|------|
| bot_test.go | 10 | Bot初始化和配置 |
| engine_test.go | 14 | 事件引擎处理 |
| matcher_test.go | 15 | 匹配器功能 |
| context_test.go | 17 | 上下文操作 |
| plugin_test.go | 13 | 插件系统 |
| rules_test.go | 20 | 规则匹配 |
| engine_bench_test.go | 9 | 基准测试 |
| concurrency_test.go | 7 | 并发测试 |

### 测试覆盖的功能

#### Bot模块
- ✅ Bot创建和初始化
- ✅ 配置选项应用
- ✅ Webhook集成
- ✅ 自定义引擎
- ✅ 多Bot实例

#### Engine模块
- ✅ 事件处理流程
- ✅ 前/中/后处理器
- ✅ 匹配器注册和执行
- ✅ 阻塞模式
- ✅ 优先级处理

#### Context模块
- ✅ 事件数据访问
- ✅ 状态管理（线程安全）
- ✅ 消息发送
- ✅ 消息回复
- ✅ 事件解码
- ✅ 生命周期管理（Retain/Release）

#### Plugin模块
- ✅ 插件注册/注销
- ✅ 插件生命周期
- ✅ 插件管理器
- ✅ 并发安全

#### Rules模块
- ✅ 事件类型匹配
- ✅ 关键词匹配
- ✅ 命令匹配
- ✅ 前缀/后缀匹配
- ✅ 规则组合（And/Or/Not）

---

## 🔧 故障排查

### 性能问题

#### 症状：吞吐量低
**可能原因**：
1. 匹配器数量过多
2. 规则过于复杂
3. Handler中有阻塞操作

**解决方案**：
```bash
# 1. 运行性能分析
go test -bench=. -cpuprofile=cpu.prof
go tool pprof -http=:8080 cpu.prof

# 2. 检查匹配器数量
# 3. 简化规则逻辑
# 4. 将耗时操作移到goroutine
```

#### 症状：内存使用过高
**可能原因**：
1. 内存泄漏
2. Goroutine泄漏
3. 大量对象分配

**解决方案**：
```bash
# 运行内存分析
go test -bench=. -memprofile=mem.prof
go tool pprof -http=:8080 mem.prof

# 检查goroutine数量
curl http://localhost:6060/debug/pprof/goroutine?debug=1
```

### 并发问题

#### 症状：数据竞争
**检测方法**：
```bash
go test -race .
```

**解决方案**：
- 使用 `ctx.SetState/GetState` 而不是直接访问
- 确保自定义代码是线程安全的

---

## 📊 监控指标

### 关键指标

#### 1. 吞吐量 (events/sec)
```
正常: >100,000
警告: <10,000
严重: <1,000
```

#### 2. 延迟 (ms)
```
正常: <10ms
警告: 10-100ms
严重: >100ms
```

#### 3. 内存使用 (MB)
```
正常: <500MB
警告: 500MB-2GB
严重: >2GB
```

#### 4. Goroutine数量
```
正常: <10,000
警告: 10,000-50,000
严重: >50,000
```

### 监控命令

```bash
# 查看当前goroutine数
curl http://localhost:6060/debug/pprof/goroutine?debug=1 | grep goroutine | wc -l

# 查看内存使用
curl http://localhost:6060/debug/pprof/heap?debug=1 | grep Alloc

# 实时监控
watch -n 1 'curl -s http://localhost:6060/debug/pprof/goroutine?debug=1 | grep goroutine | wc -l'
```

---

## 🎓 最佳实践

### 测试最佳实践

1. ✅ 每个功能都有对应的单元测试
2. ✅ 使用表驱动测试处理多个场景
3. ✅ Mock外部依赖
4. ✅ 测试并发场景
5. ✅ 定期运行基准测试

### 性能最佳实践

1. ✅ 控制匹配器数量
2. ✅ 使用规则组合而非多个匹配器
3. ✅ 合理使用SetBlock
4. ✅ 耗时操作使用goroutine
5. ✅ 定期进行性能分析

### 并发最佳实践

1. ✅ 使用线程安全的API
2. ✅ 避免共享可变状态
3. ✅ 使用Context传递数据
4. ✅ 正确处理goroutine生命周期
5. ✅ 使用sync包工具

---

## 📖 相关文档

- [快速入门](QUICKSTART.md) - 5分钟上手
- [使用指南](GUIDE.md) - 详细的API文档
- [并发安全](CONCURRENCY_FIX.md) - 并发安全说明
- [架构设计](ARCHITECTURE.md) - 框架架构

---

**最后更新**: 2025-11-29  
**框架版本**: v1.0.0
