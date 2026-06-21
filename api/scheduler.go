package api

import (
	"net/http"
	"strconv"

	"github.com/KomeiDiSanXian/remilia/builtin/scheduler"
)

// resolveScheduler 从插件管理器动态获取 scheduler 插件实例。
func (s *Server) resolveScheduler() *scheduler.Plugin {
	if s.pluginMgr == nil {
		return nil
	}
	// 通过容器获取（scheduler.Get 方式）
	inst, ok := s.pluginMgr.Get("scheduler")
	if !ok {
		return nil
	}
	api := inst.GetAPI()
	if api == nil {
		return nil
	}
	p, ok := api.(*scheduler.Plugin)
	if !ok {
		return nil
	}
	return p
}

// handleListSchedulerJobs 处理 GET /api/v1/scheduler/jobs
// scheduler 只暴露 count 不暴露列表。返回计数供前端参考。
func (s *Server) handleListSchedulerJobs(w http.ResponseWriter, _ *http.Request) {
	p := s.resolveScheduler()
	if p == nil {
		writeErr(w, 404, "scheduler plugin not available", http.StatusNotFound)
		return
	}
	writeOK(w, map[string]any{
		"count": p.Jobs(),
	})
}

// handleGetSchedulerHistory 处理 GET /api/v1/scheduler/history?n=50
func (s *Server) handleGetSchedulerHistory(w http.ResponseWriter, r *http.Request) {
	p := s.resolveScheduler()
	if p == nil {
		writeErr(w, 404, "scheduler plugin not available", http.StatusNotFound)
		return
	}
	n := parseLimit(r.URL.Query().Get("n"), 50)
	history := p.History(n)
	writeOK(w, map[string]any{
		"history": history,
		"count":   len(history),
	})
}

// handleDeleteSchedulerJob 处理 DELETE /api/v1/scheduler/jobs/{id}
func (s *Server) handleDeleteSchedulerJob(w http.ResponseWriter, r *http.Request) {
	p := s.resolveScheduler()
	if p == nil {
		writeErr(w, 404, "scheduler plugin not available", http.StatusNotFound)
		return
	}
	idStr := r.PathValue("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		writeErr(w, 400, "invalid job id", http.StatusBadRequest)
		return
	}
	p.Remove(scheduler.JobID(id))
	writeOK(w, map[string]string{"message": "job removed"})
}
