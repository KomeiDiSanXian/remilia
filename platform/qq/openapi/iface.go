package openapi

import (
	"context"

	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/tidwall/gjson"
)

// ── 子接口（按功能域划分）────────────────────────────────────────────────────

// MediaUploader 富媒体分片上传管理。
type MediaUploader interface {
	// UserUploadPrepare 单聊富媒体预上传（分片上传第一步）。
	UserUploadPrepare(ctx context.Context, openid string, req *dto.UploadPrepareRequest) (gjson.Result, error)
	// GroupUploadPrepare 群聊富媒体预上传（分片上传第一步）。
	GroupUploadPrepare(ctx context.Context, groupID string, req *dto.UploadPrepareRequest) (gjson.Result, error)
	// UserUploadPartFinish 单聊分片上传完成确认。
	UserUploadPartFinish(ctx context.Context, openid string, req *dto.UploadPartFinishRequest) (gjson.Result, error)
	// GroupUploadPartFinish 群聊分片上传完成确认。
	GroupUploadPartFinish(ctx context.Context, groupID string, req *dto.UploadPartFinishRequest) (gjson.Result, error)
}

// MessageSender 发送各类消息。
type MessageSender interface {
	SingleChat(ctx context.Context, openid string, msg *dto.Message) (gjson.Result, error)
	GroupChat(ctx context.Context, groupID string, msg *dto.Message) (gjson.Result, error)
	ChannelChat(ctx context.Context, channelID string, msg *dto.GuildMessage) (gjson.Result, error)
	DMChat(ctx context.Context, guildID string, msg *dto.GuildMessage) (gjson.Result, error)
	SingleRichMedia(ctx context.Context, openid string, media *dto.Media) (gjson.Result, error)
	GroupRichMedia(ctx context.Context, groupID string, media *dto.Media) (gjson.Result, error)
	// SingleStreamChat 流式分批发送单聊消息。
	SingleStreamChat(ctx context.Context, openid string, msg *dto.StreamMessage) (gjson.Result, error)
}

// MessageRecaller 撤回消息。
type MessageRecaller interface {
	SingleReset(ctx context.Context, openid, messageID string) (gjson.Result, error)
	GroupReset(ctx context.Context, groupID, messageID string) (gjson.Result, error)
	// hidetip=true 隐藏撤回灰条提示（仅私域机器人）
	ChannelReset(ctx context.Context, channelID, messageID string, hidetip bool) (gjson.Result, error)
	DMReset(ctx context.Context, guildID, messageID string, hidetip bool) (gjson.Result, error)
}

// InteractionResponder 回应互动事件（按钮回调等）。
type InteractionResponder interface {
	// RespondInteraction 回应 INTERACTION_CREATE 事件；code=0 表示成功。
	RespondInteraction(ctx context.Context, interactionID string, code int) (gjson.Result, error)
}

// ReactionManager 表情表态（仅频道，需 GUILD_MESSAGE_REACTIONS intent）。
type ReactionManager interface {
	// emojiType：1=系统表情，2=emoji
	AddReaction(ctx context.Context, channelID, messageID string, emojiType int, emojiID string) (gjson.Result, error)
	DeleteReaction(ctx context.Context, channelID, messageID string, emojiType int, emojiID string) (gjson.Result, error)
	// cookie：分页游标，首次传空；limit：默认 20，最大 50
	GetReactionUsers(ctx context.Context, channelID, messageID string, emojiType int, emojiID, cookie string, limit int) (gjson.Result, error)
}

// GuildManager 频道及子频道管理。
type GuildManager interface {
	GetMe(ctx context.Context) (gjson.Result, error)
	// before/after 为翻页游标（guild_id），limit=0 使用平台默认值（100）
	GetMyGuilds(ctx context.Context, before, after string, limit int) (gjson.Result, error)
	GetGuild(ctx context.Context, guildID string) (gjson.Result, error)
	GetGuildChannels(ctx context.Context, guildID string) (gjson.Result, error)
	GetChannel(ctx context.Context, channelID string) (gjson.Result, error)
	CreateGuildChannel(ctx context.Context, guildID string, req *dto.ChannelRequest) (gjson.Result, error)      // 仅私域
	UpdateGuildChannel(ctx context.Context, channelID string, req *dto.ChannelRequest) (gjson.Result, error)    // 仅私域
	DeleteGuildChannel(ctx context.Context, channelID string) (gjson.Result, error)                             // 仅私域
	CreateDirectMessageSession(ctx context.Context, req *dto.DirectMessageSessionRequest) (gjson.Result, error) // 发频道私信前必须先调用
}

