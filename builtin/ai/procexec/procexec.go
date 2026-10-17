// Package procexec 受控的进程执行。
//
// 与宿主交互的能力中，进程执行的风险最高：它可以把一次提示注入变成真正的
// 远程代码执行。因此本包只提供"最小执行面"：
//
//   - 命令必须命中调用方配置的可执行文件白名单，且只能是裸命令名
//     （不接受路径、Shell 字符串），参数以数组传入，不经过 Shell；
//   - 工作目录必须是工作区根目录内的目录；
//   - 子进程只继承显式允许的环境变量，默认丢弃其余变量（含各类密钥令牌）；
//   - 标准输出、标准错误有字节上限，超限部分丢弃但持续排空管道；
//   - 超时或取消时终止整棵进程树，并如实报告退出码与超时标记。
//
// 审批、权限与是否启用由上层（动作策略）决定，本包不持有任何模型决策。
package procexec

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/workspace"
)

// 领域错误。
var (
	// ErrDisabled 执行能力未配置（白名单为空或缺少工作区）。
	ErrDisabled = errors.New("procexec: execution is not configured")
	// ErrCommandNotAllowed 命令不在白名单内。
	ErrCommandNotAllowed = errors.New("procexec: command is not allowed")
	// ErrInvalidCommand 命令不是裸命令名（不接受路径与 Shell 字符串）。
	ErrInvalidCommand = errors.New("procexec: command must be a bare executable name")
)

// Request 一次进程执行请求。
type Request struct {
	// Command 可执行文件裸名（必须命中白名单）。
	Command string
	// Args 参数数组（不经过 Shell 解释）。
	Args []string
	// Dir 工作目录（相对工作区根目录的路径；空表示根目录）。
	Dir string
}

// Result 一次进程执行的结果。
type Result struct {
	// Command / Args / Dir 本次执行的实际参数。
	Command string
	Args    []string
	Dir     string
	// ExitCode 进程退出码（被超时/取消终止时为 -1）。
	ExitCode int
	// Stdout / Stderr 捕获的输出（可能被截断）。
	Stdout string
	Stderr string
	// TimedOut 是否因超时被终止。
	TimedOut bool
	// Truncated 输出是否被截断。
	Truncated bool
	// Duration 执行耗时。
	Duration time.Duration
}

// Config 执行器配置。
type Config struct {
	// Workspace 工作区管理器，决定可用的工作目录（必填）。
	Workspace *workspace.Manager
	// Allowlist 允许执行的可执行文件裸名（大小写不敏感，Windows 忽略 .exe 后缀）。
	// 为空表示禁用执行能力。
	Allowlist []string
	// EnvAllowlist 在基础白名单之外额外允许继承的环境变量名。
	EnvAllowlist []string
	// Timeout 单次执行超时（<=0 时取默认 120s）。
	Timeout time.Duration
	// MaxOutputBytes 标准输出与标准错误各自的捕获上限（<=0 时取默认 32 KiB）。
	MaxOutputBytes int
}

const (
	// DefaultTimeout 默认单次执行超时。
	DefaultTimeout = 120 * time.Second
	// DefaultMaxOutputBytes 单流默认输出上限。
	DefaultMaxOutputBytes = 32 << 10
	// waitDelay 取消后等待进程自行退出的宽限期（超时后强制收尾）。
	waitDelay = 3 * time.Second
)

// baseEnvAllowlist 默认继承的环境变量。
//
// 只保留定位工具链与构建缓存所必需的最小集合：各类 API Key、令牌与凭证
// 默认不进入子进程，避免"读一次环境变量"变成凭据泄露。
var baseEnvAllowlist = []string{
	"PATH",
	"HOME",
	"USERPROFILE",
	"APPDATA",
	"LOCALAPPDATA",
	"TEMP",
	"TMP",
	"TMPDIR",
	"SYSTEMROOT",
	"WINDIR",
	"SYSTEMDRIVE",
	"PATHEXT",
	"COMSPEC",
	"LANG",
	"LC_ALL",
	"TZ",
	"GOPATH",
	"GOROOT",
	"GOCACHE",
	"GOMODCACHE",
	"GOFLAGS",
	"GOTOOLCHAIN",
}

// Manager 受控进程执行器。
type Manager struct {
	workspace      *workspace.Manager
	allowlist      map[string]bool
	envAllowlist   []string
	timeout        time.Duration
	maxOutputBytes int
}

