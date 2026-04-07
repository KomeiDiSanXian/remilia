package subscription

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// ─── 测试用数据源 ─────────────────────────────────────────────────────────────

type mockSource struct {
	name  string
	items []Item
	err   error
}

func (s *mockSource) Name() string        { return s.name }
func (s *mockSource) Description() string { return "mock source for testing" }
func (s *mockSource) Poll(_ context.Context, _ string) ([]Item, error) {
	return s.items, s.err
}
func (s *mockSource) ValidateParam(param string) error {
	if param == "invalid" {
		return errors.New("invalid param")
	}
	return nil
}

// ─── 测试 Manager（不依赖 scheduler） ────────────────────────────────────────

func newTestManager() *Manager {
	return newManager(managerOpts{pollInterval: 5 * time.Minute})
}

func TestRegisterSource(t *testing.T) {
	m := newTestManager()
	src := &mockSource{name: "test"}
	m.RegisterSource(src)

	sources := m.ListSources()
	if len(sources) != 1 || sources[0].Name != "test" {
		t.Fatalf("expected 1 source named 'test', got %+v", sources)
	}
}

func TestSubscribe_Basic(t *testing.T) {
	m := newTestManager()
	m.RegisterSource(&mockSource{name: "rss"})

	target := Target{ChatID: "group-001", IsGroup: true}
	id, err := m.Subscribe("rss", "https://example.com/feed", target)
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty subscription ID")
	}

	subs := m.ListSubscriptions("group-001")
	if len(subs) != 1 {
		t.Fatalf("expected 1 subscription, got %d", len(subs))
	}
	if subs[0].SourceName != "rss" {
		t.Errorf("expected source 'rss', got %q", subs[0].SourceName)
	}
}

func TestSubscribe_SourceNotFound(t *testing.T) {
	m := newTestManager()
	_, err := m.Subscribe("nonexistent", "param", Target{ChatID: "g1", IsGroup: true})
	if !errors.Is(err, ErrSourceNotFound) {
		t.Fatalf("expected ErrSourceNotFound, got %v", err)
	}
}

func TestSubscribe_InvalidParam(t *testing.T) {
	m := newTestManager()
	m.RegisterSource(&mockSource{name: "rss"})
	_, err := m.Subscribe("rss", "invalid", Target{ChatID: "g1", IsGroup: true})
	if err == nil {
		t.Fatal("expected error for invalid param")
	}
}

func TestSubscribe_Duplicate(t *testing.T) {
	m := newTestManager()
	m.RegisterSource(&mockSource{name: "rss"})
	target := Target{ChatID: "g1", IsGroup: true}

	_, err := m.Subscribe("rss", "url", target)
	if err != nil {
		t.Fatalf("first Subscribe failed: %v", err)
	}
	_, err = m.Subscribe("rss", "url", target)
	if !errors.Is(err, ErrAlreadySubscribed) {
		t.Fatalf("expected ErrAlreadySubscribed, got %v", err)
	}
}

func TestUnsubscribe(t *testing.T) {
	m := newTestManager()
	m.RegisterSource(&mockSource{name: "rss"})

	id, _ := m.Subscribe("rss", "url", Target{ChatID: "g1", IsGroup: true})
	if err := m.Unsubscribe(id); err != nil {
		t.Fatalf("Unsubscribe failed: %v", err)
	}
	if subs := m.ListSubscriptions("g1"); len(subs) != 0 {
		t.Fatalf("expected 0 subscriptions after unsubscribe, got %d", len(subs))
	}
}

func TestUnsubscribe_NotFound(t *testing.T) {
	m := newTestManager()
	if err := m.Unsubscribe("nonexistent-id"); !errors.Is(err, ErrSubscriptionNotFound) {
		t.Fatalf("expected ErrSubscriptionNotFound, got %v", err)
	}
}

func TestListSubscriptions_AllTargets(t *testing.T) {
	m := newTestManager()
	m.RegisterSource(&mockSource{name: "rss"})

	m.Subscribe("rss", "url1", Target{ChatID: "g1", IsGroup: true})
	m.Subscribe("rss", "url2", Target{ChatID: "g2", IsGroup: true})

	all := m.ListSubscriptions("")
	if len(all) != 2 {
		t.Fatalf("expected 2 total subscriptions, got %d", len(all))
	}
}

func TestPoll_DispatchesNewItems(t *testing.T) {
	m := newTestManager()
	src := &mockSource{
		name:  "rss",
		items: []Item{{ID: "item-1", Title: "Title1"}, {ID: "item-2", Title: "Title2"}},
	}
	m.RegisterSource(src)

	var mu sync.Mutex
	dispatched := make([]Item, 0)
	m.dispatch = func(_ context.Context, _ Target, item Item) error {
		mu.Lock()
		dispatched = append(dispatched, item)
		mu.Unlock()
		return nil
	}

	target := Target{ChatID: "g1", IsGroup: true}
	m.Subscribe("rss", "url", target)

	// 手动触发一次 poll（绕过 scheduler）
	m.poll(src, "url", buildSourceKey("rss", "url"))

	mu.Lock()
	n := len(dispatched)
	mu.Unlock()
	if n != 2 {
		t.Fatalf("expected 2 dispatched items, got %d", n)
	}
}

func TestPoll_DeduplicatesItems(t *testing.T) {
	m := newTestManager()
	src := &mockSource{
		name:  "rss",
		items: []Item{{ID: "item-1", Title: "Title1"}},
	}
	m.RegisterSource(src)

	var mu sync.Mutex
	var dispatchCount int
	m.dispatch = func(_ context.Context, _ Target, _ Item) error {
		mu.Lock()
		dispatchCount++
		mu.Unlock()
		return nil
	}

	m.Subscribe("rss", "url", Target{ChatID: "g1", IsGroup: true})
	sk := buildSourceKey("rss", "url")

	// 第一次 poll → 应分发 1 条
	m.poll(src, "url", sk)
	// 第二次 poll（相同 items）→ 不应重复分发
	m.poll(src, "url", sk)

	mu.Lock()
	count := dispatchCount
	mu.Unlock()
	if count != 1 {
		t.Fatalf("expected 1 dispatch (dedup), got %d", count)
	}
}
