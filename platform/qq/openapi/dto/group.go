package dto

// ────────────────────────────────────────────────────────────────────────────
// 群聊管理 DTO（2026-08 新增）
//
// 相关接口文档：
//   - 获取群基本信息：https://bot.q.qq.com/wiki/develop/api-v2/autogen/api/v2_groups_group_openid_info.get.html
//   - 获取机器人群内状态：.../v2_groups_group_openid_bot_state.get.html
//   - 入群申请列表拉取：.../v2_groups_group_openid_join_request_list.get.html
//   - 入群申请审批：.../v2_groups_group_openid_approval_join_request_member_openid.post.html
//   - 查询群禁言状态：.../v2_groups_group_openid_restrict_chat_setting.get.html
//   - 设置群成员禁言：.../v2_groups_group_openid_restrict_chat_setting.post.html
//   - 入群自动审批策略：.../v2_groups_join_approval_strategy*.html
//   - 获取群成员列表：.../v2_groups_group_openid_members.get.html
//   - 获取群成员信息：.../v2_groups_group_openid_members_member_openid.get.html
//   - 群成员批量移除：.../v2_groups_group_openid_batch_remove_members.post.html
//   - 群黑名单查询/操作：.../v2_groups_group_openid_member_blacklist.get.html
//     .../v2_groups_group_openid_member_blacklist.post.html
// ────────────────────────────────────────────────────────────────────────────

// GroupMember 群成员（2026-09 起官方群成员接口返回的完整字段）。
type GroupMember struct {
	// MemberOpenID 成员 openid。
	MemberOpenID string `json:"member_openid,omitempty"`
	// Username 用户昵称。
	Username string `json:"username,omitempty"`
	// MemberRole 群成员角色：member-普通成员，owner-群主，admin-管理员。
	MemberRole string `json:"member_role,omitempty"`
	// Bot 是否机器人。
	Bot bool `json:"bot,omitempty"`
	// JoinedAt 入群时间（RFC3339）。
	JoinedAt string `json:"joined_at,omitempty"`
	// UnionOpenID 用户在应用/开放平台下的统一标识（如有）。
	UnionOpenID string `json:"union_openid,omitempty"`
}

// GroupMemberList 群成员列表响应。
type GroupMemberList struct {
	// Members 当前页成员列表。
	Members []GroupMember `json:"members,omitempty"`
	// NextCursor 下一页游标；空串表示已到末页。
	NextCursor string `json:"next_cursor,omitempty"`
}

// BatchRemoveGroupMembersRequest 群成员批量移除请求体。
//
// 单次最多移除 20 个成员；AddToMemberBlacklist=true 时同时加入群黑名单
// （禁止重新加入）。机器人需拥有群管理员身份。
type BatchRemoveGroupMembersRequest struct {
	// MemberOpenIDs 需要移除的成员 openid 列表，单次最多 20 个。
	MemberOpenIDs []string `json:"member_openids"`
	// AddToMemberBlacklist 是否同时加入群黑名单，默认 false。
	AddToMemberBlacklist bool `json:"add_to_member_blacklist,omitempty"`
}

// BlacklistUser 群黑名单用户。
type BlacklistUser struct {
	// UnionOpenID 用户在应用/开放平台下的统一标识（如有）。
	UnionOpenID string `json:"union_openid,omitempty"`
	// MemberOpenID 用户 openid。
	MemberOpenID string `json:"member_openid,omitempty"`
	// Username 用户昵称。
	Username string `json:"username,omitempty"`
	// BannedAt 拉黑时间（RFC3339）。
	BannedAt string `json:"banned_at,omitempty"`
	// Bot 是否为机器人账号。
	Bot bool `json:"bot,omitempty"`
}

// GroupMemberBlacklist 群黑名单响应。
type GroupMemberBlacklist struct {
	// Users 黑名单用户列表。
	Users []BlacklistUser `json:"users,omitempty"`
	// NextCursor 下一页游标；空串表示已到末页。
	NextCursor string `json:"next_cursor,omitempty"`
}

// UpdateGroupMemberBlacklistRequest 群黑名单操作请求体。
//
// Op=add 加入黑名单（目标成员在群中时无法加入），Op=del 移出黑名单；
// 单次最多操作 20 个成员。
type UpdateGroupMemberBlacklistRequest struct {
	// Op 操作类型：del 移出黑名单，add 加入黑名单。
	Op string `json:"op"`
	// MemberOpenIDs 目标成员 openid 列表，单次最多 20 个。
	MemberOpenIDs []string `json:"member_openids"`
}

