package onebot

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockAPIClient 记录动作名与参数，并按需返回预设响应数据。
type mockAPIClient struct {
	actions []string
	params  []any
	// data 按动作名返回响应 data（JSON 字符串）；未命中返回空对象。
	data map[string]string
	// notFound 中的动作名返回"动作不存在"错误（用于测试 fallback）。
	notFound map[string]bool
}

func (m *mockAPIClient) Call(_ context.Context, action string, params any) (*APIResponse, error) {
	m.actions = append(m.actions, action)
	m.params = append(m.params, params)
	if m.notFound[action] {
		return &APIResponse{Status: "failed", Retcode: 404, Data: json.RawMessage("null")},
			fmt.Errorf("onebot api: action failed (status=failed, retcode=404)")
	}
	raw, ok := m.data[action]
	if !ok {
		raw = "{}"
	}
	return &APIResponse{Status: "ok", Retcode: 0, Data: json.RawMessage(raw)}, nil
}

func newMockSender(t *testing.T) (*Sender, *mockAPIClient) {
	t.Helper()
	m := &mockAPIClient{data: map[string]string{}}
	return newSender(m), m
}

// assertAction 断言最近一次调用的动作名，并验证参数 JSON 语义等价。
func assertAction(t *testing.T, m *mockAPIClient, wantAction, wantParams string) {
	t.Helper()
	require.NotEmpty(t, m.actions)
	got := m.actions[len(m.actions)-1]
	assert.Equal(t, wantAction, got)
	if wantParams != "" {
		raw, err := json.Marshal(m.params[len(m.params)-1])
		require.NoError(t, err)
		assert.JSONEq(t, wantParams, string(raw))
	}
}

// ────────────────────────────────────────────────────────────────────────────
// 消息收发与检索
// ────────────────────────────────────────────────────────────────────────────

func TestSender_SendMsg(t *testing.T) {
	s, m := newMockSender(t)
	chain := MessageChain{textSegment("hi")}

	_, err := s.SendMsg(context.Background(), SendMsgParams{MessageType: "group", GroupID: 555, Message: chain})
	require.NoError(t, err)
	assertAction(t, m, "send_msg", `{"message_type":"group","group_id":555,"message":[{"type":"text","data":{"text":"hi"}}]}`)
}

func TestSender_SendGroupForwardMsg(t *testing.T) {
	s, m := newMockSender(t)
	node, err := NewNodeSegment("10001", "小明", MessageChain{textSegment("hello")})
	require.NoError(t, err)

	_, err = s.SendGroupForwardMsg(context.Background(), 555, MessageChain{node})
	require.NoError(t, err)
	assertAction(t, m, "send_group_forward_msg", "")
	assert.Equal(t, "send_group_forward_msg", m.actions[len(m.actions)-1])
}

func TestSender_SendPrivateForwardMsg(t *testing.T) {
	s, m := newMockSender(t)
	node, err := NewNodeSegment("10001", "小明", MessageChain{textSegment("hi")})
	require.NoError(t, err)

	_, err = s.SendPrivateForwardMsg(context.Background(), 1001, MessageChain{node})
	require.NoError(t, err)
	assertAction(t, m, "send_private_forward_msg", `{"user_id":1001,"messages":[{"type":"node","data":{"user_id":"10001","nickname":"小明","content":[{"type":"text","data":{"text":"hi"}}]}}]}`)
}

func TestSender_GetGroupMsgHistory(t *testing.T) {
	s, m := newMockSender(t)
	m.data["get_group_msg_history"] = `{"messages":[{"time":1,"message_id":2,"group_id":555,"user_id":3,"message":[{"type":"text","data":{"text":"hi"}}],"sender":{"user_id":3,"nickname":"A"}}]}`

	res, err := s.GetGroupMsgHistory(context.Background(), 555, 100, 10)
	require.NoError(t, err)
	require.Len(t, res.Messages, 1)
	assert.Equal(t, int32(2), res.Messages[0].MessageID)
	assertAction(t, m, "get_group_msg_history", `{"group_id":555,"message_seq":100,"count":10}`)
}

func TestSender_GetFriendMsgHistory(t *testing.T) {
	s, m := newMockSender(t)
	_, err := s.GetFriendMsgHistory(context.Background(), 1001, 0, 20)
	require.NoError(t, err)
	assertAction(t, m, "get_friend_msg_history", `{"user_id":1001,"count":20}`)
}

func TestSender_EssenceMsg(t *testing.T) {
	s, m := newMockSender(t)
	m.data["get_essence_msg_list"] = `[{"sender_id":1,"operator_id":2,"message_id":3,"group_id":555,"message":[{"type":"text","data":{"text":"精华"}}],"sender_nick":"A"}]`

	list, err := s.GetEssenceMsgList(context.Background(), 555)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "精华", list[0].Message.Text())
	assertAction(t, m, "get_essence_msg_list", `{"group_id":555}`)

	require.NoError(t, s.SetEssenceMsg(context.Background(), 3))
	assertAction(t, m, "set_essence_msg", `{"message_id":3}`)

	require.NoError(t, s.DeleteEssenceMsg(context.Background(), 3))
	assertAction(t, m, "delete_essence_msg", `{"message_id":3}`)
}

func TestSender_MarkMsgAsRead(t *testing.T) {
	s, m := newMockSender(t)
	require.NoError(t, s.MarkMsgAsRead(context.Background(), 42))
	assertAction(t, m, "mark_msg_as_read", `{"message_id":42}`)
}

func TestSender_UploadFiles(t *testing.T) {
	s, m := newMockSender(t)

	require.NoError(t, s.UploadGroupFile(context.Background(), 555, "/tmp/a.pdf", "a.pdf", ""))
	assertAction(t, m, "upload_group_file", `{"group_id":555,"file":"/tmp/a.pdf","name":"a.pdf"}`)

	require.NoError(t, s.UploadPrivateFile(context.Background(), 1001, "https://ex.com/b.pdf", "b.pdf"))
	assertAction(t, m, "upload_private_file", `{"user_id":1001,"file":"https://ex.com/b.pdf","name":"b.pdf"}`)
}

func TestSender_DownloadFile(t *testing.T) {
	s, m := newMockSender(t)
	m.data["download_file"] = `{"file":"/data/cache/x.pdf"}`

	res, err := s.DownloadFile(context.Background(), "https://ex.com/x.pdf", 2, map[string]string{"A": "b"}, 5000)
	require.NoError(t, err)
	assert.Equal(t, "/data/cache/x.pdf", res.File)
	assertAction(t, m, "download_file", `{"url":"https://ex.com/x.pdf","thread_count":2,"headers":{"A":"b"},"timeout":5000}`)
}

