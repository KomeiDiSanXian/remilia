package plugin

import (
	"github.com/KomeiDiSanXian/remilia/command"
	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
)

// RegistryWriter Matcher/Command 注册接口
//
// 通过 [SetupContext.Reg] 访问。插件应通过此接口注册命令和事件监听，
// 框架自动为每个 Matcher 设置 Group 和 Source，确保 Disable/Enable 功能正常。
//
// DryRun 模式下，框架注入 [noopRegistryWriter]，所有注册操作均为无副作用的空操作，
// 插件代码无需判断 ctx.DryRun。
//
// eventType 为平台无关的事件类型字符串（如 "C2C_MESSAGE_CREATE"）或 dto.EventType 常量，
// 传入空字符串 "" 表示通配所有事件类型。
type RegistryWriter interface {
	// RegisterCommand 注册命令 Matcher 并自动追踪
	RegisterCommand(eventType string, pattern string, extraRules ...context.Rule) *engine.Matcher

	// RegisterMatcher 注册自定义事件 Matcher 并自动追踪
	RegisterMatcher(eventType string, rules ...context.Rule) *engine.Matcher
}

// --- 真实实现 ---

// liveRegistryWriter 正常运行阶段的 RegistryWriter，绑定到具体 engine 和 Instance
type liveRegistryWriter struct {
	eng      engine.PluginCoordinator
	name     string
	instance *Instance
}

func newLiveRegistryWriter(eng engine.PluginCoordinator, name string, instance *Instance) RegistryWriter {
	return &liveRegistryWriter{eng: eng, name: name, instance: instance}
}

func (r *liveRegistryWriter) RegisterCommand(eventType string, pattern string, extraRules ...context.Rule) *engine.Matcher {
	if r.eng == nil {
		return nil
	}
	matcher := r.eng.OnCommand(eventType, pattern, extraRules...)
	if matcher != nil && r.name != "" {
		// SetMatcherGroup 同步更新 engine 内部的 groupIndex，
		// 确保 RemoveGroup/DisableGroup/EnableGroup 能正确找到此 Matcher。
		r.eng.SetMatcherGroup(matcher, r.name, "plugin:"+r.name)
		if r.instance != nil {
			r.instance.addMatcher(matcher)
		}
		// 注入插件感知的别名自动注册回调。
		// 回调在 Matcher.Handle() 首次被调用时触发，为 definition.Aliases 中的每个别名
		// 自动注册独立的路由 Matcher（Hidden=true，不出现在命令列表）。
		// 别名 Matcher 与主命令共享相同的 Group/Source 和 instance 追踪，
		// 从而支持插件级别的 Disable/Enable 联动。
		// 传入 extraRules 确保别名 Matcher 具有与主命令相同的额外规则（如权限检查）。
		r.injectAliasRegistrar(matcher, eventType, extraRules)
	}
	return matcher
}

// injectAliasRegistrar 向 matcher 注入别名自动注册回调。
// 当 matcher.Handle() 被调用且 definition.Aliases 非空时触发一次。
func (r *liveRegistryWriter) injectAliasRegistrar(primary *engine.Matcher, eventType string, extraRules []context.Rule) {
	primary.SetAliasRegistrar(func(def *command.Definition, handler context.Handler) {
		if def == nil || len(def.Aliases) == 0 {
			return
		}
		primaryCmd := "/" + def.Name
		for _, alias := range def.Aliases {
			aliasPattern := "/" + alias
			// 冲突检测：若别名已被其他命令（非当前主命令）占用则跳过
			if existing := r.eng.FindCommand(alias); existing != nil && existing.Command != primaryCmd {
				continue
			}
			// 通过 eng.OnCommand 注册别名路由
			aliasMatcher := r.eng.OnCommand(eventType, aliasPattern, extraRules...)
			if aliasMatcher == nil {
				continue
			}
			// 别名 Matcher 与主命令同 Group/Source，以支持 Disable/Enable 联动
			// SetMatcherGroup 同步更新 groupIndex，确保 RemoveGroup 能找到别名 Matcher。
			r.eng.SetMatcherGroup(aliasMatcher, primary.GetGroup(), primary.GetSource())
			// Hidden=true：不出现在 GetAllCommands() / /help 命令列表中
			aliasMatcher.SetDefinition(&command.Definition{Name: alias, Hidden: true})
			// 与主命令共享同一 handler
			aliasMatcher.Handle(handler)
			// 注册到插件实例以便生命周期管理
			if r.instance != nil {
				r.instance.addMatcher(aliasMatcher)
			}
		}
	})
}

func (r *liveRegistryWriter) RegisterMatcher(eventType string, rules ...context.Rule) *engine.Matcher {
	if r.eng == nil {
		return nil
	}
	matcher := r.eng.On(eventType, rules...)
	if matcher != nil && r.name != "" {
		// SetMatcherGroup 同步更新 engine 内部的 groupIndex，
		// 确保 RemoveGroup/DisableGroup/EnableGroup 能正确找到此 Matcher。
		r.eng.SetMatcherGroup(matcher, r.name, "plugin:"+r.name)
		if r.instance != nil {
			r.instance.addMatcher(matcher)
		}
	}
	return matcher
}

// --- DryRun no-op 实现（P2-3）---

// noopRegistryWriter DryRun 模式下的空操作 RegistryWriter
// 所有注册调用均立即返回 nil，无任何副作用。
// 框架内部在 RegisterMultipleSmart 依赖推断阶段注入此实现，
// 插件代码无需感知 DryRun，直接使用 ctx.Reg 即可。
type noopRegistryWriter struct{}

func (n *noopRegistryWriter) RegisterCommand(_ string, _ string, _ ...context.Rule) *engine.Matcher {
	return nil
}

func (n *noopRegistryWriter) RegisterMatcher(_ string, _ ...context.Rule) *engine.Matcher {
	return nil
}
