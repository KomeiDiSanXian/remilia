package engine

// command_index_semantics_test.go — 命令索引的匹配粒度契约
//
// 命令索引按消息的「首个空白分隔 token」精确匹配，这是 O(1) 分发的前提。
// 它与 context.OnCommand 规则的前缀语义不同：注册 "/he" 时 "/hello"
// 不会命中它（否则两条命令会互相抢流量）。

import (
	"sync/atomic"
	"testing"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
)

// TestEngine_OnCommandMatchesWholeCommandWord 记录 OnCommand 的匹配粒度：
// 精确匹配完整命令词，短命令不会抢占长命令的流量。
func TestEngine_OnCommandMatchesWholeCommandWord(t *testing.T) {
	eng := newEngineForTest(t)
	et := string(platform.EventKindPrivateMessage)

	var shortHits, longHits atomic.Int32
	short := eng.OnCommand(et, "/he")
	short.Handle(func(*context.Context) error {
		shortHits.Add(1)
		return nil
	})
	long := eng.OnCommand(et, "/hello")
	long.Handle(func(*context.Context) error {
		longHits.Add(1)
		return nil
	})

	process := func(content string) {
		eng.ProcessEvent(context.NewContextFromEvent(
			newTestPlatformEventWithContent(platform.EventKindPrivateMessage, content), nil))
	}

	process("/hello")
	assert.Equal(t, int32(1), longHits.Load(), "长命令应命中自己")
	assert.Zero(t, shortHits.Load(), "命令索引精确匹配：短命令不得抢占长命令的流量")

	process("/he")
	assert.Equal(t, int32(1), shortHits.Load(), "短命令命中自己")
	assert.Equal(t, int32(1), longHits.Load())
}

// TestEngine_OnPrefixRuleKeepsPrefixSemantics 验证需要前缀语义时应使用
// 常规索引规则（On + OnPrefix），其行为不受命令索引影响。
func TestEngine_OnPrefixRuleKeepsPrefixSemantics(t *testing.T) {
	eng := newEngineForTest(t)
	et := string(platform.EventKindPrivateMessage)

	var prefixHits atomic.Int32
	pm := eng.On(et, context.OnPrefix("/he"))
	pm.Handle(func(*context.Context) error {
		prefixHits.Add(1)
		return nil
	})

	eng.ProcessEvent(context.NewContextFromEvent(
		newTestPlatformEventWithContent(platform.EventKindPrivateMessage, "/hello"), nil))
	assert.Equal(t, int32(1), prefixHits.Load(), "OnPrefix 前缀语义照常生效")
}

// TestEngine_OnCommandPrefixConflictWarned 验证注册互为前缀的命令时不 panic
// （并走通两条路由，语义以上两条测试为准）。
func TestEngine_OnCommandPrefixConflictWarned(t *testing.T) {
	eng := newEngineForTest(t)
	et := string(platform.EventKindPrivateMessage)

	m1 := eng.OnCommand(et, "/x")
	m1.Handle(func(*context.Context) error { return nil })
	m2 := eng.OnCommand(et, "/xy")
	m2.Handle(func(*context.Context) error { return nil })

	assert.Equal(t, 2, eng.GetMatcherCount())
}
