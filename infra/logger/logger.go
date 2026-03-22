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
	// globalLogger is the package-level zerolog instance. Access via the package-level
	// convenience functions (Info, Debug, WithField, etc.). External callers must not
	// depend on the zerolog.Logger type directly.
	globalLogger zerolog.Logger

	// logFile 保存当前打开的日志文件句柄，用于关闭和轮转
	logFile   *os.File
	logFileMu sync.Mutex
)

// Config logger configuration
type Config struct {
	Level      string // log level: trace, debug, info, warn, error, fatal, panic
	Console    bool   // enable console output
	File       bool   // enable file output
	FilePath   string // log file path
	TimeFormat string // time format, default: "2006-01-02 15:04:05"
}

// DefaultConfig returns default logger configuration
func DefaultConfig() Config {
	return Config{
		Level:      "info",
		Console:    true,
		File:       false,
		FilePath:   "logs/remilia.log",
		TimeFormat: "2006-01-02 15:04:05",
	}
}

// Init initializes the global logger with the given configuration
func Init(cfg Config) error {
	logFileMu.Lock()
	// 关闭上一次打开的日志文件（如多次调用 Init 或热重载时防止 fd 泄漏）
	if logFile != nil {
		_ = logFile.Close()
		logFile = nil
	}
	logFileMu.Unlock()
	// Set time format
	timeFormat := cfg.TimeFormat
	if timeFormat == "" {
		timeFormat = "2006-01-02 15:04:05"
	}
	zerolog.TimeFieldFormat = timeFormat

	// Parse log level
	level, err := zerolog.ParseLevel(cfg.Level)
	if err != nil {
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)

	// Prepare writers
	var writers []io.Writer

	// Console output with colors
	if cfg.Console {
		consoleWriter := zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: timeFormat,
			NoColor:    false, // Enable colors
		}
		writers = append(writers, consoleWriter)
	}

	// File output
	if cfg.File {
		// Create logs directory if not exists
		logDir := filepath.Dir(cfg.FilePath)
		if err := os.MkdirAll(logDir, 0755); err != nil {
			// Fallback to console only
			_, _ = fmt.Fprintf(os.Stderr, "Failed to create log directory: %v, falling back to console only\n", err)
			cfg.File = false
			cfg.Console = true
		} else {
			// Open log file
			file, err := os.OpenFile(cfg.FilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
			if err != nil {
				// Fallback to console only
				_, _ = fmt.Fprintf(os.Stderr, "Failed to open log file: %v, falling back to console only\n", err)
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

	// If no writers specified, use stdout
	if len(writers) == 0 {
		writers = append(writers, os.Stdout)
	}

	// Create multi writer
	multi := io.MultiWriter(writers...)

	// Initialize global logger without Caller() to avoid performance overhead
	// Caller will be added only for important log levels (Error, Fatal, Panic)
	globalLogger = zerolog.New(multi).With().Timestamp().Logger()
	log.Logger = globalLogger

	return nil
}

// InitDefault initializes the logger with default configuration
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

// Fields is a helper type for structured logging fields
type Fields map[string]any

// LogWithFields wraps zerolog logger to provide logrus-like API
type LogWithFields struct {
	logger zerolog.Logger
}

// WithField adds another field (chainable)
func (l *LogWithFields) WithField(key string, value any) *LogWithFields {
	return &LogWithFields{
		logger: l.logger.With().Interface(key, value).Logger(),
	}
}

// WithFields adds multiple fields (chainable)
func (l *LogWithFields) WithFields(fields Fields) *LogWithFields {
	ctx := l.logger.With()
	for k, v := range fields {
		ctx = ctx.Interface(k, v)
	}
	return &LogWithFields{logger: ctx.Logger()}
}

// WithError adds an error field (chainable)
func (l *LogWithFields) WithError(err error) *LogWithFields {
	return &LogWithFields{
		logger: l.logger.With().Err(err).Logger(),
	}
}

// Info logs an info message
func (l *LogWithFields) Info(msg string) {
	l.logger.Info().Msg(msg)
}

// Infof logs a formatted info message
func (l *LogWithFields) Infof(format string, v ...any) {
	l.logger.Info().Msgf(format, v...)
}

// Debug logs a debug message
func (l *LogWithFields) Debug(msg string) {
	l.logger.Debug().Msg(msg)
}

// Debugf logs a formatted debug message
func (l *LogWithFields) Debugf(format string, v ...any) {
	l.logger.Debug().Msgf(format, v...)
}

// Warn logs a warning message
func (l *LogWithFields) Warn(msg string) {
	l.logger.Warn().Msg(msg)
}

// Warnf logs a formatted warning message
func (l *LogWithFields) Warnf(format string, v ...any) {
	l.logger.Warn().Msgf(format, v...)
}

// Error logs an error message with caller information
func (l *LogWithFields) Error(msg string) {
	l.logger.Error().Caller(1).Msg(msg)
}

// Errorf logs a formatted error message with caller information
func (l *LogWithFields) Errorf(format string, v ...any) {
	l.logger.Error().Caller(1).Msgf(format, v...)
}

// Fatal logs a fatal message with caller information and exits
func (l *LogWithFields) Fatal(msg string) {
	l.logger.Fatal().Caller(1).Msg(msg)
}

// Fatalf logs a formatted fatal message with caller information and exits
func (l *LogWithFields) Fatalf(format string, v ...any) {
	l.logger.Fatal().Caller(1).Msgf(format, v...)
}

// WithFields creates a logger with multiple fields
func WithFields(fields Fields) *LogWithFields {
	ctx := globalLogger.With()
	for k, v := range fields {
		ctx = ctx.Interface(k, v)
	}
	return &LogWithFields{logger: ctx.Logger()}
}

// WithField creates a logger with a single field
func WithField(key string, value any) *LogWithFields {
	return &LogWithFields{
		logger: globalLogger.With().Interface(key, value).Logger(),
	}
}

// WithError creates a logger with error field
func WithError(err error) *LogWithFields {
	return &LogWithFields{
		logger: globalLogger.With().Err(err).Logger(),
	}
}

// Log level functions

// Trace logs a trace message
func Trace(msg string) {
	globalLogger.Trace().Msg(msg)
}

// Tracef logs a formatted trace message
func Tracef(format string, v ...any) {
	globalLogger.Trace().Msgf(format, v...)
}

// Debug logs a debug message
func Debug(msg string) {
	globalLogger.Debug().Msg(msg)
}

// Debugf logs a formatted debug message
func Debugf(format string, v ...any) {
	globalLogger.Debug().Msgf(format, v...)
}

// Info logs an info message
func Info(msg string) {
	globalLogger.Info().Msg(msg)
}

// Infof logs a formatted info message
func Infof(format string, v ...any) {
	globalLogger.Info().Msgf(format, v...)
}

// Warn logs a warning message
func Warn(msg string) {
	globalLogger.Warn().Msg(msg)
}

// Warnf logs a formatted warning message
func Warnf(format string, v ...any) {
	globalLogger.Warn().Msgf(format, v...)
}

// Error logs an error message with caller information
func Error(msg string) {
	globalLogger.Error().Caller(1).Msg(msg)
}

// Errorf logs a formatted error message with caller information
func Errorf(format string, v ...any) {
	globalLogger.Error().Caller(1).Msgf(format, v...)
}

// Fatal logs a fatal message with caller information and exits
func Fatal(msg string) {
	globalLogger.Fatal().Caller(1).Msg(msg)
}

// Fatalf logs a formatted fatal message with caller information and exits
func Fatalf(format string, v ...any) {
	globalLogger.Fatal().Caller(1).Msgf(format, v...)
}

// Panic logs a panic message with caller information and panics
func Panic(msg string) {
	globalLogger.Panic().Caller(1).Msg(msg)
}

// Panicf logs a formatted panic message with caller information and panics
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
	// Initialize with default config on package load
	_ = InitDefault()
}
