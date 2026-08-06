package milky

// 本文档核验说明
//
// 本文件中的端点名、请求体字段与响应字段断言均对照 Milky 官方文档
// （v1.2.2，2026-07 核验）：https://milky.ntqqrev.org
//
//	- 系统 API：/api/system     （get_login_info、get_impl_info、get_user_profile、get_peer_pins 等）
//	- 消息 API：/api/message    （send_private_message、get_message、get_history_messages 等）
//	- 好友 API：/api/friend     （send_friend_nudge、get_friend_requests、accept_friend_request 等）
//	- 群聊 API：/api/group      （set_group_name、send_group_announcement、accept_group_request 等）
//	- 文件 API：/api/file       （upload_private_file、get_group_files、create_group_folder 等）
//
// 协议约定：POST http://{host}/api/{endpoint}，响应形如
// {"status":"ok","retcode":0,"data":{...}}；status 非 "ok" 或 retcode 非 0 视为失败。

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/errutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// ────────────────────────────────────────────────────────────────────────────
// 测试辅助：Milky 模拟服务器
// ────────────────────────────────────────────────────────────────────────────

// recordedReq 记录一次发往测试服务器的 HTTP 请求。
type recordedReq struct {
	method      string
	path        string
	auth        string
	contentType string
	body        string
}

// mockMilkyServer 按端点返回预设 JSON 响应，并记录收到的请求。
type mockMilkyServer struct {
	t    *testing.T
	reqs []recordedReq
	srv  *httptest.Server
}

func newMockMilkyServer(t *testing.T) *mockMilkyServer {
	t.Helper()
	m := &mockMilkyServer{t: t}
	m.srv = httptest.NewServer(http.HandlerFunc(m.handle))
	t.Cleanup(m.srv.Close)
	return m
}

func (m *mockMilkyServer) handle(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		m.t.Errorf("read body: %v", err)
	}
	m.reqs = append(m.reqs, recordedReq{
		method:      r.Method,
		path:        r.URL.Path,
		auth:        r.Header.Get("Authorization"),
		contentType: r.Header.Get("Content-Type"),
		body:        string(body),
	})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(m.responseFor(r.URL.Path)))
}

// responseFor 返回指定端点的预设响应；未匹配时返回通用的成功空数据响应。
func (m *mockMilkyServer) responseFor(path string) string {
	if resp, found := m.responses()[path]; found {
		return resp
	}
	return `{"status":"ok","retcode":0,"data":{}}`
}

