package engine

// matcher_group_ops_test.go — 分组操作与删除操作对临时 matcher 的覆盖
//
// 回归背景：临时 matcher 只在 TempManager 里，不进 COW state 的
// matchers/groupIndex。RemoveGroup / DisableGroup / EnableGroup /
// DeleteMatchers / DeleteAllMatchers 此前只处理 state，导致：
//   - 插件卸载后其临时会话 matcher 仍在继续路由；
//   - 被移除的 matcher 不标记 deleted，外部持有者误以为仍存活；
//   - 引擎级别名 matcher 只调 SetGroup（不更新 groupIndex），永久漏删。

import (
	"sync/atomic"
	"testing"

	"github.com/KomeiDiSanXian/remilia/command"
	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEngine_RemoveGroupCoversTempMatchers 验证 RemoveGroup 会摘除
// TempManager 中的同组临时 matcher，并把被移除的 matcher 标记 deleted。
func TestEngine_RemoveGroupCoversTempMatchers(t *testing.T) {
	eng := newEngineForTest(t)
	et := string(platform.EventKindPrivateMessage)

	var hits atomic.Int32
	temp := eng.OnTemp(et)
	temp.Handle(func(*context.Context) error {
		hits.Add(1)
		return nil
	})
	temp.SetTempWithMaxUse(100)
	eng.SetMatcherGroup(temp, "plug", "plugin:plug")

	persistent := eng.On(et)
	persistent.Handle(func(*context.Context) error {
		hits.Add(1)
		return nil
	})
	eng.SetMatcherGroup(persistent, "plug", "plugin:plug")

	require.Equal(t, 1, eng.GetTempMatcherCount())

	eng.RemoveGroup("plug")

	assert.Equal(t, 0, eng.GetTempMatcherCount(), "RemoveGroup 必须清掉同组临时 matcher")
	assert.True(t, temp.IsDeleted(), "被移除的临时 matcher 必须标记 deleted")
	assert.True(t, persistent.IsDeleted(), "被移除的常驻 matcher 必须标记 deleted")

	eng.ProcessEvent(context.NewContextFromEvent(
		newTestPlatformEventWithContent(platform.EventKindPrivateMessage, "hi"), nil))
	assert.Zero(t, hits.Load(), "被移除分组的 matcher 不得再处理事件")
}

// TestEngine_DeleteMatchersMarksDeleted 验证批量删除会标记 deleted，
// 并正确处理其中的临时 matcher。
func TestEngine_DeleteMatchersMarksDeleted(t *testing.T) {
	eng := newEngineForTest(t)
	et := string(platform.EventKindPrivateMessage)

	persistent := eng.On(et)
	persistent.Handle(func(*context.Context) error { return nil })
	temp := eng.OnTemp(et)
	temp.Handle(func(*context.Context) error { return nil })
	temp.SetTempWithMaxUse(10)

	eng.DeleteMatchers([]*Matcher{persistent, temp})

	assert.True(t, persistent.IsDeleted())
	assert.True(t, temp.IsDeleted())
	assert.Equal(t, 0, eng.GetMatcherCount())
	assert.Equal(t, 0, eng.GetTempMatcherCount(), "临时 matcher 必须从 TempManager 摘除")
}

// TestEngine_DeleteAllMatchersCoversTemp 验证 DeleteAllMatchers 覆盖临时 matcher。
func TestEngine_DeleteAllMatchersCoversTemp(t *testing.T) {
	eng := newEngineForTest(t)
	et := string(platform.EventKindPrivateMessage)

	temp := eng.OnTemp(et)
	temp.Handle(func(*context.Context) error { return nil })
	temp.SetTempWithMaxUse(10)
	persistent := eng.On(et)
	persistent.Handle(func(*context.Context) error { return nil })

	eng.DeleteAllMatchers()

	assert.Equal(t, 0, eng.GetMatcherCount())
	assert.Equal(t, 0, eng.GetTempMatcherCount())
	assert.True(t, temp.IsDeleted())
	assert.True(t, persistent.IsDeleted())
}

// TestEngine_AliasMatcherEntersGroupIndex 验证引擎级别名自动注册使用
// SetMatcherGroup，别名 matcher 可被 RemoveGroup 一并清理。
func TestEngine_AliasMatcherEntersGroupIndex(t *testing.T) {
	eng := newEngineForTest(t)
	et := string(platform.EventKindPrivateMessage)

	primary := eng.OnCommand(et, "/greet")
	eng.SetMatcherGroup(primary, "plug", "plugin:plug")
	primary.SetDefinition(&command.Definition{
		Name:    "greet",
		Aliases: []string{"hi"},
	})
	primary.Handle(func(*context.Context) error { return nil })

	require.NotNil(t, eng.FindCommand("/hi"), "别名应已注册")

	var aliasCount int
	for _, g := range eng.ListGroups() {
		if g.Name == "plug" {
			aliasCount = g.Count
		}
	}
	assert.Equal(t, 2, aliasCount, "别名 matcher 必须进入主命令的 groupIndex")

	eng.RemoveGroup("plug")
	assert.Equal(t, 0, eng.GetMatcherCount(), "RemoveGroup 必须同时清掉别名 matcher")
	assert.Empty(t, eng.internals.tempManager.Get(et))
}
