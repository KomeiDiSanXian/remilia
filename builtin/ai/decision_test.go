// Package ai decision_test.go — 回合动作决策的契约用例。
//
// 锁定决策链：候选发现（注册表 + 用户 Skill，经群策略与 RBAC）→ 选择 →
// 稳定策略，以及"本轮无需主动动作"时只收敛到保留集（默认保留级别 +
// 会话已用 + 稳定集合），不挑选可选动作、不污染选择缓存。
package ai

import (
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/catalog"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/execution"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// decisionPlugin 构造带注册表与 Skill 注册表的插件（稳定策略默认关闭）。
func decisionPlugin(t *testing.T, tools ...toolkit.Tool) *Plugin {
	t.Helper()
	p := &Plugin{
		reg:      toolkit.NewToolRegistry(),
		skillReg: toolkit.NewSkillRegistry(),
		cfg:      &config.Config{ToolSelectMax: 20, ToolBudget: 8000},
	}
	for _, tool := range tools {
		p.reg.Register(tool)
	}
	return p
}

func decisionCtx(query string) *eventctx.Context {
	evt := platform.NewSyntheticEvent("c2c", query,
		platform.WithSyntheticSender(platform.UserInfo{ID: "u1", DisplayName: "小明"}),
	)
	return eventctx.NewContextFromEvent(evt, nil)
}

// decisionSession 构造会话：最后一条用户消息用于打分，可选的历史工具调用
// 用于"会话已用"判定。
func decisionSession(query string, usedTools ...string) *session.Session {
	s := &session.Session{ID: "s1", UserID: "u1", ChatID: "c1"}
	msgs := []protocol.Message{{Role: protocol.RoleUser, Content: query}}
	for _, name := range usedTools {
		msgs = append(msgs, protocol.Message{Role: protocol.RoleAssistant, ToolCalls: []protocol.ToolCall{{Name: name}}})
	}
	s.Messages = msgs
	return s
}

func toolNamesOf(actions []toolkit.Action) []string {
	names := make([]string, 0, len(actions))
	for _, a := range actions {
		names = append(names, a.Spec.Name)
	}
	return names
}

// TestNeedActionIsConservativeByDefault 冻结现状：闸门当前恒为真，即不抑制
// 可选动作；收紧判定（如纯闲聊直接判否）属行为变更，需单独评估。
func TestNeedActionIsConservativeByDefault(t *testing.T) {
	p := decisionPlugin(t)
	assert.True(t, p.needAction())
}

// TestDecideTurnActionsWithoutActionKeepsRetainedOnly 冻结决策语义：
// 本轮无需主动动作时，只保留默认保留级别与会话已用工具，不挑选可选动作。
func TestDecideTurnActionsWithoutActionKeepsRetainedOnly(t *testing.T) {
	p := decisionPlugin(t,
		toolkit.Tool{Name: "baseline_gen", Categories: []string{toolkit.CategoryGeneral}},
		toolkit.Tool{Name: "weather", Categories: []string{"web"}, Description: "查询天气温度湿度"},
		toolkit.Tool{Name: "used_optional", Categories: []string{"web"}, Description: "历史用过的工具"},
	)
	sess := decisionSession("查一下天气", "used_optional")

	got := toolNamesOf(p.decideTurnActions(decisionCtx("查一下天气"), sess, false))

	assert.ElementsMatch(t, []string{"baseline_gen", "used_optional"}, got,
		"只保留默认保留级别与会话已用工具")
	assert.NotContains(t, got, "weather", "可选动作不得因打分入选")
}

// TestDecideTurnActionsRetainsStickyWhenNoAction 冻结不变量 N3：
// 闸门不挑选可选动作时，稳定集合（sticky）里已收敛的动作不得被清空。
//
// 工具数需大于 ToolSelectMax，选择器才会真正运行并写入稳定集合
// （工具数不超过上限时按 A1 直接原样返回，不进入稳定策略）。
func TestDecideTurnActionsRetainsStickyWhenNoAction(t *testing.T) {
	p := decisionPlugin(t,
		toolkit.Tool{Name: "baseline_gen", Categories: []string{toolkit.CategoryGeneral}},
		toolkit.Tool{Name: "weather", Categories: []string{"web"}, Description: "查询天气温度湿度"},
		toolkit.Tool{Name: "filler", Categories: []string{"web"}, Description: "无关动作"},
	)
	p.cfg.ToolSelectMax = 2
	p.cfg.ToolSetSticky = true
	p.cfg.ToolSetStickyMax = 8
	p.cfg.ToolSetTTL = 20 * time.Minute
	sess := decisionSession("查一下天气")

	first := toolNamesOf(p.decideTurnActions(decisionCtx("查一下天气"), sess, true))
	require.Contains(t, first, "weather", "首轮按打分为可选动作留有位置")

	second := toolNamesOf(p.decideTurnActions(decisionCtx("今天聊点别的"), sess, false))
	assert.Contains(t, second, "weather", "无需主动动作时稳定集合不得被清空")
	assert.Contains(t, second, "baseline_gen")
}

// TestActionCandidatesGroupPolicyThenPermission 冻结过滤语义（E4）：
// 群策略定义可用边界，RBAC 在该边界内再过滤；未声明 Permissions 的动作不受 RBAC 影响。
func TestActionCandidatesGroupPolicyThenPermission(t *testing.T) {
	p := decisionPlugin(t,
		toolkit.Tool{Name: "weather", Categories: []string{"web"}},
		toolkit.Tool{Name: "admin_only", Categories: []string{"web"}, Permissions: []string{"acl.view"}},
		toolkit.Tool{Name: "unlisted", Categories: []string{"web"}},
	)
	allowed := "weather,admin_only"
	gpm := newGroupPolicyManager(nil, "")
	gpm.SetGroup("g1", &GroupPolicy{ToolPolicy: &allowed})
	p.groupPolicies = gpm

	evt := platform.NewSyntheticEvent(platform.EventKindGroupMessage, "查天气",
		platform.WithSyntheticSender(platform.UserInfo{ID: "u1"}),
		platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true}),
	)
	ctx := eventctx.NewContextFromEvent(evt, nil)

	got := toolNamesOf(p.actionCandidates(ctx, decisionSession("查天气")))

	// 群策略边界 = {admin_only, weather}；其中 admin_only 声明了权限，
	// 当前用户无权 → 不进入候选；unlisted 不在边界内。
	assert.Equal(t, []string{"weather"}, got)
}

