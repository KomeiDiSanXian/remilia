// Package ai skill.go — Skill（技能/子代理）的类型定义与注册表。
//
// Skill 是一个可被 AI 递归调用的子代理，拥有独立的 System Prompt
// 和作用域内的工具集。每个 Skill 自动注册为 Tool，供主 LLM 或其他 Skill 调用。
package ai

// Skill 定义一个可被 AI 调用的子代理（技能）。
//
// 每个 Skill 拥有独立的 System Prompt 和作用域内的工具集，
// 执行时以子 Agent 形式独立运行：使用自己的 Prompt 构建初始消息，
// 调用 LLM 进行多轮工具调用（非流式），直至完成或达到最大深度。
//
// 字段说明：
//   - Name: 技能名称，LLM 通过此名称调用。需唯一 snake_case
//   - Description: 技能描述，LLM 据此判断何时调用该技能
//   - Parameters: 调用参数的 JSON Schema。为 nil 时自动使用 {"query": string}
//   - Prompt: 系统提示词，定义该技能的行为和角色
//   - Tools: 该技能可直接调用的工具列表（不含其他 Skill，其他 Skill 由 buildSkillTools 自动注入）
type Skill struct {
	Name        string
	Description string
	Parameters  ToolParamSchema
	Prompt      string
	Tools       []Tool
}

// SkillRegistry 管理所有已注册的 Skill，按技能名索引。
// 非线程安全，应在插件 Setup 阶段完成注册。
type SkillRegistry struct {
	skills map[string]Skill
}

// NewSkillRegistry 创建空的技能注册表。
func NewSkillRegistry() *SkillRegistry {
	return &SkillRegistry{skills: make(map[string]Skill)}
}

// Register 注册一个 Skill。同名技能仅首次注册生效，后续忽略。
func (r *SkillRegistry) Register(s Skill) {
	if _, exists := r.skills[s.Name]; exists {
		return
	}
	r.skills[s.Name] = s
}

// Get 按名称查找 Skill。第二个返回值为 false 表示未找到。
func (r *SkillRegistry) Get(name string) (Skill, bool) {
	s, ok := r.skills[name]
	return s, ok
}

// List 返回当前所有已注册技能的切片副本。
func (r *SkillRegistry) List() []Skill {
	out := make([]Skill, 0, len(r.skills))
	for _, t := range r.skills {
		out = append(out, t)
	}
	return out
}
