package ai

import (
	"strings"
	"testing"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/permission"
	"github.com/KomeiDiSanXian/remilia/platform"
)

func makeManageCtx(content string, isGroup bool) *eventctx.Context {
	return makeManageCtxRole(content, isGroup, platform.GroupRoleUnknown)
}

func makeManageCtxRole(content string, isGroup bool, role platform.GroupRole) *eventctx.Context {
	chatID := "chat"
	kind := platform.EventKindPrivateMessage
	if isGroup {
		kind = platform.EventKindGroupMessage
		chatID = "group-1"
	}
	evt := platform.NewSyntheticEvent(kind, content,
		platform.WithSyntheticSender(platform.UserInfo{ID: "user", DisplayName: "User", GroupRole: role}),
		platform.WithSyntheticChat(platform.ChatInfo{ID: chatID, IsGroup: isGroup}),
	)
	return eventctx.NewContextFromEvent(evt, nil)
}

func newManagePlugin(t *testing.T) *Plugin {
	t.Helper()
	mem, err := OpenMemoryStore(t.TempDir(), 50, time.Minute)
	if err != nil {
		t.Fatalf("OpenMemoryStore: %v", err)
	}
	t.Cleanup(mem.Close)
	return &Plugin{
		sm:     NewSessionManager(100, 20, time.Hour, nil),
		cfg:    &Config{TriggerCmd: "/ai", Markdown: true, MemoryMaxFacts: 50},
		memory: mem,
		todos:  newTodoManager(),
	}
}

// --- /ai memory ---

func TestMemoryCommandDisabled(t *testing.T) {
	p := &Plugin{cfg: &Config{}}
	if err := p.handleMemoryCommand(makeManageCtx("/ai memory", false), ""); err != nil {
		t.Fatalf("handleMemoryCommand: %v", err)
	}
}

func TestMemoryListAndClearUser(t *testing.T) {
	p := newManagePlugin(t)
	ctx := makeManageCtx("/ai memory", false)
	p.memory.Add(userScope("user"), "用户喜欢喝冰美式")
	p.memory.Add(userScope("user"), "用户的猫叫咪咪")

	if err := p.handleMemoryList(ctx); err != nil {
		t.Fatalf("handleMemoryList: %v", err)
	}
	if got := len(p.memory.Facts(userScope("user"))); got != 2 {
		t.Fatalf("facts = %d, want 2", got)
	}

	if err := p.handleMemoryCommand(ctx, "clear"); err != nil {
		t.Fatalf("handleMemoryCommand clear: %v", err)
	}
	if got := len(p.memory.Facts(userScope("user"))); got != 0 {
		t.Fatalf("after clear facts = %d, want 0", got)
	}
}

func TestMemoryListGroupShowsGroupFacts(t *testing.T) {
	p := newManagePlugin(t)
	ctx := makeManageCtx("/ai memory", true)
	p.memory.Add(groupScope("group-1"), "本群规则：禁止剧透")

	if err := p.handleMemoryList(ctx); err != nil {
		t.Fatalf("handleMemoryList: %v", err)
	}
	if got := len(p.memory.Facts(groupScope("group-1"))); got != 1 {
		t.Fatalf("group facts = %d, want 1", got)
	}
}

func TestMemoryClearGroupRequiresGroupAdmin(t *testing.T) {
	p := newManagePlugin(t)
	ctx := makeManageCtx("/ai memory clear group", true)
	p.memory.Add(groupScope("group-1"), "本群规则：禁止剧透")

	// 普通成员 → 应拒绝且保留事实。
	if err := p.handleMemoryClear(ctx, "group"); err != nil {
		t.Fatalf("handleMemoryClear: %v", err)
	}
	if got := len(p.memory.Facts(groupScope("group-1"))); got != 1 {
		t.Fatalf("group facts after denied clear = %d, want 1", got)
	}
}

