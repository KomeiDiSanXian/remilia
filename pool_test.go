package remilia

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestInstrumentedPool
func TestInstrumentedPool(t *testing.T) {
	t.Parallel()
	pool := NewInstrumentedPool(func() interface{} {
		return &Context{
			state: make(State),
		}
	})

	pool.Reset()

	obj1 := pool.Get()
	assert.NotNil(t, obj1)

	obj2 := pool.Get()
	assert.NotNil(t, obj2)

	pool.Put(obj1)
	pool.Put(obj2)

	obj3 := pool.Get()
	assert.NotNil(t, obj3)

	stats := pool.Stats()
	assert.Equal(t, uint64(3), stats.Gets)
	assert.Equal(t, uint64(2), stats.Puts)
	assert.Greater(t, stats.News, uint64(0))
}

// TestPoolStats
func TestPoolStats(t *testing.T) {
	t.Parallel()
	pool := NewInstrumentedPool(func() interface{} {
		return &Context{state: make(State)}
	})

	pool.Reset()

	obj1 := pool.Get()
	stats1 := pool.Stats()
	assert.Equal(t, uint64(1), stats1.Gets)
	assert.Equal(t, uint64(1), stats1.News)
	assert.Equal(t, 0.0, stats1.HitRate)
	pool.Put(obj1)

	obj2 := pool.Get()
	stats2 := pool.Stats()
	assert.Equal(t, uint64(2), stats2.Gets)
	assert.Equal(t, uint64(1), stats2.Puts)

	if stats2.Gets > stats2.News {
		assert.Greater(t, stats2.HitRate, 0.0)
	}

	pool.Put(obj2)
}