// TestActionCandidatesIncludesUserSkills 冻结候选来源：注册表动作 + 用户 Skill。
func TestActionCandidatesIncludesUserSkills(t *testing.T) {
	p := decisionPlugin(t, toolkit.Tool{Name: "weather", Categories: []string{"web"}})
	p.skillReg.Register(toolkit.Skill{OwnerID: "u1", Name: "user_skill", Enabled: true})

	got := toolNamesOf(p.actionCandidates(decisionCtx("随便问问"), decisionSession("随便问问")))

	assert.Contains(t, got, "weather")
	assert.Contains(t, got, "user_skill")
}

// ── 单次调用的策略评估：审批模式与放行结论 ─────────────────────────

// TestNeedsApproval 冻结审批模式与"工具是否标记审批"的组合语义。
func TestNeedsApproval(t *testing.T) {
	p := &Plugin{}
	safe := toolkit.ActionOf(toolkit.Tool{Name: "safe"})
	sensitive := toolkit.ActionOf(toolkit.Tool{Name: "sensitive", RequiresApproval: true})

	assert.False(t, p.needsApproval(safe, "off"))
	assert.False(t, p.needsApproval(sensitive, "off"))
	assert.False(t, p.needsApproval(safe, "restricted"))
	assert.True(t, p.needsApproval(sensitive, "restricted"))
	assert.True(t, p.needsApproval(safe, "always"))
	assert.True(t, p.needsApproval(sensitive, "always"))
}