// responses 为测试服务器可识别的端点响应表。
func (m *mockMilkyServer) responses() map[string]string {
	const ok = `{"status":"ok","retcode":0,"data":%s}`
	return map[string]string{
		"/api/get_impl_info":    sprintf(ok, `{"impl_name":"Milky","impl_version":"2.0.0","qq_protocol_version":"11","qq_protocol_type":"Android","milky_version":"3.1.0"}`),
		"/api/get_login_info":   sprintf(ok, `{"uin":10001,"nickname":"RemiliaBot"}`),
		"/api/get_user_profile": sprintf(ok, `{"nickname":"Alice","qid":"uid1","age":25,"sex":"female","remark":"好友","bio":"hi","level":3,"country":"CN","city":"Shanghai","school":"U"}`),
		"/api/get_friend_list": sprintf(ok, `{"friends":[
			{"user_id":1001,"nickname":"Alice","sex":"female","qid":"q1","remark":"","category":{"category_id":1,"category_name":"好友"}},
			{"user_id":1002,"nickname":"Bob","sex":"male","qid":"q2","remark":"BB","category":{"category_id":2,"category_name":"同学"}}]}`),
		"/api/get_friend_info": sprintf(ok, `{"friend":{"user_id":1001,"nickname":"Alice","sex":"female","qid":"q1","remark":"RA","category":{"category_id":1,"category_name":"好友"}}}`),
		"/api/get_group_list": sprintf(ok, `{"groups":[
			{"group_id":555,"group_name":"群A","member_count":100,"max_member_count":200,"remark":"","created_time":1000,"description":"d","question":"q","announcement":"a"},
			{"group_id":556,"group_name":"群B","member_count":10,"max_member_count":500,"remark":"r","created_time":2000}]}`),
		"/api/get_group_info": sprintf(ok, `{"group":{"group_id":555,"group_name":"群A","member_count":100,"max_member_count":200,"remark":"r","created_time":1000,"description":"d","question":"q","announcement":"a"}}`),
		"/api/get_group_member_list": sprintf(ok, `{"members":[
			{"user_id":1,"group_id":555,"nickname":"N1","card":"C1","sex":"male","title":"T1","level":1,"role":"owner","join_time":10,"last_sent_time":20},
			{"user_id":2,"group_id":555,"nickname":"N2","card":"","sex":"female","level":2,"role":"admin","join_time":30,"last_sent_time":40,"shut_up_end_time":999}]}`),
		"/api/get_group_member_info":    sprintf(ok, `{"member":{"user_id":2,"group_id":555,"nickname":"N2","card":"C2","sex":"female","level":2,"role":"member","join_time":30,"last_sent_time":40}}`),
		"/api/get_peer_pins":            sprintf(ok, `{"friends":[{"user_id":1001,"nickname":"Alice","sex":"female","qid":"q1","remark":"","category":{"category_id":1,"category_name":"好友"}}],"groups":[{"group_id":555,"group_name":"群A","member_count":100,"max_member_count":200}]}`),
		"/api/get_custom_face_url_list": sprintf(ok, `{"urls":["https://ex.com/1.png","https://ex.com/2.png"]}`),
		"/api/get_cookies":              sprintf(ok, `{"cookies":"session=abc"}`),
		"/api/get_csrf_token":           sprintf(ok, `{"csrf_token":"tok123"}`),
		"/api/get_message": sprintf(ok, `{"message":{"message_scene":"group","peer_id":555,"message_seq":123,"sender_id":1,"time":1700000000,"segments":[
			{"type":"text","data":{"text":"hello"}}]}}`),
		"/api/get_history_messages": sprintf(ok, `{"messages":[
			{"message_scene":"friend","peer_id":1001,"message_seq":1,"sender_id":1001,"time":1700000001,"segments":[{"type":"text","data":{"text":"a"}}]},
			{"message_scene":"friend","peer_id":1001,"message_seq":2,"sender_id":1001,"time":1700000002,"segments":[{"type":"face","data":{"face_id":"21"}}]}],
			"next_message_seq":3}`),
		"/api/get_resource_temp_url": sprintf(ok, `{"url":"https://ex.com/res/1"}`),
		"/api/get_forwarded_messages": sprintf(ok, `{"messages":[
			{"message_seq":1,"sender_name":"Alice","avatar_url":"https://ex.com/a.png","time":1700000001,"segments":[{"type":"text","data":{"text":"fwd"}}]}]}`),
		"/api/get_friend_requests": sprintf(ok, `{"requests":[
			{"time":100,"initiator_id":200,"initiator_uid":"uid200","target_user_id":300,"target_user_uid":"uid300","state":"pending","comment":"hi","via":"search","is_filtered":false}]}`),
		"/api/get_group_announcements": sprintf(ok, `{"announcements":[
			{"group_id":555,"announcement_id":"ann1","user_id":1,"time":100,"content":"公告1","image_url":"https://ex.com/img.png"},
			{"group_id":555,"announcement_id":"ann2","user_id":2,"time":200,"content":"公告2"}]}`),
		"/api/get_group_essence_messages": sprintf(ok, `{"messages":[
			{"group_id":555,"message_seq":1,"message_time":100,"sender_id":1,"sender_name":"A","operator_id":2,"operator_name":"B","operation_time":200,"segments":[{"type":"text","data":{"text":"精华"}}]}],
			"is_end":true}`),
		"/api/get_group_notifications": sprintf(ok, `{"notifications":[
			{"operator_id":5,"type":"join_request","state":"pending","comment":"c","group_id":555,"notification_seq":9,"initiator_id":7,"target_user_id":8,"is_filtered":false,"is_set":true}],
			"next_notification_seq":10}`),
		"/api/upload_private_file":           sprintf(ok, `{"file_id":"fid1"}`),
		"/api/upload_group_file":             sprintf(ok, `{"file_id":"fid2"}`),
		"/api/get_private_file_download_url": sprintf(ok, `{"download_url":"https://ex.com/dl1"}`),
		"/api/get_group_file_download_url":   sprintf(ok, `{"download_url":"https://ex.com/dl2"}`),
		"/api/get_group_files": sprintf(ok, `{"files":[
			{"group_id":555,"file_id":"f1","file_name":"a.txt","parent_folder_id":"","file_size":1024,"uploaded_time":100,"expire_time":200,"uploader_id":1,"downloaded_times":3}],
			"folders":[
			{"group_id":555,"folder_id":"d1","parent_folder_id":"","folder_name":"docs","created_time":100,"last_modified_time":200,"creator_id":1,"file_count":5}]}`),
		"/api/create_group_folder":  sprintf(ok, `{"folder_id":"d9"}`),
		"/api/send_private_message": sprintf(ok, `{"message_seq":123,"time":1700000000}`),
		"/api/send_group_message":   sprintf(ok, `{"message_seq":456,"time":1700000001}`),
	}
}

// sprintf 是 fmt.Sprintf 的简写（避免每个键写死完整响应）。
func sprintf(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}

