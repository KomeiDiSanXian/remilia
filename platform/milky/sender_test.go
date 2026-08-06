package milky

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/errutil"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// newTestSender 创建指向模拟服务器的 milkySender。
func newTestSender(t *testing.T, m *mockMilkyServer) *milkySender {
	t.Helper()
	a := newTestAdapter(t, m)
	return a.sender
}

func sendReq(chat platform.ChatInfo, msg platform.OutboundMessage) platform.SendRequest {
	return platform.SendRequest{Target: chat, Message: msg}
}

// ────────────────────────────────────────────────────────────────────────────
// platform.Sender.Send
// ────────────────────────────────────────────────────────────────────────────

func TestSender_Send_GroupMessage(t *testing.T) {
	m := newMockMilkyServer(t)
	s := newTestSender(t, m)

	req := sendReq(platform.ChatInfo{ID: "555", IsGroup: true}, platform.TextMessage("hello"))
	result, err := s.Send(context.Background(), req)
	require.NoError(t, err)

	assert.Equal(t, "456", result.MessageID)
	assert.Equal(t, time.Unix(1700000001, 0), result.Timestamp)
	assert.Equal(t, PlatformID, result.Platform)
	assert.NotNil(t, result.Raw)

	reqs := m.reqs
	require.Len(t, reqs, 1)
	assert.Equal(t, "/api/send_group_message", reqs[0].path)
	body := gjson.Parse(reqs[0].body)
	assert.Equal(t, int64(555), body.Get("group_id").Int())
	assert.Equal(t, "hello", body.Get("message.0.data.text").String())
}

func TestSender_Send_PrivateMessage(t *testing.T) {
	m := newMockMilkyServer(t)
	s := newTestSender(t, m)

	req := sendReq(platform.ChatInfo{ID: "1001", IsGroup: false}, platform.TextMessage("hi"))
	result, err := s.Send(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "123", result.MessageID)

	body := gjson.Parse(m.lastReq().body)
	assert.Equal(t, int64(1001), body.Get("user_id").Int())
}

func TestSender_Send_SceneOverrideFromExtra(t *testing.T) {
	m := newMockMilkyServer(t)
	s := newTestSender(t, m)

	msg := platform.TextMessage("hi")
	msg = ApplyExtra(msg, MessageExtra{Scene: "group"})
	// chatID 未编码 scene（纯数字），IsGroup 也不可信，但 Extra 指定了 group。
	req := sendReq(platform.ChatInfo{ID: "555"}, msg)

	_, err := s.Send(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "/api/send_group_message", m.lastReq().path)
}

func TestSender_Send_SceneFallbackFromIsGroup(t *testing.T) {
	m := newMockMilkyServer(t)
	s := newTestSender(t, m)

	// 纯数字 chatID + IsGroup=true → 推断为群消息。
	req := sendReq(platform.ChatInfo{ID: "555", IsGroup: true}, platform.TextMessage("hi"))
	_, err := s.Send(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "/api/send_group_message", m.lastReq().path)

	// 纯数字 chatID + IsGroup=false → 推断为私聊。
	req = sendReq(platform.ChatInfo{ID: "1001", IsGroup: false}, platform.TextMessage("hi"))
	_, err = s.Send(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "/api/send_private_message", m.lastReq().path)
}

func TestSender_Send_InvalidChatID(t *testing.T) {
	m := newMockMilkyServer(t)
	s := newTestSender(t, m)

	req := sendReq(platform.ChatInfo{ID: "not-a-chat"}, platform.TextMessage("hi"))
	_, err := s.Send(context.Background(), req)
	assert.Error(t, err)
	assert.ErrorIs(t, err, errutil.ErrNoChatInfo)
}

func TestSender_Send_EmptyMessage(t *testing.T) {
	m := newMockMilkyServer(t)
	s := newTestSender(t, m)

	req := sendReq(platform.ChatInfo{ID: "555", IsGroup: true}, platform.TextMessage(""))
	_, err := s.Send(context.Background(), req)
	assert.ErrorIs(t, err, errutil.ErrEmptyMessage)
	assert.Empty(t, m.reqs)
}

