package messagelog

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"gorm.io/gorm"
)

func TestRecord_QueryGroup(t *testing.T) {
	l := New(10)
	now := time.Now()
	for i := range 5 {
		l.Record(RecordEntry{
			ChatID:    "g1",
			UserID:    "u1",
			Content:   "消息" + string(rune('0'+i)),
			Timestamp: now.Add(time.Duration(i) * time.Second),
		})
	}
	msgs := l.QueryGroup("g1", 3)
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if msgs[0].Content != "消息2" {
		t.Errorf("expected 消息2, got %s", msgs[0].Content)
	}
	if msgs[2].Content != "消息4" {
		t.Errorf("expected 消息4, got %s", msgs[2].Content)
	}
}

func TestRecord_QueryUser(t *testing.T) {
	l := New(10)
	now := time.Now()
	l.Record(RecordEntry{ChatID: "g1", UserID: "u1", Content: "hello world", Timestamp: now})
	l.Record(RecordEntry{ChatID: "g2", UserID: "u1", Content: "foo bar", Timestamp: now.Add(time.Second)})
	l.Record(RecordEntry{ChatID: "g1", UserID: "u2", Content: "baz", Timestamp: now.Add(2 * time.Second)})

	msgs := l.QueryUser("u1", 10)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages for u1, got %d", len(msgs))
	}
}

func TestRing_Overflow(t *testing.T) {
	l := New(5)
	now := time.Now()
	for i := range 8 {
		l.Record(RecordEntry{ChatID: "g1", UserID: "u1", Content: "msg" + string(rune('0'+i)), Timestamp: now.Add(time.Duration(i) * time.Second)})
	}
	msgs := l.QueryGroup("g1", 10)
	if len(msgs) != 5 {
		t.Fatalf("expected 5 messages (ring overflow), got %d", len(msgs))
	}
	if msgs[0].Content != "msg3" {
		t.Errorf("expected msg3, got %s", msgs[0].Content)
	}
	if msgs[4].Content != "msg7" {
		t.Errorf("expected msg7, got %s", msgs[4].Content)
	}
}

func TestWordFreq(t *testing.T) {
	l := New(100)
	now := time.Now()
	l.Record(RecordEntry{ChatID: "g1", Content: "你好 世界 你好", Timestamp: now})
	l.Record(RecordEntry{ChatID: "g1", Content: "世界 再见", Timestamp: now.Add(time.Second)})

	freq := l.WordFreq("g1", 100)
	if freq["你好"] != 2 {
		t.Errorf("expected '你好' freq=2, got %d", freq["你好"])
	}
	if freq["世界"] != 2 {
		t.Errorf("expected '世界' freq=2, got %d", freq["世界"])
	}
	if freq["再见"] != 1 {
		t.Errorf("expected '再见' freq=1, got %d", freq["再见"])
	}
}

func TestClear(t *testing.T) {
	l := New(100)
	now := time.Now()
	l.Record(RecordEntry{ChatID: "g1", UserID: "u1", Content: "old message", Timestamp: now.Add(-2 * time.Hour)})
	l.Record(RecordEntry{ChatID: "g1", UserID: "u1", Content: "new message", Timestamp: now})

	l.Clear(now.Add(-time.Hour))

	msgs := l.QueryGroup("g1", 10)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message after clear, got %d", len(msgs))
	}
	if msgs[0].Content != "new message" {
		t.Errorf("expected 'new message', got %s", msgs[0].Content)
	}
}

func TestClear_RemovesEmptyGroup(t *testing.T) {
	l := New(100)
	now := time.Now()
	l.Record(RecordEntry{ChatID: "g1", Content: "old", Timestamp: now.Add(-2 * time.Hour)})
	l.Clear(now.Add(-time.Hour))
	if l.GroupCount() != 0 {
		t.Errorf("expected group to be removed after all messages cleared")
	}
}

