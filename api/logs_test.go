package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func appendMessages(b *LogBuffer, msgs ...string) {
	for _, m := range msgs {
		b.Append(LogEntry{Time: "00:00:00.000", Level: "info", Message: m})
	}
}

func entryMessages(entries []LogEntry) []string {
	msgs := make([]string, len(entries))
	for i, e := range entries {
		msgs[i] = e.Message
	}
	return msgs
}

func TestLogBufferRecentReturnsNewest(t *testing.T) {
	b := NewLogBuffer(5)
	appendMessages(b, "a", "b", "c")
	assert.Equal(t, []string{"b", "c"}, entryMessages(b.Recent(2)))

	appendMessages(b, "d", "e", "f")
	assert.Equal(t, []string{"d", "e", "f"}, entryMessages(b.Recent(3)))
}

func TestLogBufferRecentEmpty(t *testing.T) {
	b := NewLogBuffer(4)
	assert.Empty(t, b.Recent(10))

	entries, seq := b.Since(0, 10)
	assert.Empty(t, entries)
	assert.Equal(t, uint64(0), seq)
}

// TestLogBufferSinceAdvancesAfterWrap 是 SSE 停更缺陷的回归测试：
// 缓冲区写满后条目数恒定，游标必须按序号推进，新日志才能继续流出。
func TestLogBufferSinceAdvancesAfterWrap(t *testing.T) {
	b := NewLogBuffer(5)
	appendMessages(b, "a", "b", "c", "d", "e")

	entries, seq := b.Since(0, 50)
	require.Len(t, entries, 5)
	assert.Equal(t, []string{"a", "b", "c", "d", "e"}, entryMessages(entries))
	assert.Equal(t, uint64(5), seq)

	appendMessages(b, "f", "g", "h")
	entries, seq = b.Since(seq, 50)
	require.Len(t, entries, 3)
	assert.Equal(t, []string{"f", "g", "h"}, entryMessages(entries))
	assert.Equal(t, uint64(8), seq)

	// 游标落后超过缓冲区容量时，返回仍保留的最新条目并推进序号，而不是停更。
	entries, seq = b.Since(1, 50)
	require.Len(t, entries, 5)
	assert.Equal(t, []string{"d", "e", "f", "g", "h"}, entryMessages(entries))
	assert.Equal(t, uint64(8), seq)
}

func TestLogBufferSinceRespectsMax(t *testing.T) {
	b := NewLogBuffer(10)
	appendMessages(b, "a", "b", "c", "d", "e")

	entries, seq := b.Since(0, 2)
	require.Len(t, entries, 2)
	assert.Equal(t, []string{"d", "e"}, entryMessages(entries))
	assert.Equal(t, uint64(5), seq)
}
