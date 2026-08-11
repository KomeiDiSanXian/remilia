// Package ai grouppolicy.go — per-group 工具策略/提示词（Group Policy）。
//
// 对齐官方 OpenClaw 插件的 per-group 配置能力：每个群可独立配置：
//   - prompt:   群级系统提示词（覆盖全局 system_prompt）
//   - tools:    群级工具白名单（all=全部 / none=禁用全部工具 / 逗号分隔工具名）
//   - approval: 群级工具审批模式（off/restricted/always，覆盖全局 tool_approval）
//   - mention:  群级是否强制 @ 机器人触发（true=必须 @，false=自主发言）
//
// 存储：LevelDB（data/ai），单 key JSON 快照 + sync.RWMutex，仿 welcome 插件。
// 生效回退链：群显式配置 > 全局配置（__global__）> 插件默认。
//
// 管理命令（需群管理员权限）：
//
//	/ai group status                      — 查看本群生效配置
//	/ai group set prompt <text>           — 设置群提示词
//	/ai group set tools all|none|t1,t2    — 设置群工具白名单
//	/ai group set approval off|restricted|always — 设置群审批模式
//	/ai group set mention on|off          — 设置群 @ 触发要求
//	/ai group reset [prompt|tools|approval|mention|all] — 重置群配置
//	/ai group global ...                  — 管理全局默认（superadmin）
package ai