// MemberManager 频道成员管理（仅私域机器人）。
type MemberManager interface {
	GetChannelOnlineNums(ctx context.Context, channelID string) (gjson.Result, error)
	// after=上一页末尾 user_id，首页填 "0"；limit=1~400
	GetGuildMembers(ctx context.Context, guildID, after string, limit int) (gjson.Result, error)
	GetGuildRoleMembers(ctx context.Context, guildID, roleID string, startIndex, limit uint32) (gjson.Result, error)
	GetGuildMember(ctx context.Context, guildID, userID string) (gjson.Result, error)
	// addBlacklist=true 同时加黑名单；deleteHistoryMsgDays=0 不清除历史消息
	DeleteGuildMember(ctx context.Context, guildID, userID string, addBlacklist bool, deleteHistoryMsgDays int) (gjson.Result, error)
}

// RoleManager 频道身份组及权限管理（仅私域机器人）。
type RoleManager interface {
	GetGuildRoles(ctx context.Context, guildID string) (gjson.Result, error)
	CreateGuildRole(ctx context.Context, guildID string, req *dto.GuildRoleRequest) (gjson.Result, error)
	UpdateGuildRole(ctx context.Context, guildID, roleID string, req *dto.GuildRoleRequest) (gjson.Result, error)
	DeleteGuildRole(ctx context.Context, guildID, roleID string) (gjson.Result, error)
	// channelID 仅私有身份组时传入
	AddGuildMemberRole(ctx context.Context, guildID, userID, roleID, channelID string) (gjson.Result, error)
	DeleteGuildMemberRole(ctx context.Context, guildID, userID, roleID, channelID string) (gjson.Result, error)
	GetChannelMemberPermissions(ctx context.Context, channelID, userID string) (gjson.Result, error)
	UpdateChannelMemberPermissions(ctx context.Context, channelID, userID string, req *dto.PermissionRequest) (gjson.Result, error)
	GetChannelRolePermissions(ctx context.Context, channelID, roleID string) (gjson.Result, error)
	UpdateChannelRolePermissions(ctx context.Context, channelID, roleID string, req *dto.PermissionRequest) (gjson.Result, error)
}

// APIPermissionManager 接口授权管理（仅私域机器人）。
type APIPermissionManager interface {
	GetGuildAPIPermissions(ctx context.Context, guildID string) (gjson.Result, error)
	RequestGuildAPIPermission(ctx context.Context, guildID string, req *dto.APIPermissionDemandRequest) (gjson.Result, error)
}

// MuteManager 发言管理/禁言（仅私域机器人）。
type MuteManager interface {
	GetGuildMessageSetting(ctx context.Context, guildID string) (gjson.Result, error)
	// mute_seconds="0" 或 mute_end_timestamp="0" 解除禁言
	MuteGuild(ctx context.Context, guildID string, req *dto.MuteRequest) (gjson.Result, error)
	MuteGuildMember(ctx context.Context, guildID, userID string, req *dto.MuteRequest) (gjson.Result, error)
	MuteGuildMultiMembers(ctx context.Context, guildID string, req *dto.MultipleMuteRequest) (gjson.Result, error)
}

// AnnouncementManager 频道公告管理（仅私域机器人）。
type AnnouncementManager interface {
	CreateGuildAnnounce(ctx context.Context, guildID string, req *dto.CreateGuildAnnounceRequest) (gjson.Result, error)
	// messageID 传 "all" 可删除所有公告
	DeleteGuildAnnounce(ctx context.Context, guildID, messageID string) (gjson.Result, error)
}

// PinManager 精华消息管理（仅私域机器人）。
type PinManager interface {
	PinMessage(ctx context.Context, channelID, messageID string) (gjson.Result, error)
	// messageID 传 "all" 可删除所有精华消息
	UnpinMessage(ctx context.Context, channelID, messageID string) (gjson.Result, error)
	// 最多返回 20 条
	GetPinnedMessages(ctx context.Context, channelID string) (gjson.Result, error)
}

// ScheduleManager 子频道日程管理（仅私域机器人）。
type ScheduleManager interface {
	// since 为起始时间戳（毫秒），0 表示不过滤
	GetChannelSchedules(ctx context.Context, channelID string, since uint64) (gjson.Result, error)
	GetChannelSchedule(ctx context.Context, channelID, scheduleID string) (gjson.Result, error)
	CreateChannelSchedule(ctx context.Context, channelID string, req *dto.ScheduleRequest) (gjson.Result, error)
	UpdateChannelSchedule(ctx context.Context, channelID, scheduleID string, req *dto.ScheduleRequest) (gjson.Result, error)
	DeleteChannelSchedule(ctx context.Context, channelID, scheduleID string) (gjson.Result, error)
}

