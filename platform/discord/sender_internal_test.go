package discord

import (
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/bwmarrin/discordgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmojiToDiscord_Unicode(t *testing.T) {
	result := emojiToDiscord(platform.Emoji{Kind: platform.EmojiKindUnicode, Value: "👍"})
	assert.Equal(t, "👍", result)
}

func TestEmojiToDiscord_CustomWithID(t *testing.T) {
	result := emojiToDiscord(platform.Emoji{Kind: platform.EmojiKindCustom, Value: "myemoji", ID: "12345"})
	assert.Equal(t, "myemoji:12345", result)
}

func TestEmojiToDiscord_CustomNoName(t *testing.T) {
	result := emojiToDiscord(platform.Emoji{Kind: platform.EmojiKindCustom, ID: "12345"})
	assert.Equal(t, "12345", result)
}

func TestEmojiToDiscord_System(t *testing.T) {
	result := emojiToDiscord(platform.Emoji{Kind: platform.EmojiKindSystem, Value: "⭐"})
	assert.Equal(t, "⭐", result)
}

func TestEmojiToDiscord_Empty(t *testing.T) {
	result := emojiToDiscord(platform.Emoji{})
	assert.Equal(t, "", result)
}

func TestBuildMentionPrefix(t *testing.T) {
	result := buildMentionPrefix([]string{"123", "456"})
	assert.Equal(t, "<@123><@456>", result)
}

func TestBuildMentionPrefix_Empty(t *testing.T) {
	result := buildMentionPrefix(nil)
	assert.Equal(t, "", result)
	result = buildMentionPrefix([]string{})
	assert.Equal(t, "", result)
}

func TestConvertEmbeds(t *testing.T) {
	embeds := []platform.Embed{
		{
			Title:        "Title",
			Description:  "Desc",
			URL:          "https://example.com",
			Color:        0xFF0000,
			FooterText:   "Footer",
			ImageURL:     "https://example.com/img.png",
			ThumbnailURL: "https://example.com/thumb.png",
			Fields: []platform.EmbedField{
				{Name: "Field1", Value: "Val1", Inline: true},
			},
		},
	}
	result := convertEmbeds(embeds)
	require.Len(t, result, 1)
	assert.Equal(t, "Title", result[0].Title)
	assert.Equal(t, "Desc", result[0].Description)
	assert.Equal(t, 0xFF0000, result[0].Color)
	require.NotNil(t, result[0].Footer)
	assert.Equal(t, "Footer", result[0].Footer.Text)
	require.NotNil(t, result[0].Image)
	assert.Equal(t, "https://example.com/img.png", result[0].Image.URL)
	require.NotNil(t, result[0].Thumbnail)
	require.Len(t, result[0].Fields, 1)
	assert.Equal(t, "Field1", result[0].Fields[0].Name)
}

func TestConvertEmbeds_Nil(t *testing.T) {
	result := convertEmbeds(nil)
	assert.Empty(t, result)
}

func TestConvertEmbeds_Empty(t *testing.T) {
	result := convertEmbeds([]platform.Embed{})
	assert.Empty(t, result)
}

func TestConvertEmbeds_WithTimestamp(t *testing.T) {
	ts := time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC)
	embeds := []platform.Embed{
		{Title: "TS", Timestamp: ts},
	}
	result := convertEmbeds(embeds)
	assert.Contains(t, result[0].Timestamp, "2026")
}

func TestConvertButtons_AutoRow(t *testing.T) {
	buttons := []platform.Button{
		{Label: "Btn1", Style: platform.ButtonStylePrimary, ID: "b1"},
		{Label: "Btn2", Style: platform.ButtonStyleSecondary, ID: "b2"},
	}
	result := convertButtons(buttons)
	require.Len(t, result, 2)
	row1, ok := result[0].(discordgo.ActionsRow)
	require.True(t, ok)
	require.Len(t, row1.Components, 1)
	btn1, ok := row1.Components[0].(discordgo.Button)
	require.True(t, ok)
	assert.Equal(t, "Btn1", btn1.Label)
}

func TestConvertButtons_GroupedRows(t *testing.T) {
	buttons := []platform.Button{
		{Label: "A", Row: 1, ID: "a"},
		{Label: "B", Row: 1, ID: "b"},
		{Label: "C", Row: 2, ID: "c"},
	}
	result := convertButtons(buttons)
	require.Len(t, result, 2)
	row1 := result[0].(discordgo.ActionsRow)
	assert.Len(t, row1.Components, 2)
	row2 := result[1].(discordgo.ActionsRow)
	assert.Len(t, row2.Components, 1)
}

