# 测试套件文档

本文档描述了 Remilia 项目的完整测试体系。

## 测试结构

```
tests/
├── integration/     # 集成测试（端到端场景）
├── benchmark/       # 性能测试（基准测试）
├── chaos/          # 压力测试（混沌工程）
└── fuzzing/        # Fuzzing 测试（模糊测试）
```

---

## 1. 集成测试 (Integration Tests)

### 位置
`tests/integration/e2e_test.go`

### 测试场景

#### 基本功能测试
- ✅ `TestE2E_BasicCommandFlow` - 基本命令流程
- ✅ `TestE2E_CommandWithArguments` - 带参数的命令
- ✅ `TestE2E_MiddlewareChain` - 中间件链执行顺序
- ✅ `TestE2E_ErrorHandling` - 错误处理

#### 高级功能测试
- ✅ `TestE2E_AuditLogging` - 审计日志集成
- ✅ `TestE2E_TempMatcher` - 临时匹配器
- ✅ `TestE2E_ConcurrentEvents` - 并发事件处理
- ✅ `TestE2E_BatchRegistration` - 批量注册

#### 生命周期测试
- ✅ `TestE2E_PluginLifecycle` - 插件生命周期
- ✅ `TestE2E_GracefulShutdown` - 优雅关闭
- ✅ `TestE2E_FullBotLifecycle` - 完整 Bot 生命周期

### 运行方式

```bash
# 运行所有集成测试
go test ./tests/integration -v

# 运行特定测试
go test ./tests/integration -run TestE2E_BasicCommandFlow -v

# 跳过长时间运行的测试
go test ./tests/integration -short -v
```

---

## 2. 性能测试 (Benchmark Tests)

### 位置
`tests/benchmark/benchmark_test.go`

### 基准测试

#### 核心性能
- ⚡ `BenchmarkEngineProcessEvent` - 事件处理性能
- ⚡ `BenchmarkEngineProcessEventParallel` - 并行事件处理
- ⚡ `BenchmarkMatcherRegistration` - 匹配器注册
- ⚡ `BenchmarkBatchMatcherRegistration` - 批量注册

#### 命令解析
- ⚡ `BenchmarkCommandParsing` - 简单命令解析
- ⚡ `BenchmarkCommandParsingComplex` - 复杂命令解析
- ⚡ `BenchmarkTrieOperations` - Trie 树操作

#### 中间件和上下文
- ⚡ `BenchmarkMiddlewareChain` - 中间件链（1/5/10/20个）
- ⚡ `BenchmarkContextOperations` - Context 操作
- ⚡ `BenchmarkLoggerOperations` - Logger 操作

#### 高级特性
- ⚡ `BenchmarkTempMatcherOperations` - 临时匹配器
- ⚡ `BenchmarkCOWOperations` - COW 操作
- ⚡ `BenchmarkMemoryAllocation` - 内存分配
- ⚡ `BenchmarkComparisonTable` - 性能对比表

### 运行方式

```bash
# 运行所有基准测试
go test ./tests/benchmark -bench=. -benchmem

# 运行特定基准测试
go test ./tests/benchmark -bench=BenchmarkEngineProcessEvent -benchmem

# 生成性能报告
go test ./tests/benchmark -bench=. -benchmem -cpuprofile=cpu.prof -memprofile=mem.prof

# 对比性能
go test ./tests/benchmark -bench=. -benchmem > old.txt
# ... 修改代码 ...
go test ./tests/benchmark -bench=. -benchmem > new.txt
benchcmp old.txt new.txt
```

### 性能指标

典型性能指标（参考）：

| 测试 | 操作耗时 | 内存分配 | 分配次数 |
|------|---------|---------|---------|
| EngineProcessEvent | ~1-5 µs | ~500 B | ~5 allocs |
| CommandParsing | ~2-10 µs | ~1 KB | ~10 allocs |
| MiddlewareChain(5) | ~3-8 µs | ~800 B | ~8 allocs |

---

## 3. 压力测试 (Chaos Tests)

