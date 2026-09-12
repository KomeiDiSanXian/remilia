// Package minecraft 提供 Minecraft 服务器状态查询与服务器管理功能。
//
// 命令:
//   - /mc [add|rm|list] [java|bedrock] [名称|序号|主机名[:端口]] — 状态查询
//   - /mcadmin <list|status|say|whitelist|kick|save>  — RCON 服务器管理（默认关闭）
//
// AI 工具: query_minecraft_server
// AI 技能: minecraft_query
package minecraft

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/sync/singleflight"

	"github.com/KomeiDiSanXian/remilia/command"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/health"
	"github.com/KomeiDiSanXian/remilia/infra/storage"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"

	"github.com/KomeiDiSanXian/remilia/builtin/ai"
	"github.com/KomeiDiSanXian/remilia/builtin/auditlog"
	"github.com/KomeiDiSanXian/remilia/builtin/core/permission"
)

// Config minecraft 插件配置（plugins.minecraft 节）。
type Config struct {
	// DefaultServer 默认服务器（主机[:端口]）；/mc 不带参数时查询。
	DefaultServer string
	// Timeout 单次查询总超时（Java/Bedrock 直连 + API 回退共享预算）。
	Timeout time.Duration
	// CacheTTL 查询结果缓存时长（避免连查打满 mcsrvstat.us 限额）。
	CacheTTL time.Duration
	// Avatars 是否拉取在线玩家头像（mc-heads.net）。
	Avatars bool
	// DirectQuery 是否先尝试直连发包查询（Java SLP / Bedrock RakNet）。
	// 直连是裸 TCP/UDP socket，无法经过 HTTP 代理；在出站需代理或有
	// 防火墙限制的环境中仅内网可用、外网必失败——设为 false 全部改走
	// mcsrvstat.us API，避免每次外网查询先白等直连超时。
	DirectQuery bool
	// EnableQuery 是否尝试 GS4 Query 获取完整玩家列表（需服务器开 enable-query）。
	EnableQuery bool
	// GS4Mode GS4 Query 的尝试策略：
	//   auto   —— 仅当 SLP 返回的玩家样本不完整时才尝试（默认，避免无谓等待）
	//   always —— 总是尝试，0 人在线时也能拿到服务端软件/插件清单
	//   never  —— 从不尝试
	// 无论哪种策略，都要求 direct_query=true（GS4 是裸 UDP）。
	GS4Mode string
	// QueryPort GS4 Query 端口；0 = 与服务器端口一致。
	QueryPort int
	// ErrCacheTTL 查询失败（离线等）的负缓存时长；0 表示不缓存失败结果。
	ErrCacheTTL time.Duration
	// BlockPrivateTargets 是否拒绝直连私有（内网/回环）地址。
	// 默认 false：内网自建服务器正是自建 bot 的常见查询目标。
	// 仅当 /mc 对所有群成员开放、且不希望它被当作内网端口探测器时才打开。
	// 注意：API 路径始终拒绝私有地址（第三方 API 也无法路由内网）。
	BlockPrivateTargets bool
	// Cooldown /mc 查询命令的每用户冷却间隔；0 表示不限制。
	// 单次查询最坏链路为 Java 直连 + Bedrock 直连 + GS4 + 头像，耗时可达十几秒。
	Cooldown time.Duration
}

// GS4 策略取值。
const (
	GS4ModeAuto   = "auto"
	GS4ModeAlways = "always"
	GS4ModeNever  = "never"
)

// DefaultConfig minecraft 默认配置。
var DefaultConfig = Config{
	Timeout:     10 * time.Second,
	CacheTTL:    60 * time.Second,
	Avatars:     true,
	DirectQuery: true,
	EnableQuery: true,
	GS4Mode:     GS4ModeAuto,
	ErrCacheTTL: errCacheTTL,
	Cooldown:    3 * time.Second,
}

// queryTimeout GS4 Query 单次超时（未开启 enable-query 的服务器会静默等待到超时）。
const queryTimeout = 3 * time.Second

// errCacheTTL 查询失败（离线等）的负缓存时长：
// 防止群聊连查离线服务器反复走完整链路（直连超时 + API 回退），
// 同时保留"刚炸了再查一次"的及时性。
const errCacheTTL = 15 * time.Second

type mcPlugin struct {
	log      plugin.Logger
	cfg      Config
	client   *http.Client
	cache    *ttlCache[*MCServerStatus]
	errCache *ttlCache[error]
	sf       singleflight.Group
	fav      *FavManager
	// cooldown /mc 查询的每用户冷却（收藏增删查不受影响）
	cooldown *scopeLimiter
	// probes 外部依赖健康探针（mcsrvstat.us / mc-heads.net）
	probes []*health.APIProbe

	// ── 服务器管理（/mcadmin，默认关闭）──

	// adminCfg 管理配置（服务器注册表 + 权限与超时）。
	adminCfg AdminConfig
	// rcon 各服务器的 RCON 长连接池。
	rcon *rconPool
	// confirm 高危操作的二次确认存储。
	confirm *confirmStore
	// permSvc 权限服务（可选依赖，缺失时管理命令按拒绝处理）。
	permSvc *permission.Plugin
	// audit 审计日志服务（可选依赖；缺失时仅执行不记账）。
	audit *auditlog.Plugin
}

