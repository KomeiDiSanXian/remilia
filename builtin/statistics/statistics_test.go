package statistics

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/messagelog"
)

// fakeSource 可注入的 ScanAfter 实现（替代 messagelog.Default()）。
type fakeSource struct {
	mu      sync.Mutex
	entries []messagelog.RecordEntry
}

func (f *fakeSource) add(e messagelog.RecordEntry) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries = append(f.entries, e)
}

func (f *fakeSource) ScanAfter(_ time.Time, _ int64, fn func(messagelog.RecordEntry) error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, e := range f.entries {
		if err := fn(e); err != nil {
			return err
		}
	}
	return nil
}

// blockingSource 扫描开始时阻塞，测试据此在重建窗口内投递事件。
type blockingSource struct {
	entries []messagelog.RecordEntry
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *blockingSource) ScanAfter(_ time.Time, _ int64, fn func(messagelog.RecordEntry) error) error {
	b.once.Do(func() { close(b.entered) })
	<-b.release
	for _, e := range b.entries {
		if err := fn(e); err != nil {
			return err
		}
	}
	return nil
}

// newTestPlugin 启动等价于 Setup 后运行态的插件聚合器。
func newTestPlugin(t *testing.T, src ScanSource) (*Plugin, func()) {
	t.Helper()
	p := NewPlugin(WithStore(filepath.Join(t.TempDir(), "stats.db")), WithFlushInterval(20*time.Millisecond))
	db, err := OpenDB(p.path)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	p.db = db
	p.source = src
	p.ch = make(chan aggCmd, 100000)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		p.rebuild(ctx)
		p.aggregatorLoop(ctx)
	}()
	cleanup := func() {
		cancel()
		<-done
		sqlDB, _ := db.DB()
		sqlDB.Close()
	}
	return p, cleanup
}

func requireEventually(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(msg)
}

func entry(id, chat, user, content string, ts time.Time, outbound bool) messagelog.RecordEntry {
	return messagelog.RecordEntry{
		EventID:    id,
		ChatID:     chat,
		UserID:     user,
		Content:    content,
		Timestamp:  ts,
		IsOutbound: outbound,
	}
}

func TestTokenize(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"hello world", []string{"hello", "world"}},
		{"你好 世界", []string{"你好", "世界"}},
		{"a bc", []string{"bc"}},
		{"!!! ???", []string{}},
	}
	for _, tt := range tests {
		got := tokenize(tt.input)
		if len(got) != len(tt.expected) {
			t.Errorf("tokenize(%q): expected %v, got %v", tt.input, tt.expected, got)
			continue
		}
		for i, w := range tt.expected {
			if got[i] != w {
				t.Errorf("tokenize(%q)[%d]: expected %q, got %q", tt.input, i, w, got[i])
			}
		}
	}
}

// TestIncremental_EventSubscription 事件订阅增量聚合：词频 / 每日 / 用户 / 会话。
func TestIncremental_EventSubscription(t *testing.T) {
	src := &fakeSource{}
	p, cleanup := newTestPlugin(t, src)
	defer cleanup()

	base := time.Date(2026, 8, 30, 1, 0, 0, 0, time.UTC)
	p.handleRecorded(messagelog.MessageRecorded{Record: entry("e1", "g1", "u1", "你好 世界", base, false)})
	p.handleRecorded(messagelog.MessageRecorded{Record: entry("e2", "g1", "u2", "世界 再见", base.Add(time.Hour), false)})
	p.handleRecorded(messagelog.MessageRecorded{Record: entry("e3", "g1", "u1", "out", base.Add(2*time.Hour), true)})

	requireEventually(t, func() bool {
		words, err := p.WordFreq("g1", 10)
		return err == nil && len(words) == 4
	}, "word stats not flushed")

	words, err := p.WordFreq("g1", 10)
	if err != nil {
		t.Fatalf("WordFreq: %v", err)
	}
	countOf := func(w string) int {
		for _, e := range words {
			if e.Word == w {
				return e.Count
			}
		}
		return 0
	}
	if countOf("世界") != 2 || countOf("你好") != 1 || countOf("再见") != 1 || countOf("out") != 1 {
		t.Errorf("unexpected word counts: %+v", words)
	}

	daily, err := p.DailyStats("g1", time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC), time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("DailyStats: %v", err)
	}
	if len(daily) != 2 {
		t.Fatalf("expected 2 daily rows (inbound/outbound), got %+v", daily)
	}
	for _, d := range daily {
		if d.Day != "2026-08-30" {
			t.Errorf("unexpected day %q", d.Day)
		}
		if d.IsOutbound && d.Count != 1 {
			t.Errorf("expected outbound count 1, got %d", d.Count)
		}
		if !d.IsOutbound && d.Count != 2 {
			t.Errorf("expected inbound count 2, got %d", d.Count)
		}
	}

	users, err := p.UserStats("g1", time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC), time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC), 10)
	if err != nil {
		t.Fatalf("UserStats: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("expected 2 users, got %+v", users)
	}
	for _, u := range users {
		if u.UserID == "u1" && u.Count != 2 {
			t.Errorf("expected u1 count 2, got %d", u.Count)
		}
		if u.UserID == "u2" && u.Count != 1 {
			t.Errorf("expected u2 count 1, got %d", u.Count)
		}
	}

	chats, err := p.ChatStats(time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC), time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC), 10)
	if err != nil {
		t.Fatalf("ChatStats: %v", err)
	}
	if len(chats) != 1 || chats[0].ChatID != "g1" || chats[0].Count != 3 {
		t.Fatalf("unexpected chat stats: %+v", chats)
	}
}

