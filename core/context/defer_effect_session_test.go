package context

// defer_effect_session_test.go — 延迟副作用登记必须绑定匹配会话的回归测试
//
// 回归背景：DeferRuleEffect 只往 Context 里追加登记项，不区分"谁在登记"。
// FSM 的 Event.Match、业务代码手工求值等路径会把副作用登记进同一个 Context，
// 随后被引擎里某个无关 matcher 的提交执行——例如"任意状态 /cancel"流程之外
// 的规则消耗掉冷却额度（文档承诺这些场景不执行副作用，实现却会漏进提交窗口）。

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeferRuleEffect_OutsideSessionIsDropped(t *testing.T) {
	c := newTestCtx()

	var ran bool
	// 会话外（FSM/业务代码直接求值规则）的登记必须被丢弃
	c.DeferRuleEffect(func() { ran = true })
	c.CommitPendingRuleEffects()
	assert.False(t, ran, "会话外的副作用登记不应被提交")

	// 引擎路径（会话内）照常提交
	c.BeginRuleEffectSession()
	c.DeferRuleEffect(func() { ran = true })
	require.True(t, c.ruleEffectSession.Load(), "会话应处于打开状态")
	c.CommitPendingRuleEffects()
	assert.True(t, ran, "会话内的副作用登记应被提交")

	// 提交即关闭会话：其后的登记不再生效
	c.DeferRuleEffect(func() { ran = false })
	c.CommitPendingRuleEffects()
	assert.True(t, ran, "提交后会话关闭，后续登记应被丢弃")
}

func TestDeferRuleEffect_SessionClosedByDiscard(t *testing.T) {
	c := newTestCtx()

	c.BeginRuleEffectSession()
	var ran bool
	c.DeferRuleEffect(func() { ran = true })
	c.DiscardPendingRuleEffects()
	assert.False(t, c.ruleEffectSession.Load(), "丢弃即关闭会话")

	c.BeginRuleEffectSession()
	c.DeferRuleEffect(func() { ran = true })
	c.CommitPendingRuleEffects()
	assert.True(t, ran)
}