func TestGroupMessageCount(t *testing.T) {
	l := New(100)
	now := time.Now()
	for range 7 {
		l.Record(RecordEntry{ChatID: "g1", Content: "msg", Timestamp: now})
	}
	if l.GroupMessageCount("g1") != 7 {
		t.Errorf("expected 7, got %d", l.GroupMessageCount("g1"))
	}
	if l.GroupMessageCount("nonexistent") != 0 {
		t.Errorf("expected 0 for nonexistent group")
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

func TestQueryByEventID_InboundAndOutboundRings(t *testing.T) {
	l := New(10)
	now := time.Now()
	l.Record(RecordEntry{
		ChatID: "g1", UserID: "u1", Content: "user message", EventID: "in-1", Timestamp: now,
	})
	l.RecordOutbound("g1", "out-1", "bot reply", now)

	// 命中入站消息
	e, ok := l.QueryByEventID("g1", "in-1")
	if !ok {
		t.Fatal("expected inbound message found")
	}
	if e.Content != "user message" || e.IsOutbound {
		t.Errorf("unexpected inbound entry: %+v", e)
	}

	// 命中出站消息
	e, ok = l.QueryByEventID("g1", "out-1")
	if !ok {
		t.Fatal("expected outbound message found")
	}
	if e.Content != "bot reply" || !e.IsOutbound {
		t.Errorf("unexpected outbound entry: %+v", e)
	}

	// 未命中
	if _, ok := l.QueryByEventID("g1", "nonexistent"); ok {
		t.Error("expected miss for nonexistent event ID")
	}

	// 会话不匹配
	if _, ok := l.QueryByEventID("g2", "in-1"); ok {
		t.Error("expected miss for different chat")
	}
}

func TestRecordOutbound_NotInQueryGroup(t *testing.T) {
	l := New(10)
	now := time.Now()
	l.Record(RecordEntry{ChatID: "g1", UserID: "u1", Content: "user", EventID: "in-1", Timestamp: now})
	l.RecordOutbound("g1", "out-1", "bot", now)

	// QueryGroup 只返回入站消息，不含出站
	msgs := l.QueryGroup("g1", 10)
	if len(msgs) != 1 || msgs[0].Content != "user" {
		t.Errorf("QueryGroup should exclude outbound messages, got %d entries", len(msgs))
	}
}

func TestQueryByEventID_DBFallback(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "messagelog_test.db"))
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer closeDB(t, db)
	l := New(10)
	l.UseDB(db)

	now := time.Now().UnixNano()
	// 模拟已持久化但不在内存 ring 中的旧消息（入站 + 出站）
	if err := l.db.Create(&MessageRecord{
		ChatID: "g1", EventID: "old-in", Content: "old user message", Timestamp: now, CreatedAt: now,
	}).Error; err != nil {
		t.Fatalf("create inbound record: %v", err)
	}
	if err := l.db.Create(&MessageRecord{
		ChatID: "g1", EventID: "old-out", Content: "old bot reply", Timestamp: now, CreatedAt: now, IsOutbound: true,
	}).Error; err != nil {
		t.Fatalf("create outbound record: %v", err)
	}

	e, ok := l.QueryByEventID("g1", "old-in")
	if !ok || e.Content != "old user message" || e.IsOutbound {
		t.Errorf("expected DB fallback to find inbound, got %+v ok=%v", e, ok)
	}

	e, ok = l.QueryByEventID("g1", "old-out")
	if !ok || e.Content != "old bot reply" || !e.IsOutbound {
		t.Errorf("expected DB fallback to find outbound, got %+v ok=%v", e, ok)
	}

	if _, ok := l.QueryByEventID("g1", "missing"); ok {
		t.Error("expected miss for nonexistent event ID in DB")
	}
}

