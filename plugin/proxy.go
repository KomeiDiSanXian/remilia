package plugin

import "fmt"

// proxy.go — ServiceProxy：防过期的插件间同步调用代理。
//
// 问题：插件 A 在 Setup 中调用 plugin.Service[B](ctx, "B") 获取 *B，但 B 热重载后
// A 持有的指针已过时。当前通过 OnDependencyReloaded 回调手动刷新，但容易遗漏。
//
// 解决方案：ServiceProxy 每次调用时从 Container 动态解析最新实现。
//
// 用法：
//
//	storageSvc := plugin.Service[*storage.Plugin](ctx, "storage")
//
//	// 运行时调用（总是拿到最新实现）
//	if s, ok := storageSvc.Get(); ok { s.DoSomething() }
//	storageSvc.Do(func(s *storage.Plugin) { s.Set("key", "value") })

// ServiceProxy 防过期的插件服务代理。
// T 是存储在 Container 中的类型（通常为 *SomePlugin）。
// 每次操作都从容器中动态获取最新实现，热重载后自动指向新实例。
type ServiceProxy[T any] struct {
	container *Container
	name      string
}

// Service 从容器中获取指定插件的服务代理。
// 若插件未注册，panic（与 Must 语义一致）。
// 自动记录必要依赖关系，用于 Smart 注册的依赖推断。
func Service[T any](ctx *SetupContext, name string) *ServiceProxy[T] {
	if ctx.container == nil {
		panic("plugin.Service: container is nil (possibly in DryRun phase)")
	}
	ctx.mustGet(name) // 验证存在并追踪必要依赖
	return &ServiceProxy[T]{
		container: ctx.container,
		name:      name,
	}
}

// TryService 返回服务的可选代理。若插件未注册则返回 nil, false（不 panic）。
// 自动记录可选依赖关系，用于 Smart 注册的依赖推断。
func TryService[T any](ctx *SetupContext, name string) (*ServiceProxy[T], bool) {
	if ctx.container == nil {
		return nil, false
	}
	if _, ok := ctx.get(name); !ok { // get 追踪可选依赖
		return nil, false
	}
	return &ServiceProxy[T]{
		container: ctx.container,
		name:      name,
	}, true
}

// Name 返回服务名称。
func (s *ServiceProxy[T]) Name() string { return s.name }

// Get 返回服务的当前最新实现。若服务已被卸载或类型不匹配，返回 nil, false。
func (s *ServiceProxy[T]) Get() (T, bool) {
	v, ok := s.container.Get(s.name)
	if !ok {
		var zero T
		return zero, false
	}
	typed, ok := v.(T)
	if !ok {
		var zero T
		return zero, false
	}
	return typed, true
}

// Must 返回服务的当前最新实现。服务不存在时 panic。
func (s *ServiceProxy[T]) Must() T {
	v, ok := s.Get()
	if !ok {
		panic("plugin.ServiceProxy: dependency " + s.name + " is no longer available")
	}
	return v
}

// Call 在服务的当前最新实现上调用 fn，返回 fn 的错误。
// 服务不可用时返回 PluginError（可用 errors.Is 匹配）。
func (s *ServiceProxy[T]) Call(fn func(T) error) error {
	v, ok := s.Get()
	if !ok {
		return &PluginError{
			PluginName: s.name,
			Operation:  "call",
			Cause:      fmt.Errorf("dependency not available"),
		}
	}
	return fn(v)
}

// Do 在服务的当前最新实现上执行操作。服务不可用时静默跳过。
func (s *ServiceProxy[T]) Do(fn func(T)) {
	v, ok := s.Get()
	if !ok {
		return
	}
	fn(v)
}

// MustDo 与 Do 相同，但服务不可用时 panic。
func (s *ServiceProxy[T]) MustDo(fn func(T)) {
	fn(s.Must())
}
