package engine

// exec_profile_test.go — ExecProfile 的执行分类判定（同步 / 走池）
//
// 回归背景：demoted 曾是粘滞标志，只由单次 >2×阈值（100ms）的极慢样本
// 清除。"曾经很快、后来稳定 60ms"的 handler 会永久留在 Direct 分类，
// 一直占用并阻塞事件派发 goroutine。

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExecProfile_RePromotesAfterSlowStreak 验证曾经 demote 的 handler
// 在连续慢样本后重新走池。
func TestExecProfile_RePromotesAfterSlowStreak(t *testing.T) {
	p := newExecProfile()

	// 窗口填满快样本 → demote
	for range execProfileWindowSize + 8 {
		p.Record(time.Millisecond)
	}
	require.Equal(t, ExecClassDirect, p.ShouldPool(), "快 handler 应被降级为同步执行")

	// 之后稳定变慢（60ms：超过阈值但不是单次极慢）
	for range execProfileSlowStreak {
		p.Record(60 * time.Millisecond)
	}
	assert.Equal(t, ExecClassPool, p.ShouldPool(),
		"连续慢样本必须把 handler 重新提升到池（否则永久阻塞派发 goroutine）")
	assert.True(t, p.IsPromoted())
}

// TestExecProfile_SingleSlowSamplePromotes 验证单次极慢样本立即提升。
func TestExecProfile_SingleSlowSamplePromotes(t *testing.T) {
	p := newExecProfile()
	for range execProfileWindowSize + 8 {
		p.Record(time.Millisecond)
	}
	require.Equal(t, ExecClassDirect, p.ShouldPool())

	p.Record(defaultSlowThreshold*3 + time.Millisecond)
	assert.Equal(t, ExecClassPool, p.ShouldPool(), "单次极慢应立刻隔离到池")
}
