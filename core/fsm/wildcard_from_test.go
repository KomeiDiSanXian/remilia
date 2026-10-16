package fsm

// wildcard_from_test.go — From:"*" 通配迁移在存活判定与定义校验中的回归测试
//
// 回归背景：TryTransition/TryStartSession 运行时接受 From:"*"（任意状态迁移），
// 但 canTransitionFrom 只比较字面量 From，Validate 也只认字面量 From 等于
// Initial。于是：
//   - "任意状态 /cancel" 这类 FSM 的活跃会话被判定为卡在终态，用户发一句
//     无关消息就把会话连同 Data 静默删除；
//   - 只有 "*" 启动事件的 FSM 定义会被 Validate 拒绝，无法注册。

import (
	"testing"

	corectx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTryStartSession_WildcardFromKeepsSessionAlive(t *testing.T) {
	eng := NewEngine(nil)

	require.NoError(t, eng.Register(&FSM{
		Name:    "cancelable",
		Initial: "idle",
		Events: []Event{
			{Name: "start", From: "idle", To: "waiting", Match: func(ctx *corectx.Context) bool {
				return ctx.GetMessageContent() == "/start"
			}},
			// 任意状态都能取消：waiting 状态下不存在字面量 From 事件
			{Name: "cancel", From: "*", To: "", Match: func(ctx *corectx.Context) bool {
				return ctx.GetMessageContent() == "/cancel"
			}},
		},
	}))

	ok, err := eng.TryStartSession(newTestContext("/start"), "chat-wild")
	require.NoError(t, err)
	require.True(t, ok, "启动事件应命中")

	session := eng.GetSession("chat-wild")
	require.NotNil(t, session)
	require.Equal(t, State("waiting"), session.Current)

	// 与任何事件都不匹配的无关消息：会话必须保留（旧实现按"卡在终态"删除）
	require.True(t, eng.UpdateSessionData("chat-wild", func(data map[string]any) {
		data["draft"] = "keep-me"
	}))
	ok, err = eng.TryStartSession(newTestContext("hello"), "chat-wild")
	require.NoError(t, err)
	assert.False(t, ok)

	session = eng.GetSession("chat-wild")
	require.NotNil(t, session, "通配 From 存在时会话不应被清理")
	assert.Equal(t, State("waiting"), session.Current)
	assert.Equal(t, "keep-me", session.Data["draft"])

	// 通配迁移仍可正常结束会话
	_, transitioned, err := eng.TryTransition(newTestContext("/cancel"), "chat-wild")
	require.NoError(t, err)
	assert.True(t, transitioned)
	assert.Nil(t, eng.GetSession("chat-wild"), "To 为空的通配事件应结束会话")
}

func TestValidate_AcceptsWildcardStartEvent(t *testing.T) {
	f := &FSM{
		Name:    "wild-start",
		Initial: "idle",
		Events: []Event{
			{Name: "go", From: "*", To: "running", Match: func(ctx *corectx.Context) bool {
				return ctx.GetMessageContent() == "go"
			}},
		},
	}

	require.NoError(t, f.Validate(), "只有通配启动事件的 FSM 应通过校验")

	eng := NewEngine(nil)
	require.NoError(t, eng.Register(f), "运行时同样接受通配启动事件")

	ok, err := eng.TryStartSession(newTestContext("go"), "chat-wild-start")
	require.NoError(t, err)
	require.True(t, ok)

	session := eng.GetSession("chat-wild-start")
	require.NotNil(t, session)
	assert.Equal(t, State("running"), session.Current)
}
