// Package pic send_test.go — sendPicResult 按平台能力回退的发送策略测试。
package pic

import (
	stdctx "context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureEvent 最小化 platform.Event 实现。
type captureEvent struct {
	platformID string
	kind       platform.EventKind
	chat       platform.ChatInfo
}

func (e *captureEvent) Platform() string             { return e.platformID }
func (e *captureEvent) Kind() platform.EventKind     { return e.kind }
func (e *captureEvent) RawType() string              { return "TEST" }
func (e *captureEvent) Segments() []platform.Segment { return nil }
func (e *captureEvent) Chat() platform.ChatInfo      { return e.chat }
func (e *captureEvent) Sender() platform.UserInfo    { return platform.UserInfo{ID: "u1"} }
func (e *captureEvent) Timestamp() time.Time         { return time.Time{} }
func (e *captureEvent) ID() string                   { return "" }
func (e *captureEvent) RawPayload() any              { return nil }

// captureReplySender 记录所有 Send 调用的消息内容。
type captureReplySender struct {
	mu   sync.Mutex
	msgs []platform.OutboundMessage
}

func (s *captureReplySender) Send(_ stdctx.Context, req platform.SendRequest) (platform.SendResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msgs = append(s.msgs, req.Message)
	return platform.SendResult{}, nil
}

// syncDispatcher 同步执行提交的任务。
type syncSendDispatcher struct{}

func (syncSendDispatcher) Submit(_ string, task func(stdctx.Context) error) error {
	if task != nil {
		return task(stdctx.Background())
	}
	return nil
}

// newSendTestContext 构造带指定平台能力的命令上下文。
func newSendTestContext(t *testing.T, caps platform.Capabilities) (*context.Context, *captureReplySender) {
	t.Helper()
	sender := &captureReplySender{}
	evt := &captureEvent{platformID: "test", kind: platform.EventKindGroupMessage,
		chat: platform.ChatInfo{ID: "chat001"}}
	ctx := context.NewContextFromEvent(evt, sender)
	ctx.SetDispatcher(syncSendDispatcher{})
	ctx.SetPlatformCapabilities(caps)
	return ctx, sender
}

func newSendTestPlugin(t *testing.T) (*Plugin, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(testPNG(t, 32, 32))
	}))
	t.Cleanup(srv.Close)
	c := &booruClient{httpClient: srv.Client()}
	return &Plugin{client: c}, srv
}

func newSendTestSite(srv *httptest.Server) site {
	s, _ := findSite("konachan")
	s.Domain = srv.Listener.Addr().String()
	return s
}

// TestSendPicResultMarkdownPath CapMarkdown 平台：每张图一条"图片 + Markdown 作品信息"。
func TestSendPicResultMarkdownPath(t *testing.T) {
	p, srv := newSendTestPlugin(t)
	ctx, sender := newSendTestContext(t, platform.Capabilities{Markdown: true})

	s := newSendTestSite(srv)
	p.sendPicResult(ctx, stdctx.Background(), s, []picPost{{ID: 1, FileURL: srv.URL, SiteName: "Konachan"}})

	require.Len(t, sender.msgs, 1, "应只有一条消息（图与信息同条）")
	msg := sender.msgs[0]
	assert.NotEmpty(t, msg.Markdown, "作品信息应为 Markdown")
	assert.Contains(t, msg.Markdown, "[原图](")
	assert.NotContains(t, msg.Markdown, "1.", "单图消息不应带序号")
	assert.Len(t, msg.Attachments, 1, "图片附件应同条携带")
}

// TestSendPicResultCaptionFallback 无 Markdown 能力时回退图文同发（纯文本）。
func TestSendPicResultCaptionFallback(t *testing.T) {
	p, srv := newSendTestPlugin(t)
	ctx, sender := newSendTestContext(t, platform.Capabilities{Caption: true})

	s := newSendTestSite(srv)
	p.sendPicResult(ctx, stdctx.Background(), s, []picPost{{ID: 1, FileURL: srv.URL, SiteName: "Konachan"}})

	require.Len(t, sender.msgs, 1)
	msg := sender.msgs[0]
	assert.Empty(t, msg.Markdown, "不应使用 Markdown")
	assert.NotEmpty(t, msg.Text)
	assert.Len(t, msg.Attachments, 1)
}

// TestSendPicResultPlainTextFallback 无任何富文本能力：图片 + 纯文本汇总。
func TestSendPicResultPlainTextFallback(t *testing.T) {
	p, srv := newSendTestPlugin(t)
	ctx, sender := newSendTestContext(t, platform.Capabilities{})

	s := newSendTestSite(srv)
	p.sendPicResult(ctx, stdctx.Background(), s, []picPost{{ID: 1, FileURL: srv.URL, SiteName: "Konachan"}})

	require.Len(t, sender.msgs, 2)
	assert.Len(t, sender.msgs[0].Attachments, 1)
	assert.Empty(t, sender.msgs[1].Markdown, "汇总应为纯文本")
	assert.NotEmpty(t, sender.msgs[1].Text)
}

// TestSendPicResultSkipsFailedDownloads 下载失败的图应跳过且不影响其余图片编号。
func TestSendPicResultSkipsFailedDownloads(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer dead.Close()

	p, srv := newSendTestPlugin(t)
	ctx, sender := newSendTestContext(t, platform.Capabilities{Markdown: true})

	s := newSendTestSite(srv)
	posts := []picPost{
		{ID: 1, FileURL: dead.URL + "/gone.png", SiteName: "Konachan"}, // 下载失败
		{ID: 2, FileURL: srv.URL, SiteName: "Konachan"},                // 成功
	}
	p.sendPicResult(ctx, stdctx.Background(), s, posts)

	require.Len(t, sender.msgs, 1, "失败的图应跳过")
	assert.Contains(t, sender.msgs[0].Markdown, "[Konachan]", "仍应包含成功图片的信息")
	assert.Equal(t, "pic_2.png", sender.msgs[0].Attachments[0].Name)
}
