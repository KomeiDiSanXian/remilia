package main

import (
	"context"
	"embed"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/builtin/acl"
	"github.com/KomeiDiSanXian/remilia/builtin/antispam"
	"github.com/KomeiDiSanXian/remilia/builtin/auditlog"
	"github.com/KomeiDiSanXian/remilia/builtin/broadcast"
	"github.com/KomeiDiSanXian/remilia/builtin/cooldown"
	"github.com/KomeiDiSanXian/remilia/builtin/core/admin"
	"github.com/KomeiDiSanXian/remilia/builtin/core/help"
	"github.com/KomeiDiSanXian/remilia/builtin/core/permission"
	"github.com/KomeiDiSanXian/remilia/builtin/dev/debug"
	"github.com/KomeiDiSanXian/remilia/builtin/i18n"
	"github.com/KomeiDiSanXian/remilia/builtin/job"
	"github.com/KomeiDiSanXian/remilia/builtin/keywordfilter"
	"github.com/KomeiDiSanXian/remilia/builtin/pluginctrl"
	"github.com/KomeiDiSanXian/remilia/builtin/pluginstore"
	"github.com/KomeiDiSanXian/remilia/builtin/ratelimitui"
	"github.com/KomeiDiSanXian/remilia/builtin/scheduler"
	"github.com/KomeiDiSanXian/remilia/builtin/sendqueue"
	"github.com/KomeiDiSanXian/remilia/builtin/stats"
	builtinstorage "github.com/KomeiDiSanXian/remilia/builtin/storage"
	"github.com/KomeiDiSanXian/remilia/builtin/subscription"
	"github.com/KomeiDiSanXian/remilia/builtin/verifycode"
	"github.com/KomeiDiSanXian/remilia/builtin/vevent"
	"github.com/KomeiDiSanXian/remilia/command"
	"github.com/KomeiDiSanXian/remilia/config"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/core/fsm"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	infrastorage "github.com/KomeiDiSanXian/remilia/infra/storage"
	"github.com/KomeiDiSanXian/remilia/middleware"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/plugin"
	"github.com/KomeiDiSanXian/remilia/router"
)

//go:embed locales/*.yaml
var localeFS embed.FS

