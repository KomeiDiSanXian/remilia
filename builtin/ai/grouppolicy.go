// grouppolicy.go — per-group 工具策略/提示词（Group Policy）。
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
	"path/filepath"
	"strings"
	"sync"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/kv"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// globalGroupID 全局默认配置在 policies 中使用的保留键。
const globalGroupID = "__global__"

// groupPolicyKey 存储键名。
const groupPolicyKey = "grouppolicies"

// toolApprovalMode 工具审批模式。
type toolApprovalMode string

const (
	// approvalOff 不审批。
	approvalOff toolApprovalMode = "off"
	// approvalRestricted 仅审批标记 RequiresApproval 的工具。
	approvalRestricted toolApprovalMode = "restricted"
	// approvalAlways 审批所有工具。
	approvalAlways toolApprovalMode = "always"
)

// validApprovalModes 合法审批模式列表（用于校验）。
var validApprovalModes = []string{string(approvalOff), string(approvalRestricted), string(approvalAlways)}

// groupPolicy 单个群的 AI 策略配置。
//
// 字段指针语义：nil = 未显式配置（回退链中跳过）；非 nil = 显式覆盖。
type groupPolicy struct {
	// SystemPrompt 群级系统提示词；nil = 未配置（用全局/默认）。
	SystemPrompt *string `json:"system_prompt,omitempty"`
	// ToolPolicy 群级工具策略："all" | "none" | 逗号分隔工具名；nil = 未配置。
	ToolPolicy *string `json:"tool_policy,omitempty"`
	// Approval 群级审批模式；nil = 未配置。
	Approval *string `json:"approval,omitempty"`
	// RequireMention 群级是否强制 @ 触发；nil = 未配置。
	RequireMention *bool `json:"require_mention,omitempty"`
	// ReplyPolicy 群级自主发言回复决策（all/smart/judge）；nil = 未配置。
	// 仅在群被 GroupAutonomous（或 fallback）接管全量消息时生效。
	ReplyPolicy *string `json:"reply_policy,omitempty"`
	// ReplyChance 群级接话概率（0~1，smart 档有效）；nil = 未配置（用全局）。
	ReplyChance *float64 `json:"reply_chance,omitempty"`
}

// Clone 深拷贝策略（避免外部修改污染内部状态）。
func (p *groupPolicy) Clone() *groupPolicy {
	if p == nil {
		return nil
	}
	cp := &groupPolicy{}
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
	if p.ReplyPolicy != nil {
		v := *p.ReplyPolicy
		cp.ReplyPolicy = &v
	}
	if p.ReplyChance != nil {
		v := *p.ReplyChance
		cp.ReplyChance = &v
	}
	return cp
}

// Empty 判断策略是否完全未配置。
func (p *groupPolicy) Empty() bool {
	return p == nil ||
		(p.SystemPrompt == nil && p.ToolPolicy == nil && p.Approval == nil &&
			p.RequireMention == nil && p.ReplyPolicy == nil && p.ReplyChance == nil)
}

// groupPolicyManager 管理全部群的 AI 策略（内存 + LevelDB 持久化）。
type groupPolicyManager struct {
	mu       sync.RWMutex
	policies map[string]*groupPolicy
	store    *kv.DB
	path     string
}

// newGroupPolicyManager 创建策略管理器。store 为 nil 时纯内存（测试用）。
func newGroupPolicyManager(store *kv.DB, path string) *groupPolicyManager {
	m := &groupPolicyManager{
		policies: make(map[string]*groupPolicy),
		store:    store,
		path:     path,
	}
	if store != nil {
		m.load()
	}
	return m
}

