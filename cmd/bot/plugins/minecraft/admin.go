// Package minecraft admin.go — /mcadmin：通过 RCON 管理 Minecraft 服务器（第一版）。
//
// 设计约束（有意为之，改动前请先读完）：
//
//  1. 服务器注册表只来自配置文件（plugins.minecraft.admin.servers），密码支持
//     ${ENV} 展开。第一版不开放聊天自助注册：RCON 等于服务器控制台，
//     凭据必须由运维掌控；用户自助注册留待后续版本，且需要配套的加密存储。
//  2. 命令走白名单封装（list/status/say/whitelist/kick/save），
//     不提供原生控制台透传。透传一旦开放就等于给出无审计边界的任意命令执行。
//  3. 审计以"一次用户操作"为粒度写入（辅助探测命令不单独入账），
//     执行内容进审计，密码永不进审计、日志或回显。
//  4. scope 把服务器绑定到具体会话：绑定后该会话的群主/管理员（需
//     allow_group_admins）可操作；未绑定（全局）的服务器只认 RBAC 权限点。
package minecraft

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/permission/permcheck"
	"github.com/KomeiDiSanXian/remilia/command"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// ────────────────────────────────────────────────────────────────────────────
// 配置
// ────────────────────────────────────────────────────────────────────────────

// AdminServer 一台可管理的服务器。由运维在配置文件中定义。
type AdminServer struct {
	// Name 服务器名（命令中引用；需唯一）。
	Name string
	// Address 游戏地址（主机[:端口]），用于展示。
	Address string
	// RCONAddr RCON 连接地址 host:port。
	RCONAddr string
	// Password RCON 密码。配置中可写 ${ENV_VAR}，由框架的 os.ExpandEnv 展开。
	Password string
	// Scope 绑定的会话。空 = 全局（仅 RBAC 权限点可用）；
	// 否则只有该会话可见可用，且该会话的群主/管理员（allow_group_admins）可操作。
	// 可写完整 scope（"qq:123456"）或裸会话 ID（"123456"）。
	Scope string
}

// AdminConfig 服务器管理配置（plugins.minecraft.admin 节）。
type AdminConfig struct {
	// Permission 操作服务器所需的 RBAC 权限点。
	Permission string
	// Servers 服务器注册表。
	Servers []AdminServer
	// Timeout 单条 RCON 命令的连接与读写超时。
	Timeout time.Duration
	// MaxOutput 单条命令回显的最大字符数（超出截断）。
	MaxOutput int
	// Enabled 是否启用 /mcadmin。默认关闭：开启即等于把服务器控制台
	// 的一部分能力交给聊天里的使用者，必须由运维显式打开。
	Enabled bool
	// AllowGroupAdmins 是否允许"已绑定会话"的群主/群管理员操作该会话的服务器。
	// 仅对 scope 绑定到当前会话的服务器生效；全局服务器始终需要 RBAC 权限点。
	AllowGroupAdmins bool
}

// DefaultAdminConfig 服务器管理的默认配置：默认关闭。
var DefaultAdminConfig = AdminConfig{
	Enabled:          false,
	Permission:       "minecraft.admin",
	AllowGroupAdmins: true,
	Timeout:          5 * time.Second,
	MaxOutput:        1200,
}

// confirmTTL 高危操作二次确认的有效期。
const confirmTTL = 60 * time.Second

// loadAdminConfig 读取 admin 配置节；未配置或类型不符时返回默认值。
func loadAdminConfig(ctx *plugin.SetupContext) AdminConfig {
	if ctx.Config == nil {
		return DefaultAdminConfig
	}
	raw, ok := ctx.Config.Get("admin").(map[string]any)
	if !ok {
		return DefaultAdminConfig
	}
	return adminConfigFromMap(raw, ctx.Log)
}