// loadConfig 从配置中读取设置，未配置时使用默认值。
func loadConfig(ctx *plugin.SetupContext) Config {
	cfg := DefaultConfig
	if ctx.Config == nil {
		return cfg
	}
	if v := ctx.Config.GetString("default_server", ""); v != "" {
		cfg.DefaultServer = v
	}
	if v := ctx.Config.GetDuration("timeout", 0); v > 0 {
		cfg.Timeout = v
	}
	if v := ctx.Config.GetDuration("cache_ttl", 0); v > 0 {
		cfg.CacheTTL = v
	}
	cfg.Avatars = ctx.Config.GetBool("avatars", DefaultConfig.Avatars)
	cfg.DirectQuery = ctx.Config.GetBool("direct_query", DefaultConfig.DirectQuery)
	cfg.EnableQuery = ctx.Config.GetBool("enable_query", DefaultConfig.EnableQuery)
	cfg.BlockPrivateTargets = ctx.Config.GetBool("block_private_targets", DefaultConfig.BlockPrivateTargets)
	if v := ctx.Config.GetInt("query_port", 0); v > 0 {
		cfg.QueryPort = v
	}
	// 允许显式配置为 0（关闭对应特性），故以 -1 作为"未配置"哨兵
	if v := ctx.Config.GetDuration("err_cache_ttl", -1); v >= 0 {
		cfg.ErrCacheTTL = v
	}
	if v := ctx.Config.GetDuration("cooldown", -1); v >= 0 {
		cfg.Cooldown = v
	}
	cfg.GS4Mode = normalizeGS4Mode(ctx.Config.GetString("gs4_mode", ""), cfg.GS4Mode)
	return cfg
}

// normalizeGS4Mode 归一化 GS4 策略配置；空值或非法值返回 fallback。
func normalizeGS4Mode(raw, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case GS4ModeAuto, GS4ModeAlways, GS4ModeNever:
		return strings.ToLower(strings.TrimSpace(raw))
	case "":
		return fallback
	default:
		return fallback
	}
}

const mcUsage = `用法：
  /mc <主机名|收藏名|序号>      — 查询服务器（自动探测 Java/Bedrock）
  /mc java <目标>              — 强制 Java 版查询
  /mc bedrock <目标>           — 强制 Bedrock 版查询
  /mc <目标1> <目标2> ...      — 一次对比多个服务器（最多 5 个）
  /mc add <名称> <地址>        — 收藏服务器（本群/本会话）
  /mc rm <名称>                — 移除收藏
  /mc list                     — 查看收藏列表

不加端口时 Java 默认 25565，Bedrock 默认 19132`

// maxCompareTargets 单次 /mc 多目标对比的服务器数量上限（限制并发与渲染规模）。
const maxCompareTargets = 5

// maxCompareConcurrency 对比查询的并发上限，避免一次命令打出过多外网连接。
const maxCompareConcurrency = 5