func TestSender_GetFile(t *testing.T) {
	s, m := newMockSender(t)
	m.data["get_file"] = `{"file":"/data/f1","url":"https://ex.com/f1","file_size":1024}`

	res, err := s.GetFile(context.Background(), "f1")
	require.NoError(t, err)
	assert.Equal(t, "/data/f1", res.File)
	assertAction(t, m, "get_file", `{"file_id":"f1"}`)
}

func TestSender_OCRImage(t *testing.T) {
	s, m := newMockSender(t)
	m.data["ocr_image"] = `{"texts":[{"text":"你好","confidence":0.99}]}`

	res, err := s.OCRImage(context.Background(), "img.jpg")
	require.NoError(t, err)
	require.Len(t, res.Texts, 1)
	assert.Equal(t, "你好", res.Texts[0].Text)
	assertAction(t, m, "ocr_image", `{"image":"img.jpg"}`)
}

func TestSender_DeleteFriend(t *testing.T) {
	s, m := newMockSender(t)
	require.NoError(t, s.DeleteFriend(context.Background(), 1001))
	assertAction(t, m, "delete_friend", `{"user_id":1001}`)
}

func TestSender_GroupNotice(t *testing.T) {
	s, m := newMockSender(t)
	m.data["get_group_notice"] = `{"notices":[{"notice_id":"n1","content":"公告"}]}`

	require.NoError(t, s.SendGroupNotice(context.Background(), 555, "内容", "https://ex.com/i.png"))
	assertAction(t, m, "send_group_notice", `{"group_id":555,"content":"内容","image":"https://ex.com/i.png"}`)

	notices, err := s.GetGroupNotice(context.Background(), 555)
	require.NoError(t, err)
	require.Len(t, notices, 1)
	assert.Equal(t, "公告", notices[0].Content)
	assertAction(t, m, "get_group_notice", `{"group_id":555}`)
}

func TestSender_SetGroupPortrait(t *testing.T) {
	s, m := newMockSender(t)
	require.NoError(t, s.SetGroupPortrait(context.Background(), 555, "/tmp/avatar.png"))
	assertAction(t, m, "set_group_portrait", `{"group_id":555,"file":"/tmp/avatar.png"}`)
}

func TestSender_GetGroupSystemMsg(t *testing.T) {
	s, m := newMockSender(t)
	m.data["get_group_system_msg"] = `{"join_requests":[{"request_id":1,"user_id":2,"nickname":"N","group_id":555,"group_name":"G","checked":false}]}`

	res, err := s.GetGroupSystemMsg(context.Background())
	require.NoError(t, err)
	require.Len(t, res.JoinRequests, 1)
	assert.Equal(t, "N", res.JoinRequests[0].Nickname)
	assertAction(t, m, "get_group_system_msg", "{}")
}

func TestSender_GetGuildList(t *testing.T) {
	s, m := newMockSender(t)
	m.data["get_guild_list"] = `[{"guild_id":"1","guild_name":"频道"}]`

	guilds, err := s.GetGuildList(context.Background())
	require.NoError(t, err)
	require.Len(t, guilds, 1)
	assert.Equal(t, "频道", guilds[0].GuildName)
	assertAction(t, m, "get_guild_list", "{}")
}

func TestSender_SendGroupSign(t *testing.T) {
	s, m := newMockSender(t)
	require.NoError(t, s.SendGroupSign(context.Background(), 555))
	assertAction(t, m, "send_group_sign", `{"group_id":555}`)
}

// ────────────────────────────────────────────────────────────────────────────
// 戳一戳与表情回应
// ────────────────────────────────────────────────────────────────────────────

func TestSender_Poke(t *testing.T) {
	s, m := newMockSender(t)

	require.NoError(t, s.SendPoke(context.Background(), 555, 0, 1001))
	assertAction(t, m, "send_poke", `{"group_id":555,"target_id":1001}`)

	require.NoError(t, s.FriendPoke(context.Background(), 1001))
	assertAction(t, m, "friend_poke", `{"user_id":1001}`)

	require.NoError(t, s.GroupPoke(context.Background(), 555, 1001))
	assertAction(t, m, "group_poke", `{"group_id":555,"user_id":1001}`)
}

func TestSender_EmojiLike(t *testing.T) {
	s, m := newMockSender(t)
	m.data["fetch_emoji_like"] = `{"likes":[{"emoji_id":"21","emoji_count":1}]}`

	require.NoError(t, s.SetMsgEmojiLike(context.Background(), 42, "21"))
	assertAction(t, m, "set_msg_emoji_like", `{"message_id":42,"emoji_id":"21"}`)

	require.NoError(t, s.UnsetMsgEmojiLike(context.Background(), 42, "21"))
	assertAction(t, m, "unset_msg_emoji_like", `{"message_id":42,"emoji_id":"21"}`)

	res, err := s.FetchEmojiLike(context.Background(), 42)
	require.NoError(t, err)
	require.Len(t, res.Likes, 1)
	assert.Equal(t, "21", res.Likes[0].EmojiID)
	assertAction(t, m, "fetch_emoji_like", `{"message_id":42}`)
}

func TestSender_SetGroupReaction(t *testing.T) {
	s, m := newMockSender(t)
	require.NoError(t, s.SetGroupReaction(context.Background(), 555, 42, "21", true))
	assertAction(t, m, "set_group_reaction", `{"group_id":555,"message_id":42,"code":"21","is_add":true}`)
}

// ────────────────────────────────────────────────────────────────────────────
// 账号资料
// ────────────────────────────────────────────────────────────────────────────

func TestSender_QQProfile(t *testing.T) {
	s, m := newMockSender(t)
	m.data["get_qq_avatar"] = `{"url":"https://ex.com/a.png"}`

	require.NoError(t, s.SetQQAvatar(context.Background(), "/tmp/a.png"))
	assertAction(t, m, "set_qq_avatar", `{"file":"/tmp/a.png"}`)

	avatar, err := s.GetQQAvatar(context.Background(), 1001)
	require.NoError(t, err)
	assert.Equal(t, "https://ex.com/a.png", avatar.URL)
	assertAction(t, m, "get_qq_avatar", `{"user_id":1001}`)

	require.NoError(t, s.SetQQProfile(context.Background(), SetQQProfileParams{Nickname: "新昵称"}))
	assertAction(t, m, "set_qq_profile", `{"nickname":"新昵称"}`)

	require.NoError(t, s.SetFriendRemark(context.Background(), 1001, "备注"))
	assertAction(t, m, "set_friend_remark", `{"user_id":1001,"remark":"备注"}`)

	require.NoError(t, s.SetInputStatus(context.Background(), 0, 30))
	assertAction(t, m, "set_input_status", `{"event_type":0,"times":30}`)
}

