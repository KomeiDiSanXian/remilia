package plugin

import (
	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// RegistryWriter Matcher/Command 注册接口
//
// 通过 [SetupContext.Reg] 访问。插件应通过此接口注册命令和事件监听，
// 框架自动为每个 Matcher 设置 Group 和 Source，确保 Disable/Enable 功能正常。
//
// DryRun 模式下，框架注入 [noopRegistryWriter]，所有注册操作均为无副作用的空操作，
// 插件代码无需判断 ctx.DryRun。
type RegistryWriter interface {
	// RegisterCommand 注册命令 Matcher 并自动追踪
	RegisterCommand(eventType dto.EventType, pattern string, extraRules ...context.Rule) *engine.Matcher

	// RegisterMatcher 注册自定义事件 Matcher 并自动追踪
	RegisterMatcher(eventType dto.EventType, rules ...context.Rule) *engine.Matcher
}

// --- 真实实现 ---

// liveRegistryWriter 正常运行阶段的 RegistryWriter，绑定到具体 engine 和 PluginInstance
type liveRegistryWriter struct {
	eng      engine.PluginCoordinator
	name     string
	instance *PluginInstance
}

func newLiveRegistryWriter(eng engine.PluginCoordinator, name string, instance *PluginInstance) RegistryWriter {
	return &liveRegistryWriter{eng: eng, name: name, instance: instance}
}

func (r *liveRegistryWriter) RegisterCommand(eventType dto.EventType, pattern string, extraRules ...context.Rule) *engine.Matcher {
	if r.eng == nil {
		return nil
	}
	matcher := r.eng.OnCommand(eventType, pattern, extraRules...)
	if matcher != nil && r.name != "" {
		matcher.SetGroup(r.name)
		matcher.SetSource("plugin:" + r.name)
		if r.instance != nil {
			r.instance.addMatcher(matcher)
		}
	}
	return matcher
}

func (r *liveRegistryWriter) RegisterMatcher(eventType dto.EventType, rules ...context.Rule) *engine.Matcher {
	if r.eng == nil {
		return nil
	}
	matcher := r.eng.On(eventType, rules...)
	if matcher != nil && r.name != "" {
		matcher.SetGroup(r.name)
		matcher.SetSource("plugin:" + r.name)
		if r.instance != nil {
			r.instance.addMatcher(matcher)
		}
	}
	return matcher
}

// --- DryRun no-op 实现（P2-3）---

// noopRegistryWriter DryRun 模式下的空操作 RegistryWriter
// 所有注册调用均立即返回 nil，无任何副作用。
// 框架内部在 RegisterMultipleV2Smart 依赖推断阶段注入此实现，
// 插件代码无需感知 DryRun，直接使用 ctx.Reg 即可。
type noopRegistryWriter struct{}

func (n *noopRegistryWriter) RegisterCommand(_ dto.EventType, _ string, _ ...context.Rule) *engine.Matcher {
	return nil
}

func (n *noopRegistryWriter) RegisterMatcher(_ dto.EventType, _ ...context.Rule) *engine.Matcher {
	return nil
}
