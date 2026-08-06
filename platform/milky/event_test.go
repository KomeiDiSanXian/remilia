package milky

// 本文档核验说明
//
// 本文件中的事件类型、事件载荷字段与消息段字段断言均对照 Milky 官方文档
// （v1.2.2，2026-07 核验）：https://milky.ntqqrev.org
//
//	- 事件（event_type 枚举与各类型载荷）：/struct/Event
//	- 接收消息：/struct/IncomingMessage
//	- 接收消息段（text/mention/mention_all/face/reply/image/record/video/
//	  file/forward/market_face/light_app/xml）：/struct/IncomingSegment
//
// WebSocket 事件信封：{"event_type": ..., "time": ..., "self_id": ..., "data": ...}

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// envelope 构造一条 Milky WebSocket 原始事件 JSON。
func envelope(eventType string, data string) []byte {
	return []byte(`{"event_type":"` + eventType + `","time":1700000000,"self_id":10001,"data":` + data + `}`)
}

// ────────────────────────────────────────────────────────────────────────────
// message_receive
// ────────────────────────────────────────────────────────────────────────────

func TestParseMessageEvent_Group(t *testing.T) {
	raw := envelope("message_receive", `{
		"message_scene":"group","peer_id":555,"message_seq":123,"sender_id":1,"time":1700000000,
		"group":{"group_id":555,"group_name":"测试群"},
		"group_member":{"user_id":1,"nickname":"Alice","card":"阿莉","role":"admin"},
		"segments":[
			{"type":"text","data":{"text":"你好"}},
			{"type":"mention","data":{"user_id":2,"name":"Bob"}},
			{"type":"mention_all","data":{}},
			{"type":"reply","data":{"message_seq":100,"sender_id":1,"sender_name":"Alice","time":1699999999}}
		]}`)

	evt, err := parseRawEvent(raw)
	require.NoError(t, err)

	assert.Equal(t, platform.EventKindGroupMessage, evt.Kind())
	assert.Equal(t, "123", evt.ID())
	assert.Equal(t, "group:555", evt.Chat().ID)
	assert.True(t, evt.Chat().IsGroup)
	assert.Equal(t, "测试群", evt.Chat().Name)
	assert.Equal(t, "你好", evt.Content())
	assert.Equal(t, "1", evt.Sender().ID)
	assert.Equal(t, "阿莉", evt.Sender().DisplayName)
	assert.Equal(t, platform.GroupRoleAdmin, evt.Sender().GroupRole)
	assert.Len(t, evt.Attachments(), 0)

	replyEvt, ok := evt.(platform.ReplyEvent)
	require.True(t, ok)
	assert.Equal(t, "100", replyEvt.ReplyToID())

	mentionsEvt, ok := evt.(platform.MentionsEvent)
	require.True(t, ok)
	require.Len(t, mentionsEvt.Mentions(), 1)
	assert.Equal(t, "2", mentionsEvt.Mentions()[0].ID)
	assert.Equal(t, "Bob", mentionsEvt.Mentions()[0].DisplayName)
}

func TestParseMessageEvent_Friend(t *testing.T) {
	raw := envelope("message_receive", `{
		"message_scene":"friend","peer_id":1001,"message_seq":7,"sender_id":1001,"time":1700000000,
		"friend":{"user_id":1001,"nickname":"Alice","remark":"特别好友"},
		"segments":[{"type":"text","data":{"text":"hi"}}]}`)

	evt, err := parseRawEvent(raw)
	require.NoError(t, err)

	assert.Equal(t, platform.EventKindPrivateMessage, evt.Kind())
	assert.Equal(t, "7", evt.ID())
	assert.Equal(t, "friend:1001", evt.Chat().ID)
	assert.False(t, evt.Chat().IsGroup)
	assert.Equal(t, "hi", evt.Content())
	assert.Equal(t, "特别好友", evt.Sender().DisplayName)
}

