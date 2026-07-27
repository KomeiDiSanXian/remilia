package main

import (
	"context"
	"os"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/acl"
	"github.com/KomeiDiSanXian/remilia/builtin/ai"
	"github.com/KomeiDiSanXian/remilia/builtin/antispam"
	"github.com/KomeiDiSanXian/remilia/builtin/auditlog"
	"github.com/KomeiDiSanXian/remilia/builtin/autoresponder"
	"github.com/KomeiDiSanXian/remilia/builtin/cooldown"
	"github.com/KomeiDiSanXian/remilia/builtin/core/admin"
	"github.com/KomeiDiSanXian/remilia/builtin/core/help"
	"github.com/KomeiDiSanXian/remilia/builtin/core/permission"
	"github.com/KomeiDiSanXian/remilia/builtin/customcommands"
	"github.com/KomeiDiSanXian/remilia/builtin/dev/debug"
	"github.com/KomeiDiSanXian/remilia/builtin/job"
	"github.com/KomeiDiSanXian/remilia/builtin/keywordfilter"
	"github.com/KomeiDiSanXian/remilia/builtin/messagelog"
	"github.com/KomeiDiSanXian/remilia/builtin/moderation"
	"github.com/KomeiDiSanXian/remilia/builtin/ping"
	"github.com/KomeiDiSanXian/remilia/builtin/pluginctrl"
	"github.com/KomeiDiSanXian/remilia/builtin/pluginstore"
	"github.com/KomeiDiSanXian/remilia/builtin/ratelimitui"
	"github.com/KomeiDiSanXian/remilia/builtin/scheduler"
	"github.com/KomeiDiSanXian/remilia/builtin/sendqueue"
	"github.com/KomeiDiSanXian/remilia/builtin/stats"
	builtinstorage "github.com/KomeiDiSanXian/remilia/builtin/storage"
	subscriptionpkg "github.com/KomeiDiSanXian/remilia/builtin/subscription"
	"github.com/KomeiDiSanXian/remilia/builtin/verifycode"
	"github.com/KomeiDiSanXian/remilia/builtin/vevent"
	"github.com/KomeiDiSanXian/remilia/builtin/welcome"
	"github.com/KomeiDiSanXian/remilia/cmd/bot/plugins/anime"
	"github.com/KomeiDiSanXian/remilia/cmd/bot/plugins/bilibili"
	"github.com/KomeiDiSanXian/remilia/cmd/bot/plugins/css"
	"github.com/KomeiDiSanXian/remilia/cmd/bot/plugins/fortune"
	"github.com/KomeiDiSanXian/remilia/cmd/bot/plugins/genshin"
	"github.com/KomeiDiSanXian/remilia/cmd/bot/plugins/iss"
	"github.com/KomeiDiSanXian/remilia/cmd/bot/plugins/minecraft"
	"github.com/KomeiDiSanXian/remilia/cmd/bot/plugins/rpg/coc"
	"github.com/KomeiDiSanXian/remilia/cmd/bot/plugins/rpg/dice"
	"github.com/KomeiDiSanXian/remilia/cmd/bot/plugins/rpg/dnd"
	"github.com/KomeiDiSanXian/remilia/cmd/bot/plugins/sauce"
	"github.com/KomeiDiSanXian/remilia/cmd/bot/plugins/starrail"
	"github.com/KomeiDiSanXian/remilia/cmd/bot/plugins/weather"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	infrastorage "github.com/KomeiDiSanXian/remilia/infra/storage"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

const dataDir = "data"

func setupPlugins(pm *plugin.Manager, eng *engine.Engine) {
	for _, dir := range []string{dataDir, dataDir + "/db"} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			logger.WithError(err).Fatalf("[remilia] Failed to create directory: %s", dir)
		}
	}

	asPlugin := antispam.NewPlugin(antispam.DefaultConfig(), antispam.WithStore(dataDir+"/antispam"))
	cdPlugin := cooldown.NewPlugin()
	sp := stats.NewPlugin(stats.WithStore(dataDir + "/stats"))
	schedPlugin := scheduler.NewPlugin()
	aclPlugin := acl.NewPlugin(acl.WithStore(dataDir + "/acl"))
	rlPlugin := ratelimitui.NewPlugin()
	rlPlugin.BindAntispam(asPlugin)
	rlPlugin.BindCooldown(cdPlugin)
	subPlugin := subscriptionpkg.NewPlugin(
		subscriptionpkg.WithPollInterval(5*time.Minute),
		subscriptionpkg.WithStore(dataDir+"/subscription"),
	)

	storageDSN := dataDir + "/db/bot.db"

	descriptors := []*plugin.Descriptor{
		pluginctrl.New(),
		permission.New(),
		aclPlugin.Descriptor(),
		help.New(),
		welcome.New(welcome.WithStore(dataDir + "/welcome")),
		autoresponder.New(
			autoresponder.WithStore(dataDir+"/autoresponder"),
			autoresponder.WithPrefix("/"),
		),
		customcommands.New(customcommands.WithStore(dataDir + "/customcommands")),
		moderation.New(moderation.WithStore(dataDir + "/moderation")),
		admin.New(),
		debug.New(),
		verifycode.New(func(userID, role string) error {
			logger.Infof("[remilia] User %s granted role %s via verifycode", userID, role)
			return nil
		}, verifycode.WithStore(dataDir+"/verifycode")),
		asPlugin.Descriptor(),
		keywordfilter.New(keywordfilter.Config{
			OnMatch: func(ctx *eventctx.Context, matched string) error {
				logger.Warnf("[remilia] Keyword matched: %q from user %s", matched, ctx.GetUserID())
				return nil
			},
		}, keywordfilter.WithStore(dataDir+"/keywordfilter")),
		cdPlugin.Descriptor(),
		sp.Descriptor(),
		auditlog.New(),
		schedPlugin.Descriptor(),
		rlPlugin.Descriptor(),
		pluginstore.New(),
		builtinstorage.New(infrastorage.WithDSN(storageDSN)),
		sendqueue.New(sendqueue.DefaultConfig()),
		subPlugin.Descriptor(),
		job.New(),
		vevent.New(eng),
		ping.New(),
		ai.New(eng),
		weather.New(),
		iss.New(iss.WithDataDir(dataDir + "/iss")),
		css.New(css.WithDataDir(dataDir + "/css")),
		bilibili.New(),
		anime.New(),
		fortune.New(fortune.WithDataDir(dataDir + "/fortune")),
		minecraft.New(),
		genshin.New(),
		starrail.New(),
		sauce.New(),
		dice.New(),
		coc.New(),
		dnd.New(),
	}

	if err := pm.RegisterBatch(context.Background(), descriptors, plugin.WithInferDeps()); err != nil {
		logger.WithError(err).Fatal("[remilia] Failed to register plugins")
	}
	logger.Infof("[remilia] %d plugins loaded", pm.Count())
	pm.FreezeContainer()

	eng.Use(sp.Middleware())
	if ar, ok := pm.GetContainer().Get("auditlog"); ok {
		eng.Use(ar.(*auditlog.Plugin).Middleware())
	}

	mlDB, err := messagelog.OpenDB(dataDir + "/db/messagelog.db")
	if err != nil {
		logger.WithError(err).Warn("[remilia] Failed to open messagelog DB, message history disabled")
	} else {
		messagelog.Default().UseDB(mlDB)
		messagelog.Default().Start()
		eng.Use(messagelog.MessageLogger())
		logger.Info("[remilia] MessageLogger middleware enabled")
	}
}
