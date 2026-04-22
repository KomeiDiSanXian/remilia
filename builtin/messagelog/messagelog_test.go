package messagelog

import (
	"fmt"
	"testing"
	"time"
)

func TestRecord_QueryGroup(t *testing.T) {
	l := New(10)
	now := time.Now()
	for i := range 5 {
		l.Record(Message{
			GroupID:   "g1",
			UserID:    "u1",
			Content:   fmt.Sprintf("消息%d", i),
			Timestamp: now.Add(time.Duration(i) * time.Second),
		})
	}
	msgs := l.QueryGroup("g1", 3)
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	// 应该是最新的3条（消息2, 消息3, 消息4）
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
	l.Record(Message{GroupID: "g1", UserID: "u1", Content: "hello world", Timestamp: now})
	l.Record(Message{GroupID: "g2", UserID: "u1", Content: "foo bar", Timestamp: now.Add(time.Second)})
	l.Record(Message{GroupID: "g1", UserID: "u2", Content: "baz", Timestamp: now.Add(2 * time.Second)})

	msgs := l.QueryUser("u1", 10)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages for u1, got %d", len(msgs))
	}
}

func TestRing_Overflow(t *testing.T) {
	l := New(5) // 容量仅5
	now := time.Now()
	for i := range 8 {
		l.Record(Message{GroupID: "g1", UserID: "u1", Content: fmt.Sprintf("msg%d", i), Timestamp: now.Add(time.Duration(i) * time.Second)})
	}
	msgs := l.QueryGroup("g1", 10)
	// 只保留最新5条
	if len(msgs) != 5 {
		t.Fatalf("expected 5 messages (ring overflow), got %d", len(msgs))
	}
	// 最旧的应该是 msg3
	if msgs[0].Content != "msg3" {
		t.Errorf("expected msg3, got %s", msgs[0].Content)
	}
	// 最新的应该是 msg7
	if msgs[4].Content != "msg7" {
		t.Errorf("expected msg7, got %s", msgs[4].Content)
	}
}

func TestWordFreq(t *testing.T) {
	l := New(100)
	now := time.Now()
	l.Record(Message{GroupID: "g1", Content: "你好 世界 你好", Timestamp: now})
	l.Record(Message{GroupID: "g1", Content: "世界 再见", Timestamp: now.Add(time.Second)})

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
	l.Record(Message{GroupID: "g1", UserID: "u1", Content: "old message", Timestamp: now.Add(-2 * time.Hour)})
	l.Record(Message{GroupID: "g1", UserID: "u1", Content: "new message", Timestamp: now})

	l.Clear(now.Add(-time.Hour)) // 清理1小时前的消息

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
	l.Record(Message{GroupID: "g1", Content: "old", Timestamp: now.Add(-2 * time.Hour)})
	l.Clear(now.Add(-time.Hour))
	if l.GroupCount() != 0 {
		t.Errorf("expected group to be removed after all messages cleared")
	}
}

func TestGlobalFunctions(t *testing.T) {
	// 使用独立实例避免污染全局状态
	orig := defaultLogger
	defaultLogger = New(100)
	defer func() { defaultLogger = orig }()

	now := time.Now()
	Record(Message{GroupID: "g1", UserID: "u1", Content: "test message", Timestamp: now})
	msgs := QueryGroup("g1", 10)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
}

func TestGroupMessageCount(t *testing.T) {
	l := New(100)
	now := time.Now()
	for i := range 7 {
		l.Record(Message{GroupID: "g1", Content: fmt.Sprintf("msg%d", i), Timestamp: now})
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
		{"a bc", []string{"bc"}}, // "a" 长度<2 被过滤
		{"!!! ???", []string{}},  // 全标点被过滤
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
