package discord

import (
	"fmt"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/bwmarrin/discordgo"
)

// ────────────────────────────────────────────────────────────────────────────
// discordEvent
// ────────────────────────────────────────────────────────────────────────────

// discordEvent stores parsed Discord event data as a platform.Event.
//
// Implements the core platform.Event interface plus the optional extension
// interfaces: RawEvent, ReplyEvent, EditableEvent, MentionsEvent.
type discordEvent struct {
	kind          platform.EventKind
	senderInfo    platform.UserInfo
	chat          platform.ChatInfo
	segments      []platform.Segment
	timestamp     time.Time
	id            string
	rawType       string
	rawPayload    any
	isEdited      bool
	origTimestamp time.Time
	mentions      []platform.UserInfo
	// botID 机器人自身 ID（适配器注入），用于 Mentions() 的 IsSelf 判定。
	botID string
}

// ── platform.Event ──────────────────────────────────────────────────────────

func (e *discordEvent) Platform() string             { return PlatformID }
func (e *discordEvent) Kind() platform.EventKind     { return e.kind }
func (e *discordEvent) ID() string                   { return e.id }
func (e *discordEvent) Sender() platform.UserInfo    { return e.senderInfo }
func (e *discordEvent) Chat() platform.ChatInfo      { return e.chat }
func (e *discordEvent) Timestamp() time.Time         { return e.timestamp }
func (e *discordEvent) Segments() []platform.Segment { return e.segments }
func (e *discordEvent) Content() string              { return platform.SegmentsContent(e.segments) }
func (e *discordEvent) Attachments() []platform.Attachment {
	return platform.SegmentsAttachments(e.segments)
}

// ── platform.RawEvent ───────────────────────────────────────────────────────

func (e *discordEvent) RawType() string { return e.rawType }
func (e *discordEvent) RawPayload() any { return e.rawPayload }

// ── platform.ReplyEvent ─────────────────────────────────────────────────────
//
// Reply 单一真相源：委托段查找。

func (e *discordEvent) ReplyToID() string { return platform.SegmentsReplyToID(e.segments) }

// ── platform.EditableEvent ──────────────────────────────────────────────────

func (e *discordEvent) IsEdited() bool               { return e.isEdited }
func (e *discordEvent) OriginalTimestamp() time.Time { return e.origTimestamp }

// ── platform.MentionsEvent ──────────────────────────────────────────────────

// Mentions 返回消息中 @ 的用户列表；命中 botID 的条目标记 IsSelf=true
// （@ 机器人自身可被 OnMentionedBot 感知）。
func (e *discordEvent) Mentions() []platform.UserInfo {
	if len(e.mentions) == 0 {
		return nil
	}
	if e.botID == "" {
		return e.mentions
	}
	for i := range e.mentions {
		if e.mentions[i].ID == e.botID {
			e.mentions[i].IsSelf = true
		}
	}
	return e.mentions
}

// ────────────────────────────────────────────────────────────────────────────
// Helper converters
// ────────────────────────────────────────────────────────────────────────────

// userFromDiscord converts a *discordgo.User to platform.UserInfo.
func userFromDiscord(u *discordgo.User) platform.UserInfo {
	if u == nil {
		return platform.UserInfo{}
	}
	name := u.GlobalName
	if name == "" {
		name = u.Username
	}
	return platform.UserInfo{
		ID:          u.ID,
		DisplayName: name,
		IsBot:       u.Bot,
	}
}

// memberFromDiscord converts a *discordgo.Member to platform.UserInfo.
//
// Prefers the guild nickname; falls back to GlobalName then Username.
// GroupRole is inferred from Member.Permissions:
//   - PermissionAdministrator set → GroupRoleOwner
//   - PermissionManageGuild set  → GroupRoleAdmin
//   - otherwise                  → GroupRoleMember
func memberFromDiscord(m *discordgo.Member) platform.UserInfo {
	if m == nil {
		return platform.UserInfo{}
	}
	name := m.Nick
	var id string
	var isBot bool
	if m.User != nil {
		id = m.User.ID
		isBot = m.User.Bot
		if name == "" {
			name = m.User.GlobalName
		}
		if name == "" {
			name = m.User.Username
		}
	}
	// Infer group role from computed permissions (populated by Discord for guild messages
	// and interaction events; defaults to GroupRoleMember when not provided).
	role := platform.GroupRoleMember
	if m.Permissions&discordgo.PermissionAdministrator != 0 {
		role = platform.GroupRoleOwner
	} else if m.Permissions&discordgo.PermissionManageGuild != 0 {
		role = platform.GroupRoleAdmin
	}
	return platform.UserInfo{
		ID:          id,
		DisplayName: name,
		IsBot:       isBot,
		GroupRole:   role,
	}
}

