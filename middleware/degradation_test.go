package middleware

import (
	"context"
	"testing"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testReg returns a fresh prometheus.Registerer for test isolation.
func testReg() prometheus.Registerer { return prometheus.NewRegistry() }

// ── A. AdaptiveDegradation construction and defaults ────────────────────────

func TestAdaptiveDegradation_ConstructionAndDefaults(t *testing.T) {
	t.Run("NewAdaptiveDegradation empty config applies defaults", func(t *testing.T) {
		ad := NewAdaptiveDegradationWithRegistry(DegradationConfig{}, testReg())
		ad.mu.RLock()
		assert.Equal(t, 80.0, ad.config.CPUThreshold)
		assert.Equal(t, 85.0, ad.config.MemoryThreshold)
		assert.Equal(t, 500*time.Millisecond, ad.config.LatencyThreshold)
		assert.Equal(t, 5*time.Second, ad.config.MonitorInterval)
		assert.Equal(t, 10*time.Second, ad.config.RecoveryInterval)
		assert.Equal(t, 1000, ad.config.DelayQueueSize)
		assert.Equal(t, 10000, ad.config.GoroutineThreshold)
		assert.NotNil(t, ad.config.PriorityClassifier)
		ad.mu.RUnlock()
	})

	t.Run("NewAdaptiveDegradationWithRegistry custom registry doesnt panic", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		ad := NewAdaptiveDegradationWithRegistry(DegradationConfig{}, reg)
		assert.NotNil(t, ad)
	})

	t.Run("GetLevel starts at LevelNormal", func(t *testing.T) {
		ad := NewAdaptiveDegradationWithRegistry(DegradationConfig{}, testReg())
		assert.Equal(t, LevelNormal, ad.GetLevel())
	})
}

// ── B. ForceLevel and manual level control ──────────────────────────────────

func TestAdaptiveDegradation_ForceLevel(t *testing.T) {
	t.Run("force LevelLight", func(t *testing.T) {
		ad := NewAdaptiveDegradationWithRegistry(DegradationConfig{}, testReg())
		ad.ForceLevel(LevelLight)
		assert.Equal(t, LevelLight, ad.GetLevel())
	})

	t.Run("force LevelModerate", func(t *testing.T) {
		ad := NewAdaptiveDegradationWithRegistry(DegradationConfig{}, testReg())
		ad.ForceLevel(LevelModerate)
		assert.Equal(t, LevelModerate, ad.GetLevel())
	})

	t.Run("force LevelSevere", func(t *testing.T) {
		ad := NewAdaptiveDegradationWithRegistry(DegradationConfig{}, testReg())
		ad.ForceLevel(LevelSevere)
		assert.Equal(t, LevelSevere, ad.GetLevel())
	})

	t.Run("force LevelNormal resets", func(t *testing.T) {
		ad := NewAdaptiveDegradationWithRegistry(DegradationConfig{}, testReg())
		ad.ForceLevel(LevelSevere)
		ad.ForceLevel(LevelNormal)
		assert.Equal(t, LevelNormal, ad.GetLevel())
	})

	t.Run("force same level is idempotent", func(t *testing.T) {
		ad := NewAdaptiveDegradationWithRegistry(DegradationConfig{}, testReg())
		ad.ForceLevel(LevelModerate)
		ad.ForceLevel(LevelModerate)
		assert.Equal(t, LevelModerate, ad.GetLevel())
	})
}

// ── C. Middleware behavior at different levels ──────────────────────────────

func TestAdaptiveDegradation_MiddlewareNormal(t *testing.T) {
	t.Run("at LevelNormal all events pass through", func(t *testing.T) {
		ad := NewAdaptiveDegradationWithRegistry(DegradationConfig{
			PriorityClassifier: func(ctx *eventctx.Context) EventPriority {
				return PriorityLow
			},
		}, testReg())
		mw := ad.Middleware()
		called := 0
		h := mw(func(ctx *eventctx.Context) error {
			called++
			return nil
		})
		err := h(createTestContext())
		assert.NoError(t, err)
		assert.Equal(t, 1, called)
	})
}