// adminConfigFromMap 从 admin 配置节的映射解析配置。
// 与 SetupContext 解耦，便于直接测试解析与校验逻辑。
func adminConfigFromMap(raw map[string]any, log plugin.Logger) AdminConfig {
	cfg := DefaultAdminConfig

	cfg.Enabled = mapBool(raw, "enabled", cfg.Enabled)
	if v := strings.TrimSpace(mapString(raw, "permission", "")); v != "" {
		cfg.Permission = v
	}
	cfg.AllowGroupAdmins = mapBool(raw, "allow_group_admins", cfg.AllowGroupAdmins)
	if v := mapDuration(raw, "timeout", 0); v > 0 {
		cfg.Timeout = v
	}
	if v := mapInt(raw, "max_output", 0); v > 0 {
		cfg.MaxOutput = v
	}
	cfg.Servers = parseAdminServers(raw["servers"], log)
	return cfg
}

// parseAdminServers 解析服务器列表。
//
// 单条配置有问题时只跳过该条并告警，绝不让整个插件加载失败——
// 查询功能（/mc）必须不受管理配置错误影响。
func parseAdminServers(raw any, log plugin.Logger) []AdminServer {
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	seen := make(map[string]bool, len(list))
	out := make([]AdminServer, 0, len(list))

	for i, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			adminWarnf(log, "plugins.minecraft.admin.servers[%d] 不是映射，已跳过", i)
			continue
		}
		name := strings.TrimSpace(mapString(m, "name", ""))
		if name == "" {
			adminWarnf(log, "plugins.minecraft.admin.servers[%d] 缺少 name，已跳过", i)
			continue
		}
		if seen[name] {
			adminWarnf(log, "plugins.minecraft.admin.servers: 名称 %q 重复，已跳过后一条", name)
			continue
		}
		password := mapString(m, "password", "")
		if password == "" {
			adminWarnf(log, "plugins.minecraft.admin.servers[%q]: 缺少 password，已跳过"+
				"（可用 ${ENV_VAR} 引用环境变量）", name)
			continue
		}
		addr := strings.TrimSpace(mapString(m, "address", ""))
		host, _, err := parseHostPort(addr)
		if addr == "" || err != nil {
			adminWarnf(log, "plugins.minecraft.admin.servers[%q]: address 无效（应为 主机[:端口]），已跳过", name)
			continue
		}

		// RCON 端点默认与游戏地址同主机、端口取默认值，可单独覆盖
		rconHost := strings.TrimSpace(mapString(m, "rcon_host", ""))
		if rconHost == "" {
			rconHost = host
		}
		rconPort := mapInt(m, "rcon_port", 0)
		if rconPort <= 0 {
			rconPort = DefaultRCONPort
		}

		seen[name] = true
		out = append(out, AdminServer{
			Name:     name,
			Address:  addr,
			RCONAddr: fmt.Sprintf("%s:%d", rconHost, rconPort),
			Password: password,
			Scope:    strings.TrimSpace(mapString(m, "scope", "")),
		})
	}
	return out
}

func adminWarnf(log plugin.Logger, format string, args ...any) {
	if log != nil {
		log.Warnf("[minecraft] "+format, args...)
	}
}

// 以下 mapXxx 辅助函数处理 YAML 解码后的 map[string]any：
// 未使用 mapstructure 等解码库，避免为几个字段引入依赖。
func mapString(m map[string]any, key, def string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return def
}

func mapBool(m map[string]any, key string, def bool) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return def
}

func mapInt(m map[string]any, key string, def int) int {
	switch v := m[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return def
	}
}

func mapDuration(m map[string]any, key string, def time.Duration) time.Duration {
	switch v := m[key].(type) {
	case string:
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	case int:
		return time.Duration(v)
	case int64:
		return time.Duration(v)
	case float64:
		return time.Duration(v)
	}
	return def
}

// ────────────────────────────────────────────────────────────────────────────
// 会话可见性与权限
// ────────────────────────────────────────────────────────────────────────────

// serverBound 服务器是否绑定到当前会话。
func serverBound(srv *AdminServer, ctx *eventctx.Context) bool {
	if srv == nil || srv.Scope == "" {
		return false
	}
	return srv.Scope == favScope(ctx) || srv.Scope == ctx.GetChatInfo().ID
}

