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
		// D4: 字段迁移到子组件
		assert.Equal(t, ":0", adapter.conn.addr)
		assert.Greater(t, adapter.adapter.workers, 0)
	})

	t.Run("with config", func(t *testing.T) {
		cfg := config.WebhookConfig{
			WorkerCount: 4,
			EventBuffer: 200,
		}
		adapter := NewWebhookServerAdapterWithConfig(":0", info, cfg)
		require.NotNil(t, adapter)
		assert.Equal(t, 4, adapter.adapter.workers)
		assert.Equal(t, 200, adapter.conn.bufferSize)
	})
}

// TestWebhookServerAdapter_StartStop tests Start and Stop
//
// D4：Start() 现在是阻塞的（先启动 HTTP 服务器，再运行事件循环），
// 测试中需在 goroutine 中调用，并通过 Stop() 触发退出。
func TestWebhookServerAdapter_StartStop(t *testing.T) {
	adapter := NewWebhookServerAdapter(":0", newTestBotInfo())

	ctx := t.Context()

	handler := func(_ platform.Event) {}

	startErr := make(chan error, 1)
	go func() {
		startErr <- adapter.Start(ctx, handler)
	}()

	// 给服务器一点时间启动
	time.Sleep(100 * time.Millisecond)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()

	err := adapter.Stop(stopCtx)
	assert.NoError(t, err)

	// 等待 Start 返回
	select {
	case err := <-startErr:
		assert.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Error("Start did not return after Stop within timeout")
	}
}

// TestWebhookServerAdapter_DoubleStart tests starting twice.
//
// D4：Start() 现在是阻塞的，两次启动都需要放到后台 goroutine 中。
// 验证重复 Start() 不会 panic 且 Stop() 能正确清理所有资源。
func TestWebhookServerAdapter_DoubleStart(t *testing.T) {
	adapter := NewWebhookServerAdapter(":0", newTestBotInfo())

	ctx := t.Context()

	handler := func(_ platform.Event) {}

	// 两次 Start 都放到后台 goroutine，避免阻塞测试主 goroutine
	go adapter.Start(ctx, handler) //nolint:errcheck
	go adapter.Start(ctx, handler) //nolint:errcheck

	// 等待适配器完成初始化（两次 Start 任意一次成功后才能 Stop）
	time.Sleep(200 * time.Millisecond)

	// Stop 必须能正常清理所有资源，不论有多少次 Start 调用
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	err := adapter.Stop(stopCtx)
	assert.NoError(t, err)
}

// TestWebhookServerAdapter_StopBeforeStart tests stopping before start
func TestWebhookServerAdapter_StopBeforeStart(t *testing.T) {
	adapter := NewWebhookServerAdapter(":0", newTestBotInfo())
	err := adapter.Stop(context.Background())
	assert.NoError(t, err)
}
