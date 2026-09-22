// deadline.go — 以插件独立预算替换事件上下文 deadline。
//
// 全局 Timeout 中间件给事件上下文注入的是单次调用 deadline；多轮工具循环
// 共享它时，长任务会在中途被整段切断。这里把 deadline 换成回合预算。
package runtime

import (
	"context"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
)

// LiftEventDeadline 以给定预算替换事件上下文的 deadline。
// context.WithoutCancel 去掉全局 Timeout 中间件注入的 deadline 与取消信号
// （保留 values/tracing），再套上预算。返回恢复函数（恢复原 stdCtx）。
func LiftEventDeadline(ctx *eventctx.Context, budget time.Duration) func() {
	orig := ctx.Context()
	turnCtx, cancel := context.WithTimeout(context.WithoutCancel(orig), budget)
	ctx.SetStdContext(turnCtx)
	return func() {
		cancel()
		ctx.SetStdContext(orig)
	}
}
