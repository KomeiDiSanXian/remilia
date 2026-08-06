package context

import (
	"github.com/KomeiDiSanXian/remilia/platform"
)

// decode.go — 平台无关的热路径字段缓存
//
// 包含：
//   - GetMessageContent：消息内容缓存（contentOnce）
//   - GetSenderInfo：发送者信息

// GetMessageContent 获取消息内容（零拷贝 + Once 缓存）
//
// 第一次调用执行解析；同一 Context 的后续调用直接返回缓存值。
// 在多 Matcher 场景（每个 Matcher 都调用此方法做内容匹配）时开销接近零。
func (ctx *Context) GetMessageContent() string {
	if ctx == nil {
		return ""
	}
	if ctx.platformEvent == nil {
		return ""
	}
	ctx.contentOnce.Do(func() {
		ctx.content = platform.Content(ctx.platformEvent)
	})
	return ctx.content
}

// GetSenderInfo 获取发送者信息（平台无关）
func (ctx *Context) GetSenderInfo() platform.UserInfo {
	if ctx == nil {
		return platform.UserInfo{}
	}
	if ctx.platformEvent == nil {
		return platform.UserInfo{}
	}
	return ctx.platformEvent.Sender()
}

// GetChatInfo 获取当前会话信息（平台无关）。
//
// 返回 platform.ChatInfo，包含 ID、IsGroup 等字段。
// ctx 或底层事件为 nil 时返回零值。
func (ctx *Context) GetChatInfo() platform.ChatInfo {
	if ctx == nil {
		return platform.ChatInfo{}
	}
	if ctx.platformEvent == nil {
		return platform.ChatInfo{}
	}
	return ctx.platformEvent.Chat()
}