// lastReq 返回最近一次记录的请求。
func (m *mockMilkyServer) lastReq() recordedReq {
	m.t.Helper()
	require.NotEmpty(m.t, m.reqs, "expected at least one request")
	return m.reqs[len(m.reqs)-1]
}

// newTestAdapter 创建指向模拟服务器的 Adapter。
func newTestAdapter(t *testing.T, m *mockMilkyServer) *Adapter {
	t.Helper()
	adapter, err := NewAdapter(Config{BaseURL: m.srv.URL, AccessToken: "tok-123"})
	require.NoError(t, err)
	return adapter
}

// ────────────────────────────────────────────────────────────────────────────
// 系统 API
// ────────────────────────────────────────────────────────────────────────────

func TestAdapter_GetImplInfo(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	info, err := a.GetImplInfo(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "Milky", info.ImplName)
	assert.Equal(t, "2.0.0", info.ImplVersion)
	assert.Equal(t, "11", info.QQProtocolVersion)
	assert.Equal(t, "Android", info.QQProtocolType)
	assert.Equal(t, "3.1.0", info.MilkyVersion)
	assertReq(t, m, "/api/get_impl_info", "{}")
}

func TestAdapter_GetLoginInfo(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	info, err := a.GetLoginInfo(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(10001), info.Uin)
	assert.Equal(t, "RemiliaBot", info.Nickname)
	assertReq(t, m, "/api/get_login_info", "{}")
}

func TestAdapter_GetUserProfile(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	profile, err := a.GetUserProfile(context.Background(), 1001)
	require.NoError(t, err)
	assert.Equal(t, "Alice", profile.Nickname)
	assert.Equal(t, "uid1", profile.QID)
	assert.Equal(t, 25, profile.Age)
	assert.Equal(t, "Shanghai", profile.City)
	assert.Equal(t, 3, profile.Level)
	assertReq(t, m, "/api/get_user_profile", `{"user_id":1001}`)
}

func TestAdapter_GetCustomFaceURLList(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	urls, err := a.GetCustomFaceURLList(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"https://ex.com/1.png", "https://ex.com/2.png"}, urls)
	assertReq(t, m, "/api/get_custom_face_url_list", "{}")
}

func TestAdapter_GetCookies(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	cookies, err := a.GetCookies(context.Background(), "qzone.qq.com")
	require.NoError(t, err)
	assert.Equal(t, "session=abc", cookies)
	assertReq(t, m, "/api/get_cookies", `{"domain":"qzone.qq.com"}`)
}

func TestAdapter_GetCSRFToken(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	tok, err := a.GetCSRFToken(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "tok123", tok)
	assertReq(t, m, "/api/get_csrf_token", "{}")
}

// ────────────────────────────────────────────────────────────────────────────
// 好友 API
// ────────────────────────────────────────────────────────────────────────────

func TestAdapter_GetFriendList(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	friends, err := a.GetFriendList(context.Background(), true)
	require.NoError(t, err)
	require.Len(t, friends, 2)
	assert.Equal(t, int64(1001), friends[0].UserID)
	assert.Equal(t, "Alice", friends[0].Nickname)
	assert.Equal(t, 1, friends[0].Category.CategoryID)
	assert.Equal(t, "BB", friends[1].Remark)
	assertReq(t, m, "/api/get_friend_list", `{"no_cache":true}`)
}

func TestAdapter_GetFriendInfo(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	friend, err := a.GetFriendInfo(context.Background(), 1001, false)
	require.NoError(t, err)
	assert.Equal(t, int64(1001), friend.UserID)
	assert.Equal(t, "RA", friend.Remark)
	assertReq(t, m, "/api/get_friend_info", `{"user_id":1001,"no_cache":false}`)
}

func TestAdapter_GetFriendRequests(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	reqs, err := a.GetFriendRequests(context.Background(), 20, true)
	require.NoError(t, err)
	require.Len(t, reqs, 1)
	assert.Equal(t, int64(200), reqs[0].InitiatorID)
	assert.Equal(t, "uid200", reqs[0].InitiatorUID)
	assert.Equal(t, "hi", reqs[0].Comment)
	assertReq(t, m, "/api/get_friend_requests", `{"limit":20,"is_filtered":true}`)
}

func TestAdapter_SendFriendNudge(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	require.NoError(t, a.SendFriendNudge(context.Background(), 1001, true))
	assertReq(t, m, "/api/send_friend_nudge", `{"user_id":1001,"is_self":true}`)
}

func TestAdapter_SendProfileLike(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	require.NoError(t, a.SendProfileLike(context.Background(), 1001, 10))
	assertReq(t, m, "/api/send_profile_like", `{"user_id":1001,"count":10}`)
}

