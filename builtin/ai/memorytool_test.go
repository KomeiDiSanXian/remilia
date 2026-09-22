package ai

import (
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/catalog"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
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

func TestMemoryTools_AddForget(t *testing.T) {
	p := newTestMemoryToolsPlugin(t)
	toolCtx := toolCtxForTest(p, toolkit.ToolSource{
		UserID: "u1", ChatID: "g1", IsGroup: true,
	})

	tools := catalog.BuildMemoryTools()
	add := findTestTool(t, tools, catalog.MemoryAddToolName)
	forget := findTestTool(t, tools, catalog.MemoryForgetToolName)

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
	dmCtx := toolCtxForTest(p, toolkit.ToolSource{UserID: "u1", ChatID: "dm"})
	_, err = add.Execute(dmCtx, map[string]any{"text": "x", "scope": "group"})
	assert.Error(t, err)

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
	toolCtx := toolCtxForTest(p, toolkit.ToolSource{UserID: "u1", ChatID: "c"})
	add := findTestTool(t, catalog.BuildMemoryTools(), catalog.MemoryAddToolName)
	_, err := add.Execute(toolCtx, map[string]any{"text": "x"})
	assert.Error(t, err)
}

// TestMemoryQueryIsRetrievalNotAction 冻结 memory_query 的降级：记忆检索不再
// 是模型可见动作（工具集恰为 add/forget），而由上下文管线按本轮用户消息完成——
// 用户显式问"你还记得…"时，事实仍会经注入进入模型视野。
func TestMemoryQueryIsRetrievalNotAction(t *testing.T) {
	p := newTestMemoryToolsPlugin(t)

	names := make([]string, 0, 3)
	for _, tool := range catalog.BuildMemoryTools() {
		names = append(names, tool.Name)
	}
	assert.ElementsMatch(t, []string{catalog.MemoryAddToolName, catalog.MemoryForgetToolName}, names,
		"记忆检索不应再作为动作暴露给模型")

	// 显式记忆检索请求：事实经 Context 注入，而不是工具返回值。
	p.cfg = &config.Config{MemoryInjectMax: 8}
	p.memory.Add(userScope("u1"), "用户上次说服务器方案选型定在 B 方案")

	sess := &session.Session{ID: "s1", UserID: "u1", ChatID: "g1"}
	sess.Messages = []protocol.Message{{Role: protocol.RoleUser, Content: "你还记得我之前说的服务器方案吗"}}
	evt := platform.NewSyntheticEvent("c2c", "你还记得我之前说的服务器方案吗",
		platform.WithSyntheticSender(platform.UserInfo{ID: "u1", DisplayName: "小明"}),
		platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true}),
	)
	injected := p.buildMemoryContextN(eventctx.NewContextFromEvent(evt, nil), sess, p.cfg.MemoryInjectMax)
	assert.Contains(t, injected, "服务器方案选型定在 B 方案",
		"显式检索请求应由上下文管线注入相关事实")
}
