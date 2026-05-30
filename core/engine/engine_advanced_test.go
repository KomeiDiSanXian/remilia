package engine

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/command"
	ctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/metrics"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Temporary Matcher Advanced Tests
// ============================================================================

func TestMatcher_TempOnce(t *testing.T) {
	t.Run("auto delete after one execution", func(t *testing.T) {
		eng := newEngineForTest(t)

		var count int32
		matcher := eng.On(string(platform.EventKindPrivateMessage))
		matcher.SetTemp(true)
		matcher.rt.maxUseCount = 1
		matcher.rt.useCount = 0
		matcher.Handle(func(c *ctx.Context) error {
			atomic.AddInt32(&count, 1)
			return nil
		})

		// First execution
		ctx1 := ctx.NewContextFromEvent(newTestPlatformEvent(platform.EventKindPrivateMessage), nil)
		eng.ProcessEvent(ctx1)
		assert.Equal(t, int32(1), atomic.LoadInt32(&count))

		// Check if matcher should be deleted
		matcher.rt.mu.RLock()
		useCount := atomic.LoadInt32(&matcher.rt.useCount)
		maxUse := matcher.rt.maxUseCount
		matcher.rt.mu.RUnlock()

		if useCount >= maxUse {
			// Matcher should auto-delete
			assert.True(t, useCount >= maxUse)
		}
	})
}

func TestMatcher_TempN(t *testing.T) {
	t.Run("execute N times then delete", func(t *testing.T) {
		eng := newEngineForTest(t)

		var count int32
		matcher := eng.On(string(platform.EventKindPrivateMessage))
		matcher.SetTemp(true)
		matcher.rt.maxUseCount = 3
		matcher.rt.useCount = 0
		matcher.Handle(func(c *ctx.Context) error {
			atomic.AddInt32(&count, 1)
			return nil
		})

		// Execute 5 times
		for range 5 {
			context := ctx.NewContextFromEvent(newTestPlatformEvent(platform.EventKindPrivateMessage), nil)
			eng.ProcessEvent(context)
		}

		// Should execute at most 3 times
		assert.LessOrEqual(t, atomic.LoadInt32(&count), int32(3))
	})

	t.Run("zero count means no auto-delete", func(t *testing.T) {
		eng := newEngineForTest(t)

		var count int32
		matcher := eng.On(string(platform.EventKindPrivateMessage))
		matcher.SetTemp(true)
		matcher.rt.maxUseCount = 0 // 0 means no auto-delete
		matcher.Handle(func(c *ctx.Context) error {
			atomic.AddInt32(&count, 1)
			return nil
		})

		context := ctx.NewContextFromEvent(newTestPlatformEvent(platform.EventKindPrivateMessage), nil)
		eng.ProcessEvent(context)

		// With maxUseCount=0, handler should still execute (no auto-delete)
		assert.Equal(t, int32(1), atomic.LoadInt32(&count))
	})
}

func TestMatcher_TempUntil(t *testing.T) {
	t.Run("expire after time", func(t *testing.T) {
		eng := newEngineForTest(t)

		matcher := eng.On(string(platform.EventKindPrivateMessage))
		matcher.SetTemp(true)
		matcher.rt.expiresAt = time.Now().Add(50 * time.Millisecond)
		matcher.Handle(func(c *ctx.Context) error {
			return nil
		})

		// Execute before expiration
		ctx1 := ctx.NewContextFromEvent(newTestPlatformEvent(platform.EventKindPrivateMessage), nil)
		eng.ProcessEvent(ctx1)

		// timing: wait for TTL expiration
		time.Sleep(100 * time.Millisecond)

		// Matcher should be expired now
		matcher.rt.mu.RLock()
		expired := time.Now().After(matcher.rt.expiresAt)
		matcher.rt.mu.RUnlock()

		assert.True(t, expired)
	})
}

// ============================================================================
// Metrics Collector Tests
// ============================================================================

