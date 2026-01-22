package lifecycle

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockComponent is a test component
type mockComponent struct {
	name       string
	startErr   error
	stopErr    error
	startDelay time.Duration
	stopDelay  time.Duration
	started    bool
	stopped    bool
	mu         sync.Mutex
}

func newMockComponent(name string) *mockComponent {
	return &mockComponent{name: name}
}

func (m *mockComponent) Name() string {
	return m.name
}

func (m *mockComponent) Start(ctx context.Context) error {
	if m.startDelay > 0 {
		select {
		case <-time.After(m.startDelay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.startErr != nil {
		return m.startErr
	}

	m.started = true
	return nil
}

func (m *mockComponent) Stop(ctx context.Context) error {
	if m.stopDelay > 0 {
		select {
		case <-time.After(m.stopDelay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.stopErr != nil {
		return m.stopErr
	}

	m.stopped = true
	return nil
}

func (m *mockComponent) isStarted() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.started
}

func (m *mockComponent) isStopped() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stopped
}

// TestState_String tests state string representation
func TestState_String(t *testing.T) {
	tests := []struct {
		state    State
		expected string
	}{
		{StateCreated, "created"},
		{StateStarting, "starting"},
		{StateRunning, "running"},
		{StateStopping, "stopping"},
		{StateStopped, "stopped"},
		{StateFailed, "failed"},
		{State(999), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.state.String())
		})
	}
}

// TestNewManager tests creating a new manager
func TestNewManager(t *testing.T) {
	manager := NewManager()
	require.NotNil(t, manager)
	assert.Equal(t, StateCreated, manager.State())
	assert.Equal(t, 0, manager.ComponentCount())
}

// TestManager_Register tests registering components
func TestManager_Register(t *testing.T) {
	manager := NewManager()

	comp1 := newMockComponent("comp1")
	comp2 := newMockComponent("comp2")

	manager.Register(comp1)
	assert.Equal(t, 1, manager.ComponentCount())

	manager.Register(comp2)
	assert.Equal(t, 2, manager.ComponentCount())
}

// TestManager_Start tests starting components
func TestManager_Start(t *testing.T) {
	t.Run("successful start", func(t *testing.T) {
		manager := NewManager()
		comp1 := newMockComponent("comp1")
		comp2 := newMockComponent("comp2")

		manager.Register(comp1)
		manager.Register(comp2)

		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)

		assert.Equal(t, StateRunning, manager.State())
		assert.True(t, comp1.isStarted())
		assert.True(t, comp2.isStarted())
	})

	t.Run("start with delay", func(t *testing.T) {
		manager := NewManager()
		comp := newMockComponent("comp")
		comp.startDelay = 50 * time.Millisecond

		manager.Register(comp)

		ctx := context.Background()
		start := time.Now()
		err := manager.Start(ctx)
		duration := time.Since(start)

		require.NoError(t, err)
		assert.GreaterOrEqual(t, duration, 50*time.Millisecond)
		assert.Equal(t, StateRunning, manager.State())
	})

	t.Run("start with context cancellation", func(t *testing.T) {
		manager := NewManager()
		comp := newMockComponent("comp")
		comp.startDelay = 1 * time.Second

		manager.Register(comp)

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		err := manager.Start(ctx)
		require.Error(t, err)
		assert.Equal(t, StateFailed, manager.State())
	})
}

// TestManager_StartFailure tests start failure and rollback
func TestManager_StartFailure(t *testing.T) {
	manager := NewManager()

	comp1 := newMockComponent("comp1")
	comp2 := newMockComponent("comp2")
	comp2.startErr = errors.New("comp2 start failed")
	comp3 := newMockComponent("comp3")

	manager.Register(comp1)
	manager.Register(comp2)
	manager.Register(comp3)

	ctx := context.Background()
	err := manager.Start(ctx)

	require.Error(t, err)

	// Verify error type and content
	var startErr *StartError
	assert.True(t, errors.As(err, &startErr))
	assert.Equal(t, "comp2", startErr.Component)
	assert.Contains(t, err.Error(), "comp2")

	// Verify state is failed
	assert.Equal(t, StateFailed, manager.State())

	// Verify comp1 was rolled back (stopped)
	assert.True(t, comp1.isStopped())

	// Verify comp3 was never started
	assert.False(t, comp3.isStarted())
}

// TestManager_Stop tests stopping components
func TestManager_Stop(t *testing.T) {
	t.Run("successful stop", func(t *testing.T) {
		manager := NewManager()
		comp1 := newMockComponent("comp1")
		comp2 := newMockComponent("comp2")

		manager.Register(comp1)
		manager.Register(comp2)

		ctx := context.Background()
		require.NoError(t, manager.Start(ctx))

		err := manager.Stop(ctx)
		require.NoError(t, err)

		assert.Equal(t, StateStopped, manager.State())
		assert.True(t, comp1.isStopped())
		assert.True(t, comp2.isStopped())
	})

	t.Run("stop in reverse order", func(t *testing.T) {
		manager := NewManager()

		var stopOrder []string
		var mu sync.Mutex

		for i := 1; i <= 3; i++ {
			name := string(rune('0' + i))
			comp := NewSimpleComponent(name,
				func(ctx context.Context) error { return nil },
				func(ctx context.Context) error {
					mu.Lock()
					stopOrder = append(stopOrder, name)
					mu.Unlock()
					return nil
				},
			)
			manager.Register(comp)
		}

		ctx := context.Background()
		require.NoError(t, manager.Start(ctx))
		require.NoError(t, manager.Stop(ctx))

		// Components should stop in reverse order: 3, 2, 1
		assert.Equal(t, []string{"3", "2", "1"}, stopOrder)
	})
}

// TestManager_StopFailure tests stop failure handling
func TestManager_StopFailure(t *testing.T) {
	manager := NewManager()

	comp1 := newMockComponent("comp1")
	comp2 := newMockComponent("comp2")
	comp2.stopErr = errors.New("comp2 stop failed")
	comp3 := newMockComponent("comp3")

	manager.Register(comp1)
	manager.Register(comp2)
	manager.Register(comp3)

	ctx := context.Background()
	require.NoError(t, manager.Start(ctx))

	err := manager.Stop(ctx)

	require.Error(t, err)

	// Verify error type
	var stopErr *StopError
	assert.True(t, errors.As(err, &stopErr))

	// Verify state is failed
	assert.Equal(t, StateFailed, manager.State())

	// All components should attempt to stop even if one fails
	assert.True(t, comp1.isStopped())
	assert.True(t, comp3.isStopped())
}

// TestManager_StateTransitions tests invalid state transitions
func TestManager_StateTransitions(t *testing.T) {
	t.Run("cannot start when running", func(t *testing.T) {
		manager := NewManager()
		manager.Register(newMockComponent("comp"))

		ctx := context.Background()
		require.NoError(t, manager.Start(ctx))

		err := manager.Start(ctx)
		require.Error(t, err)

		var stateErr ErrInvalidState
		assert.True(t, errors.As(err, &stateErr))
		assert.Equal(t, StateRunning, stateErr.Current)
		assert.Equal(t, StateCreated, stateErr.Expected)
	})

	t.Run("cannot stop when not running", func(t *testing.T) {
		manager := NewManager()
		manager.Register(newMockComponent("comp"))

		ctx := context.Background()
		err := manager.Stop(ctx)
		require.Error(t, err)

		var stateErr ErrInvalidState
		assert.True(t, errors.As(err, &stateErr))
		assert.Equal(t, StateCreated, stateErr.Current)
		assert.Equal(t, StateRunning, stateErr.Expected)
	})
}

// TestManager_Uptime tests uptime calculation
func TestManager_Uptime(t *testing.T) {
	t.Run("uptime when running", func(t *testing.T) {
		manager := NewManager()
		manager.Register(newMockComponent("comp"))

		ctx := context.Background()
		require.NoError(t, manager.Start(ctx))

		time.Sleep(100 * time.Millisecond)
		uptime := manager.Uptime()

		assert.GreaterOrEqual(t, uptime, 100*time.Millisecond)
		assert.LessOrEqual(t, uptime, 200*time.Millisecond)
	})

	t.Run("uptime when stopped", func(t *testing.T) {
		manager := NewManager()
		manager.Register(newMockComponent("comp"))

		ctx := context.Background()
		require.NoError(t, manager.Start(ctx))

		time.Sleep(100 * time.Millisecond)
		require.NoError(t, manager.Stop(ctx))

		uptime := manager.Uptime()

		assert.GreaterOrEqual(t, uptime, 100*time.Millisecond)
		assert.LessOrEqual(t, uptime, 200*time.Millisecond)
	})

	t.Run("uptime before start", func(t *testing.T) {
		manager := NewManager()
		uptime := manager.Uptime()
		assert.Equal(t, time.Duration(0), uptime)
	})
}

// TestManager_ComponentCount tests component counting
func TestManager_ComponentCount(t *testing.T) {
	manager := NewManager()
	assert.Equal(t, 0, manager.ComponentCount())

	for i := 0; i < 5; i++ {
		manager.Register(newMockComponent("comp"))
		assert.Equal(t, i+1, manager.ComponentCount())
	}
}

// TestManager_ConcurrentAccess tests concurrent access to manager
func TestManager_ConcurrentAccess(t *testing.T) {
	manager := NewManager()

	// Register components concurrently
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			manager.Register(newMockComponent("comp" + string(rune('0'+id))))
		}(i)
	}
	wg.Wait()

	assert.Equal(t, 10, manager.ComponentCount())

	// Access state concurrently
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = manager.State()
			_ = manager.ComponentCount()
		}()
	}
	wg.Wait()
}

