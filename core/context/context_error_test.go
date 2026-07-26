package context

import (
	stdctx "context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
)

// TestContextErrorHandling 测试 Context 错误处理
func TestContextErrorHandling(t *testing.T) {
	t.Run("nil_context_operations", func(t *testing.T) {
		var ctx *Context

		// 这些操作应该安全处理 nil（返回零值而不是 panic）
		assert.NotPanics(t, func() {
			_ = ctx.Context()
			_ = ctx.GetMessageContent()
			_ = ctx.GetSenderInfo()
			_ = ctx.GetEventType()
			_ = ctx.GetPlatformEvent()
		})

		// 这些操作在 nil context 上会安全返回
		assert.Equal(t, stdctx.Background(), ctx.Context())
		assert.Equal(t, "", ctx.GetMessageContent())
		assert.Equal(t, platform.UserInfo{}, ctx.GetSenderInfo())
		assert.Equal(t, "", ctx.GetEventType())
		assert.Nil(t, ctx.GetPlatformEvent())
	})

	t.Run("nil_event_handling", func(t *testing.T) {
		ctx := NewContextFromEvent(nil, nil)

		// 应该返回合理的默认值
		assert.Equal(t, "", ctx.GetMessageContent())
		assert.Equal(t, platform.UserInfo{}, ctx.GetSenderInfo())
		assert.Equal(t, "", ctx.GetEventType())
		assert.Nil(t, ctx.GetPlatformEvent())
	})

	t.Run("invalid_json_detail", func(t *testing.T) {
		event := newMockEvent(platform.EventKindPrivateMessage)
		ctx := NewContextFromEvent(event, nil)

		// should return empty values without panic
		content := ctx.GetMessageContent()
		assert.Equal(t, "", content)

		senderInfo := ctx.GetSenderInfo()
		assert.Equal(t, platform.UserInfo{}, senderInfo)
	})

	t.Run("reserved_state_key_rejection", func(t *testing.T) {
		ctx := NewContextFromEvent(newMockEvent(platform.EventKindPrivateMessage), nil)

		// 尝试设置保留键
		ctx.Set("mw_trace", "value")
		ctx.Set("retry_attempt", 123)
		ctx.Set("_remilia_internal_key", "value")

		// 这些键应该被拒绝
		_, ok := ctx.Get("mw_trace")
		assert.False(t, ok)

		_, ok = ctx.Get("retry_attempt")
		assert.False(t, ok)

		_, ok = ctx.Get("_remilia_internal_key")
		assert.False(t, ok)
	})
}

// TestContextConcurrency 测试 Context 并发安全
func TestContextConcurrency(t *testing.T) {
	t.Run("concurrent_set_get", func(t *testing.T) {
		ctx := NewContextFromEvent(newMockEvent(platform.EventKindPrivateMessage), nil)

		const goroutines = 10
		const operations = 100

		done := make(chan bool, goroutines)

		// 并发写入
		for i := range goroutines {
			go func(id int) {
				for j := range operations {
					key := "key_" + string(rune('0'+id))
					ctx.Set(key, j)
					_, _ = ctx.Get(key)
				}
				done <- true
			}(i)
		}

		// 等待完成
		for range goroutines {
			<-done
		}

		// 验证数据一致性
		all := ctx.All()
		assert.Equal(t, goroutines, len(all))
	})

	t.Run("concurrent_extensions_access", func(t *testing.T) {
		ctx := NewContextFromEvent(newMockEvent(platform.EventKindPrivateMessage), nil)

		const goroutines = 10
		done := make(chan bool, goroutines)

		// 并发访问 Extensions
		for i := range goroutines {
			go func(id int) {
				for range 100 {
					ext := ctx.Ext()
					assert.NotNil(t, ext)
				}
				done <- true
			}(i)
		}

		// 等待完成
		for range goroutines {
			<-done
		}
	})

	t.Run("concurrent_context_operations", func(t *testing.T) {
		ctx := NewContextFromEvent(newMockEvent(platform.EventKindPrivateMessage), nil)

		const goroutines = 10
		done := make(chan bool, goroutines)

		// 并发读取操作
		for range goroutines {
			go func() {
				for range 100 {
					_ = ctx.GetMessageContent()
					_ = ctx.GetSenderInfo()
					_ = ctx.GetEventType()
				}
				done <- true
			}()
		}

		// 等待完成
		for range goroutines {
			<-done
		}
	})
}

