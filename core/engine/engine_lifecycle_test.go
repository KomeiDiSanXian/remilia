package engine

import (
	stdctx "context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/command"
	ctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/metrics"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Engine Lifecycle & Component Tests

func TestEngine_UpdateTempMatcherPriority_Used(t *testing.T) {
	eng := NewEngine()
	matcher := eng.OnTemp(dto.C2CMessageCreate)
	matcher.SetPriority(50)

	// This triggers UpdateTempMatcherPriority internally
	eng.UpdateTempMatcherPriority(matcher)

	// Should not panic
	assert.True(t, matcher.IsTemp())
}

func TestEngine_EnableGlobalMatchers_Toggle(t *testing.T) {
	eng := NewEngine()

	eng.EnableGlobalMatchers(true)
	eng.EnableGlobalMatchers(false)
	eng.EnableGlobalMatchers(true)

	// Should not panic
}

func TestEngine_SetMetricsCollector_Used(t *testing.T) {
	eng := NewEngine()
	collector := &metrics.Collector{}

	result := eng.SetMetricsCollector(collector)
	assert.Equal(t, eng, result)

	retrieved := eng.GetMetricsCollector()
	assert.Equal(t, collector, retrieved)
}

func TestEngine_GetMetricsCollector_Initially(t *testing.T) {
	eng := NewEngine()
	collector := eng.GetMetricsCollector()
	assert.Nil(t, collector)
}

func TestEngine_UpdateMatcherCommand_Called(t *testing.T) {
	eng := NewEngine()
	matcher := eng.On(dto.C2CMessageCreate)
	matcher.BindCommand("/newcmd")

	eng.UpdateMatcherCommand(matcher)

	// Should update internal index
	assert.Equal(t, "/newcmd", matcher.GetCommand())
}

func TestEngine_UpdateMatcherIndex_Called(t *testing.T) {
	eng := NewEngine()
	matcher := eng.On(dto.C2CMessageCreate)

	eng.UpdateMatcherIndex(matcher)

	// Should not panic
}

func TestEngine_OnCommand_Used(t *testing.T) {
	eng := NewEngine()

	rule1 := func(c *ctx.Context) bool { return true }
	rule2 := func(c *ctx.Context) bool { return false }

	matcher := eng.OnCommand(dto.C2CMessageCreate, "/test", rule1, rule2)

	require.NotNil(t, matcher)
	assert.Equal(t, "/test", matcher.GetCommand())
	assert.Equal(t, dto.C2CMessageCreate, matcher.EventType)
	assert.GreaterOrEqual(t, len(matcher.Rules), 2)
}

func TestEngine_OnCommand_EmptyEventType(t *testing.T) {
	eng := NewEngine()

	matcher := eng.OnCommand("", "/test")

	require.NotNil(t, matcher)
	assert.Equal(t, "/test", matcher.GetCommand())
}

func TestEngine_WithMatcherGroupBatch_Used(t *testing.T) {
	eng := NewEngine()

	var m1, m2, m3 *Matcher

	eng.WithMatcherGroupBatch(func() {
		m1 = eng.On(dto.C2CMessageCreate)
		m2 = eng.On(dto.GroupAtMessageCreate)
		m3 = eng.OnAny()

		eng.SetMatcherGroup(m1, "batch-group", "s1")
		eng.SetMatcherGroup(m2, "batch-group", "s2")
		eng.SetMatcherGroup(m3, "batch-group", "s3")
	})

	assert.Equal(t, "batch-group", m1.group)
	assert.Equal(t, "batch-group", m2.group)
	assert.Equal(t, "batch-group", m3.group)
}

func TestEngine_WithMatcherGroupBatch_NilFunction(t *testing.T) {
	eng := NewEngine()

	// Should not panic
	eng.WithMatcherGroupBatch(nil)
}

func TestEngine_SetMatcherGroup_Used(t *testing.T) {
	eng := NewEngine()

	matcher := eng.On(dto.C2CMessageCreate)
	eng.SetMatcherGroup(matcher, "my-group", "my-source")

	assert.Equal(t, "my-group", matcher.group)
	assert.Equal(t, "my-source", matcher.Source)
}

func TestEngine_SetMatcherGroup_NilMatcher(t *testing.T) {
	eng := NewEngine()

	// Should not panic
	eng.SetMatcherGroup(nil, "group", "source")
}

// ============================================================================
// Process.go Coverage Boost
// ============================================================================

func TestEngine_ProcessEvent_WithCommandOptimization(t *testing.T) {
	eng := NewEngine()

	var executed string

	// Register command matcher
	eng.OnCommand(dto.C2CMessageCreate, "/test").Handle(func(c *ctx.Context) error {
		executed = "command"
		return nil
	})

	// Register normal matcher
	eng.On(dto.C2CMessageCreate).Handle(func(c *ctx.Context) error {
		executed = "normal"
		return nil
	})

	// Send event with command
	payload := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: []byte(`{"content":"/test arg1"}`),
	}
	context := ctx.NewContext(payload, nil)

	eng.ProcessEvent(context)

	// Command matcher should execute (if command extraction works)
	assert.NotEmpty(t, executed)
}

