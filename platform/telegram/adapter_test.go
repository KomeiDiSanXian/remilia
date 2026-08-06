package telegram_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/telegram"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Test helpers ────────────────────────────────────────────────────────────

// testAPI creates a test HTTP server that responds with {ok:true, result: result}
// for any Telegram Bot API method.
func testAPI(result any) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{"ok": true, "result": result}
		json.NewEncoder(w).Encode(resp)
	}))
}

// testAPIError creates a test server that returns an error response.
func testAPIError(description string, status int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{"ok": false, "description": description}
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(resp)
	}))
}

// testAPIFunc creates a test server with a custom handler.
func testAPIFunc(handler func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(handler))
}

// newTestClient creates a Client from a token and overrides its base URL.
func newTestClient(token string, srvURL string) *telegram.Client {
	c := telegram.NewClient(token)
	telegram.SetClientBaseURL(c, srvURL+"/bot"+token)
	return c
}

// newTestAdapter creates an adapter with a client pointing at the test server.
func newTestAdapter(t *testing.T, srv *httptest.Server, token string) *telegram.PollingAdapter {
	t.Helper()
	client := newTestClient(token, srv.URL)
	botUser := &telegram.User{
		ID:        12345,
		IsBot:     true,
		FirstName: "TestBot",
		Username:  "test_bot",
	}
	return telegram.NewTestAdapter(telegram.Config{Token: token, PollTimeout: 30}, client, botUser)
}

// ── Platform ID ─────────────────────────────────────────────────────────────

func TestPlatformID(t *testing.T) {
	assert.Equal(t, "telegram", telegram.PlatformID)
}

// ── NewAdapter ──────────────────────────────────────────────────────────────

func TestNewAdapter_EmptyToken(t *testing.T) {
	_, err := telegram.NewAdapter("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "token is required")
}

func TestNewPollingAdapter_EmptyConfig(t *testing.T) {
	_, err := telegram.NewPollingAdapter(telegram.Config{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "token is required")
}

func TestNewAdapter_InvalidToken(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in short mode")
	}
	_, err := telegram.NewAdapter("invalid_token_here")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "getMe failed")
}

// ── PollingAdapter ──────────────────────────────────────────────────────────

func TestPollingAdapter_Platform(t *testing.T) {
	srv := testAPI(`{"id":12345,"is_bot":true,"first_name":"Bot","username":"bot"}`)
	defer srv.Close()

	adapter := newTestAdapter(t, srv, "test:token")
	assert.Equal(t, "telegram", adapter.Platform())
}

func TestPollingAdapter_Sender(t *testing.T) {
	srv := testAPI(`{"id":12345,"is_bot":true,"first_name":"Bot","username":"bot"}`)
	defer srv.Close()

	adapter := newTestAdapter(t, srv, "test:token")
	assert.NotNil(t, adapter.Sender())
}

func TestPollingAdapter_IsRunning_Initially(t *testing.T) {
	srv := testAPI(`{"id":12345,"is_bot":true,"first_name":"Bot","username":"bot"}`)
	defer srv.Close()

	adapter := newTestAdapter(t, srv, "test:token")
	assert.False(t, adapter.IsRunning())
}

func TestPollingAdapter_BotIdentity(t *testing.T) {
	srv := testAPI(`{"id":12345,"is_bot":true,"first_name":"Bot","username":"bot"}`)
	defer srv.Close()

	adapter := newTestAdapter(t, srv, "test:token")
	assert.Equal(t, "12345", adapter.BotID())
	assert.Equal(t, "@test_bot", adapter.BotName())
}

func TestPollingAdapter_HealthDetail(t *testing.T) {
	srv := testAPI(`{"id":12345,"is_bot":true,"first_name":"Bot","username":"bot"}`)
	defer srv.Close()

	adapter := newTestAdapter(t, srv, "test:token")
	detail := adapter.HealthDetail()
	assert.Equal(t, "long_polling", detail["connection"])
	assert.Equal(t, "@test_bot", detail["bot_username"])
	assert.Equal(t, false, detail["polling_active"])
}

func TestPollingAdapter_Stop_Idempotent(t *testing.T) {
	srv := testAPI(`{"id":12345,"is_bot":true,"first_name":"Bot","username":"bot"}`)
	defer srv.Close()

	adapter := newTestAdapter(t, srv, "test:token")
	err := adapter.Stop(context.Background())
	assert.NoError(t, err)
	// Second stop should also succeed
	err = adapter.Stop(context.Background())
	assert.NoError(t, err)
}

// ── Capabilities ────────────────────────────────────────────────────────────

func TestCapabilities(t *testing.T) {
	srv := testAPI(`{"id":12345,"is_bot":true,"first_name":"Bot","username":"bot"}`)
	defer srv.Close()

	adapter := newTestAdapter(t, srv, "test:token")
	caps := adapter.Capabilities()
	assert.True(t, caps.Markdown)
	assert.True(t, caps.Buttons)
	assert.True(t, caps.MultiAttachment)
	assert.True(t, caps.MessageEdit)
	assert.True(t, caps.MessageDelete)
	assert.True(t, caps.FileUpload)
	assert.True(t, caps.Reactions)
	assert.True(t, caps.ThreadReply)
	assert.True(t, caps.TypingIndicator)
	assert.False(t, caps.Embeds)
	assert.False(t, caps.GuildSupport)
	assert.False(t, caps.MentionAll)
	assert.False(t, caps.VoiceChannel)
	assert.Equal(t, 4096, caps.MaxTextLength)
	assert.Equal(t, 50, caps.MaxAttachmentMB)
}

