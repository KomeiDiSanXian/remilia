// Package ai freeze_prefix_test.go — 前缀缓存的冻结用例。
//
// 对应 docs/notes/27-ai-freeze-checklist.md 的 §H 12–13 与负向不变量
// N1/N6：动态上下文只允许影响稳定前缀之后的内容，ToolSet 变化必须产生
// 新的前缀边界。断言的都是请求字节，而不是 Provider 的缓存命中。
package ai

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/runtime"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	"github.com/KomeiDiSanXian/remilia/infra/jsonx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDynamicContextChangeKeepsPrefix 冻结前缀缓存边界（D3/N1/N6/GAP-5）：
// Ephemeral 动态上下文（记忆/历史检索/群聊窗口）只挂在最后一条用户消息尾部，
// 其内容变化不得改变它之前的字节。
//
// 前缀的边界按"第一条上下文通道消息"确定，而不是"除最后一条"：启用原生
// 上下文通道后，环境事实作为独立消息（RoleEnvContext）出现在 user 之前，
// 旧口径会把它算进"稳定前缀"，让断言恒真（见 docs/notes/33 §六）。
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
	// stablePrefix 取"最后一条 user 消息之前"的全部消息：本次请求里唯一允许
	// 随动态内容变化的是尾部那条 user 消息。
	stablePrefix := func(dynamic string) string {
		msgs := build(dynamic)
		cut := len(msgs)
		for i := len(msgs) - 1; i >= 0; i-- {
			if msgs[i].Role == protocol.RoleUser {
				cut = i
				break
			}
		}
		payload, err := jsonx.Marshal(msgs[:cut])
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

// TestEnvContextShiftsSuffixBoundaryOnly 冻结"原生通道下环境事实属于前缀、
// 动态上下文属于后缀"这条分界：
//
//   - env_context 在 user 之前：它的字节必须计入可复用前缀（这是唯一能突破
//     "上一轮问答尾部必然失效"的路径）；
//   - 挂在本轮 user 尾部的动态上下文不属于前缀：它一变，只有最后一条消息变。
func TestEnvContextShiftsSuffixBoundaryOnly(t *testing.T) {
	const payload = `{"date":"2026-11-17","weekday":"星期二"}`
	base := func() []protocol.Message {
		return []protocol.Message{
			{Role: protocol.RoleSystem, Content: "SYSTEM-PROMPT"},
			{Role: protocol.RoleEnvContext, Content: payload},
			{Role: protocol.RoleUser, Content: "本问"},
		}
	}
	stablePrefix := func(msgs []protocol.Message) string {
		cut := len(msgs)
		for i := len(msgs) - 1; i >= 0; i-- {
			if msgs[i].Role == protocol.RoleUser {
				cut = i
				break
			}
		}
		out, err := jsonx.Marshal(msgs[:cut])
		require.NoError(t, err)
		return string(out)
	}

	// 环境事实（前缀）相同、动态上下文（后缀）不同 → 前缀逐字节一致。
	withMemory := runtime.InjectDynamicContext(base(), "===== 长期记忆 =====\n用户喜欢猫")
	withRetrieval := runtime.InjectDynamicContext(base(), "===== 相关历史 =====\n排期讨论")
	assert.Equal(t, stablePrefix(withMemory), stablePrefix(withRetrieval),
		"动态上下文不得影响含环境事实的稳定前缀")

	// 环境事实本身必须在前缀里（否则 append-only 收益无从谈起）。
	assert.Contains(t, stablePrefix(withMemory), "2026-11-17",
		"环境事实属于可复用前缀")

	// 环境事实变化 → 前缀随之变化（这是正确的：它描述不同的时间）。
	other := []protocol.Message{
		{Role: protocol.RoleSystem, Content: "SYSTEM-PROMPT"},
		{Role: protocol.RoleEnvContext, Content: `{"date":"2026-11-18"}`},
		{Role: protocol.RoleUser, Content: "本问"},
	}
	assert.NotEqual(t, stablePrefix(withMemory), stablePrefix(other),
		"环境事实内容变化必须反映到前缀")
}

// TestToolSetChangeCreatesDistinctPrefix 冻结前缀边界（D4/GAP-6）：
// 两个不同 ToolSet 构建的请求前缀字节必须不同；同一 ToolSet 必须稳定。
func TestToolSetChangeCreatesDistinctPrefix(t *testing.T) {
	prefix := func(actions []toolkit.Action) string {
		payload, err := jsonx.Marshal(struct {
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
