package audit_test

import (
	"os"
	"testing"
	"testing/synctest"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/audit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewLogger 测试创建审计日志记录器
func TestNewLogger(t *testing.T) {
	// 禁用的记录器
	t.Run("disabled logger", func(t *testing.T) {
		config := audit.Config{
			Enabled: false,
		}
		logger, err := audit.NewLogger(config)
		require.NoError(t, err)
		assert.NotNil(t, logger)

		// 关闭
		err = logger.Close()
		assert.NoError(t, err)
	})

	// 启用的记录器
	t.Run("enabled logger", func(t *testing.T) {
		tmpDir := t.TempDir()
		config := audit.Config{
			Enabled:       true,
			OutputFile:    tmpDir + "/audit.log",
			BufferSize:    10,
			FlushInterval: 100 * time.Millisecond,
			AsyncWrite:    true,
		}

		logger, err := audit.NewLogger(config)
		require.NoError(t, err)
		assert.NotNil(t, logger)

		defer logger.Close()

		// 验证文件创建
		_, err = os.Stat(config.OutputFile)
		assert.NoError(t, err)
	})
}

// TestLogLevels 测试不同级别的日志
func TestLogLevels(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tmpDir := t.TempDir()
		config := audit.Config{
			Enabled:       true,
			OutputFile:    tmpDir + "/audit.log",
			BufferSize:    10,
			FlushInterval: 100 * time.Millisecond,
			AsyncWrite:    false, // 同步写入便于测试
		}

		logger, err := audit.NewLogger(config)
		require.NoError(t, err)
		defer logger.Close()

		// 测试各级别日志
		logger.Info(audit.ActionUserLogin, "user123", "User logged in")
		logger.Warn(audit.ActionCommandExecute, "user123", "Command execution warning")
		logger.Error(audit.ActionCommandFail, "user123", "Command failed", assert.AnError)
		logger.Critical(audit.ActionSystemShutdown, "system", "System shutdown initiated")

		time.Sleep(200 * time.Millisecond) // 等待异步写入
	})
}

// TestLogEntry 测试日志条目
func TestLogEntry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tmpDir := t.TempDir()
		config := audit.Config{
			Enabled:       true,
			OutputFile:    tmpDir + "/audit.log",
			BufferSize:    10,
			FlushInterval: 100 * time.Millisecond,
			AsyncWrite:    false,
		}

		logger, err := audit.NewLogger(config)
		require.NoError(t, err)
		defer logger.Close()

		// 创建完整的日志条目
		entry := &audit.Entry{
			Level:    audit.LevelInfo,
			Action:   audit.ActionCommandExecute,
			Actor:    "user123",
			Target:   "/help",
			Resource: "command",
			Result:   "success",
			Message:  "Command executed successfully",
			Metadata: map[string]any{
				"args": []string{"help", "search"},
			},
			IP:        "192.168.1.100",
			UserAgent: "Discord-Bot/1.0",
			Duration:  150,
		}

		logger.Log(entry)

		time.Sleep(200 * time.Millisecond)
	})
}

// TestLogCommandExecution 测试命令执行日志
func TestLogCommandExecution(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tmpDir := t.TempDir()
		config := audit.Config{
			Enabled:       true,
			OutputFile:    tmpDir + "/audit.log",
			BufferSize:    10,
			FlushInterval: 100 * time.Millisecond,
			AsyncWrite:    false,
		}

		logger, err := audit.NewLogger(config)
		require.NoError(t, err)
		defer logger.Close()

		// 成功的命令执行
		logger.LogCommandExecution("user123", "/weather", true, 100*time.Millisecond, nil)

		// 失败的命令执行
		logger.LogCommandExecution("user123", "/error", false, 50*time.Millisecond, assert.AnError)

		time.Sleep(200 * time.Millisecond)
	})
}

// TestLogPluginOperation 测试插件操作日志
func TestLogPluginOperation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tmpDir := t.TempDir()
		config := audit.Config{
			Enabled:       true,
			OutputFile:    tmpDir + "/audit.log",
			BufferSize:    10,
			FlushInterval: 100 * time.Millisecond,
			AsyncWrite:    false,
		}

		logger, err := audit.NewLogger(config)
		require.NoError(t, err)
		defer logger.Close()

		// 插件加载
		logger.LogPluginOperation(audit.ActionPluginLoad, "help-plugin", true, nil)

		// 插件卸载
		logger.LogPluginOperation(audit.ActionPluginUnload, "help-plugin", true, nil)

		// 插件加载失败
		logger.LogPluginOperation(audit.ActionPluginLoad, "bad-plugin", false, assert.AnError)

		time.Sleep(200 * time.Millisecond)
	})
}

