package main

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/api"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	infraserver "github.com/KomeiDiSanXian/remilia/infra/server"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const defaultHealthAddr = ":9001"

// serve 装配对外服务：健康检查 handler、pprof 与可选的管理 API。
func (a *app) serve() {
	a.healthHandler = newHealthHandler(a.bot, a.reg)
	applyBuildInfo(a.bot)

	a.pprofSrv = a.startPprof()
	if a.pprofSrv != nil {
		a.bridge.SetPprofServer(a.pprofSrv)
	}
	a.healthSrv = a.startHealthServer()
	a.apiSrv = a.startAPIServer()
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

// applyBuildInfo 将编译期注入的版本信息写入健康检查。
func applyBuildInfo(bot *remilia.Bot) {
	if hc := bot.HealthCheck(); hc != nil {
		hc.Version = remilia.Version
		hc.Commit = commit
		hc.BuildTime = date
	}
}

// startPprof 启动 pprof 服务器；未启用或启动失败时返回 nil。
func (a *app) startPprof() *remilia.PprofServer {
	pprofCfg := a.cfg.Pprof
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
	if a.healthHandler != nil {
		srv.AddHandler("/health", a.healthHandler)
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

// startHealthServer 启动健康检查服务器；pprof 已占用同一地址时返回 nil。
func (a *app) startHealthServer() *infraserver.HTTPServer {
	if a.pprofSrv != nil {
		return nil
	}
	addr := a.cfg.Pprof.Addr
	if addr == "" {
		addr = defaultHealthAddr
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", a.healthHandler)
	mux.Handle("/metrics", promhttp.Handler())

	srv := infraserver.NewHTTPServer(addr, mux)
	srv.WithShutdownTimeout(5 * time.Second)
	srv.Start()
	logger.Infof("[remilia] Health endpoint at http://%s/health", addr)
	return srv
}

// startAPIServer 启动管理 API 服务器；未启用时返回 nil。
func (a *app) startAPIServer() *api.Server {
	cfg := a.cfg.API
	if !cfg.Enabled {
		return nil
	}
	api.SetBuildInfo(commit, date)
	srv := api.NewServer(cfg.Addr, cfg.APIKey, api.Deps{
		Bot:              a.bot,
		PluginMgr:        a.pm,
		Registry:         a.reg,
		Engine:           a.eng,
		FSMMgr:           a.fsmMgr,
		ConfigPath:       "config.yaml",
		DashboardHandler: dashboardHandler(),
	})
	srv.Start()
	return srv
}
