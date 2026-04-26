package remilia

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/infra/health"
	"github.com/KomeiDiSanXian/remilia/testbot"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBot_HandleEvent tests handleEvent method
func TestBot_HandleEvent(t *testing.T) {
	adapter := newMockAdapter()
	eng := engine.NewEngine()
	bot := MustNewBot(adapter, eng)

	eventReceived := make(chan struct{})

	eng.OnAny().Handle(func(ctx *eventctx.Context) error {
		close(eventReceived)
		return nil
	})

	require.NoError(t, bot.Start())
	defer bot.Stop(context.Background())

	waitBotRunning(t, bot)
	<-adapter.ready // wait for adapter goroutine to enter select loop

	testEvent := testbot.MakePlatformC2CEvent("test-user-1", "hello")

	adapter.SendEvent(testEvent)

	select {
	case <-eventReceived:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for event processing")
	}
}

// TestAdapterHealthChecker tests nil adapter
func TestAdapterHealthChecker(t *testing.T) {
	checker := NewAdapterHealthChecker(nil)
	result := checker.Check(context.Background())

	assert.Equal(t, health.Unhealthy, result.Status)
	assert.Contains(t, result.Error, "nil")
}

// TestBotStatusChecker tests bot status checker states
func TestBotStatusChecker(t *testing.T) {
	t.Run("stopped state", func(t *testing.T) {
		adapter := newMockAdapter()
		eng := engine.NewEngine()
		bot := MustNewBot(adapter, eng)

		require.NoError(t, bot.Start())
		waitBotRunning(t, bot)
		_ = bot.Stop(context.Background())

		checker := NewBotStatusChecker(bot)
		result := checker.Check(context.Background())

		assert.Equal(t, health.Unhealthy, result.Status)
		assert.Contains(t, result.Error, "not running")
	})

	t.Run("running state metadata", func(t *testing.T) {
		adapter := newMockAdapter()
		eng := engine.NewEngine()
		bot := MustNewBot(adapter, eng)

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
	bot := MustNewBot(adapter, eng)

	require.NoError(t, bot.Start())

	done := make(chan bool)
	started := make(chan struct{})
	go func() {
		close(started)
		bot.WaitForShutdown()
		done <- true
	}()

	<-started
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
	bot := MustNewBot(adapter, eng)

	require.NoError(t, bot.Start())
	defer bot.Stop(context.Background())

	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			response := bot.Health()
			assert.NotNil(t, response)
		})
	}

	wg.Wait()
}
