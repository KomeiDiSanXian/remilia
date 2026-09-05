// Package minecraft 提供 Minecraft 服务器状态查询功能。
//
// 命令: /mc [add|rm|list] [java|bedrock] [名称|序号|主机名[:端口]]
// AI 工具: query_minecraft_server
// AI 技能: minecraft_query
package minecraft

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
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
	// QueryPort GS4 Query 端口；0 = 与服务器端口一致。
	QueryPort int
}

// DefaultConfig minecraft 默认配置。
var DefaultConfig = Config{
	Timeout:     10 * time.Second,
	CacheTTL:    60 * time.Second,
	Avatars:     true,
	DirectQuery: true,
	EnableQuery: true,
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
	if v := ctx.Config.GetInt("query_port", 0); v > 0 {
		cfg.QueryPort = v
	}
	return cfg
}

const mcUsage = `用法：
  /mc <主机名|收藏名|序号>      — 查询服务器（自动探测 Java/Bedrock）
  /mc java <目标>              — 强制 Java 版查询
  /mc bedrock <目标>           — 强制 Bedrock 版查询
  /mc add <名称> <地址>        — 收藏服务器（本群/本会话）
  /mc rm <名称>                — 移除收藏
  /mc list                     — 查看收藏列表

不加端口时 Java 默认 25565，Bedrock 默认 19132`

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
		Version:      "1.2.0",
		OptionalDeps: []string{"storage"},
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
  /mc add <名称> <地址>        — 收藏服务器（每会话最多 10 个）
  /mc rm <名称>                — 移除收藏
  /mc list                     — 查看收藏列表
  /mc                          — 查询配置的默认服务器

特性：
  - SRV 记录自动解析（_minecraft._tcp，结果缓存）
  - 直连发包优先（Java SLP / Bedrock RakNet），失败自动回退 mcsrvstat.us API
  - GS4 Query 完整玩家列表与服务端插件信息（需开启 enable-query）
  - 在线玩家头像展示（mc-heads.net）
  - 查询结果 TTL 缓存 + 并发查询合并
  - 私有（内网）地址不经过第三方 API

配置（plugins.minecraft）：
  default_server / timeout / cache_ttl / avatars / direct_query / enable_query / query_port
  提示：出站需代理或防火墙限制的部署（直连仅内网可用）请设 direct_query: false，
  全部走 API（代价：API 无法看到内网服务器，内网查询不可用）`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			p.log = ctx.Log
			p.cfg = loadConfig(ctx)
			p.client = &http.Client{Timeout: 15 * time.Second}
			p.cache = newTTLCache[*MCServerStatus](p.cfg.CacheTTL, 256)
			p.errCache = newTTLCache[error](errCacheTTL, 256)

			if storageSvc, ok := ctx.TryService[*storage.Plugin]("storage"); ok && !ctx.DryRun {
				if err := storageSvc.AutoMigrate(&FavServer{}); err != nil {
					return nil, fmt.Errorf("minecraft: auto migrate: %w", err)
				}
				p.fav = NewFavManager(storageSvc)
			}

			mcDef := command.NewDef("mc").Description("Minecraft 服务器状态查询").
				Arg("host", "服务器地址（主机名:端口）/收藏名/序号；留空使用默认服务器", false).
				Arg("edition", "强制版本 java/bedrock（可选，自动探测）", false).
				Example("/mc mc.hypixel.net").Example("/mc java mc.hypixel.net").
				Example("/mc add 潜行服 mc.steal.example:25565").Example("/mc 潜行服").Build()
			ctx.OnCommandDefWith("", "/mc", mcDef, p.handleMC, eventctx.OnMentionedBotOrNoMentions())

			return p, nil
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
	if p.fav != nil {
		if p.handleFav(ctx, args) {
			return nil
		}
	}

	edition := ""
	var input string
	switch {
	case len(args) == 0:
		input = p.cfg.DefaultServer
	case args[0] == "java" || args[0] == "bedrock":
		edition = args[0]
		if len(args) >= 2 {
			input = args[1]
		} else {
			input = p.cfg.DefaultServer
		}
	case len(args) >= 2 && (args[1] == "java" || args[1] == "bedrock"):
		input, edition = args[0], args[1]
	default:
		input = args[0]
	}
	if input == "" {
		ctx.ReplyError(mcUsage)
		return nil
	}

	addr, terr := p.resolveTarget(favScope(ctx), input)
	if terr != nil {
		ctx.ReplyError(terr.Error())
		return nil
	}

	host, port, perr := parseHostPort(addr)
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
	key := fmt.Sprintf("%s|%d|%s", host, port, edition)
	if s, ok := p.cache.get(key); ok {
		mcCacheHitsTotal.Inc()
		return s, nil
	}
	if err, ok := p.errCache.get(key); ok {
		mcCacheHitsTotal.Inc()
		return nil, err
	}

	// 并发合并：TTL 过期瞬间多个请求只触发一次真实查询
	v, err, _ := p.sf.Do(key, func() (any, error) {
		// 双重检查：等待期间其他请求可能已填充缓存
		if s, ok := p.cache.get(key); ok {
			mcCacheHitsTotal.Inc()
			return s, nil
		}
		if e, ok := p.errCache.get(key); ok {
			mcCacheHitsTotal.Inc()
			return nil, e
		}
		return p.fetch(key, ctx, host, port, edition)
	})
	if err != nil {
		return nil, err
	}
	return v.(*MCServerStatus), nil
}

// fetch 执行真实查询（直连或 API）并补充 GS4/头像数据，写入缓存。
func (p *mcPlugin) fetch(key string, ctx context.Context, host string, port int, edition string) (*MCServerStatus, error) {
	start := time.Now()
	ed := metricEdition(edition)

	var status *MCServerStatus
	var err error
	if p.cfg.DirectQuery {
		status, err = pingEdition(host, port, edition, p.cfg.Timeout)
	} else {
		status, err = PingViaAPI(host, port, edition, p.cfg.Timeout)
	}
	if err != nil {
		recordQuery(ed, "-", "error")
		p.errCache.set(key, err)
		return nil, err
	}
	recordQuery(status.Edition, status.Via, "ok")
	mcQueryDuration.WithLabelValues(status.Edition).Observe(time.Since(start).Seconds())

	// GS4：sample 未覆盖全部在线玩家时才值得尝试（避免无谓的 3s 等待）。
	// GS4 同为直连 UDP，随 direct_query 一并禁用。
	if p.cfg.EnableQuery && p.cfg.DirectQuery && status.Edition == "java" && status.Players.Online > len(status.Players.List) {
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
	}

	if p.cfg.Avatars {
		fetchPlayerHeads(ctx, p.client, status.Players.List)
	}

	p.cache.set(key, status)
	return status, nil
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
	return []health.Checker{}
}
