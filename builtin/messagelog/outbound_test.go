package messagelog

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// fakeInnerSender 记录被包装的调用，返回可配置结果。
type fakeInnerSender struct {
	called bool
	res    platform.SendResult
	err    error
}

func (f *fakeInnerSender) Send(_ context.Context, _ platform.SendRequest) (platform.SendResult, error) {
	f.called = true
	return f.res, f.err
}

func TestRecordingSender_PendingToSent(t *testing.T) {
	l := New(10)
	inner := &fakeInnerSender{res: platform.SendResult{MessageID: "m-1", Platform: "qq"}}
	rs := NewRecordingSender(inner, l, "qq")

	res, err := rs.Send(context.Background(), platform.SendRequest{
		Target:  platform.ChatInfo{ID: "g1", IsGroup: true},
		Message: platform.TextMessage("hello"),
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if !inner.called {
		t.Fatal("inner sender not called")
	}
	got := l.QueryChat("g1", 10, QueryOptions{Direction: DirectionOutbound})
	if len(got) != 1 {
		t.Fatalf("expected 1 outbound record, got %d", len(got))
	}
	e := got[0]
	if e.SendStatus != SendStatusSent {
		t.Errorf("expected sent, got %q", e.SendStatus)
	}
	if e.PlatformMessageID != "m-1" || e.Content != "hello" || e.Platform != "qq" {
		t.Errorf("unexpected outbound record: %+v", e)
	}
	if res.MessageID != "m-1" {
		t.Errorf("result not propagated: %+v", res)
	}
}

func TestRecordingSender_PendingToFailed(t *testing.T) {
	l := New(10)
	inner := &fakeInnerSender{err: errors.New("network down")}
	rs := NewRecordingSender(inner, l, "qq")

	_, err := rs.Send(context.Background(), platform.SendRequest{
		Target:  platform.ChatInfo{ID: "g1"},
		Message: platform.TextMessage("hi"),
	})
	if err == nil {
		t.Fatal("expected send error")
	}
	got := l.QueryChat("g1", 10, QueryOptions{Direction: DirectionOutbound})
	if len(got) != 1 {
		t.Fatalf("expected 1 outbound record, got %d", len(got))
	}
	if got[0].SendStatus != SendStatusFailed {
		t.Errorf("expected failed, got %q", got[0].SendStatus)
	}
	if got[0].LastError == "" {
		t.Error("expected last_error populated")
	}
}

// TestOutboundStatus_PersistedAsCompleted pending → sent 经 flush 后 DB 为最终状态
// （UPSERT on event_id，而非遗留 pending 行）。
func TestOutboundStatus_PersistedAsCompleted(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "outbound_status.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer closeDB(t, db)
	l := New(10)
	l.UseDB(db)
	l.Start()
	defer l.Stop()

	inner := &fakeInnerSender{res: platform.SendResult{MessageID: "m-9"}}
	rs := NewRecordingSender(inner, l, "test")
	if _, err := rs.Send(context.Background(), platform.SendRequest{
		Target:  platform.ChatInfo{ID: "g1"},
		Message: platform.TextMessage("final"),
	}); err != nil {
		t.Fatalf("send: %v", err)
	}
	l.Stop() // 触发 flush

	var m MessageRecord
	if err := db.Where("chat_id = ? AND is_outbound = ?", "g1", true).First(&m).Error; err != nil {
		t.Fatalf("query persisted record: %v", err)
	}
	if m.SendStatus != string(SendStatusSent) || m.PlatformMessageID != "m-9" {
		t.Errorf("expected sent persisted, got status=%q platform=%q", m.SendStatus, m.PlatformMessageID)
	}
}

// TestStart_ReconcilesStalePending 重启 reconcile：遗留 pending 标记为 unknown。
func TestStart_ReconcilesStalePending(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "reconcile.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer closeDB(t, db)
	now := time.Now().UnixNano()
	if err := db.Create(&MessageRecord{
		EventID: "pending-1", ChatID: "g1", Content: "in flight", Timestamp: now, CreatedAt: now,
		IsOutbound: true, SendStatus: string(SendStatusPending),
	}).Error; err != nil {
		t.Fatalf("create pending: %v", err)
	}

	l := New(10)
	l.UseDB(db)
	l.Start()
	l.Stop()

	var m MessageRecord
	if err := db.Where("event_id = ?", "pending-1").First(&m).Error; err != nil {
		t.Fatalf("query: %v", err)
	}
	if m.SendStatus != string(SendStatusUnknown) {
		t.Errorf("expected unknown after reconcile, got %q", m.SendStatus)
	}
}

// TestOnOutbound_SkipsFailedWhenConfigured record.failed_outbound=false 时跳过失败。
func TestOnOutbound_SkipsFailedWhenConfigured(t *testing.T) {
	l := New(10)
	l.recordFailedOutbound = false
	l.OnOutbound("g1", platform.SendRequest{
		Target:  platform.ChatInfo{ID: "g1"},
		Message: platform.TextMessage("hi"),
	}, platform.SendResult{}, errors.New("boom"))

	if got := l.QueryChat("g1", 10, QueryOptions{Direction: DirectionOutbound}); len(got) != 0 {
		t.Errorf("expected failed outbound skipped, got %d", len(got))
	}
}
