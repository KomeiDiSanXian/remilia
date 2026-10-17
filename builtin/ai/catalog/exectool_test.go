package catalog

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
)

// fakeProcessRunner 记录请求并返回预设结果的端口替身。
type fakeProcessRunner struct {
	result ExecResult
	err    error
	req    ExecRequest
}

func (f *fakeProcessRunner) Run(_ context.Context, req ExecRequest) (ExecResult, error) {
	f.req = req
	return f.result, f.err
}

func TestBuildExecToolsShape(t *testing.T) {
	tools := BuildExecTools(ExecToolOptions{RequireApproval: true})
	if len(tools) != 1 {
		t.Fatalf("tools = %d, want 1", len(tools))
	}
	tool := tools[0]
	if tool.Name != ProcessRunToolName {
		t.Fatalf("tool name = %q", tool.Name)
	}
	if !tool.RequiresApproval || !tool.AlwaysRequireApproval {
		t.Fatal("process_run must always require approval by default")
	}
	if len(tool.Permissions) != 1 || tool.Permissions[0] != ExecPermission {
		t.Fatalf("permissions = %v, want %s", tool.Permissions, ExecPermission)
	}
	if len(tool.Parameters.Required) != 1 || tool.Parameters.Required[0] != "command" {
		t.Fatalf("command should be required: %+v", tool.Parameters.Required)
	}

	optional := BuildExecTools(ExecToolOptions{})[0]
	if optional.AlwaysRequireApproval {
		t.Fatal("approval should be optional when explicitly disabled")
	}
}

func TestExecuteProcessRunFormatsResult(t *testing.T) {
	runner := &fakeProcessRunner{result: ExecResult{
		Command: "go", Args: []string{"test", "./..."}, Dir: "sub",
		ExitCode: 0, Stdout: "ok example.com/x", Duration: 1500 * time.Millisecond,
	}}
	tool := BuildExecTools(ExecToolOptions{})[0]
	ctx := WithCapabilities(context.Background(), Capabilities{Process: runner})

	out, err := tool.Execute(ctx, map[string]any{
		"command": "go",
		"args":    []any{"test", "./..."},
		"dir":     "sub",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{"go test ./...", "退出码 0", "ok example.com/x", "sub"} {
		if !strings.Contains(out, want) {
			t.Fatalf("result missing %q: %s", want, out)
		}
	}
	if runner.req.Command != "go" || len(runner.req.Args) != 2 || runner.req.Dir != "sub" {
		t.Fatalf("port called with %+v", runner.req)
	}
}

func TestExecuteProcessRunUnavailableAndErrors(t *testing.T) {
	tool := BuildExecTools(ExecToolOptions{})[0]

	if out, _ := tool.Execute(context.Background(), map[string]any{"command": "go"}); !strings.Contains(out, "未启用") {
		t.Fatalf("missing port should be reported: %q", out)
	}

	ctx := WithCapabilities(context.Background(), Capabilities{Process: &fakeProcessRunner{}})
	if out, _ := tool.Execute(ctx, map[string]any{}); !strings.Contains(out, "command") {
		t.Fatalf("missing command should be reported: %q", out)
	}

	failing := &fakeProcessRunner{err: errors.New("command is not allowed")}
	ctx = WithCapabilities(context.Background(), Capabilities{Process: failing})
	out, _ := tool.Execute(ctx, map[string]any{"command": "rm"})
	if !strings.Contains(out, "无法执行") || !strings.Contains(out, "not allowed") {
		t.Fatalf("port error should be surfaced: %q", out)
	}
}

func TestExecuteProcessRunTimeoutAndTruncation(t *testing.T) {
	runner := &fakeProcessRunner{result: ExecResult{
		Command: "go", ExitCode: -1, TimedOut: true, Truncated: true,
		Stdout: "partial output", Duration: 2 * time.Second,
	}}
	tool := BuildExecTools(ExecToolOptions{})[0]
	ctx := WithCapabilities(context.Background(), Capabilities{Process: runner})

	out, _ := tool.Execute(ctx, map[string]any{"command": "go"})
	for _, want := range []string{"执行超时被终止", "输出超过上限", "partial output"} {
		if !strings.Contains(out, want) {
			t.Fatalf("result missing %q: %s", want, out)
		}
	}
}

// 编译期确认动作构建使用的工具类型。
var _ toolkit.Tool = BuildExecTools(ExecToolOptions{})[0]
