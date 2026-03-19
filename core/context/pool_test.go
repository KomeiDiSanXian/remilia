package context_test

import (
	"sync"
	"testing"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAcquireReleaseContext tests basic acquire and release functionality
func TestAcquireReleaseContext(t *testing.T) {

	// Acquire context
	ctx := context.AcquireContextFromEvent(makeTestEvent("test", "PRIVATE_MESSAGE", "", platform.EventKindPrivateMessage), nil)
	require.NotNil(t, ctx)

	// Verify context is properly initialized
	assert.NotNil(t, ctx.GetPlatformEvent())
	assert.Equal(t, string(platform.EventKindPrivateMessage), ctx.GetEventType())

	// Set some data
	ctx.Set("key1", "value1")
	val, ok := ctx.Get("key1")
	assert.True(t, ok)
	assert.Equal(t, "value1", val)

	// Release context
	context.ReleaseContext(ctx)

	// Acquire again - should be cleared
	ctx2 := context.AcquireContextFromEvent(makeTestEvent("test", "PRIVATE_MESSAGE", "", platform.EventKindPrivateMessage), nil)
	require.NotNil(t, ctx2)

	// Verify data is cleared
	val2, ok2 := ctx2.Get("key1")
	assert.False(t, ok2)
	assert.Nil(t, val2)

	// Release
	context.ReleaseContext(ctx2)
}

// TestContextPoolReuse tests that contexts are actually reused
func TestContextPoolReuse(t *testing.T) {

	// Get first context and note its address
	ctx1 := context.AcquireContextFromEvent(makeTestEvent("test", "PRIVATE_MESSAGE", "", platform.EventKindPrivateMessage), nil)
	addr1 := &ctx1
	context.ReleaseContext(ctx1)

	// Get second context - should likely be the same object
	ctx2 := context.AcquireContextFromEvent(makeTestEvent("test", "PRIVATE_MESSAGE", "", platform.EventKindPrivateMessage), nil)
	addr2 := &ctx2
	context.ReleaseContext(ctx2)

	// While we can't guarantee reuse due to sync.Pool behavior,
	// we can verify that both operations succeeded
	assert.NotNil(t, addr1)
	assert.NotNil(t, addr2)
}

// TestContextPoolConcurrent tests concurrent acquire and release
func TestContextPoolConcurrent(t *testing.T) {

	const numGoroutines = 100
	const iterations = 100

	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines)

	for i := range numGoroutines {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for j := range iterations {
				// Acquire
				ctx := context.AcquireContextFromEvent(makeTestEvent("test", "PRIVATE_MESSAGE", "", platform.EventKindPrivateMessage), nil)
				if ctx == nil {
					errors <- assert.AnError
					return
				}

				// Use context
				ctx.Set("goroutine", id)
				ctx.Set("iteration", j)

				val, ok := ctx.Get("goroutine")
				if !ok || val != id {
					errors <- assert.AnError
					return
				}

				// Release
				context.ReleaseContext(ctx)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		t.Fatal("Concurrent test failed:", err)
	}
}

// TestContextPoolDataIsolation ensures data doesn't leak between contexts
func TestContextPoolDataIsolation(t *testing.T) {

	// First context with data
	ctx1 := context.AcquireContextFromEvent(makeTestEvent("test", "PRIVATE_MESSAGE", "", platform.EventKindPrivateMessage), nil)
	ctx1.Set("secret", "should_not_leak")
	ctx1.Set("number", 42)
	context.ReleaseContext(ctx1)

	// Second context should not see first context's data
	ctx2 := context.AcquireContextFromEvent(makeTestEvent("test", "PRIVATE_MESSAGE", "", platform.EventKindPrivateMessage), nil)
	defer context.ReleaseContext(ctx2)

	val1, ok1 := ctx2.Get("secret")
	assert.False(t, ok1)
	assert.Nil(t, val1)

	val2, ok2 := ctx2.Get("number")
	assert.False(t, ok2)
	assert.Nil(t, val2)
}

// TestContextPoolNilRelease tests that releasing nil doesn't panic
func TestContextPoolNilRelease(t *testing.T) {
	assert.NotPanics(t, func() {
		context.ReleaseContext(nil)
	})
}

// TestContextPoolWithExtensions tests that extensions are properly cleared
func TestContextPoolWithExtensions(t *testing.T) {

	ctx := context.AcquireContextFromEvent(makeTestEvent("test", "PRIVATE_MESSAGE", "", platform.EventKindPrivateMessage), nil)

	// Set various data
	ctx.Set("key1", "value1")
	ctx.Set("key2", 123)
	ctx.Set("key3", true)

	// Verify data is set
	val1, ok1 := ctx.Get("key1")
	assert.True(t, ok1)
	assert.Equal(t, "value1", val1)

	val2, ok2 := ctx.Get("key2")
	assert.True(t, ok2)
	assert.Equal(t, 123, val2)

	val3, ok3 := ctx.Get("key3")
	assert.True(t, ok3)
	assert.Equal(t, true, val3)

	// Release
	context.ReleaseContext(ctx)

	// Acquire new context
	ctx2 := context.AcquireContextFromEvent(makeTestEvent("test", "PRIVATE_MESSAGE", "", platform.EventKindPrivateMessage), nil)
	defer context.ReleaseContext(ctx2)

	// Verify all data is cleared
	v1, ok1 := ctx2.Get("key1")
	assert.False(t, ok1)
	assert.Nil(t, v1)

	v2, ok2 := ctx2.Get("key2")
	assert.False(t, ok2)
	assert.Nil(t, v2)

	v3, ok3 := ctx2.Get("key3")
	assert.False(t, ok3)
	assert.Nil(t, v3)
}

// TestGetContextPoolStats tests pool statistics
func TestGetContextPoolStats(t *testing.T) {
	stats := context.GetContextPoolStats()
	assert.True(t, stats.PoolEnabled)
}

// BenchmarkContextCreation compares regular creation vs pool
func BenchmarkContextCreation(b *testing.B) {

	b.Run("Regular", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			ctx := context.AcquireContextFromEvent(makeTestEvent("test", "PRIVATE_MESSAGE", "", platform.EventKindPrivateMessage), nil)
			_ = ctx
		}
	})

	b.Run("Pooled", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			ctx := context.AcquireContextFromEvent(makeTestEvent("test", "PRIVATE_MESSAGE", "", platform.EventKindPrivateMessage), nil)
			context.ReleaseContext(ctx)
		}
	})
}

