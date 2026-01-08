package remilia

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

func TestHandlerError_WithStack(t *testing.T) {
	// 启用堆栈跟踪
	EnableStackTrace(true)
	defer EnableStackTrace(false)

	// 创建一个模拟场景
	engine := NewEngine()
	matcher := engine.On(dto.C2CMessageCreate).HandleE(func(ctx *Context) error {
		return errors.New("test error")
	})

	ctx := NewContext(nil, nil)

	// 包装错误
	err := WrapError(errors.New("test error"), ctx, matcher, 1)

	// 验证错误类型
	var he HandlerError
	assert.True(t, errors.As(err, &he))

	// 验证堆栈信息
	assert.NotEmpty(t, he.Stack)
	assert.Contains(t, he.Stack, "errors_stack_test.go")
}

func TestHandlerError_WithoutStack(t *testing.T) {
	// 禁用堆栈跟踪
	EnableStackTrace(false)

	engine := NewEngine()
	matcher := engine.On(dto.C2CMessageCreate).HandleE(func(ctx *Context) error {
		return errors.New("test error")
	})

	ctx := NewContext(nil, nil)

	// 包装错误
	err := WrapError(errors.New("test error"), ctx, matcher, 1)

	// 验证错误类型
	var he HandlerError
	assert.True(t, errors.As(err, &he))

	// 验证没有堆栈信息
	assert.Empty(t, he.Stack)
}

func TestShouldCaptureStack_EnvVar(t *testing.T) {
	// 保存原始值
	original := os.Getenv("REMILIA_STACK_TRACE")
	defer func() {
		if original != "" {
			os.Setenv("REMILIA_STACK_TRACE", original)
		} else {
			os.Unsetenv("REMILIA_STACK_TRACE")
		}
		// 重置内部状态
		stackTraceEnabledOnce = sync.Once{}
		stackTraceEnabled = false
	}()

	// 测试启用
	os.Setenv("REMILIA_STACK_TRACE", "true")
	stackTraceEnabledOnce = sync.Once{}
	stackTraceEnabled = false // 重置状态
	assert.True(t, shouldCaptureStack())

	// 测试禁用
	os.Setenv("REMILIA_STACK_TRACE", "false")
	stackTraceEnabledOnce = sync.Once{}
	stackTraceEnabled = false // 重置状态
	assert.False(t, shouldCaptureStack())
}

func TestEnableStackTrace(t *testing.T) {
	// 启用
	EnableStackTrace(true)
	assert.True(t, IsStackTraceEnabled())

	// 禁用
	EnableStackTrace(false)
	assert.False(t, IsStackTraceEnabled())
}

func TestCaptureStack(t *testing.T) {
	EnableStackTrace(true)
	defer EnableStackTrace(false)

	stack := captureStack()

	// 验证堆栈不为空
	assert.NotEmpty(t, stack)

	// 验证包含当前测试文件
	assert.Contains(t, stack, "errors_stack_test.go")

	// 验证不包含 remilia 内部代码
	assert.NotContains(t, stack, "remilia/errors.go")
}

func TestFormatHandlerError(t *testing.T) {
	EnableStackTrace(true)
	defer EnableStackTrace(false)

	engine := NewEngine()
	matcher := engine.On(dto.C2CMessageCreate).HandleE(func(ctx *Context) error {
		return errors.New("test error")
	})

	ctx := NewContext(nil, nil)

	// 设置 trace
	ctx.internalSet(internalStateKeyMiddlewareTrace, []string{"middleware1", "middleware2"})

	// 包装错误
	err := WrapError(errors.New("test error"), ctx, matcher, 2)

	// 格式化错误
	formatted := FormatHandlerError(err)

	// 验证格式化输出
	assert.Contains(t, formatted, "Message: test error")
	assert.Contains(t, formatted, "Source:")
	assert.Contains(t, formatted, "Attempt: 2")
	assert.Contains(t, formatted, "Trace:")
	assert.Contains(t, formatted, "Stack:")
}

