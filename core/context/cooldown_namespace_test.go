package context

// cooldown_namespace_test.go — OnCooldown 默认 key 的命名空间回归测试
//
// 回归背景：冷却表是进程级全局单表，默认 key 只有发送者 ID，且判断与写入
// 都不带命令/matcher 维度——一个命令触发冷却后，同一用户使用另一个命令在
// 窗口内静默不响应。

import (
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOnCooldown_DefaultKeyNamespacedPerRule(t *testing.T) {
	evt := newMockEventWithSender(platform.EventKindPrivateMessage, "cd_ns_user")
	c := NewContextFromEvent(evt, nil)

	sign := OnCooldown(time.Hour, nil)
	daily := OnCooldown(time.Hour, nil)

	require.True(t, evalRuleSession(c, sign)) // 模拟引擎在 /sign 命中后提交冷却

	assert.False(t, sign(c), "同一规则实例的冷却在窗口内应拦截")
	assert.True(t, daily(c), "另一条默认 key 规则（＝另一个命令）的冷却必须独立")
}

func TestOnCooldown_SharedRuleSharesCooldown(t *testing.T) {
	evt := newMockEventWithSender(platform.EventKindPrivateMessage, "cd_shared_user")
	c := NewContextFromEvent(evt, nil)

	// 同一个规则实例挂到多个 matcher 是显式的"共享冷却"写法，行为保持不变
	shared := OnCooldown(time.Hour, nil)

	require.True(t, evalRuleSession(c, shared))

	assert.False(t, shared(c), "同一规则实例应共享冷却")
}
