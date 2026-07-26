package fsm

// fsm_review_test.go — 2026-07 core 复查修复的回归测试：
//   1. 关联 FSM 被 Unregister 后，遗留会话不再永久阻塞该 sessionID
//   2. GetSession 返回副本而非存储内的活指针

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	corectx "github.com/KomeiDiSanXian/remilia/core/context"
)

func TestReviewFix_OrphanSessionCleanup(t *testing.T) {
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

// TestReviewFix_RefreshOnActivitySlidingTTL 验证 RefreshOnActivity 滑动 TTL：
// 持续活跃的会话不会因"自创建起计"的固定 TTL 过期，停止活跃后正常过期。
func TestReviewFix_RefreshOnActivitySlidingTTL(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		eng := NewEngine(nil)
		f := &FSM{
			Name: "slide", Initial: "a",
			Timeout:           2 * time.Second,
			RefreshOnActivity: true,
			Events: []Event{{
				Name: "tick", From: "a", To: "a",
				Match: func(ctx *corectx.Context) bool { return ctx.GetMessageContent() == "tick" },
			}},
		}
		require.NoError(t, eng.Register(f))
		require.NoError(t, eng.StartSession(newTestContext("x"), "slide", "sess_slide"))

		// 每 1s 活跃一次，累计 3s（超过固定 TTL 的 2s），滑动续期应保持会话存活
		for range 3 {
			time.Sleep(1 * time.Second)
			_, ok, err := eng.TryTransition(newTestContext("tick"), "sess_slide")
			require.NoError(t, err)
			require.True(t, ok, "active session should keep transitioning")
		}
		require.NotNil(t, eng.GetSession("sess_slide"), "sliding TTL must keep an active session alive")

		// 停止活跃：超过 Timeout 后过期
		time.Sleep(3 * time.Second)
		assert.Nil(t, eng.GetSession("sess_slide"), "idle session should expire after Timeout")
	})
}

func TestReviewFix_GetSessionReturnsCopy(t *testing.T) {
	eng := NewEngine(nil)
	f := &FSM{
		Name: "copyfsm", Initial: "idle",
		Events: []Event{{
			Name: "go", From: "idle", To: "next",
			Match: func(ctx *corectx.Context) bool { return true },
		}},
	}
	require.NoError(t, eng.Register(f))
	require.NoError(t, eng.StartSession(newTestContext("x"), "copyfsm", "sess_copy"))

	s1 := eng.GetSession("sess_copy")
	require.NotNil(t, s1)
	s1.Data["injected"] = "value"
	s1.Current = "hacked"

	s2 := eng.GetSession("sess_copy")
	require.NotNil(t, s2)
	assert.NotContains(t, s2.Data, "injected", "GetSession must return a copy of Data")
	assert.Equal(t, State("idle"), s2.Current, "GetSession must return a copy, not the live pointer")
}
