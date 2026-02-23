package admin

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/command"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/plugin"
	"github.com/KomeiDiSanXian/remilia/plugins/acl"
	"github.com/KomeiDiSanXian/remilia/plugins/core/permission"
	"github.com/KomeiDiSanXian/remilia/plugins/verifycode"
)

// Plugin 管理插件
type Plugin struct {
	PluginManager *plugin.Manager
	PermPlugin    *permission.Plugin

	// 独立插件（优先使用，若已注册）
	AclPlugin *acl.Plugin
	VcPlugin  *verifycode.Plugin

	setupCtx *plugin.SetupContext // 用于注册可追踪的 Matcher
}

// New 创建管理插件（v2 API）
func New() *plugin.PluginDescriptor {
	// 创建内部实例
	v1Plugin := newAdminPluginInternal()

	return &plugin.PluginDescriptor{
		Name:        "admin",
		Version:     "2.1.0",
		Author:      "Remilia Team",
		Description: "机器人管理核心插件，提供插件管理、权限管理和配置管理功能",
		Category:    "系统",
		Tags:        []string{"管理", "系统", "核心"},
		Deps:        []string{"permission"},
		HelpText: `管理插件使用说明：

插件管理：
  /plugin list - 列出所有插件
  /plugin info <名称> - 查看插件详情
  /plugin enable <名称> - 启用插件
  /plugin disable <名称> - 禁用插件
  /plugin reload <名称> - 重载插件

权限管理：
  /perm grant <用户> <权限> - 授予权限
  /perm revoke <用户> <权限> - 撤销权限
  /perm list [用户] - 列出权限
  /perm role <用户> <角色> - 分配角色

验证码管理：
  /code gen <角色> [有效期] [次数] - 生成验证码
    示例: /code gen admin 1h 0  （生成1小时有效的一次性管理员验证码）
  /code verify <验证码> - 使用验证码获取权限
  /code list - 列出所有有效验证码
  /code revoke <验证码> - 撤销验证码

黑白名单管理：
  /acl mode <模式> - 设置模式（disabled/blacklist/whitelist）
  /acl add <用户ID> [备注] - 添加用户到列表
  /acl remove <用户ID> - 从列表移除用户
  /acl list - 列出所有用户
  /acl clear - 清空列表
  /acl stats - 查看统计信息

系统信息：
  /status - 查看系统状态
  /info - 查看机器人信息`,

		Setup: func(ctx *plugin.SetupContext) error {
			logger.Info("[AdminPlugin] Loading admin plugin (v2)...")

			// 获取依赖，MustGet("permission") 现在直接返回 *permission.Plugin
			permAPI := ctx.MustGet("permission")

			// 设置引用
			v1Plugin.PluginManager = ctx.Manager
			v1Plugin.setupCtx = ctx // 保存 SetupContext 以便注册可追踪的命令
			if permAPI != nil {
				v1Plugin.PermPlugin = permAPI.(*permission.Plugin)
			}

			// 可选：绑定独立 ACL 插件（优先于 permission 内置 ACL）
			if aclRaw, ok := ctx.Get("acl"); ok {
				if aclPlugin, ok := aclRaw.(*acl.Plugin); ok {
					v1Plugin.AclPlugin = aclPlugin
					logger.Info("[AdminPlugin] Using standalone acl plugin for ACL commands")
				}
			}

			// 可选：绑定独立 verifycode 插件（优先于 permission 内置验证码）
			if vcRaw, ok := ctx.Get("verifycode"); ok {
				if vcPlugin, ok := vcRaw.(*verifycode.Plugin); ok {
					v1Plugin.VcPlugin = vcPlugin
					logger.Info("[AdminPlugin] Using standalone verifycode plugin for /code commands")
				}
			}

			// 加载插件
			return v1Plugin.Load(ctx.Engine)
		},

		Teardown: func() error {
			logger.Info("[AdminPlugin] Admin plugin unloaded")
			return nil
		},
	}
}

// newAdminPluginInternal 创建管理插件内部实例
func newAdminPluginInternal() *Plugin {
	return &Plugin{}
}

// Load 加载插件
func (p *Plugin) Load(eng *engine.Engine) error {
	logger.Info("[AdminPlugin] Loading admin plugin...")

	// 注册主命令（使用子命令模式）
	p.registerPluginCommand(eng)
	p.registerPermCommand(eng)
	p.registerCodeCommand(eng)
	p.registerACLCommand(eng)

	// 注册系统命令
	p.registerSystemCommands(eng)

	logger.Info("[AdminPlugin] Admin plugin loaded successfully")
	return nil
}

// SetPluginManager 设置插件管理器
func (p *Plugin) SetPluginManager(pm *plugin.Manager) {
	p.PluginManager = pm
}

// SetPermissionPlugin 设置权限插件
func (p *Plugin) SetPermissionPlugin(pp *permission.Plugin) {
	p.PermPlugin = pp
}

// registerPluginCommand 注册插件管理命令（子命令模式）
func (p *Plugin) registerPluginCommand(eng *engine.Engine) {
	pluginCmd := &command.Definition{
		Name:        "plugin",
		Description: "插件管理",
		Usage:       "/plugin <子命令> [参数]",
		Category:    "系统",
		SubCommands: []*command.Definition{
			{
				Name:        "list",
				Description: "列出所有插件",
				Usage:       "/plugin list",
				Examples:    []string{"/plugin list"},
			},
			{
				Name:        "info",
				Description: "查看插件详情",
				Usage:       "/plugin info <插件名>",
				Arguments: []*command.Argument{
					{
						Name:        "name",
						Type:        command.ArgTypeString,
						Description: "插件名称",
						Required:    true,
					},
				},
				Examples: []string{"/plugin info help", "/plugin info permission"},
			},
			{
				Name:        "reload",
				Description: "重载插件",
				Usage:       "/plugin reload <插件名>",
				Arguments: []*command.Argument{
					{
						Name:        "name",
						Type:        command.ArgTypeString,
						Description: "插件名称",
						Required:    true,
					},
				},
				Examples: []string{"/plugin reload help"},
			},
		},
	}

	if p.setupCtx != nil {
		p.setupCtx.RegisterCommand(dto.C2CMessageCreate, "/plugin").
			SetDefinition(pluginCmd).
			Handle(p.handlePluginCommand)
	} else {
		eng.OnCommand(dto.C2CMessageCreate, "/plugin").
			SetDefinition(pluginCmd).
			Handle(p.handlePluginCommand)
	}
}

