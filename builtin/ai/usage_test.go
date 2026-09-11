package ai

import (
	"strings"
	"testing"
	"time"

	permissionplugin "github.com/KomeiDiSanXian/remilia/builtin/core/permission"
	"github.com/KomeiDiSanXian/remilia/command"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/permission"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// --- 测试辅助 ---

// mentionsEvent 在合成事件之上补一个 Mentions 实现，用于覆盖"结构化 @ 列表"
// 这条目标解析路径（SyntheticEvent 本身不支持 mentions 数组）。
type mentionsEvent struct {
	*platform.SyntheticEvent
	mentions []platform.UserInfo
}

func (e *mentionsEvent) Mentions() []platform.UserInfo { return e.mentions }

// newUsageCtx 构造查询场景的事件上下文。
//
// isGroup=true 时为群聊（群 ID "group-1"），否则为私聊（会话 ID 即用户 ID）；
// senderID 既是发送者也是私聊会话的另一方。senderRole 用于驱动平台群角色
// 授权分支。mentions 非空时将事件包装为携带结构化 @ 列表的事件。
func newUsageCtx(content, senderID string, isGroup bool, senderRole platform.GroupRole, mentions ...platform.UserInfo) (*eventctx.Context, *approvalCtxSender) {
	kind := platform.EventKindPrivateMessage
	chat := platform.ChatInfo{ID: senderID}
	if isGroup {
		kind = platform.EventKindGroupMessage
		chat = platform.ChatInfo{ID: "group-1", IsGroup: true}
	}
	evt := platform.NewSyntheticEvent(kind, content,
		platform.WithSyntheticPlatform("qq"),
		platform.WithSyntheticSender(platform.UserInfo{ID: senderID, DisplayName: senderID, GroupRole: senderRole}),
		platform.WithSyntheticChat(chat),
	)
	var event platform.Event = evt
	if len(mentions) > 0 {
		event = &mentionsEvent{SyntheticEvent: evt, mentions: mentions}
	}
	sender := &approvalCtxSender{}
	ctx := eventctx.NewContextFromEvent(event, sender)
	ctx.SetDispatcher(runTaskDispatcher{})
	return ctx, sender
}

// newUsagePlugin 构造带最小依赖的插件实例（会话管理器 + 触发前缀）。
func newUsagePlugin() *Plugin {
	return &Plugin{
		sm:         NewSessionManager(100, 20, time.Hour, nil),
		cfg:        &Config{TriggerCmd: "/ai", Markdown: true},
		triggerCmd: "/ai",
	}
}

// setParsed 用真实命令定义解析 content 并挂到上下文，覆盖 /ai 命令路径
// （同时回归 status/stats 的 target 参数声明是否仍能被解析器绑定）。
func setParsed(t *testing.T, ctx *eventctx.Context, content string) {
	t.Helper()
	parsed, err := command.ParseFromDefinition(content, buildAIDefinition(), "/")
	if err != nil {
		t.Fatalf("解析 %q 失败: %v", content, err)
	}
	ctx.SetParsedCommand(parsed)
}

// newSuperadminManager 返回注册了 superadmin 角色的权限管理器。
// core/permission 默认只注册 admin/user/guest，superadmin 需显式注册。
func newSuperadminManager() *permission.Manager {
	pm := permission.NewPermissionManager()
	pm.RegisterRole(permission.NewRole("superadmin",
		permission.Permission{Resource: "*", Action: "*"}))
	return pm
}

// lastReply 返回捕获到的最后一条回复文本（无回复返回 ""）。
func lastReply(sender *approvalCtxSender) string {
	sender.mu.Lock()
	defer sender.mu.Unlock()
	if len(sender.replies) == 0 {
		return ""
	}
	msg := sender.replies[len(sender.replies)-1]
	if msg.Text != "" {
		return msg.Text
	}
	return msg.Markdown
}

// --- looksLikeUserID ---

func TestLooksLikeUserID(t *testing.T) {
	cases := []struct {
		token string
		want  bool
	}{
		{"12345", true},         // QQ / Telegram 数字 ID
		{"user_123", true},      // 带分隔符的 openid
		{"9f8a7b6c-1234", true}, // 含数字的 UUID 片段
		{"openid:101", true},    // 带冒号前缀
		{"update", false},       // 英文单词：无数字
		{"detailed", false},     // 更长的英文单词同样拒绝
		{"是什么", false},          // CJK：自然语言
		{"abc", false},          // 过短
		{"12", false},           // 过短
		{"用户123", false},        // 含 CJK：按自然语言处理
		{"foo bar", false},      // 含空白（调用方已按 token 传入，防御性）
		{"emoji🎉123", false},    // 含非 ID 字符
		{"", false},             //
	}
	for _, tc := range cases {
		if got := looksLikeUserID(tc.token); got != tc.want {
			t.Errorf("looksLikeUserID(%q) = %v, want %v", tc.token, got, tc.want)
		}
	}
}

// --- matchUsageSubCommand ---

func TestMatchUsageSubCommand(t *testing.T) {
	cases := []struct {
		cmd  string
		want string
	}{
		{"status", "status"},
		{"status 12345", "status"},
		{"status @张三", "status"},
		{"状态 12345", "status"},
		{"stats", "stats"},
		{"stats @张三", "stats"},
		{"统计 12345", "stats"},
		{"statuses", ""},   // 整词匹配：不误命中复数形式
		{"statistics", ""}, // 不误命中相似词
		{"查看状态", ""},       // 中文别名要求整词
		{"status更新", ""},   // 缺少分隔空格
		{"", ""},
		{"帮我看看 stats", ""}, // 非前缀
	}
	for _, tc := range cases {
		if got := matchUsageSubCommand(tc.cmd); got != tc.want {
			t.Errorf("matchUsageSubCommand(%q) = %q, want %q", tc.cmd, got, tc.want)
		}
	}
}

func TestUsageSubCommandAliases(t *testing.T) {
	if got := usageSubCommandAliases("status"); len(got) == 0 || got[0] != "status" {
		t.Errorf("status aliases = %v, want 以 status 开头", got)
	}
	if got := usageSubCommandAliases("stats"); len(got) == 0 || got[0] != "stats" {
		t.Errorf("stats aliases = %v, want 以 stats 开头", got)
	}
	if got := usageSubCommandAliases("unknown"); got != nil {
		t.Errorf("未知子命令应返回 nil，得到 %v", got)
	}
}

// --- resolveUsageTarget ---

func TestResolveUsageTarget_PrefersMentions(t *testing.T) {
	p := newUsagePlugin()
	ctx, _ := newUsageCtx("/ai status 12345", "admin", true, platform.GroupRoleAdmin,
		platform.UserInfo{ID: "bot", IsSelf: true}, // 机器人自身应被跳过
		platform.UserInfo{ID: "12345", DisplayName: "张三"},
	)

	id, name, ok := p.resolveUsageTarget(ctx, "12345")
	if !ok {
		t.Fatal("应解析出目标用户")
	}
	if id != "12345" {
		t.Errorf("ID = %q, want 12345（结构化 @ 优先于显式 ID）", id)
	}
	if name != "张三" {
		t.Errorf("DisplayName = %q, want 张三", name)
	}
}

func TestResolveUsageTarget_FallsBackToExplicitID(t *testing.T) {
	p := newUsagePlugin()
	ctx, _ := newUsageCtx("/ai status 12345", "admin", true, platform.GroupRoleAdmin)

	id, name, ok := p.resolveUsageTarget(ctx, "12345")
	if !ok || id != "12345" || name != "" {
		t.Fatalf("got (%q, %q, %v), want (\"12345\", \"\", true)", id, name, ok)
	}
}

func TestResolveUsageTarget_AcceptsLeadingAt(t *testing.T) {
	p := newUsagePlugin()
	ctx, _ := newUsageCtx("/ai status @12345", "admin", true, platform.GroupRoleAdmin)

	id, _, ok := p.resolveUsageTarget(ctx, "@12345")
	if !ok || id != "12345" {
		t.Fatalf("got (%q, %v), want (\"12345\", true)", id, ok)
	}
}

func TestResolveUsageTarget_RejectsNaturalLanguage(t *testing.T) {
	p := newUsagePlugin()
	ctx, _ := newUsageCtx("/ai status 是什么", "admin", true, platform.GroupRoleAdmin)

	for _, rest := range []string{"", "是什么", "update", "详细说一下", "帮我看看"} {
		if id, _, ok := p.resolveUsageTarget(ctx, rest); ok {
			t.Errorf("resolveUsageTarget(%q) 不应解析出目标，得到 %q", rest, id)
		}
	}
}

// --- authorizeUsageQuery ---

func TestAuthorizeUsageQuery_GroupAdminAllowed(t *testing.T) {
	p := newUsagePlugin()
	pm := eventctx.NewPermissionManager()
	ctx, _ := newUsageCtx("/ai status 12345", "owner", true, platform.GroupRoleOwner)
	ctx.SetPermissionManager(pm)

	allowed, reason := p.authorizeUsageQuery(ctx, "12345")
	if !allowed {
		t.Fatalf("群主应可查询本群成员，被拒: %s", reason)
	}
}

func TestAuthorizeUsageQuery_GroupMemberDenied(t *testing.T) {
	p := newUsagePlugin()
	pm := eventctx.NewPermissionManager()
	ctx, _ := newUsageCtx("/ai status 12345", "member", true, platform.GroupRoleMember)
	ctx.SetPermissionManager(pm)

	allowed, reason := p.authorizeUsageQuery(ctx, "12345")
	if allowed {
		t.Fatal("普通群成员不应可查询他人")
	}
	if !strings.Contains(reason, "群管理员") {
		t.Errorf("拒绝原因应提示需要群管理员角色，得到 %q", reason)
	}
}

func TestAuthorizeUsageQuery_PrivateNonAdminDenied(t *testing.T) {
	p := newUsagePlugin()
	ctx, _ := newUsageCtx("/ai status 12345", "u1", false, platform.GroupRoleUnknown)
	ctx.SetPermissionManager(eventctx.NewPermissionManager())

	allowed, reason := p.authorizeUsageQuery(ctx, "12345")
	if allowed {
		t.Fatal("私聊普通用户不应可查询他人")
	}
	if !strings.Contains(reason, "管理员") {
		t.Errorf("拒绝原因应提示需要管理员权限，得到 %q", reason)
	}
}

func TestAuthorizeUsageQuery_RBACAdminAllowed(t *testing.T) {
	p := newUsagePlugin()
	pm := eventctx.NewPermissionManager()
	if err := pm.AssignRole("admin1", "admin"); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}
	ctx, _ := newUsageCtx("/ai status 12345", "admin1", true, platform.GroupRoleMember)
	ctx.SetPermissionManager(pm)

	allowed, reason := p.authorizeUsageQuery(ctx, "12345")
	if !allowed {
		t.Fatalf("RBAC admin 即使只是普通群成员也应可查询，被拒: %s", reason)
	}
}

