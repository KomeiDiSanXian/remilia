package procexec

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/workspace"
)

// newTestManager 建立以临时目录为工作区的执行器。
func newTestManager(t *testing.T, allow []string, mutate func(*Config)) (*Manager, string) {
	t.Helper()
	dir := t.TempDir()
	ws, err := workspace.New([]string{dir})
	if err != nil {
		t.Fatalf("workspace.New: %v", err)
	}
	cfg := Config{Workspace: ws, Allowlist: allow}
	if mutate != nil {
		mutate(&cfg)
	}
	m, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return m, dir
}

func TestNewDisabled(t *testing.T) {
	if _, err := New(Config{}); !errors.Is(err, ErrDisabled) {
		t.Fatalf("New(empty) = %v, want ErrDisabled", err)
	}
	ws, err := workspace.New([]string{t.TempDir()})
	if err != nil {
		t.Fatalf("workspace.New: %v", err)
	}
	if _, err := New(Config{Workspace: ws}); !errors.Is(err, ErrDisabled) {
		t.Fatalf("New(no allowlist) = %v, want ErrDisabled", err)
	}
	if _, err := New(Config{Allowlist: []string{"go"}}); !errors.Is(err, ErrDisabled) {
		t.Fatalf("New(no workspace) = %v, want ErrDisabled", err)
	}
}

func TestRunRejectsUnlistedAndPathCommands(t *testing.T) {
	m, _ := newTestManager(t, []string{"go"}, nil)
	if _, err := m.Run(context.Background(), Request{Command: "git"}); !errors.Is(err, ErrCommandNotAllowed) {
		t.Fatalf("unlisted command error = %v, want ErrCommandNotAllowed", err)
	}
	for _, command := range []string{`C:\Windows\System32\cmd.exe`, "./go", "sub/go"} {
		if _, err := m.Run(context.Background(), Request{Command: command}); !errors.Is(err, ErrInvalidCommand) {
			t.Fatalf("path command %q error = %v, want ErrInvalidCommand", command, err)
		}
	}
}

func TestRunSuccessAndExitCode(t *testing.T) {
	m, dir := newTestManager(t, []string{"go"}, nil)

	result, err := m.Run(context.Background(), Request{Command: "go", Args: []string{"env", "GOROOT"}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %q", result.ExitCode, result.Stderr)
	}
	if strings.TrimSpace(result.Stdout) == "" {
		t.Fatal("expected GOROOT output")
	}
	if result.Duration <= 0 {
		t.Fatal("duration should be measured")
	}

	// 工作目录必须位于工作区内。
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if _, err := m.Run(context.Background(), Request{Command: "go", Args: []string{"env"}, Dir: "sub"}); err != nil {
		t.Fatalf("Run in subdir: %v", err)
	}
	if _, err := m.Run(context.Background(), Request{Command: "go", Args: []string{"env"}, Dir: ".."}); err == nil {
		t.Fatal("working directory outside the workspace must be rejected")
	}
}

func TestRunNonZeroExit(t *testing.T) {
	m, _ := newTestManager(t, []string{"go"}, nil)
	result, err := m.Run(context.Background(), Request{
		Command: "go",
		Args:    []string{"build", "-remilia-flag-does-not-exist"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.ExitCode == 0 {
		t.Fatalf("expected non-zero exit, got %+v", result)
	}
}

func TestRunScrubsEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-based environment check is Unix only")
	}
	t.Setenv("REMLIA_SECRET_TOKEN", "super-secret-value")
	m, _ := newTestManager(t, []string{"sh"}, nil)
	result, err := m.Run(context.Background(), Request{
		Command: "sh",
		Args:    []string{"-c", "echo ${REMLIA_SECRET_TOKEN:-missing}"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.Contains(result.Stdout, "super-secret-value") {
		t.Fatal("secret environment variable leaked into the child process")
	}
	if !strings.Contains(result.Stdout, "missing") {
		t.Fatalf("expected the variable to be absent, got %q", result.Stdout)
	}
}

func TestRunTimeoutKillsProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("timeout check is Unix only")
	}
	m, _ := newTestManager(t, []string{"sleep"}, func(c *Config) {
		c.Timeout = 300 * time.Millisecond
	})
	start := time.Now()
	result, err := m.Run(context.Background(), Request{Command: "sleep", Args: []string{"5"}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.TimedOut {
		t.Fatalf("expected timeout, got %+v", result)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("timeout did not terminate the process tree in time (%s)", elapsed)
	}
}

func TestRunCapsOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-based output check is Unix only")
	}
	m, _ := newTestManager(t, []string{"sh"}, func(c *Config) {
		c.MaxOutputBytes = 64
	})
	result, err := m.Run(context.Background(), Request{
		Command: "sh",
		Args:    []string{"-c", "head -c 10000 /dev/zero | tr '\\0' 'x'"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.Truncated {
		t.Fatal("expected truncated output")
	}
	if len(result.Stdout) > 64 {
		t.Fatalf("stdout length = %d, want <= 64", len(result.Stdout))
	}
}
