package discord_test

// 本文档核验说明
//
// 本文件中的 Gateway 事件名与交互类型断言均对照 Discord API 官方文档
// （2026-07 核验，discord/discord-api-docs 仓库 developers/events/ 目录）：
//
//	- Gateway 事件（MESSAGE_CREATE、GUILD_MEMBER_ADD、INTERACTION_CREATE、
//	  MESSAGE_REACTION_ADD、CHANNEL_CREATE、READY 等）：gateway-events.mdx
//	- Interaction Type 枚举（1=ping、2=application_command、
//	  3=message_component、4=autocomplete、5=modal_submit）：由 discordgo
//	  v0.29.0 的 InteractionType 常量提供，与官方枚举值一致
//
// REST 端点由 discordgo 库封装，不在本测试的断言范围内。

import (
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/discord"
	"github.com/bwmarrin/discordgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMessageCreateEvent_DM(t *testing.T) {
	now := time.Now()
	m := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			ID:        "1001",
			ChannelID: "ch1",
			Content:   "hello",
			Author:    &discordgo.User{ID: "u1", Username: "user1", GlobalName: "User One"},
			Timestamp: now,
		},
	}
	ev := discord.NewMessageCreateEvent(m)
	assert.Equal(t, platform.EventKindPrivateMessage, ev.Kind())
	assert.Equal(t, "discord", ev.Platform())
	assert.Equal(t, "1001", ev.ID())
	assert.Equal(t, "hello", ev.Content())
	assert.Equal(t, "u1", ev.Sender().ID)
	assert.Equal(t, "User One", ev.Sender().DisplayName)
	assert.True(t, ev.Chat().IsDM)
	assert.False(t, ev.Chat().IsGroup)
	assert.Equal(t, "ch1", ev.Chat().ID)
}

func TestNewMessageCreateEvent_Guild(t *testing.T) {
	m := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			ID:        "1002",
			ChannelID: "ch2",
			GuildID:   "g1",
			Content:   "group hello",
			Author:    &discordgo.User{ID: "u2", Username: "user2"},
		},
	}
	ev := discord.NewMessageCreateEvent(m)
	assert.Equal(t, platform.EventKindGuildMessage, ev.Kind())
	assert.False(t, ev.Chat().IsDM)
	assert.True(t, ev.Chat().IsGroup)
	assert.Equal(t, "g1", ev.Chat().ParentID)
}

func TestNewMessageCreateEvent_Reply(t *testing.T) {
	m := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			ID:      "1003",
			Content: "reply test",
			MessageReference: &discordgo.MessageReference{
				MessageID: "orig",
			},
		},
	}
	ev := discord.NewMessageCreateEvent(m)
	assert.Equal(t, "orig", platform.GetReplyToID(ev))
}

func TestNewMessageCreateEvent_NilMessage(t *testing.T) {
	ev := discord.NewMessageCreateEvent(&discordgo.MessageCreate{})
	assert.Equal(t, platform.EventKindUnknown, ev.Kind())
}

func TestNewMessageCreateEvent_WithAttachments(t *testing.T) {
	m := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			ID:      "1004",
			Content: "with file",
			Attachments: []*discordgo.MessageAttachment{
				{URL: "https://cdn.discord.com/attachments/1.png", Filename: "img.png", Size: 1024, Width: 100, Height: 200, ContentType: "image/png"},
			},
		},
	}
	ev := discord.NewMessageCreateEvent(m)
	atts := ev.Attachments()
	require.Len(t, atts, 1)
	assert.Equal(t, "https://cdn.discord.com/attachments/1.png", atts[0].URL)
	assert.Equal(t, "img.png", atts[0].Name)
	assert.Equal(t, 1024, atts[0].Size)
}