// ── Event mapping ───────────────────────────────────────────────────────────

func TestNewEvent_Nil(t *testing.T) {
	assert.Nil(t, telegram.NewEvent(nil))
}

func TestNewEvent_Unsupported(t *testing.T) {
	// Empty update with no recognized fields
	upd := &telegram.Update{UpdateID: 1}
	assert.Nil(t, telegram.NewEvent(upd))
}

func TestNewEvent_PrivateMessage(t *testing.T) {
	msg := &telegram.Message{
		MessageID: 100,
		From:      &telegram.User{ID: 1000, FirstName: "Alice", Username: "alice"},
		Chat:      &telegram.Chat{ID: 1000, Type: telegram.ChatTypePrivate, FirstName: "Alice"},
		Date:      time.Now().Unix(),
		Text:      "Hello, bot!",
	}
	upd := &telegram.Update{Message: msg}

	evt := telegram.NewEvent(upd)
	require.NotNil(t, evt)

	assert.Equal(t, "telegram", evt.Platform())
	assert.Equal(t, platform.EventKindPrivateMessage, evt.Kind())
	assert.Equal(t, "100", evt.ID())
	assert.Equal(t, "Hello, bot!", platform.Content(evt))
	assert.False(t, evt.Timestamp().IsZero())
	assert.Equal(t, "1000", evt.Sender().ID)
	assert.Equal(t, "Alice (@alice)", evt.Sender().DisplayName)
	assert.False(t, evt.Sender().IsBot)
	assert.Equal(t, "1000", evt.Chat().ID)
	assert.False(t, evt.Chat().IsGroup)

	// Optional interfaces
	re, ok := evt.(platform.RawEvent)
	assert.True(t, ok)
	assert.Equal(t, "message", re.RawType())
	assert.NotNil(t, re.RawPayload())
}

func TestNewEvent_GroupMessage(t *testing.T) {
	msg := &telegram.Message{
		MessageID:  200,
		From:       &telegram.User{ID: 2000, FirstName: "Bob"},
		Chat:       &telegram.Chat{ID: -100, Type: telegram.ChatTypeSupergroup, Title: "Test Group"},
		Date:       time.Now().Unix(),
		Text:       "Group message",
		ReplyToMsg: &telegram.Message{MessageID: 199},
	}
	upd := &telegram.Update{Message: msg}

	evt := telegram.NewEvent(upd)
	require.NotNil(t, evt)

	assert.Equal(t, platform.EventKindGroupMessage, evt.Kind())
	assert.True(t, evt.Chat().IsGroup)
	assert.Equal(t, "Test Group", evt.Chat().Name)

	// ReplyEvent
	rep, ok := evt.(platform.ReplyEvent)
	assert.True(t, ok)
	assert.Equal(t, "199", rep.ReplyToID())
}

func TestNewEvent_EditedMessage(t *testing.T) {
	msg := &telegram.Message{
		MessageID: 300,
		From:      &telegram.User{ID: 3000, FirstName: "Charlie"},
		Chat:      &telegram.Chat{ID: 3000, Type: telegram.ChatTypePrivate, FirstName: "Charlie"},
		Date:      time.Now().Unix(),
		Text:      "edited text",
	}
	upd := &telegram.Update{EditedMessage: msg}

	evt := telegram.NewEvent(upd)
	require.NotNil(t, evt)

	// 编辑消息映射为 MESSAGE_UPDATE，与 doc.go 的事件映射表及 Discord 适配器一致。
	// 若仍按普通消息投递，一条被编辑成 "/ban" 的旧消息会重新触发命令。
	assert.Equal(t, platform.EventKindMessageUpdate, evt.Kind())

	ee, ok := evt.(platform.EditableEvent)
	assert.True(t, ok)
	assert.True(t, ee.IsEdited())
	assert.False(t, ee.OriginalTimestamp().IsZero())

	re, ok := evt.(platform.RawEvent)
	assert.True(t, ok)
	assert.Equal(t, "edited_message", re.RawType())
}

func TestNewEvent_ChannelPost(t *testing.T) {
	msg := &telegram.Message{
		MessageID: 400,
		Chat:      &telegram.Chat{ID: -200, Type: telegram.ChatTypeChannel, Title: "News Channel"},
		Date:      time.Now().Unix(),
		Text:      "Channel update",
	}
	upd := &telegram.Update{ChannelPost: msg}

	evt := telegram.NewEvent(upd)
	require.NotNil(t, evt)

	assert.Equal(t, platform.EventKindGuildMessage, evt.Kind())
	assert.True(t, evt.Chat().IsGroup)
}

func TestNewEvent_CallbackQuery(t *testing.T) {
	cq := &telegram.CallbackQuery{
		ID:   "callback_123",
		From: &telegram.User{ID: 4000, FirstName: "Diana"},
		Data: "btn_data",
		Message: &telegram.Message{
			MessageID: 500,
			Chat:      &telegram.Chat{ID: 4000, Type: telegram.ChatTypePrivate, FirstName: "Diana"},
			Date:      time.Now().Unix(),
		},
	}
	upd := &telegram.Update{CallbackQuery: cq}

	evt := telegram.NewEvent(upd)
	require.NotNil(t, evt)

	assert.Equal(t, platform.EventKindInteraction, evt.Kind())
	assert.Equal(t, "callback_123", evt.ID())
	assert.Equal(t, "btn_data", platform.Content(evt))
	assert.Equal(t, "4000", evt.Chat().ID)

	// Token should be set
	assert.Equal(t, "callback_123", evt.Chat().Tokens["callback_id"])
}