func TestSender_Send_FileOnlyMessage(t *testing.T) {
	m := newMockMilkyServer(t)
	s := newTestSender(t, m)

	msg := platform.TextMessage("").WithAttachments(
		platform.Attachment{URL: "https://ex.com/a.pdf", Kind: platform.AttachmentKindFile, Name: "a.pdf"},
	)
	req := sendReq(platform.ChatInfo{ID: "555", IsGroup: true}, msg)

	result, err := s.Send(context.Background(), req)
	require.NoError(t, err)
	assert.Empty(t, result.MessageID)

	require.Len(t, m.reqs, 1)
	assert.Equal(t, "/api/upload_group_file", m.lastReq().path)
	body := gjson.Parse(m.lastReq().body)
	assert.Equal(t, int64(555), body.Get("group_id").Int())
	assert.Equal(t, "https://ex.com/a.pdf", body.Get("file_uri").String())
	assert.Equal(t, "a.pdf", body.Get("file_name").String())
}

func TestSender_Send_TextPlusFile(t *testing.T) {
	m := newMockMilkyServer(t)
	s := newTestSender(t, m)

	msg := platform.TextMessage("正文").WithAttachments(
		platform.Attachment{URL: "https://ex.com/b.pdf", Kind: platform.AttachmentKindFile, Name: "b.pdf"},
	)
	req := sendReq(platform.ChatInfo{ID: "1001"}, msg)

	_, err := s.Send(context.Background(), req)
	require.NoError(t, err)

	require.Len(t, m.reqs, 2)
	assert.Equal(t, "/api/send_private_message", m.reqs[0].path)
	assert.Equal(t, "/api/upload_private_file", m.reqs[1].path)
}

func TestSender_Send_FileBase64Data(t *testing.T) {
	m := newMockMilkyServer(t)
	s := newTestSender(t, m)

	msg := platform.TextMessage("").WithAttachments(
		platform.Attachment{Data: []byte("pdf-bytes"), Kind: platform.AttachmentKindFile, Name: "c.pdf"},
	)
	req := sendReq(platform.ChatInfo{ID: "1001"}, msg)

	_, err := s.Send(context.Background(), req)
	require.NoError(t, err)
	body := gjson.Parse(m.lastReq().body)
	assert.Contains(t, body.Get("file_uri").String(), "base64://")
}

func TestSender_Send_FileNoURINoData(t *testing.T) {
	m := newMockMilkyServer(t)
	s := newTestSender(t, m)

	msg := platform.TextMessage("").WithAttachments(
		platform.Attachment{Kind: platform.AttachmentKindFile},
	)
	req := sendReq(platform.ChatInfo{ID: "1001"}, msg)

	_, err := s.Send(context.Background(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "attachment[0] must have either URL or Data")
}

func TestSender_Send_ApiErrorWrapped(t *testing.T) {
	m := newMockMilkyServer(t)
	m.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"failed","retcode":-403,"message":"denied","data":null}`))
	})
	s := newTestSender(t, m)

	req := sendReq(platform.ChatInfo{ID: "555", IsGroup: true}, platform.TextMessage("hi"))
	_, err := s.Send(context.Background(), req)
	require.Error(t, err)

	se, ok := err.(*platform.SendError)
	require.True(t, ok)
	assert.Equal(t, platform.SendErrPermDenied, se.Code)
	assert.Equal(t, "555", se.ChatID)
}

func TestSender_Send_MarkdownFallsBackToText(t *testing.T) {
	m := newMockMilkyServer(t)
	s := newTestSender(t, m)

	req := sendReq(platform.ChatInfo{ID: "555", IsGroup: true}, platform.MarkdownMessage("**bold**"))
	_, err := s.Send(context.Background(), req)
	require.NoError(t, err)

	body := gjson.Parse(m.lastReq().body)
	assert.Equal(t, "**bold**", body.Get("message.0.data.text").String())
}

// ────────────────────────────────────────────────────────────────────────────
// platform.MessageDeleter.Delete
// ────────────────────────────────────────────────────────────────────────────

func TestSender_Delete_Group(t *testing.T) {
	m := newMockMilkyServer(t)
	s := newTestSender(t, m)

	require.NoError(t, s.Delete(context.Background(), "555", "456"))
	assertReq(t, m, "/api/recall_group_message", `{"group_id":555,"message_seq":456}`)
}

func TestSender_Delete_Private(t *testing.T) {
	m := newMockMilkyServer(t)
	// 双尝试：群撤回先失败 → 落到私聊撤回
	m.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/recall_group_message" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"failed","retcode":-1,"data":{}}`))
			return
		}
		m.handle(w, r)
	})
	s := newTestSender(t, m)

	require.NoError(t, s.Delete(context.Background(), "1001", "123"))
	assertReq(t, m, "/api/recall_private_message", `{"user_id":1001,"message_seq":123}`)
}