// mentionsFromUsers converts []*discordgo.User to []platform.UserInfo.
func mentionsFromUsers(users []*discordgo.User) []platform.UserInfo {
	if len(users) == 0 {
		return nil
	}
	result := make([]platform.UserInfo, 0, len(users))
	for _, u := range users {
		if u != nil {
			result = append(result, userFromDiscord(u))
		}
	}
	return result
}

// ────────────────────────────────────────────────────────────────────────────
// Message events
// ────────────────────────────────────────────────────────────────────────────

// NewMessageCreateEvent converts a *discordgo.MessageCreate to platform.Event.
//
// EventKind:
//   - EventKindPrivateMessage  — DM channel (GuildID == "")
//   - EventKindGuildMessage    — guild channel (GuildID != "")
func NewMessageCreateEvent(m *discordgo.MessageCreate) platform.Event {
	return newMessageCreateEventWithBot(m, "")
}

// NewMessageCreateEventWithBot 与 NewMessageCreateEvent 相同，额外注入机器人
// 自身 ID 用于 Mentions() 的 IsSelf 判定。
func NewMessageCreateEventWithBot(m *discordgo.MessageCreate, botID string) platform.Event {
	return newMessageCreateEventWithBot(m, botID)
}

func newMessageCreateEventWithBot(m *discordgo.MessageCreate, botID string) platform.Event {
	e := &discordEvent{rawType: "MESSAGE_CREATE", rawPayload: m, botID: botID}
	if m.Message == nil {
		e.kind = platform.EventKindUnknown
		return e
	}

	isDM := m.GuildID == ""
	if isDM {
		e.kind = platform.EventKindPrivateMessage
	} else {
		e.kind = platform.EventKindGuildMessage
	}

	e.id = m.ID
	e.senderInfo = userFromDiscord(m.Author)
	e.chat = platform.ChatInfo{
		ID:       m.ChannelID,
		ParentID: m.GuildID,
		IsGroup:  !isDM,
		IsDM:     isDM,
	}

	e.timestamp = m.Timestamp
	e.segments = buildDiscordSegments(m.Message)
	e.mentions = mentionsFromUsers(m.Mentions)
	return e
}

// NewMessageUpdateEvent converts a *discordgo.MessageUpdate to platform.Event.
//
// EventKind: EventKindMessageUpdate
func NewMessageUpdateEvent(m *discordgo.MessageUpdate) platform.Event {
	return newMessageUpdateEventWithBot(m, "")
}

// NewMessageUpdateEventWithBot 与 NewMessageUpdateEvent 相同，额外注入机器人
// 自身 ID 用于 Mentions() 的 IsSelf 判定。
func NewMessageUpdateEventWithBot(m *discordgo.MessageUpdate, botID string) platform.Event {
	return newMessageUpdateEventWithBot(m, botID)
}

func newMessageUpdateEventWithBot(m *discordgo.MessageUpdate, botID string) platform.Event {
	e := &discordEvent{
		rawType:    "MESSAGE_UPDATE",
		rawPayload: m,
		kind:       platform.EventKindMessageUpdate,
		isEdited:   true,
		botID:      botID,
	}
	if m.Message == nil {
		return e
	}
	e.id = m.ID
	e.senderInfo = userFromDiscord(m.Author)
	isDM := m.GuildID == ""
	e.chat = platform.ChatInfo{
		ID:       m.ChannelID,
		ParentID: m.GuildID,
		IsGroup:  !isDM,
		IsDM:     isDM,
	}

	if m.EditedTimestamp != nil {
		e.timestamp = *m.EditedTimestamp
	}
	e.origTimestamp = m.Timestamp
	e.segments = buildDiscordSegments(m.Message)
	e.mentions = mentionsFromUsers(m.Mentions)
	return e
}