// serverVisible 服务器是否对当前会话可见。
// 全局服务器对所有会话可见；绑定服务器只对目标会话可见。
func serverVisible(srv *AdminServer, ctx *eventctx.Context) bool {
	if srv == nil {
		return false
	}
	return srv.Scope == "" || serverBound(srv, ctx)
}

// hasAdminPerm 是否持有 RBAC 权限点（或 superadmin 角色）。
func (p *mcPlugin) hasAdminPerm(ctx *eventctx.Context) bool {
	if p.permSvc == nil {
		return false
	}
	if permcheck.HasPermission(p.permSvc, ctx, p.adminCfg.Permission) {
		return true
	}
	return slices.Contains(p.permSvc.GetUserRoles(ctx.GetUserID()), "superadmin")
}

// isGroupAdmin 使用者的平台群角色是否达到管理员。
//
// 注意平台差异：QQ 群消息的 payload return slices.Contains(p.permSvc.GetUserRoles(ctx.GetUserID()), "superadmin")ord 等平台可正常判定。
func isGroupAdmin(ctx *eventctx.Context) bool {
	return ctx.GetChatInfo().IsGroup && ctx.GetSenderInfo().GroupRole >= platform.GroupRoleAdmin
}

// adminBaseAllowed 是否具备 /mcadmin 的基础使用资格（不针对具体服务器）。
func (p *mcPlugin) adminBaseAllowed(ctx *eventctx.Context) bool {
	if p.hasAdminPerm(ctx) {
		return true
	}
	return p.adminCfg.AllowGroupAdmins && isGroupAdmin(ctx)
}

// adminDenyReason 返回禁止操作的原因；空串表示允许。
//
// 权限服务缺失时按"拒绝"处理（与 permcheck 的 fail-open 约定不同）：
// 该功能能踢人、能广播、能改白名单，缺权限服务就放行等于向所有群成员
// 开放服务器控制台，这里宁可不可用也不要不可控。
func (p *mcPlugin) adminDenyReason(ctx *eventctx.Context, srv *AdminServer) string {
	if p.hasAdminPerm(ctx) {
		return ""
	}
	// 群主/管理员通道只对"显式绑定到当前会话"的服务器生效，
	// 否则全局服务器会被任意群的群主接管。
	if p.adminCfg.AllowGroupAdmins && serverBound(srv, ctx) && isGroupAdmin(ctx) {
		return ""
	}
	if p.permSvc == nil {
		return "权限服务不可用：请加载 permission 插件，或为使用者授予 " + p.adminCfg.Permission + " 权限"
	}
	return "权限不足：需要 " + p.adminCfg.Permission + " 权限或 superadmin 角色"
}

// findServer 按名称或序号（1 起始）在可见范围内查找服务器。
// 失败时返回 nil 与可直接回复的原因。
func (p *mcPlugin) findServer(ctx *eventctx.Context, input string) (*AdminServer, string) {
	visible := make([]*AdminServer, 0, len(p.adminCfg.Servers))
	for i := range p.adminCfg.Servers {
		if srv := &p.adminCfg.Servers[i]; serverVisible(srv, ctx) {
			visible = append(visible, srv)
		}
	}
	if len(visible) == 0 {
		return nil, "当前会话没有可管理的服务器（配置：plugins.minecraft.admin.servers）"
	}

	var found *AdminServer
	for _, srv := range visible {
		if srv.Name == input {
			found = srv
			break
		}
	}
	if found == nil {
		// 大小写不敏感兜底，方便聊天里随手输入
		for _, srv := range visible {
			if strings.EqualFold(srv.Name, input) {
				found = srv
				break
			}
		}
	}
	if found == nil {
		if n, err := parseIndex(input); err == nil && n <= len(visible) {
			found = visible[n-1]
		}
	}
	if found == nil {
		names := make([]string, 0, len(visible))
		for _, srv := range visible {
			names = append(names, srv.Name)
		}
		return nil, fmt.Sprintf("未找到服务器 %q；可用：%s", input, strings.Join(names, "、"))
	}
	if reason := p.adminDenyReason(ctx, found); reason != "" {
		return nil, reason
	}
	return found, ""
}

