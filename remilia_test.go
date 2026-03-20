package remilia

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/lifecycle"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockAdapter is a test adapter implementing engine.PlatformAdapter
type mockAdapter struct {
	startErr         error
	shutdownErr      error
	started          bool
	shutdown         bool
	goroutineStarted bool
	events           chan platform.Event
	mu               sync.Mutex

	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	handler func(platform.Event)
}

func newMockAdapter() *mockAdapter {
	return &mockAdapter{
		events: make(chan platform.Event, 10),
		done:   make(chan struct{}),
	}
}

func (m *mockAdapter) Platform() string { return "test" }

func (m *mockAdapter) Sender() platform.Sender { return &platform.NoopSender{} }

func (m *mockAdapter) StartPlatform(ctx context.Context, handler func(platform.Event)) error {
	m.mu.Lock()
	if m.startErr != nil {
		m.mu.Unlock()
		return m.startErr
	}
	m.started = true
	m.goroutineStarted = true
	m.handler = handler

	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.mu.Unlock()

	go func() {
		defer close(m.done)
		for {
			select {
			case <-m.ctx.Done():
				return
			case event, ok := <-m.events:
				if !ok {
					return
				}
				if event != nil && handler != nil {
					handler(event)
				}
			}
		}
	}()

	return nil
}

func (m *mockAdapter) SendEvent(event platform.Event) {
	m.mu.Lock()
	started := m.started
	shutdown := m.shutdown
	m.mu.Unlock()

	if !started || shutdown {
		return
	}

	select {
	case m.events <- event:
	default:
	}
}

func (m *mockAdapter) Stop(_ context.Context) error {
	m.mu.Lock()

	if m.shutdownErr != nil {
		m.mu.Unlock()
		return m.shutdownErr
	}

	if m.shutdown {
		m.mu.Unlock()
		return nil
	}

	m.shutdown = true
	goroutineStarted := m.goroutineStarted
	m.mu.Unlock()

	if m.cancel != nil {
		m.cancel()
	}

	if goroutineStarted {
		<-m.done
	}

	close(m.events)

	return nil
}

// TestNewBot tests creating a new bot
func TestNewBot(t *testing.T) {
	adapter := newMockAdapter()
	eng := engine.NewEngine()

	bot := NewBot(adapter, eng)

	require.NotNil(t, bot)
	assert.NotNil(t, bot.engine)
	assert.NotNil(t, bot.adapter)
	assert.NotNil(t, bot.lifecycle)
	assert.NotNil(t, bot.health)
	assert.NotNil(t, bot.config)
	assert.False(t, bot.IsRunning())
}

// TestNewBot_WithOptions tests creating bot with options
func TestNewBot_WithOptions(t *testing.T) {
	adapter := newMockAdapter()
	eng := engine.NewEngine()

	bot := NewBot(adapter, eng,
		WithName("test-bot"),
		WithVersion("1.0.0"),
		WithDebug(true),
	)

	require.NotNil(t, bot)
	assert.Equal(t, "test-bot", bot.config.Name)
	assert.Equal(t, "1.0.0", bot.config.Version)
	assert.True(t, bot.config.Debug)
}

// TestBot_Start tests starting the bot
func TestBot_Start(t *testing.T) {
	t.Run("successful start", func(t *testing.T) {
		adapter := newMockAdapter()
		eng := engine.NewEngine()
		bot := NewBot(adapter, eng)

		err := bot.Start()
		require.NoError(t, err)
		assert.True(t, bot.IsRunning())

		// Cleanup
		_ = bot.Stop(context.Background())
	})

	t.Run("double start", func(t *testing.T) {
		adapter := newMockAdapter()
		eng := engine.NewEngine()
		bot := NewBot(adapter, eng)

		err := bot.Start()
		require.NoError(t, err)

		// Second start should not error
		err = bot.Start()
		assert.NoError(t, err)

		_ = bot.Stop(context.Background())
	})

	t.Run("start with adapter error", func(t *testing.T) {
		adapter := newMockAdapter()
		adapter.startErr = errors.New("adapter start failed")
		eng := engine.NewEngine()
		bot := NewBot(adapter, eng)

		// In v2, adapter.Start() runs in OnRun (async), so Bot.Start() succeeds
		err := bot.Start()
		require.NoError(t, err, "Bot.Start() should succeed even if adapter will fail later")
		assert.True(t, bot.IsRunning(), "Bot should be running")

		// The adapter error will be logged but not propagate to Bot.Start()
		// Wait a bit and then stop
		time.Sleep(100 * time.Millisecond)

		_ = bot.Stop(context.Background())
	})
}