func TestNewEvent_BotAdded(t *testing.T) {
	upd := &telegram.Update{
		MyChatMember: &telegram.ChatMemberUpdated{
			Chat: &telegram.Chat{ID: -100, Type: telegram.ChatTypeSupergroup, Title: "Group"},
			From: &telegram.User{ID: 5000, FirstName: "Admin"},
			Date: time.Now().Unix(),
			NewChatMember: &telegram.ChatMember{
				User:   &telegram.User{ID: 12345, FirstName: "Bot", IsBot: true},
				Status: "member",
			},
		},
	}

	evt := telegram.NewEvent(upd)
	require.NotNil(t, evt)

	assert.Equal(t, platform.EventKindBotAdded, evt.Kind())
}

func TestNewEvent_BotRemoved(t *testing.T) {
	upd := &telegram.Update{
		MyChatMember: &telegram.ChatMemberUpdated{
			Chat: &telegram.Chat{ID: -100, Type: telegram.ChatTypeSupergroup, Title: "Group"},
			Date: time.Now().Unix(),
			NewChatMember: &telegram.ChatMember{
				User:   &telegram.User{ID: 12345, FirstName: "Bot", IsBot: true},
				Status: "kicked",
			},
		},
	}

	evt := telegram.NewEvent(upd)
	require.NotNil(t, evt)

	assert.Equal(t, platform.EventKindBotRemoved, evt.Kind())
}

func TestNewEvent_MyChatMember_UnsupportedStatus(t *testing.T) {
	upd := &telegram.Update{
		MyChatMember: &telegram.ChatMemberUpdated{
			Chat: &telegram.Chat{ID: -100, Type: telegram.ChatTypeSupergroup},
			Date: time.Now().Unix(),
			NewChatMember: &telegram.ChatMember{
				Status: "restricted",
			},
		},
	}

	evt := telegram.NewEvent(upd)
	assert.Nil(t, evt, "restricted status should not produce an event")
}

// ── User/chat conversion ────────────────────────────────────────────────────

func TestUserFromTelegram_Nil(t *testing.T) {
	u := telegram.UserFromTelegram(nil)
	assert.Equal(t, "", u.ID)
	assert.Equal(t, "", u.DisplayName)
}

func TestUserFromTelegram(t *testing.T) {
	tu := &telegram.User{ID: 42, FirstName: "Test", LastName: "User", Username: "tester"}
	u := telegram.UserFromTelegram(tu)
	assert.Equal(t, "42", u.ID)
	assert.Contains(t, u.DisplayName, "Test User")
	assert.Contains(t, u.DisplayName, "@tester")
	assert.False(t, u.IsBot)
}

func TestUserFromTelegram_Bot(t *testing.T) {
	tu := &telegram.User{ID: 99, FirstName: "Bot", IsBot: true}
	u := telegram.UserFromTelegram(tu)
	assert.True(t, u.IsBot)
}

func TestChatFromTelegram_Private(t *testing.T) {
	tc := &telegram.Chat{ID: 100, Type: telegram.ChatTypePrivate, FirstName: "Alice"}
	c := telegram.ChatFromTelegram(tc)
	assert.Equal(t, "100", c.ID)
	assert.False(t, c.IsGroup)
	assert.Equal(t, "Alice", c.Name)
}

func TestChatFromTelegram_Group(t *testing.T) {
	tc := &telegram.Chat{ID: -200, Type: telegram.ChatTypeSupergroup, Title: "Test Group"}
	c := telegram.ChatFromTelegram(tc)
	assert.Equal(t, "-200", c.ID)
	assert.True(t, c.IsGroup)
	assert.Equal(t, "Test Group", c.Name)
}

func TestChatFromTelegram_Channel(t *testing.T) {
	tc := &telegram.Chat{ID: -300, Type: telegram.ChatTypeChannel, Title: "News"}
	c := telegram.ChatFromTelegram(tc)
	assert.Equal(t, "-300", c.ID)
	assert.True(t, c.IsGroup)
}

// ── CollectAttachments ──────────────────────────────────────────────────────

func TestCollectAttachments_None(t *testing.T) {
	msg := &telegram.Message{MessageID: 1}
	atts := telegram.CollectAttachments(msg)
	assert.Empty(t, atts)
}

// assertFileID 断言附件的 Telegram file_id。
//
// file_id 不再放在 Attachment.URL 里：它不是可下载的 URL，
// 跨平台插件对其执行 http.Get 只会得到 unsupported protocol scheme。
// 现在它挂在 Extra 的 *telegram.FileMeta 上，URL 由适配器调用
// getFile 解析后回填。
func assertFileID(t *testing.T, att platform.Attachment, want string) {
	t.Helper()
	meta, ok := att.Extra[telegram.ExtraKeyFile].(*telegram.FileMeta)
	require.True(t, ok, "附件 Extra[ExtraKeyFile] 应为 *telegram.FileMeta")
	assert.Equal(t, want, meta.FileID)
	assert.Empty(t, att.URL, "URL 应留待适配器解析后回填")
}

func TestCollectAttachments_Photo(t *testing.T) {
	msg := &telegram.Message{
		Photo: []telegram.PhotoSize{
			{FileID: "small", Width: 100, Height: 100, FileSize: 1000},
			{FileID: "large", Width: 800, Height: 600, FileSize: 50000},
		},
	}
	atts := telegram.CollectAttachments(msg)
	require.Len(t, atts, 1)
	assertFileID(t, atts[0], "large")
	assert.Equal(t, 800, atts[0].Width)
	assert.Equal(t, 600, atts[0].Height)
	assert.Equal(t, 50000, atts[0].Size)
}