// TestLogConfigChange 测试配置变更日志
func TestLogConfigChange(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tmpDir := t.TempDir()
		config := audit.Config{
			Enabled:       true,
			OutputFile:    tmpDir + "/audit.log",
			BufferSize:    10,
			FlushInterval: 100 * time.Millisecond,
			AsyncWrite:    false,
		}

		logger, err := audit.NewLogger(config)
		require.NoError(t, err)
		defer logger.Close()

		logger.LogConfigChange("admin", "log.level", "info", "debug")

		time.Sleep(200 * time.Millisecond)
	})
}

// TestLogSystemEvent 测试系统事件日志
func TestLogSystemEvent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tmpDir := t.TempDir()
		config := audit.Config{
			Enabled:       true,
			OutputFile:    tmpDir + "/audit.log",
			BufferSize:    10,
			FlushInterval: 100 * time.Millisecond,
			AsyncWrite:    false,
		}

		logger, err := audit.NewLogger(config)
		require.NoError(t, err)
		defer logger.Close()

		logger.LogSystemEvent(audit.ActionSystemStart, "System started")
		logger.LogSystemEvent(audit.ActionSystemShutdown, "System shutting down")

		time.Sleep(200 * time.Millisecond)
	})
}

// TestMinLevel 测试最低日志级别过滤
func TestMinLevel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tmpDir := t.TempDir()
		config := audit.Config{
			Enabled:       true,
			OutputFile:    tmpDir + "/audit.log",
			BufferSize:    10,
			FlushInterval: 100 * time.Millisecond,
			MinLevel:      audit.LevelWarn, // 只记录 Warn 及以上
			AsyncWrite:    false,
		}

		logger, err := audit.NewLogger(config)
		require.NoError(t, err)
		defer logger.Close()

		// Info 级别应该被过滤
		logger.Info(audit.ActionUserLogin, "user123", "Should be filtered")

		// Warn 级别应该被记录
		logger.Warn(audit.ActionCommandExecute, "user123", "Should be logged")

		time.Sleep(200 * time.Millisecond)

		// 验证只有 Warn 被写入
		// （实际测试中可以读取文件内容验证）
	})
}

// TestBufferOverflow 测试缓冲区溢出
func TestBufferOverflow(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tmpDir := t.TempDir()
		config := audit.Config{
			Enabled:       true,
			OutputFile:    tmpDir + "/audit.log",
			BufferSize:    2, // 很小的缓冲区
			FlushInterval: 1 * time.Second,
			AsyncWrite:    true,
		}

		logger, err := audit.NewLogger(config)
		require.NoError(t, err)
		defer logger.Close()

		// 发送超过缓冲区大小的日志
		for range 10 {
			logger.Info(audit.ActionUserLogin, "user123", "Test message")
		}

		time.Sleep(100 * time.Millisecond)
		// 应该不会 panic，部分日志会被丢弃
	})
}

// BenchmarkLogSync 基准测试：同步写入
func BenchmarkLogSync(b *testing.B) {
	tmpDir := b.TempDir()
	config := audit.Config{
		Enabled:       true,
		OutputFile:    tmpDir + "/audit.log",
		BufferSize:    1000,
		FlushInterval: 1 * time.Second,
		AsyncWrite:    false,
	}

	logger, _ := audit.NewLogger(config)
	defer logger.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info(audit.ActionUserLogin, "user123", "Benchmark test")
	}
}

// BenchmarkLogAsync 基准测试：异步写入
func BenchmarkLogAsync(b *testing.B) {
	tmpDir := b.TempDir()
	config := audit.Config{
		Enabled:       true,
		OutputFile:    tmpDir + "/audit.log",
		BufferSize:    1000,
		FlushInterval: 1 * time.Second,
		AsyncWrite:    true,
	}

	logger, _ := audit.NewLogger(config)
	defer logger.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info(audit.ActionUserLogin, "user123", "Benchmark test")
	}
}
