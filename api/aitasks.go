// aitasks.go — AI 任务记录的只读查询与取消端点。
//
// 端点（Bearer 认证，与其它 /api/v1 一致）：
//
//	GET  /api/v1/ai/tasks?session=<id>&limit=50   会话的任务记录（倒序）
//	GET  /api/v1/ai/tasks/{id}                    任务详情
//	POST /api/v1/ai/tasks/{id}/cancel             取消任务（同步取消会话计划）
//
// 数据来自 AI 插件的任务存储（ai_tasks 表 / 进程内存储），因此插件未加载时
// 返回 404 而不是空列表：让调用方能区分"没有任务"与"能力不可用"。
package api

import (
	"errors"
	"net/http"

	"github.com/KomeiDiSanXian/remilia/builtin/ai"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/task"
)

// resolveAI 从插件管理器动态获取 AI 插件实例。
func (s *Server) resolveAI() *ai.Plugin {
	if s.pluginMgr == nil {
		return nil
	}
	inst, ok := s.pluginMgr.Get("ai")
	if !ok {
		return nil
	}
	api := inst.GetAPI()
	if api == nil {
		return nil
	}
	p, ok := api.(*ai.Plugin)
	if !ok {
		return nil
	}
	return p
}

// handleListAITasks 处理 GET /api/v1/ai/tasks?session=<id>&limit=50。
func (s *Server) handleListAITasks(w http.ResponseWriter, r *http.Request) {
	p := s.resolveAI()
	if p == nil {
		writeErr(w, 404, "ai plugin not available", http.StatusNotFound)
		return
	}
	sessionID := r.URL.Query().Get("session")
	if sessionID == "" {
		writeErr(w, 400, "session query parameter is required", http.StatusBadRequest)
		return
	}
	limit := parseLimit(r.URL.Query().Get("limit"), defaultAITaskLimit)
	list, err := p.TasksBySession(sessionID, limit)
	if err != nil {
		writeErr(w, 500, err.Error(), http.StatusInternalServerError)
		return
	}
	writeOK(w, map[string]any{
		"tasks":   list,
		"count":   len(list),
		"session": sessionID,
	})
}

// handleGetAITask 处理 GET /api/v1/ai/tasks/{id}。
func (s *Server) handleGetAITask(w http.ResponseWriter, r *http.Request) {
	p := s.resolveAI()
	if p == nil {
		writeErr(w, 404, "ai plugin not available", http.StatusNotFound)
		return
	}
	t, err := p.TaskByID(r.PathValue("id"))
	if errors.Is(err, task.ErrNotFound) {
		writeErr(w, 404, "task not found", http.StatusNotFound)
		return
	}
	if err != nil {
		writeErr(w, 500, err.Error(), http.StatusInternalServerError)
		return
	}
	writeOK(w, t)
}

// handleCancelAITask 处理 POST /api/v1/ai/tasks/{id}/cancel。
func (s *Server) handleCancelAITask(w http.ResponseWriter, r *http.Request) {
	p := s.resolveAI()
	if p == nil {
		writeErr(w, 404, "ai plugin not available", http.StatusNotFound)
		return
	}
	id := r.PathValue("id")
	if err := p.CancelTaskByID(id); err != nil {
		if errors.Is(err, task.ErrNotFound) {
			writeErr(w, 404, "task not found", http.StatusNotFound)
			return
		}
		if errors.Is(err, task.ErrTaskTerminal) {
			writeErr(w, 409, "task is already finished", http.StatusConflict)
			return
		}
		writeErr(w, 500, err.Error(), http.StatusInternalServerError)
		return
	}
	writeOK(w, map[string]any{"id": id, "status": string(task.StatusCancelled)})
}

// defaultAITaskLimit 管理面单次查询的默认条数。
const defaultAITaskLimit = 50