func TestAdapter_DeleteFriend(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	require.NoError(t, a.DeleteFriend(context.Background(), 1001))
	assertReq(t, m, "/api/delete_friend", `{"user_id":1001}`)
}

func TestAdapter_AcceptFriendRequest(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	require.NoError(t, a.AcceptFriendRequest(context.Background(), "uid200", true))
	assertReq(t, m, "/api/accept_friend_request", `{"initiator_uid":"uid200","is_filtered":true}`)
}

func TestAdapter_RejectFriendRequest(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	require.NoError(t, a.RejectFriendRequest(context.Background(), "uid200", "no thanks", false))
	assertReq(t, m, "/api/reject_friend_request", `{"initiator_uid":"uid200","is_filtered":false,"reason":"no thanks"}`)
}

// ────────────────────────────────────────────────────────────────────────────
// 群聊 API
// ────────────────────────────────────────────────────────────────────────────

func TestAdapter_GetGroupList(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	groups, err := a.GetGroupList(context.Background(), true)
	require.NoError(t, err)
	require.Len(t, groups, 2)
	assert.Equal(t, int64(555), groups[0].GroupID)
	assert.Equal(t, "群A", groups[0].GroupName)
	assert.Equal(t, 100, groups[0].MemberCount)
	assert.Equal(t, "a", groups[0].Announcement)
	assertReq(t, m, "/api/get_group_list", `{"no_cache":true}`)
}

func TestAdapter_GetGroupInfo(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	group, err := a.GetGroupInfo(context.Background(), 555, false)
	require.NoError(t, err)
	assert.Equal(t, int64(555), group.GroupID)
	assert.Equal(t, "群A", group.GroupName)
	assert.Equal(t, "q", group.Question)
	assertReq(t, m, "/api/get_group_info", `{"group_id":555,"no_cache":false}`)
}

func TestAdapter_GetGroupMemberList(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	members, err := a.GetGroupMemberList(context.Background(), 555, true)
	require.NoError(t, err)
	require.Len(t, members, 2)
	assert.Equal(t, int64(1), members[0].UserID)
	assert.Equal(t, "C1", members[0].Card)
	assert.Equal(t, "owner", members[0].Role)
	require.NotNil(t, members[1].ShutUpEndTime)
	assert.Equal(t, int64(999), *members[1].ShutUpEndTime)
	assertReq(t, m, "/api/get_group_member_list", `{"group_id":555,"no_cache":true}`)
}

func TestAdapter_GetGroupMemberInfo(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	member, err := a.GetGroupMemberInfo(context.Background(), 555, 2, false)
	require.NoError(t, err)
	assert.Equal(t, int64(2), member.UserID)
	assert.Equal(t, "C2", member.Card)
	assertReq(t, m, "/api/get_group_member_info", `{"group_id":555,"user_id":2,"no_cache":false}`)
}

func TestAdapter_GetPeerPins(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	friends, groups, err := a.GetPeerPins(context.Background())
	require.NoError(t, err)
	require.Len(t, friends, 1)
	require.Len(t, groups, 1)
	assert.Equal(t, int64(1001), friends[0].UserID)
	assert.Equal(t, int64(555), groups[0].GroupID)
	assertReq(t, m, "/api/get_peer_pins", "{}")
}

func TestAdapter_SetPeerPin(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	require.NoError(t, a.SetPeerPin(context.Background(), "group", 555, true))
	assertReq(t, m, "/api/set_peer_pin", `{"message_scene":"group","peer_id":555,"is_pinned":true}`)
}

func TestAdapter_SetAvatar(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	require.NoError(t, a.SetAvatar(context.Background(), "base64://aGk="))
	assertReq(t, m, "/api/set_avatar", `{"uri":"base64://aGk="}`)
}

func TestAdapter_SetNickname(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	require.NoError(t, a.SetNickname(context.Background(), "新昵称"))
	assertReq(t, m, "/api/set_nickname", `{"new_nickname":"新昵称"}`)
}

func TestAdapter_SetBio(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	require.NoError(t, a.SetBio(context.Background(), "个性签名"))
	assertReq(t, m, "/api/set_bio", `{"new_bio":"个性签名"}`)
}

func TestAdapter_SetGroupName(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	require.NoError(t, a.SetGroupName(context.Background(), 555, "新群名"))
	assertReq(t, m, "/api/set_group_name", `{"group_id":555,"new_group_name":"新群名"}`)
}

func TestAdapter_SetGroupAvatar(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	require.NoError(t, a.SetGroupAvatar(context.Background(), 555, "https://ex.com/img.png"))
	assertReq(t, m, "/api/set_group_avatar", `{"group_id":555,"image_uri":"https://ex.com/img.png"}`)
}