// TestContextClone 测试 Context 克隆
func TestContextClone(t *testing.T) {
	t.Run("clone_preserves_state", func(t *testing.T) {
		original := NewContextFromEvent(newMockEvent(platform.EventKindPrivateMessage), nil)

		// 设置一些状态
		original.Set("key1", "value1")
		original.Set("key2", 42)
		original.SetRetryAttempt(3)

		// 克隆
		cloned := original.Clone()

		// 验证状态被复制
		val1, ok := cloned.Get("key1")
		assert.True(t, ok)
		assert.Equal(t, "value1", val1)

		val2, ok := cloned.Get("key2")
		assert.True(t, ok)
		assert.Equal(t, 42, val2)

		attempt, ok := cloned.GetRetryAttempt()
		assert.True(t, ok)
		assert.Equal(t, 3, attempt)
	})

	t.Run("clone_is_independent", func(t *testing.T) {
		original := NewContextFromEvent(newMockEvent(platform.EventKindPrivateMessage), nil)
		original.Set("key", "original")

		cloned := original.Clone()

		// 修改克隆不应影响原始
		cloned.Set("key", "cloned")
		cloned.Set("new_key", "new_value")

		// 验证原始未改变
		val, _ := original.Get("key")
		assert.Equal(t, "original", val)

		_, ok := original.Get("new_key")
		assert.False(t, ok)

		// 验证克隆的值
		clonedVal, _ := cloned.Get("key")
		assert.Equal(t, "cloned", clonedVal)

		newVal, ok := cloned.Get("new_key")
		assert.True(t, ok)
		assert.Equal(t, "new_value", newVal)
	})

	t.Run("clone_with_nil_state", func(t *testing.T) {
		original := NewContextFromEvent(newMockEvent(platform.EventKindPrivateMessage), nil)

		cloned := original.Clone()

		// 克隆应该成功
		assert.NotNil(t, cloned)

		// 可以在克隆上设置状态
		cloned.Set("key", "value")
		val, ok := cloned.Get("key")
		assert.True(t, ok)
		assert.Equal(t, "value", val)
	})
}

// TestContextStdContext 测试标准�?context 集成
func TestContextStdContext(t *testing.T) {
	t.Run("context_with_timeout", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx := NewContextFromEvent(newMockEvent(platform.EventKindPrivateMessage), nil)

			// 设置带超时的 context
			stdCtx, cancel := stdctx.WithTimeout(stdctx.Background(), 100*time.Millisecond)
			defer cancel()

			ctx.SetStdContext(stdCtx)

			// 验证可以读取
			retrievedCtx := ctx.Context()
			assert.NotNil(t, retrievedCtx)

			// 等待超时
			time.Sleep(150 * time.Millisecond)

			// context 应该已取消
			select {
			case <-retrievedCtx.Done():
				assert.Error(t, retrievedCtx.Err())
			default:
				t.Error("Context should be cancelled")
			}
		})
	})

	t.Run("context_with_cancel", func(t *testing.T) {
		ctx := NewContextFromEvent(newMockEvent(platform.EventKindPrivateMessage), nil)

		stdCtx, cancel := stdctx.WithCancel(stdctx.Background())
		ctx.SetStdContext(stdCtx)

		// 立即取消
		cancel()

		// context 应该已取消
		retrievedCtx := ctx.Context()
		select {
		case <-retrievedCtx.Done():
			assert.Error(t, retrievedCtx.Err())
		default:
			t.Error("Context should be cancelled")
		}
	})

	t.Run("nil_context_handling", func(t *testing.T) {
		ctx := NewContextFromEvent(newMockEvent(platform.EventKindPrivateMessage), nil)

		// 设置 nil context
		ctx.SetStdContext(stdctx.TODO())

		// 应该返回 Background
		stdCtx := ctx.Context()
		assert.NotNil(t, stdCtx)
		assert.Equal(t, stdctx.Background(), stdCtx)
	})
}