func TestAuthorizeUsageQuery_DelegatedPermissionAllowed(t *testing.T) {
	permPlugin := permissionplugin.NewPlugin()
	if err := permPlugin.GrantEx("ops1", "ai.usage", "view"); err != nil {
		t.Fatalf("GrantEx: %v", err)
	}

	p := newUsagePlugin()
	p.perms = permPlugin
	ctx, _ := newUsageCtx("/ai status 12345", "ops1", false, platform.GroupRoleUnknown)

	if allowed, reason := p.authorizeUsageQuery(ctx, "12345"); !allowed {
		t.Fatalf("持有 %s 的用户应可查询，被拒: %s", aiUsageViewPermission, reason)
	}
}

func TestAuthorizeUsageQuery_SuperadminTargetProtected(t *testing.T) {
	p := newUsagePlugin()
	pm := newSuperadminManager()
	if err := pm.AssignRole("admin1", "admin"); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}
	if err := pm.AssignRole("boss", "superadmin"); err != nil {
		t.Fatalf("AssignRole superadmin: %v", err)
	}

	// admin 查 superadmin：拒绝
	ctx, _ := newUsageCtx("/ai status boss", "admin1", true, platform.GroupRoleMember)
	ctx.SetPermissionManager(pm)
	if allowed, reason := p.authorizeUsageQuery(ctx, "boss"); allowed {
		t.Fatal("admin 不应可查询 superadmin")
	} else if !strings.Contains(reason, "超级管理员") {
		t.Errorf("拒绝原因应提示超级管理员限制，得到 %q", reason)
	}

	// superadmin 查 superadmin：放行
	ctx2, _ := newUsageCtx("/ai status boss", "boss", true, platform.GroupRoleMember)
	ctx2.SetPermissionManager(pm)
	if allowed, reason := p.authorizeUsageQuery(ctx2, "boss"); !allowed {
		t.Fatalf("superadmin 应可查询 superadmin，被拒: %s", reason)
	}
}

