package wordle

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

const pluginName = "wordle"

// timeNow 是包级时钟，测试可替换以固定时间。
var timeNow = time.Now

// config 插件运行配置（来自 config.yaml 的 plugins.wordle 段）。
type config struct {
	DefaultLength int
	DefaultTries  int
	DefaultScope  Scope
	DefaultDaily  bool
	// DefaultRules 是未显式指定玩法开关时默认启用的规则集合。
	DefaultRules RuleSet
	// BlitzWindow 是限时模式默认的单步时限。
	BlitzWindow time.Duration
	Timezone    string
	TTL         time.Duration
}

// Plugin Wordle 插件实例。
type Plugin struct {
	log      plugin.Logger
	i18n     *i18n.Plugin
	store    *store
	sessions *SessionStore
	cfg      config
	location *time.Location
}

// New 创建 Wordle 插件描述符。
//
// 依赖 i18n（文案）与 storage（统计/每日锁持久化）。
func New() *plugin.Descriptor {
	p := &Plugin{}
	return &plugin.Descriptor{
		Name:    pluginName,
		Version: "0.1.0",
		Deps:    []string{"i18n", "storage"},
		Meta: &plugin.Metadata{
			Author:      "Remilia Community",
			Description: "Wordle 猜词小游戏 — 自定义长度/次数、双谜底同屏、困难/限时/连锁，以及反转/盲猜/迷雾/诱饵等 14 种颜色玩法、每日题、统计与排行榜",
			Category:    "娱乐",
			Tags:        []string{"wordle", "猜词", "益智", "游戏", "word", "连锁", "限时", "双谜底", "颜色"},
			HelpText: `Wordle 猜词小游戏

用法：
  /wordle [--length 4-7] [--tries 次数] [--daily] [--scope user|group]  开始经典对局
  /wordle [--duet|--quad|--boards N]  多谜底同屏：N 块棋盘共享猜测
  /wordle [--hard] [--blind] [--fog[=N]] [--obscure] [--gauntlet]  更难的玩法
  /wordle [--blitz[=秒]] [--chain] [--hint-cost] [--chaos] [--race]  其他玩法
  /wordle guess <单词>        提交猜测
  /wordle giveup              放弃并公布答案
  /wordle hint [类型]         提示：letter(默认) / exclude / vowels / repeat
  /wordle board               重新发送当前棋盘
  /wordle rules               查看本局玩法与进度
  /wordle stats [@用户]       查看统计
  /wordle top                 排行榜
  /wordle lang <语言>         切换语言（zh-CN / en-US）

玩法（可任意组合）：
  --hard       困难模式：已揭示的绿/黄字母必须复用
  --blind      盲猜：黄色按灰色显示，只有位置正确的字母才显示绿色
  --fog        迷雾：只保留最近 N 行判定（默认 1 行，最多 4 行）
  --obscure    冷门词库：谜底只从低频词中抽取，显著提高难度
  --gauntlet   车轮战：连锁模式下每解出一题就少一次机会（下限 2）
  --blitz      限时模式：每次作答有倒计时，超时判负（默认 60 秒）
  --chain      连锁模式：猜中后自动进入下一题，直到失败
  --hint-cost  提示消耗一次机会
  --chaos      混沌模式：开局随机附加盲猜/禁重复字母/双倍消耗/禁提示
  --race       抢分模式：群内按解谜贡献计分（仅群维度）

颜色玩法（可以任意叠加，全部只影响显示，不改变胜负判定）：
  --invert      反转：绿（正确）与灰（不存在）互换显示
  --hit-only    只留命中：绿+黄合并为黄，位置信息全部丢失
  --near        邻近色（青）：字母在谜底中且与真实位置相差 ±1
  --repeat      重复色（紫）：谜底含该字母多个副本时用紫色替代黄色
  --swap-meaning 语义互换：每局随机决定绿/黄含义互换
  --colorfog[=N] 颜色预算：每行最多保留 N 个着色格（默认 2）
  --decay[=N]   颜色衰减：只保留最近 N 行的颜色，更早的行变灰（默认 2）
  --unknown[=N] 未知格：每行随机 N 格显示为中性「?」（默认 1）
  --delayed[=N] 延迟着色：最新的 N 行暂不显示颜色（默认 1）
  --decoy[=N]   诱饵：每行随机把 N 个灰色谎报为黄色（默认 1）
  --glitch      故障：每行随机翻转一格的显示颜色
  --mole        内鬼：每局随机一个位置永远显示为绿色
  --hidden-key  隐藏键盘颜色
  --score-color 计分色：整行同色编码绿色总数，位置信息全部丢失

多谜底：--duet（2 块）/--quad（4 块）/--boards 2-4，所有棋盘共用同一串猜测，
全部解开才算胜利。可叠加 --hard/--blind/--obscure 等大幅提高难度。

默认：5 个字母、6 次机会、随机出题、群内共享对局、单谜底、无附加玩法。`,
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

	i18nSvc, ok := ctx.TryService[*i18n.Plugin]("i18n")
	if !ok {
		return nil, fmt.Errorf("wordle: i18n 服务不可用")
	}
	p.i18n = i18nSvc

	storageSvc, ok := ctx.TryService[*storage.Plugin]("storage")
	if !ok {
		return nil, fmt.Errorf("wordle: storage 服务不可用")
	}

	if !ctx.DryRun {
		if err := registerLocales(i18nSvc); err != nil {
			ctx.Log.Warnf("wordle: 注册语言包失败: %v", err)
		}
		p.store = newStore(storageSvc)
		if err := p.store.migrate(); err != nil {
			return nil, fmt.Errorf("wordle: 建表失败: %w", err)
		}
	}

	// 预加载词库，尽早暴露缺失/损坏的资源。
	for _, l := range SupportedLengths {
		if _, err := loadWordBank(l); err != nil {
			ctx.Log.Warnf("wordle: 加载 %d 字母词库失败: %v", l, err)
		}
	}

	// 周期回收空闲对局，避免内存无限增长。
	ctx.Spawn(func(runCtx context.Context) {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				// 限时对局超时后先结算统计，避免"没人再说话"导致战绩丢失。
				for _, g := range p.sessions.MarkExpired(timeNow()) {
					p.finalize(g)
				}
				p.sessions.Sweep()
			}
		}
	})

	def := command.NewDef("wordle").Description("Wordle 猜词小游戏").
		SubCommand(command.NewDef("guess").Description("提交一次猜测").
			Arg("word", "猜测的单词", false).Build()).
		SubCommand(command.NewDef("giveup").Description("放弃并公布答案").Build()).
		SubCommand(command.NewDef("hint").Description("使用一次提示").
			Arg("type", "提示类型 letter|exclude|vowels|repeat", false).Build()).
		SubCommand(command.NewDef("board").Description("重新发送当前棋盘").Build()).
		SubCommand(command.NewDef("rules").Description("查看本局玩法").Build()).
		SubCommand(command.NewDef("stats").Description("查看统计").Build()).
		SubCommand(command.NewDef("top").Description("查看排行榜").Build()).
		SubCommand(command.NewDef("lang").Description("切换语言").
			Arg("locale", "语言代码，如 zh-CN", true).Build()).
		SubCommand(command.NewDef("help").Description("查看帮助").Build()).
		Flag("length", "l", "单词长度（4-7，默认 5）", command.ArgTypeInt).
		Flag("tries", "t", "答题次数（1-12，默认 6）", command.ArgTypeInt).
		Flag("daily", "d", "使用每日题", command.ArgTypeBool).
		Flag("scope", "s", "隔离维度 user|group（默认 group）", command.ArgTypeString).
		Flag("hard", "", "困难模式：已揭示的绿/黄必须复用", command.ArgTypeBool).
		Flag("blitz", "", "限时模式：--blitz 或 --blitz=45（秒）", command.ArgTypeString).
		Flag("chain", "", "连锁模式：猜中后自动进入下一题", command.ArgTypeBool).
		Flag("hint-cost", "", "提示消耗一次机会", command.ArgTypeBool).
		Flag("chaos", "", "开局随机附加 1-2 个修饰符", command.ArgTypeBool).
		Flag("race", "", "群内抢分：按解谜贡献计分", command.ArgTypeBool).
		Flag("blind", "", "盲猜：黄色按灰色显示", command.ArgTypeBool).
		Flag("fog", "", "迷雾：--fog 或 --fog=3（只保留最近 N 行）", command.ArgTypeString).
		Flag("obscure", "", "冷门词库：谜底只取低频词", command.ArgTypeBool).
		Flag("gauntlet", "", "车轮战：连锁每解一题少一次机会", command.ArgTypeBool).
		Flag("duet", "", "双谜底同屏：两块棋盘共享猜测", command.ArgTypeBool).
		Flag("quad", "", "四谜底同屏：四块棋盘共享猜测", command.ArgTypeBool).
		Flag("boards", "", "棋盘数量（2-4）", command.ArgTypeInt).
		Flag("invert", "", "反转：绿与灰互换显示", command.ArgTypeBool).
		Flag("hit-only", "", "只留命中：绿+黄合并为黄", command.ArgTypeBool).
		Flag("near", "", "邻近色（青）：与真实位置相差 ±1", command.ArgTypeBool).
		Flag("repeat", "", "重复色（紫）：谜底含多个副本时替代黄色", command.ArgTypeBool).
		Flag("swap-meaning", "", "语义互换：每局随机交换绿/黄含义", command.ArgTypeBool).
		Flag("colorfog", "", "颜色预算：--colorfog 或 --colorfog=2", command.ArgTypeString).
		Flag("decay", "", "颜色衰减：--decay 或 --decay=2", command.ArgTypeString).
		Flag("unknown", "", "未知格：--unknown 或 --unknown=1", command.ArgTypeString).
		Flag("delayed", "", "延迟着色：--delayed 或 --delayed=1", command.ArgTypeString).
		Flag("decoy", "", "诱饵：--decoy 或 --decoy=1", command.ArgTypeString).
		Flag("glitch", "", "故障：每行随机翻转一格颜色", command.ArgTypeBool).
		Flag("mole", "", "内鬼：每局随机一个位置永远显示绿色", command.ArgTypeBool).
		Flag("hidden-key", "", "隐藏键盘颜色", command.ArgTypeBool).
		Flag("score-color", "", "计分色：整行同色编码绿色总数", command.ArgTypeBool).
		Example("/wordle").Example("/wordle --length 6 --tries 8").
		Example("/wordle --hard --chain").Example("/wordle --blitz=45 --race").
		Example("/wordle --duet").Example("/wordle --duet --obscure --blind").
		Example("/wordle --quad --fog=2 --gauntlet").
		Example("/wordle --invert").Example("/wordle --hit-only --decoy").
		Example("/wordle --near --repeat").Example("/wordle --score-color --hidden-key").
		Example("/wordle guess crane").Example("/wordle hint exclude").
		Build()
	ctx.OnCommandDefWith("", "/wordle", def, p.handleWordle, eventctx.OnMentionedBotOrNoMentions())

	// 按钮回调（Discord/Telegram 等以回调为主；QQ 走指令按钮填充命令，不产生回调）。
	ctx.Reg.RegisterMatcher(string(platform.EventKindInteraction)).Handle(p.handleInteraction)

	return p, nil
}

