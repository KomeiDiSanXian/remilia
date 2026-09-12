package minecraft

import (
	"strings"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/core/permission"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// ─── 测试辅助 ─────────────────────────────────────────────────────────────────

// adminCtx 构造一个群消息事件上下文（平台 qq）。
func adminCtx(chatID, userID string, role platform.GroupRole) *eventctx.Context {
	evt := platform.NewSyntheticEvent(
		platform.EventKindGroupMessage,
		"/mcadmin list",
		platform.WithSyntheticPlatform("qq"),
		platform.WithSyntheticChat(platform.ChatInfo{ID: chatID, IsGroup: true}),
		platform.WithSyntheticSender(platform.UserInfo{ID: userID, GroupRole: role}),
	)
	return eventctx.NewContextFromEvent(evt, &platform.NoopSender{})
}

// adminPrivateCtx 构造一个私聊事件上下文。
func adminPrivateCtx(userID string) *eventctx.Context {
	evt := platform.NewSyntheticEvent(
		platform.EventKindPrivateMessage,
		"/mcadmin list",
		platform.WithSyntheticPlatform("qq"),
		platform.WithSyntheticChat(platform.ChatInfo{ID: userID}),
		platform.WithSyntheticSender(platform.UserInfo{ID: userID}),
	)
	return eventctx.NewContextFromEvent(evt, &platform.NoopSender{})
}

func serverEntry(name, addr, pw string, extra map[string]any) map[string]any {
	m := map[string]any{"name": name, "address": addr, "password": pw}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

// ─── 配置解析 ─────────────────────────────────────────────────────────────────

func TestAdminConfigFromMapDefaults(t *testing.T) {
	cfg := adminConfigFromMap(map[string]any{}, nil)

	if cfg.Enabled {
		t.Error("管理功能默认必须关闭")
	}
	if cfg.Permission != "minecraft.admin" {
		t.Errorf("Permission = %q", cfg.Permission)
	}
	if !cfg.AllowGroupAdmins {
		t.Error("AllowGroupAdmins 默认应为 true")
	}
	if cfg.Timeout != 5*time.Second {
		t.Errorf("Timeout = %v", cfg.Timeout)
	}
	if cfg.MaxOutput != 1200 {
		t.Errorf("MaxOutput = %d", cfg.MaxOutput)
	}
	if len(cfg.Servers) != 0 {
		t.Errorf("默认不应有服务器，got %d", len(cfg.Servers))
	}
}

func TestAdminConfigFromMapOverrides(t *testing.T) {
	raw := map[string]any{
		"enabled":            true,
		"permission":         "mc.console",
		"allow_group_admins": false,
		"timeout":            "8s",
		"max_output":         300,
		"servers": []any{
			serverEntry("生存服", "mc.example.com:25565", "${PW}", nil),
		},
	}
	cfg := adminConfigFromMap(raw, nil)

	if !cfg.Enabled {
		t.Error("Enabled 应为 true")
	}
	if cfg.Permission != "mc.console" {
		t.Errorf("Permission = %q", cfg.Permission)
	}
	if cfg.AllowGroupAdmins {
		t.Error("AllowGroupAdmins 应为 false")
	}
	if cfg.Timeout != 8*time.Second {
		t.Errorf("Timeout = %v", cfg.Timeout)
	}
	if cfg.MaxOutput != 300 {
		t.Errorf("MaxOutput = %d", cfg.MaxOutput)
	}
	if len(cfg.Servers) != 1 || cfg.Servers[0].Name != "生存服" {
		t.Fatalf("Servers = %+v", cfg.Servers)
	}
}

// TestParseAdminServersSkipsInvalid 验证单条配置错误只跳过该条，
// 不会让整个插件加载失败（查询功能必须不受影响）。
func TestParseAdminServersSkipsInvalid(t *testing.T) {
	raw := []any{
		serverEntry("good", "a.example.com:25565", "pw", nil),
		map[string]any{"address": "b.example.com", "password": "pw"}, // 缺 name
		map[string]any{"name": "nopw", "address": "c.example.com"},   // 缺 password
		map[string]any{"name": "noaddr", "password": "pw"},           // 缺 address
		map[string]any{"name": "badport", "address": "d.example.com:nope", "password": "pw"},
		serverEntry("good", "e.example.com:25565", "pw", nil), // 重名
		"not-a-map",
	}
	got := parseAdminServers(raw, nil)

	if len(got) != 1 {
		t.Fatalf("应只保留 1 条有效配置，实际 %d: %+v", len(got), got)
	}
	if got[0].Name != "good" || got[0].Address != "a.example.com:25565" {
		t.Errorf("保留的条目 = %+v", got[0])
	}
}

// TestParseAdminServersRCONEndpoint 验证 RCON 端点默认派生与显式覆盖。
func TestParseAdminServersRCONEndpoint(t *testing.T) {
	raw := []any{
		serverEntry("derived", "mc.example.com:25565", "pw", nil),
		serverEntry("override", "mc.example.com:25565", "pw", map[string]any{
			"rcon_host": "10.0.0.9",
			"rcon_port": 25580,
		}),
		serverEntry("customport", "mc.example.com:25565", "pw", map[string]any{"rcon_port": 25590}),
	}
	got := parseAdminServers(raw, nil)
	if len(got) != 3 {
		t.Fatalf("应有 3 条，实际 %d", len(got))
	}
	if got[0].RCONAddr != "mc.example.com:25575" {
		t.Errorf("默认 RCON 地址 = %q", got[0].RCONAddr)
	}
	if got[1].RCONAddr != "10.0.0.9:25580" {
		t.Errorf("覆盖后的 RCON 地址 = %q", got[1].RCONAddr)
	}
	if got[2].RCONAddr != "mc.example.com:25590" {
		t.Errorf("仅覆盖端口时 = %q", got[2].RCONAddr)
	}
}

// ─── 会话绑定 ─────────────────────────────────────────────────────────────────

func TestServerVisibleAndBound(t *testing.T) {
	ctx := adminCtx("123", "u1", platform.GroupRoleMember)
	other := adminCtx("456", "u1", platform.GroupRoleMember)

	cases := []struct {
		scope          string
		visibleInGroup bool
		visibleInOther bool
		boundInGroup   bool
	}{
		{"", true, true, false},
		{"123", true, false, true},
		{"qq:123", true, false, true},
		{"999", false, false, false},
	}
	for _, c := range cases {
		srv := &AdminServer{Name: "s", Scope: c.scope}
		if got := serverVisible(srv, ctx); got != c.visibleInGroup {
			t.Errorf("scope=%q 本会话可见 = %v, 期望 %v", c.scope, got, c.visibleInGroup)
		}
		if got := serverVisible(srv, other); got != c.visibleInOther {
			t.Errorf("scope=%q 他会话可见 = %v, 期望 %v", c.scope, got, c.visibleInOther)
		}
		if got := serverBound(srv, ctx); got != c.boundInGroup {
			t.Errorf("scope=%q 本会话绑定 = %v, 期望 %v", c.scope, got, c.boundInGroup)
		}
	}
}

// ─── 权限判定 ─────────────────────────────────────────────────────────────────

func TestAdminDenyReasonMatrix(t *testing.T) {
	global := &AdminServer{Name: "global"}
	bound := &AdminServer{Name: "bound", Scope: "123"}

	t.Run("无权限服务时拒绝", func(t *testing.T) {
		p := &mcPlugin{adminCfg: AdminConfig{Permission: "minecraft.admin", AllowGroupAdmins: true}}
		ctx := adminCtx("123", "u1", platform.GroupRoleMember)
		reason := p.adminDenyReason(ctx, global)
		if reason == "" {
			t.Fatal("缺少权限服务时应拒绝")
		}
		if !strings.Contains(reason, "权限服务不可用") {
			t.Errorf("原因应说明权限服务不可用: %q", reason)
		}
	})

	t.Run("群管理员仅可操作绑定到本会话的服务器", func(t *testing.T) {
		p := &mcPlugin{adminCfg: AdminConfig{Permission: "minecraft.admin", AllowGroupAdmins: true}}
		ctx := adminCtx("123", "u1", platform.GroupRoleAdmin)

		if reason := p.adminDenyReason(ctx, bound); reason != "" {
			t.Errorf("绑定服务器应放行，got %q", reason)
		}
		if reason := p.adminDenyReason(ctx, global); reason == "" {
			t.Error("全局服务器不应被群管理员接管")
		}
	})

	t.Run("群管理员通道可关闭", func(t *testing.T) {
		p := &mcPlugin{adminCfg: AdminConfig{Permission: "minecraft.admin", AllowGroupAdmins: false}}
		ctx := adminCtx("123", "u1", platform.GroupRoleOwner)
		if reason := p.adminDenyReason(ctx, bound); reason == "" {
			t.Error("关闭群管理员通道后应拒绝")
		}
	})

	t.Run("私聊中群角色不生效", func(t *testing.T) {
		p := &mcPlugin{adminCfg: AdminConfig{Permission: "minecraft.admin", AllowGroupAdmins: true}}
		ctx := adminPrivateCtx("u1")
		if reason := p.adminDenyReason(ctx, bound); reason == "" {
			t.Error("私聊中不应凭群角色放行")
		}
	})

	t.Run("权限点持有者可操作全局服务器", func(t *testing.T) {
		perm := permission.NewPlugin()
		if err := perm.Grant("u1", "minecraft.admin"); err != nil {
			t.Fatalf("Grant: %v", err)
		}
		p := &mcPlugin{
			adminCfg: AdminConfig{Permission: "minecraft.admin", AllowGroupAdmins: true},
			permSvc:  perm,
		}
		ctx := adminCtx("123", "u1", platform.GroupRoleMember)
		if reason := p.adminDenyReason(ctx, global); reason != "" {
			t.Errorf("有权限点时应放行全局服务器，got %q", reason)
		}

		other := adminCtx("123", "u2", platform.GroupRoleMember)
		if reason := p.adminDenyReason(other, global); reason == "" {
			t.Error("无权限点的其他用户应被拒绝")
		}
	})

	t.Run("superadmin 角色放行", func(t *testing.T) {
		perm := permission.NewPlugin()
		if err := perm.AssignRole("u9", "superadmin"); err != nil {
			t.Fatalf("AssignRole: %v", err)
		}
		p := &mcPlugin{
			adminCfg: AdminConfig{Permission: "minecraft.admin", AllowGroupAdmins: true},
			permSvc:  perm,
		}
		ctx := adminCtx("123", "u9", platform.GroupRoleMember)
		if !p.hasAdminPerm(ctx) {
			t.Errorf("superadmin 应视为持有权限，roles=%v", perm.GetUserRoles("u9"))
		}
	})
}

// TestFindServerVisibility 验证跨会话不可枚举他人绑定的服务器。
func TestFindServerVisibility(t *testing.T) {
	perm := permission.NewPlugin()
	if err := perm.Grant("u1", "minecraft.admin"); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	p := &mcPlugin{
		adminCfg: AdminConfig{
			Permission: "minecraft.admin",
			Servers: []AdminServer{
				{Name: "全局", Address: "a.example.com:25565"},
				{Name: "本群的", Address: "b.example.com:25565", Scope: "123"},
				{Name: "别群的", Address: "c.example.com:25565", Scope: "456"},
			},
		},
		permSvc: perm,
	}
	ctx := adminCtx("123", "u1", platform.GroupRoleMember)

	srv, reason := p.findServer(ctx, "全局")
	if srv == nil {
		t.Fatalf("全局服务器应可找到: %s", reason)
	}
	if srv, _ = p.findServer(ctx, "本群的"); srv == nil {
		t.Error("本群绑定的服务器应可找到")
	}
	if srv, _ = p.findServer(ctx, "别群的"); srv != nil {
		t.Error("其他群绑定的服务器不应可见")
	}
	// 序号只覆盖可见集合
	if srv, _ = p.findServer(ctx, "2"); srv == nil || srv.Name != "本群的" {
		t.Errorf("序号解析错误: %+v", srv)
	}
	if srv, _ = p.findServer(ctx, "3"); srv != nil {
		t.Error("序号超出可见集合应报错")
	}
}

func TestFindServerCaseInsensitive(t *testing.T) {
	perm := permission.NewPlugin()
	if err := perm.Grant("u1", "minecraft.admin"); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	p := &mcPlugin{
		adminCfg: AdminConfig{
			Permission: "minecraft.admin",
			Servers:    []AdminServer{{Name: "Survival", Address: "a.example.com:25565"}},
		},
		permSvc: perm,
	}
	ctx := adminCtx("123", "u1", platform.GroupRoleMember)
	if srv, reason := p.findServer(ctx, "survival"); srv == nil {
		t.Errorf("名称匹配应大小写不敏感: %s", reason)
	}
}

// ─── 输入清洗 ─────────────────────────────────────────────────────────────────

// TestSanitizeConsoleArg 验证换行注入被清除。
// 玩家名或广播文本里的 \n 会让服务端把它当成第二条控制台命令执行。
func TestSanitizeConsoleArg(t *testing.T) {
	cases := map[string]string{
		"Steve":            "Steve",
		"a\nb":             "a b",
		"a\rb":             "a b",
		"  padded  ":       "padded",
		"name\nop Evil":    "name op Evil",
		"a\x00b":           "a b",
		"line1\nline2\nop": "line1 line2 op",
	}
	for in, want := range cases {
		if got := sanitizeConsoleArg(in); got != want {
			t.Errorf("sanitizeConsoleArg(%q) = %q, 期望 %q", in, got, want)
		}
	}
}

func TestValidPlayerName(t *testing.T) {
	ok := []string{"Steve", "Notch_", "player123", "a"}
	bad := []string{"", "has space", "tab\tname", strings.Repeat("x", 33)}
	for _, s := range ok {
		if !validPlayerName(s) {
			t.Errorf("%q 应视为合法玩家名", s)
		}
	}
	for _, s := range bad {
		if validPlayerName(s) {
			t.Errorf("%q 应视为非法玩家名", s)
		}
	}
}

func TestMetricCommand(t *testing.T) {
	cases := map[string]string{
		"list":                  "list",
		"say hello world":       "say",
		"whitelist add Steve":   "whitelist",
		"save-all":              "save-all",
		"kick Steve cheat here": "kick",
	}
	for in, want := range cases {
		if got := metricCommand(in); got != want {
			t.Errorf("metricCommand(%q) = %q, 期望 %q", in, got, want)
		}
	}
}

// TestIsTPSOutput 验证原版服务端的"未知命令"回显不会被当成 TPS 数据展示。
func TestIsTPSOutput(t *testing.T) {
	if !isTPSOutput("TPS from last 1m, 5m, 15m: 20.0, 20.0, 20.0") {
		t.Error("正常 TPS 输出应被识别")
	}
	if isTPSOutput("Unknown or incomplete command, see below for error") {
		t.Error("未知命令回显不应被识别为 TPS")
	}
	if isTPSOutput("未知的命令，请查看下方错误") {
		t.Error("中文未知命令回显不应被识别为 TPS")
	}
	if isTPSOutput("") {
		t.Error("空输出不应被识别为 TPS")
	}
}

// ─── 二次确认 ─────────────────────────────────────────────────────────────────

func TestConfirmStoreFlow(t *testing.T) {
	now := time.Now()
	s := newConfirmStore(time.Minute)

	calls := 0
	code := s.put("qq:1|u1", func(*eventctx.Context) { calls++ }, now)
	if len(code) != 6 {
		t.Errorf("验证码应为 6 位，got %q", code)
	}

	// 验证码错误
	if _, ok := s.take("qq:1|u1", "000000", now); ok && code != "000000" {
		t.Error("错误验证码不应通过")
	}
	// 其他用户 / 其他会话无法兑换
	if _, ok := s.take("qq:1|u2", code, now); ok {
		t.Error("其他用户不应能兑换验证码")
	}
	if _, ok := s.take("qq:2|u1", code, now); ok {
		t.Error("其他会话不应能兑换验证码")
	}
	// 正确的用户与会话
	run, ok := s.take("qq:1|u1", code, now)
	if !ok {
		t.Fatal("正确验证码应通过")
	}
	run(nil)
	if calls != 1 {
		t.Errorf("确认应执行一次，实际 %d", calls)
	}
	// 一次性：再次兑换失败
	if _, ok := s.take("qq:1|u1", code, now); ok {
		t.Error("验证码应为一次性")
	}
}

func TestConfirmStoreExpiry(t *testing.T) {
	now := time.Now()
	s := newConfirmStore(30 * time.Second)
	code := s.put("k", func(*eventctx.Context) {}, now)

	if _, ok := s.take("k", code, now.Add(31*time.Second)); ok {
		t.Error("过期验证码不应通过")
	}
}

func TestConfirmStoreReplacesPrevious(t *testing.T) {
	now := time.Now()
	s := newConfirmStore(time.Minute)
	old := s.put("k", func(*eventctx.Context) {}, now)
	_ = s.put("k", func(*eventctx.Context) {}, now)

	if _, ok := s.take("k", old, now); ok {
		t.Error("同一 key 只应保留最新一条待确认操作")
	}
}

func TestNewConfirmCodeFormat(t *testing.T) {
	for i := 0; i < 20; i++ {
		code := newConfirmCode()
		if len(code) != 6 {
			t.Fatalf("验证码格式错误: %q", code)
		}
		for _, r := range code {
			if r < '0' || r > '9' {
				t.Fatalf("验证码应为纯数字: %q", code)
			}
		}
	}
}
