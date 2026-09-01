// Package qqpanel 提供 QQ 官方指令面板与自定义菜单管理插件。
//
// 基于 QQ 开放平台 /v2/panels 与 /v2/menu 接口，可自动从引擎命令表中
// 提取命令构建指令面板 / 自定义菜单，供群聊与 C2C 场景使用。
//
// 指令面板（panel）支持 c2c / group / channel / dm 四种场景，一个机器人
// 最多 20 个面板、每个面板最多 20 个元素；自定义菜单（menu）仅 C2C 场景
// 生效，最多 10 个顶级项、每个折叠菜单最多 5 个子项。
package qqpanel

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/core/permission"
	"github.com/KomeiDiSanXian/remilia/builtin/permission/permcheck"
	"github.com/KomeiDiSanXian/remilia/command"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

const (
	// permManage 是管理指令面板与自定义菜单所需的权限点。
	permManage = "qqpanel.manage"

	// maxPanelItems 是单个指令面板的最大元素数（平台限制）。
	maxPanelItems = 20
	// maxMenuItems 是自定义菜单的最大顶级项数（平台限制）。
	maxMenuItems = 10
	// maxSubMenuItems 是折叠菜单的最大子项数（平台限制）。
	maxSubMenuItems = 5
)

// defaultExcludes 默认不进入面板/菜单的命令。
//
// 这些命令的权限校验在 handler 内部完成（admin 角色或 xxx.manage 权限点），
// 元数据中并未声明 Permissions。把它们构建进用户可见的面板/菜单意义不大，
// 反而会让普通用户点击后收到"权限不足"的提示；因此默认排除，可通过
// plugins.qqpanel.exclude 配置追加更多（默认清单始终生效）。
var defaultExcludes = []string{
	"/plugin", "/perm", "/code", "/acl", "/status", "/info",
	"/mute", "/kick", "/warn", "/warnings", "/clean",
	"/rl", "/welcome", "/farewell", "/ar", "/cc", "/debug", "/update",
	"/qqpanel", "/qqmenu",
}

// Plugin QQ 指令面板与自定义菜单管理插件。
type Plugin struct {
	info    plugin.Info
	permSvc *permission.Plugin
	exclude []string
}

// New 创建插件描述符。
func New() *plugin.Descriptor {
	p := &Plugin{}
	return &plugin.Descriptor{
		Name:         "qqpanel",
		Version:      "1.0.0",
		OptionalDeps: []string{"permission"},
		Meta: &plugin.Metadata{
			Author:      "Remilia Team",
			Description: "管理 QQ 官方指令面板与自定义菜单",
			Category:    "管理",
			Tags:        []string{"QQ", "面板", "菜单"},
			HelpText: `QQ 指令面板 / 自定义菜单管理（仅 QQ 平台生效）：
  /qqpanel setup [scope]  — 自动从命令表提取前 20 条命令创建指令面板（默认 group）
  /qqpanel list [scope]   — 列出当前场景的指令面板（默认 group）
  /qqpanel rm <panel_id>  — 删除指定指令面板
  /qqmenu synchelp        — 根据命令表自动构建 C2C 自定义菜单
  /qqmenu status          — 查询当前自定义菜单

需要 qqpanel.manage 权限。scope 取值: c2c / group / channel / dm。
自动构建会排除隐藏命令、自身命令、声明了 Permissions 的命令以及默认的管理类
命令（可通过 plugins.qqpanel.exclude 配置追加排除项）。
/help 固定置顶，保证新用户可发现（除非被显式排除）。`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			p.info = ctx.Info
			if svc, ok := ctx.TryService[*permission.Plugin]("permission"); ok {
				p.permSvc = svc
			}
			if ctx.Config != nil {
				p.exclude = ctx.Config.GetStringSlice("exclude", nil)
			}

			panelDef := command.NewDef("qqpanel").
				Description("管理 QQ 指令面板").
				Category("管理").
				SubCommand(command.NewDef("setup").Description("自动创建指令面板").Arg("scope", "生效场景(c2c/group/channel/dm)", false).Build()).
				SubCommand(command.NewDef("list").Description("列出指令面板").Arg("scope", "生效场景", false).Build()).
				SubCommand(command.NewDef("rm").Description("删除指令面板").Arg("panel_id", "面板 ID", true).Build()).
				Build()
			ctx.OnCommandDefWith("", "/qqpanel", panelDef, p.handlePanel, eventctx.OnMentionedBotOrNoMentions())

			menuDef := command.NewDef("qqmenu").
				Description("管理 QQ 自定义菜单").
				Category("管理").
				SubCommand(command.NewDef("synchelp").Description("按命令表自动构建菜单").Build()).
				SubCommand(command.NewDef("status").Description("查询当前菜单").Build()).
				Build()
			ctx.OnCommandDefWith("", "/qqmenu", menuDef, p.handleMenu, eventctx.OnMentionedBotOrNoMentions())
			return p, nil
		},
	}
}

