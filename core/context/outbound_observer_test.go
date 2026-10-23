package context_test

import (
	stdctx "context"
	"sync"
	"testing"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// idSender 返回固定 MessageID 的假发送器。
type idSender struct{}

func (idSender) Send(_ stdctx.Context, _ platform.SendRequest) (platform.SendResult, error) {
	return platform.SendResult{MessageID: "msg-1"}, nil
}

// failingDispatcher 的 Submit 直接失败（模拟入队失败）。
type failingDispatcher struct{}

func (failingDispatcher) Submit(_ string, _ func(stdctx.Context) error) error {
	return stdctx.Canceled
}

type outboundCall struct {
	chatID string
	req    platform.SendRequest
	res    platform.SendResult
	err    error
}

// recordingObserver 记录所有出站回调，供断言。
type recordingObserver struct {
	mu    sync.Mutex
	calls []outboundCall
}

func (o *recordingObserver) OnOutbound(chatID string, req platform.SendRequest, res platform.SendResult, err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.calls = append(o.calls, outboundCall{chatID: chatID, req: req, res: res, err: err})
}

func (o *recordingObserver) len() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.calls)
}

func TestReply_NotifiesOutboundObserver(t *testing.T) {
	event := makeTestEvent("qq", "C2C_MESSAGE_CREATE", "ping", platform.EventKindPrivateMessage)
	event.(*testPlatformEvent).chat.ID = "chat-1"

	obs := &recordingObserver{}
	ctx := context.NewContextFromEvent(event, idSender{})
	ctx.Ext().SetTyped(context.OutboundObserverExt{Observer: obs})
	ctx.SetDispatcher(&testDispatcher{})

	ctx.Reply(platform.TextMessage("pong"))

	require.Equal(t, 1, obs.len())
	c := obs.calls[0]
	assert.Equal(t, "chat-1", c.chatID)
	assert.Equal(t, "pong", c.req.Message.Text)
	assert.Equal(t, "msg-1", c.res.MessageID)
	assert.NoError(t, c.err)
}

func TestReplyWithContext_NotifiesOutboundObserver(t *testing.T) {
	event := makeTestEvent("discord", "MESSAGE_CREATE", "hi", platform.EventKindGroupMessage)
	event.(*testPlatformEvent).chat.ID = "g-9"

	obs := &recordingObserver{}
	ctx := context.NewContextFromEvent(event, idSender{})
	ctx.Ext().SetTyped(context.OutboundObserverExt{Observer: obs})
	ctx.SetDispatcher(&testDispatcher{})

	ctx.ReplyWithContext(stdctx.Background(), platform.MarkdownMessage("**hello**"))

	require.Equal(t, 1, obs.len())
	c := obs.calls[0]
	assert.Equal(t, "g-9", c.chatID)
	assert.Equal(t, "**hello**", c.req.Message.Markdown)
	assert.NoError(t, c.err)
}

func TestReply_NoObserverNoCallback(t *testing.T) {
	event := makeTestEvent("qq", "C2C_MESSAGE_CREATE", "ping", platform.EventKindPrivateMessage)
	event.(*testPlatformEvent).chat.ID = "chat-1"

	ctx := context.NewContextFromEvent(event, idSender{})
	ctx.SetDispatcher(&testDispatcher{})

	// 未注入观察者：不应 panic，也不应有任何回调
	ctx.Reply(platform.TextMessage("pong"))
}

func TestReply_SubmitFailureSkipsObserver(t *testing.T) {
	event := makeTestEvent("qq", "C2C_MESSAGE_CREATE", "ping", platform.EventKindPrivateMessage)
	event.(*testPlatformEvent).chat.ID = "chat-1"

	obs := &recordingObserver{}
	ctx := context.NewContextFromEvent(event, idSender{})
	ctx.Ext().SetTyped(context.OutboundObserverExt{Observer: obs})
	ctx.SetDispatcher(failingDispatcher{})

	ctx.Reply(platform.TextMessage("pong"))
	assert.Equal(t, 0, obs.len(), "observer must not fire when the task is not submitted")
}

func TestReply_ObserverSurvivesClone(t *testing.T) {
	event := makeTestEvent("qq", "C2C_MESSAGE_CREATE", "ping", platform.EventKindPrivateMessage)
	event.(*testPlatformEvent).chat.ID = "chat-1"

	obs := &recordingObserver{}
	ctx := context.NewContextFromEvent(event, idSender{})
	ctx.Ext().SetTyped(context.OutboundObserverExt{Observer: obs})
	ctx.SetDispatcher(&testDispatcher{})

	cloned := ctx.Clone()
	cloned.Reply(platform.TextMessage("from clone"))

	require.Equal(t, 1, obs.len(), "observer must survive context clone (pooled execution)")
}

// TestAddOutboundObserver_FansOut 验证追加的观察者与已注册观察者同时收到回调。
//
// 回归背景：出站观察者扩展是单槽位，messagelog 的中间件注册在发送指标中间件
// 之后、用 SetTyped 直接覆盖，导致 remilia_send_total 永远没有样本。
func TestAddOutboundObserver_FansOut(t *testing.T) {
	event := makeTestEvent("qq", "C2C_MESSAGE_CREATE", "ping", platform.EventKindPrivateMessage)
	event.(*testPlatformEvent).chat.ID = "chat-1"

	first := &recordingObserver{}
	second := &recordingObserver{}
	third := &recordingObserver{}

	ctx := context.NewContextFromEvent(event, idSender{})
	ctx.AddOutboundObserver(first)
	ctx.AddOutboundObserver(second)
	ctx.AddOutboundObserver(third) // 连续追加应保持全部观察者
	ctx.SetDispatcher(&testDispatcher{})

	ctx.Reply(platform.TextMessage("pong"))

	require.Equal(t, 1, first.len(), "已注册观察者不能被后写入者覆盖")
	assert.Equal(t, 1, second.len())
	assert.Equal(t, 1, third.len())
}

// TestAddOutboundObserver_NilIgnored 验证 nil 上下文/观察者为空操作。
func TestAddOutboundObserver_NilIgnored(t *testing.T) {
	var nilCtx *context.Context
	nilCtx.AddOutboundObserver(&recordingObserver{}) // 不应 panic

	event := makeTestEvent("qq", "C2C_MESSAGE_CREATE", "ping", platform.EventKindPrivateMessage)
	ctx := context.NewContextFromEvent(event, idSender{})
	ctx.AddOutboundObserver(nil)
	if _, ok := ctx.Ext().GetTyped[context.OutboundObserverExt](); ok {
		t.Error("nil observer must not be registered")
	}
}