func TestFormatHandlerError_NoStack(t *testing.T) {
	EnableStackTrace(false)

	engine := NewEngine()
	matcher := engine.On(dto.C2CMessageCreate).HandleE(func(ctx *Context) error {
		return errors.New("test error")
	})

	ctx := NewContext(nil, nil)

	// 包装错误
	err := WrapError(errors.New("test error"), ctx, matcher, 1)

	// 格式化错误
	formatted := FormatHandlerError(err)

	// 验证格式化输出（不包含堆栈）
	assert.Contains(t, formatted, "Message: test error")
	assert.NotContains(t, formatted, "Stack:")
}

func TestFormatHandlerError_RegularError(t *testing.T) {
	// 测试非 HandlerError
	err := errors.New("regular error")
	formatted := FormatHandlerError(err)

	assert.Equal(t, "regular error", formatted)
}

func TestHandlerError_JSON(t *testing.T) {
	EnableStackTrace(true)
	defer EnableStackTrace(false)

	engine := NewEngine()
	matcher := engine.On(dto.C2CMessageCreate).HandleE(func(ctx *Context) error {
		return errors.New("test error")
	})

	ctx := NewContext(nil, nil)

	// 包装错误
	err := WrapError(errors.New("test error"), ctx, matcher, 1)

	// 转换为 HandlerError
	var he HandlerError
	assert.True(t, errors.As(err, &he))

	// JSON 序列化
	b, jsonErr := json.Marshal(he)
	assert.NoError(t, jsonErr)

	// 验证 JSON 包含所有字段
	jsonStr := string(b)
	assert.Contains(t, jsonStr, `"message":"test error"`)
	assert.Contains(t, jsonStr, `"source":`)
	assert.Contains(t, jsonStr, `"attempt":1`)
	assert.Contains(t, jsonStr, `"stack":`)
}

func TestCaptureStack_Filter(t *testing.T) {
	EnableStackTrace(true)
	defer EnableStackTrace(false)

	stack := captureStack()

	// 验证过滤了 remilia 内部代码
	lines := strings.Split(stack, "\n")
	for _, line := range lines {
		assert.NotContains(t, line, "remilia/errors.go")
		assert.NotContains(t, line, "runtime/")
	}
}

func TestHandlerError_WithEventID(t *testing.T) {
	EnableStackTrace(false)

	engine := NewEngine()
	matcher := engine.On(dto.C2CMessageCreate).HandleE(func(ctx *Context) error {
		return errors.New("test error")
	})

	// 创建带有事件 ID 的 Context
	event := &dto.Payload{
		ID:   "test-event-123",
		Type: dto.C2CMessageCreate,
	}
	ctx := NewContext(event, nil)

	// 包装错误
	err := WrapError(errors.New("test error"), ctx, matcher, 1)

	var he HandlerError
	assert.True(t, errors.As(err, &he))
	assert.Equal(t, "test-event-123", he.EventID)
}

func TestHandlerError_WithTrace(t *testing.T) {
	EnableStackTrace(false)

	engine := NewEngine()
	matcher := engine.On(dto.C2CMessageCreate).HandleE(func(ctx *Context) error {
		return errors.New("test error")
	})

	ctx := NewContext(nil, nil)

	// 设置中间件追踪
	trace := []string{"Recover", "Logging", "Handler"}
	ctx.internalSet(internalStateKeyMiddlewareTrace, trace)

	// 包装错误
	err := WrapError(errors.New("test error"), ctx, matcher, 1)

	var he HandlerError
	assert.True(t, errors.As(err, &he))
	assert.Equal(t, trace, he.Trace)
}

func BenchmarkWrapError_WithStack(b *testing.B) {
	EnableStackTrace(true)
	defer EnableStackTrace(false)

	engine := NewEngine()
	matcher := engine.On(dto.C2CMessageCreate).HandleE(func(ctx *Context) error {
		return errors.New("test error")
	})

	ctx := NewContext(nil, nil)

	baseErr := errors.New("test error")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = WrapError(baseErr, ctx, matcher, 1)
	}
}

func BenchmarkWrapError_WithoutStack(b *testing.B) {
	EnableStackTrace(false)

	engine := NewEngine()
	matcher := engine.On(dto.C2CMessageCreate).HandleE(func(ctx *Context) error {
		return errors.New("test error")
	})

	ctx := NewContext(nil, nil)

	baseErr := errors.New("test error")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = WrapError(baseErr, ctx, matcher, 1)
	}
}
