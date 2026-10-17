package command

// registry_alias_conflict_test.go — 别名冲突与 Upsert 错误可见性的回归测试
//
// 回归背景：
//   - Upsert 没有返回值，别名冲突与非法正则模式被静默忽略/直接 panic，
//     调用方无法感知"命令注册了但别名失效/模式被忽略"；
//   - 别名校验只查"别名是否被其它命令占用"，不查"别名是否遮蔽已存在的
//     命令名"——Lookup 先命中命令名，这类别名永远不会生效。

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistry_UpsertReportsAliasConflict(t *testing.T) {
	reg := NewCommandRegistry()

	require.NoError(t, reg.RegisterWithOptions(&Definition{Name: "/a", Aliases: []string{"/x"}}, RegisterOptions{}))

	// 别名已被 /a 占用：注册失败且不写入任何数据
	err := reg.Upsert(&Definition{Name: "/b", Aliases: []string{"/x"}}, RegisterOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "/x")

	if meta, ok := reg.Lookup("/b"); ok {
		t.Fatalf("冲突时不应写入命令，却查到 %s", meta.Name)
	}
	meta, ok := reg.Lookup("/x")
	require.True(t, ok)
	assert.Equal(t, "/a", meta.Name, "原别名归属不得被覆盖")
}

func TestRegistry_UpsertReportsAliasShadowingCommandName(t *testing.T) {
	reg := NewCommandRegistry()
	require.NoError(t, reg.RegisterWithOptions(&Definition{Name: "/echo"}, RegisterOptions{}))

	// 别名遮蔽已存在的命令名：Lookup 会命中命令，该别名永不生效
	err := reg.Upsert(&Definition{Name: "/e", Aliases: []string{"/echo"}}, RegisterOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "existing command name")
	if meta, ok := reg.Lookup("/e"); ok {
		t.Fatalf("冲突时不应写入命令，却查到 %s", meta.Name)
	}
}

func TestRegistry_UpsertKeepsOwnAliasesOnUpdate(t *testing.T) {
	reg := NewCommandRegistry()
	require.NoError(t, reg.RegisterWithOptions(&Definition{Name: "/cmd", Aliases: []string{"/c"}}, RegisterOptions{}))

	// 同名更新保留自身别名：不应误判为冲突
	require.NoError(t, reg.Upsert(&Definition{Name: "/cmd", Aliases: []string{"/c"}, Description: "v2"}, RegisterOptions{}))

	meta, ok := reg.Lookup("/c")
	require.True(t, ok)
	assert.Equal(t, "/cmd", meta.Name)
	assert.Equal(t, "v2", meta.Description)
}

func TestRegistry_UpsertReportsInvalidPattern(t *testing.T) {
	reg := NewCommandRegistry()

	// 非法模式：命令仍注册（模式是可选字段），但错误必须可见
	err := reg.Upsert(&Definition{Name: "/bad"}, RegisterOptions{Pattern: "("})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid pattern")

	meta, ok := reg.Lookup("/bad")
	require.True(t, ok, "模式错误不应阻断命令注册")
	assert.Nil(t, meta.pattern)
}

func TestRegistry_RegisterRejectsDuplicateAliasInDefinition(t *testing.T) {
	reg := NewCommandRegistry()

	err := reg.RegisterWithOptions(&Definition{Name: "/dup", Aliases: []string{"/d", "/d"}}, RegisterOptions{})
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "duplicate alias"), err.Error())

	err = reg.RegisterWithOptions(&Definition{Name: "/self", Aliases: []string{"/self"}}, RegisterOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "equals the command name")
}

func TestRegistry_RegisterBatchReportsAliasShadowing(t *testing.T) {
	reg := NewCommandRegistry()

	errs := reg.RegisterBatch([]*Definition{
		{Name: "/first"},
		{Name: "/second", Aliases: []string{"/first"}},
	})
	require.Len(t, errs, 1)
	assert.Contains(t, errs["/second"].Error(), "existing command name")
	if meta, ok := reg.Lookup("/second"); ok {
		t.Fatalf("冲突时不应写入命令，却查到 %s", meta.Name)
	}
	if _, ok := reg.Lookup("/first"); !ok {
		t.Fatal("未冲突的命令应注册成功")
	}
}