func TestCollectAttachments_Audio(t *testing.T) {
	msg := &telegram.Message{
		Audio: &telegram.Audio{
			FileID: "audio_id", MimeType: "audio/mpeg", FileName: "song.mp3", FileSize: 100000,
		},
	}
	atts := telegram.CollectAttachments(msg)
	require.Len(t, atts, 1)
	assertFileID(t, atts[0], "audio_id")
	assert.Equal(t, "audio/mpeg", atts[0].MimeType)
	assert.Equal(t, "song.mp3", atts[0].Name)
}

func TestCollectAttachments_Multiple(t *testing.T) {
	msg := &telegram.Message{
		Photo: []telegram.PhotoSize{{FileID: "p1", Width: 200, Height: 150}},
		Video: &telegram.Video{FileID: "v1", Width: 1920, Height: 1080, MimeType: "video/mp4"},
	}
	atts := telegram.CollectAttachments(msg)
	require.Len(t, atts, 2)
	assertFileID(t, atts[0], "p1")
	assertFileID(t, atts[1], "v1")
}

func TestCollectAttachments_Document(t *testing.T) {
	msg := &telegram.Message{
		Document: &telegram.Document{
			FileID: "doc_id", FileName: "report.pdf", MimeType: "application/pdf", FileSize: 50000,
		},
	}
	atts := telegram.CollectAttachments(msg)
	require.Len(t, atts, 1)
	assertFileID(t, atts[0], "doc_id")
	assert.Equal(t, "application/pdf", atts[0].MimeType)
	assert.Equal(t, "report.pdf", atts[0].Name)
}

func TestCollectAttachments_Voice(t *testing.T) {
	msg := &telegram.Message{
		Voice: &telegram.Voice{
			FileID: "voice_id", MimeType: "audio/ogg", FileSize: 20000,
		},
	}
	atts := telegram.CollectAttachments(msg)
	require.Len(t, atts, 1)
	assertFileID(t, atts[0], "voice_id")
}

func TestCollectAttachments_Animation(t *testing.T) {
	msg := &telegram.Message{
		Animation: &telegram.Animation{
			FileID: "anim_id", Width: 320, Height: 240, FileSize: 15000,
		},
	}
	atts := telegram.CollectAttachments(msg)
	require.Len(t, atts, 1)
	assertFileID(t, atts[0], "anim_id")
	assert.Equal(t, 320, atts[0].Width)
	assert.Equal(t, 240, atts[0].Height)
}

func TestCollectAttachments_Sticker(t *testing.T) {
	msg := &telegram.Message{
		Sticker: &telegram.Sticker{
			FileID: "sticker_id", Width: 512, Height: 512, FileSize: 30000,
		},
	}
	atts := telegram.CollectAttachments(msg)
	require.Len(t, atts, 1)
	assertFileID(t, atts[0], "sticker_id")
	assert.Equal(t, 512, atts[0].Width)
	assert.Equal(t, 512, atts[0].Height)
}

// ── Message ID parsing ──────────────────────────────────────────────────────

func TestParseMessageID_Empty(t *testing.T) {
	assert.Equal(t, 0, telegram.ParseMessageID(""))
}

func TestParseMessageID_Valid(t *testing.T) {
	assert.Equal(t, 42, telegram.ParseMessageID("42"))
}

func TestParseMessageID_Invalid(t *testing.T) {
	assert.Equal(t, 0, telegram.ParseMessageID("abc"))
}

// ── ExtractMessageID ────────────────────────────────────────────────────────

func TestExtractMessageID_Empty(t *testing.T) {
	assert.Equal(t, "", telegram.ExtractMessageID(nil))
	assert.Equal(t, "", telegram.ExtractMessageID([]byte{}))
}

func TestExtractMessageID_Valid(t *testing.T) {
	data := []byte(`{"message_id":123,"text":"hello"}`)
	assert.Equal(t, "123", telegram.ExtractMessageID(data))
}

func TestExtractMessageID_NoMatch(t *testing.T) {
	data := []byte(`{"ok":true}`)
	assert.Equal(t, "", telegram.ExtractMessageID(data))
}

// ── ExtensionFromMIME ───────────────────────────────────────────────────────

func TestExtensionFromMIME(t *testing.T) {
	tests := []struct {
		mime     string
		expected string
	}{
		{"image/jpeg", ".jpg"},
		{"image/png", ".png"},
		{"image/gif", ".gif"},
		{"image/webp", ".webp"},
		{"audio/mpeg", ".mp3"},
		{"audio/ogg", ".ogg"},
		{"audio/wav", ".wav"},
		{"video/mp4", ".mp4"},
		{"video/webm", ".webm"},
		{"application/pdf", ".pdf"},
		{"application/zip", ".zip"},
		{"unknown/type", ".fallback"},
	}
	for _, tt := range tests {
		t.Run(tt.mime, func(t *testing.T) {
			assert.Equal(t, tt.expected, telegram.ExtensionFromMIME(tt.mime, ".fallback"))
		})
	}
}

// ── BuildInlineKeyboard ─────────────────────────────────────────────────────

func TestBuildInlineKeyboard_Empty(t *testing.T) {
	assert.Nil(t, telegram.BuildInlineKeyboard(nil))
	assert.Nil(t, telegram.BuildInlineKeyboard([]platform.Button{}))
}