func (p *Plugin) teardown(_ *plugin.TeardownContext) error {
	return nil
}

func (p *Plugin) loadConfig(ctx *plugin.SetupContext) {
	p.cfg = config{
		DefaultLength: DefaultLength,
		DefaultTries:  DefaultMaxAttempts,
		DefaultScope:  ScopeGroup,
		BlitzWindow:   60 * time.Second,
		Timezone:      "Asia/Shanghai",
		TTL:           30 * time.Minute,
	}
	if ctx.Config == nil {
		return
	}
	if v := ctx.Config.GetInt("default_length", 0); v > 0 {
		p.cfg.DefaultLength = v
	}
	if v := ctx.Config.GetInt("default_tries", 0); v > 0 {
		p.cfg.DefaultTries = v
	}
	if v := ctx.Config.GetString("default_scope", ""); v != "" {
		if s, ok := ParseScope(v); ok {
			p.cfg.DefaultScope = s
		}
	}
	p.cfg.DefaultDaily = ctx.Config.GetBool("daily", p.cfg.DefaultDaily)
	if v := ctx.Config.GetString("timezone", ""); v != "" {
		p.cfg.Timezone = v
	}
	if v := ctx.Config.GetDuration("session_ttl", 0); v > 0 {
		p.cfg.TTL = v
	}
	if v := ctx.Config.GetString("default_rules", ""); v != "" {
		rules, err := ParseRuleList(v)
		if err != nil {
			p.log.Warnf("wordle: default_rules 无效（%v），按经典规则运行", err)
		} else {
			p.cfg.DefaultRules = rules
		}
	}
	if v := ctx.Config.GetInt("blitz_seconds", 0); v > 0 {
		p.cfg.BlitzWindow = time.Duration(v) * time.Second
	}
	if !IsSupportedLength(p.cfg.DefaultLength) {
		p.cfg.DefaultLength = DefaultLength
	}
	if p.cfg.DefaultTries < minAttempts || p.cfg.DefaultTries > maxAttempts {
		p.cfg.DefaultTries = DefaultMaxAttempts
	}
}

func (p *Plugin) resolveLocation(name string) *time.Location {
	if name == "" {
		return time.Local
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		p.log.Warnf("wordle: 时区 %q 无效，回退本地时区", name)
		return time.Local
	}
	return loc
}

// today 返回配置时区下的当天日期（YYYY-MM-DD），用于每日题选词与每日锁。
func (p *Plugin) today() string {
	loc := p.location
	if loc == nil {
		loc = time.Local
	}
	return time.Now().In(loc).Format("2006-01-02")
}
