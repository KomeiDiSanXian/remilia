package dto

// ────────────────────────────────────────────────────────────────────────────
// 用户信息 DTO（2026-11 新增）
//
// 相关接口文档：
//   - 获取用户基本信息：https://bot.q.qq.com/wiki/develop/api-v2/autogen/api/v2_users_user_openid_info.get.html
// ────────────────────────────────────────────────────────────────────────────

// UserInfo 用户基本信息（GET /v2/users/{user_openid}/info 响应）。
//
// 用户与机器人无好友关系时接口返回错误码 40184001。
type UserInfo struct {
	// UnionOpenID 跨应用关联用户 openid（绑定 3 方应用时有效）。
	UnionOpenID string `json:"union_openid,omitempty"`
	// Username 用户名（昵称）。
	Username string `json:"username,omitempty"`
	// Avatar 用户头像原图 URL。
	Avatar string `json:"avatar,omitempty"`
	// AllowProactiveMsg 是否接收主动推送：true=接受主动推送。
	AllowProactiveMsg bool `json:"allow_proactive_msg,omitzero"`
}