func TestMemoryClearGroupByOwner(t *testing.T) {
	p := newManagePlugin(t)
	ctx := makeManageCtxRole("/ai memory clear group", true, platform.GroupRoleOwner)
	p.memory.Add(groupScope("group-1"), "本群规则：禁止剧透")

	// 群主 → 允许清空。
	if err := p.handleMemoryClear(ctx, "group"); err != nil {
		t.Fatalf("handleMemoryClear: %v", err)
	}
	if got := len(p.memory.Facts(groupScope("group-1"))); got != 0 {
		t.Fatalf("group facts after owner clear = %d, want 0", got)
	}
}

// TestMemoryClearGroupByRBACRole 固定 RBAC 通道：拥有 superadmin / admin 角色的
// 用户即使没有平台群主/管理员身份（GroupRole = Unknown），也能清空本群公共记忆。
//
// 该通道完全依赖 ctx.GetPermissionManager()（由 Bot 在事件入口注入），
// 因此它同时是“事件 Context 必须带上权限管理器”这条接线的回归测试：
// 一旦注入断掉，本用例与线上现象一致——超管被判为无权、群记忆删不掉。
func TestMemoryClearGroupByRBACRole(t *testing.T) {
	for _, role := range []string{"superadmin", "admin"} {
		t.Run(role, func(t *testing.T) {
			p := newManagePlugin(t)

			pm := eventctx.NewPermissionManager()
			pm.RegisterRole(permission.NewRole(role, permission.Permission{Resource: "*", Action: "*"}))
			if err := pm.AssignRole("user", role); err != nil {
				t.Fatalf("AssignRole(%s): %v", role, err)
			}

			// 平台侧无群主/管理员身份：只能靠 RBAC 角色放行。
			ctx := makeManageCtx("/ai memory clear group", true)
			ctx.SetPermissionManager(pm)
			p.memory.Add(groupScope("group-1"), "本群规则：禁止剧透")

			if err := p.handleMemoryClear(ctx, "group"); err != nil {
				t.Fatalf("handleMemoryClear: %v", err)
			}
			if got := len(p.memory.Facts(groupScope("group-1"))); got != 0 {
				t.Fatalf("角色 %s 应可清空群记忆，剩余 %d 条", role, got)
			}
		})
	}
}

// TestMemoryClearGroupByRBACRoleWithoutManager 固定未注入权限管理器时的
// fail-closed 语义：角色查不到 → 拒绝，群记忆保留。
func TestMemoryClearGroupByRBACRoleWithoutManager(t *testing.T) {
	p := newManagePlugin(t)
	ctx := makeManageCtx("/ai memory clear group", true)
	p.memory.Add(groupScope("group-1"), "本群规则：禁止剧透")

	if err := p.handleMemoryClear(ctx, "group"); err != nil {
		t.Fatalf("handleMemoryClear: %v", err)
	}
	if got := len(p.memory.Facts(groupScope("group-1"))); got != 1 {
		t.Fatalf("无权限管理器时应拒绝清空，剩余 %d 条", got)
	}
}

func TestMemoryClearInvalidScope(t *testing.T) {
	p := newManagePlugin(t)
	if err := p.handleMemoryCommand(makeManageCtx("/ai memory", false), "clear bogus"); err != nil {
		t.Fatalf("handleMemoryCommand: %v", err)
	}
}

