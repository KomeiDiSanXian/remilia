// Package ai artifacts.go — 任务产物登记：把工作区写入记录到活动任务。
//
// 任务报告需要回答"这次改了什么"。文件写入/精确替换成功后，本文件把路径与
// 大小登记到会话的活动任务（task.Artifact），随任务持久化，并由 /ai task 与
// 管理 API 展示。没有活动任务（只读部署、任务已终结）时静默跳过。
package ai

import (
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/catalog"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/runtime"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/task"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// artifactRecorder 包装工作区写入端口：成功的写入登记为任务产物。
type artifactRecorder struct {
	p     *Plugin
	ctx   *eventctx.Context
	inner catalog.FileWriteAccess
}

// Write 覆盖写入并登记产物。
func (r artifactRecorder) Write(rel, content string) (catalog.FileWriteResult, error) {
	result, err := r.inner.Write(rel, content)
	if err == nil {
		r.p.recordFileArtifact(r.ctx, result.Path, result.Bytes, result.Created)
	}
	return result, err
}

// Edit 精确替换并登记产物。
func (r artifactRecorder) Edit(rel, oldString, newString string) (catalog.FileEditResult, error) {
	result, err := r.inner.Edit(rel, oldString, newString)
	if err == nil {
		r.p.recordFileArtifact(r.ctx, result.Path, result.Bytes, false)
	}
	return result, err
}

// fileArtifactRecordingWriter 包装写入端口；inner 为空时返回 nil。
func (p *Plugin) fileArtifactRecordingWriter(ctx *eventctx.Context, inner catalog.FileWriteAccess) catalog.FileWriteAccess {
	if p == nil || inner == nil || ctx == nil {
		return nil
	}
	return artifactRecorder{p: p, ctx: ctx, inner: inner}
}

// recordFileArtifact 把一次成功的工作区写入登记为活动任务的产物。
//
// 失败（无活动任务、任务已终结、存储报错）只记日志，不影响写入结果：
// 产物登记是观测，不是写入成功的前提。
func (p *Plugin) recordFileArtifact(ctx *eventctx.Context, path string, size int, created bool) {
	if p == nil || ctx == nil || path == "" {
		return
	}
	sessionID := runtime.MakeSessionID(ctx.GetEventPlatform(), ctx.GetChatInfo().ID, ctx.GetSenderInfo().ID)

	p.taskMu.Lock()
	defer p.taskMu.Unlock()
	store := p.taskStore()
	t, err := store.ActiveBySession(sessionID)
	if err != nil {
		return
	}
	kind := task.ArtifactFileUpdated
	if created {
		kind = task.ArtifactFileCreated
	}
	if err := t.RecordArtifact(task.Artifact{
		Kind: kind,
		Name: path,
		Size: size,
		At:   time.Now(),
	}); err != nil {
		logger.Debugf("[AI] Task %s rejected artifact %q: %v", t.ID, path, err)
		return
	}
	if err := store.Save(t); err != nil {
		logger.Errorf("[AI] Failed to persist artifact for task %s: %v", t.ID, err)
	}
}