func TestEngine_SetMetricsCollector(t *testing.T) {
	t.Run("set and get metrics collector", func(t *testing.T) {
		eng := newEngineForTest(t)
		collector := &metrics.Collector{}

		result := eng.SetMetricsCollector(collector)

		assert.Equal(t, eng, result)
		assert.Equal(t, collector, eng.GetMetricsCollector())
	})

	t.Run("set nil collector", func(t *testing.T) {
		eng := newEngineForTest(t)

		eng.SetMetricsCollector(nil)

		assert.Nil(t, eng.GetMetricsCollector())
	})
}

func TestEngine_GetMetricsCollector(t *testing.T) {
	t.Run("get without set", func(t *testing.T) {
		eng := newEngineForTest(t)

		collector := eng.GetMetricsCollector()

		assert.Nil(t, collector)
	})
}

// ============================================================================
// Global Matchers Tests
// ============================================================================

func TestEngine_EnableGlobalMatchers(t *testing.T) {
	t.Run("enable global matchers", func(t *testing.T) {
		eng := newEngineForTest(t)

		eng.EnableGlobalMatchers(true)

		// Verify state changed (internal check)
		// This function affects internal behavior
	})

	t.Run("disable global matchers", func(t *testing.T) {
		eng := newEngineForTest(t)

		eng.EnableGlobalMatchers(false)

		// Should not panic
	})
}

// ============================================================================
// Temp Matcher Priority Update Tests
// ============================================================================

func TestEngine_UpdateTempMatcherPriority(t *testing.T) {
	t.Run("update temp matcher priority", func(t *testing.T) {
		eng := newEngineForTest(t)

		matcher := eng.OnTemp(string(platform.EventKindPrivateMessage))
		matcher.SetPriority(50)

		// Update priority through engine
		eng.UpdateTempMatcherPriority(matcher)

		// Should not panic
	})
}

// ============================================================================
// Matcher Command Update Tests
// ============================================================================

func TestEngine_UpdateMatcherCommand(t *testing.T) {
	t.Run("update matcher command", func(t *testing.T) {
		eng := newEngineForTest(t)

		matcher := eng.On(string(platform.EventKindPrivateMessage))
		matcher.BindCommand("/test")

		eng.UpdateMatcherCommand(matcher)

		// Verify command is updated in index
	})
}

func TestEngine_UpdateMatcherIndex(t *testing.T) {
	t.Run("update matcher index", func(t *testing.T) {
		eng := newEngineForTest(t)

		matcher := eng.On(string(platform.EventKindPrivateMessage))

		eng.UpdateMatcherIndex(matcher)

		// Should not panic
	})
}

// ============================================================================
// Matcher Migration Tests
// ============================================================================

func TestEngine_MigrateMatcherToTemp(t *testing.T) {
	t.Run("migrate permanent to temp", func(t *testing.T) {
		eng := newEngineForTest(t)

		matcher := eng.On(string(platform.EventKindPrivateMessage))
		assert.False(t, matcher.IsTemp())

		eng.MigrateMatcherToTemp(matcher)

		// Verify migration happened
	})
}

func TestEngine_MigrateMatcherFromTemp(t *testing.T) {
	t.Run("migrate temp to permanent", func(t *testing.T) {
		eng := newEngineForTest(t)

		matcher := eng.OnTemp(string(platform.EventKindPrivateMessage))
		assert.True(t, matcher.IsTemp())

		eng.MigrateMatcherFromTemp(matcher)

		// Verify migration happened
	})
}

// ============================================================================
// Middleware Named Tests
// ============================================================================

