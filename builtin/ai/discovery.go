// Package ai discovery.go — 工具与技能（Skill）的自动发现与注册。
//
// 本文件实现两种自动发现机制：
//  1. 工具发现（discoverTools）：在 Setup 阶段扫描所有已注册的**无权限**命令，
//     自动将其包装为 Tool 供 LLM 调用。跳过 AI 自身命令、隐藏命令、需权限命令。
//  2. 技能发现（DiscoverSkillProviders）：在 FreezeContainer 后扫描所有插件服务，
//     实现 SkillProvider 接口的插件自动注册其 Skill。
//
// 同样适用于 ToolProvider 接口的显式注册模式。
package ai

import (
	"context"
	"fmt"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/catalog"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// DiscoverCommands 扫描当前所有已注册的无权限命令。
// excludePlugins 为已提供显式 AI 工具（ToolProvider/SkillProvider）的插件名，
// 这些插件的命令不再自动发现为工具（避免与结构化工具重复）。
// 应在插件容器冻结后、开始处理平台事件前调用。
func (p *Plugin) DiscoverCommands(excludePlugins ...string) {
	if p.coord == nil {
		return
	}
	excludeSet := make(map[string]struct{}, len(excludePlugins))
	for _, name := range excludePlugins {
		if name != "" {
			excludeSet[name] = struct{}{}
		}
	}
	catalog.Discover(p.coord, catalog.Options{
		Allowlist:      p.cfg.ToolAllowlist,
		TriggerCmd:     p.cfg.TriggerCmd,
		ExcludePlugins: excludeSet,
	}, pluginCatalogSink{p: p})
}

// pluginCatalogSink 把自动发现的结果写回插件目录：动作进注册表，
// 命令模式进执行期映射（发现与执行可能并行，故加锁）。
type pluginCatalogSink struct{ p *Plugin }

// Has 报告动作名是否已存在（显式注册优先于自动发现）。
func (s pluginCatalogSink) Has(name string) bool {
	_, exists := s.p.reg.Get(name)
	return exists
}

// Add 登记自动发现的命令动作，并记下它对应的命令模式。
func (s pluginCatalogSink) Add(tool toolkit.Tool, commandPattern string) {
	s.p.cmdMu.Lock()
	s.p.cmdPatterns[tool.Name] = commandPattern
	s.p.cmdMu.Unlock()
	s.p.reg.Register(tool)
}

// RegisterToolProvider 注册一个实现了 ToolProvider 接口的插件所提供的工具集。
//
// 其他插件可在自己的 Setup 中通过 ctx.TryService 获取 AI 插件的服务实例
// 后调用此方法注册自定义工具，尤其是需要权限校验的敏感命令。
//
// 显式注册优先于自动发现：若工具名与自动发现的命令工具重名，
// 会先移除自动发现的版本及其命令映射，保证 LLM 调用的是插件
// 自己实现的 Execute，而非通过合成事件触发真实命令。
//
// 使用示例：
//
//	if aiSvc, ok := ctx.TryService[*ai.Plugin]("ai"); ok {
//	    aiSvc.RegisterToolProvider(myToolProvider)
//	}
func (c *catalogState) RegisterToolProvider(tp toolkit.ToolProvider) {
	for _, t := range tp.ListTools() {
		if t.Name == "" {
			continue
		}
		c.reg.Remove(t.Name)
		c.cmdMu.Lock()
		delete(c.cmdPatterns, t.Name)
		c.cmdMu.Unlock()
		c.reg.Register(t)
	}
}

// DiscoverToolProviders 扫描插件管理器中所有已注册的插件服务，
// 自动发现实现了 [ToolProvider] 接口的插件并注册其工具。
// 应在 [plugin.Manager.FreezeContainer] 之后调用。
func (c *catalogState) DiscoverToolProviders(mgr *plugin.Manager) {
	for _, name := range mgr.List() {
		svc, ok := mgr.GetContainer().Get(name)
		if !ok || svc == nil {
			continue
		}
		tp, ok := svc.(toolkit.ToolProvider)
		if !ok {
			continue
		}
		c.RegisterToolProvider(tp)
	}
}

// RegisterSkill 注册一个系统级 Skill。
// OwnerID 为空时自动设为 OwnerSystem。
// Skill 会自动包装为 Tool 供 LLM 发现和调用。
// 如果 Parameters 为空，自动使用 {"query": string} 作为默认参数。
func (p *Plugin) RegisterSkill(s toolkit.Skill) {
	s = catalog.SystemSkill(s)
	p.skillReg.Register(s)
	p.registerSkillAsTool(s)
}

// RegisterUserSkill 注册一个用户自定义 Skill。
// name 会自动添加 u_ 前缀，OwnerID 设为 ownerID。
// 注册到 skillReg 但不注册到全局 ToolRegistry（由 processWithTools 按会话注入）。
func (p *Plugin) RegisterUserSkill(s toolkit.Skill, ownerID string) error {
	s, err := catalog.UserSkill(s, ownerID)
	if err != nil {
		return err
	}

	userSkills := p.skillReg.ListByOwner(ownerID)
	if len(userSkills) >= p.cfg.MaxUserSkills {
		return fmt.Errorf("达到技能数量上限 (%d)，请先删除一个再添加", p.cfg.MaxUserSkills)
	}

	if len(s.Prompt) > p.cfg.MaxUserSkillPromptLen {
		return fmt.Errorf("技能 Prompt 过长（%d > %d），请缩短", len(s.Prompt), p.cfg.MaxUserSkillPromptLen)
	}

	return p.skillReg.Add(s)
}

func (p *Plugin) registerSkillAsTool(s toolkit.Skill) {
	skill := s
	p.reg.Register(catalog.ToolFromSkill(skill, func(ctx context.Context, args map[string]any) (string, error) {
		return p.executeSkill(ctx, skill, args)
	}))
}

// RegisterSkillProvider 注册一个实现了 SkillProvider 接口的插件所提供的技能集。
func (p *Plugin) RegisterSkillProvider(sp toolkit.SkillProvider) {
	for _, s := range sp.ListSkills() {
		p.RegisterSkill(s)
	}
}

// DiscoverSkillProviders 扫描插件管理器中所有已注册的插件服务，
// 自动发现实现了 [SkillProvider] 接口的插件并注册其技能。
// 应在 [plugin.Manager.FreezeContainer] 之后调用。
func (p *Plugin) DiscoverSkillProviders(mgr *plugin.Manager) {
	for _, name := range mgr.List() {
		svc, ok := mgr.GetContainer().Get(name)
		if !ok || svc == nil {
			continue
		}
		if sp, ok := svc.(toolkit.SkillProvider); ok {
			p.RegisterSkillProvider(sp)
		}
	}
}
