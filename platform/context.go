package platform

import stdctx "context"

// EventContext 是平台无关的事件处理上下文接口。
//
// 替代现有的 *core/context.Context，消除与 dto.Payload 的耦合。
// 框架中间件与 Handler 通过此接口操作事件，无需感知平台细节。
type EventContext interface {
	// StdCtx 返回 Go 标准库 context（用于超时/取消传播）
	StdCtx() stdctx.Context

	// Event 返回当前处理的平台事件
	Event() Event

	// Platform 返回当前事件来源平台标识
	Platform() string

	// Reply 向事件来源会话发送回复消息（快捷方法）
	Reply(msg OutboundMessage) error

	// Send 向指定会话发送消息
	Send(chatID string, msg OutboundMessage) error

	// Get/Set 字符串键值存储（供中间件传递上下文数据）
	Get(key string) (any, bool)
	Set(key string, value any)
}

// baseEventContext 是 EventContext 的基础实现，供各平台适配器嵌入或直接使用
type baseEventContext struct {
	stdCtx stdctx.Context
	event  Event
	sender Sender
	store  map[string]any
}

// NewEventContext 创建基础 EventContext 实现
func NewEventContext(ctx stdctx.Context, event Event, sender Sender) EventContext {
	return &baseEventContext{
		stdCtx: ctx,
		event:  event,
		sender: sender,
		store:  make(map[string]any),
	}
}

func (c *baseEventContext) StdCtx() stdctx.Context { return c.stdCtx }
func (c *baseEventContext) Event() Event           { return c.event }
func (c *baseEventContext) Platform() string       { return c.event.Platform() }

func (c *baseEventContext) Reply(msg OutboundMessage) error {
	chatID := c.event.Chat().ID
	return c.sender.Send(c.stdCtx, chatID, msg)
}

func (c *baseEventContext) Send(chatID string, msg OutboundMessage) error {
	return c.sender.Send(c.stdCtx, chatID, msg)
}

func (c *baseEventContext) Get(key string) (any, bool) {
	v, ok := c.store[key]
	return v, ok
}

func (c *baseEventContext) Set(key string, value any) {
	c.store[key] = value
}