func TestAdapter_SetGroupMemberCard(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	require.NoError(t, a.SetGroupMemberCard(context.Background(), 555, 2, "名片"))
	assertReq(t, m, "/api/set_group_member_card", `{"group_id":555,"user_id":2,"card":"名片"}`)
}

func TestAdapter_SetGroupMemberSpecialTitle(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	require.NoError(t, a.SetGroupMemberSpecialTitle(context.Background(), 555, 2, "头衔"))
	assertReq(t, m, "/api/set_group_member_special_title", `{"group_id":555,"user_id":2,"special_title":"头衔"}`)
}

func TestAdapter_GetGroupAnnouncements(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	anns, err := a.GetGroupAnnouncements(context.Background(), 555)
	require.NoError(t, err)
	require.Len(t, anns, 2)
	assert.Equal(t, "ann1", anns[0].AnnouncementID)
	assert.Equal(t, "公告1", anns[0].Content)
	require.NotNil(t, anns[0].ImageURL)
	assert.Equal(t, "https://ex.com/img.png", *anns[0].ImageURL)
	assert.Nil(t, anns[1].ImageURL)
	assertReq(t, m, "/api/get_group_announcements", `{"group_id":555}`)
}

func TestAdapter_SendGroupAnnouncement_WithImage(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	require.NoError(t, a.SendGroupAnnouncement(context.Background(), 555, "公告内容", "https://ex.com/img.png"))
	req := m.lastReq()
	assertReq(t, m, "/api/send_group_announcement", "")
	body := gjson.Parse(req.body)
	assert.Equal(t, int64(555), body.Get("group_id").Int())
	assert.Equal(t, "公告内容", body.Get("content").String())
	assert.Equal(t, "https://ex.com/img.png", body.Get("image_uri").String())
}

func TestAdapter_SendGroupAnnouncement_WithoutImage(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	require.NoError(t, a.SendGroupAnnouncement(context.Background(), 555, "纯文字", ""))
	body := gjson.Parse(m.lastReq().body)
	assert.Equal(t, "纯文字", body.Get("content").String())
	assert.False(t, body.Get("image_uri").Exists(), "image_uri 应为空时省略")
}

func TestAdapter_DeleteGroupAnnouncement(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	require.NoError(t, a.DeleteGroupAnnouncement(context.Background(), 555, "ann1"))
	assertReq(t, m, "/api/delete_group_announcement", `{"group_id":555,"announcement_id":"ann1"}`)
}

func TestAdapter_GetGroupEssenceMessages(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	msgs, isEnd, err := a.GetGroupEssenceMessages(context.Background(), 555, 0, 20)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.True(t, isEnd)
	assert.Equal(t, int64(1), msgs[0].MessageSeq)
	assert.Equal(t, "精华", msgs[0].Segments[0].Data.Text)
	assertReq(t, m, "/api/get_group_essence_messages", `{"group_id":555,"page_index":0,"page_size":20}`)
}

func TestAdapter_SetGroupEssenceMessage(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	require.NoError(t, a.SetGroupEssenceMessage(context.Background(), 555, 123, true))
	assertReq(t, m, "/api/set_group_essence_message", `{"group_id":555,"message_seq":123,"is_set":true}`)
}

func TestAdapter_QuitGroup(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	require.NoError(t, a.QuitGroup(context.Background(), 555))
	assertReq(t, m, "/api/quit_group", `{"group_id":555}`)
}

func TestAdapter_SendGroupNudge(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	require.NoError(t, a.SendGroupNudge(context.Background(), 555, 2))
	assertReq(t, m, "/api/send_group_nudge", `{"group_id":555,"user_id":2}`)
}

func TestAdapter_GetGroupNotifications(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	seq := int64(9)
	notifs, next, err := a.GetGroupNotifications(context.Background(), &seq, true, 10)
	require.NoError(t, err)
	require.Len(t, notifs, 1)
	assert.Equal(t, "join_request", notifs[0].Type)
	require.NotNil(t, next)
	assert.Equal(t, int64(10), *next)
	body := gjson.Parse(m.lastReq().body)
	assert.Equal(t, int64(9), body.Get("start_notification_seq").Int())
	assert.True(t, body.Get("is_filtered").Bool())
	assert.Equal(t, int64(10), body.Get("limit").Int())
}

func TestAdapter_GetGroupNotifications_NoStartSeq(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	_, _, err := a.GetGroupNotifications(context.Background(), nil, false, 0)
	require.NoError(t, err)
	body := gjson.Parse(m.lastReq().body)
	assert.False(t, body.Get("start_notification_seq").Exists())
}

func TestAdapter_AcceptGroupRequest(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	require.NoError(t, a.AcceptGroupRequest(context.Background(), 555, 9, "join_request", false))
	assertReq(t, m, "/api/accept_group_request", `{"notification_seq":9,"notification_type":"join_request","group_id":555,"is_filtered":false}`)
}