// handlePluginCommand 统一处理 plugin 命令
func (p *Plugin) handlePluginCommand(ctx *eventctx.Context) error {
	content := ctx.GetMessageContent()
	args, err := command.ParseCommandLine(content)
	if err != nil {
		return p.reply(ctx, "❌ 命令解析失败: "+err.Error())
	}

	subCommand := args.Get(0)
	if subCommand == "" {
		return p.showPluginHelp(ctx)
	}

	switch subCommand {
	case "list":
		return p.handlePluginList(ctx)
	case "info":
		return p.handlePluginInfo(ctx, args)
	case "reload":
		return p.handlePluginReload(ctx, args)
	default:
		return p.reply(ctx, fmt.Sprintf("❌ 未知的子命令: %s\n使用 /plugin 查看帮助", subCommand))
	}
}

// showPluginHelp 显示插件命令帮助
func (p *Plugin) showPluginHelp(ctx *eventctx.Context) error {
	var msg strings.Builder
	msg.WriteString("📦 插件管理\n")
	msg.WriteString(strings.Repeat("=", 40) + "\n\n")
	msg.WriteString("可用命令:\n")
	msg.WriteString("  /plugin list - 列出所有插件\n")
	msg.WriteString("  /plugin info <插件名> - 查看插件详情\n")
	msg.WriteString("  /plugin reload <插件名> - 重载插件\n")
	return p.reply(ctx, msg.String())
}

// registerPermCommand 注册权限管理命令（子命令模式）
func (p *Plugin) registerPermCommand(eng *engine.Engine) {
	permCmd := &command.Definition{
		Name:        "perm",
		Description: "权限管理",
		Usage:       "/perm <子命令> [参数]",
		Category:    "权限",
		SubCommands: []*command.Definition{
			{
				Name:        "grant",
				Description: "授予用户权限",
				Usage:       "/perm grant <用户ID> <权限>",
				Arguments: []*command.Argument{
					{Name: "userID", Type: command.ArgTypeString, Description: "用户ID", Required: true},
					{Name: "permission", Type: command.ArgTypeString, Description: "权限", Required: true},
				},
				Examples: []string{"/perm grant USER123 command.use"},
			},
			{
				Name:        "revoke",
				Description: "撤销用户权限",
				Usage:       "/perm revoke <用户ID> <权限>",
				Arguments: []*command.Argument{
					{Name: "userID", Type: command.ArgTypeString, Description: "用户ID", Required: true},
					{Name: "permission", Type: command.ArgTypeString, Description: "权限", Required: true},
				},
				Examples: []string{"/perm revoke USER123 command.use"},
			},
			{
				Name:        "list",
				Description: "列出用户权限",
				Usage:       "/perm list [用户ID]",
				Arguments: []*command.Argument{
					{Name: "userID", Type: command.ArgTypeString, Description: "用户ID（可选）", Required: false},
				},
				Examples: []string{"/perm list", "/perm list USER123"},
			},
			{
				Name:        "role",
				Description: "分配角色给用户",
				Usage:       "/perm role <用户ID> <角色>",
				Arguments: []*command.Argument{
					{Name: "userID", Type: command.ArgTypeString, Description: "用户ID", Required: true},
					{Name: "role", Type: command.ArgTypeString, Description: "角色名", Required: true},
				},
				Examples: []string{"/perm role USER123 admin"},
			},
		},
	}

	if p.setupCtx != nil {
		p.setupCtx.RegisterCommand(dto.C2CMessageCreate, "/perm").
			SetDefinition(permCmd).
			Handle(p.handlePermCommand)
	} else {
		eng.OnCommand(dto.C2CMessageCreate, "/perm").
			SetDefinition(permCmd).
			Handle(p.handlePermCommand)
	}
}

// handlePermCommand 统一处理 perm 命令
func (p *Plugin) handlePermCommand(ctx *eventctx.Context) error {
	content := ctx.GetMessageContent()
	args, err := command.ParseCommandLine(content)
	if err != nil {
		return p.reply(ctx, "❌ 命令解析失败: "+err.Error())
	}

	subCommand := args.Get(0)
	if subCommand == "" {
		return p.showPermHelp(ctx)
	}

	switch subCommand {
	case "grant":
		return p.handlePermGrant(ctx, args)
	case "revoke":
		return p.handlePermRevoke(ctx, args)
	case "list":
		return p.handlePermList(ctx, args)
	case "role":
		return p.handlePermRole(ctx, args)
	default:
		return p.reply(ctx, fmt.Sprintf("❌ 未知的子命令: %s\n使用 /perm 查看帮助", subCommand))
	}
}

// showPermHelp 显示权限命令帮助
func (p *Plugin) showPermHelp(ctx *eventctx.Context) error {
	var msg strings.Builder
	msg.WriteString("🔑 权限管理\n")
	msg.WriteString(strings.Repeat("=", 40) + "\n\n")
	msg.WriteString("可用命令:\n")
	msg.WriteString("  /perm grant <用户ID> <权限> - 授予权限\n")
	msg.WriteString("  /perm revoke <用户ID> <权限> - 撤销权限\n")
	msg.WriteString("  /perm list [用户ID] - 列出权限\n")
	msg.WriteString("  /perm role <用户ID> <角色> - 分配角色\n")
	return p.reply(ctx, msg.String())
}

