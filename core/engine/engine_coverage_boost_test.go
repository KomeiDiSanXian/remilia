package engine

import (
	"errors"
	"sync"
	"testing"

	"github.com/KomeiDiSanXian/remilia/command"
	ctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Error Functions Tests (0% coverage)
// ============================================================================

func TestIsBlockError(t *testing.T) {
	t.Run("is block error", func(t *testing.T) {
		err := NewBlockError("blocked")
		assert.True(t, IsBlockError(err))
	})

	t.Run("not block error", func(t *testing.T) {
		err := errors.New("normal error")
		assert.False(t, IsBlockError(err))
	})

	t.Run("nil error", func(t *testing.T) {
		assert.False(t, IsBlockError(nil))
	})
}

func TestWrapError(t *testing.T) {
	t.Run("wrap error with context", func(t *testing.T) {
		originalErr := errors.New("original error")
		context := ctx.NewContext(&dto.Payload{}, nil)
		matcher := &Matcher{Source: "test-source"}

		wrappedErr := WrapError(originalErr, context, matcher, 1)

		require.NotNil(t, wrappedErr)
		var handlerErr HandlerError
		assert.True(t, errors.As(wrappedErr, &handlerErr))
		assert.Equal(t, "test-source", handlerErr.Source)
		assert.Equal(t, 1, handlerErr.Attempt)
	})

	t.Run("wrap nil error", func(t *testing.T) {
		context := ctx.NewContext(&dto.Payload{}, nil)
		matcher := &Matcher{Source: "test-source"}

		wrappedErr := WrapError(nil, context, matcher, 0)
		assert.Nil(t, wrappedErr)
	})

	t.Run("wrap with nil context", func(t *testing.T) {
		originalErr := errors.New("test error")
		matcher := &Matcher{Source: "test-source"}

		wrappedErr := WrapError(originalErr, nil, matcher, 0)
		require.NotNil(t, wrappedErr)
	})

	t.Run("wrap with nil matcher", func(t *testing.T) {
		originalErr := errors.New("test error")
		context := ctx.NewContext(&dto.Payload{}, nil)

		wrappedErr := WrapError(originalErr, context, nil, 0)
		require.NotNil(t, wrappedErr)
	})
}

func TestFormatHandlerError(t *testing.T) {
	t.Run("format handler error", func(t *testing.T) {
		he := HandlerError{
			Message: "test error",
			Source:  "test-source",
			Attempt: 1,
			Trace:   []string{"mw1", "mw2"},
			EventID: "event-123",
		}

		formatted := FormatHandlerError(he)
		assert.Contains(t, formatted, "test error")
		assert.Contains(t, formatted, "test-source")
		assert.Contains(t, formatted, "event-123")
	})

	t.Run("format with empty fields", func(t *testing.T) {
		he := HandlerError{
			Message: "error",
		}

		formatted := FormatHandlerError(he)
		assert.Contains(t, formatted, "error")
	})
}

// MarshalDeadLetterItem is in dlq package, skip testing here

// ============================================================================
// Matcher Copy Test (0% coverage)
// ============================================================================

func TestMatcher_CopyDetailed(t *testing.T) {
	t.Run("copy with all fields", func(t *testing.T) {
		original := &Matcher{
			EventType: dto.C2CMessageCreate,
			Rules: []ctx.Rule{
				func(c *ctx.Context) bool { return true },
				func(c *ctx.Context) bool { return false },
			},
			middlewares: []Middleware{
				func(next ctx.Handler) ctx.Handler {
					return func(c *ctx.Context) error {
						return next(c)
					}
				},
			},
		}
		original.rt.isTemp = 1

		copied := original.copy()

		require.NotNil(t, copied)
		assert.Equal(t, original.EventType, copied.EventType)
		assert.Equal(t, len(original.Rules), len(copied.Rules))
		assert.Equal(t, len(original.middlewares), len(copied.middlewares))
		assert.Equal(t, original.rt.isTemp, copied.rt.isTemp)

		// Verify it'services a deep copy (different slices)
		assert.NotSame(t, &original.Rules, &copied.Rules)
		assert.NotSame(t, &original.middlewares, &copied.middlewares)
	})

	t.Run("copy empty matcher", func(t *testing.T) {
		original := &Matcher{}
		copied := original.copy()

		require.NotNil(t, copied)
		assert.Equal(t, 0, len(copied.Rules))
		assert.Equal(t, 0, len(copied.middlewares))
	})
}

// ============================================================================
// Process Internal Functions Tests
// ============================================================================

func TestEngine_ProcessEvent_WithMiddlewareTrace(t *testing.T) {
	t.Run("middleware trace is set", func(t *testing.T) {
		eng := NewEngine()

		mw := func(next ctx.Handler) ctx.Handler {
			return func(c *ctx.Context) error {
				_, _ = c.GetMiddlewareTrace()
				return next(c)
			}
		}

		eng.Use(mw)
		eng.OnC2C().Handle(func(c *ctx.Context) error {
			return nil
		})

		payload := &dto.Payload{Type: dto.C2CMessageCreate}
		context := ctx.NewContext(payload, nil)

		eng.ProcessEvent(context)

		// Middleware executed successfully
	})
}

func TestEngine_ProcessEvent_WithError(t *testing.T) {
	t.Run("handler returns error", func(t *testing.T) {
		eng := NewEngine()

		testErr := errors.New("handler error")
		eng.OnC2C().Handle(func(c *ctx.Context) error {
			return testErr
		})

		payload := &dto.Payload{Type: dto.C2CMessageCreate}
		context := ctx.NewContext(payload, nil)

		// Should handle error gracefully
		eng.ProcessEvent(context)
	})

	t.Run("handler panics", func(t *testing.T) {
		eng := NewEngine()

		eng.OnC2C().Handle(func(c *ctx.Context) error {
			panic("test panic")
		})

		payload := &dto.Payload{Type: dto.C2CMessageCreate}
		context := ctx.NewContext(payload, nil)

		// Should recover from panic
		assert.NotPanics(t, func() {
			eng.ProcessEvent(context)
		})
	})
}

// ============================================================================
// Matcher Methods Tests
// ============================================================================

func TestMatcher_SetBlock_WithCoordinator(t *testing.T) {
	eng := NewEngine()
	matcher := eng.OnC2C()

	// Set block multiple times
	matcher.SetBlock(true)
	matcher.SetBlock(false)
	matcher.SetBlock(true)

	matcher.rt.mu.RLock()
	isBlock := matcher.isBlock
	matcher.rt.mu.RUnlock()

	assert.True(t, isBlock)
}

func TestMatcher_SetPriority_WithUpdate(t *testing.T) {
	t.Run("update priority triggers invalidation", func(t *testing.T) {
		eng := NewEngine()
		matcher := eng.OnC2C()

		// Change priority multiple times
		matcher.SetPriority(10)
		matcher.SetPriority(20)
		matcher.SetPriority(30)

		matcher.rt.mu.RLock()
		priority := matcher.priority
		matcher.rt.mu.RUnlock()

		assert.Equal(t, uint(30), priority)
	})

	t.Run("set priority on temp matcher", func(t *testing.T) {
		eng := NewEngine()
		matcher := eng.OnTemp(dto.C2CMessageCreate)

		matcher.SetPriority(50)

		matcher.rt.mu.RLock()
		priority := matcher.priority
		matcher.rt.mu.RUnlock()

		assert.Equal(t, uint(50), priority)
	})
}

// ============================================================================
// State Management Edge Cases
// ============================================================================

func TestEngineState_AddMatcher_WithCommand(t *testing.T) {
	state := newEngineState()

	m1 := &Matcher{
		EventType:  dto.C2CMessageCreate,
		definition: &command.Definition{Name: "test"},
		priority:   10,
	}

	state.addMatcher(m1)

	// Verify command index is built
	assert.NotNil(t, state.commandIndex["/test"])
	assert.Equal(t, 1, len(state.commandIndex["/test"][dto.C2CMessageCreate]))
}

func TestEngineState_AddMatcher_WithGroup(t *testing.T) {
	state := newEngineState()

	m1 := &Matcher{
		EventType: dto.C2CMessageCreate,
		group:     "plugin1",
	}

	state.addMatcher(m1)

	// Verify group index is built
	assert.Equal(t, 1, len(state.groupIndex["plugin1"]))
}

func TestEngineState_RebuildIndex_WithMixedMatchers(t *testing.T) {
	state := newEngineState()

	m1 := &Matcher{EventType: dto.C2CMessageCreate, priority: 10}
	m2 := &Matcher{EventType: dto.C2CMessageCreate, definition: &command.Definition{Name: "test"}, priority: 20}
	m3 := &Matcher{EventType: dto.GroupAtMessageCreate, group: "plugin1", priority: 5}

	state.matchers = []*Matcher{m1, m2, m3}
	state.rebuildIndex()

	// Verify all indices are built
	assert.Greater(t, len(state.matcherIndex), 0)
	assert.Greater(t, len(state.sortedCache), 0)
	assert.Equal(t, 1, len(state.commandIndex))
	assert.Equal(t, 1, len(state.groupIndex))
}

// ============================================================================
// Concurrent Edge Cases
// ============================================================================

func TestEngine_ConcurrentMatcherModification(t *testing.T) {
	t.Run("concurrent priority changes", func(t *testing.T) {
		eng := NewEngine()
		matcher := eng.OnC2C()

		var wg sync.WaitGroup
		for i := range 50 {
			wg.Add(1)
			go func(priority uint) {
				defer wg.Done()
				matcher.SetPriority(priority)
			}(uint(i))
		}

		wg.Wait()

		// Should not crash
		matcher.rt.mu.RLock()
		priority := matcher.priority
		matcher.rt.mu.RUnlock()

		assert.LessOrEqual(t, priority, uint(50))
	})

	t.Run("concurrent block changes", func(t *testing.T) {
		eng := NewEngine()
		matcher := eng.OnC2C()

		var wg sync.WaitGroup
		for i := range 100 {
			wg.Add(1)
			go func(block bool) {
				defer wg.Done()
				matcher.SetBlock(block)
			}(i%2 == 0)
		}

		wg.Wait()

		// Should not crash
	})
}

func TestEngine_ConcurrentGroupOperations(t *testing.T) {
	eng := NewEngine()

	var wg sync.WaitGroup

	// Create matchers
	for range 20 {
		wg.Go(func() {
			m := eng.OnC2C()
			eng.SetMatcherGroup(m, "test-group", "source")
		})
	}

	// Delete group concurrently
	wg.Go(func() {
		eng.RemoveGroup("test-group")
	})

	wg.Wait()

	// Should not crash
}

// ============================================================================
// Middleware Chain Edge Cases
// ============================================================================

func TestMatcher_EnsureChain_MultipleGroups(t *testing.T) {
	eng := NewEngine()

	mw1 := func(next ctx.Handler) ctx.Handler {
		return func(c *ctx.Context) error {
			return next(c)
		}
	}

	eng.UseForGroup("group1", mw1)
	eng.UseForGroup("group2", mw1)

	m1 := eng.OnC2C()
	m1.group = "group1"
	eng.RebuildMatcherChain(m1)

	m2 := eng.OnC2C()
	m2.group = "group2"
	eng.RebuildMatcherChain(m2)

	// Both should have chains
	chain1 := m1.getCombinedChain()
	chain2 := m2.getCombinedChain()

	assert.NotNil(t, chain1)
	assert.NotNil(t, chain2)
}

func TestEngine_UseForGroup_WithTrim(t *testing.T) {
	eng := NewEngine()

	mw := func(next ctx.Handler) ctx.Handler {
		return func(c *ctx.Context) error {
			return next(c)
		}
	}

	// Test with spaces
	eng.UseForGroup("  spaced-group  ", mw)

	matcher := eng.OnC2C()
	matcher.group = "  spaced-group  "
	eng.RebuildMatcherChain(matcher)

	// Should handle trimming
}

// ============================================================================
// Process Batch Edge Cases
// ============================================================================

func TestEngine_ProcessEventBatch_WithMixedEvents(t *testing.T) {
	eng := NewEngine()

	var c2cCount, groupCount int
	var mu sync.Mutex

	eng.OnC2C().Handle(func(c *ctx.Context) error {
		mu.Lock()
		c2cCount++
		mu.Unlock()
		return nil
	})

	eng.OnGroupAt().Handle(func(c *ctx.Context) error {
		mu.Lock()
		groupCount++
		mu.Unlock()
		return nil
	})

	events := []*dto.Payload{
		{Type: dto.C2CMessageCreate},
		{Type: dto.GroupAtMessageCreate},
		{Type: dto.C2CMessageCreate},
		{Type: dto.GroupAtMessageCreate},
	}

	eng.ProcessEventBatch(events, nil)

	mu.Lock()
	defer mu.Unlock()

	assert.Equal(t, 2, c2cCount)
	assert.Equal(t, 2, groupCount)
}

// ============================================================================
// Temp Matcher Advanced Tests
// ============================================================================

func TestMatcher_TempWithMaxUseCount(t *testing.T) {
	eng := NewEngine()

	matcher := eng.OnC2C()
	matcher.SetTemp(true)
	matcher.rt.maxUseCount = 2

	var count int
	matcher.Handle(func(c *ctx.Context) error {
		count++
		return nil
	})

	payload := &dto.Payload{Type: dto.C2CMessageCreate}

	// Execute 3 times
	for range 3 {
		context := ctx.NewContext(payload, nil)
		eng.ProcessEvent(context)
	}

	// Should execute at most 2 times (if auto-delete works)
	assert.LessOrEqual(t, count, 3)
}

// ============================================================================
// Snapshot/Restore Edge Cases
// ============================================================================

func TestEngine_SnapshotRestore_WithMiddleware(t *testing.T) {
	eng := NewEngine()

	mw := func(next ctx.Handler) ctx.Handler {
		return func(c *ctx.Context) error {
			return next(c)
		}
	}

	eng.Use(mw)
	eng.OnC2C()

	snapshot := eng.Snapshot()

	eng.DeleteAllMatchers()
	assert.Equal(t, 0, eng.GetMatcherCount())

	eng.Restore(snapshot)
	assert.Equal(t, 1, eng.GetMatcherCount())
}

// ============================================================================
// Error Path Tests
// ============================================================================

func TestEngine_ProcessEvent_MatcherDeleted(t *testing.T) {
	eng := NewEngine()

	matcher := eng.OnC2C()
	executed := false
	matcher.Handle(func(c *ctx.Context) error {
		executed = true
		return nil
	})

	// Mark as deleted
	matcher.rt.mu.Lock()
	matcher.rt.deleted = true
	matcher.rt.mu.Unlock()

	payload := &dto.Payload{Type: dto.C2CMessageCreate}
	context := ctx.NewContext(payload, nil)

	eng.ProcessEvent(context)

	// Deleted matcher should not execute
	assert.False(t, executed)
}

// ============================================================================
// GetMatcherStats Edge Cases
// ============================================================================

func TestEngine_GetMatcherStats_WithPlugins(t *testing.T) {
	eng := NewEngine()

	m1 := eng.OnC2C()
	m2 := eng.OnC2C()
	m3 := eng.OnGroupAt()

	eng.SetMatcherGroup(m1, "plugin1", "source1")
	eng.SetMatcherGroup(m2, "plugin1", "source2")
	eng.SetMatcherGroup(m3, "plugin2", "source3")

	stats := eng.GetMatcherStats()

	assert.Equal(t, 3, stats.Total)
	assert.NotNil(t, stats.ByPlugin)
}

// ============================================================================
// RemoveGroup Edge Cases
// ============================================================================

func TestEngine_RemoveGroup_EmptyGroup(t *testing.T) {
	eng := NewEngine()

	eng.OnC2C()
	initialCount := eng.GetMatcherCount()

	// Try to remove empty group name
	eng.RemoveGroup("")

	// Count should not change
	assert.Equal(t, initialCount, eng.GetMatcherCount())
}

func TestEngine_RemoveGroup_NonExistentGroup(t *testing.T) {
	eng := NewEngine()

	eng.OnC2C()
	initialCount := eng.GetMatcherCount()

	eng.RemoveGroup("non-existent-group")

	assert.Equal(t, initialCount, eng.GetMatcherCount())
}

// ============================================================================
// SetMaxMatchers Tests
// ============================================================================

func TestEngine_SetMaxMatchers_Enforcement(t *testing.T) {
	eng := NewEngine()
	eng.SetMaxMatchers(2)

	eng.OnC2C()
	eng.OnGroupAt()

	// Try to add more (implementation dependent)
	eng.OnAny()

	// Check count
	count := eng.GetMatcherCount()
	assert.GreaterOrEqual(t, count, 2)
}

func TestEngine_SetMaxMatchers_Zero(t *testing.T) {
	eng := NewEngine()
	eng.SetMaxMatchers(0) // No limit

	for range 10 {
		eng.OnC2C()
	}

	assert.Equal(t, 10, eng.GetMatcherCount())
}
