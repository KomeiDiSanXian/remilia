// Package audit 提供结构化审计日志功能。
//
// 审计日志记录系统中的关键操作（用户登录、命令执行、权限变更、插件加载等），
// 支持写入文件或自定义后端，可用于合规审查和安全分析。
//
// 典型用法：
//
//	logger := audit.NewLogger(audit.Config{
//	    Enable:   true,
//	    FilePath: "audit.log",
//	})
//	logger.Log(audit.Entry{
//	    Action: audit.ActionCommandExecute,
//	    Actor:  userID,
//	    Level:  audit.LevelInfo,
//	})
package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// Level 审计日志级别
type Level int

const (
	// LevelInfo 信息级别（普通操作）
	LevelInfo Level = iota
	// LevelWarn 警告级别（需要注意的操作）
	LevelWarn
	// LevelError 错误级别（失败的操作）
	LevelError
	// LevelCritical 严重级别（安全相关操作）
	LevelCritical
)

// String 返回级别字符串
func (l Level) String() string {
	switch l {
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	case LevelCritical:
		return "CRITICAL"
	default:
		return "UNKNOWN"
	}
}

// Action 操作类型
type Action string

const (
	ActionUserLogin    Action = "user.login"
	ActionUserLogout   Action = "user.logout"
	ActionUserRegister Action = "user.register"

	ActionCommandExecute Action = "command.execute"
	ActionCommandFail    Action = "command.fail"

	ActionPluginLoad    Action = "plugin.load"
	ActionPluginUnload  Action = "plugin.unload"
	ActionPluginEnable  Action = "plugin.enable"
	ActionPluginDisable Action = "plugin.disable"

	ActionConfigUpdate Action = "config.update"
	ActionConfigReload Action = "config.reload"

	ActionSystemStart    Action = "system.start"
	ActionSystemShutdown Action = "system.shutdown"
	ActionSystemRestart  Action = "system.restart"

	ActionPermissionGrant  Action = "permission.grant"
	ActionPermissionRevoke Action = "permission.revoke"

	ActionDataCreate Action = "data.create"
	ActionDataRead   Action = "data.read"
	ActionDataUpdate Action = "data.update"
	ActionDataDelete Action = "data.delete"
)

// Entry 审计日志条目
type Entry struct {
	// ID 日志唯一标识
	ID string `json:"id"`

	// Timestamp 时间戳
	Timestamp time.Time `json:"timestamp"`

	// Level 日志级别
	Level Level `json:"level"`

	// Action 操作类型
	Action Action `json:"action"`

	// Actor 操作者（用户ID、系统等）
	Actor string `json:"actor"`

	// Target 操作目标
	Target string `json:"target,omitempty"`

	// Resource 资源类型
	Resource string `json:"resource,omitempty"`

	// Result 操作结果（success/failure）
	Result string `json:"result"`

	// Message 描述信息
	Message string `json:"message,omitempty"`

	// Metadata 附加元数据
	Metadata map[string]any `json:"metadata,omitempty"`

	// IP 来源IP地址
	IP string `json:"ip,omitempty"`

	// UserAgent 用户代理
	UserAgent string `json:"user_agent,omitempty"`

	// Duration 操作耗时（毫秒）
	Duration int64 `json:"duration_ms,omitempty"`

	// Error 错误信息
	Error string `json:"error,omitempty"`
}

// Config 审计日志配置
type Config struct {
	// Enabled 是否启用审计日志
	Enabled bool

	// OutputFile 输出文件路径
	OutputFile string

	// MaxSize 单个文件最大大小（MB）
	MaxSize int

	// MaxBackups 保留的备份文件数量
	MaxBackups int

	// MaxAge 保留天数
	MaxAge int

	// Compress 是否压缩备份文件
	Compress bool

	// BufferSize 缓冲区大小
	BufferSize int

	// FlushInterval 刷新间隔
	FlushInterval time.Duration

	// MinLevel 最低记录级别
	MinLevel Level

	// AsyncWrite 是否异步写入
	AsyncWrite bool
}

