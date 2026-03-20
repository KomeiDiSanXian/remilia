// Package testutil provides platform-agnostic test helpers for Remilia Bot Framework.
//
// Usage:
//
//	tb := testutil.New(t)
//	tb.RegisterPlugin(myplugin.New())
//
//	// Send a virtual group @bot message (platform-agnostic)
//	resp := tb.SendPlatformGroupAt("user-id-123", "group-id-456", "/hello")
//	require.Equal(t, "Hello!", resp.FirstText())
//
//	// Send a virtual C2C (private) message (platform-agnostic)
//	resp = tb.SendPlatformC2C("user-id-123", "/help")
//	require.Contains(t, resp.FirstText(), "帮助")
package testutil

import (
	stdctx "context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	botctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// ---------------------------------------------------------------------------
// MockSender — 捕获平台无关出站消息，用于断言
// ---------------------------------------------------------------------------

// MockSender implements platform.Sender and captures outbound messages for test assertions.
type MockSender struct {
	mu       sync.Mutex
	messages []platform.OutboundMessage
}

// Send implements platform.Sender.
func (s *MockSender) Send(_ stdctx.Context, msg platform.OutboundMessage) error {
	s.mu.Lock()
	s.messages = append(s.messages, msg)
	s.mu.Unlock()
	return nil
}

func (s *MockSender) drain() []platform.OutboundMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.messages
	s.messages = nil
	return out
}

// Sent returns a snapshot of all captured messages.
func (s *MockSender) Sent() []platform.OutboundMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]platform.OutboundMessage, len(s.messages))
	copy(cp, s.messages)
	return cp
}

// Clear resets captured messages.
func (s *MockSender) Clear() {
	s.mu.Lock()
	s.messages = s.messages[:0]
	s.mu.Unlock()
}

var _ platform.Sender = (*MockSender)(nil)

// ---------------------------------------------------------------------------
// PlatformResponse — 平台无关回复断言辅助
// ---------------------------------------------------------------------------

// PlatformResponse wraps captured platform-agnostic outbound messages.
type PlatformResponse struct {
	messages []platform.OutboundMessage
}

// All returns all captured outbound messages.
func (r *PlatformResponse) All() []platform.OutboundMessage { return r.messages }

// Count returns the number of messages.
func (r *PlatformResponse) Count() int { return len(r.messages) }

// First returns the first message or zero value.
func (r *PlatformResponse) First() platform.OutboundMessage {
	if len(r.messages) == 0 {
		return platform.OutboundMessage{}
	}
	return r.messages[0]
}

// FirstText returns the text of the first message.
func (r *PlatformResponse) FirstText() string { return r.First().Text }

// HasReply returns true if at least one message was captured.
func (r *PlatformResponse) HasReply() bool { return len(r.messages) > 0 }

// ---------------------------------------------------------------------------
// TestBot — 主测试 Bot（需要 testing.TB，自动 Cleanup）
// ---------------------------------------------------------------------------

// TestBot is a lightweight bot for unit tests — no real network, no webhook.
type TestBot struct {
	t          testing.TB
	eng        *engine.Engine
	mgr        *plugin.Manager
	sender     *MockSender
	timeOffset time.Duration
	timeMu     sync.RWMutex
}

// New creates a TestBot bound to t. t.Cleanup is registered automatically.
func New(t testing.TB) *TestBot {
	t.Helper()
	eng := engine.NewEngine()
	mgr := plugin.NewManager(eng)
	tb := &TestBot{
		t:      t,
		eng:    eng,
		mgr:    mgr,
		sender: &MockSender{},
	}
	t.Cleanup(func() { eng.Shutdown(stdctx.Background()) })
	return tb
}

// RegisterPlugin registers a v2 plugin descriptor. Fails the test on error.
func (tb *TestBot) RegisterPlugin(desc *plugin.PluginDescriptor) {
	tb.t.Helper()
	if err := tb.mgr.RegisterV2(desc); err != nil {
		tb.t.Fatalf("testutil: RegisterPlugin %q: %v", desc.Name, err)
	}
}

