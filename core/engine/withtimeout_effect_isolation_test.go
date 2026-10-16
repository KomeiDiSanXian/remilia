package engine

import (
	"sync/atomic"
	"testing"
	"time"

	ctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// WithTimeout 规则的迟到副作用不得被后续 matcher 提交。
//
// 回归背景：超时返回后引擎继续推进匹配循环（丢弃本 matcher 的副作用、处理
// 下一个候选），而规则 goroutine 仍在运行；它迟到的 DeferRuleEffect 会写进
// 下一个 matcher 的匹配会话，由后者的 CommitPendingRuleEffects 提交——冷却/
// 额度被错误的事件消耗。同一路径还存在对 pendingRuleEffects 的并发读写。
//
// 修复后规则跑在触发时克隆的 Context 上：迟到登记只留在副本里，无人提交。
func TestEngine_TimedOutRuleEffectNotCommittedByNextMatcher(t *testing.T) {
	eng := newEngineForTest(t)

	var leaked atomic.Bool

	// M1：5ms 超时；规则 40ms 后才登记副作用（必然迟到）
	m1 := eng.OnAny(ctx.WithTimeout(func(c *ctx.Context) bool {
		time.Sleep(40 * time.Millisecond)
		c.DeferRuleEffect(func() { leaked.Store(true) })
		return true
	}, 5*time.Millisecond))
	m1.SetPriority(10)
	m1.Handle(func(*ctx.Context) error { return nil })

	// M2：50ms 后才通过并登记自己的副作用——它的 Commit 会顺带执行
	// 落进本会话的迟到登记项（修复前）
	var own atomic.Bool
	m2 := eng.OnAny(func(c *ctx.Context) bool {
		time.Sleep(50 * time.Millisecond)
		c.DeferRuleEffect(func() { own.Store(true) })
		return true
	})
	m2.SetPriority(20)
	m2.Handle(func(*ctx.Context) error { return nil })

	eng.ProcessEvent(ctx.NewContextFromEvent(
		newTestPlatformEventWithContent(platform.EventKindPrivateMessage, "hello"), nil))

	assert.False(t, leaked.Load(), "超时规则迟到的副作用不应被后续 matcher 提交")
	assert.True(t, own.Load(), "命中 matcher 自己的副作用应照常提交")
}

// 超时规则自身的副作用（超时前登记）在规则超时时同样不提交。
func TestEngine_TimedOutRuleDoesNotCommitItsOwnEffect(t *testing.T) {
	eng := newEngineForTest(t)

	var committed atomic.Bool
	m := eng.OnAny(ctx.WithTimeout(func(c *ctx.Context) bool {
		c.DeferRuleEffect(func() { committed.Store(true) })
		time.Sleep(40 * time.Millisecond)
		return true
	}, 5*time.Millisecond))
	m.Handle(func(*ctx.Context) error { return nil })

	var otherMatched atomic.Bool
	eng.OnAny().SetPriority(20).Handle(func(*ctx.Context) error {
		otherMatched.Store(true)
		return nil
	})

	eng.ProcessEvent(ctx.NewContextFromEvent(
		newTestPlatformEventWithContent(platform.EventKindPrivateMessage, "hello"), nil))

	// 规则超时 → 该 matcher 不通过 → 其副作用不得提交（在超时前登记也一样）
	assert.False(t, committed.Load())
	require.True(t, otherMatched.Load(), "后续 matcher 仍应正常命中")
}
