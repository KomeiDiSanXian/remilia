package onebot

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig("ws://127.0.0.1:6700")
	assert.Equal(t, ModeForwardWS, cfg.Mode)
	assert.Equal(t, "ws://127.0.0.1:6700", cfg.URL)
	assert.Equal(t, 1*time.Second, cfg.ReconnectDelay)
	assert.Equal(t, 60*time.Second, cfg.ReconnectMaxDelay)
	assert.Equal(t, 10*time.Second, cfg.APITimeout)
	assert.Equal(t, 100, cfg.EventBufferSize)
}

func TestDefaultReverseConfig(t *testing.T) {
	t.Parallel()
	cfg := DefaultReverseConfig(":8080")
	assert.Equal(t, ModeReverseWS, cfg.Mode)
	assert.Equal(t, ":8080", cfg.ListenAddr)
	assert.Equal(t, 10*time.Second, cfg.APITimeout)
	assert.Equal(t, 100, cfg.EventBufferSize)
}

func TestDefaultHTTPPostConfig(t *testing.T) {
	t.Parallel()
	cfg := DefaultHTTPPostConfig(":8080", "http://127.0.0.1:5700")
	assert.Equal(t, ModeHTTPPost, cfg.Mode)
	assert.Equal(t, ":8080", cfg.ListenAddr)
	assert.Equal(t, "http://127.0.0.1:5700", cfg.URL)
	assert.Equal(t, 10*time.Second, cfg.APITimeout)
	assert.Equal(t, 100, cfg.EventBufferSize)
}

func TestConfigSetDefaults(t *testing.T) {
	t.Parallel()
	t.Run("fills zero values", func(t *testing.T) {
		var cfg Config
		cfg.setDefaults()
		assert.Equal(t, time.Second, cfg.ReconnectDelay)
		assert.Equal(t, 60*time.Second, cfg.ReconnectMaxDelay)
		assert.Equal(t, 10*time.Second, cfg.APITimeout)
		assert.Equal(t, 100, cfg.EventBufferSize)
	})

	t.Run("preserves configured values", func(t *testing.T) {
		cfg := Config{
			ReconnectDelay:    5 * time.Second,
			ReconnectMaxDelay: 30 * time.Second,
			APITimeout:        15 * time.Second,
			EventBufferSize:   200,
		}
		cfg.setDefaults()
		assert.Equal(t, 5*time.Second, cfg.ReconnectDelay)
		assert.Equal(t, 30*time.Second, cfg.ReconnectMaxDelay)
		assert.Equal(t, 15*time.Second, cfg.APITimeout)
		assert.Equal(t, 200, cfg.EventBufferSize)
	})

	t.Run("handles negative and zero values", func(t *testing.T) {
		cfg := Config{
			ReconnectDelay:    -1,
			ReconnectMaxDelay: -1,
			APITimeout:        -1,
			EventBufferSize:   -1,
		}
		cfg.setDefaults()
		assert.Equal(t, time.Second, cfg.ReconnectDelay)
		assert.Equal(t, 60*time.Second, cfg.ReconnectMaxDelay)
		assert.Equal(t, 10*time.Second, cfg.APITimeout)
		assert.Equal(t, 100, cfg.EventBufferSize)
	})
}

func TestNewForwardWSAdapter(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig("ws://127.0.0.1:6700")
	adapter := NewForwardWSAdapter(cfg)
	require.NotNil(t, adapter)

	assert.Equal(t, ModeForwardWS, adapter.config.Mode)
	assert.Equal(t, "ws://127.0.0.1:6700", adapter.config.URL)
}

func TestNewAdapter(t *testing.T) {
	t.Parallel()
	adapter := NewAdapter("ws://127.0.0.1:6700")
	require.NotNil(t, adapter)
	assert.Equal(t, ModeForwardWS, adapter.config.Mode)
	assert.Equal(t, "ws://127.0.0.1:6700", adapter.config.URL)
}

func TestForwardWSAdapter_Platform(t *testing.T) {
	t.Parallel()
	adapter := NewAdapter("ws://127.0.0.1:6700")
	assert.Equal(t, "onebot", adapter.Platform())
}

func TestForwardWSAdapter_IsRunningBeforeStart(t *testing.T) {
	t.Parallel()
	adapter := NewAdapter("ws://127.0.0.1:6700")
	assert.False(t, adapter.IsRunning())
}

func TestForwardWSAdapter_Capabilities(t *testing.T) {
	t.Parallel()
	adapter := NewAdapter("ws://127.0.0.1:6700")
	caps := adapter.Capabilities()
	assert.True(t, caps.MessageDelete)
	assert.True(t, caps.ThreadReply)
	assert.True(t, caps.MentionAll)
	assert.False(t, caps.Reactions)
	assert.False(t, caps.MessageEdit)
	assert.False(t, caps.MultiAttachment)
	assert.False(t, caps.FileUpload)
}

func TestForwardWSAdapter_BotIdentityBeforeStart(t *testing.T) {
	t.Parallel()
	adapter := NewAdapter("ws://127.0.0.1:6700")
	assert.Empty(t, adapter.BotID())
	assert.Empty(t, adapter.BotName())
}