func TestAdapter_RejectGroupRequest(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	require.NoError(t, a.RejectGroupRequest(context.Background(), 555, 9, "join_request", false, "满了"))
	assertReq(t, m, "/api/reject_group_request", `{"notification_seq":9,"notification_type":"join_request","group_id":555,"is_filtered":false,"reason":"满了"}`)
}

func TestAdapter_AcceptGroupInvitation(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	require.NoError(t, a.AcceptGroupInvitation(context.Background(), 555, 42))
	assertReq(t, m, "/api/accept_group_invitation", `{"group_id":555,"invitation_seq":42}`)
}

func TestAdapter_RejectGroupInvitation(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	require.NoError(t, a.RejectGroupInvitation(context.Background(), 555, 42))
	assertReq(t, m, "/api/reject_group_invitation", `{"group_id":555,"invitation_seq":42}`)
}

// ────────────────────────────────────────────────────────────────────────────
// 消息 API
// ────────────────────────────────────────────────────────────────────────────

func TestAdapter_GetMessage(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	msg, err := a.GetMessage(context.Background(), "group", 555, 123)
	require.NoError(t, err)
	assert.Equal(t, "group", msg.Scene)
	assert.Equal(t, int64(555), msg.PeerID)
	assert.Equal(t, "hello", msg.Segments[0].Data.Text)
	assertReq(t, m, "/api/get_message", `{"message_scene":"group","peer_id":555,"message_seq":123}`)
}

func TestAdapter_GetHistoryMessages(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	start := int64(1)
	msgs, next, err := a.GetHistoryMessages(context.Background(), "friend", 1001, &start, 10)
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	assert.Equal(t, int64(1), msgs[0].MessageSeq)
	assert.Equal(t, "a", msgs[0].Segments[0].Data.Text)
	assert.Equal(t, "21", msgs[1].Segments[0].Data.FaceID)
	require.NotNil(t, next)
	assert.Equal(t, int64(3), *next)
	body := gjson.Parse(m.lastReq().body)
	assert.Equal(t, int64(1), body.Get("start_message_seq").Int())
	assert.Equal(t, int64(10), body.Get("limit").Int())
}

func TestAdapter_GetHistoryMessages_NoStartSeq(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	_, _, err := a.GetHistoryMessages(context.Background(), "friend", 1001, nil, 0)
	require.NoError(t, err)
	body := gjson.Parse(m.lastReq().body)
	assert.False(t, body.Get("start_message_seq").Exists())
	assert.False(t, body.Get("limit").Exists())
}

func TestAdapter_GetResourceTempURL(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	u, err := a.GetResourceTempURL(context.Background(), "res1")
	require.NoError(t, err)
	assert.Equal(t, "https://ex.com/res/1", u)
	assertReq(t, m, "/api/get_resource_temp_url", `{"resource_id":"res1"}`)
}

func TestAdapter_GetForwardedMessages(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	msgs, err := a.GetForwardedMessages(context.Background(), "fwd1")
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, "Alice", msgs[0].SenderName)
	assert.Equal(t, "fwd", msgs[0].Segments[0].Data.Text)
	assertReq(t, m, "/api/get_forwarded_messages", `{"forward_id":"fwd1"}`)
}

func TestAdapter_MarkMessageAsRead(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	require.NoError(t, a.MarkMessageAsRead(context.Background(), "group", 555, 123))
	assertReq(t, m, "/api/mark_message_as_read", `{"message_scene":"group","peer_id":555,"message_seq":123}`)
}

func TestAdapter_SendPrivateMessage(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	seq, sentAt, err := a.SendPrivateMessage(context.Background(), 1001, []OutgoingSegment{&TextSegment{Text: "hi"}})
	require.NoError(t, err)
	assert.Equal(t, int64(123), seq)
	assert.Equal(t, time.Unix(1700000000, 0), sentAt)
	req := m.lastReq()
	assertReq(t, m, "/api/send_private_message", "")
	body := gjson.Parse(req.body)
	assert.Equal(t, int64(1001), body.Get("user_id").Int())
	assert.Equal(t, "hi", body.Get("message.0.data.text").String())
	assert.Equal(t, "text", body.Get("message.0.type").String())
}

func TestAdapter_SendPrivateMessage_EmptySegments(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	_, _, err := a.SendPrivateMessage(context.Background(), 1001, nil)
	assert.ErrorIs(t, err, errutil.ErrEmptyMessage)
	assert.Empty(t, m.reqs, "不应发出 HTTP 请求")
}