func TestParseMessageEvent_TempScene(t *testing.T) {
	raw := envelope("message_receive", `{
		"message_scene":"temp","peer_id":1001,"message_seq":8,"sender_id":1001,"time":1700000000,
		"segments":[{"type":"text","data":{"text":"临时会话"}}]}`)

	evt, err := parseRawEvent(raw)
	require.NoError(t, err)

	assert.Equal(t, platform.EventKindPrivateMessage, evt.Kind())
	assert.Equal(t, "temp:1001", evt.Chat().ID)
}

func TestParseMessageEvent_AllAttachmentTypes(t *testing.T) {
	raw := envelope("message_receive", `{
		"message_scene":"group","peer_id":555,"message_seq":9,"sender_id":1,"time":1700000000,
		"segments":[
			{"type":"face","data":{"face_id":"21","is_large":true}},
			{"type":"image","data":{"resource_id":"r1","temp_url":"https://ex.com/a.jpg","sub_type":"sticker","width":100,"height":200}},
			{"type":"image","data":{"resource_id":"r2","temp_url":"https://ex.com/b.jpg"}},
			{"type":"record","data":{"resource_id":"r3","temp_url":"https://ex.com/c.mp3","duration":30}},
			{"type":"video","data":{"resource_id":"r4","temp_url":"https://ex.com/d.mp4","width":640,"height":480,"duration":60}},
			{"type":"file","data":{"file_id":"f1","file_name":"a.pdf","file_size":1024,"file_hash":"hash1"}},
			{"type":"market_face","data":{"emoji_package_id":5,"emoji_id":"e1","key":"k1","url":"https://ex.com/e.png","summary":"s1"}},
			{"type":"light_app","data":{"app_name":"App","json_payload":"{\"app\":\"x\"}"}},
			{"type":"xml","data":{"service_id":3,"xml_payload":"<msg/>"}}
		]}`)

	evt, err := parseRawEvent(raw)
	require.NoError(t, err)

	// 段模型：face/market_face/light_app/xml 为段而非附件；
	// Attachments() 仅派生 image/record/video/file（5 个）。
	segs := evt.Segments()
	require.Len(t, segs, 9)

	// face → SegmentFace
	assert.Equal(t, platform.SegmentFace, segs[0].Type)
	face := segs[0].Extra[ExtraKeyFace].(*FaceSegmentMeta)
	assert.Equal(t, "21", face.FaceID)
	assert.True(t, face.IsLarge)

	atts := evt.Attachments()
	require.Len(t, atts, 5)

	sticker := atts[0]
	assert.Equal(t, "https://ex.com/a.jpg", sticker.URL)
	assert.Equal(t, "image/jpeg", sticker.MimeType)
	assert.Equal(t, 100, sticker.Width)
	imgMeta := sticker.Extra[ExtraKeyImage].(*ImageSegmentMeta)
	assert.Equal(t, "sticker", imgMeta.SubType)
	assert.Equal(t, "r1", imgMeta.ResourceID)

	normal := atts[1].Extra[ExtraKeyImage].(*ImageSegmentMeta)
	assert.Equal(t, "normal", normal.SubType)

	rec := atts[2].Extra[ExtraKeyRecord].(*RecordSegmentMeta)
	assert.Equal(t, "r3", rec.ResourceID)
	assert.Equal(t, 30, rec.Duration)
	assert.Equal(t, "audio/mpeg", atts[2].MimeType)

	vid := atts[3].Extra[ExtraKeyVideo].(*VideoSegmentMeta)
	assert.Equal(t, "r4", vid.ResourceID)
	assert.Equal(t, 60, vid.Duration)

	file := atts[4].Extra[ExtraKeyFile].(*FileSegmentMeta)
	assert.Equal(t, "f1", file.FileID)
	assert.Equal(t, "a.pdf", file.FileName)
	assert.Equal(t, int64(1024), file.FileSize)
	assert.Equal(t, "hash1", file.FileHash)
	assert.Equal(t, "a.pdf", atts[4].Name)
	assert.Equal(t, 1024, atts[4].Size)

	// market_face → SegmentFace
	assert.Equal(t, platform.SegmentFace, segs[6].Type)
	mf := segs[6].Extra[ExtraKeyMarketFace].(*MarketFaceSegmentMeta)
	assert.Equal(t, 5, mf.EmojiPackageID)
	assert.Equal(t, "e1", mf.EmojiID)
	assert.Equal(t, "k1", mf.Key)

	// light_app / xml → SegmentUnknown（Extra 保留原始数据）
	assert.Equal(t, platform.SegmentUnknown, segs[7].Type)
	la := segs[7].Extra[ExtraKeyLightApp].(*LightAppSegmentMeta)
	assert.Equal(t, "App", la.AppName)
	assert.Equal(t, `{"app":"x"}`, la.JSONPayload)

	assert.Equal(t, platform.SegmentUnknown, segs[8].Type)
	xml := segs[8].Extra[ExtraKeyXML].(*XMLSegmentMeta)
	assert.Equal(t, 3, xml.ServiceID)
	assert.Equal(t, "<msg/>", xml.XMLPayload)
}

