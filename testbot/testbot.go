package testbot

import (
	stdctx "context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/plugin"
	"github.com/tidwall/gjson"
)

// SentMessage records a captured outgoing message.
type SentMessage struct {
	Target  string
	IsGroup bool
	Msg     *dto.Message
}

// MockAPI implements openapi.OpenAPI and captures all sent messages.
type MockAPI struct {
	mu   sync.Mutex
	sent []SentMessage
}

// NewMockAPI creates a MockAPI.
func NewMockAPI() *MockAPI { return &MockAPI{} }
func (m *MockAPI) record(target string, isGroup bool, msg *dto.Message) (gjson.Result, error) {
	m.mu.Lock()
	cp := *msg
	m.sent = append(m.sent, SentMessage{Target: target, IsGroup: isGroup, Msg: &cp})
	m.mu.Unlock()
	return gjson.Result{}, nil
}
func (m *MockAPI) SingleChat(openID string, msg *dto.Message) (gjson.Result, error) {
	return m.record(openID, false, msg)
}
func (m *MockAPI) GroupChat(groupID string, msg *dto.Message) (gjson.Result, error) {
	return m.record(groupID, true, msg)
}
func (m *MockAPI) SingleRichMedia(openID string, media *dto.Media) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) GroupRichMedia(groupID string, media *dto.Media) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) SingleReset(openID, messageID string) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) GroupReset(groupID, messageID string) (gjson.Result, error) {
	return gjson.Result{}, nil
}

// Sent returns a snapshot of all captured messages.
func (m *MockAPI) Sent() []SentMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]SentMessage, len(m.sent))
	copy(cp, m.sent)
	return cp
}

// Clear clears captured messages.
func (m *MockAPI) Clear() {
	m.mu.Lock()
	m.sent = m.sent[:0]
	m.mu.Unlock()
}

// LastSent returns the last sent message or nil.
func (m *MockAPI) LastSent() *SentMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.sent) == 0 {
		return nil
	}
	cp := m.sent[len(m.sent)-1]
	return &cp
}

// MockSender implements platform.Sender and captures outbound messages for test assertions.
type MockSender struct {
	mu   sync.Mutex
	sent []platform.OutboundMessage
}

// NewMockSender creates a MockSender.
func NewMockSender() *MockSender { return &MockSender{} }

// Send implements platform.Sender.
func (s *MockSender) Send(_ stdctx.Context, _ string, msg platform.OutboundMessage) error {
	s.mu.Lock()
	s.sent = append(s.sent, msg)
	s.mu.Unlock()
	return nil
}

// Sent returns a snapshot of all captured messages.
func (s *MockSender) Sent() []platform.OutboundMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]platform.OutboundMessage, len(s.sent))
	copy(cp, s.sent)
	return cp
}

// Clear clears captured messages.
func (s *MockSender) Clear() {
	s.mu.Lock()
	s.sent = s.sent[:0]
	s.mu.Unlock()
}

// LastSent returns the last captured outbound message text, or empty string.
func (s *MockSender) LastText() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.sent) == 0 {
		return ""
	}
	return s.sent[len(s.sent)-1].Text
}

// Bot is a lightweight test Bot that injects events directly without networking.
type Bot struct {
	eng     *engine.Engine
	pm      *plugin.Manager
	api     *MockAPI
	sender  *MockSender
	plugins []*plugin.PluginDescriptor
}

// New creates a test Bot.
func New() *Bot {
	eng := engine.NewEngine()
	return &Bot{eng: eng, pm: plugin.NewManager(eng), api: NewMockAPI(), sender: NewMockSender()}
}

// RegisterPlugin registers a plugin descriptor.
func (tb *Bot) RegisterPlugin(desc *plugin.PluginDescriptor) *Bot {
	tb.plugins = append(tb.plugins, desc)
	return tb
}

// Start loads all registered plugins.
func (tb *Bot) Start() error {
	for _, desc := range tb.plugins {
		if err := tb.pm.RegisterV2(desc); err != nil {
			return fmt.Errorf("testbot: register plugin %s: %w", desc.Name, err)
		}
	}
	return nil
}

// Stop is a no-op; present for defer patterns.
func (tb *Bot) Stop() {}

// Engine returns the underlying Engine.
func (tb *Bot) Engine() *engine.Engine { return tb.eng }

// Manager returns the plugin manager.
func (tb *Bot) Manager() *plugin.Manager { return tb.pm }

// API returns the MockAPI for assertions.
func (tb *Bot) API() *MockAPI { return tb.api }

// SendGroupAt simulates a group at-Bot message.
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

