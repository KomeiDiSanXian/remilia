// envcontext_test.go — 环境事实的编码、放置与载荷契约。
//
// 这里冻结的是"硬约束"：各端点的位置要求不同，一旦放错位置请求会被 400
// 拒绝。因此每条形状都要有一条断言，而不是只测 happy path。
package runtime

import (
	"strings"
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// envFactsPayload 构造一份标准的存储载荷（JSON）。
func envFactsPayload(t *testing.T) string {
	t.Helper()
	return protocol.MarshalEnvFacts(protocol.EnvFacts{
		Date:     "2026-11-17",
		Weekday:  "星期二",
		TimeZone: "Asia/Shanghai",
		Offset:   "+08:00",
		Platform: "qq",
		Language: "中文",
	})
}

// envHistory 构造 [env, user, assistant, env, user] 的历史（规范形态：
// env 在它所属的 user 之前）。MidSystem 的换位置场景用它的前 5 条。
func envHistory(payload string) []protocol.Message {
	return []protocol.Message{
		{Role: protocol.RoleSystem, Content: "sys"},
		{Role: protocol.RoleEnvContext, Content: payload},
		{Role: protocol.RoleUser, Content: "第一问"},
		{Role: protocol.RoleAssistant, Content: "第一答"},
		{Role: protocol.RoleEnvContext, Content: payload},
		{Role: protocol.RoleUser, Content: "第二问"},
	}
}

// TestEncodeEnvContextMidSystemAcrossTwoTurns 验证多轮历史里每条 env 都
// 紧跟自己的 user（且都在 user 之后），不破坏前面的历史。
func TestEncodeEnvContextMidSystemAcrossTwoTurns(t *testing.T) {
	payload := envFactsPayload(t)
	got := EncodeEnvContext(envHistory(payload), protocol.NativeContextMidSystem)
	require.Len(t, got, 6)

	assert.Equal(t, protocol.RoleSystem, got[0].Role)
	assert.Equal(t, protocol.RoleUser, got[1].Role)
	assert.Equal(t, "第一问", got[1].Content)
	assert.Equal(t, protocol.RoleSystemMid, got[2].Role, "第一条 env 挪到它的 user 之后")
	assert.Equal(t, protocol.RoleAssistant, got[3].Role)
	assert.Equal(t, protocol.RoleUser, got[4].Role)
	assert.Equal(t, "第二问", got[4].Content)
	assert.Equal(t, protocol.RoleSystemMid, got[5].Role, "第二条 env 同样挪到 user 之后")
}

// TestEncodeEnvContextDeveloperBeforeUser 冻结 developer 形状的放置与正文：
// 保持在 user 之前，正文是 <environment> 指令块。
func TestEncodeEnvContextDeveloperBeforeUser(t *testing.T) {
	msgs := []protocol.Message{
		{Role: protocol.RoleEnvContext, Content: envFactsPayload(t)},
		{Role: protocol.RoleUser, Content: "问题"},
	}

	got := EncodeEnvContext(msgs, protocol.NativeContextDeveloper)
	require.Len(t, got, 2)
	assert.Equal(t, protocol.RoleDeveloper, got[0].Role, "env 必须紧跟 user 之前")
	assert.Equal(t, protocol.RoleUser, got[1].Role)
	assert.Contains(t, got[0].Content, "<environment>")
	assert.Contains(t, got[0].Content, "2026-11-17")
	assert.Contains(t, got[0].Content, "平台: qq", "平台标识作为普通正文保留（没有字段级校验）")
}

// TestEncodeEnvContextFallsBackOnEmptyPayload 冻结"载荷渲染不出正文时折进
// user 文本"，而不是丢掉事实。
func TestEncodeEnvContextFallsBackOnEmptyPayload(t *testing.T) {
	msgs := []protocol.Message{
		{Role: protocol.RoleSystem, Content: "sys"},
		{Role: protocol.RoleEnvContext, Content: "   "},
		{Role: protocol.RoleUser, Content: "问题"},
	}

	for _, shape := range []protocol.NativeContext{
		protocol.NativeContextDeveloper, protocol.NativeContextMidSystem,
	} {
		got := EncodeEnvContext(msgs, shape)
		require.Len(t, got, 2, "空载荷不得产生原生消息 (shape=%v)", shape)
		assert.Equal(t, protocol.RoleUser, got[1].Role)
		assert.True(t, strings.HasSuffix(got[1].Content, "问题"), "用户原话保持在后")
		for _, m := range got {
			assert.NotEqual(t, protocol.RoleDeveloper, m.Role)
			assert.NotEqual(t, protocol.RoleSystemMid, m.Role)
		}
	}
}

func TestEncodeEnvContextMidSystemMovesBehindUser(t *testing.T) {
	msgs := []protocol.Message{
		{Role: protocol.RoleSystem, Content: "sys"},
		{Role: protocol.RoleUser, Content: "第一问"},
		{Role: protocol.RoleAssistant, Content: "第一答"},
		{Role: protocol.RoleEnvContext, Content: envFactsPayload(t)},
		{Role: protocol.RoleUser, Content: "第二问"},
	}

	got := EncodeEnvContext(msgs, protocol.NativeContextMidSystem)
	require.Len(t, got, 5)

	// system 必须在 user 之后：Anthropic 不允许 system 后面跟 user。
	assert.Equal(t, protocol.RoleUser, got[3].Role)
	assert.Equal(t, "第二问", got[3].Content)
	assert.Equal(t, protocol.RoleSystemMid, got[4].Role, "中途 system 必须挪到 user 之后")
	assert.Contains(t, got[4].Content, "<environment>")

	// 前面的历史不受影响。
	assert.Equal(t, "sys", got[0].Content)
	assert.Equal(t, "第一问", got[1].Content)
}

func TestEncodeEnvContextNoneMergesIntoUser(t *testing.T) {
	msgs := []protocol.Message{
		{Role: protocol.RoleSystem, Content: "sys"},
		{Role: protocol.RoleEnvContext, Content: envFactsPayload(t)},
		{Role: protocol.RoleUser, Content: "问题"},
	}

	got := EncodeEnvContext(msgs, protocol.NativeContextNone)
	require.Len(t, got, 2, "兜底形态不产生独立消息")
	assert.Equal(t, protocol.RoleSystem, got[0].Role)
	assert.Equal(t, protocol.RoleUser, got[1].Role)
	assert.Contains(t, got[1].Content, "===== 运行时上下文 =====")
	assert.Contains(t, got[1].Content, "2026-11-17")
	assert.True(t, strings.HasSuffix(got[1].Content, "问题"), "事实必须前置，用户原话保持在后")
}

func TestEncodeEnvContextNoneMergesIntoMultimodalUser(t *testing.T) {
	msgs := []protocol.Message{
		{Role: protocol.RoleEnvContext, Content: envFactsPayload(t)},
		{Role: protocol.RoleUser, ContentParts: []protocol.ContentPart{
			{Type: protocol.ContentPartText, Text: "看图"},
			{Type: protocol.ContentPartImage, Data: []byte("img")},
		}},
	}
	orig := append([]protocol.ContentPart(nil), msgs[1].ContentParts...)

	got := EncodeEnvContext(msgs, protocol.NativeContextNone)
	require.Len(t, got, 1)
	parts := got[0].ContentParts
	require.Len(t, parts, 3, "事实作为首个 text part 前置")
	assert.Equal(t, protocol.ContentPartText, parts[0].Type)
	assert.Contains(t, parts[0].Text, "运行时上下文")
	assert.Equal(t, "看图", parts[1].Text)
	assert.Equal(t, protocol.ContentPartImage, parts[2].Type)

	assert.Equal(t, orig, msgs[1].ContentParts, "不得修改入参的 parts")
}

func TestEncodeEnvContextDropsOrphanEnv(t *testing.T) {
	// env 后面不是 user（历史被裁剪过的异常残留）：必须丢弃，
	// 否则中途 system 的位置约束会被违反。
	msgs := []protocol.Message{
		{Role: protocol.RoleUser, Content: "问"},
		{Role: protocol.RoleEnvContext, Content: envFactsPayload(t)},
		{Role: protocol.RoleAssistant, Content: "答"},
	}
	for _, shape := range []protocol.NativeContext{
		protocol.NativeContextDeveloper,
		protocol.NativeContextMidSystem,
		protocol.NativeContextNone,
	} {
		got := EncodeEnvContext(msgs, shape)
		assert.Len(t, got, 2, "orphan env 必须丢弃 (shape=%v)", shape)
		for _, m := range got {
			assert.NotEqual(t, protocol.RoleEnvContext, m.Role)
		}
	}
}

// TestEncodeEnvContextMidSystemAfterToolResult 验证工具轮里的放置：
// env 只在它所属的 user 之前出现，编码后必须落在该 user 之后、assistant 之前，
// 绝不能夹在 assistant(tool_use) 与 tool_result 之间（那样会被 400 拒绝）。
func TestEncodeEnvContextMidSystemAfterToolResult(t *testing.T) {
	payload := envFactsPayload(t)
	msgs := []protocol.Message{
		{Role: protocol.RoleSystem, Content: "sys"},
		{Role: protocol.RoleEnvContext, Content: payload},
		{Role: protocol.RoleUser, Content: "查天气"},
		{Role: protocol.RoleAssistant, ToolCalls: []protocol.ToolCall{{ID: "t1", Name: "get_weather"}}},
		{Role: protocol.RoleTool, ToolCallID: "t1", Content: "晴"},
	}

	got := EncodeEnvContext(msgs, protocol.NativeContextMidSystem)
	require.Len(t, got, 5)

	// user → system_mid → assistant(tool_use) → tool_result
	assert.Equal(t, protocol.RoleUser, got[1].Role)
	assert.Equal(t, "查天气", got[1].Content)
	assert.Equal(t, protocol.RoleSystemMid, got[2].Role, "env 紧跟它所属的 user 之后")
	assert.Equal(t, protocol.RoleAssistant, got[3].Role)
	assert.Equal(t, protocol.RoleTool, got[4].Role)

	// 关键不变量：assistant(tool_use) 与其 tool_result 之间不得插入 system。
	for i, m := range got {
		if m.Role != protocol.RoleSystemMid {
			continue
		}
		require.Less(t, i+1, len(got))
		assert.NotEqual(t, protocol.RoleTool, got[i+1].Role,
			"中途 system 不得夹在 tool_use 与 tool_result 之间")
	}
}

// TestEncodeEnvContextDeveloperBeforeToolResult 验证 developer 形状不破坏
// 工具轮的紧邻关系：env 只在 user 之前，工具轮的消息顺序不变。
func TestEncodeEnvContextDeveloperBeforeToolResult(t *testing.T) {
	msgs := []protocol.Message{
		{Role: protocol.RoleEnvContext, Content: envFactsPayload(t)},
		{Role: protocol.RoleUser, Content: "查天气"},
		{Role: protocol.RoleAssistant, ToolCalls: []protocol.ToolCall{{ID: "t1", Name: "get_weather"}}},
		{Role: protocol.RoleTool, ToolCallID: "t1", Content: "晴"},
	}
	got := EncodeEnvContext(msgs, protocol.NativeContextDeveloper)
	require.Len(t, got, 4)
	assert.Equal(t, protocol.RoleDeveloper, got[0].Role)
	assert.Equal(t, protocol.RoleUser, got[1].Role)
	assert.Equal(t, protocol.RoleAssistant, got[2].Role)
	assert.Equal(t, protocol.RoleTool, got[3].Role)
}

// TestEncodeEnvContextNoopWithoutEnv 验证无环境事实时原样返回（不开销）。
func TestEncodeEnvContextNoopWithoutEnv(t *testing.T) {
	msgs := []protocol.Message{
		{Role: protocol.RoleSystem, Content: "sys"},
		{Role: protocol.RoleUser, Content: "问"},
	}
	got := EncodeEnvContext(msgs, protocol.NativeContextDeveloper)
	assert.Equal(t, msgs, got)
}

// TestDropEnvContext 验证隐私开关的实现：环境事实整条删除，
// 不像 None 形状那样折进 user 文本（那仍然是一次注入）。
func TestDropEnvContext(t *testing.T) {
	payload := envFactsPayload(t)
	msgs := []protocol.Message{
		{Role: protocol.RoleSystem, Content: "sys"},
		{Role: protocol.RoleEnvContext, Content: payload},
		{Role: protocol.RoleUser, Content: "第一问"},
		{Role: protocol.RoleAssistant, Content: "第一答"},
		{Role: protocol.RoleEnvContext, Content: payload},
		{Role: protocol.RoleUser, Content: "第二问"},
	}

	got := DropEnvContext(msgs)
	require.Len(t, got, 4)
	for _, m := range got {
		assert.NotEqual(t, protocol.RoleEnvContext, m.Role)
	}
	assert.Equal(t, "第一问", got[1].Content, "user 原话不得被改写")
	assert.Equal(t, "第二问", got[3].Content)

	// 无 env 时原样返回（不分配新切片）。
	plain := []protocol.Message{{Role: protocol.RoleUser, Content: "问"}}
	assert.Equal(t, plain, DropEnvContext(plain))
}
