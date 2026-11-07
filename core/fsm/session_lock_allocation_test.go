package fsm

// session_lock_allocation_test.go — 会话锁的复用与分配
//
// 回归背景：sessionLock 每次调用都求值 &sync.Mutex{}（参数在查找前求值），
// 即使命中缓存也会为每次迁移分配一把锁。

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestFSM_SessionLockReused 验证同一 sessionID 总是拿到同一把锁，
// 且稳态下取锁零分配。
func TestFSM_SessionLockReused(t *testing.T) {
	eng := NewEngine(nil)

	first := eng.sessionLock("chat-lock")
	second := eng.sessionLock("chat-lock")
	assert.Same(t, first, second, "同一 sessionID 必须复用同一把会话锁")

	allocs := testing.AllocsPerRun(100, func() {
		_ = eng.sessionLock("chat-lock")
	})
	assert.Zero(t, allocs, "稳态下取会话锁不应产生分配")
}
