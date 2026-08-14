package main

import (
	"context"
	"os"
	"time"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/builtin/about"
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
	"github.com/KomeiDiSanXian/remilia/config"

	"github.com/KomeiDiSanXian/remilia/cmd/bot/plugins/anime"
	"github.com/KomeiDiSanXian/remilia/cmd/bot/plugins/bilibili"
	"github.com/KomeiDiSanXian/remilia/cmd/bot/plugins/css"
	"github.com/KomeiDiSanXian/remilia/cmd/bot/plugins/fortune"
	"github.com/KomeiDiSanXian/remilia/cmd/bot/plugins/genshin"
	"github.com/KomeiDiSanXian/remilia/cmd/bot/plugins/iss"
	"github.com/KomeiDiSanXian/remilia/cmd/bot/plugins/minecraft"
	"github.com/KomeiDiSanXian/remilia/cmd/bot/plugins/pic"
	"github.com/KomeiDiSanXian/remilia/cmd/bot/plugins/rpg/coc"
	"github.com/KomeiDiSanXian/remilia/cmd/bot/plugins/rpg/dice"
	"github.com/KomeiDiSanXian/remilia/cmd/bot/plugins/rpg/dnd"
	"github.com/KomeiDiSanXian/remilia/cmd/bot/plugins/sauce"
	"github.com/KomeiDiSanXian/remilia/cmd/bot/plugins/starrail"
	"github.com/KomeiDiSanXian/remilia/cmd/bot/plugins/updater"
	"github.com/KomeiDiSanXian/remilia/cmd/bot/plugins/weather"
	"github.com/KomeiDiSanXian/remilia/cmd/bot/plugins/websearch"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	infrastorage "github.com/KomeiDiSanXian/remilia/infra/storage"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// dataDir 插件持久化数据的根目录。
const dataDir = "data"

// pluginPlatformRegistry 平台适配器注册表，由 main 在 setupPlugins 前注入，
// 供需要主动推送的插件（如 bilibili 开播通知）在 Setup 阶段获取 sender。
var pluginPlatformRegistry *platform.Registry

// setupPluginManager 创建插件管理器并注入 Bot。
func setupPluginManager(bot *remilia.Bot, eng *engine.Engine, cfg *config.Config) *plugin.Manager {
	cp := plugin.NewYAMLConfigProvider(cfg)
	pm := plugin.NewManager(eng, plugin.WithConfigProvider(cp))
	pm.SetStrictDeps(false)
	bot.UsePlugins(pm)
	return pm
}

// setupPlugins 注册全部内置与自定义插件，冻结容器后挂载插件提供的引擎中间件。
func setupPlugins(pm *plugin.Manager, eng *engine.Engine) {
	ensureDataDirs()

	// 需要共享状态或跨插件绑定的实例先单独创建
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
		// 核心系统：控制、权限与帮助
		pluginctrl.New(),
		permission.New(),
		aclPlugin.Descriptor(),
		help.New(),

		// 消息处理与用户互动
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

		// 数据、统计与调度
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

		// AI 与内容工具
		ai.New(eng),
		weather.New(),
		websearch.New(),
		iss.New(iss.WithDataDir(dataDir + "/iss")),
		css.New(css.WithDataDir(dataDir + "/css")),
		bilibili.New(bilibili.WithPlatformRegistry(pluginPlatformRegistry)),

		// 娱乐插件
		anime.New(),
		fortune.New(fortune.WithDataDir(dataDir + "/fortune")),
		minecraft.New(),
		genshin.New(),
		starrail.New(),
		sauce.New(),
		pic.New(),
		dice.New(),
		coc.New(),
		dnd.New(),

		// 系统维护
		about.New(),
		updater.New(updater.WithDataDir(dataDir + "/updater")),
	}

	if err := pm.RegisterBatch(context.Background(), descriptors, plugin.WithInferDeps()); err != nil {
		logger.WithError(err).Fatal("[remilia] Failed to register plugins")
	}
	logger.Infof("[remilia] %d plugins loaded", pm.Count())
	pm.FreezeContainer()

	registerPluginMiddlewares(eng, pm, sp)
	setupMessageLogger(eng)
}

// ensureDataDirs 创建插件运行所需的数据目录。
func ensureDataDirs() {
	for _, dir := range []string{dataDir, dataDir + "/db"} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			logger.WithError(err).Fatalf("[remilia] Failed to create directory: %s", dir)
		}
	}
}

// registerPluginMiddlewares 在容器冻结后挂载需要引擎中间件的插件（stats、auditlog）。
func registerPluginMiddlewares(eng *engine.Engine, pm *plugin.Manager, sp *stats.Plugin) {
	eng.Use(sp.Middleware())
	if ar, ok := pm.GetContainer().Get("auditlog"); ok {
		eng.Use(ar.(*auditlog.Plugin).Middleware())
	}
}

// setupMessageLogger 打开消息历史数据库并挂载日志中间件；打开失败时仅禁用该功能。
func setupMessageLogger(eng *engine.Engine) {
	mlDB, err := messagelog.OpenDB(dataDir + "/db/messagelog.db")
	if err != nil {
		logger.WithError(err).Warn("[remilia] Failed to open messagelog DB, message history disabled")
		return
	}
	messagelog.Default().UseDB(mlDB)
	messagelog.Default().Start()
	eng.Use(messagelog.MessageLogger())
	logger.Info("[remilia] MessageLogger middleware enabled")
}
