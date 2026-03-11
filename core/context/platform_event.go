package context

// platform_event.go — platform.Event 集成层
//
// 本文件为 Context 添加 platform.Event 感知能力，实现渐进式迁移：
//
//   旧路径（QQ 专属，保持不变）：
//     AcquireContext(*dto.Payload, openapi.OpenAPI) → 走 dto.Payload 解码
//
//   新路径（平台无关）：
//     AcquireContextFromEvent(platform.Event, platform.Sender) → 走 platform.Event
//
// 两条路径在 Engine 层统一为 *context.Context，对上层 Handler/Rule 透明。
// 当 platformEvent 非 nil 时，GetMessageContent / GetEventPlatform 等方法
// 优先读取 platform.Event 数据，从而不再依赖 dto.Payload。

import (
	stdctx "context"
	"sync"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// AcquireContextFromEvent 从 platform.Event 获取 Context（新平台无关路径）
//
// 与 AcquireContext(*dto.Payload, openapi.OpenAPI) 对称，供新平台使用。
// 返回的 Context 已填充 platformEvent 字段，GetMessageContent / GetEventKind 等
// 方法会优先从 platform.Event 读取，而非从 dto.Payload 解析。
func AcquireContextFromEvent(event platform.Event, sender platform.Sender) *Context {
	ctx := contextPool.Get().(*Context)

	ctx.event = nil // 无 QQ 原始 payload
	ctx.api = nil   // 无 QQ OpenAPI
	ctx.platformEvent = event
	ctx.platformSender = sender
	ctx.matcher = nil
	ctx.extensions = nil
	ctx.extInitialized.Store(false)

	// Reset typed decode cache
	ctx.decoded = decodeCache{}

	// Reset hot-path caches —— 由 platform.Event.Content() 填充
	ctx.contentOnce = sync.Once{}
	ctx.content = ""
	ctx.authorOnce = sync.Once{}
	ctx.author = nil

	return ctx
}

// ReleaseContextFromEvent 归还由 AcquireContextFromEvent 获取的 Context
//
// 与 ReleaseContext 相同逻辑，额外清空 platformEvent / platformSender。
func ReleaseContextFromEvent(ctx *Context) {
	if ctx == nil {
		return
	}
	ctx.platformEvent = nil
	ctx.platformSender = nil
	ReleaseContext(ctx) // 复用原有清理逻辑
}

// GetPlatformEvent 返回当前 Context 绑定的 platform.Event。
//
// 若 Context 由旧路径（AcquireContext）创建，返回 nil。
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
//
//   - 新路径（platform.Event）：直接返回 event.Kind()
//   - 旧路径（dto.Payload）：根据 dto.EventType 映射到 EventKind
func (ctx *Context) GetEventKind() platform.EventKind {
	if ctx == nil {
		return platform.EventKindUnknown
	}
	if ctx.platformEvent != nil {
		return ctx.platformEvent.Kind()
	}
	// 旧路径：按 QQ 事件类型做映射
	if ctx.event == nil {
		return platform.EventKindUnknown
	}
	switch ctx.event.Type {
	case dto.C2CMessageCreate:
		return platform.EventKindPrivateMessage
	case dto.GroupAtMessageCreate:
		return platform.EventKindGroupMessage
	case dto.AtMessageCreate, dto.MessageCreate, dto.DirectMessageCreate:
		return platform.EventKindGuildMessage
	case dto.FriendAdd, dto.FriendDel, dto.C2CMsgReject, dto.C2CMsgReceive,
		dto.GroupAddRobot, dto.GroupDelRobot, dto.GroupMsgReject, dto.GroupMsgReceive:
		return platform.EventKindNotice
	case dto.Ready, dto.Resumed:
		return platform.EventKindSystem
	default:
		return platform.EventKindUnknown
	}
}

// GetEventPlatform 返回事件来源平台标识（如 "qq"、"discord"）。
//
//   - 新路径：返回 platform.Event.Platform()
//   - 旧路径：固定返回 "qq"
func (ctx *Context) GetEventPlatform() string {
	if ctx == nil {
		return ""
	}
	if ctx.platformEvent != nil {
		return ctx.platformEvent.Platform()
	}
	return "qq" // 旧路径固定为 QQ
}

// Reply 向事件来源会话发送回复（平台无关方式）。
//
//   - 新路径：通过 platform.Sender 发送 platform.OutboundMessage
//   - 旧路径：需要使用 ReplyGroup / ReplyPrivate（dto.Message 格式）
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

// IsPlatformContext 返回此 Context 是否由新平台路径（platform.Event）创建
func (ctx *Context) IsPlatformContext() bool {
	if ctx == nil {
		return false
	}
	return ctx.platformEvent != nil
}
