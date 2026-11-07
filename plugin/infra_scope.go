package plugin

import (
	"context"
	"fmt"
	"slices"
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
	// disposing 在 Dispose 开始时置位：此后不再接受新的订阅/钩子/子 Scope。
	// 与 disposed（全部清理完成）分开，因为 Dispose 可能被取消后重试——
	// 期间产生的资源若不拒绝，会错过已拍摄的快照而永久泄漏。
	disposing bool

	// Dispose 的断点续做状态：ctx 取消时保存尚未处理的项，下次调用继续。
	// disposed 只在全部清理完成后置位，避免"已取消 → 永久不再清理"。
	disposeMu         sync.Mutex
	disposeStarted    bool
	unsubscribed      bool
	remainingChildren []*Scope
	remainingHooks    []func() error
}

// Name 返回 Scope 名称（用于调试）。
func (s *Scope) Name() string { return s.name }

// Subscribe 在 EventBus 上订阅，订阅生命周期绑定到本 Scope。
// Scope 被 Dispose 时自动 Unsubscribe；对已 Dispose 的 Scope 订阅会立即
// 取消并返回错误（避免卸载并发窗口内产生无人管理的订阅泄漏）。
func (s *Scope) Subscribe(topic string, handler EventHandler) (Subscription, error) {
	// 先判关闭态再订阅：反过来的话，Dispose 快照拍摄之后登记进来的订阅
	// 永远不会被退订（父 Scope 的 Dispose 只处理快照内的条目）。
	s.mu.Lock()
	closed := s.disposing || s.disposed
	s.mu.Unlock()
	if closed {
		return nil, fmt.Errorf("scope %q already disposed", s.name)
	}

	sub, err := s.ctx.EventBus.Subscribe(topic, handler)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.disposing || s.disposed {
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
	s.mu.Lock()
	closed := s.disposing || s.disposed
	s.mu.Unlock()
	if closed {
		return nil, fmt.Errorf("scope %q already disposed", s.name)
	}

	sub, err := s.ctx.EventBus.SubscribeAll(handler)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.disposing || s.disposed {
		s.mu.Unlock()
		_ = sub.Unsubscribe()
		return nil, fmt.Errorf("scope %q already disposed", s.name)
	}
	s.subscriptions = append(s.subscriptions, sub)
	s.mu.Unlock()
	return sub, nil
}

// OnDispose 注册一个清理回调。Dispose 时按注册顺序逆序调用。
//
// 对已 Dispose 的 Scope 注册：回调不会再被任何后续 Dispose 看到，因此立即
// 就地执行（并告警），而不是静默丢失——卸载窗口里创建的清理钩子仍能得到
// 一次执行机会。
func (s *Scope) OnDispose(fn func() error) {
	if fn == nil {
		return
	}
	s.mu.Lock()
	if s.disposing || s.disposed {
		s.mu.Unlock()
		logger.WithField("scope", s.name).Warn("[Scope] OnDispose registered after dispose; running immediately")
		runDisposeHook(s.name, fn)
		return
	}
	s.disposeHooks = append(s.disposeHooks, fn)
	s.mu.Unlock()
}

// runDisposeHook 执行单个清理回调，并把 panic 转成错误（清理链不应被插件 bug 中断）。
func runDisposeHook(scopeName string, fn func() error) {
	defer func() {
		if r := recover(); r != nil {
			logger.WithFields(logger.Fields{
				"scope": scopeName,
				"panic": r,
			}).Error("[Scope] Panic in dispose hook")
		}
	}()
	if err := fn(); err != nil {
		logger.WithField("scope", scopeName).WithError(err).Warn("[Scope] Dispose hook failed")
	}
}

// Scope 创建子 Scope。子 Scope 的所有资源在父 Scope Dispose 时自动清理。
//
//	name 用于调试标识。返回的 *Scope 可作为子插件的资源容器。
//
// 父已 Dispose 时返回的子 Scope 同样处于"已 Dispose"状态：它不会挂到父的
// children 上（父不会再遍历），若返回普通子 Scope，其订阅将永远无人清理。
func (s *Scope) Scope(name string) *Scope {
	child := &Scope{
		name:   name,
		parent: s,
		ctx:    s.ctx,
	}
	s.mu.Lock()
	if s.disposing || s.disposed {
		s.mu.Unlock()
		logger.WithField("scope", name).
			Warn("[Scope] child scope created after parent disposal; it starts disposed")
		child.disposed = true
		return child
	}
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
	// 串行化同一 Scope 的 Dispose：重试与并发调用都不能重复执行已完成的项。
	s.disposeMu.Lock()
	defer s.disposeMu.Unlock()

	s.mu.Lock()
	if s.disposed {
		s.mu.Unlock()
		return nil
	}
	// 从此刻起拒绝新的订阅/钩子/子 Scope：它们不会出现在下面拍摄的快照里。
	// 只置 disposed 不够——disposed 要等全部清理完成才置位，中途（hook 内、
	// 或 ctx 取消后重试的间隙）新登记的资源会永久泄漏。
	s.disposing = true
	children := s.remainingChildren
	hooks := s.remainingHooks
	if !s.disposeStarted {
		s.disposeStarted = true
		children = append([]*Scope(nil), s.children...)
		hooks = append([]func() error(nil), s.disposeHooks...)
		slices.Reverse(children)
		slices.Reverse(hooks)
	}
	subs := s.subscriptions
	needUnsubscribe := !s.unsubscribed
	s.unsubscribed = true
	s.mu.Unlock()

	// 订阅只退一次：即使本轮被取消，后续重试也不需要重复退订。
	if needUnsubscribe {
		for _, sub := range subs {
			if err := sub.Unsubscribe(); err != nil {
				logger.WithField("scope", s.name).WithError(err).Warn("[Scope] Unsubscribe failed")
			}
			// 等已启动的 handler 结束再清理子 Scope/钩子，避免旧 handler 访问已销毁资源。
			waitSubscription(ctx, sub, s.name)
		}
	}

	// 处理前先把剩余项写回：ctx 取消时下一次调用从这里继续，不会永久丢弃。
	for len(children) > 0 {
		if ctx.Err() != nil {
			s.saveRemainingDispose(children, hooks)
			logger.WithField("scope", s.name).Warn("[Scope] Dispose cancelled during child cleanup")
			return ctx.Err()
		}
		c := children[0]
		children = children[1:]
		s.saveRemainingDispose(children, hooks)
		if err := c.Dispose(ctx); err != nil {
			logger.WithField("scope", s.name).WithError(err).Warn("[Scope] child scope Dispose failed")
		}
	}

	for len(hooks) > 0 {
		if ctx.Err() != nil {
			s.saveRemainingDispose(nil, hooks)
			logger.WithField("scope", s.name).Warn("[Scope] Dispose cancelled during hook execution")
			return ctx.Err()
		}
		hook := hooks[0]
		hooks = hooks[1:]
		s.saveRemainingDispose(nil, hooks)
		runDisposeHook(s.name, hook)
	}

	s.mu.Lock()
	s.disposed = true
	s.remainingChildren = nil
	s.remainingHooks = nil
	s.mu.Unlock()
	return nil
}

// saveRemainingDispose 记录尚未处理的清理项（供取消后重试续做）。
func (s *Scope) saveRemainingDispose(children []*Scope, hooks []func() error) {
	s.mu.Lock()
	s.remainingChildren = children
	s.remainingHooks = hooks
	s.mu.Unlock()
}

// waitSubscription 等待订阅的在途 handler 结束；ctx 取消时放弃等待并告警。
//
// 退订本身不等待（handler 内部退订自身时等待会自死锁），卸载路径在这里补上
// 等待，保证 Teardown 之后不会再有旧 handler 访问已销毁的资源。
func waitSubscription(ctx context.Context, sub Subscription, scope string) {
	w, ok := sub.(interface{ Wait() })
	if !ok {
		return
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		w.Wait()
	}()
	select {
	case <-done:
	case <-ctx.Done():
		logger.WithField("scope", scope).Warn("[Scope] Timed out waiting for in-flight event handlers")
	}
}