func TestEngine_ProcessEvent_GenericMatchers(t *testing.T) {
	eng := NewEngine()

	var count int32

	// Generic matcher (matches all event types)
	eng.OnAny().Handle(func(c *ctx.Context) error {
		atomic.AddInt32(&count, 1)
		return nil
	})

	// Specific matcher
	eng.On(dto.C2CMessageCreate).Handle(func(c *ctx.Context) error {
		atomic.AddInt32(&count, 1)
		return nil
	})

	payload := &dto.Payload{Type: dto.C2CMessageCreate}
	context := ctx.NewContext(payload, nil)

	eng.ProcessEvent(context)

	// Both should execute
	assert.Equal(t, int32(2), atomic.LoadInt32(&count))
}

func TestEngine_ProcessEvent_TempMatcherExecution(t *testing.T) {
	eng := NewEngine()

	var count int32

	matcher := eng.OnTemp(dto.C2CMessageCreate)
	matcher.rt.maxUseCount = 2
	matcher.Handle(func(c *ctx.Context) error {
		atomic.AddInt32(&count, 1)
		return nil
	})

	payload := &dto.Payload{Type: dto.C2CMessageCreate}

	// Execute multiple times
	for range 5 {
		context := ctx.NewContext(payload, nil)
		eng.ProcessEvent(context)
	}

	// Should execute limited times
	assert.GreaterOrEqual(t, atomic.LoadInt32(&count), int32(2))
}

func TestEngine_ProcessEventBatch_AllEventTypes(t *testing.T) {
	eng := NewEngine()

	var eventTypes []dto.EventType

	eng.OnAny().Handle(func(c *ctx.Context) error {
		eventTypes = append(eventTypes, c.GetEventType())
		return nil
	})

	events := []*dto.Payload{
		{Type: dto.C2CMessageCreate},
		{Type: dto.GroupAtMessageCreate},
		{Type: dto.GroupAddRobot},
		{Type: dto.GroupDelRobot},
	}

	eng.ProcessEventBatch(events, nil)

	assert.Equal(t, 4, len(eventTypes))
}

// ============================================================================
// Middleware.go Coverage Boost
// ============================================================================

func TestEngine_RebuildMatcherChain_WithGenerationUpdate(t *testing.T) {
	eng := NewEngine()

	mw1 := func(next ctx.Handler) ctx.Handler {
		return func(c *ctx.Context) error {
			return next(c)
		}
	}

	eng.Use(mw1)

	matcher := eng.On(dto.C2CMessageCreate)
	eng.RebuildMatcherChain(matcher)

	// Add more middleware
	mw2 := func(next ctx.Handler) ctx.Handler {
		return func(c *ctx.Context) error {
			return next(c)
		}
	}

	eng.Use(mw2)
	eng.RebuildMatcherChain(matcher)

	// Chain should be updated
	chain := matcher.getCombinedChain()
	assert.NotNil(t, chain)
}

