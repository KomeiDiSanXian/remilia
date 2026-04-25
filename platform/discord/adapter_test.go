package discord_test

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform/discord"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAdapter(t *testing.T) {
	adapter, err := discord.NewAdapter("fake-token-for-testing")
	require.NoError(t, err)
	require.NotNil(t, adapter)

	assert.Equal(t, "discord", adapter.Platform())
	assert.False(t, adapter.IsRunning())
	assert.NotNil(t, adapter.Sender())
}

func TestDiscordCapabilities(t *testing.T) {
	adapter, err := discord.NewAdapter("fake-token-for-testing")
	require.NoError(t, err)

	caps := adapter.Capabilities()
	assert.True(t, caps.Markdown)
	assert.True(t, caps.Embeds)
	assert.True(t, caps.GuildSupport)
	assert.True(t, caps.Reactions)
	assert.Equal(t, 2000, caps.MaxTextLength)
	assert.Equal(t, 8, caps.MaxAttachmentMB)
}

func TestDiscordSender(t *testing.T) {
	adapter, err := discord.NewAdapter("fake-token-for-testing")
	require.NoError(t, err)

	sender := adapter.Sender()
	require.NotNil(t, sender)
}
