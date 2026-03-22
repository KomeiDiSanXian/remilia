package admin

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/acl"
	"github.com/KomeiDiSanXian/remilia/builtin/core/permission"
	"github.com/KomeiDiSanXian/remilia/builtin/verifycode"
	"github.com/KomeiDiSanXian/remilia/command"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// Plugin 管理插件
type Plugin struct {
	PluginManager plugin.ManagerWriter // 管理写视图（通过 ctx.Admin 注入）
	PermPlugin    *permission.Plugin
	AclPlugin     *acl.Plugin
	VcPlugin      *verifycode.Plugin
	setupCtx      *plugin.SetupContext
}

// New 创建管理插件（v2 API）
func New() *plugin.Descriptor {
	v1Plugin := &Plugin{}
	return &plugin.Descriptor{
		Name:       "admin",
		Version:    "2.1.0",
		Deps:       []string{"permission"},
		Privileged: true, // 需要 ManagerWriter 权限（Reload/Disable/Enable/Unregister）
		Meta: &plugin.Metadata{
			Author:      "Remilia Team",
			Description: "机器人管理核心插件，提供插件管理、权限管理和配置管理功能",
			Category:    "系统",
			Tags:        []string{"管理", "系统", "核心"},
			HelpText:    "使用 /plugin, /perm, /code, /acl, /status, /info 进行管理操作",
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			ctx.Log.Info("Loading admin plugin (v2)...")
			// 使用框架约定的 Must[T]，错误信息携带插件名和依赖名上下文
			v1Plugin.PermPlugin = plugin.Must[permission.Plugin](ctx, "permission")
			// 通过 ctx.Admin 获取管理写视图（合法路径，无需私有接口断言）
			v1Plugin.PluginManager = ctx.Admin
			v1Plugin.setupCtx = ctx
			if raw, ok := ctx.Get("acl"); ok {
				if ap, ok := raw.(*acl.Plugin); ok {
					v1Plugin.AclPlugin = ap
					ctx.Log.Info("Using standalone acl plugin")
				}
			}
			if raw, ok := ctx.Get("verifycode"); ok {
				if vp, ok := raw.(*verifycode.Plugin); ok {
					v1Plugin.VcPlugin = vp
					ctx.Log.Info("Using standalone verifycode plugin")
				}
			}
			if err := v1Plugin.Load(ctx); err != nil {
				return nil, err
			}
			return v1Plugin, nil // 导出到容器，供其他插件（如 monitor/auditlog）发现
		},
		Teardown: func(ctx *plugin.TeardownContext) error {
			ctx.Log.Info("Admin plugin unloaded")
			return nil
		},
	}
}

// Load 加载插件（注册所有命令）
func (p *Plugin) Load(ctx *plugin.SetupContext) error {
	p.registerPluginCommand(ctx)
	p.registerPermCommand(ctx)
	p.registerCodeCommand(ctx)
	p.registerACLCommand(ctx)
	p.registerSystemCommands(ctx)
	return nil
}

// SetPluginManager 设置插件管理器（仅用于测试或外部初始化）
func (p *Plugin) SetPluginManager(pm plugin.ManagerWriter) { p.PluginManager = pm }

// SetPermissionPlugin 设置权限插件
func (p *Plugin) SetPermissionPlugin(pp *permission.Plugin) { p.PermPlugin = pp }

// registerPluginCommand 注册插件管理命令
func (p *Plugin) registerPluginCommand(ctx *plugin.SetupContext) {
	pluginCmd := &command.Definition{
		Name:        "plugin",
		Description: "插件管理",
		Usage:       "/plugin <子命令> [参数]",
		Category:    "系统",
		SubCommands: []*command.Definition{
			{Name: "list", Description: "列出所有插件", Usage: "/plugin list",
				Examples: []string{"/plugin list"}},
			{Name: "info", Description: "查看插件详情", Usage: "/plugin info <名称>",
				Arguments: []*command.Argument{{Name: "name", Type: command.ArgTypeString, Description: "插件名称", Required: true}},
				Examples:  []string{"/plugin info help"}},
			{Name: "reload", Description: "重载插件", Usage: "/plugin reload <名称>",
				Arguments: []*command.Argument{{Name: "name", Type: command.ArgTypeString, Description: "插件名称", Required: true}},
				Examples:  []string{"/plugin reload help"}},
			{Name: "disable", Description: "禁用插件（暂停响应，可通过 enable 恢复）", Usage: "/plugin disable <名称>",
				Arguments: []*command.Argument{{Name: "name", Type: command.ArgTypeString, Description: "插件名称", Required: true}},
				Examples:  []string{"/plugin disable antispam"}},
			{Name: "enable", Description: "启用已禁用的插件", Usage: "/plugin enable <名称>",
				Arguments: []*command.Argument{{Name: "name", Type: command.ArgTypeString, Description: "插件名称", Required: true}},
				Examples:  []string{"/plugin enable antispam"}},
			{Name: "unload", Description: "卸载插件（完全移除）", Usage: "/plugin unload <名称>",
				Arguments: []*command.Argument{{Name: "name", Type: command.ArgTypeString, Description: "插件名称", Required: true}},
				Examples:  []string{"/plugin unload debug"}},
		},
	}
	ctx.Reg.RegisterCommand("", "/plugin").
		SetDefinition(pluginCmd).
		Handle(p.handlePluginCommand)
}

