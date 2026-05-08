package trie

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchPrefix(t *testing.T) {
	trie := New[string]()

	trie.Insert("hello", "hello")
	trie.Insert("help", "help")
	trie.Insert("world", "world")

	t.Run("prefix matches", func(t *testing.T) {
		results := trie.SearchPrefix("hel")
		require.Len(t, results, 2)
	})

	t.Run("exact match only", func(t *testing.T) {
		results := trie.SearchPrefix("hello")
		require.Len(t, results, 1)
		assert.Equal(t, "hello", results[0])
	})

	t.Run("no match", func(t *testing.T) {
		results := trie.SearchPrefix("xyz")
		assert.Nil(t, results)
	})
}

func TestExactMatch(t *testing.T) {
	trie := New[string]()
	trie.Insert("foo", "foo_value")

	v, ok := trie.ExactMatch("foo")
	assert.True(t, ok)
	assert.Equal(t, "foo_value", v)

	_, ok = trie.ExactMatch("fo")
	assert.False(t, ok)

	_, ok = trie.ExactMatch("bar")
	assert.False(t, ok)
}

func TestRemove(t *testing.T) {
	trie := New[string]()
	trie.Insert("test", "val1")
	trie.Insert("test", "val2")

	trie.Remove("test", "val1")

	vals := trie.GetAll()
	assert.Len(t, vals, 1)
	assert.Equal(t, "val2", vals[0])
}

func TestClear(t *testing.T) {
	trie := New[string]()
	trie.Insert("a", "1")
	trie.Clear()

	results := trie.SearchPrefix("a")
	assert.Nil(t, results)
}

func TestGetAll(t *testing.T) {
	trie := New[string]()
	trie.Insert("x", "v1")
	trie.Insert("y", "v2")

	all := trie.GetAll()
	assert.Len(t, all, 2)
}

func TestStats(t *testing.T) {
	trie := New[string]()
	trie.Insert("ab", "1")
	trie.Insert("ac", "2")

	s := trie.Stats()
	assert.Greater(t, s.NodeCount, 0)
	assert.Greater(t, s.MaxDepth, 0)
}

func TestSearch_Basic(t *testing.T) {
	trie := New[string]()
	trie.Insert("he", "he")
	trie.Insert("she", "she")
	trie.Insert("his", "his")
	trie.Insert("hers", "hers")

	matches := trie.Search("ushers")
	require.NotNil(t, matches)
	assert.Contains(t, matches, "she")
	assert.Contains(t, matches, "he")
	assert.Contains(t, matches, "hers")
}

func TestSearch_NoOverlap(t *testing.T) {
	trie := New[string]()
	trie.Insert("abc", "abc")

	matches := trie.Search("xyz")
	assert.Nil(t, matches)

	matches = trie.Search("ababc")
	require.NotNil(t, matches)
	assert.Equal(t, "abc", matches[0])
}

func TestSearch_MultipleValues(t *testing.T) {
	trie := New[string]()
	trie.Insert("bad", "badword")
	trie.Insert("bad", "profanity")

	matches := trie.Search("say bad things")
	require.NotNil(t, matches)
	assert.Len(t, matches, 2)
	assert.Contains(t, matches, "badword")
	assert.Contains(t, matches, "profanity")
}

func TestSearch_AutoBuild(t *testing.T) {
	trie := New[string]()
	trie.Insert("test", "test")

	matches := trie.Search("some test here")
	require.NotNil(t, matches)
	assert.Equal(t, "test", matches[0])
}

func TestSearch_AfterInsertAutoRebuild(t *testing.T) {
	trie := New[string]()
	trie.Insert("first", "f")

	matches := trie.Search("first")
	require.NotNil(t, matches)
	assert.Equal(t, "f", matches[0])

	trie.Insert("second", "s")
	// 自动重建
	matches = trie.Search("second")
	require.NotNil(t, matches)
	assert.Equal(t, "s", matches[0])
}

func TestSearch_Chinese(t *testing.T) {
	trie := New[string]()
	trie.Insert("敏感词", "敏感词")
	trie.Insert("违禁", "违禁")

	matches := trie.Search("这是一条包含敏感词的消息")
	require.NotNil(t, matches)
	assert.Contains(t, matches, "敏感词")
	assert.NotContains(t, matches, "违禁")
}
