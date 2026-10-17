package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// doJSON 发起一次带认证的请求并解析统一响应体。
func doJSON(t *testing.T, h http.Handler, method, path string) (int, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(method, path, nil)
	r.Header.Set("Authorization", "Bearer test-key")
	h.ServeHTTP(w, r)
	var resp map[string]any
	_ = json.NewDecoder(w.Body).Decode(&resp)
	return w.Code, resp
}

// TestEndpoint_AITasksWithoutPlugin AI 插件未加载时任务端点返回 404，
// 让调用方能区分"没有任务"与"能力不可用"。
func TestEndpoint_AITasksWithoutPlugin(t *testing.T) {
	h := newTestServer(t, nil)

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/ai/tasks?session=synthetic:chat:user"},
		{http.MethodGet, "/api/v1/ai/tasks/t-1"},
		{http.MethodPost, "/api/v1/ai/tasks/t-1/cancel"},
	}
	for _, tc := range cases {
		code, resp := doJSON(t, h, tc.method, tc.path)
		assert.Equalf(t, http.StatusNotFound, code, "%s %s", tc.method, tc.path)
		assert.Contains(t, resp["message"], "ai plugin not available")
	}
}