// registerSystemCommands 注册系统命令
func (p *Plugin) registerSystemCommands(eng *engine.Engine) {
	if p.setupCtx != nil {
		// /status
		p.setupCtx.RegisterCommand(dto.C2CMessageCreate, "/status").
			Handle(p.handleStatus)
		// /info
		p.setupCtx.RegisterCommand(dto.C2CMessageCreate, "/info").
			Handle(p.handleInfo)
	} else {
		eng.OnCommand(dto.C2CMessageCreate, "/status").
			Handle(p.handleStatus)
		eng.OnCommand(dto.C2CMessageCreate, "/info").
			Handle(p.handleInfo)
	}
}

// handlePluginList 处理插件列表命令
func (p *Plugin) handlePluginList(ctx *eventctx.Context) error {
	if p.PluginManager == nil {
		return p.reply(ctx, "插件管理器未初始化")
	}

	// 检查权限
	if !p.checkPermission(ctx, "plugin.list") {
		return p.reply(ctx, "❌ 权限不足")
	}

	plugins := p.PluginManager.ListWithMetadata()

	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("📦 已加载插件列表 (共 %d 个)\n", len(plugins)))
	msg.WriteString(strings.Repeat("=", 30) + "\n\n")

	// 按分类组织
	categories := make(map[string][]*plugin.Metadata)
	for _, meta := range plugins {
		category := meta.Category
		if category == "" {
			category = "其他"
		}
		categories[category] = append(categories[category], meta)
	}

	for category, metas := range categories {
		msg.WriteString(fmt.Sprintf("【%s】\n", category))
		for _, meta := range metas {
			status := "✅"
			if meta.Hidden {
				status = "🔒"
			}
			msg.WriteString(fmt.Sprintf("  %s %s v%s\n", status, meta.Name, meta.Version))
			if meta.Description != "" {
				msg.WriteString(fmt.Sprintf("     %s\n", meta.Description))
			}
		}
		msg.WriteString("\n")
	}

	return p.reply(ctx, msg.String())
}

// handlePluginInfo 处理插件信息命令
func (p *Plugin) handlePluginInfo(ctx *eventctx.Context, args *command.Args) error {
	if p.PluginManager == nil {
		return p.reply(ctx, "插件管理器未初始化")
	}

	// 检查权限
	if !p.checkPermission(ctx, "plugin.info") {
		return p.reply(ctx, "❌ 权限不足")
	}

	pluginName := args.Get(1) // Get(0)="info", Get(1)=插件名

	if pluginName == "" {
		return p.reply(ctx, "用法: /plugin info <插件名>")
	}

	meta, ok := p.PluginManager.GetMetadata(pluginName)
	if !ok || meta == nil {
		return p.reply(ctx, fmt.Sprintf("插件 '%s' 不存在", pluginName))
	}

	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("🔌 插件【%s】详细信息\n", meta.Name))
	msg.WriteString(strings.Repeat("=", 30) + "\n\n")
	msg.WriteString(fmt.Sprintf("📌 版本: %s\n", meta.Version))
	msg.WriteString(fmt.Sprintf("👤 作者: %s\n", meta.Author))
	msg.WriteString(fmt.Sprintf("📂 分类: %s\n", meta.Category))
	msg.WriteString(fmt.Sprintf("📝 描述: %s\n", meta.Description))

	if len(meta.Tags) > 0 {
		msg.WriteString(fmt.Sprintf("🏷️  标签: %s\n", strings.Join(meta.Tags, ", ")))
	}

	if len(meta.Dependencies) > 0 {
		msg.WriteString(fmt.Sprintf("📦 依赖: %s\n", strings.Join(meta.Dependencies, ", ")))
	}

	if meta.Homepage != "" {
		msg.WriteString(fmt.Sprintf("🏠 主页: %s\n", meta.Homepage))
	}

	if meta.HelpText != "" {
		msg.WriteString(fmt.Sprintf("\n💡 帮助:\n%s\n", meta.HelpText))
	}

	return p.reply(ctx, msg.String())
}

// handlePluginReload 处理插件重载命令
func (p *Plugin) handlePluginReload(ctx *eventctx.Context, args *command.Args) error {
	if p.PluginManager == nil {
		return p.reply(ctx, "插件管理器未初始化")
	}

	// 检查权限
	if !p.checkPermission(ctx, "plugin.reload") {
		return p.reply(ctx, "❌ 权限不足")
	}

	pluginName := args.Get(1) // Get(0)="reload", Get(1)=插件名

	if pluginName == "" {
		return p.reply(ctx, "用法: /plugin reload <插件名>")
	}

	// 执行重载
	err := p.PluginManager.Reload(pluginName)
	if err != nil {
		return p.reply(ctx, fmt.Sprintf("❌ 重载失败: %v", err))
	}

	return p.reply(ctx, fmt.Sprintf("✅ 插件 '%s' 重载成功", pluginName))
}

// handlePermGrant 处理权限授予命令
func (p *Plugin) handlePermGrant(ctx *eventctx.Context, args *command.Args) error {
	if p.PermPlugin == nil {
		return p.reply(ctx, "权限插件未初始化")
	}

	// 检查权限
	if !p.checkPermission(ctx, "perm.grant") {
		return p.reply(ctx, "❌ 权限不足")
	}

	userID := args.Get(1)  // Get(0)="grant", Get(1)=用户ID
	permStr := args.Get(2) // Get(2)=权限

	if userID == "" || permStr == "" {
		return p.reply(ctx, "用法: /perm grant <用户ID> <权限>")
	}

	err := p.PermPlugin.Grant(userID, permStr)
	if err != nil {
		return p.reply(ctx, fmt.Sprintf("❌ 授予失败: %v", err))
	}

	return p.reply(ctx, fmt.Sprintf("✅ 已授予用户 '%s' 权限 '%s'", userID, permStr))
}