func TestAdaptiveDegradation_MiddlewareLevelLight(t *testing.T) {
	t.Run("drops low priority at Light", func(t *testing.T) {
		ad := NewAdaptiveDegradationWithRegistry(DegradationConfig{
			Strategy: DegradationDrop,
			PriorityClassifier: func(ctx *eventctx.Context) EventPriority {
				return PriorityLow
			},
		}, testReg())
		ad.ForceLevel(LevelLight)
		mw := ad.Middleware()
		called := 0
		h := mw(func(ctx *eventctx.Context) error {
			called++
			return nil
		})
		err := h(createTestContext())
		assert.NoError(t, err)
		assert.Equal(t, 0, called)
	})

	t.Run("passes normal priority at Light", func(t *testing.T) {
		ad := NewAdaptiveDegradationWithRegistry(DegradationConfig{
			Strategy: DegradationDrop,
			PriorityClassifier: func(ctx *eventctx.Context) EventPriority {
				return PriorityNormal
			},
		}, testReg())
		ad.ForceLevel(LevelLight)
		mw := ad.Middleware()
		called := 0
		h := mw(func(ctx *eventctx.Context) error {
			called++
			return nil
		})
		err := h(createTestContext())
		assert.NoError(t, err)
		assert.Equal(t, 1, called)
	})

	t.Run("passes high and critical at Light", func(t *testing.T) {
		for _, pri := range []EventPriority{PriorityHigh, PriorityCritical} {
			ad := NewAdaptiveDegradationWithRegistry(DegradationConfig{
				Strategy: DegradationDrop,
				PriorityClassifier: func(ctx *eventctx.Context) EventPriority {
					return pri
				},
			}, testReg())
			ad.ForceLevel(LevelLight)
			mw := ad.Middleware()
			called := 0
			h := mw(func(ctx *eventctx.Context) error {
				called++
				return nil
			})
			h(createTestContext())
			assert.Equal(t, 1, called, "priority %d should pass at Light level", pri)
		}
	})
}

func TestAdaptiveDegradation_MiddlewareLevelModerate(t *testing.T) {
	t.Run("drops low and normal at Moderate", func(t *testing.T) {
		for _, pri := range []EventPriority{PriorityLow, PriorityNormal} {
			ad := NewAdaptiveDegradationWithRegistry(DegradationConfig{
				Strategy: DegradationDrop,
				PriorityClassifier: func(ctx *eventctx.Context) EventPriority {
					return pri
				},
			}, testReg())
			ad.ForceLevel(LevelModerate)
			mw := ad.Middleware()
			called := 0
			h := mw(func(ctx *eventctx.Context) error {
				called++
				return nil
			})
			h(createTestContext())
			assert.Equal(t, 0, called, "priority %d should be dropped at Moderate level", pri)
		}
	})

	t.Run("passes high and critical at Moderate", func(t *testing.T) {
		for _, pri := range []EventPriority{PriorityHigh, PriorityCritical} {
			ad := NewAdaptiveDegradationWithRegistry(DegradationConfig{
				Strategy: DegradationDrop,
				PriorityClassifier: func(ctx *eventctx.Context) EventPriority {
					return pri
				},
			}, testReg())
			ad.ForceLevel(LevelModerate)
			mw := ad.Middleware()
			called := 0
			h := mw(func(ctx *eventctx.Context) error {
				called++
				return nil
			})
			h(createTestContext())
			assert.Equal(t, 1, called, "priority %d should pass at Moderate level", pri)
		}
	})
}

func TestAdaptiveDegradation_MiddlewareLevelSevere(t *testing.T) {
	t.Run("drops low normal high at Severe", func(t *testing.T) {
		for _, pri := range []EventPriority{PriorityLow, PriorityNormal, PriorityHigh} {
			ad := NewAdaptiveDegradationWithRegistry(DegradationConfig{
				Strategy: DegradationDrop,
				PriorityClassifier: func(ctx *eventctx.Context) EventPriority {
					return pri
				},
			}, testReg())
			ad.ForceLevel(LevelSevere)
			mw := ad.Middleware()
			called := 0
			h := mw(func(ctx *eventctx.Context) error {
				called++
				return nil
			})
			h(createTestContext())
			assert.Equal(t, 0, called, "priority %d should be dropped at Severe level", pri)
		}
	})

	t.Run("passes critical at Severe", func(t *testing.T) {
		ad := NewAdaptiveDegradationWithRegistry(DegradationConfig{
			Strategy: DegradationDrop,
			PriorityClassifier: func(ctx *eventctx.Context) EventPriority {
				return PriorityCritical
			},
		}, testReg())
		ad.ForceLevel(LevelSevere)
		mw := ad.Middleware()
		called := 0
		h := mw(func(ctx *eventctx.Context) error {
			called++
			return nil
		})
		h(createTestContext())
		assert.Equal(t, 1, called)
	})
}

