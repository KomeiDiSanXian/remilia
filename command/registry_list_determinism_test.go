package command

// registry_list_determinism_test.go — 列表确定性、隐藏过滤与副本语义的回归测试
//
// 回归背景：
//   - List() 直接返回编译快照的内部切片：调用方修改会污染注册表，且不
//     过滤 Hidden（自动生成的别名 Matcher 会出现在用户列表中）；
//   - Complete/List 的顺序来自 trie/map 遍历，同优先级命令在各次 recompile
//     之间顺序抖动，调用方（补全、/help 分页）无法获得稳定输出。

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistry_ListFiltersHiddenAndReturnsCopy(t *testing.T) {
	reg := NewCommandRegistry()
	require.NoError(t, reg.RegisterWithOptions(&Definition{Name: "/visible"}, RegisterOptions{Priority: 1}))
	require.NoError(t, reg.RegisterWithOptions(&Definition{Name: "/alias", Hidden: true}, RegisterOptions{}))

	list := reg.List()
	require.Len(t, list, 1)
	assert.Equal(t, "/visible", list[0].Name)

	// 副本语义：修改返回值不影响注册表
	list[0] = nil
	assert.Len(t, reg.List(), 1)

	// ListAll 保留隐藏项（内部诊断用）
	assert.Len(t, reg.ListAll(), 2)
}

func TestRegistry_OrderIsDeterministic(t *testing.T) {
	reg := NewCommandRegistry()

	// 同优先级：按名称升序，且多次 recompile 后不变
	for _, name := range []string{"/charlie", "/alpha", "/bravo"} {
		require.NoError(t, reg.RegisterWithOptions(&Definition{Name: name}, RegisterOptions{Priority: 5}))
	}

	first := names(reg.List())
	assert.Equal(t, []string{"/alpha", "/bravo", "/charlie"}, first)

	// 触发多次 recompile（注册/更新/注销）
	require.NoError(t, reg.RegisterWithOptions(&Definition{Name: "/delta"}, RegisterOptions{Priority: 5}))
	require.NoError(t, reg.Upsert(&Definition{Name: "/alpha", Description: "v2"}, RegisterOptions{}))
	require.NoError(t, reg.Unregister("/delta"))

	assert.Equal(t, first, names(reg.List()), "同优先级顺序必须稳定")

	// 优先级降序优先
	require.NoError(t, reg.RegisterWithOptions(&Definition{Name: "/zeta"}, RegisterOptions{Priority: 100}))
	assert.Equal(t, "/zeta", reg.List()[0].Name)

	// Complete 同样过滤隐藏项且顺序稳定
	require.NoError(t, reg.RegisterWithOptions(&Definition{Name: "/hidden", Hidden: true}, RegisterOptions{}))
	assert.Equal(t, []string{"/zeta", "/alpha", "/bravo", "/charlie"}, names(reg.Complete("/")), "Complete 同 List：过滤隐藏项、顺序稳定")
}

func names(metas []*Meta) []string {
	out := make([]string, 0, len(metas))
	for _, m := range metas {
		out = append(out, m.Name)
	}
	return out
}
