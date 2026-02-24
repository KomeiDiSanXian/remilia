package plugin

import (
	"fmt"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// PluginLogger 插件上下文日志器接口
//
// 通过 [SetupContext.Log] 获取，自动携带插件名前缀，无需手动写 "[PluginName] "。
//
// 示例：
//
//	Setup: func(ctx *plugin.SetupContext) error {
//	    ctx.Log.Info("Plugin loaded")           // 输出: [myplugin] Plugin loaded
//	    ctx.Log.Error("failed to init", err)    // 输出: [myplugin] failed to init  error=...
//	    return nil
//	}
type PluginLogger interface {
	// Info 记录 info 级别日志
	Info(msg string)
	// Infof 记录格式化 info 级别日志
	Infof(format string, args ...any)
	// Warn 记录 warn 级别日志
	Warn(msg string)
	// Warnf 记录格式化 warn 级别日志
	Warnf(format string, args ...any)
	// Error 记录 error 级别日志，附带 error 字段
	Error(msg string, err error)
	// Errorf 记录格式化 error 级别日志
	Errorf(format string, args ...any)
	// Debug 记录 debug 级别日志
	Debug(msg string)
	// Debugf 记录格式化 debug 级别日志
	Debugf(format string, args ...any)
	// WithField 返回附带额外字段的日志器副本（不改变原日志器）
	WithField(key string, value any) PluginLogger
}

// pluginLogger PluginLogger 的标准实现，基于 infra/logger 包
type pluginLogger struct {
	name   string        // 插件名，用于构造前缀
	fields logger.Fields // 附加字段
}

func newPluginLogger(pluginName string) PluginLogger {
	return &pluginLogger{
		name:   pluginName,
		fields: logger.Fields{},
	}
}

// entry 构建带 plugin 字段和附加字段的日志条目
func (l *pluginLogger) entry() *logger.LoggerWithFields {
	entry := logger.WithField("plugin", l.name)
	for k, v := range l.fields {
		entry = entry.WithField(k, v)
	}
	return entry
}

func (l *pluginLogger) prefix() string {
	return "[" + l.name + "] "
}

func (l *pluginLogger) Info(msg string) {
	l.entry().Info(l.prefix() + msg)
}

func (l *pluginLogger) Infof(format string, args ...any) {
	l.entry().Info(l.prefix() + fmt.Sprintf(format, args...))
}

func (l *pluginLogger) Warn(msg string) {
	l.entry().Warn(l.prefix() + msg)
}

func (l *pluginLogger) Warnf(format string, args ...any) {
	l.entry().Warn(l.prefix() + fmt.Sprintf(format, args...))
}

func (l *pluginLogger) Error(msg string, err error) {
	if err != nil {
		l.entry().WithError(err).Error(l.prefix() + msg)
	} else {
		l.entry().Error(l.prefix() + msg)
	}
}

func (l *pluginLogger) Errorf(format string, args ...any) {
	l.entry().Error(l.prefix() + fmt.Sprintf(format, args...))
}

func (l *pluginLogger) Debug(msg string) {
	l.entry().Debug(l.prefix() + msg)
}

func (l *pluginLogger) Debugf(format string, args ...any) {
	l.entry().Debug(l.prefix() + fmt.Sprintf(format, args...))
}

func (l *pluginLogger) WithField(key string, value any) PluginLogger {
	newFields := make(logger.Fields, len(l.fields)+1)
	for k, v := range l.fields {
		newFields[k] = v
	}
	newFields[key] = value
	return &pluginLogger{
		name:   l.name,
		fields: newFields,
	}
}