func TestMemoryRemoveByIndex(t *testing.T) {
	p := newManagePlugin(t)
	p.memory.Add(userScope("user"), "用户喜欢喝冰美式")
	p.memory.Add(userScope("user"), "用户的猫叫咪咪")
	p.memory.Add(userScope("user"), "用户下周出差")

	// 列表顺序由 UpdatedAt 决定（同一 tick 内时间戳可能相同，稳定排序
	// 保持插入序），因此不假设"第 2 条"是哪一条，改为自洽断言：
	// 删除后应恰好少一条，且被删的正是删除前快照中的第 2 条。
	before := p.memory.Facts(userScope("user"))
	if len(before) != 3 {
		t.Fatalf("facts = %d, want 3", len(before))
	}
	wantRemoved := before[1].Text

	if err := p.handleMemoryCommand(makeManageCtx("/ai memory", false), "remove 2"); err != nil {
		t.Fatalf("remove 2: %v", err)
	}
	after := p.memory.Facts(userScope("user"))
	if len(after) != 2 {
		t.Fatalf("facts = %d, want 2", len(after))
	}
	for _, f := range after {
		if f.Text == wantRemoved {
			t.Fatalf("index 2 (%q) should be removed, got %+v", wantRemoved, after)
		}
	}
}

func TestMemoryRemoveByText(t *testing.T) {
	p := newManagePlugin(t)
	p.memory.Add(userScope("user"), "用户喜欢喝冰美式")
	p.memory.Add(userScope("user"), "用户不喜欢喝热美式")
	p.memory.Add(userScope("user"), "用户的猫叫咪咪")

	if err := p.handleMemoryCommand(makeManageCtx("/ai memory", false), "remove 美式"); err != nil {
		t.Fatalf("remove 美式: %v", err)
	}
	facts := p.memory.Facts(userScope("user"))
	if len(facts) != 1 {
		t.Fatalf("facts = %d, want 1（仅剩咪咪）: %+v", len(facts), facts)
	}
}

func TestMemoryRemoveGroupScope(t *testing.T) {
	p := newManagePlugin(t)
	p.memory.Add(groupScope("group-1"), "本群规则：禁止剧透")
	p.memory.Add(groupScope("group-1"), "本群每周六有活动")

	if err := p.handleMemoryCommand(makeManageCtx("/ai memory", true), "remove group 剧透"); err != nil {
		t.Fatalf("remove group 剧透: %v", err)
	}
	facts := p.memory.Facts(groupScope("group-1"))
	if len(facts) != 1 {
		t.Fatalf("group facts = %d, want 1: %+v", len(facts), facts)
	}
}

func TestMemoryRemoveInvalid(t *testing.T) {
	p := newManagePlugin(t)
	if err := p.handleMemoryCommand(makeManageCtx("/ai memory", false), "remove"); err != nil {
		t.Fatalf("remove without target: %v", err)
	}
	if err := p.handleMemoryCommand(makeManageCtx("/ai memory", false), "remove 99"); err != nil {
		t.Fatalf("remove out of range: %v", err)
	}
}

func TestMemoryHelpText(t *testing.T) {
	if h := memoryHelpText("/ai"); !strings.Contains(h, "/ai memory") {
		t.Errorf("memoryHelpText missing usage: %q", h)
	}
	if h := memoryHelpText("/ai"); !strings.Contains(h, "remove") {
		t.Errorf("memoryHelpText missing remove usage: %q", h)
	}
}

// --- /ai todo ---