// handlePanel 处理 /qqpanel 命令。
func (p *Plugin) handlePanel(ctx *eventctx.Context) error {
	if ctx.GetEventPlatform() != "qq" {
		ctx.ReplyText("该命令仅支持 QQ 平台")
		return nil
	}
	args := strings.Fields(ctx.GetMessageContent())
	if len(args) < 2 {
		ctx.ReplyText("用法: /qqpanel setup [scope] | list [scope] | rm <panel_id>")
		return nil
	}
	switch args[1] {
	case "setup":
		return p.panelSetup(ctx, args)
	case "list":
		return p.panelList(ctx, args)
	case "rm":
		return p.panelRemove(ctx, args)
	default:
		ctx.ReplyText("未知子命令，可用: setup, list, rm")
		return nil
	}
}

// handleMenu 处理 /qqmenu 命令。
func (p *Plugin) handleMenu(ctx *eventctx.Context) error {
	if ctx.GetEventPlatform() != "qq" {
		ctx.ReplyText("该命令仅支持 QQ 平台")
		return nil
	}
	args := strings.Fields(ctx.GetMessageContent())
	if len(args) < 2 {
		ctx.ReplyText("用法: /qqmenu synchelp | status")
		return nil
	}
	switch args[1] {
	case "synchelp":
		return p.menuSync(ctx)
	case "status":
		return p.menuStatus(ctx)
	default:
		ctx.ReplyText("未知子命令，可用: synchelp, status")
		return nil
	}
}

// panelSetup 自动从命令表提取命令创建指令面板。
func (p *Plugin) panelSetup(ctx *eventctx.Context, args []string) error {
	if !p.checkPermission(ctx) {
		ctx.ReplyText("权限不足：需要 qqpanel.manage 权限")
		return nil
	}
	scope := "group"
	if len(args) >= 3 && args[2] != "" {
		scope = args[2]
	}
	switch scope {
	case "c2c", "group", "channel", "dm":
	default:
		ctx.ReplyText("无效场景，可用: c2c / group / channel / dm")
		return nil
	}
	api, ok := qqAPI(ctx)
	if !ok {
		ctx.ReplyText("无法访问 QQ OpenAPI")
		return nil
	}
	items := p.buildPanelItems()
	if len(items) == 0 {
		ctx.ReplyText("命令表为空，无法构建面板")
		return nil
	}
	req := &dto.CreatePanelRequest{
		Scope:      scope,
		TargetType: "all",
		Panel: &dto.Panel{
			Items:  items,
			Remark: fmt.Sprintf("auto-generated by remilia at %s", time.Now().Format(time.RFC3339)),
		},
	}
	res, err := api.CreatePanel(context.Background(), req)
	if err != nil {
		ctx.ReplyError(fmt.Sprintf("创建面板失败: %v", err))
		return nil
	}
	panelID := res.Get("panel_id").String()
	if panelID == "" {
		ctx.ReplyError("创建面板失败：响应中缺少 panel_id")
		return nil
	}
	ctx.ReplySuccess(fmt.Sprintf("已创建指令面板 %s（scope=%s，%d 个指令）", panelID, scope, len(items)))
	return nil
}