// ────────────────────────────────────────────────────────────────────────────
// 群管理扩展
// ────────────────────────────────────────────────────────────────────────────

func TestSender_GroupManagementExtensions(t *testing.T) {
	s, m := newMockSender(t)
	m.data["get_group_shut_list"] = `{"members":[{"user_id":2,"shut_up_time":1700000000}]}`

	res, err := s.GetGroupShutList(context.Background(), 555)
	require.NoError(t, err)
	require.Len(t, res.Members, 1)
	assert.Equal(t, int64(2), res.Members[0].UserID)
	assertAction(t, m, "get_group_shut_list", `{"group_id":555}`)

	require.NoError(t, s.SetGroupMsgMask(context.Background(), 555, 2))
	assertAction(t, m, "set_group_msg_mask", `{"group_id":555,"mask":2}`)

	require.NoError(t, s.BatchDeleteGroupMember(context.Background(), 555, []int64{2, 3}, true))
	assertAction(t, m, "batch_delete_group_member", `{"group_id":555,"user_ids":[2,3],"reject_add_request":true}`)
}

// ────────────────────────────────────────────────────────────────────────────
// AI 角色
// ────────────────────────────────────────────────────────────────────────────

func TestSender_AI(t *testing.T) {
	s, m := newMockSender(t)
	m.data["get_ai_characters"] = `[{"character_id":1,"character_name":"小智"}]`
	m.data["send_group_ai_record"] = `{"message_id":42}`
	m.data["get_ai_record"] = `[{"time":1,"message_id":2,"character_id":1,"message":[{"type":"text","data":{"text":"hi"}}]}]`

	chars, err := s.GetAICharacters(context.Background(), 555, 0)
	require.NoError(t, err)
	require.Len(t, chars, 1)
	assert.Equal(t, "小智", chars[0].CharacterName)
	assertAction(t, m, "get_ai_characters", `{"group_id":555}`)

	res, err := s.SendGroupAIRecord(context.Background(), 555, 1)
	require.NoError(t, err)
	assert.Equal(t, int64(42), res.MessageID)
	assertAction(t, m, "send_group_ai_record", `{"group_id":555,"character_id":1}`)

	records, err := s.GetAIRecord(context.Background(), 1)
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "hi", records[0].Message.Text())
	assertAction(t, m, "get_ai_record", `{"character_id":1}`)
}

// ────────────────────────────────────────────────────────────────────────────
// 群文件
// ────────────────────────────────────────────────────────────────────────────

func TestSender_GroupFileQueries(t *testing.T) {
	s, m := newMockSender(t)
	m.data["get_group_file_system_info"] = `{"file_count":3,"limit_count":100,"used_space":1024}`
	m.data["get_group_root_files"] = `{"files":[{"file_id":"f1","file_name":"a.txt","file_size":10}],"folders":[{"folder_id":"d1","folder_name":"docs"}]}`
	m.data["get_group_file_url"] = `{"url":"https://ex.com/f1"}`
	m.data["get_private_file_url"] = `{"url":"https://ex.com/p1"}`

	info, err := s.GetGroupFileSystemInfo(context.Background(), 555)
	require.NoError(t, err)
	assert.Equal(t, int64(3), info.FileCount)
	assertAction(t, m, "get_group_file_system_info", `{"group_id":555}`)

	files, err := s.GetGroupRootFiles(context.Background(), 555)
	require.NoError(t, err)
	require.Len(t, files.Files, 1)
	assert.Equal(t, "a.txt", files.Files[0].FileName)
	require.Len(t, files.Folders, 1)
	assert.Equal(t, "docs", files.Folders[0].FolderName)
	assertAction(t, m, "get_group_root_files", `{"group_id":555}`)

	byFolder, err := s.GetGroupFilesByFolder(context.Background(), 555, "d1")
	require.NoError(t, err)
	require.Empty(t, byFolder.Files)
	assertAction(t, m, "get_group_files_by_folder", `{"group_id":555,"folder_id":"d1"}`)

	url, err := s.GetGroupFileURL(context.Background(), 555, "f1")
	require.NoError(t, err)
	assert.Equal(t, "https://ex.com/f1", url.URL)
	assertAction(t, m, "get_group_file_url", `{"group_id":555,"file_id":"f1"}`)

	purl, err := s.GetPrivateFileURL(context.Background(), 1001, "f1")
	require.NoError(t, err)
	assert.Equal(t, "https://ex.com/p1", purl.URL)
	assertAction(t, m, "get_private_file_url", `{"user_id":1001,"file_id":"f1"}`)
}

func TestSender_GroupFileMutations(t *testing.T) {
	s, m := newMockSender(t)
	m.data["create_group_file_folder"] = `{"folder_id":"d9"}`

	require.NoError(t, s.MoveGroupFile(context.Background(), 555, "f1", "p1", "t1"))
	assertAction(t, m, "move_group_file", `{"group_id":555,"file_id":"f1","parent_id":"p1","target_id":"t1"}`)

	require.NoError(t, s.RenameGroupFile(context.Background(), 555, "f1", "新名.txt"))
	assertAction(t, m, "rename_group_file", `{"group_id":555,"file_id":"f1","new_name":"新名.txt"}`)

	require.NoError(t, s.DeleteGroupFile(context.Background(), 555, "f1"))
	assertAction(t, m, "delete_group_file", `{"group_id":555,"file_id":"f1"}`)

	folder, err := s.CreateGroupFileFolder(context.Background(), 555, "新文件夹")
	require.NoError(t, err)
	assert.Equal(t, "d9", folder.FolderID)
	assertAction(t, m, "create_group_file_folder", `{"group_id":555,"name":"新文件夹"}`)

	require.NoError(t, s.DeleteGroupFolder(context.Background(), 555, "d1"))
	assertAction(t, m, "delete_group_folder", `{"group_id":555,"folder_id":"d1"}`)

	require.NoError(t, s.RenameGroupFileFolder(context.Background(), 555, "d1", "改名"))
	assertAction(t, m, "rename_group_file_folder", `{"group_id":555,"folder_id":"d1","new_name":"改名"}`)

	require.NoError(t, s.SetGroupFileForever(context.Background(), 555, "f1"))
	assertAction(t, m, "set_group_file_forever", `{"group_id":555,"file_id":"f1"}`)
}

