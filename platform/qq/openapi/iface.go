package openapi

import (
	"context"

	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/tidwall/gjson"
)

// OpenAPI is the interface of the openapi
type OpenAPI interface {
	SingleChat(ctx context.Context, openid string, msg *dto.Message) (gjson.Result, error)          // SingleChat sends a message to the single chat
	GroupChat(ctx context.Context, groupID string, msg *dto.Message) (gjson.Result, error)          // GroupChat sends a message to the group chat
	ChannelChat(ctx context.Context, channelID string, msg *dto.GuildMessage) (gjson.Result, error) // ChannelChat sends a message to a guild text channel
	DMChat(ctx context.Context, guildID string, msg *dto.GuildMessage) (gjson.Result, error)        // DMChat sends a message to a guild direct message (DM) session
	SingleRichMedia(ctx context.Context, openid string, media *dto.Media) (gjson.Result, error)     // SingleRichMedia sends a rich media to the single chat
	GroupRichMedia(ctx context.Context, groupID string, media *dto.Media) (gjson.Result, error)     // GroupRichMedia sends a rich media to the group chat
	SingleReset(ctx context.Context, openid, messageID string) (gjson.Result, error)                // SingleReset resets a message in the single chat
	GroupReset(ctx context.Context, groupID, messageID string) (gjson.Result, error)                // GroupReset resets a message in the group chat
	ChannelReset(ctx context.Context, channelID, messageID string) (gjson.Result, error)            // ChannelReset resets a message in a text channel（仅私域机器人）
	DMReset(ctx context.Context, guildID, messageID string) (gjson.Result, error)                   // DMReset resets a message in a direct message channel（仅私域机器人）

	// ── 互动事件 ────────────────────────────────────────────────────────────

	// RespondInteraction 回应 INTERACTION_CREATE 按钮回调事件。
	// interactionID 来自事件的 id 字段；code 为结果码（0=成功）。
	// 必须在收到事件后尽快调用，否则客户端持续 loading 至超时。
	RespondInteraction(ctx context.Context, interactionID string, code int) (gjson.Result, error)

	// ── 频道管理（Channel Management）──────────────────────────────────────

	// GetMe 获取当前用户（机器人）详情。
	GetMe(ctx context.Context) (gjson.Result, error)
	// GetMyGuilds 获取机器人已加入的频道列表（分页）。
	// before/after 为翻页游标（guild_id），limit 为每页数量（默认100，最大100）。
	GetMyGuilds(ctx context.Context, before, after string, limit int) (gjson.Result, error)
	// GetGuild 获取指定频道详情。
	GetGuild(ctx context.Context, guildID string) (gjson.Result, error)
	// GetGuildChannels 获取频道下的子频道列表。
	GetGuildChannels(ctx context.Context, guildID string) (gjson.Result, error)
	// GetChannel 获取子频道详情。
	GetChannel(ctx context.Context, channelID string) (gjson.Result, error)
	// CreateGuildChannel 在频道内创建子频道（仅私域机器人）。
	CreateGuildChannel(ctx context.Context, guildID string, req *dto.ChannelRequest) (gjson.Result, error)
	// UpdateGuildChannel 修改子频道信息（仅私域机器人）。
	UpdateGuildChannel(ctx context.Context, channelID string, req *dto.ChannelRequest) (gjson.Result, error)
	// DeleteGuildChannel 删除子频道（仅私域机器人）。
	DeleteGuildChannel(ctx context.Context, channelID string) (gjson.Result, error)
	// CreateDirectMessageSession 创建频道私信会话（发送频道私信前必须先调用此接口获取 guild_id）。
	CreateDirectMessageSession(ctx context.Context, req *dto.DirectMessageSessionRequest) (gjson.Result, error)

	// ── 频道成员（仅私域机器人）──────────────────────────────────────────────

	// GetChannelOnlineNums 获取子频道在线成员数。
	GetChannelOnlineNums(ctx context.Context, channelID string) (gjson.Result, error)
	// GetGuildMembers 分页获取频道成员列表（after=上一页末尾 user_id，首页填 "0"；limit=1~400）。
	GetGuildMembers(ctx context.Context, guildID, after string, limit int) (gjson.Result, error)
	// GetGuildRoleMembers 获取频道指定身份组的成员列表（startIndex 为翻页游标，limit 为页大小）。
	GetGuildRoleMembers(ctx context.Context, guildID, roleID string, startIndex, limit uint32) (gjson.Result, error)
	// GetGuildMember 获取频道单个成员详情。
	GetGuildMember(ctx context.Context, guildID, userID string) (gjson.Result, error)
	// DeleteGuildMember 删除（踢出）频道成员。addBlacklist=true 同时加黑名单；deleteHistoryMsgDays 清除最近 N 天消息（0=不清除）。
	DeleteGuildMember(ctx context.Context, guildID, userID string, addBlacklist bool, deleteHistoryMsgDays int) (gjson.Result, error)

	// ── 频道身份组（仅私域机器人）───────────────────────────────────────────

	// GetGuildRoles 获取频道身份组列表。
	GetGuildRoles(ctx context.Context, guildID string) (gjson.Result, error)
	// CreateGuildRole 创建频道身份组。
	CreateGuildRole(ctx context.Context, guildID string, req *dto.GuildRoleRequest) (gjson.Result, error)
	// UpdateGuildRole 修改频道身份组信息。
	UpdateGuildRole(ctx context.Context, guildID, roleID string, req *dto.GuildRoleRequest) (gjson.Result, error)
	// DeleteGuildRole 删除频道身份组。
	DeleteGuildRole(ctx context.Context, guildID, roleID string) (gjson.Result, error)
	// AddGuildMemberRole 向身份组添加成员；channelID 仅私有身份组时传入。
	AddGuildMemberRole(ctx context.Context, guildID, userID, roleID, channelID string) (gjson.Result, error)
	// DeleteGuildMemberRole 从身份组移除成员；channelID 仅私有身份组时传入。
	DeleteGuildMemberRole(ctx context.Context, guildID, userID, roleID, channelID string) (gjson.Result, error)
	// GetChannelMemberPermissions 获取子频道内指定成员的权限。
	GetChannelMemberPermissions(ctx context.Context, channelID, userID string) (gjson.Result, error)
	// UpdateChannelMemberPermissions 修改子频道内指定成员的权限。
	UpdateChannelMemberPermissions(ctx context.Context, channelID, userID string, req *dto.PermissionRequest) (gjson.Result, error)
	// GetChannelRolePermissions 获取子频道内指定身份组的权限。
	GetChannelRolePermissions(ctx context.Context, channelID, roleID string) (gjson.Result, error)
	// UpdateChannelRolePermissions 修改子频道内指定身份组的权限。
	UpdateChannelRolePermissions(ctx context.Context, channelID, roleID string, req *dto.PermissionRequest) (gjson.Result, error)

	// ── 接口授权管理（仅私域机器人）─────────────────────────────────────────

	// GetGuildAPIPermissions 获取频道已授权的接口列表。
	GetGuildAPIPermissions(ctx context.Context, guildID string) (gjson.Result, error)
	// RequestGuildAPIPermission 发送接口权限授权链接到指定子频道。
	RequestGuildAPIPermission(ctx context.Context, guildID string, req *dto.APIPermissionDemandRequest) (gjson.Result, error)

	// ── 发言管理（禁言，仅私域机器人）──────────────────────────────────────

	// GetGuildMessageSetting 获取频道消息频率设置详情。
	GetGuildMessageSetting(ctx context.Context, guildID string) (gjson.Result, error)
	// MuteGuild 频道全员禁言（mute_seconds="0" 或 mute_end_timestamp="0" 解除禁言）。
	MuteGuild(ctx context.Context, guildID string, req *dto.MuteRequest) (gjson.Result, error)
	// MuteGuildMember 禁言频道指定成员。
	MuteGuildMember(ctx context.Context, guildID, userID string, req *dto.MuteRequest) (gjson.Result, error)
	// MuteGuildMultiMembers 批量禁言频道成员。
	MuteGuildMultiMembers(ctx context.Context, guildID string, req *dto.MultipleMuteRequest) (gjson.Result, error)

	// ── 内容管理：公告（仅私域机器人）──────────────────────────────────────

	// CreateGuildAnnounce 创建频道公告。
	CreateGuildAnnounce(ctx context.Context, guildID string, req *dto.CreateGuildAnnounceRequest) (gjson.Result, error)
	// DeleteGuildAnnounce 删除频道公告（messageID 传 "all" 可删除所有）。
	DeleteGuildAnnounce(ctx context.Context, guildID, messageID string) (gjson.Result, error)

	// ── 内容管理：精华消息（仅私域机器人）──────────────────────────────────

	// PinMessage 添加子频道精华消息。
	PinMessage(ctx context.Context, channelID, messageID string) (gjson.Result, error)
	// UnpinMessage 删除子频道精华消息（messageID 传 "all" 可删除所有）。
	UnpinMessage(ctx context.Context, channelID, messageID string) (gjson.Result, error)
	// GetPinnedMessages 获取子频道所有精华消息列表（最多 20 条）。
	GetPinnedMessages(ctx context.Context, channelID string) (gjson.Result, error)

	// ── 内容管理：日程（仅私域机器人）──────────────────────────────────────

	// GetChannelSchedules 获取子频道日程列表（since 为起始时间戳毫秒，0 表示不过滤）。
	GetChannelSchedules(ctx context.Context, channelID string, since uint64) (gjson.Result, error)
	// GetChannelSchedule 获取日程详情。
	GetChannelSchedule(ctx context.Context, channelID, scheduleID string) (gjson.Result, error)
	// CreateChannelSchedule 创建日程。
	CreateChannelSchedule(ctx context.Context, channelID string, req *dto.ScheduleRequest) (gjson.Result, error)
	// UpdateChannelSchedule 修改日程。
	UpdateChannelSchedule(ctx context.Context, channelID, scheduleID string, req *dto.ScheduleRequest) (gjson.Result, error)
	// DeleteChannelSchedule 删除日程。
	DeleteChannelSchedule(ctx context.Context, channelID, scheduleID string) (gjson.Result, error)

	// ── 内容管理：音频（仅私域机器人）──────────────────────────────────────

	// AudioControl 音频控制（播放/暂停/继续/停止）。
	AudioControl(ctx context.Context, channelID string, req *dto.AudioControlRequest) (gjson.Result, error)
	// BotOnMic 机器人上麦。
	BotOnMic(ctx context.Context, channelID string) (gjson.Result, error)
	// BotOffMic 机器人下麦。
	BotOffMic(ctx context.Context, channelID string) (gjson.Result, error)

	// ── 内容管理：论坛帖子（仅私域机器人）──────────────────────────────────

	// GetThreadList 获取子频道帖子列表。
	GetThreadList(ctx context.Context, channelID string) (gjson.Result, error)
	// GetThread 获取帖子详情。
	GetThread(ctx context.Context, channelID, threadID string) (gjson.Result, error)
	// CreateThread 发表帖子。
	CreateThread(ctx context.Context, channelID string, req *dto.ThreadRequest) (gjson.Result, error)
	// DeleteThread 删除帖子。
	DeleteThread(ctx context.Context, channelID, threadID string) (gjson.Result, error)
}
