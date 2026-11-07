package fsm

// session_snapshot_copy_test.go — GetSession 返回副本
//
// 回归背景：GetSession 曾返回存储内的活指针，外部修改会绕过会话锁，
// 与并发迁移构成数据竞争。

import (
	"testing"

	corectx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFSM_GetSessionReturnsCopy 验证修改 GetSession 的返回值不影响存储。
func TestFSM_GetSessionReturnsCopy(t *testing.T) {
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
