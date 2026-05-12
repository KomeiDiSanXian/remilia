package wasm

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	corectx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// RegistrationRequest 是 WASM 插件通过 register_command 宿主函数
// 向宿主发起的 Matcher 注册请求。
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

	matcher.SetGroup("wasm:" + b.module.Name())

	mod := b.module
	handlerID := req.HandlerID
	matcher.Handle(func(ctx *corectx.Context) error {
		eventJSON, _ := json.Marshal(map[string]any{
			"content":   ctx.GetMessageContent(),
			"event_id":  ctx.GetPlatformEvent().ID(),
			"sender_id": ctx.GetSenderID(),
			"chat_id":   ctx.GetChatInfo().ID,
			"platform":  ctx.GetEventPlatform(),
		})

		callCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		respJSON, err := mod.CallHandle(callCtx, eventJSON)
		if err != nil {
			ctx.Reply(platform.TextMessage(fmt.Sprintf("插件执行错误: %v", err)))
			return nil
		}
		if respJSON == nil {
			return nil
		}

		var resp struct {
			Reply string `json:"reply,omitempty"`
		}
		if err := json.Unmarshal(respJSON, &resp); err == nil && resp.Reply != "" {
			ctx.Reply(platform.TextMessage(resp.Reply))
		}
		_ = handlerID
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