// GroupInfo 群基本信息。
type GroupInfo struct {
	GroupOpenID     string   `json:"group_openid,omitempty"`
	GroupName       string   `json:"group_name,omitempty"`
	GroupFingerMemo string   `json:"group_finger_memo,omitempty"`
	GroupClassText  string   `json:"group_class_text,omitempty"`
	GroupTags       []string `json:"group_tags,omitempty"`
	GroupMemberNum  int      `json:"group_member_num,omitempty"`
}

// GroupBotState 机器人群内状态。
type GroupBotState struct {
	MemberOpenID      string `json:"member_openid,omitempty"`
	JoinedAt          string `json:"joined_at,omitempty"`
	AllowProactiveMsg bool   `json:"allow_proactive_msg,omitempty"`
	RecvMsgSetting    string `json:"recv_msg_setting,omitempty"`
	MemberRole        string `json:"member_role,omitempty"`
}

// JoinRequest 入群申请。
type JoinRequest struct {
	JoinRequestID string      `json:"join_request_id,omitempty"`
	RiskTips      string      `json:"risk_tips,omitempty"`
	UnionOpenID   string      `json:"union_openid,omitempty"`
	MemberOpenID  string      `json:"member_openid,omitempty"`
	Username      string      `json:"username,omitempty"`
	ApplyAt       string      `json:"apply_at,omitempty"`
	ApplySource   string      `json:"apply_source,omitempty"`
	InvitedBy     string      `json:"invited_by,omitempty"`
	Bot           bool        `json:"bot,omitempty"`
	VerifyInfo    *VerifyInfo `json:"verify_info,omitempty"`
}

// VerifyInfo 用户入群验证方式。
type VerifyInfo struct {
	Method        string     `json:"method,omitempty"`
	VerifyMessage string     `json:"verify_message,omitempty"`
	ReviewQAList  []ReviewQA `json:"review_qa_list,omitempty"`
}

// ReviewQA 入群验证问答。
type ReviewQA struct {
	Question string `json:"question,omitempty"`
	Answer   string `json:"answer,omitempty"`
}

