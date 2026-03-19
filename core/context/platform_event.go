package context

// platform_event.go — platform.Event 集成层
//
// Context 通过 AcquireContextFromEvent 绑定 platform.Event，
// 所有平台均使用此路径。

import (
	stdctx "context"
	"sync"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// AcquireContextFromEvent 从 platform.Event 获取 Context
//
// 与 ReleaseContextFromEvent 配对使用，从对象池获取 Context 并初始化。
func AcquireContextFromEvent(event platform.Event, sender platform.Sender) *Context {
	ctx := contextPool.Get().(*Context)

	ctx.platformEvent = event
	ctx.platformSender = sender
	ctx.matcher = nil
	ctx.extensions = nil
	ctx.extInitialized.Store(false)

	// Reset content cache
	ctx.contentOnce = sync.Once{}
	ctx.content = ""

	return ctx
}

// ReleaseContextFromEvent 归还由 AcquireContextFromEvent 获取的 Context
func ReleaseContextFromEvent(ctx *Context) {
	if ctx == nil {
		return
	}
	ctx.platformEvent = nil
	ctx.platformSender = nil
	ReleaseContext(ctx) // 复用原有清理逻辑
}

// GetPlatformEvent 返回当前 Context 绑定的 platform.Event。
func (ctx *Context) GetPlatformEvent() platform.Event {
	if ctx == nil {
		return nil
	}
	return ctx.platformEvent
}

// GetPlatformSender 返回当前 Context 绑定的 platform.Sender。
func (ctx *Context) GetPlatformSender() platform.Sender {
	if ctx == nil {
		return nil
	}
	return ctx.platformSender
}

// GetEventKind 返回平台无关的事件类别。
func (ctx *Context) GetEventKind() platform.EventKind {
	if ctx == nil {
		return platform.EventKindUnknown
	}
	if ctx.platformEvent != nil {
		return ctx.platformEvent.Kind()
	}
	return platform.EventKindUnknown
}

// GetEventType 获取事件类型字符串，供 Engine 内部路由使用。
//
// 返回 platform.EventKind 字符串（如 "PRIVATE_MESSAGE"）。
// 若无绑定事件，返回空字符串。
func (ctx *Context) GetEventType() string {
	if ctx == nil {
		return ""
	}
	if ctx.platformEvent != nil {
		return string(ctx.platformEvent.Kind())
	}
	return ""
}

// GetEventPlatform 返回事件来源平台标识（如 "qq"、"discord"）。
func (ctx *Context) GetEventPlatform() string {
	if ctx == nil {
		return ""
	}
	if ctx.platformEvent != nil {
		return ctx.platformEvent.Platform()
	}
	return ""
}

// Reply 向事件来源会话发送回复（平台无关方式）。
//
// ChatInfo（IsGroup 等）会自动注入到 Go context，供平台发送器路由使用。
//
// 示例：
//
//	ctx.Reply(platform.TextMessage("pong"))
func (ctx *Context) Reply(msg platform.OutboundMessage) error {
	if ctx == nil {
		return ErrNilContext
	}
	if ctx.platformEvent == nil || ctx.platformSender == nil {
		return ErrNoPlatformSender
	}
	chat := ctx.platformEvent.Chat()
	goCtx := platform.WithChatInfo(stdctx.Background(), chat)
	return ctx.platformSender.Send(goCtx, chat.ID, msg)
}

// ReplyWithContext 与 Reply 相同，但使用调用方传入的 context（用于超时控制）。
//
// ChatInfo 会叠加注入到 stdCtx 中，不会覆盖已有的超时/取消信号。
func (ctx *Context) ReplyWithContext(stdCtx stdctx.Context, msg platform.OutboundMessage) error {
	if ctx == nil {
		return ErrNilContext
	}
	if ctx.platformEvent == nil || ctx.platformSender == nil {
		return ErrNoPlatformSender
	}
	chat := ctx.platformEvent.Chat()
	goCtx := platform.WithChatInfo(stdCtx, chat)
	return ctx.platformSender.Send(goCtx, chat.ID, msg)
}

// IsPlatformContext 返回此 Context 是否由平台路径（platform.Event）创建
func (ctx *Context) IsPlatformContext() bool {
	if ctx == nil {
		return false
	}
	return ctx.platformEvent != nil
}
