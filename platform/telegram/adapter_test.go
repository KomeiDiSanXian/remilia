package telegram_test

import (
	"context"
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform/telegram"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAdapter(t *testing.T) {
	adapter := telegram.NewAdapter()
	require.NotNil(t, adapter)

	assert.Equal(t, "telegram", adapter.Platform())
	assert.False(t, adapter.IsRunning())
	assert.NotNil(t, adapter.Sender())
}

func TestTelegramCapabilities(t *testing.T) {
	adapter := telegram.NewAdapter()

	caps := adapter.Capabilities()
	assert.True(t, caps.Markdown)
	assert.True(t, caps.Buttons)
	assert.False(t, caps.GuildSupport)
	assert.False(t, caps.Embeds)
	assert.Equal(t, 4096, caps.MaxTextLength)
	assert.Equal(t, 50, caps.MaxAttachmentMB)
}

func TestTelegramStartNotImplemented(t *testing.T) {
	adapter := telegram.NewAdapter()

	err := adapter.Start(context.TODO(), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not yet implemented")
}