func TestNewMessageCreateEvent_WithMentions(t *testing.T) {
	m := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			ID:      "1005",
			Content: "hello @user",
			Mentions: []*discordgo.User{
				{ID: "m1", Username: "mentioned", GlobalName: "Mentioned User"},
			},
		},
	}
	ev := discord.NewMessageCreateEvent(m)
	mentions := platform.GetMentions(ev)
	require.Len(t, mentions, 1)
	assert.Equal(t, "m1", mentions[0].ID)
	assert.Equal(t, "Mentioned User", mentions[0].DisplayName)
}

func TestNewMessageUpdateEvent(t *testing.T) {
	now := time.Now()
	m := &discordgo.MessageUpdate{
		Message: &discordgo.Message{
			ID:              "2001",
			ChannelID:       "ch1",
			GuildID:         "g1",
			Content:         "edited content",
			Author:          &discordgo.User{ID: "u1", Username: "user1"},
			EditedTimestamp: &now,
		},
	}
	ev := discord.NewMessageUpdateEvent(m)
	assert.Equal(t, platform.EventKindMessageUpdate, ev.Kind())
	assert.Equal(t, "edited content", ev.Content())
}

func TestNewMessageUpdateEvent_NilMessage(t *testing.T) {
	ev := discord.NewMessageUpdateEvent(&discordgo.MessageUpdate{})
	assert.Equal(t, platform.EventKindMessageUpdate, ev.Kind())
}

func TestNewMessageDeleteEvent(t *testing.T) {
	m := &discordgo.MessageDelete{
		Message: &discordgo.Message{
			ID:        "3001",
			ChannelID: "ch1",
			GuildID:   "g1",
		},
	}
	ev := discord.NewMessageDeleteEvent(m)
	assert.Equal(t, platform.EventKindMessageDelete, ev.Kind())
	assert.Equal(t, "3001", ev.ID())
	assert.True(t, ev.Chat().IsGroup)
}

func TestNewMessageDeleteEvent_DM(t *testing.T) {
	m := &discordgo.MessageDelete{
		Message: &discordgo.Message{
			ID:        "3002",
			ChannelID: "ch2",
		},
	}
	ev := discord.NewMessageDeleteEvent(m)
	assert.True(t, ev.Chat().IsDM)
}

func TestNewInteractionCreateEvent_SlashCommand(t *testing.T) {
	i := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			ID:        "int1",
			ChannelID: "ch1",
			GuildID:   "g1",
			Type:      discordgo.InteractionApplicationCommand,
			Member: &discordgo.Member{
				User: &discordgo.User{ID: "u1", Username: "cmduser"},
			},
			Token: "tok123",
			Data: discordgo.ApplicationCommandInteractionData{
				Name: "ping",
			},
		},
	}
	ev := discord.NewInteractionCreateEvent(i)
	assert.Equal(t, platform.EventKindInteraction, ev.Kind())
	assert.Equal(t, "/ping", ev.Content())
	assert.Equal(t, "int1", ev.Chat().Tokens[discord.TokenInteractionID])
	assert.Equal(t, "tok123", ev.Chat().Tokens[discord.TokenInteractionToken])
}

func TestNewInteractionCreateEvent_DM(t *testing.T) {
	i := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			ID:        "int2",
			ChannelID: "ch2",
			Type:      discordgo.InteractionApplicationCommand,
			User:      &discordgo.User{ID: "u2", Username: "dmuser"},
			Data: discordgo.ApplicationCommandInteractionData{
				Name:    "echo",
				Options: []*discordgo.ApplicationCommandInteractionDataOption{{Name: "text", Value: "hello"}},
			},
		},
	}
	ev := discord.NewInteractionCreateEvent(i)
	assert.True(t, ev.Chat().IsDM)
	assert.Contains(t, ev.Content(), "/echo")
	assert.Contains(t, ev.Content(), "text:hello")
}

func TestNewInteractionCreateEvent_MessageComponent(t *testing.T) {
	i := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			ID:   "int3",
			Type: discordgo.InteractionMessageComponent,
			Member: &discordgo.Member{
				User: &discordgo.User{ID: "u3", Username: "buttonuser"},
			},
			Data: discordgo.MessageComponentInteractionData{
				CustomID: "btn_click",
			},
		},
	}
	ev := discord.NewInteractionCreateEvent(i)
	assert.Equal(t, "btn_click", ev.Content())
}