// openGroupPolicyStore 打开指定数据目录的 LevelDB 存储并加载策略。
// 目录不存在时自动创建。
func openGroupPolicyStore(dataDir string) (*groupPolicyManager, error) {
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
func (m *groupPolicyManager) Get(groupID string) *groupPolicy {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.policies[groupID].Clone()
}

// Effective 返回指定群的生效配置（字段级回退链：群显式配置的字段优先，
// 未配置的字段回退全局配置（__global__），再回退插件默认）。
// 返回的策略字段为 nil 表示使用插件默认值。
//
// 必须按字段合并而不是整份替换：此前只要群设置了任何一条配置，全局策略
// 就整体失效——超管设了全局 tools=none，群管理员建一条群条目即可静默恢复
// 全开，权限收紧被绕过。
func (m *groupPolicyManager) Effective(groupID string) *groupPolicy {
	m.mu.RLock()
	defer m.mu.RUnlock()
	// nil 全局策略视为全空策略（字段未配置），统一合并路径。
	global := m.policies[globalGroupID]
	if global == nil {
		global = &groupPolicy{}
	}
	if p, ok := m.policies[groupID]; ok {
		return &groupPolicy{
			SystemPrompt:   mergeField(p.SystemPrompt, global.SystemPrompt),
			ToolPolicy:     mergeField(p.ToolPolicy, global.ToolPolicy),
			Approval:       mergeField(p.Approval, global.Approval),
			ReplyPolicy:    mergeField(p.ReplyPolicy, global.ReplyPolicy),
			ReplyChance:    mergeField(p.ReplyChance, global.ReplyChance),
			RequireMention: mergeField(p.RequireMention, global.RequireMention),
		}
	}
	return global.Clone()
}

// mergeField 单字段回退：群显式配置优先，未配置回退全局。
//
// 群策略里每个可覆盖字段都是 *T（nil = 未配置），回退规则完全一致，因此只留
// 这一份泛型实现；此前按字段类型各抄一遍（*string 与 *float64 各一套），
// 新增字段时要记得再去加一个同义函数，很容易漏。
func mergeField[T any](group, g *T) *T {
	if group != nil {
		return group
	}
	return g
}

// SetGroup 设置指定群的策略（空字段表示清除对应项）。
func (m *groupPolicyManager) SetGroup(groupID string, p *groupPolicy) {
	m.mu.Lock()
	if p == nil || p.Empty() {
		delete(m.policies, groupID)
	} else {
		if m.policies[groupID] == nil {
			m.policies[groupID] = &groupPolicy{}
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
		if p.ReplyPolicy != nil {
			m.policies[groupID].ReplyPolicy = p.ReplyPolicy
		}
		if p.ReplyChance != nil {
			m.policies[groupID].ReplyChance = p.ReplyChance
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
		case "reply":
			p.ReplyPolicy = nil
			p.ReplyChance = nil
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
	data := make(map[string]*groupPolicy, len(m.policies))
	for id, p := range m.policies {
		// 深拷贝：浅拷贝只复制指针，锁外 json.Marshal 会与并发的
		// SetGroup/ResetField 读写同一结构体（数据竞争）。
		data[id] = p.Clone()
	}
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
	var data map[string]*groupPolicy
	if err := json.Unmarshal(bytes, &data); err != nil {
		logger.WithError(err).Warn("[AI] Failed to load group policies")
		return
	}
	m.mu.Lock()
	m.policies = data
	m.mu.Unlock()
}

// effectiveTools 按群策略解析工具白名单。
// 返回 (工具名集合（小写归一）, 是否启用白名单过滤)。all → (nil, false)；none → (空集, true)。
// 名字按小写归一存储：FilterTools 与注册表名比较时同样归一，避免 ValidToolName
// 允许大写导致白名单永远匹配不上。
func (p *groupPolicy) effectiveTools() (allowSet map[string]struct{}, filter bool) {
	if p == nil || p.ToolPolicy == nil {
		return nil, false
	}
	policy := strings.TrimSpace(*p.ToolPolicy)
	switch policy {
	case "", "all":
		return nil, false
	case "none":
		return map[string]struct{}{}, true
	default:
		set := make(map[string]struct{})
		for name := range strings.SplitSeq(policy, ",") {
			name = strings.ToLower(strings.TrimSpace(name))
			if name != "" {
				set[name] = struct{}{}
			}
		}
		return set, true
	}
}

// EffectiveSystemPrompt 返回生效的群提示词（空串 = 用全局/默认）。
func (p *groupPolicy) EffectiveSystemPrompt() string {
	if p == nil || p.SystemPrompt == nil {
		return ""
	}
	return *p.SystemPrompt
}

// EffectiveApproval 返回生效的审批模式（空串 = 用全局配置）。
func (p *groupPolicy) EffectiveApproval() string {
	if p == nil || p.Approval == nil {
		return ""
	}
	return *p.Approval
}

// EffectiveRequireMention 返回生效的 @ 触发要求（nil = 用全局配置）。
func (p *groupPolicy) EffectiveRequireMention() (bool, bool) {
	if p == nil || p.RequireMention == nil {
		return false, false
	}
	return *p.RequireMention, true
}

// EffectiveReplyPolicy 返回生效的群回复决策（空串 = 用全局配置）。
func (p *groupPolicy) EffectiveReplyPolicy() string {
	if p == nil || p.ReplyPolicy == nil {
		return ""
	}
	return *p.ReplyPolicy
}

// EffectiveReplyChance 返回生效的群接话概率（ok=false = 用全局配置）。
func (p *groupPolicy) EffectiveReplyChance() (float64, bool) {
	if p == nil || p.ReplyChance == nil {
		return 0, false
	}
	return *p.ReplyChance, true
}

// FilterTools 按群策略过滤动作列表。
// 策略为 nil 或未限制工具时原样返回；否则返回过滤后的动作副本（不影响原列表）。
func (p *groupPolicy) FilterTools(actions []toolkit.Action) []toolkit.Action {
	if p == nil {
		return actions
	}
	allowSet, filter := p.effectiveTools()
	if !filter {
		return actions
	}
	out := make([]toolkit.Action, 0, len(actions))
	for _, a := range actions {
		if _, ok := allowSet[strings.ToLower(a.Spec.Name)]; ok {
			out = append(out, a)
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
func (a *adminState) groupRequireMention(ctx *eventctx.Context) (require bool, ok bool) {
	if a.groupPolicies == nil {
		return false, false
	}
	chat := ctx.GetChatInfo()
	if !chat.IsGroup || chat.ID == "" {
		return false, false
	}
	gp := a.groupPolicies.Effective(chat.ID)
	return gp.EffectiveRequireMention()
}