// handlePermRevoke 处理权限撤销命令
func (p *Plugin) handlePermRevoke(ctx *eventctx.Context, args *command.Args) error {
	if p.PermPlugin == nil {
		return p.reply(ctx, "权限插件未初始化")
	}

	// 检查权限
	if !p.checkPermission(ctx, "perm.revoke") {
		return p.reply(ctx, "❌ 权限不足")
	}

	userID := args.Get(1)  // Get(0)="revoke", Get(1)=用户ID
	permStr := args.Get(2) // Get(2)=权限

	if userID == "" || permStr == "" {
		return p.reply(ctx, "用法: /perm revoke <用户ID> <权限>")
	}

	err := p.PermPlugin.Revoke(userID, permStr)
	if err != nil {
		return p.reply(ctx, fmt.Sprintf("❌ 撤销失败: %v", err))
	}

	return p.reply(ctx, fmt.Sprintf("✅ 已撤销用户 '%s' 的权限 '%s'", userID, permStr))
}

// handlePermList 处理权限列表命令
func (p *Plugin) handlePermList(ctx *eventctx.Context, args *command.Args) error {
	if p.PermPlugin == nil {
		return p.reply(ctx, "权限插件未初始化")
	}

	// 检查权限
	if !p.checkPermission(ctx, "perm.list") {
		return p.reply(ctx, "❌ 权限不足")
	}

	userID := args.Get(1) // Get(0)="list", Get(1)=用户ID（可选）
	if userID == "" {
		userID = ctx.GetUserID()
	}

	perms := p.PermPlugin.GetUserPermissions(userID)
	roles := p.PermPlugin.GetUserRoles(userID)

	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("👤 用户 '%s' 的权限信息\n", userID))
	msg.WriteString(strings.Repeat("=", 30) + "\n\n")

	if len(roles) > 0 {
		msg.WriteString(fmt.Sprintf("👥 角色 (%d):\n", len(roles)))
		for _, role := range roles {
			msg.WriteString(fmt.Sprintf("  • %s\n", role))
		}
		msg.WriteString("\n")
	}

	if len(perms) > 0 {
		msg.WriteString(fmt.Sprintf("🔑 权限 (%d):\n", len(perms)))
		for _, perm := range perms {
			msg.WriteString(fmt.Sprintf("  • %s\n", perm))
		}
	} else {
		msg.WriteString("该用户没有任何权限\n")
	}

	return p.reply(ctx, msg.String())
}

// handlePermRole 处理角色分配命令
func (p *Plugin) handlePermRole(ctx *eventctx.Context, args *command.Args) error {
	if p.PermPlugin == nil {
		return p.reply(ctx, "权限插件未初始化")
	}

	// 检查权限
	if !p.checkPermission(ctx, "perm.role") {
		return p.reply(ctx, "❌ 权限不足")
	}

	userID := args.Get(1)   // Get(0)="role", Get(1)=用户ID
	roleName := args.Get(2) // Get(2)=角色

	if userID == "" || roleName == "" {
		return p.reply(ctx, "用法: /perm role <用户ID> <角色>")
	}

	err := p.PermPlugin.AssignRole(userID, roleName)
	if err != nil {
		return p.reply(ctx, fmt.Sprintf("❌ 分配失败: %v", err))
	}

	return p.reply(ctx, fmt.Sprintf("✅ 已为用户 '%s' 分配角色 '%s'", userID, roleName))
}

// handleStatus 处理状态查询命令
func (p *Plugin) handleStatus(ctx *eventctx.Context) error {
	// 检查权限
	if !p.checkPermission(ctx, "status.view") {
		return p.reply(ctx, "❌ 权限不足")
	}

	var msg strings.Builder
	msg.WriteString("🤖 机器人状态\n")
	msg.WriteString(strings.Repeat("=", 30) + "\n\n")
	msg.WriteString("✅ 运行中\n")

	if p.PluginManager != nil {
		plugins := p.PluginManager.List()
		msg.WriteString(fmt.Sprintf("📦 已加载插件: %d 个\n", len(plugins)))
	}

	return p.reply(ctx, msg.String())
}

// handleInfo 处理信息查询命令
func (p *Plugin) handleInfo(ctx *eventctx.Context) error {
	msg := `🤖 Remilia Bot

📌 版本: v0.9.0+
🏷️  框架: Remilia
👥 开发: Remilia Team

💡 使用 /help 查看帮助`

	return p.reply(ctx, msg)
}

// checkPermission 检查权限
func (p *Plugin) checkPermission(ctx *eventctx.Context, perm string) bool {
	if p.PermPlugin == nil {
		return true // 如果没有权限插件，默认允许
	}

	userID := ctx.GetUserID()
	return p.PermPlugin.HasPermission(userID, perm)
}

// reply 回复消息
func (p *Plugin) reply(ctx *eventctx.Context, content string) error {
	msg := &dto.Message{
		Type:    dto.TextMessage,
		Content: content,
	}

	_, err := ctx.ReplyPrivate(msg)
	return err
}

// === 验证码相关功能 ===

// registerCodeCommand 注册验证码管理命令（子命令模式）
func (p *Plugin) registerCodeCommand(eng *engine.Engine) {
	codeCmd := &command.Definition{
		Name:        "code",
		Description: "验证码管理",
		Usage:       "/code <子命令> [参数]",
		Category:    "权限",
		SubCommands: []*command.Definition{
			{
				Name:        "gen",
				Description: "生成验证码",
				Usage:       "/code gen <角色> [有效期] [次数]",
				Arguments: []*command.Argument{
					{Name: "role", Type: command.ArgTypeString, Description: "角色名", Required: true},
					{Name: "expiry", Type: command.ArgTypeString, Description: "有效期（如 30m, 1h）", Required: false},
					{Name: "maxUses", Type: command.ArgTypeInt, Description: "最大使用次数", Required: false},
				},
				Examples: []string{"/code gen admin 1h 0", "/code gen user 30m 5"},
			},
			{
				Name:        "verify",
				Description: "使用验证码获取权限",
				Usage:       "/code verify <验证码>",
				Arguments: []*command.Argument{
					{Name: "code", Type: command.ArgTypeString, Description: "验证码", Required: true},
				},
				Examples: []string{"/code verify ABC123"},
			},
			{
				Name:        "list",
				Description: "列出所有有效验证码",
				Usage:       "/code list",
				Examples:    []string{"/code list"},
			},
			{
				Name:        "revoke",
				Description: "撤销验证码",
				Usage:       "/code revoke <验证码>",
				Arguments: []*command.Argument{
					{Name: "code", Type: command.ArgTypeString, Description: "验证码", Required: true},
				},
				Examples: []string{"/code revoke ABC123"},
			},
		},
	}

	// 注册私聊命令
	if p.setupCtx != nil {
		p.setupCtx.RegisterCommand(dto.C2CMessageCreate, "/code").
			SetDefinition(codeCmd).
			Handle(p.handleCodeCommand)
		// 注册群聊命令（verify 子命令）
		p.setupCtx.RegisterCommand(dto.GroupAtMessageCreate, "/code").
			SetDefinition(codeCmd).
			Handle(p.handleCodeCommand)
	} else {
		eng.OnCommand(dto.C2CMessageCreate, "/code").
			SetDefinition(codeCmd).
			Handle(p.handleCodeCommand)
		eng.OnCommand(dto.GroupAtMessageCreate, "/code").
			SetDefinition(codeCmd).
			Handle(p.handleCodeCommand)
	}
}

