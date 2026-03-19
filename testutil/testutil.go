// Package testutil provides test helpers for Remilia Bot Framework.
//
// Usage:
//
//	tb := testutil.New(t)
//	tb.RegisterPlugin(myplugin.New())
//
//	// Send a virtual group @bot message (platform-agnostic, recommended)
//	resp := tb.SendPlatformGroupAt("user-id-123", "group-id-456", "/hello")
//	require.Equal(t, "Hello!", resp.FirstText())
//
//	// Send a virtual C2C (private) message (platform-agnostic, recommended)
//	resp = tb.SendPlatformC2C("user-id-123", "/help")
//	require.Contains(t, resp.FirstText(), "帮助")
//
//	// Legacy QQ path (still supported)
//	resp2 := tb.SendGroupAt("user-openid-123", "group-openid-456", "/hello")
//	require.Equal(t, "Hello!", resp2.FirstText())
package testutil

import (
	stdctx "context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	botctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/platform"
	qqplatform "github.com/KomeiDiSanXian/remilia/platform/qq"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/plugin"
	"github.com/tidwall/gjson"
)

// Response wraps captured replies with assertion helpers (QQ 旧路径).
type Response struct {
	replies []*dto.Message
}

// All returns all captured reply messages.
func (r *Response) All() []*dto.Message { return r.replies }

// Count returns the number of replies.
func (r *Response) Count() int { return len(r.replies) }

// First returns the first reply or nil.
func (r *Response) First() *dto.Message {
	if len(r.replies) == 0 {
		return nil
	}
	return r.replies[0]
}

// FirstText returns the text content of the first reply, or empty string.
func (r *Response) FirstText() string {
	if m := r.First(); m != nil {
		return m.Content
	}
	return ""
}

// Texts returns text content of all replies.
func (r *Response) Texts() []string {
	texts := make([]string, 0, len(r.replies))
	for _, m := range r.replies {
		texts = append(texts, m.Content)
	}
	return texts
}

// HasReply returns true if there is at least one reply.
func (r *Response) HasReply() bool { return len(r.replies) > 0 }

// mockAPI captures all sent messages for assertion.
type mockAPI struct {
	mu      sync.Mutex
	replies []*dto.Message
}

func (m *mockAPI) capture(msg *dto.Message) (gjson.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.replies = append(m.replies, msg)
	return gjson.Parse(`{"id":"mock-msg-id"}`), nil
}

func (m *mockAPI) drain() []*dto.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := m.replies
	m.replies = nil
	return out
}

func (m *mockAPI) SingleChat(_ string, msg *dto.Message) (gjson.Result, error) {
	return m.capture(msg)
}
func (m *mockAPI) GroupChat(_ string, msg *dto.Message) (gjson.Result, error) {
	return m.capture(msg)
}
func (m *mockAPI) SingleRichMedia(_ string, _ *dto.Media) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *mockAPI) GroupRichMedia(_ string, _ *dto.Media) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *mockAPI) SingleReset(_, _ string) (gjson.Result, error) { return gjson.Result{}, nil }
func (m *mockAPI) GroupReset(_, _ string) (gjson.Result, error)  { return gjson.Result{}, nil }

var _ openapi.OpenAPI = (*mockAPI)(nil)

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

// mockSender captures platform.OutboundMessage for platform-agnostic tests.
type mockSender struct {
	mu       sync.Mutex
	messages []platform.OutboundMessage
}

func (s *mockSender) Send(_ stdctx.Context, _ string, msg platform.OutboundMessage) error {
	s.mu.Lock()
	s.messages = append(s.messages, msg)
	s.mu.Unlock()
	return nil
}

func (s *mockSender) drain() []platform.OutboundMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.messages
	s.messages = nil
	return out
}

