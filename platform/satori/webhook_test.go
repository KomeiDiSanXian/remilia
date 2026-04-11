package satori

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// ─── NewWebhookAdapter / DefaultWebhookConfig ─────────────────────────────────

func TestDefaultWebhookConfig(t *testing.T) {
	cfg := DefaultWebhookConfig(":8080", "chronocat", "123456")
	if cfg.ListenAddr != ":8080" {
		t.Errorf("ListenAddr: got %q", cfg.ListenAddr)
	}
	if cfg.Platform != "chronocat" {
		t.Errorf("Platform: got %q", cfg.Platform)
	}
	if cfg.UserID != "123456" {
		t.Errorf("UserID: got %q", cfg.UserID)
	}
	if cfg.Path != "/satori/webhook" {
		t.Errorf("Path: got %q, want /satori/webhook", cfg.Path)
	}
	if cfg.EventBufferSize != 256 {
		t.Errorf("EventBufferSize: got %d, want 256", cfg.EventBufferSize)
	}
}

func TestNewWebhookAdapter_DefaultPath(t *testing.T) {
	cfg := WebhookConfig{ListenAddr: ":9000", Platform: "p", UserID: "u"}
	a := NewWebhookAdapter(cfg)
	if a.cfg.Path != "/satori/webhook" {
		t.Errorf("default path: got %q", a.cfg.Path)
	}
}

func TestNewWebhookAdapter_DefaultBufferSize(t *testing.T) {
	cfg := WebhookConfig{ListenAddr: ":9000", Platform: "p", UserID: "u"}
	a := NewWebhookAdapter(cfg)
	if a.cfg.EventBufferSize != 256 {
		t.Errorf("default buffer size: got %d", a.cfg.EventBufferSize)
	}
}

func TestWebhookAdapter_Platform(t *testing.T) {
	a := NewWebhookAdapter(DefaultWebhookConfig(":8080", "myplatform", "u"))
	if a.Platform() != "myplatform" {
		t.Errorf("Platform: got %q", a.Platform())
	}
}

func TestWebhookAdapter_PlatformFallback(t *testing.T) {
	cfg := WebhookConfig{ListenAddr: ":8080", UserID: "u"} // no Platform
	a := NewWebhookAdapter(cfg)
	if a.Platform() != PlatformID {
		t.Errorf("Platform fallback: got %q, want %q", a.Platform(), PlatformID)
	}
}

func TestWebhookAdapter_SenderNil(t *testing.T) {
	a := NewWebhookAdapter(DefaultWebhookConfig(":8080", "p", "u"))
	// No WithSendConfig → should return NoopSender (not nil)
	s := a.Sender()
	if s == nil {
		t.Error("Sender should return NoopSender when not configured, not nil")
	}
	if _, ok := s.(*platform.NoopSender); !ok {
		t.Errorf("Sender without config: expected *platform.NoopSender, got %T", s)
	}
}

func TestWebhookAdapter_Capabilities_NoSendConfig(t *testing.T) {
	a := NewWebhookAdapter(DefaultWebhookConfig(":8080", "p", "u"))
	caps := a.Capabilities()
	if caps.MessageEdit {
		t.Error("MessageEdit should be false without send config")
	}
	if caps.MessageDelete {
		t.Error("MessageDelete should be false without send config")
	}
	if !caps.GuildSupport {
		t.Error("GuildSupport should always be true")
	}
	if !caps.MentionAll {
		t.Error("MentionAll should always be true")
	}
}

func TestWebhookAdapter_IsRunning_Initial(t *testing.T) {
	a := NewWebhookAdapter(DefaultWebhookConfig(":8080", "p", "u"))
	if a.IsRunning() {
		t.Error("IsRunning should be false initially")
	}
}

func TestWebhookAdapter_ClientNil(t *testing.T) {
	a := NewWebhookAdapter(DefaultWebhookConfig(":8080", "p", "u"))
	if a.Client() != nil {
		t.Error("Client should be nil without WithSendConfig")
	}
}

