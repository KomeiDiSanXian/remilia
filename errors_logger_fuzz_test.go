//go:build go1.18
// +build go1.18

package remilia

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// FuzzWrapErrorf 模糊测试错误包装
//
// 测试目标：
//   - WrapErrorf 不应该 panic
//   - 各种错误消息都应该正常处理
func FuzzWrapErrorf(f *testing.F) {
	// 种子语料
	f.Add("operation failed")
	f.Add("")
	f.Add("中文错误")
	f.Add("error\nwith\nnewlines")
	f.Add("error\twith\ttabs")
	f.Add("very long error message that might cause issues if not handled properly")

	f.Fuzz(func(t *testing.T, message string) {
		// 不应该 panic
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("WrapErrorf panicked with message %q: %v", message, r)
			}
		}()

		// 使用预定义错误
		baseErr := ErrHandlerNotSet
		wrappedErr := WrapErrorf(baseErr, message)

		// 包装后的错误不应该为 nil
		if wrappedErr == nil {
			t.Errorf("WrapErrorf returned nil for message %q", message)
			return
		}

		// 错误消息应该包含原始消息
		errStr := wrappedErr.Error()
		if errStr == "" {
			t.Errorf("WrapErrorf returned empty error string")
		}
	})
}

// FuzzWrapErrorWithContextf 模糊测试带上下文的错误包装
//
// 测试目标：
//   - WrapErrorWithContextf 不应该 panic
//   - 各种上下文字符串都应该正常处理
func FuzzWrapErrorWithContextf(f *testing.F) {
	// 种子语料
	f.Add("operation failed", "user_id=123")
	f.Add("", "")
	f.Add("error", "context=test")
	f.Add("中文错误", "中文上下文")
	f.Add("error", "key1=value1,key2=value2")

	f.Fuzz(func(t *testing.T, message, context string) {
		// 不应该 panic
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("WrapErrorWithContextf panicked with message=%q, context=%q: %v", message, context, r)
			}
		}()

		baseErr := ErrHandlerNotSet
		wrappedErr := WrapErrorWithContextf(baseErr, message, context)

		if wrappedErr == nil {
			t.Errorf("WrapErrorWithContextf returned nil")
			return
		}

		errStr := wrappedErr.Error()
		if errStr == "" {
			t.Errorf("WrapErrorWithContextf returned empty error string")
		}
	})
}

// FuzzNewValidationError 模糊测试验证错误创建
//
// 测试目标：
//   - NewValidationError 不应该 panic
//   - 各种字段名和原因都应该正常处理
func FuzzNewValidationError(f *testing.F) {
	// 种子语料
	f.Add("email", "invalid format")
	f.Add("", "")
	f.Add("password", "too short")
	f.Add("中文字段", "中文原因")
	f.Add("field\n", "reason\n")

	f.Fuzz(func(t *testing.T, field, reason string) {
		// 不应该 panic
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("NewValidationError panicked with field=%q, reason=%q: %v", field, reason, r)
			}
		}()

		err := NewValidationError(field, reason)

		if err == nil {
			t.Errorf("NewValidationError returned nil")
			return
		}

		errStr := err.Error()
		if errStr == "" {
			t.Errorf("NewValidationError returned empty error string")
		}
	})
}

// FuzzNewConfigError 模糊测试配置错误创建
//
// 测试目标：
//   - NewConfigError 不应该 panic
//   - 各种配置键和原因都应该正常处理
func FuzzNewConfigError(f *testing.F) {
	// 种子语料
	f.Add("port", "must be between 1-65535")
	f.Add("", "")
	f.Add("timeout", "invalid value")
	f.Add("中文配置", "中文原因")

	f.Fuzz(func(t *testing.T, key, reason string) {
		// 不应该 panic
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("NewConfigError panicked with key=%q, reason=%q: %v", key, reason, r)
			}
		}()

		err := NewConfigError(key, reason)

		if err == nil {
			t.Errorf("NewConfigError returned nil")
			return
		}

		// 应该包含 ErrConfigInvalid
		if !IsErrorType(err, ErrConfigInvalid) {
			t.Errorf("NewConfigError should wrap ErrConfigInvalid")
		}
	})
}

// FuzzNewLogger 模糊测试日志记录器创建
//
// 测试目标：
//   - NewLogger 不应该 panic
//   - 各种组件名都应该正常处理
func FuzzNewLogger(f *testing.F) {
	// 种子语料
	f.Add("engine")
	f.Add("context")
	f.Add("")
	f.Add("中文组件")
	f.Add("component\n")
	f.Add("very-long-component-name-that-might-cause-issues")

	f.Fuzz(func(t *testing.T, component string) {
		// 不应该 panic
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("NewLogger panicked with component %q: %v", component, r)
			}
		}()

		logger := NewLogger(component)

		if logger == nil {
			t.Errorf("NewLogger returned nil for component %q", component)
			return
		}

		// 测试日志方法不应该 panic
		logger.Debug("test debug message")
		logger.Info("test info message")
		logger.Warn("test warn message")
	})
}

