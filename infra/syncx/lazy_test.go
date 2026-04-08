package syncx

import (
	"sync"
	"testing"
)

func TestLazy_Get(t *testing.T) {
	calls := 0
	l := NewLazy(func() int {
		calls++
		return 42
	})

	for i := 0; i < 5; i++ {
		v := l.Get()
		if v != 42 {
			t.Fatalf("expected 42, got %d", v)
		}
	}
	if calls != 1 {
		t.Fatalf("init should be called exactly once, got %d", calls)
	}
}

func TestLazy_Concurrent(t *testing.T) {
	calls := 0
	var mu sync.Mutex

	l := NewLazy(func() int {
		mu.Lock()
		calls++
		mu.Unlock()
		return 99
	})

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	results := make([]int, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			results[idx] = l.Get()
		}(i)
	}
	wg.Wait()

	for i, v := range results {
		if v != 99 {
			t.Fatalf("goroutine %d got %d, want 99", i, v)
		}
	}
	mu.Lock()
	c := calls
	mu.Unlock()
	if c != 1 {
		t.Fatalf("init should be called exactly once, got %d", c)
	}
}

func TestLazy_Pointer(t *testing.T) {
	type Config struct{ Value int }

	l := NewLazy(func() *Config { return &Config{Value: 7} })
	cfg := l.Get()
	if cfg == nil || cfg.Value != 7 {
		t.Fatal("unexpected nil or wrong value from Lazy[*Config]")
	}
	// 再次获取应返回同一指针
	if l.Get() != cfg {
		t.Fatal("second Get should return same pointer")
	}
}
