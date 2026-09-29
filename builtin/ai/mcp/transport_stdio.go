package mcp

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// transport_stdio.go — stdio 传输：把外部工具服务器作为子进程启动，按其
// 标准输入/输出以换行分隔的 JSON-RPC 报文通信。
//
// 安全：命令须在白名单内、只透传白名单环境变量（见 security.go）；进程在
// 通道关闭时终止（Windows 下连同子进程树）。

type stdioTransport struct {
	cfg  ServerConfig
	cmd  *exec.Cmd
	in   io.WriteCloser
	out  *bufio.Reader
	done chan struct{}

	closeOnce sync.Once
}

// newStdioTransport 构造（不启动）一个 stdio 传输。
func newStdioTransport(cfg ServerConfig) (*stdioTransport, error) {
	cmd := exec.Command(cfg.Command, cfg.Args...)
	cmd.Env = buildEnv(cfg.Env, cfg.EnvAllowlist)
	if cfg.Cwd != "" {
		cmd.Dir = cfg.Cwd
	}
	return &stdioTransport{cfg: cfg, cmd: cmd, done: make(chan struct{})}, nil
}

// Start 启动子进程并开始读取其标准输出。
func (t *stdioTransport) Start(handler func([]byte)) error {
	stdin, err := t.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("mcp: %s: stdin pipe: %w", t.cfg.Name, err)
	}
	stdout, err := t.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("mcp: %s: stdout pipe: %w", t.cfg.Name, err)
	}
	// stderr 用于诊断，转成调试日志，避免污染协议通道或因写满而阻塞子进程。
	stderr, err := t.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("mcp: %s: stderr pipe: %w", t.cfg.Name, err)
	}
	if err := t.cmd.Start(); err != nil {
		return fmt.Errorf("mcp: %s: start %q: %w", t.cfg.Name, t.cfg.Command, err)
	}
	t.in = stdin
	t.out = bufio.NewReaderSize(stdout, 64*1024)

	go drainStderr(t.cfg.Name, stderr)
	go t.readLoop(handler)
	return nil
}

// readLoop 持续读取换行分隔的报文并交给 handler。
func (t *stdioTransport) readLoop(handler func([]byte)) {
	defer close(t.done)
	max := t.cfg.maxResponseBytes()
	for {
		line, err := t.out.ReadBytes('\n')
		if len(line) > 0 {
			line = bytes.TrimRight(line, "\r\n")
			if len(line) > 0 {
				if int64(len(line)) > max {
					logger.Warnf("[MCP] %s: dropping oversize message (%d bytes)", t.cfg.Name, len(line))
				} else {
					handler(line)
				}
			}
		}
		if err != nil {
			if err != io.EOF {
				logger.Warnf("[MCP] %s: read closed: %v", t.cfg.Name, err)
			}
			return
		}
	}
}

// Send 写入一条报文（换行分帧）。
func (t *stdioTransport) Send(ctx context.Context, msg []byte) error {
	if t.in == nil {
		return fmt.Errorf("mcp: %s: transport not started", t.cfg.Name)
	}
	select {
	case <-t.done:
		return fmt.Errorf("mcp: %s: transport closed", t.cfg.Name)
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if _, err := t.in.Write(append(msg, '\n')); err != nil {
		return fmt.Errorf("mcp: %s: write: %w", t.cfg.Name, err)
	}
	return nil
}

// Close 关闭输入并终止子进程（Windows 下连同进程树）。
func (t *stdioTransport) Close() error {
	t.closeOnce.Do(func() {
		if t.in != nil {
			_ = t.in.Close()
		}
		if t.cmd.Process != nil {
			killProcessTree(t.cmd.Process.Pid)
		}
		_ = t.cmd.Wait()
	})
	return nil
}

// Wait 在读取循环结束时关闭（连接断开）。
func (t *stdioTransport) Wait() <-chan struct{} { return t.done }

// killProcessTree 终止进程；Windows 下连带子进程树。
func killProcessTree(pid int) {
	if runtime.GOOS == "windows" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// taskkill 失败时回退到直接 kill。
		if err := exec.CommandContext(ctx, "taskkill", "/F", "/T", "/PID", fmt.Sprint(pid)).Run(); err == nil {
			return
		}
	}
	if p, err := os.FindProcess(pid); err == nil {
		_ = p.Kill()
	}
}

// drainStderr 把子进程 stderr 转为调试日志。
func drainStderr(name string, r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" {
			logger.Debugf("[MCP] %s stderr: %s", name, line)
		}
	}
}
