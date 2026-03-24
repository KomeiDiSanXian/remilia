package logger

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var (
	// globalLogger 是包级别的 zerolog 实例。通过包级便捷函数（Info、Debug、WithField 等）访问。
	// 外部调用方不应直接依赖 zerolog.Logger 类型。
	globalLogger zerolog.Logger

	// logFile 保存当前打开的日志文件句柄，用于关闭和轮转
	logFile   *os.File
	logFileMu sync.Mutex
)

// Config 日志配置
type Config struct {
	Level      string // 日志级别：trace, debug, info, warn, error, fatal, panic
	Console    bool   // 是否启用控制台输出
	File       bool   // 是否启用文件输出
	FilePath   string // 日志文件路径
	TimeFormat string // 时间格式，默认："2006-01-02 15:04:05"
}

// DefaultConfig 返回默认日志配置
func DefaultConfig() Config {
	return Config{
		Level:      "info",
		Console:    true,
		File:       false,
		FilePath:   "logs/remilia.log",
		TimeFormat: "2006-01-02 15:04:05",
	}
}

// Init 使用指定配置初始化全局日志记录器
func Init(cfg Config) error {
	logFileMu.Lock()
	// 关闭上一次打开的日志文件（如多次调用 Init 或热重载时防止 fd 泄漏）
	if logFile != nil {
		_ = logFile.Close()
		logFile = nil
	}
	logFileMu.Unlock()
	// 设置时间格式
	timeFormat := cfg.TimeFormat
	if timeFormat == "" {
		timeFormat = "2006-01-02 15:04:05"
	}
	zerolog.TimeFieldFormat = timeFormat

	// 解析日志级别
	level, err := zerolog.ParseLevel(cfg.Level)
	if err != nil {
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)

	// 准备 Writer 列表
	var writers []io.Writer

	// 控制台输出（带颜色）
	if cfg.Console {
		consoleWriter := zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: timeFormat,
			NoColor:    false, // 启用颜色
		}
		writers = append(writers, consoleWriter)
	}

	// 文件输出
	if cfg.File {
		// 如目录不存在则创建
		logDir := filepath.Dir(cfg.FilePath)
		if err := os.MkdirAll(logDir, 0755); err != nil {
			// 回退到仅控制台输出
			_, _ = fmt.Fprintf(os.Stderr, "无法创建日志目录：%v，回退到仅控制台输出\n", err)
			cfg.File = false
			cfg.Console = true
		} else {
			// 打开日志文件
			file, err := os.OpenFile(cfg.FilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
			if err != nil {
				// 回退到仅控制台输出
				_, _ = fmt.Fprintf(os.Stderr, "无法打开日志文件：%v，回退到仅控制台输出\n", err)
				cfg.File = false
				cfg.Console = true
			} else {
				writers = append(writers, file)
				logFileMu.Lock()
				logFile = file
				logFileMu.Unlock()
			}
		}
	}

	// 若无 Writer，则使用 stdout
	if len(writers) == 0 {
		writers = append(writers, os.Stdout)
	}

	// 创建多路 Writer
	multi := io.MultiWriter(writers...)

	// 初始化全局 logger，不附加 Caller() 以避免性能开销
	// 仅在重要日志级别（Error、Fatal、Panic）时附加 Caller 信息
	globalLogger = zerolog.New(multi).With().Timestamp().Logger()
	log.Logger = globalLogger

	return nil
}

// InitDefault 使用默认配置初始化日志记录器
func InitDefault() error {
	return Init(DefaultConfig())
}

// CloseLogFile 关闭当前日志文件句柄。
// 应在进程退出或 Bot.Stop() 时调用，确保缓冲区 flush 并释放文件描述符。
// 多次调用是安全的。
func CloseLogFile() {
	logFileMu.Lock()
	defer logFileMu.Unlock()
	if logFile != nil {
		_ = logFile.Close()
		logFile = nil
	}
}

// FieldsPool 是用于复用 Fields map 的对象池，减少内存分配
var FieldsPool = sync.Pool{
	New: func() any {
		return make(Fields, 8) // 预分配 8 个字段的容量
	},
}

// GetFields 从池中获取一个 Fields 对象
//
// 使用示例:
//
//	fields := logger.GetFields()
//	defer logger.PutFields(fields)
//	fields["key"] = "value"
//	logger.WithFields(fields).Info("message")
func GetFields() Fields {
	return FieldsPool.Get().(Fields)
}

// PutFields 将 Fields 对象归还到池中
//
// 注意：归还前会清空所有字段
func PutFields(f Fields) {
	// 清空所有字段
	for k := range f {
		delete(f, k)
	}
	FieldsPool.Put(f)
}

// Fields 是结构化日志字段的辅助类型
type Fields map[string]any

// LogWithFields 包装 zerolog logger，提供类似 logrus 的 API
type LogWithFields struct {
	logger zerolog.Logger
}

// WithField 追加一个字段（支持链式调用）
func (l *LogWithFields) WithField(key string, value any) *LogWithFields {
	return &LogWithFields{
		logger: l.logger.With().Interface(key, value).Logger(),
	}
}

// WithFields 追加多个字段（支持链式调用）
func (l *LogWithFields) WithFields(fields Fields) *LogWithFields {
	ctx := l.logger.With()
	for k, v := range fields {
		ctx = ctx.Interface(k, v)
	}
	return &LogWithFields{logger: ctx.Logger()}
}

