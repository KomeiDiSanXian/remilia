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
	cfg := resolveConfig()

	logCfg := cfg.Log
	if logCfg.TimeFormat == "" {
		logCfg.TimeFormat = "2006-01-02 15:04:05"
	}
	// 在 Init 之前注册日志捕获 writer，使 /api/v1/logs 能获取实时日志
	logger.SetExtraWriter(api.NewLogCaptureWriter())

	if err := logger.Init(logCfg); err != nil {
		log.Fatalf("Failed to init logger: %v", err)
	}

	tp, err := tracing.NewProvider(cfg.Tracing)
	if err != nil {
		logger.WithError(err).Fatal("[remilia] Failed to initialize tracing")
	}

	reg := setupPlatforms(cfg)

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

	eng := bot.Engine()
	bridge := setupMiddleware(eng, &cfg.Tracing, cfg)
	bridge.SetTracingProvider(tp)

	configWatcher, err := config.NewWatcher("config.yaml")
	if err != nil {
		logger.WithError(err).Warn("[remilia] Failed to create config watcher, hot-reload disabled")
	} else {
		configWatcher.Start()
		logger.Info("[remilia] Config file watcher started for hot-reload")
	}

	fsmMgr := setupRouter(bot, eng)
	pm := setupPluginManager(bot, eng, cfg)
	setupPlugins(pm, eng)
	discoverAll(bot, pm)

	healthHandler := newHealthHandler(bot, reg)
	if hc := bot.HealthCheck(); hc != nil {
		hc.Version = remilia.Version
		hc.Commit = commit
		hc.BuildTime = date
	}
	pprofSrv := startPprof(cfg.Pprof, healthHandler)
	if pprofSrv != nil {
		bridge.SetPprofServer(pprofSrv)
	}
	healthSrv := startHealthServer(cfg.Pprof.Addr, healthHandler, pprofSrv != nil)

	apiSrv := startAPIServer(cfg.API, "config.yaml", bot, eng, fsmMgr, pm, reg, dashboardHandler())

	logger.Infof("[remilia] Starting... (version=%s commit=%s date=%s)", remilia.Version, commit, date)
	if err := bot.Start(); err != nil {
		logger.WithError(err).Fatal("Failed to start bot")
	}

	// bot 启动后创建自适应限流器——此时 bot.Context() 返回真实 lifecycle context
	setupAdaptiveLimiter(eng, bridge, bot.Context())

	// 订阅 platform 热更新（仅在 bot.* 配置实际变化时触发，避免修改日志级别等无关字段导致连接断开）
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

	bot.WaitForShutdown()

	logger.Info("[remilia] Shutting down...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if apiSrv != nil {
		if err := apiSrv.Stop(shutdownCtx); err != nil {
			logger.Warnf("[remilia] API server shutdown error: %v", err)
		}
	}
	if configWatcher != nil {
		if err := configWatcher.Stop(); err != nil {
			logger.Warnf("[remilia] Config watcher shutdown error: %v", err)
		}
	}
	if healthSrv != nil {
		if err := healthSrv.Shutdown(shutdownCtx); err != nil {
			logger.Warnf("[remilia] Health server shutdown error: %v", err)
		}
	}
	if pprofSrv != nil {
		if err := pprofSrv.Stop(shutdownCtx); err != nil {
			logger.Warnf("[remilia] Pprof server shutdown error: %v", err)
		}
	}
	if err := bot.Shutdown(); err != nil {
		logger.WithError(err).Error("[remilia] Shutdown error")
	}
	if err := tp.Shutdown(shutdownCtx); err != nil {
		logger.WithError(err).Warn("[remilia] Tracing shutdown error")
	}
	logger.Info("[remilia] Stopped")
}

func startPprof(pprofCfg config.PprofConfig, healthHandler http.HandlerFunc) *remilia.PprofServer {
	if !pprofCfg.Enabled {
		return nil
	}
	interval, err := time.ParseDuration(pprofCfg.ProfileInterval)
	if err != nil {
		logger.Warnf("[remilia] Invalid pprof profile_interval %q, using default", pprofCfg.ProfileInterval)
	}
	duration, err := time.ParseDuration(pprofCfg.ProfileDuration)
	if err != nil {
		logger.Warnf("[remilia] Invalid pprof profile_duration %q, using default", pprofCfg.ProfileDuration)
	}
	addr := pprofCfg.Addr
	if addr == "" {
		addr = defaultHealthAddr
	}

	srv := remilia.NewPprofServer(remilia.PprofConfig{
		Enabled:         true,
		Addr:            addr,
		AutoProfile:     pprofCfg.AutoProfile,
		ProfileInterval: interval,
		ProfileDuration: duration,
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

func newHealthHandler(bot *remilia.Bot, reg *platform.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		platforms := make([]map[string]any, 0)
		for _, a := range reg.All() {
			platforms = append(platforms, map[string]any{
				"name": a.Platform(),
			})
		}

		resp := map[string]any{
			"running":   bot.IsRunning(),
			"uptime":    bot.Uptime().String(),
			"version":   remilia.Version,
			"commit":    commit,
			"buildDate": date,
			"platforms": platforms,
		}

		if hc := bot.HealthCheck(); hc != nil {
			hc.HTTPHandler(w, r)
			return
		}

		resp["status"] = "ok"
		if !bot.IsRunning() {
			resp["status"] = "error"
			w.WriteHeader(http.StatusServiceUnavailable)
		}

		_ = json.NewEncoder(w).Encode(resp)
	}
}

func startAPIServer(cfg config.APIConfig, configPath string, bot *remilia.Bot, eng *engine.Engine, fsmMgr *fsm.Manager, pm *plugin.Manager, reg *platform.Registry, dash http.Handler) *api.Server {
	if !cfg.Enabled {
		return nil
	}
	api.SetBuildInfo(commit, date)
	srv := api.NewServer(cfg.Addr, cfg.APIKey, api.Deps{
		Bot:              bot,
		PluginMgr:        pm,
		Registry:         reg,
		Engine:           eng,
		FSMMgr:           fsmMgr,
		ConfigPath:       configPath,
		DashboardHandler: dash,
	})
	srv.Start()
	return srv
}

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
