package webhook

import (
	"context"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

// dummy webhook impl for Verify/Sign used by Conn methods if needed

func TestNewWebhook_NoBigCacheFatal(t *testing.T) {
	// We cannot easily trigger bigcache failure here, but NewWebhook should succeed and allow basic methods
	ctx := context.Background()
	info := &dto.BotInfo{ServeAddr: ":0", AppSecret: "secret"}
	wh := NewWebhook(ctx, info)
	assert.NotNil(t, wh)
	assert.Equal(t, ":0", wh.Addr())
}

func TestHandle_InvalidBody(t *testing.T) {
	ctx := context.Background()
	info := &dto.BotInfo{ServeAddr: ":0", AppSecret: "secret"}
	c := NewWebhook(ctx, info)
	adapter := &testWebHook{Conn: c}

	var body []byte // empty body will cause JSON unmarshal error
	req := httptest.NewRequest(http.MethodPost, "/", http.NoBody)
	// Prepare signature headers to pass Verify
	req.Header.Set(HeaderTimestamp, time.Now().UTC().Format(time.RFC3339Nano))
	sign, err := c.Sign(req.Header, body)
	assert.NoError(t, err)
	req.Header.Set(HeaderSignature, hex.EncodeToString(sign))

	rw := httptest.NewRecorder()
	http.HandlerFunc(adapter.Handle).ServeHTTP(rw, req)
	resp := rw.Result()
	// empty body leads to unmarshal error -> BadRequest
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestHandleDispatch_NoCache_Dispatches(t *testing.T) {
	ctx := context.Background()
	info := &dto.BotInfo{ServeAddr: ":0", AppSecret: "secret"}
	c := NewWebhook(ctx, info)
	c.bigCache = nil // force nil cache
	p := &dto.Payload{Type: dto.C2CMessageCreate, ID: "x", Raw: []byte("{}")}
	// read from channel non-blocking
	go c.handleDispatch(p)
	select {
	case got := <-c.EventStream():
		assert.Equal(t, p, got)
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timeout waiting for dispatched event")
	}
}

type testWebHook struct{ *Conn }

func (t *testWebHook) Verify(_ http.Header, _ []byte) (bool, error)  { return true, nil }
func (t *testWebHook) Sign(_ http.Header, _ []byte) ([]byte, error)  { return nil, nil }
func (t *testWebHook) Handle(w http.ResponseWriter, r *http.Request) { t.Conn.Handle(w, r) }
func (t *testWebHook) Addr() string                                  { return t.Conn.Addr() }
func (t *testWebHook) EventStream() <-chan *dto.Payload              { return t.Conn.EventStream() }
