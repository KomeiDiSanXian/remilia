package intents

type Intents uint64

const (
	Guilds Intents = 1 << iota
	GuildMembers
	GuildMessages         = 1 << 9
	GuildMessageReactions = 1 << 10
	DirectMessage         = 1 << 12
	GroupAndC2CEvent      = 1 << 25
	Interaction           = 1 << 26
	Audit                 = 1 << 27
	ForumsEvent           = 1 << 28
	AudioAction           = 1 << 29
	PublicGuildMessages   = 1 << 30

	PrivateAll = Guilds | GuildMembers | GuildMessages | GuildMessageReactions | DirectMessage | GroupAndC2CEvent | Interaction | Audit | ForumsEvent | AudioAction | PublicGuildMessages
	PublicAll  = Guilds | GuildMembers | GuildMessageReactions | DirectMessage | GroupAndC2CEvent | Interaction | Audit | AudioAction | PublicGuildMessages
)

var Name = map[Intents]string{
	Guilds:                "Guilds",
	GuildMembers:          "GuildMembers",
	GuildMessages:         "GuildMessages",
	GuildMessageReactions: "GuildMessageReactions",
	DirectMessage:         "DirectMessage",
	GroupAndC2CEvent:      "GroupAndC2CEvent",
	Interaction:           "Interaction",
	Audit:                 "MessageAudit",
	ForumsEvent:           "ForumsEvent",
	AudioAction:           "AudioAction",
	PublicGuildMessages:   "PublicGuildMessages",
	PrivateAll:            "PrivateAll",
	PublicAll:             "PublicAll",
}
