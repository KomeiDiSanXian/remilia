package qq

import (
	"context"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/config"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestBotInfo() *dto.BotInfo {
	return &dto.BotInfo{
		QQNum:     123456,
		AppID:     7812,
		Token:     "test-token",
		AppSecret: "test-secret",
	}
}

// TestWebhookServerAdapter_New tests creating WebhookServerAdapter
func TestWebhookServerAdapter_New(t *testing.T) {
	info := newTestBotInfo()

	t.Run("with defaults", func(t *testing.T) {
		adapter := NewWebhookServerAdapter(":0", info)
		require.NotNil(t, adapter)
		assert.Equal(t, ":0", adapter.addr)
		assert.Greater(t, adapter.workers, 0)
	})

	t.Run("with config", func(t *testing.T) {
		cfg := config.WebhookConfig{
			WorkerCount: 4,
			EventBuffer: 200,
		}
		adapter := NewWebhookServerAdapterWithConfig(":0", info, cfg)
		require.NotNil(t, adapter)
		assert.Equal(t, 4, adapter.workers)
		assert.Equal(t, 200, adapter.bufferSize)
	})
}

// TestWebhookServerAdapter_StartStop tests Start and Stop
func TestWebhookServerAdapter_StartStop(t *testing.T) {
	adapter := NewWebhookServerAdapter(":0", newTestBotInfo())

	ctx := context.Background()
	handler := func(_ platform.Event) {}

	err := adapter.Start(ctx, handler)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = adapter.Stop(stopCtx)
	assert.NoError(t, err)
}

// TestWebhookServerAdapter_DoubleStart tests starting twice
func TestWebhookServerAdapter_DoubleStart(t *testing.T) {
	adapter := NewWebhookServerAdapter(":0", newTestBotInfo())

	ctx := context.Background()
	handler := func(_ platform.Event) {}

	err := adapter.Start(ctx, handler)
	require.NoError(t, err)

	err = adapter.Start(ctx, handler)
	assert.NoError(t, err)

	_ = adapter.Stop(context.Background())
}

// TestWebhookServerAdapter_StopBeforeStart tests stopping before start
func TestWebhookServerAdapter_StopBeforeStart(t *testing.T) {
	adapter := NewWebhookServerAdapter(":0", newTestBotInfo())
	err := adapter.Stop(context.Background())
	assert.NoError(t, err)
}
