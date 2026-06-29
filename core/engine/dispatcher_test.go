package engine

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewDispatcher(t *testing.T) {
	d := NewOutboundDispatcher(context.Background(), DispatcherConfig{
		MaxInflight: 32,
		QueueSize:   16,
	})
	defer d.Shutdown(context.Background())

	if d.config.MaxInflight != 32 {
		t.Fatalf("expected MaxInflight 32, got %d", d.config.MaxInflight)
	}
	if d.config.QueueSize != 16 {
		t.Fatalf("expected QueueSize 16, got %d", d.config.QueueSize)
	}
}

func TestNewDispatcherDefaults(t *testing.T) {
	d := NewOutboundDispatcher(context.Background(), DispatcherConfig{})
	defer d.Shutdown(context.Background())

	if d.config.MaxInflight != 512 {
		t.Fatalf("expected default MaxInflight 512, got %d", d.config.MaxInflight)
	}
	if d.config.QueueSize != 64 {
		t.Fatalf("expected default QueueSize 64, got %d", d.config.QueueSize)
	}
	if d.config.SendTimeout != 30*time.Second {
		t.Fatalf("expected default SendTimeout 30s, got %v", d.config.SendTimeout)
	}
}

func TestSubmitAndExecute(t *testing.T) {
	d := NewOutboundDispatcher(context.Background(), DispatcherConfig{
		MaxInflight: 32,
		QueueSize:   16,
	})
	defer d.Shutdown(context.Background())

	var executed atomic.Bool
	err := d.Submit("chat1", func(ctx context.Context) error {
		executed.Store(true)
		return nil
	})
	if err != nil {
		t.Fatalf("Submit failed: %v", err)
	}

	// Wait for execution
	time.Sleep(50 * time.Millisecond)
	if !executed.Load() {
		t.Fatal("task was not executed")
	}
}

func TestSubmitReturnsResult(t *testing.T) {
	d := NewOutboundDispatcher(context.Background(), DispatcherConfig{
		MaxInflight: 32,
		QueueSize:   16,
	})
	defer d.Shutdown(context.Background())

	want := errors.New("custom error")
	err := d.Submit("chat1", func(ctx context.Context) error {
		return want
	})
	if err != nil {
		t.Fatalf("Submit failed: %v", err)
	}

	// Wait for execution (error is captured by hooks, test just ensures Submit returns nil)
	time.Sleep(50 * time.Millisecond)
}

