# Helper Package - 测试文档

## 📊 测试概览

本测试套件为 `helper` 包提供了全面的测试覆盖，包括 unsafe 转换、字符串处理、哈希计算和泛型解析。

### 测试统计

- **总测试数**: 37 个测试用例（含子测试）
- **代码覆盖率**: 100.0%
- **测试文件**: 2 个
  - `helper_test.go` - 功能测试
  - `benchmark_test.go` - 性能基准测试

## 🧪 测试文件说明

### 1. helper_test.go

测试所有辅助函数的功能正确性。

**主要测试点**:

#### BytesToString 测试（7 个子测试）
- ✅ 正常字符串
- ✅ 空字符串
- ✅ 中文字符
- ✅ 特殊字符
- ✅ 数字
- ✅ 混合内容
- ✅ 换行和制表符

#### StringToBytes 测试（5 个子测试）
- ✅ 各种类型的字符串转换
- ✅ 空字符串处理
- ✅ Unicode 字符支持

#### 往返转换测试（7 个子测试）
- ✅ String -> Bytes -> String 转换
- ✅ 验证数据完整性
- ✅ 各种字符集测试

#### HideURL 测试（6 个子测试）
- ✅ HTTPS URL 隐藏（替换为 🔒）
- ✅ HTTP URL 隐藏（替换为 📄）
- ✅ 点号替换（替换为"点"）
- ✅ 多个点号处理
- ✅ URL 参数保留
- ✅ 空字符串处理

#### FNVHash 测试（5 个子测试）
- ✅ 不同输入的哈希计算
- ✅ 空字符串哈希
- ✅ 长字符串哈希
- ✅ 中文字符哈希
- ✅ 哈希格式验证（十六进制）

#### FNVHash 一致性测试（4 个子测试）
- ✅ 相同输入多次哈希结果一致
- ✅ 不同输入类型测试

#### FNVHash 唯一性测试
- ✅ 验证不同输入产生不同哈希
- ✅ 哈希冲突检测

#### ParseEvent 测试（3 个子测试）
- ✅ 有效事件解析
- ✅ 空 Payload 处理
- ✅ 无效 JSON 错误处理

#### ParseEvent 不同类型测试（1 个测试）
- ✅ 嵌套结构解析
- ✅ 泛型类型支持

### 2. benchmark_test.go

性能基准测试和示例代码。

**基准测试项目**:
- ✅ BytesToString 性能测试
- ✅ BytesToString vs 标准转换对比
- ✅ StringToBytes 性能测试
- ✅ StringToBytes vs 标准转换对比
- ✅ HideURL 性能测试（3 个场景）
- ✅ FNVHash 性能测试（3 个大小）
- ✅ ParseEvent 性能测试

**Example 函数**:
- ✅ ExampleBytesToString
- ✅ ExampleStringToBytes
- ✅ ExampleHideURL

## 🎯 测试覆盖率详情

### 覆盖率: 100.0%

所有函数全覆盖：
- ✅ `BytesToString` - 100%
- ✅ `StringToBytes` - 100%
- ✅ `HideURL` - 100%
- ✅ `FNVHash` - 100%
- ✅ `ParseEvent` - 100%

**测试覆盖的场景**:
- 正常输入处理
- 边界值测试（空字符串、极长字符串）
- 特殊字符处理（中文、emoji、特殊符号）
- 错误处理（无效 JSON）
- 零拷贝验证
- 哈希一致性和唯一性
- 泛型类型支持

## 🚀 运行测试

### 运行所有测试
```bash
go test -v
```

### 运行特定测试
```bash
go test -v -run TestBytesToString
go test -v -run TestHideURL
go test -v -run TestFNVHash
go test -v -run TestParseEvent
```

### 生成覆盖率报告
```bash
go test -coverprofile coverage.out -cover
go tool cover -func coverage.out
go tool cover -html coverage.out  # 生成 HTML 报告
```