// RegisterPlugins registers multiple plugins, respecting dependency order.
func (tb *TestBot) RegisterPlugins(descs ...*plugin.PluginDescriptor) {
	tb.t.Helper()
	if err := tb.mgr.RegisterMultipleV2(descs); err != nil {
		tb.t.Fatalf("testutil: RegisterPlugins: %v", err)
	}
}

// Engine returns the underlying event engine for advanced usage.
func (tb *TestBot) Engine() *engine.Engine { return tb.eng }

// Manager returns the underlying plugin manager.
func (tb *TestBot) Manager() *plugin.Manager { return tb.mgr }

// SendPlatformEvent injects a platform.Event and returns captured platform replies.
func (tb *TestBot) SendPlatformEvent(event platform.Event) *PlatformResponse {
	tb.t.Helper()
	tb.sender.drain()
	ctx := botctx.AcquireContextFromEvent(event, tb.sender)
	tb.eng.ProcessPlatformEvent(event, tb.sender)
	botctx.ReleaseContextFromEvent(ctx)
	time.Sleep(10 * time.Millisecond)
	return &PlatformResponse{messages: tb.sender.drain()}
}

// AdvanceTime shifts the internal time offset (for cooldown/scheduler testing).
func (tb *TestBot) AdvanceTime(d time.Duration) {
	tb.timeMu.Lock()
	defer tb.timeMu.Unlock()
	tb.timeOffset += d
}

// TimeOffset returns the current simulated time offset.
func (tb *TestBot) TimeOffset() time.Duration {
	tb.timeMu.RLock()
	defer tb.timeMu.RUnlock()
	return tb.timeOffset
}

// ---------------------------------------------------------------------------
// 平台无关事件工厂 & 便捷发送方法
// ---------------------------------------------------------------------------

// mockPlatformEvent is a simple platform.Event implementation for tests.
type mockPlatformEvent struct {
	kind    platform.EventKind
	sender  platform.UserInfo
	chat    platform.ChatInfo
	content string
	ts      time.Time
}

func (e *mockPlatformEvent) Platform() string                          { return "test" }
func (e *mockPlatformEvent) ID() string                                { return "" }
func (e *mockPlatformEvent) Kind() platform.EventKind                  { return e.kind }
func (e *mockPlatformEvent) RawType() string                           { return string(e.kind) }
func (e *mockPlatformEvent) Sender() platform.UserInfo                 { return e.sender }
func (e *mockPlatformEvent) Chat() platform.ChatInfo                   { return e.chat }
func (e *mockPlatformEvent) Content() string                           { return e.content }
func (e *mockPlatformEvent) Attachments() []platform.InboundAttachment { return nil }
func (e *mockPlatformEvent) Timestamp() time.Time                      { return e.ts }
func (e *mockPlatformEvent) RawPayload() any                           { return nil }

// MakePlatformC2CEvent creates a platform-agnostic private chat (C2C) event for tests.
func MakePlatformC2CEvent(userID, content string) platform.Event {
	return &mockPlatformEvent{
		kind:    platform.EventKindPrivateMessage,
		content: content,
		sender:  platform.UserInfo{ID: userID, DisplayName: userID},
		chat:    platform.ChatInfo{ID: userID, IsGroup: false},
		ts:      time.Now(),
	}
}

// MakePlatformGroupEvent creates a platform-agnostic group message event for tests.
func MakePlatformGroupEvent(userID, groupID, content string) platform.Event {
	return &mockPlatformEvent{
		kind:    platform.EventKindGroupMessage,
		content: content,
		sender:  platform.UserInfo{ID: userID, DisplayName: userID},
		chat:    platform.ChatInfo{ID: groupID, IsGroup: true},
		ts:      time.Now(),
	}
}

