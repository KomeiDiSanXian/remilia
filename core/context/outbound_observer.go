package context

// outbound_observer.go — 出站消息观察者（Outbound Observer）。
//
// 通过 [OutboundObserverExt] 类型的上下文扩展注入，[Context.Reply] /
// [Context.ReplyWithContext] 在出站发送完成、Future 解析后同步回调观察者。
//
// 这是"观察出站消息而不包装 platform.Sender"的机制：
// 包装 sender 会破坏 platformSender 上的可选接口断言（MessageDeleter、
// GroupManager、AutoModerator 等，见 platform_event.go 中各能力方法），
// 而观察者仅依赖 ctx.Reply 的调度任务，不影响任何平台能力接口。
//
// 注意：观察者只覆盖经 ctx.Reply* 发送的出站消息；
// 插件直接调用 platform.Sender.Send（绕过 ctx.Reply，如 sendqueue）不会被观察到。

import (
	stdctx "context"
	"fmt"

	"github.com/KomeiDiSanXian/remilia/infra/future"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// msgPreview 返回消息内容的截断预览（仅用于日志定位）。
func msgPreview(msg platform.OutboundMessage) string {
	text := msg.Text
	if text == "" {
		text = msg.Markdown
	}
	if text == "" {
		return "(非文本消息)"
	}
	const maxLen = 64
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen] + "..."
}

// OutboundObserver 观察出站消息发送完成后的结果。
//
// 回调在 dispatcher 的发送任务内同步执行（即 ctx.Reply 的异步发送 goroutine 中），
// 不阻塞 handler。实现方应保持轻量，避免在回调内做重 I/O。
type OutboundObserver interface {
	// OnOutbound 在出站发送完成后回调。
	// chatID 为目标会话 ID；req/res 为发送请求与平台响应；err 为发送错误（成功时为 nil）。
	OnOutbound(chatID string, req platform.SendRequest, res platform.SendResult, err error)
}

// OutboundObserverExt 通过类型扩展注入出站观察者。
// 中间件/插件应通过 [Context.AddOutboundObserver] 追加，Reply 在发送任务内
// 通过 GetTyped 读取。
type OutboundObserverExt struct {
	Observer OutboundObserver
}

// OutboundObservers 把多个观察者组合成一个（顺序回调）。
//
// 出站结果有多个独立消费者：messagelog 需要记录出站消息，发送指标需要
// 统计成功/失败。扩展槽只有一个，谁后写谁生效——此前 messagelog 的中间件
// 注册在指标中间件之后，于是 remilia_send_total 从来没有拿到过样本。
type OutboundObservers []OutboundObserver

// OnOutbound 依次回调全部观察者（nil 元素跳过）。
func (o OutboundObservers) OnOutbound(chatID string, req platform.SendRequest, res platform.SendResult, err error) {
	for _, obs := range o {
		if obs != nil {
			obs.OnOutbound(chatID, req, res, err)
		}
	}
}

// AddOutboundObserver 向上下文追加一个出站观察者，与已注册的观察者共存。
//
// 与直接 SetTyped 的区别：不会覆盖其他子系统已注入的观察者。首次注入时
// 行为与 SetTyped 相同；此后每次追加都会合并进 OutboundObservers。
func (ctx *Context) AddOutboundObserver(obs OutboundObserver) {
	if ctx == nil || obs == nil {
		return
	}
	ext, ok := ctx.Ext().GetTyped[OutboundObserverExt]()
	if !ok || ext.Observer == nil {
		ctx.Ext().SetTyped(OutboundObserverExt{Observer: obs})
		return
	}
	if multi, ok := ext.Observer.(OutboundObservers); ok {
		ctx.Ext().SetTyped(OutboundObserverExt{
			Observer: append(append(OutboundObservers(nil), multi...), obs),
		})
		return
	}
	ctx.Ext().SetTyped(OutboundObserverExt{Observer: OutboundObservers{ext.Observer, obs}})
}

// outboundObserverFrom 从上下文扩展中读取观察者，无则返回 nil。
func outboundObserverFrom(ctx *Context) OutboundObserver {
	if ctx == nil {
		return nil
	}
	ext, ok := ctx.Ext().GetTyped[OutboundObserverExt]()
	if !ok {
		return nil
	}
	return ext.Observer
}

// submitReply 在 dispatcher 任务内执行发送并通知观察者。
//
// 独立函数而非方法，避免异步任务捕获 *Context 本身（发送任务的闭包
// 只捕获 req/sender/chatID/f/obs 等局部值，与 Reply 原有的无捕获 ctx 约定一致）。
func submitReply(sendCtx stdctx.Context, req platform.SendRequest, sender platform.Sender,
	chatID string, f *future.Future[platform.SendResult], obs OutboundObserver) error {
	// Reply 层保证 Future 被 Resolve（即使是 panic）
	defer func() {
		if r := recover(); r != nil {
			f.Resolve(platform.SendResult{}, fmt.Errorf("panic in send: %v", r))
			panic(r)
		}
	}()
	res, err := sender.Send(sendCtx, req)
	if err != nil {
		// 发送失败必须可见：此前所有 ctx.Reply 的发送错误都被静默吞掉
		// （dispatcher 不记日志、messagelog.OnOutbound 遇错跳过、handler 忽略 Future），
		// 导致"handler 执行成功但消息未发出"类问题无法定位。
		logger.WithError(err).WithFields(logger.Fields{
			"chat_id":  chatID,
			"is_group": req.Target.IsGroup,
			"text":     msgPreview(req.Message),
		}).Warn("[Outbound] message send failed")
	}
	f.Resolve(res, err)
	if obs != nil {
		obs.OnOutbound(chatID, req, res, err)
	}
	return err
}