func TestBuildInlineKeyboard_SingleButton(t *testing.T) {
	buttons := []platform.Button{
		{ID: "btn1", Label: "Click Me", Style: platform.ButtonStylePrimary},
	}
	kb := telegram.BuildInlineKeyboard(buttons)
	require.NotNil(t, kb)
	require.Len(t, kb.InlineKeyboard, 1)
	require.Len(t, kb.InlineKeyboard[0], 1)
	assert.Equal(t, "Click Me", kb.InlineKeyboard[0][0].Text)
	assert.Equal(t, "btn1", kb.InlineKeyboard[0][0].CallbackData)
}

func TestBuildInlineKeyboard_LinkButton(t *testing.T) {
	buttons := []platform.Button{
		{Label: "Visit", URL: "https://example.com", Style: platform.ButtonStyleLink},
	}
	kb := telegram.BuildInlineKeyboard(buttons)
	require.NotNil(t, kb)
	assert.Equal(t, "https://example.com", kb.InlineKeyboard[0][0].URL)
	assert.Empty(t, kb.InlineKeyboard[0][0].CallbackData)
}

func TestBuildInlineKeyboard_MultipleRows(t *testing.T) {
	buttons := []platform.Button{
		{ID: "a", Label: "A", Row: 1},
		{ID: "b", Label: "B", Row: 1},
		{ID: "c", Label: "C", Row: 2},
	}
	kb := telegram.BuildInlineKeyboard(buttons)
	require.NotNil(t, kb)
	require.Len(t, kb.InlineKeyboard, 2)
	require.Len(t, kb.InlineKeyboard[0], 2)
	require.Len(t, kb.InlineKeyboard[1], 1)
}

func TestBuildInlineKeyboard_AutoRow(t *testing.T) {
	buttons := []platform.Button{
		{ID: "a", Label: "A"},
		{ID: "b", Label: "B"},
	}
	kb := telegram.BuildInlineKeyboard(buttons)
	require.NotNil(t, kb)
	// Each button with Row 0 gets its own row
	require.Len(t, kb.InlineKeyboard, 2)
}

func TestBuildInlineKeyboard_WithExtra(t *testing.T) {
	buttons := []platform.Button{
		{
			ID:    "search",
			Label: "Search",
			Extra: map[string]any{telegram.ExtraKeyInline: &telegram.InlineButtonExtra{SwitchInlineQuery: "query text"}},
		},
	}
	kb := telegram.BuildInlineKeyboard(buttons)
	require.NotNil(t, kb)
	assert.Equal(t, "query text", kb.InlineKeyboard[0][0].SwitchInline)
}

// ── FormatChatID ────────────────────────────────────────────────────────────

func TestFormatChatID(t *testing.T) {
	assert.Equal(t, "42", telegram.FormatChatID(42))
	assert.Equal(t, "-100", telegram.FormatChatID(-100))
	assert.Equal(t, "0", telegram.FormatChatID(0))
}

// ── Client ──────────────────────────────────────────────────────────────────

func TestClient_NewClient(t *testing.T) {
	c := telegram.NewClient("token123")
	assert.NotNil(t, c)
}

func TestClient_GetMe(t *testing.T) {
	srv := testAPI(telegram.User{ID: 1, FirstName: "Bot", IsBot: true, Username: "my_bot"})
	defer srv.Close()

	client := newTestClient("test:token", srv.URL)
	user, err := client.GetMe(context.Background())
	require.NoError(t, err)
	require.NotNil(t, user)
	assert.Equal(t, int64(1), user.ID)
	assert.True(t, user.IsBot)
	assert.Equal(t, "my_bot", user.Username)
}

func TestClient_GetUpdates(t *testing.T) {
	updates := []telegram.Update{
		{UpdateID: 1, Message: &telegram.Message{
			MessageID: 10, Text: "hello",
			Chat: &telegram.Chat{ID: 100, Type: telegram.ChatTypePrivate},
			Date: time.Now().Unix(),
		}},
	}
	srv := testAPI(updates)
	defer srv.Close()

	client := newTestClient("test:token", srv.URL)
	result, err := client.GetUpdates(context.Background(), 0, 10, 100)
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, int64(1), result[0].UpdateID)
}

func TestClient_GetUpdates_Error(t *testing.T) {
	srv := testAPIError("Conflict: terminated", http.StatusConflict)
	defer srv.Close()

	client := newTestClient("test:token", srv.URL)
	_, err := client.GetUpdates(context.Background(), 0, 10, 100)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API error")
}

func TestClient_SendMessage(t *testing.T) {
	srv := testAPIFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"result": map[string]any{
				"message_id": 42,
				"text":       "hello",
			},
		})
	})
	defer srv.Close()

	client := newTestClient("test:token", srv.URL)
	resp, err := client.SendMessage(context.Background(), &telegram.SendMessagePayload{
		ChatID: "100",
		Text:   "hello",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, resp)
	assert.Equal(t, "42", telegram.ExtractMessageID(resp))
}

func TestClient_DeleteMessage(t *testing.T) {
	srv := testAPI(true)
	defer srv.Close()

	client := newTestClient("test:token", srv.URL)
	err := client.DeleteMessage(context.Background(), &telegram.DeleteMessagePayload{
		ChatID: "100", MessageID: 42,
	})
	assert.NoError(t, err)
}

func TestClient_SendChatAction(t *testing.T) {
	srv := testAPI(true)
	defer srv.Close()

	client := newTestClient("test:token", srv.URL)
	err := client.SendChatAction(context.Background(), &telegram.SendChatActionPayload{
		ChatID: "100", Action: "typing",
	})
	assert.NoError(t, err)
}

