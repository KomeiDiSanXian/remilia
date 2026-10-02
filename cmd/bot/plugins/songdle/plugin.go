package songdle

import (
	"context"
	"fmt"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/i18n"
	"github.com/KomeiDiSanXian/remilia/command"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/storage"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

const pluginName = "songdle"

// 猜测次数与曲库筛选的取值范围 / 默认值。
const (
	minTries        = 1
	defaultTries    = 12
	defaultMaxTries = 30
)

// timeNow 是包级时钟，测试可替换以固定时间。
var timeNow = time.Now

// config 插件运行配置（来自 config.yaml 的 plugins.songdle 段）。
type config struct {
	// DefaultTries 默认探测次数；MaxTries 允许的上限。
	DefaultTries int
	MaxTries     int
	DefaultScope Scope
	// DefaultType / DefaultGenre 是默认曲库筛选条件（SD/DX 与流派子串）。
	DefaultType  string
	DefaultGenre string
	DefaultDaily bool
	// RevealArtist 为真时开局即公布曲师。
	RevealArtist bool
	// SongsFile / AliasFile 优先于对应 URL，URL 优先于内置数据。
	SongsFile string
	AliasFile string
	SongsURL  string
	AliasURL  string
	Timezone  string
	TTL       time.Duration
}

// Plugin 「猜音游曲目」插件实例。
type Plugin struct {
	log      plugin.Logger
	i18n     *i18n.Plugin
	store    *store
	sessions *SessionStore
	pool     *Pool
	cfg      config
	location *time.Location
}

// New 创建「猜音游曲目」插件描述符。
//
// 依赖 i18n（文案）；storage（战绩/排行榜/每日锁）为可选依赖，缺失时对局仍可进行，
// 只是 stats/top 不可用、每日题不再限制重复开局。
func New() *plugin.Descriptor {
	p := &Plugin{}
	return &plugin.Descriptor{
		Name:         pluginName,
		Version:      "0.3.0",
		Deps:         []string{"i18n"},
		OptionalDeps: []string{"storage"},
		Meta: &plugin.Metadata{
			Author:      "Remilia Community",
			Description: "猜音游曲目小游戏（Songdle）— 机器人随机选一首 maimai 曲目并涂黑曲名，玩家逐个探测曲师/流派/类型/版本/BPM/定数/绝赞数来缩小范围（🟩 命中 / 🟨 接近 / ⬜ 不对，数值另有 ↑↓），最后用曲名一锤定音",
			Category:    "娱乐",
			Tags:        []string{"songdle", "猜歌", "猜曲", "音游", "maimai", "舞萌", "maidle", "游戏", "wordle"},
			HelpText:    "猜音游曲目小游戏：/songdle 开局，/songdle 曲师 <名字>、/songdle bpm 150 等探测线索（也写作 /songdle probe <属性> <取值>），最后用 /songdle 歌名 <曲名> 提交答案。详见 /songdle help",
		},
		Setup:    p.setup,
		Teardown: p.teardown,
	}
}

func (p *Plugin) setup(ctx *plugin.SetupContext) (any, error) {
	p.log = ctx.Log
	p.loadConfig(ctx)
	p.sessions = NewSessionStore(p.cfg.TTL)
	p.location = p.resolveLocation(p.cfg.Timezone)

	pool, err := loadPool(poolOptions{
		SongsFile: p.cfg.SongsFile,
		AliasFile: p.cfg.AliasFile,
		SongsURL:  p.cfg.SongsURL,
		AliasURL:  p.cfg.AliasURL,
	})
	if err != nil {
		return nil, fmt.Errorf("songdle: 加载曲库失败: %w", err)
	}
	if pool.Len() == 0 {
		return nil, fmt.Errorf("songdle: 曲库为空，请检查内置数据或 plugins.songdle 的 songs_file / songs_url 配置")
	}
	p.pool = pool

	i18nSvc, ok := ctx.TryService[*i18n.Plugin]("i18n")
	if !ok {
		return nil, fmt.Errorf("songdle: i18n 服务不可用")
	}
	p.i18n = i18nSvc
	if !ctx.DryRun {
		if err := registerLocales(i18nSvc); err != nil {
			ctx.Log.Warnf("songdle: 注册语言包失败: %v", err)
		}
	}

	if storageSvc, ok := ctx.TryService[*storage.Plugin]("storage"); ok {
		if !ctx.DryRun {
			p.store = newStore(storageSvc)
			if err := p.store.migrate(); err != nil {
				return nil, fmt.Errorf("songdle: 建表失败: %w", err)
			}
		}
	} else {
		ctx.Log.Warnf("songdle: storage 服务不可用，战绩与排行榜将不可用")
	}

	// 周期回收空闲对局，避免内存无限增长；弃局按失败结算，防止挂机逃避败场。
	ctx.Spawn(func(runCtx context.Context) {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				for _, g := range p.sessions.Sweep() {
					g.Finished, g.Won = true, false
					p.finalize(g)
				}
			}
		}
	})

	ctx.OnCommandDefWith("", "/songdle", p.commandDef(), p.handleSongdle,
		eventctx.OnMentionedBotOrNoMentions())

	// 按钮回调（QQ 走指令按钮填充命令，不产生回调）。
	ctx.Reg.RegisterMatcher(string(platform.EventKindInteraction)).Handle(p.handleInteraction)

	return p, nil
}