func TestWebhookAdapter_ProxyURLs_InitiallyEmpty(t *testing.T) {
	a := NewWebhookAdapter(DefaultWebhookConfig(":8080", "p", "u"))
	if urls := a.ProxyURLs(); len(urls) != 0 {
		t.Errorf("ProxyURLs should be nil initially, got %v", urls)
	}
}

// ─── HTTP Handler: EVENT ──────────────────────────────────────────────────────

func makeWebhookRequest(t *testing.T, op Opcode, body any, token string) *http.Request {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/satori/webhook", bytes.NewReader(data))
	req.Header.Set("Satori-Opcode", strconv.Itoa(int(op)))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req
}

func TestWebhookHTTPHandler_Event_Dispatched(t *testing.T) {
	a := NewWebhookAdapter(DefaultWebhookConfig(":8080", "testplatform", "u"))

	var received platform.Event
	handler := func(e platform.Event) { received = e }
	h := a.makeHTTPHandler(handler)

	evt := Event{
		SN:        1,
		Type:      EventTypeMessageCreated,
		Timestamp: 1_700_000_000_000,
		Channel:   &Channel{ID: "ch-1", Type: ChannelTypeText},
		User:      &User{ID: "u-1", Name: new("Alice")},
		Message:   &Message{ID: "msg-1", Content: new("hello")},
	}
	req := makeWebhookRequest(t, OpcodeEvent, evt, "")
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("HTTP status: got %d, want 200", w.Code)
	}
	if received == nil {
		t.Fatal("handler was not called")
	}
	if received.Platform() != "testplatform" {
		t.Errorf("Event.Platform: got %q", received.Platform())
	}
	if received.Kind() != platform.EventKindGroupMessage {
		t.Errorf("Event.Kind: got %q", received.Kind())
	}
}

func TestWebhookHTTPHandler_Event_MethodNotAllowed(t *testing.T) {
	a := NewWebhookAdapter(DefaultWebhookConfig(":8080", "p", "u"))
	h := a.makeHTTPHandler(nil)

	req := httptest.NewRequest(http.MethodGet, "/satori/webhook", nil)
	req.Header.Set("Satori-Opcode", "0")
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET should be 405, got %d", w.Code)
	}
}

func TestWebhookHTTPHandler_Event_InvalidOpcode(t *testing.T) {
	a := NewWebhookAdapter(DefaultWebhookConfig(":8080", "p", "u"))
	h := a.makeHTTPHandler(nil)

	req := httptest.NewRequest(http.MethodPost, "/satori/webhook", bytes.NewReader([]byte("{}")))
	req.Header.Set("Satori-Opcode", "notanumber")
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("invalid opcode should be 400, got %d", w.Code)
	}
}

func TestWebhookHTTPHandler_Event_UnsupportedOpcode(t *testing.T) {
	a := NewWebhookAdapter(DefaultWebhookConfig(":8080", "p", "u"))
	h := a.makeHTTPHandler(nil)

	// OpcodePing (1) is not supported in webhook mode
	req := makeWebhookRequest(t, OpcodePing, nil, "")
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("unsupported opcode: got %d, want 400", w.Code)
	}
}

func TestWebhookHTTPHandler_Event_InvalidEventBody(t *testing.T) {
	a := NewWebhookAdapter(DefaultWebhookConfig(":8080", "p", "u"))
	h := a.makeHTTPHandler(nil)

	req := httptest.NewRequest(http.MethodPost, "/satori/webhook",
		bytes.NewReader([]byte("not-json")))
	req.Header.Set("Satori-Opcode", strconv.Itoa(int(OpcodeEvent)))
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("invalid event body: got %d, want 400", w.Code)
	}
}

// ─── HTTP Handler: META ───────────────────────────────────────────────────────

