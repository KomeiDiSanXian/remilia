package dealornodeal

import (
	"context"
	"fmt"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/i18n"
	"github.com/KomeiDiSanXian/remilia/command"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

const pluginName = "dealornodeal"

// 空闲对局回收时间的允许范围：过短会让进行中的对局被误回收，过长则让
// 已结束的对局长期占用内存。配置越界时按边界值处理并记录告警。
const (
	MinSessionTTL = time.Minute
	MaxSessionTTL = 24 * time.Hour
)

// timeNow 是包级时钟，测试可替换以固定时间。
var timeNow = time.Now

// config 插件运行配置（来自 config.yaml 的 plugins.dealornodeal 段）。
type config struct {
	// DefaultScope 是未显式指定 --scope 时的默认维度。
	DefaultScope Scope
	// Currency 是金额前缀（默认 "$"）；图片只绘制 ASCII，请使用 ASCII 前缀。
	Currency string
	// Cases 是默认箱数（6-30，默认经典 26）。
	Cases int
	// TTL 是空闲对局的内存回收时间。
	TTL time.Duration
}

// Plugin 成交不成交插件实例。
type Plugin struct {
	log      plugin.Logger
	i18n     *i18n.Plugin
	sessions *SessionStore
	cfg      config
}

// New 创建插件描述符。
//
// 依赖 i18n（文案）。本插件不落库：奖金与战绩都只存在于内存对局中。
func New() *plugin.Descriptor {
	p := &Plugin{}
	return &plugin.Descriptor{
		Name:    pluginName,
		Version: "0.1.0",
		Deps:    []string{"i18n"},
		Meta: &plugin.Metadata{
			Author:      "Remilia Community",
			Description: "成交不成交（Deal or No Deal）箱子里猜奖金小游戏 — 26 箱经典奖金表、轮次开箱、银行家期望值报价、最终交换抉择，支持单人独立局与群投票局，全过程配阶段图片",
			Category:    "娱乐",
			Tags:        []string{"deal-or-no-deal", "成交不成交", "开箱", "游戏", "赌博", "投票", "银行家"},
			HelpText: `成交不成交（Deal or No Deal）箱子里猜奖金

用法：
  /dond [--scope user|group] [--cases N]   开始一局（默认单人、26 箱，N 取 6-30）
  /dond pick <箱号|random>     选定自己的箱子
  /dond open <箱号...>         打开一个或多个箱子（可空格分隔，按钮可补全命令）
  /dond deal / nodeal          成交 / 不成交（群维度为投票）
  /dond resolve                群投票局提前结算当前报价（仅发起者）
  /dond swap / keep            仅剩两箱时交换 / 保留自己的箱子
  /dond board                  重新发送当前阶段图片
  /dond rules                  查看玩法说明
  /dond quit                   放弃本局（仅发起者）
  /dond lang <语言>            切换语言（zh-CN / en-US）

玩法：
  26 个箱子各藏一笔虚拟奖金（$1 ~ $1,000,000），开局随机分配。
  先选定自己的箱子，再按轮次打开其余箱子；每轮结束后银行家按剩余
  箱子的期望值报价，你可以成交拿走报价或不成交继续开箱。最后仅剩
  两个箱子时可交换，随后揭晓自己箱子的真实奖金。

维度：
  --scope user   单人局（默认）：只有发起者能操作。
  --scope group  群投票局：群内任何人可开箱；报价采用投票，
                 同意成交的票数超过参与者半数即成交。`,
		},
		Setup:    p.setup,
		Teardown: p.teardown,
	}
}

func (p *Plugin) setup(ctx *plugin.SetupContext) (any, error) {
	p.log = ctx.Log
	p.loadConfig(ctx)
	p.sessions = NewSessionStore(p.cfg.TTL)

	i18nSvc, ok := ctx.TryService[*i18n.Plugin]("i18n")
	if !ok {
		return nil, fmt.Errorf("dealornodeal: i18n 服务不可用")
	}
	p.i18n = i18nSvc
	if !ctx.DryRun {
		if err := registerLocales(i18nSvc); err != nil {
			ctx.Log.Warnf("dealornodeal: 注册语言包失败: %v", err)
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
				p.sessions.Sweep()
			}
		}
	})

	def := command.NewDef("dond").Description("成交不成交（Deal or No Deal）开箱猜奖金").
		SubCommand(command.NewDef("pick").Description("选定自己的箱子").
			Arg("case", "箱号 1-26，或 random", false).Build()).
		SubCommand(command.NewDef("open").Description("打开一个或多个箱子").
			Arg("case", "箱号 1-26，可空格分隔多个，如 1 23 15", false).Build()).
		SubCommand(command.NewDef("deal").Description("成交（群维度为投票成交）").Build()).
		SubCommand(command.NewDef("nodeal").Description("不成交（群维度为投票继续）").Build()).
		SubCommand(command.NewDef("resolve").Description("群投票局提前结算当前报价").Build()).
		SubCommand(command.NewDef("swap").Description("最终抉择：交换另一个箱子").Build()).
		SubCommand(command.NewDef("keep").Description("最终抉择：保留自己的箱子").Build()).
		SubCommand(command.NewDef("board").Description("重新发送当前阶段图片").Build()).
		SubCommand(command.NewDef("rules").Description("查看玩法说明").Build()).
		SubCommand(command.NewDef("quit").Description("放弃本局").Build()).
		SubCommand(command.NewDef("lang").Description("切换语言").
			Arg("locale", "语言代码，如 zh-CN", true).Build()).
		SubCommand(command.NewDef("help").Description("查看帮助").Build()).
		Flag("scope", "s", "维度 user|group（默认 user）", command.ArgTypeString).
		Flag("cases", "c", "箱数 6-30（默认 26）", command.ArgTypeInt).
		Example("/dond").Example("/dond --scope group").
		Example("/dond --cases 16").Example("/dond --cases 30 --scope group").
		Example("/dond pick 13").Example("/dond pick random").
		Example("/dond open 7").Example("/dond open 1 23 15").
		Example("/dond deal").
		Example("/dond nodeal").Example("/dond swap").
		Build()
	ctx.OnCommandDefWith("", "/dond", def, p.handleDond, eventctx.OnMentionedBotOrNoMentions())

	// 按钮回调（Discord/Telegram 等以回调为主；QQ 走指令按钮填充命令，不产生回调）。
	ctx.Reg.RegisterMatcher(string(platform.EventKindInteraction)).Handle(p.handleInteraction)

	return p, nil
}

