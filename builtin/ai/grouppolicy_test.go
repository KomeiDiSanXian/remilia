package ai

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/kv"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGroupPolicyCloneEmpty(t *testing.T) {
	p := &GroupPolicy{}
	assert.True(t, p.Empty())
	assert.True(t, (&GroupPolicy{}).Empty())
	assert.True(t, (*GroupPolicy)(nil).Empty())

	s := "hi"
	on := true
	p2 := &GroupPolicy{SystemPrompt: &s, RequireMention: &on}
	assert.False(t, p2.Empty())
	cp := p2.Clone()
	assert.False(t, cp == p2)
	assert.Equal(t, "hi", *cp.SystemPrompt)
	assert.Equal(t, true, *cp.RequireMention)
	// 修改副本不影响原对象
	*cp.SystemPrompt = "changed"
	assert.Equal(t, "hi", *p2.SystemPrompt)
}

func TestGroupPolicyManager_Effective(t *testing.T) {
	m := newGroupPolicyManager(nil, "")

	// 无任何配置 → 空策略
	eff := m.Effective("group_1")
	assert.True(t, eff.Empty())

	// 设置群配置 → 命中
	prompt := "群专属提示词"
	m.SetGroup("group_1", &GroupPolicy{SystemPrompt: &prompt})
	eff = m.Effective("group_1")
	assert.Equal(t, "群专属提示词", eff.EffectiveSystemPrompt())

	// 未配置的群 → 空
	assert.Equal(t, "", m.Effective("group_2").EffectiveSystemPrompt())

	// 全局配置 → 未配置群回退到全局
	gPrompt := "全局提示词"
	m.SetGroup(globalGroupID, &GroupPolicy{SystemPrompt: &gPrompt})
	assert.Equal(t, "全局提示词", m.Effective("group_2").EffectiveSystemPrompt())
	// 已配置群优先于全局
	assert.Equal(t, "群专属提示词", m.Effective("group_1").EffectiveSystemPrompt())
}

func TestGroupPolicyManager_SetMergeFields(t *testing.T) {
	m := newGroupPolicyManager(nil, "")
	prompt := "提示词"
	m.SetGroup("g1", &GroupPolicy{SystemPrompt: &prompt})

	// 二次设置只改 tools，prompt 保留
	tools := "pic,sauce"
	m.SetGroup("g1", &GroupPolicy{ToolPolicy: &tools})
	gp := m.Get("g1")
	assert.Equal(t, "提示词", *gp.SystemPrompt)
	assert.Equal(t, "pic,sauce", *gp.ToolPolicy)

	// 清空所有字段后自动删除
	m.ResetField("g1", "prompt")
	m.ResetField("g1", "tools")
	assert.Nil(t, m.Get("g1"))
}

func TestGroupPolicyManager_Reset(t *testing.T) {
	m := newGroupPolicyManager(nil, "")
	prompt := "p"
	off := "off"
	m.SetGroup("g1", &GroupPolicy{SystemPrompt: &prompt, Approval: &off})
	m.ResetField("g1", "prompt")
	gp := m.Get("g1")
	assert.Nil(t, gp.SystemPrompt)
	assert.Equal(t, "off", *gp.Approval)

	m.ResetGroup("g1")
	assert.Nil(t, m.Get("g1"))
}

