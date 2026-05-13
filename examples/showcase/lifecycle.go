package main

import "github.com/KomeiDiSanXian/remilia/infra/logger"

// lifecycleLogger 日志插件生命周期事件，用于调试插件加载/卸载流程。
type lifecycleLogger struct{}

func (l *lifecycleLogger) OnPluginLoaded(name string) {
	logger.Infof("[Lifecycle] loaded: %s", name)
}

func (l *lifecycleLogger) OnPluginUnloaded(name string) {
	logger.Infof("[Lifecycle] unloaded: %s", name)
}

func (l *lifecycleLogger) OnPluginReloaded(name string) {
	logger.Infof("[Lifecycle] reloaded: %s", name)
}

func (l *lifecycleLogger) OnPluginError(name, op string, err error) {
	logger.Warnf("[Lifecycle] error %s.%s: %v", name, op, err)
}
