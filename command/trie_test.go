package command

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrie(t *testing.T) {
	trie := NewTrie()

	// Test Insert and Search
	t.Run("Insert and Search", func(t *testing.T) {
		meta1 := &Meta{Name: "/help"}
		meta2 := &Meta{Name: "/hello"}
		meta3 := &Meta{Name: "/hell"}

		trie.Insert("/help", meta1)
		trie.Insert("/hello", meta2)
		trie.Insert("/hell", meta3)

		// Search for "/hel" should return 3 commands
		results := trie.SearchPrefix("/hel")
		assert.Equal(t, 3, len(results))

		// Search for "/help" should return 1 command
		results = trie.SearchPrefix("/help")
		assert.Equal(t, 1, len(results))

		// Search for "/x" should return empty
		results = trie.SearchPrefix("/x")
		assert.Nil(t, results)
	})

	// Test Remove
	t.Run("Remove", func(t *testing.T) {
		trie.Clear()
		meta1 := &Meta{Name: "/test"}
		trie.Insert("/test", meta1)

		results := trie.SearchPrefix("/test")
		require.Equal(t, 1, len(results))

		trie.Remove("/test", meta1)
		results = trie.SearchPrefix("/test")
		assert.Equal(t, 0, len(results))
	})

	// Test Stats
	t.Run("Stats", func(t *testing.T) {
		trie.Clear()
		trie.Insert("/cmd1", &Meta{Name: "/cmd1"})
		trie.Insert("/cmd2", &Meta{Name: "/cmd2"})

		stats := trie.Stats()
		assert.Greater(t, stats.NodeCount, 0)
		assert.Greater(t, stats.MaxDepth, 0)
	})
}

func TestCommandRegistryWithTrie(t *testing.T) {
	registry := NewCommandRegistry()

	// Register commands
	def1 := &Definition{
		Name:        "/help",
		Description: "Show help",
	}
	def2 := &Definition{
		Name:        "/hello",
		Description: "Say hello",
	}

	err := registry.Register(def1)
	require.NoError(t, err)

	err = registry.Register(def2)
	require.NoError(t, err)

	// Test prefix completion
	t.Run("Complete with Trie", func(t *testing.T) {
		results := registry.Complete("/hel")
		assert.Equal(t, 2, len(results))

		results = registry.Complete("/help")
		assert.Equal(t, 1, len(results))

		results = registry.Complete("/x")
		assert.Nil(t, results)
	})

	// Test memory efficiency
	t.Run("Memory Efficiency", func(t *testing.T) {
		// Register many commands
		for i := range 100 {
			def := &Definition{
				Name:        fmt.Sprintf("/cmd%d", i),
				Description: "Test command",
			}
			_ = registry.Register(def)
		}

		// Trie should handle this efficiently
		results := registry.Complete("/cmd")
		assert.Greater(t, len(results), 0)
	})
}

func BenchmarkTrieInsert(b *testing.B) {
	trie := NewTrie()
	meta := &Meta{Name: "/test"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		trie.Insert("/test", meta)
	}
}

func BenchmarkTrieSearch(b *testing.B) {
	trie := NewTrie()
	for i := range 100 {
		meta := &Meta{Name: fmt.Sprintf("/cmd%d", i)}
		trie.Insert(meta.Name, meta)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = trie.SearchPrefix("/cmd")
	}
}