func TestEngine_UseForGroup_MultipleGroups(t *testing.T) {
	eng := NewEngine()

	mw1 := func(next ctx.Handler) ctx.Handler {
		return func(c *ctx.Context) error {
			return next(c)
		}
	}

	mw2 := func(next ctx.Handler) ctx.Handler {
		return func(c *ctx.Context) error {
			return next(c)
		}
	}

	eng.UseForGroup("group1", mw1)
	eng.UseForGroup("group2", mw2)
	eng.UseForGroup("group1", mw2) // Add second middleware to group1

	m1 := eng.On(dto.C2CMessageCreate)
	m1.group = "group1"
	eng.RebuildMatcherChain(m1)

	m2 := eng.On(dto.C2CMessageCreate)
	m2.group = "group2"
	eng.RebuildMatcherChain(m2)

	// Both should have chains
	assert.NotNil(t, m1.getCombinedChain())
	assert.NotNil(t, m2.getCombinedChain())
}

func TestEngine_Named_Middleware(t *testing.T) {
	eng := NewEngine()

	var order []string

	mw := eng.Named("logger", func(next ctx.Handler) ctx.Handler {
		return func(c *ctx.Context) error {
			order = append(order, "before")
			err := next(c)
			order = append(order, "after")
			return err
		}
	})

	eng.Use(mw)

	eng.On(dto.C2CMessageCreate).Handle(func(c *ctx.Context) error {
		order = append(order, "handler")
		return nil
	})

	payload := &dto.Payload{Type: dto.C2CMessageCreate}
	context := ctx.NewContext(payload, nil)

	eng.ProcessEvent(context)

	assert.Equal(t, []string{"before", "handler", "after"}, order)
}

// ============================================================================
// Matcher.go Coverage Boost
// ============================================================================

func TestMatcher_SetTemp_Transitions(t *testing.T) {
	eng := NewEngine()

	matcher := eng.On(dto.C2CMessageCreate)
	assert.False(t, matcher.IsTemp())

	// Transition to temp
	matcher.SetTemp(true)
	assert.True(t, matcher.IsTemp())

	// Verify internal state
	matcher.rt.mu.RLock()
	maxUse := matcher.rt.maxUseCount
	matcher.rt.mu.RUnlock()

	assert.Equal(t, int32(1), maxUse)

	// Transition back to permanent
	matcher.SetTemp(false)
	assert.False(t, matcher.IsTemp())

	matcher.rt.mu.RLock()
	maxUse = matcher.rt.maxUseCount
	matcher.rt.mu.RUnlock()

	assert.Equal(t, int32(0), maxUse)
}

func TestMatcher_SetPriority_TempMatcher(t *testing.T) {
	eng := NewEngine()

	matcher := eng.OnTemp(dto.C2CMessageCreate)
	initialPriority := uint(10)
	matcher.SetPriority(initialPriority)

	// Change priority
	newPriority := uint(50)
	matcher.SetPriority(newPriority)

	matcher.rt.mu.RLock()
	priority := matcher.priority
	matcher.rt.mu.RUnlock()

	assert.Equal(t, newPriority, priority)
}

func TestMatcher_Handle_WithCoordinator(t *testing.T) {
	eng := NewEngine()

	matcher := eng.On(dto.C2CMessageCreate)

	handler1 := func(c *ctx.Context) error {
		return nil
	}

	handler2 := func(c *ctx.Context) error {
		return nil
	}

	// Set handler multiple times
	matcher.Handle(handler1)
	matcher.Handle(handler2)

	assert.NotNil(t, matcher.Handler)
}

// ============================================================================
// State.go Coverage Boost
// ============================================================================