// JoinRequestList 入群申请列表响应。
type JoinRequestList struct {
	List       []JoinRequest `json:"list,omitempty"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

// ApprovalJoinRequest 审批入群请求体。
type ApprovalJoinRequest struct {
	Op string `json:"op"`
	// JoinRequestID 申请 ID，从入群申请列表或 GROUP_JOIN_REQUEST 事件中获取。
	JoinRequestID string `json:"join_request_id,omitempty"`
	// RejectReason 拒绝理由，action=decline 时可填。
	RejectReason string `json:"reject_reason,omitempty"`
	// AddToMemberBlacklist 是否同时加入群黑名单，默认 false，action=decline 时可填。
	AddToMemberBlacklist bool `json:"add_to_member_blacklist,omitempty"`
}

// GroupRestrictChatSetting 群禁言状态响应。
type GroupRestrictChatSetting struct {
	GlobalRule *GlobalMuteRule   `json:"global_rule,omitempty"`
	Members    []MemberMuteState `json:"members,omitempty"`
}

// GlobalMuteRule 群级禁言规则（全员禁言配置）。
type GlobalMuteRule struct {
	Mode           string              `json:"mode,omitempty"`
	ScheduleRules  []MuteScheduleRule  `json:"schedule_rules,omitempty"`
	RecurringRules []MuteRecurringRule `json:"recurring_rules,omitempty"`
}

// MuteScheduleRule 定时禁言规则。
type MuteScheduleRule struct {
	TaskID  string `json:"task_id,omitempty"`
	StartAt string `json:"start_at,omitempty"`
	EndAt   string `json:"end_at,omitempty"`
	Enabled bool   `json:"enabled,omitempty"`
}

// MuteRecurringRule 周期禁言规则。
type MuteRecurringRule struct {
	TaskID    string `json:"task_id,omitempty"`
	Weekdays  []int  `json:"weekdays,omitempty"`
	StartTime string `json:"start_time,omitempty"`
	EndTime   string `json:"end_time,omitempty"`
	Enabled   bool   `json:"enabled,omitempty"`
}

// MemberMuteState 当前处于禁言中的用户。
type MemberMuteState struct {
	MemberOpenID string `json:"member_openid,omitempty"`
	MuteExpireAt string `json:"mute_expire_at,omitempty"`
	Username     string `json:"username,omitempty"`
	UnionOpenID  string `json:"union_openid,omitempty"`
}

// SetMemberMuteState 设置单个成员禁言。
type SetMemberMuteState struct {
	// Op 操作类型：add 增加禁言，update 更新禁言到期时间，del 解除禁言。
	Op string `json:"op"`
	// MemberOpenID 被禁言成员 openid；增加/更新时只能操作普通成员。
	MemberOpenID string `json:"member_openid"`
	// MuteExpireAt 禁言到期时间（RFC3339）；op=del 时可传空串表示立即解除禁言。
	MuteExpireAt string `json:"mute_expire_at,omitempty"`
}

// SetRestrictChatSettingRequest 设置群成员禁言请求体。
type SetRestrictChatSettingRequest struct {
	Members []SetMemberMuteState `json:"members,omitempty"`
}

// JoinApprovalStrategy 入群自动审批策略。
type JoinApprovalStrategy struct {
	StrategyID         string   `json:"strategy_id,omitempty"`
	GroupOpenIDs       []string `json:"group_openids,omitempty"`
	GroupIDs           []string `json:"group_ids,omitempty"`
	WhitelistUserCount int      `json:"whitelist_user_count,omitempty"`
	IsEnable           string   `json:"is_enable,omitempty"`
	ExpireAt           string   `json:"expire_at,omitempty"`
	CreatedAt          string   `json:"created_at,omitempty"`
	UpdatedAt          string   `json:"updated_at,omitempty"`
	Remark             string   `json:"remark,omitempty"`
}

// JoinApprovalStrategyList 策略列表响应。
type JoinApprovalStrategyList struct {
	Strategies []JoinApprovalStrategy `json:"strategies,omitempty"`
	NextCursor string                 `json:"next_cursor,omitempty"`
}

// CreateJoinApprovalStrategyRequest 创建入群自动审批策略请求体。
type CreateJoinApprovalStrategyRequest struct {
	// GroupOpenIDs 关联的群 openid 列表，最多 100 个；与 GroupIDs 互斥。
	GroupOpenIDs []string `json:"group_openids,omitempty"`
	// GroupIDs 关联的 QQ 群号列表，最多 100 个；与 GroupOpenIDs 互斥。
	GroupIDs []string `json:"group_ids,omitempty"`
	// IsEnable 是否启用策略，on-启用 off-关闭，默认 on。
	IsEnable string `json:"is_enable,omitempty"`
	// ExpireAt 过期时间（RFC3339）；不传默认一年过期。
	ExpireAt string `json:"expire_at,omitempty"`
	// Remark 策略备注，最多 255 个汉字。
	Remark string `json:"remark,omitempty"`
}

// GroupAction 修改策略时关联群增删操作。
type GroupAction struct {
	// Op 操作类型：add 新增关联群，del 删除关联群。
	Op string `json:"op"`
	// GroupOpenIDs 待操作的群 openid 列表；与 GroupIDs 互斥。
	GroupOpenIDs []string `json:"group_openids,omitempty"`
	// GroupIDs 待操作的 QQ 群号列表；与 GroupOpenIDs 互斥。
	GroupIDs []string `json:"group_ids,omitempty"`
}

// UpdateJoinApprovalStrategyRequest 修改入群自动审批策略请求体。
type UpdateJoinApprovalStrategyRequest struct {
	IsEnable    string       `json:"is_enable,omitempty"`
	ExpireAt    string       `json:"expire_at,omitempty"`
	GroupAction *GroupAction `json:"group_action,omitempty"`
	Remark      string       `json:"remark,omitempty"`
}

// UpdateWhitelistUsersRequest 修改策略白名单号码请求体。
type UpdateWhitelistUsersRequest struct {
	// Op 操作类型：add 新增号码，del 删除号码。
	Op string `json:"op"`
	// WhitelistUsers QQ 号码列表，单次最多 10000 个；使用字符串类型避免 JS 精度问题。
	WhitelistUsers []string `json:"whitelist_users"`
}

// GenerateURLLinkRequest 生成机器人分享链接请求体。
//
// https://bot.q.qq.com/wiki/develop/api-v2/autogen/api/v2_generate_url_link.post.html
type GenerateURLLinkRequest struct {
	// CallbackData 自定义回调数据，用户通过链接添加机器人时透传给开发者（最长 32 字符）。
	CallbackData string `json:"callback_data,omitempty"`
}
