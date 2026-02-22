package command

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTrieExactMatch 测试 Trie 的精确匹配功能
func TestTrieExactMatch(t *testing.T) {
	trie := NewTrie()

	// 注册命令
	def1 := &Definition{Name: "/help"}
	meta1 := &Meta{Name: "/help", Definition: def1}

	def2 := &Definition{Name: "/hello"}
	meta2 := &Meta{Name: "/hello", Definition: def2}

	def3 := &Definition{Name: "/he"}
	meta3 := &Meta{Name: "/he", Definition: def3}

	trie.Insert("/help", meta1)
	trie.Insert("/hello", meta2)
	trie.Insert("/he", meta3)

	t.Run("exact_match_found", func(t *testing.T) {
		result := trie.ExactMatch("/help")
		assert.NotNil(t, result)
		assert.Equal(t, "/help", result.Name)
	})

	t.Run("exact_match_not_found", func(t *testing.T) {
		result := trie.ExactMatch("/unknown")
		assert.Nil(t, result)
	})

	t.Run("prefix_is_not_exact_match", func(t *testing.T) {
		// "/hel" 是 "/hello" 的前缀，但不是精确匹配
		result := trie.ExactMatch("/hel")
		assert.Nil(t, result)
	})

	t.Run("exact_match_vs_prefix", func(t *testing.T) {
		// "/he" 既是独立命令，也是其他命令的前缀
		result := trie.ExactMatch("/he")
		assert.NotNil(t, result)
		assert.Equal(t, "/he", result.Name)
	})
}

// TestRegistryWithTrieOnly 测试只使用 Trie 的 Registry
func TestRegistryWithTrieOnly(t *testing.T) {
	registry := NewCommandRegistry()

	// 注册命令
	def1 := &Definition{
		Name:        "/ping",
		Description: "Test connection",
	}
	def2 := &Definition{
		Name:        "/pong",
		Description: "Response",
		Aliases:     []string{"/p"},
	}
	def3 := &Definition{
		Name:        "/help",
		Description: "Show help",
	}

	require.NoError(t, registry.Register(def1))
	require.NoError(t, registry.Register(def2))
	require.NoError(t, registry.Register(def3))

	t.Run("lookup_by_name", func(t *testing.T) {
		meta, found := registry.Lookup("/ping")
		assert.True(t, found)
		assert.NotNil(t, meta)
		assert.Equal(t, "/ping", meta.Name)
	})

	t.Run("lookup_by_alias", func(t *testing.T) {
		meta, found := registry.Lookup("/p")
		assert.True(t, found)
		assert.NotNil(t, meta)
		assert.Equal(t, "/pong", meta.Name)
	})

	t.Run("lookup_not_found", func(t *testing.T) {
		meta, found := registry.Lookup("/unknown")
		assert.False(t, found)
		assert.Nil(t, meta)
	})

	t.Run("complete_prefix", func(t *testing.T) {
		results := registry.Complete("/p")
		assert.NotNil(t, results)
		assert.Equal(t, 2, len(results)) // /ping and /pong
	})

	t.Run("list_all", func(t *testing.T) {
		all := registry.List()
		assert.Equal(t, 3, len(all))
	})

	t.Run("unregister", func(t *testing.T) {
		err := registry.Unregister("/ping")
		assert.NoError(t, err)

		meta, found := registry.Lookup("/ping")
		assert.False(t, found)
		assert.Nil(t, meta)

		// 其他命令仍然存在
		meta, found = registry.Lookup("/pong")
		assert.True(t, found)
	})

	t.Run("stats", func(t *testing.T) {
		stats := registry.GetStats()
		assert.Equal(t, 2, stats.CommandCount) // /ping 已删除
		assert.Equal(t, 1, stats.AliasCount)   // /p
	})
}

// TestRegistryDuplicateDetection 测试重复命令检测
func TestRegistryDuplicateDetection(t *testing.T) {
	registry := NewCommandRegistry()

	def1 := &Definition{Name: "/test"}
	require.NoError(t, registry.Register(def1))

	// 尝试注册相同命令
	def2 := &Definition{Name: "/test"}
	err := registry.Register(def2)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")
}