// AudioManager 音频控制（仅私域机器人）。
type AudioManager interface {
	AudioControl(ctx context.Context, channelID string, req *dto.AudioControlRequest) (gjson.Result, error)
	BotOnMic(ctx context.Context, channelID string) (gjson.Result, error)
	BotOffMic(ctx context.Context, channelID string) (gjson.Result, error)
}

// ThreadManager 论坛帖子管理（仅私域机器人）。
type ThreadManager interface {
	GetThreadList(ctx context.Context, channelID string) (gjson.Result, error)
	GetThread(ctx context.Context, channelID, threadID string) (gjson.Result, error)
	CreateThread(ctx context.Context, channelID string, req *dto.ThreadRequest) (gjson.Result, error)
	DeleteThread(ctx context.Context, channelID, threadID string) (gjson.Result, error)
}

// GatewayManager WebSocket 网关接入点管理。
type GatewayManager interface {
	// GetGateway 获取通用 WSS 接入点。
	GetGateway(ctx context.Context) (gjson.Result, error)
	// GetGatewayBot 获取带分片的 WSS 接入点（含 shard 建议数）。
	GetGatewayBot(ctx context.Context) (gjson.Result, error)
}

// BotManager 机器人自身管理。
type BotManager interface {
	// GenerateURLLink 生成机器人分享链接，用于邀请用户添加机器人为好友。
	GenerateURLLink(ctx context.Context, req *dto.GenerateURLLinkRequest) (gjson.Result, error)
}

// GroupManager 群聊管理（2026-08 新增，部分接口仅白名单机器人可用）。
type GroupManager interface {
	// GetGroupInfo 获取群基本信息。
	GetGroupInfo(ctx context.Context, groupOpenID string) (gjson.Result, error)
	// GetGroupBotState 获取机器人群内状态。
	GetGroupBotState(ctx context.Context, groupOpenID string) (gjson.Result, error)
	// GetGroupJoinRequestList 拉取入群申请列表（cursor/limit 分页）。
	GetGroupJoinRequestList(ctx context.Context, groupOpenID, cursor string, limit int) (gjson.Result, error)
	// ApproveJoinRequest 审批入群申请（approve/decline）。
	ApproveJoinRequest(ctx context.Context, groupOpenID, memberOpenID string, req *dto.ApprovalJoinRequest) (gjson.Result, error)
	// GetGroupRestrictChatSetting 查询群禁言状态。
	GetGroupRestrictChatSetting(ctx context.Context, groupOpenID string) (gjson.Result, error)
	// SetGroupMemberMute 设置群成员禁言。
	SetGroupMemberMute(ctx context.Context, groupOpenID string, req *dto.SetRestrictChatSettingRequest) (gjson.Result, error)
	// GetJoinApprovalStrategyList 查询入群自动审批策略列表。
	GetJoinApprovalStrategyList(ctx context.Context, cursor string, limit int) (gjson.Result, error)
	// CreateJoinApprovalStrategy 创建入群自动审批策略。
	CreateJoinApprovalStrategy(ctx context.Context, req *dto.CreateJoinApprovalStrategyRequest) (gjson.Result, error)
	// UpdateJoinApprovalStrategy 修改入群自动审批策略。
	UpdateJoinApprovalStrategy(ctx context.Context, strategyID string, req *dto.UpdateJoinApprovalStrategyRequest) (gjson.Result, error)
	// DeleteJoinApprovalStrategy 删除入群自动审批策略。
	DeleteJoinApprovalStrategy(ctx context.Context, strategyID string) (gjson.Result, error)
	// ExecuteJoinApprovalStrategy 执行入群自动审批策略（异步）。
	ExecuteJoinApprovalStrategy(ctx context.Context, strategyID string) (gjson.Result, error)
	// UpdateJoinApprovalStrategyWhitelist 修改入群自动审批策略的白名单号码。
	UpdateJoinApprovalStrategyWhitelist(ctx context.Context, strategyID string, req *dto.UpdateWhitelistUsersRequest) (gjson.Result, error)
}

// ── 聚合接口 ────────────────────────────────────────────────────────────────

// OpenAPI 是所有 QQ 开放平台能力的完整聚合接口。
// 若调用方只需要其中一部分能力，可直接依赖对应的子接口（如 MessageSender、
// GuildManager 等），以降低 mock 成本并使依赖意图更清晰。
type OpenAPI interface {
	MessageSender
	MessageRecaller
	InteractionResponder
	ReactionManager
	GuildManager
	MemberManager
	RoleManager
	APIPermissionManager
	MuteManager
	AnnouncementManager
	PinManager
	ScheduleManager
	AudioManager
	ThreadManager
	GatewayManager
	MediaUploader
	GroupManager
	BotManager
}
