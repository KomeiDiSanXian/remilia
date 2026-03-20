package testbot

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
	qqplatform "github.com/KomeiDiSanXian/remilia/platform/qq"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/plugin"
	"github.com/KomeiDiSanXian/remilia/testutil"
	"github.com/tidwall/gjson"
)

// Re-export platform-agnostic helpers from testutil so existing callers need not change.

// MockSender is an alias for testutil.MockSender.
type MockSender = testutil.MockSender

// MakePlatformC2CEvent re-exports testutil.MakePlatformC2CEvent.
var MakePlatformC2CEvent = testutil.MakePlatformC2CEvent

// MakePlatformGroupEvent re-exports testutil.MakePlatformGroupEvent.
var MakePlatformGroupEvent = testutil.MakePlatformGroupEvent

// ---------------------------------------------------------------------------
// MockAPI — QQ-specific mock for openapi.OpenAPI
// ---------------------------------------------------------------------------

// SentMessage captures a single message dispatched via the QQ OpenAPI mock.
type SentMessage struct {
	Target  string       // user openID (C2C) or group openID
	IsGroup bool         // true for GroupChat calls
	Content string       // msg.Content shortcut
	Msg     *dto.Message // full original message
}

// MockAPI implements openapi.OpenAPI and records all sent messages for assertions.
type MockAPI struct {
	mu      sync.Mutex
	replies []*SentMessage
}

// NewMockAPI creates a MockAPI.
func NewMockAPI() *MockAPI { return &MockAPI{} }

func (m *MockAPI) capture(target string, isGroup bool, msg *dto.Message) (gjson.Result, error) {
	content := ""
	if msg != nil {
		content = msg.Content
	}
	m.mu.Lock()
	m.replies = append(m.replies, &SentMessage{
		Target:  target,
		IsGroup: isGroup,
		Content: content,
		Msg:     msg,
	})
	m.mu.Unlock()
	return gjson.Parse(`{"id":"mock-msg-id"}`), nil
}

// Sent returns a snapshot of all captured messages (does not clear the buffer).
func (m *MockAPI) Sent() []*SentMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]*SentMessage, len(m.replies))
	copy(cp, m.replies)
	return cp
}

// LastSent returns the most-recently captured message, or nil if none.
func (m *MockAPI) LastSent() *SentMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.replies) == 0 {
		return nil
	}
	return m.replies[len(m.replies)-1]
}

// Drain returns and clears all captured messages.
func (m *MockAPI) Drain() []*SentMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := m.replies
	m.replies = nil
	return out
}

// Clear discards all captured messages.
func (m *MockAPI) Clear() {
	m.mu.Lock()
	m.replies = m.replies[:0]
	m.mu.Unlock()
}

func (m *MockAPI) SingleChat(target string, msg *dto.Message) (gjson.Result, error) {
	return m.capture(target, false, msg)
}
func (m *MockAPI) GroupChat(target string, msg *dto.Message) (gjson.Result, error) {
	return m.capture(target, true, msg)
}
func (m *MockAPI) SingleRichMedia(_ string, _ *dto.Media) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) GroupRichMedia(_ string, _ *dto.Media) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) SingleReset(_, _ string) (gjson.Result, error) { return gjson.Result{}, nil }
func (m *MockAPI) GroupReset(_, _ string) (gjson.Result, error)  { return gjson.Result{}, nil }

var _ openapi.OpenAPI = (*MockAPI)(nil)

// ---------------------------------------------------------------------------
// Bot — QQ-aware test Bot (no testing.TB, embeds testutil.Bot)
// ---------------------------------------------------------------------------

// Bot is a lightweight test Bot that supports both platform-agnostic and QQ-specific
// event injection. It embeds [testutil.Bot] so all platform-agnostic helpers are
// available directly on this type.
//
// For pure platform-agnostic tests, prefer [testutil.New] (TB-based) or
// [testutil.NewBot] (no TB). Use this type when you need to inject raw
// *dto.Payload events or assert against the QQ OpenAPI mock.
type Bot struct {
	*testutil.Bot
	api *MockAPI
}

