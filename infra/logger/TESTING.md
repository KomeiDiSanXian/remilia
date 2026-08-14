# Logger 包 - 测试文档

## 测试文件

| 文件 | 内容 |
| --- | --- |
| `logger_test.go` | 全局 logger、配置与链式字段 API 测试 |
| `pool_test.go` | Fields 对象池测试与性能基准 |

## 测试用例

### logger_test.go

- `TestSetLevel` — 设置合法 / 非法 / 空日志级别（空级别视为 NoLevel，返回 nil）
- `TestSetTimeFormat` — 设置自定义时间格式；空字符串不生效
- `TestGlobalLogger` — 包级日志函数输出（Info/Debug/Warn/Error 及 f 版本）
- `TestWithFields` / `TestWithField` / `TestWithError` — 链式字段 API
- `TestPackageLevelCaller` — 包级 Error 的 caller 指向真实调用方，而非 logger 包内部
- `TestLogWithFieldsAllLevels` — LogWithFields 全级别方法输出
- `TestLogWithFieldsPanic` — Panic 级别输出并触发 panic
- `TestInitEmptyLevelDefaultsToInfo` — Init 未配置 level 时默认 info，避免关闭全部日志

### pool_test.go

- `TestFieldsPool` — GetFields / PutFields 的复用与清空语义
- `BenchmarkFieldsWithoutPool` / `BenchmarkFieldsWithPool` / `BenchmarkFieldsWithPoolParallel` — 对象池性能对比

## 运行测试

```bash
go test ./infra/logger/
go test -race ./infra/logger/
go test -cover ./infra/logger/
go test -bench=. -benchmem ./infra/logger/
```