// TestSimpleComponent tests SimpleComponent implementation
func TestSimpleComponent(t *testing.T) {
	t.Run("with both functions", func(t *testing.T) {
		started := false
		stopped := false

		comp := NewSimpleComponent("test",
			func(ctx context.Context) error {
				started = true
				return nil
			},
			func(ctx context.Context) error {
				stopped = true
				return nil
			},
		)

		assert.Equal(t, "test", comp.Name())

		ctx := context.Background()
		require.NoError(t, comp.Start(ctx))
		assert.True(t, started)

		require.NoError(t, comp.Stop(ctx))
		assert.True(t, stopped)
	})

	t.Run("with nil functions", func(t *testing.T) {
		comp := NewSimpleComponent("test", nil, nil)

		ctx := context.Background()
		require.NoError(t, comp.Start(ctx))
		require.NoError(t, comp.Stop(ctx))
	})

	t.Run("with errors", func(t *testing.T) {
		startErr := errors.New("start error")
		stopErr := errors.New("stop error")

		comp := NewSimpleComponent("test",
			func(ctx context.Context) error { return startErr },
			func(ctx context.Context) error { return stopErr },
		)

		ctx := context.Background()
		assert.ErrorIs(t, comp.Start(ctx), startErr)
		assert.ErrorIs(t, comp.Stop(ctx), stopErr)
	})
}

