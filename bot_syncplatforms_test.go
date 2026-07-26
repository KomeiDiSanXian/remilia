package remilia

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockPlatformAdapter is a simple mock that records lifecycle events.
type mockPlatformAdapter struct {
	name      string
	mu        sync.Mutex
	started   bool
	stopped   bool
	startErr  error
	stopErr   error
	startedCh chan struct{} // closed after m.started = true
}

func newMockPlatform(name string) *mockPlatformAdapter {
	return &mockPlatformAdapter{
		name:      name,
		startedCh: make(chan struct{}),
	}
}

func (m *mockPlatformAdapter) Platform() string                    { return m.name }
func (m *mockPlatformAdapter) Sender() platform.Sender             { return &platform.NoopSender{} }
func (m *mockPlatformAdapter) Capabilities() platform.Capabilities { return platform.Capabilities{} }
func (m *mockPlatformAdapter) IsRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.started && !m.stopped
}
func (m *mockPlatformAdapter) WaitStarted(t *testing.T) {
	t.Helper()
	select {
	case <-m.startedCh:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for adapter start")
	}
}

// Start 使用传入的 ctx 阻塞直到被取消，模拟真实 adapter 行为。
func (m *mockPlatformAdapter) Start(ctx context.Context, _ func(platform.Event)) error {
	m.mu.Lock()
	if m.startErr != nil {
		m.mu.Unlock()
		return m.startErr
	}
	m.started = true
	close(m.startedCh)
	m.mu.Unlock()
	<-ctx.Done()
	return ctx.Err()
}

func (m *mockPlatformAdapter) Stop(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopErr != nil {
		return m.stopErr
	}
	m.stopped = true
	return nil
}

func TestBot_SyncPlatforms_AddAdapter(t *testing.T) {
	eng := engine.NewEngine()
	reg := platform.NewRegistry()
	bot := MustNewBot(nil, eng)
	bot.UsePlatformRegistry(reg)
	require.NoError(t, bot.Start())
	defer func() { _ = bot.Stop(context.Background()) }()

	// Add a new platform adapter
	adapter := newMockPlatform("test-add")
	err := bot.SyncPlatforms(map[string]platform.Adapter{"test-add": adapter})
	assert.NoError(t, err)

	// Verify it's in the registry
	got, ok := reg.Get("test-add")
	assert.True(t, ok)
	assert.Equal(t, adapter, got)

	// Verify it was started
	adapter.WaitStarted(t)
	assert.True(t, adapter.IsRunning())
}

func TestBot_SyncPlatforms_RemoveAdapter(t *testing.T) {
	eng := engine.NewEngine()
	reg := platform.NewRegistry()
	adapter := newMockPlatform("test-remove")
	reg.Register(adapter)
	bot := MustNewBot(nil, eng)
	bot.UsePlatformRegistry(reg)
	require.NoError(t, bot.Start())

	adapter.WaitStarted(t)
	assert.True(t, adapter.IsRunning())

	// Remove the adapter
	err := bot.SyncPlatforms(map[string]platform.Adapter{})
	assert.NoError(t, err)

	// Verify it's removed from registry
	_, ok := reg.Get("test-remove")
	assert.False(t, ok)

	// Verify it was stopped
	adapter.mu.Lock()
	assert.True(t, adapter.stopped)
	adapter.mu.Unlock()

	_ = bot.Stop(context.Background())
}

func TestBot_SyncPlatforms_ReplaceAdapter(t *testing.T) {
	eng := engine.NewEngine()
	reg := platform.NewRegistry()
	oldAdapter := newMockPlatform("test-replace")
	reg.Register(oldAdapter)
	bot := MustNewBot(nil, eng)
	bot.UsePlatformRegistry(reg)
	require.NoError(t, bot.Start())

	oldAdapter.WaitStarted(t)
	assert.True(t, oldAdapter.IsRunning())

	// Replace with new adapter
	newAdapter := newMockPlatform("test-replace")
	err := bot.SyncPlatforms(map[string]platform.Adapter{"test-replace": newAdapter})
	assert.NoError(t, err)

	// Verify old adapter was stopped
	oldAdapter.mu.Lock()
	assert.True(t, oldAdapter.stopped)
	oldAdapter.mu.Unlock()

	// Verify new adapter is in registry and running
	newAdapter.WaitStarted(t)
	got, ok := reg.Get("test-replace")
	assert.True(t, ok)
	assert.Equal(t, newAdapter, got)
	assert.True(t, newAdapter.IsRunning())

	_ = bot.Stop(context.Background())
}

func TestBot_SyncPlatforms_NilDesired(t *testing.T) {
	eng := engine.NewEngine()
	reg := platform.NewRegistry()
	bot := MustNewBot(nil, eng)
	bot.UsePlatformRegistry(reg)
	require.NoError(t, bot.Start())

	err := bot.SyncPlatforms(nil)
	assert.NoError(t, err)

	_ = bot.Stop(context.Background())
}

func TestBot_SyncPlatforms_NoRegistry(t *testing.T) {
	eng := engine.NewEngine()
	bot := MustNewBot(nil, eng)
	require.NoError(t, bot.Start())

	adapter := newMockPlatform("test-noreg")
	err := bot.SyncPlatforms(map[string]platform.Adapter{"test-noreg": adapter})
	assert.Error(t, err)

	_ = bot.Stop(context.Background())
}