// parseIndex 解析 1 起始的序号。
func parseIndex(s string) (int, error) {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, errors.New("not a number")
		}
		n = n*10 + int(r-'0')
	}
	if n < 1 {
		return 0, errors.New("out of range")
	}
	return n, nil
}

// ────────────────────────────────────────────────────────────────────────────
// 命令注册与分发
// ────────────────────────────────────────────────────────────────────────────

const mcAdminUsage = `用法：
  /mcadmin list                                — 列出可管理的服务器
  /mcadmin status <服务器>                      — 玩家列表与 TPS（Paper）
  /mcadmin say <服务器> <文本>                  — 以控制台身份广播
  /mcadmin whitelist <服务器> on|off|list       — 白名单开关与列表
  /mcadmin whitelist <服务器> add|remove <玩家>
  /mcadmin kick <服务器> <玩家> [原因]           — 踢出玩家
  /mcadmin save <服务器>                        — 立即保存世界
  /mcadmin confirm <验证码>                     — 确认高危操作

服务器由运维在 plugins.minecraft.admin.servers 配置（密码支持 ${ENV}）。`

// registerAdminCommands 注册 /mcadmin（别名 /mca）。
//
// 与 /mc 分开注册：/mc 的第一个位置参数会被收藏名/序号占用，
// 混入子命令会与名为 admin、list 的收藏冲突。
func (p *mcPlugin) registerAdminCommands(ctx *plugin.SetupContext) {
	def := command.NewDef("mcadmin").
		Alias("mca").
		Description("Minecraft 服务器管理（RCON）").
		Arg("action", "list | status | say | whitelist | kick | save | confirm", true).
		Arg("args", "子命令参数（服务器名、文本、玩家名等）", false).
		Example("/mcadmin list").
		Example("/mcadmin status 生存服").
		Example("/mcadmin say 生存服 服务器将在 10 分钟后维护").
		Example("/mcadmin whitelist 生存服 add Steve").
		Example("/mcadmin kick 生存服 Steve 使用作弊客户端").
		Example("/mcadmin save 生存服").
		Build()
	ctx.OnCommandDefWith("", "/mcadmin", def, p.handleMCAdmin, eventctx.OnMentionedBotOrNoMentions())
}

// handleMCAdmin 处理 /mcadmin 命令。
func (p *mcPlugin) handleMCAdmin(ctx *eventctx.Context) error {
	if !p.adminCfg.Enabled {
		ctx.ReplyText("服务器管理未启用。开启方式：配置 plugins.minecraft.admin.enabled: true，" +
			"并在 admin.servers 中登记服务器")
		return nil
	}
	if p.rcon == nil {
		ctx.ReplyError("RCON 连接池未初始化")
		return nil
	}

	parsed, err := eventctx.ParseCommand(ctx)
	if err != nil || len(parsed.Positional) == 0 {
		ctx.ReplyText(mcAdminUsage)
		return nil
	}
	args := parsed.Positional
	switch strings.ToLower(args[0]) {
	case "help", "?":
		ctx.ReplyText(mcAdminUsage)
	case "list", "ls":
		p.adminList(ctx)
	case "status", "info":
		p.adminStatus(ctx, args[1:])
	case "say", "broadcast":
		p.adminSay(ctx, args[1:])
	case "whitelist", "wl":
		p.adminWhitelist(ctx, args[1:])
	case "kick":
		p.adminKick(ctx, args[1:])
	case "save", "save-all":
		p.adminSave(ctx, args[1:])
	case "confirm":
		p.adminConfirm(ctx, args[1:])
	default:
		ctx.ReplyError("未知子命令。\n" + mcAdminUsage)
	}
	return nil
}

