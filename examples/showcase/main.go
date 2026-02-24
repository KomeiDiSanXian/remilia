package main

import (
	"fmt"
	"log"
	"time"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/command"
	"github.com/KomeiDiSanXian/remilia/config"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/middleware"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/plugin"
	"github.com/KomeiDiSanXian/remilia/plugins/acl"
	"github.com/KomeiDiSanXian/remilia/plugins/antispam"
	"github.com/KomeiDiSanXian/remilia/plugins/auditlog"
	"github.com/KomeiDiSanXian/remilia/plugins/conversation"
	"github.com/KomeiDiSanXian/remilia/plugins/cooldown"
	"github.com/KomeiDiSanXian/remilia/plugins/core/admin"
	"github.com/KomeiDiSanXian/remilia/plugins/core/cache"
	"github.com/KomeiDiSanXian/remilia/plugins/core/help"
	corepermission "github.com/KomeiDiSanXian/remilia/plugins/core/permission"
	"github.com/KomeiDiSanXian/remilia/plugins/core/storage"
	"github.com/KomeiDiSanXian/remilia/plugins/i18n"
	"github.com/KomeiDiSanXian/remilia/plugins/keywordfilter"
	"github.com/KomeiDiSanXian/remilia/plugins/pluginstore"
	"github.com/KomeiDiSanXian/remilia/plugins/ratelimitui"
	"github.com/KomeiDiSanXian/remilia/plugins/scheduler"
	"github.com/KomeiDiSanXian/remilia/plugins/stats"
	"github.com/KomeiDiSanXian/remilia/plugins/verifycode"
)