// ────────────────────────────────────────────────────────────────────────────
// LLOneBot/LuckyLilliaBot 专有扩展（2026-08 对照 LLB 源码核验）
// ────────────────────────────────────────────────────────────────────────────

func TestSender_LLBSystem(t *testing.T) {
	s, m := newMockSender(t)
	m.data["get_config"] = `{"log":true}`
	m.data["get_robot_uin_range"] = `[{"min_uin":"1","max_uin":"2"}]`
	m.data["send_pb"] = `{"cmd":"0x1234","hex":"aabb"}`
	m.data["get_rkey"] = `{"private_key":"pk","group_key":"gk","expired_time":100,"updated_time":"2026-01-01 00:00:00"}`
	m.data["scan_qrcode"] = `[{"text":"hello"}]`
	m.data["get_event"] = `[]`

	cfg, err := s.GetConfig(context.Background())
	require.NoError(t, err)
	assert.True(t, cfg.Log)
	assertAction(t, m, "get_config", "{}")

	require.NoError(t, s.SetConfig(context.Background(), LLOneBotConfig{Log: true}))
	assertAction(t, m, "set_config", `{"log":true}`)

	require.NoError(t, s.LLOneBotDebug(context.Background(), "ntMsgApi", "sendMsg", 1, "x"))
	assertAction(t, m, "llonebot_debug", `{"apiClass":"ntMsgApi","method":"sendMsg","args":[1,"x"]}`)

	_, err = s.GetEvent(context.Background(), "k1", 1000)
	require.NoError(t, err)
	assertAction(t, m, "get_event", `{"key":"k1","timeout":1000}`)

	ranges, err := s.GetRobotUinRange(context.Background())
	require.NoError(t, err)
	require.Len(t, ranges, 1)
	assert.Equal(t, "1", ranges[0].MinUin)
	assertAction(t, m, "get_robot_uin_range", "{}")

	pb, err := s.SendPB(context.Background(), "0x1234", "aabb")
	require.NoError(t, err)
	assert.Equal(t, "0x1234", pb.Cmd)
	assertAction(t, m, "send_pb", `{"cmd":"0x1234","hex":"aabb"}`)

	qr, err := s.ScanQRCode(context.Background(), "/tmp/qr.png")
	require.NoError(t, err)
	require.Len(t, qr, 1)
	assert.Equal(t, "hello", qr[0].Text)
	assertAction(t, m, "scan_qrcode", `{"file":"/tmp/qr.png"}`)

	rkey, err := s.GetRkey(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "pk", rkey.PrivateKey)
	assertAction(t, m, "get_rkey", "{}")
}

func TestSender_LLBMessage(t *testing.T) {
	s, m := newMockSender(t)
	m.data["forward_friend_single_msg"] = `{"message_id":99}`
	m.data["forward_group_single_msg"] = `{"message_id":99}`
	m.data["fetch_custom_face"] = `["https://ex.com/1.png"]`
	m.data["voice_msg_to_text"] = `{"text":"语音转文字"}`
	m.data["get_recommend_face"] = `{"url":["https://ex.com/r.png"]}`

	res, err := s.ForwardFriendSingleMsg(context.Background(), 123, 1001)
	require.NoError(t, err)
	assert.Equal(t, int64(99), res.MessageID)
	assertAction(t, m, "forward_friend_single_msg", `{"message_id":123,"user_id":1001}`)

	_, err = s.ForwardGroupSingleMsg(context.Background(), 123, 555)
	require.NoError(t, err)
	assertAction(t, m, "forward_group_single_msg", `{"message_id":123,"group_id":555}`)

	faces, err := s.FetchCustomFace(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"https://ex.com/1.png"}, faces)
	assertAction(t, m, "fetch_custom_face", "{}")

	text, err := s.VoiceMsg2Text(context.Background(), 123)
	require.NoError(t, err)
	assert.Equal(t, "语音转文字", text.Text)
	assertAction(t, m, "voice_msg_to_text", `{"message_id":123}`)

	rec, err := s.GetRecommendFace(context.Background(), "猫")
	require.NoError(t, err)
	require.Len(t, rec.URL, 1)
	assertAction(t, m, "get_recommend_face", `{"word":"猫"}`)
}

func TestSender_LLBUser(t *testing.T) {
	s, m := newMockSender(t)
	m.data["get_profile_like"] = `{"users":[{"uin":1001,"is_friend":true}],"next_start":20}`
	m.data["get_profile_like_me"] = `{"users":[],"next_start":0}`
	m.data["get_profile_like_count"] = `{"count":5}`
	m.data["get_friends_with_category"] = `[{"category_id":1,"category_name":"好友","buddy_list":[{"user_id":1001,"nickname":"A"}]}]`
	m.data["get_doubt_friends_add_request"] = `[{"flag":"f1","uin":"2","nick":"N","type":"doubt"}]`

	require.NoError(t, s.SetOnlineStatus(context.Background(), 11, 0, 0))
	assertAction(t, m, "set_online_status", `{"status":11,"ext_status":0,"battery_status":0}`)

	likes, err := s.GetProfileLike(context.Background(), 0, 20)
	require.NoError(t, err)
	require.Len(t, likes.Users, 1)
	assert.Equal(t, int64(1001), likes.Users[0].UIN)
	assertAction(t, m, "get_profile_like", `{"count":20}`)

	_, err = s.GetProfileLikeMe(context.Background(), -1, 0)
	require.NoError(t, err)
	assertAction(t, m, "get_profile_like_me", `{"start":-1}`)

	count, err := s.GetProfileLikeCount(context.Background(), 1001)
	require.NoError(t, err)
	assert.Equal(t, 5, count.Count)
	assertAction(t, m, "get_profile_like_count", `{"user_id":1001}`)

	cats, err := s.GetFriendsWithCategory(context.Background())
	require.NoError(t, err)
	require.Len(t, cats, 1)
	require.Len(t, cats[0].BuddyList, 1)
	assertAction(t, m, "get_friends_with_category", "{}")

	require.NoError(t, s.SetFriendCategory(context.Background(), 1001, 2))
	assertAction(t, m, "set_friend_category", `{"user_id":1001,"category_id":2}`)

	doubts, err := s.GetDoubtFriendsAddRequest(context.Background(), 50)
	require.NoError(t, err)
	require.Len(t, doubts, 1)
	assert.Equal(t, "f1", doubts[0].Flag)
	assertAction(t, m, "get_doubt_friends_add_request", `{"count":50}`)

	require.NoError(t, s.SetDoubtFriendsAddRequest(context.Background(), "f1"))
	assertAction(t, m, "set_doubt_friends_add_request", `{"flag":"f1"}`)
}

