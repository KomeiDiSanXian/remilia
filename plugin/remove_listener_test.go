package plugin

// remove_listener_test.go — RemoveListener 对不可比较监听器的回归测试
//
// 回归背景：RemoveListener 用 `l != listener` 比较接口值。动态类型不可比较
// （例如带 map 字段的结构体值）时该比较直接 panic——移除一个监听器让整个
// 管理操作崩溃。

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// nonComparableListener 的动态类型是「含 map 的结构体值」→ 不可比较。
type nonComparableListener struct {
	m map[string]int
}

func (nonComparableListener) OnPluginLoaded(string)               {}
func (nonComparableListener) OnPluginUnloaded(string)             {}
func (nonComparableListener) OnPluginReloaded(string)             {}
func (nonComparableListener) OnPluginError(string, string, error) {}

// pointerListener 是指针类型 → 可比较（按地址判定同一性）。
type pointerListener struct {
	calls int
}

func (p *pointerListener) OnPluginLoaded(string)               { p.calls++ }
func (p *pointerListener) OnPluginUnloaded(string)             {}
func (p *pointerListener) OnPluginReloaded(string)             {}
func (p *pointerListener) OnPluginError(string, string, error) {}

func TestRemoveListener_NonComparableDoesNotPanic(t *testing.T) {
	pm := NewManager(nil)
	lc := pm.lifecycle

	nonComparable := nonComparableListener{m: map[string]int{"a": 1}}
	lc.AddListener(nonComparable)
	require.Len(t, lc.listeners, 1)

	// 不可比较的动态类型：不得 panic；无法判定同一性时保留监听器
	require.NotPanics(t, func() { lc.RemoveListener(nonComparable) })
	assert.Len(t, lc.listeners, 1, "无法判定同一性的监听器应保留")

	// 指针监听器正常移除
	p := &pointerListener{}
	lc.AddListener(p)
	require.Len(t, lc.listeners, 2)

	require.NotPanics(t, func() { lc.RemoveListener(p) })
	assert.Len(t, lc.listeners, 1)

	// 未添加过的指针：不影响已有监听器
	lc.RemoveListener(&pointerListener{})
	assert.Len(t, lc.listeners, 1)
}

func TestRemoveListener_SameTypeDifferentPointerKeepsBoth(t *testing.T) {
	pm := NewManager(nil)
	lc := pm.lifecycle

	a := &pointerListener{}
	b := &pointerListener{}
	lc.AddListener(a)
	lc.AddListener(b)
	require.Len(t, lc.listeners, 2)

	lc.RemoveListener(b)
	assert.Len(t, lc.listeners, 1)

	// 通知仍到达 a
	pm.notifyLoaded("x")
	assert.Equal(t, 1, a.calls)
}
