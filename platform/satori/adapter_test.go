package satori

import (
	"testing"
)

// ─── NewAdapter ───────────────────────────────────────────────────────────────

func TestNewAdapter_InvalidConfig(t *testing.T) {
	_, err := NewAdapter(Config{}) // missing ServerURL, Platform, UserID
	if err == nil {
		t.Error("NewAdapter with empty config: expected error, got nil")
	}
}

func TestNewAdapter_OK(t *testing.T) {
	cfg := DefaultConfig("http://localhost:5140", "chronocat", "123456")
	a, err := NewAdapter(cfg)
	if err != nil {
		t.Fatalf("NewAdapter: unexpected error: %v", err)
	}
	if a == nil {
		t.Fatal("NewAdapter: returned nil")
	}
}

// ─── Platform ─────────────────────────────────────────────────────────────────

func TestAdapter_Platform_FromConfig(t *testing.T) {
	cfg := DefaultConfig("http://localhost:5140", "myplatform", "u")
	a, _ := NewAdapter(cfg)
	if a.Platform() != "myplatform" {
		t.Errorf("Platform: got %q, want myplatform", a.Platform())
	}
}

func TestAdapter_Platform_Fallback(t *testing.T) {
	// Build adapter with empty Platform – should fall back to PlatformID
	cfg := Config{
		ServerURL:         "http://localhost:5140",
		Platform:          "",
		UserID:            "u",
		Version:           "v1",
		ReconnectDelay:    1,
		MaxReconnectDelay: 1,
		EventBufferSize:   1,
		PingInterval:      1,
		HTTPTimeout:       1,
	}
	// Validate would reject empty Platform, so set after
	a := &Adapter{cfg: cfg}
	if a.Platform() != PlatformID {
		t.Errorf("Platform fallback: got %q, want %q", a.Platform(), PlatformID)
	}
}

// ─── Sender / Capabilities ───────────────────────────────────────────────────

func TestAdapter_Sender_NotNil(t *testing.T) {
	cfg := DefaultConfig("http://localhost:5140", "p", "u")
	a, _ := NewAdapter(cfg)
	if a.Sender() == nil {
		t.Error("Sender should not be nil")
	}
}

func TestAdapter_Capabilities(t *testing.T) {
	cfg := DefaultConfig("http://localhost:5140", "p", "u")
	a, _ := NewAdapter(cfg)
	caps := a.Capabilities()
	if !caps.GuildSupport {
		t.Error("GuildSupport should be true")
	}
	if !caps.Reactions {
		t.Error("Reactions should be true")
	}
	if !caps.MessageEdit {
		t.Error("MessageEdit should be true")
	}
	if !caps.MessageDelete {
		t.Error("MessageDelete should be true")
	}
}

// ─── IsRunning ────────────────────────────────────────────────────────────────

func TestAdapter_IsRunning_Initial(t *testing.T) {
	cfg := DefaultConfig("http://localhost:5140", "p", "u")
	a, _ := NewAdapter(cfg)
	if a.IsRunning() {
		t.Error("IsRunning should be false before Start")
	}
}

// ─── BotID / BotName ──────────────────────────────────────────────────────────

func TestAdapter_BotID_FallbackToConfigUserID(t *testing.T) {
	cfg := DefaultConfig("http://localhost:5140", "p", "user-99")
	a, _ := NewAdapter(cfg)
	// No READY received → falls back to cfg.UserID
	if a.BotID() != "user-99" {
		t.Errorf("BotID fallback: got %q, want user-99", a.BotID())
	}
}

func TestAdapter_BotName_EmptyBeforeReady(t *testing.T) {
	cfg := DefaultConfig("http://localhost:5140", "p", "u")
	a, _ := NewAdapter(cfg)
	if a.BotName() != "" {
		t.Errorf("BotName before READY: got %q, want empty", a.BotName())
	}
}

func TestAdapter_BotID_AfterLogin(t *testing.T) {
	cfg := DefaultConfig("http://localhost:5140", "p", "u")
	a, _ := NewAdapter(cfg)

	// Simulate READY callback filling login
	login := &Login{
		User: &User{ID: "bot-001", Name: new("MyBot")},
	}
	a.loginMu.Lock()
	a.login = login
	a.loginMu.Unlock()

	if a.BotID() != "bot-001" {
		t.Errorf("BotID after login: got %q, want bot-001", a.BotID())
	}
	if a.BotName() != "MyBot" {
		t.Errorf("BotName after login: got %q, want MyBot", a.BotName())
	}
}

