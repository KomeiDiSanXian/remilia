package plugin

import (
	"fmt"
	"reflect"
)

// proxy.go — Service / TryService：类型安全的跨插件依赖访问。
//
// Service 返回依赖的插件 API，Setup 时一次性解析（非代理）。
// 若依赖插件热加载后需要刷新引用，请使用 OnDependencyReloaded 回调。
//
//	// 按名称
//	svc := plugin.Service[*storage.Plugin](ctx, "storage")
//
//	// 按类型（自动解析，要求类型唯一）
//	svc := plugin.Service[*storage.Plugin](ctx)

// Service 返回服务的类型安全引用。
//
//	name 可选：
//	  - 提供 name 时按名称访问
//	  - 省略 name 时按类型自动解析（要求容器中唯一）
//
// DryRun 阶段返回 zero 值并追踪依赖关系，不触发 panic。
func Service[T any](ctx *SetupContext, names ...string) T {
	if ctx.container == nil {
		panic("plugin.Service: container is nil")
	}
	name := resolveName[T](ctx, names)
	if ctx.DryRun && name == "" {
		var zero T
		return zero
	}
	v := ctx.mustGet(name)
	typed, ok := v.(T)
	if !ok && !ctx.DryRun {
		panic(fmt.Sprintf("plugin.Service[%T](ctx, %q): type mismatch, container has %T", *new(T), name, v))
	}
	return typed
}

// TryService 返回服务的可选引用。
//
//	name 可选（语义同 Service）。
func TryService[T any](ctx *SetupContext, names ...string) (T, bool) {
	if ctx.container == nil {
		var zero T
		return zero, false
	}
	name := resolveOptionalName[T](ctx, names)
	if name == "" {
		var zero T
		return zero, false
	}
	v, ok := ctx.get(name)
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

// resolveName 解析服务名称。未提供 name 时按类型自动解析。
func resolveName[T any](ctx *SetupContext, names []string) string {
	if len(names) > 0 && names[0] != "" {
		return names[0]
	}
	entries := lookupServiceType[T](ctx.container)
	if len(entries) == 0 {
		if ctx.DryRun {
			ctx.pendingTypes = append(ctx.pendingTypes, reflect.TypeFor[T]())
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
