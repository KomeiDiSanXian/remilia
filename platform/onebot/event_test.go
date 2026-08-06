package onebot

// 本文档核验说明
//
// 本文件中的事件类型、事件字段断言均对照 OneBot 11 标准
// （onebot.dev/11，内容源 botuniverse/onebot-11）与主流协议端实现源码
// （2026-08 核验）：
//
//	- OneBot 11 标准：onebot.dev/11/specs/event/、/11/specs/message/、/11/specs/cqcode/
//	- NapCat：github.com/NapNeko/NapCatQQ（扩展动作 get_group_at_all_remain 等）
//	- LLOneBot / LuckyLilliaBot：github.com/LLOneBot/LuckyLilliaBot
//	  （src/onebot11/event/notice/ 与 src/onebot11/types.ts，扩展通知
//	  group_card/group_dismiss/essence/group_msg_emoji_like/flash_file、
//	  notify 子类型 poke_recall/title/profile_like、keyboard 消息段）
//	- Lagrange.OneBot（Lagrange.Core v1 分支）：event/notice 共 15 种，无扩展
//
// 已知差异（2026-08 核验）：
//   - post_type=message_sent 非 OneBot 11 标准，为 NapCat/go-cqhttp 扩展；
//   - notice_type=friend_remove 在标准与 NapCat/go-cqhttp/LLB/Lagrange 中
//     均未定义，仅保留宽容解析（见 types.go 注释）；
//   - notify/lucky_king、notify/honor 为 go-cqhttp 遗留子类型，
//     LLB/Lagrange 不产生，保留解析无碍。