func TestSender_Delete_PrivateOnlyOneAttempt(t *testing.T) {
	m := newMockMilkyServer(t)
	s := newTestSender(t, m)

	// 群撤回成功 → 不再尝试私聊
	require.NoError(t, s.Delete(context.Background(), "1001", "123"))
	assertReq(t, m, "/api/recall_group_message", `{"group_id":1001,"message_seq":123}`)
	require.Len(t, m.reqs, 1)
}

func TestSender_Delete_InvalidChatID(t *testing.T) {
	m := newMockMilkyServer(t)
	s := newTestSender(t, m)

	err := s.Delete(context.Background(), "bad", "123")
	assert.Error(t, err)
}

func TestSender_Delete_InvalidMessageID(t *testing.T) {
	m := newMockMilkyServer(t)
	s := newTestSender(t, m)

	err := s.Delete(context.Background(), "555", "abc")
	assert.Error(t, err)
}

// ────────────────────────────────────────────────────────────────────────────
// platform.GroupManager
// ────────────────────────────────────────────────────────────────────────────

func TestSender_KickMember(t *testing.T) {
	m := newMockMilkyServer(t)
	s := newTestSender(t, m)

	require.NoError(t, s.KickMember(context.Background(), "555", "2", true))
	assertReq(t, m, "/api/kick_group_member", `{"group_id":555,"user_id":2,"reject_add_request":true}`)
}

func TestSender_KickMember_ScenePrefixedGroupID(t *testing.T) {
	m := newMockMilkyServer(t)
	s := newTestSender(t, m)

	require.NoError(t, s.KickMember(context.Background(), "group:555", "2", false))
	body := gjson.Parse(m.lastReq().body)
	assert.Equal(t, int64(555), body.Get("group_id").Int())
}

func TestSender_KickMember_InvalidIDs(t *testing.T) {
	m := newMockMilkyServer(t)
	s := newTestSender(t, m)

	assert.Error(t, s.KickMember(context.Background(), "abc", "2", false))
	assert.Error(t, s.KickMember(context.Background(), "555", "abc", false))
}

func TestSender_BanMember(t *testing.T) {
	m := newMockMilkyServer(t)
	s := newTestSender(t, m)

	require.NoError(t, s.BanMember(context.Background(), "555", "2", 10*time.Minute))
	assertReq(t, m, "/api/set_group_member_mute", `{"group_id":555,"user_id":2,"duration":600}`)
}

func TestSender_BanMember_NegativeDurationUnbans(t *testing.T) {
	m := newMockMilkyServer(t)
	s := newTestSender(t, m)

	require.NoError(t, s.BanMember(context.Background(), "555", "2", -1*time.Second))
	assertReq(t, m, "/api/set_group_member_mute", `{"group_id":555,"user_id":2,"duration":0}`)
}

func TestSender_SetAdmin(t *testing.T) {
	m := newMockMilkyServer(t)
	s := newTestSender(t, m)

	require.NoError(t, s.SetAdmin(context.Background(), "555", "2", true))
	assertReq(t, m, "/api/set_group_member_admin", `{"group_id":555,"user_id":2,"is_set":true}`)
}

// ────────────────────────────────────────────────────────────────────────────
// platform.AutoModerator
// ────────────────────────────────────────────────────────────────────────────

func TestSender_DeleteMemberMessage(t *testing.T) {
	m := newMockMilkyServer(t)
	s := newTestSender(t, m)

	require.NoError(t, s.DeleteMemberMessage(context.Background(), "555", "456"))
	assertReq(t, m, "/api/recall_group_message", `{"group_id":555,"message_seq":456}`)
}

func TestSender_DeleteMemberMessage_InvalidGroupID(t *testing.T) {
	m := newMockMilkyServer(t)
	s := newTestSender(t, m)

	err := s.DeleteMemberMessage(context.Background(), "not-a-group", "456")
	assert.Error(t, err)
	assert.Empty(t, m.reqs)
}

func TestSender_MuteAll(t *testing.T) {
	m := newMockMilkyServer(t)
	s := newTestSender(t, m)

	require.NoError(t, s.MuteAll(context.Background(), "555", true))
	assertReq(t, m, "/api/set_group_whole_mute", `{"group_id":555,"is_mute":true}`)
}