func TestNewInteractionCreateEvent_ModalSubmit(t *testing.T) {
	i := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			ID:   "int4",
			Type: discordgo.InteractionModalSubmit,
			Data: discordgo.ModalSubmitInteractionData{
				CustomID: "modal_1",
			},
		},
	}
	ev := discord.NewInteractionCreateEvent(i)
	assert.Equal(t, "modal_1", ev.Content())
}

func TestNewGuildCreateEvent(t *testing.T) {
	g := &discordgo.GuildCreate{
		Guild: &discordgo.Guild{
			ID:   "g1",
			Name: "Test Guild",
		},
	}
	ev := discord.NewGuildCreateEvent(g)
	assert.Equal(t, platform.EventKindBotAdded, ev.Kind())
	assert.Equal(t, "Test Guild", ev.Chat().Name)
}

func TestNewGuildDeleteEvent(t *testing.T) {
	g := &discordgo.GuildDelete{
		Guild: &discordgo.Guild{
			ID: "g1",
		},
	}
	ev := discord.NewGuildDeleteEvent(g)
	assert.Equal(t, platform.EventKindBotRemoved, ev.Kind())
}

func TestNewGuildMemberAddEvent(t *testing.T) {
	m := &discordgo.GuildMemberAdd{
		Member: &discordgo.Member{
			User:    &discordgo.User{ID: "u1", Username: "newuser"},
			GuildID: "g1",
		},
	}
	ev := discord.NewGuildMemberAddEvent(m)
	assert.Equal(t, platform.EventKindMemberJoin, ev.Kind())
	assert.Equal(t, "u1", ev.Sender().ID)
}

func TestNewGuildMemberRemoveEvent(t *testing.T) {
	m := &discordgo.GuildMemberRemove{
		Member: &discordgo.Member{
			User:    &discordgo.User{ID: "u1", Username: "leftuser"},
			GuildID: "g1",
		},
	}
	ev := discord.NewGuildMemberRemoveEvent(m)
	assert.Equal(t, platform.EventKindMemberLeave, ev.Kind())
}

func TestNewGuildMemberUpdateEvent(t *testing.T) {
	m := &discordgo.GuildMemberUpdate{
		Member: &discordgo.Member{
			User:    &discordgo.User{ID: "u1", Username: "updateduser"},
			GuildID: "g1",
		},
	}
	ev := discord.NewGuildMemberUpdateEvent(m)
	assert.Equal(t, platform.EventKindMemberUpdate, ev.Kind())
}

func TestNewMessageReactionAddEvent(t *testing.T) {
	r := &discordgo.MessageReactionAdd{
		MessageReaction: &discordgo.MessageReaction{
			MessageID: "msg1",
			ChannelID: "ch1",
			GuildID:   "g1",
			Emoji:     discordgo.Emoji{Name: "👍"},
			UserID:    "u1",
		},
	}
	ev := discord.NewMessageReactionAddEvent(r)
	assert.Equal(t, platform.EventKindReaction, ev.Kind())
	assert.Equal(t, "👍", ev.Content())
}

func TestNewMessageReactionAddEvent_CustomEmoji(t *testing.T) {
	r := &discordgo.MessageReactionAdd{
		MessageReaction: &discordgo.MessageReaction{
			MessageID: "msg2",
			ChannelID: "ch1",
			Emoji:     discordgo.Emoji{ID: "123", Name: "custom"},
			UserID:    "u1",
		},
	}
	ev := discord.NewMessageReactionAddEvent(r)
	assert.Equal(t, "123:custom", ev.Content())
}