func (p *Plugin) handlePluginCommand(ctx *eventctx.Context) error {
	args, err := command.ParseCommandLine(ctx.GetMessageContent())
	if err != nil {
		return p.reply(ctx, "命令解析失败: "+err.Error())
	}
	switch args.Get(0) {
	case "":
		return p.reply(ctx, "插件管理: /plugin list|info|reload|disable|enable|unload <名称>")
	case "list":
		return p.handlePluginList(ctx)
	case "info":
		return p.handlePluginInfo(ctx, args)
	case "reload":
		return p.handlePluginReload(ctx, args)
	case "disable":
		return p.handlePluginDisable(ctx, args)
	case "enable":
		return p.handlePluginEnable(ctx, args)
	case "unload":
		return p.handlePluginUnload(ctx, args)
	default:
		return p.reply(ctx, fmt.Sprintf("未知子命令: %s", args.Get(0)))
	}
}

// registerPermCommand 注册权限管理命令
func (p *Plugin) registerPermCommand(ctx *plugin.SetupContext) {
	permCmd := &command.Definition{
		Name:        "perm",
		Description: "权限管理",
		Usage:       "/perm <子命令> [参数]",
		Category:    "权限",
		SubCommands: []*command.Definition{
			{Name: "grant", Description: "授予用户权限", Usage: "/perm grant <用户ID> <权限>",
				Arguments: []*command.Argument{
					{Name: "userID", Type: command.ArgTypeString, Description: "用户ID", Required: true},
					{Name: "permission", Type: command.ArgTypeString, Description: "权限", Required: true},
				}, Examples: []string{"/perm grant USER123 command.use"}},
			{Name: "revoke", Description: "撤销用户权限", Usage: "/perm revoke <用户ID> <权限>",
				Arguments: []*command.Argument{
					{Name: "userID", Type: command.ArgTypeString, Description: "用户ID", Required: true},
					{Name: "permission", Type: command.ArgTypeString, Description: "权限", Required: true},
				}, Examples: []string{"/perm revoke USER123 command.use"}},
			{Name: "list", Description: "列出用户权限", Usage: "/perm list [用户ID]",
				Arguments: []*command.Argument{
					{Name: "userID", Type: command.ArgTypeString, Description: "用户ID（可选）", Required: false},
				}, Examples: []string{"/perm list", "/perm list USER123"}},
			{Name: "role", Description: "分配角色给用户", Usage: "/perm role <用户ID> <角色>",
				Arguments: []*command.Argument{
					{Name: "userID", Type: command.ArgTypeString, Description: "用户ID", Required: true},
					{Name: "role", Type: command.ArgTypeString, Description: "角色名", Required: true},
				}, Examples: []string{"/perm role USER123 admin"}},
		},
	}
	ctx.Reg.RegisterCommand("", "/perm").
		SetDefinition(permCmd).
		Handle(p.handlePermCommand)
}

func (p *Plugin) handlePermCommand(ctx *eventctx.Context) error {
	args, err := command.ParseCommandLine(ctx.GetMessageContent())
	if err != nil {
		return p.reply(ctx, "命令解析失败: "+err.Error())
	}
	switch args.Get(0) {
	case "":
		return p.reply(ctx, "权限管理: /perm grant|revoke|list|role")
	case "grant":
		return p.handlePermGrant(ctx, args)
	case "revoke":
		return p.handlePermRevoke(ctx, args)
	case "list":
		return p.handlePermList(ctx, args)
	case "role":
		return p.handlePermRole(ctx, args)
	default:
		return p.reply(ctx, fmt.Sprintf("未知子命令: %s", args.Get(0)))
	}
}

