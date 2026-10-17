// Command executor 是 Remilia 的远程执行节点。
//
// 它把一组受控能力（工作区文件读写、白名单进程执行）以 JSON API 的形式暴露给
// 机器人进程；边界（根目录、命令白名单、环境变量白名单、超时与输出上限）全部
// 由本进程持有并强制，机器人侧即使配置被篡改也无法越出节点边界。
//
// 用法：
//
//	remilia-executor -addr :9020 -token <token> \
//	    -roots /home/ubuntu/workspace -allow go,git
//
// 文件能力无需命令白名单即可使用；不传 -allow 时节点只提供文件能力。
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/execnode"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/procexec"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/workspace"
)

func main() {
	var (
		addr       = flag.String("addr", ":9020", "监听地址")
		token      = flag.String("token", "", "访问令牌（必填，所有端点要求 Bearer 认证）")
		roots      = flag.String("roots", "", "工作区根目录，逗号分隔（必填，必须已存在）")
		allow      = flag.String("allow", "", "允许执行的命令白名单，逗号分隔（为空时只提供文件能力）")
		envAllow   = flag.String("env-allowlist", "", "额外允许子进程继承的环境变量，逗号分隔")
		timeout    = flag.Duration("timeout", 120*time.Second, "单次执行超时")
		maxOutput  = flag.Int("max-output", 32<<10, "标准输出/标准错误各自的捕获上限（字节）")
		version    = flag.String("version", "dev", "节点版本（/v1/ready 自述用）")
		quietStart = flag.Bool("quiet", false, "启动时不打印配置摘要")
	)
	flag.Parse()

	rootList := splitList(*roots)
	if len(rootList) == 0 {
		fatalf("missing -roots: 至少配置一个已存在的工作区根目录")
	}
	if strings.TrimSpace(*token) == "" {
		fatalf("missing -token: 节点拒绝在无令牌的情况下启动")
	}

	manager, err := workspace.New(rootList)
	if err != nil {
		fatalf("workspace: %v", err)
	}

	cfg := execnode.ServerConfig{
		Workspace: manager,
		Token:     *token,
		Version:   *version,
	}
	commandList := splitList(*allow)
	if len(commandList) > 0 {
		runner, rerr := procexec.New(procexec.Config{
			Workspace:      manager,
			Allowlist:      commandList,
			EnvAllowlist:   splitList(*envAllow),
			Timeout:        *timeout,
			MaxOutputBytes: *maxOutput,
		})
		if rerr != nil {
			fatalf("procexec: %v", rerr)
		}
		cfg.Runner = runner
	}

	server, err := execnode.NewServer(cfg)
	if err != nil {
		fatalf("execnode: %v", err)
	}

	if !*quietStart {
		log.Printf("remilia-executor %s listening on %s (roots=%d, allowed=%v)",
			*version, *addr, len(manager.Roots()), splitList(*allow))
	}

	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		// 读写不设硬超时：执行时长由 procexec 的超时与请求取消兜底，
		// 读超时会把长任务误杀。
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fatalf("serve: %v", err)
	}
}

// splitList 解析逗号分隔列表，去除空白项。
func splitList(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// fatalf 打印错误并退出（节点启动失败必须显式失败，不能以半配置状态运行）。
func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "remilia-executor: "+format+"\n", args...)
	os.Exit(1)
}
