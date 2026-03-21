package dto

// ────────────────────────────────────────────────────────────────────────────
// 频道管理相关 DTO（仅私域机器人可用）
// ────────────────────────────────────────────────────────────────────────────

// MuteRequest 禁言请求体（全员禁言 / 指定成员禁言 / 批量成员禁言）。
//
// mute_end_timestamp 与 mute_seconds 二选一；两者同时填写时以 mute_end_timestamp 为准。
// 解除禁言：将对应字段值设置为字符串 "0"。
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/channel/speak/patch_guild_mute.html
type MuteRequest struct {
	// MuteEndTimestamp 禁言到期绝对时间戳（秒），"0" 表示解除
	MuteEndTimestamp string `json:"mute_end_timestamp,omitempty"`
	// MuteSeconds 禁言时长（秒），"0" 表示解除
	MuteSeconds string `json:"mute_seconds,omitempty"`
}

// MultipleMuteRequest 批量成员禁言请求体。
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/channel/speak/patch_guild_mute_multi_member.html
type MultipleMuteRequest struct {
	MuteRequest
	// UserIDs 要禁言的用户 ID 列表
	UserIDs []string `json:"user_ids"`
}

// GuildRoleRequest 创建/修改频道身份组请求体。
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/channel/role-group/post_guild_role.html
type GuildRoleRequest struct {
	// Name 身份组名称（最长 100 字符）
	Name string `json:"name,omitempty"`
	// Color ARGB 颜色值
	Color uint32 `json:"color,omitempty"`
	// Hoist 是否在成员列表中单独展示：0=否，1=是
	Hoist int32 `json:"hoist,omitempty"`
}

// AddMemberRoleRequest 添加/删除身份组成员时可选传递子频道 ID（用于频道私有身份组）。
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/channel/role-group/put_guild_member_role.html
type AddMemberRoleRequest struct {
	// Channel 子频道 ID，仅在操作频道私有身份组成员时使用
	Channel *ChannelRef `json:"channel,omitempty"`
}

// ChannelRef 子频道引用，用于身份组成员操作。
type ChannelRef struct {
	// ID 子频道 ID
	ID string `json:"id"`
}

// PermissionRequest 修改子频道用户/身份组权限请求体。
//
// add 与 remove 为二进制权限位字符串（十六进制），每个 bit 对应一项权限。
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/channel/role-group/channel_permissions/put_channel_permissions.html
type PermissionRequest struct {
	// Add 需要增加的权限位（十六进制字符串，如 "0x0010"）
	Add string `json:"add,omitempty"`
	// Remove 需要删除的权限位
	Remove string `json:"remove,omitempty"`
}

// APIPermissionDemandRequest 发送接口权限授权链接请求体。
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/channel/api_permissions/
type APIPermissionDemandRequest struct {
	// ChannelID 授权链接发送到的子频道 ID
	ChannelID string `json:"channel_id"`
	// APIInfo 要申请授权的接口信息
	APIInfo *APIPermissionDemandInfo `json:"api_info"`
	// Desc 机器人申请该权限的理由
	Desc string `json:"desc,omitempty"`
}

// APIPermissionDemandInfo 申请授权的接口标识。
type APIPermissionDemandInfo struct {
	// Path 接口路径，如 "/guilds/{guild_id}/members"
	Path string `json:"path"`
	// Method HTTP 方法，如 "GET"
	Method string `json:"method"`
}

// CreateGuildAnnounceRequest 创建频道公告请求体。
//
// message_id 与 announces_type/recommend_channels 两种方式二选一。
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/channel/content/announces/post_guild_announces.html
type CreateGuildAnnounceRequest struct {
	// MessageID 推送的消息 ID（从已有消息生成公告）
	MessageID string `json:"message_id,omitempty"`
	// ChannelID 消息所在子频道 ID（与 message_id 配合使用）
	ChannelID string `json:"channel_id,omitempty"`
	// AnnouncesType 公告类型：0=成员公告，1=欢迎公告；默认 0
	AnnouncesType int `json:"announces_type,omitempty"`
	// RecommendChannels 推荐子频道列表（欢迎公告时使用）
	RecommendChannels []RecommendChannel `json:"recommend_channels,omitempty"`
}

// RecommendChannel 欢迎公告推荐子频道。
type RecommendChannel struct {
	// ChannelID 子频道 ID
	ChannelID string `json:"channel_id"`
	// Introduce 子频道简介
	Introduce string `json:"introduce"`
}

// ScheduleRequest 创建/修改日程请求体。
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/channel/content/schedule/post_schedule.html
type ScheduleRequest struct {
	// Schedule 日程对象
	Schedule *Schedule `json:"schedule"`
}

// Schedule 日程详情。
type Schedule struct {
	// Name 日程名称（最长 100 字符）
	Name string `json:"name"`
	// Description 日程描述
	Description string `json:"description,omitempty"`
	// StartTimestamp 开始时间戳（毫秒）
	StartTimestamp string `json:"start_timestamp"`
	// EndTimestamp 结束时间戳（毫秒）
	EndTimestamp string `json:"end_timestamp"`
	// Creator 创建者（仅返回时有值）
	Creator *ScheduleMember `json:"creator,omitempty"`
	// JumpChannelID 日程开始时跳转的子频道 ID
	JumpChannelID string `json:"jump_channel_id,omitempty"`
	// RemindType 提醒类型：0=不提醒，1=开始时提醒，2=开始前5分钟，...
	RemindType string `json:"remind_type"`
}

// ScheduleMember 日程成员信息（返回字段）。
type ScheduleMember struct {
	User   *ScheduleUser `json:"user,omitempty"`
	Nick   string        `json:"nick,omitempty"`
	Roles  []string      `json:"roles,omitempty"`
	JoinAt string        `json:"joined_at,omitempty"`
}

// ScheduleUser 日程成员用户信息（返回字段）。
type ScheduleUser struct {
	ID       string `json:"id,omitempty"`
	Username string `json:"username,omitempty"`
	Avatar   string `json:"avatar,omitempty"`
	Bot      bool   `json:"bot,omitempty"`
}

// AudioControlRequest 音频控制请求体。
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/channel/content/audio/audio_control.html
type AudioControlRequest struct {
	// AudioURL 音频资源 URL（开始播放时必填）
	AudioURL string `json:"audio_url,omitempty"`
	// Text 背景描述（开始播放时必填）
	Text string `json:"text,omitempty"`
	// Status 播放状态：0=开始播放，1=暂停播放，2=继续播放，3=停止播放
	Status int `json:"status"`
}

// ThreadRequest 发表帖子请求体。
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/channel/content/forum/put_thread.html
type ThreadRequest struct {
	// Title 帖子标题
	Title string `json:"title"`
	// Content 帖子内容（RichText JSON 字符串）
	Content string `json:"content"`
	// Format 内容格式：1=普通文本，2=HTML，3=Markdown，4=JSON（RichText）
	Format int `json:"format"`
}

// InteractionResponse 互动事件回应请求体。
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/trans/msg-btn.html#%E5%9B%9E%E5%BA%94
type InteractionResponse struct {
	// Code 结果码：0=成功，1=操作失败，2=频繁，3=重复操作，4=无权限，5=仅管理员可操作
	Code int `json:"code"`
}