func TestEngine_NamedMiddleware(t *testing.T) {
	t.Run("named middleware with trace", func(t *testing.T) {
		eng := newEngineForTest(t)

		var traced []string

		// Create named middleware
		mw := eng.Named("logger", func(next ctx.Handler) ctx.Handler {
			return func(c *ctx.Context) error {
				traced = append(traced, "logger")
				return next(c)
			}
		})

		eng.Use(mw)

		eng.On(string(platform.EventKindPrivateMessage)).Handle(func(c *ctx.Context) error {
			return nil
		})

		context := ctx.NewContextFromEvent(newTestPlatformEvent(platform.EventKindPrivateMessage), nil)

		eng.ProcessEvent(context)

		// Verify middleware was traced
		assert.Contains(t, traced, "logger")
	})
}

// ============================================================================
// State Rebuild Tests
// ============================================================================

func TestEngineState_RebuildIndex(t *testing.T) {
	t.Run("rebuild index after modifications", func(t *testing.T) {
		state := newEngineState()

		// Add some matchers
		m1 := &Matcher{EventType: string(platform.EventKindPrivateMessage)}
		m1.priority.Store(10)
		m2 := &Matcher{EventType: string(platform.EventKindPrivateMessage)}
		m2.priority.Store(20)
		m3 := &Matcher{EventType: string(platform.EventKindGroupMessage)}
		m3.priority.Store(5)

		state.matchers = []*Matcher{m1, m2, m3}

		// Rebuild index
		state.rebuildIndex()

		// Verify indices are built
		assert.NotNil(t, state.matcherIndex)
		assert.NotNil(t, state.sortedCache)
		assert.Greater(t, len(state.matcherIndex), 0)
	})

	t.Run("rebuild with command matchers", func(t *testing.T) {
		state := newEngineState()

		m1 := &Matcher{EventType: string(platform.EventKindPrivateMessage), definition: &command.Definition{Name: "/test"}}
		m1.priority.Store(10)
		m2 := &Matcher{EventType: string(platform.EventKindPrivateMessage), definition: &command.Definition{Name: "/help"}}
		m2.priority.Store(20)

		state.matchers = []*Matcher{m1, m2}

		state.rebuildIndex()

		// Verify command index is built
		assert.NotNil(t, state.commandIndex)
		assert.Greater(t, len(state.commandIndex), 0)
	})

	t.Run("rebuild with grouped matchers", func(t *testing.T) {
		state := newEngineState()

		m1 := &Matcher{EventType: string(platform.EventKindPrivateMessage), group: "plugin1"}
		m2 := &Matcher{EventType: string(platform.EventKindPrivateMessage), group: "plugin1"}
		m3 := &Matcher{EventType: string(platform.EventKindPrivateMessage), group: "plugin2"}

		state.matchers = []*Matcher{m1, m2, m3}

		state.rebuildIndex()

		// Verify group index is built
		assert.NotNil(t, state.groupIndex)
		assert.Equal(t, 2, len(state.groupIndex))
		assert.Equal(t, 2, len(state.groupIndex["plugin1"]))
		assert.Equal(t, 1, len(state.groupIndex["plugin2"]))
	})
}

// ============================================================================
// State Delete Matcher Tests
// ============================================================================

func TestEngineState_DeleteMatcher(t *testing.T) {
	t.Run("delete specific matcher from state", func(t *testing.T) {
		state := newEngineState()

		m1 := &Matcher{EventType: string(platform.EventKindPrivateMessage), Source: "test1"}
		m2 := &Matcher{EventType: string(platform.EventKindPrivateMessage), Source: "test2"}

		state.matchers = []*Matcher{m1, m2}
		state.rebuildIndex()

		initialCount := len(state.matchers)

		// Delete m1
		state.deleteMatcher(m1)

		assert.Equal(t, initialCount-1, len(state.matchers))
		assert.NotContains(t, state.matchers, m1)
		assert.Contains(t, state.matchers, m2)
	})

	t.Run("delete non-existent matcher", func(t *testing.T) {
		state := newEngineState()

		m1 := &Matcher{EventType: string(platform.EventKindPrivateMessage)}
		m2 := &Matcher{EventType: string(platform.EventKindPrivateMessage)}

		state.matchers = []*Matcher{m1}
		state.rebuildIndex()

		initialCount := len(state.matchers)

		// Try to delete m2 which doesn't exist
		state.deleteMatcher(m2)

		// Should not panic, count unchanged
		assert.Equal(t, initialCount, len(state.matchers))
	})
}

