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
	"regexp"
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
// 应在所有插件完成注册后调用，确保不会遗漏后注册的命令。
func (p *Plugin) discoverTools() {
	if p.coord == nil {
		return
	}

	hasAllowlist := len(p.cfg.ToolAllowlist) > 0
	allowSet := make(map[string]struct{}, len(p.cfg.ToolAllowlist))
	for _, name := range p.cfg.ToolAllowlist {
		allowSet[name] = struct{}{}
	}

	// AI 自身的触发命令（默认 /ai）不应被自动发现为工具，避免 AI 自我递归调用。
	// 若用户将 trigger_cmd 改为 /chat 等，也要一并排除。
	triggerName := ""
	if p.cfg.TriggerCmd != "" {
		triggerName = strings.TrimLeft(p.cfg.TriggerCmd, "/!$#")
	}

	allCmds := p.coord.GetAllCommands()
	for _, cmd := range allCmds {
		if cmd.Definition != nil && cmd.Definition.Hidden {
			continue
		}
		if !isCommandSafeForAI(cmd) {
			continue
		}
		name := strings.TrimLeft(cmd.Command, "/!$#")
		name = strings.ReplaceAll(name, " ", "_")
		if triggerName != "" && name == triggerName {
			continue
		}
		if hasAllowlist {
			if _, ok := allowSet[name]; !ok {
				continue
			}
		}
		// 已被显式注册（RegisterToolProvider / RegisterSkill）占用的名称
		// 不再自动发现为命令工具，避免覆盖或污染 cmdPatterns。
		if _, exists := p.reg.Get(name); exists {
			continue
		}
		tool := buildToolFromCommand(cmd)
		if tool != nil {
			p.cmdMu.Lock()
			p.cmdPatterns[tool.Name] = cmd.Command
			p.cmdMu.Unlock()
			p.reg.Register(*tool)
		}
	}
}