func (p *Plugin) teardown(_ *plugin.TeardownContext) error {
	return nil
}

func (p *Plugin) loadConfig(ctx *plugin.SetupContext) {
	p.cfg = config{
		DefaultScope: ScopeUser,
		Currency:     "$",
		Cases:        CaseCount,
		TTL:          30 * time.Minute,
	}
	if ctx.Config == nil {
		return
	}
	if v := ctx.Config.GetString("default_scope", ""); v != "" {
		if s, ok := ParseScope(v); ok {
			p.cfg.DefaultScope = s
		}
	}
	if v := ctx.Config.GetString("currency", ""); v != "" {
		p.cfg.Currency = v
	}
	if v := ctx.Config.GetInt("cases", 0); v > 0 {
		p.cfg.Cases = ClampCases(v)
	}
	if v := ctx.Config.GetDuration("session_ttl", 0); v > 0 {
		p.cfg.TTL = clampSessionTTL(v, p.log)
	}
}

// clampSessionTTL 把空闲回收时间钳制到 [MinSessionTTL, MaxSessionTTL]，
// 越界时记录告警并返回边界值。
func clampSessionTTL(d time.Duration, log plugin.Logger) time.Duration {
	switch {
	case d < MinSessionTTL:
		if log != nil {
			log.Warnf("dealornodeal: session_ttl %v 过短，已按 %v 处理", d, MinSessionTTL)
		}
		return MinSessionTTL
	case d > MaxSessionTTL:
		if log != nil {
			log.Warnf("dealornodeal: session_ttl %v 过长，已按 %v 处理", d, MaxSessionTTL)
		}
		return MaxSessionTTL
	default:
		return d
	}
}