func main() {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if err := logger.Init(logger.Config{
		Level: cfg.Log.Level, Console: true, TimeFormat: "2006-01-02 15:04:05",
	}); err != nil {
		log.Fatalf("init logger: %v", err)
	}

	// ── pprof ──────────────────────────────────────────────────────────────
	// 访问 http://localhost:9001/debug/pprof/ 查看各类 profile
	// 访问 http://localhost:9001/debug/pprof/stats 查看运行时内存统计
	// 访问 http://localhost:9001/debug/pprof/snapshot 立即触发一次快照
	pprofSrv := remilia.NewPprofServer(remilia.PprofConfig{
		Enabled:              true,
		Addr:                 "localhost:9001",
		AutoProfile:          true,
		ProfileInterval:      30 * time.Minute,
		ProfileDuration:      30 * time.Second,
		OutputDir:            "./profiles",
		EnableMutex:          true,
		EnableBlock:          true,
		MutexProfileFraction: 1,
		BlockProfileRate:     1,
	})
	if err := pprofSrv.Start(); err != nil {
		log.Fatalf("start pprof: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = pprofSrv.Stop(ctx)
	}()

	bot, err := remilia.NewBotBuilder().
		WithPlatformAdapter(qq.NewWebhookServerAdapter(":9000", &dto.BotInfo{
			QQNum: cfg.Bot.QQ.BotID, AppID: cfg.Bot.QQ.AppID,
			Token: cfg.Bot.QQ.Token, AppSecret: cfg.Bot.QQ.Secret,
		})).
		WithName("showcase-bot").WithVersion("1.0.0").
		Build()
	if err != nil {
		log.Fatalf("build bot: %v", err)
	}
	eng := bot.Engine()
	eng.Use(middleware.ProductionSet()...)
	eng.Use(requestCounterMiddleware())
	eng.Use(processingTimeMiddleware())

	// ── FSM + Router ──────────────────────────────────────────────────────────
	fsmMgr := fsm.NewManager(nil)
	rtr := router.New(eng, fsmMgr.Engine())
	// FSM 优先：活跃会话或匹配启动事件的消息优先由 FSM 处理，
	// 不命中时自动 fallthrough 到 WithCommandPrefix。
	rtr.Route(router.WithFSMRoute())
	rtr.Route(router.WithCommandPrefix())
	bot.UseRouter(rtr)

	// ── Per-Channel Engine ────────────────────────────────────────────────────
	engMgr := engine.NewEngineManager(eng)
	bot.UseEngineManager(engMgr)
	rtr.WithEngineManager(engMgr)

	pm := plugin.NewManager(eng)
	pm.SetStrictDeps(false)
	pm.AddListener(&lifecycleLogger{})

	// ── 插件实例 ────────────────────────────────────────────────────────────
	asPlugin := antispam.NewPlugin(antispam.Config{
		UserRate: 5, UserBurst: 10, GroupRate: 30, GroupBurst: 50,
		BanOnViolation: true, BanDuration: 5 * time.Minute,
	})
	cdPlugin := cooldown.NewPlugin()
	statsPlugin := stats.NewPlugin()
	schedPlugin := scheduler.NewPlugin()
	aclPlugin := acl.NewPlugin()
	rlPlugin := ratelimitui.NewPlugin()
	rlPlugin.BindAntispam(asPlugin)
	rlPlugin.BindCooldown(cdPlugin)
	i18nPlugin := i18n.NewPlugin(i18n.Config{DefaultLocale: "zh-CN"})

	// 确保数据目录存在（SQLite 不能自动创建父目录）
	if err := os.MkdirAll("data", 0755); err != nil {
		log.Fatalf("create data dir: %v", err)
	}

	// 广播插件（内存订阅模式，可改为 WithDataFile 持久化）
	bcPlugin := broadcast.NewPlugin(broadcast.DefaultConfig())

	// 发送队列插件
	sqPlugin := sendqueue.New(sendqueue.DefaultConfig())

	// 订阅框架
	subPlugin := subscription.NewPlugin(
		subscription.WithPollInterval(5*time.Minute),
		subscription.WithDispatcher(func(ctx context.Context, target subscription.Target, item subscription.Item) error {
			logger.Infof("[subscription] dispatch to %s: %s", target.ChatID, item.Title)
			// 实际环境中在此通过 sender 推送给目标会话
			return nil
		}),
	)
	// 注册一个示例数据源（每次 Poll 返回带时间戳的条目，仅用于演示）
	subPlugin.RegisterSource(&demoSource{})

	if err := pm.RegisterMultiple([]*plugin.Descriptor{
		pluginctrl.New(),
		permission.New(),
		aclPlugin.Descriptor(),
		verifycode.New(func(userID, role string) error {
			logger.Infof("[showcase] %s granted role %s via verifycode", userID, role)
			return nil
		}),
		asPlugin.Descriptor(),
		keywordfilter.New(keywordfilter.Config{
			Keywords: []string{"badword"},
			OnMatch: func(ctx *eventctx.Context, matched string) error {
				return replyCtx(ctx, "[blocked: "+matched+"]")
			},
		}),
		cdPlugin.Descriptor(),
		statsPlugin.Descriptor(),
		auditlog.New(),
		schedPlugin.Descriptor(),
		i18nPlugin.Descriptor(),
		rlPlugin.Descriptor(),
		pluginstore.New(),
		help.New(),
		admin.New(),
		builtinstorage.New(infrastorage.WithDSN("data/showcase.db")),
		bcPlugin.Descriptor(),
		sqPlugin,
		subPlugin.Descriptor(),
		job.New(),
		debug.New(),
		vevent.New(eng),
	}); err != nil {
		log.Fatalf("register plugins: %v", err)
	}
	logger.Infof("[showcase] %d plugins loaded", pm.Count())

	// 加载 locale 文件
	if entries, err := localeFS.ReadDir("locales"); err == nil {
		for _, e := range entries {
			name := e.Name()
			if strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml") {
				locale := strings.TrimSuffix(strings.TrimSuffix(name, ".yaml"), ".yml")
				if data, readErr := localeFS.ReadFile("locales/" + name); readErr == nil {
					_ = i18nPlugin.LoadBytes(locale, data)
				}
			}
		}
	}

	eng.Use(statsPlugin.Middleware())
	if ar, ok := pm.GetContainer().Get("auditlog"); ok {
		eng.Use(ar.(*auditlog.Plugin).Middleware())
	}

	if err := pm.Register(commandPlugin(pm, cdPlugin, statsPlugin, bcPlugin, subPlugin)); err != nil {
		log.Fatalf("register command plugin: %v", err)
	}

	// ── FSM 多步骤表单 ───────────────────────────────────────────────────────
	// FSM 定义自包含：启动事件（idle→ask_name）由 Router 的 TryStartSession 自动检测。
	// 开发者无需手动注册 Engine 命令或管理 fsm.Manager 引用。
	signupFSM := &fsm.FSM{
		Name: "signup", Initial: "idle",
		Events: []fsm.Event{
			// cancel 放首位：通配符 From:* 必须优先于具体状态事件，
			// 否则 "input_name"/"input_age" 的 Match(TrimSpace != "")
			// 会在其他状态优先匹配，导致 cancel 永远无法触发。
			{Name: "cancel", From: "*", To: "idle",
				Match: func(ctx *eventctx.Context) bool {
					return strings.TrimSpace(ctx.GetMessageContent()) == "/cancel"
				},
				Action: func(ctx *fsm.FSMContext) error {
					_, e := ctx.Reply(platform.TextMessage("已取消注册"))
					fsmMgr.Engine().EndSession(ctx.SessionID)
					return e
				}},
			{Name: "start", From: "idle", To: "ask_name",
				Match: func(ctx *eventctx.Context) bool {
					return strings.TrimSpace(ctx.GetMessageContent()) == "/fsmsignup"
				},
				Action: func(ctx *fsm.FSMContext) error {
					_, e := ctx.Reply(platform.TextMessage("欢迎注册！请输入您的昵称："))
					return e
				}},
			{Name: "input_name", From: "ask_name", To: "ask_age",
				Match: func(ctx *eventctx.Context) bool { return strings.TrimSpace(ctx.GetMessageContent()) != "" },
				Action: func(ctx *fsm.FSMContext) error {
					ctx.Data["name"] = strings.TrimSpace(ctx.GetMessageContent())
					_, e := ctx.Reply(platform.TextMessage(fmt.Sprintf("你好 %s！请输入年龄：", ctx.Data["name"])))
					return e
				}},
			{Name: "input_age", From: "ask_age",
				// To 为空表示终态：Action 执行后自动结束会话
				Match: func(ctx *eventctx.Context) bool { return strings.TrimSpace(ctx.GetMessageContent()) != "" },
				Action: func(ctx *fsm.FSMContext) error {
					ctx.Data["age"] = strings.TrimSpace(ctx.GetMessageContent())
					_, e := ctx.Reply(platform.TextMessage(fmt.Sprintf("注册成功！昵称：%s，年龄：%s", ctx.Data["name"], ctx.Data["age"])))
					ctx.EndSession()
					return e
				}},
			{Name: "cancel", From: "*",
				// To 为空表示终态：Action 执行后自动结束会话
				Match: func(ctx *eventctx.Context) bool {
					return strings.TrimSpace(ctx.GetMessageContent()) == "/cancel"
				},
				Action: func(ctx *fsm.FSMContext) error {
					_, e := ctx.Reply(platform.TextMessage("已取消注册"))
					ctx.EndSession()
					return e
				}},
		},
		Timeout: 3 * time.Minute,
	}
	if err := fsmMgr.Register(&fsm.FSMDescriptor{Name: "signup", FSM: signupFSM}); err != nil {
		log.Fatalf("register signup FSM: %v", err)
	}

	// 定时清理冷却记录
	schedPlugin.Every(5*time.Minute, func() {
		n := cdPlugin.CleanExpired(30 * time.Minute)
		if n > 0 {
			logger.Infof("[showcase] cleaned %d cooldown records", n)
		}
	})
	// 每日 09:00 输出 top5 命令统计
	schedPlugin.CronNamed("daily-stats", "0 0 9 * * *", func() {
		logger.Infof("[showcase] daily top5: %+v", statsPlugin.TopCommands(5))
	})

	// 事件总线订阅
	bus := pm.GetEventBus()
	if _, err := bus.Subscribe("permission.granted", func(data any) {
		logger.Infof("[eventbus] permission.granted: %v", data)
	}); err != nil {
		logger.WithError(err).Warn("subscribe failed")
	}
	if _, err := bus.SubscribeAll(func(data any) {
		logger.Debugf("[eventbus] wildcard: %v", data)
	}); err != nil {
		logger.WithError(err).Warn("SubscribeAll failed")
	}

	// pluginstore 元数据持久化
	if psRaw, ok := pm.GetContainer().Get("pluginstore"); ok {
		ps := psRaw.(*pluginstore.Plugin)
		ps.RegisterFunc("showcase-meta",
			func() (any, error) { return map[string]any{"total": statsPlugin.TotalMessages()}, nil },
			func(v any) error { logger.Infof("[pluginstore] restored: %v", v); return nil },
		)
	}

	logger.Info("[showcase] Starting... send /help to see all commands")
	logger.Info("[showcase] pprof available at http://localhost:9001/debug/pprof/")
	if err := bot.Start(); err != nil {
		logger.WithError(err).Fatal("[showcase] stopped")
	}
	bot.WaitForShutdown()
}