func TestAuthorizeUsageQuery_NoPermissionSourceDenied(t *testing.T) {
	p := newUsagePlugin()
	// 私聊 + 未接线任何权限来源 → fail-closed
	ctx, _ := newUsageCtx("/ai status 12345", "u1", false, platform.GroupRoleUnknown)

	allowed, reason := p.authorizeUsageQuery(ctx, "12345")
	if allowed {
		t.Fatal("无权限来源时应拒绝")
	}
	if !strings.Contains(reason, "权限系统未初始化") {
		t.Errorf("拒绝原因应为权限系统未初始化，得到 %q", reason)
	}
}

// --- usageSessionID ---

func TestUsageSessionID(t *testing.T) {
	group := platform.ChatInfo{ID: "group-1", IsGroup: true}
	if got, want := usageSessionID("qq", group, "12345"), "qq:group-1:12345"; got != want {
		t.Errorf("群聊会话 ID = %q, want %q", got, want)
	}
	// 私聊：ChatInfo.ID 即用户 ID，故目标与机器人的会话为 {platform}:{目标}:{目标}
	private := platform.ChatInfo{ID: "12345"}
	if got, want := usageSessionID("qq", private, "12345"), "qq:12345:12345"; got != want {
		t.Errorf("私聊会话 ID = %q, want %q", got, want)
	}
}

