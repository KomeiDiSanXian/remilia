package context

import (
	"fmt"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
)

func makeFakeContext() *Context {
	return NewContextFromEvent(newMockEvent(platform.EventKindPrivateMessage), nil)
}

// cdTestSeq 为冷却测试生成每轮唯一的 key，避免全局 cooldownStore 跨轮残留。
var cdTestSeq atomic.Int64

func TestOnCooldown_AllowsFirst(t *testing.T) {
	rule := OnCooldown(1*time.Second, func(c *Context) string { return "cd_user_first" })
	c := makeFakeContext()
	if !rule(c) {
		t.Error("first call should be allowed")
	}
}
func TestOnCooldown_BlocksSecond(t *testing.T) {
	key := fmt.Sprintf("cd_user_second_%d", cdTestSeq.Add(1))
	rule := OnCooldown(10*time.Second, func(c *Context) string { return key })
	c := makeFakeContext()
	rule(c)
	// 冷却写入是延迟副作用：模拟引擎在 matcher 命中后的提交
	c.CommitPendingRuleEffects()
	if rule(c) {
		t.Error("second call within cooldown should be blocked")
	}
}
func TestOnCooldown_AllowsAfterExpiry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rule := OnCooldown(40*time.Millisecond, func(c *Context) string { return "cd_user_expiry" })
		c := makeFakeContext()
		rule(c)
		c.CommitPendingRuleEffects()
		time.Sleep(50 * time.Millisecond)
		if !rule(c) {
			t.Error("should be allowed after cooldown expiry")
		}
	})
}
func TestOnCooldown_EmptyKeyAllows(t *testing.T) {
	rule := OnCooldown(1*time.Hour, func(c *Context) string { return "" })
	c := makeFakeContext()
	if !rule(c) {
		t.Error("empty key should always allow")
	}
}

// TestOnCooldown_DiscardDoesNotConsume 验证延迟副作用被丢弃时不消耗冷却：
// 对应"规则通过但所在 matcher 未命中（后续规则失败/其他 matcher 处理）"的场景。
func TestOnCooldown_DiscardDoesNotConsume(t *testing.T) {
	key := fmt.Sprintf("cd_user_discard_%d", cdTestSeq.Add(1))
	rule := OnCooldown(1*time.Hour, func(c *Context) string { return key })
	c := makeFakeContext()

	if !rule(c) {
		t.Fatal("first check should pass")
	}
	// 模拟引擎在后续规则失败后的丢弃
	c.DiscardPendingRuleEffects()

	// 冷却未被消耗：再次检查仍应通过
	if !rule(c) {
		t.Error("cooldown must not be consumed when pending effects are discarded")
	}
	c.CommitPendingRuleEffects()

	// 提交后进入冷却
	if rule(c) {
		t.Error("cooldown should block after effects are committed")
	}
	c.DiscardPendingRuleEffects()
}