// ────────────────────────────────────────────────────────────────────────────
// 其余事件类型
// ────────────────────────────────────────────────────────────────────────────

func TestParseMessageRecallEvent(t *testing.T) {
	raw := envelope("message_recall", `{"message_scene":"group","peer_id":555,"message_seq":123,"sender_id":1,"operator_id":2,"display_suffix":"撤回"}`)

	evt, err := parseRawEvent(raw)
	require.NoError(t, err)

	assert.Equal(t, platform.EventKindMessageDelete, evt.Kind())
	assert.Equal(t, "123", evt.ID())
	assert.Equal(t, "group:555", evt.Chat().ID)
	assert.Equal(t, "2", evt.Sender().ID)
	rawEvt, ok := evt.(platform.RawEvent)
	require.True(t, ok)
	assert.Equal(t, "message_recall", rawEvt.RawType())
	assert.NotNil(t, rawEvt.RawPayload())
}

func TestParseGroupMemberIncreaseEvent(t *testing.T) {
	raw := envelope("group_member_increase", `{"group_id":555,"user_id":3,"operator_id":1,"invitor_id":2}`)

	evt, err := parseRawEvent(raw)
	require.NoError(t, err)

	assert.Equal(t, platform.EventKindMemberJoin, evt.Kind())
	assert.Equal(t, "group:555", evt.Chat().ID)
	assert.Equal(t, "3", evt.Sender().ID)
	assert.Contains(t, evt.ID(), "member_join:555:3")
}

func TestParseGroupMemberDecreaseEvent(t *testing.T) {
	raw := envelope("group_member_decrease", `{"group_id":555,"user_id":3,"operator_id":1}`)

	evt, err := parseRawEvent(raw)
	require.NoError(t, err)

	assert.Equal(t, platform.EventKindMemberLeave, evt.Kind())
	assert.Contains(t, evt.ID(), "member_leave:555:3")
}

func TestParseGroupAdminChangeEvent(t *testing.T) {
	raw := envelope("group_admin_change", `{"group_id":555,"user_id":3,"operator_id":1,"is_set":true}`)

	evt, err := parseRawEvent(raw)
	require.NoError(t, err)

	assert.Equal(t, platform.EventKindMemberUpdate, evt.Kind())
	assert.Contains(t, evt.ID(), "admin_change:555:3")
}

func TestParseGroupMuteEvent(t *testing.T) {
	raw := envelope("group_mute", `{"group_id":555,"user_id":3,"operator_id":1,"duration":600}`)

	evt, err := parseRawEvent(raw)
	require.NoError(t, err)

	assert.Equal(t, platform.EventKindNotice, evt.Kind())
	assert.Contains(t, evt.ID(), "group_mute:555:3")
}