// --- usageSummaryText ---

func TestUsageSummaryText_NoSession(t *testing.T) {
	p := newUsagePlugin()
	ctx, _ := newUsageCtx("/ai status 12345", "owner", true, platform.GroupRoleOwner)

	out := p.usageSummaryText(ctx, "12345", "张三")
	if !strings.Contains(out, "张三") || !strings.Contains(out, "12345") {
		t.Errorf("摘要应包含目标标识: %s", out)
	}
	if !strings.Contains(out, "无") {
		t.Errorf("无会话时应说明没有记录: %s", out)
	}
	if strings.Contains(out, "LLM 调用次数") {
		t.Errorf("无会话时不应输出调用统计: %s", out)
	}
}

func TestUsageSummaryText_WithSessionCounts(t *testing.T) {
	p := newUsagePlugin()
	ctx, _ := newUsageCtx("/ai status 12345", "owner", true, platform.GroupRoleOwner)

	session := p.sm.GetOrCreate("qq:group-1:12345", "12345", "group-1")
	p.sm.AppendMessage(session, Message{Role: RoleSystem, Content: "sys"})
	p.sm.AppendMessage(session, Message{Role: RoleUser, Content: "hello"})
	session.Lock()
	session.CallCount = 3
	session.ToolCount = 2
	session.Unlock()

	out := p.usageSummaryText(ctx, "12345", "")
	if !strings.Contains(out, "`3`") {
		t.Errorf("摘要应包含 LLM 调用次数 3: %s", out)
	}
	if !strings.Contains(out, "`2`") {
		t.Errorf("摘要应包含工具调用次数 2: %s", out)
	}
	if !strings.Contains(out, "`2`（含 1 条系统提示）") {
		t.Errorf("摘要应包含消息数明细: %s", out)
	}
	// 隐私边界：只出计数，不泄漏对话正文
	if strings.Contains(out, "hello") {
		t.Errorf("摘要不应包含对话正文: %s", out)
	}
}

func TestUsageSummaryText_DoesNotCreateSession(t *testing.T) {
	p := newUsagePlugin()
	ctx, _ := newUsageCtx("/ai status 12345", "owner", true, platform.GroupRoleOwner)

	if s := p.sm.Peek("qq:group-1:12345"); s != nil {
		t.Fatal("前置条件：目标会话不应存在")
	}
	_ = p.usageSummaryText(ctx, "12345", "")

	if s := p.sm.Peek("qq:group-1:12345"); s != nil {
		t.Fatal("只读摘要不应为目标用户创建会话")
	}
}

// --- handleUsageQuery ---

func TestHandleUsageQuery_SelfFallsThrough(t *testing.T) {
	p := newUsagePlugin()
	ctx, _ := newUsageCtx("/ai status 12345", "12345", true, platform.GroupRoleMember)
	ctx.SetPermissionManager(eventctx.NewPermissionManager())

	if p.handleUsageQuery(ctx, "12345") {
		t.Fatal("查询自己应返回 false，交回原有自身查询逻辑")
	}
}

