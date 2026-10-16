package engine

import (
	"sync/atomic"
	"testing"

	ctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
)

// 空事件类型候选流去重回归测试。
//
// 回归背景：ctx.GetEventType() 为空（Context 未绑定平台事件，或事件 Kind 为空）时，
// 各索引的 specific 与 generic 两路指向同一个桶，引擎仍把两路都并入归并迭代器，
// 于是同一个 matcher 在候选流中出现两次、handler 被执行两次。
func TestEngine_EmptyEventTypeRunsHandlerOnce(t *testing.T) {
	// 空事件类型 + 指定消息内容的 Context
	emptyTypeCtx := func(content string) *ctx.Context {
		return ctx.NewContextFromEvent(
			newTestPlatformEventWithContent(platform.EventKind(""), content), nil)
	}

	t.Run("permanent index", func(t *testing.T) {
		eng := newEngineForTest(t)

		var calls atomic.Int32
		eng.OnAny().Handle(func(*ctx.Context) error {
			calls.Add(1)
			return nil
		})

		eng.ProcessEvent(emptyTypeCtx("hello"))
		assert.Equal(t, int32(1), calls.Load(), "通用 matcher 只应执行一次")
	})

	t.Run("command index", func(t *testing.T) {
		eng := newEngineForTest(t)

		var calls atomic.Int32
		eng.OnCommand("", "/ping").Handle(func(*ctx.Context) error {
			calls.Add(1)
			return nil
		})

		eng.ProcessEvent(emptyTypeCtx("/ping"))
		assert.Equal(t, int32(1), calls.Load(), "命令 matcher 只应执行一次")
	})

	t.Run("regex index", func(t *testing.T) {
		eng := newEngineForTest(t)

		var calls atomic.Int32
		eng.OnAny().Regex(`^/ping`).Handle(func(*ctx.Context) error {
			calls.Add(1)
			return nil
		})

		eng.ProcessEvent(emptyTypeCtx("/ping"))
		assert.Equal(t, int32(1), calls.Load(), "正则 matcher 只应执行一次")
	})

	t.Run("temp index", func(t *testing.T) {
		eng := newEngineForTest(t)

		var calls atomic.Int32
		m := eng.OnTemp("")
		m.SetTempWithMaxUse(5)
		m.Handle(func(*ctx.Context) error {
			calls.Add(1)
			return nil
		})

		eng.ProcessEvent(emptyTypeCtx("hello"))
		assert.Equal(t, int32(1), calls.Load(), "临时 matcher 只应执行一次")
	})
}