func TestTodoLifecycle(t *testing.T) {
	p := newManagePlugin(t)
	ctx := makeManageCtx("/ai todo", false)

	if err := p.handleTodoCommand(ctx, "add 买菜"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := p.handleTodoCommand(ctx, "add 写周报"); err != nil {
		t.Fatalf("add 2: %v", err)
	}
	items := p.todos.list("chat")
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}

	if err := p.handleTodoCommand(ctx, "done T1"); err != nil {
		t.Fatalf("done: %v", err)
	}
	items = p.todos.list("chat")
	if !items[0].Done {
		t.Fatalf("T1 should be done, got %+v", items)
	}

	if err := p.handleTodoCommand(ctx, "clear"); err != nil {
		t.Fatalf("clear done: %v", err)
	}
	items = p.todos.list("chat")
	if len(items) != 1 || items[0].ID != "T2" {
		t.Fatalf("after clear done items = %+v, want only T2", items)
	}

	if err := p.handleTodoCommand(ctx, "remove T2"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if items = p.todos.list("chat"); len(items) != 0 {
		t.Fatalf("after remove items = %+v, want empty", items)
	}
}

func TestTodoAddThenClearAll(t *testing.T) {
	p := newManagePlugin(t)
	ctx := makeManageCtx("/ai todo", false)
	_ = p.handleTodoCommand(ctx, "add 任务A")
	_ = p.handleTodoCommand(ctx, "add 任务B")
	if err := p.handleTodoCommand(ctx, "clear all"); err != nil {
		t.Fatalf("clear all: %v", err)
	}
	if items := p.todos.list("chat"); len(items) != 0 {
		t.Fatalf("items after clear all = %+v, want empty", items)
	}
}

func TestTodoMissingArgs(t *testing.T) {
	p := newManagePlugin(t)
	ctx := makeManageCtx("/ai todo", false)
	if err := p.handleTodoCommand(ctx, "add"); err != nil {
		t.Fatalf("add without arg: %v", err)
	}
	if err := p.handleTodoCommand(ctx, "done"); err != nil {
		t.Fatalf("done without id: %v", err)
	}
	if err := p.handleTodoCommand(ctx, "remove"); err != nil {
		t.Fatalf("remove without id: %v", err)
	}
	if err := p.handleTodoCommand(ctx, "clear bogus"); err != nil {
		t.Fatalf("clear bogus: %v", err)
	}
}

func TestTodoHelpText(t *testing.T) {
	if h := todoHelpText("/ai"); !strings.Contains(h, "/ai todo add") {
		t.Errorf("todoHelpText missing usage: %q", h)
	}
}

// --- /ai plan ---

func TestPlanStatusAndCancel(t *testing.T) {
	p := newManagePlugin(t)
	session := p.sm.GetOrCreate("synthetic:chat:user", "user", "chat")
	session.setPlan(&Plan{
		Task:   "整理报告",
		Active: true,
		Steps: []PlanStep{
			{ID: "step_1", Description: "收集数据", Status: PlanDone},
			{ID: "step_2", Description: "撰写报告", Status: PlanInProgress},
		},
	})
	ctx := makeManageCtx("/ai plan", false)

	if err := p.handlePlanCommand(ctx, ""); err != nil {
		t.Fatalf("status: %v", err)
	}
	if err := p.handlePlanCommand(ctx, "cancel"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	snap := session.planSnapshot()
	if snap == nil || snap.Active {
		t.Fatalf("plan should be inactive after cancel, got %+v", snap)
	}
}

func TestPlanNoPlan(t *testing.T) {
	p := newManagePlugin(t)
	ctx := makeManageCtx("/ai plan", false)
	if err := p.handlePlanCommand(ctx, ""); err != nil {
		t.Fatalf("status with no plan: %v", err)
	}
	if err := p.handlePlanCommand(ctx, "cancel"); err != nil {
		t.Fatalf("cancel with no plan: %v", err)
	}
}

func TestPlanHelpText(t *testing.T) {
	if h := planHelpText("/ai"); !strings.Contains(h, "/ai plan cancel") {
		t.Errorf("planHelpText missing usage: %q", h)
	}
}

func TestSubCommandRest(t *testing.T) {
	p := &Plugin{triggerCmd: "/ai"}
	cases := []struct {
		content  string
		prefixes []string
		want     string
	}{
		{"/ai memory clear group", []string{"memory", "记忆"}, "clear group"},
		{"@123 待办 添加 买菜", []string{"todo", "待办"}, "添加 买菜"},
		{"/ai plan", []string{"plan", "计划"}, ""},
	}
	for _, tc := range cases {
		ctx := makeManageCtx(tc.content, false)
		if got := p.subCommandRest(ctx, tc.prefixes...); got != tc.want {
			t.Errorf("subCommandRest(%q) = %q, want %q", tc.content, got, tc.want)
		}
	}
}
