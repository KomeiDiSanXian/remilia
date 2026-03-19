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
		ctx.content = ctx.platformEvent.Content()
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