// SendC2C simulates a C2C message.
func (tb *Bot) SendC2C(userOpenID, content string) {
	event := dto.C2CMessageCreateEvent{
		MessageCreateEvent: dto.MessageCreateEvent{
			Content: content,
			Author:  dto.Author{UserOpenID: userOpenID},
		},
	}
	tb.inject(dto.C2CMessageCreate, event)
}

// Inject injects an arbitrary Payload.
func (tb *Bot) Inject(payload *dto.Payload) {
	ctx := context.NewContext(payload, tb.api)
	tb.eng.ProcessEvent(ctx)
}
func (tb *Bot) inject(eventType dto.EventType, event any) {
	detail, _ := json.Marshal(event)
	tb.Inject(&dto.Payload{Operation: dto.Dispatch, Type: eventType, Detail: detail})
}

// SendPlatformEvent injects an arbitrary platform.Event and captures replies via MockSender.
func (tb *Bot) SendPlatformEvent(event platform.Event) {
	tb.eng.ProcessPlatformEvent(event, tb.sender)
}

// SenderAPI returns the MockSender for platform-agnostic message assertions.
func (tb *Bot) SenderAPI() *MockSender { return tb.sender }

// MakePlatformC2CEvent creates a platform-agnostic private chat (C2C) event for tests.
//
// userID is the sender's platform user ID; content is the message text.
func MakePlatformC2CEvent(userID, content string) platform.Event {
	return &mockPlatformEvent{
		kind:    platform.EventKindPrivateMessage,
		content: content,
		sender:  platform.UserInfo{ID: userID, DisplayName: userID},
		chat:    platform.ChatInfo{ID: userID, IsGroup: false},
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
	}
}

// SendPlatformC2C simulates a platform-agnostic C2C (private) message.
// Replies are captured in MockSender; use tb.SenderAPI() to assert.
func (tb *Bot) SendPlatformC2C(userID, content string) {
	tb.SendPlatformEvent(MakePlatformC2CEvent(userID, content))
}

// SendPlatformGroupAt simulates a platform-agnostic group message.
// Replies are captured in MockSender; use tb.SenderAPI() to assert.
func (tb *Bot) SendPlatformGroupAt(userID, groupID, content string) {
	tb.SendPlatformEvent(MakePlatformGroupEvent(userID, groupID, content))
}

// mockPlatformEvent is a minimal platform.Event implementation for tests.
type mockPlatformEvent struct {
	kind    platform.EventKind
	sender  platform.UserInfo
	chat    platform.ChatInfo
	content string
}

func (e *mockPlatformEvent) Platform() string          { return "test" }
func (e *mockPlatformEvent) Kind() platform.EventKind  { return e.kind }
func (e *mockPlatformEvent) RawType() string           { return string(e.kind) }
func (e *mockPlatformEvent) Sender() platform.UserInfo { return e.sender }
func (e *mockPlatformEvent) Chat() platform.ChatInfo   { return e.chat }
func (e *mockPlatformEvent) Content() string           { return e.content }
func (e *mockPlatformEvent) Timestamp() time.Time      { return time.Time{} }
func (e *mockPlatformEvent) RawPayload() any           { return nil }

// AssertReplied asserts that a message containing substr was sent to target.
func (tb *Bot) AssertReplied(t *testing.T, target, substr string) {
	t.Helper()
	for _, s := range tb.api.Sent() {
		if s.Target == target && s.Msg != nil && strings.Contains(s.Msg.Content, substr) {
			return
		}
	}
	t.Errorf("testbot: no message to %q containing %q; sent=%v", target, substr, tb.api.Sent())
}

// AssertNotReplied asserts no message was sent to target.
func (tb *Bot) AssertNotReplied(t *testing.T, target string) {
	t.Helper()
	for _, s := range tb.api.Sent() {
		if s.Target == target {
			t.Errorf("testbot: unexpected message to %q: %q", target, s.Msg.Content)
			return
		}
	}
}

// AssertSentCount asserts the total number of sent messages.
func (tb *Bot) AssertSentCount(t *testing.T, n int) {
	t.Helper()
	if got := len(tb.api.Sent()); got != n {
		t.Errorf("testbot: expected %d sent messages, got %d", n, got)
	}
}

// ClearSent clears the sent message log.
func (tb *Bot) ClearSent() { tb.api.Clear() }

// AssertPlatformReplied asserts that a platform message containing substr was sent.
func (tb *Bot) AssertPlatformReplied(t *testing.T, substr string) {
	t.Helper()
	for _, msg := range tb.sender.Sent() {
		if strings.Contains(msg.Text, substr) {
			return
		}
	}
	t.Errorf("testbot: no platform message containing %q; sent=%v", substr, tb.sender.Sent())
}

var _ openapi.OpenAPI = (*MockAPI)(nil)
var _ platform.Sender = (*MockSender)(nil)
