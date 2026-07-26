package job_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/job"
)

func TestPlugin_Once_Success(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
	p := job.NewPlugin()
	var called atomic.Bool

	id := p.Once("test-once", func(_ context.Context) error {
		called.Store(true)
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := p.Wait(ctx, id); err != nil {
		t.Fatalf("Wait: unexpected error: %v", err)
	}
	if !called.Load() {
		t.Error("job function was not called")
	}
	info, ok := p.Info(id)
	if !ok {
		t.Fatal("Info: job not found")
	}
	if info.Status != job.StatusDone {
		t.Errorf("expected StatusDone, got %s", info.Status)
	}
	})
}


func TestPlugin_Once_Delay(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
	p := job.NewPlugin()
	start := time.Now()
	var executedAt time.Time

	id := p.Once("delayed", func(_ context.Context) error {
		executedAt = time.Now()
		return nil
	}, job.WithDelay(100*time.Millisecond))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = p.Wait(ctx, id)

	if executedAt.Sub(start) < 80*time.Millisecond {
		t.Errorf("job executed too early: elapsed %v", executedAt.Sub(start))
	}
	})
}


func TestPlugin_Retry_Success(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
	p := job.NewPlugin()
	var attempts atomic.Int32

	id := p.Retry("retry-success", func(_ context.Context) error {
		n := attempts.Add(1)
		if n < 3 {
			return errors.New("not yet")
		}
		return nil
	}, job.WithMaxRetries(3), job.WithFixedBackoff(10*time.Millisecond))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := p.Wait(ctx, id); err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if attempts.Load() != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts.Load())
	}
	})
}


func TestPlugin_Retry_AllFail(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
	p := job.NewPlugin()
	sentinel := errors.New("always fail")

	id := p.Retry("retry-fail", func(_ context.Context) error {
		return sentinel
	}, job.WithMaxRetries(2), job.WithFixedBackoff(10*time.Millisecond))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := p.Wait(ctx, id)
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error, got: %v", err)
	}
	info, _ := p.Info(id)
	if info.Status != job.StatusFailed {
		t.Errorf("expected StatusFailed, got %s", info.Status)
	}
	if info.Attempts != 3 { // 1 initial + 2 retries
		t.Errorf("expected 3 attempts, got %d", info.Attempts)
	}
	})
}


func TestPlugin_Chain_Success(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
	p := job.NewPlugin()
	var order []int

	id := p.Chain("chain",
		func(_ context.Context) error { order = append(order, 1); return nil },
		func(_ context.Context) error { order = append(order, 2); return nil },
		func(_ context.Context) error { order = append(order, 3); return nil },
	)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := p.Wait(ctx, id); err != nil {
		t.Fatalf("chain failed: %v", err)
	}
	if len(order) != 3 || order[0] != 1 || order[1] != 2 || order[2] != 3 {
		t.Errorf("unexpected execution order: %v", order)
	}
	})
}


func TestPlugin_Chain_StopsOnError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
	p := job.NewPlugin()
	var step2Called atomic.Bool
	sentinel := errors.New("step1 fail")

	id := p.Chain("chain-fail",
		func(_ context.Context) error { return sentinel },
		func(_ context.Context) error { step2Called.Store(true); return nil },
	)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := p.Wait(ctx, id)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if step2Called.Load() {
		t.Error("step 2 should not have been called after step 1 failed")
	}
	})
}


func TestPlugin_OnDone_Callback(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
	p := job.NewPlugin()
	doneCh := make(chan job.Info, 1)

	p.Once("with-callback", func(_ context.Context) error { return nil },
		job.WithOnDone(func(info job.Info) { doneCh <- info }),
	)

	select {
	case info := <-doneCh:
		if info.Status != job.StatusDone {
			t.Errorf("expected StatusDone in callback, got %s", info.Status)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for OnDone callback")
	}
	})
}


func TestExponentialBackoff(t *testing.T) {
	fn := job.ExponentialBackoff(100*time.Millisecond, 2*time.Second)
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{1, 100 * time.Millisecond},
		{2, 200 * time.Millisecond},
		{3, 400 * time.Millisecond},
		{10, 2 * time.Second}, // capped
	}
	for _, c := range cases {
		got := fn(c.attempt)
		if got != c.want {
			t.Errorf("attempt %d: want %v, got %v", c.attempt, c.want, got)
		}
	}
}