func TestConvertButtons_LinkStyle(t *testing.T) {
	buttons := []platform.Button{
		{Label: "Link", Style: platform.ButtonStyleLink, URL: "https://example.com"},
	}
	result := convertButtons(buttons)
	require.Len(t, result, 1)
	row := result[0].(discordgo.ActionsRow)
	btn := row.Components[0].(discordgo.Button)
	assert.Equal(t, discordgo.LinkButton, btn.Style)
	assert.Equal(t, "https://example.com", btn.URL)
}

func TestConvertButtons_DangerStyle(t *testing.T) {
	buttons := []platform.Button{
		{Label: "Danger", Style: platform.ButtonStyleDanger, ID: "danger"},
	}
	result := convertButtons(buttons)
	btn := result[0].(discordgo.ActionsRow).Components[0].(discordgo.Button)
	assert.Equal(t, discordgo.DangerButton, btn.Style)
}

func TestConvertButtons_WithEmoji(t *testing.T) {
	buttons := []platform.Button{
		{Label: "Emoji", Emoji: "👍", ID: "emoji"},
	}
	result := convertButtons(buttons)
	btn := result[0].(discordgo.ActionsRow).Components[0].(discordgo.Button)
	require.NotNil(t, btn.Emoji)
	assert.Equal(t, "👍", btn.Emoji.Name)
}

func TestConvertButtons_Empty(t *testing.T) {
	result := convertButtons(nil)
	assert.Empty(t, result)
	result = convertButtons([]platform.Button{})
	assert.Empty(t, result)
}

func TestConvertButtons_MaxRowLimit(t *testing.T) {
	buttons := make([]platform.Button, 6)
	for i := range 6 {
		buttons[i] = platform.Button{Label: "B", ID: "b"}
	}
	result := convertButtons(buttons)
	require.Len(t, result, 5)
}

func TestConvertButtons_MaxButtonsPerRow(t *testing.T) {
	buttons := make([]platform.Button, 6)
	for i := range 6 {
		buttons[i] = platform.Button{Label: "B", Row: 1, ID: "b"}
	}
	result := convertButtons(buttons)
	require.Len(t, result, 1)
	row := result[0].(discordgo.ActionsRow)
	require.Len(t, row.Components, 5)
}

func TestConvertFiles(t *testing.T) {
	files := []platform.Attachment{
		{Name: "doc.pdf", MimeType: "application/pdf", Data: []byte("pdf content")},
		{Name: "img.png", MimeType: "image/png", Data: []byte("png content")},
	}
	result := convertFiles(files)
	require.Len(t, result, 2)
	assert.Equal(t, "doc.pdf", result[0].Name)
	assert.Equal(t, "application/pdf", result[0].ContentType)
}

func TestConvertFiles_SkipsURLOnly(t *testing.T) {
	files := []platform.Attachment{
		{Name: "url.pdf", URL: "https://example.com/doc.pdf"},
	}
	result := convertFiles(files)
	assert.Empty(t, result)
}

func TestConvertFiles_EmptyName(t *testing.T) {
	files := []platform.Attachment{
		{Data: []byte("data"), MimeType: "text/plain"},
	}
	result := convertFiles(files)
	require.Len(t, result, 1)
	assert.Equal(t, "attachment", result[0].Name)
}

func TestConvertFiles_Nil(t *testing.T) {
	assert.Empty(t, convertFiles(nil))
}

func TestBuildAllowedMentions_Nil(t *testing.T) {
	assert.Nil(t, buildAllowedMentions(nil))
}

func TestBuildAllowedMentions(t *testing.T) {
	am := &AllowedMentions{
		Parse:       []string{"users", "roles"},
		Roles:       []string{"r1"},
		Users:       []string{"u1"},
		RepliedUser: true,
	}
	result := buildAllowedMentions(am)
	require.NotNil(t, result)
	assert.Equal(t, []discordgo.AllowedMentionType{"users", "roles"}, result.Parse)
	assert.Equal(t, []string{"r1"}, result.Roles)
	assert.Equal(t, []string{"u1"}, result.Users)
	assert.True(t, result.RepliedUser)
}

func TestBuildAllowedMentions_Empty(t *testing.T) {
	result := buildAllowedMentions(&AllowedMentions{})
	require.NotNil(t, result)
	assert.Empty(t, result.Parse)
	assert.Empty(t, result.Roles)
}