// New creates a test Bot with a QQ MockAPI and a platform-agnostic MockSender.
func New() *Bot {
	return &Bot{
		Bot: testutil.NewBot(),
		api: NewMockAPI(),
	}
}

// API returns the QQ MockAPI for QQ-specific message assertions.
func (tb *Bot) API() *MockAPI { return tb.api }

// RegisterPlugin registers a plugin descriptor (deferred to Start).
func (tb *Bot) RegisterPlugin(desc *plugin.PluginDescriptor) *Bot {
	tb.Bot.RegisterPlugin(desc)
	return tb
}

// ---------------------------------------------------------------------------
// QQ-specific event injection
// ---------------------------------------------------------------------------

// SendGroupAt simulates a QQ group @-bot message via the full *dto.Payload path.
func (tb *Bot) SendGroupAt(groupID, userOpenID, content string) {
	event := dto.GroupAtMessageCreateEvent{
		MessageCreateEvent: dto.MessageCreateEvent{
			Content: content,
			Author:  dto.Author{UserOpenID: userOpenID, MemberOpenID: userOpenID},
		},
		GroupOpenID: groupID,
	}
	tb.inject(dto.GroupAtMessageCreate, event)
}

// SendC2C simulates a QQ C2C (private) message via the full *dto.Payload path.
func (tb *Bot) SendC2C(userOpenID, content string) {
	event := dto.C2CMessageCreateEvent{
		MessageCreateEvent: dto.MessageCreateEvent{
			Content: content,
			Author:  dto.Author{UserOpenID: userOpenID},
		},
	}
	tb.inject(dto.C2CMessageCreate, event)
}

// InjectEvent injects any platform.Event directly via the platform-agnostic engine path.
// This is the preferred injection method for platform-agnostic tests.
// For QQ-specific raw payload injection, use Inject instead.
func (tb *Bot) InjectEvent(event platform.Event) {
	tb.Engine().ProcessPlatformEvent(event, tb.SenderAPI())
}

// Inject injects an arbitrary *dto.Payload as a QQ platform event.
// It is a thin QQ-specific convenience wrapper around InjectEvent.
// For platform-agnostic injection, prefer InjectEvent.
func (tb *Bot) Inject(payload *dto.Payload) {
	tb.InjectEvent(qqplatform.NewEvent(payload))
}

func (tb *Bot) inject(eventType dto.EventType, event any) {
	detail, _ := json.Marshal(event)
	tb.Inject(&dto.Payload{Operation: dto.Dispatch, Type: eventType, Detail: detail})
}

// ---------------------------------------------------------------------------
// Assertion helpers
// ---------------------------------------------------------------------------

// AssertReplied asserts that a QQ message containing substr was captured by the MockAPI.
// The target parameter is accepted for documentation clarity but not checked (routing
// is asserted structurally via Sent/LastSent when needed).
func (tb *Bot) AssertReplied(t *testing.T, target, substr string) {
	t.Helper()
	for _, s := range tb.api.Drain() {
		if s != nil && strings.Contains(s.Content, substr) {
			_ = target
			return
		}
	}
	t.Errorf("testbot: no QQ message to %q containing %q", target, substr)
}

// AssertNotReplied asserts that no QQ message was captured by the MockAPI.
func (tb *Bot) AssertNotReplied(t *testing.T, target string) {
	t.Helper()
	for _, s := range tb.api.Drain() {
		if s != nil {
			t.Errorf("testbot: unexpected QQ message to %q: %q", target, s.Content)
			return
		}
	}
}

// AssertPlatformReplied asserts that a platform-agnostic message containing substr
// was captured by the MockSender.
func (tb *Bot) AssertPlatformReplied(t *testing.T, substr string) {
	t.Helper()
	if !tb.HasPlatformReply(substr) {
		t.Errorf("testbot: no platform message containing %q; sent=%v", substr, tb.SenderAPI().Sent())
	}
}

// ClearSent clears both the QQ MockAPI log and the platform MockSender log.
func (tb *Bot) ClearSent() {
	tb.api.Clear()
	tb.Bot.ClearSent()
}

var _ platform.Sender = (*MockSender)(nil)