func TestParseGroupWholeMuteEvent(t *testing.T) {
	raw := envelope("group_whole_mute", `{"group_id":555,"operator_id":1,"is_mute":true}`)

	evt, err := parseRawEvent(raw)
	require.NoError(t, err)

	assert.Equal(t, platform.EventKindNotice, evt.Kind())
	assert.Equal(t, "1", evt.Sender().ID)
}

func TestParseGroupMessageReactionEvent(t *testing.T) {
	raw := envelope("group_message_reaction", `{"group_id":555,"user_id":3,"message_seq":123,"face_id":"21","reaction_type":"face","is_add":true}`)

	evt, err := parseRawEvent(raw)
	require.NoError(t, err)

	assert.Equal(t, platform.EventKindReaction, evt.Kind())
	assert.Contains(t, evt.ID(), "reaction:555:3:123:21")
}

func TestParseFriendRequestEvent(t *testing.T) {
	raw := envelope("friend_request", `{"initiator_id":200,"initiator_uid":"uid200","comment":"hi","via":"search"}`)

	evt, err := parseRawEvent(raw)
	require.NoError(t, err)

	assert.Equal(t, platform.EventKindRequest, evt.Kind())
	assert.Equal(t, "friend:200", evt.Chat().ID)
	assert.Equal(t, "200", evt.Sender().ID)
	assert.Contains(t, evt.ID(), "friend_req:200")
}

func TestParseGroupJoinRequestEvent(t *testing.T) {
	raw := envelope("group_join_request", `{"group_id":555,"notification_seq":9,"is_filtered":false,"initiator_id":7,"comment":"申请"}`)

	evt, err := parseRawEvent(raw)
	require.NoError(t, err)

	assert.Equal(t, platform.EventKindRequest, evt.Kind())
	assert.Equal(t, "group:555", evt.Chat().ID)
	assert.Equal(t, "group_req:555:7:9", evt.ID())
}

func TestParseGroupInvitationEvent(t *testing.T) {
	raw := envelope("group_invitation", `{"group_id":555,"invitation_seq":42,"initiator_id":7,"source_group_id":111}`)

	evt, err := parseRawEvent(raw)
	require.NoError(t, err)

	assert.Equal(t, platform.EventKindRequest, evt.Kind())
	assert.Equal(t, "group_inv:555:42", evt.ID())
}

func TestParseBotOfflineEvent(t *testing.T) {
	raw := envelope("bot_offline", `{"reason":"登录被顶"}`)

	evt, err := parseRawEvent(raw)
	require.NoError(t, err)

	assert.Equal(t, platform.EventKindSystem, evt.Kind())
	assert.Equal(t, "登录被顶", evt.Content())
}

func TestParsePeerPinChangeEvent(t *testing.T) {
	raw := envelope("peer_pin_change", `{"message_scene":"friend","peer_id":1001,"is_pinned":true}`)

	evt, err := parseRawEvent(raw)
	require.NoError(t, err)

	assert.Equal(t, platform.EventKindNotice, evt.Kind())
	assert.Equal(t, "friend:1001", evt.Chat().ID)
	assert.Contains(t, evt.ID(), "peer_pin_change:friend:1001")
}

func TestParseGroupInvitedJoinRequestEvent(t *testing.T) {
	raw := envelope("group_invited_join_request", `{"group_id":555,"notification_seq":9,"initiator_id":7,"target_user_id":8}`)

	evt, err := parseRawEvent(raw)
	require.NoError(t, err)

	assert.Equal(t, platform.EventKindRequest, evt.Kind())
	assert.Equal(t, "invited_join_req:555:7:9", evt.ID())
}

func TestParseFriendNudgeEvent(t *testing.T) {
	raw := envelope("friend_nudge", `{"user_id":1001,"is_self_send":false,"is_self_receive":true,"display_action":"戳了戳","display_suffix":"","display_action_img_url":""}`)

	evt, err := parseRawEvent(raw)
	require.NoError(t, err)

	assert.Equal(t, platform.EventKindNotice, evt.Kind())
	assert.Equal(t, "friend:1001", evt.Chat().ID)
	assert.Equal(t, "戳了戳", evt.Content())
}

