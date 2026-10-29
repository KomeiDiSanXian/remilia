// evidence.go — 进程执行证据与计划完成闸门。
//
// 文档要求"任务完成不能仅依赖模型自行宣称成功"。可执行的确定性证据由
// process_run 提供：命令、退出码与时间。本文件记录每个会话最近一次进程执行
// 结果，并把它交给计划侧的完成闸门（catalog.PlanAccess.CompletionGuard）：
// 计划即将全部完成时，如果最近一次命令以非零退出码结束，完成请求被拒绝，
// 模型必须先修复问题并重新执行验证命令，或如实把步骤标记为 failed。
//
// 证据是会话级、进程内状态：重启后重置（重启恢复本身会把任务置为 interrupted，
// 不会绕过闸门），也不随会话记录落库。

package ai

import (
	"context"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/catalog"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/runtime"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
)

// execEvidence 一次进程执行的证据快照。
type execEvidence struct {
	// Command 命令展示行（命令名 + 参数）。
	Command string
	// ExitCode 退出码。
	ExitCode int
	// At 执行完成时间。
	At time.Time
}

// recordExecEvidence 记录会话最近一次进程执行结果。
func (p *Plugin) recordExecEvidence(ctx *eventctx.Context, result catalog.ExecResult) {
	if ctx == nil {
		return
	}
	sessionID := runtime.MakeSessionID(ctx.GetEventPlatform(), ctx.GetChatInfo().ID, ctx.GetSenderInfo().ID)
	p.evidenceMu.Lock()
	defer p.evidenceMu.Unlock()
	if p.execEvidence == nil {
		p.execEvidence = make(map[string]execEvidence)
	}
	p.execEvidence[sessionID] = execEvidence{
		Command:  runtime.CommandLine(result.Command, result.Args),
		ExitCode: result.ExitCode,
		At:       time.Now(),
	}
}

// lastExecEvidence 返回会话最近一次进程执行证据。
func (p *Plugin) lastExecEvidence(sessionID string) (execEvidence, bool) {
	p.evidenceMu.Lock()
	defer p.evidenceMu.Unlock()
	evidence, ok := p.execEvidence[sessionID]
	return evidence, ok
}

// clearExecEvidence 清除会话的执行证据（新计划开始时调用，避免旧任务的失败
// 结果阻塞新任务完成）。
func (p *Plugin) clearExecEvidence(sessionID string) {
	p.evidenceMu.Lock()
	defer p.evidenceMu.Unlock()
	delete(p.execEvidence, sessionID)
}

// evidenceRunner 包装进程执行端口：成功发起的调用结束后记录证据。
// 无法开始执行的调用（白名单拒绝等）不产生证据。
type evidenceRunner struct {
	p     *Plugin
	ctx   *eventctx.Context
	inner catalog.ProcessRunner
}

// Run 执行并记录证据。
func (r evidenceRunner) Run(ctx context.Context, req catalog.ExecRequest) (catalog.ExecResult, error) {
	result, err := r.inner.Run(ctx, req)
	if err == nil {
		r.p.recordExecEvidence(r.ctx, result)
	}
	return result, err
}

// evidenceRecordingRunner 在进程执行端口外层加证据记录；inner 为空时返回 nil。
func (p *Plugin) evidenceRecordingRunner(ctx *eventctx.Context, inner catalog.ProcessRunner) catalog.ProcessRunner {
	if inner == nil || ctx == nil {
		return nil
	}
	return evidenceRunner{p: p, ctx: ctx, inner: inner}
}