// adminList 列出当前会话可见且可操作的服务器。
func (p *mcPlugin) adminList(ctx *eventctx.Context) {
	if !p.adminBaseAllowed(ctx) {
		ctx.ReplyError(p.adminDenyReason(ctx, nil))
		return
	}

	perm := p.hasAdminPerm(ctx)
	var rows []string
	for i := range p.adminCfg.Servers {
		srv := &p.adminCfg.Servers[i]
		if !serverVisible(srv, ctx) {
			continue
		}
		// 绑定服务器对本会话群主开放；全局服务器只对权限持有者展示
		if !perm && !serverBound(srv, ctx) {
			continue
		}
		scope := "全局"
		if srv.Scope != "" {
			scope = "本会话"
		}
		rows = append(rows, fmt.Sprintf("  %d. %s — %s（RCON %s）[%s]",
			len(rows)+1, srv.Name, srv.Address, srv.RCONAddr, scope))
	}
	if len(rows) == 0 {
		ctx.ReplyText("当前会话没有可管理的服务器")
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "🎛 可管理的服务器（%d）\n", len(rows))
	b.WriteString(strings.Join(rows, "\n"))
	b.WriteString("\n\n查看状态：/mcadmin status <服务器>")
	ctx.ReplyText(b.String())
}

// adminStatus 展示玩家列表与 TPS（TPS 仅 Paper 系支持，失败不报错）。
func (p *mcPlugin) adminStatus(ctx *eventctx.Context, args []string) {
	if len(args) == 0 {
		ctx.ReplyError("用法: /mcadmin status <服务器>")
		return
	}
	srv, deny := p.findServer(ctx, args[0])
	if srv == nil {
		ctx.ReplyError(deny)
		return
	}

	players, perr := p.runRCON(srv, "list")
	// TPS 是 Paper/Purpur 等衍生端的命令，原版会返回"未知命令"；
	// 这里只作为加分项，失败即忽略，也不计入审计（属于辅助探测）。
	tps, terr := p.runRCON(srv, "tps")

	var b strings.Builder
	fmt.Fprintf(&b, "🎛 %s（%s）\n", srv.Name, srv.Address)
	if perr != nil {
		fmt.Fprintf(&b, "在线玩家：获取失败 — %s\n", rconErrText(perr))
	} else {
		fmt.Fprintf(&b, "在线玩家：%s\n", p.formatConsole(players))
	}
	if terr == nil && isTPSOutput(tps) {
		fmt.Fprintf(&b, "TPS：%s\n", p.formatConsole(tps))
	}
	ctx.ReplyText(strings.TrimRight(b.String(), "\n"))

	if perr != nil {
		p.recordAdmin(ctx, srv, "status", "list", perr)
		return
	}
	p.recordAdmin(ctx, srv, "status", "list", nil)
}

// adminSay 以控制台身份广播消息。
func (p *mcPlugin) adminSay(ctx *eventctx.Context, args []string) {
	if len(args) < 2 {
		ctx.ReplyError("用法: /mcadmin say <服务器> <文本>")
		return
	}
	srv, deny := p.findServer(ctx, args[0])
	if srv == nil {
		ctx.ReplyError(deny)
		return
	}
	text := sanitizeConsoleArg(strings.Join(args[1:], " "))
	if text == "" {
		ctx.ReplyError("广播内容不能为空")
		return
	}
	text = truncateRunes(text, 256)

	if _, err := p.runRCON(srv, "say "+text); err != nil {
		p.recordAdmin(ctx, srv, "say", "say", err)
		ctx.ReplyError(fmt.Sprintf("广播失败：%s", rconErrText(err)))
		return
	}
	p.recordAdmin(ctx, srv, "say", "say", nil)
	ctx.ReplySuccess(fmt.Sprintf("已广播到 %s：%s", srv.Name, text))
}