// registerSystemCommands 注册系统命令
func (p *Plugin) registerSystemCommands(ctx *plugin.SetupContext) {
	ctx.Reg.RegisterCommand("", "/status").Handle(p.handleStatus)
	ctx.Reg.RegisterCommand("", "/info").Handle(p.handleInfo)
}

func (p *Plugin) handlePluginList(ctx *eventctx.Context) error {
	if p.setupCtx == nil {
		return p.reply(ctx, "插件管理器未初始化")
	}
	if !p.checkPermission(ctx, "plugin.list") {
		return p.reply(ctx, "权限不足")
	}
	plugins := p.setupCtx.Info.ListWithMetadata()
	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("已加载插件列表（共 %d 个）\n", len(plugins)))
	categories := make(map[string][]*plugin.Metadata)
	for _, meta := range plugins {
		cat := meta.Category
		if cat == "" {
			cat = "其他"
		}
		categories[cat] = append(categories[cat], meta)
	}
	for cat, metas := range categories {
		msg.WriteString(fmt.Sprintf("[%s]\n", cat))
		for _, meta := range metas {
			msg.WriteString(fmt.Sprintf("  %s v%s - %s\n", meta.Name, meta.Version, meta.Description))
		}
	}
	return p.reply(ctx, msg.String())
}

func (p *Plugin) handlePluginInfo(ctx *eventctx.Context, args *command.Args) error {
	if p.setupCtx == nil {
		return p.reply(ctx, "插件管理器未初始化")
	}
	name := args.Get(1)
	if name == "" {
		return p.reply(ctx, "用法: /plugin info <插件名>")
	}
	meta, ok := p.setupCtx.Info.GetMetadata(name)
	if !ok || meta == nil {
		return p.reply(ctx, fmt.Sprintf("插件 '%s' 不存在", name))
	}
	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("插件 [%s] 详细信息\n", meta.Name))
	msg.WriteString(fmt.Sprintf("版本: %s | 作者: %s\n", meta.Version, meta.Author))
	msg.WriteString(fmt.Sprintf("描述: %s\n", meta.Description))
	if len(meta.Dependencies) > 0 {
		msg.WriteString(fmt.Sprintf("依赖: %s\n", strings.Join(meta.Dependencies, ", ")))
	}
	if meta.HelpText != "" {
		msg.WriteString(fmt.Sprintf("\n帮助:\n%s\n", meta.HelpText))
	}
	return p.reply(ctx, msg.String())
}

func (p *Plugin) handlePluginReload(ctx *eventctx.Context, args *command.Args) error {
	if p.PluginManager == nil {
		return p.reply(ctx, "插件管理器未初始化")
	}
	if !p.checkPermission(ctx, "plugin.reload") {
		return p.reply(ctx, "权限不足")
	}
	name := args.Get(1)
	if name == "" {
		return p.reply(ctx, "用法: /plugin reload <插件名>")
	}
	if err := p.PluginManager.Reload(name); err != nil {
		return p.reply(ctx, fmt.Sprintf("重载失败: %v", err))
	}
	return p.reply(ctx, fmt.Sprintf("插件 '%s' 重载成功", name))
}

func (p *Plugin) handlePluginDisable(ctx *eventctx.Context, args *command.Args) error {
	if p.PluginManager == nil {
		return p.reply(ctx, "插件管理器未初始化")
	}
	if !p.checkPermission(ctx, "plugin.disable") && !p.hasAdminRole(ctx) {
		return p.reply(ctx, "权限不足")
	}
	name := args.Get(1)
	if name == "" {
		return p.reply(ctx, "用法: /plugin disable <插件名>")
	}
	if name == "admin" {
		return p.reply(ctx, "不能禁用 admin 插件自身")
	}
	if err := p.PluginManager.Disable(name); err != nil {
		return p.reply(ctx, fmt.Sprintf("禁用失败: %v", err))
	}
	return p.reply(ctx, fmt.Sprintf("插件 '%s' 已禁用，使用 /plugin enable %s 恢复", name, name))
}

func (p *Plugin) handlePluginEnable(ctx *eventctx.Context, args *command.Args) error {
	if p.PluginManager == nil {
		return p.reply(ctx, "插件管理器未初始化")
	}
	if !p.checkPermission(ctx, "plugin.enable") && !p.hasAdminRole(ctx) {
		return p.reply(ctx, "权限不足")
	}
	name := args.Get(1)
	if name == "" {
		return p.reply(ctx, "用法: /plugin enable <插件名>")
	}
	if err := p.PluginManager.Enable(name); err != nil {
		return p.reply(ctx, fmt.Sprintf("启用失败: %v", err))
	}
	return p.reply(ctx, fmt.Sprintf("插件 '%s' 已启用", name))
}