func TestFIFOOrderSameChat(t *testing.T) {
	d := NewOutboundDispatcher(context.Background(), DispatcherConfig{
		MaxInflight: 32,
		QueueSize:   16,
	})
	defer d.Shutdown(context.Background())

	var results []int
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := range 5 {
		wg.Add(1)
		i := i
		err := d.Submit("chat_fifo", func(ctx context.Context) error {
			time.Sleep(10 * time.Millisecond)
			mu.Lock()
			results = append(results, i)
			mu.Unlock()
			wg.Done()
			return nil
		})
		if err != nil {
			t.Fatalf("Submit %d failed: %v", i, err)
		}
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(results) != 5 {
		t.Fatalf("expected 5 results, got %d", len(results))
	}
	for i, v := range results {
		if v != i {
			t.Fatalf("expected FIFO order, results[%d] = %d, want %d", i, v, i)
		}
	}
}

func TestConcurrentChats(t *testing.T) {
	d := NewOutboundDispatcher(context.Background(), DispatcherConfig{
		MaxInflight: 32,
		QueueSize:   16,
	})
	defer d.Shutdown(context.Background())

	done := make(chan struct{})
	var started atomic.Int32

	for i := range 5 {
		chatID := "chat_" + string(rune('A'+i))
		err := d.Submit(chatID, func(ctx context.Context) error {
			started.Add(1)
			time.Sleep(50 * time.Millisecond)
			return nil
		})
		if err != nil {
			t.Fatalf("Submit to %s failed: %v", chatID, err)
		}
	}
	close(done)
	time.Sleep(100 * time.Millisecond)

	if n := started.Load(); n != 5 {
		t.Fatalf("expected all 5 tasks to start, got %d", n)
	}
}

func TestQueueFull(t *testing.T) {
	d := NewOutboundDispatcher(context.Background(), DispatcherConfig{
		MaxInflight: 1,
		QueueSize:   2,
	})
	defer d.Shutdown(context.Background())

	// Submit a blocking task to occupy the inflight slot
	block := make(chan struct{})
	started := make(chan struct{})

	err := d.Submit("chat_full", func(ctx context.Context) error {
		close(started)
		<-block
		return nil
	})
	if err != nil {
		t.Fatalf("first Submit failed: %v", err)
	}
	// Wait for the worker to pop the task and start executing
	<-started

	// Fill the queue (2 slots)
	for i := range 2 {
		err = d.Submit("chat_full", func(ctx context.Context) error {
			return nil
		})
		if err != nil {
			t.Fatalf("queue Submit %d failed: %v", i, err)
		}
	}

	// Queue should be full now
	err = d.Submit("chat_full", func(ctx context.Context) error {
		return nil
	})
	if !errors.Is(err, ErrQueueFull) {
		t.Fatalf("expected ErrQueueFull, got %v", err)
	}

	close(block)
}

func TestSubmitAfterClose(t *testing.T) {
	d := NewOutboundDispatcher(context.Background(), DispatcherConfig{
		MaxInflight: 32,
		QueueSize:   16,
	})
	d.Close()
	err := d.Submit("chat1", func(ctx context.Context) error {
		return nil
	})
	if !errors.Is(err, ErrDispatcherClosed) {
		t.Fatalf("expected ErrDispatcherClosed, got %v", err)
	}
}

func TestShutdownDrainsAllTasks(t *testing.T) {
	d := NewOutboundDispatcher(context.Background(), DispatcherConfig{
		MaxInflight: 32,
		QueueSize:   16,
	})

	var count atomic.Int32
	for range 10 {
		_ = d.Submit("chat_drain", func(ctx context.Context) error {
			time.Sleep(10 * time.Millisecond)
			count.Add(1)
			return nil
		})
	}

	err := d.Shutdown(context.Background())
	if err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}

	if n := count.Load(); n != 10 {
		t.Fatalf("expected all 10 tasks to complete, got %d", n)
	}
}

func TestHooksOnStartOnDone(t *testing.T) {
	var started, done int32
	hooks := DispatcherHooks{
		OnStart: func(chatID string) {
			atomic.AddInt32(&started, 1)
		},
		OnDone: func(info DispatchInfo) {
			atomic.AddInt32(&done, 1)
		},
	}

	d := NewOutboundDispatcher(context.Background(), DispatcherConfig{
		MaxInflight: 32,
		QueueSize:   16,
		Hooks:       hooks,
	})
	defer d.Shutdown(context.Background())

	for range 3 {
		_ = d.Submit("chat_hooks", func(ctx context.Context) error {
			return nil
		})
	}

	time.Sleep(100 * time.Millisecond)
	if n := atomic.LoadInt32(&started); n != 3 {
		t.Fatalf("expected 3 OnStart calls, got %d", n)
	}
	if n := atomic.LoadInt32(&done); n != 3 {
		t.Fatalf("expected 3 OnDone calls, got %d", n)
	}
}

func TestHooksOnRejected(t *testing.T) {
	var rejected int32
	hooks := DispatcherHooks{
		OnRejected: func(chatID string, err error) {
			if errors.Is(err, ErrDispatcherClosed) {
				atomic.AddInt32(&rejected, 1)
			}
		},
	}

	d := NewOutboundDispatcher(context.Background(), DispatcherConfig{
		MaxInflight: 32,
		QueueSize:   16,
		Hooks:       hooks,
	})
	d.Close() // Reject all subsequent submits

	err := d.Submit("chat_rej", func(ctx context.Context) error {
		return nil
	})
	if !errors.Is(err, ErrDispatcherClosed) {
		t.Fatalf("expected ErrDispatcherClosed, got %v", err)
	}
	if n := atomic.LoadInt32(&rejected); n != 1 {
		t.Fatalf("expected 1 OnRejected call, got %d", n)
	}
}

