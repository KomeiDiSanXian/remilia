package api

import (
	"net/http"
	"strconv"

	"github.com/KomeiDiSanXian/remilia/builtin/auditlog"
)

// resolveAuditLog 从插件管理器动态获取审计日志插件实例。
func (s *Server) resolveAuditLog() *auditlog.Plugin {
	if s.pluginMgr == nil {
		return nil
	}
	inst, ok := s.pluginMgr.Get("auditlog")
	if !ok {
		return nil
	}
	api := inst.GetAPI()
	if api == nil {
		return nil
	}
	p, ok := api.(*auditlog.Plugin)
	if !ok {
		return nil
	}
	return p
}

// handleGetAuditLog 处理 GET /api/v1/auditlog?n=50
// 返回最近的审计日志条目。
func (s *Server) handleGetAuditLog(w http.ResponseWriter, r *http.Request) {
	p := s.resolveAuditLog()
	if p == nil {
		writeErr(w, 404, "audit log plugin not available", http.StatusNotFound)
		return
	}
	n := parseLimit(r.URL.Query().Get("n"), 50)
	entries := p.Recent(n)
	writeOK(w, map[string]any{
		"entries": entries,
		"count":   len(entries),
		"total":   p.Count(),
	})
}

// handleGetAuditLogByUser 处理 GET /api/v1/auditlog/user/{id}?n=50
func (s *Server) handleGetAuditLogByUser(w http.ResponseWriter, r *http.Request) {
	p := s.resolveAuditLog()
	if p == nil {
		writeErr(w, 404, "audit log plugin not available", http.StatusNotFound)
		return
	}
	userID := r.PathValue("id")
	n := parseLimit(r.URL.Query().Get("n"), 50)
	entries := p.ByUser(userID, n)
	writeOK(w, map[string]any{
		"entries": entries,
		"count":   len(entries),
		"user_id": userID,
	})
}

// handleGetAuditLogByAction 处理 GET /api/v1/auditlog/action/{action}?n=50
func (s *Server) handleGetAuditLogByAction(w http.ResponseWriter, r *http.Request) {
	p := s.resolveAuditLog()
	if p == nil {
		writeErr(w, 404, "audit log plugin not available", http.StatusNotFound)
		return
	}
	action := r.PathValue("action")
	n := parseLimit(r.URL.Query().Get("n"), 50)
	entries := p.ByAction(action, n)
	writeOK(w, map[string]any{
		"entries": entries,
		"count":   len(entries),
		"action":  action,
	})
}

// handleGetAuditLogCount 处理 GET /api/v1/auditlog/count
func (s *Server) handleGetAuditLogCount(w http.ResponseWriter, _ *http.Request) {
	p := s.resolveAuditLog()
	if p == nil {
		writeErr(w, 404, "audit log plugin not available", http.StatusNotFound)
		return
	}
	writeOK(w, map[string]any{"total": p.Count()})
}

// parseLimit 解析查询参数 n，限制返回值范围。
func parseLimit(s string, defaultVal int) int {
	if s == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return defaultVal
	}
	if n > 500 {
		return 500
	}
	return n
}
