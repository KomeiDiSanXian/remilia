package catalog

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCapabilitiesInvocationRoundTrip 校验能力端口经 Invocation 传递后保持等价
// （只有装配侧注入、动作侧读取这一条路径），未注入时返回零值与 false。
func TestCapabilitiesInvocationRoundTrip(t *testing.T) {
	todos := &stubTodoAccess{}
	inv := testInvocation(Capabilities{Todos: todos})

	got, ok := CapabilitiesFrom(inv)
	require.True(t, ok)
	require.NotNil(t, got.Todos)
	got.Todos.Add("c1", "x")
	assert.Equal(t, []TodoItem{{ID: "T1", Text: "x"}}, todos.List("c1"))

	_, ok = CapabilitiesFrom(nil)
	assert.False(t, ok)
}

// stubTodoAccess 只记录待办，用于校验端口注入/读取路径本身。
type stubTodoAccess struct {
	items map[string][]TodoItem
	seq   map[string]int
}

func (s *stubTodoAccess) Add(chatID, text string) string {
	if s.items == nil {
		s.items = map[string][]TodoItem{}
		s.seq = map[string]int{}
	}
	s.seq[chatID]++
	id := "T" + string(rune('0'+s.seq[chatID]))
	s.items[chatID] = append(s.items[chatID], TodoItem{ID: id, Text: text})
	return id
}

func (s *stubTodoAccess) List(chatID string) []TodoItem { return s.items[chatID] }

func (s *stubTodoAccess) SetDone(string, string, bool) bool { return false }

func (s *stubTodoAccess) Remove(string, string) bool { return false }
