package admin

import (
	"fmt"
	"strings"

	"github.com/KomeiDiSanXian/remilia/command"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/plugin"
	"github.com/KomeiDiSanXian/remilia/plugins/core/permission"
)

// Plugin 管理插件
type Plugin struct {
	*plugin.BasePlugin
	pluginManager *plugin.Manager
	permPlugin    *permission.Plugin
}

// New 创建管理插件
func New() *Plugin {
	metadata := &plugin.Metadata{
		Name:        "admin",
		Version:     "1.0.0",
		Author:      "Remilia Team",
		Description: "机器人管理核心插件，提供插件管理、权限管理和配置管理功能",
		Category:    "系统",
		Tags:        []string{"管理", "系统", "核心"},
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

系统信息：
  /status - 查看系统状态
  /info - 查看机器人信息`,
	}

	return &Plugin{
		BasePlugin: plugin.NewBasePluginWithMetadata(metadata),
	}
}

// Load 加载插件
func (p *Plugin) Load(eng *engine.Engine) error {
	logger.Info("[AdminPlugin] Loading admin plugin...")

	// 注册插件管理命令
	p.registerPluginCommands(eng)

	// 注册权限管理命令
	p.registerPermissionCommands(eng)

	// 注册系统命令
	p.registerSystemCommands(eng)

	logger.Info("[AdminPlugin] Admin plugin loaded successfully")
	return nil
}

// SetPluginManager 设置插件管理器
func (p *Plugin) SetPluginManager(pm *plugin.Manager) {
	p.pluginManager = pm
}

// SetPermissionPlugin 设置权限插件
func (p *Plugin) SetPermissionPlugin(pp *permission.Plugin) {
	p.permPlugin = pp
}

// registerPluginCommands 注册插件管理命令
func (p *Plugin) registerPluginCommands(eng *engine.Engine) {
	// /plugin list
	p.OnCommand(eng, dto.C2CMessageCreate, "/plugin list").
		Handle(p.handlePluginList)

	// /plugin info <name>
	p.OnCommand(eng, dto.C2CMessageCreate, "/plugin info").
		Handle(p.handlePluginInfo)

	// /plugin reload <name>
	p.OnCommand(eng, dto.C2CMessageCreate, "/plugin reload").
		Handle(p.handlePluginReload)
}

// registerPermissionCommands 注册权限管理命令
func (p *Plugin) registerPermissionCommands(eng *engine.Engine) {
	// /perm grant <user> <permission>
	p.OnCommand(eng, dto.C2CMessageCreate, "/perm grant").
		Handle(p.handlePermGrant)

	// /perm revoke <user> <permission>
	p.OnCommand(eng, dto.C2CMessageCreate, "/perm revoke").
		Handle(p.handlePermRevoke)

	// /perm list [user]
	p.OnCommand(eng, dto.C2CMessageCreate, "/perm list").
		Handle(p.handlePermList)

	// /perm role <user> <role>
	p.OnCommand(eng, dto.C2CMessageCreate, "/perm role").
		Handle(p.handlePermRole)
}

// registerSystemCommands 注册系统命令
func (p *Plugin) registerSystemCommands(eng *engine.Engine) {
	// /status
	p.OnCommand(eng, dto.C2CMessageCreate, "/status").
		Handle(p.handleStatus)

	// /info
	p.OnCommand(eng, dto.C2CMessageCreate, "/info").
		Handle(p.handleInfo)
}

// handlePluginList 处理插件列表命令
func (p *Plugin) handlePluginList(ctx *eventctx.Context) error {
	if p.pluginManager == nil {
		return p.reply(ctx, "插件管理器未初始化")
	}

	// 检查权限
	if !p.checkPermission(ctx, "plugin.list") {
		return p.reply(ctx, "❌ 权限不足")
	}

	plugins := p.pluginManager.ListWithMetadata()

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
func (p *Plugin) handlePluginInfo(ctx *eventctx.Context) error {
	if p.pluginManager == nil {
		return p.reply(ctx, "插件管理器未初始化")
	}

	// 检查权限
	if !p.checkPermission(ctx, "plugin.info") {
		return p.reply(ctx, "❌ 权限不足")
	}

	content := ctx.GetMessageContent()
	args, _ := command.ParseCommandLine(content)
	pluginName := args.Get(0)

	if pluginName == "" {
		return p.reply(ctx, "用法: /plugin info <插件名>")
	}

	meta, ok := p.pluginManager.GetMetadata(pluginName)
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
func (p *Plugin) handlePluginReload(ctx *eventctx.Context) error {
	if p.pluginManager == nil {
		return p.reply(ctx, "插件管理器未初始化")
	}

	// 检查权限
	if !p.checkPermission(ctx, "plugin.reload") {
		return p.reply(ctx, "❌ 权限不足")
	}

	content := ctx.GetMessageContent()
	args, _ := command.ParseCommandLine(content)
	pluginName := args.Get(0)

	if pluginName == "" {
		return p.reply(ctx, "用法: /plugin reload <插件名>")
	}

	// 执行重载
	err := p.pluginManager.Reload(pluginName)
	if err != nil {
		return p.reply(ctx, fmt.Sprintf("❌ 重载失败: %v", err))
	}

	return p.reply(ctx, fmt.Sprintf("✅ 插件 '%s' 重载成功", pluginName))
}

// handlePermGrant 处理权限授予命令
func (p *Plugin) handlePermGrant(ctx *eventctx.Context) error {
	if p.permPlugin == nil {
		return p.reply(ctx, "权限插件未初始化")
	}

	// 检查权限
	if !p.checkPermission(ctx, "perm.grant") {
		return p.reply(ctx, "❌ 权限不足")
	}

	content := ctx.GetMessageContent()
	args, _ := command.ParseCommandLine(content)

	userID := args.Get(0)
	permission := args.Get(1)

	if userID == "" || permission == "" {
		return p.reply(ctx, "用法: /perm grant <用户ID> <权限>")
	}

	err := p.permPlugin.Grant(userID, permission)
	if err != nil {
		return p.reply(ctx, fmt.Sprintf("❌ 授予失败: %v", err))
	}

	return p.reply(ctx, fmt.Sprintf("✅ 已授予用户 '%s' 权限 '%s'", userID, permission))
}

// handlePermRevoke 处理权限撤销命令
func (p *Plugin) handlePermRevoke(ctx *eventctx.Context) error {
	if p.permPlugin == nil {
		return p.reply(ctx, "权限插件未初始化")
	}

	// 检查权限
	if !p.checkPermission(ctx, "perm.revoke") {
		return p.reply(ctx, "❌ 权限不足")
	}

	content := ctx.GetMessageContent()
	args, _ := command.ParseCommandLine(content)

	userID := args.Get(0)
	permission := args.Get(1)

	if userID == "" || permission == "" {
		return p.reply(ctx, "用法: /perm revoke <用户ID> <权限>")
	}

	err := p.permPlugin.Revoke(userID, permission)
	if err != nil {
		return p.reply(ctx, fmt.Sprintf("❌ 撤销失败: %v", err))
	}

	return p.reply(ctx, fmt.Sprintf("✅ 已撤销用户 '%s' 的权限 '%s'", userID, permission))
}

// handlePermList 处理权限列表命令
func (p *Plugin) handlePermList(ctx *eventctx.Context) error {
	if p.permPlugin == nil {
		return p.reply(ctx, "权限插件未初始化")
	}

	// 检查权限
	if !p.checkPermission(ctx, "perm.list") {
		return p.reply(ctx, "❌ 权限不足")
	}

	content := ctx.GetMessageContent()
	args, _ := command.ParseCommandLine(content)

	userID := args.Get(0)
	if userID == "" {
		userID = ctx.GetUserID()
	}

	perms := p.permPlugin.GetUserPermissions(userID)
	roles := p.permPlugin.GetUserRoles(userID)

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
func (p *Plugin) handlePermRole(ctx *eventctx.Context) error {
	if p.permPlugin == nil {
		return p.reply(ctx, "权限插件未初始化")
	}

	// 检查权限
	if !p.checkPermission(ctx, "perm.role") {
		return p.reply(ctx, "❌ 权限不足")
	}

	content := ctx.GetMessageContent()
	args, _ := command.ParseCommandLine(content)

	userID := args.Get(0)
	roleName := args.Get(1)

	if userID == "" || roleName == "" {
		return p.reply(ctx, "用法: /perm role <用户ID> <角色>")
	}

	err := p.permPlugin.AssignRole(userID, roleName)
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

	if p.pluginManager != nil {
		plugins := p.pluginManager.List()
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
	if p.permPlugin == nil {
		return true // 如果没有权限插件，默认允许
	}

	userID := ctx.GetUserID()
	return p.permPlugin.HasPermission(userID, perm)
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

// Dependencies 返回依赖列表
func (p *Plugin) Dependencies() []string {
	return []string{"permission"}
}
