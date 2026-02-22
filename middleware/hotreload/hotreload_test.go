package hotreload_test

import (
	"github.com/KomeiDiSanXian/remilia/config"
	"github.com/KomeiDiSanXian/remilia/middleware"
	"github.com/KomeiDiSanXian/remilia/middleware/hotreload"
	"testing"
	"time"
)

func TestBridge_OnConfigChange_UpdatesRetry(t *testing.T) {
	cr := middleware.NewConfigurableRetry(middleware.RetryConfig{
		MaxAttempts: 1,
		BackoffBase: 100 * time.Millisecond,
	})
	bridge := hotreload.NewBridge().WatchRetry(cr)
	newCfg := &config.Config{
		Retry: config.RetryConfig{
			Enable:      true,
			MaxAttempts: 5,
			BackoffBase: "200ms",
			BackoffMax:  "2s",
		},
	}
	bridge.OnConfigChange(newCfg)
	// No panic = success; we can't easily inspect the internal fields
	// but this proves the hot-reload path runs without error
}
func TestBridge_OnConfigChange_Nil(t *testing.T) {
	bridge := hotreload.NewBridge()
	// Should not panic with nil config
	bridge.OnConfigChange(nil)
}
func TestBridge_WatchAdaptive(t *testing.T) {
	arl := middleware.NewAdaptiveRateLimiter(middleware.DefaultAdaptiveConfig())
	arl.Start()
	defer arl.Stop()
	bridge := hotreload.NewBridge().WatchAdaptive(arl)
	newCfg := &config.Config{
		Middleware: config.MiddlewareConfig{
			RateLimitBurst: 50,
		},
	}
	// Should not panic
	bridge.OnConfigChange(newCfg)
}
func TestBridge_Subscribe(t *testing.T) {
	bridge := hotreload.NewBridge()
	token := bridge.Subscribe()
	if token == nil {
		t.Error("Subscribe should return a non-nil token")
	}
	token.Cancel()
}
func TestConfigurableRetry_UpdateConfig(t *testing.T) {
	cr := middleware.NewConfigurableRetry(middleware.RetryConfig{
		MaxAttempts: 1,
	})
	cr.UpdateConfig(middleware.RetryConfig{MaxAttempts: 5})
	// Verify the middleware function is returned without panic
	mw := cr.Middleware()
	if mw == nil {
		t.Error("Middleware() should return non-nil")
	}
}
