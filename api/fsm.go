package api

import (
	"net/http"

	"github.com/KomeiDiSanXian/remilia/core/fsm"
)

// fsmEngine 返回 FSM 引擎引用。
func (s *Server) fsmEngine() *fsm.Engine {
	if s.fsmMgr != nil {
		return s.fsmMgr.Engine()
	}
	return nil
}

// handleListFSMs 处理 GET /api/v1/fsm
func (s *Server) handleListFSMs(w http.ResponseWriter, _ *http.Request) {
	eng := s.fsmEngine()
	if eng == nil {
		writeErr(w, 404, "FSM engine not available", http.StatusNotFound)
		return
	}
	writeOK(w, map[string]any{
		"fsms": eng.ListFSMs(),
	})
}

// handleGetFSM 处理 GET /api/v1/fsm/{name}
func (s *Server) handleGetFSM(w http.ResponseWriter, r *http.Request) {
	eng := s.fsmEngine()
	if eng == nil {
		writeErr(w, 404, "FSM engine not available", http.StatusNotFound)
		return
	}
	name := r.PathValue("name")
	f := eng.GetFSM(name)
	if f == nil {
		writeErr(w, 404, "FSM not found", http.StatusNotFound)
		return
	}
	writeOK(w, map[string]any{
		"name":    f.Name,
		"initial": f.Initial,
		"timeout": f.Timeout.String(),
	})
}

// handleListFSMSessions 处理 GET /api/v1/fsm/sessions
// FSM 引擎不提供枚举所有会话的公开方法。
func (s *Server) handleListFSMSessions(w http.ResponseWriter, _ *http.Request) {
	writeOK(w, map[string]any{
		"sessions": []any{},
	})
}

// handleEndFSMSession 处理 DELETE /api/v1/fsm/sessions/{id}
func (s *Server) handleEndFSMSession(w http.ResponseWriter, r *http.Request) {
	eng := s.fsmEngine()
	if eng == nil {
		writeErr(w, 404, "FSM engine not available", http.StatusNotFound)
		return
	}
	sessionID := r.PathValue("id")
	eng.EndSession(sessionID)
	writeOK(w, map[string]string{"message": "session ended"})
}
