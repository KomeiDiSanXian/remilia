package ai

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
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
	if len(s.Messages) != 0 {
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
	for range 10 {
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

func TestTrimMessagesDoesNotLeaveOrphanToolMessage(t *testing.T) {
	// 边界恰好落在 assistant(tool_calls) 与其 tool 响应之间时，
	// 起点应向前推进，首条保留消息不能是 tool（否则 API 会以 400 拒绝）。
	s := &Session{
		Messages: []Message{
			{Role: RoleSystem, Content: "sys1"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c1"}, {ID: "c2"}}},
			{Role: RoleTool, ToolCallID: "c1", Content: "r1"},
			{Role: RoleTool, ToolCallID: "c2", Content: "r2"},
			{Role: RoleUser, Content: "u1"},
		},
	}
	trimMessages(s, 3)
	if len(s.Messages) > 3 {
		t.Errorf("expected at most 3 messages after trim, got %d", len(s.Messages))
	}
	for _, m := range s.Messages {
		if m.Role == RoleTool {
			t.Errorf("trimmed messages must not contain orphan tool messages, got %+v", s.Messages)
		}
	}
	if len(s.Messages) == 0 || s.Messages[len(s.Messages)-1].Content != "u1" {
		t.Errorf("recent user message should be retained, got %+v", s.Messages)
	}
}

func TestTrimMessagesZeroMaxHistory(t *testing.T) {
	s := &Session{Messages: []Message{{Role: RoleUser, Content: "test"}}}
	trimMessages(s, 0)
	if len(s.Messages) != 1 {
		t.Error("trimMessages with 0 maxHistory should not modify")
	}
}

func TestPrepareRequestMessagesKeepsLatestUserParts(t *testing.T) {
	msgs := []Message{
		{Role: RoleSystem, Content: "sys"},
		{Role: RoleUser, Content: "", ContentParts: []ContentPart{
			{Type: ContentPartImage, Data: []byte("img1"), MimeType: "image/png"},
		}},
		{Role: RoleAssistant, Content: "reply"},
		{Role: RoleUser, Content: "", ContentParts: []ContentPart{
			{Type: ContentPartText, Text: "new question"},
			{Type: ContentPartImage, Data: []byte("img2"), MimeType: "image/png"},
		}},
	}

	retention := imageRetention{maxTurns: 5, window: 10 * time.Minute, maxPerRequest: 8}
	out, budgetDropped, staleDropped := prepareRequestMessages(msgs, retention)

	// 最后一条用户消息保留完整 ContentParts（含二进制数据）
	last := out[len(out)-1]
	if len(last.ContentParts) != 2 {
		t.Fatalf("expected latest user message to keep 2 content parts, got %d", len(last.ContentParts))
	}
	if len(last.ContentParts[1].Data) != 4 {
		t.Errorf("expected binary data retained for latest user message")
	}

	// 历史用户消息在保留窗口内（最近 5 条 + 10 分钟内）：二进制保留，支持追问图片细节
	hist := out[1]
	if len(hist.ContentParts) != 1 || len(hist.ContentParts[0].Data) != 4 {
		t.Errorf("expected historical image retained within context window, got %+v", hist.ContentParts)
	}
	if budgetDropped != 0 || staleDropped != 0 {
		t.Errorf("expected no dropped images, got budget=%d stale=%d", budgetDropped, staleDropped)
	}

	// 原始切片不受影响
	if len(msgs[1].ContentParts) != 1 {
		t.Errorf("prepareRequestMessages must not mutate input")
	}
}

func TestPrepareRequestMessagesDropsBeyondContextTurns(t *testing.T) {
	msgs := []Message{
		{Role: RoleUser, Content: "", ContentParts: []ContentPart{{Type: ContentPartImage, Data: []byte("img0")}}},
		{Role: RoleAssistant, Content: "r1"},
		{Role: RoleUser, Content: "q1"},
		{Role: RoleAssistant, Content: "r2"},
		{Role: RoleUser, Content: "q2"},
	}

	// maxTurns=2：只保留最近 2 条 user 消息（q1、q2）；img0 为第 3 旧 → 降级
	out, budgetDropped, staleDropped := prepareRequestMessages(msgs, imageRetention{maxTurns: 2, window: 0, maxPerRequest: 8})
	if staleDropped != 1 || budgetDropped != 0 {
		t.Fatalf("expected 1 stale-dropped image, got budget=%d stale=%d", budgetDropped, staleDropped)
	}
	if len(out[0].ContentParts) != 0 || out[0].Content == "" {
		t.Errorf("expected oldest user image stripped to placeholder, got %+v", out[0])
	}
	if len(out[2].ContentParts) != 0 || out[2].Content != "q1" {
		t.Errorf("expected q1 retained without parts, got %+v", out[2])
	}
}

func TestPrepareRequestMessagesDropsStaleImagesByWindow(t *testing.T) {
	now := time.Now()
	msgs := []Message{
		{Role: RoleUser, Content: "", Timestamp: now.Add(-30 * time.Minute),
			ContentParts: []ContentPart{{Type: ContentPartImage, Data: []byte("old")}}},
		{Role: RoleAssistant, Content: "r"},
		{Role: RoleUser, Content: "q", Timestamp: now},
	}

	// 时间窗 10 分钟：30 分钟前的图片即使条数在窗口内也降级
	out, budgetDropped, staleDropped := prepareRequestMessages(msgs, imageRetention{maxTurns: 5, window: 10 * time.Minute, maxPerRequest: 8})
	if staleDropped != 1 || budgetDropped != 0 {
		t.Fatalf("expected 1 stale-dropped image, got budget=%d stale=%d", budgetDropped, staleDropped)
	}
	if len(out[0].ContentParts) != 0 {
		t.Errorf("expected stale image stripped, got %+v", out[0].ContentParts)
	}
}

func TestPrepareRequestMessagesCapsImagesPerRequest(t *testing.T) {
	now := time.Now()
	msgs := []Message{
		{Role: RoleUser, Content: "", Timestamp: now.Add(-1 * time.Minute),
			ContentParts: []ContentPart{
				{Type: ContentPartImage, Data: []byte("a")},
				{Type: ContentPartImage, Data: []byte("b")},
			}},
		{Role: RoleAssistant, Content: "r"},
		{Role: RoleUser, Content: "", Timestamp: now,
			ContentParts: []ContentPart{
				{Type: ContentPartImage, Data: []byte("c")},
				{Type: ContentPartImage, Data: []byte("d")},
				{Type: ContentPartImage, Data: []byte("e")},
				{Type: ContentPartImage, Data: []byte("f")},
			}},
	}

	// maxPerRequest=5：当前轮 4 张 + 历史 2 张 → 预算只够 1 张，历史整条降级
	out, budgetDropped, staleDropped := prepareRequestMessages(msgs, imageRetention{maxTurns: 5, window: 10 * time.Minute, maxPerRequest: 5})
	if budgetDropped != 2 || staleDropped != 0 {
		t.Fatalf("expected 2 budget-dropped images, got budget=%d stale=%d", budgetDropped, staleDropped)
	}
	if len(out[0].ContentParts) != 0 {
		t.Errorf("expected over-budget historical images stripped, got %+v", out[0].ContentParts)
	}
	last := out[len(out)-1]
	if countImageParts(last.ContentParts) != 4 {
		t.Errorf("expected current turn 4 images retained, got %d", countImageParts(last.ContentParts))
	}
}

func TestPrepareRequestMessagesMaxTurnsZeroKeepsOnlyCurrent(t *testing.T) {
	msgs := []Message{
		{Role: RoleUser, Content: "", ContentParts: []ContentPart{{Type: ContentPartImage, Data: []byte("old")}}},
		{Role: RoleAssistant, Content: "r"},
		{Role: RoleUser, Content: "q"},
	}

	// maxTurns=0：仅当前轮保留附件（旧行为）
	out, budgetDropped, staleDropped := prepareRequestMessages(msgs, imageRetention{maxTurns: 0, window: 0, maxPerRequest: 8})
	if staleDropped != 1 || budgetDropped != 0 {
		t.Fatalf("expected 1 stale-dropped image, got budget=%d stale=%d", budgetDropped, staleDropped)
	}
	if len(out[0].ContentParts) != 0 {
		t.Errorf("expected historical image stripped when maxTurns=0, got %+v", out[0].ContentParts)
	}
}

func TestTruncateToolResult(t *testing.T) {
	short := "short result"
	if got := truncateToolResult(short); got != short {
		t.Errorf("expected short result unchanged, got %q", got)
	}

	long := strings.Repeat("字", 9000) // 9000 runes > 8000 limit
	got := truncateToolResult(long)
	runes := []rune(got)
	if len(runes) != maxToolResultLen+len([]rune("\n…(工具结果过长已截断)")) {
		t.Errorf("expected truncated length, got %d runes", len(runes))
	}
	// 截断后仍是合法 UTF-8（rune 边界）
	if !utf8.ValidString(got) {
		t.Error("truncated result should be valid UTF-8")
	}
	if !strings.HasSuffix(got, "…(工具结果过长已截断)") {
		t.Errorf("expected truncation marker suffix, got %q", got[len(got)-20:])
	}
}

func TestSessionLockUnlock(t *testing.T) {
	s := &Session{ID: "lock-test"}
	s.Lock()
	if s.ID != "lock-test" {
		t.Error("expected ID preserved under lock")
	}
	s.Unlock()
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

func TestSessionPendingImageExtendAndConsume(t *testing.T) {
	s := &Session{}
	now := time.Now()
	imgPart := func(data string) []ContentPart {
		return []ContentPart{{Type: ContentPartImage, Data: []byte(data)}}
	}

	s.extendPendingImage(imgPart("a"), now, 30*time.Second, 4)
	s.extendPendingImage(imgPart("b"), now.Add(5*time.Second), 30*time.Second, 4)

	// 窗口内：返回累积的 2 张图，并清除 pending
	parts, extra := s.consumePendingImage(30*time.Second, now.Add(6*time.Second))
	if len(parts) != 2 {
		t.Fatalf("expected 2 accumulated pending images, got %d", len(parts))
	}
	if extra != 0 {
		t.Fatalf("expected 0 extra images, got %d", extra)
	}
	// 已消费：再次调用返回 nil
	if got, extra := s.consumePendingImage(30*time.Second, now.Add(10*time.Second)); got != nil || extra != 0 {
		t.Errorf("expected nil after consume, got %+v extra=%d", got, extra)
	}
}

func TestSessionPendingImageExpired(t *testing.T) {
	s := &Session{}
	now := time.Now()
	s.extendPendingImage([]ContentPart{{Type: ContentPartImage, Data: []byte("a")}}, now, 30*time.Second, 4)

	// 超过窗口：消费返回 nil（静默丢弃，表情包防误触发）
	if got, extra := s.consumePendingImage(30*time.Second, now.Add(31*time.Second)); got != nil || extra != 0 {
		t.Errorf("expected nil for expired pending, got %+v extra=%d", got, extra)
	}

	// 过期后的新图片开启新窗口
	s.extendPendingImage([]ContentPart{{Type: ContentPartImage, Data: []byte("b")}}, now.Add(40*time.Second), 30*time.Second, 4)
	if got, _ := s.consumePendingImage(30*time.Second, now.Add(45*time.Second)); len(got) != 1 {
		t.Errorf("expected new pending window valid, got %+v", got)
	}
}

func TestSessionPendingImageZeroWindowAlwaysValid(t *testing.T) {
	s := &Session{}
	now := time.Now()
	// window <= 0 表示合并关闭：pending 不被记录（extend 不会创建窗口时也直接返回？）
	// 这里验证 window<=0 时 pending 永不失效（虽然入口已禁止记录，防御性验证）。
	s.extendPendingImage([]ContentPart{{Type: ContentPartImage, Data: []byte("a")}}, now, 0, 4)
	if got, _ := s.consumePendingImage(0, now.Add(24*time.Hour)); len(got) != 1 {
		t.Errorf("expected pending valid when window=0, got %+v", got)
	}
}

func TestSessionPendingImageRejectPath(t *testing.T) {
	s := &Session{}
	now := time.Now()
	s.extendPendingImage([]ContentPart{{Type: ContentPartImage, Data: []byte("a")}}, now, 30*time.Second, 4)
	// 图片数量超限拒绝时清空 pending，避免残留状态
	s.clearPendingImage()
	if got, extra := s.consumePendingImage(30*time.Second, now.Add(5*time.Second)); got != nil || extra != 0 {
		t.Errorf("expected pending cleared after reject, got %+v", got)
	}
}

func TestSessionPendingImageExtraCount(t *testing.T) {
	s := &Session{}
	now := time.Now()
	imgPart := func(data string) []ContentPart {
		return []ContentPart{{Type: ContentPartImage, Data: []byte(data)}}
	}

	// maxKeep=2：3 张图只持有前 2 张二进制，第 3 张仅计数
	s.extendPendingImage(imgPart("a"), now, 30*time.Second, 2)
	s.extendPendingImage(imgPart("b"), now.Add(1*time.Second), 30*time.Second, 2)
	s.extendPendingImage(imgPart("c"), now.Add(2*time.Second), 30*time.Second, 2)

	parts, extra := s.consumePendingImage(30*time.Second, now.Add(3*time.Second))
	if len(parts) != 2 {
		t.Fatalf("expected 2 held image parts, got %d", len(parts))
	}
	if extra != 1 {
		t.Fatalf("expected 1 extra image count, got %d", extra)
	}
}