func TestQueryGroupFromDB_ExcludesOutbound(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "messagelog_test.db"))
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer closeDB(t, db)
	l := New(10)
	l.UseDB(db)

	now := time.Now()
	ts := now.UnixNano()
	if err := l.db.Create(&MessageRecord{
		ChatID: "g1", EventID: "in-1", Content: "user", Timestamp: ts, CreatedAt: ts, IsOutbound: false,
	}).Error; err != nil {
		t.Fatalf("create inbound: %v", err)
	}
	if err := l.db.Create(&MessageRecord{
		ChatID: "g1", EventID: "out-1", Content: "bot", Timestamp: ts, CreatedAt: ts, IsOutbound: true,
	}).Error; err != nil {
		t.Fatalf("create outbound: %v", err)
	}

	entries, err := l.QueryGroupFromDB("g1", now.Add(-time.Hour), now.Add(time.Hour), 10)
	if err != nil {
		t.Fatalf("QueryGroupFromDB failed: %v", err)
	}
	if len(entries) != 1 || entries[0].Content != "user" {
		t.Errorf("QueryGroupFromDB should exclude outbound, got %d entries", len(entries))
	}
}

func TestQueryGroupRecent_DBFallback(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "messagelog_test.db"))
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer closeDB(t, db)
	l := New(10)
	l.UseDB(db)

	now := time.Now().UnixNano()
	// 模拟重启后场景：内存 ring 为空，旧消息已持久化在 SQLite
	for i := 1; i <= 5; i++ {
		if err := l.db.Create(&MessageRecord{
			ChatID: "g1", EventID: fmt.Sprintf("db-%d", i),
			Content: fmt.Sprintf("db message %d", i),
			Timestamp: now + int64(i), CreatedAt: now + int64(i),
		}).Error; err != nil {
			t.Fatalf("create db record %d: %v", i, err)
		}
	}
	// 出站消息不应被返回
	if err := l.db.Create(&MessageRecord{
		ChatID: "g1", EventID: "db-out", Content: "bot", Timestamp: now + 100, CreatedAt: now + 100, IsOutbound: true,
	}).Error; err != nil {
		t.Fatalf("create outbound record: %v", err)
	}
	// 新消息进入内存 ring
	l.Record(RecordEntry{ChatID: "g1", EventID: "mem-1", Content: "mem message", Timestamp: time.Unix(0, now+200)})

	entries := l.QueryGroupRecent("g1", 3)
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if entries[0].Content != "db message 4" || entries[1].Content != "db message 5" || entries[2].Content != "mem message" {
		t.Errorf("unexpected merged order: %+v", entries)
	}

	// 重复写入同 EventID 到内存 ring：合并时按 EventID 去重，条数不变
	l.Record(RecordEntry{ChatID: "g1", EventID: "mem-1", Content: "dup", Timestamp: time.Unix(0, now+200)})
	entries = l.QueryGroupRecent("g1", 3)
	if len(entries) != 3 {
		t.Errorf("expected dedup to keep 3 entries, got %d", len(entries))
	}
}