func TestSender_LLBGroup(t *testing.T) {
	s, m := newMockSender(t)
	m.data["get_group_ignore_add_request"] = `[]`
	m.data["get_group_signed_list"] = `[{"user_id":2,"nick":"N","time":1700000000,"rank":1}]`

	// get_group_ignored_notifies 404 → 自动回退到 LLB 的 get_group_ignore_add_request。
	m.notFound = map[string]bool{"get_group_ignored_notifies": true}
	ignored, err := s.GetGroupIgnoredNotifies(context.Background())
	require.NoError(t, err)
	assert.Empty(t, ignored)
	assertAction(t, m, "get_group_ignore_add_request", "{}")

	require.NoError(t, s.SetGroupRemark(context.Background(), 555, "备注"))
	assertAction(t, m, "set_group_remark", `{"group_id":555,"remark":"备注"}`)

	signed, err := s.GetGroupSignedList(context.Background(), 555)
	require.NoError(t, err)
	require.Len(t, signed, 1)
	assert.Equal(t, int64(2), signed[0].UserID)
	assertAction(t, m, "get_group_signed_list", `{"group_id":555}`)

	require.NoError(t, s.DeleteGroupNotice(context.Background(), 555, "n1"))
	assertAction(t, m, "_delete_group_notice", `{"group_id":555,"notice_id":"n1"}`)
}

func TestSender_LLBFlashFile(t *testing.T) {
	s, m := newMockSender(t)
	m.data["upload_flash_file"] = `{"file_set_id":"fs1","share_link":"https://qfile.qq.com/q/code","expire_time":100}`
	m.data["get_flash_file_info"] = `{"file_set_id":"fs1","title":"闪传","share_link":"https://qfile.qq.com/q/code","total_file_size":10,"files":[{"name":"a.txt","size":10}]}`
	m.data["get_flash_file_download_urls"] = `{"file_set_id":"fs1","share_link":"","files":[{"name":"a.txt","size":10,"url":"https://ex.com/a","expire":100}]}`
	m.data["reshare_flash_file"] = `{"file_set_id":"fs1","share_link":"https://qfile.qq.com/q/new","expire_time":200}`

	up, err := s.UploadFlashFile(context.Background(), "标题", []string{"/tmp/a.txt"})
	require.NoError(t, err)
	assert.Equal(t, "fs1", up.FileSetID)
	assertAction(t, m, "upload_flash_file", `{"title":"标题","paths":["/tmp/a.txt"]}`)

	require.NoError(t, s.DownloadFlashFile(context.Background(), FlashFileParams{FileSetID: "fs1"}))
	assertAction(t, m, "download_flash_file", `{"file_set_id":"fs1"}`)

	info, err := s.GetFlashFileInfo(context.Background(), FlashFileParams{ShareLink: "https://qfile.qq.com/q/code"})
	require.NoError(t, err)
	require.Len(t, info.Files, 1)
	assert.Equal(t, "a.txt", info.Files[0].Name)
	assertAction(t, m, "get_flash_file_info", `{"share_link":"https://qfile.qq.com/q/code"}`)

	urls, err := s.GetFlashFileDownloadUrls(context.Background(), FlashFileParams{FileSetID: "fs1"})
	require.NoError(t, err)
	require.Len(t, urls.Files, 1)
	assertAction(t, m, "get_flash_file_download_urls", `{"file_set_id":"fs1"}`)

	reshare, err := s.ReShareFlashFile(context.Background(), FlashFileParams{FileSetID: "fs1"})
	require.NoError(t, err)
	assert.Equal(t, int64(200), reshare.ExpireTime)
	assertAction(t, m, "reshare_flash_file", `{"file_set_id":"fs1"}`)
}

func TestSender_LLBGroupAlbum(t *testing.T) {
	s, m := newMockSender(t)
	m.data["get_group_album_list"] = `[{"album_id":"a1","name":"相册","upload_number":"3"}]`
	m.data["create_group_album"] = `{"album_id":"a2","owner":"555","name":"新相册","desc":"描述"}`
	m.data["get_group_album_media_list"] = `{"album":{"album_id":"a1","name":"相册"},"media_list":[{"type":1,"upload_time":"100","image_url":"https://ex.com/i.png"}],"next_attach_info":"c1","next_has_more":true}`

	albums, err := s.GetGroupAlbumList(context.Background(), 555)
	require.NoError(t, err)
	require.Len(t, albums, 1)
	assert.Equal(t, "a1", albums[0].AlbumID)
	assertAction(t, m, "get_group_album_list", `{"group_id":555}`)

	created, err := s.CreateGroupAlbum(context.Background(), 555, "新相册", "描述")
	require.NoError(t, err)
	assert.Equal(t, "a2", created.AlbumID)
	assertAction(t, m, "create_group_album", `{"group_id":555,"name":"新相册","desc":"描述"}`)

	require.NoError(t, s.DeleteGroupAlbum(context.Background(), 555, "a1"))
	assertAction(t, m, "delete_group_album", `{"group_id":555,"album_id":"a1"}`)

	require.NoError(t, s.UploadGroupAlbum(context.Background(), 555, "a1", []string{"/tmp/i.png"}))
	assertAction(t, m, "upload_group_album", `{"group_id":555,"album_id":"a1","files":["/tmp/i.png"]}`)

	media, err := s.GetGroupAlbumMediaList(context.Background(), 555, "a1", "c1")
	require.NoError(t, err)
	require.Len(t, media.MediaList, 1)
	assert.True(t, media.NextHasMore)
	assertAction(t, m, "get_group_album_media_list", `{"group_id":555,"album_id":"a1","attach_info":"c1"}`)
}

// ────────────────────────────────────────────────────────────────────────────
// NapCat 补充动作（2026-08 对照 NapCat 源码核验）
// ────────────────────────────────────────────────────────────────────────────

