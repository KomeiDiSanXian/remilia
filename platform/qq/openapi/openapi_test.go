package openapi

// 本文档核验说明
//
// 本文件中的所有路径、查询参数、请求体字段与响应断言均对照腾讯官方文档：
//
//	https://bot.q.qq.com/wiki/develop/api-v2/
//
// 关键页面（2026-07 核验）：
//   - 单聊消息发送/流式/撤回：/wiki/develop/api-v2/autogen/api/v2_users_user_openid_messages.post.html
//   - 群聊/频道消息发送：/wiki/develop/api-v2/autogen/api/v2_groups_group_openid_messages.post.html、
//     /wiki/develop/api-v2/server-inter/channel/message/send.html
//   - 富媒体上传/分片上传：/wiki/develop/api-v2/autogen/api/v2_users_user_id_upload_prepare.post.html
//     （分片端点为 upload_prepare / upload_part_finish，注意是下划线而非斜杠）
//   - 互动事件回应：/wiki/develop/api-v2/server-inter/message/trans/msg-btn.html（PUT /interactions/{id}，body code=0）
//   - 表情表态：/wiki/develop/api-v2/server-inter/message/trans/emoji.html
//   - 频道成员/身份组/权限/禁言/公告/精华/日程/音频/论坛：/wiki/develop/api-v2/server-inter/channel/...
//   - 私信会话：/wiki/develop/api-v2/server-inter/channel/message/dms.html（POST /users/@me/dms）
//   - 已加入频道列表：/wiki/develop/api-v2/autogen/api/users_me_guilds.get.html（before/after/limit）
//   - WSS 接入点：/wiki/develop/api-v2/openapi/wss/url_get.html、.../shard_url_get.html

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/httpclient"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/auth/token"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordedReq 记录一次发往模拟服务器的请求。
type recordedReq struct {
	method string
	path   string
	query  string
	auth   string
	appID  string
	body   string
}

// mockQQ 记录请求并按路径返回预设 JSON 响应。
//
// 注意：包级 httpclient 便捷函数使用全局默认客户端，因此本测试文件内的
// 用例必须串行执行（不使用 t.Parallel），每个用例独立创建 httptest.Server。
type mockQQ struct {
	t    *testing.T
	srv  *httptest.Server
	mu   sync.Mutex
	reqs []recordedReq
	resp map[string]string
	// status 按路径覆盖响应状态码（默认 200）。
	status map[string]int
}

func newMockQQ(t *testing.T) *mockQQ {
	t.Helper()
	return &mockQQ{t: t, resp: map[string]string{}, status: map[string]int{}}
}

func (m *mockQQ) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			m.t.Errorf("read body: %v", err)
		}
		m.mu.Lock()
		m.reqs = append(m.reqs, recordedReq{
			method: r.Method,
			path:   r.URL.Path,
			query:  r.URL.RawQuery,
			auth:   r.Header.Get("Authorization"),
			appID:  r.Header.Get("X-Callback-AppID"),
			body:   string(body),
		})
		resp, ok := m.resp[r.URL.Path]
		code := m.status[r.URL.Path]
		m.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/app/getAppAccessToken" {
			_, _ = w.Write([]byte(`{"access_token":"test-access-token","expires_in":7200}`))
			return
		}
		if code == 0 {
			code = http.StatusOK
		}
		if !ok {
			resp = `{"data":{"id":"d1"},"message":"ok"}`
		}
		w.WriteHeader(code)
		_, _ = w.Write([]byte(resp))
	})
}

// last 返回最近一次记录的请求。
func (m *mockQQ) last() recordedReq {
	m.t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	require.NotEmpty(m.t, m.reqs, "expected at least one request")
	return m.reqs[len(m.reqs)-1]
}

// newTestAPI 创建指向模拟服务器的 OpenAPI 客户端，
// 并通过模拟的 token 端点使 token.Manager 就绪。
func newTestAPI(t *testing.T, m *mockQQ) (*Client, *token.Manager) {
	t.Helper()
	m.srv = httptest.NewServer(m.handler())
	t.Cleanup(m.srv.Close)

	old := httpclient.SetDefaultHTTPClient(redirectClient(m.srv))
	t.Cleanup(func() { httpclient.SetDefaultHTTPClient(old) })

	mgr := token.NewManager(&dto.BotInfo{AppID: 102072748, AppSecret: "test-secret"})
	t.Cleanup(mgr.Stop)
	require.NoError(t, mgr.WaitReadyWithTimeout(5*time.Second))
	assert.Equal(t, "test-access-token", mgr.GetToken())
	return New(mgr), mgr
}

// redirectClient 返回一个将所有请求（无论原始 scheme/host）转发到
// 指定 httptest.Server 的 http.Client，用于在测试中拦截包级 httpclient
// 便捷函数发出的请求。
func redirectClient(srv *httptest.Server) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		r.URL.Scheme = "http"
		r.URL.Host = srv.Listener.Addr().String()
		return http.DefaultTransport.RoundTrip(r)
	})}
}

