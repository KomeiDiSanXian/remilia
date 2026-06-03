package ai

// Skill 定义一个可被 AI 调用的子代理。
// 每个 Skill 拥有独立的 System Prompt 和作用域内的工具集。
// Skill 会被自动注册为 Tool 供 LLM 发现和调用。
type Skill struct {
	Name        string
	Description string
	Parameters  ToolParamSchema
	Prompt      string
	Tools       []Tool
}

// SkillRegistry 管理所有已注册的 Skill。
type SkillRegistry struct {
	skills map[string]Skill
}

func NewSkillRegistry() *SkillRegistry {
	return &SkillRegistry{skills: make(map[string]Skill)}
}

func (r *SkillRegistry) Register(s Skill) {
	if _, exists := r.skills[s.Name]; exists {
		return
	}
	r.skills[s.Name] = s
}

func (r *SkillRegistry) Get(name string) (Skill, bool) {
	s, ok := r.skills[name]
	return s, ok
}

func (r *SkillRegistry) List() []Skill {
	out := make([]Skill, 0, len(r.skills))
	for _, s := range r.skills {
		out = append(out, s)
	}
	return out
}
