package engine

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	corectx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractCommandBehavior(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"/ping", "/ping"},
		{"/ping arg1 arg2", "/ping"},
		{"  /ping  ", "/ping"},
		{"  /ping   arg  ", "/ping"},
		{"nocommand", "nocommand"},
		{"", ""},
		{"   ", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, extractCommand(tt.input))
		})
	}
}

func TestMergeKSortedMatchers_Empty(t *testing.T) {
	result := mergeKSortedMatchers(nil, [][]*Matcher{})
	assert.Nil(t, result)

	result = mergeKSortedMatchers(nil, [][]*Matcher{{}, {}, {}})
	assert.Nil(t, result)

	// With dst, returns dst[:0] when total is 0
	dst := make([]*Matcher, 0, 5)
	result = mergeKSortedMatchers(dst, [][]*Matcher{{}, {}})
	assert.NotNil(t, result)
	assert.Empty(t, result)
}

func TestMergeKSortedMatchers_SingleList(t *testing.T) {
	m1 := makeTestMatcher("a", 10)
	m2 := makeTestMatcher("b", 20)
	result := mergeKSortedMatchers(nil, [][]*Matcher{{m1, m2}})
	require.Equal(t, 2, len(result))
	assert.Same(t, m1, result[0])
	assert.Same(t, m2, result[1])
}

func TestMergeKSortedMatchers_MultiList(t *testing.T) {
	m1 := makeTestMatcher("a", 50)
	m2 := makeTestMatcher("b", 10)
	m3 := makeTestMatcher("c", 30)
	m4 := makeTestMatcher("d", 20)

	result := mergeKSortedMatchers(nil, [][]*Matcher{
		{m1},
		{m2, m4},
		{m3},
	})
	require.Equal(t, 4, len(result))
	assert.Same(t, m2, result[0])
	assert.Same(t, m4, result[1])
	assert.Same(t, m3, result[2])
	assert.Same(t, m1, result[3])
}

func TestMergeKSortedMatchers_DstReuse(t *testing.T) {
	m1 := makeTestMatcher("a", 10)
	m2 := makeTestMatcher("b", 20)
	dst := make([]*Matcher, 0, 10)
	result := mergeKSortedMatchers(dst, [][]*Matcher{{m1, m2}})
	assert.Len(t, result, 2)
	assert.Same(t, m1, result[0])
	assert.Same(t, m2, result[1])
}

func TestMergeKSortedMatchersSix(t *testing.T) {
	m1 := makeTestMatcher("a", 50)
	m2 := makeTestMatcher("b", 10)
	m3 := makeTestMatcher("c", 30)
	m4 := makeTestMatcher("d", 20)
	m5 := makeTestMatcher("e", 5)
	m6 := makeTestMatcher("f", 40)

	result := mergeSortedMatchersSix(nil, []*Matcher{m1}, []*Matcher{m2}, []*Matcher{m3}, []*Matcher{m4}, []*Matcher{m5}, []*Matcher{m6})
	require.Equal(t, 6, len(result))
	assert.Same(t, m5, result[0])
	assert.Same(t, m2, result[1])
	assert.Same(t, m4, result[2])
	assert.Same(t, m3, result[3])
	assert.Same(t, m6, result[4])
	assert.Same(t, m1, result[5])
}

func TestMergeKSortedMatchers_PriorityBoundary(t *testing.T) {
	matchers := make([]*Matcher, 5)
	for i := range matchers {
		matchers[i] = makeTestMatcher("test", uint64(i*10))
	}
	result := mergeKSortedMatchers(nil, [][]*Matcher{
		{matchers[0], matchers[4]},
		{matchers[1]},
		{matchers[2], matchers[3]},
	})
	require.Equal(t, 5, len(result))
	for i := 1; i < len(result); i++ {
		assert.LessOrEqual(t, result[i-1].getPriority(), result[i].getPriority())
	}
}