// ────────────────────────────────────────────────────────────────────────────
// platform.InvitationHandler
// ────────────────────────────────────────────────────────────────────────────

func TestSender_AcceptGroupInvite(t *testing.T) {
	m := newMockMilkyServer(t)
	s := newTestSender(t, m)

	require.NoError(t, s.AcceptGroupInvite(context.Background(), "555:42"))
	assertReq(t, m, "/api/accept_group_invitation", `{"group_id":555,"invitation_seq":42}`)
}

func TestSender_RejectGroupInvite(t *testing.T) {
	m := newMockMilkyServer(t)
	s := newTestSender(t, m)

	require.NoError(t, s.RejectGroupInvite(context.Background(), "555:42", "没空"))
	assertReq(t, m, "/api/reject_group_invitation", `{"group_id":555,"invitation_seq":42}`)
}

func TestSender_AcceptGroupInvite_Invalid(t *testing.T) {
	m := newMockMilkyServer(t)
	s := newTestSender(t, m)

	assert.Error(t, s.AcceptGroupInvite(context.Background(), "invalid"))
	assert.Empty(t, m.reqs)
}

func TestSender_AcceptFriendRequest(t *testing.T) {
	m := newMockMilkyServer(t)
	s := newTestSender(t, m)

	require.NoError(t, s.AcceptFriendRequest(context.Background(), "uid200"))
	assertReq(t, m, "/api/accept_friend_request", `{"initiator_uid":"uid200","is_filtered":false}`)
}

func TestSender_RejectFriendRequest(t *testing.T) {
	m := newMockMilkyServer(t)
	s := newTestSender(t, m)

	require.NoError(t, s.RejectFriendRequest(context.Background(), "uid200", "no"))
	assertReq(t, m, "/api/reject_friend_request", `{"initiator_uid":"uid200","is_filtered":false,"reason":"no"}`)
}

// ────────────────────────────────────────────────────────────────────────────
// platform.ReactionSender
// ────────────────────────────────────────────────────────────────────────────

func TestSender_AddReaction_Face(t *testing.T) {
	m := newMockMilkyServer(t)
	s := newTestSender(t, m)

	require.NoError(t, s.AddReaction(context.Background(), "555", "456", platform.Emoji{ID: "21", Kind: platform.EmojiKindSystem}))
	assertReq(t, m, "/api/send_group_message_reaction", `{"group_id":555,"message_seq":456,"reaction":"21","reaction_type":"face","is_add":true}`)
}

func TestSender_AddReaction_Unicode(t *testing.T) {
	m := newMockMilkyServer(t)
	s := newTestSender(t, m)

	require.NoError(t, s.AddReaction(context.Background(), "555", "456", platform.Emoji{Value: "👍", Kind: platform.EmojiKindUnicode}))
	assertReq(t, m, "/api/send_group_message_reaction", `{"group_id":555,"message_seq":456,"reaction":"👍","reaction_type":"emoji","is_add":true}`)
}

func TestSender_RemoveReaction(t *testing.T) {
	m := newMockMilkyServer(t)
	s := newTestSender(t, m)

	require.NoError(t, s.RemoveReaction(context.Background(), "555", "456", platform.Emoji{ID: "21"}))
	body := gjson.Parse(m.lastReq().body)
	assert.False(t, body.Get("is_add").Bool())
}

func TestSender_Reaction_InvalidChatID(t *testing.T) {
	m := newMockMilkyServer(t)
	s := newTestSender(t, m)

	err := s.AddReaction(context.Background(), "bad", "456", platform.Emoji{ID: "21"})
	assert.Error(t, err)
}

func TestSender_Reaction_PlainIDAccepted(t *testing.T) {
	// 纯数字 ID 无法判断场景：不再做 scene 校验，文档注明仅群消息支持
	m := newMockMilkyServer(t)
	s := newTestSender(t, m)

	require.NoError(t, s.AddReaction(context.Background(), "555", "456", platform.Emoji{ID: "21"}))
	assertReq(t, m, "/api/send_group_message_reaction", `{"group_id":555,"message_seq":456,"reaction":"21","reaction_type":"face","is_add":true}`)
}

func TestSender_Reaction_InvalidMessageID(t *testing.T) {
	m := newMockMilkyServer(t)
	s := newTestSender(t, m)

	err := s.AddReaction(context.Background(), "555", "abc", platform.Emoji{ID: "21"})
	assert.Error(t, err)
}
