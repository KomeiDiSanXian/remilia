package logger

import (
	"io"
	"os"
	"path/filepath"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var (
	// Logger Global logger instance
	Logger zerolog.Logger
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
			return err
		}

		// Open log file
		file, err := os.OpenFile(cfg.FilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			return err
		}

		writers = append(writers, file)
	}

	// If no writers specified, use stdout
	if len(writers) == 0 {
		writers = append(writers, os.Stdout)
	}

	// Create multi writer
	multi := io.MultiWriter(writers...)

	// Initialize global logger without Caller() to avoid performance overhead
	// Caller will be added only for important log levels (Error, Fatal, Panic)
	Logger = zerolog.New(multi).With().Timestamp().Logger()
	log.Logger = Logger

	return nil
}

// InitDefault initializes the logger with default configuration
func InitDefault() error {
	return Init(DefaultConfig())
}

// Fields is a helper type for structured logging fields
type Fields map[string]interface{}

// LoggerWithFields wraps zerolog logger to provide logrus-like API
type LoggerWithFields struct {
	logger zerolog.Logger
}

// WithField adds another field (chainable)
func (l *LoggerWithFields) WithField(key string, value interface{}) *LoggerWithFields {
	return &LoggerWithFields{
		logger: l.logger.With().Interface(key, value).Logger(),
	}
}

// WithFields adds multiple fields (chainable)
func (l *LoggerWithFields) WithFields(fields Fields) *LoggerWithFields {
	ctx := l.logger.With()
	for k, v := range fields {
		ctx = ctx.Interface(k, v)
	}
	return &LoggerWithFields{logger: ctx.Logger()}
}

// WithError adds an error field (chainable)
func (l *LoggerWithFields) WithError(err error) *LoggerWithFields {
	return &LoggerWithFields{
		logger: l.logger.With().Err(err).Logger(),
	}
}

// Info logs an info message
func (l *LoggerWithFields) Info(msg string) {
	l.logger.Info().Msg(msg)
}

// Infof logs a formatted info message
func (l *LoggerWithFields) Infof(format string, v ...interface{}) {
	l.logger.Info().Msgf(format, v...)
}

// Debug logs a debug message
func (l *LoggerWithFields) Debug(msg string) {
	l.logger.Debug().Msg(msg)
}

// Debugf logs a formatted debug message
func (l *LoggerWithFields) Debugf(format string, v ...interface{}) {
	l.logger.Debug().Msgf(format, v...)
}

// Warn logs a warning message
func (l *LoggerWithFields) Warn(msg string) {
	l.logger.Warn().Msg(msg)
}

// Warnf logs a formatted warning message
func (l *LoggerWithFields) Warnf(format string, v ...interface{}) {
	l.logger.Warn().Msgf(format, v...)
}

// Error logs an error message with caller information
func (l *LoggerWithFields) Error(msg string) {
	l.logger.Error().Caller(1).Msg(msg)
}

// Errorf logs a formatted error message with caller information
func (l *LoggerWithFields) Errorf(format string, v ...interface{}) {
	l.logger.Error().Caller(1).Msgf(format, v...)
}

// Fatal logs a fatal message with caller information and exits
func (l *LoggerWithFields) Fatal(msg string) {
	l.logger.Fatal().Caller(1).Msg(msg)
}

// Fatalf logs a formatted fatal message with caller information and exits
func (l *LoggerWithFields) Fatalf(format string, v ...interface{}) {
	l.logger.Fatal().Caller(1).Msgf(format, v...)
}

// WithFields creates a logger with multiple fields
func WithFields(fields Fields) *LoggerWithFields {
	ctx := Logger.With()
	for k, v := range fields {
		ctx = ctx.Interface(k, v)
	}
	return &LoggerWithFields{logger: ctx.Logger()}
}

// WithField creates a logger with a single field
func WithField(key string, value interface{}) *LoggerWithFields {
	return &LoggerWithFields{
		logger: Logger.With().Interface(key, value).Logger(),
	}
}

// WithError creates a logger with error field
func WithError(err error) *LoggerWithFields {
	return &LoggerWithFields{
		logger: Logger.With().Err(err).Logger(),
	}
}

// Log level functions

// Trace logs a trace message
func Trace(msg string) {
	Logger.Trace().Msg(msg)
}

// Tracef logs a formatted trace message
func Tracef(format string, v ...interface{}) {
	Logger.Trace().Msgf(format, v...)
}

// Debug logs a debug message
func Debug(msg string) {
	Logger.Debug().Msg(msg)
}

// Debugf logs a formatted debug message
func Debugf(format string, v ...interface{}) {
	Logger.Debug().Msgf(format, v...)
}

// Info logs an info message
func Info(msg string) {
	Logger.Info().Msg(msg)
}

// Infof logs a formatted info message
func Infof(format string, v ...interface{}) {
	Logger.Info().Msgf(format, v...)
}

// Warn logs a warning message
func Warn(msg string) {
	Logger.Warn().Msg(msg)
}

// Warnf logs a formatted warning message
func Warnf(format string, v ...interface{}) {
	Logger.Warn().Msgf(format, v...)
}

// Error logs an error message with caller information
func Error(msg string) {
	Logger.Error().Caller(1).Msg(msg)
}

// Errorf logs a formatted error message with caller information
func Errorf(format string, v ...interface{}) {
	Logger.Error().Caller(1).Msgf(format, v...)
}

// Fatal logs a fatal message with caller information and exits
func Fatal(msg string) {
	Logger.Fatal().Caller(1).Msg(msg)
}

// Fatalf logs a formatted fatal message with caller information and exits
func Fatalf(format string, v ...interface{}) {
	Logger.Fatal().Caller(1).Msgf(format, v...)
}

// Panic logs a panic message with caller information and panics
func Panic(msg string) {
	Logger.Panic().Caller(1).Msg(msg)
}

// Panicf logs a formatted panic message with caller information and panics
func Panicf(format string, v ...interface{}) {
	Logger.Panic().Caller(1).Msgf(format, v...)
}

func init() {
	// Initialize with default config on package load
	_ = InitDefault()
}
