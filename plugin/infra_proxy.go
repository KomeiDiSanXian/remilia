package plugin

import (
	"fmt"
	"reflect"
)

// proxy.go — ServiceProxy：防过期的插件间同步调用代理。
//
// 用法：
//
//	// 按名称（原语义）
//	svc := plugin.Service[*storage.Plugin](ctx, "storage")
//
//	// 按类型（自动解析，需要类型唯一）
//	svc := plugin.Service[*storage.Plugin](ctx)

// ServiceProxy 防过期的插件服务代理。
type ServiceProxy[T any] struct {
	container *Container
	name      string
}

// Service 获取服务的类型安全代理。
//
//	name 可选：
//	  - 提供 name 时按名称访问（原语义）
//	  - 省略 name 时按类型自动解析（要求容器中唯一）
func Service[T any](ctx *SetupContext, names ...string) *ServiceProxy[T] {
	if ctx.container == nil {
		panic("plugin.Service: container is nil")
	}
	name := resolveName[T](ctx, names)
	if ctx.DryRun && name == "" {
		// 三色 DryRun：类型尚未就绪，跳过 mustGet（pendingType 已记录）
		return &ServiceProxy[T]{container: ctx.container, name: name}
	}
	v := ctx.mustGet(name)
	if !ctx.DryRun {
		if _, ok := v.(T); !ok {
			panic(fmt.Sprintf("plugin.Service[%T](ctx, %q): type mismatch, container has %T", *new(T), name, v))
		}
	}
	return &ServiceProxy[T]{container: ctx.container, name: name}
}

// TryService 返回服务的可选代理。
//
//	name 可选（语义同 Service）。
func TryService[T any](ctx *SetupContext, names ...string) (*ServiceProxy[T], bool) {
	if ctx.container == nil {
		return nil, false
	}
	name := resolveOptionalName[T](ctx, names)
	if name == "" {
		return nil, false
	}
	v, ok := ctx.get(name)
	if !ok {
		return nil, false
	}
	if !ctx.DryRun {
		if _, ok := v.(T); !ok {
			ctx.Log.Warnf("TryService[%T](ctx, %q): type mismatch, container has %T, returning nil", *new(T), name, v)
			return nil, false
		}
	}
	return &ServiceProxy[T]{container: ctx.container, name: name}, true
}

// resolveName 解析服务名称。未提供 name 时按类型自动解析。
func resolveName[T any](ctx *SetupContext, names []string) string {
	if len(names) > 0 && names[0] != "" {
		return names[0]
	}
	entries := lookupServiceType[T](ctx.container)
	if len(entries) == 0 {
		if ctx.DryRun {
			// 三色 DryRun：记录 pending 类型，等待后续轮次匹配新注册的 API
			ctx.pendingType = reflect.TypeFor[T]()
			return ""
		}
		panic(fmt.Sprintf("plugin.Service[%T](ctx): no service of this type is registered. "+
			"Use Service[T](ctx, name) to disambiguate, or check that the dependency is loaded.", *new(T)))
	}
	if len(entries) > 1 {
		panic(fmt.Sprintf("plugin.Service[%T]: ambiguous, %d services found: %v; use Service[T](ctx, name)", *new(T), len(entries), entryNames(entries)))
	}
	return entries[0].name
}

// resolveOptionalName 解析可选名称。未匹配时返回 ""。
func resolveOptionalName[T any](ctx *SetupContext, names []string) string {
	if len(names) > 0 && names[0] != "" {
		return names[0]
	}
	entries := lookupServiceType[T](ctx.container)
	if len(entries) == 0 {
		return ""
	}
	return entries[0].name
}

func entryNames(entries []*serviceEntry) []string {
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.name
	}
	return names
}

// Name 返回服务名称。
func (s *ServiceProxy[T]) Name() string { return s.name }

// Get 返回当前最新实现。已卸载或类型不匹配时返回 nil, false。
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

// Must 返回当前最新实现。不存在时 panic。
func (s *ServiceProxy[T]) Must() T {
	v, ok := s.Get()
	if !ok {
		panic("plugin.ServiceProxy: dependency " + s.name + " is no longer available")
	}
	return v
}

// Call 在实现上调用 fn。
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

// Do 在实现上执行操作。不可用时静默跳过。
func (s *ServiceProxy[T]) Do(fn func(T)) {
	v, ok := s.Get()
	if !ok {
		return
	}
	fn(v)
}

// MustDo 与 Do 相同，不可用时 panic。
func (s *ServiceProxy[T]) MustDo(fn func(T)) {
	fn(s.Must())
}
