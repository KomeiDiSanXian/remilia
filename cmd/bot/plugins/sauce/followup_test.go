package sauce

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestWaitIQDBOutcomeWithinGrace 宽限期内收到 IQDB 结果。
func TestWaitIQDBOutcomeWithinGrace(t *testing.T) {
	ch := make(chan engineOutcome, 1)
	ch <- engineOutcome{name: "IQDB", results: []SearchResult{{Title: "x"}}}

	reqCtx := t.Context()

	res, ok := waitIQDBOutcome(ch, reqCtx, 5*time.Second)
	assert.True(t, ok)
	assert.Equal(t, "IQDB", res.name)
}

// TestWaitIQDBOutcomeGraceExpired 宽限期结束仍未返回 → (_, false)。
func TestWaitIQDBOutcomeGraceExpired(t *testing.T) {
	ch := make(chan engineOutcome, 1) // 永不投递
	reqCtx := t.Context()

	start := time.Now()
	res, ok := waitIQDBOutcome(ch, reqCtx, 30*time.Millisecond)
	assert.False(t, ok)
	assert.GreaterOrEqual(t, time.Since(start), 25*time.Millisecond)
	_ = res
}

// TestWaitIQDBOutcomeCtxCanceled 整体超时（reqCtx 取消）→ 返回超时错误。
func TestWaitIQDBOutcomeCtxCanceled(t *testing.T) {
	ch := make(chan engineOutcome, 1)
	reqCtx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	res, ok := waitIQDBOutcome(ch, reqCtx, 5*time.Second)
	assert.True(t, ok)
	assert.Equal(t, "IQDB", res.name)
	assert.Error(t, res.err)
}

// TestSendIQDBFollowUpProcessesResult 补发 goroutine 能正常消费结果并
// 返回（不 panic、不阻塞），即使 ctx 无 dispatcher（Reply 以 Future 错误
// 形式解析而非崩溃）。
func TestSendIQDBFollowUpProcessesResult(t *testing.T) {
	p := &Plugin{}
	ctx := newSauceCtx()
	ch := make(chan engineOutcome, 1)
	ch <- engineOutcome{name: "IQDB", results: []SearchResult{{Title: "x"}}}
	reqCtx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		p.sendIQDBFollowUp(ctx, ch, reqCtx)
		close(done)
	}()
	select {
	case <-done:
		cancel()
	case <-time.After(2 * time.Second):
		t.Fatal("sendIQDBFollowUp 未在超时前返回")
	}
}

// TestSendIQDBFollowUpError 补发错误结果时正常返回。
func TestSendIQDBFollowUpError(t *testing.T) {
	p := &Plugin{}
	ctx := newSauceCtx()
	ch := make(chan engineOutcome, 1)
	ch <- engineOutcome{name: "IQDB", err: &iqdbQueuedError{position: 999}}
	reqCtx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		p.sendIQDBFollowUp(ctx, ch, reqCtx)
		close(done)
	}()
	select {
	case <-done:
		cancel()
	case <-time.After(2 * time.Second):
		t.Fatal("sendIQDBFollowUp 未在超时前返回")
	}
}

// TestSendIQDBFollowUpCtxDone 补发时整体超时 → 提示超时后返回。
func TestSendIQDBFollowUpCtxDone(t *testing.T) {
	p := &Plugin{}
	ctx := newSauceCtx()
	ch := make(chan engineOutcome, 1) // 永不投递
	reqCtx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	done := make(chan struct{})
	go func() {
		p.sendIQDBFollowUp(ctx, ch, reqCtx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("sendIQDBFollowUp 未在超时前返回")
	}
}

// TestIsTimeoutLike 验证超时类错误识别。
func TestIsTimeoutLike(t *testing.T) {
	assert.False(t, isTimeoutLike(nil))
	assert.False(t, isTimeoutLike(errors.New("boom")))
	assert.False(t, isTimeoutLike(fmt.Errorf("wrap: %w", errors.New("x"))))
	assert.True(t, isTimeoutLike(context.DeadlineExceeded))
	assert.True(t, isTimeoutLike(fmt.Errorf("wrap: %w", context.DeadlineExceeded)))
	// net.Error.Timeout() 实现
	nt := &netTimeoutErr{}
	assert.True(t, isTimeoutLike(nt))
}

// netTimeoutErr 模拟 net.Error 且 Timeout()=true。
type netTimeoutErr struct{}

func (e *netTimeoutErr) Error() string   { return "i/o timeout" }
func (e *netTimeoutErr) Timeout() bool   { return true }
func (e *netTimeoutErr) Temporary() bool { return false }
