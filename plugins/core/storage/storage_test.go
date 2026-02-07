package storage

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryStorage_Basic(t *testing.T) {
	storage := NewMemoryStorage()

	// Test Set and Get
	err := storage.Set("key1", []byte("value1"), 0)
	require.NoError(t, err)

	value, err := storage.Get("key1")
	require.NoError(t, err)
	assert.Equal(t, []byte("value1"), value)

	// Test Exists
	assert.True(t, storage.Exists("key1"))
	assert.False(t, storage.Exists("key2"))

	// Test Delete
	err = storage.Delete("key1")
	require.NoError(t, err)
	assert.False(t, storage.Exists("key1"))

	_, err = storage.Get("key1")
	assert.Equal(t, ErrNotFound, err)
}

func TestMemoryStorage_TTL(t *testing.T) {
	storage := NewMemoryStorage()

	// Set with TTL
	err := storage.Set("key1", []byte("value1"), 100*time.Millisecond)
	require.NoError(t, err)

	// Should exist immediately
	assert.True(t, storage.Exists("key1"))

	// Wait for expiration
	time.Sleep(150 * time.Millisecond)

	// Should not exist after expiration
	assert.False(t, storage.Exists("key1"))

	_, err = storage.Get("key1")
	assert.Equal(t, ErrExpired, err)
}

func TestMemoryStorage_Keys(t *testing.T) {
	storage := NewMemoryStorage()

	// Set multiple keys
	storage.Set("user:1", []byte("alice"), 0)
	storage.Set("user:2", []byte("bob"), 0)
	storage.Set("post:1", []byte("hello"), 0)

	// Test wildcard pattern
	keys, err := storage.Keys("user:*")
	require.NoError(t, err)
	assert.Len(t, keys, 2)
	assert.Contains(t, keys, "user:1")
	assert.Contains(t, keys, "user:2")

	// Test exact match
	keys, err = storage.Keys("post:1")
	require.NoError(t, err)
	assert.Len(t, keys, 1)
	assert.Contains(t, keys, "post:1")

	// Test all keys
	keys, err = storage.Keys("*")
	require.NoError(t, err)
	assert.Len(t, keys, 3)
}

func TestMemoryStorage_Clear(t *testing.T) {
	storage := NewMemoryStorage()

	storage.Set("key1", []byte("value1"), 0)
	storage.Set("key2", []byte("value2"), 0)
	assert.Equal(t, 2, storage.Size())

	err := storage.Clear()
	require.NoError(t, err)
	assert.Equal(t, 0, storage.Size())
	assert.False(t, storage.Exists("key1"))
	assert.False(t, storage.Exists("key2"))
}

func TestMemoryStorage_CleanExpired(t *testing.T) {
	storage := NewMemoryStorage()

	// Set keys with different TTLs
	storage.Set("key1", []byte("value1"), 50*time.Millisecond)
	storage.Set("key2", []byte("value2"), 200*time.Millisecond)
	storage.Set("key3", []byte("value3"), 0) // No expiration

	// Wait for first key to expire
	time.Sleep(100 * time.Millisecond)

	// Clean expired keys
	count := storage.CleanExpired()
	assert.Equal(t, 1, count)
	assert.False(t, storage.Exists("key1"))
	assert.True(t, storage.Exists("key2"))
	assert.True(t, storage.Exists("key3"))
}

func TestStoragePlugin_JSON(t *testing.T) {
	plugin := New()

	type User struct {
		Name string
		Age  int
	}

	// Test SetJSON and GetJSON
	user := User{Name: "Alice", Age: 25}
	err := plugin.SetJSON("user:1", user, 0)
	require.NoError(t, err)

	var retrieved User
	err = plugin.GetJSON("user:1", &retrieved)
	require.NoError(t, err)
	assert.Equal(t, user, retrieved)
}

func TestStoragePlugin_Dependencies(t *testing.T) {
	plugin := New()
	deps := plugin.Dependencies()
	assert.Empty(t, deps)
}

func BenchmarkMemoryStorage_Set(b *testing.B) {
	storage := NewMemoryStorage()
	value := []byte("test value")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		storage.Set("key", value, 0)
	}
}

func BenchmarkMemoryStorage_Get(b *testing.B) {
	storage := NewMemoryStorage()
	storage.Set("key", []byte("test value"), 0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		storage.Get("key")
	}
}

func BenchmarkMemoryStorage_Concurrent(b *testing.B) {
	storage := NewMemoryStorage()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if i%2 == 0 {
				storage.Set("key", []byte("value"), 0)
			} else {
				storage.Get("key")
			}
			i++
		}
	})
}