### 运行基准测试
```bash
# 运行所有基准测试
go test -bench=. -benchmem

# 运行特定基准测试
go test -bench=BenchmarkBytesToString -benchmem
go test -bench=BenchmarkFNVHash -benchmem

# 对比测试
go test -bench=vs_Standard -benchmem
```

### 运行示例
```bash
go test -run Example
```

## 📝 测试最佳实践

本测试套件遵循以下最佳实践：

1. **表驱动测试** - 使用结构体数组组织测试用例
2. **子测试** - 使用 `t.Run()` 组织相关测试
3. **边界条件** - 测试空输入、极端值
4. **特殊字符** - 测试 Unicode、emoji、中文等
5. **错误处理** - 验证错误场景
6. **性能基准** - 对比 unsafe 和标准方法
7. **示例代码** - 提供可执行的示例
8. **100% 覆盖** - 所有代码路径都有测试

## 🔍 测试详情

### BytesToString & StringToBytes

这两个函数使用 `unsafe` 包进行零拷贝转换。

**测试要点**:
- ✅ 转换正确性 - 内容完全一致
- ✅ 零拷贝特性 - 无内存分配
- ✅ 往返转换 - 可逆转换
- ✅ 各种字符集 - UTF-8 完全支持

**性能对比**（基准测试）:
```
unsafe 转换:    0 allocs/op
标准转换:       1 alloc/op
```

### HideURL

URL 隐藏函数，用于日志输出时保护敏感信息。

**转换规则**:
- `https://` → 🔒
- `http://` → 📄
- `.` → 点

**测试场景**:
- ✅ 各种协议（http/https）
- ✅ 域名和路径
- ✅ 查询参数保留
- ✅ 多个点号处理
- ✅ 空字符串边界情况

### FNVHash

FNV-1a 64位哈希函数，返回十六进制字符串。

**测试要点**:
- ✅ 哈希一致性 - 相同输入产生相同输出
- ✅ 哈希唯一性 - 不同输入产生不同输出
- ✅ 格式验证 - 十六进制字符串
- ✅ 长度验证 - 最多 16 个字符

**应用场景**:
- 快速哈希计算
- 缓存键生成
- 去重标识

### ParseEvent

泛型事件解析器，使用 Go 1.18+ 泛型特性。

**测试要点**:
- ✅ 基本类型解析
- ✅ 嵌套结构支持
- ✅ 空 Payload 处理
- ✅ JSON 错误处理
- ✅ 泛型类型推导

**支持的类型**:
- 基本类型（string, int, float, bool）
- 复杂结构
- 嵌套对象
- 数组和切片

## 📚 依赖

- `github.com/stretchr/testify` - 测试断言库
- `github.com/KomeiDiSanXian/remilia/openapi/dto` - DTO 定义

## 🧩 函数说明

### BytesToString
```go
func BytesToString(b []byte) string
```
零拷贝字节切片到字符串转换。

**注意**: 返回的字符串不应修改原始字节切片。

### StringToBytes
```go
func StringToBytes(s string) []byte
```
零拷贝字符串到字节切片转换。

### HideURL
```go
func HideURL(url string) string
```
隐藏 URL 中的协议和点号，用于安全日志输出。

### FNVHash
```go
func FNVHash(s string) string
```
计算字符串的 FNV-1a 哈希值并返回十六进制字符串。

### ParseEvent
```go
func ParseEvent[T any](p *dto.Payload) (*T, error)
```
泛型事件解析器，将 Payload 解析为指定类型。

## ✅ 测试状态

- 所有测试通过 ✅
- 代码覆盖率: 100.0% ✅
- 性能基准建立 ✅
- 零拷贝验证 ✅
- 哈希一致性验证 ✅
- 泛型支持验证 ✅

## 🔧 未来改进

可以考虑的测试增强：

1. **并发测试** - 测试并发安全性
2. **压力测试** - 大量数据处理
3. **内存泄漏检测** - 使用 pprof
4. **更多边界情况** - 极端输入测试
5. **模糊测试** - Fuzz testing

---

**最后更新**: 2026-01-22
**维护者**: Remilia 开发团队
