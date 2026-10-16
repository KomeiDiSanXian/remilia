package command

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Registry.Upsert 更新路径回归测试。
//
// 回归背景：Upsert 更新已存在的命令时用 Trie.Insert 追加新元数据，
// 而 ExactMatch/Lookup 取的是第一个值、GetAll 会把两份都交出来，
// 结果是更新不可见、List/Complete 出现重复项、Unregister 删不干净。

// TestRegistry_UpsertReplacesStaleMetadata 验证更新后读到的就是新元数据。
func TestRegistry_UpsertReplacesStaleMetadata(t *testing.T) {
	reg := NewCommandRegistry()
	require.NoError(t, reg.Register(&Definition{Name: "/cmd", Description: "old"}))

	reg.Upsert(&Definition{Name: "/cmd", Description: "new"}, RegisterOptions{Category: "tools"})

	meta, ok := reg.Lookup("/cmd")
	require.True(t, ok)
	assert.Equal(t, "new", meta.Description)
	assert.Equal(t, "tools", meta.Category)

	// 不出现重复项
	assert.Len(t, reg.List(), 1)
	require.Len(t, reg.Complete("/cmd"), 1)
}

// TestRegistry_UpsertKeepsCallStatistics 验证更新不会丢失调用统计。
func TestRegistry_UpsertKeepsCallStatistics(t *testing.T) {
	reg := NewCommandRegistry()
	require.NoError(t, reg.Register(&Definition{Name: "/cmd"}))

	first, ok := reg.Lookup("/cmd")
	require.True(t, ok)
	assert.Equal(t, int64(1), first.GetCallCount())

	reg.Upsert(&Definition{Name: "/cmd", Description: "updated"}, RegisterOptions{})

	second, ok := reg.Lookup("/cmd")
	require.True(t, ok)
	assert.Equal(t, int64(2), second.GetCallCount())
}

// TestRegistry_UpsertThenUnregister 验证多次 Upsert 后仍能彻底注销。
func TestRegistry_UpsertThenUnregister(t *testing.T) {
	reg := NewCommandRegistry()
	require.NoError(t, reg.Register(&Definition{Name: "/cmd", Aliases: []string{"/c"}}))
	reg.Upsert(&Definition{Name: "/cmd", Aliases: []string{"/c"}}, RegisterOptions{})
	reg.Upsert(&Definition{Name: "/cmd", Description: "updated"}, RegisterOptions{})

	require.NoError(t, reg.Unregister("/cmd"))

	_, ok := reg.Lookup("/cmd")
	assert.False(t, ok)
	_, ok = reg.Lookup("/c")
	assert.False(t, ok)
	assert.Empty(t, reg.List())
	assert.Nil(t, reg.Complete("/cmd"))
}

// TestRegistry_UpsertInvalidPatternIsIgnored 验证非法正则不会 panic，
// 只是被忽略（与 RegisterWithOptions/RegisterBatch 的容错口径一致）。
func TestRegistry_UpsertInvalidPatternIsIgnored(t *testing.T) {
	reg := NewCommandRegistry()

	assert.NotPanics(t, func() {
		reg.Upsert(&Definition{Name: "/bad"}, RegisterOptions{Pattern: "("})
	})

	meta, ok := reg.Lookup("/bad")
	require.True(t, ok)
	assert.Nil(t, meta.pattern)
	assert.Empty(t, reg.LookupByPattern("bad"))
}

// TestRegistry_UpsertUpdatesPattern 验证更新时提供合法正则会替换旧模式。
func TestRegistry_UpsertUpdatesPattern(t *testing.T) {
	reg := NewCommandRegistry()
	require.NoError(t, reg.Register(&Definition{Name: "/cmd"}))

	reg.Upsert(&Definition{Name: "/cmd"}, RegisterOptions{Pattern: "^/cmd\\b"})

	matches := reg.LookupByPattern("/cmd now")
	require.Len(t, matches, 1)
	assert.Equal(t, "/cmd", matches[0].Name)
}
