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
	"strings"

	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// discoverTools 自动扫描已注册的**无权限**命令并包装为 LLM 工具。
//
// # 安全设计
//
// ⚠️ 仅自动发现不需要任何权限的命令（Permissions 为空）。
// 需要权限的命令不会被 AI 自动发现，防止通过 AI 绕过权限检查。
//
// 对于需要 AI 调用的权限命令，插件应在自己的 Setup 中调用
// [Plugin.RegisterToolProvider] 显式注册工具，并在 Execute 中自行校验身份。
//
// # 工作原理
//
//  1. 通过 engine.Reader.GetAllCommands() 获取所有命令列表
//  2. 跳过 AI 自身命令、隐藏命令、需要权限的命令
//  3. 每个安全命令生成一个 Tool 供 LLM 调用
//
// 限制：在 Setup 阶段调用，仅能发现已注册的无权限命令。
func (p *Plugin) discoverTools() {
	if p.coord == nil {
		return
	}

	allCmds := p.coord.GetAllCommands()
	for _, cmd := range allCmds {
		if cmd.Definition != nil && cmd.Definition.Hidden {
			continue
		}
		if !isCommandSafeForAI(cmd) {
			continue
		}
		tool := buildToolFromCommand(cmd)
		if tool != nil {
			p.cmdPatterns[tool.Name] = cmd.Command
			p.reg.Register(*tool)
		}
	}
}

// isCommandSafeForAI 判断命令是否能安全地暴露给 AI。
//
// 安全条件（全部满足）：
//  1. 命令无权限要求（Permissions 为空）
//  2. 命令定义中也无权限要求（Definition.Permissions 为空）
//  3. 不是 AI 自身命令
//
// 不满足任一条件 → AI 不可调用该命令。
func isCommandSafeForAI(cmd engine.CommandInfo) bool {
	name := strings.TrimLeft(cmd.Command, "/!$#")
	if name == "" || name == "ai" {
		return false
	}
	if len(cmd.Permissions) > 0 {
		return false
	}
	if cmd.Definition != nil && len(cmd.Definition.Permissions) > 0 {
		return false
	}
	return true
}

// buildToolFromCommand 将命令信息转换为 LLM 工具。
//
// 注意：调用方应确保已通过 isCommandSafeForAI 前置检查。
func buildToolFromCommand(cmd engine.CommandInfo) *Tool {
	name := strings.TrimLeft(cmd.Command, "/!$#")
	if name == "" {
		return nil
	}
	name = strings.ReplaceAll(name, " ", "_")

	desc := cmd.Description
	if desc == "" {
		desc = fmt.Sprintf("执行命令 %s", cmd.Command)
	}

	return &Tool{
		Name:        name,
		Categories:  []string{CategoryGeneral},
		Description: desc,
		Parameters: ToolParamSchema{
			Type:       "object",
			Properties: make(map[string]ToolParamSchema),
		},
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			return fmt.Sprintf("[命令 %s 已触发]", cmd.Command), nil
		},
	}
}

// RegisterToolProvider 注册一个实现了 ToolProvider 接口的插件所提供的工具集。
//
// 其他插件可在自己的 Setup 中通过 plugin.TryService 获取 AI 插件的服务实例
// 后调用此方法注册自定义工具，尤其是需要权限校验的敏感命令。
//
// 使用示例：
//
//	if aiSvc, ok := plugin.TryService[*ai.Plugin](ctx, "ai"); ok {
//	    aiSvc.RegisterToolProvider(myToolProvider)
//	}
func (p *Plugin) RegisterToolProvider(tp ToolProvider) {
	for _, t := range tp.ListTools() {
		p.reg.Register(t)
	}
}

// DiscoverToolProviders 扫描插件管理器中所有已注册的插件服务，
// 自动发现实现了 [ToolProvider] 接口的插件并注册其工具。
// 应在 [plugin.Manager.FreezeContainer] 之后调用。
func (p *Plugin) DiscoverToolProviders(mgr *plugin.Manager) {
	for _, name := range mgr.List() {
		svc, ok := mgr.GetContainer().Get(name)
		if !ok || svc == nil {
			continue
		}
		tp, ok := svc.(ToolProvider)
		if !ok {
			continue
		}
		p.RegisterToolProvider(tp)
	}
}

// RegisterSkill 注册一个 Skill。
// Skill 会自动包装为 Tool 供 LLM 发现和调用。
// 如果 Parameters 为空，自动使用 {"query": string} 作为默认参数。
func (p *Plugin) RegisterSkill(s Skill) {
	if len(s.Parameters.Properties) == 0 {
		s.Parameters = ToolParamSchema{
			Type: "object",
			Properties: map[string]ToolParamSchema{
				"query": {Type: "string", Description: "需要该技能处理的问题"},
			},
			Required: []string{"query"},
		}
	}
	p.skillReg.Register(s)
	skill := s
	p.reg.Register(Tool{
		Name:        skill.Name,
		Description: skill.Description,
		Parameters:  skill.Parameters,
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			return p.executeSkill(ctx, skill, args)
		},
	})
}

// RegisterSkillProvider 注册一个实现了 SkillProvider 接口的插件所提供的技能集。
func (p *Plugin) RegisterSkillProvider(sp SkillProvider) {
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
		if sp, ok := svc.(SkillProvider); ok {
			p.RegisterSkillProvider(sp)
		}
	}
}
