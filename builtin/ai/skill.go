// Package ai skill.go — Skill（技能/子代理）的类型定义与注册表。
//
// Skill 是一个可被 AI 递归调用的子代理，拥有独立的 System Prompt
// 和作用域内的工具集。每个 Skill 自动注册为 Tool，供主 LLM 或其他 Skill 调用。
//
// 技能按 OwnerID 区分作用域：
//   - OwnerSystem = "system"：系统内置技能，所有用户可见可调用
//   - 用户 ID：用户自定义技能，仅该用户可见，会话时按需注入
package ai

import (
	"fmt"
	"strings"
	"sync"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

const (
	// OwnerSystem 是系统级技能的所有者标识。
	// 系统技能对所有用户可见，在插件 Setup 阶段通过 RegisterSkill 注册。
	OwnerSystem = "system"

	// UserSkillPrefix 是用户自定义技能的名称前缀。
	// 自动添加以防止与系统技能名称冲突，如用户注册 "poetry_writer" 实际名为 "u_poetry_writer"。
	UserSkillPrefix = "u_"
)

// Skill 定义一个可被 AI 调用的子代理（技能）。
//
// 每个 Skill 拥有独立的 System Prompt 和作用域内的工具集，
// 执行时以子 Agent 形式独立运行：使用自己的 Prompt 构建初始消息，
// 调用 LLM 进行多轮工具调用（非流式），直至完成或达到最大深度。
//
// 字段说明：
//   - Name: 技能名称，LLM 通过此名称调用。系统技能使用原始名称，用户技能自动加 u_ 前缀
//   - OwnerID: 所有者标识。"system" 为系统级，用户 ID 为用户级
//   - Description: 技能描述，LLM 据此判断何时调用该技能
//   - Parameters: 调用参数的 JSON Schema。为 nil 时自动使用 {"query": string}
//   - Prompt: 系统提示词，定义该技能的行为和角色
//   - Tools: 该技能可直接调用的工具列表
//   - Enabled: 是否启用。用户可开关自己的技能，禁用的技能不注入会话
//   - UsageCount: 累计调用次数，用于统计和展示
type Skill struct {
	Name        string
	OwnerID     string
	Description string
	Parameters  ToolParamSchema
	Prompt      string
	Tools       []Tool
	Enabled     bool
	UsageCount  int64
}

// SkillRegistry 管理所有已注册的 Skill，支持按所有者查询和线程安全操作。
//
// 设计要点：
//   - 使用 sync.RWMutex 保证并发安全，支持运行时注册/注销
//   - 按名称（display name）主索引，按所有者二级索引
//   - 系统技能在 Setup 阶段注册，用户技能由运行时命令注册
type SkillRegistry struct {
	mu      sync.RWMutex
	skills  map[string]Skill    // ownerID + name → Skill
	byOwner map[string][]string // ownerID → []name
}

// NewSkillRegistry 创建空的技能注册表。
func NewSkillRegistry() *SkillRegistry {
	return &SkillRegistry{
		skills:  make(map[string]Skill),
		byOwner: make(map[string][]string),
	}
}

func skillKey(ownerID, name string) string {
	return ownerID + "\x00" + name
}

// Register 注册一个系统或插件提供的 Skill。同一所有者下重名时保留先注册的版本。
// 用户创建 Skill 应使用 Add，以便把重名反馈给调用者。
func (r *SkillRegistry) Register(s Skill) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.addLocked(s); err != nil {
		logger.Warnf("[AI] %v", err)
	}
}

// Add 注册一个 Skill，并在同一所有者下名称重复时返回错误。
func (r *SkillRegistry) Add(s Skill) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.addLocked(s)
}

func (r *SkillRegistry) addLocked(s Skill) error {
	if strings.TrimSpace(s.OwnerID) == "" || strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("skill owner and name must not be empty")
	}
	key := skillKey(s.OwnerID, s.Name)
	if _, exists := r.skills[key]; exists {
		return fmt.Errorf("skill %q already exists for this owner", s.Name)
	}
	r.skills[key] = s
	r.byOwner[s.OwnerID] = append(r.byOwner[s.OwnerID], s.Name)
	return nil
}

// GetSystem 返回指定系统 Skill。
func (r *SkillRegistry) GetSystem(name string) (Skill, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.skills[skillKey(OwnerSystem, name)]
	return s, ok
}

// GetByOwner 返回指定所有者的 Skill。
func (r *SkillRegistry) GetByOwner(ownerID, name string) (Skill, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.skills[skillKey(ownerID, name)]
	return s, ok
}

// Get 为向后兼容保留，等价于 GetSystem。
func (r *SkillRegistry) Get(name string) (Skill, bool) {
	return r.GetSystem(name)
}

// SetEnabled 更新用户 Skill 的启用状态并返回更新后的副本。
func (r *SkillRegistry) SetEnabled(ownerID, name string, enabled bool) (Skill, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := skillKey(ownerID, name)
	s, ok := r.skills[key]
	if !ok {
		return Skill{}, fmt.Errorf("skill %q not found", name)
	}
	s.Enabled = enabled
	r.skills[key] = s
	return s, nil
}

// List 返回当前所有已注册技能的切片副本。
func (r *SkillRegistry) List() []Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Skill, 0, len(r.skills))
	for _, s := range r.skills {
		out = append(out, s)
	}
	return out
}

// ListByOwner 返回指定所有者的所有技能副本。
// 常用于在 processWithTools 中获取当前用户的技能列表。
func (r *SkillRegistry) ListByOwner(ownerID string) []Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := r.byOwner[ownerID]
	out := make([]Skill, 0, len(names))
	for _, name := range names {
		if s, ok := r.skills[skillKey(ownerID, name)]; ok {
			out = append(out, s)
		}
	}
	return out
}

// Remove 删除指定名称的技能。验证所有权，非所有者不能删除。
func (r *SkillRegistry) Remove(name, ownerID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.skills[skillKey(ownerID, name)]; !ok {
		return fmt.Errorf("skill %q not found", name)
	}
	r.removeLocked(name, ownerID)
	return nil
}

// removeLocked 在已持有写锁时执行删除操作。
// 调用方必须已持有 r.mu.Lock()。
func (r *SkillRegistry) removeLocked(name, ownerID string) {
	delete(r.skills, skillKey(ownerID, name))
	names := r.byOwner[ownerID]
	for i, n := range names {
		if n == name {
			r.byOwner[ownerID] = append(names[:i], names[i+1:]...)
			break
		}
	}
}

// IncrementUsage 增加指定技能的调用计数。线程安全。
func (r *SkillRegistry) IncrementUsage(ownerID, name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := skillKey(ownerID, name)
	if s, ok := r.skills[key]; ok {
		s.UsageCount++
		r.skills[key] = s
	}
}

// Promote 将指定用户技能提升为系统级技能。
func (r *SkillRegistry) Promote(name, ownerID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	s, ok := r.skills[skillKey(ownerID, name)]
	if !ok {
		return fmt.Errorf("skill %q not found", name)
	}

	newName := strings.TrimPrefix(name, UserSkillPrefix)
	if _, exists := r.skills[skillKey(OwnerSystem, newName)]; exists {
		return fmt.Errorf("a system skill named %q already exists", newName)
	}

	r.removeLocked(name, ownerID)
	s.Name = newName
	s.OwnerID = OwnerSystem
	r.skills[skillKey(OwnerSystem, newName)] = s
	r.byOwner[OwnerSystem] = append(r.byOwner[OwnerSystem], newName)
	return nil
}