func TestHandleUsageQuery_NoTargetFallsThrough(t *testing.T) {
	p := newUsagePlugin()
	ctx, _ := newUsageCtx("/ai status", "u1", true, platform.GroupRoleMember)
	ctx.SetPermissionManager(eventctx.NewPermissionManager())

	if p.handleUsageQuery(ctx, "") {
		t.Fatal("无目标时应返回 false")
	}
}

func TestHandleUsageQuery_AdminGetsSummary(t *testing.T) {
	p := newUsagePlugin()
	pm := eventctx.NewPermissionManager()
	if err := pm.AssignRole("admin1", "admin"); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}
	ctx, sender := newUsageCtx("/ai status 12345", "admin1", true, platform.GroupRoleMember)
	ctx.SetPermissionManager(pm)

	if !p.handleUsageQuery(ctx, "12345") {
		t.Fatal("admin 查询他人应被处理")
	}
	waitReplies(t, sender, 1)
	if out := lastReply(sender); !strings.Contains(out, "12345") {
		t.Errorf("回复应包含目标用户: %q", out)
	}
}

func TestHandleUsageQuery_MemberDeniedAndHandled(t *testing.T) {
	p := newUsagePlugin()
	ctx, sender := newUsageCtx("/ai status 12345", "member", true, platform.GroupRoleMember)
	ctx.SetPermissionManager(eventctx.NewPermissionManager())

	if !p.handleUsageQuery(ctx, "12345") {
		t.Fatal("拒绝也应视为已处理，避免继续走 AI 对话")
	}
	waitReplies(t, sender, 1)
	out := lastReply(sender)
	if !strings.Contains(out, "群管理员") {
		t.Errorf("应回复权限不足提示: %q", out)
	}
	if strings.Contains(out, "LLM 调用次数") {
		t.Errorf("拒绝时不得输出任何用量数据: %q", out)
	}
}

// --- 路径接线 ---

func TestExecSubCommand_StatusAndStatsWithTarget(t *testing.T) {
	// 走真实命令解析（/ai status <id>）：同时回归 status/stats 声明了 target
	// 位置参数，命令路径不再依赖触发前缀的文字清洗。
	for _, sub := range []string{"status", "stats"} {
		p := newUsagePlugin()
		pm := eventctx.NewPermissionManager()
		if err := pm.AssignRole("admin1", "admin"); err != nil {
			t.Fatalf("AssignRole: %v", err)
		}
		content := "/ai " + sub + " 12345"
		ctx, sender := newUsageCtx(content, "admin1", true, platform.GroupRoleMember)
		ctx.SetPermissionManager(pm)
		setParsed(t, ctx, content)

		if err := p.execSubCommand(ctx, sub); err != nil {
			t.Fatalf("execSubCommand(%s): %v", sub, err)
		}
		waitReplies(t, sender, 1)
		out := lastReply(sender)
		if !strings.Contains(out, "12345") {
			t.Errorf("%s 带目标应走他人查询，得到 %q", sub, out)
		}
	}
}

// TestExecSubCommand_StatusTargetFromMention 验证 /ai status 的 @ 形式：
// @ 由结构化 mentions 提供，不依赖 target 位置参数。
func TestExecSubCommand_StatusTargetFromMention(t *testing.T) {
	p := newUsagePlugin()
	pm := eventctx.NewPermissionManager()
	if err := pm.AssignRole("admin1", "admin"); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}
	ctx, sender := newUsageCtx("/ai status", "admin1", true, platform.GroupRoleMember,
		platform.UserInfo{ID: "12345", DisplayName: "张三"})
	ctx.SetPermissionManager(pm)
	setParsed(t, ctx, "/ai status")

	if err := p.execSubCommand(ctx, "status"); err != nil {
		t.Fatalf("execSubCommand(status): %v", err)
	}
	waitReplies(t, sender, 1)
	out := lastReply(sender)
	if !strings.Contains(out, "张三") || !strings.Contains(out, "12345") {
		t.Errorf("应查询 @ 的目标用户，得到 %q", out)
	}
}