// New 创建 Minecraft 服务器状态查询插件的 Descriptor。
//
// 命令:
//   - /mc [名称|序号|主机名[:端口]]（支持 java/bedrock 强制版本）
//   - /mc add|rm|list — 按会话收藏服务器
//   - 省略地址时查询配置的默认服务器（plugins.minecraft.default_server）
//
// AI:
//   - query_minecraft_server(server_address, edition) → 服务器状态文本
//   - minecraft_query — 服务器状态技能
func New() *plugin.Descriptor {
	p := &mcPlugin{cfg: DefaultConfig}
	return &plugin.Descriptor{
		Name:         "minecraft",
		Version:      "1.3.0",
		OptionalDeps: []string{"storage", "permission", "auditlog"},
		Meta: &plugin.Metadata{
			Author:      "Remilia Community",
			Description: "Minecraft 服务器状态查询（Java + Bedrock）",
			Category:    "工具",
			Tags:        []string{"Minecraft", "MC", "游戏", "服务器"},
			HelpText: `Minecraft 服务器状态查询插件

用法：
  /mc <主机名|收藏名|序号>      — 自动探测 Java/Bedrock
  /mc java <目标>              — 强制 Java 版查询
  /mc bedrock <目标>           — 强制 Bedrock 版查询
  /mc <目标1> <目标2> ...      — 一次对比多个服务器（最多 5 个）
  /mc add <名称> <地址>        — 收藏服务器（每会话最多 10 个）
  /mc rm <名称>                — 移除收藏
  /mc list                     — 查看收藏列表
  /mc                          — 查询配置的默认服务器

服务器管理（默认关闭，需配置 plugins.minecraft.admin.enabled: true）：
  /mcadmin list                        — 列出可管理的服务器
  /mcadmin status <服务器>              — 玩家列表与 TPS（Paper）
  /mcadmin say <服务器> <文本>          — 以控制台身份广播
  /mcadmin whitelist <服务器> on|off|list
  /mcadmin whitelist <服务器> add|remove <玩家>
  /mcadmin kick <服务器> <玩家> [原因]   — 踢出玩家
  /mcadmin save <服务器>                — 立即保存世界
  /mcadmin confirm <验证码>             — 确认高危操作

特性：
  - SRV 记录自动解析（Java _minecraft._tcp / Bedrock _minecraft._udp，结果缓存）
  - 直连发包优先（Java SLP / Bedrock RakNet），失败自动回退 mcsrvstat.us API
  - 服务端软件识别：version.name 品牌推断 + mcsrvstat.us software 字段，
    开启 enable-query 时由 GS4 提供更准确的服务端描述与插件清单
  - GS4 Query 完整玩家列表（vanilla SLP 只返回至多 12 人样本）
  - 在线玩家头像展示（mc-heads.net）
  - 查询结果 TTL 缓存 + 并发查询合并 + 失败负缓存
  - 查询命令冷却，避免连点打满外部服务
  - 私有（内网）地址不经过第三方 API

配置（plugins.minecraft）：
  default_server / timeout / cache_ttl / err_cache_ttl / avatars
  direct_query / enable_query / gs4_mode / query_port
  cooldown / block_private_targets

  gs4_mode: auto（默认，仅玩家样本不完整时查）/ always（总是查，0 人在线也能拿到
  服务端插件信息）/ never
  cooldown: 每个用户查询冷却（默认 3s，0 关闭）
  block_private_targets: 是否禁止查询内网地址（默认 false；/mc 对所有群成员开放且
  不希望它被当作内网端口探测器时设为 true）

  提示：出站需代理或防火墙限制的部署（直连仅内网可用）请设 direct_query: false，
  全部走 API（代价：API 无法看到内网服务器，内网查询不可用）

服务器管理配置（plugins.minecraft.admin，默认关闭）：
  enabled / permission / allow_group_admins / timeout / max_output / servers

  enabled: 是否开启 /mcadmin（默认 false）。开启即等于把服务器控制台的一部分
  能力交给聊天里的使用者，必须由运维显式打开。
  permission: 操作服务器所需的 RBAC 权限点（默认 minecraft.admin）；
  superadmin 角色始终可用。
  allow_group_admins: 是否允许已绑定会话的群主/群管理员操作该会话的服务器
  （默认 true，仅对 scope 绑定到当前会话的服务器生效）。

  servers 条目字段：
    name        服务器名（命令中引用，需唯一）
    address     游戏地址 主机[:端口]
    rcon_host   RCON 主机（默认同 address 主机）
    rcon_port   RCON 端口（默认 25575）
    password    RCON 密码，支持 ${ENV_VAR} 引用环境变量
    scope       绑定的会话（群号）；留空为全局（仅权限点可操作）

  安全说明：
    - 密码只从配置读取，不会回显、不会写入审计日志；请用 ${ENV} 引用而非明文。
    - 不提供原生控制台透传，只开放白名单子命令；每条命令都写入审计日志
      （action: minecraft.rcon）。
    - whitelist off 需二次确认（/mcadmin confirm <验证码>，60 秒内有效）。
    - 未加载权限服务时管理命令按拒绝处理（不会向群成员放行）。
    - RCON 仅 Java 版服务端支持；Bedrock（BDS）没有远程控制台，只能查询。`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			p.log = ctx.Log
			p.cfg = loadConfig(ctx)
			p.client = &http.Client{Timeout: 15 * time.Second}
			p.cache = newTTLCache[*MCServerStatus](p.cfg.CacheTTL, 256)
			p.errCache = newTTLCache[error](p.cfg.ErrCacheTTL, 256)
			p.cooldown = newScopeLimiter(p.cfg.Cooldown, limiterMaxKeys)

			if storageSvc, ok := ctx.TryService[*storage.Plugin]("storage"); ok && !ctx.DryRun {
				if err := storageSvc.AutoMigrate(&FavServer{}); err != nil {
					return nil, fmt.Errorf("minecraft: auto migrate: %w", err)
				}
				p.fav = NewFavManager(storageSvc)
			}

			// 外部依赖探针：API 回退通道与头像源。上限设为 Degraded，
			// 避免第三方站点抖动把整体健康状态拖成 Unhealthy。
			p.probes = []*health.APIProbe{
				health.NewAPIProbe("mcsrvstat.us", "https://api.mcsrvstat.us", 5*time.Second,
					health.WithMaxSeverity(health.Degraded)),
			}
			if p.cfg.Avatars {
				p.probes = append(p.probes, health.NewAPIProbe("mc-heads.net", "https://mc-heads.net", 5*time.Second,
					health.WithMaxSeverity(health.Degraded)))
			}
			if !ctx.DryRun {
				// 后台定期探测（间隔 5 分钟，避免给 mcsrvstat.us 的速率限制添压力）
				for _, probe := range p.probes {
					ctx.Spawn(func(runCtx context.Context) {
						probe.StartBackground(runCtx, 5*time.Minute)
					})
				}
			}

			mcDef := command.NewDef("mc").Description("Minecraft 服务器状态查询").
				Arg("host", "服务器地址（主机名:端口）/收藏名/序号；多个参数时并行对比；留空使用默认服务器", false).
				Arg("edition", "强制版本 java/bedrock（可选，自动探测）", false).
				Example("/mc mc.hypixel.net").Example("/mc java mc.hypixel.net").
				Example("/mc 潜行服 生存服").Example("/mc add 潜行服 mc.steal.example:25565").Build()
			ctx.OnCommandDefWith("", "/mc", mcDef, p.handleMC, eventctx.OnMentionedBotOrNoMentions())

			// 服务器管理（RCON）：命令与 /mc 分开注册，
			// 避免 /mc 的收藏名/序号位置参数与子命令互相占用。
			p.adminCfg = loadAdminConfig(ctx)
			p.rcon = newRconPool()
			p.confirm = newConfirmStore(confirmTTL)
			if svc, ok := ctx.TryService[*permission.Plugin]("permission"); ok {
				p.permSvc = svc
			}
			if svc, ok := ctx.TryService[*auditlog.Plugin]("auditlog"); ok {
				p.audit = svc
			}
			p.registerAdminCommands(ctx)

			if p.adminCfg.Enabled {
				if ctx.Log != nil {
					ctx.Log.Infof("[minecraft] 服务器管理已启用：%d 台服务器，权限点 %q",
						len(p.adminCfg.Servers), p.adminCfg.Permission)
				}
				if !ctx.DryRun {
					// 定期回收空闲 RCON 连接（不打断执行中的命令）
					ctx.Spawn(func(runCtx context.Context) {
						ticker := time.NewTicker(rconIdleSweep)
						defer ticker.Stop()
						for {
							select {
							case <-runCtx.Done():
								return
							case <-ticker.C:
								p.rcon.CloseIdle(rconIdleTimeout)
							}
						}
					})
				}
			}

			return p, nil
		},
		Teardown: func(*plugin.TeardownContext) error {
			// 关闭 RCON 连接（未启用管理时 rcon 为 nil）
			if p.rcon != nil {
				p.rcon.Close()
			}
			return nil
		},
	}
}

