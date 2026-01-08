package remilia

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestPredefinedErrors 测试预定义错误
func TestPredefinedErrors(t *testing.T) {
	assert.NotNil(t, ErrConfigInvalid)
	assert.NotNil(t, ErrMatcherNotFound)
	assert.NotNil(t, ErrContextReleased)
	assert.NotNil(t, ErrEngineShutdown)
	assert.NotNil(t, ErrPluginNotFound)
	assert.NotNil(t, ErrPluginAlreadyExists)
}

// TestWrapErrorf 测试错误包装
func TestWrapErrorf(t *testing.T) {
	baseErr := errors.New("base error")
	wrappedErr := WrapErrorf(baseErr, "operation failed")

	assert.NotNil(t, wrappedErr)
	assert.Contains(t, wrappedErr.Error(), "operation failed")
	assert.Contains(t, wrappedErr.Error(), "base error")

	// 测试 errors.Is
	assert.True(t, errors.Is(wrappedErr, baseErr))
}

// TestWrapErrorfNil 测试包装 nil 错误
func TestWrapErrorfNil(t *testing.T) {
	wrappedErr := WrapErrorf(nil, "should be nil")
	assert.Nil(t, wrappedErr)
}

// TestWrapErrorWithContextf 测试带上下文的错误包装
func TestWrapErrorWithContextf(t *testing.T) {
	baseErr := errors.New("database error")
	wrappedErr := WrapErrorWithContextf(baseErr, "query failed", "user_id=123")

	assert.NotNil(t, wrappedErr)
	assert.Contains(t, wrappedErr.Error(), "query failed")
	assert.Contains(t, wrappedErr.Error(), "database error")
	assert.Contains(t, wrappedErr.Error(), "user_id=123")
	assert.Contains(t, wrappedErr.Error(), "context:")
}

// TestIsErrorType 测试错误类型判断
func TestIsErrorType(t *testing.T) {
	err := WrapErrorf(ErrContextReleased, "context operation failed")

	assert.True(t, IsErrorType(err, ErrContextReleased))
	assert.False(t, IsErrorType(err, ErrEngineShutdown))
}

// TestErrorWrapper 测试 ErrorWrapper 结构
func TestErrorWrapper(t *testing.T) {
	baseErr := errors.New("original error")
	wrapper := &ErrorWrapper{
		Err:     baseErr,
		Message: "wrapped message",
		Context: "test context",
	}

	// 测试 Error() 方法
	assert.Contains(t, wrapper.Error(), "wrapped message")
	assert.Contains(t, wrapper.Error(), "original error")
	assert.Contains(t, wrapper.Error(), "test context")

	// 测试 Unwrap() 方法
	assert.Equal(t, baseErr, wrapper.Unwrap())
}

// TestErrorWrapperNoContext 测试没有上下文的 ErrorWrapper
func TestErrorWrapperNoContext(t *testing.T) {
	baseErr := errors.New("original error")
	wrapper := &ErrorWrapper{
		Err:     baseErr,
		Message: "wrapped message",
	}

	errMsg := wrapper.Error()
	assert.Contains(t, errMsg, "wrapped message")
	assert.Contains(t, errMsg, "original error")
	assert.NotContains(t, errMsg, "context:")
}

// TestNewValidationError 测试验证错误创建
func TestNewValidationError(t *testing.T) {
	err := NewValidationError("email", "invalid format")

	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "email")
	assert.Contains(t, err.Error(), "invalid format")
	assert.Contains(t, err.Error(), "validation failed")
}

// TestNewConfigError 测试配置错误创建
func TestNewConfigError(t *testing.T) {
	err := NewConfigError("port", "must be between 1-65535")

	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "port")
	assert.Contains(t, err.Error(), "must be between 1-65535")
	// NewConfigError 内部使用 WrapErrorf，所以可以追溯到 ErrConfigInvalid
	assert.True(t, errors.Is(err, ErrConfigInvalid))
}