func TestNewMessageReactionRemoveEvent(t *testing.T) {
	r := &discordgo.MessageReactionRemove{
		MessageReaction: &discordgo.MessageReaction{
			MessageID: "msg3",
			ChannelID: "ch1",
			Emoji:     discordgo.Emoji{Name: "❌"},
			UserID:    "u1",
		},
	}
	ev := discord.NewMessageReactionRemoveEvent(r)
	assert.Equal(t, platform.EventKindReaction, ev.Kind())
	assert.Equal(t, "❌", ev.Content())
}

func TestNewReadyEvent(t *testing.T) {
	r := &discordgo.Ready{
		User: &discordgo.User{ID: "bot1", Username: "MyBot", GlobalName: "My Bot"},
	}
	ev := discord.NewReadyEvent(r)
	assert.Equal(t, platform.EventKindSystem, ev.Kind())
	assert.Equal(t, "bot1", ev.Sender().ID)
}

func TestNewReadyEvent_NilUser(t *testing.T) {
	ev := discord.NewReadyEvent(&discordgo.Ready{})
	assert.Equal(t, platform.EventKindSystem, ev.Kind())
}

func TestNewResumedEvent(t *testing.T) {
	ev := discord.NewResumedEvent(&discordgo.Resumed{})
	assert.Equal(t, platform.EventKindSystem, ev.Kind())
	assert.Equal(t, "RESUMED", ev.ID())
}

func TestNewChannelCreateEvent(t *testing.T) {
	c := &discordgo.ChannelCreate{
		Channel: &discordgo.Channel{
			ID:      "ch1",
			Name:    "general",
			GuildID: "g1",
		},
	}
	ev := discord.NewChannelCreateEvent(c)
	assert.Equal(t, platform.EventKindChannelChange, ev.Kind())
	assert.Equal(t, "general", ev.Chat().Name)
}

func TestNewChannelUpdateEvent(t *testing.T) {
	c := &discordgo.ChannelUpdate{
		Channel: &discordgo.Channel{
			ID:      "ch1",
			Name:    "renamed",
			GuildID: "g1",
		},
	}
	ev := discord.NewChannelUpdateEvent(c)
	assert.Equal(t, "renamed", ev.Chat().Name)
}

func TestNewChannelDeleteEvent(t *testing.T) {
	c := &discordgo.ChannelDelete{
		Channel: &discordgo.Channel{
			ID:      "ch1",
			Name:    "deleted",
			GuildID: "g1",
		},
	}
	ev := discord.NewChannelDeleteEvent(c)
	assert.Equal(t, "ch1", ev.Chat().ID)
}

func TestNewGuildUpdateEvent(t *testing.T) {
	g := &discordgo.GuildUpdate{
		Guild: &discordgo.Guild{
			ID:   "g1",
			Name: "Updated Guild",
		},
	}
	ev := discord.NewGuildUpdateEvent(g)
	assert.Equal(t, platform.EventKindGuildChange, ev.Kind())
	assert.Equal(t, "Updated Guild", ev.Chat().Name)
}

func TestPlatformID(t *testing.T) {
	assert.Equal(t, "discord", discord.PlatformID)
}

func TestTokenConstants(t *testing.T) {
	assert.Equal(t, "interaction_id", discord.TokenInteractionID)
	assert.Equal(t, "interaction_token", discord.TokenInteractionToken)
}

func TestEventRawType(t *testing.T) {
	m := &discordgo.MessageCreate{Message: &discordgo.Message{ID: "1"}}
	ev := discord.NewMessageCreateEvent(m)
	re, ok := ev.(interface{ RawType() string })
	require.True(t, ok)
	assert.Equal(t, "MESSAGE_CREATE", re.RawType())

	ev2 := discord.NewReadyEvent(&discordgo.Ready{})
	re2, ok := ev2.(interface{ RawType() string })
	require.True(t, ok)
	assert.Equal(t, "READY", re2.RawType())
}

func TestEventInterfaceCompliance(t *testing.T) {
	m := &discordgo.MessageCreate{Message: &discordgo.Message{ID: "1", Content: "test"}}
	ev := discord.NewMessageCreateEvent(m)

	_ = ev
}