// favScope 收藏的会话标识：平台 + 会话 ID（群 ID 或私聊用户 ID）。
func favScope(ctx *eventctx.Context) string {
	return ctx.GetEventPlatform() + ":" + ctx.GetChatInfo().ID
}

// resolveTarget 将用户输入解析为目标地址：收藏名 → 纯数字序号 → 地址。
func (p *mcPlugin) resolveTarget(scope, input string) (addr string, err error) {
	if p.fav != nil && p.fav.Available() {
		if fav, gerr := p.fav.Get(scope, input); gerr == nil {
			return fav.Address, nil
		}
	}
	// 纯数字 → 收藏序号（1 起始）
	if n, nerr := strconv.Atoi(input); nerr == nil && n >= 1 {
		favs, lerr := p.fav.List(scope)
		if lerr != nil {
			return "", lerr
		}
		if n <= len(favs) {
			return favs[n-1].Address, nil
		}
		return "", fmt.Errorf("收藏序号超出范围（共 %d 个）", len(favs))
	}
	return input, nil
}

// handleFav 处理收藏子命令（add/rm/list）。handled=false 表示不是子命令。
func (p *mcPlugin) handleFav(ctx *eventctx.Context, args []string) (handled bool) {
	if len(args) == 0 {
		return false
	}
	scope := favScope(ctx)
	switch args[0] {
	case "list":
		handled = true
		favs, err := p.fav.List(scope)
		if err != nil {
			ctx.ReplyError(err.Error())
			return true
		}
		if len(favs) == 0 {
			ctx.ReplyText("本会话还没有收藏的服务器，用 /mc add <名称> <地址> 添加")
			return true
		}
		var b strings.Builder
		fmt.Fprintf(&b, "⛏ 收藏的服务器（%d）\n", len(favs))
		for i, fav := range favs {
			fmt.Fprintf(&b, "  %d. %s — %s\n", i+1, fav.Name, fav.Address)
		}
		b.WriteString("查询：/mc <名称> 或 /mc <序号>")
		ctx.ReplyText(b.String())
	case "add":
		handled = true
		if len(args) < 3 {
			ctx.ReplyError("用法: /mc add <名称> <地址>，如 /mc add 潜行服 mc.example.com:25565")
			return true
		}
		name, addr := args[1], args[2]
		if utf8.RuneCountInString(name) > 32 || strings.ContainsAny(name, " \t\n") {
			ctx.ReplyError("名称需为不含空格的 1~32 字符")
			return true
		}
		if _, _, perr := parseHostPort(addr); perr != nil {
			ctx.ReplyError("地址无效: " + perr.Error())
			return true
		}
		created, aerr := p.fav.Add(scope, name, addr)
		if aerr != nil {
			ctx.ReplyError(aerr.Error())
			return true
		}
		if created {
			ctx.ReplySuccess(fmt.Sprintf("已收藏 %s → %s（查询：/mc %s）", name, addr, name))
		} else {
			ctx.ReplySuccess(fmt.Sprintf("已更新 %s → %s", name, addr))
		}
	case "rm", "remove":
		handled = true
		if len(args) < 2 {
			ctx.ReplyError("用法: /mc rm <名称>")
			return true
		}
		removed, rerr := p.fav.Remove(scope, args[1])
		if rerr != nil {
			ctx.ReplyError(rerr.Error())
			return true
		}
		if !removed {
			ctx.ReplyText("没有名为 " + args[1] + " 的收藏")
			return true
		}
		ctx.ReplySuccess("已移除收藏 " + args[1])
	}
	return handled
}