func TestInvokeHandler_DeletedSkipped(t *testing.T) {
	e := newEngineForTest(t)
	m := &Matcher{Source: "test"}
	m.rt.deleted.Store(true)
	ctx := testContext()
	defer releaseCtx(ctx)
	e.invokeHandler(ctx, m)
}

func TestInvokeHandler_CallsHandler(t *testing.T) {
	e := newEngineForTest(t)
	called := false
	m := &Matcher{Source: "test"}
	m.Handle(func(ctx *corectx.Context) error {
		called = true
		return nil
	})
	ctx := testContext()
	defer releaseCtx(ctx)
	e.invokeHandler(ctx, m)
	assert.True(t, called)
}

func TestInvokeHandler_ErrorRecorded(t *testing.T) {
	e := newEngineForTest(t)
	m := &Matcher{Source: "test"}
	m.Handle(func(ctx *corectx.Context) error {
		return assert.AnError
	})
	ctx := testContext()
	defer releaseCtx(ctx)
	e.invokeHandler(ctx, m)
}

func TestInvokeHandler_NilHandlerReturnsEarly(t *testing.T) {
	e := newEngineForTest(t)
	m := &Matcher{Source: "test"}
	ctx := testContext()
	defer releaseCtx(ctx)
	e.invokeHandler(ctx, m)
}

func TestInvokeHandler_MiddlewareChainOrder(t *testing.T) {
	e := newEngineForTest(t)
	order := make([]string, 0)
	m := &Matcher{Source: "test"}
	mw1 := func(next corectx.Handler) corectx.Handler {
		return func(ctx *corectx.Context) error {
			order = append(order, "mw1_before")
			err := next(ctx)
			order = append(order, "mw1_after")
			return err
		}
	}
	mw2 := func(next corectx.Handler) corectx.Handler {
		return func(ctx *corectx.Context) error {
			order = append(order, "mw2_before")
			err := next(ctx)
			order = append(order, "mw2_after")
			return err
		}
	}
	m.Use(mw1, mw2)
	m.Handle(func(ctx *corectx.Context) error {
		order = append(order, "handler")
		return nil
	})
	ctx := testContext()
	defer releaseCtx(ctx)
	e.invokeHandler(ctx, m)
	assert.Equal(t, []string{"mw1_before", "mw2_before", "handler", "mw2_after", "mw1_after"}, order)
}

func TestInvokeHandler_TempMatcherAutoDeletes(t *testing.T) {
	e := newEngineForTest(t)
	m := &Matcher{Source: "test"}
	m.Handle(func(ctx *corectx.Context) error { return nil })
	m.SetTempWithMaxUse(1)
	ctx := testContext()
	defer releaseCtx(ctx)
	e.invokeHandler(ctx, m)
	assert.True(t, m.rt.deleted.Load())
}

func TestInvokeHandler_TempMatcherUseLimit(t *testing.T) {
	e := newEngineForTest(t)
	m := &Matcher{Source: "test"}
	m.Handle(func(ctx *corectx.Context) error { return nil })
	m.SetTempWithMaxUse(3)
	for i := range 2 {
		ctx := testContext()
		e.invokeHandler(ctx, m)
		releaseCtx(ctx)
		assert.False(t, m.rt.deleted.Load(), "not deleted at use %d", i+1)
	}
	ctx := testContext()
	defer releaseCtx(ctx)
	e.invokeHandler(ctx, m)
	assert.True(t, m.rt.deleted.Load())
}

func TestInvokeHandler_PanicRecovered(t *testing.T) {
	e := newEngineForTest(t)
	m := &Matcher{Source: "test"}
	m.Handle(func(ctx *corectx.Context) error {
		panic("test panic")
	})
	ctx := testContext()
	defer releaseCtx(ctx)
	assert.NotPanics(t, func() { e.invokeHandler(ctx, m) })
}