// TestApprovalModeFor 冻结生效审批模式的来源与优先级（群策略 > 全局 > 默认 off），
// 以及始终审批工具不受 off 模式豁免。
func TestApprovalModeFor(t *testing.T) {
	ctx, _ := newApprovalTestContext("u1")
	reg := toolkit.NewToolRegistry()
	reg.Register(toolkit.Tool{Name: "safe_tool"})
	reg.Register(toolkit.Tool{Name: "sensitive_tool", RequiresApproval: true})

	// 全局 off：都不审批
	p := &Plugin{reg: reg, cfg: &config.Config{ToolApproval: "off"}}
	assert.False(t, p.approvalModeFor(ctx, "safe_tool"))
	assert.False(t, p.approvalModeFor(ctx, "sensitive_tool"))

	// 全局 restricted：仅审批标记工具
	p.cfg.ToolApproval = "restricted"
	assert.False(t, p.approvalModeFor(ctx, "safe_tool"))
	assert.True(t, p.approvalModeFor(ctx, "sensitive_tool"))

	// 全局 always：全部审批
	p.cfg.ToolApproval = "always"
	assert.True(t, p.approvalModeFor(ctx, "safe_tool"))
	assert.True(t, p.approvalModeFor(ctx, "sensitive_tool"))

	// per-group 覆盖：群策略 approval=off 覆盖全局 always
	policy := &GroupPolicy{}
	off := string(ApprovalOff)
	policy.Approval = &off
	gpm := newGroupPolicyManager(nil, "")
	gpm.SetGroup("group_1", policy)
	p.groupPolicies = gpm
	p.cfg.ToolApproval = "always"
	assert.False(t, p.approvalModeFor(ctx, "sensitive_tool"), "group policy off should override global always")

	// per-group 覆盖：群策略 approval=always 覆盖全局 off
	always := string(ApprovalAlways)
	policy2 := &GroupPolicy{Approval: &always}
	gpm2 := newGroupPolicyManager(nil, "")
	gpm2.SetGroup("group_1", policy2)
	p2 := &Plugin{reg: reg, cfg: &config.Config{ToolApproval: "off"}, groupPolicies: gpm2}
	assert.True(t, p2.approvalModeFor(ctx, "safe_tool"), "group policy always should override global off")
}

// TestDecideToolInvocationSingleSource 冻结"调用策略评估是唯一判定点"：
// 权限不足直接拒绝并计为失败；未过审批门（本调用无需审批）放行但不授予 SendTo
// 授权，只有经审批门放行才授予。
func TestDecideToolInvocationSingleSource(t *testing.T) {
	reg := toolkit.NewToolRegistry()
	reg.Register(toolkit.Tool{Name: "plain_tool"})
	reg.Register(toolkit.Tool{Name: "protected_tool", Permissions: []string{"acl.view"}})
	reg.Register(toolkit.Tool{Name: catalog.SendToToolName, RequiresApproval: true, AlwaysRequireApproval: true})

	t.Run("声明权限但无权：拒绝且计为失败", func(t *testing.T) {
		p := &Plugin{
			reg:       reg,
			cfg:       &config.Config{ToolApproval: "off"},
			approvals: execution.NewApprovalManager(),
		}
		ctx, _ := newApprovalTestContext("u1")

		d := p.decideToolInvocation(ctx, protocol.ToolCall{Name: "protected_tool"})
		assert.False(t, d.allowed)
		assert.Contains(t, d.rejectText, "需要权限")
		assert.Error(t, d.err, "权限拒绝属失败，计入重试预算")
		assert.False(t, d.sendToGranted)
	})

	t.Run("无需审批：放行但不授予 SendTo 授权", func(t *testing.T) {
		p := &Plugin{
			reg:       reg,
			cfg:       &config.Config{ToolApproval: "off"},
			approvals: execution.NewApprovalManager(),
		}
		ctx, _ := newApprovalTestContext("u1")

		d := p.decideToolInvocation(ctx, protocol.ToolCall{Name: "plain_tool"})
		assert.True(t, d.allowed)
		assert.False(t, d.sendToGranted, "嵌套 Skill 防护依赖未授权的 sender")
		assert.Empty(t, d.rejectText)
		assert.NoError(t, d.err)
	})

	t.Run("强制审批超时：按拒绝且不授予授权", func(t *testing.T) {
		p := &Plugin{
			reg:       reg,
			cfg:       &config.Config{ToolApproval: "off", ApprovalTimeout: 100 * time.Millisecond},
			approvals: execution.NewApprovalManager(),
		}
		ctx, _ := newApprovalTestContext("u1")

		d := p.decideToolInvocation(ctx, protocol.ToolCall{Name: catalog.SendToToolName})
		assert.False(t, d.allowed)
		assert.Contains(t, d.rejectText, "已被用户拒绝执行")
		assert.NoError(t, d.err, "用户拒绝不消耗重试预算")
		assert.False(t, d.sendToGranted)
	})
}
