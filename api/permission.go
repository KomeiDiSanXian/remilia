package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/KomeiDiSanXian/remilia/core/permission"
)

type permPlugin interface {
	GetManager() *permission.Manager
}

func (s *Server) resolvePermMgr() *permission.Manager {
	if s.permMgr != nil {
		return s.permMgr
	}
	if s.pluginMgr != nil {
		inst, ok := s.pluginMgr.Get("permission")
		if ok {
			if p, ok2 := inst.GetAPI().(permPlugin); ok2 {
				return p.GetManager()
			}
		}
	}
	return nil
}

// handleListRoles 处理 GET /api/v1/permission/roles
func (s *Server) handleListRoles(w http.ResponseWriter, _ *http.Request) {
	pm := s.resolvePermMgr()
	if pm == nil {
		writeErr(w, 404, "permission manager not available", http.StatusNotFound)
		return
	}
	// 内置角色定义
	type roleDef struct {
		Name        string   `json:"name"`
		Permissions []string `json:"permissions"`
	}
	roleNames := pm.ListRoles()
	roleDefs := make([]roleDef, 0, len(roleNames))
	for _, name := range roleNames {
		if r, ok := pm.GetRole(name); ok {
			perms := r.Permissions()
			permStrs := make([]string, len(perms))
			for i, p := range perms {
				permStrs[i] = p.String()
			}
			roleDefs = append(roleDefs, roleDef{Name: name, Permissions: permStrs})
		}
	}
	writeOK(w, map[string]any{
		"roles":        roleDefs,
		"user_roles":   pm.ExportUserRoles(),
		"direct_perms": pm.ExportUserPerms(),
	})
}

// handleAssignRole 处理 POST /api/v1/permission/users/{userID}/roles
func (s *Server) handleAssignRole(w http.ResponseWriter, r *http.Request) {
	pm := s.resolvePermMgr()
	if pm == nil {
		writeErr(w, 404, "permission manager not available", http.StatusNotFound)
		return
	}
	userID := r.PathValue("userID")
	var body struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := pm.AssignRole(userID, body.Role); err != nil {
		writeErr(w, 400, err.Error(), http.StatusBadRequest)
		return
	}
	writeOK(w, map[string]string{"message": "role assigned"})
}

// handleRevokeRole 处理 DELETE /api/v1/permission/users/{userID}/roles/{role}
func (s *Server) handleRevokeRole(w http.ResponseWriter, r *http.Request) {
	pm := s.resolvePermMgr()
	if pm == nil {
		writeErr(w, 404, "permission manager not available", http.StatusNotFound)
		return
	}
	userID := r.PathValue("userID")
	role := r.PathValue("role")
	pm.RevokeRole(userID, role)
	writeOK(w, map[string]string{"message": "role revoked"})
}

// handleCreateRole 处理 POST /api/v1/permission/roles
func (s *Server) handleCreateRole(w http.ResponseWriter, r *http.Request) {
	pm := s.resolvePermMgr()
	if pm == nil {
		writeErr(w, 404, "permission manager not available", http.StatusNotFound)
		return
	}
	var body struct {
		Name        string   `json:"name"`
		Permissions []string `json:"permissions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid request body", http.StatusBadRequest)
		return
	}
	if body.Name == "" {
		writeErr(w, 400, "role name is required", http.StatusBadRequest)
		return
	}
	role := &permission.Role{Name: body.Name}
	for _, ps := range body.Permissions {
		parts := strings.SplitN(ps, ":", 2)
		res, act := parts[0], "*"
		if len(parts) == 2 {
			act = parts[1]
		}
		role.AddPermission(permission.Permission{Resource: res, Action: act})
	}
	if _, exists := pm.GetRole(body.Name); exists {
		writeErr(w, 409, "role already exists", http.StatusConflict)
		return
	}
	pm.RegisterRole(role)
	writeOK(w, map[string]string{"message": "role created"})
}

// handleDeleteRole 处理 DELETE /api/v1/permission/roles/{role}
func (s *Server) handleDeleteRole(w http.ResponseWriter, r *http.Request) {
	pm := s.resolvePermMgr()
	if pm == nil {
		writeErr(w, 404, "permission manager not available", http.StatusNotFound)
		return
	}
	name := r.PathValue("role")
	if _, ok := pm.GetRole(name); !ok {
		writeErr(w, 404, "role not found", http.StatusNotFound)
		return
	}
	// Manager doesn't have RemoveRole, so we create an empty role to overwrite
	pm.RegisterRole(&permission.Role{Name: name})
	writeOK(w, map[string]string{"message": "role deleted"})
}

// handleAddRolePermission 处理 POST /api/v1/permission/roles/{role}/permissions
func (s *Server) handleAddRolePermission(w http.ResponseWriter, r *http.Request) {
	pm := s.resolvePermMgr()
	if pm == nil {
		writeErr(w, 404, "permission manager not available", http.StatusNotFound)
		return
	}
	name := r.PathValue("role")
	role, ok := pm.GetRole(name)
	if !ok {
		writeErr(w, 404, "role not found", http.StatusNotFound)
		return
	}
	var body struct {
		Resource string `json:"resource"`
		Action   string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid request body", http.StatusBadRequest)
		return
	}
	role.AddPermission(permission.Permission{Resource: body.Resource, Action: body.Action})
	writeOK(w, map[string]string{"message": "permission added"})
}

// handleRemoveRolePermission 处理 DELETE /api/v1/permission/roles/{role}/permissions
func (s *Server) handleRemoveRolePermission(w http.ResponseWriter, r *http.Request) {
	pm := s.resolvePermMgr()
	if pm == nil {
		writeErr(w, 404, "permission manager not available", http.StatusNotFound)
		return
	}
	name := r.PathValue("role")
	role, ok := pm.GetRole(name)
	if !ok {
		writeErr(w, 404, "role not found", http.StatusNotFound)
		return
	}
	var body struct {
		Resource string `json:"resource"`
		Action   string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid request body", http.StatusBadRequest)
		return
	}
	role.RemovePermission(permission.Permission{Resource: body.Resource, Action: body.Action})
	writeOK(w, map[string]string{"message": "permission removed"})
}

// handleCheckPermission 处理 POST /api/v1/permission/check
func (s *Server) handleCheckPermission(w http.ResponseWriter, r *http.Request) {
	pm := s.resolvePermMgr()
	if pm == nil {
		writeErr(w, 404, "permission manager not available", http.StatusNotFound)
		return
	}
	var body struct {
		UserID   string `json:"user_id"`
		Resource string `json:"resource"`
		Action   string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid request body", http.StatusBadRequest)
		return
	}
	perm := permission.Permission{Resource: body.Resource, Action: body.Action}
	allowed := pm.HasPermission(body.UserID, perm)
	writeOK(w, map[string]any{
		"user_id":  body.UserID,
		"resource": body.Resource,
		"action":   body.Action,
		"allowed":  allowed,
	})
}

// handleGetUserPermissions 处理 GET /api/v1/permission/users/{userID}/permissions
func (s *Server) handleGetUserPermissions(w http.ResponseWriter, r *http.Request) {
	pm := s.resolvePermMgr()
	if pm == nil {
		writeErr(w, 404, "permission manager not available", http.StatusNotFound)
		return
	}
	userID := r.PathValue("userID")
	roles := pm.GetUserRoles(userID)
	perms := pm.GetUserPermissions(userID)
	writeOK(w, map[string]any{
		"user_id":     userID,
		"roles":       roles,
		"permissions": perms,
	})
}