// TestBot_Shutdown tests shutting down the bot
func TestBot_Shutdown(t *testing.T) {
	t.Run("successful shutdown", func(t *testing.T) {
		adapter := newMockAdapter()
		eng := engine.NewEngine()
		bot := NewBot(adapter, eng)

		require.NoError(t, bot.Start())
		time.Sleep(50 * time.Millisecond)

		ctx := context.Background()
		err := bot.Stop(ctx)
		assert.NoError(t, err)
		assert.False(t, bot.IsRunning())
	})

	t.Run("shutdown when not running", func(t *testing.T) {
		adapter := newMockAdapter()
		eng := engine.NewEngine()
		bot := NewBot(adapter, eng)

		ctx := context.Background()
		err := bot.Stop(ctx)
		assert.NoError(t, err)
	})

	t.Run("shutdown with adapter error", func(t *testing.T) {
		adapter := newMockAdapter()
		adapter.shutdownErr = errors.New("shutdown failed")
		eng := engine.NewEngine()
		bot := NewBot(adapter, eng)

		require.NoError(t, bot.Start())
		time.Sleep(50 * time.Millisecond)

		ctx := context.Background()
		err := bot.Stop(ctx)
		require.Error(t, err)
		assert.False(t, bot.IsRunning())
	})
}

// TestBot_IsRunning tests checking bot running state
func TestBot_IsRunning(t *testing.T) {
	adapter := newMockAdapter()
	eng := engine.NewEngine()
	bot := NewBot(adapter, eng)

	assert.False(t, bot.IsRunning())

	require.NoError(t, bot.Start())
	assert.True(t, bot.IsRunning())

	_ = bot.Stop(context.Background())
	assert.False(t, bot.IsRunning())
}

// TestBot_Uptime tests uptime calculation
func TestBot_Uptime(t *testing.T) {
	t.Run("uptime when running", func(t *testing.T) {
		adapter := newMockAdapter()
		eng := engine.NewEngine()
		bot := NewBot(adapter, eng)

		require.NoError(t, bot.Start())
		time.Sleep(100 * time.Millisecond)

		uptime := bot.Uptime()
		assert.GreaterOrEqual(t, uptime, 100*time.Millisecond)

		_ = bot.Stop(context.Background())
	})

	t.Run("uptime when not started", func(t *testing.T) {
		adapter := newMockAdapter()
		eng := engine.NewEngine()
		bot := NewBot(adapter, eng)

		uptime := bot.Uptime()
		assert.Equal(t, time.Duration(0), uptime)
	})

	t.Run("uptime after shutdown", func(t *testing.T) {
		adapter := newMockAdapter()
		eng := engine.NewEngine()
		bot := NewBot(adapter, eng)

		require.NoError(t, bot.Start())
		time.Sleep(100 * time.Millisecond)
		_ = bot.Stop(context.Background())

		uptime := bot.Uptime()
		assert.GreaterOrEqual(t, uptime, 100*time.Millisecond)
	})
}

// TestBot_Engine tests getting engine
func TestBot_Engine(t *testing.T) {
	adapter := newMockAdapter()
	eng := engine.NewEngine()
	bot := NewBot(adapter, eng)

	assert.Equal(t, eng, bot.Engine())
}

// TestBot_Config tests getting config
func TestBot_Config(t *testing.T) {
	adapter := newMockAdapter()
	eng := engine.NewEngine()
	bot := NewBot(adapter, eng, WithName("test-bot"))

	config := bot.Config()
	require.NotNil(t, config)
	assert.Equal(t, "test-bot", config.Name)
}

// TestBot_State tests getting lifecycle state
func TestBot_State(t *testing.T) {
	adapter := newMockAdapter()
	eng := engine.NewEngine()
	bot := NewBot(adapter, eng)

	state := bot.State()
	assert.Equal(t, lifecycle.StateCreated, state)

	require.NoError(t, bot.Start())
	state = bot.State()
	assert.Equal(t, lifecycle.StateRunning, state)

	_ = bot.Stop(context.Background())
}