func TestExecSubCommand_StatusSelfUnchanged(t *testing.T) {
	p := newUsagePlugin()
	ctx, sender := newUsageCtx("/ai status", "u1", false, platform.GroupRoleUnknown)

	session := p.sm.GetOrCreate("qq:u1:u1", "u1", "u1")
	p.sm.AppendMessage(session, Message{Role: RoleSystem, Content: "sys"})
	p.sm.AppendMessage(session, Message{Role: RoleUser, Content: "hi"})

	if err := p.execSubCommand(ctx, "status"); err != nil {
		t.Fatalf("execSubCommand(status): %v", err)
	}
	waitReplies(t, sender, 1)
	// 原有自身查询文案：会话状态 + 提供商/模型/消息数
	out := lastReply(sender)
	if !strings.Contains(out, "对话状态") {
		t.Fatalf("无目标时应保持原有自身查询行为，得到 %q", out)
	}
}

func TestHandleSubCommand_RoutesTargetedStatus(t *testing.T) {
	p := newUsagePlugin()
	pm := eventctx.NewPermissionManager()
	if err := pm.AssignRole("admin1", "admin"); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}
	ctx, sender := newUsageCtx("status 12345", "admin1", true, platform.GroupRoleMember)
	ctx.SetPermissionManager(pm)

	if !p.handleSubCommand(ctx, "status 12345") {
		t.Fatal("@机器人 路径的 \"status 12345\" 应被消费")
	}
	waitReplies(t, sender, 1)
	if out := lastReply(sender); !strings.Contains(out, "12345") {
		t.Errorf("回复应包含目标用户: %q", out)
	}
}

func TestHandleSubCommand_NaturalLanguageNotHijacked(t *testing.T) {
	p := newUsagePlugin()
	ctx, _ := newUsageCtx("status 是什么", "u1", true, platform.GroupRoleMember)
	ctx.SetPermissionManager(eventctx.NewPermissionManager())

	if p.handleSubCommand(ctx, "status 是什么") {
		t.Fatal("自然语言正文不应被当作查询他人使用状态命令")
	}
	if p.handleSubCommand(ctx, "statistics") {
		t.Fatal("相似词不应被当作 stats 子命令")
	}
}

// --- SessionManager.PeekOrLoad ---

func TestPeekOrLoad_MemoryHit(t *testing.T) {
	sm := NewSessionManager(10, 20, time.Hour, nil)
	created := sm.GetOrCreate("qq:g:u", "u", "g")

	got := sm.PeekOrLoad("qq:g:u")
	if got == nil {
		t.Fatal("内存命中应返回会话")
	}
	if got != created {
		t.Fatal("内存命中应返回同一会话指针")
	}
}

func TestPeekOrLoad_MissingReturnsNil(t *testing.T) {
	sm := NewSessionManager(10, 20, time.Hour, nil)
	if got := sm.PeekOrLoad("qq:g:absent"); got != nil {
		t.Fatalf("不存在时应返回 nil，得到 %+v", got)
	}
}

func TestPeekOrLoad_LoadsFromStorageWithoutCreating(t *testing.T) {
	store := &fakeSessionStore{}
	sm := NewSessionManager(10, 20, time.Hour, store)
	store.saved = map[string]*Session{
		"qq:g:u": {ID: "qq:g:u", UserID: "u", ChatID: "g", Messages: []Message{{Role: RoleUser, Content: "hi"}}},
	}

	got := sm.PeekOrLoad("qq:g:u")
	if got == nil || got.ID != "qq:g:u" {
		t.Fatalf("应从存储加载会话，得到 %+v", got)
	}
	// 只读：不得写回存储、不得进入内存缓存
	if store.saveCalls != 0 {
		t.Errorf("PeekOrLoad 不应写库，saveCalls = %d", store.saveCalls)
	}
	if s := sm.Peek("qq:g:u"); s != nil {
		t.Error("PeekOrLoad 不应把加载的会话放入 LRU 缓存")
	}
}

// fakeSessionStore 内存版 SessionStore，记录写操作次数。
type fakeSessionStore struct {
	saved     map[string]*Session
	saveCalls int
}

func (f *fakeSessionStore) Load(id string) (*Session, error) { return f.saved[id], nil }

func (f *fakeSessionStore) Save(session *Session) error {
	f.saveCalls++
	if f.saved == nil {
		f.saved = map[string]*Session{}
	}
	f.saved[session.ID] = session
	return nil
}

func (f *fakeSessionStore) Delete(id string) error { delete(f.saved, id); return nil }
