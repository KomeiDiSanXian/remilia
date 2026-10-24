// session_test.go — 会话状态与持久化记录的语义契约。
//
// 这些用例把"会话是什么、如何被安全读写、哪些状态跨重启保留"钉成可执行契约；
// 会话之外的策略（选择、检索、执行）不在本包范围内。
package session

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mustToRecord / mustToSession 是往返断言用的便捷包装：序列化失败在用例里
// 属于不可恢复的前置条件，直接 fail 比到处判错更清晰。
func mustToRecord(t *testing.T, s *Session) *Record {
	t.Helper()
	rec, err := s.ToRecord()
	require.NoError(t, err)
	return rec
}

func mustToSession(t *testing.T, r *Record) *Session {
	t.Helper()
	s, err := r.ToSession()
	require.NoError(t, err)
	return s
}

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
	msg := protocol.Message{Role: protocol.RoleUser, Content: "hello"}
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
		s.Messages = append(s.Messages, protocol.Message{Role: protocol.RoleUser, Content: "msg"})
	}
	TrimMessages(s, 3)
	if len(s.Messages) > 3 {
		t.Errorf("expected at most 3 messages after trim, got %d", len(s.Messages))
	}
}

func TestTrimMessagesPreservesSystem(t *testing.T) {
	s := &Session{
		Messages: []protocol.Message{
			{Role: protocol.RoleSystem, Content: "sys1"},
			{Role: protocol.RoleUser, Content: "u1"},
			{Role: protocol.RoleAssistant, Content: "a1"},
			{Role: protocol.RoleUser, Content: "u2"},
		},
	}
	TrimMessages(s, 2)
	hasSystem := false
	for _, m := range s.Messages {
		if m.Role == protocol.RoleSystem {
			hasSystem = true
			break
		}
	}
	if !hasSystem {
		t.Error("system message should be preserved after trim")
	}
}

func TestTrimMessagesNoop(t *testing.T) {
	s := &Session{Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "only one"}}}
	TrimMessages(s, 10)
	if len(s.Messages) != 1 {
		t.Errorf("expected 1 message, got %d", len(s.Messages))
	}
}

func TestTrimMessagesDoesNotLeaveOrphanToolMessage(t *testing.T) {
	// 边界恰好落在 assistant(tool_calls) 与其 tool 响应之间时，
	// 起点应向前推进，首条保留消息不能是 tool（否则 API 会以 400 拒绝）。
	s := &Session{
		Messages: []protocol.Message{
			{Role: protocol.RoleSystem, Content: "sys1"},
			{Role: protocol.RoleAssistant, ToolCalls: []protocol.ToolCall{{ID: "c1"}, {ID: "c2"}}},
			{Role: protocol.RoleTool, ToolCallID: "c1", Content: "r1"},
			{Role: protocol.RoleTool, ToolCallID: "c2", Content: "r2"},
			{Role: protocol.RoleUser, Content: "u1"},
		},
	}
	TrimMessages(s, 3)
	if len(s.Messages) > 3 {
		t.Errorf("expected at most 3 messages after trim, got %d", len(s.Messages))
	}
	for _, m := range s.Messages {
		if m.Role == protocol.RoleTool {
			t.Errorf("trimmed messages must not contain orphan tool messages, got %+v", s.Messages)
		}
	}
	if len(s.Messages) == 0 || s.Messages[len(s.Messages)-1].Content != "u1" {
		t.Errorf("recent user message should be retained, got %+v", s.Messages)
	}
}

func TestTrimMessagesZeroMaxHistory(t *testing.T) {
	s := &Session{Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "test"}}}
	TrimMessages(s, 0)
	if len(s.Messages) != 1 {
		t.Error("TrimMessages with 0 maxHistory should not modify")
	}
}