// TestContextMiddlewareTrace 测试中间件追踪
func TestContextMiddlewareTrace(t *testing.T) {
	t.Run("set_and_get_trace", func(t *testing.T) {
		ctx := NewContextFromEvent(newMockEvent(platform.EventKindPrivateMessage), nil)

		trace := []string{"mw1", "mw2", "mw3"}
		ctx.SetMiddlewareTrace(trace)

		retrievedTrace, ok := ctx.GetMiddlewareTrace()
		assert.True(t, ok)
		assert.Equal(t, trace, retrievedTrace)
	})

	t.Run("trace_is_copied", func(t *testing.T) {
		ctx := NewContextFromEvent(newMockEvent(platform.EventKindPrivateMessage), nil)

		originalTrace := []string{"mw1", "mw2"}
		ctx.SetMiddlewareTrace(originalTrace)

		// 修改原始切片
		originalTrace[0] = "modified"
		_ = append(originalTrace, "mw3")

		// 获取的追踪应该未改变
		retrievedTrace, ok := ctx.GetMiddlewareTrace()
		assert.True(t, ok)
		assert.Equal(t, []string{"mw1", "mw2"}, retrievedTrace)
	})

	t.Run("get_nonexistent_trace", func(t *testing.T) {
		ctx := NewContextFromEvent(newMockEvent(platform.EventKindPrivateMessage), nil)

		trace, ok := ctx.GetMiddlewareTrace()
		assert.False(t, ok)
		assert.Nil(t, trace)
	})
}

// TestContextRetryAttempt 测试重试次数追踪
func TestContextRetryAttempt(t *testing.T) {
	t.Run("set_and_get_attempt", func(t *testing.T) {
		ctx := NewContextFromEvent(newMockEvent(platform.EventKindPrivateMessage), nil)

		for i := 0; i <= 5; i++ {
			ctx.SetRetryAttempt(i)
			attempt, ok := ctx.GetRetryAttempt()
			assert.True(t, ok)
			assert.Equal(t, i, attempt)
		}
	})

	t.Run("get_nonexistent_attempt", func(t *testing.T) {
		ctx := NewContextFromEvent(newMockEvent(platform.EventKindPrivateMessage), nil)

		attempt, ok := ctx.GetRetryAttempt()
		assert.False(t, ok)
		assert.Equal(t, 0, attempt)
	})
}

// TestContextParsedCommand 测试命令解析
func TestContextParsedCommand(t *testing.T) {
	t.Run("set_and_get_parsed_command", func(t *testing.T) {
		ctx := NewContextFromEvent(newMockEvent(platform.EventKindPrivateMessage), nil)

		// 这里简化测试，实际的 Parsed 类型来自 command 包
		// 我们只测试 set/get 机制
		ctx.SetParsedCommand(nil)

		parsed := ctx.GetParsedCommand()
		assert.Nil(t, parsed)
	})

	t.Run("get_nonexistent_command", func(t *testing.T) {
		ctx := NewContextFromEvent(newMockEvent(platform.EventKindPrivateMessage), nil)

		parsed := ctx.GetParsedCommand()
		assert.Nil(t, parsed)
	})
}

