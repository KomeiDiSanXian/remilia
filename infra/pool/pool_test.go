package pool

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewInstrumentedPool tests creating a new instrumented pool
func TestNewInstrumentedPool(t *testing.T) {
	newFunc := func() any {
		return &struct{ Value int }{Value: 42}
	}

	pool := NewInstrumentedPool(newFunc)
	require.NotNil(t, pool)

	// Verify initial stats
	stats := pool.Stats()
	assert.Equal(t, uint64(0), stats.Gets)
	assert.Equal(t, uint64(0), stats.Puts)
	assert.Equal(t, uint64(0), stats.News)
	assert.Equal(t, 0.0, stats.HitRate)
}

// TestInstrumentedPool_Get tests getting items from pool
func TestInstrumentedPool_Get(t *testing.T) {
	callCount := 0
	newFunc := func() any {
		callCount++
		return callCount
	}

	pool := NewInstrumentedPool(newFunc)

	// First get - should create new
	v1 := pool.Get()
	assert.Equal(t, 1, v1)

	stats := pool.Stats()
	assert.Equal(t, uint64(1), stats.Gets)
	assert.Equal(t, uint64(1), stats.News)

	// Second get - should create new (pool is empty)
	v2 := pool.Get()
	assert.Equal(t, 2, v2)

	stats = pool.Stats()
	assert.Equal(t, uint64(2), stats.Gets)
	assert.Equal(t, uint64(2), stats.News)
}

// TestInstrumentedPool_Put tests putting items back to pool
func TestInstrumentedPool_Put(t *testing.T) {
	pool := NewInstrumentedPool(func() any { return 0 })

	pool.Put(100)
	pool.Put(200)

	stats := pool.Stats()
	assert.Equal(t, uint64(2), stats.Puts)
}

// TestInstrumentedPool_GetPutCycle tests get-put cycle
func TestInstrumentedPool_GetPutCycle(t *testing.T) {
	newCount := 0
	pool := NewInstrumentedPool(func() any {
		newCount++
		return newCount
	})

	// Get and put back
	v1 := pool.Get()
	assert.Equal(t, 1, v1)
	pool.Put(v1)

	// Get again - should reuse
	v2 := pool.Get()
	assert.Equal(t, 1, v2) // Same value

	stats := pool.Stats()
	assert.Equal(t, uint64(2), stats.Gets)
	assert.Equal(t, uint64(1), stats.Puts)
	assert.Equal(t, uint64(1), stats.News) // Only one new object created
}

// TestInstrumentedPool_Stats tests statistics calculation
func TestInstrumentedPool_Stats(t *testing.T) {
	tests := []struct {
		name            string
		gets            uint64
		news            uint64
		expectedHitRate float64
	}{
		{
			name:            "no gets",
			gets:            0,
			news:            0,
			expectedHitRate: 0.0,
		},
		{
			name:            "all misses",
			gets:            10,
			news:            10,
			expectedHitRate: 0.0,
		},
		{
			name:            "50% hit rate",
			gets:            10,
			news:            5,
			expectedHitRate: 50.0,
		},
		{
			name:            "100% hit rate",
			gets:            10,
			news:            0,
			expectedHitRate: 100.0,
		},
		{
			name:            "90% hit rate",
			gets:            100,
			news:            10,
			expectedHitRate: 90.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := NewInstrumentedPool(func() any { return 0 })

			// Manually set stats for testing
			pool.gets.Store(tt.gets)
			pool.news.Store(tt.news)

			stats := pool.Stats()
			assert.Equal(t, tt.gets, stats.Gets)
			assert.Equal(t, tt.news, stats.News)
			assert.InDelta(t, tt.expectedHitRate, stats.HitRate, 0.01)
		})
	}
}

// TestInstrumentedPool_Reset tests resetting statistics
func TestInstrumentedPool_Reset(t *testing.T) {
	pool := NewInstrumentedPool(func() any { return 0 })

	// Perform some operations
	for range 5 {
		v := pool.Get()
		pool.Put(v)
	}

	// Verify stats are non-zero
	stats := pool.Stats()
	assert.Greater(t, stats.Gets, uint64(0))
	assert.Greater(t, stats.Puts, uint64(0))

	// Reset
	pool.Reset()

	// Verify stats are zero
	stats = pool.Stats()
	assert.Equal(t, uint64(0), stats.Gets)
	assert.Equal(t, uint64(0), stats.Puts)
	assert.Equal(t, uint64(0), stats.News)
	assert.Equal(t, 0.0, stats.HitRate)
}

// TestInstrumentedPool_Concurrent tests concurrent access
func TestInstrumentedPool_Concurrent(t *testing.T) {
	pool := NewInstrumentedPool(func() any { return new(int) })

	const goroutines = 10
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for range goroutines {
		go func() {
			defer wg.Done()
			for range iterations {
				v := pool.Get()
				pool.Put(v)
			}
		}()
	}

	wg.Wait()

	// Verify stats
	stats := pool.Stats()
	assert.Equal(t, uint64(goroutines*iterations), stats.Gets)
	assert.Equal(t, uint64(goroutines*iterations), stats.Puts)
	assert.Greater(t, stats.News, uint64(0)) // At least some new objects created
}

