package fsm

// list_sessions_snapshot_test.go — ListSessions 快照与并发迁移的回归测试
//
// 回归背景：MemoryStorage.List 只浅拷贝 *Session，Data 仍是存储里的活 map；
// ListSessions 也只是值拷贝。管理 API 轮询（ListSessions）与 TryTransition
// 并发时既读到撕裂的会话字段，也会在读写同一张 Data map 时触发数据竞争
// （Go 语义下即 concurrent map read and map write）。

import (
	"sync"
	"testing"

	corectx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListSessions_ReturnsDeepSnapshot(t *testing.T) {
	eng := NewEngine(nil)
	require.NoError(t, eng.Register(&FSM{
		Name:    "snap",
		Initial: "idle",
		Events: []Event{
			{Name: "go", From: "idle", To: "waiting", Match: func(*corectx.Context) bool { return true }},
		},
	}))

	require.NoError(t, eng.StartSession(newTestContext(""), "snap", "chat-snap"))
	require.True(t, eng.UpdateSessionData("chat-snap", func(data map[string]any) {
		data["k"] = "v"
	}))
	// 迁移一次，确保快照读取的是迁移后的状态
	_, transitioned, err := eng.TryTransition(newTestContext(""), "chat-snap")
	require.NoError(t, err)
	require.True(t, transitioned)

	sessions := eng.ListSessions()
	require.Len(t, sessions, 1)
	assert.Equal(t, "v", sessions[0].Data["k"])

	// 修改快照不得影响存储
	sessions[0].Current = "tampered"
	sessions[0].Data["k"] = "tampered"

	live := eng.GetSession("chat-snap")
	require.NotNil(t, live)
	assert.Equal(t, State("waiting"), live.Current, "快照修改不得回写实时会话")
	assert.Equal(t, "v", live.Data["k"], "快照的 Data 必须是副本")
}

func TestListSessions_ConcurrentWithTransitions(t *testing.T) {
	eng := NewEngine(nil)
	require.NoError(t, eng.Register(&FSM{
		Name:    "race",
		Initial: "idle",
		Events: []Event{
			{Name: "go", From: "idle", To: "waiting", Match: func(*corectx.Context) bool { return true }},
			{Name: "back", From: "waiting", To: "idle", Match: func(*corectx.Context) bool { return true }},
		},
	}))

	require.NoError(t, eng.StartSession(newTestContext(""), "race", "chat-race"))

	var wg sync.WaitGroup
	wg.Add(2)
	// 迁移持续改写 Current/UpdatedAt/Data
	go func() {
		defer wg.Done()
		for i := range 300 {
			_, _, _ = eng.TryTransition(newTestContext("go"), "chat-race")
			_ = eng.UpdateSessionData("chat-race", func(data map[string]any) {
				data["n"] = i
			})
		}
	}()
	// 管理轮询读取快照
	go func() {
		defer wg.Done()
		for range 300 {
			for _, s := range eng.ListSessions() {
				_ = s.Data["n"]
				_ = s.Current
			}
		}
	}()
	wg.Wait()
}
