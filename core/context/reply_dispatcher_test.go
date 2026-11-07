package context

// reply_dispatcher_test.go — 出站发送的前置条件错误与观察者并发
//
// 回归背景：
//   - Reply 在 dispatcher 缺失时报 ErrNoPlatformSender，与"缺 sender"
//     混用会让排查方向完全跑偏；
//   - AddOutboundObserver 是「读-改-写」，并发追加会丢观察者。

import (
	stdctx "context"
	"sync"
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// noopSender 是丢弃发送请求的假发送器。
type noopSender struct{}

func (noopSender) Send(stdctx.Context, platform.SendRequest) (platform.SendResult, error) {
	return platform.SendResult{}, nil
}

// countingObserver 是只统计调用次数的观察者。
type countingObserver struct{ id int }

func (o *countingObserver) OnOutbound(string, platform.SendRequest, platform.SendResult, error) {}

// TestContext_ReplyWithoutDispatcherUsesDedicatedError 验证缺 dispatcher 与
// 缺 sender 的错误不同（两者的修复动作完全不同）。
func TestContext_ReplyWithoutDispatcherUsesDedicatedError(t *testing.T) {
	ctx := NewContextFromEvent(newMockEvent(platform.EventKindPrivateMessage), noopSender{})

	f := ctx.Reply(platform.TextMessage("hi"))
	_, err := f.Wait(stdctx.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoDispatcher)
	assert.NotErrorIs(t, err, ErrNoPlatformSender,
		"缺 dispatcher 不应报成缺 sender")
}

// TestContext_AddOutboundObserverConcurrent 验证并发追加观察者不丢回调。
func TestContext_AddOutboundObserverConcurrent(t *testing.T) {
	ctx := newTestCtx()

	const n = 64
	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func(id int) {
			defer wg.Done()
			ctx.AddOutboundObserver(&countingObserver{id: id})
		}(i)
	}
	wg.Wait()

	ext, ok := ctx.Ext().GetTyped[OutboundObserverExt]()
	require.True(t, ok)
	multi, ok := ext.Observer.(OutboundObservers)
	require.True(t, ok, "多次追加应合并为 OutboundObservers")
	assert.Len(t, multi, n, "并发追加不得丢失观察者")
}
