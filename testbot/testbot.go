package testbot

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
	qqplatform "github.com/KomeiDiSanXian/remilia/platform/qq"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/plugin"
	"github.com/tidwall/gjson"
)

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

func (m *MockAPI) SingleChat(_ context.Context, target string, msg *dto.Message) (gjson.Result, error) {
	return m.capture(target, false, msg)
}
func (m *MockAPI) GroupChat(_ context.Context, target string, msg *dto.Message) (gjson.Result, error) {
	return m.capture(target, true, msg)
}
func (m *MockAPI) ChannelChat(_ context.Context, _ string, _ *dto.GuildMessage) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) DMChat(_ context.Context, _ string, _ *dto.GuildMessage) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) SingleRichMedia(_ context.Context, _ string, _ *dto.Media) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) GroupRichMedia(_ context.Context, _ string, _ *dto.Media) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) SingleReset(_ context.Context, _, _ string) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) GroupReset(_ context.Context, _, _ string) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) ChannelReset(_ context.Context, _, _ string) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) DMReset(_ context.Context, _, _ string) (gjson.Result, error) {
	return gjson.Result{}, nil
}

// ── 互动事件 ──────────────────────────────────────────────────────────────
func (m *MockAPI) RespondInteraction(_ context.Context, _ string, _ int) (gjson.Result, error) {
	return gjson.Result{}, nil
}

// ── 频道成员 ──────────────────────────────────────────────────────────────
func (m *MockAPI) GetChannelOnlineNums(_ context.Context, _ string) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) GetGuildMembers(_ context.Context, _, _ string, _ int) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) GetGuildRoleMembers(_ context.Context, _, _ string, _, _ uint32) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) GetGuildMember(_ context.Context, _, _ string) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) DeleteGuildMember(_ context.Context, _, _ string, _ bool, _ int) (gjson.Result, error) {
	return gjson.Result{}, nil
}

// ── 频道身份组 ────────────────────────────────────────────────────────────
func (m *MockAPI) GetGuildRoles(_ context.Context, _ string) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) CreateGuildRole(_ context.Context, _ string, _ *dto.GuildRoleRequest) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) UpdateGuildRole(_ context.Context, _, _ string, _ *dto.GuildRoleRequest) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) DeleteGuildRole(_ context.Context, _, _ string) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) AddGuildMemberRole(_ context.Context, _, _, _, _ string) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) DeleteGuildMemberRole(_ context.Context, _, _, _, _ string) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) GetChannelMemberPermissions(_ context.Context, _, _ string) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) UpdateChannelMemberPermissions(_ context.Context, _, _ string, _ *dto.PermissionRequest) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) GetChannelRolePermissions(_ context.Context, _, _ string) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) UpdateChannelRolePermissions(_ context.Context, _, _ string, _ *dto.PermissionRequest) (gjson.Result, error) {
	return gjson.Result{}, nil
}

// ── 接口授权管理 ──────────────────────────────────────────────────────────
func (m *MockAPI) GetGuildAPIPermissions(_ context.Context, _ string) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) RequestGuildAPIPermission(_ context.Context, _ string, _ *dto.APIPermissionDemandRequest) (gjson.Result, error) {
	return gjson.Result{}, nil
}

// ── 发言管理 ──────────────────────────────────────────────────────────────
func (m *MockAPI) GetGuildMessageSetting(_ context.Context, _ string) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) MuteGuild(_ context.Context, _ string, _ *dto.MuteRequest) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) MuteGuildMember(_ context.Context, _, _ string, _ *dto.MuteRequest) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) MuteGuildMultiMembers(_ context.Context, _ string, _ *dto.MultipleMuteRequest) (gjson.Result, error) {
	return gjson.Result{}, nil
}

// ── 内容管理：公告 ────────────────────────────────────────────────────────
func (m *MockAPI) CreateGuildAnnounce(_ context.Context, _ string, _ *dto.CreateGuildAnnounceRequest) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) DeleteGuildAnnounce(_ context.Context, _, _ string) (gjson.Result, error) {
	return gjson.Result{}, nil
}

// ── 内容管理：精华消息 ────────────────────────────────────────────────────
func (m *MockAPI) PinMessage(_ context.Context, _, _ string) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) UnpinMessage(_ context.Context, _, _ string) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) GetPinnedMessages(_ context.Context, _ string) (gjson.Result, error) {
	return gjson.Result{}, nil
}

// ── 内容管理：日程 ────────────────────────────────────────────────────────
func (m *MockAPI) GetChannelSchedules(_ context.Context, _ string, _ uint64) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) GetChannelSchedule(_ context.Context, _, _ string) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) CreateChannelSchedule(_ context.Context, _ string, _ *dto.ScheduleRequest) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) UpdateChannelSchedule(_ context.Context, _, _ string, _ *dto.ScheduleRequest) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) DeleteChannelSchedule(_ context.Context, _, _ string) (gjson.Result, error) {
	return gjson.Result{}, nil
}