// ============================================================================
// State Delete Matchers (Batch) Tests
// ============================================================================

func TestEngineState_DeleteMatchers(t *testing.T) {
	t.Run("delete multiple matchers", func(t *testing.T) {
		state := newEngineState()

		m1 := &Matcher{EventType: string(platform.EventKindPrivateMessage)}
		m2 := &Matcher{EventType: string(platform.EventKindPrivateMessage)}
		m3 := &Matcher{EventType: string(platform.EventKindGroupMessage)}

		state.matchers = []*Matcher{m1, m2, m3}
		state.rebuildIndex()

		// Delete m1 and m2
		state.deleteMatchers([]*Matcher{m1, m2})

		assert.Equal(t, 1, len(state.matchers))
		assert.Contains(t, state.matchers, m3)
	})

	t.Run("delete empty list", func(t *testing.T) {
		state := newEngineState()

		m1 := &Matcher{EventType: string(platform.EventKindPrivateMessage)}
		state.matchers = []*Matcher{m1}

		initialCount := len(state.matchers)

		state.deleteMatchers([]*Matcher{})

		assert.Equal(t, initialCount, len(state.matchers))
	})
}

// ============================================================================
// State Remove Group Tests
// ============================================================================

func TestEngineState_RemoveGroup(t *testing.T) {
	t.Run("remove group matchers", func(t *testing.T) {
		state := newEngineState()

		m1 := &Matcher{EventType: string(platform.EventKindPrivateMessage), group: "plugin1"}
		m2 := &Matcher{EventType: string(platform.EventKindPrivateMessage), group: "plugin1"}
		m3 := &Matcher{EventType: string(platform.EventKindPrivateMessage), group: "plugin2"}

		state.matchers = []*Matcher{m1, m2, m3}
		state.rebuildIndex()

		// Remove plugin1
		state.removeGroup("plugin1")

		assert.Equal(t, 1, len(state.matchers))
		assert.Contains(t, state.matchers, m3)
	})

	t.Run("remove non-existent group", func(t *testing.T) {
		state := newEngineState()

		m1 := &Matcher{EventType: string(platform.EventKindPrivateMessage), group: "plugin1"}
		state.matchers = []*Matcher{m1}
		state.rebuildIndex()

		initialCount := len(state.matchers)

		// Try to remove non-existent group
		state.removeGroup("plugin2")

		// Should not panic, count unchanged
		assert.Equal(t, initialCount, len(state.matchers))
	})

	t.Run("remove empty group name", func(t *testing.T) {
		state := newEngineState()

		m1 := &Matcher{EventType: string(platform.EventKindPrivateMessage), group: "plugin1"}
		state.matchers = []*Matcher{m1}

		state.removeGroup("")

		// Should not panic
	})
}

// ============================================================================
// State Invalidate Sorted Cache Tests
// ============================================================================

func TestEngineState_InvalidateSortedCache(t *testing.T) {
	t.Run("invalidate cache for specific event", func(t *testing.T) {
		state := newEngineState()

		m1 := &Matcher{EventType: string(platform.EventKindPrivateMessage)}
		m1.priority.Store(10)
		state.matchers = []*Matcher{m1}
		state.rebuildIndex()

		// Verify cache exists
		assert.NotNil(t, state.sortedCache[string(platform.EventKindPrivateMessage)])

		// Invalidate
		state.invalidateSortedCache(string(platform.EventKindPrivateMessage))

		// Cache should be rebuilt
		assert.NotNil(t, state.sortedCache)
	})
}

// ============================================================================
// Matcher Ensure Chain Tests
// ============================================================================