func TestSender_NapCatGroup(t *testing.T) {
	s, m := newMockSender(t)
	m.data["get_group_info_ex"] = `{"group_id":555,"group_name":"群A","member_count":10}`
	m.data["get_group_detail_info"] = `{"group_id":555}`
	m.data["get_group_ignored_notifies"] = `[{"group_id":555,"user_id":2,"flag":"f1"}]`

	require.NoError(t, s.BatchDeleteGroupMember(context.Background(), 555, []int64{2, 3}, true))
	assertAction(t, m, "batch_delete_group_member", `{"group_id":555,"user_ids":[2,3],"reject_add_request":true}`)

	// get_group_detail_info 404 → 回退 get_group_info_ex。
	m.notFound = map[string]bool{"get_group_detail_info": true}
	ex, err := s.GetGroupDetailInfo(context.Background(), 555)
	require.NoError(t, err)
	assert.Equal(t, "群A", ex.GroupName)
	assertAction(t, m, "get_group_info_ex", `{"group_id":555}`)
	require.NoError(t, s.SetGroupMemberInvitePolicy(context.Background(), 555, 1))
	assertAction(t, m, "set_group_member_invite_policy", `{"group_id":555,"policy":1}`)

	require.NoError(t, s.SetGroupMemberPermissions(context.Background(), SetGroupMemberPermissionsParams{GroupID: 555, AllowMemberCreateGroup: boolPtr(true)}))
	assertAction(t, m, "set_group_member_permissions", `{"group_id":555,"allow_member_create_group":true}`)

	require.NoError(t, s.SetGroupNewMemberHistoryVisibility(context.Background(), 555, true))
	assertAction(t, m, "set_group_new_member_history_visibility", `{"group_id":555,"visible":true}`)

	require.NoError(t, s.SetGroupAddOption(context.Background(), SetGroupAddOptionParams{GroupID: 555, AddType: 1}))
	assertAction(t, m, "set_group_add_option", `{"group_id":555,"add_type":1}`)

	require.NoError(t, s.SetGroupRobotAddOption(context.Background(), SetGroupRobotAddOptionParams{GroupID: 555, RobotMemberSwitch: 1}))
	assertAction(t, m, "set_group_robot_add_option", `{"group_id":555,"robot_member_switch":1}`)

	require.NoError(t, s.SetGroupSearch(context.Background(), 555, 0, 0))
	assertAction(t, m, "set_group_search", `{"group_id":555}`)

	require.NoError(t, s.SendGroupSign(context.Background(), 555))
	assertAction(t, m, "send_group_sign", `{"group_id":555}`)

	ignored, err := s.GetGroupIgnoredNotifies(context.Background())
	require.NoError(t, err)
	require.Len(t, ignored, 1)
	assert.Equal(t, "f1", ignored[0].Flag)
	assertAction(t, m, "get_group_ignored_notifies", "{}")
}

func boolPtr(b bool) *bool { return &b }

func TestSender_NapCatMsg(t *testing.T) {
	s, m := newMockSender(t)
	m.data["voice_msg_to_text"] = `{"text":"语音转文字"}`
	m.data["get_emoji_likes"] = `[{"emoji_id":"21","users":[{"uin":1001,"nick_name":"A"}]}]`

	text, err := s.VoiceMsg2Text(context.Background(), 123)
	require.NoError(t, err)
	assert.Equal(t, "语音转文字", text.Text)
	assertAction(t, m, "voice_msg_to_text", `{"message_id":123}`)

	likes, err := s.GetEmojiLikes(context.Background(), GetEmojiLikesParams{GroupID: 555, MessageID: "42", EmojiID: "21"})
	require.NoError(t, err)
	require.Len(t, likes, 1)
	require.Len(t, likes[0].Users, 1)
	assertAction(t, m, "get_emoji_likes", `{"group_id":555,"message_id":"42","emoji_id":"21"}`)

	require.NoError(t, s.ClickInlineKeyboardButton(context.Background(), ClickInlineKeyboardButtonParams{GroupID: 555, BotAppID: "app1", ButtonID: "b1"}))
	assertAction(t, m, "click_inline_keyboard_button", `{"group_id":555,"bot_appid":"app1","button_id":"b1"}`)

	require.NoError(t, s.MarkMsgAsRead(context.Background(), 42))
	assertAction(t, m, "mark_msg_as_read", `{"message_id":42}`)
}

func TestSender_NapCatUser(t *testing.T) {
	s, m := newMockSender(t)
	m.data["get_unidirectional_friend_list"] = `[{"user_id":1001,"nickname":"A"}]`
	m.data["get_recent_contact"] = `[]`
	m.data["get_online_clients"] = `[]`
	m.data["translate_en2zh"] = `[{"word":"hello","translate":"你好"}]`
	m.data["check_url_safely"] = `{"level":1}`
	m.data["fetch_custom_face_detail"] = `[]`
	m.data["get_mini_app_ark"] = `[]`

	friends, err := s.GetUnidirectionalFriendList(context.Background())
	require.NoError(t, err)
	require.Len(t, friends, 1)
	assertAction(t, m, "get_unidirectional_friend_list", "{}")

	require.NoError(t, s.DeleteUnidirectionalFriend(context.Background(), 1001))
	assertAction(t, m, "delete_unidirectional_friend", `{"user_id":1001}`)

	_, err = s.GetRecentContact(context.Background())
	require.NoError(t, err)
	assertAction(t, m, "get_recent_contact", "{}")

	_, err = s.GetOnlineClients(context.Background())
	require.NoError(t, err)
	assertAction(t, m, "get_online_clients", "{}")

	require.NoError(t, s.SetDiyOnlineStatus(context.Background(), 11, 1, "冲浪"))
	assertAction(t, m, "set_diy_online_status", `{"face_id":11,"face_type":1,"wording":"冲浪"}`)

	trans, err := s.TranslateEn2Zh(context.Background(), []string{"hello"})
	require.NoError(t, err)
	require.Len(t, trans, 1)
	assert.Equal(t, "你好", trans[0].Translate)
	assertAction(t, m, "translate_en2zh", `{"words":["hello"]}`)

	check, err := s.CheckUrlSafely(context.Background(), "https://ex.com")
	require.NoError(t, err)
	assert.Equal(t, 1, check.Level)
	assertAction(t, m, "check_url_safely", `{"url":"https://ex.com"}`)

	_, err = s.FetchCustomFaceDetail(context.Background(), 10)
	require.NoError(t, err)
	assertAction(t, m, "fetch_custom_face_detail", `{"count":10}`)

	require.NoError(t, s.AddCustomFace(context.Background(), AddCustomFaceParams{File: "/tmp/f.png"}))
	assertAction(t, m, "add_custom_face", `{"file":"/tmp/f.png"}`)

	require.NoError(t, s.DeleteCustomFace(context.Background(), DeleteCustomFaceParams{ResID: "r1"}))
	assertAction(t, m, "delete_custom_face", `{"res_id":"r1"}`)

	require.NoError(t, s.SetCustomFaceDesc(context.Background(), SetCustomFaceDescParams{EmojiID: "1", ResID: "r1", MD5: "m", Desc: "新描述"}))
	assertAction(t, m, "set_custom_face_desc", `{"emoji_id":"1","res_id":"r1","md5":"m","desc":"新描述"}`)

	_, err = s.GetMiniAppArk(context.Background())
	require.NoError(t, err)
	assertAction(t, m, "get_mini_app_ark", "{}")
	require.NoError(t, s.SendArkShare(context.Background(), map[string]any{"template_id": 1}))
	assertAction(t, m, "send_ark_share", `{"template_id":1}`)

	require.NoError(t, s.SendGroupArkShare(context.Background(), map[string]any{"group_id": 555}))
	assertAction(t, m, "send_group_ark_share", `{"group_id":555}`)
}

