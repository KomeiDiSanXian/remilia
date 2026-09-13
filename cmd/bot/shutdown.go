package main

import (
	"context"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// shutdownTimeout 优雅关闭外部服务与 tracing 的总预算。
const shutdownTimeout = 10 * time.Second

// shutdown 在收到停止信号后按顺序释放各组件。
//
// 顺序与启动顺序相反（API → 配置监听 → 健康检查 → pprof），
// 单个组件关闭失败只记 Warn，不阻断其余组件的关闭。
func (a *app) shutdown() {
	logger.Info("[remilia] Shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if a.apiSrv != nil {
		stopComponent("API server", func() error { return a.apiSrv.Stop(ctx) })
	}
	if a.configWatcher != nil {
		stopComponent("Config watcher", a.configWatcher.Stop)
	}
	if a.healthSrv != nil {
		stopComponent("Health server", func() error { return a.healthSrv.Shutdown(ctx) })
	}
	if a.pprofSrv != nil {
		stopComponent("Pprof server", func() error { return a.pprofSrv.Stop(ctx) })
	}

	if err := a.bot.Shutdown(); err != nil {
		logger.WithError(err).Error("[remilia] Shutdown error")
	}
	if err := a.tp.Shutdown(ctx); err != nil {
		logger.WithError(err).Warn("[remilia] Tracing shutdown error")
	}
	logger.Info("[remilia] Stopped")
}

// stopComponent 执行单个组件关闭；失败只记录 Warn 日志。
func stopComponent(name string, stop func() error) {
	if err := stop(); err != nil {
		logger.Warnf("[remilia] %s shutdown error: %v", name, err)
	}
}
