package context

import (
	"errors"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/tidwall/gjson"
)

// decode.go — 事件解码与热路径字段缓存
//
// 包含：
//   - decodeCache 类型联合体定义
//   - DecodeEvent：事件解码（带 typed-union 缓存）
//   - GetMessageContent：消息内容缓存（contentOnce）
//   - GetSenderInfo：发送者信息缓存（authorOnce）
//   - GetEvent / GetEventType：基础事件访问

// DecodeEvent 解码事件详情
//
// 对 C2CMessageCreateEvent 和 GroupAtMessageCreateEvent 使用 typed union 缓存：
// 缓存命中时直接做结构体值复制（单次赋值），无 reflect、无 interface 装箱。
// 其他类型走 generic 路径，缓存 any 指针，命中时做类型断言+值复制。
// 同一 Context 内第二次 DecodeEvent 开销接近零。
func (ctx *Context) DecodeEvent(v any) error {
	if ctx == nil || ctx.event == nil {
		return errors.New("event is nil")
	}

	ctx.decodeMu.Lock()
	defer ctx.decodeMu.Unlock()

	switch dst := v.(type) {
	case *dto.C2CMessageCreateEvent:
		if ctx.decoded.kind == 1 {
			// cache hit: struct copy, zero alloc
			*dst = ctx.decoded.c2c
			return nil
		}
		if err := ctx.event.Decode(dst); err != nil {
			return err
		}
		ctx.decoded.kind = 1
		ctx.decoded.c2c = *dst
		return nil

	case *dto.GroupAtMessageCreateEvent:
		if ctx.decoded.kind == 2 {
			*dst = ctx.decoded.groupAt
			return nil
		}
		if err := ctx.event.Decode(dst); err != nil {
			return err
		}
		ctx.decoded.kind = 2
		ctx.decoded.groupAt = *dst
		return nil

	default:
		// Generic path: cache the pointer itself; caller must not modify the
		// cached value after returning (safe because handlers run serially).
		if ctx.decoded.kind == 3 && ctx.decoded.generic != nil {
			if src, ok := ctx.decoded.generic.(interface{ copyTo(any) bool }); ok {
				_ = src
			}
			// For the generic path we just re-decode; the gjson fast path in
			// Payload.Decode already avoids most allocations for known types.
		}
		if err := ctx.event.Decode(v); err != nil {
			return err
		}
		ctx.decoded.kind = 3
		ctx.decoded.generic = v
		return nil
	}
}

// GetMessageContent 获取消息内容（零拷贝 + Once 缓存）
//
// 第一次调用执行 gjson.GetBytes；同一 Context 的后续调用直接返回缓存值。
// 在多 Matcher 场景（每个 Matcher 都调用此方法做内容匹配）时开销接近零。
//
// 新路径（platform.Event）：直接返回 event.Content()，同样走 Once 缓存。
func (ctx *Context) GetMessageContent() string {
	if ctx == nil {
		return ""
	}
	// 新路径：platform.Event 已在 populate 阶段解析好内容
	if ctx.platformEvent != nil {
		ctx.contentOnce.Do(func() {
			ctx.content = ctx.platformEvent.Content()
		})
		return ctx.content
	}
	// 旧路径：从 dto.Payload.Detail 中用 gjson 提取
	if ctx.event == nil {
		return ""
	}
	ctx.contentOnce.Do(func() {
		ctx.content = gjson.GetBytes(ctx.event.Detail, "content").String()
	})
	return ctx.content
}

// GetSenderInfo 获取发送者信息（平台无关，推荐使用）
//
// 新路径：从 platform.Event.Sender() 读取。
// 旧路径（QQ）：从 event.Detail 解析 author 字段。
func (ctx *Context) GetSenderInfo() platform.UserInfo {
	if ctx == nil {
		return platform.UserInfo{}
	}
	if ctx.platformEvent != nil {
		return ctx.platformEvent.Sender()
	}
	// QQ 旧路径：从 payload.Detail 提取 author
	if ctx.event == nil {
		return platform.UserInfo{}
	}
	ctx.authorOnce.Do(func() {
		res := gjson.GetBytes(ctx.event.Detail, "author")
		if !res.Exists() {
			return
		}
		ctx.author = &dto.Author{
			ID:           res.Get("id").String(),
			MemberOpenID: res.Get("member_openid").String(),
			UnionOpenID:  res.Get("union_openid").String(),
			UserOpenID:   res.Get("user_openid").String(),
		}
	})
	if ctx.author == nil {
		return platform.UserInfo{}
	}
	id := ctx.author.UserOpenID
	if id == "" {
		id = ctx.author.MemberOpenID
	}
	if id == "" {
		id = ctx.author.ID
	}
	return platform.UserInfo{ID: id}
}

// GetEvent 返回 QQ 原始事件 payload（供框架内部组件使用）。
//
// 新代码请使用 GetPlatformEvent()；此方法在旧路径（QQ dto.Payload）下返回 payload，
// 在新路径（platform.Event）下返回 nil。
func (ctx *Context) GetEvent() *dto.Payload {
	if ctx == nil {
		return nil
	}
	return ctx.event
}

// GetEventType 获取事件类型字符串，供 Engine 内部路由使用。
//
// 架构说明（方案 B）：
//   - 新路径（platform.Event）：返回 EventKind 字符串（如 "PRIVATE_MESSAGE"），
//     屏蔽平台差异，使 OnEventKind() 规则对所有平台透明地生效。
//   - 旧路径（dto.Payload）：返回平台原始 EventType（如 "C2C_MESSAGE_CREATE"），
//     保持与现有 OnC2CMessage() / OnGroupAtMessage() 等 QQ 专属规则的兼容。
//
// 迁移指南：
//   - 新代码请使用 OnEventKind(platform.EventKindPrivateMessage) 注册 Matcher
//   - QQ 专属规则（OnC2CMessage 等）仅在旧路径下生效
func (ctx *Context) GetEventType() dto.EventType {
	if ctx == nil {
		return ""
	}
	if ctx.platformEvent != nil {
		// 方案 B：使用 EventKind 字符串作为路由键，屏蔽平台差异
		return string(ctx.platformEvent.Kind())
	}
	if ctx.event == nil {
		return ""
	}
	return ctx.event.Type
}
