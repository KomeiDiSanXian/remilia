package plugin

// test_engine_helper_test.go — 需要真实引擎协调器的插件测试辅助

import (
	stdctx "context"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/engine"
)

// newTestPluginEngine 创建带测试生命周期的引擎（用作 Manager 的协调器）。
func newTestPluginEngine(t *testing.T) *engine.Engine {
	t.Helper()
	eng := engine.NewEngine(engine.WithNoBackgroundWorkers())
	t.Cleanup(func() {
		ctx, cancel := stdctx.WithTimeout(stdctx.Background(), 5*time.Second)
		defer cancel()
		_ = eng.Shutdown(ctx)
	})
	return eng
}