func TestAdaptiveDegradation_MiddlewareStrategies(t *testing.T) {
	t.Run("DegradationDelay delays normal priority at Light", func(t *testing.T) {
		ad := NewAdaptiveDegradationWithRegistry(DegradationConfig{
			Strategy: DegradationDelay,
			PriorityClassifier: func(ctx *eventctx.Context) EventPriority {
				return PriorityNormal
			},
		}, testReg())
		ad.ForceLevel(LevelLight)
		mw := ad.Middleware()
		called := 0
		h := mw(func(ctx *eventctx.Context) error {
			called++
			return nil
		})
		start := time.Now()
		err := h(createTestContext())
		elapsed := time.Since(start)
		assert.NoError(t, err)
		assert.Equal(t, 1, called)
		assert.GreaterOrEqual(t, elapsed, 95*time.Millisecond,
			"normal priority at Light with DegradationDelay should delay ~100ms")
	})

	t.Run("DegradationSimplify sets degraded flag", func(t *testing.T) {
		ad := NewAdaptiveDegradationWithRegistry(DegradationConfig{
			Strategy: DegradationSimplify,
			PriorityClassifier: func(ctx *eventctx.Context) EventPriority {
				return PriorityNormal
			},
		}, testReg())
		ad.ForceLevel(LevelLight)
		mw := ad.Middleware()
		var degraded bool
		h := mw(func(ctx *eventctx.Context) error {
			degraded = IsDegraded(ctx)
			return nil
		})
		err := h(createTestContext())
		assert.NoError(t, err)
		assert.True(t, degraded)
	})

	t.Run("DegradationDelay respects context cancellation", func(t *testing.T) {
		ad := NewAdaptiveDegradationWithRegistry(DegradationConfig{
			Strategy: DegradationDelay,
			PriorityClassifier: func(ctx *eventctx.Context) EventPriority {
				return PriorityNormal
			},
		}, testReg())
		ad.ForceLevel(LevelLight)
		mw := ad.Middleware()

		ctx := createTestContext()
		cancelCtx, cancel := context.WithCancel(context.Background())
		cancel()
		ctx.SetStdContext(cancelCtx)

		h := mw(func(ctx2 *eventctx.Context) error {
			return nil
		})
		err := h(ctx)
		assert.Error(t, err)
	})

	t.Run("DegradationDrop passes high priority through at Light", func(t *testing.T) {
		ad := NewAdaptiveDegradationWithRegistry(DegradationConfig{
			Strategy: DegradationDrop,
			PriorityClassifier: func(ctx *eventctx.Context) EventPriority {
				return PriorityHigh
			},
		}, testReg())
		ad.ForceLevel(LevelLight)
		mw := ad.Middleware()
		called := 0
		h := mw(func(ctx *eventctx.Context) error {
			called++
			return nil
		})
		err := h(createTestContext())
		assert.NoError(t, err)
		assert.Equal(t, 1, called)
	})
}

// ── D. Statistics tracking ──────────────────────────────────────────────────

func TestAdaptiveDegradation_Stats(t *testing.T) {
	t.Run("TotalEvents after middleware calls", func(t *testing.T) {
		ad := NewAdaptiveDegradationWithRegistry(DegradationConfig{}, testReg())
		mw := ad.Middleware()
		h := mw(mockHandler(nil, 0))
		for range 5 {
			h(createTestContext())
		}
		stats := ad.Stats()
		assert.Equal(t, int64(5), stats.TotalEvents)
	})

	t.Run("DroppedEvents after drops", func(t *testing.T) {
		ad := NewAdaptiveDegradationWithRegistry(DegradationConfig{
			Strategy: DegradationDrop,
			PriorityClassifier: func(ctx *eventctx.Context) EventPriority {
				return PriorityLow
			},
		}, testReg())
		ad.ForceLevel(LevelLight)
		mw := ad.Middleware()
		h := mw(mockHandler(nil, 0))
		for range 3 {
			h(createTestContext())
		}
		stats := ad.Stats()
		assert.Equal(t, int64(3), stats.DroppedEvents)
	})

	t.Run("DelayedEvents after delays", func(t *testing.T) {
		ad := NewAdaptiveDegradationWithRegistry(DegradationConfig{
			Strategy: DegradationDelay,
			PriorityClassifier: func(ctx *eventctx.Context) EventPriority {
				return PriorityNormal
			},
		}, testReg())
		ad.ForceLevel(LevelLight)
		mw := ad.Middleware()
		h := mw(mockHandler(nil, 0))
		for range 2 {
			h(createTestContext())
		}
		stats := ad.Stats()
		assert.Equal(t, int64(2), stats.DelayedEvents)
	})

	t.Run("Stats reflects current level", func(t *testing.T) {
		ad := NewAdaptiveDegradationWithRegistry(DegradationConfig{}, testReg())
		ad.ForceLevel(LevelModerate)
		stats := ad.Stats()
		assert.Equal(t, LevelModerate, stats.Level)

		ad.ForceLevel(LevelNormal)
		stats = ad.Stats()
		assert.Equal(t, LevelNormal, stats.Level)
	})
}

