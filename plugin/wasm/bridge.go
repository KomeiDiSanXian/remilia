package wasm

import (
	"context"
	"fmt"
	"sync"

	corectx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// RegistrationRequest WASM 插件向宿主发起 Matcher 注册请求的参数。
type RegistrationRequest struct {
	EventType string `json:"event_type"`
	Command   string `json:"command,omitempty"`
	HandlerID int32  `json:"handler_id"`
}

// Bridge 连接 WASM 模块与 Engine，将 WASM 插件的注册请求
// 转换为 Engine 的 Matcher。
type Bridge struct {
	mu       sync.Mutex
	module   *Module
	engine   engine.MatcherWriter
	matchers []*engine.Matcher
}

// NewBridge 创建一个连接 WASM 模块与 Engine 的桥接器。
func NewBridge(mod *Module, eng engine.MatcherWriter) *Bridge {
	return &Bridge{
		module:   mod,
		engine:   eng,
		matchers: make([]*engine.Matcher, 0),
	}
}

// RegisterCommand 处理 WASM 插件的命令注册请求，在 Engine 上创建 Matcher。
func (b *Bridge) RegisterCommand(req RegistrationRequest) (*engine.Matcher, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	matcher := b.engine.OnCommand(req.EventType, req.Command)
	if matcher == nil {
		return nil, fmt.Errorf("wasm: failed to create matcher for %q", req.Command)
	}

	// 使用 engine.SetMatcherGroup（同步更新 groupIndex），
	// 使 DisableGroup/RemoveGroup("wasm:<name>") 能正确找到这些 matcher。
	b.engine.SetMatcherGroup(matcher, "wasm:"+b.module.Name(), "wasm:"+b.module.Name())

	mod := b.module
	matcher.Handle(func(ctx *corectx.Context) error {
		// 使用 TLV 编码事件
		eventTLV := NewTLVBuilder().
			WriteString("c", ctx.GetMessageContent()).
			WriteString("s", ctx.GetSenderID()).
			WriteString("p", ctx.GetEventPlatform()).
			WriteString("i", ctx.GetChatInfo().ID).
			WriteString("t", chatTypeString(ctx.GetChatInfo().IsGroup)).
			WriteString("e", ctx.GetPlatformEvent().ID()).
			Bytes()

		to := mod.callTimeout()
		callCtx, cancel := context.WithTimeout(context.Background(), to)
		defer cancel()

		respTLV, err := mod.CallHandle(callCtx, eventTLV)
		if err != nil {
			ctx.Reply(platform.TextMessage(fmt.Sprintf("插件执行错误: %v", err)))
			return nil
		}
		if respTLV == nil {
			return nil
		}

		reply := NewTLVReader(respTLV).ReadString("r")
		if reply != "" {
			ctx.Reply(platform.TextMessage(reply))
		}
		return nil
	})

	b.matchers = append(b.matchers, matcher)
	return matcher, nil
}

// Cleanup 移除 Bridge 创建的所有 Matcher。
func (b *Bridge) Cleanup() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, m := range b.matchers {
		m.Delete()
	}
	b.matchers = b.matchers[:0]
}

func chatTypeString(isGroup bool) string {
	if isGroup {
		return "group"
	}
	return "private"
}