// TestTrimMessagesKeepsPrefixStable 验证裁剪是批量的：超限时一次裁到低水位，
// 之后连续追加少量消息不会再触发裁剪。逐条滑窗会让每次请求的历史前缀都发生
// 位移，LLM 侧的前缀缓存从第一条历史消息起整段失效。
func TestTrimMessagesKeepsPrefixStable(t *testing.T) {
	s := &Session{Messages: []protocol.Message{{Role: protocol.RoleSystem, Content: "sys"}}}
	for i := 1; i <= 21; i++ {
		s.Messages = append(s.Messages, protocol.Message{Role: protocol.RoleUser, Content: fmt.Sprintf("u%d", i)})
	}

	TrimMessages(s, 20)
	if len(s.Messages) > 20 {
		t.Fatalf("expected at most 20 messages after trim, got %d", len(s.Messages))
	}
	if len(s.Messages) < 2 {
		t.Fatalf("trim must keep recent messages, got %+v", s.Messages)
	}
	first := s.Messages[1].Content

	// 追加后仍在上限以内：前缀应逐字不动
	s.Messages = append(s.Messages,
		protocol.Message{Role: protocol.RoleUser, Content: "u22"},
		protocol.Message{Role: protocol.RoleAssistant, Content: "a22"},
		protocol.Message{Role: protocol.RoleUser, Content: "u23"},
	)
	TrimMessages(s, 20)
	if s.Messages[1].Content != first {
		t.Errorf("retained window and cacheable prefix shifted: got %q want %q", s.Messages[1].Content, first)
	}
}

