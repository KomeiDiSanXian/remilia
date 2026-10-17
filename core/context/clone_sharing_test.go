package context

// clone_sharing_test.go — Clone 的复制语义契约测试
//
// 背景：copyExtensions 的注释曾自称"深拷贝"，实现只复制 string 键的 map
// 容器；类型键条目与嵌套值在两个 Context 间共享。这里把实际契约固定下来
// （文档已同步改为准确描述），避免后续改动无意间扩大/缩小共享范围。

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type sharedTypedThing struct {
	M map[string]int
}

func TestClone_ExtensionSharingContract(t *testing.T) {
	c := newTestCtx()
	c.Set("stage", "first")

	thing := &sharedTypedThing{M: map[string]int{"n": 1}}
	typ := reflect.TypeOf(thing)
	c.Ext().Set(typ, thing)

	cloned := c.Clone()

	// 字符串键存储：整表拷贝，两侧独立写
	cloned.Set("stage", "second")
	assert.Equal(t, "first", c.GetString("stage"))
	assert.Equal(t, "second", cloned.GetString("stage"))

	// 类型键存储：复制引用（浅拷贝），值本身共享
	got, ok := cloned.Ext().Get(typ)
	require.True(t, ok)
	assert.Same(t, thing, got.(*sharedTypedThing), "类型键存储按引用共享")

	got.(*sharedTypedThing).M["n"] = 2
	assert.Equal(t, 2, thing.M["n"], "改写共享值在原型上可见（契约如此）")
}
