package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/api"
	"github.com/KomeiDiSanXian/remilia/cmd/bot/plugins/updater"
	"github.com/KomeiDiSanXian/remilia/config"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/infra/tracing"
)

// loadConfig 解析运行配置并写入 app。
func (a *app) loadConfig() {
	a.cfg = resolveConfig()
}

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

// initLogger 初始化日志：补全默认时间格式，并在 Init 前注册实时日志捕获 writer，
// 使 /api/v1/logs 能获取实时日志。
func (a *app) initLogger() {
	logCfg := a.cfg.Log
	if logCfg.TimeFormat == "" {
		logCfg.TimeFormat = "2006-01-02 15:04:05"
	}
	logger.SetExtraWriter(api.NewLogCaptureWriter())

	if err := logger.Init(logCfg); err != nil {
		log.Fatalf("Failed to init logger: %v", err)
	}
}

// initTracing 初始化 OpenTelemetry tracing provider。
func (a *app) initTracing() {
	tp, err := tracing.NewProvider(a.cfg.Tracing)
	if err != nil {
		logger.WithError(err).Fatal("[remilia] Failed to initialize tracing")
	}
	a.tp = tp
}

// buildBot 基于配置和平台注册表构建 Bot 实例。
func (a *app) buildBot() {
	bot, err := remilia.NewBotBuilder().
		WithPlatformRegistry(a.reg).
		WithName("remilia").
		WithVersion(remilia.Version).
		WithEngineOptions(config.EngineOptions(a.cfg.Engine)...).
		WithOption(remilia.WithGoroutineThreshold(a.cfg.Middleware.Degradation.GoroutineThreshold)).
		Build()
	if err != nil {
		logger.WithError(err).Fatal("Failed to build bot")
	}
	a.bot = bot
	a.eng = bot.Engine()
}

// startConfigWatcher 启动配置热更新监听；创建失败时热更新被禁用。
func (a *app) startConfigWatcher() {
	w, err := config.NewWatcher("config.yaml")
	if err != nil {
		logger.WithError(err).Warn("[remilia] Failed to create config watcher, hot-reload disabled")
		return
	}
	w.Start()
	logger.Info("[remilia] Config file watcher started for hot-reload")
	a.configWatcher = w
}

// registerUpdaterShutdownHook 注册更新流程退出钩子：更新完成后先停止全部插件，
// 确定性释放 LevelDB 等数据文件句柄，便于新进程立即打开同一批数据文件。
//
// 只用 pm.StopAll 而非 bot.Shutdown：hook 从更新 handler 内部触发，
// bot.Shutdown 会等 engine 停止当前 handler 直到 30s 超时（慢且无必要）。
func (a *app) registerUpdaterShutdownHook() {
	pm := a.pm
	updater.SetShutdownHook(func() {
		logger.Info("[updater] 更新完成，停止插件以释放数据文件...")
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := pm.StopAll(ctx); err != nil {
			logger.WithError(err).Warn("[updater] 插件停止异常（将直接退出）")
		}
	})
}