// ── 命令插件 ──────────────────────────────────────────────────────────────────

func commandPlugin(
	pm *plugin.Manager,
	cd *cooldown.Plugin,
	sp *stats.Plugin,
	bc *broadcast.Plugin,
	subHandle *subscription.PluginHandle,
) *plugin.Descriptor {
	return &plugin.Descriptor{
		Name: "commands", Version: "1.0.0",
		Meta: &plugin.Metadata{
			Description: "showcase command set",
			Category:    "showcase",
		},
		Deps: []string{
			"cooldown", "stats", "i18n", "verifycode",
			"broadcast", "sendqueue", "subscription", "job",
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			// ── /ping ──────────────────────────────────────────────────────
			ctx.Reg.RegisterCommand("", "/ping").
				SetDefinition(&command.Definition{Name: "ping", Description: "Pong!", Category: "tools"}).
				Handle(func(c *eventctx.Context) error { return replyCtx(c, "Pong!") })

			// ── /status ────────────────────────────────────────────────────
			ctx.Reg.RegisterCommand("", "/status").
				SetDefinition(&command.Definition{Name: "status", Description: "Bot status", Category: "tools"}).
				Handle(func(c *eventctx.Context) error {
					top := sp.TopCommands(3)
					var msg strings.Builder
					msg.WriteString(fmt.Sprintf("total=%d top3:", sp.TotalMessages()))
					for _, t := range top {
						msg.WriteString(fmt.Sprintf(" %s*%d", t.Command, t.Count))
					}
					return replyCtx(c, msg.String())
				})

			// ── /daily ─────────────────────────────────────────────────────
			ctx.Reg.RegisterCommand("", "/daily").
				SetDefinition(&command.Definition{Name: "daily", Description: "Daily check-in (cooldown demo)", Category: "fun"}).
				Handle(func(c *eventctx.Context) error {
					uid := c.GetUserID()
					if !cd.Allow(uid, "daily", 24*time.Hour) {
						r := cd.Remaining(uid, "daily", 24*time.Hour)
						return replyCtx(c, fmt.Sprintf("cooldown: %s left", r.Round(time.Second)))
					}
					return replyCtx(c, "checked in!")
				})

			// ── /lang ──────────────────────────────────────────────────────
			ctx.Reg.RegisterCommand("", "/lang").
				SetDefinition(&command.Definition{
					Name: "lang", Description: "Switch language (i18n)", Category: "settings",
					Arguments: []*command.Argument{{Name: "locale", Type: command.ArgTypeString, Required: true}},
					Examples:  []string{"/lang zh-CN", "/lang en"},
				}).
				Handle(func(c *eventctx.Context) error {
					args, _ := command.ParseCommandLine(c.GetMessageContent())
					locale := args.Get(0)
					if locale == "" {
						return replyCtx(c, "usage: /lang <zh-CN|en>")
					}
					if raw, ok := pm.GetContainer().Get("i18n"); ok {
						raw.(*i18n.Plugin).SetLocale(c, locale)
					}
					return replyCtx(c, "language set: "+locale)
				})

			// ── /greet ─────────────────────────────────────────────────────
			ctx.Reg.RegisterCommand("", "/greet").
				SetDefinition(&command.Definition{Name: "greet", Description: "i18n greeting", Category: "demo"}).
				Handle(func(c *eventctx.Context) error {
					if raw, ok := pm.GetContainer().Get("i18n"); ok {
						t := raw.(*i18n.Plugin)
						return replyCtx(c, t.T(c, "welcome", map[string]any{"name": c.GetUserID()}))
					}
					return replyCtx(c, "hello!")
				})

			// ── /verify ────────────────────────────────────────────────────
			ctx.Reg.RegisterCommand("", "/verify").
				SetDefinition(&command.Definition{
					Name: "verify", Description: "Redeem a verification code", Category: "access",
					Arguments: []*command.Argument{{Name: "code", Type: command.ArgTypeString, Required: true}},
				}).
				Handle(func(c *eventctx.Context) error {
					args, _ := command.ParseCommandLine(c.GetMessageContent())
					code := args.Get(0)
					if code == "" {
						return replyCtx(c, "usage: /verify <code>")
					}
					raw, ok := pm.GetContainer().Get("verifycode")
					if !ok {
						return replyCtx(c, "verifycode not loaded")
					}
					role, err := raw.(*verifycode.Plugin).Verify(c.GetUserID(), code)
					if err != nil {
						return replyCtx(c, "failed: "+err.Error())
					}
					return replyCtx(c, "role granted: "+role)
				})

			// ── /aclcheck ──────────────────────────────────────────────────
			ctx.Reg.RegisterCommand("", "/aclcheck").
				SetDefinition(&command.Definition{Name: "aclcheck", Description: "Check ACL status (acl plugin)", Category: "security"}).
				Handle(func(c *eventctx.Context) error {
					raw, ok := pm.GetContainer().Get("acl")
					if !ok {
						return replyCtx(c, "acl not loaded")
					}
					p := raw.(*acl.Plugin)
					allowed := p.IsAllowed(c.GetUserID())
					return replyCtx(c, fmt.Sprintf("mode=%s user=%s allowed=%v", p.GetMode(), c.GetUserID(), allowed))
				})

			// ── /broadcast ─────────────────────────────────────────────────
			// 演示广播插件：向当前会话（作为目标列表）广播一条通知
			ctx.Reg.RegisterCommand("", "/broadcast").
				SetDefinition(&command.Definition{
					Name:        "broadcast",
					Description: "广播消息演示（broadcast 插件）",
					Category:    "demo",
					Arguments:   []*command.Argument{{Name: "msg", Type: command.ArgTypeString, Required: true}},
					Examples:    []string{"/broadcast 公告内容"},
				}).
				Handle(func(c *eventctx.Context) error {
					args, _ := command.ParseCommandLine(c.GetMessageContent())
					text := args.Get(0)
					if text == "" {
						return replyCtx(c, "usage: /broadcast <消息>")
					}
					// 将当前发送器注入广播插件，再广播给订阅群列表
					bc.SetSender(c.GetPlatformSender())
					groups := bc.ListGroupSubscribers()
					if len(groups) == 0 {
						return replyCtx(c, "暂无订阅群，使用 /bcsub 先订阅当前群")
					}
					result := bc.BroadcastToGroupsWithContext(c.Context(), groups, platform.TextMessage(text))
					return replyCtx(c, fmt.Sprintf("广播完成: total=%d success=%d failed=%d",
						result.Total, result.Success, result.Failed))
				})

			// ── /bcsub / /bcunsub ──────────────────────────────────────────
			ctx.Reg.RegisterCommand("", "/bcsub").
				SetDefinition(&command.Definition{
					Name: "bcsub", Description: "订阅当前群到广播列表", Category: "demo",
				}).
				Handle(func(c *eventctx.Context) error {
					groupID := c.GetMessageContent() // 实际应从事件获取群 ID
					if groupID == "" {
						groupID = c.GetUserID() + "_group"
					}
					bc.SubscribeGroup(groupID)
					return replyCtx(c, "已订阅群: "+groupID)
				})
			ctx.Reg.RegisterCommand("", "/bcunsub").
				SetDefinition(&command.Definition{
					Name: "bcunsub", Description: "从广播列表取消订阅当前群", Category: "demo",
				}).
				Handle(func(c *eventctx.Context) error {
					groupID := c.GetUserID() + "_group"
					bc.UnsubscribeGroup(groupID)
					return replyCtx(c, "已取消订阅群: "+groupID)
				})

			// ── /enqueue ───────────────────────────────────────────────────
			// 演示 sendqueue：将消息加入异步发送队列
			ctx.Reg.RegisterCommand("", "/enqueue").
				SetDefinition(&command.Definition{
					Name:        "enqueue",
					Description: "异步消息队列演示（sendqueue 插件）",
					Category:    "demo",
					Arguments:   []*command.Argument{{Name: "msg", Type: command.ArgTypeString, Required: true}},
					Examples:    []string{"/enqueue 异步消息内容"},
				}).
				Handle(func(c *eventctx.Context) error {
					args, _ := command.ParseCommandLine(c.GetMessageContent())
					text := args.Get(0)
					if text == "" {
						return replyCtx(c, "usage: /enqueue <消息>")
					}
					raw, ok := pm.GetContainer().Get("sendqueue")
					if !ok {
						return replyCtx(c, "sendqueue not loaded")
					}
					sq := raw.(*sendqueue.Plugin)
					sq.SetDefaultSender(c.GetPlatformSender())
					chat := platform.ChatInfo{ID: c.GetUserID(), IsGroup: false}
					if err := sq.Enqueue(chat, platform.TextMessage("[async] "+text), nil); err != nil {
						return replyCtx(c, "入队失败: "+err.Error())
					}
					return replyCtx(c, "消息已加入发送队列")
				})

			// ── /subscribe / /unsubscribe ──────────────────────────────────
			// 演示 subscription：订阅 demo 数据源
			ctx.Reg.RegisterCommand("", "/subscribe").
				SetDefinition(&command.Definition{
					Name:        "subscribe",
					Description: "订阅 demo 数据源（subscription 插件）",
					Category:    "demo",
					Examples:    []string{"/subscribe"},
				}).
				Handle(func(c *eventctx.Context) error {
					mgr := subHandle.Manager()
					id, err := mgr.Subscribe("demo", "showcase",
						subscription.Target{ChatID: c.GetUserID(), IsGroup: false})
					if err != nil {
						return replyCtx(c, "订阅失败: "+err.Error())
					}
					return replyCtx(c, "订阅成功，id="+id)
				})

			ctx.Reg.RegisterCommand("", "/unsubscribe").
				SetDefinition(&command.Definition{
					Name:        "unsubscribe",
					Description: "取消 demo 数据源订阅（subscription 插件）",
					Category:    "demo",
					Arguments:   []*command.Argument{{Name: "id", Type: command.ArgTypeString, Required: true}},
					Examples:    []string{"/unsubscribe sub-xxx"},
				}).
				Handle(func(c *eventctx.Context) error {
					args, _ := command.ParseCommandLine(c.GetMessageContent())
					id := args.Get(0)
					if id == "" {
						return replyCtx(c, "usage: /unsubscribe <id>")
					}
					mgr := subHandle.Manager()
					if err := mgr.Unsubscribe(id); err != nil {
						return replyCtx(c, "取消失败: "+err.Error())
					}
					return replyCtx(c, "已取消订阅 id="+id)
				})

			// ── /mysubs ────────────────────────────────────────────────────
			ctx.Reg.RegisterCommand("", "/mysubs").
				SetDefinition(&command.Definition{
					Name: "mysubs", Description: "查看我的订阅列表", Category: "demo",
				}).
				Handle(func(c *eventctx.Context) error {
					mgr := subHandle.Manager()
					subs := mgr.ListSubscriptions(c.GetUserID())
					if len(subs) == 0 {
						return replyCtx(c, "暂无订阅，使用 /subscribe 添加")
					}
					var sb strings.Builder
					for _, s := range subs {
						sb.WriteString(fmt.Sprintf("id=%s source=%s param=%s\n", s.ID, s.SourceName, s.Param))
					}
					return replyCtx(c, sb.String())
				})

			// ── /runjob ────────────────────────────────────────────────────
			// 演示 job 插件：提交一个带延迟的后台作业
			ctx.Reg.RegisterCommand("", "/runjob").
				SetDefinition(&command.Definition{
					Name:        "runjob",
					Description: "提交后台作业演示（job 插件）",
					Category:    "demo",
					Examples:    []string{"/runjob"},
				}).
				Handle(func(c *eventctx.Context) error {
					raw, ok := pm.GetContainer().Get("job")
					if !ok {
						return replyCtx(c, "job plugin not loaded")
					}
					runner := raw.(*job.Plugin)
					uid := c.GetUserID()
					jid := runner.Once("showcase-task",
						func(jctx context.Context) error {
							logger.Infof("[job] showcase-task for user=%s done", uid)
							return nil
						},
						job.WithDelay(3*time.Second),
						job.WithOnDone(func(info job.Info) {
							logger.Infof("[job] %s finished: status=%s attempts=%d",
								info.Name, info.Status, info.Attempts)
						}),
					)
					return replyCtx(c, fmt.Sprintf("作业已提交: id=%s (3s 后执行)", jid))
				})

			// ── /jobretrydemo ──────────────────────────────────────────────
			// 演示带重试的 job
			ctx.Reg.RegisterCommand("", "/jobretrydemo").
				SetDefinition(&command.Definition{
					Name:        "jobretrydemo",
					Description: "提交带重试的后台作业（job 插件）",
					Category:    "demo",
				}).
				Handle(func(c *eventctx.Context) error {
					raw, ok := pm.GetContainer().Get("job")
					if !ok {
						return replyCtx(c, "job plugin not loaded")
					}
					runner := raw.(*job.Plugin)
					attempt := 0
					jid := runner.Retry("retry-demo",
						func(jctx context.Context) error {
							attempt++
							if attempt < 3 {
								return fmt.Errorf("simulated failure (attempt %d)", attempt)
							}
							return nil
						},
						job.WithMaxRetries(3),
						job.WithExponentialBackoff(200*time.Millisecond, 2*time.Second),
					)
					return replyCtx(c, fmt.Sprintf("重试作业已提交: id=%s", jid))
				})

			return nil, nil
		},
	}
}

