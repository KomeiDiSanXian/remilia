package wechat_test

import (
	"context"
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform/wechat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAdapter(t *testing.T) {
	adapter := wechat.NewAdapter()
	require.NotNil(t, adapter)

	assert.Equal(t, "wechat", adapter.Platform())
	assert.False(t, adapter.IsRunning())
	assert.NotNil(t, adapter.Sender())
}

func TestWechatCapabilities(t *testing.T) {
	adapter := wechat.NewAdapter()

	caps := adapter.Capabilities()
	assert.False(t, caps.Markdown)
	assert.True(t, caps.Buttons)
	assert.False(t, caps.MultiAttachment)
	assert.False(t, caps.MessageEdit)
	assert.False(t, caps.MessageDelete)
	assert.False(t, caps.Embeds)
	assert.True(t, caps.FileUpload)
	assert.False(t, caps.GuildSupport)
	assert.False(t, caps.Reactions)
	assert.False(t, caps.ThreadReply)
	assert.False(t, caps.TypingIndicator)
	assert.False(t, caps.MentionAll)
	assert.False(t, caps.VoiceChannel)
}

func TestWechatStartNotImplemented(t *testing.T) {
	adapter := wechat.NewAdapter()

	err := adapter.Start(context.TODO(), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not yet implemented")
}

func TestWechatStopReturnsNil(t *testing.T) {
	adapter := wechat.NewAdapter()

	err := adapter.Stop(context.Background())
	assert.NoError(t, err)
}
