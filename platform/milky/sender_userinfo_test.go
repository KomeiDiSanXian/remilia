package milky

// sender_userinfo_test.go — platform.UserInfoProvider（2026-11 新增）。

import (
	"context"
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSender_GetUserInfo 验证 get_user_profile 映射为 platform.UserProfile。
//
// Milky 资料没有头像字段，因此 AvatarURL 必须保持为空（也不实现
// AvatarProvider，避免调用方拿到永远为空的头像查询）。
func TestSender_GetUserInfo(t *testing.T) {
	m := newMockMilkyServer(t)
	s := newTestSender(t, m)

	profile, err := s.GetUserInfo(context.Background(), "1001")
	require.NoError(t, err)
	assert.Equal(t, "1001", profile.ID)
	assert.Equal(t, "Alice", profile.DisplayName)
	assert.Empty(t, profile.AvatarURL, "Milky 协议没有头像字段")
	assert.False(t, profile.AllowProactiveMsg)
	assert.Empty(t, profile.UnionID)
	assertReq(t, m, "/api/get_user_profile", `{"user_id":1001}`)
}

// TestSender_GetUserInfo_InvalidID 验证非法 user_id 在本地被拦下。
func TestSender_GetUserInfo_InvalidID(t *testing.T) {
	m := newMockMilkyServer(t)
	s := newTestSender(t, m)

	_, err := s.GetUserInfo(context.Background(), "not-a-number")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid user_id")
	assert.Empty(t, m.reqs, "非法 user_id 不应发起 API 调用")
}

// TestSender_DoesNotImplementAvatarProvider 固定"没有头像 API 就不声明能力"。
func TestSender_DoesNotImplementAvatarProvider(t *testing.T) {
	m := newMockMilkyServer(t)
	s := newTestSender(t, m)

	var up platform.UserInfoProvider = s
	require.NotNil(t, up)
	if _, ok := any(s).(platform.AvatarProvider); ok {
		t.Fatal("Milky 没有头像查询 API，不应实现 platform.AvatarProvider")
	}
}
