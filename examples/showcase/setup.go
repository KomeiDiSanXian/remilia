package main

import (
	"context"
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
	subscriptionpkg "github.com/KomeiDiSanXian/remilia/builtin/subscription"
	"github.com/KomeiDiSanXian/remilia/builtin/verifycode"
	"github.com/KomeiDiSanXian/remilia/builtin/vevent"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/core/fsm"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	infrastorage "github.com/KomeiDiSanXian/remilia/infra/storage"
	"github.com/KomeiDiSanXian/remilia/middleware"
	"github.com/KomeiDiSanXian/remilia/plugin"
	"github.com/KomeiDiSanXian/remilia/router"
)

// PluginHolders 持有需要跨 setupPlugins / registerCommands 访问的插件实例。
// 减少重复从容器中 Get 的开销。
type PluginHolders struct {
	Broadcast  *broadcast.Plugin
	Cooldown   *cooldown.Plugin
	I18n       *i18n.Plugin
	Stats      *stats.Plugin
	SubHandle  *subscriptionpkg.PluginHandle
}

// setupMiddleware 配置 Engine 的中间件链。
// 中间件按注册顺序由外向内包裹 Handler。
func setupMiddleware(eng *engine.Engine) {
	eng.Use(middleware.ProductionSet()...) // 去重 + 限流 + 超时 + 熔断 + 自适应限流 + 日志
	eng.Use(requestCounterMiddleware())    // 请求计数（调试用）
	eng.Use(processingTimeMiddleware())    // 性能追踪
}

// setupRouter 配置三层路由架构。
// FSM 是内置一级路由（Priority=-1000），不受规则顺序影响。
func setupRouter(bot *remilia.Bot, eng *engine.Engine) *fsm.Manager {
	fsmMgr := fsm.NewManager(nil)
	rtr := router.New(eng, fsmMgr.Engine())
	rtr.Route(router.WithCommandPrefix())

	engMgr := engine.NewEngineManager(eng)
	bot.UseEngineManager(engMgr)
	rtr.WithEngineManager(engMgr)
	bot.UseRouter(rtr)
	return fsmMgr
}

// setupPluginManager 创建插件管理器并添加生命周期监听。
func setupPluginManager(bot *remilia.Bot, eng *engine.Engine) *plugin.Manager {
	pm := plugin.NewManager(eng)
	pm.SetStrictDeps(false)
	pm.AddListener(&lifecycleLogger{})
	bot.UsePlugins(pm)
	return pm
}