// New 构造执行器。Workspace 为空或白名单为空时返回 [ErrDisabled]。
func New(cfg Config) (*Manager, error) {
	if cfg.Workspace == nil || len(cfg.Allowlist) == 0 {
		return nil, ErrDisabled
	}
	allow := make(map[string]bool, len(cfg.Allowlist))
	for _, name := range cfg.Allowlist {
		name = normalizeCommand(name)
		if name != "" {
			allow[name] = true
		}
	}
	if len(allow) == 0 {
		return nil, ErrDisabled
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	maxOutput := cfg.MaxOutputBytes
	if maxOutput <= 0 {
		maxOutput = DefaultMaxOutputBytes
	}
	return &Manager{
		workspace:      cfg.Workspace,
		allowlist:      allow,
		envAllowlist:   append([]string(nil), cfg.EnvAllowlist...),
		timeout:        timeout,
		maxOutputBytes: maxOutput,
	}, nil
}

// Allowed 返回白名单中的命令（排序后，供日志与排查使用）。
func (m *Manager) Allowed() []string {
	if m == nil {
		return nil
	}
	out := make([]string, 0, len(m.allowlist))
	for name := range m.allowlist {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Timeout 返回单次执行超时（供节点自述与客户端展示）。
func (m *Manager) Timeout() time.Duration {
	if m == nil {
		return 0
	}
	return m.timeout
}

// Run 执行一次受控进程调用。
//
// 只有"无法开始执行"（未配置、命令不允许、工作目录非法、可执行文件不存在）
// 返回错误；进程真正运行后，非零退出码与超时都在结果里如实报告。
func (m *Manager) Run(ctx context.Context, req Request) (Result, error) {
	if m == nil || m.workspace == nil {
		return Result{}, ErrDisabled
	}
	command := strings.TrimSpace(req.Command)
	if command == "" {
		return Result{}, ErrInvalidCommand
	}
	if strings.ContainsAny(command, `/\`) {
		return Result{}, fmt.Errorf("%w: %q", ErrInvalidCommand, command)
	}
	if !m.allowlist[normalizeCommand(command)] {
		return Result{}, fmt.Errorf("%w: %q", ErrCommandNotAllowed, command)
	}

	path, err := exec.LookPath(command)
	if err != nil {
		return Result{}, fmt.Errorf("procexec: executable %q not found: %w", command, err)
	}
	if err := validateExecutable(path); err != nil {
		return Result{}, err
	}

	dir, err := m.resolveDir(req.Dir)
	if err != nil {
		return Result{}, err
	}

	runCtx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()

	stdout := &capWriter{limit: m.maxOutputBytes}
	stderr := &capWriter{limit: m.maxOutputBytes}
	cmd := exec.CommandContext(runCtx, path, req.Args...)
	cmd.Dir = dir
	cmd.Env = m.buildEnv()
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	setProcessGroup(cmd)
	cmd.Cancel = func() error { return killProcessTree(cmd) }
	cmd.WaitDelay = waitDelay

	start := time.Now()
	runErr := cmd.Run()
	duration := time.Since(start)

	result := Result{
		Command:   command,
		Args:      append([]string(nil), req.Args...),
		Dir:       req.Dir,
		Stdout:    stdout.String(),
		Stderr:    stderr.String(),
		TimedOut:  runCtx.Err() != nil && errors.Is(runCtx.Err(), context.DeadlineExceeded),
		Truncated: stdout.truncated || stderr.truncated,
		Duration:  duration,
	}
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	} else if runErr != nil {
		result.ExitCode = -1
	}
	return result, nil
}

// resolveDir 解析工作目录：空目录取第一个根目录，否则解析为根目录内的目录。
func (m *Manager) resolveDir(rel string) (string, error) {
	if strings.TrimSpace(rel) == "" {
		roots := m.workspace.Roots()
		if len(roots) == 0 {
			return "", ErrDisabled
		}
		return roots[0], nil
	}
	path, err := m.workspace.Resolve(rel)
	if err != nil {
		return "", fmt.Errorf("procexec: working directory: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("procexec: working directory: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("procexec: %q is not a directory", rel)
	}
	return path, nil
}

// buildEnv 构造子进程环境：只继承基础白名单与显式追加的变量。
func (m *Manager) buildEnv() []string {
	allowed := make(map[string]bool, len(baseEnvAllowlist)+len(m.envAllowlist))
	for _, name := range baseEnvAllowlist {
		allowed[strings.ToUpper(name)] = true
	}
	for _, name := range m.envAllowlist {
		if name = strings.ToUpper(strings.TrimSpace(name)); name != "" {
			allowed[name] = true
		}
	}
	out := make([]string, 0, len(allowed))
	for _, kv := range os.Environ() {
		name, _, found := strings.Cut(kv, "=")
		if !found {
			continue
		}
		if allowed[strings.ToUpper(name)] {
			out = append(out, kv)
		}
	}
	return out
}

// normalizeCommand 归一化命令名用于白名单比较：去空白、小写（Windows）、
// 去掉 .exe 后缀。
func normalizeCommand(name string) string {
	name = strings.TrimSpace(name)
	if runtime.GOOS == "windows" {
		name = strings.ToLower(strings.TrimSuffix(name, ".exe"))
	}
	return name
}

// validateExecutable 校验解析出的可执行文件。
//
// Windows 上只接受 .exe：.cmd/.bat/.ps1 会引入 Shell 解释层，与"参数数组、
// 不过 Shell"的执行模型相悖；Unix 上要求常规文件且带可执行位。
func validateExecutable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("procexec: stat executable: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("procexec: executable %q is not a regular file", path)
	}
	if runtime.GOOS == "windows" {
		if !strings.EqualFold(filepath.Ext(path), ".exe") {
			return fmt.Errorf("procexec: only .exe executables are supported on Windows, got %q", path)
		}
		return nil
	}
	if info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("procexec: %q is not executable", path)
	}
	return nil
}

// capWriter 有界输出缓冲：超过上限的字节被丢弃但仍报告写入成功，
// 让子进程持续排空管道而不是因写满而阻塞。
type capWriter struct {
	limit     int
	buf       []byte
	truncated bool
}

// Write 实现 io.Writer。
func (w *capWriter) Write(p []byte) (int, error) {
	if remaining := w.limit - len(w.buf); remaining > 0 {
		if len(p) <= remaining {
			w.buf = append(w.buf, p...)
		} else {
			w.buf = append(w.buf, p[:remaining]...)
			w.truncated = true
		}
	} else if len(p) > 0 {
		w.truncated = true
	}
	return len(p), nil
}

// String 返回捕获到的输出。
func (w *capWriter) String() string { return string(w.buf) }
