package ai

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNewSessionManager(t *testing.T) {
	sm := NewSessionManager(100, 20, time.Hour, nil)
	if sm == nil {
		t.Fatal("NewSessionManager returned nil")
	}
}

func TestSessionManagerDefaults(t *testing.T) {
	sm := NewSessionManager(0, 0, 0, nil)
	if sm.maxSize != 1000 {
		t.Errorf("expected default maxSize 1000, got %d", sm.maxSize)
	}
	if sm.maxHistory != 20 {
		t.Errorf("expected default maxHistory 20, got %d", sm.maxHistory)
	}
}

func TestSessionGetOrCreateNew(t *testing.T) {
	sm := NewSessionManager(100, 20, time.Hour, nil)
	s := sm.GetOrCreate("session1", "user1", "chat1")
	if s == nil {
		t.Fatal("GetOrCreate returned nil")
	}
	if s.ID != "session1" {
		t.Errorf("expected ID %q, got %q", "session1", s.ID)
	}
	if s.UserID != "user1" {
		t.Errorf("expected UserID %q, got %q", "user1", s.UserID)
	}
	if s.ChatID != "chat1" {
		t.Errorf("expected ChatID %q, got %q", "chat1", s.ChatID)
	}
}

func TestSessionGetOrCreateExisting(t *testing.T) {
	sm := NewSessionManager(100, 20, time.Hour, nil)
	s1 := sm.GetOrCreate("session1", "user1", "chat1")
	s2 := sm.GetOrCreate("session1", "user1", "chat1")
	if s1 != s2 {
		t.Error("GetOrCreate should return the same session for same ID")
	}
}

func TestSessionAppendMessage(t *testing.T) {
	sm := NewSessionManager(100, 20, time.Hour, nil)
	s := sm.GetOrCreate("session1", "user1", "chat1")
	msg := Message{Role: RoleUser, Content: "hello"}
	sm.AppendMessage(s, msg)

	s.Lock()
	if len(s.Messages) != 1 {
		t.Errorf("expected 1 message, got %d", len(s.Messages))
	}
	if s.Messages[0].Content != "hello" {
		t.Errorf("expected content %q, got %q", "hello", s.Messages[0].Content)
	}
	s.Unlock()
}

func TestSessionDelete(t *testing.T) {
	sm := NewSessionManager(100, 20, time.Hour, nil)
	sm.GetOrCreate("to_delete", "user1", "chat1")
	sm.Delete("to_delete")

	s := sm.GetOrCreate("to_delete", "user1", "chat1")
	if s.Messages != nil && len(s.Messages) != 0 {
		t.Error("deleted and re-created session should be fresh")
	}
}

func TestSessionLRUEviction(t *testing.T) {
	sm := NewSessionManager(2, 20, time.Hour, nil)
	s1 := sm.GetOrCreate("s1", "u1", "c1")
	s2 := sm.GetOrCreate("s2", "u2", "c2")
	s3 := sm.GetOrCreate("s3", "u3", "c3")

	sm.mu.RLock()
	_, ok1 := sm.sessions["s1"]
	sm.mu.RUnlock()

	if ok1 {
		t.Error("s1 should have been evicted (LRU, 2 max)")
	}
	_ = s1
	_ = s2
	_ = s3
}

func TestSessionCleanupExpired(t *testing.T) {
	sm := NewSessionManager(100, 20, 50*time.Millisecond, nil)
	sm.GetOrCreate("expired_session", "u1", "c1")

	time.Sleep(100 * time.Millisecond)
	sm.CleanupExpired()

	sm.mu.RLock()
	_, ok := sm.sessions["expired_session"]
	sm.mu.RUnlock()

	if ok {
		t.Error("expired session should have been cleaned up")
	}
}

func TestSessionCleanupActive(t *testing.T) {
	sm := NewSessionManager(100, 20, time.Hour, nil)
	sm.GetOrCreate("active_session", "u1", "c1")
	sm.CleanupExpired()

	sm.mu.RLock()
	_, ok := sm.sessions["active_session"]
	sm.mu.RUnlock()

	if !ok {
		t.Error("active session should not be cleaned up")
	}
}

func TestTrimMessages(t *testing.T) {
	s := &Session{}
	for i := 0; i < 10; i++ {
		s.Messages = append(s.Messages, Message{Role: RoleUser, Content: "msg"})
	}
	trimMessages(s, 3)
	if len(s.Messages) > 3 {
		t.Errorf("expected at most 3 messages after trim, got %d", len(s.Messages))
	}
}

func TestTrimMessagesPreservesSystem(t *testing.T) {
	s := &Session{
		Messages: []Message{
			{Role: RoleSystem, Content: "sys1"},
			{Role: RoleUser, Content: "u1"},
			{Role: RoleAssistant, Content: "a1"},
			{Role: RoleUser, Content: "u2"},
		},
	}
	trimMessages(s, 2)
	hasSystem := false
	for _, m := range s.Messages {
		if m.Role == RoleSystem {
			hasSystem = true
			break
		}
	}
	if !hasSystem {
		t.Error("system message should be preserved after trim")
	}
}