// NewMessageDeleteEvent converts a *discordgo.MessageDelete to platform.Event.
//
// EventKind: EventKindMessageDelete
func NewMessageDeleteEvent(m *discordgo.MessageDelete) platform.Event {
	isDM := m.GuildID == ""
	return &discordEvent{
		rawType:    "MESSAGE_DELETE",
		rawPayload: m,
		kind:       platform.EventKindMessageDelete,
		id:         m.ID,
		chat: platform.ChatInfo{
			ID:       m.ChannelID,
			ParentID: m.GuildID,
			IsGroup:  !isDM,
			IsDM:     isDM,
		},
		timestamp: time.Now(),
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Interaction events
// ────────────────────────────────────────────────────────────────────────────

// NewInteractionCreateEvent converts a *discordgo.InteractionCreate to platform.Event.
//
// EventKind: EventKindInteraction
//
// ChatInfo.Tokens contains:
//   - TokenInteractionID    — interaction ID (for sender lookup)
//   - TokenInteractionToken — interaction token (for follow-up messages)
func NewInteractionCreateEvent(i *discordgo.InteractionCreate) platform.Event {
	e := &discordEvent{
		rawType:    "INTERACTION_CREATE",
		rawPayload: i,
		kind:       platform.EventKindInteraction,
		id:         i.ID,
		timestamp:  time.Now(),
	}

	if i.Member != nil {
		e.senderInfo = memberFromDiscord(i.Member)
	} else if i.User != nil {
		e.senderInfo = userFromDiscord(i.User)
	}

	isDM := i.GuildID == ""
	e.chat = platform.ChatInfo{
		ID:       i.ChannelID,
		ParentID: i.GuildID,
		IsGroup:  !isDM,
		IsDM:     isDM,
		Tokens: map[string]string{
			TokenInteractionID:    i.ID,
			TokenInteractionToken: i.Token,
		},
	}

	// Extract human-readable content from interaction data.
	switch i.Type {
	case discordgo.InteractionApplicationCommand:
		data := i.ApplicationCommandData()
		var sb strings.Builder
		sb.WriteString("/")
		sb.WriteString(data.Name)
		for _, opt := range data.Options {
			fmt.Fprintf(&sb, " %s:%v", opt.Name, opt.Value)
		}
		e.segments = []platform.Segment{{Type: platform.SegmentText, Text: sb.String()}}

	case discordgo.InteractionMessageComponent:
		e.segments = []platform.Segment{{Type: platform.SegmentText, Text: i.MessageComponentData().CustomID}}

	case discordgo.InteractionModalSubmit:
		e.segments = []platform.Segment{{Type: platform.SegmentText, Text: i.ModalSubmitData().CustomID}}
	}

	return e
}

// ────────────────────────────────────────────────────────────────────────────
// Guild lifecycle events
// ────────────────────────────────────────────────────────────────────────────

// NewGuildCreateEvent converts a *discordgo.GuildCreate to platform.Event.
//
// EventKind: EventKindBotAdded (bot joined the guild)
func NewGuildCreateEvent(g *discordgo.GuildCreate) platform.Event {
	return &discordEvent{
		rawType:    "GUILD_CREATE",
		rawPayload: g,
		kind:       platform.EventKindBotAdded,
		id:         g.ID,
		chat: platform.ChatInfo{
			ID:      g.ID,
			Name:    g.Name,
			IsGroup: true,
		},
		timestamp: time.Now(),
	}
}

// NewGuildDeleteEvent converts a *discordgo.GuildDelete to platform.Event.
//
// EventKind: EventKindBotRemoved (bot left / was kicked from the guild)
func NewGuildDeleteEvent(g *discordgo.GuildDelete) platform.Event {
	return &discordEvent{
		rawType:    "GUILD_DELETE",
		rawPayload: g,
		kind:       platform.EventKindBotRemoved,
		id:         g.ID,
		chat: platform.ChatInfo{
			ID:      g.ID,
			IsGroup: true,
		},
		timestamp: time.Now(),
	}
}

// NewGuildUpdateEvent converts a *discordgo.GuildUpdate to platform.Event.
//
// EventKind: EventKindGuildChange (guild settings changed)
func NewGuildUpdateEvent(g *discordgo.GuildUpdate) platform.Event {
	return &discordEvent{
		rawType:    "GUILD_UPDATE",
		rawPayload: g,
		kind:       platform.EventKindGuildChange,
		id:         g.ID,
		chat: platform.ChatInfo{
			ID:      g.ID,
			Name:    g.Name,
			IsGroup: true,
		},
		timestamp: time.Now(),
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Guild member events
// ────────────────────────────────────────────────────────────────────────────

// NewGuildMemberAddEvent converts a *discordgo.GuildMemberAdd to platform.Event.
//
// EventKind: EventKindMemberJoin
func NewGuildMemberAddEvent(m *discordgo.GuildMemberAdd) platform.Event {
	e := &discordEvent{
		rawType:    "GUILD_MEMBER_ADD",
		rawPayload: m,
		kind:       platform.EventKindMemberJoin,
		timestamp:  time.Now(),
	}
	if m.Member != nil {
		e.senderInfo = memberFromDiscord(m.Member)
		e.id = m.GuildID + "/" + e.senderInfo.ID
	}
	e.chat = platform.ChatInfo{
		ID:      m.GuildID,
		IsGroup: true,
	}
	return e
}

// NewGuildMemberRemoveEvent converts a *discordgo.GuildMemberRemove to platform.Event.
//
// EventKind: EventKindMemberLeave
func NewGuildMemberRemoveEvent(m *discordgo.GuildMemberRemove) platform.Event {
	e := &discordEvent{
		rawType:    "GUILD_MEMBER_REMOVE",
		rawPayload: m,
		kind:       platform.EventKindMemberLeave,
		timestamp:  time.Now(),
	}
	if m.Member != nil {
		e.senderInfo = memberFromDiscord(m.Member)
		e.id = m.GuildID + "/" + e.senderInfo.ID
	}
	e.chat = platform.ChatInfo{
		ID:      m.GuildID,
		IsGroup: true,
	}
	return e
}

// NewGuildMemberUpdateEvent converts a *discordgo.GuildMemberUpdate to platform.Event.
//
// EventKind: EventKindMemberUpdate
func NewGuildMemberUpdateEvent(m *discordgo.GuildMemberUpdate) platform.Event {
	e := &discordEvent{
		rawType:    "GUILD_MEMBER_UPDATE",
		rawPayload: m,
		kind:       platform.EventKindMemberUpdate,
		timestamp:  time.Now(),
	}
	if m.Member != nil {
		e.senderInfo = memberFromDiscord(m.Member)
		e.id = m.GuildID + "/" + e.senderInfo.ID
	}
	e.chat = platform.ChatInfo{
		ID:      m.GuildID,
		IsGroup: true,
	}
	return e
}

// ────────────────────────────────────────────────────────────────────────────
// Reaction events
// ────────────────────────────────────────────────────────────────────────────

// NewMessageReactionAddEvent converts a *discordgo.MessageReactionAdd to platform.Event.
//
// EventKind: EventKindReaction
// Content: emoji name or "id:name" for custom emoji
func NewMessageReactionAddEvent(r *discordgo.MessageReactionAdd) platform.Event {
	e := &discordEvent{
		rawType:    "MESSAGE_REACTION_ADD",
		rawPayload: r,
		kind:       platform.EventKindReaction,
		timestamp:  time.Now(),
	}
	if r.MessageReaction != nil {
		e.id = r.MessageID
		isDM := r.GuildID == ""
		e.chat = platform.ChatInfo{
			ID:       r.ChannelID,
			ParentID: r.GuildID,
			IsGroup:  !isDM,
			IsDM:     isDM,
		}
		if r.Emoji.ID != "" {
			e.segments = []platform.Segment{{Type: platform.SegmentText, Text: r.Emoji.ID + ":" + r.Emoji.Name}}
		} else {
			e.segments = []platform.Segment{{Type: platform.SegmentText, Text: r.Emoji.Name}}
		}
		e.senderInfo = platform.UserInfo{ID: r.UserID}
	}
	return e
}

// NewMessageReactionRemoveEvent converts a *discordgo.MessageReactionRemove to platform.Event.
//
// EventKind: EventKindReaction
func NewMessageReactionRemoveEvent(r *discordgo.MessageReactionRemove) platform.Event {
	e := &discordEvent{
		rawType:    "MESSAGE_REACTION_REMOVE",
		rawPayload: r,
		kind:       platform.EventKindReaction,
		timestamp:  time.Now(),
	}
	if r.MessageReaction != nil {
		e.id = r.MessageID
		isDM := r.GuildID == ""
		e.chat = platform.ChatInfo{
			ID:       r.ChannelID,
			ParentID: r.GuildID,
			IsGroup:  !isDM,
			IsDM:     isDM,
		}
		if r.Emoji.ID != "" {
			e.segments = []platform.Segment{{Type: platform.SegmentText, Text: r.Emoji.ID + ":" + r.Emoji.Name}}
		} else {
			e.segments = []platform.Segment{{Type: platform.SegmentText, Text: r.Emoji.Name}}
		}
		e.senderInfo = platform.UserInfo{ID: r.UserID}
	}
	return e
}

// ────────────────────────────────────────────────────────────────────────────
// Channel events
// ────────────────────────────────────────────────────────────────────────────

// NewChannelCreateEvent converts a *discordgo.ChannelCreate to platform.Event.
//
// EventKind: EventKindChannelChange
func NewChannelCreateEvent(c *discordgo.ChannelCreate) platform.Event {
	return &discordEvent{
		rawType:    "CHANNEL_CREATE",
		rawPayload: c,
		kind:       platform.EventKindChannelChange,
		id:         c.ID,
		chat: platform.ChatInfo{
			ID:       c.ID,
			Name:     c.Name,
			ParentID: c.GuildID,
			IsGroup:  true,
		},
		timestamp: time.Now(),
	}
}

// NewChannelUpdateEvent converts a *discordgo.ChannelUpdate to platform.Event.
//
// EventKind: EventKindChannelChange
func NewChannelUpdateEvent(c *discordgo.ChannelUpdate) platform.Event {
	return &discordEvent{
		rawType:    "CHANNEL_UPDATE",
		rawPayload: c,
		kind:       platform.EventKindChannelChange,
		id:         c.ID,
		chat: platform.ChatInfo{
			ID:       c.ID,
			Name:     c.Name,
			ParentID: c.GuildID,
			IsGroup:  true,
		},
		timestamp: time.Now(),
	}
}

// NewChannelDeleteEvent converts a *discordgo.ChannelDelete to platform.Event.
//
// EventKind: EventKindChannelChange
func NewChannelDeleteEvent(c *discordgo.ChannelDelete) platform.Event {
	return &discordEvent{
		rawType:    "CHANNEL_DELETE",
		rawPayload: c,
		kind:       platform.EventKindChannelChange,
		id:         c.ID,
		chat: platform.ChatInfo{
			ID:       c.ID,
			Name:     c.Name,
			ParentID: c.GuildID,
			IsGroup:  true,
		},
		timestamp: time.Now(),
	}
}

// ────────────────────────────────────────────────────────────────────────────
// System events
// ────────────────────────────────────────────────────────────────────────────

// NewReadyEvent converts a *discordgo.Ready to platform.Event.
//
// EventKind: EventKindSystem
// Fired once when the Gateway connection is fully established.
func NewReadyEvent(r *discordgo.Ready) platform.Event {
	e := &discordEvent{
		rawType:    "READY",
		rawPayload: r,
		kind:       platform.EventKindSystem,
		id:         "READY",
		timestamp:  time.Now(),
	}
	if r.User != nil {
		e.senderInfo = userFromDiscord(r.User)
	}
	return e
}

// NewResumedEvent converts a *discordgo.Resumed to platform.Event.
//
// EventKind: EventKindSystem
// Fired when the Gateway connection is resumed after a disconnect.
func NewResumedEvent(r *discordgo.Resumed) platform.Event {
	return &discordEvent{
		rawType:    "RESUMED",
		rawPayload: r,
		kind:       platform.EventKindSystem,
		id:         "RESUMED",
		timestamp:  time.Now(),
	}
}