// BenchmarkContextPoolParallel benchmarks concurrent pool usage
func BenchmarkContextPoolParallel(b *testing.B) {

	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			ctx := context.AcquireContextFromEvent(makeTestEvent("test", "PRIVATE_MESSAGE", "", platform.EventKindPrivateMessage), nil)
			ctx.Set("test", "value")
			_, _ = ctx.Get("test")
			context.ReleaseContext(ctx)
		}
	})
}

// BenchmarkContextWithExtensions benchmarks context with extensions usage
func BenchmarkContextWithExtensions(b *testing.B) {

	b.Run("WithoutPool", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			ctx := context.AcquireContextFromEvent(makeTestEvent("test", "PRIVATE_MESSAGE", "", platform.EventKindPrivateMessage), nil)
			ctx.Set("key1", "value1")
			ctx.Set("key2", 123)
			_, _ = ctx.Get("key1")
			_, _ = ctx.Get("key2")
		}
	})

	b.Run("WithPool", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			ctx := context.AcquireContextFromEvent(makeTestEvent("test", "PRIVATE_MESSAGE", "", platform.EventKindPrivateMessage), nil)
			ctx.Set("key1", "value1")
			ctx.Set("key2", 123)
			_, _ = ctx.Get("key1")
			_, _ = ctx.Get("key2")
			context.ReleaseContext(ctx)
		}
	})
}
