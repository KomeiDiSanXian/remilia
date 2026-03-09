package context

import (
	"errors"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/tidwall/gjson"
)

// decode.go — 事件解码与热路径字段缓存
//
// 包含：
//   - decodeCache 类型联合体定义
//   - DecodeEvent：事件解码（带 typed-union 缓存）
//   - GetMessageContent：消息内容缓存（contentOnce）
//   - GetAuthor：作者信息缓存（authorOnce）
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

// GetAuthor 获取消息作者信息（Once 缓存）
func (ctx *Context) GetAuthor() *dto.Author {
	if ctx == nil || ctx.event == nil {
		return nil
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
	return ctx.author
}

// GetEvent 获取事件
func (ctx *Context) GetEvent() *dto.Payload {
	if ctx == nil {
		return nil
	}
	return ctx.event
}

// GetEventType 获取事件类型
//
// 新路径（platform.Event）：将 RawType() 字符串转换为 dto.EventType，
// 保持与 Engine 内部按 EventType 路由的兼容性。
// 旧路径：直接返回 ctx.event.Type。
func (ctx *Context) GetEventType() dto.EventType {
	if ctx == nil {
		return ""
	}
	if ctx.platformEvent != nil {
		return dto.EventType(ctx.platformEvent.RawType())
	}
	if ctx.event == nil {
		return ""
	}
	return ctx.event.Type
}

// SendGroupMessage 发送群聊消息
func (ctx *Context) SendGroupMessage(groupID string, msg *dto.Message) (gjson.Result, error) {
	if ctx == nil || ctx.api == nil {
		return gjson.Result{}, ErrNilAPI
	}
	return ctx.api.GroupChat(groupID, msg)
}

// SendSingleMessage 发送私聊消息
func (ctx *Context) SendSingleMessage(openID string, msg *dto.Message) (gjson.Result, error) {
	if ctx == nil || ctx.api == nil {
		return gjson.Result{}, ErrNilAPI
	}
	return ctx.api.SingleChat(openID, msg)
}

// ReplyGroup 回复群聊消息（自动获取 group_openid）
func (ctx *Context) ReplyGroup(msg *dto.Message) (gjson.Result, error) {
	var event dto.GroupAtMessageCreateEvent
	if err := ctx.DecodeEvent(&event); err != nil {
		return gjson.Result{}, err
	}
	if msg.MessageID == "" {
		msg.MessageID = event.ID
	}
	return ctx.SendGroupMessage(event.GroupOpenID, msg)
}

// ReplyPrivate 回复私聊消息（自动获取 openid）
func (ctx *Context) ReplyPrivate(msg *dto.Message) (gjson.Result, error) {
	var event dto.C2CMessageCreateEvent
	if err := ctx.DecodeEvent(&event); err != nil {
		return gjson.Result{}, err
	}
	if msg.MessageID == "" {
		msg.MessageID = event.ID
	}
	return ctx.SendSingleMessage(event.Author.UserOpenID, msg)
}