func TestWebhookHTTPHandler_Meta_UpdatesProxyURLs(t *testing.T) {
	a := NewWebhookAdapter(DefaultWebhookConfig(":8080", "p", "u"))
	h := a.makeHTTPHandler(nil)

	meta := MetaBody{ProxyURLs: []string{"https://proxy1.example.com", "https://proxy2.example.com"}}
	req := makeWebhookRequest(t, OpcodeMeta, meta, "")
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("META status: got %d, want 200", w.Code)
	}
	urls := a.ProxyURLs()
	if len(urls) != 2 {
		t.Fatalf("ProxyURLs: expected 2, got %d", len(urls))
	}
	if urls[0] != "https://proxy1.example.com" {
		t.Errorf("ProxyURLs[0]: got %q", urls[0])
	}
}

func TestWebhookHTTPHandler_Meta_InvalidBody_ReturnsOK(t *testing.T) {
	// parse failure on META should still return 200 (don't disrupt SDK)
	a := NewWebhookAdapter(DefaultWebhookConfig(":8080", "p", "u"))
	h := a.makeHTTPHandler(nil)

	req := httptest.NewRequest(http.MethodPost, "/satori/webhook",
		bytes.NewReader([]byte("not-json")))
	req.Header.Set("Satori-Opcode", strconv.Itoa(int(OpcodeMeta)))
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("META invalid body: got %d, want 200", w.Code)
	}
}

// ─── HTTP Handler: Token auth ────────────────────────────────────────────────

func TestWebhookHTTPHandler_Auth_OK(t *testing.T) {
	cfg := DefaultWebhookConfig(":8080", "p", "u")
	cfg.Token = "secret"
	a := NewWebhookAdapter(cfg)

	called := false
	h := a.makeHTTPHandler(func(platform.Event) { called = true })

	evt := Event{
		SN:        1,
		Type:      EventTypeMessageCreated,
		Timestamp: 1_000_000,
		Channel:   &Channel{ID: "ch", Type: ChannelTypeText},
	}
	req := makeWebhookRequest(t, OpcodeEvent, evt, "secret")
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("auth OK: got %d, want 200", w.Code)
	}
	if !called {
		t.Error("handler should have been called with valid token")
	}
}

func TestWebhookHTTPHandler_Auth_Unauthorized(t *testing.T) {
	cfg := DefaultWebhookConfig(":8080", "p", "u")
	cfg.Token = "secret"
	a := NewWebhookAdapter(cfg)

	h := a.makeHTTPHandler(func(platform.Event) { t.Error("handler should not be called") })

	evt := Event{SN: 1, Type: EventTypeMessageCreated, Timestamp: 1_000_000}
	req := makeWebhookRequest(t, OpcodeEvent, evt, "wrong-token")
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("wrong token: got %d, want 401", w.Code)
	}
}

func TestWebhookHTTPHandler_Auth_NoToken_NoCheck(t *testing.T) {
	// When no token is configured, any request should pass
	a := NewWebhookAdapter(DefaultWebhookConfig(":8080", "p", "u"))
	called := false
	h := a.makeHTTPHandler(func(platform.Event) { called = true })

	evt := Event{
		SN:        1,
		Type:      EventTypeMessageCreated,
		Timestamp: 1_000_000,
		Channel:   &Channel{ID: "ch", Type: ChannelTypeText},
	}
	req := makeWebhookRequest(t, OpcodeEvent, evt, "") // no token
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("no auth: got %d, want 200", w.Code)
	}
	if !called {
		t.Error("handler should be called when no token is configured")
	}
}

// ─── EmptyBody reader edge case ──────────────────────────────────────────────

func TestWebhookHTTPHandler_Event_EmptyBody(t *testing.T) {
	a := NewWebhookAdapter(DefaultWebhookConfig(":8080", "p", "u"))
	h := a.makeHTTPHandler(nil)

	req := httptest.NewRequest(http.MethodPost, "/satori/webhook", io.NopCloser(bytes.NewReader(nil)))
	req.Header.Set("Satori-Opcode", strconv.Itoa(int(OpcodeEvent)))
	w := httptest.NewRecorder()
	h(w, req)

	// Empty body is invalid JSON for an Event
	if w.Code != http.StatusBadRequest {
		t.Errorf("empty body: got %d, want 400", w.Code)
	}
}
