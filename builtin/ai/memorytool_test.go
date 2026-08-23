package ai

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestMemoryToolsPlugin(t *testing.T) *Plugin {
	t.Helper()
	dir := t.TempDir()
	m, err := OpenMemoryStore(dir, 50, time.Minute)
	require.NoError(t, err)
	t.Cleanup(m.Close)
	return &Plugin{memory: m}
}

func TestMemoryTools_AddQueryForget(t *testing.T) {
	p := newTestMemoryToolsPlugin(t)
	toolCtx := withToolSource(context.Background(), toolSource{
		userID: "u1", chatID: "g1", isGroup: true, p: p,
	})

	tools := p.buildMemoryTools()
	add := findTestTool(t, tools, memoryAddToolName)
	query := findTestTool(t, tools, memoryQueryToolName)
	forget := findTestTool(t, tools, memoryForgetToolName)

	// add（默认 user 作用域）
	out, err := add.Execute(toolCtx, map[string]any{"text": "用户喜欢喝冰美式"})
	require.NoError(t, err)
	assert.Contains(t, out, "已记住")
	assert.Contains(t, out, "1 条")

	// add 群作用域
	out, err = add.Execute(toolCtx, map[string]any{"text": "本群每周五团建", "scope": "group"})
	require.NoError(t, err)
	assert.Contains(t, out, "本群记忆")

	// 非群聊用 group 作用域报错
	dmCtx := withToolSource(context.Background(), toolSource{userID: "u1", chatID: "dm", p: p})
	_, err = add.Execute(dmCtx, map[string]any{"text": "x", "scope": "group"})
	assert.Error(t, err)

	// query 命中 user 记忆（关键词重叠：喜欢）
	out, err = query.Execute(toolCtx, map[string]any{"query": "喜欢喝什么"})
	require.NoError(t, err)
	assert.Contains(t, out, "冰美式")

	// query 命中 group 记忆
	out, err = query.Execute(toolCtx, map[string]any{"query": "周五团建"})
	require.NoError(t, err)
	assert.Contains(t, out, "团建")

	// forget 精确匹配
	out, err = forget.Execute(toolCtx, map[string]any{"text": "用户喜欢喝冰美式"})
	require.NoError(t, err)
	assert.Contains(t, out, "已删除")

	out, err = forget.Execute(toolCtx, map[string]any{"text": "不存在的记忆"})
	require.NoError(t, err)
	assert.Contains(t, out, "未找到")
}

func TestMemoryTools_Disabled(t *testing.T) {
	p := &Plugin{} // memory == nil
	toolCtx := withToolSource(context.Background(), toolSource{userID: "u1", chatID: "c", p: p})
	add := findTestTool(t, p.buildMemoryTools(), memoryAddToolName)
	_, err := add.Execute(toolCtx, map[string]any{"text": "x"})
	assert.Error(t, err)
}

func TestMemoryToolLimit(t *testing.T) {
	assert.Equal(t, 5, memoryToolLimit(nil))
	assert.Equal(t, 7, memoryToolLimit(float64(7)))
	assert.Equal(t, 20, memoryToolLimit(float64(999)))
	assert.Equal(t, 5, memoryToolLimit("abc"))
	assert.Equal(t, 3, memoryToolLimit("3"))
}
