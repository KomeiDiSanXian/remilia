package constant

// 本文档核验说明
//
// 本文件中的 URL 模板断言均对照腾讯官方文档：
//
//	https://bot.q.qq.com/wiki/develop/api-v2/
//
// 每个常量的行内注释已附对应的官方文档链接；分片上传端点
// （upload_prepare / upload_part_finish）为下划线路径，参见
//
//	/wiki/develop/api-v2/autogen/api/v2_users_user_id_upload_prepare.post.html

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestURLConsistency 验证所有 URL 常量均由 OpenAPIURL 基址拼接而成，
// 防止拼写错误导致请求打到错误的域名。
func TestURLConsistency(t *testing.T) {
	const base = "https://api.sgroup.qq.com"
	assert.Equal(t, base, OpenAPIURL)
	assert.Equal(t, "https://bots.qq.com/app/getAppAccessToken", AccessTokenURL)
	assert.Equal(t, base+"/gateway", GatewayURL)
	assert.Equal(t, base+"/gateway/bot", GatewayBotURL)
}

// TestURLTemplateFormatting 验证带占位符的 URL 模板经过 fmt.Sprintf
// 格式化后能得到正确的接口路径。
func TestURLTemplateFormatting(t *testing.T) {
	tests := []struct {
		name string
		url  string
		args []any
		want string
	}{
		{"single chat", SingleChatURL, []any{"openid_1"}, "https://api.sgroup.qq.com/v2/users/openid_1/messages"},
		{"group chat", GroupChatURL, []any{"gid_1"}, "https://api.sgroup.qq.com/v2/groups/gid_1/messages"},
		{"single rich media", SingleRichMediaURL, []any{"openid_1"}, "https://api.sgroup.qq.com/v2/users/openid_1/files"},
		{"group rich media", GroupRichMediaURL, []any{"gid_1"}, "https://api.sgroup.qq.com/v2/groups/gid_1/files"},
		{"single reset", SingleResetURL, []any{"openid_1", "msg_1"}, "https://api.sgroup.qq.com/v2/users/openid_1/messages/msg_1"},
		{"group reset", GroupResetURL, []any{"gid_1", "msg_1"}, "https://api.sgroup.qq.com/v2/groups/gid_1/messages/msg_1"},
		{"channel chat", ChannelChatURL, []any{"chan_1"}, "https://api.sgroup.qq.com/channels/chan_1/messages"},
		{"channel reset", ChannelResetURL, []any{"chan_1", "msg_1"}, "https://api.sgroup.qq.com/channels/chan_1/messages/msg_1"},
		{"dm reset", DMResetURL, []any{"dm_1", "msg_1"}, "https://api.sgroup.qq.com/dms/dm_1/messages/msg_1"},
		{"dm chat", DMChatURL, []any{"dm_1"}, "https://api.sgroup.qq.com/dms/dm_1/messages"},
		{"channel online nums", ChannelOnlineNumsURL, []any{"chan_1"}, "https://api.sgroup.qq.com/channels/chan_1/online_nums"},
		{"guild members", GuildMembersURL, []any{"guild_1"}, "https://api.sgroup.qq.com/guilds/guild_1/members"},
		{"guild role members", GuildRoleMembersURL, []any{"guild_1", "role_1"}, "https://api.sgroup.qq.com/guilds/guild_1/roles/role_1/members"},
		{"guild member", GuildMemberURL, []any{"guild_1", "user_1"}, "https://api.sgroup.qq.com/guilds/guild_1/members/user_1"},
		{"guild roles", GuildRolesURL, []any{"guild_1"}, "https://api.sgroup.qq.com/guilds/guild_1/roles"},
		{"guild role", GuildRoleURL, []any{"guild_1", "role_1"}, "https://api.sgroup.qq.com/guilds/guild_1/roles/role_1"},
		{"guild member role", GuildMemberRoleURL, []any{"guild_1", "user_1", "role_1"}, "https://api.sgroup.qq.com/guilds/guild_1/members/user_1/roles/role_1"},
		{"channel member perm", ChannelMemberPermURL, []any{"chan_1", "user_1"}, "https://api.sgroup.qq.com/channels/chan_1/members/user_1/permissions"},
		{"channel role perm", ChannelRolePermURL, []any{"chan_1", "role_1"}, "https://api.sgroup.qq.com/channels/chan_1/roles/role_1/permissions"},
		{"guild api permissions", GuildAPIPermissionsURL, []any{"guild_1"}, "https://api.sgroup.qq.com/guilds/guild_1/api_permission"},
		{"guild api perm demand", GuildAPIPermDemandURL, []any{"guild_1"}, "https://api.sgroup.qq.com/guilds/guild_1/api_permission/demand"},
		{"guild message setting", GuildMessageSettingURL, []any{"guild_1"}, "https://api.sgroup.qq.com/guilds/guild_1/message/setting"},
		{"guild mute", GuildMuteURL, []any{"guild_1"}, "https://api.sgroup.qq.com/guilds/guild_1/mute"},
		{"guild member mute", GuildMemberMuteURL, []any{"guild_1", "user_1"}, "https://api.sgroup.qq.com/guilds/guild_1/members/user_1/mute"},
		{"guild announces", GuildAnnouncesURL, []any{"guild_1"}, "https://api.sgroup.qq.com/guilds/guild_1/announces"},
		{"guild announce", GuildAnnounceURL, []any{"guild_1", "msg_1"}, "https://api.sgroup.qq.com/guilds/guild_1/announces/msg_1"},
		{"channel pin", ChannelPinURL, []any{"chan_1", "msg_1"}, "https://api.sgroup.qq.com/channels/chan_1/pins/msg_1"},
		{"channel pins", ChannelPinsURL, []any{"chan_1"}, "https://api.sgroup.qq.com/channels/chan_1/pins"},
		{"channel schedules", ChannelSchedulesURL, []any{"chan_1"}, "https://api.sgroup.qq.com/channels/chan_1/schedules"},
		{"channel schedule", ChannelScheduleURL, []any{"chan_1", "sched_1"}, "https://api.sgroup.qq.com/channels/chan_1/schedules/sched_1"},
		{"channel audio", ChannelAudioURL, []any{"chan_1"}, "https://api.sgroup.qq.com/channels/chan_1/audio"},
		{"channel mic", ChannelMicURL, []any{"chan_1"}, "https://api.sgroup.qq.com/channels/chan_1/mic"},
		{"channel threads", ChannelThreadsURL, []any{"chan_1"}, "https://api.sgroup.qq.com/channels/chan_1/threads"},
		{"channel thread", ChannelThreadURL, []any{"chan_1", "thread_1"}, "https://api.sgroup.qq.com/channels/chan_1/threads/thread_1"},
		{"interaction", InteractionURL, []any{"inter_1"}, "https://api.sgroup.qq.com/interactions/inter_1"},
		{"channel message reaction", ChannelMessageReactionURL, []any{"chan_1", "msg_1", 1, "21"}, "https://api.sgroup.qq.com/channels/chan_1/messages/msg_1/reactions/1/21"},
		{"users me", UsersMeURL, nil, "https://api.sgroup.qq.com/users/@me"},
		{"users me guilds", UsersMeGuildsURL, nil, "https://api.sgroup.qq.com/users/@me/guilds"},
		{"users me dms", UsersMeDMsURL, nil, "https://api.sgroup.qq.com/users/@me/dms"},
		{"guild", GuildURL, []any{"guild_1"}, "https://api.sgroup.qq.com/guilds/guild_1"},
		{"guild channels", GuildChannelsURL, []any{"guild_1"}, "https://api.sgroup.qq.com/guilds/guild_1/channels"},
		{"channel", ChannelURL, []any{"chan_1"}, "https://api.sgroup.qq.com/channels/chan_1"},
		{"user upload prepare", UserUploadPrepareURL, []any{"openid_1"}, "https://api.sgroup.qq.com/v2/users/openid_1/upload_prepare"},
		{"group upload prepare", GroupUploadPrepareURL, []any{"gid_1"}, "https://api.sgroup.qq.com/v2/groups/gid_1/upload_prepare"},
		{"user upload part finish", UserUploadPartFinishURL, []any{"openid_1"}, "https://api.sgroup.qq.com/v2/users/openid_1/upload_part_finish"},
		{"group upload part finish", GroupUploadPartFinishURL, []any{"gid_1"}, "https://api.sgroup.qq.com/v2/groups/gid_1/upload_part_finish"},
		{"stream single chat", StreamSingleChatURL, []any{"openid_1"}, "https://api.sgroup.qq.com/v2/users/openid_1/stream_messages"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, fmt.Sprintf(tt.url, tt.args...))
		})
	}
}

// TestReactionURLIntegerFormat 验证表情表态 URL 中 %d 占位符的整形参数。
func TestReactionURLIntegerFormat(t *testing.T) {
	u := fmt.Sprintf(ChannelMessageReactionURL, "chan_1", "msg_1", 2, "👍")
	assert.Equal(t, "https://api.sgroup.qq.com/channels/chan_1/messages/msg_1/reactions/2/👍", u)
}