// handleCodeCommand 统一处理 code 命令
func (p *Plugin) handleCodeCommand(ctx *eventctx.Context) error {
	content := ctx.GetMessageContent()
	args, err := command.ParseCommandLine(content)
	if err != nil {
		return p.reply(ctx, "❌ 命令解析失败: "+err.Error())
	}

	subCommand := args.Get(0)
	if subCommand == "" {
		return p.showCodeHelp(ctx)
	}

	switch subCommand {
	case "gen":
		return p.handleCodeGen(ctx, args)
	case "verify":
		return p.handleCodeVerify(ctx, args)
	case "list":
		return p.handleCodeList(ctx)
	case "revoke":
		return p.handleCodeRevoke(ctx, args)
	default:
		return p.reply(ctx, fmt.Sprintf("❌ 未知的子命令: %s\n使用 /code 查看帮助", subCommand))
	}
}

// showCodeHelp 显示验证码命令帮助
func (p *Plugin) showCodeHelp(ctx *eventctx.Context) error {
	var msg strings.Builder
	msg.WriteString("🔑 验证码管理\n")
	msg.WriteString(strings.Repeat("=", 40) + "\n\n")
	msg.WriteString("可用命令:\n")
	msg.WriteString("  /code gen <角色> [有效期] [次数] - 生成验证码\n")
	msg.WriteString("  /code verify <验证码> - 使用验证码\n")
	msg.WriteString("  /code list - 列出所有验证码\n")
	msg.WriteString("  /code revoke <验证码> - 撤销验证码\n")
	return p.reply(ctx, msg.String())
}

// handleCodeGen 生成验证码
func (p *Plugin) handleCodeGen(ctx *eventctx.Context, args *command.Args) error {
	// 检查权限（只有管理员可以生成验证码）
	if !p.checkPermission(ctx, "code.gen") && !p.hasAdminRole(ctx) {
		return p.reply(ctx, "❌ 权限不足：需要管理员权限才能生成验证码")
	}

	role := args.Get(1)
	if role == "" {
		return p.reply(ctx, "❌ 请指定角色\n用法: /code gen <角色> [有效期] [最大使用次数]\n示例: /code gen admin 1h 0")
	}

	expiryStr := args.Get(2)
	expiry := 30 * time.Minute
	if expiryStr != "" {
		var err error
		expiry, err = time.ParseDuration(expiryStr)
		if err != nil {
			return p.reply(ctx, fmt.Sprintf("❌ 无效的有效期格式: %s\n示例: 30m, 1h, 24h", expiryStr))
		}
	}

	maxUses := 0
	if maxUsesStr := args.Get(3); maxUsesStr != "" {
		if n, err := command.ParseInt(maxUsesStr); err == nil {
			maxUses = n
		} else {
			return p.reply(ctx, fmt.Sprintf("❌ 无效的使用次数: %s", maxUsesStr))
		}
	}

	// 优先使用独立 verifycode 插件
	if p.VcPlugin != nil {
		code, err := p.VcPlugin.Generate(verifycode.CodeConfig{
			Role:    role,
			TTL:     expiry,
			MaxUses: maxUses,
		})
		if err != nil {
			return p.reply(ctx, fmt.Sprintf("❌ 生成验证码失败: %v", err))
		}
		return p.reply(ctx, fmt.Sprintf("✅ 验证码已生成\n🔑 验证码: %s\n👤 授予角色: %s\n⏰ 有效期: %v\n💡 使用: /code verify %s", code, role, expiry, code))
	}

	// 回退：使用 permission 内置验证码
	if p.PermPlugin == nil {
		return p.reply(ctx, "❌ 权限系统未初始化")
	}
	code, err := p.PermPlugin.GenerateVerificationCode(role, expiry, maxUses)
	if err != nil {
		return p.reply(ctx, fmt.Sprintf("❌ 生成验证码失败: %v", err))
	}
	var msg strings.Builder
	msg.WriteString("✅ 验证码已生成\n")
	msg.WriteString(fmt.Sprintf("🔑 验证码: %s\n", code))
	msg.WriteString(fmt.Sprintf("👤 授予角色: %s\n", role))
	msg.WriteString(fmt.Sprintf("⏰ 有效期: %v\n", expiry))
	if maxUses == 0 {
		msg.WriteString("🎫 使用次数: 一次性\n")
	} else if maxUses < 0 {
		msg.WriteString("🎫 使用次数: 无限次\n")
	} else {
		msg.WriteString(fmt.Sprintf("🎫 使用次数: %d 次\n", maxUses))
	}
	msg.WriteString(fmt.Sprintf("\n💡 使用: /code verify %s", code))
	return p.reply(ctx, msg.String())
}