// ── 内容管理：音频 ────────────────────────────────────────────────────────
func (m *MockAPI) AudioControl(_ context.Context, _ string, _ *dto.AudioControlRequest) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) BotOnMic(_ context.Context, _ string) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) BotOffMic(_ context.Context, _ string) (gjson.Result, error) {
	return gjson.Result{}, nil
}

// ── 内容管理：论坛帖子 ────────────────────────────────────────────────────
func (m *MockAPI) GetThreadList(_ context.Context, _ string) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) GetThread(_ context.Context, _, _ string) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) CreateThread(_ context.Context, _ string, _ *dto.ThreadRequest) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) DeleteThread(_ context.Context, _, _ string) (gjson.Result, error) {
	return gjson.Result{}, nil
}

// ── 频道管理 ──────────────────────────────────────────────────────────────
func (m *MockAPI) GetMe(_ context.Context) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) GetMyGuilds(_ context.Context, _, _ string, _ int) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) GetGuild(_ context.Context, _ string) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) GetGuildChannels(_ context.Context, _ string) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) GetChannel(_ context.Context, _ string) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) CreateGuildChannel(_ context.Context, _ string, _ *dto.ChannelRequest) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) UpdateGuildChannel(_ context.Context, _ string, _ *dto.ChannelRequest) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) DeleteGuildChannel(_ context.Context, _ string) (gjson.Result, error) {
	return gjson.Result{}, nil
}
func (m *MockAPI) CreateDirectMessageSession(_ context.Context, _ *dto.DirectMessageSessionRequest) (gjson.Result, error) {
	return gjson.Result{}, nil
}

var _ openapi.OpenAPI = (*MockAPI)(nil)

// ---------------------------------------------------------------------------
// QQBot — QQ-aware test QQBot (no testing.TB, embeds testutil.Bot)
// ---------------------------------------------------------------------------

// QQBot is a lightweight test Bot that supports both platform-agnostic and QQ-specific
// event injection. It embeds [testutil.Bot] so all platform-agnostic helpers are
// available directly on this type.
//
// For pure platform-agnostic tests, prefer [testutil.NewBot] (TB-based) or
// [testutil.NewBot] (no TB). Use this type when you need to inject raw
// *dto.Payload events or assert against the QQ OpenAPI mock.
type QQBot struct {
	*Bot
	api *MockAPI
}

// NewQQBot creates a test QQBot with a QQ MockAPI and a platform-agnostic MockSender.
func NewQQBot() *QQBot {
	return &QQBot{
		Bot: NewBot(),
		api: NewMockAPI(),
	}
}

// API returns the QQ MockAPI for QQ-specific message assertions.
func (tb *QQBot) API() *MockAPI { return tb.api }

// RegisterPlugin registers a plugin descriptor (deferred to Start).
func (tb *QQBot) RegisterPlugin(desc *plugin.PluginDescriptor) *QQBot {
	tb.Bot.RegisterPlugin(desc)
	return tb
}

// ---------------------------------------------------------------------------
// QQ-specific event injection
// ---------------------------------------------------------------------------

// SendGroupAt simulates a QQ group @-bot message via the full *dto.Payload path.
func (tb *QQBot) SendGroupAt(groupID, userOpenID, content string) {
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
func (tb *QQBot) SendC2C(userOpenID, content string) {
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
func (tb *QQBot) InjectEvent(event platform.Event) {
	tb.Engine().ProcessPlatformEvent(event, tb.SenderAPI())
}

// Inject injects an arbitrary *dto.Payload as a QQ platform event.
// It is a thin QQ-specific convenience wrapper around InjectEvent.
// For platform-agnostic injection, prefer InjectEvent.
func (tb *QQBot) Inject(payload *dto.Payload) {
	tb.InjectEvent(qqplatform.NewEvent(payload))
}

func (tb *QQBot) inject(eventType dto.EventType, event any) {
	detail, _ := json.Marshal(event)
	tb.Inject(&dto.Payload{Operation: dto.Dispatch, Type: eventType, Detail: detail})
}

// ---------------------------------------------------------------------------
// Assertion helpers
// ---------------------------------------------------------------------------

// AssertReplied asserts that a QQ message containing substr was captured by the MockAPI.
// The target parameter is accepted for documentation clarity but not checked (routing
// is asserted structurally via Sent/LastSent when needed).
func (tb *QQBot) AssertReplied(t *testing.T, target, substr string) {
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
func (tb *QQBot) AssertNotReplied(t *testing.T, target string) {
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
func (tb *QQBot) AssertPlatformReplied(t *testing.T, substr string) {
	t.Helper()
	if !tb.HasPlatformReply(substr) {
		t.Errorf("testbot: no platform message containing %q; sent=%v", substr, tb.SenderAPI().Sent())
	}
}

// ClearSent clears both the QQ MockAPI log and the platform MockSender log.
func (tb *QQBot) ClearSent() {
	tb.api.Clear()
	tb.Bot.ClearSent()
}

var _ platform.Sender = (*MockSender)(nil)