func TestClient_AnswerCallbackQuery(t *testing.T) {
	srv := testAPI(true)
	defer srv.Close()

	client := newTestClient("test:token", srv.URL)
	err := client.AnswerCallbackQuery(context.Background(), &telegram.AnswerCallbackQueryPayload{
		CallbackQueryID: "cb_123", Text: "Done",
	})
	assert.NoError(t, err)
}

// ── User type helpers ───────────────────────────────────────────────────────

func TestUser_UserName(t *testing.T) {
	u := &telegram.User{Username: "test_bot"}
	assert.Equal(t, "@test_bot", u.UserName())
}

func TestUser_UserName_NoUsername(t *testing.T) {
	u := &telegram.User{FirstName: "Bot"}
	assert.Equal(t, "Bot", u.UserName())
}

func TestUser_DisplayName(t *testing.T) {
	u := &telegram.User{FirstName: "Alice", LastName: "Smith", Username: "alice"}
	dn := u.DisplayName()
	assert.Contains(t, dn, "Alice Smith")
	assert.Contains(t, dn, "@alice")
}

func TestUser_DisplayName_Simple(t *testing.T) {
	u := &telegram.User{FirstName: "Bot"}
	assert.Equal(t, "Bot", u.DisplayName())
}

// ── Chat type helpers ───────────────────────────────────────────────────────

func TestChat_DisplayName_Title(t *testing.T) {
	c := &telegram.Chat{Title: "Group Chat"}
	assert.Equal(t, "Group Chat", c.DisplayName())
}

func TestChat_DisplayName_Personal(t *testing.T) {
	c := &telegram.Chat{FirstName: "Alice", LastName: "Smith"}
	assert.Equal(t, "Alice Smith", c.DisplayName())
}

func TestChat_DisplayName_FirstNameOnly(t *testing.T) {
	c := &telegram.Chat{FirstName: "Alice"}
	assert.Equal(t, "Alice", c.DisplayName())
}

// ── Extra ───────────────────────────────────────────────────────────────────

func TestApplyExtra_RoundTrip(t *testing.T) {
	msg := platform.TextMessage("test")
	extra := telegram.MessageExtra{DisableNotification: true, DisableWebPreview: true}
	msg = telegram.ApplyExtra(msg, extra)

	// Verify the message was modified
	assert.NotNil(t, msg.Extra)
	assert.Equal(t, "test", msg.Text)

	// We can't directly extractExtra from test because it's unexported.
	// But we verify the message structure is correct.
}

// ── Event with attachments integration ──────────────────────────────────────

func TestEvent_WithAttachments(t *testing.T) {
	msg := &telegram.Message{
		MessageID: 1,
		Chat:      &telegram.Chat{ID: 100, Type: telegram.ChatTypePrivate},
		Date:      time.Now().Unix(),
		Text:      "Check this",
		Photo:     []telegram.PhotoSize{{FileID: "pic1", Width: 800, Height: 600}},
		Document:  &telegram.Document{FileID: "doc1", FileName: "file.pdf"},
	}
	upd := &telegram.Update{Message: msg}
	evt := telegram.NewEvent(upd)
	require.NotNil(t, evt)

	atts := platform.Attachments(evt)
	require.Len(t, atts, 2)
	assertFileID(t, atts[0], "pic1")
	assertFileID(t, atts[1], "doc1")
}

// ── Event with mentions ─────────────────────────────────────────────────────

// TestEvent_WithTextMention 覆盖 "text_mention" 实体：这是唯一自带 User 对象的
// @ 实体类型（Bot API 原文：user 字段 "For text_mention only"）。
func TestEvent_WithTextMention(t *testing.T) {
	msg := &telegram.Message{
		MessageID: 1,
		Chat:      &telegram.Chat{ID: 100, Type: telegram.ChatTypeGroup, Title: "Group"},
		Date:      time.Now().Unix(),
		Text:      "User hello",
		Entities: []telegram.MessageEntity{
			{Type: "text_mention", Offset: 0, Length: 4, User: &telegram.User{ID: 42, FirstName: "User"}},
		},
	}
	upd := &telegram.Update{Message: msg}
	evt := telegram.NewEvent(upd)
	require.NotNil(t, evt)

	me, ok := evt.(platform.MentionsEvent)
	require.True(t, ok)
	mentions := me.Mentions()
	require.Len(t, mentions, 1)
	assert.Equal(t, "42", mentions[0].ID)
}

// TestEvent_WithBotMentionIsSelf 注入 botID 后，@ 机器人自身的条目标记 IsSelf=true，
// OnMentionedBot 在 Telegram 上可命中。
func TestEvent_WithBotMentionIsSelf(t *testing.T) {
	msg := &telegram.Message{
		MessageID: 1,
		Chat:      &telegram.Chat{ID: 100, Type: telegram.ChatTypeGroup, Title: "Group"},
		Date:      time.Now().Unix(),
		Text:      "user hello",
		Entities: []telegram.MessageEntity{
			{Type: "text_mention", Offset: 0, Length: 4, User: &telegram.User{ID: 42, FirstName: "User"}},
		},
	}
	upd := &telegram.Update{Message: msg}

	// 不带 botID：IsSelf 不标记
	plain := telegram.NewEvent(upd)
	ms := plain.(platform.MentionsEvent).Mentions()
	require.Len(t, ms, 1)
	assert.False(t, ms[0].IsSelf)

	// 注入 botID（=被 @ 的 user id 42）：IsSelf=true
	withBot := telegram.NewEventWithBot(upd, "42")
	ms = withBot.(platform.MentionsEvent).Mentions()
	require.Len(t, ms, 1)
	assert.True(t, ms[0].IsSelf, "@ 机器人自身（botID 匹配）应标记 IsSelf")

	// botID 不匹配他人：不标记
	other := telegram.NewEventWithBot(upd, "999")
	ms = other.(platform.MentionsEvent).Mentions()
	require.Len(t, ms, 1)
	assert.False(t, ms[0].IsSelf)
}

