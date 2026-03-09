package platform

import (
	stdctx "context"
	"fmt"
	"sync"
)

// Sender 是平台无关的消息发送接口。
//
// 各平台适配器实现此接口，将 OutboundMessage 转换并发送到目标会话。
type Sender interface {
	// Send 发送消息到指定会话
	//
	// chatID 为目标会话 ID（私聊为用户 openID，群聊为群 ID）
	Send(ctx stdctx.Context, chatID string, msg OutboundMessage) error
}

// NoopSender 空实现，用于测试或不需要发送能力的场景
type NoopSender struct{}

// Send 什么也不做，始终返回 nil
func (n *NoopSender) Send(_ stdctx.Context, _ string, _ OutboundMessage) error {
	return nil
}

// PlatformAdapter 是平台适配器的核心接口。
//
// 每个平台（QQ、Discord、Telegram 等）实现此接口，
// 框架核心通过此接口接收事件和发送消息，不依赖任何平台 SDK。
//
// 生命周期：
//
//	Start() ──→ [事件循环，持续调用 handler] ──→ Stop()
type PlatformAdapter interface {
	// Platform 返回平台标识符（小写，如 "qq"、"discord"、"telegram"）
	Platform() string

	// StartPlatform 启动适配器事件循环（阻塞，直到 ctx 取消或出错）
	//
	// 每收到一个事件，调用 handler(event)。
	// handler 应快速返回（框架内部会在 goroutine 中处理）。
	StartPlatform(ctx stdctx.Context, handler func(Event)) error

	// Stop 优雅停止适配器
	Stop(ctx stdctx.Context) error

	// Sender 返回该平台的消息发送接口
	Sender() Sender
}

// Registry 多平台适配器注册表。
//
// 支持同时运行多个平台适配器，框架通过 Registry 管理它们的生命周期。
type Registry struct {
	mu       sync.RWMutex
	adapters map[string]PlatformAdapter
}

// NewRegistry 创建空的适配器注册表
func NewRegistry() *Registry {
	return &Registry{
		adapters: make(map[string]PlatformAdapter),
	}
}

// Register 注册一个平台适配器
//
// 若同一平台已注册，会覆盖旧适配器。
func (r *Registry) Register(adapter PlatformAdapter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters[adapter.Platform()] = adapter
}

// Get 获取指定平台的适配器
func (r *Registry) Get(platform string) (PlatformAdapter, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.adapters[platform]
	return a, ok
}

// All 返回所有已注册适配器的快照（切片顺序不保证）
func (r *Registry) All() []PlatformAdapter {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]PlatformAdapter, 0, len(r.adapters))
	for _, a := range r.adapters {
		out = append(out, a)
	}
	return out
}

// StartAll 并发启动所有已注册平台适配器
//
// 每个适配器在独立 goroutine 中运行，ctx 取消时所有适配器退出。
// handler 会收到来自所有平台的事件。
func (r *Registry) StartAll(ctx stdctx.Context, handler func(Event)) error {
	adapters := r.All()
	if len(adapters) == 0 {
		return fmt.Errorf("platform registry: no adapters registered")
	}

	errCh := make(chan error, len(adapters))
	for _, a := range adapters {
		go func() {
			if err := a.StartPlatform(ctx, handler); err != nil {
				errCh <- fmt.Errorf("platform %s: %w", a.Platform(), err)
			}
		}()
	}

	// 等待 ctx 取消
	<-ctx.Done()
	return nil
}

// StopAll 依次停止所有已注册平台适配器，收集错误
func (r *Registry) StopAll(ctx stdctx.Context) error {
	adapters := r.All()
	var errs []error
	for _, a := range adapters {
		if err := a.Stop(ctx); err != nil {
			errs = append(errs, fmt.Errorf("platform %s stop: %w", a.Platform(), err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("platform registry stop errors: %v", errs)
	}
	return nil
}