func TestParseFriendFileUploadEvent(t *testing.T) {
	raw := envelope("friend_file_upload", `{"user_id":1001,"file_id":"f1","file_name":"a.pdf","file_size":1024,"file_hash":"h1","is_self":false}`)

	evt, err := parseRawEvent(raw)
	require.NoError(t, err)

	assert.Equal(t, platform.EventKindNotice, evt.Kind())
	require.Len(t, evt.Attachments(), 1)
	assert.Equal(t, "a.pdf", evt.Attachments()[0].Name)
	meta := evt.Attachments()[0].Extra[ExtraKeyFile].(*FileSegmentMeta)
	assert.Equal(t, "f1", meta.FileID)
}

func TestParseGroupEssenceMessageChangeEvent(t *testing.T) {
	raw := envelope("group_essence_message_change", `{"group_id":555,"message_seq":123,"operator_id":1,"is_set":true}`)

	evt, err := parseRawEvent(raw)
	require.NoError(t, err)

	assert.Equal(t, platform.EventKindNotice, evt.Kind())
	assert.Contains(t, evt.ID(), "essence_change:555:123")
}

func TestParseGroupNameChangeEvent(t *testing.T) {
	raw := envelope("group_name_change", `{"group_id":555,"new_group_name":"新群名","operator_id":1}`)

	evt, err := parseRawEvent(raw)
	require.NoError(t, err)

	assert.Equal(t, platform.EventKindNotice, evt.Kind())
	assert.Equal(t, "新群名", evt.Chat().Name)
	assert.Equal(t, "新群名", evt.Content())
}

func TestParseGroupNudgeEvent(t *testing.T) {
	raw := envelope("group_nudge", `{"group_id":555,"sender_id":1,"receiver_id":2,"display_action":"戳了戳","display_suffix":"","display_action_img_url":""}`)

	evt, err := parseRawEvent(raw)
	require.NoError(t, err)

	assert.Equal(t, platform.EventKindNotice, evt.Kind())
	assert.Equal(t, "1", evt.Sender().ID)
	assert.Equal(t, "戳了戳", evt.Content())
}

func TestParseGroupFileUploadEvent(t *testing.T) {
	raw := envelope("group_file_upload", `{"group_id":555,"user_id":1,"file_id":"f1","file_name":"b.pdf","file_size":2048}`)

	evt, err := parseRawEvent(raw)
	require.NoError(t, err)

	assert.Equal(t, platform.EventKindNotice, evt.Kind())
	require.Len(t, evt.Attachments(), 1)
	assert.Equal(t, "b.pdf", evt.Attachments()[0].Name)
	assert.Equal(t, 2048, evt.Attachments()[0].Size)
}

// ────────────────────────────────────────────────────────────────────────────
// 兜底与错误路径
// ────────────────────────────────────────────────────────────────────────────

func TestParseRawEvent_UnknownType(t *testing.T) {
	raw := envelope("some_new_event", `{"anything":true}`)

	evt, err := parseRawEvent(raw)
	require.NoError(t, err)

	assert.Equal(t, platform.EventKindNotice, evt.Kind())
	assert.Contains(t, evt.ID(), "some_new_event")
	assert.Equal(t, int64(1700000000), evt.Timestamp().Unix())
	rawEvt, ok := evt.(platform.RawEvent)
	require.True(t, ok)
	assert.Equal(t, "some_new_event", rawEvt.RawType())
}

func TestParseRawEvent_MalformedEnvelope(t *testing.T) {
	_, err := parseRawEvent([]byte(`not-json`))
	assert.Error(t, err)
}

func TestParseRawEvent_MalformedData(t *testing.T) {
	raw := envelope("message_receive", `{broken`)
	_, err := parseRawEvent(raw)
	assert.Error(t, err)
}

func TestParseRawEvent_MalformedRecall(t *testing.T) {
	raw := envelope("message_recall", `{broken`)
	_, err := parseRawEvent(raw)
	assert.Error(t, err)
}