// TestTypedPool_New tests creating a new typed pool
func TestTypedPool_New(t *testing.T) {
	t.Run("int pool", func(t *testing.T) {
		pool := New(func() int { return 42 })
		require.NotNil(t, pool)
		require.NotNil(t, pool.p)
	})

	t.Run("string pool", func(t *testing.T) {
		pool := New(func() string { return "hello" })
		require.NotNil(t, pool)
		require.NotNil(t, pool.p)
	})

	t.Run("struct pool", func(t *testing.T) {
		type TestStruct struct {
			Name  string
			Value int
		}
		pool := New(func() *TestStruct {
			return &TestStruct{Name: "test", Value: 100}
		})
		require.NotNil(t, pool)
		require.NotNil(t, pool.p)
	})
}

// TestTypedPool_Get tests getting typed items from pool
func TestTypedPool_Get(t *testing.T) {
	t.Run("int pool", func(t *testing.T) {
		pool := New(func() int { return 42 })

		v := pool.Get()
		assert.Equal(t, 42, v)
	})

	t.Run("string pool", func(t *testing.T) {
		pool := New(func() string { return "hello" })

		v := pool.Get()
		assert.Equal(t, "hello", v)
	})

	t.Run("pointer pool", func(t *testing.T) {
		type Data struct{ Value int }
		pool := New(func() *Data { return &Data{Value: 100} })

		v := pool.Get()
		require.NotNil(t, v)
		assert.Equal(t, 100, v.Value)
	})
}

// TestTypedPool_Put tests putting typed items back to pool
func TestTypedPool_Put(t *testing.T) {
	pool := New(func() int { return 0 })

	pool.Put(100)
	pool.Put(200)

	stats := pool.Stats()
	assert.Equal(t, uint64(2), stats.Puts)
}

// TestTypedPool_GetPutCycle tests typed get-put cycle
func TestTypedPool_GetPutCycle(t *testing.T) {
	type Data struct {
		Value int
		Name  string
	}

	newCount := 0
	pool := New(func() *Data {
		newCount++
		return &Data{Value: newCount, Name: "test"}
	})

	// Get and modify
	v1 := pool.Get()
	assert.Equal(t, 1, v1.Value)
	v1.Value = 999
	v1.Name = "modified"

	// Put back
	pool.Put(v1)

	// Get again - should reuse same object
	v2 := pool.Get()
	assert.Equal(t, 999, v2.Value)
	assert.Equal(t, "modified", v2.Name)

	stats := pool.Stats()
	assert.Equal(t, uint64(2), stats.Gets)
	assert.Equal(t, uint64(1), stats.Puts)
	assert.Equal(t, uint64(1), stats.News)
}

// TestTypedPool_Stats tests typed pool statistics
func TestTypedPool_Stats(t *testing.T) {
	pool := New(func() []int { return make([]int, 0, 10) })

	// Perform operations
	v1 := pool.Get()
	v2 := pool.Get()
	pool.Put(v1)
	pool.Put(v2)

	stats := pool.Stats()
	assert.Equal(t, uint64(2), stats.Gets)
	assert.Equal(t, uint64(2), stats.Puts)
	assert.Equal(t, uint64(2), stats.News)
	assert.Equal(t, 0.0, stats.HitRate)
}

// TestTypedPool_Reset tests typed pool reset
func TestTypedPool_Reset(t *testing.T) {
	pool := New(func() string { return "test" })

	// Perform operations
	for range 5 {
		v := pool.Get()
		pool.Put(v)
	}

	// Reset
	pool.Reset()

	// Verify stats are zero
	stats := pool.Stats()
	assert.Equal(t, uint64(0), stats.Gets)
	assert.Equal(t, uint64(0), stats.Puts)
	assert.Equal(t, uint64(0), stats.News)
}

// TestTypedPool_Raw tests accessing raw pool
func TestTypedPool_Raw(t *testing.T) {
	pool := New(func() int { return 42 })

	raw := pool.Raw()
	require.NotNil(t, raw)

	// Verify it's the actual underlying pool
	v := raw.Get()
	assert.Equal(t, 42, v)
}

// TestTypedPool_Concurrent tests concurrent typed pool access
func TestTypedPool_Concurrent(t *testing.T) {
	type Buffer struct {
		Data []byte
	}

	pool := New(func() *Buffer {
		return &Buffer{Data: make([]byte, 0, 1024)}
	})

	const goroutines = 20
	const iterations = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := range goroutines {
		go func(id int) {
			defer wg.Done()
			for range iterations {
				buf := pool.Get()
				buf.Data = buf.Data[:0] // Reset
				buf.Data = append(buf.Data, byte(id))
				pool.Put(buf)
			}
		}(i)
	}

	wg.Wait()

	// Verify stats
	stats := pool.Stats()
	assert.Equal(t, uint64(goroutines*iterations), stats.Gets)
	assert.Equal(t, uint64(goroutines*iterations), stats.Puts)
	assert.Greater(t, stats.HitRate, 0.0) // Should have some cache hits
}

