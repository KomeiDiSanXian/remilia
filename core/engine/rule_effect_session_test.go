package engine

// rule_effect_session_test.go — 引擎侧匹配会话边界的回归测试
//
// 回归背景：同一 Context 先被非引擎路径（FSM Event.Match、业务代码直接求值）
// 登记了延迟副作用，随后交给引擎匹配；此前的实现会让引擎里某个无关 matcher
// 的提交执行这些登记项（如错误地消耗冷却）。路由器把同一个 Context 既交给
// FSM 又交给引擎，正是这条路径的真实来源。

import (
	"sync/atomic"
	"testing"
	"time"

	ctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEngine_EffectsFromOutsideMatchAreNotCommitted(t *testing.T) {
	eng := newEngineForTest(t)

	var committed atomic.Bool
	// 模拟 FSM/业务代码直接求值：登记一个副作用（不经引擎 Match）
	c := newTestCtx("hello")
	c.DeferRuleEffect(func() { committed.Store(true) })

	// 同一个 Context 交给引擎：命中 matcher 并提交其（空的）副作用集合
	m := eng.OnAny()
	m.Handle(func(*ctx.Context) error { return nil })
	eng.ProcessEvent(c)

	assert.False(t, committed.Load(), "会话外的登记不得被引擎的无关 matcher 提交")
}

func TestEngine_CooldownOutsideMatchDoesNotConsume(t *testing.T) {
	eng := newEngineForTest(t)

	// 与引擎无关的 key：直接求值一次带冷却的规则
	rule := ctx.OnCooldown(time.Hour, func(*ctx.Context) string { return "outside-match-user" })
	c := newTestCtx("hello")
	require.True(t, rule(c), "首次直接求值应通过")

	// 同一 Context 交给引擎处理（命中某个 matcher 并提交）
	m := eng.OnAny()
	m.Handle(func(*ctx.Context) error { return nil })
	eng.ProcessEvent(c)

	// 冷却未被消耗：再次直接求值仍应通过
	require.True(t, rule(c), "引擎不得替无关路径消耗冷却")
}
