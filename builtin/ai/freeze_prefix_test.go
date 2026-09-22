// Package ai freeze_prefix_test.go — 前缀缓存的冻结用例。
//
// 对应 docs/notes/27-ai-freeze-checklist.md 的 §H 12–13 与负向不变量
// N1/N6：动态上下文只允许影响稳定前缀之后的内容，ToolSet 变化必须产生
// 新的前缀边界。断言的都是请求字节，而不是 Provider 的缓存命中。
package ai

import (
	"encoding/json"
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/runtime"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDynamicContextChangeKeepsPrefix 冻结前缀缓存边界（D3/N1/N6/GAP-5）：
// 动态上下文（记忆/历史检索/运行时/群聊窗口）只挂在最后一条用户消息尾部，
// 其内容变化不得改变 System + History 组成的稳定前缀字节。
func TestDynamicContextChangeKeepsPrefix(t *testing.T) {
	history := []protocol.Message{
		{Role: protocol.RoleSystem, Content: "SYSTEM-PROMPT"},
		{Role: protocol.RoleUser, Content: "第一问"},
		{Role: protocol.RoleAssistant, Content: "第一答"},
		{Role: protocol.RoleUser, Content: "第二问"},
	}
	const memory = "===== 长期记忆 =====\n用户喜欢猫"
	const retrieval = "===== 相关历史 =====\n上周讨论过排期"

	build := func(dynamic string) []protocol.Message {
		msgs := append([]protocol.Message(nil), history...)
		return runtime.InjectDynamicContext(msgs, dynamic)
	}
	// stablePrefix 取"除最后一条用户消息之外的全部消息"作为稳定前缀。
	stablePrefix := func(dynamic string) string {
		msgs := build(dynamic)
		payload, err := json.Marshal(msgs[:len(msgs)-1])
		require.NoError(t, err)
		return string(payload)
	}

	msgs := build(memory)
	require.Len(t, msgs, len(history))
	assert.Contains(t, msgs[len(msgs)-1].Content, "用户喜欢猫", "动态上下文必须挂在最后一条用户消息上")
	assert.Equal(t, "第一问", msgs[1].Content, "历史消息不得被就地修改")
	assert.Equal(t, "第一答", msgs[2].Content, "历史消息不得被就地修改")

	assert.Equal(t, stablePrefix(memory), stablePrefix(retrieval),
		"动态上下文内容变化不得改变稳定前缀")
	assert.Equal(t, stablePrefix(memory), stablePrefix(""),
		"有无动态上下文都不得改变稳定前缀")
	assert.NotContains(t, stablePrefix(memory), "用户喜欢猫", "动态内容不得进入稳定前缀")
	assert.NotContains(t, stablePrefix(retrieval), "上周讨论过排期", "动态内容不得进入稳定前缀")
}

// TestToolSetChangeCreatesDistinctPrefix 冻结前缀边界（D4/GAP-6）：
// 两个不同 ToolSet 构建的请求前缀字节必须不同；同一 ToolSet 必须稳定。
func TestToolSetChangeCreatesDistinctPrefix(t *testing.T) {
	prefix := func(actions []toolkit.Action) string {
		payload, err := json.Marshal(struct {
			Messages []protocol.Message
			Tools    string
		}{
			Messages: []protocol.Message{{Role: protocol.RoleSystem, Content: "SYSTEM-PROMPT"}, {Role: protocol.RoleUser, Content: "问题"}},
			Tools:    toolsRequestBytes(t, actions),
		})
		require.NoError(t, err)
		return string(payload)
	}

	base := toolSet("alpha")
	grown := toolSet("alpha", "beta")

	assert.Equal(t, prefix(base), prefix(base), "同一 ToolSet 必须产生完全相同的前缀")
	assert.NotEqual(t, prefix(base), prefix(grown), "ToolSet 变化必须产生不同的前缀边界")
}