func (p *mcPlugin) handleMC(ctx *eventctx.Context) error {
	parsed, err := eventctx.ParseCommand(ctx)
	if err != nil {
		ctx.ReplyError(mcUsage)
		return nil
	}

	args := parsed.Positional
	// 收藏子命令优先处理，且不占用查询冷却。
	// 这里不额外判空：FavManager 的 nil 接收者会返回"存储服务不可用"，
	// 比把 "list" 当成主机名去查询更贴近用户意图。
	if p.handleFav(ctx, args) {
		return nil
	}

	edition, inputs, perr := p.parseQueryArgs(args)
	if perr != nil {
		ctx.ReplyError(perr.Error())
		return nil
	}

	// 冷却只作用于查询路径：单次查询最坏链路是 Java 直连 + Bedrock 直连 +
	// GS4 + 头像拉取，连点会对外网服务造成不必要的压力。
	if wait := p.cooldown.retryAfter(cooldownKey(ctx)); wait > 0 {
		ctx.ReplyText(fmt.Sprintf("⏳ 查询太频繁了，请 %s 后再试", humanizeWait(wait)))
		return nil
	}
	p.cooldown.mark(cooldownKey(ctx))

	if len(inputs) == 1 {
		return p.queryAndReply(ctx, edition, inputs[0])
	}
	return p.compareAndReply(ctx, edition, inputs)
}

// cooldownKey 查询冷却键：平台 + 会话 + 用户。
// 带会话维度是有意的——同一用户在不同群里各算一份，互不影响。
func cooldownKey(ctx *eventctx.Context) string {
	return "minecraft|" + favScope(ctx) + "|" + ctx.GetSenderInfo().ID
}

// humanizeWait 将等待时长格式化为可读文本。
func humanizeWait(d time.Duration) string {
	if d < time.Second {
		ms := d.Milliseconds()
		if ms < 1 {
			ms = 1
		}
		return fmt.Sprintf("%d 毫秒", ms)
	}
	return fmt.Sprintf("%.1f 秒", d.Seconds())
}

// parseQueryArgs 把 /mc 的位置参数解析为「强制版本 + 目标列表」。
//
// 兼容既有形式（版本可出现在第 1 或第 2 个位置），并支持多目标对比：
//
//	/mc <目标>
//	/mc java|bedrock <目标>
//	/mc <目标> java|bedrock
//	/mc <目标1> <目标2> ...
//
// 未提供目标时回退到配置的默认服务器。
func (p *mcPlugin) parseQueryArgs(args []string) (edition string, targets []string, err error) {
	rest := make([]string, 0, len(args))
	for i, a := range args {
		if edition == "" && i <= 1 && (a == "java" || a == "bedrock") {
			edition = a
			continue
		}
		rest = append(rest, a)
	}
	if len(rest) == 0 {
		if p.cfg.DefaultServer == "" {
			return "", nil, errors.New(mcUsage)
		}
		rest = append(rest, p.cfg.DefaultServer)
	}
	if len(rest) > maxCompareTargets {
		return "", nil, fmt.Errorf("一次最多对比 %d 个服务器（当前 %d 个）", maxCompareTargets, len(rest))
	}
	return edition, rest, nil
}