func TestQueryGroupRecentWithBot(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "messagelog_test.db"))
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer closeDB(t, db)
	l := New(10)
	l.UseDB(db)

	now := time.Now().UnixNano()
	// 重启后场景：内存 ring 为空，入站 + 出站（机器人回复）均在 SQLite
	if err := l.db.Create(&MessageRecord{
		ChatID: "g1", EventID: "in-1", Content: "user msg", Timestamp: now + 1, CreatedAt: now + 1,
	}).Error; err != nil {
		t.Fatalf("create inbound: %v", err)
	}
	if err := l.db.Create(&MessageRecord{
		ChatID: "g1", EventID: "out-1", Content: "bot reply", Timestamp: now + 2, CreatedAt: now + 2, IsOutbound: true,
	}).Error; err != nil {
		t.Fatalf("create outbound: %v", err)
	}

	// 仅入站：不含机器人回复
	onlyIn := l.QueryGroupRecent("g1", 10)
	if len(onlyIn) != 1 || onlyIn[0].EventID != "in-1" {
		t.Errorf("QueryGroupRecent should exclude outbound, got %+v", onlyIn)
	}

	// 含机器人：两者都返回，且时间旧→新
	withBot := l.QueryGroupRecentWithBot("g1", 10)
	if len(withBot) != 2 {
		t.Fatalf("expected 2 entries, got %d: %+v", len(withBot), withBot)
	}
	if withBot[0].EventID != "in-1" || withBot[1].EventID != "out-1" {
		t.Errorf("expected old-to-new order in-1, out-1, got %+v", withBot)
	}

	// 内存 ring 与出站 ring 混合（含去重与排序）
	l.Record(RecordEntry{ChatID: "g1", EventID: "in-2", Content: "new user", Timestamp: time.Unix(0, now+100)})
	l.RecordOutbound("g1", "out-2", "new bot reply", time.Unix(0, now+101))
	got := l.QueryGroupRecentWithBot("g1", 3)
	if len(got) != 3 {
		t.Fatalf("expected 3 entries, got %d: %+v", len(got), got)
	}
	// 应取最新 3 条且旧→新：in-2, out-1(DB), out-2(mem) 或 in-2, out-2 相关组合，
	// 只要保序且不含被截断的 in-1
	for _, e := range got {
		if e.EventID == "in-1" {
			t.Errorf("in-1 should be truncated, got %+v", got)
		}
	}
}

// closeDB 关闭 gorm 底层连接，释放 SQLite 文件句柄（Windows 上否则会阻塞 TempDir 清理）。
func closeDB(t *testing.T, db *gorm.DB) {
	t.Helper()
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql.DB: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}
}

func TestOnOutbound(t *testing.T) {
	l := New(10)
	req := platform.SendRequest{
		Target:  platform.ChatInfo{ID: "g1", IsGroup: true},
		Message: platform.TextMessage("bot reply"),
	}

	// 正常发送：记录出站消息
	l.OnOutbound("g1", req, platform.SendResult{MessageID: "out-1"}, nil)
	e, ok := l.QueryByEventID("g1", "out-1")
	if !ok || e.Content != "bot reply" || !e.IsOutbound {
		t.Fatalf("expected outbound recorded, got %+v ok=%v", e, ok)
	}

	// 发送失败：不记录
	l.OnOutbound("g1", req, platform.SendResult{}, errors.New("send failed"))
	// 平台未返回 MessageID：不记录
	l.OnOutbound("g1", req, platform.SendResult{}, nil)
	// 无文本内容：不记录
	l.OnOutbound("g1", platform.SendRequest{
		Target:  platform.ChatInfo{ID: "g1"},
		Message: platform.OutboundMessage{},
	}, platform.SendResult{MessageID: "out-2"}, nil)

	if _, ok := l.QueryByEventID("g1", "out-2"); ok {
		t.Error("expected empty-content outbound not recorded")
	}
	// 首次记录仍完整保留（未受后续跳过影响）
	if e, ok := l.QueryByEventID("g1", "out-1"); !ok || e.Content != "bot reply" {
		t.Errorf("expected original outbound intact, got %+v ok=%v", e, ok)
	}
}

func TestMessageLogger_InjectsOutboundObserver(t *testing.T) {
	defaultLogger.Start()
	defer defaultLogger.Stop()

	var gotObserver bool
	mw := MessageLogger()
	handler := mw(func(ctx *eventctx.Context) error {
		if obs, ok := eventctx.ExtGet[eventctx.OutboundObserverExt](ctx.Ext()); ok && obs.Observer != nil {
			gotObserver = true
		}
		return nil
	})

	evt := platform.NewSyntheticEvent(platform.EventKindGroupMessage, "hi")
	ctx := eventctx.NewContextFromEvent(evt, &platform.NoopSender{})
	if err := handler(ctx); err != nil {
		t.Fatalf("handler failed: %v", err)
	}
	if !gotObserver {
		t.Error("expected outbound observer injected by MessageLogger middleware")
	}
}