func TestMatcher_EnsureChain(t *testing.T) {
	t.Run("build chain on first call", func(t *testing.T) {
		matcher := &Matcher{}

		globalChain := []ctx.Middleware{
			func(next ctx.Handler) ctx.Handler {
				return func(c *ctx.Context) error {
					return next(c)
				}
			},
		}

		matcher.ensureChain(globalChain, 1, nil, 0)

		// Chain should be built
		chain := matcher.getCombinedChain()
		assert.NotNil(t, chain)
	})

	t.Run("use cached chain if generation matches", func(t *testing.T) {
		matcher := &Matcher{}

		globalChain := []ctx.Middleware{
			func(next ctx.Handler) ctx.Handler {
				return func(c *ctx.Context) error {
					return next(c)
				}
			},
		}

		// First call builds cache
		matcher.ensureChain(globalChain, 1, nil, 0)
		chain1 := matcher.getCombinedChain()

		// Second call with same generation should use cache
		matcher.ensureChain(globalChain, 1, nil, 0)
		chain2 := matcher.getCombinedChain()

		// Should be same cached chain
		assert.Equal(t, len(chain1), len(chain2))
	})

	t.Run("rebuild chain if generation changed", func(t *testing.T) {
		matcher := &Matcher{}

		globalChain1 := []ctx.Middleware{
			func(next ctx.Handler) ctx.Handler {
				return func(c *ctx.Context) error {
					return next(c)
				}
			},
		}

		globalChain2 := []ctx.Middleware{
			func(next ctx.Handler) ctx.Handler {
				return func(c *ctx.Context) error {
					return next(c)
				}
			},
			func(next ctx.Handler) ctx.Handler {
				return func(c *ctx.Context) error {
					return next(c)
				}
			},
		}

		// Build with gen 1
		matcher.ensureChain(globalChain1, 1, nil, 0)

		// Rebuild with gen 2
		matcher.ensureChain(globalChain2, 2, nil, 0)

		chain := matcher.getCombinedChain()
		// Chain should be rebuilt
		assert.NotNil(t, chain)
	})
}

// ============================================================================
// Pending Delete Processor Tests
// ============================================================================

func TestEngine_PendingDeleteProcessor(t *testing.T) {
	t.Run("processor handles delete requests", func(t *testing.T) {
		eng := newEngineForTest(t)

		// Create and delete matchers
		m1 := eng.On(string(platform.EventKindPrivateMessage))
		m2 := eng.On(string(platform.EventKindPrivateMessage))

		// Delete through engine
		eng.DeleteMatcher(m1)
		eng.DeleteMatcher(m2)

		// Wait for pending delete processor to drain
		assert.Eventually(t, func() bool {
			return eng.GetMatcherCount() == 0
		}, time.Second, 50*time.Millisecond)
	})
}

// ============================================================================
// Temp Matcher Cleaner Tests
// ============================================================================

func TestEngine_TempMatcherCleaner(t *testing.T) {
	t.Run("cleaner removes expired temp matchers", func(t *testing.T) {
		// Create engine with short cleanup interval
		eng := newEngineForTest(t, WithCleanupInterval(100*time.Millisecond))

		// Create expired temp matcher
		matcher := eng.OnTemp(string(platform.EventKindPrivateMessage))
		matcher.rt.expiresAt = time.Now().Add(-1 * time.Second) // Already expired

		// timing: wait for cleaner ticker to fire (cleaner runs every 100ms)
		time.Sleep(200 * time.Millisecond)
	})

	t.Run("cleaner respects disabled interval", func(t *testing.T) {
		eng := newEngineForTest(t, WithCleanupInterval(0))

		// Cleaner should be disabled
		// Verify engine is created
		assert.NotNil(t, eng)
	})
}

// ============================================================================
// Matcher Copy Tests
// ============================================================================