// resolveQueryTarget 将用户输入（收藏名/序号/地址）解析为 host 与 port。
// 返回的 port 可能为 0，表示未指定端口、由查询层按版本选择默认值。
func (p *mcPlugin) resolveQueryTarget(scope, input string) (string, int, error) {
	addr, err := p.resolveTarget(scope, input)
	if err != nil {
		return "", 0, err
	}
	return parseHostPort(addr)
}

// queryAndReply 查询单个服务器并回复状态卡片（失败时回复离线卡片）。
func (p *mcPlugin) queryAndReply(ctx *eventctx.Context, edition, input string) error {
	host, port, perr := p.resolveQueryTarget(favScope(ctx), input)
	if perr != nil {
		ctx.ReplyError(perr.Error())
		return nil
	}

	status, qerr := p.query(ctx.Context(), host, port, edition)
	if qerr != nil {
		// 离线：渲染离线卡片（渲染失败时回退文本）
		offline := &MCServerStatus{Host: host, Port: displayPort(port, edition), Error: qerr.Error()}
		if png, rerr := renderMCCard(offline); rerr == nil {
			ctx.Reply(platform.ImageDataMessage(png, "mc_offline.png", "image/png"))
			return nil
		}
		ctx.ReplyText(fmt.Sprintf("⛏ 服务器 %s 无法连接: %v", host, qerr))
		return nil
	}

	png, imgErr := renderMCCard(status)
	if imgErr != nil {
		ctx.ReplyText(formatMCText(status))
		return nil
	}

	ctx.Reply(platform.ImageDataMessage(png, "mc_status.png", "image/png"))
	return nil
}

// compareAndReply 并发查询多个服务器，渲染为一张对比卡片。
// 单个目标解析或查询失败不影响其余目标（失败项在卡片上显示为离线）。
func (p *mcPlugin) compareAndReply(ctx *eventctx.Context, edition string, inputs []string) error {
	scope := favScope(ctx)
	baseCtx := ctx.Context()
	entries := make([]compareEntry, len(inputs))

	var wg sync.WaitGroup
	sem := make(chan struct{}, maxCompareConcurrency)
	for i := range inputs {
		entries[i].Label = inputs[i]
		wg.Go(func() {
			select {
			case sem <- struct{}{}:
			case <-baseCtx.Done():
				entries[i].Err = baseCtx.Err()
				return
			}
			defer func() { <-sem }()

			host, port, err := p.resolveQueryTarget(scope, inputs[i])
			if err != nil {
				entries[i].Err = err
				return
			}
			entries[i].Label = displayTarget(host, port, edition)

			status, qerr := p.query(baseCtx, host, port, edition)
			if qerr != nil {
				entries[i].Err = qerr
				return
			}
			entries[i].Status = status
		})
	}
	wg.Wait()

	if png, rerr := renderMCCompareCard(entries); rerr == nil {
		ctx.Reply(platform.ImageDataMessage(png, "mc_compare.png", "image/png"))
		return nil
	}
	ctx.ReplyText(formatMCCompareText(entries))
	return nil
}

// displayTarget 对比卡片上的目标展示文本（未指定端口时补默认端口）。
func displayTarget(host string, port int, edition string) string {
	return net.JoinHostPort(host, fmt.Sprint(displayPort(port, edition)))
}

// displayPort 离线展示用端口：未指定端口时按版本回退默认值。
func displayPort(port int, edition string) int {
	if port > 0 {
		return port
	}
	if edition == "bedrock" {
		return DefaultBedrockPort
	}
	return DefaultJavaPort
}

// pingEdition 按指定版本（或自动探测）查询服务器状态。
func pingEdition(host string, port int, edition string, timeout time.Duration) (*MCServerStatus, error) {
	switch edition {
	case "java":
		return PingJava(host, port, timeout)
	case "bedrock":
		return PingBedrock(host, port, timeout)
	default:
		return Ping(host, port, timeout)
	}
}

