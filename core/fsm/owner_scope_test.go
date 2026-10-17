package fsm

// owner_scope_test.go — OwnerScoped 会话归属校验的回归测试
//
// 回归背景：Router 以 platform:chatID 粒度分配 sessionID，FSM 会话在群聊里
// 全群共享 Data：任意成员都能推进/接管他人的多步流程（例如别人填到一半的
// 表单被另一个成员用一句话改掉）。OwnerScoped 提供内核级的归属校验。

import (
	"testing"

	corectx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOwnerScoped_OnlyOwnerAdvancesSession(t *testing.T) {
	eng := NewEngine(nil)
	require.NoError(t, eng.Register(&FSM{
		Name:        "owned",
		Initial:     "idle",
		OwnerScoped: true,
		Events: []Event{
			{Name: "start", From: "idle", To: "waiting", Match: func(*corectx.Context) bool { return true }},
			{Name: "next", From: "waiting", To: "done", Match: func(*corectx.Context) bool { return true }},
		},
	}))

	owner := ownerTestCtx("user-a")
	ok, err := eng.TryStartSession(owner, "chat-owned")
	require.NoError(t, err)
	require.True(t, ok)

	session := eng.GetSession("chat-owned")
	require.NotNil(t, session)
	require.Equal(t, "user-a", session.OwnerID, "启动者应被记录为归属者")

	// 其它成员：不得推进会话
	other := ownerTestCtx("user-b")
	state, transitioned, err := eng.TryTransition(other, "chat-owned")
	require.NoError(t, err)
	assert.False(t, transitioned, "非归属者不得推进会话")
	assert.Equal(t, State("waiting"), state)

	// 归属者：正常推进
	state, transitioned, err = eng.TryTransition(owner, "chat-owned")
	require.NoError(t, err)
	assert.True(t, transitioned)
	assert.Equal(t, State("done"), state)
}

func TestOwnerScoped_DisabledByDefault(t *testing.T) {
	eng := NewEngine(nil)
	require.NoError(t, eng.Register(&FSM{
		Name:    "shared",
		Initial: "idle",
		Events: []Event{
			{Name: "start", From: "idle", To: "waiting", Match: func(*corectx.Context) bool { return true }},
			{Name: "next", From: "waiting", To: "done", Match: func(*corectx.Context) bool { return true }},
		},
	}))

	require.True(t, mustStart(t, eng, ownerTestCtx("user-a"), "chat-shared"))

	session := eng.GetSession("chat-shared")
	require.NotNil(t, session)
	assert.Empty(t, session.OwnerID, "默认不记录归属者")

	// 既有行为：会话按 sessionID 共享，任何成员都能推进
	_, transitioned, err := eng.TryTransition(ownerTestCtx("user-b"), "chat-shared")
	require.NoError(t, err)
	assert.True(t, transitioned)
}

func mustStart(t *testing.T, eng *Engine, ctx *corectx.Context, sessionID string) bool {
	t.Helper()
	ok, err := eng.TryStartSession(ctx, sessionID)
	require.NoError(t, err)
	return ok
}

// ownerTestCtx 构造指定发送者的上下文（基于 testEvent，设置 sender）。
func ownerTestCtx(senderID string) *corectx.Context {
	evt := &testEvent{
		platform: "test",
		content:  "go",
		chatID:   "chat-owner",
		kind:     platform.EventKindGroupMessage,
	}
	base := corectx.NewContextFromEvent(&senderOverrideEvent{testEvent: evt, senderID: senderID}, &platform.NoopSender{})
	return base
}

// senderOverrideEvent 覆写 testEvent.Sender() 的发送者 ID。
type senderOverrideEvent struct {
	*testEvent
	senderID string
}

func (e *senderOverrideEvent) Sender() platform.UserInfo {
	return platform.UserInfo{ID: e.senderID}
}