func TestSender_NapCatFlash(t *testing.T) {
	s, m := newMockSender(t)
	m.data["create_flash_task"] = `{"fileset_id":"fs1","share_link":"https://qfile.qq.com/q/code","expire_time":100}`
	m.data["get_fileset_id"] = `{"fileset_id":"fs1"}`
	m.data["get_fileset_info"] = `{"fileset_id":"fs1","file_count":1}`
	m.data["get_flash_file_list"] = `[{"file_name":"a.txt","file_size":10}]`
	m.data["get_flash_file_url"] = `{"url":"https://ex.com/a","expire":100}`
	m.data["get_share_link"] = `{"share_link":"https://qfile.qq.com/q/code"}`

	task, err := s.CreateFlashTask(context.Background(), []string{"/tmp/a.txt"}, "标题", "")
	require.NoError(t, err)
	assert.Equal(t, "fs1", task.FileSetID)
	assertAction(t, m, "create_flash_task", `{"files":["/tmp/a.txt"],"name":"标题"}`)

	require.NoError(t, s.DownloadFileset(context.Background(), "fs1"))
	assertAction(t, m, "download_fileset", `{"fileset_id":"fs1"}`)

	fid, err := s.GetFilesetId(context.Background(), "https://qfile.qq.com/q/code")
	require.NoError(t, err)
	assert.Equal(t, "fs1", fid.FileSetID)
	assertAction(t, m, "get_fileset_id", `{"share_link":"https://qfile.qq.com/q/code"}`)

	info, err := s.GetFilesetInfo(context.Background(), "fs1")
	require.NoError(t, err)
	assert.Equal(t, 1, info.FileCount)
	assertAction(t, m, "get_fileset_info", `{"fileset_id":"fs1"}`)

	files, err := s.GetFlashFileList(context.Background(), "fs1")
	require.NoError(t, err)
	require.Len(t, files, 1)
	assertAction(t, m, "get_flash_file_list", `{"fileset_id":"fs1"}`)

	url, err := s.GetFlashFileUrl(context.Background(), "fs1", "a.txt", 0)
	require.NoError(t, err)
	assert.Equal(t, "https://ex.com/a", url.URL)
	assertAction(t, m, "get_flash_file_url", `{"fileset_id":"fs1","file_name":"a.txt"}`)

	link, err := s.GetShareLink(context.Background(), "fs1")
	require.NoError(t, err)
	assert.Contains(t, link.ShareLink, "qfile.qq.com")
	assertAction(t, m, "get_share_link", `{"fileset_id":"fs1"}`)

	require.NoError(t, s.SendFlashMsg(context.Background(), "fs1", 1001, 0))
	assertAction(t, m, "send_flash_msg", `{"fileset_id":"fs1","user_id":1001}`)
}

// ────────────────────────────────────────────────────────────────────────────
// Lagrange.OneBot v1 补充动作
// ────────────────────────────────────────────────────────────────────────────

func TestSender_Lagrange(t *testing.T) {
	s, m := newMockSender(t)
	m.data["get_music_ark"] = `[]`
	m.data["fetch_mface_key"] = `[{"emoji_id":"1","key":"k1"}]`
	m.data["get_group_memo"] = `[{"content":"备忘录"}]`
	m.data["get_group_requests"] = `[]`
	m.data["upload_image"] = `{"url":"https://ex.com/u.png"}`

	_, err := s.GetMusicArk(context.Background(), map[string]any{"id": "123"})
	require.NoError(t, err)
	assertAction(t, m, "get_music_ark", `{"id":"123"}`)

	keys, err := s.FetchMFaceKey(context.Background(), []string{"1"})
	require.NoError(t, err)
	require.Len(t, keys, 1)
	assert.Equal(t, "k1", keys[0].Key)
	assertAction(t, m, "fetch_mface_key", `{"emoji_ids":["1"]}`)

	memos, err := s.GetGroupMemo(context.Background(), 555)
	require.NoError(t, err)
	require.Len(t, memos, 1)
	assertAction(t, m, "get_group_memo", `{"group_id":555}`)

	require.NoError(t, s.SetGroupMemo(context.Background(), 555, "新备忘录"))
	assertAction(t, m, "set_group_memo", `{"group_id":555,"content":"新备忘录"}`)

	require.NoError(t, s.DeleteGroupMemo(context.Background(), 555, "m1"))
	assertAction(t, m, "delete_group_memo", `{"group_id":555,"memo_id":"m1"}`)

	_, err = s.GetGroupRequests(context.Background(), map[string]any{})
	require.NoError(t, err)
	assertAction(t, m, "get_group_requests", "{}")

	require.NoError(t, s.SetGroupBotStatus(context.Background(), 555, true))
	assertAction(t, m, "set_group_bot_status", `{"group_id":555,"online":true}`)

	img, err := s.UploadImage(context.Background(), "/tmp/u.png")
	require.NoError(t, err)
	assert.Equal(t, "https://ex.com/u.png", img.URL)
	assertAction(t, m, "upload_image", `{"file":"/tmp/u.png"}`)

	// delete_group_folder 404 → 自动回退到 Lagrange 的 delete_group_file_folder。
	m.notFound = map[string]bool{"delete_group_folder": true}
	require.NoError(t, s.DeleteGroupFolder(context.Background(), 555, "d1"))
	assertAction(t, m, "delete_group_file_folder", `{"group_id":555,"folder_id":"d1"}`)

	require.NoError(t, s.SendPacket(context.Background(), map[string]any{"data": "x"}))
	assertAction(t, m, ".send_packet", `{"data":"x"}`)

	require.NoError(t, s.FriendJoinEmojiChain(context.Background(), map[string]any{"user_id": 1001}))
	assertAction(t, m, ".join_friend_emoji_chain", `{"user_id":1001}`)

	require.NoError(t, s.GroupJoinEmojiChain(context.Background(), map[string]any{"group_id": 555}))
	assertAction(t, m, ".join_group_emoji_chain", `{"group_id":555}`)
}