// TestStartError tests StartError type
func TestStartError(t *testing.T) {
	innerErr := errors.New("inner error")
	startErr := &StartError{
		Component: "test-component",
		Err:       innerErr,
	}

	assert.Contains(t, startErr.Error(), "test-component")
	assert.Contains(t, startErr.Error(), "inner error")
	assert.ErrorIs(t, startErr, innerErr)
}

// TestStopError tests StopError type
func TestStopError(t *testing.T) {
	innerErr := errors.New("inner error")
	stopErr := &StopError{Err: innerErr}

	assert.Contains(t, stopErr.Error(), "inner error")
	assert.ErrorIs(t, stopErr, innerErr)
}

// TestErrInvalidState tests ErrInvalidState type
func TestErrInvalidState(t *testing.T) {
	err := ErrInvalidState{
		Current:  StateRunning,
		Expected: StateCreated,
	}

	assert.Contains(t, err.Error(), "running")
	assert.Contains(t, err.Error(), "created")
}

// TestManager_ComplexScenario tests a complex lifecycle scenario
func TestManager_ComplexScenario(t *testing.T) {
	manager := NewManager()

	// Create multiple components
	comp1 := newMockComponent("database")
	comp2 := newMockComponent("cache")
	comp3 := newMockComponent("http-server")

	manager.Register(comp1)
	manager.Register(comp2)
	manager.Register(comp3)

	assert.Equal(t, 3, manager.ComponentCount())
	assert.Equal(t, StateCreated, manager.State())

	// Start all components
	ctx := context.Background()
	err := manager.Start(ctx)
	require.NoError(t, err)
	assert.Equal(t, StateRunning, manager.State())

	// Verify all started
	assert.True(t, comp1.isStarted())
	assert.True(t, comp2.isStarted())
	assert.True(t, comp3.isStarted())

	// Check uptime
	time.Sleep(50 * time.Millisecond)
	uptime := manager.Uptime()
	assert.GreaterOrEqual(t, uptime, 50*time.Millisecond)

	// Stop all components
	err = manager.Stop(ctx)
	require.NoError(t, err)
	assert.Equal(t, StateStopped, manager.State())

	// Verify all stopped
	assert.True(t, comp1.isStopped())
	assert.True(t, comp2.isStopped())
	assert.True(t, comp3.isStopped())
}

// TestManager_RestartAfterStop tests restarting after stop
func TestManager_RestartAfterStop(t *testing.T) {
	manager := NewManager()
	comp := newMockComponent("comp")
	manager.Register(comp)

	ctx := context.Background()

	// First cycle
	require.NoError(t, manager.Start(ctx))
	assert.Equal(t, StateRunning, manager.State())
	require.NoError(t, manager.Stop(ctx))
	assert.Equal(t, StateStopped, manager.State())

	// Reset mock component
	comp.mu.Lock()
	comp.started = false
	comp.stopped = false
	comp.mu.Unlock()

	// Second cycle - should be able to restart
	require.NoError(t, manager.Start(ctx))
	assert.Equal(t, StateRunning, manager.State())
	assert.True(t, comp.isStarted())
}

// BenchmarkManager_Start benchmarks starting components
func BenchmarkManager_Start(b *testing.B) {
	manager := NewManager()
	for i := 0; i < 10; i++ {
		manager.Register(newMockComponent("comp"))
	}

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Reset state for each iteration
		manager.mu.Lock()
		manager.state = StateCreated
		manager.mu.Unlock()

		_ = manager.Start(ctx)

		manager.mu.Lock()
		manager.state = StateRunning
		manager.mu.Unlock()

		_ = manager.Stop(ctx)
	}
}

// BenchmarkManager_Register benchmarks registering components
func BenchmarkManager_Register(b *testing.B) {
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		manager := NewManager()
		for j := 0; j < 100; j++ {
			manager.Register(newMockComponent("comp"))
		}
	}
}
