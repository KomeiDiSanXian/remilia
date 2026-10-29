// procadapter.go — 受控进程执行器到 catalog.ProcessRunner 端口的适配。

package ai

import (
	"context"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/catalog"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/procexec"
)

// processAccessor 把进程执行器适配为 [catalog.ProcessRunner]。
// 只做类型映射：白名单、环境清洗、超时与输出上限都在 procexec 包内。
type processAccessor struct{ m *procexec.Manager }

// Run 执行一次受控进程调用。
func (a processAccessor) Run(ctx context.Context, req catalog.ExecRequest) (catalog.ExecResult, error) {
	result, err := a.m.Run(ctx, procexec.Request{
		Command: req.Command,
		Args:    req.Args,
		Dir:     req.Dir,
	})
	if err != nil {
		return catalog.ExecResult{}, err
	}
	return catalog.ExecResult{
		Command:   result.Command,
		Args:      result.Args,
		Dir:       result.Dir,
		ExitCode:  result.ExitCode,
		Stdout:    result.Stdout,
		Stderr:    result.Stderr,
		TimedOut:  result.TimedOut,
		Truncated: result.Truncated,
		Duration:  result.Duration,
	}, nil
}

// 端口实现断言。
var _ catalog.ProcessRunner = processAccessor{}
