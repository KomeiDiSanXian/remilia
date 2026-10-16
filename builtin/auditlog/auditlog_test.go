package auditlog_test

import (
	"encoding/json"
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/auditlog"
)

func TestAuditLog_RecordAndQuery(t *testing.T) {
	p := auditlog.NewPlugin(auditlog.Config{MaxMemoryEntries: 100})
	p.RecordRaw("user1", "command", map[string]any{"cmd": "/help"})
	p.RecordRaw("user1", "command", map[string]any{"cmd": "/start"})
	p.RecordRaw("user2", "perm.grant", nil)

	if p.Count() != 3 {
		t.Fatalf("expected 3 entries, got %d", p.Count())
	}

	recent := p.Recent(2)
	if len(recent) != 2 {
		t.Fatalf("expected 2 recent entries, got %d", len(recent))
	}

	byUser := p.ByUser("user1", 10)
	if len(byUser) != 2 {
		t.Fatalf("expected 2 entries for user1, got %d", len(byUser))
	}

	byAction := p.ByAction("perm.grant", 10)
	if len(byAction) != 1 {
		t.Fatalf("expected 1 perm.grant entry, got %d", len(byAction))
	}
}

func TestAuditLog_CircularBuffer(t *testing.T) {
	p := auditlog.NewPlugin(auditlog.Config{MaxMemoryEntries: 3})

	for range 5 {
		p.RecordRaw("user", "cmd", nil)
	}

	// 环形缓冲只保留最近 3 条
	if p.Count() != 3 {
		t.Fatalf("expected 3 entries (circular buffer), got %d", p.Count())
	}
}

func TestAuditLog_Descriptor(t *testing.T) {
	desc := auditlog.New()
	if desc == nil || desc.Name != "auditlog" {
		t.Fatal("invalid descriptor")
	}
}

// TestAuditLog_LogEntryJSONContract 锁定管理 API / Dashboard 依赖的 snake_case 字段名。
func TestAuditLog_LogEntryJSONContract(t *testing.T) {
	p := auditlog.NewPlugin(auditlog.Config{MaxMemoryEntries: 10})
	p.RecordRaw("user1", "command", map[string]any{"cmd": "/help"})

	entries := p.Recent(1)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	data, err := json.Marshal(entries[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"id", "timestamp", "user_id", "action"} {
		if _, ok := m[key]; !ok {
			t.Errorf("expected key %q, got keys %v", key, m)
		}
	}
	if _, ok := m["UserID"]; ok {
		t.Errorf("unexpected Go-style key UserID, got keys %v", m)
	}
}
