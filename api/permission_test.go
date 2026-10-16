package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/KomeiDiSanXian/remilia/core/permission"
	"github.com/stretchr/testify/assert"
)

func newPermissionTestHandler(t *testing.T) (http.Handler, string) {
	t.Helper()
	srv := NewServer(":0", "test-api-key", Deps{PermissionMgr: permission.NewPermissionManager()})
	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	return mux, "test-api-key"
}

// TestDeleteRole_AllowsRecreate 回归：删除角色必须移除角色定义，
// 同名重建不能再返回 409，二次删除应返回 404。
func TestDeleteRole_AllowsRecreate(t *testing.T) {
	h, apiKey := newPermissionTestHandler(t)

	postRole := func(want int) {
		t.Helper()
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/v1/permission/roles",
			strings.NewReader(`{"name":"temp","permissions":["command:execute"]}`))
		r.Header.Set(authHeader(apiKey))
		h.ServeHTTP(w, r)
		assert.Equal(t, want, w.Code, w.Body.String())
	}

	postRole(200)
	postRole(409)

	deleteRole := func(want int) {
		t.Helper()
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodDelete, "/api/v1/permission/roles/temp", nil)
		r.Header.Set(authHeader(apiKey))
		h.ServeHTTP(w, r)
		assert.Equal(t, want, w.Code, w.Body.String())
	}

	deleteRole(200)
	// 删除后同名重建必须成功（此前定义仍在，会返回 409）
	postRole(200)
	deleteRole(200)
	// 角色确已移除
	deleteRole(404)
}
