package ai

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTodoManager(t *testing.T) {
	m := newTodoManager()
	id1 := m.add("c1", "买牛奶")
	id2 := m.add("c1", "回邮件")
	assert.Equal(t, "T1", id1)
	assert.Equal(t, "T2", id2)

	// 会话隔离
	assert.Empty(t, m.list("c2"))

	// setDone / remove
	assert.True(t, m.setDone("c1", "T1", true))
	assert.False(t, m.setDone("c1", "T99", true))
	assert.True(t, m.remove("c1", "T2"))
	assert.False(t, m.remove("c1", "T2"))
}

func TestTodoTools(t *testing.T) {
	p := &Plugin{todos: newTodoManager()}
	toolCtx := withToolSource(context.Background(), toolSource{chatID: "c1", p: p})

	tools := p.buildTodoTools()
	add := findTestTool(t, tools, todoAddToolName)
	list := findTestTool(t, tools, todoListToolName)
	done := findTestTool(t, tools, todoDoneToolName)
	remove := findTestTool(t, tools, todoRemoveToolName)

	out, err := add.Execute(toolCtx, map[string]any{"item": "买牛奶"})
	require.NoError(t, err)
	assert.Contains(t, out, "T1")
	_, err = add.Execute(toolCtx, map[string]any{"item": "回邮件"})
	require.NoError(t, err)
	_, err = add.Execute(toolCtx, map[string]any{})
	assert.Error(t, err)

	out, err = list.Execute(toolCtx, map[string]any{})
	require.NoError(t, err)
	assert.Contains(t, out, "T1")
	assert.Contains(t, out, "买牛奶")

	out, err = done.Execute(toolCtx, map[string]any{"id": "T1"})
	require.NoError(t, err)
	assert.Contains(t, out, "已完成")

	out, err = list.Execute(toolCtx, map[string]any{"filter": "pending"})
	require.NoError(t, err)
	assert.NotContains(t, out, "买牛奶")

	out, err = remove.Execute(toolCtx, map[string]any{"id": "T2"})
	require.NoError(t, err)
	assert.Contains(t, out, "已删除")

	// 无工具源上下文
	_, err = add.Execute(context.Background(), map[string]any{"item": "x"})
	assert.Error(t, err)
}
