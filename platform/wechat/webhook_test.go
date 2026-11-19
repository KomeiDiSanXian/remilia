package wechat

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/jsonx"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// ────────────────────────────────────────────────────────────────────────────
// URL 验证
// ────────────────────────────────────────────────────────────────────────────

func TestWebhookVerifyURL(t *testing.T) {
	crypt := newTestCrypt(t)
	hook := newWebhookAdapter(Config{}, crypt, nil)

	echo, err := crypt.encrypt("echo-plain")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	const ts, nonce = "1409659813", "1372623149"
	sig := crypt.Signature(ts, nonce, echo)

	req := httptest.NewRequest(http.MethodGet,
		"/wechat/aibot?msg_signature="+sig+"&timestamp="+ts+"&nonce="+nonce+"&echostr="+url.QueryEscape(echo), nil)
	rec := httptest.NewRecorder()
	hook.handleCallback(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	// 必须原样返回明文：不加引号、不带换行。
	if got := rec.Body.String(); got != "echo-plain" {
		t.Fatalf("响应 = %q, want echo-plain", got)
	}
}

func TestWebhookVerifyURLBadSignature(t *testing.T) {
	crypt := newTestCrypt(t)
	hook := newWebhookAdapter(Config{}, crypt, nil)

	req := httptest.NewRequest(http.MethodGet,
		"/wechat/aibot?msg_signature=bad&timestamp=1&nonce=2&echostr=x", nil)
	rec := httptest.NewRecorder()
	hook.handleCallback(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// 回调处理
// ────────────────────────────────────────────────────────────────────────────

func TestWebhookReceivesMessage(t *testing.T) {
	crypt := newTestCrypt(t)
	hook := newWebhookAdapter(Config{
		BotID:       "bot",
		EventBuffer: 4,
		WorkerCount: 1,
	}, crypt, nil)

	plain := `{"msgid":"m1","aibotid":"bot","chattype":"single","from":{"userid":"u1"},` +
		`"response_url":"https://qyapi.example.com/response","msgtype":"text","text":{"content":"hi"}}`
	enc, err := crypt.encrypt(plain)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	body, err := jsonx.Marshal(webhookEnvelope{Encrypt: enc})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	const ts, nonce = "1700000000", "nonce-1"
	sig := crypt.Signature(ts, nonce, enc)

	req := httptest.NewRequest(http.MethodPost,
		"/wechat/aibot?msg_signature="+url.QueryEscape(sig)+"&timestamp="+ts+"&nonce="+nonce,
		bytesReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	hook.handleCallback(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	select {
	case evt := <-hook.eventCh:
		if got := platform.Content(evt); got != "hi" {
			t.Fatalf("正文 = %q", got)
		}
		// response_url 必须随事件带出：webhook 模式只有这一条回复通道。
		if got := evt.Chat().Tokens[TokenResponseURL]; got == "" {
			t.Fatalf("缺少 response_url token: %+v", evt.Chat().Tokens)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("超时：事件未入队")
	}
}

func TestWebhookRejectsBadSignature(t *testing.T) {
	crypt := newTestCrypt(t)
	hook := newWebhookAdapter(Config{BotID: "bot"}, crypt, nil)

	body, _ := jsonx.Marshal(webhookEnvelope{Encrypt: "garbage"})
	req := httptest.NewRequest(http.MethodPost,
		"/wechat/aibot?msg_signature=bad&timestamp=1&nonce=2", bytesReader(body))
	rec := httptest.NewRecorder()
	hook.handleCallback(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestWebhookMethodNotAllowed(t *testing.T) {
	hook := newWebhookAdapter(Config{}, newTestCrypt(t), nil)
	req := httptest.NewRequest(http.MethodPut, "/wechat/aibot", nil)
	rec := httptest.NewRecorder()
	hook.handleCallback(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// 媒体解密
// ────────────────────────────────────────────────────────────────────────────

// TestDecryptMedia 用 SDK 侧相同参数验证"IV 取前 16 字节 + 32 字节填充"。
func TestDecryptMedia(t *testing.T) {
	crypt := newTestCrypt(t)
	// 媒体用同一条消息的 aeskey（此处复用同一个 43 位 key 模拟）。
	payload := []byte("PNG-BINARY-CONTENT-0123456789")
	// 按 32 字节块补 PKCS#7 后加密。
	padded := pad(payload, wecomPadBlockSize)
	enc, err := encryptCBC(crypt.key, padded)
	if err != nil {
		t.Fatalf("encryptCBC: %v", err)
	}
	got, err := DecryptMedia(enc, testAESKey)
	if err != nil {
		t.Fatalf("DecryptMedia: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("解密结果 = %q, want %q", got, payload)
	}
}

func TestDecryptMediaEmptyKey(t *testing.T) {
	data := []byte("plain")
	got, err := DecryptMedia(data, "")
	if err != nil {
		t.Fatalf("空 aeskey 应原样返回（未加密资源）: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("应原样返回明文，实际 %q", got)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// 回执匹配
// ────────────────────────────────────────────────────────────────────────────

// TestDispatchMatchesByReqID 回执帧 cmd 为空，只能按 req_id 匹配。
func TestDispatchMatchesByReqID(t *testing.T) {
	c := newWSClient(Config{}, nil)
	ch := make(chan *Frame, 1)
	c.pending["req-1"] = ch

	c.dispatch(Frame{
		Headers: FrameHeaders{ReqID: "req-1"},
		ErrCode: new(0),
	})

	select {
	case f := <-ch:
		if !f.ok() {
			t.Fatal("errcode=0 应视为成功")
		}
	default:
		t.Fatal("按 req_id 未匹配到回执")
	}
}

// TestDispatchRoutesCallbacks 推送帧交给 onEvent（连同帧头的 req_id）。
func TestDispatchRoutesCallbacks(t *testing.T) {
	var gotReqID string
	var gotBody []byte
	c := newWSClient(Config{}, func(reqID string, body jsonx.RawMessage) {
		gotReqID = reqID
		gotBody = body
	})
	c.dispatch(Frame{
		Cmd:     cmdCallback,
		Headers: FrameHeaders{ReqID: "req-9"},
		Body:    jsonx.RawMessage(`{"msgtype":"text"}`),
	})
	if gotReqID != "req-9" {
		t.Fatalf("req_id = %q，未透传给事件", gotReqID)
	}
	if len(gotBody) == 0 {
		t.Fatal("body 未透传")
	}
}

// ────────────────────────────────────────────────────────────────────────────
// response_url 回复
// ────────────────────────────────────────────────────────────────────────────

func TestReplyByResponseURL(t *testing.T) {
	var gotMsgType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			MsgType  string        `json:"msgtype"`
			Markdown *MarkdownBody `json:"markdown"`
		}
		if err := jsonx.Unmarshal([]byte(readAll(r.Body)), &payload); err != nil {
			t.Errorf("解析请求失败: %v", err)
		}
		gotMsgType = payload.MsgType
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	a, err := NewAdapter(Config{
		BotID:          "bot",
		Mode:           ModeWebhook,
		Token:          "t",
		EncodingAESKey: testAESKey,
		Webhook:        WebhookConfig{ListenAddr: ":0"},
	})
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}

	req := platform.SendRequest{
		Target: platform.ChatInfo{
			ID:     "u1",
			Tokens: map[string]string{TokenResponseURL: srv.URL},
		},
		Message: platform.TextMessage("hello"),
	}
	if _, err := a.Sender().Send(t.Context(), req); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotMsgType != "markdown" {
		t.Fatalf("msgtype = %q, want markdown", gotMsgType)
	}
}

// TestReplyByResponseURLNon200 response_url 只能用一次，二次调用必须暴露错误。
func TestReplyByResponseURLNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	a, err := NewAdapter(Config{
		BotID:          "bot",
		Mode:           ModeWebhook,
		Token:          "t",
		EncodingAESKey: testAESKey,
		Webhook:        WebhookConfig{ListenAddr: ":0"},
	})
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	req := platform.SendRequest{
		Target:  platform.ChatInfo{ID: "u1", Tokens: map[string]string{TokenResponseURL: srv.URL}},
		Message: platform.TextMessage("hello"),
	}
	if _, err := a.Sender().Send(t.Context(), req); err == nil {
		t.Fatal("HTTP 400 应报错")
	}
}

// ── 测试工具 ────────────────────────────────────────────────────────────────

// bytesReader 用标准库的 io.Reader（测试里别自造一个错误类型，
// 否则 ReadAll 会把自定义的 io.EOF 当成真实错误）。
func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }

func readAll(r io.Reader) string {
	data, _ := io.ReadAll(r)
	return string(data)
}
