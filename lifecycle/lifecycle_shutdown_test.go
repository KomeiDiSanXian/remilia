package lifecycle

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManager_StopWithTimeout(t *testing.T) {
	t.Run("stop_respects_context_timeout", func(t *testing.T) {
		m := NewManager()

		// Component that stops quickly
		fastComp := NewSimpleComponent(
			"fast",
			func(ctx context.Context) error { return nil },
			func(ctx context.Context) error {
				time.Sleep(50 * time.Millisecond)
				return nil
			},
		)

		// Component that takes too long
		slowComp := NewSimpleComponent(
			"slow",
			func(ctx context.Context) error { return nil },
			func(ctx context.Context) error {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(10 * time.Second):
					return nil
				}
			},
		)

		m.Register(fastComp)
		m.Register(slowComp)

		err := m.Start(context.Background())
		require.NoError(t, err)
		assert.Equal(t, StateRunning, m.State())

		// Try to stop with short timeout
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()

		err = m.Stop(ctx)
		// Should get timeout error because slow component times out
		assert.Error(t, err)
		assert.Equal(t, StateFailed, m.State())
	})

	t.Run("stop_handles_component_timeout", func(t *testing.T) {
		m := NewManager()

		var stopCalled atomic.Bool
		blockingComp := NewSimpleComponent(
			"blocking",
			func(ctx context.Context) error { return nil },
			func(ctx context.Context) error {
				stopCalled.Store(true)
				// Each component gets 10s timeout, but we'll exceed it
				<-ctx.Done()
				return ctx.Err()
			},
		)

		m.Register(blockingComp)

		err := m.Start(context.Background())
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		err = m.Stop(ctx)
		assert.Error(t, err)
		assert.True(t, stopCalled.Load(), "Stop should have been called")
	})

	t.Run("stop_continues_on_component_error", func(t *testing.T) {
		m := NewManager()

		var comp1Stopped, comp2Stopped, comp3Stopped atomic.Bool

		comp1 := NewSimpleComponent(
			"comp1",
			func(ctx context.Context) error { return nil },
			func(ctx context.Context) error {
				comp1Stopped.Store(true)
				return nil
			},
		)

		comp2 := NewSimpleComponent(
			"comp2",
			func(ctx context.Context) error { return nil },
			func(ctx context.Context) error {
				comp2Stopped.Store(true)
				return errors.New("comp2 stop error")
			},
		)

		comp3 := NewSimpleComponent(
			"comp3",
			func(ctx context.Context) error { return nil },
			func(ctx context.Context) error {
				comp3Stopped.Store(true)
				return nil
			},
		)

		m.Register(comp1)
		m.Register(comp2)
		m.Register(comp3)

		err := m.Start(context.Background())
		require.NoError(t, err)

		err = m.Stop(context.Background())
		assert.Error(t, err)

		// All components should attempt to stop despite error
		assert.True(t, comp1Stopped.Load())
		assert.True(t, comp2Stopped.Load())
		assert.True(t, comp3Stopped.Load())
	})

	t.Run("component_gets_individual_timeout", func(t *testing.T) {
		m := NewManager()

		var stopAttempted atomic.Bool
		blockingComp := NewSimpleComponent(
			"blocking",
			func(ctx context.Context) error { return nil },
			func(ctx context.Context) error {
				stopAttempted.Store(true)
				// Try to block longer than the per-component timeout (10s)
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(20 * time.Second):
					return nil
				}
			},
		)

		m.Register(blockingComp)

		err := m.Start(context.Background())
		require.NoError(t, err)

		// Use a longer overall timeout, but component should get 10s max
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		startTime := time.Now()
		err = m.Stop(ctx)
		duration := time.Since(startTime)

		assert.Error(t, err)
		assert.True(t, stopAttempted.Load())
		// Should take ~10s (per-component timeout), not 20s
		assert.Greater(t, duration, 9*time.Second)
		assert.Less(t, duration, 12*time.Second)
	})
}

func TestManager_ConcurrentStop(t *testing.T) {
	t.Run("concurrent_stop_calls", func(t *testing.T) {
		m := NewManager()

		comp := NewSimpleComponent(
			"comp",
			func(ctx context.Context) error { return nil },
			func(ctx context.Context) error {
				time.Sleep(100 * time.Millisecond)
				return nil
			},
		)

		m.Register(comp)

		err := m.Start(context.Background())
		require.NoError(t, err)

		// Try to stop concurrently
		var err1, err2 error
		done := make(chan struct{})

		go func() {
			err1 = m.Stop(context.Background())
			done <- struct{}{}
		}()

		go func() {
			time.Sleep(10 * time.Millisecond)
			err2 = m.Stop(context.Background())
			done <- struct{}{}
		}()

		<-done
		<-done

		// One should succeed, one should fail with invalid state
		if err1 == nil {
			assert.Error(t, err2)
		} else {
			assert.NoError(t, err2)
		}
	})
}

func TestManager_StateTransitionDuringStop(t *testing.T) {
	t.Run("state_transitions_correctly", func(t *testing.T) {
		m := NewManager()

		var statesDuringStop []State
		comp := NewSimpleComponent(
			"comp",
			func(ctx context.Context) error { return nil },
			func(ctx context.Context) error {
				// Check state during stop
				statesDuringStop = append(statesDuringStop, m.State())
				time.Sleep(50 * time.Millisecond)
				statesDuringStop = append(statesDuringStop, m.State())
				return nil
			},
		)

		m.Register(comp)

		err := m.Start(context.Background())
		require.NoError(t, err)
		assert.Equal(t, StateRunning, m.State())

		err = m.Stop(context.Background())
		assert.NoError(t, err)

		assert.Equal(t, StateStopped, m.State())
		// During stop, state should be StateStopping
		assert.Contains(t, statesDuringStop, StateStopping)
	})
}