import (
	"encoding/json"
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseEvent_PrivateMessage(t *testing.T) {
	raw := `{
		"time": 1700000000,
		"self_id": 123456,
		"post_type": "message",
		"message_type": "private",
		"sub_type": "friend",
		"message_id": 1001,
		"user_id": 98765,
		"message": "hello",
		"raw_message": "hello",
		"font": 14,
		"sender": {"user_id": 98765, "nickname": "Alice", "sex": "female", "age": 25}
	}`
	ev, err := parseEvent([]byte(raw))
	require.NoError(t, err)
	assert.Equal(t, platform.EventKindPrivateMessage, ev.Kind())
	assert.Equal(t, "onebot", ev.Platform())
	assert.Equal(t, "1001", ev.ID())
	assert.Equal(t, "hello", platform.Content(ev))
	assert.Equal(t, "98765", ev.Sender().ID)
	assert.Equal(t, "Alice", ev.Sender().DisplayName)
	assert.True(t, ev.Chat().IsDM)
	assert.False(t, ev.Chat().IsGroup)
}

func TestParseEvent_GroupMessage(t *testing.T) {
	raw := `{
		"time": 1700000001,
		"self_id": 123456,
		"post_type": "message",
		"message_type": "group",
		"sub_type": "normal",
		"message_id": 2001,
		"group_id": 555,
		"user_id": 98765,
		"message": [{"type":"text","data":{"text":"group hi"}}],
		"raw_message": "group hi",
		"font": 14,
		"sender": {"user_id": 98765, "nickname": "Bob", "card": "Bobby", "role": "admin"}
	}`
	ev, err := parseEvent([]byte(raw))
	require.NoError(t, err)
	assert.Equal(t, platform.EventKindGroupMessage, ev.Kind())
	assert.Equal(t, "2001", ev.ID())
	assert.Equal(t, "group hi", platform.Content(ev))
	assert.Equal(t, "Bobby", ev.Sender().DisplayName)
	assert.Equal(t, platform.GroupRoleAdmin, ev.Sender().GroupRole)
	assert.True(t, ev.Chat().IsGroup)
	assert.Equal(t, "555", ev.Chat().ID)
}

func TestParseEvent_PrivateMessage_WithCQString(t *testing.T) {
	raw := `{
		"time": 1700000002,
		"self_id": 123456,
		"post_type": "message",
		"message_type": "private",
		"sub_type": "friend",
		"message_id": 3001,
		"user_id": 98765,
		"message": "hello [CQ:face,id=21]",
		"raw_message": "hello [CQ:face,id=21]",
		"sender": {"user_id": 98765, "nickname": "Charlie"}
	}`
	ev, err := parseEvent([]byte(raw))
	require.NoError(t, err)
	assert.Equal(t, "hello", platform.Content(ev))
}

func TestParseEvent_GroupMessage_WithMentions(t *testing.T) {
	raw := `{
		"time": 1700000003,
		"self_id": 123456,
		"post_type": "message",
		"message_type": "group",
		"sub_type": "normal",
		"message_id": 4001,
		"group_id": 555,
		"user_id": 98765,
		"message": [{"type":"text","data":{"text":"hi "}},{"type":"at","data":{"qq":"123"}}],
		"sender": {"user_id": 98765, "nickname": "Dave"}
	}`
	ev, err := parseEvent([]byte(raw))
	require.NoError(t, err)
	mentions := platform.GetMentions(ev)
	require.Len(t, mentions, 1)
	assert.Equal(t, "123", mentions[0].ID)
}

func TestParseEvent_GroupMessage_MentionSelf(t *testing.T) {
	// @ 机器人自身（at qq == self_id）→ IsSelf=true，OnMentionedBot 可命中
	raw := `{
		"time": 1700000003,
		"self_id": 123456,
		"post_type": "message",
		"message_type": "group",
		"sub_type": "normal",
		"message_id": 4002,
		"group_id": 555,
		"user_id": 98765,
		"message": [{"type":"text","data":{"text":"hi "}},{"type":"at","data":{"qq":"123456"}}],
		"sender": {"user_id": 98765, "nickname": "Dave"}
	}`
	ev, err := parseEvent([]byte(raw))
	require.NoError(t, err)
	mentions := platform.GetMentions(ev)
	require.Len(t, mentions, 1)
	assert.Equal(t, "123456", mentions[0].ID)
	assert.True(t, mentions[0].IsSelf, "@ 机器人自身（self_id 匹配）应标记 IsSelf")

	// 普通消息（无 @）→ GetMentions 为空，OnMentionedBotOrNoMentions 放行
	raw2 := `{
		"time": 1, "self_id": 123456, "post_type": "message", "message_type": "group",
		"sub_type": "normal", "message_id": 1, "group_id": 1, "user_id": 1,
		"message": "hi",
		"sender": {"user_id": 1, "nickname": "N"}
	}`
	ev2, err := parseEvent([]byte(raw2))
	require.NoError(t, err)
	assert.Empty(t, platform.GetMentions(ev2))
}

func TestParseEvent_GroupMessage_OwnerRole(t *testing.T) {
	raw := `{
		"time": 1, "self_id": 1, "post_type": "message", "message_type": "group",
		"sub_type": "normal", "message_id": 1, "group_id": 1, "user_id": 1,
		"message": "hi",
		"sender": {"user_id": 1, "nickname": "Owner", "role": "owner"}
	}`
	ev, err := parseEvent([]byte(raw))
	require.NoError(t, err)
	assert.Equal(t, platform.GroupRoleOwner, ev.Sender().GroupRole)
}

func TestParseEvent_Notice_GroupUpload(t *testing.T) {
	raw := `{
		"time": 1700000010, "self_id": 123456, "post_type": "notice",
		"notice_type": "group_upload", "group_id": 555, "user_id": 98765,
		"file": {"id": "f1", "name": "file.pdf", "size": 1024, "busid": 0}
	}`
	ev, err := parseEvent([]byte(raw))
	require.NoError(t, err)
	assert.Equal(t, platform.EventKindNotice, ev.Kind())
	assert.True(t, ev.Chat().IsGroup)
}

func TestParseEvent_Notice_GroupDecrease_KickMe(t *testing.T) {
	raw := `{
		"time": 1, "self_id": 123, "post_type": "notice",
		"notice_type": "group_decrease", "sub_type": "kick_me",
		"group_id": 555, "user_id": 98765, "operator_id": 111
	}`
	ev, err := parseEvent([]byte(raw))
	require.NoError(t, err)
	assert.Equal(t, platform.EventKindBotRemoved, ev.Kind())
}

func TestParseEvent_Notice_GroupIncrease(t *testing.T) {
	raw := `{
		"time": 1, "self_id": 123, "post_type": "notice",
		"notice_type": "group_increase", "sub_type": "approve",
		"group_id": 555, "user_id": 98765
	}`
	ev, err := parseEvent([]byte(raw))
	require.NoError(t, err)
	assert.Equal(t, platform.EventKindMemberJoin, ev.Kind())
}

func TestParseEvent_Notice_FriendAdd(t *testing.T) {
	raw := `{
		"time": 1, "self_id": 123, "post_type": "notice",
		"notice_type": "friend_add", "user_id": 98765
	}`
	ev, err := parseEvent([]byte(raw))
	require.NoError(t, err)
	assert.Equal(t, platform.EventKindFriendAdded, ev.Kind())
	assert.True(t, ev.Chat().IsDM)
}

func TestParseEvent_Notice_FriendRemove(t *testing.T) {
	raw := `{
		"time": 1, "self_id": 123, "post_type": "notice",
		"notice_type": "friend_remove", "user_id": 98765
	}`
	ev, err := parseEvent([]byte(raw))
	require.NoError(t, err)
	assert.Equal(t, platform.EventKindFriendRemoved, ev.Kind())
}

func TestParseEvent_Notice_GroupRecall(t *testing.T) {
	raw := `{
		"time": 1, "self_id": 123, "post_type": "notice",
		"notice_type": "group_recall", "group_id": 555, "user_id": 98765, "message_id": 999
	}`
	ev, err := parseEvent([]byte(raw))
	require.NoError(t, err)
	assert.Equal(t, platform.EventKindMessageDelete, ev.Kind())
}

func TestParseEvent_Notice_FriendRecall(t *testing.T) {
	raw := `{
		"time": 1, "self_id": 123, "post_type": "notice",
		"notice_type": "friend_recall", "user_id": 98765, "message_id": 999
	}`
	ev, err := parseEvent([]byte(raw))
	require.NoError(t, err)
	assert.Equal(t, platform.EventKindMessageDelete, ev.Kind())
	assert.True(t, ev.Chat().IsDM)
}

func TestParseEvent_Notice_GroupAdmin(t *testing.T) {
	raw := `{
		"time": 1, "self_id": 123, "post_type": "notice",
		"notice_type": "group_admin", "sub_type": "set",
		"group_id": 555, "user_id": 98765
	}`
	ev, err := parseEvent([]byte(raw))
	require.NoError(t, err)
	assert.Equal(t, platform.EventKindNotice, ev.Kind())
}

func TestParseEvent_Notice_GroupBan(t *testing.T) {
	raw := `{
		"time": 1, "self_id": 123, "post_type": "notice",
		"notice_type": "group_ban", "sub_type": "ban",
		"group_id": 555, "user_id": 98765, "duration": 600
	}`
	ev, err := parseEvent([]byte(raw))
	require.NoError(t, err)
	assert.Equal(t, platform.EventKindNotice, ev.Kind())
}

func TestParseEvent_Notice_Notify_Poke(t *testing.T) {
	raw := `{
		"time": 1, "self_id": 123, "post_type": "notice",
		"notice_type": "notify", "sub_type": "poke",
		"group_id": 555, "user_id": 98765, "target_id": 123
	}`
	ev, err := parseEvent([]byte(raw))
	require.NoError(t, err)
	assert.Equal(t, platform.EventKindNotice, ev.Kind())
	assert.Contains(t, platform.Content(ev), "poke")
}

// ── LLOneBot / LuckyLilliaBot 扩展通知（2026-08 对照其源码核验）────────────

func TestParseEvent_Notice_GroupCard(t *testing.T) {
	raw := `{
		"time": 1, "self_id": 123, "post_type": "notice",
		"notice_type": "group_card", "group_id": 555, "user_id": 98765,
		"card_new": "新名片", "card_old": "旧名片"
	}`
	ev, err := parseEvent([]byte(raw))
	require.NoError(t, err)
	assert.Equal(t, platform.EventKindMemberUpdate, ev.Kind())
	assert.Equal(t, "card:旧名片→新名片", platform.Content(ev))
	re, ok := ev.(platform.RawEvent)
	require.True(t, ok)
	assert.Equal(t, "notice/group_card", re.RawType())
}

func TestParseEvent_Notice_GroupDismiss(t *testing.T) {
	raw := `{
		"time": 1, "self_id": 123, "post_type": "notice",
		"notice_type": "group_dismiss", "group_id": 555, "user_id": 1
	}`
	ev, err := parseEvent([]byte(raw))
	require.NoError(t, err)
	assert.Equal(t, "notice/group_dismiss", ev.(platform.RawEvent).RawType())
}

func TestParseEvent_Notice_Essence(t *testing.T) {
	raw := `{
		"time": 1, "self_id": 123, "post_type": "notice",
		"notice_type": "essence", "sub_type": "add",
		"group_id": 555, "message_id": 999, "sender_id": 98765, "operator_id": 1
	}`
	ev, err := parseEvent([]byte(raw))
	require.NoError(t, err)
	assert.Equal(t, "notice/essence/add", ev.(platform.RawEvent).RawType())
}

func TestParseEvent_Notice_GroupMsgEmojiLike(t *testing.T) {
	raw := `{
		"time": 1, "self_id": 123, "post_type": "notice",
		"notice_type": "group_msg_emoji_like",
		"group_id": 555, "user_id": 98765, "message_id": 999,
		"likes": [{"emoji_id": "21", "emoji_count": 1}], "is_add": true
	}`
	ev, err := parseEvent([]byte(raw))
	require.NoError(t, err)
	assert.Equal(t, platform.EventKindReaction, ev.Kind())
	assert.Equal(t, "notice/group_msg_emoji_like", ev.(platform.RawEvent).RawType())
}

func TestParseEvent_Notice_Notify_PokeRecall(t *testing.T) {
	raw := `{
		"time": 1, "self_id": 123, "post_type": "notice",
		"notice_type": "notify", "sub_type": "poke_recall",
		"group_id": 555, "user_id": 98765, "target_id": 123
	}`
	ev, err := parseEvent([]byte(raw))
	require.NoError(t, err)
	assert.Equal(t, "notice/notify/poke_recall", ev.(platform.RawEvent).RawType())
	assert.Contains(t, platform.Content(ev), "poke_recall")
}

func TestParseEvent_Notice_Notify_Title(t *testing.T) {
	raw := `{
		"time": 1, "self_id": 123, "post_type": "notice",
		"notice_type": "notify", "sub_type": "title",
		"group_id": 555, "user_id": 98765, "title": "新头衔"
	}`
	ev, err := parseEvent([]byte(raw))
	require.NoError(t, err)
	assert.Equal(t, "新头衔", platform.Content(ev))
}

func TestParseEvent_Notice_Notify_ProfileLike(t *testing.T) {
	raw := `{
		"time": 1, "self_id": 123, "post_type": "notice",
		"notice_type": "notify", "sub_type": "profile_like",
		"user_id": 98765, "operator_id": 1, "operator_nick": "Admin", "times": 3
	}`
	ev, err := parseEvent([]byte(raw))
	require.NoError(t, err)
	assert.Equal(t, "notice/notify/profile_like", ev.(platform.RawEvent).RawType())
	assert.Contains(t, platform.Content(ev), "×3")
}

func TestParseEvent_Notice_FlashFile(t *testing.T) {
	raw := `{
		"time": 1, "self_id": 123, "post_type": "notice",
		"notice_type": "flash_file", "sub_type": "downloaded",
		"title": "闪照", "share_link": "https://ex.com", "file_set_id": "f1",
		"files": [{"file_id": "1"}]
	}`
	ev, err := parseEvent([]byte(raw))
	require.NoError(t, err)
	assert.Equal(t, "notice/flash_file/downloaded", ev.(platform.RawEvent).RawType())
}

func TestParseEvent_Request_Friend(t *testing.T) {
	raw := `{
		"time": 1, "self_id": 123, "post_type": "request",
		"request_type": "friend", "user_id": 98765,
		"comment": "hello", "flag": "flag123"
	}`
	ev, err := parseEvent([]byte(raw))
	require.NoError(t, err)
	assert.Equal(t, platform.EventKindRequest, ev.Kind())
	assert.Equal(t, "flag123", ev.Chat().Tokens[TokenRequestFlag])
}

func TestParseEvent_Request_Group(t *testing.T) {
	raw := `{
		"time": 1, "self_id": 123, "post_type": "request",
		"request_type": "group", "sub_type": "invite",
		"group_id": 555, "user_id": 98765,
		"comment": "", "flag": "flag456"
	}`
	ev, err := parseEvent([]byte(raw))
	require.NoError(t, err)
	assert.Equal(t, platform.EventKindRequest, ev.Kind())
	assert.True(t, ev.Chat().IsGroup)
}

func TestParseEvent_Meta_Lifecycle(t *testing.T) {
	raw := `{
		"time": 1, "self_id": 123, "post_type": "meta_event",
		"meta_event_type": "lifecycle", "sub_type": "connect"
	}`
	ev, err := parseEvent([]byte(raw))
	require.NoError(t, err)
	assert.Equal(t, platform.EventKindSystem, ev.Kind())
	assert.Equal(t, "connect", platform.Content(ev))
}

func TestParseEvent_Meta_Heartbeat(t *testing.T) {
	raw := `{
		"time": 1, "self_id": 123, "post_type": "meta_event",
		"meta_event_type": "heartbeat",
		"status": {"online": true, "good": true},
		"interval": 5000
	}`
	ev, err := parseEvent([]byte(raw))
	require.NoError(t, err)
	assert.Equal(t, platform.EventKindSystem, ev.Kind())
	assert.Equal(t, "heartbeat", platform.Content(ev))
}

func TestParseEvent_UnknownPostType(t *testing.T) {
	raw := `{
		"time": 1, "self_id": 123, "post_type": "unknown_type"
	}`
	ev, err := parseEvent([]byte(raw))
	require.NoError(t, err)
	assert.Equal(t, platform.EventKindUnknown, ev.Kind())
}

func TestParseEvent_InvalidJSON(t *testing.T) {
	_, err := parseEvent([]byte("{invalid"))
	assert.Error(t, err)
}

func TestParseEvent_MessageSent(t *testing.T) {
	raw := `{
		"time": 1, "self_id": 123, "post_type": "message_sent",
		"message_type": "private", "sub_type": "friend",
		"message_id": 1, "user_id": 98765, "message": "sent",
		"sender": {"user_id": 123, "nickname": "Bot"}
	}`
	ev, err := parseEvent([]byte(raw))
	require.NoError(t, err)
	assert.Equal(t, platform.EventKindPrivateMessage, ev.Kind())
}

func TestParseEvent_GroupMessage_Tokens(t *testing.T) {
	raw := `{
		"time": 1, "self_id": 1, "post_type": "message", "message_type": "group",
		"sub_type": "normal", "message_id": 5001, "group_id": 555, "user_id": 1,
		"message": "hi", "sender": {"user_id": 1, "nickname": "U"}
	}`
	ev, err := parseEvent([]byte(raw))
	require.NoError(t, err)
	assert.Equal(t, "555", ev.Chat().Tokens[TokenGroupID])
	assert.Equal(t, "5001", ev.Chat().Tokens[TokenMessageID])
}

func TestForwardWSAdapter_InterfaceCompliance(t *testing.T) {
	adapter := NewAdapter("ws://127.0.0.1:6700")
	var _ platform.Adapter = adapter
	var _ platform.RecoverableAdapter = adapter
	var _ platform.BotIdentity = adapter
	var _ platform.HealthDetailer = adapter
}

func TestOnebotEvent_Interfaces(t *testing.T) {
	raw := `{"time":1,"self_id":1,"post_type":"message","message_type":"private",
		"sub_type":"friend","message_id":1,"user_id":1,"message":"test",
		"sender":{"user_id":1,"nickname":"T"}}`
	ev, err := parseEvent([]byte(raw))
	require.NoError(t, err)
	_ = ev
}

func TestIsAPIResponse(t *testing.T) {
	assert.True(t, isAPIResponse([]byte(`{"status":"ok","retcode":0,"data":{}}`)))
	assert.True(t, isAPIResponse([]byte(`{"status":"ok","retcode":0,"data":{},"echo":"e1"}`)))
	assert.False(t, isAPIResponse([]byte(`{"post_type":"message","message_type":"private"}`)))
	assert.False(t, isAPIResponse([]byte(`not json`)))
}

func TestParseEvent_GroupMessage_WithReply(t *testing.T) {
	raw := `{
		"time": 1, "self_id": 1, "post_type": "message", "message_type": "group",
		"sub_type": "normal", "message_id": 6001, "group_id": 555, "user_id": 1,
		"message": [{"type":"reply","data":{"id":"5000"}},{"type":"text","data":{"text":"reply msg"}}],
		"sender": {"user_id": 1, "nickname": "U"}
	}`
	ev, err := parseEvent([]byte(raw))
	require.NoError(t, err)
	assert.Equal(t, "5000", platform.GetReplyToID(ev))
}

func TestAPIResponse_IsOK(t *testing.T) {
	r := &APIResponse{Status: "ok", Retcode: 0}
	assert.True(t, r.IsOK())
	assert.False(t, (&APIResponse{Status: "failed", Retcode: 1}).IsOK())
	assert.False(t, (&APIResponse{Status: "ok", Retcode: 1}).IsOK())
}

func TestAPIResponse_IsAsync(t *testing.T) {
	assert.True(t, (&APIResponse{Status: "async", Retcode: 1}).IsAsync())
	assert.False(t, (&APIResponse{Status: "ok", Retcode: 0}).IsAsync())
}

func TestSegmentsToMentions(t *testing.T) {
	// 空段 → nil
	assert.Nil(t, segmentsToMentions(nil, ""))
	// at 段 → 聚合视图（保序去重）
	segs := []platform.Segment{
		{Type: platform.SegmentAt, UserID: "123"},
		{Type: platform.SegmentAt, UserID: "456"},
		{Type: platform.SegmentAt, UserID: "123"}, // 去重
		{Type: platform.SegmentMentionAll},
	}
	mentions := segmentsToMentions(segs, "")
	require.Len(t, mentions, 2)
	assert.Equal(t, "123", mentions[0].ID)
	assert.Equal(t, "456", mentions[1].ID)
	// botID 命中 → IsSelf
	mentions = segmentsToMentions([]platform.Segment{{Type: platform.SegmentAt, UserID: "123"}}, "123")
	require.Len(t, mentions, 1)
	assert.True(t, mentions[0].IsSelf)
}

func TestGroupChat(t *testing.T) {
	ci := groupChat(555)
	assert.Equal(t, "555", ci.ID)
	assert.True(t, ci.IsGroup)
}

func TestParseEvent_GroupMessage_SenderCard(t *testing.T) {
	raw := `{
		"time": 1, "self_id": 1, "post_type": "message", "message_type": "group",
		"sub_type": "normal", "message_id": 1, "group_id": 1, "user_id": 1,
		"message": "hi",
		"sender": {"user_id": 1, "nickname": "Nick", "card": "CardName"}
	}`
	ev, err := parseEvent([]byte(raw))
	require.NoError(t, err)
	assert.Equal(t, "CardName", ev.Sender().DisplayName)
}

func TestParseEvent_PrivateMessage_WithMentions(t *testing.T) {
	raw := `{
		"time": 1, "self_id": 1, "post_type": "message", "message_type": "private",
		"sub_type": "friend", "message_id": 1, "user_id": 1,
		"message": [{"type":"text","data":{"text":"hi"}}],
		"sender": {"user_id": 1, "nickname": "U"}
	}`
	ev, err := parseEvent([]byte(raw))
	require.NoError(t, err)
	assert.Empty(t, platform.GetMentions(ev))
}

func TestParseEvent_InvalidMessageType(t *testing.T) {
	raw := `{
		"time": 1, "self_id": 1, "post_type": "message",
		"message_type": "invalid_type", "message_id": 1, "user_id": 1,
		"message": "hi", "sender": {"user_id": 1, "nickname": "U"}
	}`
	ev, err := parseEvent([]byte(raw))
	require.NoError(t, err)
	assert.Equal(t, platform.EventKindUnknown, ev.Kind())
}

func TestParseEvent_Notice_GroupDecrease_Leave(t *testing.T) {
	raw := `{
		"time": 1, "self_id": 123, "post_type": "notice",
		"notice_type": "group_decrease", "sub_type": "leave",
		"group_id": 555, "user_id": 98765
	}`
	ev, err := parseEvent([]byte(raw))
	require.NoError(t, err)
	assert.Equal(t, platform.EventKindMemberLeave, ev.Kind())
}

func TestParseEvent_Notice_GroupDecrease_Kick(t *testing.T) {
	raw := `{
		"time": 1, "self_id": 123, "post_type": "notice",
		"notice_type": "group_decrease", "sub_type": "kick",
		"group_id": 555, "user_id": 98765, "operator_id": 111
	}`
	ev, err := parseEvent([]byte(raw))
	require.NoError(t, err)
	assert.Equal(t, platform.EventKindMemberLeave, ev.Kind())
}

func TestParseEvent_Notice_Notify_LuckyKing(t *testing.T) {
	raw := `{
		"time": 1, "self_id": 123, "post_type": "notice",
		"notice_type": "notify", "sub_type": "lucky_king",
		"group_id": 555, "user_id": 98765, "target_id": 123
	}`
	ev, err := parseEvent([]byte(raw))
	require.NoError(t, err)
	assert.Contains(t, platform.Content(ev), "lucky_king")
}

func TestParseEvent_Notice_Notify_Honor(t *testing.T) {
	raw := `{
		"time": 1, "self_id": 123, "post_type": "notice",
		"notice_type": "notify", "sub_type": "honor",
		"group_id": 555, "user_id": 98765, "honor_type": "talkative"
	}`
	ev, err := parseEvent([]byte(raw))
	require.NoError(t, err)
	assert.Contains(t, platform.Content(ev), "honor")
}

func TestParseEvent_UnmarshalError_PrivateMessage(t *testing.T) {
	raw := `{
		"time": "not-a-number",
		"self_id": 123,
		"post_type": "message",
		"message_type": "private"
	}`
	_, err := parseEvent([]byte(raw))
	assert.Error(t, err)
}

func TestParseEvent_UnmarshalError_Notice(t *testing.T) {
	raw := `{
		"time": "bad",
		"self_id": 123,
		"post_type": "notice",
		"notice_type": "group_upload"
	}`
	_, err := parseEvent([]byte(raw))
	assert.Error(t, err)
}

func TestParseEvent_UnmarshalError_Request(t *testing.T) {
	raw := `{
		"time": "bad",
		"self_id": 123,
		"post_type": "request",
		"request_type": "friend"
	}`
	_, err := parseEvent([]byte(raw))
	assert.Error(t, err)
}

func TestParseEvent_UnmarshalError_Meta(t *testing.T) {
	raw := `{
		"time": "bad",
		"self_id": 123,
		"post_type": "meta_event",
		"meta_event_type": "heartbeat"
	}`
	_, err := parseEvent([]byte(raw))
	assert.Error(t, err)
}

func TestOnebotEvent_RawPayload(t *testing.T) {
	raw := `{"time":1,"self_id":1,"post_type":"message","message_type":"private",
		"sub_type":"friend","message_id":1,"user_id":1,"message":"test",
		"sender":{"user_id":1,"nickname":"T"}}`
	ev, err := parseEvent([]byte(raw))
	require.NoError(t, err)
	re, ok := ev.(interface{ RawType() string })
	require.True(t, ok)
	assert.Equal(t, "message/private/friend", re.RawType())
}

func TestParseEvent_MessageSent_Group(t *testing.T) {
	raw := `{
		"time": 1, "self_id": 123, "post_type": "message_sent",
		"message_type": "group", "sub_type": "normal",
		"message_id": 1, "group_id": 555, "user_id": 123,
		"message": "sent", "sender": {"user_id": 123, "nickname": "Bot"}
	}`
	ev, err := parseEvent([]byte(raw))
	require.NoError(t, err)
	assert.Equal(t, platform.EventKindGroupMessage, ev.Kind())
}

func TestParseEvent_Notice_UnknownNoticeType(t *testing.T) {
	raw := `{
		"time": 1, "self_id": 123, "post_type": "notice",
		"notice_type": "some_unknown_notice", "group_id": 555, "user_id": 1
	}`
	ev, err := parseEvent([]byte(raw))
	require.NoError(t, err)
	assert.Equal(t, platform.EventKindNotice, ev.Kind())
}

func TestParseUnknownEvent(t *testing.T) {
	ev := parseUnknownEvent([]byte(`{"raw":true}`), "some/type")
	assert.Equal(t, platform.EventKindUnknown, ev.Kind())
	re := ev.(interface{ RawType() string })
	assert.Equal(t, "some/type", re.RawType())
}

func TestJsonMarshalTime(t *testing.T) {
	// verify that the marshal/unmarshal round-trip works for time
	type testEvent struct {
		Time int64 `json:"time"`
	}
	var te testEvent
	err := json.Unmarshal([]byte(`{"time":1700000000}`), &te)
	require.NoError(t, err)
	assert.Equal(t, int64(1700000000), te.Time)
}
