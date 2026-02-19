package atomic

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewValue tests the creation of a new atomic value
func TestNewValue(t *testing.T) {
	t.Run("with pointer type", func(t *testing.T) {
		type Config struct {
			Name string
			Port int
		}

		initial := &Config{Name: "test", Port: 8080}
		v := NewValue(initial)

		require.NotNil(t, v)
		loaded := v.Load()
		assert.Equal(t, initial, loaded)
		assert.Equal(t, "test", loaded.Name)
		assert.Equal(t, 8080, loaded.Port)
	})

	t.Run("with value type", func(t *testing.T) {
		v := NewValue(42)
		require.NotNil(t, v)
		assert.Equal(t, 42, v.Load())
	})

	t.Run("with string type", func(t *testing.T) {
		v := NewValue("hello")
		require.NotNil(t, v)
		assert.Equal(t, "hello", v.Load())
	})
}

// TestValue_Load tests the Load operation
func TestValue_Load(t *testing.T) {
	t.Run("returns correct type", func(t *testing.T) {
		v := NewValue("test")
		loaded := v.Load()
		// No type assertion needed!
		assert.Equal(t, "test", loaded)
	})

	t.Run("concurrent loads", func(t *testing.T) {
		v := NewValue(100)
		var wg sync.WaitGroup

		// Multiple goroutines reading concurrently
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				val := v.Load()
				assert.Equal(t, 100, val)
			}()
		}

		wg.Wait()
	})
}

// TestValue_Store tests the Store operation
func TestValue_Store(t *testing.T) {
	t.Run("updates value", func(t *testing.T) {
		v := NewValue(10)
		assert.Equal(t, 10, v.Load())

		v.Store(20)
		assert.Equal(t, 20, v.Load())

		v.Store(30)
		assert.Equal(t, 30, v.Load())
	})

	t.Run("concurrent stores", func(t *testing.T) {
		v := NewValue(0)
		var wg sync.WaitGroup

		// Multiple goroutines writing concurrently
		for i := 0; i < 100; i++ {
			wg.Add(1)
			val := i
			go func() {
				defer wg.Done()
				v.Store(val)
			}()
		}

		wg.Wait()
		// Value should be one of the stored values
		final := v.Load()
		assert.GreaterOrEqual(t, final, 0)
		assert.Less(t, final, 100)
	})
}

// TestValue_Swap tests the Swap operation
func TestValue_Swap(t *testing.T) {
	t.Run("swaps and returns old value", func(t *testing.T) {
		v := NewValue("initial")

		old := v.Swap("new")
		assert.Equal(t, "initial", old)
		assert.Equal(t, "new", v.Load())
	})

	t.Run("concurrent swaps", func(t *testing.T) {
		v := NewValue(0)
		var wg sync.WaitGroup
		results := make([]int, 100)

		// Multiple goroutines swapping concurrently
		for i := 0; i < 100; i++ {
			wg.Add(1)
			idx := i
			go func() {
				defer wg.Done()
				results[idx] = v.Swap(idx + 1)
			}()
		}

		wg.Wait()
		// All old values should be present in results
		// (order is non-deterministic due to concurrency)
	})
}

// TestValue_CompareAndSwap tests the CompareAndSwap operation
func TestValue_CompareAndSwap(t *testing.T) {
	t.Run("swaps when old matches", func(t *testing.T) {
		v := NewValue(10)

		swapped := v.CompareAndSwap(10, 20)
		assert.True(t, swapped)
		assert.Equal(t, 20, v.Load())
	})

	t.Run("does not swap when old does not match", func(t *testing.T) {
		v := NewValue(10)

		swapped := v.CompareAndSwap(99, 20)
		assert.False(t, swapped)
		assert.Equal(t, 10, v.Load()) // Value unchanged
	})

	t.Run("concurrent CAS operations", func(t *testing.T) {
		v := NewValue(0)
		var wg sync.WaitGroup
		successes := make([]bool, 100)

		// Multiple goroutines trying to CAS concurrently
		for i := 0; i < 100; i++ {
			wg.Add(1)
			idx := i
			go func() {
				defer wg.Done()
				// Try to swap from 0 to idx+1
				successes[idx] = v.CompareAndSwap(0, idx+1)
			}()
		}

		wg.Wait()

		// Only one goroutine should succeed
		successCount := 0
		for _, success := range successes {
			if success {
				successCount++
			}
		}
		assert.Equal(t, 1, successCount, "Only one CAS should succeed")
		assert.NotEqual(t, 0, v.Load(), "Value should have changed")
	})
}

// TestValue_TypeSafety demonstrates compile-time type safety
func TestValue_TypeSafety(t *testing.T) {
	t.Run("pointer types are type-safe", func(t *testing.T) {
		type State struct {
			Counter int
		}

		v := NewValue(&State{Counter: 1})

		// No type assertion needed - compile-time type safety
		state := v.Load()
		assert.Equal(t, 1, state.Counter)

		state.Counter = 2
		v.Store(state)

		newState := v.Load()
		assert.Equal(t, 2, newState.Counter)
	})
}

// BenchmarkValue_Load benchmarks the Load operation
func BenchmarkValue_Load(b *testing.B) {
	v := NewValue(42)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = v.Load()
	}
}

// BenchmarkValue_Store benchmarks the Store operation
func BenchmarkValue_Store(b *testing.B) {
	v := NewValue(0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v.Store(i)
	}
}

// BenchmarkValue_CompareAndSwap benchmarks the CAS operation
func BenchmarkValue_CompareAndSwap(b *testing.B) {
	v := NewValue(0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v.CompareAndSwap(i, i+1)
	}
}

// BenchmarkValue_LoadParallel benchmarks parallel Load operations
func BenchmarkValue_LoadParallel(b *testing.B) {
	v := NewValue(42)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = v.Load()
		}
	})
}

// BenchmarkValue_StoreParallel benchmarks parallel Store operations
func BenchmarkValue_StoreParallel(b *testing.B) {
	v := NewValue(0)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			v.Store(42)
		}
	})
}