// TestTrimMessagesHeadroomScalesWithWindow 验证低水位余量随窗口放大：
// 大窗口（生产 max_history=50）一次让出 12 条余量（≈4 轮），裁剪间隔变长；
// 小窗口维持 TrimLowWaterMin 下限，不退化成逐轮裁剪。
func TestTrimMessagesHeadroomScalesWithWindow(t *testing.T) {
	cases := []struct{ targetOther, want int }{
		{0, 0}, {1, TrimLowWaterMin}, {4, TrimLowWaterMin},
		{19, TrimLowWaterMin}, {49, 12}, {99, 24},
	}
	for _, tc := range cases {
		if got := trimHeadroom(tc.targetOther); got != tc.want {
			t.Errorf("trimHeadroom(%d) = %d, want %d", tc.targetOther, got, tc.want)
		}
	}

	s := &Session{Messages: []protocol.Message{{Role: protocol.RoleSystem, Content: "sys"}}}
	for i := 1; i <= 50; i++ {
		s.Messages = append(s.Messages, protocol.Message{Role: protocol.RoleUser, Content: fmt.Sprintf("u%d", i)})
	}

	TrimMessages(s, 50)
	// 保留 49-12=37 条非 system 消息
	if len(s.Messages) != 38 {
		t.Fatalf("expected 38 messages after trim, got %d", len(s.Messages))
	}
	first := s.Messages[1].Content

	// 余量以内继续追加：前缀逐字不动
	for i := 51; i <= 62; i++ {
		s.Messages = append(s.Messages, protocol.Message{Role: protocol.RoleUser, Content: fmt.Sprintf("u%d", i)})
	}
	TrimMessages(s, 50)
	if s.Messages[1].Content != first {
		t.Errorf("retained prefix shifted within headroom: got %q want %q", s.Messages[1].Content, first)
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
	sm.AppendMessage(s, protocol.Message{Role: protocol.RoleUser, Content: "hello"})

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

// TestMessagesForPersistence 落库不写附件二进制，但保留引用（AttachmentID/SourceURL），
// 让重启后还能按引用取回；完全没有引用的附件退化为文本占位。
func TestMessagesForPersistence(t *testing.T) {
	msgs := []protocol.Message{
		{Role: protocol.RoleUser, Content: "text only"},
		{
			Role: protocol.RoleUser,
			ContentParts: []protocol.ContentPart{
				{Type: protocol.ContentPartText, Text: "part text"},
				{Type: protocol.ContentPartImage, Data: []byte("image data"), SourceURL: "https://ex/a.png", AttachmentID: 7, MimeType: "image/png"},
				{Type: protocol.ContentPartAudio, Data: []byte("audio data")},
			},
		},
	}
	result := MessagesForPersistence(msgs)
	if result[0].Content != "text only" {
		t.Errorf("expected %q, got %q", "text only", result[0].Content)
	}

	parts := result[1].ContentParts
	if len(parts) != 2 {
		t.Fatalf("expected text + referenced image parts, got %+v", parts)
	}
	if parts[1].SourceURL != "https://ex/a.png" || parts[1].AttachmentID != 7 {
		t.Errorf("attachment reference must survive persistence: %+v", parts[1])
	}
	if len(parts[1].Data) != 0 {
		t.Error("binary must never be persisted with the message")
	}
	// 无引用的音频无法恢复，降级为文本占位。
	if !strings.Contains(result[1].Content, "[用户发送了音频") {
		t.Errorf("unreferenced audio should degrade to a text placeholder, got %q", result[1].Content)
	}

	// 序列化后不得出现二进制内容。
	rec := mustToRecord(t, &Session{ID: "s", Messages: msgs})
	for _, leaked := range []string{"image data", "audio data"} {
		if strings.Contains(rec.Messages, leaked) {
			t.Errorf("persisted record must not contain attachment bytes (%q)", leaked)
		}
	}
}

// TestSessionRecordKeepsAttachmentRefs 附件引用随历史落库：重启后仍能按
// AttachmentID / SourceURL 取回二进制，但落库内容里不含任何字节。
func TestSessionRecordKeepsAttachmentRefs(t *testing.T) {
	src := &Session{
		ID: "p:c:u",
		Messages: []protocol.Message{{
			Role: protocol.RoleUser,
			ContentParts: []protocol.ContentPart{
				{Type: protocol.ContentPartText, Text: "看图"},
				{Type: protocol.ContentPartImage, SourceURL: "https://ex/a.png", AttachmentID: 42, MimeType: "image/png", Data: []byte("secret-bytes")},
			},
		}},
	}

	rec := mustToRecord(t, src)
	assert.NotContains(t, rec.Messages, "secret-bytes", "attachment bytes must never reach the database")

	restored := mustToSession(t, rec)
	require.Len(t, restored.Messages, 1)
	parts := restored.Messages[0].ContentParts
	require.Len(t, parts, 2, "text + referenced image")
	assert.Equal(t, int64(42), parts[1].AttachmentID)
	assert.Equal(t, "https://ex/a.png", parts[1].SourceURL)
	assert.Equal(t, "image/png", parts[1].MimeType)
	assert.Empty(t, parts[1].Data, "bytes are re-hydrated on request, never stored")
}

func TestSessionToRecordRoundTrip(t *testing.T) {
	s := &Session{
		ID:        "test:id",
		UserID:    "user1",
		ChatID:    "chat1",
		Messages:  []protocol.Message{{Role: protocol.RoleUser, Content: "hello"}},
		CallCount: 3,
		ToolCount: 5,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	rec := mustToRecord(t, s)
	if rec.ID != "test:id" {
		t.Errorf("expected ID %q, got %q", "test:id", rec.ID)
	}
	if rec.CallCount != 3 {
		t.Errorf("expected CallCount 3, got %d", rec.CallCount)
	}

	restored := mustToSession(t, rec)
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

// TestSessionRecordCorruptedJSON 历史损坏必须上抛错误，不能降级成"空历史"：
// 空历史会在下一次落库时把库里那行覆盖掉，等于把可修复的读失败变成永久丢数据。
func TestSessionRecordCorruptedJSON(t *testing.T) {
	rec := &Record{
		ID:       "corrupted",
		Messages: "{invalid json",
	}
	if _, err := rec.ToSession(); err == nil {
		t.Fatal("expected error for corrupted messages JSON")
	}

	// 空列不算损坏：历史版本/外部工具写入的裸行可能是空串。
	empty, err := (&Record{ID: "empty", Messages: ""}).ToSession()
	if err != nil {
		t.Fatalf("empty messages should not be treated as corrupted: %v", err)
	}
	if len(empty.Messages) != 0 {
		t.Errorf("expected no messages, got %d", len(empty.Messages))
	}
}

func TestSessionCache(t *testing.T) {
	s := &Session{}
	cached := s.CachedContent("http://example.com/img.png")
	if cached != nil {
		t.Error("expected nil for empty cache")
	}

	s.SetCachedContent("http://example.com/img.png", []byte("data"), "image/png", "")
	cached = s.CachedContent("http://example.com/img.png")
	if cached == nil {
		t.Fatal("expected cached content")
	}
	if string(cached.Data) != "data" {
		t.Errorf("expected data %q, got %q", "data", string(cached.Data))
	}
}

func TestSessionCacheExpired(t *testing.T) {
	s := &Session{}
	s.SetCachedContent("http://example.com/img.png", []byte("data"), "image/png", "")
	// Manipulate cache expiry
	s.Lock()
	if s.contentCache != nil {
		s.contentCache["http://example.com/img.png"].ExpireAt = time.Now().Add(-time.Minute)
	}
	s.Unlock()

	cached := s.CachedContent("http://example.com/img.png")
	if cached != nil {
		t.Error("expected nil for expired cache")
	}
}

// TestContentCacheByteBudget 附件缓存必须同时受字节预算约束：只按条数限流时，
// 32 条 × max_attachment_size(默认 20MB) 就是 640MB/会话。
func TestContentCacheByteBudget(t *testing.T) {
	const budget = int64(10 << 20) // 10MB
	const chunk = 3 << 20          // 每张 3MB

	s := &Session{}
	for _, url := range []string{"a", "b", "c", "d"} {
		s.SetCachedContentWithBudget(url, make([]byte, chunk), "image/png", "", budget)
	}

	s.Lock()
	total := s.contentCacheBytesLocked()
	entries := len(s.contentCache)
	s.Unlock()

	assert.LessOrEqual(t, total, budget, "cache must respect the byte budget")
	assert.Equal(t, 3, entries, "oldest entry should be evicted to fit the budget")
	assert.NotNil(t, s.CachedContent("d"), "the just-written entry must be kept")
	assert.Nil(t, s.CachedContent("a"), "oldest entry should be gone")
}

// TestContentCacheKeepsOversizedEntry 单条超过预算时保留刚写入的那条：
// 刚下载完就丢弃等于白下载，且下一次请求还会再下一遍。
func TestContentCacheKeepsOversizedEntry(t *testing.T) {
	const budget = int64(1 << 20)
	s := &Session{}
	s.SetCachedContentWithBudget("big", make([]byte, 4<<20), "image/png", "", budget)

	assert.NotNil(t, s.CachedContent("big"))
}

// TestStripBinaryPartsOlderThan 回合结束后只释放"请求时本来就不会再发送"的
// 历史附件；当前轮（时间戳在窗口内）与时间戳未知的消息必须原样保留。
func TestStripBinaryPartsOlderThan(t *testing.T) {
	now := time.Now()
	s := &Session{Messages: []protocol.Message{
		{Role: protocol.RoleUser, Timestamp: now.Add(-time.Hour), ContentParts: []protocol.ContentPart{
			{Type: protocol.ContentPartText, Text: "看这张"},
			{Type: protocol.ContentPartImage, Data: []byte("old-bytes")},
		}},
		{Role: protocol.RoleUser, Timestamp: now, ContentParts: []protocol.ContentPart{
			{Type: protocol.ContentPartImage, Data: []byte("fresh-bytes")},
		}},
		{Role: protocol.RoleUser, ContentParts: []protocol.ContentPart{
			{Type: protocol.ContentPartImage, Data: []byte("unknown-ts")},
		}},
	}}

	released := s.StripBinaryPartsOlderThan(now.Add(-10 * time.Minute))
	assert.Equal(t, 2, released, "only the stale message's parts are released")

	msgs := s.SnapshotMessages()
	assert.Nil(t, msgs[0].ContentParts, "stale parts must be downgraded")
	assert.Equal(t, "看这张\n[历史图片未随本次请求发送]", msgs[0].Content)
	assert.Len(t, msgs[1].ContentParts, 1, "current turn must keep its images")
	assert.Len(t, msgs[2].ContentParts, 1, "unknown timestamp must not be touched")

	// 幂等：已降级的消息不再重复计数。
	assert.Equal(t, 0, s.StripBinaryPartsOlderThan(now.Add(-10*time.Minute)))
}

func TestSessionRecordJSONRoundTrip(t *testing.T) {
	msg := protocol.Message{Role: protocol.RoleUser, Content: "test"}
	data, _ := json.Marshal(msg)
	var restored protocol.Message
	json.Unmarshal(data, &restored)
	if restored.Role != protocol.RoleUser {
		t.Errorf("expected RoleUser, got %v", restored.Role)
	}
}

func TestSessionPendingImageExtendAndConsume(t *testing.T) {
	s := &Session{}
	now := time.Now()
	imgRef := func(id string) []PendingImageRef {
		return []PendingImageRef{{ChatID: "g1", PlatformMsgID: id, URL: "http://x/" + id + ".png", MimeType: "image/png"}}
	}

	s.ExtendPendingImage(imgRef("a"), now, 30*time.Second, 4)
	s.ExtendPendingImage(imgRef("b"), now.Add(5*time.Second), 30*time.Second, 4)

	// 窗口内：返回累积的 2 张图，并清除 pending
	refs, extra := s.ConsumePendingImage(30*time.Second, now.Add(6*time.Second))
	if len(refs) != 2 {
		t.Fatalf("expected 2 accumulated pending image refs, got %d", len(refs))
	}
	if refs[0].PlatformMsgID != "a" || refs[1].PlatformMsgID != "b" {
		t.Errorf("expected refs in order a,b, got %+v", refs)
	}
	if extra != 0 {
		t.Fatalf("expected 0 extra images, got %d", extra)
	}
	// 已消费：再次调用返回 nil
	if got, extra := s.ConsumePendingImage(30*time.Second, now.Add(10*time.Second)); got != nil || extra != 0 {
		t.Errorf("expected nil after consume, got %+v extra=%d", got, extra)
	}
}

func TestSessionPendingImageExpired(t *testing.T) {
	s := &Session{}
	now := time.Now()
	s.ExtendPendingImage([]PendingImageRef{{ChatID: "g1", PlatformMsgID: "a"}}, now, 30*time.Second, 4)

	// 超过窗口：消费返回 nil（静默丢弃，表情包防误触发）
	if got, extra := s.ConsumePendingImage(30*time.Second, now.Add(31*time.Second)); got != nil || extra != 0 {
		t.Errorf("expected nil for expired pending, got %+v extra=%d", got, extra)
	}

	// 过期后的新图片开启新窗口
	s.ExtendPendingImage([]PendingImageRef{{ChatID: "g1", PlatformMsgID: "b"}}, now.Add(40*time.Second), 30*time.Second, 4)
	if got, _ := s.ConsumePendingImage(30*time.Second, now.Add(45*time.Second)); len(got) != 1 {
		t.Errorf("expected new pending window valid, got %+v", got)
	}
}

func TestSessionPendingImageZeroWindowAlwaysValid(t *testing.T) {
	s := &Session{}
	now := time.Now()
	// window <= 0 表示合并关闭：pending 不被记录（extend 不会创建窗口时也直接返回？）
	// 这里验证 window<=0 时 pending 永不失效（虽然入口已禁止记录，防御性验证）。
	s.ExtendPendingImage([]PendingImageRef{{ChatID: "g1", PlatformMsgID: "a"}}, now, 0, 4)
	if got, _ := s.ConsumePendingImage(0, now.Add(24*time.Hour)); len(got) != 1 {
		t.Errorf("expected pending valid when window=0, got %+v", got)
	}
}

func TestSessionPendingImageExtraCount(t *testing.T) {
	s := &Session{}
	now := time.Now()
	imgRef := func(id string) []PendingImageRef {
		return []PendingImageRef{{ChatID: "g1", PlatformMsgID: id, URL: "http://x/" + id + ".png"}}
	}

	// maxKeep=2：3 张图只持有前 2 张二进制，第 3 张仅计数
	s.ExtendPendingImage(imgRef("a"), now, 30*time.Second, 2)
	s.ExtendPendingImage(imgRef("b"), now.Add(1*time.Second), 30*time.Second, 2)
	s.ExtendPendingImage(imgRef("c"), now.Add(2*time.Second), 30*time.Second, 2)

	refs, extra := s.ConsumePendingImage(30*time.Second, now.Add(3*time.Second))
	if len(refs) != 2 {
		t.Fatalf("expected 2 held image refs, got %d", len(refs))
	}
	if extra != 1 {
		t.Fatalf("expected 1 extra image count, got %d", extra)
	}
}

// TestSessionPendingImagePersistence 引用随会话记录持久化，重启后可恢复。
func TestSessionPendingImagePersistence(t *testing.T) {
	s := &Session{ID: "s1", UserID: "u1", ChatID: "g1"}
	now := time.Now()
	s.ExtendPendingImage([]PendingImageRef{
		{ChatID: "g1", PlatformMsgID: "img-1", URL: "http://x/1.png", MimeType: "image/png"},
		{ChatID: "g1", PlatformMsgID: "img-2", URL: "http://x/2.png"},
	}, now, 30*time.Second, 4)

	rec := mustToRecord(t, s)
	if rec.PendingImages == "" {
		t.Fatal("expected pending images serialized in session record")
	}

	restored := mustToSession(t, rec)
	refs, extra := restored.ConsumePendingImage(30*time.Second, now.Add(5*time.Second))
	if extra != 0 {
		t.Errorf("expected no extra, got %d", extra)
	}
	if len(refs) != 2 {
		t.Fatalf("expected 2 restored refs, got %d", len(refs))
	}
	if refs[0].PlatformMsgID != "img-1" || refs[0].URL != "http://x/1.png" || refs[0].MimeType != "image/png" {
		t.Errorf("ref[0] mismatch: %+v", refs[0])
	}
	if refs[1].PlatformMsgID != "img-2" {
		t.Errorf("ref[1] mismatch: %+v", refs[1])
	}

	// 窗口过期后恢复：消费返回 nil（时间戳参与判定）
	restored2 := mustToSession(t, rec)
	if got, _ := restored2.ConsumePendingImage(30*time.Second, now.Add(time.Hour)); got != nil {
		t.Errorf("expected nil after window expiry post-restore, got %+v", got)
	}
}

// TestAgentStatePersistenceRoundTrip 落库状态跨重启逐项还原，
// 其余会话内存态必须重置为初始值。归属表与持久化 schema 的对照见
// builtin/ai 的代理状态清单，这里验证的是会话记录自身的往返语义。
func TestAgentStatePersistenceRoundTrip(t *testing.T) {
	createdAt := time.Date(2024, 5, 1, 12, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(90 * time.Minute)
	imageAt := createdAt.Add(time.Minute)

	s := &Session{
		ID:        "qq:group:1:user:2",
		UserID:    "2",
		ChatID:    "1",
		Messages:  []protocol.Message{{Role: protocol.RoleUser, Content: "你好"}, {Role: protocol.RoleAssistant, Content: "在的"}},
		CallCount: 3,
		ToolCount: 5,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}
	s.SetPlan(&Plan{
		Task:   "跨重启任务",
		Active: true,
		Steps:  []PlanStep{{ID: "step_1", Description: "第一步", Status: PlanInProgress}},
	})
	s.pendingImage = &PendingImageState{
		Parts: []PendingImageRef{{
			ChatID: "1", PlatformMsgID: "m1", URL: "https://example.invalid/1.png", MimeType: "image/png",
		}},
		Extra:     2,
		Timestamp: imageAt,
	}

	restored := mustToSession(t, mustToRecord(t, s))

	assert.Equal(t, s.ID, restored.ID)
	assert.Equal(t, s.UserID, restored.UserID)
	assert.Equal(t, s.ChatID, restored.ChatID)
	assert.Equal(t, s.Messages, restored.Messages)
	assert.Equal(t, s.CallCount, restored.CallCount)
	assert.Equal(t, s.ToolCount, restored.ToolCount)
	assert.True(t, s.CreatedAt.Equal(restored.CreatedAt))
	assert.True(t, s.UpdatedAt.Equal(restored.UpdatedAt))

	require.NotNil(t, restored.plan)
	assert.Equal(t, *s.plan, *restored.plan)
	require.NotNil(t, restored.pendingImage)
	assert.Equal(t, *s.pendingImage, *restored.pendingImage)

	assert.Nil(t, restored.toolSet)
	assert.Nil(t, restored.selCache)
	assert.Nil(t, restored.ragCache)
	assert.Nil(t, restored.trace)
	assert.Nil(t, restored.contentCache)
	assert.Nil(t, restored.toolFailures)
	assert.Zero(t, restored.PlanAutoRounds())
	assert.False(t, restored.PlanAutoStopped())
	assert.False(t, restored.imageOverflowNotified)
	assert.False(t, restored.TurnActive())
}

func TestNoopSessionStore(t *testing.T) {
	store := &NoopStore{}
	s, err := store.Load("test")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if s != nil {
		t.Error("expected nil session from noop store")
	}
	if err := store.Save(&Session{ID: "test"}); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	if err := store.Delete("test"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
}

// countingSessionStore 记录 Save 次数，用于验证批量落库。
type countingSessionStore struct {
	saved map[string]*Session
	saves int
}

func (c *countingSessionStore) Load(id string) (*Session, error) { return c.saved[id], nil }

func (c *countingSessionStore) Save(s *Session) error {
	c.saves++
	if c.saved == nil {
		c.saved = map[string]*Session{}
	}
	c.saved[s.ID] = s
	return nil
}

func (c *countingSessionStore) Delete(id string) error { delete(c.saved, id); return nil }

// TestAppendMessageNoPersistBatchesWrites 冻结：回合内的多条中间消息
// 追加不逐条写库，回合结束时一次 SaveSession 合并为单次整记录 upsert，
// 消除写放大。
func TestAppendMessageNoPersistBatchesWrites(t *testing.T) {
	store := &countingSessionStore{}
	sm := NewSessionManager(10, 5, time.Hour, store)
	s := sm.GetOrCreate("s", "u", "c")

	for i := range 5 {
		sm.AppendMessageNoPersist(s, protocol.Message{
			Role:       protocol.RoleTool,
			ToolCallID: fmt.Sprintf("c%d", i),
			Content:    "r",
		})
	}
	if store.saves != 0 {
		t.Fatalf("AppendMessageNoPersist 不应写库，saves = %d", store.saves)
	}
	if got := len(s.SnapshotMessages()); got != 5 {
		t.Fatalf("追加应生效，消息数 = %d", got)
	}

	sm.SaveSession(s)
	if store.saves != 1 {
		t.Fatalf("回合末一次 SaveSession 应只写一次库，saves = %d", store.saves)
	}

	// AppendMessage 仍保持"追加即持久化"的语义（用户消息等跨回合即时落库）。
	sm.AppendMessage(s, protocol.Message{Role: protocol.RoleUser, Content: "hi"})
	if store.saves != 2 {
		t.Fatalf("AppendMessage 应即时持久化，saves = %d", store.saves)
	}
}