// DefaultConfig 返回默认配置
func DefaultConfig() Config {
	return Config{
		Enabled:       false,
		OutputFile:    "./logs/audit.log",
		MaxSize:       100, // 100MB
		MaxBackups:    10,
		MaxAge:        30, // 30天
		Compress:      true,
		BufferSize:    1000,
		FlushInterval: 5 * time.Second,
		MinLevel:      LevelInfo,
		AsyncWrite:    true,
	}
}

// Logger 审计日志记录器
type Logger struct {
	config  Config
	file    *os.File
	buffer  chan *Entry
	mu      sync.Mutex
	stopCh  chan struct{}
	wg      sync.WaitGroup
	counter uint64
}

// NewLogger 创建审计日志记录器
func NewLogger(config Config) (*Logger, error) {
	if !config.Enabled {
		return &Logger{
			config: config,
			buffer: make(chan *Entry, 1),
			stopCh: make(chan struct{}),
		}, nil
	}

	// 创建输出目录
	dir := filepath.Dir(config.OutputFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create audit log directory: %w", err)
	}

	// 打开日志文件
	file, err := os.OpenFile(config.OutputFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open audit log file: %w", err)
	}

	l := &Logger{
		config: config,
		file:   file,
		buffer: make(chan *Entry, config.BufferSize),
		stopCh: make(chan struct{}),
	}

	// 启动异步写入
	if config.AsyncWrite {
		l.wg.Add(1)
		go l.writeLoop()
	}

	logger.Info("[Audit] Audit logger initialized")
	return l, nil
}

// Log 记录审计日志
func (l *Logger) Log(entry *Entry) {
	if !l.config.Enabled {
		return
	}

	// 检查级别
	if entry.Level < l.config.MinLevel {
		return
	}

	// 设置时间戳和ID
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}
	if entry.ID == "" {
		entry.ID = l.generateID()
	}

	if l.config.AsyncWrite {
		// 异步写入：发送到缓冲区
		select {
		case l.buffer <- entry:
		default:
			logger.Warn("[Audit] Audit log buffer full, dropping entry")
		}
	} else {
		// 同步写入：直接写入文件，不经过 channel（避免无消费者时丢失日志）
		l.writeBatch([]*Entry{entry})
	}
}

// writeLoop 异步写入循环
func (l *Logger) writeLoop() {
	defer l.wg.Done()

	ticker := time.NewTicker(l.config.FlushInterval)
	defer ticker.Stop()

	batch := make([]*Entry, 0, 100)

	for {
		select {
		case entry := <-l.buffer:
			batch = append(batch, entry)

			// 批量写入
			if len(batch) >= 100 {
				l.writeBatch(batch)
				batch = batch[:0]
			}

		case <-ticker.C:
			// 定期刷新
			if len(batch) > 0 {
				l.writeBatch(batch)
				batch = batch[:0]
			}

		case <-l.stopCh:
			// 修复 #3：使用非阻塞 drain 替代 len()+<-ch 模式。
			// 原代码中 len() 与 <-ch 不是原子操作，并发写入时可能导致阻塞。
			// 使用 select+default 确保在 channel 为空时立即退出。
		drain:
			for {
				select {
				case entry := <-l.buffer:
					batch = append(batch, entry)
				default:
					break drain
				}
			}
			if len(batch) > 0 {
				l.writeBatch(batch)
			}
			return
		}
	}
}

// writeBatch 批量写入日志
func (l *Logger) writeBatch(entries []*Entry) {
	l.mu.Lock()
	defer l.mu.Unlock()

	for _, entry := range entries {
		data, err := json.Marshal(entry)
		if err != nil {
			logger.WithError(err).Error("[Audit] Failed to marshal audit entry")
			continue
		}

		if _, err := l.file.Write(append(data, '\n')); err != nil {
			logger.WithError(err).Error("[Audit] Failed to write audit entry")
		}
	}

	// 刷新到磁盘
	if err := l.file.Sync(); err != nil {
		logger.WithError(err).Error("[Audit] Failed to sync audit log")
	}
}