// panelList 列出指定场景的指令面板。
func (p *Plugin) panelList(ctx *eventctx.Context, args []string) error {
	scope := "group"
	if len(args) >= 3 && args[2] != "" {
		scope = args[2]
	}
	switch scope {
	case "c2c", "group", "channel", "dm":
	default:
		ctx.ReplyText("无效场景，可用: c2c / group / channel / dm")
		return nil
	}
	api, ok := qqAPI(ctx)
	if !ok {
		ctx.ReplyText("无法访问 QQ OpenAPI")
		return nil
	}
	res, err := api.GetPanelList(context.Background(), scope, "", 50)
	if err != nil {
		ctx.ReplyError(fmt.Sprintf("查询面板失败: %v", err))
		return nil
	}
	records := res.Get("records").Array()
	if len(records) == 0 {
		ctx.ReplyText(fmt.Sprintf("scope=%s 暂无指令面板", scope))
		return nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s 场景指令面板（%d 个）:\n", scope, len(records))
	for _, r := range records {
		pid := r.Get("panel_id").String()
		target := r.Get("target_type").String()
		itemCount := len(r.Get("panel.items").Array())
		remark := r.Get("panel.remark").String()
		fmt.Fprintf(&sb, "- %s（%s，%d 项）", pid, target, itemCount)
		if remark != "" {
			fmt.Fprintf(&sb, "：%s", remark)
		}
		sb.WriteString("\n")
	}
	ctx.ReplyText(strings.TrimSpace(sb.String()))
	return nil
}

// panelRemove 删除指定指令面板。
func (p *Plugin) panelRemove(ctx *eventctx.Context, args []string) error {
	if !p.checkPermission(ctx) {
		ctx.ReplyText("权限不足：需要 qqpanel.manage 权限")
		return nil
	}
	if len(args) < 3 || args[2] == "" {
		ctx.ReplyText("用法: /qqpanel rm <panel_id>")
		return nil
	}
	api, ok := qqAPI(ctx)
	if !ok {
		ctx.ReplyText("无法访问 QQ OpenAPI")
		return nil
	}
	if _, err := api.DeletePanel(context.Background(), args[2]); err != nil {
		ctx.ReplyError(fmt.Sprintf("删除面板失败: %v", err))
		return nil
	}
	ctx.ReplySuccess(fmt.Sprintf("已删除面板 %s", args[2]))
	return nil
}

// menuSync 根据命令表自动构建并覆盖自定义菜单。
func (p *Plugin) menuSync(ctx *eventctx.Context) error {
	if !p.checkPermission(ctx) {
		ctx.ReplyText("权限不足：需要 qqpanel.manage 权限")
		return nil
	}
	api, ok := qqAPI(ctx)
	if !ok {
		ctx.ReplyText("无法访问 QQ OpenAPI")
		return nil
	}
	menu := p.buildMenu()
	if menu == nil {
		ctx.ReplyText("命令表为空，无法构建菜单")
		return nil
	}
	if _, err := api.UpdateMenu(context.Background(), &dto.UpdateMenuRequest{Menu: menu}); err != nil {
		ctx.ReplyError(fmt.Sprintf("更新菜单失败: %v", err))
		return nil
	}
	ctx.ReplySuccess(fmt.Sprintf("已同步自定义菜单（%d 个顶级项）", len(menu.Items)))
	return nil
}

// menuStatus 查询当前自定义菜单配置。
func (p *Plugin) menuStatus(ctx *eventctx.Context) error {
	api, ok := qqAPI(ctx)
	if !ok {
		ctx.ReplyText("无法访问 QQ OpenAPI")
		return nil
	}
	res, err := api.GetMenu(context.Background())
	if err != nil {
		ctx.ReplyError(fmt.Sprintf("查询菜单失败: %v", err))
		return nil
	}
	menu := res.Get("menu")
	items := menu.Get("items").Array()
	if len(items) == 0 {
		ctx.ReplyText("当前未配置自定义菜单")
		return nil
	}
	var sb strings.Builder
	version := res.Get("version").Int()
	if version == 0 {
		version = menu.Get("version").Int()
	}
	fmt.Fprintf(&sb, "当前自定义菜单（版本 %d，仅 C2C 生效）:\n", version)
	for _, it := range items {
		name := it.Get("name").String()
		typ := it.Get("type").String()
		if typ == "menu" {
			fmt.Fprintf(&sb, "- [菜单] %s（%d 个子项）\n", name, len(it.Get("sub_menu_items").Array()))
			continue
		}
		target := it.Get("send_message").String()
		if target == "" {
			target = it.Get("link").String()
		}
		fmt.Fprintf(&sb, "- [%s] %s → %s\n", typ, name, target)
	}
	ctx.ReplyText(strings.TrimSpace(sb.String()))
	return nil
}

// checkPermission 检查 qqpanel.manage 权限（permission 插件未加载时放行）。
func (p *Plugin) checkPermission(ctx *eventctx.Context) bool {
	return permcheck.HasPermission(p.permSvc, ctx, permManage)
}

// qqAPI 从事件上下文获取 QQ OpenAPI 句柄。
func qqAPI(ctx *eventctx.Context) (openapi.OpenAPI, bool) {
	api, ok := ctx.GetPlatformAPIAs[openapi.OpenAPI]()
	return api, ok && api != nil
}

// commandInfos 返回去重、按命令名排序的命令列表。
func (p *Plugin) commandInfos() []engine.CommandInfo {
	if p.info == nil || p.info.Coordinator() == nil {
		return nil
	}
	cmds := p.info.Coordinator().GetAllCommands()
	excluded := make(map[string]bool, len(defaultExcludes)+len(p.exclude))
	for _, name := range defaultExcludes {
		excluded[name] = true
	}
	for _, name := range p.exclude {
		excluded[name] = true
	}
	seen := make(map[string]bool, len(cmds))
	out := make([]engine.CommandInfo, 0, len(cmds))
	for _, c := range cmds {
		if c.Command == "" || seen[c.Command] {
			continue
		}
		// 跳过插件自身的管理命令（/qqpanel /qqmenu）
		if c.Plugin == "qqpanel" {
			continue
		}
		// 跳过在命令定义中声明了所需权限的命令
		if len(c.Permissions) > 0 {
			continue
		}
		// 跳过默认或用户配置的管理类命令
		if excluded[c.Command] {
			continue
		}
		seen[c.Command] = true
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Command < out[j].Command })
	return out
}

