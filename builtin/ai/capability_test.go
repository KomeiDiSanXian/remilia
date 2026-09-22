package ai

import (
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/catalog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestToolCapabilitiesNilSafety 守住"类型化 nil 指针不得赋给端口"这一约束：
// 未启用的能力必须保持为 nil 端口，否则动作侧的 `caps.X == nil` 降级判定会失效。
func TestToolCapabilitiesNilSafety(t *testing.T) {
	bare := (&Plugin{}).toolCapabilities()
	assert.Nil(t, bare.Memory, "memory 未启用时端口必须为 nil")
	assert.Nil(t, bare.Todos, "todos 未启用时端口必须为 nil")
	assert.Nil(t, bare.Reminders, "reminders 未启用时端口必须为 nil")
	assert.Nil(t, bare.Scheduler, "scheduler 未启用时端口必须为 nil")

	// 空 Plugin 指针同样不得 panic，端口保持为零值。
	var nilPlugin *Plugin
	assert.Equal(t, catalog.Capabilities{}, nilPlugin.toolCapabilities())
}

// TestToolCapabilitiesComposition 校验组合根按启用状态装配端口。
func TestToolCapabilitiesComposition(t *testing.T) {
	dir := t.TempDir()
	mem, err := OpenMemoryStore(dir, 50, time.Minute)
	require.NoError(t, err)
	t.Cleanup(mem.Close)

	p := &Plugin{
		memory:    mem,
		todos:     newTodoManager(),
		reminders: newReminderManager(),
	}
	caps := p.toolCapabilities()

	require.NotNil(t, caps.Memory)
	require.NotNil(t, caps.Todos)
	require.NotNil(t, caps.Reminders)
	require.NotNil(t, caps.Scheduler)

	// 端口必须转发到同一份底层管理器，保证与直接调用管理器等价。
	caps.Todos.Add("c1", "写文档")
	require.Len(t, caps.Todos.List("c1"), 1)
	assert.Equal(t, "写文档", p.todos.list("c1")[0].Text)

	caps.Memory.Add("user", "u1", "用户喜欢冰美式")
	assert.Equal(t, 1, caps.Memory.Count("user", "u1"))
	assert.Len(t, p.memory.Facts(userScope("u1")), 1)

	// 群作用域映射到群记忆键，且取消删除同样命中同一作用域。
	caps.Memory.Add("group", "g1", "本群每周五团建")
	assert.Equal(t, 1, caps.Memory.Count("group", "g1"))
	assert.Len(t, p.memory.Facts(groupScope("g1")), 1)
	assert.True(t, caps.Memory.Remove("group", "g1", "本群每周五团建"))
	assert.Empty(t, p.memory.Facts(groupScope("g1")))
}
