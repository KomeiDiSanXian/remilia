package dlq

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── platform.Event stub ─────────────────────────────────────────────────────

type testPlatformEvent struct {
	platformID string
	rawType    string
	content    string
	kind       platform.EventKind
	chatID     string
	senderID   string
}

func (e *testPlatformEvent) Platform() string                          { return e.platformID }
func (e *testPlatformEvent) Kind() platform.EventKind                  { return e.kind }
func (e *testPlatformEvent) RawType() string                           { return e.rawType }
func (e *testPlatformEvent) Content() string                           { return e.content }

func (e *testPlatformEvent) Segments() []platform.Segment {
	if e.content == "" {
		return nil
	}
	return []platform.Segment{{Type: platform.SegmentText, Text: e.content}}
}
func (e *testPlatformEvent) Chat() platform.ChatInfo                   { return platform.ChatInfo{ID: e.chatID} }
func (e *testPlatformEvent) Sender() platform.UserInfo                 { return platform.UserInfo{ID: e.senderID} }
func (e *testPlatformEvent) Timestamp() time.Time                      { return time.Unix(1700000000, 0) }
func (e *testPlatformEvent) ID() string                                { return "" }
func (e *testPlatformEvent) RawPayload() any                           { return nil }
func (e *testPlatformEvent) Attachments() []platform.Attachment { return nil }

func makeTestPlatformEvent(platformID, rawType string) platform.Event {
	return &testPlatformEvent{
		platformID: platformID,
		rawType:    rawType,
		content:    "hello",
		kind:       platform.EventKindPrivateMessage,
		chatID:     "chat-001",
		senderID:   "sender-001",
	}
}

// ─── PlatformFileConsumer 测试 ────────────────────────────────────────────────

// TestPlatformFileConsumer_Consume_WritesJSONLine 验证写入文件并可反序列化
func TestPlatformFileConsumer_Consume_WritesJSONLine(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "platform_dlq.jsonl")

	consumer := PlatformFileConsumer{Path: path}
	event := makeTestPlatformEvent("qq", "message_create")

	item := Item[platform.Event]{
		Data:    event,
		Err:     assert.AnError,
		Attempt: 2,
		Source:  "test-handler",
	}

	consumer.Consume(item)

	content, err := os.ReadFile(path)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	require.Len(t, lines, 1)

	var rec PlatformDeadLetterRecord
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &rec))

	require.NotNil(t, rec.Event)
	assert.Equal(t, "qq", rec.Event.Platform)
	assert.Equal(t, "message_create", rec.Event.RawType)
	assert.Equal(t, "chat-001", rec.Event.ChatID)
	assert.Equal(t, "sender-001", rec.Event.SenderID)
	assert.NotZero(t, rec.Event.TimestampUnix)

	assert.NotEmpty(t, rec.Error.Message)
	assert.Equal(t, "test-handler", rec.Error.Source)
	assert.Equal(t, 2, rec.Error.Attempt)
}

// TestPlatformFileConsumer_Consume_AppendMultiple 验证多次写入追加而非覆盖
func TestPlatformFileConsumer_Consume_AppendMultiple(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "multi.jsonl")
	consumer := PlatformFileConsumer{Path: path}

	for i := range 3 {
		consumer.Consume(Item[platform.Event]{
			Data:    makeTestPlatformEvent("discord", "message"),
			Attempt: i + 1,
		})
	}

	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	lineCount := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lineCount++
		var rec PlatformDeadLetterRecord
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &rec))
		require.NotNil(t, rec.Event)
		assert.Equal(t, "discord", rec.Event.Platform)
	}
	assert.Equal(t, 3, lineCount)
}

// TestPlatformFileConsumer_Consume_NilEvent 验证 nil event 不 panic
func TestPlatformFileConsumer_Consume_NilEvent(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "nil.jsonl")
	consumer := PlatformFileConsumer{Path: path}

	assert.NotPanics(t, func() {
		consumer.Consume(Item[platform.Event]{Data: nil, Err: assert.AnError, Attempt: 1})
	})

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	var rec PlatformDeadLetterRecord
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(string(content))), &rec))
	assert.Nil(t, rec.Event)
}

// ─── PlatformWebhookConsumer 测试 ─────────────────────────────────────────────

