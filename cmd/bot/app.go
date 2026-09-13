package main

import (
	"log"
	"net/http"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/api"
	"github.com/KomeiDiSanXian/remilia/cmd/bot/plugins/updater"
	"github.com/KomeiDiSanXian/remilia/config"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/core/fsm"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	infraserver "github.com/KomeiDiSanXian/remilia/infra/server"
	"github.com/KomeiDiSanXian/remilia/infra/tracing"
	"github.com/KomeiDiSanXian/remilia/middleware/hotreload"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// app 持有 cmd/bot 运行期间的全部长生命周期组件。
//
// 装配由 run 按固定顺序推进，每个阶段把产出的组件写回字段，供后续阶段与
// shutdown 复用。此前这些组件是 main 的局部变量，跨函数传递只能靠追加参数
// （如 buildDesiredAdapters 每次重建适配器）或包级全局变量（如平台注册表）。
type app struct {
	cfg *config.Config
	tp  *tracing.Provider

	reg *platform.Registry
	bot *remilia.Bot
	eng *engine.Engine

	bridge *hotreload.Bridge
	fsmMgr *fsm.Manager
	pm     *plugin.Manager

	configWatcher *config.Watcher

	healthHandler http.HandlerFunc
	pprofSrv      *remilia.PprofServer
	healthSrv     *infraserver.HTTPServer
	apiSrv        *api.Server
}

func newApp() *app { return &app{} }

// run 完成装配、启动与优雅关闭。
//
// 阶段顺序存在硬依赖，不可重排：
//   - 更新后启动确认必须早于任何端口绑定与配置加载；
//   - wirePermissionManager 必须在插件注册之后、bot.Start 之前，
//     否则所有基于 ctx 的 RBAC 检查都会退化为“权限系统未初始化”；
//   - setupAdaptiveLimiter 必须在 bot.Start 之后，此时 bot.Context()
//     才返回真实 lifecycle context。
func (a *app) run() {
	// 更新后启动确认：等待旧进程退出并校验版本（详见 updater 包文档）。
	if err := updater.HandlePendingUpdate(); err != nil {
		log.Printf("[updater] 更新确认流程异常: %v", err)
	}

	a.loadConfig()
	a.initLogger()
	a.initTracing()

	a.setupPlatforms()
	a.buildBot()

	a.setupMiddleware()
	a.startConfigWatcher()
	a.setupRouter()

	a.setupPluginManager()
	a.setupPlugins()
	a.registerUpdaterShutdownHook()

	a.discoverAll()
	a.wirePermissionManager()
	a.serve()

	logger.Infof("[remilia] Starting... (version=%s commit=%s date=%s)", remilia.Version, commit, date)
	if err := a.bot.Start(); err != nil {
		logger.WithError(err).Fatal("Failed to start bot")
	}

	a.setupAdaptiveLimiter()

	// 仅在 bot.* 配置实际变化时同步平台，避免修改日志级别等无关字段导致连接断开
	a.subscribePlatformHotReload()

	a.bot.WaitForShutdown()
	a.shutdown()
}