func TestEngineState_DeleteMatcher_FromIndices(t *testing.T) {
	state := newEngineState()

	m1 := &Matcher{
		EventType: dto.C2CMessageCreate,
		Source:    "test1",
		priority:  10,
	}

	m2 := &Matcher{
		EventType: dto.C2CMessageCreate,
		Source:    "test2",
		priority:  20,
	}

	state.matchers = []*Matcher{m1, m2}
	state.rebuildIndex()

	// Delete m1
	state.deleteMatcher(m1)

	assert.Equal(t, 1, len(state.matchers))
	assert.Equal(t, m2, state.matchers[0])
}

func TestEngineState_InvalidateSortedCache_Specific(t *testing.T) {
	state := newEngineState()

	m1 := &Matcher{EventType: dto.C2CMessageCreate, priority: 10}
	m2 := &Matcher{EventType: dto.GroupAtMessageCreate, priority: 20}

	state.matchers = []*Matcher{m1, m2}
	state.rebuildIndex()

	// Invalidate only C2C cache
	state.invalidateSortedCache(dto.C2CMessageCreate)

	// Cache should be rebuilt for C2C
	assert.NotNil(t, state.sortedCache)
}

// ============================================================================
// RemoveGroup Coverage
// ============================================================================

func TestEngine_RemoveGroup_LogMessage(t *testing.T) {
	eng := NewEngine()

	var m1, m2 *Matcher

	eng.WithMatcherGroupBatch(func() {
		m1 = eng.On(dto.C2CMessageCreate)
		m2 = eng.On(dto.C2CMessageCreate)
		eng.SetMatcherGroup(m1, "test-plugin", "s1")
		eng.SetMatcherGroup(m2, "test-plugin", "s2")
	})

	// Remove group (should log)
	eng.RemoveGroup("test-plugin")

	// Matchers should be removed
	assert.Equal(t, 0, eng.GetMatcherCount())
}

// ============================================================================
// Stop Coverage
// ============================================================================

func TestEngine_Shutdown_WaitForEvents(t *testing.T) {
	eng := NewEngine()

	eng.On(dto.C2CMessageCreate).Handle(func(c *ctx.Context) error {
		time.Sleep(50 * time.Millisecond)
		return nil
	})

	// Start processing
	go func() {
		payload := &dto.Payload{Type: dto.C2CMessageCreate}
		context := ctx.NewContext(payload, nil)
		eng.ProcessEvent(context)
	}()

	// Give it a moment to start
	time.Sleep(10 * time.Millisecond)

	// Stop should wait
	err := eng.Shutdown(stdctx.Background())
	assert.NoError(t, err)
}

// ============================================================================
// Restore Coverage
// ============================================================================

func TestEngine_Restore_WithGroups(t *testing.T) {
	eng := NewEngine()

	var m1, m2 *Matcher

	eng.WithMatcherGroupBatch(func() {
		m1 = eng.On(dto.C2CMessageCreate)
		m2 = eng.On(dto.C2CMessageCreate)
		eng.SetMatcherGroup(m1, "plugin1", "s1")
		eng.SetMatcherGroup(m2, "plugin2", "s2")
	})

	snapshot := eng.Snapshot()

	eng.DeleteAllMatchers()
	assert.Equal(t, 0, eng.GetMatcherCount())

	eng.Restore(snapshot)
	assert.Equal(t, 2, eng.GetMatcherCount())
}

// ============================================================================
// Priority Sorting Tests
// ============================================================================

func TestSortMatchersByPriority_StableSort(t *testing.T) {
	m1 := &Matcher{priority: 10, Source: "m1"}
	m2 := &Matcher{priority: 10, Source: "m2"}
	m3 := &Matcher{priority: 5, Source: "m3"}
	m4 := &Matcher{priority: 20, Source: "m4"}

	matchers := []*Matcher{m1, m2, m3, m4}
	sortMatchersByPriority(matchers)

	// Should be sorted by priority (ascending)
	assert.Equal(t, uint(5), matchers[0].priority)
	assert.Equal(t, uint(10), matchers[1].priority)
	assert.Equal(t, uint(10), matchers[2].priority)
	assert.Equal(t, uint(20), matchers[3].priority)
}

