package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/config"
	"github.com/KomeiDiSanXian/remilia/infra/health"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	infraserver "github.com/KomeiDiSanXian/remilia/infra/server"
	"github.com/KomeiDiSanXian/remilia/infra/tracing"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	commit string
	date   string
)

const defaultHealthAddr = ":9001"

func main() {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v\nPlease copy config.example.yaml to config.yaml", err)
	}

	logCfg := cfg.Log
	if logCfg.TimeFormat == "" {
		logCfg.TimeFormat = "2006-01-02 15:04:05"
	}
	if err := logger.Init(logCfg); err != nil {
		log.Fatalf("Failed to init logger: %v", err)
	}

	tp, err := tracing.NewProvider(cfg.Tracing)
	if err != nil {
		logger.WithError(err).Fatal("[bot] Failed to initialize tracing")
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
	setupMiddleware(eng, &cfg.Tracing)
	fsmMgr := setupRouter(bot, eng)
	pm := setupPluginManager(bot, eng, cfg)
	setupPlugins(pm, eng)
	_ = fsmMgr

	healthHandler := newHealthHandler(bot, reg)
	pprofSrv := startPprof(cfg.Pprof, healthHandler)
	healthSrv := startHealthServer(cfg.Pprof.Addr, healthHandler, pprofSrv != nil)

	logger.Infof("[bot] Starting... (version=%s commit=%s date=%s)", remilia.Version, commit, date)
	if err := bot.Start(); err != nil {
		logger.WithError(err).Fatal("Failed to start bot")
	}

	discoverAll(bot, pm)

	bot.WaitForShutdown()

	logger.Info("[bot] Shutting down...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if healthSrv != nil {
		_ = healthSrv.Shutdown(shutdownCtx)
	}
	if pprofSrv != nil {
		_ = pprofSrv.Stop(shutdownCtx)
	}
	if err := bot.Shutdown(); err != nil {
		logger.WithError(err).Error("[bot] Shutdown error")
	}
	if err := tp.Shutdown(shutdownCtx); err != nil {
		logger.WithError(err).Warn("[bot] Tracing shutdown error")
	}
	logger.Info("[bot] Stopped")
}

func startPprof(pprofCfg config.PprofConfig, healthHandler http.HandlerFunc) *remilia.PprofServer {
	if !pprofCfg.Enabled {
		return nil
	}
	interval, _ := time.ParseDuration(pprofCfg.ProfileInterval)
	duration, _ := time.ParseDuration(pprofCfg.ProfileDuration)
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
		logger.WithError(err).Warn("[bot] Failed to start pprof")
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
			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			defer cancel()
			checkResp := hc.Check(ctx)
			resp["status"] = checkResp.Status
			resp["groups"] = checkResp.Groups
			resp["time"] = checkResp.Time

			if checkResp.Status == health.Unhealthy || checkResp.Status == health.Critical {
				w.WriteHeader(http.StatusServiceUnavailable)
			}
		} else {
			resp["status"] = "ok"
			if !bot.IsRunning() {
				resp["status"] = "error"
				w.WriteHeader(http.StatusServiceUnavailable)
			}
		}

		_ = json.NewEncoder(w).Encode(resp)
	}
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
	logger.Infof("[bot] Health endpoint at http://%s/health", addr)
	return srv
}
