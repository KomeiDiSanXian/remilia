package engine

import (
	"sync/atomic"
	"testing"
	"time"

	ctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
)

// OnCooldown 默认 key 必须按命令隔离。
//
// 回归背景：默认 key 只有发送者 ID，而冷却表是进程级全局单表——一个命令
// 触发冷却后，同一用户使用另一个命令在窗口内静默不响应。
func TestEngine_CooldownDefaultKeyIsPerCommand(t *testing.T) {
	eng := newEngineForTest(t)

	var signCalls, dailyCalls atomic.Int32

	mSign := eng.OnCommand("", "/sign", ctx.OnCooldown(time.Hour, nil))
	mSign.Handle(func(*ctx.Context) error {
		signCalls.Add(1)
		return nil
	})

	mDaily := eng.OnCommand("", "/daily", ctx.OnCooldown(time.Hour, nil))
	mDaily.Handle(func(*ctx.Context) error {
		dailyCalls.Add(1)
		return nil
	})

	process := func(content string) {
		eng.ProcessEvent(ctx.NewContextFromEvent(
			newTestPlatformEventWithContent(platform.EventKindPrivateMessage, content), nil))
	}

	process("/sign")
	assert.Equal(t, int32(1), signCalls.Load(), "/sign 首次应命中")

	process("/sign")
	assert.Equal(t, int32(1), signCalls.Load(), "/sign 冷却中不应重复命中")

	process("/daily")
	assert.Equal(t, int32(1), dailyCalls.Load(), "另一个命令的冷却必须独立")
}
