package telegram_test

// userinfo_test.go — platform.UserInfoProvider / AvatarProvider（2026-11）。

import (
	"context"
	"net/http"
	"testing"

	"github.com/KomeiDiSanXian/remilia/infra/jsonx"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/telegram"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSender_GetUserInfo 验证 getChat 响应映射为 platform.UserProfile。
//
// Telegram 没有独立的用户资料接口：私聊时 chat_id 与 user id 相同。
func TestSender_GetUserInfo(t *testing.T) {
	srv := testAPIFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/botTOK/getChat", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		jsonx.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"result": map[string]any{
				"id":         1001,
				"type":       "private",
				"first_name": "Alice",
				"last_name":  "Wang",
				"username":   "alice",
			},
		})
	})
	defer srv.Close()

	sender := telegram.NewSender(newTestClient("TOK", srv.URL), "bot1")
	profile, err := sender.GetUserInfo(context.Background(), "1001")
	require.NoError(t, err)
	assert.Equal(t, "1001", profile.ID)
	assert.Equal(t, "Alice Wang", profile.DisplayName, "Chat.DisplayName 不含 @username")
	assert.Empty(t, profile.AvatarURL, "UserProfile 不复用 ChatPhoto")
	assert.False(t, profile.AllowProactiveMsg)
	assert.Empty(t, profile.UnionID)
}

// TestSender_GetUserAvatarURL 验证 getChat → getFile → FileURL 的头像链路。
func TestSender_GetUserAvatarURL(t *testing.T) {
	srv := testAPIFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/botTOK/getChat":
			jsonx.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"result": map[string]any{
					"id":         1001,
					"type":       "private",
					"first_name": "Alice",
					"photo": map[string]any{
						"small_file_id": "small1",
						"big_file_id":   "big1",
					},
				},
			})
		case "/botTOK/getFile":
			jsonx.NewEncoder(w).Encode(map[string]any{
				"ok":     true,
				"result": map[string]any{"file_id": "big1", "file_path": "photos/avatar.jpg"},
			})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	})
	defer srv.Close()

	sender := telegram.NewSender(newTestClient("TOK", srv.URL), "bot1")
	url, err := sender.GetUserAvatarURL(context.Background(), "1001")
	require.NoError(t, err)
	assert.Equal(t, "https://api.telegram.org/file/botTOK/photos/avatar.jpg", url)
}

// TestSender_GetUserAvatarURL_NoPhoto 验证用户没有头像时返回空串而非错误。
func TestSender_GetUserAvatarURL_NoPhoto(t *testing.T) {
	srv := testAPIFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		jsonx.NewEncoder(w).Encode(map[string]any{
			"ok":     true,
			"result": map[string]any{"id": 1001, "type": "private", "first_name": "Alice"},
		})
	})
	defer srv.Close()

	sender := telegram.NewSender(newTestClient("TOK", srv.URL), "bot1")
	url, err := sender.GetUserAvatarURL(context.Background(), "1001")
	require.NoError(t, err)
	assert.Empty(t, url)
}

// TestSender_GetUserInfo_Error 验证 API 错误向上传播。
func TestSender_GetUserInfo_Error(t *testing.T) {
	srv := testAPIError("Bad Request: chat not found", http.StatusBadRequest)
	defer srv.Close()

	sender := telegram.NewSender(newTestClient("TOK", srv.URL), "bot1")
	_, err := sender.GetUserInfo(context.Background(), "404")
	require.Error(t, err)
}

// TestSender_ImplementsUserInfoProvider 接口实现断言。
func TestSender_ImplementsUserInfoProvider(t *testing.T) {
	sender := telegram.NewSender(newTestClient("TOK", "http://127.0.0.1:1"), "bot1")
	var _ platform.UserInfoProvider = sender
	var _ platform.AvatarProvider = sender
}