func (p *Plugin) handlePluginUnload(ctx *eventctx.Context, args *command.Args) error {
	if p.PluginManager == nil {
		return p.reply(ctx, "插件管理器未初始化")
	}
	if !p.checkPermission(ctx, "plugin.unload") && !p.hasAdminRole(ctx) {
		return p.reply(ctx, "权限不足")
	}
	name := args.Get(1)
	if name == "" {
		return p.reply(ctx, "用法: /plugin unload <插件名>")
	}
	if name == "admin" {
		return p.reply(ctx, "不能卸载 admin 插件自身")
	}
	if err := p.PluginManager.Unregister(name); err != nil {
		return p.reply(ctx, fmt.Sprintf("卸载失败: %v", err))
	}
	return p.reply(ctx, fmt.Sprintf("插件 '%s' 已卸载", name))
}

func (p *Plugin) handlePermGrant(ctx *eventctx.Context, args *command.Args) error {
	if p.PermPlugin == nil {
		return p.reply(ctx, "权限插件未初始化")
	}
	if !p.checkPermission(ctx, "perm.grant") {
		return p.reply(ctx, "权限不足")
	}
	userID, perm := args.Get(1), args.Get(2)
	if userID == "" || perm == "" {
		return p.reply(ctx, "用法: /perm grant <用户ID> <权限>")
	}
	if err := p.PermPlugin.Grant(userID, perm); err != nil {
		return p.reply(ctx, fmt.Sprintf("授予失败: %v", err))
	}
	return p.reply(ctx, fmt.Sprintf("已授予用户 '%s' 权限 '%s'", userID, perm))
}

func (p *Plugin) handlePermRevoke(ctx *eventctx.Context, args *command.Args) error {
	if p.PermPlugin == nil {
		return p.reply(ctx, "权限插件未初始化")
	}
	if !p.checkPermission(ctx, "perm.revoke") {
		return p.reply(ctx, "权限不足")
	}
	userID, perm := args.Get(1), args.Get(2)
	if userID == "" || perm == "" {
		return p.reply(ctx, "用法: /perm revoke <用户ID> <权限>")
	}
	if err := p.PermPlugin.Revoke(userID, perm); err != nil {
		return p.reply(ctx, fmt.Sprintf("撤销失败: %v", err))
	}
	return p.reply(ctx, fmt.Sprintf("已撤销用户 '%s' 的权限 '%s'", userID, perm))
}

func (p *Plugin) handlePermList(ctx *eventctx.Context, args *command.Args) error {
	if p.PermPlugin == nil {
		return p.reply(ctx, "权限插件未初始化")
	}
	if !p.checkPermission(ctx, "perm.list") {
		return p.reply(ctx, "权限不足")
	}
	userID := args.Get(1)
	if userID == "" {
		userID = ctx.GetUserID()
	}
	perms := p.PermPlugin.GetUserPermissions(userID)
	roles := p.PermPlugin.GetUserRoles(userID)
	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("用户 '%s':\n", userID))
	if len(roles) > 0 {
		msg.WriteString(fmt.Sprintf("角色: %s\n", strings.Join(roles, ", ")))
	}
	if len(perms) > 0 {
		msg.WriteString(fmt.Sprintf("权限 (%d): %s\n", len(perms), strings.Join(perms, ", ")))
	} else {
		msg.WriteString("无权限\n")
	}
	return p.reply(ctx, msg.String())
}

func (p *Plugin) handlePermRole(ctx *eventctx.Context, args *command.Args) error {
	if p.PermPlugin == nil {
		return p.reply(ctx, "权限插件未初始化")
	}
	if !p.checkPermission(ctx, "perm.role") {
		return p.reply(ctx, "权限不足")
	}
	userID, role := args.Get(1), args.Get(2)
	if userID == "" || role == "" {
		return p.reply(ctx, "用法: /perm role <用户ID> <角色>")
	}
	if err := p.PermPlugin.AssignRole(userID, role); err != nil {
		return p.reply(ctx, fmt.Sprintf("分配失败: %v", err))
	}
	return p.reply(ctx, fmt.Sprintf("已为用户 '%s' 分配角色 '%s'", userID, role))
}

