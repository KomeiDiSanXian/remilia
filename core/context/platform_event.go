package context

// platform_event.go — platform.Event 集成层
//
// Context 通过 AcquireContextFromEvent 绑定 platform.Event，
// 所有平台均使用此路径。

import (
	stdctx "context"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// AcquireContextFromEvent 从 platform.Event 获取 Context
//
// 与 ReleaseContextFromEvent 配对使用，从对象池获取 Context 并初始化。
func AcquireContextFromEvent(event platform.Event, sender platform.Sender) *Context {
	ctx := contextPool.Get().(*Context)

	ctx.platformEvent = event
	ctx.platformSender = sender
	ctx.platformCaps = platform.Capabilities{} // 由 Engine 在 ProcessPlatformEvent 中注入
	ctx.botID = ""                             // 由 Engine 在 ProcessPlatformEventEx 中注入
	ctx.matcher = nil
	ctx.extensions = nil
	ctx.extInitialized.Store(false)

	// ctx.ctx 在 ReleaseContext 时已被设为 stdctx.Background()，无需再检查

	// Reset content cache
	ctx.contentOnce = sync.Once{}
	ctx.content = ""

	ctx.refCount.Store(1) // 基引用，由 processEventContext 在结束时 Release
	return ctx
}

// ReleaseContextFromEvent 等价于 ctx.Release()。
//
// 保留此函数用于兼容已有调用点。新代码应优先使用 ctx.Release()。
func ReleaseContextFromEvent(ctx *Context) {
	if ctx == nil {
		return
	}
	ctx.Release()
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

// Reply 向事件来源会话发送回复（平台无关方式），返回平台响应摘要。
//
// 返回的 SendResult.MessageID 可直接用于定时撤回、编辑等后续操作：
//
//	result, err := ctx.Reply(platform.TextMessage("pong"))
//	if err != nil { return err }
//	time.AfterFunc(10*time.Second, func() {
//	    if deleter, ok := ctx.GetPlatformSender().(platform.MessageDeleter); ok {
//	        _ = deleter.Delete(context.Background(), chat.ID, result.MessageID)
//	    }
//	})
func (ctx *Context) Reply(msg platform.OutboundMessage) (platform.SendResult, error) {
	if ctx == nil {
		return platform.SendResult{}, ErrNilContext
	}
	if ctx.platformEvent == nil || ctx.platformSender == nil {
		return platform.SendResult{}, ErrNoPlatformSender
	}
	req := platform.SendRequest{
		Target:  ctx.platformEvent.Chat(),
		Message: msg,
	}
	return ctx.platformSender.Send(ctx.Context(), req)
}

// ReplyWithContext 与 Reply 相同，但使用调用方传入的 context（用于超时控制）。
func (ctx *Context) ReplyWithContext(stdCtx stdctx.Context, msg platform.OutboundMessage) (platform.SendResult, error) {
	if ctx == nil {
		return platform.SendResult{}, ErrNilContext
	}
	if ctx.platformEvent == nil || ctx.platformSender == nil {
		return platform.SendResult{}, ErrNoPlatformSender
	}
	req := platform.SendRequest{
		Target:  ctx.platformEvent.Chat(),
		Message: msg,
	}
	return ctx.platformSender.Send(stdCtx, req)
}

// GetPlatformCapabilities 返回当前平台的能力声明。
//
// 由框架在 Engine.ProcessPlatformEvent 调用时自动注入，Handler 可用此方法
// 实现跨平台"渐进增强"策略（优先使用丰富特性，降级到纯文本）：
//
//	caps := ctx.GetPlatformCapabilities()
//	if caps.Embeds {
//	    ctx.Reply(platform.TextMessage("").WithEmbeds(embed))
//	} else {
//	    ctx.Reply(platform.MarkdownMessage(embed.Title + "\n" + embed.Description))
//	}
func (ctx *Context) GetPlatformCapabilities() platform.Capabilities {
	if ctx == nil {
		return platform.Capabilities{}
	}
	return ctx.platformCaps
}

// IsPlatformContext 返回此 Context 是否由平台路径（platform.Event）创建
func (ctx *Context) IsPlatformContext() bool {
	if ctx == nil {
		return false
	}
	return ctx.platformEvent != nil
}

// SetBotID 注入机器人自身的平台 ID（框架内部，由 Engine.ProcessPlatformEventEx 调用）。
//
// 注入后 IsFromSelf() 可正确判断事件是否由机器人自身触发。
func (ctx *Context) SetBotID(id string) {
	if ctx == nil {
		return
	}
	ctx.botID = id
}

// GetBotID 返回注入的机器人自身平台 ID。
//
// 未注入时返回空字符串。
func (ctx *Context) GetBotID() string {
	if ctx == nil {
		return ""
	}
	return ctx.botID
}

// IsFromSelf 报告当前事件是否由机器人自身触发。
//
// 需要引擎在 ProcessPlatformEventEx 中注入 botID 才能正常工作；
// 未注入时始终返回 false（安全默认值，不会误过滤正常消息）。
//
// 典型用法（在 Handler 或中间件中防止自回复）：
//
//	eng.On(string(platform.EventKindPrivateMessage)).Handle(func(ctx *context.Context) error {
//	    if ctx.IsFromSelf() {
//	        return nil // 忽略机器人自身发出的消息
//	    }
//	    // ... 正常处理
//	})
func (ctx *Context) IsFromSelf() bool {
	if ctx == nil || ctx.botID == "" {
		return false
	}
	if ctx.platformEvent == nil {
		return false
	}
	return ctx.platformEvent.Sender().ID == ctx.botID
}

// TryGetGroupMembers 尝试获取当前群的成员列表。
//
// 底层通过 platform.GroupInfoProvider 接口实现；若平台 Sender 不支持
// 此接口，或当前不是群组会话，返回 (nil, false)。
//
// 使用场景：需要获取群成员列表时（如随机选择成员、统计等）。
//
// 注意：大型群组下成员列表可能很长，各平台可能有分页限制；
// OneBot 通常可以返回完整列表。
//
// 使用示例：
//
//	members, ok := ctx.TryGetGroupMembers()
//	if !ok {
//	    // 平台不支持，降级到被动登记策略
//	}
//	for _, m := range members {
//	    fmt.Println(m.DisplayName)
//	}
func (ctx *Context) TryGetGroupMembers() ([]platform.GroupMemberInfo, bool) {
	if ctx == nil || ctx.platformSender == nil || ctx.platformEvent == nil {
		return nil, false
	}
	chat := ctx.platformEvent.Chat()
	if !chat.IsGroup || chat.ID == "" {
		return nil, false
	}
	gip, ok := ctx.platformSender.(platform.GroupInfoProvider)
	if !ok {
		return nil, false
	}
	members, err := gip.GetGroupMemberList(ctx.Context(), chat.ID)
	if err != nil {
		return nil, false
	}
	return members, true
}

// TryDeleteMemberMessage 尝试撤回当前消息。
// 底层通过 platform.AutoModerator 接口实现；若平台 Sender 不支持，
// 或消息 ID 为空，静默返回 nil（不报错）。
//
// 使用场景：黑名单过滤、违禁词检测、自动审核等需要撤回他人消息的场景。
//
// 注意：撤回他人消息需要机器人具有群管理员权限；无权限时平台会返回错误。
//
// 使用示例：
//
//	// handler 内，检测违规消息后撤回
//	if isSpam(ctx.GetMessageContent()) {
//	    _ = ctx.TryDeleteMemberMessage()
//	}
func (ctx *Context) TryDeleteMemberMessage() error {
	if ctx == nil || ctx.platformSender == nil || ctx.platformEvent == nil {
		return nil
	}
	am, ok := ctx.platformSender.(platform.AutoModerator)
	if !ok {
		return nil // 平台不支持，静默忽略
	}
	msgID := ctx.platformEvent.ID()
	if msgID == "" {
		return nil // 无法定位消息，忽略
	}
	chatID := ctx.platformEvent.Chat().ID
	return am.DeleteMemberMessage(ctx.Context(), chatID, msgID)
}

// TryMuteMessageAuthor 尝试禁言当前消息的发送者。
//
// 底层通过 platform.GroupManager 接口实现；若平台 Sender 不支持，静默返回 nil。
//
// 使用场景：黑名单自动禁言、违规自动处罚等。
//
// 注意：需要机器人具有群管理员权限；在私聊场景中（非群组消息）此操作无意义，静默忽略。
//
// 使用示例：
//
//	if isSpam(ctx.GetMessageContent()) {
//	    _ = ctx.TryMuteMessageAuthor(5 * time.Minute)
//	}
func (ctx *Context) TryMuteMessageAuthor(duration time.Duration) error {
	if ctx == nil || ctx.platformSender == nil || ctx.platformEvent == nil {
		return nil
	}
	chat := ctx.platformEvent.Chat()
	if !chat.IsGroup {
		return nil // 私聊场景忽略
	}
	gm, ok := ctx.platformSender.(platform.GroupManager)
	if !ok {
		return nil
	}
	userID := ctx.platformEvent.Sender().ID
	if userID == "" {
		return nil
	}
	return gm.BanMember(ctx.Context(), chat.ID, userID, duration)
}