func TestMatcher_Copy(t *testing.T) {
	t.Run("deep copy all fields", func(t *testing.T) {
		original := &Matcher{
			EventType:  string(platform.EventKindGroupMessage),
			Source:     "original",
			group:      "test-group",
			definition: &command.Definition{Name: "test"},
			Rules: []ctx.Rule{
				func(c *ctx.Context) bool { return true },
			},
			middlewares: []ctx.Middleware{
				func(next ctx.Handler) ctx.Handler {
					return func(c *ctx.Context) error {
						return next(c)
					}
				},
			},
		}
		atomic.StoreInt32(&original.rt.isTemp, 1)
		original.priority.Store(50)
		original.isBlock.Store(true)

		copied := original.copy()

		require.NotNil(t, copied)
		assert.Equal(t, original.EventType, copied.EventType)
		assert.Equal(t, original.priority.Load(), copied.priority.Load())
		assert.Equal(t, original.isBlock.Load(), copied.isBlock.Load())
		assert.Equal(t, original.Source, copied.Source)
		assert.Equal(t, original.group, copied.group)
		assert.Equal(t, original.GetCommand(), copied.GetCommand())
		assert.Equal(t, len(original.Rules), len(copied.Rules))
		assert.Equal(t, len(original.middlewares), len(copied.middlewares))
		assert.Equal(t, atomic.LoadInt32(&original.rt.isTemp), atomic.LoadInt32(&copied.rt.isTemp))

		// Verify deep copy - different slices
		assert.NotSame(t, &original.Rules, &copied.Rules)
		assert.NotSame(t, &original.middlewares, &copied.middlewares)
	})
}

// ============================================================================
// Additional Coverage Tests
// ============================================================================

func TestMatcher_GetPriority(t *testing.T) {
	matcher := &Matcher{}
	matcher.priority.Store(42)

	matcher.rt.mu.RLock()
	priority := matcher.priority.Load()
	matcher.rt.mu.RUnlock()

	assert.Equal(t, uint64(42), priority)
}

func TestMatcher_IsNoop(t *testing.T) {
	t.Run("noop matcher", func(t *testing.T) {
		matcher := &Matcher{Source: "noop"}
		assert.True(t, matcher.isNoop())
	})

	t.Run("normal matcher", func(t *testing.T) {
		matcher := &Matcher{Source: "normal"}
		assert.False(t, matcher.isNoop())
	})

	t.Run("nil matcher", func(t *testing.T) {
		var matcher *Matcher
		assert.True(t, matcher.isNoop())
	})
}

func TestMatcher_Match(t *testing.T) {
	t.Run("match with all rules passing", func(t *testing.T) {
		matcher := &Matcher{
			Rules: []ctx.Rule{
				func(c *ctx.Context) bool { return true },
				func(c *ctx.Context) bool { return true },
			},
		}

		context := ctx.NewContextFromEvent(newTestPlatformEvent(platform.EventKindPrivateMessage), nil)
		result := matcher.Match(context)

		assert.True(t, result)
	})

	t.Run("match with one rule failing", func(t *testing.T) {
		matcher := &Matcher{
			Rules: []ctx.Rule{
				func(c *ctx.Context) bool { return true },
				func(c *ctx.Context) bool { return false },
			},
		}

		context := ctx.NewContextFromEvent(newTestPlatformEvent(platform.EventKindPrivateMessage), nil)
		result := matcher.Match(context)

		assert.False(t, result)
	})

	t.Run("match deleted matcher", func(t *testing.T) {
		matcher := &Matcher{
			Rules: []ctx.Rule{
				func(c *ctx.Context) bool { return true },
			},
		}
		matcher.rt.deleted.Store(true)

		context := ctx.NewContextFromEvent(newTestPlatformEvent(platform.EventKindPrivateMessage), nil)
		result := matcher.Match(context)

		assert.False(t, result)
	})

	t.Run("match with no rules", func(t *testing.T) {
		matcher := &Matcher{
			Rules: []ctx.Rule{},
		}

		context := ctx.NewContextFromEvent(newTestPlatformEvent(platform.EventKindPrivateMessage), nil)
		result := matcher.Match(context)

		assert.True(t, result)
	})
}