func (p *Plugin) handleStatus(ctx *eventctx.Context) error {
	var msg strings.Builder
	msg.WriteString("机器人运行中\n")
	if p.setupCtx != nil {
		msg.WriteString(fmt.Sprintf("已加载插件: %d 个\n", p.setupCtx.Info.Count()))
	}
	return p.reply(ctx, msg.String())
}

func (p *Plugin) handleInfo(ctx *eventctx.Context) error {
	return p.reply(ctx, "Remilia Bot\n框架: Remilia\n使用 /help 查看帮助")
}

func (p *Plugin) checkPermission(ctx *eventctx.Context, perm string) bool {
	if p.PermPlugin == nil {
		return true
	}
	return p.PermPlugin.HasPermission(ctx.GetUserID(), perm)
}

func (p *Plugin) hasAdminRole(ctx *eventctx.Context) bool {
	if p.PermPlugin == nil {
		return false
	}
	return slices.Contains(p.PermPlugin.GetUserRoles(ctx.GetUserID()), "admin")
}

func (p *Plugin) reply(ctx *eventctx.Context, content string) error {
	return ctx.Reply(platform.TextMessage(content))
}

// === 验证码相关功能 ===

func (p *Plugin) registerCodeCommand(ctx *plugin.SetupContext) {
	codeCmd := &command.Definition{
		Name:        "code",
		Description: "验证码管理",
		Usage:       "/code <子命令> [参数]",
		Category:    "权限",
		SubCommands: []*command.Definition{
			{Name: "gen", Description: "生成验证码", Usage: "/code gen <角色> [有效期] [次数]",
				Arguments: []*command.Argument{
					{Name: "role", Type: command.ArgTypeString, Description: "角色名", Required: true},
					{Name: "expiry", Type: command.ArgTypeString, Description: "有效期（如 30m, 1h）", Required: false},
					{Name: "maxUses", Type: command.ArgTypeInt, Description: "最大使用次数", Required: false},
				}, Examples: []string{"/code gen admin 1h 0"}},
			{Name: "verify", Description: "使用验证码获取权限", Usage: "/code verify <验证码>",
				Arguments: []*command.Argument{{Name: "code", Type: command.ArgTypeString, Description: "验证码", Required: true}},
				Examples:  []string{"/code verify ABC123"}},
			{Name: "list", Description: "列出所有有效验证码", Usage: "/code list",
				Examples: []string{"/code list"}},
			{Name: "revoke", Description: "撤销验证码", Usage: "/code revoke <验证码>",
				Arguments: []*command.Argument{{Name: "code", Type: command.ArgTypeString, Description: "验证码", Required: true}},
				Examples:  []string{"/code revoke ABC123"}},
		},
	}
	ctx.Reg.RegisterCommand("", "/code").SetDefinition(codeCmd).Handle(p.handleCodeCommand)
}

func (p *Plugin) handleCodeCommand(ctx *eventctx.Context) error {
	args, err := command.ParseCommandLine(ctx.GetMessageContent())
	if err != nil {
		return p.reply(ctx, "命令解析失败: "+err.Error())
	}
	switch args.Get(0) {
	case "":
		return p.reply(ctx, "验证码命令: /code gen|verify|list|revoke")
	case "gen":
		return p.handleCodeGen(ctx, args)
	case "verify":
		return p.handleCodeVerify(ctx, args)
	case "list":
		return p.handleCodeList(ctx)
	case "revoke":
		return p.handleCodeRevoke(ctx, args)
	default:
		return p.reply(ctx, fmt.Sprintf("未知子命令: %s", args.Get(0)))
	}
}

func (p *Plugin) handleCodeGen(ctx *eventctx.Context, args *command.Args) error {
	if !p.checkPermission(ctx, "code.gen") && !p.hasAdminRole(ctx) {
		return p.reply(ctx, "权限不足：需要管理员权限才能生成验证码")
	}
	role := args.Get(1)
	if role == "" {
		return p.reply(ctx, "用法: /code gen <角色> [有效期] [次数]\n示例: /code gen admin 1h 0")
	}
	expiry := 30 * time.Minute
	if s := args.Get(2); s != "" {
		var err error
		if expiry, err = time.ParseDuration(s); err != nil {
			return p.reply(ctx, fmt.Sprintf("无效的有效期格式: %s", s))
		}
	}
	maxUses := 0
	if s := args.Get(3); s != "" {
		if n, err := command.ParseInt(s); err == nil {
			maxUses = n
		}
	}
	if p.VcPlugin != nil {
		code, err := p.VcPlugin.Generate(verifycode.CodeConfig{Role: role, TTL: expiry, MaxUses: maxUses})
		if err != nil {
			return p.reply(ctx, fmt.Sprintf("生成验证码失败: %v", err))
		}
		return p.reply(ctx, fmt.Sprintf("验证码: %s\n角色: %s\n有效期: %v\n使用: /code verify %s", code, role, expiry, code))
	}
	if p.PermPlugin == nil {
		return p.reply(ctx, "权限系统未初始化")
	}
	code, err := p.PermPlugin.GenerateVerificationCode(role, expiry, maxUses)
	if err != nil {
		return p.reply(ctx, fmt.Sprintf("生成验证码失败: %v", err))
	}
	return p.reply(ctx, fmt.Sprintf("验证码: %s\n角色: %s\n有效期: %v\n使用: /code verify %s", code, role, expiry, code))
}