import (
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
	"strings"
	"sync"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/kv"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// globalGroupID 全局默认配置在 policies 中使用的保留键。
const globalGroupID = "__global__"

// groupPolicyKey 存储键名。
const groupPolicyKey = "grouppolicies"

// ToolApprovalMode 工具审批模式。
type ToolApprovalMode string

const (
	// ApprovalOff 不审批。
	ApprovalOff ToolApprovalMode = "off"
	// ApprovalRestricted 仅审批标记 RequiresApproval 的工具。
	ApprovalRestricted ToolApprovalMode = "restricted"
	// ApprovalAlways 审批所有工具。
	ApprovalAlways ToolApprovalMode = "always"
)

// ValidApprovalModes 合法审批模式列表（用于校验）。
var ValidApprovalModes = []string{string(ApprovalOff), string(ApprovalRestricted), string(ApprovalAlways)}

// GroupPolicy 单个群的 AI 策略配置。
//
// 字段指针语义：nil = 未显式配置（回退链中跳过）；非 nil = 显式覆盖。
type GroupPolicy struct {
	// SystemPrompt 群级系统提示词；nil = 未配置（用全局/默认）。
	SystemPrompt *string `json:"system_prompt,omitempty"`
	// ToolPolicy 群级工具策略："all" | "none" | 逗号分隔工具名；nil = 未配置。
	ToolPolicy *string `json:"tool_policy,omitempty"`
	// Approval 群级审批模式；nil = 未配置。
	Approval *string `json:"approval,omitempty"`
	// RequireMention 群级是否强制 @ 触发；nil = 未配置。
	RequireMention *bool `json:"require_mention,omitempty"`
}

// Clone 深拷贝策略（避免外部修改污染内部状态）。
func (p *GroupPolicy) Clone() *GroupPolicy {
	if p == nil {
		return nil
	}
	cp := &GroupPolicy{}
	if p.SystemPrompt != nil {
		v := *p.SystemPrompt
		cp.SystemPrompt = &v
	}
	if p.ToolPolicy != nil {
		v := *p.ToolPolicy
		cp.ToolPolicy = &v
	}
	if p.Approval != nil {
		v := *p.Approval
		cp.Approval = &v
	}
	if p.RequireMention != nil {
		v := *p.RequireMention
		cp.RequireMention = &v
	}
	return cp
}

// Empty 判断策略是否完全未配置。
func (p *GroupPolicy) Empty() bool {
	return p == nil ||
		(p.SystemPrompt == nil && p.ToolPolicy == nil && p.Approval == nil && p.RequireMention == nil)
}

// groupPolicyManager 管理全部群的 AI 策略（内存 + LevelDB 持久化）。
type groupPolicyManager struct {
	mu       sync.RWMutex
	policies map[string]*GroupPolicy
	store    *kv.DB
	path     string
}

// newGroupPolicyManager 创建策略管理器。store 为 nil 时纯内存（测试用）。
func newGroupPolicyManager(store *kv.DB, path string) *groupPolicyManager {
	m := &groupPolicyManager{
		policies: make(map[string]*GroupPolicy),
		store:    store,
		path:     path,
	}
	if store != nil {
		m.load()
	}
	return m
}

// OpenGroupPolicyStore 打开指定数据目录的 LevelDB 存储并加载策略。
// 目录不存在时自动创建。
func OpenGroupPolicyStore(dataDir string) (*groupPolicyManager, error) {
	dir := filepath.Join(dataDir, "ai")
	db, err := kv.Open(dir)
	if err != nil {
		return nil, fmt.Errorf("open group policy kv store: %w", err)
	}
	return newGroupPolicyManager(db, dir), nil
}

// Close 关闭底层存储（插件 Teardown 时调用）。
func (m *groupPolicyManager) Close() {
	if m.store != nil {
		_ = m.store.Close()
	}
}

// Get 返回指定群的显式配置（nil = 无显式配置）。
func (m *groupPolicyManager) Get(groupID string) *GroupPolicy {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.policies[groupID].Clone()
}

// Effective 返回指定群的生效配置（群显式配置 > 全局 > 空策略）。
// 返回的策略字段为 nil 表示使用插件默认值。
func (m *groupPolicyManager) Effective(groupID string) *GroupPolicy {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if p, ok := m.policies[groupID]; ok {
		return p.Clone()
	}
	if g, ok := m.policies[globalGroupID]; ok {
		return g.Clone()
	}
	return &GroupPolicy{}
}

// SetGroup 设置指定群的策略（空字段表示清除对应项）。
func (m *groupPolicyManager) SetGroup(groupID string, p *GroupPolicy) {
	m.mu.Lock()
	if p == nil || p.Empty() {
		delete(m.policies, groupID)
	} else {
		if m.policies[groupID] == nil {
			m.policies[groupID] = &GroupPolicy{}
		}
		// 只合并非 nil 字段，保留未涉及字段的既有配置
		if p.SystemPrompt != nil {
			m.policies[groupID].SystemPrompt = p.SystemPrompt
		}
		if p.ToolPolicy != nil {
			m.policies[groupID].ToolPolicy = p.ToolPolicy
		}
		if p.Approval != nil {
			m.policies[groupID].Approval = p.Approval
		}
		if p.RequireMention != nil {
			m.policies[groupID].RequireMention = p.RequireMention
		}
		if m.policies[groupID].Empty() {
			delete(m.policies, groupID)
		}
	}
	m.mu.Unlock()
	m.save()
}

// ResetGroup 清空指定群的全部配置（回退到全局/默认）。
func (m *groupPolicyManager) ResetGroup(groupID string) {
	m.mu.Lock()
	delete(m.policies, groupID)
	m.mu.Unlock()
	m.save()
}

// ResetField 清空指定群的某个字段。
func (m *groupPolicyManager) ResetField(groupID, field string) {
	m.mu.Lock()
	p := m.policies[groupID]
	if p != nil {
		switch field {
		case "prompt":
			p.SystemPrompt = nil
		case "tools":
			p.ToolPolicy = nil
		case "approval":
			p.Approval = nil
		case "mention":
			p.RequireMention = nil
		}
		if p.Empty() {
			delete(m.policies, groupID)
		}
	}
	m.mu.Unlock()
	m.save()
}

// save 持久化全部策略（锁外拷贝 + JSON 落盘）。
func (m *groupPolicyManager) save() {
	if m.store == nil {
		return
	}
	m.mu.RLock()
	data := make(map[string]*GroupPolicy, len(m.policies))
	maps.Copy(data, m.policies)
	m.mu.RUnlock()
	bytes, err := json.Marshal(data)
	if err != nil {
		logger.WithError(err).Warn("[AI] Failed to marshal group policies")
		return
	}
	if err := m.store.Set([]byte(groupPolicyKey), bytes); err != nil {
		logger.WithError(err).Warn("[AI] Failed to save group policies")
	}
}

// load 从存储加载全部策略。
func (m *groupPolicyManager) load() {
	if m.store == nil {
		return
	}
	bytes, err := m.store.Get([]byte(groupPolicyKey))
	if err != nil {
		return
	}
	var data map[string]*GroupPolicy
	if err := json.Unmarshal(bytes, &data); err != nil {
		logger.WithError(err).Warn("[AI] Failed to load group policies")
		return
	}
	m.mu.Lock()
	m.policies = data
	m.mu.Unlock()
}

// effectiveTools 按群策略解析工具白名单。
// 返回 (工具名集合, 是否启用白名单过滤)。all → (nil, false)；none → (空集, true)。
func (p *GroupPolicy) effectiveTools() (allowSet map[string]struct{}, filter bool) {
	if p == nil || p.ToolPolicy == nil {
		return nil, false
	}
	policy := strings.TrimSpace(*p.ToolPolicy)
	switch {
	case policy == "" || policy == "all":
		return nil, false
	case policy == "none":
		return map[string]struct{}{}, true
	default:
		set := make(map[string]struct{})
		for name := range strings.SplitSeq(policy, ",") {
			name = strings.TrimSpace(name)
			if name != "" {
				set[name] = struct{}{}
			}
		}
		return set, true
	}
}

// EffectiveSystemPrompt 返回生效的群提示词（空串 = 用全局/默认）。
func (p *GroupPolicy) EffectiveSystemPrompt() string {
	if p == nil || p.SystemPrompt == nil {
		return ""
	}
	return *p.SystemPrompt
}

// EffectiveApproval 返回生效的审批模式（空串 = 用全局配置）。
func (p *GroupPolicy) EffectiveApproval() string {
	if p == nil || p.Approval == nil {
		return ""
	}
	return *p.Approval
}

// EffectiveRequireMention 返回生效的 @ 触发要求（nil = 用全局配置）。
func (p *GroupPolicy) EffectiveRequireMention() (bool, bool) {
	if p == nil || p.RequireMention == nil {
		return false, false
	}
	return *p.RequireMention, true
}

// filterToolsByGroupPolicy 按群策略过滤工具列表。
// 返回过滤后的工具副本（不影响原列表）。
func filterToolsByGroupPolicy(tools []Tool, policy *GroupPolicy) []Tool {
	if policy == nil {
		return tools
	}
	allowSet, filter := policy.effectiveTools()
	if !filter {
		return tools
	}
	out := make([]Tool, 0, len(tools))
	for _, t := range tools {
		if _, ok := allowSet[t.Name]; ok {
			out = append(out, t)
		}
	}
	return out
}

// isGroupAdmin 判断发送者是否为群管理员（平台角色 + RBAC 角色双通道）。
//
// 参考 pluginctrl.isGroupAdmin 的判定顺序：
//  1. 平台发送者群角色 >= Admin（群主/管理员）
//  2. RBAC 角色包含 superadmin/admin
func (p *Plugin) isGroupAdmin(ctx *eventctx.Context) bool {
	sender := ctx.GetSenderInfo()
	if sender.GroupRole >= platform.GroupRoleAdmin {
		return true
	}
	return p.isAdmin(ctx)
}

// groupRequireMention 返回当前群策略的 @ 触发要求。
// ok=false 表示群策略未配置 mention 字段（交给全局 matcher 决定）。
func (p *Plugin) groupRequireMention(ctx *eventctx.Context) (require bool, ok bool) {
	if p.groupPolicies == nil {
		return false, false
	}
	chat := ctx.GetChatInfo()
	if !chat.IsGroup || chat.ID == "" {
		return false, false
	}
	gp := p.groupPolicies.Effective(chat.ID)
	return gp.EffectiveRequireMention()
}