// TestBot is a lightweight bot for unit tests — no real network, no webhook.
type TestBot struct {
	t          testing.TB
	eng        *engine.Engine
	mgr        *plugin.Manager
	sender     *mockSender
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
		sender: &mockSender{},
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
// This is the platform-agnostic counterpart to SendC2C / SendGroupAt.
func (tb *TestBot) SendPlatformEvent(event platform.Event) *PlatformResponse {
	tb.t.Helper()
	tb.sender.drain()
	ctx := botctx.AcquireContextFromEvent(event, tb.sender)
	tb.eng.ProcessPlatformEvent(event, tb.sender)
	botctx.ReleaseContextFromEvent(ctx)
	time.Sleep(10 * time.Millisecond)
	return &PlatformResponse{messages: tb.sender.drain()}
}

// SendC2C injects a virtual C2C (private chat) message and returns captured replies.
//
// Deprecated: prefer SendPlatformC2C which uses the platform-agnostic path.
// This method now internally uses ProcessPlatformEvent; replies are captured
// via the platform.Sender and returned as *PlatformResponse.
func (tb *TestBot) SendC2C(userOpenID, content string) *PlatformResponse {
	tb.t.Helper()
	return tb.dispatch(tb.c2cPayload(userOpenID, content))
}

// SendGroupAt injects a virtual group @Bot message and returns captured replies.
//
// Deprecated: prefer SendPlatformGroupAt which uses the platform-agnostic path.
// This method now internally uses ProcessPlatformEvent; replies are captured
// via the platform.Sender and returned as *PlatformResponse.
func (tb *TestBot) SendGroupAt(userOpenID, groupOpenID, content string) *PlatformResponse {
	tb.t.Helper()
	return tb.dispatch(tb.groupAtPayload(userOpenID, groupOpenID, content))
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

// ----- internals -----

func (tb *TestBot) dispatch(payload *dto.Payload) *PlatformResponse {
	tb.sender.drain()
	event := qqplatform.NewEvent(payload)
	tb.eng.ProcessPlatformEvent(event, tb.sender)
	time.Sleep(10 * time.Millisecond)
	return &PlatformResponse{messages: tb.sender.drain()}
}

func (tb *TestBot) c2cPayload(userOpenID, content string) *dto.Payload {
	raw, _ := json.Marshal(map[string]any{
		"author":    map[string]any{"user_openid": userOpenID},
		"content":   content,
		"id":        "test-event-" + userOpenID,
		"msg_id":    "test-msg-id",
		"event_id":  "test-event-id",
		"timestamp": time.Now().Unix(),
		"msg_seq":   1,
	})
	return &dto.Payload{
		Operation: dto.Dispatch,
		Type:      dto.C2CMessageCreate,
		Detail:    raw,
		Raw:       raw,
	}
}

func (tb *TestBot) groupAtPayload(userOpenID, groupOpenID, content string) *dto.Payload {
	raw, _ := json.Marshal(map[string]any{
		"author":       map[string]any{"user_openid": userOpenID},
		"group_openid": groupOpenID,
		"content":      content,
		"id":           "test-event-" + userOpenID,
		"msg_id":       "test-msg-id",
		"event_id":     "test-event-id",
		"timestamp":    time.Now().Unix(),
		"msg_seq":      1,
	})
	return &dto.Payload{
		Operation: dto.Dispatch,
		Type:      dto.GroupAtMessageCreate,
		Detail:    raw,
		Raw:       raw,
	}
}

// ----- platform-agnostic test helpers -----

// mockPlatformEvent is a simple platform.Event implementation for tests.
type mockPlatformEvent struct {
	kind    platform.EventKind
	sender  platform.UserInfo
	chat    platform.ChatInfo
	content string
	ts      time.Time
}

func (e *mockPlatformEvent) Platform() string          { return "test" }
func (e *mockPlatformEvent) ID() string                { return "" }
func (e *mockPlatformEvent) Kind() platform.EventKind  { return e.kind }
func (e *mockPlatformEvent) RawType() string           { return string(e.kind) }
func (e *mockPlatformEvent) Sender() platform.UserInfo { return e.sender }
func (e *mockPlatformEvent) Chat() platform.ChatInfo   { return e.chat }
func (e *mockPlatformEvent) Content() string           { return e.content }
func (e *mockPlatformEvent) Timestamp() time.Time      { return e.ts }
func (e *mockPlatformEvent) RawPayload() any           { return nil }

// MakePlatformC2CEvent creates a platform-agnostic private chat (C2C) event for tests.
//
// userID is the sender's platform user ID; content is the message text.
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
//
// userID is the sender's ID; groupID is the group/channel ID; content is the message text.
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
//
// This is the recommended replacement for SendC2C in new tests.
func (tb *TestBot) SendPlatformC2C(userID, content string) *PlatformResponse {
	tb.t.Helper()
	return tb.SendPlatformEvent(MakePlatformC2CEvent(userID, content))
}

// SendPlatformGroupAt injects a platform-agnostic group message event and returns captured replies.
//
// This is the recommended replacement for SendGroupAt in new tests.
func (tb *TestBot) SendPlatformGroupAt(userID, groupID, content string) *PlatformResponse {
	tb.t.Helper()
	return tb.SendPlatformEvent(MakePlatformGroupEvent(userID, groupID, content))
}
