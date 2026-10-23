// exectool.go — 受控进程执行动作（process_run）。
//
// 动作只通过消费方端口 [ProcessRunner] 执行命令：白名单、工作目录边界、
// 环境变量清洗、超时与输出上限都由装配侧注入的实现负责
// （builtin/ai/procexec）。没有注入端口时动作返回明确的不可用提示。
//
// 安全默认：
//   - 要求 RBAC 权限 ai.exec.run；
//   - 默认在任何审批模式下都强制人工审批（宿主进程执行不可逆）；
//   - 不接受 Shell 字符串，命令与参数分开传入；具体允许哪些命令由
//     执行器白名单决定。
package catalog

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
)

const (
	// ProcessRunToolName 执行受控进程的动作名。
	ProcessRunToolName = "process_run"
	// ExecPermission 进程执行动作所需的 RBAC 权限。
	ExecPermission = "ai.exec.run"
)

// ExecRequest 一次进程执行请求（由动作层构造，不含超时等运行时策略）。
type ExecRequest struct {
	// Command 可执行文件裸名（必须命中执行器白名单）。
	Command string
	// Args 参数数组，不经过 Shell。
	Args []string
	// Dir 相对工作区根目录的工作目录；空表示根目录。
	Dir string
}

// ExecResult 一次进程执行的结果。
type ExecResult struct {
	Command   string
	Args      []string
	Dir       string
	ExitCode  int
	Stdout    string
	Stderr    string
	TimedOut  bool
	Truncated bool
	Duration  time.Duration
}

// ProcessRunner 受控进程执行端口。
type ProcessRunner interface {
	// Run 执行一次请求；只有"无法开始执行"返回错误，非零退出码在结果中报告。
	Run(ctx context.Context, req ExecRequest) (ExecResult, error)
}

// ExecToolOptions 进程执行动作的注册选项。
type ExecToolOptions struct {
	// RequireApproval 是否在任何审批模式下都强制人工审批（默认应为 true）。
	RequireApproval bool
}

// BuildExecTools 构建进程执行动作集。
func BuildExecTools(opts ExecToolOptions) []toolkit.Tool {
	return []toolkit.Tool{
		{
			Name:                  ProcessRunToolName,
			Description:           "在授权的工作区内执行一个白名单命令（如测试、构建）。命令与参数分开传入，不经过 Shell；返回退出码、标准输出与标准错误。修改或安装类命令需要管理员预先加入执行白名单",
			Categories:            []string{"workspace", "exec"},
			RequiresApproval:      true,
			AlwaysRequireApproval: opts.RequireApproval,
			Permissions:           []string{ExecPermission},
			Parameters: protocol.ToolParamSchema{
				Type: "object",
				Properties: map[string]protocol.ToolParamSchema{
					"command": {
						Type:        "string",
						Description: "可执行文件名（裸名，如 go / git / python），必须命中执行白名单",
					},
					"args": {
						Type:        "array",
						Items:       &protocol.ToolParamSchema{Type: "string"},
						Description: "参数数组，例如 [\"test\", \"./...\"]",
					},
					"dir": {
						Type:        "string",
						Description: "相对工作区根目录的工作目录；省略时使用根目录",
					},
				},
				Required: []string{"command"},
			},
			Execute: executeProcessRun,
		},
	}
}

// executeProcessRun 执行受控进程并格式化结果。
func executeProcessRun(ctx context.Context, inv *toolkit.Invocation, args map[string]any) (toolkit.ToolResult, error) {
	runner := processRunnerFromInvocation(inv)
	if runner == nil {
		return toolkit.TextResult("错误: 进程执行能力未启用（需要 workspace_roots 与 exec_allowlist）"), nil
	}
	command, _ := args["command"].(string)
	command = strings.TrimSpace(command)
	if command == "" {
		return toolkit.TextResult("错误: 缺少 command 参数"), nil
	}
	dir, _ := args["dir"].(string)

	result, err := runner.Run(ctx, ExecRequest{
		Command: command,
		Args:    stringSliceArg(args["args"]),
		Dir:     strings.TrimSpace(dir),
	})
	if err != nil {
		return toolkit.TextResult("错误: 无法执行: " + err.Error()), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "命令 `%s`（工作目录 %s）\n", formatCommandLine(result.Command, result.Args), displayDir(result.Dir))
	switch {
	case result.TimedOut:
		fmt.Fprintf(&b, "结果: 执行超时被终止（耗时 %s）\n", result.Duration.Round(time.Millisecond))
	default:
		fmt.Fprintf(&b, "结果: 退出码 %d（耗时 %s）\n", result.ExitCode, result.Duration.Round(time.Millisecond))
	}
	if result.Truncated {
		b.WriteString("（输出超过上限，仅保留前段）\n")
	}
	if stdout := strings.TrimSpace(result.Stdout); stdout != "" {
		b.WriteString("\nstdout:\n")
		b.WriteString(stdout)
		b.WriteString("\n")
	}
	if stderr := strings.TrimSpace(result.Stderr); stderr != "" {
		b.WriteString("\nstderr:\n")
		b.WriteString(stderr)
		b.WriteString("\n")
	}
	if strings.TrimSpace(result.Stdout) == "" && strings.TrimSpace(result.Stderr) == "" {
		b.WriteString("\n（没有输出）")
	}
	return toolkit.TextResult(strings.TrimSpace(b.String())), nil
}

// processRunnerFromInvocation 从调用上下文提取进程执行端口；未注入返回 nil。
func processRunnerFromInvocation(inv *toolkit.Invocation) ProcessRunner {
	caps, ok := CapabilitiesFrom(inv)
	if !ok {
		return nil
	}
	return caps.Process
}

// formatCommandLine 拼接命令展示行（仅用于回填文本，不参与执行）。
func formatCommandLine(command string, args []string) string {
	if len(args) == 0 {
		return command
	}
	return command + " " + strings.Join(args, " ")
}

// displayDir 展示工作目录（空表示根目录）。
func displayDir(dir string) string {
	if strings.TrimSpace(dir) == "" {
		return "工作区根目录"
	}
	return dir
}