func TestInvokeHandler_FastPath(t *testing.T) {
	e := newEngineForTest(t)
	var count atomic.Int32
	m := &Matcher{Source: "test"}
	m.Handle(func(ctx *corectx.Context) error {
		count.Add(1)
		return nil
	})
	ctx := testContext()
	defer releaseCtx(ctx)
	e.invokeHandler(ctx, m)
	assert.Equal(t, int32(1), count.Load())
	e.invokeHandler(ctx, m)
	assert.Equal(t, int32(2), count.Load())
}

func TestGetOrBuildIterChain_NoChain(t *testing.T) {
	e := &Engine{}
	called := false
	h := func(ctx *corectx.Context) error { called = true; return nil }
	result := e.getOrBuildIterChain(&Matcher{}, nil, h)
	ctx := testContext()
	defer releaseCtx(ctx)
	result(ctx)
	assert.True(t, called)
}

func TestGetOrBuildIterChain_WithMiddleware(t *testing.T) {
	e := &Engine{}
	order := make([]string, 0)
	mw := func(next corectx.Handler) corectx.Handler {
		return func(ctx *corectx.Context) error {
			order = append(order, "mw")
			return next(ctx)
		}
	}
	h := func(ctx *corectx.Context) error { order = append(order, "handler"); return nil }
	matcher := &Matcher{}
	result := e.getOrBuildIterChain(matcher, []corectx.Middleware{mw}, h)
	ctx := testContext()
	defer releaseCtx(ctx)
	result(ctx)
	assert.Equal(t, []string{"mw", "handler"}, order)
}

func TestGetOrBuildIterChain_CacheReuse(t *testing.T) {
	e := &Engine{}
	matcher := &Matcher{}
	count := 0
	h := func(ctx *corectx.Context) error { count++; return nil }
	mw := func(next corectx.Handler) corectx.Handler { return next }
	result1 := e.getOrBuildIterChain(matcher, []corectx.Middleware{mw}, h)
	result2 := e.getOrBuildIterChain(matcher, []corectx.Middleware{mw}, h)
	assert.NotNil(t, result1)
	assert.NotNil(t, result2)

	// Both result1 and result2 should invoke the same underlying handler
	ctx := testContext()
	defer releaseCtx(ctx)
	result1(ctx)
	assert.Equal(t, 1, count)
	result2(ctx)
	assert.Equal(t, 2, count)
}

func TestProcessPendingDeletes(t *testing.T) {
	e := newEngineForTest(t)
	m := &Matcher{Source: "test"}
	m.rt.deleted.Store(true) // invokeHandler sets this before sending to channel
	e.services.pendingDeleteCh <- m
	e.processPendingDeletes()
	// After processing, the matcher should be removed from the engine state
	assert.Equal(t, 0, len(e.state.Load().matchers))
}

func TestProcessEvent_EventWg(t *testing.T) {
	e := newEngineForTest(t)
	handled := false
	e.OnEventKind(platform.EventKindPrivateMessage).Handle(func(ctx *corectx.Context) error {
		handled = true
		return nil
	})
	evt := newTestPlatformEvent(platform.EventKindPrivateMessage)
	ctx := corectx.AcquireContextFromEvent(evt, nil)
	e.ProcessEvent(ctx)
	corectx.ReleaseContextFromEvent(ctx)
	assert.True(t, handled)
}

func TestProcessEvent_ShutdownWaits(t *testing.T) {
	e := newEngineForTest(t)
	var count atomic.Int32
	e.OnAny().Handle(func(ctx *corectx.Context) error {
		count.Add(1)
		return nil
	})
	const n = 10
	for range n {
		ctx := corectx.AcquireContextFromEvent(newTestPlatformEvent(platform.EventKindPrivateMessage), nil)
		go e.ProcessEvent(ctx)
	}
	time.Sleep(50 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	assert.NoError(t, e.Shutdown(ctx))
	assert.Equal(t, int32(n), count.Load())
}