// query 查询服务器状态：TTL 缓存 + singleflight 并发合并；
// 查询失败走短 TTL 负缓存；Java 版在线时按需补全 GS4 完整玩家列表与玩家头像。
func (p *mcPlugin) query(ctx context.Context, host string, port int, edition string) (*MCServerStatus, error) {
	// key 使用归一化主机名：大小写 / 末尾根点差异不应造成重复查询
	key := fmt.Sprintf("%s|%d|%s", normalizeHostKey(host), port, edition)
	if s, ok := p.cache.get(key); ok {
		mcCacheHitsTotal.Inc()
		return s, nil
	}
	if err, ok := p.errCache.get(key); ok {
		mcCacheHitsTotal.Inc()
		return nil, err
	}

	// 并发合并：TTL 过期瞬间多个请求只触发一次真实查询。
	//
	// 用 DoChan 而非 Do，配合上面的单一 fetcher 语义，解决两个问题：
	//  1. 首个调用方的 context 不再被所有等待者共享——查询在
	//     context.WithoutCancel 派生的独立预算上执行，任一调用方取消
	//     都不会让其他等待者失败，也不会把 context.Canceled 写进负缓存；
	//  2. 等待中的调用方可以在自己的 ctx 结束时提前返回，不必干等。
	ch := p.sf.DoChan(key, func() (val any, ferr error) {
		// 查询解析的是不可信的外部数据，而 singleflight 的 chan 路径在 fn
		// panic 时会以 `go panic(e)` 的形式终止进程（无法被上层 recover），
		// 所以这里兜底把 panic 转成查询错误，避免个别异常服务器拖垮整个 bot。
		defer func() {
			if r := recover(); r != nil {
				val, ferr = nil, fmt.Errorf("minecraft: 查询内部错误: %v", r)
				if p.log != nil {
					p.log.Errorf("[minecraft] query panic for %s: %v", key, r)
				}
			}
		}()

		// 双重检查：等待期间其他请求可能已填充缓存
		if s, ok := p.cache.get(key); ok {
			mcCacheHitsTotal.Inc()
			return s, nil
		}
		if e, ok := p.errCache.get(key); ok {
			mcCacheHitsTotal.Inc()
			return nil, e
		}
		qctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), p.queryBudget())
		defer cancel()
		return p.fetch(key, qctx, host, port, edition)
	})

	select {
	case res := <-ch:
		if res.Err != nil {
			return nil, res.Err
		}
		status, ok := res.Val.(*MCServerStatus)
		if !ok || status == nil {
			return nil, errors.New("minecraft: 查询结果类型异常")
		}
		return status, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// queryBudget 单次查询的内部总预算：主查询 + GS4 + 头像。
// 单独计算而不直接用配置的 timeout，因为共享查询已与调用方解耦，
// 需要覆盖后续几段补充数据的耗时。
func (p *mcPlugin) queryBudget() time.Duration {
	return p.cfg.Timeout + 2*queryTimeout + avatarFetchTimeout
}

// fetch 执行真实查询（直连或 API）并补充 GS4/头像数据，写入缓存。
func (p *mcPlugin) fetch(key string, ctx context.Context, host string, port int, edition string) (*MCServerStatus, error) {
	start := time.Now()
	ed := metricEdition(edition)

	// 可选的内网地址防护：/mc 对所有群成员开放时，避免被当作内网端口探测器
	if p.cfg.BlockPrivateTargets && isPrivateTarget(host) {
		err := fmt.Errorf("%w（目标为私有地址，已被 block_private_targets 禁止）", ErrNotOnline)
		p.setErrCache(key, err)
		return nil, err
	}

	var status *MCServerStatus
	var err error
	if p.cfg.DirectQuery {
		status, err = pingEdition(host, port, edition, p.cfg.Timeout)
	} else {
		status, err = PingViaAPI(host, port, edition, p.cfg.Timeout)
	}
	if err != nil {
		recordQuery(ed, "-", "error")
		p.setErrCache(key, err)
		return nil, err
	}
	recordQuery(status.Edition, status.Via, "ok")
	mcQueryDuration.WithLabelValues(status.Edition).Observe(time.Since(start).Seconds())

	// GS4：补充完整玩家列表与服务端软件信息（需服务器开启 enable-query）
	if reason := p.gs4SkipReason(status); reason == "" {
		// 使用 SRV 解析后的地址与端口（与 SLP 直连目标一致）
		qhost, qport := status.Host, status.Port
		if p.cfg.QueryPort > 0 {
			qport = p.cfg.QueryPort
		}
		gs4, qerr := QueryGS4(ctx, qhost, qport, queryTimeout)
		if qerr == nil {
			mcGS4Total.WithLabelValues("ok").Inc()
			applyGS4(status, gs4)
		} else {
			mcGS4Total.WithLabelValues("failed").Inc()
			if p.log != nil {
				p.log.Debugf("[minecraft] GS4 query %s:%d failed: %v", qhost, qport, qerr)
			}
		}
	} else {
		mcGS4Total.WithLabelValues("skipped").Inc()
		if p.log != nil {
			p.log.Debugf("[minecraft] GS4 query skipped: %s", reason)
		}
	}

	if p.cfg.Avatars {
		fetchPlayerHeads(ctx, p.client, status.Players.List)
	}

	p.cache.set(key, status)
	return status, nil
}

// setErrCache 写入负缓存。调用方主动取消不算"服务器离线"，因此不写入。
func (p *mcPlugin) setErrCache(key string, err error) {
	if p.cfg.ErrCacheTTL <= 0 || errors.Is(err, context.Canceled) {
		return
	}
	p.errCache.set(key, err)
}

// gs4SkipReason 返回跳过 GS4 的原因；返回空串表示应当尝试。
//
// 此前仅在「在线人数 > SLP 玩家样本数」时尝试，导致 0 人在线、或样本已完整
// （vanilla 会返回至多 12 人样本）的服务器永远拿不到服务端软件与插件清单。
// 现在尝试时机由 gs4_mode 决定：auto 保持原有省时策略，always 总能拿到信息。
func (p *mcPlugin) gs4SkipReason(status *MCServerStatus) string {
	switch {
	case !p.cfg.EnableQuery:
		return "enable_query=false"
	case !p.cfg.DirectQuery:
		return "direct_query=false（GS4 为裸 UDP，无法经 HTTP 代理）"
	case status == nil || status.Edition != "java":
		return "非 Java 版"
	case p.cfg.GS4Mode == GS4ModeNever:
		return "gs4_mode=never"
	case p.cfg.GS4Mode == GS4ModeAlways:
		return ""
	case status.Players.Online > len(status.Players.List):
		return ""
	default:
		return "gs4_mode=auto 且 SLP 玩家样本已完整"
	}
}

// metricEdition 指标标签用版本名（空 → auto）。
func metricEdition(edition string) string {
	if edition == "" {
		return "auto"
	}
	return edition
}

// ListTools 返回 AI 可调用的工具列表。实现 ai.ToolProvider。
func (p *mcPlugin) ListTools() []ai.Tool {
	return []ai.Tool{
		{
			Name:        "query_minecraft_server",
			Categories:  []string{"minecraft"},
			Description: "查询 Minecraft 服务器的状态，包括玩家数、版本、模式、在线玩家列表和延迟等信息",
			Parameters: ai.ToolParamSchema{
				Type: "object",
				Properties: map[string]ai.ToolParamSchema{
					"server_address": {
						Type:        "string",
						Description: "服务器地址（主机名[:端口]）、本会话收藏的服务器名称或序号（见 /mc add、/mc list）。Java 版默认 25565，Bedrock 版默认 19132；留空时使用配置的默认服务器",
					},
					"edition": {
						Type:        "string",
						Description: "服务器版本：java、bedrock 或留空自动探测",
						Enum:        []string{"java", "bedrock"},
					},
				},
			},
			Execute: func(gctx context.Context, args map[string]any) (string, error) {
				addr, _ := args["server_address"].(string)
				edition, _ := args["edition"].(string)
				if addr == "" {
					addr = p.cfg.DefaultServer
				}
				if addr == "" {
					return "", fmt.Errorf("请提供服务器地址")
				}
				// 会话收藏解析：AI 可用收藏名/序号指代服务器（与 /mc 命令同一收藏库）
				if scope := toolScopeFromAI(gctx); scope != "" {
					resolved, rerr := p.resolveTarget(scope, addr)
					if rerr != nil {
						return "", rerr
					}
					addr = resolved
				}
				host, port, perr := parseHostPort(addr)
				if perr != nil {
					return "", perr
				}
				status, err := p.query(gctx, host, port, edition)
				if err != nil {
					return fmt.Sprintf("服务器 %s 无法连接: %v", addr, err), nil
				}
				return formatMCText(status), nil
			},
		},
	}
}

// toolScopeFromAI 从工具执行 context 提取收藏 scope（平台:会话），
// 与 /mc 命令的 favScope 同构。无会话信息（测试等直接调用场景）返回空串。
func toolScopeFromAI(gctx context.Context) string {
	src, ok := ai.ToolSourceFromContext(gctx)
	if !ok || src.ChatID == "" {
		return ""
	}
	return src.Platform + ":" + src.ChatID
}

// ListSkills 返回 AI 技能列表。实现 ai.SkillProvider。
func (p *mcPlugin) ListSkills() []ai.Skill {
	return []ai.Skill{
		{
			Name:        "minecraft_query",
			Description: "Minecraft 服务器状态查询",
			Prompt: `你是一个 Minecraft 服务器状态查询助手。
当用户询问某个 Minecraft 服务器的状态时，使用 query_minecraft_server 工具进行查询。
server_address 可以是服务器地址，也可以是本会话收藏的服务器名称或序号（用户说"查一下潜行服"时先用名称）。
返回的信息包括：服务器在线状态、MOTD、版本、游戏模式、在线玩家数（含玩家列表和头像）和延迟。

如果服务器无法连接，请用友好的语气告知用户。`,
			Tools: p.ListTools(),
		},
	}
}

// HealthCheckers 返回插件的健康探针。实现 health.CheckProvider。
func (p *mcPlugin) HealthCheckers() []health.Checker {
	out := make([]health.Checker, 0, len(p.probes)+1)
	for _, probe := range p.probes {
		out = append(out, probe)
	}
	// 追加插件自身状态（缓存规模、关键配置），便于 /health?view=full 排查
	out = append(out, pluginHealthChecker{p: p})
	return out
}
