package fsm

// transition_result_test.go — 迁移结果与终态语义
//
// 回归背景：TryTransition 在结束会话的迁移上返回迁移前的 Current，
// 与文档「newState：迁移尝试后的状态」不符，调用方据此会误判会话仍在。

import (
	"testing"

	corectx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFSM_TerminalTransitionReturnsEmptyState 验证结束会话的迁移
// 返回新状态 ""。
func TestFSM_TerminalTransitionReturnsEmptyState(t *testing.T) {
	eng := NewEngine(nil)
	require.NoError(t, eng.Register(&FSM{
		Name:    "terminal",
		Initial: "idle",
		Events: []Event{
			{Name: "go", From: "idle", To: "waiting", Match: func(*corectx.Context) bool { return true }},
			{Name: "end", From: "waiting", To: "", Match: func(*corectx.Context) bool {
				return true
			}},
		},
	}))
	require.NoError(t, eng.StartSession(newTestContext(""), "terminal", "chat-terminal"))

	state, ok, err := eng.TryTransition(newTestContext(""), "chat-terminal")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, State("waiting"), state)

	state, ok, err = eng.TryTransition(newTestContext(""), "chat-terminal")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, State(""), state, "终态迁移后的状态应为空（会话已结束）")
	assert.Nil(t, eng.GetSession("chat-terminal"))
}
