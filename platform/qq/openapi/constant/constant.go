package constant

const (
	// AccessTokenURL is the url to get access token
	//
	// https://bot.q.qq.com/wiki/develop/api-v2/dev-prepare/interface-framework/api-use.html#%E8%8E%B7%E5%8F%96%E8%B0%83%E7%94%A8%E5%87%AD%E8%AF%81
	AccessTokenURL = "https://bots.qq.com/app/getAppAccessToken"
	// OpenAPIURL is the base url of qq bot openapi
	//
	// should fill authorization header with access token like: QQBot {access_token}
	//
	// https://bot.q.qq.com/wiki/develop/api-v2/dev-prepare/interface-framework/api-use.html#%E9%89%B4%E6%9D%83%E6%96%B9%E5%BC%8F
	OpenAPIURL = "https://api.sgroup.qq.com"
	// GatewayURL is the url to get websocket gateway
	//
	// must fill Content-Type with application/json
	//
	// https://bot.q.qq.com/wiki/develop/api-v2/openapi/wss/url_get.html
	GatewayURL = OpenAPIURL + "/gateway"

	// SingleChatURL /v2/users/{openid}/messages
	//
	// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/send-receive/send.html#%E5%8D%95%E8%81%8A
	SingleChatURL = OpenAPIURL + "/v2/users/%s/messages"

	// GroupChatURL /v2/groups/{group_openid}/messages
	//
	// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/send-receive/send.html#%E7%BE%A4%E8%81%8A
	GroupChatURL = OpenAPIURL + "/v2/groups/%s/messages"

	// SingleRichMediaURL /v2/users/{openid}/files
	//
	// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/send-receive/rich-media.html#%E7%94%A8%E4%BA%8E%E5%8D%95%E8%81%8A
	SingleRichMediaURL = OpenAPIURL + "/v2/users/%s/files"

	// GroupRichMediaURL /v2/groups/{group_openid}/files
	//
	// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/send-receive/rich-media.html#%E7%94%A8%E4%BA%8E%E7%BE%A4%E8%81%8A
	GroupRichMediaURL = OpenAPIURL + "/v2/groups/%s/files"

	// SingleResetURL /v2/users/{openid}/messages/{msg_id}
	//
	// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/send-receive/reset.html#%E5%8D%95%E8%81%8A
	SingleResetURL = OpenAPIURL + "/v2/users/%s/messages/%s"

	// GroupResetURL /v2/groups/{group_openid}/messages/{msg_id}
	//
	// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/send-receive/reset.html#%E7%BE%A4%E8%81%8A
	GroupResetURL = OpenAPIURL + "/v2/groups/%s/messages/%s"

	// ChannelChatURL POST /channels/{channel_id}/messages
	//
	// 发送消息到文字子频道，使用频道专属请求体（GuildMessage），
	// 与群聊/单聊 API 的请求结构体完全不同。
	//
	// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/post_messages.html
	ChannelChatURL = OpenAPIURL + "/channels/%s/messages"

	// ChannelResetURL DELETE /channels/{channel_id}/messages/{message_id}?hidetip=false
	//
	// 撤回文字子频道消息（仅私域机器人可用）。
	// hidetip: true=隐藏灰条提示，false=显示（默认）。
	//
	// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/send-receive/reset.html#%E6%96%87%E5%AD%97%E5%AD%90%E9%A2%91%E9%81%93
	ChannelResetURL = OpenAPIURL + "/channels/%s/messages/%s"

	// DMResetURL DELETE /dms/{guild_id}/messages/{message_id}?hidetip=false
	//
	// 撤回频道私信消息（仅私域机器人可用，只能撤回机器人自己发送的私信）。
	// hidetip: true=隐藏灰条提示，false=显示（默认）。
	//
	// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/send-receive/reset.html#%E9%A2%91%E9%81%93%E7%A7%81%E4%BF%A1
	DMResetURL = OpenAPIURL + "/dms/%s/messages/%s"

	// ── 频道成员 ──────────────────────────────────────────────────────────────

	// ChannelOnlineNumsURL GET /channels/{channel_id}/online_nums
	//
	// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/channel/role/get_online_nums.html
	ChannelOnlineNumsURL = OpenAPIURL + "/channels/%s/online_nums"

	// GuildMembersURL GET /guilds/{guild_id}/members
	//
	// 分页查询频道成员列表（仅私域机器人）。
	// 查询参数：after=上一页最后一个 user_id（首页填 0），limit=1~400。
	//
	// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/channel/role/member/get_members.html
	GuildMembersURL = OpenAPIURL + "/guilds/%s/members"

	// GuildRoleMembersURL GET /guilds/{guild_id}/roles/{role_id}/members
	//
	// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/channel/role/member/get_role_members.html
	GuildRoleMembersURL = OpenAPIURL + "/guilds/%s/roles/%s/members"

	// GuildMemberURL GET|DELETE /guilds/{guild_id}/members/{user_id}
	//
	// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/channel/role/member/get_member.html
	GuildMemberURL = OpenAPIURL + "/guilds/%s/members/%s"

	// ── 频道身份组与权限管理 ──────────────────────────────────────────────────

	// GuildRolesURL GET|POST /guilds/{guild_id}/roles
	//
	// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/channel/role-group/get_guild_roles.html
	GuildRolesURL = OpenAPIURL + "/guilds/%s/roles"

	// GuildRoleURL PATCH|DELETE /guilds/{guild_id}/roles/{role_id}
	//
	// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/channel/role-group/patch_guild_role.html
	GuildRoleURL = OpenAPIURL + "/guilds/%s/roles/%s"

	// GuildMemberRoleURL PUT|DELETE /guilds/{guild_id}/members/{user_id}/roles/{role_id}
	//
	// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/channel/role-group/put_guild_member_role.html
	GuildMemberRoleURL = OpenAPIURL + "/guilds/%s/members/%s/roles/%s"

	// ChannelMemberPermURL GET|PUT /channels/{channel_id}/members/{user_id}/permissions
	//
	// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/channel/role-group/channel_permissions/get_channel_permissions.html
	ChannelMemberPermURL = OpenAPIURL + "/channels/%s/members/%s/permissions"

	// ChannelRolePermURL GET|PUT /channels/{channel_id}/roles/{role_id}/permissions
	//
	// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/channel/role-group/channel_permissions/get_channel_roles_permissions.html
	ChannelRolePermURL = OpenAPIURL + "/channels/%s/roles/%s/permissions"

	// ── 接口授权管理 ──────────────────────────────────────────────────────────

	// GuildAPIPermissionsURL GET /guilds/{guild_id}/api_permission
	//
	// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/channel/api_permissions/
	GuildAPIPermissionsURL = OpenAPIURL + "/guilds/%s/api_permission"

	// GuildAPIPermDemandURL POST /guilds/{guild_id}/api_permission/demand
	//
	// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/channel/api_permissions/
	GuildAPIPermDemandURL = OpenAPIURL + "/guilds/%s/api_permission/demand"

	// ── 发言管理（禁言）──────────────────────────────────────────────────────

	// GuildMessageSettingURL GET /guilds/{guild_id}/message/setting
	//
	// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/channel/speak/setting/message_setting.html
	GuildMessageSettingURL = OpenAPIURL + "/guilds/%s/message/setting"

	// GuildMuteURL PATCH /guilds/{guild_id}/mute（全员 / 批量成员）
	//
	// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/channel/speak/patch_guild_mute.html
	GuildMuteURL = OpenAPIURL + "/guilds/%s/mute"

	// GuildMemberMuteURL PATCH /guilds/{guild_id}/members/{user_id}/mute
	//
	// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/channel/speak/patch_guild_member_mute.html
	GuildMemberMuteURL = OpenAPIURL + "/guilds/%s/members/%s/mute"

	// ── 内容管理：公告 ────────────────────────────────────────────────────────

	// GuildAnnouncesURL POST /guilds/{guild_id}/announces
	//
	// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/channel/content/announces/post_guild_announces.html
	GuildAnnouncesURL = OpenAPIURL + "/guilds/%s/announces"

	// GuildAnnounceURL DELETE /guilds/{guild_id}/announces/{message_id}
	//
	// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/channel/content/announces/delete_guild_announces.html
	GuildAnnounceURL = OpenAPIURL + "/guilds/%s/announces/%s"

	// ── 内容管理：精华消息 ────────────────────────────────────────────────────

	// ChannelPinURL PUT|DELETE /channels/{channel_id}/pins/{message_id}
	//
	// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/channel/content/pins/put_pins_message.html
	ChannelPinURL = OpenAPIURL + "/channels/%s/pins/%s"

	// ChannelPinsURL GET /channels/{channel_id}/pins
	//
	// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/channel/content/pins/get_pins_message.html
	ChannelPinsURL = OpenAPIURL + "/channels/%s/pins"

	// ── 内容管理：日程 ────────────────────────────────────────────────────────

	// ChannelSchedulesURL GET|POST /channels/{channel_id}/schedules
	//
	// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/channel/content/schedule/get_schedules.html
	ChannelSchedulesURL = OpenAPIURL + "/channels/%s/schedules"

	// ChannelScheduleURL GET|PATCH|DELETE /channels/{channel_id}/schedules/{schedule_id}
	//
	// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/channel/content/schedule/get_schedule.html
	ChannelScheduleURL = OpenAPIURL + "/channels/%s/schedules/%s"

	// ── 内容管理：音频 ────────────────────────────────────────────────────────

	// ChannelAudioURL POST /channels/{channel_id}/audio
	//
	// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/channel/content/audio/audio_control.html
	ChannelAudioURL = OpenAPIURL + "/channels/%s/audio"

	// ChannelMicURL PUT|DELETE /channels/{channel_id}/mic
	//
	// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/channel/content/audio/put_mic.html
	ChannelMicURL = OpenAPIURL + "/channels/%s/mic"

	// ── 内容管理：论坛帖子 ────────────────────────────────────────────────────

	// ChannelThreadsURL GET /channels/{channel_id}/threads
	//
	// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/channel/content/forum/get_threads_list.html
	ChannelThreadsURL = OpenAPIURL + "/channels/%s/threads"

	// ChannelThreadURL GET|PUT|DELETE /channels/{channel_id}/threads/{thread_id}
	//
	// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/channel/content/forum/get_thread.html
	ChannelThreadURL = OpenAPIURL + "/channels/%s/threads/%s"

	// ── 互动事件回应 ──────────────────────────────────────────────────────────

	// InteractionURL PUT /interactions/{interaction_id}
	//
	// 回应按钮互动事件，必须在收到 INTERACTION_CREATE 后调用，否则客户端持续 loading。
	// body: {"code": 0}（0=成功，1=操作失败，2=频繁，3=重复，4=无权限，5=仅管理员）
	//
	// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/trans/msg-btn.html#%E5%9B%9E%E5%BA%94
	InteractionURL = OpenAPIURL + "/interactions/%s"
)