func TestBuildMessageSend_Content(t *testing.T) {
	msg := platform.TextMessage("hello")
	result := buildMessageSend(msg, MessageExtra{})
	assert.Equal(t, "hello", result.Content)
}

func TestBuildMessageSend_Markdown(t *testing.T) {
	msg := platform.MarkdownMessage("**bold**")
	result := buildMessageSend(msg, MessageExtra{})
	assert.Equal(t, "**bold**", result.Content)
}

func TestBuildMessageSend_WithMentions(t *testing.T) {
	msg := platform.TextMessage("hello").WithMentions("123")
	result := buildMessageSend(msg, MessageExtra{})
	assert.Equal(t, "<@123>hello", result.Content)
}

func TestBuildMessageSend_WithReply(t *testing.T) {
	msg := platform.TextMessage("hello").WithReply("999")
	result := buildMessageSend(msg, MessageExtra{})
	require.NotNil(t, result.Reference)
	assert.Equal(t, "999", result.Reference.MessageID)
}

func TestBuildMessageSend_WithEmbeds(t *testing.T) {
	msg := platform.TextMessage("hello").WithEmbeds(
		platform.Embed{Title: "Embed Title"},
	)
	result := buildMessageSend(msg, MessageExtra{})
	require.Len(t, result.Embeds, 1)
	assert.Equal(t, "Embed Title", result.Embeds[0].Title)
}

func TestBuildMessageSend_WithTTS(t *testing.T) {
	msg := platform.TextMessage("hello")
	result := buildMessageSend(msg, MessageExtra{TTS: true})
	assert.True(t, result.TTS)
}

func TestBuildMessageEdit(t *testing.T) {
	msg := platform.TextMessage("edited")
	result := buildMessageEdit("ch1", "msg1", msg, MessageExtra{})
	assert.Equal(t, "ch1", result.Channel)
	assert.Equal(t, "msg1", result.ID)
	assert.Equal(t, "edited", *result.Content)
}

func TestBuildInteractionResponse(t *testing.T) {
	msg := platform.TextMessage("response")
	result := buildInteractionResponse(msg, MessageExtra{Ephemeral: true})
	assert.Equal(t, discordgo.InteractionResponseChannelMessageWithSource, result.Type)
	assert.Equal(t, "response", result.Data.Content)
	assert.Equal(t, discordgo.MessageFlagsEphemeral, result.Data.Flags&discordgo.MessageFlagsEphemeral)
}

func TestBuildInteractionResponse_SuppressEmbeds(t *testing.T) {
	msg := platform.TextMessage("no embeds")
	result := buildInteractionResponse(msg, MessageExtra{SuppressEmbeds: true})
	assert.Equal(t, discordgo.MessageFlagsSuppressEmbeds, result.Data.Flags&discordgo.MessageFlagsSuppressEmbeds)
}

func TestBuildInteractionResponse_WithMentions(t *testing.T) {
	msg := platform.TextMessage("hello").WithMentions("123")
	result := buildInteractionResponse(msg, MessageExtra{})
	assert.Equal(t, "<@123>hello", result.Data.Content)
}

func TestBuildFollowupParams(t *testing.T) {
	msg := platform.TextMessage("followup")
	result := buildFollowupParams(msg, MessageExtra{})
	assert.Equal(t, "followup", result.Content)
}

func TestBuildFollowupParams_Ephemeral(t *testing.T) {
	msg := platform.TextMessage("ephemeral")
	result := buildFollowupParams(msg, MessageExtra{Ephemeral: true})
	assert.Equal(t, discordgo.MessageFlagsEphemeral, result.Flags&discordgo.MessageFlagsEphemeral)
}

func TestDiscordCapabilities(t *testing.T) {
	caps := discordCapabilities()
	assert.True(t, caps.Markdown)
	assert.True(t, caps.Buttons)
	assert.True(t, caps.MessageEdit)
	assert.True(t, caps.MessageDelete)
	assert.True(t, caps.Embeds)
	assert.True(t, caps.FileUpload)
	assert.True(t, caps.GuildSupport)
	assert.True(t, caps.Reactions)
	assert.True(t, caps.TypingIndicator)
	assert.True(t, caps.MentionAll)
	assert.Equal(t, 2000, caps.MaxTextLength)
	assert.Equal(t, 8, caps.MaxAttachmentMB)
}
