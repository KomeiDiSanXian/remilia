package sauce

import (
	"testing"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/mock"
	"github.com/stretchr/testify/assert"
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
	p.beginImageWait(ctx, p.allEngines())
	assert.NotEmpty(t, ctx)
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