// TestTypedPool_DifferentTypes tests pools with different types
func TestTypedPool_DifferentTypes(t *testing.T) {
	t.Run("slice pool", func(t *testing.T) {
		pool := New(func() []string {
			return make([]string, 0, 10)
		})

		v := pool.Get()
		assert.NotNil(t, v)
		assert.Equal(t, 0, len(v))
		assert.Equal(t, 10, cap(v))

		v = append(v, "test")
		pool.Put(v)

		v2 := pool.Get()
		assert.Equal(t, 1, len(v2))
		assert.Equal(t, "test", v2[0])
	})

	t.Run("map pool", func(t *testing.T) {
		pool := New(func() map[string]int {
			return make(map[string]int, 10)
		})

		v := pool.Get()
		assert.NotNil(t, v)
		assert.Equal(t, 0, len(v))

		v["key"] = 123
		pool.Put(v)

		v2 := pool.Get()
		assert.Equal(t, 123, v2["key"])
	})

	t.Run("channel pool", func(t *testing.T) {
		pool := New(func() chan int {
			return make(chan int, 5)
		})

		v := pool.Get()
		assert.NotNil(t, v)

		v <- 42
		pool.Put(v)

		v2 := pool.Get()
		val := <-v2
		assert.Equal(t, 42, val)
	})
}

// TestTypedPool_LargeObjects tests pool with large objects
func TestTypedPool_LargeObjects(t *testing.T) {
	type LargeStruct struct {
		Data [1024 * 10]byte // 10KB
		ID   int
	}

	pool := New(func() *LargeStruct {
		return &LargeStruct{}
	})

	// Get and put multiple times
	for i := range 100 {
		obj := pool.Get()
		obj.ID = i
		pool.Put(obj)
	}

	stats := pool.Stats()
	assert.Equal(t, uint64(100), stats.Gets)
	assert.Equal(t, uint64(100), stats.Puts)

	// Should have high hit rate due to reuse
	assert.Greater(t, stats.HitRate, 50.0)
}

// TestStats_Structure tests Stats struct
func TestStats_Structure(t *testing.T) {
	stats := Stats{
		Gets:    100,
		Puts:    90,
		News:    10,
		HitRate: 90.0,
	}

	assert.Equal(t, uint64(100), stats.Gets)
	assert.Equal(t, uint64(90), stats.Puts)
	assert.Equal(t, uint64(10), stats.News)
	assert.Equal(t, 90.0, stats.HitRate)
}

// TestPoolInterface tests Pool interface implementation
func TestPoolInterface(t *testing.T) {
	var _ Pool = (*InstrumentedPool)(nil)

	pool := NewInstrumentedPool(func() any { return 42 })

	// Use through interface
	var iface Pool = pool

	v := iface.Get()
	assert.NotNil(t, v)

	iface.Put(v)

	// Verify stats
	stats := pool.Stats()
	assert.Equal(t, uint64(1), stats.Gets)
	assert.Equal(t, uint64(1), stats.Puts)
}

// BenchmarkInstrumentedPool_Get benchmarks Get operation
func BenchmarkInstrumentedPool_Get(b *testing.B) {
	pool := NewInstrumentedPool(func() any { return new(int) })

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = pool.Get()
	}
}

// BenchmarkInstrumentedPool_GetPut benchmarks Get-Put cycle
func BenchmarkInstrumentedPool_GetPut(b *testing.B) {
	pool := NewInstrumentedPool(func() any { return new(int) })

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		v := pool.Get()
		pool.Put(v)
	}
}

// BenchmarkTypedPool_Get benchmarks typed Get operation
func BenchmarkTypedPool_Get(b *testing.B) {
	pool := New(func() *int { return new(int) })

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = pool.Get()
	}
}

// BenchmarkTypedPool_GetPut benchmarks typed Get-Put cycle
func BenchmarkTypedPool_GetPut(b *testing.B) {
	pool := New(func() *int { return new(int) })

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		v := pool.Get()
		pool.Put(v)
	}
}

// BenchmarkTypedPool_Concurrent benchmarks concurrent access
func BenchmarkTypedPool_Concurrent(b *testing.B) {
	pool := New(func() []byte { return make([]byte, 1024) })

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			v := pool.Get()
			pool.Put(v)
		}
	})
}

// BenchmarkStdPool_Comparison benchmarks standard sync.Pool for comparison
func BenchmarkStdPool_Comparison(b *testing.B) {
	pool := &sync.Pool{
		New: func() any { return new(int) },
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		v := pool.Get()
		pool.Put(v)
	}
}