func main() {
	cfg, err := config.Load("../../config.yaml")
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if err := logger.Init(logger.Config{
		Level: cfg.Log.Level, Console: true, TimeFormat: "2006-01-02 15:04:05",
	}); err != nil {
		log.Fatalf("init logger: %v", err)
	}
	bot, err := remilia.NewBotBuilder().
		WithBotInfo(&dto.BotInfo{
			QQNum: cfg.Bot.BotID, AppID: cfg.Bot.AppID,
			Token: cfg.Bot.Token, AppSecret: cfg.Bot.Secret,
		}).
		WithWebhook(":8080").WithName("showcase-bot").WithVersion("1.0.0").
		Build()
	if err != nil {
		log.Fatalf("build bot: %v", err)
	}
	eng := bot.Engine()
	eng.Use(middleware.ProductionSet()...)
	eng.Use(requestCounterMiddleware())
	pm := plugin.NewManager(eng)
	pm.SetStrictDeps(true)
	pm.AddListener(&lifecycleLogger{})
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
	if err := pm.RegisterMultipleV2([]*plugin.PluginDescriptor{
		storage.New(),
		corepermission.New(),
		acl.Descriptor(aclPlugin),
		verifycode.New(func(userID, role string) error {
			logger.Infof("[showcase] %s granted role %s via verifycode", userID, role)
			return nil
		}),
		antispam.Descriptor(asPlugin),
		keywordfilter.New(keywordfilter.Config{
			Keywords: []string{"badword"},
			OnMatch: func(ctx *eventctx.Context, matched string) error {
				return replyCtx(ctx, "[blocked: "+matched+"]")
			},
		}),
		cooldown.Descriptor(cdPlugin),
		stats.Descriptor(statsPlugin),
		auditlog.New(),
		scheduler.Descriptor(schedPlugin),
		i18n.New(i18n.Config{DefaultLocale: "zh-CN", LocaleDir: "locales/"}),
		ratelimitui.Descriptor(rlPlugin),
		pluginstore.New(),
		conversation.New(),
		cache.New(),
		help.New(),
		admin.New(),
	}); err != nil {
		log.Fatalf("register plugins: %v", err)
	}
	logger.Infof("[showcase] %d plugins loaded", pm.Count())
	eng.Use(statsPlugin.Middleware())
	if ar, ok := pm.GetContainer().Get("auditlog"); ok {
		eng.Use(ar.(*auditlog.Plugin).Middleware())
	}
	if err := pm.RegisterV2(commandPlugin(pm, cdPlugin, statsPlugin)); err != nil {
		log.Fatalf("register command plugin: %v", err)
	}
	schedPlugin.Every(5*time.Minute, func() {
		n := cdPlugin.CleanExpired(30 * time.Minute)
		if n > 0 {
			logger.Infof("[showcase] cleaned %d cooldown records", n)
		}
	})
	schedPlugin.CronNamed("daily-stats", "0 9 * * *", func() {
		logger.Infof("[showcase] daily top5: %+v", statsPlugin.TopCommands(5))
	})
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
	if psRaw, ok := pm.GetContainer().Get("pluginstore"); ok {
		ps := psRaw.(*pluginstore.Plugin)
		ps.RegisterFunc("showcase-meta",
			func() (any, error) { return map[string]any{"total": statsPlugin.TotalMessages()}, nil },
			func(v any) error { logger.Infof("[pluginstore] restored: %v", v); return nil },
		)
	}
	logger.Info("[showcase] Starting... send /help to see all commands")
	if err := bot.Start(); err != nil {
		logger.WithError(err).Fatal("[showcase] stopped")
	}
}
func commandPlugin(pm *plugin.Manager, cd *cooldown.Plugin, sp *stats.Plugin) *plugin.PluginDescriptor {
	return &plugin.PluginDescriptor{
		Name: "commands", Version: "1.0.0",
		Meta: &plugin.PluginMeta{
			Description: "showcase command set",
			Category:    "showcase",
		},
		Deps: []string{"cooldown", "stats", "i18n", "verifycode", "conversation"},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			// /ping
			ctx.Reg.RegisterCommand(dto.C2CMessageCreate, "/ping").
				SetDefinition(&command.Definition{Name: "ping", Description: "Pong!", Category: "tools"}).
				Handle(func(c *eventctx.Context) error { return replyCtx(c, "Pong!") })
			// /status — stats plugin demo
			ctx.Reg.RegisterCommand(dto.C2CMessageCreate, "/status").
				SetDefinition(&command.Definition{Name: "status", Description: "Bot status", Category: "tools"}).
				Handle(func(c *eventctx.Context) error {
					top := sp.TopCommands(3)
					msg := fmt.Sprintf("total=%d top3:", sp.TotalMessages())
					for _, t := range top {
						msg += fmt.Sprintf(" %s*%d", t.Command, t.Count)
					}
					return replyCtx(c, msg)
				})
			// /daily — cooldown plugin demo (24h)
			ctx.Reg.RegisterCommand(dto.C2CMessageCreate, "/daily").
				SetDefinition(&command.Definition{Name: "daily", Description: "Daily check-in (cooldown demo)", Category: "fun"}).
				Handle(func(c *eventctx.Context) error {
					uid := c.GetUserID()
					if !cd.Allow(uid, "daily", 24*time.Hour) {
						r := cd.Remaining(uid, "daily", 24*time.Hour)
						return replyCtx(c, fmt.Sprintf("cooldown: %s left", r.Round(time.Second)))
					}
					return replyCtx(c, "checked in!")
				})
			// /lang — i18n locale switch
			ctx.Reg.RegisterCommand(dto.C2CMessageCreate, "/lang").
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
			// /greet — i18n template render
			ctx.Reg.RegisterCommand(dto.C2CMessageCreate, "/greet").
				SetDefinition(&command.Definition{Name: "greet", Description: "i18n greeting", Category: "demo"}).
				Handle(func(c *eventctx.Context) error {
					if raw, ok := pm.GetContainer().Get("i18n"); ok {
						t := raw.(*i18n.Plugin)
						return replyCtx(c, t.T(c, "welcome", map[string]any{"name": c.GetUserID()}))
					}
					return replyCtx(c, "hello!")
				})
			// /verify — standalone verifycode plugin
			ctx.Reg.RegisterCommand(dto.C2CMessageCreate, "/verify").
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
			// /register — conversation multi-step flow
			var regMachine *conversation.Machine
			if raw, ok := pm.GetContainer().Get("conversation"); ok {
				conv := raw.(*conversation.Plugin)
				regMachine = conv.NewMachine("register").
					Step("name", "Enter your nickname:", func(c *eventctx.Context, s *conversation.Session) error {
						s.Data["name"] = c.GetMessageContent()
						return nil
					}).
					Step("email", "Enter your email:", func(c *eventctx.Context, s *conversation.Session) error {
						s.Data["email"] = c.GetMessageContent()
						return nil
					}).
					Done(func(c *eventctx.Context, s *conversation.Session) error {
						return replyCtx(c, fmt.Sprintf("registered! name=%v email=%v", s.Data["name"], s.Data["email"]))
					})
				// dispatch handler for in-progress sessions
				ctx.Reg.RegisterMatcher(dto.C2CMessageCreate, conv.InSession("register")).
					Handle(conv.DispatchFor("register"))
			}
			ctx.Reg.RegisterCommand(dto.C2CMessageCreate, "/register").
				SetDefinition(&command.Definition{Name: "register", Description: "Multi-step registration (conversation demo)", Category: "demo"}).
				Handle(func(c *eventctx.Context) error {
					if regMachine == nil {
						return replyCtx(c, "conversation not loaded")
					}
					raw, _ := pm.GetContainer().Get("conversation")
					return raw.(*conversation.Plugin).Start(c, regMachine)
				})
			// /aclcheck — standalone acl plugin
			ctx.Reg.RegisterCommand(dto.C2CMessageCreate, "/aclcheck").
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
			return nil, nil
		},
	}
}
func replyCtx(ctx *eventctx.Context, content string) error {
	msg := &dto.Message{Type: dto.TextMessage, Content: content}
	if ctx.GetEventType() == dto.GroupAtMessageCreate {
		_, err := ctx.ReplyGroup(msg)
		return err
	}
	_, err := ctx.ReplyPrivate(msg)
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

type lifecycleLogger struct{}

func (l *lifecycleLogger) OnPluginLoaded(name string) {
	logger.Infof("[lifecycle] loaded: %s", name)
}
func (l *lifecycleLogger) OnPluginUnloaded(name string) {
	logger.Infof("[lifecycle] unloaded: %s", name)
}
func (l *lifecycleLogger) OnPluginReloaded(name string) {
	logger.Infof("[lifecycle] reloaded: %s", name)
}
func (l *lifecycleLogger) OnPluginError(name, op string, err error) {
	logger.Warnf("[lifecycle] error %s.%s: %v", name, op, err)
}