// ── E. Reset and UpdateConfig ───────────────────────────────────────────────

func TestAdaptiveDegradation_Reset(t *testing.T) {
	t.Run("Reset zeros all counters", func(t *testing.T) {
		ad := NewAdaptiveDegradationWithRegistry(DegradationConfig{
			PriorityClassifier: func(ctx *eventctx.Context) EventPriority {
				return PriorityLow
			},
		}, testReg())
		ad.ForceLevel(LevelLight)
		mw := ad.Middleware()
		h := mw(mockHandler(nil, 0))
		for range 5 {
			h(createTestContext())
		}
		ad.Reset()
		stats := ad.Stats()
		assert.Equal(t, int64(0), stats.TotalEvents)
		assert.Equal(t, int64(0), stats.DroppedEvents)
		assert.Equal(t, int64(0), stats.DelayedEvents)
	})
}

func TestAdaptiveDegradation_UpdateConfig(t *testing.T) {
	t.Run("UpdateConfig changes config values", func(t *testing.T) {
		ad := NewAdaptiveDegradationWithRegistry(DegradationConfig{}, testReg())
		ad.UpdateConfig(DegradationConfig{
			CPUThreshold:         90.0,
			MemoryThreshold:      95.0,
			LatencyThreshold:     1 * time.Second,
			GoroutineThreshold:   20000,
			EnableGoroutineLimit: true,
		})
		ad.mu.RLock()
		assert.Equal(t, 90.0, ad.config.CPUThreshold)
		assert.Equal(t, 95.0, ad.config.MemoryThreshold)
		assert.Equal(t, 1*time.Second, ad.config.LatencyThreshold)
		assert.Equal(t, 20000, ad.config.GoroutineThreshold)
		assert.True(t, ad.config.EnableGoroutineLimit)
		ad.mu.RUnlock()
	})

	t.Run("UpdateConfig zero values preserve existing", func(t *testing.T) {
		ad := NewAdaptiveDegradationWithRegistry(DegradationConfig{}, testReg())
		ad.mu.RLock()
		assert.Equal(t, 80.0, ad.config.CPUThreshold)
		ad.mu.RUnlock()

		ad.UpdateConfig(DegradationConfig{})
		ad.mu.RLock()
		assert.Equal(t, 80.0, ad.config.CPUThreshold)
		assert.Equal(t, 85.0, ad.config.MemoryThreshold)
		assert.Equal(t, 500*time.Millisecond, ad.config.LatencyThreshold)
		ad.mu.RUnlock()
	})
}

// ── F. OnLevelChange callback ──────────────────────────────────────────────

func TestAdaptiveDegradation_OnLevelChange(t *testing.T) {
	t.Run("callback fires on ForceLevel with correct from/to", func(t *testing.T) {
		var from, to DegradationLevel
		ad := NewAdaptiveDegradationWithRegistry(DegradationConfig{
			OnLevelChange: func(f, t DegradationLevel) {
				from, to = f, t
			},
		}, testReg())
		ad.ForceLevel(LevelModerate)
		assert.Equal(t, LevelNormal, from)
		assert.Equal(t, LevelModerate, to)
	})

	t.Run("callback fires again on second ForceLevel", func(t *testing.T) {
		calls := 0
		var from, to DegradationLevel
		ad := NewAdaptiveDegradationWithRegistry(DegradationConfig{
			OnLevelChange: func(f, t DegradationLevel) {
				calls++
				from, to = f, t
			},
		}, testReg())
		ad.ForceLevel(LevelLight)
		ad.ForceLevel(LevelSevere)
		assert.Equal(t, 2, calls)
		assert.Equal(t, LevelLight, from)
		assert.Equal(t, LevelSevere, to)
	})

	t.Run("callback panic is recovered", func(t *testing.T) {
		ad := NewAdaptiveDegradationWithRegistry(DegradationConfig{
			OnLevelChange: func(from, to DegradationLevel) {
				panic("test panic from callback")
			},
		}, testReg())
		assert.NotPanics(t, func() {
			ad.ForceLevel(LevelSevere)
		})
	})
}

