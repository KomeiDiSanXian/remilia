package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/KomeiDiSanXian/remilia/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestHandler(t *testing.T) (http.Handler, string) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(cfgPath, []byte("bot:\n  qq:\n    webhook:\n      host: \"0.0.0.0\"\n      port: 8080\n"), 0644)
	require.NoError(t, err)
	_, err = config.Load(cfgPath)
	require.NoError(t, err)

	deps := Deps{ConfigPath: cfgPath}
	srv := NewServer(":0", "test-api-key", deps)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	return mux, "test-api-key"
}

func authHeader(apiKey string) (string, string) {
	return "Authorization", "Bearer " + apiKey
}

func TestHandleHealth(t *testing.T) {
	h, _ := newTestHandler(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	h.ServeHTTP(w, r)
	assert.Equal(t, 200, w.Code)

	var resp APIResponse
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, 0, resp.Code)
}

func TestHandleVersion(t *testing.T) {
	h, _ := newTestHandler(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/version", nil)
	h.ServeHTTP(w, r)
	assert.Equal(t, 200, w.Code)
}

func TestAuthMiddleware_ValidKey(t *testing.T) {
	h, apiKey := newTestHandler(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/bots", nil)
	r.Header.Set(authHeader(apiKey))
	h.ServeHTTP(w, r)
	assert.NotEqual(t, 401, w.Code)
}

func TestAuthMiddleware_MissingKey(t *testing.T) {
	h, _ := newTestHandler(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/bots", nil)
	h.ServeHTTP(w, r)
	assert.Equal(t, 401, w.Code)
}

func TestAuthMiddleware_WrongKey(t *testing.T) {
	h, _ := newTestHandler(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/bots", nil)
	r.Header.Set("Authorization", "Bearer wrong-key")
	h.ServeHTTP(w, r)
	assert.Equal(t, 401, w.Code)
}

func TestAuthMiddleware_BadScheme(t *testing.T) {
	h, _ := newTestHandler(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/bots", nil)
	r.Header.Set("Authorization", "Basic dGVzdDp0ZXN0")
	h.ServeHTTP(w, r)
	assert.Equal(t, 401, w.Code)
}

func TestHandleGetConfig(t *testing.T) {
	h, apiKey := newTestHandler(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	r.Header.Set(authHeader(apiKey))
	h.ServeHTTP(w, r)
	assert.Equal(t, 200, w.Code)

	var resp APIResponse
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, 0, resp.Code)
}

func TestHandleGetConfig_NoAuth(t *testing.T) {
	h, _ := newTestHandler(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	h.ServeHTTP(w, r)
	assert.Equal(t, 401, w.Code)
}

func TestHandleReloadConfig(t *testing.T) {
	h, apiKey := newTestHandler(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/config/reload", nil)
	r.Header.Set(authHeader(apiKey))
	h.ServeHTTP(w, r)
	assert.Equal(t, 200, w.Code)
}

func TestWriteConfigUpdate_FileNotFound(t *testing.T) {
	srv := &Server{configPath: "/nonexistent/path/config.yaml"}
	err := srv.writeConfigUpdate(map[string]any{"log": map[string]any{"level": "debug"}})
	assert.Error(t, err)
}

func TestWriteConfigUpdate_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(path, []byte("invalid: [bad"), 0644)
	require.NoError(t, err)

	srv := &Server{configPath: path}
	err = srv.writeConfigUpdate(map[string]any{"foo": "bar"})
	assert.Error(t, err)
}

func TestHandleBots_NoAuth(t *testing.T) {
	h, _ := newTestHandler(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/bots", nil)
	h.ServeHTTP(w, r)
	assert.Equal(t, 401, w.Code)
}

func TestHandlePlugins_NoAuth(t *testing.T) {
	h, _ := newTestHandler(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/plugins", nil)
	h.ServeHTTP(w, r)
	assert.Equal(t, 401, w.Code)
}

func TestHandlePlatforms_NoAuth(t *testing.T) {
	h, _ := newTestHandler(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/platforms", nil)
	h.ServeHTTP(w, r)
	assert.Equal(t, 401, w.Code)
}

func TestHandlePermission_NoAuth(t *testing.T) {
	h, _ := newTestHandler(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/permission/roles", nil)
	h.ServeHTTP(w, r)
	assert.Equal(t, 401, w.Code)
}

func TestHandleFSM_NoAuth(t *testing.T) {
	h, _ := newTestHandler(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/fsm", nil)
	h.ServeHTTP(w, r)
	assert.Equal(t, 401, w.Code)
}

func TestHandleLogs_NoAuth(t *testing.T) {
	h, _ := newTestHandler(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/logs", nil)
	h.ServeHTTP(w, r)
	assert.Equal(t, 401, w.Code)
}

func TestHandleGetBots_WithAuth(t *testing.T) {
	h, apiKey := newTestHandler(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/bots", nil)
	r.Header.Set(authHeader(apiKey))
	h.ServeHTTP(w, r)
	assert.Equal(t, 200, w.Code)

	var resp APIResponse
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, 0, resp.Code)
}

func TestHandleGetPlugins_WithAuth(t *testing.T) {
	h, apiKey := newTestHandler(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/plugins", nil)
	r.Header.Set(authHeader(apiKey))
	h.ServeHTTP(w, r)
	assert.Equal(t, 200, w.Code)
}

func TestHandleGetPlatforms_WithAuth(t *testing.T) {
	h, apiKey := newTestHandler(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/platforms", nil)
	r.Header.Set(authHeader(apiKey))
	h.ServeHTTP(w, r)
	assert.Equal(t, 200, w.Code)
}

func TestHandleUnknownRoute(t *testing.T) {
	h, _ := newTestHandler(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/nonexistent", nil)
	h.ServeHTTP(w, r)
	assert.Equal(t, 404, w.Code)
}

func TestHandleStats(t *testing.T) {
	h, apiKey := newTestHandler(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil)
	r.Header.Set(authHeader(apiKey))
	h.ServeHTTP(w, r)
	// pluginMgr 为空时返回 404
	assert.Equal(t, 404, w.Code)
}

func TestDeleteConfigKey_Empty(t *testing.T) {
	m := map[string]any{"keep": "me"}
	deleteConfigKey(m)
	assert.Equal(t, "me", m["keep"])
}

func TestDeleteConfigKey_NonMapIntermediate(t *testing.T) {
	m := map[string]any{"key": "scalar"}
	deleteConfigKey(m, "key", "sub")
	assert.Equal(t, "scalar", m["key"])
}
