package context_test

// replywithcontext_ctx_test.go — ReplyWithContext 对调用方 context 的回归测试
//
// 回归背景：ReplyWithContext 此前完全忽略传入的 stdCtx：函数体只用 Dispatcher
// 自建的发送 context（baseCtx + SendTimeout），调用方文档承诺的超时/取消控制
// 不存在——调用方以为能提前放弃，实际上发送会一直跑到调度器超时。

import (
	stdctx "context"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cancelAwareSender 阻塞在发送中，直到收到的 context 被取消。
// 兜底 2s 超时：旧实现下发送 context 不带调用方的取消信号，
// 测试会在兜底路径上失败而不是永久挂起。
type cancelAwareSender struct {
	calls chan stdctx.Context
}

func newCancelAwareSender() *cancelAwareSender {
	return &cancelAwareSender{calls: make(chan stdctx.Context, 1)}
}

func (s *cancelAwareSender) Send(c stdctx.Context, _ platform.SendRequest) (platform.SendResult, error) {
	s.calls <- c
	select {
	case <-c.Done():
		return platform.SendResult{}, c.Err()
	case <-time.After(2 * time.Second):
		return platform.SendResult{}, nil
	}
}

func TestReplyWithContext_CallerContextAbortsSend(t *testing.T) {
	event := makeTestEvent("qq", "C2C_MESSAGE_CREATE", "hi", platform.EventKindPrivateMessage)
	sender := newCancelAwareSender()
	ctx := context.NewContextFromEvent(event, sender)
	ctx.SetDispatcher(&testDispatcher{})

	caller, cancel := stdctx.WithTimeout(stdctx.Background(), 30*time.Millisecond)
	defer cancel()

	f := ctx.ReplyWithContext(caller, platform.TextMessage("hello"))

	_, err := f.Wait(stdctx.Background())
	require.Error(t, err, "调用方 context 超时应中止发送")
	assert.ErrorIs(t, err, stdctx.Canceled)

	// 发送确实拿到了带取消信号的 context，且原始原因可读
	got := <-sender.calls
	assert.Error(t, got.Err(), "发送 context 应已随调用方 context 取消")
	assert.ErrorIs(t, stdctx.Cause(got), stdctx.DeadlineExceeded)
}

func TestReplyWithContext_AlreadyCanceledContextSkipsSend(t *testing.T) {
	event := makeTestEvent("qq", "C2C_MESSAGE_CREATE", "hi", platform.EventKindPrivateMessage)
	sender := newCancelAwareSender()
	ctx := context.NewContextFromEvent(event, sender)
	ctx.SetDispatcher(&testDispatcher{})

	caller, cancel := stdctx.WithCancel(stdctx.Background())
	cancel()

	f := ctx.ReplyWithContext(caller, platform.TextMessage("hello"))

	_, err := f.Wait(stdctx.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, stdctx.Canceled)

	select {
	case <-sender.calls:
		t.Error("已取消的调用方 context 不应提交发送")
	default:
	}
}

func TestReplyWithContext_BackgroundContextStillSends(t *testing.T) {
	event := makeTestEvent("qq", "C2C_MESSAGE_CREATE", "hi", platform.EventKindPrivateMessage)
	event.(*testPlatformEvent).chat.ID = "chat-bg"

	type sent struct {
		chatID string
		text   string
	}
	var got *sent
	sender := &captureTestSender{fn: func(chat platform.ChatInfo, m platform.OutboundMessage) {
		got = &sent{chatID: chat.ID, text: m.Text}
	}}
	ctx := context.NewContextFromEvent(event, sender)
	ctx.SetDispatcher(&testDispatcher{})

	f := ctx.ReplyWithContext(stdctx.Background(), platform.TextMessage("pong"))
	_, err := f.Wait(stdctx.Background())

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "chat-bg", got.chatID)
	assert.Equal(t, "pong", got.text)
}
