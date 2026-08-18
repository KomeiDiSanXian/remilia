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
// 返回当前所有 FSM 会话的只读快照。
func (s *Server) handleListFSMSessions(w http.ResponseWriter, _ *http.Request) {
	eng := s.fsmEngine()
	if eng == nil {
		writeErr(w, 404, "FSM engine not available", http.StatusNotFound)
		return
	}
	type sessionResp struct {
		ID        string `json:"id"`
		FSMName   string `json:"fsm_name"`
		Current   string `json:"current"`
		CreatedAt int64  `json:"created_at"`
		UpdatedAt int64  `json:"updated_at"`
		ExpireAt  int64  `json:"expire_at,omitempty"`
	}
	sessions := eng.ListSessions()
	out := make([]sessionResp, 0, len(sessions))
	for _, sess := range sessions {
		out = append(out, sessionResp{
			ID:        sess.ID,
			FSMName:   sess.FSMName,
			Current:   string(sess.Current),
			CreatedAt: sess.CreatedAt,
			UpdatedAt: sess.UpdatedAt,
			ExpireAt:  sess.ExpireAt,
		})
	}
	writeOK(w, map[string]any{
		"sessions": out,
		"count":    len(out),
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