// SendPlatformC2C injects a platform-agnostic private chat (C2C) event and returns captured replies.
func (tb *TestBot) SendPlatformC2C(userID, content string) *PlatformResponse {
	tb.t.Helper()
	return tb.SendPlatformEvent(MakePlatformC2CEvent(userID, content))
}

// SendPlatformGroupAt injects a platform-agnostic group message event and returns captured replies.
func (tb *TestBot) SendPlatformGroupAt(userID, groupID, content string) *PlatformResponse {
	tb.t.Helper()
	return tb.SendPlatformEvent(MakePlatformGroupEvent(userID, groupID, content))
}

// ---------------------------------------------------------------------------
// Bot — 无 testing.TB 依赖的轻量测试 Bot（适合基准测试和集成测试）
// ---------------------------------------------------------------------------

// Bot is a lightweight test bot without a testing.TB dependency.
// Use this for benchmarks or integration tests where TB auto-failure is not desired.
// For unit tests, use [New] which returns a [*TestBot] with automatic cleanup.
type Bot struct {
	eng     *engine.Engine
	mgr     *plugin.Manager
	sender  *MockSender
	plugins []*plugin.PluginDescriptor
}

// NewBot creates a Bot for benchmarks or integration tests.
func NewBot() *Bot {
	eng := engine.NewEngine()
	return &Bot{
		eng:    eng,
		mgr:    plugin.NewManager(eng),
		sender: &MockSender{},
	}
}

// RegisterPlugin appends a plugin descriptor; it is registered during [Bot.Start].
func (b *Bot) RegisterPlugin(desc *plugin.PluginDescriptor) *Bot {
	b.plugins = append(b.plugins, desc)
	return b
}

// RegisterPlugins appends multiple descriptors; they are registered during [Bot.Start].
func (b *Bot) RegisterPlugins(descs ...*plugin.PluginDescriptor) *Bot {
	b.plugins = append(b.plugins, descs...)
	return b
}

// Start loads all registered plugins into the engine. Returns the first error encountered.
func (b *Bot) Start() error {
	for _, desc := range b.plugins {
		if err := b.mgr.RegisterV2(desc); err != nil {
			return fmt.Errorf("testutil: Bot.Start register plugin %q: %w", desc.Name, err)
		}
	}
	return nil
}

// Stop is a no-op; present for symmetry with Start and defer patterns.
func (b *Bot) Stop() {}

// Engine returns the underlying event engine.
func (b *Bot) Engine() *engine.Engine { return b.eng }

// Manager returns the plugin manager.
func (b *Bot) Manager() *plugin.Manager { return b.mgr }

// SenderAPI returns the MockSender for capturing and asserting outbound messages.
func (b *Bot) SenderAPI() *MockSender { return b.sender }

// SendPlatformEvent injects a platform.Event directly into the engine.
func (b *Bot) SendPlatformEvent(event platform.Event) {
	b.eng.ProcessPlatformEvent(event, b.sender)
}

// SendPlatformC2C simulates a platform-agnostic private (C2C) message.
func (b *Bot) SendPlatformC2C(userID, content string) {
	b.SendPlatformEvent(MakePlatformC2CEvent(userID, content))
}

// SendPlatformGroupAt simulates a platform-agnostic group message.
func (b *Bot) SendPlatformGroupAt(userID, groupID, content string) {
	b.SendPlatformEvent(MakePlatformGroupEvent(userID, groupID, content))
}

// ClearSent discards all currently captured outbound messages.
func (b *Bot) ClearSent() { b.sender.Clear() }

// HasPlatformReply returns true if any captured message's text contains substr.
// Useful for assertions in benchmarks or table-driven tests without a TB.
func (b *Bot) HasPlatformReply(substr string) bool {
	for _, msg := range b.sender.Sent() {
		if strings.Contains(msg.Text, substr) {
			return true
		}
	}
	return false
}
