package telegram

import (
	stdctx "context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// TestWebhookPathFromURL 固化 public_url → 接收路径的推导规则。
func TestWebhookPathFromURL(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{"https://bot.example.com/telegram", "/telegram"},
		{"https://bot.example.com/", "/"},
		{"https://bot.example.com", ""},
		{"", ""},
		{"://bad url", ""},
	}
	for _, tt := range tests {
		if got := webhookPathFromURL(tt.raw); got != tt.want {
			t.Errorf("webhookPathFromURL(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
}

// TestWebhookConfig_SetDefaults 检查默认值与路径补前导斜杠。
func TestWebhookConfig_SetDefaults(t *testing.T) {
	cfg := WebhookConfig{PublicURL: "https://bot.example.com/hook", ListenAddr: ":8443"}
	cfg.setDefaults()

	if cfg.Path != "/hook" {
		t.Errorf("Path = %q, want /hook", cfg.Path)
	}
	if cfg.EventBuffer != defaultWebhookBuffer {
		t.Errorf("EventBuffer = %d, want %d", cfg.EventBuffer, defaultWebhookBuffer)
	}
	if cfg.WorkerCount != defaultWebhookWorkers {
		t.Errorf("WorkerCount = %d, want %d", cfg.WorkerCount, defaultWebhookWorkers)
	}
	if cfg.ShutdownTimeout != defaultWebhookShutdownTimeout {
		t.Errorf("ShutdownTimeout = %v, want %v", cfg.ShutdownTimeout, defaultWebhookShutdownTimeout)
	}

	// 无路径的 public_url 回落到默认路径；显式 path 补齐前导斜杠。
	fallback := WebhookConfig{PublicURL: "https://bot.example.com"}
	fallback.setDefaults()
	if fallback.Path != defaultWebhookPath {
		t.Errorf("fallback Path = %q, want %q", fallback.Path, defaultWebhookPath)
	}

	explicit := WebhookConfig{Path: "hook"}
	explicit.setDefaults()
	if explicit.Path != "/hook" {
		t.Errorf("explicit Path = %q, want /hook", explicit.Path)
	}
}

// newTestWebhookAdapter 构造只用于 handleUpdate 的最小适配器（不联网）。
func newTestWebhookAdapter(buffer int, secret string) *WebhookAdapter {
	return &WebhookAdapter{
		cfg:     WebhookConfig{SecretToken: secret, Path: "/telegram"},
		botUser: &User{ID: 1, FirstName: "tgbot", Username: "tgbot"},
		eventCh: make(chan platform.Event, buffer),
	}
}

func telegramUpdateBody() string {
	return `{"update_id":1,"message":{"message_id":7,"date":1700000000,` +
		`"chat":{"id":42,"type":"private"},"from":{"id":9,"is_bot":false,"first_name":"u"},` +
		`"text":"hello"}}`
}

// TestWebhookAdapter_HandleUpdate_Auth 校验 secret_token：缺失/错误必须 401，
// 正确时 200 且事件入队。
func TestWebhookAdapter_HandleUpdate_Auth(t *testing.T) {
	a := newTestWebhookAdapter(4, "s3cret")

	post := func(secret string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/telegram", strings.NewReader(telegramUpdateBody()))
		if secret != "" {
			req.Header.Set(webhookHeaderSecretToken, secret)
		}
		rec := httptest.NewRecorder()
		a.handleUpdate(rec, req)
		return rec
	}

	if rec := post(""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing secret: code = %d, want 401", rec.Code)
	}
	if rec := post("wrong"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong secret: code = %d, want 401", rec.Code)
	}
	if rec := post("s3cret"); rec.Code != http.StatusOK {
		t.Fatalf("valid secret: code = %d, want 200", rec.Code)
	}
	if got := len(a.eventCh); got != 1 {
		t.Fatalf("queued events = %d, want 1", got)
	}
}

// TestWebhookAdapter_HandleUpdate_QueueFull 队列满时返回 503 让 Telegram 重投，
// 而不是回 200 静默丢消息。
func TestWebhookAdapter_HandleUpdate_QueueFull(t *testing.T) {
	a := newTestWebhookAdapter(0, "")

	req := httptest.NewRequest(http.MethodPost, "/telegram", strings.NewReader(telegramUpdateBody()))
	rec := httptest.NewRecorder()
	a.handleUpdate(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d, want 503", rec.Code)
	}
}

// TestWebhookAdapter_HandleUpdate_RejectsWrongMethod 只接受 POST。
func TestWebhookAdapter_HandleUpdate_RejectsWrongMethod(t *testing.T) {
	a := newTestWebhookAdapter(1, "")

	req := httptest.NewRequest(http.MethodGet, "/telegram", nil)
	rec := httptest.NewRecorder()
	a.handleUpdate(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("code = %d, want 405", rec.Code)
	}
}

// TestNewWebhookAdapter_Validation 构造参数校验（此路径不触发网络）。
func TestNewWebhookAdapter_Validation(t *testing.T) {
	if _, err := NewWebhookAdapter(Config{}); err == nil {
		t.Fatal("expected error for empty token")
	}
	if _, err := NewWebhookAdapter(Config{Token: "t"}); err == nil {
		t.Fatal("expected error for missing webhook config")
	}
	if _, err := NewWebhookAdapter(Config{Token: "t", Webhook: &WebhookConfig{PublicURL: "https://x"}}); err == nil {
		t.Fatal("expected error for missing listen_addr")
	}
	if _, err := NewWebhookAdapter(Config{Token: "t", Webhook: &WebhookConfig{ListenAddr: ":8443"}}); err == nil {
		t.Fatal("expected error for missing public_url")
	}
}

// TestWebhookAdapter_StopWithoutStart 未启动时 Stop 应安全返回。
func TestWebhookAdapter_StopWithoutStart(t *testing.T) {
	a := newTestWebhookAdapter(1, "")
	ctx, cancel := stdctx.WithTimeout(stdctx.Background(), time.Second)
	defer cancel()
	if err := a.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if a.IsRunning() {
		t.Fatal("adapter must not report running")
	}
}
