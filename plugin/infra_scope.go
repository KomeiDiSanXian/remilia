package plugin

import (
	"context"
	"fmt"
	"sync"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// Scope 插件资源子上下文。
// 追踪 EventBus 订阅和用户注册的清理回调（dispose hooks），
// 支持树形结构（子 Scope 在父 Scope Dispose 时自动级联清理）。
// 与 Teardown 互补：Teardown 负责业务清理，Scope 负责框架资源清理。
type Scope struct {
	name   string
	parent *Scope
	ctx    *SetupContext

	children      []*Scope
	subscriptions []Subscription
	disposeHooks  []func() error

	mu       sync.Mutex
	disposed bool
}

// Name 返回 Scope 名称（用于调试）。
func (s *Scope) Name() string { return s.name }

// Subscribe 在 EventBus 上订阅，订阅生命周期绑定到本 Scope。
// Scope 被 Dispose 时自动 Unsubscribe；对已 Dispose 的 Scope 订阅会立即
// 取消并返回错误（避免卸载并发窗口内产生无人管理的订阅泄漏）。
func (s *Scope) Subscribe(topic string, handler EventHandler) (Subscription, error) {
	sub, err := s.ctx.EventBus.Subscribe(topic, handler)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.disposed {
		s.mu.Unlock()
		_ = sub.Unsubscribe()
		return nil, fmt.Errorf("scope %q already disposed", s.name)
	}
	s.subscriptions = append(s.subscriptions, sub)
	s.mu.Unlock()
	return sub, nil
}

// SubscribeAll 在 EventBus 上订阅所有事件，生命周期绑定到本 Scope。
func (s *Scope) SubscribeAll(handler EventHandler) (Subscription, error) {
	sub, err := s.ctx.EventBus.SubscribeAll(handler)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.disposed {
		s.mu.Unlock()
		_ = sub.Unsubscribe()
		return nil, fmt.Errorf("scope %q already disposed", s.name)
	}
	s.subscriptions = append(s.subscriptions, sub)
	s.mu.Unlock()
	return sub, nil
}

// OnDispose 注册一个清理回调。Dispose 时按注册顺序逆序调用。
func (s *Scope) OnDispose(fn func() error) {
	s.mu.Lock()
	s.disposeHooks = append(s.disposeHooks, fn)
	s.mu.Unlock()
}

// Scope 创建子 Scope。子 Scope 的所有资源在父 Scope Dispose 时自动清理。
//
//	name 用于调试标识。返回的 *Scope 可作为子插件的资源容器。
func (s *Scope) Scope(name string) *Scope {
	child := &Scope{
		name:   name,
		parent: s,
		ctx:    s.ctx,
	}
	s.mu.Lock()
	s.children = append(s.children, child)
	s.mu.Unlock()
	return child
}

// Dispose 清理 Scope 及其所有子 Scope 的资源。
// ctx 用于超时/取消控制：ctx 到期时跳过剩余的 children/hooks 清理并返回 ctx.Err()，
// 但 EventBus 订阅退订（纯框架操作，无用户代码）无论如何都会执行——
// 否则被取消的 Dispose 已置 disposed=true，这些订阅将永远无人清理。
// 清理顺序：children（逆序）→ hooks（逆序）→ subscriptions
func (s *Scope) Dispose(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	if s.disposed {
		s.mu.Unlock()
		return nil
	}
	s.disposed = true
	children := s.children
	subs := s.subscriptions
	hooks := s.disposeHooks
	s.mu.Unlock()

	// 订阅退订放在 defer 中，保证即使 children/hooks 阶段因 ctx 取消提前返回也会执行
	defer func() {
		for _, sub := range subs {
			if err := sub.Unsubscribe(); err != nil {
				logger.WithField("scope", s.name).WithError(err).Warn("[Scope] Unsubscribe failed")
			}
		}
	}()

	// 逆序销毁子 Scope（深度优先）
	for i := len(children) - 1; i >= 0; i-- {
		select {
		case <-ctx.Done():
			logger.WithField("scope", s.name).Warn("[Scope] Dispose cancelled during child cleanup")
			return ctx.Err()
		default:
		}
		if err := children[i].Dispose(ctx); err != nil {
			logger.WithField("scope", s.name).WithError(err).Warn("[Scope] child scope Dispose failed")
		}
	}

	// 执行用户注册的清理回调（逆序）
	for i := len(hooks) - 1; i >= 0; i-- {
		select {
		case <-ctx.Done():
			logger.WithField("scope", s.name).Warn("[Scope] Dispose cancelled during hook execution")
			return ctx.Err()
		default:
		}
		if err := hooks[i](); err != nil {
			logger.WithField("scope", s.name).WithError(err).Warn("[Scope] Dispose hook failed")
		}
	}

	return nil
}
