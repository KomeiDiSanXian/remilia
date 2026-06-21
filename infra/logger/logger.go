package logger

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var (
	// globalLogger 是包级别的 zerolog 实例。通过 defaultLogger 的便捷方法访问。
	// 外部调用方不应直接依赖 zerolog.Logger 类型。
	globalLogger zerolog.Logger

	// defaultLogger 是包级默认 Logger 实例。所有包级函数（Info、Debug、WithField 等）
	// 均委托给此实例。可通过 SetLogger() 替换，或创建独立 Logger 实例实现日志隔离。
	// 使用 atomic.Pointer 保护并发读写安全（避免 SetLogger 与包级函数之间的数据竞争）。
	defaultLogger atomic.Pointer[Logger]

	// logFile 保存当前打开的日志文件句柄，用于关闭和轮转
	logFile   *os.File
	logFileMu sync.Mutex
)

// Config 日志配置。
//
// 此类型同时作为 config.Config.Log 的配置结构体（config 包通过类型别名引用），
// 因此所有字段均带有 yaml/mapstructure tag，可直接被 YAML 反序列化。
//
// Format 与 Console 字段的关系：
//   - Format 为 YAML 向后兼容字段，"text" 等价于 Console=true，"json" 等价于 Console=false。
//   - 若同时设置 Console 和 Format，Console 优先（Init 中显式处理）。
type Config struct {
	// Level 日志级别：trace, debug, info, warn, error, fatal, panic
	Level string `yaml:"level" mapstructure:"level"`

	// Format 输出格式（向后兼容）："text"（人类可读）或 "json"（结构化）。
	// 等效于设置 Console：Format="text" → Console=true；Format="json" → Console=false。
	// 若已明确设置 Console，此字段将被忽略。
	Format string `yaml:"format" mapstructure:"format"`

	// Console 是否启用控制台输出（zerolog.ConsoleWriter，带颜色的人类可读格式）
	Console bool `yaml:"console" mapstructure:"console"`

	// File 是否启用文件输出（JSON 格式写入 FilePath）
	File bool `yaml:"file" mapstructure:"file"`

	// FilePath 日志文件路径（File=true 时生效）
	FilePath string `yaml:"file_path" mapstructure:"file_path"`

	// TimeFormat 时间戳格式，默认："2006-01-02 15:04:05"
	TimeFormat string `yaml:"time_format" mapstructure:"time_format"`
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

// Logger 是一个可实例化的日志记录器，封装 zerolog 并提供与包级函数相同的便捷 API。
//
// 使用场景：
//   - 多 Bot 实例需要独立日志级别/输出时，创建各自 Logger
//   - 测试中需要捕获日志输出
//
// 示例：
//
//	l := logger.NewLogger(zerolog.New(os.Stdout))
//	l.Info("hello")
//	l.WithField("key", "val").Warn("with fields")
//
// 包级函数（Info, Debug, WithField 等）始终委托给 defaultLogger。
type Logger struct {
	l zerolog.Logger
}

// NewLogger 创建一个新的 Logger 实例。
func NewLogger(l zerolog.Logger) *Logger {
	return &Logger{l: l}
}

// SetLevel 运行时动态调整日志级别。
// level 取值：trace, debug, info, warn, error, fatal, panic。
//
// 示例：
//
//	logger.SetLevel("debug") // 临时开启 Debug 排查问题
func SetLevel(level string) error {
	l, err := zerolog.ParseLevel(level)
	if err != nil {
		return fmt.Errorf("logger: invalid level %q (valid: trace, debug, info, warn, error, fatal, panic)", level)
	}
	zerolog.SetGlobalLevel(l)
	return nil
}

// SetLogger 替换包级默认 Logger 实例。
// 多 Bot 实例场景可独立创建 Logger 并替换默认值。
func SetLogger(l *Logger) {
	defaultLogger.Store(l)
}

// DefaultLogger 返回当前包级默认 Logger 实例。
func DefaultLogger() *Logger {
	return defaultLogger.Load()
}

// Zap 返回底层 zerolog.Logger（供适配器/桥接使用）。
func (l *Logger) Zap() zerolog.Logger {
	return l.l
}

// Trace 输出 trace 级别日志
func (l *Logger) Trace(msg string) {
	l.l.Trace().Msg(msg)
}

// Tracef 输出格式化 trace 级别日志
func (l *Logger) Tracef(format string, v ...any) {
	l.l.Trace().Msgf(format, v...)
}

// Debug 输出 debug 级别日志
func (l *Logger) Debug(msg string) {
	l.l.Debug().Msg(msg)
}

// Debugf 输出格式化 debug 级别日志
func (l *Logger) Debugf(format string, v ...any) {
	l.l.Debug().Msgf(format, v...)
}

// Info 输出 info 级别日志
func (l *Logger) Info(msg string) {
	l.l.Info().Msg(msg)
}

// Infof 输出格式化 info 级别日志
func (l *Logger) Infof(format string, v ...any) {
	l.l.Info().Msgf(format, v...)
}

// Warn 输出 warn 级别日志
func (l *Logger) Warn(msg string) {
	l.l.Warn().Msg(msg)
}

// Warnf 输出格式化 warn 级别日志
func (l *Logger) Warnf(format string, v ...any) {
	l.l.Warn().Msgf(format, v...)
}

// Error 输出带调用位置信息的 error 级别日志
func (l *Logger) Error(msg string) {
	l.l.Error().Caller(1).Msg(msg)
}

// Errorf 输出带调用位置信息的格式化 error 级别日志
func (l *Logger) Errorf(format string, v ...any) {
	l.l.Error().Caller(1).Msgf(format, v...)
}

// Fatal 输出带调用位置信息的 fatal 级别日志并退出
func (l *Logger) Fatal(msg string) {
	l.l.Fatal().Caller(1).Msg(msg)
}

// Fatalf 输出带调用位置信息的格式化 fatal 级别日志并退出
func (l *Logger) Fatalf(format string, v ...any) {
	l.l.Fatal().Caller(1).Msgf(format, v...)
}

// Panic 输出带调用位置信息的 panic 级别日志并 panic
func (l *Logger) Panic(msg string) {
	l.l.Panic().Caller(1).Msg(msg)
}

// Panicf 输出带调用位置信息的格式化 panic 级别日志并 panic
func (l *Logger) Panicf(format string, v ...any) {
	l.l.Panic().Caller(1).Msgf(format, v...)
}

// WithFields 创建带多个 fields 的 LogWithFields
func (l *Logger) WithFields(fields Fields) *LogWithFields {
	ctx := l.l.With()
	for k, v := range fields {
		ctx = ctx.Interface(k, v)
	}
	return &LogWithFields{logger: ctx.Logger()}
}

// WithField 创建带单个 field 的 LogWithFields
func (l *Logger) WithField(key string, value any) *LogWithFields {
	return &LogWithFields{
		logger: l.l.With().Interface(key, value).Logger(),
	}
}

// WithError 创建带 error field 的 LogWithFields
func (l *Logger) WithError(err error) *LogWithFields {
	return &LogWithFields{
		logger: l.l.With().Err(err).Logger(),
	}
}

// Validate 验证日志配置有效性
func (c *Config) Validate() error {
	validLevels := map[string]bool{
		"trace": true, "debug": true, "info": true,
		"warn": true, "error": true, "fatal": true, "panic": true,
	}
	if c.Level != "" && !validLevels[c.Level] {
		return fmt.Errorf("log.level must be one of [trace, debug, info, warn, error, fatal, panic], got '%s'", c.Level)
	}
	validFormats := map[string]bool{"": true, "text": true, "json": true}
	if !validFormats[c.Format] {
		return fmt.Errorf("log.format must be one of [text, json], got '%s'", c.Format)
	}
	return nil
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

	// 将 Format 字段应用为 Console 的回退默认值（向后兼容 config.yaml 的 format: text/json）。
	// 仅当 Console 和 File 均未显式设置时才生效，避免覆盖调用方的明确意图。
	if !cfg.Console && !cfg.File {
		switch cfg.Format {
		case "text":
			cfg.Console = true
		case "json", "":
			// json 模式：不用 ConsoleWriter，输出到 stdout（后续 len(writers)==0 时自动兜底）
		}
	}

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

	// 添加自定义捕获 Writer（用于日志流）
	extraWriter := GetExtraWriter()
	if extraWriter != nil {
		writers = append(writers, extraWriter)
	}

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
			file, err := os.OpenFile(cfg.FilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
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
	defaultLogger.Store(&Logger{l: globalLogger})

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
	return defaultLogger.Load().WithFields(fields)
}

// WithField 创建带单个字段的 logger
func WithField(key string, value any) *LogWithFields {
	return defaultLogger.Load().WithField(key, value)
}

// WithError 创建带错误字段的 logger
func WithError(err error) *LogWithFields {
	return defaultLogger.Load().WithError(err)
}

// 日志级别函数 — 委托给 defaultLogger

// Trace 输出 trace 级别日志
func Trace(msg string) {
	defaultLogger.Load().Trace(msg)
}

// Tracef 输出格式化 trace 级别日志
func Tracef(format string, v ...any) {
	defaultLogger.Load().Tracef(format, v...)
}

// Debug 输出 debug 级别日志
func Debug(msg string) {
	defaultLogger.Load().Debug(msg)
}

// Debugf 输出格式化 debug 级别日志
func Debugf(format string, v ...any) {
	defaultLogger.Load().Debugf(format, v...)
}

// Info 输出 info 级别日志
func Info(msg string) {
	defaultLogger.Load().Info(msg)
}

// Infof 输出格式化 info 级别日志
func Infof(format string, v ...any) {
	defaultLogger.Load().Infof(format, v...)
}

// Warn 输出 warn 级别日志
func Warn(msg string) {
	defaultLogger.Load().Warn(msg)
}

// Warnf 输出格式化 warn 级别日志
func Warnf(format string, v ...any) {
	defaultLogger.Load().Warnf(format, v...)
}

// Error 输出带调用位置信息的 error 级别日志
func Error(msg string) {
	defaultLogger.Load().Error(msg)
}

// Errorf 输出带调用位置信息的格式化 error 级别日志
func Errorf(format string, v ...any) {
	defaultLogger.Load().Errorf(format, v...)
}

// Fatal 输出带调用位置信息的 fatal 级别日志并退出
func Fatal(msg string) {
	defaultLogger.Load().Fatal(msg)
}

// Fatalf 输出带调用位置信息的格式化 fatal 级别日志并退出
func Fatalf(format string, v ...any) {
	defaultLogger.Load().Fatalf(format, v...)
}

// Panic 输出带调用位置信息的 panic 级别日志并 panic
func Panic(msg string) {
	defaultLogger.Load().Panic(msg)
}

// Panicf 输出带调用位置信息的格式化 panic 级别日志并 panic
func Panicf(format string, v ...any) {
	defaultLogger.Load().Panicf(format, v...)
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
	defaultLogger.Store(&Logger{l: globalLogger})
}

// InitTest 初始化一个仅输出 Error 及以上级别的测试 logger。
// 相比 InitNop，保留了关键错误日志，便于测试时排查问题。
func InitTest() {
	zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	globalLogger = zerolog.New(os.Stderr).With().Timestamp().Logger()
	log.Logger = globalLogger
	defaultLogger.Store(&Logger{l: globalLogger})
}

// ---- 外部 Writer 支持（用于日志流） ----

var extraWriterMu sync.RWMutex
var extraWriter io.Writer

// SetExtraWriter 注册一个额外的 io.Writer，所有日志同时写入此 writer。
// 必须在 logger.Init() 之前调用才能生效。
func SetExtraWriter(w io.Writer) {
	extraWriterMu.Lock()
	defer extraWriterMu.Unlock()
	extraWriter = w
}

// GetExtraWriter 返回已注册的额外 writer。
func GetExtraWriter() io.Writer {
	extraWriterMu.RLock()
	defer extraWriterMu.RUnlock()
	return extraWriter
}

func init() {
	// 初始化时使用默认配置初始化
	_ = InitDefault()
}
