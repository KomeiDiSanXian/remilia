package main

import (
	"context"
	"os"
	"strconv"
	"strings"
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
	"github.com/KomeiDiSanXian/remilia/builtin/knowledgebase"
	"github.com/KomeiDiSanXian/remilia/builtin/messagelog"
	"github.com/KomeiDiSanXian/remilia/builtin/moderation"
	"github.com/KomeiDiSanXian/remilia/builtin/ping"
	"github.com/KomeiDiSanXian/remilia/builtin/pluginctrl"
	"github.com/KomeiDiSanXian/remilia/builtin/pluginstore"
	"github.com/KomeiDiSanXian/remilia/builtin/ratelimitui"
	"github.com/KomeiDiSanXian/remilia/builtin/scheduler"
	"github.com/KomeiDiSanXian/remilia/builtin/sendqueue"
	"github.com/KomeiDiSanXian/remilia/builtin/statistics"
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

	// messagelog 事实层事件广播：必须在插件 Setup（订阅 MessageRecorded）之前
	// 注入发布器，且早于 setupMessageLogger 内的 Start()。
	messagelog.Default().SetEventPublisher(pm.GetEventBus())

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
		statistics.New(statistics.WithStore(dataDir + "/db/statistics.db")),
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
		knowledgebase.New(),
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
	cfg, _ := config.Get()
	var mlCfg *config.MessagelogConfig
	if cfg != nil {
		mlCfg = cfg.Messagelog
	}
	if mlCfg != nil && !mlCfg.Enabled {
		logger.Info("[remilia] MessageLogger disabled by config")
		return
	}

	dbPath := dataDir + "/db/messagelog.db"
	if mlCfg != nil && mlCfg.DBPath != "" {
		dbPath = mlCfg.DBPath
	}
	mlDB, err := messagelog.OpenDB(dbPath)
	if err != nil {
		logger.WithError(err).Warn("[remilia] Failed to open messagelog DB, message history disabled")
		return
	}
	messagelog.Default().UseDB(mlDB)

	opts := messagelog.Options{}
	if mlCfg != nil {
		if d, err := time.ParseDuration(mlCfg.Flush.Interval); err == nil && d > 0 {
			opts.FlushInterval = d
		}
		if mlCfg.Flush.BatchSize > 0 {
			opts.BatchSize = mlCfg.Flush.BatchSize
		}
		if mlCfg.Flush.QueueSize > 0 {
			opts.QueueSize = mlCfg.Flush.QueueSize
		}
		// spool 默认启用（Durable 语义）；显式配置 false 才关闭
		opts.SpoolEnabled = mlCfg.Spool.Enabled == nil || *mlCfg.Spool.Enabled
		if mlCfg.Spool.Dir != "" {
			opts.SpoolDir = mlCfg.Spool.Dir
		} else {
			opts.SpoolDir = dataDir + "/spool/messagelog"
		}
		if n, err := config.ParseSize(mlCfg.Spool.MaxSize); err == nil && n > 0 {
			opts.SpoolMaxSize = n
		}
		if mlCfg.Cache.PerChatCapacity > 0 {
			opts.CachePerChat = mlCfg.Cache.PerChatCapacity
		}
		if mlCfg.Cache.GlobalMaxEntries > 0 {
			opts.CacheGlobal = mlCfg.Cache.GlobalMaxEntries
		}
		// 空闲优先淘汰默认开启；显式 false 关闭
		if mlCfg.Cache.IdleEvict != nil {
			opts.CacheIdleEvict = mlCfg.Cache.IdleEvict
		}
		opts.RecordSystemEvents = mlCfg.Record.SystemEvents
		// 记录失败出站默认开启；显式 false 关闭
		if mlCfg.Record.FailedOutbound != nil && !*mlCfg.Record.FailedOutbound {
			f := false
			opts.RecordFailedOutbound = &f
		}
		// 附件生命周期（默认：metadata 落库 + hot_window 下载）
		attDir := mlCfg.Attachments.Dir
		if attDir == "" {
			attDir = dataDir + "/attachments"
		}
		att := &messagelog.AttachmentOptions{
			Dir:                 attDir,
			Scope:               "hot_window",
			HotWindowAge:        24 * time.Hour,
			MaxPending:          10000,
			DownloadConcurrency: 8,
			DownloadRetries:     3,
			DownloadBackoff:     []time.Duration{time.Second, 5 * time.Second, 30 * time.Second},
			MaxSize:             20 << 20,
			LazyFallback:        true,
			GCEnabled:           true,
			GracePeriod:         7 * 24 * time.Hour,
			BackfillEnabled:     true,
			BackfillBatch:       50,
			IdleThreshold:       0.3,
			BackfillAttempts:    3,
			BackfillInterval:    time.Minute,
			GCInterval:          30 * time.Minute,
		}
		if mlCfg.Attachments.Scope != "" {
			att.Scope = mlCfg.Attachments.Scope
		}
		if d, err := time.ParseDuration(mlCfg.Attachments.HotWindowAge); err == nil && d > 0 {
			att.HotWindowAge = d
		}
		if n, err := config.ParseSize(mlCfg.Attachments.MaxDiskUsage); err == nil && n >= 0 {
			att.MaxDiskUsage = n
		}
		if mlCfg.Attachments.MaxPendingTasks > 0 {
			att.MaxPending = mlCfg.Attachments.MaxPendingTasks
		}
		if mlCfg.Attachments.DownloadConcurrency > 0 {
			att.DownloadConcurrency = mlCfg.Attachments.DownloadConcurrency
		}
		if mlCfg.Attachments.DownloadRetries >= 0 {
			att.DownloadRetries = mlCfg.Attachments.DownloadRetries
		}
		if len(mlCfg.Attachments.DownloadBackoff) > 0 {
			var seq []time.Duration
			for _, s := range mlCfg.Attachments.DownloadBackoff {
				if d, err := time.ParseDuration(s); err == nil {
					seq = append(seq, d)
				}
			}
			if len(seq) > 0 {
				att.DownloadBackoff = seq
			}
		}
		if n, err := config.ParseSize(mlCfg.Attachments.MaxSize); err == nil && n >= 0 {
			att.MaxSize = n
		}
		// rate_limit_per_host 形如 "10/s"
		if rl := mlCfg.Attachments.RateLimitPerHost; rl != "" {
			if f, ok := parsePerSecond(rl); ok {
				att.RatePerHost = f
			}
		}
		if mlCfg.Attachments.LazyFallback != nil {
			att.LazyFallback = *mlCfg.Attachments.LazyFallback
		}
		if mlCfg.Attachments.GC.Enabled != nil {
			att.GCEnabled = *mlCfg.Attachments.GC.Enabled
		}
		if d, err := time.ParseDuration(mlCfg.Attachments.GC.GracePeriod); err == nil && d > 0 {
			att.GracePeriod = d
		}
		if mlCfg.Attachments.Backfill.Enabled != nil {
			att.BackfillEnabled = *mlCfg.Attachments.Backfill.Enabled
		}
		if mlCfg.Attachments.Backfill.BatchSize > 0 {
			att.BackfillBatch = mlCfg.Attachments.Backfill.BatchSize
		}
		if mlCfg.Attachments.Backfill.IdleThreshold > 0 {
			att.IdleThreshold = mlCfg.Attachments.Backfill.IdleThreshold
		}
		if mlCfg.Attachments.Backfill.MaxAttempts > 0 {
			att.BackfillAttempts = mlCfg.Attachments.Backfill.MaxAttempts
		}
		opts.Attachments = att

		// 历史保留（默认永久保留）
		if mlCfg.Retention.Days > 0 || mlCfg.Retention.MaxEntries > 0 {
			ret := &messagelog.RetentionOptions{
				Days:            mlCfg.Retention.Days,
				MaxEntries:      mlCfg.Retention.MaxEntries,
				CleanupInterval: time.Hour,
			}
			if d, err := time.ParseDuration(mlCfg.Retention.CleanupInterval); err == nil && d > 0 {
				ret.CleanupInterval = d
			}
			opts.Retention = ret
		}
	}
	messagelog.Default().Start(opts)
	eng.Use(messagelog.MessageLogger())
	logger.Info("[remilia] MessageLogger middleware enabled")
}

// parsePerSecond 解析 "10/s" 形式的每秒速率；无法解析返回 ok=false。
func parsePerSecond(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "/s")
	s = strings.TrimSuffix(s, "/S")
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || f <= 0 {
		return 0, false
	}
	return f, true
}