// setupPlugins 创建所有插件实例并注册到控制器。
func setupPlugins(pm *plugin.Manager, eng *engine.Engine) *PluginHolders {
	if err := os.MkdirAll("data", 0755); err != nil {
		logger.WithError(err).Fatal("create data dir")
	}

	asPlugin := antispam.NewPlugin(antispam.Config{
		UserRate: 5, UserBurst: 10, GroupRate: 30, GroupBurst: 50,
		BanOnViolation: true, BanDuration: 5 * time.Minute,
	})
	cdPlugin := cooldown.NewPlugin()
	sp := stats.NewPlugin()
	schedPlugin := scheduler.NewPlugin()
	aclPlugin := acl.NewPlugin()
	rlPlugin := ratelimitui.NewPlugin()
	rlPlugin.BindAntispam(asPlugin)
	rlPlugin.BindCooldown(cdPlugin)
	i18nPlugin := i18n.NewPlugin(i18n.Config{DefaultLocale: "zh-CN"})
	bcPlugin := broadcast.NewPlugin(broadcast.DefaultConfig())
	subPlugin := subscriptionpkg.NewPlugin(
		subscriptionpkg.WithPollInterval(5*time.Minute),
		subscriptionpkg.WithDispatcher(func(ctx context.Context, target subscriptionpkg.Target, item subscriptionpkg.Item) error {
			logger.Infof("[subscription] dispatch to %s: %s", target.ChatID, item.Title)
			return nil
		}),
	)
	subPlugin.RegisterSource(&demoSource{})

	// 批量注册插件（自动按依赖拓扑排序）
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
			OnMatch:  replyFunc("[blocked: %s]"),
		}),
		cdPlugin.Descriptor(),
		sp.Descriptor(),
		auditlog.New(),
		schedPlugin.Descriptor(),
		i18nPlugin.Descriptor(),
		rlPlugin.Descriptor(),
		pluginstore.New(),
		help.New(),
		admin.New(),
		builtinstorage.New(infrastorage.WithDSN("data/showcase.db")),
		bcPlugin.Descriptor(),
		sendqueue.New(sendqueue.DefaultConfig()),
		subPlugin.Descriptor(),
		job.New(),
		debug.New(),
		vevent.New(eng),
	}); err != nil {
		logger.WithError(err).Fatal("register plugins")
	}
	logger.Infof("[showcase] %d plugins loaded", pm.Count())

	// 注册后置中间件（依赖插件实例）
	eng.Use(sp.Middleware())
	if ar, ok := pm.GetContainer().Get("auditlog"); ok {
		eng.Use(ar.(*auditlog.Plugin).Middleware())
	}

	// pluginstore 元数据持久化
	if psRaw, ok := pm.GetContainer().Get("pluginstore"); ok {
		ps := psRaw.(*pluginstore.Plugin)
		ps.RegisterFunc("showcase-meta",
			func() (any, error) { return map[string]any{"total": sp.TotalMessages()}, nil },
			func(v any) error { logger.Infof("[pluginstore] restored: %v", v); return nil },
		)
	}

	return &PluginHolders{
		Broadcast: bcPlugin, Cooldown: cdPlugin, I18n: i18nPlugin, Stats: sp,
		SubHandle: subPlugin,
	}
}

// registerCommands 注册主命令插件。
func registerCommands(pm *plugin.Manager) {
	if err := pm.Register(commandPlugin(pm)); err != nil {
		logger.WithError(err).Fatal("register command plugin")
	}
}

// registerSignupFSM 注册 FSM 多步骤表单到管理器。
func registerSignupFSM(fsmMgr *fsm.Manager, pm *plugin.Manager) {
	signupFSM := buildSignupFSM()
	if err := fsmMgr.Register(&fsm.FSMDescriptor{Name: "signup", FSM: signupFSM}); err != nil {
		logger.WithError(err).Fatal("register signup FSM")
	}
}

// getPluginHolders 从容器中取出插件实例备用。
func getPluginHolders(pm *plugin.Manager) *PluginHolders {
	return &PluginHolders{
		Cooldown:  mustGet[*cooldown.Plugin](pm, "cooldown"),
		Stats:     mustGet[*stats.Plugin](pm, "stats"),
		I18n:      mustGet[*i18n.Plugin](pm, "i18n"),
		Broadcast: mustGet[*broadcast.Plugin](pm, "broadcast"),
	}
}

// mustGet 从容器获取插件实例，不存在则 panic。
func mustGet[T any](pm *plugin.Manager, name string) T {
	v, ok := pm.GetContainer().Get(name)
	if !ok {
		panic("plugin " + name + " not found")
	}
	return v.(T)
}

// loadLocales 从嵌入的 FS 加载多语言文件。
func loadLocales(pm *plugin.Manager) {
	raw, ok := pm.GetContainer().Get("i18n")
	if !ok {
		return
	}
	i18nPlugin := raw.(*i18n.Plugin)
	if entries, err := localeFS.ReadDir("locales"); err == nil {
		for _, e := range entries {
			name := e.Name()
			if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
				continue
			}
			locale := strings.TrimSuffix(strings.TrimSuffix(name, ".yaml"), ".yml")
			data, err := localeFS.ReadFile("locales/" + name)
			if err != nil {
				continue
			}
			_ = i18nPlugin.LoadBytes(locale, data)
		}
	}
}