// adminWhitelist 管理白名单。off 属于高危操作，需二次确认。
func (p *mcPlugin) adminWhitelist(ctx *eventctx.Context, args []string) {
	if len(args) < 2 {
		ctx.ReplyError("用法: /mcadmin whitelist <服务器> on|off|list|add|remove [玩家]")
		return
	}
	srv, deny := p.findServer(ctx, args[0])
	if srv == nil {
		ctx.ReplyError(deny)
		return
	}
	action := strings.ToLower(args[1])

	switch action {
	case "on", "enable":
		p.runWhitelistToggle(ctx, srv, "whitelist on", "已开启白名单")
	case "list":
		out, err := p.runRCON(srv, "whitelist list")
		if err != nil {
			p.recordAdmin(ctx, srv, "whitelist", "whitelist list", err)
			ctx.ReplyError("获取白名单失败：" + rconErrText(err))
			return
		}
		p.recordAdmin(ctx, srv, "whitelist", "whitelist list", nil)
		ctx.ReplyText(fmt.Sprintf("📋 %s 白名单：\n%s", srv.Name, p.formatConsole(out)))
	case "off", "disable":
		// 关闭白名单等于对所有人开门，必须二次确认
		p.askConfirm(ctx, func(c *eventctx.Context) {
			p.runWhitelistToggle(c, srv, "whitelist off", "已关闭白名单（任何玩家都可进入）")
		}, fmt.Sprintf("⚠️ 关闭 %s 的白名单会让任何玩家都能进入该服务器。", srv.Name))
	case "add", "remove", "rm":
		if len(args) < 3 {
			ctx.ReplyError(fmt.Sprintf("用法: /mcadmin whitelist <服务器> %s <玩家>", action))
			return
		}
		player := sanitizeConsoleArg(args[2])
		if !validPlayerName(player) {
			ctx.ReplyError("玩家名无效（1~32 字符，不含空白）")
			return
		}
		verb := "add"
		if action != "add" {
			verb = "remove"
		}
		cmd := fmt.Sprintf("whitelist %s %s", verb, player)
		if _, err := p.runRCON(srv, cmd); err != nil {
			p.recordAdmin(ctx, srv, "whitelist", cmd, err)
			ctx.ReplyError(fmt.Sprintf("操作失败：%s", rconErrText(err)))
			return
		}
		p.recordAdmin(ctx, srv, "whitelist", cmd, nil)
		if verb == "add" {
			ctx.ReplySuccess(fmt.Sprintf("已把 %s 加入 %s 的白名单", player, srv.Name))
		} else {
			ctx.ReplySuccess(fmt.Sprintf("已把 %s 移出 %s 的白名单", player, srv.Name))
		}
	default:
		ctx.ReplyError("用法: /mcadmin whitelist <服务器> on|off|list|add|remove [玩家]")
	}
}

// runWhitelistToggle 执行白名单开关并回复。
func (p *mcPlugin) runWhitelistToggle(ctx *eventctx.Context, srv *AdminServer, cmd, okMsg string) {
	if _, err := p.runRCON(srv, cmd); err != nil {
		p.recordAdmin(ctx, srv, "whitelist", cmd, err)
		ctx.ReplyError(fmt.Sprintf("操作失败：%s", rconErrText(err)))
		return
	}
	p.recordAdmin(ctx, srv, "whitelist", cmd, nil)
	ctx.ReplySuccess(okMsg)
}

// adminKick 踢出玩家。
func (p *mcPlugin) adminKick(ctx *eventctx.Context, args []string) {
	if len(args) < 2 {
		ctx.ReplyError("用法: /mcadmin kick <服务器> <玩家> [原因]")
		return
	}
	srv, deny := p.findServer(ctx, args[0])
	if srv == nil {
		ctx.ReplyError(deny)
		return
	}
	player := sanitizeConsoleArg(args[1])
	if !validPlayerName(player) {
		ctx.ReplyError("玩家名无效（1~32 字符，不含空白）")
		return
	}
	cmd := "kick " + player
	if len(args) > 2 {
		if reason := sanitizeConsoleArg(strings.Join(args[2:], " ")); reason != "" {
			cmd += " " + truncateRunes(reason, 128)
		}
	}
	if _, err := p.runRCON(srv, cmd); err != nil {
		p.recordAdmin(ctx, srv, "kick", cmd, err)
		ctx.ReplyError(fmt.Sprintf("踢出失败：%s", rconErrText(err)))
		return
	}
	p.recordAdmin(ctx, srv, "kick", cmd, nil)
	ctx.ReplySuccess(fmt.Sprintf("已把 %s 踢出 %s", player, srv.Name))
}