### 位置
`tests/chaos/chaos_test.go`

### 混沌场景

#### 故障注入
- 💥 `TestChaos_RandomFailures` - 随机失败（30%失败率）
- 💥 `TestChaos_TimeoutHandling` - 超时处理
- 💥 `TestChaos_CascadingFailures` - 级联失败

#### 高负载
- 💥 `TestChaos_HighConcurrency` - 高并发（500并发）
- 💥 `TestChaos_MemoryPressure` - 内存压力
- 💥 `TestChaos_StressTest` - 综合压力测试

#### 资源限制
- 💥 `TestChaos_ResourceExhaustion` - 资源耗尽
- 💥 `TestChaos_SlowHandler` - 慢处理器
- 💥 `TestChaos_GracefulDegradation` - 优雅降级

#### 动态场景
- 💥 `TestChaos_RapidRegistrationUnregistration` - 快速注册/注销
- 💥 `TestChaos_MixedOperations` - 混合操作

### 运行方式

```bash
# 运行所有混沌测试
go test ./tests/chaos -v -timeout 30m

# 运行特定混沌测试
go test ./tests/chaos -run TestChaos_HighConcurrency -v

# 跳过混沌测试（它们很慢）
go test ./tests/chaos -short -v
```

### 混沌测试报告示例

```
压力测试结果:
  总请求数: 10000
  成功: 9000 (90.00%)
  失败: 1000 (10.00%)
  耗时: 10.5s
  QPS: 952.38
```

---

## 4. Fuzzing 测试 (Fuzzing Tests)

### 位置
`tests/fuzzing/fuzzing_test.go`

### Fuzzing 场景

#### 输入验证
- 🔍 `FuzzEventPayload` - 事件负载
- 🔍 `FuzzCommandParsing` - 命令解析
- 🔍 `FuzzArgumentParsing` - 参数解析
- 🔍 `FuzzJSONDecoding` - JSON 解码

#### 核心功能
- 🔍 `FuzzEngineProcessEvent` - Engine 事件处理
- 🔍 `FuzzContextOperations` - Context 操作
- 🔍 `FuzzTrieOperations` - Trie 树操作
- 🔍 `FuzzMatcherRules` - 匹配规则

#### 边界条件
- 🔍 `FuzzSpecialCharacters` - 特殊字符
- 🔍 `FuzzMemoryBounds` - 内存边界
- 🔍 `FuzzMiddlewareChain` - 中间件链
- 🔍 `FuzzConcurrentOperations` - 并发操作

### 运行方式

```bash
# 运行单个 fuzz 测试（Go 1.18+）
go test ./tests/fuzzing -fuzz=FuzzCommandParsing -fuzztime=30s

# 使用语料库
go test ./tests/fuzzing -fuzz=FuzzCommandParsing -fuzztime=1m

# 查看覆盖率
go test ./tests/fuzzing -fuzz=FuzzCommandParsing -fuzztime=10s -coverprofile=fuzz.out
go tool cover -html=fuzz.out
```

### Fuzzing 最佳实践

1. **种子语料库**: 提供有代表性的输入
2. **边界条件**: 测试空值、极大值、特殊字符
3. **不应panic**: 任何输入都不应导致 panic
4. **错误处理**: 验证错误类型合理
5. **语料库管理**: 保存有价值的输入

---

## 测试覆盖率

### 查看覆盖率

```bash
# 生成覆盖率报告
go test ./... -coverprofile=coverage.out

# 查看 HTML 报告
go tool cover -html=coverage.out

# 查看包级别覆盖率
go tool cover -func=coverage.out
```

### 目标覆盖率

| 模块 | 目标 | 当前 |
|------|-----|------|
| core/engine | 85%+ | - |
| core/context | 80%+ | - |
| command | 80%+ | - |
| middleware | 75%+ | - |

---

## CI/CD 集成

### GitHub Actions 示例

