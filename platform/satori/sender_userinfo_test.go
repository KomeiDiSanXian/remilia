// Package satori sender_userinfo_test.go 覆盖 2026-11 新增的
// platform.UserInfoProvider / platform.AvatarProvider 实现。
package satori

import (
	stdctx "context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newUserTestSender 构造一个指向 httptest 服务端的 satoriSender。
func newUserTestSender(t *testing.T, handler http.HandlerFunc) *satoriSender {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return newSender(newClient(Config{ServerURL: srv.URL, Platform: "test", UserID: "bot", Version: "v1"}))
}

// TestSender_GetUserInfo 验证 user.get 响应映射为 platform.UserProfile，
// 且 Nick 优先于 Name。
func TestSender_GetUserInfo(t *testing.T) {
	var gotPath string
	s := newUserTestSender(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"id":"u1","name":"qq_user_1","nick":"小明","avatar":"https://ex.com/a.png","is_bot":false}`))
	})

	profile, err := s.GetUserInfo(stdctx.Background(), "u1")
	require.NoError(t, err)
	assert.Equal(t, "u1", profile.ID)
	assert.Equal(t, "小明", profile.DisplayName, "Nick 应优先于 Name")
	assert.Equal(t, "https://ex.com/a.png", profile.AvatarURL)
	assert.False(t, profile.AllowProactiveMsg)
	assert.Empty(t, profile.UnionID)
	assert.Equal(t, "/v1/user.get", gotPath)
}

// TestSender_GetUserInfo_NameFallback 验证没有 Nick 时使用 Name。
func TestSender_GetUserInfo_NameFallback(t *testing.T) {
	s := newUserTestSender(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"u1","name":"Alice"}`))
	})

	profile, err := s.GetUserInfo(stdctx.Background(), "u1")
	require.NoError(t, err)
	assert.Equal(t, "Alice", profile.DisplayName)
	assert.Empty(t, profile.AvatarURL)
}

// TestSender_GetUserInfo_HTTPError 验证非 2xx 响应向上传播错误。
func TestSender_GetUserInfo_HTTPError(t *testing.T) {
	s := newUserTestSender(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`method not found`))
	})

	_, err := s.GetUserInfo(stdctx.Background(), "u1")
	require.Error(t, err)
}

// TestSender_GetUserAvatarURL 验证头像查询复用 user.get。
func TestSender_GetUserAvatarURL(t *testing.T) {
	s := newUserTestSender(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"u1","name":"Alice","avatar":"https://ex.com/a.png"}`))
	})

	url, err := s.GetUserAvatarURL(stdctx.Background(), "u1")
	require.NoError(t, err)
	assert.Equal(t, "https://ex.com/a.png", url)
}

// TestSender_ImplementsUserInfoProvider 接口实现断言。
func TestSender_ImplementsUserInfoProvider(t *testing.T) {
	s := newSender(newClient(Config{ServerURL: "http://127.0.0.1:1", Version: "v1"}))
	var _ platform.UserInfoProvider = s
	var _ platform.AvatarProvider = s
}

// TestConvertEvent_SenderAvatar 验证事件里的 User.avatar 被映射到
// platform.UserInfo.AvatarURL（2026-11 起 AvatarURL 成为跨平台字段）。
func TestConvertEvent_SenderAvatar(t *testing.T) {
	e := makeEvent(EventTypeMessageCreated, ChannelTypeText)
	e.User = &User{ID: "u-1", Name: new("Alice"), Avatar: new("https://ex.com/a.png")}

	sender := convertEvent(e, "p").Sender()
	if sender.AvatarURL != "https://ex.com/a.png" {
		t.Errorf("Sender.AvatarURL: got %q", sender.AvatarURL)
	}
}
