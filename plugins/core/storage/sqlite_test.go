package storage

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSQLiteStorage_Basic(t *testing.T) {
	// 创建临时数据库文件
	dbPath := "test_basic.db"
	defer os.Remove(dbPath)

	storage, err := NewSQLiteStorage(dbPath)
	require.NoError(t, err)
	defer storage.Close()

	// Test Set and Get
	err = storage.Set("key1", []byte("value1"), 0)
	require.NoError(t, err)

	value, err := storage.Get("key1")
	require.NoError(t, err)
	assert.Equal(t, []byte("value1"), value)

	// Test not found
	_, err = storage.Get("nonexistent")
	assert.Equal(t, ErrNotFound, err)

	// Test Delete
	err = storage.Delete("key1")
	require.NoError(t, err)

	_, err = storage.Get("key1")
	assert.Equal(t, ErrNotFound, err)
}

func TestSQLiteStorage_TTL(t *testing.T) {
	dbPath := "test_ttl.db"
	defer os.Remove(dbPath)

	storage, err := NewSQLiteStorage(dbPath)
	require.NoError(t, err)
	defer storage.Close()

	// Set with TTL
	err = storage.Set("key1", []byte("value1"), 100*time.Millisecond)
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

func TestSQLiteStorage_Keys(t *testing.T) {
	dbPath := "test_keys.db"
	defer os.Remove(dbPath)

	storage, err := NewSQLiteStorage(dbPath)
	require.NoError(t, err)
	defer storage.Close()

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

func TestSQLiteStorage_Clear(t *testing.T) {
	dbPath := "test_clear.db"
	defer os.Remove(dbPath)

	storage, err := NewSQLiteStorage(dbPath)
	require.NoError(t, err)
	defer storage.Close()

	storage.Set("key1", []byte("value1"), 0)
	storage.Set("key2", []byte("value2"), 0)

	size, err := storage.Size()
	require.NoError(t, err)
	assert.Equal(t, 2, size)

	err = storage.Clear()
	require.NoError(t, err)

	size, err = storage.Size()
	require.NoError(t, err)
	assert.Equal(t, 0, size)

	assert.False(t, storage.Exists("key1"))
	assert.False(t, storage.Exists("key2"))
}

func TestSQLiteStorage_CleanExpired(t *testing.T) {
	dbPath := "test_clean.db"
	defer os.Remove(dbPath)

	storage, err := NewSQLiteStorage(dbPath)
	require.NoError(t, err)
	defer storage.Close()

	// Set keys with different TTLs
	storage.Set("key1", []byte("value1"), 100*time.Millisecond)
	storage.Set("key2", []byte("value2"), 500*time.Millisecond)
	storage.Set("key3", []byte("value3"), 0) // No expiration

	// Wait for first key to expire
	time.Sleep(200 * time.Millisecond)

	// Clean expired keys
	count, err := storage.CleanExpired()
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	assert.False(t, storage.Exists("key1"))
	assert.True(t, storage.Exists("key2"))
	assert.True(t, storage.Exists("key3"))
}

func TestSQLiteStorage_Update(t *testing.T) {
	dbPath := "test_update.db"
	defer os.Remove(dbPath)

	storage, err := NewSQLiteStorage(dbPath)
	require.NoError(t, err)
	defer storage.Close()

	// Set initial value
	storage.Set("key1", []byte("value1"), 0)

	// Update value
	storage.Set("key1", []byte("value2"), 0)

	value, err := storage.Get("key1")
	require.NoError(t, err)
	assert.Equal(t, []byte("value2"), value)

	// Size should not change
	size, err := storage.Size()
	require.NoError(t, err)
	assert.Equal(t, 1, size)
}

func TestSQLiteStorage_Stats(t *testing.T) {
	dbPath := "test_stats.db"
	defer os.Remove(dbPath)

	storage, err := NewSQLiteStorage(dbPath)
	require.NoError(t, err)
	defer storage.Close()

	// Add some data
	storage.Set("key1", []byte("value1"), 0)
	storage.Set("key2", []byte("value2"), 50*time.Millisecond)
	storage.Set("key3", []byte("value3"), 0)

	// Wait for key2 to expire
	time.Sleep(100 * time.Millisecond)

	stats, err := storage.Stats()
	require.NoError(t, err)

	assert.Equal(t, 3, stats["total_keys"])
	assert.Equal(t, 2, stats["valid_keys"])
	assert.Equal(t, 1, stats["expired_keys"])
	assert.NotNil(t, stats["db_path"])
}

func TestSQLiteStorage_Compact(t *testing.T) {
	dbPath := "test_compact.db"
	defer os.Remove(dbPath)

	storage, err := NewSQLiteStorage(dbPath)
	require.NoError(t, err)
	defer storage.Close()

	// Add and delete some data
	for i := 0; i < 100; i++ {
		storage.Set("key", []byte("value"), 0)
		storage.Delete("key")
	}

	// Compact database
	err = storage.Compact()
	require.NoError(t, err)
}

func TestSQLiteStorage_Concurrent(t *testing.T) {
	dbPath := "test_concurrent.db"
	defer os.Remove(dbPath)

	storage, err := NewSQLiteStorage(dbPath)
	require.NoError(t, err)
	defer storage.Close()

	// Concurrent writes and reads
	done := make(chan bool, 10)

	for i := 0; i < 5; i++ {
		go func(id int) {
			for j := 0; j < 10; j++ {
				key := "key"
				storage.Set(key, []byte("value"), 0)
			}
			done <- true
		}(i)
	}

	for i := 0; i < 5; i++ {
		go func() {
			for j := 0; j < 10; j++ {
				storage.Get("key")
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestSQLiteStorage_Persistence(t *testing.T) {
	dbPath := "test_persistence.db"
	defer os.Remove(dbPath)

	// Create storage and add data
	storage1, err := NewSQLiteStorage(dbPath)
	require.NoError(t, err)

	storage1.Set("key1", []byte("value1"), 0)
	storage1.Set("key2", []byte("value2"), 0)
	storage1.Close()

	// Reopen storage
	storage2, err := NewSQLiteStorage(dbPath)
	require.NoError(t, err)
	defer storage2.Close()

	// Data should persist
	value, err := storage2.Get("key1")
	require.NoError(t, err)
	assert.Equal(t, []byte("value1"), value)

	value, err = storage2.Get("key2")
	require.NoError(t, err)
	assert.Equal(t, []byte("value2"), value)

	size, err := storage2.Size()
	require.NoError(t, err)
	assert.Equal(t, 2, size)
}

func TestSQLiteStoragePlugin_Integration(t *testing.T) {
	dbPath := "test_plugin.db"
	defer os.Remove(dbPath)

	sqliteStorage, err := NewSQLiteStorage(dbPath)
	require.NoError(t, err)
	defer sqliteStorage.Close()

	plugin := NewWithBackend(sqliteStorage)

	// Test plugin methods
	plugin.Set("key1", []byte("value1"), 0)

	value, err := plugin.Get("key1")
	require.NoError(t, err)
	assert.Equal(t, []byte("value1"), value)

	plugin.Delete("key1")
	_, err = plugin.Get("key1")
	assert.Equal(t, ErrNotFound, err)
}

func BenchmarkSQLiteStorage_Set(b *testing.B) {
	dbPath := "bench_set.db"
	defer os.Remove(dbPath)

	storage, _ := NewSQLiteStorage(dbPath)
	defer storage.Close()

	value := []byte("test value")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		storage.Set("key", value, 0)
	}
}

func BenchmarkSQLiteStorage_Get(b *testing.B) {
	dbPath := "bench_get.db"
	defer os.Remove(dbPath)

	storage, _ := NewSQLiteStorage(dbPath)
	defer storage.Close()

	storage.Set("key", []byte("test value"), 0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		storage.Get("key")
	}
}

func BenchmarkSQLiteStorage_Concurrent(b *testing.B) {
	dbPath := "bench_concurrent.db"
	defer os.Remove(dbPath)

	storage, _ := NewSQLiteStorage(dbPath)
	defer storage.Close()

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
