package lifecycle

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestManager_RollbackWithTimeout tests that rollback uses independent timeout
func TestManager_RollbackWithTimeout(t *testing.T) {
	t.Run("rollback_uses_independent_timeout", func(t *testing.T) {
		manager := NewManager()

		// Create components
		comp1Started := false
		comp2Started := false
		comp1Stopped := false
		comp1 := NewSimpleComponent("comp1",
			func(ctx context.Context) error {
				comp1Started = true
				return nil
			},
			func(ctx context.Context) error {
				comp1Stopped = true
				return nil
			},
		)

		comp2 := NewSimpleComponent("comp2",
			func(ctx context.Context) error {
				comp2Started = true
				return errors.New("comp2 start failed")
			},
			func(ctx context.Context) error {

				return nil
			},
		)

		manager.Register(comp1)
		manager.Register(comp2)

		// Use a canceled context for Start
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		// Start should fail and rollback comp1
		err := manager.Start(ctx)
		assert.Error(t, err)
		assert.True(t, comp1Started, "comp1 should be started")
		// comp2.Start() was called (that's where it set comp2Started=true and returned error)
		assert.True(t, comp2Started, "comp2 Start function was called (even though it failed)")

		// Wait a bit for rollback to complete
		time.Sleep(100 * time.Millisecond)

		// comp1 should be stopped despite ctx being canceled
		// Note: comp2's Start function was called (it's where the error occurred)
		assert.True(t, comp1Stopped, "comp1 should be rolled back even with canceled context")
	})
	t.Run("rollback_with_per_component_timeout", func(t *testing.T) {
		manager := NewManager()

		comp1Stopped := false
		comp2Stopped := false

		// comp1 takes too long to stop but should timeout
		comp1 := NewSimpleComponent("comp1",
			func(ctx context.Context) error {
				return nil
			},
			func(ctx context.Context) error {
				// Try to block for 15 seconds, but respect context timeout
				timer := time.NewTimer(15 * time.Second)
				defer timer.Stop()

				t.Logf("comp1 Stop started, waiting for timeout or context done")
				select {
				case <-timer.C:
					// If we reach here, timeout didn't work
					t.Logf("comp1 Stop: timer fired after 15s")
					comp1Stopped = true
					return nil
				case <-ctx.Done():
					// Context timeout hit (expected after 10s)
					t.Logf("comp1 Stop: context cancelled: %v", ctx.Err())
					return ctx.Err()
				}
			},
		)

		comp2 := NewSimpleComponent("comp2",
			func(ctx context.Context) error {
				// Don't set comp2Started here since we want to test that comp2 didn't start
				return errors.New("comp2 start failed")
			},
			func(ctx context.Context) error {
				comp2Stopped = true
				return nil
			},
		)

		manager.Register(comp1)
		manager.Register(comp2)

		// Start should fail on comp2
		start := time.Now()
		err := manager.Start(context.Background())
		elapsed := time.Since(start)

		assert.Error(t, err)

		// Rollback should take about 10 seconds (comp1 Stop timeout)
		// Wait for rollback to complete
		assert.GreaterOrEqual(t, elapsed.Seconds(), 10.0, "rollback should wait for comp1 Stop timeout")
		assert.Less(t, elapsed.Seconds(), 12.0, "rollback should not wait full 15 seconds")

		// comp1.Stop() was interrupted by timeout, so comp1Stopped should still be false
		assert.False(t, comp1Stopped, "comp1 stop should timeout before completing 15s sleep")
		assert.False(t, comp2Stopped, "comp2 should not be started, so not stopped")
	})

	t.Run("rollback_continues_on_error", func(t *testing.T) {
		manager := NewManager()

		comp1Stopped := false
		comp2Stopped := false

		comp1 := NewSimpleComponent("comp1",
			func(ctx context.Context) error {
				return nil
			},
			func(ctx context.Context) error {
				comp1Stopped = true
				return errors.New("comp1 stop error")
			},
		)

		comp2 := NewSimpleComponent("comp2",
			func(ctx context.Context) error {
				return nil
			},
			func(ctx context.Context) error {
				comp2Stopped = true
				return nil
			},
		)

		comp3 := NewSimpleComponent("comp3",
			func(ctx context.Context) error {
				// Start fails
				return errors.New("comp3 start failed")
			},
			func(ctx context.Context) error {
				return nil
			},
		)

		manager.Register(comp1)
		manager.Register(comp2)
		manager.Register(comp3)

		// Start should fail on comp3
		err := manager.Start(context.Background())
		assert.Error(t, err)

		// Wait for rollback to complete
		time.Sleep(100 * time.Millisecond)

		// Both comp1 and comp2 should attempt to stop
		// even though comp1 returns an error
		assert.True(t, comp1Stopped, "comp1 should attempt to stop")
		assert.True(t, comp2Stopped, "comp2 should stop despite comp1 error")
	})

	t.Run("rollback_with_all_components_blocking", func(t *testing.T) {
		manager := NewManager()

		var stopAttempts atomic.Int32

		comp1 := NewSimpleComponent("comp1",
			func(ctx context.Context) error {
				return nil
			},
			func(ctx context.Context) error {
				stopAttempts.Add(1)
				<-ctx.Done() // Block until timeout
				return ctx.Err()
			},
		)

		comp2 := NewSimpleComponent("comp2",
			func(ctx context.Context) error {
				return errors.New("comp2 start failed")
			},
			func(ctx context.Context) error {
				stopAttempts.Add(1)
				return nil
			},
		)

		manager.Register(comp1)
		manager.Register(comp2)

		// Start should fail and trigger rollback
		start := time.Now()
		err := manager.Start(context.Background())
		elapsed := time.Since(start)

		assert.Error(t, err)
		// Rollback should complete within reasonable time (10s per component + overhead)
		assert.Less(t, elapsed, 15*time.Second, "Rollback should not block indefinitely")
		assert.Equal(t, int32(1), stopAttempts.Load(), "comp1 should attempt to stop")
	})
}