// WithError 追加错误字段（支持链式调用）
func (l *LogWithFields) WithError(err error) *LogWithFields {
	return &LogWithFields{
		logger: l.logger.With().Err(err).Logger(),
	}
}

// Info 输出 info 级别日志
func (l *LogWithFields) Info(msg string) {
	l.logger.Info().Msg(msg)
}

// Infof 输出格式化 info 级别日志
func (l *LogWithFields) Infof(format string, v ...any) {
	l.logger.Info().Msgf(format, v...)
}

// Debug 输出 debug 级别日志
func (l *LogWithFields) Debug(msg string) {
	l.logger.Debug().Msg(msg)
}

// Debugf 输出格式化 debug 级别日志
func (l *LogWithFields) Debugf(format string, v ...any) {
	l.logger.Debug().Msgf(format, v...)
}

// Warn 输出 warn 级别日志
func (l *LogWithFields) Warn(msg string) {
	l.logger.Warn().Msg(msg)
}

// Warnf 输出格式化 warn 级别日志
func (l *LogWithFields) Warnf(format string, v ...any) {
	l.logger.Warn().Msgf(format, v...)
}

// Error 输出带调用位置信息的 error 级别日志
func (l *LogWithFields) Error(msg string) {
	l.logger.Error().Caller(1).Msg(msg)
}

// Errorf 输出带调用位置信息的格式化 error 级别日志
func (l *LogWithFields) Errorf(format string, v ...any) {
	l.logger.Error().Caller(1).Msgf(format, v...)
}

// Fatal 输出带调用位置信息的 fatal 级别日志并退出
func (l *LogWithFields) Fatal(msg string) {
	l.logger.Fatal().Caller(1).Msg(msg)
}

// Fatalf 输出带调用位置信息的格式化 fatal 级别日志并退出
func (l *LogWithFields) Fatalf(format string, v ...any) {
	l.logger.Fatal().Caller(1).Msgf(format, v...)
}

// WithFields 创建带多个字段的 logger
func WithFields(fields Fields) *LogWithFields {
	ctx := globalLogger.With()
	for k, v := range fields {
		ctx = ctx.Interface(k, v)
	}
	return &LogWithFields{logger: ctx.Logger()}
}

// WithField 创建带单个字段的 logger
func WithField(key string, value any) *LogWithFields {
	return &LogWithFields{
		logger: globalLogger.With().Interface(key, value).Logger(),
	}
}

// WithError 创建带错误字段的 logger
func WithError(err error) *LogWithFields {
	return &LogWithFields{
		logger: globalLogger.With().Err(err).Logger(),
	}
}

// 日志级别函数

// Trace 输出 trace 级别日志
func Trace(msg string) {
	globalLogger.Trace().Msg(msg)
}

// Tracef 输出格式化 trace 级别日志
func Tracef(format string, v ...any) {
	globalLogger.Trace().Msgf(format, v...)
}

// Debug 输出 debug 级别日志
func Debug(msg string) {
	globalLogger.Debug().Msg(msg)
}

// Debugf 输出格式化 debug 级别日志
func Debugf(format string, v ...any) {
	globalLogger.Debug().Msgf(format, v...)
}

// Info 输出 info 级别日志
func Info(msg string) {
	globalLogger.Info().Msg(msg)
}

// Infof 输出格式化 info 级别日志
func Infof(format string, v ...any) {
	globalLogger.Info().Msgf(format, v...)
}

// Warn 输出 warn 级别日志
func Warn(msg string) {
	globalLogger.Warn().Msg(msg)
}

// Warnf 输出格式化 warn 级别日志
func Warnf(format string, v ...any) {
	globalLogger.Warn().Msgf(format, v...)
}

// Error 输出带调用位置信息的 error 级别日志
func Error(msg string) {
	globalLogger.Error().Caller(1).Msg(msg)
}

// Errorf 输出带调用位置信息的格式化 error 级别日志
func Errorf(format string, v ...any) {
	globalLogger.Error().Caller(1).Msgf(format, v...)
}

// Fatal 输出带调用位置信息的 fatal 级别日志并退出
func Fatal(msg string) {
	globalLogger.Fatal().Caller(1).Msg(msg)
}

// Fatalf 输出带调用位置信息的格式化 fatal 级别日志并退出
func Fatalf(format string, v ...any) {
	globalLogger.Fatal().Caller(1).Msgf(format, v...)
}

// Panic 输出带调用位置信息的 panic 级别日志并 panic
func Panic(msg string) {
	globalLogger.Panic().Caller(1).Msg(msg)
}

// Panicf 输出带调用位置信息的格式化 panic 级别日志并 panic
func Panicf(format string, v ...any) {
	globalLogger.Panic().Caller(1).Msgf(format, v...)
}

// InitNop 初始化一个静默的 logger（丢弃所有输出）。
// 用于测试场景，避免控制台产生噪声日志。
//
// 示例：
//
//	func TestMain(m *testing.M) {
//	    logger.InitNop()
//	    os.Exit(m.Run())
//	}
func InitNop() {
	globalLogger = zerolog.Nop()
	log.Logger = globalLogger
}

// InitTest 初始化一个仅输出 Error 及以上级别的测试 logger。
// 相比 InitNop，保留了关键错误日志，便于测试时排查问题。
func InitTest() {
	zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	globalLogger = zerolog.New(os.Stderr).With().Timestamp().Logger()
	log.Logger = globalLogger
}

func init() {
	// 包加载时使用默认配置初始化
	_ = InitDefault()
}