func (p *Plugin) teardown(_ *plugin.TeardownContext) error { return nil }

// commandDef 构建 /songdle 的命令定义。
func (p *Plugin) commandDef() *command.Definition {
	return command.NewDef("songdle").
		Alias("猜歌", "猜曲", "sg").
		Description("猜音游曲目小游戏（Songdle / Maidle 风格）").
		SubCommand(command.NewDef("guess").Alias("g", "answer", "title").Description("提交最终答案（曲名或俗称）").
			Arg("title", "曲名 / 俗称", false).Build()).
		SubCommand(command.NewDef("probe").Alias("p", "ask").Description("探测一项元数据，如 曲师 / 流派 / bpm / 定数").
			Arg("attribute", "属性名（曲师/流派/类型/版本/bpm/定数/绝赞）", false).
			Arg("value", "要探测的取值", true).Build()).
		SubCommand(command.NewDef("board").Alias("b").Description("重新发送当前提示板").Build()).
		SubCommand(command.NewDef("giveup").Alias("surrender").Description("放弃并公布答案").Build()).
		SubCommand(command.NewDef("pool").Alias("data", "info").Description("查看曲库信息").Build()).
		SubCommand(command.NewDef("stats").Alias("stat").Description("查看个人战绩").Build()).
		SubCommand(command.NewDef("top").Alias("rank").Description("查看排行榜").Build()).
		SubCommand(command.NewDef("lang").Alias("language").Description("切换语言").
			Arg("locale", "语言代码，如 zh-CN", false).Build()).
		SubCommand(command.NewDef("help").Alias("?").Description("查看帮助").Build()).
		Flag("tries", "n", "猜测次数上限", command.ArgTypeInt).
		Flag("scope", "s", "隔离维度 user|group", command.ArgTypeString).
		Flag("type", "t", "曲目类型 SD|DX|all", command.ArgTypeString).
		Flag("genre", "c", "流派关键词（子串匹配）", command.ArgTypeString).
		Flag("daily", "d", "每日同一题", command.ArgTypeBool).
		Flag("artist", "a", "开局即公布曲师", command.ArgTypeBool).
		Example("/songdle").Example("/songdle --tries 15 --type DX").
		Example("/songdle --genre 东方").Example("/songdle --daily").
		Example("/songdle 曲师 sasakure.UK").Example("/songdle bpm 150").
		Example("/songdle probe 定数 13.5").Example("/songdle 歌名 Halcyon").
		Example("/songdle stats").Example("/songdle top").
		Build()
}

func (p *Plugin) loadConfig(ctx *plugin.SetupContext) {
	p.cfg = config{
		DefaultTries: defaultTries,
		MaxTries:     defaultMaxTries,
		DefaultScope: ScopeGroup,
		Timezone:     "Asia/Shanghai",
		TTL:          30 * time.Minute,
	}
	if ctx.Config == nil {
		return
	}
	if v := ctx.Config.GetInt("default_tries", 0); v >= minTries {
		p.cfg.DefaultTries = v
	}
	if v := ctx.Config.GetInt("max_tries", 0); v >= minTries {
		p.cfg.MaxTries = v
	}
	if p.cfg.MaxTries < p.cfg.DefaultTries {
		p.cfg.MaxTries = p.cfg.DefaultTries
	}
	if v := ctx.Config.GetString("default_scope", ""); v != "" {
		if s, ok := ParseScope(v); ok {
			p.cfg.DefaultScope = s
		}
	}
	if v := ctx.Config.GetString("default_type", ""); v != "" {
		if ty, ok := normalizeType(v); ok {
			p.cfg.DefaultType = ty
		}
	}
	if v := ctx.Config.GetString("default_genre", ""); v != "" {
		p.cfg.DefaultGenre = v
	}
	p.cfg.DefaultDaily = ctx.Config.GetBool("daily", p.cfg.DefaultDaily)
	p.cfg.RevealArtist = ctx.Config.GetBool("artist", p.cfg.RevealArtist)
	if v := ctx.Config.GetString("songs_file", ""); v != "" {
		p.cfg.SongsFile = v
	}
	if v := ctx.Config.GetString("alias_file", ""); v != "" {
		p.cfg.AliasFile = v
	}
	if v := ctx.Config.GetString("songs_url", ""); v != "" {
		p.cfg.SongsURL = v
	}
	if v := ctx.Config.GetString("alias_url", ""); v != "" {
		p.cfg.AliasURL = v
	}
	if v := ctx.Config.GetString("timezone", ""); v != "" {
		p.cfg.Timezone = v
	}
	if v := ctx.Config.GetDuration("session_ttl", 0); v > 0 {
		p.cfg.TTL = v
	}
}

func (p *Plugin) resolveLocation(name string) *time.Location {
	if name == "" {
		return time.Local
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		p.warnf("songdle: 时区 %q 无效，回退本地时区", name)
		return time.Local
	}
	return loc
}

// today 返回配置时区下的当天日期（YYYY-MM-DD），用于每日题选曲与每日锁。
func (p *Plugin) today() string {
	loc := p.location
	if loc == nil {
		loc = time.Local
	}
	return timeNow().In(loc).Format("2006-01-02")
}

// warnf 在 logger 可用时输出告警（测试环境可能未注入 logger）。
func (p *Plugin) warnf(format string, args ...any) {
	if p.log != nil {
		p.log.Warnf(format, args...)
	}
}
