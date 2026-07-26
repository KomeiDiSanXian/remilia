package main

import (
	"fmt"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// replyCtx 快捷发送文本回复。简化 handler 中 ctx.Reply 的调用。
func replyCtx(ctx *eventctx.Context, content string) error {
	ctx.Reply(platform.TextMessage(content))
	return nil
}

// replyFunc 返回一个闭包，供 keywordfilter 的 OnMatch 回调使用。
func replyFunc(format string) func(ctx *eventctx.Context, matched string) error {
	return func(ctx *eventctx.Context, matched string) error {
		return replyCtx(ctx, fmt.Sprintf(format, matched))
	}
}
