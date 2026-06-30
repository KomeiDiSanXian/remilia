package future

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewFutureIsNotDone(t *testing.T) {
	f := New[int]()
	if f.IsDone() {
		t.Fatal("new future should not be done")
	}
}

func TestResolveCompletesFuture(t *testing.T) {
	f := New[string]()
	ok := f.Resolve("hello", nil)
	if !ok {
		t.Fatal("first Resolve should return true")
	}
	if !f.IsDone() {
		t.Fatal("future should be done after Resolve")
	}
}

func TestWaitReturnsValue(t *testing.T) {
	f := New[int]()
	f.Resolve(42, nil)
	val, err := f.Wait(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != 42 {
		t.Fatalf("expected 42, got %d", val)
	}
}

func TestWaitReturnsError(t *testing.T) {
	want := errors.New("oops")
	f := New[int]()
	f.Resolve(0, want)
	_, err := f.Wait(context.Background())
	if !errors.Is(err, want) {
		t.Fatalf("expected %v, got %v", want, err)
	}
}

func TestMultipleResolveOnlyFirstWorks(t *testing.T) {
	f := New[int]()
	ok1 := f.Resolve(1, nil)
	ok2 := f.Resolve(2, nil)
	if !ok1 {
		t.Fatal("first Resolve should return true")
	}
	if ok2 {
		t.Fatal("second Resolve should return false")
	}
	val, _ := f.Wait(context.Background())
	if val != 1 {
		t.Fatalf("expected 1 (first value), got %d", val)
	}
}

func TestWaitBlocksUntilResolve(t *testing.T) {
	f := New[int]()
	done := make(chan struct{})
	go func() {
		val, err := f.Wait(context.Background())
		if err != nil || val != 99 {
			t.Errorf("expected 99/nil, got %d/%v", val, err)
		}
		close(done)
	}()
	time.Sleep(10 * time.Millisecond)
	if f.IsDone() {
		t.Fatal("future should not be done yet")
	}
	f.Resolve(99, nil)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Wait did not unblock after Resolve")
	}
}

func TestWaitRespectsContextCancel(t *testing.T) {
	f := New[int]()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := f.Wait(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestWaitRespectsContextDeadline(t *testing.T) {
	f := New[int]()
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	_, err := f.Wait(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}
}

func TestResultNonBlocking(t *testing.T) {
	f := New[int]()
	_, err := f.Result()
	if !errors.Is(err, ErrNotReady) {
		t.Fatalf("expected ErrNotReady, got %v", err)
	}
	f.Resolve(1, nil)
	val, err := f.Result()
	if err != nil || val != 1 {
		t.Fatalf("expected 1/nil, got %d/%v", val, err)
	}
}

func TestDoneChannelClosedAfterResolve(t *testing.T) {
	f := New[int]()
	select {
	case <-f.Done():
		t.Fatal("Done channel should not be closed before Resolve")
	default:
	}
	f.Resolve(1, nil)
	select {
	case <-f.Done():
	default:
		t.Fatal("Done channel should be closed after Resolve")
	}
}

func TestMustWaitReturnsValue(t *testing.T) {
	f := New[string]()
	f.Resolve("ok", nil)
	val := f.MustWait(context.Background())
	if val != "ok" {
		t.Fatalf("expected ok, got %s", val)
	}
}

func TestMustWaitPanicsOnError(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic")
		}
	}()
	f := New[int]()
	f.Resolve(0, errors.New("fail"))
	f.MustWait(context.Background())
}

func TestConcurrentResolveAndWait(t *testing.T) {
	f := New[int]()
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			val, err := f.Wait(context.Background())
			if err != nil || val != 100 {
				t.Errorf("expected 100/nil, got %d/%v", val, err)
			}
		})
	}
	time.Sleep(10 * time.Millisecond)
	f.Resolve(100, nil)
	wg.Wait()
}

func TestConcurrentResolveRace(t *testing.T) {
	f := New[int]()
	var count atomic.Int32
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			if f.Resolve(1, nil) {
				count.Add(1)
			}
		})
	}
	wg.Wait()
	if count.Load() != 1 {
		t.Fatalf("expected exactly 1 successful Resolve, got %d", count.Load())
	}
}

func BenchmarkFutureAlloc(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		f := New[int]()
		_ = f
	}
}

func BenchmarkFutureResolve(b *testing.B) {
	f := New[int]()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		f.Resolve(i, nil)
	}
}
