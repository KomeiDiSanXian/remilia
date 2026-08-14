package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/api"
	"github.com/KomeiDiSanXian/remilia/cmd/bot/plugins/updater"
	"github.com/KomeiDiSanXian/remilia/config"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/core/fsm"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	infraserver "github.com/KomeiDiSanXian/remilia/infra/server"
	"github.com/KomeiDiSanXian/remilia/infra/tracing"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	commit string
	date   string
)

const defaultHealthAddr = ":9001"

// resolveConfig 尝试加载配置文件，优先级：
//  1. 当前工作目录的 config.yaml
//  2. 可执行文件所在目录的 config.yaml
//  3. 内嵌的 config.default.yaml（sidecar 模式自动使用）
func resolveConfig() *config.Config {
	// 搜索路径
	candidates := []string{"config.yaml"}

	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "config.yaml"))
	}

	for _, path := range candidates {
		cfg, err := config.Load(path)
		if err == nil {
			logger.Infof("[remilia] Loaded config from %s", path)
			return cfg
		}
	}

	// 全部失败 → 将内嵌默认配置写入临时文件并加载
	logger.Warn("[remilia] No config.yaml found, using embedded default config")

	tmpDir := filepath.Join(os.TempDir(), "remilia")
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		log.Fatalf("Failed to create temp config dir: %v", err)
	}
	tmpPath := filepath.Join(tmpDir, "config.default.yaml")
	if err := os.WriteFile(tmpPath, []byte(defaultConfigYAML), 0644); err != nil {
		log.Fatalf("Failed to write default config: %v", err)
	}
	cfg, err := config.Load(tmpPath)
	if err != nil {
		log.Fatalf("Failed to load default config: %v", err)
	}
	logger.Infof("[remilia] Using default config from %s", tmpPath)
	return cfg
}

func main() {
	// 更新后启动确认：等待旧进程退出并校验版本（详见 updater 包文档）。
	// 必须位于任何端口绑定/配置加载之前。
	if err := updater.HandlePendingUpdate(); err != nil {
		log.Printf("[updater] 更新确认流程异常: %v", err)
	}

	cfg := resolveConfig()
	initLogger(cfg)

	tp := initTracing(cfg)

	reg := setupPlatforms(cfg)
	bot := buildBot(cfg, reg)
	eng := bot.Engine()

	bridge := setupMiddleware(eng, &cfg.Tracing, cfg)
	bridge.SetTracingProvider(tp)

	configWatcher := startConfigWatcher()

	fsmMgr := setupRouter(bot, eng)
	pm := setupPluginManager(bot, eng, cfg)
	pluginPlatformRegistry = reg
	setupPlugins(pm, eng)

	// 更新器在 Windows 上退出前先停止全部插件：Teardown 确定性释放 LevelDB 等
	// 数据文件句柄，新进程随后启动时无需依赖任何时序猜测即可安全打开数据文件。
	// 只用 pm.StopAll 而非 bot.Shutdown：hook 从更新 handler 内部触发，
	// bot.Shutdown 会等 engine 停止当前 handler 直到 30s 超时（慢且无必要）。
	registerUpdaterShutdownHook(pm)

	discoverAll(bot, pm)

	healthHandler := newHealthHandler(bot, reg)
	applyBuildInfo(bot)

	pprofSrv := startPprof(cfg.Pprof, healthHandler)
	if pprofSrv != nil {
		bridge.SetPprofServer(pprofSrv)
	}
	healthSrv := startHealthServer(cfg.Pprof.Addr, healthHandler, pprofSrv != nil)

	apiSrv := startAPIServer(cfg.API, "config.yaml", apiServerDeps{
		bot:       bot,
		engine:    eng,
		fsm:       fsmMgr,
		plugins:   pm,
		registry:  reg,
		dashboard: dashboardHandler(),
	})

	logger.Infof("[remilia] Starting... (version=%s commit=%s date=%s)", remilia.Version, commit, date)
	if err := bot.Start(); err != nil {
		logger.WithError(err).Fatal("Failed to start bot")
	}

	// bot 启动后创建自适应限流器——此时 bot.Context() 返回真实 lifecycle context
	setupAdaptiveLimiter(eng, bridge, bot.Context())

	// 订阅 platform 热更新（仅在 bot.* 配置实际变化时触发，避免修改日志级别等无关字段导致连接断开）
	subscribePlatformHotReload(bot, cfg)

	bot.WaitForShutdown()

	logger.Info("[remilia] Shutting down...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	shutdownComponents(shutdownCtx,
		shutdownStep{name: "API server", stop: func(ctx context.Context) error {
			if apiSrv == nil {
				return nil
			}
			return apiSrv.Stop(ctx)
		}},
		shutdownStep{name: "Config watcher", stop: func(context.Context) error {
			if configWatcher == nil {
				return nil
			}
			return configWatcher.Stop()
		}},
		shutdownStep{name: "Health server", stop: func(ctx context.Context) error {
			if healthSrv == nil {
				return nil
			}
			return healthSrv.Shutdown(ctx)
		}},
		shutdownStep{name: "Pprof server", stop: func(ctx context.Context) error {
			if pprofSrv == nil {
				return nil
			}
			return pprofSrv.Stop(ctx)
		}},
	)
	if err := bot.Shutdown(); err != nil {
		logger.WithError(err).Error("[remilia] Shutdown error")
	}
	if err := tp.Shutdown(shutdownCtx); err != nil {
		logger.WithError(err).Warn("[remilia] Tracing shutdown error")
	}
	logger.Info("[remilia] Stopped")
}