// TestEvent_WithUsernameMention 覆盖 "mention" 实体：真实报文中它**不带** User，
// 只能按 offset/length 从正文中切出 "@username"。
//
// 旧实现要求 Type=="mention" && User!=nil，该条件对真实报文恒为假，
// 于是 Mentions() 永远为空、OnMentionedBot() 之类的规则在 Telegram 上永不命中。
func TestEvent_WithUsernameMention(t *testing.T) {
	msg := &telegram.Message{
		MessageID: 1,
		Chat:      &telegram.Chat{ID: 100, Type: telegram.ChatTypeGroup, Title: "Group"},
		Date:      time.Now().Unix(),
		Text:      "@user hello",
		Entities: []telegram.MessageEntity{
			{Type: "mention", Offset: 0, Length: 5},
		},
	}
	upd := &telegram.Update{Message: msg}
	evt := telegram.NewEvent(upd)
	require.NotNil(t, evt)

	me, ok := evt.(platform.MentionsEvent)
	require.True(t, ok)
	mentions := me.Mentions()
	require.Len(t, mentions, 1)
	assert.Equal(t, "user", mentions[0].DisplayName)
}

// TestEvent_MentionOffsetIsUTF16 固定 offset/length 的单位语义。
//
// Telegram 的实体偏移量以 UTF-16 代码单元计。"🎉" 在 UTF-16 中占 2 个单元、
// 在 UTF-8 中占 4 字节、按 rune 只算 1 个——三者互不相同：若按字节或 rune 切片，
// 这里切出来的都不会是 "@user"。
func TestEvent_MentionOffsetIsUTF16(t *testing.T) {
	msg := &telegram.Message{
		MessageID: 1,
		Chat:      &telegram.Chat{ID: 100, Type: telegram.ChatTypeGroup, Title: "Group"},
		Date:      time.Now().Unix(),
		Text:      "🎉 @user hello",
		Entities: []telegram.MessageEntity{
			{Type: "mention", Offset: 3, Length: 5}, // 🎉 占 2 个单元 + 空格 1 个
		},
	}
	upd := &telegram.Update{Message: msg}
	evt := telegram.NewEvent(upd)
	require.NotNil(t, evt)

	me, ok := evt.(platform.MentionsEvent)
	require.True(t, ok)
	mentions := me.Mentions()
	require.Len(t, mentions, 1)
	assert.Equal(t, "user", mentions[0].DisplayName)
}

// TestEvent_MentionOutOfRangeIgnored 越界实体应被跳过而非 panic。
func TestEvent_MentionOutOfRangeIgnored(t *testing.T) {
	msg := &telegram.Message{
		MessageID: 1,
		Chat:      &telegram.Chat{ID: 100, Type: telegram.ChatTypeGroup, Title: "Group"},
		Date:      time.Now().Unix(),
		Text:      "hi",
		Entities: []telegram.MessageEntity{
			{Type: "mention", Offset: 0, Length: 99},
			{Type: "mention", Offset: -1, Length: 2},
		},
	}
	upd := &telegram.Update{Message: msg}
	evt := telegram.NewEvent(upd)
	require.NotNil(t, evt)

	me, ok := evt.(platform.MentionsEvent)
	require.True(t, ok)
	assert.Empty(t, me.Mentions())
}

// ── Client request verification ─────────────────────────────────────────────

func TestClient_SendMessage_RequestMethod(t *testing.T) {
	srv := testAPIFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{}})
	})
	defer srv.Close()

	client := newTestClient("test:token", srv.URL)
	_, err := client.SendMessage(context.Background(), &telegram.SendMessagePayload{
		ChatID: "100", Text: "test",
	})
	assert.NoError(t, err)
}

func TestClient_SendMessage_RequestBody(t *testing.T) {
	srv := testAPIFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		assert.Equal(t, "100", body["chat_id"])
		assert.Equal(t, "hello", body["text"])
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{}})
	})
	defer srv.Close()

	client := newTestClient("test:token", srv.URL)
	_, err := client.SendMessage(context.Background(), &telegram.SendMessagePayload{
		ChatID: "100", Text: "hello",
	})
	assert.NoError(t, err)
}

// ── 出站段路径：Segments 优先、保序、交错 at 保真 ─────────────────────

func TestSender_SegmentsInterleavedAt(t *testing.T) {
	srv := testAPIFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		assert.Equal(t, "100", body["chat_id"])
		// at 段降级为 "@ID" 文本，保序拼接（原文不含 at 后空格）
		assert.Equal(t, "@42一段文本 @7文本...", body["text"])
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"message_id": 1}})
	})
	defer srv.Close()

	sender := telegram.NewSender(newTestClient("test:token", srv.URL), "bot1")
	_, err := sender.Send(context.Background(), platform.SendRequest{
		Target: platform.ChatInfo{ID: "100"},
		Message: platform.OutboundMessage{Segments: []platform.Segment{
			{Type: platform.SegmentAt, UserID: "42"},
			{Type: platform.SegmentText, Text: "一段文本 "},
			{Type: platform.SegmentAt, UserID: "7"},
			{Type: platform.SegmentText, Text: "文本..."},
		}},
	})
	assert.NoError(t, err)
}