func (p *Plugin) handleCodeVerify(ctx *eventctx.Context, args *command.Args) error {
	code := args.Get(1)
	if code == "" {
		return p.reply(ctx, "用法: /code verify <验证码>")
	}
	userID := ctx.GetUserID()
	if p.VcPlugin != nil {
		role, err := p.VcPlugin.Verify(userID, code)
		if err != nil {
			return p.reply(ctx, fmt.Sprintf("验证失败: %v", err))
		}
		return p.reply(ctx, fmt.Sprintf("验证成功！已获得角色: %s", role))
	}
	if p.PermPlugin == nil {
		return p.reply(ctx, "权限系统未初始化")
	}
	role, err := p.PermPlugin.VerifyAndGrantRole(code, userID)
	if err != nil {
		return p.reply(ctx, fmt.Sprintf("验证失败: %v", err))
	}
	return p.reply(ctx, fmt.Sprintf("验证成功！已获得角色: %s", role))
}

func (p *Plugin) handleCodeList(ctx *eventctx.Context) error {
	if !p.checkPermission(ctx, "code.list") && !p.hasAdminRole(ctx) {
		return p.reply(ctx, "权限不足：需要管理员权限")
	}
	if p.VcPlugin != nil {
		codes := p.VcPlugin.ListValid()
		if len(codes) == 0 {
			return p.reply(ctx, "当前没有有效的验证码")
		}
		var msg strings.Builder
		for i, c := range codes {
			msg.WriteString(fmt.Sprintf("%d. %s -> %s\n", i+1, c.Code, c.Role))
		}
		return p.reply(ctx, msg.String())
	}
	if p.PermPlugin == nil {
		return p.reply(ctx, "权限系统未初始化")
	}
	codes := p.PermPlugin.ListVerificationCodes()
	if len(codes) == 0 {
		return p.reply(ctx, "当前没有有效的验证码")
	}
	var msg strings.Builder
	for i, c := range codes {
		msg.WriteString(fmt.Sprintf("%d. %s -> %s (过期: %s)\n", i+1, c.Code, c.Role, c.ExpiresAt.Format("15:04:05")))
	}
	return p.reply(ctx, msg.String())
}

func (p *Plugin) handleCodeRevoke(ctx *eventctx.Context, args *command.Args) error {
	if !p.checkPermission(ctx, "code.revoke") && !p.hasAdminRole(ctx) {
		return p.reply(ctx, "权限不足：需要管理员权限")
	}
	code := args.Get(1)
	if code == "" {
		return p.reply(ctx, "用法: /code revoke <验证码>")
	}
	if p.VcPlugin != nil {
		if !p.VcPlugin.Revoke(code) {
			return p.reply(ctx, fmt.Sprintf("验证码 %s 不存在", code))
		}
		return p.reply(ctx, fmt.Sprintf("验证码 %s 已撤销", code))
	}
	if p.PermPlugin == nil {
		return p.reply(ctx, "权限系统未初始化")
	}
	if err := p.PermPlugin.RevokeVerificationCode(code); err != nil {
		return p.reply(ctx, fmt.Sprintf("撤销失败: %v", err))
	}
	return p.reply(ctx, fmt.Sprintf("验证码 %s 已撤销", code))
}

// === 黑白名单相关功能 ===