// adminSave 立即保存世界。
func (p *mcPlugin) adminSave(ctx *eventctx.Context, args []string) {
	if len(args) == 0 {
		ctx.ReplyError("用法: /mcadmin save <服务器>")
		return
	}
	srv, deny := p.findServer(ctx, args[0])
	if srv == nil {
		ctx.ReplyError(deny)
		return
	}
	if _, err := p.runRCON(srv, "save-all"); err != nil {
		p.recordAdmin(ctx, srv, "save", "save-all", err)
		ctx.ReplyError(fmt.Sprintf("保存失败：%s", rconErrText(err)))
		return
	}
	p.recordAdmin(ctx, srv, "save", "save-all", nil)
	ctx.ReplySuccess(fmt.Sprintf("已触发 %s 保存世界", srv.Name))
}

// adminConfirm 兑换一次待确认的高危操作。
func (p *mcPlugin) adminConfirm(ctx *eventctx.Context, args []string) {
	if len(args) == 0 {
		ctx.ReplyError("用法: /mcadmin confirm <验证码>")
		return
	}
	if p.confirm == nil {
		ctx.ReplyError("没有待确认的操作")
		return
	}
	run, ok := p.confirm.take(confirmKey(ctx), args[0], time.Now())
	if !ok {
		ctx.ReplyError("验证码无效或已过期（请重新发起操作）")
		return
	}
	run(ctx)
}

// ────────────────────────────────────────────────────────────────────────────
// 执行、格式化与审计
// ────────────────────────────────────────────────────────────────────────────

// runRCON 执行一条 RCON 命令并记录指标。
//
// 这里刻意不写审计：审计以"一次用户操作"为粒度由各处理器写入，
// 否则 status 这类会附加探测命令（如 tps）的操作会污染审计流。
func (p *mcPlugin) runRCON(srv *AdminServer, cmd string) (string, error) {
	if p.rcon == nil {
		return "", errors.New("RCON 连接池未初始化")
	}
	out, err := p.rcon.Do(srv.Name, srv.RCONAddr, srv.Password, p.adminCfg.Timeout, cmd)

	result := "ok"
	switch {
	case err == nil:
	case errors.Is(err, ErrRconAuth):
		result = "auth_failed"
	case errors.Is(err, ErrRconUnreachable):
		result = "unreachable"
	default:
		result = "error"
	}
	mcAdminTotal.WithLabelValues(metricCommand(cmd), result).Inc()

	if p.log != nil {
		if err != nil {
			p.log.Warnf("[minecraft] rcon %s %q failed: %v", srv.Name, cmd, err)
		} else {
			p.log.Infof("[minecraft] rcon %s %q ok", srv.Name, cmd)
		}
	}
	return out, err
}

// recordAdmin 写入审计日志。
//
// 写入内容只有服务器名、命令与结果；密码既不参与也不出现在任何字段中。
func (p *mcPlugin) recordAdmin(ctx *eventctx.Context, srv *AdminServer, action, cmd string, err error) {
	if p.audit == nil {
		return
	}
	meta := map[string]any{
		"server":  srv.Name,
		"action":  action,
		"command": cmd,
		"scope":   srv.Scope,
		"ok":      err == nil,
	}
	if err != nil {
		meta["error"] = err.Error()
	}
	p.audit.Record(ctx, "minecraft.rcon", meta)
}

// formatConsole 把控制台输出整理成适合聊天展示的文本。
func (p *mcPlugin) formatConsole(out string) string {
	out = strings.TrimSpace(stripMotd(out))
	if out == "" {
		return "（无输出）"
	}
	return truncateRunes(out, p.adminCfg.MaxOutput)
}

// rconErrText 把 RCON 错误转成可读提示。
func rconErrText(err error) string {
	switch {
	case errors.Is(err, ErrRconAuth):
		return "RCON 密码错误（请检查 plugins.minecraft.admin.servers 中的 password）"
	case errors.Is(err, ErrRconUnreachable):
		return "无法连接 RCON 端口（请确认服务端已开启 rcon、端口与地址正确）"
	default:
		return err.Error()
	}
}

// isTPSOutput 判断 tps 命令的输出是否为有效 TPS 数据
// （原版服务端会把未知命令的报错文本返回，需要过滤掉）。
func isTPSOutput(out string) bool {
	low := strings.ToLower(out)
	return strings.Contains(low, "tps") && !strings.Contains(low, "unknown") && !strings.Contains(low, "未知")
}