func TestAdapter_SendGroupMessage(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	seq, sentAt, err := a.SendGroupMessage(context.Background(), 555, []OutgoingSegment{&FaceSegment{FaceID: "21"}})
	require.NoError(t, err)
	assert.Equal(t, int64(456), seq)
	assert.Equal(t, time.Unix(1700000001, 0), sentAt)
	body := gjson.Parse(m.lastReq().body)
	assert.Equal(t, int64(555), body.Get("group_id").Int())
	assert.Equal(t, "face", body.Get("message.0.type").String())
	assert.Equal(t, "21", body.Get("message.0.data.face_id").String())
}

func TestAdapter_RecallPrivateMessage(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	require.NoError(t, a.RecallPrivateMessage(context.Background(), 1001, 123))
	assertReq(t, m, "/api/recall_private_message", `{"user_id":1001,"message_seq":123}`)
}

func TestAdapter_RecallGroupMessage(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	require.NoError(t, a.RecallGroupMessage(context.Background(), 555, 456))
	assertReq(t, m, "/api/recall_group_message", `{"group_id":555,"message_seq":456}`)
}

// ────────────────────────────────────────────────────────────────────────────
// 文件 API
// ────────────────────────────────────────────────────────────────────────────

func TestAdapter_UploadPrivateFile(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	fileID, err := a.UploadPrivateFile(context.Background(), 1001, "https://ex.com/a.pdf", "a.pdf")
	require.NoError(t, err)
	assert.Equal(t, "fid1", fileID)
	assertReq(t, m, "/api/upload_private_file", `{"user_id":1001,"file_uri":"https://ex.com/a.pdf","file_name":"a.pdf"}`)
}

func TestAdapter_UploadGroupFile(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	fileID, err := a.UploadGroupFile(context.Background(), 555, "folder1", "https://ex.com/b.pdf", "b.pdf")
	require.NoError(t, err)
	assert.Equal(t, "fid2", fileID)
	assertReq(t, m, "/api/upload_group_file", `{"group_id":555,"parent_folder_id":"folder1","file_uri":"https://ex.com/b.pdf","file_name":"b.pdf"}`)
}

func TestAdapter_GetPrivateFileDownloadURL(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	u, err := a.GetPrivateFileDownloadURL(context.Background(), 1001, "f1", "hash1")
	require.NoError(t, err)
	assert.Equal(t, "https://ex.com/dl1", u)
	assertReq(t, m, "/api/get_private_file_download_url", `{"user_id":1001,"file_id":"f1","file_hash":"hash1"}`)
}

func TestAdapter_GetGroupFileDownloadURL(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	u, err := a.GetGroupFileDownloadURL(context.Background(), 555, "f1")
	require.NoError(t, err)
	assert.Equal(t, "https://ex.com/dl2", u)
	assertReq(t, m, "/api/get_group_file_download_url", `{"group_id":555,"file_id":"f1"}`)
}

func TestAdapter_GetGroupFiles(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	files, folders, err := a.GetGroupFiles(context.Background(), 555, "")
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Len(t, folders, 1)
	assert.Equal(t, "f1", files[0].FileID)
	assert.Equal(t, "a.txt", files[0].FileName)
	assert.Equal(t, int64(1024), files[0].FileSize)
	assert.Equal(t, 3, files[0].DownloadedTimes)
	require.NotNil(t, files[0].ExpireTime)
	assert.Equal(t, int64(200), *files[0].ExpireTime)
	assert.Equal(t, "docs", folders[0].FolderName)
	assert.Equal(t, 5, folders[0].FileCount)
	assertReq(t, m, "/api/get_group_files", `{"group_id":555,"parent_folder_id":""}`)
}

func TestAdapter_MoveGroupFile(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	require.NoError(t, a.MoveGroupFile(context.Background(), 555, "f1", "p1", "t1"))
	assertReq(t, m, "/api/move_group_file", `{"group_id":555,"file_id":"f1","parent_folder_id":"p1","target_folder_id":"t1"}`)
}

func TestAdapter_RenameGroupFile(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	require.NoError(t, a.RenameGroupFile(context.Background(), 555, "f1", "p1", "新名.txt"))
	assertReq(t, m, "/api/rename_group_file", `{"group_id":555,"file_id":"f1","parent_folder_id":"p1","new_file_name":"新名.txt"}`)
}

func TestAdapter_DeleteGroupFile(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	require.NoError(t, a.DeleteGroupFile(context.Background(), 555, "f1"))
	assertReq(t, m, "/api/delete_group_file", `{"group_id":555,"file_id":"f1"}`)
}

func TestAdapter_CreateGroupFolder(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	folderID, err := a.CreateGroupFolder(context.Background(), 555, "新文件夹")
	require.NoError(t, err)
	assert.Equal(t, "d9", folderID)
	assertReq(t, m, "/api/create_group_folder", `{"group_id":555,"folder_name":"新文件夹"}`)
}

