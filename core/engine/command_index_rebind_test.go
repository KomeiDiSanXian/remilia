package engine

// command_index_rebind_test.go — 命令索引在运行期改名后的重建
//
// 回归背景：BindCommand / SetDefinition 改命令名时只刷新命令元数据缓存，
// 不做索引重建，commandIndex 里旧键仍指向同一 matcher——改名后旧命令
// 继续触发，新命令反而打不中。

import (
	"sync/atomic"
	"testing"

	"github.com/KomeiDiSanXian/remilia/command"
	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
)

// TestEngine_BindCommandRemovesOldRoute 验证 BindCommand 改名后
// 旧命令不再触发（commandIndex 全量重建）。
func TestEngine_BindCommandRemovesOldRoute(t *testing.T) {
	eng := newEngineForTest(t)
	et := string(platform.EventKindPrivateMessage)

	var oldHits, newHits atomic.Int32
	m := eng.OnCommand(et, "/old")
	m.Handle(func(*context.Context) error {
		oldHits.Add(1)
		return nil
	})
	// 另建一个真正占用 /new 的命令，验证改名后的 matcher 不会再从旧键路由。
	m2 := eng.OnCommand(et, "/new")
	m2.Handle(func(*context.Context) error {
		newHits.Add(1)
		return nil
	})
	m.BindCommand("/new2")

	eng.ProcessEvent(context.NewContextFromEvent(
		newTestPlatformEventWithContent(platform.EventKindPrivateMessage, "/old"), nil))
	eng.ProcessEvent(context.NewContextFromEvent(
		newTestPlatformEventWithContent(platform.EventKindPrivateMessage, "/new"), nil))

	assert.Zero(t, oldHits.Load(), "改名后旧命令不得再触发")
	assert.Equal(t, int32(1), newHits.Load(), "新命令路由不受影响")
}

// TestEngine_SetDefinitionRenameReindexes 验证 SetDefinition 换名同样重建索引。
func TestEngine_SetDefinitionRenameReindexes(t *testing.T) {
	eng := newEngineForTest(t)
	et := string(platform.EventKindPrivateMessage)

	var hits atomic.Int32
	m := eng.OnCommand(et, "/alpha")
	m.Handle(func(*context.Context) error {
		hits.Add(1)
		return nil
	})

	m.SetDefinition(&command.Definition{Name: "beta"})

	eng.ProcessEvent(context.NewContextFromEvent(
		newTestPlatformEventWithContent(platform.EventKindPrivateMessage, "/alpha"), nil))
	assert.Zero(t, hits.Load(), "改名后旧命令名不得再命中")

	eng.ProcessEvent(context.NewContextFromEvent(
		newTestPlatformEventWithContent(platform.EventKindPrivateMessage, "/beta"), nil))
	assert.Equal(t, int32(1), hits.Load(), "新命令名应可路由")
}
