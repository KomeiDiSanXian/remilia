package engine

import (
	"sync/atomic"
	"testing"

	ctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
)

// OnCommand 只给前缀（不含命令名）时的行为回归测试。
//
// 回归背景：OnCommand("/")、OnCommand("!!") 这类模式没有命令名，
// 此前仍被标记为 commandIndexed（Match 跳过 Rules[0] 前缀检查），却又
// 进不了命令索引：注册后静默不匹配，一旦索引重建就变成匹配一切。
func TestEngine_OnCommandPrefixOnlyIsRuleDriven(t *testing.T) {
	eng := newEngineForTest(t)

	var calls atomic.Int32
	eng.OnCommand("", "/").Handle(func(*ctx.Context) error {
		calls.Add(1)
		return nil
	})

	process := func(content string) {
		eng.ProcessEvent(ctx.NewContextFromEvent(
			newTestPlatformEventWithContent(platform.EventKindPrivateMessage, content), nil))
	}

	process("/help")
	assert.Equal(t, int32(1), calls.Load(), "前缀消息应命中")

	process("hello world")
	assert.Equal(t, int32(1), calls.Load(), "非前缀消息不应命中")

	// 再注册一个命令触发索引重建，行为必须保持一致
	eng.OnCommand("", "/ping").Handle(func(*ctx.Context) error { return nil })

	process("hello again")
	assert.Equal(t, int32(1), calls.Load(), "索引重建后非前缀消息仍不应命中")

	process("/help again")
	assert.Equal(t, int32(2), calls.Load(), "索引重建后前缀消息仍应命中")
}