func TestGroupPolicyManager_Persistence(t *testing.T) {
	dir := t.TempDir()
	db, err := kv.Open(filepath.Join(dir, "ai"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	m := newGroupPolicyManager(db, dir)
	prompt := "持久化提示词"
	m.SetGroup("g1", &GroupPolicy{SystemPrompt: &prompt})
	// 触发落盘
	m.save()

	// 重新加载（模拟重启）
	m2 := newGroupPolicyManager(db, dir)
	assert.Equal(t, "持久化提示词", m2.Effective("g1").EffectiveSystemPrompt())
}

func TestGroupPolicyManager_EmptyStore(t *testing.T) {
	dir := t.TempDir()
	db, err := kv.Open(filepath.Join(dir, "ai"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	m := newGroupPolicyManager(db, dir)
	// 空存储加载不应 panic 且返回空
	assert.Nil(t, m.Get("g1"))
}

func TestEffectiveTools(t *testing.T) {
	// nil 策略 → 不过滤
	_, filter := (*GroupPolicy)(nil).effectiveTools()
	assert.False(t, filter)

	// all → 不过滤
	all := "all"
	assert.False(t, func() bool { _, f := (&GroupPolicy{ToolPolicy: &all}).effectiveTools(); return f }())

	// none → 过滤且空集
	none := "none"
	set, filter := (&GroupPolicy{ToolPolicy: &none}).effectiveTools()
	assert.True(t, filter)
	assert.Empty(t, set)

	// 白名单 → 过滤指定集合
	list := "pic, sauce ,bilibili"
	set, filter = (&GroupPolicy{ToolPolicy: &list}).effectiveTools()
	assert.True(t, filter)
	assert.Len(t, set, 3)
	_, ok := set["sauce"]
	assert.True(t, ok)
}

func TestFilterToolsByGroupPolicy(t *testing.T) {
	tools := []Tool{
		{Name: "pic"},
		{Name: "sauce"},
		{Name: "bilibili"},
	}

	// nil 策略 → 原样返回
	assert.Len(t, filterToolsByGroupPolicy(tools, nil), 3)

	// all → 原样
	all := "all"
	assert.Len(t, filterToolsByGroupPolicy(tools, &GroupPolicy{ToolPolicy: &all}), 3)

	// none → 空
	none := "none"
	assert.Empty(t, filterToolsByGroupPolicy(tools, &GroupPolicy{ToolPolicy: &none}))

	// 白名单 → 仅保留命中工具
	list := "pic,bilibili"
	out := filterToolsByGroupPolicy(tools, &GroupPolicy{ToolPolicy: &list})
	assert.Len(t, out, 2)
	assert.Equal(t, "pic", out[0].Name)
	assert.Equal(t, "bilibili", out[1].Name)
}

func TestGroupPolicyJSONRoundTrip(t *testing.T) {
	prompt := "提示词"
	on := true
	p := &GroupPolicy{SystemPrompt: &prompt, RequireMention: &on}
	data, err := json.Marshal(p)
	require.NoError(t, err)

	var back GroupPolicy
	require.NoError(t, json.Unmarshal(data, &back))
	assert.Equal(t, "提示词", *back.SystemPrompt)
	assert.Equal(t, true, *back.RequireMention)
}

// TestGroupPolicyStore_OpenPath 验证 OpenGroupPolicyStore 创建目录并可用。
func TestGroupPolicyStore_OpenPath(t *testing.T) {
	dir := t.TempDir()
	m, err := OpenGroupPolicyStore(dir)
	require.NoError(t, err)

	prompt := "测试"
	m.SetGroup("g1", &GroupPolicy{SystemPrompt: &prompt})
	// 关闭后才能在同一目录重新打开（LevelDB 独占锁）
	m.Close()

	m2, err := OpenGroupPolicyStore(dir)
	require.NoError(t, err)
	t.Cleanup(m2.Close)
	assert.Equal(t, "测试", m2.Effective("g1").EffectiveSystemPrompt())

	// 确认目录结构
	_, err = os.Stat(filepath.Join(dir, "ai"))
	require.NoError(t, err)
}

func TestGroupPolicyStore_CloseIdempotent(t *testing.T) {
	dir := t.TempDir()
	m, err := OpenGroupPolicyStore(dir)
	require.NoError(t, err)
	m.Close()
	m.Close() // 重复关闭不应 panic
}

// TestHandleGroupSetAndStatus 验证 /ai group 命令设置与状态查询全链路。
func TestHandleGroupSetAndStatus(t *testing.T) {
	evt := platform.NewSyntheticEvent(
		platform.EventKindGroupMessage,
		"group set tools pic,sauce",
		platform.WithSyntheticSender(platform.UserInfo{ID: "admin1", GroupRole: platform.GroupRoleAdmin}),
		platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true}),
	)
	sender := &approvalCtxSender{}
	ctx := eventctx.NewContextFromEvent(evt, sender)
	p := &Plugin{
		cfg:           &Config{TriggerCmd: "/ai", ToolApproval: "off"},
		groupPolicies: newGroupPolicyManager(nil, ""),
	}

	// 群管理员设置工具白名单
	require.NoError(t, p.handleGroupSet(ctx, []string{"tools", "pic,sauce"}))
	gp := p.groupPolicies.Get("g1")
	require.NotNil(t, gp)
	require.NotNil(t, gp.ToolPolicy)
	assert.Equal(t, "pic,sauce", *gp.ToolPolicy)

	// 非管理员被拒绝（普通成员）
	evt2 := platform.NewSyntheticEvent(
		platform.EventKindGroupMessage,
		"group set prompt x",
		platform.WithSyntheticSender(platform.UserInfo{ID: "member1", GroupRole: platform.GroupRoleMember}),
		platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true}),
	)
	ctx2 := eventctx.NewContextFromEvent(evt2, sender)
	require.NoError(t, p.handleGroupSet(ctx2, []string{"prompt", "x"}))
	assert.Nil(t, p.groupPolicies.Get("g1").SystemPrompt, "非管理员不应能修改策略")

	// 私聊场景被拒绝
	evt3 := platform.NewSyntheticEvent(
		platform.EventKindGroupMessage,
		"group status",
		platform.WithSyntheticSender(platform.UserInfo{ID: "u1"}),
		platform.WithSyntheticChat(platform.ChatInfo{ID: "user1", IsGroup: false}),
	)
	ctx3 := eventctx.NewContextFromEvent(evt3, sender)
	require.NoError(t, p.handleGroupStatus(ctx3))
}