func (p *Plugin) registerACLCommand(ctx *plugin.SetupContext) {
	aclCmd := &command.Definition{
		Name:        "acl",
		Description: "黑白名单管理",
		Usage:       "/acl <子命令> [参数]",
		Category:    "权限",
		SubCommands: []*command.Definition{
			{Name: "mode", Description: "设置黑白名单模式", Usage: "/acl mode <模式>",
				Arguments: []*command.Argument{{Name: "mode", Type: command.ArgTypeString, Description: "disabled/blacklist/whitelist", Required: false}},
				Examples:  []string{"/acl mode blacklist"}},
			{Name: "add", Description: "添加用户到列表", Usage: "/acl add <用户ID> [备注]",
				Arguments: []*command.Argument{
					{Name: "userID", Type: command.ArgTypeString, Description: "用户ID", Required: true},
					{Name: "note", Type: command.ArgTypeString, Description: "备注", Required: false},
				}, Examples: []string{"/acl add USER123"}},
			{Name: "remove", Description: "从列表移除用户", Usage: "/acl remove <用户ID>",
				Arguments: []*command.Argument{{Name: "userID", Type: command.ArgTypeString, Description: "用户ID", Required: true}},
				Examples:  []string{"/acl remove USER123"}},
			{Name: "list", Description: "列出所有用户", Usage: "/acl list", Examples: []string{"/acl list"}},
			{Name: "clear", Description: "清空列表", Usage: "/acl clear", Examples: []string{"/acl clear"}},
			{Name: "stats", Description: "查看统计信息", Usage: "/acl stats", Examples: []string{"/acl stats"}},
		},
	}
	ctx.Reg.RegisterCommand("", "/acl").
		SetDefinition(aclCmd).
		Handle(p.handleACLCommand)
}

func (p *Plugin) handleACLCommand(ctx *eventctx.Context) error {
	args, err := command.ParseCommandLine(ctx.GetMessageContent())
	if err != nil {
		return p.reply(ctx, "命令解析失败: "+err.Error())
	}
	switch args.Get(0) {
	case "":
		return p.reply(ctx, "黑白名单: /acl mode|add|remove|list|clear|stats")
	case "mode":
		return p.handleACLMode(ctx, args)
	case "add":
		return p.handleACLAdd(ctx, args)
	case "remove":
		return p.handleACLRemove(ctx, args)
	case "list":
		return p.handleACLList(ctx)
	case "clear":
		return p.handleACLClear(ctx)
	case "stats":
		return p.handleACLStats(ctx)
	default:
		return p.reply(ctx, fmt.Sprintf("未知子命令: %s", args.Get(0)))
	}
}

func (p *Plugin) handleACLMode(ctx *eventctx.Context, args *command.Args) error {
	if !p.checkPermission(ctx, "acl.manage") && !p.hasAdminRole(ctx) {
		return p.reply(ctx, "权限不足：需要管理员权限")
	}
	modeStr := args.Get(1)
	if p.AclPlugin != nil {
		if modeStr == "" {
			return p.reply(ctx, fmt.Sprintf("当前模式: %s\n可用: disabled, blacklist, whitelist", p.AclPlugin.GetMode()))
		}
		mode, err := acl.ParseMode(modeStr)
		if err != nil {
			return p.reply(ctx, fmt.Sprintf("无效模式: %s", modeStr))
		}
		p.AclPlugin.SetMode(mode)
		return p.reply(ctx, fmt.Sprintf("模式已设置为: %s", mode))
	}
	if p.PermPlugin == nil {
		return p.reply(ctx, "权限系统未初始化")
	}
	if modeStr == "" {
		return p.reply(ctx, fmt.Sprintf("当前模式: %s\n可用: disabled, blacklist, whitelist", p.PermPlugin.GetACLMode().String()))
	}
	var mode permission.ListMode
	switch strings.ToLower(modeStr) {
	case "disabled", "disable", "off":
		mode = permission.ModeDisabled
	case "blacklist", "black":
		mode = permission.ModeBlacklist
	case "whitelist", "white":
		mode = permission.ModeWhitelist
	default:
		return p.reply(ctx, fmt.Sprintf("无效模式: %s", modeStr))
	}
	p.PermPlugin.SetACLMode(mode)
	return p.reply(ctx, fmt.Sprintf("模式已设置为: %s", mode.String()))
}