// ── G. defaultPriorityClassifier ───────────────────────────────────────────

func TestDefaultPriorityClassifier(t *testing.T) {
	t.Run("unknown event type returns PriorityLow", func(t *testing.T) {
		ctx := createTestContext()
		assert.Equal(t, PriorityLow, defaultPriorityClassifier(ctx))
	})

	t.Run("MESSAGE_CREATE returns PriorityNormal", func(t *testing.T) {
		evt := &middlewareTestEvent{id: "1", kind: platform.EventKind("MESSAGE_CREATE")}
		ctx := eventctx.AcquireContextFromEvent(evt, nil)
		assert.Equal(t, PriorityNormal, defaultPriorityClassifier(ctx))
	})

	t.Run("GROUP_AT_MESSAGE_CREATE returns PriorityHigh", func(t *testing.T) {
		evt := &middlewareTestEvent{id: "1", kind: platform.EventKind("GROUP_AT_MESSAGE_CREATE")}
		ctx := eventctx.AcquireContextFromEvent(evt, nil)
		assert.Equal(t, PriorityHigh, defaultPriorityClassifier(ctx))
	})

	t.Run("C2C_MESSAGE_CREATE returns PriorityHigh", func(t *testing.T) {
		evt := &middlewareTestEvent{id: "1", kind: platform.EventKind("C2C_MESSAGE_CREATE")}
		ctx := eventctx.AcquireContextFromEvent(evt, nil)
		assert.Equal(t, PriorityHigh, defaultPriorityClassifier(ctx))
	})

	t.Run("GUILD_CREATE returns PriorityCritical", func(t *testing.T) {
		evt := &middlewareTestEvent{id: "1", kind: platform.EventKind("GUILD_CREATE")}
		ctx := eventctx.AcquireContextFromEvent(evt, nil)
		assert.Equal(t, PriorityCritical, defaultPriorityClassifier(ctx))
	})

	t.Run("GUILD_DELETE returns PriorityCritical", func(t *testing.T) {
		evt := &middlewareTestEvent{id: "1", kind: platform.EventKind("GUILD_DELETE")}
		ctx := eventctx.AcquireContextFromEvent(evt, nil)
		assert.Equal(t, PriorityCritical, defaultPriorityClassifier(ctx))
	})

	t.Run("GROUP_ADD_ROBOT returns PriorityCritical", func(t *testing.T) {
		evt := &middlewareTestEvent{id: "1", kind: platform.EventKind("GROUP_ADD_ROBOT")}
		ctx := eventctx.AcquireContextFromEvent(evt, nil)
		assert.Equal(t, PriorityCritical, defaultPriorityClassifier(ctx))
	})

	t.Run("GROUP_DEL_ROBOT returns PriorityCritical", func(t *testing.T) {
		evt := &middlewareTestEvent{id: "1", kind: platform.EventKind("GROUP_DEL_ROBOT")}
		ctx := eventctx.AcquireContextFromEvent(evt, nil)
		assert.Equal(t, PriorityCritical, defaultPriorityClassifier(ctx))
	})

	t.Run("INTERACTION_CREATE returns PriorityHigh", func(t *testing.T) {
		evt := &middlewareTestEvent{id: "1", kind: platform.EventKind("INTERACTION_CREATE")}
		ctx := eventctx.AcquireContextFromEvent(evt, nil)
		assert.Equal(t, PriorityHigh, defaultPriorityClassifier(ctx))
	})
}

// ── H. SetDegraded / IsDegraded ────────────────────────────────────────────