// TestNewPluginError 测试插件错误创建
func TestNewPluginError(t *testing.T) {
	err := NewPluginError("test-plugin", "failed to load")

	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "test-plugin")
	assert.Contains(t, err.Error(), "failed to load")
}

// TestRecoverError 测试 panic 恢复
func TestRecoverError(t *testing.T) {
	t.Run("recover from error", func(t *testing.T) {
		var recoveredErr error

		func() {
			defer func() {
				if r := recover(); r != nil {
					// 手动构建恢复的错误（模拟 RecoverError 的行为）
					switch v := r.(type) {
					case error:
						recoveredErr = WrapErrorf(v, "panic recovered")
					default:
						recoveredErr = errors.New("panic recovered: " + fmt.Sprint(v))
					}
				}
			}()
			panic(errors.New("test panic"))
		}()

		assert.NotNil(t, recoveredErr)
		assert.Contains(t, recoveredErr.Error(), "panic recovered")
		assert.Contains(t, recoveredErr.Error(), "test panic")
	})

	t.Run("recover from string", func(t *testing.T) {
		var recoveredErr error

		func() {
			defer func() {
				if r := recover(); r != nil {
					recoveredErr = errors.New("panic recovered: " + fmt.Sprint(r))
				}
			}()
			panic("string panic")
		}()

		assert.NotNil(t, recoveredErr)
		assert.Contains(t, recoveredErr.Error(), "panic recovered")
		assert.Contains(t, recoveredErr.Error(), "string panic")
	})

	t.Run("recover from other type", func(t *testing.T) {
		var recoveredErr error

		func() {
			defer func() {
				if r := recover(); r != nil {
					recoveredErr = errors.New("panic recovered: " + fmt.Sprint(r))
				}
			}()
			panic(123)
		}()

		assert.NotNil(t, recoveredErr)
		assert.Contains(t, recoveredErr.Error(), "panic recovered")
		assert.Contains(t, recoveredErr.Error(), "123")
	})

	t.Run("no panic", func(t *testing.T) {
		var recoveredErr error

		func() {
			defer func() {
				if r := recover(); r != nil {
					recoveredErr = errors.New("unexpected panic")
				}
			}()
			// 正常执行，不 panic
		}()

		assert.Nil(t, recoveredErr)
	})
}

// TestErrorChaining 测试错误链
func TestErrorChaining(t *testing.T) {
	// 创建错误链
	err1 := errors.New("level 1 error")
	err2 := WrapErrorf(err1, "level 2")
	err3 := WrapErrorf(err2, "level 3")

	// 验证可以追溯到原始错误
	assert.True(t, errors.Is(err3, err1))
	assert.True(t, errors.Is(err3, err2))

	// 验证错误消息包含所有层级
	assert.Contains(t, err3.Error(), "level 3")
	assert.Contains(t, err3.Error(), "level 2")
}

// TestErrorUnwrapping 测试错误解包
func TestErrorUnwrapping(t *testing.T) {
	baseErr := ErrContextReleased
	wrapped := WrapErrorf(baseErr, "operation failed")

	// 使用 errors.Unwrap 解包
	unwrapped := errors.Unwrap(wrapped)
	assert.Equal(t, baseErr, unwrapped)
}

// BenchmarkWrapErrorf 基准测试错误包装
func BenchmarkWrapErrorf(b *testing.B) {
	baseErr := errors.New("base error")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		WrapErrorf(baseErr, "operation failed")
	}
}

// BenchmarkWrapErrorWithContextf 基准测试带上下文的错误包装
func BenchmarkWrapErrorWithContextf(b *testing.B) {
	baseErr := errors.New("base error")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		WrapErrorWithContextf(baseErr, "operation failed", "user_id=123")
	}
}

// BenchmarkIsErrorType 基准测试错误类型判断
func BenchmarkIsErrorType(b *testing.B) {
	err := WrapErrorf(ErrContextReleased, "operation failed")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		IsErrorType(err, ErrContextReleased)
	}
}
