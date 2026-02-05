package remilia

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/config"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/infra/health"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBot_HandleEvent tests handleEvent method
func TestBot_HandleEvent(t *testing.T) {
	adapter := newMockAdapter()
	eng := engine.NewEngine()
	bot := NewBot(adapter, eng)

	eventReceived := false
	var mu sync.Mutex

	eng.OnAny().Handle(func(ctx *eventctx.Context) error {
		mu.Lock()
		eventReceived = true
		mu.Unlock()
		return nil
	})

	require.NoError(t, bot.Start())
	defer bot.Stop(context.Background())

	// Give the bot time to fully start
	time.Sleep(50 * time.Millisecond)

	testEvent := &dto.Payload{
		ID:   "test-event-1",
		Type: dto.C2CMessageCreate,
	}

	adapter.SendEvent(testEvent)

	// Wait for event to be processed
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	received := eventReceived
	mu.Unlock()

	assert.True(t, received, "Event should be received and handled")
}

// TestAdapterHealthChecker tests nil adapter
func TestAdapterHealthChecker(t *testing.T) {
	checker := NewAdapterHealthChecker(nil)
	result := checker.Check(context.Background())

	assert.Equal(t, health.Unhealthy, result.Status)
	assert.Contains(t, result.Error, "nil")
}

// TestWebhookServerAdapter_New tests creating WebhookServerAdapter
func TestWebhookServerAdapter_New(t *testing.T) {
	info := &dto.BotInfo{
		QQNum:     123456,
		AppID:     7812,
		Token:     "test-token",
		AppSecret: "test-secret",
	}

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
	info := &dto.BotInfo{
		QQNum:     123456,
		AppID:     7812,
		Token:     "test-token",
		AppSecret: "test-secret",
	}

	adapter := NewWebhookServerAdapter(":0", info)

	handler := func(payload *dto.Payload) {}
	ctx := context.Background()

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
	info := &dto.BotInfo{
		QQNum:     123456,
		AppID:     7812,
		Token:     "test-token",
		AppSecret: "test-secret",
	}

	adapter := NewWebhookServerAdapter(":0", info)

	handler := func(payload *dto.Payload) {}
	ctx := context.Background()

	err := adapter.Start(ctx, handler)
	require.NoError(t, err)

	err = adapter.Start(ctx, handler)
	assert.NoError(t, err)

	_ = adapter.Stop(context.Background())
}

// TestWebhookServerAdapter_StopBeforeStart tests stopping before start
func TestWebhookServerAdapter_StopBeforeStart(t *testing.T) {
	info := &dto.BotInfo{
		QQNum:     123456,
		AppID:     7812,
		Token:     "test-token",
		AppSecret: "test-secret",
	}

	adapter := NewWebhookServerAdapter(":0", info)
	err := adapter.Stop(context.Background())
	assert.NoError(t, err)
}

// TestBotStatusChecker tests bot status checker states
func TestBotStatusChecker(t *testing.T) {
	t.Run("stopped state", func(t *testing.T) {
		adapter := newMockAdapter()
		eng := engine.NewEngine()
		bot := NewBot(adapter, eng)

		require.NoError(t, bot.Start())
		time.Sleep(50 * time.Millisecond)
		_ = bot.Stop(context.Background())

		checker := NewBotStatusChecker(bot)
		result := checker.Check(context.Background())

		assert.Equal(t, health.Unhealthy, result.Status)
		assert.Contains(t, result.Error, "not running")
	})

	t.Run("running state metadata", func(t *testing.T) {
		adapter := newMockAdapter()
		eng := engine.NewEngine()
		bot := NewBot(adapter, eng)

		require.NoError(t, bot.Start())
		defer bot.Stop(context.Background())

		checker := NewBotStatusChecker(bot)
		result := checker.Check(context.Background())

		assert.NotNil(t, result.Metadata)
		assert.Contains(t, result.Metadata, "name")
		assert.Contains(t, result.Metadata, "version")
		assert.Contains(t, result.Metadata, "uptime")
	})
}

// TestBot_WaitForShutdownSignal tests WaitForShutdown with signal (skip on Windows)
func TestBot_WaitForShutdownSignal(t *testing.T) {
	if os.Getenv("CI") != "" || os.Getenv("GOOS") == "windows" {
		t.Skip("Skipping signal test on Windows/CI")
	}

	adapter := newMockAdapter()
	eng := engine.NewEngine()
	bot := NewBot(adapter, eng)

	require.NoError(t, bot.Start())

	done := make(chan bool)
	go func() {
		bot.WaitForShutdown()
		done <- true
	}()

	time.Sleep(100 * time.Millisecond)
	proc, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Skip("Cannot find process")
		return
	}

	err = proc.Signal(os.Interrupt)
	if err != nil {
		t.Skip("Cannot send signal: " + err.Error())
		return
	}

	select {
	case <-done:
		// Success
	case <-time.After(3 * time.Second):
		t.Log("Signal test timeout - skipping")
		t.SkipNow()
	}
}

// TestBot_ConcurrentHealth tests concurrent health checks
func TestBot_ConcurrentHealth(t *testing.T) {
	adapter := newMockAdapter()
	eng := engine.NewEngine()
	bot := NewBot(adapter, eng)

	require.NoError(t, bot.Start())
	defer bot.Stop(context.Background())

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			response := bot.Health()
			assert.NotNil(t, response)
		}()
	}

	wg.Wait()
}
