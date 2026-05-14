package plugin

import (
	"fmt"
	"maps"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// Logger 插件上下文日志器接口
//
// 通过 [SetupContext.Log] 获取，自动携带插件名的结构化字段，无需手动写 "[PluginName] "。
//
// 示例：
//
//	Setup: func(ctx *plugin.SetupContext) (any, error) {
//	    ctx.Log.Info("Plugin loaded")
//	    ctx.Log.Infow("user banned", "userID", uid, "reason", reason)
//	    ctx.Log.Error("failed to init", err)
//	    return p, nil
//	}
type Logger interface {
	// Info 记录 info 级别日志
	Info(msg string)
	// Infof 记录格式化 info 级别日志
	Infof(format string, args ...any)
	// Infow 记录带结构化字段的 info 日志（w = with fields）
	// 用法：ctx.Log.Infow("user banned", "userID", uid, "reason", reason)
	// keysAndValues 应为交替的 key(string), value(any) 对；奇数个参数时末尾 key 被忽略。
	Infow(msg string, keysAndValues ...any)
	// Warn 记录 warn 级别日志
	Warn(msg string)
	// Warnf 记录格式化 warn 级别日志
	Warnf(format string, args ...any)
	// Warnw 记录带结构化字段的 warn 日志
	Warnw(msg string, keysAndValues ...any)
	// Error 记录 error 级别日志，附带 error 字段
	Error(msg string, err error)
	// Errorf 记录格式化 error 级别日志
	Errorf(format string, args ...any)
	// Debug 记录 debug 级别日志
	Debug(msg string)
	// Debugf 记录格式化 debug 级别日志
	Debugf(format string, args ...any)
	// Debugw 记录带结构化字段的 debug 日志
	Debugw(msg string, keysAndValues ...any)
	// WithField 返回附带额外字段的日志器副本（不改变原日志器）
	WithField(key string, value any) Logger
}

// pluginLogger Logger 的标准实现，基于 infra/logger 包
type pluginLogger struct {
	name   string        // 插件名，用于构造结构化字段
	fields logger.Fields // 附加字段
}

func newPluginLogger(pluginName string) Logger {
	return &pluginLogger{
		name:   pluginName,
		fields: logger.Fields{},
	}
}

// entry 构建带 plugin 字段和附加字段的日志条目
func (l *pluginLogger) entry() *logger.LogWithFields {
	entry := logger.WithField("plugin", l.name)
	for k, v := range l.fields {
		entry = entry.WithField(k, v)
	}
	return entry
}

// entryWithKVs 构建带额外 key-value 对的日志条目
func (l *pluginLogger) entryWithKVs(keysAndValues []any) *logger.LogWithFields {
	entry := l.entry()
	for i := 0; i+1 < len(keysAndValues); i += 2 {
		key, ok := keysAndValues[i].(string)
		if !ok {
			key = fmt.Sprintf("%v", keysAndValues[i])
		}
		entry = entry.WithField(key, keysAndValues[i+1])
	}
	return entry
}

func (l *pluginLogger) Info(msg string) {
	l.entry().Info(msg)
}

func (l *pluginLogger) Infof(format string, args ...any) {
	l.entry().Info(fmt.Sprintf(format, args...))
}

func (l *pluginLogger) Infow(msg string, keysAndValues ...any) {
	l.entryWithKVs(keysAndValues).Info(msg)
}

func (l *pluginLogger) Warn(msg string) {
	l.entry().Warn(msg)
}

func (l *pluginLogger) Warnf(format string, args ...any) {
	l.entry().Warn(fmt.Sprintf(format, args...))
}

func (l *pluginLogger) Warnw(msg string, keysAndValues ...any) {
	l.entryWithKVs(keysAndValues).Warn(msg)
}

func (l *pluginLogger) Error(msg string, err error) {
	if err != nil {
		l.entry().WithError(err).Error(msg)
	} else {
		l.entry().Error(msg)
	}
}

func (l *pluginLogger) Errorf(format string, args ...any) {
	l.entry().Error(fmt.Sprintf(format, args...))
}

func (l *pluginLogger) Debug(msg string) {
	l.entry().Debug(msg)
}

func (l *pluginLogger) Debugf(format string, args ...any) {
	l.entry().Debug(fmt.Sprintf(format, args...))
}

func (l *pluginLogger) Debugw(msg string, keysAndValues ...any) {
	l.entryWithKVs(keysAndValues).Debug(msg)
}

func (l *pluginLogger) WithField(key string, value any) Logger {
	newFields := make(logger.Fields, len(l.fields)+1)
	maps.Copy(newFields, l.fields)
	newFields[key] = value
	return &pluginLogger{
		name:   l.name,
		fields: newFields,
	}
}