// initLogger 初始化日志：补全默认时间格式，并在 Init 前注册实时日志捕获 writer，
// 使 /api/v1/logs 能获取实时日志。
func initLogger(cfg *config.Config) {
	logCfg := cfg.Log
	if logCfg.TimeFormat == "" {
		logCfg.TimeFormat = "2006-01-02 15:04:05"
	}
	logger.SetExtraWriter(api.NewLogCaptureWriter())

	if err := logger.Init(logCfg); err != nil {
		log.Fatalf("Failed to init logger: %v", err)
	}
}

// initTracing 初始化 OpenTelemetry tracing provider。
func initTracing(cfg *config.Config) *tracing.Provider {
	tp, err := tracing.NewProvider(cfg.Tracing)
	if err != nil {
		logger.WithError(err).Fatal("[remilia] Failed to initialize tracing")
	}
	return tp
}

// buildBot 基于配置和平台注册表构建 Bot 实例。
func buildBot(cfg *config.Config, reg *platform.Registry) *remilia.Bot {
	bot, err := remilia.NewBotBuilder().
		WithPlatformRegistry(reg).
		WithName("remilia").
		WithVersion(remilia.Version).
		WithEngineOptions(config.EngineOptions(cfg.Engine)...).
		WithOption(remilia.WithGoroutineThreshold(cfg.Middleware.Degradation.GoroutineThreshold)).
		Build()
	if err != nil {
		logger.WithError(err).Fatal("Failed to build bot")
	}
	return bot
}

// startConfigWatcher 启动配置热更新监听；创建失败时返回 nil（热更新被禁用）。
func startConfigWatcher() *config.Watcher {
	configWatcher, err := config.NewWatcher("config.yaml")
	if err != nil {
		logger.WithError(err).Warn("[remilia] Failed to create config watcher, hot-reload disabled")
		return nil
	}
	configWatcher.Start()
	logger.Info("[remilia] Config file watcher started for hot-reload")
	return configWatcher
}

// registerUpdaterShutdownHook 注册更新流程退出钩子：更新完成后先停止全部插件，
// 确定性释放 LevelDB 等数据文件句柄，便于新进程立即打开同一批数据文件。
func registerUpdaterShutdownHook(pm *plugin.Manager) {
	updater.SetShutdownHook(func() {
		logger.Info("[updater] 更新完成，停止插件以释放数据文件...")
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := pm.StopAll(ctx); err != nil {
			logger.WithError(err).Warn("[updater] 插件停止异常（将直接退出）")
		}
	})
}

// applyBuildInfo 将编译期注入的版本信息写入健康检查。
func applyBuildInfo(bot *remilia.Bot) {
	if hc := bot.HealthCheck(); hc != nil {
		hc.Version = remilia.Version
		hc.Commit = commit
		hc.BuildTime = date
	}
}

// subscribePlatformHotReload 订阅平台热更新：仅当 bot.* 配置实际变化时同步平台适配器，
// 避免修改日志级别等无关字段导致连接断开。
func subscribePlatformHotReload(bot *remilia.Bot, cfg *config.Config) {
	var lastBotCfg = &cfg.Bot // 启动时的 bot 配置
	var lastBotMu sync.Mutex  // 保护 lastBotCfg 并发读写
	config.Subscribe(func(newCfg *config.Config) {
		if newCfg == nil {
			return
		}
		lastBotMu.Lock()
		changed := lastBotCfg.HasChanged(&newCfg.Bot)
		if changed {
			*lastBotCfg = newCfg.Bot
		}
		lastBotMu.Unlock()
		if !changed {
			return
		}
		desired := buildDesiredAdapters(newCfg)
		if err := bot.SyncPlatforms(desired); err != nil {
			logger.WithError(err).Error("[remilia] Failed to sync platforms")
		}
	})
}

