package context

// rules_withtimeout_effects_test.go — WithTimeout 与延迟副作用的隔离回归测试
//
// 回归背景：WithTimeout 把规则放到独立 goroutine 里执行，但把原 Context 直接
// 交给该 goroutine。引擎在超时返回后会继续推进匹配循环（Discard/Commit 延迟
// 副作用、SetMatcher…），于是：
//   - 迟到的 DeferRuleEffect 会写进后续 matcher 的匹配会话并被它提交，
//     等于用错误的事件消耗冷却；
//   - 规则 goroutine 与引擎并发读写 Context 字段（数据竞争）。

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithTimeout_EffectIsolation(t *testing.T) {
	t.Run("超时前登记的副作用随匹配会话提交", func(t *testing.T) {
		ctx := newTestCtx()

		var ran bool
		rule := WithTimeout(func(c *Context) bool {
			c.DeferRuleEffect(func() { ran = true })
			return true
		}, time.Second)

		require.True(t, evalRuleSession(ctx, rule))
		assert.True(t, ran, "规则在超时前返回，副作用应照常提交")
	})

	t.Run("超时后登记的副作用被丢弃", func(t *testing.T) {
		ctx := newTestCtx()

		ruleDone := make(chan struct{})
		var ran bool
		rule := WithTimeout(func(c *Context) bool {
			time.Sleep(30 * time.Millisecond)
			// 引擎已按超时丢弃本次匹配：这次登记属于迟到的写入
			c.DeferRuleEffect(func() { ran = true })
			close(ruleDone)
			return true
		}, 5*time.Millisecond)

		ctx.BeginRuleEffectSession()
		require.False(t, rule(ctx))
		// 引擎在超时后立即丢弃副作用
		ctx.DiscardPendingRuleEffects()

		// 等规则 goroutine 真正结束（它还在派生 Context 上写）
		<-ruleDone

		ctx.CommitPendingRuleEffects()
		assert.False(t, ran, "超时规则迟到的副作用不应被提交")
		assert.Empty(t, ctx.pendingRuleEffects)
	})

	t.Run("规则读取触发时的快照", func(t *testing.T) {
		ctx := newTestCtx()
		ctx.Set("stage", "first")

		released := make(chan struct{})
		seen := make(chan string, 1)
		rule := WithTimeout(func(c *Context) bool {
			<-released
			seen <- c.GetString("stage")
			return true
		}, 5*time.Millisecond)

		require.False(t, rule(ctx))

		// 超时后引擎会继续推进：规则应看到触发时的快照，而不是之后的写入
		ctx.Set("stage", "second")
		close(released)

		assert.Equal(t, "first", <-seen)
	})
}