// generateID 生成唯一ID
func (l *Logger) generateID() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.counter++
	return fmt.Sprintf("%d_%d", time.Now().UnixNano(), l.counter)
}

// Close 关闭审计日志记录器
func (l *Logger) Close() error {
	if !l.config.Enabled {
		return nil
	}

	// 停止写入循环
	close(l.stopCh)
	l.wg.Wait()

	// 关闭文件
	if l.file != nil {
		return l.file.Close()
	}

	logger.Info("[Audit] Audit logger closed")
	return nil
}

// Info 记录信息级别日志
func (l *Logger) Info(action Action, actor, message string) {
	l.Log(&Entry{
		Level:   LevelInfo,
		Action:  action,
		Actor:   actor,
		Result:  "success",
		Message: message,
	})
}

// Warn 记录警告级别日志
func (l *Logger) Warn(action Action, actor, message string) {
	l.Log(&Entry{
		Level:   LevelWarn,
		Action:  action,
		Actor:   actor,
		Result:  "warning",
		Message: message,
	})
}

// Error 记录错误级别日志
func (l *Logger) Error(action Action, actor, message string, err error) {
	entry := &Entry{
		Level:   LevelError,
		Action:  action,
		Actor:   actor,
		Result:  "failure",
		Message: message,
	}
	if err != nil {
		entry.Error = err.Error()
	}
	l.Log(entry)
}

// Critical 记录严重级别日志
func (l *Logger) Critical(action Action, actor, message string) {
	l.Log(&Entry{
		Level:   LevelCritical,
		Action:  action,
		Actor:   actor,
		Result:  "critical",
		Message: message,
	})
}

// LogWithMetadata 记录带元数据的日志
func (l *Logger) LogWithMetadata(level Level, action Action, actor string, metadata map[string]any) {
	l.Log(&Entry{
		Level:    level,
		Action:   action,
		Actor:    actor,
		Result:   "success",
		Metadata: metadata,
	})
}

// LogCommandExecution 记录命令执行
func (l *Logger) LogCommandExecution(actor, command string, success bool, duration time.Duration, err error) {
	result := "success"
	action := ActionCommandExecute
	level := LevelInfo

	if !success {
		result = "failure"
		action = ActionCommandFail
		level = LevelError
	}

	entry := &Entry{
		Level:    level,
		Action:   action,
		Actor:    actor,
		Target:   command,
		Resource: "command",
		Result:   result,
		Duration: duration.Milliseconds(),
	}

	if err != nil {
		entry.Error = err.Error()
	}

	l.Log(entry)
}

// LogPluginOperation 记录插件操作
func (l *Logger) LogPluginOperation(action Action, pluginName string, success bool, err error) {
	result := "success"
	level := LevelInfo

	if !success {
		result = "failure"
		level = LevelError
	}

	entry := &Entry{
		Level:    level,
		Action:   action,
		Target:   pluginName,
		Resource: "plugin",
		Result:   result,
		Actor:    "system",
	}

	if err != nil {
		entry.Error = err.Error()
	}

	l.Log(entry)
}

// LogConfigChange 记录配置变更
func (l *Logger) LogConfigChange(actor, configKey, oldValue, newValue string) {
	l.Log(&Entry{
		Level:    LevelInfo,
		Action:   ActionConfigUpdate,
		Actor:    actor,
		Target:   configKey,
		Resource: "config",
		Result:   "success",
		Metadata: map[string]any{
			"old_value": oldValue,
			"new_value": newValue,
		},
	})
}

// LogSystemEvent 记录系统事件
func (l *Logger) LogSystemEvent(action Action, message string) {
	l.Log(&Entry{
		Level:   LevelInfo,
		Action:  action,
		Actor:   "system",
		Result:  "success",
		Message: message,
	})
}