func TestAdapter_RenameGroupFolder(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	require.NoError(t, a.RenameGroupFolder(context.Background(), 555, "d1", "改名"))
	assertReq(t, m, "/api/rename_group_folder", `{"group_id":555,"folder_id":"d1","new_folder_name":"改名"}`)
}

func TestAdapter_DeleteGroupFolder(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	require.NoError(t, a.DeleteGroupFolder(context.Background(), 555, "d1"))
	assertReq(t, m, "/api/delete_group_folder", `{"group_id":555,"folder_id":"d1"}`)
}

// ────────────────────────────────────────────────────────────────────────────
// 群成员管理 API
// ────────────────────────────────────────────────────────────────────────────

func TestAdapter_KickGroupMember(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	require.NoError(t, a.KickGroupMember(context.Background(), 555, 2, true))
	assertReq(t, m, "/api/kick_group_member", `{"group_id":555,"user_id":2,"reject_add_request":true}`)
}

func TestAdapter_SetGroupMemberMute(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	require.NoError(t, a.SetGroupMemberMute(context.Background(), 555, 2, 600))
	assertReq(t, m, "/api/set_group_member_mute", `{"group_id":555,"user_id":2,"duration":600}`)
}

func TestAdapter_SetGroupWholeMute(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	require.NoError(t, a.SetGroupWholeMute(context.Background(), 555, true))
	assertReq(t, m, "/api/set_group_whole_mute", `{"group_id":555,"is_mute":true}`)
}

func TestAdapter_SetGroupMemberAdmin(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	require.NoError(t, a.SetGroupMemberAdmin(context.Background(), 555, 2, true))
	assertReq(t, m, "/api/set_group_member_admin", `{"group_id":555,"user_id":2,"is_set":true}`)
}

func TestAdapter_SendGroupMessageReaction(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	require.NoError(t, a.SendGroupMessageReaction(context.Background(), 555, 123, "21", "face", true))
	assertReq(t, m, "/api/send_group_message_reaction", `{"group_id":555,"message_seq":123,"reaction":"21","reaction_type":"face","is_add":true}`)
}

// ────────────────────────────────────────────────────────────────────────────
// 错误路径
// ────────────────────────────────────────────────────────────────────────────

func TestMilkyClient_Unauthorized(t *testing.T) {
	m := newMockMilkyServer(t)
	m.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	a := newTestAdapter(t, m)

	_, err := a.GetLoginInfo(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unauthorized")
}

func TestMilkyClient_EndpointNotFound(t *testing.T) {
	m := newMockMilkyServer(t)
	m.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	a := newTestAdapter(t, m)

	_, err := a.GetLoginInfo(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "endpoint not found")
}

func TestMilkyClient_UnsupportedMediaType(t *testing.T) {
	m := newMockMilkyServer(t)
	m.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnsupportedMediaType)
	})
	a := newTestAdapter(t, m)

	_, err := a.GetLoginInfo(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported media type")
}

func TestMilkyClient_ApiError(t *testing.T) {
	m := newMockMilkyServer(t)
	m.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"failed","retcode":-403,"message":"permission denied","data":null}`))
	})
	a := newTestAdapter(t, m)

	_, err := a.GetLoginInfo(context.Background())
	require.Error(t, err)
	apiErr, ok := err.(*apiError)
	require.True(t, ok)
	assert.Equal(t, "get_login_info", apiErr.Endpoint)
	assert.Equal(t, -403, apiErr.Retcode)
	assert.Equal(t, "permission denied", apiErr.Message)
}

func TestMilkyClient_InvalidJSONResponse(t *testing.T) {
	m := newMockMilkyServer(t)
	m.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not-json`))
	})
	a := newTestAdapter(t, m)

	_, err := a.GetLoginInfo(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse response")
}

func TestMilkyClient_NetworkError(t *testing.T) {
	// 指向一个未监听端口，模拟网络错误。
	a, err := NewAdapter(Config{BaseURL: "http://127.0.0.1:1", APITimeout: time.Second})
	require.NoError(t, err)

	_, err = a.GetLoginInfo(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "call get_login_info")
}

func TestMilkyClient_AuthorizationHeader(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	_, err := a.GetLoginInfo(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "Bearer tok-123", m.lastReq().auth)
}

// assertReq 断言最近一次请求的路径、方法、Content-Type，并验证 body 与期望 JSON 语义等价。
func assertReq(t *testing.T, m *mockMilkyServer, path, wantBody string) {
	t.Helper()
	req := m.lastReq()
	assert.Equal(t, path, req.path)
	assert.Equal(t, http.MethodPost, req.method)
	assert.Equal(t, "application/json", req.contentType)
	if wantBody != "" {
		assert.JSONEq(t, wantBody, req.body)
	}
}