// sanitizeConsoleArg 清理用户输入中的控制字符。
//
// 换行会终止当前命令并被服务端当作新命令解析，
// 因此必须在拼接前清除（例如把"玩家名\nop Evil"注入成两条命令）。
func sanitizeConsoleArg(s string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', 0:
			return ' '
		}
		return r
	}, s))
}

// validPlayerName 校验玩家名：1~32 字符且不含空白。
//
// 只做必要的注入防护与长度约束，不强制 Java 版命名规则——
// 部分服务端插件允许更宽的名字，过严会挡住正常使用。
func validPlayerName(s string) bool {
	if s == "" || len([]rune(s)) > 32 {
		return false
	}
	return !strings.ContainsAny(s, " \t")
}

// metricCommand 取命令的首个词作为指标标签，避免标签基数爆炸。
func metricCommand(cmd string) string {
	if i := strings.IndexAny(cmd, " \t"); i > 0 {
		return cmd[:i]
	}
	return cmd
}

// confirmKey 二次确认的作用域键：会话 + 用户。
// 绑定到发起人，其他人拿到验证码也无法代为确认。
func confirmKey(ctx *eventctx.Context) string {
	return favScope(ctx) + "|" + ctx.GetSenderInfo().ID
}

// ────────────────────────────────────────────────────────────────────────────
// 二次确认
// ────────────────────────────────────────────────────────────────────────────

// confirmStore 存放待确认的高危操作。
//
// 相比在命令里带 --yes，独立验证码更稳妥：命令解析器会把 --key 后面的
// token 吞成该 flag 的值（如 "--yes Steve" 会吃掉玩家名），
// 而验证码流程与解析器完全解耦，并且天然带有效期与一次性语义。
type confirmStore struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]confirmEntry
}

type confirmEntry struct {
	code    string
	expires time.Time
	run     func(*eventctx.Context)
}

// newConfirmStore 创建确认存储。ttl <= 0 时使用默认值。
func newConfirmStore(ttl time.Duration) *confirmStore {
	if ttl <= 0 {
		ttl = confirmTTL
	}
	return &confirmStore{ttl: ttl, entries: make(map[string]confirmEntry)}
}

// put 登记一个待确认操作，返回验证码。同一 key 同时只保留最新一条。
func (s *confirmStore) put(key string, run func(*eventctx.Context), now time.Time) string {
	code := newConfirmCode()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)
	s.entries[key] = confirmEntry{code: code, expires: now.Add(s.ttl), run: run}
	return code
}

// take 校验并消费一条待确认操作。
func (s *confirmStore) take(key, code string, now time.Time) (func(*eventctx.Context), bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)

	entry, ok := s.entries[key]
	if !ok || entry.code != code {
		return nil, false
	}
	delete(s.entries, key)
	return entry.run, true
}

// pruneLocked 清理过期条目。调用方须持有 s.mu。
func (s *confirmStore) pruneLocked(now time.Time) {
	for k, e := range s.entries {
		if now.After(e.expires) {
			delete(s.entries, k)
		}
	}
}

// askConfirm 回复一条待确认提示。
func (p *mcPlugin) askConfirm(ctx *eventctx.Context, run func(*eventctx.Context), warn string) {
	if p.confirm == nil {
		ctx.ReplyError("确认机制不可用，操作已取消")
		return
	}
	code := p.confirm.put(confirmKey(ctx), run, time.Now())
	ctx.ReplyText(fmt.Sprintf("%s\n\n确认请在 %s 内发送：/mcadmin confirm %s\n（验证码与发起人、本会话绑定，过期后需重新操作）",
		warn, humanizeWait(confirmTTL), code))
}

// newConfirmCode 生成 6 位数字验证码。
//
// 用 crypto/rand 而非 math/rand：验证码本身已绑定发起人，
// 但仍然不希望可预测（例如被同会话的其他人在窗口期内撞中）。
func newConfirmCode() string {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return fmt.Sprintf("%06d", time.Now().UnixNano()%1_000_000)
	}
	return fmt.Sprintf("%06d", n.Int64())
}
