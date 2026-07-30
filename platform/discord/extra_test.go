package discord_test

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/discord"
	"github.com/stretchr/testify/assert"
)

func TestApplyExtra(t *testing.T) {
	msg := platform.TextMessage("hello")
	extra := discord.MessageExtra{
		TTS:       true,
		Ephemeral: true,
	}
	msg = discord.ApplyExtra(msg, extra)

	assert.Equal(t, "hello", msg.Text)
}

func TestMessageExtraRoundTrip(t *testing.T) {
	msg := platform.TextMessage("test")
	extra := discord.MessageExtra{
		TTS:            true,
		Ephemeral:      true,
		SuppressEmbeds: true,
		AllowedMentions: &discord.AllowedMentions{
			Parse:       []string{"users"},
			Users:       []string{"123"},
			RepliedUser: true,
		},
	}
	msg = discord.ApplyExtra(msg, extra)
	assert.Equal(t, "test", msg.Text)
}

func TestAllowedMentions(t *testing.T) {
	am := &discord.AllowedMentions{
		Parse:       []string{"users", "roles"},
		Roles:       []string{"r1", "r2"},
		Users:       []string{"u1"},
		RepliedUser: true,
	}
	assert.Equal(t, []string{"users", "roles"}, am.Parse)
	assert.Equal(t, []string{"r1", "r2"}, am.Roles)
	assert.Equal(t, []string{"u1"}, am.Users)
	assert.True(t, am.RepliedUser)
}

func TestAllowedMentions_Empty(t *testing.T) {
	am := &discord.AllowedMentions{}
	assert.Empty(t, am.Parse)
	assert.Empty(t, am.Roles)
	assert.Empty(t, am.Users)
	assert.False(t, am.RepliedUser)
}

func TestMessageExtra_ZeroValue(t *testing.T) {
	extra := discord.MessageExtra{}
	assert.False(t, extra.TTS)
	assert.False(t, extra.Ephemeral)
	assert.False(t, extra.SuppressEmbeds)
	assert.Nil(t, extra.AllowedMentions)
}

func TestApplyExtra_Chain(t *testing.T) {
	msg := platform.TextMessage("multi")
	msg = discord.ApplyExtra(msg, discord.MessageExtra{TTS: true})
	msg = discord.ApplyExtra(msg, discord.MessageExtra{Ephemeral: true})

	extra := discord.MessageExtra{}
	_ = extra
	assert.Equal(t, "multi", msg.Text)
}