// handleCodeVerify 验证码验证
func (p *Plugin) handleCodeVerify(ctx *eventctx.Context, args *command.Args) error {
	code := args.Get(1)
	if code == "" {
		return p.reply(ctx, "❌ 请提供验证码\n用法: /code verify <验证码>")
	}
	userID := ctx.GetUserID()

	// 优先使用独立 verifycode 插件
	if p.VcPlugin != nil {
		role, err := p.VcPlugin.Verify(userID, code)
		if err != nil {
			return p.reply(ctx, fmt.Sprintf("❌ 验证失败: %v", err))
		}
		return p.reply(ctx, fmt.Sprintf("✅ 验证成功！您已获得角色: %s", role))
	}

	// 回退：使用 permission 内置验证码
	if p.PermPlugin == nil {
		return p.reply(ctx, "❌ 权限系统未初始化")
	}
	role, err := p.PermPlugin.VerifyAndGrantRole(code, userID)
	if err != nil {
		return p.reply(ctx, fmt.Sprintf("❌ 验证失败: %v", err))
	}
	return p.reply(ctx, fmt.Sprintf("✅ 验证成功！您已获得角色: %s", role))
}

// handleCodeList 列出验证码
func (p *Plugin) handleCodeList(ctx *eventctx.Context) error {
	// 检查权限
	if !p.checkPermission(ctx, "code.list") && !p.hasAdminRole(ctx) {
		return p.reply(ctx, "❌ 权限不足：需要管理员权限")
	}

	if p.VcPlugin != nil {
		codes := p.VcPlugin.ListValid()
		if len(codes) == 0 {
			return p.reply(ctx, "📋 当前没有有效的验证码")
		}
		var msg strings.Builder
		msg.WriteString(fmt.Sprintf("📋 有效验证码列表 (共 %d 个)\n", len(codes)))
		for i, c := range codes {
			msg.WriteString(fmt.Sprintf("%d. %s → 角色: %s", i+1, c.Code, c.Role))
			if c.ExpiresAt != nil {
				msg.WriteString(fmt.Sprintf(" (过期: %s)", c.ExpiresAt.Format("15:04:05")))
			}
			msg.WriteString("\n")
		}
		return p.reply(ctx, msg.String())
	}

	if p.PermPlugin == nil {
		return p.reply(ctx, "❌ 权限系统未初始化")
	}
	codes := p.PermPlugin.ListVerificationCodes()
	if len(codes) == 0 {
		return p.reply(ctx, "📋 当前没有有效的验证码")
	}
	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("📋 有效验证码列表 (共 %d 个)\n", len(codes)))
	for i, code := range codes {
		msg.WriteString(fmt.Sprintf("%d. %s → 角色: %s (过期: %s)\n", i+1, code.Code, code.Role, code.ExpiresAt.Format("15:04:05")))
	}
	return p.reply(ctx, msg.String())
}

// handleCodeRevoke 撤销验证码
func (p *Plugin) handleCodeRevoke(ctx *eventctx.Context, args *command.Args) error {
	// 检查权限
	if !p.checkPermission(ctx, "code.revoke") && !p.hasAdminRole(ctx) {
		return p.reply(ctx, "❌ 权限不足：需要管理员权限")
	}

	code := args.Get(1)
	if code == "" {
		return p.reply(ctx, "❌ 请提供验证码\n用法: /code revoke <验证码>")
	}

	if p.VcPlugin != nil {
		if !p.VcPlugin.Revoke(code) {
			return p.reply(ctx, fmt.Sprintf("❌ 验证码 %s 不存在", code))
		}
		return p.reply(ctx, fmt.Sprintf("✅ 验证码 %s 已撤销", code))
	}

	if p.PermPlugin == nil {
		return p.reply(ctx, "❌ 权限系统未初始化")
	}
	if err := p.PermPlugin.RevokeVerificationCode(code); err != nil {
		return p.reply(ctx, fmt.Sprintf("❌ 撤销失败: %v", err))
	}
	return p.reply(ctx, fmt.Sprintf("✅ 验证码 %s 已撤销", code))
}

// hasAdminRole 检查用户是否有管理员角色
func (p *Plugin) hasAdminRole(ctx *eventctx.Context) bool {
	if p.PermPlugin == nil {
		return false
	}

	userID := ctx.GetUserID()
	roles := p.PermPlugin.GetUserRoles(userID)
	return slices.Contains(roles, "admin")
}

// === 黑白名单相关功能 ===

// registerACLCommand 注册黑白名单管理命令（子命令模式）
func (p *Plugin) registerACLCommand(eng *engine.Engine) {
	aclCmd := &command.Definition{
		Name:        "acl",
		Description: "黑白名单管理",
		Usage:       "/acl <子命令> [参数]",
		Category:    "权限",
		SubCommands: []*command.Definition{
			{
				Name:        "mode",
				Description: "设置黑白名单模式",
				Usage:       "/acl mode <模式>",
				Arguments: []*command.Argument{
					{Name: "mode", Type: command.ArgTypeString, Description: "模式(disabled/blacklist/whitelist)", Required: false},
				},
				Examples: []string{"/acl mode blacklist", "/acl mode whitelist", "/acl mode disabled"},
			},
			{
				Name:        "add",
				Description: "添加用户到列表",
				Usage:       "/acl add <用户ID> [备注]",
				Arguments: []*command.Argument{
					{Name: "userID", Type: command.ArgTypeString, Description: "用户ID", Required: true},
					{Name: "note", Type: command.ArgTypeString, Description: "备注", Required: false},
				},
				Examples: []string{"/acl add USER123 违规用户", "/acl add VIP001 VIP会员"},
			},
			{
				Name:        "remove",
				Description: "从列表移除用户",
				Usage:       "/acl remove <用户ID>",
				Arguments: []*command.Argument{
					{Name: "userID", Type: command.ArgTypeString, Description: "用户ID", Required: true},
				},
				Examples: []string{"/acl remove USER123"},
			},
			{
				Name:        "list",
				Description: "列出所有用户",
				Usage:       "/acl list",
				Examples:    []string{"/acl list"},
			},
			{
				Name:        "clear",
				Description: "清空列表",
				Usage:       "/acl clear",
				Examples:    []string{"/acl clear"},
			},
			{
				Name:        "stats",
				Description: "查看统计信息",
				Usage:       "/acl stats",
				Examples:    []string{"/acl stats"},
			},
		},
	}

	if p.setupCtx != nil {
		p.setupCtx.RegisterCommand(dto.C2CMessageCreate, "/acl").
			SetDefinition(aclCmd).
			Handle(p.handleACLCommand)
	} else {
		eng.OnCommand(dto.C2CMessageCreate, "/acl").
			SetDefinition(aclCmd).
			Handle(p.handleACLCommand)
	}
}