// roundTripFunc 将函数适配为 http.RoundTripper。
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// assertLast 断言最近一次请求的方法、路径、鉴权头，并验证 body 语义等价。
func assertLast(t *testing.T, m *mockQQ, method, path, wantBody string) {
	t.Helper()
	req := m.last()
	assert.Equal(t, method, req.method)
	assert.Equal(t, path, req.path)
	assert.Equal(t, "QQBot test-access-token", req.auth)
	if wantBody != "" {
		assert.JSONEq(t, wantBody, req.body)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// 消息发送
// ────────────────────────────────────────────────────────────────────────────

func TestClient_SingleChat(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.SingleChat(context.Background(), "openid_1", &dto.Message{Content: "hello", Type: dto.TextMessage})
	require.NoError(t, err)
	assertLast(t, m, http.MethodPost, "/v2/users/openid_1/messages", `{"content":"hello","msg_type":0}`)
}

func TestClient_GroupChat(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.GroupChat(context.Background(), "gid_1", &dto.Message{Content: "hi"})
	require.NoError(t, err)
	assertLast(t, m, http.MethodPost, "/v2/groups/gid_1/messages", `{"content":"hi","msg_type":0}`)
}

// TestClient_GroupChat_ErrorBody 验证：QQ 以 4xx + 错误码体拒绝消息时，
// GroupChat 必须返回错误（此前 DoJSON 不检查状态码，错误被当作成功吞掉，
// 表现为"handler 成功但消息未发出"）。
func TestClient_GroupChat_ErrorBody(t *testing.T) {
	m := newMockQQ(t)
	m.status["/v2/groups/gid_1/messages"] = http.StatusForbidden
	m.resp["/v2/groups/gid_1/messages"] = `{"code":40034105,"message":"主动消息发送失败，无权限"}`
	api, _ := newTestAPI(t, m)

	_, err := api.GroupChat(context.Background(), "gid_1", &dto.Message{Content: "hi"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "40034105")
	assert.Contains(t, err.Error(), "主动消息发送失败")
}

// TestClient_GroupChat_ErrorCodeIn2xx 验证：QQ 部分接口 HTTP 200 但响应体携带
// 非零错误码时同样必须报错（与 RespondInteraction 的 err_code 检查一致）。
func TestClient_GroupChat_ErrorCodeIn2xx(t *testing.T) {
	m := newMockQQ(t)
	m.resp["/v2/groups/gid_1/messages"] = `{"code":40034125,"message":"参数错误"}`
	api, _ := newTestAPI(t, m)

	_, err := api.GroupChat(context.Background(), "gid_1", &dto.Message{Content: "hi"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "40034125")
}

// TestClient_GroupChat_ErrCodeIn2xx 验证 err_code 字段（HTTP 200 时）同样被识别。
func TestClient_GroupChat_ErrCodeIn2xx(t *testing.T) {
	m := newMockQQ(t)
	m.resp["/v2/groups/gid_1/messages"] = `{"err_code":40034128,"err_msg":"被动回复时间或次数超限"}`
	api, _ := newTestAPI(t, m)

	_, err := api.GroupChat(context.Background(), "gid_1", &dto.Message{Content: "hi"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "40034128")
}

func TestClient_ChannelChat(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.ChannelChat(context.Background(), "chan_1", &dto.GuildMessage{Content: "hi"})
	require.NoError(t, err)
	assertLast(t, m, http.MethodPost, "/channels/chan_1/messages", `{"content":"hi"}`)
}

func TestClient_DMChat(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.DMChat(context.Background(), "dm_1", &dto.GuildMessage{Content: "hi"})
	require.NoError(t, err)
	assertLast(t, m, http.MethodPost, "/dms/dm_1/messages", `{"content":"hi"}`)
}

func TestClient_SingleRichMedia(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.SingleRichMedia(context.Background(), "openid_1", &dto.Media{Type: dto.ImageFile, URL: "https://ex.com/a.png"})
	require.NoError(t, err)
	assertLast(t, m, http.MethodPost, "/v2/users/openid_1/files", `{"file_type":1,"url":"https://ex.com/a.png","srv_send_msg":false}`)
}

func TestClient_GroupRichMedia(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.GroupRichMedia(context.Background(), "gid_1", &dto.Media{Type: dto.VideoFile, URL: "https://ex.com/a.mp4"})
	require.NoError(t, err)
	assertLast(t, m, http.MethodPost, "/v2/groups/gid_1/files", `{"file_type":2,"url":"https://ex.com/a.mp4","srv_send_msg":false}`)
}

func TestClient_SingleStreamChat(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.SingleStreamChat(context.Background(), "openid_1", &dto.StreamMessage{ContentRaw: "part1"})
	require.NoError(t, err)
	assertLast(t, m, http.MethodPost, "/v2/users/openid_1/stream_messages", `{"input_state":0,"content_raw":"part1"}`)
}

func TestClient_UserUploadPrepare(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.UserUploadPrepare(context.Background(), "openid_1", &dto.UploadPrepareRequest{FileType: dto.ImageFile, FileSize: 1024, FileName: "a.png"})
	require.NoError(t, err)
	assertLast(t, m, http.MethodPost, "/v2/users/openid_1/upload_prepare", `{"file_type":1,"file_size":1024,"file_name":"a.png"}`)
}

func TestClient_GroupUploadPrepare(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.GroupUploadPrepare(context.Background(), "gid_1", &dto.UploadPrepareRequest{FileType: dto.ImageFile, FileSize: 2048})
	require.NoError(t, err)
	assertLast(t, m, http.MethodPost, "/v2/groups/gid_1/upload_prepare", `{"file_type":1,"file_size":2048}`)
}

func TestClient_UserUploadPartFinish(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.UserUploadPartFinish(context.Background(), "openid_1", &dto.UploadPartFinishRequest{UploadID: "u1", PartIndex: 3})
	require.NoError(t, err)
	assertLast(t, m, http.MethodPost, "/v2/users/openid_1/upload_part_finish", `{"upload_id":"u1","part_index":3}`)
}

func TestClient_GroupUploadPartFinish(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.GroupUploadPartFinish(context.Background(), "gid_1", &dto.UploadPartFinishRequest{UploadID: "u2", PartIndex: 0})
	require.NoError(t, err)
	assertLast(t, m, http.MethodPost, "/v2/groups/gid_1/upload_part_finish", `{"upload_id":"u2","part_index":0}`)
}

// ────────────────────────────────────────────────────────────────────────────
// 消息撤回
// ────────────────────────────────────────────────────────────────────────────

func TestClient_SingleReset(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.SingleReset(context.Background(), "openid_1", "msg_1")
	require.NoError(t, err)
	assertLast(t, m, http.MethodDelete, "/v2/users/openid_1/messages/msg_1", "")
}

func TestClient_GroupReset(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.GroupReset(context.Background(), "gid_1", "msg_1")
	require.NoError(t, err)
	assertLast(t, m, http.MethodDelete, "/v2/groups/gid_1/messages/msg_1", "")
}

func TestClient_ChannelReset_HidetipQuery(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.ChannelReset(context.Background(), "chan_1", "msg_1", true)
	require.NoError(t, err)
	req := m.last()
	assert.Equal(t, http.MethodDelete, req.method)
	assert.Equal(t, "/channels/chan_1/messages/msg_1", req.path)
	assert.Equal(t, "hidetip=true", req.query)
}

func TestClient_DMReset(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.DMReset(context.Background(), "dm_1", "msg_1", false)
	require.NoError(t, err)
	req := m.last()
	assert.Equal(t, "/dms/dm_1/messages/msg_1", req.path)
	assert.Equal(t, "hidetip=false", req.query)
}

func TestClient_Delete_NoContent(t *testing.T) {
	m := newMockQQ(t)
	m.status["/v2/users/openid_1/messages/msg_1"] = http.StatusNoContent
	api, _ := newTestAPI(t, m)

	result, err := api.SingleReset(context.Background(), "openid_1", "msg_1")
	require.NoError(t, err)
	assert.False(t, result.Exists())
}

func TestClient_Delete_Non2xx(t *testing.T) {
	m := newMockQQ(t)
	m.status["/v2/users/openid_1/messages/msg_1"] = http.StatusNotFound
	api, _ := newTestAPI(t, m)

	_, err := api.SingleReset(context.Background(), "openid_1", "msg_1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 404")
}

// ────────────────────────────────────────────────────────────────────────────
// 互动事件与表情表态
// ────────────────────────────────────────────────────────────────────────────

func TestClient_RespondInteraction(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.RespondInteraction(context.Background(), "inter_1", 0)
	require.NoError(t, err)
	req := m.last()
	assert.Equal(t, http.MethodPut, req.method)
	assert.Equal(t, "/interactions/inter_1", req.path)
	assert.Equal(t, "102072748", req.appID)
	assert.JSONEq(t, `{"code":0}`, req.body)
}

func TestClient_RespondInteraction_Non2xx(t *testing.T) {
	m := newMockQQ(t)
	m.status["/interactions/inter_1"] = http.StatusInternalServerError
	api, _ := newTestAPI(t, m)

	_, err := api.RespondInteraction(context.Background(), "inter_1", 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 500")
}

func TestClient_RespondInteraction_ErrCode(t *testing.T) {
	m := newMockQQ(t)
	m.resp["/interactions/inter_1"] = `{"err_code":1,"message":"操作失败"}`
	api, _ := newTestAPI(t, m)

	_, err := api.RespondInteraction(context.Background(), "inter_1", 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "err_code=1")
}

func TestClient_AddReaction(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.AddReaction(context.Background(), "chan_1", "msg_1", 1, "21")
	require.NoError(t, err)
	assertLast(t, m, http.MethodPut, "/channels/chan_1/messages/msg_1/reactions/1/21", "")
}

func TestClient_DeleteReaction(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.DeleteReaction(context.Background(), "chan_1", "msg_1", 2, "👍")
	require.NoError(t, err)
	assertLast(t, m, http.MethodDelete, "/channels/chan_1/messages/msg_1/reactions/2/👍", "")
}

func TestClient_GetReactionUsers(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.GetReactionUsers(context.Background(), "chan_1", "msg_1", 1, "21", "cursor1", 20)
	require.NoError(t, err)
	req := m.last()
	assert.Equal(t, http.MethodGet, req.method)
	assert.Equal(t, "/channels/chan_1/messages/msg_1/reactions/1/21", req.path)
	assert.Contains(t, req.query, "cookie=cursor1")
	assert.Contains(t, req.query, "limit=20")
}

// ────────────────────────────────────────────────────────────────────────────
// 频道管理
// ────────────────────────────────────────────────────────────────────────────

func TestClient_GetMe(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.GetMe(context.Background())
	require.NoError(t, err)
	assertLast(t, m, http.MethodGet, "/users/@me", "")
}

func TestClient_GetMyGuilds_NoParams(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.GetMyGuilds(context.Background(), "", "", 0)
	require.NoError(t, err)
	req := m.last()
	assert.Equal(t, "/users/@me/guilds", req.path)
	assert.Empty(t, req.query)
}

func TestClient_GetMyGuilds_WithParams(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.GetMyGuilds(context.Background(), "before_1", "after_2", 50)
	require.NoError(t, err)
	req := m.last()
	assert.Equal(t, "/users/@me/guilds", req.path)
	assert.Contains(t, req.query, "before=before_1")
	assert.Contains(t, req.query, "after=after_2")
	assert.Contains(t, req.query, "limit=50")
}

func TestClient_GetGuild(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.GetGuild(context.Background(), "guild_1")
	require.NoError(t, err)
	assertLast(t, m, http.MethodGet, "/guilds/guild_1", "")
}

func TestClient_GetGuildChannels(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.GetGuildChannels(context.Background(), "guild_1")
	require.NoError(t, err)
	assertLast(t, m, http.MethodGet, "/guilds/guild_1/channels", "")
}

func TestClient_GetChannel(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.GetChannel(context.Background(), "chan_1")
	require.NoError(t, err)
	assertLast(t, m, http.MethodGet, "/channels/chan_1", "")
}

func TestClient_CreateGuildChannel(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.CreateGuildChannel(context.Background(), "guild_1", &dto.ChannelRequest{Name: "新频道"})
	require.NoError(t, err)
	assertLast(t, m, http.MethodPost, "/guilds/guild_1/channels", `{"name":"新频道"}`)
}

func TestClient_UpdateGuildChannel(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.UpdateGuildChannel(context.Background(), "chan_1", &dto.ChannelRequest{Name: "改名"})
	require.NoError(t, err)
	assertLast(t, m, http.MethodPatch, "/channels/chan_1", `{"name":"改名"}`)
}

func TestClient_DeleteGuildChannel(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.DeleteGuildChannel(context.Background(), "chan_1")
	require.NoError(t, err)
	assertLast(t, m, http.MethodDelete, "/channels/chan_1", "")
}

func TestClient_CreateDirectMessageSession(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.CreateDirectMessageSession(context.Background(), &dto.DirectMessageSessionRequest{RecipientID: "user_1", SourceGuildID: "guild_1"})
	require.NoError(t, err)
	assertLast(t, m, http.MethodPost, "/users/@me/dms", `{"recipient_id":"user_1","source_guild_id":"guild_1"}`)
}

// ────────────────────────────────────────────────────────────────────────────
// 频道成员
// ────────────────────────────────────────────────────────────────────────────

func TestClient_GetChannelOnlineNums(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.GetChannelOnlineNums(context.Background(), "chan_1")
	require.NoError(t, err)
	assertLast(t, m, http.MethodGet, "/channels/chan_1/online_nums", "")
}

func TestClient_GetGuildMembers(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.GetGuildMembers(context.Background(), "guild_1", "0", 100)
	require.NoError(t, err)
	req := m.last()
	assert.Equal(t, "/guilds/guild_1/members", req.path)
	assert.Contains(t, req.query, "after=0")
	assert.Contains(t, req.query, "limit=100")
}

func TestClient_GetGuildRoleMembers(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.GetGuildRoleMembers(context.Background(), "guild_1", "role_1", 0, 50)
	require.NoError(t, err)
	req := m.last()
	assert.Equal(t, "/guilds/guild_1/roles/role_1/members", req.path)
	assert.Contains(t, req.query, "start_index=0")
	assert.Contains(t, req.query, "limit=50")
}

func TestClient_GetGuildMember(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.GetGuildMember(context.Background(), "guild_1", "user_1")
	require.NoError(t, err)
	assertLast(t, m, http.MethodGet, "/guilds/guild_1/members/user_1", "")
}

func TestClient_DeleteGuildMember(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.DeleteGuildMember(context.Background(), "guild_1", "user_1", true, 7)
	require.NoError(t, err)
	assertLast(t, m, http.MethodDelete, "/guilds/guild_1/members/user_1", `{"add_blacklist":true,"delete_history_msg_days":7}`)
}

// ────────────────────────────────────────────────────────────────────────────
// 身份组与权限
// ────────────────────────────────────────────────────────────────────────────

func TestClient_GetGuildRoles(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.GetGuildRoles(context.Background(), "guild_1")
	require.NoError(t, err)
	assertLast(t, m, http.MethodGet, "/guilds/guild_1/roles", "")
}

func TestClient_CreateGuildRole(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.CreateGuildRole(context.Background(), "guild_1", &dto.GuildRoleRequest{Name: "管理"})
	require.NoError(t, err)
	assertLast(t, m, http.MethodPost, "/guilds/guild_1/roles", `{"name":"管理"}`)
}

func TestClient_UpdateGuildRole(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.UpdateGuildRole(context.Background(), "guild_1", "role_1", &dto.GuildRoleRequest{Name: "改名"})
	require.NoError(t, err)
	assertLast(t, m, http.MethodPatch, "/guilds/guild_1/roles/role_1", `{"name":"改名"}`)
}

func TestClient_DeleteGuildRole(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.DeleteGuildRole(context.Background(), "guild_1", "role_1")
	require.NoError(t, err)
	assertLast(t, m, http.MethodDelete, "/guilds/guild_1/roles/role_1", "")
}

func TestClient_AddGuildMemberRole_NoChannel(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.AddGuildMemberRole(context.Background(), "guild_1", "user_1", "role_1", "")
	require.NoError(t, err)
	req := m.last()
	assert.Equal(t, http.MethodPut, req.method)
	assert.Equal(t, "/guilds/guild_1/members/user_1/roles/role_1", req.path)
	assert.Empty(t, req.body, "channelID 为空时不应携带 body")
}

func TestClient_AddGuildMemberRole_WithChannel(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.AddGuildMemberRole(context.Background(), "guild_1", "user_1", "role_1", "chan_1")
	require.NoError(t, err)
	req := m.last()
	assert.Equal(t, http.MethodPut, req.method)
	assert.JSONEq(t, `{"channel":{"id":"chan_1"}}`, req.body)
}

func TestClient_DeleteGuildMemberRole_NoChannel(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.DeleteGuildMemberRole(context.Background(), "guild_1", "user_1", "role_1", "")
	require.NoError(t, err)
	req := m.last()
	assert.Equal(t, http.MethodDelete, req.method)
	assert.Empty(t, req.body)
}

func TestClient_DeleteGuildMemberRole_WithChannel(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.DeleteGuildMemberRole(context.Background(), "guild_1", "user_1", "role_1", "chan_1")
	require.NoError(t, err)
	assert.JSONEq(t, `{"channel":{"id":"chan_1"}}`, m.last().body)
}

func TestClient_GetChannelMemberPermissions(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.GetChannelMemberPermissions(context.Background(), "chan_1", "user_1")
	require.NoError(t, err)
	assertLast(t, m, http.MethodGet, "/channels/chan_1/members/user_1/permissions", "")
}

func TestClient_UpdateChannelMemberPermissions(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.UpdateChannelMemberPermissions(context.Background(), "chan_1", "user_1", &dto.PermissionRequest{Add: "1"})
	require.NoError(t, err)
	assertLast(t, m, http.MethodPut, "/channels/chan_1/members/user_1/permissions", `{"add":"1"}`)
}

func TestClient_GetChannelRolePermissions(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.GetChannelRolePermissions(context.Background(), "chan_1", "role_1")
	require.NoError(t, err)
	assertLast(t, m, http.MethodGet, "/channels/chan_1/roles/role_1/permissions", "")
}

func TestClient_UpdateChannelRolePermissions(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.UpdateChannelRolePermissions(context.Background(), "chan_1", "role_1", &dto.PermissionRequest{Remove: "4"})
	require.NoError(t, err)
	assertLast(t, m, http.MethodPut, "/channels/chan_1/roles/role_1/permissions", `{"remove":"4"}`)
}

// ────────────────────────────────────────────────────────────────────────────
// 接口授权 / 发言管理
// ────────────────────────────────────────────────────────────────────────────

func TestClient_GetGuildAPIPermissions(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.GetGuildAPIPermissions(context.Background(), "guild_1")
	require.NoError(t, err)
	assertLast(t, m, http.MethodGet, "/guilds/guild_1/api_permission", "")
}

func TestClient_RequestGuildAPIPermission(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.RequestGuildAPIPermission(context.Background(), "guild_1", &dto.APIPermissionDemandRequest{ChannelID: "chan_1", APIInfo: &dto.APIPermissionDemandInfo{Path: "/v2/users/{openid}/messages", Method: "POST"}})
	require.NoError(t, err)
	req := m.last()
	assert.Equal(t, http.MethodPost, req.method)
	assert.Equal(t, "/guilds/guild_1/api_permission/demand", req.path)
	assert.Contains(t, req.body, `"path":"/v2/users/{openid}/messages"`)
	assert.Contains(t, req.body, `"method":"POST"`)
}

func TestClient_GetGuildMessageSetting(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.GetGuildMessageSetting(context.Background(), "guild_1")
	require.NoError(t, err)
	assertLast(t, m, http.MethodGet, "/guilds/guild_1/message/setting", "")
}

func TestClient_MuteGuild(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.MuteGuild(context.Background(), "guild_1", &dto.MuteRequest{MuteEndTimestamp: "0"})
	require.NoError(t, err)
	assertLast(t, m, http.MethodPatch, "/guilds/guild_1/mute", `{"mute_end_timestamp":"0"}`)
}

func TestClient_MuteGuildMember(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.MuteGuildMember(context.Background(), "guild_1", "user_1", &dto.MuteRequest{MuteSeconds: "600"})
	require.NoError(t, err)
	assertLast(t, m, http.MethodPatch, "/guilds/guild_1/members/user_1/mute", `{"mute_seconds":"600"}`)
}

func TestClient_MuteGuildMultiMembers(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.MuteGuildMultiMembers(context.Background(), "guild_1", &dto.MultipleMuteRequest{
		MuteRequest: dto.MuteRequest{MuteSeconds: "300"},
		UserIDs:     []string{"u1", "u2"},
	})
	require.NoError(t, err)
	assertLast(t, m, http.MethodPatch, "/guilds/guild_1/mute", `{"mute_seconds":"300","user_ids":["u1","u2"]}`)
}

// ────────────────────────────────────────────────────────────────────────────
// 公告 / 精华 / 日程 / 音频 / 论坛 / 网关
// ────────────────────────────────────────────────────────────────────────────

func TestClient_CreateGuildAnnounce(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.CreateGuildAnnounce(context.Background(), "guild_1", &dto.CreateGuildAnnounceRequest{MessageID: "msg_1"})
	require.NoError(t, err)
	assertLast(t, m, http.MethodPost, "/guilds/guild_1/announces", `{"message_id":"msg_1"}`)
}

func TestClient_DeleteGuildAnnounce(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.DeleteGuildAnnounce(context.Background(), "guild_1", "all")
	require.NoError(t, err)
	assertLast(t, m, http.MethodDelete, "/guilds/guild_1/announces/all", "")
}

func TestClient_PinMessage(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.PinMessage(context.Background(), "chan_1", "msg_1")
	require.NoError(t, err)
	assertLast(t, m, http.MethodPut, "/channels/chan_1/pins/msg_1", "")
}

func TestClient_UnpinMessage(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.UnpinMessage(context.Background(), "chan_1", "msg_1")
	require.NoError(t, err)
	assertLast(t, m, http.MethodDelete, "/channels/chan_1/pins/msg_1", "")
}

func TestClient_GetPinnedMessages(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.GetPinnedMessages(context.Background(), "chan_1")
	require.NoError(t, err)
	assertLast(t, m, http.MethodGet, "/channels/chan_1/pins", "")
}

func TestClient_GetChannelSchedules_WithSince(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.GetChannelSchedules(context.Background(), "chan_1", 123)
	require.NoError(t, err)
	req := m.last()
	assert.Equal(t, "/channels/chan_1/schedules", req.path)
	assert.Contains(t, req.query, "since=123")
}

func TestClient_GetChannelSchedules_NoSince(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.GetChannelSchedules(context.Background(), "chan_1", 0)
	require.NoError(t, err)
	assert.Empty(t, m.last().query)
}

func TestClient_GetChannelSchedule(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.GetChannelSchedule(context.Background(), "chan_1", "sched_1")
	require.NoError(t, err)
	assertLast(t, m, http.MethodGet, "/channels/chan_1/schedules/sched_1", "")
}

func TestClient_CreateChannelSchedule(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.CreateChannelSchedule(context.Background(), "chan_1", &dto.ScheduleRequest{Schedule: &dto.Schedule{Name: "日程", StartTimestamp: "1700000000000", EndTimestamp: "1700003600000", RemindType: "1"}})
	require.NoError(t, err)
	assertLast(t, m, http.MethodPost, "/channels/chan_1/schedules", `{"schedule":{"name":"日程","start_timestamp":"1700000000000","end_timestamp":"1700003600000","remind_type":"1"}}`)
}

func TestClient_UpdateChannelSchedule(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.UpdateChannelSchedule(context.Background(), "chan_1", "sched_1", &dto.ScheduleRequest{Schedule: &dto.Schedule{Name: "改", StartTimestamp: "1700000000000", EndTimestamp: "1700003600000", RemindType: "0"}})
	require.NoError(t, err)
	assertLast(t, m, http.MethodPatch, "/channels/chan_1/schedules/sched_1", `{"schedule":{"name":"改","start_timestamp":"1700000000000","end_timestamp":"1700003600000","remind_type":"0"}}`)
}

func TestClient_DeleteChannelSchedule(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.DeleteChannelSchedule(context.Background(), "chan_1", "sched_1")
	require.NoError(t, err)
	assertLast(t, m, http.MethodDelete, "/channels/chan_1/schedules/sched_1", "")
}

func TestClient_AudioControl(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.AudioControl(context.Background(), "chan_1", &dto.AudioControlRequest{Status: 0})
	require.NoError(t, err)
	assertLast(t, m, http.MethodPost, "/channels/chan_1/audio", `{"status":0}`)
}

func TestClient_BotOnMic(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.BotOnMic(context.Background(), "chan_1")
	require.NoError(t, err)
	assertLast(t, m, http.MethodPut, "/channels/chan_1/mic", "")
}

func TestClient_BotOffMic(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.BotOffMic(context.Background(), "chan_1")
	require.NoError(t, err)
	assertLast(t, m, http.MethodDelete, "/channels/chan_1/mic", "")
}

func TestClient_GetThreadList(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.GetThreadList(context.Background(), "chan_1")
	require.NoError(t, err)
	assertLast(t, m, http.MethodGet, "/channels/chan_1/threads", "")
}

func TestClient_GetThread(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.GetThread(context.Background(), "chan_1", "thread_1")
	require.NoError(t, err)
	assertLast(t, m, http.MethodGet, "/channels/chan_1/threads/thread_1", "")
}

func TestClient_CreateThread(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.CreateThread(context.Background(), "chan_1", &dto.ThreadRequest{Title: "帖子"})
	require.NoError(t, err)
	assertLast(t, m, http.MethodPut, "/channels/chan_1/threads", `{"title":"帖子","content":"","format":0}`)
}

func TestClient_DeleteThread(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.DeleteThread(context.Background(), "chan_1", "thread_1")
	require.NoError(t, err)
	assertLast(t, m, http.MethodDelete, "/channels/chan_1/threads/thread_1", "")
}

func TestClient_GetGateway(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.GetGateway(context.Background())
	require.NoError(t, err)
	assertLast(t, m, http.MethodGet, "/gateway", "")
}

func TestClient_GetGatewayBot(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.GetGatewayBot(context.Background())
	require.NoError(t, err)
	assertLast(t, m, http.MethodGet, "/gateway/bot", "")
}

// ────────────────────────────────────────────────────────────────────────────
// 基础方法与错误路径
// ────────────────────────────────────────────────────────────────────────────

func TestClient_AuthHeader(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	assert.Equal(t, "QQBot test-access-token", api.authHeader())
}

func TestClient_Post_TokenNotReady(t *testing.T) {
	m := newMockQQ(t)
	api, mgr := newTestAPI(t, m)
	mgr.Stop() // 停止后 WaitReadyWithContext 应立即失败

	_, err := api.Post(context.Background(), "https://api.bot.qq.com/v2/users/u1/messages", map[string]any{"content": "hi"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "access token 未就绪")
}

func TestClient_Post_ContextCancelled(t *testing.T) {
	m := newMockQQ(t)
	api, mgr := newTestAPI(t, m)
	_ = mgr

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := api.Post(ctx, "https://api.bot.qq.com/v2/users/u1/messages", map[string]any{"content": "hi"})
	require.Error(t, err)
}

func TestClient_ResponseData(t *testing.T) {
	m := newMockQQ(t)
	m.resp["/v2/users/u1/messages"] = `{"data":{"id":"new_msg_1"}}`
	api, _ := newTestAPI(t, m)

	result, err := api.SingleChat(context.Background(), "u1", &dto.Message{Content: "hi"})
	require.NoError(t, err)
	assert.Equal(t, "new_msg_1", result.Get("data.id").String())
	assert.True(t, result.Get("data.id").Exists())
}

// ────────────────────────────────────────────────────────────────────────────
// 群聊管理（2026-08 新增）
// ────────────────────────────────────────────────────────────────────────────

func TestClient_GetGroupInfo(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.GetGroupInfo(context.Background(), "gid_1")
	require.NoError(t, err)
	assertLast(t, m, http.MethodGet, "/v2/groups/gid_1/info", "")
}

func TestClient_GetGroupBotState(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.GetGroupBotState(context.Background(), "gid_1")
	require.NoError(t, err)
	assertLast(t, m, http.MethodGet, "/v2/groups/gid_1/bot_state", "")
}

func TestClient_GetGroupJoinRequestList(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.GetGroupJoinRequestList(context.Background(), "gid_1", "cursor_1", 50)
	require.NoError(t, err)
	req := m.last()
	assert.Equal(t, "/v2/groups/gid_1/join_request_list", req.path)
	assert.Equal(t, "cursor=cursor_1&limit=50", req.query)
}

func TestClient_GetGroupJoinRequestList_NoCursor(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.GetGroupJoinRequestList(context.Background(), "gid_1", "", 0)
	require.NoError(t, err)
	req := m.last()
	assert.Equal(t, "/v2/groups/gid_1/join_request_list", req.path)
	assert.Equal(t, "", req.query)
}

func TestClient_ApproveJoinRequest(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.ApproveJoinRequest(context.Background(), "gid_1", "mem_1", &dto.ApprovalJoinRequest{Op: "approve", JoinRequestID: "req_1"})
	require.NoError(t, err)
	assertLast(t, m, http.MethodPost, "/v2/groups/gid_1/approval_join_request/mem_1", `{"op":"approve","join_request_id":"req_1"}`)
}

func TestClient_GetGroupRestrictChatSetting(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.GetGroupRestrictChatSetting(context.Background(), "gid_1")
	require.NoError(t, err)
	assertLast(t, m, http.MethodGet, "/v2/groups/gid_1/restrict_chat_setting", "")
}

func TestClient_SetGroupMemberMute(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	req := &dto.SetRestrictChatSettingRequest{Members: []dto.SetMemberMuteState{{Op: "add", MemberOpenID: "mem_1", MuteExpireAt: "2026-08-05T11:23:05+08:00"}}}
	_, err := api.SetGroupMemberMute(context.Background(), "gid_1", req)
	require.NoError(t, err)
	assertLast(t, m, http.MethodPost, "/v2/groups/gid_1/restrict_chat_setting", `{"members":[{"op":"add","member_openid":"mem_1","mute_expire_at":"2026-08-05T11:23:05+08:00"}]}`)
}

func TestClient_GetJoinApprovalStrategyList(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.GetJoinApprovalStrategyList(context.Background(), "cursor_1", 20)
	require.NoError(t, err)
	req := m.last()
	assert.Equal(t, "/v2/groups/join_approval_strategy", req.path)
	assert.Equal(t, "cursor=cursor_1&limit=20", req.query)
}

func TestClient_CreateJoinApprovalStrategy(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.CreateJoinApprovalStrategy(context.Background(), &dto.CreateJoinApprovalStrategyRequest{GroupIDs: []string{"123456"}, IsEnable: "on"})
	require.NoError(t, err)
	assertLast(t, m, http.MethodPost, "/v2/groups/join_approval_strategy", `{"group_ids":["123456"],"is_enable":"on"}`)
}

func TestClient_UpdateJoinApprovalStrategy(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.UpdateJoinApprovalStrategy(context.Background(), "st_1", &dto.UpdateJoinApprovalStrategyRequest{IsEnable: "off"})
	require.NoError(t, err)
	assertLast(t, m, http.MethodPatch, "/v2/groups/join_approval_strategy/st_1", `{"is_enable":"off"}`)
}

func TestClient_DeleteJoinApprovalStrategy(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.DeleteJoinApprovalStrategy(context.Background(), "st_1")
	require.NoError(t, err)
	assertLast(t, m, http.MethodDelete, "/v2/groups/join_approval_strategy/st_1", "")
}

func TestClient_ExecuteJoinApprovalStrategy(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.ExecuteJoinApprovalStrategy(context.Background(), "st_1")
	require.NoError(t, err)
	assertLast(t, m, http.MethodPost, "/v2/groups/join_approval_strategy/st_1/execute", `{}`)
}

func TestClient_UpdateJoinApprovalStrategyWhitelist(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.UpdateJoinApprovalStrategyWhitelist(context.Background(), "st_1", &dto.UpdateWhitelistUsersRequest{Op: "add", WhitelistUsers: []string{"1234567"}})
	require.NoError(t, err)
	assertLast(t, m, http.MethodPost, "/v2/groups/join_approval_strategy/st_1/whitelist_users", `{"op":"add","whitelist_users":["1234567"]}`)
}

func TestClient_GenerateURLLink(t *testing.T) {
	m := newMockQQ(t)
	api, _ := newTestAPI(t, m)

	_, err := api.GenerateURLLink(context.Background(), &dto.GenerateURLLinkRequest{CallbackData: "custom_data_123"})
	require.NoError(t, err)
	assertLast(t, m, http.MethodPost, "/v2/generate_url_link", `{"callback_data":"custom_data_123"}`)
}
