package discord

// sender_userinfo_test.go — platform.UserInfoProvider / AvatarProvider（2026-11）。

import (
	stdctx "context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/bwmarrin/discordgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// redirectTransport 把发往 Discord API 的请求转到本地测试服务器。
type redirectTransport struct {
	target *httptest.Server
}

func (rt *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	u := *req.URL
	u.Scheme = "http"
	u.Host = rt.target.Listener.Addr().String()
	clone := req.Clone(req.Context())
	clone.URL = &u
	clone.Host = u.Host
	return http.DefaultTransport.RoundTrip(clone)
}

// newUserTestSender 构造一个 REST 请求打到本地测试服务器的 discordSender。
func newUserTestSender(t *testing.T, handler http.HandlerFunc) *discordSender {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	sess, err := discordgo.New("Bot test-token")
	require.NoError(t, err)
	sess.Client = &http.Client{Transport: &redirectTransport{target: srv}}
	return &discordSender{session: sess}
}

// TestSender_GetUserInfo 验证 GET /users/{id} 映射为 platform.UserProfile，
// 并按 SDK 的 AvatarURL 规则拼出头像地址。
func TestSender_GetUserInfo(t *testing.T) {
	var gotPath string
	s := newUserTestSender(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"u1","username":"alice","global_name":"Alice","avatar":"abc123"}`))
	})

	profile, err := s.GetUserInfo(stdctx.Background(), "u1")
	require.NoError(t, err)
	assert.Equal(t, "/api/v9/users/u1", gotPath)
	assert.Equal(t, "u1", profile.ID)
	assert.Equal(t, "Alice", profile.DisplayName, "global_name 优先于 username")
	assert.Equal(t, discordgo.EndpointUserAvatar("u1", "abc123"), profile.AvatarURL)
	assert.Empty(t, profile.UnionID)
	assert.False(t, profile.AllowProactiveMsg)
}

// TestSender_GetUserInfo_NoAvatar 验证用户没有自定义头像时 AvatarURL 为空
// （不回退到 Discord 默认头像 URL，避免调用方误判"有头像"）。
func TestSender_GetUserInfo_NoAvatar(t *testing.T) {
	s := newUserTestSender(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"u2","username":"bob"}`))
	})

	profile, err := s.GetUserInfo(stdctx.Background(), "u2")
	require.NoError(t, err)
	assert.Equal(t, "bob", profile.DisplayName)
	assert.Empty(t, profile.AvatarURL)
}

// TestSender_GetUserInfo_Error 验证 REST 错误向上传播。
func TestSender_GetUserInfo_Error(t *testing.T) {
	s := newUserTestSender(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Unknown User","code":10013}`))
	})

	_, err := s.GetUserInfo(stdctx.Background(), "ghost")
	require.Error(t, err)
}

// TestSender_GetUserAvatarURL 验证头像查询复用用户查询。
func TestSender_GetUserAvatarURL(t *testing.T) {
	s := newUserTestSender(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"u1","username":"alice","avatar":"abc123"}`))
	})

	url, err := s.GetUserAvatarURL(stdctx.Background(), "u1")
	require.NoError(t, err)
	assert.Equal(t, discordgo.EndpointUserAvatar("u1", "abc123"), url)
}

// TestSender_ImplementsUserInfoProvider 接口实现断言。
func TestSender_ImplementsUserInfoProvider(t *testing.T) {
	s := &discordSender{}
	var _ platform.UserInfoProvider = s
	var _ platform.AvatarProvider = s
}