func TestSender_SegmentsReplyToMessageID(t *testing.T) {
	srv := testAPIFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		assert.Equal(t, float64(555), body["reply_to_message_id"])
		assert.Equal(t, "hi", body["text"])
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"message_id": 2}})
	})
	defer srv.Close()

	sender := telegram.NewSender(newTestClient("test:token", srv.URL), "bot1")
	_, err := sender.Send(context.Background(), platform.SendRequest{
		Target: platform.ChatInfo{ID: "100"},
		Message: platform.OutboundMessage{Segments: []platform.Segment{
			{Type: platform.SegmentReply, ReplyToID: "555"},
			{Type: platform.SegmentText, Text: "hi"},
		}},
	})
	assert.NoError(t, err)
}

// ── Context cancellation ────────────────────────────────────────────────────

func TestClient_ContextCancellation(t *testing.T) {
	srv := testAPIFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": nil})
	})
	defer srv.Close()

	client := newTestClient("test:token", srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancel

	_, err := client.GetUpdates(ctx, 0, 30, 100)
	require.Error(t, err)
	// Should be a context canceled error
	assert.Contains(t, err.Error(), "context canceled")
}

// ── Adapter Start/Stop lifecycle ────────────────────────────────────────────

func TestAdapter_StartStop(t *testing.T) {
	msg := &telegram.Message{
		MessageID: 1, Text: "ping",
		Chat: &telegram.Chat{ID: 100, Type: telegram.ChatTypePrivate},
		Date: time.Now().Unix(),
	}
	updates := []telegram.Update{{UpdateID: 1, Message: msg}}

	srv := testAPI(updates)
	defer srv.Close()

	adapter := newTestAdapter(t, srv, "test:token")

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	eventCh := make(chan platform.Event, 1)
	go func() {
		adapter.Start(ctx, func(e platform.Event) {
			select {
			case eventCh <- e:
			default:
			}
		})
	}()

	select {
	case evt := <-eventCh:
		require.NotNil(t, evt)
		assert.Equal(t, "ping", platform.Content(evt))
	case <-ctx.Done():
		t.Log("no event received within timeout (expected if polling returns empty)")
	}

	adapter.Stop(context.Background())
}

// ── PollingAdapter Capabilities consistency ─────────────────────────────────

func TestPollingAdapter_CapabilitiesWithAdapter(t *testing.T) {
	srv := testAPI(`{"id":12345,"is_bot":true,"first_name":"Bot","username":"bot"}`)
	defer srv.Close()

	adapter := newTestAdapter(t, srv, "test:token")
	caps := adapter.Capabilities()
	assert.False(t, caps.Embeds, "Telegram does not support Discord-style embeds")
	assert.False(t, caps.GuildSupport, "Telegram does not have guild/server hierarchy")
	assert.False(t, caps.MentionAll, "Telegram does not support @everyone")
	assert.False(t, caps.VoiceChannel, "Telegram does not have voice channels")
}

// ── Edge cases ──────────────────────────────────────────────────────────────

func TestNewEvent_Message_NoFrom(t *testing.T) {
	// Channel posts have no "from" field
	msg := &telegram.Message{
		MessageID: 1,
		Chat:      &telegram.Chat{ID: -200, Type: telegram.ChatTypeChannel, Title: "Channel"},
		Date:      time.Now().Unix(),
		Text:      "Channel post",
	}
	upd := &telegram.Update{ChannelPost: msg}
	evt := telegram.NewEvent(upd)
	require.NotNil(t, evt)
	assert.Empty(t, evt.Sender().ID, "no sender for channel posts")
}

func TestNewEvent_CallbackQuery_NoMessage(t *testing.T) {
	// Callback queries from inline queries don't have a message
	cq := &telegram.CallbackQuery{
		ID: "cb_1", From: &telegram.User{ID: 1, FirstName: "User"}, Data: "data",
	}
	upd := &telegram.Update{CallbackQuery: cq}
	evt := telegram.NewEvent(upd)
	require.NotNil(t, evt)
	assert.Empty(t, evt.Chat().ID, "no chat for inline callback queries")
}

func TestExtractMessageID_InvalidJSON(t *testing.T) {
	assert.Equal(t, "", telegram.ExtractMessageID([]byte(`{invalid`)))
}

// ── Benchmarks ──────────────────────────────────────────────────────────────

func BenchmarkNewEvent_PrivateMessage(b *testing.B) {
	msg := &telegram.Message{
		MessageID: 100,
		From:      &telegram.User{ID: 1000, FirstName: "Alice", Username: "alice"},
		Chat:      &telegram.Chat{ID: 1000, Type: telegram.ChatTypePrivate, FirstName: "Alice"},
		Date:      time.Now().Unix(),
		Text:      "Hello, bot!",
	}
	upd := &telegram.Update{Message: msg}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		telegram.NewEvent(upd)
	}
}

func BenchmarkExtractMessageID(b *testing.B) {
	data := []byte(`{"message_id":12345,"text":"hello world","chat":{"id":42}}`)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		telegram.ExtractMessageID(data)
	}
}

func BenchmarkBuildInlineKeyboard(b *testing.B) {
	buttons := make([]platform.Button, 10)
	for i := range buttons {
		buttons[i] = platform.Button{
			ID:    fmt.Sprintf("btn_%d", i),
			Label: fmt.Sprintf("Button %d", i),
			Row:   i % 3,
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		telegram.BuildInlineKeyboard(buttons)
	}
}