// ── 辅助函数 ──────────────────────────────────────────────────────────────────

func replyCtx(ctx *eventctx.Context, content string) error {
	_, err := ctx.Reply(platform.TextMessage(content))
	return err
}

func requestCounterMiddleware() eventctx.Middleware {
	var count int64
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			count++
			logger.Debugf("[showcase] req #%d", count)
			return next(ctx)
		}
	}
}

func processingTimeMiddleware() eventctx.Middleware {
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			start := time.Now()
			err := next(ctx)
			elapsed := time.Since(start)
			cmd := ctx.GetMessageContent()
			if len(cmd) > 30 {
				cmd = cmd[:30] + "..."
			}
			logger.Infof("[perf] user=%s cmd=%q total=%v",
				ctx.GetUserID(), cmd, elapsed)
			return err
		}
	}
}

type lifecycleLogger struct{}

func (l *lifecycleLogger) OnPluginLoaded(name string) {
	logger.Infof("[Lifecycle] loaded: %s", name)
}
func (l *lifecycleLogger) OnPluginUnloaded(name string) {
	logger.Infof("[Lifecycle] unloaded: %s", name)
}
func (l *lifecycleLogger) OnPluginReloaded(name string) {
	logger.Infof("[Lifecycle] reloaded: %s", name)
}
func (l *lifecycleLogger) OnPluginError(name, op string, err error) {
	logger.Warnf("[Lifecycle] error %s.%s: %v", name, op, err)
}

// ── demoSource：subscription 框架的示例数据源 ─────────────────────────────────

// demoSource 演示数据源：每次 Poll 返回一条带当前时间戳的条目，用于展示推送订阅框架。
type demoSource struct{}

func (s *demoSource) Name() string { return "demo" }
func (s *demoSource) Description() string {
	return "示例数据源（每次 Poll 返回当前时间戳）"
}
func (s *demoSource) Poll(_ context.Context, param string) ([]subscription.Item, error) {
	return []subscription.Item{{
		ID:    fmt.Sprintf("demo-%d", time.Now().UnixNano()),
		Title: fmt.Sprintf("[demo:%s] 推送时间：%s", param, time.Now().Format("15:04:05")),
		Body:  "这是一条来自 demo 数据源的测试推送内容。",
	}}, nil
}
func (s *demoSource) ValidateParam(param string) error {
	if param == "" {
		return fmt.Errorf("demo source: param must not be empty")
	}
	return nil
}
