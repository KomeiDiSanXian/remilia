package plugin

import (
	"sync"

	corectx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// scope.go — PluginScope：追踪插件子上下文，卸载时级联清理所有资源。
//
// 设计原则（受 Koishi 启发）：
//   - 每个 Scope 独立追踪其创建的所有资源（subscriptions、middleware、hooks、children）
//   - Scope 被 Dispose 时，逆序清理所有资源，子 Scope 先于父 Scope
//   - 与现有 Teardown 机制互补：Teardown 负责业务清理，Scope 负责框架资源清理

// Scope 插件资源子上下文。
// 通过 [SetupContext.Scope] 创建，生命周期绑定到父插件。
// 父插件卸载时，所有子 Scope 自动级联清理。
type Scope struct {
	name   string
	parent *Scope
	ctx    *SetupContext

	children      []*Scope
	subscriptions []Subscription
	mwResetters   []func()
	disposeHooks  []func() error
	extraKeys     []string

	mu       sync.Mutex
	disposed bool
}

// Name 返回 Scope 名称（用于调试）。
func (s *Scope) Name() string { return s.name }

// Subscribe 在 EventBus 上订阅，订阅生命周期绑定到本 Scope。
// Scope 被 Dispose 时自动 Unsubscribe。
func (s *Scope) Subscribe(topic string, handler EventHandler) (Subscription, error) {
	sub, err := s.ctx.EventBus.Subscribe(topic, handler)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
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

// UseEngineForGroup 注入分组中间件，卸载时自动清除。
// 等价于 ctx.UseEngineForGroup，但 Scope 被 Dispose 时自动调用 ResetGroupMiddleware。
func (s *Scope) UseEngineForGroup(group string, mw ...corectx.Middleware) {
	if s.ctx == nil || group == "" || len(mw) == 0 {
		return
	}
	s.ctx.UseEngineForGroup(group, mw...)

	s.mu.Lock()
	s.mwResetters = append(s.mwResetters, func() {
		s.ctx.NewGroupMiddlewareResetter()(group)
	})
	s.mu.Unlock()
}

// ExportAs 注册额外的容器导出项，Scope 被 Dispose 时自动从容器中移除。
func (s *Scope) ExportAs(key string, api any) {
	if s.ctx == nil || key == "" {
		return
	}
	s.ctx.ExportAs(key, api)

	s.mu.Lock()
	s.extraKeys = append(s.extraKeys, key)
	s.mu.Unlock()
}

// Dispose 清理 Scope 及其所有子 Scope 的资源。
// 清理顺序：children（逆序）→ 当前 Scope 的资源（逆序）
func (s *Scope) Dispose() error {
	s.mu.Lock()
	if s.disposed {
		s.mu.Unlock()
		return nil
	}
	s.disposed = true
	children := s.children
	subs := s.subscriptions
	hooks := s.disposeHooks
	resetters := s.mwResetters
	keys := s.extraKeys
	s.mu.Unlock()

	// 逆序销毁子 Scope（深度优先）
	for i := len(children) - 1; i >= 0; i-- {
		if err := children[i].Dispose(); err != nil {
			logger.WithField("scope", s.name).WithError(err).Warn("[Scope] child scope Dispose failed")
		}
	}

	// 清理 EventBus 订阅
	for _, sub := range subs {
		if err := sub.Unsubscribe(); err != nil {
			logger.WithField("scope", s.name).WithError(err).Warn("[Scope] Unsubscribe failed")
		}
	}

	// 清理引擎中间件
	for _, reset := range resetters {
		reset()
	}

	// 清理容器导出项
	if s.ctx != nil && s.ctx.container != nil {
		for _, key := range keys {
			s.ctx.container.Remove(key)
		}
	}

	// 执行用户注册的清理回调（逆序）
	for i := len(hooks) - 1; i >= 0; i-- {
		if err := hooks[i](); err != nil {
			logger.WithField("scope", s.name).WithError(err).Warn("[Scope] Dispose hook failed")
		}
	}

	return nil
}
