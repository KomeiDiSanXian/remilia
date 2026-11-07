package fsm

// session_sliding_ttl_test.go — 滑动 TTL（RefreshOnActivity）
//
// 语义：Timeout 默认自会话创建起计（固定 TTL）；开启 RefreshOnActivity 后
// 每次成功迁移都把过期时间重置为 now+Timeout，长对话保持活跃就不会过期。

import (
	"testing"
	"testing/synctest"
	"time"

	corectx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFSM_RefreshOnActivitySlidingTTL 验证活跃会话不会因固定 TTL 过期，
// 停止活跃后正常过期。
func TestFSM_RefreshOnActivitySlidingTTL(t *testing.T) {
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