func TestRingBuffer(t *testing.T) {
	r := newRingBuffer(3)

	// Push 3 items
	if !r.push(queuedTask{enqueueAt: time.Now(), run: nil}) {
		t.Fatal("push should succeed")
	}
	if !r.push(queuedTask{enqueueAt: time.Now(), run: nil}) {
		t.Fatal("push should succeed")
	}
	if !r.push(queuedTask{enqueueAt: time.Now(), run: nil}) {
		t.Fatal("push should succeed")
	}
	if r.push(queuedTask{enqueueAt: time.Now(), run: nil}) {
		t.Fatal("push should fail when full")
	}

	// Pop 3 items
	for i := range 3 {
		_, ok := r.pop()
		if !ok {
			t.Fatalf("pop %d should succeed", i)
		}
	}

	// Should be empty
	_, ok := r.pop()
	if ok {
		t.Fatal("pop should fail when empty")
	}

	// Push again (wrap-around test)
	if !r.push(queuedTask{enqueueAt: time.Now(), run: nil}) {
		t.Fatal("wrap-around push failed")
	}
	_, ok = r.pop()
	if !ok {
		t.Fatal("wrap-around pop failed")
	}
}

func TestPanicRecover(t *testing.T) {
	var recovered atomic.Bool
	d := NewOutboundDispatcher(context.Background(), DispatcherConfig{
		MaxInflight: 32,
		QueueSize:   16,
		Recover: func(r any) {
			recovered.Store(true)
		},
	})
	defer d.Shutdown(context.Background())

	_ = d.Submit("chat_panic", func(ctx context.Context) error {
		panic("test panic")
	})

	time.Sleep(100 * time.Millisecond)
	if !recovered.Load() {
		t.Fatal("panic was not recovered")
	}
}

func TestForceClose(t *testing.T) {
	d := NewOutboundDispatcher(context.Background(), DispatcherConfig{
		MaxInflight: 32,
		QueueSize:   16,
	})

	started := make(chan struct{})
	var executed atomic.Bool
	_ = d.Submit("chat_force", func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		executed.Store(true)
		return ctx.Err()
	})
	<-started

	d.ForceClose()
	time.Sleep(50 * time.Millisecond)

	if !executed.Load() {
		t.Fatal("task should have been cancelled")
	}
}

func TestConcurrentSubmitDifferentChats(t *testing.T) {
	d := NewOutboundDispatcher(context.Background(), DispatcherConfig{
		MaxInflight: 32,
		QueueSize:   16,
	})
	defer d.Shutdown(context.Background())

	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		i := i
		go func() {
			defer wg.Done()
			chatID := "chat_" + string(rune('A'+i))
			err := d.Submit(chatID, func(ctx context.Context) error {
				return nil
			})
			if err != nil {
				t.Errorf("concurrent Submit failed: %v", err)
			}
		}()
	}
	wg.Wait()
}

func TestChatQueueLifecycle(t *testing.T) {
	d := NewOutboundDispatcher(context.Background(), DispatcherConfig{
		MaxInflight: 32,
		QueueSize:   16,
	})
	defer d.Shutdown(context.Background())

	// Submit and wait for completion
	_ = d.Submit("chat_life", func(ctx context.Context) error {
		return nil
	})
	time.Sleep(50 * time.Millisecond)

	// Queue should have been deleted
	_, ok := d.queues.Load("chat_life")
	if ok {
		t.Fatal("chatQueue should have been deleted after idle")
	}

	// Resubmit should work (recreate)
	err := d.Submit("chat_life", func(ctx context.Context) error {
		return nil
	})
	if err != nil {
		t.Fatalf("resubmit failed: %v", err)
	}
}