// buildPanelItems 从命令表构建面板元素，/help 固定保留在首位。
func (p *Plugin) buildPanelItems() []dto.PanelItem {
	cmds := p.commandInfos()
	items := make([]dto.PanelItem, 0, min(len(cmds), maxPanelItems))
	// /help 固定置顶，保证其一定出现在面板中（除非被用户显式排除）。
	if c, ok := findCommand(cmds, "/help"); ok {
		items = append(items, panelItem(c))
	}
	for _, c := range cmds {
		if len(items) >= maxPanelItems {
			break
		}
		if c.Command == "/help" {
			continue
		}
		items = append(items, panelItem(c))
	}
	return items
}

// panelItem 将命令信息转换为面板元素。
func panelItem(c engine.CommandInfo) dto.PanelItem {
	return dto.PanelItem{
		Name: truncateWeight(c.Command, 14),
		Desc: truncateWeight(c.Description, 30),
		Type: "command",
	}
}

// findCommand 在命令列表中按命令名查找。
func findCommand(cmds []engine.CommandInfo, name string) (engine.CommandInfo, bool) {
	for _, c := range cmds {
		if c.Command == name {
			return c, true
		}
	}
	return engine.CommandInfo{}, false
}

// buildMenu 从命令表构建 C2C 自定义菜单：
// 第一个顶级项为折叠菜单（/help 固定为首个子项，其余最多 4 个子项），
// 剩余指令作为顶级 send_message 项。
func (p *Plugin) buildMenu() *dto.Menu {
	cmds := p.commandInfos()
	if len(cmds) == 0 {
		return nil
	}
	menu := &dto.Menu{}
	// 折叠菜单子项：/help 固定为首项。
	subs := make([]dto.SubMenuItem, 0, maxSubMenuItems)
	if c, ok := findCommand(cmds, "/help"); ok {
		subs = append(subs, subMenuItem(c))
	}
	for _, c := range cmds {
		if len(subs) >= maxSubMenuItems {
			break
		}
		if c.Command == "/help" {
			continue
		}
		subs = append(subs, subMenuItem(c))
	}
	menu.Items = append(menu.Items, dto.MenuItem{Name: "常用指令", Type: "menu", SubMenuItems: subs})

	for _, c := range cmds {
		if len(menu.Items) >= maxMenuItems {
			break
		}
		if c.Command == "/help" {
			continue
		}
		menu.Items = append(menu.Items, dto.MenuItem{
			Name:        truncateWeight(c.Command, 10),
			Type:        "send_message",
			SendMessage: c.Command,
		})
	}
	return menu
}

// subMenuItem 将命令信息转换为菜单子项。
func subMenuItem(c engine.CommandInfo) dto.SubMenuItem {
	return dto.SubMenuItem{
		Name:        truncateWeight(c.Command, 14),
		Type:        "send_message",
		SendMessage: c.Command,
	}
}

// truncateWeight 按平台字符权重截断字符串：ASCII 计 1 个字符，其余字符
// （如中文）计 2 个字符，与 QQ 面板/菜单的字符限制规则一致。
func truncateWeight(s string, max int) string {
	if max <= 0 {
		return ""
	}
	var sb strings.Builder
	weight := 0
	for _, r := range s {
		w := 1
		if r > 127 {
			w = 2
		}
		if weight+w > max {
			break
		}
		sb.WriteRune(r)
		weight += w
	}
	return sb.String()
}
