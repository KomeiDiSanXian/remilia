package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/errutil"
	"github.com/KomeiDiSanXian/remilia/plugin"
	"github.com/stretchr/testify/assert"
)

// stubCoordinator 让生命周期前置检查通过；测试只访问不存在的插件，
// 因此嵌入的接口方法不会被调用。
type stubCoordinator struct{ engine.PluginCoordinator }

func TestWritePluginOpError(t *testing.T) {
	w := httptest.NewRecorder()
	writePluginOpError(w, "enable", "ghost", errutil.ErrPluginNotFound)
	assert.Equal(t, 404, w.Code)

	w = httptest.NewRecorder()
	writePluginOpError(w, "reload", "demo", errors.New("boom"))
	assert.Equal(t, 500, w.Code)
}

// TestPluginLifecycle_MissingPluginReturns404 回归：对不存在的插件启停/重载
// 此前一律返回 500，应返回 404。
func TestPluginLifecycle_MissingPluginReturns404(t *testing.T) {
	mgr := plugin.NewManager(&stubCoordinator{})
	srv := NewServer(":0", "test-api-key", Deps{PluginMgr: mgr})
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	for _, action := range []string{"enable", "disable", "reload"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/v1/plugins/ghost/"+action, nil)
		r.Header.Set(authHeader("test-api-key"))
		mux.ServeHTTP(w, r)
		assert.Equal(t, 404, w.Code, "action=%s body=%s", action, w.Body.String())
	}
}
