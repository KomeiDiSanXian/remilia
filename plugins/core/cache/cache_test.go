package cache

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCache_BasicOperations(t *testing.T) {
	cache := NewLRUCache(3)

	// Test Set and Get
	cache.Set("key1", []byte("value1"), 0)
	value, found := cache.Get("key1")
	require.True(t, found)
	assert.Equal(t, []byte("value1"), value)

	// Test not found
	_, found = cache.Get("nonexistent")
	assert.False(t, found)

	// Test Delete
	cache.Delete("key1")
	_, found = cache.Get("key1")
	assert.False(t, found)
}

func TestCache_LRUEviction(t *testing.T) {
	cache := NewLRUCache(3)

	// Fill cache to capacity
	cache.Set("key1", []byte("value1"), 0)
	cache.Set("key2", []byte("value2"), 0)
	cache.Set("key3", []byte("value3"), 0)

	assert.Equal(t, 3, cache.Size())

	// Add one more, should evict key1 (oldest)
	cache.Set("key4", []byte("value4"), 0)
	assert.Equal(t, 3, cache.Size())

	_, found := cache.Get("key1")
	assert.False(t, found, "key1 should have been evicted")

	_, found = cache.Get("key2")
	assert.True(t, found, "key2 should still exist")
}

func TestCache_LRUOrder(t *testing.T) {
	cache := NewLRUCache(3)

	cache.Set("key1", []byte("value1"), 0)
	cache.Set("key2", []byte("value2"), 0)
	cache.Set("key3", []byte("value3"), 0)

	// Access key1, making it most recently used
	cache.Get("key1")

	// Add key4, should evict key2 (now oldest)
	cache.Set("key4", []byte("value4"), 0)

	_, found := cache.Get("key2")
	assert.False(t, found, "key2 should have been evicted")

	_, found = cache.Get("key1")
	assert.True(t, found, "key1 should still exist")
}

func TestCache_TTL(t *testing.T) {
	cache := NewLRUCache(10)

	// Set with TTL
	cache.Set("key1", []byte("value1"), 100*time.Millisecond)

	// Should exist immediately
	_, found := cache.Get("key1")
	assert.True(t, found)

	// Wait for expiration
	time.Sleep(150 * time.Millisecond)

	// Should not exist after expiration
	_, found = cache.Get("key1")
	assert.False(t, found)
}

func TestCache_CleanExpired(t *testing.T) {
	cache := NewLRUCache(10)

	// Set keys with different TTLs
	cache.Set("key1", []byte("value1"), 50*time.Millisecond)
	cache.Set("key2", []byte("value2"), 200*time.Millisecond)
	cache.Set("key3", []byte("value3"), 0) // No expiration

	// Wait for first key to expire
	time.Sleep(100 * time.Millisecond)

	// Clean expired keys
	count := cache.CleanExpired()
	assert.Equal(t, 1, count)

	_, found := cache.Get("key1")
	assert.False(t, found)

	_, found = cache.Get("key2")
	assert.True(t, found)

	_, found = cache.Get("key3")
	assert.True(t, found)
}

func TestCache_Clear(t *testing.T) {
	cache := NewLRUCache(10)

	cache.Set("key1", []byte("value1"), 0)
	cache.Set("key2", []byte("value2"), 0)
	assert.Equal(t, 2, cache.Size())

	cache.Clear()
	assert.Equal(t, 0, cache.Size())

	_, found := cache.Get("key1")
	assert.False(t, found)
}

func TestCache_Stats(t *testing.T) {
	cache := NewLRUCache(3)

	// Initial stats
	stats := cache.Stats()
	assert.Equal(t, int64(0), stats.Hits)
	assert.Equal(t, int64(0), stats.Misses)

	// Set and get (hit)
	cache.Set("key1", []byte("value1"), 0)
	cache.Get("key1")

	stats = cache.Stats()
	assert.Equal(t, int64(1), stats.Hits)
	assert.Equal(t, int64(0), stats.Misses)

	// Get non-existent key (miss)
	cache.Get("nonexistent")

	stats = cache.Stats()
	assert.Equal(t, int64(1), stats.Hits)
	assert.Equal(t, int64(1), stats.Misses)

	// Test eviction
	cache.Set("key2", []byte("value2"), 0)
	cache.Set("key3", []byte("value3"), 0)
	cache.Set("key4", []byte("value4"), 0) // Should evict key1

	stats = cache.Stats()
	assert.Equal(t, int64(1), stats.Evictions)
}

func TestCache_HitRate(t *testing.T) {
	cache := NewLRUCache(10)

	cache.Set("key1", []byte("value1"), 0)

	// 3 hits, 2 misses
	cache.Get("key1")
	cache.Get("key1")
	cache.Get("key1")
	cache.Get("nonexistent1")
	cache.Get("nonexistent2")

	stats := cache.Stats()
	hitRate := stats.HitRate()

	assert.Equal(t, 0.6, hitRate) // 3/5 = 0.6
}

func TestCache_UpdateValue(t *testing.T) {
	cache := NewLRUCache(10)

	cache.Set("key1", []byte("value1"), 0)
	cache.Set("key1", []byte("value2"), 0) // Update

	value, found := cache.Get("key1")
	require.True(t, found)
	assert.Equal(t, []byte("value2"), value)

	// Size should not change
	assert.Equal(t, 1, cache.Size())
}

func TestCachePlugin_Integration(t *testing.T) {
	plugin := NewWithCapacity(5)

	// Test plugin methods
	plugin.Set("key1", []byte("value1"), 0)

	value, found := plugin.Get("key1")
	require.True(t, found)
	assert.Equal(t, []byte("value1"), value)

	plugin.Delete("key1")
	_, found = plugin.Get("key1")
	assert.False(t, found)

	// Test stats
	plugin.Set("key2", []byte("value2"), 0)
	plugin.Get("key2")

	stats := plugin.Stats()
	assert.Greater(t, stats.Hits, int64(0))

	// Test clear
	plugin.Clear()
	assert.Equal(t, 0, plugin.Size())
}

func TestCachePlugin_Dependencies(t *testing.T) {
	plugin := New()
	deps := plugin.Dependencies()
	assert.Contains(t, deps, "storage")
}

func BenchmarkCache_Set(b *testing.B) {
	cache := NewLRUCache(1000)
	value := []byte("test value")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Set("key", value, 0)
	}
}

func BenchmarkCache_Get(b *testing.B) {
	cache := NewLRUCache(1000)
	cache.Set("key", []byte("test value"), 0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Get("key")
	}
}

func BenchmarkCache_GetMiss(b *testing.B) {
	cache := NewLRUCache(1000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Get("nonexistent")
	}
}

func BenchmarkCache_Concurrent(b *testing.B) {
	cache := NewLRUCache(1000)
	cache.Set("key", []byte("value"), 0)

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if i%2 == 0 {
				cache.Get("key")
			} else {
				cache.Set("key", []byte("value"), 0)
			}
			i++
		}
	})
}