// validToolNameRegex 用于校验工具名称是否符合 AI API 的要求（仅含字母、数字、下划线、连字符）。
var validToolNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// isCommandSafeForAI 判断命令是否能安全地暴露给 AI。
//
// 安全条件（全部满足）：
//  1. 命令无权限要求（Permissions 为空）
//  2. 命令定义中也无权限要求（Definition.Permissions 为空）
//  3. 不是 AI 自身命令
//  4. 命令名称能构成合法的工具名（仅含 a-zA-Z0-9_-）
//
// 不满足任一条件 → AI 不可调用该命令。
func isCommandSafeForAI(cmd engine.CommandInfo) bool {
	name := strings.TrimLeft(cmd.Command, "/!$#")
	name = strings.ReplaceAll(name, " ", "_")
	if name == "" || name == "ai" {
		return false
	}
	if !validToolNameRegex.MatchString(name) {
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

// sanitizeToolName 确保工具名称只包含 [a-zA-Z0-9_-]。
// 不符合的字符会被替换为下划线，连续多个下划线合并为一个。
func sanitizeToolName(name string) string {
	// 先替换所有非法字符为下划线
	result := invalidToolNameChars.ReplaceAllString(name, "_")
	// 合并连续下划线
	result = multiUnderscore.ReplaceAllString(result, "_")
	// 去掉首尾下划线
	result = strings.Trim(result, "_")
	if result == "" {
		result = "unknown_tool"
	}
	return result
}

var invalidToolNameChars = regexp.MustCompile(`[^a-zA-Z0-9_-]`)
var multiUnderscore = regexp.MustCompile(`_+`)

// deriveToolCategory 从命令信息中推断工具的分类。
// 优先使用命令定义中的 Category，其次使用插件名，兜底为 "general"。
func deriveToolCategory(cmd engine.CommandInfo) string {
	if cmd.Category != "" {
		return cmd.Category
	}
	if cmd.Plugin != "" && cmd.Plugin != "global" {
		return cmd.Plugin
	}
	if cmd.Definition != nil && cmd.Definition.Category != "" {
		return cmd.Definition.Category
	}
	return CategoryGeneral
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
	name = sanitizeToolName(name)

	desc := cmd.Description
	if desc == "" {
		desc = fmt.Sprintf("执行命令 %s", cmd.Command)
	}

	category := deriveToolCategory(cmd)
	return &Tool{
		Name:        name,
		Categories:  []string{category},
		Description: desc,
		Parameters: ToolParamSchema{
			Type: "object",
			Properties: map[string]ToolParamSchema{
				"arguments": {
					Type:        "string",
					Description: "传递给命令的原始参数；无参数命令可省略",
				},
			},
		},
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			return fmt.Sprintf("[命令 %s 已触发]", cmd.Command), nil
		},
	}
}

// DiscoverCommands 扫描当前所有已注册的无权限命令。
// 应在插件容器冻结后、开始处理平台事件前调用。
func (p *Plugin) DiscoverCommands() {
	p.discoverTools()
}

// RegisterToolProvider 注册一个实现了 ToolProvider 接口的插件所提供的工具集。
//
// 其他插件可在自己的 Setup 中通过 plugin.TryService 获取 AI 插件的服务实例
// 后调用此方法注册自定义工具，尤其是需要权限校验的敏感命令。
//
// 显式注册优先于自动发现：若工具名与自动发现的命令工具重名，
// 会先移除自动发现的版本及其命令映射，保证 LLM 调用的是插件
// 自己实现的 Execute，而非通过合成事件触发真实命令。
//
// 使用示例：
//
//	if aiSvc, ok := plugin.TryService[*ai.Plugin](ctx, "ai"); ok {
//	    aiSvc.RegisterToolProvider(myToolProvider)
//	}
func (p *Plugin) RegisterToolProvider(tp ToolProvider) {
	for _, t := range tp.ListTools() {
		if t.Name == "" {
			continue
		}
		p.reg.Remove(t.Name)
		p.cmdMu.Lock()
		delete(p.cmdPatterns, t.Name)
		p.cmdMu.Unlock()
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

// RegisterSkill 注册一个系统级 Skill。
// OwnerID 为空时自动设为 OwnerSystem。
// Skill 会自动包装为 Tool 供 LLM 发现和调用。
// 如果 Parameters 为空，自动使用 {"query": string} 作为默认参数。
func (p *Plugin) RegisterSkill(s Skill) {
	if s.OwnerID == "" {
		s.OwnerID = OwnerSystem
	}
	p.applyDefaultParamSchema(&s)
	p.skillReg.Register(s)
	p.registerSkillAsTool(s)
}

// RegisterUserSkill 注册一个用户自定义 Skill。
// name 会自动添加 u_ 前缀，OwnerID 设为 ownerID。
// 注册到 skillReg 但不注册到全局 ToolRegistry（由 processWithTools 按会话注入）。
func (p *Plugin) RegisterUserSkill(s Skill, ownerID string) error {
	name := strings.TrimPrefix(strings.TrimSpace(s.Name), UserSkillPrefix)
	if !userSkillNamePattern.MatchString(name) {
		return fmt.Errorf("技能名称只能使用字母、数字、下划线或连字符，长度为 1–62")
	}
	s.OwnerID = ownerID
	s.Name = UserSkillPrefix + name
	p.applyDefaultParamSchema(&s)
	if !s.Enabled {
		s.Enabled = true
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

var userSkillNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,62}$`)

func (p *Plugin) applyDefaultParamSchema(s *Skill) {
	if len(s.Parameters.Properties) == 0 {
		s.Parameters = ToolParamSchema{
			Type: "object",
			Properties: map[string]ToolParamSchema{
				"query": {Type: "string", Description: "需要该技能处理的问题"},
			},
			Required: []string{"query"},
		}
	}
}

func (p *Plugin) registerSkillAsTool(s Skill) {
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
