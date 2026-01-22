# Remilia Errors Package

[![Go Reference](https://pkg.go.dev/badge/github.com/KomeiDiSanXian/remilia/errors.svg)](https://pkg.go.dev/github.com/KomeiDiSanXian/remilia/errors)

通用错误处理工具包，提供错误包装、类型检查、堆栈追踪等功能。

## 📦 安装

```go
import "github.com/KomeiDiSanXian/remilia/errors"
```

## 🎯 核心功能

### 1. 预定义错误

框架提供了一组稳定的预定义错误，可以使用 `errors.Is` 进行检查：

```go
var (
    ErrConfigInvalid     = errors.New("invalid configuration")
    ErrMatcherNotFound   = errors.New("matcher not found")
    ErrContextReleased   = errors.New("context already released")
    ErrEngineShutdown    = errors.New("engine is shutting down")
    ErrInvalidEventID    = errors.New("invalid event ID")
    ErrHandlerNotSet     = errors.New("handler not set")
    ErrRuleCompileFailed = errors.New("rule compile failed")
    ErrDedupCacheFull    = errors.New("dedup cache full")
    ErrDeadLetterFailed  = errors.New("dead letter processing failed")
)
```

**使用示例：**

```go
if errors.IsErrorType(err, errors.ErrConfigInvalid) {
    log.Println("Configuration error detected")
}
```

### 2. 错误包装

提供带上下文信息的错误包装：

```go
// 简单包装
err := errors.WrapErrorf(originalErr, "operation failed")

// 带上下文包装
err := errors.WrapErrorWithContextf(
    originalErr, 
    "database query failed",
    "user_id=123, table=users"
)
```

**ErrorWrapper 结构：**

```go
type ErrorWrapper struct {
    Err     error  // 原始错误
    Message string // 错误消息
    Context string // 上下文信息（可选）
}
```

### 3. 错误类型检查

```go
// 检查错误类型
if errors.IsErrorType(err, errors.ErrContextReleased) {
    // 处理上下文已释放错误
}

// 使用 errors.Is (标准库)
if errors.Is(err, errors.ErrEngineShutdown) {
    // 引擎已关闭
}

// 使用 errors.As (标准库)
var wrapper *errors.ErrorWrapper
if errors.As(err, &wrapper) {
    log.Printf("Context: %s", wrapper.Context)
}
```

### 4. 堆栈追踪

可选的堆栈追踪功能，用于调试：

```go
// 启用堆栈追踪
errors.EnableStackTrace(true)

// 检查是否启用
if errors.IsStackTraceEnabled() {
    // ...
}

// 手动捕获堆栈
stack := errors.CaptureStack()
log.Printf("Stack trace:\n%s", stack)
```

**环境变量控制：**

```bash
export REMILIA_STACK_TRACE=true
```

### 5. Panic 恢复

将 panic 转换为错误：

```go
func riskyOperation() (err error) {
    defer func() {
        if recovered := errors.RecoverError(); recovered != nil {
            err = recovered
        }
    }()
    
    // 可能 panic 的代码
    panic("something went wrong")
}
```

### 6. 便捷错误创建

```go
// 验证错误
err := errors.NewValidationError("email", "invalid format")
// Output: validation failed for field 'email': invalid format

// 配置错误
err := errors.NewConfigError("port", "must be between 1-65535")
// 可以用 errors.Is 检查 ErrConfigInvalid

// 插件错误
err := errors.NewPluginError("auth-plugin", "failed to initialize")
// Output: plugin 'auth-plugin': failed to initialize
```

## 📚 完整示例

### 基础错误处理

```go
package main

import (
    "fmt"
    "github.com/KomeiDiSanXian/remilia/errors"
)

func connectDatabase(dsn string) error {
    if dsn == "" {
        return errors.NewConfigError("dsn", "cannot be empty")
    }
    
    // 模拟数据库连接失败
    originalErr := fmt.Errorf("connection timeout")
    return errors.WrapErrorWithContextf(
        originalErr,
        "failed to connect to database",
        fmt.Sprintf("dsn=%s", dsn),
    )
}

func main() {
    err := connectDatabase("")
    if err != nil {
        // 检查是否是配置错误
        if errors.IsErrorType(err, errors.ErrConfigInvalid) {
            fmt.Println("Configuration error:", err)
        }
    }
}
```

### 错误链处理

```go
func processRequest() error {
    err := validateInput()
    if err != nil {
        return errors.WrapErrorf(err, "request validation failed")
    }
    
    err = saveToDatabase()
    if err != nil {
        return errors.WrapErrorWithContextf(
            err,
            "database operation failed",
            "operation=insert, table=users",
        )
    }
    
    return nil
}

func handleRequest() {
    err := processRequest()
    if err != nil {
        // 可以追溯到原始错误
        var validationErr *ValidationError
        if errors.As(err, &validationErr) {
            log.Printf("Validation failed: %v", validationErr)
        }
        
        // 检查错误链
        fmt.Printf("Error: %v\n", err)
    }
}
```

### 使用堆栈追踪

```go
func debugMode() {
    // 启用堆栈追踪（仅用于调试）
    errors.EnableStackTrace(true)
    defer errors.EnableStackTrace(false)
    
    err := riskyOperation()
    if err != nil {
        // 错误现在包含堆栈信息
        log.Printf("Error with stack: %+v", err)
    }
}
```

## 🔄 向后兼容

Root 包提供了完整的兼容层，旧代码无需修改：

```go
import "github.com/KomeiDiSanXian/remilia"

// 旧 API 仍然可用（通过 root 包）
err := remilia.WrapErrorf(baseErr, "operation failed")
if remilia.IsErrorType(err, remilia.ErrConfigInvalid) {
    // ...
}
```

**推荐迁移到新 API：**

```go
import "github.com/KomeiDiSanXian/remilia/errors"

// 新 API（推荐）
err := errors.WrapErrorf(baseErr, "operation failed")
if errors.IsErrorType(err, errors.ErrConfigInvalid) {
    // ...
}
```

## 🎨 最佳实践

### 1. 使用预定义错误

```go
// ✅ 好：使用预定义错误
if err != nil {
    return errors.ErrConfigInvalid
}

// ❌ 避免：创建临时错误
if err != nil {
    return fmt.Errorf("invalid configuration")
}
```

### 2. 添加上下文信息

```go
// ✅ 好：添加有用的上下文
return errors.WrapErrorWithContextf(
    err,
    "failed to process user",
    fmt.Sprintf("user_id=%s, operation=%s", userID, op),
)

// ❌ 避免：丢失上下文
return err
```

### 3. 错误检查优先级

```go
// 1. 首先检查特定错误类型
if errors.IsErrorType(err, errors.ErrConfigInvalid) {
    // 处理配置错误
}

// 2. 然后检查错误包装器
var wrapper *errors.ErrorWrapper
if errors.As(err, &wrapper) {
    log.Printf("Context: %s", wrapper.Context)
}

// 3. 最后通用错误处理
if err != nil {
    log.Printf("Error: %v", err)
}
```

### 4. 堆栈追踪仅用于调试

```go
// ✅ 好：仅在开发/调试时启用
if os.Getenv("DEBUG") == "true" {
    errors.EnableStackTrace(true)
}

// ❌ 避免：在生产环境中始终启用（性能影响）
errors.EnableStackTrace(true) // 不要这样做
```

## 🔧 性能考虑

- **错误包装**: 零额外内存分配（除了 ErrorWrapper 本身）
- **类型检查**: O(1) 时间复杂度
- **堆栈追踪**: 仅在启用时有性能开销（约 1-2μs）

## 📖 API 文档

完整的 API 文档请查看：
- [pkg.go.dev](https://pkg.go.dev/github.com/KomeiDiSanXian/remilia/errors)
- [GoDoc](https://godoc.org/github.com/KomeiDiSanXian/remilia/errors)

## 🆚 与标准库的关系

本包是对 Go 标准库 `errors` 包的扩展，完全兼容：

- 使用 `errors.Is` 和 `errors.As` 进行错误检查
- 实现 `Unwrap()` 方法支持错误链
- 可以与标准库无缝配合使用

## 📝 变更日志

### v0.9.0 (2026-01-20)

- ✨ 初始发布
- 🎯 从 root 包迁移
- 📦 独立的错误处理包
- ✅ 100% 向后兼容
- 📖 完整文档

## 📄 许可证

Apache License 2.0 - 查看 [LICENSE](../LICENSE) 文件
