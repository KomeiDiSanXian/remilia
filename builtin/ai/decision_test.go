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

// TestNeedActionGate 冻结装配侧的闸门策略：
//
//	auto（默认）：纯社交寒暄判否、任务请求判真、计划进行中判真；
//	always：恒真，与收紧前一致，用于恢复旧行为。
//
// 判定口径本身（白名单与保守性）见 decision/needaction_test.go。
func TestNeedActionGate(t *testing.T) {
	pendingPlan := &session.Plan{
		Task:   "分步任务",
		Active: true,
		Steps:  []session.PlanStep{{ID: "1", Description: "第一步", Status: session.PlanPending}},
	}
	cases := []struct {
		name  string
		gate  string
		query string
		plan  *session.Plan
		want  bool
	}{
		{"auto 纯社交判否", config.NeedActionAuto, "你好", nil, false},
		{"auto 闲聊判真", config.NeedActionAuto, "随便问问", nil, true},
		{"auto 任务请求判真", config.NeedActionAuto, "帮我查下明天的天气", nil, true},
		{"零值配置等同默认 auto", "", "谢谢啦", nil, false},
		{"auto 计划进行中判真", config.NeedActionAuto, "你好", pendingPlan, true},
		{"always 恒真", config.NeedActionAlways, "你好", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := decisionPlugin(t)
			p.cfg.NeedActionGate = tc.gate
			sess := decisionSession(tc.query)
			if tc.plan != nil {
				sess.SetPlan(tc.plan)
			}
			assert.Equal(t, tc.want, p.needAction(decisionCtx(tc.query), sess))
		})
	}
}

// TestNeedActionGateNilSafe 冻结装配侧的防御：nil 插件配置、nil 会话与 nil 事件
// 上下文都不得 panic——闸门可能在任何信号缺失的路径上被调用。
func TestNeedActionGateNilSafe(t *testing.T) {
	empty := &Plugin{}
	assert.True(t, empty.needAction(nil, nil), "无配置时保守判真")

	p := decisionPlugin(t)
	assert.True(t, p.needAction(nil, nil), "无会话与事件时保守判真")
	assert.True(t, p.needAction(nil, decisionSession("你好")), "无事件时保守判真")
	assert.False(t, p.needAction(decisionCtx("你好"), nil), "无会话不影响按消息判定")
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

// TestMandatorySelectionRetainedWithoutAction 冻结显式强制保留的语义：
// 即便动作不属于通用类别、也不在会话已用集合里，声明 SelectionMandatory
// 的动作在"本轮无需主动动作"时仍必须保留（SelectionMandatory 因此可达）。
func TestMandatorySelectionRetainedWithoutAction(t *testing.T) {
	p := decisionPlugin(t,
		toolkit.Tool{Name: "plain_general", Categories: []string{toolkit.CategoryGeneral}},
		toolkit.Tool{Name: "forced", Categories: []string{"web"}, Selection: new(toolkit.SelectionMandatory), Description: "强制保留"},
		toolkit.Tool{Name: "optional", Categories: []string{"web"}, Description: "可选动作"},
	)
	sess := decisionSession("随便聊聊")

	got := toolNamesOf(p.decideTurnActions(decisionCtx("随便聊聊"), sess, false))

	assert.Contains(t, got, "forced", "显式强制保留的动作不得被闸门抑制")
	assert.Contains(t, got, "plain_general")
	assert.NotContains(t, got, "optional")
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
	gpm.SetGroup("g1", &groupPolicy{ToolPolicy: &allowed})
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
	policy := &groupPolicy{}
	off := string(approvalOff)
	policy.Approval = &off
	gpm := newGroupPolicyManager(nil, "")
	gpm.SetGroup("group_1", policy)
	p.groupPolicies = gpm
	p.cfg.ToolApproval = "always"
	assert.False(t, p.approvalModeFor(ctx, "sensitive_tool"), "group policy off should override global always")

	// per-group 覆盖：群策略 approval=always 覆盖全局 off
	always := string(approvalAlways)
	policy2 := &groupPolicy{Approval: &always}
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
	reg.Register(toolkit.Tool{Name: catalog.SendToToolName, RequiresApproval: true, AlwaysRequireApproval: true, GrantsSendTo: true})

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

// TestSendToGrantBoundToDeclarativePolicy 冻结 SendTo 授权的声明式绑定：
// 一次审批只解锁动作自己声明的能力——声明 GrantsSendTo 的动作通过审批后获得
// 授权；未声明的动作即便因 always 模式被审批放行，也不获得 SendTo 授权。
func TestSendToGrantBoundToDeclarativePolicy(t *testing.T) {
	cases := []struct {
		name  string
		grant bool
	}{
		{"声明 GrantsSendTo：授予授权", true},
		{"未声明 GrantsSendTo：不授予授权", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reg := toolkit.NewToolRegistry()
			reg.Register(toolkit.Tool{
				Name:                  "risky",
				RequiresApproval:      true,
				AlwaysRequireApproval: true,
				GrantsSendTo:          tc.grant,
			})
			p := &Plugin{
				reg:       reg,
				cfg:       &config.Config{ToolApproval: "always", ApprovalTimeout: 2 * time.Second},
				approvals: execution.NewApprovalManager(),
			}
			ctx, _ := newApprovalTestContext("u1")

			done := make(chan toolInvocationDecision, 1)
			go func() {
				done <- p.decideToolInvocation(ctx, protocol.ToolCall{Name: "risky"})
			}()

			var id string
			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) && id == "" {
				if ids := p.approvals.PendingIDs(); len(ids) > 0 {
					id = ids[0]
				}
				time.Sleep(5 * time.Millisecond)
			}
			require.NotEmpty(t, id, "always 模式应发起审批")
			require.True(t, p.approvals.Resolve(id, "u1", true))

			select {
			case d := <-done:
				assert.True(t, d.allowed)
				assert.Equal(t, tc.grant, d.sendToGranted)
			case <-time.After(3 * time.Second):
				t.Fatal("decideToolInvocation 未返回")
			}
		})
	}
}
