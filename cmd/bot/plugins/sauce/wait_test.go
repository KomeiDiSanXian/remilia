package sauce

import (
	"strings"
	"testing"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/mock"
	"github.com/KomeiDiSanXian/remilia/plugin/plugintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newSauceCtx 构造带事件与 mock sender 的最小 Context。
func newSauceCtx() *eventctx.Context {
	evt := &replyQuoteEvent{
		segments: []platform.Segment{{Type: platform.SegmentText, Text: "/sauce"}},
	}
	return eventctx.NewContextFromEvent(evt, mock.NewSender())
}

func TestCancelOnceSafe(t *testing.T) {
	// 多次调用不应 panic（sync.Once 保证清理只执行一次）
	w := &imageWait{}
	w.cancelOnce(nil, "第一次")
	w.cancelOnce(nil, "第二次")
	w.cancelOnce(nil, "") // 静默清理路径
}

func TestCancelOnceNilMatcherSafe(t *testing.T) {
	w := &imageWait{}
	w.cancelOnce(nil, "msg") // 不应 panic
	w.cancelOnce(nil, "")    // 静默清理
}

func TestImageWaitTimeoutConfig(t *testing.T) {
	p := &Plugin{cfg: &fakeConfig{vals: map[string]any{"image_wait_timeout": time.Duration(30 * time.Second)}}}
	assert.Equal(t, 30*time.Second, p.imageWaitTimeout())

	p2 := &Plugin{}
	assert.Equal(t, 60*time.Second, p2.imageWaitTimeout())
}

func TestImageWaitDisabledRegistry(t *testing.T) {
	// reg 为 nil 时应回退到旧行为（提示必须带图），不 panic
	p := &Plugin{}
	ctx := newSauceCtx()
	p.beginImageWait(ctx, p.allEngines(), false)
	assert.NotEmpty(t, ctx)
}

// TestImageWaitBlocksSubsequentMatchers 验证等待 matcher 在等待窗口内
// 独占消费发起者的下一条消息（优先级提前 + 阻断），阻止 AI 等后续
// matcher 对窗口内消息（尤其是图片）抢先响应。
func TestImageWaitBlocksSubsequentMatchers(t *testing.T) {
	eng := engine.NewEngine(engine.WithNoBackgroundWorkers())
	defer func() { _ = eng.Shutdown(t.Context()) }()
	sctx := plugintest.NewSetupContext("sauce", &plugintest.SetupOptions{Engine: eng})
	defer plugintest.StopSetupContext(sctx)

	api, err := New().Setup(sctx)
	require.NoError(t, err)
	p := api.(*Plugin)

	// 模拟 AI：同事件上的普通 matcher（默认优先级 50），等待窗口内不应命中
	var aiFired bool
	eng.On(string(platform.EventKindPrivateMessage)).Handle(func(c *eventctx.Context) error {
		aiFired = true
		return nil
	})

	sender := mock.NewSender()
	cmdEvt := &replyQuoteEvent{segments: []platform.Segment{{Type: platform.SegmentText, Text: "/sauce"}}}
	p.beginImageWait(eventctx.NewContextFromEvent(cmdEvt, sender), p.allEngines(), false)
	require.Equal(t, 1, eng.GetTempMatcherCount())

	// 等待窗口内发起者发送非图片消息：应由等待 matcher 独占消费并阻断后续 matcher
	textEvt := &replyQuoteEvent{segments: []platform.Segment{{Type: platform.SegmentText, Text: "hello"}}}
	eng.ProcessEvent(eventctx.NewContextFromEvent(textEvt, sender))
	eng.WaitForAsyncHandlers()

	assert.False(t, aiFired, "wait matcher must block subsequent matchers (AI) during the wait window")
	assert.Equal(t, 0, eng.GetTempMatcherCount(), "consumed wait matcher must be removed")

	// 取消提示经 OutboundDispatcher 异步发送，轮询等待落盘到 mock sender
	assert.Eventually(t, func() bool {
		for _, c := range sender.Snapshot() {
			if strings.Contains(c.Msg.Text, "已取消本次搜索") {
				return true
			}
		}
		return false
	}, time.Second, 10*time.Millisecond, "non-image message during wait should trigger cancel notice")
}

func TestSearchTimeoutConfig(t *testing.T) {
	p := &Plugin{cfg: &fakeConfig{vals: map[string]any{"search_timeout": time.Duration(120 * time.Second)}}}
	assert.Equal(t, 120*time.Second, p.searchTimeout())
	assert.Equal(t, 90*time.Second, (&Plugin{}).searchTimeout())
}

func TestIQDBConfigDefaults(t *testing.T) {
	p := &Plugin{}
	assert.Equal(t, 45*time.Second, p.iqdbTimeout())
	assert.Equal(t, 1, p.iqdbRetries())
	assert.Equal(t, 10*time.Second, p.iqdbGrace())

	p2 := &Plugin{cfg: &fakeConfig{vals: map[string]any{
		"iqdb_timeout": time.Duration(30 * time.Second),
		"iqdb_retries": 2,
		"iqdb_grace":   time.Duration(20 * time.Second),
	}}}
	assert.Equal(t, 30*time.Second, p2.iqdbTimeout())
	assert.Equal(t, 2, p2.iqdbRetries())
	assert.Equal(t, 20*time.Second, p2.iqdbGrace())
}