// startPprof 启动 pprof 服务器；未启用或启动失败时返回 nil。
func startPprof(pprofCfg config.PprofConfig, healthHandler http.HandlerFunc) *remilia.PprofServer {
	if !pprofCfg.Enabled {
		return nil
	}
	addr := pprofCfg.Addr
	if addr == "" {
		addr = defaultHealthAddr
	}
	def := remilia.DefaultPprofConfig()

	srv := remilia.NewPprofServer(remilia.PprofConfig{
		Enabled:         true,
		Addr:            addr,
		AutoProfile:     pprofCfg.AutoProfile,
		ProfileInterval: parsePprofDuration("profile_interval", pprofCfg.ProfileInterval, def.ProfileInterval),
		ProfileDuration: parsePprofDuration("profile_duration", pprofCfg.ProfileDuration, def.ProfileDuration),
		OutputDir:       pprofCfg.OutputDir,
		EnableMutex:     pprofCfg.EnableMutex,
		EnableBlock:     pprofCfg.EnableBlock,
	})
	if healthHandler != nil {
		srv.AddHandler("/health", healthHandler)
	}
	srv.AddHandler("/metrics", promhttp.Handler().ServeHTTP)
	if err := srv.Start(); err != nil {
		logger.WithError(err).Warn("[remilia] Failed to start pprof")
		return nil
	}
	return srv
}

// parsePprofDuration 解析 pprof 时间配置，解析失败时回退默认值并告警。
func parsePprofDuration(name, value string, fallback time.Duration) time.Duration {
	if value == "" {
		return fallback
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		logger.Warnf("[remilia] Invalid pprof %s %q, using default", name, value)
		return fallback
	}
	return d
}

// newHealthHandler 返回健康检查 HTTP handler。
// 已挂载自定义健康检查时直接委托其输出，否则输出内置 JSON 状态。
func newHealthHandler(bot *remilia.Bot, reg *platform.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if hc := bot.HealthCheck(); hc != nil {
			hc.HTTPHandler(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/json")

		platforms := make([]map[string]any, 0, reg.Len())
		for _, a := range reg.All() {
			platforms = append(platforms, map[string]any{
				"name": a.Platform(),
			})
		}

		running := bot.IsRunning()
		resp := map[string]any{
			"running":   running,
			"uptime":    bot.Uptime().String(),
			"version":   remilia.Version,
			"commit":    commit,
			"buildDate": date,
			"platforms": platforms,
			"status":    "ok",
		}
		if !running {
			resp["status"] = "error"
			w.WriteHeader(http.StatusServiceUnavailable)
		}

		_ = json.NewEncoder(w).Encode(resp)
	}
}

// apiServerDeps API 服务器启动所需的运行时组件。
type apiServerDeps struct {
	bot       *remilia.Bot
	engine    *engine.Engine
	fsm       *fsm.Manager
	plugins   *plugin.Manager
	registry  *platform.Registry
	dashboard http.Handler
}

// startAPIServer 启动管理 API 服务器；未启用时返回 nil。
func startAPIServer(cfg config.APIConfig, configPath string, deps apiServerDeps) *api.Server {
	if !cfg.Enabled {
		return nil
	}
	api.SetBuildInfo(commit, date)
	srv := api.NewServer(cfg.Addr, cfg.APIKey, api.Deps{
		Bot:              deps.bot,
		PluginMgr:        deps.plugins,
		Registry:         deps.registry,
		Engine:           deps.engine,
		FSMMgr:           deps.fsm,
		ConfigPath:       configPath,
		DashboardHandler: deps.dashboard,
	})
	srv.Start()
	return srv
}

// startHealthServer 启动健康检查服务器；pprof 已占用同一地址时返回 nil。
func startHealthServer(addr string, healthHandler http.HandlerFunc, pprofRunning bool) *infraserver.HTTPServer {
	if pprofRunning {
		return nil
	}
	if addr == "" {
		addr = defaultHealthAddr
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)
	mux.Handle("/metrics", promhttp.Handler())

	srv := infraserver.NewHTTPServer(addr, mux)
	srv.WithShutdownTimeout(5 * time.Second)
	srv.Start()
	logger.Infof("[remilia] Health endpoint at http://%s/health", addr)
	return srv
}

// shutdownStep 描述一个关闭步骤；stop 为 nil 时表示组件未启动，跳过执行。
type shutdownStep struct {
	name string
	stop func(context.Context) error
}

// shutdownComponents 按顺序执行关闭步骤：单步失败只记录 Warn 日志，不中断后续步骤。
func shutdownComponents(ctx context.Context, steps ...shutdownStep) {
	for _, s := range steps {
		if s.stop == nil {
			continue
		}
		if err := s.stop(ctx); err != nil {
			logger.Warnf("[remilia] %s shutdown error: %v", s.name, err)
		}
	}
}
