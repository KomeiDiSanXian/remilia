// Package testutil provides test helpers for Remilia Bot Framework.
//
// Usage:
//
//	tb := testutil.New(t)
//	tb.RegisterPlugin(myplugin.New())
//
//	// Send a virtual group @bot message
//	resp := tb.SendGroupAt("user-openid-123", "group-openid-456", "/hello")
//	require.Equal(t, "Hello!", resp.FirstText())
//
//	// Send a virtual C2C (private) message
//	resp = tb.SendC2C("user-openid-123", "/help")
//	require.Contains(t, resp.FirstText(), "帮助")
package testutil

import (
	stdctx "context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	botctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/openapi"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/plugin"
	"github.com/tidwall/gjson"
)

// Response wraps captured replies with assertion helpers.
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

// TestBot is a lightweight bot for unit tests — no real network, no webhook.
type TestBot struct {
	t          testing.TB
	eng        *engine.Engine
	mgr        *plugin.Manager
	api        *mockAPI
	timeOffset time.Duration
	timeMu     sync.RWMutex
}

// New creates a TestBot bound to t. t.Cleanup is registered automatically.
func New(t testing.TB) *TestBot {
	t.Helper()
	eng := engine.NewEngine()
	mgr := plugin.NewManager(eng)
	tb := &TestBot{
		t:   t,
		eng: eng,
		mgr: mgr,
		api: &mockAPI{},
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

// SendC2C injects a virtual C2C (private chat) message and returns captured replies.
func (tb *TestBot) SendC2C(userOpenID, content string) *Response {
	tb.t.Helper()
	return tb.dispatch(tb.c2cPayload(userOpenID, content))
}

// SendGroupAt injects a virtual group @Bot message and returns captured replies.
func (tb *TestBot) SendGroupAt(userOpenID, groupOpenID, content string) *Response {
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

func (tb *TestBot) dispatch(payload *dto.Payload) *Response {
	tb.api.drain()
	c := botctx.NewContext(payload, tb.api)
	tb.eng.ProcessEvent(c)
	time.Sleep(10 * time.Millisecond)
	return &Response{replies: tb.api.drain()}
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