// TestSender_NapCatQunAlbum 验证 get_qun_album_list（NapCat 相册动作名）回退。
func TestSender_NapCatQunAlbum(t *testing.T) {
	s, m := newMockSender(t)
	m.data["get_qun_album_list"] = `[{"album_id":"a1","name":"相册"}]`
	m.notFound = map[string]bool{"get_group_album_list": true}

	albums, err := s.GetGroupAlbumList(context.Background(), 555, "c1")
	require.NoError(t, err)
	require.Len(t, albums, 1)
	assert.Equal(t, "a1", albums[0].AlbumID)
	assertAction(t, m, "get_qun_album_list", `{"group_id":555,"attach_info":"c1"}`)
}

// ────────────────────────────────────────────────────────────────────────────
// 标准接口（platform.GroupSettings / MessageHistoryProvider / AnnouncementManager）
// ────────────────────────────────────────────────────────────────────────────

func TestSender_StandardInterfaces(t *testing.T) {
	s, m := newMockSender(t)
	m.data["get_group_msg_history"] = `{"messages":[{"time":1,"message_id":2,"group_id":555,"user_id":3,"message":[{"type":"text","data":{"text":"hi"}}],"sender":{"user_id":3,"nickname":"A"}}]}`
	m.data["get_friend_msg_history"] = `{"messages":[{"time":2,"message_id":3,"user_id":1001,"message":[{"type":"text","data":{"text":"yo"}}]}]}`
	m.data["get_group_notice"] = `{"notices":[{"notice_id":"n1","content":"公告","user_id":1,"time":100}]}`

	// 编译期接口实现检查。
	var _ platform.GroupSettings = s
	var _ platform.MessageHistoryProvider = s
	var _ platform.AnnouncementManager = s

	// GroupSettings：已有方法签名直接匹配接口。
	require.NoError(t, s.SetGroupName(context.Background(), "555", "新名"))
	require.NoError(t, s.SetGroupCard(context.Background(), "555", "2", "名片"))
	require.NoError(t, s.SetGroupSpecialTitle(context.Background(), "555", "2", "头衔"))
	require.NoError(t, s.LeaveGroup(context.Background(), "555", false))

	// MessageHistoryProvider。
	history, err := s.GetGroupHistory(context.Background(), "555", 10)
	require.NoError(t, err)
	require.Len(t, history, 1)
	assert.Equal(t, "hi", history[0].Content)
	assert.Equal(t, "3", history[0].Sender.ID)
	assert.Equal(t, "2", history[0].ID)

	friendHistory, err := s.GetFriendHistory(context.Background(), "1001", 10)
	require.NoError(t, err)
	require.Len(t, friendHistory, 1)
	assert.Equal(t, "yo", friendHistory[0].Content)

	// AnnouncementManager。
	require.NoError(t, s.SendAnnouncement(context.Background(), "555", "内容", ""))
	assertAction(t, m, "send_group_notice", `{"group_id":555,"content":"内容"}`)

	anns, err := s.GetAnnouncements(context.Background(), "555")
	require.NoError(t, err)
	require.Len(t, anns, 1)
	assert.Equal(t, "n1", anns[0].ID)
	assert.Equal(t, "公告", anns[0].Content)
	assert.Equal(t, "1", anns[0].PublisherID)
}

// TestSender_FallbackChain 验证 404 自动回退链。
func TestSender_FallbackChain(t *testing.T) {
	s, m := newMockSender(t)
	m.data["set_group_sign"] = `{}`
	m.data["fetch_ptt_text"] = `{"text":"转写"}`
	m.data["set_group_kick_members"] = `{}`
	m.data["_send_group_notice"] = `{}`
	m.notFound = map[string]bool{
		"send_group_sign":           true,
		"voice_msg_to_text":         true,
		"batch_delete_group_member": true,
		"send_group_notice":         true,
	}

	require.NoError(t, s.SendGroupSign(context.Background(), 555))
	assertAction(t, m, "set_group_sign", `{"group_id":555}`)

	text, err := s.VoiceMsg2Text(context.Background(), 123)
	require.NoError(t, err)
	assert.Equal(t, "转写", text.Text)
	assertAction(t, m, "fetch_ptt_text", `{"message_id":123}`)

	require.NoError(t, s.BatchDeleteGroupMember(context.Background(), 555, []int64{1}, false))
	assertAction(t, m, "set_group_kick_members", `{"group_id":555,"user_id":[1]}`)
	require.NoError(t, s.SendGroupNotice(context.Background(), 555, "内容", ""))
	assertAction(t, m, "_send_group_notice", `{"group_id":555,"content":"内容"}`)
}

// TestFallback_NotFoundOnly 业务错误不应触发 fallback。
func TestFallback_NotFoundOnly(t *testing.T) {
	s, m := newMockSender(t)
	m.data["send_group_sign"] = `{}`
	// 主动作返回业务错误（非 404），不应尝试备选动作。
	m.notFound = map[string]bool{}
	_ = m
	// 业务错误通过 data 无法模拟，此处验证 isActionNotFound 判定。
	assert.False(t, isActionNotFound(fmt.Errorf("onebot api: action failed (status=failed, retcode=-403)")))
	assert.False(t, isActionNotFound(nil))
	assert.True(t, isActionNotFound(fmt.Errorf("onebot api: action failed (status=failed, retcode=404)")))
	assert.True(t, isActionNotFound(fmt.Errorf("onebot api: action \"x\" not found")))
	_ = s
}