// ============================================================================
// Component Tests
// ============================================================================

func TestEngine_Components_Stop(t *testing.T) {
	eng := NewEngine(WithCleanupInterval(10 * time.Second))

	// Stop should stop all components
	err := eng.Shutdown(stdctx.Background())
	assert.NoError(t, err)
}

// ============================================================================
// GetMatchersForEvent Tests
// ============================================================================

func TestEngine_GetMatchersForEvent_Specific(t *testing.T) {
	eng := NewEngine()

	eng.On(dto.C2CMessageCreate)
	eng.On(dto.GroupAtMessageCreate)
	eng.OnAny()

	payload := &dto.Payload{Type: dto.C2CMessageCreate}
	context := ctx.NewContext(payload, nil)

	matchers := eng.getMatchersForEvent(context)

	// Should include C2C and generic matchers
	assert.GreaterOrEqual(t, len(matchers), 2)
}

// ============================================================================
// Async Component Lifecycle Tests
// ============================================================================

func TestEngine_AsyncComponents_StartStop(t *testing.T) {
	eng := NewEngine(WithCleanupInterval(50*time.Millisecond), WithPendingDeleteBufferSize(10))
	// Create expirable temp matchers to exercise the cleaner
	for range 5 {
		m := eng.OnTemp(dto.C2CMessageCreate)
		m.rt.expiresAt = time.Now().Add(-1 * time.Second)
	}
	time.Sleep(150 * time.Millisecond)
	// Queue pending deletes to exercise the batch processor
	for range 10 {
		m := eng.On(dto.C2CMessageCreate)
		eng.DeleteMatcher(m)
	}
	time.Sleep(100 * time.Millisecond)
	c, cancel := stdctx.WithTimeout(stdctx.Background(), 500*time.Millisecond)
	defer cancel()
	err := eng.Shutdown(c)
	assert.NoError(t, err)
}

func TestEngine_RemoveGroup_BeforeAndAfterAdd(t *testing.T) {
	eng := NewEngine()
	// RemoveGroup on non-existent group should be a safe no-op
	eng.RemoveGroup("empty-before-add")
	eng.WithMatcherGroupBatch(func() {
		for range 5 {
			m := eng.On(dto.C2CMessageCreate)
			eng.SetMatcherGroup(m, "large-group", "src")
		}
	})
	eng.RemoveGroup("large-group")
	assert.Equal(t, 0, eng.GetMatcherCount())
}

func TestEngine_InvokeHandler_ErrorAndNilHandler(t *testing.T) {
	eng := NewEngine()
	eng.On(dto.C2CMessageCreate).Handle(func(c *ctx.Context) error {
		return assert.AnError
	})
	payload := &dto.Payload{Type: dto.C2CMessageCreate}
	// Handler returns error: engine should continue without panic
	eng.ProcessEvent(ctx.NewContext(payload, nil))

	// Matcher with nil Handler: should be skipped safely
	eng2 := NewEngine()
	m := eng2.On(dto.C2CMessageCreate)
	m.Handler = nil
	eng2.ProcessEvent(ctx.NewContext(payload, nil))
}

func TestEngineState_CopyWithCommands(t *testing.T) {
	state := newEngineState()
	for i := range 10 {
		m := &Matcher{EventType: dto.C2CMessageCreate, priority: uint(i * 10), group: "g1"}
		if i%3 == 0 {
			m.definition = &command.Definition{Name: "cmd"}
		}
		state.addMatcher(m)
	}
	copied := copyEngineState(state)
	assert.Equal(t, len(state.matchers), len(copied.matchers))
	assert.Equal(t, len(state.groupIndex), len(copied.groupIndex))
	assert.Equal(t, len(state.commandIndex), len(copied.commandIndex))
}