func (p *Plugin) handleACLAdd(ctx *eventctx.Context, args *command.Args) error {
	if !p.checkPermission(ctx, "acl.manage") && !p.hasAdminRole(ctx) {
		return p.reply(ctx, "权限不足：需要管理员权限")
	}
	userID := args.Get(1)
	if userID == "" {
		return p.reply(ctx, "用法: /acl add <用户ID> [备注]")
	}
	note := ""
	if args.Len() > 2 {
		var parts []string
		for i := 2; i < args.Len(); i++ {
			parts = append(parts, args.Get(i))
		}
		note = strings.Join(parts, " ")
	}
	if p.AclPlugin != nil {
		p.AclPlugin.Add(userID, note)
		return p.reply(ctx, fmt.Sprintf("已添加 %s 到 %s", userID, p.AclPlugin.GetMode()))
	}
	if p.PermPlugin == nil {
		return p.reply(ctx, "权限系统未初始化")
	}
	if p.PermPlugin.GetACLMode() == permission.ModeDisabled {
		return p.reply(ctx, "黑白名单功能未启用，请先使用 /acl mode 设置模式")
	}
	p.PermPlugin.AddToACL(userID, note)
	return p.reply(ctx, fmt.Sprintf("已添加用户 '%s'", userID))
}

func (p *Plugin) handleACLRemove(ctx *eventctx.Context, args *command.Args) error {
	if !p.checkPermission(ctx, "acl.manage") && !p.hasAdminRole(ctx) {
		return p.reply(ctx, "权限不足：需要管理员权限")
	}
	userID := args.Get(1)
	if userID == "" {
		return p.reply(ctx, "用法: /acl remove <用户ID>")
	}
	if p.AclPlugin != nil {
		if !p.AclPlugin.Remove(userID) {
			return p.reply(ctx, fmt.Sprintf("用户 %s 不在列表中", userID))
		}
		return p.reply(ctx, fmt.Sprintf("已移除: %s", userID))
	}
	if p.PermPlugin == nil {
		return p.reply(ctx, "权限系统未初始化")
	}
	if !p.PermPlugin.RemoveFromACL(userID) {
		return p.reply(ctx, fmt.Sprintf("用户 %s 不在列表中", userID))
	}
	return p.reply(ctx, fmt.Sprintf("已移除: %s", userID))
}

func (p *Plugin) handleACLList(ctx *eventctx.Context) error {
	if !p.checkPermission(ctx, "acl.view") && !p.hasAdminRole(ctx) {
		return p.reply(ctx, "权限不足：需要管理员权限")
	}
	if p.AclPlugin != nil {
		entries := p.AclPlugin.List()
		var msg strings.Builder
		msg.WriteString(fmt.Sprintf("黑白名单 %s 模式 (%d 用户):\n", p.AclPlugin.GetMode(), len(entries)))
		for i, e := range entries {
			msg.WriteString(fmt.Sprintf("%d. %s\n", i+1, e.UserID))
		}
		return p.reply(ctx, msg.String())
	}
	if p.PermPlugin == nil {
		return p.reply(ctx, "权限系统未初始化")
	}
	users := p.PermPlugin.ListACL()
	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("黑白名单 %s 模式 (%d 用户):\n", p.PermPlugin.GetACLMode().String(), len(users)))
	for i, u := range users {
		msg.WriteString(fmt.Sprintf("%d. %s\n", i+1, u.UserID))
	}
	return p.reply(ctx, msg.String())
}

func (p *Plugin) handleACLClear(ctx *eventctx.Context) error {
	if !p.checkPermission(ctx, "acl.manage") && !p.hasAdminRole(ctx) {
		return p.reply(ctx, "权限不足：需要管理员权限")
	}
	if p.AclPlugin != nil {
		count := p.AclPlugin.Count()
		p.AclPlugin.Clear()
		return p.reply(ctx, fmt.Sprintf("已清空黑白名单（移除 %d 个用户）", count))
	}
	if p.PermPlugin == nil {
		return p.reply(ctx, "权限系统未初始化")
	}
	return p.reply(ctx, fmt.Sprintf("已清空黑白名单（移除 %d 个用户）", p.PermPlugin.ClearACL()))
}

func (p *Plugin) handleACLStats(ctx *eventctx.Context) error {
	if !p.checkPermission(ctx, "acl.view") && !p.hasAdminRole(ctx) {
		return p.reply(ctx, "权限不足：需要管理员权限")
	}
	if p.AclPlugin != nil {
		return p.reply(ctx, fmt.Sprintf("黑白名单: 模式=%s, 用户数=%d", p.AclPlugin.GetMode(), p.AclPlugin.Count()))
	}
	if p.PermPlugin == nil {
		return p.reply(ctx, "权限系统未初始化")
	}
	stats := p.PermPlugin.GetACLStats()
	return p.reply(ctx, fmt.Sprintf("黑白名单: 模式=%s, 用户数=%d", stats.Mode.String(), stats.UserCount))
}

// Dependencies 返回依赖列表
func (p *Plugin) Dependencies() []string { return []string{"permission"} }