func TestTrimMessagesNoop(t *testing.T) {
	s := &Session{Messages: []Message{{Role: RoleUser, Content: "only one"}}}
	trimMessages(s, 10)
	if len(s.Messages) != 1 {
		t.Errorf("expected 1 message, got %d", len(s.Messages))
	}
}

func TestTrimMessagesZeroMaxHistory(t *testing.T) {
	s := &Session{Messages: []Message{{Role: RoleUser, Content: "test"}}}
	trimMessages(s, 0)
	if len(s.Messages) != 1 {
		t.Error("trimMessages with 0 maxHistory should not modify")
	}
}

func TestSessionLockUnlock(t *testing.T) {
	s := &Session{}
	s.Lock()
	s.Unlock()
}

func TestSessionLockTurnUnlockTurn(t *testing.T) {
	s := &Session{}
	s.LockTurn()
	s.UnlockTurn()
}

func TestSessionSnapshotMessages(t *testing.T) {
	sm := NewSessionManager(100, 20, time.Hour, nil)
	s := sm.GetOrCreate("snap_test", "u1", "c1")
	sm.AppendMessage(s, Message{Role: RoleUser, Content: "hello"})

	snapshot := s.SnapshotMessages()
	if len(snapshot) != 1 {
		t.Errorf("expected 1 message in snapshot, got %d", len(snapshot))
	}
	// Modify snapshot, original should be unchanged
	snapshot[0].Content = "modified"
	s.Lock()
	if s.Messages[0].Content != "hello" {
		t.Error("Snapshot should not affect original messages")
	}
	s.Unlock()
}

func TestMessagesForPersistence(t *testing.T) {
	msgs := []Message{
		{Role: RoleUser, Content: "text only"},
		{
			Role: RoleUser,
			ContentParts: []ContentPart{
				{Type: ContentPartText, Text: "part text"},
				{Type: ContentPartImage, Data: []byte("image data")},
				{Type: ContentPartAudio, Data: []byte("audio data")},
			},
		},
	}
	result := messagesForPersistence(msgs)
	if result[0].Content != "text only" {
		t.Errorf("expected %q, got %q", "text only", result[0].Content)
	}
	if len(result[1].ContentParts) != 0 {
		t.Error("ContentParts should be cleared after persistence")
	}
}

func TestSessionToRecordRoundTrip(t *testing.T) {
	s := &Session{
		ID:        "test:id",
		UserID:    "user1",
		ChatID:    "chat1",
		Messages:  []Message{{Role: RoleUser, Content: "hello"}},
		CallCount: 3,
		ToolCount: 5,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	rec := s.toRecord()
	if rec.ID != "test:id" {
		t.Errorf("expected ID %q, got %q", "test:id", rec.ID)
	}
	if rec.CallCount != 3 {
		t.Errorf("expected CallCount 3, got %d", rec.CallCount)
	}

	restored := rec.toSession()
	if restored.ID != "test:id" {
		t.Errorf("expected ID %q, got %q", "test:id", restored.ID)
	}
	if len(restored.Messages) != 1 {
		t.Errorf("expected 1 message, got %d", len(restored.Messages))
	}
	if restored.CallCount != 3 {
		t.Errorf("expected CallCount 3, got %d", restored.CallCount)
	}
}

func TestSessionRecordCorruptedJSON(t *testing.T) {
	rec := &sessionRecord{
		ID:       "corrupted",
		Messages: "{invalid json",
	}
	s := rec.toSession()
	if s.Messages != nil {
		t.Error("expected nil messages for corrupted JSON")
	}
}

func TestSessionCache(t *testing.T) {
	s := &Session{}
	cached := s.getCachedContent("http://example.com/img.png")
	if cached != nil {
		t.Error("expected nil for empty cache")
	}

	s.setCachedContent("http://example.com/img.png", []byte("data"), "image/png", "")
	cached = s.getCachedContent("http://example.com/img.png")
	if cached == nil {
		t.Fatal("expected cached content")
	}
	if string(cached.Data) != "data" {
		t.Errorf("expected data %q, got %q", "data", string(cached.Data))
	}
}

func TestSessionCacheExpired(t *testing.T) {
	s := &Session{}
	s.setCachedContent("http://example.com/img.png", []byte("data"), "image/png", "")
	// Manipulate cache expiry
	s.Lock()
	if s.contentCache != nil {
		s.contentCache["http://example.com/img.png"].ExpireAt = time.Now().Add(-time.Minute)
	}
	s.Unlock()

	cached := s.getCachedContent("http://example.com/img.png")
	if cached != nil {
		t.Error("expected nil for expired cache")
	}
}

func TestSessionRecordJSONRoundTrip(t *testing.T) {
	msg := Message{Role: RoleUser, Content: "test"}
	data, _ := json.Marshal(msg)
	var restored Message
	json.Unmarshal(data, &restored)
	if restored.Role != RoleUser {
		t.Errorf("expected RoleUser, got %v", restored.Role)
	}
}
