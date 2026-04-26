package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/KomeiDiSanXian/remilia/config"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

func main() {
	logger.Info("[ConfigHotReload] Configuration hot-reload example")

	// 创建配置监听器
	watcher, err := config.NewWatcher("config.yaml")
	if err != nil {
		log.Fatalf("Failed to create config watcher: %v\nPlease copy config.example.yaml to config.yaml", err)
	}
	defer watcher.Stop()

	// 添加配置变更回调
	watcher.AddCallback(func(oldConfig, newConfig *config.Config) error {
		logger.Info("[ConfigHotReload] Configuration changed!")

		// 检测日志级别变更
		if oldConfig.Log.Level != newConfig.Log.Level {
			logger.WithFields(logger.Fields{
				"old": oldConfig.Log.Level,
				"new": newConfig.Log.Level,
			}).Info("[ConfigHotReload] Log level changed (restart to apply)")
		}

		// 检测中间件配置变更
		if oldConfig.Middleware.Logging != newConfig.Middleware.Logging {
			logger.WithFields(logger.Fields{
				"old": oldConfig.Middleware.Logging,
				"new": newConfig.Middleware.Logging,
			}).Info("[ConfigHotReload] Logging middleware changed")
		}

		// 检测并发限制变更
		if oldConfig.Concurrency.Limit != newConfig.Concurrency.Limit {
			logger.WithFields(logger.Fields{
				"old": oldConfig.Concurrency.Limit,
				"new": newConfig.Concurrency.Limit,
			}).Info("[ConfigHotReload] Concurrency limit changed")
		}

		return nil
	})

	// 启动监听
	watcher.Start()

	// 打印初始配置
	cfg := watcher.GetConfig()
	logger.WithFields(logger.Fields{
		"app_id":      cfg.Bot.QQ.AppID,
		"log_level":   cfg.Log.Level,
		"port":        cfg.Server.Port,
		"concurrency": cfg.Concurrency.Limit,
	}).Info("[ConfigHotReload] Initial configuration loaded")

	logger.Info("[ConfigHotReload] Application running...")
	logger.Info("[ConfigHotReload] Modify config.yaml to see hot-reload!")
	logger.Info("[ConfigHotReload] Press Ctrl+C to stop")

	// 优雅关闭
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	logger.Info("[ConfigHotReload] Shutting down...")
}