func TestSetDegraded_IsDegraded(t *testing.T) {
	t.Run("SetDegraded nil is no-op", func(t *testing.T) {
		assert.NotPanics(t, func() {
			SetDegraded(nil)
		})
	})

	t.Run("IsDegraded nil returns false", func(t *testing.T) {
		assert.False(t, IsDegraded(nil))
	})

	t.Run("SetDegraded then IsDegraded returns true", func(t *testing.T) {
		ctx := createTestContext()
		SetDegraded(ctx)
		assert.True(t, IsDegraded(ctx))
	})

	t.Run("IsDegraded on clean context returns false", func(t *testing.T) {
		ctx := createTestContext()
		assert.False(t, IsDegraded(ctx))
	})

	t.Run("SetDegraded sets both ext and user-state key", func(t *testing.T) {
		ctx := createTestContext()
		SetDegraded(ctx)
		_, extOK := eventctx.ExtGet[DegradedExt](ctx.Ext())
		assert.True(t, extOK, "DegradedExt should be set in typed extensions")
		v, stateOK := ctx.Get(CtxKeyDegraded)
		assert.True(t, stateOK, "CtxKeyDegraded should be set in user-state")
		assert.Equal(t, true, v)
	})

	t.Run("IsDegraded falls back to user-state key", func(t *testing.T) {
		ctx := createTestContext()
		ctx.Set(CtxKeyDegraded, true)
		assert.True(t, IsDegraded(ctx), "should detect degraded via user-state key fallback")
	})

	t.Run("independent contexts do not interfere", func(t *testing.T) {
		ctx1 := createTestContext()
		ctx2 := createTestContext()
		SetDegraded(ctx1)
		assert.True(t, IsDegraded(ctx1))
		assert.False(t, IsDegraded(ctx2))
	})
}

// ── I. Context keys ────────────────────────────────────────────────────────

func TestCtxKeyConstants(t *testing.T) {
	assert.Equal(t, "request_id", CtxKeyRequestID)
	assert.Equal(t, "degraded", CtxKeyDegraded)
	assert.Equal(t, "user_id", CtxKeyUserID)
}

// ── J. Middleware with custom PriorityClassifier ────────────────────────────

func TestAdaptiveDegradation_CustomPriorityClassifier(t *testing.T) {
	t.Run("custom classifier overrides default", func(t *testing.T) {
		ad := NewAdaptiveDegradationWithRegistry(DegradationConfig{
			Strategy: DegradationDrop,
			PriorityClassifier: func(ctx *eventctx.Context) EventPriority {
				return PriorityCritical
			},
		}, testReg())
		ad.ForceLevel(LevelSevere)
		mw := ad.Middleware()
		called := 0
		h := mw(func(ctx *eventctx.Context) error {
			called++
			return nil
		})
		err := h(createTestContext())
		assert.NoError(t, err)
		assert.Equal(t, 1, called, "custom classifier returning PriorityCritical should pass at Severe")
	})
}

// ── K. Edge cases ──────────────────────────────────────────────────────────

func TestAdaptiveDegradation_EdgeCases(t *testing.T) {
	t.Run("nil registry does not panic", func(t *testing.T) {
		assert.NotPanics(t, func() {
			ad := NewAdaptiveDegradationWithRegistry(DegradationConfig{}, nil)
			require.NotNil(t, ad)
		})
	})

	t.Run("Middleware with nil config classifier uses default", func(t *testing.T) {
		ad := NewAdaptiveDegradationWithRegistry(DegradationConfig{}, testReg())
		// defaultPriorityClassifier should not panic on a valid context
		mw := ad.Middleware()
		h := mw(mockHandler(nil, 0))
		err := h(createTestContext())
		assert.NoError(t, err)
	})

	t.Run("ForceLevel from Normal to Normal triggers callback correctly", func(t *testing.T) {
		called := false
		ad := NewAdaptiveDegradationWithRegistry(DegradationConfig{
			OnLevelChange: func(from, to DegradationLevel) {
				called = true
			},
		}, testReg())
		ad.ForceLevel(LevelNormal)
		assert.True(t, called)
	})

	t.Run("DegradationDrop unknown strategy treated as drop", func(t *testing.T) {
		ad := NewAdaptiveDegradationWithRegistry(DegradationConfig{
			Strategy: DegradationStrategy(99),
		}, testReg())
		ad.ForceLevel(LevelNormal) // at normal, should pass through regardless
		mw := ad.Middleware()
		called := 0
		h := mw(func(ctx *eventctx.Context) error {
			called++
			return nil
		})
		h(createTestContext())
		assert.Equal(t, 1, called)
	})
}

// ── L. Prometheus metrics isolation ─────────────────────────────────────────

func TestAdaptiveDegradation_MetricsIsolation(t *testing.T) {
	t.Run("separate registries do not conflict", func(t *testing.T) {
		reg1 := prometheus.NewRegistry()
		reg2 := prometheus.NewRegistry()
		ad1 := NewAdaptiveDegradationWithRegistry(DegradationConfig{}, reg1)
		ad2 := NewAdaptiveDegradationWithRegistry(DegradationConfig{}, reg2)
		assert.NotNil(t, ad1)
		assert.NotNil(t, ad2)
	})
}
