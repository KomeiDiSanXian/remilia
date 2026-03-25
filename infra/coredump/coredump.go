package coredump

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// Config 核心转储配置。
type Config struct {
	// Dir 核心转储文件存储目录，默认为 "coredumps"。
	Dir string

	// CrashLogEnabled 是否启用崩溃日志文件。
	// 启用后，Go 运行时的崩溃报告（goroutine 栈信息）将同时写入文件，
	// 而不仅仅输出到 stderr。
	CrashLogEnabled bool

	// DiagnoseOnEnable 是否在 Enable 时自动执行环境诊断。
	// 默认为 true。
	//
	// 设置为 false 可跳过自动诊断（如在容器编排环境中，
	// 环境可能在 Enable 后才完全就绪）。
	// 此时可稍后手动调用 [Diagnose] 执行诊断。
	DiagnoseOnEnable bool
}

// defaultConfig 返回默认配置。
func defaultConfig() Config {
	return Config{
		Dir:              "coredumps",
		CrashLogEnabled:  true,
		DiagnoseOnEnable: true,
	}
}

// Option 用于配置核心转储的选项函数。
type Option func(*Config)

// WithDir 设置核心转储文件的存储目录。
func WithDir(dir string) Option {
	return func(c *Config) {
		if dir != "" {
			c.Dir = dir
		}
	}
}

// WithCrashLog 设置是否启用崩溃日志文件。
// 崩溃日志会记录 Go 运行时的崩溃报告（goroutine 栈跟踪），
// 方便在核心转储不可用时仍能获取崩溃现场信息。
func WithCrashLog(enabled bool) Option {
	return func(c *Config) {
		c.CrashLogEnabled = enabled
	}
}

// WithDiagnoseOnEnable 设置是否在 Enable 时自动执行环境诊断。
//
// 设置为 false 可延迟诊断到手动调用 [Diagnose] 时执行。
// 适用于以下场景：
//   - 容器环境中 Enable 时配置尚未就绪
//   - systemd-coredump 等服务延迟加载
//   - 仅在 debug 模式下需要诊断输出
func WithDiagnoseOnEnable(enabled bool) Option {
	return func(c *Config) {
		c.DiagnoseOnEnable = enabled
	}
}

// Enable 启用核心转储生成。
//
// 调用后，程序将在崩溃时尽可能生成核心转储文件：
//   - 设置 GOTRACEBACK=crash，使 Go 运行时在 panic 时触发操作系统级崩溃信号
//   - 在 Unix 系统上，设置 RLIMIT_CORE 为无限制
//   - 在 Windows 系统上，注册异常处理器以生成 MiniDump 文件
//   - 可选地将运行时崩溃报告写入日志文件
//
// Enable 应在程序启动早期调用（通常在 main 函数开头）。
func Enable(opts ...Option) error {
	cfg := defaultConfig()
	for _, o := range opts {
		o(&cfg)
	}

	// 确保输出目录存在
	if err := os.MkdirAll(cfg.Dir, 0o755); err != nil {
		return fmt.Errorf("coredump: 创建目录 %q 失败: %w", cfg.Dir, err)
	}

	// 设置 GOTRACEBACK=crash
	// 这使 Go 运行时在 panic 时发送 SIGABRT（Unix）或 RaiseException（Windows），
	// 从而触发操作系统的核心转储机制。
	// 没有此设置，Go panic 只会打印栈信息后退出，不会产生核心转储。
	debug.SetTraceback("crash")

	// 设置崩溃日志输出文件
	if cfg.CrashLogEnabled {
		if err := setupCrashLog(cfg.Dir); err != nil {
			logger.Warnf("coredump: 设置崩溃日志失败: %v", err)
		}
	}

	// 执行平台特定的核心转储配置
	if err := enablePlatform(&cfg); err != nil {
		return fmt.Errorf("coredump: 平台配置失败: %w", err)
	}

	abs, _ := filepath.Abs(cfg.Dir)
	logger.Infof("coredump: 核心转储已启用，输出目录: %s", abs)

	// 可选的环境诊断
	if cfg.DiagnoseOnEnable {
		diagnose()
	}

	return nil
}

// Diagnose 执行平台环境诊断并将结果记录到日志。
//
// 诊断内容因平台而异：
//   - Linux：检查 dumpable 标志、core_pattern 配置、容器环境
//   - macOS：检查 kern.corefile sysctl、/cores 目录
//   - Windows/其他：无操作
//
// 可在以下场景手动调用：
//   - 使用 [WithDiagnoseOnEnable](false) 跳过了自动诊断
//   - 需要在运行时重新检查环境（如容器配置变更后）
//   - Debug 模式下按需输出诊断信息
func Diagnose() {
	diagnose()
}

// setupCrashLog 利用 debug.SetCrashOutput 将运行时崩溃报告重定向到文件。
//
// Go 运行时在程序崩溃时会打印所有 goroutine 的栈信息到 stderr。
// 此函数将这些信息额外写入一个文件，便于事后分析。
// 文件在调用时创建（0 字节），仅在实际崩溃时才写入内容。
func setupCrashLog(dir string) error {
	timestamp := time.Now().Format("20060102-150405")
	filename := filepath.Join(dir, fmt.Sprintf("crash-%s-%d.log", timestamp, os.Getpid()))

	f, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("创建崩溃日志文件失败: %w", err)
	}

	if err := debug.SetCrashOutput(f, debug.CrashOptions{}); err != nil {
		f.Close()
		return fmt.Errorf("设置崩溃输出失败: %w", err)
	}

	// 注意：f 不能关闭，运行时需要在崩溃时写入此文件。
	return nil
}

// dumpFilePath 生成带时间戳和 PID 的转储文件路径。
func dumpFilePath(dir, ext string) string {
	timestamp := time.Now().Format("20060102-150405")
	return filepath.Join(dir, fmt.Sprintf("core-%s-%d.%s", timestamp, os.Getpid(), ext))
}