// TestPlatformWebhookConsumer_Consume_Success 验证 2xx 时成功发送
func TestPlatformWebhookConsumer_Consume_Success(t *testing.T) {
	var received atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var body PlatformDeadLetterRecord
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.NotNil(t, body.Event)
		assert.Equal(t, "telegram", body.Event.Platform)

		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	consumer := PlatformWebhookConsumer{URL: srv.URL, Timeout: 2 * time.Second, MaxRetries: 0}
	consumer.Consume(Item[platform.Event]{
		Data:    makeTestPlatformEvent("telegram", "msg"),
		Attempt: 1,
	})

	assert.Equal(t, int32(1), received.Load())
}

// TestPlatformWebhookConsumer_Consume_RetryOnFailure 验证失败时按 MaxRetries 重试
func TestPlatformWebhookConsumer_Consume_RetryOnFailure(t *testing.T) {
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := callCount.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	consumer := PlatformWebhookConsumer{URL: srv.URL, Timeout: 2 * time.Second, MaxRetries: 3}
	consumer.Consume(Item[platform.Event]{
		Data:    makeTestPlatformEvent("wechat", "text"),
		Attempt: 1,
	})

	assert.GreaterOrEqual(t, callCount.Load(), int32(3))
}

// TestPlatformWebhookConsumer_Consume_AllRetriesFail 验证全部重试失败时不 panic
func TestPlatformWebhookConsumer_Consume_AllRetriesFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	consumer := PlatformWebhookConsumer{URL: srv.URL, Timeout: 1 * time.Second, MaxRetries: 1}
	assert.NotPanics(t, func() {
		consumer.Consume(Item[platform.Event]{
			Data:    makeTestPlatformEvent("qq", "fail"),
			Attempt: 1,
		})
	})
}

// ─── MarshalPlatformEventItem 测试 ───────────────────────────────────────────

// TestMarshalPlatformEventItem 验证序列化时字段填充正确
func TestMarshalPlatformEventItem(t *testing.T) {
	t.Run("all fields populated", func(t *testing.T) {
		event := makeTestPlatformEvent("qq", "c2c_message_create")
		item := Item[platform.Event]{
			Data:    event,
			Err:     assert.AnError,
			Attempt: 3,
			Source:  "engine",
		}

		data, err := MarshalPlatformEventItem(item)
		require.NoError(t, err)

		var rec PlatformDeadLetterRecord
		require.NoError(t, json.Unmarshal(data, &rec))

		require.NotNil(t, rec.Event)
		assert.Equal(t, "qq", rec.Event.Platform)
		assert.Equal(t, "c2c_message_create", rec.Event.RawType)
		assert.Equal(t, "chat-001", rec.Event.ChatID)
		assert.Equal(t, "sender-001", rec.Event.SenderID)
		assert.NotZero(t, rec.Event.TimestampUnix)

		assert.NotEmpty(t, rec.Error.Message)
		assert.Equal(t, "engine", rec.Error.Source)
		assert.Equal(t, 3, rec.Error.Attempt)
	})

	t.Run("nil event data", func(t *testing.T) {
		item := Item[platform.Event]{Data: nil, Err: assert.AnError, Attempt: 1}
		data, err := MarshalPlatformEventItem(item)
		require.NoError(t, err)

		var rec PlatformDeadLetterRecord
		require.NoError(t, json.Unmarshal(data, &rec))
		assert.Nil(t, rec.Event)
		assert.NotEmpty(t, rec.Error.Message)
	})

	t.Run("nil error", func(t *testing.T) {
		item := Item[platform.Event]{Data: makeTestPlatformEvent("discord", "msg"), Err: nil}
		data, err := MarshalPlatformEventItem(item)
		require.NoError(t, err)

		var rec PlatformDeadLetterRecord
		require.NoError(t, json.Unmarshal(data, &rec))
		assert.Empty(t, rec.Error.Message)
	})
}

// BenchmarkMarshalPlatformEventItem 基准测试平台事件序列化
func BenchmarkMarshalPlatformEventItem(b *testing.B) {
	item := Item[platform.Event]{
		Data:    makeTestPlatformEvent("qq", "c2c_message_create"),
		Err:     assert.AnError,
		Attempt: 3,
		Source:  "benchmark",
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = MarshalPlatformEventItem(item)
	}
}