// TestManager_RollbackCompleteness tests that rollback is complete
func TestManager_RollbackCompleteness(t *testing.T) {
	t.Run("rollback_order_is_reverse", func(t *testing.T) {
		manager := NewManager()

		var stopOrder []string
		var mu sync.Mutex

		comp1 := NewSimpleComponent("comp1",
			func(ctx context.Context) error {
				return nil
			},
			func(ctx context.Context) error {
				mu.Lock()
				stopOrder = append(stopOrder, "comp1")
				mu.Unlock()
				return nil
			},
		)

		comp2 := NewSimpleComponent("comp2",
			func(ctx context.Context) error {
				return nil
			},
			func(ctx context.Context) error {
				mu.Lock()
				stopOrder = append(stopOrder, "comp2")
				mu.Unlock()
				return nil
			},
		)

		comp3 := NewSimpleComponent("comp3",
			func(ctx context.Context) error {
				return errors.New("comp3 start failed")
			},
			func(ctx context.Context) error {
				mu.Lock()
				stopOrder = append(stopOrder, "comp3")
				mu.Unlock()
				return nil
			},
		)

		manager.Register(comp1)
		manager.Register(comp2)
		manager.Register(comp3)

		err := manager.Start(context.Background())
		assert.Error(t, err)

		// Wait for rollback
		time.Sleep(100 * time.Millisecond)

		// Rollback should stop in reverse order: comp2, comp1
		assert.Equal(t, []string{"comp2", "comp1"}, stopOrder, "Rollback should stop in reverse order")
	})

	t.Run("rollback_after_partial_start", func(t *testing.T) {
		manager := NewManager()

		comp1Started := false
		comp2Started := false
		comp4Started := false

		comp1Stopped := false
		comp2Stopped := false

		comp1 := NewSimpleComponent("comp1", func(ctx context.Context) error {
			comp1Started = true
			return nil
		}, func(ctx context.Context) error {
			comp1Stopped = true
			return nil
		})

		comp2 := NewSimpleComponent("comp2", func(ctx context.Context) error {
			comp2Started = true
			return nil
		}, func(ctx context.Context) error {
			comp2Stopped = true
			return nil
		})

		comp3 := NewSimpleComponent("comp3", func(ctx context.Context) error {
			// comp3Started = true  // Not checked
			return errors.New("comp3 failed")
		}, func(ctx context.Context) error {
			return nil
		})

		comp4 := NewSimpleComponent("comp4", func(ctx context.Context) error {
			comp4Started = true
			return nil
		}, func(ctx context.Context) error {
			return nil
		})

		manager.Register(comp1)
		manager.Register(comp2)
		manager.Register(comp3)
		manager.Register(comp4)

		err := manager.Start(context.Background())
		assert.Error(t, err)

		// Wait for rollback
		time.Sleep(100 * time.Millisecond)

		// Check which components started
		assert.True(t, comp1Started)
		assert.True(t, comp2Started)
		// comp3 start is attempted but fails
		assert.False(t, comp4Started, "comp4 should not start after comp3 fails")

		// Check which components were stopped during rollback
		assert.True(t, comp1Stopped, "comp1 should be stopped during rollback")
		assert.True(t, comp2Stopped, "comp2 should be stopped during rollback")
	})
}
