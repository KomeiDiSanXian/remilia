package fsm

// session_orphan_cleanup_test.go — 关联 FSM 注销后的遗留会话
//
// 回归背景：会话所属 FSM 被 Unregister 后，遗留会话曾被直接忽略（return false），
// 在未设置 Timeout 时该 sessionID 永久无法开启任何新会话。

import (
	"testing"

	corectx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFSM_OrphanSessionCleanup 验证 FSM 注销后遗留会话不阻塞新会话。
func TestFSM_OrphanSessionCleanup(t *testing.T) {
	eng := NewEngine(nil)

	fsmA := &FSM{
		Name: "orphan_a", Initial: "idle",
		Events: []Event{{
			Name: "go", From: "idle", To: "wait",
			Match: func(ctx *corectx.Context) bool { return ctx.GetMessageContent() == "/a" },
		}},
	}
	require.NoError(t, eng.Register(fsmA))

	ok, err := eng.TryStartSession(newTestContext("/a"), "orphan_chat")
	require.NoError(t, err)
	require.True(t, ok)
	require.NotNil(t, eng.GetSession("orphan_chat"))

	// 注销 FSM 后，遗留会话（引用已不存在的 FSM）不得阻塞该 sessionID 开启新会话
	eng.Unregister("orphan_a")

	fsmB := &FSM{
		Name: "orphan_b", Initial: "idle",
		Events: []Event{{
			Name: "start", From: "idle", To: "",
			Match: func(ctx *corectx.Context) bool { return ctx.GetMessageContent() == "/b" },
		}},
	}
	require.NoError(t, eng.Register(fsmB))

	ok, err = eng.TryStartSession(newTestContext("/b"), "orphan_chat")
	require.NoError(t, err)
	assert.True(t, ok, "orphan session (unregistered FSM) must not block new sessions")
	assert.Nil(t, eng.GetSession("orphan_chat"), "terminal start event should end the session")
}