// handleACLCommand 统一处理 acl 命令
func (p *Plugin) handleACLCommand(ctx *eventctx.Context) error {
	content := ctx.GetMessageContent()
	args, err := command.ParseCommandLine(content)
	if err != nil {
		return p.reply(ctx, "❌ 命令解析失败: "+err.Error())
	}

	subCommand := args.Get(0)
	if subCommand == "" {
		return p.showACLHelp(ctx)
	}

	switch subCommand {
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
		return p.reply(ctx, fmt.Sprintf("❌ 未知的子命令: %s\n使用 /acl 查看帮助", subCommand))
	}
}

// showACLHelp 显示黑白名单命令帮助
func (p *Plugin) showACLHelp(ctx *eventctx.Context) error {
	var msg strings.Builder
	msg.WriteString("🛡️  黑白名单管理\n")
	msg.WriteString(strings.Repeat("=", 40) + "\n\n")
	msg.WriteString("可用命令:\n")
	msg.WriteString("  /acl mode <模式> - 设置模式\n")
	msg.WriteString("  /acl add <用户ID> [备注] - 添加用户\n")
	msg.WriteString("  /acl remove <用户ID> - 移除用户\n")
	msg.WriteString("  /acl list - 列出所有用户\n")
	msg.WriteString("  /acl clear - 清空列表\n")
	msg.WriteString("  /acl stats - 查看统计\n")
	return p.reply(ctx, msg.String())
}

// handleACLMode 设置黑白名单模式
func (p *Plugin) handleACLMode(ctx *eventctx.Context, args *command.Args) error {
	// 检查权限
	if !p.checkPermission(ctx, "acl.manage") && !p.hasAdminRole(ctx) {
		return p.reply(ctx, "❌ 权限不足：需要管理员权限")
	}

	modeStr := args.Get(1) // Get(0)="mode", Get(1)=模式

	// 独立 ACL 插件路径
	if p.AclPlugin != nil {
		if modeStr == "" {
			currentMode := p.AclPlugin.GetMode()
			return p.reply(ctx, fmt.Sprintf("当前模式: %s\n\n可用模式:\n- disabled (禁用)\n- blacklist (黑名单)\n- whitelist (白名单)\n\n用法: /acl mode <模式>", currentMode))
		}
		mode, err := acl.ParseMode(modeStr)
		if err != nil {
			return p.reply(ctx, fmt.Sprintf("❌ 无效的模式: %s\n可用模式: disabled, blacklist, whitelist", modeStr))
		}
		p.AclPlugin.SetMode(mode)
		return p.reply(ctx, fmt.Sprintf("✅ 黑白名单模式已设置为: %s", mode))
	}

	// 回退：使用 permission 插件的内置 ACL
	if p.PermPlugin == nil {
		return p.reply(ctx, "❌ 权限系统未初始化")
	}
	if modeStr == "" {
		currentMode := p.PermPlugin.GetACLMode()
		return p.reply(ctx, fmt.Sprintf("当前模式: %s\n\n可用模式:\n- disabled (禁用)\n- blacklist (黑名单)\n- whitelist (白名单)\n\n用法: /acl mode <模式>", currentMode.String()))
	}
	var mode permission.ListMode
	switch strings.ToLower(modeStr) {
	case "disabled", "disable", "off":
		mode = permission.ModeDisabled
	case "blacklist", "black", "bl":
		mode = permission.ModeBlacklist
	case "whitelist", "white", "wl":
		mode = permission.ModeWhitelist
	default:
		return p.reply(ctx, fmt.Sprintf("❌ 无效的模式: %s\n可用模式: disabled, blacklist, whitelist", modeStr))
	}
	p.PermPlugin.SetACLMode(mode)
	var msg strings.Builder
	msg.WriteString("✅ 黑白名单模式已设置\n")
	msg.WriteString(strings.Repeat("=", 40) + "\n\n")
	msg.WriteString(fmt.Sprintf("🔧 当前模式: %s\n\n", mode.String()))
	switch mode {
	case permission.ModeDisabled:
		msg.WriteString("💡 说明: 黑白名单功能已禁用，所有用户都可以访问")
	case permission.ModeBlacklist:
		msg.WriteString("💡 说明: 黑名单模式，列表中的用户将被禁止访问\n")
		msg.WriteString("   使用 /acl add <用户ID> 添加到黑名单")
	case permission.ModeWhitelist:
		msg.WriteString("💡 说明: 白名单模式，只有列表中的用户可以访问\n")
		msg.WriteString("   使用 /acl add <用户ID> 添加到白名单")
	}

	return p.reply(ctx, msg.String())
}

// handleACLAdd 添加用户到黑白名单
func (p *Plugin) handleACLAdd(ctx *eventctx.Context, args *command.Args) error {
	// 检查权限
	if !p.checkPermission(ctx, "acl.manage") && !p.hasAdminRole(ctx) {
		return p.reply(ctx, "❌ 权限不足：需要管理员权限")
	}

	userID := args.Get(1)
	if userID == "" {
		return p.reply(ctx, "❌ 请指定用户ID\n用法: /acl add <用户ID> [备注]")
	}
	note := ""
	if args.Len() > 2 {
		var noteArgs []string
		for i := 2; i < args.Len(); i++ {
			noteArgs = append(noteArgs, args.Get(i))
		}
		note = strings.Join(noteArgs, " ")
	}

	if p.AclPlugin != nil {
		p.AclPlugin.Add(userID, note)
		return p.reply(ctx, fmt.Sprintf("✅ 用户 %s 已添加到 %s", userID, p.AclPlugin.GetMode()))
	}

	if p.PermPlugin == nil {
		return p.reply(ctx, "❌ 权限系统未初始化")
	}
	mode := p.PermPlugin.GetACLMode()
	if mode == permission.ModeDisabled {
		return p.reply(ctx, "❌ 黑白名单功能未启用\n请先使用 /acl mode 设置模式")
	}
	p.PermPlugin.AddToACL(userID, note)
	return p.reply(ctx, fmt.Sprintf("✅ 已添加用户 '%s' 到 %s", userID, mode.String()))
}