```yaml
name: Tests

on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
        with:
          go-version: '1.21'
      
      # 单元测试
      - name: Unit Tests
        run: go test ./... -short -v -coverprofile=coverage.out
      
      # 基准测试
      - name: Benchmark Tests
        run: go test ./tests/benchmark -bench=. -benchmem
      
      # Fuzzing（短时间）
      - name: Fuzz Tests
        run: |
          go test ./tests/fuzzing -fuzz=FuzzCommandParsing -fuzztime=10s
          go test ./tests/fuzzing -fuzz=FuzzEventPayload -fuzztime=10s
      
      # 覆盖率报告
      - name: Upload Coverage
        uses: codecov/codecov-action@v3
        with:
          file: ./coverage.out

  integration:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
      
      # 集成测试
      - name: Integration Tests
        run: go test ./tests/integration -v
  
  chaos:
    runs-on: ubuntu-latest
    if: github.event_name == 'push' && github.ref == 'refs/heads/main'
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
      
      # 混沌测试（仅主分支）
      - name: Chaos Tests
        run: go test ./tests/chaos -v -timeout 30m
```

---

## 本地测试脚本

### 全量测试脚本 (`test-all.sh`)

```bash
#!/bin/bash

echo "================================"
echo "  Remilia 测试套件"
echo "================================"
echo ""

# 1. 单元测试
echo "[1/4] 运行单元测试..."
go test ./... -short -v -coverprofile=coverage.out
if [ $? -ne 0 ]; then
    echo "❌ 单元测试失败"
    exit 1
fi
echo "✅ 单元测试通过"
echo ""

# 2. 集成测试
echo "[2/4] 运行集成测试..."
go test ./tests/integration -v
if [ $? -ne 0 ]; then
    echo "❌ 集成测试失败"
    exit 1
fi
echo "✅ 集成测试通过"
echo ""

# 3. 基准测试
echo "[3/4] 运行基准测试..."
go test ./tests/benchmark -bench=. -benchmem -benchtime=1s
if [ $? -ne 0 ]; then
    echo "❌ 基准测试失败"
    exit 1
fi
echo "✅ 基准测试通过"
echo ""

# 4. 覆盖率
echo "[4/4] 生成覆盖率报告..."
go tool cover -func=coverage.out | tail -1
echo ""

echo "================================"
echo "  ✅ 所有测试通过！"
echo "================================"
```

### 快速测试脚本 (`test-quick.sh`)

```bash
#!/bin/bash

echo "运行快速测试..."
go test ./... -short -count=1
```

---

## 故障排查

### 常见问题

**Q: 测试超时**
```bash
# 增加超时时间
go test ./tests/chaos -timeout 30m
```

**Q: 内存不足**
```bash
# 限制并发测试
go test ./... -p 1
```

**Q: Fuzzing 找到崩溃**
```bash
# 查看崩溃输入
cat testdata/fuzz/FuzzCommandParsing/<hash>

# 重现崩溃
go test ./tests/fuzzing -run=FuzzCommandParsing/<hash>
```

---

## 性能优化建议

基于测试结果的优化建议：

1. **高频操作优化**
   - 使用对象池减少分配
   - 缓存重复计算结果
   - 避免不必要的复制

2. **并发优化**
   - 使用 COW 模式减少锁竞争
   - 批量操作减少同步开销
   - 合理使用 goroutine 池

3. **内存优化**
   - 复用缓冲区
   - 及时释放大对象
   - 监控 GC 压力

---

## 附录

### 测试工具

- **testing**: Go 标准测试框架
- **testify**: 断言库
- **pprof**: 性能分析
- **go-fuzz**: Fuzzing 工具（Go 1.18+ 内置）

### 相关文档

- [Go Testing Guide](https://go.dev/doc/tutorial/add-a-test)
- [Benchmarking](https://pkg.go.dev/testing#hdr-Benchmarks)
- [Fuzzing](https://go.dev/security/fuzz/)
- [Table-Driven Tests](https://dave.cheney.net/2019/05/07/prefer-table-driven-tests)

---

**文档更新**: 2026-02-01  
**维护者**: Remilia Team
