package context

// clone_deadline_test.go — Clone 对 std context 派生语义
//
// 语义：Clone 给出独立的 std context——继承原 context 的 deadline，
// 但不随原 context 的取消而被取消（异步执行时由调用方自行 Cancel）。

import (
	stdctx "context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// TestContextClone_PreservesDeadline 验证克隆继承 deadline，且不受原
// context 取消影响。
func TestContextClone_PreservesDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		stdCtx, cancel := stdctx.WithTimeout(stdctx.Background(), 5*time.Second)
		defer cancel()

		evt := newMockEvent(platform.EventKindPrivateMessage)
		ctx := NewContextFromEvent(evt, noopSender{})
		ctx.SetStdContext(stdCtx)

		cloned := ctx.Clone()

		originalDeadline, ok := ctx.Context().Deadline()
		if !ok {
			t.Fatal("original context should have a deadline")
		}
		clonedDeadline, ok := cloned.Context().Deadline()
		if !ok {
			t.Fatal("cloned context should keep the deadline")
		}
		if !originalDeadline.Equal(clonedDeadline) {
			t.Fatalf("cloned deadline = %v, want %v", clonedDeadline, originalDeadline)
		}

		// 原 context 取消不得影响克隆
		cancel()
		time.Sleep(10 * time.Millisecond)
		select {
		case <-cloned.Context().Done():
			t.Error("cloned context must not be cancelled when the original is")
		default:
		}
	})
}