// TestContextStateManagement 测试状态管理
func TestContextStateManagement(t *testing.T) {
	t.Run("all_returns_copy", func(t *testing.T) {
		ctx := NewContextFromEvent(newMockEvent(platform.EventKindPrivateMessage), nil)

		ctx.Set("key1", "value1")
		ctx.Set("key2", 42)

		all1 := ctx.All()
		assert.Equal(t, 2, len(all1))

		// 修改返回的 map 不应影响 context
		all1["key3"] = "value3"
		delete(all1, "key1")

		all2 := ctx.All()
		assert.Equal(t, 2, len(all2))
		assert.Contains(t, all2, "key1")
		assert.NotContains(t, all2, "key3")
	})

	t.Run("delete_key", func(t *testing.T) {
		ctx := NewContextFromEvent(newMockEvent(platform.EventKindPrivateMessage), nil)

		ctx.Set("key", "value")
		_, ok := ctx.Get("key")
		assert.True(t, ok)

		ctx.Delete("key")
		_, ok = ctx.Get("key")
		assert.False(t, ok)
	})

	t.Run("set_nil_value_deletes", func(t *testing.T) {
		ctx := NewContextFromEvent(newMockEvent(platform.EventKindPrivateMessage), nil)

		ctx.Set("key", "value")
		ctx.Set("key", nil) // nil 是 no-op，key 仍然存在

		// key 应仍然存在（nil 不删除）
		v, ok := ctx.Get("key")
		assert.True(t, ok)
		assert.Equal(t, "value", v)

		// 显式删除
		ctx.Delete("key")
		_, ok = ctx.Get("key")
		assert.False(t, ok)
	})

	t.Run("empty_state", func(t *testing.T) {
		ctx := NewContextFromEvent(newMockEvent(platform.EventKindPrivateMessage), nil)

		all := ctx.All()
		assert.NotNil(t, all)
		assert.Equal(t, 0, len(all))
	})
}

// TestContextMatcher 测试 Matcher 引用
func TestContextMatcher(t *testing.T) {
	t.Run("get_matcher_source", func(t *testing.T) {
		ctx := NewContextFromEvent(newMockEvent(platform.EventKindPrivateMessage), nil)

		// 默认应该返回空
		source := ctx.GetMatcherSource()
		assert.Equal(t, "", source)
	})
}

// TestContextMessageParsing 测试消息解析
// TestContextMessageParsing tests message parsing via new path
func TestContextMessageParsing(t *testing.T) {
	t.Run("parse_valid_message", func(t *testing.T) {
		event := newMockEventWithContent(platform.EventKindPrivateMessage, "Hello, World!")
		event.sender = platform.UserInfo{ID: "openid-456"}
		ctx := NewContextFromEvent(event, nil)
		content := ctx.GetMessageContent()
		assert.Equal(t, "Hello, World!", content)
		senderInfo := ctx.GetSenderInfo()
		assert.Equal(t, "openid-456", senderInfo.ID)
	})
	t.Run("parse_empty_content", func(t *testing.T) {
		ctx := NewContextFromEvent(newMockEvent(platform.EventKindPrivateMessage), nil)
		content := ctx.GetMessageContent()
		assert.Equal(t, "", content)
	})
	t.Run("parse_missing_author", func(t *testing.T) {
		ctx := NewContextFromEvent(newMockEvent(platform.EventKindPrivateMessage), nil)
		senderInfo := ctx.GetSenderInfo()
		assert.Equal(t, platform.UserInfo{}, senderInfo)
	})
}

// BenchmarkContextOperations Context operations benchmark
func BenchmarkContextOperations(b *testing.B) {
	event := newMockEvent(platform.EventKindPrivateMessage)
	b.Run("NewContextFromEvent", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = NewContextFromEvent(event, nil)
		}
	})
	b.Run("GetMessageContent", func(b *testing.B) {
		ctx := NewContextFromEvent(event, nil)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = ctx.GetMessageContent()
		}
	})
	b.Run("SetGet", func(b *testing.B) {
		ctx := NewContextFromEvent(event, nil)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			ctx.Set("key", i)
			_, _ = ctx.Get("key")
		}
	})
	b.Run("Clone", func(b *testing.B) {
		ctx := NewContextFromEvent(event, nil)
		ctx.Set("key1", "value1")
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = ctx.Clone()
		}
	})
}