// ─── ProxyURLs ────────────────────────────────────────────────────────────────

func TestAdapter_ProxyURLs_InitiallyNil(t *testing.T) {
	cfg := DefaultConfig("http://localhost:5140", "p", "u")
	a, _ := NewAdapter(cfg)
	if urls := a.ProxyURLs(); urls != nil {
		t.Errorf("ProxyURLs should be nil initially, got %v", urls)
	}
}

func TestAdapter_ProxyURLs_AfterMeta(t *testing.T) {
	cfg := DefaultConfig("http://localhost:5140", "p", "u")
	a, _ := NewAdapter(cfg)

	a.proxyMu.Lock()
	a.proxyURLs = []string{"https://proxy.example.com"}
	a.proxyMu.Unlock()

	urls := a.ProxyURLs()
	if len(urls) != 1 || urls[0] != "https://proxy.example.com" {
		t.Errorf("ProxyURLs: got %v", urls)
	}
}

func TestAdapter_ProxyURLs_ReturnsCopy(t *testing.T) {
	cfg := DefaultConfig("http://localhost:5140", "p", "u")
	a, _ := NewAdapter(cfg)
	a.proxyMu.Lock()
	a.proxyURLs = []string{"https://proxy.example.com"}
	a.proxyMu.Unlock()

	urls := a.ProxyURLs()
	urls[0] = "mutated"

	// Mutation of returned slice should not affect internal state
	a.proxyMu.RLock()
	if a.proxyURLs[0] != "https://proxy.example.com" {
		t.Errorf("ProxyURLs should return a copy; internal got %q", a.proxyURLs[0])
	}
	a.proxyMu.RUnlock()
}

// ─── Client ───────────────────────────────────────────────────────────────────

func TestAdapter_Client_NotNil(t *testing.T) {
	cfg := DefaultConfig("http://localhost:5140", "p", "u")
	a, _ := NewAdapter(cfg)
	if a.Client() == nil {
		t.Error("Client should not be nil")
	}
}

// ─── OnDisconnect ─────────────────────────────────────────────────────────────

func TestAdapter_OnDisconnect_Register(t *testing.T) {
	cfg := DefaultConfig("http://localhost:5140", "p", "u")
	a, _ := NewAdapter(cfg)

	called := 0
	unreg := a.OnDisconnect(func(err error) { called++ })

	// Manually trigger all disconnect fns
	a.disconnMu.Lock()
	fns := make([]func(error), len(a.disconnFns))
	copy(fns, a.disconnFns)
	a.disconnMu.Unlock()
	for _, fn := range fns {
		if fn != nil {
			fn(nil)
		}
	}

	if called != 1 {
		t.Errorf("OnDisconnect callback: called %d times, want 1", called)
	}

	// Unregister and fire again
	unreg()
	called = 0

	a.disconnMu.Lock()
	fns2 := make([]func(error), len(a.disconnFns))
	copy(fns2, a.disconnFns)
	a.disconnMu.Unlock()
	for _, fn := range fns2 {
		if fn != nil {
			fn(nil)
		}
	}
	if called != 0 {
		t.Errorf("after Unregister: called %d times, want 0", called)
	}
}

func TestAdapter_OnDisconnect_NilCallback(t *testing.T) {
	cfg := DefaultConfig("http://localhost:5140", "p", "u")
	a, _ := NewAdapter(cfg)
	unreg := a.OnDisconnect(nil) // should not panic
	if unreg == nil {
		t.Error("OnDisconnect(nil) should return a no-op unregister func, not nil")
	}
	unreg() // calling it should not panic
}

// ─── Stop with no running goroutine ──────────────────────────────────────────

func TestAdapter_Stop_WithoutStart(t *testing.T) {
	cfg := DefaultConfig("http://localhost:5140", "p", "u")
	a, _ := NewAdapter(cfg)
	// Stop without calling Start should not panic or error
	if err := a.Stop(nil); err != nil { //nolint:staticcheck
		t.Errorf("Stop without Start: unexpected error: %v", err)
	}
}