// TestMentionedBot 验证 mentionedBot 判断。
func TestMentionedBot(t *testing.T) {
	// 未 @ 机器人（SyntheticEvent 无 mentions）
	evt := platform.NewSyntheticEvent(
		platform.EventKindGroupMessage,
		"hello",
		platform.WithSyntheticSender(platform.UserInfo{ID: "u1"}),
		platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true}),
	)
	ctx := eventctx.NewContextFromEvent(evt, nil)
	assert.False(t, mentionedBot(ctx))

	// nil 事件
	assert.False(t, mentionedBot(nil))
}

// TestGroupRequireMention 验证 per-group mention 判定。
func TestGroupRequireMention(t *testing.T) {
	evt := platform.NewSyntheticEvent(
		platform.EventKindGroupMessage,
		"hello",
		platform.WithSyntheticSender(platform.UserInfo{ID: "u1"}),
		platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true}),
	)
	ctx := eventctx.NewContextFromEvent(evt, nil)
	p := &Plugin{groupPolicies: newGroupPolicyManager(nil, "")}

	// 未配置 → ok=false
	_, ok := p.groupRequireMention(ctx)
	assert.False(t, ok)

	// mention=on → (true, true)
	on := true
	p.groupPolicies.SetGroup("g1", &GroupPolicy{RequireMention: &on})
	require, ok := p.groupRequireMention(ctx)
	assert.True(t, ok)
	assert.True(t, require)

	// mention=off → (false, true)
	off := false
	p.groupPolicies.SetGroup("g1", &GroupPolicy{RequireMention: &off})
	require, ok = p.groupRequireMention(ctx)
	assert.True(t, ok)
	assert.False(t, require)

	// 私聊场景 → ok=false
	evt2 := platform.NewSyntheticEvent(
		platform.EventKindGroupMessage,
		"hello",
		platform.WithSyntheticSender(platform.UserInfo{ID: "u1"}),
		platform.WithSyntheticChat(platform.ChatInfo{ID: "u1", IsGroup: false}),
	)
	ctx2 := eventctx.NewContextFromEvent(evt2, nil)
	_, ok = p.groupRequireMention(ctx2)
	assert.False(t, ok)
}
