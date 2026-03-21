package intents

// Event is intent event name
type Event string

const (
	// GUILDS (1 << 0)

	GuildCreate   Event = "GUILD_CREATE"
	GuildUpdate   Event = "GUILD_UPDATE"
	GuildDelete   Event = "GUILD_DELETE"
	ChannelCreate Event = "CHANNEL_CREATE"
	ChannelUpdate Event = "CHANNEL_UPDATE"
	ChannelDelete Event = "CHANNEL_DELETE"

	// GUILD_MEMBERS (1 << 1)

	GuildMemberAdd    Event = "GUILD_MEMBER_ADD"
	GuildMemberUpdate Event = "GUILD_MEMBER_UPDATE"
	GuildMemberRemove Event = "GUILD_MEMBER_REMOVE"

	// GUILD_MESSAGES (1 << 9)

	MessageCreate Event = "MESSAGE_CREATE"
	MessageDelete Event = "MESSAGE_DELETE"

	// GUILD_MESSAGE_REACTIONS (1 << 10)

	MessageReactionAdd    Event = "MESSAGE_REACTION_ADD"
	MessageReactionRemove Event = "MESSAGE_REACTION_REMOVE"

	// DIRECT_MESSAGE (1 << 12)

	DirectMessageCreate Event = "DIRECT_MESSAGE_CREATE"
	DirectMessageDelete Event = "DIRECT_MESSAGE_DELETE"

	// GROUP_AND_C2C_EVENT (1 << 25)

	C2CMessageCreate     Event = "C2C_MESSAGE_CREATE"
	FriendAdd            Event = "FRIEND_ADD"
	FriendDel            Event = "FRIEND_DEL"
	C2CMessageReject     Event = "C2C_MSG_REJECT"
	C2CMessageReceive    Event = "C2C_MSG_RECEIVE"
	GroupAtMessageCreate Event = "GROUP_AT_MESSAGE_CREATE"
	GroupAddRobot        Event = "GROUP_ADD_ROBOT"
	GroupDelRobot        Event = "GROUP_DEL_ROBOT"
	GroupMessageReject   Event = "GROUP_MSG_REJECT"
	GroupMessageReceive  Event = "GROUP_MSG_RECEIVE"

	// INTERACTION (1 << 26)

	InteractionCreate Event = "INTERACTION_CREATE"

	// MESSAGE_AUDIT (1 << 27)

	MessageAuditPass   Event = "MESSAGE_AUDIT_PASS"
	MessageAuditReject Event = "MESSAGE_AUDIT_REJECT"

	// FORUMS_EVENT (1 << 28)

	ForumsThreadCreate      Event = "FORUMS_THREAD_CREATE"
	ForumsThreadUpdate      Event = "FORUMS_THREAD_UPDATE"
	ForumsThreadDelete      Event = "FORUMS_THREAD_DELETE"
	ForumPostCreate         Event = "FORUM_POST_CREATE"
	ForumPostDelete         Event = "FORUM_POST_DELETE"
	ForumReplyCreate        Event = "FORUM_REPLY_CREATE"
	ForumReplyDelete        Event = "FORUM_REPLY_DELETE"
	ForumPublishAuditResult Event = "FORUM_PUBLISH_AUDIT_RESULT"

	// AUDIO_ACTION (1 << 29)

	AudioStart  Event = "AUDIO_START"
	AudioFinish Event = "AUDIO_FINISH"
	AudioOnMic  Event = "AUDIO_ON_MIC"
	AudioOffMic Event = "AUDIO_OFF_MIC"

	// PUBLIC_GUILD_MESSAGES (1 << 30)

	AtMessageCreate     Event = "AT_MESSAGE_CREATE"
	PublicMessageDelete Event = "PUBLIC_MESSAGE_DELETE"
)
