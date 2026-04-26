package engine

// alias_registrar_test.go — aliasRegistrar 钩子及自动别名注册测试
//
// 测试范围：
//   - injectBaseAliasRegistrar：引擎级别的基础别名注册
//   - SetAliasRegistrar / Handle：触发时机与幂等性
//   - 别名 Matcher 继承 Group / Source
//   - 重复别名（commandIndex 冲突）跳过
//   - extraRules 不为空时基础版本跳过

import (
	"sync/atomic"
	"testing"

	"github.com/KomeiDiSanXian/remilia/command"
	ctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHandle_TriggersAliasRegistrar 验证 Handle() 调用时别名回调被触发。
func TestHandle_TriggersAliasRegistrar(t *testing.T) {
	var called int32
	eng := newEngineForTest(t)
	m := eng.OnCommand(string(platform.EventKindPrivateMessage), "/ping")
	m.SetAliasRegistrar(func(def *command.Definition, h ctx.Handler) {
		atomic.AddInt32(&called, 1)
	})
	m.SetDefinition(&command.Definition{Name: "ping", Aliases: []string{"p"}})
	m.Handle(func(c *ctx.Context) error { return nil })

	assert.EqualValues(t, 1, atomic.LoadInt32(&called), "aliasRegistrar should be called exactly once")
}

// TestHandle_AliasRegistrarCalledOnce 验证多次调用 Handle() 时回调只触发一次。
func TestHandle_AliasRegistrarCalledOnce(t *testing.T) {
	var called int32
	eng := newEngineForTest(t)
	m := eng.OnCommand(string(platform.EventKindPrivateMessage), "/ping2")
	m.SetAliasRegistrar(func(def *command.Definition, h ctx.Handler) {
		atomic.AddInt32(&called, 1)
	})
	m.SetDefinition(&command.Definition{Name: "ping2", Aliases: []string{"p2"}})

	handler := func(c *ctx.Context) error { return nil }
	m.Handle(handler)
	m.Handle(handler) // 第二次调用不应再触发
	m.Handle(handler) // 第三次

	assert.EqualValues(t, 1, atomic.LoadInt32(&called), "aliasRegistrar must fire only once regardless of Handle() call count")
}

// TestHandle_NoAliases_RegistrarNotCalled 验证无别名时回调不触发。
func TestHandle_NoAliases_RegistrarNotCalled(t *testing.T) {
	var called int32
	eng := newEngineForTest(t)
	m := eng.OnCommand(string(platform.EventKindPrivateMessage), "/noalias")
	m.SetAliasRegistrar(func(def *command.Definition, h ctx.Handler) {
		atomic.AddInt32(&called, 1)
	})
	m.SetDefinition(&command.Definition{Name: "noalias"}) // 无 Aliases
	m.Handle(func(c *ctx.Context) error { return nil })

	assert.EqualValues(t, 0, atomic.LoadInt32(&called), "aliasRegistrar should NOT fire when Aliases is empty")
}

// getFirstMatcherForCmd 从 commandIndex 中取出指定命令词的第一个 Matcher（辅助函数）。
func getFirstMatcherForCmd(s *state, cmd string) (*Matcher, bool) {
	perType, ok := s.commandIndex[cmd]
	if !ok {
		return nil, false
	}
	for _, matchers := range perType {
		if len(matchers) > 0 {
			return matchers[0], true
		}
	}
	return nil, false
}

// TestInjectBaseAliasRegistrar_RegistersAliasMatchers 验证基础别名注册会在引擎内创建别名 Matcher。
func TestInjectBaseAliasRegistrar_RegistersAliasMatchers(t *testing.T) {
	eng := newEngineForTest(t)
	primary := eng.OnCommand(string(platform.EventKindPrivateMessage), "/hello")
	primary.SetGroup("mygroup")
	primary.SetSource("plugin:myplugin")
	primary.SetDefinition(&command.Definition{
		Name:    "hello",
		Aliases: []string{"hi", "hey"},
	})
	primary.Handle(func(c *ctx.Context) error { return nil })

	// 验证别名路由已注册到 commandIndex
	s := eng.state.Load()
	_, hiFound := s.commandIndex["/hi"]
	_, heyFound := s.commandIndex["/hey"]
	assert.True(t, hiFound, "alias '/hi' should be in commandIndex")
	assert.True(t, heyFound, "alias '/hey' should be in commandIndex")
}

// TestInjectBaseAliasRegistrar_AliasInheritsGroupAndSource 验证别名 Matcher 继承主命令的 Group/Source。
func TestInjectBaseAliasRegistrar_AliasInheritsGroupAndSource(t *testing.T) {
	eng := newEngineForTest(t)
	primary := eng.OnCommand(string(platform.EventKindPrivateMessage), "/greet")
	primary.SetGroup("greetGroup")
	primary.SetSource("plugin:greetPlugin")
	primary.SetDefinition(&command.Definition{
		Name:    "greet",
		Aliases: []string{"gr"},
	})
	primary.Handle(func(c *ctx.Context) error { return nil })

	s := eng.state.Load()
	aliasMatcher, found := getFirstMatcherForCmd(s, "/gr")
	require.True(t, found, "alias '/gr' should be registered")
	assert.Equal(t, "greetGroup", aliasMatcher.GetGroup(), "alias matcher should inherit group")
	assert.Equal(t, "plugin:greetPlugin", aliasMatcher.GetSource(), "alias matcher should inherit source")
}

// TestInjectBaseAliasRegistrar_SkipsDuplicateAlias 验证已存在于 commandIndex 的别名被跳过。
func TestInjectBaseAliasRegistrar_SkipsDuplicateAlias(t *testing.T) {
	eng := newEngineForTest(t)

	// 先注册 /dup
	eng.OnCommand(string(platform.EventKindPrivateMessage), "/dup").
		Handle(func(c *ctx.Context) error { return nil })

	// 再注册 /original，其别名 dup 已被占用（commandIndex 中已有 "/dup"）
	primary := eng.OnCommand(string(platform.EventKindPrivateMessage), "/original")
	primary.SetDefinition(&command.Definition{
		Name:    "original",
		Aliases: []string{"dup"},
	})
	primary.Handle(func(c *ctx.Context) error { return nil })

	// 验证 /dup 仍然存在（未被删除）且 /original 的别名注册被跳过（不新增第二条记录）
	s := eng.state.Load()
	dupMatchers, ok := s.commandIndex["/dup"]
	require.True(t, ok, "/dup must still be in commandIndex")
	totalDup := 0
	for _, ms := range dupMatchers {
		totalDup += len(ms)
	}
	assert.Equal(t, 1, totalDup, "there should still be exactly 1 matcher for '/dup' (alias skipped)")
}

// TestInjectBaseAliasRegistrar_SkipsWhenExtraRules 验证携带 extraRules 时基础别名注册跳过。
func TestInjectBaseAliasRegistrar_SkipsWhenExtraRules(t *testing.T) {
	eng := newEngineForTest(t)
	extraRule := func(c *ctx.Context) bool { return true }
	primary := eng.OnCommand(string(platform.EventKindPrivateMessage), "/secure", extraRule)
	primary.SetDefinition(&command.Definition{
		Name:    "secure",
		Aliases: []string{"sec"},
	})
	primary.Handle(func(c *ctx.Context) error { return nil })

	// 有 extraRules 时，基础别名注册应该跳过，避免别名缺少必要的规则
	s := eng.state.Load()
	_, secFound := s.commandIndex["/sec"]
	assert.False(t, secFound,
		"base alias registrar should skip when extraRules are present (alias would lack required rules)")
}

// TestSetAliasRegistrar_NilDefinition 验证 def 为 nil 时不 panic。
func TestSetAliasRegistrar_NilDefinition(t *testing.T) {
	eng := newEngineForTest(t)
	m := eng.OnCommand(string(platform.EventKindPrivateMessage), "/nildef")
	m.SetAliasRegistrar(func(def *command.Definition, h ctx.Handler) {
		// 不应被调用（definition 为 nil 时）
		t.Error("registrar should not be called when definition is nil")
	})
	// 不调用 SetDefinition，保持 definition 为含空别名的结构
	// OnCommand 内部已设置了 definition.Name，但没有 Aliases，所以回调不触发
	assert.NotPanics(t, func() {
		m.Handle(func(c *ctx.Context) error { return nil })
	})
}