// handleACLRemove 从黑白名单移除用户
func (p *Plugin) handleACLRemove(ctx *eventctx.Context, args *command.Args) error {
	// 检查权限
	if !p.checkPermission(ctx, "acl.manage") && !p.hasAdminRole(ctx) {
		return p.reply(ctx, "❌ 权限不足：需要管理员权限")
	}

	userID := args.Get(1)
	if userID == "" {
		return p.reply(ctx, "❌ 请指定用户ID\n用法: /acl remove <用户ID>")
	}

	if p.AclPlugin != nil {
		removed := p.AclPlugin.Remove(userID)
		if !removed {
			return p.reply(ctx, fmt.Sprintf("❌ 用户 %s 不在列表中", userID))
		}
		return p.reply(ctx, fmt.Sprintf("✅ 已从列表中移除用户: %s", userID))
	}

	if p.PermPlugin == nil {
		return p.reply(ctx, "❌ 权限系统未初始化")
	}
	removed := p.PermPlugin.RemoveFromACL(userID)
	if !removed {
		return p.reply(ctx, fmt.Sprintf("❌ 用户 %s 不在列表中", userID))
	}

	return p.reply(ctx, fmt.Sprintf("✅ 已从列表中移除用户: %s", userID))
}

// handleACLList 列出黑白名单中的所有用户
func (p *Plugin) handleACLList(ctx *eventctx.Context) error {
	// 检查权限
	if !p.checkPermission(ctx, "acl.view") && !p.hasAdminRole(ctx) {
		return p.reply(ctx, "❌ 权限不足：需要管理员权限")
	}

	if p.AclPlugin != nil {
		mode := p.AclPlugin.GetMode()
		entries := p.AclPlugin.List()
		var msg strings.Builder
		msg.WriteString(fmt.Sprintf("📋 黑白名单 - %s模式 (%d 用户)\n", mode, len(entries)))
		if len(entries) == 0 {
			msg.WriteString("列表为空")
		} else {
			for i, e := range entries {
				msg.WriteString(fmt.Sprintf("%d. %s", i+1, e.UserID))
				if e.Remark != "" {
					msg.WriteString(fmt.Sprintf(" (%s)", e.Remark))
				}
				msg.WriteString("\n")
			}
		}
		return p.reply(ctx, msg.String())
	}

	if p.PermPlugin == nil {
		return p.reply(ctx, "❌ 权限系统未初始化")
	}
	mode := p.PermPlugin.GetACLMode()
	users := p.PermPlugin.ListACL()
	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("📋 黑白名单 - %s模式\n", mode.String()))
	msg.WriteString(strings.Repeat("=", 40) + "\n\n")
	if len(users) == 0 {
		msg.WriteString("列表为空")
	} else {
		msg.WriteString(fmt.Sprintf("共 %d 个用户:\n\n", len(users)))
		for i, user := range users {
			msg.WriteString(fmt.Sprintf("%d. %s", i+1, user.UserID))
			if user.Note != "" {
				msg.WriteString(fmt.Sprintf("\n   备注: %s", user.Note))
			}
			msg.WriteString("\n\n")
		}
	}

	return p.reply(ctx, msg.String())
}

// handleACLClear 清空黑白名单
func (p *Plugin) handleACLClear(ctx *eventctx.Context) error {
	// 检查权限
	if !p.checkPermission(ctx, "acl.manage") && !p.hasAdminRole(ctx) {
		return p.reply(ctx, "❌ 权限不足：需要管理员权限")
	}

	if p.AclPlugin != nil {
		count := p.AclPlugin.Count()
		p.AclPlugin.Clear()
		return p.reply(ctx, fmt.Sprintf("✅ 已清空黑白名单（移除 %d 个用户）", count))
	}

	if p.PermPlugin == nil {
		return p.reply(ctx, "❌ 权限系统未初始化")
	}
	count := p.PermPlugin.ClearACL()
	return p.reply(ctx, fmt.Sprintf("✅ 已清空黑白名单（移除 %d 个用户）", count))
}

// handleACLStats 查看黑白名单统计信息
func (p *Plugin) handleACLStats(ctx *eventctx.Context) error {
	// 检查权限
	if !p.checkPermission(ctx, "acl.view") && !p.hasAdminRole(ctx) {
		return p.reply(ctx, "❌ 权限不足：需要管理员权限")
	}

	if p.AclPlugin != nil {
		mode := p.AclPlugin.GetMode()
		count := p.AclPlugin.Count()
		return p.reply(ctx, fmt.Sprintf("📊 黑白名单统计\n🔧 模式: %s\n👥 用户数: %d", mode, count))
	}

	if p.PermPlugin == nil {
		return p.reply(ctx, "❌ 权限系统未初始化")
	}
	stats := p.PermPlugin.GetACLStats()
	var msg strings.Builder
	msg.WriteString("📊 黑白名单统计\n")
	msg.WriteString(strings.Repeat("=", 40) + "\n\n")
	msg.WriteString(fmt.Sprintf("🔧 当前模式: %s\n", stats.Mode.String()))
	msg.WriteString(fmt.Sprintf("👥 用户数量: %d\n", stats.UserCount))
	return p.reply(ctx, msg.String())
}

// Dependencies 返回依赖列表
func (p *Plugin) Dependencies() []string {
	return []string{"permission"}
}
