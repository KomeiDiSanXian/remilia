package engine

// temp_matcher_expiry_test.go — 临时 matcher 的过期登记与清理
//
// 回归背景：OnTemp 创建的 matcher 之后再调用 SetTempWithTimeout，此前不会
// 补登过期堆——该 matcher 永不过期，会话状态随用户不发消息而长期泄漏。

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// TestTempMatcher_TimeoutAfterOnTempExpires 验证 OnTemp 之后再设超时
// 能正确登记过期堆并在超时后被清理。
func TestTempMatcher_TimeoutAfterOnTempExpires(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		eng := newEngineForTest(t)

		m := eng.OnTemp(string(platform.EventKindPrivateMessage))
		m.Handle(func(ctx *context.Context) error { return nil })
		m.SetTempWithTimeout(50 * time.Millisecond)

		if got := eng.GetTempMatcherCount(); got != 1 {
			t.Fatalf("temp count = %d, want 1", got)
		}

		time.Sleep(100 * time.Millisecond)
		eng.cleanExpiredMatchers()

		if !m.IsDeleted() {
			t.Fatal("temp matcher with timeout set after OnTemp should expire and be marked deleted")
		}
		if got := eng.GetTempMatcherCount(); got != 0 {
			t.Fatalf("temp count after cleanup = %d, want 0", got)
		}
	})
}
