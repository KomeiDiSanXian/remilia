package fsm

// start_order_test.go — FSM 启动顺序与注册键一致性的回归测试
//
// 回归背景：
//   - TryStartSession 快照 FSM 列表时遍历 map：多个 FSM 匹配同一消息时的
//     胜者随机（每次调用都可能不同），先启动者对同名 sessionID 形成"随机饥饿"；
//   - Manager 以 desc.Name 为 key、Engine 以 FSM.Name 为索引键，两者不一致时
//     Unregister(desc.Name) 返回成功但 FSM 仍在拦截消息。

import (
	"testing"

	corectx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTryStartSession_DeterministicRegistrationOrder(t *testing.T) {
	eng := NewEngine(nil)

	// 两个 FSM 都匹配同一条消息：WinsFirst 先注册
	require.NoError(t, eng.Register(&FSM{
		Name:    "wins-first",
		Initial: "idle",
		Events: []Event{
			{Name: "go", From: "idle", To: "running", Match: func(*corectx.Context) bool { return true }},
		},
	}))
	require.NoError(t, eng.Register(&FSM{
		Name:    "loses-second",
		Initial: "idle",
		Events: []Event{
			{Name: "go", From: "idle", To: "running", Match: func(*corectx.Context) bool { return true }},
		},
	}))

	for i := 0; i < 20; i++ {
		sessionID := "chat-order-" + string(rune('a'+i))
		ok, err := eng.TryStartSession(newTestContext("go"), sessionID)
		require.NoError(t, err)
		require.True(t, ok)
		session := eng.GetSession(sessionID)
		require.NotNil(t, session)
		assert.Equal(t, "wins-first", session.FSMName, "胜者必须是先注册的 FSM")
		assert.Nil(t, eng.GetSession("chat-order-2"), "同一 sessionID 只应被一个 FSM 接管")
	}

	// 注销先注册者后，后者接管（顺序随之变化）
	eng.Unregister("wins-first")
	ok, err := eng.TryStartSession(newTestContext("go"), "chat-order-next")
	require.NoError(t, err)
	require.True(t, ok)
	session := eng.GetSession("chat-order-next")
	require.NotNil(t, session)
	assert.Equal(t, "loses-second", session.FSMName)
}

func TestManager_RejectsDescriptorNameMismatch(t *testing.T) {
	m := NewManager(nil)

	desc := &FSMDescriptor{
		Name: "descriptor-name",
		FSM: &FSM{
			Name:    "fsm-name",
			Initial: "idle",
			Events: []Event{
				{Name: "go", From: "idle", To: "running", Match: func(*corectx.Context) bool { return true }},
			},
		},
	}

	err := m.Register(desc)
	require.Error(t, err, "描述符名与 FSM 名不一致必须被拒绝")
	assert.Contains(t, err.Error(), "must equal")

	// 一致时可以注册，且注销真正生效
	desc.FSM.Name = "descriptor-name"
	require.NoError(t, m.Register(desc))
	require.NoError(t, m.Unregister("descriptor-name"))
	assert.Nil(t, m.GetEngine().GetFSM("descriptor-name"), "注销必须真的移除 FSM")
}