// TestBot_ConvenienceMethods tests convenience methods
func TestBot_ConvenienceMethods(t *testing.T) {
	adapter := newMockAdapter()
	eng := engine.NewEngine()
	bot := NewBot(adapter, eng)

	t.Run("OnAny", func(t *testing.T) {
		matcher := bot.OnAny()
		assert.NotNil(t, matcher)
	})

	t.Run("OnEventKind_PrivateMessage", func(t *testing.T) {
		matcher := bot.OnEventKind(platform.EventKindPrivateMessage)
		assert.NotNil(t, matcher)
	})

	t.Run("OnEventKind_GroupMessage", func(t *testing.T) {
		matcher := bot.OnEventKind(platform.EventKindGroupMessage)
		assert.NotNil(t, matcher)
	})

	t.Run("Engine_On", func(t *testing.T) {
		matcher := bot.Engine().On("C2C_MESSAGE_CREATE")
		assert.NotNil(t, matcher)
	})
}

// TestOptions tests option functions
func TestOptions(t *testing.T) {
	adapter := newMockAdapter()
	eng := engine.NewEngine()

	t.Run("WithConfig", func(t *testing.T) {
		config := &Config{
			Name:    "custom-bot",
			Version: "2.0.0",
			Debug:   true,
		}

		bot := NewBot(adapter, eng, WithConfig(config))
		assert.Equal(t, "custom-bot", bot.config.Name)
		assert.Equal(t, "2.0.0", bot.config.Version)
		assert.True(t, bot.config.Debug)
	})

	t.Run("WithName", func(t *testing.T) {
		bot := NewBot(adapter, eng, WithName("named-bot"))
		assert.Equal(t, "named-bot", bot.config.Name)
	})

	t.Run("WithVersion", func(t *testing.T) {
		bot := NewBot(adapter, eng, WithVersion("3.0.0"))
		assert.Equal(t, "3.0.0", bot.config.Version)
	})

	t.Run("WithDebug", func(t *testing.T) {
		bot := NewBot(adapter, eng, WithDebug(true))
		assert.True(t, bot.config.Debug)
	})

	t.Run("WithAdapter", func(t *testing.T) {
		newAdapter := newMockAdapter()
		bot := NewBot(adapter, eng, WithAdapter(newAdapter))
		// Note: WithAdapter doesn't replace adapter in NewBot
		// It's used in factory NewBotWithDefault() function
		assert.NotNil(t, bot.adapter)
	})

	t.Run("WithEngine", func(t *testing.T) {
		newEngine := engine.NewEngine()
		bot := NewBot(adapter, eng, WithEngine(newEngine))
		// Note: WithEngine doesn't replace engine in NewBot
		// It's used in factory NewBotWithDefault() function
		assert.NotNil(t, bot.engine)
	})
}

// TestHealthChecker tests health checker
func TestHealthChecker(t *testing.T) {
	t.Run("health when not running", func(t *testing.T) {
		adapter := newMockAdapter()
		eng := engine.NewEngine()
		bot := NewBot(adapter, eng)

		health := bot.Health()
		require.NotNil(t, health)
		assert.NotEqual(t, "", string(health.Status))
		// Bot not running check
	})

	t.Run("health when running", func(t *testing.T) {
		adapter := newMockAdapter()
		eng := engine.NewEngine()
		bot := NewBot(adapter, eng)

		require.NoError(t, bot.Start())
		time.Sleep(50 * time.Millisecond)

		health := bot.Health()
		require.NotNil(t, health)
		assert.Equal(t, "healthy", string(health.Status))
		// Bot running check
		// engine check
		// Adapter check
		// Check has Time field
		assert.NotZero(t, health.Time)

		_ = bot.Stop(context.Background())
	})
}

// TestHealthCheck tests creating health check instance
func TestHealthCheck(t *testing.T) {
	adapter := newMockAdapter()
	eng := engine.NewEngine()
	bot := NewBot(adapter, eng)

	healthCheck := bot.HealthCheck()
	require.NotNil(t, healthCheck)
}

// BenchmarkBot_Start benchmarks bot startup
func BenchmarkBot_Start(b *testing.B) {
	adapter := newMockAdapter()
	eng := engine.NewEngine()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		bot := NewBot(adapter, eng)
		_ = bot.Start()
		_ = bot.Stop(context.Background())
	}
}

// BenchmarkBot_Health benchmarks health check
func BenchmarkBot_Health(b *testing.B) {
	adapter := newMockAdapter()
	eng := engine.NewEngine()
	bot := NewBot(adapter, eng)
	_ = bot.Start()
	defer func() { _ = bot.Stop(context.Background()) }()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = bot.Health()
	}
}
