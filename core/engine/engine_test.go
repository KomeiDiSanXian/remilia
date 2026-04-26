package engine

import (
	stdctx "context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// engine Core Tests
// ============================================================================

func TestEngine_NewEngine(t *testing.T) {
	t.Run("default configuration", func(t *testing.T) {
		eng := newEngineForTest(t)

		require.NotNil(t, eng)
		assert.NotNil(t, eng.state.Load())
		assert.NotNil(t, eng.middleware.Load())
		assert.NotNil(t, eng.services.tempManager)
		assert.NotNil(t, eng.services.matcherPool)
		assert.NotNil(t, eng.services.pendingDeleteCh)
	})

	t.Run("with custom cleanup interval", func(t *testing.T) {
		eng := newEngineForTest(t, WithCleanupInterval(1*time.Minute))

		require.NotNil(t, eng)
		assert.Equal(t, 1*time.Minute, eng.services.tempMatcherCleanerInterval)
	})

	t.Run("with zero cleanup interval", func(t *testing.T) {
		eng := newEngineForTest(t, WithCleanupInterval(0))

		require.NotNil(t, eng)
		assert.Equal(t, time.Duration(0), eng.services.tempMatcherCleanerInterval)
	})

	t.Run("with custom pending delete buffer", func(t *testing.T) {
		eng := newEngineForTest(t, WithPendingDeleteBufferSize(500))

		require.NotNil(t, eng)
		assert.Equal(t, 500, cap(eng.services.pendingDeleteCh))
	})
}

func TestEngine_Shutdown(t *testing.T) {
	t.Run("shutdown success", func(t *testing.T) {
		eng := newEngineForTest(t)

		context := stdctx.Background()
		err := eng.Shutdown(context)

		assert.NoError(t, err)
	})

	t.Run("shutdown with timeout", func(t *testing.T) {
		eng := newEngineForTest(t)

		context, cancel := stdctx.WithTimeout(stdctx.Background(), 100*time.Millisecond)
		defer cancel()

		err := eng.Shutdown(context)
		assert.NoError(t, err)
	})

	t.Run("shutdown multiple times", func(t *testing.T) {
		eng := newEngineForTest(t)

		context := stdctx.Background()
		err1 := eng.Shutdown(context)
		err2 := eng.Shutdown(context)

		assert.NoError(t, err1)
		assert.NoError(t, err2) // Should be idempotent
	})
}

func TestEngine_Close(t *testing.T) {
	t.Run("close engine", func(t *testing.T) {
		eng := newEngineForTest(t)

		// Should not panic
		eng.Shutdown(stdctx.Background())
	})
}

// ============================================================================
// Matcher Registration Tests
// ============================================================================

func TestEngine_OnAny(t *testing.T) {
	t.Run("register handler", func(t *testing.T) {
		eng := newEngineForTest(t)

		matcher := eng.OnAny()

		require.NotNil(t, matcher)
		assert.Equal(t, "", matcher.EventType)
	})

	t.Run("with rules", func(t *testing.T) {
		eng := newEngineForTest(t)

		rule := func(c *ctx.Context) bool { return true }
		matcher := eng.OnAny(rule)

		require.NotNil(t, matcher)
		assert.Equal(t, 1, len(matcher.Rules))
	})
}

func TestEngine_OnC2C(t *testing.T) {
	t.Run("register C2C handler", func(t *testing.T) {
		eng := newEngineForTest(t)

		matcher := eng.On(string(platform.EventKindPrivateMessage))

		require.NotNil(t, matcher)
		assert.Equal(t, string(platform.EventKindPrivateMessage), matcher.EventType)
	})

	t.Run("with rules", func(t *testing.T) {
		eng := newEngineForTest(t)

		rule := func(c *ctx.Context) bool { return true }
		matcher := eng.On(string(platform.EventKindPrivateMessage), rule)

		require.NotNil(t, matcher)
		assert.Equal(t, string(platform.EventKindPrivateMessage), matcher.EventType)
		assert.Greater(t, len(matcher.Rules), 0)
	})
}

func TestEngine_OnGroupAt(t *testing.T) {
	t.Run("register GroupAt handler", func(t *testing.T) {
		eng := newEngineForTest(t)

		matcher := eng.On(string(platform.EventKindGroupMessage))

		require.NotNil(t, matcher)
		assert.Equal(t, string(platform.EventKindGroupMessage), matcher.EventType)
	})
}

func TestEngine_On(t *testing.T) {
	t.Run("register custom event handler", func(t *testing.T) {
		eng := newEngineForTest(t)

		matcher := eng.On(string(platform.EventKindNotice))

		require.NotNil(t, matcher)
		assert.Equal(t, string(platform.EventKindNotice), matcher.EventType)
	})

	t.Run("with multiple rules", func(t *testing.T) {
		eng := newEngineForTest(t)

		rule1 := func(c *ctx.Context) bool { return true }
		rule2 := func(c *ctx.Context) bool { return false }
		matcher := eng.On(string(platform.EventKindPrivateMessage), rule1, rule2)

		require.NotNil(t, matcher)
		assert.Equal(t, 2, len(matcher.Rules))
	})
}

func TestEngine_OnGroupAdd(t *testing.T) {
	eng := newEngineForTest(t)
	matcher := eng.On(string(platform.EventKindNotice))
	require.NotNil(t, matcher)
	assert.Equal(t, string(platform.EventKindNotice), matcher.EventType)
}

func TestEngine_OnGroupDel(t *testing.T) {
	eng := newEngineForTest(t)
	matcher := eng.On(string(platform.EventKindSystem))
	require.NotNil(t, matcher)
	assert.Equal(t, string(platform.EventKindSystem), matcher.EventType)
}

func TestEngine_OnFullMatch(t *testing.T) {
	eng := newEngineForTest(t)
	matcher := eng.OnFullMatch("test")
	require.NotNil(t, matcher)
	assert.Greater(t, len(matcher.Rules), 0)
}

// ============================================================================
// Matcher Methods Tests
// ============================================================================

func TestMatcher_Handle(t *testing.T) {
	t.Run("set handler", func(t *testing.T) {
		eng := newEngineForTest(t)

		executed := false
		handler := func(c *ctx.Context) error {
			executed = true
			return nil
		}

		// Handle is a terminal method (void return) — keep *Matcher from eng.OnAny()
		matcher := eng.OnAny()
		matcher.Handle(handler)

		require.NotNil(t, matcher)
		assert.NotNil(t, matcher.Handler)

		// Test handler execution
		context := ctx.AcquireContextFromEvent(newTestPlatformEvent(platform.EventKindPrivateMessage), nil)
		err := matcher.Handler(context)
		assert.NoError(t, err)
		assert.True(t, executed)
	})

	t.Run("handle with error", func(t *testing.T) {
		eng := newEngineForTest(t)

		expectedErr := errors.New("handler error")
		handler := func(c *ctx.Context) error {
			return expectedErr
		}

		matcher := eng.OnAny()
		matcher.Handle(handler)

		context := ctx.AcquireContextFromEvent(newTestPlatformEvent(platform.EventKindPrivateMessage), nil)
		err := matcher.Handler(context)
		assert.Equal(t, expectedErr, err)
	})
}

func TestMatcher_SetBlock(t *testing.T) {
	t.Run("set blocking", func(t *testing.T) {
		eng := newEngineForTest(t)

		matcher := eng.OnAny().SetBlock(true)

		require.NotNil(t, matcher)

		matcher.rt.mu.RLock()
		isBlock := matcher.isBlock.Load()
		matcher.rt.mu.RUnlock()

		assert.True(t, isBlock)
	})
}

func TestMatcher_SetPriority(t *testing.T) {
	t.Run("set priority", func(t *testing.T) {
		eng := newEngineForTest(t)

		matcher := eng.OnAny().SetPriority(100)

		require.NotNil(t, matcher)

		matcher.rt.mu.RLock()
		priority := matcher.priority.Load()
		matcher.rt.mu.RUnlock()

		assert.Equal(t, uint64(100), priority)
	})
}

func TestMatcher_SetSource(t *testing.T) {
	t.Run("set source", func(t *testing.T) {
		eng := newEngineForTest(t)

		matcher := eng.OnAny().SetSource("test-source")

		require.NotNil(t, matcher)
		assert.Equal(t, "test-source", matcher.Source)
	})

	t.Run("GetSource method", func(t *testing.T) {
		eng := newEngineForTest(t)

		matcher := eng.OnAny().SetSource("my-source")
		assert.Equal(t, "my-source", matcher.GetSource())
	})

	t.Run("GetSource nil matcher", func(t *testing.T) {
		var matcher *Matcher
		assert.Equal(t, "", matcher.GetSource())
	})
}

func TestMatcher_Delete(t *testing.T) {
	t.Run("delete matcher", func(t *testing.T) {
		eng := newEngineForTest(t)

		matcher := eng.OnAny()
		matcher.Delete()

		// Verify matcher is marked as deleted
		assert.True(t, matcher.IsDeleted())
	})
}

func TestMatcher_IsDeleted(t *testing.T) {
	eng := newEngineForTest(t)
	matcher := eng.OnAny()

	assert.False(t, matcher.IsDeleted())

	matcher.Delete()
	assert.True(t, matcher.IsDeleted())
}

func TestMatcher_GetCommand(t *testing.T) {
	eng := newEngineForTest(t)
	matcher := eng.OnAny()
	matcher.BindCommand("/test")

	assert.Equal(t, "/test", matcher.GetCommand())
}

func TestMatcher_IsTemp(t *testing.T) {
	eng := newEngineForTest(t)
	matcher := eng.OnAny()

	assert.False(t, matcher.IsTemp())

	matcher.SetTemp(true)
	assert.True(t, matcher.IsTemp())
}

// ============================================================================
// Process Event Tests
// ============================================================================

func TestEngine_ProcessEvent(t *testing.T) {
	t.Run("process event with matching handler", func(t *testing.T) {
		eng := newEngineForTest(t)

		executed := false
		eng.On(string(platform.EventKindPrivateMessage)).Handle(func(c *ctx.Context) error {
			executed = true
			return nil
		})

		context := ctx.AcquireContextFromEvent(newTestPlatformEvent(platform.EventKindPrivateMessage), nil)
		eng.ProcessEvent(context)

		assert.True(t, executed)
	})

	t.Run("process event with no matching handler", func(t *testing.T) {
		eng := newEngineForTest(t)

		executed := false
		eng.On(string(platform.EventKindPrivateMessage)).Handle(func(c *ctx.Context) error {
			executed = true
			return nil
		})

		// Different event type
		context := ctx.AcquireContextFromEvent(newTestPlatformEvent(platform.EventKindGroupMessage), nil)
		eng.ProcessEvent(context)

		assert.False(t, executed)
	})

	t.Run("process event with blocking matcher", func(t *testing.T) {
		eng := newEngineForTest(t)

		executed1 := false
		executed2 := false

		eng.On(string(platform.EventKindPrivateMessage)).SetBlock(true).Handle(func(c *ctx.Context) error {
			executed1 = true
			return nil
		})

		eng.On(string(platform.EventKindPrivateMessage)).Handle(func(c *ctx.Context) error {
			executed2 = true
			return nil
		})

		context := ctx.AcquireContextFromEvent(newTestPlatformEvent(platform.EventKindPrivateMessage), nil)
		eng.ProcessEvent(context)

		assert.True(t, executed1)
		assert.False(t, executed2) // Should not execute after blocking
	})

	t.Run("process event with priority", func(t *testing.T) {
		eng := newEngineForTest(t)

		var executionOrder []int
		var mu sync.Mutex

		eng.On(string(platform.EventKindPrivateMessage)).SetPriority(10).Handle(func(c *ctx.Context) error {
			mu.Lock()
			executionOrder = append(executionOrder, 10)
			mu.Unlock()
			return nil
		})

		eng.On(string(platform.EventKindPrivateMessage)).SetPriority(100).Handle(func(c *ctx.Context) error {
			mu.Lock()
			executionOrder = append(executionOrder, 100)
			mu.Unlock()
			return nil
		})

		eng.On(string(platform.EventKindPrivateMessage)).SetPriority(50).Handle(func(c *ctx.Context) error {
			mu.Lock()
			executionOrder = append(executionOrder, 50)
			mu.Unlock()
			return nil
		})

		context := ctx.AcquireContextFromEvent(newTestPlatformEvent(platform.EventKindPrivateMessage), nil)
		eng.ProcessEvent(context)

		// Priority design: LOWER number = HIGHER priority (like Linux nice value)
		assert.Equal(t, []int{10, 50, 100}, executionOrder)
	})

	t.Run("process event with rule filtering", func(t *testing.T) {
		eng := newEngineForTest(t)

		executed1 := false
		executed2 := false

		// Matcher with failing rule
		eng.On(string(platform.EventKindPrivateMessage), func(c *ctx.Context) bool {
			return false
		}).Handle(func(c *ctx.Context) error {
			executed1 = true
			return nil
		})

		// Matcher with passing rule
		eng.On(string(platform.EventKindPrivateMessage), func(c *ctx.Context) bool {
			return true
		}).Handle(func(c *ctx.Context) error {
			executed2 = true
			return nil
		})

		context := ctx.AcquireContextFromEvent(newTestPlatformEvent(platform.EventKindPrivateMessage), nil)
		eng.ProcessEvent(context)

		assert.False(t, executed1)
		assert.True(t, executed2)
	})
}

func TestEngine_ProcessEventBatch(t *testing.T) {
	t.Run("process multiple events", func(t *testing.T) {
		eng := newEngineForTest(t)

		var count int32
		eng.OnEventKind(platform.EventKindPrivateMessage).Handle(func(c *ctx.Context) error {
			atomic.AddInt32(&count, 1)
			return nil
		})

		events := []platform.Event{
			newTestPlatformEvent(platform.EventKindPrivateMessage),
			newTestPlatformEvent(platform.EventKindPrivateMessage),
			newTestPlatformEvent(platform.EventKindPrivateMessage),
		}

		eng.ProcessPlatformEventBatch(events, nil)

		assert.Equal(t, int32(3), atomic.LoadInt32(&count))
	})

	t.Run("process empty batch", func(t *testing.T) {
		eng := newEngineForTest(t)

		// Should not panic
		eng.ProcessPlatformEventBatch([]platform.Event{}, nil)
	})
}

// ============================================================================
// Middleware Tests
// ============================================================================

func TestEngine_Use(t *testing.T) {
	t.Run("register global middleware", func(t *testing.T) {
		eng := newEngineForTest(t)

		executed := false
		mw := func(next ctx.Handler) ctx.Handler {
			return func(c *ctx.Context) error {
				executed = true
				return next(c)
			}
		}

		eng.Use(mw)

		eng.On(string(platform.EventKindPrivateMessage)).Handle(func(c *ctx.Context) error {
			return nil
		})

		context := ctx.AcquireContextFromEvent(newTestPlatformEvent(platform.EventKindPrivateMessage), nil)
		eng.ProcessEvent(context)

		assert.True(t, executed)
	})

	t.Run("middleware chain order", func(t *testing.T) {
		eng := newEngineForTest(t)

		var order []int
		var mu sync.Mutex

		mw1 := func(next ctx.Handler) ctx.Handler {
			return func(c *ctx.Context) error {
				mu.Lock()
				order = append(order, 1)
				mu.Unlock()
				err := next(c)
				mu.Lock()
				order = append(order, 4)
				mu.Unlock()
				return err
			}
		}

		mw2 := func(next ctx.Handler) ctx.Handler {
			return func(c *ctx.Context) error {
				mu.Lock()
				order = append(order, 2)
				mu.Unlock()
				err := next(c)
				mu.Lock()
				order = append(order, 3)
				mu.Unlock()
				return err
			}
		}

		eng.Use(mw1, mw2)

		eng.On(string(platform.EventKindPrivateMessage)).Handle(func(c *ctx.Context) error {
			return nil
		})

		context := ctx.AcquireContextFromEvent(newTestPlatformEvent(platform.EventKindPrivateMessage), nil)
		eng.ProcessEvent(context)

		assert.Equal(t, []int{1, 2, 3, 4}, order)
	})
}

func TestEngine_UseForGroup(t *testing.T) {
	t.Run("register group middleware", func(t *testing.T) {
		eng := newEngineForTest(t)

		groupExecuted := false
		globalExecuted := false
		var mu sync.Mutex

		groupMw := func(next ctx.Handler) ctx.Handler {
			return func(c *ctx.Context) error {
				mu.Lock()
				groupExecuted = true
				mu.Unlock()
				return next(c)
			}
		}

		globalMw := func(next ctx.Handler) ctx.Handler {
			return func(c *ctx.Context) error {
				mu.Lock()
				globalExecuted = true
				mu.Unlock()
				return next(c)
			}
		}

		eng.Use(globalMw)
		eng.UseForGroup("test-group", groupMw)

		matcher := eng.On(string(platform.EventKindPrivateMessage))
		matcher.group = "test-group"
		matcher.Handle(func(c *ctx.Context) error {
			return nil
		})

		// Rebuild chain to apply group middleware
		eng.RebuildMatcherChain(matcher)

		context := ctx.AcquireContextFromEvent(newTestPlatformEvent(platform.EventKindPrivateMessage), nil)
		eng.ProcessEvent(context)

		mu.Lock()
		ge := globalExecuted
		gre := groupExecuted
		mu.Unlock()

		assert.True(t, ge)
		assert.True(t, gre)
	})

	t.Run("empty group name ignored", func(t *testing.T) {
		eng := newEngineForTest(t)

		mw := func(next ctx.Handler) ctx.Handler {
			return next
		}

		result := eng.UseForGroup("", mw)
		assert.Equal(t, eng, result)
	})
}

// ============================================================================
// Blocking Tests
// ============================================================================

func TestEngine_SetBlock(t *testing.T) {
	t.Run("set global blocking", func(t *testing.T) {
		eng := newEngineForTest(t)

		eng.SetBlock(true)

		state := eng.state.Load() // 无需类型断言
		assert.True(t, state.block)
	})

	t.Run("blocking stops after first match", func(t *testing.T) {
		eng := newEngineForTest(t)
		eng.SetBlock(true)

		executed1 := false
		executed2 := false
		var mu sync.Mutex

		eng.On(string(platform.EventKindPrivateMessage)).Handle(func(c *ctx.Context) error {
			mu.Lock()
			executed1 = true
			mu.Unlock()
			return nil
		})

		eng.On(string(platform.EventKindPrivateMessage)).Handle(func(c *ctx.Context) error {
			mu.Lock()
			executed2 = true
			mu.Unlock()
			return nil
		})

		context := ctx.AcquireContextFromEvent(newTestPlatformEvent(platform.EventKindPrivateMessage), nil)
		eng.ProcessEvent(context)

		mu.Lock()
		e1 := executed1
		e2 := executed2
		mu.Unlock()

		assert.True(t, e1)
		assert.False(t, e2)
	})
}

// ============================================================================
// Delete Tests
// ============================================================================

func TestEngine_DeleteMatcher(t *testing.T) {
	t.Run("delete specific matcher", func(t *testing.T) {
		eng := newEngineForTest(t)

		matcher := eng.On(string(platform.EventKindPrivateMessage))
		initialCount := eng.GetMatcherCount()

		eng.DeleteMatcher(matcher)

		assert.Equal(t, initialCount-1, eng.GetMatcherCount())
	})
}

func TestEngine_DeleteAllMatchers(t *testing.T) {
	t.Run("delete all matchers", func(t *testing.T) {
		eng := newEngineForTest(t)

		eng.On(string(platform.EventKindPrivateMessage))
		eng.On(string(platform.EventKindGroupMessage))
		eng.OnAny()

		eng.DeleteAllMatchers()

		state := eng.state.Load()
		assert.Equal(t, 0, len(state.matcherIndex))
	})
}

func TestEngine_DeleteMatchers(t *testing.T) {
	eng := newEngineForTest(t)

	m1 := eng.On(string(platform.EventKindPrivateMessage))
	m2 := eng.On(string(platform.EventKindGroupMessage))

	initialCount := eng.GetMatcherCount()

	eng.DeleteMatchers([]*Matcher{m1, m2})

	assert.Equal(t, initialCount-2, eng.GetMatcherCount())
}

func TestEngine_RemoveGroup(t *testing.T) {
	eng := newEngineForTest(t)

	matcher := eng.On(string(platform.EventKindPrivateMessage))
	eng.SetMatcherGroup(matcher, "test-group", "")

	eng.RemoveGroup("test-group")

	assert.Equal(t, 0, eng.GetMatcherCount())
}

// ============================================================================
// Temporary Matcher Tests
// ============================================================================

func TestMatcher_SetTemp(t *testing.T) {
	t.Run("set temp status", func(t *testing.T) {
		eng := newEngineForTest(t)

		matcher := eng.On(string(platform.EventKindPrivateMessage))

		assert.False(t, matcher.IsTemp())

		matcher.SetTemp(true)
		assert.True(t, matcher.IsTemp())

		matcher.SetTemp(false)
		assert.False(t, matcher.IsTemp())
	})
}

func TestEngine_OnTemp(t *testing.T) {
	eng := newEngineForTest(t)

	matcher := eng.OnTemp(string(platform.EventKindPrivateMessage))

	require.NotNil(t, matcher)
	assert.True(t, matcher.IsTemp())
}

// ============================================================================
// Concurrent Tests
// ============================================================================

func TestEngine_ConcurrentProcessing(t *testing.T) {
	t.Run("concurrent event processing", func(t *testing.T) {
		eng := newEngineForTest(t)

		var count int32
		eng.On(string(platform.EventKindPrivateMessage)).Handle(func(c *ctx.Context) error {
			atomic.AddInt32(&count, 1)
			return nil
		})

		var wg sync.WaitGroup

		// Process 100 events concurrently
		for range 100 {
			wg.Go(func() {
				context := ctx.AcquireContextFromEvent(newTestPlatformEvent(platform.EventKindPrivateMessage), nil)
				eng.ProcessEvent(context)
			})
		}

		wg.Wait()

		assert.Equal(t, int32(100), atomic.LoadInt32(&count))
	})

	t.Run("concurrent matcher registration", func(t *testing.T) {
		eng := newEngineForTest(t)

		var wg sync.WaitGroup

		for range 50 {
			wg.Go(func() {
				eng.On(string(platform.EventKindPrivateMessage)).Handle(func(c *ctx.Context) error {
					return nil
				})
			})
		}

		wg.Wait()
	})

	t.Run("concurrent read and write", func(t *testing.T) {
		eng := newEngineForTest(t)

		eng.On(string(platform.EventKindPrivateMessage)).Handle(func(c *ctx.Context) error {
			return nil
		})

		var wg sync.WaitGroup

		// Concurrent reads (ProcessEvent)
		for range 50 {
			wg.Go(func() {
				context := ctx.AcquireContextFromEvent(newTestPlatformEvent(platform.EventKindPrivateMessage), nil)
				eng.ProcessEvent(context)
			})
		}

		// Concurrent writes (register matcher)
		for range 10 {
			wg.Go(func() {
				eng.On(string(platform.EventKindGroupMessage)).Handle(func(c *ctx.Context) error {
					return nil
				})
			})
		}

		wg.Wait()
	})
}

// ============================================================================
// Error Handling Tests
// ============================================================================

func TestHandlerError(t *testing.T) {
	t.Run("create handler error", func(t *testing.T) {
		he := HandlerError{
			Message: "test error",
			Source:  "test-source",
			Attempt: 1,
			Trace:   []string{"mw1", "mw2"},
			EventID: "event-123",
		}

		assert.Equal(t, "test error", he.Error())
		assert.Equal(t, "test-source", he.Source)
		assert.Equal(t, 1, he.Attempt)
		assert.Equal(t, 2, len(he.Trace))
	})
}

// ============================================================================
// Matcher Stats Tests
// ============================================================================

func TestEngine_GetMatcherCount(t *testing.T) {
	eng := newEngineForTest(t)

	assert.Equal(t, 0, eng.GetMatcherCount())

	eng.On(string(platform.EventKindPrivateMessage))
	eng.On(string(platform.EventKindGroupMessage))

	assert.Equal(t, 2, eng.GetMatcherCount())
}

func TestEngine_GetTempMatcherCount(t *testing.T) {
	eng := newEngineForTest(t)

	assert.Equal(t, 0, eng.GetTempMatcherCount())

	eng.OnTemp(string(platform.EventKindPrivateMessage))

	assert.Equal(t, 1, eng.GetTempMatcherCount())
}

func TestEngine_GetMatcherStats(t *testing.T) {
	eng := newEngineForTest(t)

	eng.On(string(platform.EventKindPrivateMessage))
	eng.On(string(platform.EventKindGroupMessage))
	eng.OnTemp(string(platform.EventKindPrivateMessage))

	stats := eng.GetMatcherStats()

	assert.GreaterOrEqual(t, stats.Total, 2)
}

func TestEngine_SetMaxMatchers(t *testing.T) {
	eng := newEngineForTest(t)

	eng.SetMaxMatchers(100)

	assert.Equal(t, 100, eng.GetMaxMatchers())
}

// ============================================================================
// Snapshot and Restore Tests
// ============================================================================

func TestEngine_Snapshot(t *testing.T) {
	eng := newEngineForTest(t)

	eng.On(string(platform.EventKindPrivateMessage))
	eng.On(string(platform.EventKindGroupMessage))

	snapshot := eng.Snapshot()

	require.NotNil(t, snapshot)
	require.NotNil(t, snapshot.data)
}

func TestEngine_Restore(t *testing.T) {
	eng := newEngineForTest(t)

	eng.On(string(platform.EventKindPrivateMessage))
	snapshot := eng.Snapshot()

	eng.DeleteAllMatchers()
	assert.Equal(t, 0, eng.GetMatcherCount())

	eng.Restore(snapshot)
	assert.Equal(t, 1, eng.GetMatcherCount())
}

// ============================================================================
// Command Extraction Tests
// ============================================================================

func TestExtractCommand(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple command", "/test", "/test"},
		{"command with args", "/test arg1 arg2", "/test"},
		{"with leading space", "  /test", "/test"},
		{"no command", "hello world", "hello"}, // extractCommand returns first word
		{"empty string", "", ""},
		{"just slash", "/", "/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractCommand(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// ============================================================================
// Benchmark Tests
// ============================================================================

func BenchmarkEngine_ProcessEvent(b *testing.B) {
	eng := newEngineForTest(b)

	eng.On(string(platform.EventKindPrivateMessage)).Handle(func(c *ctx.Context) error {
		return nil
	})

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		context := ctx.AcquireContextFromEvent(newTestPlatformEvent(platform.EventKindPrivateMessage), nil)
		eng.ProcessEvent(context)
	}
}

func BenchmarkEngine_RegisterMatcher(b *testing.B) {
	eng := newEngineForTest(b)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		eng.On(string(platform.EventKindPrivateMessage)).Handle(func(c *ctx.Context) error {
			return nil
		})
	}
}

func BenchmarkEngine_ConcurrentProcess(b *testing.B) {
	eng := newEngineForTest(b)

	eng.On(string(platform.EventKindPrivateMessage)).Handle(func(c *ctx.Context) error {
		return nil
	})

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			context := ctx.AcquireContextFromEvent(newTestPlatformEvent(platform.EventKindPrivateMessage), nil)
			eng.ProcessEvent(context)
		}
	})
}
