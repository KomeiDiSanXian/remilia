package webhook

import (
	"bytes"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/stretchr/testify/assert"
)

func TestNewWebhook(t *testing.T) {
	info := &dto.BotInfo{ServeAddr: ":0", AppSecret: "secret"}
	wh := NewWebhook(info)
	assert.NotNil(t, wh)
	assert.Equal(t, ":0", wh.Addr())
	assert.Equal(t, 1, cap(wh.eventChan))
}

func TestNewWithBuffer_DefaultsToOne(t *testing.T) {
	info := &dto.BotInfo{ServeAddr: ":0", AppSecret: "secret"}

	conn := NewWithBuffer(info, 0)
	assert.Equal(t, 1, cap(conn.eventChan))

	conn2 := NewWithBuffer(info, -5)
	assert.Equal(t, 1, cap(conn2.eventChan))
}

func TestHandle_InvalidBody(t *testing.T) {
	info := &dto.BotInfo{ServeAddr: ":0", AppSecret: "secret"}
	c := NewWebhook(info)
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

func TestHandleDispatch_Dispatches(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		info := &dto.BotInfo{ServeAddr: ":0", AppSecret: "secret"}
		c := NewWithBuffer(info, 10)
		p := &dto.Payload{Type: dto.C2CMessageCreate, ID: "x", Raw: []byte("{}")}
		go c.handleDispatch(p)
		select {
		case got := <-c.EventStream():
			assert.Equal(t, p, got)
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("timeout waiting for dispatched event")
		}
	})
}

func TestHandleDispatch_ChannelFull_DropsPayload(t *testing.T) {
	info := &dto.BotInfo{ServeAddr: ":0", AppSecret: "secret"}
	c := NewWithBuffer(info, 0) // buffer=1（最小值）

	// 填满 channel
	first := &dto.Payload{Type: dto.C2CMessageCreate, ID: "first", Raw: []byte("{}")}
	c.eventChan <- first

	// 再投递一个，channel 满，应被丢弃
	second := &dto.Payload{Type: dto.C2CMessageCreate, ID: "second", Raw: []byte("{}")}
	c.handleDispatch(second)

	assert.Equal(t, uint64(1), c.GetDroppedEventsCount())
}

func TestHandle_MethodNotAllowed(t *testing.T) {
	info := &dto.BotInfo{ServeAddr: ":0", AppSecret: "secret"}
	c := NewWebhook(info)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rw := httptest.NewRecorder()
	c.Handle(rw, req)

	resp := rw.Result()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
	assert.Equal(t, http.MethodPost, resp.Header.Get("Allow"))
}

func TestHandle_MissingSignatureHeader(t *testing.T) {
	info := &dto.BotInfo{ServeAddr: ":0", AppSecret: "secret"}
	c := NewWebhook(info)

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`))
	rw := httptest.NewRecorder()
	c.Handle(rw, req)

	assert.Equal(t, http.StatusBadRequest, rw.Result().StatusCode)
}

func TestHandle_BodyTooLarge(t *testing.T) {
	info := &dto.BotInfo{ServeAddr: ":0", AppSecret: "secret"}
	c := NewWebhook(info)

	big := bytes.Repeat([]byte("a"), maxWebhookBodyBytes+1)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(big))
	req.Header.Set(HeaderSignature, "x")
	rw := httptest.NewRecorder()
	c.Handle(rw, req)

	assert.Equal(t, http.StatusRequestEntityTooLarge, rw.Result().StatusCode)
}

type testWebHook struct{ *Conn }

func (t *testWebHook) Verify(_ http.Header, _ []byte) (bool, error)  { return true, nil }
func (t *testWebHook) Sign(_ http.Header, _ []byte) ([]byte, error)  { return nil, nil }
func (t *testWebHook) Handle(w http.ResponseWriter, r *http.Request) { t.Conn.Handle(w, r) }
func (t *testWebHook) Addr() string                                  { return t.Conn.Addr() }
func (t *testWebHook) EventStream() <-chan *dto.Payload              { return t.Conn.EventStream() }