// FuzzStructuredLoggerWithField 模糊测试日志字段添加
//
// 测试目标：
//   - WithField 不应该 panic
//   - 各种字段名和值都应该正常处理
func FuzzStructuredLoggerWithField(f *testing.F) {
	// 种子语料
	f.Add("key", "value")
	f.Add("", "")
	f.Add("中文键", "中文值")
	f.Add("key\n", "value\n")

	f.Fuzz(func(t *testing.T, key, value string) {
		// 不应该 panic
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("WithField panicked with key=%q, value=%q: %v", key, value, r)
			}
		}()

		logger := NewLogger("test")
		fieldLogger := logger.WithField(key, value)

		if fieldLogger == nil {
			t.Errorf("WithField returned nil")
			return
		}

		// 测试日志输出不应该 panic
		fieldLogger.Info("test message")
	})
}

// FuzzErrorMessage 模糊测试错误消息
//
// 测试目标：
//   - 各种错误消息都应该正常处理
func FuzzErrorMessage(f *testing.F) {
	// 种子语料
	f.Add("event-1")
	f.Add("")
	f.Add("very-long-event-id-with-many-characters-to-test-limits")
	f.Add("中文事件ID")
	f.Add("event\nwith\nnewlines")
	f.Add("event-123456789")

	f.Fuzz(func(t *testing.T, message string) {
		// 不应该 panic
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Error handling panicked with message %q: %v", message, r)
			}
		}()

		// 测试各种错误创建函数
		err1 := NewValidationError("field", message)
		if err1 == nil {
			t.Errorf("NewValidationError returned nil")
		}

		err2 := NewConfigError("key", message)
		if err2 == nil {
			t.Errorf("NewConfigError returned nil")
		}

		err3 := NewPluginError("plugin", message)
		if err3 == nil {
			t.Errorf("NewPluginError returned nil")
		}
	})
}

// FuzzMatcherSetSource 模糊测试 Matcher 来源设置
//
// 测试目标：
//   - 各种来源字符串都应该正常处理
func FuzzMatcherSetSource(f *testing.F) {
	// 种子语料
	f.Add("plugin:test")
	f.Add("global")
	f.Add("")
	f.Add("中文来源")
	f.Add("source\nwith\nnewlines")

	f.Fuzz(func(t *testing.T, source string) {
		// 不应该 panic
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("SetSource panicked with source %q: %v", source, r)
			}
		}()

		engine := NewEngine()
		matcher := engine.OnC2C()
		matcher.Source = source

		// 验证来源被设置
		if matcher.Source != source {
			t.Errorf("Source not set correctly: expected %q, got %q", source, matcher.Source)
		}
	})
}

// FuzzEngineProcessEventBatch 模糊测试批量事件处理
//
// 测试目标：
//   - ProcessEventBatch 不应该 panic
//   - 各种批量大小都应该正常处理
func FuzzEngineProcessEventBatch(f *testing.F) {
	// 种子语料 - 批量大小
	f.Add(uint8(0))
	f.Add(uint8(1))
	f.Add(uint8(10))
	f.Add(uint8(100))
	f.Add(uint8(255))

	f.Fuzz(func(t *testing.T, batchSize uint8) {
		// 不应该 panic
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("ProcessEventBatch panicked with batchSize %d: %v", batchSize, r)
			}
		}()

		engine := NewEngine()

		// 注册一个简单的 Handler
		engine.OnAny().HandleE(func(ctx *Context) error {
			return nil
		})

		// 创建事件批次
		events := make([]*dto.Payload, batchSize)
		for i := uint8(0); i < batchSize; i++ {
			events[i] = &dto.Payload{
				Type: dto.C2CMessageCreate,
				ID:   dto.EventID("event-" + string(rune(i))),
			}
		}

		// 处理批量事件
		engine.ProcessEventBatch(events, nil)
	})
}

// FuzzContextClone 模糊测试 Context 克隆
//
// 测试目标：
//   - Clone 不应该 panic
//   - 克隆的 Context 应该独立
func FuzzContextClone(f *testing.F) {
	// 种子语料
	f.Add("state-key", "state-value")
	f.Add("", "")
	f.Add("中文键", "中文值")

	f.Fuzz(func(t *testing.T, key, value string) {
		// 不应该 panic
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Clone panicked with key=%q, value=%q: %v", key, value, r)
			}
		}()

		event := &dto.Payload{
			Type: dto.C2CMessageCreate,
			ID:   "fuzz-test",
		}
		ctx := NewContext(event, nil)

		// 设置状态
		ctx.Set(key, value)

		// 克隆
		cloned := ctx.Clone()
		if cloned == nil {
			t.Errorf("Clone returned nil")
			return
		}

		// 修改克隆的状态不应影响原始 Context
		cloned.Set(key, "modified")

		// 验证原始 Context 未被修改
		originalValue, ok := ctx.Get(key)
		if ok && originalValue != value {
			t.Errorf("Original context was modified by clone")
		}
	})
}