// TestRebuild_FullScan 全量重建：扫描事实源并聚合。
func TestRebuild_FullScan(t *testing.T) {
	src := &fakeSource{}
	base := time.Date(2026, 8, 30, 1, 0, 0, 0, time.UTC)
	src.add(entry("e1", "g1", "u1", "你好 世界", base, false))
	src.add(entry("e2", "g1", "u2", "世界 再见", base.Add(time.Hour), false))

	p, cleanup := newTestPlugin(t, src)
	defer cleanup()

	requireEventually(t, func() bool {
		words, err := p.WordFreq("g1", 10)
		return err == nil && len(words) == 3
	}, "rebuild not flushed")

	words, _ := p.WordFreq("g1", 10)
	for _, w := range words {
		if w.Word == "世界" && w.Count != 2 {
			t.Errorf("expected 世界=2 after rebuild, got %d", w.Count)
		}
	}
}

// TestRebuild_DedupeQueuedEvents 重建期间到达的重复事件不重复计数。
func TestRebuild_DedupeQueuedEvents(t *testing.T) {
	base := time.Date(2026, 8, 30, 1, 0, 0, 0, time.UTC)
	e1 := entry("e1", "g1", "u1", "你好", base, false)
	src := &blockingSource{
		entries: []messagelog.RecordEntry{e1},
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}

	p := NewPlugin(WithStore(filepath.Join(t.TempDir(), "stats.db")), WithFlushInterval(20*time.Millisecond))
	db, err := OpenDB(p.path)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	p.db = db
	p.source = src
	p.ch = make(chan aggCmd, 100000)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		p.rebuild(ctx)
		p.aggregatorLoop(ctx)
	}()
	defer func() {
		cancel()
		<-done
		sqlDB, _ := db.DB()
		sqlDB.Close()
	}()

	// 等扫描开始，在重建窗口内投递与事实源重复的事件
	<-src.entered
	p.enqueue(&e1)
	close(src.release)

	requireEventually(t, func() bool {
		words, err := p.WordFreq("g1", 10)
		return err == nil && len(words) == 1
	}, "dedupe rebuild not flushed")

	words, _ := p.WordFreq("g1", 10)
	if len(words) != 1 || words[0].Word != "你好" || words[0].Count != 1 {
		t.Fatalf("expected 你好 counted once, got %+v", words)
	}
}

// TestRebuild_QueuedNewEventSurvives 重建期间到达的新事件不丢失。
func TestRebuild_QueuedNewEventSurvives(t *testing.T) {
	base := time.Date(2026, 8, 30, 1, 0, 0, 0, time.UTC)
	src := &blockingSource{
		entries: []messagelog.RecordEntry{entry("e1", "g1", "u1", "你好", base, false)},
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}

	p := NewPlugin(WithStore(filepath.Join(t.TempDir(), "stats.db")), WithFlushInterval(20*time.Millisecond))
	db, err := OpenDB(p.path)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	p.db = db
	p.source = src
	p.ch = make(chan aggCmd, 100000)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		p.rebuild(ctx)
		p.aggregatorLoop(ctx)
	}()
	defer func() {
		cancel()
		<-done
		sqlDB, _ := db.DB()
		sqlDB.Close()
	}()

	<-src.entered
	e2 := entry("e2", "g1", "u1", "你好", base.Add(time.Hour), false)
	p.enqueue(&e2)
	close(src.release)

	requireEventually(t, func() bool {
		words, err := p.WordFreq("g1", 10)
		return err == nil && len(words) == 1
	}, "queued new event not flushed")

	words, _ := p.WordFreq("g1", 10)
	if len(words) != 1 || words[0].Count != 2 {
		t.Fatalf("expected 你好 counted twice (scan + queued), got %+v", words)
	}
}

// TestDroppedEvents 队列满丢弃计数。
func TestDroppedEvents(t *testing.T) {
	p := NewPlugin(WithStore(filepath.Join(t.TempDir(), "stats.db")))
	p.ch = make(chan aggCmd, 1)
	p.enqueue(&messagelog.RecordEntry{EventID: "a"})
	p.enqueue(&messagelog.RecordEntry{EventID: "b"})
	p.enqueue(&messagelog.RecordEntry{EventID: "c"})
	if got := p.DroppedEvents(); got != 2 {
		t.Fatalf("expected 2 dropped events, got %d", got)
	}
}
